package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		docIDs      = flag.String("docs", "", "Comma-separated list of document IDs to check")
		tenantID    = flag.String("tenant", "", "Tenant ID (required)")
		projectID   = flag.String("project", "", "GCP Project ID")
		credentials = flag.String("credentials", "", "Path to GCP credentials JSON file (optional, uses default if empty)")
	)
	flag.Parse()

	if *docIDs == "" {
		log.Fatal("--docs flag is required")
	}
	if *projectID == "" {
		log.Fatal("--project flag is required")
	}

	docIDList := strings.Split(*docIDs, ",")
	for i := range docIDList {
		docIDList[i] = strings.TrimSpace(docIDList[i])
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
	docsRepo := repo.NewDocumentsRepository(fsClient)

	// Auto-detect tenantID from first CoreDoc if not provided
	detectedTenantID := *tenantID
	if detectedTenantID == "" && len(docIDList) > 0 {
		firstDoc, err := coreDocsRepo.Get(ctx, docIDList[0])
		if err == nil && firstDoc != nil {
			detectedTenantID = firstDoc.TenantID
			log.Printf("Auto-detected tenantID from CoreDoc %s: %s", docIDList[0], detectedTenantID)
		}
	}
	if detectedTenantID == "" {
		log.Fatal("--tenant flag is required (could not auto-detect from CoreDoc)")
	}

	// Initialize Vertex AI Search client (if configured)
	// Vertex AI Search uses OAuth2 (Application Default Credentials), not API key
	// Pass empty string for apiKey to force OAuth2 authentication
	vertexLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexLocation == "" {
		vertexLocation = "global"
	}
	vertexDataStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	var vertexClient *ai.VertexAISearchClient
	if vertexDataStoreID != "" {
		// Use OAuth2 (Application Default Credentials) - pass empty string for apiKey
		vertexClient = ai.NewVertexAISearchClient(*projectID, vertexLocation, vertexDataStoreID, "")
		log.Printf("Vertex AI Search client initialized: project=%s, location=%s, dataStore=%s (using OAuth2)", *projectID, vertexLocation, vertexDataStoreID)
	}

	// Check each document
	results := make([]map[string]interface{}, 0, len(docIDList))
	for _, docID := range docIDList {
		if docID == "" {
			continue
		}
		result := checkDocument(ctx, docID, detectedTenantID, coreDocsRepo, docsRepo, vertexClient)
		results = append(results, result)
	}

	// Output results as JSON
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal results: %v", err)
	}
	fmt.Println(string(output))
}

func checkDocument(
	ctx context.Context,
	docID string,
	tenantID string,
	coreDocsRepo *repo.CoreDocsRepo,
	docsRepo *repo.DocumentsRepository,
	vertexClient *ai.VertexAISearchClient,
) map[string]interface{} {
	result := map[string]interface{}{
		"docId": docID,
	}

	// 1. Check CoreDoc
	coreDoc, err := coreDocsRepo.Get(ctx, docID)
	if err != nil {
		result["error"] = fmt.Sprintf("Failed to get CoreDoc: %v", err)
		result["coreDocExists"] = false
		return result
	}
	result["coreDocExists"] = true
	result["coreDoc"] = map[string]interface{}{
		"id":               coreDoc.ID,
		"filename":         coreDoc.Filename,
		"contentType":      coreDoc.ContentType,
		"projectId":        coreDoc.ProjectID,
		"tenantId":         coreDoc.TenantID,
		"parseStatus":      coreDoc.ParseStatus,
		"hasExtractedText": coreDoc.ExtractedText != nil && *coreDoc.ExtractedText != "",
		"extractedTextLength": func() int {
			if coreDoc.ExtractedText != nil {
				return len(*coreDoc.ExtractedText)
			}
			return 0
		}(),
		"storagePath": coreDoc.StoragePath,
		"docType":     coreDoc.DocType,
	}

	// 2. Check if Document exists in rag_documents
	var ragDoc *models.Document
	var ragDocErr error
	if tenantID != "" {
		ragDoc, ragDocErr = docsRepo.GetByCoreDocID(ctx, tenantID, docID)
	} else {
		ragDocErr = fmt.Errorf("tenantID not provided, skipping RAG document check")
	}
	if ragDocErr != nil {
		result["ragDocumentExists"] = false
		result["ragDocumentError"] = ragDocErr.Error()
	} else if ragDoc != nil {
		result["ragDocumentExists"] = true
		result["ragDocument"] = map[string]interface{}{
			"id":             ragDoc.ID,
			"sourceId":       ragDoc.SourceID,
			"externalId":     ragDoc.ExternalID,
			"title":          ragDoc.Title,
			"status":         ragDoc.Status,
			"storagePath":    ragDoc.StoragePath,
			"hasFullText":    ragDoc.FullText != "",
			"fullTextLength": len(ragDoc.FullText),
			"attrs":          ragDoc.Attrs,
		}
	} else {
		result["ragDocumentExists"] = false
		result["ragDocumentError"] = "Document not found"
	}

	// 3. Determine which URI to check in Vertex AI Search
	var documentURI string
	var uriSource string
	if ragDoc != nil && ragDoc.StoragePath != nil && *ragDoc.StoragePath != "" {
		documentURI = *ragDoc.StoragePath
		uriSource = "rag_document"
	} else if coreDoc.StoragePath != nil && *coreDoc.StoragePath != "" {
		documentURI = *coreDoc.StoragePath
		uriSource = "core_doc"
	} else {
		result["vertexSearchCheck"] = map[string]interface{}{
			"skipped": true,
			"reason":  "no_storage_path",
		}
		return result
	}

	result["documentURI"] = documentURI
	result["uriSource"] = uriSource

	// 4. Check Vertex AI Search status
	if vertexClient != nil && vertexClient.IsConfigured() {
		indexed, err := vertexClient.CheckDocumentStatus(ctx, documentURI, tenantID)
		if err != nil {
			result["vertexSearchCheck"] = map[string]interface{}{
				"error": err.Error(),
			}
		} else {
			result["vertexSearchCheck"] = map[string]interface{}{
				"indexed": indexed,
				"uri":     documentURI,
			}
		}
	} else {
		result["vertexSearchCheck"] = map[string]interface{}{
			"skipped": true,
			"reason":  "vertex_search_not_configured",
		}
	}

	return result
}
