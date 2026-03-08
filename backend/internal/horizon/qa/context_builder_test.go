package qa

import (
	"context"
	"strings"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildContext_ImpactMap(t *testing.T) {
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
				Axis:          "physical_flow/supply",
				Amplitude:     0.45,
				Summary:       "AIS: 3 vessels delayed",
			},
		},
		ChainImpacts: []horizonmodels.ChainImpact{
			{
				DisplayName:          "Indonesia Nickel Chain",
				PathStressScore:      0.74,
				AffectedNodes:        2,
				TotalNodes:           3,
				AffectedLegs:         1,
				LegCount:             1,
				BottleneckSummary:    "/fac/id/port -> /sea-route/malacca -> /fac/cn/refinery",
				RecoveryLatencyHours: 96,
				DominantFactors:      []string{"affected_zone_pressure", "reroute_penalty"},
				FlowRefs:             []string{"malacca_metals_east_asia"},
			},
		},
	}

	prompt, err := b.BuildContext(ctx, plan, simResult, nil, []string{"Chain: Indonesia Nickel"})
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}

	if !strings.Contains(prompt, "IMPACT MAP") {
		t.Error("expected IMPACT MAP section")
	}
	if !strings.Contains(prompt, "/org/pt-vale-indonesia") {
		t.Error("expected canonical path in impact map")
	}
	if !strings.Contains(prompt, "physical_flow/supply: 0.72") {
		t.Error("expected raw axis/amplitude in impact map")
	}
	if !strings.Contains(prompt, "PROPAGATION CHAINS") {
		t.Error("expected PROPAGATION CHAINS section")
	}
	if !strings.Contains(prompt, "PATH STRESS / BOTTLENECK CHAINS") {
		t.Error("expected chain stress section")
	}
	if !strings.Contains(prompt, "Indonesia Nickel Chain") {
		t.Error("expected chain impact in context")
	}
}

func TestBuildContext_EmptySimulation(t *testing.T) {
	ctx := context.Background()
	b := NewAnswerContextBuilder()

	plan := &horizonmodels.QuestionPlan{
		NormalizedQuestion: "How would a VAT increase affect battery costs?",
		AnswerMode:         "impact_assessment",
		TimeHorizon:        "medium_term",
	}

	simResult := &horizonmodels.SimulationResult{
		NodeImpacts:  nil,
		TracePaths:   nil,
		LeafEvidence: nil,
	}

	prompt, err := b.BuildContext(ctx, plan, simResult, nil, nil)
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}

	if !strings.Contains(prompt, "IMPACT MAP") {
		t.Error("expected IMPACT MAP section")
	}
	if !strings.Contains(prompt, "no propagation") {
		t.Error("expected 'no propagation' hint for empty simulation")
	}
}
