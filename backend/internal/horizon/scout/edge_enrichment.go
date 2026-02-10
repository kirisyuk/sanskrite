package scout

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// applyBestEffortEdgeEnrichment adds soft completeness metadata for edges.
// It never blocks edge creation.
func applyBestEffortEdgeEnrichment(edge *models.DependencyEdge) {
	if edge == nil {
		return
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}

	switch edge.Type {
	case models.DependencyTypeOwns:
		syncOwnershipPercent(edge)
		if !hasOwnershipPercent(edge) {
			addEdgeEnrichmentHint(edge, "What is the ownership percentage for this OWNS relation?")
		}
	case models.DependencyTypeInvestsIn:
		if !hasInvestmentAmount(edge) {
			addEdgeEnrichmentHint(edge, "What was the investment amount for this INVESTS_IN relation?")
		}
		if !hasRoundQualifier(edge) {
			addEdgeEnrichmentHint(edge, "What investment round was this (seed/series_a/series_b/etc.)?")
		}
	case models.DependencyTypeEmployedBy, models.DependencyTypeBoardMemberOf, models.DependencyTypeManages:
		normalizeTemporalModality(edge)
		if lacksTemporalClarity(edge) {
			addEdgeEnrichmentHint(edge, "Is this relationship current or historical? Provide edgeModality and validFrom/validTo dates when available.")
		}
	default:
		normalizeTemporalModality(edge)
	}
}

func syncOwnershipPercent(edge *models.DependencyEdge) {
	if edge == nil {
		return
	}
	// Metrics -> qualifiers
	if edge.Metrics != nil {
		if mv, ok := edge.Metrics["ownership_percent"]; ok {
			if _, exists := edge.Qualifiers["ownership_percent"]; !exists {
				edge.Qualifiers["ownership_percent"] = mv.Value
			}
		}
	}
	// Qualifiers -> metrics (ownership_percent / stake_pct alias)
	if edge.Metrics == nil {
		edge.Metrics = map[string]models.MetricValue{}
	}
	if _, ok := edge.Metrics["ownership_percent"]; !ok {
		if v, ok := parseNumericQualifier(edge.Qualifiers["ownership_percent"]); ok {
			edge.Metrics["ownership_percent"] = metricPercent(v, edge)
			return
		}
		if v, ok := parseNumericQualifier(edge.Qualifiers["stake_pct"]); ok {
			edge.Metrics["ownership_percent"] = metricPercent(v, edge)
			edge.Qualifiers["ownership_percent"] = v
		}
	}
}

func metricPercent(v float64, edge *models.DependencyEdge) models.MetricValue {
	unit := "percent"
	conf := edge.Properties.Criticality
	if conf <= 0 || conf > 1 {
		conf = 0.7
	}
	now := time.Now().UTC()
	return models.MetricValue{
		Value:      v,
		Unit:       &unit,
		Confidence: conf,
		ObservedAt: now,
		Provenance: models.Provenance{
			Method: "best_effort_edge_sync",
		},
	}
}

func hasOwnershipPercent(edge *models.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	if edge.Metrics != nil {
		if _, ok := edge.Metrics["ownership_percent"]; ok {
			return true
		}
	}
	if _, ok := parseNumericQualifier(edge.Qualifiers["ownership_percent"]); ok {
		return true
	}
	if _, ok := parseNumericQualifier(edge.Qualifiers["stake_pct"]); ok {
		return true
	}
	return false
}

func hasInvestmentAmount(edge *models.DependencyEdge) bool {
	if edge == nil || edge.Metrics == nil {
		return false
	}
	if _, ok := edge.Metrics["investment_amount"]; ok {
		return true
	}
	if _, ok := edge.Metrics["round_size_total"]; ok {
		return true
	}
	return false
}

func hasRoundQualifier(edge *models.DependencyEdge) bool {
	if edge == nil || edge.Qualifiers == nil {
		return false
	}
	for _, k := range []string{"round", "fundingRound", "roundType"} {
		if v, ok := edge.Qualifiers[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			return true
		}
	}
	return false
}

func normalizeTemporalModality(edge *models.DependencyEdge) {
	if edge == nil {
		return
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	if raw, ok := edge.Qualifiers["edgeModality"]; ok {
		if m := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))); m != "" && m != "<nil>" {
			edge.Qualifiers["edgeModality"] = m
			return
		}
	}
	switch {
	case edge.ValidTo != nil:
		if time.Now().After(*edge.ValidTo) {
			edge.Qualifiers["edgeModality"] = "historical"
		}
	case edge.ValidFrom != nil:
		if time.Now().Before(*edge.ValidFrom) {
			edge.Qualifiers["edgeModality"] = "projected"
		}
	}
}

func lacksTemporalClarity(edge *models.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	modality := ""
	if edge.Qualifiers != nil {
		if raw, ok := edge.Qualifiers["edgeModality"]; ok {
			modality = strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
			if modality == "<nil>" {
				modality = ""
			}
		}
	}
	if modality == "historical" && edge.ValidTo == nil {
		return true
	}
	if modality == "" && edge.ValidFrom == nil && edge.ValidTo == nil {
		return true
	}
	return false
}

func addEdgeEnrichmentHint(edge *models.DependencyEdge, hint string) {
	if edge == nil || strings.TrimSpace(hint) == "" {
		return
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	const key = "enrichmentHints"
	cur, ok := edge.Qualifiers[key]
	if !ok {
		edge.Qualifiers[key] = []string{hint}
		return
	}
	switch v := cur.(type) {
	case []string:
		for _, s := range v {
			if s == hint {
				return
			}
		}
		edge.Qualifiers[key] = append(v, hint)
	case []interface{}:
		out := make([]string, 0, len(v)+1)
		seen := map[string]struct{}{}
		for _, raw := range v {
			s := strings.TrimSpace(fmt.Sprint(raw))
			if s == "" {
				continue
			}
			if _, exists := seen[s]; exists {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		if _, exists := seen[hint]; !exists {
			out = append(out, hint)
		}
		edge.Qualifiers[key] = out
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == hint {
			edge.Qualifiers[key] = []string{hint}
		} else {
			edge.Qualifiers[key] = []string{s, hint}
		}
	}
}

func parseNumericQualifier(raw interface{}) (float64, bool) {
	if raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		s = strings.TrimSuffix(s, "%")
		s = strings.ReplaceAll(s, ",", "")
		if s == "" {
			return 0, false
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
