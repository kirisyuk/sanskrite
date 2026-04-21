package discovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonsignallanding "github.com/igorkirisyuk/sanskrite/internal/horizon/signallanding"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
			name: "dockworkers strike is maritime disruption not protest",
			event: horizonMonitorFeedEvent{
				Category: "infrastructure",
				Title:    "Dockworkers strike shuts down Canada's Port of Vancouver",
				Summary:  "A dockworkers strike shut down Port of Vancouver operations, disrupting container flows and rail-linked cargo movements.",
			},
			want: "risk:maritime_hazard",
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

func TestHeuristicSignalSeedPaths_IncludeInternationalChokepoints(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:   "Houthis warn they will block shipping through Bab el-Mandeb",
		Summary: "Red Sea traffic faces new closure threats from Yemen-based militants.",
	}
	paths := heuristicSignalSeedPaths(e, "/loc/ye")
	joined := strings.Join(paths, "|")
	for _, want := range []string{"/loc/int/bab-el-mandeb", "/loc/int/red-sea"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected chokepoint seed path %q in %v", want, paths)
		}
	}
}

func TestSignalLandingZoneRefsPrefersSpecificZones(t *testing.T) {
	result := &horizonsignallanding.Result{
		Claims: []*horizonmodels.SignalImpactClaim{
			{AffectedZoneRefs: []string{"/zone/ae/military_conflict/united-arab-emirates"}},
		},
		Hits: []*horizonmodels.SignalLandingHit{
			{CanonicalRef: "/zone/ae/military_conflict/jebel-ali-port"},
			{CanonicalRef: "/zone/ae/military_conflict/dubai"},
		},
		Summaries: []*horizonmodels.SignalLandingSummary{
			{AffectedZoneRefs: []string{"/zone/ae/military_conflict/dubai"}},
		},
	}
	refs := signalLandingZoneRefs(result)
	if len(refs) < 3 {
		t.Fatalf("expected multiple zone refs, got %v", refs)
	}
	if refs[0] != "/zone/ae/military_conflict/jebel-ali-port" {
		t.Fatalf("expected most specific zone first, got %v", refs)
	}
}

func TestLandingZoneFragmentToCandidate(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath:  "/zone/ae/military_conflict/dubai",
		Class:           ah.FragmentClassGeo,
		Subtype:           "affected_zone",
		GeoSlug:        "ae",
		RadiusKm:       24,
		HasCoordinates: true,
		Labels:         map[string]string{"en": "Dubai (armed tension)"},
		Qualifiers: map[string]interface{}{
			"conditionType": "military_conflict",
			"geoPrecision":  "precise",
			"zoneType":      "military_conflict",
			"zoneSubType":   "armed_tension",
		},
	}
	candidate := landingZoneFragmentToCandidate(frag)
	if candidate == nil {
		t.Fatal("expected candidate from landing zone fragment")
	}
	if candidate.CanonicalPath != frag.CanonicalPath {
		t.Fatalf("expected canonical path %s, got %s", frag.CanonicalPath, candidate.CanonicalPath)
	}
	if candidate.CountryCode != "AE" {
		t.Fatalf("expected country AE, got %s", candidate.CountryCode)
	}
	if candidate.GeoPrecision != "precise" {
		t.Fatalf("expected precise geo, got %s", candidate.GeoPrecision)
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

func TestShouldSuppressAffectedZoneBySemanticDrift_LiveBlogExplainer(t *testing.T) {
	text := normalizeKeywordText("Why did US and Israel attack Iran and how long could the war last?")
	if !shouldSuppressAffectedZoneBySemanticDrift(text, &horizonMonitorFeedEvent{Title: "Why did US and Israel attack Iran and how long could the war last?"}, horizonMonitorNewsClassification{Category: "military"}) {
		t.Fatalf("expected explainer/liveblog question to be suppressed")
	}
}

func TestShouldSuppressAffectedZoneByEventProfile_ExerciseAndDrugInterdiction(t *testing.T) {
	exerciseText := normalizeKeywordText("USS Gerald R. Ford now in the Red Sea, USS George H.W. Bush wraps pre-deployment exercises")
	exerciseEvent := &horizonMonitorFeedEvent{
		Title: "USS Gerald R. Ford now in the Red Sea, USS George H.W. Bush wraps pre-deployment exercises",
		URL:   "https://www.defensenews.com/naval/2026/03/09/uss-gerald-r-ford-now-in-the-red-sea/",
	}
	exerciseCls := horizonMonitorNewsClassification{Category: "military", Level: "medium"}
	if !shouldSuppressAffectedZoneByEventProfile(exerciseText, exerciseEvent, exerciseCls, "military_conflict", wmHeuristics().ProfileArticle(exerciseText, affectedZoneSourceHost(exerciseEvent))) {
		t.Fatalf("expected exercise-only military movement to be suppressed")
	}

	drugText := normalizeKeywordText("SOUTHCOM Strike on Suspected Drug Boat Kills 6")
	drugEvent := &horizonMonitorFeedEvent{
		Title: "SOUTHCOM Strike on Suspected Drug Boat Kills 6",
		URL:   "https://www.defense.gov/News/2026/03/09/southcom-strike-on-suspected-drug-boat-kills-6/",
	}
	drugCls := horizonMonitorNewsClassification{Category: "military", Level: "high"}
	if !shouldSuppressAffectedZoneByEventProfile(drugText, drugEvent, drugCls, "military_conflict", wmHeuristics().ProfileArticle(drugText, affectedZoneSourceHost(drugEvent))) {
		t.Fatalf("expected drug interdiction military headline to be suppressed as affected zone")
	}
}

func TestRuleArticleAssessment_SuppressesAdministrativeAndExerciseCases(t *testing.T) {
	p := &HorizonMonitorPipeline{}
	exercise := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "defensenews.com",
		Title:      "U.S. Army Attack Helicopters Hold Maritime Attack Drills in the Pacific",
		Summary:    "Exercise-focused article without a concrete battlefield incident.",
	}, horizonMonitorNewsClassification{Category: "military", Level: "medium"})
	if exercise.ShouldCreateZone || exercise.ShouldOnlySignal || exercise.IsEvent {
		t.Fatalf("expected drills article to be suppressed, got %+v", exercise)
	}

	commentary := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "apnews.com",
		Title:      "South Africa’s president calls Trump’s policy to offer refuge to white Afrikaners racist",
		Summary:    "Political commentary and reaction coverage.",
	}, horizonMonitorNewsClassification{Category: "politics", Level: "medium"})
	if commentary.ShouldCreateZone || commentary.ShouldOnlySignal || commentary.IsEvent {
		t.Fatalf("expected political commentary to be suppressed, got %+v", commentary)
	}

	liveBlog := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "theguardian.com",
		Title:      "White House live updates: Trump and DHS latest politics updates",
		Summary:    "Live blog of US politics with latest updates.",
	}, horizonMonitorNewsClassification{Category: "diplomatic", Level: "medium"})
	if liveBlog.ShouldCreateZone || liveBlog.ShouldOnlySignal || liveBlog.IsEvent {
		t.Fatalf("expected live blog commentary to be suppressed, got %+v", liveBlog)
	}

	deployment := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "defensenews.com",
		Title:      "Turkey sends six F-16 fighters to Northern Cyprus amid Iran missile threats",
		Summary:    "The aircraft were sent as part of a phased approach to enhancing security.",
	}, horizonMonitorNewsClassification{Category: "military", Level: "high"})
	if deployment.ShouldCreateZone || deployment.ShouldOnlySignal || deployment.IsEvent {
		t.Fatalf("expected force posture deployment to be suppressed, got %+v", deployment)
	}

	cyberCampaign := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "thehackernews.com",
		Title:      "SloppyLemming Targets Pakistan and Bangladesh Governments Using Dual Malware Chains",
		Summary:    "Targets government entities using dual malware chains.",
	}, horizonMonitorNewsClassification{Category: "cyber", Level: "high"})
	if cyberCampaign.ShouldCreateZone || cyberCampaign.ShouldOnlySignal || cyberCampaign.IsEvent {
		t.Fatalf("expected cyber campaign without outage to be suppressed, got %+v", cyberCampaign)
	}
}

func TestRuleArticleAssessment_ConcreteOperationalIncidentOverridesAnalysisLikeFraming(t *testing.T) {
	p := &HorizonMonitorPipeline{}
	assessment := p.ruleArticleAssessment(discoverynlp.DocumentInput{
		SourceHost: "reuters.com",
		Title:      "UAE's Fujairah stops some oil loading operations after drone attack",
		Summary:    "Operational disruption at the export hub followed a reported drone attack.",
	}, horizonMonitorNewsClassification{Category: "infrastructure", Level: "high"})
	if !assessment.IsEvent || !assessment.ShouldCreateZone {
		t.Fatalf("expected concrete operational incident to stay eventful, got %+v", assessment)
	}
	if assessment.RejectionReason != "" || assessment.EventClass != "real_event" {
		t.Fatalf("expected concrete incident override to clear rejection, got %+v", assessment)
	}
}

func TestInferZonePlaceName_DoesNotFallbackToCountryForExerciseOrRoundup(t *testing.T) {
	exerciseEvent := &horizonMonitorFeedEvent{
		Title:       "U.S. Army Attack Helicopters Hold Maritime Attack Drills in the Pacific",
		Summary:     "Exercise-focused article without a concrete battlefield incident.",
		CountryCode: "PS",
		CountryName: "Palestine",
	}
	if place := inferZonePlaceName(exerciseEvent, normalizeKeywordText(strings.Join([]string{exerciseEvent.Title, exerciseEvent.Summary}, " ")), "military_conflict"); place != "" {
		t.Fatalf("expected no country fallback place for exercise, got %q", place)
	}

	roundupEvent := &horizonMonitorFeedEvent{
		Title:       "Turkey sends six F-16 fighters to Northern Cyprus amid Iran missile threats",
		Summary:     "Regional military deployments and threats across multiple countries.",
		CountryCode: "FR",
		CountryName: "France",
	}
	if place := inferZonePlaceName(roundupEvent, normalizeKeywordText(strings.Join([]string{roundupEvent.Title, roundupEvent.Summary}, " ")), "military_conflict"); !strings.EqualFold(place, "Northern Cyprus") {
		t.Fatalf("expected extracted place Northern Cyprus, got %q", place)
	}

	prefixEvent := &horizonMonitorFeedEvent{
		Title:       "Lebanon: 300,000 already displaced as Israel issues mass evacuation orders | NRC - ReliefWeb",
		CountryCode: "IL",
		CountryName: "Israel",
	}
	if place := inferZonePlaceName(prefixEvent, normalizeKeywordText(prefixEvent.Title), "humanitarian_crisis"); !strings.EqualFold(place, "Lebanon") {
		t.Fatalf("expected title-prefix place Lebanon, got %q", place)
	}
}

func TestInferZonePlaceName_PrefersTrustedLLMGeoHint(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:   "Cyberattack forces major Australian port operator offline in Sydney",
		Summary: "Port operations remain suspended after ransomware attack disrupted container handling.",
		Raw: map[string]interface{}{
			"llmArticleAdjudication": map[string]interface{}{
				"isEvent":         true,
				"eventConfidence": 0.96,
				"place":           "Sydney",
				"countryCode":     "AU",
				"geoConfidence":   0.95,
			},
		},
	}
	if place := inferZonePlaceName(event, normalizeKeywordText(strings.Join([]string{event.Title, event.Summary}, " ")), "cyber_disruption"); place != "Sydney" {
		t.Fatalf("expected trusted llm place hint, got %q", place)
	}
}

func TestLLMAffectedZoneConditionOverride(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Raw: map[string]interface{}{
			"llmArticleAdjudication": map[string]interface{}{
				"isEvent":         true,
				"eventConfidence": 0.94,
				"conditionType":   "disease_outbreak",
			},
		},
	}
	conditionType, zoneType, zoneSubType, score, ok := llmAffectedZoneConditionOverride(event)
	if !ok {
		t.Fatal("expected llm condition override")
	}
	if conditionType != "disease_outbreak" || zoneType != "humanitarian_crisis" || zoneSubType != "epidemic" {
		t.Fatalf("unexpected llm override: %s %s %s", conditionType, zoneType, zoneSubType)
	}
	if score < 0.94 {
		t.Fatalf("expected score to track llm confidence, got %.2f", score)
	}
}

func TestApplyNLPResultToEvent_ShadowEntitiesDoNotAffectRuntimeAnchors(t *testing.T) {
	p := &HorizonMonitorPipeline{entityExtractMode: discoverynlp.ModeShadow}
	event := &horizonMonitorFeedEvent{Raw: map[string]interface{}{}}
	entities := &discoverynlp.EntityExtractionResult{
		Entities: []discoverynlp.ExtractedEntity{
			{Surface: "Ras Laffan Industrial City", EntityType: "facility", Role: "facility", Confidence: 0.88},
		},
	}

	p.applyNLPResultToEvent(event, nil, entities, nil)

	if _, ok := event.Raw["nlpEntities"]; ok {
		t.Fatalf("shadow mode must not write nlpEntities into runtime event raw")
	}
	if _, ok := event.Raw["nlpEntitiesShadow"]; !ok {
		t.Fatalf("shadow mode should keep extracted entities under nlpEntitiesShadow")
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

func TestGeocodeAffectedZoneCoordinates_UsesGeocoderFallback(t *testing.T) {
	pipeline := (&HorizonMonitorPipeline{}).WithGeocodingClient(mockZoneGeocoder{
		byQuery: map[string]ontology.GeocodingResult{
			"Al Ahmadi, KW": {Lat: 29.0769, Lon: 48.0839},
		},
	})
	lat, lon, precision, ok := geocodeAffectedZoneCoordinates(context.Background(), pipeline, "Al Ahmadi", "KW", "none")
	if !ok {
		t.Fatalf("expected geocode fallback to resolve coordinates")
	}
	if !horizonmodels.IsRealGeoCoordinate(lat, lon) {
		t.Fatalf("expected valid coordinates, got %.4f, %.4f", lat, lon)
	}
	if precision != "precise" {
		t.Fatalf("expected precise precision, got %q", precision)
	}
}

type mockZoneGeocoder struct {
	byQuery map[string]ontology.GeocodingResult
}

func (m mockZoneGeocoder) Geocode(_ context.Context, address string) (*ontology.GeocodingResult, error) {
	if result, ok := m.byQuery[address]; ok {
		copy := result
		return &copy, nil
	}
	return nil, nil
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

func TestLooksLikeConflictDrivenOperationalDisruption_ExcludesLaborStrike(t *testing.T) {
	text := "Dockworkers strike shuts down Canada's Port of Vancouver, disrupting container cargo operations"
	if looksLikeConflictDrivenOperationalDisruption(text) {
		t.Fatal("expected labor strike outage to not be treated as conflict-driven operational disruption")
	}
}

func TestLooksLikeLaborStrikeOperationalDisruption(t *testing.T) {
	text := "Dockworkers strike shuts down Canada's Port of Vancouver, disrupting container cargo operations"
	if !looksLikeLaborStrikeOperationalDisruption(text) {
		t.Fatal("expected dockworker port shutdown to be recognized as labor-driven operational disruption")
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
	if !shouldAttemptAffectedZoneEntityAnchor("Bandar Abbas refinery", "Iran", "precise", true, strong) {
		t.Fatal("expected precise operational place to still trigger entity-anchor lookup")
	}
}

func TestShouldAttemptAffectedZoneEntityAnchor_ForOperationalPlaceEvenWhenGeoRecognized(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Qatar remains offline due to overnight US attack on Ras Laffan",
		Summary:     "Qatar's LNG and helium infrastructure remains offline after an overnight attack on Ras Laffan.",
		CountryCode: "QA",
		CountryName: "Qatar",
	}
	if !shouldAttemptAffectedZoneEntityAnchor("Ras Laffan", "Qatar", "precise", true, e) {
		t.Fatal("expected operational site label to keep entity-anchor lookup enabled")
	}
}

func TestBuildAffectedZoneAnchorFromFragment_AllowsPlaceholderFacilityWithCoordinatesAndStrongMatch(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Qatar remains offline due to overnight US attack on Ras Laffan",
		Summary:     "Qatar's LNG and helium infrastructure remains offline after an overnight attack on Ras Laffan.",
		CountryCode: "QA",
		CountryName: "Qatar",
	}
	frag := &horizonmodels.HorizonFragment{
		ID:            "fac-ras-laffan",
		Class:         FragmentTypeFacility,
		CanonicalPath: "/fac/na/ras-laffan",
		Labels:        map[string]string{"en": "Ras Laffan"},
		Coordinates:   &horizonmodels.Coordinates{Lat: 25.8976567, Lon: 51.5331527},
	}
	anchor, score := buildAffectedZoneAnchorFromFragment(t.Context(), "Ras Laffan", "QA", e, frag, 0.96, nil)
	if score <= 0 {
		t.Fatalf("expected positive score, got %f", score)
	}
	if !anchor.HasCoordinates {
		t.Fatal("expected placeholder facility with real coordinates to be accepted")
	}
	if anchor.FragmentPath != "/fac/na/ras-laffan" {
		t.Fatalf("unexpected fragment path: %s", anchor.FragmentPath)
	}
	if anchor.CountryCode != "QA" {
		t.Fatalf("expected inferred country code QA, got %q", anchor.CountryCode)
	}
}

func TestAffectedZoneExactAnchorFragmentScore_PrefersOperationalFacilityVariant(t *testing.T) {
	plain := &horizonmodels.HorizonFragment{
		ID:            "fac-ras-laffan",
		Class:         FragmentTypeFacility,
		CanonicalPath: "/fac/na/ras-laffan",
		Labels:        map[string]string{"en": "Ras Laffan"},
		Coordinates:   &horizonmodels.Coordinates{Lat: 25.8976567, Lon: 51.5331527},
	}
	richer := &horizonmodels.HorizonFragment{
		ID:            "fac-ras-laffan-industrial-city",
		Class:         FragmentTypeFacility,
		CanonicalPath: "/fac/na/ras-laffan-industrial-city",
		Labels:        map[string]string{"en": "Ras Laffan Industrial City"},
		Coordinates:   &horizonmodels.Coordinates{Lat: 25.8976567, Lon: 51.5331527},
	}
	if got, want := affectedZoneExactAnchorFragmentScore("Ras Laffan", richer), affectedZoneExactAnchorFragmentScore("Ras Laffan", plain); got <= want {
		t.Fatalf("expected operational facility variant to score higher: richer=%f plain=%f", got, want)
	}
}

func TestAffectedZoneCountryFromFragment_IgnoresPlaceholderNAForFacility(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		ID:            "fac-ras-laffan",
		Class:         FragmentTypeFacility,
		CanonicalPath: "/fac/na/ras-laffan",
	}
	if got := affectedZoneCountryFromFragment(frag); got != "" {
		t.Fatalf("expected empty country for placeholder facility path, got %q", got)
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

func TestExtractPlaceFromTitle_PreservesPortOfLocation(t *testing.T) {
	title := "Dockworkers strike shuts down Canada's Port of Vancouver"
	if got := extractPlaceFromTitle(title); got != "Port of Vancouver" {
		t.Fatalf("expected Port of Vancouver, got %q", got)
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
	if strings.EqualFold(geo, "fr") || strings.EqualFold(cc, "FR") {
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

func TestInferAffectedZoneCandidate_SuppressesLegalCommercialDispute(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Roman Abramovich ready to fight UK government over proceeds from £2.5bn Chelsea sale",
		Summary:     "Legal and commercial dispute over sanctions-era sale proceeds.",
		CountryCode: "GB",
		CountryName: "United Kingdom",
		URL:         "https://www.theguardian.com/football/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for legal/commercial dispute, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesRetrospectiveConflictNarrative(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "The final voyage of the Iranian warship sunk by the US",
		Summary:     "A retrospective look at a warship sunk near Sri Lanka, bringing conflict close to India.",
		CountryCode: "IN",
		CountryName: "India",
		URL:         "https://www.bbc.com/news/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no affected zone for retrospective conflict narrative, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_SuppressesForcePostureHormuzCommentary(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Macron says France, allies preparing defensive mission to reopen Strait of Hormuz",
		Summary:     "France and allies are preparing a defensive mission to help reopen the Strait of Hormuz after shipping disruptions.",
		CountryCode: "FR",
		CountryName: "France",
		URL:         "https://www.france24.com/en/example",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if got := inferAffectedZoneCandidate(e, cls); got != nil {
		t.Fatalf("expected no new affected zone for force-posture Hormuz commentary, got %+v", got)
	}
}

func TestInferAffectedZoneCandidate_CreatesBabElMandebThreatZone(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Houthis warn they will block shipping through Bab el-Mandeb",
		Summary:     "Red Sea maritime traffic faces closure threats as militants target vessels near the chokepoint.",
		CountryCode: "YE",
		CountryName: "Yemen",
		URL:         "https://www.reuters.com/world/middle-east/example-bab-el-mandeb",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	got := inferAffectedZoneCandidate(e, cls)
	if got == nil {
		t.Fatal("expected affected zone candidate")
	}
	if got.CanonicalPath != "/zone/sea/military_conflict/bab-el-mandeb" {
		t.Fatalf("unexpected canonical path: %s", got.CanonicalPath)
	}
	if !got.HasCoordinates || got.GeoToken != "sea" {
		t.Fatalf("expected map-ready sea chokepoint candidate, got %+v", got)
	}
	if got.WarmOnly {
		t.Fatalf("credible chokepoint threat should create zone, got warmOnly candidate %+v", got)
	}
}

func TestInferAffectedZoneCandidate_PrefersChokepointOverCountryFallback(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Middle East crisis live: Mojtaba Khamanei says strait of Hormuz will remain closed in first statement as supreme leader",
		Summary:     "Iran escalates attacks on infrastructure and transport across the Gulf as tanker traffic faces closure risk.",
		CountryCode: "IN",
		CountryName: "India",
		URL:         "https://www.theguardian.com/world/example-hormuz-live",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	got := inferAffectedZoneCandidate(e, cls)
	if got == nil {
		t.Fatal("expected affected zone candidate")
	}
	if got.GeoToken != "sea" {
		t.Fatalf("expected sea geo token, got %+v", got)
	}
	if got.CanonicalPath != "/zone/sea/military_conflict/strait-of-hormuz" {
		t.Fatalf("unexpected canonical path: %s", got.CanonicalPath)
	}
}

func TestCollectAffectedZoneLinkedFragmentPaths_UsesLocationAnchorAndChokepointSeeds(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:        "Houthis warn they will block shipping through Bab el-Mandeb",
		Summary:      "Red Sea maritime traffic faces closure threats near the chokepoint.",
		LocationPath: "/loc/int/bab-el-mandeb",
	}
	anchor := &affectedZoneEntityAnchor{FragmentPath: "/fac/ye/port/hodeidah-port"}
	got := collectAffectedZoneLinkedFragmentPaths(e, anchor)
	for _, want := range []string{"/loc/int/bab-el-mandeb", "/loc/int/red-sea", "/fac/ye/port/hodeidah-port"} {
		if !containsString(got, want) {
			t.Fatalf("expected linked paths to contain %q, got %v", want, got)
		}
	}
}

func TestCollectAffectedZoneLinkedFragmentPaths_KeepsProductAnchor(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:        "Explainer on energy markets",
		LocationPath: "/domain/v1/supply-chain/geo/ae",
	}
	anchor := &affectedZoneEntityAnchor{FragmentPath: "/product/global/crude-oil"}
	got := collectAffectedZoneLinkedFragmentPaths(e, anchor)
	if len(got) != 1 || got[0] != "/product/global/crude-oil" {
		t.Fatalf("expected product anchor to be retained, got %v", got)
	}
}

func TestCollectAffectedZoneLinkedFragmentPaths_DropsBroadCountryGeoWhenSpecificGeoPresent(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:        "UAE's Fujairah stops some oil loading operations after drone attack",
		LocationPath: "/loc/ae",
	}
	anchor := &affectedZoneEntityAnchor{FragmentPath: "/loc/ae/na/fujairah"}
	got := collectAffectedZoneLinkedFragmentPaths(e, anchor)
	if containsString(got, "/loc/ae") {
		t.Fatalf("expected broad country path to be pruned, got %v", got)
	}
	if !containsString(got, "/loc/ae/na/fujairah") {
		t.Fatalf("expected precise geo path to be retained, got %v", got)
	}
}

func TestLooksLikeAffectedZoneLinkPath_AllowsProduct(t *testing.T) {
	if !looksLikeAffectedZoneLinkPath("/product/finished/mechanical/assembly/helium") {
		t.Fatal("expected product path to be bindable")
	}
}

func TestInferAffectedZoneCandidate_SuppressesExplainerQuestion(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:       "Why Does The Port Of Fujairah Matter To The Oil Market?",
		Summary:     "An explainer on the port's role in regional oil flows and pricing.",
		CountryCode: "AE",
		CountryName: "United Arab Emirates",
		URL:         "https://www.example.com/explainer-fujairah",
	}
	cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")), e)
	if candidate := inferAffectedZoneCandidate(e, cls); candidate != nil {
		t.Fatalf("expected explainer question to be suppressed, got %+v", candidate)
	}
}

func TestShouldAttemptAffectedZoneEntityAnchor_ForSpecificIndustrialSiteOnCountryFallback(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		Title:        "Qatar remains offline due to overnight US attack on Ras Laffan",
		Summary:      "Qatar's LNG and helium infrastructure remains offline after an overnight attack on Ras Laffan.",
		CountryCode:  "QA",
		LocationPath: "/loc/qa",
	}
	if !shouldAttemptAffectedZoneEntityAnchor("Ras Laffan", "Qatar", "none", false, e) {
		t.Fatal("expected entity anchor lookup for specific industrial site on country fallback")
	}
}

func TestFilterRedundantAffectedZoneTargets_DropsBroaderGeoAncestor(t *testing.T) {
	targets := []*horizonmodels.HorizonFragment{
		{ID: "country", Class: FragmentTypeGeoRegion, CanonicalPath: "/loc/ae"},
		{ID: "city", Class: FragmentTypeGeoRegion, CanonicalPath: "/loc/ae/na/fujairah"},
		{ID: "org", Class: FragmentTypeOrganization, CanonicalPath: "/org/bm/frontline"},
	}
	got := filterRedundantAffectedZoneTargets(targets, &horizonmodels.HorizonFragment{ID: "city", Class: FragmentTypeGeoRegion, CanonicalPath: "/loc/ae/na/fujairah"})
	if len(got) != 2 {
		t.Fatalf("expected 2 targets after pruning, got %d: %#v", len(got), got)
	}
	for _, frag := range got {
		if frag != nil && frag.CanonicalPath == "/loc/ae" {
			t.Fatalf("expected broader country geo target to be pruned, got %#v", got)
		}
	}
}

func TestShouldEscalateArticleToLLM_AmbiguousViolentIncident(t *testing.T) {
	doc := discoverynlp.DocumentInput{
		SourceHost: "apnews.com",
		Title:      "At least six people killed and five injured in bus fire in Switzerland",
		Summary:    "Authorities are investigating the overnight blaze after a deadly highway incident.",
	}
	cls := horizonMonitorNewsClassification{Category: "military", Level: "high", Confidence: 0.92}
	base := &discoverynlp.ArticleAssessment{
		IsEvent:             true,
		EventClass:          "real_event",
		EventConfidence:     0.91,
		ShouldCreateZone:    true,
		ShouldWarmExisting:  false,
		ShouldOnlySignal:    false,
		ConcreteOutageScore: 0.80,
	}
	if !shouldEscalateArticleToLLM(doc, cls, base) {
		t.Fatal("expected ambiguous violent incident to escalate to LLM adjudication")
	}
}

func TestMaintainStickyAffectedZoneActivity_HoldsOperationalOutage(t *testing.T) {
	now := time.Now().UTC()
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/qa/infrastructure_disruption/ras-laffan",
		Class:          ah.FragmentClassGeo,
		Subtype:          "affected_zone",
		Qualifiers: map[string]interface{}{
			"requiresExplicitRecovery": true,
			"recoveryStatus":           affectedZoneRecoveryStatusActive,
		},
	}
	st := affectedZoneState{
		Score:         9,
		Hits24h:       0,
		LastMatchedAt: now.Add(-2 * time.Hour),
		LastUpdatedAt: now.Add(-2 * time.Hour),
	}
	if !maintainStickyAffectedZoneActivity(frag, &st, now) {
		t.Fatal("expected sticky outage to be maintained")
	}
	if st.Score < 42 {
		t.Fatalf("expected sticky outage floor, got %.2f", st.Score)
	}
}

func TestApplyStickyRecoveryObservation_ClearsStickyOutage(t *testing.T) {
	now := time.Now().UTC()
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/qa/infrastructure_disruption/ras-laffan",
		Class:          ah.FragmentClassGeo,
		Subtype:          "affected_zone",
		Qualifiers: map[string]interface{}{
			"requiresExplicitRecovery": true,
			"recoveryStatus":           affectedZoneRecoveryStatusActive,
		},
		State: map[string]horizonmodels.MetricValue{
			zoneMetricHypeScore:   {Value: 78},
			zoneMetricHits24h:     {Value: 12},
			zoneMetricLastMatchTS: {Value: float64(now.Add(-1 * time.Hour).Unix())},
		},
		Metadata: horizonmodels.FragmentMetadata{LastUpdatedAt: now.Add(-1 * time.Hour)},
	}
	event := &horizonMonitorFeedEvent{
		Title:      "Qatar LNG loading operations resumed after disruption at Ras Laffan",
		Summary:    "Loading operations resumed and exports restarted after the outage.",
		OccurredAt: now,
	}
	if !applyStickyRecoveryObservation(frag, event, now) {
		t.Fatal("expected recovery observation to apply")
	}
	if got := affectedZoneRecoveryStatus(frag); got != affectedZoneRecoveryStatusRestored {
		t.Fatalf("expected restored status, got %q", got)
	}
	st := parseAffectedZoneState(frag)
	if st.Score > 18.1 {
		t.Fatalf("expected recovered zone score to cool, got %.2f", st.Score)
	}
}

func TestHistoricalConcreteIncidentSignalAndZoneInference(t *testing.T) {
	cases := []struct {
		name          string
		event         *horizonMonitorFeedEvent
		wantPathPart  string
		wantGeo       string
		wantPrecision string
		wantCondition string
		wantAxis      string
	}{
		{
			name: "fujairah drone attack",
			event: &horizonMonitorFeedEvent{
				Title:       "UAE's Fujairah stops some oil loading operations after drone attack",
				Summary:     "Operations were halted at Fujairah after the reported drone attack.",
				CountryCode: "AE",
				CountryName: "United Arab Emirates",
				URL:         "https://www.reuters.com/world/middle-east/example-fujairah",
				Severity:    "high",
			},
			wantPathPart:  "/fujairah",
			wantGeo:       "ae",
			wantPrecision: "precise",
			wantCondition: "military_conflict",
			wantAxis:      horizonmodels.AxisPhysicalFlowSupply,
		},
		{
			name: "ras laffan outage",
			event: &horizonMonitorFeedEvent{
				Title:          "Qatar remains offline due to overnight US attack on Ras Laffan",
				Summary:        "Qatar's LNG and helium infrastructure remains offline after an overnight attack on Ras Laffan, extending disruption to energy and industrial gas supply chains.",
				CountryCode:    "QA",
				CountryName:    "Qatar",
				URL:            "https://www.gasworld.com/story/qatar-remains-offline-due-to-overnight-us-attack-on-ras-laffan/",
				Severity:       "high",
				Lat:            25.897657,
				Lon:            51.533153,
				HasCoordinates: true,
			},
			wantPathPart:  "/ras-laffan",
			wantGeo:       "qa",
			wantPrecision: "precise",
			wantAxis:      horizonmodels.AxisPhysicalFlowSupply,
		},
		{
			name: "hormuz gps jamming",
			event: &horizonMonitorFeedEvent{
				Title:    "Critical Threat Persists in Hormuz as Attacks and GPS Jamming Shake Shipping",
				Summary:  "Shipping disruptions and navigation interference continue in the Strait of Hormuz.",
				URL:      "https://www.reuters.com/world/middle-east/example-hormuz",
				Severity: "high",
			},
			wantPathPart:  "/strait-of-hormuz",
			wantGeo:       "sea",
			wantPrecision: "precise",
			wantCondition: "military_conflict",
			wantAxis:      horizonmodels.AxisPhysicalFlowSupply,
		},
		{
			name: "salalah port drone strike",
			event: &horizonMonitorFeedEvent{
				Title:       "Oman works to contain fire at Salalah port after drones strike",
				Summary:     "Emergency crews responded after drones struck Salalah port.",
				CountryCode: "OM",
				CountryName: "Oman",
				URL:         "https://www.reuters.com/world/middle-east/example-salalah",
				Severity:    "high",
			},
			wantPathPart:  "/salalah-port",
			wantGeo:       "om",
			wantPrecision: "precise",
			wantCondition: "military_conflict",
		},
		{
			name: "kharg island strike",
			event: &horizonMonitorFeedEvent{
				Title:       "The US hit Iran's Kharg Island, key to its economy and security",
				Summary:     "Kharg Island was hit in a strike on a strategic Iranian oil export site.",
				CountryCode: "IR",
				CountryName: "Iran",
				URL:         "https://www.apnews.com/example-kharg",
				Severity:    "high",
			},
			wantPathPart:  "/kharg-island",
			wantGeo:       "ir",
			wantPrecision: "precise",
			wantCondition: "military_conflict",
			wantAxis:      horizonmodels.AxisPhysicalFlowSupply,
		},
	}

	p := &HorizonMonitorPipeline{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{tc.event.Title, tc.event.Summary}, " ")), tc.event)
			doc := discoverynlp.DocumentInput{
				SourceHost: affectedZoneSourceHost(tc.event),
				Title:      tc.event.Title,
				Summary:    tc.event.Summary,
			}
			assessment := p.ruleArticleAssessment(doc, cls)
			if !assessment.IsEvent || !assessment.ShouldCreateZone {
				t.Fatalf("expected event gate to allow concrete incident, got %+v", assessment)
			}

			candidate := inferAffectedZoneCandidate(tc.event, cls)
			if candidate == nil {
				text := normalizeKeywordText(strings.Join([]string{tc.event.Title, tc.event.Summary, cls.Category, cls.Level}, " "))
				place := inferZonePlaceName(tc.event, text, "")
				geoToken, countryCode, countryName := inferZoneGeo(tc.event, place, text, "")
				cond, zoneType, sub, score := inferZoneConditionAndSubtypeScored(text, cls)
				t.Fatalf("expected affected zone candidate; cls=%+v place=%q geo=%q country=%q/%q concrete=%t cond=%q zoneType=%q sub=%q score=%.2f text=%q", cls, place, geoToken, countryCode, countryName, looksLikeConcreteOperationalIncident(text, cls), cond, zoneType, sub, score, text)
			}
			if candidate.WarmOnly {
				t.Fatalf("expected concrete incident to materialize, got warm-only candidate %+v", candidate)
			}
			if !strings.Contains(candidate.CanonicalPath, tc.wantPathPart) {
				t.Fatalf("expected canonical path to contain %q, got %q", tc.wantPathPart, candidate.CanonicalPath)
			}
			if candidate.GeoToken != tc.wantGeo {
				t.Fatalf("expected geo token %q, got %q", tc.wantGeo, candidate.GeoToken)
			}
			if tc.wantCondition != "" && candidate.ConditionType != tc.wantCondition {
				t.Fatalf("expected condition type %q, got %q", tc.wantCondition, candidate.ConditionType)
			}
			if !candidate.HasCoordinates || candidate.GeoPrecision != tc.wantPrecision {
				t.Fatalf("expected map-ready precise geometry, got hasCoordinates=%v precision=%q", candidate.HasCoordinates, candidate.GeoPrecision)
			}

			tc.event.SignalType = inferHorizonMonitorSignalType("", tc.event, cls)
			locationPath := "/loc/" + candidate.GeoToken
			eventFact := &coremodels.OntologyFact{
				BaseOntologyFields: coremodels.BaseOntologyFields{
					ID:           "fact-" + strings.ReplaceAll(tc.name, " ", "-"),
					LocationPath: &locationPath,
				},
				Confidence: 0.84,
				SourceIDs:  []string{"src-1"},
			}
			if signal := p.buildSignalDescriptor(eventFact, tc.event); signal == nil {
				t.Fatal("expected signal descriptor")
			} else if signal.ID == "" || signal.FactID == "" || tc.event.SignalType == "risk:temporal_anomaly" {
				t.Fatalf("unexpected signal state: signal=%+v inferred=%q", signal, tc.event.SignalType)
			} else if tc.wantAxis != "" && signal.Axis != tc.wantAxis {
				t.Fatalf("expected signal axis %q, got %q", tc.wantAxis, signal.Axis)
			}
		})
	}
}

func TestSelectSemanticAffectedZoneCanonicalPath_DoesNotMergeDifferentChokepoints(t *testing.T) {
	candidate := &affectedZoneCandidate{
		CanonicalPath: "/zone/sea/military_conflict/bab-el-mandeb",
		Label:         "Bab el-Mandeb (armed tension)",
		ConditionType: "military_conflict",
		GeoToken:      "sea",
	}
	results := []ai.SearchResult{
		{
			RelevanceScore: 0.99,
			Document: ai.SearchDocument{
				StructData: map[string]interface{}{"canonicalPath": "/zone/sea/military_conflict/strait-of-hormuz"},
			},
		},
		{
			RelevanceScore: 0.91,
			Document: ai.SearchDocument{
				StructData: map[string]interface{}{"canonicalPath": "/zone/sea/military_conflict/bab-el-mandeb"},
			},
		},
	}
	gotPath, _ := selectSemanticAffectedZoneCanonicalPath(candidate, results)
	if gotPath != "/zone/sea/military_conflict/bab-el-mandeb" {
		t.Fatalf("expected Bab el-Mandeb path, got %q", gotPath)
	}
}

func TestInferZoneConditionAndSubtypeScored_LaborStrikePortMapsToInfrastructure(t *testing.T) {
	cond, zoneType, sub, _ := inferZoneConditionAndSubtypeScored(
		normalizeKeywordText("Dockworkers strike shuts down Canada's Port of Vancouver, disrupting container cargo operations"),
		horizonMonitorNewsClassification{Category: "infrastructure", Level: "high"},
	)
	if cond != "infrastructure_disruption" || zoneType != "infrastructure_disruption" || sub != "systems_disruption" {
		t.Fatalf("expected infrastructure disruption, got cond=%s zoneType=%s sub=%s", cond, zoneType, sub)
	}
}

func TestShouldPreferSpecificAffectedZoneCreation_ReplacesGenericOperationalSlug(t *testing.T) {
	candidate := &affectedZoneCandidate{
		CanonicalPath:        "/zone/ca/infrastructure_disruption/port-of-vancouver",
		Label:                "Port of Vancouver",
		GeoPrecision:         "entity_anchor",
		CountryCode:          "CA",
		CountryName:          "Canada",
		GeoConfidence:        0.82,
		ResolutionConfidence: 0.84,
	}
	existing := &horizonmodels.HorizonFragment{
		ID:            "zone-port",
		Class:          ah.FragmentClassGeo,
		Subtype:          "affected_zone",
		CanonicalPath: "/zone/ca/infrastructure_disruption/port",
		Qualifiers:    map[string]interface{}{"geoPrecision": "precise"},
	}
	if !shouldPreferSpecificAffectedZoneCreation(candidate, existing) {
		t.Fatal("expected specific port-of-vancouver zone to replace generic /port zone")
	}
}

func TestExtractPossessiveOperationalPlaceFromTitle(t *testing.T) {
	cases := map[string]string{
		"UAE's Fujairah stops some oil loading operations after drone attack": "Fujairah",
		"The US hit Iran's Kharg Island, key to its economy and security":     "Kharg Island",
		"Oman works to contain fire at Salalah port after drones strike":      "",
		"Britain's policy on Iran draws criticism":                            "",
	}
	for title, want := range cases {
		if got := extractPossessiveOperationalPlaceFromTitle(title); got != want {
			t.Fatalf("title=%q: want %q got %q", title, want, got)
		}
	}
}

func TestShouldPreferSpecificAffectedZoneCreation(t *testing.T) {
	candidate := &affectedZoneCandidate{
		CanonicalPath:        "/zone/ae/infrastructure_disruption/fujairah",
		Label:                "Fujairah (systems disruption)",
		ConditionType:        "infrastructure_disruption",
		CountryCode:          "AE",
		CountryName:          "United Arab Emirates",
		GeoPrecision:         "precise",
		GeoConfidence:        0.84,
		ResolutionConfidence: 0.88,
	}
	existing := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/ae/infrastructure_disruption/united-arab-emirates",
		Class:          ah.FragmentClassGeo,
		Subtype:          "affected_zone",
		Status:        string(horizonmodels.FragmentStatusActive),
		Qualifiers: map[string]interface{}{
			"geoPrecision": "approximate_country",
		},
		State: map[string]horizonmodels.MetricValue{
			zoneMetricLastMatchTS: {Value: float64(time.Now().Add(-72 * time.Hour).Unix())},
		},
	}
	if !shouldPreferSpecificAffectedZoneCreation(candidate, existing) {
		t.Fatal("expected precise operational candidate to bypass coarse country-level zone")
	}
}

func TestAffectedZoneBoost_ConcreteMilitaryIncidentGetsHotStart(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:      "Two killed, 12 injured after projectile fell on residential location in Saudi Arabia",
		Summary:    "Projectile strike hit a residential location in Saudi Arabia.",
		Severity:   "high",
		OccurredAt: time.Now().UTC(),
	}
	cls := horizonMonitorNewsClassification{Category: "conflict", Level: "high", Confidence: 0.9}
	candidate := &affectedZoneCandidate{
		ConditionType:        "military_conflict",
		Label:                "Saudi Arabia (kinetic operations)",
		EventConfidence:      0.92,
		GeoConfidence:        0.44,
		ResolutionConfidence: 0.71,
	}
	if got := affectedZoneBoost(event, cls, candidate); got < 34 {
		t.Fatalf("expected concrete military incident to get strong initial boost, got %.2f", got)
	}
}

func TestBuildEventFromDocument_SanitizesInvalidUTF8(t *testing.T) {
	p := &HorizonMonitorPipeline{}
	doc := prov.NormalizedDocument{
		ExternalID: "evt-\xff",
		Title:      "Kharg \xffIsland hit",
		FullText:   "Fire at port \xff after strike",
		URL:        "https://example.com/\xff",
	}
	event := p.buildEventFromDocument("src-\xff", "intel_rss", "military", "", "https://feed.example/\xff", doc)
	if event == nil {
		t.Fatal("expected event")
	}
	for _, v := range []string{event.SourceID, event.ExternalID, event.Title, event.Summary, event.URL} {
		if strings.ContainsRune(v, '\xff') {
			t.Fatalf("expected invalid UTF-8 bytes to be removed, got %q", v)
		}
	}
}

func TestBuildIncomingAffectedZoneFragment_MapReadyCoordinates(t *testing.T) {
	now := time.Date(2026, 3, 15, 8, 3, 0, 0, time.UTC)
	candidate := &affectedZoneCandidate{
		CanonicalPath:  "/zone/sea/infrastructure_disruption/strait-of-hormuz",
		Label:          "Strait of Hormuz (critical infra)",
		ConditionType:  "infrastructure_disruption",
		ZoneType:       "infrastructure_disruption",
		ZoneSubType:    "systems_disruption",
		GeoToken:       "sea",
		RadiusKm:       180,
		Lat:            26.5667,
		Lon:            56.2500,
		HasCoordinates: true,
		GeoPrecision:   "precise",
	}
	event := &horizonMonitorFeedEvent{
		ExternalID:  "evt-hormuz",
		Title:       "Critical Threat Persists in Hormuz as Attacks and GPS Jamming Shake Shipping",
		Summary:     "Shipping disruptions continue in the Strait of Hormuz.",
		URL:         "https://www.reuters.com/world/middle-east/example-hormuz",
		OccurredAt:  now,
		Severity:    "high",
		CountryName: "Strait of Hormuz",
	}
	fragment := buildIncomingAffectedZoneFragment(
		candidate,
		"src-1",
		event,
		horizonMonitorNewsClassification{Category: "infrastructure", Level: "high"},
		now,
		now,
		9.5,
	)
	if fragment == nil {
		t.Fatal("expected fragment")
	}
	if !fragment.IsAffectedZoneGeo() {
		t.Fatalf("expected affected zone fragment, got class=%q subtype=%q", fragment.Class, fragment.Subtype)
	}
	if !fragment.HasCoordinates || fragment.Coordinates == nil {
		t.Fatalf("expected map-ready coordinates, got %+v", fragment.Coordinates)
	}
	if fragment.Coordinates.Lat != candidate.Lat || fragment.Coordinates.Lon != candidate.Lon {
		t.Fatalf("unexpected coordinates %+v", fragment.Coordinates)
	}
	if fragment.RadiusKm != candidate.RadiusKm {
		t.Fatalf("expected radius %.1f, got %.1f", candidate.RadiusKm, fragment.RadiusKm)
	}
	if got := strings.TrimSpace(asString(fragment.Qualifiers["geoPrecision"])); got != "precise" {
		t.Fatalf("expected geoPrecision=precise, got %q", got)
	}
}

func TestInferZoneGeo_PrefersTextCountryOverPublisherCountry(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:       "Tehran shrouded in toxic cloud after overnight explosion",
		Summary:     "Witnesses in Iran reported a toxic cloud over Tehran after the blast.",
		CountryCode: "FR",
		CountryName: "France",
		URL:         "https://www.france24.com/en/example",
	}
	geo, cc, country := inferZoneGeo(event, "", strings.Join([]string{event.Title, event.Summary}, " "), "military_conflict")
	if geo != "ir" || cc != "IR" || country != "Iran" {
		t.Fatalf("expected text country to beat publisher country, got geo=%q cc=%q country=%q", geo, cc, country)
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
			Class:          ah.FragmentClassGeo,
			Subtype:          "affected_zone",
			Labels:        map[string]string{"en": "Tehran Armed Tension"},
			Qualifiers: map[string]interface{}{
				"zoneSubType":  "armed_tension",
				"geoPrecision": "precise",
			},
			Metadata: horizonmodels.FragmentMetadata{LastUpdatedAt: now},
		},
		{
			CanonicalPath: "/zone/ir/military_conflict/iran-wide-alert",
			Class:          ah.FragmentClassGeo,
			Subtype:          "affected_zone",
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

func TestLooksLikeAdministrativeCommentaryText_Ceremony(t *testing.T) {
	text := "U.S. Hostage and Wrongful Detainee Day Flag Ceremony - U.S. Department of State"
	if !looksLikeAdministrativeCommentaryText(text) {
		t.Fatal("expected ceremony text to be treated as administrative commentary")
	}
}

func TestLooksLikeAdministrativeCommentaryText_DBriefAndRewild(t *testing.T) {
	cases := []string{
		"The D Brief: Iran war spreads across Mideast; And a bit more.",
		"Countries can rewild borders to deter invasions, says EU environment chief",
	}
	for _, text := range cases {
		if !looksLikeAdministrativeCommentaryText(text) {
			t.Fatalf("expected %q to be treated as administrative/broad commentary", text)
		}
	}
}

func TestLooksLikeCivilIncidentWithoutMilitaryContext_SynagogueExplosion(t *testing.T) {
	text := "Belgium to investigate targeted synagogue explosion in Liege after police secured the area"
	if !looksLikeCivilIncidentWithoutMilitaryContext(text) {
		t.Fatal("expected synagogue explosion to be treated as civil incident without military context")
	}
}

func TestLooksLikeExerciseWithoutIncidentText(t *testing.T) {
	text := "Navy fighters rehearse anti-ship missions off California during a large-scale exercise"
	if !looksLikeExerciseWithoutIncidentText(text) {
		t.Fatal("expected rehearsal text to be treated as exercise without incident")
	}
}

func TestIsLikelyTitlePlaceCandidate_RejectsMilitaryAssetName(t *testing.T) {
	if isLikelyTitlePlaceCandidate("USS George Washington") {
		t.Fatal("expected military asset names to be rejected as place candidates")
	}
}

func TestInferZoneGeoFromPlace_NorthSea(t *testing.T) {
	geoToken, countryCode, countryName := inferZoneGeoFromPlace("North Sea")
	if geoToken != "sea" || countryCode != "" || countryName != "North Sea" {
		t.Fatalf("unexpected geo inference: token=%s cc=%s name=%s", geoToken, countryCode, countryName)
	}
}

func TestShouldSuppressWeakAffectedZoneInference_RewildBorders(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title:   "Countries can rewild borders to deter invasions, says EU environment chief",
		Summary: "Jessika Roswall cites Poland and Finland, which have made border areas near Russia or its allies more hostile to cross.",
		Raw: map[string]interface{}{
			"sourceHost": "theguardian.com",
		},
	}
	cls := horizonMonitorNewsClassification{Category: "conflict", Level: "critical"}
	text := normalizeKeywordText(event.Title + " " + event.Summary)
	if !shouldSuppressWeakAffectedZoneInference(text, event, cls, "military_conflict", 1.18) {
		t.Fatal("expected rewild-borders commentary to be suppressed for affected-zone inference")
	}
}

func TestLooksLikeCapabilityOrFeatureText(t *testing.T) {
	cases := []string{
		"Meet the startups trying to build military-specific AI",
		"Brazil to develop air-launched anti-ship missile capability",
		"Ukraine Unbroken review – five searing dramas about the history and horror of Russia’s invasion",
	}
	for _, text := range cases {
		if !looksLikeCapabilityOrFeatureText(text) {
			t.Fatalf("expected %q to be treated as capability/feature commentary", text)
		}
	}
}

func TestShouldSuppressWeakAffectedZoneInference_DiseaseTrendArticle(t *testing.T) {
	event := &horizonMonitorFeedEvent{
		Title: "Average UK office attendance settling at highest level since before Covid",
	}
	cls := horizonMonitorNewsClassification{Category: "health", Level: "medium"}
	text := normalizeKeywordText(event.Title)
	if !shouldSuppressWeakAffectedZoneInference(text, event, cls, "disease_outbreak", 1.22) {
		t.Fatal("expected trend article mentioning covid to be suppressed for disease_outbreak zone inference")
	}
}

func TestShouldPromoteAffectedZonePrimaryEvidence_RejectsWeakAnalysisOverwrite(t *testing.T) {
	f := &horizonmodels.HorizonFragment{
		Class: ah.FragmentClassGeo,
		Subtype: "affected_zone",
		Qualifiers: map[string]interface{}{
			"sourceTitle":        "Missile strike hits refinery near Bandar Abbas",
			"sourceOccurredAt":   time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"primarySourceScore": 0.86,
			"conditionType":      "military_conflict",
		},
	}
	c := &affectedZoneCandidate{ConditionType: "military_conflict", InferenceScore: 1.12, GeoPrecision: "precise"}
	e := &horizonMonitorFeedEvent{
		Title:      "Iran and the Gulf: Why Hedging Is No Longer Enough",
		Summary:    "Analysis of regional balancing and strategic posture.",
		OccurredAt: time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC),
		URL:        "https://warontherocks.com/example",
	}
	cls := horizonMonitorNewsClassification{Category: "conflict", Level: "medium"}
	if shouldPromoteAffectedZonePrimaryEvidence(f, c, cls, e) {
		t.Fatal("expected weak analysis article not to overwrite strong existing primary evidence")
	}
}

func TestShouldUseAffectedZoneStoredSourceEvidence_RejectsWeakStoredSource(t *testing.T) {
	f := &horizonmodels.HorizonFragment{
		Class: ah.FragmentClassGeo,
		Subtype: "affected_zone",
		Qualifiers: map[string]interface{}{
			"sourceTitle":            "Using the 2026 Winter Olympics to Build Soft Power",
			"sourceSummary":          "A commentary on soft-power strategy and national image.",
			"sourceUrl":              "https://www.csis.org/example",
			"sourceCountryCode":      "KP",
			"primarySourceScore":     0.73,
			"classificationCategory": "humanitarian",
			"classificationLevel":    "medium",
			"classificationSource":   "heuristic",
			"conditionType":          "humanitarian_crisis",
			"zoneInferenceScore":     0.81,
		},
	}
	if shouldUseAffectedZoneStoredSourceEvidence(f) {
		t.Fatal("expected weak stored commentary source not to be trusted as primary zone evidence")
	}
}

func TestShouldUseAffectedZoneStoredSourceEvidence_RejectsCountryConflictWithExplicitPlace(t *testing.T) {
	f := &horizonmodels.HorizonFragment{
		Class: ah.FragmentClassGeo,
		Subtype: "affected_zone",
		Qualifiers: map[string]interface{}{
			"sourceTitle":            "Tehran shrouded in toxic cloud after US-Israeli strikes on oil deposits",
			"sourceSummary":          "Huge fires broke out in Tehran after Israeli strikes targeted Iranian oil infrastructure.",
			"sourceUrl":              "https://www.france24.com/example",
			"sourceCountryCode":      "FR",
			"primarySourceScore":     0.80,
			"classificationCategory": "conflict",
			"classificationLevel":    "high",
			"classificationSource":   "heuristic",
			"conditionType":          "military_conflict",
			"zoneInferenceScore":     2.8,
		},
	}
	if shouldUseAffectedZoneStoredSourceEvidence(f) {
		t.Fatal("expected stored source evidence to be rejected when explicit place conflicts with stored country")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesLegalCommercialDispute(t *testing.T) {
	text := normalizeKeywordText("Roman Abramovich ready to fight UK government over proceeds from £2.5bn Chelsea sale")
	cls := horizonMonitorNewsClassification{Category: "military", Level: "critical"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected legal commercial dispute to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesRetrospectiveConflictNarrative(t *testing.T) {
	text := normalizeKeywordText("The final voyage of the Iranian warship sunk by the US")
	cls := horizonMonitorNewsClassification{Category: "military", Level: "high"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected retrospective conflict narrative to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesSportsPoliticalHumanInterest(t *testing.T) {
	text := normalizeKeywordText("Five Iranian footballers in Australian safe house after Asian Cup protest")
	cls := horizonMonitorNewsClassification{Category: "protest", Level: "medium"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected sports political human-interest text to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesChokepointSecurityMissionWithoutDisruption(t *testing.T) {
	text := normalizeKeywordText("Macron says France allies preparing defensive mission to reopen Strait of Hormuz")
	cls := horizonMonitorNewsClassification{Category: "infrastructure", Level: "high"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected chokepoint security mission without outage to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesCompanyScopedCyberBreach(t *testing.T) {
	text := normalizeKeywordText("Ericsson US discloses data breach after service provider hack affecting employees and customers")
	cls := horizonMonitorNewsClassification{Category: "cyber", Level: "high"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected company-scoped cyber breach without outage to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesInvestigatedSystemsBreach(t *testing.T) {
	text := normalizeKeywordText("FBI investigates breach of surveillance and wiretap systems in the United States")
	cls := horizonMonitorNewsClassification{Category: "cyber", Level: "medium"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected investigated systems breach without outage to be suppressed as semantic drift")
	}
}

func TestShouldSuppressAffectedZoneBySemanticDrift_SuppressesOffshoreShippingIncidentAttributedElsewhere(t *testing.T) {
	text := normalizeKeywordText("Putin accuses Ukraine of attacking gas tanker that exploded and sank off Libya")
	cls := horizonMonitorNewsClassification{Category: "terrorism", Level: "high"}
	if !shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls) {
		t.Fatal("expected offshore shipping incident attributed elsewhere to be suppressed as semantic drift")
	}
}
