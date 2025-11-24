package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func main() {
	var (
		projectID   = flag.String("project", "", "GCP Project ID")
		tenantID    = flag.String("tenant", "", "Tenant ID (required)")
		credentials = flag.String("credentials", "", "Path to GCP credentials JSON file (optional)")
	)
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project flag is required")
	}
	if *tenantID == "" {
		log.Fatal("--tenant flag is required")
	}

	ctx := context.Background()

	// Initialize Firestore client
	var opts []option.ClientOption
	if *credentials != "" {
		opts = append(opts, option.WithCredentialsFile(*credentials))
	}
	fsClient, err := firestore.NewClient(ctx, *projectID, opts...)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Initialize Vertex AI Search client
	vertexSearchLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexSearchLocation == "" {
		vertexSearchLocation = "global"
	}
	vertexSearchDataStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	if vertexSearchDataStoreID == "" {
		vertexSearchDataStoreID = "rag-documents-store"
	}
	ragDocsBucket := os.Getenv("RAG_DOCUMENTS_BUCKET")
	if ragDocsBucket == "" {
		ragDocsBucket = fmt.Sprintf("%s-rag-documents", *projectID)
	}

	if vertexSearchDataStoreID == "" {
		log.Fatal("VERTEX_SEARCH_DATA_STORE_ID environment variable is required")
	}

	// Use OAuth2 (Application Default Credentials) for Vertex AI Search
	// Pass empty string for apiKey to force OAuth2 authentication
	// Note: GEMINI_API_KEY may be set for other services, but Vertex AI Search requires OAuth2
	vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexSearchLocation, vertexSearchDataStoreID, "")

	log.Printf("Syncing documents for tenant: %s", *tenantID)
	log.Printf("  Project: %s", *projectID)
	log.Printf("  Location: %s", vertexSearchLocation)
	log.Printf("  DataStore: %s", vertexSearchDataStoreID)
	log.Printf("  Bucket: %s", ragDocsBucket)

	// Trigger sync for tenant
	gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, *tenantID)
	importReq := &ai.ImportDocumentsRequest{
		GCSPath:            gcsPath,
		ReconciliationMode: "INCREMENTAL",
	}

	log.Printf("Importing documents from: %s", gcsPath)
	operationName, err := vertexSearchClient.ImportDocuments(ctx, importReq)
	if err != nil {
		log.Fatalf("Failed to trigger sync: %v", err)
	}

	log.Printf("✓ Sync triggered successfully")
	log.Printf("  Operation: %s", operationName)
	log.Printf("  Note: Documents will be available in Vertex AI Search within 1-2 minutes")
}
