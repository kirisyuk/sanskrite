/**
 * Single place that defines which map objects already show a facility name on marker layers.
 * Path legs defer endpoint object chips for these ids so captions are not split across two systems.
 */

export type MarkerMapHighlightRole = 'selected' | 'gateway' | 'search';

export type MarkerMapHighlight = { id: string; role: MarkerMapHighlightRole };

const ROLES_WITH_ENDPOINT_CAPTION: ReadonlySet<MarkerMapHighlightRole> = new Set([
  'gateway',
  'selected',
  'search',
]);

/**
 * Object ids that display an endpoint-style caption on geo markers (unclustered point or highlight chip).
 * Path-leg layer should not duplicate the same object name at leg endpoints for these ids.
 */
export function objectIdsWithMarkerEndpointCaption(
  highlightedObjects: readonly MarkerMapHighlight[] | null | undefined,
): ReadonlySet<string> {
  const out = new Set<string>();
  for (const item of highlightedObjects || []) {
    const id = String(item?.id || '').trim();
    if (!id || !ROLES_WITH_ENDPOINT_CAPTION.has(item.role)) continue;
    out.add(id);
  }
  return out;
}
