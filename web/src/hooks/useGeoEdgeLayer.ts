import { useEffect, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import { api } from '../lib/api';
import type {
  SupplyChainLegRiskOverlay,
  SupplyChainObjectRiskOverlay,
  SupplyChainOverlayMetric,
  SupplyChainPathRiskOverlay,
} from '../lib/api';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { buildPointFeature, ensureMarkerImages, GEO_MARKER_LAYER_ANCHOR } from './useGeoMarkerLayer';
import { MAP_TEXT_FONT_STACK } from './useMapInstance';
import { humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'geo-edges';
const ENDPOINT_SRC = 'geo-edge-endpoints';
const GLOW_LAYER = 'geo-edges-glow';
const HOP_3_LAYER = 'geo-edges-hop-3';
const HOP_2_LAYER = 'geo-edges-hop-2';
const HOP_1_LAYER = 'geo-edges-hop-1';
const SUPPLY_HOP_3_LAYER = 'geo-edges-supply-hop-3';
const SUPPLY_HOP_2_LAYER = 'geo-edges-supply-hop-2';
const SUPPLY_HOP_1_LAYER = 'geo-edges-supply-hop-1';
const LABEL_LAYER = 'geo-edges-label';
const SUPPLY_LABEL_LAYER = 'geo-edges-supply-label';
const CORRIDOR_FIELD_LAYER = 'geo-edges-corridor-field';
const CORRIDOR_FIELD_ACCENT_LAYER = 'geo-edges-corridor-field-accent';
const ENDPOINT_POINTS_LAYER = 'geo-edge-endpoints-points';
const ENDPOINT_LABEL_LAYER = 'geo-edge-endpoints-label';
const ENDPOINT_LABEL_CHIP_DARK = 'geo-edge-endpoint-chip-dark';
const ENDPOINT_LABEL_CHIP_LIGHT = 'geo-edge-endpoint-chip-light';
const SUPPLY_LABEL_CHIP_DARK = 'geo-edge-supply-chip-dark';
const SUPPLY_LABEL_CHIP_LIGHT = 'geo-edge-supply-chip-light';
const MAX_HOPS = 3;

/** Layer IDs that should prevent map click from clearing selection (fog, edges, endpoints). */
export const GEO_EDGE_INTERACTIVE_LAYERS = [
  CORRIDOR_FIELD_LAYER,
  CORRIDOR_FIELD_ACCENT_LAYER,
  ENDPOINT_POINTS_LAYER,
  ENDPOINT_LABEL_LAYER,
  HOP_1_LAYER,
  HOP_2_LAYER,
  HOP_3_LAYER,
  SUPPLY_HOP_1_LAYER,
  SUPPLY_HOP_2_LAYER,
  SUPPLY_HOP_3_LAYER,
  GLOW_LAYER,
  SUPPLY_LABEL_LAYER,
  LABEL_LAYER,
] as const;

type ThemeMode = 'dark' | 'light';
type EdgeFamily = 'supply' | 'control' | 'influence' | 'location' | 'market' | 'risk' | 'social' | 'other';
type EdgeVisualKind = 'primary' | 'indirect' | 'cluster';
type LonLat = [number, number];
type LocalPoint = { x: number; y: number };
type EndpointLabelTier = 'selected' | 'direct' | 'path' | 'context' | 'propagation';
type EndpointLabelMeta = {
  labelText: string;
  priority: number;
  visible: boolean;
  chip: boolean;
  subdued: boolean;
  strong: boolean;
  tier: EndpointLabelTier;
  narrativeKeys: string[];
};
type EndpointSourceEntry = {
  key: string;
  object: GeoObject;
  displayPoint?: [number, number];
  anchorPoint?: [number, number];
};
type EndpointPlacedEntry = {
  key: string;
  object: GeoObject;
  displayPoint: [number, number];
  anchorPoint: [number, number];
};
type EndpointRenderState = {
  placedEntries: EndpointPlacedEntry[];
  tierByKey: Map<string, EndpointLabelTier>;
  narrativeKeysByEndpointKey: Map<string, string[]>;
  forcedVisibleKeys: Set<string>;
};
type BushOrigin = {
  key: string;
  point: [number, number];
};
type BushGroup = {
  id: string;
  entries: EndpointPlacedEntry[];
  origin: BushOrigin;
};
type FragmentTracePayload = Awaited<ReturnType<typeof api.getFragmentTrace>>;

export interface GeoEdgeTraceSummary {
  objectRiskOverlay?: SupplyChainObjectRiskOverlay | null;
  summary: string;
  whyItMatters: Array<{ title: string; body: string }>;
  directRelations: Array<{
    narrativeKey: string;
    edgeId?: string;
    relatedFragmentId: string;
    relatedLabel: string;
    relatedType?: string;
    relatedCanonicalPath?: string;
    type: string;
    family: EdgeFamily;
    semanticDirection?: string;
    businessMeaning?: string;
    whyRelevant?: string;
  }>;
  contextRelations: Array<{
    narrativeKey: string;
    edgeId?: string;
    relatedFragmentId: string;
    relatedLabel: string;
    relatedType?: string;
    relatedCanonicalPath?: string;
    type: string;
    family: EdgeFamily;
    semanticDirection?: string;
    businessMeaning?: string;
    whyRelevant?: string;
  }>;
  supplyGroups: Array<{
    id: string;
    narrativeKey: string;
    direction: 'inbound' | 'outbound' | 'context';
    productRef?: string;
    productLabel: string;
    summary: string;
    routeCount: number;
    stressedRouteCount: number;
    pinnedRouteCount: number;
    maxStressScore: number;
    hasBottleneck: boolean;
    paths: Array<{
      id: string;
      narrativeKey: string;
      title: string;
      counterpartLabel: string;
      direction: 'inbound' | 'outbound' | 'context';
      destinationLabel: string;
      role: string;
      reason: string;
      stressScore: number;
      legCount: number;
      isPinned: boolean;
      hasBottleneck: boolean;
      primaryMechanism?: string;
      riskTag?: string;
    }>;
  }>;
  propagationNodeCount: number;
  propagationEdgeCount: number;
  visibleLineCount: number;
  directCount: number;
  contextCount: number;
  supplyPathCount: number;
  stressedSupplyPathCount: number;
}

export interface GeoEdgeSelectionExportData {
  exportedAt: string;
  selectedObject: GeoObject;
  requestedHops: number;
  lensId?: string;
  trace: FragmentTracePayload;
  traceSummary: GeoEdgeTraceSummary | null;
  discoveredObjects: GeoObject[];
  relatedPathArtifacts: MaterializedArtifact[];
  directRelations: ExplainRelation[];
  contextRelations: ExplainRelation[];
  visibleNodeIds: string[];
  visiblePathRefs: string[];
  mutedObjectIds: string[];
  highlightedGatewayObjectIds: string[];
  renderedEdgeFeatureCollection: GeoJSON.FeatureCollection;
  renderedEndpointFeatureCollection: GeoJSON.FeatureCollection;
}

interface ExplainRelation {
  edgeId?: string;
  sourceId: string;
  targetId: string;
  relatedFragmentId: string;
  relatedLabel: string;
  relatedType?: string;
  relatedCanonicalPath?: string;
  relatedCoordinates?: { lat: number; lon: number };
  type: string;
  family: EdgeFamily;
  semanticDirection?: string;
  businessMeaning?: string;
  whyRelevant?: string;
  priority?: number;
  criticality?: number;
}

type TraceObjectInfo = Pick<GeoObject, 'id' | 'canonicalPath' | 'type' | 'label' | 'facilityKind' | 'description' | 'status' | 'inScope'> & {
  coordinates?: { lat: number; lon: number };
};

const SUPPLY_BUNDLE_MIN_DISTANCE_KM = 1400;
const RADIAL_BUNDLE_MIN_DISTANCE_KM = 1400;
const SUPPLY_BUNDLE_MIN_ROUTES = 3;
const SUPPLY_BUNDLE_AZIMUTH_DEG = 20;
const SUPPLY_BUNDLE_LOCAL_CELL_DEG = 3.5;
const SUPPLY_BUNDLE_MAX_LOCAL_SPREAD_KM = 680;
const SUPPLY_BUNDLE_MAX_REMOTE_SPREAD_KM = 1800;
const SUPPLY_BUNDLE_MAX_PRESERVE_ENDPOINT_DISTANCE_KM = 2600;
const CORRIDOR_SPATIAL_CLUSTER_LINK_KM = 900;
const ENDPOINT_MARKER_SIZE_PX = 28;
const ENDPOINT_MARKER_PADDING_PX = 4;
const ENDPOINT_LABEL_OFFSET_X_PX = 20;
const ENDPOINT_LABEL_HEIGHT_PX = 15;
const ENDPOINT_LABEL_CHAR_WIDTH_PX = 6.4;
const ENDPOINT_LABEL_MAX_WIDTH_PX = 220;
const DIRECT_LEGEND_COLOR = '#60a5fa';
const CONTEXT_LEGEND_COLOR = '#2dd4bf';
const SUPPLY_LEGEND_COLOR = '#f59e0b';
const PROPAGATION_LEGEND_COLOR = '#94a3b8';

function sanitizeOverlayToken(value?: string): string {
  const normalized = String(value || '').trim();
  if (!normalized) return '';
  if (['<nil>', '<Nil>', 'nil', 'null', 'undefined'].includes(normalized)) return '';
  return normalized;
}

function mechanismLabel(value?: string): string {
  const normalized = sanitizeOverlayToken(value);
  if (!normalized) return '';
  return normalized
    .replace(/[_-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function mechanismShortTag(value?: string): string {
  const normalized = sanitizeOverlayToken(value).toLowerCase();
  if (!normalized) return '';
  if (normalized.includes('inventory')) return 'Inventory';
  if (normalized.includes('lead_time') || normalized.includes('lead time')) return 'Lead time';
  if (normalized.includes('substitut')) return 'Substitution';
  if (normalized.includes('sole_source') || normalized.includes('sole source')) return 'Sole source';
  if (normalized.includes('route') || normalized.includes('bottleneck') || normalized.includes('reroute')) return 'Bottleneck';
  if (normalized.includes('customs')) return 'Customs';
  if (normalized.includes('sanction')) return 'Sanctions';
  if (normalized.includes('export')) return 'Export';
  if (normalized.includes('service')) return 'Service';
  if (normalized.includes('control')) return 'Controls';
  return mechanismLabel(normalized);
}

function primaryMetricLabel(metrics?: SupplyChainOverlayMetric[] | null): string {
  const first = Array.isArray(metrics) ? metrics.find((item) => sanitizeOverlayToken(String(item?.label || item?.key || ''))) : null;
  if (!first) return '';
  return sanitizeOverlayToken(String(first.label || mechanismLabel(first.key) || first.key || ''));
}

function primaryRiskTag(pathOverlay?: SupplyChainPathRiskOverlay | null, legOverlay?: SupplyChainLegRiskOverlay | null): string {
  const mechanism = sanitizeOverlayToken(String(legOverlay?.primaryMechanism || pathOverlay?.primaryMechanism || ''));
  if (mechanism) return mechanismShortTag(mechanism);
  const metric = primaryMetricLabel(legOverlay?.metrics || pathOverlay?.topMetrics || []);
  return metric ? compactLabel(metric, 16) : '';
}

function supplyMechanismColor(theme: ThemeMode, kind: EdgeVisualKind, mechanism?: string): string {
  const normalized = sanitizeOverlayToken(mechanism).toLowerCase();
  if (normalized.includes('route') || normalized.includes('bottleneck') || normalized.includes('reroute')) {
    return theme === 'dark' ? '#fb923c' : '#c2410c';
  }
  if (normalized.includes('inventory') || normalized.includes('lead_time') || normalized.includes('lead time')) {
    return theme === 'dark' ? '#fbbf24' : '#b45309';
  }
  if (normalized.includes('substitut') || normalized.includes('sole_source') || normalized.includes('sole source')) {
    return theme === 'dark' ? '#fb7185' : '#be123c';
  }
  if (normalized.includes('customs') || normalized.includes('sanction') || normalized.includes('export') || normalized.includes('control')) {
    return theme === 'dark' ? '#93c5fd' : '#2563eb';
  }
  if (normalized.includes('service')) {
    return theme === 'dark' ? '#5eead4' : '#0f766e';
  }
  return supplyStrokeColor(theme, kind);
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
  return sanitizeOverlayToken(String(artifactLegField(leg, ...keys) || ''));
}

function artifactLegStringList(leg: any, ...keys: string[]): string[] {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (!Array.isArray(value)) return [];
  return value.map((item) => sanitizeOverlayToken(String(item || ''))).filter(Boolean);
}

function artifactLegNumber(leg: any, ...keys: string[]): number | null {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (typeof value !== 'number' || !Number.isFinite(value)) return null;
  return value;
}

function directStrokeColor(): string {
  return DIRECT_LEGEND_COLOR;
}

function contextStrokeColor(): string {
  return CONTEXT_LEGEND_COLOR;
}

function supplyLegendColor(): string {
  return SUPPLY_LEGEND_COLOR;
}

function propagationLegendColor(): string {
  return PROPAGATION_LEGEND_COLOR;
}

function endpointChipImageId(theme: ThemeMode): string {
  return theme === 'dark' ? ENDPOINT_LABEL_CHIP_DARK : ENDPOINT_LABEL_CHIP_LIGHT;
}

function supplyLabelChipImageId(theme: ThemeMode): string {
  return theme === 'dark' ? SUPPLY_LABEL_CHIP_DARK : SUPPLY_LABEL_CHIP_LIGHT;
}

function drawRoundedRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number,
): void {
  const r = Math.max(0, Math.min(radius, width / 2, height / 2));
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + width - r, y);
  ctx.quadraticCurveTo(x + width, y, x + width, y + r);
  ctx.lineTo(x + width, y + height - r);
  ctx.quadraticCurveTo(x + width, y + height, x + width - r, y + height);
  ctx.lineTo(x + r, y + height);
  ctx.quadraticCurveTo(x, y + height, x, y + height - r);
  ctx.lineTo(x, y + r);
  ctx.quadraticCurveTo(x, y, x + r, y);
  ctx.closePath();
}

function renderEndpointChipImage(theme: ThemeMode): {
  width: number;
  height: number;
  data: Uint8ClampedArray;
  pixelRatio: number;
  stretchX: Array<[number, number]>;
  stretchY: Array<[number, number]>;
  content: [number, number, number, number];
} {
  const pixelRatio = 2;
  const width = 96;
  const height = 40;
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d')!;
  const fill = theme === 'dark' ? 'rgba(6, 10, 16, 0.94)' : 'rgba(255, 255, 255, 0.94)';
  const stroke = theme === 'dark' ? 'rgba(148, 163, 184, 0.28)' : 'rgba(15, 23, 42, 0.16)';
  const shadow = theme === 'dark' ? 'rgba(0, 0, 0, 0.44)' : 'rgba(15, 23, 42, 0.14)';

  ctx.clearRect(0, 0, width, height);
  ctx.shadowColor = shadow;
  ctx.shadowBlur = 8;
  ctx.shadowOffsetY = 2;
  drawRoundedRect(ctx, 2, 3, width - 4, height - 6, 12);
  ctx.fillStyle = fill;
  ctx.fill();
  ctx.shadowColor = 'transparent';
  ctx.shadowBlur = 0;
  ctx.shadowOffsetY = 0;
  ctx.lineWidth = 1;
  ctx.strokeStyle = stroke;
  ctx.stroke();

  const imageData = ctx.getImageData(0, 0, width, height);
  return {
    width,
    height,
    data: imageData.data,
    pixelRatio,
    stretchX: [[20, width - 20]],
    stretchY: [[14, height - 14]],
    content: [18, 10, width - 18, height - 10],
  };
}

function renderSupplyLabelChipImage(theme: ThemeMode): {
  width: number;
  height: number;
  data: Uint8ClampedArray;
  pixelRatio: number;
  stretchX: Array<[number, number]>;
  stretchY: Array<[number, number]>;
  content: [number, number, number, number];
} {
  const pixelRatio = 2;
  const width = 112;
  const height = 44;
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d')!;
  const fill = theme === 'dark' ? 'rgba(9, 14, 22, 0.84)' : 'rgba(255, 252, 247, 0.94)';
  const stroke = theme === 'dark' ? 'rgba(251, 146, 60, 0.24)' : 'rgba(194, 65, 12, 0.18)';
  const shadow = theme === 'dark' ? 'rgba(0, 0, 0, 0.22)' : 'rgba(120, 53, 15, 0.08)';

  ctx.clearRect(0, 0, width, height);
  ctx.shadowColor = shadow;
  ctx.shadowBlur = 6;
  ctx.shadowOffsetY = 2;
  drawRoundedRect(ctx, 2, 4, width - 4, height - 8, 14);
  ctx.fillStyle = fill;
  ctx.fill();
  ctx.shadowColor = 'transparent';
  ctx.shadowBlur = 0;
  ctx.shadowOffsetY = 0;
  ctx.lineWidth = 1.2;
  ctx.strokeStyle = stroke;
  ctx.stroke();

  const imageData = ctx.getImageData(0, 0, width, height);
  return {
    width,
    height,
    data: imageData.data,
    pixelRatio,
    stretchX: [[24, width - 24]],
    stretchY: [[16, height - 16]],
    content: [20, 11, width - 20, height - 11],
  };
}

function ensureEndpointChipImage(map: maplibregl.Map, theme: ThemeMode): void {
  const id = endpointChipImageId(theme);
  if (map.hasImage(id)) return;
  const image = renderEndpointChipImage(theme);
  map.addImage(id, image as any);
}

function ensureSupplyLabelChipImage(map: maplibregl.Map, theme: ThemeMode): void {
  const id = supplyLabelChipImageId(theme);
  if (map.hasImage(id)) return;
  const image = renderSupplyLabelChipImage(theme);
  map.addImage(id, image as any);
}

function toSoftHex(hex: string, blend: number): string {
  const match = hex.match(/^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i);
  if (!match) return hex;
  const next = [match[1], match[2], match[3]].map((chunk) => {
    const channel = parseInt(chunk, 16);
    const value = Math.round(channel * (1 - blend) + 255 * blend);
    return value.toString(16).padStart(2, '0');
  });
  return `#${next.join('')}`;
}

function normalizeEdgeType(value: unknown): string {
  return String(value || '').trim().toUpperCase();
}

function stablePairKey(a: string, b: string): string {
  return a < b ? `${a}::${b}` : `${b}::${a}`;
}

function geoCellKey(point: [number, number], cellDeg = SUPPLY_BUNDLE_LOCAL_CELL_DEG): string {
  const lonBucket = Math.floor((point[0] + 180) / cellDeg);
  const latBucket = Math.floor((point[1] + 90) / cellDeg);
  return `${lonBucket}:${latBucket}`;
}

function hashString(value: string): number {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return hash;
}

function shouldSpatiallySplitCorridor(channel: string): boolean {
  return channel === 'trace' || channel === 'propagation';
}

function featureRepresentativePoint(feature: GeoJSON.Feature): [number, number] | null {
  if (feature.geometry?.type === 'Point') {
    const coords = feature.geometry.coordinates;
    if (Array.isArray(coords) && coords.length >= 2) return [Number(coords[0]), Number(coords[1])];
    return null;
  }
  if (feature.geometry?.type === 'LineString') {
    const coords = feature.geometry.coordinates as [number, number][];
    if (coords.length === 0) return null;
    return linePointAt(coords, 0.72);
  }
  return null;
}

function resolveEndpointObject(
  endpointKey: string,
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
): GeoObject | null {
  if (endpointKey.startsWith('node:')) return traceObjectToGeoObject(objectById.get(endpointKey.slice(5)));
  if (endpointKey.startsWith('path:')) return traceObjectToGeoObject(objectByPath.get(endpointKey.slice(5)));
  return null;
}

function clusterEndpointKeysByDistance(
  entries: Array<{ key: string; point: [number, number] }>,
  maxLinkKm: number,
): Array<Array<{ key: string; point: [number, number] }>> {
  return clusterPointEntriesByDistance(entries, maxLinkKm);
}

function repartitionSpatialCorridors(
  features: GeoJSON.Feature[],
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
  centerPoint: [number, number] | null,
): GeoJSON.Feature[] {
  type CorridorBucket = {
    corridorId: string;
    channel: string;
    features: GeoJSON.Feature[];
    endpointEntries: Map<string, { key: string; point: [number, number] }>;
  };

  const buckets = new Map<string, CorridorBucket>();
  for (const feature of features) {
    const corridorId = String(feature.properties?.corridorId || '').trim();
    if (!corridorId) continue;
    if (!buckets.has(corridorId)) {
      buckets.set(corridorId, {
        corridorId,
        channel: String(feature.properties?.channel || '').trim(),
        features: [],
        endpointEntries: new Map(),
      });
    }
    const bucket = buckets.get(corridorId)!;
    if (!bucket.channel) bucket.channel = String(feature.properties?.channel || '').trim();
    bucket.features.push(feature);
    for (const endpointKey of featureBadgeEndpointRefs(feature)) {
      if (bucket.endpointEntries.has(endpointKey)) continue;
      const object = resolveEndpointObject(endpointKey, objectById, objectByPath);
      if (!object?.coordinates) continue;
      bucket.endpointEntries.set(endpointKey, {
        key: endpointKey,
        point: [object.coordinates.lon, object.coordinates.lat],
      });
    }
  }

  const reassigned: GeoJSON.Feature[] = [];
  const rewriteFeatureForCluster = (
    feature: GeoJSON.Feature,
    clusterCorridorId: string,
    suffix: string,
    clusterEndpointKeys?: Set<string>,
  ): GeoJSON.Feature => {
    const filteredBadgeRefs = clusterEndpointKeys
      ? featureBadgeEndpointRefs(feature).filter((key) => clusterEndpointKeys.has(key))
      : featureBadgeEndpointRefs(feature);
    const badgeEndpointNodeIds = filteredBadgeRefs
      .filter((key) => key.startsWith('node:'))
      .map((key) => key.slice(5));
    const badgeEndpointPathRefs = filteredBadgeRefs
      .filter((key) => key.startsWith('path:'))
      .map((key) => key.slice(5));
    return {
      ...feature,
      id: feature.id == null ? `${clusterCorridorId}:${suffix}` : `${String(feature.id)}:${suffix}`,
      properties: {
        ...(feature.properties || {}),
        corridorId: clusterCorridorId,
        corridorBaseId: String(feature.properties?.corridorId || '').trim(),
        ...(filteredBadgeRefs.length > 0
          ? {
              badgeEndpointNodeIds,
              badgeEndpointPathRefs,
            }
          : {}),
      },
    };
  };

  for (const feature of features) {
    const corridorId = String(feature.properties?.corridorId || '').trim();
    if (!corridorId || !buckets.has(corridorId)) {
      reassigned.push(feature);
      continue;
    }
    const bucket = buckets.get(corridorId)!;
    if (!shouldSpatiallySplitCorridor(bucket.channel) || bucket.endpointEntries.size < 2) {
      reassigned.push(feature);
      continue;
    }

    const clusters = clusterEndpointKeysByDistance(
      Array.from(bucket.endpointEntries.values()),
      CORRIDOR_SPATIAL_CLUSTER_LINK_KM,
    );
    if (clusters.length < 2) {
      reassigned.push(feature);
      continue;
    }

    const clusterInfo = clusters.map((cluster, index) => ({
      corridorId: `${bucket.corridorId}:geo:${index}`,
      endpointKeys: new Set(cluster.map((entry) => entry.key)),
      centroid: averagePoint(cluster.map((entry) => entry.point)),
    }));
    const endpointRefs = featureBadgeEndpointRefs(feature).filter((key) => bucket.endpointEntries.has(key));

    if (endpointRefs.length > 0) {
      const touchedClusters = clusterInfo.filter((candidate) => endpointRefs.some((key) => candidate.endpointKeys.has(key)));
      if (feature.geometry?.type === 'LineString' && touchedClusters.length > 1) {
        touchedClusters.forEach((cluster, index) => {
          reassigned.push(rewriteFeatureForCluster(feature, cluster.corridorId, `cluster-shared-${index}`, cluster.endpointKeys));
        });
        continue;
      }
      let targetKey = endpointRefs[0];
      if (centerPoint && endpointRefs.length > 1) {
        targetKey = [...endpointRefs].sort((left, right) => {
          const leftPoint = bucket.endpointEntries.get(left)?.point || centerPoint;
          const rightPoint = bucket.endpointEntries.get(right)?.point || centerPoint;
          return (
            approximateDistanceKm(rightPoint[0], rightPoint[1], centerPoint[0], centerPoint[1])
            - approximateDistanceKm(leftPoint[0], leftPoint[1], centerPoint[0], centerPoint[1])
          );
        })[0];
      }
      const cluster = clusterInfo.find((candidate) => candidate.endpointKeys.has(targetKey))
        || touchedClusters[0]
        || clusterInfo[0];
      reassigned.push(rewriteFeatureForCluster(feature, cluster.corridorId, `cluster-${hashString(targetKey)}`, cluster.endpointKeys));
      continue;
    }

    const representativePoint = featureRepresentativePoint(feature);
    if (feature.geometry?.type === 'LineString') {
      clusterInfo.forEach((cluster, index) => {
        reassigned.push(rewriteFeatureForCluster(feature, cluster.corridorId, `shared-${index}`, cluster.endpointKeys));
      });
      continue;
    }
    if (!representativePoint) {
      reassigned.push(rewriteFeatureForCluster(feature, clusterInfo[0].corridorId, 'cluster-0', clusterInfo[0].endpointKeys));
      continue;
    }
    const nearestCluster = [...clusterInfo].sort((left, right) => (
      approximateDistanceKm(representativePoint[0], representativePoint[1], left.centroid[0], left.centroid[1])
      - approximateDistanceKm(representativePoint[0], representativePoint[1], right.centroid[0], right.centroid[1])
    ))[0];
    reassigned.push(rewriteFeatureForCluster(feature, nearestCluster.corridorId, `cluster-${hashString(nearestCluster.corridorId)}`, nearestCluster.endpointKeys));
  }

  return reassigned;
}

function edgeFamily(edgeOrType: { type?: string; family?: EdgeFamily } | string): EdgeFamily {
  if (typeof edgeOrType !== 'string' && edgeOrType?.family) return edgeOrType.family;
  const type = typeof edgeOrType === 'string' ? edgeOrType : (edgeOrType.type || '');
  if (['SUPPLIES_TO', 'DEPENDS_ON', 'PRODUCES', 'EXTRACTS_FROM', 'REQUIRES', 'USES', 'SUPPORTS'].includes(type)) {
    return 'supply';
  }
  if (['CONTROLS', 'MANAGES', 'OWNS', 'INVESTS_IN', 'BOARD_MEMBER_OF', 'EMPLOYED_BY', 'FOUNDED'].includes(type)) {
    return 'control';
  }
  if (['REGULATES', 'ASSURES', 'AUDITED_BY', 'INFLUENCES', 'AFFECTS', 'IMPACTS'].includes(type)) {
    return 'influence';
  }
  if (['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN'].includes(type)) {
    return 'location';
  }
  if (['PARTICIPATES_IN', 'TRADED_IN', 'HAS_MARKET_SEGMENT', 'HAS_SUBMARKET'].includes(type)) {
    return 'market';
  }
  if (['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES'].includes(type)) {
    return 'risk';
  }
  if (['RELATED_TO', 'RELATIVE_OF', 'PARTNERS_WITH', 'COOPERATES_WITH'].includes(type)) {
    return 'social';
  }
  return 'other';
}

function humanizeEdgeType(edgeType: string): string {
  return edgeType
    .toLowerCase()
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function relationStrokeColor(theme: ThemeMode, family: EdgeFamily, semanticDirection?: string, emphasize = false): string {
  void theme;
  void family;
  void semanticDirection;
  return emphasize ? directStrokeColor() : contextStrokeColor();
}

function directRelationLineColor(theme: ThemeMode): string {
  return theme === 'dark' ? '#8faed8' : '#5d83b8';
}

function buildDirectRelationVisual(
  theme: ThemeMode,
  distanceKm: number,
  fanoutCount: number,
  bundleSize: number,
): {
  color: string;
  baseOpacity: number;
  baseWidth: number;
  glowOpacity: number;
  distanceFactor: number;
  fanoutFactor: number;
  bundleFactor: number;
  longHaul: boolean;
  denseFanout: boolean;
} {
  const distanceFactor = clamp(1 - Math.min(distanceKm, 4200) / 4200 * 0.24, 0.76, 1);
  const fanoutFactor = clamp(1 - Math.log2(fanoutCount + 1) * 0.11, 0.56, 1);
  const bundleFactor = clamp(1 - Math.log2(bundleSize + 1) * 0.08, 0.78, 1);
  const widthDistanceFactor = distanceKm >= 2400 ? 0.84 : distanceKm >= 1200 ? 0.9 : distanceKm >= 500 ? 0.96 : 1;
  const opacityFactor = distanceFactor * fanoutFactor * bundleFactor;
  return {
    color: directRelationLineColor(theme),
    baseOpacity: clamp((theme === 'dark' ? 0.78 : 0.7) * opacityFactor, 0.3, 0.8),
    baseWidth: clamp(2.28 * widthDistanceFactor * (0.94 + fanoutFactor * 0.12), 1.18, 2.4),
    glowOpacity: clamp((theme === 'dark' ? 0.042 : 0.034) * opacityFactor, 0.01, 0.055),
    distanceFactor,
    fanoutFactor,
    bundleFactor,
    longHaul: distanceKm >= 1200,
    denseFanout: fanoutCount >= 9,
  };
}

function buildSupplyPathVisual(
  theme: ThemeMode,
  kind: EdgeVisualKind,
  distanceKm: number,
  fanoutCount: number,
  bundleSize: number,
  riskScore = 0,
): {
  color: string;
  baseOpacity: number;
  baseWidth: number;
  glowOpacity: number;
  distanceFactor: number;
  fanoutFactor: number;
  bundleFactor: number;
  longHaul: boolean;
  denseFanout: boolean;
} {
  const distanceFactor = clamp(1 - Math.min(distanceKm, 5600) / 5600 * 0.22, 0.78, 1);
  const fanoutFactor = clamp(1 - Math.log2(fanoutCount + 1) * 0.12, 0.5, 1);
  const bundleFactor = clamp(1 - Math.log2(bundleSize + 1) * 0.09, 0.74, 1);
  const widthDistanceFactor = distanceKm >= 3200 ? 0.82 : distanceKm >= 1600 ? 0.9 : distanceKm >= 600 ? 0.96 : 1;
  const kindOpacityBase = kind === 'primary' ? (theme === 'dark' ? 0.66 : 0.58) : kind === 'indirect' ? (theme === 'dark' ? 0.54 : 0.48) : (theme === 'dark' ? 0.42 : 0.36);
  const kindWidthBase = kind === 'primary' ? 2.05 : kind === 'indirect' ? 1.72 : 1.36;
  const kindGlowBase = kind === 'primary' ? (theme === 'dark' ? 0.034 : 0.028) : kind === 'indirect' ? 0.024 : 0.014;
  const riskFactor = clamp(0.88 + clamp(riskScore, 0, 1)*0.54, 0.88, 1.42);
  const opacityFactor = distanceFactor * fanoutFactor * bundleFactor;
  return {
    color: supplyStrokeColor(theme, kind),
    baseOpacity: clamp(kindOpacityBase * opacityFactor * clamp(0.94+riskScore*0.18, 0.94, 1.12), 0.18, 0.78),
    baseWidth: clamp(kindWidthBase * widthDistanceFactor * (0.94 + fanoutFactor * 0.1) * riskFactor, 1.0, 2.9),
    glowOpacity: clamp(kindGlowBase * opacityFactor * clamp(0.9+riskScore*0.45, 0.9, 1.35), 0.008, 0.05),
    distanceFactor,
    fanoutFactor,
    bundleFactor,
    longHaul: distanceKm >= 1600,
    denseFanout: fanoutCount >= 9,
  };
}

function normalizeExplainRelation(value: any): ExplainRelation | null {
  const sourceId = String(value?.sourceFragmentId || '').trim();
  const targetId = String(value?.targetFragmentId || '').trim();
  const relatedFragmentId = String(value?.relatedFragmentId || '').trim();
  const type = normalizeEdgeType(value?.type);
  if (!sourceId || !targetId || !relatedFragmentId || !type) return null;
  const family = edgeFamily(typeof value?.family === 'string' ? value.family : type);
  const coordinates = value?.relatedCoordinates && Number.isFinite(value.relatedCoordinates.lat) && Number.isFinite(value.relatedCoordinates.lon)
    ? { lat: Number(value.relatedCoordinates.lat), lon: Number(value.relatedCoordinates.lon) }
    : undefined;
  return {
    edgeId: String(value?.edgeId || '').trim() || undefined,
    sourceId,
    targetId,
    relatedFragmentId,
    relatedLabel: String(value?.relatedLabel || '').trim() || humanizeCanonicalPath(String(value?.relatedCanonicalPath || '').trim()) || relatedFragmentId,
    relatedType: String(value?.relatedType || '').trim() || undefined,
    relatedCanonicalPath: String(value?.relatedCanonicalPath || '').trim() || undefined,
    relatedCoordinates: coordinates,
    type,
    family,
    semanticDirection: String(value?.semanticDirection || '').trim() || undefined,
    businessMeaning: String(value?.businessMeaning || '').trim() || undefined,
    whyRelevant: String(value?.whyRelevant || '').trim() || undefined,
    priority: typeof value?.priority === 'number' ? value.priority : undefined,
    criticality: typeof value?.criticality === 'number' ? value.criticality : undefined,
  };
}

function humanizeFragmentType(type: string): string {
  return String(type || '')
    .toLowerCase()
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function approximateDistanceKm(lon1: number, lat1: number, lon2: number, lat2: number): number {
  const toRadians = (value: number) => (value * Math.PI) / 180;
  const dLat = toRadians(lat2 - lat1);
  const dLon = toRadians(lon2 - lon1);
  const lat1Rad = toRadians(lat1);
  const lat2Rad = toRadians(lat2);
  const a = Math.sin(dLat / 2) ** 2
    + Math.cos(lat1Rad) * Math.cos(lat2Rad) * Math.sin(dLon / 2) ** 2;
  return 6371 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

function toLocalKm(lat0: number, lon0: number, lat: number, lon: number): { x: number; y: number } {
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat0 * Math.PI) / 180));
  return {
    x: (lon - lon0) * kmPerDegLon,
    y: (lat - lat0) * kmPerDegLat,
  };
}

function fromLocalKm(lat0: number, lon0: number, x: number, y: number): [number, number] {
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat0 * Math.PI) / 180));
  return [lon0 + x / kmPerDegLon, lat0 + y / kmPerDegLat];
}

function arcPoints(
  lon1: number,
  lat1: number,
  lon2: number,
  lat2: number,
  curveSeed: string,
  hop: 1 | 2 | 3,
  numPoints = 20,
): [number, number][] {
  const midLon = (lon1 + lon2) / 2;
  const midLat = (lat1 + lat2) / 2;
  const dx = lon2 - lon1;
  const dy = lat2 - lat1;
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  const perpX = -dy / len;
  const perpY = dx / len;
  const direction = hashString(curveSeed) % 2 === 0 ? 1 : -1;
  const baseBulge = Math.min(Math.max(len * 0.14, 0.25), 5.2);
  const hopFactor = hop === 1 ? 0.96 : hop === 2 ? 0.72 : 0.54;
  const jitter = 0.9 + (hashString(`${curveSeed}:${hop}`) % 13) / 42;
  const bulge = baseBulge * hopFactor * jitter * direction;
  const offsetLon = midLon + perpX * bulge;
  const offsetLat = midLat + perpY * bulge;
  const points: [number, number][] = [];
  for (let i = 0; i <= numPoints; i += 1) {
    const t = i / numPoints;
    const t1 = 1 - t;
    const lon = t1 * t1 * lon1 + 2 * t1 * t * offsetLon + t * t * lon2;
    const lat = t1 * t1 * lat1 + 2 * t1 * t * offsetLat + t * t * lat2;
    points.push([lon, lat]);
  }
  return points;
}

function gentleArcPoints(
  start: [number, number],
  end: [number, number],
  curveSeed: string,
  distanceKm: number,
  numPoints = 16,
): [number, number][] {
  if (distanceKm < 35) return [start, end];
  const [lon1, lat1] = start;
  const [lon2, lat2] = end;
  const midLon = (lon1 + lon2) / 2;
  const midLat = (lat1 + lat2) / 2;
  const dx = lon2 - lon1;
  const dy = lat2 - lat1;
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  const perpX = -dy / len;
  const perpY = dx / len;
  const direction = hashString(curveSeed) % 2 === 0 ? 1 : -1;
  const bulge = clamp(
    len * (distanceKm < 180 ? 0.04 : distanceKm < 450 ? 0.06 : 0.085),
    0.02,
    distanceKm < 180 ? 0.08 : distanceKm < 450 ? 0.18 : 0.32,
  ) * direction;
  const offsetLon = midLon + perpX * bulge;
  const offsetLat = midLat + perpY * bulge;
  const points: [number, number][] = [];
  for (let i = 0; i <= numPoints; i += 1) {
    const t = i / numPoints;
    const t1 = 1 - t;
    const lon = t1 * t1 * lon1 + 2 * t1 * t * offsetLon + t * t * lon2;
    const lat = t1 * t1 * lat1 + 2 * t1 * t * offsetLat + t * t * lat2;
    points.push([lon, lat]);
  }
  return points;
}

function quadraticCurvePoints(
  start: [number, number],
  end: [number, number],
  control: [number, number],
  numPoints = 16,
): [number, number][] {
  const [lon1, lat1] = start;
  const [lon2, lat2] = control;
  const [lon3, lat3] = end;
  const points: [number, number][] = [];
  for (let index = 0; index <= numPoints; index += 1) {
    const t = index / numPoints;
    const t1 = 1 - t;
    points.push([
      t1 * t1 * lon1 + 2 * t1 * t * lon2 + t * t * lon3,
      t1 * t1 * lat1 + 2 * t1 * t * lat2 + t * t * lat3,
    ]);
  }
  return points;
}

function pointOnSegment(a: [number, number], b: [number, number], t: number): [number, number] {
  return [
    a[0] + (b[0] - a[0]) * t,
    a[1] + (b[1] - a[1]) * t,
  ];
}

function projectPointToSegment(
  a: [number, number],
  b: [number, number],
  point: [number, number],
): { point: [number, number]; t: number } {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const lenSq = dx * dx + dy * dy;
  if (lenSq <= 1e-9) return { point: a, t: 0 };
  const rawT = ((point[0] - a[0]) * dx + (point[1] - a[1]) * dy) / lenSq;
  const t = clamp(rawT, 0, 1);
  return { point: pointOnSegment(a, b, t), t };
}

function dedupeSequentialPoints(points: [number, number][]): [number, number][] {
  const out: [number, number][] = [];
  for (const [lon, lat] of points) {
    const prev = out[out.length - 1];
    if (prev && Math.abs(prev[0] - lon) < 1e-7 && Math.abs(prev[1] - lat) < 1e-7) continue;
    out.push([lon, lat]);
  }
  return out;
}

function roundCoord(value: number, digits = 6): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

function unwrapLine(line: LonLat[]): LonLat[] {
  if (line.length === 0) return [];
  const out: LonLat[] = [[line[0][0], line[0][1]]];
  for (let index = 1; index < line.length; index += 1) {
    let lon = line[index][0];
    const lat = line[index][1];
    const prevLon = out[out.length - 1][0];
    while (lon - prevLon > 180) lon -= 360;
    while (lon - prevLon < -180) lon += 360;
    out.push([lon, lat]);
  }
  return out;
}

function wrapLonTo180(lon: number): number {
  const wrapped = ((lon + 180) % 360 + 360) % 360 - 180;
  if (wrapped === -180 && lon > 0) return 180;
  return wrapped;
}

function lonZone(lon: number): number {
  return Math.floor((lon + 180 - 1e-9) / 360);
}

function splitAtDateline(unwrapped: LonLat[]): LonLat[][] {
  if (unwrapped.length === 0) return [];
  const segments: LonLat[][] = [];
  let prev: LonLat = unwrapped[0];
  let current: LonLat[] = [[roundCoord(wrapLonTo180(prev[0])), roundCoord(prev[1])]];

  for (let index = 1; index < unwrapped.length; index += 1) {
    const next = unwrapped[index];
    const prevZone = lonZone(prev[0]);
    const nextZone = lonZone(next[0]);

    if (prevZone === nextZone) {
      current.push([roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]);
      prev = next;
      continue;
    }

    const movingEast = nextZone > prevZone;
    const boundaryUnwrapped = movingEast ? 180 + prevZone * 360 : -180 + prevZone * 360;
    const denom = next[0] - prev[0];
    if (Math.abs(denom) < 1e-9) {
      current.push([roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]);
      prev = next;
      continue;
    }

    const t = (boundaryUnwrapped - prev[0]) / denom;
    const latCross = prev[1] + t * (next[1] - prev[1]);
    const endLon = movingEast ? 180 : -180;
    const startLon = movingEast ? -180 : 180;

    current.push([endLon, roundCoord(latCross)]);
    if (current.length > 1) segments.push(dedupeSequentialPoints(current));

    current = [[startLon, roundCoord(latCross)], [roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]];
    prev = next;
  }

  if (current.length > 1) segments.push(dedupeSequentialPoints(current));
  return segments;
}

function smoothPolyline(points: [number, number][], passes = 2): [number, number][] {
  let current = dedupeSequentialPoints(points);
  if (current.length < 3) return current;
  for (let pass = 0; pass < passes; pass += 1) {
    const next: [number, number][] = [current[0]];
    for (let index = 0; index < current.length - 1; index += 1) {
      const a = current[index];
      const b = current[index + 1];
      const q: [number, number] = [
        a[0] * 0.75 + b[0] * 0.25,
        a[1] * 0.75 + b[1] * 0.25,
      ];
      const r: [number, number] = [
        a[0] * 0.25 + b[0] * 0.75,
        a[1] * 0.25 + b[1] * 0.75,
      ];
      next.push(q, r);
    }
    next.push(current[current.length - 1]);
    current = dedupeSequentialPoints(next);
  }
  return current;
}

function smoothRouteCoordinates(
  points: [number, number][],
  routeSeed: string,
  hop: 1 | 2 | 3,
): [number, number][] {
  const clean = dedupeSequentialPoints(points);
  if (clean.length < 2) return clean;
  if (clean.length === 2) {
    return arcPoints(clean[0][0], clean[0][1], clean[1][0], clean[1][1], routeSeed, hop);
  }
  const passes = hop >= 3 ? 3 : 2;
  return smoothPolyline(clean, passes);
}

function appendAlignedSegment(
  stitched: LonLat[],
  segment: LonLat[],
): LonLat[] {
  if (segment.length === 0) return stitched;
  if (stitched.length === 0) return [...segment];

  const anchorLon = stitched[stitched.length - 1][0];
  let offset = 0;
  while (segment[0][0] + offset - anchorLon > 180) offset -= 360;
  while (segment[0][0] + offset - anchorLon < -180) offset += 360;

  for (let index = 0; index < segment.length; index += 1) {
    const point: LonLat = [segment[index][0] + offset, segment[index][1]];
    const prev = stitched[stitched.length - 1];
    if (prev && Math.abs(prev[0] - point[0]) < 1e-7 && Math.abs(prev[1] - point[1]) < 1e-7) continue;
    stitched.push(point);
  }
  return stitched;
}

function stitchSupplyRouteCoordinates(
  points: LonLat[],
  routeSeed: string,
  hop: 1 | 2 | 3,
): LonLat[] {
  const clean = dedupeSequentialPoints(points);
  if (clean.length < 2) return clean;
  if (clean.length === 2) {
    const [start, end] = clean;
    const distanceKm = approximateDistanceKm(start[0], start[1], end[0], end[1]);
    const raw = distanceKm >= 1800
      ? gentleArcPoints(start, end, routeSeed, distanceKm, hop === 1 ? 20 : 16)
      : arcPoints(start[0], start[1], end[0], end[1], routeSeed, hop);
    return unwrapLine(raw);
  }

  let stitched: LonLat[] = [];
  for (let index = 0; index < clean.length - 1; index += 1) {
    const start = clean[index];
    const end = clean[index + 1];
    const distanceKm = approximateDistanceKm(start[0], start[1], end[0], end[1]);
    const raw = distanceKm < 35
      ? [start, end]
      : gentleArcPoints(
          start,
          end,
          `${routeSeed}:${index}`,
          distanceKm,
          distanceKm >= 1800 ? 18 : distanceKm >= 600 ? 14 : 10,
        );
    stitched = appendAlignedSegment(stitched, unwrapLine(raw));
  }
  return stitched;
}

function lerpPoint(a: [number, number], b: [number, number], t: number): [number, number] {
  return [
    a[0] + (b[0] - a[0]) * t,
    a[1] + (b[1] - a[1]) * t,
  ];
}

function segmentLength(a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  return Math.sqrt(dx * dx + dy * dy);
}

function offsetAlongNormal(a: [number, number], b: [number, number], point: [number, number], offset: number): [number, number] {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  return [
    point[0] + (-dy / len) * offset,
    point[1] + (dx / len) * offset,
  ];
}

function branchSplitOnSharedAxis(
  axisStart: [number, number],
  axisEnd: [number, number],
  endpoint: [number, number],
  splitRatio = 2 / 3,
): { point: [number, number]; t: number } {
  const projected = projectPointToSegment(axisStart, axisEnd, endpoint);
  const t = projected.t * splitRatio;
  return { point: pointOnSegment(axisStart, axisEnd, t), t };
}

function concaveCurveToAxis(
  start: [number, number],
  end: [number, number],
  axisStart: [number, number],
  axisEnd: [number, number],
): [number, number][] {
  const midpoint = lerpPoint(start, end, 0.5);
  const projectedMid = projectPointToSegment(axisStart, axisEnd, midpoint).point;
  const controlPoint = lerpPoint(midpoint, projectedMid, 0.62);
  return quadraticCurvePoints(start, end, controlPoint);
}

function supplyStrokeColor(theme: ThemeMode, kind: EdgeVisualKind): string {
  void theme;
  void kind;
  return supplyLegendColor();
}

function primaryStrokeColor(theme: ThemeMode): string {
  void theme;
  return directStrokeColor();
}

function propagationStrokeColor(theme: ThemeMode, kind: EdgeVisualKind): string {
  void theme;
  void kind;
  return propagationLegendColor();
}

function isGeoLocCanonicalPath(value?: string): boolean {
  return String(value || '').trim().toLowerCase().startsWith('/loc/');
}

function isGeoLocType(value?: string): boolean {
  const normalized = String(value || '').trim().toUpperCase();
  return normalized === 'GEO_REGION'
    || normalized === 'GEO_LOCATION'
    || normalized === 'LOCATION'
    || normalized.endsWith('_REGION')
    || normalized.endsWith('_LOCATION');
}

function isGeoLocObject(object?: Pick<TraceObjectInfo, 'canonicalPath' | 'type'> | null): boolean {
  if (!object) return false;
  return isGeoLocCanonicalPath(object.canonicalPath) || isGeoLocType(object.type);
}

function shouldSuppressConnection(
  sourceId: string,
  targetId: string,
  objectById: Map<string, TraceObjectInfo>,
): boolean {
  if (!sourceId || !targetId) return true;
  if (sourceId === targetId) return true;
  return isGeoLocObject(objectById.get(sourceId)) || isGeoLocObject(objectById.get(targetId));
}

function linePointAt(coords: [number, number][], ratio: number): [number, number] {
  if (coords.length === 0) return [0, 0];
  if (coords.length === 1) return coords[0];
  const clampedRatio = clamp(ratio, 0, 1);
  const lengths: number[] = [];
  let total = 0;
  for (let index = 0; index < coords.length - 1; index += 1) {
    const length = segmentLength(coords[index], coords[index + 1]);
    lengths.push(length);
    total += length;
  }
  if (total <= 0) return coords[0];
  let target = total * clampedRatio;
  for (let index = 0; index < lengths.length; index += 1) {
    const length = lengths[index];
    if (target <= length) {
      const start = coords[index];
      const end = coords[index + 1];
      const t = length <= 0 ? 0 : target / length;
      return lerpPoint(start, end, t);
    }
    target -= length;
  }
  return coords[coords.length - 1];
}

function lineLength(coords: [number, number][]): number {
  let total = 0;
  for (let index = 0; index < coords.length - 1; index += 1) {
    total += segmentLength(coords[index], coords[index + 1]);
  }
  return total;
}

function buildSplitLineGeometry(
  coords: [number, number][],
  labelRatio: number,
): {
  geometry: GeoJSON.LineString | GeoJSON.MultiLineString;
  labelPoint: [number, number];
} | null {
  const clean = dedupeSequentialPoints(coords);
  if (clean.length < 2) return null;
  const segments = splitAtDateline(unwrapLine(clean)).filter((segment) => segment.length >= 2);
  if (segments.length === 0) return null;
  const longestSegment = segments.reduce((best, segment) => (
    lineLength(segment) > lineLength(best) ? segment : best
  ), segments[0]);
  return {
    geometry: segments.length === 1
      ? { type: 'LineString', coordinates: segments[0] }
      : { type: 'MultiLineString', coordinates: segments },
    labelPoint: linePointAt(longestSegment, labelRatio),
  };
}

function averagePoint(points: [number, number][]): [number, number] {
  if (points.length === 0) return [0, 0];
  return [
    points.reduce((sum, point) => sum + point[0], 0) / points.length,
    points.reduce((sum, point) => sum + point[1], 0) / points.length,
  ];
}

function estimatePointClusterLinkKm(points: [number, number][]): number {
  if (points.length <= 1) return 180;
  const nearestDistances = points.map(() => Number.POSITIVE_INFINITY);
  let maxSpanKm = 0;

  for (let index = 0; index < points.length; index += 1) {
    for (let inner = index + 1; inner < points.length; inner += 1) {
      const distanceKm = approximateDistanceKm(
        points[index][0],
        points[index][1],
        points[inner][0],
        points[inner][1],
      );
      maxSpanKm = Math.max(maxSpanKm, distanceKm);
      nearestDistances[index] = Math.min(nearestDistances[index], distanceKm);
      nearestDistances[inner] = Math.min(nearestDistances[inner], distanceKm);
    }
  }

  const finiteNearest = nearestDistances.filter((value) => Number.isFinite(value) && value > 0);
  const avgNearestKm = finiteNearest.length > 0
    ? finiteNearest.reduce((sum, value) => sum + value, 0) / finiteNearest.length
    : Math.max(maxSpanKm * 0.45, 180);
  return clamp(Math.max(avgNearestKm * 2.35, maxSpanKm * 0.2, 180), 180, CORRIDOR_SPATIAL_CLUSTER_LINK_KM);
}

function clusterPointEntriesByDistance<T extends { point: [number, number] }>(
  entries: T[],
  maxLinkKm: number,
): T[][] {
  if (entries.length <= 1) return entries.length === 0 ? [] : [entries];
  const visited = new Set<number>();
  const clusters: T[][] = [];

  for (let index = 0; index < entries.length; index += 1) {
    if (visited.has(index)) continue;
    const queue = [index];
    visited.add(index);
    const cluster: T[] = [];
    while (queue.length > 0) {
      const currentIndex = queue.shift()!;
      const current = entries[currentIndex]!;
      cluster.push(current);
      for (let probeIndex = 0; probeIndex < entries.length; probeIndex += 1) {
        if (visited.has(probeIndex)) continue;
        const probe = entries[probeIndex]!;
        const distanceKm = approximateDistanceKm(
          current.point[0],
          current.point[1],
          probe.point[0],
          probe.point[1],
        );
        if (distanceKm > maxLinkKm) continue;
        visited.add(probeIndex);
        queue.push(probeIndex);
      }
    }
    clusters.push(cluster);
  }

  return clusters;
}

function densestClusterAnchor(points: [number, number][]): [number, number] {
  if (points.length === 0) return [0, 0];
  if (points.length === 1) return points[0]!;

  const radiusKm = clamp(estimatePointClusterLinkKm(points) * 0.72, 140, 640);
  let bestIndex = 0;
  let bestScore = Number.NEGATIVE_INFINITY;

  for (let index = 0; index < points.length; index += 1) {
    const candidate = points[index]!;
    let score = 0;
    for (const point of points) {
      const distanceKm = approximateDistanceKm(candidate[0], candidate[1], point[0], point[1]);
      const normalized = distanceKm / Math.max(radiusKm, 1);
      score += Math.exp(-(normalized * normalized) * 1.25);
    }
    if (score > bestScore) {
      bestScore = score;
      bestIndex = index;
    }
  }

  const anchor = points[bestIndex]!;
  let totalWeight = 0;
  let sumLon = 0;
  let sumLat = 0;
  for (const point of points) {
    const distanceKm = approximateDistanceKm(anchor[0], anchor[1], point[0], point[1]);
    if (distanceKm > radiusKm * 1.05) continue;
    const normalized = distanceKm / Math.max(radiusKm, 1);
    const weight = Math.exp(-(normalized * normalized) * 1.1);
    totalWeight += weight;
    sumLon += point[0] * weight;
    sumLat += point[1] * weight;
  }

  if (totalWeight <= 0) return anchor;
  return [sumLon / totalWeight, sumLat / totalWeight];
}

function azimuthDegrees(from: [number, number], to: [number, number]): number {
  const angle = Math.atan2(to[1] - from[1], to[0] - from[0]) * (180 / Math.PI);
  return (angle + 360) % 360;
}

function visualKindWeight(kind: EdgeVisualKind): number {
  if (kind === 'primary') return 3;
  if (kind === 'indirect') return 2;
  return 1;
}

function strongerVisualKind(left: EdgeVisualKind, right: EdgeVisualKind): EdgeVisualKind {
  return visualKindWeight(left) >= visualKindWeight(right) ? left : right;
}

function compactMeaning(value: string): string {
  const text = String(value || '').trim();
  if (!text) return '';
  const normalized = text.replace(/\.$/, '');
  if (normalized.length <= 26) return normalized;
  const words = normalized.split(/\s+/).slice(0, 4).join(' ');
  return words.length <= 26 ? words : `${words.slice(0, 23).trim()}...`;
}

function objectDisplayName(nodeId: string, objectById: Map<string, TraceObjectInfo>): string {
  const object = objectById.get(nodeId);
  if (!object) return 'cluster';
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath || 'cluster';
}

function buildLabelText(nodeId: string, meaning: string, objectById: Map<string, TraceObjectInfo>): string {
  const name = objectDisplayName(nodeId, objectById);
  const trimmedName = name.length > 24 ? `${name.slice(0, 21).trim()}...` : name;
  const shortMeaning = compactMeaning(meaning);
  return shortMeaning ? `Towards ${trimmedName}\n${shortMeaning}` : `Towards ${trimmedName}`;
}

function displayNameByPath(path: string, objectByPath: Map<string, TraceObjectInfo>): string {
  const object = objectByPath.get(path);
  if (!object) return humanizeCanonicalPath(path) || path;
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath || path;
}

function compactLabel(value: string, maxLength = 30): string {
  let text = String(value || '').trim();
  if (!text) return '';
  text = text
    .replace(/\b(company|co\.?)\b/gi, '')
    .replace(/\b(limited|ltd\.?)\b/gi, '')
    .replace(/\b(corporation|corp\.?)\b/gi, '')
    .replace(/\b(incorporated|inc\.?)\b/gi, '')
    .replace(/\b(llc|plc|gmbh|s\.a\.|ag|bv|nv|pte\.?)\b/gi, '')
    .replace(/\s*,\s*/g, ', ')
    .replace(/\s{2,}/g, ' ')
    .replace(/(?:,\s*)+$/g, '')
    .replace(/^[\s,.-]+|[\s,.-]+$/g, '')
    .trim();
  if (text.length <= maxLength) return text;
  const commaCut = text.split(',')[0]?.trim() || text;
  if (commaCut.length <= maxLength) return commaCut;
  return `${commaCut.slice(0, maxLength - 3).trim()}...`;
}

function normalizeLabelForCompare(value: string): string {
  return String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
}

function unresolvedSupplyCounterpartLabel(direction: 'inbound' | 'outbound' | 'context', focusLabel: string): string {
  if (direction === 'inbound') return 'Unresolved upstream source';
  if (direction === 'outbound') return 'Unresolved downstream endpoint';
  return `Internal processing at ${focusLabel}`;
}

function isSupplyPlaceholderLabel(value: string): boolean {
  const normalized = normalizeLabelForCompare(value);
  return normalized === 'unresolved upstream source' || normalized === 'unresolved downstream endpoint';
}

function pickDistinctSupplyLabel(
  refs: string[],
  objectByPath: Map<string, TraceObjectInfo>,
  focusPath?: string,
  focusLabel?: string,
): string {
  const normalizedFocusLabel = normalizeLabelForCompare(String(focusLabel || '').trim());
  const trimmedFocusPath = String(focusPath || '').trim();
  for (const ref of refs) {
    const trimmedRef = String(ref || '').trim();
    if (!trimmedRef) continue;
    if (trimmedFocusPath && trimmedRef === trimmedFocusPath) continue;
    const label = labelFromPathRef(trimmedRef);
    if (!label) continue;
    if (normalizedFocusLabel && normalizeLabelForCompare(label) === normalizedFocusLabel) continue;
    const fromObject = objectByPath.get(trimmedRef);
    if (fromObject?.label && normalizedFocusLabel && normalizeLabelForCompare(fromObject.label) === normalizedFocusLabel) continue;
    return label;
  }
  return '';
}

function traceEndpointLabel(
  artifact: MaterializedArtifact,
  endpoint: 'supplier' | 'customer' | 'upstream' | 'downstream',
  focusLabel?: string,
): string {
  const candidate = artifact.traceEndpoints?.[endpoint];
  const label = String(candidate?.label || '').trim();
  if (!label) return '';
  if (focusLabel && normalizeLabelForCompare(label) === normalizeLabelForCompare(focusLabel)) return '';
  return label;
}

function describeSupplyReason(
  role: string,
  direction: 'inbound' | 'outbound' | 'context',
  productLabel?: string,
  unresolvedCounterpart = false,
): string {
  const normalized = String(role || '').trim().toLowerCase();
  const label = String(productLabel || '').trim();
  const productText = label ? ` ${label}` : '';
  if (unresolvedCounterpart) {
    if (direction === 'inbound') return `Mapped as an inbound${productText} route feeding the selected object, but the upstream source is not resolved on the map yet.`;
    if (direction === 'outbound') return `Mapped as an outbound${productText} route leaving the selected object, but the downstream endpoint is not resolved on the map yet.`;
  }
  if (normalized === 'direct_anchor') {
    if (direction === 'inbound') return `Mapped as an inbound${productText} route feeding the selected object.`;
    if (direction === 'outbound') return `Mapped as an outbound${productText} route leaving the selected object.`;
    return label
      ? `Mapped as an internal or surrounding ${label} route anchored on the selected object.`
      : 'Mapped as an internal or surrounding route anchored on the selected object.';
  }
  if (normalized === 'route_overlap') {
    return label
      ? `Shown because this ${label} route shares facilities, operators, or market anchors with the selected object.`
      : 'Shown because this route shares facilities, operators, or market anchors with the selected object.';
  }
  if (normalized === 'route_context') {
    return label
      ? `Shown as surrounding context for the selected object's ${label} network.`
      : 'Shown as surrounding context for the selected object.';
  }
  if (normalized === 'pinned') {
    return 'Pinned visible around the selected object for comparison.';
  }
  if (direction === 'inbound') return 'Mapped as an upstream route feeding the selected object.';
  if (direction === 'outbound') return 'Mapped as a downstream route leaving the selected object.';
  return 'Mapped as surrounding supply context for the selected object.';
}

function buildSupplyLabelText(
  artifact: MaterializedArtifact,
  productRef: string,
  supplierRef: string,
  customerRef: string,
  transportViaRefs: string[],
  objectByPath: Map<string, TraceObjectInfo>,
  focusObject?: Pick<GeoObject, 'label' | 'canonicalPath'> | null,
  riskTag?: string,
): string {
  const productLabel = compactLabel(displayNameByPath(productRef, objectByPath), 28);
  const focusPath = String(focusObject?.canonicalPath || '').trim();
  const focusLabel = String(focusObject?.label || '').trim();
  const direction = classifySupplyDirection(focusPath, supplierRef, customerRef);
  const resolvedUpstream = traceEndpointLabel(artifact, 'upstream', focusLabel) || traceEndpointLabel(artifact, 'supplier', focusLabel);
  const resolvedDownstream = traceEndpointLabel(artifact, 'downstream', focusLabel) || traceEndpointLabel(artifact, 'customer', focusLabel);
  const inboundLabel = pickDistinctSupplyLabel([supplierRef, ...transportViaRefs], objectByPath, focusPath, focusLabel);
  const outboundLabel = pickDistinctSupplyLabel([...transportViaRefs].reverse().concat(customerRef), objectByPath, focusPath, focusLabel);
  const shortRiskTag = compactLabel(String(riskTag || '').trim(), 16);
  if (shortRiskTag) {
    if (direction === 'inbound' && (resolvedUpstream || inboundLabel)) return `${productLabel}\n${shortRiskTag} · from ${compactLabel(resolvedUpstream || inboundLabel, 14)}`;
    if (direction === 'outbound' && (resolvedDownstream || outboundLabel)) return `${productLabel}\n${shortRiskTag} · to ${compactLabel(resolvedDownstream || outboundLabel, 14)}`;
    if (direction === 'context') {
      const resolvedContext = resolvedDownstream || resolvedUpstream;
      const contextLabel = pickDistinctSupplyLabel(
        [supplierRef, ...transportViaRefs, customerRef],
        objectByPath,
        focusPath,
        focusLabel,
      );
      return resolvedContext || contextLabel
        ? `${productLabel}\n${shortRiskTag} · via ${compactLabel(resolvedContext || contextLabel, 14)}`
        : `${productLabel}\n${shortRiskTag}`;
    }
  }
  if (direction === 'inbound' && (resolvedUpstream || inboundLabel)) return `${productLabel}\nfrom ${compactLabel(resolvedUpstream || inboundLabel, 18)}`;
  if (direction === 'outbound' && (resolvedDownstream || outboundLabel)) return `${productLabel}\nto ${compactLabel(resolvedDownstream || outboundLabel, 18)}`;
  if (direction === 'context') {
    const resolvedContext = resolvedDownstream || resolvedUpstream;
    const contextLabel = pickDistinctSupplyLabel(
      [supplierRef, ...transportViaRefs, customerRef],
      objectByPath,
      focusPath,
      focusLabel,
    );
    return resolvedContext || contextLabel ? `${productLabel}\nvia ${compactLabel(resolvedContext || contextLabel, 18)}` : `${productLabel}\ninternal processing`;
  }
  if (transportViaRefs.length > 0) {
    const viaLabel = pickDistinctSupplyLabel(transportViaRefs, objectByPath, focusPath, focusLabel);
    return viaLabel ? `${productLabel}\nvia ${compactLabel(viaLabel, 18)}` : productLabel;
  }
  return productLabel;
}

function buildSupplyBundleLabelText(
  routesCount: number,
  sampleRoute: { productRef: string; direction: 'inbound' | 'outbound' | 'context'; riskTag?: string },
  objectByPath: Map<string, TraceObjectInfo>,
): string {
  const productLabel = compactLabel(displayNameByPath(sampleRoute.productRef, objectByPath), 24);
  const riskTag = compactLabel(String(sampleRoute.riskTag || '').trim(), 14);
  if (routesCount <= 1) return riskTag ? `${productLabel}\n${riskTag}` : productLabel;
  const directionLabel =
    sampleRoute.direction === 'inbound'
      ? 'inbound routes'
      : sampleRoute.direction === 'outbound'
        ? 'outbound routes'
        : 'context routes';
  return riskTag ? `${productLabel}\n${riskTag} · x${routesCount}` : `${productLabel}\n${directionLabel} x${routesCount}`;
}

function fragmentToGeoObject(
  fragment: { id?: string; canonicalPath?: string; type?: string; label?: string; [key: string]: unknown },
  coordsMap: Record<string, { lat: number; lon: number }>,
): GeoObject | null {
  const traceObject = fragmentToTraceObject(fragment, coordsMap);
  if (!traceObject?.coordinates) return null;
  return traceObject;
}

function fragmentToTraceObject(
  fragment: { id?: string; canonicalPath?: string; type?: string; label?: string; [key: string]: unknown },
  coordsMap: Record<string, { lat: number; lon: number }>,
): TraceObjectInfo | null {
  const id = fragment?.id ?? (fragment as any)?.ID;
  if (!id || typeof id !== 'string') return null;
  const lat = (fragment as any)?.coordinates?.lat ?? coordsMap[id]?.lat;
  const lon = (fragment as any)?.coordinates?.lon ?? coordsMap[id]?.lon;
  return {
    id,
    canonicalPath: (fragment?.canonicalPath ?? (fragment as any)?.CanonicalPath ?? '') as string,
    type: (fragment?.type ?? (fragment as any)?.Type ?? 'OTHER') as string,
    facilityKind: (fragment as any)?.facilityKind ?? (fragment as any)?.FacilityKind,
    label: (fragment?.label ?? (fragment as any)?.Label ?? '') as string | undefined,
    description: (fragment as any)?.description ?? (fragment as any)?.Description,
    status: (fragment as any)?.status ?? (fragment as any)?.Status,
    coordinates: Number.isFinite(lat) && Number.isFinite(lon) ? { lat: lat as number, lon: lon as number } : undefined,
  };
}

function traceObjectToGeoObject(traceObject?: TraceObjectInfo | null): GeoObject | null {
  if (!traceObject) return null;
  const lat = traceObject.coordinates?.lat;
  const lon = traceObject.coordinates?.lon;
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  return {
    id: traceObject.id,
    canonicalPath: traceObject.canonicalPath || traceObject.id,
    type: traceObject.type || 'OTHER',
    facilityKind: traceObject.facilityKind,
    label: traceObject.label,
    description: traceObject.description,
    status: traceObject.status,
    coordinates: { lat: lat!, lon: lon! },
    inScope: traceObject.inScope,
  };
}

function featureEndpointRefs(feature: GeoJSON.Feature): string[] {
  const nodeIds = Array.isArray(feature.properties?.endpointNodeIds)
    ? feature.properties.endpointNodeIds.map((value: unknown) => `node:${String(value || '').trim()}`).filter(Boolean)
    : [];
  const pathRefs = Array.isArray(feature.properties?.endpointPathRefs)
    ? feature.properties.endpointPathRefs.map((value: unknown) => `path:${String(value || '').trim()}`).filter(Boolean)
    : [];
  return [...nodeIds, ...pathRefs];
}

function featureBadgeEndpointRefs(feature: GeoJSON.Feature): string[] {
  const nodeIds = Array.isArray(feature.properties?.badgeEndpointNodeIds)
    ? feature.properties.badgeEndpointNodeIds.map((value: unknown) => `node:${String(value || '').trim()}`).filter(Boolean)
    : [];
  const pathRefs = Array.isArray(feature.properties?.badgeEndpointPathRefs)
    ? feature.properties.badgeEndpointPathRefs.map((value: unknown) => `path:${String(value || '').trim()}`).filter(Boolean)
    : [];
  const refs = [...nodeIds, ...pathRefs];
  return refs.length > 0 ? refs : featureEndpointRefs(feature);
}

function endpointMarkerFeatureId(key: string): string {
  return `${key}:marker`;
}

function endpointLabelFeatureId(key: string): string {
  return `${key}:label`;
}

function buildEndpointFeatureCollection(
  endpointEntries: EndpointSourceEntry[],
  theme: ThemeMode,
  labelMetaByKey?: Map<string, EndpointLabelMeta>,
): GeoJSON.FeatureCollection {
  return {
    type: 'FeatureCollection',
    features: endpointEntries.flatMap(({ key, object }) => {
      const actualPoint: [number, number] = [object.coordinates!.lon, object.coordinates!.lat];
      const meta = labelMetaByKey?.get(key);
      const endpointLabel = meta?.labelText || compactLabel(String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath, 28);
      const output: GeoJSON.Feature[] = [];
      const feature = buildPointFeature({
        ...object,
        coordinates: { lat: actualPoint[1], lon: actualPoint[0] },
      }, theme) as GeoJSON.Feature<GeoJSON.Point>;
      feature.id = endpointMarkerFeatureId(key);
      feature.properties = {
        ...(feature.properties || {}),
        kind: 'endpoint-marker',
        endpointKey: key,
      };
      output.push(feature);
      output.push({
        id: endpointLabelFeatureId(key),
        type: 'Feature',
        geometry: { type: 'Point', coordinates: actualPoint },
        properties: {
          kind: 'endpoint-label',
          endpointKey: key,
          endpointLabel,
          labelPriority: meta?.priority ?? 0,
          labelVisible: meta?.visible !== false ? 1 : 0,
          labelChip: meta?.chip ? 1 : 0,
          labelSubdued: meta?.subdued ? 1 : 0,
          labelStrong: meta?.strong ? 1 : 0,
          labelTier: meta?.tier || 'propagation',
        },
      } as GeoJSON.Feature<GeoJSON.Point>);
      return output;
    }),
  };
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

function endpointLabelText(object: GeoObject): string {
  return compactLabel(String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath, 28);
}

function endpointMarkerRect(point: { x: number; y: number }): ScreenRect {
  const half = ENDPOINT_MARKER_SIZE_PX / 2;
  return {
    left: point.x - half,
    right: point.x + half,
    top: point.y - half,
    bottom: point.y + half,
  };
}

function endpointLabelRect(point: { x: number; y: number; object?: GeoObject; entry?: { object: GeoObject } }): ScreenRect {
  const object = point.object || point.entry?.object;
  const label = object ? endpointLabelText(object) : '';
  const width = clamp(label.length * ENDPOINT_LABEL_CHAR_WIDTH_PX, 34, ENDPOINT_LABEL_MAX_WIDTH_PX);
  return {
    left: point.x + ENDPOINT_LABEL_OFFSET_X_PX,
    right: point.x + ENDPOINT_LABEL_OFFSET_X_PX + width,
    top: point.y - ENDPOINT_LABEL_HEIGHT_PX / 2,
    bottom: point.y + ENDPOINT_LABEL_HEIGHT_PX / 2,
  };
}

function endpointMarkersOverlap(
  a: { x: number; y: number },
  b: { x: number; y: number },
  padding = ENDPOINT_MARKER_PADDING_PX,
): boolean {
  const markerA = endpointMarkerRect(a);
  const markerB = endpointMarkerRect(b);
  return rectsOverlap(markerA, markerB, padding);
}

function endpointTierPriority(tier: EndpointLabelTier): number {
  if (tier === 'selected') return 100;
  if (tier === 'direct') return 82;
  if (tier === 'path') return 68;
  if (tier === 'context') return 52;
  return 40;
}

function buildEndpointLabelMetaMap(
  map: maplibregl.Map,
  endpointEntries: EndpointPlacedEntry[],
  tierByKey: Map<string, EndpointLabelTier>,
  narrativeKeysByEndpointKey: Map<string, string[]>,
  forcedVisibleKeys: ReadonlySet<string>,
  activeNarrativeKey: string | null,
  zoom: number,
): Map<string, EndpointLabelMeta> {
  const activeNarrative = String(activeNarrativeKey || '').trim();
  const activeBoostKeys = new Set<string>();
  if (activeNarrative) {
    for (const entry of endpointEntries) {
      const narrativeKeys = narrativeKeysByEndpointKey.get(entry.key) || [];
      if (narrativeKeys.includes(activeNarrative)) activeBoostKeys.add(entry.key);
    }
  }

  const visibleCandidates = endpointEntries
    .sort((left, right) => {
      const leftTier = tierByKey.get(left.key) || 'propagation';
      const rightTier = tierByKey.get(right.key) || 'propagation';
      const leftScore = endpointTierPriority(leftTier) + (activeBoostKeys.has(left.key) ? 24 : 0);
      const rightScore = endpointTierPriority(rightTier) + (activeBoostKeys.has(right.key) ? 24 : 0);
      return rightScore - leftScore;
    });
  const globalBudget = zoom >= 9 ? 34 : zoom >= 8 ? 28 : zoom >= 7 ? 24 : zoom >= 6 ? 20 : zoom >= 5 ? 14 : 10;
  const projectedEntries = endpointEntries.map((entry) => {
    const projected = map.project({ lng: entry.anchorPoint[0], lat: entry.anchorPoint[1] });
    return { ...entry, x: projected.x, y: projected.y };
  });
  const projectedByKey = new Map(projectedEntries.map((entry) => [entry.key, entry] as const));
  const markerRects = projectedEntries.map((entry) => ({
    key: entry.key,
    rect: endpointMarkerRect(entry),
  }));
  const occupiedLabelRects: ScreenRect[] = [];
  const globallyVisible = new Set<string>();
  let visibleOptionalCount = 0;

  for (const entry of visibleCandidates) {
    const tier = tierByKey.get(entry.key) || 'propagation';
    const forcedVisible = forcedVisibleKeys.has(entry.key);
    const alwaysVisible = tier === 'selected' || tier === 'direct' || tier === 'path' || tier === 'context';
    const protectedLabel =
      forcedVisible
      || alwaysVisible
      || activeBoostKeys.has(entry.key);
    const projected = projectedByKey.get(entry.key);
    if (!projected) continue;
    const labelRect = endpointLabelRect(projected);
    const collidesWithAcceptedLabel = occupiedLabelRects.some((rect) => rectsOverlap(rect, labelRect, protectedLabel ? 2 : 5));
    const collidesWithMarker = markerRects.some((marker) => marker.key !== entry.key && rectsOverlap(marker.rect, labelRect, 2));

    if (!protectedLabel) {
      if (visibleOptionalCount >= globalBudget) continue;
      if (collidesWithAcceptedLabel || collidesWithMarker) continue;
      visibleOptionalCount += 1;
    }

    if (protectedLabel && !alwaysVisible && collidesWithAcceptedLabel && zoom < 5.5 && !forcedVisible) continue;
    globallyVisible.add(entry.key);
    occupiedLabelRects.push(labelRect);
  }

  const meta = new Map<string, EndpointLabelMeta>();
  for (const entry of endpointEntries) {
    const tier = tierByKey.get(entry.key) || 'propagation';
    const active = activeBoostKeys.has(entry.key);
    const strong = tier === 'selected' || tier === 'direct' || tier === 'path' || tier === 'context' || active;
    const priority = endpointTierPriority(tier) + (active ? 32 : 0) + (strong ? 8 : 0);
    const visible = globallyVisible.has(entry.key);
    meta.set(entry.key, {
      labelText: compactLabel(
        String(entry.object.label || '').trim() || humanizeCanonicalPath(entry.object.canonicalPath) || entry.object.canonicalPath,
        strong ? 32 : 24,
      ),
      priority,
      visible,
      chip: visible && strong,
      subdued: !active && tier === 'propagation',
      strong,
      tier,
      narrativeKeys: narrativeKeysByEndpointKey.get(entry.key) || [],
    });
  }
  return meta;
}

function buildEndpointSourceData(
  renderState: EndpointRenderState,
  theme: ThemeMode,
  activeNarrativeKey: string | null,
  map: maplibregl.Map,
  zoom: number,
): GeoJSON.FeatureCollection {
  const labelMetaByKey = buildEndpointLabelMetaMap(
    map,
    renderState.placedEntries,
    renderState.tierByKey,
    renderState.narrativeKeysByEndpointKey,
    renderState.forcedVisibleKeys,
    activeNarrativeKey,
    zoom,
  );
  return buildEndpointFeatureCollection(renderState.placedEntries, theme, labelMetaByKey);
}

function spreadEndpointEntries(
  map: maplibregl.Map,
  entries: Array<{ key: string; object: GeoObject }>,
): Array<{ key: string; object: GeoObject; displayPoint: [number, number]; anchorPoint: [number, number] }> {
  if (entries.length === 0) return [];
  if (entries.length === 1) {
    const object = entries[0].object;
    return [{
      ...entries[0],
      displayPoint: [object.coordinates!.lon, object.coordinates!.lat],
      anchorPoint: [object.coordinates!.lon, object.coordinates!.lat],
    }];
  }

  const points = entries.map((entry, index) => {
    const anchorPoint: [number, number] = [entry.object.coordinates!.lon, entry.object.coordinates!.lat];
    const projected = map.project({ lng: anchorPoint[0], lat: anchorPoint[1] });
    return {
      entry,
      index,
      anchorPoint,
      x: projected.x,
      y: projected.y,
      ox: projected.x,
      oy: projected.y,
      angle: (Math.PI * 2 * index) / Math.max(entries.length, 1),
    };
  });

  const zoom = map.getZoom();
  const zoomSpreadFactor = clamp((zoom - 3.8) / 4.4, 0, 1);
  const minDistancePx = ENDPOINT_MARKER_SIZE_PX + ENDPOINT_MARKER_PADDING_PX + zoomSpreadFactor * 10;
  const maxOffsetPx = 10 + zoomSpreadFactor * 26;
  const collisionPadding = 2 + zoomSpreadFactor * 4;
  const relaxationIterations = zoom >= 8 ? 14 : zoom >= 6 ? 10 : 7;
  const hasAnyOverlap = () => {
    for (let i = 0; i < points.length; i += 1) {
      for (let j = i + 1; j < points.length; j += 1) {
        if (endpointMarkersOverlap(points[i], points[j], collisionPadding)) return true;
      }
    }
    return false;
  };
  if (!hasAnyOverlap()) {
    return points.map((point) => ({
      key: point.entry.key,
      object: point.entry.object,
      anchorPoint: point.anchorPoint,
      displayPoint: point.anchorPoint,
    }));
  }

  for (let iteration = 0; iteration < relaxationIterations; iteration += 1) {
    for (let i = 0; i < points.length; i += 1) {
      for (let j = i + 1; j < points.length; j += 1) {
        const a = points[i];
        const b = points[j];
        if (!endpointMarkersOverlap(a, b, collisionPadding)) continue;
        let dx = b.x - a.x;
        let dy = b.y - a.y;
        let distance = Math.sqrt(dx * dx + dy * dy);
        if (distance < 1e-4) {
          dx = Math.cos(a.angle);
          dy = Math.sin(a.angle);
          distance = 1;
        }
        const nx = dx / distance;
        const ny = dy / distance;
        const push = clamp(Math.max(minDistancePx - distance, 4), 2.5, 11 + zoomSpreadFactor * 3.5) * 0.4;
        a.x -= nx * push;
        a.y -= ny * push;
        b.x += nx * push;
        b.y += ny * push;
      }
    }
    for (const point of points) {
      const dx = point.x - point.ox;
      const dy = point.y - point.oy;
      const distance = Math.sqrt(dx * dx + dy * dy);
      if (distance > maxOffsetPx) {
        const scale = maxOffsetPx / distance;
        point.x = point.ox + dx * scale;
        point.y = point.oy + dy * scale;
      }
    }
  }

  return points.map((point) => {
    const lngLat = map.unproject([point.x, point.y]);
    return {
      key: point.entry.key,
      object: point.entry.object,
      anchorPoint: point.anchorPoint,
      displayPoint: [lngLat.lng, lngLat.lat] as [number, number],
    };
  });
}

function strongestCorridorFeature(
  features: GeoJSON.Feature[],
): GeoJSON.Feature<GeoJSON.LineString> | null {
  const lines = features.filter((feature) =>
    feature.geometry?.type === 'LineString'
    && ['primary', 'indirect', 'cluster'].includes(String(feature.properties?.kind || '').trim()),
  ) as GeoJSON.Feature<GeoJSON.LineString>[];
  if (lines.length === 0) return null;
  return [...lines].sort((left, right) => {
    const leftScore =
      (Number(left.properties?.renderPriority) || 0) * 10
      + (Number(left.properties?.baseWidth) || 0) * 4
      + (Number(left.properties?.baseOpacity) || 0);
    const rightScore =
      (Number(right.properties?.renderPriority) || 0) * 10
      + (Number(right.properties?.baseWidth) || 0) * 4
      + (Number(right.properties?.baseOpacity) || 0);
    return rightScore - leftScore;
  })[0];
}

function strongestFeatureForKeys(
  features: GeoJSON.Feature[],
  keys: ReadonlySet<string>,
): GeoJSON.Feature<GeoJSON.LineString> | null {
  if (keys.size === 0) return null;
  return strongestCorridorFeature(
    features.filter((feature) => {
      if (feature.geometry?.type !== 'LineString') return false;
      return featureConnectionKeys(feature).some((key) => keys.has(key));
    }),
  );
}

function featureConnectionKeys(feature: GeoJSON.Feature): string[] {
  const refs = new Set<string>([...featureEndpointRefs(feature), ...featureBadgeEndpointRefs(feature)]);
  const sourceId = String(feature.properties?.sourceId || '').trim();
  const targetId = String(feature.properties?.targetId || '').trim();
  if (sourceId) refs.add(`node:${sourceId}`);
  if (targetId) refs.add(`node:${targetId}`);
  return Array.from(refs);
}

function resolveBushOriginKey(
  key: string,
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
): BushOrigin | null {
  if (key.startsWith('node:')) {
    const object = objectById.get(key.slice(5));
    if (!object?.coordinates) return null;
    return {
      key,
      point: [object.coordinates.lon, object.coordinates.lat],
    };
  }
  if (key.startsWith('path:')) {
    const object = objectByPath.get(key.slice(5));
    if (!object?.coordinates) return null;
    return {
      key,
      point: [object.coordinates.lon, object.coordinates.lat],
    };
  }
  return null;
}

function resolveBushOriginForGroup(
  cluster: EndpointPlacedEntry[],
  corridorFeatures: GeoJSON.Feature[],
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
): BushOrigin | null {
  const clusterKeys = new Set(cluster.map((entry) => entry.key));
  if (clusterKeys.size === 0) return null;

  const centroid = densestClusterAnchor(cluster.map((entry) => entry.anchorPoint));
  const scores = new Map<string, number>();

  for (const feature of corridorFeatures) {
    if (feature.geometry?.type !== 'LineString') continue;
    const connectionKeys = featureConnectionKeys(feature);
    if (!connectionKeys.some((key) => clusterKeys.has(key))) continue;
    const renderWeight = Math.max(1, Number(feature.properties?.renderPriority) || 1) / 18;
    for (const candidateKey of connectionKeys) {
      if (!candidateKey || clusterKeys.has(candidateKey)) continue;
      const resolved = resolveBushOriginKey(candidateKey, objectById, objectByPath);
      if (!resolved) continue;
      const distancePenalty = approximateDistanceKm(
        resolved.point[0],
        resolved.point[1],
        centroid[0],
        centroid[1],
      ) / 1000;
      const current = scores.get(candidateKey) || 0;
      scores.set(candidateKey, current + renderWeight - Math.min(distancePenalty, 0.75));
    }
  }

  const best = Array.from(scores.entries()).sort((left, right) => right[1] - left[1])[0];
  if (!best) return null;
  return resolveBushOriginKey(best[0], objectById, objectByPath);
}

function buildSelectedBushGroups(
  endpointEntries: EndpointPlacedEntry[],
  corridorFeatures: GeoJSON.Feature[],
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
  featureIdBase: string,
): BushGroup[] {
  if (endpointEntries.length <= 1) return [];
  const bushLinkKm = clamp(estimatePointClusterLinkKm(endpointEntries.map((entry) => entry.anchorPoint)) * 0.58, 90, 320);
  const minClusterSize = endpointEntries.length >= 5 ? 3 : 2;
  const clusters = clusterPointEntriesByDistance(
    endpointEntries.map((entry) => ({ ...entry, point: entry.anchorPoint })),
    bushLinkKm,
  )
    .map((cluster) => cluster.map(({ point: _point, ...entry }) => entry))
    .filter((cluster) => cluster.length >= minClusterSize)
    .sort((left, right) => right.length - left.length);

  return clusters.flatMap((entries, index) => {
    const origin = resolveBushOriginForGroup(entries, corridorFeatures, objectById, objectByPath);
    if (!origin) return [];
    return [{
      id: `${featureIdBase}:${index}`,
      entries,
      origin,
    }];
  });
}

function buildSelectedBushConnectorFeatures(
  group: BushGroup,
  corridorFeatures: GeoJSON.Feature[],
): GeoJSON.Feature[] {
  const sample = strongestFeatureForKeys(
    corridorFeatures,
    new Set(group.entries.map((entry) => entry.key)),
  );
  if (!sample || group.entries.length === 0) return [];

  const channel = String(sample.properties?.channel || '').trim();
  const color = String(sample.properties?.color || '').trim() || supplyLegendColor();
  const kind = String(sample.properties?.kind || 'primary').trim();
  const hop = Number(sample.properties?.hop) || 1;
  const renderPriority = Number(sample.properties?.renderPriority) || 20;
  const baseWidth = Number(sample.properties?.baseWidth) || 2.2;
  const baseOpacity = Number(sample.properties?.baseOpacity) || 0.88;
  const glowOpacity = Number(sample.properties?.glowOpacity) || 0.12;
  const narrativeKey = String(sample.properties?.narrativeKey || '').trim();
  const multiple = group.entries.length > 1;

  return group.entries.map((endpoint) => {
    const distanceKm = approximateDistanceKm(
      group.origin.point[0],
      group.origin.point[1],
      endpoint.anchorPoint[0],
      endpoint.anchorPoint[1],
    );
    const coordinates = !multiple && kind === 'primary' && channel !== 'propagation'
      ? [group.origin.point, endpoint.anchorPoint]
      : gentleArcPoints(group.origin.point, endpoint.anchorPoint, `${group.id}:${endpoint.key}`, distanceKm);
    return {
      id: `${group.id}:branch:${endpoint.key}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates },
      properties: {
        kind,
        hop,
        color,
        channel,
        corridorId: `${group.id}:${group.origin.key}`,
        narrativeKey,
        endpointNodeIds: endpoint.key.startsWith('node:') ? [endpoint.key.slice(5)] : [],
        endpointPathRefs: endpoint.key.startsWith('path:') ? [endpoint.key.slice(5)] : [],
        renderPriority,
        baseOpacity: multiple ? clamp(baseOpacity - 0.08, 0.24, 0.88) : baseOpacity,
        baseWidth: multiple ? clamp(baseWidth * 0.82, 0.9, 4.8) : baseWidth,
        glowOpacity: multiple ? clamp(glowOpacity * 0.72, 0.02, 0.16) : glowOpacity,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>;
  });
}

function rotateLocal(point: LocalPoint, angle: number): LocalPoint {
  const cos = Math.cos(angle);
  const sin = Math.sin(angle);
  return {
    x: point.x * cos - point.y * sin,
    y: point.x * sin + point.y * cos,
  };
}

function localDistanceKm(a: LocalPoint, b: LocalPoint): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

function buildFogPolygon(
  lat0: number,
  lon0: number,
  center: LocalPoint,
  radiusXKm: number,
  radiusYKm: number,
  rotationRad: number,
  seed: string,
  wobble = 0.08,
  segments = 32,
): GeoJSON.Polygon {
  const ring: [number, number][] = [];
  for (let index = 0; index <= segments; index += 1) {
    const angle = (Math.PI * 2 * index) / segments;
    const noise = ((hashString(`${seed}:${index}`) % 1000) / 1000) - 0.5;
    const radiusScale = 1 + noise * wobble * 2;
    const local = rotateLocal({
      x: Math.cos(angle) * radiusXKm * radiusScale,
      y: Math.sin(angle) * radiusYKm * radiusScale,
    }, rotationRad);
    ring.push(fromLocalKm(lat0, lon0, center.x + local.x, center.y + local.y));
  }
  return { type: 'Polygon', coordinates: [ring] };
}

type SelectedCorridorFieldMetrics = {
  centerLat: number;
  centerLon: number;
  threshold: number;
  minX: number;
  maxX: number;
  minY: number;
  maxY: number;
  nodes: Array<{
    key: string;
    object: GeoObject;
    x: number;
    y: number;
    radiusKm: number;
    weight: number;
  }>;
  bridges: Array<{
    x: number;
    y: number;
    radiusKm: number;
    weight: number;
  }>;
};

function buildSelectedCorridorFieldMetrics(
  endpointEntries: Array<{ key: string; object: GeoObject; displayPoint: [number, number]; anchorPoint: [number, number] }>,
): SelectedCorridorFieldMetrics | null {
  if (endpointEntries.length < 2) return null;
  const centroid = averagePoint(endpointEntries.map((entry) => entry.anchorPoint));
  const centerLon = centroid[0];
  const centerLat = centroid[1];
  const rawNodes = endpointEntries.map((entry) => {
    const local = toLocalKm(centerLat, centerLon, entry.anchorPoint[1], entry.anchorPoint[0]);
    return {
      key: entry.key,
      object: entry.object,
      x: local.x,
      y: local.y,
    };
  });

  const nearestDistances = rawNodes.map(() => Number.POSITIVE_INFINITY);
  let maxSpanKm = 0;
  for (let index = 0; index < rawNodes.length; index += 1) {
    for (let inner = index + 1; inner < rawNodes.length; inner += 1) {
      const distanceKm = localDistanceKm(rawNodes[index], rawNodes[inner]);
      maxSpanKm = Math.max(maxSpanKm, distanceKm);
      nearestDistances[index] = Math.min(nearestDistances[index], distanceKm);
      nearestDistances[inner] = Math.min(nearestDistances[inner], distanceKm);
    }
  }

  const nearestSamples = nearestDistances.filter((value) => Number.isFinite(value) && value > 0);
  const avgNearestKm = nearestSamples.length > 0
    ? nearestSamples.reduce((sum, value) => sum + value, 0) / nearestSamples.length
    : Math.max(maxSpanKm * 0.55, 24);
  const baseRadiusKm = clamp(Math.max(avgNearestKm * 0.68, maxSpanKm * 0.24, 26), 22, 220);
  const pairLimitKm = Math.max(baseRadiusKm * 2.9, avgNearestKm * 1.35, 120);

  const nodes = rawNodes.map((node, index) => {
    const nearestKm = Number.isFinite(nearestDistances[index]) ? nearestDistances[index] : avgNearestKm;
    return {
      ...node,
      radiusKm: clamp(baseRadiusKm * 0.96 + nearestKm * 0.22, 24, baseRadiusKm * 1.28),
      weight: clamp(0.86 + (nearestKm / Math.max(avgNearestKm, 1)) * 0.1, 0.86, 1.04),
    };
  });

  const bridges: SelectedCorridorFieldMetrics['bridges'] = [];
  for (let index = 0; index < nodes.length; index += 1) {
    for (let inner = index + 1; inner < nodes.length; inner += 1) {
      const distanceKm = localDistanceKm(nodes[index], nodes[inner]);
      if (distanceKm > pairLimitKm) continue;
      bridges.push({
        x: (nodes[index].x + nodes[inner].x) / 2,
        y: (nodes[index].y + nodes[inner].y) / 2,
        radiusKm: clamp(distanceKm * 0.62, 18, baseRadiusKm * 1.12),
        weight: clamp(0.16 + (1 - distanceKm / pairLimitKm) * 0.2, 0.16, 0.34),
      });
    }
  }

  const allLobes = [
    ...nodes.map((node) => ({ x: node.x, y: node.y, radiusKm: node.radiusKm })),
    ...bridges.map((bridge) => ({ x: bridge.x, y: bridge.y, radiusKm: bridge.radiusKm })),
  ];
  const minX = Math.min(...allLobes.map((entry) => entry.x - entry.radiusKm * 1.34));
  const maxX = Math.max(...allLobes.map((entry) => entry.x + entry.radiusKm * 1.34));
  const minY = Math.min(...allLobes.map((entry) => entry.y - entry.radiusKm * 1.34));
  const maxY = Math.max(...allLobes.map((entry) => entry.y + entry.radiusKm * 1.34));

  return {
    centerLat,
    centerLon,
    threshold: nodes.length >= 6 ? 0.19 : nodes.length >= 4 ? 0.21 : 0.23,
    minX,
    maxX,
    minY,
    maxY,
    nodes,
    bridges,
  };
}

function evaluateSelectedCorridorFieldStrength(
  metrics: SelectedCorridorFieldMetrics,
  point: LocalPoint,
): number {
  let strength = 0;
  for (const node of metrics.nodes) {
    const distanceKm = localDistanceKm(point, node);
    const normalized = distanceKm / Math.max(node.radiusKm, 1);
    strength += node.weight * Math.exp(-(normalized * normalized) * 1.35);
  }
  for (const bridge of metrics.bridges) {
    const distanceKm = localDistanceKm(point, bridge);
    const normalized = distanceKm / Math.max(bridge.radiusKm, 1);
    strength += bridge.weight * Math.exp(-(normalized * normalized) * 1.6);
  }
  return strength;
}

function buildSelectedCorridorFieldFeatures(
  endpointEntries: Array<{ key: string; object: GeoObject; displayPoint: [number, number]; anchorPoint: [number, number] }>,
  corridorId: string,
  theme: ThemeMode,
): GeoJSON.Feature[] {
  const metrics = buildSelectedCorridorFieldMetrics(endpointEntries);
  if (!metrics) return [];

  const nodeCount = metrics.nodes.length;
  const bridgeCount = metrics.bridges.length;
  const densityLoad = nodeCount + bridgeCount * 0.45;
  const spanXKm = metrics.maxX - metrics.minX;
  const spanYKm = metrics.maxY - metrics.minY;
  const spanKm = Math.max(spanXKm, spanYKm);
  const baseColor = theme === 'dark' ? '#4b5563' : '#64748b';
  const accentColor = theme === 'dark' ? '#64748b' : '#94a3b8';
  const out: GeoJSON.Feature[] = [];
  const opacityScale = clamp(1 / Math.pow(Math.max(densityLoad, 1), 0.46), 0.18, 0.84);
  const radiusScale = clamp(1 / Math.pow(Math.max(nodeCount, 1), 0.08), 0.8, 1);
  const fillOpacityBase = (theme === 'dark' ? 0.046 : 0.038) * opacityScale;
  const canopyScale = nodeCount >= 18 ? 0.54 : nodeCount >= 12 ? 0.6 : nodeCount >= 8 ? 0.68 : 0.76;

  const pushFogFeature = (
    idSuffix: string,
    center: LocalPoint,
    radiusXKm: number,
    radiusYKm: number,
    opacity: number,
    rotationRad: number,
    wobble: number,
  ) => {
    out.push({
      id: `${corridorId}:field:${idSuffix}`,
      type: 'Feature',
      geometry: buildFogPolygon(
        metrics.centerLat,
        metrics.centerLon,
        center,
        radiusXKm,
        radiusYKm,
        rotationRad,
        `${corridorId}:${idSuffix}`,
        wobble,
      ),
      properties: {
        kind: 'corridor-field',
        corridorId,
        color: baseColor,
        fillOpacity: opacity,
        accentColor,
        accentOpacity: 0,
      },
    } as GeoJSON.Feature<GeoJSON.Polygon>);
  };

  const canopyRotation = ((hashString(`${corridorId}:canopy`) % 360) * Math.PI) / 180;
  pushFogFeature(
    'canopy:inner',
    { x: 0, y: 0 },
    clamp(spanXKm * canopyScale, 22, 180),
    clamp(spanYKm * canopyScale, 20, 160),
    fillOpacityBase * 0.92,
    canopyRotation * 0.7,
    0.05,
  );
  if (nodeCount <= 9) {
    pushFogFeature(
      'canopy:outer',
      { x: 0, y: 0 },
      clamp(spanXKm * (canopyScale + 0.1), 28, 240),
      clamp(spanYKm * (canopyScale + 0.08), 24, 210),
      fillOpacityBase * 0.62,
      canopyRotation,
      0.06,
    );
  }

  const nodeEntries = metrics.nodes.length > 14
    ? metrics.nodes.filter((_, index) => index % 2 === 0)
    : metrics.nodes;
  nodeEntries.forEach((node, index) => {
    const normalizedWeight = clamp((node.weight - 0.86) / 0.18, 0, 1);
    const rotationRad = ((hashString(`${corridorId}:node:${node.key}`) % 360) * Math.PI) / 180;
    const coreRadiusX = clamp(node.radiusKm * (1.02 + normalizedWeight * 0.08) * radiusScale, 20, spanKm * 0.26);
    const coreRadiusY = clamp(node.radiusKm * (0.84 + normalizedWeight * 0.06) * radiusScale, 18, spanKm * 0.22);
    if (nodeCount <= 10 || index % 2 === 0) {
      pushFogFeature(
        `node:${index}:outer`,
        { x: node.x, y: node.y },
        coreRadiusX * 1.22,
        coreRadiusY * 1.18,
        fillOpacityBase * (0.42 + normalizedWeight * 0.08),
        rotationRad,
        0.08,
      );
    }
    pushFogFeature(
      `node:${index}:core`,
      { x: node.x, y: node.y },
      coreRadiusX,
      coreRadiusY,
      fillOpacityBase * (0.7 + normalizedWeight * 0.14),
      rotationRad * 0.82,
      0.07,
    );
  });

  const bridgeEntries = [...metrics.bridges]
    .sort((left, right) => right.weight - left.weight)
    .slice(0, nodeCount >= 16 ? 4 : nodeCount >= 10 ? 5 : 7);
  bridgeEntries.forEach((bridge, index) => {
    const normalizedWeight = clamp((bridge.weight - 0.16) / 0.18, 0, 1);
    const rotationRad = ((hashString(`${corridorId}:bridge:${index}`) % 360) * Math.PI) / 180;
    pushFogFeature(
      `bridge:${index}`,
      { x: bridge.x, y: bridge.y },
      clamp(bridge.radiusKm * 1.12 * radiusScale, 14, spanKm * 0.2),
      clamp(bridge.radiusKm * 0.82 * radiusScale, 12, spanKm * 0.16),
      fillOpacityBase * (0.34 + normalizedWeight * 0.1),
      rotationRad,
      0.08,
    );
    if (bridge.weight >= 0.24 && nodeCount <= 8) {
      pushFogFeature(
        `bridge:${index}:echo`,
        { x: bridge.x, y: bridge.y },
        clamp(bridge.radiusKm * 1.48 * radiusScale, 20, spanKm * 0.26),
        clamp(bridge.radiusKm * 0.96 * radiusScale, 14, spanKm * 0.2),
        fillOpacityBase * 0.28,
        rotationRad * 0.72,
        0.08,
      );
    }
  });

  return out;
}

function buildSelectedCorridorMutedObjectIds(
  endpointEntries: Array<{ key: string; object: GeoObject; displayPoint: [number, number]; anchorPoint: [number, number] }>,
  objects: GeoObject[],
  selectedObjectId: string | null,
): string[] {
  if (endpointEntries.length === 0) return [];
  const metrics = buildSelectedCorridorFieldMetrics(endpointEntries);
  if (!metrics) return [];
  const selectedKeys = new Set(endpointEntries.map((entry) => entry.key));
  const muted = new Set<string>();

  for (const object of objects) {
    if (!object.id || object.id === selectedObjectId) continue;
    const pathKey = object.canonicalPath ? `path:${object.canonicalPath}` : '';
    if (selectedKeys.has(`node:${object.id}`) || (pathKey && selectedKeys.has(pathKey))) continue;
    const lat = object.coordinates?.lat;
    const lon = object.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const local = toLocalKm(metrics.centerLat, metrics.centerLon, lat!, lon!);
    const boundsPadKm = Math.max(
      20,
      ...metrics.nodes.map((node) => node.radiusKm * 0.28),
      ...metrics.bridges.map((bridge) => bridge.radiusKm * 0.22),
    );
    if (
      local.x < metrics.minX - boundsPadKm
      || local.x > metrics.maxX + boundsPadKm
      || local.y < metrics.minY - boundsPadKm
      || local.y > metrics.maxY + boundsPadKm
    ) continue;
    const strength = evaluateSelectedCorridorFieldStrength(metrics, local);
    if (strength >= metrics.threshold * 0.82) muted.add(object.id);
  }
  return Array.from(muted);
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function buildSupplyPathFeatures(
  pathArtifacts: MaterializedArtifact[],
  coordsByPath: Map<string, { lon: number; lat: number }>,
  objectByPath: Map<string, TraceObjectInfo>,
  theme: ThemeMode,
  focusObject?: Pick<GeoObject, 'canonicalPath' | 'coordinates'> | null,
): {
  features: GeoJSON.Feature[];
  visiblePathRefs: Set<string>;
  coveredPairs: Set<string>;
  routeCount: number;
} {
  type CandidateRoute = {
    artifactId: string;
    legIndex: number;
    productRef: string;
    narrativeKey: string;
    direction: 'inbound' | 'outbound' | 'context';
    supplierRef: string;
    customerRef: string;
    transportViaRefs: string[];
    coordinates: [number, number][];
    directDistanceKm: number;
    score: number;
    labelText: string;
    criticality: number;
    primaryMechanism?: string;
    riskTag?: string;
  };

  type OrientedCandidateRoute = CandidateRoute & {
    rank: number;
    kind: EdgeVisualKind;
    localPoint: [number, number];
    remotePoint: [number, number];
    azimuthDeg: number;
  };

  const shouldBundleRoute = (_route: OrientedCandidateRoute): boolean => {
    return false;
  };

  const shouldRenderRouteLabel = (route: OrientedCandidateRoute, featureRole: 'overview' | 'detail'): boolean => {
    if (featureRole === 'detail') return false;
    if (route.kind === 'cluster') return false;
    if (supplyFanoutCount >= 10 && route.rank >= 4) return false;
    if (supplyFanoutCount >= 7 && route.transportViaRefs.length > 0 && route.rank >= 2) return false;
    if (route.directDistanceKm >= 5000 && route.rank >= 2) return false;
    return true;
  };

  const byRouteKey = new Map<string, CandidateRoute>();

  for (const artifact of pathArtifacts) {
    const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
    const multiLegPath = legs.length > 1;
    const pathRiskOverlay = artifact.riskOverlay || null;
    for (const leg of legs) {
      const supplierRef = artifactLegString(leg, 'supplierRef', 'SupplierRef');
      const customerRef = artifactLegString(leg, 'customerRef', 'CustomerRef');
      const productRef = artifactLegString(leg, 'productRef', 'ProductRef') || String(artifact.productAxis || '').trim();
      if (!supplierRef || !customerRef || !productRef) continue;
      const legIndex = artifactLegNumber(leg, 'index', 'Index') ?? 0;
      const legRiskOverlay = Array.isArray(pathRiskOverlay?.legs)
        ? (pathRiskOverlay!.legs!.find((item) => Number(item?.legIndex) === legIndex) || null)
        : null;
      const criticality = Math.max(
        Number(legRiskOverlay?.criticality ?? 0),
        Number(pathRiskOverlay?.criticality ?? 0),
        Number(artifact.pathStressScore ?? artifact.payload?.stress?.pathScore ?? 0),
      );
      const primaryMechanism = String(legRiskOverlay?.primaryMechanism || pathRiskOverlay?.primaryMechanism || '').trim() || undefined;
      const riskTag = primaryRiskTag(pathRiskOverlay, legRiskOverlay) || undefined;

      const transportViaRefs = artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs');
      const refs = [supplierRef, ...transportViaRefs, customerRef];
      const coordinates = dedupeSequentialPoints(
        refs
          .map((ref) => coordsByPath.get(ref))
          .filter((point): point is { lon: number; lat: number } => Boolean(point))
          .map((point) => [point.lon, point.lat] as [number, number]),
      );
      if (coordinates.length < 2) continue;

      const directDistanceKm = approximateDistanceKm(
        coordinates[0][0],
        coordinates[0][1],
        coordinates[coordinates.length - 1][0],
        coordinates[coordinates.length - 1][1],
      );
      const routeKey = `${stablePairKey(supplierRef, customerRef)}::${productRef}`;
      const score =
        Math.min(directDistanceKm, 5000) * 0.07
        + transportViaRefs.length * 24
        + (artifactLegStringList(leg, 'flowRefs', 'FlowRefs').length > 0 ? 18 : 0)
        + (artifactLegStringList(leg, 'zoneRefs', 'ZoneRefs').length > 0 ? 10 : 0)
        + (artifactLegStringList(leg, 'affectedZoneRefs', 'AffectedZoneRefs').length > 0 ? 10 : 0)
        + (multiLegPath ? 22 : 0)
        + ((artifact.traceRole === 'route_overlap' || artifact.traceRole === 'route_context') ? 12 : 0)
        + (artifact.isPinned ? 6 : 0)
        + criticality * 90;
      const direction = classifySupplyDirection(String(focusObject?.canonicalPath || '').trim(), supplierRef, customerRef);
      const candidate: CandidateRoute = {
        artifactId: String(artifact.id || '').trim(),
        legIndex,
        productRef,
        narrativeKey: supplyNarrativeKey(direction, productRef, artifact.id),
        direction,
        supplierRef,
        customerRef,
        transportViaRefs,
        coordinates: stitchSupplyRouteCoordinates(
          [coordinates[0], coordinates[coordinates.length - 1]],
          `supply:${routeKey}`,
          directDistanceKm >= 1200 ? 3 : directDistanceKm >= 250 ? 2 : 1,
        ),
        directDistanceKm,
        score,
        labelText: buildSupplyLabelText(artifact, productRef, supplierRef, customerRef, transportViaRefs, objectByPath, focusObject, riskTag),
        criticality,
        primaryMechanism,
        riskTag,
      };
      const existing = byRouteKey.get(routeKey);
      if (!existing || candidate.score > existing.score) {
        byRouteKey.set(routeKey, candidate);
      }
    }
  }

  const ranked = Array.from(byRouteKey.values())
    .sort((left, right) => right.score - left.score)
    .slice(0, 12);
  const supplyFanoutCount = ranked.length;
  const visiblePathRefs = new Set<string>();
  const coveredPairs = new Set<string>();
  const features: GeoJSON.Feature[] = [];

  const pushRouteFeature = (
    route: OrientedCandidateRoute,
    kind: EdgeVisualKind,
    coordinates: [number, number][],
    labelText: string,
    labelRatio: number,
    widthMultiplier = 1,
    opacityBoost = 0,
    colorOverride?: string,
    corridorId?: string,
    featureRole: 'overview' | 'detail' = 'overview',
    bundleSize = 1,
  ) => {
    const lineId = `supply:${route.artifactId || route.productRef}:${route.legIndex}:${hashString(`${route.supplierRef}:${route.customerRef}:${labelText}`)}`;
    const visual = buildSupplyPathVisual(theme, kind, route.directDistanceKm, supplyFanoutCount, bundleSize, route.criticality);
    const color = colorOverride || supplyMechanismColor(theme, kind, route.primaryMechanism) || visual.color;
    const geometry = buildSplitLineGeometry(coordinates, labelRatio);
    const normalizedLabelText = String(labelText || '').trim();
    if (!geometry) return;

    features.push({
      id: lineId,
      type: 'Feature',
      geometry: geometry.geometry,
      properties: {
        kind,
        hop: kind === 'primary' ? 1 : kind === 'indirect' ? 2 : 3,
        color,
        channel: 'supply_path',
        corridorId: corridorId || `supply-route:${lineId}`,
        featureRole,
        endpointPathRefs: [route.supplierRef, route.customerRef],
        narrativeKey: route.narrativeKey,
        primaryMechanism: route.primaryMechanism || '',
        riskTag: route.riskTag || '',
        criticality: route.criticality,
        renderPriority: kind === 'primary' ? 28 : kind === 'indirect' ? 24 : 20,
        baseOpacity: clamp(visual.baseOpacity + opacityBoost, 0.16, 0.82),
        baseWidth: clamp(visual.baseWidth * widthMultiplier, 0.96, 4.6),
        glowOpacity: visual.glowOpacity,
      },
    } as GeoJSON.Feature<GeoJSON.LineString | GeoJSON.MultiLineString>);
    if (normalizedLabelText && shouldRenderRouteLabel(route, featureRole)) {
      features.push({
        id: `${lineId}:label`,
        type: 'Feature',
        geometry: { type: 'Point', coordinates: geometry.labelPoint },
        properties: {
          kind: 'label',
          channel: 'supply_path',
          corridorId: corridorId || `supply-route:${lineId}`,
          featureRole,
          narrativeKey: route.narrativeKey,
          labelText: normalizedLabelText,
          labelColor: color,
          labelPriority: Math.round((route.criticality || 0) * 1000) + (kind === 'primary' ? 300 : kind === 'indirect' ? 180 : 80),
        },
      } as GeoJSON.Feature<GeoJSON.Point>);
    }
  };

  const focusCoords = focusObject?.coordinates
    && Number.isFinite(focusObject.coordinates.lat)
    && Number.isFinite(focusObject.coordinates.lon)
    ? [focusObject.coordinates.lon, focusObject.coordinates.lat] as [number, number]
    : null;
  const focusPath = String(focusObject?.canonicalPath || '').trim();

  const orientedRoutes: OrientedCandidateRoute[] = ranked.map((route, index) => {
    const kind: EdgeVisualKind =
      index < 5 || route.directDistanceKm >= 1200 || route.transportViaRefs.length > 0
        ? 'primary'
        : index < 9 || route.directDistanceKm >= 320
          ? 'indirect'
          : 'cluster';
    const start = route.coordinates[0];
    const end = route.coordinates[route.coordinates.length - 1];
    let localPoint = end;
    let remotePoint = start;
    if (focusPath && route.supplierRef === focusPath) {
      localPoint = start;
      remotePoint = end;
    } else if (focusPath && route.customerRef === focusPath) {
      localPoint = end;
      remotePoint = start;
    } else if (focusCoords) {
      const startDistance = approximateDistanceKm(start[0], start[1], focusCoords[0], focusCoords[1]);
      const endDistance = approximateDistanceKm(end[0], end[1], focusCoords[0], focusCoords[1]);
      if (startDistance < endDistance) {
        localPoint = start;
        remotePoint = end;
      }
    }

    return {
      ...route,
      rank: index,
      kind,
      localPoint,
      remotePoint,
      azimuthDeg: azimuthDegrees(remotePoint, localPoint),
    };
  });

  const bundleGroups = new Map<string, OrientedCandidateRoute[]>();

  for (const route of orientedRoutes) {
    coveredPairs.add(stablePairKey(route.supplierRef, route.customerRef));
    visiblePathRefs.add(route.supplierRef);
    visiblePathRefs.add(route.customerRef);
    route.transportViaRefs.forEach((ref) => visiblePathRefs.add(ref));

    if (!shouldBundleRoute(route)) {
      continue;
    }
    const azimuthBucket = Math.round(route.azimuthDeg / SUPPLY_BUNDLE_AZIMUTH_DEG);
    const distanceBucket = Math.round(route.directDistanceKm / 1200);
    const remoteCell = geoCellKey(route.remotePoint);
    const localCell = geoCellKey(route.localPoint);
    const key = `${route.narrativeKey}:${azimuthBucket}:${distanceBucket}:${remoteCell}:${localCell}`;
    if (!bundleGroups.has(key)) bundleGroups.set(key, []);
    bundleGroups.get(key)!.push(route);
  }

  const bundledRouteIds = new Set<string>();

  for (const [groupKey, group] of bundleGroups.entries()) {
    const localCentroid = averagePoint(group.map((route) => route.localPoint));
    const remoteCentroid = averagePoint(group.map((route) => route.remotePoint));
    const localSpreadKm = Math.max(
      0,
      ...group.map((route) => approximateDistanceKm(route.localPoint[0], route.localPoint[1], localCentroid[0], localCentroid[1])),
    );
    const remoteSpreadKm = Math.max(
      0,
      ...group.map((route) => approximateDistanceKm(route.remotePoint[0], route.remotePoint[1], remoteCentroid[0], remoteCentroid[1])),
    );
    if (localSpreadKm > SUPPLY_BUNDLE_MAX_LOCAL_SPREAD_KM || remoteSpreadKm > SUPPLY_BUNDLE_MAX_REMOTE_SPREAD_KM) {
      continue;
    }

    const strongestKind = group.reduce<EdgeVisualKind>((current, route) => strongerVisualKind(current, route.kind), 'cluster');
    const averageDistanceKm = group.reduce((sum, route) => sum + route.directDistanceKm, 0) / Math.max(1, group.length);
    const averageCriticality = group.reduce((sum, route) => sum + route.criticality, 0) / Math.max(1, group.length);
    const trunkMechanism = group.find((route) => route.primaryMechanism)?.primaryMechanism || '';
    const trunkColor = supplyMechanismColor(theme, strongestKind, trunkMechanism) || supplyStrokeColor(theme, 'primary');
    const trunkVisual = buildSupplyPathVisual(theme, strongestKind, averageDistanceKm, supplyFanoutCount, group.length, averageCriticality);
    const branchPlans = group.map((route) => ({
      route,
      split: branchSplitOnSharedAxis(remoteCentroid, localCentroid, route.localPoint),
    }));
    const trunkEnd = branchPlans.reduce(
      (best, candidate) => (candidate.split.t > best.t ? candidate.split : best),
      { point: remoteCentroid, t: 0 },
    );
    const trunkCoords: [number, number][] = [remoteCentroid, trunkEnd.point];
    const trunkId = `supply-bundle:${groupKey}:${hashString(group.map((route) => route.artifactId).join(':'))}`;

    features.push({
      id: trunkId,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: trunkCoords },
      properties: {
        kind: strongestKind,
        hop: strongestKind === 'primary' ? 1 : strongestKind === 'indirect' ? 2 : 3,
        color: trunkColor,
        channel: 'supply_path',
        corridorId: trunkId,
        featureRole: 'overview',
        narrativeKey: group[0]?.narrativeKey || trunkId,
        primaryMechanism: trunkMechanism,
        riskTag: group[0]?.riskTag || '',
        criticality: averageCriticality,
        renderPriority: strongestKind === 'primary' ? 30 : strongestKind === 'indirect' ? 26 : 22,
        baseOpacity: clamp(trunkVisual.baseOpacity + Math.min(0.08, group.length * 0.012), 0.24, 0.74),
        baseWidth: clamp(trunkVisual.baseWidth + Math.log2(group.length + 1) * 0.4, 1.4, 3.9),
        glowOpacity: clamp(trunkVisual.glowOpacity + 0.01, 0.014, 0.05),
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    features.push({
      id: `${trunkId}:label`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(trunkCoords, 0.5) },
      properties: {
        kind: 'label',
        channel: 'supply_path',
        corridorId: trunkId,
        featureRole: 'overview',
        narrativeKey: group[0]?.narrativeKey || trunkId,
        labelText: buildSupplyBundleLabelText(group.length, group[0], objectByPath),
        labelColor: trunkColor,
        labelPriority: Math.round(averageCriticality * 1000) + 340,
      },
    } as GeoJSON.Feature<GeoJSON.Point>);

    const sortedPlans = [...branchPlans].sort((left, right) => {
      const leftAngle = azimuthDegrees(localCentroid, left.route.localPoint);
      const rightAngle = azimuthDegrees(localCentroid, right.route.localPoint);
      return leftAngle - rightAngle;
    });

    sortedPlans.forEach(({ route, split }) => {
      bundledRouteIds.add(route.artifactId ? `${route.artifactId}:${route.legIndex}` : `${route.supplierRef}:${route.customerRef}:${route.productRef}:${route.legIndex}`);
      const branchCoords = concaveCurveToAxis(split.point, route.localPoint, remoteCentroid, localCentroid);
      const labelRatio = sortedPlans.length > 5 ? 0.8 : 0.72;
      pushRouteFeature(
        route,
        route.kind === 'cluster' ? 'indirect' : route.kind,
        branchCoords,
        route.labelText,
        labelRatio,
        0.9,
        -0.08,
        trunkColor,
        trunkId,
        'detail',
        group.length,
      );
    });
  }

  orientedRoutes.forEach((route) => {
    const routeId = route.artifactId ? `${route.artifactId}:${route.legIndex}` : `${route.supplierRef}:${route.customerRef}:${route.productRef}:${route.legIndex}`;
    if (bundledRouteIds.has(routeId)) return;
    pushRouteFeature(
      route,
      route.kind,
      route.coordinates,
      route.labelText,
      0.5,
      1,
      0,
      undefined,
      undefined,
      'overview',
      1,
    );
  });

  return { features, visiblePathRefs, coveredPairs, routeCount: ranked.length };
}

function buildExplainRelationFeatures(
  centerId: string,
  relations: ExplainRelation[],
  idToCoords: Map<string, { lon: number; lat: number }>,
  theme: ThemeMode,
  section: 'direct' | 'context',
): {
  features: GeoJSON.Feature[];
  visibleNodeIds: Set<string>;
} {
  const center = idToCoords.get(centerId);
  const features: GeoJSON.Feature[] = [];
  const visibleNodeIds = new Set<string>([centerId]);
  if (!center) return { features, visibleNodeIds };

  const start: [number, number] = [center.lon, center.lat];
  const prepared = relations.flatMap((relation, index) => {
    const relatedCoords = idToCoords.get(relation.relatedFragmentId) || relation.relatedCoordinates || null;
    if (!relatedCoords) return [];
    const end: [number, number] = [relatedCoords.lon, relatedCoords.lat];
    return [{
      relation,
      index,
      renderKey: `${section}:${relation.relatedFragmentId}:${relation.type}:${index}`,
      end,
      distanceKm: approximateDistanceKm(start[0], start[1], end[0], end[1]),
      corridorId: `${section}:${relation.relatedFragmentId}`,
      narrativeKey: relation.edgeId || `${section}:${relation.relatedFragmentId}:${relation.type}`,
      labelText: `${relation.relatedLabel}\n${compactMeaning(relation.businessMeaning || humanizeEdgeType(relation.type))}`,
      azimuthDeg: azimuthDegrees(start, end),
    }];
  });

  const directFanoutCount = section === 'direct' ? prepared.length : 0;
  const bundledCoordinateByRelationId = new Map<string, [number, number][]>();
  const bundleSizeByRelationId = new Map<string, number>();

  if (section === 'direct' && directFanoutCount >= 8) {
    const groups = new Map<string, typeof prepared>();
    const azimuthStep = directFanoutCount >= 18 ? 8 : directFanoutCount >= 12 ? 10 : 12;

    for (const item of prepared) {
      const azimuthBucket = Math.round(item.azimuthDeg / azimuthStep);
      const distanceBucket = item.distanceKm >= 2200 ? 3 : item.distanceKm >= 1200 ? 2 : item.distanceKm >= 420 ? 1 : 0;
      const key = `${azimuthBucket}:${distanceBucket}`;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(item);
    }

    for (const group of groups.values()) {
      if (group.length < 3) continue;
      const averageDistanceKm = group.reduce((sum, item) => sum + item.distanceKm, 0) / group.length;
      if (averageDistanceKm < 240) continue;
      const centroid = averagePoint(group.map((item) => item.end));
      const bundleRatio = clamp(
        0.14 + Math.log2(group.length + 1) * 0.026 + (averageDistanceKm >= 1500 ? 0.04 : averageDistanceKm >= 700 ? 0.02 : 0),
        0.14,
        0.26,
      );
      const splitPoint = pointOnSegment(start, centroid, bundleRatio);
      const sortedGroup = [...group].sort((left, right) => left.azimuthDeg - right.azimuthDeg);
      for (const item of sortedGroup) {
        const branch = concaveCurveToAxis(splitPoint, item.end, start, centroid);
        bundledCoordinateByRelationId.set(item.renderKey, dedupeSequentialPoints([start, ...branch]));
        bundleSizeByRelationId.set(item.renderKey, group.length);
      }
    }
  }

  prepared.forEach((item) => {
    const { relation, index, renderKey, end, distanceKm, corridorId, narrativeKey, labelText } = item;
    const kind: EdgeVisualKind = section === 'direct' ? 'primary' : 'indirect';
    const bundleSize = section === 'direct' ? (bundleSizeByRelationId.get(renderKey) || 1) : 1;
    const directVisual = section === 'direct'
      ? buildDirectRelationVisual(theme, distanceKm, directFanoutCount, bundleSize)
      : null;
    const color = directVisual?.color || relationStrokeColor(theme, relation.family, relation.semanticDirection, false);
    const coordinates = section === 'direct'
      ? bundledCoordinateByRelationId.get(renderKey)
        || (distanceKm < 220 ? [start, end] : gentleArcPoints(start, end, `${section}:${relation.relatedFragmentId}:${relation.type}`, distanceKm, 14))
      : gentleArcPoints(start, end, `${section}:${relation.relatedFragmentId}:${relation.type}`, distanceKm, 18);

    features.push({
      id: `${corridorId}:line:${index}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates },
      properties: {
        kind,
        hop: section === 'direct' ? 1 : 2,
        channel: `relation_${section}`,
        corridorId,
        featureRole: 'overview',
        endpointNodeIds: [relation.relatedFragmentId],
        badgeEndpointNodeIds: [relation.relatedFragmentId],
        sourceId: relation.sourceId,
        targetId: relation.targetId,
        narrativeKey,
        relationType: relation.type,
        semanticDirection: relation.semanticDirection || 'context',
        color,
        baseOpacity: directVisual ? directVisual.baseOpacity : (theme === 'dark' ? 0.92 : 0.82),
        baseWidth: directVisual ? directVisual.baseWidth : 2.24,
        glowOpacity: directVisual ? directVisual.glowOpacity : 0.11,
        renderPriority: section === 'direct' ? 26 : 18,
        directDistanceKm: section === 'direct' ? distanceKm : 0,
        directFanoutCount: section === 'direct' ? directFanoutCount : 0,
        directBundleSize: section === 'direct' ? bundleSize : 1,
        directLongHaul: directVisual?.longHaul ? 1 : 0,
        directDenseFanout: directVisual?.denseFanout ? 1 : 0,
        directDistanceFactor: directVisual?.distanceFactor ?? 1,
        directFanoutFactor: directVisual?.fanoutFactor ?? 1,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    features.push({
      id: `${corridorId}:label:${index}`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(coordinates, 0.5) },
      properties: {
        kind: 'label',
        channel: `relation_${section}`,
        corridorId,
        featureRole: 'overview',
        narrativeKey,
        labelText,
        labelColor: color,
      },
    } as GeoJSON.Feature<GeoJSON.Point>);
    visibleNodeIds.add(relation.relatedFragmentId);
  });

  return { features, visibleNodeIds };
}

function buildSimplePropagationFeatures(
  centerId: string,
  propagation: { edges?: Array<{ edgeId?: string; sourceFragmentId?: string; targetFragmentId?: string; hopsFromSource?: number }>; nodeIds?: string[] } | null | undefined,
  idToCoords: Map<string, { lon: number; lat: number }>,
  objectById: Map<string, TraceObjectInfo>,
  theme: ThemeMode,
): GeoJSON.Feature[] {
  const rawEdges = Array.isArray(propagation?.edges) ? propagation.edges : [];
  const features: GeoJSON.Feature[] = [];
  const seen = new Set<string>();
  for (const edge of rawEdges) {
    const sourceId = String(edge?.sourceFragmentId || '').trim();
    const targetId = String(edge?.targetFragmentId || '').trim();
    if (!sourceId || !targetId || shouldSuppressConnection(sourceId, targetId, objectById)) continue;
    const hop = Math.max(1, Math.min(3, Number(edge?.hopsFromSource) || 1));
    const edgeKey = String(edge?.edgeId || '').trim() || `${sourceId}->${targetId}:${hop}`;
    if (seen.has(edgeKey)) continue;
    seen.add(edgeKey);
    const source = idToCoords.get(sourceId);
    const target = idToCoords.get(targetId);
	    if (!source || !target) continue;
	    const kind: EdgeVisualKind = hop <= 1 ? 'primary' : hop === 2 ? 'indirect' : 'cluster';
	    const color = propagationStrokeColor(theme, kind);
	    const propagationOpacityBase = kind === 'primary' ? 0.806 : kind === 'indirect' ? 0.676 : 0.52;
	    const propagationGlowBase = kind === 'cluster' ? 0.052 : 0.104;
	    const coords = gentleArcPoints([source.lon, source.lat], [target.lon, target.lat], `prop:${edgeKey}`, approximateDistanceKm(source.lon, source.lat, target.lon, target.lat), 14);
	    features.push({
      id: `prop:${edgeKey}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: coords },
      properties: {
        kind,
        hop,
        channel: 'propagation',
        corridorId: `prop:${edgeKey}`,
        featureRole: 'overview',
        endpointNodeIds: [sourceId, targetId],
        badgeEndpointNodeIds: [sourceId === centerId ? targetId : sourceId, targetId],
        sourceId,
	        targetId,
	        narrativeKey: 'propagation:overview',
	        semanticDirection: sourceId === centerId ? 'downstream' : targetId === centerId ? 'upstream' : 'context',
	        color,
	        baseOpacity: propagationOpacityBase,
	        baseWidth: kind === 'primary' ? 1.8 : kind === 'indirect' ? 1.5 : 1.15,
	        glowOpacity: propagationGlowBase,
	        renderPriority: kind === 'primary' ? 12 : kind === 'indirect' ? 10 : 8,
	      },
	    } as GeoJSON.Feature<GeoJSON.LineString>);
  }
  return features;
}

function supplyNarrativeKey(direction: 'inbound' | 'outbound' | 'context', productRef?: string, fallbackId?: string): string {
  const productKey = String(productRef || '').trim();
  if (productKey) return `supply:${direction}:${productKey}`;
  return `supply:${direction}:${String(fallbackId || 'unknown').trim() || 'unknown'}`;
}

function normalizeSupplyReasonText(value?: string): string {
  const text = String(value || '').trim();
  if (!text) return '';
  const lowered = text.toLowerCase();
  if (lowered.includes('overlapping graph entities')) {
    return '';
  }
  return text.replace(/\.$/, '');
}

function describeSupplyTraceRole(role?: string, productLabel?: string): string {
  const normalized = String(role || '').trim().toLowerCase();
  const label = String(productLabel || '').trim().toLowerCase();
  if (normalized === 'direct_anchor') {
    return label ? `Direct anchor path for ${label} linked to the selected object` : 'Direct anchor path linked to the selected object';
  }
  if (normalized === 'route_overlap') {
    return label
      ? `Shares intermediate entities with nearby ${label} routes`
      : 'Shares intermediate entities with nearby routes around the selected object';
  }
  if (normalized === 'route_context') {
    return label
      ? `Adds surrounding context for the ${label} network`
      : 'Adds surrounding network context around the selected object';
  }
  if (normalized === 'pinned') {
    return 'Pinned supply path kept visible for this object';
  }
  return '';
}

function labelFromPathRef(value?: string): string {
  const ref = String(value || '').trim();
  return ref ? (humanizeCanonicalPath(ref) || ref) : 'Unspecified destination';
}

function classifySupplyDirection(
  focusPath: string,
  supplierRef: string,
  customerRef: string,
): 'inbound' | 'outbound' | 'context' {
  if (focusPath && focusPath === supplierRef && focusPath !== customerRef) return 'outbound';
  if (focusPath && focusPath === customerRef && focusPath !== supplierRef) return 'inbound';
  return 'context';
}

function analyzeSupplyArtifactPerspective(
  artifact: MaterializedArtifact,
  focusObject?: Pick<GeoObject, 'label' | 'canonicalPath'> | null,
): {
  direction: 'inbound' | 'outbound' | 'context';
  counterpartLabel: string;
  title: string;
  reason: string;
  productRef?: string;
  productLabel: string;
  supplierRef: string;
  customerRef: string;
} {
  const focusPath = String(focusObject?.canonicalPath || '').trim();
  const focusLabel = String(focusObject?.label || '').trim() || labelFromPathRef(focusPath);
  const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
  const firstLeg = legs.length > 0 ? legs[0] : null;
  const productRef = artifactLegString(firstLeg, 'productRef', 'ProductRef') || String(artifact.productAxis || '').trim() || undefined;
  const productLabel = productRef ? (humanizeCanonicalPath(productRef) || productRef) : 'Unspecified product';
  const inboundLeg = legs.find((leg) => artifactLegString(leg, 'customerRef', 'CustomerRef') === focusPath && artifactLegString(leg, 'supplierRef', 'SupplierRef') !== focusPath) || null;
  const outboundLeg = legs.find((leg) => artifactLegString(leg, 'supplierRef', 'SupplierRef') === focusPath && artifactLegString(leg, 'customerRef', 'CustomerRef') !== focusPath) || null;

  let direction: 'inbound' | 'outbound' | 'context' = 'context';
  if (inboundLeg && !outboundLeg) direction = 'inbound';
  else if (outboundLeg && !inboundLeg) direction = 'outbound';
  else {
    const supplierRef = artifactLegString(firstLeg, 'supplierRef', 'SupplierRef') || String(artifact.anchorSupplier || '').trim();
    const customerRef = artifactLegString(firstLeg, 'customerRef', 'CustomerRef') || String(artifact.anchorCustomer || '').trim();
    const fallbackDirection = classifySupplyDirection(focusPath, supplierRef, customerRef);
    const isInternalLoop = Boolean(focusPath) && supplierRef === customerRef && supplierRef === focusPath;
    direction = isInternalLoop ? 'context' : fallbackDirection;
  }

  const sourceLeg = direction === 'inbound' ? inboundLeg : direction === 'outbound' ? outboundLeg : firstLeg;
  const supplierRef = artifactLegString(sourceLeg, 'supplierRef', 'SupplierRef') || String(artifact.anchorSupplier || '').trim();
  const customerRef = artifactLegString(sourceLeg, 'customerRef', 'CustomerRef') || String(artifact.anchorCustomer || '').trim();
  const routeRefs = legs.flatMap((leg) => [
    artifactLegString(leg, 'supplierRef', 'SupplierRef'),
    ...artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs'),
    artifactLegString(leg, 'customerRef', 'CustomerRef'),
  ]).filter(Boolean);
  const internalLoop = Boolean(focusPath) && supplierRef === customerRef && supplierRef === focusPath;
  const resolvedUpstream = traceEndpointLabel(artifact, 'upstream', focusLabel) || traceEndpointLabel(artifact, 'supplier', focusLabel);
  const resolvedDownstream = traceEndpointLabel(artifact, 'downstream', focusLabel) || traceEndpointLabel(artifact, 'customer', focusLabel);
  const resolvedCounterpartLabel = internalLoop
    ? ''
    : direction === 'inbound'
      ? resolvedUpstream
      || pickDistinctSupplyLabel(
          [artifactLegString(sourceLeg, 'supplierRef', 'SupplierRef'), ...artifactLegStringList(sourceLeg, 'transportViaRefs', 'TransportViaRefs')],
          new Map(),
          focusPath,
          focusLabel,
        )
      : direction === 'outbound'
        ? resolvedDownstream
        || pickDistinctSupplyLabel(
            [...artifactLegStringList(sourceLeg, 'transportViaRefs', 'TransportViaRefs').reverse(), artifactLegString(sourceLeg, 'customerRef', 'CustomerRef')],
            new Map(),
            focusPath,
            focusLabel,
          )
        : resolvedDownstream
          || resolvedUpstream
          || pickDistinctSupplyLabel(routeRefs, new Map(), focusPath, focusLabel);
  const counterpartLabel = internalLoop
    ? `Internal processing at ${focusLabel}`
    : resolvedCounterpartLabel || unresolvedSupplyCounterpartLabel(direction, focusLabel);
  const title = direction === 'inbound'
    ? `From ${counterpartLabel}`
    : direction === 'outbound'
      ? `To ${counterpartLabel}`
      : counterpartLabel;
  const role = String(artifact.traceRole || '').trim() || 'path';
  const reason = normalizeSupplyReasonText(artifact.discoveryReason || artifact.traceReason)
    || describeSupplyReason(role, direction, productLabel, !internalLoop && !resolvedCounterpartLabel);
  return {
    direction,
    counterpartLabel,
    title,
    reason,
    productRef,
    productLabel,
    supplierRef,
    customerRef,
  };
}

function buildSupplyDirectionSummary(
  supplyGroups: GeoEdgeTraceSummary['supplyGroups'],
): string {
  if (supplyGroups.length === 0) return '';
  const inboundCount = supplyGroups.reduce((sum, group) => sum + (group.direction === 'inbound' ? group.routeCount : 0), 0);
  const outboundCount = supplyGroups.reduce((sum, group) => sum + (group.direction === 'outbound' ? group.routeCount : 0), 0);
  const contextCount = supplyGroups.reduce((sum, group) => sum + (group.direction === 'context' ? group.routeCount : 0), 0);
  const parts: string[] = [];
  if (inboundCount > 0) parts.push(`${inboundCount} inbound supply route(s)`);
  if (outboundCount > 0) parts.push(`${outboundCount} outbound supply route(s)`);
  if (contextCount > 0) parts.push(`${contextCount} contextual route(s)`);
  return parts.join(', ');
}

function buildExplainSupplyPathItems(
  pathArtifacts: MaterializedArtifact[],
  focusObject?: Pick<GeoObject, 'label' | 'canonicalPath'> | null,
): GeoEdgeTraceSummary['supplyGroups'] {
  const normalized = [...pathArtifacts]
    .sort((left, right) => {
      const leftStress = left.pathStressScore ?? left.payload?.stress?.pathScore ?? 0;
      const rightStress = right.pathStressScore ?? right.payload?.stress?.pathScore ?? 0;
      if (leftStress !== rightStress) return rightStress - leftStress;
      return Number(right.tracePriority || 0) - Number(left.tracePriority || 0);
    })
    .map((artifact) => {
      const perspective = analyzeSupplyArtifactPerspective(artifact, focusObject);
      const stressScore = Number(artifact.pathStressScore ?? artifact.payload?.stress?.pathScore ?? 0);
      return {
        id: artifact.id,
        narrativeKey: supplyNarrativeKey(perspective.direction, perspective.productRef, artifact.id),
        title: perspective.title,
        counterpartLabel: perspective.counterpartLabel,
        direction: perspective.direction,
        destinationLabel: labelFromPathRef(perspective.customerRef),
        role: String(artifact.traceRole || '').trim() || 'path',
        reason: perspective.reason || describeSupplyTraceRole(String(artifact.traceRole || '').trim(), perspective.productLabel) || 'Mapped supply route connected to this object',
        productRef: perspective.productRef,
        productLabel: perspective.productLabel,
        stressScore,
        legCount: Number(artifact.legCount || (Array.isArray(artifact.payload?.legs) ? artifact.payload!.legs!.length : 0) || 0),
        isPinned: Boolean(artifact.isPinned),
        hasBottleneck: typeof (artifact.bottleneckLegIndex ?? artifact.payload?.stress?.bottleneckLegIndex) === 'number'
          && Number(artifact.bottleneckLegIndex ?? artifact.payload?.stress?.bottleneckLegIndex) >= 0,
        primaryMechanism: String(artifact.riskOverlay?.primaryMechanism || '').trim() || undefined,
        riskTag: primaryRiskTag(artifact.riskOverlay || null, null) || undefined,
      };
    });

  const groups = new Map<string, GeoEdgeTraceSummary['supplyGroups'][number]>();
  for (const item of normalized) {
    const key = item.narrativeKey;
    if (!groups.has(key)) {
      groups.set(key, {
        id: key,
        narrativeKey: key,
        direction: item.direction,
        productRef: item.productRef,
        productLabel: item.productLabel,
        summary: '',
        routeCount: 0,
        stressedRouteCount: 0,
        pinnedRouteCount: 0,
        maxStressScore: 0,
        hasBottleneck: false,
        paths: [],
      });
    }
    const group = groups.get(key)!;
    group.routeCount += 1;
    group.stressedRouteCount += item.stressScore >= 0.58 ? 1 : 0;
    group.pinnedRouteCount += item.isPinned ? 1 : 0;
    group.maxStressScore = Math.max(group.maxStressScore, item.stressScore);
    group.hasBottleneck = group.hasBottleneck || item.hasBottleneck;
    group.paths.push({
      id: item.id,
      narrativeKey: item.narrativeKey,
      title: item.title,
      counterpartLabel: item.counterpartLabel,
      direction: item.direction,
      destinationLabel: item.destinationLabel,
      role: item.role,
      reason: item.reason,
      stressScore: item.stressScore,
      legCount: item.legCount,
      isPinned: item.isPinned,
      hasBottleneck: item.hasBottleneck,
      primaryMechanism: item.primaryMechanism,
      riskTag: item.riskTag,
    });
  }

  return Array.from(groups.values())
    .map((group) => {
      group.paths.sort((left, right) => {
        if (left.stressScore !== right.stressScore) return right.stressScore - left.stressScore;
        if (left.legCount !== right.legCount) return right.legCount - left.legCount;
        return left.counterpartLabel.localeCompare(right.counterpartLabel);
      });
      const previewCounterparts = Array.from(
        new Set(
          group.paths
            .map((path) => path.counterpartLabel)
            .filter((value) => value && !isSupplyPlaceholderLabel(value)),
        ),
      ).slice(0, 3);
      if (group.direction === 'inbound') {
        group.summary = previewCounterparts.length > 0
          ? `Consumed from ${previewCounterparts.join(', ')}${group.routeCount > previewCounterparts.length ? ` +${group.routeCount - previewCounterparts.length} more` : ''}`
          : `Consumes through ${group.routeCount} mapped inbound route${group.routeCount > 1 ? 's' : ''}`;
      } else if (group.direction === 'outbound') {
        group.summary = previewCounterparts.length > 0
          ? `Supplied into ${previewCounterparts.join(', ')}${group.routeCount > previewCounterparts.length ? ` +${group.routeCount - previewCounterparts.length} more` : ''}`
          : `Supplied through ${group.routeCount} mapped outbound route${group.routeCount > 1 ? 's' : ''}`;
      } else {
        group.summary = previewCounterparts.length > 0
          ? `Mapped around ${previewCounterparts.join(', ')}${group.routeCount > previewCounterparts.length ? ` +${group.routeCount - previewCounterparts.length} more` : ''}`
          : 'Contextual supply routes linked to this object';
      }
      return group;
    })
    .sort((left, right) => {
      if (left.maxStressScore !== right.maxStressScore) return right.maxStressScore - left.maxStressScore;
      if (left.routeCount !== right.routeCount) return right.routeCount - left.routeCount;
      return left.productLabel.localeCompare(right.productLabel);
    })
    .slice(0, 6);
}

function buildExplainTraceSummary(
  explain: {
    summary?: string;
    whyItMatters?: Array<{ title: string; body: string }>;
    directRelations?: any[];
    contextRelations?: any[];
  } | null | undefined,
  directRelations: ExplainRelation[],
  contextRelations: ExplainRelation[],
  pathArtifacts: MaterializedArtifact[],
  visibleLineCount: number,
  propagation?: { nodeIds?: string[]; edges?: Array<unknown> } | null,
  focusObject?: Pick<GeoObject, 'label' | 'canonicalPath'> | null,
  objectRiskOverlay?: SupplyChainObjectRiskOverlay | null,
): GeoEdgeTraceSummary {
  const supplyGroups = buildExplainSupplyPathItems(pathArtifacts, focusObject);
  const stressedSupplyPathCount = supplyGroups.reduce((sum, group) => sum + group.stressedRouteCount, 0);
  const focusLabel = String(focusObject?.label || '').trim()
    || humanizeCanonicalPath(String(focusObject?.canonicalPath || '').trim())
    || 'This object';
  const fallbackSupplySummary = buildSupplyDirectionSummary(supplyGroups);
  const summary = String(objectRiskOverlay?.summary || '').trim()
    || String(explain?.summary || '').trim()
    || (fallbackSupplySummary ? `${focusLabel} matters here through ${fallbackSupplySummary}.` : '')
    || 'No explanation available for this object yet.';
  const whyItMatters = Array.isArray(explain?.whyItMatters)
    ? explain!.whyItMatters!
      .map((item) => ({ title: String(item?.title || '').trim(), body: String(item?.body || '').trim() }))
      .filter((item) => item.title && item.body)
    : [];
  if (whyItMatters.length === 0 && supplyGroups.length > 0) {
    const inboundGroups = supplyGroups.filter((group) => group.direction === 'inbound');
    const outboundGroups = supplyGroups.filter((group) => group.direction === 'outbound');
    if (inboundGroups.length > 0) {
      whyItMatters.push({
        title: 'Inputs',
        body: `${focusLabel} depends on ${inboundGroups.map((group) => group.productLabel).join(', ')} as inbound supply.`,
      });
    }
    if (outboundGroups.length > 0) {
      whyItMatters.push({
        title: 'Outputs',
        body: `${focusLabel} sends ${outboundGroups.map((group) => group.productLabel).join(', ')} through mapped outbound routes.`,
      });
    }
    if (whyItMatters.length === 0) {
      whyItMatters.push({
        title: 'Supply context',
        body: `${focusLabel} is linked to ${supplyGroups.length} product route group(s) on this map.`,
      });
    }
  }
  return {
    objectRiskOverlay,
    summary,
    whyItMatters,
    directRelations: directRelations.map((relation) => ({
      narrativeKey: relation.edgeId || `direct:${relation.relatedFragmentId}:${relation.type}`,
      edgeId: relation.edgeId,
      relatedFragmentId: relation.relatedFragmentId,
      relatedLabel: relation.relatedLabel,
      relatedType: relation.relatedType,
      relatedCanonicalPath: relation.relatedCanonicalPath,
      type: relation.type,
      family: relation.family,
      semanticDirection: relation.semanticDirection,
      businessMeaning: relation.businessMeaning,
      whyRelevant: relation.whyRelevant,
    })),
    contextRelations: contextRelations.map((relation) => ({
      narrativeKey: relation.edgeId || `context:${relation.relatedFragmentId}:${relation.type}`,
      edgeId: relation.edgeId,
      relatedFragmentId: relation.relatedFragmentId,
      relatedLabel: relation.relatedLabel,
      relatedType: relation.relatedType,
      relatedCanonicalPath: relation.relatedCanonicalPath,
      type: relation.type,
      family: relation.family,
      semanticDirection: relation.semanticDirection,
      businessMeaning: relation.businessMeaning,
      whyRelevant: relation.whyRelevant,
    })),
    supplyGroups,
    propagationNodeCount: Array.isArray(propagation?.nodeIds) ? propagation.nodeIds.length : 0,
    propagationEdgeCount: Array.isArray(propagation?.edges) ? propagation.edges.length : 0,
    visibleLineCount,
    directCount: directRelations.length,
    contextCount: contextRelations.length,
    supplyPathCount: pathArtifacts.length,
    stressedSupplyPathCount,
  };
}

function addEdgeLayers(map: maplibregl.Map, theme: ThemeMode): void {
  const opacityFactor = 0.94;
  map.addLayer({
    id: CORRIDOR_FIELD_LAYER,
    type: 'fill',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor-field'],
    paint: {
      'fill-color': ['get', 'color'],
      'fill-opacity': [
        'interpolate', ['linear'], ['zoom'],
        3, ['*', ['coalesce', ['get', 'fillOpacity'], 0.02], 0.5],
        6, ['*', ['coalesce', ['get', 'fillOpacity'], 0.02], 0.82],
        10, ['*', ['coalesce', ['get', 'fillOpacity'], 0.02], 1.06],
        13, ['*', ['coalesce', ['get', 'fillOpacity'], 0.02], 1.18],
      ],
    },
  }, map.getLayer(GEO_MARKER_LAYER_ANCHOR) ? GEO_MARKER_LAYER_ANCHOR : undefined);

  map.addLayer({
    id: CORRIDOR_FIELD_ACCENT_LAYER,
    type: 'fill',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor-field'],
    paint: {
      'fill-color': ['coalesce', ['get', 'accentColor'], ['get', 'color']],
      'fill-opacity': [
        'interpolate', ['linear'], ['zoom'],
        3, ['*', ['coalesce', ['get', 'accentOpacity'], 0.012], 0.44],
        6, ['*', ['coalesce', ['get', 'accentOpacity'], 0.012], 0.78],
        10, ['*', ['coalesce', ['get', 'accentOpacity'], 0.012], 1.04],
        13, ['*', ['coalesce', ['get', 'accentOpacity'], 0.012], 1.18],
      ],
    },
  }, map.getLayer(GEO_MARKER_LAYER_ANCHOR) ? GEO_MARKER_LAYER_ANCHOR : undefined);

  map.addLayer({
    id: GLOW_LAYER,
    type: 'line',
    source: SRC,
    filter: ['in', ['get', 'kind'], ['literal', ['primary', 'indirect']]],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['case', ['==', ['get', 'channel'], 'relation_direct'], ['+', ['get', 'baseWidth'], 1.15], ['+', ['get', 'baseWidth'], 2.8]],
        ['boolean', ['feature-state', 'hover'], false],
        ['case', ['==', ['get', 'channel'], 'relation_direct'], ['+', ['get', 'baseWidth'], 0.72], ['+', ['get', 'baseWidth'], 2.2]],
        ['==', ['get', 'channel'], 'relation_direct'],
        ['+', ['get', 'baseWidth'], 0.3],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 1.8],
        ['+', ['get', 'baseWidth'], 1.4],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          ['case', ['==', ['get', 'channel'], 'relation_direct'], ['min', 0.12, ['+', ['get', 'glowOpacity'], 0.045]], ['min', 0.32, ['+', ['get', 'glowOpacity'], 0.12]]],
          ['boolean', ['feature-state', 'hover'], false],
          ['case', ['==', ['get', 'channel'], 'relation_direct'], ['min', 0.09, ['+', ['get', 'glowOpacity'], 0.03]], ['min', 0.28, ['+', ['get', 'glowOpacity'], 0.08]]],
          ['get', 'glowOpacity'],
        ],
        opacityFactor,
      ],
      'line-blur': [
        'case',
        ['==', ['get', 'channel'], 'relation_direct'],
        0.7,
        1.1,
      ],
    },
  });

  map.addLayer({
    id: HOP_3_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'cluster'],
      ['!=', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['+', ['get', 'baseWidth'], 2.8],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 2.2],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.65],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        'interpolate', ['linear'], ['zoom'],
        4,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.78, ['+', ['get', 'baseOpacity'], 0.2]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.68, ['+', ['get', 'baseOpacity'], 0.16]],
            ['==', ['get', 'channel'], 'propagation'],
            ['*', ['get', 'baseOpacity'], 0.92],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          1,
        ],
        6,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.78, ['+', ['get', 'baseOpacity'], 0.2]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.68, ['+', ['get', 'baseOpacity'], 0.16]],
            ['==', ['get', 'channel'], 'propagation'],
            ['*', ['get', 'baseOpacity'], 0.92],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.96,
        ],
        8,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.78, ['+', ['get', 'baseOpacity'], 0.2]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.68, ['+', ['get', 'baseOpacity'], 0.16]],
            ['==', ['get', 'channel'], 'propagation'],
            ['*', ['get', 'baseOpacity'], 0.92],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.9,
        ],
        10,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.78, ['+', ['get', 'baseOpacity'], 0.2]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.68, ['+', ['get', 'baseOpacity'], 0.16]],
            ['==', ['get', 'channel'], 'propagation'],
            ['*', ['get', 'baseOpacity'], 0.92],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.84,
        ],
      ],
      'line-dasharray': [
        'case',
        ['==', ['get', 'channel'], 'propagation'],
        ['literal', [1.1, 1.7]],
        ['literal', [1, 0]],
      ],
    },
  });

  map.addLayer({
    id: SUPPLY_HOP_3_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'cluster'],
      ['==', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['+', ['get', 'baseWidth'], 1.1],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.7],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.22],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        'interpolate', ['linear'], ['zoom'],
        4,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.72, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.64, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          1,
        ],
        6,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.72, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.64, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.96,
        ],
        8,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.72, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.64, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.9,
        ],
        10,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.72, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.64, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.84,
        ],
      ],
      'line-dasharray': ['literal', [1, 0]],
    },
  });

  map.addLayer({
    id: HOP_2_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'indirect'],
      ['!=', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-dasharray': [
        'case',
        ['==', ['get', 'channel'], 'propagation'],
        ['literal', [0.8, 1.8]],
        ['==', ['get', 'channel'], 'relation_context'],
        ['literal', [3, 4]],
        ['literal', [1.4, 1.9]],
      ],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['+', ['get', 'baseWidth'], 1.15],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.8],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.45],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        'interpolate', ['linear'], ['zoom'],
        4,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.92, ['+', ['get', 'baseOpacity'], 0.24]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.82, ['+', ['get', 'baseOpacity'], 0.18]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          1,
        ],
        6,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.92, ['+', ['get', 'baseOpacity'], 0.24]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.82, ['+', ['get', 'baseOpacity'], 0.18]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.97,
        ],
        8,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.92, ['+', ['get', 'baseOpacity'], 0.24]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.82, ['+', ['get', 'baseOpacity'], 0.18]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.94,
        ],
        10,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.92, ['+', ['get', 'baseOpacity'], 0.24]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.82, ['+', ['get', 'baseOpacity'], 0.18]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.9,
        ],
      ],
    },
  });

  map.addLayer({
    id: SUPPLY_HOP_2_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'indirect'],
      ['==', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-dasharray': ['literal', [1.4, 1.9]],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['+', ['get', 'baseWidth'], 0.72],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.46],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.18],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        'interpolate', ['linear'], ['zoom'],
        4,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.74, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.66, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          1,
        ],
        6,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.74, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.66, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.97,
        ],
        8,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.74, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.66, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.94,
        ],
        10,
        [
          '*',
          [
            'case',
            ['boolean', ['feature-state', 'active'], false],
            ['min', 0.74, ['+', ['get', 'baseOpacity'], 0.12]],
            ['boolean', ['feature-state', 'hover'], false],
            ['min', 0.66, ['+', ['get', 'baseOpacity'], 0.08]],
            ['get', 'baseOpacity'],
          ],
          opacityFactor,
          0.9,
        ],
      ],
    },
  });

  map.addLayer({
    id: HOP_1_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'primary'],
      ['!=', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['case', ['==', ['get', 'channel'], 'relation_direct'], ['+', ['get', 'baseWidth'], 0.65], ['+', ['get', 'baseWidth'], 1.35]],
        ['boolean', ['feature-state', 'hover'], false],
        ['case', ['==', ['get', 'channel'], 'relation_direct'], ['+', ['get', 'baseWidth'], 0.42], ['+', ['get', 'baseWidth'], 0.95]],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.55],
        ['==', ['get', 'channel'], 'relation_direct'],
        ['+', ['get', 'baseWidth'], 0.14],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          ['case', ['==', ['get', 'channel'], 'relation_direct'], ['min', 0.9, ['+', ['get', 'baseOpacity'], 0.08]], 1],
          ['boolean', ['feature-state', 'hover'], false],
          ['case', ['==', ['get', 'channel'], 'relation_direct'], ['min', 0.84, ['+', ['get', 'baseOpacity'], 0.05]], 1],
          ['==', ['get', 'channel'], 'relation_direct'],
          ['get', 'baseOpacity'],
          ['get', 'baseOpacity'],
        ],
        opacityFactor,
      ],
      'line-dasharray': [
        'case',
        ['==', ['get', 'channel'], 'propagation'],
        ['literal', [1.2, 1.6]],
        ['literal', [1, 0]],
      ],
    },
  });

  map.addLayer({
    id: SUPPLY_HOP_1_LAYER,
    type: 'line',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'primary'],
      ['==', ['get', 'channel'], 'supply_path'],
    ],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        ['+', ['get', 'baseWidth'], 0.78],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.5],
        ['==', ['get', 'channel'], 'supply_path'],
        ['+', ['get', 'baseWidth'], 0.22],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          ['min', 0.78, ['+', ['get', 'baseOpacity'], 0.12]],
          ['boolean', ['feature-state', 'hover'], false],
          ['min', 0.7, ['+', ['get', 'baseOpacity'], 0.08]],
          ['get', 'baseOpacity'],
        ],
        opacityFactor,
      ],
      'line-dasharray': [
        'case',
        ['==', ['get', 'channel'], 'propagation'],
        ['literal', [1.2, 1.6]],
        ['literal', [1, 0]],
      ],
    },
  });

  map.addLayer({
    id: LABEL_LAYER,
    type: 'symbol',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'label'],
      ['!=', ['get', 'channel'], 'supply_path'],
    ],
    layout: {
      'symbol-sort-key': ['coalesce', ['get', 'labelPriority'], 0],
      'text-field': ['get', 'labelText'],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': [
        'interpolate', ['linear'], ['zoom'],
        4, 9.8,
        6, 10.6,
        8, 11.3,
      ],
      'text-max-width': 12,
      'text-offset': [0, -0.25],
      'text-anchor': 'bottom-left',
      'text-allow-overlap': false,
      'text-ignore-placement': false,
    },
    paint: {
      'text-color': ['get', 'labelColor'],
      'text-halo-color': theme === 'dark' ? 'rgba(6,18,31,0.92)' : 'rgba(255,255,255,0.92)',
      'text-halo-width': 1.2,
      'text-opacity': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        0.96,
        ['==', ['get', 'channel'], 'supply_path'],
        0.5,
        ['==', ['get', 'channel'], 'relation_context'],
        0.42,
        ['==', ['get', 'channel'], 'propagation'],
        0.3,
        0.54,
      ],
    },
  });

  map.addLayer({
    id: SUPPLY_LABEL_LAYER,
    type: 'symbol',
    source: SRC,
    filter: [
      'all',
      ['==', ['get', 'kind'], 'label'],
      ['==', ['get', 'channel'], 'supply_path'],
    ],
    layout: {
      'symbol-sort-key': ['coalesce', ['get', 'labelPriority'], 0],
      'icon-image': supplyLabelChipImageId(theme),
      'icon-anchor': 'center',
      'icon-text-fit': 'both',
      'icon-text-fit-padding': [4, 7, 4, 7],
      'text-field': ['get', 'labelText'],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': [
        'interpolate', ['linear'], ['zoom'],
        4, 10,
        6, 10.8,
        8, 11.4,
      ],
      'text-max-width': 13,
      'text-anchor': 'center',
      'text-offset': [0, 0],
      'text-allow-overlap': false,
      'text-ignore-placement': false,
    },
    paint: {
      'icon-opacity': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        0.88,
        0.74,
      ],
      'text-color': theme === 'dark' ? 'rgba(241,245,249,0.92)' : 'rgba(15,23,42,0.84)',
      'text-halo-color': theme === 'dark' ? 'rgba(8,12,18,0.86)' : 'rgba(255,252,247,0.92)',
      'text-halo-width': 0.52,
      'text-opacity': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        0.94,
        0.8,
      ],
    },
  });

  map.addLayer({
    id: ENDPOINT_POINTS_LAYER,
    type: 'symbol',
    source: ENDPOINT_SRC,
    filter: ['==', ['get', 'kind'], 'endpoint-marker'],
    layout: {
      'icon-image': ['get', 'iconKey'],
      'icon-size': 1,
      'icon-allow-overlap': true,
      'icon-ignore-placement': true,
    },
    paint: {
      'icon-opacity': 0.96,
    },
  });

  map.addLayer({
    id: ENDPOINT_LABEL_LAYER,
    type: 'symbol',
    source: ENDPOINT_SRC,
    filter: ['==', ['get', 'kind'], 'endpoint-label'],
    layout: {
      'symbol-sort-key': ['get', 'labelPriority'],
      'icon-image': endpointChipImageId(theme),
      'icon-anchor': 'center',
      'icon-allow-overlap': true,
      'icon-ignore-placement': true,
      'icon-text-fit': 'both',
      'icon-text-fit-padding': [4, 8, 4, 8],
      'text-field': ['get', 'endpointLabel'],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': [
        'interpolate', ['linear'], ['zoom'],
        4, ['case', ['==', ['get', 'labelStrong'], 1], 11.5, 10.5],
        6, ['case', ['==', ['get', 'labelStrong'], 1], 12.25, 11],
        8, ['case', ['==', ['get', 'labelStrong'], 1], 13, 11.5],
        10, ['case', ['==', ['get', 'labelStrong'], 1], 13.5, 12],
      ],
      'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
      'text-radial-offset': [
        'case',
        ['==', ['get', 'labelStrong'], 1],
        1.45,
        1.28,
      ],
      'text-allow-overlap': true,
      'text-ignore-placement': true,
    },
    paint: {
      'icon-opacity': [
        'interpolate', ['linear'], ['zoom'],
        4,
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          0.98,
          ['==', ['get', 'labelChip'], 1],
          ['case', ['==', ['get', 'labelStrong'], 1], 0.9, 0.74],
          0,
        ],
        6.5,
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          0.98,
          ['==', ['get', 'labelChip'], 1],
          ['case', ['==', ['get', 'labelStrong'], 1], 0.96, 0.84],
          0,
        ],
        9,
        [
          'case',
          ['boolean', ['feature-state', 'active'], false],
          0.98,
          ['==', ['get', 'labelChip'], 1],
          ['case', ['==', ['get', 'labelStrong'], 1], 0.99, 0.92],
          0,
        ],
      ],
      'text-color': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        theme === 'dark' ? '#ffffff' : 'rgba(2,6,23,0.98)',
        ['==', ['get', 'labelStrong'], 1],
        theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
        ['==', ['get', 'labelSubdued'], 1],
        theme === 'dark' ? 'rgba(241,245,249,0.88)' : 'rgba(15,23,42,0.82)',
        theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
      ],
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
      'text-halo-width': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        2,
        ['==', ['get', 'labelStrong'], 1],
        1.7,
        ['==', ['get', 'labelChip'], 1],
        1.4,
        1.1,
      ],
      'text-opacity': [
        'case',
        ['boolean', ['feature-state', 'active'], false],
        1,
        ['==', ['get', 'labelVisible'], 1],
        ['case', ['==', ['get', 'labelSubdued'], 1], 0.84, ['==', ['get', 'labelStrong'], 1], 1, 0.94],
        0,
      ],
    },
  });
}

export function useGeoEdgeLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  selectedObject: GeoObject | null,
  objects: GeoObject[],
  theme: ThemeMode,
  hops: number = MAX_HOPS,
  lensId?: string,
) {
  const [edgesLoading, setEdgesLoading] = useState(false);
  const [edgesCount, setEdgesCount] = useState(0);
  const [visibleEdgesCount, setVisibleEdgesCount] = useState(0);
  const [discoveredObjects, setDiscoveredObjects] = useState<GeoObject[]>([]);
  const [traceSummary, setTraceSummary] = useState<GeoEdgeTraceSummary | null>(null);
  const [relatedPathArtifacts, setRelatedPathArtifacts] = useState<MaterializedArtifact[]>([]);
  const [selectionExportData, setSelectionExportData] = useState<GeoEdgeSelectionExportData | null>(null);
  const [mutedObjectIds, setMutedObjectIds] = useState<string[]>([]);
  const [highlightedGatewayObjectIds, setHighlightedGatewayObjectIds] = useState<string[]>([]);
  const [panelNarrativeKey, setPanelNarrativeKey] = useState<string | null>(null);
  const [hoveredNarrativeKey, setHoveredNarrativeKey] = useState<string | null>(null);
  const [featureNarrativeEpoch, setFeatureNarrativeEpoch] = useState(0);
  const mountedRef = useRef(true);
  const featureIdsByNarrativeKeyRef = useRef<Map<string, Array<string | number>>>(new Map());
  const endpointMarkerIdsByNarrativeKeyRef = useRef<Map<string, Array<string | number>>>(new Map());
  const endpointRenderStateRef = useRef<EndpointRenderState | null>(null);
  const activeNarrativeFeatureIdsRef = useRef<Array<string | number>>([]);
  const activeNarrativeEndpointIdsRef = useRef<Array<string | number>>([]);
  const activeNarrativeKey = panelNarrativeKey || hoveredNarrativeKey;

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (!map || !selectedObject || !canAccessMapStyle(map) || !map.getSource(SRC)) return;
    try {
      for (const id of activeNarrativeFeatureIdsRef.current) {
        map.setFeatureState({ source: SRC, id }, { active: false });
      }
      for (const id of activeNarrativeEndpointIdsRef.current) {
        if (map.getSource(ENDPOINT_SRC)) {
          map.setFeatureState({ source: ENDPOINT_SRC, id }, { active: false });
        }
      }
      activeNarrativeFeatureIdsRef.current = [];
      activeNarrativeEndpointIdsRef.current = [];
      if (!activeNarrativeKey) return;
      const featureIds = featureIdsByNarrativeKeyRef.current.get(activeNarrativeKey) || [];
      for (const id of featureIds) {
        map.setFeatureState({ source: SRC, id }, { active: true });
      }
      activeNarrativeFeatureIdsRef.current = featureIds;
      const endpointIds = endpointMarkerIdsByNarrativeKeyRef.current.get(activeNarrativeKey) || [];
      for (const id of endpointIds) {
        if (map.getSource(ENDPOINT_SRC)) {
          map.setFeatureState({ source: ENDPOINT_SRC, id }, { active: true });
        }
      }
      activeNarrativeEndpointIdsRef.current = endpointIds;
    } catch {
      activeNarrativeFeatureIdsRef.current = [];
      activeNarrativeEndpointIdsRef.current = [];
    }
  }, [activeNarrativeKey, featureNarrativeEpoch, map, selectedObject?.id, styleEpoch]);

  useEffect(() => {
    if (!map || !selectedObject || !canAccessMapStyle(map)) return;

    const refreshEndpointLabels = () => {
      const renderState = endpointRenderStateRef.current;
      const endpointSrc = map.getSource(ENDPOINT_SRC) as maplibregl.GeoJSONSource | undefined;
      if (!renderState || !endpointSrc) return;
      endpointSrc.setData(buildEndpointSourceData(renderState, theme, activeNarrativeKey, map, map.getZoom()));
      setFeatureNarrativeEpoch((value) => value + 1);
    };

    refreshEndpointLabels();
    map.on('zoomend', refreshEndpointLabels);
    map.on('moveend', refreshEndpointLabels);
    return () => {
      try {
        map.off('zoomend', refreshEndpointLabels);
        map.off('moveend', refreshEndpointLabels);
      } catch {
        /* handler may already be detached */
      }
    };
  }, [activeNarrativeKey, map, selectedObject?.id, styleEpoch, theme]);

  useEffect(() => {
    if (!map || !isReady) return;
    let hoveredEdgeId: string | number | null = null;

    const clearHoveredEdge = () => {
      if (hoveredEdgeId == null) return;
      try {
        if (canAccessMapStyle(map) && map.getSource(SRC)) {
          map.setFeatureState({ source: SRC, id: hoveredEdgeId }, { hover: false });
        }
      } catch { /* source removed during style transition */ }
      hoveredEdgeId = null;
      setHoveredNarrativeKey(null);
      map.getCanvas().style.cursor = '';
    };

    const setHoveredEdge = (edgeId: string | number | null) => {
      if (hoveredEdgeId === edgeId) return;
      clearHoveredEdge();
      if (edgeId == null) return;
      try {
        if (canAccessMapStyle(map) && map.getSource(SRC)) {
          map.setFeatureState({ source: SRC, id: edgeId }, { hover: true });
          hoveredEdgeId = edgeId;
          map.getCanvas().style.cursor = 'pointer';
        }
      } catch { /* source removed during style transition */ }
    };

    const removeLayers = () => {
      clearHoveredEdge();
      try {
        if (map.getLayer(ENDPOINT_LABEL_LAYER)) map.removeLayer(ENDPOINT_LABEL_LAYER);
        if (map.getLayer(ENDPOINT_POINTS_LAYER)) map.removeLayer(ENDPOINT_POINTS_LAYER);
        if (map.getLayer(SUPPLY_LABEL_LAYER)) map.removeLayer(SUPPLY_LABEL_LAYER);
        if (map.getLayer(CORRIDOR_FIELD_ACCENT_LAYER)) map.removeLayer(CORRIDOR_FIELD_ACCENT_LAYER);
        if (map.getLayer(CORRIDOR_FIELD_LAYER)) map.removeLayer(CORRIDOR_FIELD_LAYER);
        if (map.getLayer(LABEL_LAYER)) map.removeLayer(LABEL_LAYER);
        if (map.getLayer(SUPPLY_HOP_1_LAYER)) map.removeLayer(SUPPLY_HOP_1_LAYER);
        if (map.getLayer(SUPPLY_HOP_2_LAYER)) map.removeLayer(SUPPLY_HOP_2_LAYER);
        if (map.getLayer(SUPPLY_HOP_3_LAYER)) map.removeLayer(SUPPLY_HOP_3_LAYER);
        if (map.getLayer(HOP_1_LAYER)) map.removeLayer(HOP_1_LAYER);
        if (map.getLayer(HOP_2_LAYER)) map.removeLayer(HOP_2_LAYER);
        if (map.getLayer(HOP_3_LAYER)) map.removeLayer(HOP_3_LAYER);
        if (map.getLayer(GLOW_LAYER)) map.removeLayer(GLOW_LAYER);
        if (map.getSource(ENDPOINT_SRC)) map.removeSource(ENDPOINT_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };

    const handleEdgeHover = (event: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feature = event.features?.[0];
      if (!feature || feature.id == null) return;
      setHoveredEdge(feature.id);
      const nextNarrativeKey = String(feature.properties?.narrativeKey || '').trim();
      setHoveredNarrativeKey(nextNarrativeKey || null);
    };

    const handleEdgeLeave = () => {
      clearHoveredEdge();
    };

    const bringEndpointLayersToFront = () => {
      try {
        if (map.getLayer(ENDPOINT_POINTS_LAYER)) map.moveLayer(ENDPOINT_POINTS_LAYER);
        if (map.getLayer(ENDPOINT_LABEL_LAYER)) map.moveLayer(ENDPOINT_LABEL_LAYER);
      } catch { /* style/layers may be mid-transition */ }
    };

    if (!selectedObject) {
      removeLayers();
      setDiscoveredObjects([]);
      setEdgesLoading(false);
      setEdgesCount(0);
      setVisibleEdgesCount(0);
      setTraceSummary(null);
      setRelatedPathArtifacts([]);
      setSelectionExportData(null);
      setMutedObjectIds([]);
      setHighlightedGatewayObjectIds([]);
      setPanelNarrativeKey(null);
      setHoveredNarrativeKey(null);
      featureIdsByNarrativeKeyRef.current = new Map();
      endpointMarkerIdsByNarrativeKeyRef.current = new Map();
      endpointRenderStateRef.current = null;
      activeNarrativeFeatureIdsRef.current = [];
      activeNarrativeEndpointIdsRef.current = [];
      return;
    }

    const requestedHops = Math.min(MAX_HOPS, Math.max(1, hops));
    const idToCoords = new Map<string, { lon: number; lat: number }>();
    const coordsByPath = new Map<string, { lon: number; lat: number }>();
    for (const obj of objects) {
      const lat = obj.coordinates?.lat;
      const lon = obj.coordinates?.lon;
      if (Number.isFinite(lat) && Number.isFinite(lon)) {
        idToCoords.set(obj.id, { lon: lon!, lat: lat! });
        if (obj.canonicalPath) {
          coordsByPath.set(obj.canonicalPath, { lon: lon!, lat: lat! });
        }
      }
    }

    let cancelled = false;
    setEdgesLoading(true);
    setEdgesCount(0);
    setVisibleEdgesCount(0);
    setDiscoveredObjects([]);
    setTraceSummary(null);
    setRelatedPathArtifacts([]);
    setSelectionExportData(null);
    setMutedObjectIds([]);
    setHighlightedGatewayObjectIds([]);
    setPanelNarrativeKey(null);
    setHoveredNarrativeKey(null);
    featureIdsByNarrativeKeyRef.current = new Map();
    endpointMarkerIdsByNarrativeKeyRef.current = new Map();
    endpointRenderStateRef.current = null;
    activeNarrativeFeatureIdsRef.current = [];
    activeNarrativeEndpointIdsRef.current = [];

    api
      .getFragmentTrace(selectedObject.id, {
        direction: 'both',
        includeFragments: true,
        includeCounts: true,
        includeUnindexed: true,
        includeCoordinates: true,
        limit: requestedHops === 1 ? 120 : requestedHops === 2 ? 180 : 260,
        maxEdges: requestedHops === 1 ? 72 : requestedHops === 2 ? 120 : 180,
        maxPerType: requestedHops === 3 ? 20 : 24,
        hops: requestedHops,
        lensId,
      })
      .then(async (res) => {
        if (cancelled || !mountedRef.current) return;

        const serverCoords = res?.graph?.coordinates ?? {};
        const fragments = res?.graph?.fragments ?? [];
        const pathArtifacts = Array.isArray(res?.supplyChains?.chains) ? (res.supplyChains.chains as MaterializedArtifact[]) : [];
        const objectIds = new Set(objects.map((obj) => obj.id));
        const objectById = new Map<string, TraceObjectInfo>(objects.map((obj) => [obj.id, obj] as const));
        const objectByPath = new Map<string, TraceObjectInfo>(
          objects
            .filter((obj) => Boolean(obj.canonicalPath))
            .map((obj) => [obj.canonicalPath, obj] as const),
        );

        for (const [id, coordinates] of Object.entries(serverCoords)) {
          if (coordinates && Number.isFinite(coordinates.lat) && Number.isFinite(coordinates.lon)) {
            idToCoords.set(id, { lon: coordinates.lon, lat: coordinates.lat });
          }
        }

        for (const fragment of fragments) {
          const traceObject = fragmentToTraceObject(fragment, serverCoords);
          if (!traceObject) continue;
          objectById.set(traceObject.id, traceObject);
          if (traceObject.canonicalPath) {
            objectByPath.set(traceObject.canonicalPath, traceObject);
          }
          if (traceObject.canonicalPath && traceObject.coordinates) {
            coordsByPath.set(traceObject.canonicalPath, { lon: traceObject.coordinates.lon, lat: traceObject.coordinates.lat });
          }
        }

        const directRelations = Array.isArray(res?.explain?.directRelations)
          ? res.explain!.directRelations!.map((item) => normalizeExplainRelation(item)).filter((item): item is ExplainRelation => Boolean(item))
          : [];
        const contextRelations = Array.isArray(res?.explain?.contextRelations)
          ? res.explain!.contextRelations!.map((item) => normalizeExplainRelation(item)).filter((item): item is ExplainRelation => Boolean(item))
          : [];
        const { features: directFeatures, visibleNodeIds: directNodeIds } = buildExplainRelationFeatures(
          selectedObject.id,
          directRelations,
          idToCoords,
          theme,
          'direct',
        );
        const { features: contextFeatures, visibleNodeIds: contextNodeIds } = buildExplainRelationFeatures(
          selectedObject.id,
          contextRelations,
          idToCoords,
          theme,
          'context',
        );
        const visibleNodeIds = new Set<string>([...Array.from(directNodeIds), ...Array.from(contextNodeIds)]);
        const {
          features: supplyPathFeatures,
          visiblePathRefs,
          coveredPairs: supplyCoveredPairs,
        } = buildSupplyPathFeatures(pathArtifacts, coordsByPath, objectByPath, theme, selectedObject);
        const propagationFeatures = buildSimplePropagationFeatures(selectedObject.id, res?.propagation || null, idToCoords, objectById, theme)
          .filter((feature) => {
            const sourceId = String(feature.properties?.sourceId || '').trim();
            const targetId = String(feature.properties?.targetId || '').trim();
            const pairKey = sourceId && targetId ? stablePairKey(sourceId, targetId) : '';
            const sourcePath = String(objectById.get(sourceId)?.canonicalPath || '').trim();
            const targetPath = String(objectById.get(targetId)?.canonicalPath || '').trim();
            const canonicalPairKey = sourcePath && targetPath ? stablePairKey(sourcePath, targetPath) : '';
            return !(pairKey && supplyCoveredPairs.has(pairKey)) && !(canonicalPairKey && supplyCoveredPairs.has(canonicalPairKey));
          });
        propagationFeatures.forEach((feature) => {
          const sourceId = String(feature.properties?.sourceId || '').trim();
          const targetId = String(feature.properties?.targetId || '').trim();
          if (sourceId) visibleNodeIds.add(sourceId);
          if (targetId) visibleNodeIds.add(targetId);
        });

        const newDiscovered: GeoObject[] = [];
        for (const fragment of fragments) {
          const fragmentId = String(((fragment as any)?.id ?? (fragment as any)?.ID ?? '')).trim();
          const traceObject = objectById.get(fragmentId);
          if (!traceObject) continue;
          const object = traceObject.coordinates ? fragmentToGeoObject(fragment, serverCoords) : null;
          if (
            object
            && (visibleNodeIds.has(object.id) || visiblePathRefs.has(object.canonicalPath))
            && !objectIds.has(object.id)
          ) {
            newDiscovered.push(object);
            objectIds.add(object.id);
          }
        }

        if (cancelled || !mountedRef.current) return;
        setDiscoveredObjects(newDiscovered);
        setRelatedPathArtifacts(pathArtifacts);
        removeLayers();
        setEdgesCount(directRelations.length + contextRelations.length);
        const visibleObjectById = new Map<string, GeoObject>();
        for (const object of [...objects, ...newDiscovered]) {
          if (!object?.id || visibleObjectById.has(object.id)) continue;
          visibleObjectById.set(object.id, object);
        }
        const assignEndpointTier = (target: Map<string, EndpointLabelTier>, key: string, tier: EndpointLabelTier) => {
          const trimmed = String(key || '').trim();
          if (!trimmed) return;
          const current = target.get(trimmed);
          if (!current || endpointTierPriority(tier) > endpointTierPriority(current)) {
            target.set(trimmed, tier);
          }
        };

        const relatedObjectIds = new Set<string>([selectedObject.id]);
        for (const nodeId of visibleNodeIds) relatedObjectIds.add(nodeId);
        for (const object of visibleObjectById.values()) {
          if (object.canonicalPath && visiblePathRefs.has(object.canonicalPath)) {
            relatedObjectIds.add(object.id);
          }
        }
        const endpointTierByKey = new Map<string, EndpointLabelTier>();
        assignEndpointTier(endpointTierByKey, `node:${selectedObject.id}`, 'selected');
        if (selectedObject.canonicalPath) assignEndpointTier(endpointTierByKey, `path:${selectedObject.canonicalPath}`, 'selected');
        for (const nodeId of directNodeIds) assignEndpointTier(endpointTierByKey, `node:${nodeId}`, nodeId === selectedObject.id ? 'selected' : 'direct');
        for (const nodeId of contextNodeIds) assignEndpointTier(endpointTierByKey, `node:${nodeId}`, nodeId === selectedObject.id ? 'selected' : 'context');
        for (const pathRef of visiblePathRefs) assignEndpointTier(endpointTierByKey, `path:${pathRef}`, pathRef === selectedObject.canonicalPath ? 'selected' : 'path');
        for (const feature of propagationFeatures) {
          for (const ref of featureBadgeEndpointRefs(feature)) assignEndpointTier(endpointTierByKey, ref, 'propagation');
        }

        const endpointEntryByKey = new Map<string, { key: string; object: GeoObject }>();
        for (const nodeId of visibleNodeIds) {
          const object = traceObjectToGeoObject(objectById.get(nodeId));
          if (!object?.coordinates) continue;
          endpointEntryByKey.set(`node:${object.id}`, { key: `node:${object.id}`, object });
        }
        for (const pathRef of visiblePathRefs) {
          const object = traceObjectToGeoObject(objectByPath.get(pathRef));
          if (!object?.coordinates) continue;
          endpointEntryByKey.set(`path:${pathRef}`, { key: `path:${pathRef}`, object });
        }
        const endpointEntries = Array.from(endpointEntryByKey.values());
        const spreadEntries = endpointEntries.length > 0 ? spreadEndpointEntries(map, endpointEntries) : [];
        const remoteEndpointEntries = spreadEntries.filter((entry) =>
          entry.key !== `node:${selectedObject.id}`
          && entry.key !== `path:${String(selectedObject.canonicalPath || '').trim()}`,
        );
        const bushConnectorSeedFeatures = [
          ...directFeatures,
          ...contextFeatures,
          ...supplyPathFeatures,
          ...propagationFeatures,
        ];
        const bushGroups = buildSelectedBushGroups(
          remoteEndpointEntries,
          bushConnectorSeedFeatures,
          objectById,
          objectByPath,
          `focus-bush:${selectedObject.id}`,
        );
        const bushEndpointKeys = new Set<string>(
          bushGroups.flatMap((group) => group.entries.map((entry) => entry.key)),
        );
        const corridorFieldFeatures = bushGroups.flatMap((group) =>
          buildSelectedCorridorFieldFeatures(group.entries, group.id, theme),
        );
        const bushConnectorFeatures = bushGroups.flatMap((group) =>
          buildSelectedBushConnectorFeatures(group, bushConnectorSeedFeatures),
        );
        const mutedObjectIds = new Set<string>();
        for (const group of bushGroups) {
          for (const mutedId of buildSelectedCorridorMutedObjectIds(group.entries, Array.from(visibleObjectById.values()), selectedObject.id)) {
            if (!relatedObjectIds.has(mutedId)) mutedObjectIds.add(mutedId);
          }
        }

        const allFeatures = [
          ...corridorFieldFeatures,
          ...bushConnectorFeatures,
          ...directFeatures,
          ...contextFeatures,
          ...supplyPathFeatures,
          ...propagationFeatures,
        ];
        const featureIdsByNarrativeKey = new Map<string, Array<string | number>>();
        const endpointMarkerIdsByNarrativeKey = new Map<string, Array<string | number>>();
        const narrativeKeysByEndpointKey = new Map<string, string[]>();
        for (const feature of allFeatures) {
          const narrativeKey = String(feature.properties?.narrativeKey || '').trim();
          if (narrativeKey) {
            if (feature.id != null) {
              if (!featureIdsByNarrativeKey.has(narrativeKey)) featureIdsByNarrativeKey.set(narrativeKey, []);
              featureIdsByNarrativeKey.get(narrativeKey)!.push(feature.id);
            }
            for (const endpointKey of featureBadgeEndpointRefs(feature)) {
              if (!endpointKey) continue;
              if (!narrativeKeysByEndpointKey.has(endpointKey)) narrativeKeysByEndpointKey.set(endpointKey, []);
              narrativeKeysByEndpointKey.get(endpointKey)!.push(narrativeKey);
            }
          }
        }
        featureIdsByNarrativeKeyRef.current = featureIdsByNarrativeKey;
        for (const [endpointKey, keys] of narrativeKeysByEndpointKey.entries()) {
          const markerId = endpointMarkerFeatureId(endpointKey);
          const labelId = endpointLabelFeatureId(endpointKey);
          for (const narrativeKey of Array.from(new Set(keys))) {
            if (!endpointMarkerIdsByNarrativeKey.has(narrativeKey)) endpointMarkerIdsByNarrativeKey.set(narrativeKey, []);
            endpointMarkerIdsByNarrativeKey.get(narrativeKey)!.push(markerId);
            endpointMarkerIdsByNarrativeKey.get(narrativeKey)!.push(labelId);
          }
        }
        endpointMarkerIdsByNarrativeKeyRef.current = endpointMarkerIdsByNarrativeKey;
        const endpointRenderState: EndpointRenderState = {
          placedEntries: spreadEntries,
          tierByKey: endpointTierByKey,
          narrativeKeysByEndpointKey,
          forcedVisibleKeys: bushEndpointKeys,
        };
        endpointRenderStateRef.current = endpointRenderState;
        setFeatureNarrativeEpoch((value) => value + 1);
        setMutedObjectIds(Array.from(mutedObjectIds));
        setHighlightedGatewayObjectIds(Array.from(relatedObjectIds).filter((id) => id !== selectedObject.id));
        const visibleLineCount = allFeatures.filter((feature) => feature.geometry?.type === 'LineString').length;
        setVisibleEdgesCount(visibleLineCount);
        const nextTraceSummary = buildExplainTraceSummary(
          res?.explain || null,
          directRelations,
          contextRelations,
          pathArtifacts,
          visibleLineCount,
          res?.propagation || null,
          selectedObject,
          res?.supplyChains?.riskOverlay || null,
        );
        setTraceSummary(nextTraceSummary);
        const nextEndpointFeatureCollection = buildEndpointSourceData(endpointRenderState, theme, activeNarrativeKey, map, map.getZoom());
        setSelectionExportData({
          exportedAt: new Date().toISOString(),
          selectedObject,
          requestedHops,
          lensId,
          trace: res as FragmentTracePayload,
          traceSummary: nextTraceSummary,
          discoveredObjects: newDiscovered,
          relatedPathArtifacts: pathArtifacts,
          directRelations,
          contextRelations,
          visibleNodeIds: Array.from(visibleNodeIds),
          visiblePathRefs: Array.from(visiblePathRefs),
          mutedObjectIds: Array.from(mutedObjectIds),
          highlightedGatewayObjectIds: Array.from(relatedObjectIds).filter((id) => id !== selectedObject.id),
          renderedEdgeFeatureCollection: {
            type: 'FeatureCollection',
            features: allFeatures,
          },
          renderedEndpointFeatureCollection: nextEndpointFeatureCollection,
        });

        if (allFeatures.length === 0 || !canAccessMapStyle(map)) {
          setEdgesLoading(false);
          return;
        }

        await ensureMarkerImages(map, spreadEntries.map((entry) => entry.object), theme);
        ensureEndpointChipImage(map, theme);
        ensureSupplyLabelChipImage(map, theme);
        map.addSource(SRC, { type: 'geojson', data: { type: 'FeatureCollection', features: allFeatures } });
        map.addSource(ENDPOINT_SRC, {
          type: 'geojson',
          data: nextEndpointFeatureCollection,
        });
        addEdgeLayers(map, theme);
        bringEndpointLayersToFront();
        map.on('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.on('mousemove', SUPPLY_HOP_1_LAYER, handleEdgeHover);
        map.on('mousemove', SUPPLY_HOP_2_LAYER, handleEdgeHover);
        map.on('mousemove', SUPPLY_HOP_3_LAYER, handleEdgeHover);
        map.on('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_3_LAYER, handleEdgeLeave);
        map.on('mouseleave', SUPPLY_HOP_1_LAYER, handleEdgeLeave);
        map.on('mouseleave', SUPPLY_HOP_2_LAYER, handleEdgeLeave);
        map.on('mouseleave', SUPPLY_HOP_3_LAYER, handleEdgeLeave);
        if (mountedRef.current) setEdgesLoading(false);
      })
      .catch(() => {
        if (!cancelled && mountedRef.current) {
          setDiscoveredObjects([]);
          setEdgesLoading(false);
          setEdgesCount(0);
          setVisibleEdgesCount(0);
          setTraceSummary(null);
          setRelatedPathArtifacts([]);
          setSelectionExportData(null);
          setMutedObjectIds([]);
          setHighlightedGatewayObjectIds([]);
          setPanelNarrativeKey(null);
          setHoveredNarrativeKey(null);
          featureIdsByNarrativeKeyRef.current = new Map();
          endpointMarkerIdsByNarrativeKeyRef.current = new Map();
          endpointRenderStateRef.current = null;
          activeNarrativeFeatureIdsRef.current = [];
          activeNarrativeEndpointIdsRef.current = [];
        }
      });

    return () => {
      cancelled = true;
      clearHoveredEdge();
      try {
        map.off('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.off('mousemove', SUPPLY_HOP_1_LAYER, handleEdgeHover);
        map.off('mousemove', SUPPLY_HOP_2_LAYER, handleEdgeHover);
        map.off('mousemove', SUPPLY_HOP_3_LAYER, handleEdgeHover);
        map.off('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_3_LAYER, handleEdgeLeave);
        map.off('mouseleave', SUPPLY_HOP_1_LAYER, handleEdgeLeave);
        map.off('mouseleave', SUPPLY_HOP_2_LAYER, handleEdgeLeave);
        map.off('mouseleave', SUPPLY_HOP_3_LAYER, handleEdgeLeave);
      } catch { /* handlers may not be attached */ }
      removeLayers();
    };
  }, [selectedObject?.id, objects, hops, map, isReady, styleEpoch, theme, lensId]);

  return {
    edgesLoading,
    edgesCount,
    visibleEdgesCount,
    discoveredObjects,
    traceSummary,
    relatedPathArtifacts,
    selectionExportData,
    mutedObjectIds,
    highlightedGatewayObjectIds,
    activeNarrativeKey,
    setActiveNarrativeKey: setPanelNarrativeKey,
  };
}
