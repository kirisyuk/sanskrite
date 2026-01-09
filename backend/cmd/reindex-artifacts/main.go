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
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID    = flag.String("project", "", "GCP Project ID (or set GCP_PROJECT_ID)")
		tenantID     = flag.String("tenant", "", "Tenant ID filter (optional; if omitted, will scan all tenants)")
		projectDocID = flag.String("project-id", "", "Project ID filter (optional)")
		artifactID   = flag.String("artifact-id", "", "Specific artifact CoreDoc ID (optional)")
		dryRun       = flag.Bool("dry-run", false, "Dry run: don't index, just report")
		force        = flag.Bool("force", false, "Force reindex even if rag_document exists")
		limit        = flag.Int("limit", 0, "Limit number of artifacts processed (0 = no limit)")
		credentials  = flag.String("credentials", "", "Path to GCP credentials JSON file (optional; otherwise uses ADC)")
	)
	flag.Parse()

	if *projectID == "" {
		*projectID = strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	}
	if *projectID == "" {
		*projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if *projectID == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT)")
	}
	tenantFilter := strings.TrimSpace(*tenantID)
	projectFilter := strings.TrimSpace(*projectDocID)

	ctx := context.Background()

	// Firestore client (ADC by default; optionally via credentials file)
	var opts []option.ClientOption
	if strings.TrimSpace(*credentials) != "" {
		opts = append(opts, option.WithCredentialsFile(strings.TrimSpace(*credentials)))
	}
	fsClient, err := firestore.NewClient(ctx, *projectID, opts...)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Repositories
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient)
	docsRepo := repo.NewDocumentsRepository(fsClient)
	chunksRepo := repo.NewChunksRepository(fsClient)

	// Embeddings (prefer OpenAI; fallback to Gemini key, still via executor)
	modelManager := ai.NewAIModelManager()
	aiServices := []ai.AIService{}
	openaiAPIKey := gcp.GetEnvOrSecret(ctx, *projectID, "OPENAI_API_KEY", "openai_api_key")
	if strings.TrimSpace(openaiAPIKey) != "" {
		aiServices = append(aiServices, ai.NewOpenAIService(openaiAPIKey, modelManager))
		log.Printf("Embeddings provider: OpenAI")
	}
	if len(aiServices) == 0 {
		geminiAPIKey := gcp.GetEnvOrSecret(ctx, *projectID, "GEMINI_API_KEY", "gemini_api_key")
		if strings.TrimSpace(geminiAPIKey) != "" {
			aiServices = append(aiServices, ai.NewVertexAIService(geminiAPIKey, modelManager))
			log.Printf("Embeddings provider: Vertex AI (Gemini)")
		}
	}
	if len(aiServices) == 0 {
		log.Fatal("No embedding API keys found. Set OPENAI_API_KEY or GEMINI_API_KEY env var, or ensure secrets exist in GCP Secret Manager.")
	}
	exec := ai.NewAITaskExecutor(aiServices, modelManager)
	emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")

	// Optional: enable GCS storage for rag_documents (needed for Vertex AI Search import)
	gcsClient, gcsErr := gcp.NewGCSClient(ctx, *projectID)
	if gcsErr != nil {
		log.Printf("Warning: failed to create GCS client (StoragePath/Vertex sync may be disabled): %v", gcsErr)
	}
	ragDocsBucket := strings.TrimSpace(os.Getenv("RAG_DOCUMENTS_BUCKET"))
	if ragDocsBucket != "" && gcsClient != nil {
		docsRepo = docsRepo.WithStorage(gcsClient, ragDocsBucket)
		log.Printf("RAG documents storage enabled: bucket=%s", ragDocsBucket)
	} else {
		log.Printf("RAG documents storage NOT configured (set RAG_DOCUMENTS_BUCKET to enable StoragePath)")
	}

	// Optional: configure Vertex AI Search sync + operation tracking
	vertexSearchLocation := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
	if vertexSearchLocation == "" {
		vertexSearchLocation = "global"
	}
	vertexSearchDataStoreID := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"))
	if vertexSearchDataStoreID != "" && ragDocsBucket != "" {
		vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexSearchLocation, vertexSearchDataStoreID, "")
		docsRepo = docsRepo.WithVertexSearchSync(func(ctx context.Context, tid string) (string, error) {
			gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tid)
			importReq := &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "INCREMENTAL",
			}
			opName, err := vertexSearchClient.ImportDocuments(ctx, importReq)
			if err != nil {
				log.Printf("[VertexAISearch] Failed to trigger sync for tenant %s: %v", tid, err)
				return "", err
			}
			log.Printf("[VertexAISearch] Sync triggered for tenant %s: operation=%s", tid, opName)

			// Track operation asynchronously; marks VertexSearchIndexedAt for tenant docs
			go func(operationName string, tenant string) {
				trackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				if err := ai.TrackImportOperation(trackCtx, vertexSearchClient, docsRepo, operationName, tenant, 5*time.Minute); err != nil {
					log.Printf("[VertexAISearch] Failed to track operation %s: %v", operationName, err)
				}
			}(opName, tid)

			return opName, nil
		})
		log.Printf("Vertex AI Search sync configured: location=%s dataStore=%s bucket=%s", vertexSearchLocation, vertexSearchDataStoreID, ragDocsBucket)
	} else {
		log.Printf("Vertex AI Search sync NOT configured (need VERTEX_SEARCH_DATA_STORE_ID + RAG_DOCUMENTS_BUCKET).")
	}

	// Projects handler (we reuse IndexCoreDoc implementation, including artifact section assembly)
	logger := log.New(os.Stdout, "[reindex-artifacts] ", log.LstdFlags)
	projectsRepo := repo.NewProjectsRepository(fsClient)
	ph := httphandlers.NewProjectsHandler(projectsRepo, logger).WithRAG(docsRepo, chunksRepo, emb)

	// Collect artifacts to process
	artifacts := make([]*models.CoreDoc, 0)
	if strings.TrimSpace(*artifactID) != "" {
		cd, err := coreDocsRepo.Get(ctx, strings.TrimSpace(*artifactID))
		if err != nil || cd == nil {
			log.Fatalf("Artifact not found: id=%s err=%v", strings.TrimSpace(*artifactID), err)
		}
		if tenantFilter != "" && cd.TenantID != tenantFilter {
			log.Fatalf("Artifact tenant mismatch: artifact.tenantId=%s expected=%s", cd.TenantID, tenantFilter)
		}
		artifacts = append(artifacts, cd)
	} else {
		// Avoid composite indexes: query by tenantId only (if provided), otherwise scan all and filter in memory.
		var snaps []*firestore.DocumentSnapshot
		var err error
		if tenantFilter != "" {
			snaps, err = fsClient.Collection("core_docs").Where("tenantId", "==", tenantFilter).Documents(ctx).GetAll()
		} else {
			snaps, err = fsClient.Collection("core_docs").Documents(ctx).GetAll()
		}
		if err != nil {
			log.Fatalf("Failed to query CoreDocs: %v", err)
		}
		for _, snap := range snaps {
			var cd models.CoreDoc
			if err := snap.DataTo(&cd); err != nil {
				continue
			}
			if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
				if projectFilter != "" && cd.ProjectID != projectFilter {
					continue
				}
				artifacts = append(artifacts, &cd)
			}
		}
	}

	if *limit > 0 && len(artifacts) > *limit {
		artifacts = artifacts[:*limit]
	}

	log.Printf("Found %d dynamic artifacts to process (tenantFilter=%s projectFilter=%s force=%v dryRun=%v)", len(artifacts), tenantFilter, projectFilter, *force, *dryRun)

	processed := 0
	indexed := 0
	skipped := 0
	failed := 0
	noContent := 0

	for _, cd := range artifacts {
		if cd == nil {
			continue
		}
		processed++

		// Skip artifacts without structure (nothing to assemble/index)
		if cd.ArtifactConfig == nil || len(cd.ArtifactConfig.Structure) == 0 {
			noContent++
			log.Printf("Skipping artifact %s (%s): no artifactConfig.structure", cd.ID, cd.Filename)
			continue
		}

		// Enforce tenant filter if provided
		if tenantFilter != "" && cd.TenantID != tenantFilter {
			skipped++
			log.Printf("Skipping artifact %s (%s): tenant mismatch (%s != %s)", cd.ID, cd.Filename, cd.TenantID, tenantFilter)
			continue
		}

		if !*force {
			// If rag document exists, consider indexed (avoid re-embedding costs)
			ragDoc, err := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
			if err == nil && ragDoc != nil {
				skipped++
				log.Printf("Skipping artifact %s (%s): rag_document exists (use --force to reindex)", cd.ID, cd.Filename)
				continue
			}
		}

		if *dryRun {
			log.Printf("[dry-run] Would index artifact %s (%s)", cd.ID, cd.Filename)
			continue
		}

		// Give each artifact a reasonable timeout (embeddings can be slow)
		itemCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err := ph.IndexCoreDoc(itemCtx, cd.TenantID, cd)
		cancel()
		if err != nil {
			failed++
			log.Printf("✗ Failed to index artifact %s (%s): %v", cd.ID, cd.Filename, err)
			continue
		}

		indexed++
		log.Printf("✓ Indexed artifact %s (%s)", cd.ID, cd.Filename)
	}

	log.Printf("\nDone.")
	log.Printf("  processed:  %d", processed)
	log.Printf("  indexed:    %d", indexed)
	log.Printf("  skipped:    %d", skipped)
	log.Printf("  noContent:  %d", noContent)
	log.Printf("  failed:     %d", failed)
}

