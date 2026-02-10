package scout

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyBestEffortEdgeEnrichment_OwnsSyncsOwnershipPercent(t *testing.T) {
	edge := &models.DependencyEdge{
		Type:       models.DependencyTypeOwns,
		Qualifiers: map[string]interface{}{},
		Metrics: map[string]models.MetricValue{
			"ownership_percent": {Value: 62.5},
		},
	}
	applyBestEffortEdgeEnrichment(edge)
	if got, ok := edge.Qualifiers["ownership_percent"]; !ok || got == nil {
		t.Fatalf("expected qualifiers.ownership_percent to be mirrored from metrics")
	}
}

func TestApplyBestEffortEdgeEnrichment_OwnsBackfillsMetricFromQualifier(t *testing.T) {
	edge := &models.DependencyEdge{
		Type:       models.DependencyTypeOwns,
		Qualifiers: map[string]interface{}{"stake_pct": "45%"},
	}
	applyBestEffortEdgeEnrichment(edge)
	mv, ok := edge.Metrics["ownership_percent"]
	if !ok {
		t.Fatalf("expected metrics.ownership_percent to be backfilled")
	}
	if mv.Value != 45 {
		t.Fatalf("expected ownership_percent=45, got %.2f", mv.Value)
	}
}

func TestApplyBestEffortEdgeEnrichment_AddsTemporalHintForHistoricalEmployment(t *testing.T) {
	edge := &models.DependencyEdge{
		Type:       models.DependencyTypeEmployedBy,
		Qualifiers: map[string]interface{}{"edgeModality": "historical"},
	}
	applyBestEffortEdgeEnrichment(edge)
	hints, ok := edge.Qualifiers["enrichmentHints"]
	if !ok || hints == nil {
		t.Fatalf("expected enrichment hint for historical employment without validTo")
	}
}

func TestApplyBestEffortEdgeEnrichment_SetsHistoricalFromDates(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	edge := &models.DependencyEdge{
		Type:       models.DependencyTypeManages,
		Qualifiers: map[string]interface{}{},
		ValidTo:    &past,
	}
	applyBestEffortEdgeEnrichment(edge)
	if got := edge.Qualifiers["edgeModality"]; got != "historical" {
		t.Fatalf("expected edgeModality=historical, got %v", got)
	}
}
