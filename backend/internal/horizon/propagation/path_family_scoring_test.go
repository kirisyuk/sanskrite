package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestScorePathFamiliesDetectsCrossBorderEnergySupplyChain(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{
		Axis:     horizonmodels.AxisGeoConflict,
		Polarity: "risk",
	}
	trace := []SignalPropagationTraceEvent{
		{
			Outcome:      "propagated",
			EdgeType:     string(horizonmodels.DependencyTypeSuppliesTo),
			Channel:      "economic",
			Scope:        "cross_border",
			Spillover:    "cross_border_economic",
			FromPath:     "/fac/ir/bandar-abbas-refinery",
			ToPath:       "/market/de/energy/natural-gas",
			AmplitudeOut: 0.62,
			Hops:         2,
		},
		{
			Outcome:      "propagated",
			EdgeType:     string(horizonmodels.DependencyTypeDependsOn),
			Channel:      "operational",
			Scope:        "cross_border",
			Spillover:    "cross_border_economic",
			FromPath:     "/market/de/energy/natural-gas",
			ToPath:       "/fac/cn/lng-intensive-plant",
			AmplitudeOut: 0.54,
			Hops:         3,
		},
	}

	summary := ScorePathFamilies(signal, trace, 3)
	if summary.PathFamilies == 0 {
		t.Fatal("expected path families")
	}
	if summary.BusinessPathFamilies == 0 {
		t.Fatal("expected at least one business path family")
	}
	if summary.CrossBorderFamilies == 0 {
		t.Fatal("expected at least one cross-border family")
	}
	found := false
	for _, family := range summary.TopFamilies {
		if family.Key == "energy_supply_chain" {
			found = true
			if family.BusinessTailSteps == 0 {
				t.Fatalf("expected energy family to have business tails: %+v", family)
			}
		}
	}
	if !found {
		t.Fatalf("expected energy_supply_chain in top families, got %+v", summary.TopFamilies)
	}
}

func TestClassifyPathFamiliesDetectsChokepointLogistics(t *testing.T) {
	ev := SignalPropagationTraceEvent{
		Outcome:   "propagated",
		EdgeType:  string(horizonmodels.DependencyTypeOperatesIn),
		Channel:   "geopolitical",
		Scope:     "cross_border",
		Spillover: "cross_border_operational",
		FromPath:  "/loc/na/persian-gulf/strait-of-hormuz",
		ToPath:    "/fac/ae/jebel-ali-port",
	}
	keys := classifyPathFamilies(ev)
	found := false
	for _, key := range keys {
		if key == "chokepoint_logistics" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected chokepoint_logistics, got %v", keys)
	}
}
