"use client";

import { useEffect, useState } from "react";
import {
  setServiceMode,
  triggerServiceRetrain,
  usePredictions,
  useServiceMetrics,
  useServices,
} from "../../../lib/api";
import type { OperatingMode } from "@/app/types/dashboard";

const MODE_OPTIONS: OperatingMode[] = ["shadow", "recommend", "autonomous"];

function normalizeMode(raw: string | undefined): OperatingMode {
  if (raw === "shadow" || raw === "recommend" || raw === "autonomous") {
    return raw;
  }
  return "shadow";
}

export function ServiceClientView({ name }: { name: string }) {
  const { data: metrics, error: metricsError } = useServiceMetrics(name);
  const { data: predictions, error: predsError } = usePredictions(name);
  const { data: services } = useServices();

  const service = services?.find((item) => item.name === name);
  const [mode, setMode] = useState<OperatingMode>("shadow");
  const [isSavingMode, setIsSavingMode] = useState(false);
  const [isRetraining, setIsRetraining] = useState(false);
  const [retrainMessage, setRetrainMessage] = useState("");

  useEffect(() => {
    setMode(normalizeMode(service?.mode));
  }, [service?.mode]);

  const onModeChange = async (nextMode: OperatingMode) => {
    setMode(nextMode);
    setIsSavingMode(true);
    try {
      await setServiceMode(name, nextMode, "service-page");
    } finally {
      setIsSavingMode(false);
    }
  };

  const onRetrain = async () => {
    setIsRetraining(true);
    setRetrainMessage("");
    try {
      const result = await triggerServiceRetrain(name);
      setRetrainMessage(result.message);
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
            Detailed service metrics and predictions
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

      {retrainMessage ? (
        <p className="text-sm text-zinc-300 bg-zinc-900 border border-zinc-800 rounded p-3">
          {retrainMessage}
        </p>
      ) : null}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 flex flex-col items-center justify-center min-h-[300px] text-zinc-500">
          <h3>Traffic History (Last 24h)</h3>
          {metricsError ? (
            <span className="text-red-400">Failed to load</span>
          ) : metrics ? (
            <span>{metrics.length} points loaded</span>
          ) : (
            <span>Loading chart data...</span>
          )}
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 flex flex-col items-center justify-center min-h-[300px] text-zinc-500">
          <h3>Predictions (p50 / p90 bands)</h3>
          {predsError ? (
            <span className="text-red-400">Failed to load</span>
          ) : predictions ? (
            <span>{predictions.length} predictions loaded</span>
          ) : (
            <span>Loading predictions...</span>
          )}
        </section>
      </div>
    </div>
  );
}

