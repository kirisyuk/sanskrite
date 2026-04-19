package models

import (
	"math"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// transmission_qualifiers.go — runtime/analytical qualifier surface for
// edges that carry quantitative flow semantics (throughput share,
// reroute friction, capacity bottleneck, …).
//
// Status (P1, schema_version 2.0)
// -------------------------------
// These qualifiers are NOT YET promoted to the AH attribute_spec — they
// live on the free-form Qualifiers bag. Promotion is tracked under the
// `materialized-payload-rewrite` follow-up; once promoted, the matching
// ah.AttrKey* constant replaces the local string literal and getters
// move to attributes.go.
//
// Relation gating
// ---------------
// IsTransmissionSensitive used to key on the legacy DependencyType
// enum + DependencyFacet. Both are retired (see migration_map.yaml).
// The new gate is the AH RelationName: a fixed set of canonical
// supply-chain relations (`supplies`, `leg_connects`, `passes_through`,
// `transships_at`) is always treated as transmission-sensitive; any
// other relation falls back to "sensitive iff quantitative qualifier
// already present" — preserving the original intent of "if the data is
// there, model it" without requiring upstream code to migrate first.

// Transmission qualifier keys (Qualifiers bag; not yet AH attr_spec).
const (
	QualifierThroughputShare        = "throughput_share"            // 0..1 share of flow routed through this edge
	QualifierReroutePenalty         = "reroute_penalty"             // 0..1 substitution/reroute friction
	QualifierRerouteLatencyDeltaHrs = "reroute_latency_delta_hours" // additional hours when rerouting
	QualifierRerouteCostDeltaPct    = "reroute_cost_delta_pct"      // percent cost increase when rerouting
	QualifierCapacityConstraint     = "capacity_constraint"         // 0..1 (1 = severe bottleneck)

	// Legacy synonyms accepted by the readers below for back-compat with
	// pre-AH heuristics output. Writers MUST use the canonical Qualifier*
	// keys above.
	QualifierAliasFlowShare           = "flow_share"
	QualifierAliasShipmentShare       = "shipment_share"
	QualifierAliasRouteShare          = "route_share"
	QualifierAliasReroutePenaltyScore = "reroute_penalty_score"
	QualifierAliasSubstitutionPenalty = "substitution_penalty"
	QualifierAliasLatencyDeltaHours   = "latency_delta_hours"
	QualifierAliasRerouteDelayHours   = "reroute_delay_hours"
	QualifierAliasCostDeltaPct        = "cost_delta_pct"
	QualifierAliasRerouteCostPercent  = "reroute_cost_percent"
	QualifierAliasCapacityUtilization = "capacity_utilization"
	QualifierAliasCongestionScore     = "congestion_score"
	QualifierAliasSubstitutability    = "substitutability"
)

// transmissionRelations is the closed set of AH relations that always
// carry transmission semantics regardless of qualifier presence.
var transmissionRelations = map[ah.RelationName]struct{}{
	ah.RelationNameSupplies:      {},
	ah.RelationNameLegConnects:   {},
	ah.RelationNamePassesThrough: {},
	ah.RelationNameTransshipsAt:  {},
}

// allTransmissionQualifierKeys is the union of canonical + alias keys
// HasAnyTransmissionQualifier looks up. Kept as a package-level slice so
// its allocation amortises across the call.
var allTransmissionQualifierKeys = []string{
	QualifierThroughputShare,
	QualifierReroutePenalty,
	QualifierRerouteLatencyDeltaHrs,
	QualifierRerouteCostDeltaPct,
	QualifierCapacityConstraint,
	QualifierAliasFlowShare,
	QualifierAliasShipmentShare,
	QualifierAliasRouteShare,
	QualifierAliasReroutePenaltyScore,
	QualifierAliasSubstitutionPenalty,
	QualifierAliasLatencyDeltaHours,
	QualifierAliasRerouteDelayHours,
	QualifierAliasCostDeltaPct,
	QualifierAliasRerouteCostPercent,
	QualifierAliasCapacityUtilization,
	QualifierAliasCongestionScore,
	QualifierAliasSubstitutability,
}

// GetThroughputShare returns 0..1 share of flow this edge transmits.
// Reads canonical key first, then known synonyms; returns 0 when missing.
func (e *DependencyEdge) GetThroughputShare() float64 {
	return clamp01(e.firstQualifierFloat(
		QualifierThroughputShare,
		QualifierAliasFlowShare,
		QualifierAliasShipmentShare,
		QualifierAliasRouteShare,
	))
}

// GetReroutePenalty returns 0..1 friction score for substitution/reroute.
func (e *DependencyEdge) GetReroutePenalty() float64 {
	return clamp01(e.firstQualifierFloat(
		QualifierReroutePenalty,
		QualifierAliasReroutePenaltyScore,
		QualifierAliasSubstitutionPenalty,
	))
}

// GetCapacityConstraint returns 0..1 capacity-bottleneck score.
// Accepts capacity_utilization in 0..100 form and rescales to 0..1.
func (e *DependencyEdge) GetCapacityConstraint() float64 {
	v := e.firstQualifierFloat(
		QualifierCapacityConstraint,
		QualifierAliasCapacityUtilization,
		QualifierAliasCongestionScore,
	)
	if v > 1 && v <= 100 {
		v = v / 100
	}
	return clamp01(v)
}

// GetRerouteLatencyDeltaHours returns additional hours expected when
// rerouting through alternative paths. Negative inputs collapse to 0.
func (e *DependencyEdge) GetRerouteLatencyDeltaHours() float64 {
	v := e.firstQualifierFloat(
		QualifierRerouteLatencyDeltaHrs,
		QualifierAliasLatencyDeltaHours,
		QualifierAliasRerouteDelayHours,
	)
	if v < 0 {
		return 0
	}
	return v
}

// GetRerouteCostDeltaPct returns percent cost increase on reroute.
// Accepts string forms with trailing % (e.g. "35%"). Negative inputs
// collapse to 0.
func (e *DependencyEdge) GetRerouteCostDeltaPct() float64 {
	for _, key := range []string{
		QualifierRerouteCostDeltaPct,
		QualifierAliasCostDeltaPct,
		QualifierAliasRerouteCostPercent,
	} {
		if e == nil || e.Qualifiers == nil {
			break
		}
		raw, ok := e.Qualifiers[key]
		if !ok || raw == nil {
			continue
		}
		if s, ok := raw.(string); ok {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				if f < 0 {
					return 0
				}
				return f
			}
		}
		if v, ok := attrFloat(e.Qualifiers, key); ok {
			if v < 0 {
				return 0
			}
			return v
		}
	}
	return 0
}

// HasAnyTransmissionQualifier reports whether the edge carries at least
// one transmission-related qualifier OR metric (canonical or alias).
// Used by the propagation layer to decide whether to invoke the
// transmission model for this edge.
func (e *DependencyEdge) HasAnyTransmissionQualifier() bool {
	if e == nil {
		return false
	}
	for _, key := range allTransmissionQualifierKeys {
		if e.Qualifiers != nil {
			if _, ok := attrFloat(e.Qualifiers, key); ok {
				return true
			}
		}
		if e.Metrics != nil {
			if _, ok := e.Metrics[key]; ok {
				return true
			}
		}
	}
	return false
}

// IsTransmissionSensitive reports whether the edge should be processed
// by the transmission model. Returns true when:
//
//  1. The relation is in the closed transmission-relation set
//     (`supplies`, `leg_connects`, `passes_through`, `transships_at`); OR
//  2. The edge already carries at least one transmission qualifier or
//     metric (preserves the legacy "if data is there, model it" intent).
//
// The legacy DEPENDS_ON+facet gate is retired with the DependencyFacet
// enum (see migration_map.yaml — facets dropped entirely).
func (e *DependencyEdge) IsTransmissionSensitive() bool {
	if e == nil {
		return false
	}
	if _, ok := transmissionRelations[e.Relation]; ok {
		return true
	}
	return e.HasAnyTransmissionQualifier()
}

// firstQualifierFloat returns the first numeric qualifier value found
// for any of the supplied keys, or 0 when none is set.
func (e *DependencyEdge) firstQualifierFloat(keys ...string) float64 {
	if e == nil || e.Qualifiers == nil {
		return 0
	}
	for _, key := range keys {
		if v, ok := attrFloat(e.Qualifiers, key); ok {
			return v
		}
	}
	return 0
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}
