import test from 'node:test';
import assert from 'node:assert/strict';

import { productStreamEdgeVisualKind } from './taxonomy';

test('productStreamEdgeVisualKind matches stream API + legacy tokens', () => {
  assert.equal(productStreamEdgeVisualKind('assembly'), 'assembly');
  assert.equal(productStreamEdgeVisualKind('usage'), 'usage');
  assert.equal(productStreamEdgeVisualKind('transformation'), 'transformation');
  assert.equal(productStreamEdgeVisualKind('USED_IN'), 'usage');
  assert.equal(productStreamEdgeVisualKind('used-in'), 'usage');
  assert.equal(productStreamEdgeVisualKind('assembles_into'), 'assembly');
  assert.equal(productStreamEdgeVisualKind('transforms_into'), 'transformation');
  assert.equal(productStreamEdgeVisualKind(''), 'transformation');
  assert.equal(productStreamEdgeVisualKind(undefined), 'transformation');
});
