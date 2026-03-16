package horizon

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func TestVertexFragmentIDFromResult_UsesURI(t *testing.T) {
	got := vertexFragmentIDFromResult(ai.SearchResult{
		Document: ai.SearchDocument{
			URI: "gs://bucket/global/horizon_fragments/cp_123.json",
		},
	})
	if got != "cp_123" {
		t.Fatalf("vertexFragmentIDFromResult()=%q, want cp_123", got)
	}
}

func TestVertexEdgeFragmentIDs_DeduplicatesAndKeepsBothEndpoints(t *testing.T) {
	got := vertexEdgeFragmentIDs(ai.SearchResult{
		Document: ai.SearchDocument{
			StructData: map[string]interface{}{
				"sourceFragmentId": "cp_source",
				"targetFragmentId": "cp_target",
			},
		},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 ids, got %d (%v)", len(got), got)
	}
	if got[0] != "cp_source" || got[1] != "cp_target" {
		t.Fatalf("unexpected ids: %v", got)
	}
}
