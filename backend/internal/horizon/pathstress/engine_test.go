package pathstress

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEvaluateLeg_ChokepointAndAffectedIncreaseStress(t *testing.T) {
	eval := NewEvaluator()
	leg := horizonmodels.SupplyChainPathLeg{
		Index:             0,
		Mode:              "sea",
		TransportViaRefs:  []string{"/fac/sea/strait-of-hormuz-terminal"},
		ZoneRefs:          []string{"omn_muscat_sohar_industrial_corridor"},
		AffectedZoneRefs:  []string{"/zone/sea/political_tension/strait-of-hormuz"},
		SpatialSemantics:  []string{"route_qualified", "chokepoint_exposed", "affected_zone_pressure"},
		BindingConfidence: 0.84,
		AlternateFlowRefs: nil,
	}
	stress := eval.EvaluateLeg(leg)
	if stress == nil {
		t.Fatal("expected stress")
	}
	if stress.StressScore <= 0.45 {
		t.Fatalf("stress score=%f, want elevated", stress.StressScore)
	}
	if stress.Rerouteability >= 0.55 {
		t.Fatalf("rerouteability=%f, want constrained", stress.Rerouteability)
	}
}

func TestEvaluatePath_PicksBottleneckLeg(t *testing.T) {
	eval := NewEvaluator()
	legs := []horizonmodels.SupplyChainPathLeg{
		{Index: 0, Mode: "road", Stress: eval.EvaluateLeg(horizonmodels.SupplyChainPathLeg{Index: 0, Mode: "road"})},
		{Index: 1, Mode: "pipeline_gas", TransportViaRefs: []string{"/fac/na/europipe-ii"}, SpatialSemantics: []string{"route_qualified"}, Stress: eval.EvaluateLeg(horizonmodels.SupplyChainPathLeg{Index: 1, Mode: "pipeline_gas", TransportViaRefs: []string{"/fac/na/europipe-ii"}, SpatialSemantics: []string{"route_qualified"}})},
	}
	pathStress := eval.EvaluatePath(legs)
	if pathStress == nil {
		t.Fatal("expected path stress")
	}
	if pathStress.BottleneckLegIndex != 1 {
		t.Fatalf("bottleneck=%d, want 1", pathStress.BottleneckLegIndex)
	}
	if pathStress.PathScore <= 0 {
		t.Fatalf("path score=%f, want positive", pathStress.PathScore)
	}
}
