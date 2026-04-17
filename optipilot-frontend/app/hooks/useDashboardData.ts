"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  fetchAuditTrail,
  fetchServiceMetrics,
  fetchServicePredictions,
  fetchServicesSnapshot,
  fetchSystemStatusSnapshot,
  setAllServicesMode,
  setKillSwitch,
  type ControllerDecision,
  type ServiceSummary,
} from "@/lib/api";
import { useWsEvents } from "@/lib/ws";
import type {
  AuditLogEntry,
  OperatingMode,
  ReplicaState,
  ScalingAction,
  TrafficPoint,
} from "@/app/types/dashboard";

const MODE_OPTIONS: OperatingMode[] = ["shadow", "recommend", "autonomous"];
const MAX_TRAFFIC_POINTS = 48;
const MAX_AUDIT_LOGS = 64;

type JsonRecord = Record<string, unknown>;

interface PredictionSnapshot {
  p50: number;
  p90: number;
}

function asRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" ? (value as JsonRecord) : {};
}

function readString(
  source: JsonRecord,
  keys: string[],
  fallback = "",
): string {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" && value.trim() !== "") {
      return value;
    }
  }
  return fallback;
}

function readNumber(source: JsonRecord, keys: string[], fallback = 0): number {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return fallback;
}

function readBoolean(
  source: JsonRecord,
  keys: string[],
  fallback = false,
): boolean {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "boolean") {
      return value;
    }
  }
  return fallback;
}

function toTimestampMillis(raw: unknown): number {
  if (typeof raw === "number") {
    return raw;
  }
  if (typeof raw === "string") {
    const parsed = Date.parse(raw);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return Date.now();
}

function buildTimeLabel(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function normalizeMode(mode: string): OperatingMode | null {
  const normalized = mode.toLowerCase();
  if (normalized === "shadow" || normalized === "recommend" || normalized === "autonomous") {
    return normalized;
  }
  return null;
}

function deriveAction(
  oldReplicas: number,
  newReplicas: number,
  executed: boolean,
): ScalingAction {
  if (newReplicas === oldReplicas) {
    return "hold";
  }
  if (!executed) {
    return "simulated";
  }
  return newReplicas > oldReplicas ? "scale_up" : "scale_down";
}

function mapDecisionToAuditEntry(decision: ControllerDecision): AuditLogEntry {
  const oldReplicas = Number(decision.OldReplicas ?? 0);
  const newReplicas = Number(decision.NewReplicas ?? oldReplicas);
  return {
    id: String(decision.ID),
    timestamp: toTimestampMillis(decision.CreatedAt),
    service: decision.ServiceName ?? "unknown",
    previousReplicas: oldReplicas,
    newReplicas,
    confidence: Number(decision.ConfidenceScore ?? 0),
    forecastP50: Number(decision.RpsP50 ?? 0),
    forecastP90: Number(decision.RpsP90 ?? 0),
    mode: String(decision.ScalingMode ?? "unknown"),
    action: deriveAction(oldReplicas, newReplicas, Boolean(decision.Executed)),
    reason: decision.Reason ?? "",
  };
}

function mapServicesToReplicaState(services: ServiceSummary[]): ReplicaState[] {
  return services.map((service) => ({
    service: service.name,
    replicas: service.current_replicas ?? 0,
    minReplicas: service.min_replicas ?? 1,
    maxReplicas: service.max_replicas ?? Math.max(1, service.current_replicas ?? 1),
  }));
}

function readPredictionFromRecord(record: JsonRecord): PredictionSnapshot {
  const p50 = readNumber(record, ["rps_p50", "RpsP50", "p50", "P50"], 0);
  const p90 = readNumber(record, ["rps_p90", "RpsP90", "p90", "P90"], p50);
  return { p50, p90: Math.max(p90, p50) };
}

function buildTrafficSeries(
  metrics: JsonRecord[],
  predictions: JsonRecord[],
): TrafficPoint[] {
  const sortedMetrics = [...metrics].sort(
    (a, b) =>
      toTimestampMillis(a.CollectedAt ?? a.collected_at ?? a.Timestamp ?? a.timestamp) -
      toTimestampMillis(b.CollectedAt ?? b.collected_at ?? b.Timestamp ?? b.timestamp),
  );
  const sortedPredictions = [...predictions].sort(
    (a, b) =>
      toTimestampMillis(a.timestamp ?? a.Timestamp) -
      toTimestampMillis(b.timestamp ?? b.Timestamp),
  );

  let predictionIndex = 0;
  let activePrediction: PredictionSnapshot | null = null;

  const points = sortedMetrics.map((metric) => {
    const timestamp = toTimestampMillis(
      metric.CollectedAt ?? metric.collected_at ?? metric.Timestamp ?? metric.timestamp,
    );
    while (predictionIndex < sortedPredictions.length) {
      const candidate = sortedPredictions[predictionIndex];
      const candidateTimestamp = toTimestampMillis(
        candidate.timestamp ?? candidate.Timestamp,
      );
      if (candidateTimestamp > timestamp) {
        break;
      }
      activePrediction = readPredictionFromRecord(candidate);
      predictionIndex += 1;
    }
    const actual = readNumber(metric, ["RPS", "rps"], 0);
    const predictedP50 = activePrediction?.p50 ?? actual;
    const predictedP90 = activePrediction?.p90 ?? Math.max(actual, predictedP50);
    return {
      timestamp,
      timeLabel: buildTimeLabel(timestamp),
      actual,
      predictedP50,
      predictedP90,
    };
  });

  return points.slice(-MAX_TRAFFIC_POINTS);
}

export function useDashboardData() {
  const { lastMessage, isConnected } = useWsEvents();

  const [isLoading, setIsLoading] = useState(true);
  const [mode, setModeState] = useState<OperatingMode>("shadow");
  const [killSwitchActive, setKillSwitchActive] = useState(false);
  const [trafficSeries, setTrafficSeries] = useState<TrafficPoint[]>([]);
  const [replicas, setReplicas] = useState<ReplicaState[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>([]);
  const [lastUpdatedAt, setLastUpdatedAt] = useState(0);
  const [serviceNames, setServiceNames] = useState<string[]>([]);

  const primaryServiceRef = useRef("");
  const predictionsByServiceRef = useRef<Record<string, PredictionSnapshot>>({});
  const killSwitchRef = useRef(false);

  useEffect(() => {
    killSwitchRef.current = killSwitchActive;
  }, [killSwitchActive]);

  const loadInitialSnapshot = useCallback(async () => {
    setIsLoading(true);
    try {
      const [services, decisions, systemStatus] = await Promise.all([
        fetchServicesSnapshot(),
        fetchAuditTrail(64),
        fetchSystemStatusSnapshot(),
      ]);

      const names = services.map((service) => service.name);
      setServiceNames(names);
      setReplicas(mapServicesToReplicaState(services));

      const discoveredMode = normalizeMode(services[0]?.mode ?? "");
      if (discoveredMode) {
        setModeState(discoveredMode);
      }

      const killSwitchFromStatus = Boolean(
        systemStatus.controller?.global_kill_switch,
      );
      setKillSwitchActive(killSwitchFromStatus);
      killSwitchRef.current = killSwitchFromStatus;

      setAuditLogs(
        decisions
          .map(mapDecisionToAuditEntry)
          .sort((a, b) => b.timestamp - a.timestamp)
          .slice(0, MAX_AUDIT_LOGS),
      );

      const primaryService = names[0] ?? "";
      primaryServiceRef.current = primaryService;

      if (primaryService) {
        const [metrics, predictions] = await Promise.all([
          fetchServiceMetrics(primaryService, 60),
          fetchServicePredictions(primaryService, 100),
        ]);
        const normalizedMetrics = metrics.map(asRecord);
        const normalizedPredictions = predictions.map(asRecord);
        setTrafficSeries(
          buildTrafficSeries(normalizedMetrics, normalizedPredictions),
        );
        const latestPrediction = normalizedPredictions.at(-1);
        if (latestPrediction) {
          predictionsByServiceRef.current[primaryService] =
            readPredictionFromRecord(latestPrediction);
        }
      } else {
        setTrafficSeries([]);
      }

      setLastUpdatedAt(Date.now());
    } catch (error) {
      console.error("Failed to load dashboard snapshot", error);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadInitialSnapshot();
  }, [loadInitialSnapshot]);

  useEffect(() => {
    if (!lastMessage) {
      return;
    }

    const event = asRecord(lastMessage as unknown as JsonRecord);
    const eventType = readString(event, ["type"]);
    const eventData = asRecord(event.data);
    const eventTimestamp = toTimestampMillis(event.timestamp);

    if (eventType === "prediction") {
      const service = readString(eventData, ["service", "Service"]);
      if (service) {
        const prediction = readPredictionFromRecord(eventData);
        predictionsByServiceRef.current[service] = prediction;
        if (service === primaryServiceRef.current) {
          setTrafficSeries((previous) => {
            if (previous.length === 0) {
              return previous;
            }
            const next = [...previous];
            const last = next[next.length - 1];
            next[next.length - 1] = {
              ...last,
              predictedP50: prediction.p50,
              predictedP90: prediction.p90,
            };
            return next;
          });
        }
      }
      setLastUpdatedAt(eventTimestamp);
      return;
    }

    if (eventType === "metrics_update") {
      const service = readString(eventData, ["service", "Service"]);
      if (service === primaryServiceRef.current) {
        const actual = readNumber(eventData, ["rps", "RPS"], 0);
        const latestPrediction =
          predictionsByServiceRef.current[service] ?? { p50: actual, p90: actual };
        setTrafficSeries((previous) => {
          const point: TrafficPoint = {
            timestamp: eventTimestamp,
            timeLabel: buildTimeLabel(eventTimestamp),
            actual,
            predictedP50: latestPrediction.p50,
            predictedP90: Math.max(latestPrediction.p90, latestPrediction.p50),
          };
          return [...previous, point].slice(-MAX_TRAFFIC_POINTS);
        });
      }
      setLastUpdatedAt(eventTimestamp);
      return;
    }

    if (eventType === "scaling_decision") {
      const service = readString(eventData, ["service", "Service"]);
      const oldReplicas = readNumber(eventData, ["old_replicas", "OldReplicas"], 0);
      const newReplicas = readNumber(eventData, ["new_replicas", "NewReplicas"], oldReplicas);
      const executed = readBoolean(eventData, ["executed", "Executed"], false);
      const reason = readString(eventData, ["reason", "Reason"], "scaling decision");

      setReplicas((previous) =>
        previous.map((replica) =>
          replica.service === service
            ? { ...replica, replicas: newReplicas, lastScaledAt: eventTimestamp }
            : replica,
        ),
      );

      const action = deriveAction(oldReplicas, newReplicas, executed);
      setAuditLogs((previous) =>
        [
          {
            id: `event-${eventTimestamp}-${service}`,
            timestamp: eventTimestamp,
            service,
            previousReplicas: oldReplicas,
            newReplicas,
            confidence: 0,
            forecastP50: 0,
            forecastP90: 0,
            mode: "event",
            action,
            reason,
          },
          ...previous,
        ].slice(0, MAX_AUDIT_LOGS),
      );
      setLastUpdatedAt(eventTimestamp);
      return;
    }

    if (eventType === "mode_change") {
      const nextMode = normalizeMode(readString(eventData, ["new_mode", "NewMode"]));
      if (nextMode) {
        setModeState(nextMode);
      }
      setLastUpdatedAt(eventTimestamp);
      return;
    }

    if (eventType === "alert") {
      const message = readString(eventData, ["message", "Message"]).toLowerCase();
      if (message.includes("global kill switch enabled")) {
        setKillSwitchActive(true);
      }
      if (message.includes("global kill switch disabled")) {
        setKillSwitchActive(false);
      }
      setLastUpdatedAt(eventTimestamp);
    }
  }, [lastMessage]);

  const setMode = useCallback(
    (nextMode: OperatingMode) => {
      setModeState(nextMode);
      if (serviceNames.length === 0) {
        return;
      }
      void setAllServicesMode(serviceNames, nextMode).catch((error) => {
        console.error("Failed to update service mode", error);
        void loadInitialSnapshot();
      });
    },
    [loadInitialSnapshot, serviceNames],
  );

  const toggleKillSwitch = useCallback(() => {
    const nextState = !killSwitchRef.current;
    setKillSwitchActive(nextState);
    killSwitchRef.current = nextState;
    void setKillSwitch(nextState).catch((error) => {
      console.error("Failed to toggle kill switch", error);
      void loadInitialSnapshot();
    });
  }, [loadInitialSnapshot]);

  const recentAuditLogs = useMemo(() => auditLogs.slice(0, 5), [auditLogs]);

  return {
    isLoading,
    isRealtimeConnected: isConnected,
    mode,
    modeOptions: MODE_OPTIONS,
    killSwitchActive,
    trafficSeries,
    replicas,
    recentAuditLogs,
    lastUpdatedAt,
    setMode,
    toggleKillSwitch,
  };
}

