package models

import (
	"strings"
)

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
	// PRODUCT OWNS ORGANIZATION is almost always a modeling error (product doesn't own a company).
	// Canonical intent: ORGANIZATION PRODUCES PRODUCT.
	//
	// Only apply in this direction (PRODUCT -> ORG), NOT the reverse.
	if edgeType == DependencyTypeOwns &&
		sourceType == FragmentTypeProduct &&
		targetType == FragmentTypeOrganization {
		return DependencyTypeProduces, true, true, "canonicalize: PRODUCT OWNS ORGANIZATION -> ORGANIZATION PRODUCES PRODUCT"
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
