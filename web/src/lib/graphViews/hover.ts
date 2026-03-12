import type { Edge, Node } from 'reactflow';
import type { GraphViewHoverMode } from './types';

export interface HoverHighlightResult {
  nodeIds: Set<string>;
  edgeIds: Set<string>;
  ultimatePersonIds?: Set<string>;
}

export interface ComputeHoverHighlightInput<T = any> {
  edges: Edge[];
  hoveredNodeId?: string | null;
  hoverMode: GraphViewHoverMode;
  isHierarchicalView: boolean;
  nodes: Node<T>[];
  perfMode: boolean;
}

export function computeHoverHighlight<T = any>(
  input: ComputeHoverHighlightInput<T>,
): HoverHighlightResult | null {
  const { edges, hoveredNodeId, hoverMode, isHierarchicalView, nodes, perfMode } = input;
  if (perfMode) return null;
  if (hoverMode === 'off' || hoverMode === 'signal-propagation') return null;
  const hovered = String(hoveredNodeId || '').trim();
  if (!hovered) return null;
  const isControlLike = hoverMode === 'control' || hoverMode === 'expansion';

  const visibleNodeIds = new Set<string>();
  const groupIds = new Set<string>();
  const xById = new Map<string, number>();
  const typeById = new Map<string, string>();
  let hoveredNode: Node<T> | undefined;

  for (let i = 0; i < nodes.length; i++) {
    const node = nodes[i]!;
    const id = String(node.id || '').trim();
    if (!id) continue;
    if (id === hovered) hoveredNode = node;
    if ((node as any).hidden) continue;

    const isGroup = Boolean((node.data as any)?.isGroup);
    visibleNodeIds.add(id);
    if (isGroup) {
      groupIds.add(id);
      continue;
    }
    const x = typeof (node as any)?.position?.x === 'number' ? (node as any).position.x : undefined;
    if (typeof x === 'number' && Number.isFinite(x)) xById.set(id, x);
    const nodeType = String((node.data as any)?.type || '').toUpperCase().trim();
    if (nodeType) typeById.set(id, nodeType);
  }

  if (!hoveredNode || (hoveredNode.data as any)?.isGroup || !visibleNodeIds.has(hovered)) return null;

  if (hoverMode === 'decision-chain') {
    return computeDecisionChainHighlight(edges, groupIds, hovered, visibleNodeIds, xById);
  }

  const isPerson = (id: string) => (typeById.get(id) || '') === 'PERSON';
  const normalizeType = (t: string) => String(t || '').toUpperCase().trim();
  const CONTROL_CHAIN_TYPES = new Set<string>([
    'OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'INVESTS_IN',
    'CONTROLS', 'MANAGES',
    'BOARD_MEMBER_OF', 'EXECUTIVE_OF',
  ]);
  const CONTROL_GOVERNANCE_TYPES = new Set<string>(['EMPLOYED_BY', 'FOUNDED']);
  const EXPANSION_EXTRA_TYPES = new Set<string>(['PARTNERS_WITH']);
  const CONTROL_ULTIMATE_BLOCKING_TYPES = new Set<string>([
    'OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'INVESTS_IN', 'CONTROLS', 'MANAGES',
  ]);
  const isEdgeTypeOfMode = (t: string) => {
    const u = normalizeType(t);
    switch (hoverMode) {
      case 'control':
        return CONTROL_CHAIN_TYPES.has(u) || CONTROL_GOVERNANCE_TYPES.has(u);
      case 'expansion':
        return CONTROL_CHAIN_TYPES.has(u) || CONTROL_GOVERNANCE_TYPES.has(u) || EXPANSION_EXTRA_TYPES.has(u);
      case 'production':
        return u === 'PRODUCES';
      case 'supplies':
        return u === 'SUPPLIES_TO';
      case 'competition':
        return u === 'COMPETES_WITH' || u === 'CONFLICTS_WITH' || u === 'OPPOSES';
      default:
        return false;
    }
  };

  type Adj = { to: string; edgeId: string; edgeType: string };
  type AdjIn = { from: string; edgeId: string; edgeType: string };
  const adj = new Map<string, Adj[]>();
  const adjOut = new Map<string, Adj[]>();
  const adjIn = new Map<string, AdjIn[]>();
  const governanceEdges: Array<{ id: string; a: string; b: string }> = [];
  const partnerEdges: Array<{ id: string; a: string; b: string }> = [];
  type RawEdge = { from: string; to: string; edgeId: string; edgeType: string };
  const rawControlEdges: RawEdge[] = [];
  const pairKey = (a: string, b: string) => (a < b ? `${a}|${b}` : `${b}|${a}`);
  const pairDirections = new Map<string, { aToB: RawEdge[]; bToA: RawEdge[] }>();

  const addDirected = (from: string, to: string, edgeId: string, edgeType: string) => {
    if (!from || !to) return;
    if (!adjOut.has(from)) adjOut.set(from, []);
    adjOut.get(from)!.push({ to, edgeId, edgeType });
    if (!adjIn.has(to)) adjIn.set(to, []);
    adjIn.get(to)!.push({ from, edgeId, edgeType });
  };

  for (const edge of edges) {
    const edgeId = String((edge as any)?.id || '').trim();
    const src = String((edge as any)?.source || '').trim();
    const dst = String((edge as any)?.target || '').trim();
    if (!edgeId || !src || !dst) continue;
    if (!visibleNodeIds.has(src) || !visibleNodeIds.has(dst)) continue;
    if (groupIds.has(src) || groupIds.has(dst)) continue;

    const edgeType = String((edge as any)?.data?.edgeType || (edge as any)?.label || (edge as any)?.type || '');
    if (!isEdgeTypeOfMode(edgeType)) continue;
    const normalized = normalizeType(edgeType);

    if (isControlLike) {
      if (hoverMode === 'expansion' && EXPANSION_EXTRA_TYPES.has(normalized)) {
        partnerEdges.push({ id: edgeId, a: src, b: dst });
        continue;
      }
      if (CONTROL_GOVERNANCE_TYPES.has(normalized)) {
        governanceEdges.push({ id: edgeId, a: src, b: dst });
        continue;
      }
      if (!CONTROL_CHAIN_TYPES.has(normalized)) continue;

      let from = src;
      let to = dst;
      if (normalized === 'SUBSIDIARY_OF') {
        from = dst;
        to = src;
      }
      if (normalized === 'BOARD_MEMBER_OF' || normalized === 'EXECUTIVE_OF') {
        const srcIsPerson = isPerson(src);
        const dstIsPerson = isPerson(dst);
        if (srcIsPerson && !dstIsPerson) {
          from = src;
          to = dst;
        } else if (dstIsPerson && !srcIsPerson) {
          from = dst;
          to = src;
        }
      }

      const key = pairKey(from, to);
      if (!pairDirections.has(key)) pairDirections.set(key, { aToB: [], bToA: [] });
      const pair = pairDirections.get(key)!;
      const aNode = from < to ? from : to;
      if (from === aNode) pair.aToB.push({ from, to, edgeId, edgeType: normalized });
      else pair.bToA.push({ from, to, edgeId, edgeType: normalized });
      rawControlEdges.push({ from, to, edgeId, edgeType: normalized });
      continue;
    }

    if (!adj.has(src)) adj.set(src, []);
    if (!adj.has(dst)) adj.set(dst, []);
    adj.get(src)!.push({ to: dst, edgeId, edgeType: normalized });
    adj.get(dst)!.push({ to: src, edgeId, edgeType: normalized });
  }

  if (isControlLike) {
    const EPS_X = 12;
    const conflictResolved = new Map<string, 'aToB' | 'bToA'>();
    for (const [key, pair] of pairDirections.entries()) {
      if (pair.aToB.length === 0 || pair.bToA.length === 0) continue;
      const [aNode, bNode] = key.split('|');
      const ax = xById.get(aNode!);
      const bx = xById.get(bNode!);
      if (typeof ax === 'number' && typeof bx === 'number' && Math.abs(ax - bx) > EPS_X) {
        conflictResolved.set(key, ax > bx ? 'aToB' : 'bToA');
        continue;
      }
      const scoreType = (t: string) => {
        if (t === 'OWNS' || t === 'PARENT_OF') return 3;
        if (t === 'INVESTS_IN' || t === 'SUBSIDIARY_OF') return 2;
        return 1;
      };
      const aScore = Math.max(...pair.aToB.map((e) => scoreType(e.edgeType)));
      const bScore = Math.max(...pair.bToA.map((e) => scoreType(e.edgeType)));
      conflictResolved.set(key, aScore >= bScore ? 'aToB' : 'bToA');
    }

    for (const rawEdge of rawControlEdges) {
      const key = pairKey(rawEdge.from, rawEdge.to);
      const resolved = conflictResolved.get(key);
      if (resolved) {
        const aNode = rawEdge.from < rawEdge.to ? rawEdge.from : rawEdge.to;
        const isAToB = rawEdge.from === aNode;
        if ((resolved === 'aToB' && !isAToB) || (resolved === 'bToA' && isAToB)) continue;
      }
      addDirected(rawEdge.from, rawEdge.to, rawEdge.edgeId, rawEdge.edgeType);
    }
  }

  if (isControlLike) {
    if (adjOut.size === 0 && adjIn.size === 0) return null;
  } else if (adj.size === 0) {
    return null;
  }

  const usedEdgeIds = new Set<string>();
  const maxSteps = isControlLike ? 6 : 1;
  if (!isControlLike) {
    const visited = new Set<string>([hovered]);
    const queue: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
    while (queue.length > 0) {
      const cur = queue.shift()!;
      if (cur.depth >= maxSteps) continue;
      for (const next of (adj.get(cur.id) || [])) {
        usedEdgeIds.add(next.edgeId);
        if (!visited.has(next.to)) {
          visited.add(next.to);
          queue.push({ id: next.to, depth: cur.depth + 1 });
        }
      }
    }
    if (visited.size <= 1 || usedEdgeIds.size === 0) return null;
    return { nodeIds: visited, edgeIds: usedEdgeIds };
  }

  const downVisited = new Set<string>([hovered]);
  const upVisited = new Set<string>([hovered]);
  const qUp: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
  while (qUp.length > 0) {
    const cur = qUp.shift()!;
    if (cur.depth >= maxSteps) continue;
    for (const next of (adjIn.get(cur.id) || [])) {
      usedEdgeIds.add(next.edgeId);
      if (!upVisited.has(next.from)) {
        upVisited.add(next.from);
        qUp.push({ id: next.from, depth: cur.depth + 1 });
      }
    }
  }

  const EPS_X = 12;
  const hoveredX = xById.get(hovered);
  const qDown: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
  while (qDown.length > 0) {
    const cur = qDown.shift()!;
    if (cur.depth >= maxSteps) continue;
    if (!isPerson(hovered) && isPerson(cur.id)) continue;
    if (cur.id !== hovered && upVisited.has(cur.id)) continue;
    for (const next of (adjOut.get(cur.id) || [])) {
      usedEdgeIds.add(next.edgeId);
      if (next.to !== hovered && upVisited.has(next.to)) continue;
      if (isHierarchicalView) {
        const cx = xById.get(cur.id);
        const tx = xById.get(next.to);
        if (
          typeof cx === 'number' &&
          typeof tx === 'number' &&
          Number.isFinite(cx) &&
          Number.isFinite(tx) &&
          (tx > cx - EPS_X || (typeof hoveredX === 'number' && Number.isFinite(hoveredX) && tx > hoveredX - EPS_X))
        ) {
          continue;
        }
      }
      if (!downVisited.has(next.to)) {
        downVisited.add(next.to);
        qDown.push({ id: next.to, depth: cur.depth + 1 });
      }
    }
  }

  const visited = new Set<string>();
  for (const id of downVisited) visited.add(id);
  for (const id of upVisited) visited.add(id);

  if (governanceEdges.length > 0) {
    if (visited.size > 1) {
      for (const edge of governanceEdges) {
        if (visited.has(edge.a) && visited.has(edge.b)) usedEdgeIds.add(edge.id);
      }
    } else {
      for (const edge of governanceEdges) {
        if (edge.a === hovered) {
          visited.add(edge.b);
          usedEdgeIds.add(edge.id);
        } else if (edge.b === hovered) {
          visited.add(edge.a);
          usedEdgeIds.add(edge.id);
        }
      }
    }
  }

  if (partnerEdges.length > 0) {
    for (const edge of partnerEdges) {
      if (visited.has(edge.a) && visited.has(edge.b)) usedEdgeIds.add(edge.id);
    }
    for (const edge of partnerEdges) {
      if (edge.a === hovered) {
        visited.add(edge.b);
        usedEdgeIds.add(edge.id);
      } else if (edge.b === hovered) {
        visited.add(edge.a);
        usedEdgeIds.add(edge.id);
      }
    }
  }

  if (visited.size <= 1 || usedEdgeIds.size === 0) return null;

  const ultimatePersonIds = new Set<string>();
  for (const id of upVisited) {
    if (!isPerson(id)) continue;
    const nextUp = adjIn.get(id) || [];
    const hasHigherController = nextUp.some((next) =>
      upVisited.has(next.from) && CONTROL_ULTIMATE_BLOCKING_TYPES.has(normalizeType(next.edgeType))
    );
    if (!hasHigherController) ultimatePersonIds.add(id);
  }

  return { nodeIds: visited, edgeIds: usedEdgeIds, ultimatePersonIds };
}

function computeDecisionChainHighlight(
  edges: Edge[],
  groupIds: Set<string>,
  hovered: string,
  visibleNodeIds: Set<string>,
  xById: Map<string, number>,
): HoverHighlightResult | null {
  type CorridorAdjOut = { to: string; edgeId: string };
  type CorridorAdjIn = { from: string; edgeId: string };
  const out = new Map<string, CorridorAdjOut[]>();
  const inb = new Map<string, CorridorAdjIn[]>();
  const addDirected = (from: string, to: string, edgeId: string) => {
    if (!out.has(from)) out.set(from, []);
    out.get(from)!.push({ to, edgeId });
    if (!inb.has(to)) inb.set(to, []);
    inb.get(to)!.push({ from, edgeId });
  };
  const normalizeType = (t: string) => String(t || '').toUpperCase().trim();
  const DEPENDENCY_TYPES = new Set(['DEPENDS', 'DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
  const INVERTED_TYPES = new Set<string>(['SUBSIDIARY_OF', ...Array.from(DEPENDENCY_TYPES)]);
  const BIDIRECTIONAL_TYPES = new Set<string>(['PARTNERS_WITH', 'COOPERATES_WITH', 'COMPETES_WITH', 'RELATED_TO', 'LINKED_TO']);
  const X_EPS = 12;
  for (const edge of edges) {
    const edgeId = String((edge as any)?.id || '').trim();
    const src = String((edge as any)?.source || '').trim();
    const dst = String((edge as any)?.target || '').trim();
    if (!edgeId || !src || !dst) continue;
    if (!visibleNodeIds.has(src) || !visibleNodeIds.has(dst)) continue;
    if (groupIds.has(src) || groupIds.has(dst)) continue;
    const edgeType = normalizeType(String((edge as any)?.data?.edgeType || (edge as any)?.label || (edge as any)?.type || ''));
    const corridorEdge = Boolean((edge as any)?.data?.corridorEdge) || edgeType.startsWith('CORRIDOR_');
    if (!corridorEdge) continue;
    let from = src;
    let to = dst;
    const sx = xById.get(src);
    const tx = xById.get(dst);
    const hasGeometry =
      typeof sx === 'number' &&
      Number.isFinite(sx) &&
      typeof tx === 'number' &&
      Number.isFinite(tx) &&
      Math.abs(sx - tx) > X_EPS;
    if (hasGeometry) {
      from = (sx as number) > (tx as number) ? src : dst;
      to = (sx as number) > (tx as number) ? dst : src;
    } else if (INVERTED_TYPES.has(edgeType)) {
      from = dst;
      to = src;
    }
    addDirected(from, to, edgeId);
    if (BIDIRECTIONAL_TYPES.has(edgeType)) addDirected(to, from, edgeId);
  }
  if (out.size === 0 && inb.size === 0) return null;

  const edgeIds = new Set<string>();
  const nodeIds = new Set<string>([hovered]);
  const maxHops = 8;
  const downQueue: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
  const downSeen = new Set<string>([hovered]);
  while (downQueue.length > 0) {
    const cur = downQueue.shift()!;
    if (cur.depth >= maxHops) continue;
    for (const next of (out.get(cur.id) || [])) {
      edgeIds.add(next.edgeId);
      nodeIds.add(next.to);
      if (!downSeen.has(next.to)) {
        downSeen.add(next.to);
        downQueue.push({ id: next.to, depth: cur.depth + 1 });
      }
    }
  }

  const upQueue: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
  const upSeen = new Set<string>([hovered]);
  while (upQueue.length > 0) {
    const cur = upQueue.shift()!;
    if (cur.depth >= maxHops) continue;
    for (const next of (inb.get(cur.id) || [])) {
      edgeIds.add(next.edgeId);
      nodeIds.add(next.from);
      if (!upSeen.has(next.from)) {
        upSeen.add(next.from);
        upQueue.push({ id: next.from, depth: cur.depth + 1 });
      }
    }
  }

  return edgeIds.size === 0 ? null : { nodeIds, edgeIds };
}
