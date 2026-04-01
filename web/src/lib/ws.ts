import { resolveWsBrokerBaseUrl } from './gcpBackend';

export function apiBaseToWs(baseUrl: string) {
  if (baseUrl.startsWith('https://')) return 'wss://' + baseUrl.slice('https://'.length);
  if (baseUrl.startsWith('http://')) return 'ws://' + baseUrl.slice('http://'.length);
  return baseUrl.replace(/^https?:/, (m) => (m === 'https:' ? 'wss:' : 'ws:'));
}

/** WebSocket broker base URL (https). Default: Cloud Run nv-horizon-ws in GCP. */
export function getWsBrokerBaseUrl(): string {
  return resolveWsBrokerBaseUrl();
}

export function buildWsUrl(_tenantId: string) {
  const base = getWsBrokerBaseUrl();
  const wsBase = apiBaseToWs(base);
  return `${wsBase}/ws`;
}





