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

func TestApplyEdgeCanonicalizationPolicyHasSubmarketChildToParent(t *testing.T) {
	// Child (more segments) as source, parent as target -> should swap to parent→child
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   horizonmodels.DependencyTypeHasSubmarket,
		SourceType: horizonmodels.FragmentTypeMarket,
		TargetType: horizonmodels.FragmentTypeMarket,
		SourcePath: "/market/global/finance/banking/cross-border-payments/b2b",
		TargetPath: "/market/global/finance/banking/cross-border-payments",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for HAS_SUBMARKET child→parent")
	}
	if got.EdgeType != horizonmodels.DependencyTypeHasSubmarket {
		t.Fatalf("expected HAS_SUBMARKET unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (parent→child)")
	}
}
