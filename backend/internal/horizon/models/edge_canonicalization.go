package models

import (
	"strings"
)

// =============================================================================
// EDGE UTILITY FUNCTIONS
// =============================================================================
// These functions are shared across packages (scout, policy, repo) to avoid
// circular imports.

// SymmetricEdgeTypes defines edge types where the relationship is reciprocal.
// For these types, we normalize source/target to ensure consistent representation.
var SymmetricEdgeTypes = map[DependencyType]bool{
	DependencyTypeCompetesWith: true, // "A competes with B" = "B competes with A"
	DependencyTypePartnersWith: true, // "A partners with B" = "B partners with A"
	DependencyTypeRelativeOf:   true, // "A is relative of B" = "B is relative of A"
	DependencyTypeRelatedTo:    true, // "A is related to B" = "B is related to A"
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

// CanonicalizeEdge rewrites an edge type (and optionally direction) based on endpoint fragment types.
// Returns: (newType, shouldSwapEndpoints, changed, reason).
//
// Keep this conservative and explicit. This is used at creation time and during cleanup.
func CanonicalizeEdge(edgeType DependencyType, sourceType, targetType FragmentType) (DependencyType, bool, bool, string) {
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

	// OWNS(MARKET → MARKET) → HAS_SUBMARKET
	// LLM often says "beverages owns beer market" meaning hierarchy, not ownership.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeMarket {
		return DependencyTypeHasSubmarket, false, true, "canonicalize: MARKET OWNS MARKET -> MARKET HAS_SUBMARKET MARKET"
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

	// PRODUCES(MARKET → PRODUCT) → PARTICIPATES_IN (swap)
	// A market doesn't "produce" a product; this usually means the product participates in that market.
	if edgeType == DependencyTypeProduces &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET PRODUCES PRODUCT -> PRODUCT PARTICIPATES_IN MARKET"
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

	// SUPPLIES_TO(PRODUCT → PRODUCT) → TRANSFORMS_INTO
	// If one product supplies to another, it's typically an upstream→downstream transformation/value-add step.
	if edgeType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeProduct {
		return DependencyTypeTransformsInto, false, true, "canonicalize: PRODUCT SUPPLIES_TO PRODUCT -> PRODUCT TRANSFORMS_INTO PRODUCT"
	}

	// SUPPLIES_TO(PRODUCT → MARKET) → PARTICIPATES_IN
	// If a product supplies to a market, canonical intent is market participation/coverage.
	if edgeType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeMarket {
		return DependencyTypeParticipatesIn, false, true, "canonicalize: PRODUCT SUPPLIES_TO MARKET -> PRODUCT PARTICIPATES_IN MARKET"
	}

	// OPERATES_IN(GEO → ENTITY) is reversed; canonicalize to ENTITY LOCATED_IN GEO (swap).
	// Common LLM error: "Las Vegas operates in Switch Inc" (city → org).
	if edgeType == DependencyTypeOperatesIn &&
		sourceType == FragmentTypeGeoRegion &&
		(targetType == FragmentTypeOrganization || targetType == FragmentTypeFacility || targetType == FragmentTypePerson || targetType == FragmentTypeProduct) {
		return DependencyTypeLocatedIn, true, true, "canonicalize: GEO OPERATES_IN ENTITY -> ENTITY LOCATED_IN GEO"
	}

	// PARTICIPATES_IN(PRODUCT → MARKET) → DEPENDS_ON (swap)
	// Market depends on the product category/value chain, not vice versa.
	// NOTE: per policy, allow PRODUCT PARTICIPATES_IN MARKET (no canonicalization).
	if edgeType == DependencyTypeParticipatesIn &&
		sourceType == FragmentTypeMarket &&
		targetType == FragmentTypeProduct {
		return DependencyTypeParticipatesIn, true, true, "canonicalize: MARKET PARTICIPATES_IN PRODUCT -> PRODUCT PARTICIPATES_IN MARKET"
	}

	// SUPPLIES_TO(PRODUCT → ORGANIZATION) → CONSUMES (swap)
	if edgeType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeConsumes, true, true, "canonicalize: PRODUCT SUPPLIES_TO ORGANIZATION -> ORGANIZATION CONSUMES PRODUCT"
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

	return edgeType, false, false, ""
}

// CanonicalizeEdgeWithPaths rewrites an edge type (and optionally direction) based on endpoint fragment types and canonical paths.
// This extends CanonicalizeEdge with path-based checks (e.g., for community subtypes).
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdgeWithPaths(edgeType DependencyType, sourceType, targetType FragmentType, sourcePath, targetPath string) (DependencyType, bool, bool, string) {
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

	// Additional path-based canonicalization: OWNS from ORG to COMMUNITY (dynasty/tribe) should be reversed.
	// Dynasties and tribes own organizations, not the reverse.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeCommunity &&
		isDynastyOrTribeCommunity(targetPath) {
		// Reverse the edge: COMMUNITY OWNS ORGANIZATION
		return edgeType, true, true, "canonicalize: ORGANIZATION OWNS COMMUNITY(dynasty/tribe) -> COMMUNITY(dynasty/tribe) OWNS ORGANIZATION"
	}

	return edgeType, shouldSwap, changed, reason
}
