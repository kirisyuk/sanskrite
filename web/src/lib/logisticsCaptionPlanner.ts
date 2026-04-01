import {
  buildMeaningAggregateZones,
  type MeaningAggregateZone,
} from './pathLegMeaningAggregation';

export type LogisticsCaptionFamily = 'leg' | 'route';

export interface LogisticsCaptionRect {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

export interface LogisticsCaptionCandidate {
  id: string;
  family: LogisticsCaptionFamily;
  productRef: string;
  x: number;
  y: number;
  width: number;
  height: number;
  active?: boolean;
  stressScore?: number;
  priority?: number;
}

export interface LogisticsCaptionAggregate extends MeaningAggregateZone {
  family: LogisticsCaptionFamily;
  priority: number;
  rect: LogisticsCaptionRect;
}

export interface LogisticsCaptionPlan {
  visibleEntryIds: Set<string>;
  hiddenEntryIds: Set<string>;
  occupiedRects: LogisticsCaptionRect[];
  aggregates: LogisticsCaptionAggregate[];
}

export interface LogisticsCaptionPlannerOptions {
  zoom: number;
  candidates: LogisticsCaptionCandidate[];
  blockedRects?: LogisticsCaptionRect[];
  blockedPadding?: number;
  itemPadding?: number;
  aggregatePadding?: number;
  buildAggregateRect: (center: { x: number; y: number }, label: string) => LogisticsCaptionRect;
}

export interface LogisticsCaptionFamilyPlanOptions {
  zoom: number;
  legCandidates: LogisticsCaptionCandidate[];
  routeCandidates: LogisticsCaptionCandidate[];
  buildLegAggregateRect: (center: { x: number; y: number }, label: string) => LogisticsCaptionRect;
  buildRouteAggregateRect: (center: { x: number; y: number }, label: string) => LogisticsCaptionRect;
}

export interface LogisticsCaptionFamilyPlans {
  leg: LogisticsCaptionPlan;
  route: LogisticsCaptionPlan;
}

export interface PublishedLegCaptionPlan {
  occupiedRects: LogisticsCaptionRect[];
  visibleEntryIds: string[];
}

export const PATH_LEG_CAPTION_PLAN_KEY = '__horizonPathLegCaptionPlan';

type PlanTarget = {
  [PATH_LEG_CAPTION_PLAN_KEY]?: PublishedLegCaptionPlan;
};

function rectFromCandidate(candidate: LogisticsCaptionCandidate): LogisticsCaptionRect {
  return {
    left: candidate.x - candidate.width / 2,
    right: candidate.x + candidate.width / 2,
    top: candidate.y - candidate.height / 2,
    bottom: candidate.y + candidate.height / 2,
  };
}

export function logisticsCaptionRectsOverlap(
  left: LogisticsCaptionRect,
  right: LogisticsCaptionRect,
  padding = 0,
): boolean {
  return !(
    left.right + padding < right.left
    || right.right + padding < left.left
    || left.bottom + padding < right.top
    || right.bottom + padding < left.top
  );
}

function compareCandidates(
  left: Pick<LogisticsCaptionCandidate, 'priority' | 'active' | 'stressScore' | 'id'>,
  right: Pick<LogisticsCaptionCandidate, 'priority' | 'active' | 'stressScore' | 'id'>,
): number {
  const leftPriority = Number(left.priority || 0);
  const rightPriority = Number(right.priority || 0);
  if (leftPriority !== rightPriority) return rightPriority - leftPriority;
  if (Boolean(left.active) !== Boolean(right.active)) return Number(Boolean(right.active)) - Number(Boolean(left.active));
  const leftStress = Number(left.stressScore || 0);
  const rightStress = Number(right.stressScore || 0);
  if (Math.abs(leftStress - rightStress) > 1e-6) return rightStress - leftStress;
  return left.id.localeCompare(right.id);
}

function compareZones(
  left: LogisticsCaptionAggregate,
  right: LogisticsCaptionAggregate,
): number {
  if (left.priority !== right.priority) return right.priority - left.priority;
  if (left.entryIds.length !== right.entryIds.length) return right.entryIds.length - left.entryIds.length;
  if (left.active !== right.active) return Number(right.active) - Number(left.active);
  if (Math.abs(left.stressScore - right.stressScore) > 1e-6) return right.stressScore - left.stressScore;
  return left.label.localeCompare(right.label);
}

export function planCaptionDensity(
  options: LogisticsCaptionPlannerOptions,
): LogisticsCaptionPlan {
  const {
    zoom,
    blockedRects = [],
    blockedPadding = 8,
    itemPadding = 4,
    aggregatePadding = 4,
    buildAggregateRect,
  } = options;
  const candidates = [...(options.candidates || [])].filter((candidate) => candidate.id);
  const candidateById = new Map(candidates.map((candidate) => [candidate.id, candidate] as const));
  const usedIds = new Set<string>();
  const hiddenEntryIds = new Set<string>();
  const visibleEntryIds = new Set<string>();
  const individualOccupiedRects: LogisticsCaptionRect[] = [];
  const aggregateOccupiedRects: LogisticsCaptionRect[] = [];

  const aggregateZones = buildMeaningAggregateZones(
    candidates
      .filter((candidate) => candidate.productRef)
      .map((candidate) => ({
        id: candidate.id,
        productRef: candidate.productRef,
        x: candidate.x,
        y: candidate.y,
        width: candidate.width,
        height: candidate.height,
        active: candidate.active,
        stressScore: candidate.stressScore,
      })),
    zoom,
  )
    .map((zone): LogisticsCaptionAggregate | null => {
      const members = zone.entryIds
        .map((entryId) => candidateById.get(entryId))
        .filter((candidate): candidate is LogisticsCaptionCandidate => Boolean(candidate));
      if (members.length === 0) return null;
      const primary = [...members].sort(compareCandidates)[0];
      return {
        ...zone,
        family: primary.family,
        priority: Math.max(...members.map((candidate) => Number(candidate.priority || 0))),
        rect: buildAggregateRect({ x: zone.x, y: zone.y }, zone.label),
      };
    })
    .filter((zone): zone is LogisticsCaptionAggregate => Boolean(zone))
    .sort(compareZones);

  const acceptedAggregates: LogisticsCaptionAggregate[] = [];
  for (const aggregate of aggregateZones) {
    const overlapsBlocked = blockedRects.some((rect) => logisticsCaptionRectsOverlap(rect, aggregate.rect, blockedPadding));
    const overlapsAggregate = aggregateOccupiedRects.some((rect) => logisticsCaptionRectsOverlap(rect, aggregate.rect, aggregatePadding));
    if (overlapsBlocked || overlapsAggregate) continue;
    acceptedAggregates.push(aggregate);
    aggregateOccupiedRects.push(aggregate.rect);
    aggregate.entryIds.forEach((entryId) => {
      usedIds.add(entryId);
      hiddenEntryIds.add(entryId);
    });
  }

  const sortedCandidates = [...candidates].sort(compareCandidates);
  for (const candidate of sortedCandidates) {
    if (usedIds.has(candidate.id)) continue;
    const rect = rectFromCandidate(candidate);
    const overlapsBlocked = blockedRects.some((blockedRect) => logisticsCaptionRectsOverlap(blockedRect, rect, blockedPadding));
    const overlapsAggregate = aggregateOccupiedRects.some((aggregateRect) => logisticsCaptionRectsOverlap(aggregateRect, rect, aggregatePadding));
    const overlapsItem = individualOccupiedRects.some((occupiedRect) => logisticsCaptionRectsOverlap(occupiedRect, rect, itemPadding));
    if (overlapsBlocked || overlapsAggregate || overlapsItem) {
      hiddenEntryIds.add(candidate.id);
      continue;
    }
    visibleEntryIds.add(candidate.id);
    individualOccupiedRects.push(rect);
  }

  return {
    visibleEntryIds,
    hiddenEntryIds,
    occupiedRects: [...aggregateOccupiedRects, ...individualOccupiedRects],
    aggregates: acceptedAggregates,
  };
}

export function planLogisticsCaptionFamilies(
  options: LogisticsCaptionFamilyPlanOptions,
): LogisticsCaptionFamilyPlans {
  const leg = planCaptionDensity({
    zoom: options.zoom,
    candidates: options.legCandidates,
    buildAggregateRect: options.buildLegAggregateRect,
    blockedPadding: 0,
    itemPadding: options.zoom >= 7 ? 10 : 6,
    aggregatePadding: options.zoom >= 7 ? 10 : 6,
  });
  const route = planCaptionDensity({
    zoom: options.zoom,
    candidates: options.routeCandidates,
    blockedRects: leg.occupiedRects,
    buildAggregateRect: options.buildRouteAggregateRect,
    blockedPadding: 8,
    itemPadding: 4,
    aggregatePadding: 4,
  });
  return { leg, route };
}

export function publishLegCaptionPlan(
  target: PlanTarget | null | undefined,
  plan: LogisticsCaptionPlan,
): void {
  if (!target) return;
  target[PATH_LEG_CAPTION_PLAN_KEY] = {
    occupiedRects: [...plan.occupiedRects],
    visibleEntryIds: Array.from(plan.visibleEntryIds),
  };
}

export function readPublishedLegCaptionPlan(
  target: PlanTarget | null | undefined,
): PublishedLegCaptionPlan | null {
  if (!target) return null;
  return target[PATH_LEG_CAPTION_PLAN_KEY] || null;
}
