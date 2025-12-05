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
)

func main() {
	var (
		projectID   = flag.String("project", "", "GCP Project ID")
		tenantID    = flag.String("tenant", "", "Tenant ID (optional, if not set processes all tenants)")
		credentials = flag.String("credentials", "", "Path to GCP credentials JSON file (optional)")
		waitBetween = flag.Duration("wait", 5*time.Second, "Wait time between tenant syncs")
	)
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			*projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}
	}
	if *projectID == "" {
		log.Fatal("--project flag or GCP_PROJECT_ID env var is required")
	}

	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	fmt.Fprintf(os.Stdout, "=== Vertex AI Search Sync Script ===\n")
	fmt.Fprintf(os.Stdout, "Project: %s\n\n", *projectID)
	os.Stdout.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Initialize Firestore client
	var opts []option.ClientOption
	if *credentials != "" {
		opts = append(opts, option.WithCredentialsFile(*credentials))
		log.Printf("Using credentials file: %s", *credentials)
	} else {
		log.Printf("Using Application Default Credentials")
	}

	log.Printf("Connecting to Firestore...")
	fsClient, err := firestore.NewClient(ctx, *projectID, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to create Firestore client: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: Run 'gcloud auth application-default login --project=%s'\n", *projectID)
		os.Exit(1)
	}
	defer fsClient.Close()
	log.Printf("✓ Firestore client created successfully")

	// Initialize Vertex AI Search client
	vertexSearchLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexSearchLocation == "" {
		vertexSearchLocation = "global"
	}
	vertexSearchDataStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	if vertexSearchDataStoreID == "" {
		vertexSearchDataStoreID = "rag-documents-store"
	}

	coreDocsBucket := os.Getenv("CORE_DOCS_BUCKET")
	if coreDocsBucket == "" {
		coreDocsBucket = fmt.Sprintf("%s-core-docs", *projectID)
	}
	ragDocsBucket := os.Getenv("RAG_DOCUMENTS_BUCKET")
	if ragDocsBucket == "" {
		ragDocsBucket = fmt.Sprintf("%s-rag-documents", *projectID)
	}

	if vertexSearchDataStoreID == "" {
		log.Fatal("VERTEX_SEARCH_DATA_STORE_ID environment variable is required")
	}

	// Use OAuth2 (Application Default Credentials) for Vertex AI Search
	vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexSearchLocation, vertexSearchDataStoreID, "")

	log.Printf("Location: %s", vertexSearchLocation)
	log.Printf("DataStore: %s", vertexSearchDataStoreID)
	log.Printf("Core Docs Bucket: %s", coreDocsBucket)
	log.Printf("RAG Documents Bucket: %s", ragDocsBucket)

	log.Printf("Initializing Vertex AI Search client...")

	// Get all tenants
	var tenants []string
	if *tenantID != "" {
		tenants = []string{*tenantID}
	} else {
		tenants = getAllTenants(ctx, fsClient)
		log.Printf("Found %d tenants", len(tenants))
	}

	totalSynced := 0
	totalErrors := 0

	// Sync CoreDocs bucket for each tenant
	log.Printf("\n=== Syncing CoreDocs bucket ===")
	for i, tenantID := range tenants {
		log.Printf("\n[%d/%d] Syncing tenant: %s (CoreDocs)", i+1, len(tenants), tenantID)
		gcsPath := fmt.Sprintf("gs://%s/%s/**", coreDocsBucket, tenantID)
		if err := syncTenant(ctx, vertexSearchClient, gcsPath, tenantID, "CoreDocs"); err != nil {
			log.Printf("Error syncing CoreDocs for tenant %s: %v", tenantID, err)
			totalErrors++
		} else {
			totalSynced++
		}
		if i < len(tenants)-1 {
			time.Sleep(*waitBetween)
		}
	}

	// Sync RAG Documents bucket for each tenant
	log.Printf("\n=== Syncing RAG Documents bucket ===")
	for i, tenantID := range tenants {
		log.Printf("\n[%d/%d] Syncing tenant: %s (RAG Documents)", i+1, len(tenants), tenantID)
		gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tenantID)
		if err := syncTenant(ctx, vertexSearchClient, gcsPath, tenantID, "RAG Documents"); err != nil {
			log.Printf("Error syncing RAG Documents for tenant %s: %v", tenantID, err)
			totalErrors++
		} else {
			totalSynced++
		}
		if i < len(tenants)-1 {
			time.Sleep(*waitBetween)
		}
	}

	fmt.Fprintf(os.Stdout, "\n=== Summary ===\n")
	fmt.Fprintf(os.Stdout, "Successfully synced: %d tenant-bucket combinations\n", totalSynced)
	fmt.Fprintf(os.Stdout, "Errors: %d\n", totalErrors)
	fmt.Fprintf(os.Stdout, "\nNote: Documents will be available in Vertex AI Search within 1-2 minutes\n")

	log.Printf("\n=== Summary ===")
	log.Printf("Successfully synced: %d tenant-bucket combinations", totalSynced)
	log.Printf("Errors: %d", totalErrors)
	log.Printf("\nNote: Documents will be available in Vertex AI Search within 1-2 minutes")

	if totalErrors > 0 {
		os.Exit(1)
	}
}

func getAllTenants(ctx context.Context, fsClient *firestore.Client) []string {
	tenantMap := make(map[string]bool)

	// Get tenants from projects collection
	log.Printf("Querying projects collection...")
	projectsCol := fsClient.Collection("projects")
	projectDocs, err := projectsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query projects collection: %v", err)
	} else {
		log.Printf("Found %d projects", len(projectDocs))
		for _, doc := range projectDocs {
			var p struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&p); err == nil && p.TenantID != "" {
				tenantMap[p.TenantID] = true
			}
		}
	}

	// Get tenants from core_docs collection
	log.Printf("Querying core_docs collection...")
	coreDocsCol := fsClient.Collection("core_docs")
	coreDocs, err := coreDocsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query core_docs collection: %v", err)
	} else {
		log.Printf("Found %d core_docs", len(coreDocs))
		for _, doc := range coreDocs {
			var cd struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&cd); err == nil && cd.TenantID != "" {
				tenantMap[cd.TenantID] = true
			}
		}
	}

	// Get tenants from rag_documents collection
	log.Printf("Querying rag_documents collection...")
	ragDocsCol := fsClient.Collection("rag_documents")
	ragDocs, err := ragDocsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query rag_documents collection: %v", err)
	} else {
		log.Printf("Found %d rag_documents", len(ragDocs))
		for _, doc := range ragDocs {
			var d struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&d); err == nil && d.TenantID != "" {
				tenantMap[d.TenantID] = true
			}
		}
	}

	tenants := make([]string, 0, len(tenantMap))
	for tid := range tenantMap {
		tenants = append(tenants, tid)
	}
	log.Printf("Total unique tenants found: %d", len(tenants))
	return tenants
}

func syncTenant(ctx context.Context, client *ai.VertexAISearchClient, gcsPath, tenantID, bucketType string) error {
	importReq := &ai.ImportDocumentsRequest{
		GCSPath:            gcsPath,
		ReconciliationMode: "INCREMENTAL",
	}

	log.Printf("  Importing from: %s", gcsPath)
	operationName, err := client.ImportDocuments(ctx, importReq)
	if err != nil {
		return fmt.Errorf("failed to trigger sync: %w", err)
	}

	log.Printf("  ✓ Sync triggered successfully")
	log.Printf("    Operation: %s", operationName)
	return nil
}

