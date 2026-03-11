import { useEffect, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import { api } from '../lib/api';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { buildPointFeature, ensureMarkerImages } from './useGeoMarkerLayer';
import { MAP_TEXT_FONT_STACK } from './useMapInstance';
import { geoCodeToShortName, humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'geo-edges';
const ENDPOINT_SRC = 'geo-edge-endpoints';
const GLOW_LAYER = 'geo-edges-glow';
const HOP_3_LAYER = 'geo-edges-hop-3';
const HOP_2_LAYER = 'geo-edges-hop-2';
const HOP_1_LAYER = 'geo-edges-hop-1';
const LABEL_LAYER = 'geo-edges-label';
const CORRIDOR_FIELD_LAYER = 'geo-edges-corridor-field';
const CORRIDOR_FIELD_ACCENT_LAYER = 'geo-edges-corridor-field-accent';
const CORRIDOR_GUIDE_LAYER = 'geo-edges-corridor-guide';
const CORRIDOR_GLOW_LAYER = 'geo-edges-corridor-glow';
const CORRIDOR_LAYER = 'geo-edges-corridor';
const CORRIDOR_ICON_LAYER = 'geo-edges-corridor-icon';
const CORRIDOR_LABEL_LAYER = 'geo-edges-corridor-label';
const ENDPOINT_CALLOUT_LAYER = 'geo-edge-endpoints-callout';
const ENDPOINT_POINTS_LAYER = 'geo-edge-endpoints-points';
const ENDPOINT_LABEL_LAYER = 'geo-edge-endpoints-label';
const MAX_HOPS = 3;

/** Layer IDs that should prevent map click from clearing selection (corridor badges, edges, endpoints). */
export const GEO_EDGE_INTERACTIVE_LAYERS = [
  CORRIDOR_FIELD_LAYER,
  CORRIDOR_FIELD_ACCENT_LAYER,
  CORRIDOR_GLOW_LAYER,
  CORRIDOR_GUIDE_LAYER,
  CORRIDOR_LAYER,
  CORRIDOR_ICON_LAYER,
  CORRIDOR_LABEL_LAYER,
  ENDPOINT_POINTS_LAYER,
  HOP_1_LAYER,
  HOP_2_LAYER,
  HOP_3_LAYER,
  GLOW_LAYER,
  LABEL_LAYER,
] as const;

type ThemeMode = 'dark' | 'light';
type EdgeFamily = 'supply' | 'control' | 'influence' | 'location' | 'market' | 'risk' | 'social' | 'other';
type EdgeVisualKind = 'primary' | 'indirect' | 'cluster';
type LocalPoint = { x: number; y: number };

export interface GeoEdgeTraceSummary {
  summary: string;
  whyItMatters: Array<{ title: string; body: string }>;
  directRelations: Array<{
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
  supplyPaths: Array<{
    id: string;
    title: string;
    role: string;
    reason: string;
    productRef?: string;
    productLabel?: string;
    stressScore: number;
    legCount: number;
    isPinned: boolean;
    hasBottleneck: boolean;
  }>;
  propagationNodeCount: number;
  propagationEdgeCount: number;
  visibleLineCount: number;
  directCount: number;
  contextCount: number;
  supplyPathCount: number;
  stressedSupplyPathCount: number;
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
const CORRIDOR_SPATIAL_CLUSTER_LINK_KM = 900;
const ENDPOINT_MARKER_SIZE_PX = 28;
const ENDPOINT_MARKER_PADDING_PX = 4;
const ENDPOINT_LABEL_OFFSET_X_PX = 14;
const ENDPOINT_LABEL_HEIGHT_PX = 15;
const ENDPOINT_LABEL_CHAR_WIDTH_PX = 6.4;
const ENDPOINT_LABEL_MAX_WIDTH_PX = 220;

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
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

function extractCountryCodes(canonicalPath?: string): string[] {
  if (!canonicalPath) return [];
  const parts = canonicalPath.toLowerCase().split('/').filter(Boolean);
  if (parts.length < 2) return [];
  const namespace = parts[0];
  if (['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community', 'zone'].includes(namespace)) {
    const segment = parts[1] || '';
    return segment
      .split('-')
      .map((value) => value.trim().toUpperCase())
      .filter((value) => value.length === 2 && /^[A-Z]{2}$/.test(value) && value !== 'NA');
  }
  return [];
}

function getCountryDisplay(canonicalPath?: string): { iso2: string | null; name: string } {
  const codes = extractCountryCodes(canonicalPath);
  if (codes.length === 0) {
    return { iso2: null, name: 'Cross-border / unknown' };
  }
  return {
    iso2: codes.join('-'),
    name: codes.map((code) => geoCodeToShortName(code)).join(' / '),
  };
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
  const semantic = String(semanticDirection || '').trim().toLowerCase();
  if (family === 'supply') return theme === 'dark' ? (emphasize ? '#fbbf24' : '#f59e0b') : (emphasize ? '#d97706' : '#b45309');
  if (family === 'control') return theme === 'dark' ? (emphasize ? '#93c5fd' : '#60a5fa') : (emphasize ? '#2563eb' : '#1d4ed8');
  if (family === 'risk' || family === 'influence') return theme === 'dark' ? (emphasize ? '#fda4af' : '#fb7185') : (emphasize ? '#e11d48' : '#be123c');
  if (family === 'location' || family === 'market') return theme === 'dark' ? (emphasize ? '#5eead4' : '#2dd4bf') : (emphasize ? '#0f766e' : '#0d9488');
  if (family === 'social') return theme === 'dark' ? '#c4b5fd' : '#7c3aed';
  if (semantic === 'upstream') return theme === 'dark' ? '#cbd5f5' : '#64748b';
  if (semantic === 'downstream') return theme === 'dark' ? '#dbeafe' : '#475569';
  return theme === 'dark' ? '#cbd5e1' : '#64748b';
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
  if (theme === 'dark') {
    if (kind === 'primary') return '#f59e0b';
    if (kind === 'indirect') return '#fbbf24';
    return '#fcd34d';
  }
  if (kind === 'primary') return '#b45309';
  if (kind === 'indirect') return '#d97706';
  return '#f59e0b';
}

function primaryStrokeColor(theme: ThemeMode): string {
  return theme === 'dark' ? '#60a5fa' : '#2563eb';
}

function propagationStrokeColor(theme: ThemeMode, kind: EdgeVisualKind): string {
  if (theme === 'dark') {
    if (kind === 'primary') return '#cbd5e1';
    if (kind === 'indirect') return '#94a3b8';
    return '#64748b';
  }
  if (kind === 'primary') return '#64748b';
  if (kind === 'indirect') return '#94a3b8';
  return '#cbd5e1';
}

function corridorBadgeColor(
  theme: ThemeMode,
  corridorType: 'supply' | 'control' | 'dependency' | 'signal' | 'context',
): string {
  void corridorType;
  return supplyStrokeColor(theme, 'primary');
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
  const text = String(value || '').trim();
  if (!text) return '';
  if (text.length <= maxLength) return text;
  return `${text.slice(0, maxLength - 3).trim()}...`;
}

function buildSupplyLabelText(
  productRef: string,
  supplierRef: string,
  customerRef: string,
  transportViaRefs: string[],
  objectByPath: Map<string, TraceObjectInfo>,
): string {
  const productLabel = compactLabel(displayNameByPath(productRef, objectByPath), 28);
  if (transportViaRefs.length > 0) {
    const viaLabel = compactLabel(displayNameByPath(transportViaRefs[0], objectByPath), 24);
    return viaLabel ? `${productLabel}\nvia ${viaLabel}` : productLabel;
  }
  const supplierLabel = compactLabel(displayNameByPath(supplierRef, objectByPath), 16);
  const customerLabel = compactLabel(displayNameByPath(customerRef, objectByPath), 16);
  if (!supplierLabel || !customerLabel) return productLabel;
  return `${productLabel}\n${supplierLabel} -> ${customerLabel}`;
}

function buildSupplyBundleLabelText(
  routesCount: number,
  sampleRoute: { productRef: string; supplierRef: string; customerRef: string },
  objectByPath: Map<string, TraceObjectInfo>,
): string {
  const productLabel = compactLabel(displayNameByPath(sampleRoute.productRef, objectByPath), 24);
  if (routesCount <= 1) return productLabel;
  return `${productLabel}\nshared corridor x${routesCount}`;
}

type RadialBundleRoute = {
  routeId: string;
  targetId: string;
  corridorId?: string;
  endPoint: [number, number];
  gatewayNodeIds?: string[];
  badgeEndpointNodeIds?: string[];
  kind: EdgeVisualKind;
  color: string;
  baseOpacity: number;
  baseWidth: number;
  glowOpacity: number;
  renderPriority: number;
  labelText?: string;
};

function buildRadialBundleFeatures(
  sourcePoint: [number, number] | null,
  routes: RadialBundleRoute[],
  theme: ThemeMode,
  channel: 'trace' | 'propagation',
  bundleLabelText: (group: RadialBundleRoute[]) => string,
  options?: { labelSingles?: boolean; labelBundles?: boolean },
) : { features: GeoJSON.Feature[]; corridorAssignments: Map<string, string> } {
  if (!sourcePoint || routes.length === 0) return { features: [], corridorAssignments: new Map() };
  const features: GeoJSON.Feature[] = [];
  const corridorAssignments = new Map<string, string>();
  const groups = new Map<string, RadialBundleRoute[]>();
  const bundledIds = new Set<string>();
  const labelSingles = options?.labelSingles ?? true;
  const labelBundles = options?.labelBundles ?? true;

  for (const route of routes) {
    const distanceKm = approximateDistanceKm(sourcePoint[0], sourcePoint[1], route.endPoint[0], route.endPoint[1]);
    if (distanceKm < RADIAL_BUNDLE_MIN_DISTANCE_KM) continue;
    const azimuthBucket = Math.round(azimuthDegrees(sourcePoint, route.endPoint) / SUPPLY_BUNDLE_AZIMUTH_DEG);
    const distanceBucket = Math.round(distanceKm / 1200);
    const remoteCell = geoCellKey(route.endPoint);
    const key = `${azimuthBucket}:${distanceBucket}:${remoteCell}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(route);
  }

  for (const [groupKey, group] of groups.entries()) {
    const centroid = averagePoint(group.map((route) => route.endPoint));
    const spreadKm = Math.max(
      0,
      ...group.map((route) => approximateDistanceKm(route.endPoint[0], route.endPoint[1], centroid[0], centroid[1])),
    );
    if (spreadKm > SUPPLY_BUNDLE_MAX_REMOTE_SPREAD_KM) continue;

    const strongestKind = group.reduce<EdgeVisualKind>((current, route) => strongerVisualKind(current, route.kind), 'cluster');
    const trunkColor = channel === 'propagation'
      ? propagationStrokeColor(theme, strongestKind)
      : primaryStrokeColor(theme);
    const gatewayNodeIds = Array.from(new Set(group.flatMap((route) => route.gatewayNodeIds || []).filter(Boolean)));
    const corridorId = `${channel}:corridor:${groupKey}:${hashString(group.map((route) => route.routeId).join(':'))}`;
    group.forEach((route) => corridorAssignments.set(route.targetId, corridorId));
    const branchPlans = group.map((route) => ({
      route,
      split: branchSplitOnSharedAxis(sourcePoint, centroid, route.endPoint),
    }));
    const trunkEnd = branchPlans.reduce(
      (best, candidate) => (candidate.split.t > best.t ? candidate.split : best),
      { point: sourcePoint, t: 0 },
    );
    const trunkCoords: [number, number][] = [sourcePoint, trunkEnd.point];
    const trunkId = `${channel}-bundle:${groupKey}:${hashString(group.map((route) => route.routeId).join(':'))}`;
    features.push({
      id: trunkId,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: trunkCoords },
      properties: {
        kind: strongestKind,
        hop: strongestKind === 'primary' ? 1 : strongestKind === 'indirect' ? 2 : 3,
        color: trunkColor,
        channel,
        corridorId,
        gatewayNodeIds,
        badgeEndpointNodeIds: Array.from(new Set(group.flatMap((route) => route.badgeEndpointNodeIds || [route.targetId]).filter(Boolean))),
        featureRole: 'overview',
        renderPriority: Math.max(...group.map((route) => route.renderPriority)) + 2,
        baseOpacity: clamp(Math.max(...group.map((route) => route.baseOpacity)) + Math.min(0.16, group.length * 0.025), 0.34, 0.96),
        baseWidth: clamp(Math.max(...group.map((route) => route.baseWidth)) + Math.log2(group.length + 1) * 0.95, 1.8, 8),
        glowOpacity: clamp(Math.max(...group.map((route) => route.glowOpacity)) + 0.03, 0.04, 0.28),
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    if (labelBundles) {
      features.push({
        id: `${trunkId}:label`,
        type: 'Feature',
        geometry: { type: 'Point', coordinates: linePointAt(trunkCoords, 0.56) },
        properties: {
          kind: 'label',
          corridorId,
          featureRole: 'overview',
          labelText: group.length === 1 ? (group[0].labelText || bundleLabelText(group)) : bundleLabelText(group),
          labelColor: trunkColor,
        },
      } as GeoJSON.Feature<GeoJSON.Point>);
    }

    const sortedPlans = [...branchPlans].sort((left, right) => {
      const leftAngle = azimuthDegrees(centroid, left.route.endPoint);
      const rightAngle = azimuthDegrees(centroid, right.route.endPoint);
      return leftAngle - rightAngle;
    });
    sortedPlans.forEach(({ route, split }) => {
      bundledIds.add(route.routeId);
      const branchCoords = concaveCurveToAxis(split.point, route.endPoint, sourcePoint, centroid);
      features.push({
        id: `${route.routeId}:branch`,
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: branchCoords },
        properties: {
          kind: route.kind === 'cluster' ? 'indirect' : route.kind,
          hop: route.kind === 'primary' ? 1 : route.kind === 'indirect' ? 2 : 3,
          color: trunkColor,
          channel,
          corridorId,
          gatewayNodeIds: route.gatewayNodeIds || gatewayNodeIds,
          badgeEndpointNodeIds: route.badgeEndpointNodeIds || [route.targetId],
          featureRole: 'detail',
          endpointNodeIds: [route.targetId],
          renderPriority: route.renderPriority,
          baseOpacity: clamp(route.baseOpacity - 0.08, 0.22, 0.92),
          baseWidth: clamp(route.baseWidth * 0.82, 0.9, 4.8),
          glowOpacity: clamp(route.glowOpacity * 0.75, 0.02, 0.18),
        },
      } as GeoJSON.Feature<GeoJSON.LineString>);
    });
  }

  for (const route of routes) {
    if (bundledIds.has(route.routeId)) continue;
    const corridorId = route.corridorId || `${channel}:corridor:${route.targetId}`;
    corridorAssignments.set(route.targetId, corridorId);
    const coords: [number, number][] = route.kind === 'primary'
      ? [sourcePoint, route.endPoint]
      : smoothRouteCoordinates([sourcePoint, route.endPoint], route.routeId, route.kind === 'indirect' ? 2 : 3);
    features.push({
      id: route.routeId,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: coords },
      properties: {
        kind: route.kind,
        hop: route.kind === 'primary' ? 1 : route.kind === 'indirect' ? 2 : 3,
        color: route.color,
        channel,
        corridorId,
        gatewayNodeIds: route.gatewayNodeIds || [],
        badgeEndpointNodeIds: route.badgeEndpointNodeIds || [route.targetId],
        featureRole: 'overview',
        endpointNodeIds: [route.targetId],
        renderPriority: route.renderPriority,
        baseOpacity: route.baseOpacity,
        baseWidth: route.baseWidth,
        glowOpacity: route.glowOpacity,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    if (labelSingles && route.labelText) {
      features.push({
        id: `${route.routeId}:label`,
        type: 'Feature',
        geometry: { type: 'Point', coordinates: linePointAt(coords, 0.22) },
        properties: {
          kind: 'label',
          corridorId,
          featureRole: 'overview',
          labelText: route.labelText,
          labelColor: route.color,
        },
      } as GeoJSON.Feature<GeoJSON.Point>);
    }
  }

  return { features, corridorAssignments };
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

function buildEndpointFeatureCollection(
  endpointEntries: Array<{ key: string; object: GeoObject; displayPoint?: [number, number]; anchorPoint?: [number, number] }>,
  theme: ThemeMode,
): GeoJSON.FeatureCollection {
  return {
    type: 'FeatureCollection',
    features: endpointEntries.flatMap(({ key, object, displayPoint, anchorPoint }) => {
      const actualPoint: [number, number] = [object.coordinates!.lon, object.coordinates!.lat];
      const point = displayPoint || actualPoint;
      const output: GeoJSON.Feature[] = [];
      if (
        anchorPoint
        && (Math.abs(anchorPoint[0] - point[0]) > 1e-6 || Math.abs(anchorPoint[1] - point[1]) > 1e-6)
      ) {
        output.push({
          id: `${key}:callout`,
          type: 'Feature',
          geometry: { type: 'LineString', coordinates: [anchorPoint, point] },
          properties: {
            kind: 'endpoint-callout',
            endpointKey: key,
          },
        } as GeoJSON.Feature<GeoJSON.LineString>);
      }
      const feature = buildPointFeature({
        ...object,
        coordinates: { lat: point[1], lon: point[0] },
      }, theme) as GeoJSON.Feature<GeoJSON.Point>;
      feature.properties = {
        ...(feature.properties || {}),
        kind: 'endpoint-marker',
        endpointKey: key,
        endpointLabel: String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath,
      };
      output.push(feature);
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
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath;
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

function endpointLabelRect(point: { x: number; y: number; entry: { object: GeoObject } }): ScreenRect {
  const label = endpointLabelText(point.entry.object);
  const width = clamp(label.length * ENDPOINT_LABEL_CHAR_WIDTH_PX, 34, ENDPOINT_LABEL_MAX_WIDTH_PX);
  return {
    left: point.x + ENDPOINT_LABEL_OFFSET_X_PX,
    right: point.x + ENDPOINT_LABEL_OFFSET_X_PX + width,
    top: point.y - ENDPOINT_LABEL_HEIGHT_PX / 2,
    bottom: point.y + ENDPOINT_LABEL_HEIGHT_PX / 2,
  };
}

function endpointsOverlap(
  a: { x: number; y: number; entry: { object: GeoObject } },
  b: { x: number; y: number; entry: { object: GeoObject } },
): boolean {
  const markerA = endpointMarkerRect(a);
  const markerB = endpointMarkerRect(b);
  const labelA = endpointLabelRect(a);
  const labelB = endpointLabelRect(b);
  return (
    rectsOverlap(markerA, markerB, ENDPOINT_MARKER_PADDING_PX)
    || rectsOverlap(labelA, labelB, 4)
    || rectsOverlap(labelA, markerB, 4)
    || rectsOverlap(labelB, markerA, 4)
  );
}

function inferCorridorType(channel: string, labelHints: string[]): 'supply' | 'control' | 'dependency' | 'signal' | 'context' {
  if (channel === 'supply_path') return 'supply';
  if (channel === 'propagation') return 'signal';
  const combined = labelHints.join(' ').toLowerCase();
  if (combined.includes('control') || combined.includes('ownership')) return 'control';
  if (combined.includes('dependency') || combined.includes('supply')) return 'dependency';
  return 'context';
}

function corridorTypeLabel(corridorType: 'supply' | 'control' | 'dependency' | 'signal' | 'context'): string {
  if (corridorType === 'supply') return 'Supply';
  if (corridorType === 'control') return 'Control';
  if (corridorType === 'dependency') return 'Dependency';
  if (corridorType === 'signal') return 'Signal';
  return 'Context';
}

function buildCorridorDestinationLabel(objects: GeoObject[]): string {
  if (objects.length === 0) return 'remote cluster';
  const countryCounts = new Map<string, number>();
  for (const object of objects) {
    const display = getCountryDisplay(object.canonicalPath);
    const key = display.name || 'Cross-border / unknown';
    countryCounts.set(key, (countryCounts.get(key) || 0) + 1);
  }
  const sortedCountries = Array.from(countryCounts.entries()).sort((left, right) => right[1] - left[1]);
  const top = sortedCountries[0]?.[0] || '';
  if (sortedCountries.length === 1) return top;
  return `${top} +${sortedCountries.length - 1}`;
}

function buildCorridorOverviewFeatures(
  features: GeoJSON.Feature[],
  objectById: Map<string, TraceObjectInfo>,
  objectByPath: Map<string, TraceObjectInfo>,
  theme: ThemeMode,
  centerPoint: [number, number] | null,
): GeoJSON.Feature[] {
  type CorridorBucket = {
    corridorId: string;
    channel: string;
    lineFeatures: GeoJSON.Feature<GeoJSON.LineString>[];
    endpointKeys: Set<string>;
    labelHints: string[];
  };

  const buckets = new Map<string, CorridorBucket>();
  for (const feature of features) {
    const corridorId = String(feature.properties?.corridorId || '').trim();
    if (!corridorId) continue;
    if (!buckets.has(corridorId)) {
      buckets.set(corridorId, {
        corridorId,
        channel: String(feature.properties?.channel || '').trim(),
        lineFeatures: [],
        endpointKeys: new Set<string>(),
        labelHints: [],
      });
    }
    const bucket = buckets.get(corridorId)!;
    if (!bucket.channel) bucket.channel = String(feature.properties?.channel || '').trim();
    const kind = String(feature.properties?.kind || '').trim();
    if (kind === 'label') {
      bucket.labelHints.push(String(feature.properties?.labelText || '').trim());
      continue;
    }
    if (feature.geometry?.type !== 'LineString') continue;
    bucket.lineFeatures.push(feature as GeoJSON.Feature<GeoJSON.LineString>);
    featureBadgeEndpointRefs(feature).forEach((key) => bucket.endpointKeys.add(key));
  }

  const overview: GeoJSON.Feature[] = [];
  for (const bucket of buckets.values()) {
    const endpointObjects = Array.from(bucket.endpointKeys)
      .map((key) => {
        if (key.startsWith('node:')) return traceObjectToGeoObject(objectById.get(key.slice(5)));
        if (key.startsWith('path:')) return traceObjectToGeoObject(objectByPath.get(key.slice(5)));
        return null;
      })
      .filter((object): object is GeoObject => Boolean(object?.coordinates));
    if (bucket.lineFeatures.length === 0) continue;

    const anchorPoint = endpointObjects.length > 0
      ? densestClusterAnchor(endpointObjects.map((object) => [object.coordinates!.lon, object.coordinates!.lat] as [number, number]))
      : linePointAt(bucket.lineFeatures[0].geometry.coordinates as [number, number][], 0.76);
    const corridorType = inferCorridorType(bucket.channel, bucket.labelHints);
    const count = endpointObjects.length > 0 ? endpointObjects.length : bucket.lineFeatures.length;
    const destinationLabel = buildCorridorDestinationLabel(endpointObjects);
    const typeLabel = corridorTypeLabel(corridorType);
    if (centerPoint) {
      const distanceKm = approximateDistanceKm(centerPoint[0], centerPoint[1], anchorPoint[0], anchorPoint[1]);
      overview.push({
        id: `corridor-guide:${bucket.corridorId}`,
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: gentleArcPoints(centerPoint, anchorPoint, `corridor-guide:${bucket.corridorId}`, distanceKm) },
        properties: {
          kind: 'corridor-guide',
          corridorId: bucket.corridorId,
          channel: bucket.channel,
          color: theme === 'dark' ? 'rgba(148,163,184,0.75)' : 'rgba(100,116,139,0.72)',
        },
      } as GeoJSON.Feature<GeoJSON.LineString>);
    }
    overview.push({
      id: `corridor:${bucket.corridorId}`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: anchorPoint },
      properties: {
        kind: 'corridor',
        corridorId: bucket.corridorId,
        channel: bucket.channel,
        color: corridorBadgeColor(theme, corridorType),
        corridorRadius: clamp(9 + Math.log2(count + 1) * 3.6, 10, 18),
        corridorCount: count,
        labelText: `${typeLabel} x${count}\n${destinationLabel}`,
      },
    } as GeoJSON.Feature<GeoJSON.Point>);
  }
  return overview;
}

function decorateCorridorFeatures(
  features: GeoJSON.Feature[],
  selectedCorridorId: string | null,
): GeoJSON.Feature[] {
  return features.map((feature) => {
    const corridorId = String(feature.properties?.corridorId || '').trim();
    const selected = Boolean(selectedCorridorId) && corridorId === selectedCorridorId;
    const dimmed = Boolean(selectedCorridorId) && corridorId !== selectedCorridorId;
    return {
      ...feature,
      properties: {
        ...(feature.properties || {}),
        corridorSelected: selected,
        corridorDimmed: dimmed,
      },
    };
  });
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

  const minDistancePx = ENDPOINT_MARKER_SIZE_PX + ENDPOINT_MARKER_PADDING_PX;
  const maxOffsetPx = 42;
  const hasAnyOverlap = () => {
    for (let i = 0; i < points.length; i += 1) {
      for (let j = i + 1; j < points.length; j += 1) {
        if (endpointsOverlap(points[i], points[j])) return true;
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

  for (let iteration = 0; iteration < 12; iteration += 1) {
    for (let i = 0; i < points.length; i += 1) {
      for (let j = i + 1; j < points.length; j += 1) {
        const a = points[i];
        const b = points[j];
        if (!endpointsOverlap(a, b)) continue;
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
        const push = clamp(Math.max(minDistancePx - distance, 8), 4, 18) * 0.52;
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

function buildSelectedCorridorBushFeatures(
  centerPoint: [number, number] | null,
  corridorFeatures: GeoJSON.Feature[],
  endpointEntries: Array<{ key: string; object: GeoObject; displayPoint: [number, number]; anchorPoint: [number, number] }>,
): GeoJSON.Feature[] {
  if (!centerPoint || corridorFeatures.length === 0 || endpointEntries.length === 0) return corridorFeatures;
  const sample = strongestCorridorFeature(corridorFeatures);
  if (!sample) return corridorFeatures;

  const channel = String(sample.properties?.channel || '').trim();
  const color = String(sample.properties?.color || '').trim() || '#f59e0b';
  const kind = String(sample.properties?.kind || 'primary').trim();
  const hop = Number(sample.properties?.hop) || 1;
  const corridorId = String(sample.properties?.corridorId || '').trim();
  const renderPriority = Number(sample.properties?.renderPriority) || 20;
  const baseWidth = Number(sample.properties?.baseWidth) || 2.2;
  const baseOpacity = Number(sample.properties?.baseOpacity) || 0.88;
  const glowOpacity = Number(sample.properties?.glowOpacity) || 0.12;

  if (endpointEntries.length === 1) {
    const endpoint = endpointEntries[0];
    const distanceKm = approximateDistanceKm(centerPoint[0], centerPoint[1], endpoint.displayPoint[0], endpoint.displayPoint[1]);
    const directCoords = kind === 'primary' && channel !== 'propagation'
      ? [centerPoint, endpoint.displayPoint]
      : gentleArcPoints(centerPoint, endpoint.displayPoint, `${corridorId}:${endpoint.key}`, distanceKm);
    return [{
      id: `${corridorId}:direct`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: directCoords },
      properties: {
        kind,
        hop,
        color,
        channel,
        corridorId,
        renderPriority,
        baseOpacity,
        baseWidth,
        glowOpacity,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>];
  }

  const clusterLinkKm = estimatePointClusterLinkKm(endpointEntries.map((entry) => entry.anchorPoint));
  const endpointClusters = clusterPointEntriesByDistance(
    endpointEntries.map((entry) => ({ ...entry, point: entry.anchorPoint })),
    clusterLinkKm,
  )
    .map((cluster) => cluster.map(({ point: _point, ...entry }) => entry))
    .sort((left, right) => right.length - left.length);
  const features: GeoJSON.Feature[] = [];

  endpointClusters.forEach((cluster, clusterIndex) => {
    if (cluster.length === 1) {
      const endpoint = cluster[0]!;
      const distanceKm = approximateDistanceKm(centerPoint[0], centerPoint[1], endpoint.displayPoint[0], endpoint.displayPoint[1]);
      const directCoords = kind === 'primary' && channel !== 'propagation'
        ? [centerPoint, endpoint.displayPoint]
        : gentleArcPoints(centerPoint, endpoint.displayPoint, `${corridorId}:${endpoint.key}:cluster:${clusterIndex}`, distanceKm);
      features.push({
        id: `${corridorId}:direct:${clusterIndex}:${endpoint.key}`,
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: directCoords },
        properties: {
          kind,
          hop,
          color,
          channel,
          corridorId,
          endpointNodeIds: endpoint.key.startsWith('node:') ? [endpoint.key.slice(5)] : [],
          endpointPathRefs: endpoint.key.startsWith('path:') ? [endpoint.key.slice(5)] : [],
          renderPriority,
          baseOpacity,
          baseWidth,
          glowOpacity,
        },
      } as GeoJSON.Feature<GeoJSON.LineString>);
      return;
    }

    const clusterAnchor = densestClusterAnchor(cluster.map((entry) => entry.displayPoint));
    const splitPoint = pointOnSegment(centerPoint, clusterAnchor, 0.8);
    features.push({
      id: `${corridorId}:trunk:${clusterIndex}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: [centerPoint, splitPoint] },
      properties: {
        kind: kind === 'cluster' ? 'indirect' : kind,
        hop,
        color,
        channel,
        corridorId,
        renderPriority: renderPriority + 2,
        baseOpacity: clamp(baseOpacity + 0.06, 0.32, 0.96),
        baseWidth: clamp(baseWidth + 0.5, 1.4, 6.4),
        glowOpacity: clamp(glowOpacity + 0.03, 0.03, 0.28),
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);

    for (const endpoint of cluster) {
      const midpoint = lerpPoint(splitPoint, endpoint.displayPoint, 0.5);
      const control = lerpPoint(midpoint, clusterAnchor, 0.58);
      features.push({
        id: `${corridorId}:branch:${clusterIndex}:${endpoint.key}`,
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: quadraticCurvePoints(splitPoint, endpoint.displayPoint, control) },
        properties: {
          kind: 'indirect',
          hop,
          color,
          channel,
          corridorId,
          endpointNodeIds: endpoint.key.startsWith('node:') ? [endpoint.key.slice(5)] : [],
          endpointPathRefs: endpoint.key.startsWith('path:') ? [endpoint.key.slice(5)] : [],
          renderPriority,
          baseOpacity: clamp(baseOpacity - 0.08, 0.24, 0.88),
          baseWidth: clamp(baseWidth * 0.78, 0.9, 4.4),
          glowOpacity: clamp(glowOpacity * 0.7, 0.02, 0.16),
        },
      } as GeoJSON.Feature<GeoJSON.LineString>);
    }
  });

  if (features.length === 0) {
    for (const endpoint of endpointEntries) {
      const distanceKm = approximateDistanceKm(centerPoint[0], centerPoint[1], endpoint.displayPoint[0], endpoint.displayPoint[1]);
      features.push({
        id: `${corridorId}:fallback:${endpoint.key}`,
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: gentleArcPoints(centerPoint, endpoint.displayPoint, `${corridorId}:${endpoint.key}:fallback`, distanceKm) },
        properties: {
          kind,
          hop,
          color,
          channel,
          corridorId,
          endpointNodeIds: endpoint.key.startsWith('node:') ? [endpoint.key.slice(5)] : [],
          endpointPathRefs: endpoint.key.startsWith('path:') ? [endpoint.key.slice(5)] : [],
          renderPriority,
          baseOpacity,
          baseWidth,
          glowOpacity,
        },
      } as GeoJSON.Feature<GeoJSON.LineString>);
    }
  }
  return features;
}

function buildTrianglePolygon(
  lat0: number,
  lon0: number,
  vertices: [LocalPoint, LocalPoint, LocalPoint],
): GeoJSON.Polygon {
  const ring: [number, number][] = vertices.map((point) => fromLocalKm(lat0, lon0, point.x, point.y));
  ring.push(ring[0]);
  return { type: 'Polygon', coordinates: [ring] };
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
  const centroid = averagePoint(endpointEntries.map((entry) => entry.displayPoint));
  const centerLon = centroid[0];
  const centerLat = centroid[1];
  const rawNodes = endpointEntries.map((entry) => {
    const local = toLocalKm(centerLat, centerLon, entry.displayPoint[1], entry.displayPoint[0]);
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

  const spanKm = Math.max(metrics.maxX - metrics.minX, metrics.maxY - metrics.minY);
  const cellKm = clamp(spanKm * 0.045, 5, 18);
  const baseColor = theme === 'dark' ? '#4b5563' : '#475569';
  const accentColor = theme === 'dark' ? '#94a3b8' : '#64748b';
  const out: GeoJSON.Feature[] = [];

  let rowIndex = 0;
  for (let y = metrics.minY; y <= metrics.maxY + cellKm; y += cellKm) {
    let colIndex = 0;
    for (let x = metrics.minX; x <= metrics.maxX + cellKm; x += cellKm) {
      const tl: LocalPoint = { x, y };
      const tr: LocalPoint = { x: x + cellKm, y };
      const br: LocalPoint = { x: x + cellKm, y: y + cellKm };
      const bl: LocalPoint = { x, y: y + cellKm };
      const flip = ((rowIndex + colIndex) & 1) === 0;
      const triangles: [LocalPoint, LocalPoint, LocalPoint][] = flip
        ? [[tl, tr, bl], [tr, br, bl]]
        : [[tl, tr, br], [tl, br, bl]];

      for (let triangleIndex = 0; triangleIndex < triangles.length; triangleIndex += 1) {
        const triangle = triangles[triangleIndex]!;
        const center = {
          x: (triangle[0].x + triangle[1].x + triangle[2].x) / 3,
          y: (triangle[0].y + triangle[1].y + triangle[2].y) / 3,
        };
        const strength = evaluateSelectedCorridorFieldStrength(metrics, center);
        if (strength < metrics.threshold) continue;
        const edgeFactor = clamp((strength - metrics.threshold) / Math.max(1 - metrics.threshold, 0.001), 0, 1);
        const jitter = ((hashString(`${corridorId}:${rowIndex}:${colIndex}:${triangleIndex}`) % 1000) / 1000);
        const keepThreshold = clamp(0.46 + edgeFactor * 0.5, 0.42, 0.96);
        if (jitter > keepThreshold) continue;

        out.push({
          id: `${corridorId}:field:${rowIndex}:${colIndex}:${triangleIndex}`,
          type: 'Feature',
          geometry: buildTrianglePolygon(metrics.centerLat, metrics.centerLon, triangle),
          properties: {
            kind: 'corridor-field',
            corridorId,
            color: baseColor,
            fillOpacity: (theme === 'dark' ? 0.052 : 0.04) * (0.62 + edgeFactor * 1.3),
            accentColor,
            accentOpacity: (theme === 'dark' ? 0.03 : 0.022) * Math.pow(edgeFactor, 1.04) * 1.3,
          },
        } as GeoJSON.Feature<GeoJSON.Polygon>);
      }
      colIndex += 1;
    }
    rowIndex += 1;
  }
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

function buildOverviewCorridorMutedObjectIds(
  map: maplibregl.Map,
  corridorFeatures: GeoJSON.Feature[],
  objects: GeoObject[],
  selectedObjectId: string | null,
): string[] {
  const badges = corridorFeatures
    .filter((feature) => String(feature.properties?.kind || '').trim() === 'corridor' && feature.geometry?.type === 'Point')
    .map((feature) => {
      const geometry = feature.geometry;
      const coordinates = geometry?.type === 'Point'
        ? geometry.coordinates as [number, number]
        : undefined;
      if (!coordinates || coordinates.length < 2) return null;
      const projected = map.project({ lng: coordinates[0], lat: coordinates[1] });
      const corridorRadius = Number(feature.properties?.corridorRadius) || 12;
      return {
        x: projected.x,
        y: projected.y,
        radiusPx: corridorRadius * 5.2 + 54,
      };
    })
    .filter((entry): entry is { x: number; y: number; radiusPx: number } => Boolean(entry));
  if (badges.length === 0) return [];

  const muted = new Set<string>();
  for (const object of objects) {
    if (!object.id || object.id === selectedObjectId) continue;
    const lat = object.coordinates?.lat;
    const lon = object.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const projected = map.project({ lng: lon!, lat: lat! });
    const insideBadgeZone = badges.some((badge) => {
      const distancePx = Math.hypot(projected.x - badge.x, projected.y - badge.y);
      return distancePx <= badge.radiusPx;
    });
    if (insideBadgeZone) muted.add(object.id);
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
    supplierRef: string;
    customerRef: string;
    transportViaRefs: string[];
    coordinates: [number, number][];
    directDistanceKm: number;
    score: number;
    labelText: string;
  };

  type OrientedCandidateRoute = CandidateRoute & {
    kind: EdgeVisualKind;
    localPoint: [number, number];
    remotePoint: [number, number];
    azimuthDeg: number;
  };

  const byRouteKey = new Map<string, CandidateRoute>();

  for (const artifact of pathArtifacts) {
    const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
    const multiLegPath = legs.length > 1;
    for (const leg of legs) {
      const supplierRef = String(leg?.supplierRef || '').trim();
      const customerRef = String(leg?.customerRef || '').trim();
      const productRef = String(leg?.productRef || artifact.productAxis || '').trim();
      if (!supplierRef || !customerRef || !productRef) continue;

      const transportViaRefs = (leg?.transportViaRefs || [])
        .map((ref) => String(ref || '').trim())
        .filter(Boolean);
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
        + ((leg?.flowRefs?.length || 0) > 0 ? 18 : 0)
        + ((leg?.zoneRefs?.length || 0) > 0 ? 10 : 0)
        + ((leg?.affectedZoneRefs?.length || 0) > 0 ? 10 : 0)
        + (multiLegPath ? 22 : 0)
        + ((artifact.traceRole === 'route_overlap' || artifact.traceRole === 'route_context') ? 12 : 0)
        + (artifact.isPinned ? 6 : 0);
      const candidate: CandidateRoute = {
        artifactId: String(artifact.id || '').trim(),
        legIndex: typeof leg?.index === 'number' ? leg.index : 0,
        productRef,
        supplierRef,
        customerRef,
        transportViaRefs,
        coordinates: smoothRouteCoordinates(
          coordinates,
          `supply:${routeKey}`,
          directDistanceKm >= 1200 ? 3 : directDistanceKm >= 250 ? 2 : 1,
        ),
        directDistanceKm,
        score,
        labelText: buildSupplyLabelText(productRef, supplierRef, customerRef, transportViaRefs, objectByPath),
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
  const visiblePathRefs = new Set<string>();
  const coveredPairs = new Set<string>();
  const features: GeoJSON.Feature[] = [];

  const pushRouteFeature = (
    route: CandidateRoute,
    kind: EdgeVisualKind,
    coordinates: [number, number][],
    labelText: string,
    labelRatio: number,
    widthMultiplier = 1,
    opacityBoost = 0,
    colorOverride?: string,
    corridorId?: string,
    featureRole: 'overview' | 'detail' = 'overview',
  ) => {
    const lineId = `supply:${route.artifactId || route.productRef}:${route.legIndex}:${hashString(`${route.supplierRef}:${route.customerRef}:${labelText}`)}`;
    const color = colorOverride || supplyStrokeColor(theme, kind);
    const baseOpacity = kind === 'primary'
      ? (theme === 'dark' ? 0.92 : 0.82)
      : kind === 'indirect'
        ? (theme === 'dark' ? 0.82 : 0.7)
        : (theme === 'dark' ? 0.62 : 0.54);
    const baseWidth = kind === 'primary' ? 2.7 : kind === 'indirect' ? 2.15 : 1.6;

    features.push({
      id: lineId,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates },
      properties: {
        kind,
        hop: kind === 'primary' ? 1 : kind === 'indirect' ? 2 : 3,
        color,
        channel: 'supply_path',
        corridorId: corridorId || `supply-route:${lineId}`,
        featureRole,
        endpointPathRefs: [route.supplierRef, route.customerRef],
        renderPriority: kind === 'primary' ? 28 : kind === 'indirect' ? 24 : 20,
        baseOpacity: clamp(baseOpacity + opacityBoost, 0.28, 0.96),
        baseWidth: clamp(baseWidth * widthMultiplier, 1.2, 6.6),
        glowOpacity: kind === 'cluster' ? 0.05 : 0.16,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    features.push({
      id: `${lineId}:label`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(coordinates, labelRatio) },
      properties: {
        kind: 'label',
        corridorId: corridorId || `supply-route:${lineId}`,
        featureRole,
        labelText,
        labelColor: color,
      },
    } as GeoJSON.Feature<GeoJSON.Point>);
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

    if (route.directDistanceKm < SUPPLY_BUNDLE_MIN_DISTANCE_KM) {
      continue;
    }
    const azimuthBucket = Math.round(route.azimuthDeg / SUPPLY_BUNDLE_AZIMUTH_DEG);
    const distanceBucket = Math.round(route.directDistanceKm / 1200);
    const remoteCell = geoCellKey(route.remotePoint);
    const localCell = geoCellKey(route.localPoint);
    const key = `${azimuthBucket}:${distanceBucket}:${remoteCell}:${localCell}`;
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
    const trunkColor = supplyStrokeColor(theme, 'primary');
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
        renderPriority: strongestKind === 'primary' ? 30 : strongestKind === 'indirect' ? 26 : 22,
        baseOpacity: clamp((theme === 'dark' ? 0.74 : 0.66) + Math.min(0.18, group.length * 0.03), 0.42, 0.94),
        baseWidth: clamp((strongestKind === 'primary' ? 3.2 : strongestKind === 'indirect' ? 2.8 : 2.2) + Math.log2(group.length + 1) * 1.15, 2.2, 7.8),
        glowOpacity: 0.18,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    features.push({
      id: `${trunkId}:label`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(trunkCoords, 0.58) },
      properties: {
        kind: 'label',
        corridorId: trunkId,
        featureRole: 'overview',
        labelText: buildSupplyBundleLabelText(group.length, group[0], objectByPath),
        labelColor: trunkColor,
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
      route.transportViaRefs.length > 0 ? 0.52 : 0.34,
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

  relations.forEach((relation, index) => {
    const relatedCoords = idToCoords.get(relation.relatedFragmentId) || relation.relatedCoordinates || null;
    if (!relatedCoords) return;
    const start: [number, number] = [center.lon, center.lat];
    const end: [number, number] = [relatedCoords.lon, relatedCoords.lat];
    const distanceKm = approximateDistanceKm(start[0], start[1], end[0], end[1]);
    const kind: EdgeVisualKind = section === 'direct' ? 'primary' : 'indirect';
    const color = relationStrokeColor(theme, relation.family, relation.semanticDirection, section === 'direct');
    const coordinates = section === 'direct' && distanceKm < 220
      ? [start, end]
      : gentleArcPoints(start, end, `${section}:${relation.relatedFragmentId}:${relation.type}`, distanceKm, section === 'direct' ? 14 : 18);
    const corridorId = `${section}:${relation.relatedFragmentId}`;
    const labelText = `${relation.relatedLabel}\n${compactMeaning(relation.businessMeaning || humanizeEdgeType(relation.type))}`;
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
        relationType: relation.type,
        semanticDirection: relation.semanticDirection || 'context',
        color,
        baseOpacity: section === 'direct' ? (theme === 'dark' ? 0.9 : 0.82) : (theme === 'dark' ? 0.68 : 0.58),
        baseWidth: section === 'direct' ? 2.4 : 1.8,
        glowOpacity: section === 'direct' ? 0.12 : 0.08,
        renderPriority: section === 'direct' ? 26 : 18,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    features.push({
      id: `${corridorId}:label:${index}`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(coordinates, section === 'direct' ? 0.72 : 0.76) },
      properties: {
        kind: 'label',
        corridorId,
        featureRole: 'overview',
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
    const pairKey = stablePairKey(sourceId, targetId);
    if (seen.has(pairKey)) continue;
    seen.add(pairKey);
    const source = idToCoords.get(sourceId);
    const target = idToCoords.get(targetId);
    if (!source || !target) continue;
    const hop = Math.max(1, Math.min(3, Number(edge?.hopsFromSource) || 1));
    const kind: EdgeVisualKind = hop <= 1 ? 'primary' : hop === 2 ? 'indirect' : 'cluster';
    const color = propagationStrokeColor(theme, kind);
    const coords = gentleArcPoints([source.lon, source.lat], [target.lon, target.lat], `prop:${pairKey}`, approximateDistanceKm(source.lon, source.lat, target.lon, target.lat), 14);
    features.push({
      id: `prop:${edge?.edgeId || pairKey}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: coords },
      properties: {
        kind,
        hop,
        channel: 'propagation',
        corridorId: `prop:${pairKey}`,
        featureRole: 'overview',
        endpointNodeIds: [sourceId, targetId],
        badgeEndpointNodeIds: [sourceId === centerId ? targetId : sourceId, targetId],
        sourceId,
        targetId,
        semanticDirection: sourceId === centerId ? 'downstream' : targetId === centerId ? 'upstream' : 'context',
        color,
        baseOpacity: kind === 'primary' ? 0.5 : kind === 'indirect' ? 0.42 : 0.3,
        baseWidth: kind === 'primary' ? 1.55 : kind === 'indirect' ? 1.25 : 0.95,
        glowOpacity: kind === 'cluster' ? 0.02 : 0.06,
        renderPriority: kind === 'primary' ? 12 : kind === 'indirect' ? 10 : 8,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
  }
  return features;
}

function buildExplainSupplyPathItems(pathArtifacts: MaterializedArtifact[]): GeoEdgeTraceSummary['supplyPaths'] {
  return [...pathArtifacts]
    .sort((left, right) => {
      const leftStress = left.pathStressScore ?? left.payload?.stress?.pathScore ?? 0;
      const rightStress = right.pathStressScore ?? right.payload?.stress?.pathScore ?? 0;
      if (leftStress !== rightStress) return rightStress - leftStress;
      return Number(right.tracePriority || 0) - Number(left.tracePriority || 0);
    })
    .slice(0, 6)
    .map((artifact) => {
      const firstLeg = Array.isArray(artifact.payload?.legs) && artifact.payload?.legs.length > 0 ? artifact.payload.legs[0] : null;
      const productRef = String(firstLeg?.productRef || artifact.productAxis || '').trim() || undefined;
      const stressScore = Number(artifact.pathStressScore ?? artifact.payload?.stress?.pathScore ?? 0);
      const title = String(artifact.displayName || '').trim()
        || [artifact.anchorSupplier, productRef, artifact.anchorCustomer].filter(Boolean).join(' -> ')
        || artifact.id;
      return {
        id: artifact.id,
        title,
        role: String(artifact.traceRole || '').trim() || 'path',
        reason: String(artifact.discoveryReason || artifact.traceReason || '').trim() || 'Mapped supply path linked to this object.',
        productRef,
        productLabel: productRef ? humanizeCanonicalPath(productRef) : undefined,
        stressScore,
        legCount: Number(artifact.legCount || (Array.isArray(artifact.payload?.legs) ? artifact.payload!.legs!.length : 0) || 0),
        isPinned: Boolean(artifact.isPinned),
        hasBottleneck: typeof (artifact.bottleneckLegIndex ?? artifact.payload?.stress?.bottleneckLegIndex) === 'number'
          && Number(artifact.bottleneckLegIndex ?? artifact.payload?.stress?.bottleneckLegIndex) >= 0,
      };
    });
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
): GeoEdgeTraceSummary {
  const supplyPaths = buildExplainSupplyPathItems(pathArtifacts);
  const stressedSupplyPathCount = supplyPaths.filter((item) => item.stressScore >= 0.58).length;
  return {
    summary: String(explain?.summary || '').trim() || 'No explanation available for this object yet.',
    whyItMatters: Array.isArray(explain?.whyItMatters)
      ? explain!.whyItMatters!
        .map((item) => ({ title: String(item?.title || '').trim(), body: String(item?.body || '').trim() }))
        .filter((item) => item.title && item.body)
      : [],
    directRelations: directRelations.map((relation) => ({
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
    supplyPaths,
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
  const opacityFactor = 0.85;
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
  });

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
  });

  map.addLayer({
    id: CORRIDOR_GLOW_LAYER,
    type: 'circle',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor'],
    paint: {
      'circle-radius': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        ['+', ['get', 'corridorRadius'], 6],
        ['+', ['get', 'corridorRadius'], 5],
      ],
      'circle-color': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? '#334155' : '#cbd5e1',
        '#f59e0b',
      ],
      'circle-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? 0.14 : 0.1,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.06,
        0.14,
      ],
      'circle-blur': 0.8,
    },
  });

  map.addLayer({
    id: CORRIDOR_GUIDE_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor-guide'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        1.6,
        1.1,
      ],
      'line-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.62,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.16,
        0.34,
      ],
      'line-dasharray': ['literal', [1.2, 2.2]],
    },
  });

  map.addLayer({
    id: CORRIDOR_LAYER,
    type: 'circle',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor'],
    paint: {
      'circle-radius': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        ['+', ['get', 'corridorRadius'], 1.5],
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'corridorRadius'], 2],
        ['get', 'corridorRadius'],
      ],
      'circle-color': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? 'rgba(15,23,42,0.94)' : 'rgba(226,232,240,0.94)',
        theme === 'dark' ? 'rgba(44,22,2,0.92)' : 'rgba(120,53,15,0.92)',
      ],
      'circle-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.94,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.42,
        0.88,
      ],
      'circle-stroke-color': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? 'rgba(251,191,36,0.92)' : 'rgba(180,83,9,0.94)',
        ['get', 'color'],
      ],
      'circle-stroke-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.92,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.45,
        0.82,
      ],
      'circle-stroke-width': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        2.2,
        ['boolean', ['feature-state', 'hover'], false],
        2.4,
        1.9,
      ],
    },
  });

  map.addLayer({
    id: CORRIDOR_ICON_LAYER,
    type: 'symbol',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor'],
    layout: {
      'text-field': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        'o',
        '↔',
      ],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        11.5,
        13,
      ],
      'text-anchor': 'center',
      'text-allow-overlap': true,
      'text-ignore-placement': true,
    },
    paint: {
      'text-color': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? '#e2e8f0' : '#334155',
        '#fbbf24',
      ],
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.9)' : 'rgba(120,53,15,0.88)',
      'text-halo-width': 0.8,
      'text-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.92,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.46,
        0.92,
      ],
    },
  });

  map.addLayer({
    id: CORRIDOR_LABEL_LAYER,
    type: 'symbol',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'corridor'],
    layout: {
      'text-field': ['get', 'labelText'],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        10.5,
        11,
      ],
      'text-offset': [0, 1.25],
      'text-anchor': 'top',
      'text-allow-overlap': true,
      'text-ignore-placement': true,
      'text-max-width': 10,
    },
    paint: {
      'text-color': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        theme === 'dark' ? 'rgba(251,191,36,0.9)' : 'rgba(146,64,14,0.9)',
        theme === 'dark' ? 'rgba(241,245,249,0.96)' : 'rgba(15,23,42,0.92)',
      ],
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.94)' : 'rgba(255,255,255,0.94)',
      'text-halo-width': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        1,
        1.2,
      ],
      'text-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.82,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.36,
        0.92,
      ],
    },
  });

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
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 2.2],
        ['+', ['get', 'baseWidth'], 1.4],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'hover'], false],
          ['min', 0.28, ['+', ['get', 'glowOpacity'], 0.08]],
          ['get', 'glowOpacity'],
        ],
        opacityFactor,
      ],
      'line-blur': 1.1,
    },
  });

  map.addLayer({
    id: HOP_3_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'cluster'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.65],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'hover'], false],
          ['min', 0.58, ['+', ['get', 'baseOpacity'], 0.16]],
          ['get', 'baseOpacity'],
        ],
        opacityFactor,
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
    id: HOP_2_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'indirect'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-dasharray': [
        'case',
        ['==', ['get', 'channel'], 'propagation'],
        ['literal', [0.8, 1.8]],
        ['literal', [1.4, 1.9]],
      ],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.8],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'hover'], false],
          ['min', 0.76, ['+', ['get', 'baseOpacity'], 0.18]],
          ['get', 'baseOpacity'],
        ],
        opacityFactor,
      ],
    },
  });

  map.addLayer({
    id: HOP_1_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'primary'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.95],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        '*',
        [
          'case',
          ['boolean', ['feature-state', 'hover'], false],
          1,
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
    filter: ['==', ['get', 'kind'], 'label'],
    layout: {
      'text-field': ['get', 'labelText'],
      'text-size': 11,
      'text-max-width': 12,
      'text-offset': [0, -0.25],
      'text-anchor': 'bottom-left',
      'text-allow-overlap': false,
      'text-ignore-placement': false,
    },
    paint: {
      'text-color': ['get', 'labelColor'],
      'text-halo-color': theme === 'dark' ? 'rgba(6,18,31,0.92)' : 'rgba(255,255,255,0.92)',
      'text-halo-width': 1.4,
      'text-opacity': 0.95,
    },
  });

  map.addLayer({
    id: ENDPOINT_CALLOUT_LAYER,
    type: 'line',
    source: ENDPOINT_SRC,
    filter: ['==', ['get', 'kind'], 'endpoint-callout'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': theme === 'dark' ? 'rgba(148,163,184,0.7)' : 'rgba(100,116,139,0.65)',
      'line-width': 0.9,
      'line-opacity': 0.72,
      'line-dasharray': ['literal', [0.8, 1.6]],
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
    filter: ['==', ['get', 'kind'], 'endpoint-marker'],
    layout: {
      'text-field': ['get', 'endpointLabel'],
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': 11,
      'text-offset': [0.9, 0.05],
      'text-anchor': 'left',
      'text-allow-overlap': true,
      'text-ignore-placement': true,
    },
    paint: {
      'text-color': theme === 'dark' ? 'rgba(241,245,249,0.96)' : 'rgba(15,23,42,0.92)',
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.94)' : 'rgba(255,255,255,0.94)',
      'text-halo-width': 1.2,
      'text-opacity': 0.96,
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
  const [mutedObjectIds, setMutedObjectIds] = useState<string[]>([]);
  const [highlightedGatewayObjectIds, setHighlightedGatewayObjectIds] = useState<string[]>([]);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

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
        if (map.getLayer(ENDPOINT_CALLOUT_LAYER)) map.removeLayer(ENDPOINT_CALLOUT_LAYER);
        if (map.getLayer(CORRIDOR_FIELD_ACCENT_LAYER)) map.removeLayer(CORRIDOR_FIELD_ACCENT_LAYER);
        if (map.getLayer(CORRIDOR_FIELD_LAYER)) map.removeLayer(CORRIDOR_FIELD_LAYER);
        if (map.getLayer(CORRIDOR_LABEL_LAYER)) map.removeLayer(CORRIDOR_LABEL_LAYER);
        if (map.getLayer(CORRIDOR_ICON_LAYER)) map.removeLayer(CORRIDOR_ICON_LAYER);
        if (map.getLayer(CORRIDOR_LAYER)) map.removeLayer(CORRIDOR_LAYER);
        if (map.getLayer(CORRIDOR_GLOW_LAYER)) map.removeLayer(CORRIDOR_GLOW_LAYER);
        if (map.getLayer(CORRIDOR_GUIDE_LAYER)) map.removeLayer(CORRIDOR_GUIDE_LAYER);
        if (map.getLayer(LABEL_LAYER)) map.removeLayer(LABEL_LAYER);
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
    };

    const handleEdgeLeave = () => {
      clearHoveredEdge();
    };

    const bringEndpointLayersToFront = () => {
      try {
        if (map.getLayer(ENDPOINT_CALLOUT_LAYER)) map.moveLayer(ENDPOINT_CALLOUT_LAYER);
        if (map.getLayer(ENDPOINT_POINTS_LAYER)) map.moveLayer(ENDPOINT_POINTS_LAYER);
        if (map.getLayer(ENDPOINT_LABEL_LAYER)) map.moveLayer(ENDPOINT_LABEL_LAYER);
      } catch { /* style/layers may be mid-transition */ }
    };

    const bringCorridorLayersToFront = () => {
      try {
        if (map.getLayer(CORRIDOR_FIELD_LAYER)) map.moveLayer(CORRIDOR_FIELD_LAYER);
        if (map.getLayer(CORRIDOR_FIELD_ACCENT_LAYER)) map.moveLayer(CORRIDOR_FIELD_ACCENT_LAYER);
        if (map.getLayer(CORRIDOR_GUIDE_LAYER)) map.moveLayer(CORRIDOR_GUIDE_LAYER);
        if (map.getLayer(CORRIDOR_GLOW_LAYER)) map.moveLayer(CORRIDOR_GLOW_LAYER);
        if (map.getLayer(CORRIDOR_LAYER)) map.moveLayer(CORRIDOR_LAYER);
        if (map.getLayer(CORRIDOR_ICON_LAYER)) map.moveLayer(CORRIDOR_ICON_LAYER);
        if (map.getLayer(CORRIDOR_LABEL_LAYER)) map.moveLayer(CORRIDOR_LABEL_LAYER);
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
      setMutedObjectIds([]);
      setHighlightedGatewayObjectIds([]);
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
    setMutedObjectIds([]);
    setHighlightedGatewayObjectIds([]);

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
        const allFeatures = [
          ...directFeatures,
          ...contextFeatures,
          ...supplyPathFeatures,
          ...propagationFeatures,
        ];
        const endpointEntries = Array.from(visibleNodeIds)
          .map((nodeId) => traceObjectToGeoObject(objectById.get(nodeId)))
          .filter((object): object is GeoObject => Boolean(object?.coordinates))
          .map((object) => ({ key: `node:${object.id}`, object }));
        const spreadEntries = endpointEntries.length > 0 ? spreadEndpointEntries(map, endpointEntries) : [];
        const visibleLineCount = allFeatures.filter((feature) => feature.geometry?.type === 'LineString').length;
        setVisibleEdgesCount(visibleLineCount);
        setTraceSummary(buildExplainTraceSummary(res?.explain || null, directRelations, contextRelations, pathArtifacts, visibleLineCount, res?.propagation || null));

        if (allFeatures.length === 0 || !canAccessMapStyle(map)) {
          setEdgesLoading(false);
          return;
        }

        await ensureMarkerImages(map, spreadEntries.map((entry) => entry.object), theme);
        map.addSource(SRC, { type: 'geojson', data: { type: 'FeatureCollection', features: allFeatures } });
        map.addSource(ENDPOINT_SRC, {
          type: 'geojson',
          data: buildEndpointFeatureCollection(spreadEntries, theme),
        });
        addEdgeLayers(map, theme);
        bringCorridorLayersToFront();
        bringEndpointLayersToFront();
        map.on('mousemove', CORRIDOR_GLOW_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_GUIDE_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_ICON_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_LABEL_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.on('mouseleave', CORRIDOR_GLOW_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_GUIDE_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_ICON_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_LABEL_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_3_LAYER, handleEdgeLeave);
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
          setMutedObjectIds([]);
          setHighlightedGatewayObjectIds([]);
        }
      });

    return () => {
      cancelled = true;
      clearHoveredEdge();
      try {
        map.off('mousemove', CORRIDOR_GLOW_LAYER, handleEdgeHover);
        map.off('mousemove', CORRIDOR_GUIDE_LAYER, handleEdgeHover);
        map.off('mousemove', CORRIDOR_LAYER, handleEdgeHover);
        map.off('mousemove', CORRIDOR_ICON_LAYER, handleEdgeHover);
        map.off('mousemove', CORRIDOR_LABEL_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.off('mouseleave', CORRIDOR_GLOW_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_GUIDE_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_ICON_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_LABEL_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_3_LAYER, handleEdgeLeave);
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
    mutedObjectIds,
    highlightedGatewayObjectIds,
  };
}
