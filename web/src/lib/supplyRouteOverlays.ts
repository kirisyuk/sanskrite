import { humanizeCanonicalPath, humanizeSlug } from './uiUtils';

export interface SupplyRouteOverlay {
  overlayKey: string;
  routeVariantKey: string;
  routeProfileKey: string;
  orderedRefs: string[];
  transportViaRefs: string[];
  routeCoordinates: Array<{ lat: number; lon: number }>;
  routeGeometrySource: string;
  mode: string;
  flowRefs: string[];
  alternateFlowRefs: string[];
  geoRefs: string[];
  zoneRefs: string[];
  affectedZoneRefs: string[];
  spatialSemantics: string[];
  vesselType: string;
  bindingConfidence: number;
  segmentConfidence: number;
  endpointBindingPolicy: string;
  status: string;
}

function readField<T = unknown>(value: any, camelKey: string, pascalKey?: string): T | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const pascal = pascalKey || `${camelKey.charAt(0).toUpperCase()}${camelKey.slice(1)}`;
  return (value as any)?.[camelKey] ?? (value as any)?.[pascal];
}

function readStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item || '').trim()).filter(Boolean);
}

function readNumber(value: unknown): number {
  const num = Number(value);
  return Number.isFinite(num) ? num : 0;
}

function readCoordinates(value: unknown): Array<{ lat: number; lon: number }> {
  if (!Array.isArray(value)) return [];
  return value
    .map((entry) => {
      const item = entry as { lat?: unknown; lon?: unknown; Lat?: unknown; Lon?: unknown };
      const lat = Number(item?.lat ?? item?.Lat);
      const lon = Number(item?.lon ?? item?.Lon);
      return Number.isFinite(lat) && Number.isFinite(lon) ? { lat, lon } : null;
    })
    .filter((item): item is { lat: number; lon: number } => Boolean(item));
}

function uniqueStrings(values: string[]): string[] {
  return Array.from(new Set(values.map((item) => String(item || '').trim()).filter(Boolean)));
}

function buildOverlayKey(overlay: Partial<SupplyRouteOverlay>): string {
  const variant = String(overlay.routeVariantKey || '').trim();
  if (variant) return `variant:${variant}`;
  const profile = String(overlay.routeProfileKey || '').trim();
  if (profile) return `profile:${profile}`;
  const refs = uniqueStrings(overlay.orderedRefs || []);
  if (refs.length > 0) return `refs:${refs.join('|')}`;
  const via = uniqueStrings(overlay.transportViaRefs || []);
  if (via.length > 0) return `via:${via.join('|')}`;
  return '';
}

function normalizeOverlay(raw: Partial<SupplyRouteOverlay>): SupplyRouteOverlay {
  const normalized: SupplyRouteOverlay = {
    overlayKey: String(raw.overlayKey || '').trim(),
    routeVariantKey: String(raw.routeVariantKey || '').trim(),
    routeProfileKey: String(raw.routeProfileKey || '').trim(),
    orderedRefs: uniqueStrings(raw.orderedRefs || []),
    transportViaRefs: uniqueStrings(raw.transportViaRefs || []),
    routeCoordinates: readCoordinates(raw.routeCoordinates),
    routeGeometrySource: String(raw.routeGeometrySource || '').trim(),
    mode: String(raw.mode || '').trim(),
    flowRefs: uniqueStrings(raw.flowRefs || []),
    alternateFlowRefs: uniqueStrings(raw.alternateFlowRefs || []),
    geoRefs: uniqueStrings(raw.geoRefs || []),
    zoneRefs: uniqueStrings(raw.zoneRefs || []),
    affectedZoneRefs: uniqueStrings(raw.affectedZoneRefs || []),
    spatialSemantics: uniqueStrings(raw.spatialSemantics || []),
    vesselType: String(raw.vesselType || '').trim(),
    bindingConfidence: readNumber(raw.bindingConfidence),
    segmentConfidence: readNumber(raw.segmentConfidence),
    endpointBindingPolicy: String(raw.endpointBindingPolicy || '').trim(),
    status: String(raw.status || '').trim(),
  };
  normalized.overlayKey = buildOverlayKey(normalized);
  return normalized;
}

function synthesizeLegacyOverlay(leg: any): SupplyRouteOverlay | null {
  const overlay = normalizeOverlay({
    routeVariantKey: String(readField(leg, 'routeVariantKey') || '').trim(),
    routeProfileKey: String(readField(leg, 'routeProfileKey') || '').trim(),
    orderedRefs: [
      String(readField(leg, 'supplierRef') || '').trim(),
      ...readStringList(readField(leg, 'transportViaRefs')),
      String(readField(leg, 'customerRef') || '').trim(),
    ].filter(Boolean),
    transportViaRefs: readStringList(readField(leg, 'transportViaRefs')),
    routeCoordinates: readCoordinates(readField(leg, 'routeCoordinates')),
    routeGeometrySource: String(readField(leg, 'routeGeometrySource') || '').trim(),
    mode: String(readField(leg, 'mode') || '').trim(),
    flowRefs: readStringList(readField(leg, 'flowRefs')),
    alternateFlowRefs: readStringList(readField(leg, 'alternateFlowRefs')),
    geoRefs: readStringList(readField(leg, 'geoRefs')),
    zoneRefs: readStringList(readField(leg, 'zoneRefs')),
    affectedZoneRefs: readStringList(readField(leg, 'affectedZoneRefs')),
    spatialSemantics: readStringList(readField(leg, 'spatialSemantics')),
    vesselType: String(readField(leg, 'vesselType') || '').trim(),
    bindingConfidence: readNumber(readField(leg, 'bindingConfidence')),
    segmentConfidence: readNumber(readField(leg, 'segmentConfidence')),
    endpointBindingPolicy: String(readField(leg, 'endpointBindingPolicy') || '').trim(),
    status: 'primary',
  });
  return overlay.overlayKey ? overlay : null;
}

function overlayQualityRank(source: string): number {
  switch (String(source || '').trim()) {
    case 'route_geometry_profile':
    case 'curated_flow_catalog':
      return 5;
    case 'composed_route_segments':
      return 4;
    case 'facility_route_overlay':
      return 3;
    case 'partial_segment_geometry':
      return 2;
    case 'fragmentary_waypoint_sequence':
      return 1;
    default:
      return 0;
  }
}

export function selectPrimaryLegRouteOverlay(overlays: SupplyRouteOverlay[]): SupplyRouteOverlay | null {
  if (!overlays.length) return null;
  let best = overlays[0];
  let bestScore = -1;
  for (const overlay of overlays) {
    let score = overlay.bindingConfidence * 10 + overlay.segmentConfidence * 4;
    score += overlayQualityRank(overlay.routeGeometrySource) * 3;
    score += overlay.flowRefs.length * 0.6;
    if (overlay.routeCoordinates.length >= 2) score += 0.8;
    if (overlay.status === 'primary') score += 1.5;
    if (score > bestScore) {
      best = overlay;
      bestScore = score;
    }
  }
  return best;
}

export function readLegRouteOverlays(leg: any): SupplyRouteOverlay[] {
  const rawOverlays = readField<any[]>(leg, 'routeOverlays');
  const overlays = Array.isArray(rawOverlays)
    ? rawOverlays.map((item: any) => normalizeOverlay({
        overlayKey: readField(item, 'overlayKey'),
        routeVariantKey: readField(item, 'routeVariantKey'),
        routeProfileKey: readField(item, 'routeProfileKey'),
        orderedRefs: readStringList(readField(item, 'orderedRefs')),
        transportViaRefs: readStringList(readField(item, 'transportViaRefs')),
        routeCoordinates: readCoordinates(readField(item, 'routeCoordinates')),
        routeGeometrySource: readField(item, 'routeGeometrySource'),
        mode: readField(item, 'mode'),
        flowRefs: readStringList(readField(item, 'flowRefs')),
        alternateFlowRefs: readStringList(readField(item, 'alternateFlowRefs')),
        geoRefs: readStringList(readField(item, 'geoRefs')),
        zoneRefs: readStringList(readField(item, 'zoneRefs')),
        affectedZoneRefs: readStringList(readField(item, 'affectedZoneRefs')),
        spatialSemantics: readStringList(readField(item, 'spatialSemantics')),
        vesselType: readField(item, 'vesselType'),
        bindingConfidence: readField(item, 'bindingConfidence'),
        segmentConfidence: readField(item, 'segmentConfidence'),
        endpointBindingPolicy: readField(item, 'endpointBindingPolicy'),
        status: readField(item, 'status'),
      }))
    : [];
  const legacy = synthesizeLegacyOverlay(leg);
  if (legacy) overlays.push(legacy);
  const deduped = new Map<string, SupplyRouteOverlay>();
  for (const overlay of overlays) {
    if (!overlay.overlayKey) continue;
    if (!deduped.has(overlay.overlayKey)) {
      deduped.set(overlay.overlayKey, overlay);
      continue;
    }
    const current = deduped.get(overlay.overlayKey)!;
    deduped.set(overlay.overlayKey, normalizeOverlay({
      ...current,
      orderedRefs: current.orderedRefs.length > 0 ? current.orderedRefs : overlay.orderedRefs,
      transportViaRefs: [...current.transportViaRefs, ...overlay.transportViaRefs],
      routeCoordinates: current.routeCoordinates.length > 0 ? current.routeCoordinates : overlay.routeCoordinates,
      routeGeometrySource: current.routeGeometrySource || overlay.routeGeometrySource,
      mode: current.mode || overlay.mode,
      flowRefs: [...current.flowRefs, ...overlay.flowRefs],
      alternateFlowRefs: [...current.alternateFlowRefs, ...overlay.alternateFlowRefs],
      geoRefs: [...current.geoRefs, ...overlay.geoRefs],
      zoneRefs: [...current.zoneRefs, ...overlay.zoneRefs],
      affectedZoneRefs: [...current.affectedZoneRefs, ...overlay.affectedZoneRefs],
      spatialSemantics: [...current.spatialSemantics, ...overlay.spatialSemantics],
      vesselType: current.vesselType || overlay.vesselType,
      bindingConfidence: current.bindingConfidence || overlay.bindingConfidence,
      segmentConfidence: current.segmentConfidence || overlay.segmentConfidence,
      endpointBindingPolicy: current.endpointBindingPolicy || overlay.endpointBindingPolicy,
      status: current.status || overlay.status,
    }));
  }
  const normalized = Array.from(deduped.values());
  const primary = selectPrimaryLegRouteOverlay(normalized);
  return normalized.map((overlay) => ({
    ...overlay,
    status: primary && overlay.overlayKey === primary.overlayKey ? 'primary' : (overlay.status || 'alternate'),
  }));
}

export function humanizeRouteGeometrySource(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'route_geometry_profile':
      return 'Saved route profile';
    case 'curated_flow_catalog':
      return 'Curated corridor';
    case 'composed_route_segments':
      return 'Composed route';
    case 'facility_route_overlay':
      return 'Facility route overlay';
    case 'partial_segment_geometry':
      return 'Partial route';
    case 'fragmentary_waypoint_sequence':
      return 'Waypoint hypothesis';
    case 'flow':
      return 'Mapped flow';
    case 'fragment':
      return 'Approximate route';
    default:
      return humanizeSlug(raw);
  }
}

export function humanizeTransportMode(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'sea':
    case 'ocean':
      return 'Sea';
    case 'air':
      return 'Air';
    case 'rail':
      return 'Rail';
    case 'road':
      return 'Road';
    case 'pipeline_oil':
      return 'Oil pipeline';
    case 'pipeline_gas':
      return 'Gas pipeline';
    case 'pipeline_h2':
      return 'Hydrogen pipeline';
    case 'pipeline_co2':
      return 'CO2 pipeline';
    case 'power_hvdc':
      return 'HVDC power';
    case 'power_hvac':
      return 'HVAC power';
    case 'internet_cable':
      return 'Cable';
    case 'ferry_roro':
      return 'Ferry / Ro-Ro';
    case 'intermodal':
    case 'multimodal':
      return 'Intermodal';
    default:
      return humanizeSlug(raw);
  }
}

export function describeRouteOverlay(overlay: SupplyRouteOverlay | null | undefined): string {
  if (!overlay) return '';
  const flowLabel = overlay.flowRefs
    .map((ref) => humanizeCanonicalPath(ref) || humanizeSlug(ref))
    .filter(Boolean)[0] || '';
  if (flowLabel) return flowLabel;
  const modeLabel = humanizeTransportMode(overlay.mode);
  const viaLabels = overlay.transportViaRefs
    .map((ref) => humanizeCanonicalPath(ref) || humanizeSlug(ref))
    .filter(Boolean);
  const vesselLabel = overlay.vesselType ? humanizeSlug(overlay.vesselType) : '';
  if (viaLabels.length > 0) {
    const head = viaLabels[0];
    const extra = viaLabels.length > 1 ? ` +${viaLabels.length - 1}` : '';
    const base = modeLabel ? `${modeLabel} via ${head}${extra}` : `Via ${head}${extra}`;
    return vesselLabel ? `${base} • ${vesselLabel}` : base;
  }
  const geometryLabel = humanizeRouteGeometrySource(overlay.routeGeometrySource);
  if (modeLabel && geometryLabel) {
    return vesselLabel ? `${modeLabel} • ${vesselLabel}` : modeLabel;
  }
  return vesselLabel || modeLabel || geometryLabel;
}
