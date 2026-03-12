import test from 'node:test';
import assert from 'node:assert/strict';
import type { Edge, Node } from 'reactflow';
import { computeHoverHighlight } from './hover';
import { layoutInfluenceNetwork, layoutSupplyFlow, layoutValueChain } from './layouts';
import { getGraphViewExpansionPolicy, getGraphViewFilterPolicy, getGraphViewOverlayPolicy, getGraphViewToolbarPolicy } from './runtime';
import { graphViewToSemantic, normalizeGraphView } from './types';

function makeNode(id: string, data: Record<string, any>): Node<any> {
  return {
    id,
    position: { x: 0, y: 0 },
    data,
    type: 'default',
  };
}

function makeEdge(id: string, source: string, target: string, edgeType: string, extra?: Record<string, any>): Edge {
  return {
    id,
    source,
    target,
    data: {
      edgeType,
      ...extra,
    },
  };
}

test('normalizeGraphView keeps new and legacy ids stable', () => {
  assert.equal(normalizeGraphView('supply-flow'), 'supply-flow');
  assert.equal(normalizeGraphView('levels'), 'levels');
  assert.equal(normalizeGraphView('unknown'), 'hierarchical');
  assert.equal(graphViewToSemantic('supply-chain'), 'supply-flow');
  assert.equal(getGraphViewOverlayPolicy('supply-flow').showSupplyChainOverlay, true);
  assert.equal(getGraphViewOverlayPolicy('hierarchical').showExpansionModeMenu, true);
  assert.equal(getGraphViewExpansionPolicy('influence-network').edgeTypes.includes('OWNS'), true);
  assert.deepEqual(
    getGraphViewToolbarPolicy('value-chain').highlightModes.map((mode) => mode.key),
    ['off', 'production', 'supplies', 'competition', 'signal-propagation'],
  );
  assert.deepEqual(getGraphViewToolbarPolicy('influence-network').relationFilterOptions, []);
  assert.deepEqual(getGraphViewToolbarPolicy('hierarchical').relationFilterOptions, ['supply-chain', 'customers', 'market', 'context']);
});

test('supply flow layout classifies stages, chokepoints and edge classes', async () => {
  const nodes = [
    makeNode('ore', { canonicalPath: '/deposit/om/copper', label: 'Copper ore', type: 'PRODUCT' }),
    makeNode('plant', { canonicalPath: '/org/om/smelter', label: 'Smelter', type: 'ORGANIZATION' }),
    makeNode('port', { canonicalPath: '/loc/om/duqm-port', label: 'Duqm Port', type: 'FACILITY' }),
    makeNode('buyer', { canonicalPath: '/market/ru/aerospace', label: 'Russian buyer', type: 'MARKET' }),
  ];
  const edges = [
    makeEdge('e1', 'ore', 'plant', 'SUPPLIES_TO'),
    makeEdge('e2', 'plant', 'port', 'SUPPLIES_TO', { criticality: 0.7 }),
    makeEdge('e3', 'port', 'buyer', 'SUPPLIES_TO'),
  ];

  const arranged = await layoutSupplyFlow(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(arranged.edges.map((edge) => [edge.id, edge]));

  assert.equal((nodeById.get('ore')?.data as any)?.supplyFlowMeta?.stage, 'upstream');
  assert.equal((nodeById.get('port')?.data as any)?.supplyFlowMeta?.role, 'gateway');
  assert.equal((nodeById.get('buyer')?.data as any)?.supplyFlowMeta?.stage, 'destination');
  assert.equal((edgeById.get('e2')?.data as any)?.supplyFlowMeta?.flowClass, 'material-input');
  assert.equal((edgeById.get('e2')?.data as any)?.supplyFlowMeta?.variant, 'primary');
  assert.ok((nodeById.get('plant')?.position?.x || 0) > (nodeById.get('ore')?.position?.x || 0));
});

test('influence network layout clusters family and control links', async () => {
  const nodes = [
    makeNode('family', { canonicalPath: '/community/om/al-said/family', label: 'Al Said Family', type: 'COMMUNITY' }),
    makeNode('holdco', { canonicalPath: '/org/om/holding-company', label: 'Holding Co', type: 'ORGANIZATION' }),
    makeNode('foreign', { canonicalPath: '/org/gb/foreign-asset', label: 'Foreign Asset', type: 'ORGANIZATION' }),
    makeNode('gov', { canonicalPath: '/gov/gb/regulator', label: 'UK Regulator', type: 'GOV' }),
  ];
  const edges = [
    makeEdge('i1', 'family', 'holdco', 'OWNS'),
    makeEdge('i2', 'holdco', 'foreign', 'CONTROLS'),
    makeEdge('i3', 'gov', 'foreign', 'REGULATES'),
  ];

  const arranged = await layoutInfluenceNetwork(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(arranged.edges.map((edge) => [edge.id, edge]));

  assert.equal((nodeById.get('family')?.data as any)?.influenceMeta?.actorRole, 'family');
  assert.equal((nodeById.get('family')?.data as any)?.influenceMeta?.clusterKind, 'family');
  assert.equal((nodeById.get('foreign')?.data as any)?.influenceMeta?.countryAnchor, 'GB');
  assert.equal((edgeById.get('i1')?.data as any)?.influenceMeta?.influenceClass, 'ownership-control');
  assert.equal((edgeById.get('i3')?.data as any)?.influenceMeta?.influenceClass, 'regulatory-leverage');
});

test('value chain layout uses stage columns and disables semantic group filters', async () => {
  const nodes = [
    makeNode('raw', { canonicalPath: '/deposit/br/lithium', label: 'Lithium', type: 'PRODUCT' }),
    makeNode('component', { canonicalPath: '/product/cn/battery-cell', label: 'Battery cell component', type: 'PRODUCT' }),
    makeNode('launch', { canonicalPath: '/fac/ae/space-launch-center', label: 'Launch integration center', type: 'FACILITY' }),
    makeNode('market', { canonicalPath: '/market/gcc/space-services', label: 'Space services market', type: 'MARKET' }),
  ];
  const edges = [
    makeEdge('v1', 'raw', 'component', 'SUPPLIES_TO'),
    makeEdge('v2', 'component', 'launch', 'TRANSFORMS_INTO'),
    makeEdge('v3', 'launch', 'market', 'SUPPLIES_TO'),
  ];

  const arranged = await layoutValueChain(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const policy = getGraphViewFilterPolicy('value-chain');

  assert.equal((nodeById.get('raw')?.data as any)?.valueChainMeta?.stage, 'inputs');
  assert.equal((nodeById.get('component')?.data as any)?.valueChainMeta?.stage, 'components');
  assert.equal((nodeById.get('launch')?.data as any)?.valueChainMeta?.role, 'facility');
  assert.equal((nodeById.get('market')?.data as any)?.valueChainMeta?.stage, 'markets');
  assert.ok((nodeById.get('market')?.position?.x || 0) > (nodeById.get('launch')?.position?.x || 0));
  assert.equal(policy.semanticGroupFilter, false);
  assert.equal(policy.semanticGroupColors, false);
});

test('hover highlight computes control cluster and decision chain outside cockpit', () => {
  const nodes = [
    makeNode('family', { type: 'COMMUNITY' }),
    makeNode('holdco', { type: 'ORGANIZATION' }),
    makeNode('asset', { type: 'ORGANIZATION' }),
    makeNode('person', { type: 'PERSON' }),
  ].map((node, index) => ({
    ...node,
    position: { x: 500 - index * 120, y: 80 + index * 40 },
  }));
  const edges = [
    makeEdge('c1', 'family', 'holdco', 'OWNS'),
    makeEdge('c2', 'holdco', 'asset', 'CONTROLS'),
    makeEdge('c3', 'person', 'holdco', 'BOARD_MEMBER_OF'),
    makeEdge('dc1', 'family', 'holdco', 'CORRIDOR_CONTROL', { corridorEdge: true }),
    makeEdge('dc2', 'holdco', 'asset', 'CORRIDOR_CONTROL', { corridorEdge: true }),
  ];

  const control = computeHoverHighlight({
    nodes,
    edges,
    hoveredNodeId: 'holdco',
    hoverMode: 'control',
    isHierarchicalView: true,
    perfMode: false,
  });
  assert.ok(control);
  assert.equal(control?.nodeIds.has('family'), true);
  assert.equal(control?.nodeIds.has('asset'), true);
  assert.equal(control?.edgeIds.has('c2'), true);

  const decisionChain = computeHoverHighlight({
    nodes,
    edges,
    hoveredNodeId: 'holdco',
    hoverMode: 'decision-chain',
    isHierarchicalView: true,
    perfMode: false,
  });
  assert.ok(decisionChain);
  assert.equal(decisionChain?.nodeIds.has('family'), true);
  assert.equal(decisionChain?.nodeIds.has('asset'), true);
  assert.equal(decisionChain?.edgeIds.has('dc1'), true);

  const supplies = computeHoverHighlight({
    nodes,
    edges: [makeEdge('s1', 'family', 'holdco', 'SUPPLIES_TO')],
    hoveredNodeId: 'holdco',
    hoverMode: 'supplies',
    isHierarchicalView: false,
    perfMode: false,
  });
  assert.ok(supplies);
  assert.equal(supplies?.nodeIds.has('family'), true);
});
