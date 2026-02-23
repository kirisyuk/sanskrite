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
