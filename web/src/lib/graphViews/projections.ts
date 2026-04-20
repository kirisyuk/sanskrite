import type { Edge, Node } from 'reactflow';
import { RELATION } from '../taxonomy.generated';
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
  InfluenceActorRole,
  InfluenceClass,
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
  const assignments = new Map<string, LayeredAssignment>();

  type ActorBase = ReturnType<typeof classifyInfluenceActor>;
  type DraftNode = {
    id: string;
    node: Node<T>;
    label: string;
    degree: number;
    base: ActorBase;
    meta: InfluenceViewMeta;
  };
  type VisibleEdge = {
    edge: Edge;
    id: string;
    source: string;
    target: string;
    influenceClass: InfluenceClass;
    edgeType: string;
  };

  const actorPriority = (role: InfluenceActorRole): number => {
    switch (role) {
      case 'dynasty':
        return 0;
      case 'family':
        return 1;
      case 'tribe':
        return 2;
      case 'community':
        return 3;
      case 'organization':
        return 4;
      case 'facility':
        return 5;
      case 'intermediary':
        return 6;
      case 'person':
        return 7;
      case 'government':
        return 8;
      case 'regulator':
        return 9;
      case 'market':
        return 10;
      case 'country':
        return 11;
      case 'region':
        return 12;
      default:
        return 20;
    }
  };

  const ownershipLaneByRole: Record<InfluenceActorRole, number> = {
    dynasty: 0,
    family: 0,
    tribe: 1,
    community: 1,
    organization: 2,
    facility: 2,
    intermediary: 3,
    person: 3,
    government: 2,
    regulator: 1,
    market: 4,
    country: 5,
    region: 5,
    other: 6,
  };

  const railLane = {
    lineage: 0,
    governance: 1,
    ownership: 2,
    people: 3,
    market: 4,
    geography: 5,
    other: 6,
  } as const;

  const ownershipPriority = (edgeType: string): number => {
    switch (edgeType) {
      case RELATION.BeneficiallyOwns:
      case RELATION.SubsidiaryOf:
        return 1;
      case RELATION.ControlsVia:
        return 2;
      case RELATION.ManagedBy:
        return 3;
      case RELATION.InvestedIn:
        return 4;
      default:
        return 99;
    }
  };

  const directionForOwnership = (edgeType: string, source: string, target: string): { parentId: string; childId: string } | null => {
    if (!source || !target) return null;
    if (edgeType === RELATION.SubsidiaryOf) return { parentId: target, childId: source };
    if (
      edgeType === RELATION.BeneficiallyOwns ||
      edgeType === RELATION.ControlsVia ||
      edgeType === RELATION.ManagedBy ||
      edgeType === RELATION.InvestedIn
    ) {
      return { parentId: source, childId: target };
    }
    return null;
  };

  const roleForRail = (draft: DraftNode, fallback?: InfluenceViewMeta['rail']): NonNullable<InfluenceViewMeta['rail']> => {
    if (draft.meta.isOwnershipCore) return 'ownership';
    if (fallback) return fallback;
    switch (draft.base.actorRole) {
      case 'dynasty':
      case 'family':
      case 'tribe':
      case 'community':
        return 'lineage';
      case 'person':
      case 'intermediary':
        return 'people';
      case 'government':
      case 'regulator':
        return 'governance';
      case 'market':
        return 'market';
      case 'country':
      case 'region':
        return 'geography';
      case 'organization':
      case 'facility':
        return 'ownership';
      default:
        return 'other';
    }
  };

  const rawVisibleNodes = nodes
    .map((node) => {
      const id = String(node.id || '').trim();
      if (!id) return null;
      const base = classifyInfluenceActor(node);
      if (viewState.actorClassFilter !== 'all' && base.actorRole !== viewState.actorClassFilter) return null;
      const clusterId = base.geoAnchor ? `${base.clusterIdSeed}:${base.geoAnchor.id}` : base.clusterIdSeed;
      const degree = 0;
      const meta: InfluenceViewMeta = {
        clusterId,
        clusterKind: base.clusterKind,
        clusterLabel: base.clusterLabel,
        actorRole: base.actorRole,
        geoAnchor: base.geoAnchor,
        countryAnchor: base.geoAnchor?.kind === 'country' ? base.geoAnchor.id : undefined,
        centralityBucket: 'low',
      };
      return {
        id,
        node,
        label: labelOf(node),
        degree,
        base,
        meta,
      } satisfies DraftNode;
    })
    .filter((node): node is DraftNode => Boolean(node));

  const visibleNodeIds = new Set(rawVisibleNodes.map((node) => node.id));
  const visibleEdges = edges
    .map((edge) => {
      const id = String(edge.id || '').trim();
      const source = String(edge.source || '').trim();
      const target = String(edge.target || '').trim();
      if (!id || !source || !target) return null;
      if (!visibleNodeIds.has(source) || !visibleNodeIds.has(target)) return null;
      const influenceClass = classifyInfluenceRelation(edgeTypeOf(edge));
      if (viewState.influenceClassFilter !== 'all' && influenceClass !== viewState.influenceClassFilter) return null;
      return {
        edge,
        id,
        source,
        target,
        influenceClass,
        edgeType: edgeTypeOf(edge),
      } satisfies VisibleEdge;
    })
    .filter((edge): edge is VisibleEdge => Boolean(edge));

  const visibleDegrees = degreeMapFor(visibleEdges.map((edge) => edge.edge));
  const nodeById = new Map<string, DraftNode>();
  for (const draft of rawVisibleNodes) {
    const degree = visibleDegrees.get(draft.id) || 0;
    draft.degree = degree;
    draft.meta.centralityBucket = degree >= 6 ? 'high' : degree >= 3 ? 'medium' : 'low';
    nodeById.set(draft.id, draft);
  }

  const parentByNodeId = new Map<string, string>();
  const parentPriorityByNodeId = new Map<string, number>();
  const childrenByParentId = new Map<string, string[]>();
  const ownershipNodeIds = new Set<string>();

  for (const edge of visibleEdges) {
    if (edge.influenceClass !== 'ownership-control') continue;
    const directed = directionForOwnership(edge.edgeType, edge.source, edge.target);
    if (!directed) continue;
    const priority = ownershipPriority(edge.edgeType);
    const existingPriority = parentPriorityByNodeId.get(directed.childId);
    if (existingPriority == null || priority < existingPriority) {
      parentByNodeId.set(directed.childId, directed.parentId);
      parentPriorityByNodeId.set(directed.childId, priority);
    }
    ownershipNodeIds.add(directed.parentId);
    ownershipNodeIds.add(directed.childId);
  }

  for (const [childId, parentId] of parentByNodeId.entries()) {
    const list = childrenByParentId.get(parentId) || [];
    list.push(childId);
    childrenByParentId.set(parentId, list);
  }

  const rootScore = (nodeId: string): number => {
    const draft = nodeById.get(nodeId);
    if (!draft) return 0;
    let score = 0;
    if (draft.base.actorRole === 'dynasty') score += 12;
    else if (draft.base.actorRole === 'family') score += 10;
    else if (draft.base.actorRole === 'tribe' || draft.base.actorRole === 'community') score += 8;
    else if (draft.base.actorRole === 'organization') score += 5;
    score += draft.degree;
    return score;
  };

  const ownershipRoots = Array.from(ownershipNodeIds)
    .filter((nodeId) => !parentByNodeId.has(nodeId) || !ownershipNodeIds.has(parentByNodeId.get(nodeId)!))
    .sort((a, b) => {
      const scoreDiff = rootScore(b) - rootScore(a);
      if (scoreDiff !== 0) return scoreDiff;
      return (nodeById.get(a)?.label || a).localeCompare(nodeById.get(b)?.label || b);
    });

  const depthByNodeId = new Map<string, number>();
  const rootByNodeId = new Map<string, string>();
  const visitOwnership = (nodeId: string, rootId: string, depth: number) => {
    const prevDepth = depthByNodeId.get(nodeId);
    if (prevDepth != null && prevDepth <= depth) return;
    depthByNodeId.set(nodeId, depth);
    rootByNodeId.set(nodeId, rootId);
    const children = (childrenByParentId.get(nodeId) || []).slice().sort((a, b) => {
      const aNode = nodeById.get(a);
      const bNode = nodeById.get(b);
      const actorDiff = actorPriority(aNode?.base.actorRole || 'other') - actorPriority(bNode?.base.actorRole || 'other');
      if (actorDiff !== 0) return actorDiff;
      return (aNode?.label || a).localeCompare(bNode?.label || b);
    });
    for (const childId of children) visitOwnership(childId, rootId, depth + 1);
  };

  for (const rootId of ownershipRoots) {
    visitOwnership(rootId, rootId, 0);
  }

  for (const nodeId of ownershipNodeIds) {
    if (depthByNodeId.has(nodeId)) continue;
    visitOwnership(nodeId, nodeId, 0);
  }

  for (const nodeId of ownershipNodeIds) {
    const draft = nodeById.get(nodeId);
    if (!draft) continue;
    draft.meta.isOwnershipCore = true;
    draft.meta.controlDepth = depthByNodeId.get(nodeId) ?? 0;
    draft.meta.rootId = rootByNodeId.get(nodeId) || nodeId;
    draft.meta.anchorId = parentByNodeId.get(nodeId) || nodeId;
    draft.meta.isRootOwner = (draft.meta.controlDepth ?? 0) === 0;
  }

  const incidentVisibleEdges = new Map<string, VisibleEdge[]>();
  const addIncident = (nodeId: string, edge: VisibleEdge) => {
    const list = incidentVisibleEdges.get(nodeId) || [];
    list.push(edge);
    incidentVisibleEdges.set(nodeId, list);
  };
  for (const edge of visibleEdges) {
    addIncident(edge.source, edge);
    addIncident(edge.target, edge);
  }

  const placementByNodeId = new Map<string, { column: number; anchorId: string; rootId: string; rail: NonNullable<InfluenceViewMeta['rail']> }>();

  for (const draft of rawVisibleNodes) {
    if (!draft.meta.isOwnershipCore) continue;
    const column = draft.meta.controlDepth ?? 0;
    const rootId = draft.meta.rootId || draft.id;
    const anchorId = draft.meta.anchorId || draft.id;
    placementByNodeId.set(draft.id, { column, anchorId, rootId, rail: 'ownership' });
    draft.meta.rail = 'ownership';
  }

  const inferPlacement = (draft: DraftNode): { column: number; anchorId: string; rootId: string; rail: NonNullable<InfluenceViewMeta['rail']> } => {
    const incident = incidentVisibleEdges.get(draft.id) || [];
    let best: { score: number; column: number; anchorId: string; rootId: string; rail: NonNullable<InfluenceViewMeta['rail']> } | null = null;
    for (const edge of incident) {
      const otherId = edge.source === draft.id ? edge.target : edge.source;
      const otherPlacement = placementByNodeId.get(otherId);
      if (!otherPlacement) continue;
      let rail = roleForRail(draft);
      let column = otherPlacement.column;
      let score = 0;
      switch (edge.influenceClass) {
        case 'board-executive':
          rail = 'people';
          column = otherPlacement.column;
          score = 140;
          break;
        case 'family-community':
          rail = roleForRail(draft, 'lineage');
          column = Math.max(0, otherPlacement.column - (rail === 'lineage' ? 1 : 0));
          score = 130;
          break;
        case 'regulatory-leverage':
        case 'political-access':
          rail = 'governance';
          column = otherPlacement.column + 1;
          score = 120;
          break;
        case 'commercial-dependence':
          rail = draft.base.actorRole === 'market' ? 'market' : roleForRail(draft);
          column = otherPlacement.column + (rail === 'ownership' ? 0 : 1);
          score = 96;
          break;
        case 'influence-general':
          rail = roleForRail(draft);
          column = otherPlacement.column + (rail === 'market' || rail === 'geography' ? 2 : 1);
          score = 88;
          break;
        case 'other':
          rail = roleForRail(draft);
          column = otherPlacement.column + (rail === 'lineage' ? 0 : 1);
          score = 60;
          break;
        default:
          rail = roleForRail(draft);
          column = otherPlacement.column;
          score = 80;
          break;
      }
      if (draft.base.actorRole === 'country' || draft.base.actorRole === 'region') {
        rail = 'geography';
        column = otherPlacement.column + 2;
        score += 4;
      } else if (draft.base.actorRole === 'market') {
        rail = 'market';
        column = otherPlacement.column + 2;
        score += 4;
      } else if (draft.base.actorRole === 'government' || draft.base.actorRole === 'regulator') {
        rail = 'governance';
        column = otherPlacement.column + 1;
        score += 6;
      }
      if (!best || score > best.score) {
        best = {
          score,
          column,
          anchorId: otherPlacement.anchorId || otherId,
          rootId: otherPlacement.rootId || otherId,
          rail,
        };
      }
    }
    if (best) {
      return { column: Math.max(0, best.column), anchorId: best.anchorId, rootId: best.rootId, rail: best.rail };
    }

    const fallbackRail = roleForRail(draft);
    const fallbackColumn =
      fallbackRail === 'lineage' ? 0 :
      fallbackRail === 'ownership' ? 1 :
      fallbackRail === 'people' ? 2 :
      fallbackRail === 'governance' ? 3 :
      fallbackRail === 'market' ? 4 :
      fallbackRail === 'geography' ? 5 :
      3;
    return { column: fallbackColumn, anchorId: draft.id, rootId: draft.id, rail: fallbackRail };
  };

  for (let pass = 0; pass < 4; pass += 1) {
    let changed = false;
    for (const draft of rawVisibleNodes) {
      if (placementByNodeId.has(draft.id)) continue;
      const placement = inferPlacement(draft);
      placementByNodeId.set(draft.id, placement);
      draft.meta.anchorId = placement.anchorId;
      draft.meta.rootId = placement.rootId;
      draft.meta.rail = placement.rail;
      changed = true;
    }
    if (!changed) break;
  }

  const projectedNodes = rawVisibleNodes.map((draft) => {
    const placement = placementByNodeId.get(draft.id) || {
      column: 0,
      anchorId: draft.id,
      rootId: draft.id,
      rail: roleForRail(draft),
    };
    draft.meta.anchorId = draft.meta.anchorId || placement.anchorId;
    draft.meta.rootId = draft.meta.rootId || placement.rootId;
    draft.meta.rail = draft.meta.rail || placement.rail;
    const laneBase = railLane[draft.meta.rail || 'other'] ?? railLane.other;
    const lane = draft.meta.isOwnershipCore
      ? ownershipLaneByRole[draft.base.actorRole] ?? railLane.ownership
      : laneBase;
    const stableOrderHint = Number(viewState.stableOrderHints?.[draft.id] ?? 0) || 0;
    const clusterRoot = draft.meta.rootId || placement.rootId || draft.id;
    const clusterAnchor = draft.meta.anchorId || placement.anchorId || draft.id;
    assignments.set(draft.id, {
      column: placement.column,
      lane,
      clusterKey: draft.meta.isOwnershipCore
        ? `spine:${clusterRoot}`
        : `${draft.meta.rail || 'other'}:${clusterAnchor}:${clusterRoot}`,
      orderKey: `${String(stableOrderHint).padStart(6, '0')}:${String(draft.meta.controlDepth ?? 99).padStart(2, '0')}:${actorPriority(draft.base.actorRole)}:${draft.label}`,
      importance:
        draft.degree +
        (draft.meta.isRootOwner ? 10 : 0) +
        (draft.meta.isOwnershipCore ? 7 : 0) +
        (draft.base.actorRole === 'regulator' ? 3 : 0) +
        (draft.base.actorRole === 'market' ? 2 : 0),
      pinned:
        Boolean(draft.meta.isRootOwner) ||
        draft.base.actorRole === 'country' ||
        draft.base.actorRole === 'region' ||
        draft.base.actorRole === 'dynasty' ||
        draft.base.actorRole === 'family',
    });
    return {
      ...draft.node,
      data: {
        ...(draft.node.data as any),
        graphView: 'influence-network',
        geoAnchor: draft.meta.geoAnchor,
        influenceMeta: draft.meta,
      },
    };
  });

  const projectedNodeById = new Map(projectedNodes.map((node) => [String(node.id || '').trim(), node]));
  const projectedEdges = visibleEdges.map((entry) => {
    const sourceMeta = (projectedNodeById.get(entry.source)?.data as any)?.influenceMeta as InfluenceViewMeta | undefined;
    const targetMeta = (projectedNodeById.get(entry.target)?.data as any)?.influenceMeta as InfluenceViewMeta | undefined;
    const sameAnchor = Boolean(sourceMeta?.anchorId && targetMeta?.anchorId && sourceMeta.anchorId === targetMeta.anchorId);
    const sameRail = Boolean(sourceMeta?.rail && targetMeta?.rail && sourceMeta.rail === targetMeta.rail);
    const preferredAxis =
      entry.influenceClass === 'ownership-control' ? 'horizontal' :
      entry.influenceClass === 'board-executive' ? 'vertical' :
      entry.influenceClass === 'family-community' ? 'vertical' :
      entry.influenceClass === 'regulatory-leverage' || entry.influenceClass === 'political-access'
        ? (sameAnchor ? 'vertical' : 'horizontal')
        : sameRail && sameAnchor
          ? 'vertical'
          : 'horizontal';
    const bundleId =
      sourceMeta?.rootId && targetMeta?.rootId && sourceMeta.rootId === targetMeta.rootId
        ? `${sourceMeta.rootId}:${entry.influenceClass}:${sourceMeta.rail || 'other'}:${targetMeta.rail || 'other'}`
        : sourceMeta?.geoAnchor?.id && targetMeta?.geoAnchor?.id
          ? `${sourceMeta.geoAnchor.id}:${targetMeta.geoAnchor.id}:${entry.influenceClass}`
          : `${sourceMeta?.clusterId || 'source'}:${targetMeta?.clusterId || 'target'}:${entry.influenceClass}`;
    return decorateEdgeMeta(entry.edge, {
      graphView: 'influence-network',
      routingHint: {
        preferredAxis,
        sameCluster: sameAnchor && sameRail,
        sameLane: sameRail,
        bundleId,
      },
      influenceMeta: { influenceClass: entry.influenceClass },
    });
  });

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
