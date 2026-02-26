package repo

import (
	"context"
	"log"
	"math/rand"
	"strings"
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
	pendingSet  map[string]struct{} // de-dup within the pending batch

	// Import function that accepts batch of URIs
	importFn func(ctx context.Context, uris []string) (operationName string, err error)

	// Configuration
	maxBatchSize  int           // Max URIs per batch (Discovery Engine limit is ~1000)
	flushInterval time.Duration // Time to wait before flushing partial batch
	minBatchSize  int           // Minimum batch size before flush (to avoid single-item batches)

	// Flush timer
	flushTimer *time.Timer
	timerMu    sync.Mutex

	// Ensure at most one flush is executing at a time.
	flushMu  sync.Mutex
	flushing bool

	// Backoff on rate-limit/quota errors (429).
	nextAllowed time.Time
	backoff     time.Duration

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
		MaxBatchSize:  90,               // Discovery Engine limit is 100; use 90 for safety margin
		FlushInterval: 30 * time.Second, // Reduce request rate under bursty updates
		MinBatchSize:  25,               // Avoid single-item batches (common spam source)
	}
}

// Discovery Engine hard limit for gcsSource.inputUris (dataSchema=document).
const discoveryEngineMaxInputUris = 100

// NewVertexSyncBatcher creates a new batcher.
func NewVertexSyncBatcher(
	importFn func(ctx context.Context, uris []string) (string, error),
	config VertexSyncBatcherConfig,
) *VertexSyncBatcher {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 90
	}
	if config.MaxBatchSize > discoveryEngineMaxInputUris {
		config.MaxBatchSize = discoveryEngineMaxInputUris
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 1
	}

	return &VertexSyncBatcher{
		pendingURIs:   make([]string, 0, config.MaxBatchSize),
		pendingSet:    make(map[string]struct{}, config.MaxBatchSize),
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
	gcsURI = strings.TrimSpace(gcsURI)
	if gcsURI == "" {
		return
	}
	b.mu.Lock()
	if _, ok := b.pendingSet[gcsURI]; ok {
		b.mu.Unlock()
		// Ensure a flush is scheduled even if we got a duplicate.
		b.scheduleFlush()
		return
	}
	b.pendingURIs = append(b.pendingURIs, gcsURI)
	b.pendingSet[gcsURI] = struct{}{}
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
	for _, u := range gcsURIs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := b.pendingSet[u]; ok {
			continue
		}
		b.pendingURIs = append(b.pendingURIs, u)
		b.pendingSet[u] = struct{}{}
	}
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
	b.scheduleFlushAfter(b.flushInterval, false)
}

// scheduleFlushAfter schedules a flush after the given duration.
// If forceReset is true, it will replace any existing timer.
func (b *VertexSyncBatcher) scheduleFlushAfter(d time.Duration, forceReset bool) {
	b.timerMu.Lock()
	defer b.timerMu.Unlock()

	if d <= 0 {
		d = b.flushInterval
	}
	if b.flushTimer != nil && !forceReset {
		return // Timer already scheduled
	}
	if b.flushTimer != nil && forceReset {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}

	b.flushTimer = time.AfterFunc(d, func() {
		b.timerMu.Lock()
		b.flushTimer = nil
		b.timerMu.Unlock()
		b.flush(true)
	})
}

// Flush sends all pending URIs to Vertex AI Search.
// Safe to call concurrently.
func (b *VertexSyncBatcher) Flush() {
	b.flush(false)
}

func (b *VertexSyncBatcher) flush(force bool) {
	// Ensure only one flush runs at a time (prevents import spamming under high concurrency).
	b.flushMu.Lock()
	if b.flushing {
		b.flushMu.Unlock()
		return
	}
	b.flushing = true
	b.flushMu.Unlock()
	defer func() {
		b.flushMu.Lock()
		b.flushing = false
		b.flushMu.Unlock()
	}()

	b.mu.Lock()
	// Respect backoff window (typically from 429 responses).
	if !b.nextAllowed.IsZero() {
		now := time.Now()
		if now.Before(b.nextAllowed) {
			wait := b.nextAllowed.Sub(now)
			b.mu.Unlock()
			b.scheduleFlushAfter(wait, true)
			return
		}
	}
	if len(b.pendingURIs) == 0 {
		b.mu.Unlock()
		return
	}
	if !force && len(b.pendingURIs) < b.minBatchSize {
		b.mu.Unlock()
		// Keep an eventual timer-based forced flush for low-volume traffic.
		b.scheduleFlush()
		return
	}

	// Take all pending URIs
	uris := b.pendingURIs
	b.pendingURIs = make([]string, 0, b.maxBatchSize)
	b.pendingSet = make(map[string]struct{}, b.maxBatchSize)
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
			// Backoff + requeue on 429 to avoid hammering Vertex AI Search import API.
			if isRateLimit429(err) {
				// exponential backoff: 5s, 10s, 20s, ... up to 2m (with jitter)
				if b.backoff <= 0 {
					b.backoff = 5 * time.Second
				} else {
					b.backoff = b.backoff * 2
				}
				if b.backoff > 2*time.Minute {
					b.backoff = 2 * time.Minute
				}
				jitter := time.Duration(rand.Intn(500)) * time.Millisecond
				b.nextAllowed = time.Now().Add(b.backoff + jitter)

				// Requeue failed batch (dedup against current pending).
				for _, u := range batch {
					if _, ok := b.pendingSet[u]; ok {
						continue
					}
					b.pendingURIs = append(b.pendingURIs, u)
					b.pendingSet[u] = struct{}{}
				}
				b.mu.Unlock()
				log.Printf("[VertexSyncBatcher] Batch import failed (count=%d): %v", len(batch), err)
				b.scheduleFlushAfter(b.backoff, true)
				return
			}
			b.mu.Unlock()
			log.Printf("[VertexSyncBatcher] Batch import failed (count=%d): %v", len(batch), err)
		} else {
			// Success: clear backoff.
			b.backoff = 0
			b.nextAllowed = time.Time{}
			b.mu.Unlock()
			log.Printf("[VertexSyncBatcher] Batch import success: count=%d operation=%s", len(batch), opName)
		}
	}
}

func isRateLimit429(err error) bool {
	if err == nil {
		return false
	}
	// We wrap errors as: "vertex AI search import API error 429: ..." in the client.
	// Also accept common variants.
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "error 429") ||
		strings.Contains(msg, "code\": 429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "resource_exhausted")
}

// FlushSync flushes synchronously and waits for completion.
func (b *VertexSyncBatcher) FlushSync() {
	b.flush(true)
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
	b.flush(true)
}
