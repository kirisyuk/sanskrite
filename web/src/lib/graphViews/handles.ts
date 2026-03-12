import type { Edge, Node } from 'reactflow';
import type { HorizonNodeData } from '../../components/HorizonGraphNodeTypes';

function nodeMapFor(nodes: Node<HorizonNodeData>[]): Map<string, Node<HorizonNodeData>> {
  const out = new Map<string, Node<HorizonNodeData>>();
  for (const node of nodes) {
    const data = node.data as any;
    if (data?.isGroup) continue;
    out.set(String(node.id || '').trim(), node);
    if (data?.fragmentId) out.set(String(data.fragmentId).trim(), node);
  }
  return out;
}

function pointOf(node?: Node<HorizonNodeData>) {
  return {
    x: Number((node as any)?.position?.x) || 0,
    y: Number((node as any)?.position?.y) || 0,
  };
}

function directionalHandles(
  sourceNode: Node<HorizonNodeData> | undefined,
  targetNode: Node<HorizonNodeData> | undefined,
  opts: { sameLaneThreshold?: number; preferVertical?: boolean } = {},
): { sourceHandle: string; targetHandle: string } {
  const source = pointOf(sourceNode);
  const target = pointOf(targetNode);
  const dx = target.x - source.x;
  const dy = target.y - source.y;
  const sameLaneThreshold = opts.sameLaneThreshold ?? 36;

  if (opts.preferVertical || Math.abs(dx) <= sameLaneThreshold) {
    if (dy >= 0) return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
    return { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
  }

  if (dx > 0) return { sourceHandle: 'source-right', targetHandle: 'target-left' };
  return { sourceHandle: 'source-left', targetHandle: 'target-right' };
}

export function recalculateSupplyFlowHandles(
  edges: Edge[],
  nodes: Node<HorizonNodeData>[],
): Edge[] {
  const nodeMap = nodeMapFor(nodes);
  return edges.map((edge) => {
    const sourceNode = nodeMap.get(String(edge.source || '').trim());
    const targetNode = nodeMap.get(String(edge.target || '').trim());
    const meta = ((edge as any)?.data?.supplyFlowMeta || {}) as any;
    const routingHint = ((edge as any)?.data?.routingHint || {}) as any;
    const preferVertical = routingHint.preferredAxis === 'vertical' || meta.flowClass === 'jurisdiction' || meta.flowClass === 'route-support';
    const handles = directionalHandles(sourceNode, targetNode, { sameLaneThreshold: 44, preferVertical });
    return { ...edge, ...handles };
  });
}

export function recalculateInfluenceHandles(
  edges: Edge[],
  nodes: Node<HorizonNodeData>[],
): Edge[] {
  const nodeMap = nodeMapFor(nodes);
  return edges.map((edge) => {
    const sourceNode = nodeMap.get(String(edge.source || '').trim());
    const targetNode = nodeMap.get(String(edge.target || '').trim());
    const routingHint = ((edge as any)?.data?.routingHint || {}) as any;
    const sameCluster = Boolean(routingHint.sameCluster);
    const handles = directionalHandles(sourceNode, targetNode, { sameLaneThreshold: sameCluster ? 72 : 30, preferVertical: sameCluster });
    return { ...edge, ...handles };
  });
}

export function recalculateValueChainHandles(
  edges: Edge[],
  nodes: Node<HorizonNodeData>[],
): Edge[] {
  const nodeMap = nodeMapFor(nodes);
  return edges.map((edge) => {
    const sourceNode = nodeMap.get(String(edge.source || '').trim());
    const targetNode = nodeMap.get(String(edge.target || '').trim());
    const meta = ((edge as any)?.data?.valueChainMeta || {}) as any;
    const routingHint = ((edge as any)?.data?.routingHint || {}) as any;
    const preferVertical = routingHint.preferredAxis === 'vertical' || meta.valueClass === 'governance-context';
    const handles = directionalHandles(sourceNode, targetNode, { sameLaneThreshold: 40, preferVertical });
    return { ...edge, ...handles };
  });
}
