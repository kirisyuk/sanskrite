package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// EdgeAllowlistByDomainMode delegates to contract semantics as the source of truth
// for runtime contract isolation, then re-attaches the scout-level legacy
// literals (transforms_into, assembles_into, used_in, extracts_from, …) that
// contractsemantics drops because they have no AH RelationName equivalent.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// taxonomy.AllowedEdgeTypesForDomainMode returns the SoT wire tokens (including
// legacy-only ones such as "TRANSFORMS_INTO" that the migration map folds into
// site siteTransforms[]). contractsemantics.normalizeWireRelations strips the
// dropped tokens, but scout still surfaces those edges during the migration
// window — read-side back-compat with pre-migration Firestore documents and
// scaffold emissions. We re-attach the scout legacy literal here so the
// runtime allowlist used by AllowlistContains stays a strict superset of the
// AH allowedEdges set.
func EdgeAllowlistByDomainMode(mode string) []DependencyType {
	profile := contractsemantics.ProfileForDomainMode(mode)
	allowed := profile.AllowedEdges
	out := make([]DependencyType, 0, len(allowed)+8)
	seen := make(map[DependencyType]struct{}, len(allowed)+8)
	for _, edgeType := range allowed {
		if _, dup := seen[edgeType]; dup {
			continue
		}
		seen[edgeType] = struct{}{}
		out = append(out, edgeType)
	}
	for _, legacy := range scoutLegacyAllowlistFor(mode) {
		if _, dup := seen[legacy]; dup {
			continue
		}
		seen[legacy] = struct{}{}
		out = append(out, legacy)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EdgeAllowlistByDomainModes returns the deduplicated union of allowed edges across
// normalized domain modes, preserving contract semantics as the source of truth.
func EdgeAllowlistByDomainModes(modes []string) []DependencyType {
	composed := contractsemantics.ComposeDomainModes(modes)
	allowed := composed.AllowedEdges
	out := make([]DependencyType, 0, len(allowed)+8)
	seen := make(map[DependencyType]struct{}, len(allowed)+8)
	for _, edgeType := range allowed {
		if _, dup := seen[edgeType]; dup {
			continue
		}
		seen[edgeType] = struct{}{}
		out = append(out, edgeType)
	}
	for _, mode := range modes {
		for _, legacy := range scoutLegacyAllowlistFor(mode) {
			if _, dup := seen[legacy]; dup {
				continue
			}
			seen[legacy] = struct{}{}
			out = append(out, legacy)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// scoutLegacyAllowlistFor returns the per-mode wire tokens (UPPER_SNAKE)
// that the taxonomy SoT in internal/taxonomy/domain_mode.go declares as
// allowed edges for `mode`, plus the matching scout snake_case literal
// when the wire token has no AH home (so legacy reads still match).
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// We can't rely solely on contractsemantics.ProfileForDomainMode here:
//
//  1. Tokens marked `Dropped: true` in legacyRelationAliases (e.g.
//     TRANSFORMS_INTO, HAS_SUBGROUP, AFFILIATED_WITH) are stripped from
//     the AH allowedEdges set but are still listed by the SoT taxonomy
//     for the corresponding mode and must remain in scout's runtime
//     allowlist for read-side back-compat.
//
//  2. Tokens with an AH home that is NOT a one-to-one rewrite (e.g.
//     CONTROLS_ACCESS -> imposes+attributes, AUDITED_BY -> assures+swap)
//     must only count as in-allowlist when the SoT taxonomy explicitly
//     lists the wire form. Returning the wire form here lets
//     AllowlistContains match it via strict equality without leaking
//     "AUDITED_BY allowed under assurance" through a wire->AH fallback.
//
// Keep this in sync with the per-mode allowlists in
// internal/taxonomy/domain_mode.go.
func scoutLegacyAllowlistFor(mode string) []DependencyType {
	wire := taxonomy.AllowedEdgeTypesForDomainMode(mode)
	if len(wire) == 0 {
		return nil
	}
	out := make([]DependencyType, 0, 2*len(wire))
	for _, w := range wire {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		out = append(out, DependencyType(w))
		if _, ok := models.NormalizeWireRelationStrict(w); ok {
			continue
		}
		if legacy := normalizeToScoutRelation(w); legacy != "" {
			out = append(out, legacy)
		}
	}
	return out
}

// scoutKnownDependencyTypes is the set of every scout-level DependencyType
// literal exposed by legacy_aliases.go. Used by normalizeToScoutRelation to
// detect inputs that are already in the scout vocabulary so we don't run
// them through the wire→AH alias map (which would, for example, collapse
// the legacy literal "operates_via" onto the AH "wields" form even though
// scout's runtime overlay rules treat the two as semantically distinct
// during the migration window).
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// keep this set in sync with the const block in legacy_aliases.go.
var scoutKnownDependencyTypes = map[DependencyType]struct{}{
	DependencyTypeOwns:                {},
	DependencyTypeSubsidiaryOf:        {},
	DependencyTypeControls:            {},
	DependencyTypeInvestsIn:           {},
	DependencyTypeSuppliesTo:          {},
	DependencyTypeProduces:            {},
	DependencyTypeRegulates:           {},
	DependencyTypeAssures:             {},
	DependencyTypeInfluences:          {},
	DependencyTypeGatekeepsDecision:   {},
	DependencyTypeUsesMechanism:       {},
	DependencyTypeImposesCondition:    {},
	DependencyTypeAdministers:         {},
	DependencyTypeMemberOf:            {},
	DependencyTypeTradedIn:            {},
	DependencyTypePartnersWith:        {},
	DependencyTypeAffects:             {},
	DependencyTypeBoardMemberOf:       {},
	DependencyTypeMergedInto:          {},
	DependencyTypeOperatesIn:          {},
	DependencyTypeOperatesVia:         {},
	DependencyTypeManages:             {},
	DependencyTypeHasSubgroup:         {},
	DependencyTypeHasSubmarket:        {},
	DependencyTypeHasMarketSegment:    {},
	DependencyTypeAffiliatedWith:      {},
	DependencyTypeAssociatedWith:      {},
	DependencyTypeEmployedBy:          {},
	DependencyTypeWorksFor:            {},
	DependencyTypeFounded:             {},
	DependencyTypeKeyPerson:           {},
	DependencyTypeHasKeyPerson:        {},
	DependencyTypeLocatedIn:           {},
	DependencyTypeDependsOn:           {},
	DependencyTypeFocusesOn:           {},
	DependencyTypeCompetesWith:        {},
	DependencyTypeParticipatesIn:      {},
	DependencyTypeInGeoScope:          {},
	DependencyTypeControlsAccess:      {},
	DependencyTypeAppliesTo:           {},
	DependencyTypeTransformsInto:      {},
	DependencyTypeAssemblesInto:       {},
	DependencyTypeExtractsFrom:        {},
	DependencyTypeUsedIn:              {},
	DependencyTypeAuditedBy:           {},
	DependencyTypeInvolvesActor:       {},
	DependencyTypeAbout:               {},
	DependencyTypeSurfaces:            {},
	DependencyTypeRelatedTo:           {},
	DependencyTypeRelativeOf:          {},
	DependencyTypeRenamedTo:           {},
	DependencyTypeRebrandedTo:         {},
	DependencyTypeLegalSuccessorOf:    {},
	DependencyTypeSpunOffFrom:         {},
	DependencyTypeAcquiredBy:          {},
	DependencyTypeCorporateTransition: {},
	DependencyTypeDominates:           {},
	DependencyTypeDomainRelation:      {},
	DependencyTypeRelatedDomain:       {},
	DependencyTypeBroadensTo:          {},
	DependencyTypeNarrowsTo:           {},
}

// normalizeToScoutRelation maps any wire-format relation token to the
// scout-package DependencyType form used by the supply-chain overlay
// switch in engine.go and by AllowlistContains.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
//   - When the lowercased input is already a known scout DependencyType
//     literal we keep it as-is. This preserves the legacy semantics for
//     tokens like OPERATES_VIA / MANAGES / ACQUIRED_BY whose AH targets
//     (wields / managed_by / acquired_stake_from) carry stricter overlay
//     rules at scout level than the legacy literal did. Mixing the two
//     would silently lose pre-migration edges that scout still surfaces
//     during the migration window.
//   - Otherwise we delegate to the wire→AH alias map so legacy
//     UPPER_SNAKE tokens (OWNS, INFLUENCES, ASSURES, …) reach the scout
//     constants that hold their AH RelationName (DependencyTypeOwns,
//     DependencyTypeInfluences, DependencyTypeAssures, …).
//   - Unknown / dropped tokens fall back to lowercase so the caller can
//     still see them (e.g. for filtering decisions).
func normalizeToScoutRelation(s string) DependencyType {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := DependencyType(strings.ToLower(s))
	if _, ok := scoutKnownDependencyTypes[lower]; ok {
		return lower
	}
	if rel, ok := models.NormalizeWireRelationStrict(s); ok {
		return rel
	}
	return lower
}

// AllowlistContains reports whether the allowlist contains the relation t.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// `allowlist` is sourced from contractsemantics.ProfileForDomainMode, which
// returns canonical lowercase ah.RelationName entries. Legacy callers
// (scout tests, persisted Firestore data, prompts emitted by the older
// agent runtimes) still pass UPPER_SNAKE wire tokens such as "PRODUCES"
// or "OWNS". Normalize the probe through the wire→AH alias map so a
// strict equality check can match either form. We never weaken the
// allowlist: a wire token that has no AH home (Dropped relations from
// legacyRelationAliases, e.g. AUDITED_BY) still falls through to the
// strict edgesemantics equality check and is rejected.
func AllowlistContains(allowlist []DependencyType, t DependencyType) bool {
	if edgesemantics.AllowlistContains(allowlist, t) {
		return true
	}
	if scoutForm := normalizeToScoutRelation(string(t)); scoutForm != "" && scoutForm != t {
		if edgesemantics.AllowlistContains(allowlist, scoutForm) {
			return true
		}
	}
	return false
}

// DomainScaffoldRelationTypesString / IsDomainScaffoldRelationType /
// IsMarketRelationType are provided by scout/legacy_helpers.go after the
// AH "Big Bang" migration removed the edgesemantics helpers.

// SupplyChainEdgeTypesForPrompt returns prompt enum string for supply-chain-only schemas.
// Use in supply-chain contract prompts instead of AllEdgeTypesForPrompt.
func SupplyChainEdgeTypesForPrompt() string {
	return taxonomy.SupplyChainEdgeTypesForPrompt()
}

// IsSupplyChainEdgeType returns true if t is in SupplyChainEdgeTypes (case-insensitive).
// Use for runtime guards before materialization.
func IsSupplyChainEdgeType(t string) bool {
	return taxonomy.IsSupplyChainEdgeType(t)
}

// IsAllowedSupplyChainRuntimeRelation applies the runtime supply-chain gate with
// the same domain/market scaffold exceptions used by relation pre-filtering.
func IsAllowedSupplyChainRuntimeRelation(sourcePath, targetPath string, relationType DependencyType) bool {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	// MIGRATION NOTE (AH taxonomy): use the scout-aware normalizer so the
	// switch below — keyed on scout DependencyType constants (snake_case
	// AH RelationName for relations that have an AH home, snake_case
	// legacy literal otherwise) — receives values in matching shape.
	// taxonomy.NormalizeRelationType would return UPPER_SNAKE strings
	// here and silently miss every case.
	relationType = normalizeToScoutRelation(string(relationType))
	if relationType == "" {
		return false
	}
	switch relationType {
	case DependencyTypeCompetesWith, DependencyTypePartnersWith, DependencyTypeOperatesVia, DependencyTypeMemberOf, DependencyTypeInvestsIn:
		return allowSupplyChainOverlayByPath(sourcePath, targetPath, relationType)
	}
	if IsSupplyChainEdgeType(string(relationType)) {
		return true
	}
	if allowSupplyChainOverlayByPath(sourcePath, targetPath, relationType) {
		return true
	}
	if allowReversibleSupplyChainOverlayByPath(sourcePath, targetPath, relationType) {
		return true
	}
	if strings.HasPrefix(sourcePath, "/domain/") && IsDomainScaffoldRelationType(relationType) {
		return true
	}
	if strings.HasPrefix(sourcePath, "/market/") && (IsDomainScaffoldRelationType(relationType) || IsMarketRelationType(relationType)) {
		return true
	}
	return false
}

// IsAllowedSupplyChainPersistRelation keeps final persist gates aligned with the
// runtime relation contract, including scaffold exceptions from domain/market anchors.
func IsAllowedSupplyChainPersistRelation(sourcePath, targetPath string, relationType DependencyType) bool {
	return IsAllowedSupplyChainRuntimeRelation(sourcePath, targetPath, relationType)
}

func allowReversibleSupplyChainOverlayByPath(sourcePath, targetPath string, relationType DependencyType) bool {
	switch relationType {
	case DependencyTypeAdministers:
	default:
		return false
	}
	if !allowSupplyChainOverlayByPath(targetPath, sourcePath, relationType) {
		return false
	}
	sourceType := InferFragmentTypeFromCanonicalPath(sourcePath)
	targetType := InferFragmentTypeFromCanonicalPath(targetPath)
	if sourceType == "" || targetType == "" {
		return false
	}
	// MIGRATION NOTE (AH taxonomy): use scout's ValidateEdgeDirection so
	// the legacy back-compat rows seeded into scout.EdgeDirectionPolicies
	// (e.g. ah.RelationNameOperates org->org for ADMINISTERS) participate
	// in the swap-detection that gates reversible supply-chain overlay
	// edges. The bare edgesemantics call would only see the AH-canonical
	// pairs and refuse to reverse legacy program/gov ↔ org/org edges.
	//
	// When source and target are the same FragmentClass (e.g. /program/
	// and /gov/ both resolve to ah.FragmentClassOrg under the legacy
	// (Class, Subtype) decomposition) ValidateEdgeDirection cannot
	// distinguish the two directions and reports valid=true /
	// shouldReverse=false even though the path-based gate above already
	// proved we need to swap the *paths*. Treat that case as eligible
	// for reverse-overlay: the path swap is what canonicalization
	// actually consumes, and the AH catalog has nothing to say about
	// org↔org subtype distinctions.
	valid, shouldReverse, _ := ValidateEdgeDirection(relationType, sourceType, targetType)
	if shouldReverse {
		return true
	}
	if valid && sourceType == targetType {
		return true
	}
	return false
}
