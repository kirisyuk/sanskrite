package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	mapactivity "github.com/igorkirisyuk/sanskrite/internal/horizon/mapactivity"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
)

type sample struct {
	Path         string
	Supplier     string
	Product      string
	Customer     string
	Via          []string
	Mode         string
	FlowRefs     []string
	ZoneRefs     []string
	AffectedRefs []string
	Semantics    []string
	Confidence   float64
	StressScore  float64
}

type kv struct {
	Key   string
	Count int
}

type bindEngine struct {
	catalog       *mapactivity.Catalog
	pathToID      map[string]string
	pathToAffects map[string][]string
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	limit := flag.Int("limit", 1200, "max supply links to inspect")
	samples := flag.Int("samples", 8, "sample count per bucket")
	flag.Parse()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	db := fs.Client()

	engine := &bindEngine{catalog: mapactivity.LoadCatalog(), pathToID: map[string]string{}, pathToAffects: map[string][]string{}}
	supplyLinks := make([]*h.HorizonFragment, 0, *limit)
	idToPath := map[string]string{}

	fragIter := db.Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	for {
		doc, err := fragIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var f h.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		cp := strings.TrimSpace(f.CanonicalPath)
		id := strings.TrimSpace(f.ID)
		if cp != "" && id != "" {
			if _, ok := engine.pathToID[cp]; !ok {
				engine.pathToID[cp] = id
			}
			if _, ok := idToPath[id]; !ok {
				idToPath[id] = cp
			}
		}
		if f.Type == h.FragmentTypeSupplyLink && len(supplyLinks) < *limit {
			copyFrag := f
			supplyLinks = append(supplyLinks, &copyFrag)
		}
	}

	edgeIter := db.Collection("horizon_dependency_edges").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Where("type", "==", string(h.DependencyTypeAffects)).Documents(ctx)
	for {
		doc, err := edgeIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var e h.DependencyEdge
		if err := doc.DataTo(&e); err != nil {
			continue
		}
		sourcePath := strings.TrimSpace(idToPath[strings.TrimSpace(e.SourceFragmentID)])
		targetPath := strings.TrimSpace(idToPath[strings.TrimSpace(e.TargetFragmentID)])
		if sourcePath == "" || targetPath == "" || !strings.HasPrefix(sourcePath, "/zone/") {
			continue
		}
		engine.pathToAffects[targetPath] = appendUnique(engine.pathToAffects[targetPath], sourcePath)
	}

	total := len(supplyLinks)
	modeCounts := map[string]int{}
	semCounts := map[string]int{}
	flowCoverage := 0
	zoneCoverage := 0
	affectedCoverage := 0
	viaCoverage := 0
	viaFlowCoverage := 0
	viaZoneCoverage := 0
	bindingStrong := 0
	stressStrong := 0
	totalFlowRefs := 0
	totalZoneRefs := 0
	totalStress := 0.0
	totalRecovery := 0.0
	flowRefCounts := map[string]int{}
	zoneRefCounts := map[string]int{}
	missingAll := make([]sample, 0, *samples)
	missingFlow := make([]sample, 0, *samples)
	rich := make([]sample, 0, *samples)
	stressed := make([]sample, 0, *samples)
	byCountryPair := map[string]int{}
	stressEval := pathstress.NewEvaluator()

	for _, link := range supplyLinks {
		if link == nil {
			continue
		}
		supplier := firstNonEmpty(deref(link.SupplierFacilityRef), deref(link.SupplierRef))
		customer := firstNonEmpty(deref(link.CustomerFacilityRef), deref(link.CustomerRef))
		product := deref(link.ProductRef)
		binding := engine.bind(supplier, customer, product, link.TransportViaRefs, metricValue(link.State, "confidence"))
		stress := stressEval.EvaluateLeg(h.SupplyChainPathLeg{
			SupplierRef:       supplier,
			CustomerRef:       customer,
			ProductRef:        product,
			TransportViaRefs:  append([]string(nil), link.TransportViaRefs...),
			Mode:              binding.Mode,
			GeoRefs:           append([]string(nil), binding.GeoRefs...),
			FlowRefs:          append([]string(nil), binding.FlowRefs...),
			AlternateFlowRefs: append([]string(nil), binding.AlternateFlowRefs...),
			ZoneRefs:          append([]string(nil), binding.ZoneRefs...),
			AffectedZoneRefs:  append([]string(nil), binding.AffectedZoneRefs...),
			SpatialSemantics:  append([]string(nil), binding.SpatialSemantics...),
			BindingConfidence: binding.BindingConfidence,
			SegmentConfidence: metricValue(link.State, "confidence"),
		})
		inc(modeCounts, binding.Mode)
		for _, sem := range binding.SpatialSemantics {
			inc(semCounts, sem)
		}
		if len(link.TransportViaRefs) > 0 {
			viaCoverage++
		}
		if len(binding.FlowRefs) > 0 {
			flowCoverage++
			totalFlowRefs += len(binding.FlowRefs)
			for _, ref := range binding.FlowRefs {
				inc(flowRefCounts, ref)
			}
		}
		if len(binding.ZoneRefs) > 0 {
			zoneCoverage++
			totalZoneRefs += len(binding.ZoneRefs)
			for _, ref := range binding.ZoneRefs {
				inc(zoneRefCounts, ref)
			}
		}
		if len(binding.AffectedZoneRefs) > 0 {
			affectedCoverage++
		}
		if binding.BindingConfidence >= 0.75 {
			bindingStrong++
		}
		if stress != nil {
			totalStress += stress.StressScore
			totalRecovery += stress.RecoveryLatencyHrs
			if stress.StressScore >= 0.60 {
				stressStrong++
			}
		}
		if len(link.TransportViaRefs) > 0 && len(binding.FlowRefs) > 0 {
			viaFlowCoverage++
		}
		if len(link.TransportViaRefs) > 0 && len(binding.ZoneRefs) > 0 {
			viaZoneCoverage++
		}
		inc(byCountryPair, geoPair(supplier, customer))
		s := sample{Path: link.CanonicalPath, Supplier: supplier, Product: product, Customer: customer, Via: append([]string(nil), link.TransportViaRefs...), Mode: binding.Mode, FlowRefs: binding.FlowRefs, ZoneRefs: binding.ZoneRefs, AffectedRefs: binding.AffectedZoneRefs, Semantics: binding.SpatialSemantics, Confidence: binding.BindingConfidence}
		if stress != nil {
			s.StressScore = stress.StressScore
		}
		if len(binding.FlowRefs) == 0 && len(binding.ZoneRefs) == 0 && len(binding.AffectedZoneRefs) == 0 && len(missingAll) < *samples {
			missingAll = append(missingAll, s)
		}
		if len(binding.FlowRefs) == 0 && len(binding.ZoneRefs) > 0 && len(missingFlow) < *samples {
			missingFlow = append(missingFlow, s)
		}
		if len(binding.FlowRefs) > 0 && len(binding.ZoneRefs) > 0 && len(rich) < *samples {
			rich = append(rich, s)
		}
		if stress != nil && stress.StressScore >= 0.60 && len(stressed) < *samples {
			stressed = append(stressed, s)
		}
	}

	fmt.Printf("tenant=%s project=%s supply_links=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), total)
	fmt.Printf("transport_via= %d (%.1f%%)\n", viaCoverage, pct(viaCoverage, total))
	fmt.Printf("flow_bound=    %d (%.1f%%)\n", flowCoverage, pct(flowCoverage, total))
	fmt.Printf("zone_bound=    %d (%.1f%%)\n", zoneCoverage, pct(zoneCoverage, total))
	fmt.Printf("affected_bound=%d (%.1f%%)\n", affectedCoverage, pct(affectedCoverage, total))
	fmt.Printf("binding_strong=%d (%.1f%%)\n", bindingStrong, pct(bindingStrong, total))
	fmt.Printf("stress_strong=%d (%.1f%%)\n", stressStrong, pct(stressStrong, total))
	fmt.Printf("via_flow_bound=%d (%.1f%% of via)\n", viaFlowCoverage, pct(viaFlowCoverage, viaCoverage))
	fmt.Printf("via_zone_bound=%d (%.1f%% of via)\n", viaZoneCoverage, pct(viaZoneCoverage, viaCoverage))
	fmt.Printf("avg_flow_refs= %.2f\n", avg(totalFlowRefs, flowCoverage))
	fmt.Printf("avg_zone_refs= %.2f\n", avg(totalZoneRefs, zoneCoverage))
	fmt.Printf("avg_stress= %.2f\n", avgFloat(totalStress, total))
	fmt.Printf("avg_recovery_hours= %.1f\n\n", avgFloat(totalRecovery, total))
	printTop("modes", modeCounts, 12)
	printTop("semantics", semCounts, 16)
	printTop("country_pairs", byCountryPair, 16)
	printTop("top_flows", flowRefCounts, 16)
	printTop("top_zones", zoneRefCounts, 16)
	printSamples("rich_samples", rich)
	printSamples("stressed_samples", stressed)
	printSamples("missing_all_samples", missingAll)
	printSamples("missing_flow_samples", missingFlow)
}

type binding struct {
	Mode              string
	GeoRefs           []string
	FlowRefs          []string
	AlternateFlowRefs []string
	ZoneRefs          []string
	AffectedZoneRefs  []string
	SpatialSemantics  []string
	BindingConfidence float64
}

func (e *bindEngine) bind(supplier, customer, product string, via []string, segmentConfidence float64) binding {
	out := binding{}
	out.Mode = inferMode(supplier, customer, product, via)
	out.GeoRefs = uniqStrings(collectGeoRefs(supplier, customer, via))
	out.FlowRefs, out.AlternateFlowRefs = e.matchFlows(supplier, customer, product, via, out.Mode, out.GeoRefs)
	out.ZoneRefs = e.matchZones(supplier, customer, product, via, out.GeoRefs)
	out.AffectedZoneRefs = e.lookupAffectedZones(supplier, customer, via)
	out.SpatialSemantics = inferSpatialSemantics(via, out)
	out.BindingConfidence = inferBindingConfidence(via, segmentConfidence, out)
	return out
}

func inferMode(supplier, customer, product string, via []string) string {
	text := strings.ToLower(strings.Join(append([]string{supplier, customer, product}, via...), " "))
	modes := make([]string, 0, 4)
	add := func(mode string) {
		for _, ex := range modes {
			if ex == mode {
				return
			}
		}
		modes = append(modes, mode)
	}
	if matchAny(text, "pipeline", "lng-terminal", "gas-terminal", "compressor") {
		if matchAny(text, "hydrogen", "h2") {
			add("pipeline_h2")
		} else if matchAny(text, "gas", "lng", "natural-gas") {
			add("pipeline_gas")
		} else {
			add("pipeline_oil")
		}
	}
	if matchAny(text, "airport", "air-base", "cargo-air", "airlift") {
		add("air")
	}
	if matchAny(text, "rail", "railway", "yard", "station") {
		add("rail")
	}
	if matchAny(text, "road", "truck", "highway", "distribution-center", "warehouse", "logistics-hub") {
		add("road")
	}
	if matchAny(text, "port", "harbor", "seaport", "terminal", "canal", "strait", "shipping", "vessel", "sea", "maritime") {
		add("sea")
	}
	if matchAny(text, "cable", "landing-station", "internet-exchange") {
		add("internet_cable")
	}
	if len(modes) == 0 {
		if len(via) > 1 {
			return "intermodal"
		}
		if matchAny(text, "gas", "lng", "crude", "oil") {
			return "sea"
		}
		return "intermodal"
	}
	if len(modes) > 1 {
		return "intermodal"
	}
	return modes[0]
}

func (e *bindEngine) matchFlows(supplier, customer, product string, via []string, mode string, geoRefs []string) ([]string, []string) {
	text := bindingMatchText(supplier, customer, product, via)
	countries := geoISOSet(geoRefs)
	routeEvidence := hasRouteEvidence(supplier, customer, product, via)
	crossBorder := len(countries) > 1
	type scored struct {
		id    string
		score float64
	}
	matches := make([]scored, 0, 8)
	for _, flow := range e.catalog.Flows {
		score := 0.0
		exactMode := strings.TrimSpace(flow.FlowMode) != "" && strings.TrimSpace(flow.FlowMode) == strings.TrimSpace(mode)
		if exactMode {
			score += 0.55
		} else if strings.TrimSpace(mode) == "intermodal" && strings.TrimSpace(flow.FlowMode) == "intermodal" {
			score += 0.30
		} else if strings.TrimSpace(mode) != "" && strings.TrimSpace(flow.FlowMode) != "" {
			continue
		}
		countryMatched := len(countries) > 0 && intersectsFold(keysOfSet(countries), flow.CountryHints)
		if countryMatched {
			score += 0.35
		}
		kw := keywordMatchScore(text, flow.Keywords)
		specificKW := specificKeywordMatchScore(text, flow.Keywords)
		if kw > 0 {
			score += minFloat(kw*0.18, 0.45)
		}
		if specificKW > 0 {
			score += minFloat(specificKW*0.32, 0.75)
		}
		if routeEvidence {
			score += 0.12
		}
		if crossBorder && strings.TrimSpace(flow.FlowMode) == "intermodal" {
			score += 0.10
		}
		if !routeEvidence && !countryMatched && kw < 1.35 {
			continue
		}
		if len(via) == 0 && len(countries) < 2 && specificKW < 0.95 {
			continue
		}
		if !countryMatched && specificKW < 0.95 {
			continue
		}
		if strings.TrimSpace(flow.FlowMode) == "intermodal" && !crossBorder && kw < 1.60 {
			continue
		}
		if exactMode && !countryMatched && specificKW < 0.95 && !routeEvidence {
			continue
		}
		if score >= 1.05 {
			matches = append(matches, scored{flow.ID, score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].id < matches[j].id
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) == 0 {
		return nil, nil
	}
	primary, alt := []string{matches[0].id}, []string{}
	firstScore := matches[0].score
	for _, m := range matches[1:] {
		if len(primary) < 2 && m.score >= firstScore-0.10 && m.score >= 1.18 {
			primary = append(primary, m.id)
			continue
		}
		if routeEvidence && len(alt) < 1 && m.score >= firstScore-0.06 && m.score >= 1.28 {
			alt = append(alt, m.id)
		}
	}
	return primary, alt
}

func (e *bindEngine) matchZones(supplier, customer, product string, via []string, geoRefs []string) []string {
	text := bindingMatchText(supplier, customer, product, via)
	countries := geoISOSet(geoRefs)
	zoneHints := inferZoneTypes(text)
	type scored struct {
		id    string
		score float64
	}
	matches := make([]scored, 0, 8)
	for _, zone := range e.catalog.Zones {
		score := 0.0
		countryMatched := false
		if _, ok := countries[strings.ToUpper(strings.TrimSpace(zone.CountryISO2))]; ok {
			score += 0.75
			countryMatched = true
		}
		if _, ok := zoneHints[strings.TrimSpace(zone.ZoneType)]; ok {
			score += 0.55
		}
		kw := keywordMatchScore(text, zone.Keywords)
		if kw > 0 {
			score += minFloat(kw*0.22, 0.75)
		}
		if !countryMatched && kw < 1.40 {
			continue
		}
		if len(zoneHints) == 0 && kw < 1.15 {
			continue
		}
		if score >= 1.20 {
			matches = append(matches, scored{zone.ID, score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].id < matches[j].id
		}
		return matches[i].score > matches[j].score
	})
	out := []string{}
	for i, m := range matches {
		if i >= 2 {
			break
		}
		out = append(out, m.id)
	}
	return out
}

func (e *bindEngine) lookupAffectedZones(supplier, customer string, via []string) []string {
	refs := uniqStrings(append([]string{supplier, customer}, via...))
	out := []string{}
	for _, ref := range refs {
		out = append(out, e.pathToAffects[strings.TrimSpace(ref)]...)
	}
	return uniqStrings(out)
}

func inferZoneTypes(text string) map[string]struct{} {
	out := map[string]struct{}{}
	if matchAny(text, "port", "harbor", "terminal", "canal", "strait", "shipping", "warehouse") {
		out["logistics_chokepoint"] = struct{}{}
	}
	if matchAny(text, "lng", "gas", "crude", "oil", "refinery", "petrochemical", "energy") {
		out["energy_hub"] = struct{}{}
	}
	if matchAny(text, "plant", "factory", "fab", "industrial", "assembly", "manufacturing") {
		out["industrial_cluster"] = struct{}{}
	}
	if matchAny(text, "free-zone", "free zone", "sez", "special-economic-zone") {
		out["special_economic_zone"] = struct{}{}
	}
	if matchAny(text, "mine", "field", "deposit", "extraction", "well") {
		out["resource_extraction"] = struct{}{}
	}
	if matchAny(text, "data-center", "landing-station", "subsea-cable", "internet-exchange", "cable") {
		out["data_infrastructure"] = struct{}{}
	}
	if matchAny(text, "air-base", "naval-base", "munitions", "arsenal", "defense") {
		out["military_industrial"] = struct{}{}
	}
	return out
}

func inferSpatialSemantics(via []string, b binding) []string {
	out := []string{}
	if len(via) > 0 {
		out = append(out, "route_qualified")
	}
	if len(b.FlowRefs) > 0 {
		out = append(out, "flow_bound")
	}
	if len(b.ZoneRefs) > 0 {
		out = append(out, "zone_bound")
	}
	if len(b.AffectedZoneRefs) > 0 {
		out = append(out, "affected_zone_pressure")
	}
	if len(b.GeoRefs) > 1 {
		out = append(out, "cross_border")
	}
	if len(b.AlternateFlowRefs) > 0 {
		out = append(out, "alternate_route")
	}
	for _, ref := range via {
		if matchAny(ref, "strait", "canal", "hormuz", "suez", "bab-el-mandeb", "malacca") {
			out = append(out, "chokepoint_exposed")
			break
		}
	}
	return uniqStrings(out)
}

func bindingMatchText(supplier, customer, product string, via []string) string {
	parts := []string{
		refBindingTokens(supplier),
		refBindingTokens(customer),
		refBindingTokens(product),
	}
	for _, ref := range via {
		parts = append(parts, refBindingTokens(ref))
	}
	return strings.Join(parts, " ")
}

func refBindingTokens(ref string) string {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if ref == "" {
		return ""
	}
	parts := strings.FieldsFunc(ref, func(r rune) bool {
		switch r {
		case '/', '-', '_', ':', '.', ',':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || bindingStopToken(part) {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

func bindingStopToken(token string) bool {
	switch token {
	case "fac", "org", "gov", "loc", "product", "market", "community", "deposit", "track", "zone", "raw", "processed", "distribution", "midstream", "downstream", "upstream", "services", "global", "na", "other", "misc", "system", "group", "holding", "holdings", "corp", "corporation", "company", "co", "ltd", "llc", "plc", "ag", "sa", "nv", "se", "spa":
		return true
	default:
		return false
	}
}

func hasRouteEvidence(supplier, customer, product string, via []string) bool {
	if len(via) > 0 {
		return true
	}
	text := strings.ToLower(strings.Join(append([]string{supplier, customer, product}, via...), " "))
	return matchAny(text, "port", "harbor", "seaport", "terminal", "canal", "strait", "shipping", "vessel", "pipeline", "rail", "railway", "airport", "air-base", "truck", "road", "highway", "logistics-hub", "warehouse", "landing-station", "subsea-cable")
}

func inferBindingConfidence(via []string, segmentConfidence float64, b binding) float64 {
	score := 0.2
	if len(via) > 0 {
		score += 0.18
	}
	if b.Mode != "" {
		score += 0.12
	}
	if len(b.GeoRefs) > 0 {
		score += 0.14
	}
	if len(b.FlowRefs) > 0 {
		score += 0.16
	}
	if len(b.ZoneRefs) > 0 {
		score += 0.10
	}
	if len(b.AffectedZoneRefs) > 0 {
		score += 0.15
	}
	if len(b.AlternateFlowRefs) > 0 {
		score += 0.05
	}
	if segmentConfidence > 0 {
		score = (score * 0.7) + (clamp01(segmentConfidence) * 0.3)
	}
	return clamp01(score)
}

func collectGeoRefs(supplier, customer string, via []string) []string {
	refs := append([]string{supplier, customer}, via...)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if geo := canonicalGeoRef(ref); geo != "" {
			out = append(out, geo)
		}
	}
	return out
}
func canonicalGeoRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "/loc/") {
		parts := strings.Split(strings.Trim(ref, "/"), "/")
		if len(parts) >= 2 && len(parts[1]) == 2 {
			return "/loc/" + strings.ToLower(parts[1])
		}
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	switch parts[0] {
	case "fac", "org", "gov", "deposit", "market", "community":
		if len(parts[1]) == 2 && !isPlaceholderGeoCode(parts[1]) {
			return "/loc/" + strings.ToLower(parts[1])
		}
	}
	return ""
}
func isPlaceholderGeoCode(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	return code == "na" || code == "xx" || code == "zz"
}
func geoISOSet(geoRefs []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, ref := range geoRefs {
		parts := strings.Split(strings.Trim(strings.TrimSpace(ref), "/"), "/")
		if len(parts) >= 2 && len(parts[1]) == 2 {
			out[strings.ToUpper(parts[1])] = struct{}{}
		}
	}
	return out
}
func keysOfSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func keywordMatchScore(text string, keywords []string) float64 {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || len(keywords) == 0 {
		return 0
	}
	score := 0.0
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if len(kw) < 3 {
			continue
		}
		if strings.Contains(text, kw) {
			if strings.Contains(kw, " ") {
				score += 1.0
			} else {
				score += 0.45
			}
		}
	}
	if score > 2.5 {
		return 2.5
	}
	return score
}
func specificKeywordMatchScore(text string, keywords []string) float64 {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || len(keywords) == 0 {
		return 0
	}
	score := 0.0
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if len(kw) < 4 || genericBindingKeyword(kw) {
			continue
		}
		if strings.Contains(text, kw) {
			if strings.Contains(kw, " ") {
				score += 1.0
			} else {
				score += 0.55
			}
		}
	}
	if score > 2.0 {
		return 2.0
	}
	return score
}
func genericBindingKeyword(token string) bool {
	switch token {
	case "sea", "road", "rail", "air", "port", "trade", "energy", "freight", "cargo", "container", "shipping", "intermodal", "corridor", "lane", "pipeline", "gas", "oil", "power", "hub", "route", "logistics", "maritime":
		return true
	default:
		return false
	}
}
func intersectsFold(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := map[string]struct{}{}
	for _, item := range left {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item != "" {
			seen[item] = struct{}{}
		}
	}
	for _, item := range right {
		item = strings.ToUpper(strings.TrimSpace(item))
		if _, ok := seen[item]; ok {
			return true
		}
	}
	return false
}
func appendUnique(items []string, extra string) []string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return items
	}
	for _, item := range items {
		if strings.TrimSpace(item) == extra {
			return items
		}
	}
	return append(items, extra)
}
func metricValue(state map[string]h.MetricValue, key string) float64 {
	if state == nil {
		return 0
	}
	if v, ok := state[key]; ok {
		return v.Value
	}
	return 0
}
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
func firstNonEmpty(items ...string) string {
	for _, i := range items {
		i = strings.TrimSpace(i)
		if i != "" {
			return i
		}
	}
	return ""
}
func canonicalGeo(ref string) string {
	ref = strings.TrimSpace(ref)
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) >= 2 {
		switch parts[0] {
		case "fac", "org", "gov", "deposit", "market", "community", "loc":
			if len(parts[1]) == 2 {
				return strings.ToUpper(parts[1])
			}
		}
	}
	return "na"
}
func geoPair(a, b string) string { return canonicalGeo(a) + "->" + canonicalGeo(b) }
func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * 100 / float64(total)
}
func avg(total, count int) float64 {
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}
func avgFloat(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
func inc(m map[string]int, k string) {
	k = strings.TrimSpace(k)
	if k == "" {
		k = "(empty)"
	}
	m[k]++
}
func printTop(label string, m map[string]int, n int) {
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Key < items[j].Key
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > n {
		items = items[:n]
	}
	fmt.Println(label + ":")
	for _, item := range items {
		fmt.Printf("  %s: %d\n", item.Key, item.Count)
	}
	fmt.Println()
}
func printSamples(label string, samples []sample) {
	fmt.Println(label + ":")
	for _, s := range samples {
		fmt.Printf("  - %s\n", s.Path)
		fmt.Printf("    mode=%s confidence=%.2f\n", s.Mode, s.Confidence)
		if s.StressScore > 0 {
			fmt.Printf("    stress=%.2f\n", s.StressScore)
		}
		fmt.Printf("    supplier=%s\n", s.Supplier)
		fmt.Printf("    customer=%s\n", s.Customer)
		fmt.Printf("    via=%v\n", s.Via)
		fmt.Printf("    flows=%v zones=%v affected=%v semantics=%v\n", s.FlowRefs, s.ZoneRefs, s.AffectedRefs, s.Semantics)
	}
	fmt.Println()
}
func uniqStrings(in []string) []string {
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
func matchAny(text string, terms ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
