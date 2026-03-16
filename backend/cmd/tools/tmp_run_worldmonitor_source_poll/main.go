package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

func main() {
	var (
		sourceID  = flag.String("source", "", "source id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		projectID = flag.String("project", "etigram", "project id")
	)
	flag.Parse()
	if strings.TrimSpace(*sourceID) == "" {
		log.Fatal("--source is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer fsClient.Close()

	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient.Client(), "v1", 10*time.Minute)
	_ = taxonomy.RefreshRuntimeTaxonomy(ctx)
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
	_ = taxonomy.RefreshMarketSegmentDictionary(ctx)

	logger := log.New(os.Stdout, "[tmp-worldmonitor-poll] ", log.LstdFlags|log.Lshortfile)
	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())
	ontologyFactsRepo := repo.NewOntologyFactsRepository(fsClient.Client())
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())
	lensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fsClient.Client())
	profileLookup := horizonpropagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		logger.Printf("profile lookup init failed, using defaults: %v", err)
	}
	propagationEngine := horizonpropagation.NewPropagationEngine(
		fragmentsRepo,
		versionsRepo,
		edgesRepo,
		lensesRepo,
		nil,
	).WithProfileLookup(profileLookup)

	publisher, err := realtime.NewPublisher(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	exec := ai.NewAITaskExecutor(nil, nil)
	wc := crawler.NewWebCrawler()
	tgBase := os.Getenv("TELEGRAM_SERVICE_URL")
	tgClient := telegram.NewClient(tgBase)
	orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, nil, sourcesRepo, publisher, logger)
	orchestrator = orchestrator.WithSourceProjects(sourceProjectsRepo)
	reg := prov.NewRegistry()
	reg.Register(prov.NewRSSProvider())
	orchestrator = orchestrator.WithDocumentIngest(reg, docsRepo)
	emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
	orchestrator = orchestrator.WithEmbedding(emb)
	orchestrator = orchestrator.WithIngestHistory(ingestHistoryRepo)
	pipeline := discovery.NewHorizonMonitorPipeline(
		ontologyFactsRepo,
		exec,
		fragmentsRepo,
		edgesRepo,
		propagationEngine,
		profileLookup,
		logger,
	)
	orchestrator = orchestrator.WithHorizonMonitorPipeline(pipeline)

	poller := discovery.NewSourcePoller(orchestrator, logger)
	if err := poller.RunSourcePoll(ctx, discovery.SourceTaskPayload{
		SourceID:   strings.TrimSpace(*sourceID),
		TenantID:   strings.TrimSpace(*tenantID),
		Kind:       "rss",
		LeaseOwner: "manual-worldmonitor-eval",
	}); err != nil {
		log.Fatal(err)
	}
}
