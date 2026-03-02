package qa

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildContext_WithInterpretations(t *testing.T) {
	ctx := context.Background()
	b := NewAnswerContextBuilder()

	plan := &horizonmodels.QuestionPlan{
		NormalizedQuestion: "How would a VAT increase affect battery costs?",
		AnswerMode:         "impact_assessment",
		TimeHorizon:        "medium_term",
	}

	simResult := &horizonmodels.SimulationResult{
		NodeImpacts: []horizonmodels.NodeImpact{
			{
				FragmentID:     "f1",
				CanonicalPath:  "/org/pt-vale-indonesia",
				Name:           "PT Vale Indonesia",
				AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.72},
				Hops:           0,
			},
		},
		TracePaths: []horizonmodels.TracePath{
			{
				NodeCanonicalPaths: []string{"/org/pt-vale-indonesia", "/product/nickel-ore"},
				Axis:               "physical_flow/supply",
				Amplitudes:         []float64{0.72, 0.58},
			},
		},
		LeafEvidence: []horizonmodels.LeafSignalEvidence{
			{
				FragmentID:    "f1",
				CanonicalPath: "/org/pt-vale-indonesia",
				Axis:         "physical_flow/supply",
				Amplitude:    0.45,
				Summary:      "AIS: 3 vessels delayed",
			},
		},
	}

	interpretations := []*interpretation.RiskInterpretation{
		{
			NodeID:        "f1",
			CanonicalPath: "/org/pt-vale-indonesia",
			Polarity:      "risk",
			Summary:       "PT Vale Indonesia faces moderate supply chain risk from VAT increase. Nickel ore production costs would rise by 3-5%.",
			Impact: interpretation.ImpactEstimate{
				Operational: "Cost pass-through to downstream",
				Financial:   "+3-5% nickel ore cost",
				Timeline:    "6-8 weeks",
			},
			Mitigations: []interpretation.Mitigation{
				{Action: "Increase cathode buffer", Description: "~6 weeks"},
			},
		},
	}

	prompt, err := b.BuildContext(ctx, plan, simResult, nil, []string{"Chain: Indonesia Nickel"}, interpretations, nil)
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}

	// Must use human-readable interpretations, not raw axis/amplitude
	if !strings.Contains(prompt, "NODE INTERPRETATIONS") {
		t.Error("expected NODE INTERPRETATIONS section when interpretations provided")
	}
	if !strings.Contains(prompt, "PT Vale Indonesia faces moderate supply chain risk") {
		t.Error("expected interpretation summary in prompt")
	}
	if !strings.Contains(prompt, "Cost pass-through to downstream") {
		t.Error("expected impact in prompt")
	}
	if !strings.Contains(prompt, "Increase cathode buffer") {
		t.Error("expected mitigation in prompt")
	}
}

func TestBuildContext_WithoutInterpretations(t *testing.T) {
	ctx := context.Background()
	b := NewAnswerContextBuilder()

	plan := &horizonmodels.QuestionPlan{
		NormalizedQuestion: "How would a VAT increase affect battery costs?",
		AnswerMode:         "impact_assessment",
		TimeHorizon:        "medium_term",
	}

	simResult := &horizonmodels.SimulationResult{
		NodeImpacts: []horizonmodels.NodeImpact{
			{
				FragmentID:     "f1",
				CanonicalPath:  "/org/pt-vale-indonesia",
				AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.72},
				Hops:           0,
			},
		},
		TracePaths:   nil,
		LeafEvidence: nil,
	}

	prompt, err := b.BuildContext(ctx, plan, simResult, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}

	// Must use raw IMPACT MAP when no interpretations
	if !strings.Contains(prompt, "IMPACT MAP") {
		t.Error("expected IMPACT MAP section when no interpretations")
	}
	if !strings.Contains(prompt, "physical_flow/supply: 0.72") {
		t.Error("expected raw axis/amplitude when no interpretations")
	}
}
