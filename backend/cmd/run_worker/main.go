package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
	)
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("GCP_PROJECT_ID environment variable or --project flag is required")
		}
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Load Horizon taxonomy from Firestore and keep it cached (source of truth for normalization).
	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient.Client(), "v1", 10*time.Minute)
	if err := taxonomy.RefreshRuntimeTaxonomy(ctx); err != nil {
		logger := log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile)
		logger.Printf("WARNING: failed to load horizon taxonomy: %v", err)
	}
	taxonomy.StartRuntimeTaxonomyAutoRefresh(ctx, 5*time.Minute, func(err error) {
		if err != nil {
			logger := log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile)
			logger.Printf("WARNING: horizon taxonomy refresh failed: %v", err)
		}
	})

	// Load market segment normalization dictionary from Firestore.
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
	if err := taxonomy.RefreshMarketSegmentDictionary(ctx); err != nil {
		logger := log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile)
		logger.Printf("WARNING: failed to load market segment dictionary: %v", err)
	}
	taxonomy.StartMarketSegmentDictionaryAutoRefresh(ctx, 10*time.Minute, func(err error) {
		if err != nil {
			logger := log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile)
			logger.Printf("WARNING: market segment dictionary refresh failed: %v", err)
		}
	})

	logger := log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile)

	// Initialize repositories
	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	chunksRepo := repo.NewChunksRepository(fsClient.Client())
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())
	ontologyFactsRepo := repo.NewOntologyFactsRepository(fsClient.Client())

	// Create publisher
	publisher, err := realtime.NewPublisher(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Publisher: %v", err)
	}

	// Create AI executor (minimal for embeddings)
	exec := ai.NewAITaskExecutor(nil, nil)

	// Create crawler
	wc := crawler.NewWebCrawler()

	// Create Telegram client
	tgBase := strings.TrimSpace(os.Getenv("TELEGRAM_SERVICE_URL"))
	if tgBase == "" {
		logger.Printf("WARNING: TELEGRAM_SERVICE_URL is not set - Telegram features will be disabled")
	}
	tgClient := telegram.NewClient(tgBase)

	// Create orchestrator
	orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, nil, sourcesRepo, publisher, logger)
	orchestrator = orchestrator.WithSourceProjects(sourceProjectsRepo)
	orchestrator = orchestrator.WithRAG(prov.NewRegistry(), docsRepo, chunksRepo)

	// Create embedding provider
	emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
	orchestrator = orchestrator.WithEmbedding(emb)
	orchestrator = orchestrator.WithIngestHistory(ingestHistoryRepo)
	orchestrator = orchestrator.WithHorizonMonitorPipeline(
		discovery.NewHorizonMonitorPipeline(
			ontologyFactsRepo,
			exec,
			nil,
			nil,
			nil,
			nil,
			logger,
		),
	)

	// Create poller
	psClient := publisher.Client()
	poller := discovery.NewSourcePoller(psClient, "source.poll", orchestrator, logger)

	fmt.Printf("Starting source poller worker at %s...\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Press Ctrl+C to stop\n\n")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := poller.Start(ctx); err != nil {
			errChan <- err
		}
	}()

	// Wait for signal or error
	select {
	case <-sigChan:
		fmt.Printf("\nShutting down...\n")
		cancel()
		time.Sleep(2 * time.Second)
	case err := <-errChan:
		log.Fatalf("Worker error: %v", err)
	}

	fmt.Printf("Worker stopped\n")
}
