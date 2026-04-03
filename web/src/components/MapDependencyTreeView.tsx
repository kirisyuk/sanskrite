import React from 'react';
import ReactFlow, {
  Background,
  Controls,
  MarkerType,
  type Edge,
  type Node,
  Position,
} from 'reactflow';
import 'reactflow/dist/style.css';
import type { GeoObject } from '../hooks/useGeoObjects';
import type { GeoEdgeSelectionExportData, GeoEdgeTraceSummary } from '../hooks/useGeoEdgeLayer';
import { mapLayerColor } from '../lib/mapRenderPolicy';
import { readLegRouteOverlays, selectPrimaryLegRouteOverlay } from '../lib/supplyRouteOverlays';
import { humanizeCanonicalPath } from '../lib/uiUtils';
import { HorizonNodeIcon, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';

type ThemeMode = 'dark' | 'light';
type TreeLane =
  | 'selected'
  | 'upstream-direct'
  | 'upstream-context'
  | 'upstream-propagation'
  | 'downstream-direct'
  | 'downstream-context'
  | 'downstream-propagation'
  | 'supply-inbound'
  | 'supply-outbound'
  | 'supply-context';
type TreeNodeKind = 'selected' | 'object' | 'supply-group' | 'route' | 'summary';
type TreeEdgeKind = 'direct' | 'context' | 'propagation' | 'supply' | 'supply-route';
type TreeDirection = 'upstream' | 'downstream' | 'context';
type TreeSide = 'upstream' | 'downstream' | 'center';

type GraphBadgeDraft = {
  label: string;
  kind: TreeEdgeKind;
};

interface MapDependencyTreeViewProps {
  isDark: boolean;
  selectedObject: GeoObject;
  objects: GeoObject[];
  selectionExportData: GeoEdgeSelectionExportData | null;
  traceSummary: GeoEdgeTraceSummary | null;
}

type GraphNodeDraft = {
  id: string;
  label: string;
  subtitle: string;
  lane: TreeLane;
  kind: TreeNodeKind;
  column: number;
  badges: GraphBadgeDraft[];
  canonicalPath?: string;
  objectType?: string;
  facilityKind?: string;
  note?: string;
};

type GraphEdgeDraft = {
  id: string;
  source: string;
  target: string;
  kind: TreeEdgeKind;
  label?: string;
};

const NODE_WIDTH = 192;
const NODE_HEIGHT = 64;
const COLUMN_GAP = 286;
const NODE_GAP = 28;
const COLUMN_CENTER = 720;
const COLUMN_CENTER_Y = 420;
const SUPPLY_PATH_LIMIT = 4;

const LANE_ORDER: TreeLane[] = [
  'selected',
  'supply-inbound',
  'upstream-direct',
  'upstream-context',
  'upstream-propagation',
  'supply-context',
  'supply-outbound',
  'downstream-direct',
  'downstream-context',
  'downstream-propagation',
];

const LANE_LABEL: Record<TreeLane, string> = {
  selected: 'Selected asset',
  'upstream-direct': 'Direct upstream',
  'upstream-context': 'Upstream context',
  'upstream-propagation': 'Indirect upstream',
  'downstream-direct': 'Direct downstream',
  'downstream-context': 'Downstream context',
  'downstream-propagation': 'Indirect downstream',
  'supply-inbound': 'Inbound supply',
  'supply-outbound': 'Outbound supply',
  'supply-context': 'Supply context',
};

const UPSTREAM_LANES: TreeLane[] = [
  'supply-inbound',
  'upstream-direct',
  'upstream-context',
  'upstream-propagation',
];

const DOWNSTREAM_LANES: TreeLane[] = [
  'supply-outbound',
  'downstream-direct',
  'downstream-context',
  'downstream-propagation',
];

const CENTER_LANES: TreeLane[] = ['selected', 'supply-context'];

const LANE_PRIORITY: Record<TreeLane, number> = {
  selected: 100,
  'supply-inbound': 90,
  'supply-outbound': 90,
  'supply-context': 85,
  'upstream-direct': 80,
  'downstream-direct': 80,
  'upstream-context': 72,
  'downstream-context': 72,
  'upstream-propagation': 64,
  'downstream-propagation': 64,
};

type TreeObjectInfo = {
  id?: string;
  canonicalPath?: string;
  label?: string;
  type?: string;
  facilityKind?: string;
  description?: string;
};

type EndpointMarkerInfo = {
  id?: string;
  canonicalPath?: string;
  label?: string;
  type?: string;
  color?: string;
};

const TRANSPORT_VISUAL_TYPES = new Set([
  'FACILITY_SEAPORT',
  'FACILITY_AIRPORT',
  'FACILITY_TERMINAL',
  'FACILITY_PIPELINE',
  'FACILITY_CABLE',
]);

function compactText(value: string, maxLength: number): string {
  const normalized = String(value || '').replace(/\s+/g, ' ').trim();
  if (!normalized) return '';
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, Math.max(0, maxLength - 1)).trimEnd()}…`;
}

function objectLabel(object: Partial<GeoObject> | null | undefined): string {
  return String(object?.label || '').trim()
    || humanizeCanonicalPath(String(object?.canonicalPath || '').trim())
    || String(object?.canonicalPath || '').trim()
    || String(object?.id || '').trim()
    || 'Unknown node';
}

function objectSubtitle(object: Partial<GeoObject> | null | undefined): string {
  const type = String(object?.type || '').trim().replace(/_/g, ' ').toLowerCase();
  const canonicalPath = String(object?.canonicalPath || '').trim();
  if (type && canonicalPath) return `${type} · ${compactText(canonicalPath, 38)}`;
  return type || compactText(canonicalPath, 44) || 'Mapped node';
}

function edgeLabel(value?: string): string {
  return compactText(
    String(value || '')
      .trim()
      .replace(/_/g, ' ')
      .replace(/\s+/g, ' '),
    22,
  );
}

function laneForSupplyDirection(direction?: 'inbound' | 'outbound' | 'context'): TreeLane {
  if (direction === 'inbound') return 'supply-inbound';
  if (direction === 'outbound') return 'supply-outbound';
  return 'supply-context';
}

function treeDirectionForSupply(direction?: 'inbound' | 'outbound' | 'context'): TreeDirection {
  if (direction === 'inbound') return 'upstream';
  if (direction === 'outbound') return 'downstream';
  return 'context';
}

function semanticColumn(direction?: string, fallbackMagnitude = 1): number {
  const normalized = String(direction || '').trim().toLowerCase();
  if (normalized === 'upstream' || normalized === 'inbound') return -Math.max(1, Math.abs(fallbackMagnitude));
  if (normalized === 'downstream' || normalized === 'outbound') return Math.max(1, Math.abs(fallbackMagnitude));
  return Math.max(1, Math.abs(fallbackMagnitude));
}

function semanticLane(direction?: string, kind: 'direct' | 'context' | 'propagation' = 'direct'): TreeLane {
  const normalized = String(direction || '').trim().toLowerCase();
  const upstream = normalized === 'upstream' || normalized === 'inbound';
  const downstream = normalized === 'downstream' || normalized === 'outbound';
  if (kind === 'context') return upstream ? 'upstream-context' : 'downstream-context';
  if (kind === 'propagation') return upstream ? 'upstream-propagation' : downstream ? 'downstream-propagation' : 'downstream-propagation';
  return upstream ? 'upstream-direct' : downstream ? 'downstream-direct' : 'downstream-direct';
}

function relationBadgeLabel(direction: string, kind: 'direct' | 'context' | 'propagation' | 'supply'): string {
  const normalized = String(direction || '').trim().toLowerCase();
  const prefix = normalized === 'upstream' || normalized === 'inbound'
    ? kind === 'supply'
      ? 'INBOUND'
      : kind === 'context'
        ? 'UPSTREAM'
        : 'DIRECT UPSTREAM'
    : normalized === 'downstream' || normalized === 'outbound'
      ? kind === 'supply'
        ? 'OUTBOUND'
        : kind === 'context'
          ? 'DOWNSTREAM'
          : 'DIRECT DOWNSTREAM'
      : 'CONTEXT';
  if (kind === 'propagation') return `${normalized === 'upstream' ? 'INDIRECT UPSTREAM' : 'INDIRECT DOWNSTREAM'}`;
  if (kind === 'context') return `${prefix} CONTEXT`;
  if (kind === 'supply') return `${prefix} SUPPLY`;
  return prefix;
}

function mergeBadges(current: GraphBadgeDraft[], next: GraphBadgeDraft[]): GraphBadgeDraft[] {
  const merged = [...current];
  for (const badge of next) {
    if (!merged.some((item) => item.label === badge.label && item.kind === badge.kind)) {
      merged.push(badge);
    }
  }
  return merged;
}

function preferredLane(current: TreeLane, next: TreeLane): TreeLane {
  return LANE_PRIORITY[next] > LANE_PRIORITY[current] ? next : current;
}

function laneSide(lane: TreeLane): TreeSide {
  if (lane === 'selected' || lane === 'supply-context') return 'center';
  if (lane === 'supply-inbound' || lane.startsWith('upstream-')) return 'upstream';
  return 'downstream';
}

function laneForSide(lane: TreeLane, side: Exclude<TreeSide, 'center'>): TreeLane {
  if (lane === 'selected' || lane === 'supply-context') return lane;
  if (lane === 'supply-inbound' || lane === 'supply-outbound') {
    return side === 'upstream' ? 'supply-inbound' : 'supply-outbound';
  }
  if (lane === 'upstream-context' || lane === 'downstream-context') {
    return side === 'upstream' ? 'upstream-context' : 'downstream-context';
  }
  if (lane === 'upstream-propagation' || lane === 'downstream-propagation') {
    return side === 'upstream' ? 'upstream-propagation' : 'downstream-propagation';
  }
  return side === 'upstream' ? 'upstream-direct' : 'downstream-direct';
}

function normalizeTreeObject(value: unknown): TreeObjectInfo | null {
  if (!value || typeof value !== 'object') return null;
  const raw = value as Record<string, unknown>;
  const id = String(raw.id || raw.ID || '').trim();
  const canonicalPath = String(raw.canonicalPath || raw.CanonicalPath || '').trim();
  if (!id && !canonicalPath) return null;
  const identifiers = raw.identifiers && typeof raw.identifiers === 'object' ? raw.identifiers as Record<string, unknown> : null;
  return {
    id: id || undefined,
    canonicalPath: canonicalPath || undefined,
    label: String(raw.label || raw.Label || (identifiers?.shortName ?? identifiers?.legalName) || '').trim() || undefined,
    type: String(raw.type || raw.Type || '').trim() || undefined,
    facilityKind: String(raw.facilityKind || raw.FacilityKind || identifiers?.facilityKind || '').trim() || undefined,
    description: String(raw.description || raw.Description || '').trim() || undefined,
  };
}

function featureLines(selectionExportData: GeoEdgeSelectionExportData | null): Array<Record<string, unknown>> {
  const features = selectionExportData?.renderedEdgeFeatureCollection?.features || [];
  return features
    .filter((feature) => Boolean(feature) && (feature as { geometry?: { type?: string } }).geometry?.type === 'LineString')
    .map((feature) => ((feature as { properties?: Record<string, unknown> }).properties || {}) as Record<string, unknown>);
}

function endpointMarkers(selectionExportData: GeoEdgeSelectionExportData | null): EndpointMarkerInfo[] {
  const features = selectionExportData?.renderedEndpointFeatureCollection?.features || [];
  return features
    .map((feature) => ((feature as { properties?: Record<string, unknown> }).properties || {}) as Record<string, unknown>)
    .filter((properties) => String(properties.kind || '').trim() === 'endpoint-marker')
    .map((properties) => ({
      id: String(properties.id || '').trim() || undefined,
      canonicalPath: String(properties.canonicalPath || '').trim() || undefined,
      label: String(properties.label || '').trim() || undefined,
      type: String(properties.type || '').trim() || undefined,
      color: String(properties.color || '').trim() || undefined,
    }));
}

function detectTransportVisualType(
  visualType: string | undefined,
  canonicalPath?: string,
  label?: string,
  facilityKind?: string,
): string | null {
  const normalizedVisualType = String(visualType || '').trim().toUpperCase();
  if (TRANSPORT_VISUAL_TYPES.has(normalizedVisualType)) return normalizedVisualType;

  const raw = `${String(canonicalPath || '')} ${String(label || '')} ${String(facilityKind || '')}`
    .toLowerCase()
    .replace(/[_/]+/g, ' ');
  const has = (pattern: RegExp) => pattern.test(raw);

  if (has(/\b(airport|air\s*hub|air\s*cargo)\b/)) return 'FACILITY_AIRPORT';
  if (has(/\b(port|harbor|harbour|seaport|anchorage)\b/)) return 'FACILITY_SEAPORT';
  if (has(/\b(canal|terminal|intermodal|rail\s*hub|freight\s*hub|freight\s*terminal|logistics\s*hub|customs|gateway)\b/)) {
    return 'FACILITY_TERMINAL';
  }
  if (has(/\b(pipeline|pipe)\b/)) return 'FACILITY_PIPELINE';
  if (has(/\b(cable|fiber|optic)\b/)) return 'FACILITY_CABLE';
  return null;
}

function buildTreeGraph(
  selectedObject: GeoObject,
  objects: GeoObject[],
  selectionExportData: GeoEdgeSelectionExportData | null,
  traceSummary: GeoEdgeTraceSummary | null,
  theme: ThemeMode,
): { nodes: Node[]; edges: Edge[] } {
  const selectedId = String(selectedObject.id || '').trim();
  const selectedCanonicalPath = String(selectedObject.canonicalPath || '').trim();
  const objectById = new Map<string, TreeObjectInfo>();
  const objectByCanonicalPath = new Map<string, TreeObjectInfo>();
  const idByCanonicalPath = new Map<string, string>();

  const registerObjectInfo = (value: unknown) => {
    const normalized = normalizeTreeObject(value);
    if (!normalized) return;
    if (normalized.id) {
      const current = objectById.get(normalized.id) || {};
      const merged = {
        ...current,
        ...normalized,
        id: current.id || normalized.id,
        canonicalPath: current.canonicalPath || normalized.canonicalPath,
        label: current.label || normalized.label,
        type: current.type || normalized.type,
        facilityKind: current.facilityKind || normalized.facilityKind,
        description: current.description || normalized.description,
      };
      objectById.set(normalized.id, merged);
      if (merged.canonicalPath && !idByCanonicalPath.has(merged.canonicalPath)) {
        idByCanonicalPath.set(merged.canonicalPath, normalized.id);
      }
    }
    if (normalized.canonicalPath) {
      const current = objectByCanonicalPath.get(normalized.canonicalPath) || {};
      const merged = {
        ...current,
        ...normalized,
        id: current.id || normalized.id,
        canonicalPath: normalized.canonicalPath,
        label: current.label || normalized.label,
        type: current.type || normalized.type,
        facilityKind: current.facilityKind || normalized.facilityKind,
        description: current.description || normalized.description,
      };
      objectByCanonicalPath.set(normalized.canonicalPath, merged);
      if (merged.id && !objectById.has(merged.id)) objectById.set(merged.id, merged);
      if (merged.id && !idByCanonicalPath.has(normalized.canonicalPath)) idByCanonicalPath.set(normalized.canonicalPath, merged.id);
    }
  };

  for (const object of objects) registerObjectInfo(object);
  for (const object of selectionExportData?.discoveredObjects || []) registerObjectInfo(object);
  registerObjectInfo(selectedObject);
  for (const fragment of Array.isArray((selectionExportData?.trace as any)?.graph?.fragments) ? (selectionExportData?.trace as any).graph.fragments : []) {
    registerObjectInfo(fragment);
  }
  for (const marker of endpointMarkers(selectionExportData)) registerObjectInfo(marker);

  const draftNodes = new Map<string, GraphNodeDraft>();
  const draftEdges = new Map<string, GraphEdgeDraft>();

  const resolveTreeObject = (input: TreeObjectInfo): TreeObjectInfo => {
    const canonicalPath = String(input.canonicalPath || '').trim();
    const objectId = String(input.id || '').trim() || (canonicalPath ? String(idByCanonicalPath.get(canonicalPath) || '').trim() : '');
    const byId = objectId ? objectById.get(objectId) : undefined;
    const byPath = canonicalPath ? objectByCanonicalPath.get(canonicalPath) : undefined;
    return {
      id: objectId || byId?.id || byPath?.id,
      canonicalPath: canonicalPath || byId?.canonicalPath || byPath?.canonicalPath,
      label: input.label || byId?.label || byPath?.label,
      type: input.type || byId?.type || byPath?.type,
      facilityKind: input.facilityKind || byId?.facilityKind || byPath?.facilityKind,
      description: input.description || byId?.description || byPath?.description,
    };
  };

  const treeNodeIdForObject = (input: TreeObjectInfo): string => {
    const resolved = resolveTreeObject(input);
    if (resolved.id) return `object:${resolved.id}`;
    if (resolved.canonicalPath) return `path:${resolved.canonicalPath}`;
    return `anon:${compactText(resolved.label || 'unknown-node', 32).toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  };

  const scopedRouteNodeIdForObject = (scope: string, input: TreeObjectInfo): string => {
    const resolved = resolveTreeObject(input);
    const normalizedScope = compactText(scope || 'route-scope', 72).toLowerCase().replace(/[^a-z0-9:/_-]+/g, '-');
    if (resolved.id) return `route:${normalizedScope}:object:${resolved.id}`;
    if (resolved.canonicalPath) return `route:${normalizedScope}:path:${resolved.canonicalPath}`;
    return `route:${normalizedScope}:anon:${compactText(resolved.label || 'unknown-node', 32).toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  };

  const ensureNode = (next: GraphNodeDraft) => {
    const current = draftNodes.get(next.id);
    if (!current) {
      draftNodes.set(next.id, next);
      return;
    }
    draftNodes.set(next.id, {
      ...current,
      ...next,
      column: current.kind === 'selected'
        ? current.column
        : current.column === 0
          ? next.column
          : next.column === 0
            ? current.column
            : Math.abs(next.column) > Math.abs(current.column)
              ? next.column
              : current.column,
      badges: mergeBadges(current.badges, next.badges),
      subtitle: current.subtitle || next.subtitle,
      note: current.note || next.note,
      canonicalPath: current.canonicalPath || next.canonicalPath,
      objectType: current.objectType || next.objectType,
      facilityKind: current.facilityKind || next.facilityKind,
      lane: current.kind === 'selected' ? current.lane : preferredLane(current.lane, next.lane),
      kind: current.kind === 'selected' ? current.kind : next.kind,
    });
  };

  const ensureEdge = (next: GraphEdgeDraft) => {
    if (!draftEdges.has(next.id)) draftEdges.set(next.id, next);
  };

  const upsertObjectNode = (
    input: TreeObjectInfo,
    lane: TreeLane,
    column: number,
    badge: GraphBadgeDraft,
    note?: string,
  ): string => {
    const resolved = resolveTreeObject(input);
    const nodeId = treeNodeIdForObject(resolved);
    ensureNode({
      id: nodeId,
      label: objectLabel(resolved),
      subtitle: '',
      lane,
      kind: 'object',
      column,
      badges: [badge],
      canonicalPath: resolved.canonicalPath,
      objectType: resolved.type,
      facilityKind: resolved.facilityKind,
      note: note || resolved.description,
    });
    return nodeId;
  };

  const upsertSupplyRouteObjectNode = (
    scope: string,
    input: TreeObjectInfo,
    lane: TreeLane,
    column: number,
    badge: GraphBadgeDraft,
    note?: string,
  ): string => {
    const resolved = resolveTreeObject(input);
    const nodeId = scopedRouteNodeIdForObject(scope, resolved);
    ensureNode({
      id: nodeId,
      label: objectLabel(resolved),
      subtitle: '',
      lane,
      kind: 'object',
      column,
      badges: [badge],
      canonicalPath: resolved.canonicalPath,
      objectType: resolved.type,
      facilityKind: resolved.facilityKind,
      note: note || resolved.description,
    });
    return nodeId;
  };

  const addEdge = (source: string, target: string, kind: TreeEdgeKind, label?: string) => {
    if (!source || !target || source === target) return;
    ensureEdge({
      id: `${kind}:${source}:${target}:${label || ''}`,
      source,
      target,
      kind,
      label,
    });
  };

  const seedNodeId = `object:${selectedId}`;
  ensureNode({
    id: seedNodeId,
    label: objectLabel(selectedObject),
    subtitle: objectSubtitle(selectedObject),
    lane: 'selected',
    kind: 'selected',
    column: 0,
    badges: [{ label: 'SELECTED ASSET', kind: 'direct' }],
    canonicalPath: String(selectedObject.canonicalPath || '').trim(),
    objectType: selectedObject.type,
    facilityKind: selectedObject.facilityKind,
    note: 'Selected on map',
  });
  const renderedLineProps = featureLines(selectionExportData);
  const relationMeta = new Map<string, { label?: string; note?: string }>();
  for (const relation of selectionExportData?.directRelations || []) {
    const direction = String(relation.semanticDirection || '').trim();
    if (relation.edgeId) {
      relationMeta.set(`edge:${relation.edgeId}`, {
        label: relation.businessMeaning || relation.type,
        note: relation.whyRelevant,
      });
    }
    relationMeta.set(`direct:${relation.relatedFragmentId}:${relation.type}:${direction}`, {
      label: relation.businessMeaning || relation.type,
      note: relation.whyRelevant,
    });
  }
  for (const relation of selectionExportData?.contextRelations || []) {
    const direction = String(relation.semanticDirection || '').trim();
    if (relation.edgeId) {
      relationMeta.set(`edge:${relation.edgeId}`, {
        label: relation.businessMeaning || relation.type,
        note: relation.whyRelevant,
      });
    }
    relationMeta.set(`context:${relation.relatedFragmentId}:${relation.type}:${direction}`, {
      label: relation.businessMeaning || relation.type,
      note: relation.whyRelevant,
    });
  }

  for (const properties of renderedLineProps) {
    const channel = String(properties.channel || '').trim();
    if (channel !== 'relation_direct' && channel !== 'relation_context') continue;
    const sourceId = String(properties.sourceId || '').trim();
    const targetId = String(properties.targetId || '').trim();
    if (!sourceId || !targetId) continue;
    const relationKind = channel === 'relation_direct' ? 'direct' : 'context';
    const relatedId = sourceId === selectedId ? targetId : sourceId;
    if (!relatedId || relatedId === selectedId) continue;
    const direction = String(properties.semanticDirection || (targetId === selectedId ? 'upstream' : 'downstream')).trim().toLowerCase();
    const hop = Number(properties.hop);
    const column = semanticColumn(direction, Number.isFinite(hop) ? hop : relationKind === 'context' ? 2 : 1);
    const relatedInfo = resolveTreeObject({ id: relatedId });
    const relationType = String(properties.relationType || '').trim();
    const narrativeKey = String(properties.narrativeKey || '').trim();
    const meta = relationMeta.get(`narrative:${narrativeKey}`)
      || relationMeta.get(`edge:${narrativeKey}`)
      || relationMeta.get(`${relationKind}:${relatedId}:${relationType}:${direction}`);
    const nodeId = upsertObjectNode(
      relatedInfo,
      semanticLane(direction, relationKind),
      column,
      { label: relationBadgeLabel(direction, relationKind), kind: relationKind },
      meta?.note,
    );
    if (direction === 'upstream') addEdge(nodeId, seedNodeId, relationKind, edgeLabel(meta?.label || relationType));
    else addEdge(seedNodeId, nodeId, relationKind, edgeLabel(meta?.label || relationType));
  }

  const artifactById = new Map<string, Record<string, unknown>>();
  for (const artifact of selectionExportData?.relatedPathArtifacts || []) {
    const artifactId = String((artifact as any)?.id || '').trim();
    if (artifactId) artifactById.set(artifactId, artifact as unknown as Record<string, unknown>);
  }

  const artifactCounterpart = (
    artifact: Record<string, unknown> | undefined,
    direction: 'inbound' | 'outbound' | 'context',
  ): TreeObjectInfo | null => {
    if (!artifact) return null;
    const traceEndpoints = artifact.traceEndpoints && typeof artifact.traceEndpoints === 'object'
      ? artifact.traceEndpoints as Record<string, unknown>
      : null;
    const pick = direction === 'inbound'
      ? (traceEndpoints?.upstream || traceEndpoints?.supplier || traceEndpoints?.customer)
      : direction === 'outbound'
        ? (traceEndpoints?.downstream || traceEndpoints?.customer || traceEndpoints?.supplier)
        : (traceEndpoints?.customer || traceEndpoints?.supplier || traceEndpoints?.upstream || traceEndpoints?.downstream);
    const normalized = normalizeTreeObject(pick);
    if (!normalized) return null;
    if (String(normalized.canonicalPath || '').trim() === selectedCanonicalPath) {
      const fallback = direction === 'inbound'
        ? normalizeTreeObject(traceEndpoints?.supplier || traceEndpoints?.upstream)
        : normalizeTreeObject(traceEndpoints?.customer || traceEndpoints?.downstream);
      if (fallback && String(fallback.canonicalPath || '').trim() !== selectedCanonicalPath) return fallback;
    }
    return normalized;
  };

  const artifactRouteRefs = (
    artifact: Record<string, unknown> | undefined,
    direction: 'inbound' | 'outbound' | 'context',
    productRef?: string,
    counterpartCanonicalPath?: string,
  ): string[] => {
    if (!artifact || direction === 'context') return [];
    const payload = artifact.payload && typeof artifact.payload === 'object'
      ? artifact.payload as Record<string, unknown>
      : null;
    const legs = Array.isArray(payload?.legs) ? payload.legs as Array<Record<string, unknown>> : [];
    const normalizedProductRef = String(productRef || '').trim();
    const normalizedCounterpart = String(counterpartCanonicalPath || '').trim();

    let bestRefs: string[] = [];
    let bestScore = -1;
    for (const leg of legs) {
      const legProductRef = String(leg.ProductRef || leg.productRef || '').trim();
      const supplierRef = String(leg.SupplierRef || leg.supplierRef || '').trim();
      const customerRef = String(leg.CustomerRef || leg.customerRef || '').trim();
      const overlays = readLegRouteOverlays(leg);
      const primaryOverlay = selectPrimaryLegRouteOverlay(overlays);
      const orderedRefs = primaryOverlay?.orderedRefs?.length
        ? primaryOverlay.orderedRefs
        : [
            supplierRef,
            ...(primaryOverlay?.transportViaRefs || []),
            customerRef,
          ].filter(Boolean);
      if (orderedRefs.length < 2) continue;

      let score = 0;
      if (normalizedProductRef && legProductRef === normalizedProductRef) score += 6;
      if (direction === 'inbound') {
        if (customerRef === selectedCanonicalPath) score += 8;
        if (normalizedCounterpart && supplierRef === normalizedCounterpart) score += 5;
      } else {
        if (supplierRef === selectedCanonicalPath) score += 8;
        if (normalizedCounterpart && customerRef === normalizedCounterpart) score += 5;
      }
      if (primaryOverlay?.orderedRefs?.length) score += 2;
      if (score > bestScore) {
        bestScore = score;
        bestRefs = orderedRefs;
      }
    }

    if (!bestRefs.length) {
      const traceEndpoints = artifact.traceEndpoints && typeof artifact.traceEndpoints === 'object'
        ? artifact.traceEndpoints as Record<string, unknown>
        : null;
      const start = direction === 'inbound'
        ? normalizeTreeObject(traceEndpoints?.upstream || traceEndpoints?.supplier)
        : normalizeTreeObject(traceEndpoints?.downstream || traceEndpoints?.customer);
      if (start?.canonicalPath) return [start.canonicalPath];
      return [];
    }

    const deduped: string[] = [];
    for (const ref of bestRefs) {
      const normalized = String(ref || '').trim();
      if (!normalized || deduped[deduped.length - 1] === normalized) continue;
      deduped.push(normalized);
    }
    return deduped;
  };

  for (const group of traceSummary?.supplyGroups || []) {
    const groupId = `supply-group:${String(group.id || group.narrativeKey || group.productRef || group.productLabel).trim()}`;
    const routeScope = `supply:${group.direction || 'context'}:${String(group.productRef || group.id || group.productLabel || 'group').trim()}`;
    const directionLabel = group.direction === 'inbound' ? 'Inbound' : group.direction === 'outbound' ? 'Outbound' : 'Context';
    const groupColumn = group.direction === 'inbound' ? -1 : group.direction === 'outbound' ? 1 : 1;
    ensureNode({
      id: groupId,
      label: compactText(group.productLabel || 'Supply group', 42),
      subtitle: `${directionLabel} · ${group.routeCount} routes`,
      lane: laneForSupplyDirection(group.direction),
      kind: 'supply-group',
      column: groupColumn,
      badges: [{ label: relationBadgeLabel(treeDirectionForSupply(group.direction), 'supply'), kind: 'supply' }],
      canonicalPath: group.productRef,
      objectType: 'DEPENDENCY',
      note: group.summary,
    });
    if (group.direction === 'inbound') addEdge(groupId, seedNodeId, 'supply', directionLabel.toLowerCase());
    else addEdge(seedNodeId, groupId, 'supply', directionLabel.toLowerCase());

    const visiblePaths = group.paths.slice(0, SUPPLY_PATH_LIMIT);
    visiblePaths.forEach((path, index) => {
      const artifact = artifactById.get(String(path.id || '').trim());
      const counterpart = artifactCounterpart(artifact, group.direction);
      const routeBits = [
        Number.isFinite(Number(path.stressScore)) && Number(path.stressScore) > 0
          ? `stress ${Number(path.stressScore).toFixed(2)}`
          : '',
        edgeLabel(path.role),
      ].filter(Boolean);
      const routeRefs = artifactRouteRefs(
        artifact,
        group.direction,
        group.productRef,
        String(counterpart?.canonicalPath || '').trim(),
      );
      if (counterpart) {
        const legRefs = routeRefs
          .filter((ref) => ref !== selectedCanonicalPath)
          .filter((ref) => ref !== String(counterpart.canonicalPath || '').trim());
        const outwardRefs = (group.direction === 'inbound'
          ? [String(counterpart.canonicalPath || '').trim(), ...legRefs]
          : [...legRefs, String(counterpart.canonicalPath || '').trim()])
          .filter(Boolean);
        const columnStart = group.direction === 'inbound'
          ? -(outwardRefs.length + 1)
          : 2;
        if (group.direction === 'inbound') {
          let previousNodeId = '';
          outwardRefs.forEach((ref, refIndex) => {
            const nodeId = upsertSupplyRouteObjectNode(
              routeScope,
              { canonicalPath: ref },
              laneForSupplyDirection(group.direction),
              columnStart + refIndex,
              { label: relationBadgeLabel(treeDirectionForSupply(group.direction), 'supply'), kind: 'supply-route' },
              path.reason,
            );
            if (previousNodeId) addEdge(previousNodeId, nodeId, 'supply-route');
            previousNodeId = nodeId;
          });
          if (previousNodeId) addEdge(previousNodeId, groupId, 'supply-route', routeBits.join(' · ') || edgeLabel(path.title));
          return;
        }

        let previousNodeId = groupId;
        outwardRefs.forEach((ref, refIndex) => {
          const nodeId = upsertSupplyRouteObjectNode(
            routeScope,
            { canonicalPath: ref },
            laneForSupplyDirection(group.direction),
            columnStart + refIndex,
            { label: relationBadgeLabel(treeDirectionForSupply(group.direction), 'supply'), kind: 'supply-route' },
            path.reason,
          );
          addEdge(previousNodeId, nodeId, 'supply-route', refIndex === outwardRefs.length - 1 ? routeBits.join(' · ') || edgeLabel(path.title) : undefined);
          previousNodeId = nodeId;
        });
        return;
      }

      const routeColumn = group.direction === 'inbound' ? -2 : group.direction === 'outbound' ? 2 : 2;
      const routeNodeId = `supply-route:${groupId}:${String(path.id || index).trim()}`;
      const routeLabel = path.counterpartLabel || path.destinationLabel || path.title || 'Supply route';
      ensureNode({
        id: routeNodeId,
        label: compactText(routeLabel, 42),
        subtitle: routeBits.join(' · ') || edgeLabel(path.reason) || 'Mapped route',
        lane: laneForSupplyDirection(group.direction),
        kind: 'route',
        column: routeColumn,
        badges: [{ label: relationBadgeLabel(treeDirectionForSupply(group.direction), 'supply'), kind: 'supply-route' }],
        objectType: 'SUPPLIER',
        note: path.reason,
      });
      if (group.direction === 'inbound') addEdge(routeNodeId, groupId, 'supply-route', routeBits.join(' · ') || edgeLabel(path.title));
      else addEdge(groupId, routeNodeId, 'supply-route', routeBits.join(' · ') || edgeLabel(path.title));
    });

    if (group.paths.length > SUPPLY_PATH_LIMIT) {
      const summaryNodeId = `supply-summary:${groupId}`;
      ensureNode({
        id: summaryNodeId,
        label: `+${group.paths.length - SUPPLY_PATH_LIMIT} more routes`,
        subtitle: 'Expand on map for the full route set',
        lane: laneForSupplyDirection(group.direction),
        kind: 'summary',
        column: group.direction === 'inbound' ? -2 : 2,
        badges: [],
        objectType: 'DEPENDENCY',
      });
      if (group.direction === 'inbound') addEdge(summaryNodeId, groupId, 'supply-route');
      else addEdge(groupId, summaryNodeId, 'supply-route');
    }
  }

  const propagationEdges = Array.isArray((selectionExportData?.trace as any)?.propagation?.edges)
    ? ((selectionExportData?.trace as any).propagation.edges as Array<Record<string, unknown>>)
    : [];
  const forward = new Map<string, Set<string>>();
  const backward = new Map<string, Set<string>>();
  for (const edge of propagationEdges) {
    const sourceId = String(edge.sourceFragmentId || '').trim();
    const targetId = String(edge.targetFragmentId || '').trim();
    if (!sourceId || !targetId) continue;
    if (!forward.has(sourceId)) forward.set(sourceId, new Set());
    if (!backward.has(targetId)) backward.set(targetId, new Set());
    forward.get(sourceId)!.add(targetId);
    backward.get(targetId)!.add(sourceId);
  }

  const bfs = (adjacency: Map<string, Set<string>>) => {
    const depth = new Map<string, number>();
    const parent = new Map<string, string>();
    const queue: string[] = [selectedId];
    depth.set(selectedId, 0);
    while (queue.length > 0) {
      const current = queue.shift()!;
      const currentDepth = depth.get(current) || 0;
      for (const next of adjacency.get(current) || []) {
        if (depth.has(next)) continue;
        depth.set(next, currentDepth + 1);
        parent.set(next, current);
        queue.push(next);
      }
    }
    return { depth, parent };
  };

  const downstreamTree = bfs(forward);
  const upstreamTree = bfs(backward);

  const propagationVisible = new Map<string, { object: TreeObjectInfo; hop: number }>();
  const registerVisiblePropagation = (input: TreeObjectInfo, hop: number) => {
    const resolved = resolveTreeObject(input);
    const nodeId = treeNodeIdForObject(resolved);
    const current = propagationVisible.get(nodeId);
    if (!current || hop < current.hop) propagationVisible.set(nodeId, { object: resolved, hop });
  };

  for (const properties of renderedLineProps) {
    if (String(properties.channel || '').trim() !== 'propagation') continue;
    const hop = Number(properties.hop);
    const renderedHop = Number.isFinite(hop) ? Math.max(1, hop) : 1;
    const endpointNodeIds = Array.isArray(properties.endpointNodeIds) ? properties.endpointNodeIds : [];
    const endpointPathRefs = Array.isArray(properties.endpointPathRefs) ? properties.endpointPathRefs : [];
    for (const value of endpointNodeIds) {
      const endpointId = String(value || '').trim();
      if (!endpointId || endpointId === selectedId) continue;
      registerVisiblePropagation({ id: endpointId }, renderedHop);
    }
    for (const value of endpointPathRefs) {
      const canonicalPath = String(value || '').trim();
      if (!canonicalPath || canonicalPath === selectedCanonicalPath) continue;
      registerVisiblePropagation({ canonicalPath }, renderedHop);
    }
  }

  const visiblePropagationEntries = Array.from(propagationVisible.values())
    .map((entry) => {
      const resolved = resolveTreeObject(entry.object);
      const objectId = String(resolved.id || '').trim() || (resolved.canonicalPath ? String(idByCanonicalPath.get(resolved.canonicalPath) || '').trim() : '');
      const downstreamDepth = objectId ? downstreamTree.depth.get(objectId) : undefined;
      const upstreamDepth = objectId ? upstreamTree.depth.get(objectId) : undefined;
      const direction: TreeDirection = downstreamDepth != null && (upstreamDepth == null || downstreamDepth <= upstreamDepth)
        ? 'downstream'
        : 'upstream';
      const structuralDepth = direction === 'downstream'
        ? downstreamDepth ?? entry.hop
        : upstreamDepth ?? entry.hop;
      return {
        object: resolved,
        nodeId: treeNodeIdForObject(resolved),
        objectId,
        direction,
        renderHop: entry.hop,
        structuralDepth: Math.max(1, structuralDepth || 1),
      };
    })
    .filter((entry) => entry.nodeId !== seedNodeId)
    .sort((left, right) => left.structuralDepth - right.structuralDepth || left.object.label?.localeCompare(right.object.label || '') || 0);

  for (const entry of visiblePropagationEntries) {
    const directionString = entry.direction === 'upstream' ? 'upstream' : 'downstream';
    const nodeId = upsertObjectNode(
      entry.object,
      semanticLane(directionString, 'propagation'),
      semanticColumn(directionString, entry.renderHop || entry.structuralDepth),
      { label: relationBadgeLabel(directionString, 'propagation'), kind: 'propagation' },
    );
    if (!entry.objectId) {
      if (entry.direction === 'upstream') addEdge(nodeId, seedNodeId, 'propagation', 'spread');
      else addEdge(seedNodeId, nodeId, 'propagation', 'spread');
      continue;
    }

    if (entry.direction === 'downstream') {
      let sourceNodeId = seedNodeId;
      let parentId = downstreamTree.parent.get(entry.objectId);
      while (parentId) {
        if (parentId === selectedId) {
          sourceNodeId = seedNodeId;
          break;
        }
        const candidateNodeId = treeNodeIdForObject({ id: parentId });
        if (draftNodes.has(candidateNodeId)) {
          sourceNodeId = candidateNodeId;
          break;
        }
        parentId = downstreamTree.parent.get(parentId);
      }
      addEdge(sourceNodeId, nodeId, 'propagation', 'spread');
      continue;
    }

    let targetNodeId = seedNodeId;
    let parentId = upstreamTree.parent.get(entry.objectId);
    while (parentId) {
      if (parentId === selectedId) {
        targetNodeId = seedNodeId;
        break;
      }
      const candidateNodeId = treeNodeIdForObject({ id: parentId });
      if (draftNodes.has(candidateNodeId)) {
        targetNodeId = candidateNodeId;
        break;
      }
      parentId = upstreamTree.parent.get(parentId);
    }
    addEdge(nodeId, targetNodeId, 'propagation', 'spread');
  }

  const nodeIds = Array.from(draftNodes.keys());
  const graphOutgoing = new Map<string, string[]>();
  const graphIncoming = new Map<string, string[]>();
  for (const nodeId of nodeIds) {
    graphOutgoing.set(nodeId, []);
    graphIncoming.set(nodeId, []);
  }
  for (const edge of draftEdges.values()) {
    if (edge.kind === 'propagation') continue;
    if (!graphOutgoing.has(edge.source)) graphOutgoing.set(edge.source, []);
    if (!graphIncoming.has(edge.target)) graphIncoming.set(edge.target, []);
    graphOutgoing.get(edge.source)!.push(edge.target);
    graphIncoming.get(edge.target)!.push(edge.source);
  }

  let tarjanIndex = 0;
  const tarjanStack: string[] = [];
  const tarjanIndices = new Map<string, number>();
  const tarjanLowlinks = new Map<string, number>();
  const tarjanOnStack = new Set<string>();
  const componentByNode = new Map<string, number>();
  const components: string[][] = [];

  const strongConnect = (nodeId: string) => {
    tarjanIndices.set(nodeId, tarjanIndex);
    tarjanLowlinks.set(nodeId, tarjanIndex);
    tarjanIndex += 1;
    tarjanStack.push(nodeId);
    tarjanOnStack.add(nodeId);

    for (const nextNodeId of graphOutgoing.get(nodeId) || []) {
      if (!tarjanIndices.has(nextNodeId)) {
        strongConnect(nextNodeId);
        tarjanLowlinks.set(
          nodeId,
          Math.min(tarjanLowlinks.get(nodeId)!, tarjanLowlinks.get(nextNodeId)!),
        );
      } else if (tarjanOnStack.has(nextNodeId)) {
        tarjanLowlinks.set(
          nodeId,
          Math.min(tarjanLowlinks.get(nodeId)!, tarjanIndices.get(nextNodeId)!),
        );
      }
    }

    if (tarjanLowlinks.get(nodeId) === tarjanIndices.get(nodeId)) {
      const componentId = components.length;
      const members: string[] = [];
      let popped = '';
      do {
        popped = tarjanStack.pop() || '';
        if (!popped) break;
        tarjanOnStack.delete(popped);
        componentByNode.set(popped, componentId);
        members.push(popped);
      } while (popped !== nodeId);
      components.push(members);
    }
  };

  for (const nodeId of nodeIds) {
    if (!tarjanIndices.has(nodeId)) strongConnect(nodeId);
  }

  const componentOutgoing = new Map<number, Set<number>>();
  const componentIncoming = new Map<number, Set<number>>();
  for (let componentId = 0; componentId < components.length; componentId += 1) {
    componentOutgoing.set(componentId, new Set());
    componentIncoming.set(componentId, new Set());
  }
  for (const edge of draftEdges.values()) {
    if (edge.kind === 'propagation') continue;
    const sourceComponent = componentByNode.get(edge.source);
    const targetComponent = componentByNode.get(edge.target);
    if (sourceComponent == null || targetComponent == null || sourceComponent === targetComponent) continue;
    componentOutgoing.get(sourceComponent)!.add(targetComponent);
    componentIncoming.get(targetComponent)!.add(sourceComponent);
  }

  const longestDepthFrom = (
    startComponentId: number,
    adjacency: Map<number, Set<number>>,
  ): Map<number, number> => {
    const reachable = new Set<number>([startComponentId]);
    const queue: number[] = [startComponentId];
    while (queue.length > 0) {
      const current = queue.shift()!;
      for (const next of adjacency.get(current) || []) {
        if (reachable.has(next)) continue;
        reachable.add(next);
        queue.push(next);
      }
    }

    const indegree = new Map<number, number>();
    for (const componentId of reachable) indegree.set(componentId, 0);
    for (const componentId of reachable) {
      for (const next of adjacency.get(componentId) || []) {
        if (!reachable.has(next)) continue;
        indegree.set(next, (indegree.get(next) || 0) + 1);
      }
    }

    const topoQueue: number[] = Array.from(reachable).filter((componentId) => (indegree.get(componentId) || 0) === 0);
    const depth = new Map<number, number>([[startComponentId, 0]]);
    while (topoQueue.length > 0) {
      const current = topoQueue.shift()!;
      const currentDepth = depth.get(current);
      for (const next of adjacency.get(current) || []) {
        if (!reachable.has(next)) continue;
        if (currentDepth != null) {
          depth.set(next, Math.max(depth.get(next) ?? Number.NEGATIVE_INFINITY, currentDepth + 1));
        }
        indegree.set(next, (indegree.get(next) || 0) - 1);
        if ((indegree.get(next) || 0) === 0) topoQueue.push(next);
      }
    }
    return depth;
  };

  const seedComponentId = componentByNode.get(seedNodeId);
  const downstreamDepthByComponent = seedComponentId == null
    ? new Map<number, number>()
    : longestDepthFrom(seedComponentId, componentOutgoing);
  const upstreamDepthByComponent = seedComponentId == null
    ? new Map<number, number>()
    : longestDepthFrom(seedComponentId, componentIncoming);

  for (const [nodeId, draft] of draftNodes.entries()) {
    if (draft.kind === 'selected') continue;
    const componentId = componentByNode.get(nodeId);
    if (componentId == null) continue;
    const upstreamDepth = upstreamDepthByComponent.get(componentId);
    const downstreamDepth = downstreamDepthByComponent.get(componentId);
    const hasUpstreamDepth = typeof upstreamDepth === 'number' && Number.isFinite(upstreamDepth) && upstreamDepth > 0;
    const hasDownstreamDepth = typeof downstreamDepth === 'number' && Number.isFinite(downstreamDepth) && downstreamDepth > 0;
    if (!hasUpstreamDepth && !hasDownstreamDepth) continue;

    const currentSide = laneSide(draft.lane);
    let resolvedSide: Exclude<TreeSide, 'center'>;
    if (hasUpstreamDepth && !hasDownstreamDepth) {
      resolvedSide = 'upstream';
    } else if (hasDownstreamDepth && !hasUpstreamDepth) {
      resolvedSide = 'downstream';
    } else if (currentSide !== 'center') {
      resolvedSide = currentSide;
    } else if (draft.column < 0) {
      resolvedSide = 'upstream';
    } else if (draft.column > 0) {
      resolvedSide = 'downstream';
    } else {
      resolvedSide = (upstreamDepth as number) <= (downstreamDepth as number) ? 'upstream' : 'downstream';
    }

    const depth = resolvedSide === 'upstream' ? upstreamDepth : downstreamDepth;
    if (typeof depth === 'number' && Number.isFinite(depth) && depth > 0) {
      draft.column = resolvedSide === 'upstream' ? -depth : depth;
      draft.lane = laneForSide(draft.lane, resolvedSide);
      draftNodes.set(nodeId, draft);
    }
  }

  let columnAdjusted = true;
  let columnPass = 0;
  while (columnAdjusted && columnPass < nodeIds.length * 2) {
    columnAdjusted = false;
    columnPass += 1;
    for (const edge of draftEdges.values()) {
      if (edge.kind === 'propagation') continue;
      const source = draftNodes.get(edge.source);
      const target = draftNodes.get(edge.target);
      if (!source || !target) continue;

      if (source.column >= 0 && target.column >= 0 && target.kind !== 'selected') {
        const nextTargetColumn = Math.max(target.column, source.column + 1);
        if (nextTargetColumn !== target.column) {
          target.column = nextTargetColumn;
          draftNodes.set(target.id, target);
          columnAdjusted = true;
        }
      }

      if (source.column <= 0 && target.column <= 0 && source.kind !== 'selected') {
        const nextSourceColumn = Math.min(source.column, target.column - 1);
        if (nextSourceColumn !== source.column) {
          source.column = nextSourceColumn;
          draftNodes.set(source.id, source);
          columnAdjusted = true;
        }
      }
    }
  }

  const nodesByColumnLane = new Map<string, GraphNodeDraft[]>();
  for (const draft of draftNodes.values()) {
    const key = `${draft.column}:${draft.lane}`;
    const list = nodesByColumnLane.get(key) || [];
    list.push(draft);
    nodesByColumnLane.set(key, list);
  }

  const columns = Array.from(new Set(Array.from(draftNodes.values()).map((node) => node.column))).sort((a, b) => a - b);
  const kindWeight = (kind: TreeNodeKind) => {
    if (kind === 'supply-group') return 0;
    if (kind === 'object') return 1;
    if (kind === 'route') return 2;
    if (kind === 'summary') return 3;
    return -1;
  };
  const orderedByCell = new Map<string, GraphNodeDraft[]>();
  for (const column of columns) {
    const laneOrder = column < 0 ? UPSTREAM_LANES : column > 0 ? DOWNSTREAM_LANES : CENTER_LANES;
    for (const lane of laneOrder) {
      const cellKey = `${column}:${lane}`;
      const list = [...(nodesByColumnLane.get(cellKey) || [])].sort((left, right) => {
        if (left.kind !== right.kind) return kindWeight(left.kind) - kindWeight(right.kind);
        return left.label.localeCompare(right.label);
      });
      if (list.length > 0) orderedByCell.set(cellKey, list);
    }
  }

  const crossingIncoming = new Map<string, string[]>();
  const crossingOutgoing = new Map<string, string[]>();
  for (const edge of draftEdges.values()) {
    if (edge.kind === 'propagation') continue;
    const source = draftNodes.get(edge.source);
    const target = draftNodes.get(edge.target);
    if (!source || !target || source.column === target.column) continue;
    crossingOutgoing.set(edge.source, [...(crossingOutgoing.get(edge.source) || []), edge.target]);
    crossingIncoming.set(edge.target, [...(crossingIncoming.get(edge.target) || []), edge.source]);
  }

  const absoluteOrder = () => {
    const order = new Map<string, number>();
    for (const column of columns) {
      const laneOrder = column < 0 ? UPSTREAM_LANES : column > 0 ? DOWNSTREAM_LANES : CENTER_LANES;
      let index = 0;
      for (const lane of laneOrder) {
        const cellKey = `${column}:${lane}`;
        for (const draft of orderedByCell.get(cellKey) || []) {
          order.set(draft.id, index);
          index += 1;
        }
      }
    }
    return order;
  };

  const barycenterFor = (
    draft: GraphNodeDraft,
    column: number,
    order: Map<string, number>,
    direction: 'ltr' | 'rtl',
  ): number | null => {
    const neighbors = direction === 'ltr'
      ? (crossingIncoming.get(draft.id) || [])
      : (crossingOutgoing.get(draft.id) || []);
    const scores = neighbors
      .map((nodeId) => draftNodes.get(nodeId))
      .filter((neighbor): neighbor is GraphNodeDraft => Boolean(neighbor))
      .filter((neighbor) => direction === 'ltr' ? neighbor.column < column : neighbor.column > column)
      .map((neighbor) => order.get(neighbor.id))
      .filter((value): value is number => typeof value === 'number' && Number.isFinite(value));
    if (scores.length === 0) return null;
    return scores.reduce((sum, value) => sum + value, 0) / scores.length;
  };

  for (let pass = 0; pass < 6; pass += 1) {
    for (const direction of ['ltr', 'rtl'] as const) {
      const order = absoluteOrder();
      const traversal = direction === 'ltr' ? columns : [...columns].reverse();
      for (const column of traversal) {
        const laneOrder = column < 0 ? UPSTREAM_LANES : column > 0 ? DOWNSTREAM_LANES : CENTER_LANES;
        for (const lane of laneOrder) {
          const cellKey = `${column}:${lane}`;
          const list = orderedByCell.get(cellKey);
          if (!list || list.length < 2) continue;
          const next = [...list].sort((left, right) => {
            const leftBary = barycenterFor(left, column, order, direction);
            const rightBary = barycenterFor(right, column, order, direction);
            if (leftBary != null && rightBary != null && leftBary !== rightBary) return leftBary - rightBary;
            if (leftBary != null && rightBary == null) return -1;
            if (leftBary == null && rightBary != null) return 1;
            if (left.kind !== right.kind) return kindWeight(left.kind) - kindWeight(right.kind);
            return left.label.localeCompare(right.label);
          });
          orderedByCell.set(cellKey, next);
        }
      }
    }
  }

  const positionByNodeId = new Map<string, { x: number; y: number }>();
  for (const column of columns) {
    const laneOrder = column < 0 ? UPSTREAM_LANES : column > 0 ? DOWNSTREAM_LANES : CENTER_LANES;
    let totalHeight = 0;
    let blockCount = 0;
    for (const lane of laneOrder) {
      const cellKey = `${column}:${lane}`;
      const list = orderedByCell.get(cellKey) || [];
      if (list.length === 0) continue;
      totalHeight += list.length * NODE_HEIGHT + Math.max(0, list.length - 1) * NODE_GAP;
      blockCount += 1;
    }
    if (blockCount > 1) totalHeight += (blockCount - 1) * 24;
    let currentY = COLUMN_CENTER_Y - totalHeight / 2;
    for (const lane of laneOrder) {
      const cellKey = `${column}:${lane}`;
      const list = orderedByCell.get(cellKey) || [];
      if (list.length === 0) continue;
      list.forEach((draft, index) => {
        positionByNodeId.set(draft.id, {
          x: COLUMN_CENTER + column * COLUMN_GAP,
          y: currentY + index * (NODE_HEIGHT + NODE_GAP),
        });
      });
      currentY += list.length * (NODE_HEIGHT + NODE_GAP) + 24;
    }
  }

  const renderNodeLabel = (draft: GraphNodeDraft) => {
    const baseVisualType = draft.kind === 'supply-group'
      ? 'DEPENDENCY'
      : draft.kind === 'route'
        ? 'SUPPLIER'
        : getHorizonNodeVisualType(draft.objectType, draft.canonicalPath, draft.label, draft.facilityKind)
          || draft.objectType
          || 'default';
    const transportVisualType = detectTransportVisualType(
      baseVisualType,
      draft.canonicalPath,
      draft.label,
      draft.facilityKind,
    );
    const isTransportNode = Boolean(transportVisualType);
    const visualType = transportVisualType || baseVisualType;
    const colors = getNodeColors(visualType, theme);
    const isSelected = draft.kind === 'selected';
    const showSubtitle = draft.kind === 'supply-group' || draft.kind === 'summary';
    const edgeColor = (kind: TreeEdgeKind): string => {
      if (kind === 'direct') return mapLayerColor('direct', theme);
      if (kind === 'context') return mapLayerColor('context', theme);
      if (kind === 'propagation') return mapLayerColor('propagation', theme);
      return mapLayerColor('supply', theme);
    };
    return (
      <div
        style={{
          width: NODE_WIDTH,
          minHeight: NODE_HEIGHT,
          borderRadius: 18,
          padding: '10px 12px',
          background: isTransportNode
            ? 'var(--bg-elevated)'
            : 'var(--bg-elevated)',
          border: `1px ${isTransportNode ? 'dashed' : 'solid'} ${isSelected ? colors.accent : (isTransportNode ? colors.accent : 'var(--border-subtle)')}`,
          boxShadow: isSelected
            ? (theme === 'dark' ? `0 0 0 1px ${colors.accent}, 0 20px 44px rgba(0,0,0,0.24)` : `0 0 0 1px ${colors.accent}, 0 20px 44px rgba(15,23,42,0.12)`)
            : isTransportNode
              ? (theme === 'dark' ? `0 10px 22px rgba(8,47,73,0.32)` : `0 10px 22px rgba(14,165,233,0.10)`)
              : (theme === 'dark' ? '0 12px 26px rgba(0,0,0,0.18)' : '0 12px 26px rgba(15,23,42,0.06)'),
          color: 'var(--text-heading)',
          display: 'flex',
          flexDirection: 'column',
          gap: 9,
          position: 'relative',
          overflow: 'hidden',
        }}
        title={[draft.label, LANE_LABEL[draft.lane], draft.note].filter(Boolean).join('\n')}
      >
          <div
            style={{
              position: 'absolute',
              inset: 0,
              borderTop: isTransportNode ? 'none' : `3px solid ${colors.accent}`,
              borderRadius: 18,
              pointerEvents: 'none',
            }}
          />
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
          <div
            style={{
              width: 30,
              height: 30,
              borderRadius: isTransportNode ? 999 : 10,
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(15,23,42,0.03)',
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'var(--border-subtle)'}`,
              flexShrink: 0,
            }}
          >
            <HorizonNodeIcon type={visualType} color={colors.accent} size={15} />
          </div>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div
              style={{
                fontSize: 12,
                fontWeight: 800,
                lineHeight: 1.25,
                color: 'var(--text-heading)',
                wordBreak: 'break-word',
              }}
            >
              {draft.label}
            </div>
            {showSubtitle && draft.subtitle ? (
              <div
                style={{
                  marginTop: 3,
                  fontSize: 10,
                  lineHeight: 1.3,
                  color: 'var(--text-secondary-strong)',
                  wordBreak: 'break-word',
                }}
              >
                {draft.subtitle}
              </div>
            ) : null}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', minHeight: 14 }}>
          {draft.badges.map((badge) => {
            const badgeColor = edgeColor(badge.kind);
            return (
              <span
                key={`${draft.id}:${badge.label}:${badge.kind}`}
                title={badge.label}
                style={{
                  width: 14,
                  height: 14,
                  borderRadius: 999,
                  background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(15,23,42,0.03)',
                  border: `1px solid ${badgeColor}`,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <span
                  style={{
                    width: 6,
                    height: 6,
                    borderRadius: 999,
                    background: badgeColor,
                    display: 'block',
                  }}
                />
              </span>
            );
          })}
        </div>
      </div>
    );
  };

  const nodes: Node[] = Array.from(draftNodes.values()).map((draft) => {
    const baseVisualType = draft.kind === 'supply-group'
      ? 'DEPENDENCY'
      : draft.kind === 'route'
        ? 'SUPPLIER'
        : getHorizonNodeVisualType(draft.objectType, draft.canonicalPath, draft.label, draft.facilityKind)
          || draft.objectType
          || 'default';
    const isTransportNode = Boolean(
      detectTransportVisualType(baseVisualType, draft.canonicalPath, draft.label, draft.facilityKind),
    );
    return {
      id: draft.id,
      position: positionByNodeId.get(draft.id) || { x: 0, y: 0 },
      data: { label: renderNodeLabel(draft) },
      type: 'default',
      draggable: false,
      selectable: true,
      zIndex: isTransportNode ? 6 : 4,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      style: {
        width: NODE_WIDTH,
        background: 'transparent',
        border: 'none',
        padding: 0,
      },
    };
  });

  const edges: Edge[] = Array.from(draftEdges.values()).map((draft) => {
    const stroke = draft.kind === 'direct'
      ? mapLayerColor('direct', theme)
      : draft.kind === 'context'
        ? mapLayerColor('context', theme)
        : draft.kind === 'propagation'
          ? mapLayerColor('propagation', theme)
          : mapLayerColor('supply', theme);
    const isPropagation = draft.kind === 'propagation';
    return {
      id: draft.id,
      source: draft.source,
      target: draft.target,
      type: 'simplebezier',
      animated: false,
      label: undefined,
      labelStyle: {
        fill: theme === 'dark' ? 'rgba(208,208,216,0.92)' : 'rgba(11,13,18,0.78)',
        fontSize: 10,
        fontWeight: 700,
      },
      labelBgStyle: {
        fill: theme === 'dark' ? 'rgba(26,26,26,0.92)' : 'rgba(255,255,255,0.92)',
        fillOpacity: 1,
      },
      style: {
        stroke,
        strokeWidth: draft.kind === 'direct' ? 2.6 : draft.kind === 'supply' ? 2.4 : isPropagation ? 1.05 : 2,
        opacity: isPropagation ? 0.34 : 1,
        strokeDasharray: draft.kind === 'context'
          ? '6 5'
          : isPropagation
            ? '4 6'
            : undefined,
      },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: stroke,
        width: isPropagation ? 10 : 18,
        height: isPropagation ? 10 : 18,
      },
    };
  });

  return { nodes, edges };
}

export const MapDependencyTreeView: React.FC<MapDependencyTreeViewProps> = ({
  isDark,
  selectedObject,
  objects,
  selectionExportData,
  traceSummary,
}) => {
  const theme: ThemeMode = isDark ? 'dark' : 'light';
  const { nodes, edges } = React.useMemo(
    () => buildTreeGraph(selectedObject, objects, selectionExportData, traceSummary, theme),
    [objects, selectedObject, selectionExportData, theme, traceSummary],
  );

  return (
    <div
      style={{
        position: 'absolute',
        inset: 0,
        zIndex: 11,
        background: 'var(--bg-elevated)',
      }}
    >
      <div
        style={{
          position: 'absolute',
          left: 18,
          top: 18,
          zIndex: 2,
          padding: '10px 12px',
          borderRadius: 14,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
          boxShadow: isDark ? '0 12px 30px rgba(0,0,0,0.18)' : '0 12px 30px rgba(15,23,42,0.06)',
          backdropFilter: 'blur(12px)',
          color: 'var(--text-heading)',
          maxWidth: 360,
        }}
      >
        <div style={{ fontSize: 12, fontWeight: 800 }}>Dependency Tree</div>
        <div style={{ marginTop: 4, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
          Inbound and upstream dependencies extend to the left. Downstream dependencies extend to the right. The map state stays loaded underneath.
        </div>
      </div>

      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        minZoom={0.2}
        maxZoom={1.5}
        proOptions={{ hideAttribution: true }}
      >
        <Background
          gap={20}
          size={1.1}
          color={isDark ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.08)'}
        />
        <Controls
          position="bottom-left"
          style={{
            borderRadius: 14,
            overflow: 'hidden',
            boxShadow: isDark ? '0 10px 24px rgba(0,0,0,0.18)' : '0 10px 24px rgba(15,23,42,0.08)',
          }}
        />
      </ReactFlow>
    </div>
  );
};

export default MapDependencyTreeView;
