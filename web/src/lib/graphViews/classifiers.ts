import type { Edge, Node } from 'reactflow';
import type {
  GeoAnchorMeta,
  InfluenceActorRole,
  InfluenceClass,
  InfluenceClusterKind,
  SupplyFlowRole,
  SupplyFlowRouteFamily,
  SupplyFlowStage,
  SupplyFlowSupportRole,
  ValueChainRole,
  ValueChainStage,
  ValueChainValueClass,
} from './types';

type PositionableNode<T = any> = Node<T>;

const BLOC_ALIASES: Record<string, string> = {
  gcc: 'GCC',
  brics: 'BRICS',
  'brics-plus': 'BRICS+',
  bricsplus: 'BRICS+',
  mena: 'MENA',
  eu: 'EU',
  asean: 'ASEAN',
  cis: 'CIS',
  eaeu: 'EAEU',
};

const REGION_ALIASES: Record<string, string> = {
  global: 'Global',
  worldwide: 'Global',
  europe: 'Europe',
  asia: 'Asia',
  mena: 'MENA',
  gulf: 'Gulf',
  gcc: 'GCC',
  levant: 'Levant',
  caucasus: 'Caucasus',
  africa: 'Africa',
  americas: 'Americas',
};

export const normalizeEdgeType = (raw: unknown): string =>
  String(raw || '').trim().toUpperCase() || 'OTHER';

export const edgeTypeOf = (edge: Edge): string =>
  normalizeEdgeType((edge as any)?.data?.edgeType || (edge as any)?.data?.subkind || (edge as any)?.label || (edge as any)?.type);

export const canonicalPathOf = (node: PositionableNode): string =>
  String((node.data as any)?.canonicalPath || (node.data as any)?.fragmentId || node.id || '').trim().toLowerCase();

export const labelOf = (node: PositionableNode): string =>
  String((node.data as any)?.label || node.id || '').trim().toLowerCase();

export const typeOf = (node: PositionableNode): string =>
  String((node.data as any)?.type || '').trim().toUpperCase();

export function classifyGeoAnchor(path: string, label = '', type = ''): GeoAnchorMeta | undefined {
  const parts = String(path || '').trim().toLowerCase().split('/').filter(Boolean);
  const text = `${String(path || '').toLowerCase()} ${String(label || '').toLowerCase()} ${String(type || '').toLowerCase()}`;
  if (parts.length >= 2) {
    const candidate = parts[1] || '';
    if (/^[a-z]{2}$/.test(candidate) && candidate !== 'na') {
      return { kind: 'country', id: candidate.toUpperCase(), label: candidate.toUpperCase() };
    }
    if (candidate.includes('-')) {
      const members = candidate.split('-').map((item) => item.trim().toUpperCase()).filter((item) => /^[A-Z]{2}$/.test(item));
      if (members.length >= 2) {
        return {
          kind: 'composite',
          id: candidate.toUpperCase(),
          label: members.join('-'),
          members,
        };
      }
    }
    if (BLOC_ALIASES[candidate]) {
      return { kind: 'bloc', id: BLOC_ALIASES[candidate], label: BLOC_ALIASES[candidate] };
    }
    if (REGION_ALIASES[candidate]) {
      return { kind: candidate === 'global' ? 'global' : 'region', id: candidate.toUpperCase(), label: REGION_ALIASES[candidate] };
    }
  }
  if (/\b(brics\+?|gcc|mena|eaeu|asean|cis)\b/.test(text)) {
    const match = text.match(/\b(brics\+?|gcc|mena|eaeu|asean|cis)\b/);
    if (match?.[1]) {
      const key = match[1].replace('+', '-plus');
      const labelValue = BLOC_ALIASES[key] || BLOC_ALIASES[match[1]] || match[1].toUpperCase();
      return { kind: 'bloc', id: labelValue, label: labelValue };
    }
  }
  if (/\b(global|worldwide)\b/.test(text)) {
    return { kind: 'global', id: 'GLOBAL', label: 'Global' };
  }
  return undefined;
}

export function classifyInfluenceActor(node: PositionableNode): {
  actorRole: InfluenceActorRole;
  clusterKind: InfluenceClusterKind;
  clusterIdSeed: string;
  clusterLabel: string;
  geoAnchor?: GeoAnchorMeta;
} {
  const cp = canonicalPathOf(node);
  const type = typeOf(node);
  const label = labelOf(node);
  const geoAnchor = classifyGeoAnchor(cp, label, type);

  if (cp.startsWith('/community/') || type === 'COMMUNITY') {
    if (/dynasty/.test(`${cp} ${label}`)) {
      return { actorRole: 'dynasty', clusterKind: 'family', clusterIdSeed: cp.split('/').slice(0, 4).join('/') || 'dynasty', clusterLabel: 'Dynasty', geoAnchor };
    }
    if (/tribe|tribal/.test(`${cp} ${label}`)) {
      return { actorRole: 'tribe', clusterKind: 'community', clusterIdSeed: cp.split('/').slice(0, 4).join('/') || 'tribe', clusterLabel: 'Tribe', geoAnchor };
    }
    if (/family/.test(`${cp} ${label}`)) {
      return { actorRole: 'family', clusterKind: 'family', clusterIdSeed: cp.split('/').slice(0, 4).join('/') || 'family', clusterLabel: 'Family', geoAnchor };
    }
    return { actorRole: 'community', clusterKind: 'community', clusterIdSeed: cp.split('/').slice(0, 4).join('/') || 'community', clusterLabel: 'Community', geoAnchor };
  }
  if (type === 'GOV' || cp.startsWith('/gov/')) {
    const actorRole: InfluenceActorRole = /regulat|authority|ministry|agency/.test(label) ? 'regulator' : 'government';
    return { actorRole, clusterKind: 'institution', clusterIdSeed: geoAnchor ? `institution:${geoAnchor.id}` : 'institution', clusterLabel: actorRole === 'regulator' ? 'Regulator' : 'Government', geoAnchor };
  }
  if (type === 'PERSON') {
    return { actorRole: 'person', clusterKind: 'institution', clusterIdSeed: geoAnchor ? `person:${geoAnchor.id}` : 'person', clusterLabel: 'Person', geoAnchor };
  }
  if (cp.startsWith('/org/') || type === 'ORGANIZATION') {
    return { actorRole: 'organization', clusterKind: 'organization', clusterIdSeed: geoAnchor ? `organization:${geoAnchor.id}` : 'organization', clusterLabel: 'Organization', geoAnchor };
  }
  if (cp.startsWith('/market/') || type === 'MARKET') {
    return { actorRole: 'market', clusterKind: 'market', clusterIdSeed: geoAnchor ? `market:${geoAnchor.id}` : 'market', clusterLabel: 'Market', geoAnchor };
  }
  if (cp.startsWith('/loc/') || cp.startsWith('/region/') || type.includes('GEO') || type === 'COUNTRY' || type === 'REGION') {
    const actorRole: InfluenceActorRole = geoAnchor?.kind === 'country' ? 'country' : 'region';
    return { actorRole, clusterKind: 'geography', clusterIdSeed: geoAnchor?.id || 'geography', clusterLabel: actorRole === 'country' ? 'Country' : 'Region', geoAnchor };
  }
  if (cp.startsWith('/program/') || type === 'PROGRAM' || type === 'FACILITY' || cp.startsWith('/fac/')) {
    return {
      actorRole: type === 'FACILITY' || cp.startsWith('/fac/') ? 'facility' : 'intermediary',
      clusterKind: type === 'FACILITY' || cp.startsWith('/fac/') ? 'organization' : 'institution',
      clusterIdSeed: geoAnchor ? `asset:${geoAnchor.id}` : 'asset',
      clusterLabel: type === 'FACILITY' || cp.startsWith('/fac/') ? 'Facility' : 'Intermediary',
      geoAnchor,
    };
  }
  return { actorRole: 'other', clusterKind: 'other', clusterIdSeed: geoAnchor ? `other:${geoAnchor.id}` : 'other', clusterLabel: 'Other', geoAnchor };
}

export function classifyInfluenceRelation(edgeType: string): InfluenceClass {
  const et = normalizeEdgeType(edgeType);
  if (et === 'OWNS' || et === 'CONTROLS' || et === 'MANAGES' || et === 'INVESTS_IN' || et === 'PARENT_OF' || et === 'SUBSIDIARY_OF') {
    return 'ownership-control';
  }
  if (et === 'BOARD_MEMBER_OF' || et === 'EXECUTIVE_OF' || et === 'EMPLOYED_BY' || et === 'FOUNDED') {
    return 'board-executive';
  }
  if (et === 'REGULATES' || et === 'ADMINISTERS' || et === 'CONTROLS_ACCESS' || et === 'IMPOSES_CONDITION' || et === 'GATEKEEPS_DECISION') {
    return et === 'CONTROLS_ACCESS' || et === 'GATEKEEPS_DECISION' ? 'political-access' : 'regulatory-leverage';
  }
  if (et === 'SUPPLIES_TO' || et === 'DEPENDS_ON' || et === 'REQUIRES' || et === 'USES' || et === 'NEEDS') {
    return 'commercial-dependence';
  }
  if (et === 'MEMBER_OF' || et === 'AFFILIATED_WITH' || et === 'HAS_SUBGROUP' || et === 'RELATIVE_OF') {
    return 'family-community';
  }
  if (et === 'INFLUENCES' || et === 'AFFECTS' || et === 'IMPACTS' || et === 'SUPPORTS' || et === 'PARTNERS_WITH') {
    return 'influence-general';
  }
  return 'other';
}

export function classifySupplyFlowNode(node: PositionableNode, incidentEdgeTypes: string[], degree: number): {
  stage: SupplyFlowStage;
  role: SupplyFlowRole;
  lane: string;
  geoAnchor?: GeoAnchorMeta;
  supportRole: SupplyFlowSupportRole;
  routeFamily: SupplyFlowRouteFamily;
  isGateway: boolean;
  isChokepoint: boolean;
  confidence: number;
} {
  const cp = canonicalPathOf(node);
  const label = labelOf(node);
  const type = typeOf(node);
  const geoAnchor = classifyGeoAnchor(cp, label, type);
  const edgeTypes = new Set(incidentEdgeTypes.map(normalizeEdgeType));

  const jurisdictionLike =
    type === 'GOV' ||
    type === 'GOVERNMENT' ||
    type === 'GOVERNMENT_AGENCY' ||
    edgeTypes.has('REGULATES') ||
    edgeTypes.has('ADMINISTERS') ||
    cp.startsWith('/gov/');
  const transitLike =
    cp.startsWith('/loc/') ||
    cp.startsWith('/region/') ||
    /port|canal|strait|corridor|terminal|hub|checkpoint|gateway/.test(label);
  const rawInputLike =
    cp.startsWith('/deposit/') ||
    cp.startsWith('/product/') ||
    /material|commodity|feedstock|ore|mineral|lng|gas|oil|spare|part|wafer|metal/.test(`${cp} ${label}`) ||
    edgeTypes.has('EXTRACTS_FROM');
  const destinationLike =
    cp.startsWith('/market/') ||
    /customer|buyer|operator|utility|airline|carrier|consumer|market/.test(label);
  const hasTransform = edgeTypes.has('TRANSFORMS_INTO') || edgeTypes.has('PRODUCES');

  let stage: SupplyFlowStage = 'transformation';
  let role: SupplyFlowRole = 'transformer';
  let lane = 'transformation';
  let supportRole: SupplyFlowSupportRole = 'none';

  if (transitLike) {
    stage = 'transit';
    role = /port|canal|strait|corridor|hub|gateway/.test(label) ? 'gateway' : 'transit';
    lane = 'transit';
  } else if (jurisdictionLike) {
    stage = 'jurisdiction';
    role = 'regulator';
    lane = 'jurisdiction';
    supportRole = 'governance';
  } else if (rawInputLike) {
    stage = 'upstream';
    role = 'source';
    lane = 'inputs';
  } else if (destinationLike) {
    stage = 'destination';
    role = 'destination';
    lane = 'destination';
  } else if (hasTransform) {
    stage = 'transformation';
    role = 'transformer';
    lane = 'transformation';
  } else if (edgeTypes.has('OWNS') || edgeTypes.has('CONTROLS') || edgeTypes.has('MANAGES')) {
    stage = 'jurisdiction';
    role = 'context';
    lane = 'support';
    supportRole = 'ownership';
  } else {
    stage = 'transit';
    role = 'context';
    lane = 'support';
    supportRole = 'support';
  }

  return {
    stage,
    role,
    lane,
    geoAnchor,
    supportRole,
    routeFamily: degree >= 4 ? 'primary' : degree >= 2 ? 'uncertain' : 'context',
    isGateway: role === 'gateway',
    isChokepoint: (role === 'gateway' || role === 'transit') && degree >= 3,
    confidence: degree >= 4 ? 0.9 : degree >= 2 ? 0.72 : 0.55,
  };
}

export function classifySupplyFlowRelation(edgeType: string, criticality?: number): {
  flowClass: 'material-input' | 'product-transformation' | 'logistics-leg' | 'jurisdiction' | 'corporate-context' | 'route-support';
  variant: SupplyFlowRouteFamily;
} {
  const et = normalizeEdgeType(edgeType);
  let flowClass: 'material-input' | 'product-transformation' | 'logistics-leg' | 'jurisdiction' | 'corporate-context' | 'route-support' = 'route-support';
  if (et === 'TRANSFORMS_INTO' || et === 'PRODUCES') flowClass = 'product-transformation';
  else if (et === 'SUPPLIES_TO' || et === 'EXTRACTS_FROM' || et === 'CONSUMES') flowClass = 'material-input';
  else if (et === 'LOCATED_IN' || et === 'OPERATES_IN' || et === 'BASED_IN' || et === 'HEADQUARTERED_IN' || et === 'REGULATES' || et === 'ADMINISTERS') flowClass = 'jurisdiction';
  else if (et === 'OWNS' || et === 'CONTROLS' || et === 'MANAGES' || et === 'INVESTS_IN') flowClass = 'corporate-context';
  else if (et === 'DEPENDS_ON' || et === 'REQUIRES' || et === 'USES' || et === 'NEEDS') flowClass = 'route-support';
  else flowClass = 'logistics-leg';

  const crit = typeof criticality === 'number' && Number.isFinite(criticality) ? criticality : 0;
  const variant: SupplyFlowRouteFamily =
    crit >= 0.85 ? 'blocked' :
    crit >= 0.65 ? 'primary' :
    crit >= 0.4 ? 'fallback' :
    'uncertain';

  return { flowClass, variant };
}

export function classifyValueChainNode(node: PositionableNode, incidentEdgeTypes: string[]): {
  stage: ValueChainStage;
  role: ValueChainRole;
  swimlane: string;
  geoAnchor?: GeoAnchorMeta;
  supportRail: boolean;
  valuePoolBand: 'low' | 'medium' | 'high';
  bottleneck: boolean;
} {
  const cp = canonicalPathOf(node);
  const type = typeOf(node);
  const label = labelOf(node);
  const geoAnchor = classifyGeoAnchor(cp, label, type);
  const edgeTypes = new Set(incidentEdgeTypes.map(normalizeEdgeType));

  let stage: ValueChainStage = 'support';
  let role: ValueChainRole = 'support';
  let supportRail = true;

  if (cp.startsWith('/deposit/') || /material|commodity|feedstock|ore|mineral|lithium|lng|gas|oil|rare earth|wafer/.test(`${cp} ${label}`)) {
    stage = 'inputs';
    role = 'product';
    supportRail = false;
  } else if (/component|chip|module|sensor|battery-cell|engine|processor|avionics|board/.test(`${cp} ${label}`)) {
    stage = 'components';
    role = 'product';
    supportRail = false;
  } else if (/subsystem|payload|bus|launch-vehicle|satellite-platform|subassembly|thruster|payload/.test(`${cp} ${label}`) || type === 'TECHNOLOGY') {
    stage = 'subsystems';
    role = type === 'TECHNOLOGY' ? 'technology' : 'product';
    supportRail = false;
  } else if (cp.startsWith('/fac/') || /plant|assembly|integration|fab|yard|launch|foundry|center/.test(label)) {
    stage = 'integration';
    role = 'facility';
    supportRail = false;
  } else if (/operator|service|carrier|provider|constellation|mission/.test(label) || edgeTypes.has('USES')) {
    stage = 'operators';
    role = /service|provider/.test(label) ? 'service' : 'operator';
    supportRail = false;
  } else if (cp.startsWith('/market/') || type === 'MARKET' || edgeTypes.has('TRADED_IN') || edgeTypes.has('PARTICIPATES_IN')) {
    stage = 'markets';
    role = 'market';
    supportRail = false;
  } else if (cp.startsWith('/gov/') || type === 'GOV' || /regulat|authority|ministry|agency/.test(label)) {
    stage = 'support';
    role = 'regulator';
    supportRail = true;
  } else if (geoAnchor?.kind === 'country' || geoAnchor?.kind === 'region' || geoAnchor?.kind === 'bloc') {
    stage = 'support';
    role = 'country';
    supportRail = true;
  } else if (cp.startsWith('/org/') || type === 'ORGANIZATION') {
    stage = edgeTypes.has('TRANSFORMS_INTO') || edgeTypes.has('PRODUCES')
      ? 'integration'
      : edgeTypes.has('SUPPLIES_TO')
        ? 'operators'
        : 'support';
    role = 'company';
    supportRail = stage === 'support';
  }

  const valuePoolBand =
    stage === 'markets' || stage === 'operators'
      ? 'high'
      : stage === 'integration' || stage === 'subsystems'
        ? 'medium'
        : 'low';

  return {
    stage,
    role,
    swimlane: supportRail ? 'support' : role,
    geoAnchor,
    supportRail,
    valuePoolBand,
    bottleneck: stage === 'inputs' || stage === 'components',
  };
}

export function classifyValueChainRelation(edgeType: string): ValueChainValueClass {
  const et = normalizeEdgeType(edgeType);
  if (et === 'TRANSFORMS_INTO') return 'transforms-into';
  if (et === 'SUPPLIES_TO' || et === 'PRODUCES' || et === 'EXTRACTS_FROM') return 'supplies-input';
  if (et === 'DEPENDS_ON' || et === 'REQUIRES' || et === 'USES' || et === 'NEEDS') return 'enables';
  if (et === 'REGULATES' || et === 'ADMINISTERS' || et === 'LOCATED_IN' || et === 'OPERATES_IN' || et === 'BASED_IN' || et === 'HEADQUARTERED_IN') {
    return 'governance-context';
  }
  return 'sells-into';
}
