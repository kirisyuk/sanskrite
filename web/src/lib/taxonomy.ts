/**
 * Client-side taxonomy helpers (AH wire). Relation coercion accepts canonical AH
 * `snake_case` names from `RELATION_NAMES`, case-insensitive, with `-` treated as `_`.
 * Synthetic `CORRIDOR_*` is handled only in `normalizeEdgeType` (not coerced here).
 */
import {
  FRAGMENT_CLASS,
  FRAGMENT_CLASSES,
  RELATION,
  RELATION_NAMES,
  type FragmentClass,
  type RelationName,
} from './taxonomy.generated';

const RELATION_NAME_SET = new Set<string>(RELATION_NAMES);

/**
 * If `raw` matches a known AH relation (any case, hyphen or underscore), return canonical RelationName.
 * Returns undefined for empty input, `CORRIDOR_*`, and unknown strings.
 */
export function tryCoerceWireRelation(raw: string | undefined | null): RelationName | undefined {
  const s = String(raw ?? '').trim();
  if (!s) return undefined;
  if (s.toUpperCase().startsWith('CORRIDOR_')) {
    return undefined;
  }
  const norm = s.toLowerCase().replace(/-/g, '_');
  if (RELATION_NAME_SET.has(norm)) {
    return norm as RelationName;
  }
  return undefined;
}

/**
 * Canonical relation string for graph/layout code: AH snake_case, synthetic CORRIDOR_* unchanged (uppercase),
 * unknown → `other` (layout default bucket).
 */
export function normalizeEdgeType(raw: unknown): string {
  const s = String(raw ?? '').trim();
  if (!s) return 'other';
  if (s.toUpperCase().startsWith('CORRIDOR_')) {
    return s.toUpperCase();
  }
  const coerced = tryCoerceWireRelation(s);
  if (coerced) return coerced;
  return 'other';
}

/**
 * `/api/horizon/products/stream` edge `relationKind`: backend normalizes to assembly | usage | transformation;
 * accept legacy spellings and UPPER_SNAKE transform labels for older caches.
 */
export type ProductStreamEdgeVisualKind = 'assembly' | 'usage' | 'transformation';

export function productStreamEdgeVisualKind(raw: string | undefined | null): ProductStreamEdgeVisualKind {
  const s = String(raw ?? '').trim();
  if (!s) return 'transformation';
  const norm = s.toLowerCase().replace(/-/g, '_');
  if (norm === 'assembly' || norm === 'assembles_into' || norm === 'assemblesinto') return 'assembly';
  if (norm === 'usage' || norm === 'used_in' || norm === 'usedin') return 'usage';
  if (norm === 'transformation' || norm === 'transform' || norm === 'transforms_into' || norm === 'transformsinto') {
    return 'transformation';
  }
  const upper = s.toUpperCase().replace(/-/g, '_');
  if (upper === 'assembles_into') return 'assembly';
  if (upper === 'USED_IN') return 'usage';
  if (upper === 'transforms_into') return 'transformation';
  return 'transformation';
}

export function coerceWireRelation(raw: unknown): RelationName {
  const coerced = tryCoerceWireRelation(String(raw ?? '').trim());
  if (coerced) return coerced;
  return RELATION.Supplies;
}

// ---- Fragment class (AH wire `class` + optional `subtype`) ----

const FRAGMENT_CLASS_SET = new Set<string>(FRAGMENT_CLASSES);

const LEGACY_FRAGMENT_TYPE_TO_CLASS: Record<string, FragmentClass> = {
  ORGANIZATION: FRAGMENT_CLASS.Org,
  ORG: FRAGMENT_CLASS.Org,
  FACILITY: FRAGMENT_CLASS.Site,
  SITE: FRAGMENT_CLASS.Site,
  PRODUCT: FRAGMENT_CLASS.Product,
  PERSON: FRAGMENT_CLASS.Person,
  MARKET: FRAGMENT_CLASS.Market,
  COMMUNITY: FRAGMENT_CLASS.Community,
  GOV: FRAGMENT_CLASS.Org,
  GOVERNMENT: FRAGMENT_CLASS.Org,
  GOVERNMENT_AGENCY: FRAGMENT_CLASS.Org,
  GEO_REGION: FRAGMENT_CLASS.Geo,
  GEO: FRAGMENT_CLASS.Geo,
  COUNTRY: FRAGMENT_CLASS.Geo,
  REGION: FRAGMENT_CLASS.Geo,
  DECISION_POINT: FRAGMENT_CLASS.Decision,
  DECISION: FRAGMENT_CLASS.Decision,
  MECHANISM: FRAGMENT_CLASS.Mechanism,
  PROGRAM: FRAGMENT_CLASS.Project,
  PROJECT: FRAGMENT_CLASS.Project,
  EVENT: FRAGMENT_CLASS.Evidence,
  EVIDENCE: FRAGMENT_CLASS.Evidence,
  TECHNOLOGY: FRAGMENT_CLASS.Product,
  SERVICE: FRAGMENT_CLASS.Project,
  DOMAIN: FRAGMENT_CLASS.Org,
  SUPPLIER: FRAGMENT_CLASS.Org,
  DEPENDENCY: FRAGMENT_CLASS.Org,
  deposit: FRAGMENT_CLASS.Product,
  RISK: FRAGMENT_CLASS.Evidence,
  MEDIA_CHANNEL: FRAGMENT_CLASS.Market,
  REGIME: FRAGMENT_CLASS.Regime,
  ROUTE: FRAGMENT_CLASS.Route,
  MOBILE_ASSET: FRAGMENT_CLASS.MobileAsset,
  VESSEL_CLASS: FRAGMENT_CLASS.VesselClass,
};

/** Resolve wire `class` (snake_case) or legacy FragmentType UPPER_SNAKE to FragmentClass. */
export function tryParseFragmentClass(raw: string | undefined | null): FragmentClass | undefined {
  const s = String(raw ?? '').trim();
  if (!s) return undefined;
  const lower = s.toLowerCase();
  if (FRAGMENT_CLASS_SET.has(lower)) return lower as FragmentClass;
  return LEGACY_FRAGMENT_TYPE_TO_CLASS[s.toUpperCase()];
}

/** Title Case labels for AH fragment classes (e.g. `mobile_asset` → "Mobile Asset"). */
export function humanizeFragmentClass(cls: FragmentClass): string {
  return cls
    .split('_')
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(' ');
}
