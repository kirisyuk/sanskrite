package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
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
	runtimeRepo := repo.NewSourceRuntimeStatesRepository(fsClient.Client())

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
	fmt.Printf("  SourceScope: %s\n", models.SourceScopeValue(ds))
	fmt.Printf("  ProcessingMode: %s\n", models.SourceProcessingModeValue(ds))
	fmt.Printf("  OutputMode: %s\n", models.SourceOutputModeValue(ds))
	fmt.Printf("  DisableLLMProcessing: %t\n", models.ShouldDisableLLMProcessing(ds))
	if ds.Details.CadencePolicy != nil {
		fmt.Printf("  CadencePolicy: base=%s min=%s max=%s emptyBackoff=%s errorBackoff=%s freshness=%s lease=%s\n",
			fmtInt(ds.Details.CadencePolicy.BaseIntervalMinutes),
			fmtInt(ds.Details.CadencePolicy.MinIntervalMinutes),
			fmtInt(ds.Details.CadencePolicy.MaxIntervalMinutes),
			fmtFloat(ds.Details.CadencePolicy.EmptyBackoffMultiplier),
			fmtFloat(ds.Details.CadencePolicy.ErrorBackoffMultiplier),
			fmtInt(ds.Details.CadencePolicy.FreshnessBoostWindowMinutes),
			fmtInt(ds.Details.CadencePolicy.LeaseDurationMinutes),
		)
	}
	fmt.Printf("  LastPolledAt: %s\n", fmtTime(ds.LastPolledAt))
	fmt.Printf("  LastFoundAt: %s\n", fmtTime(ds.LastFoundAt))
	fmt.Printf("  NextPollAt: %s\n", fmtTime(ds.NextPollAt))
	fmt.Printf("  ErrorCount: %d\n", ds.ErrorCount)
	fmt.Printf("  PollBackoffSec: %s\n", fmtInt(ds.PollBackoffSec))

	rt, err := runtimeRepo.Get(ctx, ds.ID)
	if err != nil {
		fmt.Printf("  RuntimeState: %v\n", err)
		return
	}
	fmt.Printf("  RuntimeState.ID: %s\n", rt.ID)
	fmt.Printf("  RuntimeState.Scope: %s\n", rt.Scope)
	fmt.Printf("  RuntimeState.ProcessingMode: %s\n", rt.ProcessingMode)
	fmt.Printf("  RuntimeState.OutputMode: %s\n", rt.OutputMode)
	fmt.Printf("  RuntimeState.LastPolledAt: %s\n", fmtTime(rt.LastPolledAt))
	fmt.Printf("  RuntimeState.LastFoundAt: %s\n", fmtTime(rt.LastFoundAt))
	fmt.Printf("  RuntimeState.LastSuccessAt: %s\n", fmtTime(rt.LastSuccessAt))
	fmt.Printf("  RuntimeState.LastErrorAt: %s\n", fmtTime(rt.LastErrorAt))
	fmt.Printf("  RuntimeState.NextPollAt: %s\n", fmtTime(rt.NextPollAt))
	fmt.Printf("  RuntimeState.LeaseOwner: %s\n", fmtString(rt.LeaseOwner))
	fmt.Printf("  RuntimeState.LeaseUntil: %s\n", fmtTime(rt.LeaseUntil))
	fmt.Printf("  RuntimeState.ConsecutiveEmptyPolls: %d\n", rt.ConsecutiveEmptyPolls)
	fmt.Printf("  RuntimeState.ConsecutiveErrors: %d\n", rt.ConsecutiveErrors)
	fmt.Printf("  RuntimeState.LastStatus: %s\n", fmtString(rt.LastStatus))
	fmt.Printf("  RuntimeState.LastYield: %s\n", fmtFloat(rt.LastYield))
	fmt.Printf("  RuntimeState.AvgYield: %s\n", fmtFloat(rt.AvgYield))
	fmt.Printf("  RuntimeState.TotalRuns: %d\n", rt.TotalRuns)
	fmt.Printf("  RuntimeState.TotalDocumentsProcessed: %d\n", rt.TotalDocumentsProcessed)
	fmt.Printf("  RuntimeState.TotalDocumentsCreated: %d\n", rt.TotalDocumentsCreated)
	fmt.Printf("  RuntimeState.CreatedAt: %s\n", rt.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	fmt.Printf("  RuntimeState.UpdatedAt: %s\n", rt.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
}

func fmtTime(v *time.Time) string {
	if v == nil {
		return "<nil>"
	}
	return v.UTC().Format("2006-01-02T15:04:05Z")
}

func fmtString(v *string) string {
	if v == nil {
		return "<nil>"
	}
	if s := strings.TrimSpace(*v); s != "" {
		return s
	}
	return "<empty>"
}

func fmtInt(v *int) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *v)
}

func fmtFloat(v *float64) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%.3f", *v)
}
