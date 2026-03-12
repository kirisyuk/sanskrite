import type { Edge, Node } from 'reactflow';
import type {
  InfluenceActorRole,
  InfluenceViewMeta,
  SharedGraphEdgeMeta,
  SupplyFlowRole,
  SupplyFlowStage,
  SupplyFlowViewMeta,
  ValueChainRole,
  ValueChainStage,
  ValueChainViewMeta,
} from './types';

type PositionableNode<T = any> = Node<T>;

const normalizeEdgeType = (raw: unknown): string =>
  String(raw || '').trim().toUpperCase() || 'OTHER';

const edgeTypeOf = (edge: Edge): string =>
  normalizeEdgeType((edge as any)?.data?.edgeType || (edge as any)?.data?.subkind || (edge as any)?.label || (edge as any)?.type);

const canonicalPathOf = (node: PositionableNode): string =>
  String((node.data as any)?.canonicalPath || (node.data as any)?.fragmentId || node.id || '').trim().toLowerCase();

const labelOf = (node: PositionableNode): string =>
  String((node.data as any)?.label || node.id || '').trim().toLowerCase();

const typeOf = (node: PositionableNode): string =>
  String((node.data as any)?.type || '').trim().toUpperCase();

const degreeMapFor = (edges: Edge[]): Map<string, number> => {
  const out = new Map<string, number>();
  for (const e of edges) {
    const s = String(e.source || '').trim();
    const t = String(e.target || '').trim();
    if (!s || !t) continue;
    out.set(s, (out.get(s) || 0) + 1);
    out.set(t, (out.get(t) || 0) + 1);
  }
  return out;
};

const outgoingById = (edges: Edge[]): Map<string, Edge[]> => {
  const out = new Map<string, Edge[]>();
  for (const e of edges) {
    const s = String(e.source || '').trim();
    if (!s) continue;
    const list = out.get(s) || [];
    list.push(e);
    out.set(s, list);
  }
  return out;
};

const incomingById = (edges: Edge[]): Map<string, Edge[]> => {
  const out = new Map<string, Edge[]>();
  for (const e of edges) {
    const t = String(e.target || '').trim();
    if (!t) continue;
    const list = out.get(t) || [];
    list.push(e);
    out.set(t, list);
  }
  return out;
};

function extractCountryAnchor(path: string): string | undefined {
  const parts = String(path || '').trim().toLowerCase().split('/').filter(Boolean);
  if (parts.length < 2) return undefined;
  const namespace = parts[0];
  if (['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community', 'zone'].includes(namespace)) {
    const candidate = parts[1] || '';
    if (/^[a-z]{2}$/.test(candidate)) return candidate.toUpperCase();
  }
  return undefined;
}

function classifySupplyFlowNode(
  node: PositionableNode,
  inEdges: Edge[],
  outEdges: Edge[],
  degree: number,
): SupplyFlowViewMeta {
  const cp = canonicalPathOf(node);
  const label = labelOf(node);
  const type = typeOf(node);
  const edgeTypes = new Set([...inEdges, ...outEdges].map(edgeTypeOf));
  const suppliesOut = outEdges.some((e) => edgeTypeOf(e) === 'SUPPLIES_TO');
  const suppliesIn = inEdges.some((e) => edgeTypeOf(e) === 'SUPPLIES_TO');
  const transforms = edgeTypes.has('TRANSFORMS_INTO');
  const extracts = edgeTypes.has('EXTRACTS_FROM');
  const jurisdictionLike =
    type === 'GOV' ||
    type === 'GOVERNMENT' ||
    type === 'GOVERNMENT_AGENCY' ||
    edgeTypes.has('REGULATES') ||
    cp.startsWith('/gov/');
  const transitLike =
    cp.startsWith('/loc/') ||
    cp.startsWith('/region/') ||
    /port|canal|strait|corridor|terminal|hub/.test(label);
  const rawInputLike =
    cp.startsWith('/deposit/') ||
    /material|commodity|feedstock|ore|mineral|lng|gas|oil|spare|part/.test(cp) ||
    extracts;
  const destinationLike =
    cp.startsWith('/market/') ||
    /customer|buyer|operator|utility|airline|carrier|consumer/.test(label);

  let stage: SupplyFlowStage = 'transformation';
  let role: SupplyFlowRole = 'transformer';
  let lane = 'core';

  if (jurisdictionLike) {
    stage = 'jurisdiction';
    role = 'regulator';
    lane = 'jurisdiction';
  } else if (transitLike) {
    stage = 'transit';
    role = /port|canal|strait|corridor|hub/.test(label) ? 'gateway' : 'context';
    lane = role === 'gateway' ? 'transit' : 'context';
  } else if (rawInputLike || (suppliesOut && !suppliesIn && !destinationLike)) {
    stage = 'upstream';
    role = 'source';
    lane = 'inputs';
  } else if (destinationLike || (suppliesIn && !suppliesOut)) {
    stage = 'destination';
    role = 'destination';
    lane = 'destination';
  } else if (transforms || (suppliesIn && suppliesOut)) {
    stage = 'transformation';
    role = 'transformer';
    lane = 'transformation';
  }

  return {
    stage,
    role,
    lane,
    isGateway: role === 'gateway',
    isChokepoint: (role === 'gateway' || stage === 'transit') && degree >= 3,
    routeGroup: stage,
    confidence: degree >= 4 ? 0.9 : degree >= 2 ? 0.7 : 0.55,
  };
}

function classifyInfluenceNode(node: PositionableNode, degree: number): InfluenceViewMeta {
  const cp = canonicalPathOf(node);
  const type = typeOf(node);
  const countryAnchor = extractCountryAnchor(cp);
  let actorRole: InfluenceActorRole = 'other';
  let clusterKind: InfluenceViewMeta['clusterKind'] = 'other';

  if (cp.startsWith('/community/') || type === 'COMMUNITY') {
    actorRole = /family|dynasty/.test(cp) ? 'family' : 'community';
    clusterKind = 'family';
  } else if (type === 'GOV' || cp.startsWith('/gov/')) {
    actorRole = 'government';
    clusterKind = 'institution';
  } else if (type === 'PERSON') {
    actorRole = 'person';
    clusterKind = 'institution';
  } else if (cp.startsWith('/org/') || type === 'ORGANIZATION') {
    actorRole = 'organization';
    clusterKind = 'organization';
  } else if (cp.startsWith('/market/') || type === 'MARKET') {
    actorRole = 'market';
    clusterKind = 'market';
  } else if (cp.startsWith('/loc/') || cp.startsWith('/region/') || type.includes('GEO') || type === 'COUNTRY') {
    actorRole = 'country';
    clusterKind = 'geography';
  } else if (cp.startsWith('/program/') || type === 'PROGRAM') {
    actorRole = 'intermediary';
    clusterKind = 'institution';
  }

  const centralityBucket = degree >= 5 ? 'high' : degree >= 3 ? 'medium' : 'low';
  const clusterBase =
    clusterKind === 'family'
      ? (cp.split('/').slice(0, 4).join('/') || 'family')
      : clusterKind === 'geography'
        ? countryAnchor || 'global'
        : countryAnchor
          ? `${clusterKind}:${countryAnchor}`
          : `${clusterKind}:${actorRole}`;

  return {
    clusterId: clusterBase,
    clusterKind,
    actorRole,
    countryAnchor,
    centralityBucket,
  };
}

function classifyValueChainNode(node: PositionableNode): ValueChainViewMeta {
  const cp = canonicalPathOf(node);
  const type = typeOf(node);
  const label = labelOf(node);

  let stage: ValueChainStage = 'integration';
  let role: ValueChainRole = 'company';

  if (cp.startsWith('/deposit/') || /material|commodity|feedstock|ore|mineral|lithium|lng|gas|oil/.test(cp)) {
    stage = 'inputs';
    role = 'product';
  } else if (/component|chip|module|sensor|battery-cell|engine|processor/.test(cp)) {
    stage = 'components';
    role = 'product';
  } else if (/subsystem|payload|bus|launch-vehicle|satellite-platform|subassembly/.test(cp) || type === 'TECHNOLOGY') {
    stage = 'subsystems';
    role = type === 'TECHNOLOGY' ? 'technology' : 'product';
  } else if (cp.startsWith('/fac/') || /plant|assembly|integration|fab|yard|launch/.test(label)) {
    stage = 'integration';
    role = 'facility';
  } else if (cp.startsWith('/market/') || type === 'MARKET') {
    stage = 'markets';
    role = 'market';
  } else if (cp.startsWith('/gov/') || type === 'GOV') {
    stage = 'markets';
    role = 'regulator';
  } else if (/operator|service|carrier|platform|provider/.test(label)) {
    stage = 'operators';
    role = 'company';
  }

  return {
    stage,
    role,
    swimlane: role,
    valuePoolBand:
      stage === 'markets' || stage === 'operators'
        ? 'high'
        : stage === 'integration' || stage === 'subsystems'
          ? 'medium'
          : 'low',
    bottleneck: stage === 'inputs' || stage === 'components',
  };
}

function columnarLayout<T>(
  nodes: Node<T>[],
  assignments: Map<string, { column: number; lane: number; cluster?: string; order: string }>,
  options: { nodeWidth?: number; nodeHeight?: number; columnGap?: number; rowGap?: number },
): Node<T>[] {
  const nodeWidth = options.nodeWidth ?? 180;
  const nodeHeight = options.nodeHeight ?? 161;
  const columnGap = options.columnGap ?? Math.max(nodeWidth + 120, 320);
  const rowGap = options.rowGap ?? Math.max(Math.floor(nodeHeight * 0.9), 160);

  const buckets = new Map<string, string[]>();
  for (const node of nodes) {
    const meta = assignments.get(String(node.id || '').trim());
    if (!meta) continue;
    const key = `${meta.column}:${meta.lane}:${meta.cluster || 'default'}`;
    const list = buckets.get(key) || [];
    list.push(node.id);
    buckets.set(key, list);
  }

  for (const ids of buckets.values()) {
    ids.sort((a, b) => {
      const ma = assignments.get(a);
      const mb = assignments.get(b);
      return String(ma?.order || a).localeCompare(String(mb?.order || b));
    });
  }

  return nodes.map((node) => {
    const id = String(node.id || '').trim();
    const meta = assignments.get(id);
    if (!meta) return node;
    const key = `${meta.column}:${meta.lane}:${meta.cluster || 'default'}`;
    const ids = buckets.get(key) || [id];
    const index = Math.max(0, ids.indexOf(id));
    const x = meta.column * columnGap;
    const y = meta.lane * rowGap + index * (nodeHeight + 24);
    return {
      ...node,
      position: { x, y },
    };
  });
}

function decorateEdgeMeta(edge: Edge, meta: SharedGraphEdgeMeta): Edge {
  return {
    ...edge,
    data: {
      ...((edge as any).data || {}),
      ...meta,
    },
  };
}

export async function layoutSupplyFlow<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const degrees = degreeMapFor(edges);
  const incoming = incomingById(edges);
  const outgoing = outgoingById(edges);
  const assignments = new Map<string, { column: number; lane: number; cluster?: string; order: string }>();
  const flowStageOrder: Record<SupplyFlowStage, number> = {
    upstream: 0,
    transformation: 1,
    transit: 2,
    destination: 3,
    jurisdiction: 2,
  };
  const laneOrder: Record<string, number> = {
    jurisdiction: 0,
    inputs: 1,
    transformation: 2,
    transit: 3,
    destination: 4,
    context: 5,
    core: 2,
  };

  const laidOutNodes = columnarLayout(
    nodes.map((node) => {
      const meta = classifySupplyFlowNode(
        node,
        incoming.get(String(node.id || '').trim()) || [],
        outgoing.get(String(node.id || '').trim()) || [],
        degrees.get(String(node.id || '').trim()) || 0,
      );
      assignments.set(String(node.id || '').trim(), {
        column: flowStageOrder[meta.stage],
        lane: laneOrder[meta.lane] ?? 3,
        cluster: meta.stage,
        order: `${meta.role}:${labelOf(node)}`,
      });
      return {
        ...node,
        data: {
          ...(node.data as any),
          graphView: 'supply-flow',
          supplyFlowMeta: meta,
        },
      };
    }),
    assignments,
    options,
  );

  const decoratedEdges = edges.map((edge) => {
    const et = edgeTypeOf(edge);
    const meta: SharedGraphEdgeMeta['supplyFlowMeta'] = {
      flowClass:
        et === 'TRANSFORMS_INTO'
          ? 'product-transformation'
          : et === 'SUPPLIES_TO' || et === 'PRODUCES' || et === 'EXTRACTS_FROM'
            ? 'material-input'
            : et === 'LOCATED_IN' || et === 'OPERATES_IN' || et === 'BASED_IN' || et === 'HEADQUARTERED_IN'
              ? 'jurisdiction'
              : et === 'REGULATES' || et === 'ADMINISTERS'
                ? 'jurisdiction'
                : et === 'OWNS' || et === 'CONTROLS' || et === 'MANAGES'
                  ? 'corporate-context'
                  : 'logistics-leg',
      variant:
        Number((edge as any)?.data?.criticality || 0) >= 0.85
          ? 'blocked'
          : Number((edge as any)?.data?.criticality || 0) >= 0.55
            ? 'primary'
            : 'uncertain',
      confidence: Number((edge as any)?.data?.criticality || 0) || undefined,
    };
    return decorateEdgeMeta(edge, { graphView: 'supply-flow', supplyFlowMeta: meta });
  });

  return { nodes: laidOutNodes, edges: decoratedEdges };
}

export async function layoutInfluenceNetwork<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const degrees = degreeMapFor(edges);
  const assignments = new Map<string, { column: number; lane: number; cluster?: string; order: string }>();
  const clusterColumn: Record<InfluenceViewMeta['clusterKind'], number> = {
    family: 0,
    organization: 1,
    institution: 2,
    geography: 3,
    market: 4,
    other: 5,
  };
  const actorLane: Record<InfluenceActorRole, number> = {
    family: 0,
    community: 1,
    organization: 2,
    government: 1,
    person: 2,
    country: 0,
    market: 3,
    intermediary: 2,
    other: 4,
  };

  const laidOutNodes = columnarLayout(
    nodes.map((node) => {
      const meta = classifyInfluenceNode(node, degrees.get(String(node.id || '').trim()) || 0);
      assignments.set(String(node.id || '').trim(), {
        column: clusterColumn[meta.clusterKind],
        lane: actorLane[meta.actorRole],
        cluster: meta.clusterId,
        order: `${meta.centralityBucket}:${labelOf(node)}`,
      });
      return {
        ...node,
        data: {
          ...(node.data as any),
          graphView: 'influence-network',
          influenceMeta: meta,
        },
      };
    }),
    assignments,
    options,
  );

  const decoratedEdges = edges.map((edge) => {
    const et = edgeTypeOf(edge);
    const influenceClass: NonNullable<SharedGraphEdgeMeta['influenceMeta']>['influenceClass'] =
      et === 'OWNS' || et === 'CONTROLS' || et === 'MANAGES'
        ? 'ownership-control'
        : et === 'BOARD_MEMBER_OF' || et === 'EXECUTIVE_OF' || et === 'EMPLOYED_BY'
          ? 'board-executive'
          : et === 'REGULATES' || et === 'ADMINISTERS'
            ? 'regulatory-leverage'
            : et === 'INFLUENCES' || et === 'AFFECTS' || et === 'IMPACTS'
              ? 'political-access'
              : et === 'SUPPLIES_TO' || et === 'DEPENDS_ON' || et === 'REQUIRES'
                ? 'commercial-dependence'
                : et === 'MEMBER_OF' || et === 'AFFILIATED_WITH' || et === 'HAS_SUBGROUP' || et === 'RELATIVE_OF'
                  ? 'family-community'
                  : 'other';
    return decorateEdgeMeta(edge, {
      graphView: 'influence-network',
      influenceMeta: { influenceClass },
    });
  });

  return { nodes: laidOutNodes, edges: decoratedEdges };
}

export async function layoutValueChain<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: { nodeWidth?: number; nodeHeight?: number },
): Promise<{ nodes: Node<T>[]; edges: Edge[] }> {
  const assignments = new Map<string, { column: number; lane: number; cluster?: string; order: string }>();
  const stageColumn: Record<ValueChainStage, number> = {
    inputs: 0,
    components: 1,
    subsystems: 2,
    integration: 3,
    operators: 4,
    markets: 5,
  };
  const roleLane: Record<ValueChainRole, number> = {
    regulator: 0,
    country: 1,
    facility: 2,
    technology: 3,
    product: 4,
    company: 5,
    market: 6,
  };

  const laidOutNodes = columnarLayout(
    nodes.map((node) => {
      const meta = classifyValueChainNode(node);
      assignments.set(String(node.id || '').trim(), {
        column: stageColumn[meta.stage],
        lane: roleLane[meta.role],
        cluster: meta.stage,
        order: `${meta.role}:${labelOf(node)}`,
      });
      return {
        ...node,
        data: {
          ...(node.data as any),
          graphView: 'value-chain',
          valueChainMeta: meta,
        },
      };
    }),
    assignments,
    options,
  );

  const decoratedEdges = edges.map((edge) => {
    const et = edgeTypeOf(edge);
    const valueClass: NonNullable<SharedGraphEdgeMeta['valueChainMeta']>['valueClass'] =
      et === 'TRANSFORMS_INTO'
        ? 'transforms-into'
        : et === 'SUPPLIES_TO' || et === 'PRODUCES'
          ? 'supplies-input'
          : et === 'DEPENDS_ON' || et === 'REQUIRES' || et === 'USES'
            ? 'enables'
            : 'sells-into';
    return decorateEdgeMeta(edge, {
      graphView: 'value-chain',
      valueChainMeta: { valueClass },
    });
  });

  return { nodes: laidOutNodes, edges: decoratedEdges };
}
