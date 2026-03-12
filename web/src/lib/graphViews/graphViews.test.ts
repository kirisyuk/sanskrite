import test from 'node:test';
import assert from 'node:assert/strict';
import type { Edge, Node } from 'reactflow';
import { classifyGeoAnchor, classifyInfluenceActor, classifyInfluenceRelation, classifyValueChainNode } from './classifiers';
import { recalculateInfluenceHandles, recalculateSupplyFlowHandles, recalculateValueChainHandles } from './handles';
import { layoutInfluenceNetwork, layoutSupplyFlow, layoutValueChain } from './layouts';
import {
  arrangeGraphView,
  getGraphViewExpansionPolicy,
  getGraphViewFilterPolicy,
  getGraphViewOverlayPolicy,
  getGraphViewToolbarPolicy,
} from './runtime';
import { graphViewToSemantic, normalizeGraphView } from './types';

function makeNode(id: string, data: Record<string, any>, position = { x: 0, y: 0 }): Node<any> {
  return {
    id,
    position,
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

test('geo and actor classifiers separate bloc/country and family/community roles', () => {
  assert.deepEqual(classifyGeoAnchor('/market/gcc/space-services')?.label, 'GCC');
  assert.deepEqual(classifyGeoAnchor('/region/brics-plus/space')?.label, 'BRICS+');

  const family = classifyInfluenceActor(makeNode('family', {
    canonicalPath: '/community/om/al-said/family',
    label: 'Al Said Family',
    type: 'COMMUNITY',
  }));
  const tribe = classifyInfluenceActor(makeNode('tribe', {
    canonicalPath: '/community/om/bani-yas/tribe',
    label: 'Bani Yas Tribe',
    type: 'COMMUNITY',
  }));
  const regulator = classifyInfluenceActor(makeNode('gov', {
    canonicalPath: '/gov/gb/financial-conduct-authority',
    label: 'Financial Conduct Authority',
    type: 'GOV',
  }));

  assert.equal(family.actorRole, 'family');
  assert.equal(family.clusterKind, 'family');
  assert.equal(tribe.actorRole, 'tribe');
  assert.equal(tribe.clusterKind, 'community');
  assert.equal(regulator.actorRole, 'regulator');
  assert.equal(classifyInfluenceRelation('INFLUENCES'), 'influence-general');
});

test('supply flow layout classifies routes and keeps chokepoints pinned to the flow columns', async () => {
  const nodes = [
    makeNode('ore', { canonicalPath: '/deposit/om/copper', label: 'Copper ore', type: 'PRODUCT' }),
    makeNode('plant', { canonicalPath: '/org/om/smelter', label: 'Smelter', type: 'ORGANIZATION' }),
    makeNode('port', { canonicalPath: '/loc/om/duqm-port', label: 'Duqm Port', type: 'FACILITY' }),
    makeNode('buyer', { canonicalPath: '/market/ru/aerospace', label: 'Russian buyer', type: 'MARKET' }),
    makeNode('customs', { canonicalPath: '/gov/om/customs-authority', label: 'Customs Authority', type: 'GOV' }),
  ];
  const edges = [
    makeEdge('e1', 'ore', 'plant', 'SUPPLIES_TO'),
    makeEdge('e2', 'plant', 'port', 'SUPPLIES_TO', { criticality: 0.7 }),
    makeEdge('e3', 'port', 'buyer', 'SUPPLIES_TO', { criticality: 0.9 }),
    makeEdge('e4', 'customs', 'port', 'REGULATES'),
  ];

  const arranged = await layoutSupplyFlow(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(arranged.edges.map((edge) => [edge.id, edge]));

  assert.equal((nodeById.get('ore')?.data as any)?.supplyFlowMeta?.stage, 'upstream');
  assert.equal((nodeById.get('port')?.data as any)?.supplyFlowMeta?.role, 'gateway');
  assert.equal((nodeById.get('buyer')?.data as any)?.supplyFlowMeta?.stage, 'destination');
  assert.equal((nodeById.get('customs')?.data as any)?.supplyFlowMeta?.supportRole, 'governance');
  assert.equal((edgeById.get('e2')?.data as any)?.supplyFlowMeta?.flowClass, 'material-input');
  assert.equal((edgeById.get('e3')?.data as any)?.supplyFlowMeta?.variant, 'blocked');
  assert.ok((nodeById.get('plant')?.position?.x || 0) > (nodeById.get('ore')?.position?.x || 0));
});

test('influence network layout keeps geography and family clusters distinct', async () => {
  const nodes = [
    makeNode('family', { canonicalPath: '/community/om/al-said/family', label: 'Al Said Family', type: 'COMMUNITY' }),
    makeNode('tribe', { canonicalPath: '/community/ae/bani-yas/tribe', label: 'Bani Yas Tribe', type: 'COMMUNITY' }),
    makeNode('holdco', { canonicalPath: '/org/om/holding-company', label: 'Holding Co', type: 'ORGANIZATION' }),
    makeNode('foreign', { canonicalPath: '/org/gb/foreign-asset', label: 'Foreign Asset', type: 'ORGANIZATION' }),
    makeNode('gov', { canonicalPath: '/gov/gb/regulator', label: 'UK Regulator', type: 'GOV' }),
    makeNode('bloc', { canonicalPath: '/market/gcc/space-services', label: 'GCC space services', type: 'MARKET' }),
  ];
  const edges = [
    makeEdge('i1', 'family', 'holdco', 'OWNS'),
    makeEdge('i2', 'holdco', 'foreign', 'CONTROLS'),
    makeEdge('i3', 'gov', 'foreign', 'REGULATES'),
    makeEdge('i4', 'tribe', 'family', 'AFFILIATED_WITH'),
    makeEdge('i5', 'foreign', 'bloc', 'INFLUENCES'),
  ];

  const arranged = await layoutInfluenceNetwork(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(arranged.edges.map((edge) => [edge.id, edge]));

  assert.equal((nodeById.get('family')?.data as any)?.influenceMeta?.actorRole, 'family');
  assert.equal((nodeById.get('tribe')?.data as any)?.influenceMeta?.actorRole, 'tribe');
  assert.equal((nodeById.get('gov')?.data as any)?.influenceMeta?.actorRole, 'regulator');
  assert.equal((nodeById.get('foreign')?.data as any)?.influenceMeta?.geoAnchor?.id, 'GB');
  assert.equal((nodeById.get('bloc')?.data as any)?.influenceMeta?.geoAnchor?.label, 'GCC');
  assert.equal((edgeById.get('i3')?.data as any)?.influenceMeta?.influenceClass, 'regulatory-leverage');
  assert.equal((edgeById.get('i5')?.data as any)?.influenceMeta?.influenceClass, 'influence-general');
});

test('value chain layout keeps regulators in support rail and economic stages left-to-right', async () => {
  const regulatorMeta = classifyValueChainNode(
    makeNode('gov', { canonicalPath: '/gov/ae/space-regulator', label: 'Space regulator', type: 'GOV' }),
    ['REGULATES'],
  );
  assert.equal(regulatorMeta.stage, 'support');
  assert.equal(regulatorMeta.role, 'regulator');

  const nodes = [
    makeNode('raw', { canonicalPath: '/deposit/br/lithium', label: 'Lithium', type: 'PRODUCT' }),
    makeNode('component', { canonicalPath: '/product/cn/battery-cell', label: 'Battery cell component', type: 'PRODUCT' }),
    makeNode('launch', { canonicalPath: '/fac/ae/space-launch-center', label: 'Launch integration center', type: 'FACILITY' }),
    makeNode('ops', { canonicalPath: '/org/ae/orbital-service-provider', label: 'Orbital service provider', type: 'ORGANIZATION' }),
    makeNode('market', { canonicalPath: '/market/gcc/space-services', label: 'Space services market', type: 'MARKET' }),
    makeNode('gov', { canonicalPath: '/gov/ae/space-regulator', label: 'Space regulator', type: 'GOV' }),
  ];
  const edges = [
    makeEdge('v1', 'raw', 'component', 'SUPPLIES_TO'),
    makeEdge('v2', 'component', 'launch', 'TRANSFORMS_INTO'),
    makeEdge('v3', 'launch', 'ops', 'SUPPLIES_TO'),
    makeEdge('v4', 'ops', 'market', 'PARTICIPATES_IN'),
    makeEdge('v5', 'gov', 'ops', 'REGULATES'),
  ];

  const arranged = await layoutValueChain(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const policy = getGraphViewFilterPolicy('value-chain');

  assert.equal((nodeById.get('raw')?.data as any)?.valueChainMeta?.stage, 'inputs');
  assert.equal((nodeById.get('component')?.data as any)?.valueChainMeta?.stage, 'components');
  assert.equal((nodeById.get('ops')?.data as any)?.valueChainMeta?.stage, 'operators');
  assert.equal((nodeById.get('gov')?.data as any)?.valueChainMeta?.stage, 'support');
  assert.ok(((nodeById.get('market')?.position?.x || 0) > (nodeById.get('launch')?.position?.x || 0)));
  assert.equal(policy.semanticGroupFilter, false);
  assert.equal(policy.semanticGroupColors, false);
});

test('specialized handle policies prefer vertical routing for same-cluster or support edges', () => {
  const nodes = [
    makeNode('a', { graphView: 'influence-network', influenceMeta: { clusterId: 'family:OM', clusterKind: 'family', actorRole: 'family' } }, { x: 0, y: 0 }),
    makeNode('b', { graphView: 'influence-network', influenceMeta: { clusterId: 'family:OM', clusterKind: 'family', actorRole: 'dynasty' } }, { x: 10, y: 140 }),
    makeNode('c', { graphView: 'value-chain', valueChainMeta: { stage: 'support', role: 'regulator' } }, { x: 100, y: 0 }),
    makeNode('d', { graphView: 'value-chain', valueChainMeta: { stage: 'operators', role: 'operator' } }, { x: 110, y: 140 }),
  ];
  const influenceEdges = recalculateInfluenceHandles([
    makeEdge('e1', 'a', 'b', 'AFFILIATED_WITH', { routingHint: { sameCluster: true } }),
  ], nodes as any);
  const valueEdges = recalculateValueChainHandles([
    makeEdge('e2', 'c', 'd', 'REGULATES', { routingHint: { preferredAxis: 'vertical' }, valueChainMeta: { valueClass: 'governance-context' } }),
  ], nodes as any);
  const flowEdges = recalculateSupplyFlowHandles([
    makeEdge('e3', 'a', 'd', 'SUPPLIES_TO', { routingHint: { preferredAxis: 'horizontal' }, supplyFlowMeta: { flowClass: 'material-input' } }),
  ], nodes as any);

  assert.equal(influenceEdges[0]?.sourceHandle, 'source-bottom');
  assert.equal(valueEdges[0]?.sourceHandle, 'source-bottom');
  assert.equal(flowEdges[0]?.sourceHandle, 'source-right');
});

test('arrangeGraphView keeps Hierarchical delegated to legacy levels layout and handles', async () => {
  let levelsArrangeCalls = 0;
  let levelsHandleCalls = 0;
  const nodes = [makeNode('root', { fragmentId: 'root', levelsPane: 'right' })];
  const edges: Edge[] = [];

  const arranged = await arrangeGraphView('hierarchical', nodes, edges, {
    nodeWidth: 180,
    nodeHeight: 161,
    nodeSpacing: 60,
    layerSpacing: 150,
    maxChildRows: 3,
    expandedNodeIds: new Set(),
    collapsedSubcategories: new Set(),
    expandedRoleGroups: new Set(),
    rightDockStateByNodeId: {},
    graphViewState: {},
    onToggleSubcategoryCollapse: () => {},
    onToggleRoleOverflow: () => {},
    onToggleRightDock: () => {},
    layoutFullGraph: async (input) => input,
    layoutLinearFullGraph: async (input) => input,
    layoutLevelsTwoPane: async (input) => {
      levelsArrangeCalls += 1;
      return input;
    },
    rebuildGroupContainers: (input) => input,
    recalculateFreeformHandles: (input) => input,
    recalculateLinearHandles: (input) => input,
    recalculateLevelsTwoPaneHandles: (input) => {
      levelsHandleCalls += 1;
      return input;
    },
  });

  assert.equal(levelsArrangeCalls, 1);
  assert.equal(levelsHandleCalls, 1);
  assert.equal((arranged.nodes[0]?.data as any)?.graphView, 'hierarchical');
});
