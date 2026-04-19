package regulatorytransform

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestNormalizeRegulatoryEdgeType_usesMechanism(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterPair(Pair{
		FromPath:      "/decision/regulatory/eu-cbam-rollout",
		ToPath:        "/mechanism/regulatory/carbon-border-adjustment-mechanism",
		RelationKind:  "uses_mechanism",
		CanonicalType: "",
	})
	got, changed := reg.NormalizeRegulatoryEdgeType(
		"/decision/regulatory/eu-cbam-rollout",
		"/mechanism/regulatory/carbon-border-adjustment-mechanism",
		ah.RelationNameInfluences,
	)
	if !changed || got != ah.RelationNameWields {
		t.Fatalf("expected wields, changed=%v got=%s", changed, got)
	}
}

func TestNormalizeRegulatoryEdgeType_imposesCondition(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterPair(Pair{
		FromPath:     "/mechanism/regulatory/carbon-border-adjustment-mechanism",
		ToPath:       "/org/eu/steel-importer-acme",
		RelationKind: "imposes_condition",
	})
	got, changed := reg.NormalizeRegulatoryEdgeType(
		"/mechanism/regulatory/carbon-border-adjustment-mechanism",
		"/org/eu/steel-importer-acme",
		ah.RelationNameInfluences,
	)
	if !changed || got != ah.RelationNameImposes {
		t.Fatalf("expected imposes, changed=%v got=%s", changed, got)
	}
}

func TestRegisterPair_duplicateFromTo_lastWins(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterPair(Pair{
		FromPath:     "/decision/regulatory/a",
		ToPath:       "/mechanism/regulatory/b",
		RelationKind: "uses_mechanism",
	})
	reg.RegisterPair(Pair{
		FromPath:     "/decision/regulatory/a",
		ToPath:       "/mechanism/regulatory/b",
		RelationKind: "imposes_condition",
	})
	if reg.PairCount() != 1 {
		t.Fatalf("PairCount=%d, want 1", reg.PairCount())
	}
	got, changed := reg.NormalizeRegulatoryEdgeType(
		"/decision/regulatory/a",
		"/mechanism/regulatory/b",
		ah.RelationNameInfluences,
	)
	if !changed || got != ah.RelationNameImposes {
		t.Fatalf("expected last imposes, changed=%v got=%s", changed, got)
	}
}
