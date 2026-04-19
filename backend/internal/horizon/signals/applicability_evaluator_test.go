package signals

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEquivalentLocationPaths_InternationalAlias(t *testing.T) {
	if !equivalentLocationPaths("/loc/int/na/red-sea", "/loc/int/red-sea") {
		t.Fatal("expected international Red Sea paths to match")
	}
	if !equivalentLocationPaths("/loc/int/na/bab-el-mandeb", "/loc/int/bab-el-mandeb") {
		t.Fatal("expected Bab el-Mandeb aliases to match")
	}
	if equivalentLocationPaths("/loc/int/na/red-sea", "/loc/int/na/strait-of-hormuz") {
		t.Fatal("expected different chokepoints to stay distinct")
	}
}

func TestAxisTypeFit_AllowsTransitChokepointGeoForLogistics(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/loc/int/red-sea",
		Class:         FragmentTypeGeoRegion,
		Labels:        map[string]string{"en": "Red Sea"},
		Identifiers: map[string]string{
			"geoKind": "chokepoint_anchor",
		},
	}
	score, reason := axisTypeFit(horizonmodels.AxisLogisticsCapacity, frag)
	if score < 0.9 {
		t.Fatalf("expected chokepoint geo region to fit logistics, got score=%.2f reason=%s", score, reason)
	}
}

func TestExternalApplicabilityEvaluator_ExactLocationAliasPassesHighStakesSeed(t *testing.T) {
	evaluator := NewExternalApplicabilityEvaluator()
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisLogisticsCapacity,
		LocationPath: "/loc/int/na/red-sea",
	}
	fragment := &horizonmodels.HorizonFragment{
		ID:            "frag-red-sea",
		CanonicalPath: "/loc/int/red-sea",
		Class:         FragmentTypeGeoRegion,
		Labels:        map[string]string{"en": "Red Sea"},
		Identifiers: map[string]string{
			"geoKind": "chokepoint_anchor",
		},
	}
	decision := evaluator.Evaluate(context.Background(), signal, fragment)
	if !decision.Applicable {
		t.Fatalf("expected exact chokepoint seed to be applicable, got %+v", decision)
	}
	if decision.Score < 0.8 {
		t.Fatalf("expected strong applicability score, got %+v", decision)
	}
}
