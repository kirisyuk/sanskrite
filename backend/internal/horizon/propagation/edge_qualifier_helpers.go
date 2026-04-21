package propagation

import (
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// Edge facet constants and qualifier accessors for signal propagation.

// Edge facet constants — re-exported from graphenrich. The canonical
// definitions live next to the inference rules in graphenrich/facets.go.
const (
	FacetLogistics      = graphenrich.FacetLogistics
	FacetInfrastructure = graphenrich.FacetInfrastructure
	FacetSupply         = graphenrich.FacetSupply
	FacetDistribution   = graphenrich.FacetDistribution
	FacetPayment        = graphenrich.FacetPayment
)

// DependencyTypeTriggeredBy is a persisted-graph relation token outside AH.
const DependencyTypeTriggeredBy DependencyType = "triggered_by"

// edgeSubstitutability returns substitutability from the edge qualifier bag.
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

// edgeFacet reads the edge facet from the qualifier bag.
func edgeFacet(edge *models.DependencyEdge) graphenrich.EdgeFacet {
	if edge == nil || edge.Qualifiers == nil {
		return ""
	}
	val, ok := edge.Qualifiers[graphenrich.QualifierFacet]
	if !ok || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return graphenrich.EdgeFacet(strings.TrimSpace(v))
	case graphenrich.EdgeFacet:
		return v
	}
	return ""
}
