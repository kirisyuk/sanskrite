package watchdog

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestEvaluateSignalSemanticScore_TailRiskEscalation(t *testing.T) {
	f := &models.OntologyFact{
		BaseOntologyFields: models.BaseOntologyFields{
			Tags: []string{
				"signal_impact:1.0",
				"signal_likelihood:0.1",
				"signal_modality:escalation",
				"signal_horizon:short_term",
				"signal_escalation:warning",
			},
		},
		Kind:       "signal",
		Type:       "risk:geo/conflict",
		Confidence: 0.8,
	}

	score, details := evaluateSignalSemanticScore(f)
	if details.EscalationLevel != "warning" {
		t.Fatalf("unexpected escalation level: %q", details.EscalationLevel)
	}
	if score < 0.55 {
		t.Fatalf("expected strong tail-risk semantic score, got %.4f", score)
	}
}

func TestEvaluateSignalSemanticScore_RhetoricNoise(t *testing.T) {
	f := &models.OntologyFact{
		Kind:       "signal",
		Type:       "risk:info/uncertainty",
		Confidence: 0.6,
		Summary:    "Amplitude: 0.20; Impact: 0.20; Likelihood: 0.20; Modality: rhetoric; Horizon: short_term; Escalation: none",
	}

	score, _ := evaluateSignalSemanticScore(f)
	if score >= 0.2 {
		t.Fatalf("expected low rhetoric score, got %.4f", score)
	}
}

func TestIsSignalRiskOpportunityFact(t *testing.T) {
	if !isSignalRiskOpportunityFact(&models.OntologyFact{Kind: "signal", Type: "risk:geo/conflict"}) {
		t.Fatalf("risk signal should be accepted")
	}
	if !isSignalRiskOpportunityFact(&models.OntologyFact{Kind: "signal", Type: "opportunity:physical_flow/demand"}) {
		t.Fatalf("opportunity signal should be accepted")
	}
	if isSignalRiskOpportunityFact(&models.OntologyFact{Kind: "event", Type: "risk:geo/conflict"}) {
		t.Fatalf("non-signal fact should be rejected")
	}
}
