package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP project ID")
		batchSize = flag.Int("batch", 100, "Batch size for migration")
		limit     = flag.Int("limit", 0, "Limit number of documents to migrate (0 = all)")
		docType   = flag.String("type", "all", "Document type: 'rag', 'core', or 'all'")
	)
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("Project ID is required (--project or GCP_PROJECT_ID env var)")
		}
	}

	ctx := context.Background()

	// Initialize Firestore
	opts := []option.ClientOption{}
	if creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); creds != "" {
		opts = append(opts, option.WithCredentialsFile(creds))
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore: %v", err)
	}
	defer fsClient.Close()

	firestoreClient := fsClient.Client()

	// Initialize BigQuery
	bigqueryDatasetID := os.Getenv("BIGQUERY_DATASET_ID")
	if bigqueryDatasetID == "" {
		bigqueryDatasetID = "horizon_documents"
	}

	bqClient, err := gcp.NewBigQueryClient(ctx, *projectID, bigqueryDatasetID, opts...)
	if err != nil {
		log.Fatalf("Failed to initialize BigQuery: %v", err)
	}
	defer bqClient.Close()

	log.Printf("Starting BigQuery migration for project: %s, dataset: %s", *projectID, bigqueryDatasetID)
	log.Printf("Batch size: %d, Limit: %d, Type: %s", *batchSize, *limit, *docType)

	var totalMigrated int

	// Migrate RAG documents
	if *docType == "all" || *docType == "rag" {
		log.Println("Migrating RAG documents...")
		migrated, err := migrateRAGDocuments(ctx, firestoreClient, bqClient, *batchSize, *limit)
		if err != nil {
			log.Printf("Error migrating RAG documents: %v", err)
		} else {
			log.Printf("Migrated %d RAG documents", migrated)
			totalMigrated += migrated
		}
	}

	// Migrate CoreDocs
	if *docType == "all" || *docType == "core" {
		log.Println("Migrating CoreDocs...")
		migrated, err := migrateCoreDocs(ctx, firestoreClient, bqClient, *batchSize, *limit)
		if err != nil {
			log.Printf("Error migrating CoreDocs: %v", err)
		} else {
			log.Printf("Migrated %d CoreDocs", migrated)
			totalMigrated += migrated
		}
	}

	log.Printf("Migration complete. Total migrated: %d", totalMigrated)
}

func migrateRAGDocuments(ctx context.Context, fsClient *firestore.Client, bqClient *gcp.BigQueryClient, batchSize, limit int) (int, error) {
	col := fsClient.Collection("rag_documents")
	var migrated int
	var lastDocID string

	for {
		// Fetch documents in batches using Firestore pagination
		q := col.OrderBy("createdAt", firestore.Asc).Limit(batchSize)
		if lastDocID != "" {
			// Get last document snapshot for cursor
			lastDoc, err := col.Doc(lastDocID).Get(ctx)
			if err == nil {
				q = q.StartAfter(lastDoc)
			}
		}

		iter := q.Documents(ctx)
		docs := []*models.Document{}
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return migrated, fmt.Errorf("failed to iterate documents: %w", err)
			}
			var d models.Document
			if err := doc.DataTo(&d); err != nil {
				continue
			}
			docs = append(docs, &d)
		}

		if len(docs) == 0 {
			break
		}

		// Prepare batch for BigQuery
		rows := make([]map[string]interface{}, 0, len(docs))
		for _, doc := range docs {
			storagePath := ""
			if doc.StoragePath != nil {
				storagePath = *doc.StoragePath
			}

			row := map[string]interface{}{
				"id":           doc.ID,
				"tenantId":     doc.TenantID,
				"sourceId":     doc.SourceID,
				"externalId":   doc.ExternalID,
				"title":        doc.Title,
				"url":          doc.URL,
				"documentPath": doc.DocumentPath,
				"storagePath":  storagePath,
				"status":       doc.Status,
				"contentType":  doc.ContentType,
				"documentType": doc.DocumentType,
				"language":     doc.Language,
				"contentHash":  doc.ContentHash,
				"ingestedAt":   doc.IngestedAt,
				"createdAt":    doc.CreatedAt,
				"updatedAt":    doc.UpdatedAt,
			}
			rows = append(rows, row)
		}

		// Insert batch into BigQuery
		if err := bqClient.InsertRows(ctx, "documents_metadata", rows); err != nil {
			log.Printf("Warning: failed to insert batch (continuing): %v", err)
		} else {
			migrated += len(rows)
			log.Printf("Migrated batch of %d RAG documents (total: %d)", len(rows), migrated)
		}

		lastDocID = docs[len(docs)-1].ID

		if limit > 0 && migrated >= limit {
			break
		}

		// Small delay to avoid overwhelming BigQuery
		time.Sleep(100 * time.Millisecond)
	}

	return migrated, nil
}

func migrateCoreDocs(ctx context.Context, fsClient *firestore.Client, bqClient *gcp.BigQueryClient, batchSize, limit int) (int, error) {
	col := fsClient.Collection("core_docs")
	var migrated int
	var lastDocID string

	for {
		// Fetch CoreDocs in batches using Firestore pagination
		q := col.OrderBy("createdAt", firestore.Asc).Limit(batchSize)
		if lastDocID != "" {
			// Get last document snapshot for cursor
			lastDoc, err := col.Doc(lastDocID).Get(ctx)
			if err == nil {
				q = q.StartAfter(lastDoc)
			}
		}

		iter := q.Documents(ctx)
		docs := []*models.CoreDoc{}
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return migrated, fmt.Errorf("failed to iterate CoreDocs: %w", err)
			}
			var d models.CoreDoc
			if err := doc.DataTo(&d); err != nil {
				continue
			}
			docs = append(docs, &d)
		}

		if len(docs) == 0 {
			break
		}

		// Prepare batch for BigQuery
		rows := make([]map[string]interface{}, 0, len(docs))
		for _, doc := range docs {
			storagePath := ""
			if doc.StoragePath != nil {
				storagePath = *doc.StoragePath
			}

			description := ""
			if doc.Description != nil {
				description = *doc.Description
			}

			extractedText := ""
			if doc.ExtractedText != nil {
				extractedText = *doc.ExtractedText
			}

			parseStatus := ""
			if doc.ParseStatus != nil {
				parseStatus = *doc.ParseStatus
			}

			docType := ""
			if doc.DocType != nil {
				docType = *doc.DocType
			}

			createdBy := ""
			if doc.CreatedBy != nil {
				createdBy = *doc.CreatedBy
			}

			var sizeBytes interface{}
			if doc.SizeBytes != nil {
				sizeBytes = *doc.SizeBytes
			} else {
				sizeBytes = nil
			}

			var pageCount interface{}
			if doc.PageCount != nil {
				pageCount = *doc.PageCount
			} else {
				pageCount = nil
			}

			row := map[string]interface{}{
				"id":            doc.ID,
				"tenantId":      doc.TenantID,
				"projectId":     doc.ProjectID,
				"filename":      doc.Filename,
				"contentType":   doc.ContentType,
				"description":   description,
				"sizeBytes":     sizeBytes,
				"storagePath":   storagePath,
				"extractedText": extractedText,
				"parseStatus":   parseStatus,
				"parseMeta":     nil,
				"pageCount":     pageCount,
				"docType":       docType,
				"createdBy":     createdBy,
				"createdAt":     doc.CreatedAt,
				"updatedAt":     doc.UpdatedAt,
			}
			rows = append(rows, row)
		}

		// Insert batch into BigQuery
		if err := bqClient.InsertRows(ctx, "core_docs_metadata", rows); err != nil {
			log.Printf("Warning: failed to insert batch (continuing): %v", err)
		} else {
			migrated += len(rows)
			log.Printf("Migrated batch of %d CoreDocs (total: %d)", len(rows), migrated)
		}

		lastDocID = docs[len(docs)-1].ID

		if limit > 0 && migrated >= limit {
			break
		}

		// Small delay to avoid overwhelming BigQuery
		time.Sleep(100 * time.Millisecond)
	}

	return migrated, nil
}
