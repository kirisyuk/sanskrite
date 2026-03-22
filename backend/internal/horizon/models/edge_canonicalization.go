package models

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// =============================================================================
// EDGE UTILITY FUNCTIONS
// =============================================================================
// These functions are shared across packages (scout, policy, repo) to avoid
// circular imports.

// SymmetricEdgeTypes defines edge types where the relationship is reciprocal (canonical types per TZ §6.1).
var SymmetricEdgeTypes = map[DependencyType]bool{
	DependencyTypeCompetesWith:   true,
	DependencyTypeRelativeOf:     true,
	DependencyTypeAssociatedWith: true, // canonical for RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH
	DependencyTypeDomainRelation: true, // canonical for domain↔domain
}

// IsSelfLoop returns true if the edge points to itself.
func IsSelfLoop(sourceID, targetID string) bool {
	return strings.TrimSpace(sourceID) == strings.TrimSpace(targetID)
}

// NormalizeSymmetricEdge ensures consistent source/target ordering for symmetric edges.
// Returns: (normalizedSourceID, normalizedTargetID, edgeType, wasSwapped)
func NormalizeSymmetricEdge(sourceID, targetID string, edgeType DependencyType) (string, string, DependencyType, bool) {
	if !SymmetricEdgeTypes[edgeType] {
		return sourceID, targetID, edgeType, false
	}

	// Normalize by placing smaller ID as source
	if strings.Compare(sourceID, targetID) > 0 {
		return targetID, sourceID, edgeType, true
	}
	return sourceID, targetID, edgeType, false
}

// IsSymmetricEdge returns true if the edge type is symmetric.
func IsSymmetricEdge(edgeType DependencyType) bool {
	return SymmetricEdgeTypes[edgeType]
}

// IsProductTransitionEdgeType reports whether the edge type is a product->product
// upstream/downstream step used in supply-chain continuity checks.
func IsProductTransitionEdgeType(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyTypeTransformsInto, DependencyTypeAssemblesInto:
		return true
	default:
		return false
	}
}

// ProductToProductEdgeTypeFromPaths selects the canonical product->product edge type.
// Assembly is reserved for nearest-step component/subassembly -> assembled product links.
// Everything else stays TRANSFORMS_INTO.
func ProductToProductEdgeTypeFromPaths(sourcePath, targetPath string) DependencyType {
	if shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath) {
		return DependencyTypeAssemblesInto
	}
	return DependencyTypeTransformsInto
}

func shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath string) bool {
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
	sourceLooksSubassembly := srcCategory == "component" || srcStage == "assembly" || srcStage == "manufacturing" || srcCategory == "finished"
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

// isDynastyOrTribeCommunity checks if a canonicalPath represents a dynasty or tribe community.
// Returns true if path matches /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}.
func isDynastyOrTribeCommunity(canonicalPath string) bool {
	if canonicalPath == "" {
		return false
	}
	if !strings.HasPrefix(canonicalPath, "/community/") {
		return false
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) < 3 {
		return false
	}
	// parts[0] = "community", parts[1] = geo, parts[2] = type
	communityType := strings.ToLower(strings.TrimSpace(parts[2]))
	return communityType == "dynasty" || communityType == "tribe"
}

// isGovMarketRegulatorEdge returns true when a /gov/ entity uses a regulatory
// edge type towards a MARKET, which should NOT be rewritten to PARTICIPATES_IN.
func isGovMarketRegulatorEdge(edgeType DependencyType, sourceType, targetType FragmentType, sourcePath, targetPath string) bool {
	govPath := ""
	if sourceType == FragmentTypeOrganization && targetType == FragmentTypeMarket {
		govPath = sourcePath
	} else if sourceType == FragmentTypeMarket && targetType == FragmentTypeOrganization {
		govPath = targetPath
	}
	if !strings.HasPrefix(strings.TrimSpace(govPath), "/gov/") {
		return false
	}
	switch edgeType {
	case DependencyTypeRegulates, DependencyTypeControlsAccess, DependencyTypeInfluences, DependencyTypeAdministers:
		return true
	}
	return false
}

func canonicalizeOrgMarketToParticipates(
	edgeType DependencyType,
	sourceType, targetType FragmentType,
) (DependencyType, bool, bool, string) {
	if edgeType == DependencyTypeParticipatesIn {
		return edgeType, false, false, ""
	}
	if sourceType == FragmentTypeOrganization && targetType == FragmentTypeMarket {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: ORGANIZATION * MARKET -> ORGANIZATION PARTICIPATES_IN MARKET"
	}
	if sourceType == FragmentTypeMarket && targetType == FragmentTypeOrganization {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET * ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET"
	}
	return edgeType, false, false, ""
}

func canonicalDomainAnchorFallbackType(targetType FragmentType) DependencyType {
	if targetType == FragmentTypeMechanism || targetType == FragmentTypeDecisionPoint {
		return DependencyTypeSurfaces
	}
	return DependencyTypeFocusesOn
}

func isDomainRelationTypeCompatibleWithTarget(edgeType DependencyType, targetType FragmentType) bool {
	switch edgeType {
	case DependencyTypeFocusesOn:
		switch targetType {
		case FragmentTypeDomain, FragmentTypeOrganization, FragmentTypePerson, FragmentTypeMarket,
			FragmentTypeProduct, FragmentTypeProgram, FragmentTypeFacility, FragmentTypeGeoRegion,
			FragmentTypeAffectedZone, FragmentTypeCommunity, FragmentTypeMediaChannel, FragmentTypeDeposit:
			return true
		}
	case DependencyTypeInvolvesActor:
		return targetType == FragmentTypeOrganization || targetType == FragmentTypePerson || targetType == FragmentTypeCommunity
	case DependencyTypeInGeoScope:
		return targetType == FragmentTypeGeoRegion
	case DependencyTypeAbout:
		switch targetType {
		case FragmentTypeDomain, FragmentTypeOrganization, FragmentTypePerson, FragmentTypeMarket,
			FragmentTypeProduct, FragmentTypeProgram, FragmentTypeFacility, FragmentTypeGeoRegion,
			FragmentTypeCommunity, FragmentTypeMediaChannel, FragmentTypeDeposit:
			return true
		}
	case DependencyTypeRelatedDomain, DependencyTypeDomainRelation, DependencyTypeBroadensTo, DependencyTypeNarrowsTo:
		return targetType == FragmentTypeDomain
	case DependencyTypeAffects:
		switch targetType {
		case FragmentTypeDomain, FragmentTypeFacility, FragmentTypeOrganization, FragmentTypeGeoRegion,
			FragmentTypeProduct, FragmentTypeProgram:
			return true
		}
	case DependencyTypeSurfaces:
		return targetType == FragmentTypeDecisionPoint || targetType == FragmentTypeMechanism
	case DependencyTypeDependsOn:
		switch targetType {
		case FragmentTypeDomain, FragmentTypeOrganization, FragmentTypePerson, FragmentTypeMarket,
			FragmentTypeProgram, FragmentTypeFacility, FragmentTypeProduct, FragmentTypeGeoRegion,
			FragmentTypeMediaChannel, FragmentTypeCommunity, FragmentTypeDeposit:
			return true
		}
	}
	return false
}

// CanonicalizeEdge rewrites an edge type (and optionally direction) based on endpoint fragment types.
// Returns: (newType, shouldSwapEndpoints, changed, reason).
//
// Keep this conservative and explicit. This is used at creation time and during cleanup.
func CanonicalizeEdge(edgeType DependencyType, sourceType, targetType FragmentType) (DependencyType, bool, bool, string) {
	// HAS_SUBMARKET (deprecated) → HAS_MARKET_SEGMENT
	if edgeType == DependencyTypeHasSubmarket {
		edgeType = DependencyTypeHasMarketSegment
	}
	// Transition types → canonical CORPORATE_TRANSITION first (TZ §6.1).
	if IsTransitionEdgeType(edgeType) && edgeType != DependencyTypeCorporateTransition {
		edgeType = DependencyTypeCorporateTransition
	}
	// Corporate transition edges are only valid for organization-to-organization links.
	if IsTransitionEdgeType(edgeType) {
		if sourceType == FragmentTypeOrganization && targetType == FragmentTypeOrganization {
			return edgeType, false, false, ""
		}
		return DependencyTypeInfluences, false, true, "canonicalize: non-org transition edge -> INFLUENCES"
	}

	// =============================================================================
	// DOMAIN-related canonicalization (TZ §6.1: domain↔domain only FOCUSES_ON and DOMAIN_RELATION)
	// =============================================================================
	if sourceType == FragmentTypeDomain && targetType == FragmentTypeDomain {
		switch edgeType {
		case DependencyTypeFocusesOn, DependencyTypeDomainRelation:
			return edgeType, false, false, ""
		case DependencyTypeRelatedDomain, DependencyTypeBroadensTo, DependencyTypeNarrowsTo, DependencyTypeAffects:
			return DependencyTypeDomainRelation, false, true, "canonicalize: DOMAIN↔DOMAIN -> DOMAIN_RELATION"
		case DependencyTypeDependsOn:
			return DependencyTypeDomainRelation, false, true, "canonicalize: DOMAIN DEPENDS_ON DOMAIN -> DOMAIN_RELATION"
		default:
			return DependencyTypeFocusesOn, false, true, "canonicalize: DOMAIN→DOMAIN unknown -> FOCUSES_ON"
		}
	}

	// Domain → non-domain (entity): preserve valid domain-scaffold relation semantics.
	if sourceType == FragmentTypeDomain && targetType != FragmentTypeDomain {
		if isDomainRelationTypeCompatibleWithTarget(edgeType, targetType) {
			return edgeType, false, false, ""
		}
		fallback := canonicalDomainAnchorFallbackType(targetType)
		if fallback == DependencyTypeSurfaces {
			return fallback, false, true, "canonicalize: domain→strategic-target -> SURFACES"
		}
		return fallback, false, true, "canonicalize: domain→entity incompatible -> FOCUSES_ON"
	}

	// AFFILIATED_WITH(PERSON → COMMUNITY) → MEMBER_OF (canonical membership).
	if edgeType == DependencyTypeAffiliatedWith &&
		sourceType == FragmentTypePerson &&
		targetType == FragmentTypeCommunity {
		return DependencyTypeMemberOf, false, true, "canonicalize: PERSON AFFILIATED_WITH COMMUNITY -> MEMBER_OF"
	}
	// AFFILIATED_WITH(ORGANIZATION → COMMUNITY/PERSON) → swap to COMMUNITY/PERSON AFFILIATED_WITH ORGANIZATION.
	// Canonical: community/person is affiliated with org, not the reverse. Must run before generic AFFILIATED_WITH→ASSOCIATED_WITH.
	if edgeType == DependencyTypeAffiliatedWith &&
		sourceType == FragmentTypeOrganization &&
		(targetType == FragmentTypeCommunity || targetType == FragmentTypePerson) {
		return DependencyTypeAffiliatedWith, true, true, "canonicalize: ORGANIZATION AFFILIATED_WITH COMMUNITY/PERSON -> COMMUNITY/PERSON AFFILIATED_WITH ORGANIZATION"
	}
	// AFFILIATED_WITH(COMMUNITY/PERSON → ORGANIZATION) is canonical and must be preserved.
	if edgeType == DependencyTypeAffiliatedWith &&
		(sourceType == FragmentTypeCommunity || sourceType == FragmentTypePerson) &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeAffiliatedWith, false, false, ""
	}
	// RELATED_TO(ENTITY ↔ MARKET) → PARTICIPATES_IN (must run before generic RELATED_TO→ASSOCIATED_WITH).
	if edgeType == DependencyTypeRelatedTo &&
		(sourceType == FragmentTypeOrganization || sourceType == FragmentTypeFacility || sourceType == FragmentTypeCommunity || sourceType == FragmentTypeProgram) &&
		targetType == FragmentTypeMarket {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: ENTITY RELATED_TO MARKET -> ENTITY PARTICIPATES_IN MARKET"
	}
	if edgeType == DependencyTypeRelatedTo &&
		sourceType == FragmentTypeMarket &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility || targetType == FragmentTypeCommunity || targetType == FragmentTypeProgram) {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET RELATED_TO ENTITY -> ENTITY PARTICIPATES_IN MARKET"
	}
	// RELATED_TO / AFFILIATED_WITH / PARTNERS_WITH → ASSOCIATED_WITH (canonical).
	switch edgeType {
	case DependencyTypeRelatedTo, DependencyTypeAffiliatedWith, DependencyTypePartnersWith:
		return DependencyTypeAssociatedWith, false, true, "canonicalize: relation -> ASSOCIATED_WITH"
	}

	// DOMINATES → PARTICIPATES_IN (with marketRole/market_share in qualifiers/metrics).
	if edgeType == DependencyTypeDominates {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: DOMINATES -> PARTICIPATES_IN"
	}

	// WORKS_FOR / KEY_PERSON / HAS_KEY_PERSON → EMPLOYED_BY (canonical employment).
	switch edgeType {
	case DependencyTypeWorksFor, DependencyTypeKeyPerson, DependencyTypeHasKeyPerson:
		return DependencyTypeEmployedBy, false, true, "canonicalize: employment/key person -> EMPLOYED_BY"
	}

	// ORG OWNS PERSON is invalid: ownership should flow from PERSON to ORG.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypePerson {
		return DependencyTypeOwns, true, true, "canonicalize: ORGANIZATION OWNS PERSON -> PERSON OWNS ORGANIZATION"
	}

	// PRODUCT OWNS ORGANIZATION is almost always a modeling error (product doesn't own a company).
	// Canonical intent: ORGANIZATION PRODUCES PRODUCT.
	//
	// Only apply in this direction (PRODUCT -> ORG), NOT the reverse.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeProduces, true, true, "canonicalize: PRODUCT OWNS ORGANIZATION -> ORGANIZATION PRODUCES PRODUCT"
	}

	// OWNS(GEO_REGION → ORGANIZATION) → LOCATED_IN (swap)
	// A location doesn't "own" an organization; best-effort intent is that the org is located in that geo.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeGeoRegion &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeLocatedIn, true, true, "canonicalize: GEO_REGION OWNS ORGANIZATION -> ORGANIZATION LOCATED_IN GEO_REGION"
	}

	// OWNS(COMMUNITY → ORGANIZATION) usually indicates informal control, not legal ownership.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeCommunity &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeControls, false, true, "canonicalize: COMMUNITY OWNS ORGANIZATION -> COMMUNITY CONTROLS ORGANIZATION"
	}

	// EXTRACTS_FROM is strict by ontology: FACILITY -> DEPOSIT.
	// However, in practice LLMs often emit ORG -> DEPOSIT when the specific facility is unknown.
	// Best-effort canonicalization: treat it as ORG OPERATES_IN DEPOSIT (no loss of meaning, keeps graph usable).
	if edgeType == DependencyTypeExtractsFrom &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeDeposit {
		return DependencyTypeOperatesIn, false, true, "canonicalize: ORGANIZATION EXTRACTS_FROM DEPOSIT -> ORGANIZATION OPERATES_IN DEPOSIT"
	}
	if edgeType == DependencyTypeExtractsFrom &&
		sourceType == FragmentTypeDeposit &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeOperatesIn, true, true, "canonicalize: DEPOSIT EXTRACTS_FROM ORGANIZATION -> ORGANIZATION OPERATES_IN DEPOSIT"
	}

	// =============================================================================
	// MARKET-related canonicalization
	// =============================================================================

	// APPLIES_TO(ENTITY → MECHANISM) is inverted; canonical direction is MECHANISM → ENTITY.
	// Keep this before generic ORG↔MARKET normalization so APPLIES_TO does not collapse into PARTICIPATES_IN.
	if edgeType == DependencyTypeAppliesTo &&
		sourceType != FragmentTypeMechanism &&
		targetType == FragmentTypeMechanism {
		return DependencyTypeAppliesTo, true, true, "canonicalize: ENTITY APPLIES_TO MECHANISM -> MECHANISM APPLIES_TO ENTITY"
	}

	// APPLIES_TO with non-mechanism source is usually a wrong relation label.
	// Best-effort fallback: recast as INFLUENCES to preserve weak effect semantics.
	// This must run before generic ORG↔MARKET normalization.
	if edgeType == DependencyTypeAppliesTo &&
		sourceType != FragmentTypeMechanism &&
		targetType != FragmentTypeMechanism {
		return DependencyTypeInfluences, false, true, "canonicalize: non-mechanism APPLIES_TO -> INFLUENCES"
	}

	// ORG↔MARKET canonical contract: use only PARTICIPATES_IN in ORG -> MARKET direction.
	// NOTE: CanonicalizeEdge has no path info; GOV exemption is applied in CanonicalizeEdgeWithPaths.
	if newType, shouldSwap, changed, reason := canonicalizeOrgMarketToParticipates(edgeType, sourceType, targetType); changed {
		return newType, shouldSwap, changed, reason
	}

	// OWNS(MARKET → MARKET) → HAS_MARKET_SEGMENT
	// LLM often says "beverages owns beer market" meaning hierarchy, not ownership.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeMarket {
		return DependencyTypeHasMarketSegment, false, true, "canonicalize: MARKET OWNS MARKET -> MARKET HAS_MARKET_SEGMENT MARKET"
	}

	// DEPENDS(ORG → MARKET) → PARTICIPATES_IN
	// LLM interprets "company depends on market" (revenue exposure) but correct semantic is participation.
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeMarket {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: ORGANIZATION DEPENDS_ON MARKET -> ORGANIZATION PARTICIPATES_IN MARKET"
	}

	// DEPENDS(FACILITY → MARKET) → PARTICIPATES_IN
	// Same logic for facilities.
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeFacility &&
		targetType == FragmentTypeMarket {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: FACILITY DEPENDS_ON MARKET -> FACILITY PARTICIPATES_IN MARKET"
	}
	// DEPENDS(MARKET → ORG/FACILITY) → PARTICIPATES_IN (swap)
	// If the edge is reversed, flip it to ORG/FACILITY -> MARKET.
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET DEPENDS_ON ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET"
	}
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeFacility {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET DEPENDS_ON FACILITY -> FACILITY PARTICIPATES_IN MARKET"
	}
	// DEPENDS(MARKET → PRODUCT) → TRADED_IN (swap)
	// Market "depends on" product category usually means the product is traded/price-discovered in that market.
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTradedIn, true, true, "canonicalize: MARKET DEPENDS_ON PRODUCT -> PRODUCT TRADED_IN MARKET"
	}

	// REGULATES(MARKET → ORGANIZATION) → REGULATES (swap)
	// Markets are regulated by authorities; if the edge is reversed, swap it.
	if edgeType == DependencyTypeRegulates &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeRegulates, true, true, "canonicalize: MARKET REGULATES ORGANIZATION -> ORGANIZATION REGULATES MARKET"
	}

	// PARTICIPATES_IN(MARKET → ORG/FAC/COMMUNITY) → PARTICIPATES_IN (swap)
	if edgeType == DependencyTypeParticipatesIn &&
		sourceType == FragmentTypeMarket &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility || targetType == FragmentTypeCommunity || targetType == FragmentTypeProgram) {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET PARTICIPATES_IN ENTITY -> ENTITY PARTICIPATES_IN MARKET"
	}

	// INVESTS_IN(MARKET → ORGANIZATION): Market does not "invest" in org; org participates in market.
	if edgeType == DependencyTypeInvestsIn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET"
	}

	// OPERATES_IN(MARKET → ORGANIZATION): market is not an operator; org participates in market.
	if edgeType == DependencyTypeOperatesIn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET OPERATES_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET"
	}

	// OWNS(ORG → PROGRAM) → ADMINISTERS
	// Organizations typically administer/operate programs rather than "own" them.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeProgram {
		return DependencyTypeAdministers, false, true, "canonicalize: ORGANIZATION OWNS PROGRAM -> ORGANIZATION ADMINISTERS PROGRAM"
	}

	// DOMINATES(MARKET → ORG/FAC/COMMUNITY) → DOMINATES (swap)
	if edgeType == DependencyTypeDominates &&
		sourceType == FragmentTypeMarket &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility || targetType == FragmentTypeCommunity) {
		return DependencyTypeDominates, true, true, "canonicalize: MARKET DOMINATES ENTITY -> ENTITY DOMINATES MARKET"
	}

	// PRODUCES(MARKET → PRODUCT) → TRADED_IN (swap)
	// A market doesn't "produce" a product; best-effort intent is that the product is traded in that market.
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTradedIn, true, true, "canonicalize: MARKET PRODUCES PRODUCT -> PRODUCT TRADED_IN MARKET"
	}

	// PRODUCES(MARKET → FACILITY) → PARTICIPATES_IN (swap)
	// A market doesn't "produce" a facility; best-effort intent is that the facility participates in that market.
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeFacility {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET PRODUCES FACILITY -> FACILITY PARTICIPATES_IN MARKET"
	}

	// LOCATED_IN(ORG → FACILITY) → OPERATES_IN
	// LOCATED_IN is reserved for GEO_REGION targets; facilities are concrete sites/parks/complexes.
	if edgeType == DependencyTypeLocatedIn &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeFacility {
		return DependencyTypeOperatesIn, false, true, "canonicalize: ORGANIZATION LOCATED_IN FACILITY -> ORGANIZATION OPERATES_IN FACILITY"
	}

	// LOCATED_IN(GEO_REGION → ENTITY) → swap to ENTITY LOCATED_IN GEO_REGION
	// Common LLM error: region "located in" community/org; canonical is entity located in region.
	if edgeType == DependencyTypeLocatedIn &&
		sourceType == FragmentTypeGeoRegion &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility ||
			targetType == FragmentTypeCommunity || targetType == FragmentTypePerson || targetType == FragmentTypeDeposit) {
		return DependencyTypeLocatedIn, true, true, "canonicalize: GEO_REGION LOCATED_IN ENTITY -> ENTITY LOCATED_IN GEO_REGION"
	}

	// SUPPLIES_TO(PRODUCT → PRODUCT) → TRANSFORMS_INTO
	// If one product supplies to another, it's typically an upstream→downstream transformation/value-add step.
	if edgeType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTransformsInto, false, true, "canonicalize: PRODUCT SUPPLIES_TO PRODUCT -> PRODUCT TRANSFORMS_INTO PRODUCT"
	}

	// PRODUCES(PRODUCT → PRODUCT) → TRANSFORMS_INTO
	// Product-to-product "produces" usually describes value-chain transformation.
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTransformsInto, false, true, "canonicalize: PRODUCT PRODUCES PRODUCT -> PRODUCT TRANSFORMS_INTO PRODUCT"
	}

	// SUPPLIES_TO(PRODUCT → MARKET) → TRADED_IN
	// If a product supplies to a market, canonical intent is market trading/price discovery.
	if edgeType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeMarket {
		return DependencyTypeTradedIn, false, true, "canonicalize: PRODUCT SUPPLIES_TO MARKET -> PRODUCT TRADED_IN MARKET"
	}

	// OPERATES_IN(GEO → ENTITY) is reversed; canonicalize to ENTITY LOCATED_IN GEO (swap).
	// Common LLM error: "Las Vegas operates in Switch Inc" (city → org).
	if edgeType == DependencyTypeOperatesIn &&
		sourceType == FragmentTypeGeoRegion &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility || targetType == FragmentTypePerson || targetType == FragmentTypeProduct) {
		return DependencyTypeLocatedIn, true, true, "canonicalize: GEO OPERATES_IN ENTITY -> ENTITY LOCATED_IN GEO"
	}

	// PARTICIPATES_IN(MARKET → PRODUCT) → TRADED_IN (swap)
	// Product is traded in the market; actors participate in markets.
	if edgeType == DependencyTypeParticipatesIn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTradedIn, true, true, "canonicalize: MARKET PARTICIPATES_IN PRODUCT -> PRODUCT TRADED_IN MARKET"
	}
	if edgeType == DependencyTypeParticipatesIn &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeMarket {
		return DependencyTypeTradedIn, false, true, "canonicalize: PRODUCT PARTICIPATES_IN MARKET -> PRODUCT TRADED_IN MARKET"
	}
	if edgeType == DependencyTypeRelatedTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeMarket {
		return DependencyTypeTradedIn, false, true, "canonicalize: PRODUCT RELATED_TO MARKET -> PRODUCT TRADED_IN MARKET"
	}
	if edgeType == DependencyTypeRelatedTo &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTradedIn, true, true, "canonicalize: MARKET RELATED_TO PRODUCT -> PRODUCT TRADED_IN MARKET"
	}
	if edgeType == DependencyTypeDependsOn &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeMarket {
		return DependencyTypeTradedIn, false, true, "canonicalize: PRODUCT DEPENDS_ON MARKET -> PRODUCT TRADED_IN MARKET"
	}

	// CONSUMES(actor→product) → SUPPLIES_TO (swap): legacy CONSUMES removed from taxonomy.
	// Canonical form: PRODUCT SUPPLIES_TO ORGANIZATION/FACILITY.
	if edgeType == DependencyTypeConsumes &&
		targetType == FragmentTypeProduct &&
		(sourceType == FragmentTypeOrganization || sourceType == FragmentTypeFacility || sourceType == FragmentTypeProduct) {
		return DependencyTypeSuppliesTo, true, true, "canonicalize: CONSUMES deprecated -> PRODUCT SUPPLIES_TO ORGANIZATION/FACILITY (swap)"
	}

	// PRODUCES(ORGANIZATION → ORGANIZATION) is frequently a mistyped supply relation.
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeSuppliesTo, false, true, "canonicalize: ORGANIZATION PRODUCES ORGANIZATION -> ORGANIZATION SUPPLIES_TO ORGANIZATION"
	}

	// MANAGES(PERSON → PERSON) is not formal management in current ontology.
	if edgeType == DependencyTypeManages &&
		sourceType == FragmentTypePerson &&
		targetType == FragmentTypePerson {
		return DependencyTypeRelatedTo, false, true, "canonicalize: PERSON MANAGES PERSON -> PERSON RELATED_TO PERSON"
	}

	// INVESTS_IN(ORGANIZATION → FACILITY) usually means operating/asset position.
	if edgeType == DependencyTypeInvestsIn &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeFacility {
		return DependencyTypeOperatesIn, false, true, "canonicalize: ORGANIZATION INVESTS_IN FACILITY -> ORGANIZATION OPERATES_IN FACILITY"
	}

	// OPERATES_IN(PERSON → GEO) → LOCATED_IN
	if edgeType == DependencyTypeOperatesIn &&
		sourceType == FragmentTypePerson &&
		targetType == FragmentTypeGeoRegion {
		return DependencyTypeLocatedIn, false, true, "canonicalize: PERSON OPERATES_IN GEO -> PERSON LOCATED_IN GEO"
	}

	// CONTROLS(GEO → ORG) → CONTROLS (swap)
	if edgeType == DependencyTypeControls &&
		sourceType == FragmentTypeGeoRegion &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeControls, true, true, "canonicalize: GEO CONTROLS ORGANIZATION -> ORGANIZATION CONTROLS GEO"
	}

	// FOUNDED(COMMUNITY → PERSON): Person founds community, not the reverse.
	// Swap to PERSON → COMMUNITY.
	if edgeType == DependencyTypeFounded &&
		sourceType == FragmentTypeCommunity &&
		targetType == FragmentTypePerson {
		return DependencyTypeFounded, true, true, "canonicalize: COMMUNITY FOUNDED PERSON -> PERSON FOUNDED COMMUNITY"
	}

	// FOUNDED(ORGANIZATION → PERSON): Person founds org/program, not the reverse.
	if edgeType == DependencyTypeFounded &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypePerson {
		return DependencyTypeFounded, true, true, "canonicalize: ORGANIZATION FOUNDED PERSON -> PERSON FOUNDED ORGANIZATION"
	}

	// ADMINISTERS: source MUST be ORGANIZATION/GEO_REGION. FACILITY cannot administer.
	// "Facility administered by org" → swap to ORG ADMINISTERS FACILITY.
	if edgeType == DependencyTypeAdministers &&
		sourceType == FragmentTypeFacility &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeProgram || targetType == FragmentTypeGeoRegion) {
		return DependencyTypeAdministers, true, true, "canonicalize: FACILITY ADMINISTERS ORG -> ORG ADMINISTERS FACILITY (swap only)"
	}
	// ADMINISTERS: DEPOSIT cannot administer. "Deposit administered by gov/org" → swap.
	if edgeType == DependencyTypeAdministers &&
		sourceType == FragmentTypeDeposit &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeProgram || targetType == FragmentTypeGeoRegion) {
		return DependencyTypeAdministers, true, true, "canonicalize: DEPOSIT ADMINISTERS ORG -> ORG ADMINISTERS DEPOSIT (swap only)"
	}
	// CONTROLS: source MUST be ORG/PERSON/COMMUNITY. PRODUCT cannot control.
	if edgeType == DependencyTypeControls &&
		sourceType == FragmentTypeProduct &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility) {
		return DependencyTypeControls, true, true, "canonicalize: PRODUCT CONTROLS ORG -> ORG CONTROLS PRODUCT (swap only)"
	}

	return edgeType, false, false, ""
}

// CanonicalizeEdgeWithPaths rewrites an edge type (and optionally direction) based on endpoint fragment types and canonical paths.
// This extends CanonicalizeEdge with path-based checks (e.g., for community subtypes, product-path retargeting).
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdgeWithPaths(edgeType DependencyType, sourceType, targetType FragmentType, sourcePath, targetPath string) (DependencyType, bool, bool, string) {
	// =============================================================================
	// Path-based canonicalization BEFORE standard type-based rules.
	// These intercept cases where the canonical path reveals the correct target type
	// that the fragment type metadata got wrong.
	// =============================================================================

	// ADMINISTERS: /org/ → /gov/ is backwards (gov bodies administer, not companies).
	if edgeType == DependencyTypeAdministers &&
		strings.HasPrefix(strings.TrimSpace(sourcePath), "/org/") &&
		strings.HasPrefix(strings.TrimSpace(targetPath), "/gov/") {
		return DependencyTypeAdministers, true, true, "canonicalize: ADMINISTERS /org -> /gov swapped to /gov -> /org"
	}

	// OWNS: /org/ → /gov/ is backwards (gov owns state-owned enterprises, not vice versa).
	if edgeType == DependencyTypeOwns &&
		strings.HasPrefix(strings.TrimSpace(sourcePath), "/org/") &&
		strings.HasPrefix(strings.TrimSpace(targetPath), "/gov/") {
		return DependencyTypeOwns, true, true, "canonicalize: OWNS /org -> /gov swapped to /gov -> /org (gov owns state-owned enterprises)"
	}

	// CONTROLS: /product/ → /gov/ or /org/ is backwards (gov/org controls product).
	if edgeType == DependencyTypeControls &&
		strings.HasPrefix(strings.TrimSpace(sourcePath), "/product/") &&
		(strings.HasPrefix(strings.TrimSpace(targetPath), "/gov/") || strings.HasPrefix(strings.TrimSpace(targetPath), "/org/")) {
		return DependencyTypeControls, true, true, "canonicalize: CONTROLS /product/ -> /gov/|/org/ swapped to /gov/|/org/ -> /product/"
	}

	// PRODUCES(ORG → ORG) where target path starts with /product/:
	// The target is actually a PRODUCT mistyped as ORGANIZATION.
	// Fix: keep PRODUCES, signal that targetType should be corrected to PRODUCT.
	// This is more accurate than the generic ORG→ORG fallback (→ SUPPLIES_TO).
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeOrganization &&
		strings.HasPrefix(strings.TrimSpace(targetPath), "/product/") {
		// Edge type stays PRODUCES (ORG → PRODUCT is valid).
		// The caller should reconcile targetType to PRODUCT based on path.
		return DependencyTypeProduces, false, true, "canonicalize: ORGANIZATION PRODUCES ORGANIZATION(path=/product/...) -> keep PRODUCES, target is mistyped PRODUCT"
	}

	// GOV→MARKET exemption: if a /gov/ entity uses a regulatory edge type towards
	// a MARKET, preserve it as-is (do NOT let CanonicalizeEdge rewrite to PARTICIPATES_IN).
	if isGovMarketRegulatorEdge(edgeType, sourceType, targetType, sourcePath, targetPath) {
		return edgeType, false, false, ""
	}

	// First apply standard canonicalization
	newType, shouldSwap, changed, reason := CanonicalizeEdge(edgeType, sourceType, targetType)
	if changed {
		// If standard canonicalization changed the edge, use those values
		// but we may need to adjust paths if swap occurred
		if shouldSwap {
			sourcePath, targetPath = targetPath, sourcePath
			sourceType, targetType = targetType, sourceType
		}
		edgeType = newType
	}

	if edgeType == DependencyTypeTransformsInto &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeProduct &&
		sourcePath != "" && targetPath != "" &&
		ProductToProductEdgeTypeFromPaths(sourcePath, targetPath) == DependencyTypeAssemblesInto {
		return DependencyTypeAssemblesInto, shouldSwap, true, "canonicalize: PRODUCT transition promoted to ASSEMBLES_INTO by product category/stage"
	}

	// Additional path-based canonicalization: OWNS from ORG to COMMUNITY (dynasty/tribe) should be reversed.
	// Dynasties and tribes own organizations, not the reverse.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeCommunity &&
		isDynastyOrTribeCommunity(targetPath) {
		// Reverse the edge: COMMUNITY OWNS ORGANIZATION
		return edgeType, true, true, "canonicalize: ORGANIZATION OWNS COMMUNITY(dynasty/tribe) -> COMMUNITY(dynasty/tribe) OWNS ORGANIZATION"
	}

	// HAS_MARKET_SEGMENT: parent (broader market) → child (segment). Child path = parent path + one segment.
	// Swap when child is source so we never create bidirectional parent↔child edges.
	if edgeType == DependencyTypeHasMarketSegment &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeMarket &&
		sourcePath != "" && targetPath != "" {
		segSrc := pathSegmentCount(sourcePath)
		segTgt := pathSegmentCount(targetPath)
		// Source is child (more segments) and target is parent: swap to parent→child
		if segSrc > segTgt {
			return edgeType, true, true, "canonicalize: HAS_MARKET_SEGMENT child→parent -> parent→child (by segment count)"
		}
		// Target path is prefix of source: source is child, target is parent; swap
		tgtPrefix := strings.TrimSuffix(strings.TrimSpace(targetPath), "/") + "/"
		if tgtPrefix != "/" && strings.HasPrefix(strings.TrimSpace(sourcePath), tgtPrefix) {
			return edgeType, true, true, "canonicalize: HAS_MARKET_SEGMENT child→parent -> parent→child (by path prefix)"
		}
		// Source path is prefix of target: correct direction, no swap
	}

	// Product transition edges: upstream product stage must point to downstream stage.
	// Example: extraction -> processing (never the reverse).
	if IsProductTransitionEdgeType(edgeType) &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeProduct &&
		sourcePath != "" && targetPath != "" {
		srcStageRank, srcOk := ProductStageRankFromCanonicalPath(sourcePath)
		tgtStageRank, tgtOk := ProductStageRankFromCanonicalPath(targetPath)
		if srcOk && tgtOk && srcStageRank > tgtStageRank {
			return edgeType, !shouldSwap, true, fmt.Sprintf("canonicalize: %s downstream->upstream -> upstream->downstream (by product stage order)", edgeType)
		}
	}

	return edgeType, shouldSwap, changed, reason
}

func pathSegmentCount(path string) int {
	p := strings.Trim(path, "/")
	if p == "" {
		return 0
	}
	return len(strings.Split(p, "/"))
}

// ProductStageRankFromCanonicalPath returns the stage-order rank for a product canonical path.
// Single source of truth for stage-rank comparison (used by canonicalization policy and supply chain parser).
func ProductStageRankFromCanonicalPath(path string) (int, bool) {
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

// MarketSegmentPathsFollowRule returns true if parentPath and childPath follow the rule:
// child path = parent path + exactly one segment. Best-effort: also accepts prefix match
// when segment count differs by 1 (legacy paths may not be strictly +1).
func MarketSegmentPathsFollowRule(parentPath, childPath string) bool {
	parentPath = strings.TrimSpace(strings.Trim(parentPath, "/"))
	childPath = strings.TrimSpace(strings.Trim(childPath, "/"))
	if parentPath == "" || childPath == "" {
		return true // no path info, cannot validate
	}
	// Child must start with parent prefix
	prefix := parentPath + "/"
	if !strings.HasPrefix(childPath, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(childPath, prefix)
	if suffix == "" {
		return false // child equals parent
	}
	// Ideal: exactly one extra segment (no nested slashes in suffix)
	if !strings.Contains(suffix, "/") {
		return true
	}
	// Best-effort: allow multiple segments (e.g. /market/sa/finance -> /market/sa/finance/banking/retail)
	// as long as child is strictly longer
	return pathSegmentCount(childPath) > pathSegmentCount(parentPath)
}
