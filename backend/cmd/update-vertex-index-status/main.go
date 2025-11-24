package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID   = flag.String("project", "", "GCP Project ID")
		tenantID    = flag.String("tenant", "", "Tenant ID (optional, will process all tenants if not specified)")
		credentials = flag.String("credentials", "", "Path to GCP credentials JSON file (optional)")
		forceSync   = flag.Bool("force-sync", false, "Force re-sync with Vertex AI Search (triggers new import operation)")
	)
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project flag is required")
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

	docsRepo := repo.NewDocumentsRepository(fsClient)

	// Initialize Vertex AI Search client if force-sync is enabled
	var vertexSearchClient *ai.VertexAISearchClient
	var ragDocsBucket string
	if *forceSync {
		vertexSearchLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
		if vertexSearchLocation == "" {
			vertexSearchLocation = "global"
		}
		vertexSearchDataStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
		ragDocsBucket = os.Getenv("RAG_DOCUMENTS_BUCKET")

		if vertexSearchDataStoreID != "" {
			vertexSearchClient = ai.NewVertexAISearchClient(*projectID, vertexSearchLocation, vertexSearchDataStoreID, "")
			log.Printf("Vertex AI Search client initialized for re-sync")
		} else {
			log.Fatal("VERTEX_SEARCH_DATA_STORE_ID environment variable is required for --force-sync")
		}
	}

	if *forceSync && vertexSearchClient != nil && ragDocsBucket != "" {
		// Determine tenants to sync
		var tenants []string
		if *tenantID != "" {
			tenants = []string{*tenantID}
			log.Printf("Processing tenant: %s", *tenantID)
		} else {
			// Query all unique tenant IDs (simple query without complex filters)
			col := docsRepo.Collection()
			docs, err := col.Documents(ctx).GetAll()
			if err != nil {
				log.Fatalf("Failed to query documents: %v", err)
			}
			tenantMap := make(map[string]bool)
			for _, docSnap := range docs {
				var d struct {
					TenantID    string `firestore:"tenantId"`
					StoragePath string `firestore:"storagePath"`
				}
				if err := docSnap.DataTo(&d); err == nil && d.TenantID != "" && d.StoragePath != "" {
					tenantMap[d.TenantID] = true
				}
			}
			for tid := range tenantMap {
				tenants = append(tenants, tid)
			}
			log.Printf("Found %d tenants with documents", len(tenants))
		}

		log.Printf("Triggering sync for %d tenant(s)...", len(tenants))
		for _, tid := range tenants {
			log.Printf("Syncing tenant: %s", tid)
			gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tid)
			importReq := &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "INCREMENTAL",
			}
			operationName, err := vertexSearchClient.ImportDocuments(ctx, importReq)
			if err != nil {
				log.Printf("Failed to trigger sync for tenant %s: %v", tid, err)
				continue
			}
			log.Printf("Sync triggered for tenant %s: operation=%s", tid, operationName)

			// Track operation and update status (synchronously to ensure completion)
			trackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			log.Printf("Tracking operation for tenant %s ...", tid)
			if err := ai.TrackImportOperation(trackCtx, vertexSearchClient, docsRepo, operationName, tid, 5*time.Minute); err != nil {
				log.Printf("Failed to track operation %s: %v", operationName, err)
				continue
			}
			log.Printf("Tenant %s documents marked as indexed after operation %s", tid, operationName)
		}

		log.Printf("Sync operations completed. Documents marked as indexed for processed tenants.")
	} else {
		// Just mark existing documents as indexed (assuming they are already indexed)
		// This is a one-time migration for documents that were indexed before we started tracking
		log.Printf("Marking existing documents as indexed (one-time migration)...")
		log.Printf("Note: This assumes documents are already indexed. Use --force-sync to trigger re-sync and verify.")

		if *tenantID != "" {
			if err := docsRepo.MarkTenantDocumentsAsIndexed(ctx, *tenantID); err != nil {
				log.Fatalf("Failed to mark documents as indexed: %v", err)
			}
			log.Printf("Marked documents as indexed for tenant: %s", *tenantID)
		} else {
			// Query all documents to get unique tenant IDs
			col := docsRepo.Collection()
			allDocs, err := col.Documents(ctx).GetAll()
			if err != nil {
				log.Fatalf("Failed to query documents: %v", err)
			}

			// Process all tenants
			tenants := make(map[string]bool)
			for _, docSnap := range allDocs {
				var d struct {
					TenantID    string `firestore:"tenantId"`
					StoragePath string `firestore:"storagePath"`
				}
				if err := docSnap.DataTo(&d); err == nil && d.TenantID != "" && d.StoragePath != "" {
					tenants[d.TenantID] = true
				}
			}

			for tenantID := range tenants {
				if err := docsRepo.MarkTenantDocumentsAsIndexed(ctx, tenantID); err != nil {
					log.Printf("Failed to mark documents for tenant %s: %v", tenantID, err)
					continue
				}
				log.Printf("Marked documents as indexed for tenant: %s", tenantID)
			}
		}
	}

	log.Printf("Done!")
}
