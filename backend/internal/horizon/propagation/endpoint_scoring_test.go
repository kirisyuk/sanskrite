package propagation

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestScoreEndpointConflictRemoteFacilityBeatsLocalOrganization(t *testing.T) {
	profile := DefaultEndpointScoringProfile()
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
	}

	remoteFacility := &horizonmodels.HorizonFragment{
		ID:            "fac-cn-1",
		CanonicalPath: "/fac/cn/lng-intensive-plant",
		Class:         FragmentTypeFacility,
		IndexPaths:    []string{"/industry/lng", "/replaceability-low"},
	}
	localOrg := &horizonmodels.HorizonFragment{
		ID:            "org-ir-1",
		CanonicalPath: "/org/ir/shipping-association",
		Class:         FragmentTypeOrganization,
	}

	facScore := scoreEndpoint(signal, "ir", remoteFacility, []string{"seed", "mid", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)
	orgScore := scoreEndpoint(signal, "ir", localOrg, []string{"seed", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)

	if !facScore.Remote {
		t.Fatal("expected remote facility to be remote")
	}
	if facScore.Interestingness <= orgScore.Interestingness {
		t.Fatalf("expected remote facility interestingness > local org, got %.3f <= %.3f", facScore.Interestingness, orgScore.Interestingness)
	}
	if facScore.Interestingness < profile.BusinessThreshold {
		t.Fatalf("expected remote facility to clear business threshold %.2f, got %.3f", profile.BusinessThreshold, facScore.Interestingness)
	}
}

func TestEndpointSigmoidMonotonic(t *testing.T) {
	if endpointSigmoid(-1) >= endpointSigmoid(0) || endpointSigmoid(0) >= endpointSigmoid(1) {
		t.Fatal("expected sigmoid to be strictly increasing around 0")
	}
}

func TestRerankDiverseEndpointsAvoidsSameCountrySameTypeFlood(t *testing.T) {
	profile := DefaultEndpointScoringProfile()
	candidates := []EndpointInterestingness{
		{FragmentID: "a", CanonicalPath: "/fac/ir/a", FragmentType: FragmentTypeFacility, Country: "ir", Interestingness: 0.96},
		{FragmentID: "b", CanonicalPath: "/fac/ir/b", FragmentType: FragmentTypeFacility, Country: "ir", Interestingness: 0.95},
		{FragmentID: "c", CanonicalPath: "/market/de/energy/gas", FragmentType: FragmentTypeMarket, Country: "de", Interestingness: 0.89},
		{FragmentID: "d", CanonicalPath: "/fac/cn/lng-plant", FragmentType: FragmentTypeFacility, Country: "cn", Interestingness: 0.88},
	}

	selected := rerankDiverseEndpoints(candidates, 3, profile)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected endpoints, got %d", len(selected))
	}
	if selected[0].FragmentID != "a" {
		t.Fatalf("expected top candidate first, got %s", selected[0].FragmentID)
	}
	if selected[1].FragmentID == "b" {
		t.Fatalf("expected second pick to diversify away from same-country same-type duplicate, got %s", selected[1].FragmentID)
	}
}

func TestScoreTraceEndpointRewardsCrossBorderBusinessPhysics(t *testing.T) {
	profile := DefaultEndpointScoringProfile()
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
	}
	remoteFacility := &horizonmodels.HorizonFragment{
		ID:            "fac-cn-1",
		CanonicalPath: "/fac/cn/lng-intensive-plant",
		Class:         FragmentTypeFacility,
		IndexPaths:    []string{"/industry/lng", "/replaceability-low"},
	}
	trace := SignalPropagationTraceEvent{
		Outcome:      "propagated",
		ToPath:       remoteFacility.CanonicalPath,
		Hops:         3,
		Scope:        "cross_border",
		Channel:      "economic",
		AxisOut:      horizonmodels.AxisPhysicalFlowSupply,
		AmplitudeOut: 0.63,
		Factors: map[string]float64{
			"throughput_factor":          1.20,
			"capacity_constraint_factor": 1.10,
			"waveguide_factor":           1.08,
		},
	}

	score := scoreTraceEndpoint(signal, "ir", remoteFacility, trace, profile)
	if !score.Remote {
		t.Fatal("expected trace-scored facility to be remote")
	}
	if score.Interestingness < profile.BusinessThreshold {
		t.Fatalf("expected cross-border business trace to clear business threshold %.2f, got %.3f", profile.BusinessThreshold, score.Interestingness)
	}
	if score.Hops != 3 {
		t.Fatalf("expected hops=3, got %d", score.Hops)
	}
}

func TestScoreEndpointPenalizesSphereTailAgainstConcreteFacility(t *testing.T) {
	profile := DefaultEndpointScoringProfile()
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
	}
	sphere := &horizonmodels.HorizonFragment{
		ID:            "sphere-1",
		CanonicalPath: "/sphere/geopolitics",
		Class:         FragmentTypeGeoRegion,
	}
	facility := &horizonmodels.HorizonFragment{
		ID:            "fac-de-1",
		CanonicalPath: "/fac/de/critical-plant",
		Class:         FragmentTypeFacility,
		IndexPaths:    []string{"/replaceability-low"},
	}
	sphereScore := scoreEndpoint(signal, "ir", sphere, []string{"seed", "mid", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)
	facScore := scoreEndpoint(signal, "ir", facility, []string{"seed", "mid", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)
	if facScore.Interestingness <= sphereScore.Interestingness {
		t.Fatalf("expected concrete facility > sphere hub, got facility=%.3f sphere=%.3f", facScore.Interestingness, sphereScore.Interestingness)
	}
}

func TestScoreEndpointPenalizesGenericLocTail(t *testing.T) {
	profile := DefaultEndpointScoringProfile()
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
	}
	genericLoc := &horizonmodels.HorizonFragment{
		ID:            "loc-1",
		CanonicalPath: "/loc/ir/na/na",
		Class:         FragmentTypeGeoRegion,
	}
	facility := &horizonmodels.HorizonFragment{
		ID:            "fac-cn-1",
		CanonicalPath: "/fac/cn/lng-plant",
		Class:         FragmentTypeFacility,
	}
	locScore := scoreEndpoint(signal, "ir", genericLoc, []string{"seed", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)
	facScore := scoreEndpoint(signal, "ir", facility, []string{"seed", "mid", "tail"}, nil, map[string]*horizonmodels.HorizonFragment{}, context.Background(), profile)
	if facScore.Interestingness <= locScore.Interestingness {
		t.Fatalf("expected concrete facility > generic loc, got facility=%.3f loc=%.3f", facScore.Interestingness, locScore.Interestingness)
	}
}
