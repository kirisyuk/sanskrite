package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
		sourceID  = flag.String("source", "", "Source ID")
		tenantID  = flag.String("tenant", "", "Tenant ID")
		limit     = flag.Int("limit", 10, "Limit number of history entries")
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

	if *tenantID == "" {
		log.Fatal("--tenant flag is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())

	fmt.Printf("Checking ingest history for source: %s (tenant: %s)\n\n", *sourceID, *tenantID)

	history, err := ingestHistoryRepo.ListBySource(ctx, *tenantID, *sourceID, *limit)
	if err != nil {
		log.Fatalf("Failed to list ingest history: %v", err)
	}

	fmt.Printf("Found %d history entries:\n\n", len(history))
	for i, h := range history {
		fmt.Printf("%d. StartedAt: %s\n", i+1, h.StartedAt.Format("2006-01-02 15:04:05"))
		if h.CompletedAt != nil {
			fmt.Printf("   CompletedAt: %s\n", h.CompletedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("   Status: %s\n", h.Status)
		if h.Error != nil {
			fmt.Printf("   Error: %s\n", *h.Error)
		}
		fmt.Printf("   DocumentsProcessed: %d\n", h.DocumentsProcessed)
		fmt.Printf("   DocumentsCreated: %d\n", h.DocumentsCreated)
		if h.DurationMs != nil {
			fmt.Printf("   Duration: %d ms\n", *h.DurationMs)
		}
		fmt.Println()
	}
}

