import type { Edge, Node } from 'reactflow';

export const SEMANTIC_GRAPH_VIEWS = [
  'hierarchical',
  'supply-flow',
  'influence-network',
  'value-chain',
] as const;

export const LEGACY_GRAPH_LAYOUTS = [
  'freeform',
  'linear',
  'levels',
  'supply-chain',
] as const;

export type SemanticGraphView = (typeof SEMANTIC_GRAPH_VIEWS)[number];
export type LegacyGraphLayoutMode = (typeof LEGACY_GRAPH_LAYOUTS)[number];
export type GraphViewId = SemanticGraphView | LegacyGraphLayoutMode;

export type GraphViewHoverMode =
  | 'off'
  | 'control'
  | 'expansion'
  | 'production'
  | 'supplies'
  | 'competition'
  | 'decision-chain'
  | 'signal-propagation';

export type GeoAnchorKind =
  | 'country'
  | 'region'
  | 'bloc'
  | 'global'
  | 'composite'
  | 'unknown';

export interface GeoAnchorMeta {
  kind: GeoAnchorKind;
  id: string;
  label: string;
  members?: string[];
}

export type SupplyFlowStage =
  | 'upstream'
  | 'transformation'
  | 'transit'
  | 'destination'
  | 'jurisdiction';

export type SupplyFlowRole =
  | 'source'
  | 'transformer'
  | 'gateway'
  | 'transit'
  | 'destination'
  | 'regulator'
  | 'context';

export type SupplyFlowRouteFamily =
  | 'primary'
  | 'fallback'
  | 'blocked'
  | 'uncertain'
  | 'context';

export type SupplyFlowSupportRole =
  | 'governance'
  | 'ownership'
  | 'support'
  | 'none';

export type InfluenceActorRole =
  | 'family'
  | 'dynasty'
  | 'tribe'
  | 'community'
  | 'organization'
  | 'government'
  | 'regulator'
  | 'person'
  | 'country'
  | 'region'
  | 'market'
  | 'intermediary'
  | 'facility'
  | 'other';

export type InfluenceClusterKind =
  | 'family'
  | 'community'
  | 'organization'
  | 'institution'
  | 'geography'
  | 'market'
  | 'other';

export type InfluenceClass =
  | 'ownership-control'
  | 'board-executive'
  | 'political-access'
  | 'regulatory-leverage'
  | 'commercial-dependence'
  | 'family-community'
  | 'influence-general'
  | 'other';

export type ValueChainStage =
  | 'inputs'
  | 'components'
  | 'subsystems'
  | 'integration'
  | 'operators'
  | 'markets'
  | 'support';

export type ValueChainRole =
  | 'company'
  | 'country'
  | 'product'
  | 'technology'
  | 'facility'
  | 'market'
  | 'operator'
  | 'service'
  | 'regulator'
  | 'support';

export type ValueChainValueClass =
  | 'transforms-into'
  | 'supplies-input'
  | 'enables'
  | 'sells-into'
  | 'governance-context';

export interface HierarchicalViewMeta {
  pane?: 'right' | 'left' | 'dock';
  rightDockEnabled?: Array<'governance' | 'supply' | 'dependencies' | 'structure'>;
  stackGroupKey?: string;
  stackKind?: string;
}

export interface SupplyFlowViewMeta {
  stage: SupplyFlowStage;
  role: SupplyFlowRole;
  lane: string;
  geoAnchor?: GeoAnchorMeta;
  isChokepoint?: boolean;
  isGateway?: boolean;
  routeFamily?: SupplyFlowRouteFamily;
  routeGroup?: string;
  supportRole?: SupplyFlowSupportRole;
  pathDepth?: number;
  confidence?: number;
}

export interface InfluenceViewMeta {
  clusterId: string;
  clusterKind: InfluenceClusterKind;
  clusterLabel?: string;
  actorRole: InfluenceActorRole;
  geoAnchor?: GeoAnchorMeta;
  countryAnchor?: string;
  centralityBucket?: 'low' | 'medium' | 'high';
}

export interface ValueChainViewMeta {
  stage: ValueChainStage;
  role: ValueChainRole;
  swimlane?: string;
  geoAnchor?: GeoAnchorMeta;
  supportRail?: boolean;
  valuePoolBand?: 'low' | 'medium' | 'high';
  bottleneck?: boolean;
}

export interface SharedGraphNodeMeta {
  graphView?: SemanticGraphView;
  geoAnchor?: GeoAnchorMeta;
  hierarchicalMeta?: HierarchicalViewMeta;
  supplyFlowMeta?: SupplyFlowViewMeta;
  influenceMeta?: InfluenceViewMeta;
  valueChainMeta?: ValueChainViewMeta;
}

export interface SharedGraphEdgeMeta {
  graphView?: SemanticGraphView;
  routingHint?: {
    preferredAxis?: 'horizontal' | 'vertical';
    sameLane?: boolean;
    sameCluster?: boolean;
    reverseFlow?: boolean;
    bundleId?: string;
  };
  supplyFlowMeta?: {
    flowClass:
      | 'material-input'
      | 'product-transformation'
      | 'logistics-leg'
      | 'jurisdiction'
      | 'corporate-context'
      | 'route-support';
    variant?: SupplyFlowRouteFamily;
    confidence?: number;
  };
  influenceMeta?: {
    influenceClass: InfluenceClass;
  };
  valueChainMeta?: {
    valueClass: ValueChainValueClass;
  };
}

export interface GraphViewArrangeResult<T = any> {
  nodes: Node<T>[];
  edges: Edge[];
}

export interface GraphViewFilterPolicy {
  semanticGroupFilter: boolean;
  semanticGroupColors: boolean;
}

export interface SupplyFlowGraphViewState {
  routeFamily: SupplyFlowRouteFamily | 'all';
  localFlowFilter: 'all' | 'material-input' | 'product-transformation' | 'logistics-leg' | 'jurisdiction' | 'corporate-context' | 'route-support';
  pinnedNodeIds: string[];
  showContextRail: boolean;
}

export interface InfluenceGraphViewState {
  actorClassFilter: InfluenceActorRole | 'all';
  influenceClassFilter: InfluenceClass | 'all';
  collapsedClusterIds: string[];
  geoOverlay: boolean;
  stableOrderHints: Record<string, number>;
}

export interface ValueChainGraphViewState {
  supportRailVisible: boolean;
  swimlaneMode: 'role' | 'geography' | 'product-family';
  collapsedStages: ValueChainStage[];
  activeOverlay: 'bottlenecks' | 'value-pools' | 'concentration' | 'margin-proxy';
}

export type PersistedGraphViewState = Partial<{
  hierarchical: Record<string, any>;
  'supply-flow': SupplyFlowGraphViewState;
  'influence-network': InfluenceGraphViewState;
  'value-chain': ValueChainGraphViewState;
}>;

export const GRAPH_VIEW_LABELS: Record<GraphViewId, string> = {
  freeform: 'Freeform',
  linear: 'Linear',
  levels: 'Hierarchical',
  'supply-chain': 'Supply Chain',
  hierarchical: 'Hierarchical',
  'supply-flow': 'Supply Flow',
  'influence-network': 'Influence Network',
  'value-chain': 'Value Chain',
};

export function defaultGraphViewState(view: SemanticGraphView): Record<string, any> {
  switch (view) {
    case 'supply-flow':
      return {
        routeFamily: 'all',
        localFlowFilter: 'all',
        pinnedNodeIds: [],
        showContextRail: true,
      } satisfies SupplyFlowGraphViewState;
    case 'influence-network':
      return {
        actorClassFilter: 'all',
        influenceClassFilter: 'all',
        collapsedClusterIds: [],
        geoOverlay: false,
        stableOrderHints: {},
      } satisfies InfluenceGraphViewState;
    case 'value-chain':
      return {
        supportRailVisible: true,
        swimlaneMode: 'role',
        collapsedStages: [],
        activeOverlay: 'bottlenecks',
      } satisfies ValueChainGraphViewState;
    default:
      return {};
  }
}

export function normalizeGraphViewState(raw: unknown): PersistedGraphViewState {
  const viewState = (raw && typeof raw === 'object' ? raw : {}) as Record<string, any>;
  return {
    hierarchical: viewState.hierarchical && typeof viewState.hierarchical === 'object' ? viewState.hierarchical : undefined,
    'supply-flow': {
      ...defaultGraphViewState('supply-flow'),
      ...(viewState['supply-flow'] && typeof viewState['supply-flow'] === 'object' ? viewState['supply-flow'] : {}),
    } as SupplyFlowGraphViewState,
    'influence-network': {
      ...defaultGraphViewState('influence-network'),
      ...(viewState['influence-network'] && typeof viewState['influence-network'] === 'object' ? viewState['influence-network'] : {}),
    } as InfluenceGraphViewState,
    'value-chain': {
      ...defaultGraphViewState('value-chain'),
      ...(viewState['value-chain'] && typeof viewState['value-chain'] === 'object' ? viewState['value-chain'] : {}),
    } as ValueChainGraphViewState,
  };
}

export function normalizeGraphView(raw: unknown, fallback: GraphViewId = 'hierarchical'): GraphViewId {
  const value = String(raw || '').trim().toLowerCase();
  switch (value) {
    case 'freeform':
    case 'linear':
    case 'levels':
    case 'supply-chain':
    case 'hierarchical':
    case 'supply-flow':
    case 'influence-network':
    case 'value-chain':
      return value as GraphViewId;
    default:
      return fallback;
  }
}

export function graphViewToSemantic(view: GraphViewId): SemanticGraphView | null {
  switch (view) {
    case 'levels':
    case 'hierarchical':
      return 'hierarchical';
    case 'supply-chain':
    case 'supply-flow':
      return 'supply-flow';
    case 'influence-network':
      return 'influence-network';
    case 'value-chain':
      return 'value-chain';
    default:
      return null;
  }
}

export function graphViewToLegacyLayoutMode(view: GraphViewId): LegacyGraphLayoutMode {
  switch (view) {
    case 'hierarchical':
      return 'levels';
    case 'supply-flow':
      return 'supply-chain';
    case 'influence-network':
    case 'value-chain':
      return 'supply-chain';
    default:
      return view;
  }
}

export function isHierarchicalGraphView(view: GraphViewId): boolean {
  return view === 'levels' || view === 'hierarchical';
}

export function isSupplyFlowGraphView(view: GraphViewId): boolean {
  return view === 'supply-chain' || view === 'supply-flow';
}

export function isInfluenceNetworkGraphView(view: GraphViewId): boolean {
  return view === 'influence-network';
}

export function isValueChainGraphView(view: GraphViewId): boolean {
  return view === 'value-chain';
}

export function defaultGraphViewFilterPolicy(view: GraphViewId): GraphViewFilterPolicy {
  if (isInfluenceNetworkGraphView(view) || isValueChainGraphView(view)) {
    return {
      semanticGroupFilter: false,
      semanticGroupColors: false,
    };
  }
  return {
    semanticGroupFilter: true,
    semanticGroupColors: true,
  };
}
