package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "", "GCP Project ID")
	)
	flag.Parse()

	ctx := context.Background()

	// Initialize Firestore client
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

	sourcesRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())

	// Get all sources (rss, telegram, web, horizon_monitor_feed)
	kinds := []string{"rss", "telegram_channel", "web", "horizon_monitor_feed", "worldmonitor_feed"}
	sources, err := sourcesRepo.ListByKinds(ctx, kinds)
	if err != nil {
		log.Fatalf("Failed to list sources: %v", err)
	}

	fmt.Printf("\n=== Found %d sources (rss, telegram_channel, web, horizon_monitor_feed) ===\n\n", len(sources))

	activeCount := 0
	for _, ds := range sources {
		// Check active project links for this source
		activeStatus := "active"
		var links []*models.SourceProject
		if ds.TenantID == "" {
			// Public source - check all tenants
			links, err = sourceProjectsRepo.ListBySource(ctx, "", ds.ID, &activeStatus)
		} else {
			links, err = sourceProjectsRepo.ListBySource(ctx, ds.TenantID, ds.ID, &activeStatus)
		}

		if err != nil {
			fmt.Printf("ERROR checking links for source %s: %v\n", ds.ID, err)
			continue
		}

		hasActiveProjects := len(links) > 0
		if hasActiveProjects {
			activeCount++
		}

		// Get all links (not just active) for info
		allLinks, _ := sourceProjectsRepo.ListBySource(ctx, ds.TenantID, ds.ID, nil)
		if ds.TenantID == "" {
			allLinks, _ = sourceProjectsRepo.ListBySource(ctx, "", ds.ID, nil)
		}

		status := "❌ INACTIVE"
		if hasActiveProjects {
			status = "✅ ACTIVE"
		}

		fmt.Printf("%s Source: %s (kind: %s, status: %s)\n", status, ds.ID, ds.Kind, ds.Status)
		fmt.Printf("   DisplayName: %s\n", ds.DisplayName)
		fmt.Printf("   TenantID: %s\n", ds.TenantID)
		if ds.NextPollAt != nil {
			fmt.Printf("   NextPollAt: %s\n", ds.NextPollAt.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("   NextPollAt: not set\n")
		}
		if ds.LastPolledAt != nil {
			fmt.Printf("   LastPolledAt: %s\n", ds.LastPolledAt.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("   LastPolledAt: never\n")
		}
		fmt.Printf("   Project links: %d total, %d active\n", len(allLinks), len(links))
		if len(links) > 0 {
			fmt.Printf("   Active projects:\n")
			for _, link := range links {
				fmt.Printf("     - ProjectID: %s, TenantID: %s, Status: %s\n", link.ProjectID, link.TenantID, link.Status)
			}
		}
		fmt.Println()
	}

	fmt.Printf("=== Summary ===\n")
	fmt.Printf("Total sources: %d\n", len(sources))
	fmt.Printf("Sources with active projects: %d\n", activeCount)
	fmt.Printf("Sources without active projects: %d\n", len(sources)-activeCount)

	// Also check source_ingest_history for recent activity
	ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())
	fmt.Printf("\n=== Recent ingest history (last 10 entries) ===\n")
	// We need to check history for each source
	for _, ds := range sources {
		if len(sources) > 10 {
			// Limit to first 10 sources for history check
			break
		}
		history, err := ingestHistoryRepo.ListBySource(ctx, ds.TenantID, ds.ID, 5)
		if err == nil && len(history) > 0 {
			fmt.Printf("\nSource %s (%s):\n", ds.ID, ds.Kind)
			for _, h := range history {
				status := h.Status
				if h.Error != nil {
					status = fmt.Sprintf("%s (error: %s)", status, *h.Error)
				}
				fmt.Printf("  - %s: %s, docs: %d/%d, duration: %dms\n",
					h.StartedAt.Format("2006-01-02 15:04:05"),
					status,
					h.DocumentsCreated,
					h.DocumentsProcessed,
					func() int {
						if h.DurationMs != nil {
							return *h.DurationMs
						}
						return 0
					}())
			}
		}
	}
}

