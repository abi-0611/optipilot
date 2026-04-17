export type OperatingMode = "shadow" | "recommend" | "autonomous";

export type ScalingAction = "scale_up" | "scale_down" | "hold" | "simulated";

export interface TrafficPoint {
  timestamp: number;
  timeLabel: string;
  actual: number;
  predictedP50: number;
  predictedP90: number;
}

export interface ReplicaState {
  service: string;
  replicas: number;
  minReplicas: number;
  maxReplicas: number;
  lastScaledAt?: number;
}

export interface AuditLogEntry {
  id: string;
  timestamp: number;
  service: string;
  previousReplicas: number;
  newReplicas: number;
  confidence: number;
  forecastP50: number;
  forecastP90: number;
  mode: string;
  action: ScalingAction;
  reason: string;
}
