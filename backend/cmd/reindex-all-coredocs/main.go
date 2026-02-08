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

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project ID")
	tenantID := flag.String("tenant", "", "Tenant ID (optional, indexes all tenants if not specified)")
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("Project ID is required. Use -project flag or set GCP_PROJECT_ID env var")
		}
	}

	ctx := context.Background()

	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	docsRepo := repo.NewDocumentsRepository(fsClient)
	chunksRepo := repo.NewChunksRepository(fsClient)

	// Initialize embedding provider - try to get API keys from GCP Secret Manager
	modelManager := ai.NewAIModelManager()
	aiServices := []ai.AIService{}

	// Try OpenAI first
	openaiAPIKey := gcp.GetEnvOrSecret(ctx, *projectID, "OPENAI_API_KEY", "openai_api_key")
	if openaiAPIKey != "" {
		aiServices = append(aiServices, ai.NewOpenAIService(openaiAPIKey, modelManager))
		log.Printf("Using OpenAI for embeddings")
	}

	// Try Gemini/Vertex AI as fallback
	if len(aiServices) == 0 {
		geminiAPIKey := gcp.GetEnvOrSecret(ctx, *projectID, "GEMINI_API_KEY", "gemini_api_key")
		if geminiAPIKey != "" {
			aiServices = append(aiServices, ai.NewGeminiService(geminiAPIKey, modelManager))
			log.Printf("Using Vertex AI (Gemini) for embeddings")
		}
	}

	if len(aiServices) == 0 {
		log.Fatal("No API keys found. Set OPENAI_API_KEY or GEMINI_API_KEY env var, or ensure secrets exist in GCP Secret Manager")
	}

	exec := ai.NewAITaskExecutor(aiServices, modelManager)
	emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")

	// Query all CoreDocs
	var docs []*firestore.DocumentSnapshot
	if *tenantID != "" {
		docs, err = fsClient.Collection("core_docs").Where("tenantId", "==", *tenantID).Documents(ctx).GetAll()
	} else {
		docs, err = fsClient.Collection("core_docs").Documents(ctx).GetAll()
	}
	if err != nil {
		log.Fatalf("Failed to query CoreDocs: %v", err)
	}

	log.Printf("Found %d CoreDocs to check", len(docs))

	indexed := 0
	skipped := 0
	failed := 0

	for _, docSnap := range docs {
		var cd models.CoreDoc
		if err := docSnap.DataTo(&cd); err != nil {
			log.Printf("Failed to parse CoreDoc %s: %v", docSnap.Ref.ID, err)
			failed++
			continue
		}

		// Skip dynamic artifacts (they are generated, not indexed)
		if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
			log.Printf("Skipping dynamic artifact %s (%s)", cd.ID, cd.Filename)
			skipped++
			continue
		}

		// Check if already indexed (has RAG Document)
		ragDoc, err := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
		if err == nil && ragDoc != nil {
			log.Printf("Document %s (%s) already indexed, skipping", cd.ID, cd.Filename)
			skipped++
			continue
		}

		// Index the document
		if err := indexCoreDoc(ctx, &cd, cd.TenantID, docsRepo, chunksRepo, emb); err != nil {
			log.Printf("Failed to index CoreDoc %s (%s): %v", cd.ID, cd.Filename, err)
			failed++
			continue
		}

		log.Printf("Indexed CoreDoc %s (%s)", cd.ID, cd.Filename)
		indexed++
	}

	log.Printf("\nIndexing complete:")
	log.Printf("  Indexed: %d", indexed)
	log.Printf("  Skipped: %d", skipped)
	log.Printf("  Failed:  %d", failed)
	log.Printf("  Total:   %d", len(docs))
}

// indexCoreDoc indexes a CoreDoc by creating/updating Document and Chunks with embeddings
func indexCoreDoc(ctx context.Context, cd *models.CoreDoc, tenantID string, docsRepo *repo.DocumentsRepository, chunksRepo *repo.ChunksRepository, emb ai.EmbeddingProvider) error {
	// Get full extracted text
	var text string
	if cd.ExtractedText != nil {
		text = *cd.ExtractedText
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("no text to index")
	}

	// Virtual SourceID for project documents
	virtualSourceID := fmt.Sprintf("project:%s", cd.ProjectID)
	externalID := fmt.Sprintf("coredoc:%s", cd.ID)

	// Check if Document already exists
	existingDoc, _ := docsRepo.GetByCoreDocID(ctx, tenantID, cd.ID)
	var docID string

	if existingDoc != nil {
		// Update existing document
		existingDoc.FullText = text
		existingDoc.Title = cd.Filename
		existingDoc.ContentType = cd.ContentType
		existingDoc.DocumentType = "coredoc"
		existingDoc.DocumentPath = fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID)
		existingDoc.ContentHash = repo.HashContent(text)
		if err := docsRepo.Upsert(ctx, existingDoc); err != nil {
			return fmt.Errorf("failed to update document: %w", err)
		}
		docID = existingDoc.ID
		// Delete old chunks before creating new ones
		_ = chunksRepo.DeleteByDoc(ctx, tenantID, docID)
	} else {
		// Create new document
		now := time.Now().UTC()
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

	// Chunk and embed
	parts := discovery.ChunkText(text, 1200)
	if len(parts) == 0 {
		return nil
	}

	vectors, err := emb.EmbedTexts(ctx, parts)
	if err != nil {
		return fmt.Errorf("failed to create embeddings: %w", err)
	}

	now := time.Now().UTC()
	for i := range parts {
		ch := &models.Chunk{
			TenantID:         tenantID,
			DocID:            docID,
			SourceID:         virtualSourceID,
			Position:         i,
			Text:             parts[i],
			Embedding:        vectors[i],
			EmbeddingModel:   emb.ModelID(),
			EmbeddingVersion: "v1",
			Attrs:            map[string]any{"documentType": "coredoc", "coreDocId": cd.ID},
			ContentHash:      repo.HashContent(parts[i]),
			CreatedAt:        now,
		}
		if _, err := chunksRepo.Create(ctx, ch); err != nil {
			return fmt.Errorf("failed to create chunk: %w", err)
		}
	}

	// Mark document as embedded
	if doc, _ := docsRepo.GetByID(ctx, docID); doc != nil {
		doc.Status = "embedded"
		_ = docsRepo.Upsert(ctx, doc)
	}

	return nil
}
