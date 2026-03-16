package signals

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func TestVertexEdgeCandidates_ExtractsSourceAndTarget(t *testing.T) {
	got := vertexEdgeCandidates(ai.SearchResult{
		Document: ai.SearchDocument{
			StructData: map[string]interface{}{
				"sourceFragmentId": "cp_source",
				"sourcePath":       "/zone/qa/military_conflict/ras-laffan",
				"targetFragmentId": "cp_target",
				"targetPath":       "/product/finished/mechanical/assembly/helium",
			},
		},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d (%v)", len(got), got)
	}
	if got[0].fragmentID != "cp_source" || got[0].canonicalPath != "/zone/qa/military_conflict/ras-laffan" {
		t.Fatalf("unexpected source candidate: %+v", got[0])
	}
	if got[1].fragmentID != "cp_target" || got[1].canonicalPath != "/product/finished/mechanical/assembly/helium" {
		t.Fatalf("unexpected target candidate: %+v", got[1])
	}
}
