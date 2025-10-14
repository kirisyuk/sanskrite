package main

import (
	"context"
	"log"
	"os"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID not set")
	}
	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	sproj := repo.NewSourceProjectsRepository(fs.Client())

	// Scan datasources and create sourceProjects for primary and related projects
	docs, err := fs.Client().Collection("core_datasources").Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("list datasources: %v", err)
	}
	now := time.Now().UTC()
	created := 0
	for _, d := range docs {
		var ds models.DataSource
		if err := d.DataTo(&ds); err != nil {
			continue
		}
		// primary
		if ds.ProjectID != nil && *ds.ProjectID != "" {
			sp := &models.SourceProject{TenantID: ds.TenantID, SourceID: ds.ID, ProjectID: *ds.ProjectID, Status: "active", CreatedAt: now, UpdatedAt: now}
			if _, err := sproj.Create(ctx, sp); err == nil {
				created++
			}
		}
		// related
		for _, pid := range ds.RelatedProjectIDs {
			sp := &models.SourceProject{TenantID: ds.TenantID, SourceID: ds.ID, ProjectID: pid, Status: "active", CreatedAt: now, UpdatedAt: now}
			if _, err := sproj.Create(ctx, sp); err == nil {
				created++
			}
		}
	}
	log.Printf("migration complete: created %d sourceProjects", created)
}
