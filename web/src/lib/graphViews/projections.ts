import type { Edge, Node } from 'reactflow';
import {
  canonicalPathOf,
  classifyGeoAnchor,
  classifyInfluenceActor,
  classifyInfluenceRelation,
  classifySupplyFlowNode,
  classifySupplyFlowRelation,
  classifyValueChainNode,
  classifyValueChainRelation,
  edgeTypeOf,
  labelOf,
  typeOf,
} from './classifiers';
import type {
  InfluenceGraphViewState,
  SharedGraphEdgeMeta,
  SupplyFlowGraphViewState,
  ValueChainGraphViewState,
  SupplyFlowViewMeta,
  InfluenceViewMeta,
  ValueChainViewMeta,
} from './types';
import type { LayeredAssignment } from './ordering';

export interface GraphProjectionResult<T = any> {
  nodes: Node<T>[];
  edges: Edge[];
  assignments: Map<string, LayeredAssignment>;
}

function degreeMapFor(edges: Edge[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const e of edges) {
    const s = String(e.source || '').trim();
    const t = String(e.target || '').trim();
    if (!s || !t) continue;
    out.set(s, (out.get(s) || 0) + 1);
    out.set(t, (out.get(t) || 0) + 1);
  }
  return out;
}

function incidentByNode(edges: Edge[]): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const edge of edges) {
    const edgeType = edgeTypeOf(edge);
    const source = String(edge.source || '').trim();
    const target = String(edge.target || '').trim();
    if (!source || !target) continue;
    const add = (id: string) => {
      const list = out.get(id) || [];
      list.push(edgeType);
      out.set(id, list);
    };
    add(source);
    add(target);
  }
  return out;
}

function decorateEdgeMeta(edge: Edge, meta: SharedGraphEdgeMeta): Edge {
  return {
    ...edge,
    data: {
      ...((edge as any).data || {}),
      ...meta,
    },
  };
}

export function buildSupplyFlowProjection<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  viewState: SupplyFlowGraphViewState,
): GraphProjectionResult<T> {
  const degrees = degreeMapFor(edges);
  const incident = incidentByNode(edges);
  const assignments = new Map<string, LayeredAssignment>();

  const stageColumn = {
    upstream: 0,
    transformation: 1,
    transit: 2,
    destination: 3,
    jurisdiction: 1,
  } as const;
  const laneOrder: Record<string, number> = {
    jurisdiction: 0,
    support: 1,
    inputs: 2,
    transformation: 3,
    transit: 4,
    destination: 5,
  };

  const projectedNodes = nodes
    .map((node) => {
      const meta = classifySupplyFlowNode(
        node,
        incident.get(String(node.id || '').trim()) || [],
        degrees.get(String(node.id || '').trim()) || 0,
      );
      if (!viewState.showContextRail && (meta.supportRole === 'governance' || meta.supportRole === 'support')) {
        return null;
      }
      const routeFamily = meta.routeFamily || 'context';
      assignments.set(String(node.id || '').trim(), {
        column: stageColumn[meta.stage],
        lane: laneOrder[meta.lane] ?? 3,
        clusterKey: `${meta.geoAnchor?.id || 'GLOBAL'}:${meta.stage}`,
        orderKey: `${routeFamily}:${labelOf(node)}`,
        importance: (meta.isGateway ? 6 : 0) + (meta.isChokepoint ? 5 : 0) + (degrees.get(String(node.id || '').trim()) || 0),
        pinned: Boolean(meta.isGateway || meta.isChokepoint),
      });
      const nodeData = {
        ...(node.data as any),
        graphView: 'supply-flow',
        geoAnchor: meta.geoAnchor,
        supplyFlowMeta: {
          ...meta,
          routeFamily,
        } satisfies SupplyFlowViewMeta,
      };
      return { ...node, data: nodeData };
    })
    .filter((node): node is Node<T> => Boolean(node));

  const visibleNodeIds = new Set(projectedNodes.map((node) => String(node.id || '').trim()));
  const projectedEdges = edges
    .filter((edge) => visibleNodeIds.has(String(edge.source || '').trim()) && visibleNodeIds.has(String(edge.target || '').trim()))
    .map((edge) => {
      const relation = classifySupplyFlowRelation(edgeTypeOf(edge), Number((edge as any)?.data?.criticality || 0));
      if (viewState.routeFamily !== 'all' && relation.variant !== viewState.routeFamily && relation.flowClass !== 'jurisdiction') {
        return null;
      }
      if (viewState.localFlowFilter !== 'all' && relation.flowClass !== viewState.localFlowFilter) {
        return null;
      }
      const sourceMeta = (projectedNodes.find((node) => node.id === edge.source)?.data as any)?.supplyFlowMeta as SupplyFlowViewMeta | undefined;
      const targetMeta = (projectedNodes.find((node) => node.id === edge.target)?.data as any)?.supplyFlowMeta as SupplyFlowViewMeta | undefined;
      return decorateEdgeMeta(edge, {
        graphView: 'supply-flow',
        routingHint: {
          preferredAxis: relation.flowClass === 'jurisdiction' || relation.flowClass === 'route-support' ? 'vertical' : 'horizontal',
          sameLane: sourceMeta?.lane === targetMeta?.lane,
          reverseFlow: Boolean(sourceMeta && targetMeta && stageColumn[sourceMeta.stage] > stageColumn[targetMeta.stage]),
          bundleId: sourceMeta?.geoAnchor?.id && targetMeta?.geoAnchor?.id ? `${sourceMeta.geoAnchor.id}:${targetMeta.geoAnchor.id}:${relation.flowClass}` : undefined,
        },
        supplyFlowMeta: {
          flowClass: relation.flowClass,
          variant: relation.variant,
          confidence: Number((edge as any)?.data?.criticality || 0) || undefined,
        },
      });
    })
    .filter((edge): edge is Edge => Boolean(edge));

  return { nodes: projectedNodes, edges: projectedEdges, assignments };
}

export function buildInfluenceProjection<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  viewState: InfluenceGraphViewState,
): GraphProjectionResult<T> {
  const degrees = degreeMapFor(edges);
  const assignments = new Map<string, LayeredAssignment>();
  const clusterColumn = {
    family: 0,
    community: 1,
    organization: 2,
    institution: 3,
    geography: 4,
    market: 5,
    other: 6,
  } as const;
  const actorLane = {
    family: 0,
    dynasty: 0,
    tribe: 1,
    community: 1,
    organization: 2,
    government: 2,
    regulator: 1,
    person: 3,
    country: 0,
    region: 0,
    market: 4,
    intermediary: 3,
    facility: 2,
    other: 5,
  } as const;

  const projectedNodes = nodes
    .map((node) => {
      const metaBase = classifyInfluenceActor(node);
      if (viewState.actorClassFilter !== 'all' && metaBase.actorRole !== viewState.actorClassFilter) {
        return null;
      }
      const degree = degrees.get(String(node.id || '').trim()) || 0;
      const clusterId = metaBase.geoAnchor
        ? `${metaBase.clusterIdSeed}:${metaBase.geoAnchor.id}`
        : metaBase.clusterIdSeed;
      const influenceMeta: InfluenceViewMeta = {
        clusterId,
        clusterKind: metaBase.clusterKind,
        clusterLabel: metaBase.clusterLabel,
        actorRole: metaBase.actorRole,
        geoAnchor: metaBase.geoAnchor,
        countryAnchor: metaBase.geoAnchor?.kind === 'country' ? metaBase.geoAnchor.id : undefined,
        centralityBucket: degree >= 6 ? 'high' : degree >= 3 ? 'medium' : 'low',
      };
      assignments.set(String(node.id || '').trim(), {
        column: clusterColumn[influenceMeta.clusterKind],
        lane: actorLane[influenceMeta.actorRole],
        clusterKey: influenceMeta.clusterId,
        orderKey: `${influenceMeta.centralityBucket}:${labelOf(node)}`,
        importance: degree + (influenceMeta.centralityBucket === 'high' ? 6 : influenceMeta.centralityBucket === 'medium' ? 3 : 0),
        pinned: influenceMeta.actorRole === 'country' || influenceMeta.actorRole === 'region' || influenceMeta.actorRole === 'family' || influenceMeta.actorRole === 'dynasty',
      });
      return {
        ...node,
        data: {
          ...(node.data as any),
          graphView: 'influence-network',
          geoAnchor: influenceMeta.geoAnchor,
          influenceMeta,
        },
      };
    })
    .filter((node): node is Node<T> => Boolean(node));

  const visibleNodeIds = new Set(projectedNodes.map((node) => String(node.id || '').trim()));
  const projectedEdges = edges
    .filter((edge) => visibleNodeIds.has(String(edge.source || '').trim()) && visibleNodeIds.has(String(edge.target || '').trim()))
    .map((edge) => {
      const influenceClass = classifyInfluenceRelation(edgeTypeOf(edge));
      if (viewState.influenceClassFilter !== 'all' && influenceClass !== viewState.influenceClassFilter) return null;
      const sourceNode = projectedNodes.find((node) => node.id === edge.source);
      const targetNode = projectedNodes.find((node) => node.id === edge.target);
      const sourceMeta = (sourceNode?.data as any)?.influenceMeta as InfluenceViewMeta | undefined;
      const targetMeta = (targetNode?.data as any)?.influenceMeta as InfluenceViewMeta | undefined;
      const bundleId =
        sourceMeta?.geoAnchor?.id && targetMeta?.geoAnchor?.id
          ? `${sourceMeta.geoAnchor.id}:${targetMeta.geoAnchor.id}:${influenceClass}`
          : `${sourceMeta?.clusterId || 'source'}:${targetMeta?.clusterId || 'target'}:${influenceClass}`;
      return decorateEdgeMeta(edge, {
        graphView: 'influence-network',
        routingHint: {
          preferredAxis: sourceMeta?.clusterId === targetMeta?.clusterId ? 'vertical' : 'horizontal',
          sameCluster: sourceMeta?.clusterId === targetMeta?.clusterId,
          bundleId,
        },
        influenceMeta: { influenceClass },
      });
    })
    .filter((edge): edge is Edge => Boolean(edge));

  return { nodes: projectedNodes, edges: projectedEdges, assignments };
}

export function buildValueChainProjection<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  viewState: ValueChainGraphViewState,
): GraphProjectionResult<T> {
  const incident = incidentByNode(edges);
  const assignments = new Map<string, LayeredAssignment>();
  const stageColumn = {
    inputs: 0,
    components: 1,
    subsystems: 2,
    integration: 3,
    operators: 4,
    markets: 5,
    support: 2,
  } as const;

  const roleLaneBySwimlaneMode = (meta: ValueChainViewMeta): number => {
    if (meta.supportRail) return 0;
    if (viewState.swimlaneMode === 'geography') {
      const geo = meta.geoAnchor?.id || 'GLOBAL';
      return geo.split('').reduce((sum, char) => sum + char.charCodeAt(0), 0) % 4 + 1;
    }
    if (viewState.swimlaneMode === 'product-family') {
      const family = meta.role === 'product' ? 1 : meta.role === 'technology' ? 2 : meta.role === 'facility' ? 3 : 4;
      return family;
    }
    const byRole: Record<string, number> = {
      company: 4,
      operator: 4,
      service: 5,
      facility: 3,
      technology: 2,
      product: 2,
      market: 6,
      regulator: 0,
      country: 1,
      support: 0,
    };
    return byRole[meta.role] ?? 4;
  };

  const projectedNodes = nodes
    .map((node) => {
      const meta = classifyValueChainNode(node, incident.get(String(node.id || '').trim()) || []);
      if (!viewState.supportRailVisible && meta.supportRail) return null;
      if (viewState.collapsedStages.includes(meta.stage)) return null;
      const stageColumnIndex = stageColumn[meta.stage];
      assignments.set(String(node.id || '').trim(), {
        column: stageColumnIndex,
        lane: roleLaneBySwimlaneMode(meta),
        clusterKey: meta.supportRail ? `support:${meta.geoAnchor?.id || meta.role}` : `${meta.stage}:${meta.geoAnchor?.id || meta.swimlane}`,
        orderKey: `${meta.valuePoolBand}:${labelOf(node)}`,
        importance: (meta.bottleneck ? 7 : 0) + (meta.supportRail ? 1 : 3),
        pinned: Boolean(meta.bottleneck || meta.role === 'market'),
      });
      return {
        ...node,
        data: {
          ...(node.data as any),
          graphView: 'value-chain',
          geoAnchor: meta.geoAnchor,
          valueChainMeta: meta,
        },
      };
    })
    .filter((node): node is Node<T> => Boolean(node));

  const visibleNodeIds = new Set(projectedNodes.map((node) => String(node.id || '').trim()));
  const projectedEdges = edges
    .filter((edge) => visibleNodeIds.has(String(edge.source || '').trim()) && visibleNodeIds.has(String(edge.target || '').trim()))
    .map((edge) => {
      const valueClass = classifyValueChainRelation(edgeTypeOf(edge));
      const sourceNode = projectedNodes.find((node) => node.id === edge.source);
      const targetNode = projectedNodes.find((node) => node.id === edge.target);
      const sourceMeta = (sourceNode?.data as any)?.valueChainMeta as ValueChainViewMeta | undefined;
      const targetMeta = (targetNode?.data as any)?.valueChainMeta as ValueChainViewMeta | undefined;
      return decorateEdgeMeta(edge, {
        graphView: 'value-chain',
        routingHint: {
          preferredAxis: sourceMeta?.stage === targetMeta?.stage || valueClass === 'governance-context' ? 'vertical' : 'horizontal',
          sameLane: sourceMeta?.swimlane === targetMeta?.swimlane,
          reverseFlow: Boolean(sourceMeta && targetMeta && stageColumn[sourceMeta.stage] > stageColumn[targetMeta.stage]),
          bundleId: `${sourceMeta?.stage || 'source'}:${targetMeta?.stage || 'target'}:${valueClass}`,
        },
        valueChainMeta: { valueClass },
      });
    });

  return { nodes: projectedNodes, edges: projectedEdges, assignments };
}

export function inferNodeGeoAnchor<T = any>(node: Node<T>) {
  return classifyGeoAnchor(canonicalPathOf(node), labelOf(node), typeOf(node));
}
