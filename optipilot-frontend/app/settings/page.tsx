"use client";

import { useSystemStatus, setGlobalMode } from "../../lib/api";
import { useWsEvents } from "../../lib/ws";

export default function SettingsPage() {
  const { data: status, error } = useSystemStatus();
  const { isConnected } = useWsEvents();

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-3xl font-bold mb-8">Settings & System Status</h1>
      
      <div className="grid gap-8">
        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6">
          <h2 className="text-xl font-semibold mb-4 text-red-400">Emergency Control</h2>
          <button className="px-6 py-3 bg-red-600 hover:bg-red-700 text-white font-bold rounded shadow-lg transition-transform active:scale-95">
            GLOBAL KILL SWITCH
          </button>
          <p className="mt-2 text-sm text-zinc-400">Immediately disables all autonomous scaling and reverts to shadow mode.</p>
        </section>

        <section className="bg-zinc-900 border border-zinc-800 rounded-lg p-6">
          <h2 className="text-xl font-semibold mb-4">Connection Status</h2>
          <ul className="space-y-4 text-sm">
            <li className="flex items-center gap-2">
              <span className={`w-3 h-3 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`}></span>
              WebSocket: {isConnected ? 'Connected' : 'Disconnected'}
            </li>
            <li className="flex items-center gap-2">
              <span className={`w-3 h-3 rounded-full ${status ? 'bg-green-500' : (error ? 'bg-red-500' : 'bg-yellow-500')}`}></span>
              Controller API: {status ? 'Connected' : (error ? 'Error' : 'Connecting...')}
            </li>
          </ul>
        </section>
      </div>
    </div>
  );
}