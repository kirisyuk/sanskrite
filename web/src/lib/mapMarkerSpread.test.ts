import test from 'node:test';
import assert from 'node:assert/strict';
import {
  estimateMarkerSpreadLabelWidth,
  markerSpreadLabelRect,
  spreadMarkerPresentation,
} from './mapMarkerSpread';

function rectsOverlap(
  left: { minX: number; minY: number; maxX: number; maxY: number },
  right: { minX: number; minY: number; maxX: number; maxY: number },
): boolean {
  return !(
    left.maxX <= right.minX
    || left.minX >= right.maxX
    || left.maxY <= right.minY
    || left.minY >= right.maxY
  );
}

function rectIntersectsCircle(
  rect: { minX: number; minY: number; maxX: number; maxY: number },
  centerX: number,
  centerY: number,
  radius: number,
): boolean {
  const closestX = Math.min(rect.maxX, Math.max(rect.minX, centerX));
  const closestY = Math.min(rect.maxY, Math.max(rect.minY, centerY));
  const dx = centerX - closestX;
  const dy = centerY - closestY;
  return (dx * dx) + (dy * dy) <= radius * radius;
}

test('spreadMarkerPresentation leaves isolated marker at its origin', () => {
  const [placement] = spreadMarkerPresentation(
    [{ id: 'solo', x: 120, y: 160, label: 'Solo plant', priority: 100 }],
    [],
    { zoom: 8 },
  );

  assert.ok(placement);
  assert.equal(placement.displaced, false);
  assert.equal(placement.x, 120);
  assert.equal(placement.y, 160);
});

test('spreadMarkerPresentation separates dense long-label pairs', () => {
  const placements = spreadMarkerPresentation(
    [
      { id: 'a', x: 200, y: 200, label: 'Meloman Toy Packaging Production', priority: 2000 },
      { id: 'b', x: 212, y: 205, label: 'Technodom Almaty Logistics Center', priority: 1000 },
    ],
    [],
    { zoom: 8 },
  );

  assert.equal(placements.length, 2);
  assert.ok(placements.every((placement) => placement.displaced));
  const first = placements[0];
  const second = placements[1];
  const firstRect = markerSpreadLabelRect(first.x, first.y, estimateMarkerSpreadLabelWidth(first.label), first.anchor);
  const secondRect = markerSpreadLabelRect(second.x, second.y, estimateMarkerSpreadLabelWidth(second.label), second.anchor);

  assert.equal(rectsOverlap(firstRect, secondRect), false);
  assert.ok(Math.hypot(first.x - second.x, first.y - second.y) > 40);
});

test('spreadMarkerPresentation avoids passive marker obstacles', () => {
  const [placement] = spreadMarkerPresentation(
    [{ id: 'gateway', x: 320, y: 260, label: 'Almaty Toy Production Gateway', priority: 3000 }],
    [{ x: 366, y: 260, radius: 16 }],
    { zoom: 9 },
  );

  assert.ok(placement);
  const labelRect = markerSpreadLabelRect(
    placement.x,
    placement.y,
    estimateMarkerSpreadLabelWidth(placement.label),
    placement.anchor,
  );

  assert.equal(rectIntersectsCircle(labelRect, 366, 260, 22), false);
});
