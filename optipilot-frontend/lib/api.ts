import useSWR from "swr";
import type { OperatingMode } from "@/app/types/dashboard";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

type JsonRecord = Record<string, unknown>;

export interface ServiceSummary {
  name: string;
  namespace: string;
  min_replicas: number;
  max_replicas: number;
  current_replicas: number;
  mode: string;
  paused: boolean;
  last_metrics?: JsonRecord;
  last_prediction?: JsonRecord;
  model_status?: JsonRecord;
}

export interface ControllerDecision {
  ID: number;
  ServiceName: string;
  OldReplicas: number;
  NewReplicas: number;
  ScalingMode: string;
  ModelVersion: string;
  Reason: string;
  RpsP50: number;
  RpsP90: number;
  ConfidenceScore: number;
  Executed: boolean;
  CreatedAt: string;
}

export interface SystemStatus {
  controller?: {
    healthy?: boolean;
    uptime_sec?: number;
    metrics_row_count?: number;
    global_kill_switch?: boolean;
  };
  forecaster?: {
    connected?: boolean;
    error?: string;
  };
  prometheus?: {
    connected?: boolean;
    error?: string;
  };
  websocket?: {
    connections?: number;
  };
}

export interface ServiceModelStatus {
  ServiceName: string;
  ModelVersion: string;
  CurrentMAPE: number;
  ScalingMode: string;
  LastTrainedAt: string;
  LastRecalibratedAt: string;
  TrainingDataPoints: number;
  UpdatedAt: string;
}

const withBase = (path: string) => `${API_URL}${path}`;

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  if (!response.ok) {
    let details = `${response.status} ${response.statusText}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload?.error) {
        details = `${details}: ${payload.error}`;
      }
    } catch {
      // Ignore JSON parsing failures and keep the status text.
    }
    throw new Error(`API request failed (${details})`);
  }
  return (await response.json()) as T;
}

export async function fetchServicesSnapshot(): Promise<ServiceSummary[]> {
  const payload = await fetchJSON<{ services: ServiceSummary[] }>(
    withBase("/api/services"),
  );
  return payload.services ?? [];
}

export async function fetchServiceMetrics(
  name: string,
  minutes = 60,
): Promise<JsonRecord[]> {
  const payload = await fetchJSON<{ service: string; metrics: JsonRecord[] }>(
    withBase(`/api/services/${encodeURIComponent(name)}/metrics?minutes=${minutes}`),
  );
  return payload.metrics ?? [];
}

export async function fetchServicePredictions(
  name: string,
  limit = 100,
): Promise<JsonRecord[]> {
  const payload = await fetchJSON<{ service: string; predictions: JsonRecord[] }>(
    withBase(`/api/services/${encodeURIComponent(name)}/predictions?limit=${limit}`),
  );
  return payload.predictions ?? [];
}

export async function fetchAuditTrail(limit = 200): Promise<ControllerDecision[]> {
  const payload = await fetchJSON<{ audit: ControllerDecision[] }>(
    withBase(`/api/audit?limit=${limit}`),
  );
  return payload.audit ?? [];
}

export async function fetchServiceDecisions(
  name: string,
  limit = 100,
): Promise<ControllerDecision[]> {
  const payload = await fetchJSON<{ service: string; decisions: ControllerDecision[] }>(
    withBase(`/api/services/${encodeURIComponent(name)}/decisions?limit=${limit}`),
  );
  return payload.decisions ?? [];
}

export async function fetchServiceModel(
  name: string,
): Promise<ServiceModelStatus | null> {
  try {
    return await fetchJSON<ServiceModelStatus>(
      withBase(`/api/services/${encodeURIComponent(name)}/model`),
    );
  } catch (error) {
    if (error instanceof Error && error.message.includes("(404")) {
      return null;
    }
    throw error;
  }
}

export async function fetchSystemStatusSnapshot(): Promise<SystemStatus> {
  return fetchJSON<SystemStatus>(withBase("/api/system/status"));
}

export async function setServiceMode(
  serviceName: string,
  mode: OperatingMode,
  triggeredBy = "dashboard-ui",
): Promise<{ service: string; mode: string }> {
  return fetchJSON<{ service: string; mode: string }>(
    withBase(`/api/services/${encodeURIComponent(serviceName)}/mode`),
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode, triggered_by: triggeredBy }),
    },
  );
}

export async function setAllServicesMode(
  serviceNames: string[],
  mode: OperatingMode,
  triggeredBy = "dashboard-ui",
): Promise<void> {
  await Promise.all(
    serviceNames.map((serviceName) =>
      setServiceMode(serviceName, mode, triggeredBy),
    ),
  );
}

export async function setKillSwitch(
  enabled: boolean,
): Promise<{ enabled: boolean }> {
  return fetchJSON<{ enabled: boolean }>(withBase("/api/kill-switch"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  });
}

export async function triggerServiceRetrain(
  serviceName: string,
): Promise<{ success: boolean; message: string }> {
  return fetchJSON<{ success: boolean; message: string }>(
    withBase(`/api/services/${encodeURIComponent(serviceName)}/retrain`),
    { method: "POST" },
  );
}

export function useServices() {
  return useSWR(withBase("/api/services"), async (url) => {
    const payload = await fetchJSON<{ services: ServiceSummary[] }>(url);
    return payload.services ?? [];
  });
}

export function useServiceMetrics(name: string, minutes = 60) {
  return useSWR(
    withBase(`/api/services/${encodeURIComponent(name)}/metrics?minutes=${minutes}`),
    async (url) => {
      const payload = await fetchJSON<{ service: string; metrics: JsonRecord[] }>(url);
      return payload.metrics ?? [];
    },
  );
}

export function usePredictions(name: string, limit = 60) {
  return useSWR(
    withBase(`/api/services/${encodeURIComponent(name)}/predictions?limit=${limit}`),
    async (url) => {
      const payload = await fetchJSON<{ service: string; predictions: JsonRecord[] }>(
        url,
      );
      return payload.predictions ?? [];
    },
  );
}

export function useServiceDecisions(name: string, limit = 100) {
  return useSWR(
    withBase(`/api/services/${encodeURIComponent(name)}/decisions?limit=${limit}`),
    async (url) => {
      const payload = await fetchJSON<{ service: string; decisions: ControllerDecision[] }>(
        url,
      );
      return payload.decisions ?? [];
    },
  );
}

export function useServiceModel(name: string) {
  return useSWR(withBase(`/api/services/${encodeURIComponent(name)}/model`), async (url) => {
    try {
      return await fetchJSON<ServiceModelStatus>(url);
    } catch (error) {
      if (error instanceof Error && error.message.includes("(404")) {
        return null;
      }
      throw error;
    }
  });
}

export function useAuditLog(limit = 200) {
  return useSWR(withBase(`/api/audit?limit=${limit}`), async (url) => {
    const payload = await fetchJSON<{ audit: ControllerDecision[] }>(url);
    return payload.audit ?? [];
  });
}

export function useSystemStatus() {
  return useSWR(withBase("/api/system/status"), fetchJSON<SystemStatus>);
}
