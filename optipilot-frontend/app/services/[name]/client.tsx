"use client";

import { useServiceMetrics, usePredictions } from "../../../lib/api";

export function ServiceClientView({ name }: { name: string }) {
  const { data: metrics, error: metricsError } = useServiceMetrics(name);
  const { data: predictions, error: predsError } = usePredictions(name);

  return (
    <div className="p-8 max-w-6xl mx-auto space-y-6">
      <header className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold capitalize text-cyan-400">{name}</h1>
          <p className="text-sm text-zinc-400 mt-1">Detailed service metrics and predictions</p>
        </div>
        <div className="flex items-center gap-4">
          <select className="bg-zinc-800 border-zinc-700 text-sm rounded p-2 focus:ring-2 focus:ring-cyan-500 outline-none text-white">
            <option>Shadow</option>
            <option>Recommend</option>
            <option>Autonomous</option>
          </select>
          <button className="bg-zinc-800 hover:bg-zinc-700 text-sm font-medium px-4 py-2 rounded transition-colors text-white">
            Retrain Model
          </button>
        </div>
      </header>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 flex flex-col items-center justify-center min-h-[300px] text-zinc-500">
          <h3>Traffic History (Last 24h)</h3>
          {metricsError ? <span className="text-red-400">Failed to load</span> : (metrics ? <span>Data loaded</span> : <span>Loading chart data...</span>)}
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6 flex flex-col items-center justify-center min-h-[300px] text-zinc-500">
          <h3>Predictions (p50 / p90 bands)</h3>
          {predsError ? <span className="text-red-400">Failed to load</span> : (predictions ? <span>Data loaded</span> : <span>Loading predictions...</span>)}
        </section>
      </div>
    </div>
  );
}