"use client";

import { useState } from "react";
import { setKillSwitch, useSystemStatus } from "../../lib/api";
import { useWsEvents } from "../../lib/ws";

export default function SettingsPage() {
  const { data: status, error, mutate } = useSystemStatus();
  const { isConnected } = useWsEvents();
  const [isUpdatingKillSwitch, setIsUpdatingKillSwitch] = useState(false);

  const killSwitchEnabled = Boolean(status?.controller?.global_kill_switch);

  const onToggleKillSwitch = async () => {
    setIsUpdatingKillSwitch(true);
    try {
      await setKillSwitch(!killSwitchEnabled);
      await mutate();
    } finally {
      setIsUpdatingKillSwitch(false);
    }
  };

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-3xl font-bold mb-8">Settings &amp; System Status</h1>

      <div className="grid gap-8">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6">
          <h2 className="text-xl font-semibold mb-4 text-red-400">Emergency Control</h2>
          <button
            type="button"
            disabled={isUpdatingKillSwitch}
            onClick={onToggleKillSwitch}
            className="px-6 py-3 bg-red-600 hover:bg-red-700 disabled:bg-zinc-700 text-white font-bold rounded shadow-lg transition-transform active:scale-95"
          >
            {killSwitchEnabled ? "DISABLE KILL SWITCH" : "ENABLE KILL SWITCH"}
          </button>
          <p className="mt-2 text-sm text-zinc-400">
            Immediately disables autonomous scaling when enabled.
          </p>
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6">
          <h2 className="text-xl font-semibold mb-4">Connection Status</h2>
          <ul className="space-y-4 text-sm">
            <li className="flex items-center gap-2">
              <span
                className={`w-3 h-3 rounded-full ${isConnected ? "bg-green-500" : "bg-red-500"}`}
              />
              WebSocket: {isConnected ? "Connected" : "Disconnected"}
            </li>
            <li className="flex items-center gap-2">
              <span
                className={`w-3 h-3 rounded-full ${status ? "bg-green-500" : error ? "bg-red-500" : "bg-yellow-500"}`}
              />
              Controller API: {status ? "Connected" : error ? "Error" : "Connecting..."}
            </li>
            <li className="text-zinc-400">
              Forecaster:{" "}
              {status?.forecaster?.connected
                ? "Connected"
                : status?.forecaster?.error || "Unavailable"}
            </li>
            <li className="text-zinc-400">
              Prometheus:{" "}
              {status?.prometheus?.connected
                ? "Connected"
                : status?.prometheus?.error || "Unavailable"}
            </li>
          </ul>
        </section>
      </div>
    </div>
  );
}

