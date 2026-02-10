package policy

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestCheckEdgeCompleteness_OwnsMissingPercent(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e1",
		Type: horizonmodels.DependencyTypeOwns,
	}
	violations := pc.checkEdgeCompleteness(edge)
	if len(violations) == 0 {
		t.Fatalf("expected at least one completeness violation")
	}
	found := false
	for _, v := range violations {
		if v.Rule == "edge_completeness_owns_percent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edge_completeness_owns_percent violation")
	}
}

func TestCheckEdgeCompleteness_InvestsMissingAmountAndRound(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e2",
		Type: horizonmodels.DependencyTypeInvestsIn,
	}
	violations := pc.checkEdgeCompleteness(edge)
	if len(violations) < 2 {
		t.Fatalf("expected 2 violations for missing amount and round, got %d", len(violations))
	}
}
