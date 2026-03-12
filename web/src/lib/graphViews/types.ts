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
  | 'destination'
  | 'regulator'
  | 'context';

export type InfluenceActorRole =
  | 'family'
  | 'community'
  | 'organization'
  | 'government'
  | 'person'
  | 'country'
  | 'market'
  | 'intermediary'
  | 'other';

export type ValueChainStage =
  | 'inputs'
  | 'components'
  | 'subsystems'
  | 'integration'
  | 'operators'
  | 'markets';

export type ValueChainRole =
  | 'company'
  | 'country'
  | 'product'
  | 'technology'
  | 'facility'
  | 'market'
  | 'regulator';

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
  isChokepoint?: boolean;
  isGateway?: boolean;
  routeGroup?: string;
  confidence?: number;
}

export interface InfluenceViewMeta {
  clusterId: string;
  clusterKind: 'family' | 'organization' | 'institution' | 'geography' | 'market' | 'other';
  actorRole: InfluenceActorRole;
  countryAnchor?: string;
  centralityBucket?: 'low' | 'medium' | 'high';
}

export interface ValueChainViewMeta {
  stage: ValueChainStage;
  role: ValueChainRole;
  swimlane?: string;
  valuePoolBand?: 'low' | 'medium' | 'high';
  bottleneck?: boolean;
}

export interface SharedGraphNodeMeta {
  graphView?: SemanticGraphView;
  hierarchicalMeta?: HierarchicalViewMeta;
  supplyFlowMeta?: SupplyFlowViewMeta;
  influenceMeta?: InfluenceViewMeta;
  valueChainMeta?: ValueChainViewMeta;
}

export interface SharedGraphEdgeMeta {
  graphView?: SemanticGraphView;
  supplyFlowMeta?: {
    flowClass:
      | 'material-input'
      | 'product-transformation'
      | 'logistics-leg'
      | 'jurisdiction'
      | 'corporate-context';
    variant?: 'primary' | 'fallback' | 'blocked' | 'uncertain';
    confidence?: number;
  };
  influenceMeta?: {
    influenceClass:
      | 'ownership-control'
      | 'board-executive'
      | 'political-access'
      | 'regulatory-leverage'
      | 'commercial-dependence'
      | 'family-community'
      | 'other';
  };
  valueChainMeta?: {
    valueClass: 'transforms-into' | 'supplies-input' | 'enables' | 'sells-into';
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
