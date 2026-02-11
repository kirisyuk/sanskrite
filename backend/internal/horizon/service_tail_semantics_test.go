package horizon

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestAssessTailScenarioMode_UsesSignalSemanticsWhenAvailable(t *testing.T) {
	resp := &FragmentRollupResponse{
		MetricName: "/metric/risk/geo/conflict",
		Value:      0.42,
	}
	primary := models.MetricValue{
		Value:      0.42,
		Confidence: 0.91,
	}

	tail := assessTailScenarioMode(resp, primary, signalSemanticTailContext{
		Likelihood: 0.18,
		Impact:     0.95,
		Modality:   models.SignalModalityProjected,
		Escalation: models.EscalationLevelWarning,
		Samples:    4,
	})

	if !tail.Enabled {
		t.Fatalf("expected tail mode enabled")
	}
	if tail.Likelihood > 0.25 {
		t.Fatalf("expected semantics likelihood to dominate, got %.4f", tail.Likelihood)
	}
	if tail.Impact < 0.90 {
		t.Fatalf("expected high semantic impact, got %.4f", tail.Impact)
	}
	if tail.Escalation != models.EscalationLevelWarning {
		t.Fatalf("expected escalation=warning, got %q", tail.Escalation)
	}
}

func TestAssessTailScenarioMode_FallsBackWithoutSignalSemantics(t *testing.T) {
	resp := &FragmentRollupResponse{
		MetricName: "/metric/risk/geo/conflict",
		Value:      0.40,
	}
	primary := models.MetricValue{
		Value:      0.40,
		Confidence: 0.90,
	}

	tail := assessTailScenarioMode(resp, primary, signalSemanticTailContext{})
	if tail.Enabled {
		t.Fatalf("expected normal mode without semantic tail evidence")
	}
	if tail.Likelihood != 0.90 {
		t.Fatalf("expected fallback likelihood from primary confidence, got %.4f", tail.Likelihood)
	}
}

func TestParseSignalSemanticsFromFact_TagsAndSummaryFallback(t *testing.T) {
	fTagged := &coremodels.OntologyFact{
		Kind:       "signal",
		Type:       "risk:geo/conflict",
		Confidence: 0.4,
		BaseOntologyFields: coremodels.BaseOntologyFields{
			Tags: []string{
				"signal_impact:0.87",
				"signal_likelihood:0.31",
				"signal_modality:escalation",
				"signal_escalation:warning",
			},
		},
	}
	sem := parseSignalSemanticsFromFact(fTagged)
	if !sem.Ready {
		t.Fatalf("expected semantics ready")
	}
	if sem.Impact < 0.86 || sem.Impact > 0.88 {
		t.Fatalf("unexpected impact: %.4f", sem.Impact)
	}
	if sem.Likelihood < 0.30 || sem.Likelihood > 0.32 {
		t.Fatalf("unexpected likelihood: %.4f", sem.Likelihood)
	}
	if sem.Modality != models.SignalModalityEscalation {
		t.Fatalf("unexpected modality: %q", sem.Modality)
	}
	if sem.Escalation != models.EscalationLevelWarning {
		t.Fatalf("unexpected escalation: %q", sem.Escalation)
	}

	fSummary := &coremodels.OntologyFact{
		Kind:       "signal",
		Type:       "risk:logistics/capacity",
		Confidence: 0.55,
		Summary:    "Impact: 0.70; Likelihood: 0.25; Modality: rhetoric; Escalation: none",
	}
	sem2 := parseSignalSemanticsFromFact(fSummary)
	if !sem2.Ready {
		t.Fatalf("expected summary semantics ready")
	}
	if sem2.Impact < 0.69 || sem2.Impact > 0.71 {
		t.Fatalf("unexpected summary impact: %.4f", sem2.Impact)
	}
	if sem2.Likelihood < 0.24 || sem2.Likelihood > 0.26 {
		t.Fatalf("unexpected summary likelihood: %.4f", sem2.Likelihood)
	}
	if sem2.Modality != models.SignalModalityRhetoric {
		t.Fatalf("unexpected summary modality: %q", sem2.Modality)
	}
}
