package regstress

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestEvaluatePathStress(t *testing.T) {
	st := EvaluatePathStress(RegulatoryImpactPath{
		Legs: []RegulatoryImpactLeg{
			{PassThrough: 0.8},
			{PassThrough: 0.5},
		},
	})
	if st.CumulativeExposure <= 0 {
		t.Fatalf("expected positive exposure, got %v", st.CumulativeExposure)
	}
	if st.ObservabilityScore <= 0 || st.ObservabilityScore > 1 {
		t.Fatalf("observability out of range: %v", st.ObservabilityScore)
	}
}

func TestEvaluateScenario(t *testing.T) {
	base := RegulatoryImpactPath{
		Legs: []RegulatoryImpactLeg{{FromPath: "/mechanism/regulatory/x", ToPath: "/org/a", RelationType: "APPLIES_TO", PassThrough: 0.9}},
	}
	res := EvaluateScenario(base, RegulatoryScenario{ID: "t1", Overrides: map[string]float64{"/mechanism/regulatory/x": 1.2}})
	if res.Delta == 0 && res.BaseStress == 0 {
		t.Fatalf("expected non-zero stress, base=%v scenario=%v", res.BaseStress, res.ScenarioStress)
	}
}

func TestBuildPathsFromRelations_Bounded(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			SourceCanonicalPath: "/mechanism/regulatory/m",
			TargetCanonicalPath: "/loc/ae/01/dubai",
			RelationType:        "IN_GEO_SCOPE",
			Qualifiers:          map[string]interface{}{"lag_quarters": 2},
		},
		{
			SourceCanonicalPath: "/mechanism/regulatory/m",
			TargetCanonicalPath: "/community/ae/industry-association/dubai-family-business-network",
			RelationType:        "AFFECTS",
			Qualifiers:          map[string]interface{}{"measurement_lag_months": 6},
		},
		{SourceCanonicalPath: "/community/ae/industry-association/dubai-family-business-network", TargetCanonicalPath: "/media/linkedin/dubai-business-founders", RelationType: "OPERATES_VIA"},
	}
	paths := BuildPathsFromRelations(rels, "/domain/x", 3)
	if len(paths) < 2 {
		t.Fatalf("expected geo/community/media paths, got %d", len(paths))
	}
	foundGeo := false
	foundMedia := false
	for _, path := range paths {
		if len(path.Legs) == 0 {
			continue
		}
		last := path.Legs[len(path.Legs)-1]
		switch last.ToPath {
		case "/loc/ae/01/dubai":
			foundGeo = true
			if path.Legs[0].LagQuarters != 2 {
				t.Fatalf("expected lag from qualifiers, got %+v", path.Legs[0])
			}
		case "/media/linkedin/dubai-business-founders":
			foundMedia = true
		}
	}
	if !foundGeo {
		t.Fatalf("expected path terminating at geo scope")
	}
	if !foundMedia {
		t.Fatalf("expected path terminating at media channel")
	}
}
