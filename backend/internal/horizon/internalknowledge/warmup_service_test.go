package internalknowledge

import (
	"context"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
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

func TestShouldRetryWarmStateWrite(t *testing.T) {
	if !shouldRetryWarmStateWrite(context.DeadlineExceeded) {
		t.Fatal("expected deadline exceeded to be retryable")
	}
	if !shouldRetryWarmStateWrite(grpcstatus.Error(codes.Unavailable, "firestore unavailable")) {
		t.Fatal("expected grpc unavailable to be retryable")
	}
	if shouldRetryWarmStateWrite(assertErr("permission denied")) {
		t.Fatal("did not expect permission errors to be retryable")
	}
}

func TestPendingImportStillActiveTreatsFreshManagedBatchAsLive(t *testing.T) {
	// pendingImportStillActive compares metadata times to time.Now(); keep offsets relative to "now".
	now := time.Now().UTC()
	svc := &WarmupService{
		cfg: WarmupConfig{
			ImportWaitTimeout: 45 * time.Minute,
			StaleRunningAfter: 45 * time.Minute,
		},
	}
	lens := &hmodels.Lens{ID: "lens-1"}
	state := map[string]any{
		"importOperation":    "operations/import-1",
		"pendingImportSince": now.Add(-20 * time.Minute).Format(time.RFC3339Nano),
	}
	status := &ai.OperationStatus{
		Done: false,
		Metadata: map[string]any{
			"createTime":   now.Add(-22 * time.Minute).Format(time.RFC3339Nano),
			"updateTime":   now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			"successCount": 5,
			"totalCount":   28,
		},
	}

	if !svc.pendingImportStillActive(lens, state, status) {
		t.Fatal("expected fresh managed batch import to be treated as active")
	}
}

func TestPendingImportStillActiveAllowsRecoveryWhenManagedBatchLooksStalled(t *testing.T) {
	now := time.Now().UTC()
	svc := &WarmupService{
		cfg: WarmupConfig{
			ImportWaitTimeout: 30 * time.Minute,
			StaleRunningAfter: 45 * time.Minute,
		},
	}
	lens := &hmodels.Lens{ID: "lens-2"}
	state := map[string]any{
		"importOperation":    "operations/import-2",
		"pendingImportSince": now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
	}
	status := &ai.OperationStatus{
		Done: false,
		Metadata: map[string]any{
			"createTime":   now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
			"updateTime":   now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
			"successCount": 5,
			"totalCount":   28,
		},
	}

	if svc.pendingImportStillActive(lens, state, status) {
		t.Fatal("expected obviously stale managed batch to be eligible for recovery")
	}
}

func TestParseVertexImportProgress(t *testing.T) {
	now := time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC)
	progress := parseVertexImportProgress(&ai.OperationStatus{
		Metadata: map[string]any{
			"createTime":   now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
			"updateTime":   now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			"successCount": "7",
			"totalCount":   12,
		},
	})
	if progress.SuccessCount != 7 || progress.TotalCount != 12 {
		t.Fatalf("unexpected progress counts: %+v", progress)
	}
	if progress.CreateTime.IsZero() || progress.UpdateTime.IsZero() {
		t.Fatalf("expected progress times to parse, got %+v", progress)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
