"use client";

import { useMemo, useState } from "react";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  setServiceMode,
  triggerServiceRetrain,
  usePredictions,
  useServiceDecisions,
  useServiceMetrics,
  useServiceModel,
  useServices,
} from "../../../lib/api";
import type { OperatingMode } from "@/app/types/dashboard";

const MODE_OPTIONS: OperatingMode[] = ["shadow", "recommend", "autonomous"];
const MAX_DECISIONS = 20;

function normalizeMode(raw: string | undefined): OperatingMode {
  if (raw === "shadow" || raw === "recommend" || raw === "autonomous") {
    return raw;
  }
  return "shadow";
}

type JsonRecord = Record<string, unknown>;

interface TrafficChartPoint {
  timeLabel: string;
  actualRps: number;
  predictedP50: number;
  predictedP90: number;
}

interface PredictionChartPoint {
  timeLabel: string;
  p50: number;
  p90: number;
  replicas: number;
}

function asRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" ? (value as JsonRecord) : {};
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

function readTimestamp(source: JsonRecord, keys: string[]): number {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" || typeof value === "number") {
      const parsed = Date.parse(String(value));
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return Date.now();
}

function buildTimeLabel(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function buildTrafficSeries(
  metrics: JsonRecord[],
  predictions: JsonRecord[],
): TrafficChartPoint[] {
  const sortedMetrics = [...metrics].sort(
    (a, b) =>
      readTimestamp(a, ["CollectedAt", "collected_at", "Timestamp", "timestamp"]) -
      readTimestamp(b, ["CollectedAt", "collected_at", "Timestamp", "timestamp"]),
  );
  const sortedPredictions = [...predictions].sort(
    (a, b) =>
      readTimestamp(a, ["timestamp", "Timestamp"]) -
      readTimestamp(b, ["timestamp", "Timestamp"]),
  );

  let predictionIndex = 0;
  let latestPrediction: { p50: number; p90: number } | null = null;

  return sortedMetrics.map((metric) => {
    const timestamp = readTimestamp(metric, [
      "CollectedAt",
      "collected_at",
      "Timestamp",
      "timestamp",
    ]);

    while (predictionIndex < sortedPredictions.length) {
      const candidate = sortedPredictions[predictionIndex];
      if (readTimestamp(candidate, ["timestamp", "Timestamp"]) > timestamp) {
        break;
      }
      const p50 = readNumber(candidate, ["rps_p50", "RpsP50", "p50", "P50"], 0);
      const p90 = Math.max(
        p50,
        readNumber(candidate, ["rps_p90", "RpsP90", "p90", "P90"], p50),
      );
      latestPrediction = { p50, p90 };
      predictionIndex += 1;
    }

    const actual = readNumber(metric, ["RPS", "rps"], 0);
    return {
      timeLabel: buildTimeLabel(timestamp),
      actualRps: actual,
      predictedP50: latestPrediction?.p50 ?? actual,
      predictedP90: latestPrediction?.p90 ?? actual,
    };
  });
}

function buildPredictionSeries(predictions: JsonRecord[]): PredictionChartPoint[] {
  return [...predictions]
    .sort(
      (a, b) =>
        readTimestamp(a, ["timestamp", "Timestamp"]) -
        readTimestamp(b, ["timestamp", "Timestamp"]),
    )
    .map((prediction) => {
      const p50 = readNumber(prediction, ["rps_p50", "RpsP50", "p50", "P50"], 0);
      const p90 = Math.max(
        p50,
        readNumber(prediction, ["rps_p90", "RpsP90", "p90", "P90"], p50),
      );
      return {
        timeLabel: buildTimeLabel(readTimestamp(prediction, ["timestamp", "Timestamp"])),
        p50,
        p90,
        replicas: readNumber(
          prediction,
          ["recommended_replicas", "RecommendedReplicas", "replicas", "Replicas"],
          0,
        ),
      };
    });
}

export function ServiceClientView({ name }: { name: string }) {
  const { data: metrics, error: metricsError } = useServiceMetrics(name);
  const { data: predictions, error: predsError } = usePredictions(name);
  const { data: decisions, error: decisionsError } = useServiceDecisions(
    name,
    MAX_DECISIONS,
  );
  const { data: modelStatus, error: modelError } = useServiceModel(name);
  const { data: services, mutate: mutateServices } = useServices();

  const service = services?.find((item) => item.name === name);
  const serviceMode = normalizeMode(service?.mode);
  const [pendingMode, setPendingMode] = useState<OperatingMode | null>(null);
  const [isSavingMode, setIsSavingMode] = useState(false);
  const [isRetraining, setIsRetraining] = useState(false);
  const [feedbackMessage, setFeedbackMessage] = useState("");
  const mode = pendingMode ?? serviceMode;

  const trafficSeries = useMemo(() => {
    if (!metrics || !predictions) {
      return [];
    }
    return buildTrafficSeries(metrics.map(asRecord), predictions.map(asRecord));
  }, [metrics, predictions]);

  const predictionSeries = useMemo(() => {
    if (!predictions) {
      return [];
    }
    return buildPredictionSeries(predictions.map(asRecord));
  }, [predictions]);

  const onModeChange = async (nextMode: OperatingMode) => {
    setPendingMode(nextMode);
    setIsSavingMode(true);
    setFeedbackMessage("");
    try {
      await setServiceMode(name, nextMode, "service-page");
      await mutateServices();
    } catch (error) {
      console.error("Failed to update service mode", error);
      setFeedbackMessage(
        `Failed to update mode: ${error instanceof Error ? error.message : "unknown error"}`,
      );
    } finally {
      setPendingMode(null);
      setIsSavingMode(false);
    }
  };

  const onRetrain = async () => {
    setIsRetraining(true);
    setFeedbackMessage("");
    try {
      const result = await triggerServiceRetrain(name);
      setFeedbackMessage(result.message);
    } catch (error) {
      console.error("Failed to trigger retrain", error);
      setFeedbackMessage(
        `Failed to trigger retrain: ${error instanceof Error ? error.message : "unknown error"}`,
      );
    } finally {
      setIsRetraining(false);
    }
  };

  return (
    <div className="p-8 max-w-6xl mx-auto space-y-6">
      <header className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold capitalize text-cyan-400">{name}</h1>
          <p className="text-sm text-zinc-400 mt-1">
            Detailed service metrics, prediction bands, and scaling audit state
          </p>
        </div>
        <div className="flex items-center gap-4">
          <select
            value={mode}
            disabled={isSavingMode}
            onChange={(event) => {
              void onModeChange(event.target.value as OperatingMode);
            }}
            className="bg-zinc-800 border-zinc-700 text-sm rounded p-2 focus:ring-2 focus:ring-cyan-500 outline-none text-white"
          >
            {MODE_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={onRetrain}
            disabled={isRetraining}
            className="bg-zinc-800 hover:bg-zinc-700 text-sm font-medium px-4 py-2 rounded transition-colors text-white disabled:bg-zinc-700"
          >
            {isRetraining ? "Retraining..." : "Retrain Model"}
          </button>
        </div>
      </header>

      {feedbackMessage ? (
        <p className="text-sm text-zinc-300 bg-zinc-900 border border-zinc-800 rounded p-3">
          {feedbackMessage}
        </p>
      ) : null}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 min-h-[320px]">
          <h3 className="mb-3 text-sm font-semibold text-zinc-200">
            Traffic History (actual vs predicted)
          </h3>
          {metricsError ? (
            <span className="text-red-400 text-sm">Failed to load metrics</span>
          ) : trafficSeries.length > 0 ? (
            <div className="h-[260px] w-full">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={trafficSeries}>
                  <CartesianGrid stroke="#263040" strokeDasharray="3 3" />
                  <XAxis
                    dataKey="timeLabel"
                    minTickGap={18}
                    tick={{ fill: "#9ca3af", fontSize: 12 }}
                    axisLine={{ stroke: "#374151" }}
                    tickLine={{ stroke: "#374151" }}
                  />
                  <YAxis
                    tick={{ fill: "#9ca3af", fontSize: 12 }}
                    axisLine={{ stroke: "#374151" }}
                    tickLine={{ stroke: "#374151" }}
                    width={52}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#111827",
                      borderColor: "#374151",
                      color: "#e5e7eb",
                    }}
                    labelStyle={{ color: "#cbd5e1" }}
                    formatter={(value) => `${Number(value).toFixed(1)} RPS`}
                  />
                  <Legend wrapperStyle={{ color: "#cbd5e1", fontSize: 12 }} />
                  <Line
                    type="monotone"
                    dataKey="actualRps"
                    name="Actual"
                    stroke="#38bdf8"
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                  />
                  <Line
                    type="monotone"
                    dataKey="predictedP50"
                    name="Predicted p50"
                    stroke="#a78bfa"
                    strokeWidth={2}
                    strokeDasharray="6 4"
                    dot={false}
                    isAnimationActive={false}
                  />
                  <Line
                    type="monotone"
                    dataKey="predictedP90"
                    name="Predicted p90"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    strokeDasharray="3 3"
                    dot={false}
                    isAnimationActive={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <span className="text-zinc-500 text-sm">Loading chart data...</span>
          )}
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 min-h-[320px]">
          <h3 className="mb-3 text-sm font-semibold text-zinc-200">
            Prediction Trend (p50 / p90 + replicas)
          </h3>
          {predsError ? (
            <span className="text-red-400 text-sm">Failed to load predictions</span>
          ) : predictionSeries.length > 0 ? (
            <div className="h-[260px] w-full">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={predictionSeries}>
                  <CartesianGrid stroke="#263040" strokeDasharray="3 3" />
                  <XAxis
                    dataKey="timeLabel"
                    minTickGap={18}
                    tick={{ fill: "#9ca3af", fontSize: 12 }}
                    axisLine={{ stroke: "#374151" }}
                    tickLine={{ stroke: "#374151" }}
                  />
                  <YAxis
                    yAxisId="rps"
                    tick={{ fill: "#9ca3af", fontSize: 12 }}
                    axisLine={{ stroke: "#374151" }}
                    tickLine={{ stroke: "#374151" }}
                    width={48}
                  />
                  <YAxis
                    yAxisId="replicas"
                    orientation="right"
                    allowDecimals={false}
                    tick={{ fill: "#9ca3af", fontSize: 12 }}
                    axisLine={{ stroke: "#374151" }}
                    tickLine={{ stroke: "#374151" }}
                    width={40}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#111827",
                      borderColor: "#374151",
                      color: "#e5e7eb",
                    }}
                    labelStyle={{ color: "#cbd5e1" }}
                  />
                  <Legend wrapperStyle={{ color: "#cbd5e1", fontSize: 12 }} />
                  <Line
                    yAxisId="rps"
                    type="monotone"
                    dataKey="p50"
                    name="p50 RPS"
                    stroke="#a78bfa"
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                  />
                  <Line
                    yAxisId="rps"
                    type="monotone"
                    dataKey="p90"
                    name="p90 RPS"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                  />
                  <Line
                    yAxisId="replicas"
                    type="monotone"
                    dataKey="replicas"
                    name="Recommended replicas"
                    stroke="#34d399"
                    strokeWidth={2}
                    strokeDasharray="4 4"
                    dot={false}
                    isAnimationActive={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <span className="text-zinc-500 text-sm">Loading predictions...</span>
          )}
        </section>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 xl:col-span-1 space-y-3">
          <h3 className="text-sm font-semibold text-zinc-200">Service Snapshot</h3>
          <p className="text-sm text-zinc-400">
            Namespace: <span className="text-zinc-200">{service?.namespace ?? "unknown"}</span>
          </p>
          <p className="text-sm text-zinc-400">
            Replicas:{" "}
            <span className="text-zinc-200">
              {service?.current_replicas ?? 0} ({service?.min_replicas ?? 0}-
              {service?.max_replicas ?? 0})
            </span>
          </p>
          <p className="text-sm text-zinc-400">
            Paused: <span className="text-zinc-200">{service?.paused ? "yes" : "no"}</span>
          </p>
          <p className="text-sm text-zinc-400">
            Model:{" "}
            <span className="text-zinc-200">{modelStatus?.ModelVersion ?? "not available"}</span>
          </p>
          <p className="text-sm text-zinc-400">
            MAPE:{" "}
            <span className="text-zinc-200">
              {modelStatus ? `${(modelStatus.CurrentMAPE * 100).toFixed(1)}%` : "n/a"}
            </span>
          </p>
          {modelError ? <p className="text-xs text-red-400">Model status unavailable</p> : null}
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 xl:col-span-2">
          <h3 className="mb-3 text-sm font-semibold text-zinc-200">
            Recent Scaling Decisions
          </h3>
          {decisionsError ? (
            <p className="text-sm text-red-400">Failed to load scaling decisions</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-zinc-400 border-b border-zinc-800">
                  <tr>
                    <th className="py-2 pr-4">Time</th>
                    <th className="py-2 pr-4">Mode</th>
                    <th className="py-2 pr-4">Change</th>
                    <th className="py-2 pr-4">Confidence</th>
                    <th className="py-2 pr-4">Status</th>
                    <th className="py-2">Reason</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-800">
                  {(decisions ?? []).map((decision) => (
                    <tr key={decision.ID}>
                      <td className="py-2 pr-4 text-zinc-300">
                        {new Date(decision.CreatedAt).toLocaleTimeString()}
                      </td>
                      <td className="py-2 pr-4 text-zinc-300">{decision.ScalingMode}</td>
                      <td className="py-2 pr-4 text-zinc-300">
                        {decision.OldReplicas} → {decision.NewReplicas}
                      </td>
                      <td className="py-2 pr-4 text-zinc-300">
                        {(Number(decision.ConfidenceScore ?? 0) * 100).toFixed(0)}%
                      </td>
                      <td className="py-2 pr-4">
                        <span
                          className={`rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${
                            decision.Executed
                              ? "bg-emerald-900/60 text-emerald-200"
                              : "bg-zinc-700 text-zinc-100"
                          }`}
                        >
                          {decision.Executed ? "executed" : "simulated"}
                        </span>
                      </td>
                      <td className="py-2 text-zinc-400">{decision.Reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {(decisions ?? []).length === 0 ? (
                <p className="text-sm text-zinc-500 mt-3">No scaling decisions yet.</p>
              ) : null}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
