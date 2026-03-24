package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type recognitionAxisCandidate struct {
	Axis    string
	Score   float64
	Reasons []string
}

func BuildInputRecognition(req *horizonmodels.SimulationRunRequest, lens *horizonmodels.Lens, refs horizonmodels.SimulationBaselineRefs) map[string]any {
	if req == nil {
		return nil
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = strings.TrimSpace(req.Mode)
	}
	corpus := recognitionCorpus(req, lens, refs)
	axisCandidates := recognizeAxes(req, lens, corpus)
	objects := recognizeObjects(req, lens, refs, corpus)
	volume := recognizeVolume(lens, refs, objects)
	out := map[string]any{
		"query":            question,
		"axisCandidates":   axisCandidates,
		"objectCandidates": objects,
		"volumeProfile":    volume,
		"primaryAxis":      "",
		"summary":          "",
	}
	if len(axisCandidates) > 0 {
		out["primaryAxis"] = stringValue(axisCandidates[0], "axis")
	}
	out["summary"] = recognitionSummary(out)
	return out
}

func recognitionCorpus(req *horizonmodels.SimulationRunRequest, lens *horizonmodels.Lens, refs horizonmodels.SimulationBaselineRefs) string {
	parts := []string{strings.TrimSpace(req.Question), strings.TrimSpace(req.Mode)}
	if req.Signal != nil {
		parts = append(parts, req.Signal.Text, req.Signal.Type, req.Signal.Axis, req.Signal.LocationPath)
		parts = append(parts, req.Signal.ActorPaths...)
		parts = append(parts, req.Signal.SeedPaths...)
	}
	if lens != nil {
		parts = append(parts, lens.Name, lens.OriginalQuery)
		parts = append(parts, lens.Scope...)
		parts = append(parts, lens.DomainModes...)
		for _, dq := range lens.DomainQueries {
			parts = append(parts, dq.Title, dq.Description, dq.ContractType, dq.CanonicalPath)
		}
		parts = append(parts, viewConfigPaths(lens.ViewConfig)...)
	}
	parts = append(parts, refs.ScopePaths...)
	parts = append(parts, refs.FragmentPaths...)
	return strings.ToLower(strings.Join(compactStringsPreserveOrder(parts), " | "))
}

func recognizeAxes(req *horizonmodels.SimulationRunRequest, lens *horizonmodels.Lens, corpus string) []map[string]any {
	score := map[string]float64{}
	reasons := map[string][]string{}
	add := func(axis string, delta float64, reason string) {
		if axis == "" || delta <= 0 {
			return
		}
		score[axis] += delta
		if reason != "" {
			reasons[axis] = append(reasons[axis], reason)
		}
	}
	if req.Signal != nil {
		add(strings.TrimSpace(req.Signal.Axis), 1.4, "explicit signal axis")
		add(axisFromShockType(req.Signal.Type), 1.0, "signal type mapping")
	}
	if lens != nil {
		for _, mode := range lens.DomainModes {
			switch strings.TrimSpace(mode) {
			case "supply_chain":
				add(horizonmodels.AxisPhysicalFlowSupply, 0.7, "domain mode supply_chain")
				add(horizonmodels.AxisLogisticsCapacity, 0.45, "domain mode supply_chain")
			case "market":
				add(horizonmodels.AxisPhysicalFlowDemand, 0.45, "domain mode market")
				add(horizonmodels.AxisFinancialSettlement, 0.35, "domain mode market")
			case "governance":
				add(horizonmodels.AxisLegalCompliance, 0.55, "domain mode governance")
			}
		}
		for _, dq := range lens.DomainQueries {
			switch strings.TrimSpace(strings.ToLower(dq.ContractType)) {
			case "supply-chain":
				add(horizonmodels.AxisPhysicalFlowSupply, 0.8, "domain query supply-chain")
				add(horizonmodels.AxisLogisticsCapacity, 0.5, "domain query supply-chain")
			case "competitive-landscape", "sector-overview":
				add(horizonmodels.AxisPhysicalFlowDemand, 0.4, "domain query market/sector")
				add(horizonmodels.AxisInfoUncertainty, 0.2, "domain query market/sector")
			}
		}
		for _, hint := range structuralAxisHints(lens) {
			add(hint.Axis, hint.Weight, hint.Reason)
		}
	}
	for _, rule := range []struct {
		axis   string
		weight float64
		tokens []string
		reason string
	}{
		{horizonmodels.AxisLegalCompliance, 0.9, []string{"sanction", "compliance", "license", "export control", "regulatory"}, "legal/compliance language"},
		{horizonmodels.AxisFinancialSettlement, 0.85, []string{"payment", "settlement", "bank", "insurance", "finance", "cost", "pricing", "margin"}, "financial language"},
		{horizonmodels.AxisPhysicalFlowSupply, 0.9, []string{"supply chain", "producer", "manufacturing", "delivered", "upstream", "downstream", "facility"}, "supply language"},
		{horizonmodels.AxisPhysicalFlowDemand, 0.65, []string{"market", "consumer", "retail", "demand", "sales"}, "demand language"},
		{horizonmodels.AxisLogisticsCapacity, 0.8, []string{"shipping", "route", "port", "terminal", "carrier", "logistics"}, "logistics language"},
		{horizonmodels.AxisInfoReputation, 0.55, []string{"reputation", "confidence", "narrative", "public", "market interpretation"}, "reputation language"},
		{horizonmodels.AxisInfoUncertainty, 0.5, []string{"uncertainty", "volatility", "ambiguity", "exception"}, "uncertainty language"},
	} {
		for _, token := range rule.tokens {
			if strings.Contains(corpus, token) {
				add(rule.axis, rule.weight, rule.reason)
			}
		}
	}
	candidates := make([]recognitionAxisCandidate, 0, len(score))
	for axis, value := range score {
		candidates = append(candidates, recognitionAxisCandidate{
			Axis:    axis,
			Score:   round3(value),
			Reasons: compactStringsPreserveOrder(reasons[axis]),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Axis < candidates[j].Axis
		}
		return candidates[i].Score > candidates[j].Score
	})
	out := make([]map[string]any, 0, min(len(candidates), 5))
	for _, item := range candidates[:min(len(candidates), 5)] {
		out = append(out, map[string]any{
			"axis":    item.Axis,
			"score":   item.Score,
			"reasons": item.Reasons,
		})
	}
	return out
}

type axisHint struct {
	Axis   string
	Weight float64
	Reason string
}

func structuralAxisHints(lens *horizonmodels.Lens) []axisHint {
	if lens == nil {
		return nil
	}
	paths := compactStringsPreserveOrder(append(append([]string{}, lens.Scope...), viewConfigPaths(lens.ViewConfig)...))
	if len(paths) == 0 {
		return nil
	}
	kindCounts := map[string]int{}
	for _, path := range paths {
		kindCounts[objectKindFromPath(path)]++
	}
	joined := strings.ToLower(strings.Join(paths, " | "))
	hints := []axisHint{}
	add := func(axis string, weight float64, reason string) {
		if axis == "" || weight <= 0 {
			return
		}
		hints = append(hints, axisHint{Axis: axis, Weight: weight, Reason: reason})
	}

	if kindCounts["product"] >= 1 && (kindCounts["org"] >= 1 || kindCounts["fac"] >= 1 || kindCounts["domain"] >= 1) {
		add(horizonmodels.AxisPhysicalFlowSupply, 0.7, "scope product-flow structure")
	}
	if kindCounts["product"] >= 2 && kindCounts["org"] == 0 && kindCounts["fac"] == 0 && kindCounts["domain"] == 0 && kindCounts["market"] == 0 {
		add(horizonmodels.AxisPhysicalFlowSupply, 0.55, "product-only transformation/exposure scope")
	}
	if kindCounts["fac"] >= 1 && (strings.Contains(joined, "/port/") || strings.Contains(joined, "port-") || strings.Contains(joined, "terminal") || strings.Contains(joined, "logistics")) {
		add(horizonmodels.AxisLogisticsCapacity, 0.75, "scope logistics facilities")
	}
	if kindCounts["fac"] >= 2 {
		add(horizonmodels.AxisLogisticsCapacity, 0.35, "multi-facility scope")
	}
	if kindCounts["market"] >= 1 {
		add(horizonmodels.AxisPhysicalFlowDemand, 0.35, "market node in scope")
	}
	if kindCounts["market"] >= 1 && kindCounts["product"] >= 1 {
		add(horizonmodels.AxisFinancialSettlement, 0.25, "market plus product exposure")
	}
	if strings.Contains(joined, "raw-material") || strings.Contains(joined, "mineral") || strings.Contains(joined, "mining") || strings.Contains(joined, "lng") || strings.Contains(joined, "semiconductor") || strings.Contains(joined, "automotive") || strings.Contains(joined, "retail-grocery") || strings.Contains(joined, "space") {
		add(horizonmodels.AxisPhysicalFlowSupply, 0.45, "sector path structure")
	}
	if strings.Contains(joined, "route") || strings.Contains(joined, "carrier") || strings.Contains(joined, "shipment") || strings.Contains(joined, "terminal") {
		add(horizonmodels.AxisLogisticsCapacity, 0.45, "routing path structure")
	}
	if kindCounts["gov"] >= 1 {
		add(horizonmodels.AxisLegalCompliance, 0.3, "government node in scope")
	}
	return hints
}

func recognizeObjects(req *horizonmodels.SimulationRunRequest, lens *horizonmodels.Lens, refs horizonmodels.SimulationBaselineRefs, corpus string) []map[string]any {
	candidates := map[string]map[string]any{}
	add := func(path, source string, score float64) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/") {
			return
		}
		entry := candidates[path]
		if entry == nil {
			entry = map[string]any{
				"path":    path,
				"kind":    objectKindFromPath(path),
				"score":   0.0,
				"sources": []string{},
			}
			candidates[path] = entry
		}
		entry["score"] = round3(floatValue(entry, "score", 0) + score)
		entry["sources"] = compactStringsPreserveOrder(append(stringSliceAny(entry["sources"]), source))
	}
	if req.Signal != nil {
		for _, path := range req.Signal.ActorPaths {
			add(path, "signal.actor", 1.0)
		}
		for _, path := range req.Signal.SeedPaths {
			add(path, "signal.seed", 0.9)
		}
		add(req.Signal.LocationPath, "signal.location", 1.0)
	}
	for _, path := range refs.ScopePaths {
		boost := 0.25
		if objectMentionedInCorpus(path, corpus) {
			boost = 0.6
		}
		add(path, "baseline.scope", boost)
	}
	for _, path := range refs.FragmentPaths {
		boost := 0.18
		if objectMentionedInCorpus(path, corpus) {
			boost = 0.45
		}
		add(path, "baseline.fragment", boost)
	}
	if lens != nil {
		for _, path := range lens.Scope {
			boost := 0.35
			if objectMentionedInCorpus(path, corpus) {
				boost = 0.75
			}
			add(path, "lens.scope", boost)
		}
		for _, path := range viewConfigPaths(lens.ViewConfig) {
			add(path, "lens.viewConfig", 0.42)
		}
		for _, dq := range lens.DomainQueries {
			add(dq.CanonicalPath, "lens.domainQuery", 0.7)
		}
		for key, ref := range lens.ScopeArtifacts {
			_ = key
			add(ref.AnchorSupplier, "lens.artifact", 0.64)
			add(ref.AnchorCustomer, "lens.artifact", 0.64)
			add(ref.ProductAxis, "lens.artifact", 0.58)
			for _, path := range ref.NodeRefs {
				add(path, "lens.artifact", 0.48)
			}
		}
	}
	type item struct {
		path   string
		object map[string]any
	}
	items := make([]item, 0, len(candidates))
	for path, object := range candidates {
		items = append(items, item{path: path, object: object})
	}
	sort.Slice(items, func(i, j int) bool {
		left := floatValue(items[i].object, "score", 0)
		right := floatValue(items[j].object, "score", 0)
		if left == right {
			return items[i].path < items[j].path
		}
		return left > right
	})
	out := make([]map[string]any, 0, min(len(items), 12))
	for _, item := range items[:min(len(items), 12)] {
		out = append(out, item.object)
	}
	return out
}

func recognizeVolume(lens *horizonmodels.Lens, refs horizonmodels.SimulationBaselineRefs, objects []map[string]any) map[string]any {
	kindCounts := map[string]int{}
	for _, object := range objects {
		kindCounts[stringValue(object, "kind")]++
	}
	geoSet := map[string]struct{}{}
	for _, path := range append(append([]string{}, refs.ScopePaths...), refs.FragmentPaths...) {
		if geo := geoFromPath(path); geo != "" {
			geoSet[geo] = struct{}{}
		}
	}
	artifactCount := len(refs.ArtifactKeys)
	if lens != nil && len(lens.ScopeArtifacts) > artifactCount {
		artifactCount = len(lens.ScopeArtifacts)
	}
	queryText := ""
	domainQueryCount := 0
	if lens != nil {
		queryText = strings.ToLower(strings.TrimSpace(lens.OriginalQuery + " " + lens.Name))
		domainQueryCount = len(lens.DomainQueries)
	}
	kindBreadth := 0
	for _, count := range kindCounts {
		if count > 0 {
			kindBreadth++
		}
	}
	chainBreadth := kindCounts["org"] + kindCounts["fac"] + kindCounts["product"] + kindCounts["gov"]
	profile := "single_chain"
	switch {
	case len(objects) <= 3 && kindCounts["domain"] == 0 && kindCounts["market"] == 0:
		profile = "focused_object"
	case len(geoSet) >= 4 && (artifactCount >= 2 || chainBreadth >= 5 || kindCounts["market"] >= 1):
		profile = "cross_border_network"
	case looksSectorNetwork(queryText) || domainQueryCount >= 2:
		profile = "sector_network"
	case kindCounts["domain"] >= 2 || kindCounts["market"] >= 2:
		profile = "sector_network"
	case kindCounts["domain"] >= 1 && kindCounts["market"] >= 1 && (chainBreadth >= 6 || kindBreadth >= 5):
		profile = "sector_network"
	case kindCounts["domain"] == 0 && kindCounts["market"] == 0 && chainBreadth <= 2:
		profile = "focused_object"
	}
	return map[string]any{
		"profile":           profile,
		"geographyBreadth":  len(geoSet),
		"artifactBreadth":   artifactCount,
		"objectKindCounts":  intMapAny(kindCounts),
		"scopePathCount":    len(refs.ScopePaths),
		"fragmentPathCount": len(refs.FragmentPaths),
	}
}

func looksSectorNetwork(query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return false
	}
	for _, token := range []string{
		"whole industry",
		"industry supply chain",
		"sector",
		"ecosystem",
		"europe wide",
		"europe-wide",
		"brics",
		"gcc",
		"including producers",
		"value chain",
		"detailed",
		"map the supply chain",
	} {
		if strings.Contains(query, token) {
			return true
		}
	}
	return false
}

func recognitionSummary(recognition map[string]any) string {
	axis := stringValue(recognition, "primaryAxis")
	objects := mapSlice(recognition["objectCandidates"])
	volume := mapValue(recognition["volumeProfile"])
	if axis == "" {
		axis = "unresolved"
	}
	topObjects := make([]string, 0, min(len(objects), 3))
	for _, object := range objects[:min(len(objects), 3)] {
		topObjects = append(topObjects, stringValue(object, "path"))
	}
	return fmt.Sprintf("Primary axis: %s. Main objects: %s. Scope profile: %s.",
		axis,
		strings.Join(topObjects, ", "),
		stringValue(volume, "profile"),
	)
}

func objectMentionedInCorpus(path, corpus string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" {
		return false
	}
	parts := strings.Split(path, "/")
	tail := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.TrimSpace(parts[i]) != "" {
			tail = strings.ReplaceAll(parts[i], "-", " ")
			break
		}
	}
	return tail != "" && strings.Contains(corpus, tail)
}

func objectKindFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "org"
	case strings.HasPrefix(path, "/fac/"):
		return "fac"
	case strings.HasPrefix(path, "/market/"):
		return "market"
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/loc/"):
		return "geo"
	case strings.HasPrefix(path, "/domain/"):
		return "domain"
	case strings.HasPrefix(path, "/artifact/"):
		return "artifact"
	case strings.HasPrefix(path, "/gov/"):
		return "gov"
	default:
		return "node"
	}
}

func geoFromPath(path string) string {
	parts := strings.Split(strings.TrimSpace(path), "/")
	for i, part := range parts {
		if part == "loc" && i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
		if part == "geo" && i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func viewConfigPaths(viewConfig map[string]interface{}) []string {
	if len(viewConfig) == 0 {
		return nil
	}
	var out []string
	for _, key := range []string{"deferredScopePaths", "focusNodePaths", "highlightPaths", "spherePaths", "filterPaths"} {
		out = append(out, stringSliceFromAny(viewConfig[key])...)
	}
	if directive := mapValue(viewConfig["viewDirective"]); len(directive) > 0 {
		out = append(out, stringSliceFromAny(directive["focusNodePaths"])...)
		out = append(out, stringSliceFromAny(directive["highlightPaths"])...)
	}
	return compactStringsPreserveOrder(out)
}

func mapSlice(raw any) []map[string]any {
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	generic, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(generic))
	for _, item := range generic {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}

func stringValue(input map[string]any, key string) string {
	if len(input) == 0 {
		return ""
	}
	if value, ok := input[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func floatValue(input map[string]any, key string, fallback float64) float64 {
	if len(input) == 0 {
		return fallback
	}
	switch value := input[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}

func intMapAny(values map[string]int) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func stringSliceAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactStringsPreserveOrder(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return compactStringsPreserveOrder(out)
	default:
		return nil
	}
}

func round3(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}
