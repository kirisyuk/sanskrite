package policy

import "strings"

// DecisionTelemetryInput describes a policy decision event for aggregated KPI counters.
type DecisionTelemetryInput struct {
	Kind          string
	Decision      PolicyDecision
	SourceType    string
	TargetType    string
	RelationType  string
	FinalStatus   string
	FinalReason   string
	Canonicalized bool
}

// DecisionTelemetrySnapshot is a normalized telemetry payload derived from a decision event.
type DecisionTelemetrySnapshot struct {
	Counters           map[string]int64
	InvalidTripleDims  map[string]string
	InvalidTripleCount map[string]int64
}

// BuildDecisionTelemetrySnapshot converts policy outcomes into counters required by Big Bang KPI tracking.
// The returned counters are numerators; rates are computed downstream as counter / policy_decision_total.
func BuildDecisionTelemetrySnapshot(in DecisionTelemetryInput) DecisionTelemetrySnapshot {
	counters := map[string]int64{
		"policy_decision_total": 1,
	}

	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = "edge"
	}
	counters["policy_decision_kind_"+strings.ToLower(kind)] = 1

	action := strings.ToLower(strings.TrimSpace(in.Decision.Action))
	status := strings.ToLower(strings.TrimSpace(in.FinalStatus))
	reason := strings.TrimSpace(in.FinalReason)
	if reason == "" {
		if v, _ := in.Decision.Params["reason"].(string); strings.TrimSpace(v) != "" {
			reason = strings.TrimSpace(v)
		}
	}
	reasonLower := strings.ToLower(reason)

	autoFixCounted := false
	penaltyCounted := false
	quarantineCounted := false

	switch action {
	case "reject":
		counters["policy_reject_non_blocking_rate"] = 1
	case "auto_fix", "reverse":
		counters["auto_fix_rate"] = 1
		counters["auto_fix_hit_rate"] = 1
		autoFixCounted = true
	case "allow_with_penalty", "penalty":
		counters["penalty_rate"] = 1
		penaltyCounted = true
	case "quarantine", "quarantine_soft":
		counters["quarantine_rate"] = 1
		quarantineCounted = true
	}

	switch status {
	case "auto_fixed":
		if !autoFixCounted {
			counters["auto_fix_rate"] = 1
			counters["auto_fix_hit_rate"] = 1
			autoFixCounted = true
		}
	case "penalized":
		if !penaltyCounted {
			counters["penalty_rate"] = 1
			penaltyCounted = true
		}
	case "quarantined":
		if !quarantineCounted {
			counters["quarantine_rate"] = 1
			quarantineCounted = true
		}
	}

	if in.Canonicalized && !autoFixCounted {
		counters["auto_fix_rate"] = 1
		counters["auto_fix_hit_rate"] = 1
		autoFixCounted = true
	}

	if strings.Contains(reasonLower, "missing direction policy") {
		counters["unknown_direction_policy_count"] = 1
	}
	if strings.Contains(reasonLower, "type_path_mismatch") {
		counters["type_path_mismatch_rate"] = 1
	}
	if action == "allow_with_penalty" {
		if strings.Contains(reasonLower, "weak-link") ||
			strings.Contains(reasonLower, "semantically plausible") ||
			strings.Contains(reasonLower, "influences to sphere") {
			counters["false_positive_reject_rate"] = 1
		}
	}

	invalidAction := action == "quarantine" || action == "quarantine_soft" || action == "reject"
	unknownDirection := counters["unknown_direction_policy_count"] > 0
	if (invalidAction || unknownDirection) &&
		strings.TrimSpace(in.RelationType) != "" &&
		strings.TrimSpace(in.SourceType) != "" &&
		strings.TrimSpace(in.TargetType) != "" {
		return DecisionTelemetrySnapshot{
			Counters: counters,
			InvalidTripleDims: map[string]string{
				"edgeType":   strings.TrimSpace(in.RelationType),
				"sourceType": strings.TrimSpace(in.SourceType),
				"targetType": strings.TrimSpace(in.TargetType),
				"kind":       strings.ToLower(kind),
			},
			InvalidTripleCount: map[string]int64{
				"top_invalid_triples": 1,
			},
		}
	}

	return DecisionTelemetrySnapshot{Counters: counters}
}
