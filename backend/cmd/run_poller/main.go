package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
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
	
	// Create publisher for Pub/Sub
	publisher, err := realtime.NewPublisher(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Publisher: %v", err)
	}
	
	// Create orchestrator (minimal setup for poller)
	orchestrator := discovery.NewOrchestrator(nil, nil, nil, nil, sourcesRepo, publisher, logger)
	orchestrator = orchestrator.WithSourceProjects(sourceProjectsRepo)

	// Create poller (it will use orchestrator's publisher)
	psClient := publisher.Client()
	poller := discovery.NewSourcePoller(psClient, "source.poll.due", orchestrator, logger)

	fmt.Printf("Running PublishDue at %s...\n\n", time.Now().Format("2006-01-02 15:04:05"))
	
	if err := poller.PublishDue(ctx); err != nil {
		log.Fatalf("PublishDue failed: %v", err)
	}

	fmt.Printf("\n✅ PublishDue completed successfully!\n")
	fmt.Printf("Check Pub/Sub topic 'source.poll.due' for published messages.\n")
}

