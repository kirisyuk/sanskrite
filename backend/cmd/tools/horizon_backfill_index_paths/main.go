package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (Horizon is global-only; default: global)")

	pageSize   = flag.Int("page-size", 200, "Page size for Firestore pagination (recommended: 100-500)")
	limit      = flag.Int("limit", 0, "Limit number of documents to process (0 = all)")
	startAfter = flag.String("start-after", "", "Resume pagination after this document ID (__name__). Useful after restarts.")

	dryRun = flag.Bool("dry-run", true, "Dry run mode: only report would-change counts (no writes)")

	sleepPerPageMs = flag.Int("sleep-per-page-ms", 250, "Sleep between pages (ms) to avoid hammering Firestore")
	updateQPS      = flag.Float64("update-qps", 10, "Max updates per second (0 = no throttle)")

	refreshClientMinutes = flag.Int("refresh-client-minutes", 25, "Recreate Firestore client every N minutes (0 = never). Helps with long runs / token issues.")
	pageRetryMax         = flag.Int("page-retry-max", 5, "Max retries for a failed page fetch (transient errors)")

	touchTimestamps = flag.Bool("touch-timestamps", true, "When updating indexPaths, also update updatedAt and metadata.lastUpdatedAt")
)

type stats struct {
	Scanned    int
	WouldWrite int
	Wrote      int
	Skipped    int
	Errors     int
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	// Horizon fragments are global-only; keep flag for UX but enforce.
	if strings.TrimSpace(*tenantID) != "" && strings.TrimSpace(*tenantID) != "global" {
		log.Printf("WARNING: Horizon is global-only. Overriding --tenant=%q to \"global\".", strings.TrimSpace(*tenantID))
		*tenantID = "global"
	}

	if *pageSize <= 0 {
		*pageSize = 200
	}
	if *pageSize > 1000 {
		log.Printf("WARNING: page-size=%d is large; consider 200-500 to reduce latency spikes.", *pageSize)
	}

	ctx := context.Background()

	var fsClient *firestore.Client
	var clientCreatedAt time.Time
	mustEnsureClient := func() {
		if fsClient != nil {
			return
		}
		c, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
		if err != nil {
			log.Fatalf("Failed to create Firestore client: %v", err)
		}
		fsClient = c
		clientCreatedAt = time.Now()
	}
	closeClient := func() {
		if fsClient != nil {
			_ = fsClient.Close()
		}
		fsClient = nil
	}
	refreshClientNow := func(reason string) {
		closeClient()
		mustEnsureClient()
		if reason != "" {
			log.Printf("INFO: Firestore client refreshed (%s)", reason)
		} else {
			log.Printf("INFO: Firestore client refreshed")
		}
	}
	shouldRefreshClient := func() bool {
		if fsClient == nil {
			return true
		}
		if *refreshClientMinutes <= 0 {
			return false
		}
		if time.Since(clientCreatedAt) < time.Duration(*refreshClientMinutes)*time.Minute {
			return false
		}
		return true
	}

	mustEnsureClient()
	defer func() {
		closeClient()
	}()

	log.Printf("Starting Horizon indexPaths backfill (project=%s dry-run=%v page-size=%d tenant=%s)...",
		strings.TrimSpace(*projectID), *dryRun, *pageSize, strings.TrimSpace(*tenantID))

	var st stats
	var lastDocID string
	if strings.TrimSpace(*startAfter) != "" {
		lastDocID = strings.TrimSpace(*startAfter)
		log.Printf("INFO: Resuming from start-after=%s", lastDocID)
	}

	// Update rate limiter
	var tick <-chan time.Time
	if *updateQPS > 0 {
		interval := time.Duration(float64(time.Second) / *updateQPS)
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}

	for {
		// IMPORTANT: refresh the client only between pages (never during an active iterator).
		if shouldRefreshClient() {
			refreshClientNow(fmt.Sprintf("every %d minutes", *refreshClientMinutes))
		}

		col := fsClient.Collection("horizon_fragments")

		var pageDocs []*firestore.DocumentSnapshot
		var pageLastID string

		// Fetch a page with retry on transient failures.
		retries := 0
		for {
			q := col.Where("tenantId", "==", "global").OrderBy(firestore.DocumentID, firestore.Asc).Limit(*pageSize)
			if strings.TrimSpace(lastDocID) != "" {
				q = q.StartAfter(lastDocID)
			}

			iter := q.Documents(ctx)
			pageDocs = make([]*firestore.DocumentSnapshot, 0, *pageSize)
			var iterErr error
			for {
				doc, err := iter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					iterErr = err
					break
				}
				pageDocs = append(pageDocs, doc)
			}
			iter.Stop()

			if iterErr == nil {
				break
			}

			retries++
			st.Errors++
			log.Printf("ERROR: page iteration failed after scanned=%d (retry %d/%d): %v", st.Scanned, retries, *pageRetryMax, iterErr)

			// Refresh client and retry same page (do NOT advance lastDocID).
			refreshClientNow("retry after iteration error")

			if *pageRetryMax > 0 && retries >= *pageRetryMax {
				log.Printf("FATAL: exceeded page retries (%d). Stopping.", *pageRetryMax)
				return
			}
			time.Sleep(time.Duration(minInt(1000, 150*retries)) * time.Millisecond)
		}

		if len(pageDocs) == 0 {
			break
		}

		pageLastID = pageDocs[len(pageDocs)-1].Ref.ID

		for _, doc := range pageDocs {
			if doc == nil || !doc.Exists() {
				continue
			}
			if *limit > 0 && st.Scanned >= *limit {
				break
			}
			st.Scanned++

			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				st.Errors++
				log.Printf("WARNING: failed to decode fragment %s: %v", doc.Ref.ID, err)
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}

			desired := models.BuildIndexPaths(&frag, nil)

			// Normalize existing to avoid meaningless order diffs.
			existingNorm := models.NormalizeIndexPaths(models.MaxIndexPaths, frag.IndexPaths)
			desiredNorm := models.NormalizeIndexPaths(models.MaxIndexPaths, desired)

			if reflect.DeepEqual(existingNorm, desiredNorm) {
				st.Skipped++
				continue
			}

			st.WouldWrite++
			if *dryRun {
				continue
			}

			// Throttle updates (best-effort).
			if tick != nil {
				<-tick
			}

			now := time.Now().UTC()
			updates := []firestore.Update{
				{Path: "indexPaths", Value: desiredNorm},
			}
			if *touchTimestamps {
				updates = append(updates,
					firestore.Update{Path: "updatedAt", Value: now},
					firestore.Update{Path: "metadata.lastUpdatedAt", Value: now},
				)
			}

			if _, err := doc.Ref.Update(ctx, updates); err != nil {
				st.Errors++
				log.Printf("ERROR: update failed for %s: %v", doc.Ref.ID, err)
				continue
			}
			st.Wrote++

			if st.Wrote%250 == 0 {
				log.Printf("Progress: scanned=%d wrote=%d skipped=%d errors=%d (last=%s)", st.Scanned, st.Wrote, st.Skipped, st.Errors, pageLastID)
			}
		}

		if *limit > 0 && st.Scanned >= *limit {
			break
		}

		// Advance cursor only after successful page processing.
		lastDocID = pageLastID

		if *sleepPerPageMs > 0 {
			time.Sleep(time.Duration(*sleepPerPageMs) * time.Millisecond)
		}

		// Periodic progress line per page.
		log.Printf("Page done: scanned=%d wouldWrite=%d wrote=%d skipped=%d errors=%d (cursor=%s)", st.Scanned, st.WouldWrite, st.Wrote, st.Skipped, st.Errors, lastDocID)
	}

	log.Println("\n=== Backfill Summary ===")
	log.Printf("Scanned:    %d", st.Scanned)
	log.Printf("WouldWrite: %d", st.WouldWrite)
	log.Printf("Wrote:      %d", st.Wrote)
	log.Printf("Skipped:    %d", st.Skipped)
	log.Printf("Errors:     %d", st.Errors)
	log.Printf("DryRun:     %v", *dryRun)

	if *dryRun {
		log.Println("\nRun with --dry-run=false to apply changes.")
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
