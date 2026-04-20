package qa

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestComputeChainImpacts_PathStressAndBindings(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		ID:           "chain-1",
		Kind:         horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey: "chain/demo",
		NodeRefs:     []string{"/org/qa/qatarenergy", "/fac/qa/ras-laffan", "/fac/nl/port-of-rotterdam"},
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/qa/qatarenergy"},
				{Class: ah.FragmentClassSite, Ref: "/fac/qa/ras-laffan"},
				{Class: ah.FragmentClassSite, Ref: "/fac/nl/port-of-rotterdam"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:            0,
					SupplierRef:      "/fac/qa/ras-laffan",
					CustomerRef:      "/fac/nl/port-of-rotterdam",
					TransportViaRefs: []string{"/sea-route/strait-of-hormuz"},
					FlowRefs:         []string{"lng_hormuz_eu"},
					ZoneRefs:         []string{"gulf_energy_arc"},
					AffectedZoneRefs: []string{"/zone/sea/political_tension/strait-of-hormuz"},
				},
			},
			&horizonmodels.SupplyChainPathStress{
				PathScore:          0.81,
				BottleneckLegIndex: 0,
				RecoveryLatencyHrs: 96,
				DominantFactors:    []string{"affected_zone_pressure", "reroute_penalty"},
			},
			nil,
		),
	}

	impacts := ComputeChainImpacts([]*horizonmodels.MaterializedArtifact{artifact}, []horizonmodels.NodeImpact{
		{CanonicalPath: "/fac/qa/ras-laffan", TotalAmplitude: 0.7},
		{CanonicalPath: "/fac/nl/port-of-rotterdam", TotalAmplitude: 0.5},
	})
	if len(impacts) != 1 {
		t.Fatalf("expected 1 chain impact, got %d", len(impacts))
	}
	got := impacts[0]
	if got.PathStressScore <= 0 {
		t.Fatalf("expected path stress score, got %+v", got)
	}
	if got.AffectedLegs != 1 || got.LegCount != 1 {
		t.Fatalf("expected affected leg counts, got %+v", got)
	}
	if len(got.FlowRefs) != 1 || got.FlowRefs[0] != "lng_hormuz_eu" {
		t.Fatalf("expected flow refs to be preserved, got %+v", got.FlowRefs)
	}
	if got.BottleneckSummary == "" {
		t.Fatalf("expected bottleneck summary, got %+v", got)
	}
}
