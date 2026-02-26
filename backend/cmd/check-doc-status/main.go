package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project ID")
	docID := flag.String("doc", "", "CoreDoc ID to check")
	datasetID := flag.String("dataset", "h1horizon_488615_documents", "BigQuery dataset ID")
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("Project ID is required")
		}
	}

	if *docID == "" {
		log.Fatal("Document ID is required")
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

	coreDocsRepo := repo.NewCoreDocsRepo(fsClient)
	docsRepo := repo.NewDocumentsRepository(fsClient)

	// Get CoreDoc
	cd, err := coreDocsRepo.Get(ctx, *docID)
	if err != nil {
		log.Fatalf("Failed to get CoreDoc: %v", err)
	}

	fmt.Printf("Document: %s (%s)\n", cd.ID, cd.Filename)
	fmt.Printf("TenantID: %s\n", cd.TenantID)
	fmt.Printf("ProjectID: %s\n", cd.ProjectID)
	if cd.StoragePath != nil {
		fmt.Printf("StoragePath: %s\n", *cd.StoragePath)
	} else {
		fmt.Printf("StoragePath: <nil>\n")
	}
	if cd.ExtractedText != nil {
		fmt.Printf("ExtractedText length: %d\n", len(*cd.ExtractedText))
	} else {
		fmt.Printf("ExtractedText: <nil>\n")
	}

	// Check BigQuery status
	exists, err := bqClient.DocumentExistsInTable(ctx, "core_docs_metadata", cd.ID)
	if err != nil {
		fmt.Printf("BigQuery check error: %v\n", err)
	} else {
		fmt.Printf("BigQuery status: %v\n", exists)
	}

	// Check indexing status
	ragDoc, err := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
	if err != nil {
		fmt.Printf("RAG Document: not found\n")
	} else if ragDoc != nil {
		fmt.Printf("RAG Document ID: %s\n", ragDoc.ID)
		if ragDoc.StoragePath != nil {
			fmt.Printf("RAG StoragePath: %s\n", *ragDoc.StoragePath)
		}
		if ragDoc.VertexSearchIndexedAt != nil {
			fmt.Printf("VertexSearchIndexedAt: %v\n", *ragDoc.VertexSearchIndexedAt)
		} else {
			fmt.Printf("VertexSearchIndexedAt: <nil>\n")
		}
	}
}
