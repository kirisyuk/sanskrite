package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestValidateRegulatoryImpactLayerCoverage_ReachesGeoCommunityAndMedia(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{SourceCanonicalPath: "/gov/ae/ministry-of-finance", TargetCanonicalPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", RelationType: "GATEKEEPS_DECISION"},
		{SourceCanonicalPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", TargetCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", RelationType: "USES_MECHANISM"},
		{SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetCanonicalPath: "/loc/ae/01/dubai", RelationType: "IN_GEO_SCOPE"},
		{SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetCanonicalPath: "/market/ae/capital/foreign-direct-investment", RelationType: "AFFECTS"},
		{SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetCanonicalPath: "/community/ae/industry-association/dubai-family-business-network", RelationType: "APPLIES_TO"},
		{SourceCanonicalPath: "/community/ae/industry-association/dubai-family-business-network", TargetCanonicalPath: "/media/linkedin/dubai-business-founders", RelationType: "OPERATES_VIA"},
		{SourceCanonicalPath: "/gov/ae/federal-tax-authority", TargetCanonicalPath: "/program/ae/corporate-tax-compliance-rollout", RelationType: "ADMINISTERS"},
	}

	cov := ValidateRegulatoryImpactLayerCoverage(rels)
	if _, ok := cov.MechanismReachable["/mechanism/regulatory/corporate-income-tax-rate-increase"]; !ok {
		t.Fatalf("expected reachable mechanism layer")
	}
	if _, ok := cov.GeoReachable["/loc/ae/01/dubai"]; !ok {
		t.Fatalf("expected reachable geo scope")
	}
	if _, ok := cov.CommunityReachable["/community/ae/industry-association/dubai-family-business-network"]; !ok {
		t.Fatalf("expected reachable community transmission layer")
	}
	if _, ok := cov.MediaReachable["/media/linkedin/dubai-business-founders"]; !ok {
		t.Fatalf("expected reachable media channel")
	}
	if _, ok := cov.ProgramReachable["/program/ae/corporate-tax-compliance-rollout"]; !ok {
		t.Fatalf("expected reachable program layer")
	}
	if len(cov.OrphanPaths) != 0 {
		t.Fatalf("expected no orphans, got %v", cov.OrphanPaths)
	}
}

func TestComputeRegulatoryGraphSufficiency_FlagsMissingGeoAndCommunityLayers(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{SourceCanonicalPath: "/gov/ae/ministry-of-finance", TargetCanonicalPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", RelationType: "GATEKEEPS_DECISION"},
		{SourceCanonicalPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", TargetCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", RelationType: "USES_MECHANISM"},
		{SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetCanonicalPath: "/loc/ae/01/dubai", RelationType: "IN_GEO_SCOPE"},
		{SourceCanonicalPath: "/community/ae/industry-association/dubai-family-business-network", TargetCanonicalPath: "/media/linkedin/dubai-business-founders", RelationType: "OPERATES_VIA"},
	}

	suff := ComputeRegulatoryGraphSufficiency(rels)
	if string(suff.Tier) != string(RegulatorySufficiencyForensicGap) {
		t.Fatalf("expected forensic gap tier, got %s", suff.Tier)
	}
	if !containsGap(suff.Gaps, "missing_community_transmission") {
		t.Fatalf("expected missing_community_transmission gap, got %v", suff.Gaps)
	}
	if !containsGap(suff.Gaps, "missing_media_channel_layer") {
		t.Fatalf("expected missing_media_channel_layer gap, got %v", suff.Gaps)
	}
	if !containsGap(suff.Gaps, "orphan_mechanism_or_decision_nodes") {
		t.Fatalf("expected orphan_mechanism_or_decision_nodes gap, got %v", suff.Gaps)
	}
}

func containsGap(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
