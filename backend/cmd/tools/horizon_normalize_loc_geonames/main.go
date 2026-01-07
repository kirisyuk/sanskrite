package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID            = flag.String("project", "", "GCP project ID (required)")
	tenantID             = flag.String("tenant", "global", "Tenant ID (default: global)")
	limit                = flag.Int("limit", 0, "Max number of /loc fragments to process (0 = all)")
	maxScan              = flag.Int("max-scan", 100000, "Max fragments to scan before stopping (safety cap)")
	pageSize             = flag.Int("page-size", 200, "Firestore page size for scanning")
	startAfter           = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID")
	cursorFile           = flag.String("cursor-file", "", "Optional path to persist resume cursor. Loaded if --start-after is empty; saved periodically.")
	saveCursorEveryPages = flag.Int("save-cursor-every-pages", 5, "Persist cursor after every N pages when --cursor-file is set")

	apply  = flag.Bool("apply", false, "Apply changes (update fragments). Default: dry-run only.")
	dryRun = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")

	progressEvery        = flag.Int("progress-every", 50, "Print progress logs every N processed rows (0 disables)")
	progressScannedEvery = flag.Int("progress-scanned-every", 500, "Print progress logs every N scanned docs (0 disables)")
	heartbeatSec         = flag.Int("heartbeat-sec", 15, "Emit heartbeat logs every N seconds while running (0 disables)")

	skipConfidenceBelow = flag.Float64("skip-confidence-below", 0.8, "Skip applying normalization if confidence < this threshold")
)

// Telemetry counters
type stats struct {
	Scanned          int64
	Processed        int64
	Normalized       int64
	Unchanged        int64
	Ambiguous        int64
	Collisions       int64
	Errors           int64
	Applied          int64
	SkippedLowConf   int64
	GeoNamesAPICalls int64
}

func (s *stats) clone() stats {
	return stats{
		Scanned:          atomic.LoadInt64(&s.Scanned),
		Processed:        atomic.LoadInt64(&s.Processed),
		Normalized:       atomic.LoadInt64(&s.Normalized),
		Unchanged:        atomic.LoadInt64(&s.Unchanged),
		Ambiguous:        atomic.LoadInt64(&s.Ambiguous),
		Collisions:       atomic.LoadInt64(&s.Collisions),
		Errors:           atomic.LoadInt64(&s.Errors),
		Applied:          atomic.LoadInt64(&s.Applied),
		SkippedLowConf:   atomic.LoadInt64(&s.SkippedLowConf),
		GeoNamesAPICalls: atomic.LoadInt64(&s.GeoNamesAPICalls),
	}
}

type cursorState struct {
	StartAfter string    `json:"startAfter"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Scanned    int64     `json:"scanned,omitempty"`
	Processed  int64     `json:"processed,omitempty"`
}

// Row represents a single normalization result for telemetry/logging
type Row struct {
	FragmentID string   `json:"fragmentId"`
	OldPath    string   `json:"oldPath"`
	NewPath    string   `json:"newPath"`
	Changed    bool     `json:"changed"`
	Confidence float64  `json:"confidence"`
	Steps      []string `json:"steps,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Applied    bool     `json:"applied,omitempty"`
	Error      string   `json:"error,omitempty"`
}

var globalLastSeen atomic.Value // string

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *dryRun {
		*apply = false
	}
	if *pageSize <= 0 {
		*pageSize = 200
	}
	if *pageSize > 1000 {
		*pageSize = 1000
	}
	if *maxScan <= 0 {
		*maxScan = 100000
	}

	ctx := context.Background()

	// Load cursor from file if --start-after is empty
	if strings.TrimSpace(*startAfter) == "" && strings.TrimSpace(*cursorFile) != "" {
		if st, err := readCursorFile(*cursorFile); err == nil && strings.TrimSpace(st.StartAfter) != "" {
			*startAfter = st.StartAfter
			log.Printf("Loaded cursor from %s: start-after=%s", *cursorFile, *startAfter)
		}
	}

	// Firestore
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	// GeoNames client (cached)
	geonamesBaseClient := httphandlers.GetGeoNamesClientFromEnv()
	if geonamesBaseClient == nil {
		log.Fatal("FATAL: GEONAMES_USERNAME not set; cannot proceed")
	}
	geonamesAdapter := geonames.NewAdapter(geonamesBaseClient)
	externalCacheRepo := repo.NewExternalDataCacheRepository(db)
	cachedGeoNamesClient := geonames.NewCachedClient(geonamesAdapter, externalCacheRepo, geonames.CacheOptions{
		Enabled: true,
		TTL:     7 * 24 * time.Hour, // 7 days
	})

	// Fragments repo (for updates)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)

	// CanonicalPathResolver with GeoNames (nil policy and deduplicator - not needed for this tool)
	resolver := scout.NewCanonicalPathResolver(fragmentsRepo, nil, nil).WithGeoNamesClient(cachedGeoNamesClient)

	log.Printf("=== Horizon: Normalize /loc via GeoNames ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d maxScan=%d pageSize=%d skipConfidenceBelow=%.2f",
		*projectID, *tenantID, *apply, *limit, *maxScan, *pageSize, *skipConfidenceBelow)
	if strings.TrimSpace(*startAfter) != "" {
		log.Printf("resume cursor: start-after=%s", strings.TrimSpace(*startAfter))
	}

	// Signal handling for graceful shutdown with cursor print
	globalLastSeen.Store(strings.TrimSpace(*startAfter))
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		if v := globalLastSeen.Load(); v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				log.Printf("resume cursor: start-after=%s", strings.TrimSpace(s))
			}
		}
		log.Printf("FATAL: interrupted")
		os.Exit(130)
	}()

	rows, st, lastSeen, err := scan(ctx, db, fragmentsRepo, resolver)
	if err != nil {
		if strings.TrimSpace(lastSeen) != "" {
			log.Printf("resume cursor: start-after=%s", strings.TrimSpace(lastSeen))
		}
		log.Fatalf("FATAL: scan failed: %v", err)
	}

	// Print sample rows (first 20)
	printSampleRows(rows, 20)

	// Final stats
	stFinal := st.clone()
	log.Printf("\n=== FINAL STATS ===")
	log.Printf("scanned=%d processed=%d normalized=%d unchanged=%d ambiguous=%d collisions=%d errors=%d applied=%d skippedLowConf=%d geonamesAPICalls=%d",
		stFinal.Scanned, stFinal.Processed, stFinal.Normalized, stFinal.Unchanged, stFinal.Ambiguous, stFinal.Collisions, stFinal.Errors, stFinal.Applied, stFinal.SkippedLowConf, stFinal.GeoNamesAPICalls)

	if strings.TrimSpace(lastSeen) != "" {
		log.Printf("resume cursor: start-after=%s", strings.TrimSpace(lastSeen))
	}

	// Save final cursor
	if strings.TrimSpace(*cursorFile) != "" {
		_ = writeCursorFileAtomic(*cursorFile, cursorState{
			StartAfter: lastSeen,
			UpdatedAt:  time.Now().UTC(),
			Scanned:    stFinal.Scanned,
			Processed:  stFinal.Processed,
		})
	}

	if !*apply {
		log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
	}

	log.Printf("✓ Done.")
}

func scan(ctx context.Context, db *firestore.Client, fragmentsRepo *hrepo.HorizonFragmentsRepository, resolver *scout.CanonicalPathResolver) ([]Row, *stats, string, error) {
	col := db.Collection("horizon_fragments")

	st := &stats{}
	var rows []Row
	var rowsMu sync.Mutex

	cursor := strings.TrimSpace(*startAfter)
	lastSeen := cursor
	pages := 0
	transientErrs := 0
	const maxTransientErrs = 10

	// Heartbeat goroutine
	stopHeartbeat := make(chan struct{})
	if *heartbeatSec > 0 {
		go func() {
			t := time.NewTicker(time.Duration(*heartbeatSec) * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					c := st.clone()
					log.Printf("heartbeat: scanned=%d processed=%d normalized=%d unchanged=%d ambiguous=%d errors=%d applied=%d geonamesAPICalls=%d lastSeen=%s",
						c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Ambiguous, c.Errors, c.Applied, c.GeoNamesAPICalls, lastSeen)
				case <-stopHeartbeat:
					return
				}
			}
		}()
	}
	defer close(stopHeartbeat)

	for {
		if *limit > 0 && int(atomic.LoadInt64(&st.Processed)) >= *limit {
			break
		}
		if int(atomic.LoadInt64(&st.Scanned)) >= *maxScan {
			log.Printf("INFO: reached maxScan=%d, stopping", *maxScan)
			break
		}

		// Build query: /loc/* fragments only
		q := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("governance.status", "==", string(hmodels.FragmentStatusActive)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastSeen != "" {
			q = q.StartAfter(lastSeen)
		}

		iter := q.Documents(ctx)
		pageCount := 0
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				if isTransientFirestoreErr(err) && transientErrs < maxTransientErrs {
					transientErrs++
					backoff := time.Duration(1<<minInt(transientErrs, 5)) * time.Second
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					log.Printf("WARNING: transient Firestore error (retry %d/%d in %s): %v (cursor=%s)",
						transientErrs, maxTransientErrs, backoff, err, lastSeen)
					time.Sleep(backoff)
					pageCount = *pageSize // force continue
					break
				}
				return rows, st, lastSeen, fmt.Errorf("failed to iterate fragments: %w", err)
			}
			pageDocs = append(pageDocs, doc)
			pageCount++
		}
		iter.Stop()

		if len(pageDocs) == 0 {
			break
		}

		for _, doc := range pageDocs {
			atomic.AddInt64(&st.Scanned, 1)
			lastSeen = doc.Ref.ID
			globalLastSeen.Store(lastSeen)

			if *limit > 0 && int(atomic.LoadInt64(&st.Processed)) >= *limit {
				break
			}
			if int(atomic.LoadInt64(&st.Scanned)) > *maxScan {
				break
			}

			var frag hmodels.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				atomic.AddInt64(&st.Errors, 1)
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}

			// Filter: only /loc/* paths
			if !strings.HasPrefix(frag.CanonicalPath, "/loc/") {
				continue
			}

			atomic.AddInt64(&st.Processed, 1)

			// Run resolver
			result, err := resolver.Resolve(ctx, frag.CanonicalPath, hmodels.FragmentTypeGeoRegion)
			row := Row{
				FragmentID: frag.ID,
				OldPath:    frag.CanonicalPath,
			}
			if err != nil {
				atomic.AddInt64(&st.Errors, 1)
				row.Error = err.Error()
				row.Reason = "resolve_error"
				row.NewPath = frag.CanonicalPath
				row.Confidence = 1.0
			} else if result != nil {
				row.NewPath = result.CanonicalPath
				row.Confidence = result.Confidence
				row.Steps = result.NormalizationSteps
				row.Changed = result.CanonicalPath != frag.CanonicalPath

				if row.Changed {
					atomic.AddInt64(&st.Normalized, 1)
					row.Reason = "normalized"

					// Check confidence threshold
					if result.Confidence < *skipConfidenceBelow {
						atomic.AddInt64(&st.SkippedLowConf, 1)
						row.Reason = fmt.Sprintf("low_confidence (%.2f < %.2f)", result.Confidence, *skipConfidenceBelow)
					} else if *apply {
						// Avoid creating duplicate canonicalPath entries.
						if result.ExistingFragment != nil && strings.TrimSpace(result.ExistingFragment.ID) != "" && result.ExistingFragment.ID != frag.ID {
							atomic.AddInt64(&st.Collisions, 1)
							row.Reason = fmt.Sprintf("collision: target path already exists (id=%s)", strings.TrimSpace(result.ExistingFragment.ID))
							row.Applied = false
						} else {
							// Actually update the fragment
							frag.CanonicalPath = result.CanonicalPath
							frag.IndexPaths = hmodels.BuildIndexPaths(&frag, nil)
							if err := fragmentsRepo.Update(ctx, &frag); err != nil {
								atomic.AddInt64(&st.Errors, 1)
								row.Error = err.Error()
								row.Applied = false
							} else {
								atomic.AddInt64(&st.Applied, 1)
								row.Applied = true
							}
						}
					}
				} else {
					atomic.AddInt64(&st.Unchanged, 1)
					row.Reason = "unchanged"
				}
			} else {
				atomic.AddInt64(&st.Unchanged, 1)
				row.NewPath = frag.CanonicalPath
				row.Confidence = 1.0
				row.Reason = "no_result"
			}

			// Check if GeoNames was called (heuristic: steps contain "geonames")
			for _, step := range row.Steps {
				if strings.Contains(strings.ToLower(step), "geonames") {
					atomic.AddInt64(&st.GeoNamesAPICalls, 1)
					break
				}
			}

			rowsMu.Lock()
			rows = append(rows, row)
			rowsMu.Unlock()

			// Progress logging
			processed := int(atomic.LoadInt64(&st.Processed))
			if *progressEvery > 0 && processed%*progressEvery == 0 {
				c := st.clone()
				log.Printf("progress: scanned=%d processed=%d normalized=%d unchanged=%d errors=%d applied=%d lastDoc=%s",
					c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Errors, c.Applied, lastSeen)
			}
		}

		pages++
		// Periodic cursor save
		if strings.TrimSpace(*cursorFile) != "" && *saveCursorEveryPages > 0 && pages%*saveCursorEveryPages == 0 {
			c := st.clone()
			_ = writeCursorFileAtomic(*cursorFile, cursorState{
				StartAfter: lastSeen,
				UpdatedAt:  time.Now().UTC(),
				Scanned:    c.Scanned,
				Processed:  c.Processed,
			})
		}

		// Progress logging by scanned count
		scanned := int(atomic.LoadInt64(&st.Scanned))
		if *progressScannedEvery > 0 && scanned%*progressScannedEvery == 0 {
			c := st.clone()
			log.Printf("progress(scan): scanned=%d processed=%d normalized=%d unchanged=%d errors=%d applied=%d lastDoc=%s",
				c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Errors, c.Applied, lastSeen)
		}

		if pageCount < *pageSize {
			break
		}
	}

	return rows, st, lastSeen, nil
}

func printSampleRows(rows []Row, maxRows int) {
	if len(rows) == 0 {
		log.Printf("\nNo /loc fragments found or processed.")
		return
	}
	log.Printf("\n=== SAMPLE RESULTS (first %d) ===", minInt(len(rows), maxRows))
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for i, r := range rows {
		if i >= maxRows {
			break
		}
		fmt.Printf("---\n")
		_ = enc.Encode(r)
	}
}

func isTransientFirestoreErr(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
			return true
		}
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "wsarecv") ||
		strings.Contains(s, "forcibly closed") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "unavailable") {
		return true
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func readCursorFile(path string) (cursorState, error) {
	var st cursorState
	path = strings.TrimSpace(path)
	if path == "" {
		return st, fmt.Errorf("empty cursor file path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func writeCursorFileAtomic(path string, st cursorState) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
