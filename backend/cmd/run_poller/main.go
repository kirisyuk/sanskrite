package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
	)
	flag.Parse()

	ctx := context.Background()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("GCP_PROJECT_ID environment variable or --project flag is required")
		}
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	logger := log.New(os.Stdout, "[poller-test] ", log.LstdFlags|log.Lshortfile)

	// Initialize repositories
	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	sourceRuntimeRepo := repo.NewSourceRuntimeStatesRepository(fsClient.Client())

	// Create orchestrator (minimal setup for poller)
	orchestrator := discovery.NewOrchestrator(nil, nil, nil, nil, sourcesRepo, nil, logger)
	orchestrator = orchestrator.WithSourceProjects(sourceProjectsRepo)

	poller := discovery.NewSourcePoller(orchestrator, logger).WithRuntimeStates(sourceRuntimeRepo)
	baseURL := strings.TrimSpace(os.Getenv("CLOUD_TASKS_TARGET_BASE_URL"))
	saEmail := strings.TrimSpace(os.Getenv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT"))
	if baseURL == "" || saEmail == "" {
		log.Fatal("CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT are required for source coordination")
	}
	queue := strings.TrimSpace(os.Getenv("CLOUD_TASKS_SOURCE_QUEUE"))
	if queue == "" {
		queue = "nv-horizon-source-poll"
	}
	location := strings.TrimSpace(os.Getenv("CLOUD_TASKS_LOCATION"))
	if location == "" {
		location = "us-central1"
	}
	aud := strings.TrimSpace(os.Getenv("CLOUD_TASKS_OIDC_AUDIENCE"))
	dispatcher, err := discovery.NewSourceCloudTasksDispatcher(
		ctx,
		*projectID,
		location,
		queue,
		strings.TrimRight(baseURL, "/")+"/api/v1/internal/tasks/sources/poll",
		saEmail,
		aud,
	)
	if err != nil {
		log.Fatalf("Failed to create source Cloud Tasks dispatcher: %v", err)
	}
	defer dispatcher.Close()
	poller = poller.WithTaskDispatcher(dispatcher)

	fmt.Printf("Running source coordination at %s...\n\n", time.Now().Format("2006-01-02 15:04:05"))

	if err := poller.CoordinateDue(ctx); err != nil {
		log.Fatalf("CoordinateDue failed: %v", err)
	}

	fmt.Printf("\n✅ Source coordination completed successfully!\n")
	fmt.Printf("Check Cloud Tasks queue %q for enqueued source execution tasks.\n", queue)
}
