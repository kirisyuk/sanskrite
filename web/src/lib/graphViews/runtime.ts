import type { Edge, Node } from 'reactflow';
import { RELATION } from '../taxonomy.generated';
import { normalizeEdgeType } from '../taxonomy';
import { recalculateInfluenceHandles, recalculateSupplyFlowHandles, recalculateValueChainHandles } from './handles';
import { layoutInfluenceNetwork, layoutSupplyFlow, layoutValueChain } from './layouts';
import {
  defaultGraphViewFilterPolicy,
  defaultGraphViewState,
  graphViewToSemantic,
  isHierarchicalGraphView,
  isInfluenceNetworkGraphView,
  isSupplyFlowGraphView,
  isValueChainGraphView,
  type GraphViewArrangeResult,
  type GraphViewFilterPolicy,
  type GraphViewHoverMode,
  type GraphViewId,
  type HierarchicalViewMeta,
  type PersistedGraphViewState,
  type SemanticGraphView,
} from './types';
import type { HorizonViewDirective } from '../../types/horizonView';
import type { SemanticGroup } from '../elkLayout';

type RightDockKey = 'governance' | 'supply' | 'dependencies' | 'structure';

export interface GraphViewRuntimeContext<T = any> {
  nodeWidth: number;
  nodeHeight: number;
  nodeSpacing: number;
  layerSpacing: number;
  maxChildRows: number;
  expandedNodeIds: Set<string>;
  collapsedSubcategories: Set<string>;
  expandedRoleGroups: Set<string>;
  rightDockStateByNodeId: Record<string, RightDockKey[]>;
  expandedStackKeysOverride?: Set<string>;
  hierarchicalLeftPaneMode?: 'default' | 'decision-chain';
  graphViewState: PersistedGraphViewState;
  onToggleSubcategoryCollapse: (groupId: string, edgeType: string, nextCollapsed: boolean) => void;
  onToggleRoleOverflow: (groupKey: string, nextExpanded: boolean) => void;
  onToggleRightDock: (nodeId: string, key: RightDockKey, nextEnabled: boolean) => void;
  layoutFullGraph: (nodes: Node<T>[], edges: Edge[], options: Record<string, any>) => Promise<Node<T>[]>;
  layoutLinearFullGraph: (nodes: Node<T>[], edges: Edge[], options: Record<string, any>) => Promise<Node<T>[]>;
  layoutLevelsTwoPane: (nodes: Node<T>[], edges: Edge[], options: Record<string, any>) => Promise<Node<T>[]>;
  rebuildGroupContainers: (nodes: Node<T>[], options: Record<string, any>) => Node<T>[];
  recalculateFreeformHandles: (edges: Edge[], nodes: Node<T>[]) => Edge[];
  recalculateLinearHandles: (edges: Edge[], nodes: Node<T>[]) => Edge[];
  recalculateLevelsTwoPaneHandles: (edges: Edge[], nodes: Node<T>[]) => Edge[];
}

export interface GraphViewHoverPolicy {
  allowSignalPropagation: boolean;
  allowedModes: GraphViewHoverMode[];
}

export interface GraphViewSelectionPolicy {
  allowFocusMode: boolean;
  allowSecondaryEdges: boolean;
}

export interface GraphViewController<T = any> {
  id: GraphViewId;
  filterPolicy: GraphViewFilterPolicy;
  overlayPolicy: GraphViewOverlayPolicy;
  toolbarPolicy: GraphViewToolbarPolicy;
  hoverPolicy: GraphViewHoverPolicy;
  selectionPolicy: GraphViewSelectionPolicy;
  arrange(nodes: Node<T>[], edges: Edge[], context: GraphViewRuntimeContext<T>): Promise<GraphViewArrangeResult<T>>;
  getExpansionPolicy?(context: GraphViewExpansionContext): GraphViewExpansionPolicy;
  filterVisibleEdges?(edges: Edge[], context: GraphViewVisibleEdgeContext): Edge[];
}

export interface GraphViewOverlayPolicy {
  showSupplyChainOverlay: boolean;
  allowLocalRelationFilters: boolean;
  showExpansionModeMenu: boolean;
}

export interface GraphViewToolbarPolicy {
  showFocusMode: boolean;
  showSecondaryEdgeToggle: boolean;
  relationFilterOptions: SemanticGroup[];
  highlightModes: Array<{ key: GraphViewHoverMode; label: string; hint: string }>;
}

export interface GraphViewExpansionContext {
  isMarket?: boolean;
  hierarchicalEdgeTypes?: string[];
  supplyFlowEdgeTypes?: string[];
}

export interface GraphViewExpansionPolicy {
  edgeTypes: string[];
  usesFilteredExpansion: boolean;
  direction: 'both';
  includeFragments: boolean;
  includeUnindexed: boolean;
  maxNeighbors: number;
  maxPerType: number;
  limit: number;
  runSupplemental: boolean;
}

export interface GraphViewVisibleEdgeContext {
  viewDirective?: HorizonViewDirective | null;
  edgeViewFilter: SemanticGroup[] | 'all';
  showSecondaryEdges: boolean;
  topDepEdgeSet?: Set<string> | null;
  paneByKey?: Map<string, string>;
  edgeTypeOf: (edge: Edge) => string;
  getSemanticGroup: (edgeType: string, directed: boolean) => string;
  getEdgePriority: (edgeType: string) => number;
}

const isGroupNode = (node: Node<any>): boolean => Boolean((node.data as any)?.isGroup);

const addGraphViewMeta = <T,>(nodes: Node<T>[], semanticView: GraphViewId): Node<T>[] => {
  const semantic = graphViewToSemantic(semanticView);
  if (!semantic) return nodes;
  return nodes.map((node) => {
    const d: any = node.data || {};
    const hierarchicalMeta: HierarchicalViewMeta | undefined =
      semantic === 'hierarchical'
        ? {
            pane: String(d?.levelsPane || '').trim() as HierarchicalViewMeta['pane'],
            rightDockEnabled: Array.isArray(d?.rightDockEnabled) ? d.rightDockEnabled : undefined,
            stackGroupKey: String(d?.stackGroupKey || '').trim() || undefined,
            stackKind: String(d?.stackKind || '').trim() || undefined,
          }
        : d?.hierarchicalMeta;
    return {
      ...node,
      data: {
        ...d,
        graphView: semantic,
        ...(hierarchicalMeta ? { hierarchicalMeta } : {}),
      },
    };
  });
};

const restoreExpandedNodes = <T,>(nodes: Node<T>[], expandedNodeIds: Set<string>, keepGroups = false): Node<T>[] => {
  return nodes.map((node) => {
    if (isGroupNode(node) && !keepGroups) return node;
    if (isGroupNode(node)) return node;
    const fid = String((node.data as any)?.fragmentId || node.id || '').trim();
    if (!fid || !expandedNodeIds.has(fid) || Boolean((node.data as any)?.expanded)) return node;
    return {
      ...node,
      data: {
        ...(node.data as any),
        expanded: true,
      },
    };
  });
};

const stripDerivedNodes = <T,>(nodes: Node<T>[]): Node<T>[] => {
  return nodes.filter((node) => {
    const id = String((node as any)?.id || '').trim();
    const d: any = (node as any)?.data;
    if (d?.isGroup) return false;
    if (id.startsWith('levels:')) return false;
    if (id.startsWith('group:')) return false;
    if (id.startsWith('stack:group:')) return false;
    if (Boolean(d?.isStackGroup)) return false;
    if (id.includes(':ctrl:')) return false;
    if (id.includes(':card:')) return false;
    return true;
  });
};

const flattenNodesForLayout = <T,>(nodes: Node<T>[]): Node<T>[] => {
  return nodes.map((node) => ({
    ...node,
    parentNode: undefined,
    parentId: undefined,
    extent: undefined,
    expandParent: undefined,
    data: {
      ...(node.data as any),
      parentNode: undefined,
      parentId: undefined,
    },
  }));
};

function fullHighlightModes(): Array<{ key: GraphViewHoverMode; label: string; hint: string }> {
  return [
    { key: 'off', label: 'Off', hint: 'No emphasis' },
    { key: 'control', label: 'Control', hint: 'Equity ownership + control chains (dashed) + governance overlay' },
    { key: 'expansion', label: 'Expansion', hint: 'Control chains + partners (1-hop)' },
    { key: 'production', label: 'Produces', hint: 'Direct producers/outputs' },
    { key: 'supplies', label: 'Supplies', hint: 'Direct suppliers/inputs' },
    { key: 'competition', label: 'Competes', hint: 'Direct competition/conflict' },
    { key: 'decision-chain', label: 'Decision Chain', hint: 'Directed corridor around hovered node (authority -> execution -> impact)' },
    { key: 'signal-propagation', label: 'Signal propagation', hint: 'Backend propagation trace from selected node (12-axis rules).' },
  ];
}

function semanticStateFor(view: GraphViewId, graphViewState: PersistedGraphViewState): Record<string, any> {
  const semantic = graphViewToSemantic(view);
  if (!semantic || semantic === 'hierarchical') return {};
  return graphViewState[semantic] || defaultGraphViewState(semantic);
}

const freeformController: GraphViewController<any> = {
  id: 'freeform',
  filterPolicy: defaultGraphViewFilterPolicy('freeform'),
  overlayPolicy: { showSupplyChainOverlay: false, allowLocalRelationFilters: false, showExpansionModeMenu: false },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: ['supply-chain', 'customers', 'market', 'context'],
    highlightModes: fullHighlightModes(),
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: fullHighlightModes().map((mode) => mode.key) },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const arranged = await context.layoutFullGraph(restoreExpandedNodes(nodes, context.expandedNodeIds), edges, {
      direction: 'DOWN',
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
      nodeSpacing: context.nodeSpacing,
      layerSpacing: context.layerSpacing,
      maxChildRows: context.maxChildRows,
    });
    const withGroups = context.rebuildGroupContainers(arranged, {
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
    });
    return {
      nodes: addGraphViewMeta(withGroups, 'hierarchical'),
      edges: context.recalculateFreeformHandles(edges, withGroups),
    };
  },
};

const linearController: GraphViewController<any> = {
  id: 'linear',
  filterPolicy: defaultGraphViewFilterPolicy('linear'),
  overlayPolicy: { showSupplyChainOverlay: false, allowLocalRelationFilters: false, showExpansionModeMenu: false },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: ['supply-chain', 'customers', 'market', 'context'],
    highlightModes: fullHighlightModes(),
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: fullHighlightModes().map((mode) => mode.key) },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const arranged = await context.layoutLinearFullGraph(restoreExpandedNodes(nodes, context.expandedNodeIds), edges, {
      direction: 'DOWN',
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
      nodeSpacing: context.nodeSpacing,
      layerSpacing: context.layerSpacing,
      maxChildRows: context.maxChildRows,
    });
    return {
      nodes: addGraphViewMeta(arranged, 'hierarchical'),
      edges: context.recalculateLinearHandles(edges, arranged),
    };
  },
};

const hierarchicalController: GraphViewController<any> = {
  id: 'hierarchical',
  filterPolicy: defaultGraphViewFilterPolicy('hierarchical'),
  overlayPolicy: { showSupplyChainOverlay: false, allowLocalRelationFilters: true, showExpansionModeMenu: true },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: ['supply-chain', 'customers', 'market', 'context'],
    highlightModes: fullHighlightModes(),
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: fullHighlightModes().map((mode) => mode.key) },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const nodesNoRightStacks = (nodes as any[]).filter((node) => {
      const id = String((node as any)?.id || '').trim();
      const d: any = (node as any)?.data;
      if (id.startsWith('stack:group:')) return false;
      if (Boolean(d?.isStackGroup)) return false;
      return true;
    });
    const arranged = await context.layoutLevelsTwoPane(
      restoreExpandedNodes(nodesNoRightStacks, context.expandedNodeIds, true),
      edges,
      {
        direction: 'DOWN',
        nodeWidth: context.nodeWidth,
        nodeHeight: context.nodeHeight,
        nodeSpacing: context.nodeSpacing,
        layerSpacing: context.layerSpacing,
        maxChildRows: context.maxChildRows,
        collapsedSubcategories: context.collapsedSubcategories,
        onToggleSubcategoryCollapse: context.onToggleSubcategoryCollapse,
        expandedRoleGroups: context.expandedRoleGroups,
        onToggleRoleOverflow: context.onToggleRoleOverflow,
        compactLeftPane: true,
        leftPaneMode: context.hierarchicalLeftPaneMode || 'default',
        rightDockMode: true,
        rightDockStateByNodeId: context.rightDockStateByNodeId,
        onToggleRightDock: context.onToggleRightDock,
        expandedStackKeysOverride: context.expandedStackKeysOverride,
      },
    );
    const decorated = addGraphViewMeta(arranged, 'hierarchical');
    return {
      nodes: decorated,
      edges: context.recalculateLevelsTwoPaneHandles(edges, decorated),
    };
  },
  getExpansionPolicy(context) {
    const edgeTypes = Array.isArray(context.hierarchicalEdgeTypes) ? context.hierarchicalEdgeTypes : [];
    const mergedEdgeTypes =
      context.isMarket && edgeTypes.length > 0
        ? Array.from(new Set([...edgeTypes, RELATION.MemberOf, RELATION.ListedOn, RELATION.Influences]))
        : edgeTypes;
    const usesFilteredExpansion = mergedEdgeTypes.length > 0;
    return {
      edgeTypes: mergedEdgeTypes,
      usesFilteredExpansion,
      direction: 'both',
      includeFragments: usesFilteredExpansion,
      includeUnindexed: Boolean(context.isMarket),
      maxNeighbors: 72,
      maxPerType: 18,
      limit: 500,
      runSupplemental: Boolean(context.isMarket),
    };
  },
};

const supplyFlowController: GraphViewController<any> = {
  id: 'supply-flow',
  filterPolicy: defaultGraphViewFilterPolicy('supply-flow'),
  overlayPolicy: { showSupplyChainOverlay: true, allowLocalRelationFilters: true, showExpansionModeMenu: false },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: ['supply-chain', 'customers', 'market', 'context'],
    highlightModes: [
      { key: 'off', label: 'Off', hint: 'No emphasis' },
      { key: 'production', label: 'Produces', hint: 'Direct production and transformation links' },
      { key: 'supplies', label: 'Supplies', hint: 'Direct suppliers and inputs' },
      { key: 'competition', label: 'Competes', hint: 'Direct competition/conflict' },
      { key: 'signal-propagation', label: 'Signal propagation', hint: 'Backend propagation trace from selected node (12-axis rules).' },
    ],
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: ['off', 'production', 'supplies', 'competition', 'signal-propagation'] },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const baseNodes = flattenNodesForLayout(restoreExpandedNodes(stripDerivedNodes(nodes), context.expandedNodeIds));
    const laidOut = await layoutSupplyFlow(baseNodes, edges, {
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
      viewState: semanticStateFor('supply-flow', context.graphViewState) as any,
    });
    return {
      nodes: addGraphViewMeta(laidOut.nodes, 'supply-flow'),
      edges: recalculateSupplyFlowHandles(laidOut.edges as any, laidOut.nodes as any),
    };
  },
  getExpansionPolicy(context) {
    const edgeTypes = Array.isArray(context.supplyFlowEdgeTypes) ? context.supplyFlowEdgeTypes : [];
    return {
      edgeTypes,
      usesFilteredExpansion: edgeTypes.length > 0,
      direction: 'both',
      includeFragments: true,
      includeUnindexed: false,
      maxNeighbors: 72,
      maxPerType: 18,
      limit: 500,
      runSupplemental: true,
    };
  },
  filterVisibleEdges(edges, context) {
    return filterControlEdgeDuplicates(edges, context);
  },
};

const influenceNetworkController: GraphViewController<any> = {
  id: 'influence-network',
  filterPolicy: defaultGraphViewFilterPolicy('influence-network'),
  overlayPolicy: { showSupplyChainOverlay: false, allowLocalRelationFilters: true, showExpansionModeMenu: false },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: [],
    highlightModes: [
      { key: 'off', label: 'Off', hint: 'No emphasis' },
      { key: 'control', label: 'Control', hint: 'Ownership, control, and governance ties' },
      { key: 'expansion', label: 'Expansion', hint: 'Control cluster plus partner spillover' },
      { key: 'competition', label: 'Competes', hint: 'Competitive or conflict adjacency' },
      { key: 'signal-propagation', label: 'Signal propagation', hint: 'Backend propagation trace from selected node (12-axis rules).' },
    ],
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: ['off', 'control', 'expansion', 'competition', 'signal-propagation'] },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const baseNodes = flattenNodesForLayout(restoreExpandedNodes(stripDerivedNodes(nodes), context.expandedNodeIds));
    const laidOut = await layoutInfluenceNetwork(baseNodes, edges, {
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
      viewState: semanticStateFor('influence-network', context.graphViewState) as any,
    });
    return {
      nodes: addGraphViewMeta(laidOut.nodes, 'influence-network'),
      edges: recalculateInfluenceHandles(laidOut.edges as any, laidOut.nodes as any),
    };
  },
  getExpansionPolicy() {
    return {
      edgeTypes: [
        RELATION.BeneficiallyOwns,
        RELATION.ControlsVia,
        RELATION.ManagedBy,
        RELATION.InvestedIn,
        RELATION.HoldsBoardSeat,
        RELATION.MemberOf,
        RELATION.JointVentureWith,
        RELATION.Regulates,
        RELATION.Operates,
        RELATION.Influences,
        RELATION.Disrupts,
        RELATION.Supplies,
        RELATION.RequiresCapability,
      ],
      usesFilteredExpansion: true,
      direction: 'both',
      includeFragments: true,
      includeUnindexed: false,
      maxNeighbors: 72,
      maxPerType: 18,
      limit: 500,
      runSupplemental: true,
    };
  },
};

const valueChainController: GraphViewController<any> = {
  id: 'value-chain',
  filterPolicy: defaultGraphViewFilterPolicy('value-chain'),
  overlayPolicy: { showSupplyChainOverlay: false, allowLocalRelationFilters: true, showExpansionModeMenu: false },
  toolbarPolicy: {
    showFocusMode: true,
    showSecondaryEdgeToggle: true,
    relationFilterOptions: [],
    highlightModes: [
      { key: 'off', label: 'Off', hint: 'No emphasis' },
      { key: 'production', label: 'Produces', hint: 'Stage-to-stage transformation flow' },
      { key: 'supplies', label: 'Supplies', hint: 'Input dependencies and suppliers' },
      { key: 'competition', label: 'Competes', hint: 'Competitive adjacency' },
      { key: 'signal-propagation', label: 'Signal propagation', hint: 'Backend propagation trace from selected node (12-axis rules).' },
    ],
  },
  hoverPolicy: { allowSignalPropagation: true, allowedModes: ['off', 'production', 'supplies', 'competition', 'signal-propagation'] },
  selectionPolicy: { allowFocusMode: true, allowSecondaryEdges: true },
  async arrange(nodes, edges, context) {
    const baseNodes = flattenNodesForLayout(restoreExpandedNodes(stripDerivedNodes(nodes), context.expandedNodeIds));
    const laidOut = await layoutValueChain(baseNodes, edges, {
      nodeWidth: context.nodeWidth,
      nodeHeight: context.nodeHeight,
      viewState: semanticStateFor('value-chain', context.graphViewState) as any,
    });
    return {
      nodes: addGraphViewMeta(laidOut.nodes, 'value-chain'),
      edges: recalculateValueChainHandles(laidOut.edges as any, laidOut.nodes as any),
    };
  },
  getExpansionPolicy() {
    return {
      edgeTypes: [
        RELATION.Produces,
        RELATION.Supplies,
        RELATION.RequiresCapability,
        RELATION.IncorporatedIn,
        RELATION.Operates,
        RELATION.MemberOf,
        RELATION.ListedOn,
        RELATION.Influences,
      ],
      usesFilteredExpansion: true,
      direction: 'both',
      includeFragments: true,
      includeUnindexed: false,
      maxNeighbors: 72,
      maxPerType: 18,
      limit: 500,
      runSupplemental: true,
    };
  },
};

const controllerRegistry = new Map<GraphViewId, GraphViewController<any>>([
  ['freeform', freeformController],
  ['linear', linearController],
  ['levels', hierarchicalController],
  ['hierarchical', hierarchicalController],
  ['supply-chain', supplyFlowController],
  ['supply-flow', supplyFlowController],
  ['influence-network', influenceNetworkController],
  ['value-chain', valueChainController],
]);

export function getGraphViewController(view: GraphViewId): GraphViewController<any> {
  return controllerRegistry.get(view) || hierarchicalController;
}

function filterControlEdgeDuplicates(edges: Edge[], context: GraphViewVisibleEdgeContext): Edge[] {
  const paneByKey = context.paneByKey;
  if (!paneByKey || paneByKey.size === 0) return edges;
  const CONTROLISH = new Set<string>([
    RELATION.BeneficiallyOwns,
    RELATION.SubsidiaryOf,
    RELATION.InvestedIn,
    RELATION.ControlsVia,
    RELATION.ManagedBy,
    RELATION.HoldsBoardSeat,
    RELATION.MemberOf,
    RELATION.Influences,
  ]);
  const pairKey = (a: string, b: string) => (a < b ? `${a}||${b}` : `${b}||${a}`);
  const strength = (t: string): number => {
    const n = normalizeEdgeType(t);
    if (n === RELATION.BeneficiallyOwns) return 10;
    if (n === RELATION.SubsidiaryOf) return 12;
    if (n === RELATION.InvestedIn) return 20;
    if (n === RELATION.ControlsVia || n === RELATION.ManagedBy) return 30;
    if (n === RELATION.HoldsBoardSeat) return 40;
    if (n === RELATION.MemberOf) return 50;
    if (n === RELATION.Influences) return 60;
    return 100;
  };
  const isRight = (id: string) => (paneByKey.get(id) || '') === 'right';

  const bestByPair = new Map<string, { id: string; rank: number }>();
  for (const edge of edges) {
    const source = String((edge as any)?.source || '').trim();
    const target = String((edge as any)?.target || '').trim();
    if (!source || !target || source === target) continue;
    if (!isRight(source) || !isRight(target)) continue;
    const edgeType = context.edgeTypeOf(edge);
    if (!CONTROLISH.has(edgeType)) continue;
    const key = pairKey(source, target);
    const rank = strength(edgeType);
    const prev = bestByPair.get(key);
    if (!prev || rank < prev.rank || (rank === prev.rank && String(edge.id || '').localeCompare(prev.id) < 0)) {
      bestByPair.set(key, { id: String(edge.id || ''), rank });
    }
  }
  if (bestByPair.size === 0) return edges;
  return edges.filter((edge) => {
    const source = String((edge as any)?.source || '').trim();
    const target = String((edge as any)?.target || '').trim();
    if (!source || !target || source === target) return true;
    if (!isRight(source) || !isRight(target)) return true;
    const edgeType = context.edgeTypeOf(edge);
    if (!CONTROLISH.has(edgeType)) return true;
    const best = bestByPair.get(pairKey(source, target));
    return !best || String(edge.id || '') === best.id;
  });
}

export async function arrangeGraphView<T = any>(
  view: GraphViewId,
  nodes: Node<T>[],
  edges: Edge[],
  context: GraphViewRuntimeContext<T>,
): Promise<GraphViewArrangeResult<T>> {
  const controller = getGraphViewController(view);
  const arranged = await controller.arrange(nodes, edges, context);
  if (isSupplyFlowGraphView(view) || isInfluenceNetworkGraphView(view) || isValueChainGraphView(view)) {
    const flattenedNodes = arranged.nodes.map((node) => {
      const pid = String((node as any)?.parentId ?? (node as any)?.parentNode ?? '').trim();
      if (!pid) return node;
      if (pid.startsWith('levels:') || pid.startsWith('group:') || pid.startsWith('stack:')) {
        return {
          ...node,
          parentId: undefined,
          parentNode: undefined,
          extent: undefined,
          expandParent: undefined,
          data: {
            ...(node.data as any),
            parentId: undefined,
            parentNode: undefined,
          },
        };
      }
      return node;
    });
    return { nodes: flattenedNodes, edges: arranged.edges };
  }
  return arranged;
}

export function getGraphViewFilterPolicy(view: GraphViewId): GraphViewFilterPolicy {
  return getGraphViewController(view).filterPolicy;
}

export function isControllerDrivenGraphView(view: GraphViewId): boolean {
  return isHierarchicalGraphView(view) || isSupplyFlowGraphView(view) || isInfluenceNetworkGraphView(view) || isValueChainGraphView(view);
}

export function getGraphViewOverlayPolicy(view: GraphViewId): GraphViewOverlayPolicy {
  return getGraphViewController(view).overlayPolicy;
}

export function getGraphViewToolbarPolicy(view: GraphViewId): GraphViewToolbarPolicy {
  return getGraphViewController(view).toolbarPolicy;
}

export function getGraphViewHoverPolicy(view: GraphViewId): GraphViewHoverPolicy {
  return getGraphViewController(view).hoverPolicy;
}

export function getGraphViewSelectionPolicy(view: GraphViewId): GraphViewSelectionPolicy {
  return getGraphViewController(view).selectionPolicy;
}

export function getGraphViewExpansionPolicy(view: GraphViewId, context: GraphViewExpansionContext = {}): GraphViewExpansionPolicy {
  const controller = getGraphViewController(view);
  if (controller.getExpansionPolicy) return controller.getExpansionPolicy(context);
  return {
    edgeTypes: [],
    usesFilteredExpansion: false,
    direction: 'both',
    includeFragments: false,
    includeUnindexed: false,
    maxNeighbors: 72,
    maxPerType: 18,
    limit: 500,
    runSupplemental: true,
  };
}

function normalizedDirectiveRelationSet(...parts: (readonly string[] | undefined)[]): Set<string> | null {
  const raw: string[] = [];
  for (const p of parts) {
    if (!p?.length) continue;
    for (const v of p) {
      const s = String(v || '').trim();
      if (s) raw.push(s);
    }
  }
  if (!raw.length) return null;
  const set = new Set<string>();
  for (const v of raw) {
    const n = normalizeEdgeType(v);
    if (n) set.add(n);
  }
  return set.size > 0 ? set : null;
}

export function filterEdgesForGraphView(
  view: GraphViewId,
  edges: Edge[],
  context: GraphViewVisibleEdgeContext,
): Edge[] {
  const controller = getGraphViewController(view);
  let filtered = controller.filterVisibleEdges ? controller.filterVisibleEdges(edges, context) : edges;

  const d = context.viewDirective;
  const filterRelSet = normalizedDirectiveRelationSet(d?.filterRelations, d?.filterRelTypes);
  const excludeRelSet = normalizedDirectiveRelationSet(d?.excludeRelations, d?.excludeRelTypes);

  if (filterRelSet && filterRelSet.size > 0) {
    filtered = filtered.filter((edge) => filterRelSet.has(context.edgeTypeOf(edge)));
  }
  if (excludeRelSet && excludeRelSet.size > 0) {
    filtered = filtered.filter((edge) => !excludeRelSet.has(context.edgeTypeOf(edge)));
  }
  if (controller.filterPolicy.semanticGroupFilter && context.edgeViewFilter !== 'all' && context.edgeViewFilter.length > 0) {
    const allowed = new Set(context.edgeViewFilter);
    filtered = filtered.filter((edge) => allowed.has(context.getSemanticGroup(context.edgeTypeOf(edge), true) as SemanticGroup));
  }
  if (!context.showSecondaryEdges) {
    filtered = filtered.filter((edge) => context.getEdgePriority(context.edgeTypeOf(edge)) < 4);
  }
  if (context.topDepEdgeSet && context.topDepEdgeSet.size > 0) {
    const present = new Set<string>(filtered.map((edge) => String((edge as any)?.id || '').trim()).filter(Boolean));
    const forced = edges.filter((edge) => {
      const id = String((edge as any)?.id || '').trim();
      return Boolean(id) && context.topDepEdgeSet?.has(id) && !present.has(id);
    });
    if (forced.length > 0) filtered = [...filtered, ...forced];
  }
  return filtered;
}

export function viewStateFor<T extends Record<string, any> = Record<string, any>>(
  view: SemanticGraphView,
  graphViewState: PersistedGraphViewState,
): T {
  return (graphViewState[view] || defaultGraphViewState(view)) as T;
}
