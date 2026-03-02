package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestPostProcessingPolicy_ShouldRun_Tier0(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      0,
		RecursionDepth:    0,
		MaxRecursionDepth: 2,
	}
	if !p.ShouldRun(StepSourcePlanning) {
		t.Error("tier 0 should allow source planning")
	}
	if !p.ShouldRun(StepResearch) {
		t.Error("tier 0 should allow research")
	}
	if !p.ShouldRun(StepCritic) {
		t.Error("tier 0 should allow critic")
	}
	if !p.ShouldRun(StepReValidation) {
		t.Error("tier 0 should allow re-validation")
	}
	if !p.ShouldRun(StepMetrics) {
		t.Error("tier 0 should allow metrics")
	}
	if !p.ShouldRun(StepArtifactMaterialization) {
		t.Error("tier 0 should allow artifact materialization")
	}
	if !p.ShouldRun(StepCascadeChildScheduling) {
		t.Error("tier 0 should allow cascade child scheduling (no domain peer)")
	}
}

func TestPostProcessingPolicy_ShouldRun_Tier1(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      1,
		RecursionDepth:    1,
		MaxRecursionDepth: 2,
	}
	if !p.ShouldRun(StepSourcePlanning) {
		t.Error("tier 1 should allow source planning")
	}
	if !p.ShouldRun(StepResearch) {
		t.Error("tier 1 should allow research")
	}
	if !p.ShouldRun(StepCritic) {
		t.Error("tier 1 should allow critic")
	}
	if p.ShouldRun(StepReValidation) {
		t.Error("tier 1 should skip re-validation")
	}
	if p.ShouldRun(StepMetrics) {
		t.Error("tier 1 should skip metrics")
	}
}

func TestPostProcessingPolicy_ShouldRun_Tier2(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      2,
		RecursionDepth:    2,
		MaxRecursionDepth: 2,
	}
	if p.ShouldRun(StepSourcePlanning) {
		t.Error("tier 2 should skip source planning")
	}
	if p.ShouldRun(StepResearch) {
		t.Error("tier 2 should skip research")
	}
	if p.ShouldRun(StepCritic) {
		t.Error("tier 2 should skip critic")
	}
}

func TestPostProcessingPolicy_CascadeBlockedByDomainPeer(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:           0,
		RecursionDepth:         0,
		MaxRecursionDepth:      2,
		HasDomainExpansionPeer: true,
	}
	if p.ShouldRun(StepCascadeChildScheduling) {
		t.Error("entity job with domain peer should not schedule cascade children")
	}
	if p.ShouldRun(StepRelationChildScheduling) {
		t.Error("entity job with domain peer should not schedule relation children")
	}
}

func TestPostProcessingPolicy_RelationChildOptInOverridesDomainPeer(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:                 0,
		RecursionDepth:               0,
		MaxRecursionDepth:            2,
		HasDomainExpansionPeer:       true,
		RelationChildSchedulingOptIn: true,
	}
	if !p.ShouldRun(StepRelationChildScheduling) {
		t.Error("scout-it with relationChildSchedulingOptIn should allow relation children despite domain peer")
	}
}

func TestPostProcessingPolicy_MarketTargetAllowsRelationChildrenInDomainExpansion(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:           0,
		RecursionDepth:        0,
		MaxRecursionDepth:     2,
		HasDomainExpansionPeer: true,
		TargetIsMarket:        true,
	}
	if !p.ShouldRun(StepRelationChildScheduling) {
		t.Error("market target in domain expansion should allow relation children (participants, sub-markets)")
	}
}

func TestPostProcessingPolicy_MarketAtMaxDepthGetsOneExtraHop(t *testing.T) {
	// recursionDepth=1, maxRecursionDepth=1: normally blocked, but market target gets one extra hop.
	p := &PostProcessingPolicy{
		PipelineTier:           0,
		RecursionDepth:        1,
		MaxRecursionDepth:     1,
		HasDomainExpansionPeer: true,
		TargetIsMarket:        true,
	}
	if !p.ShouldRun(StepRelationChildScheduling) {
		t.Error("market at max depth in domain expansion should get one extra hop for participants/sub-markets")
	}
}

func TestPostProcessingPolicy_CascadeAllowedForDomainExpansionJob(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:           0,
		RecursionDepth:         0,
		MaxRecursionDepth:      2,
		HasDomainExpansionPeer: true,
		IsDomainExpansionJob:   true,
	}
	if !p.ShouldRun(StepCascadeChildScheduling) {
		t.Error("domain expansion job should always allow cascade children")
	}
	if !p.ShouldRun(StepRelationChildScheduling) {
		t.Error("domain expansion job should allow relation children")
	}
}

func TestPostProcessingPolicy_ResearchSkippedWhenPlanModeDone(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:         0,
		PlanModeResearchDone: true,
	}
	if p.ShouldRun(StepResearch) {
		t.Error("research should be skipped when planModeResearchDone=true")
	}
}

func TestPostProcessingPolicy_ResearchMode_DomainHolistic(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:  0,
		DomainModes:   []string{taxonomy.DomainModeSupplyChain},
		OriginalQuery: "Turkey natural gas supply chain",
	}
	mode := p.ResearchMode()
	if mode != ResearchModeDomainHolistic {
		t.Errorf("expected DomainHolistic, got %d", mode)
	}
}

func TestPostProcessingPolicy_ResearchMode_QueryContextual(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:  0,
		DomainModes:   []string{taxonomy.DomainModeGeneral},
		OriginalQuery: "some analysis query",
	}
	mode := p.ResearchMode()
	if mode != ResearchModeQueryContextual {
		t.Errorf("expected QueryContextual, got %d", mode)
	}
}

func TestPostProcessingPolicy_ResearchMode_EntityTypeHolistic(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier: 0,
	}
	mode := p.ResearchMode()
	if mode != ResearchModeEntityTypeHolistic {
		t.Errorf("expected EntityTypeHolistic, got %d", mode)
	}
}

func TestPostProcessingPolicy_ResearchMode_GapDriven(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:    0,
		EpistemicStatus: models.EpistemicStatusVerified,
		CriticGapCount:  2,
	}
	mode := p.ResearchMode()
	if mode != ResearchModeGapDriven {
		t.Errorf("expected GapDriven, got %d", mode)
	}
}

func TestPostProcessingPolicy_ResearchMode_Skip(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier: 1,
	}
	mode := p.ResearchMode()
	if mode == ResearchModeSkip {
		t.Errorf("tier 1 should not be forced into skip mode, got %d", mode)
	}
}

func TestPostProcessingPolicy_ResearchMode_Skip_Tier2(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier: 2,
	}
	mode := p.ResearchMode()
	if mode != ResearchModeSkip {
		t.Errorf("expected Skip on tier 2, got %d", mode)
	}
}

func TestPostProcessingPolicy_PrimaryDomainMode(t *testing.T) {
	tests := []struct {
		name   string
		modes  []string
		expect string
	}{
		{"supply_chain", []string{taxonomy.DomainModeGeneral, taxonomy.DomainModeSupplyChain}, taxonomy.DomainModeSupplyChain},
		{"general_only", []string{taxonomy.DomainModeGeneral}, ""},
		{"empty", nil, ""},
		{"governance", []string{taxonomy.DomainModeGovernance}, taxonomy.DomainModeGovernance},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &PostProcessingPolicy{DomainModes: tc.modes}
			got := p.PrimaryDomainMode()
			if got != tc.expect {
				t.Errorf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}

func TestPostProcessingPolicy_NilSafe(t *testing.T) {
	var p *PostProcessingPolicy
	if !p.ShouldRun(StepResearch) {
		t.Error("nil policy should allow all steps")
	}
	if p.ResearchMode() != ResearchModeGapDriven {
		t.Error("nil policy should default to GapDriven research mode")
	}
}

func TestRelationChildScheduling_AtMaxDepth(t *testing.T) {
	p := &PostProcessingPolicy{
		RecursionDepth:       2,
		MaxRecursionDepth:    2,
		IsDomainExpansionJob: true,
	}
	if p.ShouldRun(StepRelationChildScheduling) {
		t.Error("should block relation children at max recursion depth")
	}
}

func TestPostProcessingPolicy_CanSkipPlanning_Tier2WithCoverage(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      2,
		CriticalOpenCount: 0,
		EvidenceCoverage:  0.9,
	}
	if !p.CanSkipPlanning() {
		t.Fatal("expected planning skip for tier2 with no critical gaps and high coverage")
	}
}

func TestPostProcessingPolicy_CanSkipPlanning_BlockedByCriticalGaps(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      2,
		CriticalOpenCount: 2,
		EvidenceCoverage:  1.0,
	}
	if p.CanSkipPlanning() {
		t.Fatal("expected planning skip to be blocked when critical gaps are open")
	}
}

func TestPostProcessingPolicy_CanSkipPlanning_BlockedByLowCoverage(t *testing.T) {
	p := &PostProcessingPolicy{
		PipelineTier:      2,
		CriticalOpenCount: 0,
		EvidenceCoverage:  0.40,
	}
	if p.CanSkipPlanning() {
		t.Fatal("expected planning skip to be blocked with low evidence coverage")
	}
}
