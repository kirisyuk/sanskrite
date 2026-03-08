package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// applyBestEffortEdgeEnrichment centralizes deterministic edge enrichment in graphenrich.
// Optional endpointPaths can provide sourcePath,targetPath for better transport/facet inference.
func applyBestEffortEdgeEnrichment(edge *models.DependencyEdge, endpointPaths ...string) {
	sourcePath := ""
	targetPath := ""
	if len(endpointPaths) > 0 {
		sourcePath = endpointPaths[0]
	}
	if len(endpointPaths) > 1 {
		targetPath = endpointPaths[1]
	}
	graphenrich.ApplyEdgeHeuristics(edge, sourcePath, targetPath)
}

func parseNumericQualifier(raw interface{}) (float64, bool) {
	return graphenrich.ParseNumericQualifier(raw)
}
