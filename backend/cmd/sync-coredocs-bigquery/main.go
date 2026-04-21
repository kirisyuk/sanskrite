package main

import (
	"context"
	"flag"
	"log"
	"os"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func main() {
	projectID := flag.String("project", "", "GCP project ID")
	datasetID := flag.String("dataset", "horizon_documents", "BigQuery dataset ID")
	tenantID := flag.String("tenant", "", "Tenant ID (optional, syncs all tenants if not specified)")
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

	// Initialize BigQuery
	bqClient, err := gcp.NewBigQueryClient(ctx, *projectID, *datasetID)
	if err != nil {
		log.Fatalf("Failed to create BigQuery client: %v", err)
	}
	defer bqClient.Close()

	// Query all CoreDocs
	var docs []*firestore.DocumentSnapshot
	if *tenantID != "" {
		var err error
		docs, err = fsClient.Collection("core_docs").Where("tenantId", "==", *tenantID).Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("Failed to query CoreDocs: %v", err)
		}
	} else {
		var err error
		docs, err = fsClient.Collection("core_docs").Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("Failed to query CoreDocs: %v", err)
		}
	}

	log.Printf("Found %d CoreDocs to sync", len(docs))

	synced := 0
	failed := 0

	for _, docSnap := range docs {
		var cd models.CoreDoc
		if err := docSnap.DataTo(&cd); err != nil {
			log.Printf("Failed to parse CoreDoc %s: %v", docSnap.Ref.ID, err)
			failed++
			continue
		}

		// Check if document exists in BigQuery
		exists, err := bqClient.DocumentExistsInTable(ctx, "core_docs_metadata", cd.ID)
		if err != nil {
			log.Printf("Failed to check BigQuery status for %s: %v", cd.ID, err)
			failed++
			continue
		}

		if exists {
			log.Printf("Document %s (%s) already exists in BigQuery, skipping", cd.ID, cd.Filename)
			continue
		}

		// Sync to BigQuery directly (bypass async goroutine)
		// We need to call syncToBigQuery synchronously for the script
		if bqClient != nil {
			// Call syncToBigQuery directly
			storagePath := ""
			if cd.StoragePath != nil {
				storagePath = *cd.StoragePath
			}
			description := ""
			if cd.Description != nil {
				description = *cd.Description
			}
			extractedText := ""
			if cd.ExtractedText != nil {
				extractedText = *cd.ExtractedText
			}
			parseStatus := ""
			if cd.ParseStatus != nil {
				parseStatus = *cd.ParseStatus
			}
			docType := ""
			if cd.DocType != nil {
				docType = *cd.DocType
			}
			createdBy := ""
			if cd.CreatedBy != nil {
				createdBy = *cd.CreatedBy
			}
			var sizeBytes *int64
			if cd.SizeBytes != nil {
				sizeBytes = cd.SizeBytes
			}
			var pageCount *int
			if cd.PageCount != nil {
				pageCount = cd.PageCount
			}

			row := map[string]interface{}{
				"id":            cd.ID,
				"tenantId":      cd.TenantID,
				"projectId":     cd.ProjectID,
				"filename":      cd.Filename,
				"contentType":   cd.ContentType,
				"description":   description,
				"sizeBytes":     sizeBytes,
				"storagePath":   storagePath,
				"extractedText": extractedText,
				"parseStatus":   parseStatus,
				"parseMeta":     cd.ParseMeta,
				"pageCount":     pageCount,
				"docType":       docType,
				"createdBy":     createdBy,
				"createdAt":     cd.CreatedAt,
				"updatedAt":     cd.UpdatedAt,
			}

			// Use UpsertRow - it handles JSON fields correctly and handles duplicates via MERGE
			if err := bqClient.UpsertRow(ctx, "core_docs_metadata", row); err != nil {
				log.Printf("Failed to sync CoreDoc %s (%s) to BigQuery: %v", cd.ID, cd.Filename, err)
				failed++
				continue
			}

			// Verify it was inserted
			exists, err := bqClient.DocumentExistsInTable(ctx, "core_docs_metadata", cd.ID)
			if err != nil {
				log.Printf("Warning: failed to verify BigQuery sync for %s: %v", cd.ID, err)
			} else if !exists {
				log.Printf("Warning: document %s was not found in BigQuery after sync", cd.ID)
				failed++
				continue
			}
		}

		log.Printf("Synced CoreDoc %s (%s) to BigQuery", cd.ID, cd.Filename)
		synced++
	}

	log.Printf("\nSync complete:")
	log.Printf("  Synced: %d", synced)
	log.Printf("  Failed: %d", failed)
	log.Printf("  Total:  %d", len(docs))
}
