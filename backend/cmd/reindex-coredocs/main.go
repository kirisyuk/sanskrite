package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID    = flag.String("project", "", "GCP Project ID")
		tenantID     = flag.String("tenant", "", "Tenant ID (optional, will process all tenants if not specified)")
		projectDocID = flag.String("project-id", "", "Specific project ID to process (optional)")
		dryRun       = flag.Bool("dry-run", false, "Dry run mode (don't actually reindex)")
		credentials  = flag.String("credentials", "", "Path to GCP credentials JSON file (optional)")
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

	// Initialize repositories
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient)

	// Query CoreDocs
	var coreDocs []*models.CoreDoc
	if *projectDocID != "" {
		// Get docs for specific project
		if *tenantID == "" {
			log.Fatal("--tenant is required when --project-id is specified")
		}
		docs, err := coreDocsRepo.ListByProject(ctx, *tenantID, *projectDocID, 0, nil)
		if err != nil {
			log.Fatalf("Failed to list CoreDocs: %v", err)
		}
		coreDocs = docs
	} else {
		// Get all CoreDocs for tenant (without project filter)
		if *tenantID == "" {
			log.Fatal("--tenant is required when --project-id is not specified")
		}
		log.Printf("Listing all CoreDocs for tenant: %s", *tenantID)
		// Query directly from Firestore collection (without OrderBy to avoid index requirement)
		col := fsClient.Collection("core_docs")
		q := col.Where("tenantId", "==", *tenantID)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("Failed to list CoreDocs: %v", err)
		}
		coreDocs = make([]*models.CoreDoc, 0, len(docs))
		for _, d := range docs {
			var cd models.CoreDoc
			if err := d.DataTo(&cd); err != nil {
				log.Printf("Warning: failed to parse CoreDoc %s: %v", d.Ref.ID, err)
				continue
			}
			coreDocs = append(coreDocs, &cd)
		}
		log.Printf("Found %d CoreDocs for tenant", len(coreDocs))
	}

	log.Printf("Found %d CoreDocs to check", len(coreDocs))

	// Check each CoreDoc
	needsReindexing := 0
	alreadyIndexed := 0
	noText := 0
	dynamicArtifacts := 0

	for _, cd := range coreDocs {
		// Check if it has ExtractedText
		if cd.ExtractedText == nil || strings.TrimSpace(*cd.ExtractedText) == "" {
			noText++
			continue
		}

		// Skip dynamic artifacts
		if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
			dynamicArtifacts++
			continue
		}

		// Check if Document exists and has StoragePath
		docsRepo := repo.NewDocumentsRepository(fsClient)
		ragDoc, err := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
		if err == nil && ragDoc != nil && ragDoc.StoragePath != nil && *ragDoc.StoragePath != "" {
			alreadyIndexed++
			log.Printf("✓ %s (%s) - already indexed (has StoragePath)", cd.ID, cd.Filename)
		} else {
			needsReindexing++
			if ragDoc != nil && (ragDoc.StoragePath == nil || *ragDoc.StoragePath == "") {
				log.Printf("✗ %s (%s) - needs reindexing (missing StoragePath)", cd.ID, cd.Filename)
			} else {
				log.Printf("✗ %s (%s) - needs reindexing", cd.ID, cd.Filename)
			}
			if !*dryRun {
				// Will be indexed below
			}
		}
	}

	log.Printf("\nSummary:")
	log.Printf("  Total CoreDocs: %d", len(coreDocs))
	log.Printf("  Already indexed: %d", alreadyIndexed)
	log.Printf("  Needs reindexing: %d", needsReindexing)
	log.Printf("  No extracted text: %d", noText)
	log.Printf("  Dynamic artifacts (skipped): %d", dynamicArtifacts)

	if *dryRun {
		if needsReindexing > 0 {
			log.Printf("\nRun with --dry-run=false to actually reindex documents")
		}
		return
	}

	// Initialize GCS client for document storage
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	docsRepo := repo.NewDocumentsRepository(fsClient)

	// Configure Vertex AI Search sync if available
	vertexSearchLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexSearchLocation == "" {
		vertexSearchLocation = "global"
	}
	vertexSearchDataStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	ragDocsBucket := os.Getenv("RAG_DOCUMENTS_BUCKET")

	// Configure GCS storage for documents (required for StoragePath creation)
	if ragDocsBucket != "" && gcsClient != nil {
		docsRepo = docsRepo.WithStorage(gcsClient, ragDocsBucket)
		log.Printf("RAG documents storage enabled: bucket=%s", ragDocsBucket)
	} else {
		log.Printf("Warning: GCS storage not configured - StoragePath will not be created for documents")
		log.Printf("  Required env var: RAG_DOCUMENTS_BUCKET")
	}

	var vertexSearchClient *ai.VertexAISearchClient
	// Use OAuth2 (Application Default Credentials) for Vertex AI Search
	// Pass empty string for apiKey to force OAuth2 authentication
	// Note: GEMINI_API_KEY may be set for other services, but Vertex AI Search requires OAuth2
	if vertexSearchDataStoreID != "" && ragDocsBucket != "" {
		vertexSearchClient = ai.NewVertexAISearchClient(*projectID, vertexSearchLocation, vertexSearchDataStoreID, "")
		// Configure automatic sync with Vertex AI Search
		docsRepo = docsRepo.WithVertexSearchSync(func(ctx context.Context, tenantID string) (string, error) {
			gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tenantID)
			importReq := &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "INCREMENTAL",
			}
			operationName, err := vertexSearchClient.ImportDocuments(ctx, importReq)
			if err != nil {
				log.Printf("[VertexAISearch] Failed to trigger sync for tenant %s: %v", tenantID, err)
				return "", err
			}
			log.Printf("[VertexAISearch] Sync triggered for tenant %s: operation=%s", tenantID, operationName)

			// Start tracking operation asynchronously
			go func(opName string, tid string) {
				trackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				if err := ai.TrackImportOperation(trackCtx, vertexSearchClient, docsRepo, opName, tid, 5*time.Minute); err != nil {
					log.Printf("[VertexAISearch] Failed to track operation %s: %v", opName, err)
				}
			}(operationName, tenantID)

			return operationName, nil
		})
		log.Printf("Vertex AI Search sync configured: bucket=%s, location=%s, dataStore=%s", ragDocsBucket, vertexSearchLocation, vertexSearchDataStoreID)
	} else {
		log.Printf("Warning: Vertex AI Search not configured - documents will be created but not synced to Vertex AI Search")
		log.Printf("  Required env vars: VERTEX_SEARCH_DATA_STORE_ID, RAG_DOCUMENTS_BUCKET")
		log.Printf("  Note: Will use Application Default Credentials (OAuth2) for authentication")
	}

	log.Printf("\nStarting reindexing...")
	reindexed := 0
	failed := 0

	for _, cd := range coreDocs {
		// Skip if already indexed or doesn't meet criteria
		if cd.ExtractedText == nil || strings.TrimSpace(*cd.ExtractedText) == "" {
			continue
		}
		if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
			continue
		}
		ragDoc, _ := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
		// Reindex if Document doesn't exist or doesn't have StoragePath
		if ragDoc != nil && ragDoc.StoragePath != nil && *ragDoc.StoragePath != "" {
			continue // Already indexed with StoragePath
		}

		// Reindex
		if err := indexCoreDoc(ctx, cd, cd.TenantID, docsRepo); err != nil {
			log.Printf("✗ Failed to reindex %s (%s): %v", cd.ID, cd.Filename, err)
			failed++
		} else {
			log.Printf("✓ Reindexed %s (%s)", cd.ID, cd.Filename)
			reindexed++
		}
	}

	log.Printf("\nReindexing complete:")
	log.Printf("  Reindexed: %d", reindexed)
	log.Printf("  Failed: %d", failed)
}

// indexCoreDoc indexes a CoreDoc by creating/updating Document (uploads to GCS, triggers Vertex sync)
func indexCoreDoc(ctx context.Context, cd *models.CoreDoc, tenantID string, docsRepo *repo.DocumentsRepository) error {
	if docsRepo == nil {
		return fmt.Errorf("document indexing not configured (DocumentsRepository required)")
	}
	if cd.ExtractedText == nil || strings.TrimSpace(*cd.ExtractedText) == "" {
		return fmt.Errorf("no text to index")
	}
	// Skip indexing for dynamic artifacts
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
		return fmt.Errorf("dynamic artifacts are not indexed")
	}

	// Virtual SourceID for project documents
	virtualSourceID := fmt.Sprintf("project:%s", cd.ProjectID)
	externalID := fmt.Sprintf("coredoc:%s", cd.ID)
	text := strings.TrimSpace(*cd.ExtractedText)

	// Check if Document already exists
	existingDoc, _ := docsRepo.GetByCoreDocID(ctx, tenantID, cd.ID)
	var docID string
	now := time.Now().UTC()

	if existingDoc != nil {
		// Update existing document
		existingDoc.FullText = text
		existingDoc.Title = cd.Filename
		existingDoc.ContentType = cd.ContentType
		existingDoc.DocumentType = "coredoc"
		existingDoc.DocumentPath = fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID)
		existingDoc.ContentHash = repo.HashContent(text)
		existingDoc.UpdatedAt = now
		existingDoc.Status = "new"
		if err := docsRepo.Upsert(ctx, existingDoc); err != nil {
			return fmt.Errorf("failed to update document: %w", err)
		}
		docID = existingDoc.ID
	} else {
		// Create new document
		doc := &models.Document{
			TenantID:     tenantID,
			SourceID:     virtualSourceID,
			ExternalID:   externalID,
			Title:        cd.Filename,
			DocumentPath: fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID),
			FullText:     text,
			Status:       "new",
			ContentType:  cd.ContentType,
			DocumentType: "coredoc",
			ContentHash:  repo.HashContent(text),
			IngestedAt:   now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Attrs: map[string]any{
				"coreDocId":    cd.ID,
				"projectId":    cd.ProjectID,
				"documentType": "coredoc",
			},
		}
		id, err := docsRepo.Create(ctx, doc)
		if err != nil {
			return fmt.Errorf("failed to create document: %w", err)
		}
		docID = id
	}

	// Mark document as indexed (GCS upload + Vertex sync handled by docsRepo.Create/Upsert)
	if doc, _ := docsRepo.GetByID(ctx, docID); doc != nil {
		doc.Status = "indexed"
		_ = docsRepo.Upsert(ctx, doc)
	}

	return nil
}
