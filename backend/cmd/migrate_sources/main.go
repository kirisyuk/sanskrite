package main

import (
	"context"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
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
	projs := repo.NewProjectsRepository(fs.Client())

	// Scan datasources and create sourceProjects for primary and related projects
	docs, err := fs.Client().Collection("core_datasources").Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("list datasources: %v", err)
	}
	now := time.Now().UTC()
	created := 0
	updatedPublic := 0
	// Collect public datasource IDs for later profile migration
	publicSourceIDs := map[string]struct{}{}
	for _, d := range docs {
		var ds models.DataSource
		if err := d.DataTo(&ds); err != nil {
			continue
		}
		// Determine public kinds
		isPublic := ds.Kind == "web" || ds.Kind == "rss" || ds.Kind == "telegram_channel"

		// 1) Create source-project links (idempotent): resolve tenantId from Project
		// Primary project
		if ds.ProjectID != nil && *ds.ProjectID != "" {
			if p, err := projs.Get(ctx, *ds.ProjectID); err == nil && p != nil {
				ensureLink(ctx, fs.Client(), sproj, p.TenantID, *ds.ProjectID, ds.ID, now, &created)
			}
		}
		// Related projects
		for _, pid := range ds.RelatedProjectIDs {
			if p, err := projs.Get(ctx, pid); err == nil && p != nil {
				ensureLink(ctx, fs.Client(), sproj, p.TenantID, pid, ds.ID, now, &created)
			}
		}

		// 2) For public sources: null tenantId and set pk to global
		if isPublic {
			publicSourceIDs[ds.ID] = struct{}{}
			needUpdate := false
			if ds.TenantID != "" {
				ds.TenantID = ""
				needUpdate = true
			}
			if ds.Pk != "global" {
				ds.Pk = "global"
				needUpdate = true
			}
			if needUpdate {
				if err := repo.NewDataSourcesRepository(fs.Client()).Upsert(ctx, &ds); err == nil {
					updatedPublic++
				} else {
					log.Printf("warn: failed to upsert datasource %s: %v", ds.ID, err)
				}
			}
		}
	}

	// 3) Migrate SourceProfiles: set tenantId="" for profiles of public sources
	profDocs, err := fs.Client().Collection("core_source_profiles").Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("list source profiles: %v", err)
	}
	profilesUpdated := 0
	for _, d := range profDocs {
		var sp models.SourceProfile
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		if _, ok := publicSourceIDs[sp.DataSourceID]; ok {
			if sp.TenantID != "" {
				// Soft update: partial update of tenantId to empty string, bypassing repo validation
				if _, err := fs.Client().Collection("core_source_profiles").Doc(d.Ref.ID).Update(ctx, []firestore.Update{{Path: "tenantId", Value: ""}}); err == nil {
					profilesUpdated++
				} else {
					log.Printf("warn: failed to soft-update source profile %s: %v", d.Ref.ID, err)
				}
			}
		}
	}

	log.Printf("migration complete: created %d sourceProjects, updated %d public datasources, updated %d public profiles", created, updatedPublic, profilesUpdated)
}

// ensureLink creates SourceProject link if it does not already exist
func ensureLink(ctx context.Context, client *firestore.Client, sproj *repo.SourceProjectsRepository, tenantID, projectID, sourceID string, now time.Time, created *int) {
	// Check existence
	q := client.Collection("rag_source_projects").Where("tenantId", "==", tenantID).Where("projectId", "==", projectID).Where("sourceId", "==", sourceID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err == nil && len(docs) > 0 {
		return
	}
	sp := &models.SourceProject{TenantID: tenantID, SourceID: sourceID, ProjectID: projectID, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, err := sproj.Create(ctx, sp); err == nil {
		*created++
	}
}
