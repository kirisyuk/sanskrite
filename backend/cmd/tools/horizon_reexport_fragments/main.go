package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/api/iterator"
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
		projectID     = flag.String("project", getenvDefault("GCP_PROJECT", "sanskrite"), "GCP project id")
		bucket        = flag.String("bucket", "", "GCS bucket for horizon fragments (default: ${PROJECT_ID}-horizon-fragments)")
		tenantID      = flag.String("tenant", "global", "TenantID for Horizon fragments (normally 'global')")
		limit         = flag.Int("limit", 0, "Max number of fragments to export (0 = no limit)")
		batchSize     = flag.Int("batch", 100, "Batch size for processing fragments")
		skipExisting  = flag.Bool("skip-existing", false, "Skip fragments that already exist in GCS (continue from where stopped)")
		heartbeat     = flag.Duration("heartbeat", 30*time.Second, "Log heartbeat while listing Firestore (0 = disable)")
		stallTimeout  = flag.Duration("stall-timeout", 0, "Abort if no progress for this duration (0 = disable)")
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
		"project=%s bucket=%s tenant=%s limit=%d batchSize=%d skipExisting=%v heartbeat=%v stallTimeout=%v",
		*projectID, *bucket, *tenantID, *limit, *batchSize, *skipExisting, *heartbeat, *stallTimeout,
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

	// List all fragments
	log.Printf("Listing fragments from Firestore...")
	query := fragmentsRepo.Query()
	if *limit > 0 {
		query = query.Limit(*limit)
	}

	iter := query.Documents(ctx)
	var count int
	var exported int
	var errCount int
	var unavailableCount int

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

	for {
		doc, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			// Firestore gRPC can temporarily return Unavailable. Without backoff this becomes a tight loop.
			if status.Code(err) == codes.Unavailable {
				unavailableCount++
				consecutiveUnavailable++
				now := time.Now()
				if lastErrLog.IsZero() || now.Sub(lastErrLog) > 5*time.Second {
					log.Printf("WARNING: Firestore Unavailable (consecutive=%d, total=%d). Backing off %v. Last error: %v", consecutiveUnavailable, unavailableCount, unavailableBackoff, err)
					lastErrLog = now
				}
				select {
				case <-ctx.Done():
					// Context canceled (e.g. stall-timeout). Exit cleanly without spamming logs.
					log.Printf("ERROR: Firestore iteration aborted while backing off: %v", ctx.Err())
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
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Printf("ERROR: Firestore iteration aborted: %v", err)
				break
			}
			log.Printf("ERROR: failed to iterate fragments: %v", err)
			errCount++
			// avoid busy loop on persistent failures
			time.Sleep(250 * time.Millisecond)
			continue
		}

		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			log.Printf("ERROR: failed to parse fragment %s: %v", doc.Ref.ID, err)
			errCount++
			continue
		}
		fragment.ID = doc.Ref.ID

		count++
		lastProgress = time.Now()
		// reset Unavailable backoff after successful progress
		consecutiveUnavailable = 0
		unavailableBackoff = 500 * time.Millisecond
		if !firstDocLogged {
			firstDocLogged = true
			log.Printf("First fragment received after %v", time.Since(startTime))
		}

		// Check if file already exists in GCS (if skip-existing flag is set)
		if *skipExisting {
			objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
			exists, err := gcsClient.ObjectExists(ctx, *bucket, objectPath)
			if err != nil {
				// Only log non-404 errors (permissions, network issues, etc.)
				// 404 errors are expected when object doesn't exist
				log.Printf("WARNING: failed to check if fragment %s exists in GCS: %v", fragment.ID, err)
			} else if exists {
				// Skip this fragment, it's already exported
				continue
			}
		}

		// Export using repository (will use new format, skip sync for batch export)
		if err := fragmentsRepo.ExportToGCS(ctx, &fragment, true); err != nil {
			log.Printf("ERROR: failed to export fragment %s (%s): %v", fragment.ID, fragment.CanonicalPath, err)
			errCount++
			continue
		}

		exported++

		if count%*batchSize == 0 {
			elapsed := time.Since(startTime)
			log.Printf("Progress: processed %d fragments, exported %d, errors %d (%.1f frags/sec)", 
				count, exported, errCount, float64(count)/elapsed.Seconds())
		}

		if *limit > 0 && count >= *limit {
			break
		}
	}
DONE:
	close(done)

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

