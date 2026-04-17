"use client";

import { GlobalControlPanel } from "@/app/components/dashboard/panels/GlobalControlPanel";
import { LiveTrafficPanel } from "@/app/components/dashboard/panels/LiveTrafficPanel";
import { RecentAuditLogsPanel } from "@/app/components/dashboard/panels/RecentAuditLogsPanel";
import { ReplicaStatusPanel } from "@/app/components/dashboard/panels/ReplicaStatusPanel";
import { useDashboardData } from "@/app/hooks/useDashboardData";
import type { OperatingMode } from "@/app/types/dashboard";

export function DashboardHome() {
  const {
    isLoading,
    isRealtimeConnected,
    mode,
    modeOptions,
    killSwitchActive,
    trafficSeries,
    replicas,
    recentAuditLogs,
    lastUpdatedAt,
    setMode,
    toggleKillSwitch,
  } = useDashboardData();

  const onModeChange = (value: OperatingMode) => {
    setMode(value);
  };

  return (
    <div className="min-h-screen bg-[#0b0f14] text-zinc-100">
      <main className="mx-auto w-full max-w-[1600px] p-4 md:p-6">
        <header className="mb-4 flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
          <div>
            <h1 className="text-xl font-semibold text-zinc-100 md:text-2xl">
              OptiPilot Predictive Autoscaling Dashboard
            </h1>
            <p className="text-sm text-zinc-400">
              Real-time monitoring and control for Kubernetes scaling modes.
            </p>
          </div>
          <div className="flex items-center gap-2 text-xs">
            <span
              className={`rounded px-2 py-1 ${
                isRealtimeConnected
                  ? "bg-emerald-900/60 text-emerald-200"
                  : "bg-red-900/60 text-red-200"
              }`}
            >
              WebSocket: {isRealtimeConnected ? "connected" : "disconnected"}
            </span>
            <span className="rounded bg-zinc-800 px-2 py-1 text-zinc-300">
              Last update{" "}
              {lastUpdatedAt > 0
                ? new Date(lastUpdatedAt).toLocaleTimeString()
                : "pending"}
            </span>
          </div>
        </header>

        <div className="grid grid-cols-1 gap-4 xl:grid-cols-12">
          <div className="xl:col-span-12">
            <GlobalControlPanel
              mode={mode}
              modeOptions={modeOptions}
              killSwitchActive={killSwitchActive}
              onModeChange={onModeChange}
              onKillSwitchToggle={toggleKillSwitch}
            />
          </div>

          <div className="xl:col-span-8">
            <LiveTrafficPanel trafficSeries={trafficSeries} />
          </div>

          <div className="xl:col-span-4">
            <ReplicaStatusPanel replicas={replicas} now={lastUpdatedAt} />
          </div>

          <div className="xl:col-span-12">
            <RecentAuditLogsPanel logs={recentAuditLogs} />
          </div>
        </div>

        {isLoading ? (
          <div className="mt-4 rounded-md border border-zinc-800 bg-[#12171f] p-3 text-sm text-zinc-400">
            Loading initial REST snapshot...
          </div>
        ) : null}
      </main>
    </div>
  );
}
