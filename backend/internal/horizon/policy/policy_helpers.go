package policy

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// This file provides package-local helpers that previously lived in
// internal/horizon/models (edge_canonicalization.go, edge_policy_helpers.go,
// dependency_edge.go, common.go) and were removed during the AH taxonomy
// migration. The policy layer still needs these primitives to evaluate edge
// rules and validate strategic-edge qualifier shapes; we recreate them here
// using the new ah.RelationName / ah.FragmentClass vocabulary so the policy
// package compiles without re-introducing the legacy `horizonmodels.*Type`
// surface.

// =============================================================================
// SELF-LOOP / SYMMETRIC EDGES
// =============================================================================

// IsSelfLoopIDs returns true if the edge's source and target ids are the same.
// The legacy helper lived in models.IsSelfLoop. Renamed locally to avoid
// shadowing scout.IsSelfLoop while keeping behaviour identical.
func IsSelfLoopIDs(sourceID, targetID string) bool {
	return strings.TrimSpace(sourceID) == strings.TrimSpace(targetID)
}

// symmetricRelations enumerates AH relations whose endpoints are
// interchangeable (canonical model: the relation is unordered). Per the
// AH catalogue the relevant entries are joint_venture_with and any
// legacy aliases we still translate to one of those (associated_with,
// competes_with, related_to, …) — those legacy values remain string
// literals so that wire payloads from older agents still match.
var symmetricRelations = map[ah.RelationName]bool{
	ah.RelationNameJointVentureWith: true,
	ah.RelationNameAlternateTo:      true,

	relCompetesWith:   true,
	relAssociatedWith: true,
	relRelatedTo:      true,
	relAffiliatedWith: true,
	relPartnersWith:   true,
	relRelativeOf:     true,
	relDomainRelation: true,
}

// IsSymmetricEdgeRelation returns true when an AH relation (or one of the
// legacy wire aliases held in legacy_aliases*.go) is symmetric.
func IsSymmetricEdgeRelation(rel ah.RelationName) bool {
	return symmetricRelations[rel]
}

// NormalizeSymmetricEdge places the lexicographically smaller endpoint id on
// the source side for symmetric relations. Returns (src, tgt, rel, swapped).
// For asymmetric relations the input is returned unchanged.
func NormalizeSymmetricEdge(sourceID, targetID string, rel ah.RelationName) (string, string, ah.RelationName, bool) {
	if !IsSymmetricEdgeRelation(rel) {
		return sourceID, targetID, rel, false
	}
	if strings.Compare(sourceID, targetID) > 0 {
		return targetID, sourceID, rel, true
	}
	return sourceID, targetID, rel, false
}

// =============================================================================
// TRANSITION RELATIONS
// =============================================================================

var transitionRelations = map[ah.RelationName]bool{
	ah.RelationNameMergedInto: true,

	relCorporateTransition: true,
	relRenamedTo:           true,
	relRebrandedTo:         true,
	relLegalSuccessorOf:    true,
	relSpunOffFrom:         true,
	relAcquiredBy:          true,
}

// IsTransitionEdgeRelation reports whether the relation describes a corporate
// transition (rename / merger / spin-off / acquisition).
func IsTransitionEdgeRelation(rel ah.RelationName) bool {
	return transitionRelations[rel]
}

var transitionIntentTokens = []string{
	"RENAME", "REBRAND", "SUCCESSOR", "MERGED", "MERGER",
	"SPIN", "SPUN", "ACQUISITION", "ACQUIRED", "TAKEOVER",
	"FKA", "FORMERLY",
}

// IsTransitionLikeRelationName reports whether a raw wire string looks like a
// corporate-transition intent (canonical AH transition relation OR fuzzy
// keyword match).
func IsTransitionLikeRelationName(raw string) bool {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return false
	}
	if IsTransitionEdgeRelation(ah.RelationName(strings.ToLower(s))) {
		return true
	}
	if IsTransitionEdgeRelation(ah.RelationName(s)) {
		return true
	}
	for _, token := range transitionIntentTokens {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

// =============================================================================
// PRODUCT TRANSITION HELPERS (used by canonicalisation)
// =============================================================================

func isProductTransitionRelation(rel ah.RelationName) bool {
	switch rel {
	case relTransformsInto, relAssemblesInto, relUsedIn:
		return true
	}
	return false
}

func productToProductRelationFromPaths(sourcePath, targetPath string) ah.RelationName {
	if shouldPreferAssembleByProductPaths(sourcePath, targetPath) {
		return relAssemblesInto
	}
	return relTransformsInto
}

func shouldPreferAssembleByProductPaths(sourcePath, targetPath string) bool {
	src, srcOK := parseProductCanonicalParts(sourcePath)
	tgt, tgtOK := parseProductCanonicalParts(targetPath)
	if !srcOK || !tgtOK {
		return false
	}
	srcCategory := strings.ToLower(strings.TrimSpace(src.Category))
	tgtCategory := strings.ToLower(strings.TrimSpace(tgt.Category))
	srcStage := strings.ToLower(strings.TrimSpace(src.Stage))
	tgtStage := strings.ToLower(strings.TrimSpace(tgt.Stage))

	targetLooksAssembled := tgtCategory == "finished" || tgtStage == "assembly"
	sourceLooksSubassembly := srcCategory == "component" ||
		srcStage == "assembly" || srcStage == "manufacturing" || srcCategory == "finished"
	if !targetLooksAssembled || !sourceLooksSubassembly {
		return false
	}
	if srcCategory == "raw" {
		return false
	}
	return true
}

func parseProductCanonicalParts(path string) (*taxonomy.ProductPathParts, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return nil, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return nil, false
	}
	return parts.Product, true
}

func productStageRankFromCanonicalPath(path string) (int, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return 0, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return 0, false
	}
	stage := strings.ToLower(strings.TrimSpace(parts.Product.Stage))
	if stage == "" {
		return 0, false
	}
	order := taxonomy.GetProductStageOrder()
	for i, s := range order {
		if strings.ToLower(strings.TrimSpace(s)) == stage {
			return i, true
		}
	}
	return 0, false
}

func pathSegmentCount(path string) int {
	p := strings.Trim(strings.TrimSpace(path), "/")
	if p == "" {
		return 0
	}
	return len(strings.Split(p, "/"))
}

func isDynastyOrTribeCommunity(canonicalPath string) bool {
	if canonicalPath == "" || !strings.HasPrefix(canonicalPath, "/community/") {
		return false
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) < 3 {
		return false
	}
	communityType := strings.ToLower(strings.TrimSpace(parts[2]))
	return communityType == "dynasty" || communityType == "tribe"
}

// MarketSegmentPathsFollowRule reports whether the parent/child market paths
// follow the canonical "child = parent + segment" rule. Best-effort: also
// accepts longer prefix matches because legacy data often skipped strict
// +1-segment.
func MarketSegmentPathsFollowRule(parentPath, childPath string) bool {
	parentPath = strings.TrimSpace(strings.Trim(parentPath, "/"))
	childPath = strings.TrimSpace(strings.Trim(childPath, "/"))
	if parentPath == "" || childPath == "" {
		return true
	}
	prefix := parentPath + "/"
	if !strings.HasPrefix(childPath, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(childPath, prefix)
	if suffix == "" {
		return false
	}
	if !strings.Contains(suffix, "/") {
		return true
	}
	return pathSegmentCount(childPath) > pathSegmentCount(parentPath)
}

// =============================================================================
// CANONICALIZATION (path-aware)
// =============================================================================

// CanonicalizeEdgeWithPaths reapplies the small subset of path-aware rewrites
// that the legacy models.CanonicalizeEdgeWithPaths performed. We deliberately
// keep this minimal: the heavy lifting is now handled by edgesemantics and the
// AH endpoint catalog, and the edge_direction_autofix layer covers the
// non-path swaps. Returns (newRel, shouldSwap, changed, reason).
func CanonicalizeEdgeWithPaths(
	rel ah.RelationName,
	sourceClass, targetClass ah.FragmentClass,
	sourcePath, targetPath string,
) (ah.RelationName, bool, bool, string) {
	src := strings.TrimSpace(sourcePath)
	tgt := strings.TrimSpace(targetPath)

	// ADMINISTERS: /org -> /gov is backwards (gov bodies administer).
	if rel == relAdministers &&
		strings.HasPrefix(src, "/org/") && strings.HasPrefix(tgt, "/gov/") {
		return relAdministers, true, true,
			"canonicalize: ADMINISTERS /org -> /gov swapped to /gov -> /org"
	}
	// OWNS: /org -> /gov is backwards (gov owns SOEs).
	if rel == relOwns &&
		strings.HasPrefix(src, "/org/") && strings.HasPrefix(tgt, "/gov/") {
		return relOwns, true, true,
			"canonicalize: OWNS /org -> /gov swapped to /gov -> /org (gov owns state-owned enterprises)"
	}
	// CONTROLS: /product -> /gov|/org is backwards.
	if rel == relControls &&
		strings.HasPrefix(src, "/product/") &&
		(strings.HasPrefix(tgt, "/gov/") || strings.HasPrefix(tgt, "/org/")) {
		return relControls, true, true,
			"canonicalize: CONTROLS /product/ -> /gov/|/org/ swapped to /gov/|/org/ -> /product/"
	}

	// PRODUCT transition stage ordering: upstream stage must be source.
	if isProductTransitionRelation(rel) &&
		sourceClass == ah.FragmentClassProduct && targetClass == ah.FragmentClassProduct &&
		src != "" && tgt != "" {
		srcRank, srcOK := productStageRankFromCanonicalPath(src)
		tgtRank, tgtOK := productStageRankFromCanonicalPath(tgt)
		if srcOK && tgtOK && srcRank > tgtRank {
			return rel, true, true, fmt.Sprintf(
				"canonicalize: %s downstream->upstream -> upstream->downstream (by product stage order)", rel)
		}
	}

	// PRODUCT transition kind: transforms_into -> assembles_into when paths fit.
	if rel == relTransformsInto &&
		sourceClass == ah.FragmentClassProduct && targetClass == ah.FragmentClassProduct &&
		productToProductRelationFromPaths(src, tgt) == relAssemblesInto {
		return relAssemblesInto, false, true,
			"canonicalize: PRODUCT transition promoted to assembles_into by product category/stage"
	}

	// PRODUCT PRODUCES PRODUCT -> transforms_into (or assembles_into via paths).
	// Legacy PRODUCES between two products is a transformation, not a creation
	// relation. Re-route to the AH transform/assembly relation.
	if rel == relProduces &&
		sourceClass == ah.FragmentClassProduct && targetClass == ah.FragmentClassProduct {
		desired := productToProductRelationFromPaths(src, tgt)
		if desired == "" {
			desired = relTransformsInto
		}
		return desired, false, true,
			"canonicalize: PRODUCT PRODUCES PRODUCT -> " + string(desired)
	}

	// PRODUCT OWNS ORG -> ORG PRODUCES PRODUCT. Legacy emit-side LLMs sometimes
	// invert the relationship (source is the product, target is the producer);
	// rewrite type to PRODUCES and swap so direction matches AH semantics
	// (org -> product).
	if rel == relOwns &&
		sourceClass == ah.FragmentClassProduct && targetClass == ah.FragmentClassOrg {
		return relProduces, true, true,
			"canonicalize: PRODUCT OWNS ORG -> ORG PRODUCES PRODUCT (swap + rewrite)"
	}

	// EXTRACTS_FROM -> OPERATES_IN. Legacy extraction relation is dropped under
	// AH; the residual semantic is "org operates this site". Direction is
	// canonicalised so that org sits on the source side.
	if rel == relExtractsFrom {
		switch {
		case sourceClass == ah.FragmentClassOrg && targetClass == ah.FragmentClassSite:
			return relOperatesIn, false, true,
				"canonicalize: ORG EXTRACTS_FROM SITE -> ORG OPERATES_IN SITE"
		case sourceClass == ah.FragmentClassSite && targetClass == ah.FragmentClassOrg:
			return relOperatesIn, true, true,
				"canonicalize: SITE EXTRACTS_FROM ORG -> ORG OPERATES_IN SITE (swap + rewrite)"
		}
	}

	// APPLIES_TO + MECHANISM endpoint: canonical form is MECHANISM -> ENTITY.
	// If the LLM emitted ENTITY APPLIES_TO MECHANISM, swap so MECHANISM is on
	// the source side. Other APPLIES_TO patterns fold into INFLUENCES (legacy
	// catch-all relation).
	if rel == relAppliesTo {
		switch {
		case sourceClass != ah.FragmentClassMechanism && targetClass == ah.FragmentClassMechanism:
			return relAppliesTo, true, true,
				"canonicalize: ENTITY APPLIES_TO MECHANISM -> MECHANISM APPLIES_TO ENTITY (swap)"
		case sourceClass == ah.FragmentClassMechanism:
			// Already canonical: MECHANISM APPLIES_TO ENTITY — leave as-is.
		default:
			return relInfluences, false, true,
				"canonicalize: APPLIES_TO between non-mechanism endpoints -> INFLUENCES"
		}
	}

	// FOUNDED + PERSON target: canonical direction is PERSON FOUNDED COMMUNITY
	// or PERSON FOUNDED ORG. If a community/org is on the source side, swap.
	if rel == relFounded && targetClass == ah.FragmentClassPerson &&
		(sourceClass == ah.FragmentClassCommunity || sourceClass == ah.FragmentClassOrg) {
		return relFounded, true, true,
			"canonicalize: COMMUNITY|ORG FOUNDED PERSON -> PERSON FOUNDED COMMUNITY|ORG (swap)"
	}

	// MARKET INVESTS_IN ORG -> ORG PARTICIPATES_IN MARKET (swap + rewrite).
	// Legacy INVESTS_IN with a market on the source side is reinterpreted as
	// participation, not capital flow.
	if rel == relInvestsIn &&
		sourceClass == ah.FragmentClassMarket && targetClass == ah.FragmentClassOrg {
		return relParticipatesIn, true, true,
			"canonicalize: MARKET INVESTS_IN ORG -> ORG PARTICIPATES_IN MARKET (swap + rewrite)"
	}

	// RELATED_TO between an org and a market is canonically PARTICIPATES_IN.
	// Direction is canonicalised so org sits on the source side.
	if rel == relRelatedTo {
		switch {
		case sourceClass == ah.FragmentClassOrg && targetClass == ah.FragmentClassMarket:
			return relParticipatesIn, false, true,
				"canonicalize: ORG RELATED_TO MARKET -> ORG PARTICIPATES_IN MARKET"
		case sourceClass == ah.FragmentClassMarket && targetClass == ah.FragmentClassOrg:
			return relParticipatesIn, true, true,
				"canonicalize: MARKET RELATED_TO ORG -> ORG PARTICIPATES_IN MARKET (swap)"
		}
	}

	// OWNS into a dynasty/tribe community: COMMUNITY OWNS ORG, not the reverse.
	if rel == relOwns &&
		sourceClass == ah.FragmentClassOrg && targetClass == ah.FragmentClassCommunity &&
		isDynastyOrTribeCommunity(tgt) {
		return rel, true, true,
			"canonicalize: ORGANIZATION OWNS COMMUNITY(dynasty/tribe) -> COMMUNITY(dynasty/tribe) OWNS ORGANIZATION"
	}

	// HAS_MARKET_SEGMENT: parent should be source.
	if rel == relHasMarketSegment &&
		sourceClass == ah.FragmentClassMarket && targetClass == ah.FragmentClassMarket &&
		src != "" && tgt != "" {
		segSrc := pathSegmentCount(src)
		segTgt := pathSegmentCount(tgt)
		if segSrc > segTgt {
			return rel, true, true,
				"canonicalize: HAS_MARKET_SEGMENT child→parent -> parent→child (by segment count)"
		}
		tgtPrefix := strings.TrimSuffix(tgt, "/") + "/"
		if tgtPrefix != "/" && strings.HasPrefix(src, tgtPrefix) {
			return rel, true, true,
				"canonicalize: HAS_MARKET_SEGMENT child→parent -> parent→child (by path prefix)"
		}
	}

	return rel, false, false, ""
}

// CanonicalizationPenalty returns a confidence penalty for "weak" canonical
// rewrites where the target type is correct but the underlying signal was
// thin. Mirrors the legacy models.CanonicalizationPenalty function.
func CanonicalizationPenalty(
	oldRel, newRel ah.RelationName,
	sourceClass, targetClass ah.FragmentClass,
) float64 {
	switch {
	case oldRel == relProduces &&
		newRel == relSuppliesTo &&
		sourceClass == ah.FragmentClassOrg &&
		targetClass == ah.FragmentClassOrg:
		return 0.10
	case oldRel == relInvestsIn &&
		newRel == relOperatesIn &&
		sourceClass == ah.FragmentClassOrg &&
		targetClass == ah.FragmentClassSite:
		return 0.10
	default:
		return 0
	}
}

// =============================================================================
// EVIDENCE / OWNERSHIP HEURISTIC
// =============================================================================

// HasOwnershipEvidence reports whether the supplied reason / qualifiers /
// metrics carry ownership semantics (legal stake, voting rights, control
// language). Ported verbatim from the deleted models.HasOwnershipEvidence.
func HasOwnershipEvidence(
	reason string,
	qualifiers map[string]interface{},
	metrics map[string]float64,
) bool {
	ownershipMetricKeys := []string{
		"ownership_percent", "voting_percent", "voting_rights_percent",
		"equity_share", "stake_percent", "controlling_interest",
	}
	for _, k := range ownershipMetricKeys {
		if v, ok := metrics[k]; ok && v > 0 {
			return true
		}
	}

	ownershipQualifierKeys := []string{
		"ownership_percent", "stake_pct", "voting_percent",
		"voting_rights_percent", "equity_share", "stake_percent",
		"share_type", "control_type", "mechanism",
		"ownershipEvidence", "isOwner",
	}
	for _, k := range ownershipQualifierKeys {
		raw, ok := qualifiers[k]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			if v {
				return true
			}
		case float64:
			if v > 0 {
				return true
			}
		case int:
			if v > 0 {
				return true
			}
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "" {
				continue
			}
			switch s {
			case "true", "yes", "owner", "direct", "indirect", "beneficial",
				"equity", "board", "golden_share", "veto", "trustee", "state",
				"contractual", "board_majority", "veto_right",
				"trustee_mandate", "nominee_structure":
				return true
			}
		}
	}

	text := strings.ToLower(strings.TrimSpace(reason))
	if text == "" {
		return false
	}
	ownershipTerms := []string{
		"owns", "owned", "ownership", "stake", "equity",
		"acquire", "acquired", "subsidiary", "majority share",
		"controlling interest", "beneficial owner",
		"ultimate beneficial owner", "nominee", "trustee",
	}
	for _, term := range ownershipTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

// =============================================================================
// CLOSED-ENUM VALIDATORS (mechanism category, control_access type, condition type)
// =============================================================================
//
// These were `IsValidMechanismCategory`, `IsValidControlAccessType` and
// `IsValidConditionType` in the deleted models package. They're consumed by
// the policy checker for strategic-edge qualifier validation.

var validMechanismCategories = map[string]bool{
	"regulatory":    true,
	"economic":      true,
	"technological": true,
	"contractual":   true,
	"political":     true,
}

func IsValidMechanismCategory(category string) bool {
	return validMechanismCategories[strings.ToLower(strings.TrimSpace(category))]
}

var validControlAccessTypes = map[string]bool{
	"block":   true,
	"enable":  true,
	"delay":   true,
	"monitor": true,
}

func IsValidControlAccessType(t string) bool {
	return validControlAccessTypes[strings.ToLower(strings.TrimSpace(t))]
}

var validConditionTypes = map[string]bool{
	"cost":       true,
	"compliance": true,
	"technical":  true,
}

func IsValidConditionType(t string) bool {
	return validConditionTypes[strings.ToLower(strings.TrimSpace(t))]
}

// EdgeDirectionRule is re-exported via validation_policy.go from edgesemantics.
// keep import alive for forward-compat helpers below.
var _ = edgesemantics.EdgeDirectionRule{}
