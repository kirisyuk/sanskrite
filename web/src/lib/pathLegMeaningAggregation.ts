import { humanizeCanonicalPath } from './uiUtils';

export interface MeaningAggregateEntry {
  productRef: string;
  active?: boolean;
  stressScore?: number;
}

export interface MeaningAggregateSummary {
  label: string;
  productRefs: string[];
}

export interface MeaningAggregateZoneEntry {
  id: string;
  productRef: string;
  x: number;
  y: number;
  width: number;
  height: number;
  active?: boolean;
  stressScore?: number;
}

export interface MeaningAggregateZone {
  entryIds: string[];
  label: string;
  productRefs: string[];
  x: number;
  y: number;
  active: boolean;
  stressScore: number;
}

function truncateAggregateLabel(value: string, maxLength: number): string {
  const normalized = String(value || '').replace(/\s+/g, ' ').trim();
  if (!normalized) return '';
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, Math.max(0, maxLength - 3)).trimEnd()}...`;
}

function maxAggregateNamesForZoom(zoom: number): number {
  if (zoom <= 5.5) return 1;
  if (zoom <= 7.5) return 2;
  return 3;
}

function minAggregateClusterSizeForZoom(zoom: number): number {
  if (zoom >= 8) return 4;
  if (zoom >= 6) return 4;
  return 4;
}

function clusterJoinDistanceForZoom(zoom: number): number {
  if (zoom >= 8) return 40;
  if (zoom >= 6) return 52;
  return 64;
}

function clusterMaxSpanXForZoom(zoom: number): number {
  if (zoom >= 8) return 168;
  if (zoom >= 6) return 184;
  return 220;
}

function clusterMaxSpanYForZoom(zoom: number): number {
  if (zoom >= 8) return 112;
  if (zoom >= 6) return 132;
  return 156;
}

function rectsOverlap(
  left: MeaningAggregateZoneEntry,
  right: MeaningAggregateZoneEntry,
  padding = 0,
): boolean {
  const leftHalfWidth = left.width / 2;
  const leftHalfHeight = left.height / 2;
  const rightHalfWidth = right.width / 2;
  const rightHalfHeight = right.height / 2;
  return !(
    left.x + leftHalfWidth + padding < right.x - rightHalfWidth
    || right.x + rightHalfWidth + padding < left.x - leftHalfWidth
    || left.y + leftHalfHeight + padding < right.y - rightHalfHeight
    || right.y + rightHalfHeight + padding < left.y - leftHalfHeight
  );
}

function distance(left: MeaningAggregateZoneEntry, right: MeaningAggregateZoneEntry): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

function clusterBounds(entries: MeaningAggregateZoneEntry[]): { minX: number; minY: number; maxX: number; maxY: number } {
  let minX = Number.POSITIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;
  for (const entry of entries) {
    minX = Math.min(minX, entry.x - entry.width / 2);
    minY = Math.min(minY, entry.y - entry.height / 2);
    maxX = Math.max(maxX, entry.x + entry.width / 2);
    maxY = Math.max(maxY, entry.y + entry.height / 2);
  }
  return { minX, minY, maxX, maxY };
}

function entriesCanShareZone(
  left: MeaningAggregateZoneEntry,
  right: MeaningAggregateZoneEntry,
  zoom: number,
): boolean {
  const overlap = rectsOverlap(left, right, zoom >= 7 ? 10 : 6);
  const near = distance(left, right) <= clusterJoinDistanceForZoom(zoom);
  return overlap || near;
}

export function buildMeaningAggregateSummary(
  entries: MeaningAggregateEntry[],
  zoom: number,
): MeaningAggregateSummary | null {
  const byProductRef = new Map<string, { score: number; activeCount: number; label: string }>();
  for (const entry of entries || []) {
    const productRef = String(entry?.productRef || '').trim();
    if (!productRef) continue;
    const label = truncateAggregateLabel(humanizeCanonicalPath(productRef) || productRef, 26);
    if (!label) continue;
    const existing = byProductRef.get(productRef) || { score: 0, activeCount: 0, label };
    existing.score += 1 + Math.max(0, Number(entry?.stressScore || 0)) * 0.35;
    existing.activeCount += entry?.active ? 1 : 0;
    existing.label = existing.label || label;
    byProductRef.set(productRef, existing);
  }

  const ranked = Array.from(byProductRef.entries())
    .sort((left, right) => {
      const [, leftMeta] = left;
      const [, rightMeta] = right;
      if (leftMeta.activeCount !== rightMeta.activeCount) return rightMeta.activeCount - leftMeta.activeCount;
      if (Math.abs(leftMeta.score - rightMeta.score) > 1e-6) return rightMeta.score - leftMeta.score;
      return leftMeta.label.localeCompare(rightMeta.label);
    });
  if (ranked.length < 2) return null;

  const productRefs = ranked.map(([productRef]) => productRef);
  const maxNames = maxAggregateNamesForZoom(zoom);
  const visibleLabels = ranked.slice(0, maxNames).map(([, meta]) => meta.label).filter(Boolean);
  if (visibleLabels.length === 0) return null;

  const remainingCount = Math.max(0, ranked.length - visibleLabels.length);
  const suffix = remainingCount > 0 ? ` + ${remainingCount} more` : '';
  return {
    label: `Products:\n${visibleLabels.join(', ')}${suffix}`,
    productRefs,
  };
}

export function buildMeaningAggregateZones(
  entries: MeaningAggregateZoneEntry[],
  zoom: number,
): MeaningAggregateZone[] {
  const minClusterSize = minAggregateClusterSizeForZoom(zoom);
  const maxSpanX = clusterMaxSpanXForZoom(zoom);
  const maxSpanY = clusterMaxSpanYForZoom(zoom);
  const sorted = [...entries].sort((left, right) => {
    if (Boolean(left.active) !== Boolean(right.active)) return Number(Boolean(right.active)) - Number(Boolean(left.active));
    return Number(right.stressScore || 0) - Number(left.stressScore || 0);
  });
  const used = new Set<string>();
  const zones: MeaningAggregateZone[] = [];

  for (const seed of sorted) {
    if (used.has(seed.id)) continue;
    const cluster: MeaningAggregateZoneEntry[] = [seed];
    let centroidX = seed.x;
    let centroidY = seed.y;
    used.add(seed.id);

    for (const candidate of sorted) {
      if (used.has(candidate.id)) continue;
      const touchesCluster = cluster.some((member) => entriesCanShareZone(member, candidate, zoom));
      if (!touchesCluster) continue;
      const nextCluster = [...cluster, candidate];
      const nextBounds = clusterBounds(nextCluster);
      if ((nextBounds.maxX - nextBounds.minX) > maxSpanX || (nextBounds.maxY - nextBounds.minY) > maxSpanY) continue;
      cluster.push(candidate);
      const nextCentroidX = nextCluster.reduce((sum, entry) => sum + entry.x, 0) / nextCluster.length;
      const nextCentroidY = nextCluster.reduce((sum, entry) => sum + entry.y, 0) / nextCluster.length;
      centroidX = nextCentroidX;
      centroidY = nextCentroidY;
      used.add(candidate.id);
    }

    if (cluster.length < minClusterSize) {
      for (const entry of cluster) used.delete(entry.id);
      continue;
    }

    const summary = buildMeaningAggregateSummary(
      cluster.map((entry) => ({
        productRef: entry.productRef,
        active: entry.active,
        stressScore: entry.stressScore,
      })),
      zoom,
    );
    if (!summary) {
      for (const entry of cluster) used.delete(entry.id);
      continue;
    }

    zones.push({
      entryIds: cluster.map((entry) => entry.id),
      label: summary.label,
      productRefs: summary.productRefs,
      x: centroidX,
      y: centroidY,
      active: cluster.some((entry) => Boolean(entry.active)),
      stressScore: Math.max(...cluster.map((entry) => Number(entry.stressScore || 0)), 0),
    });
  }

  return zones;
}
