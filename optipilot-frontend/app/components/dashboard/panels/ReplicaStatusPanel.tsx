import { PanelShell } from "@/app/components/dashboard/PanelShell";
import type { ReplicaState } from "@/app/types/dashboard";

interface ReplicaStatusPanelProps {
  replicas: ReplicaState[];
  now: number;
}

export function ReplicaStatusPanel({ replicas, now }: ReplicaStatusPanelProps) {
  return (
    <PanelShell
      title="Replica Status Panel"
      subtitle="Current pod counts across monitored services"
      className="h-[380px]"
    >
      <div className="space-y-4">
        {replicas.map((service) => {
          const fillPercent = (service.replicas / service.maxReplicas) * 100;
          const recentlyScaled =
            service.lastScaledAt !== undefined && now - service.lastScaledAt < 6500;

          return (
            <div
              key={service.service}
              className={`rounded-md border border-zinc-800 bg-[#0f141b] p-3 transition ${
                recentlyScaled ? "shadow-[0_0_0_1px_rgba(34,211,238,0.5)]" : ""
              }`}
            >
              <div className="mb-2 flex items-center justify-between">
                <span className="text-sm font-medium text-zinc-200">{service.service}</span>
                <span className="text-xs text-zinc-400">
                  {service.replicas} / {service.maxReplicas} replicas
                </span>
              </div>

              <div className="h-2 w-full overflow-hidden rounded-full bg-zinc-800">
                <div
                  className={`h-full rounded-full bg-cyan-500 transition-all duration-500 ${
                    recentlyScaled ? "animate-pulse" : ""
                  }`}
                  style={{ width: `${fillPercent}%` }}
                />
              </div>

              <div className="mt-3 flex flex-wrap gap-1.5">
                {Array.from({ length: service.maxReplicas }).map((_, index) => {
                  const isActive = index < service.replicas;
                  return (
                    <span
                      key={`${service.service}-pod-${index}`}
                      className={`h-2.5 w-2.5 rounded-full ${
                        isActive ? "bg-cyan-400" : "bg-zinc-700"
                      } ${recentlyScaled && isActive ? "animate-pulse" : ""}`}
                    />
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </PanelShell>
  );
}
