package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID   = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
		tenantID    = flag.String("tenant", "global", "tenant id")
		mode        = flag.String("mode", "sync", "mode: sync|explain-subject|explain-trigger|replay-trigger|cleanup-legacy|metrics")
		subjectID   = flag.String("subject-id", "", "watch subject id")
		triggerID   = flag.String("trigger-id", "", "watch trigger id")
		limit       = flag.Int("limit", 100, "list limit")
		includeGlob = flag.Bool("include-global", false, "include global tenant during sync")
		apply       = flag.Bool("apply", false, "apply changes for sync/cleanup")
	)
	flag.Parse()

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore.NewClient: %v", err)
	}
	defer db.Close()

	agentJobsRepo := coreRepo.NewAgentJobsRepository(db)
	service := horizonwatchruntime.NewService(
		horizonrepo.NewWatchSubjectsRepository(db),
		horizonrepo.NewWatchTriggersRepository(db),
		horizonrepo.NewWatchTargetsRepository(db),
		horizonrepo.NewWatchdogConfigsRepository(db),
		horizonrepo.NewScopeAgentConfigsRepository(db),
		horizonrepo.NewLensesRepository(db),
		horizonrepo.NewLensArtifactsRepository(db),
		horizonrepo.NewHorizonFragmentsRepository(db),
		horizonrepo.NewDependencyEdgesRepository(db),
	).WithScopeAgentRunsRepository(horizonrepo.NewScopeAgentRunsRepository(db)).
		WithWatchBriefsRepository(horizonrepo.NewWatchBriefsRepository(db)).
		WithWatchFeedEntriesRepository(horizonrepo.NewWatchFeedEntriesRepository(db)).
		WithRiskJobsRepository(horizonrepo.NewRiskJobsRepository(db)).
		WithScoutingJobsRepository(horizonrepo.NewScoutingJobsRepository(agentJobsRepo))

	var out any
	switch strings.TrimSpace(*mode) {
	case "sync", "cleanup-legacy":
		if *apply {
			out, err = service.SyncTenant(ctx, strings.TrimSpace(*tenantID), *includeGlob, *limit)
		} else {
			out, err = service.PreviewSyncTenant(ctx, strings.TrimSpace(*tenantID), *includeGlob, *limit)
		}
	case "explain-subject":
		if strings.TrimSpace(*subjectID) == "" {
			log.Fatal("--subject-id is required")
		}
		out, err = service.SubjectDetail(ctx, strings.TrimSpace(*subjectID), *limit)
	case "explain-trigger":
		if strings.TrimSpace(*triggerID) == "" {
			log.Fatal("--trigger-id is required")
		}
		trigger, gerr := service.GetTrigger(ctx, strings.TrimSpace(*triggerID))
		if gerr != nil {
			err = gerr
			break
		}
		detail := map[string]any{"trigger": trigger}
		if trigger != nil && strings.TrimSpace(trigger.SubjectID) != "" {
			if subject, serr := service.SubjectDetail(ctx, trigger.SubjectID, *limit); serr == nil {
				detail["subject"] = subject
			}
		}
		out = detail
	case "replay-trigger":
		if strings.TrimSpace(*triggerID) == "" {
			log.Fatal("--trigger-id is required")
		}
		out, err = service.ReplayTrigger(ctx, strings.TrimSpace(*triggerID))
	case "metrics":
		out = service.MetricsSnapshot()
	default:
		log.Fatalf("unsupported mode %q", strings.TrimSpace(*mode))
	}
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatal(err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
