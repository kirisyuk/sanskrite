package discovery

import (
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestNormalizeHorizonMonitorIngestMode(t *testing.T) {
	ds := &coremodels.DataSource{}
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeDocumentFeed {
		t.Fatalf("expected default mode %s, got %s", wmIngestModeDocumentFeed, got)
	}

	v := wmIngestModeOntologyFact
	ds.Details.HorizonMonitorIngestMode = &v
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeOntologyFact {
		t.Fatalf("expected mode %s, got %s", wmIngestModeOntologyFact, got)
	}

	bad := "unknown_mode"
	ds.Details.HorizonMonitorIngestMode = &bad
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeDocumentFeed {
		t.Fatalf("expected fallback mode %s, got %s", wmIngestModeDocumentFeed, got)
	}
}

func TestSignalTypeFromFeedType(t *testing.T) {
	cases := map[string]string{
		"gdelt_geo_protest": "risk:protest",
		"opensky_flights":   "risk:military_flight",
		"ais_snapshot":      "risk:ais_disruption",
		"service_status":    "risk:service_outage",
	}
	for in, want := range cases {
		if got := signalTypeFromFeedType(in); got != want {
			t.Fatalf("feedType=%s: want %s got %s", in, want, got)
		}
	}
}

func TestHorizonMonitorBlueprintFromSignal(t *testing.T) {
	bp, ok := horizonMonitorBlueprintFromSignal("risk:maritime_hazard")
	if !ok {
		t.Fatalf("expected blueprint for risk:maritime_hazard")
	}
	if bp.Axis == "" || bp.Mechanism == "" {
		t.Fatalf("blueprint should include axis and mechanism")
	}
}

func TestClassifyHorizonMonitorNewsKeyword(t *testing.T) {
	cls := classifyHorizonMonitorNewsKeyword("Missile attack reported after military escalation")
	if cls.Level != "high" && cls.Level != "critical" {
		t.Fatalf("expected high/critical level, got %s", cls.Level)
	}
	if cls.Category == "general" {
		t.Fatalf("expected specific category, got %s", cls.Category)
	}
}

func TestInferHorizonMonitorSignalType_GenericNews(t *testing.T) {
	cases := []struct {
		name  string
		event horizonMonitorFeedEvent
		want  string
	}{
		{
			name: "maritime chokepoint",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "Houthis threaten shipping through the Strait of Hormuz",
				Summary:  "Regional escalation raises tanker risk in the chokepoint.",
			},
			want: "risk:maritime_hazard",
		},
		{
			name: "cyber outage",
			event: horizonMonitorFeedEvent{
				Category: "cyber",
				Title:    "Telecom outage after cyber attack disrupts mobile network",
			},
			want: "risk:internet_outage",
		},
		{
			name: "street protest",
			event: horizonMonitorFeedEvent{
				Category: "mainstream",
				Title:    "Mass protest erupts after election dispute",
			},
			want: "risk:protest",
		},
		{
			name: "military strikes are not protest",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "3,000 strikes, 43 ships hit: US operations against Iran by the numbers",
			},
			want: "risk:conflict_event",
		},
		{
			name: "typhoons aircraft are not climate anomaly",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "UK sends additional Typhoons to bolster Qatar's air defence",
				Summary:  "Royal Air Force fighters reinforce regional posture.",
			},
			want: "risk:conflict_event",
		},
		{
			name: "production planning is not operational conflict signal",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "Missile makers agree to quadruple production, Trump says",
				Summary:  "Industry planning and production expansion were discussed.",
			},
			want: "risk:temporal_anomaly",
		},
		{
			name: "cyber strategy article is not maritime hazard",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "Trump's new cyber strategy calls for tougher responses to threats",
				Summary:  "Policy document outlines future cyber response posture.",
			},
			want: "risk:temporal_anomaly",
		},
		{
			name: "anti-drone defense article is not aviation disruption",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "Pentagon task force to conduct laser test against drones",
				Summary:  "Task force prepares anti-drone defense test for regional posture.",
			},
			want: "risk:conflict_event",
		},
		{
			name: "military hardware headline is not protest",
			event: horizonMonitorFeedEvent{
				Category: "defense",
				Title:    "Israeli F-16 Appears With Mysterious JDAM Precision Bomb",
				Summary:  "Military hardware sighting raises questions about strike configuration.",
			},
			want: "risk:conflict_event",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(tc.event.Title), &tc.event)
			if got := inferHorizonMonitorSignalType("", &tc.event, cls); got != tc.want {
				t.Fatalf("want %s got %s", tc.want, got)
			}
		})
	}
}

func TestHeuristicSignalSeedNames(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Houthis threaten shipping through the Strait of Hormuz",
		Summary:     "The Pentagon and IRGC posture changes raise tanker concerns.",
		CountryName: "Iran",
	}
	seeds := heuristicSignalSeedNames(e)
	joined := strings.ToLower(strings.Join(seeds, " | "))
	for _, want := range []string{"strait of hormuz", "houthis", "united states department of defense", "islamic revolutionary guard corps"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected seed list to contain %q, got %v", want, seeds)
		}
	}
}

func TestHeuristicSignalSeedNames_IncludeGeoAnchors(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "UK sends additional Typhoons to bolster Qatar's air defence",
		Summary: "Royal Air Force fighters reinforce regional posture as Iran tensions persist.",
	}
	seeds := heuristicSignalSeedNames(e)
	joined := strings.ToLower(strings.Join(seeds, " | "))
	for _, want := range []string{"qatar", "qatar air defense", "united kingdom", "united kingdom air force"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected geo anchor seed %q in %v", want, seeds)
		}
	}
}

func TestHeuristicSignalSeedPaths_MultiCountry(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "UK sends additional Typhoons to bolster Qatar's air defence",
		Summary: "Royal Air Force fighters reinforce regional posture as Iran tensions persist.",
	}
	paths := heuristicSignalSeedPaths(e, "/loc/gb")
	joined := strings.Join(paths, "|")
	for _, want := range []string{"/loc/ir", "/loc/qa"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected seed path %q in %v", want, paths)
		}
	}
	if strings.Contains(joined, "/loc/gb") {
		t.Fatalf("did not expect primary location path duplicated in seed paths: %v", paths)
	}
}

func TestHeuristicSignalSeedNames_FiltersHeadlineNoise(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "Related Posts: Backstage Partner and Exquisite Class in Iran",
		Summary: "House Armed Services Committee and Defense Production Act remain central.",
	}
	seeds := heuristicSignalSeedNames(e)
	joined := strings.ToLower(strings.Join(seeds, " | "))
	for _, bad := range []string{"related posts", "backstage partner", "exquisite class", "in iran"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unexpected noisy seed %q in %v", bad, seeds)
		}
	}
	for _, want := range []string{"house armed services committee", "defense production act"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected useful seed %q in %v", want, seeds)
		}
	}
}

func TestHeuristicSignalSeedNames_FiltersDocumentMarkupNoise(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "TOP SECRET HAND DISTRIBUTION ONLY",
		Summary: "CODEX LEVEL II PRINTED BY regional desk while BAE Systems coordinates with Israel port authorities.",
	}
	seeds := heuristicSignalSeedNames(e)
	joined := strings.ToLower(strings.Join(seeds, " | "))
	for _, bad := range []string{"top secret", "hand distribution only", "codex level ii", "printed by"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unexpected document-markup seed %q in %v", bad, seeds)
		}
	}
	if !strings.Contains(joined, "bae systems") {
		t.Fatalf("expected operational seed to survive, got %v", seeds)
	}
}

func TestExtractCountryCodesFromText_AmbiguityPolicy(t *testing.T) {
	if got := extractCountryCodesFromText("Port of Savannah, Georgia handles container traffic", 2); len(got) != 0 {
		t.Fatalf("expected georgia state context to be ignored, got %v", got)
	}
	got := extractCountryCodesFromText("RAF adds to drone tally over Jordan as Amman coordinates response", 2)
	if len(got) == 0 || got[0] != "JO" {
		t.Fatalf("expected Jordan country detection, got %v", got)
	}
	if got := extractCountryCodesFromText("Michael Jordan brand expands retail footprint", 2); len(got) != 0 {
		t.Fatalf("expected person context to be ignored, got %v", got)
	}
}

func TestShouldSuppressHeuristicSignalPropagation(t *testing.T) {
	if !shouldSuppressHeuristicSignalPropagation(&horizonMonitorFeedEvent{
		FeedType:   "breaking-defense",
		SignalType: "risk:temporal_anomaly",
		Title:      "How technology, strategy and industry are shaping the next fleet",
	}) {
		t.Fatal("expected generic analysis temporal anomaly to be suppressed")
	}
	if shouldSuppressHeuristicSignalPropagation(&horizonMonitorFeedEvent{
		FeedType:   "breaking-defense",
		SignalType: "risk:temporal_anomaly",
		Title:      "Missile strike disrupts operations at regional base",
	}) {
		t.Fatal("expected concrete disruptive event to be retained")
	}
	if shouldSuppressHeuristicSignalPropagation(&horizonMonitorFeedEvent{
		FeedType:   "usgs_earthquakes",
		SignalType: "risk:temporal_anomaly",
		Title:      "M 5.7 - Southern Mid-Atlantic Ridge",
	}) {
		t.Fatal("expected machine feed temporal anomaly to be retained")
	}
	if !shouldSuppressHeuristicSignalPropagation(&horizonMonitorFeedEvent{
		FeedType:   "intel_rss",
		SignalType: "risk:temporal_anomaly",
		Title:      "Trump's new cyber strategy calls for tougher responses to threats",
		Summary:    "Policy document outlines future cyber response posture.",
	}) {
		t.Fatal("expected strategic planning article to be suppressed")
	}
	if !shouldSuppressHeuristicSignalPropagation(&horizonMonitorFeedEvent{
		FeedType:   "intel_rss",
		SignalType: "risk:maritime_hazard",
		Title:      "Bunker Talk: Let’s Talk About All The Things We Did And Didn’t Cover This Week",
		Summary:    "Commentary and roundup episode without a concrete disruptive event.",
	}) {
		t.Fatal("expected roundup commentary to be suppressed even if misclassified")
	}
}

func TestMergeAndAggregate(t *testing.T) {
	p := NewHorizonMonitorPipeline(nil, nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	countries, cells := p.mergeAndAggregate([]horizonMonitorSignalObservation{
		{
			CountryCode:    "US",
			CountryName:    "United States",
			SignalType:     "risk:service_outage",
			Severity:       "high",
			Timestamp:      now,
			Lat:            37.7,
			Lon:            -122.4,
			HasCoordinates: true,
			SourceID:       "src-1",
		},
		{
			CountryCode:    "US",
			CountryName:    "United States",
			SignalType:     "risk:protest",
			Severity:       "medium",
			Timestamp:      now,
			Lat:            37.8,
			Lon:            -122.3,
			HasCoordinates: true,
			SourceID:       "src-2",
		},
	})

	us, ok := countries["US"]
	if !ok {
		t.Fatalf("expected US country aggregate")
	}
	if us.TotalSignals != 2 {
		t.Fatalf("expected totalSignals=2, got %d", us.TotalSignals)
	}
	if us.DistinctSignalTypes != 2 {
		t.Fatalf("expected distinctSignalTypes=2, got %d", us.DistinctSignalTypes)
	}
	if us.ConvergenceScore <= 0 {
		t.Fatalf("expected positive convergence score")
	}
	if len(cells) == 0 {
		t.Fatalf("expected at least one grid cell aggregate")
	}
}

func TestTrackCanonicalPath(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		SourceID:       "src-opensky",
		ExternalID:     "evt-1",
		Title:          "RCH123",
		CountryCode:    "US",
		Lat:            37.6213,
		Lon:            -122.3790,
		HasCoordinates: true,
	}
	tr := horizonTrackObservation{
		Kind:            horizonmodels.TrackKindAir,
		ObjectType:      "military_aircraft",
		ExternalTrackID: "abc123",
		DisplayName:     "RCH123",
	}

	got := trackCanonicalPath(e, tr)
	if !strings.HasPrefix(got, "/track/air/us/") {
		t.Fatalf("expected /track/air/us prefix, got %q", got)
	}
	if !strings.Contains(got, "rch123-") {
		t.Fatalf("expected slugged display name in canonical path, got %q", got)
	}
}

func TestTrackCanonicalPathIdInPath(t *testing.T) {
	e := &horizonMonitorFeedEvent{CountryCode: "ID", SourceID: "src-ais", ExternalID: "evt-1"}
	tr := horizonTrackObservation{
		Kind:            horizonmodels.TrackKindSea,
		ObjectType:      "vessel",
		IdScheme:        "mmsi",
		ExternalTrackID: "525123456",
		DisplayName:     "MV Test",
	}
	got := trackCanonicalPath(e, tr)
	want := "/track/sea/id/mmsi-525123456"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	e2 := &horizonMonitorFeedEvent{CountryCode: "US", SourceID: "src-opensky", ExternalID: "evt-2"}
	tr2 := horizonTrackObservation{
		Kind:            horizonmodels.TrackKindAir,
		ObjectType:      "military_aircraft",
		IdScheme:        "icao24",
		ExternalTrackID: "a1b2c3",
		DisplayName:     "RCH456",
	}
	got2 := trackCanonicalPath(e2, tr2)
	want2 := "/track/air/us/icao24-a1b2c3"
	if got2 != want2 {
		t.Fatalf("got %q, want %q", got2, want2)
	}
}

func TestInferAffectedZoneCandidate_WarmOnlyForOngoingConflict(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Iran conflict 2026: UK sends additional Typhoons to bolster Qatar's air defence",
		Summary:     "Analysis of an ongoing regional confrontation and force posture.",
		CountryCode: "IR",
		CountryName: "Iran",
	}
	cls := classifyHorizonMonitorNewsKeyword(e.Title)
	c := inferAffectedZoneCandidate(e, cls)
	if c == nil {
		t.Fatal("expected candidate for ongoing conflict headline")
	}
	if c.ConditionType != "military_conflict" {
		t.Fatalf("expected military_conflict, got %q", c.ConditionType)
	}
	if !c.WarmOnly {
		t.Fatalf("expected warm-only candidate, got %+v", c)
	}
}

func TestInferAffectedZoneCoordinates_GeoPrecision(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Lat:            35.6892,
		Lon:            51.3890,
		HasCoordinates: true,
	}
	_, _, hasCoord, precision := inferAffectedZoneCoordinates(e, "Tehran", "IR")
	if !hasCoord || precision != "precise" {
		t.Fatalf("expected precise geo from event coordinates, got hasCoord=%v precision=%q", hasCoord, precision)
	}

	_, _, hasCoord2, precision2 := inferAffectedZoneCoordinates(nil, "", "IR")
	if !hasCoord2 || precision2 != "approximate_country" {
		t.Fatalf("expected approximate country geo fallback, got hasCoord=%v precision=%q", hasCoord2, precision2)
	}
}

func TestMergeTopicGeoAggregates(t *testing.T) {
	p := NewHorizonMonitorPipeline(nil, nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	aggs := p.mergeTopicGeoAggregates([]horizonMonitorTopicGeoObservation{
		{CountryCode: "IR", CountryName: "Iran", Topic: "artificial-intelligence", Timestamp: now, SourceID: "src-1"},
		{CountryCode: "IR", CountryName: "Iran", Topic: "artificial-intelligence", Timestamp: now, SourceID: "src-2"},
		{CountryCode: "DE", CountryName: "Germany", Topic: "semiconductors", Timestamp: now, SourceID: "src-3"},
	})
	ir, ok := aggs["ir|artificial-intelligence"]
	if !ok {
		t.Fatal("expected Iran AI aggregate")
	}
	if ir.TotalMentions != 2 {
		t.Fatalf("expected 2 mentions, got %d", ir.TotalMentions)
	}
	if len(ir.SourceIDs) != 2 {
		t.Fatalf("expected 2 unique source IDs, got %d", len(ir.SourceIDs))
	}
}

func TestHorizonMonitorLineageSignal(t *testing.T) {
	lineage := horizonMonitorLineage(coremodels.FactExtractionLayerSignal, false, false)
	if lineage == nil {
		t.Fatal("expected lineage")
	}
	if lineage.CurrentLayer != coremodels.FactExtractionLayerSignal {
		t.Fatalf("unexpected current layer: %q", lineage.CurrentLayer)
	}
	if len(lineage.Steps) < 3 {
		t.Fatalf("expected source+heuristic+signal steps, got %d", len(lineage.Steps))
	}
	if lineage.Steps[0].Layer != coremodels.FactExtractionLayerSourceMechanics {
		t.Fatalf("expected source mechanics first, got %q", lineage.Steps[0].Layer)
	}
}

func TestRSSSignalFlowCountersObserve(t *testing.T) {
	var counters rssSignalFlowCounters

	counters.observe(&horizonMonitorSignalFlowAudit{
		Axis:                 "supply",
		SeedCount:            3,
		AffectedFragments:    7,
		PathsTraced:          5,
		TraceDropped:         2,
		ChainRefs:            1,
		EscalationLevel:      horizonmodels.EscalationLevelWarning,
		PropagationTried:     true,
		LocationOnly:         true,
		SemanticSeedNames:    2,
		BusinessEndpoints:    2,
		NonObviousEndpoints:  1,
		FacilityEndpoints:    1,
		RemoteEndpoints:      2,
		LongPathEndpoints:    1,
		PathFamilies:         2,
		BusinessPathFamilies: 1,
		CrossBorderFamilies:  1,
		Breakpoints:          2,
		ThresholdBreakpoints: 1,
		TransferSkips:        1,
		HopLimitBreakpoints:  1,
		BusinessBreakpoints:  2,
	})
	counters.observe(&horizonMonitorSignalFlowAudit{
		Axis:             horizonmodels.AxisInfoUncertainty,
		EscalationLevel:  horizonmodels.EscalationLevelWatch,
		SeedCount:        1,
		PropagationTried: true,
	})

	data := counters.telemetryCounters()
	if data["signals_total"] != 2 {
		t.Fatalf("expected signals_total=2, got %d", data["signals_total"])
	}
	if data["signals_seeded_total"] != 2 {
		t.Fatalf("expected signals_seeded_total=2, got %d", data["signals_seeded_total"])
	}
	if data["signals_no_seed_total"] != 0 {
		t.Fatalf("expected signals_no_seed_total=0, got %d", data["signals_no_seed_total"])
	}
	if data["signals_propagated_total"] != 1 {
		t.Fatalf("expected signals_propagated_total=1, got %d", data["signals_propagated_total"])
	}
	if data["signals_location_only_total"] != 1 {
		t.Fatalf("expected signals_location_only_total=1, got %d", data["signals_location_only_total"])
	}
	if data["signals_semantic_named_total"] != 1 {
		t.Fatalf("expected signals_semantic_named_total=1, got %d", data["signals_semantic_named_total"])
	}
	if data["signals_info_uncertainty_total"] != 1 {
		t.Fatalf("expected signals_info_uncertainty_total=1, got %d", data["signals_info_uncertainty_total"])
	}
	if data["signals_business_endpoint_total"] != 1 {
		t.Fatalf("expected signals_business_endpoint_total=1, got %d", data["signals_business_endpoint_total"])
	}
	if data["signals_zero_business_total"] != 1 {
		t.Fatalf("expected signals_zero_business_total=1, got %d", data["signals_zero_business_total"])
	}
	if data["signals_non_obvious_endpoint_total"] != 1 {
		t.Fatalf("expected signals_non_obvious_endpoint_total=1, got %d", data["signals_non_obvious_endpoint_total"])
	}
	if data["signals_remote_endpoint_total"] != 1 {
		t.Fatalf("expected signals_remote_endpoint_total=1, got %d", data["signals_remote_endpoint_total"])
	}
	if data["signals_long_path_endpoint_total"] != 1 {
		t.Fatalf("expected signals_long_path_endpoint_total=1, got %d", data["signals_long_path_endpoint_total"])
	}
	if data["signals_path_family_total"] != 1 {
		t.Fatalf("expected signals_path_family_total=1, got %d", data["signals_path_family_total"])
	}
	if data["signals_business_path_family_total"] != 1 {
		t.Fatalf("expected signals_business_path_family_total=1, got %d", data["signals_business_path_family_total"])
	}
	if data["signals_cross_border_family_total"] != 1 {
		t.Fatalf("expected signals_cross_border_family_total=1, got %d", data["signals_cross_border_family_total"])
	}
	if data["signals_breakpoint_total"] != 1 {
		t.Fatalf("expected signals_breakpoint_total=1, got %d", data["signals_breakpoint_total"])
	}
	if data["breakpoint_threshold_total"] != 1 {
		t.Fatalf("expected breakpoint_threshold_total=1, got %d", data["breakpoint_threshold_total"])
	}
	if data["breakpoint_transfer_skip_total"] != 1 {
		t.Fatalf("expected breakpoint_transfer_skip_total=1, got %d", data["breakpoint_transfer_skip_total"])
	}
	if data["breakpoint_hop_limit_total"] != 1 {
		t.Fatalf("expected breakpoint_hop_limit_total=1, got %d", data["breakpoint_hop_limit_total"])
	}
	if data["breakpoint_business_total"] != 2 {
		t.Fatalf("expected breakpoint_business_total=2, got %d", data["breakpoint_business_total"])
	}
	if data["facility_endpoints_total"] != 1 {
		t.Fatalf("expected facility_endpoints_total=1, got %d", data["facility_endpoints_total"])
	}
	if data["seeds_total"] != 4 || data["affected_fragments_total"] != 7 {
		t.Fatalf("unexpected seeded/affected totals: %+v", data)
	}
	if data["propagation_paths_total"] != 5 || data["trace_dropped_total"] != 2 {
		t.Fatalf("unexpected path/trace totals: %+v", data)
	}
	if data["signals_zero_reach_total"] != 1 {
		t.Fatalf("expected signals_zero_reach_total=1, got %d", data["signals_zero_reach_total"])
	}
	if data["chain_refs_total"] != 1 {
		t.Fatalf("expected chain_refs_total=1, got %d", data["chain_refs_total"])
	}
	if data["warning_or_critical_total"] != 1 {
		t.Fatalf("expected warning_or_critical_total=1, got %d", data["warning_or_critical_total"])
	}
}

func TestHorizonMonitorEscalationLevelFromSeverity(t *testing.T) {
	cases := map[string]string{
		"critical": horizonmodels.EscalationLevelCritical,
		"high":     horizonmodels.EscalationLevelWarning,
		"medium":   horizonmodels.EscalationLevelWatch,
		"low":      horizonmodels.EscalationLevelNone,
	}
	for input, want := range cases {
		if got := horizonMonitorEscalationLevelFromSeverity(input); got != want {
			t.Fatalf("severity=%s: want %s got %s", input, want, got)
		}
	}
}

func TestSelectDeterministicAffectedZoneMatch(t *testing.T) {
	now := time.Now().UTC()
	candidate := &affectedZoneCandidate{
		GeoToken:      "ir",
		ConditionType: "military_conflict",
		ZoneSubType:   "armed_tension",
		Label:         "Tehran Armed Tension",
		GeoPrecision:  "precise",
	}
	fragments := []*horizonmodels.HorizonFragment{
		{
			CanonicalPath: "/zone/ir/military_conflict/tehran-armed-tension",
			Type:          horizonmodels.FragmentTypeAffectedZone,
			Labels:        map[string]string{"en": "Tehran Armed Tension"},
			Qualifiers: map[string]interface{}{
				"zoneSubType":  "armed_tension",
				"geoPrecision": "precise",
			},
			Metadata: horizonmodels.FragmentMetadata{LastUpdatedAt: now},
		},
		{
			CanonicalPath: "/zone/ir/military_conflict/iran-wide-alert",
			Type:          horizonmodels.FragmentTypeAffectedZone,
			Labels:        map[string]string{"en": "Iran Wide Alert"},
			Qualifiers: map[string]interface{}{
				"zoneSubType":  "terror_threat",
				"geoPrecision": "approximate_country",
			},
			Metadata: horizonmodels.FragmentMetadata{LastUpdatedAt: now.Add(-96 * time.Hour)},
		},
	}
	best := selectDeterministicAffectedZoneMatch(candidate, fragments)
	if best == nil {
		t.Fatal("expected deterministic match")
	}
	if best.CanonicalPath != "/zone/ir/military_conflict/tehran-armed-tension" {
		t.Fatalf("unexpected match: %s", best.CanonicalPath)
	}
}
