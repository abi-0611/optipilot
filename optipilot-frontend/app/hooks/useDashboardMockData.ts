"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  AuditLogEntry,
  OperatingMode,
  ReplicaState,
  TrafficPoint,
} from "@/app/types/dashboard";

interface ServiceConfig {
  name: string;
  minReplicas: number;
  maxReplicas: number;
  initialReplicas: number;
  trafficShare: number;
  targetRpsPerReplica: number;
}

const UPDATE_INTERVAL_MS = 3000;
const MAX_TRAFFIC_POINTS = 48;
const MAX_AUDIT_LOGS = 64;

const SERVICES: ServiceConfig[] = [
  {
    name: "auth-service",
    minReplicas: 2,
    maxReplicas: 12,
    initialReplicas: 4,
    trafficShare: 0.28,
    targetRpsPerReplica: 16,
  },
  {
    name: "payment-gateway",
    minReplicas: 2,
    maxReplicas: 14,
    initialReplicas: 5,
    trafficShare: 0.34,
    targetRpsPerReplica: 18,
  },
  {
    name: "frontend-ui",
    minReplicas: 3,
    maxReplicas: 18,
    initialReplicas: 6,
    trafficShare: 0.38,
    targetRpsPerReplica: 20,
  },
];

const MODE_OPTIONS: OperatingMode[] = ["shadow", "recommend", "autonomous"];

const clamp = (value: number, min: number, max: number) =>
  Math.max(min, Math.min(max, value));

const randomBetween = (min: number, max: number) =>
  Math.random() * (max - min) + min;

const buildTimeLabel = (timestamp: number) =>
  new Date(timestamp).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });

const buildTrafficPoint = (timestamp: number, previousActual: number) => {
  const waveOne = Math.sin(timestamp / 120_000) * 32;
  const waveTwo = Math.cos(timestamp / 190_000) * 18;
  const noise = randomBetween(-8, 8);
  const driftTowardWave = previousActual * 0.72 + (170 + waveOne + waveTwo) * 0.28;
  const actual = clamp(driftTowardWave + noise, 60, 320);
  const predictedP50 = clamp(actual * randomBetween(0.96, 1.04), 55, 340);
  const predictedP90 = clamp(
    predictedP50 + randomBetween(10, 30),
    predictedP50 + 4,
    360,
  );

  return {
    timestamp,
    timeLabel: buildTimeLabel(timestamp),
    actual: Number(actual.toFixed(2)),
    predictedP50: Number(predictedP50.toFixed(2)),
    predictedP90: Number(predictedP90.toFixed(2)),
  } satisfies TrafficPoint;
};

export function useDashboardMockData() {
  const [isLoading, setIsLoading] = useState(true);
  const [mode, setMode] = useState<OperatingMode>("shadow");
  const [killSwitchActive, setKillSwitchActive] = useState(false);
  const [trafficSeries, setTrafficSeries] = useState<TrafficPoint[]>([]);
  const [replicas, setReplicas] = useState<ReplicaState[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>([]);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<number>(0);

  const modeRef = useRef(mode);
  const killSwitchRef = useRef(killSwitchActive);
  const trafficRef = useRef<TrafficPoint[]>([]);
  const replicaRef = useRef<ReplicaState[]>([]);

  useEffect(() => {
    modeRef.current = mode;
  }, [mode]);

  useEffect(() => {
    killSwitchRef.current = killSwitchActive;
  }, [killSwitchActive]);

  const pushTrafficPoint = useCallback((point: TrafficPoint) => {
    setTrafficSeries((prev) => {
      const next = [...prev, point].slice(-MAX_TRAFFIC_POINTS);
      trafficRef.current = next;
      return next;
    });
  }, []);

  const pushAuditEntry = useCallback((entry: AuditLogEntry) => {
    setAuditLogs((prev) => [entry, ...prev].slice(0, MAX_AUDIT_LOGS));
  }, []);

  const evaluateReplicaDecision = useCallback(
    (forecastPoint: TrafficPoint, timestamp: number) => {
      setReplicas((prevReplicas) => {
        const selectedServiceIndex = Math.floor(Math.random() * SERVICES.length);
        const service = SERVICES[selectedServiceIndex];
        const current = prevReplicas.find((item) => item.service === service.name);
        if (!current) {
          return prevReplicas;
        }

        const serviceTraffic =
          forecastPoint.predictedP90 *
          service.trafficShare *
          randomBetween(0.92, 1.08);

        const desiredReplicas = clamp(
          Math.round(serviceTraffic / service.targetRpsPerReplica),
          service.minReplicas,
          service.maxReplicas,
        );

        const modeNow = modeRef.current;
        const direction =
          desiredReplicas > current.replicas
            ? "scale_up"
            : desiredReplicas < current.replicas
              ? "scale_down"
              : "hold";

        const canApplyInAutonomous =
          modeNow === "autonomous" && !killSwitchRef.current;
        const canApplyInRecommend =
          modeNow === "recommend" && !killSwitchRef.current && Math.random() > 0.4;
        const shouldApply =
          direction !== "hold" && (canApplyInAutonomous || canApplyInRecommend);

        const nextReplicas = shouldApply ? desiredReplicas : current.replicas;
        const action =
          direction === "hold"
            ? "hold"
            : shouldApply
              ? direction
              : "simulated";

        if (direction !== "hold" || killSwitchRef.current) {
          const reason = killSwitchRef.current
            ? "Global kill switch active. Decision simulated only."
            : modeNow === "shadow"
              ? "Shadow mode. Recommendation logged only."
              : modeNow === "recommend" && !shouldApply
                ? "Recommend mode. Awaiting operator approval."
                : "Scaling action executed from predictive signal.";

          pushAuditEntry({
            id: `${timestamp}-${service.name}`,
            timestamp,
            service: service.name,
            previousReplicas: current.replicas,
            newReplicas: desiredReplicas,
            confidence: Number(randomBetween(0.72, 0.98).toFixed(2)),
            forecastP50: forecastPoint.predictedP50,
            forecastP90: forecastPoint.predictedP90,
            mode: modeNow,
            action,
            reason,
          });
        }

        const next = prevReplicas.map((item) =>
          item.service === service.name
            ? {
                ...item,
                replicas: nextReplicas,
                lastScaledAt: shouldApply ? timestamp : item.lastScaledAt,
              }
            : item,
        );

        replicaRef.current = next;
        return next;
      });
    },
    [pushAuditEntry],
  );

  const emitRealtimeUpdate = useCallback(() => {
    const timestamp = Date.now();
    const previous = trafficRef.current[trafficRef.current.length - 1];
    const previousActual = previous?.actual ?? randomBetween(130, 190);
    const point = buildTrafficPoint(timestamp, previousActual);
    pushTrafficPoint(point);
    evaluateReplicaDecision(point, timestamp);
    setLastUpdatedAt(timestamp);
  }, [evaluateReplicaDecision, pushTrafficPoint]);

  useEffect(() => {
    let active = true;
    let intervalId: number | undefined;

    const initializeFromMockRest = async () => {
      await new Promise((resolve) => setTimeout(resolve, 550));
      if (!active) {
        return;
      }

      const now = Date.now();
      const initialReplicas = SERVICES.map<ReplicaState>((service) => ({
        service: service.name,
        replicas: service.initialReplicas,
        minReplicas: service.minReplicas,
        maxReplicas: service.maxReplicas,
      }));

      const seedTraffic: TrafficPoint[] = [];
      let previousActual = randomBetween(120, 180);
      for (let i = MAX_TRAFFIC_POINTS - 1; i >= 0; i -= 1) {
        const timestamp = now - i * UPDATE_INTERVAL_MS;
        const point = buildTrafficPoint(timestamp, previousActual);
        previousActual = point.actual;
        seedTraffic.push(point);
      }

      setReplicas(initialReplicas);
      replicaRef.current = initialReplicas;
      setTrafficSeries(seedTraffic);
      trafficRef.current = seedTraffic;
      setLastUpdatedAt(now);

      pushAuditEntry({
        id: `${now}-bootstrap`,
        timestamp: now,
        service: "frontend-ui",
        previousReplicas: 5,
        newReplicas: 6,
        confidence: 0.91,
        forecastP50: seedTraffic[seedTraffic.length - 1].predictedP50,
        forecastP90: seedTraffic[seedTraffic.length - 1].predictedP90,
        mode: "shadow",
        action: "simulated",
        reason: "Initial REST snapshot loaded. Streaming mode enabled.",
      });

      setIsLoading(false);

      intervalId = window.setInterval(() => {
        emitRealtimeUpdate();
      }, UPDATE_INTERVAL_MS);
    };

    void initializeFromMockRest();

    return () => {
      active = false;
      if (intervalId) {
        window.clearInterval(intervalId);
      }
    };
  }, [emitRealtimeUpdate, pushAuditEntry]);

  const recentAuditLogs = useMemo(() => auditLogs.slice(0, 5), [auditLogs]);

  return {
    isLoading,
    mode,
    modeOptions: MODE_OPTIONS,
    killSwitchActive,
    trafficSeries,
    replicas,
    recentAuditLogs,
    lastUpdatedAt,
    setMode,
    toggleKillSwitch: () => setKillSwitchActive((prev) => !prev),
  };
}
