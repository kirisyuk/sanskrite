package pathquery

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestScoreSupplyChainOverlap_nilArtifact(t *testing.T) {
	s, m := scoreSupplyChainOverlap(nil, supplyChainLensContext{})
	if s != 0 || m != nil {
		t.Fatalf("%v %v", s, m)
	}
}

func TestScoreSupplyChainOverlap_nonZero(t *testing.T) {
	art := &horizonmodels.MaterializedArtifact{
		NodeRefs:         []string{"/a"},
		FlowRefs:         []string{"/f"},
		ZoneRefs:         []string{"/z"},
		AffectedZoneRefs: []string{"/az"},
	}
	ctx := supplyChainLensContext{
		NodeRefs:         []string{"/a"},
		FlowRefs:         []string{"/f"},
		ZoneRefs:         []string{"/z"},
		AffectedZoneRefs: []string{"/az"},
	}
	s, m := scoreSupplyChainOverlap(art, ctx)
	if s <= 0 || len(m) == 0 {
		t.Fatalf("score=%v matched=%v", s, m)
	}
}

func TestDescribeSupplyChainDiscovery(t *testing.T) {
	if describeSupplyChainDiscovery("node_overlap", nil) == "" {
		t.Fatal()
	}
	if describeSupplyChainDiscovery("unknown", []string{"x"}) == "" {
		t.Fatal()
	}
}
