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
type EdgeInteractionState = {
  corridorId: string | null;
  endpointKey: string | null;
};
type LocalPoint = { x: number; y: number };

export interface GeoEdgeTraceSummary {
  totalEdges: number;
  visibleEdges: number;
  supplyRouteCount: number;
  hopCounts: Record<1 | 2 | 3, number>;
  branchCount: number;
  pathArtifactCount: number;
  pathLegCount: number;
  pinnedPathCount: number;
  discoveredPathCount: number;
  stressedPathCount: number;
  bottleneckPathCount: number;
  directPathCount: number;
  routePathCount: number;
  zonePathCount: number;
  affectedPathCount: number;
  propagationNodeCount: number;
  propagationEdgeCount: number;
  topFamilies: Array<{ family: EdgeFamily; count: number }>;
  countryHighlights: Array<{
    countryIso2: string | null;
    countryName: string;
    itemCount: number;
    items: Array<{
      objectId: string;
      label: string;
      type: string;
      hop: 1 | 2 | 3;
      relationLabel: string;
      businessMeaning: string;
    }>;
  }>;
}

interface RawDependencyEdge {
  sourceId: string;
  targetId: string;
  type: string;
  family?: EdgeFamily;
  priority?: number;
  businessMeaning?: string;
  semanticDirection?: string;
}

type TraceObjectInfo = Pick<GeoObject, 'id' | 'canonicalPath' | 'type' | 'label' | 'facilityKind' | 'description' | 'status' | 'inScope'> & {
  coordinates?: { lat: number; lon: number };
};

interface SelectedTraceEdge extends RawDependencyEdge {
  edgeId: string;
  pairKey: string;
  hop: 1 | 2 | 3;
  minDepth: number;
  family: EdgeFamily;
  distanceKm: number;
  branchRootId: string;
  parentNodeId: string | null;
  parentFamily: EdgeFamily | null;
  rootFamily: EdgeFamily | null;
  continuationScore: number;
  sourceCoords: { lon: number; lat: number } | null;
  targetCoords: { lon: number; lat: number } | null;
}

interface RenderDependencyEdge extends SelectedTraceEdge {
  color: string;
  coordinates: [number, number][];
  baseOpacity: number;
  baseWidth: number;
  glowOpacity: number;
}

interface TraceRoute {
  routeId: string;
  targetNodeId: string;
  branchRootId: string;
  hop: 1 | 2 | 3;
  type: string;
  family: EdgeFamily;
  continuationScore: number;
  color: string;
  rawCoordinates: [number, number][];
  coordinates: [number, number][];
  baseOpacity: number;
  baseWidth: number;
  glowOpacity: number;
}

const MAX_VISIBLE_CORRIDORS = 8;
const MAX_PRIMARY_EDGES = 7;
const MAX_INDIRECT_BRANCHES = 5;
const MAX_CLUSTER_EDGES = 8;
const SUPPLY_BUNDLE_MIN_DISTANCE_KM = 1400;
const RADIAL_BUNDLE_MIN_DISTANCE_KM = 1400;
const SUPPLY_BUNDLE_MIN_ROUTES = 3;
const SUPPLY_BUNDLE_AZIMUTH_DEG = 20;
const SUPPLY_BUNDLE_LOCAL_CELL_DEG = 3.5;
const SUPPLY_BUNDLE_MAX_LOCAL_SPREAD_KM = 680;
const SUPPLY_BUNDLE_MAX_REMOTE_SPREAD_KM = 1800;
const ENDPOINT_MARKER_SIZE_PX = 28;
const ENDPOINT_MARKER_PADDING_PX = 4;
const ENDPOINT_LABEL_OFFSET_X_PX = 14;
const ENDPOINT_LABEL_HEIGHT_PX = 15;
const ENDPOINT_LABEL_CHAR_WIDTH_PX = 6.4;
const ENDPOINT_LABEL_MAX_WIDTH_PX = 220;

const EDGE_PRIORITY: Record<string, number> = {
  SUPPLIES_TO: 100,
  DEPENDS_ON: 99,
  PRODUCES: 97,
  EXTRACTS_FROM: 96,
  REQUIRES: 95,
  USES: 94,
  CONTROLS: 92,
  MANAGES: 90,
  OWNS: 88,
  INVESTS_IN: 86,
  REGULATES: 84,
  ASSURES: 82,
  AUDITED_BY: 80,
  OPERATES_IN: 76,
  LOCATED_IN: 74,
  PARTICIPATES_IN: 72,
  TRADED_IN: 73,
  SUPPORTS: 70,
  PARTNERS_WITH: 68,
};

const FAMILY_VISUAL_WEIGHT: Record<EdgeFamily, number> = {
  supply: 1.04,
  control: 1.01,
  influence: 0.95,
  market: 0.88,
  risk: 0.9,
  social: 0.82,
  location: 0.78,
  other: 0.74,
};

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

function edgePriority(edgeOrType: RawDependencyEdge | string): number {
  if (typeof edgeOrType !== 'string' && typeof edgeOrType?.priority === 'number' && Number.isFinite(edgeOrType.priority)) {
    return edgeOrType.priority;
  }
  const type = typeof edgeOrType === 'string' ? edgeOrType : edgeOrType.type;
  return EDGE_PRIORITY[type] ?? 40;
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

function edgeFamily(edgeOrType: RawDependencyEdge | string): EdgeFamily {
  if (typeof edgeOrType !== 'string' && edgeOrType?.family) return edgeOrType.family;
  const type = typeof edgeOrType === 'string' ? edgeOrType : edgeOrType.type;
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

function businessMeaningForEdgeType(edgeOrType: RawDependencyEdge | string): string {
  if (typeof edgeOrType !== 'string' && edgeOrType?.businessMeaning) return edgeOrType.businessMeaning;
  const edgeType = typeof edgeOrType === 'string' ? edgeOrType : edgeOrType.type;
  switch (edgeType) {
    case 'SUPPLIES_TO':
    case 'PRODUCES':
    case 'EXTRACTS_FROM':
      return 'Operational supply path';
    case 'DEPENDS_ON':
    case 'REQUIRES':
    case 'USES':
      return 'Dependency exposure';
    case 'CONTROLS':
    case 'MANAGES':
    case 'OWNS':
    case 'INVESTS_IN':
      return 'Control or ownership chain';
    case 'LOCATED_IN':
    case 'OPERATES_IN':
    case 'BASED_IN':
    case 'HEADQUARTERED_IN':
      return 'Geographic operating context';
    case 'PARTICIPATES_IN':
      return 'Market participation context';
    case 'TRADED_IN':
      return 'Product trading and price-discovery context';
    case 'HAS_MARKET_SEGMENT':
    case 'HAS_SUBMARKET':
      return 'Market positioning context';
    case 'REGULATES':
    case 'ASSURES':
    case 'AUDITED_BY':
    case 'INFLUENCES':
    case 'AFFECTS':
    case 'IMPACTS':
      return 'External influence or oversight';
    case 'COMPETES_WITH':
    case 'CONFLICTS_WITH':
    case 'OPPOSES':
      return 'Competitive or risk pressure';
    default:
      return 'Context relationship';
  }
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

function routeStrokeColor(theme: ThemeMode, hop: 1 | 2 | 3, weight = 0): string {
  if (theme === 'dark') {
    if (hop === 1) return weight > 1 ? '#b9c4ff' : '#9fb0ff';
    if (hop === 2) return weight > 1 ? '#c3ccef' : '#a9b7df';
    return weight > 1 ? '#cfd5e8' : '#b8c0d8';
  }
  if (hop === 1) return weight > 1 ? '#5067a8' : '#5e74b4';
  if (hop === 2) return weight > 1 ? '#66779f' : '#7182a9';
  return weight > 1 ? '#7c879d' : '#8a95aa';
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

function bundleRouteCoordinates(
  rawPoints: [number, number][],
  routeSeed: string,
  hop: 1 | 2 | 3,
  laneIndex: number,
  laneCount: number,
): [number, number][] {
  const clean = dedupeSequentialPoints(rawPoints);
  if (clean.length < 2) return clean;
  if (clean.length === 2) {
    const base = arcPoints(clean[0][0], clean[0][1], clean[1][0], clean[1][1], routeSeed, hop);
    if (laneCount <= 1) return base;
    const laneCenter = (laneCount - 1) / 2;
    const laneOffset = (laneIndex - laneCenter) * (hop === 1 ? 0.55 : 0.42);
    const start = clean[0];
    const end = clean[1];
    const startAnchor = offsetAlongNormal(start, end, lerpPoint(start, end, 0.18), laneOffset * 0.9);
    const endAnchor = offsetAlongNormal(start, end, lerpPoint(start, end, 0.82), laneOffset * 0.45);
    return smoothRouteCoordinates([start, startAnchor, endAnchor, end], routeSeed, hop);
  }

  const source = clean[0];
  const firstLeg = clean[1];
  const lastLegStart = clean[clean.length - 2];
  const target = clean[clean.length - 1];
  const interior = clean.slice(1, -1);
  const laneCenter = (laneCount - 1) / 2;
  const laneSigned = laneIndex - laneCenter;
  const laneOffset = laneSigned * (hop === 1 ? 0.52 : hop === 2 ? 0.38 : 0.28);
  const routePoints: [number, number][] = [source];

  const sourceSpan = Math.min(Math.max(segmentLength(source, firstLeg), 1), 8);
  const sourceAnchorA = offsetAlongNormal(
    source,
    firstLeg,
    lerpPoint(source, firstLeg, hop === 1 ? 0.16 : 0.22),
    laneOffset * 0.8,
  );
  const sourceAnchorB = offsetAlongNormal(
    source,
    firstLeg,
    lerpPoint(source, firstLeg, hop === 1 ? 0.34 : 0.42),
    laneOffset * (sourceSpan > 3 ? 1.05 : 0.75),
  );
  routePoints.push(sourceAnchorA, sourceAnchorB);

  if (interior.length === 1) {
    const via = interior[0];
    routePoints.push(
      offsetAlongNormal(source, target, lerpPoint(source, via, 0.58), laneOffset * 0.4),
      offsetAlongNormal(source, target, lerpPoint(via, target, 0.42), laneOffset * 0.28),
    );
  } else if (interior.length > 1) {
    const firstVia = interior[0];
    const lastVia = interior[interior.length - 1];
    const centroid: [number, number] = [
      interior.reduce((sum, point) => sum + point[0], 0) / interior.length,
      interior.reduce((sum, point) => sum + point[1], 0) / interior.length,
    ];
    routePoints.push(
      offsetAlongNormal(source, target, lerpPoint(source, firstVia, 0.62), laneOffset * 0.36),
      offsetAlongNormal(source, target, centroid, laneOffset * 0.18),
      offsetAlongNormal(source, target, lerpPoint(lastVia, target, 0.38), laneOffset * 0.24),
    );
  }

  const targetSpan = Math.min(Math.max(segmentLength(lastLegStart, target), 1), 8);
  const targetAnchor = offsetAlongNormal(
    lastLegStart,
    target,
    lerpPoint(lastLegStart, target, 0.72),
    laneOffset * (targetSpan > 3 ? 0.55 : 0.35),
  );
  routePoints.push(targetAnchor, target);

  return smoothRouteCoordinates(routePoints, routeSeed, hop);
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

function indirectStrokeColor(theme: ThemeMode): string {
  return theme === 'dark' ? '#93c5fd' : '#3b82f6';
}

function clusterStrokeColor(theme: ThemeMode): string {
  return theme === 'dark' ? '#cbd5e1' : '#94a3b8';
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

function shouldSuppressRawEdge(
  edge: RawDependencyEdge,
  objectById: Map<string, TraceObjectInfo>,
): boolean {
  if (shouldSuppressConnection(edge.sourceId, edge.targetId, objectById)) return true;
  return edgeFamily(edge) === 'location';
}

function topologyKindForNodes(
  centerId: string,
  sourceId: string,
  targetId: string,
  depths: Map<string, number>,
  fallbackHop = 1,
): EdgeVisualKind {
  const sourceDepth = depths.get(sourceId);
  const targetDepth = depths.get(targetId);
  const minDepth = Math.min(
    Number.isFinite(sourceDepth) ? sourceDepth as number : fallbackHop,
    Number.isFinite(targetDepth) ? targetDepth as number : fallbackHop,
  );
  const maxDepth = Math.max(
    Number.isFinite(sourceDepth) ? sourceDepth as number : fallbackHop,
    Number.isFinite(targetDepth) ? targetDepth as number : fallbackHop,
  );

  if (sourceId === centerId || targetId === centerId || maxDepth <= 1) return 'primary';
  if (minDepth >= 2 && maxDepth >= 2) return 'cluster';
  return 'indirect';
}

function colorForVisualKind(theme: ThemeMode, kind: EdgeVisualKind): string {
  if (kind === 'primary') return primaryStrokeColor(theme);
  if (kind === 'indirect') return indirectStrokeColor(theme);
  return clusterStrokeColor(theme);
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

function buildIndirectCorridorCoordinates(
  pathPoints: [number, number][],
  routeSeed: string,
): [number, number][] {
  const clean = dedupeSequentialPoints(pathPoints);
  if (clean.length < 2) return clean;
  if (clean.length === 2) {
    return smoothRouteCoordinates(clean, routeSeed, 2);
  }

  const source = clean[0];
  const target = clean[clean.length - 1];
  const viaPoints = clean.slice(1, -1);
  const directDistanceKm = approximateDistanceKm(source[0], source[1], target[0], target[1]);
  if (directDistanceKm < 220) {
    if (viaPoints.length === 0) return gentleArcPoints(source, target, routeSeed, directDistanceKm);
    const viaCentroid: [number, number] = [
      viaPoints.reduce((sum, point) => sum + point[0], 0) / viaPoints.length,
      viaPoints.reduce((sum, point) => sum + point[1], 0) / viaPoints.length,
    ];
    const midpoint = lerpPoint(source, target, 0.5);
    const control = lerpPoint(midpoint, viaCentroid, 0.28);
    return smoothPolyline([source, control, target], 2);
  }
  const viaCentroid: [number, number] = [
    viaPoints.reduce((sum, point) => sum + point[0], 0) / viaPoints.length,
    viaPoints.reduce((sum, point) => sum + point[1], 0) / viaPoints.length,
  ];
  const cross = (target[0] - source[0]) * (viaCentroid[1] - source[1]) - (target[1] - source[1]) * (viaCentroid[0] - source[0]);
  const direction = Math.abs(cross) > 1e-6 ? Math.sign(cross) : (hashString(routeSeed) % 2 === 0 ? 1 : -1);
  const span = Math.min(Math.max(segmentLength(source, target), 1), 16);
  const corridorOffset = clamp(
    span * (directDistanceKm < 800 ? 0.02 : 0.026),
    0.06,
    directDistanceKm < 800 ? 0.2 : 0.42,
  ) * direction;
  const sourceAnchor = offsetAlongNormal(
    source,
    target,
    lerpPoint(source, viaCentroid, 0.42),
    corridorOffset * 0.55,
  );
  const midAnchor = offsetAlongNormal(
    source,
    target,
    viaCentroid,
    corridorOffset * 0.72,
  );
  const targetAnchor = offsetAlongNormal(
    source,
    target,
    lerpPoint(viaCentroid, target, 0.58),
    corridorOffset * 0.42,
  );

  return smoothPolyline([source, sourceAnchor, midAnchor, targetAnchor, target], 3);
}

function nodeDepthForEdge(centerId: string, edge: SelectedTraceEdge): string {
  if (edge.hop === 1) {
    return edge.sourceId === centerId ? edge.targetId : edge.sourceId;
  }
  return edge.parentNodeId === edge.sourceId ? edge.targetId : edge.sourceId;
}

function buildNodeLinkById(centerId: string, edges: SelectedTraceEdge[]): Map<string, SelectedTraceEdge> {
  const byNode = new Map<string, SelectedTraceEdge>();
  for (const edge of edges) {
    if (edge.hop === 1 && edge.sourceId !== centerId && edge.targetId !== centerId) continue;
    const nodeId = nodeDepthForEdge(centerId, edge);
    const existing = byNode.get(nodeId);
    if (!existing || edge.continuationScore > existing.continuationScore) {
      byNode.set(nodeId, edge);
    }
  }
  return byNode;
}

function reconstructPathPoints(
  centerId: string,
  targetNodeId: string,
  nodeLinkById: Map<string, SelectedTraceEdge>,
): { points: [number, number][]; deepestEdge: SelectedTraceEdge | null } {
  const chain: SelectedTraceEdge[] = [];
  let currentId = targetNodeId;
  while (true) {
    const edge = nodeLinkById.get(currentId);
    if (!edge) break;
    chain.push(edge);
    if (edge.hop === 1) break;
    currentId = String(edge.parentNodeId || '').trim();
    if (!currentId) break;
  }
  if (chain.length === 0) return { points: [], deepestEdge: null };
  const ordered = [...chain].reverse();
  const points: [number, number][] = [];
  for (const edge of ordered) {
    if (edge.hop === 1) {
      if (!edge.sourceCoords || !edge.targetCoords) return { points: [], deepestEdge: null };
      const start = edge.sourceId === centerId ? edge.sourceCoords : edge.targetCoords;
      const end = edge.sourceId === centerId ? edge.targetCoords : edge.sourceCoords;
      if (points.length === 0) points.push([start.lon, start.lat]);
      points.push([end.lon, end.lat]);
      continue;
    }
    const parentId = String(edge.parentNodeId || '').trim();
    const childId = parentId === edge.sourceId ? edge.targetId : edge.sourceId;
    const childCoords = childId === edge.sourceId ? edge.sourceCoords : edge.targetCoords;
    if (!childCoords) return { points: [], deepestEdge: null };
    points.push([childCoords.lon, childCoords.lat]);
  }
  return { points: dedupeSequentialPoints(points), deepestEdge: ordered[ordered.length - 1] || null };
}

function reconstructPathNodeIds(
  centerId: string,
  targetNodeId: string,
  nodeLinkById: Map<string, SelectedTraceEdge>,
): string[] {
  const ids: string[] = [centerId];
  let currentId = targetNodeId;
  const chain: string[] = [];
  while (currentId) {
    chain.push(currentId);
    const edge = nodeLinkById.get(currentId);
    if (!edge || edge.hop === 1) break;
    currentId = String(edge.parentNodeId || '').trim();
  }
  chain.reverse().forEach((id) => {
    if (!ids.includes(id)) ids.push(id);
  });
  return ids;
}

function buildTraceVisualFeatures(
  centerId: string,
  selectedEdges: SelectedTraceEdge[],
  rawEdges: RawDependencyEdge[],
  depths: Map<string, number>,
  parents: Map<string, string>,
  objectById: Map<string, TraceObjectInfo>,
  idToCoords: Map<string, { lon: number; lat: number }>,
  theme: ThemeMode,
): {
  graphFeatures: GeoJSON.Feature[];
  visibleNodeIds: Set<string>;
} {
  const visibleNodeIds = new Set<string>([centerId]);
  const graphFeatures: GeoJSON.Feature[] = [];
  const nodeLinkById = buildNodeLinkById(centerId, selectedEdges);
  const visiblePrimaryNodeIds = new Set<string>();
  const centerCoords = idToCoords.get(centerId);

  const primaryEdges = selectedEdges
    .filter((edge) => edge.hop === 1 && (edge.sourceId === centerId || edge.targetId === centerId))
    .sort((left, right) => right.continuationScore - left.continuationScore)
    .slice(0, MAX_PRIMARY_EDGES);

  const primaryRoutes: RadialBundleRoute[] = [];
  for (const edge of primaryEdges) {
    if (!edge.sourceCoords || !edge.targetCoords) continue;
    const targetNodeId = edge.sourceId === centerId ? edge.targetId : edge.sourceId;
    visibleNodeIds.add(targetNodeId);
    visiblePrimaryNodeIds.add(targetNodeId);
    const end = edge.sourceId === centerId ? edge.targetCoords : edge.sourceCoords;
    primaryRoutes.push({
      routeId: `primary:${targetNodeId}`,
      targetId: targetNodeId,
      corridorId: `trace-branch:${targetNodeId}`,
      endPoint: [end.lon, end.lat],
      kind: 'primary',
      color: primaryStrokeColor(theme),
      baseOpacity: theme === 'dark' ? 0.88 : 0.76,
      baseWidth: 2.2,
      glowOpacity: theme === 'dark' ? 0.14 : 0.1,
      renderPriority: 18,
      labelText: buildLabelText(targetNodeId, businessMeaningForEdgeType(edge), objectById),
    });
  }
  const { features: primaryFeatures, corridorAssignments } = buildRadialBundleFeatures(
    centerCoords ? [centerCoords.lon, centerCoords.lat] : null,
    primaryRoutes,
    theme,
    'trace',
    (group) => {
      const sample = group[0];
      const meaning = compactMeaning((sample?.labelText || '').split('\n')[1] || 'Shared context corridor');
      return `Towards ${group.length} nearby nodes\n${meaning}`;
    },
    { labelSingles: true, labelBundles: true },
  );
  graphFeatures.push(...primaryFeatures);

  const branchCandidates = new Map<string, { targetNodeId: string; edge: SelectedTraceEdge; score: number }>();
  for (const [nodeId, edge] of nodeLinkById.entries()) {
    if ((depths.get(nodeId) || 0) < 2) continue;
    const branchKey = String(edge.branchRootId || nodeId).trim() || nodeId;
    if (visiblePrimaryNodeIds.size > 0 && !visiblePrimaryNodeIds.has(branchKey)) continue;
    const existing = branchCandidates.get(branchKey);
    const score = edge.continuationScore + (depths.get(nodeId) || 0) * 12;
    if (!existing || score > existing.score) {
      branchCandidates.set(branchKey, { targetNodeId: nodeId, edge, score });
    }
  }

  const indirectBranches = Array.from(branchCandidates.values())
    .sort((left, right) => right.score - left.score)
    .slice(0, MAX_INDIRECT_BRANCHES);
  const selectedRemoteNodeIds = new Set<string>();
  const selectedOriginNeighborIds = new Set<string>();

  for (const branch of indirectBranches) {
    const { points, deepestEdge } = reconstructPathPoints(centerId, branch.targetNodeId, nodeLinkById);
    if (!deepestEdge || points.length < 2) continue;
    const pathNodeIds = reconstructPathNodeIds(centerId, branch.targetNodeId, nodeLinkById);
    const coords = buildIndirectCorridorCoordinates(points, `indirect:${branch.targetNodeId}`);
    const lineId = `indirect:${branch.targetNodeId}`;
    const corridorId = corridorAssignments.get(deepestEdge.branchRootId) || `trace-branch:${deepestEdge.branchRootId}`;
    graphFeatures.push({
      id: lineId,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: coords },
      properties: {
        kind: 'indirect',
        hop: deepestEdge.hop,
        color: indirectStrokeColor(theme),
        channel: 'trace',
        corridorId,
        featureRole: 'detail',
        endpointNodeIds: [branch.targetNodeId],
        renderPriority: 14,
        baseOpacity: theme === 'dark' ? 0.7 : 0.58,
        baseWidth: 1.95,
        glowOpacity: theme === 'dark' ? 0.1 : 0.07,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    graphFeatures.push({
      id: `${lineId}:label`,
      type: 'Feature',
      geometry: { type: 'Point', coordinates: linePointAt(coords, 0.18) },
      properties: {
        kind: 'label',
        corridorId,
        featureRole: 'detail',
        labelText: buildLabelText(branch.targetNodeId, businessMeaningForEdgeType(deepestEdge), objectById),
        labelColor: indirectStrokeColor(theme),
      },
    } as GeoJSON.Feature<GeoJSON.Point>);
    for (const nodeId of pathNodeIds) {
      visibleNodeIds.add(nodeId);
      const depth = depths.get(nodeId) || 0;
      if (depth === 1) selectedOriginNeighborIds.add(nodeId);
      if (depth >= 2) selectedRemoteNodeIds.add(nodeId);
    }
    visibleNodeIds.add(branch.targetNodeId);
    visibleNodeIds.add(deepestEdge.branchRootId);
  }

  const clusterCandidates = new Map<string, { sourceId: string; targetId: string; sourceBranch: string; edge: RawDependencyEdge; score: number }>();
  for (const edge of rawEdges) {
    const sourceDepth = depths.get(edge.sourceId) || 0;
    const targetDepth = depths.get(edge.targetId) || 0;
    if (sourceDepth < 2 || targetDepth < 2) continue;
    if (!selectedRemoteNodeIds.has(edge.sourceId) || !selectedRemoteNodeIds.has(edge.targetId)) continue;
    const sourceCoords = idToCoords.get(edge.sourceId);
    const targetCoords = idToCoords.get(edge.targetId);
    if (!sourceCoords || !targetCoords) continue;
    const sourceBranch = deriveBranchRootId(centerId, edge.sourceId, parents);
    const targetBranch = deriveBranchRootId(centerId, edge.targetId, parents);
    if (!sourceBranch || !targetBranch || sourceBranch !== targetBranch) continue;
    if (selectedOriginNeighborIds.size > 0 && !selectedOriginNeighborIds.has(sourceBranch)) continue;
    const pairKey = stablePairKey(edge.sourceId, edge.targetId);
    const score = edgePriority(edge) + Math.min(sourceDepth, targetDepth) * 8;
    const existing = clusterCandidates.get(pairKey);
    if (!existing || score > existing.score) {
      clusterCandidates.set(pairKey, { sourceId: edge.sourceId, targetId: edge.targetId, sourceBranch, edge, score });
    }
  }

  const clusterEdges = Array.from(clusterCandidates.values())
    .sort((left, right) => right.score - left.score)
    .slice(0, MAX_CLUSTER_EDGES);

  for (const item of clusterEdges) {
    const sourceCoords = idToCoords.get(item.sourceId);
    const targetCoords = idToCoords.get(item.targetId);
    if (!sourceCoords || !targetCoords) continue;
    const coords = smoothRouteCoordinates(
      [
        [sourceCoords.lon, sourceCoords.lat],
        [targetCoords.lon, targetCoords.lat],
      ],
      `cluster:${item.sourceId}:${item.targetId}`,
      3,
    );
    graphFeatures.push({
      id: `cluster:${item.sourceId}:${item.targetId}`,
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: coords },
      properties: {
        kind: 'cluster',
        hop: 3,
        color: clusterStrokeColor(theme),
        channel: 'trace',
        corridorId: corridorAssignments.get(item.sourceBranch) || `trace-branch:${item.sourceBranch}`,
        featureRole: 'detail',
        endpointNodeIds: [item.sourceId, item.targetId],
        renderPriority: 10,
        baseOpacity: theme === 'dark' ? 0.42 : 0.34,
        baseWidth: 1.2,
        glowOpacity: theme === 'dark' ? 0.04 : 0.03,
      },
    } as GeoJSON.Feature<GeoJSON.LineString>);
    visibleNodeIds.add(item.sourceId);
    visibleNodeIds.add(item.targetId);
  }

  return { graphFeatures, visibleNodeIds };
}

function edgeAggregationCellSize(zoom: number): number {
  if (zoom >= 11.5) return 0;
  if (zoom >= 10) return 18;
  if (zoom >= 8.5) return 28;
  if (zoom >= 7) return 40;
  return 56;
}

function aggregateGraphFeaturesForZoom(
  map: maplibregl.Map,
  features: GeoJSON.Feature[],
): GeoJSON.Feature[] {
  const cellSize = edgeAggregationCellSize(map.getZoom());
  if (cellSize <= 0) return features;

  const labelsByLineId = new Map<string, GeoJSON.Feature<GeoJSON.Point>>();
  const passthrough: GeoJSON.Feature[] = [];
  const graphLines: GeoJSON.Feature<GeoJSON.LineString>[] = [];

  for (const feature of features) {
    const kind = String(feature.properties?.kind || '').trim();
    if (kind === 'label' && typeof feature.id === 'string' && feature.id.endsWith(':label')) {
      labelsByLineId.set(feature.id.slice(0, -6), feature as GeoJSON.Feature<GeoJSON.Point>);
      continue;
    }
    if (kind === 'primary' || kind === 'indirect' || kind === 'cluster') {
      if (feature.geometry?.type === 'LineString') graphLines.push(feature as GeoJSON.Feature<GeoJSON.LineString>);
      continue;
    }
    passthrough.push(feature);
  }

  const grouped = new Map<string, GeoJSON.Feature<GeoJSON.LineString>[]>();
  const lineScore = (feature: GeoJSON.Feature<GeoJSON.LineString>): number => {
    const width = Number(feature.properties?.baseWidth) || 0;
    const opacity = Number(feature.properties?.baseOpacity) || 0;
    const hop = Number(feature.properties?.hop) || 0;
    const renderPriority = Number(feature.properties?.renderPriority) || 0;
    return renderPriority * 10 + width * 10 + opacity * 6 + hop * 2;
  };

  for (const feature of graphLines) {
    const coords = feature.geometry.coordinates as [number, number][];
    if (coords.length < 2) {
      passthrough.push(feature);
      continue;
    }
    const kind = String(feature.properties?.kind || '').trim();
    const channel = String(feature.properties?.channel || '').trim();
    const start = map.project({ lng: coords[0][0], lat: coords[0][1] });
    const end = map.project({ lng: coords[coords.length - 1][0], lat: coords[coords.length - 1][1] });
    const sx = Math.round(start.x / cellSize);
    const sy = Math.round(start.y / cellSize);
    const ex = Math.round(end.x / cellSize);
    const ey = Math.round(end.y / cellSize);
    const key = `${channel}:${kind}:${sx}:${sy}:${ex}:${ey}`;
    if (!grouped.has(key)) grouped.set(key, []);
    grouped.get(key)!.push(feature);
  }

  const out: GeoJSON.Feature[] = [...passthrough];
  for (const group of grouped.values()) {
    const representative = [...group].sort((left, right) => lineScore(right) - lineScore(left))[0];
    const label = representative.id != null ? labelsByLineId.get(String(representative.id)) : undefined;
    if (group.length === 1) {
      out.push(representative);
      if (label) out.push(label);
      continue;
    }

    const merged = {
      ...representative,
      properties: {
        ...(representative.properties || {}),
        baseWidth: clamp((Number(representative.properties?.baseWidth) || 1) + Math.min(1.6, Math.log2(group.length + 1) * 0.45), 1, 6),
        baseOpacity: clamp((Number(representative.properties?.baseOpacity) || 0.5) + Math.min(0.14, group.length * 0.025), 0.2, 0.98),
        glowOpacity: clamp((Number(representative.properties?.glowOpacity) || 0.08) + Math.min(0.04, group.length * 0.01), 0.04, 0.3),
      },
    } as GeoJSON.Feature<GeoJSON.LineString>;
    const kind = String(representative.properties?.kind || '').trim();
    if (kind === 'indirect') {
      const starts = group.map((feature) => feature.geometry.coordinates[0] as [number, number]);
      const ends = group.map((feature) => feature.geometry.coordinates[feature.geometry.coordinates.length - 1] as [number, number]);
      const avg = (points: [number, number][]): [number, number] => [
        points.reduce((sum, point) => sum + point[0], 0) / points.length,
        points.reduce((sum, point) => sum + point[1], 0) / points.length,
      ];
      const start = avg(starts);
      const end = avg(ends);
      const distanceKm = approximateDistanceKm(start[0], start[1], end[0], end[1]);
      merged.geometry = {
        type: 'LineString',
        coordinates: gentleArcPoints(start, end, `merged:${String(representative.id || 'indirect')}`, distanceKm),
      };
    }
    out.push(merged);

    if (label) {
      const mergedLabel = {
        ...label,
        geometry: kind === 'indirect'
          ? { type: 'Point', coordinates: linePointAt((merged.geometry as GeoJSON.LineString).coordinates as [number, number][], 0.18) }
          : label.geometry,
        properties: {
          ...(label.properties || {}),
          labelText: representative.properties?.channel === 'supply_path'
            ? `Supply routes x${group.length}`
            : kind === 'cluster'
              ? `Cluster of ${group.length}`
              : `Towards ${group.length} nearby nodes`,
        },
      } as GeoJSON.Feature<GeoJSON.Point>;
      out.push(mergedLabel);
    }
  }

  return out;
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
    featureEndpointRefs(feature).forEach((key) => bucket.endpointKeys.add(key));
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
      ? averagePoint(endpointObjects.map((object) => [object.coordinates!.lon, object.coordinates!.lat] as [number, number]))
      : linePointAt(bucket.lineFeatures[0].geometry.coordinates as [number, number][], 0.76);
    const corridorType = inferCorridorType(bucket.channel, bucket.labelHints);
    const count = Math.max(endpointObjects.length, bucket.lineFeatures.length);
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

  const centroid = averagePoint(endpointEntries.map((entry) => entry.displayPoint));
  const splitPoint = pointOnSegment(centerPoint, centroid, 0.8);
  const features: GeoJSON.Feature[] = [{
    id: `${corridorId}:trunk`,
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
  } as GeoJSON.Feature<GeoJSON.LineString>];

  for (const endpoint of endpointEntries) {
    const midpoint = lerpPoint(splitPoint, endpoint.displayPoint, 0.5);
    const control = lerpPoint(midpoint, centroid, 0.58);
    features.push({
      id: `${corridorId}:branch:${endpoint.key}`,
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
  const baseRadiusKm = clamp(Math.max(avgNearestKm * 0.52, maxSpanKm * 0.18, 20), 16, 180);
  const pairLimitKm = Math.max(baseRadiusKm * 2.4, avgNearestKm * 1.18, 90);

  const nodes = rawNodes.map((node, index) => {
    const nearestKm = Number.isFinite(nearestDistances[index]) ? nearestDistances[index] : avgNearestKm;
    return {
      ...node,
      radiusKm: clamp(baseRadiusKm * 0.84 + nearestKm * 0.16, 18, baseRadiusKm * 1.12),
      weight: clamp(0.8 + (nearestKm / Math.max(avgNearestKm, 1)) * 0.08, 0.8, 0.96),
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
        radiusKm: clamp(distanceKm * 0.52, 14, baseRadiusKm * 0.94),
        weight: clamp(0.12 + (1 - distanceKm / pairLimitKm) * 0.16, 0.12, 0.28),
      });
    }
  }

  const allLobes = [
    ...nodes.map((node) => ({ x: node.x, y: node.y, radiusKm: node.radiusKm })),
    ...bridges.map((bridge) => ({ x: bridge.x, y: bridge.y, radiusKm: bridge.radiusKm })),
  ];
  const minX = Math.min(...allLobes.map((entry) => entry.x - entry.radiusKm * 1.12));
  const maxX = Math.max(...allLobes.map((entry) => entry.x + entry.radiusKm * 1.12));
  const minY = Math.min(...allLobes.map((entry) => entry.y - entry.radiusKm * 1.12));
  const maxY = Math.max(...allLobes.map((entry) => entry.y + entry.radiusKm * 1.12));

  return {
    centerLat,
    centerLon,
    threshold: nodes.length >= 6 ? 0.22 : nodes.length >= 4 ? 0.24 : 0.26,
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
  const cellKm = clamp(spanKm * 0.042, 5, 16);
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
            fillOpacity: (theme === 'dark' ? 0.044 : 0.035) * (0.54 + edgeFactor * 1.22),
            accentColor,
            accentOpacity: (theme === 'dark' ? 0.024 : 0.018) * Math.pow(edgeFactor, 1.08) * 1.22,
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
    if (local.x < metrics.minX || local.x > metrics.maxX || local.y < metrics.minY || local.y > metrics.maxY) continue;
    const strength = evaluateSelectedCorridorFieldStrength(metrics, local);
    if (strength >= metrics.threshold * 0.94) muted.add(object.id);
  }
  return Array.from(muted);
}

function deriveNodeDepths(centerId: string, edges: RawDependencyEdge[]): {
  depths: Map<string, number>;
  parents: Map<string, string>;
} {
  const adjacency = new Map<string, Set<string>>();
  const link = (from: string, to: string) => {
    if (!adjacency.has(from)) adjacency.set(from, new Set());
    adjacency.get(from)!.add(to);
  };
  for (const edge of edges) {
    link(edge.sourceId, edge.targetId);
    link(edge.targetId, edge.sourceId);
  }

  const depths = new Map<string, number>([[centerId, 0]]);
  const parents = new Map<string, string>();
  const queue = [centerId];
  while (queue.length > 0) {
    const current = queue.shift()!;
    const currentDepth = depths.get(current) ?? 0;
    if (currentDepth >= MAX_HOPS) continue;
    for (const next of adjacency.get(current) || []) {
      if (depths.has(next)) continue;
      depths.set(next, currentDepth + 1);
      parents.set(next, current);
      queue.push(next);
    }
  }
  return { depths, parents };
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function deriveBranchRootId(centerId: string, nodeId: string, parents: Map<string, string>): string {
  let current = nodeId;
  let parent = parents.get(current);
  while (parent && parent !== centerId) {
    current = parent;
    parent = parents.get(current);
  }
  return current;
}

function buildParentLinkFamilies(centerId: string, rawEdges: RawDependencyEdge[], parents: Map<string, string>): Map<string, EdgeFamily> {
  const byChild = new Map<string, { family: EdgeFamily; priority: number }>();
  for (const edge of rawEdges) {
    const sourceParent = parents.get(edge.sourceId);
    const targetParent = parents.get(edge.targetId);
    const family = edgeFamily(edge);
    const priority = edgePriority(edge);
    if (sourceParent === edge.targetId) {
      const prev = byChild.get(edge.sourceId);
      if (!prev || priority > prev.priority) {
        byChild.set(edge.sourceId, { family, priority });
      }
    }
    if (targetParent === edge.sourceId) {
      const prev = byChild.get(edge.targetId);
      if (!prev || priority > prev.priority) {
        byChild.set(edge.targetId, { family, priority });
      }
    }
  }
  if (!byChild.has(centerId)) {
    byChild.set(centerId, { family: 'other', priority: 0 });
  }
  return new Map(Array.from(byChild.entries()).map(([key, value]) => [key, value.family]));
}

function isSemanticBridge(parentFamily: EdgeFamily | null, nextFamily: EdgeFamily): boolean {
  if (!parentFamily) return false;
  if (parentFamily === nextFamily) return true;
  if ((parentFamily === 'supply' || parentFamily === 'control' || parentFamily === 'risk') && (nextFamily === 'location' || nextFamily === 'market')) {
    return true;
  }
  if ((parentFamily === 'location' || parentFamily === 'market') && (nextFamily === 'supply' || nextFamily === 'control' || nextFamily === 'risk')) {
    return true;
  }
  if (nextFamily === 'influence' && (parentFamily === 'supply' || parentFamily === 'control' || parentFamily === 'risk')) {
    return true;
  }
  return false;
}

function continuationScoreForCandidate(
  type: string,
  family: EdgeFamily,
  hop: 1 | 2 | 3,
  distanceKm: number,
  parentFamily: EdgeFamily | null,
  rootFamily: EdgeFamily | null,
): number {
  const priority = edgePriority(type);
  let score = priority;
  if (parentFamily && family === parentFamily) score += 28;
  if (rootFamily && family === rootFamily) score += 16;
  if (isSemanticBridge(parentFamily, family)) score += 18;
  if (isSemanticBridge(rootFamily, family)) score += 10;
  if (family === 'location') score += hop === 2 ? 8 : 3;
  if (family === 'market') score += hop === 2 ? 7 : 3;
  if (family === 'influence') score += 6;
  score -= hop === 2 ? distanceKm / 95 : distanceKm / 70;
  if (hop === 3 && family === 'location') score -= 8;
  if (hop === 3 && family === 'social') score -= 6;
  return score;
}

function dedupeBestByPair<T extends SelectedTraceEdge | RenderDependencyEdge>(edges: T[], scoreFor: (edge: T) => number): T[] {
  const byPair = new Map<string, T>();
  for (const edge of edges) {
    const pairKey = edge.pairKey;
    const existing = byPair.get(pairKey);
    if (!existing || scoreFor(edge) > scoreFor(existing)) {
      byPair.set(pairKey, edge);
    }
  }
  return Array.from(byPair.values());
}

function buildSelectedTraceEdges(
  centerId: string,
  rawEdges: RawDependencyEdge[],
  idToCoords: Map<string, { lon: number; lat: number }>,
): SelectedTraceEdge[] {
  const { depths, parents } = deriveNodeDepths(centerId, rawEdges);
  const parentLinkFamilies = buildParentLinkFamilies(centerId, rawEdges, parents);
  const hop1Candidates: SelectedTraceEdge[] = [];
  const hop2Candidates: SelectedTraceEdge[] = [];
  const hop3Candidates: SelectedTraceEdge[] = [];

  for (const edge of rawEdges) {
    const sourceDepth = depths.get(edge.sourceId);
    const targetDepth = depths.get(edge.targetId);
    if (sourceDepth == null || targetDepth == null) continue;

    const minDepth = Math.min(sourceDepth, targetDepth);
    const maxDepth = Math.max(sourceDepth, targetDepth);
    if (maxDepth < 1 || maxDepth > MAX_HOPS) continue;

    const sourceCoords = idToCoords.get(edge.sourceId) ?? null;
    const targetCoords = idToCoords.get(edge.targetId) ?? null;

    if (maxDepth - minDepth > 1) continue;
    if (maxDepth > 1) {
      const deeperId = sourceDepth > targetDepth ? edge.sourceId : edge.targetId;
      const shallowerId = deeperId === edge.sourceId ? edge.targetId : edge.sourceId;
      if (parents.get(deeperId) !== shallowerId) continue;
    }

    const hop = maxDepth as 1 | 2 | 3;
    const family = edgeFamily(edge);
    const shallowerId = sourceDepth <= targetDepth ? edge.sourceId : edge.targetId;
    const deeperId = shallowerId === edge.sourceId ? edge.targetId : edge.sourceId;
    const branchRootId = hop === 1
      ? (sourceDepth === 1 ? edge.sourceId : edge.targetId)
      : deriveBranchRootId(centerId, deeperId, parents);
    const parentFamily = hop === 1 ? null : (parentLinkFamilies.get(shallowerId) ?? null);
    const rootFamily = hop === 1
      ? family
      : (parentLinkFamilies.get(branchRootId) ?? parentFamily ?? null);
    const distanceKm = sourceCoords && targetCoords
      ? approximateDistanceKm(
        sourceCoords.lon,
        sourceCoords.lat,
        targetCoords.lon,
        targetCoords.lat,
      )
      : 0;
    const continuationScore = continuationScoreForCandidate(edge.type, family, hop, distanceKm, parentFamily, rootFamily);
    const pairKey = stablePairKey(edge.sourceId, edge.targetId);
    const edgeId = `${pairKey}:${edge.type}`;
    const candidate: SelectedTraceEdge = {
      ...edge,
      edgeId,
      pairKey,
      hop,
      minDepth,
      family,
      distanceKm,
      branchRootId,
      parentNodeId: hop === 1 ? null : shallowerId,
      parentFamily,
      rootFamily,
      continuationScore,
      sourceCoords,
      targetCoords,
    };

    if (hop === 1) hop1Candidates.push(candidate);
    else if (hop === 2) hop2Candidates.push(candidate);
    else hop3Candidates.push(candidate);
  }

  const directEdges = dedupeBestByPair(hop1Candidates, (edge) => edge.continuationScore + 1000);

  const groupedHop2 = new Map<string, SelectedTraceEdge[]>();
  for (const edge of dedupeBestByPair(hop2Candidates, (candidate) => candidate.continuationScore)) {
    const key = edge.parentNodeId || edge.branchRootId;
    if (!groupedHop2.has(key)) groupedHop2.set(key, []);
    groupedHop2.get(key)!.push(edge);
  }
  const selectedHop2: SelectedTraceEdge[] = [];
  for (const group of groupedHop2.values()) {
    const ranked = group.sort((left, right) => right.continuationScore - left.continuationScore);
    const threshold = (ranked[0]?.continuationScore ?? 0) - 14;
    ranked
      .filter((edge, index) => index < 4 || edge.continuationScore >= threshold)
      .slice(0, 5)
      .forEach((edge) => selectedHop2.push(edge));
  }

  const allowedHop2Nodes = new Set(selectedHop2.map((edge) => edge.sourceId).concat(selectedHop2.map((edge) => edge.targetId)));
  const groupedHop3 = new Map<string, SelectedTraceEdge[]>();
  for (const edge of dedupeBestByPair(hop3Candidates, (candidate) => candidate.continuationScore)) {
    if (edge.parentNodeId && !allowedHop2Nodes.has(edge.parentNodeId)) continue;
    const key = edge.parentNodeId || edge.branchRootId;
    if (!groupedHop3.has(key)) groupedHop3.set(key, []);
    groupedHop3.get(key)!.push(edge);
  }
  const selectedHop3: SelectedTraceEdge[] = [];
  for (const group of groupedHop3.values()) {
    const ranked = group.sort((left, right) => right.continuationScore - left.continuationScore);
    const threshold = (ranked[0]?.continuationScore ?? 0) - 10;
    ranked
      .filter((edge, index) => index < 2 || edge.continuationScore >= threshold)
      .slice(0, 3)
      .forEach((edge) => selectedHop3.push(edge));
  }

  const selectedEdges = [...directEdges, ...selectedHop2, ...selectedHop3].sort((left, right) => {
    if (left.hop !== right.hop) return left.hop - right.hop;
    return right.continuationScore - left.continuationScore;
  });

  if (selectedEdges.length > 0) return selectedEdges;

  const broadFallback = dedupeBestByPair(
    [...hop1Candidates, ...hop2Candidates, ...hop3Candidates],
    (edge) => edge.continuationScore - edge.hop * 10,
  )
    .sort((left, right) => {
      if (left.hop !== right.hop) return left.hop - right.hop;
      return right.continuationScore - left.continuationScore;
    })
    .slice(0, 16);

  return broadFallback;
}

function buildRenderableEdges(centerId: string, edges: SelectedTraceEdge[], theme: ThemeMode): RenderDependencyEdge[] {
  const nodeLinkById = new Map<string, SelectedTraceEdge>();
  const childCounts = new Map<string, number>();

  for (const edge of edges) {
    if (edge.hop === 1 && edge.sourceId !== centerId && edge.targetId !== centerId) continue;
    const deeperNodeId = edge.hop === 1
      ? (edge.sourceId === centerId ? edge.targetId : edge.sourceId)
      : (edge.parentNodeId === edge.sourceId ? edge.targetId : edge.sourceId);
    const existing = nodeLinkById.get(deeperNodeId);
    if (!existing || edge.continuationScore > existing.continuationScore) {
      nodeLinkById.set(deeperNodeId, edge);
    }
    const parentId = edge.hop === 1 ? null : edge.parentNodeId;
    if (parentId) {
      childCounts.set(parentId, (childCounts.get(parentId) || 0) + 1);
    }
  }

  const routes: TraceRoute[] = [];
  for (const [nodeId, edge] of nodeLinkById.entries()) {
    const hasChildren = (childCounts.get(nodeId) || 0) > 0;
    if (hasChildren) continue;

    const pathEdges: SelectedTraceEdge[] = [];
    let currentId = nodeId;
    while (true) {
      const currentEdge = nodeLinkById.get(currentId);
      if (!currentEdge) break;
      pathEdges.push(currentEdge);
      currentId = currentEdge.hop === 1 ? '' : String(currentEdge.parentNodeId || '').trim();
      if (!currentId) break;
    }

    const orderedEdges = [...pathEdges].reverse();
    const points: [number, number][] = [];
    let invalid = false;
    for (const pathEdge of orderedEdges) {
      if (pathEdge.hop === 1) {
        if (!pathEdge.sourceCoords || !pathEdge.targetCoords) {
          invalid = true;
          break;
        }
        if (pathEdge.sourceId !== centerId && pathEdge.targetId !== centerId) {
          invalid = true;
          break;
        }
        const start = pathEdge.sourceId === centerId ? pathEdge.sourceCoords : pathEdge.targetCoords;
        const end = pathEdge.sourceId === centerId ? pathEdge.targetCoords : pathEdge.sourceCoords;
        if (points.length === 0) points.push([start.lon, start.lat]);
        points.push([end.lon, end.lat]);
        continue;
      }

      const parentId = String(pathEdge.parentNodeId || '').trim();
      const parentCoords = parentId === pathEdge.sourceId ? pathEdge.sourceCoords : pathEdge.targetCoords;
      const childCoords = parentId === pathEdge.sourceId ? pathEdge.targetCoords : pathEdge.sourceCoords;
      if (!parentCoords || !childCoords) {
        invalid = true;
        break;
      }
      if (points.length === 0) points.push([parentCoords.lon, parentCoords.lat]);
      points.push([childCoords.lon, childCoords.lat]);
    }
    if (invalid || points.length < 2) continue;

    const deepestEdge = orderedEdges[orderedEdges.length - 1];
    const distanceFactor = deepestEdge.hop === 1
      ? 1
      : deepestEdge.hop === 2
        ? clamp(1.1 - deepestEdge.distanceKm / 3600, 0.62, 1)
        : clamp(1 - deepestEdge.distanceKm / 3000, 0.48, 0.84);
    const familyWeight = FAMILY_VISUAL_WEIGHT[deepestEdge.family];
    const baseOpacity = clamp(
      (deepestEdge.hop === 1 ? 0.92 : deepestEdge.hop === 2 ? 0.68 : 0.48) * distanceFactor * familyWeight,
      deepestEdge.hop === 1 ? 0.66 : deepestEdge.hop === 2 ? 0.38 : 0.24,
      0.96,
    );
    const baseWidth = clamp(
      (deepestEdge.hop === 1 ? 2.15 : deepestEdge.hop === 2 ? 1.85 : 1.48)
        * (0.92 + distanceFactor * 0.18)
        * Math.max(0.92, familyWeight),
      deepestEdge.hop === 1 ? 1.85 : deepestEdge.hop === 2 ? 1.4 : 1.12,
      deepestEdge.hop === 1 ? 2.55 : deepestEdge.hop === 2 ? 2.15 : 1.68,
    );

    routes.push({
      routeId: `route:${nodeId}:${deepestEdge.type}`,
      targetNodeId: nodeId,
      branchRootId: deepestEdge.branchRootId,
      hop: deepestEdge.hop,
      type: deepestEdge.type,
      family: deepestEdge.family,
      continuationScore: deepestEdge.continuationScore,
      color: routeStrokeColor(theme, deepestEdge.hop),
      rawCoordinates: points,
      coordinates: points,
      baseOpacity,
      baseWidth,
      glowOpacity: clamp(baseOpacity * (deepestEdge.hop === 1 ? 0.22 : deepestEdge.hop === 2 ? 0.18 : 0.15), 0.06, 0.22),
    });
  }

  const dedupedRoutes = new Map<string, TraceRoute>();
  for (const route of routes) {
    const routeKey = `${route.targetNodeId}:${route.hop}`;
    const existing = dedupedRoutes.get(routeKey);
    if (!existing || route.continuationScore > existing.continuationScore) {
      dedupedRoutes.set(routeKey, route);
    }
  }

  const groupedRoutes = new Map<string, TraceRoute[]>();
  for (const route of dedupedRoutes.values()) {
    const source = route.coordinates[0];
    const next = route.coordinates[1] || route.coordinates[route.coordinates.length - 1];
    const angle = Math.atan2(next[1] - source[1], next[0] - source[0]);
    const angleBucket = Math.round(angle / (Math.PI / 6));
    const groupKey = `corridor:${angleBucket}`;
    if (!groupedRoutes.has(groupKey)) groupedRoutes.set(groupKey, []);
    groupedRoutes.get(groupKey)!.push(route);
  }

  const reducedRoutes: TraceRoute[] = [];
  for (const group of groupedRoutes.values()) {
    group.sort((left, right) => {
      if (left.hop !== right.hop) return left.hop - right.hop;
      return left.targetNodeId.localeCompare(right.targetNodeId);
    });
    if (group.length === 1) {
      const only = group[0];
      only.coordinates = bundleRouteCoordinates(only.rawCoordinates, only.routeId, only.hop, 0, 1);
      reducedRoutes.push(only);
      continue;
    }

    const representative = [...group].sort((left, right) => {
      if (left.hop !== right.hop) return right.hop - left.hop;
      return right.continuationScore - left.continuationScore;
    })[0];
    representative.coordinates = bundleRouteCoordinates(representative.rawCoordinates, representative.routeId, representative.hop, 0, 1);
    representative.baseWidth = clamp(representative.baseWidth + Math.log2(group.length + 1) * 0.55, representative.baseWidth, representative.baseWidth + 1.8);
    representative.baseOpacity = clamp(representative.baseOpacity + Math.min(0.16, group.length * 0.035), representative.baseOpacity, 0.96);
    representative.glowOpacity = clamp(representative.glowOpacity + Math.min(0.06, group.length * 0.015), representative.glowOpacity, 0.28);
    representative.color = routeStrokeColor(theme, representative.hop, group.length);
    reducedRoutes.push(representative);
  }

  const visibleRoutes = reducedRoutes
    .sort((left, right) => {
      const leftScore = left.continuationScore + left.hop * 18 + left.baseWidth * 6;
      const rightScore = right.continuationScore + right.hop * 18 + right.baseWidth * 6;
      return rightScore - leftScore;
    })
    .slice(0, MAX_VISIBLE_CORRIDORS);

  return visibleRoutes.flatMap((route) => {
    const seedEdge = nodeLinkById.get(route.targetNodeId);
    if (!seedEdge) return [];
    return [{
      ...seedEdge,
      edgeId: route.routeId,
      pairKey: `${route.targetNodeId}:${route.hop}`,
      color: route.color,
      coordinates: route.coordinates,
      baseOpacity: route.baseOpacity,
      baseWidth: route.baseWidth,
      glowOpacity: route.glowOpacity,
    }];
  });
}

function buildPropagationFeatures(
  centerId: string,
  propagation: { edges?: Array<{ edgeId?: string; sourceFragmentId?: string; targetFragmentId?: string; hopsFromSource?: number }>; nodeIds?: string[] } | null | undefined,
  idToCoords: Map<string, { lon: number; lat: number }>,
  objectById: Map<string, TraceObjectInfo>,
  depths: Map<string, number>,
  theme: ThemeMode,
  suppressedPairs: Set<string>,
): GeoJSON.Feature[] {
  const rawEdges = Array.isArray(propagation?.edges) ? propagation.edges : [];
  const selected: GeoJSON.Feature[] = [];
  const seenPairs = new Set<string>();
  const centerCoords = idToCoords.get(centerId);
  const radialRoutes: RadialBundleRoute[] = [];

  for (const edge of rawEdges) {
    const sourceId = String(edge?.sourceFragmentId || '').trim();
    const targetId = String(edge?.targetFragmentId || '').trim();
    if (shouldSuppressConnection(sourceId, targetId, objectById)) continue;
    const source = idToCoords.get(sourceId);
    const target = idToCoords.get(targetId);
    if (!source || !target) continue;
    const sourceCanonicalPath = String(objectById.get(sourceId)?.canonicalPath || '').trim();
    const targetCanonicalPath = String(objectById.get(targetId)?.canonicalPath || '').trim();
    const pairKey = stablePairKey(sourceId, targetId);
    const canonicalPairKey = sourceCanonicalPath && targetCanonicalPath
      ? stablePairKey(sourceCanonicalPath, targetCanonicalPath)
      : '';
    if (suppressedPairs.has(pairKey) || (canonicalPairKey && suppressedPairs.has(canonicalPairKey))) continue;
    if (seenPairs.has(pairKey)) continue;
    seenPairs.add(pairKey);
    const hop = Math.max(1, Math.min(4, Number(edge?.hopsFromSource) || 1));
    const kind = topologyKindForNodes(centerId, sourceId, targetId, depths, hop);
    const opacity = kind === 'primary'
      ? clamp(theme === 'dark' ? 0.64 : 0.54, 0.2, 0.8)
      : kind === 'indirect'
        ? clamp(theme === 'dark' ? 0.56 : 0.46, 0.2, 0.7)
        : clamp(theme === 'dark' ? 0.32 : 0.26, 0.16, 0.5);
    const width = kind === 'primary'
      ? 1.7
      : kind === 'indirect'
        ? 1.35
        : 0.95;
    const routeId = `prop:${edge?.edgeId || pairKey}`;
    const sourceIsCenter = sourceId === centerId;
    const targetIsCenter = targetId === centerId;
    if (centerCoords && (sourceIsCenter || targetIsCenter)) {
      const end = sourceIsCenter ? [target.lon, target.lat] as [number, number] : [source.lon, source.lat] as [number, number];
      radialRoutes.push({
        routeId,
        targetId: sourceIsCenter ? targetId : sourceId,
        corridorId: `prop-corridor:${sourceIsCenter ? targetId : sourceId}`,
        endPoint: end,
        kind,
        color: propagationStrokeColor(theme, kind),
        baseOpacity: opacity,
        baseWidth: width,
        glowOpacity: kind === 'cluster' ? 0 : clamp(opacity * 0.18, 0.03, 0.12),
        renderPriority: kind === 'primary' ? 12 : kind === 'indirect' ? 9 : 6,
      });
      continue;
    }
    const coordinates: [number, number][] = kind === 'indirect'
      ? arcPoints(source.lon, source.lat, target.lon, target.lat, `prop:${pairKey}`, 2, 18)
      : [
        [source.lon, source.lat],
        [target.lon, target.lat],
      ];
    selected.push({
      id: routeId,
      type: 'Feature',
      geometry: {
        type: 'LineString',
        coordinates,
      },
      properties: {
        kind,
        channel: 'propagation',
        hop,
        corridorId: `prop-corridor:${sourceIsCenter ? targetId : sourceId}`,
        featureRole: 'overview',
        endpointNodeIds: [sourceId, targetId],
        sourceId,
        targetId,
        color: propagationStrokeColor(theme, kind),
        baseOpacity: opacity,
        baseWidth: width,
        glowOpacity: kind === 'cluster' ? 0 : clamp(opacity * 0.18, 0.03, 0.12),
        renderPriority: kind === 'primary' ? 12 : kind === 'indirect' ? 9 : 6,
        semanticDirection: sourceId === centerId ? 'downstream' : targetId === centerId ? 'upstream' : 'context',
      },
    });
  }

  selected.push(...buildRadialBundleFeatures(
    centerCoords ? [centerCoords.lon, centerCoords.lat] : null,
    radialRoutes,
    theme,
    'propagation',
    (group) => `Propagation corridor x${group.length}`,
    { labelSingles: false, labelBundles: false },
  ).features);

  return selected;
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

function buildTraceSummary(
  edges: SelectedTraceEdge[],
  objectById: Map<string, TraceObjectInfo>,
  centerId: string,
  visibleEdges: number,
  supplyRouteCount: number,
  pathArtifacts: MaterializedArtifact[],
  propagation?: { nodeIds?: string[]; edges?: Array<unknown> } | null,
): GeoEdgeTraceSummary {
  const hopCounts: Record<1 | 2 | 3, number> = { 1: 0, 2: 0, 3: 0 };
  const familyCounts = new Map<EdgeFamily, number>();
  const branchIds = new Set<string>();
  const countryGroups = new Map<string, {
    countryIso2: string | null;
    countryName: string;
    items: Array<{
      objectId: string;
      label: string;
      type: string;
      hop: 1 | 2 | 3;
      relationLabel: string;
      businessMeaning: string;
      score: number;
    }>;
  }>();

  for (const edge of edges) {
    hopCounts[edge.hop] += 1;
    familyCounts.set(edge.family, (familyCounts.get(edge.family) || 0) + 1);
    branchIds.add(edge.branchRootId);

    const targetObjectId = edge.sourceId === centerId ? edge.targetId : edge.targetId === centerId ? edge.sourceId : (
      edge.hop === edge.minDepth ? edge.targetId : edge.sourceId
    );
    const targetObject = objectById.get(targetObjectId);
    if (!targetObject || targetObject.id === centerId) continue;

    const countryDisplay = getCountryDisplay(targetObject.canonicalPath);
    const groupKey = countryDisplay.iso2 || 'unknown';
    if (!countryGroups.has(groupKey)) {
      countryGroups.set(groupKey, {
        countryIso2: countryDisplay.iso2,
        countryName: countryDisplay.name,
        items: [],
      });
    }

    const group = countryGroups.get(groupKey)!;
    const score = 100 - edge.hop * 12 + edge.continuationScore;
    const existingItem = group.items.find((item) => item.objectId === targetObject.id);
    const nextItem = {
      objectId: targetObject.id,
      label: targetObject.label || humanizeCanonicalPath(targetObject.canonicalPath) || targetObject.canonicalPath,
      type: humanizeFragmentType(targetObject.type),
      hop: edge.hop,
      relationLabel: humanizeEdgeType(edge.type),
      businessMeaning: businessMeaningForEdgeType(edge),
      score,
    };
    if (!existingItem) {
      group.items.push(nextItem);
    } else if (score > existingItem.score) {
      Object.assign(existingItem, nextItem);
    }
  }
  const pinnedPathCount = pathArtifacts.filter((artifact) => artifact.isPinned).length;
  const discoveredPathCount = Math.max(0, pathArtifacts.length - pinnedPathCount);
  const stressedPathCount = pathArtifacts.filter((artifact) => (artifact.pathStressScore ?? artifact.payload?.stress?.pathScore ?? 0) >= 0.58).length;
  const bottleneckPathCount = pathArtifacts.filter((artifact) => {
    const value = artifact.bottleneckLegIndex ?? artifact.payload?.stress?.bottleneckLegIndex;
    return typeof value === 'number' && Number.isFinite(value) && value >= 0;
  }).length;
  const directPathCount = pathArtifacts.filter((artifact) => artifact.traceRole === 'direct_anchor').length;
  const routePathCount = pathArtifacts.filter((artifact) => artifact.traceRole === 'route_overlap' || artifact.traceRole === 'route_context').length;
  const zonePathCount = pathArtifacts.filter((artifact) => artifact.traceRole === 'zone_overlap' || artifact.traceRole === 'zone_context').length;
  const affectedPathCount = pathArtifacts.filter((artifact) => artifact.traceRole === 'affected_zone_overlap' || artifact.traceRole === 'affected_context').length;

  return {
    totalEdges: edges.length,
    visibleEdges,
    supplyRouteCount,
    hopCounts,
    branchCount: branchIds.size,
    pathArtifactCount: pathArtifacts.length,
    pathLegCount: pathArtifacts.reduce((acc, artifact) => acc + (Array.isArray((artifact as any)?.payload?.legs) ? (artifact as any).payload.legs.length : 0), 0),
    pinnedPathCount,
    discoveredPathCount,
    stressedPathCount,
    bottleneckPathCount,
    directPathCount,
    routePathCount,
    zonePathCount,
    affectedPathCount,
    propagationNodeCount: Array.isArray(propagation?.nodeIds) ? propagation.nodeIds.length : 0,
    propagationEdgeCount: Array.isArray(propagation?.edges) ? propagation.edges.length : 0,
    topFamilies: Array.from(familyCounts.entries())
      .sort((left, right) => right[1] - left[1])
      .slice(0, 3)
      .map(([family, count]) => ({ family, count })),
    countryHighlights: Array.from(countryGroups.values())
      .map((group) => ({
        countryIso2: group.countryIso2,
        countryName: group.countryName,
        itemCount: group.items.length,
        items: group.items
          .sort((left, right) => right.score - left.score)
          .slice(0, 3)
          .map(({ score: _score, ...item }) => item),
      }))
      .sort((left, right) => right.itemCount - left.itemCount)
      .slice(0, 4),
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
        ['+', ['get', 'corridorRadius'], 8],
        ['+', ['get', 'corridorRadius'], 5],
      ],
      'circle-color': '#f59e0b',
      'circle-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.22,
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
      'circle-color': theme === 'dark' ? 'rgba(44,22,2,0.92)' : 'rgba(120,53,15,0.92)',
      'circle-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.98,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.42,
        0.88,
      ],
      'circle-stroke-color': ['get', 'color'],
      'circle-stroke-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        1,
        ['boolean', ['get', 'corridorDimmed'], false],
        0.45,
        0.82,
      ],
      'circle-stroke-width': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        2.8,
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
      'text-field': '↔',
      'text-font': [...MAP_TEXT_FONT_STACK],
      'text-size': 13,
      'text-anchor': 'center',
      'text-allow-overlap': true,
      'text-ignore-placement': true,
    },
    paint: {
      'text-color': '#fbbf24',
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.9)' : 'rgba(120,53,15,0.88)',
      'text-halo-width': 0.8,
      'text-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        1,
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
      'text-size': 11,
      'text-offset': [0, 1.25],
      'text-anchor': 'top',
      'text-allow-overlap': true,
      'text-ignore-placement': true,
      'text-max-width': 10,
    },
    paint: {
      'text-color': theme === 'dark' ? 'rgba(241,245,249,0.96)' : 'rgba(15,23,42,0.92)',
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.94)' : 'rgba(255,255,255,0.94)',
      'text-halo-width': 1.2,
      'text-opacity': [
        'case',
        ['boolean', ['get', 'corridorSelected'], false],
        0.98,
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
  const mountedRef = useRef(true);
  const interactionRef = useRef<EdgeInteractionState>({ corridorId: null, endpointKey: null });

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (!map || !isReady) return;
    let hoveredEdgeId: string | number | null = null;
    let baseFeatures: GeoJSON.Feature[] = [];
    let baseCorridorFeatures: GeoJSON.Feature[] = [];
    let baseEndpointEntries: Array<{ key: string; object: GeoObject; corridorIds: string[] }> = [];

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

    const suppressMarkerClear = () => {
      (map as unknown as { __suppressGeoMarkerClear?: boolean }).__suppressGeoMarkerClear = true;
    };

    const featureCorridorId = (feature: GeoJSON.Feature): string => String(feature.properties?.corridorId || '').trim();
    const featureRole = (feature: GeoJSON.Feature): string => String(feature.properties?.featureRole || '').trim();
    const featureHasEndpoint = (feature: GeoJSON.Feature, endpointKey: string): boolean =>
      featureEndpointRefs(feature).includes(endpointKey);

    const applyDisplayState = () => {
      if (!canAccessMapStyle(map) || !map.getSource(SRC) || !map.getSource(ENDPOINT_SRC)) return;
      const state = interactionRef.current;
      const isOverview = !state.corridorId;
      const selectedEndpointKey = state.endpointKey;
      const corridorFeatures = baseFeatures.filter((feature) => {
        const corridorId = featureCorridorId(feature);
        if (isOverview) return false;
        return corridorId === state.corridorId;
      });
      const corridorLineFeatures = corridorFeatures.filter((feature) => feature.geometry?.type === 'LineString');
      const detailLineFeatures = corridorLineFeatures.filter((feature) => featureRole(feature) === 'detail');
      const selectedLineFeatures = (selectedEndpointKey
        ? (detailLineFeatures.length > 0 ? detailLineFeatures : corridorLineFeatures).filter((feature) => featureHasEndpoint(feature, selectedEndpointKey))
        : (detailLineFeatures.length > 0 ? detailLineFeatures : corridorLineFeatures));
      const selectedCorridorEntries = baseEndpointEntries.filter((entry) => {
        if (!state.corridorId) return false;
        if (!entry.corridorIds.includes(state.corridorId)) return false;
        if (!state.endpointKey) return true;
        return entry.key === state.endpointKey;
      });
      const spreadEntries = (!isOverview && selectedCorridorEntries.length > 0)
        ? spreadEndpointEntries(map, selectedCorridorEntries.map(({ key, object }) => ({ key, object })))
        : [];
      const centerPoint = selectedObject?.coordinates
        ? [selectedObject.coordinates.lon, selectedObject.coordinates.lat] as [number, number]
        : null;
      const fieldFeatures = (!isOverview && !state.endpointKey && state.corridorId)
        ? buildSelectedCorridorFieldFeatures(spreadEntries, state.corridorId, theme)
        : [];
      const displayFeatures = isOverview
        ? decorateCorridorFeatures(baseCorridorFeatures, null)
        : [
          ...decorateCorridorFeatures(
            baseCorridorFeatures.filter((feature) =>
              !(featureCorridorId(feature) === state.corridorId && String(feature.properties?.kind || '').trim() === 'corridor-guide'),
            ),
            state.corridorId,
          ),
          ...fieldFeatures,
          ...(state.endpointKey
            ? selectedLineFeatures
            : buildSelectedCorridorBushFeatures(
              centerPoint,
              selectedLineFeatures,
              spreadEntries,
            )),
        ];
      (map.getSource(SRC) as maplibregl.GeoJSONSource).setData({
        type: 'FeatureCollection',
        features: displayFeatures,
      });

      (map.getSource(ENDPOINT_SRC) as maplibregl.GeoJSONSource).setData(
        buildEndpointFeatureCollection(spreadEntries, theme),
      );
      setMutedObjectIds(
        !isOverview && !state.endpointKey
          ? buildSelectedCorridorMutedObjectIds(spreadEntries, objects, selectedObject?.id || null)
          : [],
      );
      bringEndpointLayersToFront();
      setVisibleEdgesCount(displayFeatures.filter((feature) => feature.geometry?.type === 'LineString').length);
    };

    const handleEdgeClick = (event: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feature = event.features?.[0] as GeoJSON.Feature | undefined;
      if (!feature) return;
      const corridorId = featureCorridorId(feature);
      if (!corridorId) return;
      suppressMarkerClear();
      if (interactionRef.current.corridorId !== corridorId) {
        interactionRef.current = { corridorId, endpointKey: null };
      } else if (interactionRef.current.endpointKey) {
        interactionRef.current = { corridorId, endpointKey: null };
      }
      applyDisplayState();
    };

    const handleEndpointClick = (event: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feature = event.features?.[0];
      const endpointKey = String(feature?.properties?.endpointKey || '').trim();
      if (!endpointKey || !interactionRef.current.corridorId) return;
      suppressMarkerClear();
      interactionRef.current = {
        corridorId: interactionRef.current.corridorId,
        endpointKey: interactionRef.current.endpointKey === endpointKey ? null : endpointKey,
      };
      applyDisplayState();
    };

    if (!selectedObject) {
      interactionRef.current = { corridorId: null, endpointKey: null };
      removeLayers();
      setDiscoveredObjects([]);
      setEdgesLoading(false);
      setEdgesCount(0);
      setVisibleEdgesCount(0);
      setTraceSummary(null);
      setRelatedPathArtifacts([]);
      setMutedObjectIds([]);
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
    interactionRef.current = { corridorId: null, endpointKey: null };

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

        const graph = res?.graph ?? {};
        const edges = graph?.edges ?? [];
        const serverCoords = graph?.coordinates ?? {};
        const fragments = graph?.fragments ?? [];
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

        const rawEdges: RawDependencyEdge[] = [];
        for (const edge of edges) {
          const sourceId = String(edge?.sourceFragmentId ?? edge?.sourceFragmentID ?? '').trim();
          const targetId = String(edge?.targetFragmentId ?? edge?.targetFragmentID ?? '').trim();
          const type = normalizeEdgeType(edge?.type);
          if (!sourceId || !targetId || !type) continue;
          const candidate: RawDependencyEdge = {
            sourceId,
            targetId,
            type,
            family: typeof edge?.family === 'string' ? edge.family as EdgeFamily : undefined,
            priority: typeof edge?.priority === 'number' ? edge.priority : undefined,
            businessMeaning: typeof edge?.businessMeaning === 'string' ? edge.businessMeaning : undefined,
            semanticDirection: typeof edge?.semanticDirection === 'string' ? edge.semanticDirection : undefined,
          };
          if (shouldSuppressRawEdge(candidate, objectById)) continue;
          rawEdges.push(candidate);
        }

        const { depths, parents } = deriveNodeDepths(selectedObject.id, rawEdges);
        const selectedEdges = buildSelectedTraceEdges(selectedObject.id, rawEdges, idToCoords);
        const { graphFeatures, visibleNodeIds } = buildTraceVisualFeatures(
          selectedObject.id,
          selectedEdges,
          rawEdges,
          depths,
          parents,
          objectById,
          idToCoords,
          theme,
        );
        const {
          features: supplyPathFeatures,
          visiblePathRefs,
          coveredPairs: supplyCoveredPairs,
          routeCount: supplyRouteCount,
        } = buildSupplyPathFeatures(pathArtifacts, coordsByPath, objectByPath, theme, selectedObject);
        const propagationTopologyEdges: RawDependencyEdge[] = [];
        for (const edge of Array.isArray(res?.propagation?.edges) ? res.propagation.edges : []) {
          const sourceId = String(edge?.sourceFragmentId || '').trim();
          const targetId = String(edge?.targetFragmentId || '').trim();
          if (shouldSuppressConnection(sourceId, targetId, objectById)) continue;
          propagationTopologyEdges.push({ sourceId, targetId, type: 'PROPAGATES', family: 'other' });
        }
        const propagationDepths = deriveNodeDepths(selectedObject.id, [...rawEdges, ...propagationTopologyEdges]).depths;
        const propagationFeatures = buildPropagationFeatures(
          selectedObject.id,
          res?.propagation || null,
          idToCoords,
          objectById,
          propagationDepths,
          theme,
          supplyCoveredPairs,
        );
        for (const edge of propagationFeatures) {
          const sourceId = String(edge.properties?.sourceId || '').trim();
          const targetId = String(edge.properties?.targetId || '').trim();
          if (sourceId) visibleNodeIds.add(sourceId);
          if (targetId) visibleNodeIds.add(targetId);
        }

        const newDiscovered: GeoObject[] = [];
        for (const fragment of fragments) {
          const fragmentId = String(((fragment as any)?.id ?? (fragment as any)?.ID ?? '')).trim();
          const traceObject = objectById.get(fragmentId);
          if (!traceObject) continue;
          const object = traceObject.coordinates ? fragmentToGeoObject(fragment, serverCoords) : null;
          if (
            object
            && !isGeoLocObject(traceObject)
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
        setEdgesCount(selectedEdges.length);
        const allFeatures = [...supplyPathFeatures, ...graphFeatures, ...propagationFeatures];
        const centerPoint = selectedObject.coordinates
          ? [selectedObject.coordinates.lon, selectedObject.coordinates.lat] as [number, number]
          : null;
        baseCorridorFeatures = buildCorridorOverviewFeatures(allFeatures, objectById, objectByPath, theme, centerPoint)
          .slice(0, MAX_VISIBLE_CORRIDORS * 2);
        const initialDisplayFeatures = decorateCorridorFeatures(baseCorridorFeatures, null);
        const visibleGraphLineCount = initialDisplayFeatures.filter((feature) => feature.geometry?.type === 'LineString').length;
        setTraceSummary(buildTraceSummary(selectedEdges, objectById, selectedObject.id, visibleGraphLineCount, supplyRouteCount, pathArtifacts, res?.propagation || null));

        if (allFeatures.length === 0 || !canAccessMapStyle(map)) {
          setEdgesLoading(false);
          return;
        }

        const endpointEntryMap = new Map<string, { key: string; object: GeoObject; corridorIds: Set<string> }>();
        const rememberEndpointEntry = (key: string, object: GeoObject | null, corridorId: string) => {
          if (!object?.coordinates || !corridorId) return;
          const existing = endpointEntryMap.get(key);
          if (existing) {
            existing.corridorIds.add(corridorId);
            return;
          }
          endpointEntryMap.set(key, { key, object, corridorIds: new Set([corridorId]) });
        };
        for (const feature of allFeatures) {
          if (feature.geometry?.type !== 'LineString') continue;
          const corridorId = featureCorridorId(feature);
          if (!corridorId) continue;
          for (const endpointKey of featureEndpointRefs(feature)) {
            if (endpointKey.startsWith('node:')) {
              const clean = endpointKey.slice(5);
              rememberEndpointEntry(`node:${clean}`, traceObjectToGeoObject(objectById.get(clean)), corridorId);
              continue;
            }
            if (endpointKey.startsWith('path:')) {
              const clean = endpointKey.slice(5);
              rememberEndpointEntry(`path:${clean}`, traceObjectToGeoObject(objectByPath.get(clean)), corridorId);
            }
          }
        }
        baseEndpointEntries = Array.from(endpointEntryMap.values()).map((entry) => ({
          key: entry.key,
          object: entry.object,
          corridorIds: Array.from(entry.corridorIds),
        }));

        await ensureMarkerImages(map, baseEndpointEntries.map((entry) => entry.object), theme);
        baseFeatures = allFeatures;
        map.addSource(SRC, { type: 'geojson', data: { type: 'FeatureCollection', features: initialDisplayFeatures } });
        map.addSource(ENDPOINT_SRC, {
          type: 'geojson',
          data: buildEndpointFeatureCollection([], theme),
        });
        addEdgeLayers(map, theme);
        bringEndpointLayersToFront();
        setTimeout(bringEndpointLayersToFront, 0);
        if (typeof requestAnimationFrame === 'function') {
          requestAnimationFrame(() => bringEndpointLayersToFront());
        }
        applyDisplayState();
        map.on('mousemove', CORRIDOR_GLOW_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_GUIDE_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_ICON_LAYER, handleEdgeHover);
        map.on('mousemove', CORRIDOR_LABEL_LAYER, handleEdgeHover);
        map.on('mouseleave', CORRIDOR_GLOW_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_GUIDE_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_ICON_LAYER, handleEdgeLeave);
        map.on('mouseleave', CORRIDOR_LABEL_LAYER, handleEdgeLeave);
        map.on('click', CORRIDOR_GLOW_LAYER, handleEdgeClick);
        map.on('click', CORRIDOR_GUIDE_LAYER, handleEdgeClick);
        map.on('click', CORRIDOR_LAYER, handleEdgeClick);
        map.on('click', CORRIDOR_ICON_LAYER, handleEdgeClick);
        map.on('click', CORRIDOR_LABEL_LAYER, handleEdgeClick);
        map.on('click', ENDPOINT_POINTS_LAYER, handleEndpointClick);
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
        map.off('mouseleave', CORRIDOR_GLOW_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_GUIDE_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_ICON_LAYER, handleEdgeLeave);
        map.off('mouseleave', CORRIDOR_LABEL_LAYER, handleEdgeLeave);
        map.off('click', CORRIDOR_GLOW_LAYER, handleEdgeClick);
        map.off('click', CORRIDOR_GUIDE_LAYER, handleEdgeClick);
        map.off('click', CORRIDOR_LAYER, handleEdgeClick);
        map.off('click', CORRIDOR_ICON_LAYER, handleEdgeClick);
        map.off('click', CORRIDOR_LABEL_LAYER, handleEdgeClick);
        map.off('click', ENDPOINT_POINTS_LAYER, handleEndpointClick);
      } catch { /* handlers may not be attached */ }
      removeLayers();
    };
  }, [selectedObject?.id, objects, hops, map, isReady, styleEpoch, theme, lensId]);

  return { edgesLoading, edgesCount, visibleEdgesCount, discoveredObjects, traceSummary, relatedPathArtifacts, mutedObjectIds };
}
