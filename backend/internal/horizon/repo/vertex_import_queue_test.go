package repo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func TestVertexImportIntentNormalizedDefaults(t *testing.T) {
	intent := (VertexImportIntent{
		URI:         " gs://bucket/path.json ",
		DataStoreID: " store ",
	}).Normalized()

	if intent.URI != "gs://bucket/path.json" {
		t.Fatalf("unexpected URI: %q", intent.URI)
	}
	if intent.DataStoreID != "store" {
		t.Fatalf("unexpected store: %q", intent.DataStoreID)
	}
	if intent.DataSchema != "document" {
		t.Fatalf("expected default schema=document, got %q", intent.DataSchema)
	}
	if intent.ReconciliationMode != "INCREMENTAL" {
		t.Fatalf("expected default reconciliation mode, got %q", intent.ReconciliationMode)
	}
	if intent.EnqueuedAt == "" {
		t.Fatal("expected enqueuedAt to be populated")
	}
}

func TestVertexImportBudgetRejectsResourceExhausted(t *testing.T) {
	budget := NewVertexImportBudget(VertexImportBudgetConfig{
		BaseImportsPerMinute: 8,
		MaxImportsPerMinute:  16,
		BaseInflightImports:  3,
		MaxInflightImports:   8,
		TargetPendingLRO:     3,
		Adaptive:             true,
	})
	permit, err := budget.AcquireImportPermit(context.Background(), "edges")
	if err != nil {
		t.Fatalf("acquire permit: %v", err)
	}
	permit.Reject(errors.New(`vertex AI search import API error 429: {"code":429,"message":"RESOURCE_EXHAUSTED"}`))

	stats := budget.Stats()
	if stats.TotalResourceExhausted != 1 {
		t.Fatalf("expected resource exhausted count=1, got %d", stats.TotalResourceExhausted)
	}
	if stats.CurrentImportsPerMinute >= 8 {
		t.Fatalf("expected rate to reduce after 429, got %.2f", stats.CurrentImportsPerMinute)
	}
	if stats.CurrentInflightCap >= 3 {
		t.Fatalf("expected inflight cap to reduce after 429, got %d", stats.CurrentInflightCap)
	}
	if stats.InflightOperations != 0 {
		t.Fatalf("expected inflight operations to be released, got %d", stats.InflightOperations)
	}
}

func TestVertexImportBudgetCompletesAndRecovers(t *testing.T) {
	budget := NewVertexImportBudget(VertexImportBudgetConfig{
		BaseImportsPerMinute: 2,
		MaxImportsPerMinute:  5,
		BaseInflightImports:  1,
		MaxInflightImports:   3,
		TargetPendingLRO:     2,
		Adaptive:             true,
	})

	for i := 0; i < 8; i++ {
		permit, err := budget.AcquireImportPermit(context.Background(), "fragments")
		if err != nil {
			t.Fatalf("acquire permit %d: %v", i, err)
		}
		permit.Complete(nil, 2*time.Second)
	}

	stats := budget.Stats()
	if stats.TotalCompleted != 8 {
		t.Fatalf("expected completed count=8, got %d", stats.TotalCompleted)
	}
	if stats.CurrentImportsPerMinute <= 2 {
		t.Fatalf("expected rate to recover above base, got %.2f", stats.CurrentImportsPerMinute)
	}
	if stats.CurrentInflightCap <= 1 {
		t.Fatalf("expected inflight cap to recover above base, got %d", stats.CurrentInflightCap)
	}
	if stats.AverageOperationLatency <= 0 {
		t.Fatalf("expected average latency to be recorded, got %s", stats.AverageOperationLatency)
	}
}

func TestVertexImportBudgetAcceptTracksOperationAsynchronously(t *testing.T) {
	budget := NewVertexImportBudget(VertexImportBudgetConfig{
		BaseImportsPerMinute: 2,
		MaxImportsPerMinute:  5,
		BaseInflightImports:  1,
		MaxInflightImports:   3,
		TargetPendingLRO:     2,
		Adaptive:             true,
		OperationTimeout:     2 * time.Second,
	})

	permit, err := budget.AcquireImportPermit(context.Background(), "warmup")
	if err != nil {
		t.Fatalf("acquire permit: %v", err)
	}
	permit.Accept("operations/import-123", func(ctx context.Context, operation string) (*ai.OperationStatus, error) {
		return &ai.OperationStatus{Done: true}, nil
	})

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		stats := budget.Stats()
		if stats.TotalAccepted == 1 && stats.TotalCompleted == 1 && stats.TotalRejected == 0 && stats.InflightOperations == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	stats := budget.Stats()
	t.Fatalf("expected async accept to complete operation, got %+v", stats)
}

func TestVertexImportControllerFiltersMissingObjectsBeforeImport(t *testing.T) {
	controller := &vertexImportController{
		key: "fragments|document|INCREMENTAL",
		worker: &VertexImportWorker{
			objectExists: func(ctx context.Context, uri string) (bool, error) {
				return !strings.Contains(uri, "missing"), nil
			},
		},
	}
	existingWaiter := &vertexImportTask{result: make(chan error, 1)}
	missingWaiter := &vertexImportTask{result: make(chan error, 1)}
	entries := []*vertexImportPending{
		{
			intent: VertexImportIntent{URI: "gs://bucket/existing.json"},
			waiters: []*vertexImportTask{existingWaiter},
		},
		{
			intent: VertexImportIntent{URI: "gs://bucket/missing.json"},
			waiters: []*vertexImportTask{missingWaiter},
		},
	}

	uris, waiters, skipped, err := controller.filterExistingEntries(entries)
	if err != nil {
		t.Fatalf("filter existing entries: %v", err)
	}
	if len(uris) != 1 || uris[0] != "gs://bucket/existing.json" {
		t.Fatalf("unexpected filtered uris: %+v", uris)
	}
	if len(waiters) != 1 || waiters[0] != existingWaiter {
		t.Fatalf("unexpected kept waiters: %+v", waiters)
	}
	if len(skipped) != 1 || skipped[0] != missingWaiter {
		t.Fatalf("unexpected skipped waiters: %+v", skipped)
	}
}
