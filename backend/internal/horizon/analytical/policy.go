package analytical

import (
	"strings"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type RefreshPolicy struct {
	RefreshClass      string
	RecomputeTriggers []string
	StaleAfter        time.Duration
	CostClass         string
}

func PrimitiveRefreshPolicy(primitiveType string) RefreshPolicy {
	switch strings.TrimSpace(primitiveType) {
	case "route_concentration", "low_redundancy_corridor", "chokepoint_dependency", "route_fragility", "customs_friction", "disruption_sensitive_node", "path_criticality", "fragment_risk_signal":
		return RefreshPolicy{
			RefreshClass:      "signal_driven",
			RecomputeTriggers: []string{"graph_change", "metric_change", "signal_change"},
			StaleAfter:        24 * time.Hour,
			CostClass:         "cheap",
		}
	case "fragment_impact_signal", "fragment_metric_signal":
		return RefreshPolicy{
			RefreshClass:      "metric_driven",
			RecomputeTriggers: []string{"metric_change", "signal_change"},
			StaleAfter:        24 * time.Hour,
			CostClass:         "cheap",
		}
	case "supply_link_risk":
		return RefreshPolicy{
			RefreshClass:      "immediate",
			RecomputeTriggers: []string{"graph_change", "metric_change"},
			StaleAfter:        7 * 24 * time.Hour,
			CostClass:         "cheap",
		}
	default:
		return RefreshPolicy{
			RefreshClass:      "scheduled",
			RecomputeTriggers: []string{"graph_change", "metric_change"},
			StaleAfter:        7 * 24 * time.Hour,
			CostClass:         "cheap",
		}
	}
}

func AggregateRefreshPolicy(aggregateType, layer string) RefreshPolicy {
	if strings.EqualFold(strings.TrimSpace(layer), "scenario") || strings.EqualFold(strings.TrimSpace(aggregateType), "disruption_impact_summary") {
		return RefreshPolicy{
			RefreshClass:      "scenario_only",
			RecomputeTriggers: []string{"simulation_run", "query_demand"},
			StaleAfter:        72 * time.Hour,
			CostClass:         "expensive",
		}
	}
	switch strings.TrimSpace(aggregateType) {
	case "weak_points_summary":
		return RefreshPolicy{
			RefreshClass:      "immediate",
			RecomputeTriggers: []string{"primitive_change", "signal_change", "watch_change"},
			StaleAfter:        24 * time.Hour,
			CostClass:         "medium",
		}
	default:
		return RefreshPolicy{
			RefreshClass:      "scheduled",
			RecomputeTriggers: []string{"primitive_change"},
			StaleAfter:        24 * time.Hour,
			CostClass:         "medium",
		}
	}
}

func ApplyPrimitivePolicy(primitive *hmodels.AnalyticalPrimitive, now time.Time) {
	if primitive == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := PrimitiveRefreshPolicy(primitive.PrimitiveType)
	if strings.TrimSpace(primitive.RefreshClass) == "" {
		primitive.RefreshClass = policy.RefreshClass
	}
	if len(primitive.RecomputeTriggers) == 0 {
		primitive.RecomputeTriggers = append([]string(nil), policy.RecomputeTriggers...)
	}
	if primitive.StaleAt == nil && policy.StaleAfter > 0 {
		value := now.Add(policy.StaleAfter)
		primitive.StaleAt = &value
	}
}

func ApplyAggregatePolicy(aggregate *hmodels.AnalyticalAggregate, now time.Time) {
	if aggregate == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := AggregateRefreshPolicy(aggregate.AggregateType, aggregate.Layer)
	if strings.TrimSpace(aggregate.RefreshClass) == "" {
		aggregate.RefreshClass = policy.RefreshClass
	}
	if len(aggregate.RecomputeTriggers) == 0 {
		aggregate.RecomputeTriggers = append([]string(nil), policy.RecomputeTriggers...)
	}
	if aggregate.StaleAt == nil && policy.StaleAfter > 0 {
		value := now.Add(policy.StaleAfter)
		aggregate.StaleAt = &value
	}
}
