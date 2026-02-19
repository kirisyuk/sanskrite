export function apiBaseToWs(baseUrl: string) {
  if (baseUrl.startsWith('https://')) return 'wss://' + baseUrl.slice('https://'.length);
  if (baseUrl.startsWith('http://')) return 'ws://' + baseUrl.slice('http://'.length);
  return baseUrl.replace(/^https?:/, (m) => (m === 'https:' ? 'wss:' : 'ws:'));
}

const WS_BROKER_URL = 'https://h1horizon-ws-yulmgw47va-uc.a.run.app';

export function buildWsUrl(_tenantId: string) {
  const wsBase = apiBaseToWs(WS_BROKER_URL.replace(/\/$/, ''));
  return `${wsBase}/ws`;
}





