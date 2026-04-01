import test from 'node:test';
import assert from 'node:assert/strict';

import { buildMeaningAggregateSummary, buildMeaningAggregateZones } from './pathLegMeaningAggregation';

test('buildMeaningAggregateSummary ranks active and repeated products first', () => {
  const summary = buildMeaningAggregateSummary([
    { productRef: '/product/raw/hydrocarbon/extraction/natural-gas', active: false, stressScore: 0.2 },
    { productRef: '/product/raw/hydrocarbon/extraction/natural-gas', active: false, stressScore: 0.3 },
    { productRef: '/product/processed/chemical/feedstock/ammonia', active: true, stressScore: 0.1 },
    { productRef: '/product/processed/chemical/feedstock/ammonia', active: true, stressScore: 0.2 },
    { productRef: '/product/processed/food/processed-foods', active: false, stressScore: 0.4 },
  ], 6.5);

  assert.ok(summary);
  assert.deepEqual(summary?.productRefs.slice(0, 3), [
    '/product/processed/chemical/feedstock/ammonia',
    '/product/raw/hydrocarbon/extraction/natural-gas',
    '/product/processed/food/processed-foods',
  ]);
  assert.match(summary?.label || '', /^Products:\n/i);
  assert.match(summary?.label || '', /Ammonia/i);
});

test('buildMeaningAggregateSummary compacts long product lists by zoom', () => {
  const summary = buildMeaningAggregateSummary([
    { productRef: '/product/a' },
    { productRef: '/product/b' },
    { productRef: '/product/c' },
  ], 5);

  assert.ok(summary);
  assert.equal(summary?.productRefs.length, 3);
  assert.match(summary?.label || '', /\+ 2 more$/);
});

test('buildMeaningAggregateSummary returns null when there is no multi-product aggregate', () => {
  assert.equal(buildMeaningAggregateSummary([{ productRef: '/product/a' }, { productRef: '/product/a' }], 7), null);
  assert.equal(buildMeaningAggregateSummary([], 7), null);
});

test('buildMeaningAggregateZones aggregates only dense local caption zones', () => {
  const zones = buildMeaningAggregateZones([
    { id: 'a1', productRef: '/product/processed/chemical/feedstock/ammonia', x: 100, y: 100, width: 80, height: 28 },
    { id: 'a2', productRef: '/product/raw/hydrocarbon/extraction/natural-gas', x: 118, y: 104, width: 80, height: 28 },
    { id: 'a3', productRef: '/product/processed/food/processed-foods', x: 136, y: 108, width: 80, height: 28 },
    { id: 'a4', productRef: '/product/finished/consumer/retail/flour', x: 126, y: 126, width: 80, height: 28 },
    { id: 'a5', productRef: '/product/processed/food/grain/wheat', x: 140, y: 132, width: 80, height: 28 },
    { id: 'solo', productRef: '/product/processed/food/staples/rice', x: 260, y: 220, width: 80, height: 28 },
  ], 6.5);

  assert.equal(zones.length, 1);
  assert.deepEqual(zones[0]?.entryIds.sort(), ['a1', 'a2', 'a3', 'a4', 'a5']);
});

test('buildMeaningAggregateZones keeps separated dense zones apart', () => {
  const zones = buildMeaningAggregateZones([
    { id: 'c1', productRef: '/product/processed/chemical/feedstock/ammonia', x: 100, y: 100, width: 84, height: 28 },
    { id: 'c2', productRef: '/product/raw/hydrocarbon/extraction/natural-gas', x: 132, y: 104, width: 84, height: 28 },
    { id: 'c3', productRef: '/product/processed/food/processed-foods', x: 164, y: 108, width: 84, height: 28 },
    { id: 'c4', productRef: '/product/finished/consumer/retail/flour', x: 196, y: 112, width: 84, height: 28 },
    { id: 'c5', productRef: '/product/processed/food/grain/wheat', x: 312, y: 116, width: 84, height: 28 },
    { id: 'c6', productRef: '/product/processed/food/staples/rice', x: 344, y: 120, width: 84, height: 28 },
    { id: 'c7', productRef: '/product/processed/food/sugar', x: 376, y: 124, width: 84, height: 28 },
    { id: 'c8', productRef: '/product/processed/food/oil', x: 408, y: 128, width: 84, height: 28 },
  ], 6.5);

  assert.equal(zones.length, 2);
  assert.ok(zones.every((zone) => zone.entryIds.length === 4));
});
