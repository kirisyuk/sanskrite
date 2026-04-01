package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// This tool creates a minimal end-to-end dataset:
// - picks an existing HorizonFragment with CreatedByProjectID
// - creates/updates a RUNNING WatchdogConfig due now
// - inserts a fresh ontology_fact linked to fragment canonicalPath
//
// Then you can call the watchdog cron endpoint and verify it triggers a scouting job.
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	tenantID := flag.String("tenant", "global", "tenantId (default global)")
	limit := flag.Int("limit", 200, "how many fragments to scan before giving up")
	flag.Parse()

	if *projectID == "" {
		*projectID = "nv-horizon"
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	watchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)
	dataSourcesRepo := repo.NewDataSourcesRepository(db)
	factsRepo := repo.NewOntologyFactsRepository(db)

	// 1) Pick any fragment with canonicalPath (fragments are global)
	var picked *horizonmodels.HorizonFragment

	docs, err := db.Collection("horizon_fragments").Limit(*limit).Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("failed to list horizon_fragments: %v", err)
	}
	for _, d := range docs {
		var f horizonmodels.HorizonFragment
		if err := d.DataTo(&f); err != nil {
			continue
		}
		if f.CanonicalPath != "" {
			picked = &f
			break
		}
	}
	if picked == nil {
		log.Fatalf("no suitable horizon fragment found (need canonicalPath)")
	}
	log.Printf("Picked fragment: id=%s canonicalPath=%s", picked.ID, picked.CanonicalPath)

	// 2) Pick any existing core DataSource that has an active project link
	var dsID string
	var linkedProjectID string
	links, err := db.Collection("rag_source_projects").Where("tenantId", "==", *tenantID).Where("status", "==", "active").Limit(1).Documents(ctx).GetAll()
	if err == nil && len(links) > 0 {
		var sp models.SourceProject
		if err := links[0].DataTo(&sp); err == nil {
			dsID = sp.SourceID
			linkedProjectID = sp.ProjectID
		}
	}
	if dsID == "" {
		// No active links yet. Create a minimal web datasource and link it to any existing project.
		pdocs, err := db.Collection("core_projects").Limit(1).Documents(ctx).GetAll()
		if err != nil || len(pdocs) == 0 {
			log.Fatalf("no core_projects found; cannot create a source link for smoke test")
		}
		linkedProjectID = pdocs[0].Ref.ID

		uri := fmt.Sprintf("https://en.wikipedia.org/wiki/%s", "Harman_International")
		ds := &models.DataSource{
			TenantID:    *tenantID,
			DisplayName: "Smoke Source (Wikipedia)",
			Kind:        "web",
			Status:      "active",
			OwnerUserID: "system",
			Details: models.SourceDetails{
				URI: &uri,
			},
		}
		createdID, err := dataSourcesRepo.Create(ctx, ds)
		if err != nil {
			log.Fatalf("failed to create datasource: %v", err)
		}
		dsID = createdID

		sp := &models.SourceProject{
			TenantID:  *tenantID,
			SourceID:  dsID,
			ProjectID: linkedProjectID,
			Status:    "active",
			Weights:   map[string]float64{},
			SyncConfig: map[string]any{},
		}
		spRepo := repo.NewSourceProjectsRepository(db)
		if _, err := spRepo.Create(ctx, sp); err != nil {
			log.Fatalf("failed to create rag_source_projects link: %v", err)
		}
		log.Printf("Created datasource+link: sourceId=%s projectId=%s uri=%s", dsID, linkedProjectID, uri)
	}
	// If projectId wasn't resolved from the raw query, resolve via repo helper.
	if linkedProjectID == "" {
		if pid, err := dataSourcesRepo.ResolveProjectIDForSource(ctx, *tenantID, dsID); err == nil {
			linkedProjectID = pid
		}
	}
	log.Printf("Picked dataSourceId=%s projectId=%s", dsID, linkedProjectID)

	// 3) Ensure a watchdog config exists and is due now
	now := time.Now().UTC()
	cfg, err := watchdogRepo.GetByTargetFragmentID(ctx, *tenantID, picked.ID)
	if err != nil {
		// create
		newCfg := &horizonmodels.WatchdogConfig{
			TenantID:         *tenantID,
			TargetFragmentID: picked.ID,
			ScanInterval:     "PT5M",
			SourceIDs:        nil,
			UseProjectSources: false,
			TriggerThreshold: 0.5,
			Status:           string(horizonmodels.WatchdogStatusRunning),
			LastScanAt:       ptrTime(now.Add(-2 * time.Hour)),
			NextScanAt:       ptrTime(now.Add(-1 * time.Minute)),
		}
		id, err := watchdogRepo.Create(ctx, newCfg)
		if err != nil {
			log.Fatalf("failed to create watchdog config: %v", err)
		}
		log.Printf("Created watchdog config: %s", id)
	} else {
		// update to be due and running
		cfg.Status = string(horizonmodels.WatchdogStatusRunning)
		cfg.TriggerThreshold = 0.5
		cfg.ScanInterval = "PT5M"
		cfg.NextScanAt = ptrTime(now.Add(-1 * time.Minute))
		if cfg.LastScanAt == nil {
			cfg.LastScanAt = ptrTime(now.Add(-2 * time.Hour))
		}
		if err := watchdogRepo.Update(ctx, cfg); err != nil {
			log.Fatalf("failed to update watchdog config: %v", err)
		}
		log.Printf("Updated watchdog config: %s", cfg.ID)
	}

	// 4) Insert a fresh ontology fact linked to the fragment canonicalPath
	fact := &models.OntologyFact{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  *tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "watchdog_facts_smoke",
			UpdatedBy: "watchdog_facts_smoke",
			Labels:    map[string]string{"en": "Smoke test fact"},
			Status:    "active",
			CanonicalPaths: []string{
				picked.CanonicalPath,
			},
			ActorPaths: []string{
				picked.CanonicalPath,
			},
		},
		Kind:       "event",
		Type:       "smoke_test",
		Title:      fmt.Sprintf("Smoke fact for %s", picked.CanonicalPath),
		Summary:    "This fact is created by watchdog_facts_smoke tool to validate watchdog->facts->scout wiring.",
		Confidence: 0.95,
		ObservedAt: ptrTime(now),
		SourceIDs:  []string{dsID},
		SourceRefs: []models.SourceRef{{SourceID: "manual", SourceType: "tool"}},
	}
	fact.VectorText = fact.GenerateVectorText()
	factID, err := factsRepo.Create(ctx, fact)
	if err != nil {
		log.Fatalf("failed to create ontology_fact: %v", err)
	}
	log.Printf("Created ontology_fact: id=%s canonicalPath=%s sourceId=%s", factID, picked.CanonicalPath, dsID)

	log.Printf("DONE. Now run the watchdog cron endpoint (Cloud Scheduler or POST /api/v1/admin/horizon/watchdog/run) and verify a scouting job is created.")
}

func ptrTime(t time.Time) *time.Time { return &t }


