/**
 * Client-side taxonomy helpers (AH wire). P13 will delete `legacyRelationToAh` once
 * browser caches / persisted prefs no longer emit UPPER_SNAKE.
 */
import {
  RELATION,
  RELATION_NAMES,
  type RelationName,
} from './taxonomy.generated';

const RELATION_NAME_SET = new Set<string>(RELATION_NAMES);

/** P12.01: UPPER_SNAKE / FE-only tokens → AH RelationName (mirrors backend legacyRelationAliases + lens FE aliases). */
export const legacyRelationToAh: Readonly<Record<string, RelationName>> = {
  // Core supply-chain
  SUPPLIES_TO: RELATION.Supplies,
  PRODUCES: RELATION.Produces,
  CONSUMES: RELATION.Supplies,
  EXTRACTS_FROM: RELATION.Produces,
  USED_IN: RELATION.Supplies,
  TRANSFORMS_INTO: RELATION.Produces,
  ASSEMBLES_INTO: RELATION.Produces,
  DEPENDS_ON: RELATION.Supplies,

  // Geographic / locational (dropped on backend; best-effort for cached lens edges)
  LOCATED_IN: RELATION.IncorporatedIn,
  OPERATES_IN: RELATION.Operates,
  IN_GEO_SCOPE: RELATION.ConstrainedBy,
  BASED_IN: RELATION.IncorporatedIn,
  HEADQUARTERED_IN: RELATION.IncorporatedIn,

  // Ownership / control
  OWNS: RELATION.BeneficiallyOwns,
  PARENT_OF: RELATION.BeneficiallyOwns,
  SUBSIDIARY_OF: RELATION.SubsidiaryOf,
  CONTROLS: RELATION.ControlsVia,
  CONTROLS_ACCESS: RELATION.Imposes,
  ADMINISTERS: RELATION.Operates,
  MANAGES: RELATION.ManagedBy,
  OPERATES_VIA: RELATION.Wields,

  // Corporate events
  INVESTS_IN: RELATION.InvestedIn,
  PARTNERS_WITH: RELATION.JointVentureWith,
  BOARD_MEMBER_OF: RELATION.HoldsBoardSeat,
  EXECUTIVE_OF: RELATION.HoldsBoardSeat,
  FOUNDED: RELATION.Influences,
  EMPLOYED_BY: RELATION.MemberOf,
  WORKS_FOR: RELATION.MemberOf,
  KEY_PERSON: RELATION.Influences,
  HAS_KEY_PERSON: RELATION.Influences,
  RENAMED_TO: RELATION.Influences,
  REBRANDED_TO: RELATION.Influences,
  LEGAL_SUCCESSOR_OF: RELATION.MergedInto,
  MERGED_INTO: RELATION.MergedInto,
  SPUN_OFF_FROM: RELATION.SpunOut,
  ACQUIRED_BY: RELATION.AcquiredStakeFrom,
  CORPORATE_TRANSITION: RELATION.MergedInto,

  // Regulation / regime
  REGULATES: RELATION.Regulates,
  IMPOSES_CONDITION: RELATION.Imposes,
  APPLIES_TO: RELATION.Imposes,
  GATEKEEPS_DECISION: RELATION.Gatekeeps,
  USES_MECHANISM: RELATION.Wields,

  // Influence
  INFLUENCES: RELATION.Influences,
  DOMINATES: RELATION.Influences,
  COMPETES_WITH: RELATION.AlternateTo,
  CONFLICTS_WITH: RELATION.Contradicts,
  OPPOSES: RELATION.Influences,
  PARTICIPATES_IN: RELATION.MemberOf,

  // Markets
  HAS_MARKET_SEGMENT: RELATION.Influences,
  HAS_SUBMARKET: RELATION.Influences,
  TRADED_IN: RELATION.ListedOn,

  // Assurance
  ASSURES: RELATION.Assures,
  AUDITED_BY: RELATION.Assures,

  // Membership / affiliation
  MEMBER_OF: RELATION.MemberOf,
  AFFILIATED_WITH: RELATION.JointVentureWith,
  ASSOCIATED_WITH: RELATION.JointVentureWith,
  RELATIVE_OF: RELATION.Influences,
  RELATED_TO: RELATION.Influences,
  HAS_SUBGROUP: RELATION.MemberOf,

  // Domain bookkeeping (legacy)
  DOMAIN_RELATION: RELATION.Influences,
  RELATED_DOMAIN: RELATION.Influences,
  BROADENS_TO: RELATION.Influences,
  NARROWS_TO: RELATION.Influences,
  FOCUSES_ON: RELATION.Influences,

  // Event scaffolding
  INVOLVES_ACTOR: RELATION.Influences,
  ABOUT: RELATION.Influences,
  SURFACES: RELATION.Influences,
  AFFECTS: RELATION.Disrupts,
  IMPACTS: RELATION.Disrupts,
  TRIGGERED_BY: RELATION.Influences,

  // Supply-link / misc legacy
  SUPPLY_LINK: RELATION.Supplies,

  // FE-only aliases (lens_item.go lensItemFEOnlyEdgeAliases)
  COOPERATES_WITH: RELATION.JointVentureWith,
  REQUIRES: RELATION.RequiresCapability,
  NEEDS: RELATION.RequiresCapability,
  USES: RELATION.RequiresCapability,
  SIMILAR_TO: RELATION.AlternateTo,
  LINKED_TO: RELATION.Influences,
  SUPPORTS: RELATION.SupportedBy,
};

let legacyWarnCount = 0;
const LEGACY_WARN_CAP = 24;

function warnLegacyMap(from: string, to: RelationName) {
  if (legacyWarnCount >= LEGACY_WARN_CAP) return;
  legacyWarnCount += 1;
  console.warn('[P13 drain] legacyRelationToAh', { from, to });
}

/**
 * If `raw` is already an AH relation (any case), return canonical RelationName.
 * Otherwise map legacy UPPER_SNAKE / FE synonyms. Returns undefined if unmappable.
 */
export function tryCoerceWireRelation(raw: string | undefined | null): RelationName | undefined {
  const s = String(raw ?? '').trim();
  if (!s) return undefined;
  if (s.startsWith('CORRIDOR_')) {
    return undefined;
  }
  const lower = s.toLowerCase();
  if (RELATION_NAME_SET.has(lower)) {
    return lower as RelationName;
  }
  const upper = s.toUpperCase();
  const mapped = legacyRelationToAh[upper];
  if (mapped) {
    if (upper !== lower && !RELATION_NAME_SET.has(lower)) {
      warnLegacyMap(upper, mapped);
    }
    return mapped;
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

export function coerceWireRelation(raw: unknown): RelationName {
  const coerced = tryCoerceWireRelation(String(raw ?? '').trim());
  if (coerced) return coerced;
  const s = String(raw ?? '').trim();
  if (s) warnLegacyMap(s, RELATION.Supplies);
  return RELATION.Supplies;
}
