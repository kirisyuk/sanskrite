export function shouldRefetchGeoObjectsForLensEvent(
  event: { type?: string; lensId?: string; data?: any } | null | undefined,
  lensId: string | undefined,
): boolean {
  const targetLensId = String(lensId || '').trim();
  if (!targetLensId || !event) return false;
  const eventType = String(event.type || '').trim();
  if (!eventType) return false;
  const eventLensId = String(event.lensId || event.data?.lensId || '').trim();
  if (eventLensId && eventLensId !== targetLensId) return false;
  return (
    eventType === 'horizon.lens.updated' ||
    eventType === 'horizon.supply_chain.exploration.materialized'
  );
}
