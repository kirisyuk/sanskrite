import test from 'node:test';
import assert from 'node:assert/strict';

import { FRAGMENT_CLASS, RELATION } from './taxonomy.generated';
import { pathPayloadEdgeRelation, pathPayloadNodeFragmentClass } from './materializedPayload';

test('pathPayloadEdgeRelation prefers relation (AH snake_case)', () => {
  assert.equal(pathPayloadEdgeRelation({ relation: 'supplies', from: 0, to: 1 }), RELATION.Supplies);
  assert.equal(pathPayloadEdgeRelation({ relation: 'produces', from: 0, to: 1 }), RELATION.Produces);
});

test('pathPayloadEdgeRelation maps legacy supply_link and logistics', () => {
  assert.equal(pathPayloadEdgeRelation({ type: 'SUPPLY_LINK', from: 0, to: 1 }), RELATION.Supplies);
  assert.equal(pathPayloadEdgeRelation({ type: 'LOGISTICS', role: 'x', from: 0, to: 1 }), RELATION.Wields);
  assert.equal(pathPayloadEdgeRelation({ role: 'logistics_leg', from: 0, to: 1 }), RELATION.Wields);
});

test('pathPayloadNodeFragmentClass reads class or legacy type', () => {
  assert.equal(pathPayloadNodeFragmentClass({ class: 'org', ref: '/org/a' }), FRAGMENT_CLASS.Org);
  assert.equal(pathPayloadNodeFragmentClass({ type: 'ORGANIZATION', ref: '/org/a' }), FRAGMENT_CLASS.Org);
  assert.equal(pathPayloadNodeFragmentClass({ class: 'site', subtype: 'factory', ref: '/fac/x' }), FRAGMENT_CLASS.Site);
});
