package internalknowledge

import (
	"context"
	"testing"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestShouldReconcileLensAllowsStaleQueuedHotStateWithoutAssemblyReady(t *testing.T) {
	svc := &WarmupService{cfg: WarmupConfig{StaleQueuedAfter: 5 * time.Minute}}
	now := time.Date(2026, time.April, 3, 18, 0, 0, 0, time.UTC)
	lens := &hmodels.Lens{
		ID:     "lens-1",
		Status: string(hmodels.LensStatusActive),
		ViewConfig: map[string]any{
			"internalKnowledge": map[string]any{
				"status":     internalKnowledgeStateQueued,
				"queuedAt":   now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
				"corpusId":   "test-corpus",
				"hotReadyAt": now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			},
		},
	}

	reason, ok := svc.shouldReconcileLens(lens, now)
	if !ok {
		t.Fatalf("expected stale queued lens to be eligible for reconcile")
	}
	if reason != "queued_reconcile" {
		t.Fatalf("unexpected reconcile reason: %s", reason)
	}
}

func TestShouldReconcileLensSkipsQueuedReconcileWhenFullIsAlreadyReady(t *testing.T) {
	svc := &WarmupService{cfg: WarmupConfig{StaleQueuedAfter: 5 * time.Minute}}
	now := time.Date(2026, time.April, 3, 18, 0, 0, 0, time.UTC)
	lens := &hmodels.Lens{
		ID:     "lens-3",
		Status: string(hmodels.LensStatusActive),
		ViewConfig: map[string]any{
			"internalKnowledge": map[string]any{
				"status":      internalKnowledgeStateQueued,
				"lastReason":  "queued_reconcile",
				"queuedAt":    now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
				"corpusId":    "test-corpus",
				"fullReadyAt": now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
				"completedAt": now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
				"availableTemperatures": []string{
					PreparedTemperatureHot,
					PreparedTemperatureWarm,
					PreparedTemperatureFull,
				},
			},
		},
	}

	if reason, ok := svc.shouldReconcileLens(lens, now); ok {
		t.Fatalf("expected full-ready queued reconcile state to be skipped, got reason=%s", reason)
	}
}

func TestShouldReconcileLensRejectsColdLensWithoutAssemblyOrWarmState(t *testing.T) {
	svc := &WarmupService{cfg: WarmupConfig{StaleQueuedAfter: 5 * time.Minute}}
	now := time.Date(2026, time.April, 3, 18, 0, 0, 0, time.UTC)
	lens := &hmodels.Lens{
		ID:         "lens-2",
		Status:     string(hmodels.LensStatusActive),
		ViewConfig: map[string]any{},
	}

	if reason, ok := svc.shouldReconcileLens(lens, now); ok {
		t.Fatalf("expected cold lens to stay ineligible, got reason=%s", reason)
	}
}

func TestFilterPreparedDocsAtTemperatureReturnsDeltaOnly(t *testing.T) {
	docs := []PreparedCorpusDoc{
		{ID: "hot-1", Temperature: PreparedTemperatureHot},
		{ID: "warm-1", Temperature: PreparedTemperatureWarm},
		{ID: "full-1", Temperature: PreparedTemperatureFull},
		{ID: "warm-2", Temperature: PreparedTemperatureWarm},
	}

	got := filterPreparedDocsAtTemperature(docs, PreparedTemperatureWarm)
	if len(got) != 2 {
		t.Fatalf("expected 2 warm docs, got %d", len(got))
	}
	if got[0].ID != "warm-1" || got[1].ID != "warm-2" {
		t.Fatalf("unexpected warm docs order/content: %+v", got)
	}

	full := filterPreparedDocsAtTemperature(docs, PreparedTemperatureFull)
	if len(full) != 1 || full[0].ID != "full-1" {
		t.Fatalf("expected single full doc, got %+v", full)
	}
}

func TestShouldDeferImportCompletion(t *testing.T) {
	if !shouldDeferImportCompletion(context.DeadlineExceeded) {
		t.Fatal("expected deadline exceeded to be deferrable")
	}
	if !shouldDeferImportCompletion(assertErr("vertex import timeout after 45m0s")) {
		t.Fatal("expected vertex import timeout to be deferrable")
	}
	if shouldDeferImportCompletion(assertErr("vertex operation failed: 13 internal")) {
		t.Fatal("did not expect real vertex failure to be deferrable")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
