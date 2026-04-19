package propagation

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyEdgeFamilyPhysicsBoostsConflictThroughChokepointLogistics(t *testing.T) {
	fromFrag := &horizonmodels.HorizonFragment{
		CanonicalPath:  "/loc/na/strait-of-hormuz/na",
		GeoFeatureType: "strait",
	}
	edge := &horizonmodels.DependencyEdge{
		Relation: DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet":               string(FacetLogistics),
			"throughput_share":    0.75,
			"capacity_constraint": 0.80,
			"reroute_penalty":     0.40,
			"substitutability":    0.10,
		},
	}

	summary := ApplyEdgeFamilyPhysics(horizonmodels.AxisGeoConflict, fromFrag, edge, DefaultEdgeFamilyPhysicsProfile())
	if summary.Family != "chokepoint_logistics" {
		t.Fatalf("family = %q, want chokepoint_logistics", summary.Family)
	}
	if summary.Multiplier <= 1.15 {
		t.Fatalf("expected chokepoint/logistics multiplier > 1.15, got %.3f", summary.Multiplier)
	}
}

func TestApplyEdgeFamilyPhysicsPenalizesHistoricalRegulatoryPath(t *testing.T) {
	past := time.Now().UTC().Add(-24 * time.Hour)
	edge := &horizonmodels.DependencyEdge{
		Relation: DependencyTypeRegulates,
		ValidTo: &past,
	}

	summary := ApplyEdgeFamilyPhysics(horizonmodels.AxisGeoConflict, nil, edge, DefaultEdgeFamilyPhysicsProfile())
	if summary.Family != "regulatory_control" {
		t.Fatalf("family = %q, want regulatory_control", summary.Family)
	}
	if summary.Multiplier >= 0.85 {
		t.Fatalf("expected historical regulatory multiplier < 0.85, got %.3f", summary.Multiplier)
	}
}
