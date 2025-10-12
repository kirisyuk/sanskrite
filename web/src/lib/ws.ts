export function apiBaseToWs(baseUrl: string) {
  if (baseUrl.startsWith('https://')) return 'wss://' + baseUrl.slice('https://'.length);
  if (baseUrl.startsWith('http://')) return 'ws://' + baseUrl.slice('http://'.length);
  return baseUrl.replace(/^https?:/, (m) => (m === 'https:' ? 'wss:' : 'ws:'));
}

export function buildWsUrl(tenantId: string) {
  const base = (import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
  const wsBase = apiBaseToWs(base);
  return `${wsBase}/ws?tenantId=${encodeURIComponent(tenantId)}`;
}





