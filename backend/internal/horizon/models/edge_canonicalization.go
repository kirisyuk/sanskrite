package models

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

	return edgeType, false, false, ""
}


