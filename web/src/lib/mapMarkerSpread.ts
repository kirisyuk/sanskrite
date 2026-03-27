export type MarkerSpreadAnchor = 'right' | 'left' | 'top' | 'bottom';

export interface MarkerSpreadCandidate {
  id: string;
  x: number;
  y: number;
  label: string;
  priority?: number;
}

export interface MarkerSpreadObstacle {
  x: number;
  y: number;
  radius?: number;
}

export interface MarkerSpreadPlacement extends MarkerSpreadCandidate {
  originX: number;
  originY: number;
  anchor: MarkerSpreadAnchor;
  displaced: boolean;
}

export interface MarkerSpreadOptions {
  zoom: number;
  markerRadiusPx?: number;
  obstacleRadiusPx?: number;
  iterations?: number;
}

export interface MarkerSpreadRect {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

const LABEL_HEIGHT_PX = 22;
const LABEL_GAP_PX = 12;
const DEFAULT_MARKER_RADIUS_PX = 17;
const DEFAULT_OBSTACLE_RADIUS_PX = 15;
const LABEL_CHAR_WIDTH_PX = 6.9;
const LABEL_MIN_WIDTH_PX = 92;
const LABEL_MAX_WIDTH_PX = 280;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function stableAngle(seed: string, index: number): number {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) {
    hash = ((hash << 5) - hash + seed.charCodeAt(i)) | 0;
  }
  const normalized = Math.abs(hash + index * 101) % 360;
  return (normalized / 180) * Math.PI;
}

function rectsOverlap(a: MarkerSpreadRect, b: MarkerSpreadRect, padding = 0): boolean {
  return !(
    a.maxX + padding <= b.minX
    || a.minX - padding >= b.maxX
    || a.maxY + padding <= b.minY
    || a.minY - padding >= b.maxY
  );
}

function overlapArea(a: MarkerSpreadRect, b: MarkerSpreadRect): number {
  const width = Math.max(0, Math.min(a.maxX, b.maxX) - Math.max(a.minX, b.minX));
  const height = Math.max(0, Math.min(a.maxY, b.maxY) - Math.max(a.minY, b.minY));
  return width * height;
}

export function estimateMarkerSpreadLabelWidth(label: string): number {
  const normalized = String(label || '').trim();
  if (!normalized) return LABEL_MIN_WIDTH_PX;
  const width = 28 + normalized.length * LABEL_CHAR_WIDTH_PX;
  return clamp(width, LABEL_MIN_WIDTH_PX, LABEL_MAX_WIDTH_PX);
}

export function markerSpreadLabelRect(
  x: number,
  y: number,
  labelWidth: number,
  anchor: MarkerSpreadAnchor,
  markerRadiusPx = DEFAULT_MARKER_RADIUS_PX,
): MarkerSpreadRect {
  if (anchor === 'left') {
    return {
      minX: x - markerRadiusPx - LABEL_GAP_PX - labelWidth,
      maxX: x - markerRadiusPx - LABEL_GAP_PX,
      minY: y - LABEL_HEIGHT_PX / 2,
      maxY: y + LABEL_HEIGHT_PX / 2,
    };
  }
  if (anchor === 'top') {
    return {
      minX: x - labelWidth / 2,
      maxX: x + labelWidth / 2,
      minY: y - markerRadiusPx - LABEL_GAP_PX - LABEL_HEIGHT_PX,
      maxY: y - markerRadiusPx - LABEL_GAP_PX,
    };
  }
  if (anchor === 'bottom') {
    return {
      minX: x - labelWidth / 2,
      maxX: x + labelWidth / 2,
      minY: y + markerRadiusPx + LABEL_GAP_PX,
      maxY: y + markerRadiusPx + LABEL_GAP_PX + LABEL_HEIGHT_PX,
    };
  }
  return {
    minX: x + markerRadiusPx + LABEL_GAP_PX,
    maxX: x + markerRadiusPx + LABEL_GAP_PX + labelWidth,
    minY: y - LABEL_HEIGHT_PX / 2,
    maxY: y + LABEL_HEIGHT_PX / 2,
  };
}

function rectIntersectsCircle(
  rect: MarkerSpreadRect,
  centerX: number,
  centerY: number,
  radius: number,
): boolean {
  const closestX = clamp(centerX, rect.minX, rect.maxX);
  const closestY = clamp(centerY, rect.minY, rect.maxY);
  const dx = centerX - closestX;
  const dy = centerY - closestY;
  return (dx * dx) + (dy * dy) <= radius * radius;
}

function deriveAnchor(dx: number, dy: number, fallbackAngle: number): MarkerSpreadAnchor {
  let vx = dx;
  let vy = dy;
  if (Math.abs(vx) < 1 && Math.abs(vy) < 1) {
    vx = Math.cos(fallbackAngle);
    vy = Math.sin(fallbackAngle);
  }
  if (Math.abs(vx) >= Math.abs(vy)) {
    return vx >= 0 ? 'right' : 'left';
  }
  return vy >= 0 ? 'bottom' : 'top';
}

function anchorOrder(preferred: MarkerSpreadAnchor): MarkerSpreadAnchor[] {
  switch (preferred) {
    case 'left':
      return ['left', 'top', 'bottom', 'right'];
    case 'top':
      return ['top', 'left', 'right', 'bottom'];
    case 'bottom':
      return ['bottom', 'right', 'left', 'top'];
    default:
      return ['right', 'bottom', 'top', 'left'];
  }
}

type SpreadPointState = {
  candidate: MarkerSpreadCandidate;
  index: number;
  originX: number;
  originY: number;
  x: number;
  y: number;
  labelWidth: number;
  fallbackAngle: number;
  localCenterX: number;
  localCenterY: number;
  maxOffsetPx: number;
  priority: number;
  anchor: MarkerSpreadAnchor;
};

export function spreadMarkerPresentation(
  candidates: MarkerSpreadCandidate[],
  obstacles: MarkerSpreadObstacle[],
  options: MarkerSpreadOptions,
): MarkerSpreadPlacement[] {
  if (candidates.length === 0) return [];

  const markerRadius = options.markerRadiusPx ?? DEFAULT_MARKER_RADIUS_PX;
  const obstacleRadius = options.obstacleRadiusPx ?? DEFAULT_OBSTACLE_RADIUS_PX;
  const zoomFactor = clamp((options.zoom - 4) / 6, 0, 1.45);
  const iterations = options.iterations ?? (options.zoom >= 10 ? 28 : options.zoom >= 8 ? 24 : options.zoom >= 6 ? 18 : 14);
  const neighborRadiusPx = 64 + zoomFactor * 44;
  const minMarkerDistancePx = markerRadius * 2 + 6 + zoomFactor * 10;
  const pairPushBase = 5.2 + zoomFactor * 2.4;
  const obstaclePushBase = 4.4 + zoomFactor * 2;
  const attraction = 0.12 - zoomFactor * 0.03;

  const states: SpreadPointState[] = candidates.map((candidate, index) => {
    const labelWidth = estimateMarkerSpreadLabelWidth(candidate.label);
    let centerX = candidate.x;
    let centerY = candidate.y;
    let neighborCount = 0;
    for (let otherIndex = 0; otherIndex < candidates.length; otherIndex += 1) {
      if (otherIndex === index) continue;
      const other = candidates[otherIndex];
      const dx = other.x - candidate.x;
      const dy = other.y - candidate.y;
      if ((dx * dx) + (dy * dy) > neighborRadiusPx * neighborRadiusPx) continue;
      centerX += other.x;
      centerY += other.y;
      neighborCount += 1;
    }
    const localCenterX = centerX / (neighborCount + 1);
    const localCenterY = centerY / (neighborCount + 1);
    let angle = Math.atan2(candidate.y - localCenterY, candidate.x - localCenterX);
    if (!Number.isFinite(angle) || (Math.abs(candidate.x - localCenterX) < 0.5 && Math.abs(candidate.y - localCenterY) < 0.5)) {
      angle = stableAngle(candidate.id, index);
    }
    const density = neighborCount;
    const seedOffsetPx = density > 0
      ? 16 + Math.min(40, labelWidth * 0.08 + density * 5.5)
      : 0;
    const x = candidate.x + Math.cos(angle) * seedOffsetPx;
    const y = candidate.y + Math.sin(angle) * seedOffsetPx;
    return {
      candidate,
      index,
      originX: candidate.x,
      originY: candidate.y,
      x,
      y,
      labelWidth,
      fallbackAngle: angle,
      localCenterX,
      localCenterY,
      maxOffsetPx: 28 + zoomFactor * 42 + Math.min(52, labelWidth * 0.22 + density * 8),
      priority: Number(candidate.priority || 0),
      anchor: deriveAnchor(x - localCenterX, y - localCenterY, angle),
    };
  });

  for (let iteration = 0; iteration < iterations; iteration += 1) {
    for (const state of states) {
      state.anchor = deriveAnchor(state.x - state.localCenterX, state.y - state.localCenterY, state.fallbackAngle);
    }

    for (let i = 0; i < states.length; i += 1) {
      for (let j = i + 1; j < states.length; j += 1) {
        const left = states[i];
        const right = states[j];
        let dx = right.x - left.x;
        let dy = right.y - left.y;
        let distance = Math.sqrt((dx * dx) + (dy * dy));
        if (distance < 1e-3) {
          dx = Math.cos(left.fallbackAngle);
          dy = Math.sin(left.fallbackAngle);
          distance = 1;
        }
        const leftRect = markerSpreadLabelRect(left.x, left.y, left.labelWidth, left.anchor, markerRadius);
        const rightRect = markerSpreadLabelRect(right.x, right.y, right.labelWidth, right.anchor, markerRadius);
        let push = 0;

        if (distance < minMarkerDistancePx) {
          push += (minMarkerDistancePx - distance) * 0.42;
        }
        if (rectsOverlap(leftRect, rightRect, 8)) {
          push += pairPushBase + overlapArea(leftRect, rightRect) * 0.012;
        }
        if (push <= 0) continue;

        const nx = dx / distance;
        const ny = dy / distance;
        left.x -= nx * push;
        left.y -= ny * push;
        right.x += nx * push;
        right.y += ny * push;
      }
    }

    for (const state of states) {
      const labelRect = markerSpreadLabelRect(state.x, state.y, state.labelWidth, state.anchor, markerRadius);
      for (const obstacle of obstacles) {
        let dx = state.x - obstacle.x;
        let dy = state.y - obstacle.y;
        let distance = Math.sqrt((dx * dx) + (dy * dy));
        if (distance < 1e-3) {
          dx = Math.cos(state.fallbackAngle);
          dy = Math.sin(state.fallbackAngle);
          distance = 1;
        }
        const effectiveObstacleRadius = obstacle.radius ?? obstacleRadius;
        let push = 0;
        const minDistance = markerRadius + effectiveObstacleRadius + 4;
        if (distance < minDistance) {
          push += (minDistance - distance) * 0.38;
        }
        if (rectIntersectsCircle(labelRect, obstacle.x, obstacle.y, effectiveObstacleRadius + 6)) {
          push += obstaclePushBase;
        }
        if (push <= 0) continue;
        const nx = dx / distance;
        const ny = dy / distance;
        state.x += nx * push;
        state.y += ny * push;
      }

      state.x += (state.originX - state.x) * attraction;
      state.y += (state.originY - state.y) * attraction;

      const offsetX = state.x - state.originX;
      const offsetY = state.y - state.originY;
      const offsetDistance = Math.sqrt((offsetX * offsetX) + (offsetY * offsetY));
      if (offsetDistance > state.maxOffsetPx) {
        const scale = state.maxOffsetPx / offsetDistance;
        state.x = state.originX + offsetX * scale;
        state.y = state.originY + offsetY * scale;
      }
    }
  }

  const assignedRects: MarkerSpreadRect[] = [];
  const ordered = [...states].sort((left, right) => {
    if (left.priority !== right.priority) return right.priority - left.priority;
    return left.index - right.index;
  });

  for (const state of ordered) {
    const preferred = deriveAnchor(state.x - state.localCenterX, state.y - state.localCenterY, state.fallbackAngle);
    let bestAnchor = preferred;
    let bestScore = Number.POSITIVE_INFINITY;

    for (const [choiceIndex, candidateAnchor] of anchorOrder(preferred).entries()) {
      const rect = markerSpreadLabelRect(state.x, state.y, state.labelWidth, candidateAnchor, markerRadius);
      let penalty = choiceIndex * 18;
      for (const assignedRect of assignedRects) {
        if (rectsOverlap(rect, assignedRect, 6)) {
          penalty += 1200 + overlapArea(rect, assignedRect) * 0.02;
        }
      }
      for (const obstacle of obstacles) {
        const effectiveObstacleRadius = obstacle.radius ?? obstacleRadius;
        if (rectIntersectsCircle(rect, obstacle.x, obstacle.y, effectiveObstacleRadius + 5)) {
          penalty += 240;
        }
      }
      if (penalty < bestScore) {
        bestScore = penalty;
        bestAnchor = candidateAnchor;
      }
    }

    state.anchor = bestAnchor;
    assignedRects.push(markerSpreadLabelRect(state.x, state.y, state.labelWidth, state.anchor, markerRadius));
  }

  return states
    .sort((left, right) => left.index - right.index)
    .map((state) => ({
      ...state.candidate,
      originX: state.originX,
      originY: state.originY,
      x: state.x,
      y: state.y,
      anchor: state.anchor,
      displaced: Math.hypot(state.x - state.originX, state.y - state.originY) >= 6,
    }));
}
