import type { Edge, Node } from 'reactflow';
import { layeredClusterLayout } from './ordering';
import {
  buildInfluenceProjection,
  buildSupplyFlowProjection,
  buildValueChainProjection,
} from './projections';
import type {
  InfluenceGraphViewState,
  SharedGraphEdgeMeta,
  SupplyFlowGraphViewState,
  ValueChainGraphViewState,
} from './types';

function ensureGraphViewEdgeMeta(edge: Edge, extra: Partial<SharedGraphEdgeMeta>): Edge {
  return {
    ...edge,
    data: {
      ...((edge as any)?.data || {}),
      ...extra,
    },
  };
}

export async function layoutSupplyFlow<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number; viewState?: SupplyFlowGraphViewState },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const projection = buildSupplyFlowProjection(nodes, edges, options.viewState || {
    routeFamily: 'all',
    localFlowFilter: 'all',
    pinnedNodeIds: [],
    showContextRail: true,
  });
  const laidOutNodes = layeredClusterLayout(projection.nodes, projection.edges, projection.assignments, {
    nodeWidth: options.nodeWidth,
    nodeHeight: options.nodeHeight,
    columnGap: 380,
    laneGap: 248,
    clusterGap: 52,
    subcolumnGap: 92,
  });
  const laidOutEdges = projection.edges.map((edge) => ensureGraphViewEdgeMeta(edge, { graphView: 'supply-flow' }));
  return { nodes: laidOutNodes, edges: laidOutEdges };
}

export async function layoutInfluenceNetwork<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number; viewState?: InfluenceGraphViewState },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const projection = buildInfluenceProjection(nodes, edges, options.viewState || {
    actorClassFilter: 'all',
    influenceClassFilter: 'all',
    collapsedClusterIds: [],
    geoOverlay: false,
    stableOrderHints: {},
  });
  const laidOutNodes = layeredClusterLayout(projection.nodes, projection.edges, projection.assignments, {
    nodeWidth: options.nodeWidth,
    nodeHeight: options.nodeHeight,
    columnGap: 340,
    laneGap: 220,
    clusterGap: 64,
    subcolumnGap: 124,
  });
  const laidOutEdges = projection.edges.map((edge) => ensureGraphViewEdgeMeta(edge, { graphView: 'influence-network' }));
  return { nodes: laidOutNodes, edges: laidOutEdges };
}

export async function layoutValueChain<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number; viewState?: ValueChainGraphViewState },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const projection = buildValueChainProjection(nodes, edges, options.viewState || {
    supportRailVisible: true,
    swimlaneMode: 'role',
    collapsedStages: [],
    activeOverlay: 'bottlenecks',
  });
  const laidOutNodes = layeredClusterLayout(projection.nodes, projection.edges, projection.assignments, {
    nodeWidth: options.nodeWidth,
    nodeHeight: options.nodeHeight,
    columnGap: 360,
    laneGap: 210,
    clusterGap: 44,
    subcolumnGap: 84,
  });
  const laidOutEdges = projection.edges.map((edge) => ensureGraphViewEdgeMeta(edge, { graphView: 'value-chain' }));
  return { nodes: laidOutNodes, edges: laidOutEdges };
}
