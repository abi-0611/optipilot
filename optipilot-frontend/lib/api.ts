import useSWR from 'swr';

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function useServices() {
  return useSWR(`${API_URL}/api/services`, fetcher);
}

export function useServiceMetrics(name: string, minutes: number = 60) {
  return useSWR(`${API_URL}/api/services/${name}/metrics?minutes=${minutes}`, fetcher);
}

export function usePredictions(name: string, limit: number = 60) {
  return useSWR(`${API_URL}/api/services/${name}/predictions?limit=${limit}`, fetcher);
}

export function useAuditLog() {
  return useSWR(`${API_URL}/api/audit`, fetcher);
}

export function useSystemStatus() {
  return useSWR(`${API_URL}/api/status`, fetcher);
}

export async function setGlobalMode(mode: string) {
  const res = await fetch(`${API_URL}/api/settings/mode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  });
  return res.json();
}
