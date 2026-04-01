package services

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestCreateSnapshotIncludesGraphSources(t *testing.T) {
	svc := &ArtifactContextService{}
	lensID := "lens-123"
	reportType := "consulting_memo"
	sources := models.ArtifactContextSources{
		GraphSources: []models.ArtifactGraphContextSource{
			{
				Kind:       "lens_supply_chain_report",
				LensID:     &lensID,
				ReportType: &reportType,
			},
		},
	}
	context := &GatheredContext{
		GraphContexts: []GraphContext{
			{
				Kind:          "lens_supply_chain_report",
				LensID:        &lensID,
				ReportType:    &reportType,
				Subject:       "Visible Supply Chain",
				VisiblePaths:  intPtr(12),
				VisibleHubs:   intPtr(3),
				VisibleChains: intPtr(8),
				Sections: map[string]GraphSectionContext{
					"what_this_network_is":    {ID: "what_this_network_is"},
					"management_implications": {ID: "management_implications"},
				},
			},
		},
	}

	snapshot := svc.createSnapshot(sources, context)
	if len(snapshot.GraphSources) != 1 {
		t.Fatalf("expected 1 graph source, got %d", len(snapshot.GraphSources))
	}
	item := snapshot.GraphSources[0]
	if item.Kind != "lens_supply_chain_report" {
		t.Fatalf("unexpected kind: %s", item.Kind)
	}
	if item.LensID == nil || *item.LensID != lensID {
		t.Fatalf("unexpected lens id: %#v", item.LensID)
	}
	if item.VisiblePaths == nil || *item.VisiblePaths != 12 {
		t.Fatalf("unexpected visible paths: %#v", item.VisiblePaths)
	}
	if len(item.SectionIDs) != 2 {
		t.Fatalf("expected 2 section ids, got %d", len(item.SectionIDs))
	}
}

func intPtr(value int) *int {
	return &value
}
