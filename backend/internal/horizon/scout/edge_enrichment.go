package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// applyBestEffortEdgeEnrichment centralizes deterministic edge enrichment in graphenrich.
// Optional endpointPaths can provide sourcePath,targetPath for better transport/facet inference.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
// scout still emits a handful of legacy snake_case relation literals
// during the AH migration window (e.g. depends_on, employed_by) that
// have no AH RelationName equivalent and therefore are invisible to the
// graphenrich relation switch. We translate them to the closest AH
// home for the duration of the heuristic pass so the contract metadata
// + temporal-clarity hooks fire, then restore the wire-form relation
// so downstream consumers (Firestore writers, scaffold preservers)
// keep seeing the legacy token they expect.
//
// Mappings honour migration_map.yaml:
//   - depends_on  -> supplies   (site->site rewrite branch is the
//     dominant transmission case; AH supplies is what triggers the
//     transmission contract metadata heuristics).
//   - employed_by -> member_of  (per-pair: drop_after_migration but
//     scout retains the temporal-clarity hint pre-drop so writers can
//     stamp validFrom/validTo before the relation is folded out).
func applyBestEffortEdgeEnrichment(edge *models.DependencyEdge, endpointPaths ...string) {
	sourcePath := ""
	targetPath := ""
	if len(endpointPaths) > 0 {
		sourcePath = endpointPaths[0]
	}
	if len(endpointPaths) > 1 {
		targetPath = endpointPaths[1]
	}
	originalRelation := edge.Relation
	switch originalRelation {
	case DependencyType("depends_on"):
		edge.Relation = ah.RelationNameSupplies
	case DependencyType("employed_by"):
		edge.Relation = ah.RelationNameMemberOf
	}
	graphenrich.ApplyEdgeHeuristics(edge, sourcePath, targetPath)
	if edge.Relation != originalRelation {
		edge.Relation = originalRelation
	}
}

func parseNumericQualifier(raw interface{}) (float64, bool) {
	return graphenrich.ParseNumericQualifier(raw)
}
