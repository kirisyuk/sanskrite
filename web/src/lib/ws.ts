export function apiBaseToWs(baseUrl: string) {
  if (baseUrl.startsWith('https://')) return 'wss://' + baseUrl.slice('https://'.length);
  if (baseUrl.startsWith('http://')) return 'ws://' + baseUrl.slice('http://'.length);
  return baseUrl.replace(/^https?:/, (m) => (m === 'https:' ? 'wss:' : 'ws:'));
}

/** WebSocket broker base URL (https). Falls back to API base if unset. */
export function getWsBrokerBaseUrl(): string {
  const wsUrl = import.meta.env.VITE_WS_BASE_URL || import.meta.env.VITE_WS_BROKER_URL;
  if (wsUrl) return wsUrl.replace(/\/$/, '');
  const apiUrl = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
  return apiUrl.replace(/\/$/, '');
}

export function buildWsUrl(_tenantId: string) {
  const base = getWsBrokerBaseUrl();
  const wsBase = apiBaseToWs(base);
  return `${wsBase}/ws`;
}





