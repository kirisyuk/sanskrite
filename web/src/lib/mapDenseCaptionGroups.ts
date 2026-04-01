export interface DenseCaptionEntry {
  id: string;
  x: number;
  y: number;
  priority?: number;
}

export interface DenseCaptionGroup {
  id: string;
  x: number;
  y: number;
  memberIds: string[];
}

export interface DenseCaptionGroupOptions {
  joinDistancePx?: number;
  minGroupSize?: number;
  maxSpanXPx?: number;
  maxSpanYPx?: number;
}

type Bounds = {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
};

function distanceSq(left: DenseCaptionEntry, right: DenseCaptionEntry): number {
  const dx = left.x - right.x;
  const dy = left.y - right.y;
  return dx * dx + dy * dy;
}

function appendBounds(bounds: Bounds, entry: DenseCaptionEntry): Bounds {
  return {
    minX: Math.min(bounds.minX, entry.x),
    minY: Math.min(bounds.minY, entry.y),
    maxX: Math.max(bounds.maxX, entry.x),
    maxY: Math.max(bounds.maxY, entry.y),
  };
}

function emptyBounds(entry: DenseCaptionEntry): Bounds {
  return {
    minX: entry.x,
    minY: entry.y,
    maxX: entry.x,
    maxY: entry.y,
  };
}

function boundsFit(bounds: Bounds, maxSpanXPx: number, maxSpanYPx: number): boolean {
  return (bounds.maxX - bounds.minX) <= maxSpanXPx && (bounds.maxY - bounds.minY) <= maxSpanYPx;
}

export function buildDenseCaptionGroups(
  entries: DenseCaptionEntry[],
  options: DenseCaptionGroupOptions = {},
): DenseCaptionGroup[] {
  const joinDistancePx = options.joinDistancePx ?? 56;
  const minGroupSize = options.minGroupSize ?? 4;
  const maxSpanXPx = options.maxSpanXPx ?? 168;
  const maxSpanYPx = options.maxSpanYPx ?? 124;
  const joinDistanceSq = joinDistancePx * joinDistancePx;
  const sorted = [...entries]
    .filter((entry) => entry.id)
    .sort((left, right) => {
      const priorityDelta = Number(right.priority || 0) - Number(left.priority || 0);
      if (priorityDelta !== 0) return priorityDelta;
      return left.id.localeCompare(right.id);
    });
  const used = new Set<string>();
  const groups: DenseCaptionGroup[] = [];

  for (const seed of sorted) {
    if (used.has(seed.id)) continue;
    const members: DenseCaptionEntry[] = [seed];
    let bounds = emptyBounds(seed);
    let changed = true;

    while (changed) {
      changed = false;
      for (const candidate of sorted) {
        if (used.has(candidate.id) || members.some((member) => member.id === candidate.id)) continue;
        const touches = members.some((member) => distanceSq(member, candidate) <= joinDistanceSq);
        if (!touches) continue;
        const nextBounds = appendBounds(bounds, candidate);
        if (!boundsFit(nextBounds, maxSpanXPx, maxSpanYPx)) continue;
        members.push(candidate);
        bounds = nextBounds;
        changed = true;
      }
    }

    if (members.length < minGroupSize) continue;
    members.forEach((member) => used.add(member.id));
    const centroidX = members.reduce((sum, member) => sum + member.x, 0) / members.length;
    const centroidY = members.reduce((sum, member) => sum + member.y, 0) / members.length;
    groups.push({
      id: `dense-caption:${members.map((member) => member.id).sort().join('|')}`,
      x: centroidX,
      y: centroidY,
      memberIds: members.map((member) => member.id),
    });
  }

  return groups;
}
