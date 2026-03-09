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
	ds.Details.OutputMode = &v
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeOntologyFact {
		t.Fatalf("expected mode %s, got %s", wmIngestModeOntologyFact, got)
	}

	bad := "unknown_mode"
	ds.Details.OutputMode = &bad
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

func TestInferAffectedZoneCandidate_SuppressesOpinionAndStaleDrift(t *testing.T) {
	cases := []horizonMonitorFeedEvent{
		{
			Title:   "McCain Delivers His Own Foreign Policy Speech",
			Summary: "Commentary on foreign policy positioning.",
			URL:     "https://www.realcleardefense.com/2016/05/02/mccain_delivers_his_own_foreign_policy_speech_281567.html",
		},
		{
			Title:   "At a Glance: the U.S. Defense Industrial Base",
			Summary: "Overview of the current industrial base posture.",
			URL:     "https://warontherocks.com/2026/01/at-a-glance-the-u-s-defense-industrial-base/",
		},
		{
			Title:   "WHO validates Brazil for eliminating mother-to-child transmission of HIV",
			Summary: "Public health validation and progress update.",
			URL:     "https://www.who.int/news/item/18-12-2025-who-validates-brazil-for-eliminating-mother-to-child-transmission-of-hiv",
		},
	}
	for _, e := range cases {
		cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(e.Title), &e)
		if c := inferAffectedZoneCandidate(&e, cls); c != nil {
			t.Fatalf("expected no candidate for %q, got %+v", e.Title, c)
		}
	}
}

func TestInferAffectedZoneCandidate_PlaceOverridesCountryMentions(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "How the War in Iran Is Disrupting the World's Oil",
		Summary:     "Strait of Hormuz shipping and tanker risk remain in focus.",
		CountryCode: "IL",
		CountryName: "Israel",
		URL:         "https://www.nytimes.com/video/world/middleeast/100000010750140/how-the-war-in-iran-is-disrupting-the-worlds-oil.html",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	c := inferAffectedZoneCandidate(e, cls)
	if c == nil {
		t.Fatal("expected candidate")
	}
	if !strings.Contains(c.CanonicalPath, "/strait-of-hormuz") {
		t.Fatalf("expected strait slug, got %s", c.CanonicalPath)
	}
	if strings.Contains(c.CanonicalPath, "/il/") {
		t.Fatalf("expected place-based geo override, got %s", c.CanonicalPath)
	}
}

func TestExtractKnownPlaceFromText_DoesNotFallbackToCountryMention(t *testing.T) {
	text := "From Bush Sr to Trump: the risks, lessons and legacy of US interference in the Middle East"
	if got := extractKnownPlaceFromText(text); got != "" {
		t.Fatalf("expected no extracted place from generic country mention, got %q", got)
	}
}

func TestShouldSkipAffectedZoneAsStale(t *testing.T) {
	now := time.Date(2026, 3, 8, 22, 0, 0, 0, time.UTC)
	c := &affectedZoneCandidate{ConditionType: "military_conflict"}
	if !shouldSkipAffectedZoneAsStale(now, now.Add(-45*24*time.Hour), &horizonMonitorFeedEvent{URL: "https://www.realcleardefense.com/2024/03/26/japan_and_us_deepen_military_connections_against_china_1020679.html"}, c) {
		t.Fatal("expected stale observation to be skipped")
	}
	if shouldSkipAffectedZoneAsStale(now, now.Add(-2*24*time.Hour), &horizonMonitorFeedEvent{URL: "https://www.nytimes.com/video/world/middleeast/100000010750140/how-the-war-in-iran-is-disrupting-the-worlds-oil.html"}, c) {
		t.Fatal("did not expect fresh observation to be skipped")
	}
}

func TestExtractAffectedZoneObjectAnchors_FiltersGenericCountrySeeds(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "At a Glance: the U.S. Defense Industrial Base",
		Summary:     "The United Kingdom and Greece were referenced in a broad industry snapshot.",
		CountryCode: "US",
		CountryName: "United States",
	}
	got := extractAffectedZoneObjectAnchors(e, 8)
	joined := strings.ToLower(strings.Join(got, " | "))
	for _, bad := range []string{"united kingdom", "greece", "united states government"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unexpected generic anchor %q in %v", bad, got)
		}
	}
}

func TestShouldAttemptAffectedZoneEntityAnchor(t *testing.T) {
	weak := &horizonMonitorFeedEvent{
		Title:   "Iran conflict hits South Hook LNG Terminal logistics",
		Summary: "Operators warned of disruption at the terminal.",
	}
	if !shouldAttemptAffectedZoneEntityAnchor("Iran", "Iran", "approximate_country", false, weak) {
		t.Fatal("expected weak place with operational anchor to attempt entity-anchor lookup")
	}
	strong := &horizonMonitorFeedEvent{
		Title:   "Explosion reported near Bandar Abbas refinery",
		Summary: "Local emergency services responded at the refinery site.",
	}
	if shouldAttemptAffectedZoneEntityAnchor("Bandar Abbas refinery", "Iran", "precise", true, strong) {
		t.Fatal("did not expect precise operational place to trigger entity-anchor lookup")
	}
}

func TestNormalizeAffectedZonePlace_StripsSystemArtifacts(t *testing.T) {
	if got := normalizeAffectedZonePlace("domain-v1-affected-zone-geo-strait-of-hormuz", "Iran"); got != "Iran" {
		t.Fatalf("expected country fallback, got %q", got)
	}
	if got := normalizeAffectedZonePlace("supply-chain/global/ras-laffan", "Qatar"); got != "Qatar" {
		t.Fatalf("expected country fallback, got %q", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesHeadlineLikeGlobalGarbage(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "Decision to authorize seismic code implementation but warn of last 24 hours European risks",
		Summary: "Analysis roundup of implementation and warning themes.",
		URL:     "https://example.com/analysis",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no candidate for headline-like global garbage, got %+v", got)
	}
}

func TestExtractPlaceFromTitle_RejectsNonPlaceHeadlineTail(t *testing.T) {
	title := "Conflict decision in authorize seismic code implementation triggers debate"
	if got := extractPlaceFromTitle(title); got != "" {
		t.Fatalf("expected no place from non-place title tail, got %q", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesWeakEconomicPseudoZone(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:    "Energy prices and inflation remain under pressure",
		Summary:  "Economic analysis of market conditions and policy outlook.",
		Category: "economic",
		URL:      "https://example.com/economy",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected weak economic pseudo-event to be suppressed, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesCyberAdvisoryPseudoEvent(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "Google Confirms CVE-2026-21385 in Qualcomm Android Component Exploited",
		Summary: "Google disclosed a high-severity flaw impacting an open-source Qualcomm component used in Android devices has been exploited in the wild.",
		URL:     "https://thehackernews.com/2026/03/google-confirms-cve-2026-21385-in.html",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if cls.Level != "low" || cls.Category != "cyber" {
		t.Fatalf("expected downgraded cyber advisory classification, got %+v", cls)
	}
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for cyber advisory pseudo-event, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesAnalysisBroadRegionRoundup(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "Breaking the Cycle: Building Wildfire Resilience Across Latin America - World Bank Blogs",
		Summary: "Breaking the Cycle: Building Wildfire Resilience Across Latin America World Bank Blogs",
		URL:     "https://news.google.com/rss/articles/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for analysis/broad-region roundup, got %+v", got)
	}
}

func TestKnownZoneHotspotCoordinates_SeaAndResidualRegions(t *testing.T) {
	for _, tc := range []struct {
		place string
		cc    string
	}{
		{place: "Bab el-Mandeb", cc: ""},
		{place: "Fergana Valley", cc: ""},
		{place: "North Kivu", cc: "CD"},
		{place: "English Channel", cc: ""},
		{place: "Strait of Gibraltar", cc: ""},
		{place: "Malacca Strait", cc: ""},
		{place: "Panama Canal", cc: "PA"},
	} {
		if _, _, ok := knownZoneHotspotCoordinates(tc.place, tc.cc); !ok {
			t.Fatalf("expected hotspot coordinates for %q", tc.place)
		}
	}
}

func TestInferAffectedZoneCandidate_SuppressesPlaceQualityGarbage(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		summary string
		wantNil bool
	}{
		{
			name:    "web shell campaign garbage",
			title:   "Ongoing web shell compromises target multiple industries",
			summary: "Researchers warn the campaign affected 16 countries but do not report a concrete outage.",
			wantNil: true,
		},
		{
			name:    "numeric pseudo place",
			title:   "Critical cyber exposure hits 16 countries",
			summary: "The campaign expands, but no specific facility outage or service disruption is confirmed.",
			wantNil: true,
		},
		{
			name:    "time phrase pseudo place",
			title:   "Violence in West Bank this week",
			summary: "A running weekly recap of incidents and political statements.",
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := &horizonMonitorFeedEvent{
				Title:       tc.title,
				Summary:     tc.summary,
				CountryCode: "IL",
				CountryName: "Israel",
				URL:         "https://thehackernews.com/example",
			}
			cls := horizonMonitorNewsClassification{Category: "cyber", Level: "high"}
			candidate := inferAffectedZoneCandidate(event, cls)
			if tc.wantNil && candidate != nil {
				t.Fatalf("expected candidate to be suppressed, got %+v", candidate)
			}
		})
	}
}

func TestNormalizeAffectedZonePlace_StripsTimeSuffixes(t *testing.T) {
	if got := normalizeAffectedZonePlace("West Bank this week", "Palestine"); got != "West Bank" {
		t.Fatalf("expected trailing time suffix to be stripped, got %q", got)
	}
}

func TestCountryCentroidFallback_NewCountries(t *testing.T) {
	for _, cc := range []string{"BD", "BH", "KP"} {
		lat, lon, ok := countryCentroid(cc)
		if !ok {
			t.Fatalf("expected centroid for %s", cc)
		}
		if lat == 0 && lon == 0 {
			t.Fatalf("unexpected zero centroid for %s", cc)
		}
	}
}

func TestInferZoneGeo_DoesNotTrustPublisherCountryForMilitaryConflict(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:       "US, Israel strikes hit oil depot in Tehran",
		Summary:     "France24 coverage of strikes in Tehran.",
		CountryCode: "FR",
		CountryName: "France",
		URL:         "https://www.france24.com/en/us-israel-hit-oil-depot-in-tehran",
	}
	geo, cc, _ := inferZoneGeo(event, "", strings.Join([]string{event.Title, event.Summary}, " "), "military_conflict")
	if geo != "global" || cc != "" {
		t.Fatalf("expected publisher country fallback to be ignored for military conflict, got geo=%q cc=%q", geo, cc)
	}
}

func TestInferZoneGeo_TrustsOfficialDisasterCountryFallback(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:       "M 4.9 - 157 km SSE of Vilyuchinsk, Russia",
		Summary:     "USGS earthquake event page.",
		CountryCode: "RU",
		CountryName: "Russia",
		URL:         "https://earthquake.usgs.gov/earthquakes/eventpage/us7000s2zf",
	}
	geo, cc, _ := inferZoneGeo(event, "", strings.Join([]string{event.Title, event.Summary}, " "), "earthquake_affected")
	if geo != "ru" || cc != "RU" {
		t.Fatalf("expected official disaster fallback to retain country, got geo=%q cc=%q", geo, cc)
	}
}

func TestInferAffectedZoneCandidate_SuppressesAdministrativeStrikePseudoConflict(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Ireland strikes new five-year drug pricing deals, speeding access to medicines - Euractiv",
		Summary:     "Drug pricing agreement accelerates access to medicines.",
		CountryCode: "IE",
		CountryName: "Ireland",
		URL:         "https://news.google.com/rss/articles/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for administrative strike/deal wording, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesCivilExplosionPseudoConflict(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Police in Norway investigate an explosion outside the US Embassy in Oslo - AP News",
		Summary:     "Police investigate the incident outside the embassy compound.",
		CountryCode: "NO",
		CountryName: "Norway",
		URL:         "https://news.google.com/rss/articles/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if cls.Category != "crime" {
		t.Fatalf("expected crime-style downgrade for civil explosion, got %+v", cls)
	}
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for civil explosion pseudo-conflict, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesAdministrativeConflictCommentary(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Iran: New Supreme Leader set to be announced",
		Summary:     "Political succession coverage and commentary.",
		CountryCode: "IR",
		CountryName: "Iran",
		URL:         "https://www.france24.com/en/iran-new-supreme-leader-set-to-be-announced",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for administrative/political succession commentary, got %+v", got)
	}
}

func TestExtractUSGSPlaceFromTitle(t *testing.T) {
	got := extractUSGSPlaceFromTitle("M 5.5 - 2 km SSE of Rodotópi, Greece")
	if got != "Rodotópi, Greece" {
		t.Fatalf("unexpected USGS place extraction: %q", got)
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
