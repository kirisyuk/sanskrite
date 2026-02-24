// horizon_test_signaling_rss verifies the signaling pipeline for one RSS source.
//
// Flow: RSS source → IngestRSS → document.extract → OntologyFact(event) [FactsExtractor in server]
//       → POST /api/horizon/events:extract-signals (manual) → SignalPipeline.ProcessEvent
//
// Facts-only path: no Document/Chunks; FactsExtractor consumes document.extract and persists to GCS/Vertex.
// 12-axis signals: run server, then call POST /api/horizon/events:extract-signals with event IDs.
//
// Usage:
//
//	GCP_PROJECT_ID=h1horizon go run ./cmd/tools/horizon_test_signaling_rss/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	gcp 	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	testTenantID = "signaling-test"
	hnSourceID   = "31mwZSXeA7LQ3OWYKU1f" // Hacker News from seed
)

func main() {
	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project")
	sourceID := flag.String("source", hnSourceID, "RSS source ID to test")
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project or GCP_PROJECT_ID required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fsClient.Close()

	logger := log.New(os.Stdout, "[signaling-test] ", log.LstdFlags)

	// Repos
	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	projectsRepo := repo.NewProjectsRepository(fsClient.Client())
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())

	// Ensure source exists
	ds, err := sourcesRepo.Get(ctx, *sourceID)
	if err != nil || ds == nil {
		log.Fatalf("source %s not found: %v", *sourceID, err)
	}
	if ds.Kind != "rss" {
		log.Fatalf("source %s is not rss (kind=%s)", *sourceID, ds.Kind)
	}

	// Ensure project exists (create or reuse)
	var testProjectID string
	projs, err := projectsRepo.List(ctx, testTenantID, nil)
	if err != nil || len(projs) == 0 {
		proj := &models.Project{
			Pk:          testTenantID,
			TenantID:    testTenantID,
			DisplayName: "Signaling Test Project",
			Status:      "active",
			OwnerUserID: "system",
		}
		testProjectID, err = projectsRepo.Create(ctx, proj)
		if err != nil {
			log.Fatalf("create project: %v", err)
		}
		logger.Printf("created project %s", testProjectID)
	} else {
		testProjectID = projs[0].ID
	}

	// Link source to project
	links, _ := sourceProjectsRepo.ListBySource(ctx, "", *sourceID, strPtr("active"))
	if len(links) == 0 {
		sp := &models.SourceProject{
			TenantID:  testTenantID,
			SourceID:  *sourceID,
			ProjectID: testProjectID,
			Status:    "active",
		}
		if _, err := sourceProjectsRepo.Create(ctx, sp); err != nil {
			log.Fatalf("create source-project link: %v", err)
		}
		logger.Printf("linked source %s to project %s", *sourceID, testProjectID)
	}

	// Activate source
	if ds.Status != "active" {
		ds.Status = "active"
		if err := sourcesRepo.Upsert(ctx, ds); err != nil {
			log.Fatalf("activate source: %v", err)
		}
		logger.Printf("activated source %s", *sourceID)
	}

	// Publisher
	publisher, err := realtime.NewPublisher(ctx, *projectID)
	if err != nil {
		log.Fatalf("publisher: %v", err)
	}

	wc := crawler.NewWebCrawler()
	exec := ai.NewAITaskExecutor(nil, nil)

	reg := prov.NewRegistry()
	reg.Register(prov.NewRSSProvider())

	orch := discovery.NewOrchestrator(exec, wc, nil, projectsRepo, sourcesRepo, publisher, logger)
	orch = orch.WithSourceProjects(sourceProjectsRepo)
	orch = orch.WithDocumentIngest(reg, docsRepo) // Facts-only path: publishes document.extract, no Vertex indexing
	orch = orch.WithIngestHistory(ingestHistoryRepo)

	// Run IngestRSS (facts-only: publishes document.extract)
	logger.Printf("running IngestRSS for source %s (%s)...", *sourceID, ds.DisplayName)
	if err := orch.IngestRSS(ctx, testTenantID, *sourceID, nil); err != nil {
		log.Fatalf("IngestRSS: %v", err)
	}

	// Ingest history
	history, _ := ingestHistoryRepo.ListBySource(ctx, "", *sourceID, 3)
	if len(history) > 0 {
		h := history[0]
		logger.Printf("last ingest: status=%s docs published=%d/%d", h.Status, h.DocumentsCreated, h.DocumentsProcessed)
	}

	// FactsExtractor (in server) consumes document.extract → OntologyFacts
	logger.Printf("document.extract events published; FactsExtractor (run server) persists to ontology_facts")

	fmt.Println("\n✅ Signaling pipeline test complete.")
	fmt.Println("1. Run server to consume document.extract → ontology_facts (FactsExtractor)")
	fmt.Println("2. For 12-axis signals: POST /api/horizon/events:extract-signals with event IDs")
}

func strPtr(s string) *string { return &s }
