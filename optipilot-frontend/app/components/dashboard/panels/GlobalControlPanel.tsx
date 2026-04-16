import { PanelShell } from "@/app/components/dashboard/PanelShell";
import type { OperatingMode } from "@/app/types/dashboard";

interface GlobalControlPanelProps {
  mode: OperatingMode;
  modeOptions: OperatingMode[];
  killSwitchActive: boolean;
  onModeChange: (mode: OperatingMode) => void;
  onKillSwitchToggle: () => void;
}

const modeLabel: Record<OperatingMode, string> = {
  shadow: "Shadow",
  recommend: "Recommend",
  autonomous: "Autonomous",
};

export function GlobalControlPanel({
  mode,
  modeOptions,
  killSwitchActive,
  onModeChange,
  onKillSwitchToggle,
}: GlobalControlPanelProps) {
  return (
    <PanelShell
      title="Global Control Panel"
      subtitle="System-level operational controls for predictive scaling"
    >
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={onKillSwitchToggle}
            className={`rounded-md px-5 py-2.5 text-sm font-semibold tracking-wide transition ${
              killSwitchActive
                ? "bg-red-500 text-red-50 hover:bg-red-400"
                : "bg-red-800/70 text-red-100 hover:bg-red-700/80"
            }`}
          >
            {killSwitchActive ? "Kill Switch: ON" : "Global Kill Switch"}
          </button>
          <span
            className={`rounded-md px-2 py-1 text-xs font-medium ${
              killSwitchActive
                ? "bg-red-900/70 text-red-200"
                : "bg-emerald-900/60 text-emerald-200"
            }`}
          >
            {killSwitchActive ? "Scaling paused" : "Scaling active"}
          </span>
        </div>

        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <span className="text-zinc-400">Global Mode</span>
          <select
            value={mode}
            onChange={(event) => onModeChange(event.target.value as OperatingMode)}
            className="rounded-md border border-zinc-700 bg-[#0f141b] px-3 py-2 text-sm text-zinc-100 outline-none ring-cyan-500 transition focus:ring-2"
          >
            {modeOptions.map((option) => (
              <option key={option} value={option}>
                {modeLabel[option]}
              </option>
            ))}
          </select>
        </label>
      </div>
    </PanelShell>
  );
}
