package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
		sourceID  = flag.String("source", "", "Source ID to find")
	)
	flag.Parse()

	ctx := context.Background()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			log.Fatal("GCP_PROJECT_ID environment variable or --project flag is required")
		}
	}

	if *sourceID == "" {
		log.Fatal("--source flag is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())

	// Try to get source
	ds, err := sourcesRepo.Get(ctx, *sourceID)
	if err != nil {
		fmt.Printf("Source not found in core_datasources: %v\n", err)
		fmt.Printf("Trying to search in all collections...\n\n")
		
		// Try to search in source_projects to find tenantID
		sprojRepo := repo.NewSourceProjectsRepository(fsClient.Client())
		links, err := sprojRepo.ListBySource(ctx, "", *sourceID, nil)
		if err == nil && len(links) > 0 {
			fmt.Printf("Found source-project links:\n")
			for _, link := range links {
				fmt.Printf("  TenantID: %s, ProjectID: %s, Status: %s\n", link.TenantID, link.ProjectID, link.Status)
			}
			// Try to get source with tenantID from first link
			if len(links) > 0 {
				tenantID := links[0].TenantID
				fmt.Printf("\nTrying to get source with tenantID=%s...\n", tenantID)
				// Sources are global, so tenantID shouldn't matter, but let's try
			}
		}
		
		// List all sources to see if ID is similar
		fmt.Printf("\nListing all sources to find similar ID...\n")
		kinds := []string{"rss", "telegram_channel", "web"}
		allSources, err := sourcesRepo.ListByKinds(ctx, kinds)
		if err == nil {
			fmt.Printf("Found %d sources total\n", len(allSources))
			for _, s := range allSources {
				if len(s.ID) > 8 && (*sourceID)[:8] == s.ID[:8] {
					fmt.Printf("  Similar ID: %s (kind: %s, displayName: %s)\n", s.ID, s.Kind, s.DisplayName)
				}
			}
		}
		return
	}

	fmt.Printf("✅ Found source:\n")
	fmt.Printf("  ID: %s\n", ds.ID)
	fmt.Printf("  Kind: %s\n", ds.Kind)
	fmt.Printf("  DisplayName: %s\n", ds.DisplayName)
	fmt.Printf("  Status: %s\n", ds.Status)
	fmt.Printf("  TenantID: %s\n", ds.TenantID)
	if ds.Details.ChannelUsername != nil {
		fmt.Printf("  ChannelUsername: %s\n", *ds.Details.ChannelUsername)
	}
	if ds.Details.RSSURL != nil {
		fmt.Printf("  RSSURL: %s\n", *ds.Details.RSSURL)
	}
	if ds.Details.URI != nil {
		fmt.Printf("  URI: %s\n", *ds.Details.URI)
	}
}

