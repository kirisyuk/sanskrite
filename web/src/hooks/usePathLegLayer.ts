import { useEffect, useMemo, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { STATIC_TRANSPORT_FLOW_DATA, type TransportFlowFeature } from '../lib/staticTransportFlows';
import { buildPointFeature, ensureMarkerImages } from './useGeoMarkerLayer';
import { MAP_TEXT_FONT_STACK } from './useMapInstance';
import { humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'path-legs-src';
const GLOW_LAYER = 'path-legs-glow';
const PINNED_LAYER = 'path-legs-pinned';
const DISCOVERED_LAYER = 'path-legs-discovered';
const BOTTLENECK_LAYER = 'path-legs-bottlenecks';
const LEG_OBJECT_ANCHOR_LAYER = 'path-legs-object-anchors';
const LEG_DIRECTION_LAYER = 'path-legs-direction';
const LEG_OBJECT_LAYER = 'path-legs-objects';
const LEG_OBJECT_LABEL_LAYER = 'path-legs-object-labels';
const LEG_MEANING_LABEL_LAYER = 'path-legs-meaning-labels';

type ThemeMode = 'dark' | 'light';

export interface PathLegLayerStats {
  artifactCount: number;
  legCount: number;
  boundLegCount: number;
  bottleneckCount: number;
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function artifactLegField<T = unknown>(leg: any, ...keys: string[]): T | undefined {
  for (const key of keys) {
    if (leg && leg[key] != null) return leg[key] as T;
  }
  return undefined;
}

function artifactLegString(leg: any, ...keys: string[]): string {
  return String(artifactLegField(leg, ...keys) || '').trim();
}

function artifactLegStringList(leg: any, ...keys: string[]): string[] {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item || '').trim()).filter(Boolean);
}

function artifactLegNumber(leg: any, ...keys: string[]): number | null {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (typeof value !== 'number' || !Number.isFinite(value)) return null;
  return value;
}

function toFixedSafe(value: number | undefined, digits = 2): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(digits) : 'n/a';
}

function escapeHtml(input: string): string {
  return String(input || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function pathLegColor(stressScore: number, theme: ThemeMode): string {
  if (stressScore >= 0.78) return theme === 'dark' ? '#e88a9d' : '#c2414f';
  if (stressScore >= 0.58) return theme === 'dark' ? '#d8a247' : '#a16207';
  if (stressScore >= 0.36) return theme === 'dark' ? '#67aee6' : '#3b82c6';
  return theme === 'dark' ? '#6fc7d8' : '#1b7b84';
}

function legObjectLabel(obj: GeoObject): string {
  return String(obj.label || '').trim() || humanizeCanonicalPath(obj.canonicalPath) || obj.canonicalPath;
}

function legMeaningLabel(
  productRef: string,
  flowMode: string,
  flowRefs: string[],
): string {
  const productLabel = productRef ? (humanizeCanonicalPath(productRef) || productRef) : '';
  const modeLabel = flowMode || '';
  const vesselishRef = flowRefs.find((ref) => /vessel|ship|tanker/i.test(ref)) || '';
  const vesselLabel = vesselishRef ? humanizeCanonicalPath(vesselishRef) || vesselishRef : '';
  const parts = [productLabel, modeLabel, vesselLabel].filter(Boolean);
  if (parts.length === 0) return '';
  if (parts.length === 1) return parts[0];
  return `${parts[0]}\n${parts.slice(1).join(' • ')}`;
}

function pointAlongLine(
  coords: [number, number][],
  fraction: number,
): { point: [number, number]; bearing: number } | null {
  if (coords.length === 0) return null;
  if (coords.length === 1) return { point: coords[0], bearing: 0 };

  let total = 0;
  const segmentLengths: number[] = [];
  for (let index = 1; index < coords.length; index += 1) {
    const start = coords[index - 1];
    const end = coords[index];
    const dx = end[0] - start[0];
    const dy = end[1] - start[1];
    const length = Math.hypot(dx, dy);
    segmentLengths.push(length);
    total += length;
  }

  if (total <= 1e-9) {
    const center = coords[Math.floor(coords.length / 2)] || coords[0] || null;
    return center ? { point: center, bearing: 0 } : null;
  }

  const target = clamp(fraction, 0, 1) * total;
  let traversed = 0;
  for (let index = 1; index < coords.length; index += 1) {
    const start = coords[index - 1];
    const end = coords[index];
    const segmentLength = segmentLengths[index - 1] || 0;
    if (traversed + segmentLength < target) {
      traversed += segmentLength;
      continue;
    }
    const remaining = target - traversed;
    const ratio = segmentLength > 1e-9 ? remaining / segmentLength : 0;
    return {
      point: [
        start[0] + (end[0] - start[0]) * ratio,
        start[1] + (end[1] - start[1]) * ratio,
      ],
      bearing: Math.atan2(end[1] - start[1], end[0] - start[0]) * 180 / Math.PI,
    };
  }

  const last = coords[coords.length - 1] || null;
  const prev = coords[coords.length - 2] || last;
  return last ? {
    point: last,
    bearing: Math.atan2(last[1] - prev[1], last[0] - prev[0]) * 180 / Math.PI,
  } : null;
}

function midpoint(coords: [number, number][]): [number, number] | null {
  return pointAlongLine(coords, 0.5)?.point || null;
}

function dedupeCoords(coords: [number, number][]): [number, number][] {
  const out: [number, number][] = [];
  for (const [lon, lat] of coords) {
    const prev = out[out.length - 1];
    if (prev && Math.abs(prev[0] - lon) < 1e-7 && Math.abs(prev[1] - lat) < 1e-7) continue;
    out.push([lon, lat]);
  }
  return out;
}

type ScreenRect = {
  left: number;
  right: number;
  top: number;
  bottom: number;
};

function rectsOverlap(a: ScreenRect, b: ScreenRect, padding = 0): boolean {
  return !(
    a.right + padding < b.left
    || b.right + padding < a.left
    || a.bottom + padding < b.top
    || b.bottom + padding < a.top
  );
}

function lineLabelWidthPx(text: string, maxWidth: number): number {
  const longestLine = String(text || '')
    .split('\n')
    .reduce((max, line) => Math.max(max, line.trim().length), 0);
  return clamp(longestLine * 6.4, 34, maxWidth);
}

function legObjectMarkerRect(point: { x: number; y: number }): ScreenRect {
  const half = 14;
  return {
    left: point.x - half,
    right: point.x + half,
    top: point.y - half,
    bottom: point.y + half,
  };
}

function legObjectLabelRect(point: { x: number; y: number }, text: string): ScreenRect {
  const width = lineLabelWidthPx(text, 220);
  return {
    left: point.x + 20,
    right: point.x + 20 + width,
    top: point.y - 8,
    bottom: point.y + 8,
  };
}

function legMeaningLabelRect(point: { x: number; y: number }, text: string): ScreenRect {
  const lines = Math.max(1, String(text || '').split('\n').filter(Boolean).length);
  const width = lineLabelWidthPx(text, 180);
  const height = lines > 1 ? 28 : 16;
  return {
    left: point.x - width / 2,
    right: point.x + width / 2,
    top: point.y - height - 12,
    bottom: point.y - 12,
  };
}

function normalizeMeaningText(value: string): string {
  return String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
}

function stylePathLegPopup(popup: maplibregl.Popup, theme: ThemeMode): void {
  const root = popup.getElement();
  const content = root.querySelector('.maplibregl-popup-content') as HTMLElement | null;
  const tip = root.querySelector('.maplibregl-popup-tip') as HTMLElement | null;
  const background = theme === 'dark' ? 'rgba(5, 12, 24, 0.96)' : 'rgba(255,255,255,0.98)';
  const foreground = theme === 'dark' ? '#e2e8f0' : '#0f172a';
  const border = theme === 'dark' ? '1px solid rgba(148,163,184,0.24)' : '1px solid rgba(15,23,42,0.12)';

  if (content) {
    content.style.background = background;
    content.style.color = foreground;
    content.style.border = border;
    content.style.borderRadius = '16px';
    content.style.boxShadow = theme === 'dark'
      ? '0 20px 48px rgba(2,6,23,0.58)'
      : '0 20px 48px rgba(15,23,42,0.16)';
    content.style.padding = '14px 16px';
  }
  if (tip) {
    tip.style.borderTopColor = background;
    tip.style.borderBottomColor = background;
    tip.style.borderLeftColor = background;
    tip.style.borderRightColor = background;
  }
}

function buildViewportPathLegFeatureCollection(
  featureCollection: GeoJSON.FeatureCollection,
  map: maplibregl.Map,
): GeoJSON.FeatureCollection {
  const features = (featureCollection.features || []).map((feature) => ({
    ...feature,
    geometry: feature.geometry ? JSON.parse(JSON.stringify(feature.geometry)) : feature.geometry,
    properties: { ...(feature.properties || {}) },
  })) as GeoJSON.Feature[];

  const zoom = map.getZoom();
  const anchorFeatures: GeoJSON.Feature[] = [];
  const legObjects = features
    .filter((feature) => feature.geometry?.type === 'Point' && String((feature.properties || {}).kind || '').trim() === 'leg-object')
    .map((feature) => {
      const coordinates = (feature.geometry as GeoJSON.Point).coordinates as [number, number];
      const props = (feature.properties || {}) as Record<string, any>;
      return {
        feature,
        coordinates,
        projected: map.project({ lng: coordinates[0], lat: coordinates[1] }),
        labelPriority: Number(props.labelPriority || 0),
        pathActive: Number(props.pathActive || 0) === 1,
        objectRole: String(props.objectRole || '').trim(),
        labelText: String(props.labelText || '').trim(),
      };
    });

  const groups: Array<typeof legObjects> = [];
  const groupRadiusPx = zoom >= 8 ? 12 : zoom >= 6 ? 10 : 8;
  for (const item of legObjects) {
    let targetGroup: typeof legObjects | null = null;
    let bestDistanceSq = Number.POSITIVE_INFINITY;
    for (const group of groups) {
      const centroidX = group.reduce((sum, entry) => sum + entry.projected.x, 0) / group.length;
      const centroidY = group.reduce((sum, entry) => sum + entry.projected.y, 0) / group.length;
      const dx = item.projected.x - centroidX;
      const dy = item.projected.y - centroidY;
      const distSq = dx * dx + dy * dy;
      if (distSq <= groupRadiusPx * groupRadiusPx && distSq < bestDistanceSq) {
        bestDistanceSq = distSq;
        targetGroup = group;
      }
    }
    if (targetGroup) targetGroup.push(item);
    else groups.push([item]);
  }

  for (const group of groups) {
    if (group.length <= 1) continue;
    const centroidX = group.reduce((sum, entry) => sum + entry.projected.x, 0) / group.length;
    const centroidY = group.reduce((sum, entry) => sum + entry.projected.y, 0) / group.length;
    const sortedGroup = [...group].sort((left, right) => {
      if (left.pathActive !== right.pathActive) return Number(right.pathActive) - Number(left.pathActive);
      if (left.labelPriority !== right.labelPriority) return right.labelPriority - left.labelPriority;
      const roleRank = (role: string) => (role === 'supplier' || role === 'customer' ? 1 : 0);
      if (roleRank(left.objectRole) !== roleRank(right.objectRole)) return roleRank(right.objectRole) - roleRank(left.objectRole);
      return left.labelText.localeCompare(right.labelText);
    });
    const primaryCount = Math.min(sortedGroup.length, 6);
    const baseRadius = sortedGroup.length === 2 ? 16 : sortedGroup.length <= 4 ? 18 : 20;
    for (let index = 0; index < sortedGroup.length; index += 1) {
      const item = sortedGroup[index];
      const ring = Math.floor(index / 6);
      const ringSlots = ring === 0 ? primaryCount : Math.min(sortedGroup.length - ring * 6, 6);
      const slotIndex = ring === 0 ? index : index - ring * 6;
      const angle = (-Math.PI / 2) + ((Math.PI * 2) * slotIndex) / Math.max(ringSlots, 1);
      const radius = baseRadius + ring * 12;
      const offsetPoint = {
        x: centroidX + Math.cos(angle) * radius,
        y: centroidY + Math.sin(angle) * radius,
      };
      const offsetLngLat = map.unproject(offsetPoint as maplibregl.PointLike);
      item.feature.geometry = {
        type: 'Point',
        coordinates: [offsetLngLat.lng, offsetLngLat.lat],
      };
      const props = (item.feature.properties || {}) as Record<string, any>;
      props.spreadGroupSize = sortedGroup.length;
      props.spreadIndex = index;
      anchorFeatures.push({
        type: 'Feature',
        id: `${String(item.feature.id || props.objectId || props.canonicalPath || index)}:anchor`,
        geometry: {
          type: 'LineString',
          coordinates: [item.coordinates, [offsetLngLat.lng, offsetLngLat.lat]],
        },
        properties: {
          kind: 'leg-object-anchor',
          anchorColor: String(props.color || (props.pathActive ? '#cbd5e1' : '#94a3b8')),
          anchorOpacity: props.pathActive === 1 ? 0.34 : 0.2,
          anchorWidth: sortedGroup.length >= 4 ? 1.05 : 0.95,
        },
      } as GeoJSON.Feature);
    }
  }

  const markerRects: ScreenRect[] = [];
  const objectLabelRects: ScreenRect[] = [];
  const meaningCandidates: Array<{
    feature: GeoJSON.Feature;
    rect: ScreenRect;
    cellKey: string;
    dedupeKey: string;
    active: boolean;
    stressScore: number;
  }> = [];

  for (const feature of features) {
    if (feature.geometry?.type !== 'Point') continue;
    const coordinates = (feature.geometry as GeoJSON.Point).coordinates as [number, number];
    const projected = map.project({ lng: coordinates[0], lat: coordinates[1] });
    const props = (feature.properties || {}) as Record<string, any>;
    const kind = String(props.kind || '').trim();
    if (kind === 'leg-object') {
      const labelText = String(props.labelText || '').trim();
      markerRects.push(legObjectMarkerRect(projected));
      if (labelText) objectLabelRects.push(legObjectLabelRect(projected, labelText));
      continue;
    }
    if (kind !== 'leg-meaning') continue;
    const meaningText = String(props.meaningText || '').trim();
    const rect = legMeaningLabelRect(projected, meaningText);
    const cellWidth = zoom >= 8 ? 150 : zoom >= 6 ? 180 : 240;
    const cellHeight = zoom >= 8 ? 110 : zoom >= 6 ? 130 : 170;
    const dedupeWidth = zoom >= 8 ? 180 : zoom >= 6 ? 220 : 320;
    const dedupeHeight = zoom >= 8 ? 140 : zoom >= 6 ? 170 : 240;
    const cellKey = `${Math.floor(projected.x / cellWidth)}:${Math.floor(projected.y / cellHeight)}`;
    const dedupeKey = `${normalizeMeaningText(meaningText)}:${Math.floor(projected.x / dedupeWidth)}:${Math.floor(projected.y / dedupeHeight)}`;
    meaningCandidates.push({
      feature,
      rect,
      cellKey,
      dedupeKey,
      active: Number(props.pathActive || 0) === 1,
      stressScore: Number(props.stressScore || 0),
    });
  }

  meaningCandidates.sort((left, right) => {
    if (left.active !== right.active) return Number(right.active) - Number(left.active);
    return right.stressScore - left.stressScore;
  });

  const acceptedRects: ScreenRect[] = [];
  const usedCells = new Map<string, number>();
  const usedMeaningKeys = new Set<string>();

  for (const candidate of meaningCandidates) {
    const props = (candidate.feature.properties || {}) as Record<string, any>;
    const suppressForObjectLabels = zoom >= 5.5;
    const suppressForMarkers = zoom >= 7.25;
    const objectLabelPadding = zoom >= 8 ? 8 : zoom >= 6 ? 4 : 0;
    const markerPadding = zoom >= 8 ? 8 : zoom >= 7 ? 3 : 0;
    const meaningPadding = zoom >= 8 ? 10 : zoom >= 6 ? 6 : 2;
    const collidesWithObjectLabel = suppressForObjectLabels
      ? objectLabelRects.some((rect) => rectsOverlap(rect, candidate.rect, objectLabelPadding))
      : false;
    const collidesWithMarker = suppressForMarkers
      ? markerRects.some((rect) => rectsOverlap(rect, candidate.rect, markerPadding))
      : false;
    const collidesWithMeaning = acceptedRects.some((rect) => rectsOverlap(rect, candidate.rect, meaningPadding));
    const cellBudget = zoom >= 8 ? 1 : zoom >= 6.5 ? 2 : zoom >= 5 ? 4 : 7;
    const cellCount = usedCells.get(candidate.cellKey) || 0;
    const duplicateMeaning = usedMeaningKeys.has(candidate.dedupeKey);
    const visible = !collidesWithObjectLabel && !collidesWithMarker && !collidesWithMeaning && !duplicateMeaning && cellCount < cellBudget;
    props.meaningVisible = visible ? 1 : 0;
    if (!visible) continue;
    acceptedRects.push(candidate.rect);
    usedCells.set(candidate.cellKey, cellCount + 1);
    usedMeaningKeys.add(candidate.dedupeKey);
  }

  return {
    type: 'FeatureCollection',
    features: [...features, ...anchorFeatures],
  };
}

function lineOptionsFromFeature(feature: TransportFlowFeature | undefined): [number, number][][] {
  const geometry = feature?.geometry;
  if (!geometry) return [];
  if (geometry.type === 'LineString') {
    return [dedupeCoords((geometry.coordinates || []) as [number, number][])];
  }
  if (geometry.type === 'MultiLineString') {
    return ((geometry.coordinates || []) as [number, number][][]).map((line) => dedupeCoords(line));
  }
  return [];
}

function distanceSq(a: [number, number], b: [number, number]): number {
  const dx = a[0] - b[0];
  const dy = a[1] - b[1];
  return dx * dx + dy * dy;
}

function orientLineForEndpoints(line: [number, number][], start: [number, number], end: [number, number]): [number, number][] {
  if (line.length < 2) return line;
  const first = line[0];
  const last = line[line.length - 1];
  const direct = distanceSq(start, first) + distanceSq(end, last);
  const reverse = distanceSq(start, last) + distanceSq(end, first);
  return reverse < direct ? [...line].reverse() : line;
}

function maybeConnectEndpoints(line: [number, number][], fallback: [number, number][]): [number, number][] {
  if (line.length < 2 || fallback.length < 2) return line;
  const start = fallback[0];
  const end = fallback[fallback.length - 1];
  const oriented = orientLineForEndpoints(line, start, end);
  const out = [...oriented];
  if (distanceSq(start, out[0]) > 1e-7) {
    out.unshift(start);
  }
  if (distanceSq(end, out[out.length - 1]) > 1e-7) {
    out.push(end);
  }
  return dedupeCoords(out);
}

function selectFlowGeometry(
  flowRefs: string[],
  fallbackCoords: [number, number][],
  flowById: Map<string, TransportFlowFeature>,
): { coords: [number, number][]; flowId?: string; flowMode?: string; geometrySource: 'flow' | 'fragment'; snapped: boolean } {
  if (fallbackCoords.length < 2) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  const start = fallbackCoords[0];
  const end = fallbackCoords[fallbackCoords.length - 1];
  let best: { coords: [number, number][]; score: number; flowId: string; flowMode?: string } | null = null;

  for (const ref of flowRefs) {
    const feature = flowById.get(ref);
    for (const option of lineOptionsFromFeature(feature)) {
      if (option.length < 2) continue;
      const oriented = orientLineForEndpoints(option, start, end);
      const score = distanceSq(start, oriented[0]) + distanceSq(end, oriented[oriented.length - 1]);
      if (!best || score < best.score) {
        best = {
          coords: maybeConnectEndpoints(oriented, fallbackCoords),
          score,
          flowId: ref,
          flowMode: feature?.properties?.flowMode ? String(feature.properties.flowMode) : undefined,
        };
      }
    }
  }

  if (!best) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  return {
    coords: best.coords,
    flowId: best.flowId,
    flowMode: best.flowMode,
    geometrySource: 'flow',
    snapped: true,
  };
}

export function usePathLegLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  artifacts: MaterializedArtifact[],
  objects: GeoObject[],
  theme: ThemeMode,
  enabled: boolean,
  activeArtifactIds: string[] = [],
) {
  const [stats, setStats] = useState<PathLegLayerStats>({
    artifactCount: 0,
    legCount: 0,
    boundLegCount: 0,
    bottleneckCount: 0,
  });
  const popupRef = useRef<maplibregl.Popup | null>(null);

  const data = useMemo(() => {
    const activeArtifactSet = new Set((activeArtifactIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const hasActiveArtifacts = activeArtifactSet.size > 0;
    const coordsByPath = new Map<string, { lon: number; lat: number }>();
    const objectByPath = new Map<string, GeoObject>();
    const flowById = new Map<string, TransportFlowFeature>();
    for (const obj of objects) {
      const cp = String(obj?.canonicalPath || '').trim();
      const lat = Number(obj?.coordinates?.lat);
      const lon = Number(obj?.coordinates?.lon);
      if (!cp || !Number.isFinite(lat) || !Number.isFinite(lon)) continue;
      coordsByPath.set(cp, { lon, lat });
      if (!objectByPath.has(cp)) objectByPath.set(cp, obj);
    }
    for (const feature of STATIC_TRANSPORT_FLOW_DATA.features || []) {
      const id = String(feature?.id || '').trim();
      if (id) flowById.set(id, feature);
    }

    let legCount = 0;
    let boundLegCount = 0;
    let bottleneckCount = 0;

    const features: GeoJSON.Feature[] = [];
    const legObjectFeatures = new Map<string, GeoJSON.Feature>();
    const legObjectEntries = new Map<string, GeoObject>();
    for (const artifact of artifacts || []) {
      const artifactId = String(artifact.id || '').trim();
      const isNarrativeActive = !hasActiveArtifacts || activeArtifactSet.has(artifactId);
      const displayName = String(artifact.displayName || artifact.id || 'Supply path').trim();
      const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
      const bottleneckIndex = artifact.payload?.stress?.bottleneckLegIndex;
      for (const leg of legs) {
        const legIndex = artifactLegNumber(leg, 'index', 'Index');
        if (legIndex == null) continue;
        const refs = [
          artifactLegString(leg, 'supplierRef', 'SupplierRef'),
          ...artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs'),
          artifactLegString(leg, 'customerRef', 'CustomerRef'),
        ].filter(Boolean);
        const fallbackCoords = dedupeCoords(
          refs
            .map((ref) => coordsByPath.get(ref))
            .filter((point): point is { lon: number; lat: number } => Boolean(point))
            .map((point) => [point.lon, point.lat] as [number, number]),
        );
        const flowRefs = artifactLegStringList(leg, 'flowRefs', 'FlowRefs');
        const { coords, flowId, flowMode, geometrySource, snapped } = selectFlowGeometry(flowRefs, fallbackCoords, flowById);
        if (coords.length < 2) continue;
        legCount += 1;
        for (const [refIndex, ref] of refs.entries()) {
          const object = objectByPath.get(ref);
          if (!object?.coordinates) continue;
          const featureKey = object.id || ref;
          const role = refIndex === 0 ? 'supplier' : refIndex === refs.length - 1 ? 'customer' : 'via';
          const nextPriority = role === 'supplier' || role === 'customer' ? 96 : 88;
          const existing = legObjectFeatures.get(featureKey);
          if (!existing) {
            const pointFeature = buildPointFeature(object, theme, false) as GeoJSON.Feature<GeoJSON.Point>;
            pointFeature.id = `leg-object:${featureKey}`;
            pointFeature.properties = {
              ...(pointFeature.properties || {}),
              kind: 'leg-object',
              objectId: object.id,
              canonicalPath: object.canonicalPath,
              labelText: legObjectLabel(object),
              labelPriority: nextPriority,
              objectRole: role,
              pathActive: isNarrativeActive ? 1 : 0,
            };
            legObjectFeatures.set(featureKey, pointFeature);
            legObjectEntries.set(featureKey, object);
          } else {
            const props = (existing.properties || {}) as Record<string, any>;
            props.labelPriority = Math.max(Number(props.labelPriority || 0), nextPriority);
            props.pathActive = Number(props.pathActive || 0) || (isNarrativeActive ? 1 : 0);
            if (role === 'supplier' || role === 'customer') props.objectRole = role;
          }
        }
        const zoneRefs = artifactLegStringList(leg, 'zoneRefs', 'ZoneRefs');
        const affectedZoneRefs = artifactLegStringList(leg, 'affectedZoneRefs', 'AffectedZoneRefs');
        const spatialSemantics = artifactLegStringList(leg, 'spatialSemantics', 'SpatialSemantics');
        const stressPayload = artifactLegField<any>(leg, 'stress', 'Stress');
        const dominantFactors = Array.isArray(stressPayload?.dominantFactors || stressPayload?.DominantFactors)
          ? (stressPayload.dominantFactors || stressPayload.DominantFactors).map((item: unknown) => String(item || '').trim()).filter(Boolean)
          : [];
        const stressScore = clamp(Number(stressPayload?.stressScore ?? stressPayload?.StressScore) || 0, 0, 1);
        const bindingConfidence = clamp(Number(artifactLegField(leg, 'bindingConfidence', 'BindingConfidence')) || 0, 0, 1);
        const isBound = flowRefs.length > 0 || zoneRefs.length > 0 || affectedZoneRefs.length > 0 || spatialSemantics.length > 0;
        if (isBound) boundLegCount += 1;
        const isBottleneck = typeof bottleneckIndex === 'number' && bottleneckIndex === legIndex;
        if (isBottleneck) bottleneckCount += 1;
        const emphasis = isNarrativeActive ? 1 : 0.24;
        const lineWidth = (1.35 + stressScore * 2.25 + (isBottleneck ? 0.7 : 0)) * (isNarrativeActive ? 1 : 0.74);
        const lineOpacity = clamp((0.17 + bindingConfidence * 0.26 + stressScore * 0.18) * emphasis, 0.05, 0.62);
        const glowOpacity = clamp((0.028 + stressScore * 0.08) * (isNarrativeActive ? 1 : 0.2), 0.008, 0.11);

        features.push({
          type: 'Feature',
          id: `${artifactId}:leg:${legIndex}`,
          geometry: {
            type: 'LineString',
            coordinates: coords,
          },
          properties: {
            kind: 'leg',
            artifactId,
            displayName,
            legIndex,
            role: String((leg as any)?.role || '').trim(),
            supplierRef: artifactLegString(leg, 'supplierRef', 'SupplierRef'),
            customerRef: artifactLegString(leg, 'customerRef', 'CustomerRef'),
            productRef: artifactLegString(leg, 'productRef', 'ProductRef'),
            flowRefs,
            flowGeometryId: flowId || '',
            flowMode: flowMode || '',
            geometrySource,
            snappedToFlow: snapped,
            zoneRefs,
            affectedZoneRefs,
            spatialSemantics,
            dominantFactors,
            discoverySource: String(artifact.discoverySource || '').trim(),
            discoveryReason: String(artifact.discoveryReason || '').trim(),
            traceRole: String(artifact.traceRole || '').trim(),
            isPinned: Boolean(artifact.isPinned),
            stressScore,
            fragility: Number(stressPayload?.fragility ?? stressPayload?.Fragility) || 0,
            rerouteability: Number(stressPayload?.rerouteability ?? stressPayload?.Rerouteability) || 0,
            zonePressure: Number(stressPayload?.zonePressure ?? stressPayload?.ZonePressure) || 0,
            affectedZonePressure: Number(stressPayload?.affectedZonePressure ?? stressPayload?.AffectedZonePressure) || 0,
            recoveryLatencyHours: Number(stressPayload?.recoveryLatencyHours ?? stressPayload?.RecoveryLatencyHrs) || 0,
            bindingConfidence,
            isNarrativeActive,
            color: pathLegColor(stressScore, theme),
            lineWidth,
            lineOpacity,
            glowOpacity,
            isBottleneck,
          },
        } as GeoJSON.Feature);

        const meaningPoint = midpoint(coords);
        const meaningLabel = legMeaningLabel(
          artifactLegString(leg, 'productRef', 'ProductRef'),
          String(flowMode || '').trim(),
          flowRefs,
        );
        if (meaningPoint && meaningLabel) {
          features.push({
            type: 'Feature',
            id: `${artifactId}:leg:${legIndex}:meaning`,
            geometry: {
              type: 'Point',
              coordinates: meaningPoint,
            },
            properties: {
              kind: 'leg-meaning',
              artifactId,
              legIndex,
              meaningText: meaningLabel,
              color: pathLegColor(stressScore, theme),
              pathActive: isNarrativeActive ? 1 : 0,
              stressScore,
            },
          } as GeoJSON.Feature);
        }

        if (isBottleneck) {
          const focusPoint = midpoint(coords);
          if (focusPoint) {
            features.push({
              type: 'Feature',
              id: `${artifact.id}:leg:${legIndex}:bottleneck`,
              geometry: {
                type: 'Point',
                coordinates: focusPoint,
              },
              properties: {
                kind: 'bottleneck',
                artifactId,
                displayName,
                legIndex,
                stressScore,
                dominantFactors,
                bindingConfidence,
                isNarrativeActive,
                color: pathLegColor(Math.max(stressScore, 0.6), theme),
                circleOpacity: isNarrativeActive ? 0.74 : 0.22,
              },
            } as GeoJSON.Feature);
          }
        }
      }
    }

    features.push(...Array.from(legObjectFeatures.values()));

    return {
      featureCollection: {
        type: 'FeatureCollection',
        features,
      } as GeoJSON.FeatureCollection,
      markerObjects: Array.from(legObjectEntries.values()),
      stats: {
        artifactCount: artifacts.length,
        legCount,
        boundLegCount,
        bottleneckCount,
      } satisfies PathLegLayerStats,
    };
  }, [activeArtifactIds, artifacts, objects, theme]);

  useEffect(() => {
    setStats(data.stats);
  }, [data]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    void ensureMarkerImages(map, data.markerObjects, theme);
    const viewportFeatureCollection = buildViewportPathLegFeatureCollection(data.featureCollection, map);

    if (!map.getSource(SRC)) {
      map.addSource(SRC, {
        type: 'geojson',
        data: viewportFeatureCollection as any,
      });
    }

    if (!map.getLayer(GLOW_LAYER)) {
      map.addLayer({
        id: GLOW_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg']],
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['+', ['get', 'lineWidth'], 2.4],
          'line-opacity': ['case', ['boolean', ['get', 'isBottleneck'], false], ['*', ['get', 'glowOpacity'], 1.15], ['get', 'glowOpacity']],
          'line-blur': 0.95,
        },
      });
    }

    if (!map.getLayer(PINNED_LAYER)) {
      map.addLayer({
        id: PINNED_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg'], ['==', ['get', 'isPinned'], true]],
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['get', 'lineWidth'],
          'line-opacity': ['get', 'lineOpacity'],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [1.6, 1.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [2.6, 1.4]],
            ['literal', [1, 0]],
          ],
        },
      });
    }

    if (!map.getLayer(DISCOVERED_LAYER)) {
      map.addLayer({
        id: DISCOVERED_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg'], ['!=', ['get', 'isPinned'], true]],
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['*', ['get', 'lineWidth'], 0.92],
          'line-opacity': ['*', ['get', 'lineOpacity'], 0.82],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [0.9, 2.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [1.6, 2.2]],
            ['literal', [3.1, 1.8]],
          ],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_ANCHOR_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_ANCHOR_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg-object-anchor']],
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['coalesce', ['get', 'anchorColor'], theme === 'dark' ? '#94a3b8' : '#64748b'],
          'line-width': ['coalesce', ['get', 'anchorWidth'], 1],
          'line-opacity': ['coalesce', ['get', 'anchorOpacity'], 0.24],
        },
      });
    }

    if (!map.getLayer(BOTTLENECK_LAYER)) {
      map.addLayer({
        id: BOTTLENECK_LAYER,
        type: 'circle',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], 'bottleneck']],
        paint: {
          'circle-radius': ['+', 5, ['*', ['get', 'stressScore'], 7]],
          'circle-color': ['get', 'color'],
          'circle-stroke-color': theme === 'dark' ? '#f8fafc' : '#0f172a',
          'circle-stroke-width': 1.4,
          'circle-opacity': ['coalesce', ['get', 'circleOpacity'], 0.9],
        },
      });
    }

    if (!map.getLayer(LEG_DIRECTION_LAYER)) {
      map.addLayer({
        id: LEG_DIRECTION_LAYER,
        type: 'symbol',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg']],
        layout: {
          'symbol-placement': 'line',
          'symbol-spacing': [
            'interpolate', ['linear'], ['zoom'],
            3, 170,
            5, 140,
            7, 116,
            9, 92,
          ],
          'text-field': '»',
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            3, 13,
            5, 15,
            7, 17,
            9, 19,
          ],
          'text-keep-upright': false,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'text-color': ['get', 'color'],
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 0.9,
          'text-opacity': [
            'case',
            ['==', ['get', 'isNarrativeActive'], true],
            0.88,
            0.7,
          ],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_LAYER,
        type: 'symbol',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], 'leg-object']],
        layout: {
          'icon-image': ['get', 'iconKey'],
          'icon-size': 1,
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
        },
        paint: {
          'icon-opacity': [
            'case',
            ['==', ['get', 'pathActive'], 1],
            0.98,
            0.9,
          ],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_LABEL_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], 'leg-object']],
        layout: {
          'symbol-sort-key': ['get', 'labelPriority'],
          'text-field': ['get', 'labelText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            4, 11,
            6, 11.5,
            8, 12,
            10, 12.5,
          ],
          'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
          'text-radial-offset': 1.35,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'text-color': theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 1.7,
          'text-opacity': [
            'case',
            ['==', ['get', 'pathActive'], 1],
            1,
            0.9,
          ],
        },
      });
    }

    if (!map.getLayer(LEG_MEANING_LABEL_LAYER)) {
      map.addLayer({
        id: LEG_MEANING_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], 'leg-meaning']],
        layout: {
          'text-field': ['get', 'meaningText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            4, 10,
            6, 10.5,
            8, 11,
          ],
          'text-max-width': 12,
          'text-offset': [0, -0.2],
          'text-anchor': 'bottom',
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'text-color': [
            'case',
            ['==', ['get', 'pathActive'], 1],
            theme === 'dark' ? '#ecfeff' : 'rgba(8,47,73,0.96)',
            theme === 'dark' ? '#e2e8f0' : 'rgba(15,23,42,0.92)',
          ],
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.985)' : 'rgba(255,255,255,0.985)',
          'text-halo-width': [
            'case',
            ['==', ['get', 'pathActive'], 1],
            2.8,
            2.3,
          ],
          'text-halo-blur': 0.7,
          'text-opacity': [
            'case',
            ['!=', ['get', 'meaningVisible'], 1],
            0,
            ['==', ['get', 'pathActive'], 1],
            0.9,
            0.76,
          ],
        },
      });
    }

    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    src?.setData(viewportFeatureCollection as any);

    const visible = (enabled || activeArtifactIds.length > 0) && data.stats.legCount > 0;
    for (const layer of [GLOW_LAYER, PINNED_LAYER, DISCOVERED_LAYER, LEG_OBJECT_ANCHOR_LAYER, BOTTLENECK_LAYER, LEG_DIRECTION_LAYER, LEG_OBJECT_LAYER, LEG_OBJECT_LABEL_LAYER, LEG_MEANING_LABEL_LAYER]) {
      if (map.getLayer(layer)) {
        map.setLayoutProperty(layer, 'visibility', visible ? 'visible' : 'none');
      }
    }
  }, [map, isReady, styleEpoch, enabled, theme, data, activeArtifactIds]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const refreshViewportLabels = () => {
      const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
      if (!src) return;
      src.setData(buildViewportPathLegFeatureCollection(data.featureCollection, map) as any);
    };
    refreshViewportLabels();
    map.on('zoomend', refreshViewportLabels);
    map.on('moveend', refreshViewportLabels);
    return () => {
      map.off('zoomend', refreshViewportLabels);
      map.off('moveend', refreshViewportLabels);
    };
  }, [map, isReady, styleEpoch, data.featureCollection]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const onEnter = () => {
      map.getCanvas().style.cursor = 'pointer';
    };
    const onLeave = () => {
      map.getCanvas().style.cursor = '';
      popupRef.current?.remove();
      popupRef.current = null;
    };
    const onClick = (event: maplibregl.MapLayerMouseEvent) => {
      const feature = event.features?.[0];
      const props = (feature?.properties || {}) as Record<string, any>;
      if (!feature || props.kind !== 'leg') return;
      popupRef.current?.remove();
      const parseList = (value: unknown): string[] => {
        if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean);
        if (typeof value !== 'string') return [];
        const trimmed = value.trim();
        if (!trimmed) return [];
        if (trimmed.startsWith('[')) {
          try {
            const parsed = JSON.parse(trimmed);
            return Array.isArray(parsed) ? parsed.map((item) => String(item || '').trim()).filter(Boolean) : [];
          } catch {
            return [];
          }
        }
        return trimmed.split(',').map((item) => item.trim()).filter(Boolean);
      };
      const dominantFactors = parseList(props.dominantFactors);
      const flowRefs = parseList(props.flowRefs);
      const zoneRefs = parseList(props.zoneRefs);
      const affectedZoneRefs = parseList(props.affectedZoneRefs);
      const spatialSemantics = parseList(props.spatialSemantics);
      const discoveryReason = String(props.discoveryReason || '').trim();
      const traceRole = String(props.traceRole || '').trim();
      const discoverySource = String(props.discoverySource || '').trim();

      const html = `
        <div style="display:flex;flex-direction:column;gap:6px;min-width:240px;max-width:320px;">
          <div style="font-size:12px;font-weight:800;">${escapeHtml(String(props.displayName || 'Supply path'))}</div>
          <div style="font-size:11px;opacity:0.78;">${props.isPinned ? 'Pinned path' : 'Discovered path'} • Leg ${escapeHtml(String(props.legIndex ?? ''))}${props.isBottleneck ? ' • bottleneck' : ''}</div>
          <div style="font-size:11px;display:flex;flex-wrap:wrap;gap:6px;">
            <span>stress ${escapeHtml(toFixedSafe(Number(props.stressScore), 2))}</span>
            <span>bind ${escapeHtml(toFixedSafe(Number(props.bindingConfidence) * 100, 0))}%</span>
            <span>recovery ${escapeHtml(toFixedSafe(Number(props.recoveryLatencyHours), 0))}h</span>
          </div>
          ${traceRole ? `<div style="font-size:11px;"><strong>Trace role:</strong> ${escapeHtml(traceRole.replace(/_/g, ' '))}</div>` : ''}
          ${discoveryReason ? `<div style="font-size:11px;"><strong>Why shown:</strong> ${escapeHtml(discoveryReason)}</div>` : ''}
          ${!discoveryReason && discoverySource ? `<div style="font-size:11px;"><strong>Source:</strong> ${escapeHtml(discoverySource)}</div>` : ''}
          ${typeof props.flowMode === 'string' && props.flowMode ? `<div style="font-size:11px;"><strong>Mode:</strong> ${escapeHtml(String(props.flowMode))}${props.flowGeometryId ? ` • ${escapeHtml(String(props.flowGeometryId))}` : ''}</div>` : ''}
          ${typeof props.geometrySource === 'string' && props.geometrySource ? `<div style="font-size:11px;"><strong>Geometry:</strong> ${escapeHtml(String(props.geometrySource))}${props.snappedToFlow ? ' • snapped' : ''}</div>` : ''}
          ${spatialSemantics.length > 0 ? `<div style="font-size:11px;"><strong>Spatial:</strong> ${escapeHtml(spatialSemantics.join(', '))}</div>` : ''}
          ${flowRefs.length > 0 ? `<div style="font-size:11px;"><strong>Flows:</strong> ${escapeHtml(flowRefs.join(', '))}</div>` : ''}
          ${zoneRefs.length > 0 ? `<div style="font-size:11px;"><strong>Zones:</strong> ${escapeHtml(zoneRefs.join(', '))}</div>` : ''}
          ${affectedZoneRefs.length > 0 ? `<div style="font-size:11px;"><strong>Affected:</strong> ${escapeHtml(affectedZoneRefs.join(', '))}</div>` : ''}
          ${dominantFactors.length > 0 ? `<div style="font-size:11px;"><strong>Factors:</strong> ${escapeHtml(dominantFactors.join(', '))}</div>` : ''}
        </div>
      `;
      popupRef.current = new maplibregl.Popup({
        closeButton: false,
        closeOnClick: false,
        offset: 12,
      })
        .setLngLat(event.lngLat)
        .setHTML(html)
        .addTo(map);
      stylePathLegPopup(popupRef.current, theme);
    };

    map.on('mouseenter', PINNED_LAYER, onEnter);
    map.on('mouseleave', PINNED_LAYER, onLeave);
    map.on('click', PINNED_LAYER, onClick);
    map.on('mouseenter', DISCOVERED_LAYER, onEnter);
    map.on('mouseleave', DISCOVERED_LAYER, onLeave);
    map.on('click', DISCOVERED_LAYER, onClick);
    return () => {
      map.off('mouseenter', PINNED_LAYER, onEnter);
      map.off('mouseleave', PINNED_LAYER, onLeave);
      map.off('click', PINNED_LAYER, onClick);
      map.off('mouseenter', DISCOVERED_LAYER, onEnter);
      map.off('mouseleave', DISCOVERED_LAYER, onLeave);
      map.off('click', DISCOVERED_LAYER, onClick);
      popupRef.current?.remove();
      popupRef.current = null;
    };
  }, [map, isReady, styleEpoch, theme]);

  return stats;
}
