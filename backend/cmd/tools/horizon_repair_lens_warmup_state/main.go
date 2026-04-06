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
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID string
		lensID    string
		reason    string
	)

	flag.StringVar(&projectID, "project", firstNonEmpty(strings.TrimSpace(getenv("GCP_PROJECT_ID")), strings.TrimSpace(getenv("GOOGLE_CLOUD_PROJECT")), "horde-horizon-test-20260405"), "GCP project id")
	flag.StringVar(&lensID, "lens-id", "", "lens id to repair")
	flag.StringVar(&reason, "reason", "stalled_pending_import_manual_repair", "repair reason")
	flag.Parse()

	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	reason = strings.TrimSpace(reason)
	if lensID == "" {
		log.Fatal("--lens-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	lensesRepo := hrepo.NewLensesRepository(fs.Client())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	patch := map[string]any{
		"status":                         "queued",
		"queuedAt":                       now,
		"runningAt":                      "",
		"lastQueuedAt":                   now,
		"lastReason":                     reason,
		"lastError":                      "",
		"importOperation":                "",
		"pendingStageHash":               "",
		"pendingStageDocCount":           0,
		"pendingImportSince":             "",
		"pendingImportCreatedAt":         "",
		"pendingImportMetadataUpdatedAt": "",
		"pendingImportSuccessCount":      0,
		"pendingImportTotalCount":        0,
		"pendingImportLastCheckedAt":     "",
	}
	if err := lensesRepo.PatchInternalKnowledgeStateFields(ctx, lensID, patch); err != nil {
		log.Fatalf("repair patch failed: %v", err)
	}
	fmt.Printf("repaired lens=%s reason=%s queuedAt=%s\n", lensID, reason, now)
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(key)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
