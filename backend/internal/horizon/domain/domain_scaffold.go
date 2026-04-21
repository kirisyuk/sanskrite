package domain

import (
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Domain scaffold relations and edge qualifier shims used by the domain engine.

// domainScaffoldRelationTypes is the canonical relation set used for DOMAIN
// expansion prompts and domain-anchor sanitization. Names are kept as
// snake_case literals because several have been folded out of AH but are
// still consumed from legacy persisted graph data.
var domainScaffoldRelationTypes = []ah.RelationName{
	ah.RelationName("focuses_on"),
	ah.RelationName("involves_actor"),
	ah.RelationName("in_geo_scope"),
	ah.RelationName("about"),
	ah.RelationName("affects"),
	ah.RelationName("depends_on"),
	ah.RelationName("surfaces"),
	ah.RelationName("related_domain"),
}

// IsDomainScaffoldRelationType reports whether the relation belongs to the
// DOMAIN expansion scaffold set.
func IsDomainScaffoldRelationType(t ah.RelationName) bool {
	for _, allowed := range domainScaffoldRelationTypes {
		if allowed == t {
			return true
		}
	}
	return false
}

// edgeSubstitutability reads substitutability from the edge qualifier bag.

func edgeSubstitutability(edge *models.DependencyEdge) float64 {
	if edge == nil || edge.Qualifiers == nil {
		return 0
	}
	val, ok := edge.Qualifiers[models.QualifierSubstitutability]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0
		}
		return f
	}
	return 0
}
