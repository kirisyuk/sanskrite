package pathbuilder

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEvaluatePathSufficiency_ValidatedJoin(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef: "/org/us/supplier-a",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/processor-b",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-01-01"),
					"end":   metricTime("2025-12-31"),
				},
			},
			{
				SupplierRef: "/org/us/processor-b",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/customer-c",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-02-01"),
					"end":   metricTime("2025-11-30"),
				},
			},
		},
	}
	_, _, legs, _ := builder.pathToMaterializedPayload(context.Background(), path)
	sufficiency := builder.evaluatePathSufficiency(context.Background(), path, legs)
	if sufficiency == nil {
		t.Fatal("expected sufficiency")
	}
	if sufficiency.Status != models.SupplyChainSufficiencyValidated {
		t.Fatalf("status=%s, want %s", sufficiency.Status, models.SupplyChainSufficiencyValidated)
	}
	if len(sufficiency.JoinAssessments) != 1 || sufficiency.JoinAssessments[0].Status != models.SupplyChainSufficiencyValidated {
		t.Fatalf("join assessments=%+v", sufficiency.JoinAssessments)
	}
}

func TestEvaluatePathSufficiency_ForensicGapOnActorMismatch(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef: "/org/us/supplier-a",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/processor-b",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-01-01"),
					"end":   metricTime("2025-12-31"),
				},
			},
			{
				SupplierRef: "/org/us/unrelated-x",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/customer-c",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-02-01"),
					"end":   metricTime("2025-11-30"),
				},
			},
		},
	}
	_, _, legs, _ := builder.pathToMaterializedPayload(context.Background(), path)
	sufficiency := builder.evaluatePathSufficiency(context.Background(), path, legs)
	if sufficiency == nil {
		t.Fatal("expected sufficiency")
	}
	if sufficiency.Status != models.SupplyChainSufficiencyForensicGap {
		t.Fatalf("status=%s, want %s", sufficiency.Status, models.SupplyChainSufficiencyForensicGap)
	}
	if len(sufficiency.HardFailures) == 0 {
		t.Fatal("expected hard failures")
	}
}

func TestEvaluatePathSufficiency_HeuristicForRouteSensitiveWithoutTransport(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef: "/org/ae/supplier-a",
				ProductRef:  "/product/raw/hydrocarbon/distribution/lng",
				CustomerRef: "/org/in/customer-b",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-01-01"),
					"end":   metricTime("2025-12-31"),
				},
			},
		},
	}
	_, _, legs, _ := builder.pathToMaterializedPayload(context.Background(), path)
	sufficiency := builder.evaluatePathSufficiency(context.Background(), path, legs)
	if sufficiency == nil {
		t.Fatal("expected sufficiency")
	}
	if sufficiency.Status != models.SupplyChainSufficiencyHeuristic {
		t.Fatalf("status=%s, want %s", sufficiency.Status, models.SupplyChainSufficiencyHeuristic)
	}
	if len(sufficiency.OpenGaps) == 0 {
		t.Fatal("expected open gaps")
	}
}

func TestEvaluatePathSufficiency_ForensicGapOnBlockedJoinHint(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef: "/org/us/supplier-a",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/processor-b",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-01-01"),
					"end":   metricTime("2025-12-31"),
				},
			},
			{
				SupplierRef: "/org/us/processor-b",
				ProductRef:  "/product/component/test/shared-input",
				CustomerRef: "/org/us/customer-c",
				Metrics: map[string]models.MetricValue{
					"start": metricTime("2025-02-01"),
					"end":   metricTime("2025-11-30"),
				},
			},
		},
		JoinAssessmentHints: []models.SupplyChainJoinAssessment{
			{
				Index:         0,
				LeftLegIndex:  0,
				RightLegIndex: 1,
				EntityReuse:   models.SupplyChainJoinEntityReuseBlocked,
				Status:        models.SupplyChainSufficiencyForensicGap,
				Reasons:       []string{"non_allowlisted_reuse_ref:/org/us/invented-middleman"},
			},
		},
	}
	_, _, legs, _ := builder.pathToMaterializedPayload(context.Background(), path)
	sufficiency := builder.evaluatePathSufficiency(context.Background(), path, legs)
	if sufficiency == nil {
		t.Fatal("expected sufficiency")
	}
	if sufficiency.Status != models.SupplyChainSufficiencyForensicGap {
		t.Fatalf("status=%s, want %s", sufficiency.Status, models.SupplyChainSufficiencyForensicGap)
	}
	if got := sufficiency.JoinAssessments[0].EntityReuse; got != models.SupplyChainJoinEntityReuseBlocked {
		t.Fatalf("entityReuse=%s, want %s", got, models.SupplyChainJoinEntityReuseBlocked)
	}
}

func metricTime(value string) models.MetricValue {
	return models.MetricValue{Unit: &value}
}
