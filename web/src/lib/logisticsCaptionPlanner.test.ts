import test from 'node:test';
import assert from 'node:assert/strict';

import {
  planCaptionDensity,
  planLogisticsCaptionFamilies,
} from './logisticsCaptionPlanner';

test('planCaptionDensity replaces a dense local group with one aggregate', () => {
  const plan = planCaptionDensity({
    zoom: 6.5,
    candidates: [
      { id: 'a1', family: 'leg', productRef: '/product/a', x: 100, y: 100, width: 84, height: 28, priority: 10 },
      { id: 'a2', family: 'leg', productRef: '/product/b', x: 122, y: 104, width: 84, height: 28, priority: 10 },
      { id: 'a3', family: 'leg', productRef: '/product/c', x: 142, y: 108, width: 84, height: 28, priority: 10 },
      { id: 'a4', family: 'leg', productRef: '/product/d', x: 126, y: 124, width: 84, height: 28, priority: 10 },
      { id: 'a5', family: 'leg', productRef: '/product/e', x: 148, y: 128, width: 84, height: 28, priority: 10 },
    ],
    buildAggregateRect: ({ x, y }, _label) => ({
      left: x - 60,
      right: x + 60,
      top: y - 20,
      bottom: y + 20,
    }),
  });

  assert.equal(plan.aggregates.length, 1);
  assert.equal(plan.visibleEntryIds.size, 0);
  assert.deepEqual([...plan.hiddenEntryIds].sort(), ['a1', 'a2', 'a3', 'a4', 'a5']);
});

test('planLogisticsCaptionFamilies gives leg captions priority over route captions in the same zone', () => {
  const plans = planLogisticsCaptionFamilies({
    zoom: 6.5,
    legCandidates: [
      { id: 'l1', family: 'leg', productRef: '/product/a', x: 100, y: 100, width: 84, height: 28, priority: 100 },
      { id: 'l2', family: 'leg', productRef: '/product/b', x: 120, y: 102, width: 84, height: 28, priority: 100 },
      { id: 'l3', family: 'leg', productRef: '/product/c', x: 140, y: 104, width: 84, height: 28, priority: 100 },
      { id: 'l4', family: 'leg', productRef: '/product/d', x: 124, y: 120, width: 84, height: 28, priority: 100 },
      { id: 'l5', family: 'leg', productRef: '/product/e', x: 146, y: 122, width: 84, height: 28, priority: 100 },
    ],
    routeCandidates: [
      { id: 'r1', family: 'route', productRef: '/product/x', x: 108, y: 106, width: 90, height: 26, priority: 50 },
      { id: 'r2', family: 'route', productRef: '/product/y', x: 132, y: 110, width: 90, height: 26, priority: 50 },
      { id: 'r3', family: 'route', productRef: '/product/z', x: 156, y: 114, width: 90, height: 26, priority: 50 },
      { id: 'r4', family: 'route', productRef: '/product/q', x: 140, y: 132, width: 90, height: 26, priority: 50 },
    ],
    buildLegAggregateRect: ({ x, y }, _label) => ({
      left: x - 70,
      right: x + 70,
      top: y - 22,
      bottom: y + 22,
    }),
    buildRouteAggregateRect: ({ x, y }, _label) => ({
      left: x - 65,
      right: x + 65,
      top: y - 20,
      bottom: y + 20,
    }),
  });

  assert.equal(plans.leg.aggregates.length, 1);
  assert.equal(plans.route.aggregates.length, 0);
  assert.equal(plans.route.visibleEntryIds.size, 0);
  assert.deepEqual([...plans.route.hiddenEntryIds].sort(), ['r1', 'r2', 'r3', 'r4']);
});
