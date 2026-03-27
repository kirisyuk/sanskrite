package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type scopeDerivedShoulder struct {
	AgentClass     string
	DisplayName    string
	CountryCode    string
	CountryName    string
	OriginRefs     []string
	AllowedActions []string
	Severity       float64
	PathCount      int
	MaxStress      float64
}

type scopeDerivedProduct struct {
	AgentClass     string
	DisplayName    string
	ProductPath    string
	ProductName    string
	OriginRefs     []string
	AllowedActions []string
	Severity       float64
	PathCount      int
	MaxStress      float64
}

func scopeDerivedPopulationExtras(run *horizonmodels.SimulationRun) []map[string]any {
	out := make([]map[string]any, 0, 12)
	for _, shoulder := range scopeDerivedShoulders(run) {
		severity := clamp(0.35+shoulder.Severity*0.25, 0.35, 0.92)
		out = append(out, map[string]any{
			"agentClass":       shoulder.AgentClass,
			"displayName":      shoulder.DisplayName,
			"originKind":       "graph_scope_derivation",
			"originRefs":       compactStrings(shoulder.OriginRefs),
			"generatedBy":      "sim.scope.population.derived",
			"justification":    "Graph-derived regional shoulder from simulation scope",
			"populationWeight": round3(1.02 + shoulder.Severity*0.08),
			"traits":           map[string]any{"attention": round3(0.68 + shoulder.Severity*0.16), "riskBias": round3(0.56 + shoulder.Severity*0.14)},
			"beliefState":      map[string]any{"trust": round3(0.52 - shoulder.Severity*0.08), "stress": severity},
			"resourceState":    map[string]any{"budget": 0.56},
			"allowedActions":   shoulder.AllowedActions,
			"messageStyle":     "regional-shoulder",
			"policyHints": map[string]any{
				"role":        "regional-shoulder",
				"countryCode": shoulder.CountryCode,
				"countryName": shoulder.CountryName,
				"riskScore":   round3(shoulder.Severity),
				"pathCount":   shoulder.PathCount,
				"maxStress":   round3(shoulder.MaxStress),
			},
		})
	}
	for _, product := range scopeDerivedProducts(run) {
		severity := clamp(0.38+product.Severity*0.28, 0.38, 0.94)
		out = append(out, map[string]any{
			"agentClass":       product.AgentClass,
			"displayName":      product.DisplayName,
			"originKind":       "graph_scope_derivation",
			"originRefs":       compactStrings(product.OriginRefs),
			"generatedBy":      "sim.scope.population.derived",
			"justification":    "Graph-derived product risk desk from simulation scope",
			"populationWeight": round3(1.03 + product.Severity*0.09),
			"traits":           map[string]any{"attention": round3(0.7 + product.Severity*0.18), "riskBias": round3(0.58 + product.Severity*0.16)},
			"beliefState":      map[string]any{"trust": round3(0.5 - product.Severity*0.1), "stress": severity},
			"resourceState":    map[string]any{"budget": 0.52},
			"allowedActions":   product.AllowedActions,
			"messageStyle":     "product-risk",
			"policyHints": map[string]any{
				"role":        "product-risk",
				"productPath": product.ProductPath,
				"productName": product.ProductName,
				"riskScore":   round3(product.Severity),
				"pathCount":   product.PathCount,
				"maxStress":   round3(product.MaxStress),
			},
		})
	}
	return out
}

func scopeDerivedMonitoringOverlay(run *horizonmodels.SimulationRun) []map[string]any {
	out := make([]map[string]any, 0, 12)
	for _, shoulder := range scopeDerivedShoulders(run) {
		why := fmt.Sprintf("%s appears in the graph as an active external shoulder feeding the current chain.", shoulder.CountryName)
		if shoulder.Severity > 0 {
			why = fmt.Sprintf("%s shows elevated path pressure in materialized paths and should be monitored as a live external shoulder.", shoulder.CountryName)
		}
		out = append(out, map[string]any{
			"id":          "shoulder_load_" + shoulder.CountryCode,
			"title":       shoulder.CountryName + " shoulder load",
			"cadence":     "weekly",
			"kind":        "regional_shoulder_kpi",
			"disciplines": []string{"supply_operations", "trade_compliance"},
			"functions":   []string{"logistics", "procurement", "operations"},
			"why":         why,
		})
	}
	for _, product := range scopeDerivedProducts(run) {
		why := fmt.Sprintf("%s is present in active scope paths and should be monitored as a specific stress-bearing product family.", product.ProductName)
		if product.Severity > 0 {
			why = fmt.Sprintf("%s already carries elevated path stress in the materialized chain and should be treated as a first-class product risk.", product.ProductName)
		}
		out = append(out, map[string]any{
			"id":          "product_availability_" + scopeSafeSlug(scopeLastPathSegment(product.ProductPath)),
			"title":       titleWords(product.ProductName) + " availability",
			"cadence":     "weekly",
			"kind":        "product_kpi",
			"disciplines": []string{"product_program", "supply_operations"},
			"functions":   []string{"product", "operations", "procurement"},
			"why":         why,
		})
	}
	return out
}

func scopeDerivedShoulders(run *horizonmodels.SimulationRun) []scopeDerivedShoulder {
	if !scopeSupportsDerivedGraph(run) {
		return nil
	}
	primaryGeo := recognitionPrimaryGeo(run)
	previewSignals := scopeDerivedShoulderSignals(run)
	allowScopePathFallback := len(previewSignals) == 0
	type bucket struct {
		code       string
		facilities []string
		score      float64
		pathCount  int
		maxStress  float64
	}
	buckets := map[string]*bucket{}
	for code, signal := range previewSignals {
		entry := &bucket{
			code:       code,
			facilities: append([]string(nil), signal.Refs...),
			score:      signal.Score,
			pathCount:  signal.PathCount,
			maxStress:  signal.MaxStress,
		}
		buckets[code] = entry
	}
	for _, path := range scopePaths(run) {
		if !strings.HasPrefix(path, "/fac/") {
			continue
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 3 {
			continue
		}
		code := strings.ToLower(strings.TrimSpace(parts[1]))
		if code == "" || code == primaryGeo {
			continue
		}
		entry := buckets[code]
		if entry == nil {
			if !allowScopePathFallback {
				continue
			}
			entry = &bucket{code: code}
			buckets[code] = entry
		}
		entry.facilities = append(entry.facilities, path)
		entry.score += derivedShoulderScore(path)
		if signal := previewSignals[code]; signal.Score > 0 {
			entry.score += signal.Score * 0.25
			entry.pathCount = max(entry.pathCount, signal.PathCount)
			entry.maxStress = maxFloat(entry.maxStress, signal.MaxStress)
		}
	}
	items := make([]bucket, 0, len(buckets))
	for _, entry := range buckets {
		if len(entry.facilities) == 0 {
			continue
		}
		sort.Strings(entry.facilities)
		items = append(items, *entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].code < items[j].code
		}
		return items[i].score > items[j].score
	})
	out := make([]scopeDerivedShoulder, 0, min(len(items), 4))
	for _, item := range items[:min(len(items), 4)] {
		out = append(out, scopeDerivedShoulder{
			AgentClass:     "regional_supply_shoulder_" + item.code,
			DisplayName:    scopeCountryName(item.code) + " supply shoulder",
			CountryCode:    item.code,
			CountryName:    scopeCountryName(item.code),
			OriginRefs:     item.facilities[:min(len(item.facilities), 3)],
			AllowedActions: []string{string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionIssueStatement), string(horizonmodels.SimulationActionDoNothing)},
			Severity:       round3(item.score),
			PathCount:      item.pathCount,
			MaxStress:      round3(item.maxStress),
		})
	}
	return out
}

func scopeDerivedProducts(run *horizonmodels.SimulationRun) []scopeDerivedProduct {
	if !scopeSupportsDerivedGraph(run) {
		return nil
	}
	previewSignals := scopeDerivedProductSignals(run)
	type item struct {
		path      string
		score     float64
		pathCount int
		maxStress float64
	}
	seen := map[string]struct{}{}
	items := make([]item, 0, 8)
	for path, signal := range previewSignals {
		if !scopeIsOperationalProductPath(path) {
			continue
		}
		seen[path] = struct{}{}
		items = append(items, item{path: path, score: signal.Score, pathCount: signal.PathCount, maxStress: signal.MaxStress})
	}
	for _, path := range scopePaths(run) {
		if !strings.HasPrefix(path, "/product/") {
			continue
		}
		if !scopeIsOperationalProductPath(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		score := derivedProductScore(path)
		if score <= 0 {
			continue
		}
		score += previewSignals[path].Score * 0.25
		items = append(items, item{path: path, score: score, pathCount: previewSignals[path].PathCount, maxStress: previewSignals[path].MaxStress})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].path < items[j].path
		}
		return items[i].score > items[j].score
	})
	out := make([]scopeDerivedProduct, 0, min(len(items), 5))
	for _, item := range items[:min(len(items), 5)] {
		name := strings.ReplaceAll(scopeLastPathSegment(item.path), "-", " ")
		actions := []string{string(horizonmodels.SimulationActionReprioritizeFlow), string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionDoNothing)}
		if strings.Contains(item.path, "/raw/chemical/") || strings.Contains(item.path, "chemical") {
			actions = []string{string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionDoNothing)}
		}
		if strings.Contains(item.path, "telemetry") || strings.Contains(item.path, "electronic") {
			actions = []string{string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionReprioritizeFlow), string(horizonmodels.SimulationActionAdjustGuidance), string(horizonmodels.SimulationActionDoNothing)}
		}
		out = append(out, scopeDerivedProduct{
			AgentClass:     "product_risk_manager_" + scopeSafeSlug(scopeLastPathSegment(item.path)),
			DisplayName:    name + " risk desk",
			ProductPath:    item.path,
			ProductName:    name,
			OriginRefs:     []string{item.path},
			AllowedActions: actions,
			Severity:       round3(item.score),
			PathCount:      item.pathCount,
			MaxStress:      round3(item.maxStress),
		})
	}
	return out
}

type scopeMetricSignal struct {
	Score     float64
	PathCount int
	MaxStress float64
	Refs      []string
}

func scopeDerivedShoulderSignals(run *horizonmodels.SimulationRun) map[string]scopeMetricSignal {
	out := map[string]scopeMetricSignal{}
	for _, raw := range mapSlice(scopeAnySlice(run.BaselinePreview["artifactTopShoulders"])) {
		code := strings.ToLower(strings.TrimSpace(firstString(raw, "code")))
		if code == "" {
			continue
		}
		out[code] = scopeMetricSignal{
			Score:     floatValue(raw, "riskScore", 0),
			PathCount: int(floatValue(raw, "pathCount", 0)),
			MaxStress: floatValue(raw, "maxStress", 0),
			Refs:      stringSliceAny(raw["refs"]),
		}
	}
	return out
}

func scopeDerivedProductSignals(run *horizonmodels.SimulationRun) map[string]scopeMetricSignal {
	out := map[string]scopeMetricSignal{}
	for _, raw := range mapSlice(scopeAnySlice(run.BaselinePreview["artifactTopProducts"])) {
		path := strings.TrimSpace(firstString(raw, "path"))
		if path == "" {
			continue
		}
		out[path] = scopeMetricSignal{
			Score:     floatValue(raw, "riskScore", 0),
			PathCount: int(floatValue(raw, "pathCount", 0)),
			MaxStress: floatValue(raw, "maxStress", 0),
		}
	}
	return out
}

func scopeAnySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func scopeSupportsDerivedGraph(run *horizonmodels.SimulationRun) bool {
	axis := runAxis(run)
	switch axis {
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisFinancialSettlement, horizonmodels.AxisLegalCompliance, horizonmodels.AxisLegalAccess:
		return len(scopePaths(run)) > 0
	default:
		return false
	}
}

func scopePaths(run *horizonmodels.SimulationRun) []string {
	if run == nil {
		return nil
	}
	return compactStrings(append(append([]string{}, run.BaselineRefs.ScopePaths...), run.Request.SeedPaths...))
}

func recognitionPrimaryGeo(run *horizonmodels.SimulationRun) string {
	for _, object := range recognitionObjectCandidates(mapValue(run.Recognition)["objectCandidates"]) {
		path := strings.TrimSpace(firstString(object, "path"))
		if code := scopeCountryCodeFromPath(path); code != "" {
			return code
		}
	}
	return ""
}

func derivedShoulderScore(path string) float64 {
	score := 1.0
	switch {
	case strings.Contains(path, "port"), strings.Contains(path, "terminal"), strings.Contains(path, "logistics"), strings.Contains(path, "supply-base"), strings.Contains(path, "hub"):
		score += 1.3
	case strings.Contains(path, "assembly"), strings.Contains(path, "factory"), strings.Contains(path, "plant"), strings.Contains(path, "manufacturing"), strings.Contains(path, "facility"), strings.Contains(path, "center"):
		score += 0.8
	}
	return score
}

func derivedProductScore(path string) float64 {
	score := 0.0
	switch {
	case strings.Contains(path, "toy"), strings.Contains(path, "action-figure"), strings.Contains(path, "board-game"), strings.Contains(path, "construction-toy"), strings.Contains(path, "doll"):
		score += 1.75
	case strings.Contains(path, "telemetry"), strings.Contains(path, "electronic"):
		score += 1.9
	case strings.Contains(path, "well-completion"):
		score += 1.8
	case strings.Contains(path, "pump"):
		score += 1.7
	case strings.Contains(path, "chemical"):
		score += 1.7
	case strings.Contains(path, "drill-bit"), strings.Contains(path, "drill-bits"):
		score += 1.55
	case strings.Contains(path, "spare-parts"), strings.Contains(path, "equipment"):
		score += 1.45
	}
	switch {
	case strings.Contains(path, "/finished/"):
		score += 0.25
	case strings.Contains(path, "/component/"):
		score += 0.15
	case strings.Contains(path, "/raw/"):
		score += 0.10
	}
	return score
}

func scopeIsOperationalProductPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	switch {
	case path == "":
		return false
	case strings.Contains(path, "transshipment"),
		strings.Contains(path, "intermodal"),
		strings.Contains(path, "logistics"),
		strings.Contains(path, "warehouse"),
		strings.Contains(path, "distribution"),
		strings.Contains(path, "clearance"),
		strings.Contains(path, "forwarding"):
		return false
	default:
		return true
	}
}

func scopeCountryCodeFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "domain":
		for i := 1; i < len(parts)-1; i++ {
			if parts[i] == "geo" && len(strings.TrimSpace(parts[i+1])) == 2 {
				return strings.ToLower(strings.TrimSpace(parts[i+1]))
			}
		}
	}
	return ""
}

func scopeCountryName(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "cn":
		return "China"
	case "kz":
		return "Kazakhstan"
	case "tr":
		return "Turkey"
	case "in":
		return "India"
	case "sg":
		return "Singapore"
	case "ae":
		return "UAE"
	case "sa":
		return "Saudi Arabia"
	case "ru":
		return "Russia"
	case "us":
		return "United States"
	default:
		return strings.ToUpper(strings.TrimSpace(code))
	}
}

func scopeLastPathSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func scopeSafeSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "-")
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}
