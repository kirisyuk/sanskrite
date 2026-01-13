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
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	updateTimeoutS = flag.Int("update-timeout-s", 25, "Per-document update timeout in seconds (prevents hanging forever; default: 25)")
	pageTimeoutS   = flag.Int("page-timeout-s", 60, "Per-page Firestore query timeout in seconds (prevents hanging on page fetch; default: 60)")

	refreshClientMinutes = flag.Int("refresh-client-minutes", 25, "Recreate Firestore client every N minutes (0 = never). Helps with long runs / token issues.")
	pageRetryMax         = flag.Int("page-retry-max", 5, "Max retries for a failed page fetch (transient errors)")

	touchTimestamps = flag.Bool("touch-timestamps", true, "When updating derived fields, also update updatedAt and metadata.lastUpdatedAt")

	dictVersion = flag.String("dict-version", "v1", "Market segment dictionary version to load (doc id)")
)

type stats struct {
	Scanned    int
	WouldWrite int
	Wrote      int
	Skipped    int
	Errors     int
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*tenantID) != "" && strings.TrimSpace(*tenantID) != "global" {
		log.Printf("WARNING: Horizon is global-only. Overriding --tenant=%q to \"global\".", strings.TrimSpace(*tenantID))
		*tenantID = "global"
	}
	if *pageSize <= 0 {
		*pageSize = 200
	}
	if *updateTimeoutS <= 0 {
		*updateTimeoutS = 25
	}
	if *pageTimeoutS <= 0 {
		*pageTimeoutS = 60
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
	defer closeClient()

	// Load market segment dictionary into memory (so NormalizeMarketSegments uses it).
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient, strings.TrimSpace(*dictVersion), 20*time.Minute)
	if err := taxonomy.RefreshMarketSegmentDictionary(ctx); err != nil {
		log.Printf("WARNING: failed to load market segment dictionary: %v (will use built-in rules)", err)
	}

	log.Printf("Starting Horizon marketSegmentNorms backfill (project=%s dry-run=%v page-size=%d tenant=%s dict=%s)...",
		strings.TrimSpace(*projectID), *dryRun, *pageSize, strings.TrimSpace(*tenantID), strings.TrimSpace(*dictVersion))

	var st stats
	var lastDocID string
	if strings.TrimSpace(*startAfter) != "" {
		lastDocID = strings.TrimSpace(*startAfter)
		log.Printf("INFO: Resuming from start-after=%s", lastDocID)
	}

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
		if shouldRefreshClient() {
			refreshClientNow(fmt.Sprintf("every %d minutes", *refreshClientMinutes))
		}

		col := fsClient.Collection("horizon_fragments")

		var pageDocs []*firestore.DocumentSnapshot
		var pageLastID string

		retries := 0
		for {
			log.Printf("INFO: fetching page (pageSize=%d startAfter=%s)", *pageSize, strings.TrimSpace(lastDocID))
			q := col.Where("tenantId", "==", "global").OrderBy(firestore.DocumentID, firestore.Asc).Limit(*pageSize)
			if strings.TrimSpace(lastDocID) != "" {
				q = q.StartAfter(lastDocID)
			}

			pctx, pcancel := context.WithTimeout(ctx, time.Duration(*pageTimeoutS)*time.Second)
			iter := q.Documents(pctx)
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
			pcancel()

			if iterErr == nil {
				break
			}

			retries++
			st.Errors++
			log.Printf("ERROR: page iteration failed after scanned=%d (retry %d/%d): %v", st.Scanned, retries, *pageRetryMax, iterErr)
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

			desiredNorms, desiredDomains, desiredVerticals, desiredPairs := taxonomy.NormalizeMarketSegments(frag.MarketSegments)

			// Normalize existing: compare by deep-equal.
			if reflect.DeepEqual(frag.MarketSegmentNorms, desiredNorms) &&
				reflect.DeepEqual(frag.MarketSegmentNormDomains, desiredDomains) &&
				reflect.DeepEqual(frag.MarketSegmentNormVerticals, desiredVerticals) &&
				reflect.DeepEqual(frag.MarketSegmentNormPairs, desiredPairs) {
				st.Skipped++
				continue
			}

			st.WouldWrite++
			if *dryRun {
				continue
			}

			if tick != nil {
				<-tick
			}

			now := time.Now().UTC()
			updates := []firestore.Update{
				{Path: "marketSegmentNorms", Value: desiredNorms},
				{Path: "marketSegmentNormDomains", Value: desiredDomains},
				{Path: "marketSegmentNormVerticals", Value: desiredVerticals},
				{Path: "marketSegmentNormPairs", Value: desiredPairs},
			}
			if *touchTimestamps {
				updates = append(updates,
					firestore.Update{Path: "updatedAt", Value: now},
					firestore.Update{Path: "metadata.lastUpdatedAt", Value: now},
				)
			}

			uctx, cancel := context.WithTimeout(ctx, time.Duration(*updateTimeoutS)*time.Second)
			_, err := doc.Ref.Update(uctx, updates)
			cancel()
			if err != nil {
				st.Errors++
				log.Printf("ERROR: update failed for %s: %v", doc.Ref.ID, err)
				// If this looks like a hung/expired RPC, refresh the client to recover.
				if strings.Contains(strings.ToLower(err.Error()), "deadline") {
					refreshClientNow("update deadline exceeded")
				}
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

		lastDocID = pageLastID
		log.Printf("Page done: scanned=%d wouldWrite=%d wrote=%d skipped=%d errors=%d (resume=start-after=%s)",
			st.Scanned, st.WouldWrite, st.Wrote, st.Skipped, st.Errors, lastDocID)

		if *sleepPerPageMs > 0 {
			time.Sleep(time.Duration(*sleepPerPageMs) * time.Millisecond)
		}
	}

	log.Printf("Done: scanned=%d wouldWrite=%d wrote=%d skipped=%d errors=%d", st.Scanned, st.WouldWrite, st.Wrote, st.Skipped, st.Errors)
}
