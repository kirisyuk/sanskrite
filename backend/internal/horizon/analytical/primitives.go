package analytical

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type SupplyLinkPrimitiveInput struct {
	TenantID            string
	ScopeRef            string
	ProductRef          string
	SupplierRef         string
	CustomerRef         string
	TransportViaRefs    []string
	Criticality         float64
	CoverageStatus      string
	GateStatus          string
	Summary             string
	PrimaryMechanism    string
	PrimaryMetricKey    string
	RouteFragility      float64
	RouteRerouteability float64
	MechanismScores     map[string]float64
	MechanismSummaries  map[string]string
	EffectiveMetrics    map[string]hmodels.MetricValue
	EvidenceFactIDs     []string
	SourceIDs           []string
	ComputedAt          time.Time
}

func BuildSupplyLinkPrimitives(in SupplyLinkPrimitiveInput) []*hmodels.AnalyticalPrimitive {
	now := in.ComputedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	baseBasis := compactRefs(append([]string{
		strings.TrimSpace(in.ScopeRef),
		strings.TrimSpace(in.SupplierRef),
		strings.TrimSpace(in.CustomerRef),
		strings.TrimSpace(in.ProductRef),
	}, in.TransportViaRefs...))
	base := func(primitiveType string, subjectRef string, severity float64, explanation string) *hmodels.AnalyticalPrimitive {
		primitive := &hmodels.AnalyticalPrimitive{
			TenantID:        strings.TrimSpace(in.TenantID),
			ScopeType:       "supply_link",
			ScopeRef:        strings.TrimSpace(in.ScopeRef),
			PrimitiveType:   strings.TrimSpace(primitiveType),
			Layer:           "baseline",
			SourceClass:     "pipeline",
			SubjectRef:      strings.TrimSpace(subjectRef),
			ProductRefs:     compactRefs([]string{in.ProductRef}),
			RouteRefs:       compactRefs(in.TransportViaRefs),
			BasisRefs:       baseBasis,
			BasisDigest:     primitiveBasisDigest(append(append([]string{}, baseBasis...), in.CoverageStatus, in.GateStatus, in.PrimaryMechanism, primitiveType, strconv.FormatFloat(severity, 'f', 4, 64))...),
			SeverityScore:   clamp01(severity),
			Confidence:      primitiveConfidence(in.EffectiveMetrics, 0.78),
			CoverageStatus:  strings.TrimSpace(in.CoverageStatus),
			GateStatus:      strings.TrimSpace(in.GateStatus),
			Explanation:     strings.TrimSpace(explanation),
			MissingData:     missingDataMarkers(in.CoverageStatus, in.GateStatus, in.EffectiveMetrics),
			EvidenceFactIDs: compactStrings(in.EvidenceFactIDs),
			SourceIDs:       compactStrings(in.SourceIDs),
			ComputedAt:      now,
			Qualifiers: map[string]interface{}{
				"primaryMechanism": strings.TrimSpace(in.PrimaryMechanism),
				"primaryMetricKey": strings.TrimSpace(in.PrimaryMetricKey),
			},
		}
		ApplyPrimitivePolicy(primitive, now)
		primitive.Normalize()
		return primitive
	}

	out := make([]*hmodels.AnalyticalPrimitive, 0, len(in.MechanismScores)+4)
	out = append(out, base("supply_link_risk", in.SupplierRef, in.Criticality, firstNonEmpty(strings.TrimSpace(in.Summary), "Deterministic supply-link risk primitive.")))
	if in.RouteFragility > 0 {
		out = append(out, base("route_concentration", firstNonEmptyRef(in.TransportViaRefs), in.RouteFragility, "Route concentration is elevated on the current corridor."))
	}
	if in.RouteRerouteability > 0 && in.RouteRerouteability < 0.45 {
		out = append(out, base("low_redundancy_corridor", firstNonEmptyRef(in.TransportViaRefs), 1-in.RouteRerouteability, "Rerouteability is low for the current corridor."))
	}
	for _, mechanismScore := range sortedMechanismScores(in.MechanismScores) {
		mechanism := mechanismScore.key
		score := mechanismScore.score
		explanation := in.MechanismSummaries[mechanism]
		subjectRef := in.SupplierRef
		if mechanism == "route_fragility" || mechanism == "customs_friction" {
			subjectRef = firstNonEmptyRef(in.TransportViaRefs)
		}
		if mechanism == "route_fragility" && len(in.TransportViaRefs) > 0 {
			for _, ref := range compactRefs(in.TransportViaRefs) {
				out = append(out, base("chokepoint_dependency", ref, score, firstNonEmpty(explanation, "Current corridor depends on a constrained transport waypoint.")))
			}
			continue
		}
		out = append(out, base(mechanism, subjectRef, score, explanation))
	}
	return dedupePrimitives(out)
}

func BuildFragmentStatePrimitives(fragment *hmodels.HorizonFragment, computedAt time.Time) []*hmodels.AnalyticalPrimitive {
	if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" || len(fragment.State) == 0 {
		return nil
	}
	now := computedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	metricNames := make([]string, 0, len(fragment.State))
	for metricName := range fragment.State {
		if !metricEligibleForPrimitive(metricName) {
			continue
		}
		metricNames = append(metricNames, strings.TrimSpace(metricName))
	}
	sort.Strings(metricNames)
	if len(metricNames) == 0 {
		return nil
	}
	scopeRef := strings.TrimSpace(fragment.CanonicalPath)
	nodeRefs := compactRefs([]string{scopeRef})
	productRefs := []string(nil)
	if fragment.Class == FragmentTypeProduct {
		productRefs = compactRefs([]string{scopeRef})
	}
	out := make([]*hmodels.AnalyticalPrimitive, 0, len(metricNames))
	for _, metricName := range metricNames {
		value := fragment.State[metricName]
		primitive := &hmodels.AnalyticalPrimitive{
			TenantID:        strings.TrimSpace(firstNonEmpty(fragment.TenantID, "global")),
			ScopeType:       "fragment",
			ScopeRef:        scopeRef,
			PrimitiveType:   fragmentMetricPrimitiveType(metricName),
			Layer:           fragmentMetricLayer(metricName),
			SourceClass:     "state_refresh",
			SubjectRef:      scopeRef,
			ProductRefs:     productRefs,
			NodeRefs:        nodeRefs,
			AffectedRefs:    nodeRefs,
			BasisRefs:       nodeRefs,
			BasisDigest:     primitiveBasisDigest(scopeRef, metricName, strconv.FormatFloat(value.Value, 'f', 6, 64), formatMetricUnit(value.Unit)),
			SeverityScore:   clamp01(absFloatAnalytical(value.Value)),
			Confidence:      fragmentMetricConfidence(value),
			Explanation:     fragmentMetricExplanation(metricName, value),
			EvidenceFactIDs: compactStrings(value.Provenance.DocumentIDs),
			SourceIDs:       compactStrings(append([]string{}, value.Provenance.SourceIDs...)),
			ComputedAt:      now,
			Qualifiers: map[string]interface{}{
				"metricName":  metricName,
				"metricValue": value.Value,
				"metricUnit":  formatMetricUnit(value.Unit),
			},
		}
		ApplyPrimitivePolicy(primitive, now)
		primitive.Normalize()
		out = append(out, primitive)
	}
	return dedupePrimitives(out)
}

func BuildPathPrimitivesFromArtifact(artifact *hmodels.MaterializedArtifact, evidenceIDs []string, sourceClass string, computedAt time.Time) []*hmodels.AnalyticalPrimitive {
	if artifact == nil || artifact.Kind != hmodels.MaterializedKindSupplyChainPath {
		return nil
	}
	parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return nil
	}
	now := computedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	basisRefs := compactRefs(append(append(append([]string{}, artifact.NodeRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...))
	base := func(primitiveType string, subjectRef string, severity float64, explanation string, nodeRefs, routeRefs, zoneRefs []string) *hmodels.AnalyticalPrimitive {
		primitive := &hmodels.AnalyticalPrimitive{
			TenantID:        strings.TrimSpace(artifact.TenantID),
			ScopeType:       "path_artifact",
			ScopeRef:        strings.TrimSpace(artifact.CanonicalKey),
			PrimitiveType:   strings.TrimSpace(primitiveType),
			Layer:           pathPrimitiveLayer(sourceClass),
			SourceClass:     firstNonEmpty(strings.TrimSpace(sourceClass), "runtime_read"),
			SubjectRef:      strings.TrimSpace(subjectRef),
			ProductRefs:     compactRefs([]string{artifact.ProductAxis}),
			NodeRefs:        compactRefs(nodeRefs),
			RouteRefs:       compactRefs(routeRefs),
			ZoneRefs:        compactRefs(zoneRefs),
			AffectedRefs:    compactRefs(artifact.AffectedZoneRefs),
			BasisRefs:       basisRefs,
			BasisDigest:     primitiveBasisDigest(append(append([]string{}, basisRefs...), primitiveType, subjectRef, artifact.CanonicalKey, strconv.FormatFloat(severity, 'f', 4, 64))...),
			SeverityScore:   clamp01(severity),
			Confidence:      0.72,
			Explanation:     strings.TrimSpace(explanation),
			EvidenceFactIDs: compactStrings(evidenceIDs),
			ComputedAt:      now,
		}
		ApplyPrimitivePolicy(primitive, now)
		primitive.Normalize()
		return primitive
	}

	out := make([]*hmodels.AnalyticalPrimitive, 0, 8)
	if parsed.Stress != nil {
		out = append(out, base(
			"path_criticality",
			firstNonEmpty(strings.TrimSpace(artifact.ProductAxis), strings.TrimSpace(artifact.AnchorCustomer)),
			parsed.Stress.PathScore,
			"Materialized path stress indicates a concentrated weak point in the current chain.",
			artifact.NodeRefs,
			artifact.FlowRefs,
			append(append([]string{}, artifact.ZoneRefs...), artifact.AffectedZoneRefs...),
		))
	}
	bottleneckIdx := -1
	if parsed.Stress != nil {
		bottleneckIdx = parsed.Stress.BottleneckLegIndex
	}
	for _, leg := range parsed.Legs {
		if leg.Stress == nil {
			continue
		}
		if leg.Index == bottleneckIdx {
			nodeRefs := compactRefs(append([]string{leg.SupplierRef, leg.CustomerRef}, leg.TransportViaRefs...))
			out = append(out, base(
				"disruption_sensitive_node",
				firstNonEmptyRef(append([]string{leg.SupplierRef, leg.CustomerRef}, leg.TransportViaRefs...)),
				leg.Stress.StressScore,
				"Leg-level stress marks this node/corridor as disruption-sensitive.",
				nodeRefs,
				append([]string{}, leg.FlowRefs...),
				append(append([]string{}, leg.ZoneRefs...), leg.AffectedZoneRefs...),
			))
			if leg.Stress.Rerouteability < 0.45 {
				out = append(out, base(
					"low_redundancy_corridor",
					firstNonEmptyRef(leg.TransportViaRefs),
					1-leg.Stress.Rerouteability,
					"Bottleneck leg has low rerouteability and limited corridor redundancy.",
					nodeRefs,
					append([]string{}, leg.FlowRefs...),
					append(append([]string{}, leg.ZoneRefs...), leg.AffectedZoneRefs...),
				))
			}
			for _, viaRef := range compactRefs(leg.TransportViaRefs) {
				out = append(out, base(
					"chokepoint_dependency",
					viaRef,
					leg.Stress.Fragility,
					"Path depends on a chokepoint-sensitive transport waypoint.",
					nodeRefs,
					append([]string{}, leg.FlowRefs...),
					append(append([]string{}, leg.ZoneRefs...), leg.AffectedZoneRefs...),
				))
			}
		}
	}
	return dedupePrimitives(out)
}

func metricEligibleForPrimitive(metricName string) bool {
	metricName = strings.TrimSpace(strings.ToLower(metricName))
	switch {
	case strings.HasPrefix(metricName, "/metric/risk/"):
		return true
	case strings.HasPrefix(metricName, "/metric/policy_impact/"):
		return true
	case strings.HasPrefix(metricName, "/metric/impact/"):
		return true
	default:
		return false
	}
}

func fragmentMetricPrimitiveType(metricName string) string {
	metricName = strings.TrimSpace(strings.ToLower(metricName))
	switch {
	case strings.HasPrefix(metricName, "/metric/risk/"):
		return "fragment_risk_signal"
	case strings.HasPrefix(metricName, "/metric/policy_impact/"), strings.HasPrefix(metricName, "/metric/impact/"):
		return "fragment_impact_signal"
	default:
		return "fragment_metric_signal"
	}
}

func fragmentMetricLayer(metricName string) string {
	metricName = strings.TrimSpace(strings.ToLower(metricName))
	switch {
	case strings.HasPrefix(metricName, "/metric/policy_impact/"), strings.HasPrefix(metricName, "/metric/impact/"):
		return "scenario"
	default:
		return "baseline"
	}
}

func fragmentMetricConfidence(value hmodels.MetricValue) float64 {
	if value.Confidence > 0 {
		return clamp01(value.Confidence)
	}
	return 0.65
}

func fragmentMetricExplanation(metricName string, value hmodels.MetricValue) string {
	parts := []string{
		"Fragment metric",
		humanizeMetricName(metricName),
		"signals updated local analytical pressure",
	}
	if unit := formatMetricUnit(value.Unit); unit != "" {
		parts = append(parts, "("+strconv.FormatFloat(value.Value, 'f', 3, 64)+" "+unit+")")
	} else {
		parts = append(parts, "("+strconv.FormatFloat(value.Value, 'f', 3, 64)+")")
	}
	return strings.Join(parts, " ")
}

func humanizeMetricName(metricName string) string {
	metricName = strings.TrimSpace(metricName)
	if metricName == "" {
		return "unknown_metric"
	}
	parts := strings.Split(metricName, "/")
	if len(parts) == 0 {
		return metricName
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return last
}

func formatMetricUnit(unit *string) string {
	if unit == nil {
		return ""
	}
	return strings.TrimSpace(*unit)
}

func absFloatAnalytical(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func sortedMechanismScores(in map[string]float64) []struct {
	key   string
	score float64
} {
	out := make([]struct {
		key   string
		score float64
	}, 0, len(in))
	for key, score := range in {
		key = strings.TrimSpace(key)
		if key == "" || score <= 0 {
			continue
		}
		out = append(out, struct {
			key   string
			score float64
		}{key: key, score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].key < out[j].key
		}
		return out[i].score > out[j].score
	})
	return out
}

func primitiveBasisDigest(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(compactStrings(parts), "\n")))
	return hex.EncodeToString(sum[:10])
}

func missingDataMarkers(coverageStatus, gateStatus string, metrics map[string]hmodels.MetricValue) []string {
	out := make([]string, 0, 4)
	if strings.TrimSpace(coverageStatus) != "" && !strings.EqualFold(strings.TrimSpace(coverageStatus), "complete") {
		out = append(out, "coverage:"+strings.TrimSpace(coverageStatus))
	}
	if strings.TrimSpace(gateStatus) != "" && !strings.EqualFold(strings.TrimSpace(gateStatus), "ready") {
		out = append(out, "gate:"+strings.TrimSpace(gateStatus))
	}
	for _, key := range []string{"share", "switch_time_days", "technical_substitutability", "regulatory_substitutability"} {
		if _, ok := metrics[strings.TrimSpace(key)]; !ok {
			out = append(out, "missing:"+key)
		}
	}
	return compactStrings(out)
}

func primitiveConfidence(metrics map[string]hmodels.MetricValue, fallback float64) float64 {
	if len(metrics) == 0 {
		return fallback
	}
	total := 0.0
	count := 0.0
	for _, metric := range metrics {
		if metric.Confidence <= 0 {
			continue
		}
		total += metric.Confidence
		count++
	}
	if count == 0 {
		return fallback
	}
	return clamp01(total / count)
}

func dedupePrimitives(in []*hmodels.AnalyticalPrimitive) []*hmodels.AnalyticalPrimitive {
	seen := make(map[string]*hmodels.AnalyticalPrimitive, len(in))
	order := make([]string, 0, len(in))
	for _, primitive := range in {
		if primitive == nil {
			continue
		}
		primitive.Normalize()
		key := strings.Join([]string{
			primitive.ScopeRef,
			primitive.PrimitiveType,
			primitive.SubjectRef,
			strings.Join(primitive.ProductRefs, "|"),
		}, "::")
		if existing, ok := seen[key]; ok {
			if primitive.SeverityScore > existing.SeverityScore {
				seen[key] = primitive
			}
			continue
		}
		seen[key] = primitive
		order = append(order, key)
	}
	out := make([]*hmodels.AnalyticalPrimitive, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return out
}

func compactRefs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
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
	sort.Strings(out)
	return out
}

func compactStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyRef(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func pathPrimitiveLayer(sourceClass string) string {
	sourceClass = strings.ToLower(strings.TrimSpace(sourceClass))
	switch {
	case sourceClass == "":
		return "analytical_overlay"
	case strings.Contains(sourceClass, "materialization"), strings.Contains(sourceClass, "pipeline"), strings.Contains(sourceClass, "baseline"):
		return "baseline"
	default:
		return "analytical_overlay"
	}
}
