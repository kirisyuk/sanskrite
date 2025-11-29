package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
		sourceID  = flag.String("source", "", "Source ID to test")
	)
	flag.Parse()

	ctx := context.Background()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("GCP_PROJECT_ID environment variable or --project flag is required")
		}
	}

	if *sourceID == "" {
		log.Fatal("--source flag is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	logger := log.New(os.Stdout, "[test-worker] ", log.LstdFlags|log.Lshortfile)

	// Initialize repositories
	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	chunksRepo := repo.NewChunksRepository(fsClient.Client())
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())

	// Get source to determine tenantID and kind
	ds, err := sourcesRepo.Get(ctx, *sourceID)
	if err != nil {
		log.Fatalf("Failed to get source: %v", err)
	}

	// Get tenantID from active project links
	tenantID := ds.TenantID
	if tenantID == "" {
		activeStatus := "active"
		links, err := sourceProjectsRepo.ListBySource(ctx, "", *sourceID, &activeStatus)
		if err == nil && len(links) > 0 {
			tenantID = links[0].TenantID
		}
	}

	// Create publisher
	publisher, err := realtime.NewPublisher(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Publisher: %v", err)
	}

	// Create AI executor with model manager
	modelManager := ai.NewAIModelManager()
	aiServices := []ai.AIService{}
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		aiServices = append(aiServices, ai.NewOpenAIService(openaiKey, modelManager))
	}
	exec := ai.NewAITaskExecutor(aiServices, modelManager)

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

	fmt.Printf("Testing ingest for source: %s (kind: %s, tenantID: %s)\n", *sourceID, ds.Kind, tenantID)
	fmt.Printf("Before processing:\n")
	fmt.Printf("  NextPollAt: %v\n", ds.NextPollAt)
	fmt.Printf("  LastPolledAt: %v\n", ds.LastPolledAt)
	fmt.Println()

	// Process ingest directly
	ctxP, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var pollErr error
	if ds.Kind == "telegram_channel" && ds.Details.ChannelUsername != nil {
		last := 0
		if ds.Details.LastProcessedMessageID != nil {
			last = *ds.Details.LastProcessedMessageID
		}
		fmt.Printf("Calling IngestTelegram for @%s (lastMessageID: %d)...\n\n", *ds.Details.ChannelUsername, last)
		pollErr = orchestrator.IngestTelegram(ctxP, tenantID, *sourceID, *ds.Details.ChannelUsername, last)
		if pollErr != nil {
			fmt.Printf("IngestTelegram error: %v\n", pollErr)
		} else {
			fmt.Printf("IngestTelegram completed successfully\n")
		}
	}

	// Re-fetch source to see updates
	dsAfter, err := sourcesRepo.Get(ctx, *sourceID)
	if err != nil {
		log.Fatalf("Failed to re-fetch source: %v", err)
	}

	fmt.Printf("\nAfter processing:\n")
	if dsAfter.NextPollAt != nil {
		fmt.Printf("  ✅ NextPollAt: %s\n", dsAfter.NextPollAt.Format(time.RFC3339))
	} else {
		fmt.Printf("  ❌ NextPollAt: NOT SET\n")
	}
	if dsAfter.LastPolledAt != nil {
		fmt.Printf("  ✅ LastPolledAt: %s\n", dsAfter.LastPolledAt.Format(time.RFC3339))
	} else {
		fmt.Printf("  ❌ LastPolledAt: NOT SET\n")
	}

	// Check ingest history
	history, err := ingestHistoryRepo.ListBySource(ctx, tenantID, *sourceID, 3)
	if err == nil && len(history) > 0 {
		fmt.Printf("\n  Recent ingest history:\n")
		for _, h := range history {
			status := h.Status
			if h.Error != nil {
				status = fmt.Sprintf("%s (error: %s)", status, *h.Error)
			}
			fmt.Printf("    - %s: %s, docs: %d/%d\n",
				h.StartedAt.Format("2006-01-02 15:04:05"),
				status,
				h.DocumentsCreated,
				h.DocumentsProcessed)
		}
	} else {
		fmt.Printf("\n  ⚠️  No ingest history found\n")
	}

	fmt.Printf("\n✅ Test completed!\n")
}
