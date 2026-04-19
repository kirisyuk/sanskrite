package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// =============================================================================
// EDGE POLICIES
// =============================================================================
// This file defines normalization and validation policies for dependency edges.
// Policies are enforced at creation time and during cleanup operations.

// =============================================================================
// Policy 1: Symmetric Edge Normalization
// =============================================================================
// Some edge types represent symmetric relationships where direction doesn't matter.
// To avoid duplicates (A->B and B->A), we normalize by always placing the
// lexicographically smaller ID as source.

// SymmetricEdgeTypes is provided by scout/legacy_helpers.go after the AH
// "Big Bang" migration removed models.SymmetricEdgeTypes.

// NormalizeSymmetricEdge mirrors the legacy models.NormalizeSymmetricEdge
// helper: for symmetric relations it ensures a deterministic source/target
// ordering by placing the lexicographically smaller fragment ID as source.
func NormalizeSymmetricEdge(sourceID, targetID string, edgeType DependencyType) (string, string, DependencyType, bool) {
	if !SymmetricEdgeTypes[edgeType] {
		return sourceID, targetID, edgeType, false
	}
	if strings.Compare(sourceID, targetID) > 0 {
		return targetID, sourceID, edgeType, true
	}
	return sourceID, targetID, edgeType, false
}

// =============================================================================
// Policy 2: Edge Direction Validation
// =============================================================================
// Certain edge types have semantically meaningful directions that must be enforced.
// When an edge violates direction rules, it should be reversed (not deleted).

// EdgeDirectionRule delegates to edgesemantics.EdgeDirectionRule.
// Kept for backwards compatibility.
type EdgeDirectionRule = edgesemantics.EdgeDirectionRule

// =============================================================================
// Policy 2.5: Edge Canonicalization (Type + Direction Rewrites)
// =============================================================================
// Some edge patterns are not just "wrong direction" but "wrong relation type".
// In those cases, reversing is not sufficient: we rewrite type and (optionally) swap.
//
// NOTE: Keep this conservative and explicit. This is used at creation time and during cleanup.

// CanonicalizeEdge rewrites an edge type (and optionally direction) based on
// endpoint fragment types. After the AH "Big Bang" migration moved the
// canonicalization logic into the policy layer, this is now a thin wrapper
// over policy.CanonicalizeEdgeWithPaths called with empty paths.
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdge(edgeType DependencyType, sourceType, targetType FragmentType) (DependencyType, bool, bool, string) {
	return policy.CanonicalizeEdgeWithPaths(edgeType, sourceType, targetType, "", "")
}

// CanonicalizeEdgeWithPaths rewrites an edge type (and optionally direction)
// based on endpoint fragment types and canonical paths. Delegates to the
// policy layer (single source of truth post-AH migration).
func CanonicalizeEdgeWithPaths(edgeType DependencyType, sourceType, targetType FragmentType, sourcePath, targetPath string) (DependencyType, bool, bool, string) {
	return policy.CanonicalizeEdgeWithPaths(edgeType, sourceType, targetType, sourcePath, targetPath)
}

// EdgeDirectionPolicies is the scout view of the AH endpoint catalog,
// overlaid with legacy entries that scout still surfaces during the
// AH "Big Bang" migration window.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// The AH SoT (edgesemantics.EdgeDirectionPolicies) is keyed by canonical
// AH RelationName and intentionally has NO entry for the read-side
// legacy literals scout still emits during the migration window
// (extracts_from, located_in, operates_in, associated_with, …). It also
// lacks the legacy DEPOSIT (ah.FragmentClassSite) endpoint kinds that
// pre-migration code relied on for OWNS/PRODUCES/etc. We seed those
// here so the scout-level direction validator and
// CanonicalizeEdge wrappers continue to honour the pre-migration
// contract for back-compat consumers (Firestore replays, scaffolds).
var EdgeDirectionPolicies = buildScoutEdgeDirectionPolicies()

func buildScoutEdgeDirectionPolicies() map[ah.RelationName]edgesemantics.EdgeDirectionRule {
	out := make(map[ah.RelationName]edgesemantics.EdgeDirectionRule, len(edgesemantics.EdgeDirectionPolicies)+8)
	for rel, rule := range edgesemantics.EdgeDirectionPolicies {
		out[rel] = rule
	}
	addClass := func(slice []ah.FragmentClass, cls ah.FragmentClass) []ah.FragmentClass {
		for _, c := range slice {
			if c == cls {
				return slice
			}
		}
		return append(slice, cls)
	}
	addPair := func(rule edgesemantics.EdgeDirectionRule, from, to ah.FragmentClass, kind string) edgesemantics.EdgeDirectionRule {
		for _, p := range rule.Pairs {
			if p.From == from && p.To == to {
				rule.ValidSourceClasses = addClass(rule.ValidSourceClasses, from)
				rule.ValidTargetClasses = addClass(rule.ValidTargetClasses, to)
				return rule
			}
		}
		rule.Pairs = append(rule.Pairs, ah.EndpointKindRow{From: from, To: to, Kind: kind})
		rule.ValidSourceClasses = addClass(rule.ValidSourceClasses, from)
		rule.ValidTargetClasses = addClass(rule.ValidTargetClasses, to)
		return rule
	}

	// LEGACY (read-side only): OWNS / BeneficiallyOwns historically allowed
	// /org -> /deposit (now ah.FragmentClassSite + subtype=deposit). Keep
	// the parent-class entry so Firestore replays and scaffold writers
	// continue to accept the org->site shape.
	if rule, ok := out[ah.RelationNameBeneficiallyOwns]; ok {
		rule = addPair(rule, ah.FragmentClassOrg, ah.FragmentClassSite, "legacy_org_owns_site")
		out[ah.RelationNameBeneficiallyOwns] = rule
	}

	// LEGACY (read-side only): ADMINISTERS / RelationNameOperates historically
	// also covered org->org and org->program (e.g. ministry administers a
	// state-owned program / agency). The AH catalog narrowed Operates to
	// org->site (operator-of-record); scout still emits the legacy shape
	// during the migration window so canonicalization can swap reversed
	// program->gov / org->program edges before persistence. Without this
	// row scout.ValidateEdgeDirection would refuse to recognise the
	// legacy pair and the supply-chain reversible-overlay gate would
	// reject the edge outright.
	if rule, ok := out[ah.RelationNameOperates]; ok {
		rule = addPair(rule, ah.FragmentClassOrg, ah.FragmentClassOrg, "legacy_org_administers_org")
		out[ah.RelationNameOperates] = rule
	}

	// LEGACY (read-side only): PRODUCES historically allowed
	// site (DEPOSIT) -> product (extractive output). The AH catalog now
	// lists only org->product; keep the site->product back-compat row so
	// scout's deposit-anchored extraction edges still validate.
	if rule, ok := out[ah.RelationNameProduces]; ok {
		rule = addPair(rule, ah.FragmentClassSite, ah.FragmentClassProduct, "legacy_site_produces_product")
		out[ah.RelationNameProduces] = rule
	}

	// LEGACY (read-side only): EXTRACTS_FROM has no AH home (folded into
	// site siteTransforms[]). Pre-migration semantics: facility -> deposit
	// (now site -> site via subtype=deposit, but at the parent class
	// level the edge is fac -> site). Use the legacy DependencyType key
	// so existing scout call sites resolve.
	out[DependencyTypeExtractsFrom] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassSite},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassSite},
		Pairs:              []ah.EndpointKindRow{{From: ah.FragmentClassSite, To: ah.FragmentClassSite, Kind: "legacy_extracts_from"}},
		Description:        "LEGACY (read-side only): facility extracts from deposit (both ah.FragmentClassSite, deposit subtype on target).",
	}

	// LEGACY (read-side only): LOCATED_IN — historical site/org/community
	// "is located in" a geo region. AH folds location into attribute
	// scaffolding; scout keeps the edge for back-compat queries.
	out[DependencyTypeLocatedIn] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassSite, ah.FragmentClassOrg, ah.FragmentClassCommunity},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassGeo},
		Pairs: []ah.EndpointKindRow{
			{From: ah.FragmentClassSite, To: ah.FragmentClassGeo, Kind: "legacy_site_located_in_geo"},
			{From: ah.FragmentClassOrg, To: ah.FragmentClassGeo, Kind: "legacy_org_located_in_geo"},
			{From: ah.FragmentClassCommunity, To: ah.FragmentClassGeo, Kind: "legacy_community_located_in_geo"},
		},
		Description: "LEGACY (read-side only): entity is geographically located in a region.",
	}

	// LEGACY (read-side only): OPERATES_IN — historical org/community
	// "operates in" a geo region or site (deposit). Mirror of the
	// pre-migration policy.
	out[DependencyTypeOperatesIn] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassOrg, ah.FragmentClassCommunity},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassGeo, ah.FragmentClassSite, ah.FragmentClassMarket},
		Pairs: []ah.EndpointKindRow{
			{From: ah.FragmentClassOrg, To: ah.FragmentClassGeo, Kind: "legacy_org_operates_in_geo"},
			{From: ah.FragmentClassOrg, To: ah.FragmentClassSite, Kind: "legacy_org_operates_in_site"},
			{From: ah.FragmentClassOrg, To: ah.FragmentClassMarket, Kind: "legacy_org_operates_in_market"},
			{From: ah.FragmentClassCommunity, To: ah.FragmentClassGeo, Kind: "legacy_community_operates_in_geo"},
		},
		Description: "LEGACY (read-side only): operational presence in a region/market/site.",
	}

	// LEGACY (read-side only): ASSOCIATED_WITH — symmetric "vague"
	// affiliation. AH dropped this as LLM noise; scout retains it for
	// read-side Firestore back-compat with pre-migration scaffolds.
	out[DependencyTypeAssociatedWith] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassOrg, ah.FragmentClassPerson, ah.FragmentClassCommunity},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassOrg, ah.FragmentClassPerson, ah.FragmentClassCommunity},
		Description:        "LEGACY (read-side only): symmetric affiliation between actors.",
	}

	// LEGACY (read-side only): CORPORATE_TRANSITION wrapper edge.
	out[DependencyTypeCorporateTransition] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassOrg},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassOrg},
		Description:        "LEGACY (read-side only): umbrella edge for renamed_to / merged_into / spun_off_from / acquired_by.",
	}

	// LEGACY (read-side only): DOMAIN_RELATION lattice edge. AH folds
	// DOMAIN into evidence overlays; scout keeps the lattice link for
	// backwards-compatible domain bookkeeping.
	out[DependencyTypeDomainRelation] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassEvidence},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassEvidence},
		Description:        "LEGACY (read-side only): symmetric domain<->domain bookkeeping link.",
	}

	// LEGACY (read-side only): RELATIVE_OF — symmetric kinship link
	// between persons. AH folds into attributes.relations[]; scout
	// keeps the edge for back-compat reads + LLM prompt vocabulary.
	out[DependencyTypeRelativeOf] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassPerson},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassPerson},
		Description:        "LEGACY (read-side only): kinship link between persons.",
	}

	// LEGACY (read-side only): RENAMED_TO / REBRANDED_TO — corporate
	// rename succession. AH folds into HorizonFragment.nameHistory[];
	// scout keeps the edge for back-compat queries + LLM prompts.
	for _, rel := range []DependencyType{
		DependencyTypeRenamedTo,
		DependencyTypeRebrandedTo,
	} {
		out[rel] = edgesemantics.EdgeDirectionRule{
			ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassOrg},
			ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassOrg},
			Description:        "LEGACY (read-side only): organizational rename succession (folded into nameHistory under AH).",
		}
	}

	// LEGACY (read-side only): EMPLOYED_BY / WORKS_FOR / KEY_PERSON /
	// HAS_KEY_PERSON / FOUNDED — actor↔actor relations dropped or
	// folded under AH (employed_by/works_for dropped as LLM noise,
	// key_person folded into attributes.key_persons[], founded folded
	// into attributes.founders[]). Scout keeps direction policies so
	// the LLM prompt vocabulary stays stable + scaffold paths can
	// validate during the migration window.
	for _, rel := range []DependencyType{
		DependencyTypeEmployedBy,
		DependencyTypeWorksFor,
		DependencyTypeKeyPerson,
		DependencyTypeHasKeyPerson,
		DependencyTypeFounded,
	} {
		out[rel] = edgesemantics.EdgeDirectionRule{
			ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassPerson, ah.FragmentClassOrg},
			ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassPerson, ah.FragmentClassOrg},
			Description:        "LEGACY (read-side only): person/org employment-or-affiliation edge folded under AH (employed_by/works_for dropped, key_person/founded folded to attributes).",
		}
	}

	// LEGACY (read-side only): AUDITED_BY — covered by AH assures
	// (assurance_type=audit). Scout keeps for back-compat reads.
	out[DependencyTypeAuditedBy] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassOrg},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassOrg},
		Description:        "LEGACY (read-side only): organisational audit relation (canonical AH form is `assures` with assurance_type=audit).",
	}

	// LEGACY (read-side only): HAS_SUBMARKET hierarchy. AH folded into
	// attributes.submarkets[]; scout keeps the legacy edge for read-side
	// back-compat with pre-migration market lattices.
	out[DependencyTypeHasSubmarket] = edgesemantics.EdgeDirectionRule{
		ValidSourceClasses: []ah.FragmentClass{ah.FragmentClassMarket},
		ValidTargetClasses: []ah.FragmentClass{ah.FragmentClassMarket},
		Pairs:              []ah.EndpointKindRow{{From: ah.FragmentClassMarket, To: ah.FragmentClassMarket, Kind: "legacy_market_has_submarket"}},
		Description:        "LEGACY (read-side only): market parent -> submarket lattice link.",
	}

	return out
}

// ValidateEdgeDirection checks if an edge has correct direction according to policies.
// Returns: (isValid, shouldReverse, rule)
// - isValid: true if current direction is valid
// - shouldReverse: true if reversing would make it valid
// - rule: the applicable rule (nil if no rule defined)
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// scout maintains its own EdgeDirectionPolicies overlay (see
// buildScoutEdgeDirectionPolicies above) that augments the AH SoT with
// legacy back-compat rows (org->site for OWNS, site->product for
// PRODUCES, org->org for ADMINISTERS/Operates, plus orphan legacy
// relations like extracts_from / located_in / operates_in /
// associated_with that have no AH home). We honour the overlay first
// and only fall through to edgesemantics when the relation has no
// scout-side rule, so callers get the broader pre-migration acceptance
// surface they relied on while AH stays canonically narrow.
func ValidateEdgeDirection(edgeType DependencyType, sourceType, targetType FragmentType) (bool, bool, *EdgeDirectionRule) {
	if rule, ok := EdgeDirectionPolicies[edgeType]; ok {
		ruleCopy := rule
		valid := false
		canReverse := false
		for _, p := range rule.Pairs {
			if p.From == sourceType && p.To == targetType {
				valid = true
				break
			}
			if p.From == targetType && p.To == sourceType {
				canReverse = true
			}
		}
		if !valid && len(rule.Pairs) == 0 {
			srcAllowed := len(rule.ValidSourceClasses) == 0
			dstAllowed := len(rule.ValidTargetClasses) == 0
			for _, c := range rule.ValidSourceClasses {
				if c == sourceType {
					srcAllowed = true
					break
				}
			}
			for _, c := range rule.ValidTargetClasses {
				if c == targetType {
					dstAllowed = true
					break
				}
			}
			valid = srcAllowed && dstAllowed
		}
		if valid {
			return true, false, &ruleCopy
		}
		if canReverse {
			return false, true, &ruleCopy
		}
		return false, false, &ruleCopy
	}
	return edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
}

// ShouldReverseEdge is a convenience function that returns true if the edge
// should be reversed to comply with direction policies.
func ShouldReverseEdge(edgeType DependencyType, sourceType, targetType FragmentType) bool {
	_, shouldReverse, _ := ValidateEdgeDirection(edgeType, sourceType, targetType)
	return shouldReverse
}

// =============================================================================
// Policy 3: Self-Loop Prevention
// =============================================================================
// Edges where source == target are typically errors and should be rejected.

// IsSelfLoop returns true when source and target IDs are the same fragment.
// Inlined here to avoid the deleted models.IsSelfLoop helper.
func IsSelfLoop(sourceID, targetID string) bool {
	return strings.TrimSpace(sourceID) == strings.TrimSpace(targetID)
}

// =============================================================================
// Policy 4: Edge Deduplication
// =============================================================================
// Duplicate edges (same source, target, type) should be merged, not duplicated.
// This is handled at the repository level, but the policy is documented here.

// EdgeKey generates a unique key for edge deduplication.
// For symmetric edges, the key is normalized to ensure A->B and B->A produce the same key.
func EdgeKey(sourceID, targetID string, edgeType DependencyType) string {
	if SymmetricEdgeTypes[edgeType] {
		if strings.Compare(sourceID, targetID) > 0 {
			sourceID, targetID = targetID, sourceID
		}
	}
	return sourceID + "|" + targetID + "|" + string(edgeType)
}
