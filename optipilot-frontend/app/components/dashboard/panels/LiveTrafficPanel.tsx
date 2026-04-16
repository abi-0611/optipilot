import dynamic from "next/dynamic";
import { PanelShell } from "@/app/components/dashboard/PanelShell";
import type { TrafficPoint } from "@/app/types/dashboard";

interface LiveTrafficPanelProps {
  trafficSeries: TrafficPoint[];
}

const LiveTrafficChart = dynamic(
  () =>
    import("@/app/components/dashboard/panels/LiveTrafficChart").then(
      (module) => module.LiveTrafficChart,
    ),
  {
    ssr: false,
    loading: () => <div className="h-[300px] animate-pulse rounded bg-[#0f141b]" />,
  },
);

export function LiveTrafficPanel({ trafficSeries }: LiveTrafficPanelProps) {
  return (
    <PanelShell
      title="Live Traffic Panel"
      subtitle="Actual and forecasted requests-per-second (RPS) stream"
      className="h-[380px]"
    >
      <div className="h-[300px] w-full">
        <LiveTrafficChart trafficSeries={trafficSeries} />
      </div>
    </PanelShell>
  );
}
