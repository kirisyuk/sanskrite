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
		limit     = flag.Int("limit", 10, "Limit number of documents")
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

	docsRepo := repo.NewDocumentsRepository(fsClient.Client())

	fmt.Printf("Checking documents for source: %s (tenant: %s)\n\n", *sourceID, *tenantID)

	// List documents by source
	docs, err := docsRepo.ListBySource(ctx, *tenantID, *sourceID, *limit)
	if err != nil {
		log.Fatalf("Failed to list documents: %v", err)
	}

	fmt.Printf("Found %d documents:\n\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("%d. Document ID: %s\n", i+1, doc.ID)
		fmt.Printf("   ExternalID: %s\n", doc.ExternalID)
		fmt.Printf("   Title: %s\n", doc.Title)
		fmt.Printf("   URL: %s\n", doc.URL)
		fmt.Printf("   Status: %s\n", doc.Status)
		fmt.Printf("   CreatedAt: %s\n", doc.CreatedAt.Format("2006-01-02 15:04:05"))
		if doc.OriginPublishedAt != nil {
			fmt.Printf("   OriginPublishedAt: %s\n", doc.OriginPublishedAt.Format("2006-01-02 15:04:05"))
		}
		if len(doc.FullText) > 100 {
			fmt.Printf("   Text preview: %s...\n", doc.FullText[:100])
		} else {
			fmt.Printf("   Text: %s\n", doc.FullText)
		}
		fmt.Println()
	}

	// Check chunks (simplified - just count)
	fmt.Printf("Note: Use Firestore console to check chunks for each document\n")
}

