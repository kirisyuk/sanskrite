package discovery

import (
	"context"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonsignals "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type graphAuditEventSummary struct {
	feedName             string
	title                string
	signalType           string
	axis                 string
	kernelMode           string
	locationPath         string
	countryCode          string
	seeds                int
	tailSelected         int
	locationOnly         bool
	affected             int
	longPaths            int
	remoteTails          int
	interestingEndpoints int
	nonObviousEndpoints  int
	facilityEndpoints    int
	pathFamilies         int
	businessPathFamilies int
	crossBorderFamilies  int
	breakpoints          int
	thresholdBreakpoints int
	transferSkips        int
	hopLimitBreakpoints  int
	traceDropped         int
	longestPathHops      int
	topRemoteTails       []string
	topPathFamilies      []string
	seedSample           []string
	seedPathSample       []string
	noSeed               bool
	propagationError     string
}

// RUN_LIVE_RSS_GRAPH_AUDIT=1 go test ./internal/discovery -run TestLiveRSSGraphTailAudit -v
func TestLiveRSSGraphTailAudit(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RUN_LIVE_RSS_GRAPH_AUDIT")) != "1" {
		t.Skip("set RUN_LIVE_RSS_GRAPH_AUDIT=1 to run live heuristic graph audit")
	}

	projectID := firstNonEmptyString(
		strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")),
		strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")),
		"etigram",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		t.Fatalf("firestore init: %v", err)
	}
	defer fsClient.Close()

	feeds, err := loadIntelFeedsForProbe()
	if err != nil {
		t.Fatalf("load feeds: %v", err)
	}
	feeds = filterProbeFeeds(feeds, os.Getenv("RSS_GRAPH_AUDIT_MATCH"))
	if len(feeds) == 0 {
		t.Fatal("no feeds selected for graph audit")
	}

	feedLimit := envInt("RSS_GRAPH_AUDIT_FEEDS", 4)
	docLimit := envInt("RSS_GRAPH_AUDIT_DOCS", 4)
	if feedLimit > len(feeds) {
		feedLimit = len(feeds)
	}
	feeds = feeds[:feedLimit]

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())
	lensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fsClient.Client())
	profileLookup := horizonpropagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		t.Fatalf("profile lookup init: %v", err)
	}

	engine := horizonpropagation.NewPropagationEngine(
		fragmentsRepo,
		versionsRepo,
		edgesRepo,
		lensesRepo,
		nil,
	).WithProfileLookup(profileLookup)

	logger := log.New(io.Discard, "", 0)
	pipeline := NewHorizonMonitorPipeline(nil, nil, fragmentsRepo, edgesRepo, engine, profileLookup, logger)
	if pipeline.seedInjector != nil {
		applicability := horizonsignals.NewExternalApplicabilityEvaluator().
			WithFragmentsRepo(fragmentsRepo).
			WithEdgesRepo(edgesRepo)
		pipeline.seedInjector = pipeline.seedInjector.
			WithApplicabilityEvaluator(applicability).
			WithMaterializedRepo(horizonrepo.NewMaterializedArtifactsRepository(fsClient.Client()))
		if vertexClient := newAuditVertexClient(projectID); vertexClient != nil {
			pipeline.seedInjector = pipeline.seedInjector.WithVertexSearch(vertexClient)
		}
	}

	rss := prov.NewRSSProvider()
	endpointScorer := horizonpropagation.ScoreInterestingEndpoints
	summaries := make([]graphAuditEventSummary, 0, feedLimit*docLimit)
	totalSignals := 0
	noSeedSignals := 0
	locationOnlySignals := 0
	remoteTailSignals := 0
	longPathSignals := 0
	interestingEndpointSignals := 0
	nonObviousEndpointSignals := 0
	businessPathFamilySignals := 0
	totalAffected := 0

	for _, feed := range feeds {
		feedCtx, feedCancel := context.WithTimeout(ctx, 35*time.Second)
		docs, ingestErr := rss.Ingest(feedCtx, "global", feed.URL)
		feedCancel()
		if ingestErr != nil {
			t.Logf("feed %q ingest error: %v", feed.Name, ingestErr)
			continue
		}
		if docLimit > 0 && len(docs) > docLimit {
			docs = docs[:docLimit]
		}

		for _, doc := range docs {
			event := pipeline.buildEventFromDocument("graph-audit", "intel_rss", feed.Type, "", feed.URL, doc)
			if event == nil {
				continue
			}
			eventFact := syntheticAuditEventFact(event, feed.Name)
			signal := pipeline.buildSignalDescriptor(eventFact, event)
			if signal == nil {
				continue
			}
			totalSignals++

			summary := graphAuditEventSummary{
				feedName:       feed.Name,
				title:          event.Title,
				signalType:     event.SignalType,
				axis:           signal.Axis,
				locationPath:   signal.LocationPath,
				countryCode:    strings.ToLower(strings.TrimSpace(event.CountryCode)),
				locationOnly:   len(signal.SeedPaths) == 0 && len(signal.SeedNames) == 0 && strings.TrimSpace(signal.LocationPath) != "",
				seedPathSample: append([]string(nil), signal.SeedPaths...),
			}
			if summary.locationOnly {
				locationOnlySignals++
			}

			seedIDs, seedErr := pipeline.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
			if seedErr != nil {
				if seedErr == horizonsignals.ErrNoSeedNodes {
					noSeedSignals++
					summary.noSeed = true
				} else {
					summary.propagationError = seedErr.Error()
				}
				summaries = append(summaries, summary)
				continue
			}
			summary.seeds = len(seedIDs)
			summary.seedSample = auditSeedPaths(ctx, fragmentsRepo, seedIDs, 5)

			traceCtx := horizonpropagation.WithSignalTraceConfig(ctx, horizonpropagation.SignalTraceConfig{
				Enabled:  true,
				MaxSteps: envInt("RSS_GRAPH_AUDIT_TRACE_STEPS", 900),
			})
			kernelCfg := horizonpropagation.DefaultHeuristicSignalKernelConfig(signal)
			summary.kernelMode = kernelCfg.Mode
			traceCtx = horizonpropagation.WithSignalKernelConfig(traceCtx, kernelCfg)
			result, err := engine.SimulateSignalFromSeeds(traceCtx, signal, profileLookup, seedIDs)
			if err != nil {
				summary.propagationError = err.Error()
				summaries = append(summaries, summary)
				continue
			}

			summary.affected = len(result.AffectedFragments)
			summary.tailSelected = int(result.Metrics["typed_rwr_selected"])
			summary.traceDropped = result.TraceDropped
			summary.longPaths, summary.remoteTails, summary.longestPathHops, summary.topRemoteTails = summarizePropagationPaths(result.PropagationPaths, summary.countryCode)
			if endpointSummary, scoreErr := endpointScorer(ctx, fragmentsRepo, signal, result.PropagationPaths, result.Trace, 5); scoreErr == nil {
				summary.interestingEndpoints = endpointSummary.BusinessEndpoints
				summary.nonObviousEndpoints = endpointSummary.NonObviousEndpoints
				summary.facilityEndpoints = endpointSummary.FacilityEndpoints
				summary.topRemoteTails = make([]string, 0, len(endpointSummary.TopEndpoints))
				for _, item := range endpointSummary.TopEndpoints {
					if strings.TrimSpace(item.CanonicalPath) != "" {
						summary.topRemoteTails = append(summary.topRemoteTails, strings.TrimSpace(item.CanonicalPath))
					}
				}
			}
			pathFamilySummary := horizonpropagation.ScorePathFamilies(signal, result.Trace, 4)
			summary.pathFamilies = pathFamilySummary.PathFamilies
			summary.businessPathFamilies = pathFamilySummary.BusinessPathFamilies
			summary.crossBorderFamilies = pathFamilySummary.CrossBorderFamilies
			breakpointSummary := horizonpropagation.AuditTraceBreakpoints(result.Trace, 4)
			summary.breakpoints = breakpointSummary.PathBreakpoints
			summary.thresholdBreakpoints = breakpointSummary.ThresholdDrops
			summary.transferSkips = breakpointSummary.TransferSkips
			summary.hopLimitBreakpoints = breakpointSummary.HopLimitDrops
			summary.topPathFamilies = make([]string, 0, len(pathFamilySummary.TopFamilies))
			for _, family := range pathFamilySummary.TopFamilies {
				summary.topPathFamilies = append(summary.topPathFamilies, family.Key)
			}
			if summary.remoteTails > 0 {
				remoteTailSignals++
			}
			if summary.longPaths > 0 {
				longPathSignals++
			}
			if summary.interestingEndpoints > 0 {
				interestingEndpointSignals++
			}
			if summary.nonObviousEndpoints > 0 {
				nonObviousEndpointSignals++
			}
			if summary.businessPathFamilies > 0 {
				businessPathFamilySignals++
			}
			totalAffected += summary.affected
			summaries = append(summaries, summary)
		}
	}

	if totalSignals == 0 {
		t.Fatal("no heuristic signals produced for graph audit")
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].nonObviousEndpoints == summaries[j].nonObviousEndpoints {
			if summaries[i].interestingEndpoints == summaries[j].interestingEndpoints {
				if summaries[i].affected == summaries[j].affected {
					return summaries[i].title < summaries[j].title
				}
				return summaries[i].affected > summaries[j].affected
			}
			return summaries[i].interestingEndpoints > summaries[j].interestingEndpoints
		}
		return summaries[i].nonObviousEndpoints > summaries[j].nonObviousEndpoints
	})

	t.Logf("graph audit config: project=%s feeds=%d docs_per_feed=%d", projectID, len(feeds), docLimit)
	for i, summary := range summaries {
		if i >= 12 {
			break
		}
		switch {
		case summary.noSeed:
			t.Logf("event[%d] NO_SEED feed=%s axis=%s signal=%s country=%s location=%s title=%q",
				i+1, summary.feedName, summary.axis, summary.signalType, summary.countryCode, summary.locationPath, summary.title)
		case summary.propagationError != "":
			t.Logf("event[%d] ERROR feed=%s axis=%s signal=%s seeds=%d err=%s title=%q",
				i+1, summary.feedName, summary.axis, summary.signalType, summary.seeds, summary.propagationError, summary.title)
		default:
			t.Logf("event[%d] feed=%s axis=%s kernel=%s signal=%s seeds=%d tail_selected=%d location_only=%v affected=%d interesting_endpoints=%d non_obvious=%d facilities=%d path_families=%d business_families=%d cross_border_families=%d breakpoints=%d threshold_breakpoints=%d transfer_skips=%d hop_limit_breakpoints=%d remote_tails=%d long_paths=%d longest_hops=%d title=%q",
				i+1, summary.feedName, summary.axis, summary.kernelMode, summary.signalType, summary.seeds, summary.tailSelected, summary.locationOnly, summary.affected, summary.interestingEndpoints, summary.nonObviousEndpoints, summary.facilityEndpoints, summary.pathFamilies, summary.businessPathFamilies, summary.crossBorderFamilies, summary.breakpoints, summary.thresholdBreakpoints, summary.transferSkips, summary.hopLimitBreakpoints, summary.remoteTails, summary.longPaths, summary.longestPathHops, summary.title)
			if len(summary.seedSample) > 0 {
				t.Logf("event[%d] seed_sample=%v", i+1, summary.seedSample)
			}
			if len(summary.topRemoteTails) > 0 {
				t.Logf("event[%d] endpoint_sample=%v", i+1, summary.topRemoteTails)
			}
			if len(summary.topPathFamilies) > 0 {
				t.Logf("event[%d] path_family_sample=%v", i+1, summary.topPathFamilies)
			}
		}
	}

	avgAffected := float64(totalAffected) / float64(totalSignals)
	t.Logf(
		"summary signals=%d no_seed=%d seeded=%.3f location_only=%.3f interesting_endpoint_signals=%.3f non_obvious_endpoint_signals=%.3f business_path_family_signals=%.3f remote_tail_signals=%.3f long_path_signals=%.3f avg_affected=%.2f",
		totalSignals,
		noSeedSignals,
		float64(totalSignals-noSeedSignals)/float64(totalSignals),
		float64(locationOnlySignals)/float64(totalSignals),
		float64(interestingEndpointSignals)/float64(totalSignals),
		float64(nonObviousEndpointSignals)/float64(totalSignals),
		float64(businessPathFamilySignals)/float64(totalSignals),
		float64(remoteTailSignals)/float64(totalSignals),
		float64(longPathSignals)/float64(totalSignals),
		avgAffected,
	)
}

// RUN_LIVE_HISTORICAL_ZONE_AUDIT=1 go test ./internal/discovery -run TestLiveHistoricalConcreteIncidentPropagationAudit -v
func TestLiveHistoricalConcreteIncidentPropagationAudit(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RUN_LIVE_HISTORICAL_ZONE_AUDIT")) != "1" {
		t.Skip("set RUN_LIVE_HISTORICAL_ZONE_AUDIT=1 to run live historical propagation audit")
	}

	projectID := firstNonEmptyString(
		strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")),
		strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")),
		"etigram",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		t.Fatalf("firestore init: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())
	lensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fsClient.Client())
	profileLookup := horizonpropagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		t.Fatalf("profile lookup init: %v", err)
	}

	engine := horizonpropagation.NewPropagationEngine(
		fragmentsRepo,
		versionsRepo,
		edgesRepo,
		lensesRepo,
		nil,
	).WithProfileLookup(profileLookup)

	logger := log.New(io.Discard, "", 0)
	pipeline := NewHorizonMonitorPipeline(nil, nil, fragmentsRepo, edgesRepo, engine, profileLookup, logger)
	if pipeline.seedInjector != nil {
		applicability := horizonsignals.NewExternalApplicabilityEvaluator().
			WithFragmentsRepo(fragmentsRepo).
			WithEdgesRepo(edgesRepo)
		pipeline.seedInjector = pipeline.seedInjector.
			WithApplicabilityEvaluator(applicability).
			WithMaterializedRepo(horizonrepo.NewMaterializedArtifactsRepository(fsClient.Client()))
		if vertexClient := newAuditVertexClient(projectID); vertexClient != nil {
			pipeline.seedInjector = pipeline.seedInjector.WithVertexSearch(vertexClient)
		}
	}

	cases := []struct {
		name  string
		event horizonMonitorFeedEvent
	}{
		{
			name: "Fujairah drone attack",
			event: horizonMonitorFeedEvent{
				SourceID:    "historical-audit",
				FeedType:    "intel_rss",
				ExternalID:  "historical-fujairah-drone-attack",
				Title:       "UAE's Fujairah stops some oil loading operations after drone attack",
				Summary:     "Operations were halted at Fujairah after the reported drone attack.",
				URL:         "https://www.reuters.com/world/middle-east/example-fujairah",
				CountryCode: "AE",
				CountryName: "United Arab Emirates",
				Severity:    "high",
				Confidence:  0.84,
				OccurredAt:  time.Date(2026, 3, 15, 8, 2, 0, 0, time.UTC),
			},
		},
		{
			name: "Hormuz GPS jamming",
			event: horizonMonitorFeedEvent{
				SourceID:   "historical-audit",
				FeedType:   "intel_rss",
				ExternalID: "historical-hormuz-gps-jamming",
				Title:      "Critical Threat Persists in Hormuz as Attacks and GPS Jamming Shake Shipping",
				Summary:    "Shipping disruptions and navigation interference continue in the Strait of Hormuz.",
				URL:        "https://www.reuters.com/world/middle-east/example-hormuz",
				Severity:   "high",
				Confidence: 0.84,
				OccurredAt: time.Date(2026, 3, 15, 8, 3, 0, 0, time.UTC),
			},
		},
		{
			name: "Salalah port drone strike",
			event: horizonMonitorFeedEvent{
				SourceID:    "historical-audit",
				FeedType:    "intel_rss",
				ExternalID:  "historical-salalah-port-drone-strike",
				Title:       "Oman works to contain fire at Salalah port after drones strike",
				Summary:     "Emergency crews responded after drones struck Salalah port.",
				URL:         "https://www.reuters.com/world/middle-east/example-salalah",
				CountryCode: "OM",
				CountryName: "Oman",
				Severity:    "high",
				Confidence:  0.84,
				OccurredAt:  time.Date(2026, 3, 15, 7, 19, 0, 0, time.UTC),
			},
		},
		{
			name: "Kharg Island strike",
			event: horizonMonitorFeedEvent{
				SourceID:    "historical-audit",
				FeedType:    "intel_rss",
				ExternalID:  "historical-kharg-island-strike",
				Title:       "The US hit Iran's Kharg Island, key to its economy and security",
				Summary:     "Kharg Island was hit in a strike on a strategic Iranian oil export site.",
				URL:         "https://www.apnews.com/example-kharg",
				CountryCode: "IR",
				CountryName: "Iran",
				Severity:    "high",
				Confidence:  0.84,
				OccurredAt:  time.Date(2026, 3, 15, 7, 58, 0, 0, time.UTC),
			},
		},
		{
			name: "Bab el-Mandeb blockade threat",
			event: horizonMonitorFeedEvent{
				SourceID:    "historical-audit",
				FeedType:    "intel_rss",
				ExternalID:  "historical-bab-el-mandeb-blockade-threat",
				Title:       "Houthis warn they will block shipping through Bab el-Mandeb",
				Summary:     "Red Sea maritime traffic faces closure threats as militants target vessels near the chokepoint.",
				URL:         "https://www.reuters.com/world/middle-east/example-bab-el-mandeb",
				CountryCode: "YE",
				CountryName: "Yemen",
				Severity:    "high",
				Confidence:  0.81,
				OccurredAt:  time.Date(2026, 3, 15, 8, 5, 0, 0, time.UTC),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := tc.event
			cls := normalizeClassificationWithSourceHint(classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " ")), &event)
			candidate := inferAffectedZoneCandidate(&event, cls)
			if candidate == nil || !candidate.HasCoordinates {
				t.Fatalf("expected map-ready affected zone candidate, got %+v", candidate)
			}
			if event.LocationPath == "" {
				switch {
				case candidate.CountryCode != "":
					event.LocationPath = locationPathFromCountryCode(candidate.CountryCode)
				case candidate.GeoToken != "":
					event.LocationPath = "/loc/" + candidate.GeoToken
				}
			}
			event.SignalType = inferHorizonMonitorSignalType("", &event, cls)
			if bp, ok := horizonMonitorBlueprintFromSignal(event.SignalType); ok {
				event.EventType = eventTypeFromSignalType(bp.SignalType)
				event.SpherePaths = append([]string{}, bp.Sphere...)
			}

			eventFact := syntheticAuditEventFact(&event, tc.name)
			if eventFact.LocationPath == nil && strings.TrimSpace(event.LocationPath) != "" {
				loc := strings.TrimSpace(event.LocationPath)
				eventFact.LocationPath = &loc
			}
			signal := pipeline.buildSignalDescriptor(eventFact, &event)
			if signal == nil {
				t.Fatal("expected signal descriptor")
			}

			seedIDs, err := pipeline.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
			if err != nil {
				t.Fatalf("seed resolution failed: %v", err)
			}
			if len(seedIDs) == 0 {
				t.Fatal("expected non-empty seed set")
			}

			traceCtx := horizonpropagation.WithSignalTraceConfig(ctx, horizonpropagation.SignalTraceConfig{
				Enabled:  true,
				MaxSteps: envInt("RSS_GRAPH_AUDIT_TRACE_STEPS", 900),
			})
			traceCtx = horizonpropagation.WithSignalKernelConfig(traceCtx, horizonpropagation.DefaultHeuristicSignalKernelConfig(signal))
			result, err := engine.SimulateSignalFromSeeds(traceCtx, signal, profileLookup, seedIDs)
			if err != nil {
				t.Fatalf("propagation failed: %v", err)
			}
			if len(result.AffectedFragments) == 0 && len(result.PropagationPaths) == 0 {
				t.Fatalf("expected propagation effect, got affected=%d paths=%d", len(result.AffectedFragments), len(result.PropagationPaths))
			}
			endpointSummary, scoreErr := horizonpropagation.ScoreInterestingEndpoints(ctx, fragmentsRepo, signal, result.PropagationPaths, result.Trace, 5)
			if scoreErr != nil {
				t.Fatalf("endpoint scoring failed: %v", scoreErr)
			}
			longPaths, remoteTails, longestPathHops, topRemote := summarizePropagationPaths(result.PropagationPaths, strings.ToLower(strings.TrimSpace(candidate.CountryCode)))
			pathFamilySummary := horizonpropagation.ScorePathFamilies(signal, result.Trace, 4)
			t.Logf(
				"candidate=%s seeds=%d affected=%d paths=%d remoteTails=%d longPaths=%d longestHops=%d interesting=%d nonObvious=%d businessFamilies=%d topRemote=%v",
				candidate.CanonicalPath,
				len(seedIDs),
				len(result.AffectedFragments),
				len(result.PropagationPaths),
				remoteTails,
				longPaths,
				longestPathHops,
				endpointSummary.BusinessEndpoints,
				endpointSummary.NonObviousEndpoints,
				pathFamilySummary.BusinessPathFamilies,
				topRemote,
			)
		})
	}
}

func syntheticAuditEventFact(event *horizonMonitorFeedEvent, sourceLabel string) *coremodels.OntologyFact {
	now := event.OccurredAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var locPtr *string
	if lp := strings.TrimSpace(event.LocationPath); lp != "" {
		loc := lp
		locPtr = &loc
	}
	return &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             "audit_evt_" + shortHash(strings.Join([]string{sourceLabel, event.ExternalID, event.Title}, "|")),
			TenantID:       "global",
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			Labels:         map[string]string{"en": strings.TrimSpace(event.Title)},
			Status:         "active",
			CanonicalPaths: uniqueStrings([]string{"/event/horizon-monitor/" + slug(event.ExternalID)}),
			LocationPath:   locPtr,
			SpherePaths:    uniqueStrings(event.SpherePaths),
		},
		Kind:       "event",
		Type:       strings.TrimSpace(event.EventType),
		Title:      strings.TrimSpace(event.Title),
		Summary:    strings.TrimSpace(event.Summary),
		Confidence: wmClamp01(event.Confidence),
		OccurredAt: &now,
		ObservedAt: &now,
	}
}

func summarizePropagationPaths(paths [][]string, localCountry string) (longPaths int, remoteTails int, longestHops int, topRemote []string) {
	remoteSeen := map[string]int{}
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		hops := len(path) - 1
		if hops > longestHops {
			longestHops = hops
		}
		if hops >= 2 {
			longPaths++
		}
		tail := strings.TrimSpace(path[len(path)-1])
		if tail == "" {
			continue
		}
		if isRemoteTail(localCountry, tail) {
			remoteSeen[tail]++
		}
	}
	remoteTails = len(remoteSeen)
	type kv struct {
		path  string
		count int
	}
	items := make([]kv, 0, len(remoteSeen))
	for path, count := range remoteSeen {
		items = append(items, kv{path: path, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].path < items[j].path
		}
		return items[i].count > items[j].count
	})
	for i := 0; i < len(items) && i < 5; i++ {
		topRemote = append(topRemote, items[i].path)
	}
	return longPaths, remoteTails, longestHops, topRemote
}

func isRemoteTail(localCountry, canonicalPath string) bool {
	localCountry = strings.ToLower(strings.TrimSpace(localCountry))
	tailGeo := geoTokenFromCanonicalPath(canonicalPath)
	if tailGeo == "" {
		return false
	}
	if localCountry == "" {
		return tailGeo != "global"
	}
	return tailGeo != localCountry
}

func geoTokenFromCanonicalPath(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "fac", "deposit", "market", "loc", "zone", "community", "track":
		return strings.TrimSpace(parts[1])
	case "segment":
		if len(parts) >= 4 {
			return strings.TrimSpace(parts[3])
		}
	}
	return ""
}

func auditSeedPaths(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, seedIDs []string, limit int) []string {
	out := make([]string, 0, min(limit, len(seedIDs)))
	for _, id := range seedIDs {
		if len(out) >= limit {
			break
		}
		frag, err := repo.Get(ctx, id)
		if err != nil || frag == nil {
			continue
		}
		out = append(out, strings.TrimSpace(frag.CanonicalPath))
	}
	return out
}

func filterProbeFeeds(feeds []probeFeed, raw string) []probeFeed {
	matchers := splitCSVLower(raw)
	if len(matchers) == 0 {
		return feeds
	}
	out := make([]probeFeed, 0, len(feeds))
	for _, feed := range feeds {
		hay := strings.ToLower(strings.Join([]string{feed.Name, feed.URL, feed.Type}, " "))
		for _, token := range matchers {
			if strings.Contains(hay, token) {
				out = append(out, feed)
				break
			}
		}
	}
	return out
}

func splitCSVLower(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func newAuditVertexClient(projectID string) *ai.VertexAISearchClient {
	dataStoreID := firstNonEmptyString(
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID")),
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID")),
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")),
	)
	if dataStoreID == "" && strings.TrimSpace(projectID) != "" {
		dataStoreID = gcp.GetEnvOrSecret(context.Background(), strings.TrimSpace(projectID), "VERTEX_SEARCH_DATA_STORE_ID", "vertex_search_data_store_id")
	}
	if dataStoreID == "" {
		return nil
	}
	location := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
	if location == "" {
		location = "global"
	}
	client := ai.NewVertexAISearchClient(projectID, location, dataStoreID, "")
	if client == nil || !client.IsConfigured() {
		return nil
	}
	return client
}
