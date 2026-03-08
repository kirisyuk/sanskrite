package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildPropagationNodeOverlay_ClassifiesChokepointAndDefense(t *testing.T) {
	chokepoint := &horizonmodels.HorizonFragment{
		CanonicalPath:  "/loc/om/strait-of-hormuz",
		Type:           horizonmodels.FragmentTypeGeoRegion,
		GeoFeatureType: "strait",
		Tags:           []string{"chokepoint"},
	}
	overlay := BuildPropagationNodeOverlay(chokepoint)
	if overlay.PrimaryRole != "chokepoint" {
		t.Fatalf("expected chokepoint primary role, got %+v", overlay)
	}
	if !overlay.HasRole("bridge") || !overlay.HasExposure("route_dependency") {
		t.Fatalf("expected chokepoint bridge/route exposure, got %+v", overlay)
	}

	defense := &horizonmodels.HorizonFragment{
		CanonicalPath: "/fac/qa/al-udeid-air-base",
		Type:          horizonmodels.FragmentTypeFacility,
		Labels:        map[string]string{"en": "Al Udeid Air Base"},
	}
	overlay = BuildPropagationNodeOverlay(defense)
	if overlay.PrimaryRole != "defense_operator" {
		t.Fatalf("expected defense operator, got %+v", overlay)
	}
}

func TestBuildPropagationNodeOverlay_DemotesGenericDistributor(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/fac/gb/birmingham-export-distribution-center",
		Type:          horizonmodels.FragmentTypeFacility,
		Labels:        map[string]string{"en": "Birmingham Export Distribution Center"},
	}
	overlay := BuildPropagationNodeOverlay(frag)
	if overlay.BusinessRole != "distributor" {
		t.Fatalf("expected distributor role, got %+v", overlay)
	}
	if overlay.OperationalScore >= 0.70 {
		t.Fatalf("expected generic distributor to stay below high operational relevance, got %+v", overlay)
	}
}

func TestBuildPropagationEdgeOverlay_BoostsRouteTransit(t *testing.T) {
	from := &horizonmodels.HorizonFragment{
		CanonicalPath:  "/loc/om/strait-of-hormuz",
		Type:           horizonmodels.FragmentTypeGeoRegion,
		GeoFeatureType: "strait",
	}
	edge := &horizonmodels.DependencyEdge{
		Type: horizonmodels.DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet": "logistics",
		},
	}
	overlay := BuildPropagationEdgeOverlay(horizonmodels.AxisGeoConflict, edge, from)
	if overlay.TransmissionProfile != "route_transit" {
		t.Fatalf("expected route_transit, got %+v", overlay)
	}
	if overlay.WeightHint <= 1.0 {
		t.Fatalf("expected route transit edge weight > 1, got %+v", overlay)
	}
}

func TestBuildPropagationNodeOverlay_UsesAffectedZoneBlastMetrics(t *testing.T) {
	zone := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/sea/political_tension/strait-of-hormuz",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		State: map[string]horizonmodels.MetricValue{
			horizonmodels.MetricPathImportanceStrategic:           {Value: 0.52},
			horizonmodels.MetricPathImportanceBridge:              {Value: 0.53},
			horizonmodels.MetricPathImportanceBusinessBlastRadius: {Value: 0.56},
			horizonmodels.MetricPathImportanceConflictBlastRadius: {Value: 0.84},
		},
	}
	overlay := BuildPropagationNodeOverlay(zone)
	if overlay.PrimaryRole != "zone_field" {
		t.Fatalf("expected zone_field role, got %+v", overlay)
	}
	if !overlay.HasRole("bridge") || !overlay.HasExposure("market_demand") {
		t.Fatalf("expected affected zone bridge and market exposure, got %+v", overlay)
	}
	if overlay.CriticalityScore < 0.55 {
		t.Fatalf("expected affected zone criticality to be elevated, got %+v", overlay)
	}
}
