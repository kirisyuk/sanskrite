package mapactivity

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	mapActivityFactKind = "map_activity"

	metricScore       = "map_score"
	metricHits1h      = "map_hits_1h"
	metricHits24h     = "map_hits_24h"
	metricTotalMent   = "map_total_mentions"
	metricLastBoost   = "map_last_boost"
	metricLastMatchTS = "map_last_match_unix"

	halfLifeScoreH = 8.0
	halfLife1hH    = 1.0
	halfLife24hH   = 24.0
)

var zoneTypeHeuristicKeywords = map[string][]string{
	"logistics_chokepoint": {
		"port", "harbor", "shipping", "freight", "container", "maritime", "strait", "canal",
		"chokepoint", "blockade", "transshipment", "customs", "supply chain",
	},
	"energy_hub": {
		"oil", "crude", "gas", "lng", "refinery", "petrochemical", "export terminal",
		"bunkering", "opec", "energy terminal", "pipeline", "offshore field",
	},
	"industrial_cluster": {
		"manufacturing", "industrial", "factory", "plant", "assembly", "semiconductor",
		"metals", "steel", "automotive", "shipyard", "supply chain",
	},
	"special_economic_zone": {
		"special economic zone", "free zone", "freeport", "export zone", "industrial park", "sez",
	},
	"data_infrastructure": {
		"subsea cable", "undersea cable", "fiber", "data center", "telecom", "internet outage",
		"network outage", "cyber attack", "backbone",
	},
	"military_industrial": {
		"airbase", "naval base", "munition", "weapons", "defense industry", "drone", "missile",
		"arsenal", "defense production",
	},
	"resource_extraction": {
		"mining", "mine", "ore", "lithium", "copper", "nickel", "drilling", "offshore rig",
		"extraction", "critical minerals",
	},
}

var flowModeHeuristicKeywords = map[string][]string{
	"sea": {
		"shipping", "vessel", "warship", "tanker", "container ship", "strait", "canal", "maritime",
		"navy", "port congestion", "ship traffic",
	},
	"air": {
		"airspace", "aviation", "flight", "airport", "runway", "air corridor",
		"fighter jet", "bomber", "drone", "interceptor",
	},
	"road": {
		"highway", "truck", "trucking", "convoy", "border crossing", "road corridor",
	},
	"rail": {
		"rail", "railway", "freight train", "rail corridor", "rolling stock", "terminal rail",
	},
	"inland_waterway": {
		"river transport", "barge", "inland waterway", "lock", "river port",
	},
	"intermodal": {
		"intermodal", "dry port", "logistics hub", "transshipment", "freight terminal", "multi modal",
	},
	"pipeline_oil": {
		"oil pipeline", "crude pipeline", "pipeline sabotage", "oil flow",
	},
	"pipeline_gas": {
		"gas pipeline", "natural gas", "pipeline flow", "gas transit", "lng supply",
	},
	"pipeline_h2": {
		"hydrogen pipeline", "h2 pipeline", "hydrogen corridor",
	},
	"pipeline_co2": {
		"co2 pipeline", "carbon capture", "ccs corridor",
	},
	"power_hvdc": {
		"hvdc", "power interconnector", "transmission line", "grid link", "blackout",
	},
	"power_hvac": {
		"hvac transmission", "power grid", "substation", "grid stability", "electricity corridor",
	},
	"ferry_roro": {
		"ferry", "ro ro", "ro-ro", "passenger vessel", "vehicle ferry",
	},
	"internet_cable": {
		"subsea cable", "undersea cable", "cable cut", "fiber cut", "internet outage",
		"telecom disruption", "backbone link",
	},
}

var lowSignalNoiseMarkers = []string{
	"submenu",
	"primary menu",
	"about menu",
	"site wide navigation",
	"filter by",
	"book event",
	"podcast network",
	"departments submenu",
	"regions submenu",
	"topics submenu",
}

var highSignalRiskMarkers = []string{
	"war", "strike", "attack", "missile", "drone", "airstrike", "invasion", "troops",
	"sanctions", "embargo", "pipeline", "blackout", "internet outage", "cable cut",
	"port", "shipping", "strait", "canal", "earthquake", "flood", "wildfire", "hurricane",
	"cyber attack", "ransomware", "botnet", "phishing",
}

type eventTopicProfile struct {
	military       bool
	cyber          bool
	shipping       bool
	energy         bool
	infrastructure bool
	disaster       bool
	economic       bool
	protest        bool
	diplomatic     bool
}

type ObservedEvent struct {
	Title         string
	Summary       string
	Severity      string
	CountryCode   string
	FeedType      string
	Category      string
	Level         string
	OccurredAt    time.Time
	SourceEventID string
}

type Engine struct {
	factsRepo *corerepo.OntologyFactsRepository
	catalog   *Catalog
	logger    *log.Logger
}

func NewEngine(factsRepo *corerepo.OntologyFactsRepository, logger *log.Logger) *Engine {
	if factsRepo == nil {
		return nil
	}
	if logger == nil {
		logger = log.New(log.Writer(), "", log.LstdFlags)
	}
	return &Engine{
		factsRepo: factsRepo,
		catalog:   LoadCatalog(),
		logger:    logger,
	}
}

type matchAggregate struct {
	kind          string
	itemID        string
	itemName      string
	countryISO2   string
	countryName   string
	zoneType      string
	flowMode      string
	boost         float64
	hits          float64
	maxConfidence float64
	lastMatchedAt time.Time
	lastTitle     string
}

func (e *Engine) ObserveEvents(ctx context.Context, tenantID, sourceID string, events []ObservedEvent) error {
	if e == nil || e.factsRepo == nil || e.catalog == nil || len(events) == 0 {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	now := time.Now().UTC()
	aggs := make(map[string]*matchAggregate)

	for _, ev := range events {
		matched := e.matchEvent(ev, now)
		for _, m := range matched {
			key := m.kind + ":" + m.itemID
			a := aggs[key]
			if a == nil {
				cp := *m
				a = &cp
				aggs[key] = a
			}
			a.boost += m.boost
			a.hits += m.hits
			if m.maxConfidence > a.maxConfidence {
				a.maxConfidence = m.maxConfidence
			}
			if m.lastMatchedAt.After(a.lastMatchedAt) {
				a.lastMatchedAt = m.lastMatchedAt
				a.lastTitle = m.lastTitle
			}
		}
	}

	if len(aggs) == 0 {
		return nil
	}

	for _, agg := range aggs {
		if err := e.upsertAggregate(ctx, tenantID, sourceID, agg, now); err != nil {
			e.logf("mapactivity: upsert failed kind=%s id=%s: %v", agg.kind, agg.itemID, err)
		}
	}
	return nil
}

func (e *Engine) matchEvent(ev ObservedEvent, now time.Time) []*matchAggregate {
	if e.catalog == nil {
		return nil
	}
	text := normalizePhrase(strings.Join([]string{ev.Title, ev.Summary, ev.Category, ev.FeedType, ev.Level}, " "))
	if text == "" {
		return nil
	}
	if isLowSignalEvent(text) {
		return nil
	}
	matchedAt := ev.OccurredAt.UTC()
	if matchedAt.IsZero() {
		matchedAt = now
	}
	if matchedAt.After(now.Add(2 * time.Hour)) {
		matchedAt = now
	}
	intensity := eventIntensity(ev, now)
	if intensity <= 0 {
		return nil
	}
	explicitCountry := strings.ToUpper(strings.TrimSpace(ev.CountryCode))
	inferredCountries := e.catalog.inferCountryCodes(text)
	topic := buildEventTopicProfile(text, ev.Category, ev.Level)

	zones := make([]*matchAggregate, 0, 8)
	for _, z := range e.catalog.Zones {
		kw := keywordMatchScore(text, z.Keywords)
		kw += zoneHeuristicScore(text, z.ZoneType)
		kw += zoneTopicScore(z.ZoneType, topic)
		countryBoost := 0.0
		switch {
		case explicitCountry != "" && explicitCountry == z.CountryISO2:
			countryBoost = 1.0
		case stringInSet(z.CountryISO2, inferredCountries):
			countryBoost = 0.42
		}
		if countryBoost == 0 && kw < 0.9 {
			continue
		}
		if countryBoost > 0 && kw < 0.45 {
			continue
		}
		confidence := countryBoost + kw
		if confidence < 0.75 {
			continue
		}
		boost := intensity * (0.6 + confidence*0.52) * 6.0
		zones = append(zones, &matchAggregate{
			kind:          "zone",
			itemID:        z.ID,
			itemName:      z.Name,
			countryISO2:   z.CountryISO2,
			countryName:   z.CountryName,
			zoneType:      z.ZoneType,
			boost:         boost,
			hits:          0.6 + confidence,
			maxConfidence: confidence,
			lastMatchedAt: matchedAt,
			lastTitle:     strings.TrimSpace(ev.Title),
		})
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].boost > zones[j].boost })
	if len(zones) > 6 {
		zones = zones[:6]
	}

	flows := make([]*matchAggregate, 0, 8)
	for _, f := range e.catalog.Flows {
		kw := keywordMatchScore(text, f.Keywords)
		kw += flowHeuristicScore(text, f.FlowMode)
		kw += flowTopicScore(f.FlowMode, topic)
		if kw < 1.0 {
			continue
		}
		countryBoost := 0.0
		if explicitCountry != "" && stringInSet(explicitCountry, f.CountryHints) {
			countryBoost = 0.5
		} else if intersectsFold(f.CountryHints, inferredCountries) {
			countryBoost = 0.25
		}
		confidence := kw + countryBoost
		boost := intensity * (0.45 + confidence*0.55) * 5.0
		flows = append(flows, &matchAggregate{
			kind:          "flow",
			itemID:        f.ID,
			itemName:      f.Name,
			flowMode:      f.FlowMode,
			boost:         boost,
			hits:          0.5 + confidence,
			maxConfidence: confidence,
			lastMatchedAt: matchedAt,
			lastTitle:     strings.TrimSpace(ev.Title),
		})
	}
	sort.Slice(flows, func(i, j int) bool { return flows[i].boost > flows[j].boost })
	if len(flows) > 8 {
		flows = flows[:8]
	}

	out := make([]*matchAggregate, 0, len(zones)+len(flows))
	out = append(out, zones...)
	out = append(out, flows...)
	return out
}

func isLowSignalEvent(text string) bool {
	if text == "" {
		return true
	}
	noiseHits := 0
	for _, marker := range lowSignalNoiseMarkers {
		if strings.Contains(text, marker) {
			noiseHits++
		}
	}
	if noiseHits == 0 {
		return false
	}
	for _, marker := range highSignalRiskMarkers {
		if strings.Contains(text, marker) {
			return false
		}
	}
	return noiseHits >= 1
}

func buildEventTopicProfile(text, category, level string) eventTopicProfile {
	text = normalizePhrase(strings.Join([]string{text, category, level}, " "))
	matchAny := func(terms ...string) bool {
		for _, t := range terms {
			if strings.Contains(text, normalizePhrase(t)) {
				return true
			}
		}
		return false
	}
	return eventTopicProfile{
		military: matchAny(
			"military", "war", "missile", "drone", "airstrike", "troops", "frigate", "warship",
			"submarine", "fighter jet", "interceptor", "deterrence", "arsenal",
		),
		cyber: matchAny(
			"cyber", "ransomware", "phishing", "botnet", "malware", "exploit", "breach",
		),
		shipping: matchAny(
			"shipping", "maritime", "vessel", "tanker", "container ship", "port", "strait", "canal",
		),
		energy: matchAny(
			"oil", "crude", "gas", "lng", "refinery", "pipeline", "energy terminal", "opec",
		),
		infrastructure: matchAny(
			"blackout", "internet outage", "service outage", "cable cut", "network outage", "grid",
			"port congestion", "supply chain",
		),
		disaster: matchAny(
			"earthquake", "flood", "wildfire", "hurricane", "storm", "humanitarian crisis",
		),
		economic: matchAny(
			"sanctions", "embargo", "tariff", "trade war", "inflation", "food price", "hunger", "gdp",
		),
		protest:    matchAny("protest", "riot", "unrest", "demonstration"),
		diplomatic: matchAny("summit", "ceasefire", "negotiation", "agreement", "treaty"),
	}
}

func zoneTopicScore(zoneType string, topic eventTopicProfile) float64 {
	zoneType = strings.ToLower(strings.TrimSpace(zoneType))
	switch zoneType {
	case "logistics_chokepoint":
		switch {
		case topic.military && topic.shipping:
			return 0.45
		case topic.shipping && topic.infrastructure:
			return 0.55
		case topic.shipping:
			return 0.35
		}
	case "energy_hub":
		switch {
		case topic.energy && topic.economic:
			return 0.55
		case topic.energy:
			return 0.4
		case topic.infrastructure && topic.energy:
			return 0.48
		}
	case "data_infrastructure":
		switch {
		case topic.cyber && topic.infrastructure:
			return 0.62
		case topic.cyber:
			return 0.42
		case topic.infrastructure:
			return 0.3
		}
	case "resource_extraction":
		switch {
		case topic.energy && topic.economic:
			return 0.4
		case topic.economic:
			return 0.25
		}
	case "industrial_cluster", "special_economic_zone":
		if topic.economic || topic.infrastructure {
			return 0.25
		}
	case "military_industrial":
		if topic.military {
			return 0.55
		}
	}
	return 0
}

func flowTopicScore(flowMode string, topic eventTopicProfile) float64 {
	flowMode = strings.ToLower(strings.TrimSpace(flowMode))
	switch flowMode {
	case "sea", "ferry_roro":
		if topic.military && topic.shipping {
			return 0.55
		}
		if topic.shipping {
			return 0.45
		}
	case "air":
		if topic.military || topic.disaster {
			return 0.35
		}
	case "road", "rail", "inland_waterway", "intermodal":
		if topic.infrastructure || topic.disaster || topic.protest {
			return 0.28
		}
	case "pipeline_oil", "pipeline_gas", "pipeline_h2", "pipeline_co2":
		if topic.infrastructure && topic.energy {
			return 0.6
		}
		if topic.energy {
			return 0.52
		}
	case "power_hvdc", "power_hvac":
		if topic.infrastructure || topic.energy {
			return 0.5
		}
	case "internet_cable":
		if topic.cyber && topic.infrastructure {
			return 0.68
		}
		if topic.cyber {
			return 0.45
		}
	}
	return 0
}

func zoneHeuristicScore(text, zoneType string) float64 {
	zoneType = strings.ToLower(strings.TrimSpace(zoneType))
	terms := zoneTypeHeuristicKeywords[zoneType]
	if len(terms) == 0 {
		return 0
	}
	return keywordMatchScore(text, terms) * 0.7
}

func flowHeuristicScore(text, flowMode string) float64 {
	flowMode = strings.ToLower(strings.TrimSpace(flowMode))
	terms := flowModeHeuristicKeywords[flowMode]
	if len(terms) == 0 {
		return 0
	}
	return keywordMatchScore(text, terms) * 0.65
}

func stringInSet(value string, arr []string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || len(arr) == 0 {
		return false
	}
	for _, v := range arr {
		if strings.ToUpper(strings.TrimSpace(v)) == value {
			return true
		}
	}
	return false
}

func intersectsFold(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, v := range left {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		seen[v] = struct{}{}
	}
	for _, v := range right {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			return true
		}
	}
	return false
}

func eventIntensity(ev ObservedEvent, now time.Time) float64 {
	sev := strings.ToLower(strings.TrimSpace(ev.Severity))
	sevW := 1.0
	switch sev {
	case "critical", "high", "severe", "major":
		sevW = 1.55
	case "medium", "moderate":
		sevW = 1.0
	case "low", "minor", "info":
		sevW = 0.7
	}
	lvl := strings.ToLower(strings.TrimSpace(ev.Level))
	lvlW := 1.0
	switch lvl {
	case "critical":
		lvlW = 1.7
	case "high":
		lvlW = 1.35
	case "medium":
		lvlW = 1.0
	case "low", "info":
		lvlW = 0.75
	}
	cat := strings.ToLower(strings.TrimSpace(ev.Category))
	catW := 1.0
	switch cat {
	case "conflict", "terrorism", "military", "disaster":
		catW = 1.35
	case "cyber", "infrastructure", "protest", "diplomatic":
		catW = 1.15
	case "economic":
		catW = 1.05
	}
	occurred := ev.OccurredAt.UTC()
	if occurred.IsZero() {
		occurred = now
	}
	ageH := now.Sub(occurred).Hours()
	if ageH < 0 {
		ageH = 0
	}
	if ageH > 168 {
		return 0
	}
	freshW := math.Exp(-ageH / 24.0)
	if freshW < 0.12 {
		freshW = 0.12
	}
	return sevW * lvlW * catW * freshW
}

func keywordMatchScore(text string, keywords []string) float64 {
	if text == "" || len(keywords) == 0 {
		return 0
	}
	score := 0.0
	for _, kw := range keywords {
		kw = normalizePhrase(kw)
		if len(kw) < 3 {
			continue
		}
		if strings.Contains(text, kw) {
			w := 0.55
			if strings.Contains(kw, " ") {
				w = 1.15
			} else if len(kw) >= 8 {
				w = 0.8
			}
			score += w
			if score >= 4.2 {
				break
			}
		}
	}
	return math.Min(2.8, score)
}

func (e *Engine) upsertAggregate(ctx context.Context, tenantID, sourceID string, agg *matchAggregate, now time.Time) error {
	if agg == nil || agg.itemID == "" {
		return nil
	}
	factID := mapActivityFactID(agg.kind, agg.itemID)
	state := persistedState{}
	createdAt := now
	sourceIDs := []string{strings.TrimSpace(sourceID)}

	existing, err := e.factsRepo.Get(ctx, factID)
	if err == nil && existing != nil {
		state = parsePersistedState(existing)
		createdAt = existing.CreatedAt
		sourceIDs = append(sourceIDs, existing.SourceIDs...)
	} else if err != nil {
		if status.Code(err) != codes.NotFound {
			return err
		}
	}

	state.applyDecay(now)
	state.Score = clamp(state.Score+agg.boost, 0, 100)
	state.Hits1h = clamp(state.Hits1h+agg.hits, 0, 2000)
	state.Hits24h = clamp(state.Hits24h+agg.hits, 0, 10000)
	state.TotalMentions = clamp(state.TotalMentions+1, 0, 1e9)
	state.LastBoost = agg.boost
	if agg.lastMatchedAt.IsZero() {
		agg.lastMatchedAt = now
	}
	state.LastMatchedAt = agg.lastMatchedAt
	state.LastUpdatedAt = now

	factType := "zone_activity"
	cp := "/map/activity/zone/" + slug(agg.itemID)
	if agg.kind == "flow" {
		factType = "flow_activity"
		cp = "/map/activity/flow/" + slug(agg.itemID)
	}
	canonicalPaths := []string{cp}
	var locPtr *string
	if agg.kind == "zone" && agg.countryISO2 != "" {
		lp := "/loc/" + strings.ToLower(strings.TrimSpace(agg.countryISO2))
		canonicalPaths = append(canonicalPaths, lp)
		locPtr = &lp
	}

	summary := fmt.Sprintf("score=%.2f hits1h=%.2f hits24h=%.2f", state.Score, state.Hits1h, state.Hits24h)
	if strings.TrimSpace(agg.lastTitle) != "" {
		summary += " | " + strings.TrimSpace(agg.lastTitle)
	}

	fact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             factID,
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      createdAt,
			UpdatedAt:      now,
			CreatedBy:      "horizon_monitor_pipeline",
			UpdatedBy:      "horizon_monitor_pipeline",
			Labels:         map[string]string{"en": agg.itemName},
			Description:    strings.TrimSpace(agg.lastTitle),
			Status:         "active",
			CanonicalPaths: canonicalPaths,
			LocationPath:   locPtr,
			Tags: uniqueStrings([]string{
				"horizon-monitor",
				"map-activity",
				"processing_mode:heuristic",
				"llm:false",
				"map_kind:" + agg.kind,
				"map_item_id:" + agg.itemID,
				"map_schema:" + strings.TrimSpace(e.catalog.Schema),
				"zone_type:" + strings.ToLower(strings.TrimSpace(agg.zoneType)),
				"flow_mode:" + strings.ToLower(strings.TrimSpace(agg.flowMode)),
				"country:" + strings.ToLower(strings.TrimSpace(agg.countryISO2)),
			}),
		},
		Kind:           mapActivityFactKind,
		Type:           factType,
		Title:          agg.itemName,
		Summary:        summary,
		Confidence:     clamp(state.Score/100.0, 0.05, 0.99),
		CanonicalNames: canonicalNamesForAggregate(agg),
		OccurredAt:     &state.LastMatchedAt,
		ObservedAt:     &now,
		SourceIDs:      uniqueStrings(sourceIDs),
		SourceRefs:     []coremodels.SourceRef{{SourceID: agg.itemID, SourceType: "map_item"}},
		MetricUpdates: []coremodels.FactMetricUpdate{
			{MetricName: metricScore, Value: state.Score, Confidence: 0.95, ObservedAt: &now},
			{MetricName: metricHits1h, Value: state.Hits1h, Confidence: 0.95, ObservedAt: &now},
			{MetricName: metricHits24h, Value: state.Hits24h, Confidence: 0.95, ObservedAt: &now},
			{MetricName: metricTotalMent, Value: state.TotalMentions, Confidence: 0.95, ObservedAt: &now},
			{MetricName: metricLastBoost, Value: state.LastBoost, Confidence: 0.95, ObservedAt: &now},
			{MetricName: metricLastMatchTS, Value: float64(state.LastMatchedAt.Unix()), Confidence: 0.95, ObservedAt: &now},
		},
	}
	return e.factsRepo.Upsert(ctx, fact)
}

func canonicalNamesForAggregate(agg *matchAggregate) []string {
	if agg == nil {
		return nil
	}
	return uniqueStrings([]string{agg.itemName, agg.countryName, agg.zoneType, agg.flowMode})
}

type persistedState struct {
	Score         float64
	Hits1h        float64
	Hits24h       float64
	TotalMentions float64
	LastBoost     float64
	LastMatchedAt time.Time
	LastUpdatedAt time.Time
}

func parsePersistedState(f *coremodels.OntologyFact) persistedState {
	st := persistedState{}
	if f == nil {
		return st
	}
	for _, mu := range f.MetricUpdates {
		switch strings.TrimSpace(mu.MetricName) {
		case metricScore:
			st.Score = mu.Value
		case metricHits1h:
			st.Hits1h = mu.Value
		case metricHits24h:
			st.Hits24h = mu.Value
		case metricTotalMent:
			st.TotalMentions = mu.Value
		case metricLastBoost:
			st.LastBoost = mu.Value
		case metricLastMatchTS:
			if mu.Value > 0 {
				st.LastMatchedAt = time.Unix(int64(mu.Value), 0).UTC()
			}
		}
	}
	if st.LastMatchedAt.IsZero() && f.OccurredAt != nil {
		st.LastMatchedAt = f.OccurredAt.UTC()
	}
	st.LastUpdatedAt = f.UpdatedAt.UTC()
	if st.LastUpdatedAt.IsZero() && f.ObservedAt != nil {
		st.LastUpdatedAt = f.ObservedAt.UTC()
	}
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = time.Now().UTC()
	}
	return st
}

func (s *persistedState) applyDecay(now time.Time) {
	if s == nil {
		return
	}
	deltaH := now.Sub(s.LastUpdatedAt).Hours()
	if deltaH <= 0 {
		return
	}
	s.Score = decayExp(s.Score, deltaH, halfLifeScoreH)
	s.Hits1h = decayExp(s.Hits1h, deltaH, halfLife1hH)
	s.Hits24h = decayExp(s.Hits24h, deltaH, halfLife24hH)
	s.LastUpdatedAt = now
}

func decayExp(v, deltaHours, halfLifeHours float64) float64 {
	if v <= 0 || deltaHours <= 0 || halfLifeHours <= 0 {
		return v
	}
	factor := math.Exp(-math.Ln2 * deltaHours / halfLifeHours)
	if factor < 0 {
		factor = 0
	}
	return v * factor
}

func mapActivityFactID(kind, itemID string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(kind + "|" + itemID)))
	return "hm_map_activity_" + kind + "_" + hex.EncodeToString(h[:8])
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "--", "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return strconv.FormatInt(time.Now().UTC().Unix(), 10)
	}
	return s
}

func (e *Engine) logf(format string, args ...interface{}) {
	if e != nil && e.logger != nil {
		e.logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}
