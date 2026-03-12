import type { Edge, Node } from 'reactflow';

export interface LayeredAssignment {
  column: number;
  lane: number;
  clusterKey: string;
  orderKey: string;
  importance: number;
  pinned?: boolean;
}

export interface LayeredLayoutOptions {
  nodeWidth?: number;
  nodeHeight?: number;
  columnGap?: number;
  laneGap?: number;
  clusterGap?: number;
  subcolumnGap?: number;
}

const DEFAULT_NODE_WIDTH = 180;
const DEFAULT_NODE_HEIGHT = 161;

function groupByBucket(assignments: Map<string, LayeredAssignment>): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const [id, meta] of assignments.entries()) {
    const key = `${meta.column}:${meta.lane}:${meta.clusterKey}`;
    const list = out.get(key) || [];
    list.push(id);
    out.set(key, list);
  }
  return out;
}

function buildNeighborIndex(edges: Edge[]): Map<string, Set<string>> {
  const out = new Map<string, Set<string>>();
  const add = (a: string, b: string) => {
    const list = out.get(a) || new Set<string>();
    list.add(b);
    out.set(a, list);
  };
  for (const edge of edges) {
    const source = String(edge.source || '').trim();
    const target = String(edge.target || '').trim();
    if (!source || !target) continue;
    add(source, target);
    add(target, source);
  }
  return out;
}

function computeClusterOrder(
  assignments: Map<string, LayeredAssignment>,
  neighbors: Map<string, Set<string>>,
): Map<string, number> {
  const clusterScores = new Map<string, { score: number; count: number; importance: number }>();
  for (const [id, meta] of assignments.entries()) {
    const clusterKey = `${meta.column}:${meta.lane}:${meta.clusterKey}`;
    const entry = clusterScores.get(clusterKey) || { score: 0, count: 0, importance: 0 };
    const linkedColumns = Array.from(neighbors.get(id) || [])
      .map((neighborId) => assignments.get(neighborId))
      .filter((neighbor): neighbor is LayeredAssignment => Boolean(neighbor))
      .map((neighbor) => neighbor.column);
    const barycenter = linkedColumns.length > 0
      ? linkedColumns.reduce((sum, value) => sum + value, 0) / linkedColumns.length
      : meta.column;
    entry.score += barycenter;
    entry.count += 1;
    entry.importance += meta.importance;
    clusterScores.set(clusterKey, entry);
  }

  const out = new Map<string, number>();
  const laneKeys = new Map<string, Array<{ key: string; score: number; importance: number }>>();
  for (const [clusterKey, entry] of clusterScores.entries()) {
    const [column, lane] = clusterKey.split(':');
    const laneKey = `${column}:${lane}`;
    const list = laneKeys.get(laneKey) || [];
    list.push({
      key: clusterKey,
      score: entry.count > 0 ? entry.score / entry.count : 0,
      importance: entry.importance,
    });
    laneKeys.set(laneKey, list);
  }

  for (const list of laneKeys.values()) {
    list.sort((a, b) => {
      if (a.score !== b.score) return a.score - b.score;
      if (a.importance !== b.importance) return b.importance - a.importance;
      return a.key.localeCompare(b.key);
    });
    list.forEach((item, index) => out.set(item.key, index));
  }
  return out;
}

export function layeredClusterLayout<T>(
  nodes: Node<T>[],
  edges: Edge[],
  assignments: Map<string, LayeredAssignment>,
  options: LayeredLayoutOptions = {},
): Node<T>[] {
  const nodeWidth = options.nodeWidth ?? DEFAULT_NODE_WIDTH;
  const nodeHeight = options.nodeHeight ?? DEFAULT_NODE_HEIGHT;
  const columnGap = options.columnGap ?? Math.max(nodeWidth + 170, 360);
  const laneGap = options.laneGap ?? Math.max(nodeHeight + 120, 260);
  const clusterGap = options.clusterGap ?? 52;
  const subcolumnGap = options.subcolumnGap ?? 108;

  const neighbors = buildNeighborIndex(edges);
  const clusterOrder = computeClusterOrder(assignments, neighbors);
  const buckets = groupByBucket(assignments);

  const rankedIdsByBucket = new Map<string, string[]>();
  for (const [bucketKey, ids] of buckets.entries()) {
    ids.sort((a, b) => {
      const metaA = assignments.get(a)!;
      const metaB = assignments.get(b)!;
      const neighborsA = Array.from(neighbors.get(a) || []).map((id) => assignments.get(id)?.column).filter((v): v is number => typeof v === 'number');
      const neighborsB = Array.from(neighbors.get(b) || []).map((id) => assignments.get(id)?.column).filter((v): v is number => typeof v === 'number');
      const baryA = neighborsA.length > 0 ? neighborsA.reduce((sum, value) => sum + value, 0) / neighborsA.length : metaA.column;
      const baryB = neighborsB.length > 0 ? neighborsB.reduce((sum, value) => sum + value, 0) / neighborsB.length : metaB.column;
      if (metaA.pinned && !metaB.pinned) return -1;
      if (!metaA.pinned && metaB.pinned) return 1;
      if (baryA !== baryB) return baryA - baryB;
      if (metaA.importance !== metaB.importance) return metaB.importance - metaA.importance;
      return metaA.orderKey.localeCompare(metaB.orderKey);
    });
    rankedIdsByBucket.set(bucketKey, ids);
  }

  return nodes.map((node) => {
    const id = String(node.id || '').trim();
    const meta = assignments.get(id);
    if (!meta) return node;

    const clusterBucketKey = `${meta.column}:${meta.lane}:${meta.clusterKey}`;
    const ids = rankedIdsByBucket.get(clusterBucketKey) || [id];
    const index = Math.max(0, ids.indexOf(id));
    const clusterIndex = clusterOrder.get(clusterBucketKey) || 0;
    const xBase = meta.column * columnGap;
    const x = xBase + clusterIndex * subcolumnGap;
    const yBase = meta.lane * laneGap + clusterIndex * clusterGap;
    const y = yBase + index * (nodeHeight + 26);

    return {
      ...node,
      position: { x, y },
    };
  });
}
