package repo

import (
	"context"
	"log"
	"sync"
	"time"
)

// VertexSyncBatcher batches Vertex AI Search sync requests to avoid quota exhaustion.
// Instead of calling ImportDocuments for each fragment individually, it collects
// GCS URIs and flushes them in a single batch request.
type VertexSyncBatcher struct {
	mu sync.Mutex

	// Pending URIs to sync
	pendingURIs []string

	// Import function that accepts batch of URIs
	importFn func(ctx context.Context, uris []string) (operationName string, err error)

	// Configuration
	maxBatchSize  int           // Max URIs per batch (Discovery Engine limit is ~1000)
	flushInterval time.Duration // Time to wait before flushing partial batch
	minBatchSize  int           // Minimum batch size before flush (to avoid single-item batches)

	// Flush timer
	flushTimer *time.Timer
	timerMu    sync.Mutex

	// Stats
	totalBatches   int
	totalDocuments int
	totalErrors    int
}

// VertexSyncBatcherConfig configuration for the batcher.
type VertexSyncBatcherConfig struct {
	MaxBatchSize  int           // Default: 100
	FlushInterval time.Duration // Default: 5 seconds
	MinBatchSize  int           // Default: 1
}

// DefaultVertexSyncBatcherConfig returns sensible defaults.
func DefaultVertexSyncBatcherConfig() VertexSyncBatcherConfig {
	return VertexSyncBatcherConfig{
		MaxBatchSize:  100,           // Stay well under 1000 limit
		FlushInterval: 5 * time.Second, // Flush every 5s if not full
		MinBatchSize:  1,             // Flush even single items after interval
	}
}

// NewVertexSyncBatcher creates a new batcher.
func NewVertexSyncBatcher(
	importFn func(ctx context.Context, uris []string) (string, error),
	config VertexSyncBatcherConfig,
) *VertexSyncBatcher {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 1
	}

	return &VertexSyncBatcher{
		pendingURIs:   make([]string, 0, config.MaxBatchSize),
		importFn:      importFn,
		maxBatchSize:  config.MaxBatchSize,
		flushInterval: config.FlushInterval,
		minBatchSize:  config.MinBatchSize,
	}
}

// Add adds a GCS URI to the pending batch.
// If the batch reaches maxBatchSize, it triggers an immediate flush.
// Returns immediately (non-blocking).
func (b *VertexSyncBatcher) Add(gcsURI string) {
	b.mu.Lock()
	b.pendingURIs = append(b.pendingURIs, gcsURI)
	shouldFlush := len(b.pendingURIs) >= b.maxBatchSize
	b.mu.Unlock()

	if shouldFlush {
		go b.Flush()
	} else {
		b.scheduleFlush()
	}
}

// AddBatch adds multiple URIs at once.
func (b *VertexSyncBatcher) AddBatch(gcsURIs []string) {
	if len(gcsURIs) == 0 {
		return
	}

	b.mu.Lock()
	b.pendingURIs = append(b.pendingURIs, gcsURIs...)
	shouldFlush := len(b.pendingURIs) >= b.maxBatchSize
	b.mu.Unlock()

	if shouldFlush {
		go b.Flush()
	} else {
		b.scheduleFlush()
	}
}

// scheduleFlush schedules a flush after flushInterval if not already scheduled.
func (b *VertexSyncBatcher) scheduleFlush() {
	b.timerMu.Lock()
	defer b.timerMu.Unlock()

	if b.flushTimer != nil {
		return // Timer already scheduled
	}

	b.flushTimer = time.AfterFunc(b.flushInterval, func() {
		b.timerMu.Lock()
		b.flushTimer = nil
		b.timerMu.Unlock()
		b.Flush()
	})
}

// Flush sends all pending URIs to Vertex AI Search.
// Safe to call concurrently.
func (b *VertexSyncBatcher) Flush() {
	b.mu.Lock()
	if len(b.pendingURIs) < b.minBatchSize {
		b.mu.Unlock()
		return
	}

	// Take all pending URIs
	uris := b.pendingURIs
	b.pendingURIs = make([]string, 0, b.maxBatchSize)
	b.mu.Unlock()

	// Cancel any pending timer
	b.timerMu.Lock()
	if b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
	b.timerMu.Unlock()

	if len(uris) == 0 {
		return
	}

	// Execute import in batches of maxBatchSize
	for i := 0; i < len(uris); i += b.maxBatchSize {
		end := i + b.maxBatchSize
		if end > len(uris) {
			end = len(uris)
		}
		batch := uris[i:end]

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		opName, err := b.importFn(ctx, batch)
		cancel()

		b.mu.Lock()
		b.totalBatches++
		b.totalDocuments += len(batch)
		if err != nil {
			b.totalErrors++
			b.mu.Unlock()
			log.Printf("[VertexSyncBatcher] Batch import failed (count=%d): %v", len(batch), err)
		} else {
			b.mu.Unlock()
			log.Printf("[VertexSyncBatcher] Batch import success: count=%d operation=%s", len(batch), opName)
		}
	}
}

// FlushSync flushes synchronously and waits for completion.
func (b *VertexSyncBatcher) FlushSync() {
	// Temporarily set minBatchSize to 1 to force flush
	b.mu.Lock()
	oldMin := b.minBatchSize
	b.minBatchSize = 1
	b.mu.Unlock()

	b.Flush()

	b.mu.Lock()
	b.minBatchSize = oldMin
	b.mu.Unlock()
}

// Stats returns current statistics.
func (b *VertexSyncBatcher) Stats() (batches, documents, errors, pending int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totalBatches, b.totalDocuments, b.totalErrors, len(b.pendingURIs)
}

// PendingCount returns the number of pending URIs.
func (b *VertexSyncBatcher) PendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pendingURIs)
}

// Close flushes any remaining URIs and stops the batcher.
func (b *VertexSyncBatcher) Close() {
	b.timerMu.Lock()
	if b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
	b.timerMu.Unlock()

	// Final flush
	b.mu.Lock()
	b.minBatchSize = 1
	b.mu.Unlock()
	b.Flush()
}
