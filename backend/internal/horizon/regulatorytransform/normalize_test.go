package regulatorytransform

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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
		models.DependencyTypeDependsOn,
	)
	if !changed || got != models.DependencyTypeUsesMechanism {
		t.Fatalf("expected USES_MECHANISM, changed=%v got=%s", changed, got)
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
		models.DependencyTypeInfluences,
	)
	if !changed || got != models.DependencyTypeImposesCondition {
		t.Fatalf("expected IMPOSES_CONDITION, changed=%v got=%s", changed, got)
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
		models.DependencyTypeDependsOn,
	)
	if !changed || got != models.DependencyTypeImposesCondition {
		t.Fatalf("expected last IMPOSES_CONDITION, changed=%v got=%s", changed, got)
	}
}
