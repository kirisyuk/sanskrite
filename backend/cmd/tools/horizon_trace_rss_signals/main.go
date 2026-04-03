// horizon_trace_rss_signals runs the full RSS-first pipeline: fetch RSS → extract facts → signals → propagation.
//
// Flow:
//  1. Fetch RSS feed (via -feed-url or -source for DataSource ID)
//  2. Run LLM extraction per document → OntologyFact(events) in ontology_facts
//  3. For each event: SignalPipeline.ProcessEvent (extract signals → seed injection → propagation)
//  4. Output: event → signals → seed resolution (found/missing) → propagation result
//
// Usage:
//
//	GCP_PROJECT_ID=xxx go run ./cmd/tools/horizon_trace_rss_signals -project=xxx -feed-url=https://...
//
//	# Or use existing DataSource (reads RSS URL from Firestore)
//	go run ./cmd/tools/horizon_trace_rss_signals -project=xxx -source=31mwZSXeA7LQ3OWYKU1f
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/extraction"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/domain"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonproductdict "github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonsignallanding "github.com/igorkirisyuk/sanskrite/internal/horizon/signallanding"
	horizonsignals "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	tenantID = "global"
)

var (
	projectID     = flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project ID")
	feedURL       = flag.String("feed-url", "", "RSS feed URL (e.g. https://news.ycombinator.com/rss)")
	sourceID      = flag.String("source", "", "DataSource ID (reads RSS URL from Firestore)")
	docsLimit     = flag.Int("docs-limit", 5, "Max documents to extract per run (0 = all)")
	eventsLimit   = flag.Int("events-limit", 20, "Max events to process for signals (0 = all)")
	eventsOnly    = flag.Bool("events-only", false, "Skip RSS fetch and extraction; only process existing events from ontology_facts")
	simulate      = flag.Bool("simulate", false, "Simulate propagation (no persistence to fragments)")
	verbose       = flag.Bool("verbose", false, "Verbose logging")
	vertexStoreID = flag.String("vertex-store", "", "Vertex AI Search horizon datastore ID (env: VERTEX_SEARCH_HORIZON_DATA_STORE_ID)")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project or GCP_PROJECT_ID required")
	}

	eventsOnlyMode := *eventsOnly

	feedURLToUse := strings.TrimSpace(*feedURL)
	if !eventsOnlyMode && feedURLToUse == "" && strings.TrimSpace(*sourceID) != "" {
		fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
		if err != nil {
			log.Fatalf("firestore: %v", err)
		}
		defer fs.Close()
		ds, err := repo.NewDataSourcesRepository(fs).Get(ctx, strings.TrimSpace(*sourceID))
		if err != nil || ds == nil {
			log.Fatalf("source %s not found: %v", *sourceID, err)
		}
		if ds.Details.RSSURL == nil || strings.TrimSpace(*ds.Details.RSSURL) == "" {
			log.Fatalf("source %s has no RSS URL", *sourceID)
		}
		feedURLToUse = strings.TrimSpace(*ds.Details.RSSURL)
	}
	if !eventsOnlyMode && feedURLToUse == "" {
		log.Fatal("--feed-url or --source required (or use -events-only to process existing events)")
	}

	logger := log.New(os.Stdout, "[trace-rss] ", log.LstdFlags)

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	// AI executor for extraction
	apiKey := loadAPIKey(ctx, *projectID)
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY or VERTEX_API_KEY required (env or Secret Manager)")
	}
	_ = os.Setenv("GEMINI_API_KEY", apiKey)
	mm := ai.NewAIModelManager()
	exec := ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)

	// Repos
	entitiesRepo := repo.NewEntityRepository(fs)
	eventsRepo := repo.NewEventRepository(fs)
	claimsRepo := repo.NewClaimRepository(fs)
	relationsRepo := repo.NewRelationRepository(fs)
	sourcesRepo := repo.NewSourceRepository(fs)
	ontologyFactsRepo := repo.NewOntologyFactsRepository(fs)
	conceptsRepo := repo.NewConceptRepository(fs)
	uncertaintyRepo := repo.NewUncertaintyModelRepository(fs)
	trustRepo := repo.NewTrustRepository(fs)
	causalLinksRepo := repo.NewCausalLinkRepository(fs)

	// Ontology extractor deps (minimal: nil for optional)
	entityResolver := ontology.NewEntityResolver(entitiesRepo, nil, nil, exec, logger)
	trustIntegrator := ontology.NewTrustIntegrator(sourcesRepo, claimsRepo, eventsRepo, trustRepo, logger)
	conflictDetector := ontology.NewConflictDetector(claimsRepo, uncertaintyRepo, trustIntegrator, logger)
	causalDetector := ontology.NewCausalDetector(eventsRepo, causalLinksRepo, nil, exec, logger)
	domainService := domain.NewDomainService(conceptsRepo, relationsRepo, nil, exec, logger)

	ontologyExtractor := ontology.NewOntologyExtractor(
		entitiesRepo, eventsRepo, claimsRepo, relationsRepo, sourcesRepo,
		ontologyFactsRepo, entityResolver, nil, trustIntegrator, conflictDetector, causalDetector, domainService, logger,
	)

	ndExtractor := extraction.NewNormalizedDocumentExtractor(exec, ontologyExtractor)

	if !eventsOnlyMode {
		// Step 1: Fetch RSS
		logger.Printf("Fetching RSS from %s...", feedURLToUse)
		rssProvider := prov.NewRSSProvider()
		docs, err := rssProvider.Ingest(ctx, tenantID, feedURLToUse)
		if err != nil {
			log.Fatalf("RSS ingest failed: %v", err)
		}
		logger.Printf("Fetched %d documents", len(docs))

		if len(docs) > 0 {
			limit := *docsLimit
			if limit <= 0 {
				limit = len(docs)
			}
			if limit > len(docs) {
				limit = len(docs)
			}
			docs = docs[:limit]

			// Step 2: Extract facts (inline, no Pub/Sub)
			sourceIDForExtract := feedURLToUse
			if strings.TrimSpace(*sourceID) != "" {
				sourceIDForExtract = strings.TrimSpace(*sourceID)
			}
			for i := range docs {
				nd := &docs[i]
				if strings.TrimSpace(nd.FullText) == "" {
					continue
				}
				logger.Printf("Extracting doc %d/%d: %s", i+1, len(docs), truncate(nd.Title, 60))
				if err := ndExtractor.ExtractAndPersist(ctx, tenantID, sourceIDForExtract, nd); err != nil {
					logger.Printf("Extract failed: %v", err)
				}
			}
		}
	} else {
		logger.Printf("Events-only mode: skipping RSS fetch (processing existing ontology_facts — use -feed-url or -source for fresh RSS)")
	}

	// Step 3: List events
	events, err := ontologyFactsRepo.ListByTenantAndKind(ctx, tenantID, "event", 500)
	if err != nil {
		log.Fatalf("List events failed: %v", err)
	}
	logger.Printf("Found %d events in ontology_facts", len(events))

	if len(events) == 0 {
		logger.Printf("No events to process. Run with more docs or check extraction.")
		return
	}

	evLimit := *eventsLimit
	if evLimit <= 0 {
		evLimit = len(events)
	}
	if evLimit > len(events) {
		evLimit = len(events)
	}
	events = events[:evLimit]

	// Step 4: Wire SignalPipeline
	horizonFragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fs)
	horizonEdgesRepo := horizonrepo.NewDependencyEdgesRepository(fs)
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fs)
	lensesRepo := horizonrepo.NewLensesRepository(fs)
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fs)
	profileLookup := horizonpropagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		logger.Printf("ProfileLookup init: %v (using defaults)", err)
	}

	propagationEngine := horizonpropagation.NewPropagationEngine(
		horizonFragmentsRepo, versionsRepo, horizonEdgesRepo, lensesRepo, nil,
	).WithProfileLookup(profileLookup)
	productDictionary := horizonproductdict.NewRuntime(fs)

	signalExtractor := horizonsignals.NewSignalExtractor(nil, exec)
	applicability := horizonsignals.NewExternalApplicabilityEvaluator().
		WithFragmentsRepo(horizonFragmentsRepo).
		WithEdgesRepo(horizonEdgesRepo)
	seedInjector := horizonsignals.NewSeedInjector(horizonFragmentsRepo, horizonEdgesRepo).
		WithApplicabilityEvaluator(applicability)

	// Vertex AI Search semantic resolution (primary tool for ontology path -> graph fragment)
	horizonDS := strings.TrimSpace(*vertexStoreID)
	if horizonDS == "" {
		horizonDS = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
	}
	var vertexClient *ai.VertexAISearchClient
	if horizonDS != "" {
		vertexLoc := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
		if vertexLoc == "" {
			vertexLoc = "global"
		}
		vertexClient = ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), vertexLoc, horizonDS, "")
		if vertexClient.IsConfigured() {
			seedInjector = seedInjector.WithVertexSearch(vertexClient)
			logger.Printf("Vertex AI Search semantic resolution enabled (horizon datastore=%s)", horizonDS)
		}
	}

	signalPipeline := horizonscout.NewSignalPipeline(
		signalExtractor, seedInjector, propagationEngine, profileLookup, ontologyFactsRepo,
	).WithEscalationLLM(exec)
	signalLandingService := horizonsignallanding.NewService(
		horizonrepo.NewSignalLandingSessionsRepository(fs),
		horizonrepo.NewSignalImpactClaimsRepository(fs),
		horizonrepo.NewSignalLandingHitsRepository(fs),
		horizonrepo.NewSignalLandingSummariesRepository(fs),
		horizonrepo.NewSignalMissingAnchorCandidatesRepository(fs),
		horizonFragmentsRepo,
	).WithProductDictionary(productDictionary)
	if vertexClient != nil && vertexClient.IsConfigured() {
		signalLandingService = signalLandingService.WithVertexSearch(vertexClient)
	}
	signalPipeline = signalPipeline.WithSignalLanding(signalLandingService)

	// Step 5: Process each event
	fmt.Println("\n=== RSS-first pipeline trace ===")
	for i, ev := range events {
		fmt.Printf("\n--- Event %d/%d: %s ---\n", i+1, len(events), truncate(ev.Title, 80))
		fmt.Printf("  ID: %s  Type: %s\n", ev.ID, ev.Type)
		fmt.Printf("  CanonicalPaths: %v\n", ev.CanonicalPaths)
		fmt.Printf("  ActorPaths: %v\n", ev.ActorPaths)
		if ev.LocationPath != nil {
			fmt.Printf("  LocationPath: %s\n", *ev.LocationPath)
		}

		if *simulate {
			// Simulate: extract signals, check seed resolution, run propagation in simulate mode
			runSimulation(ctx, signalExtractor, seedInjector, propagationEngine, profileLookup, horizonFragmentsRepo, ev)
		} else {
			if err := signalPipeline.ProcessEvent(ctx, ev); err != nil {
				logger.Printf("ProcessEvent failed: %v", err)
			}
		}
	}
	fmt.Println("\n=== Done ===")
}

func runSimulation(
	ctx context.Context,
	extractor *horizonsignals.SignalExtractor,
	seedInjector *horizonsignals.SeedInjector,
	engine *horizonpropagation.PropagationEngine,
	profileLookup *horizonpropagation.ProfileLookup,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	ev *models.OntologyFact,
) {
	signals, err := extractor.Extract(ctx, ev)
	if err != nil {
		log.Printf("  Signal extraction failed: %v", err)
		return
	}
	if len(signals) == 0 {
		fmt.Println("  No signals extracted")
		return
	}
	fmt.Printf("  Signals: %d\n", len(signals))
	for _, sig := range signals {
		fmt.Printf("    - %s axis=%s amplitude=%.2f\n", sig.Polarity, sig.Axis, sig.Amplitude)
		fmt.Printf("      SeedPaths: %v\n", sig.SeedPaths)
		if len(sig.SeedNames) > 0 {
			fmt.Printf("      SeedNames: %v\n", sig.SeedNames)
		}
		// Check seed resolution (exact path or Vertex AI Search semantic by name)
		var foundIDs []string
		for i, p := range sig.SeedPaths {
			searchName := ""
			if i < len(sig.SeedNames) {
				searchName = sig.SeedNames[i]
			}
			fragID := seedInjector.ResolvePath(ctx, strings.TrimSpace(p), searchName)
			if fragID != "" {
				foundIDs = append(foundIDs, fragID)
				frag, _ := fragmentsRepo.Get(ctx, fragID)
				pathNote := ""
				if frag != nil && frag.CanonicalPath != p {
					pathNote = fmt.Sprintf(" (graph path: %s)", frag.CanonicalPath)
				}
				fmt.Printf("      ✓ %s -> fragment %s%s\n", p, fragID, pathNote)
			} else {
				fmt.Printf("      ✗ %s -> NOT IN GRAPH\n", p)
			}
		}
		if len(foundIDs) == 0 {
			fmt.Printf("      → No seeds, propagation skipped\n")
			continue
		}
		traceCtx := horizonpropagation.WithSignalTraceConfig(ctx, horizonpropagation.SignalTraceConfig{
			Enabled:  true,
			MaxSteps: 500,
		})
		result, err := engine.SimulateSignalFromSeeds(traceCtx, sig, profileLookup, foundIDs)
		if err != nil {
			fmt.Printf("      → Propagation error: %v\n", err)
			continue
		}
		fmt.Printf("      → Affected: %d nodes, paths: %d\n",
			len(result.AffectedFragments), len(result.PropagationPaths))
		if *verbose && len(result.PropagationPaths) > 0 {
			for _, path := range result.PropagationPaths[:min(5, len(result.PropagationPaths))] {
				fmt.Printf("        path: %v\n", path)
			}
		}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func loadAPIKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY", "vertex_api_key", "VERTEX_API_KEY"} {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
