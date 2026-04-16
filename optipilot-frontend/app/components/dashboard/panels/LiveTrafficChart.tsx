import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { TrafficPoint } from "@/app/types/dashboard";

interface LiveTrafficChartProps {
  trafficSeries: TrafficPoint[];
}

export function LiveTrafficChart({ trafficSeries }: LiveTrafficChartProps) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={trafficSeries} margin={{ top: 8, right: 20, left: 0, bottom: 0 }}>
        <CartesianGrid stroke="#263040" strokeDasharray="3 3" />
        <XAxis
          dataKey="timeLabel"
          minTickGap={18}
          tick={{ fill: "#9ca3af", fontSize: 12 }}
          axisLine={{ stroke: "#374151" }}
          tickLine={{ stroke: "#374151" }}
        />
        <YAxis
          tick={{ fill: "#9ca3af", fontSize: 12 }}
          axisLine={{ stroke: "#374151" }}
          tickLine={{ stroke: "#374151" }}
          width={48}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: "#111827",
            borderColor: "#374151",
            color: "#e5e7eb",
          }}
          labelStyle={{ color: "#cbd5e1" }}
          formatter={(value) => `${Number(value).toFixed(1)} RPS`}
        />
        <Legend wrapperStyle={{ color: "#cbd5e1", fontSize: 12 }} />
        <Line
          type="monotone"
          dataKey="actual"
          name="Actual Traffic"
          stroke="#38bdf8"
          strokeWidth={2}
          dot={false}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="predictedP50"
          name="Predicted p50"
          stroke="#a78bfa"
          strokeWidth={2}
          strokeDasharray="6 4"
          dot={false}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="predictedP90"
          name="Predicted p90"
          stroke="#f59e0b"
          strokeWidth={2}
          strokeDasharray="3 3"
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}
