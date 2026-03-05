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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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

	// Optional: configure GCS + Vertex so deletes and updates keep search index consistent (apply mode).
	vertexStoreID  = flag.String("vertex-store", "", "Vertex AI Search data store ID (default from env or prod)")
	vertexLoc      = flag.String("vertex-location", "", "Vertex AI Search location (default from env or prod)")
	ragBucket      = flag.String("rag-bucket", "", "GCS bucket used for Vertex indexing (default from env or prod)")
	skipVertexSync = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync triggers (avoid 429); still writes Firestore+GCS. Run a bulk import separately.")

	// GeoNames attachment (optional backfill on existing fragments).
	applyGeoNamesID = flag.Bool("apply-geonames-id", false, "Also attach GeoNames ID (identifiers.geonamesId + KBRef) to /loc fragments with city-like depth when missing (apply mode only)")
)

// Telemetry counters
type stats struct {
	Scanned             int64
	Processed           int64
	Normalized          int64
	Unchanged           int64
	Ambiguous           int64
	Collisions          int64
	Deduped             int64
	DuplicatesDeleted   int64
	EdgesReassigned     int64
	EdgesDeleted        int64
	FramesMigrated      int64
	Errors              int64
	Applied             int64
	SkippedLowConf      int64
	GeoNamesBaseCalls   int64
	GeoNamesIDsAttached int64
}

func (s *stats) clone() stats {
	return stats{
		Scanned:             atomic.LoadInt64(&s.Scanned),
		Processed:           atomic.LoadInt64(&s.Processed),
		Normalized:          atomic.LoadInt64(&s.Normalized),
		Unchanged:           atomic.LoadInt64(&s.Unchanged),
		Ambiguous:           atomic.LoadInt64(&s.Ambiguous),
		Collisions:          atomic.LoadInt64(&s.Collisions),
		Deduped:             atomic.LoadInt64(&s.Deduped),
		DuplicatesDeleted:   atomic.LoadInt64(&s.DuplicatesDeleted),
		EdgesReassigned:     atomic.LoadInt64(&s.EdgesReassigned),
		EdgesDeleted:        atomic.LoadInt64(&s.EdgesDeleted),
		FramesMigrated:      atomic.LoadInt64(&s.FramesMigrated),
		Errors:              atomic.LoadInt64(&s.Errors),
		Applied:             atomic.LoadInt64(&s.Applied),
		SkippedLowConf:      atomic.LoadInt64(&s.SkippedLowConf),
		GeoNamesBaseCalls:   atomic.LoadInt64(&s.GeoNamesBaseCalls),
		GeoNamesIDsAttached: atomic.LoadInt64(&s.GeoNamesIDsAttached),
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

type countingGeoNames struct {
	base coreontology.GeoNamesClientInterface
	n    atomic.Int64
}

func (c *countingGeoNames) Search(ctx context.Context, query string, maxResults int, opts coreontology.GeoNamesSearchOptions) (*coreontology.GeoNamesSearchResponse, error) {
	c.n.Add(1)
	return c.base.Search(ctx, query, maxResults, opts)
}
func (c *countingGeoNames) GetPlace(ctx context.Context, geonameID int) (*coreontology.GeoNamesPlace, error) {
	c.n.Add(1)
	return c.base.GetPlace(ctx, geonameID)
}
func (c *countingGeoNames) GetHierarchy(ctx context.Context, geonameID int) (*coreontology.GeoNamesHierarchyResponse, error) {
	c.n.Add(1)
	return c.base.GetHierarchy(ctx, geonameID)
}
func (c *countingGeoNames) GetCountryCode(ctx context.Context, lat, lon float64) (string, error) {
	c.n.Add(1)
	return c.base.GetCountryCode(ctx, lat, lon)
}

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
	geoCount := &countingGeoNames{base: geonamesAdapter}
	externalCacheRepo := coreRepo.NewExternalDataCacheRepository(db)
	cachedGeoNamesClient := geonames.NewCachedClient(geoCount, externalCacheRepo, geonames.CacheOptions{
		Enabled: true,
		TTL:     7 * 24 * time.Hour, // 7 days
	})

	// Defaults consistent with production Cloud Run (used in apply mode so deletes/updates sync to index).
	if *vertexStoreID == "" {
		*vertexStoreID = os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	}
	if *vertexStoreID == "" {
		*vertexStoreID = "rag-documents-store"
	}
	if *vertexLoc == "" {
		*vertexLoc = os.Getenv("VERTEX_SEARCH_LOCATION")
	}
	if *vertexLoc == "" {
		*vertexLoc = "global"
	}
	if *ragBucket == "" {
		*ragBucket = os.Getenv("RAG_DOCUMENTS_BUCKET")
	}
	if *ragBucket == "" {
		*ragBucket = fmt.Sprintf("%s-rag-documents", strings.TrimSpace(*projectID))
	}

	edgesRepo := hrepo.NewDependencyEdgesRepository(db)
	framesRepo := hrepo.NewFramesRepository(db)

	var gcsClient *gcp.GCSClient
	var vertexSearchClient *ai.VertexAISearchClient
	if *apply {
		gcsClient, err = gcp.NewGCSClient(ctx, *projectID)
		if err != nil {
			log.Fatalf("FATAL: failed to init GCS client: %v", err)
		}
		vertexSearchClient = ai.NewVertexAISearchClient(*projectID, *vertexLoc, *vertexStoreID, "")
		if vertexSearchClient == nil || !vertexSearchClient.IsConfigured() {
			log.Printf("WARNING: Vertex AI Search client not configured; will proceed, but index sync may lag.")
		}
	}

	// Fragments repo (for updates/deletes). Configure storage+vertex sync in apply mode.
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	if *apply && gcsClient != nil {
		fragmentsRepo = fragmentsRepo.WithStorage(gcsClient, *ragBucket)
		if !*skipVertexSync && vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
			fragmentsRepo = fragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				return vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
					GCSPath:            strings.TrimSpace(gcsURI),
					ReconciliationMode: "INCREMENTAL",
					DataSchema:         "document",
				})
			})
		} else if *skipVertexSync {
			log.Printf("INFO: Vertex sync is disabled (--skip-vertex-sync). Writes will update Firestore+GCS only.")
		}
	}

	// CanonicalPathResolver with GeoNames (nil policy and deduplicator - not needed for this tool)
	resolver := scout.NewCanonicalPathResolver(fragmentsRepo, nil, nil).WithGeoNamesClient(cachedGeoNamesClient)

	log.Printf("=== Horizon: Normalize /loc via GeoNames ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d maxScan=%d pageSize=%d skipConfidenceBelow=%.2f applyGeoNamesId=%v skipVertexSync=%v",
		*projectID, *tenantID, *apply, *limit, *maxScan, *pageSize, *skipConfidenceBelow, *applyGeoNamesID, *skipVertexSync)
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

	rows, st, lastSeen, err := scan(ctx, db, fragmentsRepo, edgesRepo, framesRepo, resolver, cachedGeoNamesClient, geoCount)
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
	log.Printf("scanned=%d processed=%d normalized=%d unchanged=%d ambiguous=%d collisions=%d deduped=%d dupDeleted=%d edgesReassigned=%d edgesDeleted=%d framesMigrated=%d errors=%d applied=%d skippedLowConf=%d geonamesBaseCalls=%d",
		stFinal.Scanned, stFinal.Processed, stFinal.Normalized, stFinal.Unchanged, stFinal.Ambiguous, stFinal.Collisions, stFinal.Deduped, stFinal.DuplicatesDeleted, stFinal.EdgesReassigned, stFinal.EdgesDeleted, stFinal.FramesMigrated, stFinal.Errors, stFinal.Applied, stFinal.SkippedLowConf, stFinal.GeoNamesBaseCalls)
	log.Printf("geonamesIdsAttached=%d (apply-geonames-id=%v)", stFinal.GeoNamesIDsAttached, *applyGeoNamesID)

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

func scan(
	ctx context.Context,
	db *firestore.Client,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	edgesRepo *hrepo.DependencyEdgesRepository,
	framesRepo *hrepo.FramesRepository,
	resolver *scout.CanonicalPathResolver,
	geonamesClient coreontology.GeoNamesClientInterface,
	geoCount *countingGeoNames,
) ([]Row, *stats, string, error) {
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
					baseCalls := int64(0)
					if geoCount != nil {
						baseCalls = geoCount.n.Load()
					}
					atomic.StoreInt64(&st.GeoNamesBaseCalls, baseCalls)
					log.Printf("heartbeat: scanned=%d processed=%d normalized=%d unchanged=%d ambiguous=%d collisions=%d deduped=%d dupDeleted=%d edgesReassigned=%d edgesDeleted=%d framesMigrated=%d errors=%d applied=%d geonamesBaseCalls=%d lastSeen=%s",
						c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Ambiguous, c.Collisions, c.Deduped, c.DuplicatesDeleted, c.EdgesReassigned, c.EdgesDeleted, c.FramesMigrated, c.Errors, c.Applied, baseCalls, lastSeen)
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
						// If target path already exists, deduplicate into the canonical fragment.
						if result.ExistingFragment != nil && strings.TrimSpace(result.ExistingFragment.ID) != "" && result.ExistingFragment.ID != frag.ID {
							atomic.AddInt64(&st.Collisions, 1)
							row.Reason = fmt.Sprintf("collision: dedup into existing (id=%s)", strings.TrimSpace(result.ExistingFragment.ID))

							reassigned, deletedEdges, migratedFrames, dupDeleted, derr := dedupCollision(
								ctx, db, fragmentsRepo, edgesRepo, framesRepo, &frag, result.ExistingFragment,
							)
							if derr != nil {
								atomic.AddInt64(&st.Errors, 1)
								row.Error = derr.Error()
								row.Applied = false
							} else {
								atomic.AddInt64(&st.Deduped, 1)
								atomic.AddInt64(&st.EdgesReassigned, int64(reassigned))
								atomic.AddInt64(&st.EdgesDeleted, int64(deletedEdges))
								atomic.AddInt64(&st.FramesMigrated, int64(migratedFrames))
								if dupDeleted {
									atomic.AddInt64(&st.DuplicatesDeleted, 1)
								}
								atomic.AddInt64(&st.Applied, 1)
								row.Applied = true
							}
						} else {
							// Actually update the fragment
							oldPath := frag.CanonicalPath
							frag.CanonicalPath = result.CanonicalPath
							if *applyGeoNamesID {
								if attached, gid := maybeAttachGeoNamesID(ctx, geonamesClient, &frag); attached && gid != "" {
									if frag.Identifiers == nil {
										frag.Identifiers = map[string]string{}
									}
									frag.Identifiers["geonamesId"] = gid
									row.Steps = append(row.Steps, fmt.Sprintf("geonames_id_attach: %s", gid))
								}
							}
							frag.IndexPaths = hmodels.BuildIndexPaths(&frag, nil)
							if err := fragmentsRepo.Update(ctx, &frag); err != nil {
								atomic.AddInt64(&st.Errors, 1)
								row.Error = err.Error()
								row.Applied = false
							} else {
								moved, ferr := migrateFrames(ctx, db, framesRepo, strings.TrimSpace(*tenantID), oldPath, frag.CanonicalPath, 1000)
								if ferr != nil {
									atomic.AddInt64(&st.Errors, 1)
									row.Error = fmt.Sprintf("updated fragment, but failed to migrate frames: %v", ferr)
								} else if moved > 0 {
									atomic.AddInt64(&st.FramesMigrated, int64(moved))
								}
								atomic.AddInt64(&st.Applied, 1)
								row.Applied = true

								// Count GeoNames ID attachment if present.
								if *applyGeoNamesID {
									if frag.Identifiers != nil && strings.TrimSpace(frag.Identifiers["geonamesId"]) != "" {
										atomic.AddInt64(&st.GeoNamesIDsAttached, 1)
									}
								}
							}
						}
					}
				} else {
					atomic.AddInt64(&st.Unchanged, 1)
					row.Reason = "unchanged"

					// Optional: attach GeoNames ID even if canonicalPath doesn't change.
					if *apply && *applyGeoNamesID {
						if attached, gid := maybeAttachGeoNamesID(ctx, geonamesClient, &frag); attached && gid != "" {
							if frag.Identifiers == nil {
								frag.Identifiers = map[string]string{}
							}
							frag.Identifiers["geonamesId"] = gid
							// Persist only the GeoNames ID (no path changes).
							if err := fragmentsRepo.Update(ctx, &frag); err != nil {
								atomic.AddInt64(&st.Errors, 1)
								row.Error = fmt.Sprintf("failed to attach geonamesId: %v", err)
							} else {
								atomic.AddInt64(&st.Applied, 1)
								atomic.AddInt64(&st.GeoNamesIDsAttached, 1)
								row.Applied = true
								row.Steps = append(row.Steps, fmt.Sprintf("geonames_id_attach: %s", gid))
								row.Reason = "geonames_id_attached"
							}
						}
					}
				}
			} else {
				atomic.AddInt64(&st.Unchanged, 1)
				row.NewPath = frag.CanonicalPath
				row.Confidence = 1.0
				row.Reason = "no_result"
			}

			if geoCount != nil {
				atomic.StoreInt64(&st.GeoNamesBaseCalls, geoCount.n.Load())
			}

			rowsMu.Lock()
			rows = append(rows, row)
			rowsMu.Unlock()

			// Progress logging
			processed := int(atomic.LoadInt64(&st.Processed))
			if *progressEvery > 0 && processed%*progressEvery == 0 {
				c := st.clone()
				log.Printf("progress: scanned=%d processed=%d normalized=%d unchanged=%d collisions=%d deduped=%d dupDeleted=%d edgesReassigned=%d edgesDeleted=%d framesMigrated=%d errors=%d applied=%d lastDoc=%s",
					c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Collisions, c.Deduped, c.DuplicatesDeleted, c.EdgesReassigned, c.EdgesDeleted, c.FramesMigrated, c.Errors, c.Applied, lastSeen)
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
			log.Printf("progress(scan): scanned=%d processed=%d normalized=%d unchanged=%d collisions=%d deduped=%d errors=%d applied=%d lastDoc=%s",
				c.Scanned, c.Processed, c.Normalized, c.Unchanged, c.Collisions, c.Deduped, c.Errors, c.Applied, lastSeen)
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

// dedupCollision merges a duplicate fragment into the existing canonical fragment:
// - fill-only merge into canonical (no overwrites)
// - reassign dependency edges from dup -> canonical (dedup-safe)
// - migrate frames by targetCanonicalPath (oldPath -> newPath)
// - delete duplicate fragment (Firestore + GCS + Vertex sync if configured on repo)
func dedupCollision(
	ctx context.Context,
	db *firestore.Client,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	edgesRepo *hrepo.DependencyEdgesRepository,
	framesRepo *hrepo.FramesRepository,
	dup *hmodels.HorizonFragment,
	canonical *hmodels.HorizonFragment,
) (edgesReassigned, edgesDeleted, framesMigrated int, dupDeleted bool, err error) {
	if dup == nil || canonical == nil {
		return 0, 0, 0, false, fmt.Errorf("nil dup/canonical")
	}
	if strings.TrimSpace(dup.ID) == "" || strings.TrimSpace(canonical.ID) == "" {
		return 0, 0, 0, false, fmt.Errorf("missing dup/canonical id")
	}
	if strings.TrimSpace(dup.ID) == strings.TrimSpace(canonical.ID) {
		return 0, 0, 0, false, nil
	}

	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	// Best-effort fill-only merge from dup into canonical.
	if mergeFragmentData(canonical, dup) {
		if err := fragmentsRepo.Update(ctx, canonical); err != nil {
			return 0, 0, 0, false, fmt.Errorf("update canonical failed: %w", err)
		}
	}

	// Reassign edges (ID-based).
	reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, dup.ID, canonical.ID)
	if err != nil {
		return reassigned, deleted, 0, false, err
	}

	// Migrate frames (path-based).
	moved, err := migrateFrames(ctx, db, framesRepo, tenant, dup.CanonicalPath, canonical.CanonicalPath, 1000)
	if err != nil {
		return reassigned, deleted, moved, false, err
	}

	// Delete duplicate fragment (handles GCS + Vertex if repo configured).
	if err := fragmentsRepo.Delete(ctx, dup.ID); err != nil {
		return reassigned, deleted, moved, false, fmt.Errorf("delete duplicate failed: %w", err)
	}
	return reassigned, deleted, moved, true, nil
}

// mergeFragmentData merges data from source into target (fill-only).
func mergeFragmentData(target, source *hmodels.HorizonFragment) bool {
	if target == nil || source == nil {
		return false
	}
	changed := false

	// Labels
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Identifiers
	if target.Identifiers == nil {
		target.Identifiers = map[string]string{}
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// KBRefs (append missing)
	existing := map[string]struct{}{}
	for _, r := range target.KBRefs {
		key := strings.TrimSpace(r.KB) + ":" + strings.TrimSpace(r.ExternalID)
		existing[key] = struct{}{}
	}
	for _, r := range source.KBRefs {
		key := strings.TrimSpace(r.KB) + ":" + strings.TrimSpace(r.ExternalID)
		if _, ok := existing[key]; ok {
			continue
		}
		target.KBRefs = append(target.KBRefs, r)
		existing[key] = struct{}{}
		changed = true
	}

	// Description
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}

	// Tags (union)
	tagSet := map[string]struct{}{}
	for _, t := range target.Tags {
		tagSet[t] = struct{}{}
	}
	for _, t := range source.Tags {
		if _, ok := tagSet[t]; ok {
			continue
		}
		target.Tags = append(target.Tags, t)
		tagSet[t] = struct{}{}
		changed = true
	}

	// Merge OrgSize: fill if target is empty/unknown ("na")
	if strings.TrimSpace(source.OrgSize) != "" {
		cur := strings.TrimSpace(strings.ToLower(target.OrgSize))
		in := strings.TrimSpace(source.OrgSize)
		if cur == "" || cur == "na" {
			target.OrgSize = in
			changed = true
		}
	}

	// Merge MarketSegments: union + dedup
	if len(source.MarketSegments) > 0 {
		if target.MarketSegments == nil {
			target.MarketSegments = make([]string, 0, len(source.MarketSegments))
		}
		seen := make(map[string]bool)
		for _, s := range target.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = true
			}
		}
		for _, s := range source.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			target.MarketSegments = append(target.MarketSegments, s)
			changed = true
		}
	}

	// Audit trail
	if strings.TrimSpace(source.CanonicalPath) != "" {
		if target.Identifiers["merged_paths"] == "" {
			target.Identifiers["merged_paths"] = strings.TrimSpace(source.CanonicalPath)
			changed = true
		} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
			target.Identifiers["merged_paths"] += "," + strings.TrimSpace(source.CanonicalPath)
			changed = true
		}
	}

	return changed
}

// reassignEdges moves dependency edges from oldFragmentID to newFragmentID, deleting duplicates/self-loops.
func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *hrepo.DependencyEdgesRepository, oldID, newID string) (reassigned, deleted int, err error) {
	if db == nil || edgesRepo == nil {
		return 0, 0, fmt.Errorf("edges repo not configured")
	}
	tenant := "global"
	edgesCol := db.Collection("horizon_dependency_edges")

	// Source edges
	docs1, err := edgesCol.Where("tenantId", "==", tenant).Where("sourceFragmentId", "==", oldID).Documents(ctx).GetAll()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range docs1 {
		var edge hmodels.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		ns, nt, _, _ := scout.NormalizeSymmetricEdge(newID, edge.TargetFragmentID, edge.Type)
		if scout.IsSelfLoop(ns, nt) {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenant, ns, nt, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	// Target edges
	docs2, err := edgesCol.Where("tenantId", "==", tenant).Where("targetFragmentId", "==", oldID).Documents(ctx).GetAll()
	if err != nil {
		return reassigned, deleted, err
	}
	for _, d := range docs2 {
		var edge hmodels.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		ns, nt, _, _ := scout.NormalizeSymmetricEdge(edge.SourceFragmentID, newID, edge.Type)
		if scout.IsSelfLoop(ns, nt) {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenant, ns, nt, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}
	return reassigned, deleted, nil
}

func migrateFrames(ctx context.Context, db *firestore.Client, framesRepo *hrepo.FramesRepository, tenantID, fromCanonicalPath, toCanonicalPath string, limit int) (int, error) {
	if db == nil || framesRepo == nil {
		return 0, nil
	}
	fromCanonicalPath = strings.TrimSpace(fromCanonicalPath)
	toCanonicalPath = strings.TrimSpace(toCanonicalPath)
	if fromCanonicalPath == "" || toCanonicalPath == "" || fromCanonicalPath == toCanonicalPath {
		return 0, nil
	}
	frames, err := framesRepo.ListByTargetCanonicalPath(ctx, tenantID, fromCanonicalPath, limit)
	if err != nil {
		return 0, err
	}
	if len(frames) == 0 {
		return 0, nil
	}
	col := db.Collection("horizon_frames")
	updated := 0
	for _, f := range frames {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		_, err := col.Doc(f.ID).Update(ctx, []firestore.Update{
			{Path: "targetCanonicalPath", Value: toCanonicalPath},
		})
		if err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func maybeAttachGeoNamesID(ctx context.Context, geonamesClient coreontology.GeoNamesClientInterface, f *hmodels.HorizonFragment) (bool, string) {
	if geonamesClient == nil || f == nil {
		return false, ""
	}
	// Only attach to GEO_REGION fragments with /loc paths where a city-like token is present.
	cp := strings.TrimSpace(f.CanonicalPath)
	if !strings.HasPrefix(cp, "/loc/") {
		return false, ""
	}
	ccParsed, level1, level2, ok := taxonomy.ParseLocCanonicalPath(cp)
	if !ok {
		return false, ""
	}
	cc := strings.ToUpper(strings.TrimSpace(ccParsed))
	if len(cc) != 2 {
		return false, ""
	}
	adm := strings.TrimSpace(level1)
	city := strings.TrimSpace(level2)
	if city == "" {
		city = strings.TrimSpace(level1)
		adm = ""
	}
	if city == "" {
		return false, ""
	}

	// Skip if already set.
	for _, r := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(r.KB), "GeoNames") && strings.TrimSpace(r.ExternalID) != "" {
			return false, strings.TrimSpace(r.ExternalID)
		}
	}
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["geonamesId"]); v != "" {
			return false, v
		}
	}

	cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(city)))
	if cityName == "" {
		return false, ""
	}

	opts := coreontology.GeoNamesSearchOptions{
		Country:      cc,
		FeatureClass: "P",
		NameEquals:   true,
	}
	// Only pass adminCode1 when it looks like a GeoNames code (2 letters or 1-3 digits).
	if looksLikeGeoNamesAdminCode(adm) {
		opts.AdminCode1 = strings.ToUpper(strings.TrimSpace(adm))
	}

	resp, err := geonamesClient.Search(ctx, cityName, 10, opts)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return false, ""
	}

	// Filter by country
	results := make([]coreontology.GeoNamesPlace, 0, len(resp.Results))
	for _, r := range resp.Results {
		if strings.EqualFold(strings.TrimSpace(r.CountryCode), cc) {
			results = append(results, r)
		}
	}
	if len(results) == 0 {
		return false, ""
	}

	// If admin isn't a code, attempt to match by adminName1/adminCode1 token.
	adminToken := normalizeGeoToken(adm)
	if adminToken != "" && !strings.EqualFold(strings.TrimSpace(adm), "na") && strings.TrimSpace(opts.AdminCode1) == "" {
		filtered := make([]coreontology.GeoNamesPlace, 0, len(results))
		for _, r := range results {
			if adminToken == normalizeGeoToken(r.AdminCode1) || adminToken == normalizeGeoToken(r.AdminName1) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			results = filtered
		}
	}

	best := (*coreontology.GeoNamesPlace)(nil)
	if len(results) == 1 {
		best = &results[0]
	} else {
		// Conservative: try dominant-by-population heuristic.
		if b, ok := pickDominantByPopulation(results, cityName); ok {
			best = b
		}
	}
	if best == nil || best.GeoNameID <= 0 {
		return false, ""
	}
	return true, strconv.Itoa(best.GeoNameID)
}

func looksLikeGeoNamesAdminCode(admin string) bool {
	admin = strings.ToUpper(strings.TrimSpace(admin))
	if admin == "" || strings.EqualFold(admin, "NA") {
		return false
	}
	if len(admin) == 2 && admin[0] >= 'A' && admin[0] <= 'Z' && admin[1] >= 'A' && admin[1] <= 'Z' {
		return true
	}
	if len(admin) >= 1 && len(admin) <= 3 {
		for i := 0; i < len(admin); i++ {
			if admin[i] < '0' || admin[i] > '9' {
				return false
			}
		}
		return true
	}
	return false
}

func normalizeGeoToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.NewReplacer("-", "", "_", "", " ", "").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pickDominantByPopulation(results []coreontology.GeoNamesPlace, cityName string) (*coreontology.GeoNamesPlace, bool) {
	if len(results) == 0 {
		return nil, false
	}
	want := slugify(cityName)
	cands := make([]coreontology.GeoNamesPlace, 0, len(results))
	for _, r := range results {
		if slugify(r.Name) != want {
			continue
		}
		if r.Population <= 0 {
			continue
		}
		cands = append(cands, r)
	}
	if len(cands) == 0 {
		return nil, false
	}
	best := cands[0]
	second := int64(0)
	for _, r := range cands {
		if r.Population > best.Population {
			second = best.Population
			best = r
			continue
		}
		if r.Population > second && r.Population <= best.Population {
			second = r.Population
		}
	}
	if best.Population < 100000 {
		return nil, false
	}
	if second == 0 {
		return &best, true
	}
	if float64(best.Population) >= float64(second)*5.0 {
		return &best, true
	}
	return nil, false
}

func slugify(s string) string {
	return taxonomy.Slugify(s)
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
