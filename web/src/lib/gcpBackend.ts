/**
 * Horizon web always talks to Cloud Run API and WS broker in GCP (nv-horizon-test, us-central1).
 * Override with VITE_API_BASE_URL / VITE_WS_BROKER_URL / VITE_WS_BASE_URL for another project or stage.
 */

export const DEFAULT_GCP_API_BASE_URL = 'https://nv-horizon-api-vosmg5vzoa-uc.a.run.app';
export const DEFAULT_GCP_WS_BROKER_BASE_URL = 'https://nv-horizon-ws-vosmg5vzoa-uc.a.run.app';

function trimVite(key: 'VITE_API_BASE_URL' | 'VITE_WS_BROKER_URL' | 'VITE_WS_BASE_URL'): string {
  const v = import.meta.env[key];
  if (v == null) return '';
  return String(v).trim();
}

export function resolveApiBaseUrl(): string {
  const u = trimVite('VITE_API_BASE_URL');
  return (u || DEFAULT_GCP_API_BASE_URL).replace(/\/$/, '');
}

export function resolveWsBrokerBaseUrl(): string {
  const ws = trimVite('VITE_WS_BASE_URL');
  if (ws) return ws.replace(/\/$/, '');
  const broker = trimVite('VITE_WS_BROKER_URL');
  if (broker) return broker.replace(/\/$/, '');
  return DEFAULT_GCP_WS_BROKER_BASE_URL.replace(/\/$/, '');
}
