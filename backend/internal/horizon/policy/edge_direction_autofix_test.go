package policy

import (
	"testing"
)

func TestResolveEdgeDirectionAutoFixMarketInvestsInOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relInvestsIn,
		classMarket,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET INVESTS_IN ORGANIZATION")
	}
	if fix.NewType != relParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixMarketOperatesInOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relOperatesIn,
		classMarket,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET OPERATES_IN ORGANIZATION")
	}
	if fix.NewType != relParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixOrganizationMarketGeneric(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relConsumes,
		classOrganization,
		classMarket,
	)
	if !ok {
		t.Fatalf("expected auto-fix for ORGANIZATION * MARKET")
	}
	if fix.NewType != relParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestResolveEdgeDirectionAutoFixMarketOrganizationGeneric(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relConsumes,
		classMarket,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET * ORGANIZATION")
	}
	if fix.NewType != relParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixProductMarketGenericBecomesTradedIn(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relDependsOn,
		classProduct,
		classMarket,
	)
	if !ok {
		t.Fatalf("expected auto-fix for PRODUCT * MARKET")
	}
	if fix.NewType != relTradedIn {
		t.Fatalf("expected newType TRADED_IN, got %s", fix.NewType)
	}
	if fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestResolveEdgeDirectionAutoFixMarketProductGenericBecomesTradedInWithSwap(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relParticipatesIn,
		classMarket,
		classProduct,
	)
	if !ok {
		t.Fatalf("expected auto-fix for MARKET * PRODUCT")
	}
	if fix.NewType != relTradedIn {
		t.Fatalf("expected newType TRADED_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixCommunityFoundedPerson(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relFounded,
		classCommunity,
		classPerson,
	)
	if !ok {
		t.Fatalf("expected auto-fix for COMMUNITY FOUNDED PERSON")
	}
	if fix.NewType != relFounded {
		t.Fatalf("expected newType FOUNDED (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (PERSON founds COMMUNITY)")
	}
}

func TestResolveEdgeDirectionAutoFixCommunityOwnsOrganization_IsPreserved(t *testing.T) {
	_, ok := ResolveEdgeDirectionAutoFix(
		relOwns,
		classCommunity,
		classOrganization,
	)
	if ok {
		t.Fatalf("expected canonical COMMUNITY -> ORGANIZATION OWNS to be preserved without auto-fix")
	}
}

func TestResolveEdgeDirectionAutoFixGeoMemberOfOrganization(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relMemberOf,
		classGeoRegion,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for GEO_REGION MEMBER_OF ORGANIZATION")
	}
	if fix.NewType != relLocatedIn {
		t.Fatalf("expected LOCATED_IN, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestResolveEdgeDirectionAutoFixOrganizationOwnsGeo(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relOwns,
		classOrganization,
		classGeoRegion,
	)
	if !ok {
		t.Fatalf("expected auto-fix for ORGANIZATION OWNS GEO_REGION")
	}
	if fix.NewType != relLocatedIn {
		t.Fatalf("expected LOCATED_IN, got %s", fix.NewType)
	}
	if fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=false")
	}
}

func TestResolveEdgeDirectionAutoFixDepositAdministersOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relAdministers,
		classDeposit,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for DEPOSIT ADMINISTERS ORGANIZATION")
	}
	if fix.NewType != relAdministers {
		t.Fatalf("expected newType ADMINISTERS (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS DEPOSIT)")
	}
}

func TestResolveEdgeDirectionAutoFixFacilityAdministersOrg(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relAdministers,
		classFacility,
		classOrganization,
	)
	if !ok {
		t.Fatalf("expected auto-fix for FACILITY ADMINISTERS ORGANIZATION")
	}
	if fix.NewType != relAdministers {
		t.Fatalf("expected newType ADMINISTERS (unchanged), got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (ORG ADMINISTERS FACILITY)")
	}
}

func TestApplyEdgeCanonicalizationPolicyUsesDirectionAutoFix(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relInvestsIn,
		SourceType: classMarket,
		TargetType: classOrganization,
		SourcePath: "/market/us/technology/semiconductors",
		TargetPath: "/org/us/acme-inc",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change")
	}
	if got.EdgeType != relParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestApplyEdgeCanonicalizationPolicyMarketOperatesInOrg(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relOperatesIn,
		SourceType: classMarket,
		TargetType: classOrganization,
		SourcePath: "/market/global/energy/oil-gas",
		TargetPath: "/org/us/acme-energy",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change")
	}
	if got.EdgeType != relParticipatesIn {
		t.Fatalf("expected PARTICIPATES_IN, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}

func TestApplyEdgeCanonicalizationPolicyLocatedInGeoToEntity(t *testing.T) {
	// GEO_REGION LOCATED_IN COMMUNITY -> swap to COMMUNITY LOCATED_IN GEO_REGION
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relLocatedIn,
		SourceType: classGeoRegion,
		TargetType: classCommunity,
		SourcePath: "/loc/sa/na/na",
		TargetPath: "/community/sa/industry-association/steel-producers",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for GEO_REGION LOCATED_IN COMMUNITY")
	}
	if got.EdgeType != relLocatedIn {
		t.Fatalf("expected LOCATED_IN unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (COMMUNITY LOCATED_IN GEO_REGION)")
	}
}

func TestApplyEdgeCanonicalizationPolicyAffiliatedWithOrgToCommunity(t *testing.T) {
	// ORGANIZATION AFFILIATED_WITH COMMUNITY -> swap to COMMUNITY AFFILIATED_WITH ORGANIZATION
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relAffiliatedWith,
		SourceType: classOrganization,
		TargetType: classCommunity,
		SourcePath: "/org/eu/steel-association-inc",
		TargetPath: "/community/eu/industry-association/steel-producers",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for ORGANIZATION AFFILIATED_WITH COMMUNITY")
	}
	if got.EdgeType != relAffiliatedWith {
		t.Fatalf("expected AFFILIATED_WITH unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (COMMUNITY AFFILIATED_WITH ORGANIZATION)")
	}
}

func TestResolveEdgeDirectionAutoFix_GovRegulatesMarketExempt(t *testing.T) {
	_, ok := ResolveEdgeDirectionAutoFix(
		relRegulates,
		classOrganization,
		classMarket,
		"/gov/us/federal-reserve", "/market/us/finance/banking",
	)
	if ok {
		t.Fatalf("GOV REGULATES MARKET should NOT trigger auto-fix to PARTICIPATES_IN")
	}
}

func TestResolveEdgeDirectionAutoFix_NonGovRegulatesMarketStillFixes(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relRegulates,
		classOrganization,
		classMarket,
		"/org/us/jpmorgan-chase", "/market/us/finance/banking",
	)
	if !ok {
		t.Fatalf("non-GOV ORG REGULATES MARKET should trigger auto-fix")
	}
	if fix.NewType != relParticipatesIn {
		t.Fatalf("expected newType PARTICIPATES_IN, got %s", fix.NewType)
	}
}

func TestResolveEdgeDirectionAutoFix_GovDependsOnMarketBecomesRegulates(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relDependsOn,
		classOrganization,
		classMarket,
		"/gov/cl/ministerio-de-mineria", "/market/cl/materials/raw-materials",
	)
	if !ok {
		t.Fatalf("GOV DEPENDS_ON MARKET should trigger auto-fix")
	}
	if fix.NewType != relRegulates {
		t.Fatalf("expected newType REGULATES, got %s", fix.NewType)
	}
	if fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=false (GOV is already source)")
	}
}

func TestResolveEdgeDirectionAutoFix_MarketDependsOnGovBecomesRegulatesSwap(t *testing.T) {
	fix, ok := ResolveEdgeDirectionAutoFix(
		relDependsOn,
		classMarket,
		classOrganization,
		"/market/cl/materials/raw-materials", "/gov/cl/ministerio-de-mineria",
	)
	if !ok {
		t.Fatalf("MARKET DEPENDS_ON GOV should trigger auto-fix")
	}
	if fix.NewType != relRegulates {
		t.Fatalf("expected newType REGULATES, got %s", fix.NewType)
	}
	if !fix.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (GOV should become source)")
	}
}

func TestApplyEdgeCanonicalizationPolicy_SupplyChainModeBlocksParticipatesInRewrite(t *testing.T) {
	// DEPENDS_ON (ORG->MARKET) normally rewrites to PARTICIPATES_IN.
	// With SupplyChainMode=true, PARTICIPATES_IN is out-of-contract; block the rewrite.
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relDependsOn,
		SourceType:      classOrganization,
		TargetType:      classMarket,
		SourcePath:      "/org/us/acme",
		TargetPath:      "/market/global/electronics",
		SupplyChainMode: true,
	})
	if got.Changed {
		t.Fatalf("SupplyChainMode must block DEPENDS_ON->PARTICIPATES_IN rewrite; got Changed=true, EdgeType=%s", got.EdgeType)
	}
	if got.EdgeType != relDependsOn {
		t.Fatalf("expected DEPENDS_ON preserved, got %s", got.EdgeType)
	}
}

func TestApplyEdgeCanonicalizationPolicy_SupplyChainModeAllowsDependsOn(t *testing.T) {
	// DEPENDS_ON is in contract; no rewrite; should pass through.
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relDependsOn,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		SourcePath:      "/org/us/acme",
		TargetPath:      "/org/cn/supplier",
		SupplyChainMode: true,
	})
	if got.Changed {
		t.Fatalf("DEPENDS_ON (ORG->ORG) should not change in supply-chain mode, got %s", got.EdgeType)
	}
	if got.EdgeType != relDependsOn {
		t.Fatalf("expected DEPENDS_ON, got %s", got.EdgeType)
	}
}

func TestApplyEdgeCanonicalizationPolicy_GovRegulatesMarketPreserved(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relRegulates,
		SourceType: classOrganization,
		TargetType: classMarket,
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
		EdgeType:   relHasMarketSegment,
		SourceType: classMarket,
		TargetType: classMarket,
		SourcePath: "/market/global/finance/banking/cross-border-payments/b2b",
		TargetPath: "/market/global/finance/banking/cross-border-payments",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for HAS_MARKET_SEGMENT child→parent")
	}
	if got.EdgeType != relHasMarketSegment {
		t.Fatalf("expected HAS_MARKET_SEGMENT unchanged, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (parent→child)")
	}
}

func TestApplyEdgeCanonicalizationPolicy_LegacyAuditedByToAssures(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relAuditedBy,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		SourcePath:      "/org/cn/wingtech",
		TargetPath:      "/org/us/deloitte",
		RawRelationType: "AUDITED_BY",
		Qualifiers:      map[string]interface{}{},
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for legacy AUDITED_BY")
	}
	if got.EdgeType != relAssures {
		t.Fatalf("expected ASSURES, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true (auditor should become source)")
	}
}

func TestApplyEdgeCanonicalizationPolicy_RawAuditedByAssuresSwap(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relAssures,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		SourcePath:      "/org/cn/wingtech",
		TargetPath:      "/org/us/deloitte",
		RawRelationType: "AUDITED_BY",
		Qualifiers:      map[string]interface{}{},
	})
	if !got.Changed {
		t.Fatalf("expected direction canonicalization change for raw AUDITED_BY")
	}
	if got.EdgeType != relAssures {
		t.Fatalf("expected ASSURES, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true for raw AUDITED_BY semantics")
	}
}

func TestApplyEdgeCanonicalizationPolicy_ControlsAuditCueToAssures(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relControls,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		SourcePath:      "/org/cn/wingtech",
		TargetPath:      "/org/us/deloitte",
		RawRelationType: "CONTROLS",
		Reason:          "external auditor for Wingtech in annual report",
		Qualifiers:      map[string]interface{}{},
	})
	if !got.Changed {
		t.Fatalf("expected CONTROLS with audit cue to be canonicalized")
	}
	if got.EdgeType != relAssures {
		t.Fatalf("expected ASSURES, got %s", got.EdgeType)
	}
	if got.ShouldSwap {
		t.Fatalf("expected shouldSwap=false for source-assures-target cue")
	}
}

func TestApplyEdgeCanonicalizationPolicy_ControlsAuditedByCueSwaps(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        relControls,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		SourcePath:      "/org/cn/wingtech",
		TargetPath:      "/org/us/deloitte",
		RawRelationType: "CONTROLS",
		Reason:          "Wingtech is audited by Deloitte",
		Qualifiers:      map[string]interface{}{},
	})
	if !got.Changed {
		t.Fatalf("expected CONTROLS with audited-by cue to be canonicalized")
	}
	if got.EdgeType != relAssures {
		t.Fatalf("expected ASSURES, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true for audited-by cue")
	}
}
