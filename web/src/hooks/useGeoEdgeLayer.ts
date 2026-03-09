import { useEffect, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import { api } from '../lib/api';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { getEdgeColor } from '../components/HorizonGraphNodeTypes';
import { geoCodeToShortName, humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'geo-edges';
const GLOW_LAYER = 'geo-edges-glow';
const HOP_3_LAYER = 'geo-edges-hop-3';
const HOP_2_LAYER = 'geo-edges-hop-2';
const HOP_1_LAYER = 'geo-edges-hop-1';
const PROP_GLOW_LAYER = 'geo-propagation-glow';
const PROP_MAIN_LAYER = 'geo-propagation-main';
const MAX_HOPS = 3;

type ThemeMode = 'dark' | 'light';
type EdgeFamily = 'supply' | 'control' | 'influence' | 'location' | 'market' | 'risk' | 'social' | 'other';

export interface GeoEdgeTraceSummary {
  totalEdges: number;
  visibleEdges: number;
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

function buildRenderableEdges(edges: SelectedTraceEdge[], theme: ThemeMode): RenderDependencyEdge[] {
  return edges.flatMap((edge) => {
    if (!edge.sourceCoords || !edge.targetCoords) return [];
    const distanceFactor = edge.hop === 1
      ? 1
      : edge.hop === 2
        ? clamp(1.08 - edge.distanceKm / 3600, 0.58, 0.98)
        : clamp(0.98 - edge.distanceKm / 3000, 0.4, 0.8);
    const baseColor = getEdgeColor(edge.type);
    const color = toSoftHex(baseColor, theme === 'dark'
      ? (edge.hop === 1 ? 0.16 : edge.hop === 2 ? 0.3 : 0.46)
      : (edge.hop === 1 ? 0.08 : edge.hop === 2 ? 0.18 : 0.32));
    const familyWeight = FAMILY_VISUAL_WEIGHT[edge.family];
    const baseOpacity = clamp(
      (edge.hop === 1 ? 0.9 : edge.hop === 2 ? 0.58 : 0.34) * distanceFactor * familyWeight,
      edge.hop === 1 ? 0.62 : edge.hop === 2 ? 0.32 : 0.16,
      0.94,
    );
    const baseWidth = clamp(
      (edge.hop === 1 ? 2.1 : edge.hop === 2 ? 1.45 : 1.08) * (0.9 + distanceFactor * 0.22) * Math.max(0.9, familyWeight),
      edge.hop === 1 ? 1.8 : edge.hop === 2 ? 1.18 : 0.96,
      edge.hop === 1 ? 2.5 : edge.hop === 2 ? 1.75 : 1.2,
    );

    return [{
      ...edge,
      color,
      coordinates: arcPoints(
        edge.sourceCoords.lon,
        edge.sourceCoords.lat,
        edge.targetCoords.lon,
        edge.targetCoords.lat,
        edge.edgeId,
        edge.hop,
      ),
      baseOpacity,
      baseWidth,
      glowOpacity: clamp(baseOpacity * (edge.hop === 1 ? 0.22 : edge.hop === 2 ? 0.18 : 0.14), 0.06, 0.22),
    }];
  });
}

function buildPropagationFeatures(
  centerId: string,
  propagation: { edges?: Array<{ edgeId?: string; sourceFragmentId?: string; targetFragmentId?: string; hopsFromSource?: number }>; nodeIds?: string[] } | null | undefined,
  idToCoords: Map<string, { lon: number; lat: number }>,
  theme: ThemeMode,
): GeoJSON.Feature<GeoJSON.LineString>[] {
  const rawEdges = Array.isArray(propagation?.edges) ? propagation.edges : [];
  const selected: GeoJSON.Feature<GeoJSON.LineString>[] = [];
  const seenPairs = new Set<string>();

  for (const edge of rawEdges) {
    const sourceId = String(edge?.sourceFragmentId || '').trim();
    const targetId = String(edge?.targetFragmentId || '').trim();
    if (!sourceId || !targetId) continue;
    const source = idToCoords.get(sourceId);
    const target = idToCoords.get(targetId);
    if (!source || !target) continue;
    const pairKey = stablePairKey(sourceId, targetId);
    if (seenPairs.has(pairKey)) continue;
    seenPairs.add(pairKey);
    const hop = Math.max(1, Math.min(4, Number(edge?.hopsFromSource) || 1));
    const color = theme === 'dark'
      ? (hop <= 1 ? '#f472b6' : hop === 2 ? '#c084fc' : '#a78bfa')
      : (hop <= 1 ? '#be185d' : hop === 2 ? '#7c3aed' : '#6d28d9');
    const opacity = clamp(hop <= 1 ? 0.62 : hop === 2 ? 0.48 : 0.34, 0.2, 0.7);
    const width = clamp(hop <= 1 ? 1.8 : hop === 2 ? 1.45 : 1.15, 1.0, 2.0);
    selected.push({
      id: `prop:${edge?.edgeId || pairKey}`,
      type: 'Feature',
      geometry: {
        type: 'LineString',
        coordinates: [
          [source.lon, source.lat],
          [target.lon, target.lat],
        ],
      },
      properties: {
        kind: 'propagation',
        hop,
        color,
        baseOpacity: opacity,
        baseWidth: width,
        glowOpacity: clamp(opacity * 0.35, 0.08, 0.26),
        semanticDirection: sourceId === centerId ? 'downstream' : targetId === centerId ? 'upstream' : 'context',
      },
    });
  }

  return selected;
}

function buildTraceSummary(
  edges: SelectedTraceEdge[],
  objectById: Map<string, TraceObjectInfo>,
  centerId: string,
  visibleEdges: number,
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
  const isDark = theme === 'dark';
  map.addLayer({
    id: GLOW_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'graph'],
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
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['min', 0.38, ['+', ['get', 'glowOpacity'], 0.14]],
        ['get', 'glowOpacity'],
      ],
      'line-blur': 1.3,
    },
  });

  map.addLayer({
    id: HOP_3_LAYER,
    type: 'line',
    source: SRC,
    filter: ['all', ['==', ['get', 'kind'], 'graph'], ['==', ['get', 'hop'], 3]],
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
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['min', 0.8, ['+', ['get', 'baseOpacity'], 0.28]],
        ['get', 'baseOpacity'],
      ],
      'line-dasharray': [1.2, 3.4],
    },
  });

  map.addLayer({
    id: HOP_2_LAYER,
    type: 'line',
    source: SRC,
    filter: ['all', ['==', ['get', 'kind'], 'graph'], ['==', ['get', 'hop'], 2]],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': [
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['+', ['get', 'baseWidth'], 0.8],
        ['get', 'baseWidth'],
      ],
      'line-opacity': [
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        ['min', 0.94, ['+', ['get', 'baseOpacity'], 0.24]],
        ['get', 'baseOpacity'],
      ],
      'line-dasharray': [2.1, 2.6],
    },
  });

  map.addLayer({
    id: HOP_1_LAYER,
    type: 'line',
    source: SRC,
    filter: ['all', ['==', ['get', 'kind'], 'graph'], ['==', ['get', 'hop'], 1]],
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
        'case',
        ['boolean', ['feature-state', 'hover'], false],
        1,
        ['get', 'baseOpacity'],
      ],
    },
  });

  if (isDark) {
    map.setPaintProperty(HOP_3_LAYER, 'line-dasharray', [1.1, 3.1]);
  }

  map.addLayer({
    id: PROP_GLOW_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'propagation'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': ['+', ['get', 'baseWidth'], 2],
      'line-opacity': ['get', 'glowOpacity'],
      'line-blur': 1.1,
    },
  });

  map.addLayer({
    id: PROP_MAIN_LAYER,
    type: 'line',
    source: SRC,
    filter: ['==', ['get', 'kind'], 'propagation'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': ['get', 'color'],
      'line-width': ['get', 'baseWidth'],
      'line-opacity': ['get', 'baseOpacity'],
      'line-dasharray': ['literal', [0.8, 1.8]],
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
        if (map.getLayer(PROP_MAIN_LAYER)) map.removeLayer(PROP_MAIN_LAYER);
        if (map.getLayer(PROP_GLOW_LAYER)) map.removeLayer(PROP_GLOW_LAYER);
        if (map.getLayer(HOP_1_LAYER)) map.removeLayer(HOP_1_LAYER);
        if (map.getLayer(HOP_2_LAYER)) map.removeLayer(HOP_2_LAYER);
        if (map.getLayer(HOP_3_LAYER)) map.removeLayer(HOP_3_LAYER);
        if (map.getLayer(GLOW_LAYER)) map.removeLayer(GLOW_LAYER);
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

    if (!selectedObject) {
      removeLayers();
      setDiscoveredObjects([]);
      setEdgesLoading(false);
      setEdgesCount(0);
      setVisibleEdgesCount(0);
      setTraceSummary(null);
      setRelatedPathArtifacts([]);
      return;
    }

    const requestedHops = Math.min(MAX_HOPS, Math.max(1, hops));
    const idToCoords = new Map<string, { lon: number; lat: number }>();
    for (const obj of objects) {
      const lat = obj.coordinates?.lat;
      const lon = obj.coordinates?.lon;
      if (Number.isFinite(lat) && Number.isFinite(lon)) {
        idToCoords.set(obj.id, { lon: lon!, lat: lat! });
      }
    }

    let cancelled = false;
    setEdgesLoading(true);
    setEdgesCount(0);
    setVisibleEdgesCount(0);
    setDiscoveredObjects([]);
    setTraceSummary(null);
    setRelatedPathArtifacts([]);

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
      .then((res) => {
        if (cancelled || !mountedRef.current) return;

        const rawEdges: RawDependencyEdge[] = [];
        const graph = res?.graph ?? {};
        const edges = graph?.edges ?? [];
        const serverCoords = graph?.coordinates ?? {};
        const fragments = graph?.fragments ?? [];
        const pathArtifacts = Array.isArray(res?.supplyChains?.chains) ? (res.supplyChains.chains as MaterializedArtifact[]) : [];
        const objectIds = new Set(objects.map((obj) => obj.id));
        const objectById = new Map<string, TraceObjectInfo>(objects.map((obj) => [obj.id, obj] as const));

        for (const [id, coordinates] of Object.entries(serverCoords)) {
          if (coordinates && Number.isFinite(coordinates.lat) && Number.isFinite(coordinates.lon)) {
            idToCoords.set(id, { lon: coordinates.lon, lat: coordinates.lat });
          }
        }

        for (const edge of edges) {
          const sourceId = String(edge?.sourceFragmentId ?? edge?.sourceFragmentID ?? '').trim();
          const targetId = String(edge?.targetFragmentId ?? edge?.targetFragmentID ?? '').trim();
          const type = normalizeEdgeType(edge?.type);
          if (!sourceId || !targetId || !type) continue;
          rawEdges.push({
            sourceId,
            targetId,
            type,
            family: typeof edge?.family === 'string' ? edge.family as EdgeFamily : undefined,
            priority: typeof edge?.priority === 'number' ? edge.priority : undefined,
            businessMeaning: typeof edge?.businessMeaning === 'string' ? edge.businessMeaning : undefined,
            semanticDirection: typeof edge?.semanticDirection === 'string' ? edge.semanticDirection : undefined,
          });
        }

        const selectedEdges = buildSelectedTraceEdges(selectedObject.id, rawEdges, idToCoords);
        const renderableEdges = buildRenderableEdges(selectedEdges, theme);
        const propagationFeatures = buildPropagationFeatures(selectedObject.id, res?.propagation || null, idToCoords, theme);
        const visibleNodeIds = new Set<string>([selectedObject.id]);
        for (const edge of renderableEdges) {
          visibleNodeIds.add(edge.sourceId);
          visibleNodeIds.add(edge.targetId);
        }
        for (const edge of Array.isArray(res?.propagation?.edges) ? res.propagation.edges : []) {
          if (edge?.sourceFragmentId) visibleNodeIds.add(String(edge.sourceFragmentId));
          if (edge?.targetFragmentId) visibleNodeIds.add(String(edge.targetFragmentId));
        }

        const newDiscovered: GeoObject[] = [];
        for (const fragment of fragments) {
          const traceObject = fragmentToTraceObject(fragment, serverCoords);
          if (!traceObject) continue;
          objectById.set(traceObject.id, traceObject);
          const object = traceObject.coordinates ? fragmentToGeoObject(fragment, serverCoords) : null;
          if (object && visibleNodeIds.has(object.id) && !objectIds.has(object.id)) {
            newDiscovered.push(object);
            objectIds.add(object.id);
          }
        }

        if (cancelled || !mountedRef.current) return;
        setDiscoveredObjects(newDiscovered);
        setRelatedPathArtifacts(pathArtifacts);
        removeLayers();
        setEdgesCount(selectedEdges.length);
        setVisibleEdgesCount(renderableEdges.length);
        setTraceSummary(buildTraceSummary(selectedEdges, objectById, selectedObject.id, renderableEdges.length, pathArtifacts, res?.propagation || null));

        if ((renderableEdges.length === 0 && propagationFeatures.length === 0) || !canAccessMapStyle(map)) {
          setEdgesLoading(false);
          return;
        }

        const graphFeatures: GeoJSON.Feature<GeoJSON.LineString>[] = renderableEdges.map((edge) => ({
          id: edge.edgeId,
          type: 'Feature',
          geometry: { type: 'LineString', coordinates: edge.coordinates },
          properties: {
            kind: 'graph',
            hop: edge.hop,
            type: edge.type,
            family: edge.family,
            color: edge.color,
            baseOpacity: edge.baseOpacity,
            baseWidth: edge.baseWidth,
            glowOpacity: edge.glowOpacity,
          },
        } as GeoJSON.Feature<GeoJSON.LineString>));
        const features: GeoJSON.Feature<GeoJSON.LineString>[] = [...graphFeatures, ...propagationFeatures];

        map.addSource(SRC, { type: 'geojson', data: { type: 'FeatureCollection', features } });
        addEdgeLayers(map, theme);
        map.on('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.on('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.on('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.on('mouseleave', HOP_3_LAYER, handleEdgeLeave);
        map.on('mousemove', PROP_MAIN_LAYER, handleEdgeHover);
        map.on('mouseleave', PROP_MAIN_LAYER, handleEdgeLeave);
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
        }
      });

    return () => {
      cancelled = true;
      clearHoveredEdge();
      try {
        map.off('mousemove', HOP_1_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_2_LAYER, handleEdgeHover);
        map.off('mousemove', HOP_3_LAYER, handleEdgeHover);
        map.off('mouseleave', HOP_1_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_2_LAYER, handleEdgeLeave);
        map.off('mouseleave', HOP_3_LAYER, handleEdgeLeave);
        map.off('mousemove', PROP_MAIN_LAYER, handleEdgeHover);
        map.off('mouseleave', PROP_MAIN_LAYER, handleEdgeLeave);
      } catch { /* handlers may not be attached */ }
      removeLayers();
    };
  }, [selectedObject?.id, objects, hops, map, isReady, styleEpoch, theme, lensId]);

  return { edgesLoading, edgesCount, visibleEdgesCount, discoveredObjects, traceSummary, relatedPathArtifacts };
}
