package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
		sourceID  = flag.String("source", "", "Source ID to check (optional)")
	)
	flag.Parse()

	ctx := context.Background()

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

	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())

	fmt.Printf("\n=== Checking sources and ingest history ===\n\n")

	if *sourceID != "" {
		// Check specific source
		ds, err := sourcesRepo.Get(ctx, *sourceID)
		if err != nil {
			log.Fatalf("Failed to get source: %v", err)
		}
		printSourceDetails(ds, ingestHistoryRepo, ctx)
	} else {
		// Check all RSS and Telegram sources
		kinds := []string{"rss", "telegram_channel"}
		sources, err := sourcesRepo.ListByKinds(ctx, kinds)
		if err != nil {
			log.Fatalf("Failed to list sources: %v", err)
		}

		for _, ds := range sources {
			printSourceDetails(ds, ingestHistoryRepo, ctx)
			fmt.Println()
		}
	}
}

func printSourceDetails(ds *models.DataSource, ingestHistoryRepo *repo.SourceIngestHistoryRepository, ctx context.Context) {
	fmt.Printf("Source: %s (%s)\n", ds.ID, ds.Kind)
	fmt.Printf("  DisplayName: %s\n", ds.DisplayName)
	fmt.Printf("  Status: %s\n", ds.Status)
	
	if ds.NextPollAt != nil {
		fmt.Printf("  ✅ NextPollAt: %s\n", ds.NextPollAt.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  ❌ NextPollAt: NOT SET\n")
	}
	
	if ds.LastPolledAt != nil {
		fmt.Printf("  ✅ LastPolledAt: %s\n", ds.LastPolledAt.Format("2006-01-02 15:04:05"))
		age := time.Since(*ds.LastPolledAt)
		fmt.Printf("    (age: %v)\n", age.Round(time.Minute))
	} else {
		fmt.Printf("  ❌ LastPolledAt: NOT SET\n")
	}

	// Check ingest history
	history, err := ingestHistoryRepo.ListBySource(ctx, ds.TenantID, ds.ID, 5)
	if err == nil && len(history) > 0 {
		fmt.Printf("  Recent ingest history (%d entries):\n", len(history))
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
		fmt.Printf("  ⚠️  No ingest history found\n")
	}
}

