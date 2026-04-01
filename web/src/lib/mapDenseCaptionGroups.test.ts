import test from 'node:test';
import assert from 'node:assert/strict';
import { buildDenseCaptionGroups } from './mapDenseCaptionGroups';

test('buildDenseCaptionGroups groups dense screen-space entries when cluster size exceeds threshold', () => {
  const groups = buildDenseCaptionGroups([
    { id: 'a', x: 100, y: 100 },
    { id: 'b', x: 126, y: 104 },
    { id: 'c', x: 118, y: 132 },
    { id: 'd', x: 142, y: 126 },
    { id: 'far', x: 360, y: 360 },
  ], {
    joinDistancePx: 42,
    minGroupSize: 4,
    maxSpanXPx: 96,
    maxSpanYPx: 96,
  });

  assert.equal(groups.length, 1);
  assert.deepEqual(new Set(groups[0].memberIds), new Set(['a', 'b', 'c', 'd']));
});

test('buildDenseCaptionGroups leaves small nearby sets ungrouped', () => {
  const groups = buildDenseCaptionGroups([
    { id: 'a', x: 100, y: 100 },
    { id: 'b', x: 128, y: 108 },
    { id: 'c', x: 114, y: 134 },
  ], {
    joinDistancePx: 44,
    minGroupSize: 4,
  });

  assert.equal(groups.length, 0);
});

test('buildDenseCaptionGroups refuses overly wide connected chains', () => {
  const groups = buildDenseCaptionGroups([
    { id: 'a', x: 100, y: 100 },
    { id: 'b', x: 140, y: 102 },
    { id: 'c', x: 178, y: 98 },
    { id: 'd', x: 220, y: 100 },
  ], {
    joinDistancePx: 44,
    minGroupSize: 4,
    maxSpanXPx: 96,
    maxSpanYPx: 64,
  });

  assert.equal(groups.length, 0);
});
