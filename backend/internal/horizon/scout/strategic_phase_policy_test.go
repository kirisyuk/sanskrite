package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestShouldRunInlineStrategicCollect_DefaultOffAndOptIn(t *testing.T) {
	job := &models.ScoutingJob{}
	job.State = map[string]interface{}{}
	if shouldRunInlineStrategicCollect(job) {
		t.Fatalf("inline strategic collect must be disabled by default")
	}
	job.State["enableInlineStrategicCollect"] = true
	if !shouldRunInlineStrategicCollect(job) {
		t.Fatalf("inline strategic collect must be enabled when explicit opt-in is set")
	}
}

func TestShouldRunStrategicOverlay_NoSupplyChainFallback(t *testing.T) {
	e := &ScoutEngine{}
	if e.shouldRunStrategicOverlay("supply-chain", 3, 2) {
		t.Fatalf("supply-chain must not auto-run strategic overlay")
	}
	if e.shouldRunStrategicOverlay("unknown-contract", 3, 2) {
		t.Fatalf("unknown contracts must not auto-run strategic overlay")
	}
	if !e.shouldRunStrategicOverlay("risk-assessment", 3, 2) {
		t.Fatalf("risk-assessment should auto-run strategic overlay")
	}
}

func TestSanitizeDomainResearchRelations_StripsCrossModeNoise(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{
			TargetCanonicalPath: "/market/global/technology/telecom",
			RelationType:        "HAS_SUBMARKET",
		},
		{
			SourceCanonicalPath: "/org/cn/huawei",
			TargetCanonicalPath: "/org/kz/kazakhtelecom",
			RelationType:        "SUPPLIES_TO",
		},
		{
			SourceCanonicalPath: "/org/cn/huawei",
			TargetCanonicalPath: "/org/kz/kazakhtelecom",
			RelationType:        "PARTICIPATES_IN",
		},
	}
	out := sanitizeDomainResearchRelations("/domain/v1/supply-chain/telecom", "supply-chain", in)
	if len(out) != 2 {
		t.Fatalf("expected 2 sanitized relations, got %d", len(out))
	}
	if out[0].RelationType != string(models.DependencyTypeFocusesOn) {
		t.Fatalf("domain-sourced non-scaffold relation must normalize to FOCUSES_ON, got %s", out[0].RelationType)
	}
	if out[1].RelationType != string(models.DependencyTypeSuppliesTo) {
		t.Fatalf("explicit org-sourced SUPPLIES_TO must be preserved, got %s", out[1].RelationType)
	}
}
