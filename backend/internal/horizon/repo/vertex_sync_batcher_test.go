package repo

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestVertexSyncBatcher_TimerFlushesSmallBatch(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		imported [][]string
	)
	done := make(chan struct{}, 1)

	b := NewVertexSyncBatcher(
		func(_ context.Context, uris []string) (string, error) {
			mu.Lock()
			cpy := append([]string(nil), uris...)
			imported = append(imported, cpy)
			mu.Unlock()
			select {
			case done <- struct{}{}:
			default:
			}
			return "op/test", nil
		},
		VertexSyncBatcherConfig{
			MaxBatchSize:  10,
			MinBatchSize:  5,
			FlushInterval: 20 * time.Millisecond,
		},
	)
	defer b.Close()

	b.Add("gs://bucket/global/horizon_fragments/f1.json")

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected timer-based flush for small batch, but no import happened")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(imported) != 1 {
		t.Fatalf("expected exactly one imported batch, got %d", len(imported))
	}
	if len(imported[0]) != 1 {
		t.Fatalf("expected 1 URI in imported batch, got %d", len(imported[0]))
	}
	if imported[0][0] != "gs://bucket/global/horizon_fragments/f1.json" {
		t.Fatalf("unexpected imported URI: %q", imported[0][0])
	}
}
