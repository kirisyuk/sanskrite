package policy

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestResolveEdgeDirectionAutoFixMarketInvestsInOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeInvestsIn,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET INVESTS_IN ORGANIZATION")
	}
	if fix.NewType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixMarketOperatesInOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeOperatesIn,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET OPERATES_IN ORGANIZATION")
	}
	if fix.NewType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixOrganizationMarketGeneric(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeConsumes,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeMarket,
	)
	if !ok {
		t.Fatalf("expected auto-fix for ORGANIZATION * MARKET")
	}
	if fix.NewType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestResolveEdgeDirectionAutoFixMarketOrganizationGeneric(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeConsumes,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET * ORGANIZATION")
	}
	if fix.NewType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixCommunityFoundedPerson(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeFounded,
		horizonmodels.FragmentTypeCommunity,
		horizonmodels.FragmentTypePerson,
	)
	if !ok {
		t.Fatalf("expected auto-fix for COMMUNITY FOUNDED PERSON")
	}
	if fix.NewType != horizonmodels.DependencyTypeFounded {
		t.Fatalf("expected newType FOUNDED (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (PERSON founds COMMUNITY)")
	}
}

func TestResolveEdgeDirectionAutoFixDepositAdministersOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeAdministers,
		horizonmodels.FragmentTypeDeposit,
		horizonmodels.FragmentTypeOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for DEPOSIT ADMINISTERS ORGANIZATION")
	}
	if fix.NewType != horizonmodels.DependencyTypeAdministers {
		t.Fatalf("expected newType ADMINISTERS (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS DEPOSIT)")
	}
}

func TestResolveEdgeDirectionAutoFixFacilityAdministersOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeAdministers,
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for FACILITY ADMINISTERS ORGANIZATION")
	}
	if fix.NewType != horizonmodels.DependencyTypeAdministers {
		t.Fatalf("expected newType ADMINISTERS (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS FACILITY)")
	}
}

func TestApplyEdgeCanonicalizationPolicyUsesDirectionAutoFix(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeInvestsIn,
		SourceType: horizonmodels.FragmentTypeMarket,
		TargetType: horizonmodels.FragmentTypeOrganization,
		SourcePath: "/market/us/technology/semiconductors",
		TargetPath: "/org/us/acme-inc",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change")
	}
	if got.EdgeType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestApplyEdgeCanonicalizationPolicyMarketOperatesInOrg(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeOperatesIn,
		SourceType: horizonmodels.FragmentTypeMarket,
		TargetType: horizonmodels.FragmentTypeOrganization,
		SourcePath: "/market/global/energy/oil-gas",
		TargetPath: "/org/us/acme-energy",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change")
	}
	if got.EdgeType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestApplyEdgeCanonicalizationPolicyLocatedInGeoToEntity(t *testing.T) {
	// GEO_REGION LOCATED_IN COMMUNITY -> swap to COMMUNITY LOCATED_IN GEO_REGION
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeLocatedIn,
		SourceType: horizonmodels.FragmentTypeGeoRegion,
		TargetType: horizonmodels.FragmentTypeCommunity,
		SourcePath: "/loc/sa/na/na",
		TargetPath: "/community/sa/industry-association/steel-producers",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for GEO_REGION LOCATED_IN COMMUNITY")
	}
	if got.EdgeType != horizonmodels.DependencyTypeLocatedIn {
		t.Fatalf("expected LOCATED_IN unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (COMMUNITY LOCATED_IN GEO_REGION)")
	}
}

func TestApplyEdgeCanonicalizationPolicyAffiliatedWithOrgToCommunity(t *testing.T) {
	// ORGANIZATION AFFILIATED_WITH COMMUNITY -> swap to COMMUNITY AFFILIATED_WITH ORGANIZATION
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeAffiliatedWith,
		SourceType: horizonmodels.FragmentTypeOrganization,
		TargetType: horizonmodels.FragmentTypeCommunity,
		SourcePath: "/org/eu/steel-association-inc",
		TargetPath: "/community/eu/industry-association/steel-producers",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for ORGANIZATION AFFILIATED_WITH COMMUNITY")
	}
	if got.EdgeType != horizonmodels.DependencyTypeAffiliatedWith {
		t.Fatalf("expected AFFILIATED_WITH unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (COMMUNITY AFFILIATED_WITH ORGANIZATION)")
	}
}

func TestResolveEdgeDirectionAutoFix_GovRegulatesMarketExempt(t *testing.T) {
	_, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeRegulates,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeMarket,
		"/gov/us/federal-reserve", "/market/us/finance/banking",
	)
	if ok {
		t.Fatalf("GOV REGULATES MARKET should NOT trigger auto-fix to PARTICIPATES_IN")
	}
}

func TestResolveEdgeDirectionAutoFix_NonGovRegulatesMarketStillFixes(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		horizonmodels.DependencyTypeRegulates,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeMarket,
		"/org/us/jpmorgan-chase", "/market/us/finance/banking",
	)
	if !ok {
		t.Fatalf("non-GOV ORG REGULATES MARKET should trigger auto-fix")
	}
	if fix.NewType != horizonmodels.DependencyTypeParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
}

func TestApplyEdgeCanonicalizationPolicy_GovRegulatesMarketPreserved(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeRegulates,
		SourceType: horizonmodels.FragmentTypeOrganization,
		TargetType: horizonmodels.FragmentTypeMarket,
		SourcePath: "/gov/us/federal-reserve",
		TargetPath: "/market/us/finance/banking",
	})
	if got.Changed {
		t.Fatalf("GOV REGULATES MARKET should not be rewritten, but got changed to %s (reason: %s)", got.EdgeType, got.Reason)
	}
}

func TestApplyEdgeCanonicalizationPolicyHasMarketSegmentChildToParent(t *testing.T) {
	// Child (more segments) as source, parent as target -> should swap to parent→child
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeHasMarketSegment,
		SourceType: horizonmodels.FragmentTypeMarket,
		TargetType: horizonmodels.FragmentTypeMarket,
		SourcePath: "/market/global/finance/banking/cross-border-payments/b2b",
		TargetPath: "/market/global/finance/banking/cross-border-payments",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for HAS_MARKET_SEGMENT child→parent")
	}
	if got.EdgeType != horizonmodels.DependencyTypeHasMarketSegment {
		t.Fatalf("expected HAS_MARKET_SEGMENT unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (parent→child)")
	}
}
