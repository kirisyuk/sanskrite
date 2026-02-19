package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Re-export all Horizon fragments from Firestore to GCS in the new format
// compatible with Discovery Engine dataSchema=document

func main() {
	var (
		projectID       = flag.String("project", getenvDefault("GCP_PROJECT", "h1horizon"), "GCP project id")
		bucket          = flag.String("bucket", "", "GCS bucket for horizon fragments (default: ${PROJECT_ID}-horizon-fragments)")
		tenantID        = flag.String("tenant", "global", "TenantID for Horizon fragments (normally 'global')")
		limit           = flag.Int("limit", 0, "Max number of fragments to export (0 = no limit)")
		batchSize       = flag.Int("batch", 100, "Batch size for processing fragments")
		skipExisting    = flag.Bool("skip-existing", false, "Skip fragments that already exist in GCS (continue from where stopped)")
		heartbeat       = flag.Duration("heartbeat", 30*time.Second, "Log heartbeat while listing Firestore (0 = disable)")
		stallTimeout    = flag.Duration("stall-timeout", 0, "Abort if no progress for this duration (0 = disable)")
		checkpointFile  = flag.String("checkpoint-file", "/tmp/horizon_reexport_checkpoint.txt", "File to store resume checkpoint (last Firestore docID)")
		checkpointEvery = flag.Int("checkpoint-every", 100, "Write checkpoint every N processed docs (default 100)")
		startAfterDocID = flag.String("start-after", "", "Start after Firestore document ID (used with OrderBy(__name__))")
		resume          = flag.Bool("resume", false, "Resume from checkpoint-file (overrides --start-after if checkpoint exists)")
		pageSize        = flag.Int("page-size", 300, "Firestore page size per query (default 300). Lower reduces long-tail latency risk.")
	)
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *bucket == "" {
		*bucket = os.Getenv("HORIZON_FRAGMENTS_BUCKET")
	}
	if *bucket == "" {
		*bucket = fmt.Sprintf("%s-horizon-fragments", *projectID)
		log.Printf(`INFO: HORIZON_FRAGMENTS_BUCKET not set; defaulting to %q`, *bucket)
	}

	log.Printf("=== Horizon Fragments Re-export ===")
	log.Printf(
		"project=%s bucket=%s tenant=%s limit=%d batchSize=%d skipExisting=%v heartbeat=%v stallTimeout=%v checkpointFile=%s resume=%v startAfter=%q",
		*projectID, *bucket, *tenantID, *limit, *batchSize, *skipExisting, *heartbeat, *stallTimeout, *checkpointFile, *resume, *startAfterDocID,
	)

	// Firestore
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	// GCS
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init GCS client: %v", err)
	}

	// Repository with storage configured (will use new format automatically)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db).
		WithStorage(gcsClient, *bucket)

	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}
	if *resume {
		if cp, ok := readCheckpoint(*checkpointFile); ok && strings.TrimSpace(cp.LastDocID) != "" {
			*startAfterDocID = strings.TrimSpace(cp.LastDocID)
			log.Printf("Resume enabled: starting after checkpoint lastDocID=%q (processed=%d exported=%d errors=%d updatedAt=%s)",
				cp.LastDocID, cp.Processed, cp.Exported, cp.Errors, cp.UpdatedAt)
		} else {
			log.Printf("Resume enabled: checkpoint not found/empty (%s); starting from beginning", *checkpointFile)
		}
	}

	// List all fragments
	log.Printf("Listing fragments from Firestore...")
	var count int
	var exported int
	var errCount int
	var unavailableCount int
	var lastDocID string

	startTime := time.Now()
	lastProgress := startTime
	firstDocLogged := false
	lastErrLog := time.Time{}
	consecutiveUnavailable := 0
	unavailableBackoff := 500 * time.Millisecond
	maxUnavailableBackoff := 30 * time.Second

	done := make(chan struct{})
	if *heartbeat > 0 {
		go func() {
			t := time.NewTicker(*heartbeat)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					since := time.Since(lastProgress)
					elapsed := time.Since(startTime)
					log.Printf("Heartbeat: processed=%d exported=%d errors=%d unavailable=%d elapsed=%v sinceLastProgress=%v", count, exported, errCount, unavailableCount, elapsed, since)
					if *stallTimeout > 0 && since > *stallTimeout {
						log.Printf("ERROR: no progress for %v (> stall-timeout=%v). Canceling Firestore iterator.", since, *stallTimeout)
						cancel()
					}
				case <-done:
					return
				}
			}
		}()
	}

	// Page-based scan: avoids long-lived streaming iterator, which is sensitive to P99 minute-level latency.
	cursor := strings.TrimSpace(*startAfterDocID)
	remaining := *limit
	if *pageSize <= 0 {
		*pageSize = 300
	}

	for {
		if *limit > 0 && remaining <= 0 {
			break
		}
		q := db.Collection("horizon_fragments").Query.OrderBy(firestore.DocumentID, firestore.Asc)
		if cursor != "" {
			q = q.StartAfter(cursor)
		}
		ps := *pageSize
		if *limit > 0 && remaining < ps {
			ps = remaining
		}
		q = q.Limit(ps)

		// Give each page fetch a reasonable budget; if it times out, we retry with backoff.
		pageCtx, pageCancel := context.WithTimeout(ctx, 2*time.Minute)
		docs, err := q.Documents(pageCtx).GetAll()
		pageCancel()

		if err != nil {
			// Treat DeadlineExceeded similarly to transient Unavailable.
			code := status.Code(err)
			if errors.Is(err, context.DeadlineExceeded) || code == codes.DeadlineExceeded || code == codes.Unavailable {
				unavailableCount++
				consecutiveUnavailable++
				now := time.Now()
				if lastErrLog.IsZero() || now.Sub(lastErrLog) > 5*time.Second {
					log.Printf("WARNING: Firestore transient error (code=%s, consecutive=%d, total=%d). Backing off %v. Last error: %v",
						code.String(), consecutiveUnavailable, unavailableCount, unavailableBackoff, err)
					lastErrLog = now
				}
				select {
				case <-ctx.Done():
					log.Printf("ERROR: Firestore paging aborted: %v", ctx.Err())
					errCount++
					goto DONE
				case <-time.After(unavailableBackoff):
				}
				if unavailableBackoff < maxUnavailableBackoff {
					unavailableBackoff *= 2
					if unavailableBackoff > maxUnavailableBackoff {
						unavailableBackoff = maxUnavailableBackoff
					}
				}
				continue
			}

			if errors.Is(err, context.Canceled) {
				log.Printf("ERROR: Firestore paging aborted: %v", err)
				break
			}
			log.Printf("ERROR: failed to fetch page: %v", err)
			errCount++
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if len(docs) == 0 {
			break
		}

		// reset backoff after successful page fetch
		consecutiveUnavailable = 0
		unavailableBackoff = 500 * time.Millisecond

		for _, d := range docs {
			var fragment models.HorizonFragment
			if err := d.DataTo(&fragment); err != nil {
				log.Printf("ERROR: failed to parse fragment %s: %v", d.Ref.ID, err)
				errCount++
				continue
			}
			fragment.ID = d.Ref.ID

			count++
			if *limit > 0 {
				remaining--
			}
			lastProgress = time.Now()
			lastDocID = d.Ref.ID
			cursor = lastDocID

			if !firstDocLogged {
				firstDocLogged = true
				log.Printf("First fragment received after %v", time.Since(startTime))
			}

			// Tenant filter (in-memory).
			if tenant != "" && tenant != "all" && strings.TrimSpace(fragment.TenantID) != "" && strings.TrimSpace(fragment.TenantID) != tenant {
				maybeWriteCheckpoint(*checkpointFile, *checkpointEvery, lastDocID, count, exported, errCount)
				continue
			}

			// Check if file already exists in GCS (if skip-existing flag is set)
			if *skipExisting {
				objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
				exists, err := gcsClient.ObjectExists(ctx, *bucket, objectPath)
				if err != nil {
					log.Printf("WARNING: failed to check if fragment %s exists in GCS: %v", fragment.ID, err)
				} else if exists {
					maybeWriteCheckpoint(*checkpointFile, *checkpointEvery, lastDocID, count, exported, errCount)
					continue
				}
			}

			// Export using repository (will use new format, skip sync for batch export)
			if err := fragmentsRepo.ExportToGCS(ctx, &fragment, true); err != nil {
				log.Printf("ERROR: failed to export fragment %s (%s): %v", fragment.ID, fragment.CanonicalPath, err)
				errCount++
				maybeWriteCheckpoint(*checkpointFile, *checkpointEvery, lastDocID, count, exported, errCount)
				continue
			}

			exported++
			maybeWriteCheckpoint(*checkpointFile, *checkpointEvery, lastDocID, count, exported, errCount)

			if count%*batchSize == 0 {
				elapsed := time.Since(startTime)
				log.Printf("Progress: processed %d fragments, exported %d, errors %d (%.1f frags/sec)",
					count, exported, errCount, float64(count)/elapsed.Seconds())
			}
		}
	}
DONE:
	close(done)
	// Final checkpoint on exit.
	if strings.TrimSpace(*checkpointFile) != "" && strings.TrimSpace(lastDocID) != "" {
		_ = writeCheckpoint(*checkpointFile, lastDocID, count, exported, errCount)
	}

	elapsed := time.Since(startTime)
	log.Printf("=== Re-export completed ===")
	log.Printf("Total processed: %d", count)
	log.Printf("Successfully exported: %d", exported)
	log.Printf("Errors: %d", errCount)
	log.Printf("Time elapsed: %v (%.1f frags/sec)", elapsed, float64(count)/elapsed.Seconds())
}

func getenvDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

type checkpoint struct {
	LastDocID string
	Processed int
	Exported  int
	Errors    int
	UpdatedAt string
}

func readCheckpoint(path string) (checkpoint, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return checkpoint{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return checkpoint{}, false
	}
	lines := strings.Split(string(b), "\n")
	cp := checkpoint{}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || !strings.Contains(ln, "=") {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "lastDocID":
			cp.LastDocID = v
		case "processed":
			if n, _ := strconv.Atoi(v); n >= 0 {
				cp.Processed = n
			}
		case "exported":
			if n, _ := strconv.Atoi(v); n >= 0 {
				cp.Exported = n
			}
		case "errors":
			if n, _ := strconv.Atoi(v); n >= 0 {
				cp.Errors = n
			}
		case "updatedAt":
			cp.UpdatedAt = v
		}
	}
	if strings.TrimSpace(cp.LastDocID) == "" {
		return checkpoint{}, false
	}
	return cp, true
}

func maybeWriteCheckpoint(path string, every int, lastDocID string, processed, exported, errCount int) {
	path = strings.TrimSpace(path)
	lastDocID = strings.TrimSpace(lastDocID)
	if path == "" || lastDocID == "" {
		return
	}
	if every <= 0 || processed <= 0 {
		return
	}
	if processed%every != 0 {
		return
	}
	_ = writeCheckpoint(path, lastDocID, processed, exported, errCount)
}

func writeCheckpoint(path string, lastDocID string, processed, exported, errCount int) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	content := fmt.Sprintf(
		"lastDocID=%s\nprocessed=%d\nexported=%d\nerrors=%d\nupdatedAt=%s\n",
		lastDocID,
		processed,
		exported,
		errCount,
		time.Now().UTC().Format(time.RFC3339),
	)
	return os.WriteFile(path, []byte(content), 0o644)
}
