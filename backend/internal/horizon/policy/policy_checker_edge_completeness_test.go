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

func TestCheckEdgeCompleteness_OwnsMissingShareType(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e3",
		Type: horizonmodels.DependencyTypeOwns,
		Qualifiers: map[string]interface{}{
			"ownership_percent": 87,
		},
	}
	violations := pc.checkEdgeCompleteness(edge)
	found := false
	for _, v := range violations {
		if v.Rule == "edge_completeness_owns_share_type" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edge_completeness_owns_share_type violation")
	}
}

func TestCheckEdgeCompleteness_ControlsMissingMechanism(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e4",
		Type: horizonmodels.DependencyTypeControls,
	}
	violations := pc.checkEdgeCompleteness(edge)
	found := false
	for _, v := range violations {
		if v.Rule == "edge_completeness_controls_mechanism" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edge_completeness_controls_mechanism violation")
	}
}

func TestCheckEdgeCompleteness_AffectsMissingTransmissionQualifiers(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e5",
		Type: horizonmodels.DependencyTypeAffects,
	}
	violations := pc.checkEdgeCompleteness(edge)
	found := false
	for _, v := range violations {
		if v.Rule == "edge_completeness_transmission_qualifiers" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edge_completeness_transmission_qualifiers violation")
	}
}

func TestCheckEdgeCompleteness_InGeoScopeMissingScopeBasis(t *testing.T) {
	pc := &PolicyChecker{}
	edge := &horizonmodels.DependencyEdge{
		ID:   "e6",
		Type: horizonmodels.DependencyTypeInGeoScope,
	}
	violations := pc.checkEdgeCompleteness(edge)
	found := false
	for _, v := range violations {
		if v.Rule == "edge_completeness_geo_scope_basis" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edge_completeness_geo_scope_basis violation")
	}
}
