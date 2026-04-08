import test from 'node:test';
import assert from 'node:assert/strict';

import { shouldRefetchGeoObjectsForLensEvent } from './useGeoObjectEvents';

test('shouldRefetchGeoObjectsForLensEvent accepts lens-updated events for the active lens', () => {
  assert.equal(
    shouldRefetchGeoObjectsForLensEvent(
      { type: 'horizon.lens.updated', lensId: 'lens-1' },
      'lens-1',
    ),
    true,
  );
});

test('shouldRefetchGeoObjectsForLensEvent accepts directed materialization events', () => {
  assert.equal(
    shouldRefetchGeoObjectsForLensEvent(
      { type: 'horizon.supply_chain.exploration.materialized', data: { lensId: 'lens-1' } },
      'lens-1',
    ),
    true,
  );
});

test('shouldRefetchGeoObjectsForLensEvent rejects unrelated lens events', () => {
  assert.equal(
    shouldRefetchGeoObjectsForLensEvent(
      { type: 'horizon.lens.updated', lensId: 'lens-2' },
      'lens-1',
    ),
    false,
  );
  assert.equal(
    shouldRefetchGeoObjectsForLensEvent(
      { type: 'horizon.graph.node.upserted', lensId: 'lens-1' },
      'lens-1',
    ),
    false,
  );
});
