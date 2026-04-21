import React from 'react';
import { createPortal } from 'react-dom';
import maplibregl from 'maplibre-gl';
import { useMapInstance } from '../hooks/useMapInstance';
import { useGeoObjects, type GeoObject } from '../hooks/useGeoObjects';
import { useGeoMarkerLayer } from '../hooks/useGeoMarkerLayer';
import { useGeoZoneLayer } from '../hooks/useGeoZoneLayer';
import { useGeoEdgeLayer, GEO_EDGE_INTERACTIVE_LAYERS } from '../hooks/useGeoEdgeLayer';
import { useStaticZoneLayer } from '../hooks/useStaticZoneLayer';
import { useTransportFlowLayer } from '../hooks/useTransportFlowLayer';
import {
  PATH_LEG_LABELS_UPDATED_EVENT,
  PATH_LEG_INTERACTIVE_LAYERS,
  PATH_LEG_OBJECT_LAYER_ID,
  usePathLegLayer,
  type PathLegVisibilityPolicy,
} from '../hooks/usePathLegLayer';
import { usePathLegClusterLayer } from '../hooks/usePathLegClusterLayer';
import { useVesselLayer, vesselsApiAvailable as isVesselsApiAvailable } from '../hooks/useVesselLayer';
import { MapFilterPanel } from './MapFilterPanel';
import { HorizonNodeIcon, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import { MapObjectStackPanel } from './MapObjectStackPanel';
import { MapDependencyTreeView } from './MapDependencyTreeView';
import type { MapObjectInspectorPayload } from './MapObjectDetail';
import type { MapDrawerPanelType } from './HorizonDrawers';
import { SimulationExplanationOverlay, type SimulationExplanationOverlayState } from './SimulationExplanationOverlay';
import { SimulationMapOverlayPanel, type SimulationMapOverlaySelection } from './SimulationMapOverlayPanel';
import { LensBottomPaletteShell, LensBottomPaletteTrigger, buildLensBottomPaletteChrome } from './LensBottomPalette';
import type { LensChatMapFocus } from './LensChatDockPlaceholder';
import { STATIC_ZONE_DATA, deriveZoneTypeCounts, type StaticZoneFeature, type StaticZoneType } from '../lib/staticZones';
import {
  STATIC_TRANSPORT_FLOW_DATA,
  deriveTransportFlowModeCounts,
  type TransportFlowFeature,
  type TransportFlowMode,
} from '../lib/staticTransportFlows';
import {
  api,
  type HorizonSimulationOverlayItem,
  type IdentifyStressVisibleRelation,
  type SupplyChainArtifactResponse,
} from '../lib/api';
import type { MaterializedArtifact } from './SupplyChainOverlay';
import type { GeoMarkerSelection } from '../hooks/useGeoMarkerLayer';
import { pathPayloadNodeIsProduct } from '../lib/materializedPayload';
import { humanizeCanonicalPath, humanizeSlug } from '../lib/uiUtils';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { mapLayerColor } from '../lib/mapRenderPolicy';
import {
  artifactMatchesRouteVisibility,
  resolvedArtifactLegIndex,
  type MapRouteVisibilitySelection,
} from '../lib/mapRouteVisibility';
import { objectIdsWithMarkerEndpointCaption } from '../lib/markerPresentationPolicy';
import { buildDenseCaptionGroups } from '../lib/mapDenseCaptionGroups';

export interface WorldMapPanelProps {
  scope?: { lensId: string };
  onOpenDocuments?: (() => void) | null;
  chatMapFocus?: LensChatMapFocus | null;
  onClearChatMapFocus?: (() => void) | null;
  onMapInspectorStateChange?: ((state: MapObjectInspectorPayload | null) => void) | null;
  activeMapDrawerPanel?: MapDrawerPanelType | null;
  onMapDrawerPanelChange?: ((panel: MapDrawerPanelType | null) => void) | null;
  mapDrawerHost?: HTMLDivElement | null;
}

type MapActivityPayload = Awaited<ReturnType<typeof api.getHorizonMapActivity>>;
type ZoneActivityItem = MapActivityPayload['zones'][number];
type FlowActivityItem = MapActivityPayload['flows'][number];
type MapObjectStackSelection = Extract<GeoMarkerSelection, { kind: 'stack' }>;
type AffectedZoneFreshnessWindow = 14 | 30 | 90 | 180;
type SearchSuggestion = {
  object: GeoObject;
  title: string;
  subtitle: string;
  score: number;
};
type SearchMode = 'objects' | 'supply-links';
type HorizonSearchEntity = Awaited<ReturnType<typeof api.searchHorizonEntities>>['entities'][number];
type ProductRouteGroup = {
  key: string;
  label: string;
  canonicalPath?: string;
  stage: string;
  stageLabel: string;
  searchText: string;
  artifactIds: string[];
  routeCount: number;
  pinnedCount: number;
  topRoutes: string[];
};
type ProductRouteEntry = {
  key: string;
  label: string;
  canonicalPath?: string;
  stage: string;
  searchText: string;
};
type ProductStageFacet = {
  key: string;
  label: string;
  routeCount: number;
};
type SimulationExplanationCache = Record<string, SimulationExplanationOverlayState>;
type DenseCaptionOverlayGroup = {
  id: string;
  x: number;
  y: number;
  label: string;
  totalCount: number;
  visibleObjects: GeoObject[];
};

const normalizeCountryFromCanonicalPath = (value: string): string => {
  const match = String(value || '').trim().toLowerCase().match(/^\/(?:fac|org|loc|gov|deposit|market|product)\/([a-z]{2})(?:\/|$)/);
  return match?.[1] || '';
};

const compactStringList = (values: string[]): string[] =>
  Array.from(new Set(values.map((value) => String(value || '').trim()).filter(Boolean)));

const compactNonEmptyStrings = (values: string[]): string[] =>
  Array.from(new Set(values.map((value) => String(value || '').trim()).filter(Boolean)));

const isObjectLikeSupplyAnchor = (entity: HorizonSearchEntity | null | undefined): boolean => {
  const canonicalPath = String(entity?.canonicalPath || '').trim();
  if (!canonicalPath.startsWith('/')) return false;
  return !canonicalPath.startsWith('/supply-link/')
    && !canonicalPath.startsWith('/supply-chain/')
    && !canonicalPath.startsWith('/artifact/');
};

const rankSupplyAnchorEntity = (entity: HorizonSearchEntity | null | undefined): number => {
  if (!entity) return 99;
  return isObjectLikeSupplyAnchor(entity) ? 0 : 1;
};

const normalizeSupplyAnchorEntities = (entities: HorizonSearchEntity[]): HorizonSearchEntity[] => {
  return [...entities].sort((left, right) => {
    const rankDiff = rankSupplyAnchorEntity(left) - rankSupplyAnchorEntity(right);
    if (rankDiff !== 0) return rankDiff;
    return String(left?.name || left?.canonicalPath || '').localeCompare(String(right?.name || right?.canonicalPath || ''));
  });
};
type SupplyChainSearchResult = {
  artifact: SupplyChainArtifactResponse;
  previewPayload?: any;
  match?: any;
  score?: number;
  supplyLinkCoverage?: any;
};
type ScopeTargetMode = 'new' | 'existing';
type DirectedExploreDirection = 'upstream' | 'downstream';
type DockPanel = 'filter' | null;
type FilterOverlayMode = 'products' | 'routes';
type MapSurfaceMode = 'map' | 'dependency-tree';

const AFFECTED_ZONE_FRESHNESS_WINDOWS: AffectedZoneFreshnessWindow[] = [14, 30, 90, 180];
const PRODUCT_STAGE_ORDER = [
  'extraction',
  'refining',
  'smelting',
  'liquefaction',
  'cracking',
  'processing',
  'manufacturing',
  'assembly',
  'fresh',
  'medical-supplies',
  'delivery',
  'distribution',
  'export',
  'service',
] as const;

function titleCaseLabel(value?: string): string {
  const normalized = humanizeSlug(value).trim();
  if (!normalized) return '';
  if (/[A-Z]/.test(normalized)) return normalized;
  return normalized
    .split(/\s+/g)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function parseProductCanonicalPath(path?: string): {
  category: string;
  productType: string;
  stage: string;
  slug: string;
} | null {
  const value = String(path || '').trim();
  if (!value.startsWith('/product/')) return null;
  const parts = value.split('/').filter(Boolean);
  if (parts.length < 5 || parts[0] !== 'product') return null;
  return {
    category: String(parts[1] || '').trim().toLowerCase(),
    productType: String(parts[2] || '').trim().toLowerCase(),
    stage: String(parts[3] || '').trim().toLowerCase(),
    slug: String(parts[4] || '').trim().toLowerCase(),
  };
}

function productStageKey(value?: string): string {
  const parsed = parseProductCanonicalPath(value);
  return parsed?.stage || 'other';
}

function productStageLabel(stage: string): string {
  return stage === 'other' ? 'Other' : titleCaseLabel(stage) || stage;
}

function productStageRank(stage: string): number {
  const index = PRODUCT_STAGE_ORDER.indexOf(stage as typeof PRODUCT_STAGE_ORDER[number]);
  return index >= 0 ? index : Number.MAX_SAFE_INTEGER;
}

function isDirectedExplorationCanonicalPath(canonicalPath?: string): boolean {
  const cp = String(canonicalPath || '').trim().toLowerCase();
  return cp.startsWith('/org/')
    || cp.startsWith('/gov/')
    || cp.startsWith('/fac/')
    || cp.startsWith('/product/')
    || cp.startsWith('/deposit/');
}

function affectedZoneTimestamp(obj: GeoObject): number | null {
  const value = obj.lastMatchedAt || obj.updatedAt || obj.createdAt;
  if (!value) return null;
  const ts = new Date(value).getTime();
  return Number.isFinite(ts) ? ts : null;
}

function artifactRefreshSignature(artifact: MaterializedArtifact | null | undefined): string {
  const riskOverlay = artifact?.riskOverlay || null;
  const value = artifact || ({} as MaterializedArtifact);
  return [
    String(value.id || '').trim(),
    String(value.canonicalKey || '').trim(),
    String(value.displayName || '').trim(),
    String(value.anchorSupplier || '').trim(),
    String(value.anchorCustomer || '').trim(),
    String(value.productAxis || '').trim(),
    String(value.legCount || ''),
    String(value.pathStressScore || ''),
    String(value.traceRole || '').trim(),
    String(value.familyId || '').trim(),
    Array.isArray(value.nodeRefs) ? value.nodeRefs.join('|') : '',
    Array.isArray(value.flowRefs) ? value.flowRefs.join('|') : '',
    Array.isArray(value.zoneRefs) ? value.zoneRefs.join('|') : '',
    Array.isArray(value.affectedZoneRefs) ? value.affectedZoneRefs.join('|') : '',
    JSON.stringify({
      criticality: riskOverlay?.criticality,
      primaryMechanism: riskOverlay?.primaryMechanism,
      topMetrics: Array.isArray(riskOverlay?.topMetrics) ? riskOverlay.topMetrics.length : 0,
      topMechanisms: Array.isArray(riskOverlay?.mechanisms) ? riskOverlay.mechanisms.length : 0,
      legs: Array.isArray(riskOverlay?.legs) ? riskOverlay.legs.length : 0,
      bottleneckLegIndex: riskOverlay?.bottleneckLegIndex,
    }),
  ].join('::');
}

function sameArtifactRefreshSet(prev: MaterializedArtifact[], next: MaterializedArtifact[]): boolean {
  if (prev === next) return true;
  if (prev.length !== next.length) return false;
  for (let index = 0; index < prev.length; index += 1) {
    if (artifactRefreshSignature(prev[index]) !== artifactRefreshSignature(next[index])) return false;
  }
  return true;
}

function isAffectedZoneRenderable(
  obj: GeoObject,
  freshnessWindowDays: AffectedZoneFreshnessWindow,
  showArchived: boolean,
  nowTs: number,
): boolean {
  if (obj.type !== 'AFFECTED_ZONE') return true;
  if (showArchived) return true;

  const lifecycleStatus = String(obj.status || '').trim().toLowerCase();
  if (lifecycleStatus === 'archived' || lifecycleStatus === 'deleted') return false;

  const status = String(obj.activityStatus || '').trim().toLowerCase();
  if (status === 'hot' || status === 'active' || status === 'watch') return true;

  const ts = affectedZoneTimestamp(obj);
  if (!ts) return false;
  const ageDays = (nowTs - ts) / 86_400_000;

  if (status === 'cooling') return ageDays <= freshnessWindowDays;
  if (status === 'inactive') return false;
  return ageDays <= freshnessWindowDays;
}

function getSearchObjectTitle(obj: GeoObject): string {
  return String(obj.label || '').trim() || humanizeCanonicalPath(obj.canonicalPath) || obj.canonicalPath;
}

function compareMapObjectsForDenseCaption(left: GeoObject, right: GeoObject): number {
  const scopeDelta = Number(right.inScope !== false) - Number(left.inScope !== false);
  if (scopeDelta !== 0) return scopeDelta;
  return getSearchObjectTitle(left).localeCompare(getSearchObjectTitle(right));
}

function denseCaptionObjectAccent(object: GeoObject, theme: 'dark' | 'light'): { accent: string; visualType: string } {
  const visualType = getHorizonNodeVisualType(object.type, object.canonicalPath, object.label, object.facilityKind) || object.type;
  return {
    visualType,
    accent: getNodeColors(visualType, theme).accent,
  };
}

function getSearchObjectSubtitle(obj: GeoObject): string {
  const type = String(obj.type || '').replace(/_/g, ' ').toLowerCase();
  const path = String(obj.canonicalPath || '').trim();
  if (!path) return type;
  return type ? `${type} · ${path}` : path;
}

function buildSearchText(obj: GeoObject): string {
  return [
    obj.label,
    obj.canonicalPath,
    obj.type,
    obj.zoneType,
    obj.zoneSubType,
    obj.description,
  ]
    .map((part) => String(part || '').trim().toLowerCase())
    .filter(Boolean)
    .join(' ');
}

function artifactRouteTitle(artifact: MaterializedArtifact | null | undefined): string {
  const productAxis = String(artifact?.productAxis || '').trim();
  const productFallback = artifactRouteProductEntries(artifact)[0]?.label
    || (productAxis.startsWith('/product/') ? (humanizeCanonicalPath(productAxis) || productAxis) : productAxis);
  return String(
    artifact?.displayName
    || productFallback
    || humanizeCanonicalPath(String(artifact?.canonicalKey || ''))
    || artifact?.id
    || 'Supply route',
  ).trim();
}

function artifactRouteSubtitle(artifact: MaterializedArtifact | null | undefined): string {
  const supplier = String(artifact?.anchorSupplier || artifact?.traceEndpoints?.supplier?.label || 'upstream').trim();
  const customer = String(artifact?.anchorCustomer || artifact?.traceEndpoints?.customer?.label || 'downstream').trim();
  return `${supplier} → ${customer}`;
}

function artifactRouteProducts(artifact: MaterializedArtifact | null | undefined): string[] {
  return artifactRouteProductEntries(artifact).map((entry) => entry.label).slice(0, 3);
}

function artifactRouteProductEntries(artifact: MaterializedArtifact | null | undefined): ProductRouteEntry[] {
  const seen = new Set<string>();
  const out: ProductRouteEntry[] = [];
  const payload = artifact?.payload as MaterializedArtifact['payload'] & {
    productNodes?: Array<{ class?: string; type?: string; ref?: string; name?: string }>;
  };
  const productNodes = Array.isArray(payload?.productNodes) ? payload.productNodes : [];
  const productNameByRef = new Map<string, string>();
  for (const node of productNodes) {
    const ref = String(node?.ref || '').trim();
    if (!ref || !pathPayloadNodeIsProduct(node)) continue;
    const name = String(node?.name || '').trim();
    if (name && !productNameByRef.has(ref)) productNameByRef.set(ref, name);
  }
  const push = (value: string, preferredLabel?: string) => {
    const raw = String(value || '').trim();
    if (!raw || seen.has(raw)) return;
    seen.add(raw);
    const canonicalPath = raw.startsWith('/product/') ? raw : undefined;
    const parsed = parseProductCanonicalPath(canonicalPath);
    const stage = productStageKey(canonicalPath);
    const label = String(preferredLabel || '').trim()
      || (canonicalPath ? (humanizeCanonicalPath(canonicalPath) || canonicalPath) : '')
      || titleCaseLabel(raw)
      || raw;
    out.push({
      key: canonicalPath || label.toLowerCase(),
      label,
      canonicalPath,
      stage,
      searchText: [
        label,
        raw,
        parsed?.category,
        parsed?.productType,
        productStageLabel(stage),
      ]
        .map((part) => String(part || '').trim().toLowerCase())
        .filter(Boolean)
        .join(' '),
    });
  };
  const direct = String(artifact?.productAxis || '').trim();
  if (direct) push(direct, productNameByRef.get(direct));
  const legs = Array.isArray(artifact?.payload?.legs) ? artifact!.payload!.legs! : [];
  for (const leg of legs) {
    const productRef = String(leg?.productRef || '').trim();
    if (!productRef) continue;
    push(productRef, productNameByRef.get(productRef));
  }
  for (const node of productNodes) {
    const productRef = String(node?.ref || '').trim();
    if (!productRef || !pathPayloadNodeIsProduct(node)) continue;
    push(productRef, node?.name);
  }
  return out;
}

function artifactRouteSearchText(artifact: MaterializedArtifact | null | undefined): string {
  return [
    artifactRouteTitle(artifact),
    artifactRouteSubtitle(artifact),
    String(artifact?.canonicalKey || '').trim(),
    String(artifact?.discoveryReason || '').trim(),
    ...artifactRouteProductEntries(artifact).map((entry) => entry.searchText),
  ]
    .join(' ')
    .toLowerCase();
}

function mapDockButtonStyle(isDark: boolean, active: boolean, accent: string): React.CSSProperties {
  return {
    width: 44,
    height: 44,
    borderRadius: 14,
    border: active
      ? `1px solid ${accent}`
      : (isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.12)'),
    background: active
      ? (isDark ? 'rgba(8,15,26,0.96)' : 'rgba(255,255,255,0.98)')
      : (isDark ? 'rgba(5,11,17,0.86)' : 'rgba(248,250,252,0.94)'),
    color: active ? accent : 'var(--text-heading)',
    boxShadow: active
      ? (isDark ? 'inset 0 1px 0 rgba(255,255,255,0.05), 0 14px 30px rgba(0,0,0,0.34)' : 'inset 0 1px 0 rgba(255,255,255,0.8), 0 14px 30px rgba(15,23,42,0.16)')
      : (isDark ? '0 10px 24px rgba(0,0,0,0.28)' : '0 10px 24px rgba(15,23,42,0.08)'),
    backdropFilter: 'blur(12px)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    transform: active ? 'translateY(1px)' : 'translateY(0)',
    transition: 'transform 160ms ease, border-color 160ms ease, background-color 160ms ease, color 160ms ease',
  };
}

function mapDockControlButtonStyle(isDark: boolean, active: boolean, accent: string): React.CSSProperties {
  return {
    minWidth: 112,
    height: 42,
    padding: '0 12px',
    borderRadius: 14,
    border: active
      ? `1px solid ${accent}`
      : (isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.12)'),
    background: active
      ? (isDark ? 'rgba(8,15,26,0.96)' : 'rgba(255,255,255,0.98)')
      : (isDark ? 'rgba(5,11,17,0.86)' : 'rgba(248,250,252,0.94)'),
    color: active ? accent : 'var(--text-heading)',
    boxShadow: active
      ? (isDark ? 'inset 0 1px 0 rgba(255,255,255,0.05), 0 14px 30px rgba(0,0,0,0.34)' : 'inset 0 1px 0 rgba(255,255,255,0.8), 0 14px 30px rgba(15,23,42,0.16)')
      : (isDark ? '0 10px 24px rgba(0,0,0,0.28)' : '0 10px 24px rgba(15,23,42,0.08)'),
    backdropFilter: 'blur(12px)',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: '0.01em',
    transition: 'transform 160ms ease, border-color 160ms ease, background-color 160ms ease, color 160ms ease',
    transform: active ? 'translateY(1px)' : 'translateY(0)',
  };
}

function mapSurfaceToggleShellStyle(isDark: boolean): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: 6,
    borderRadius: 16,
    border: isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.10)',
    background: isDark ? 'rgba(5,11,17,0.88)' : 'rgba(255,255,255,0.94)',
    boxShadow: isDark ? '0 14px 32px rgba(0,0,0,0.28)' : '0 14px 32px rgba(15,23,42,0.10)',
    backdropFilter: 'blur(14px)',
  };
}

function mapSurfaceToggleButtonStyle(
  isDark: boolean,
  active: boolean,
): React.CSSProperties {
  return {
    minWidth: 148,
    height: 38,
    padding: '0 14px',
    borderRadius: 12,
    border: active
      ? '1px solid rgba(102,153,153,0.32)'
      : '1px solid transparent',
    background: active
      ? (isDark ? 'rgba(102,153,153,0.18)' : 'rgba(102,153,153,0.10)')
      : 'transparent',
    color: active ? '#669999' : 'var(--text-secondary-strong)',
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: '0.01em',
    cursor: 'pointer',
    transition: 'background-color 160ms ease, color 160ms ease, border-color 160ms ease',
  };
}

function mapDockRailButtonStyle(isDark: boolean, active: boolean): React.CSSProperties {
  return {
    width: 40,
    height: 40,
    borderRadius: 10,
    border: active
      ? 'none'
      : (isDark ? '1px solid rgba(255,255,255,0.1)' : '1px solid rgba(15,23,42,0.1)'),
    background: active
      ? '#669999'
      : (isDark ? 'rgba(8, 14, 22, 0.92)' : 'rgba(255,255,255,0.95)'),
    color: active ? '#fff' : 'var(--text-heading)',
    boxShadow: active
      ? '0 10px 22px rgba(0, 0, 0, 0.28)'
      : (isDark ? '0 10px 24px rgba(0,0,0,0.24)' : '0 10px 24px rgba(15,23,42,0.08)'),
    backdropFilter: 'blur(12px)',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    transition: 'border-color 160ms ease, background-color 160ms ease, color 160ms ease, transform 160ms ease',
    transform: active ? 'translateY(1px)' : 'translateY(0)',
  };
}

function mapDockOverlayCardStyle(isDark: boolean, width: number): React.CSSProperties {
  return {
    width,
    maxWidth: 'min(calc(100vw - 72px), 360px)',
    borderRadius: 18,
    border: isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.10)',
    background: isDark ? 'rgba(6, 18, 31, 0.94)' : 'rgba(255,255,255,0.96)',
    boxShadow: isDark ? '0 22px 60px rgba(0,0,0,0.42)' : '0 22px 60px rgba(15,23,42,0.14)',
    backdropFilter: 'blur(18px)',
    overflow: 'hidden',
  };
}

function mapDrawerPanelShellStyle(isDark: boolean): React.CSSProperties {
  return {
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    padding: '4px 2px 10px 0',
    color: 'var(--text-heading)',
  };
}

function mapDrawerPanelLeadStyle(isDark: boolean): React.CSSProperties {
  return {
    borderRadius: '16px 16px 16px 8px',
    border: '1px solid var(--border-subtle)',
    background: isDark
      ? 'rgba(255,255,255,0.025)'
      : 'rgba(15,23,42,0.028)',
    padding: '12px 14px',
    display: 'grid',
    gap: 6,
  };
}

function mapDrawerSectionStyle(
  isDark: boolean,
  tone: 'default' | 'muted' | 'accent' | 'danger' = 'default',
): React.CSSProperties {
  const toneMap = {
    default: {
      background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(15,23,42,0.03)',
      border: '1px solid var(--border-subtle)',
    },
    muted: {
      background: isDark ? 'rgba(255,255,255,0.018)' : 'rgba(15,23,42,0.022)',
      border: '1px solid var(--border-subtle)',
    },
    accent: {
      background: isDark ? 'rgba(102,153,153,0.10)' : 'rgba(102,153,153,0.08)',
      border: isDark ? '1px solid rgba(102,153,153,0.24)' : '1px solid rgba(102,153,153,0.18)',
    },
    danger: {
      background: isDark ? 'rgba(239,68,68,0.10)' : 'rgba(239,68,68,0.06)',
      border: isDark ? '1px solid rgba(239,68,68,0.22)' : '1px solid rgba(239,68,68,0.16)',
    },
  } as const;
  return {
    borderRadius: 16,
    padding: '12px 14px',
    display: 'grid',
    gap: 10,
    ...toneMap[tone],
  };
}

function mapDrawerMetricCardStyle(isDark: boolean): React.CSSProperties {
  return {
    padding: '10px 11px',
    borderRadius: 14,
    background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
    border: '1px solid var(--border-subtle)',
  };
}

function mapDrawerActionButtonStyle(
  isDark: boolean,
  variant: 'default' | 'accent' = 'default',
  disabled = false,
): React.CSSProperties {
  const accent = variant === 'accent';
  return {
    width: '100%',
    borderRadius: 14,
    border: accent
      ? (isDark ? '1px solid rgba(102,153,153,0.28)' : '1px solid rgba(102,153,153,0.22)')
      : '1px solid var(--border-subtle)',
    background: accent
      ? (isDark ? 'rgba(102,153,153,0.14)' : 'rgba(102,153,153,0.10)')
      : (isDark ? 'rgba(255,255,255,0.022)' : 'rgba(15,23,42,0.024)'),
    color: disabled ? 'var(--text-secondary)' : 'var(--text-heading)',
    padding: '10px 12px',
    fontSize: 12,
    fontWeight: 700,
    letterSpacing: '0.01em',
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.62 : 1,
    transition: 'background-color 160ms ease, border-color 160ms ease, opacity 160ms ease',
  };
}

function slugifyDownloadPart(value: string, fallback: string): string {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return normalized || fallback;
}

function downloadJsonFile(filename: string, payload: unknown): void {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => window.URL.revokeObjectURL(url), 0);
}

function scoreSearchObject(obj: GeoObject, query: string): SearchSuggestion | null {
  const title = getSearchObjectTitle(obj);
  const subtitle = getSearchObjectSubtitle(obj);
  const haystack = buildSearchText(obj);
  const trimmed = query.trim().toLowerCase();
  const inScopeBoost = obj.inScope === false ? 0 : 8;
  const hypeBoost = Number.isFinite(Number(obj.hype)) ? Math.min(Number(obj.hype) / 8, 14) : 0;

  if (!trimmed) {
    return {
      object: obj,
      title,
      subtitle,
      score: inScopeBoost + hypeBoost,
    };
  }

  if (!haystack.includes(trimmed)) return null;

  let score = inScopeBoost + hypeBoost;
  const titleLower = title.toLowerCase();
  const pathLower = String(obj.canonicalPath || '').toLowerCase();

  if (titleLower === trimmed) score += 120;
  else if (titleLower.startsWith(trimmed)) score += 80;
  else if (titleLower.includes(trimmed)) score += 48;

  if (pathLower.startsWith(trimmed)) score += 24;
  else if (pathLower.includes(trimmed)) score += 12;

  for (const token of trimmed.split(/\s+/).filter(Boolean)) {
    if (titleLower.startsWith(token)) score += 18;
    else if (titleLower.includes(token)) score += 10;
    else if (haystack.includes(token)) score += 4;
  }

  return { object: obj, title, subtitle, score };
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

function collectArtifactPathRefs(artifacts: MaterializedArtifact[]): string[] {
  const seen = new Set<string>();
  const refs: string[] = [];
  for (const artifact of artifacts || []) {
    const legs = Array.isArray(artifact?.payload?.legs) ? artifact.payload.legs : [];
    for (const leg of legs) {
      const candidates = [
        artifactLegString(leg, 'supplierRef', 'SupplierRef'),
        ...artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs'),
        artifactLegString(leg, 'customerRef', 'CustomerRef'),
      ];
      for (const ref of candidates) {
        if (!ref || seen.has(ref)) continue;
        seen.add(ref);
        refs.push(ref);
      }
    }
  }
  return refs;
}

function collectArtifactTerminalPathRefs(artifacts: MaterializedArtifact[]): string[] {
  const seen = new Set<string>();
  const refs: string[] = [];
  for (const artifact of artifacts || []) {
    const legs = Array.isArray(artifact?.payload?.legs) ? artifact.payload.legs : [];
    for (const leg of legs) {
      const candidates = [
        artifactLegString(leg, 'supplierRef', 'SupplierRef'),
        artifactLegString(leg, 'customerRef', 'CustomerRef'),
      ];
      for (const ref of candidates) {
        if (!ref || seen.has(ref)) continue;
        seen.add(ref);
        refs.push(ref);
      }
    }
  }
  return refs;
}

function collectArtifactKeys(artifacts: MaterializedArtifact[]): string[] {
  const seen = new Set<string>();
  const keys: string[] = [];
  for (const artifact of artifacts || []) {
    const key = artifactCatalogKey(artifact);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    keys.push(key);
  }
  return keys;
}

function artifactCatalogKey(artifact: MaterializedArtifact | null | undefined): string {
  return String(artifact?.canonicalKey || artifact?.id || '').trim();
}

function collectArtifactSupplyLinkRefs(artifacts: MaterializedArtifact[]): string[] {
  const seen = new Set<string>();
  const refs: string[] = [];
  const push = (value: unknown) => {
    const ref = String(value || '').trim();
    if (!ref || !ref.startsWith('/supply-link/') || seen.has(ref)) return;
    seen.add(ref);
    refs.push(ref);
  };
  for (const artifact of artifacts || []) {
    for (const ref of Array.isArray(artifact?.matchedRefs) ? artifact.matchedRefs : []) push(ref);
    for (const ref of Array.isArray(artifact?.nodeRefs) ? artifact.nodeRefs : []) push(ref);
    const payload = artifact?.payload as (MaterializedArtifact['payload'] & {
      nodes?: Array<{ ref?: string }>;
      edges?: Array<{ supplyLinkId?: string; supplyLinkCanonicalPath?: string; qualifiers?: Record<string, any> }>;
      legs?: Array<{ supplyLinkRef?: string; supplyLinkCanonicalPath?: string }>;
    }) | undefined;
    for (const node of Array.isArray(payload?.nodes) ? payload.nodes : []) push(node?.ref);
    for (const edge of Array.isArray(payload?.edges) ? payload.edges : []) {
      push(edge?.supplyLinkId);
      push(edge?.supplyLinkCanonicalPath);
      push(edge?.qualifiers?.supplyLinkCanonicalPath);
    }
    for (const leg of Array.isArray(payload?.legs) ? payload.legs : []) {
      push(leg?.supplyLinkRef);
      push(leg?.supplyLinkCanonicalPath);
    }
  }
  return refs;
}

function collectArtifactChatFocusRefs(artifact: MaterializedArtifact | null | undefined): string[] {
  const seen = new Set<string>();
  const refs: string[] = [];
  const push = (value: unknown) => {
    const ref = String(value || '').trim();
    if (!ref || !ref.startsWith('/') || seen.has(ref)) return;
    seen.add(ref);
    refs.push(ref);
  };
  if (!artifact) return refs;
  push(artifact.anchorSupplier);
  push(artifact.anchorCustomer);
  push(artifact.productAxis);
  for (const ref of artifact.nodeRefs || []) push(ref);
  for (const ref of artifact.flowRefs || []) push(ref);
  for (const ref of artifact.zoneRefs || []) push(ref);
  for (const ref of artifact.affectedZoneRefs || []) push(ref);
  for (const ref of collectArtifactPathRefs([artifact])) push(ref);
  for (const ref of collectArtifactSupplyLinkRefs([artifact])) push(ref);
  return refs;
}

function collectVisibleGraphRelations(
  selectionExportData: any,
  objects: GeoObject[],
  selectedObject: GeoObject | null,
): IdentifyStressVisibleRelation[] {
  const idToPath = new Map<string, string>();
  for (const object of objects || []) {
    const id = String(object?.id || '').trim();
    const canonicalPath = String(object?.canonicalPath || '').trim();
    if (id && canonicalPath && !idToPath.has(id)) idToPath.set(id, canonicalPath);
  }
  if (selectedObject?.id && selectedObject?.canonicalPath) {
    idToPath.set(String(selectedObject.id).trim(), String(selectedObject.canonicalPath).trim());
  }
  const out: IdentifyStressVisibleRelation[] = [];
  const seen = new Set<string>();
  const push = (relation: IdentifyStressVisibleRelation) => {
    const sourceCanonicalPath = String(relation.sourceCanonicalPath || '').trim();
    const targetCanonicalPath = String(relation.targetCanonicalPath || '').trim();
    const edgeId = String(relation.edgeId || '').trim();
    const relationType = String(relation.relationType || '').trim();
    const relationRole = String(relation.relationRole || '').trim();
    if (!sourceCanonicalPath && !targetCanonicalPath && !edgeId) return;
    const key = [edgeId, sourceCanonicalPath, targetCanonicalPath, relationType, relationRole].join('|');
    if (seen.has(key)) return;
    seen.add(key);
    out.push({
      edgeId: edgeId || undefined,
      sourceCanonicalPath: sourceCanonicalPath || undefined,
      targetCanonicalPath: targetCanonicalPath || undefined,
      relationType: relationType || undefined,
      relationFamily: String(relation.relationFamily || '').trim() || undefined,
      relationRole: relationRole || undefined,
      businessMeaning: String(relation.businessMeaning || '').trim() || undefined,
      whyRelevant: String(relation.whyRelevant || '').trim() || undefined,
      criticality: typeof relation.criticality === 'number' ? relation.criticality : undefined,
    });
  };

  const relationLists = [
    { items: selectionExportData?.directRelations, role: 'direct' },
    { items: selectionExportData?.contextRelations, role: 'context' },
  ];
  for (const list of relationLists) {
    for (const raw of Array.isArray(list.items) ? list.items : []) {
      const sourceId = String(raw?.sourceId || '').trim();
      const targetId = String(raw?.targetId || '').trim();
      const sourceCanonicalPath = idToPath.get(sourceId) || '';
      const targetCanonicalPath = String(raw?.relatedCanonicalPath || '').trim() || idToPath.get(targetId) || '';
      push({
        edgeId: String(raw?.edgeId || '').trim() || undefined,
        sourceCanonicalPath: sourceCanonicalPath || undefined,
        targetCanonicalPath: targetCanonicalPath || undefined,
        relationType: String(raw?.type || '').trim() || undefined,
        relationFamily: String(raw?.family || '').trim() || undefined,
        relationRole: list.role,
        businessMeaning: String(raw?.businessMeaning || '').trim() || undefined,
        whyRelevant: String(raw?.whyRelevant || '').trim() || undefined,
        criticality: typeof raw?.criticality === 'number' ? raw.criticality : undefined,
      });
    }
  }

  for (const raw of Array.isArray(selectionExportData?.trace?.propagation?.edges) ? selectionExportData.trace.propagation.edges : []) {
    const sourceId = String(raw?.sourceFragmentId || '').trim();
    const targetId = String(raw?.targetFragmentId || '').trim();
    const sourceCanonicalPath = idToPath.get(sourceId) || '';
    const targetCanonicalPath = idToPath.get(targetId) || '';
    push({
      edgeId: String(raw?.edgeId || '').trim() || undefined,
      sourceCanonicalPath: sourceCanonicalPath || undefined,
      targetCanonicalPath: targetCanonicalPath || undefined,
      relationType: 'PROPAGATES_TO',
      relationFamily: 'propagation',
      relationRole: 'propagation',
    });
  }

  return out;
}

export const WorldMapPanel: React.FC<WorldMapPanelProps> = ({
  scope,
  onOpenDocuments = null,
  chatMapFocus = null,
  onClearChatMapFocus = null,
  onMapInspectorStateChange = null,
  activeMapDrawerPanel = null,
  onMapDrawerPanelChange = null,
  mapDrawerHost = null,
}) => {
  const overlayTopOffset = scope?.lensId ? 58 : 10;
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  const { map, isReady, styleEpoch, theme } = useMapInstance(containerRef);
  const isDark = theme === 'dark';

  const [dateFrom, setDateFrom] = React.useState('');
  const [dateTo, setDateTo] = React.useState('');
  const [activeTypes, setActiveTypes] = React.useState<Set<string> | null>(null);
  const [searchOpen, setSearchOpen] = React.useState(false);
  const [searchMode, setSearchMode] = React.useState<SearchMode>('objects');
  const [searchQuery, setSearchQuery] = React.useState('');
  const searchQueryTrim = searchQuery.trim();
  const [searchActiveIndex, setSearchActiveIndex] = React.useState(0);
  const [supplyAnchorAQuery, setSupplyAnchorAQuery] = React.useState('');
  const [supplyAnchorBQuery, setSupplyAnchorBQuery] = React.useState('');
  const [supplyAnchorA, setSupplyAnchorA] = React.useState<HorizonSearchEntity | null>(null);
  const [supplyAnchorB, setSupplyAnchorB] = React.useState<HorizonSearchEntity | null>(null);
  const [betweenAnchors, setBetweenAnchors] = React.useState(false);
  const [supplyMaxLegs, setSupplyMaxLegs] = React.useState(6);
  const [supplySearchLoading, setSupplySearchLoading] = React.useState(false);
  const [supplySearchError, setSupplySearchError] = React.useState<string | null>(null);
  const [supplySearchResults, setSupplySearchResults] = React.useState<SupplyChainSearchResult[]>([]);
  const [manualPathArtifacts, setManualPathArtifacts] = React.useState<MaterializedArtifact[]>([]);
  const [manualChainOverlayOpen, setManualChainOverlayOpen] = React.useState(false);
  const [manualChainOverlayCollapsed, setManualChainOverlayCollapsed] = React.useState(false);
  const [activeManualChain, setActiveManualChain] = React.useState<SupplyChainSearchResult | null>(null);
  const [anchorASuggestions, setAnchorASuggestions] = React.useState<HorizonSearchEntity[]>([]);
  const [anchorBSuggestions, setAnchorBSuggestions] = React.useState<HorizonSearchEntity[]>([]);
  const [anchorASuggestionsLoading, setAnchorASuggestionsLoading] = React.useState(false);
  const [anchorBSuggestionsLoading, setAnchorBSuggestionsLoading] = React.useState(false);
  const [showAnchorASuggestions, setShowAnchorASuggestions] = React.useState(false);
  const [showAnchorBSuggestions, setShowAnchorBSuggestions] = React.useState(false);
  const [scopeDialogOpen, setScopeDialogOpen] = React.useState(false);
  const [scopeDialogSubmitting, setScopeDialogSubmitting] = React.useState(false);
  const [scopeDialogError, setScopeDialogError] = React.useState<string | null>(null);
  const [scopeDialogTargetMode, setScopeDialogTargetMode] = React.useState<ScopeTargetMode>(scope?.lensId ? 'existing' : 'new');
  const [scopeDialogUseCurrentLens, setScopeDialogUseCurrentLens] = React.useState(Boolean(scope?.lensId));
  const [scopeDialogProjectId, setScopeDialogProjectId] = React.useState('');
  const [scopeDialogLensId, setScopeDialogLensId] = React.useState(scope?.lensId || '');
  const [scopeDialogLensName, setScopeDialogLensName] = React.useState('');
  const [scopeDialogProjects, setScopeDialogProjects] = React.useState<Array<{ id: string; displayName?: string }>>([]);
  const [scopeDialogLenses, setScopeDialogLenses] = React.useState<Array<{ id: string; name: string }>>([]);
  const [scopeDialogCreatedLensId, setScopeDialogCreatedLensId] = React.useState<string | null>(null);
  const [zonesEnabled, setZonesEnabled] = React.useState(false);
  const [activeZoneTypes, setActiveZoneTypes] = React.useState<Set<StaticZoneType> | null>(null);
  const [flowsEnabled, setFlowsEnabled] = React.useState(false);
  const [pathLegsEnabled, setPathLegsEnabled] = React.useState(Boolean(scope?.lensId));
  const [pathLegClusteringEnabled, setPathLegClusteringEnabled] = React.useState(Boolean(scope?.lensId));
  const [activeDockPanel, setActiveDockPanel] = React.useState<DockPanel>(null);
  const [filterOverlayMode, setFilterOverlayMode] = React.useState<FilterOverlayMode>('products');
  const [routeSearchQuery, setRouteSearchQuery] = React.useState('');
  const [productSearchQuery, setProductSearchQuery] = React.useState('');
  const [simulationSelection, setSimulationSelection] = React.useState<SimulationMapOverlaySelection | null>(null);
  const [simulationExplanationCache, setSimulationExplanationCache] = React.useState<SimulationExplanationCache>({});
  const [activeProductStages, setActiveProductStages] = React.useState<Set<string> | null>(null);
  const [selectedProductGroupKeys, setSelectedProductGroupKeys] = React.useState<string[]>([]);
  const [selectedRouteArtifactIds, setSelectedRouteArtifactIds] = React.useState<string[]>([]);
  const [focusedRouteArtifactIds, setFocusedRouteArtifactIds] = React.useState<string[]>([]);
  const [hoveredRouteArtifactIds, setHoveredRouteArtifactIds] = React.useState<string[]>([]);
  const [activeFlowModes, setActiveFlowModes] = React.useState<Set<TransportFlowMode> | null>(null);
  const [affectedZoneFreshnessDays, setAffectedZoneFreshnessDays] = React.useState<AffectedZoneFreshnessWindow>(30);
  const [showArchivedAffectedZones, setShowArchivedAffectedZones] = React.useState(false);
  const [vesselsEnabled, setVesselsEnabled] = React.useState(false);
  const [hops, setHops] = React.useState(3);
  const vesselsApiAvailable = isVesselsApiAvailable();
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);
  const [surfaceMode, setSurfaceMode] = React.useState<MapSurfaceMode>('map');
  const [directedExploreSubmitting, setDirectedExploreSubmitting] = React.useState<DirectedExploreDirection | null>(null);
  const [directedExploreError, setDirectedExploreError] = React.useState<string | null>(null);
  const [searchHighlightedObjectId, setSearchHighlightedObjectId] = React.useState<string | null>(null);
  const [hoveredMapObjectId, setHoveredMapObjectId] = React.useState<string | null>(null);
  const [stackSelection, setStackSelection] = React.useState<MapObjectStackSelection | null>(null);
  const [denseCaptionGroups, setDenseCaptionGroups] = React.useState<DenseCaptionOverlayGroup[]>([]);
  const [expandedDenseCaptionGroupId, setExpandedDenseCaptionGroupId] = React.useState<string | null>(null);
  const [suppressedDenseCaptionObjectIds, setSuppressedDenseCaptionObjectIds] = React.useState<string[]>([]);
  const [zoneActivityById, setZoneActivityById] = React.useState<Record<string, ZoneActivityItem>>({});
  const [flowActivityById, setFlowActivityById] = React.useState<Record<string, FlowActivityItem>>({});
  const [pathArtifacts, setPathArtifacts] = React.useState<MaterializedArtifact[]>([]);
  const [pathArtifactsLoaded, setPathArtifactsLoaded] = React.useState(false);
  const realtimeArtifacts = useHorizonRealtimeStore((s) => s.artifacts);
  const identifyJobs = useHorizonRealtimeStore((s) => s.identifyJobs);
  const completeSubstrateJobs = useHorizonRealtimeStore((s) => s.completeSubstrateJobs);
  const recoverMetricsJobs = useHorizonRealtimeStore((s) => s.recoverMetricsJobs);
  const [completeSubstrateSubmitting, setCompleteSubstrateSubmitting] = React.useState(false);
  const [completeSubstrateError, setCompleteSubstrateError] = React.useState<string | null>(null);
  const [completeSubstrateJobId, setCompleteSubstrateJobId] = React.useState<string | null>(null);
  const [dismissedCompleteSubstrateOverlayKey, setDismissedCompleteSubstrateOverlayKey] = React.useState<string | null>(null);
  const [recoverMetricsSubmitting, setRecoverMetricsSubmitting] = React.useState(false);
  const [recoverMetricsError, setRecoverMetricsError] = React.useState<string | null>(null);
  const [recoverMetricsJobId, setRecoverMetricsJobId] = React.useState<string | null>(null);
  const [dismissedRecoverMetricsOverlayKey, setDismissedRecoverMetricsOverlayKey] = React.useState<string | null>(null);
  const [identifySubmitting, setIdentifySubmitting] = React.useState(false);
  const [identifyError, setIdentifyError] = React.useState<string | null>(null);
  const [identifyJobId, setIdentifyJobId] = React.useState<string | null>(null);
  const [dismissedIdentifyOverlayKey, setDismissedIdentifyOverlayKey] = React.useState<string | null>(null);
  const stackSelectionRef = React.useRef<MapObjectStackSelection | null>(null);
  const appliedChatMapFocusRef = React.useRef<string | null>(null);
  const searchInputRef = React.useRef<HTMLInputElement | null>(null);
  const scopeDialogBackdropRef = React.useRef<HTMLDivElement | null>(null);
  const scopeLensRef = React.useRef(scope?.lensId || '');
  const identifyViewIdRef = React.useRef(
    (globalThis.crypto && 'randomUUID' in globalThis.crypto)
      ? (globalThis.crypto as Crypto).randomUUID()
      : `world-map-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  );
  const identifyRefreshKeyRef = React.useRef<string | null>(null);

  React.useEffect(() => {
    scopeLensRef.current = scope?.lensId || '';
  }, [scope?.lensId]);

  React.useEffect(() => {
    stackSelectionRef.current = stackSelection;
  }, [stackSelection]);

  const filters = React.useMemo(
    () => ({ dateFrom, dateTo, activeTypes }),
    [dateFrom, dateTo, activeTypes],
  );

  const mapSearchPaletteChrome = React.useMemo(
    () => buildLensBottomPaletteChrome(isDark ? 'dark' : 'light', searchOpen),
    [isDark, searchOpen],
  );

  const lensPathRefs = React.useMemo(() => collectArtifactPathRefs(pathArtifacts), [pathArtifacts]);
  const preferredGeoObjectPaths = React.useMemo(() => {
    if (!scope?.lensId) return undefined;
    return pathArtifactsLoaded ? lensPathRefs : null;
  }, [lensPathRefs, pathArtifactsLoaded, scope?.lensId]);
  const { allObjects, filteredObjects, typeCounts, loading, error } = useGeoObjects(
    scope,
    filters,
    preferredGeoObjectPaths,
    Boolean(scope?.lensId),
  );
  const showMapPreparingOverlay = !isReady || (loading && allObjects.length === 0);
  const chatFocusPathSet = React.useMemo(
    () => new Set((chatMapFocus?.canonicalPaths || []).map((value) => String(value || '').trim()).filter(Boolean)),
    [chatMapFocus],
  );
  const routeVisibilitySelection = React.useMemo<MapRouteVisibilitySelection>(() => ({
    artifactIds: selectedRouteArtifactIds,
    productRefs: selectedProductGroupKeys,
  }), [selectedProductGroupKeys, selectedRouteArtifactIds]);
  const [simulationStressLegState, setSimulationStressLegState] = React.useState<{
    legIds: string[];
    legCaptionById: Record<string, string>;
  }>({ legIds: [], legCaptionById: {} });
  const simulationEdgeStressSelection = React.useMemo(
    () => ({
      enabled: (simulationSelection?.activeItems?.length || 0) > 0,
      artifactIds: simulationSelection?.matchedArtifactIds || [],
      legIds: simulationStressLegState.legIds,
      legCaptionById: simulationStressLegState.legCaptionById,
    }),
    [simulationSelection, simulationStressLegState],
  );

  const {
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
    setActiveNarrativeKey,
    mapInteractionSpotlightObjectIds,
  } = useGeoEdgeLayer(
    map,
    isReady,
    styleEpoch,
    selectedObject,
    filteredObjects,
    theme,
    hops,
    scope?.lensId,
    routeVisibilitySelection,
    simulationEdgeStressSelection,
    Boolean(simulationEdgeStressSelection.enabled && pathLegsEnabled),
    hoveredMapObjectId,
  );
  const mapObjects = React.useMemo(
    () => (discoveredObjects.length > 0 ? [...filteredObjects, ...discoveredObjects] : filteredObjects),
    [filteredObjects, discoveredObjects],
  );
  const visibleMapObjects = React.useMemo(() => {
    const nowTs = Date.now();
    return mapObjects.filter((obj) =>
      isAffectedZoneRenderable(obj, affectedZoneFreshnessDays, showArchivedAffectedZones, nowTs),
    );
  }, [affectedZoneFreshnessDays, mapObjects, showArchivedAffectedZones]);
  const chatFocusedObjects = React.useMemo(
    () => visibleMapObjects.filter((object) => chatFocusPathSet.has(String(object?.canonicalPath || '').trim())),
    [chatFocusPathSet, visibleMapObjects],
  );
  const chatFocusedObjectIdSet = React.useMemo(
    () => new Set(chatFocusedObjects.map((object) => String(object?.id || '').trim()).filter(Boolean)),
    [chatFocusedObjects],
  );
  const affectedZoneVisibility = React.useMemo(() => {
    const nowTs = Date.now();
    let total = 0;
    let visible = 0;
    let hidden = 0;
    for (const obj of mapObjects) {
      if (obj.type !== 'AFFECTED_ZONE') continue;
      total += 1;
      if (isAffectedZoneRenderable(obj, affectedZoneFreshnessDays, showArchivedAffectedZones, nowTs)) visible += 1;
      else hidden += 1;
    }
    return { total, visible, hidden };
  }, [affectedZoneFreshnessDays, mapObjects, showArchivedAffectedZones]);
  const affectedZoneCutoffLabel = React.useMemo(() => {
    const cutoff = new Date(Date.now() - affectedZoneFreshnessDays * 86_400_000);
    return cutoff.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }, [affectedZoneFreshnessDays]);
  const searchSuggestions = React.useMemo(() => {
    const deduped = new Map<string, GeoObject>();
    for (const obj of visibleMapObjects) {
      const key = obj.id || obj.canonicalPath;
      if (!key || deduped.has(key)) continue;
      deduped.set(key, obj);
    }
    return Array.from(deduped.values())
      .map((obj) => scoreSearchObject(obj, searchQuery))
      .filter((item): item is SearchSuggestion => Boolean(item))
      .sort((a, b) => b.score - a.score || a.title.localeCompare(b.title))
      .slice(0, 8);
  }, [searchQuery, visibleMapObjects]);
  const activeSupplyArtifactIds = React.useMemo(() => {
    if (!activeNarrativeKey || !traceSummary?.supplyGroups?.length) return [] as string[];
    const group = traceSummary.supplyGroups.find((item) => item.narrativeKey === activeNarrativeKey);
    if (!group) return [] as string[];
    return Array.from(new Set(group.paths.map((path) => String(path.id || '').trim()).filter(Boolean)));
  }, [activeNarrativeKey, traceSummary]);
  const supplyNarrativeLegsVisible = activeSupplyArtifactIds.length > 0;
  const routeCatalog = React.useMemo(() => {
    const byKey = new Map<string, MaterializedArtifact>();
    const register = (artifact: MaterializedArtifact | null | undefined) => {
      const key = artifactCatalogKey(artifact);
      if (!key || byKey.has(key)) return;
      byKey.set(key, artifact!);
    };
    for (const artifact of pathArtifacts) register(artifact);
    for (const artifact of relatedPathArtifacts) register(artifact);
    for (const artifact of manualPathArtifacts) register(artifact);
    register(activeManualChain?.artifact as MaterializedArtifact | undefined);
    return Array.from(byKey.values());
  }, [activeManualChain, manualPathArtifacts, pathArtifacts, relatedPathArtifacts]);
  const selectedRouteArtifactIdSet = React.useMemo(
    () => new Set(selectedRouteArtifactIds.map((value) => String(value || '').trim()).filter(Boolean)),
    [selectedRouteArtifactIds],
  );
  const focusedRouteArtifactIdSetBase = React.useMemo(
    () => new Set(focusedRouteArtifactIds.map((value) => String(value || '').trim()).filter(Boolean)),
    [focusedRouteArtifactIds],
  );
  const chatFocusedArtifacts = React.useMemo(
    () => routeCatalog.filter((artifact) => collectArtifactChatFocusRefs(artifact).some((ref) => chatFocusPathSet.has(ref))),
    [chatFocusPathSet, routeCatalog],
  );
  const chatFocusedArtifactIdSet = React.useMemo(
    () => new Set(chatFocusedArtifacts.map((artifact) => String(artifact?.id || '').trim()).filter(Boolean)),
    [chatFocusedArtifacts],
  );
  const chatFocusRouteEmphasisVisible = Boolean(chatMapFocus && chatFocusedArtifactIdSet.size > 0);
  const effectiveFocusedRouteArtifactIdSet = React.useMemo(() => {
    const next = new Set<string>(focusedRouteArtifactIdSetBase);
    for (const artifactId of chatFocusedArtifactIdSet) next.add(artifactId);
    return next;
  }, [chatFocusedArtifactIdSet, focusedRouteArtifactIdSetBase]);
  const hoveredRouteArtifactIdSet = React.useMemo(
    () => new Set(hoveredRouteArtifactIds.map((value) => String(value || '').trim()).filter(Boolean)),
    [hoveredRouteArtifactIds],
  );
  const focusedRouteArtifactIdSet = React.useMemo(() => {
    const next = new Set<string>(effectiveFocusedRouteArtifactIdSet);
    for (const artifactId of hoveredRouteArtifactIdSet) next.add(artifactId);
    return next;
  }, [effectiveFocusedRouteArtifactIdSet, hoveredRouteArtifactIdSet]);
  const focusedRouteArtifacts = React.useMemo(
    () => routeCatalog.filter((artifact) => focusedRouteArtifactIdSet.has(String(artifact?.id || '').trim())),
    [focusedRouteArtifactIdSet, routeCatalog],
  );
  const focusedRouteTerminalPathSet = React.useMemo(
    () => new Set(collectArtifactTerminalPathRefs(focusedRouteArtifacts)),
    [focusedRouteArtifacts],
  );
  const routeEmphasisVisible = focusedRouteArtifactIdSet.size > 0;
  const focusedSupplyArtifactIds = React.useMemo(() => {
    if (focusedRouteArtifactIdSet.size > 0) return Array.from(focusedRouteArtifactIdSet);
    const out = new Set<string>();
    for (const artifactId of activeSupplyArtifactIds) {
      const trimmed = String(artifactId || '').trim();
      if (trimmed) out.add(trimmed);
    }
    const manualArtifactId = String((activeManualChain?.artifact as MaterializedArtifact | undefined)?.id || '').trim();
    if (manualArtifactId) out.add(manualArtifactId);
    if (selectedObject?.id) {
      for (const artifact of relatedPathArtifacts) {
        const artifactId = String(artifact?.id || '').trim();
        if (artifactId) out.add(artifactId);
      }
    }
    return Array.from(out);
  }, [activeManualChain, activeSupplyArtifactIds, focusedRouteArtifactIdSet, relatedPathArtifacts, selectedObject?.id]);
  const activePathArtifacts = React.useMemo(() => {
    if (!pathLegsEnabled && !supplyNarrativeLegsVisible && !routeEmphasisVisible) return [] as MaterializedArtifact[];
    const merged = new Map<string, MaterializedArtifact>();
    const register = (artifact: MaterializedArtifact | null | undefined) => {
      const key = artifactCatalogKey(artifact);
      if (!key) return;
      if (!artifactMatchesRouteVisibility(artifact, routeVisibilitySelection)) return;
      merged.set(key, artifact!);
    };
    if (pathLegsEnabled) {
      const shouldKeepForChatFocus = (artifact: MaterializedArtifact | null | undefined) => {
        if (!chatFocusRouteEmphasisVisible) return true;
        const artifactId = String(artifact?.id || '').trim();
        return artifactId ? chatFocusedArtifactIdSet.has(artifactId) : false;
      };
      for (const artifact of pathArtifacts) {
        if (!shouldKeepForChatFocus(artifact)) continue;
        register(artifact);
      }
      for (const artifact of relatedPathArtifacts) {
        if (!shouldKeepForChatFocus(artifact)) continue;
        register(artifact);
      }
      for (const artifact of manualPathArtifacts) {
        if (!shouldKeepForChatFocus(artifact)) continue;
        register(artifact);
      }
    } else {
      for (const artifact of relatedPathArtifacts) {
        const artifactId = String(artifact?.id || '').trim();
        if (!artifactId || !activeSupplyArtifactIds.includes(artifactId)) continue;
        register(artifact);
      }
    }
    if (routeEmphasisVisible) {
      for (const artifact of routeCatalog) {
        const artifactId = String(artifact?.id || '').trim();
        if (!artifactId || !focusedRouteArtifactIdSet.has(artifactId)) continue;
        register(artifact);
      }
    }
    return Array.from(merged.values());
  }, [activeSupplyArtifactIds, chatFocusRouteEmphasisVisible, chatFocusedArtifactIdSet, manualPathArtifacts, pathArtifacts, pathLegsEnabled, relatedPathArtifacts, routeCatalog, routeEmphasisVisible, routeVisibilitySelection, focusedRouteArtifactIdSet, supplyNarrativeLegsVisible]);
  const clusterProjectionEnabled = pathLegClusteringEnabled && (pathLegsEnabled || supplyNarrativeLegsVisible || routeEmphasisVisible);
  const activePathRefs = React.useMemo(() => collectArtifactPathRefs(activePathArtifacts), [activePathArtifacts]);
  const preferredOverlayObjectPaths = React.useMemo(() => {
    if (!clusterProjectionEnabled) return undefined;
    return activePathRefs.length > 0 ? activePathRefs : null;
  }, [activePathRefs, clusterProjectionEnabled]);
  const { allObjects: overlayPathObjects } = useGeoObjects(
    scope,
    filters,
    preferredOverlayObjectPaths,
    true,
  );
  const overlayPathObjectIds = React.useMemo(
    () => new Set(overlayPathObjects.map((object) => String(object?.id || '').trim()).filter(Boolean)),
    [overlayPathObjects],
  );
  const activePathRefSet = React.useMemo(
    () => new Set(activePathRefs.map((ref) => String(ref || '').trim()).filter(Boolean)),
    [activePathRefs],
  );
  const pathOverlayObjects = React.useMemo(() => {
    const byId = new Map<string, GeoObject>();
    for (const object of [...allObjects, ...overlayPathObjects, ...discoveredObjects]) {
      if (!object?.id || byId.has(object.id)) continue;
      byId.set(object.id, object);
    }
    return Array.from(byId.values());
  }, [allObjects, discoveredObjects, overlayPathObjects]);
  const forcedRouteEndpointObjects = React.useMemo(() => {
    if (!routeEmphasisVisible || focusedRouteTerminalPathSet.size === 0) return [] as GeoObject[];
    const byId = new Map<string, GeoObject>();
    for (const object of pathOverlayObjects) {
      const canonicalPath = String(object?.canonicalPath || '').trim();
      if (!canonicalPath || !focusedRouteTerminalPathSet.has(canonicalPath)) continue;
      const key = String(object?.id || canonicalPath).trim();
      if (!key || byId.has(key)) continue;
      byId.set(key, object);
    }
    return Array.from(byId.values());
  }, [focusedRouteTerminalPathSet, pathOverlayObjects, routeEmphasisVisible]);
  const forcedRouteEndpointObjectIds = React.useMemo(
    () => new Set(forcedRouteEndpointObjects.map((object) => String(object?.id || '').trim()).filter(Boolean)),
    [forcedRouteEndpointObjects],
  );
  /** All leg endpoint path refs (supplier / via / customer) on focused routes — not only terminals. */
  const focusedRouteLegParticipantPathSet = React.useMemo(
    () =>
      new Set(
        collectArtifactPathRefs(focusedRouteArtifacts)
          .map((ref) => String(ref || '').trim())
          .filter(Boolean),
      ),
    [focusedRouteArtifacts],
  );
  const forcedRouteLegParticipantObjectIds = React.useMemo(() => {
    if (!routeEmphasisVisible || focusedRouteLegParticipantPathSet.size === 0) return new Set<string>();
    const ids = new Set<string>();
    const consider = (object: GeoObject | null | undefined) => {
      const cp = String(object?.canonicalPath || '').trim();
      const oid = String(object?.id || '').trim();
      if (!oid || !cp || !focusedRouteLegParticipantPathSet.has(cp)) return;
      ids.add(oid);
    };
    for (const object of pathOverlayObjects) consider(object);
    for (const object of visibleMapObjects) consider(object);
    return ids;
  }, [focusedRouteLegParticipantPathSet, pathOverlayObjects, routeEmphasisVisible, visibleMapObjects]);
  const simulationFocusedPathRefs = React.useMemo(
    () =>
      new Set(
        [
          ...(simulationSelection?.targetRefs || []),
          ...(simulationSelection?.productPaths || []),
        ]
          .map((value) => String(value || '').trim())
          .filter(Boolean),
      ),
    [simulationSelection],
  );
  const simulationFocusedCountryCodes = React.useMemo(
    () =>
      new Set(
        (simulationSelection?.targetCountryCodes || [])
          .map((value) => String(value || '').trim().toLowerCase())
          .filter(Boolean),
      ),
    [simulationSelection],
  );
  const simulationLegFocus = React.useMemo(() => {
    const empty = {
      revealedLegIds: [] as string[],
      visibleObjectPaths: [] as string[],
      visibleObjectIds: [] as string[],
      stressedLegIds: [] as string[],
      legCaptionById: {} as Record<string, string>,
    };
    if ((simulationSelection?.activeItems?.length || 0) === 0) return empty;
    if (activePathArtifacts.length === 0) return empty;
    const revealedLegIds = new Set<string>();
    const visibleObjectPaths = new Set<string>();
    const visibleObjectIds = new Set<string>();
    const stressedLegIds = new Set<string>();
    const legCaptionById: Record<string, string> = {};
    const hypothesisArtifactIdSet = new Set((simulationSelection?.hypothesisArtifactIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const objectIdByPath = new Map<string, string>();
    for (const object of pathOverlayObjects) {
      const canonicalPath = String(object?.canonicalPath || '').trim();
      const objectId = String(object?.id || '').trim();
      if (!canonicalPath || !objectId || objectIdByPath.has(canonicalPath)) continue;
      objectIdByPath.set(canonicalPath, objectId);
    }
    const requireProduct = simulationFocusedPathRefs.size > 0
      && Array.from(simulationFocusedPathRefs).some((ref) => ref.startsWith('/product/'));
    const requireRouteTarget = simulationFocusedPathRefs.size > 0 || simulationFocusedCountryCodes.size > 0;
    for (const artifact of activePathArtifacts) {
      const artifactId = String(artifact?.id || '').trim();
      if (!artifactId) continue;
      const legs = Array.isArray(artifact?.payload?.legs) ? artifact.payload.legs : [];
      const isHypothesisArtifact = hypothesisArtifactIdSet.has(artifactId);
      const matchedLegs: Array<{
        legIndex: number;
        refs: string[];
        overlayTitles: string[];
        supplierRef: string;
        customerRef: string;
      }> = [];
      const legDetails = new Map<number, {
        refs: string[];
        supplierRef: string;
        customerRef: string;
        overlayTitles: string[];
      }>();
      legs.forEach((leg: any, index: number) => {
        const legIndex = resolvedArtifactLegIndex(leg, index);
        const supplierRef = String(leg?.supplierRef || leg?.SupplierRef || leg?.supplier_ref || '').trim();
        const customerRef = String(leg?.customerRef || leg?.CustomerRef || leg?.customer_ref || '').trim();
        const productRefRaw = String(leg?.productRef || leg?.ProductRef || leg?.product_ref || '').trim();
        const transportViaRaw = Array.isArray(leg?.transportViaRefs) ? leg.transportViaRefs
          : Array.isArray(leg?.TransportViaRefs) ? leg.TransportViaRefs
          : Array.isArray(leg?.transport_via_refs) ? leg.transport_via_refs
          : [];
        const flowRefsRaw = Array.isArray(leg?.flowRefs) ? leg.flowRefs
          : Array.isArray(leg?.FlowRefs) ? leg.FlowRefs
          : Array.isArray(leg?.flow_refs) ? leg.flow_refs
          : [];
        const zoneRefsRaw = Array.isArray(leg?.zoneRefs) ? leg.zoneRefs
          : Array.isArray(leg?.ZoneRefs) ? leg.ZoneRefs
          : Array.isArray(leg?.zone_refs) ? leg.zone_refs
          : [];
        const affectedZoneRefsRaw = Array.isArray(leg?.affectedZoneRefs) ? leg.affectedZoneRefs
          : Array.isArray(leg?.AffectedZoneRefs) ? leg.AffectedZoneRefs
          : Array.isArray(leg?.affected_zone_refs) ? leg.affected_zone_refs
          : [];
        const refs = [
          supplierRef,
          customerRef,
          productRefRaw,
          ...(transportViaRaw.map((value: unknown) => String(value || '').trim())),
          ...(flowRefsRaw.map((value: unknown) => String(value || '').trim())),
          ...(zoneRefsRaw.map((value: unknown) => String(value || '').trim())),
          ...(affectedZoneRefsRaw.map((value: unknown) => String(value || '').trim())),
        ].filter(Boolean);
        const productRef = productRefRaw;
        const countries = refs.map(normalizeCountryFromCanonicalPath).filter(Boolean);
        const productHit = productRef ? simulationFocusedPathRefs.has(productRef) : false;
        const refHit = refs.some((ref) => simulationFocusedPathRefs.has(ref));
        const countryHit = countries.some((code) => simulationFocusedCountryCodes.has(code));
        const matches = (requireProduct ? productHit : true) && (requireRouteTarget ? (refHit || countryHit || productHit) : true);
        const overlayTitles = compactNonEmptyStrings(
          (simulationSelection?.activeItems || [])
            .filter((item) => {
              const targetRefs = Array.isArray(item.targetRefs)
                ? item.targetRefs.map((value) => String(value || '').trim()).filter(Boolean)
                : [];
              const targetCountryCode = String(item.targetCountryCode || '').trim().toLowerCase();
              const targetProductPath = String(item.productPath || '').trim();
              const targetRefHit = targetRefs.some((ref) => refs.includes(ref));
              const targetCountryHit = targetCountryCode ? countries.includes(targetCountryCode) : false;
              const targetProductHit = targetProductPath ? productRef === targetProductPath : false;
              return targetRefHit || targetCountryHit || targetProductHit;
            })
            .map((item) => String(item.title || item.id || '').trim()),
        );
        legDetails.set(legIndex, {
          refs,
          supplierRef,
          customerRef,
          overlayTitles,
        });
        if (!matches) return;
        matchedLegs.push({
          legIndex,
          refs,
          overlayTitles,
          supplierRef,
          customerRef,
        });
      });
      if (matchedLegs.length === 0) continue;
      const artifactStressTitles = compactNonEmptyStrings(matchedLegs.flatMap((entry) => entry.overlayTitles)).slice(0, 2);
      const legIndexesToReveal = isHypothesisArtifact
        ? Array.from(new Set(matchedLegs.map((entry) => entry.legIndex))).sort((a, b) => a - b)
        : (() => {
            const minLegIndex = Math.min(...matchedLegs.map((entry) => entry.legIndex));
            const maxLegIndex = Math.max(...matchedLegs.map((entry) => entry.legIndex));
            const out: number[] = [];
            for (let legIndex = minLegIndex; legIndex <= maxLegIndex; legIndex += 1) out.push(legIndex);
            return out;
          })();
      for (const legIndex of legIndexesToReveal) {
        const detail = legDetails.get(legIndex);
        if (!detail) continue;
        const legId = `${artifactId}:leg:${legIndex}`;
        revealedLegIds.add(legId);
        stressedLegIds.add(legId);
        for (const ref of detail.refs) {
          visibleObjectPaths.add(ref);
          const objectId = objectIdByPath.get(ref);
          if (objectId) visibleObjectIds.add(objectId);
        }
        const stressTitle = compactNonEmptyStrings(detail.overlayTitles).slice(0, 2).join(' + ') || artifactStressTitles.join(' + ');
        const sourceLabel = detail.supplierRef ? (humanizeCanonicalPath(detail.supplierRef) || detail.supplierRef) : '';
        const targetLabel = detail.customerRef ? (humanizeCanonicalPath(detail.customerRef) || detail.customerRef) : '';
        const linkLabel = sourceLabel && targetLabel ? `${sourceLabel} -> ${targetLabel}` : sourceLabel || targetLabel;
        const statusLabel = isHypothesisArtifact ? 'Inferred route' : 'Validated path';
        const captionParts = [stressTitle, statusLabel, linkLabel].filter(Boolean);
        if (captionParts.length > 0) legCaptionById[legId] = captionParts.join('\n');
      }
    }
    return {
      revealedLegIds: Array.from(revealedLegIds),
      visibleObjectPaths: Array.from(visibleObjectPaths),
      visibleObjectIds: Array.from(visibleObjectIds),
      stressedLegIds: Array.from(stressedLegIds),
      legCaptionById,
    };
  }, [activePathArtifacts, pathOverlayObjects, simulationFocusedCountryCodes, simulationFocusedPathRefs, simulationSelection]);
  React.useEffect(() => {
    setSimulationStressLegState((prev) => {
      const nextIds = simulationLegFocus.stressedLegIds;
      const nextCaptions = simulationLegFocus.legCaptionById;
      const idsMatch = prev.legIds.length === nextIds.length && prev.legIds.every((id, i) => id === nextIds[i]);
      const captionKeysPrev = Object.keys(prev.legCaptionById);
      const captionKeysNext = Object.keys(nextCaptions);
      const captionsMatch =
        captionKeysPrev.length === captionKeysNext.length
        && captionKeysNext.every((key) => prev.legCaptionById[key] === nextCaptions[key]);
      if (idsMatch && captionsMatch) return prev;
      return { legIds: nextIds, legCaptionById: nextCaptions };
    });
  }, [simulationLegFocus.stressedLegIds, simulationLegFocus.legCaptionById]);
  const simulationFocusedObjectIds = React.useMemo(() => {
    if (simulationLegFocus.visibleObjectIds.length > 0) return simulationLegFocus.visibleObjectIds;
    if ((simulationSelection?.activeItems?.length || 0) === 0 || (simulationFocusedPathRefs.size === 0 && simulationFocusedCountryCodes.size === 0)) return [] as string[];
    const byId = new Set<string>();
    for (const object of pathOverlayObjects) {
      const objectId = String(object?.id || '').trim();
      if (!objectId) continue;
      const canonicalPath = String(object?.canonicalPath || '').trim();
      const countryCode = normalizeCountryFromCanonicalPath(canonicalPath);
      if (
        (canonicalPath && simulationFocusedPathRefs.has(canonicalPath))
        || (countryCode && simulationFocusedCountryCodes.has(countryCode))
      ) {
        byId.add(objectId);
      }
    }
    return Array.from(byId);
  }, [pathOverlayObjects, simulationFocusedCountryCodes, simulationFocusedPathRefs, simulationSelection, simulationLegFocus.visibleObjectIds]);
  const simulationFocusedObjectIdSet = React.useMemo(
    () => new Set(simulationFocusedObjectIds),
    [simulationFocusedObjectIds],
  );
  const simulationStressEnabled = (simulationSelection?.activeItems?.length || 0) > 0;
  const simulationValidatedArtifactIds = React.useMemo(
    () => simulationSelection?.validatedArtifactIds || [],
    [simulationSelection],
  );
  const simulationHypothesisArtifactIds = React.useMemo(
    () => simulationSelection?.hypothesisArtifactIds || [],
    [simulationSelection],
  );
  const primarySimulationOverlay = React.useMemo<HorizonSimulationOverlayItem | null>(() => {
    const items = simulationSelection?.activeItems || [];
    if (items.length === 0) return null;
    return items[items.length - 1] || null;
  }, [simulationSelection]);
  const primarySimulationOverlayId = String(primarySimulationOverlay?.id || '').trim();
  const primarySimulationRunId = String(simulationSelection?.runId || '').trim();
  const primarySimulationLensId = String(simulationSelection?.lensId || scope?.lensId || '').trim();
  const primarySimulationMatchedArtifactIds = React.useMemo(() => {
    if (!primarySimulationOverlayId) return [] as string[];
    return simulationSelection?.matchedArtifactIdsByOverlay?.[primarySimulationOverlayId] || [];
  }, [primarySimulationOverlayId, simulationSelection]);
  const primarySimulationExplanationKey = primarySimulationRunId && primarySimulationOverlayId
    ? `${primarySimulationRunId}:${primarySimulationOverlayId}`
    : '';
  const primarySimulationExplanationState = primarySimulationExplanationKey
    ? (simulationExplanationCache[primarySimulationExplanationKey] || { status: 'idle' })
    : { status: 'idle' as const };
  React.useEffect(() => {
    if (!simulationStressEnabled || !primarySimulationRunId || !primarySimulationOverlayId || !primarySimulationOverlay) return;
    const cacheKey = `${primarySimulationRunId}:${primarySimulationOverlayId}`;
    const current = simulationExplanationCache[cacheKey];
    if (current && (current.status === 'loading' || current.status === 'ready' || current.status === 'error')) return;
    let cancelled = false;
    setSimulationExplanationCache((prev) => ({
      ...prev,
      [cacheKey]: { status: 'loading' },
    }));
    api.explainHorizonSimulationOverlay(primarySimulationRunId, {
      overlayId: primarySimulationOverlayId,
      kind: String(primarySimulationOverlay.kind || '').trim() || undefined,
      matchedArtifactIds: primarySimulationMatchedArtifactIds,
    })
      .then((response) => {
        if (cancelled) return;
        setSimulationExplanationCache((prev) => ({
          ...prev,
          [cacheKey]: {
            status: 'ready',
            data: response.explanation,
          },
        }));
      })
      .catch((err: any) => {
        if (cancelled) return;
        setSimulationExplanationCache((prev) => ({
          ...prev,
          [cacheKey]: {
            status: 'error',
            error: err?.response?.data?.error || err?.message || 'Failed to generate explanation',
          },
        }));
      });
    return () => {
      cancelled = true;
    };
  }, [
    primarySimulationMatchedArtifactIds,
    primarySimulationOverlay,
    primarySimulationOverlayId,
    primarySimulationRunId,
    simulationExplanationCache,
    simulationStressEnabled,
  ]);
  const effectiveMutedObjectIds = React.useMemo(() => {
    if (!routeEmphasisVisible && !chatMapFocus && !selectedObject?.id && !activeNarrativeKey) return mutedObjectIds;
    const edgeFocusedObjectIds = activeNarrativeKey
      ? (mapInteractionSpotlightObjectIds || [])
      : highlightedGatewayObjectIds;
    const protectedIds = new Set<string>([
      ...edgeFocusedObjectIds,
      ...Array.from(forcedRouteEndpointObjectIds),
      ...Array.from(forcedRouteLegParticipantObjectIds),
      ...Array.from(simulationFocusedObjectIdSet),
      ...Array.from(chatFocusedObjectIdSet),
      ...(selectedObject?.id ? [selectedObject.id] : []),
      ...(searchHighlightedObjectId ? [searchHighlightedObjectId] : []),
    ]);
    const next = new Set<string>(mutedObjectIds);
    const muteCandidates = routeEmphasisVisible
      ? [...visibleMapObjects, ...pathOverlayObjects]
      : visibleMapObjects;
    for (const object of muteCandidates) {
      const objectId = String(object?.id || '').trim();
      if (!objectId || protectedIds.has(objectId)) continue;
      next.add(objectId);
    }
    return Array.from(next);
  }, [
    forcedRouteEndpointObjectIds,
    forcedRouteLegParticipantObjectIds,
    chatFocusedObjectIdSet,
    highlightedGatewayObjectIds,
    mapInteractionSpotlightObjectIds,
    mutedObjectIds,
    routeEmphasisVisible,
    activeNarrativeKey,
    chatMapFocus,
    pathOverlayObjects,
    searchHighlightedObjectId,
    selectedObject,
    simulationFocusedObjectIdSet,
    visibleMapObjects,
  ]);
  const effectiveMapInteractionSpotlightObjectIds = React.useMemo(() => {
    const next = new Set<string>((mapInteractionSpotlightObjectIds || []).map((id) => String(id || '').trim()).filter(Boolean));
    if (chatMapFocus) {
      for (const id of chatFocusedObjectIdSet) next.add(id);
      for (const id of forcedRouteEndpointObjectIds) next.add(id);
      for (const id of highlightedGatewayObjectIds) next.add(id);
      if (selectedObject?.id) next.add(String(selectedObject.id).trim());
    }
    return next.size > 0 ? Array.from(next) : null;
  }, [chatFocusedObjectIdSet, chatMapFocus, forcedRouteEndpointObjectIds, highlightedGatewayObjectIds, mapInteractionSpotlightObjectIds, selectedObject?.id]);
  const routeSearchQueryTrim = routeSearchQuery.trim().toLowerCase();
  const filteredRouteCatalog = React.useMemo(() => {
    const items = routeSearchQueryTrim
      ? routeCatalog.filter((artifact) => artifactRouteSearchText(artifact).includes(routeSearchQueryTrim))
      : routeCatalog;
    return [...items].sort((left, right) => artifactRouteTitle(left).localeCompare(artifactRouteTitle(right)));
  }, [routeCatalog, routeSearchQueryTrim]);
  const productSearchQueryTrim = productSearchQuery.trim().toLowerCase();
  const productOverlayData = React.useMemo(() => {
    const groups = new Map<string, ProductRouteGroup>();
    const stageRouteCounts = new Map<string, number>();
    for (const artifact of routeCatalog) {
      const artifactId = String(artifact?.id || '').trim();
      if (!artifactId) continue;
      const artifactStages = new Set<string>();
      const products = artifactRouteProductEntries(artifact);
      const entries = products.length > 0
        ? products
        : [{
          key: 'unspecified-product',
          label: 'Unspecified product',
          canonicalPath: undefined,
          stage: 'other',
          searchText: 'unspecified product other',
        } satisfies ProductRouteEntry];
      for (const entry of entries) {
        const key = String(entry.key || '').trim().toLowerCase() || 'unspecified-product';
        if (!groups.has(key)) {
          const stage = String(entry.stage || 'other').trim().toLowerCase() || 'other';
          groups.set(key, {
            key,
            label: entry.label,
            canonicalPath: entry.canonicalPath,
            stage,
            stageLabel: productStageLabel(stage),
            searchText: [
              entry.searchText,
              entry.canonicalPath,
            ]
              .map((part) => String(part || '').trim().toLowerCase())
              .filter(Boolean)
              .join(' '),
            artifactIds: [],
            routeCount: 0,
            pinnedCount: 0,
            topRoutes: [],
          });
        }
        const group = groups.get(key)!;
        group.artifactIds.push(artifactId);
        group.routeCount += 1;
        if (artifact.isPinned) group.pinnedCount += 1;
        if (group.topRoutes.length < 3) group.topRoutes.push(artifactRouteTitle(artifact));
        artifactStages.add(group.stage);
      }
      for (const stage of artifactStages) {
        stageRouteCounts.set(stage, (stageRouteCounts.get(stage) || 0) + 1);
      }
    }
    const allGroups = Array.from(groups.values())
      .map((group) => ({
        ...group,
        artifactIds: Array.from(new Set(group.artifactIds)),
        topRoutes: Array.from(new Set(group.topRoutes)).slice(0, 3),
      }))
      .sort((left, right) =>
        productStageRank(left.stage) - productStageRank(right.stage)
        || right.routeCount - left.routeCount
        || left.label.localeCompare(right.label));
    const stageFacets: ProductStageFacet[] = Array.from(stageRouteCounts.entries())
      .map(([key, routeCount]) => ({
        key,
        label: productStageLabel(key),
        routeCount,
      }))
      .sort((left, right) =>
        productStageRank(left.key) - productStageRank(right.key)
        || right.routeCount - left.routeCount
        || left.label.localeCompare(right.label));
    const groupsByStage = activeProductStages && activeProductStages.size > 0
      ? allGroups.filter((group) => activeProductStages.has(group.stage))
      : allGroups;
    const visibleGroups = productSearchQueryTrim
      ? groupsByStage.filter((group) => group.searchText.includes(productSearchQueryTrim))
      : groupsByStage;
    const allGroupsByKey = new Map<string, ProductRouteGroup>();
    for (const group of allGroups) {
      allGroupsByKey.set(String(group.key || '').trim().toLowerCase(), group);
    }
    return { stageFacets, visibleGroups, allGroupsByKey };
  }, [activeProductStages, productSearchQueryTrim, routeCatalog]);
  const productStageFacets = productOverlayData.stageFacets;
  const productRouteGroups = productOverlayData.visibleGroups;
  const productRouteGroupsByKey = productOverlayData.allGroupsByKey;
  const dependencyTreeAvailable = Boolean(
    selectedObject
    && !edgesLoading
    && (
      (selectionExportData?.directRelations?.length || 0) > 0
      || (selectionExportData?.contextRelations?.length || 0) > 0
      || (traceSummary?.propagationEdgeCount || 0) > 0
      || (traceSummary?.supplyPathCount || 0) > 0
    ),
  );
  const viewOverlayOpen = activeMapDrawerPanel === 'view';
  const simulationOverlayOpen = activeMapDrawerPanel === 'simulation';
  const exportToolsOverlayOpen = activeMapDrawerPanel === 'export-tools';
  const filterOverlayOpen = activeDockPanel === 'filter';
  const routesOverlayOpen = filterOverlayOpen && filterOverlayMode === 'routes';
  const productsOverlayOpen = filterOverlayOpen && filterOverlayMode === 'products';
  const allStaticZones = STATIC_ZONE_DATA.features;
  const zoneTypeCounts = React.useMemo(() => deriveZoneTypeCounts(allStaticZones), [allStaticZones]);
  const filteredStaticZones = React.useMemo(() => {
    if (!zonesEnabled) return [] as StaticZoneFeature[];
    if (!activeZoneTypes) return allStaticZones;
    return allStaticZones.filter((f) => activeZoneTypes.has(f.properties.zoneType));
  }, [zonesEnabled, activeZoneTypes, allStaticZones]);
  const allTransportFlows = STATIC_TRANSPORT_FLOW_DATA.features;
  const flowModeCounts = React.useMemo(() => deriveTransportFlowModeCounts(allTransportFlows), [allTransportFlows]);
  const filteredTransportFlows = React.useMemo(() => {
    if (!flowsEnabled) return [] as TransportFlowFeature[];
    if (!activeFlowModes) return allTransportFlows;
    return allTransportFlows.filter((f) => activeFlowModes.has(f.properties.flowMode));
  }, [flowsEnabled, activeFlowModes, allTransportFlows]);
  const exportBaseName = React.useMemo(
    () => slugifyDownloadPart(selectedObject ? getSearchObjectTitle(selectedObject) : 'world-map', 'world-map'),
    [selectedObject],
  );
  const latestIdentifyJob = React.useMemo(() => {
    const jobs = Object.values(identifyJobs || {}).filter(
      (job) => String(job?.lensId || '').trim() === String(scope?.lensId || '').trim(),
    );
    if (jobs.length === 0) return null;
    jobs.sort((left, right) => (right.updatedAt || 0) - (left.updatedAt || 0));
    if (identifyJobId) {
      const exact = jobs.find((job) => String(job.jobId || '').trim() === identifyJobId);
      if (exact) return exact;
    }
    return jobs[0] || null;
  }, [identifyJobId, identifyJobs, scope?.lensId]);
  const latestCompleteSubstrateJob = React.useMemo(() => {
    const jobs = Object.values(completeSubstrateJobs || {}).filter(
      (job) => String(job?.lensId || '').trim() === String(scope?.lensId || '').trim(),
    );
    if (jobs.length === 0) return null;
    jobs.sort((left, right) => (right.updatedAt || 0) - (left.updatedAt || 0));
    if (completeSubstrateJobId) {
      const exact = jobs.find((job) => String(job.jobId || '').trim() === completeSubstrateJobId);
      if (exact) return exact;
    }
    return jobs[0] || null;
  }, [completeSubstrateJobId, completeSubstrateJobs, scope?.lensId]);
  const latestRecoverMetricsJob = React.useMemo(() => {
    const jobs = Object.values(recoverMetricsJobs || {}).filter(
      (job) => String(job?.lensId || '').trim() === String(scope?.lensId || '').trim(),
    );
    if (jobs.length === 0) return null;
    jobs.sort((left, right) => (right.updatedAt || 0) - (left.updatedAt || 0));
    if (recoverMetricsJobId) {
      const exact = jobs.find((job) => String(job.jobId || '').trim() === recoverMetricsJobId);
      if (exact) return exact;
    }
    return jobs[0] || null;
  }, [recoverMetricsJobId, recoverMetricsJobs, scope?.lensId]);
  const completeSubstrateRunning = latestCompleteSubstrateJob?.state === 'started'
    || latestCompleteSubstrateJob?.state === 'running';
  const recoverMetricsRunning = latestRecoverMetricsJob?.state === 'started'
    || latestRecoverMetricsJob?.state === 'running';
  const identifyRunning = latestIdentifyJob?.state === 'started'
    || latestIdentifyJob?.state === 'running'
    || latestIdentifyJob?.state === 'propagating';
  const identifyScopeArtifacts = React.useMemo(
    () => (activePathArtifacts.length > 0 ? activePathArtifacts : routeCatalog),
    [activePathArtifacts, routeCatalog],
  );
  const identifyVisibleNodeRefs = React.useMemo(() => {
    const refs = new Set<string>();
    const push = (value: string | null | undefined) => {
      const ref = String(value || '').trim();
      if (ref) refs.add(ref);
    };
    push(selectedObject?.canonicalPath);
    for (const object of visibleMapObjects) push(object?.canonicalPath);
    return Array.from(refs);
  }, [selectedObject?.canonicalPath, visibleMapObjects]);
  const identifyVisibleArtifactKeys = React.useMemo(
    () => collectArtifactKeys(identifyScopeArtifacts),
    [identifyScopeArtifacts],
  );
  const identifyVisibleSupplyLinkRefs = React.useMemo(() => {
    const refs = new Set<string>();
    for (const ref of collectArtifactSupplyLinkRefs(identifyScopeArtifacts)) refs.add(ref);
    const selectedPath = String(selectedObject?.canonicalPath || '').trim();
    if (selectedPath.startsWith('/supply-link/')) refs.add(selectedPath);
    return Array.from(refs);
  }, [identifyScopeArtifacts, selectedObject?.canonicalPath]);
  const identifyVisibleRelations = React.useMemo(
    () => collectVisibleGraphRelations(selectionExportData, mapObjects, selectedObject),
    [mapObjects, selectedObject, selectionExportData],
  );
  const toolScopePayload = React.useMemo(() => ({
    scopeMode: 'visible_set' as const,
    selectedCanonicalPath: String(selectedObject?.canonicalPath || '').trim() || undefined,
    visibleNodeRefs: identifyVisibleNodeRefs,
    visibleArtifactKeys: identifyVisibleArtifactKeys,
    visibleSupplyLinkRefs: identifyVisibleSupplyLinkRefs,
    visibleRelations: identifyVisibleRelations,
    clientViewId: identifyViewIdRef.current,
    maxNodes: 40,
    maxArtifacts: 25,
  }), [
    identifyVisibleArtifactKeys,
    identifyVisibleNodeRefs,
    identifyVisibleRelations,
    identifyVisibleSupplyLinkRefs,
    selectedObject?.canonicalPath,
  ]);
  const identifyTouchedRouteLabels = React.useMemo(() => {
    const routeByKey = new Map<string, MaterializedArtifact>();
    for (const artifact of routeCatalog) {
      const keys = [String(artifact?.canonicalKey || '').trim(), String(artifact?.id || '').trim()].filter(Boolean);
      for (const key of keys) {
        if (!routeByKey.has(key)) routeByKey.set(key, artifact);
      }
    }
    return (latestIdentifyJob?.touchedArtifactKeys || [])
      .map((key) => {
        const artifact = routeByKey.get(String(key || '').trim());
        return artifact ? artifactRouteTitle(artifact) : (humanizeCanonicalPath(String(key || '').trim()) || String(key || '').trim());
      })
      .filter(Boolean)
      .slice(0, 6);
  }, [latestIdentifyJob?.touchedArtifactKeys, routeCatalog]);
  const identifyTouchedSupplyLinkLabels = React.useMemo(
    () => (latestIdentifyJob?.touchedSupplyLinks || [])
      .map((ref) => humanizeCanonicalPath(String(ref || '').trim()) || String(ref || '').trim())
      .filter(Boolean)
      .slice(0, 6),
    [latestIdentifyJob?.touchedSupplyLinks],
  );
  const identifyTouchedNodeLabels = React.useMemo(
    () => (latestIdentifyJob?.touchedNodeRefs || [])
      .map((ref) => humanizeCanonicalPath(String(ref || '').trim()) || String(ref || '').trim())
      .filter(Boolean)
      .slice(0, 6),
    [latestIdentifyJob?.touchedNodeRefs],
  );
  const identifyOverlayKey = React.useMemo(() => {
    const jobId = String(latestIdentifyJob?.jobId || '').trim();
    if (jobId) return `job:${jobId}`;
    const localError = String(identifyError || '').trim();
    if (localError) return `error:${localError}`;
    return '';
  }, [identifyError, latestIdentifyJob?.jobId]);
  const identifyOverlayVisible = Boolean(identifyOverlayKey) && dismissedIdentifyOverlayKey !== identifyOverlayKey;
  const completeSubstrateOverlayKey = React.useMemo(() => {
    const jobId = String(latestCompleteSubstrateJob?.jobId || '').trim();
    if (jobId) return `job:${jobId}`;
    const localError = String(completeSubstrateError || '').trim();
    if (localError) return `error:${localError}`;
    return '';
  }, [completeSubstrateError, latestCompleteSubstrateJob?.jobId]);
  const completeSubstrateOverlayVisible = Boolean(completeSubstrateOverlayKey)
    && dismissedCompleteSubstrateOverlayKey !== completeSubstrateOverlayKey;
  const recoverMetricsOverlayKey = React.useMemo(() => {
    const jobId = String(latestRecoverMetricsJob?.jobId || '').trim();
    if (jobId) return `job:${jobId}`;
    const localError = String(recoverMetricsError || '').trim();
    if (localError) return `error:${localError}`;
    return '';
  }, [latestRecoverMetricsJob?.jobId, recoverMetricsError]);
  const recoverMetricsOverlayVisible = Boolean(recoverMetricsOverlayKey)
    && dismissedRecoverMetricsOverlayKey !== recoverMetricsOverlayKey;
  const identifyErrorLines = React.useMemo(() => {
    const raw = [
      ...(Array.isArray(latestIdentifyJob?.errors) ? latestIdentifyJob.errors : []),
      latestIdentifyJob?.error,
      identifyError,
    ];
    return Array.from(new Set(raw.map((value) => String(value || '').trim()).filter(Boolean))).slice(0, 4);
  }, [identifyError, latestIdentifyJob]);
  const completeSubstrateErrorLines = React.useMemo(() => {
    const raw = [
      latestCompleteSubstrateJob?.error,
      completeSubstrateError,
    ];
    return Array.from(new Set(raw.map((value) => String(value || '').trim()).filter(Boolean))).slice(0, 4);
  }, [completeSubstrateError, latestCompleteSubstrateJob]);
  const recoverMetricsErrorLines = React.useMemo(() => {
    const raw = [
      latestRecoverMetricsJob?.error,
      recoverMetricsError,
    ];
    return Array.from(new Set(raw.map((value) => String(value || '').trim()).filter(Boolean))).slice(0, 4);
  }, [latestRecoverMetricsJob, recoverMetricsError]);
  const reloadSupplyChains = React.useCallback(async () => {
    const requestedLensId = String(scope?.lensId || '').trim();
    if (!requestedLensId) return;
    try {
      const payload = await api.getSupplyChainsByLens(requestedLensId);
      if (scopeLensRef.current !== requestedLensId) return;
      const nextArtifacts = Array.isArray(payload?.chains) ? (payload.chains as MaterializedArtifact[]) : [];
      setPathArtifacts((current) => (sameArtifactRefreshSet(current, nextArtifacts) ? current : nextArtifacts));
      setPathArtifactsLoaded(true);
    } catch {
      if (scopeLensRef.current !== requestedLensId) return;
      setPathArtifacts((current) => (current.length === 0 ? current : []));
      setPathArtifactsLoaded(true);
    }
  }, [scope?.lensId]);

  const handleExportJson = React.useCallback(() => {
    if (!selectedObject || !selectionExportData) return;
    const exportedAt = new Date().toISOString();
    const stamp = exportedAt.replace(/[:.]/g, '-');
    const selectedSelectionPayload = {
      kind: 'world-map-selection',
      scope: scope || null,
      theme,
      ...selectionExportData,
    };
    const loadedGraphPayload = {
      kind: 'world-map-loaded-graph',
      exportedAt,
      scope: scope || null,
      theme,
      selectedObjectId: selectedObject.id,
      filters: {
        dateFrom,
        dateTo,
        activeTypes: activeTypes ? Array.from(activeTypes) : null,
        affectedZoneFreshnessDays,
        showArchivedAffectedZones,
        zonesEnabled,
        activeZoneTypes: activeZoneTypes ? Array.from(activeZoneTypes) : null,
        flowsEnabled,
        activeFlowModes: activeFlowModes ? Array.from(activeFlowModes) : null,
        pathLegsEnabled,
        pathLegClusteringEnabled,
        vesselsEnabled,
        hops,
      },
      objects: {
        all: allObjects,
        filteredIds: filteredObjects.map((obj) => obj.id),
        visibleIds: visibleMapObjects.map((obj) => obj.id),
        discoveredIds: discoveredObjects.map((obj) => obj.id),
      },
      pathArtifacts: {
        lens: pathArtifacts,
        related: relatedPathArtifacts,
        manual: manualPathArtifacts,
        activeOverlay: activePathArtifacts,
      },
      selectionGraph: selectionExportData,
      overlays: {
        mutedObjectIds: effectiveMutedObjectIds,
        highlightedGatewayObjectIds,
      },
    };

    downloadJsonFile(`${stamp}-${exportBaseName}-selection.json`, selectedSelectionPayload);
    window.requestAnimationFrame(() => {
      downloadJsonFile(`${stamp}-${exportBaseName}-loaded-graph.json`, loadedGraphPayload);
    });
  }, [
    activeFlowModes,
    activePathArtifacts,
    activeTypes,
    activeZoneTypes,
    affectedZoneFreshnessDays,
    allObjects,
    dateFrom,
    dateTo,
    discoveredObjects,
    exportBaseName,
    filteredObjects,
    flowsEnabled,
    highlightedGatewayObjectIds,
    hops,
    manualPathArtifacts,
    effectiveMutedObjectIds,
    pathArtifacts,
    pathLegClusteringEnabled,
    pathLegsEnabled,
    relatedPathArtifacts,
    scope,
    selectedObject,
    selectionExportData,
    showArchivedAffectedZones,
    theme,
    traceSummary,
    vesselsEnabled,
    visibleMapObjects,
    zonesEnabled,
  ]);

  React.useEffect(() => {
    if (dependencyTreeAvailable) return;
    setSurfaceMode('map');
  }, [dependencyTreeAvailable]);

  React.useEffect(() => {
    if (surfaceMode !== 'map' || !map) return;
    window.requestAnimationFrame(() => {
      try {
        map.resize();
      } catch {
        /* keep surface switching resilient */
      }
    });
  }, [map, surfaceMode]);

  React.useEffect(() => {
    setPathLegsEnabled(Boolean(scope?.lensId));
    setPathLegClusteringEnabled(Boolean(scope?.lensId));
    setCompleteSubstrateError(null);
    setCompleteSubstrateJobId(null);
    setRecoverMetricsError(null);
    setRecoverMetricsJobId(null);
    setIdentifyError(null);
    setIdentifyJobId(null);
    if (scope?.lensId) {
      setPathArtifactsLoaded(false);
      return;
    }
    setPathArtifacts([]);
    setPathArtifactsLoaded(true);
  }, [scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId) return;
    let cancelled = false;
    const load = async () => {
      if (cancelled) return;
      await reloadSupplyChains();
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [reloadSupplyChains, scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId) return;
    const items = Object.values(realtimeArtifacts || {});
    if (items.length === 0) return;
    setPathArtifacts((prev) => {
      const next = new Map<string, MaterializedArtifact>();
      for (const artifact of prev) {
        const key = String(artifact?.id || artifact?.canonicalKey || '').trim();
        if (!key) continue;
        next.set(key, artifact);
      }
      for (const raw of items) {
        const key = String(raw?.id || raw?.canonicalKey || '').trim();
        if (!key) continue;
        const current = next.get(key);
        const candidate: MaterializedArtifact = {
          ...(current || { id: key, kind: 'SUPPLY_CHAIN_PATH' }),
          id: key,
          lensId: raw?.lensId || current?.lensId,
          pathStatus: raw?.pathStatus || current?.pathStatus,
          canonicalKey: raw?.canonicalKey || current?.canonicalKey,
          displayName: raw?.displayName || current?.displayName,
          anchorSupplier: raw?.anchorSupplier || current?.anchorSupplier,
          anchorCustomer: raw?.anchorCustomer || current?.anchorCustomer,
          productAxis: raw?.productAxis || current?.productAxis,
          nodeRefs: raw?.nodeRefs || current?.nodeRefs,
          flowRefs: raw?.flowRefs || current?.flowRefs,
          zoneRefs: raw?.zoneRefs || current?.zoneRefs,
          affectedZoneRefs: raw?.affectedZoneRefs || current?.affectedZoneRefs,
          legCount: typeof raw?.legCount === 'number' ? raw.legCount : current?.legCount,
          bindingScore: typeof raw?.bindingScore === 'number' ? raw.bindingScore : current?.bindingScore,
          pathStressScore: typeof raw?.pathStressScore === 'number' ? raw.pathStressScore : current?.pathStressScore,
          riskOverlay: raw?.riskOverlay || current?.riskOverlay,
          payload: raw?.payload || current?.payload,
        };
        next.set(key, candidate);
      }
      const merged = Array.from(next.values());
      return sameArtifactRefreshSet(prev, merged) ? prev : merged;
    });
  }, [realtimeArtifacts, scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId || !latestCompleteSubstrateJob?.jobId) return;
    const state = String(latestCompleteSubstrateJob.state || '').trim();
    if (state !== 'completed' && state !== 'failed' && !latestCompleteSubstrateJob.requiresRefetch) return;
    const refreshKey = [
      latestCompleteSubstrateJob.jobId,
      state,
      String(latestCompleteSubstrateJob.updatedAt || ''),
      latestCompleteSubstrateJob.requiresRefetch ? 'refetch' : 'steady',
    ].join(':');
    if (identifyRefreshKeyRef.current === `substrate:${refreshKey}`) return;
    identifyRefreshKeyRef.current = `substrate:${refreshKey}`;
    if (latestCompleteSubstrateJob.requiresRefetch || state === 'completed') {
      void reloadSupplyChains();
    }
  }, [latestCompleteSubstrateJob, reloadSupplyChains, scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId || !latestRecoverMetricsJob?.jobId) return;
    const state = String(latestRecoverMetricsJob.state || '').trim();
    if (state !== 'completed' && state !== 'failed' && !latestRecoverMetricsJob.requiresRefetch) return;
    const refreshKey = [
      latestRecoverMetricsJob.jobId,
      state,
      String(latestRecoverMetricsJob.updatedAt || ''),
      latestRecoverMetricsJob.requiresRefetch ? 'refetch' : 'steady',
    ].join(':');
    if (identifyRefreshKeyRef.current === `metrics:${refreshKey}`) return;
    identifyRefreshKeyRef.current = `metrics:${refreshKey}`;
    if (latestRecoverMetricsJob.requiresRefetch || state === 'completed') {
      void reloadSupplyChains();
    }
  }, [latestRecoverMetricsJob, reloadSupplyChains, scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId || !latestIdentifyJob?.jobId) return;
    const state = String(latestIdentifyJob.state || '').trim();
    if (state !== 'completed' && state !== 'failed' && state !== 'partial_success' && !latestIdentifyJob.requiresRefetch) return;
    const refreshKey = [
      latestIdentifyJob.jobId,
      state,
      String(latestIdentifyJob.updatedAt || ''),
      latestIdentifyJob.requiresRefetch ? 'refetch' : 'steady',
    ].join(':');
    if (identifyRefreshKeyRef.current === refreshKey) return;
    identifyRefreshKeyRef.current = refreshKey;
    if (latestIdentifyJob.requiresRefetch || state === 'completed' || state === 'partial_success') {
      void reloadSupplyChains();
    }
  }, [latestIdentifyJob, reloadSupplyChains, scope?.lensId]);

  React.useEffect(() => {
    const availableIds = new Set(routeCatalog.map((artifact) => String(artifact.id || '').trim()).filter(Boolean));
    setSelectedRouteArtifactIds((current) => {
      const next = current.filter((id) => availableIds.has(String(id || '').trim()));
      return next.length === current.length ? current : next;
    });
  }, [routeCatalog]);

  React.useEffect(() => {
    const availableGroupKeys = new Set(Array.from(productRouteGroupsByKey.keys()));
    setSelectedProductGroupKeys((current) => {
      const next = current.filter((key) => availableGroupKeys.has(String(key || '').trim().toLowerCase()));
      return next.length === current.length ? current : next;
    });
  }, [productRouteGroupsByKey]);

  const resetProductsPanelState = React.useCallback(() => {
    setSelectedProductGroupKeys([]);
    setSelectedRouteArtifactIds([]);
    setActiveProductStages(null);
    setProductSearchQuery('');
  }, []);

  const selectedProductGroupKeySet = React.useMemo(
    () => new Set(selectedProductGroupKeys.map((value) => String(value || '').trim().toLowerCase()).filter(Boolean)),
    [selectedProductGroupKeys],
  );
  const routeFilterActive = selectedRouteArtifactIdSet.size > 0 || selectedProductGroupKeySet.size > 0;

  React.useEffect(() => {
    if (simulationOverlayOpen) return;
    setFocusedRouteArtifactIds([]);
    setSimulationSelection(null);
  }, [simulationOverlayOpen]);

  React.useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const payload = await api.getHorizonMapActivity({ lensId: scope?.lensId });
        if (cancelled) return;
        const z: Record<string, ZoneActivityItem> = {};
        const f: Record<string, FlowActivityItem> = {};
        for (const item of payload?.zones || []) {
          if (item?.id) z[String(item.id)] = item;
        }
        for (const item of payload?.flows || []) {
          if (item?.id) f[String(item.id)] = item;
        }
        setZoneActivityById(z);
        setFlowActivityById(f);
      } catch {
        if (!cancelled) {
          setZoneActivityById({});
          setFlowActivityById({});
        }
      }
    };
    void load();
    const timer = window.setInterval(() => { void load(); }, 60_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [scope?.lensId]);

  const clearStackSelection = React.useCallback(() => {
    stackSelectionRef.current?.clearTransient?.();
    stackSelectionRef.current = null;
    setStackSelection(null);
  }, []);

  React.useEffect(() => () => {
    stackSelectionRef.current?.clearTransient?.();
  }, []);

  const selectObjectFromMapInteraction = React.useCallback((object: GeoObject | null) => {
    clearStackSelection();
    setFocusedRouteArtifactIds([]);
    setHoveredRouteArtifactIds([]);
    setActiveNarrativeKey(null);
    setSearchHighlightedObjectId(null);
    setSelectedObject(object);
  }, [clearStackSelection, setActiveNarrativeKey]);

  const handleMarkerSelection = React.useCallback((selection: GeoMarkerSelection) => {
    if (selection.kind === 'clear') {
      selectObjectFromMapInteraction(null);
      return;
    }

    if (selection.kind === 'object') {
      selectObjectFromMapInteraction(selection.object);
      return;
    }

    clearStackSelection();
    setFocusedRouteArtifactIds([]);
    setHoveredRouteArtifactIds([]);
    setActiveNarrativeKey(null);
    setSearchHighlightedObjectId(null);
    setSelectedObject(null);
    setStackSelection(selection);
  }, [clearStackSelection, selectObjectFromMapInteraction, setActiveNarrativeKey]);

  React.useEffect(() => {
    if (!selectedObject) return;
    if (visibleMapObjects.some((obj) => obj.id === selectedObject.id)) return;
    setSearchHighlightedObjectId((current) => (current === selectedObject.id ? null : current));
    setSelectedObject(null);
  }, [selectedObject, visibleMapObjects]);

  React.useEffect(() => {
    setDirectedExploreSubmitting(null);
    setDirectedExploreError(null);
    setHoveredRouteArtifactIds([]);
  }, [selectedObject?.id]);

  React.useEffect(() => {
    if (!searchOpen) return;
    const timer = window.setTimeout(() => searchInputRef.current?.focus(), 0);
    return () => window.clearTimeout(timer);
  }, [searchOpen]);

  React.useEffect(() => {
    if (!expandedDenseCaptionGroupId) return;
    if (denseCaptionGroups.some((group) => group.id === expandedDenseCaptionGroupId)) return;
    setExpandedDenseCaptionGroupId(null);
  }, [denseCaptionGroups, expandedDenseCaptionGroupId]);

  React.useEffect(() => {
    if (!expandedDenseCaptionGroupId) return;
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest('[data-dense-caption-overlay="true"]')) return;
      setExpandedDenseCaptionGroupId(null);
    };
    document.addEventListener('pointerdown', handlePointerDown, true);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
    };
  }, [expandedDenseCaptionGroupId]);

  React.useEffect(() => {
    setSearchActiveIndex(0);
  }, [searchMode, searchQuery, searchOpen, supplySearchResults]);

  React.useEffect(() => {
    if (searchMode !== 'supply-links') {
      setShowAnchorASuggestions(false);
      setShowAnchorBSuggestions(false);
      return;
    }
    const query = supplyAnchorAQuery.trim();
    if (!query || supplyAnchorA?.name === query || supplyAnchorA?.canonicalPath === query) {
      setAnchorASuggestions([]);
      setAnchorASuggestionsLoading(false);
      return;
    }
    let cancelled = false;
    setAnchorASuggestionsLoading(true);
    const timer = window.setTimeout(async () => {
      try {
        const response = await api.searchHorizonEntities(query, 6);
        if (cancelled) return;
        setAnchorASuggestions(normalizeSupplyAnchorEntities(Array.isArray(response?.entities) ? response.entities : []));
      } catch {
        if (!cancelled) setAnchorASuggestions([]);
      } finally {
        if (!cancelled) setAnchorASuggestionsLoading(false);
      }
    }, 180);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [searchMode, supplyAnchorA, supplyAnchorAQuery]);

  React.useEffect(() => {
    if (searchMode !== 'supply-links' || !betweenAnchors) {
      setShowAnchorBSuggestions(false);
      return;
    }
    const query = supplyAnchorBQuery.trim();
    if (!query || supplyAnchorB?.name === query || supplyAnchorB?.canonicalPath === query) {
      setAnchorBSuggestions([]);
      setAnchorBSuggestionsLoading(false);
      return;
    }
    let cancelled = false;
    setAnchorBSuggestionsLoading(true);
    const timer = window.setTimeout(async () => {
      try {
        const response = await api.searchHorizonEntities(query, 6);
        if (cancelled) return;
        setAnchorBSuggestions(normalizeSupplyAnchorEntities(Array.isArray(response?.entities) ? response.entities : []));
      } catch {
        if (!cancelled) setAnchorBSuggestions([]);
      } finally {
        if (!cancelled) setAnchorBSuggestionsLoading(false);
      }
    }, 180);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [betweenAnchors, searchMode, supplyAnchorB, supplyAnchorBQuery]);

  React.useEffect(() => {
    if (!scopeDialogOpen) return;
    let cancelled = false;
    const loadProjects = async () => {
      try {
        const projects = await api.listProjects('horizon');
        if (cancelled) return;
        setScopeDialogProjects(projects.map((project) => ({ id: project.id, displayName: project.displayName })));
        if (!scope?.lensId && !scopeDialogProjectId && projects[0]?.id) {
          setScopeDialogProjectId(projects[0].id);
        }
      } catch (error: any) {
        if (!cancelled) setScopeDialogError(error?.message || 'Failed to load Horizon projects.');
      }
    };
    void loadProjects();
    return () => {
      cancelled = true;
    };
  }, [scope?.lensId, scopeDialogOpen, scopeDialogProjectId]);

  React.useEffect(() => {
    if (!scopeDialogOpen || scopeDialogTargetMode !== 'existing' || (scope?.lensId && scopeDialogUseCurrentLens)) return;
    const projectId = scopeDialogProjectId.trim();
    if (!projectId) {
      setScopeDialogLenses([]);
      return;
    }
    let cancelled = false;
    const loadLenses = async () => {
      try {
        const response = await api.listHorizonLenses(projectId, { limit: 100 });
        if (cancelled) return;
        setScopeDialogLenses(Array.isArray(response?.lenses) ? response.lenses.map((lens) => ({ id: lens.id, name: lens.name })) : []);
      } catch (error: any) {
        if (!cancelled) setScopeDialogError(error?.message || 'Failed to load lenses.');
      }
    };
    void loadLenses();
    return () => {
      cancelled = true;
    };
  }, [scope?.lensId, scopeDialogOpen, scopeDialogProjectId, scopeDialogTargetMode, scopeDialogUseCurrentLens]);

  const focusObjectOnMap = React.useCallback((object: GeoObject) => {
    const lat = object.coordinates?.lat;
    const lon = object.coordinates?.lon;
    if (!map || !Number.isFinite(lat) || !Number.isFinite(lon)) return;

    const radiusKm = Number(object.radiusKm);
    if (Number.isFinite(radiusKm) && radiusKm > 8) {
      const kmPerDegLat = 111.32;
      const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat! * Math.PI) / 180));
      const padKm = Math.min(radiusKm * 1.1, 900);
      const dLat = padKm / kmPerDegLat;
      const dLon = padKm / kmPerDegLon;
      map.fitBounds(
        [
          [lon! - dLon, lat! - dLat],
          [lon! + dLon, lat! + dLat],
        ],
        { padding: { top: 120, right: 120, bottom: 120, left: 120 }, duration: 900, essential: true },
      );
    } else {
      map.flyTo({
        center: [lon!, lat!],
        zoom: Math.max(map.getZoom(), 7.5),
        speed: 1,
        curve: 1.25,
        essential: true,
      });
    }

    selectObjectFromMapInteraction(object);
    setSearchHighlightedObjectId(object.id);
    setSearchOpen(false);
    setSearchQuery('');
  }, [map, selectObjectFromMapInteraction]);

  const focusObjectsOnMap = React.useCallback((objects: GeoObject[]) => {
    const focusable = objects.filter((object) =>
      Number.isFinite(object?.coordinates?.lat) && Number.isFinite(object?.coordinates?.lon),
    );
    if (!map || focusable.length === 0) return false;
    if (focusable.length === 1) {
      focusObjectOnMap(focusable[0]);
      return true;
    }
    const bounds = new maplibregl.LngLatBounds();
    for (const object of focusable) {
      bounds.extend([Number(object.coordinates!.lon), Number(object.coordinates!.lat)]);
    }
    if (bounds.isEmpty()) return false;
    map.fitBounds(bounds, {
      padding: { top: 120, right: 120, bottom: 140, left: 120 },
      duration: 900,
      essential: true,
    });
    selectObjectFromMapInteraction(focusable[0]);
    setSearchOpen(false);
    setSearchQuery('');
    return true;
  }, [focusObjectOnMap, map, selectObjectFromMapInteraction]);

  React.useEffect(() => {
    const sourceId = String(chatMapFocus?.sourceMessageId || '').trim();
    if (!sourceId) {
      appliedChatMapFocusRef.current = null;
      return;
    }
    const focusKey = [
      sourceId,
      chatFocusedObjects.map((object) => String(object?.id || '').trim()).join('|'),
      Array.from(chatFocusedArtifactIdSet).join('|'),
    ].join('::');
    if (appliedChatMapFocusRef.current === focusKey) return;
    if (chatFocusedObjects.length > 0) {
      if (focusObjectsOnMap(chatFocusedObjects)) {
        appliedChatMapFocusRef.current = focusKey;
        return;
      }
    }
    if (chatFocusedArtifactIdSet.size > 0) {
      setPathLegsEnabled(true);
      appliedChatMapFocusRef.current = focusKey;
    }
  }, [chatFocusedArtifactIdSet, chatFocusedObjects, chatMapFocus, focusObjectsOnMap]);

  const handleDirectedExplore = React.useCallback(
    async (direction: DirectedExploreDirection) => {
      if (!scope?.lensId || !selectedObject) return;
      const canonicalPath = String(selectedObject.canonicalPath || '').trim();
      if (!isDirectedExplorationCanonicalPath(canonicalPath)) {
        setDirectedExploreError('Directed exploration is available only for organizations, facilities, products, deposits, and government entities.');
        return;
      }

      const correlationId = (globalThis.crypto && 'randomUUID' in globalThis.crypto)
        ? (globalThis.crypto as any).randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;
      const fragmentId = String(selectedObject.id || '').trim();

      try {
        setDirectedExploreSubmitting(direction);
        setDirectedExploreError(null);
        await api.scopeScoutHorizonLensNode(
          scope.lensId,
          {
            canonicalPath,
            fragmentId: fragmentId || undefined,
            maxRecursionDepth: 1,
            mode: 'supply_chain',
            specialties: ['supply_chain', 'products_and_services'],
            exploration: {
              domainMode: 'supply_chain',
              direction,
              hotPathNodeIds: fragmentId ? [fragmentId] : undefined,
              renderTarget: 'map',
              ignoreFreshness: true,
            },
          },
          { correlationId, sessionId },
        );
      } catch (error: any) {
        const detail =
          String(error?.response?.data?.error || '').trim()
          || String(error?.response?.data?.detail || '').trim()
          || String(error?.message || '').trim();
        setDirectedExploreError(detail || 'Failed to start directed exploration.');
      } finally {
        setDirectedExploreSubmitting(null);
      }
    },
    [scope?.lensId, selectedObject],
  );

  const mapInspectorState = React.useMemo<MapObjectInspectorPayload | null>(() => {
    if (!selectedObject) return null;
    return {
      object: selectedObject,
      edgesLoading,
      edgesCount,
      visibleEdgesCount,
      traceSummary,
      activeNarrativeKey,
      onNarrativeHover: setActiveNarrativeKey,
      activeSupplyRouteIds: hoveredRouteArtifactIds,
      onSupplyRouteHover: setHoveredRouteArtifactIds,
      directedExploreSupported: Boolean(scope?.lensId) && isDirectedExplorationCanonicalPath(selectedObject?.canonicalPath),
      directedExploreBusy: directedExploreSubmitting,
      directedExploreError,
      directedExploreDisabledReason:
        !scope?.lensId
          ? 'Open the map inside a lens to run directed exploration.'
          : !isDirectedExplorationCanonicalPath(selectedObject?.canonicalPath)
            ? 'This object type is not supported for directed supply-chain exploration.'
            : undefined,
      onDirectedExplore: (direction) => {
        void handleDirectedExplore(direction);
      },
      onClose: () => {
        setActiveNarrativeKey(null);
        selectObjectFromMapInteraction(null);
      },
    };
  }, [
    activeNarrativeKey,
    directedExploreError,
    directedExploreSubmitting,
    edgesCount,
    edgesLoading,
    handleDirectedExplore,
    hoveredRouteArtifactIds,
    scope?.lensId,
    selectObjectFromMapInteraction,
    selectedObject,
    traceSummary,
    visibleEdgesCount,
  ]);

  React.useEffect(() => {
    onMapInspectorStateChange?.(mapInspectorState);
  }, [mapInspectorState, onMapInspectorStateChange]);

  const handleCompleteSubstrate = React.useCallback(async () => {
    if (!scope?.lensId || completeSubstrateRunning || completeSubstrateSubmitting) return;
    try {
      setCompleteSubstrateSubmitting(true);
      setCompleteSubstrateError(null);
      setDismissedCompleteSubstrateOverlayKey(null);
      const response = await api.completeSubstrateForLens(scope.lensId, {
        ...toolScopePayload,
        maxCompletions: 80,
      });
      if (response?.jobId) {
        setCompleteSubstrateJobId(String(response.jobId));
      }
    } catch (error: any) {
      const detail =
        String(error?.response?.data?.error || '').trim()
        || String(error?.response?.data?.detail || '').trim()
        || String(error?.message || '').trim();
      setCompleteSubstrateError(detail || 'Failed to start link completion.');
    } finally {
      setCompleteSubstrateSubmitting(false);
    }
  }, [
    completeSubstrateRunning,
    completeSubstrateSubmitting,
    scope?.lensId,
    toolScopePayload,
  ]);

  const handleRecoverMetrics = React.useCallback(async () => {
    if (!scope?.lensId || recoverMetricsRunning || recoverMetricsSubmitting) return;
    try {
      setRecoverMetricsSubmitting(true);
      setRecoverMetricsError(null);
      setDismissedRecoverMetricsOverlayKey(null);
      const response = await api.recoverMetricsForLens(scope.lensId, {
        ...toolScopePayload,
        maxRecoveries: 80,
      });
      if (response?.jobId) {
        setRecoverMetricsJobId(String(response.jobId));
      }
    } catch (error: any) {
      const detail =
        String(error?.response?.data?.error || '').trim()
        || String(error?.response?.data?.detail || '').trim()
        || String(error?.message || '').trim();
      setRecoverMetricsError(detail || 'Failed to start metrics recovery.');
    } finally {
      setRecoverMetricsSubmitting(false);
    }
  }, [
    recoverMetricsRunning,
    recoverMetricsSubmitting,
    scope?.lensId,
    toolScopePayload,
  ]);

  const handleIdentifyStresses = React.useCallback(async () => {
    if (!scope?.lensId || identifyRunning || identifySubmitting) return;
    try {
      setIdentifySubmitting(true);
      setIdentifyError(null);
      setDismissedIdentifyOverlayKey(null);
      const response = await api.identifyStressesForLens(scope.lensId, {
        ...toolScopePayload,
        maxFindings: 120,
      });
      if (response?.jobId) {
        setIdentifyJobId(String(response.jobId));
      }
    } catch (error: any) {
      const detail =
        String(error?.response?.data?.error || '').trim()
        || String(error?.response?.data?.detail || '').trim()
        || String(error?.message || '').trim();
      setIdentifyError(detail || 'Failed to start stress identification.');
    } finally {
      setIdentifySubmitting(false);
    }
  }, [
    identifyRunning,
    identifySubmitting,
    scope?.lensId,
    toolScopePayload,
  ]);

  const resolveSupplyAnchor = React.useCallback(async (query: string): Promise<HorizonSearchEntity | null> => {
    const trimmed = query.trim();
    if (!trimmed) return null;
    const response = await api.searchHorizonEntities(trimmed, 5);
    const entities = normalizeSupplyAnchorEntities(Array.isArray(response?.entities) ? response.entities : []);
    if (entities.length === 0) return null;
    const exact = entities.find((entity) => {
      const name = String(entity?.name || '').trim().toLowerCase();
      const canonicalPath = String(entity?.canonicalPath || '').trim().toLowerCase();
      const needle = trimmed.toLowerCase();
      return name === needle || canonicalPath === needle;
    });
    if (exact) return exact;
    const preferred = entities.find((entity) => isObjectLikeSupplyAnchor(entity));
    return preferred || entities[0] || null;
  }, []);

  const selectAnchorA = React.useCallback((entity: HorizonSearchEntity) => {
    setSupplyAnchorA(entity);
    setSupplyAnchorAQuery(String(entity?.name || entity?.canonicalPath || '').trim());
    setShowAnchorASuggestions(false);
  }, []);

  const selectAnchorB = React.useCallback((entity: HorizonSearchEntity) => {
    setSupplyAnchorB(entity);
    setSupplyAnchorBQuery(String(entity?.name || entity?.canonicalPath || '').trim());
    setShowAnchorBSuggestions(false);
  }, []);

  const closeManualChainOverlay = React.useCallback(() => {
    setManualChainOverlayCollapsed(true);
  }, []);

  const dismissManualChainOverlay = React.useCallback(() => {
    setManualChainOverlayCollapsed(false);
    setManualChainOverlayOpen(false);
    setActiveManualChain(null);
    setManualPathArtifacts([]);
  }, []);

  const closeSearchPalette = React.useCallback(() => {
    setSearchOpen(false);
    setSearchQuery('');
    setSupplySearchError(null);
    setShowAnchorASuggestions(false);
    setShowAnchorBSuggestions(false);
  }, []);

  const openSearchPalette = React.useCallback(() => {
    setSearchOpen(true);
  }, []);

  const toggleSearchPalette = React.useCallback(() => {
    if (searchOpen) {
      closeSearchPalette();
      return;
    }
    openSearchPalette();
  }, [closeSearchPalette, openSearchPalette, searchOpen]);

  const closeMapDockOverlays = React.useCallback(() => {
    setActiveDockPanel(null);
    onMapDrawerPanelChange?.(null);
  }, [onMapDrawerPanelChange]);

  const handleSearchKeyDown = React.useCallback((event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeSearchPalette();
      return;
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setSearchActiveIndex((idx) => Math.min(idx + 1, Math.max(searchSuggestions.length - 1, 0)));
      return;
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      setSearchActiveIndex((idx) => Math.max(idx - 1, 0));
      return;
    }
    if (event.key === 'Enter') {
      const selected = searchSuggestions[searchActiveIndex];
      if (!selected) return;
      event.preventDefault();
      focusObjectOnMap(selected.object);
    }
  }, [closeSearchPalette, focusObjectOnMap, searchActiveIndex, searchSuggestions]);

  const closeScopeDialog = React.useCallback(() => {
    setScopeDialogOpen(false);
    setScopeDialogSubmitting(false);
    setScopeDialogError(null);
    setScopeDialogCreatedLensId(null);
  }, []);

  React.useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const key = String(event.key || '').toLowerCase();
      if ((event.ctrlKey || event.metaKey) && !event.altKey && key === 'k') {
        event.preventDefault();
        event.stopPropagation();
        toggleSearchPalette();
        return;
      }
      if (event.key !== 'Escape') return;
      if (scopeDialogOpen && !scopeDialogSubmitting) {
        event.preventDefault();
        closeScopeDialog();
        return;
      }
      if (searchOpen) {
        event.preventDefault();
        closeSearchPalette();
        return;
      }
      if (activeDockPanel) {
        event.preventDefault();
        closeMapDockOverlays();
        return;
      }
      if (manualChainOverlayOpen) {
        event.preventDefault();
        closeManualChainOverlay();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [
    closeMapDockOverlays,
    closeManualChainOverlay,
    closeScopeDialog,
    closeSearchPalette,
    activeDockPanel,
    manualChainOverlayOpen,
    scopeDialogOpen,
    scopeDialogSubmitting,
    searchOpen,
    toggleSearchPalette,
  ]);

  const openScopeDialog = React.useCallback((result: SupplyChainSearchResult | null) => {
    const artifact = result?.artifact as MaterializedArtifact | undefined;
    setActiveManualChain(result);
    setScopeDialogError(null);
    setScopeDialogCreatedLensId(null);
    setScopeDialogTargetMode(scope?.lensId ? 'existing' : 'new');
    setScopeDialogUseCurrentLens(Boolean(scope?.lensId));
    setScopeDialogLensId(scope?.lensId || '');
    setScopeDialogProjectId('');
    setScopeDialogLensName(String(artifact?.displayName || 'Supply chain lens').trim() || 'Supply chain lens');
    setScopeDialogOpen(true);
  }, [scope?.lensId]);

  const handleRunSupplySearch = React.useCallback(async () => {
    try {
      setSupplySearchLoading(true);
      setSupplySearchError(null);
      const anchorA = supplyAnchorA || await resolveSupplyAnchor(supplyAnchorAQuery);
      if (!anchorA?.canonicalPath) {
        setSupplySearchError('Object A was not resolved.');
        setSupplySearchLoading(false);
        return;
      }
      const anchorB = betweenAnchors ? (supplyAnchorB || await resolveSupplyAnchor(supplyAnchorBQuery)) : null;
      if (betweenAnchors && !anchorB?.canonicalPath) {
        setSupplySearchError('Object B was not resolved.');
        setSupplySearchLoading(false);
        return;
      }
      setSupplyAnchorA(anchorA);
      if (anchorB) setSupplyAnchorB(anchorB);
      const pq = await api.queryHorizonPaths({
        from: anchorA.canonicalPath,
        to: anchorB?.canonicalPath,
        maxHops: supplyMaxLegs,
        lensId: scope?.lensId,
        includeStress: true,
      });
      const paths = Array.isArray(pq?.paths) ? pq.paths : [];
      const limit = 12;
      const results: SupplyChainSearchResult[] = paths.slice(0, limit).map((item, idx) => {
        const artifact: SupplyChainArtifactResponse = {
          id: item.id,
          kind: item.kind,
          payload: item.payload,
          displayName: item.displayName,
          nodeRefs: item.nodeRefs,
          riskOverlay: item.riskOverlay,
        };
        return {
          artifact,
          previewPayload: item.payload,
          score: 1000 - idx,
          match: anchorB?.canonicalPath
            ? { directedMatch: true, anchorIndexes: [0, 1] }
            : { anchorIndexes: [0] },
        };
      });
      setSupplySearchResults(results);
      if (!results.length) {
        setSupplySearchError('No supply-chain paths matched these anchors.');
      }
    } catch (error: any) {
      setSupplySearchError(error?.message || 'Supply-chain search failed.');
    } finally {
      setSupplySearchLoading(false);
    }
  }, [
    betweenAnchors,
    resolveSupplyAnchor,
    scope?.lensId,
    supplyAnchorA,
    supplyAnchorAQuery,
    supplyAnchorB,
    supplyAnchorBQuery,
    supplyMaxLegs,
  ]);

  const activateSupplySearchResult = React.useCallback((result: SupplyChainSearchResult, mode: 'replace' | 'append' = 'replace') => {
    const artifact = result?.artifact;
    if (!artifact?.id) return;
    const previewArtifact: MaterializedArtifact = {
      ...(artifact as MaterializedArtifact),
      payload: (result?.previewPayload || artifact.payload) as MaterializedArtifact['payload'],
    };
    setActiveManualChain(result);
    setManualPathArtifacts((prev) => {
      if (mode === 'append') {
        const next = new Map<string, MaterializedArtifact>();
        for (const item of prev) {
          if (item?.id) next.set(item.id, item);
        }
        next.set(previewArtifact.id, previewArtifact);
        return Array.from(next.values());
      }
      return [previewArtifact];
    });
    setManualChainOverlayCollapsed(false);
    setManualChainOverlayOpen(true);
    setPathLegsEnabled(true);
    setSearchOpen(false);
  }, []);

  const handleSelectSupplySearchResult = React.useCallback((result: SupplyChainSearchResult) => {
    activateSupplySearchResult(result, 'replace');
  }, [activateSupplySearchResult]);

  const handleSupplySearchKeyDown = React.useCallback((event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeSearchPalette();
      return;
    }
    if (event.key === 'ArrowDown') {
      if (supplySearchResults.length > 0) {
        event.preventDefault();
        setSearchActiveIndex((idx) => Math.min(idx + 1, Math.max(supplySearchResults.length - 1, 0)));
      }
      return;
    }
    if (event.key === 'ArrowUp') {
      if (supplySearchResults.length > 0) {
        event.preventDefault();
        setSearchActiveIndex((idx) => Math.max(idx - 1, 0));
      }
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const selected = supplySearchResults[searchActiveIndex];
      if (selected) {
        handleSelectSupplySearchResult(selected);
        return;
      }
      void handleRunSupplySearch();
    }
  }, [closeSearchPalette, handleRunSupplySearch, handleSelectSupplySearchResult, searchActiveIndex, supplySearchResults]);

  const handleAddChainToScope = React.useCallback(async (result: SupplyChainSearchResult | null) => {
    openScopeDialog(result);
  }, [openScopeDialog]);

  const handleConfirmAddChainToScope = React.useCallback(async () => {
    const artifact = activeManualChain?.artifact as MaterializedArtifact | undefined;
    const canonicalKey = String(artifact?.canonicalKey || '').trim();
    if (!canonicalKey) {
      setScopeDialogError('Selected chain has no canonical key.');
      return;
    }
    const scopeEntry = `/artifact/${canonicalKey}`;
    try {
      setScopeDialogSubmitting(true);
      setScopeDialogError(null);
      if (scope?.lensId && scopeDialogUseCurrentLens) {
        const lens = await api.getHorizonLens(scope.lensId);
        const nextScope = Array.from(new Set([...(Array.isArray(lens.scope) ? lens.scope : []), scopeEntry]));
        await api.updateHorizonLens(scope.lensId, { ...lens, scope: nextScope }, { skipScouting: true });
        setScopeDialogCreatedLensId(scope.lensId);
        return;
      }
      if (scopeDialogTargetMode === 'existing') {
        if (!scopeDialogLensId.trim()) {
          setScopeDialogError('Choose an existing lens.');
          return;
        }
        const lens = await api.getHorizonLens(scopeDialogLensId.trim());
        const nextScope = Array.from(new Set([...(Array.isArray(lens.scope) ? lens.scope : []), scopeEntry]));
        await api.updateHorizonLens(scopeDialogLensId.trim(), { ...lens, scope: nextScope }, { skipScouting: true });
        setScopeDialogCreatedLensId(scopeDialogLensId.trim());
        return;
      }
      if (!scopeDialogProjectId.trim()) {
        setScopeDialogError('Choose a project.');
        return;
      }
      const created = await api.createHorizonLens({
        projectId: scopeDialogProjectId.trim(),
        name: scopeDialogLensName.trim() || String(artifact?.displayName || 'Supply chain lens').trim() || 'Supply chain lens',
        scope: [scopeEntry],
        viewConfig: { graphView: 'supply-flow', layoutMode: 'supply-chain' },
        status: 'ACTIVE',
      }, { skipScouting: true });
      setScopeDialogCreatedLensId(created.id);
    } catch (error: any) {
      setScopeDialogError(error?.message || 'Failed to add chain to scope.');
    } finally {
      setScopeDialogSubmitting(false);
    }
  }, [activeManualChain, scope?.lensId, scopeDialogLensId, scopeDialogLensName, scopeDialogProjectId, scopeDialogTargetMode, scopeDialogUseCurrentLens]);

  const handleOpenCreatedLens = React.useCallback(() => {
    if (!scopeDialogCreatedLensId) return;
    try {
      if (scopeDialogProjectId.trim()) {
        localStorage.setItem('horizon.selectedProjectId', scopeDialogProjectId.trim());
      }
      localStorage.setItem('horizon.selectedLensId', scopeDialogCreatedLensId);
    } catch {
      // ignore localStorage failures
    }
    window.location.assign('/horizon');
  }, [scopeDialogCreatedLensId, scopeDialogProjectId]);

  const pathLegClusterState = usePathLegClusterLayer(
    map,
    isReady,
    styleEpoch,
    activePathArtifacts,
    pathOverlayObjects,
    theme,
    clusterProjectionEnabled,
    focusedSupplyArtifactIds,
    { selectedObject },
  );
  const effectivePathLegVisibilityPolicy = React.useMemo<PathLegVisibilityPolicy | undefined>(() => {
    const base = pathLegClusterState.visibilityPolicy;
    const hasSimulationLegFocus = simulationLegFocus.revealedLegIds.length > 0;
    if ((!base || Object.keys(base).length === 0) && !hasSimulationLegFocus) return base;
    const suppressed = new Set<string>((base?.suppressedLegIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    if (hasSimulationLegFocus) {
      for (const artifact of activePathArtifacts) {
        const artifactId = String(artifact?.id || '').trim();
        if (!artifactId) continue;
        const legs = Array.isArray(artifact?.payload?.legs) ? artifact.payload.legs : [];
        legs.forEach((leg: any, index: number) => {
          const legIndex = resolvedArtifactLegIndex(leg, index);
          const legId = `${artifactId}:leg:${legIndex}`;
          if (!simulationLegFocus.revealedLegIds.includes(legId)) suppressed.add(legId);
        });
      }
    }
    const revealed = [...(base?.revealedLegIds || []), ...simulationLegFocus.revealedLegIds]
      .map((value) => String(value || '').trim())
      .filter((value) => value && !suppressed.has(value));
    return {
      ...base,
      suppressedLegIds: Array.from(suppressed),
      revealedLegIds: revealed,
      visibleObjectIds: compactStringList([...(base?.visibleObjectIds || []), ...simulationLegFocus.visibleObjectIds]),
      visibleObjectPaths: compactStringList([...(base?.visibleObjectPaths || []), ...simulationLegFocus.visibleObjectPaths]),
      stressedLegIds: compactStringList([...(base?.stressedLegIds || []), ...simulationLegFocus.stressedLegIds]),
      validatedArtifactIds: compactStringList([...(base?.validatedArtifactIds || []), ...simulationValidatedArtifactIds]),
      hypothesisArtifactIds: compactStringList([...(base?.hypothesisArtifactIds || []), ...simulationHypothesisArtifactIds]),
      legCaptionById: {
        ...(base?.legCaptionById || {}),
        ...(simulationLegFocus.legCaptionById || {}),
      },
    };
  }, [activePathArtifacts, pathLegClusterState.visibilityPolicy, simulationHypothesisArtifactIds, simulationLegFocus, simulationValidatedArtifactIds]);
  const worldMapHighlightedMarkers = React.useMemo(
    () => {
      const narrativeGatewayIds = activeNarrativeKey
        ? (mapInteractionSpotlightObjectIds || [])
        : highlightedGatewayObjectIds;
      return [
        ...(selectedObject?.id ? [{ id: selectedObject.id, role: 'selected' as const }] : []),
        ...Array.from(chatFocusedObjectIdSet).map((id) => ({ id, role: 'search' as const })),
        ...Array.from(forcedRouteEndpointObjectIds).map((id) => ({ id, role: 'gateway' as const })),
        ...narrativeGatewayIds.map((id) => ({ id, role: 'gateway' as const })),
        ...Array.from(simulationFocusedObjectIdSet).map((id) => ({ id, role: 'gateway' as const })),
        ...(searchHighlightedObjectId ? [{ id: searchHighlightedObjectId, role: 'search' as const }] : []),
      ];
    },
    [
      activeNarrativeKey,
      chatFocusedObjectIdSet,
      forcedRouteEndpointObjectIds,
      highlightedGatewayObjectIds,
      mapInteractionSpotlightObjectIds,
      searchHighlightedObjectId,
      selectedObject?.id,
      simulationFocusedObjectIdSet,
    ],
  );
  const worldMapHighlightedMarkerIdSet = React.useMemo(
    () => new Set(worldMapHighlightedMarkers.map((item) => String(item.id || '').trim()).filter(Boolean)),
    [worldMapHighlightedMarkers],
  );
  const pathLegDeferEndpointLabelObjectIds = React.useMemo(
    () => objectIdsWithMarkerEndpointCaption(worldMapHighlightedMarkers),
    [worldMapHighlightedMarkers],
  );
  const markerLayerOptions = React.useMemo(
    () => ({
      extraInteractiveLayers: [...GEO_EDGE_INTERACTIVE_LAYERS, ...PATH_LEG_INTERACTIVE_LAYERS],
      suppressClearOnClusterClick: Boolean(selectedObject),
      highlightedObjects: worldMapHighlightedMarkers,
      mutedObjectIds: effectiveMutedObjectIds,
      mapInteractionSpotlightObjectIds: effectiveMapInteractionSpotlightObjectIds,
      onMapObjectHover: setHoveredMapObjectId,
    }),
    [
      effectiveMutedObjectIds,
      effectiveMapInteractionSpotlightObjectIds,
      selectedObject,
      worldMapHighlightedMarkers,
    ],
  );
  const pathLegInteractionHandlers = React.useMemo(() => ({
    onSelectedArtifactIdsChange: setFocusedRouteArtifactIds,
    onSelectObject: selectObjectFromMapInteraction,
  }), [selectObjectFromMapInteraction]);
  const baseMarkerObjects = React.useMemo(() => {
    const candidatesById = new Map<string, GeoObject>();
    const nowTs = Date.now();
    for (const object of [...visibleMapObjects, ...pathOverlayObjects, ...forcedRouteEndpointObjects]) {
      if (!object) continue;
      if (object.type === 'AFFECTED_ZONE' && !isAffectedZoneRenderable(object, affectedZoneFreshnessDays, showArchivedAffectedZones, nowTs)) continue;
      const key = String(object?.id || object?.canonicalPath || '').trim();
      if (!key || candidatesById.has(key)) continue;
      candidatesById.set(key, object);
    }
    return Array.from(candidatesById.values());
  }, [
    affectedZoneFreshnessDays,
    forcedRouteEndpointObjects,
    pathOverlayObjects,
    showArchivedAffectedZones,
    visibleMapObjects,
  ]);
  const suppressedDenseCaptionObjectIdSet = React.useMemo(
    () => new Set(suppressedDenseCaptionObjectIds.map((value) => String(value || '').trim()).filter(Boolean)),
    [suppressedDenseCaptionObjectIds],
  );
  useGeoMarkerLayer(map, isReady, styleEpoch, baseMarkerObjects, theme, handleMarkerSelection, markerLayerOptions);
  useGeoZoneLayer(map, isReady, styleEpoch, visibleMapObjects, theme);
  useStaticZoneLayer(map, isReady, styleEpoch, filteredStaticZones, theme, zonesEnabled, zoneActivityById, visibleMapObjects);
  useTransportFlowLayer(map, isReady, styleEpoch, filteredTransportFlows, theme, flowsEnabled, flowActivityById);
  const pathLegStats = usePathLegLayer(
    map,
    isReady,
    styleEpoch,
    activePathArtifacts,
    pathOverlayObjects,
    theme,
    pathLegsEnabled || (!pathLegClusteringEnabled && (supplyNarrativeLegsVisible || routeEmphasisVisible)),
    focusedSupplyArtifactIds,
    effectivePathLegVisibilityPolicy,
    routeVisibilitySelection,
    simulationStressEnabled,
    pathLegDeferEndpointLabelObjectIds,
    suppressedDenseCaptionObjectIdSet,
    pathLegInteractionHandlers,
  );
  const denseCaptionObjectById = React.useMemo(() => {
    const byId = new Map<string, GeoObject>();
    for (const object of [...pathOverlayObjects, ...visibleMapObjects, ...forcedRouteEndpointObjects]) {
      const objectId = String(object?.id || '').trim();
      if (objectId && !byId.has(objectId)) byId.set(objectId, object);
    }
    return byId;
  }, [forcedRouteEndpointObjects, pathOverlayObjects, visibleMapObjects]);

  React.useEffect(() => {
    if (!map || !isReady || !scope?.lensId || !pathLegsEnabled) {
      setDenseCaptionGroups((current) => (current.length === 0 ? current : []));
      setSuppressedDenseCaptionObjectIds((current) => (current.length === 0 ? current : []));
      return;
    }

    let cancelled = false;
    let rafId = 0;
    const sync = () => {
      if (cancelled || !map) return;
      try {
        if (!map.getLayer(PATH_LEG_OBJECT_LAYER_ID)) {
          setDenseCaptionGroups((current) => (current.length === 0 ? current : []));
          setSuppressedDenseCaptionObjectIds((current) => (current.length === 0 ? current : []));
          return;
        }
        const canvas = map.getCanvas();
        const width = Math.max(1, Math.round(canvas.clientWidth || 0));
        const height = Math.max(1, Math.round(canvas.clientHeight || 0));
        const features = map.queryRenderedFeatures(
          [[0, 0], [width, height]],
          { layers: [PATH_LEG_OBJECT_LAYER_ID] },
        ) as maplibregl.MapGeoJSONFeature[];
        const joinDistancePx = map.getZoom() >= 9.5 ? 68 : map.getZoom() >= 7.5 ? 60 : 54;
        const entryByObjectId = new Map<string, { id: string; x: number; y: number; priority: number }>();
        for (const feature of features) {
          if (feature.geometry?.type !== 'Point') continue;
          const objectId = String(feature.properties?.objectId || '').trim();
          if (!objectId || worldMapHighlightedMarkerIdSet.has(objectId) || !denseCaptionObjectById.has(objectId)) continue;
          const geometryPoint = feature.geometry as GeoJSON.Point;
          const coordinates = geometryPoint.coordinates as [number, number];
          const projected = map.project({ lng: coordinates[0], lat: coordinates[1] });
          if (!Number.isFinite(projected.x) || !Number.isFinite(projected.y)) continue;
          const priority = Number(feature.properties?.labelPriority || 0);
          const current = entryByObjectId.get(objectId);
          if (!current || priority > current.priority) {
            entryByObjectId.set(objectId, {
              id: objectId,
              x: projected.x,
              y: projected.y,
              priority,
            });
          }
        }
        const grouped = buildDenseCaptionGroups(
          Array.from(entryByObjectId.values()),
          {
            joinDistancePx,
            minGroupSize: 4,
            maxSpanXPx: joinDistancePx * 2.9,
            maxSpanYPx: joinDistancePx * 2.25,
          },
        );
        const nextGroups = grouped
          .map((group) => {
            const objects = group.memberIds
              .map((objectId) => denseCaptionObjectById.get(objectId))
              .filter((object): object is GeoObject => Boolean(object))
              .sort(compareMapObjectsForDenseCaption);
            if (objects.length < 4) return null;
            const primaryLabel = getSearchObjectTitle(objects[0]);
            const moreCount = Math.max(0, objects.length - 1);
            return {
              id: group.id,
              x: group.x,
              y: group.y,
              totalCount: objects.length,
              visibleObjects: objects,
              label: moreCount > 0 ? `${primaryLabel} +${moreCount} more` : primaryLabel,
            } satisfies DenseCaptionOverlayGroup;
          })
          .filter((group): group is DenseCaptionOverlayGroup => Boolean(group));
        const nextObjectIds = Array.from(new Set(
          grouped
            .flatMap((group) => group.memberIds)
            .map((objectId) => String(objectId || '').trim())
            .filter((objectId) => objectId && denseCaptionObjectById.has(objectId)),
        ));
        setDenseCaptionGroups((current) => {
          if (
            current.length === nextGroups.length
            && current.every((group, index) =>
              group.id === nextGroups[index].id
              && group.label === nextGroups[index].label
              && group.totalCount === nextGroups[index].totalCount
              && Math.abs(group.x - nextGroups[index].x) < 0.5
              && Math.abs(group.y - nextGroups[index].y) < 0.5
              && group.visibleObjects.length === nextGroups[index].visibleObjects.length
              && group.visibleObjects.every((object, objectIndex) => object.id === nextGroups[index].visibleObjects[objectIndex]?.id)
            )
          ) {
            return current;
          }
          return nextGroups;
        });
        setSuppressedDenseCaptionObjectIds((current) => {
          if (current.length === nextObjectIds.length && current.every((id, index) => id === nextObjectIds[index])) return current;
          return nextObjectIds;
        });
      } catch {
        if (cancelled) return;
        setDenseCaptionGroups((current) => (current.length === 0 ? current : []));
        setSuppressedDenseCaptionObjectIds((current) => (current.length === 0 ? current : []));
      }
    };
    const scheduleSync = () => {
      if (rafId) window.cancelAnimationFrame(rafId);
      rafId = window.requestAnimationFrame(sync);
    };
    scheduleSync();
    map.on('move', scheduleSync);
    map.on('zoom', scheduleSync);
    map.on('resize', scheduleSync);
    map.on('idle', scheduleSync);
    map.on(PATH_LEG_LABELS_UPDATED_EVENT, scheduleSync);
    return () => {
      cancelled = true;
      if (rafId) window.cancelAnimationFrame(rafId);
      map.off('move', scheduleSync);
      map.off('zoom', scheduleSync);
      map.off('resize', scheduleSync);
      map.off('idle', scheduleSync);
      map.off(PATH_LEG_LABELS_UPDATED_EVENT, scheduleSync);
    };
  }, [denseCaptionObjectById, isReady, map, pathLegsEnabled, scope?.lensId, styleEpoch, worldMapHighlightedMarkerIdSet]);
  useVesselLayer(map, isReady, styleEpoch, vesselsEnabled, theme);

  const handleToggleType = React.useCallback((type: string) => {
    setActiveTypes((prev) => {
      if (!prev) {
        const all = new Set(typeCounts.map((tc) => tc.type));
        all.delete(type);
        return all;
      }
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      if (next.size === typeCounts.length) return null;
      return next;
    });
  }, [typeCounts]);

  const filterActive = activeTypes !== null;
  const visibleStaticZonesCount = filteredStaticZones.length;
  const visibleTransportFlowsCount = filteredTransportFlows.length;

  const handleToggleZoneType = React.useCallback((type: StaticZoneType) => {
    setActiveZoneTypes((prev) => {
      if (!prev) {
        const all = new Set(zoneTypeCounts.map((zc) => zc.type));
        all.delete(type);
        return all;
      }
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      if (next.size === zoneTypeCounts.length) return null;
      return next;
    });
  }, [zoneTypeCounts]);

  const handleToggleFlowMode = React.useCallback((mode: TransportFlowMode) => {
    setActiveFlowModes((prev) => {
      if (!prev) {
        const all = new Set(flowModeCounts.map((fc) => fc.mode));
        all.delete(mode);
        return all;
      }
      const next = new Set(prev);
      if (next.has(mode)) next.delete(mode);
      else next.add(mode);
      if (next.size === flowModeCounts.length) return null;
      return next;
    });
  }, [flowModeCounts]);

  return (
    <div className="flex-1 min-h-0" style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ position: 'relative', flex: 1, minHeight: 0 }}>
        <div ref={containerRef} style={{ width: '100%', height: '100%', minHeight: 280 }} />

        {showMapPreparingOverlay && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 24,
              background: isDark ? 'rgba(2, 6, 23, 0.2)' : 'rgba(248, 250, 252, 0.26)',
              zIndex: 18,
            }}
          >
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                gap: 10,
                padding: '16px 20px',
                borderRadius: 16,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                boxShadow: isDark ? '0 12px 40px rgba(0,0,0,0.28)' : '0 12px 40px rgba(15,23,42,0.12)',
                minWidth: 220,
                color: 'var(--text-secondary-strong)',
              }}
            >
              <style>{`
                @keyframes horizon-map-dot-bounce {
                  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                  40% { transform: translateY(-6px); opacity: 1; }
                }
              `}</style>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                {[0, 1, 2].map((i) => (
                  <div
                    key={i}
                    style={{
                      width: 10,
                      height: 10,
                      borderRadius: 999,
                      background: '#f59e0b',
                      animation: 'horizon-map-dot-bounce 1.1s infinite ease-in-out',
                      animationDelay: `${i * 0.14}s`,
                    }}
                  />
                ))}
              </div>
              <div style={{ fontSize: 12, fontWeight: 650 }}>
                Preparing the map...
              </div>
            </div>
          </div>
        )}

        {error && (
          <div style={{ position: 'absolute', left: 10, right: 10, bottom: 10, padding: '8px 10px', borderRadius: 8, border: '1px solid rgba(253,164,175,0.45)', backgroundColor: 'rgba(127,29,29,0.35)', color: '#fecdd3', fontSize: 12 }}>
            {error}
          </div>
        )}

        {dependencyTreeAvailable && (
          <div
            style={{
              position: 'absolute',
              top: Math.max(12, overlayTopOffset),
              left: '50%',
              transform: 'translateX(-50%)',
              zIndex: 25,
              pointerEvents: 'auto',
            }}
          >
            <div style={mapSurfaceToggleShellStyle(isDark)}>
              <button
                type="button"
                onClick={() => setSurfaceMode('map')}
                style={mapSurfaceToggleButtonStyle(isDark, surfaceMode === 'map')}
              >
                View on Map
              </button>
              <button
                type="button"
                onClick={() => setSurfaceMode('dependency-tree')}
                style={mapSurfaceToggleButtonStyle(isDark, surfaceMode === 'dependency-tree')}
              >
                View as Dependency Tree
              </button>
            </div>
          </div>
        )}

        {dependencyTreeAvailable && surfaceMode === 'dependency-tree' && selectedObject && (
          <MapDependencyTreeView
            isDark={isDark}
            selectedObject={selectedObject}
            objects={mapObjects}
            selectionExportData={selectionExportData}
            traceSummary={traceSummary}
          />
        )}

        <div
          style={{
            position: 'absolute',
            top: 12,
            right: 12,
            zIndex: 24,
            display: 'flex',
            alignItems: 'flex-start',
            gap: 10,
            pointerEvents: 'none',
            userSelect: 'none',
          }}
        >
          <div style={{ position: 'relative', pointerEvents: 'auto' }}>
            <button
              type="button"
              title={filterOverlayOpen ? 'Hide route filters' : 'Show route filters'}
              aria-label={filterOverlayOpen ? 'Hide route filters' : 'Show route filters'}
              onClick={() => {
                setActiveDockPanel((current) => (current === 'filter' ? null : 'filter'));
              }}
              style={mapDockControlButtonStyle(isDark, filterOverlayOpen, isDark ? '#fbbf24' : '#d97706')}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M4 6h16" />
                <path d="M7 12h10" />
                <path d="M10 18h4" />
              </svg>
              <span>Filter</span>
            </button>
            {filterOverlayOpen && (
              <div style={{ position: 'absolute', top: 'calc(100% + 10px)', right: 0 }}>
                <div style={{ ...mapDockOverlayCardStyle(isDark, 392), padding: 14, display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                    <div>
                      <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--text-heading)' }}>Filter</div>
                      <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        Work on the currently visible route graph, not on a separate drawer scope.
                      </div>
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
                      {routeCatalog.length} visible routes
                    </div>
                  </div>

                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
                      gap: 8,
                    }}
                  >
                    {([
                      { key: 'products', label: 'By products' },
                      { key: 'routes', label: 'By routes' },
                    ] as const).map((item) => {
                      const active = filterOverlayMode === item.key;
                      return (
                        <button
                          key={item.key}
                          type="button"
                          onClick={() => {
                            setFilterOverlayMode(item.key);
                            setActiveDockPanel('filter');
                          }}
                          style={{
                            borderRadius: 12,
                            border: active ? '1px solid rgba(251,191,36,0.34)' : '1px solid var(--border-subtle)',
                            background: active
                              ? (isDark ? 'rgba(251,191,36,0.12)' : 'rgba(245,158,11,0.10)')
                              : mapSearchPaletteChrome.subtleSurface,
                            color: active ? '#fbbf24' : 'var(--text-secondary-strong)',
                            padding: '10px 12px',
                            fontSize: 12,
                            fontWeight: 800,
                            cursor: 'pointer',
                          }}
                        >
                          {item.label}
                        </button>
                      );
                    })}
                  </div>

                  {routesOverlayOpen ? (
                    <>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                        <div>
                          <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--text-heading)' }}>Supply Routes</div>
                          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                            Search loaded routes and highlight selected paths.
                          </div>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
                          {selectedRouteArtifactIdSet.size > 0 ? `${selectedRouteArtifactIdSet.size} selected` : `${routeCatalog.length} loaded`}
                        </div>
                      </div>

                      <div style={{ display: 'flex', gap: 8 }}>
                        <input
                          type="search"
                          value={routeSearchQuery}
                          onChange={(event) => setRouteSearchQuery(event.target.value)}
                          placeholder="Search routes, anchors, products"
                          style={{
                            flex: 1,
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: mapSearchPaletteChrome.subtleSurface,
                            color: 'var(--text-heading)',
                            padding: '9px 11px',
                            fontSize: 12,
                          }}
                        />
                        <button
                          type="button"
                          onClick={() => {
                            setSelectedProductGroupKeys([]);
                            setSelectedRouteArtifactIds([]);
                          }}
                          disabled={selectedRouteArtifactIdSet.size === 0}
                          style={{
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: 'transparent',
                            color: selectedRouteArtifactIdSet.size > 0 ? 'var(--text-secondary-strong)' : 'var(--text-secondary)',
                            padding: '9px 10px',
                            fontSize: 11,
                            fontWeight: 700,
                            cursor: selectedRouteArtifactIdSet.size > 0 ? 'pointer' : 'not-allowed',
                            opacity: selectedRouteArtifactIdSet.size > 0 ? 1 : 0.55,
                            whiteSpace: 'nowrap',
                          }}
                        >
                          Clear selected
                        </button>
                      </div>

                      <div style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        <span>{filteredRouteCatalog.length} shown</span>
                        <span>•</span>
                        <span>{routeFilterActive ? 'Only filtered routes are visible' : 'All loaded routes are active'}</span>
                      </div>

                      <div
                        style={{
                          borderRadius: 12,
                          border: '1px solid var(--border-subtle)',
                          background: mapSearchPaletteChrome.subtleSurface,
                          padding: '10px 12px',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 12,
                        }}
                      >
                        <div style={{ minWidth: 0 }}>
                          <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Cluster route lines</div>
                          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                            {routeFilterActive
                              ? 'Filtering no longer changes clustering behavior. The standard map clustering still applies.'
                              : 'Use the same clustering mode as in Filters.'}
                          </div>
                        </div>
                        <button
                          type="button"
                          onClick={() => setPathLegClusteringEnabled((value) => !value)}
                          style={{
                            borderRadius: 999,
                            border: pathLegClusteringEnabled ? '1px solid rgba(14,165,233,0.32)' : '1px solid var(--border-subtle)',
                            background: pathLegClusteringEnabled
                              ? (isDark ? 'rgba(14,165,233,0.16)' : 'rgba(14,165,233,0.10)')
                              : 'transparent',
                            color: pathLegClusteringEnabled ? (isDark ? '#bae6fd' : '#075985') : 'var(--text-secondary-strong)',
                            padding: '6px 10px',
                            fontSize: 11,
                            fontWeight: 800,
                            cursor: 'pointer',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {pathLegClusteringEnabled ? 'On' : 'Off'}
                        </button>
                      </div>

                      <div style={{ maxHeight: 420, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8, paddingRight: 2 }}>
                        {filteredRouteCatalog.length === 0 && (
                          <div style={{ borderRadius: 12, border: '1px solid var(--border-subtle)', background: mapSearchPaletteChrome.subtleSurface, padding: '12px 14px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                            No loaded routes match this search.
                          </div>
                        )}
                        {filteredRouteCatalog.map((artifact) => {
                          const artifactId = String(artifact.id || '').trim();
                          const selected = selectedRouteArtifactIdSet.has(artifactId);
                          const products = artifactRouteProducts(artifact);
                          return (
                            <button
                              key={artifactId}
                              type="button"
                              onClick={() => {
                                setSelectedProductGroupKeys([]);
                                setSelectedRouteArtifactIds((current) => {
                                  const currentSet = new Set(current.map((value) => String(value || '').trim()).filter(Boolean));
                                  if (currentSet.has(artifactId)) currentSet.delete(artifactId);
                                  else currentSet.add(artifactId);
                                  return Array.from(currentSet);
                                });
                              }}
                              style={{
                                width: '100%',
                                textAlign: 'left',
                                borderRadius: 12,
                                border: selected ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                                background: selected ? mapSearchPaletteChrome.activeItemBackground : mapSearchPaletteChrome.subtleSurface,
                                color: 'var(--text-heading)',
                                padding: '11px 12px',
                                display: 'flex',
                                flexDirection: 'column',
                                gap: 6,
                                cursor: 'pointer',
                              }}
                            >
                              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                                <div style={{ minWidth: 0 }}>
                                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>
                                    {artifactRouteTitle(artifact)}
                                  </div>
                                  <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                    {artifactRouteSubtitle(artifact)}
                                  </div>
                                </div>
                                <div
                                  style={{
                                    minWidth: 18,
                                    height: 18,
                                    borderRadius: 999,
                                    border: selected ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                                    background: selected ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 10,
                                    fontWeight: 900,
                                    color: selected ? 'var(--text-heading)' : 'transparent',
                                  }}
                                >
                                  ✓
                                </div>
                              </div>
                              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                <span>{artifact.legCount || (Array.isArray(artifact.payload?.legs) ? artifact.payload!.legs!.length : 0)} legs</span>
                                {typeof artifact.pathStressScore === 'number' && artifact.pathStressScore > 0 && (
                                  <span>stress {artifact.pathStressScore.toFixed(2)}</span>
                                )}
                                {artifact.isPinned && <span>Pinned</span>}
                              </div>
                              {products.length > 0 && (
                                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                  {products.join(', ')}
                                </div>
                              )}
                            </button>
                          );
                        })}
                      </div>
                    </>
                  ) : (
                    <>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                        <div>
                          <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--text-heading)' }}>Route Products</div>
                          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                            Select or clear all loaded routes grouped under a product.
                          </div>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
                          {productRouteGroups.length} groups
                        </div>
                      </div>

                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                        <button
                          type="button"
                          onClick={() => setActiveProductStages(null)}
                          style={{
                            borderRadius: 999,
                            border: !activeProductStages || activeProductStages.size === 0
                              ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}`
                              : '1px solid var(--border-subtle)',
                            background: !activeProductStages || activeProductStages.size === 0
                              ? mapSearchPaletteChrome.activeItemBackground
                              : mapSearchPaletteChrome.subtleSurface,
                            color: 'var(--text-heading)',
                            padding: '7px 10px',
                            fontSize: 11,
                            fontWeight: 700,
                            cursor: 'pointer',
                          }}
                        >
                          All stages
                        </button>
                        {productStageFacets.map((facet) => {
                          const active = Boolean(activeProductStages?.has(facet.key));
                          return (
                            <button
                              key={facet.key}
                              type="button"
                              onClick={() => {
                                setActiveProductStages((current) => {
                                  const next = new Set(Array.from(current || []));
                                  if (next.has(facet.key)) next.delete(facet.key);
                                  else next.add(facet.key);
                                  return next.size > 0 ? next : null;
                                });
                              }}
                              style={{
                                borderRadius: 999,
                                border: active
                                  ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}`
                                  : '1px solid var(--border-subtle)',
                                background: active
                                  ? mapSearchPaletteChrome.activeItemBackground
                                  : mapSearchPaletteChrome.subtleSurface,
                                color: 'var(--text-heading)',
                                padding: '7px 10px',
                                fontSize: 11,
                                fontWeight: 700,
                                cursor: 'pointer',
                              }}
                            >
                              {facet.label} · {facet.routeCount}
                            </button>
                          );
                        })}
                      </div>

                      <div style={{ display: 'flex', gap: 8 }}>
                        <input
                          type="search"
                          value={productSearchQuery}
                          onChange={(event) => setProductSearchQuery(event.target.value)}
                          placeholder="Search products"
                          style={{
                            flex: 1,
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: mapSearchPaletteChrome.subtleSurface,
                            color: 'var(--text-heading)',
                            padding: '9px 11px',
                            fontSize: 12,
                          }}
                        />
                        <button
                          type="button"
                          onClick={resetProductsPanelState}
                          disabled={
                            selectedRouteArtifactIdSet.size === 0
                            && (!activeProductStages || activeProductStages.size === 0)
                            && !productSearchQueryTrim
                          }
                          style={{
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: 'transparent',
                            color:
                              selectedRouteArtifactIdSet.size > 0 || (activeProductStages && activeProductStages.size > 0) || productSearchQueryTrim
                                ? 'var(--text-secondary-strong)'
                                : 'var(--text-secondary)',
                            padding: '9px 10px',
                            fontSize: 11,
                            fontWeight: 700,
                            cursor:
                              selectedRouteArtifactIdSet.size > 0 || (activeProductStages && activeProductStages.size > 0) || productSearchQueryTrim
                                ? 'pointer'
                                : 'not-allowed',
                            opacity:
                              selectedRouteArtifactIdSet.size > 0 || (activeProductStages && activeProductStages.size > 0) || productSearchQueryTrim
                                ? 1
                                : 0.55,
                            whiteSpace: 'nowrap',
                          }}
                        >
                          Reset
                        </button>
                      </div>

                      <div style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        <span>{productRouteGroups.length} shown</span>
                        <span>•</span>
                        <span>{activeProductStages && activeProductStages.size > 0 ? `${activeProductStages.size} stages active` : 'All stages'}</span>
                        <span>•</span>
                        <span>{selectedRouteArtifactIdSet.size > 0 ? `${selectedRouteArtifactIdSet.size} routes selected` : 'No route filter applied'}</span>
                      </div>

                      <div style={{ maxHeight: 420, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8, paddingRight: 2 }}>
                        {productRouteGroups.length === 0 && (
                          <div style={{ borderRadius: 12, border: '1px solid var(--border-subtle)', background: mapSearchPaletteChrome.subtleSurface, padding: '12px 14px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                            No loaded product groups match this search.
                          </div>
                        )}
                        {productRouteGroups.map((group) => {
                          const selectedCount = group.artifactIds.filter((artifactId) => selectedRouteArtifactIdSet.has(artifactId)).length;
                          const allSelected = selectedCount > 0 && selectedCount === group.artifactIds.length;
                          const partiallySelected = selectedCount > 0 && !allSelected;
                          const groupKey = String(group.key || '').trim().toLowerCase();
                          const groupFilterActive = selectedProductGroupKeySet.has(groupKey);
                          return (
                            <button
                              key={group.key}
                              type="button"
                              onClick={() => {
                                setSelectedProductGroupKeys((current) => {
                                  const next = new Set(current.map((value) => String(value || '').trim().toLowerCase()).filter(Boolean));
                                  if (groupFilterActive) next.delete(groupKey);
                                  else next.add(groupKey);
                                  return Array.from(next);
                                });
                                setSelectedRouteArtifactIds((current) => {
                                  const next = new Set(current.map((value) => String(value || '').trim()).filter(Boolean));
                                  if (allSelected) {
                                    for (const artifactId of group.artifactIds) next.delete(artifactId);
                                  } else {
                                    for (const artifactId of group.artifactIds) next.add(artifactId);
                                  }
                                  return Array.from(next);
                                });
                              }}
                              style={{
                                width: '100%',
                                textAlign: 'left',
                                borderRadius: 12,
                                border: allSelected || partiallySelected || groupFilterActive ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                                background: allSelected || partiallySelected || groupFilterActive ? mapSearchPaletteChrome.activeItemBackground : mapSearchPaletteChrome.subtleSurface,
                                color: 'var(--text-heading)',
                                padding: '11px 12px',
                                display: 'flex',
                                flexDirection: 'column',
                                gap: 6,
                                cursor: 'pointer',
                              }}
                            >
                              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                                <div style={{ minWidth: 0 }}>
                                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>
                                    {group.label}
                                  </div>
                                  <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                    {group.stageLabel} • {group.routeCount} routes
                                    {group.pinnedCount > 0 ? ` • ${group.pinnedCount} pinned` : ''}
                                    {selectedCount > 0 ? ` • ${selectedCount} selected` : ''}
                                  </div>
                                </div>
                                <div
                                  style={{
                                    minWidth: 18,
                                    height: 18,
                                    borderRadius: 999,
                                    border: allSelected || partiallySelected ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                                    background: allSelected || partiallySelected ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 10,
                                    fontWeight: 900,
                                    color: allSelected || partiallySelected ? 'var(--text-heading)' : 'transparent',
                                  }}
                                >
                                  {allSelected ? '✓' : partiallySelected ? '•' : '✓'}
                                </div>
                              </div>
                              {group.topRoutes.length > 0 && (
                                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                  {group.topRoutes.join(' • ')}
                                </div>
                              )}
                            </button>
                          );
                        })}
                      </div>
                    </>
                  )}
                </div>
              </div>
            )}
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, pointerEvents: 'auto' }}>
            <div style={{ position: 'relative' }}>
              <button
                type="button"
                title={viewOverlayOpen ? 'Hide view controls' : 'Show view controls'}
                aria-label={viewOverlayOpen ? 'Hide view controls' : 'Show view controls'}
                onClick={() => {
                  onMapDrawerPanelChange?.(activeMapDrawerPanel === 'view' ? null : 'view');
                }}
                style={mapDockRailButtonStyle(isDark, viewOverlayOpen)}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <line x1="4" y1="6" x2="20" y2="6" />
                  <line x1="7" y1="12" x2="17" y2="12" />
                  <line x1="10" y1="18" x2="14" y2="18" />
                </svg>
              </button>
              {viewOverlayOpen && mapDrawerHost ? createPortal(
                <div style={mapDrawerPanelShellStyle(isDark)}>
                    <div style={mapDrawerPanelLeadStyle(isDark)}>
                      <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                        Manage visibility, network depth and operational windows for the current map view.
                      </div>
                      <div style={{ fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        Changes here affect only what is currently rendered in the map and route graph.
                      </div>
                    </div>

                    <div
                      style={mapDrawerSectionStyle(isDark, 'muted')}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'baseline' }}>
                        <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Current view</div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
                          {visibleMapObjects.length} visible
                        </div>
                      </div>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
                          gap: 8,
                        }}
                      >
                      {[
                        { label: 'Objects', value: `${visibleMapObjects.length}`, note: filterActive ? `${filteredObjects.length}/${allObjects.length} in scope` : 'Current view' },
                        { label: 'Route lines', value: pathLegsEnabled ? `${pathLegStats.legCount}` : 'Off', note: pathLegsEnabled ? (pathLegClusteringEnabled ? 'clustered when possible' : 'expanded') : 'Hidden' },
                        { label: 'Zones', value: zonesEnabled ? `${affectedZoneVisibility.visible}` : 'Off', note: zonesEnabled ? `${affectedZoneVisibility.total} tracked` : 'Not shown' },
                        { label: 'Flows', value: flowsEnabled ? `${visibleTransportFlowsCount}` : 'Off', note: flowsEnabled ? 'Transport corridors visible' : 'Not shown' },
                      ].map((item) => (
                        <div
                          key={item.label}
                          style={mapDrawerMetricCardStyle(isDark)}
                        >
                          <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                            {item.label}
                          </div>
                          <div style={{ marginTop: 5, fontSize: 15, fontWeight: 800, color: 'var(--text-heading)' }}>{item.value}</div>
                          <div style={{ marginTop: 4, fontSize: 10, lineHeight: 1.4, color: 'var(--text-secondary-strong)' }}>{item.note}</div>
                        </div>
                      ))}
                      </div>
                    </div>

                    <div style={mapDrawerSectionStyle(isDark, 'default')}>
                      <MapFilterPanel
                        floating
                        presentation="drawer"
                        isDark={isDark}
                        paletteTheme={theme}
                        typeCounts={typeCounts}
                        zoneTypeCounts={zoneTypeCounts}
                        flowModeCounts={flowModeCounts}
                        activeTypes={activeTypes}
                        activeZoneTypes={activeZoneTypes}
                        activeFlowModes={activeFlowModes}
                        zonesEnabled={zonesEnabled}
                        flowsEnabled={flowsEnabled}
                        pathLegsEnabled={pathLegsEnabled}
                        pathLegClusteringEnabled={pathLegClusteringEnabled}
                        pathLegCount={pathLegStats.legCount}
                        vesselsEnabled={vesselsEnabled}
                        vesselsApiAvailable={vesselsApiAvailable}
                        dateFrom={dateFrom}
                        dateTo={dateTo}
                        hops={hops}
                        loadingData={loading}
                        onSetDateFrom={setDateFrom}
                        onSetDateTo={setDateTo}
                        onSetHops={setHops}
                        onToggleType={handleToggleType}
                        onToggleZoneType={handleToggleZoneType}
                        onToggleFlowMode={handleToggleFlowMode}
                        onToggleZonesEnabled={() => setZonesEnabled((v) => !v)}
                        onToggleFlowsEnabled={() => setFlowsEnabled((v) => !v)}
                        onTogglePathLegsEnabled={() => setPathLegsEnabled((v) => !v)}
                        onTogglePathLegClusteringEnabled={() => setPathLegClusteringEnabled((v) => !v)}
                        onToggleVesselsEnabled={() => setVesselsEnabled((v) => !v)}
                        onSelectAll={() => setActiveTypes(null)}
                        onSelectNone={() => setActiveTypes(new Set())}
                        onSelectAllZoneTypes={() => setActiveZoneTypes(null)}
                        onSelectNoneZoneTypes={() => setActiveZoneTypes(new Set())}
                        onSelectAllFlowModes={() => setActiveFlowModes(null)}
                        onSelectNoneFlowModes={() => setActiveFlowModes(new Set())}
                      />
                    </div>
                </div>,
                mapDrawerHost,
              ) : null}
            </div>

            <div style={{ position: 'relative' }}>
              <button
                type="button"
                title={simulationOverlayOpen ? 'Hide simulation overlays' : 'Show simulation overlays'}
                aria-label={simulationOverlayOpen ? 'Hide simulation overlays' : 'Show simulation overlays'}
                onClick={() => {
                  onMapDrawerPanelChange?.(activeMapDrawerPanel === 'simulation' ? null : 'simulation');
                }}
                style={mapDockRailButtonStyle(isDark, simulationOverlayOpen)}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M5 19V9" />
                  <path d="M12 19V5" />
                  <path d="M19 19v-7" />
                </svg>
              </button>
              {simulationOverlayOpen && mapDrawerHost ? createPortal(
                <div style={mapDrawerPanelShellStyle(isDark)}>
                  <div style={mapDrawerPanelLeadStyle(isDark)}>
                    <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                      Explore simulation overlays against the currently visible route graph.
                    </div>
                    <div style={{ fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                      Activate overlays to project stress hypotheses directly onto the map without leaving the current lens.
                    </div>
                  </div>
                  <div style={mapDrawerSectionStyle(isDark, 'default')}>
                  <SimulationMapOverlayPanel
                    lensId={scope?.lensId || null}
                    routeCatalog={routeCatalog}
                    selectedRouteArtifactIds={focusedRouteArtifactIds}
                    onChangeSelectedRouteArtifactIds={setFocusedRouteArtifactIds}
                    onSelectionChange={setSimulationSelection}
                    isDark={isDark}
                  />
                  </div>
                </div>,
                mapDrawerHost,
              ) : null}
            </div>

            <div style={{ position: 'relative' }}>
              <button
                type="button"
                title={exportToolsOverlayOpen ? 'Hide export and tools' : 'Show export and tools'}
                aria-label={exportToolsOverlayOpen ? 'Hide export and tools' : 'Show export and tools'}
                onClick={() => {
                  onMapDrawerPanelChange?.(activeMapDrawerPanel === 'export-tools' ? null : 'export-tools');
                }}
                style={mapDockRailButtonStyle(isDark, exportToolsOverlayOpen)}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M12 3v8" />
                  <path d="m8 8 4 4 4-4" />
                  <path d="M4 17h16" />
                  <path d="M7 21h10" />
                </svg>
              </button>
              {exportToolsOverlayOpen && mapDrawerHost ? createPortal(
                <div
                  style={{
                    ...mapDrawerPanelShellStyle(isDark),
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 12,
                  }}
                >
                    <div style={mapDrawerPanelLeadStyle(isDark)}>
                      <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                        Export the current map state or run maintenance jobs on the visible route graph.
                      </div>
                      <div style={{ fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        {selectedObject ? `Current asset: ${getSearchObjectTitle(selectedObject)}` : 'Pick an asset on the map to unlock selection export.'}
                      </div>
                    </div>

                    <div style={mapDrawerSectionStyle(isDark, 'default')}>
                      <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Export</div>
                      <div style={{ fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        Download a JSON snapshot for the active object selection and the graph data already loaded into the map.
                      </div>
                      <button
                        type="button"
                        onClick={handleExportJson}
                        disabled={!selectedObject || !selectionExportData || edgesLoading}
                        title={!selectedObject ? 'Select an asset to export JSON files' : 'Download selection and loaded-graph JSON files'}
                        style={mapDrawerActionButtonStyle(
                          isDark,
                          'default',
                          !selectedObject || !selectionExportData || edgesLoading,
                        )}
                      >
                        {edgesLoading && selectedObject ? 'Preparing JSON...' : 'Download JSON snapshot'}
                      </button>
                      {scope?.lensId && onOpenDocuments ? (
                        <button
                          type="button"
                          onClick={onOpenDocuments}
                          style={mapDrawerActionButtonStyle(isDark, 'accent')}
                        >
                          Documents
                        </button>
                      ) : null}
                    </div>

                    <div style={mapDrawerSectionStyle(isDark, 'muted')}>
                      <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Tools</div>
                      <div style={{ fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        {!scope?.lensId
                          ? 'Open a Horizon lens to run supply-chain tools.'
                          : 'Run supply-chain maintenance jobs over the currently visible map graph.'}
                      </div>
                      {([
                        {
                          key: 'complete',
                          label: 'Complete Links',
                          desc: completeSubstrateSubmitting || completeSubstrateRunning ? 'Running…' : 'Materialize routes',
                          disabled: !scope?.lensId || completeSubstrateRunning || completeSubstrateSubmitting,
                          onClick: () => { void handleCompleteSubstrate(); },
                        },
                        {
                          key: 'recover',
                          label: 'Recover Metrics',
                          desc: recoverMetricsSubmitting || recoverMetricsRunning ? 'Running…' : 'Fill route metrics',
                          disabled: !scope?.lensId || recoverMetricsRunning || recoverMetricsSubmitting,
                          onClick: () => { void handleRecoverMetrics(); },
                        },
                        {
                          key: 'identify',
                          label: 'Identify Stresses',
                          desc: identifySubmitting || identifyRunning ? 'Running…' : 'Apply risk metrics',
                          disabled: !scope?.lensId || identifyRunning || identifySubmitting,
                          onClick: () => { void handleIdentifyStresses(); },
                        },
                      ]).map((action) => (
                        <button
                          key={action.key}
                          type="button"
                          onClick={action.onClick}
                          disabled={action.disabled}
                          style={{
                            ...mapDrawerActionButtonStyle(isDark, 'default', action.disabled),
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            gap: 10,
                          }}
                        >
                          <span style={{ fontSize: 13, fontWeight: 700 }}>{action.label}</span>
                          <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{action.desc}</span>
                        </button>
                      ))}
                    </div>

                    {completeSubstrateOverlayVisible && (
                      <div style={mapDrawerSectionStyle(isDark, completeSubstrateErrorLines.length > 0 ? 'danger' : 'accent')}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                          <div>
                            <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Link Completion</div>
                            <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                              {latestCompleteSubstrateJob ? `State: ${String(latestCompleteSubstrateJob.state || 'started').replace(/_/g, ' ')}` : 'Launch failed'}
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={() => {
                              if (!completeSubstrateOverlayKey) return;
                              setDismissedCompleteSubstrateOverlayKey(completeSubstrateOverlayKey);
                            }}
                            style={{ width: 22, height: 22, borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer', padding: 0 }}
                          >
                            ×
                          </button>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          Visible nodes {latestCompleteSubstrateJob?.acceptedNodes ?? identifyVisibleNodeRefs.length} • Visible routes {latestCompleteSubstrateJob?.acceptedArtifacts ?? identifyVisibleArtifactKeys.length} • Created {latestCompleteSubstrateJob?.artifactsCreated ?? 0} • Updated {latestCompleteSubstrateJob?.artifactsUpdated ?? 0}
                        </div>
                        {completeSubstrateErrorLines.length > 0 && (
                          <div style={{ fontSize: 11, lineHeight: 1.45, color: isDark ? '#fecaca' : '#991b1b' }}>
                            {completeSubstrateErrorLines.join(' • ')}
                          </div>
                        )}
                      </div>
                    )}

                    {recoverMetricsOverlayVisible && (
                      <div style={mapDrawerSectionStyle(isDark, recoverMetricsErrorLines.length > 0 ? 'danger' : 'accent')}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                          <div>
                            <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Metric Recovery</div>
                            <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                              {latestRecoverMetricsJob ? `State: ${String(latestRecoverMetricsJob.state || 'started').replace(/_/g, ' ')}` : 'Launch failed'}
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={() => {
                              if (!recoverMetricsOverlayKey) return;
                              setDismissedRecoverMetricsOverlayKey(recoverMetricsOverlayKey);
                            }}
                            style={{ width: 22, height: 22, borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer', padding: 0 }}
                          >
                            ×
                          </button>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          Visible nodes {latestRecoverMetricsJob?.acceptedNodes ?? identifyVisibleNodeRefs.length} • Visible routes {latestRecoverMetricsJob?.acceptedArtifacts ?? identifyVisibleArtifactKeys.length} • Updated {latestRecoverMetricsJob?.artifactsUpdated ?? 0} • Recovered {latestRecoverMetricsJob?.recoveredLinks?.length ?? 0}
                        </div>
                        {recoverMetricsErrorLines.length > 0 && (
                          <div style={{ fontSize: 11, lineHeight: 1.45, color: isDark ? '#fecaca' : '#991b1b' }}>
                            {recoverMetricsErrorLines.join(' • ')}
                          </div>
                        )}
                      </div>
                    )}

                    {identifyOverlayVisible && (
                      <div style={mapDrawerSectionStyle(isDark, identifyErrorLines.length > 0 ? 'danger' : 'accent')}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                          <div>
                            <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Stress Job</div>
                            <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                              {latestIdentifyJob ? `State: ${String(latestIdentifyJob.state || 'started').replace(/_/g, ' ')}` : 'Launch failed'}
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={() => {
                              if (!identifyOverlayKey) return;
                              setDismissedIdentifyOverlayKey(identifyOverlayKey);
                            }}
                            style={{ width: 22, height: 22, borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer', padding: 0 }}
                          >
                            ×
                          </button>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          Visible nodes {latestIdentifyJob?.acceptedNodes ?? identifyVisibleNodeRefs.length} • Visible routes {latestIdentifyJob?.acceptedArtifacts ?? identifyVisibleArtifactKeys.length} • Findings {latestIdentifyJob?.findingsApplied ?? 0} • Events {latestIdentifyJob?.eventsPersisted ?? 0}
                        </div>
                        {identifyTouchedRouteLabels.length > 0 && (
                          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                            Touched routes: {identifyTouchedRouteLabels.join(' • ')}
                          </div>
                        )}
                        {identifyErrorLines.length > 0 && (
                          <div style={{ fontSize: 11, lineHeight: 1.45, color: isDark ? '#fecaca' : '#991b1b' }}>
                            {identifyErrorLines.join(' • ')}
                          </div>
                        )}
                      </div>
                    )}
                </div>,
                mapDrawerHost,
              ) : null}
            </div>
          </div>
        </div>

        <LensBottomPaletteTrigger
          open={searchOpen}
          onClick={() => toggleSearchPalette()}
          title={searchOpen ? 'Close map search (Ctrl+K)' : 'Find object or supply chain (Ctrl+K)'}
          ariaLabel={searchOpen ? 'Close map search palette' : 'Open map search palette'}
          chrome={mapSearchPaletteChrome}
          bottom={10}
          zIndex={4}
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
        </LensBottomPaletteTrigger>

        <LensBottomPaletteShell
          open={searchOpen}
          onBackdropClick={closeSearchPalette}
          chrome={mapSearchPaletteChrome}
          width={520}
          maxWidth="90vw"
          paddingBottom={72}
          zIndex={24}
        >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12, padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    {(['objects', 'supply-links'] as const).map((mode) => {
                      const active = searchMode === mode;
                      return (
                        <button
                          key={mode}
                          type="button"
                          onClick={() => setSearchMode(mode)}
                          style={{
                            borderRadius: 999,
                            border: active ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                            background: active ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                            color: active ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                            padding: '6px 10px',
                            fontSize: 12,
                            fontWeight: 700,
                            cursor: 'pointer',
                          }}
                        >
                          {mode === 'objects' ? 'Objects' : 'Supply links'}
                        </button>
                      );
                    })}
                  </div>
                  {searchMode === 'objects' ? (
                    <div style={{ display: 'flex', alignItems: 'center' }}>
                      <div style={{ marginRight: 12, color: 'var(--text-secondary-strong)', display: 'flex' }}>
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                          <circle cx="11" cy="11" r="7" />
                          <path d="m20 20-3.5-3.5" />
                        </svg>
                      </div>
                      <input
                        ref={searchInputRef}
                        value={searchQuery}
                        onChange={(event) => setSearchQuery(event.target.value)}
                        onKeyDown={handleSearchKeyDown}
                        placeholder="Find object, zone, org, facility..."
                        autoComplete="off"
                        spellCheck={false}
                        style={{
                          flex: 1,
                          border: 'none',
                          outline: 'none',
                          background: 'transparent',
                          color: 'var(--text-heading)',
                          fontSize: 16,
                          fontWeight: 500,
                          lineHeight: 1.4,
                        }}
                      />
                    </div>
                  ) : (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                      <div style={{ position: 'relative' }}>
                        <input
                          value={supplyAnchorAQuery}
                          onFocus={() => setShowAnchorASuggestions(true)}
                          onBlur={() => window.setTimeout(() => setShowAnchorASuggestions(false), 120)}
                          onChange={(event) => {
                            setSupplyAnchorAQuery(event.target.value);
                            setSupplyAnchorA(null);
                            setShowAnchorASuggestions(true);
                          }}
                          onKeyDown={handleSupplySearchKeyDown}
                          placeholder="Object A"
                          autoComplete="off"
                          spellCheck={false}
                          style={{
                            width: '100%',
                            borderRadius: 12,
                            border: '1px solid var(--border-subtle)',
                            background: mapSearchPaletteChrome.subtleSurface,
                            color: 'var(--text-heading)',
                            fontSize: 14,
                            fontWeight: 500,
                            lineHeight: 1.4,
                            padding: '10px 12px',
                          }}
                        />
                        {showAnchorASuggestions && (anchorASuggestionsLoading || anchorASuggestions.length > 0) && (
                          <div
                            style={{
                              position: 'absolute',
                              top: 'calc(100% + 6px)',
                              left: 0,
                              right: 0,
                              zIndex: 28,
                              borderRadius: 12,
                              border: '1px solid var(--border-subtle)',
                              background: 'var(--bg-elevated)',
                              boxShadow: mapSearchPaletteChrome.panelShadow,
                              overflowX: 'hidden',
                              overflowY: 'auto',
                              maxHeight: 260,
                            }}
                          >
                            {anchorASuggestionsLoading && (
                              <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                                Resolving object A...
                              </div>
                            )}
                            {!anchorASuggestionsLoading && anchorASuggestions.map((entity) => (
                              <button
                                key={entity.id || entity.canonicalPath}
                                type="button"
                                onMouseDown={(event) => event.preventDefault()}
                                onClick={() => selectAnchorA(entity)}
                                style={{
                                  width: '100%',
                                  textAlign: 'left',
                                  padding: '10px 12px',
                                  border: 'none',
                                  borderBottom: '1px solid var(--border-subtle)',
                                  background: 'transparent',
                                  cursor: 'pointer',
                                }}
                              >
                                <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>{entity.name || humanizeCanonicalPath(entity.canonicalPath)}</div>
                                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{entity.type} · {entity.canonicalPath}</div>
                              </button>
                            ))}
                          </div>
                        )}
                      </div>
                      {supplyAnchorA?.canonicalPath && (
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          A: {supplyAnchorA.name} · {supplyAnchorA.canonicalPath}
                        </div>
                      )}
                      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                        <input
                          type="checkbox"
                          checked={betweenAnchors}
                          onChange={(event) => setBetweenAnchors(event.target.checked)}
                        />
                        Between A and B
                      </label>
                      {betweenAnchors && (
                        <>
                          <div style={{ position: 'relative' }}>
                            <input
                              value={supplyAnchorBQuery}
                              onFocus={() => setShowAnchorBSuggestions(true)}
                              onBlur={() => window.setTimeout(() => setShowAnchorBSuggestions(false), 120)}
                              onChange={(event) => {
                                setSupplyAnchorBQuery(event.target.value);
                                setSupplyAnchorB(null);
                                setShowAnchorBSuggestions(true);
                              }}
                              onKeyDown={handleSupplySearchKeyDown}
                              placeholder="Object B"
                              autoComplete="off"
                              spellCheck={false}
                              style={{
                                width: '100%',
                                borderRadius: 12,
                                border: '1px solid var(--border-subtle)',
                                background: mapSearchPaletteChrome.subtleSurface,
                                color: 'var(--text-heading)',
                                fontSize: 14,
                                fontWeight: 500,
                                lineHeight: 1.4,
                                padding: '10px 12px',
                              }}
                            />
                            {showAnchorBSuggestions && (anchorBSuggestionsLoading || anchorBSuggestions.length > 0) && (
                              <div
                                style={{
                                  position: 'absolute',
                              top: 'calc(100% + 6px)',
                              left: 0,
                              right: 0,
                              zIndex: 28,
                              borderRadius: 12,
                              border: '1px solid var(--border-subtle)',
                              background: 'var(--bg-elevated)',
                              boxShadow: mapSearchPaletteChrome.panelShadow,
                              overflowX: 'hidden',
                              overflowY: 'auto',
                              maxHeight: 260,
                            }}
                          >
                                {anchorBSuggestionsLoading && (
                                  <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                                    Resolving object B...
                                  </div>
                                )}
                                {!anchorBSuggestionsLoading && anchorBSuggestions.map((entity) => (
                                  <button
                                    key={entity.id || entity.canonicalPath}
                                    type="button"
                                    onMouseDown={(event) => event.preventDefault()}
                                    onClick={() => selectAnchorB(entity)}
                                    style={{
                                      width: '100%',
                                      textAlign: 'left',
                                      padding: '10px 12px',
                                      border: 'none',
                                      borderBottom: '1px solid var(--border-subtle)',
                                      background: 'transparent',
                                      cursor: 'pointer',
                                    }}
                                  >
                                    <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>{entity.name || humanizeCanonicalPath(entity.canonicalPath)}</div>
                                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{entity.type} · {entity.canonicalPath}</div>
                                  </button>
                                ))}
                              </div>
                            )}
                          </div>
                          {supplyAnchorB?.canonicalPath && (
                            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                              B: {supplyAnchorB.name} · {supplyAnchorB.canonicalPath}
                            </div>
                          )}
                        </>
                      )}
                      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                        <label style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                          Max legs: <strong style={{ color: 'var(--text-heading)' }}>{supplyMaxLegs}</strong>
                        </label>
                        <input
                          type="range"
                          min={1}
                          max={12}
                          value={supplyMaxLegs}
                          onChange={(event) => setSupplyMaxLegs(Number(event.target.value))}
                          style={{ flex: 1 }}
                        />
                        <button
                          type="button"
                          onClick={() => void handleRunSupplySearch()}
                          style={{
                            borderRadius: 10,
                            border: `1px solid ${mapSearchPaletteChrome.activeItemBorder}`,
                            background: mapSearchPaletteChrome.activeItemBackground,
                            color: 'var(--text-heading)',
                            padding: '8px 10px',
                            fontSize: 12,
                            fontWeight: 700,
                            cursor: 'pointer',
                          }}
                        >
                          Search
                        </button>
                      </div>
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => {
                      closeSearchPalette();
                    }}
                    style={{
                      alignSelf: 'flex-end',
                      padding: '4px 10px',
                      borderRadius: 999,
                      border: '1px solid var(--border-subtle)',
                      background: mapSearchPaletteChrome.subtleSurface,
                      color: 'var(--text-secondary-strong)',
                      cursor: 'pointer',
                      fontSize: 11,
                      fontWeight: 700,
                      lineHeight: 1,
                    }}
                  >
                    Esc
                  </button>
                </div>

                <div style={{ maxHeight: 400, overflowY: 'auto', padding: '6px 0' }}>
                  {searchMode === 'objects' && !searchQueryTrim && (
                    <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                      Type to search loaded map objects and jump the camera to the selection.
                    </div>
                  )}

                  {searchMode === 'objects' && searchQueryTrim && (
                    <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                      Loaded Objects
                    </div>
                  )}

                  {searchMode === 'objects' && (searchSuggestions.length > 0 ? searchSuggestions.map((item, index) => {
                    const active = index === searchActiveIndex;
                    return (
                      <button
                        key={item.object.id || item.object.canonicalPath}
                        type="button"
                        onMouseEnter={() => setSearchActiveIndex(index)}
                        onClick={() => focusObjectOnMap(item.object)}
                        style={{
                          width: '100%',
                          textAlign: 'left',
                          padding: '10px 16px',
                          border: 'none',
                          borderLeft: `3px solid ${active ? mapSearchPaletteChrome.activeItemBorder : 'transparent'}`,
                          background: active
                            ? mapSearchPaletteChrome.activeItemBackground
                            : 'transparent',
                          cursor: 'pointer',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 2,
                          transition: 'background 0.1s',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ fontSize: 14, fontWeight: 650, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {item.title}
                          </div>
                          {item.object.inScope === false && (
                            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: mapSearchPaletteChrome.subtleSurface }}>
                              Neighbor
                            </div>
                          )}
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--text-secondary)', opacity: 0.9, wordBreak: 'break-word' }}>
                          {item.subtitle}
                        </div>
                      </button>
                    );
                  }) : searchQueryTrim ? (
                    <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                      No loaded map objects match this query.
                    </div>
                  ) : null)}

                  {searchMode === 'supply-links' && (
                    <>
                      {supplySearchLoading && (
                        <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                          Searching supply-chain paths...
                        </div>
                      )}
                      {!supplySearchLoading && !supplySearchResults.length && !supplySearchError && (
                        <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                          Resolve object A, optionally object B, then search materialized supply links.
                        </div>
                      )}
                      {supplySearchError && (
                        <div style={{ padding: '18px 24px', color: '#fca5a5', fontSize: 13 }}>
                          {supplySearchError}
                        </div>
                      )}
                      {supplySearchResults.map((result, index) => {
                        const active = index === searchActiveIndex;
                        const artifact = result.artifact as MaterializedArtifact;
                        return (
                          <button
                            key={artifact.id || `${artifact.displayName}-${index}`}
                            type="button"
                            onMouseEnter={() => setSearchActiveIndex(index)}
                            onClick={() => handleSelectSupplySearchResult(result)}
                            style={{
                              width: '100%',
                              textAlign: 'left',
                              padding: '12px 16px',
                              border: 'none',
                              borderLeft: `3px solid ${active ? mapSearchPaletteChrome.activeItemBorder : 'transparent'}`,
                              background: active ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                              cursor: 'pointer',
                              display: 'flex',
                              flexDirection: 'column',
                              gap: 4,
                            }}
                          >
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                              <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-heading)' }}>
                                {artifact.displayName || humanizeCanonicalPath(String(artifact.canonicalKey || '')) || 'Supply chain'}
                              </div>
                              {artifact.isPinned && (
                                <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: mapSearchPaletteChrome.subtleSurface }}>
                                  Pinned
                                </div>
                              )}
                            </div>
                            <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                              {artifact.anchorSupplier || 'upstream'} → {artifact.anchorCustomer || 'downstream'}
                            </div>
                            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                              {(result.match?.clipped ? 'Clipped preview' : 'Full preview')}
                              {typeof result.match?.legSpan === 'number' ? ` • matched ${result.match.legSpan} legs` : ''}
                              {typeof artifact.pathStressScore === 'number' && artifact.pathStressScore > 0 ? ` • stress ${artifact.pathStressScore.toFixed(2)}` : ''}
                              {typeof artifact.bindingScore === 'number' && artifact.bindingScore > 0 ? ` • bind ${artifact.bindingScore.toFixed(2)}` : ''}
                            </div>
                          </button>
                        );
                      })}
                    </>
                  )}
                </div>
        </LensBottomPaletteShell>

        <div style={{
          position: 'absolute', left: 10, bottom: 10, padding: '6px 10px', borderRadius: 8,
          backgroundColor: isDark ? 'rgba(5, 11, 17, 0.78)' : 'rgba(248, 250, 252, 0.92)',
          border: isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.15)',
          color: 'var(--text-secondary-strong)', fontSize: 11,
        }}>
          {filterActive
            ? `Shown: ${filteredObjects.length} / ${allObjects.length}`
            : `Objects: ${allObjects.length}`}
          {zonesEnabled ? ` | Zones: ${visibleStaticZonesCount}` : ''}
          {flowsEnabled ? ` | Flows: ${visibleTransportFlowsCount}` : ''}
          {pathLegsEnabled ? ` | Path legs: ${pathLegStats.legCount}` : ''}
          {routeCatalog.length > 0 ? ' | Dotted route: path exists, exact geometry unknown' : ''}
          {vesselsEnabled ? ' | Vessels (AIS)' : ''}
        </div>

        {chatMapFocus && (chatFocusedObjects.length > 0 || chatFocusedArtifactIdSet.size > 0) && (
          <div
            style={{
              position: 'absolute',
              left: 10,
              top: Math.max(12, overlayTopOffset - 54),
              zIndex: 19,
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '10px 12px',
              borderRadius: 14,
              backdropFilter: 'blur(14px)',
              background: isDark ? 'rgba(6,18,31,0.86)' : 'rgba(255,255,255,0.92)',
              border: isDark ? '1px solid rgba(102,153,153,0.24)' : '1px solid rgba(15,23,42,0.08)',
              boxShadow: isDark ? '0 12px 32px rgba(0,0,0,0.3)' : '0 12px 28px rgba(15,23,42,0.12)',
              color: 'var(--text-heading)',
              maxWidth: 340,
            }}
          >
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Chat Focus
              </div>
              <div style={{ marginTop: 3, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                {chatFocusedObjects.length > 0 ? `${chatFocusedObjects.length} map objects` : '0 map objects'}
                {chatFocusedArtifactIdSet.size > 0 ? ` • ${chatFocusedArtifactIdSet.size} routes` : ''}
              </div>
            </div>
            <button
              type="button"
              onClick={() => onClearChatMapFocus?.()}
              style={{
                borderRadius: 999,
                border: '1px solid var(--border-subtle)',
                background: 'transparent',
                color: 'var(--text-secondary-strong)',
                padding: '7px 10px',
                fontSize: 11,
                fontWeight: 700,
                cursor: 'pointer',
                whiteSpace: 'nowrap',
              }}
            >
              Clear
            </button>
          </div>
        )}

        {denseCaptionGroups.map((group) => {
          const isExpanded = expandedDenseCaptionGroupId === group.id;
          const chipLeft = Math.max(12, Math.min(group.x, (map?.getCanvas().clientWidth || 0) - 12));
          const chipTop = Math.max(18, group.y - 18);
          const chipBorder = isExpanded
            ? (isDark ? '1.5px solid rgba(245,158,11,0.78)' : '1.5px solid rgba(217,119,6,0.52)')
            : (isDark ? '1.5px solid rgba(245,158,11,0.58)' : '1.5px solid rgba(217,119,6,0.38)');
          const chipShadow = isExpanded
            ? (isDark ? '0 0 0 1px rgba(245,158,11,0.16), 0 10px 24px rgba(0,0,0,0.28)' : '0 0 0 1px rgba(245,158,11,0.12), 0 10px 24px rgba(15,23,42,0.1)')
            : (isDark ? '0 0 0 1px rgba(245,158,11,0.1), 0 10px 24px rgba(0,0,0,0.28)' : '0 0 0 1px rgba(245,158,11,0.08), 0 10px 24px rgba(15,23,42,0.1)');
          return (
            <div
              key={group.id}
              style={{
                position: 'absolute',
                left: chipLeft,
                top: chipTop,
                zIndex: 21,
                transform: 'translate(-50%, -100%)',
                pointerEvents: 'auto',
              }}
              data-dense-caption-overlay="true"
            >
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  setExpandedDenseCaptionGroupId((current) => current === group.id ? null : group.id);
                }}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  maxWidth: 248,
                  padding: '6px 10px',
                  borderRadius: 999,
                  border: chipBorder,
                  background: isDark ? 'rgba(6,14,22,0.84)' : 'rgba(255,255,255,0.88)',
                  color: 'var(--text-heading)',
                  boxShadow: chipShadow,
                  backdropFilter: 'blur(10px)',
                  cursor: 'pointer',
                  fontSize: 11,
                  fontWeight: 700,
                  lineHeight: 1.25,
                  textAlign: 'left',
                }}
              >
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {group.label}
                </span>
              </button>

              {isExpanded && (
                <div
                  style={{
                    marginTop: 6,
                    width: 292,
                    maxWidth: 'min(calc(100vw - 24px), 292px)',
                    maxHeight: 280,
                    overflowY: 'auto',
                    padding: 8,
                    borderRadius: 16,
                    border: isDark ? '1px solid rgba(255,255,255,0.08)' : '1px solid rgba(15,23,42,0.08)',
                    background: isDark ? 'rgba(5,14,24,0.88)' : 'rgba(255,255,255,0.9)',
                    boxShadow: isDark ? '0 18px 42px rgba(0,0,0,0.32)' : '0 18px 38px rgba(15,23,42,0.12)',
                    backdropFilter: 'blur(18px)',
                    overflowX: 'hidden',
                  }}
                  onClick={(event) => event.stopPropagation()}
                >
                  <div
                    style={{
                      marginBottom: 8,
                      padding: '2px 4px 0',
                      display: 'flex',
                      alignItems: 'baseline',
                      justifyContent: 'space-between',
                      gap: 10,
                    }}
                  >
                    <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                      {group.totalCount} objects
                    </div>
                    <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                      Tap to focus
                    </div>
                  </div>

                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)',
                      gap: 6,
                    }}
                  >
                    {group.visibleObjects.map((object) => {
                      const { accent, visualType } = denseCaptionObjectAccent(object, theme);
                      return (
                        <button
                          key={object.id}
                          type="button"
                          onClick={() => {
                            setExpandedDenseCaptionGroupId(null);
                            selectObjectFromMapInteraction(object);
                          }}
                          title={getSearchObjectTitle(object)}
                          style={{
                            width: '100%',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 6,
                            minWidth: 0,
                            padding: '5px 7px',
                            borderRadius: 10,
                            border: isDark ? '1px solid rgba(255,255,255,0.06)' : '1px solid rgba(15,23,42,0.06)',
                            background: isDark ? 'rgba(255,255,255,0.035)' : 'rgba(15,23,42,0.03)',
                            color: 'var(--text-heading)',
                            cursor: 'pointer',
                            textAlign: 'left',
                            transition: 'background-color 140ms ease, border-color 140ms ease, transform 140ms ease',
                          }}
                          onMouseEnter={(event) => {
                            event.currentTarget.style.background = isDark ? `${accent}20` : `${accent}14`;
                            event.currentTarget.style.borderColor = isDark ? `${accent}44` : `${accent}2e`;
                          }}
                          onMouseLeave={(event) => {
                            event.currentTarget.style.background = isDark ? 'rgba(255,255,255,0.035)' : 'rgba(15,23,42,0.03)';
                            event.currentTarget.style.borderColor = isDark ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)';
                          }}
                        >
                          <span
                            style={{
                              width: 18,
                              height: 18,
                              borderRadius: 999,
                              flexShrink: 0,
                              display: 'grid',
                              placeItems: 'center',
                              background: isDark ? `${accent}14` : `${accent}12`,
                              border: `1px solid ${isDark ? `${accent}40` : `${accent}24`}`,
                            }}
                          >
                            <HorizonNodeIcon type={visualType} color={accent} size={10} />
                          </span>
                          <span style={{ minWidth: 0, flex: 1 }}>
                            <span
                              style={{
                                display: 'block',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                fontSize: 11,
                                fontWeight: 600,
                                lineHeight: 1.2,
                                whiteSpace: 'nowrap',
                              }}
                            >
                              {getSearchObjectTitle(object)}
                            </span>
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          );
        })}

        {stackSelection && (
          <MapObjectStackPanel
            objects={stackSelection.objects}
            isDark={isDark}
            paletteTheme={theme}
            topOffset={overlayTopOffset}
            priorityObjectId={searchHighlightedObjectId}
            onClose={clearStackSelection}
            onShowFan={stackSelection.canSpiderfy ? stackSelection.spiderfy : undefined}
            onSelectObject={(object) => {
              selectObjectFromMapInteraction(object);
            }}
          />
        )}

        {manualChainOverlayOpen && activeManualChain?.artifact && (
          manualChainOverlayCollapsed ? (
            <div
              style={{
                position: 'absolute',
                right: 12,
                top: 70,
                zIndex: 22,
                pointerEvents: 'auto',
              }}
            >
              <button
                type="button"
                onClick={() => setManualChainOverlayCollapsed(false)}
                title={`Show ${String(activeManualChain.artifact.displayName || 'Supply chain').trim()}`}
                aria-label={`Show ${String(activeManualChain.artifact.displayName || 'Supply chain').trim()}`}
                style={mapDockButtonStyle(isDark, true, mapLayerColor('supply', theme))}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M4 7h11" />
                  <path d="M4 12h16" />
                  <path d="M4 17h9" />
                  <path d="m16 4 4 8-4 8" />
                </svg>
              </button>
            </div>
          ) : (
          <div
            style={{
              position: 'absolute',
              right: 10,
              top: 70,
              width: 360,
              maxWidth: 'calc(100vw - 20px)',
              borderRadius: 16,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: isDark ? '0 20px 60px rgba(0,0,0,0.45)' : '0 20px 60px rgba(15,23,42,0.18)',
              padding: 16,
              zIndex: 22,
              display: 'flex',
              flexDirection: 'column',
              gap: 10,
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 15, fontWeight: 800, color: 'var(--text-heading)' }}>
                  {String(activeManualChain.artifact.displayName || 'Supply chain').trim()}
                </div>
                <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  {activeManualChain.artifact.anchorSupplier || 'upstream'} → {activeManualChain.artifact.anchorCustomer || 'downstream'}
                </div>
              </div>
              <button
                type="button"
                onClick={closeManualChainOverlay}
                title="Collapse supply chain preview"
                aria-label="Collapse supply chain preview"
                style={{
                  borderRadius: 999,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  width: 30,
                  height: 30,
                  cursor: 'pointer',
                }}
              >
                ×
              </button>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              {activeManualChain.match?.clipped ? 'Preview is clipped around the matched anchors.' : 'Full chain preview is displayed on the map.'}
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              {typeof activeManualChain.artifact.pathStressScore === 'number' && activeManualChain.artifact.pathStressScore > 0 ? `Stress ${activeManualChain.artifact.pathStressScore.toFixed(2)}` : 'Stress n/a'}
              {typeof activeManualChain.artifact.bindingScore === 'number' && activeManualChain.artifact.bindingScore > 0 ? ` • Binding ${activeManualChain.artifact.bindingScore.toFixed(2)}` : ''}
              {typeof activeManualChain.match?.legSpan === 'number' ? ` • Matches ${activeManualChain.match.legSpan}` : ''}
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button
                type="button"
                onClick={() => void handleAddChainToScope(activeManualChain)}
                style={{
                  borderRadius: 10,
                  border: `1px solid ${mapSearchPaletteChrome.activeItemBorder}`,
                  background: mapSearchPaletteChrome.activeItemBackground,
                  color: 'var(--text-heading)',
                  padding: '9px 12px',
                  fontSize: 12,
                  fontWeight: 700,
                  cursor: 'pointer',
                }}
              >
                {scope?.lensId ? 'Add full chain to current lens' : 'Create lens from chain'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setSearchMode('supply-links');
                  setSearchOpen(true);
                }}
                style={{
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  padding: '9px 12px',
                  fontSize: 12,
                  fontWeight: 700,
                  cursor: 'pointer',
                }}
              >
                Open search results
              </button>
            </div>
            {supplySearchResults.length > 1 && (
              <div style={{ marginTop: 2, display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ fontSize: 11, fontWeight: 800, letterSpacing: 0.3, textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                  Search Results
                </div>
                <div style={{ maxHeight: 220, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {supplySearchResults.map((result) => {
                    const artifact = result.artifact as MaterializedArtifact;
                    const active = artifact?.id === (activeManualChain.artifact as MaterializedArtifact)?.id;
                    return (
                      <div
                        key={artifact.id}
                        style={{
                          borderRadius: 12,
                          border: active ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                          background: active ? mapSearchPaletteChrome.activeItemBackground : mapSearchPaletteChrome.subtleSurface,
                          padding: '10px 12px',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 6,
                        }}
                      >
                        <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                          {artifact.displayName || humanizeCanonicalPath(String(artifact.canonicalKey || '')) || 'Supply chain'}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          {artifact.anchorSupplier || 'upstream'} → {artifact.anchorCustomer || 'downstream'}
                        </div>
                        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                          {!active && (
                            <button
                              type="button"
                              onClick={() => activateSupplySearchResult(result, 'replace')}
                              style={{
                                borderRadius: 8,
                                border: '1px solid var(--border-subtle)',
                                background: 'transparent',
                                color: 'var(--text-secondary-strong)',
                                padding: '6px 8px',
                                fontSize: 11,
                                fontWeight: 700,
                                cursor: 'pointer',
                              }}
                            >
                              Replace preview
                            </button>
                          )}
                          <button
                            type="button"
                            onClick={() => activateSupplySearchResult(result, 'append')}
                            style={{
                              borderRadius: 8,
                              border: '1px solid var(--border-subtle)',
                              background: 'transparent',
                              color: 'var(--text-secondary-strong)',
                              padding: '6px 8px',
                              fontSize: 11,
                              fontWeight: 700,
                              cursor: 'pointer',
                            }}
                          >
                            Show together
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
          )
        )}

        {scopeDialogOpen && (
          <div
            ref={scopeDialogBackdropRef}
            role="dialog"
            aria-modal="true"
            onClick={(event) => {
              if (event.target === scopeDialogBackdropRef.current && !scopeDialogSubmitting) {
                closeScopeDialog();
              }
            }}
            style={{
              position: 'absolute',
              inset: 0,
              zIndex: 30,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 20,
              background: isDark ? 'rgba(0,0,0,0.45)' : 'rgba(15,23,42,0.16)',
              backdropFilter: 'blur(4px)',
            }}
          >
            <div
              style={{
                width: 520,
                maxWidth: 'calc(100vw - 24px)',
                borderRadius: 18,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                boxShadow: isDark ? '0 24px 70px rgba(0,0,0,0.55)' : '0 24px 70px rgba(15,23,42,0.22)',
                padding: 18,
                display: 'flex',
                flexDirection: 'column',
                gap: 14,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                <div>
                  <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--text-heading)' }}>
                    {scope?.lensId ? 'Add Full Chain To Scope' : 'Create Lens From Chain'}
                  </div>
                  <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    {String((activeManualChain?.artifact as MaterializedArtifact | undefined)?.displayName || 'Supply chain').trim()}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={closeScopeDialog}
                  disabled={scopeDialogSubmitting}
                  style={{
                    width: 32,
                    height: 32,
                    borderRadius: 999,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    cursor: scopeDialogSubmitting ? 'not-allowed' : 'pointer',
                  }}
                >
                  ×
                </button>
              </div>

              {scopeDialogCreatedLensId ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    {scope?.lensId ? 'The chain has been added to the selected lens.' : 'The chain lens is ready.'}
                  </div>
                  <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
                    <button
                      type="button"
                      onClick={handleOpenCreatedLens}
                      style={{
                        borderRadius: 10,
                        border: `1px solid ${mapSearchPaletteChrome.activeItemBorder}`,
                        background: mapSearchPaletteChrome.activeItemBackground,
                        color: 'var(--text-heading)',
                        padding: '10px 12px',
                        fontSize: 12,
                        fontWeight: 800,
                        cursor: 'pointer',
                      }}
                    >
                      Open lens
                    </button>
                    <button
                      type="button"
                      onClick={closeScopeDialog}
                      style={{
                        borderRadius: 10,
                        border: '1px solid var(--border-subtle)',
                        background: 'transparent',
                        color: 'var(--text-secondary-strong)',
                        padding: '10px 12px',
                        fontSize: 12,
                        fontWeight: 700,
                        cursor: 'pointer',
                      }}
                    >
                      Close
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  {scope?.lensId ? (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                        <button
                          type="button"
                          onClick={() => {
                            setScopeDialogUseCurrentLens(true);
                            setScopeDialogTargetMode('existing');
                            setScopeDialogLensId(scope.lensId || '');
                          }}
                          style={{
                            borderRadius: 999,
                            border: scopeDialogUseCurrentLens ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                            background: scopeDialogUseCurrentLens ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                            color: scopeDialogUseCurrentLens ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                            padding: '7px 10px',
                            fontSize: 12,
                            fontWeight: 700,
                            cursor: 'pointer',
                          }}
                        >
                          Current lens
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            setScopeDialogUseCurrentLens(false);
                            setScopeDialogTargetMode('existing');
                          }}
                          style={{
                            borderRadius: 999,
                            border: !scopeDialogUseCurrentLens ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                            background: !scopeDialogUseCurrentLens ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                            color: !scopeDialogUseCurrentLens ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                            padding: '7px 10px',
                            fontSize: 12,
                            fontWeight: 700,
                            cursor: 'pointer',
                          }}
                        >
                          Another lens
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <button
                        type="button"
                        onClick={() => setScopeDialogTargetMode('new')}
                        style={{
                          borderRadius: 999,
                          border: scopeDialogTargetMode === 'new' ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                          background: scopeDialogTargetMode === 'new' ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                          color: scopeDialogTargetMode === 'new' ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                          padding: '7px 10px',
                          fontSize: 12,
                          fontWeight: 700,
                          cursor: 'pointer',
                        }}
                      >
                        New lens
                      </button>
                      <button
                        type="button"
                        onClick={() => setScopeDialogTargetMode('existing')}
                        style={{
                          borderRadius: 999,
                          border: scopeDialogTargetMode === 'existing' ? `1px solid ${mapSearchPaletteChrome.activeItemBorder}` : '1px solid var(--border-subtle)',
                          background: scopeDialogTargetMode === 'existing' ? mapSearchPaletteChrome.activeItemBackground : 'transparent',
                          color: scopeDialogTargetMode === 'existing' ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                          padding: '7px 10px',
                          fontSize: 12,
                          fontWeight: 700,
                          cursor: 'pointer',
                        }}
                      >
                        Existing lens
                      </button>
                    </div>
                  )}

                  {(!scope?.lensId || !scopeDialogUseCurrentLens) && (
                    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary-strong)' }}>Project</span>
                      <select
                        value={scopeDialogProjectId}
                        onChange={(event) => {
                          setScopeDialogProjectId(event.target.value);
                          setScopeDialogLensId('');
                        }}
                        style={{
                          borderRadius: 12,
                          border: '1px solid var(--border-subtle)',
                          background: mapSearchPaletteChrome.subtleSurface,
                          color: 'var(--text-heading)',
                          padding: '10px 12px',
                          fontSize: 13,
                        }}
                      >
                        <option value="">Choose project</option>
                        {scopeDialogProjects.map((project) => (
                          <option key={project.id} value={project.id}>
                            {project.displayName || project.id}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}

                  {scopeDialogTargetMode === 'new' && !scope?.lensId && (
                    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary-strong)' }}>Lens name</span>
                      <input
                        value={scopeDialogLensName}
                        onChange={(event) => setScopeDialogLensName(event.target.value)}
                        placeholder="Supply chain lens"
                        style={{
                          borderRadius: 12,
                          border: '1px solid var(--border-subtle)',
                          background: mapSearchPaletteChrome.subtleSurface,
                          color: 'var(--text-heading)',
                          padding: '10px 12px',
                          fontSize: 13,
                        }}
                      />
                    </label>
                  )}

                  {scopeDialogTargetMode === 'existing' && (!scope?.lensId || !scopeDialogUseCurrentLens) && (
                    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary-strong)' }}>Lens</span>
                      <select
                        value={scopeDialogLensId}
                        onChange={(event) => setScopeDialogLensId(event.target.value)}
                        style={{
                          borderRadius: 12,
                          border: '1px solid var(--border-subtle)',
                          background: mapSearchPaletteChrome.subtleSurface,
                          color: 'var(--text-heading)',
                          padding: '10px 12px',
                          fontSize: 13,
                        }}
                      >
                        <option value="">Choose lens</option>
                        {scopeDialogLenses.map((lens) => (
                          <option key={lens.id} value={lens.id}>
                            {lens.name}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}

                  {scopeDialogError && (
                    <div style={{ fontSize: 12, color: '#fca5a5' }}>
                      {scopeDialogError}
                    </div>
                  )}

                  <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                    <button
                      type="button"
                      onClick={closeScopeDialog}
                      disabled={scopeDialogSubmitting}
                      style={{
                        borderRadius: 10,
                        border: '1px solid var(--border-subtle)',
                        background: 'transparent',
                        color: 'var(--text-secondary-strong)',
                        padding: '10px 12px',
                        fontSize: 12,
                        fontWeight: 700,
                        cursor: scopeDialogSubmitting ? 'not-allowed' : 'pointer',
                      }}
                    >
                      Cancel
                    </button>
                    <button
                      type="button"
                      onClick={() => void handleConfirmAddChainToScope()}
                      disabled={scopeDialogSubmitting}
                      style={{
                        borderRadius: 10,
                        border: `1px solid ${mapSearchPaletteChrome.activeItemBorder}`,
                        background: mapSearchPaletteChrome.activeItemBackground,
                        color: 'var(--text-heading)',
                        padding: '10px 12px',
                        fontSize: 12,
                        fontWeight: 800,
                        cursor: scopeDialogSubmitting ? 'not-allowed' : 'pointer',
                      }}
                    >
                      {scopeDialogSubmitting
                        ? 'Saving...'
                        : scope?.lensId
                          ? (scopeDialogUseCurrentLens ? 'Add to current lens' : 'Add to selected lens')
                          : (scopeDialogTargetMode === 'new' ? 'Create lens' : 'Add to existing lens')}
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {simulationStressEnabled && primarySimulationOverlay && primarySimulationRunId && primarySimulationLensId && (
          <SimulationExplanationOverlay
            isDark={isDark}
            lensId={primarySimulationLensId}
            runId={primarySimulationRunId}
            overlay={primarySimulationOverlay}
            state={primarySimulationExplanationState}
            additionalSelectedCount={Math.max(0, (simulationSelection?.activeItems?.length || 0) - 1)}
          />
        )}

        {selectedObject && !stackSelection && (
          <div
            style={{
              position: 'absolute',
              right: 12,
              bottom: 12,
              zIndex: 18,
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
              padding: '10px 12px',
              borderRadius: 14,
              backdropFilter: 'blur(14px)',
              background: isDark ? 'rgba(6,18,31,0.82)' : 'rgba(255,255,255,0.9)',
              border: isDark ? '1px solid rgba(148,163,184,0.18)' : '1px solid rgba(15,23,42,0.08)',
              boxShadow: isDark ? '0 12px 32px rgba(0,0,0,0.3)' : '0 12px 28px rgba(15,23,42,0.12)',
              color: 'var(--text-heading)',
              minWidth: 206,
            }}
          >
            <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
              Active map layers
            </div>
            {[
              { label: 'Direct links', note: `${traceSummary?.directCount ?? edgesCount} core relations`, color: mapLayerColor('direct', theme) },
              { label: 'Business context', note: `${traceSummary?.contextCount ?? 0} surrounding links`, color: mapLayerColor('context', theme) },
              { label: 'Supply routes', note: `${traceSummary?.supplyPathCount ?? 0} mapped routes`, color: mapLayerColor('supply', theme) },
              { label: 'Propagation', note: `${traceSummary?.propagationEdgeCount ?? 0} secondary links`, color: mapLayerColor('propagation', theme) },
              { label: 'Route legs', note: pathLegsEnabled ? `${pathLegStats.legCount} visible` : 'Hidden', color: mapLayerColor('supply', theme) },
            ].map((item) => (
              <div key={item.label} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ width: 10, height: 10, borderRadius: 999, background: item.color, flexShrink: 0 }} />
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 11, fontWeight: 700 }}>{item.label}</div>
                  <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>{item.note}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default WorldMapPanel;
