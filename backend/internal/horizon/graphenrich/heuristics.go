package graphenrich

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func ApplyFragmentHeuristics(fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	changed := false

	if infer := inferGeoFeatureType(fragment); strings.TrimSpace(fragment.GeoFeatureType) == "" && infer != "" {
		fragment.GeoFeatureType = infer
		changed = true
	}
	if infer := inferGovType(fragment); strings.TrimSpace(fragment.GovType) == "" && infer != "" {
		fragment.GovType = infer
		changed = true
	}
	if domain, vertical := inferMarketProfile(fragment); strings.TrimSpace(fragment.MarketDomain) == "" && domain != "" {
		fragment.MarketDomain = domain
		changed = true
		if strings.TrimSpace(fragment.MarketVertical) == "" && vertical != "" {
			fragment.MarketVertical = vertical
			changed = true
		}
	}
	if strings.TrimSpace(fragment.MarketVertical) == "" {
		if _, vertical := inferMarketProfile(fragment); vertical != "" {
			fragment.MarketVertical = vertical
			changed = true
		}
	}
	if len(fragment.MarketSegments) > 0 {
		norm := taxonomy.NormalizeSegmentPaths(fragment.MarketSegments)
		if !equalStringSlices(fragment.MarketSegments, norm) {
			fragment.MarketSegments = norm
			changed = true
		}
	}
	if len(fragment.IndustryPaths) == 0 {
		if inferred := inferIndustryPaths(fragment); len(inferred) > 0 {
			fragment.IndustryPaths = horizonmodels.NormalizeIndexPaths(16, inferred)
			changed = true
		}
	}
	if shouldTagChokepoint(fragment) && !hasTag(fragment.Tags, "chokepoint") {
		fragment.Tags = append(fragment.Tags, "chokepoint")
		changed = true
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
	normalizedVia, viaChanged := normalizeTransportViaQualifiers(edge.Qualifiers)
	if viaChanged {
		changed = true
	}
	if normalizeTemporalModality(edge) {
		changed = true
	}
	if strings.TrimSpace(string(edge.GetFacet())) == "" {
		if facet := inferEdgeFacet(edge, sourcePath, targetPath); facet != "" {
			edge.Qualifiers["facet"] = facet
			changed = true
		}
	}

	switch edge.Type {
	case horizonmodels.DependencyTypeOwns:
		if syncOwnershipPercent(edge) {
			changed = true
		}
		if !hasOwnershipPercent(edge) {
			if addEdgeEnrichmentHint(edge, "What is the ownership percentage for this OWNS relation?") {
				changed = true
			}
		}
	case horizonmodels.DependencyTypeInvestsIn:
		if !hasInvestmentAmount(edge) {
			if addEdgeEnrichmentHint(edge, "What was the investment amount for this INVESTS_IN relation?") {
				changed = true
			}
		}
		if !hasRoundQualifier(edge) {
			if addEdgeEnrichmentHint(edge, "What investment round was this (seed/series_a/series_b/etc.)?") {
				changed = true
			}
		}
	case horizonmodels.DependencyTypeDependsOn:
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
	case horizonmodels.DependencyTypeSuppliesTo, horizonmodels.DependencyTypeExtractsFrom, horizonmodels.DependencyTypeProduces:
		if applyTransmissionRouteHeuristics(edge, sourcePath, targetPath, normalizedVia) {
			changed = true
		}
	case horizonmodels.DependencyTypeEmployedBy, horizonmodels.DependencyTypeBoardMemberOf, horizonmodels.DependencyTypeManages:
		if lacksTemporalClarity(edge) {
			if addEdgeEnrichmentHint(edge, "Is this relationship current or historical? Provide edgeModality and validFrom/validTo dates when available.") {
				changed = true
			}
		}
	}

	return changed
}

func inferGeoFeatureType(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	text := fragmentText(fragment)
	for _, item := range []struct {
		token string
		kind  string
	}{
		{"hormuz", "strait"},
		{"mandeb", "strait"},
		{"bosporus", "strait"},
		{"malacca", "strait"},
		{"strait", "strait"},
		{"canal", "canal"},
		{"pass", "pass"},
		{"border-crossing", "border-crossing"},
		{"border crossing", "border-crossing"},
		{"pipeline-corridor", "pipeline-corridor"},
		{"pipeline corridor", "pipeline-corridor"},
		{"airspace", "airspace"},
		{"airport", "port"},
		{"seaport", "port"},
		{"port", "port"},
		{"city", "city"},
		{"province", "region"},
		{"region", "region"},
		{"state", "region"},
	} {
		if strings.Contains(text, item.token) {
			return item.kind
		}
	}
	if strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/loc/") {
		switch depth := geoDepth(fragment.CanonicalPath); {
		case depth >= 4:
			return "city"
		case depth == 3:
			return "region"
		case depth == 2:
			return "country"
		}
	}
	return ""
}

func inferGovType(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if !strings.HasPrefix(cp, "/gov/") {
		return ""
	}
	slug := pathSlug(cp)
	for _, item := range []struct {
		token string
		kind  string
	}{
		{"ministry-of-", "ministry"},
		{"department-of-", "department"},
		{"-authority", "authority"},
		{"-agency", "agency"},
		{"-service", "service"},
		{"-bureau", "bureau"},
		{"-office", "office"},
		{"-commission", "commission"},
		{"-council", "council"},
		{"-committee", "committee"},
		{"-regulator", "regulator"},
		{"government", "government"},
	} {
		if strings.Contains(slug, item.token) {
			return item.kind
		}
	}
	return ""
}

func inferMarketProfile(fragment *horizonmodels.HorizonFragment) (string, string) {
	if fragment == nil {
		return "", ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if strings.HasPrefix(cp, "/market/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 4 {
			return strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])
		}
	}
	if len(fragment.MarketSegments) > 0 {
		if seg, ok := taxonomy.NormalizeSegmentPath(fragment.MarketSegments[0]); ok {
			parts := strings.Split(strings.Trim(seg, "/"), "/")
			if len(parts) >= 4 && parts[0] == "segment" {
				return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
			}
		}
	}
	text := fragmentText(fragment)
	for _, item := range []struct {
		token    string
		domain   string
		vertical string
	}{
		{"semiconductor", "technology", "semiconductors"},
		{"chip", "technology", "semiconductors"},
		{"battery", "industrial", "ev-batteries"},
		{"automotive", "industrial", "automotive"},
		{"vehicle", "industrial", "automotive"},
		{"steel", "materials", "steel"},
		{"aluminum", "materials", "aluminum"},
		{"oil", "energy", "oil-and-gas"},
		{"gas", "energy", "oil-and-gas"},
		{"lng", "energy", "lng"},
		{"shipping", "industrial", "shipping"},
		{"port", "industrial", "port-services"},
		{"logistics", "industrial", "logistics"},
		{"pharma", "healthcare", "pharmaceuticals"},
		{"hospital", "healthcare", "health-services"},
		{"telecom", "telecom", "telecommunications"},
		{"cloud", "technology", "cloud"},
	} {
		if strings.Contains(text, item.token) {
			return item.domain, item.vertical
		}
	}
	return "", ""
}

func inferIndustryPaths(fragment *horizonmodels.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	text := fragmentText(fragment)
	for _, item := range []struct {
		token string
		path  string
	}{
		{"semiconductor", "/industry/technology/electronics/semiconductors/semiconductor-manufacturing"},
		{"chip", "/industry/technology/electronics/semiconductors/semiconductor-manufacturing"},
		{"battery", "/industry/industrial/manufacturing/electrical-equipment/ev-batteries"},
		{"automotive", "/industry/industrial/manufacturing/automotive/automotive-manufacturing"},
		{"vehicle", "/industry/industrial/manufacturing/automotive/automotive-manufacturing"},
		{"shipping", "/industry/industrial/transportation/maritime/shipping-services"},
		{"port", "/industry/industrial/transportation/logistics/port-operations"},
		{"logistics", "/industry/industrial/transportation/logistics/logistics-services"},
		{"oil", "/industry/energy/hydrocarbons/oil-and-gas/upstream"},
		{"gas", "/industry/energy/hydrocarbons/oil-and-gas/upstream"},
		{"lng", "/industry/energy/hydrocarbons/lng/lng-infrastructure"},
		{"steel", "/industry/materials/metals/steel/steel-production"},
		{"pharma", "/industry/healthcare/pharmaceuticals/pharmaceuticals/pharmaceutical-manufacturing"},
	} {
		if strings.Contains(text, item.token) {
			return []string{item.path}
		}
	}
	if strings.TrimSpace(fragment.MarketDomain) != "" || strings.TrimSpace(fragment.MarketVertical) != "" {
		switch strings.TrimSpace(fragment.MarketDomain) {
		case "energy":
			return []string{"/industry/energy/hydrocarbons/oil-and-gas"}
		case "industrial":
			if strings.Contains(strings.TrimSpace(fragment.MarketVertical), "shipping") {
				return []string{"/industry/industrial/transportation/maritime/shipping-services"}
			}
			if strings.Contains(strings.TrimSpace(fragment.MarketVertical), "port") {
				return []string{"/industry/industrial/transportation/logistics/port-operations"}
			}
			return []string{"/industry/industrial/manufacturing"}
		case "technology":
			return []string{"/industry/technology"}
		case "materials":
			return []string{"/industry/materials"}
		case "healthcare":
			return []string{"/industry/healthcare"}
		case "telecom":
			return []string{"/industry/telecom"}
		}
	}
	return nil
}

func shouldTagChokepoint(fragment *horizonmodels.HorizonFragment) bool {
	feature := strings.ToLower(strings.TrimSpace(fragment.GeoFeatureType))
	switch feature {
	case "strait", "canal", "pass", "pipeline-corridor":
		return true
	}
	text := fragmentText(fragment)
	for _, token := range []string{"strait", "canal", "hormuz", "mandeb", "suez", "malacca", "bosporus"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func inferEdgeFacet(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) string {
	if edge == nil {
		return ""
	}
	text := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(string(edge.Type)),
		strings.TrimSpace(sourcePath),
		strings.TrimSpace(targetPath),
	}, " "))
	if len(normalizeTransportVia(edge.Qualifiers)) > 0 || strings.Contains(text, "port") || strings.Contains(text, "shipping") || strings.Contains(text, "strait") || strings.Contains(text, "canal") {
		return string(horizonmodels.FacetLogistics)
	}
	switch edge.Type {
	case horizonmodels.DependencyTypeSuppliesTo, horizonmodels.DependencyTypeProduces, horizonmodels.DependencyTypeExtractsFrom:
		return string(horizonmodels.FacetSupply)
	case horizonmodels.DependencyTypeDependsOn:
		if strings.Contains(text, "regulator") || strings.Contains(text, "/gov/") {
			return string(horizonmodels.FacetRegulatory)
		}
		if strings.Contains(text, "grid") || strings.Contains(text, "pipeline") || strings.Contains(text, "terminal") || strings.Contains(text, "power") {
			return string(horizonmodels.FacetInfrastructure)
		}
		return string(horizonmodels.FacetSupply)
	case horizonmodels.DependencyTypeParticipatesIn, horizonmodels.DependencyTypeHasMarketSegment, horizonmodels.DependencyTypeHasSubmarket:
		return string(horizonmodels.FacetDistribution)
	}
	return ""
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
	}
	for _, v := range fragment.Labels {
		parts = append(parts, strings.ToLower(strings.TrimSpace(v)))
	}
	for _, tag := range fragment.Tags {
		parts = append(parts, strings.ToLower(strings.TrimSpace(tag)))
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

func pathSlug(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
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
	raw, found := q[horizonmodels.QualifierTransportVia]
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
	if existing, ok := q[horizonmodels.QualifierTransportVia]; ok && sameTransportVia(existing, normalized) {
		return normalized, false
	}
	q[horizonmodels.QualifierTransportVia] = normalized
	return normalized, true
}

func sameTransportVia(existing interface{}, want []string) bool {
	got := normalizeTransportVia(map[string]interface{}{horizonmodels.QualifierTransportVia: existing})
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
	for _, token := range []string{"strait", "canal", "hormuz", "mandeb", "suez", "malacca", "port", "terminal", "pipeline"} {
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
	return horizonmodels.MetricValue{
		Value:      v,
		Unit:       &unit,
		Confidence: conf,
		ObservedAt: now,
		Provenance: horizonmodels.Provenance{Method: "best_effort_edge_sync"},
	}
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
	facet := edge.GetFacet()
	if facet == "" {
		return addEdgeEnrichmentHint(edge, "Classify DEPENDS_ON facet (logistics/infrastructure/supply/payment/distribution/regulatory/...) to improve transmission reasoning.")
	}
	needsTransmissionQualifiers := facet == horizonmodels.FacetLogistics || facet == horizonmodels.FacetInfrastructure || facet == horizonmodels.FacetSupply || facet == horizonmodels.FacetDistribution
	if !needsTransmissionQualifiers {
		return false
	}
	changed := false
	if !hasAnyNumericQualifier(edge, horizonmodels.QualifierThroughputShare, horizonmodels.QualifierCapacityConstraint, "substitutability") {
		changed = addEdgeEnrichmentHint(edge, "Add at least one transmission qualifier: throughput_share (0..1), capacity_constraint (0..1), or substitutability (0..1).") || changed
	}
	if facet == horizonmodels.FacetLogistics && !hasAnyNumericQualifier(edge, horizonmodels.QualifierReroutePenalty, horizonmodels.QualifierRerouteLatencyDeltaHrs, horizonmodels.QualifierRerouteCostDeltaPct) {
		changed = addEdgeEnrichmentHint(edge, "For logistics dependencies, add reroute_penalty (0..1) and reroute latency/cost deltas (hours/percent).") || changed
	}
	return changed
}

func applyTransmissionContractMetadata(edge *horizonmodels.DependencyEdge) bool {
	if edge == nil || edge.Type != horizonmodels.DependencyTypeDependsOn {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	facet := edge.GetFacet()
	needsContract := edge.IsTransmissionSensitive() || facet == ""
	if !needsContract {
		return false
	}
	hasBase := hasAnyNumericQualifier(edge, horizonmodels.QualifierThroughputShare, horizonmodels.QualifierCapacityConstraint, "substitutability")
	hasReroute := hasAnyNumericQualifier(edge, horizonmodels.QualifierReroutePenalty, horizonmodels.QualifierRerouteLatencyDeltaHrs, horizonmodels.QualifierRerouteCostDeltaPct)
	hasAny := hasBase || hasReroute
	status := horizonmodels.TransmissionContractStatusComplete
	missing := make([]string, 0, 3)
	if facet == "" {
		missing = appendMissingTransmissionField(missing, "facet")
	}
	if !hasBase {
		missing = appendMissingTransmissionField(missing, "throughput_share|capacity_constraint|substitutability")
	}
	if facet == horizonmodels.FacetLogistics && !hasReroute {
		missing = appendMissingTransmissionField(missing, "reroute_penalty|reroute_latency_delta_hours|reroute_cost_delta_pct")
	}
	if len(missing) > 0 {
		if hasAny {
			status = horizonmodels.TransmissionContractStatusPartial
		} else {
			status = horizonmodels.TransmissionContractStatusMissing
		}
	}
	mode := horizonmodels.TransmissionContractModeInferred
	if edge.HasAnyTransmissionQualifier() {
		mode = horizonmodels.TransmissionContractModeExplicit
	}
	changed := setQualifier(edge.Qualifiers, horizonmodels.QualifierTransmissionContractVersion, horizonmodels.TransmissionContractVersionV1)
	changed = setQualifier(edge.Qualifiers, horizonmodels.QualifierTransmissionContractStatus, status) || changed
	changed = setQualifier(edge.Qualifiers, horizonmodels.QualifierTransmissionContractMode, mode) || changed
	if len(missing) > 0 {
		changed = setQualifier(edge.Qualifiers, horizonmodels.QualifierTransmissionContractMissing, missing) || changed
	} else if _, ok := edge.Qualifiers[horizonmodels.QualifierTransmissionContractMissing]; ok {
		delete(edge.Qualifiers, horizonmodels.QualifierTransmissionContractMissing)
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
	edge.Metrics[key] = horizonmodels.MetricValue{
		Value:      v,
		Unit:       unit,
		Confidence: conf,
		ObservedAt: now,
		Provenance: horizonmodels.Provenance{Method: "best_effort_transmission_sync"},
	}
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
