package models

import "testing"

func TestCanonicalizeEdge_MarketOperatesInOrganization(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeOperatesIn,
		FragmentTypeMarket,
		FragmentTypeOrganization,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_OrganizationMarketAlwaysParticipates(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeConsumes,
		FragmentTypeOrganization,
		FragmentTypeMarket,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_MarketOrganizationAlwaysParticipatesWithSwap(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeConsumes,
		FragmentTypeMarket,
		FragmentTypeOrganization,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_ProductMarketAlwaysTradedIn(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeDependsOn,
		FragmentTypeProduct,
		FragmentTypeMarket,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeTradedIn {
		t.Fatalf("expected TRADED_IN, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_MarketProductAlwaysTradedInWithSwap(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeParticipatesIn,
		FragmentTypeMarket,
		FragmentTypeProduct,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeTradedIn {
		t.Fatalf("expected TRADED_IN, got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_NonMechanismAppliesToMarket_ToInfluences(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeAppliesTo,
		FragmentTypeOrganization,
		FragmentTypeMarket,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeInfluences {
		t.Fatalf("expected INFLUENCES, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdgeWithPaths_TransformsIntoDownstreamToUpstream_Swaps(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(
		DependencyTypeTransformsInto,
		FragmentTypeProduct,
		FragmentTypeProduct,
		"/product/finished/mineral/processing/iron-pellet",
		"/product/raw/mineral/extraction/iron-ore",
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeTransformsInto {
		t.Fatalf("expected TRANSFORMS_INTO, got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdgeWithPaths_TransformsIntoUpstreamToDownstream_Stays(t *testing.T) {
	newType, shouldSwap, changed, _ := CanonicalizeEdgeWithPaths(
		DependencyTypeTransformsInto,
		FragmentTypeProduct,
		FragmentTypeProduct,
		"/product/raw/mineral/extraction/iron-ore",
		"/product/finished/mineral/processing/iron-pellet",
	)
	if changed {
		t.Fatalf("expected no canonicalization change")
	}
	if newType != DependencyTypeTransformsInto {
		t.Fatalf("expected TRANSFORMS_INTO, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestProductToProductEdgeTypeFromPaths_ComponentToFinished_PrefersAssemblesInto(t *testing.T) {
	edgeType := ProductToProductEdgeTypeFromPaths(
		"/product/component/mechanical/assembly/tire",
		"/product/finished/mechanical/assembly/electric-vehicle",
	)
	if edgeType != DependencyTypeAssemblesInto {
		t.Fatalf("expected ASSEMBLES_INTO, got %s", edgeType)
	}
}

func TestCanonicalizeEdgeWithPaths_TransformsIntoComponentToFinished_PromotesAssemblesInto(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(
		DependencyTypeTransformsInto,
		FragmentTypeProduct,
		FragmentTypeProduct,
		"/product/component/mechanical/assembly/tire",
		"/product/finished/mechanical/assembly/electric-vehicle",
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeAssemblesInto {
		t.Fatalf("expected ASSEMBLES_INTO, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_DepositAdministersOrg_SwapOnly(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeAdministers,
		FragmentTypeDeposit,
		FragmentTypeOrganization,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeAdministers {
		t.Fatalf("expected ADMINISTERS (unchanged), got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS DEPOSIT)")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdgeWithPaths_OrgAdministersGov_Swap(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(
		DependencyTypeAdministers,
		FragmentTypeOrganization,
		FragmentTypeOrganization,
		"/org/iq/state-company-for-phosphates-jsc",
		"/gov/iq/ministry-of-industry",
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeAdministers {
		t.Fatalf("expected ADMINISTERS (unchanged), got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true (/gov/ ADMINISTERS /org/)")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdgeWithPaths_OrgOwnsGov_Swap(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(
		DependencyTypeOwns,
		FragmentTypeOrganization,
		FragmentTypeOrganization,
		"/org/iq/agricultural-cooperative-bank",
		"/gov/iq/federal-government",
	)
	if !changed {
		t.Fatalf("expected canonicalization change for OWNS /org/ -> /gov/")
	}
	if newType != DependencyTypeOwns {
		t.Fatalf("expected OWNS (unchanged), got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true (/gov/ OWNS /org/)")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_FacilityAdministersOrg_SwapOnly(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeAdministers,
		FragmentTypeFacility,
		FragmentTypeOrganization,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeAdministers {
		t.Fatalf("expected ADMINISTERS (unchanged), got %s", newType)
	}
	if !shouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS FACILITY)")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_DomainToMechanismFallbacksToSurfaces(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeFocusesOn,
		FragmentTypeDomain,
		FragmentTypeMechanism,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeSurfaces {
		t.Fatalf("expected SURFACES, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_DomainToGeoInGeoScopePreserved(t *testing.T) {
	newType, shouldSwap, changed, _ := CanonicalizeEdge(
		DependencyTypeInGeoScope,
		FragmentTypeDomain,
		FragmentTypeGeoRegion,
	)
	if changed {
		t.Fatalf("expected no canonicalization change")
	}
	if newType != DependencyTypeInGeoScope {
		t.Fatalf("expected IN_GEO_SCOPE, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestCanonicalizeEdge_DomainToProductInGeoScopeFallbacksToFocusesOn(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeInGeoScope,
		FragmentTypeDomain,
		FragmentTypeProduct,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeFocusesOn {
		t.Fatalf("expected FOCUSES_ON, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_DomainToOrganizationAboutPreserved(t *testing.T) {
	newType, shouldSwap, changed, _ := CanonicalizeEdge(
		DependencyTypeAbout,
		FragmentTypeDomain,
		FragmentTypeOrganization,
	)
	if changed {
		t.Fatalf("expected no canonicalization change")
	}
	if newType != DependencyTypeAbout {
		t.Fatalf("expected ABOUT, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestCanonicalizeEdge_PersonAffiliatedWithCommunity_BecomesMemberOf(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeAffiliatedWith,
		FragmentTypePerson,
		FragmentTypeCommunity,
	)
	if !changed {
		t.Fatalf("expected canonicalization change")
	}
	if newType != DependencyTypeMemberOf {
		t.Fatalf("expected MEMBER_OF, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason == "" {
		t.Fatalf("expected non-empty canonicalization reason")
	}
}

func TestCanonicalizeEdge_CommunityAffiliatedWithOrganization_IsPreserved(t *testing.T) {
	newType, shouldSwap, changed, reason := CanonicalizeEdge(
		DependencyTypeAffiliatedWith,
		FragmentTypeCommunity,
		FragmentTypeOrganization,
	)
	if changed {
		t.Fatalf("expected canonical AFFILIATED_WITH to be preserved")
	}
	if newType != DependencyTypeAffiliatedWith {
		t.Fatalf("expected AFFILIATED_WITH, got %s", newType)
	}
	if shouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
	if reason != "" {
		t.Fatalf("expected empty reason for preserved canonical edge, got %q", reason)
	}
}
