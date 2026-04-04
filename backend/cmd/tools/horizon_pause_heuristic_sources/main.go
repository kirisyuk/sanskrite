package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

const defaultPauseReason = "focus_runtime_replaced"

func main() {
	var (
		projectID   = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
		apply       = flag.Bool("apply", false, "Apply changes")
		pauseReason = flag.String("pause-reason", defaultPauseReason, "Pause reason to write")
	)
	flag.Parse()

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	dsRepo := repo.NewDataSourcesRepository(fsClient.Client())
	sources, err := dsRepo.ListByKinds(ctx, []string{
		models.SourceKindRSS,
		models.SourceKindHorizonFeed,
		models.SourceKindWorldMonitorLegacy,
	})
	if err != nil {
		log.Fatalf("list sources failed: %v", err)
	}

	changed := 0
	candidates := 0
	now := time.Now().UTC()
	for _, ds := range sources {
		if ds == nil {
			continue
		}
		if !models.IsGlobalSystemSource(ds) || !models.UsesHeuristicProcessing(ds) {
			continue
		}
		candidates++
		if strings.EqualFold(strings.TrimSpace(ds.Status), "paused") && strings.TrimSpace(models.PauseReasonValue(ds)) == strings.TrimSpace(*pauseReason) {
			fmt.Printf("already-paused\t%s\t%s\t%s\n", ds.ID, ds.Kind, ds.DisplayName)
			continue
		}
		fmt.Printf("pause\t%s\t%s\t%s\tmode=%s\tstatus=%s\n", ds.ID, ds.Kind, ds.DisplayName, models.SourceProcessingModeValue(ds), ds.Status)
		if !*apply {
			continue
		}
		ds.Status = "paused"
		ds.PauseReason = strPtr(strings.TrimSpace(*pauseReason))
		ds.NextPollAt = nil
		ds.UpdatedAt = now
		ds.SyncCompatForWrite()
		if err := dsRepo.Upsert(ctx, ds); err != nil {
			log.Fatalf("pause source %s failed: %v", ds.ID, err)
		}
		changed++
	}

	log.Printf("heuristic source pause complete: candidates=%d changed=%d apply=%v", candidates, changed, *apply)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func strPtr(v string) *string {
	out := strings.TrimSpace(v)
	if out == "" {
		return nil
	}
	return &out
}
