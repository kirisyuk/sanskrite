import test from 'node:test';
import assert from 'node:assert/strict';
import type { Edge, Node } from 'reactflow';
import { RELATION } from '../taxonomy.generated';
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
  assert.equal(getGraphViewExpansionPolicy('influence-network').edgeTypes.includes(RELATION.BeneficiallyOwns), true);
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
  assert.equal(classifyInfluenceRelation(RELATION.Influences), 'influence-general');
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
    makeEdge('e1', 'ore', 'plant', RELATION.Supplies),
    makeEdge('e2', 'plant', 'port', RELATION.Supplies, { criticality: 0.7 }),
    makeEdge('e3', 'port', 'buyer', RELATION.Supplies, { criticality: 0.9 }),
    makeEdge('e4', 'customs', 'port', RELATION.Regulates),
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

test('influence network layout builds an ownership spine with attached governance and market rails', async () => {
  const nodes = [
    makeNode('dynasty', { canonicalPath: '/community/om/al-said/dynasty', label: 'Al Said Dynasty', type: 'COMMUNITY' }),
    makeNode('family', { canonicalPath: '/community/om/al-said/family', label: 'Al Said Family', type: 'COMMUNITY' }),
    makeNode('holdco', { canonicalPath: '/org/om/holding-company', label: 'Holding Co', type: 'ORGANIZATION' }),
    makeNode('opco', { canonicalPath: '/org/gb/operating-company', label: 'Operating Co', type: 'ORGANIZATION' }),
    makeNode('asset', { canonicalPath: '/fac/gb/strategic-terminal', label: 'Strategic Terminal', type: 'FACILITY' }),
    makeNode('executive', { canonicalPath: '/person/om/chairman', label: 'Chairman', type: 'PERSON' }),
    makeNode('gov', { canonicalPath: '/gov/gb/regulator', label: 'UK Regulator', type: 'GOV' }),
    makeNode('bloc', { canonicalPath: '/market/gcc/space-services', label: 'GCC space services', type: 'MARKET' }),
    makeNode('country', { canonicalPath: '/loc/gb', label: 'United Kingdom', type: 'COUNTRY' }),
  ];
  const edges = [
    makeEdge('i0', 'dynasty', 'family', RELATION.MemberOf),
    makeEdge('i1', 'family', 'holdco', RELATION.BeneficiallyOwns),
    makeEdge('i2', 'holdco', 'opco', RELATION.ControlsVia),
    makeEdge('i3', 'opco', 'asset', RELATION.BeneficiallyOwns),
    makeEdge('i4', 'executive', 'opco', RELATION.HoldsBoardSeat),
    makeEdge('i5', 'gov', 'opco', RELATION.Regulates),
    makeEdge('i6', 'opco', 'bloc', RELATION.Influences),
    makeEdge('i7', 'country', 'opco', RELATION.Disrupts),
  ];

  const arranged = await layoutInfluenceNetwork(nodes, edges, {});
  const nodeById = new Map(arranged.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(arranged.edges.map((edge) => [edge.id, edge]));

  const familyX = nodeById.get('family')?.position?.x || 0;
  const holdcoX = nodeById.get('holdco')?.position?.x || 0;
  const opcoX = nodeById.get('opco')?.position?.x || 0;
  const assetX = nodeById.get('asset')?.position?.x || 0;
  const executiveX = nodeById.get('executive')?.position?.x || 0;
  const govX = nodeById.get('gov')?.position?.x || 0;
  const blocX = nodeById.get('bloc')?.position?.x || 0;
  const countryX = nodeById.get('country')?.position?.x || 0;

  assert.ok((nodeById.get('dynasty')?.position?.x || 0) <= familyX);
  assert.ok(familyX < holdcoX);
  assert.ok(holdcoX < opcoX);
  assert.ok(opcoX < assetX);
  assert.ok(Math.abs(executiveX - opcoX) < 180);
  assert.ok(govX > opcoX);
  assert.ok(blocX > opcoX);
  assert.ok(countryX >= blocX || countryX > opcoX);
  assert.equal((nodeById.get('family')?.data as any)?.influenceMeta?.actorRole, 'family');
  assert.equal((nodeById.get('dynasty')?.data as any)?.influenceMeta?.actorRole, 'dynasty');
  assert.equal((nodeById.get('gov')?.data as any)?.influenceMeta?.actorRole, 'regulator');
  assert.equal((nodeById.get('opco')?.data as any)?.influenceMeta?.geoAnchor?.id, 'GB');
  assert.equal((nodeById.get('bloc')?.data as any)?.influenceMeta?.geoAnchor?.label, 'GCC');
  assert.equal((nodeById.get('family')?.data as any)?.influenceMeta?.isRootOwner, true);
  assert.equal((nodeById.get('holdco')?.data as any)?.influenceMeta?.controlDepth, 1);
  assert.equal((nodeById.get('opco')?.data as any)?.influenceMeta?.controlDepth, 2);
  assert.equal((nodeById.get('executive')?.data as any)?.influenceMeta?.rail, 'people');
  assert.equal((nodeById.get('gov')?.data as any)?.influenceMeta?.rail, 'governance');
  assert.equal((edgeById.get('i4')?.data as any)?.routingHint?.preferredAxis, 'vertical');
  assert.equal((edgeById.get('i5')?.data as any)?.influenceMeta?.influenceClass, 'regulatory-leverage');
  assert.equal((edgeById.get('i6')?.data as any)?.influenceMeta?.influenceClass, 'influence-general');
});

test('value chain layout keeps regulators in support rail and economic stages left-to-right', async () => {
  const regulatorMeta = classifyValueChainNode(
    makeNode('gov', { canonicalPath: '/gov/ae/space-regulator', label: 'Space regulator', type: 'GOV' }),
    [RELATION.Regulates],
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
    makeEdge('v1', 'raw', 'component', RELATION.Supplies),
    makeEdge('v2', 'component', 'launch', RELATION.Produces),
    makeEdge('v3', 'launch', 'ops', RELATION.Supplies),
    makeEdge('v4', 'ops', 'market', RELATION.MemberOf),
    makeEdge('v5', 'gov', 'ops', RELATION.Regulates),
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

test('specialized handle policies prefer vertical routing for same-cluster and horizontal routing for ownership spines', () => {
  const nodes = [
    makeNode('a', { graphView: 'influence-network', influenceMeta: { clusterId: 'family:OM', clusterKind: 'family', actorRole: 'family' } }, { x: 0, y: 0 }),
    makeNode('b', { graphView: 'influence-network', influenceMeta: { clusterId: 'family:OM', clusterKind: 'family', actorRole: 'dynasty' } }, { x: 10, y: 140 }),
    makeNode('e', { graphView: 'influence-network', influenceMeta: { clusterId: 'spine:OM', clusterKind: 'organization', actorRole: 'organization' } }, { x: 0, y: 220 }),
    makeNode('f', { graphView: 'influence-network', influenceMeta: { clusterId: 'spine:OM', clusterKind: 'organization', actorRole: 'organization' } }, { x: 240, y: 240 }),
    makeNode('c', { graphView: 'value-chain', valueChainMeta: { stage: 'support', role: 'regulator' } }, { x: 100, y: 0 }),
    makeNode('d', { graphView: 'value-chain', valueChainMeta: { stage: 'operators', role: 'operator' } }, { x: 110, y: 140 }),
  ];
  const influenceEdges = recalculateInfluenceHandles([
    makeEdge('e1', 'a', 'b', RELATION.JointVentureWith, { routingHint: { sameCluster: true } }),
    makeEdge('e4', 'e', 'f', RELATION.BeneficiallyOwns, { routingHint: { preferredAxis: 'horizontal' } }),
  ], nodes as any);
  const valueEdges = recalculateValueChainHandles([
    makeEdge('e2', 'c', 'd', RELATION.Regulates, { routingHint: { preferredAxis: 'vertical' }, valueChainMeta: { valueClass: 'governance-context' } }),
  ], nodes as any);
  const flowEdges = recalculateSupplyFlowHandles([
    makeEdge('e3', 'a', 'd', RELATION.Supplies, { routingHint: { preferredAxis: 'horizontal' }, supplyFlowMeta: { flowClass: 'material-input' } }),
  ], nodes as any);

  assert.equal(influenceEdges[0]?.sourceHandle, 'source-bottom');
  assert.equal(influenceEdges[1]?.sourceHandle, 'source-right');
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
