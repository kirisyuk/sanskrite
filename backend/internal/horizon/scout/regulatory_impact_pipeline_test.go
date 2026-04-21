package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestDedupeDiscoveredRelations_KeepsHigherConfidence(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{SourceCanonicalPath: "/gov/eu/ec", TargetCanonicalPath: "/decision/trade/cbam", RelationType: "GATEKEEPS_DECISION", Confidence: 0.5},
		{SourceCanonicalPath: "/gov/eu/ec", TargetCanonicalPath: "/decision/trade/cbam", RelationType: "GATEKEEPS_DECISION", Confidence: 0.9},
	}
	out := dedupeDiscoveredRelations(in)
	if len(out) != 1 {
		t.Fatalf("want 1 edge, got %d", len(out))
	}
	if out[0].Confidence < 0.85 {
		t.Fatalf("expected higher confidence kept, got %v", out[0].Confidence)
	}
}

func TestPolicyImpactMaterializationRoot_PrefersInstrument(t *testing.T) {
	plan := &agents.RegulatoryImpactExecutionPlan{
		Queries: []agents.RegulatoryImpactQuery{{
			Instrument: "/mechanism/regulatory/carbon-border-adjustment",
		}},
	}
	got := policyImpactMaterializationRoot("/domain/v1/regulatory-impact/geo/eu", nil, plan, nil)
	if got != "/mechanism/regulatory/carbon-border-adjustment" {
		t.Fatalf("instrument root: got %q", got)
	}
}

func TestPolicyImpactMaterializationRoot_FallsBackToMechanismFromRels(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			SourceCanonicalPath: "/gov/eu/ec",
			TargetCanonicalPath: "/decision/trade/cbam-review",
			RelationType:        string(ah.RelationNameGatekeeps),
			Confidence:          0.8,
		},
		{
			SourceCanonicalPath: "/decision/trade/cbam-review",
			TargetCanonicalPath: "/mechanism/regulatory/cbam",
			RelationType:        string(ah.RelationNameWields),
			Confidence:          0.75,
		},
	}
	got := policyImpactMaterializationRoot("/domain/v1/regulatory-impact/geo/eu", nil, nil, rels)
	if got != "/mechanism/regulatory/cbam" {
		t.Fatalf("expected mechanism from relations, got %q", got)
	}
}

func TestSanitizeDomainResearchRelations_PolicyImpactAllowlist(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{
			SourceCanonicalPath: "/gov/de/bafin",
			TargetCanonicalPath: "/org/de/example-bank",
			RelationType:        "REGULATES",
			Confidence:          0.8,
		},
		{
			SourceCanonicalPath: "/org/de/example-bank",
			TargetCanonicalPath: "/market/de/banking/retail",
			RelationType:        "SUPPLIES_TO",
			Confidence:          0.7,
		},
	}
	out := sanitizeDomainResearchRelations("/domain/v1/regulatory-impact/geo/de", "regulatory-impact", in)
	if len(out) != 1 {
		t.Fatalf("SUPPLIES_TO must be dropped for policy_impact mode, got %d relations", len(out))
	}
	// MIGRATION NOTE (AH taxonomy / docs/archive/taxonomy-migration-2026/migration_map.yaml):
	// sanitizeDomainResearchRelations now emits scout DependencyType
	// values (snake_case AH RelationName) for non-domain-anchored
	// edges so downstream materializers / allowlists can pin on the
	// scout vocabulary directly. ah.RelationNameRegulates ==
	// ah.RelationNameRegulates == "regulates" (the lowercase canonical
	// form) — the previous UPPER_SNAKE assertion belonged to the
	// pre-migration wire-form pipeline.
	if out[0].RelationType != string(ah.RelationNameRegulates) {
		t.Fatalf("got relation %q", out[0].RelationType)
	}
}

func TestRegulatoryCascadeDirectAffectedPaths_KeepGeoAndCommunityTargets(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase",
			TargetCanonicalPath: "/loc/ae/01/dubai",
			RelationType:        "IN_GEO_SCOPE",
			Confidence:          0.9,
		},
		{
			SourceCanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase",
			TargetCanonicalPath: "/community/ae/industry-association/dubai-family-business-network",
			RelationType:        "AFFECTS",
			Confidence:          0.8,
		},
	}

	got := regulatoryCascadeDirectAffectedPaths(rels)
	if len(got) != 2 {
		t.Fatalf("expected geo and community targets, got %v", got)
	}
	if got[0] != "/community/ae/industry-association/dubai-family-business-network" && got[1] != "/community/ae/industry-association/dubai-family-business-network" {
		t.Fatalf("missing community target in %v", got)
	}
	if got[0] != "/loc/ae/01/dubai" && got[1] != "/loc/ae/01/dubai" {
		t.Fatalf("missing geo target in %v", got)
	}
}
