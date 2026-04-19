package graphenrich

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func ApplyFragmentHeuristics(fragment *horizonmodels.HorizonFragment) bool {
	return ApplyFragmentHeuristicsWithContext(fragment, FragmentContext{})
}

func ApplyFragmentHeuristicsWithContext(fragment *horizonmodels.HorizonFragment, ctx FragmentContext) bool {
	if fragment == nil {
		return false
	}
	changed := false
	report := AnalyzeFragmentHeuristicsWithContext(fragment, ctx)
	if applyFragmentReport(fragment, report) {
		changed = true
	}
	if len(fragment.MarketSegments) > 0 {
		norm := taxonomy.NormalizeSegmentPaths(fragment.MarketSegments)
		if !equalStringSlices(fragment.MarketSegments, norm) {
			fragment.MarketSegments = norm
			changed = true
		}
	}
	return changed
}

func ApplyEdgeHeuristics(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) bool {
	if edge == nil {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	changed := false
	report := AnalyzeEdgeHeuristics(edge, sourcePath, targetPath)
	normalizedVia, viaChanged := normalizeTransportViaQualifiers(edge.Qualifiers)
	if viaChanged {
		changed = true
	}
	if normalizeTemporalModality(edge) {
		changed = true
	}
	if string(edgeFacet(edge)) == "" {
		if facet := strings.TrimSpace(report.Facet.Value); facet != "" && report.Facet.Confidence >= 0.34 {
			edge.Qualifiers[QualifierFacet] = facet
			changed = true
		}
	}

	rel := edge.Relation
	switch {
	case rel == ah.RelationNameBeneficiallyOwns:
		if syncOwnershipPercent(edge) {
			changed = true
		}
		if !hasOwnershipPercent(edge) {
			if addEdgeEnrichmentHint(edge, "What is the ownership percentage for this beneficially_owns relation?") {
				changed = true
			}
		}
	case rel == ah.RelationNameInvestedIn:
		if !hasInvestmentAmount(edge) {
			if addEdgeEnrichmentHint(edge, "What was the investment amount for this invested_in relation?") {
				changed = true
			}
		}
		if !hasRoundQualifier(edge) {
			if addEdgeEnrichmentHint(edge, "What investment round was this (seed/series_a/series_b/etc.)?") {
				changed = true
			}
		}
	case edge.IsTransmissionSensitive():
		if normalizeTransmissionQualifiers(edge) {
			changed = true
		}
		if applyTransmissionRouteHeuristics(edge, sourcePath, targetPath, normalizedVia) {
			changed = true
		}
		if applyTransmissionContractMetadata(edge) {
			changed = true
		}
		if addTransmissionHints(edge) {
			changed = true
		}
	case isEmploymentRelation(rel):
		if lacksTemporalClarity(edge) {
			if addEdgeEnrichmentHint(edge, "Is this relationship current or historical? Provide edgeModality and validFrom/validTo dates when available.") {
				changed = true
			}
		}
	}
	if writeEdgeHeuristicReport(edge, report) {
		changed = true
	}
	return changed
}

func fragmentText(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)),
		strings.ToLower(strings.TrimSpace(fragment.Description)),
		strings.ToLower(strings.TrimSpace(fragment.GeoFeatureType)),
		strings.ToLower(strings.TrimSpace(fragment.MarketDomain)),
		strings.ToLower(strings.TrimSpace(fragment.MarketVertical)),
		strings.ToLower(strings.TrimSpace(fragment.GovType)),
		strings.ToLower(strings.TrimSpace(fragment.EntityPath)),
		strings.ToLower(strings.TrimSpace(fragment.ParentPath)),
	}
	for _, v := range fragment.Labels {
		parts = append(parts, strings.ToLower(strings.TrimSpace(v)))
	}
	for _, tag := range fragment.Tags {
		parts = append(parts, strings.ToLower(strings.TrimSpace(tag)))
	}
	for _, seg := range fragment.MarketSegments {
		parts = append(parts, strings.ToLower(strings.TrimSpace(seg)))
	}
	for _, path := range fragment.IndustryPaths {
		parts = append(parts, strings.ToLower(strings.TrimSpace(path)))
	}
	return strings.Join(parts, " ")
}

func geoDepth(path string) int {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 || parts[0] != "loc" {
		return 0
	}
	return len(parts) - 1
}

func hasTag(tags []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(tag)) == want {
			return true
		}
	}
	return false
}

func normalizeTransportVia(q map[string]interface{}) []string {
	if len(q) == 0 {
		return nil
	}
	raw, found := q[QualifierTransportVia]
	if !found {
		for _, alias := range []string{"transportVia", "route_via", "routeVia", "transit_via", "via"} {
			if aliasRaw, aliasFound := q[alias]; aliasFound {
				raw = aliasRaw
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	switch v := raw.(type) {
	case []string:
		return compactStrings(v)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return compactStrings(out)
	default:
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return compactStrings([]string{s})
		}
		return nil
	}
}

func normalizeTransportViaQualifiers(q map[string]interface{}) ([]string, bool) {
	normalized := normalizeTransportVia(q)
	if len(normalized) == 0 {
		return nil, false
	}
	if existing, ok := q[QualifierTransportVia]; ok && sameTransportVia(existing, normalized) {
		return normalized, false
	}
	q[QualifierTransportVia] = normalized
	return normalized, true
}

func sameTransportVia(existing interface{}, want []string) bool {
	got := normalizeTransportVia(map[string]interface{}{QualifierTransportVia: existing})
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func compactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func looksRouteConstrained(sourcePath, targetPath string, via []string) bool {
	text := strings.ToLower(strings.Join(append([]string{sourcePath, targetPath}, via...), " "))
	for _, token := range []string{"strait", "canal", "hormuz", "mandeb", "suez", "malacca", "port", "terminal", "pipeline", "airport", "shipping"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func ParseNumericQualifier(raw interface{}) (float64, bool) {
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

func syncOwnershipPercent(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	changed := false
	if edge.Metrics != nil {
		if mv, ok := edge.Metrics["ownership_percent"]; ok {
			if _, exists := edge.Qualifiers["ownership_percent"]; !exists {
				edge.Qualifiers["ownership_percent"] = mv.Value
				changed = true
			}
		}
	}
	if edge.Metrics == nil {
		edge.Metrics = map[string]horizonmodels.MetricValue{}
	}
	if _, ok := edge.Metrics["ownership_percent"]; !ok {
		if v, ok := ParseNumericQualifier(edge.Qualifiers["ownership_percent"]); ok {
			edge.Metrics["ownership_percent"] = metricPercent(v, edge)
			changed = true
			return changed
		}
		if v, ok := ParseNumericQualifier(edge.Qualifiers["stake_pct"]); ok {
			edge.Metrics["ownership_percent"] = metricPercent(v, edge)
			edge.Qualifiers["ownership_percent"] = v
			changed = true
		}
	}
	return changed
}

func metricPercent(v float64, edge *horizonmodels.DependencyEdge) horizonmodels.MetricValue {
	unit := "percent"
	conf := edge.Properties.Criticality
	if conf <= 0 || conf > 1 {
		conf = 0.7
	}
	now := time.Now().UTC()
	return horizonmodels.MetricValue{Value: v, Unit: &unit, Confidence: conf, ObservedAt: now, Provenance: horizonmodels.Provenance{Method: "best_effort_edge_sync"}}
}

func hasOwnershipPercent(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	if edge.Metrics != nil {
		if _, ok := edge.Metrics["ownership_percent"]; ok {
			return true
		}
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers["ownership_percent"]); ok {
		return true
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers["stake_pct"]); ok {
		return true
	}
	return false
}

func hasInvestmentAmount(edge *horizonmodels.DependencyEdge) bool {
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

func hasRoundQualifier(edge *horizonmodels.DependencyEdge) bool {
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

func normalizeTemporalModality(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	if raw, ok := edge.Qualifiers["edgeModality"]; ok {
		if m := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))); m != "" && m != "<nil>" {
			if edge.Qualifiers["edgeModality"] != m {
				edge.Qualifiers["edgeModality"] = m
				return true
			}
			return false
		}
	}
	switch {
	case edge.ValidTo != nil:
		if time.Now().After(*edge.ValidTo) {
			edge.Qualifiers["edgeModality"] = "historical"
			return true
		}
	case edge.ValidFrom != nil:
		if time.Now().Before(*edge.ValidFrom) {
			edge.Qualifiers["edgeModality"] = "projected"
			return true
		}
	}
	return false
}

func lacksTemporalClarity(edge *horizonmodels.DependencyEdge) bool {
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

func addEdgeEnrichmentHint(edge *horizonmodels.DependencyEdge, hint string) bool {
	if edge == nil || strings.TrimSpace(hint) == "" {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	const key = "enrichmentHints"
	cur, ok := edge.Qualifiers[key]
	if !ok {
		edge.Qualifiers[key] = []string{hint}
		return true
	}
	switch v := cur.(type) {
	case []string:
		for _, s := range v {
			if s == hint {
				return false
			}
		}
		edge.Qualifiers[key] = append(v, hint)
		return true
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
			edge.Qualifiers[key] = out
			return true
		}
		return false
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == hint {
			edge.Qualifiers[key] = []string{hint}
			return true
		}
		edge.Qualifiers[key] = []string{s, hint}
		return true
	}
}

func normalizeTransmissionQualifiers(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	changed := false
	changed = normalizeNumericAlias(edge.Qualifiers, horizonmodels.QualifierThroughputShare, "flow_share", "shipment_share", "route_share", "throughputShare") || changed
	changed = normalizeNumericAlias(edge.Qualifiers, horizonmodels.QualifierReroutePenalty, "reroute_penalty_score", "substitution_penalty", "reroutePenalty") || changed
	changed = normalizeNumericAlias(edge.Qualifiers, horizonmodels.QualifierRerouteLatencyDeltaHrs, "latency_delta_hours", "reroute_delay_hours", "rerouteLatencyDeltaHours") || changed
	changed = normalizeNumericAlias(edge.Qualifiers, horizonmodels.QualifierRerouteCostDeltaPct, "cost_delta_pct", "reroute_cost_percent", "rerouteCostDeltaPct") || changed
	changed = normalizeNumericAlias(edge.Qualifiers, horizonmodels.QualifierCapacityConstraint, "capacity_utilization", "congestion_score", "capacityUtilization") || changed
	if edge.Metrics == nil {
		edge.Metrics = map[string]horizonmodels.MetricValue{}
	}
	changed = metricToQualifier(edge, horizonmodels.QualifierThroughputShare) || changed
	changed = metricToQualifier(edge, horizonmodels.QualifierReroutePenalty) || changed
	changed = metricToQualifier(edge, horizonmodels.QualifierRerouteLatencyDeltaHrs) || changed
	changed = metricToQualifier(edge, horizonmodels.QualifierRerouteCostDeltaPct) || changed
	changed = metricToQualifier(edge, horizonmodels.QualifierCapacityConstraint) || changed
	unitHours := "hours"
	unitPct := "percent"
	changed = qualifierToMetric(edge, horizonmodels.QualifierThroughputShare, nil) || changed
	changed = qualifierToMetric(edge, horizonmodels.QualifierReroutePenalty, nil) || changed
	changed = qualifierToMetric(edge, horizonmodels.QualifierRerouteLatencyDeltaHrs, &unitHours) || changed
	changed = qualifierToMetric(edge, horizonmodels.QualifierRerouteCostDeltaPct, &unitPct) || changed
	changed = qualifierToMetric(edge, horizonmodels.QualifierCapacityConstraint, nil) || changed
	return changed
}

func applyTransmissionRouteHeuristics(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string, via []string) bool {
	if edge == nil || len(via) == 0 || !looksRouteConstrained(sourcePath, targetPath, via) {
		return false
	}
	changed := false
	if _, ok := ParseNumericQualifier(edge.Qualifiers[horizonmodels.QualifierReroutePenalty]); !ok {
		edge.Qualifiers[horizonmodels.QualifierReroutePenalty] = 0.38
		changed = true
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers[horizonmodels.QualifierCapacityConstraint]); !ok {
		edge.Qualifiers[horizonmodels.QualifierCapacityConstraint] = 0.22
		changed = true
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers["substitutability"]); !ok {
		edge.Qualifiers["substitutability"] = 0.18
		changed = true
	}
	return changed
}

func addTransmissionHints(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	facet := edgeFacet(edge)
	if facet == "" {
		return addEdgeEnrichmentHint(edge, "Classify edge facet (logistics/infrastructure/supply/payment/distribution/regulatory/...) to improve transmission reasoning.")
	}
	needsTransmissionQualifiers := facet == FacetLogistics || facet == FacetInfrastructure || facet == FacetSupply || facet == FacetDistribution || facet == FacetPayment
	if !needsTransmissionQualifiers {
		return false
	}
	changed := false
	if !hasAnyNumericQualifier(edge, horizonmodels.QualifierThroughputShare, horizonmodels.QualifierCapacityConstraint, "substitutability") {
		changed = addEdgeEnrichmentHint(edge, "Add at least one transmission qualifier: throughput_share (0..1), capacity_constraint (0..1), or substitutability (0..1).") || changed
	}
	if facet == FacetLogistics && !hasAnyNumericQualifier(edge, horizonmodels.QualifierReroutePenalty, horizonmodels.QualifierRerouteLatencyDeltaHrs, horizonmodels.QualifierRerouteCostDeltaPct) {
		changed = addEdgeEnrichmentHint(edge, "For logistics dependencies, add reroute_penalty (0..1) and reroute latency/cost deltas (hours/percent).") || changed
	}
	return changed
}

func applyTransmissionContractMetadata(edge *horizonmodels.DependencyEdge) bool {
	// Under AH the transmission gate is on edge.Relation +
	// quantitative qualifier presence (see DependencyEdge.IsTransmission
	// Sensitive). The legacy DEPENDS_ON+facet narrowing is gone.
	if edge == nil || !edge.IsTransmissionSensitive() {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	facet := edgeFacet(edge)
	hasBase := hasAnyNumericQualifier(edge, horizonmodels.QualifierThroughputShare, horizonmodels.QualifierCapacityConstraint, "substitutability")
	hasReroute := hasAnyNumericQualifier(edge, horizonmodels.QualifierReroutePenalty, horizonmodels.QualifierRerouteLatencyDeltaHrs, horizonmodels.QualifierRerouteCostDeltaPct)
	hasAny := hasBase || hasReroute
	status := TransmissionContractStatusComplete
	missing := make([]string, 0, 3)
	if facet == "" {
		missing = appendMissingTransmissionField(missing, "facet")
	}
	if !hasBase {
		missing = appendMissingTransmissionField(missing, "throughput_share|capacity_constraint|substitutability")
	}
	if facet == FacetLogistics && !hasReroute {
		missing = appendMissingTransmissionField(missing, "reroute_penalty|reroute_latency_delta_hours|reroute_cost_delta_pct")
	}
	if len(missing) > 0 {
		if hasAny {
			status = TransmissionContractStatusPartial
		} else {
			status = TransmissionContractStatusMissing
		}
	}
	mode := TransmissionContractModeInferred
	if edge.HasAnyTransmissionQualifier() {
		mode = TransmissionContractModeExplicit
	}
	changed := setQualifier(edge.Qualifiers, QualifierTransmissionContractVersion, TransmissionContractVersionV1)
	changed = setQualifier(edge.Qualifiers, QualifierTransmissionContractStatus, status) || changed
	changed = setQualifier(edge.Qualifiers, QualifierTransmissionContractMode, mode) || changed
	if len(missing) > 0 {
		changed = setQualifier(edge.Qualifiers, QualifierTransmissionContractMissing, missing) || changed
	} else if _, ok := edge.Qualifiers[QualifierTransmissionContractMissing]; ok {
		delete(edge.Qualifiers, QualifierTransmissionContractMissing)
		changed = true
	}
	return changed
}

func normalizeNumericAlias(dst map[string]interface{}, canonical string, aliases ...string) bool {
	if dst == nil {
		return false
	}
	if v, ok := ParseNumericQualifier(dst[canonical]); ok {
		if cur, ok2 := dst[canonical].(float64); ok2 && cur == v {
			return false
		}
		dst[canonical] = v
		return true
	}
	for _, alias := range aliases {
		if v, ok := ParseNumericQualifier(dst[alias]); ok {
			dst[canonical] = v
			return true
		}
	}
	return false
}

func metricToQualifier(edge *horizonmodels.DependencyEdge, key string) bool {
	if edge == nil || edge.Metrics == nil || edge.Qualifiers == nil {
		return false
	}
	if _, exists := edge.Qualifiers[key]; exists {
		return false
	}
	if mv, ok := edge.Metrics[key]; ok {
		edge.Qualifiers[key] = mv.Value
		return true
	}
	return false
}

func qualifierToMetric(edge *horizonmodels.DependencyEdge, key string, unit *string) bool {
	if edge == nil || edge.Metrics == nil {
		return false
	}
	if _, exists := edge.Metrics[key]; exists {
		return false
	}
	v, ok := ParseNumericQualifier(edge.Qualifiers[key])
	if !ok {
		return false
	}
	conf := edge.Properties.Criticality
	if conf <= 0 || conf > 1 {
		conf = 0.7
	}
	now := time.Now().UTC()
	edge.Metrics[key] = horizonmodels.MetricValue{Value: v, Unit: unit, Confidence: conf, ObservedAt: now, Provenance: horizonmodels.Provenance{Method: "best_effort_transmission_sync"}}
	return true
}

func hasAnyNumericQualifier(edge *horizonmodels.DependencyEdge, keys ...string) bool {
	if edge == nil {
		return false
	}
	for _, key := range keys {
		if _, ok := ParseNumericQualifier(edge.Qualifiers[key]); ok {
			return true
		}
		if edge.Metrics != nil {
			if _, ok := edge.Metrics[key]; ok {
				return true
			}
		}
	}
	return false
}

func appendMissingTransmissionField(fields []string, field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return fields
	}
	for _, existing := range fields {
		if existing == field {
			return fields
		}
	}
	return append(fields, field)
}

func setQualifier(q map[string]interface{}, key string, value interface{}) bool {
	if q == nil {
		return false
	}
	if cur, ok := q[key]; ok && fmt.Sprint(cur) == fmt.Sprint(value) {
		return false
	}
	q[key] = value
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
