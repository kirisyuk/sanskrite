package pathbinding

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	mapactivity "github.com/igorkirisyuk/sanskrite/internal/horizon/mapactivity"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type SegmentInput struct {
	SupplierRef       string
	CustomerRef       string
	ProductRef        string
	TransportViaRefs  []string
	SegmentConfidence float64
}

type SegmentBinding struct {
	Mode              string
	GeoRefs           []string
	FlowRefs          []string
	AlternateFlowRefs []string
	ZoneRefs          []string
	AffectedZoneRefs  []string
	SpatialSemantics  []string
	BindingConfidence float64
}

type Engine struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	catalog       *mapactivity.Catalog

	mu                  sync.RWMutex
	fragmentByPathCache map[string]*horizonmodels.HorizonFragment
	fragmentsByPathCache map[string][]*horizonmodels.HorizonFragment
	zoneRefsByPathCache map[string][]string
	lookupRefsByPath    map[string][]string
}

func NewEngine(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *Engine {
	return &Engine{
		fragmentsRepo:        fragmentsRepo,
		edgesRepo:            edgesRepo,
		catalog:              mapactivity.LoadCatalog(),
		fragmentByPathCache:  make(map[string]*horizonmodels.HorizonFragment),
		fragmentsByPathCache: make(map[string][]*horizonmodels.HorizonFragment),
		zoneRefsByPathCache:  make(map[string][]string),
		lookupRefsByPath:     make(map[string][]string),
	}
}

func (e *Engine) BindSegment(ctx context.Context, in SegmentInput) SegmentBinding {
	binding := SegmentBinding{}
	binding.Mode = inferMode(in)
	binding.GeoRefs = uniqStrings(collectGeoRefs(in.SupplierRef, in.CustomerRef, in.TransportViaRefs))
	binding.FlowRefs, binding.AlternateFlowRefs = e.matchFlows(in, binding.Mode, binding.GeoRefs)
	binding.ZoneRefs = e.matchZones(in, binding.GeoRefs)
	binding.AffectedZoneRefs = e.lookupAffectedZones(ctx, in.SupplierRef, in.CustomerRef, in.ProductRef, in.TransportViaRefs)
	binding.SpatialSemantics = inferSpatialSemantics(in, binding)
	binding.BindingConfidence = inferBindingConfidence(in, binding)
	return binding
}

func inferMode(in SegmentInput) string {
	text := strings.ToLower(strings.Join(append([]string{in.SupplierRef, in.CustomerRef, in.ProductRef}, in.TransportViaRefs...), " "))
	modes := make([]string, 0, 4)
	add := func(mode string) {
		for _, existing := range modes {
			if existing == mode {
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
	if matchAny(text, "rail", "railway", "yard", "intermodal-rail", "station") {
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
		if len(in.TransportViaRefs) > 1 {
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

func (e *Engine) matchFlows(in SegmentInput, mode string, geoRefs []string) ([]string, []string) {
	if e == nil || e.catalog == nil {
		return nil, nil
	}
	text := bindingMatchText(in)
	countries := geoISOSet(geoRefs)
	routeEvidence := hasRouteEvidence(in)
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
		if len(in.TransportViaRefs) == 0 && len(countries) < 2 && specificKW < 0.95 {
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
			matches = append(matches, scored{id: flow.ID, score: score})
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
	primary := make([]string, 0, 2)
	alt := make([]string, 0, 1)
	firstScore := matches[0].score
	primary = append(primary, matches[0].id)
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

func (e *Engine) matchZones(in SegmentInput, geoRefs []string) []string {
	if e == nil || e.catalog == nil {
		return nil
	}
	text := bindingMatchText(in)
	countries := geoISOSet(geoRefs)
	zoneHints := inferZoneTypes(in)
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
			matches = append(matches, scored{id: zone.ID, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].id < matches[j].id
		}
		return matches[i].score > matches[j].score
	})
	out := make([]string, 0, 2)
	for i, m := range matches {
		if i >= 2 {
			break
		}
		out = append(out, m.id)
	}
	return out
}

func affectedZoneLookupRefs(supplierRef, customerRef, productRef string, viaRefs []string) []string {
	refs := make([]string, 0, 3+len(viaRefs))
	for _, ref := range append([]string{supplierRef, customerRef, productRef}, viaRefs...) {
		refs = append(refs, expandAffectedZoneLookupRef(ref)...)
	}
	return uniqStrings(refs)
}

func expandAffectedZoneLookupRef(ref string) []string {
	ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
	if ref == "" {
		return nil
	}
	out := []string{ref}
	if country := affectedZoneCountryOverlayRef(ref); country != "" {
		out = append(out, country)
	}
	if parent := affectedZoneLocationCountryParent(ref); parent != "" {
		out = append(out, parent)
	}
	out = append(out, affectedZoneStrategicAliasRefs(ref)...)
	return uniqStrings(out)
}

func affectedZoneCountryOverlayRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	if parts[0] == "loc" {
		if len(parts) >= 2 && len(parts[1]) == 2 && !isPlaceholderCountryCode(parts[1]) {
			return "/loc/" + strings.ToLower(strings.TrimSpace(parts[1]))
		}
		return ""
	}
	if len(parts) < 3 || len(parts[1]) != 2 || isPlaceholderCountryCode(parts[1]) {
		return ""
	}
	switch parts[0] {
	case "fac", "org", "gov", "deposit":
		return "/loc/" + strings.ToLower(strings.TrimSpace(parts[1]))
	default:
		return ""
	}
}

func affectedZoneLocationCountryParent(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) < 2 || len(parts[1]) != 2 || isPlaceholderCountryCode(parts[1]) {
		return ""
	}
	return "/loc/" + strings.ToLower(strings.TrimSpace(parts[1]))
}

func affectedZoneStrategicAliasRefs(ref string) []string {
	ref = normalizeAffectedZoneLookupRef(ref)
	if ref == "" {
		return nil
	}
	out := make([]string, 0, 4)
	add := func(paths ...string) {
		for _, path := range paths {
			path = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(path))
			if path != "" {
				out = append(out, path)
			}
		}
	}
	switch {
	case strings.Contains(ref, "bab_el_mandeb"):
		add("/loc/int/bab-el-mandeb", "/loc/ye/bab-el-mandeb")
	case strings.Contains(ref, "strait_of_hormuz") || strings.Contains(ref, "hormuz"):
		add("/loc/int/strait-of-hormuz", "/loc/ir/hormuz", "/loc/ir/hormozgan/strait-of-hormuz")
	case strings.Contains(ref, "red_sea"):
		add("/loc/int/red-sea")
	case strings.Contains(ref, "suez_canal") || strings.Contains(ref, "/loc/eg/suez") || strings.Contains(ref, "/fac/eg/suez"):
		add("/loc/int/suez-canal", "/loc/eg/suez")
	case strings.Contains(ref, "strait_of_malacca") || strings.Contains(ref, "malacca_strait"):
		add("/loc/int/strait-of-malacca", "/loc/my/malacca")
	case strings.Contains(ref, "panama_canal"):
		add("/loc/int/panama-canal", "/loc/pa/panama")
	}
	return uniqStrings(out)
}

func normalizeAffectedZoneLookupRef(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	ref = strings.ReplaceAll(ref, " ", "_")
	ref = strings.ReplaceAll(ref, "-", "_")
	for strings.Contains(ref, "//") {
		ref = strings.ReplaceAll(ref, "//", "/")
	}
	return strings.TrimSuffix(ref, "/")
}

func isPlaceholderCountryCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "", "na", "xx", "zz":
		return true
	default:
		return false
	}
}

func (e *Engine) lookupAffectedZones(ctx context.Context, supplierRef, customerRef, productRef string, viaRefs []string) []string {
	out := make([]string, 0, 8)
	for _, ref := range affectedZoneLookupRefs(supplierRef, customerRef, productRef, viaRefs) {
		if ref == "" {
			continue
		}
		for _, lookupRef := range e.lookupRefsForPath(ctx, ref) {
			out = append(out, e.zoneRefsForPath(ctx, lookupRef)...)
		}
	}
	return uniqStrings(out)
}

func (e *Engine) lookupRefsForPath(ctx context.Context, canonicalPath string) []string {
	canonicalPath = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(canonicalPath))
	if canonicalPath == "" {
		return nil
	}
	if e == nil {
		return []string{canonicalPath}
	}
	e.mu.RLock()
	if cached, ok := e.lookupRefsByPath[canonicalPath]; ok {
		e.mu.RUnlock()
		return append([]string(nil), cached...)
	}
	e.mu.RUnlock()

	refs := expandAffectedZoneLookupRef(canonicalPath)
	if e.fragmentsRepo != nil && e.edgesRepo != nil {
		refs = append(refs, e.relatedActorLookupRefs(ctx, canonicalPath)...)
	}
	refs = uniqStrings(refs)

	e.mu.Lock()
	e.lookupRefsByPath[canonicalPath] = append([]string(nil), refs...)
	e.mu.Unlock()
	return refs
}

func (e *Engine) relatedActorLookupRefs(ctx context.Context, canonicalPath string) []string {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil
	}
	frag := e.fragmentByPath(ctx, canonicalPath)
	if frag == nil || strings.TrimSpace(frag.ID) == "" {
		return nil
	}
	isOrg := strings.HasPrefix(canonicalPath, "/org/") || strings.HasPrefix(canonicalPath, "/gov/")
	isAsset := strings.HasPrefix(canonicalPath, "/fac/") || strings.HasPrefix(canonicalPath, "/deposit/")
	if !isOrg && !isAsset {
		return nil
	}

	relatedIDs := make(map[string]struct{}, 8)
	for _, edgeType := range []DependencyType{
		DependencyTypeOwns,
		DependencyTypeControls,
		DependencyTypeOperatesIn,
	} {
		et := edgeType
		if isOrg {
			edges, err := e.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), &et, 64)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge == nil {
					continue
				}
				if targetID := strings.TrimSpace(edge.TargetFragmentID); targetID != "" {
					relatedIDs[targetID] = struct{}{}
				}
			}
			continue
		}
		edges, err := e.edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), &et, 64)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil {
				continue
			}
			if sourceID := strings.TrimSpace(edge.SourceFragmentID); sourceID != "" {
				relatedIDs[sourceID] = struct{}{}
			}
		}
	}
	if len(relatedIDs) == 0 {
		return nil
	}

	out := make([]string, 0, len(relatedIDs)*2)
	for id := range relatedIDs {
		rel, err := e.fragmentsRepo.Get(ctx, id)
		if err != nil || rel == nil {
			continue
		}
		out = append(out, expandAffectedZoneLookupRef(rel.CanonicalPath)...)
	}
	return uniqStrings(out)
}

func (e *Engine) zoneRefsForPath(ctx context.Context, canonicalPath string) []string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" || e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil
	}
	e.mu.RLock()
	if cached, ok := e.zoneRefsByPathCache[canonicalPath]; ok {
		e.mu.RUnlock()
		return append([]string(nil), cached...)
	}
	e.mu.RUnlock()
	frags := e.fragmentsByPath(ctx, canonicalPath)
	if len(frags) == 0 {
		return nil
	}
	typ := DependencyTypeAffects
	refs := make([]string, 0, 8)
	for _, frag := range frags {
		if frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		edges, err := e.edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), &typ, 64)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.SourceFragmentID) == "" {
				continue
			}
			sourceFrag, err := e.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID))
			if err != nil || sourceFrag == nil {
				continue
			}
			if sourceFrag.IsAffectedZoneGeo() || strings.HasPrefix(strings.TrimSpace(sourceFrag.CanonicalPath), "/zone/") {
				refs = append(refs, strings.TrimSpace(sourceFrag.CanonicalPath))
			}
		}
	}
	refs = uniqStrings(refs)
	e.mu.Lock()
	e.zoneRefsByPathCache[canonicalPath] = append([]string(nil), refs...)
	e.mu.Unlock()
	return refs
}

func (e *Engine) fragmentByPath(ctx context.Context, canonicalPath string) *horizonmodels.HorizonFragment {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" || e == nil || e.fragmentsRepo == nil {
		return nil
	}
	e.mu.RLock()
	if frag, ok := e.fragmentByPathCache[canonicalPath]; ok {
		e.mu.RUnlock()
		return frag
	}
	e.mu.RUnlock()
	frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil || frag == nil {
		return nil
	}
	e.mu.Lock()
	e.fragmentByPathCache[canonicalPath] = frag
	e.mu.Unlock()
	return frag
}

func (e *Engine) fragmentsByPath(ctx context.Context, canonicalPath string) []*horizonmodels.HorizonFragment {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" || e == nil || e.fragmentsRepo == nil {
		return nil
	}
	e.mu.RLock()
	if cached, ok := e.fragmentsByPathCache[canonicalPath]; ok {
		e.mu.RUnlock()
		return append([]*horizonmodels.HorizonFragment(nil), cached...)
	}
	e.mu.RUnlock()

	frags, err := e.fragmentsRepo.ListByCanonicalPath(ctx, canonicalPath)
	if err != nil || len(frags) == 0 {
		if frag := e.fragmentByPath(ctx, canonicalPath); frag != nil {
			frags = []*horizonmodels.HorizonFragment{frag}
		}
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(frags))
	for _, frag := range frags {
		if frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		out = append(out, frag)
	}
	e.mu.Lock()
	e.fragmentsByPathCache[canonicalPath] = append([]*horizonmodels.HorizonFragment(nil), out...)
	e.mu.Unlock()
	return append([]*horizonmodels.HorizonFragment(nil), out...)
}

func inferZoneTypes(in SegmentInput) map[string]struct{} {
	text := bindingText(in)
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

func inferSpatialSemantics(in SegmentInput, binding SegmentBinding) []string {
	out := make([]string, 0, 8)
	if len(in.TransportViaRefs) > 0 {
		out = append(out, "route_qualified")
	}
	if len(binding.FlowRefs) > 0 {
		out = append(out, "flow_bound")
	}
	if len(binding.ZoneRefs) > 0 {
		out = append(out, "zone_bound")
	}
	if len(binding.AffectedZoneRefs) > 0 {
		out = append(out, "affected_zone_pressure")
	}
	if len(binding.GeoRefs) > 1 {
		out = append(out, "cross_border")
	}
	if len(binding.AlternateFlowRefs) > 0 {
		out = append(out, "alternate_route")
	}
	text := bindingMatchText(in)
	if matchAny(text, "strait", "canal", "hormuz", "suez", "bab-el-mandeb", "malacca") {
		out = append(out, "chokepoint_exposed")
	}
	return uniqStrings(out)
}

func inferBindingConfidence(in SegmentInput, binding SegmentBinding) float64 {
	score := 0.2
	if len(in.TransportViaRefs) > 0 {
		score += 0.18
	}
	if binding.Mode != "" {
		score += 0.12
	}
	if len(binding.GeoRefs) > 0 {
		score += 0.14
	}
	if len(binding.FlowRefs) > 0 {
		score += 0.16
	}
	if len(binding.ZoneRefs) > 0 {
		score += 0.10
	}
	if len(binding.AffectedZoneRefs) > 0 {
		score += 0.15
	}
	if len(binding.AlternateFlowRefs) > 0 {
		score += 0.05
	}
	if in.SegmentConfidence > 0 {
		score = (score * 0.7) + (clamp01(in.SegmentConfidence) * 0.3)
	}
	return clamp01(score)
}

func bindingText(in SegmentInput) string {
	parts := []string{in.SupplierRef, in.CustomerRef, in.ProductRef}
	parts = append(parts, in.TransportViaRefs...)
	return strings.ToLower(strings.Join(parts, " "))
}

func bindingMatchText(in SegmentInput) string {
	parts := []string{
		refBindingTokens(in.SupplierRef),
		refBindingTokens(in.CustomerRef),
		refBindingTokens(in.ProductRef),
	}
	for _, ref := range in.TransportViaRefs {
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

func hasRouteEvidence(in SegmentInput) bool {
	if len(in.TransportViaRefs) > 0 {
		return true
	}
	text := bindingText(in)
	return matchAny(text, "port", "harbor", "seaport", "terminal", "canal", "strait", "shipping", "vessel", "pipeline", "rail", "railway", "airport", "air-base", "truck", "road", "highway", "logistics-hub", "warehouse", "landing-station", "subsea-cable")
}

func collectGeoRefs(supplierRef, customerRef string, viaRefs []string) []string {
	refs := []string{supplierRef, customerRef}
	refs = append(refs, viaRefs...)
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
	out := make(map[string]struct{}, len(geoRefs))
	for _, ref := range geoRefs {
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "/loc/") {
			continue
		}
		parts := strings.Split(strings.Trim(ref, "/"), "/")
		if len(parts) >= 2 && len(parts[1]) == 2 {
			out[strings.ToUpper(parts[1])] = struct{}{}
		}
	}
	return out
}

func keysOfSet(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	seen := make(map[string]struct{}, len(left))
	for _, item := range left {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	for _, item := range right {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
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
