"use client";

import { useAuditLog } from "../../lib/api";

export default function AuditPage() {
  const { data: logs = [], error } = useAuditLog(200);

  return (
    <div className="p-8 max-w-6xl mx-auto">
      <h1 className="text-3xl font-bold mb-8">Scaling Audit Log</h1>
      {error && <div className="text-red-500 mb-4">Error loading audit logs.</div>}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden shrink-0">
        <table className="w-full text-left">
          <thead className="bg-zinc-950 border-b border-zinc-800 text-zinc-400 text-sm font-medium uppercase">
            <tr>
              <th className="p-4 py-3">Timestamp</th>
              <th className="p-4 py-3">Service</th>
              <th className="p-4 py-3">Action</th>
              <th className="p-4 py-3">Reason</th>
              <th className="p-4 py-3">Diff</th>
            </tr>
          </thead>
          <tbody className="text-sm divide-y divide-zinc-800">
            {logs.map((log) => {
              const diff = (log.NewReplicas ?? 0) - (log.OldReplicas ?? 0);
              const action =
                diff === 0
                  ? "hold"
                  : log.Executed
                    ? diff > 0
                      ? "scale_up"
                      : "scale_down"
                    : "simulated";
              return (
                <tr
                  key={log.ID}
                  className="hover:bg-zinc-800/50 transition-colors"
                >
                  <td className="p-4">
                    {new Date(log.CreatedAt).toLocaleString()}
                  </td>
                  <td className="p-4 text-cyan-400">{log.ServiceName}</td>
                  <td className="p-4">
                    <span className="px-2 py-0.5 rounded-full bg-blue-900/50 text-blue-200 text-xs font-semibold">
                      {action}
                    </span>
                  </td>
                  <td className="p-4 truncate max-w-[200px]">{log.Reason}</td>
                  <td className="p-4">{diff > 0 ? `+${diff}` : diff}</td>
                </tr>
              );
            })}
            {logs.length === 0 ? (
              <tr>
                <td colSpan={5} className="p-4 text-center text-zinc-500">
                  No logs found or loading...
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

