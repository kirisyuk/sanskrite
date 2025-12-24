package scout

import (
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// horizonVertexClient returns a Vertex AI Search client scoped to the Horizon Fragments datastore, if configured.
// We rely on datastore isolation (no filters) because our current JSON "document" schema datastore does not
// support filtering on structData.*.
func horizonVertexClient(base *ai.VertexAISearchClient) *ai.VertexAISearchClient {
	if base == nil {
		return nil
	}
	ds := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
	if ds == "" {
		return base
	}
	return base.WithDataStore(ds)
}




