import { PanelShell } from "@/app/components/dashboard/PanelShell";
import type { AuditLogEntry } from "@/app/types/dashboard";

interface RecentAuditLogsPanelProps {
  logs: AuditLogEntry[];
}

const ACTION_CLASSES: Record<AuditLogEntry["action"], string> = {
  scale_up: "bg-emerald-900/60 text-emerald-200",
  scale_down: "bg-amber-900/60 text-amber-200",
  simulated: "bg-zinc-700 text-zinc-100",
  hold: "bg-sky-900/60 text-sky-200",
};

export function RecentAuditLogsPanel({ logs }: RecentAuditLogsPanelProps) {
  return (
    <PanelShell
      title="Recent Audit Logs"
      subtitle="Last 5 scaling decisions with execution metadata"
      className="h-[340px]"
    >
      <div className="h-[250px] overflow-y-auto pr-1">
        <div className="space-y-3">
          {logs.map((log) => (
            <article
              key={log.id}
              className="rounded-md border border-zinc-800 bg-[#0f141b] p-3"
            >
              <div className="mb-1 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold text-zinc-200">{log.service}</span>
                  <span
                    className={`rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${ACTION_CLASSES[log.action]}`}
                  >
                    {log.action.replace("_", " ")}
                  </span>
                </div>
                <time className="text-xs text-zinc-500">
                  {new Date(log.timestamp).toLocaleTimeString()}
                </time>
              </div>

              <p className="text-xs text-zinc-400">{log.reason}</p>

              <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-zinc-300 md:grid-cols-5">
                <span>
                  replicas: {log.previousReplicas} → {log.newReplicas}
                </span>
                <span>confidence: {(log.confidence * 100).toFixed(0)}%</span>
                <span>mode: {log.mode}</span>
                <span>p50: {log.forecastP50.toFixed(1)} RPS</span>
                <span>p90: {log.forecastP90.toFixed(1)} RPS</span>
              </div>
            </article>
          ))}
          {logs.length === 0 ? (
            <p className="text-sm text-zinc-500">No scaling decisions yet.</p>
          ) : null}
        </div>
      </div>
    </PanelShell>
  );
}
