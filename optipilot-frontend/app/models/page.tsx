"use client";

import { useState } from "react";
import Link from "next/link";
import { 
  useServices, 
  useServiceModel, 
  triggerServiceRetrain,
  simulateServicePrediction,
  type ServiceSummary,
  type ServiceModelStatus 
} from "../../lib/api";
import { Brain, RefreshCw, CheckCircle2, AlertCircle, ExternalLink, Play } from "lucide-react";

function ModelStatusCard({ service }: { service: ServiceSummary }) {
  const { data: modelStatus, error: modelError, isLoading, mutate: mutateModel } = useServiceModel(service.name);
  const [isRetraining, setIsRetraining] = useState(false);
  const [isSimulating, setIsSimulating] = useState(false);
  const [showSimulate, setShowSimulate] = useState(false);
  const [simInput, setSimInput] = useState("10, 15, 25, 40, 35");
  const [simResult, setSimResult] = useState<any>(null);
  const [feedback, setFeedback] = useState<{ message: string; type: "success" | "error" } | null>(null);

  const onRetrain = async () => {
    setIsRetraining(true);
    setFeedback(null);
    try {
      const result = await triggerServiceRetrain(service.name);
      setFeedback({ message: result.message, type: "success" });
      void mutateModel();
    } catch (error) {
      console.error("Failed to trigger retrain", error);
      setFeedback({ 
        message: `Failed to trigger retrain: ${error instanceof Error ? error.message : "unknown error"}`, 
        type: "error" 
      });
    } finally {
      setIsRetraining(false);
    }
  };

  const onSimulate = async () => {
    setIsSimulating(true);
    setFeedback(null);
    try {
      const rpsArray = simInput.split(",").map(v => parseFloat(v.trim())).filter(v => !isNaN(v));
      if (rpsArray.length === 0) throw new Error("Please enter valid RPS values");
      const result = await simulateServicePrediction(service.name, rpsArray);
      setSimResult(result);
    } catch (error) {
      setFeedback({ 
        message: `Simulation failed: ${error instanceof Error ? error.message : "unknown error"}`, 
        type: "error" 
      });
    } finally {
      setIsSimulating(false);
    }
  };

  const isNotFound = !isLoading && modelStatus === null;
  const mapePercent = modelStatus ? (modelStatus.CurrentMAPE * 100).toFixed(2) : "N/A";
  const accuracyColor = modelStatus 
    ? modelStatus.CurrentMAPE < 0.1 
      ? "text-emerald-400" 
      : modelStatus.CurrentMAPE < 0.25 
        ? "text-amber-400" 
        : "text-red-400"
    : "text-zinc-500";

  return (
    <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-6 flex flex-col h-full hover:border-zinc-700 transition-colors">
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-cyan-500/10 rounded-lg">
            <Brain className="w-5 h-5 text-cyan-400" />
          </div>
          <div>
            <h3 className="text-lg font-bold text-zinc-100 capitalize">{service.name}</h3>
            <div className="flex items-center gap-2">
              <p className="text-xs text-zinc-500">{service.namespace}</p>
              {modelStatus?.IsTraining && (
                <span className="flex items-center gap-1 text-[10px] text-amber-400 animate-pulse font-bold uppercase tracking-tight bg-amber-400/10 px-1.5 py-0.5 rounded border border-amber-400/20">
                  <RefreshCw className="w-2.5 h-2.5 animate-spin" />
                  Training
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => setShowSimulate(!showSimulate)}
            className={`p-1.5 rounded-md transition-colors ${showSimulate ? "text-amber-400 bg-amber-400/10" : "text-zinc-500 hover:text-amber-400 hover:bg-zinc-800"}`}
            title="Manual Inference Test"
          >
            <Play className="w-4 h-4" />
          </button>
          <Link 
            href={`/services/${service.name}`}
            className="p-1.5 text-zinc-500 hover:text-cyan-400 hover:bg-zinc-800 rounded-md transition-colors"
            title="View Service Details"
          >
            <ExternalLink className="w-4 h-4" />
          </Link>
        </div>
      </div>

      {!showSimulate ? (
        <>
          {modelStatus?.TrainingState === "failed" && (
            <div className="mb-4 p-2 bg-red-500/10 border border-red-500/20 rounded text-[10px] text-red-400 flex items-start gap-2">
              <AlertCircle className="w-3 h-3 mt-0.5 shrink-0" />
              <p>Training Failed: {modelStatus.TrainingMessage}</p>
            </div>
          )}
          <div className="grid grid-cols-2 gap-4 mb-6 flex-1">
            <div className="space-y-1">
              <p className="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Model Version</p>
              <p className="text-sm text-zinc-200 font-mono truncate">
                {isLoading ? "Loading..." : isNotFound ? "None" : modelStatus?.ModelVersion}
              </p>
            </div>
            <div className="space-y-1">
              <p className="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Accuracy (MAPE)</p>
              <p className={`text-sm font-bold ${accuracyColor}`}>
                {isLoading ? "---" : isNotFound ? "N/A" : `${mapePercent}%`}
              </p>
            </div>
            <div className="space-y-1">
              <p className="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Training Points</p>
              <p className="text-sm text-zinc-200">
                {isLoading ? "---" : isNotFound ? "0" : modelStatus?.TrainingDataPoints?.toLocaleString()}
              </p>
            </div>
            <div className="space-y-1">
              <p className="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Last Trained</p>
              <p className="text-sm text-zinc-200">
                {isLoading ? "---" : isNotFound ? "Never" : modelStatus?.LastTrainedAt 
                  ? new Date(modelStatus.LastTrainedAt).toLocaleDateString()
                  : "Never"}
              </p>
            </div>
          </div>
        </>
      ) : (
        <div className="mb-6 flex-1 space-y-4">
          <div className="space-y-1">
            <label className="text-[10px] uppercase tracking-wider text-amber-500 font-semibold">Input RPS History</label>
            <input 
              type="text"
              value={simInput}
              onChange={(e) => setSimInput(e.target.value)}
              placeholder="e.g. 10, 15, 20"
              className="w-full bg-zinc-950 border border-zinc-800 rounded px-2 py-1.5 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-amber-500"
            />
          </div>

          {simResult && (
            <div className="p-3 bg-zinc-950 border border-zinc-800 rounded space-y-2">
              <p className="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Simulation Result</p>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div>
                  <span className="text-zinc-500">p50:</span> <span className="text-cyan-400 font-mono">{simResult.rps_p50?.toFixed(1)}</span>
                </div>
                <div>
                  <span className="text-zinc-500">p90:</span> <span className="text-amber-400 font-mono">{simResult.rps_p90?.toFixed(1)}</span>
                </div>
                <div>
                  <span className="text-zinc-500">replicas:</span> <span className="text-emerald-400 font-mono">{simResult.recommended_replicas}</span>
                </div>
                <div>
                  <span className="text-zinc-500">conf:</span> <span className="text-zinc-300 font-mono">{(simResult.confidence_score * 100).toFixed(0)}%</span>
                </div>
              </div>
            </div>
          )}

          <button
            onClick={onSimulate}
            disabled={isSimulating}
            className="w-full py-1.5 bg-amber-600 hover:bg-amber-500 disabled:bg-zinc-800 rounded text-xs font-bold text-white transition-colors"
          >
            {isSimulating ? "Running..." : "Run Inference Test"}
          </button>
        </div>
      )}

      {feedback && (
        <div className={`mb-4 p-3 rounded-lg text-xs flex items-center gap-2 ${
          feedback.type === "success" ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/20" : "bg-red-500/10 text-red-400 border border-red-500/20"
        }`}>
          {feedback.type === "success" ? <CheckCircle2 className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
          {feedback.message}
        </div>
      )}

      {!showSimulate && (
        <button
          onClick={onRetrain}
          disabled={isRetraining || modelError}
          className="w-full py-2.5 px-4 bg-zinc-800 hover:bg-zinc-700 disabled:bg-zinc-800/50 disabled:text-zinc-600 rounded-lg text-sm font-semibold text-zinc-100 flex items-center justify-center gap-2 transition-all active:scale-[0.98]"
        >
          <RefreshCw className={`w-4 h-4 ${isRetraining ? "animate-spin" : ""}`} />
          {isRetraining ? "Requesting Retrain..." : "Retrain Model"}
        </button>
      )}
    </div>
  );
}

export default function ModelsPage() {
  const { data: services, error: servicesError, isLoading } = useServices();

  return (
    <div className="p-8 max-w-7xl mx-auto">
      <header className="mb-10">
        <h1 className="text-3xl font-bold text-zinc-100">Forecasting & ML Models</h1>
        <p className="text-zinc-400 mt-2 max-w-2xl">
          Monitor the health and performance of the predictive models for each service. 
          Models are automatically retrained periodically, but can be manually triggered if accuracy drifts.
        </p>
      </header>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-64 bg-zinc-900/50 border border-zinc-800 animate-pulse rounded-xl" />
          ))}
        </div>
      ) : servicesError ? (
        <div className="bg-red-900/20 border border-red-500/30 p-6 rounded-xl text-red-400 flex items-center gap-4">
          <AlertCircle className="w-6 h-6" />
          <div>
            <h2 className="font-bold text-lg">Failed to load services</h2>
            <p className="text-sm opacity-80">Please check your connection to the OptiPilot controller API.</p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {services?.map((service) => (
            <ModelStatusCard key={service.name} service={service} />
          ))}
          {services?.length === 0 && (
            <div className="col-span-full py-20 text-center bg-zinc-900/30 border border-dashed border-zinc-800 rounded-xl">
              <p className="text-zinc-500">No services found.</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
