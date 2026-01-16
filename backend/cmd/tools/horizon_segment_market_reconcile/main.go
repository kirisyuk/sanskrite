package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: horizon_segment_market_reconcile
//
// Goal (segment-first):
// - Observe how well /market fragments align with /segment taxonomy.
// - Report drift:
//   - ORG/FAC have marketSegments but derived /market(v2) nodes are missing
//   - legacy freeform markets (/market/{geo}/{slug}) that are not mappable to /segment yet
//
// Default mode is dry-run reporting only (no writes).

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	limit      = flag.Int("limit", 0, "Limit number of fragments to scan (0 = all)")
	pageSize   = flag.Int("page-size", 300, "Firestore page size")
	startAfter = flag.String("start-after", "", "Resume pagination after this document ID (__name__)")

	dryRun = flag.Bool("dry-run", true, "Dry run: report only (no writes)")

	// Optional: create missing derived market nodes (does not rewrite existing market nodes).
	createMissingMarkets = flag.Bool("create-missing-markets", false, "Create missing derived /market(v2) fragments for segments (requires --dry-run=false)")
)

type stats struct {
	Scanned int

	OrgsWithSegments int
	SegmentsTotal    int

	DerivedMarketsExpected int
	DerivedMarketsMissing  int
	DerivedMarketsCreated  int

	LegacyMarkets int
	V2Markets     int
}

func main() {
	flag.Parse()

	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		log.Fatal("--project is required (or set GOOGLE_CLOUD_PROJECT)")
	}
	tid := strings.TrimSpace(*tenantID)
	if tid == "" {
		tid = "global"
	}
	if tid != "global" {
		log.Printf("WARNING: Horizon is global-only. Overriding --tenant=%q to \"global\".", tid)
		tid = "global"
	}
	if *pageSize <= 0 {
		*pageSize = 300
	}
	if *createMissingMarkets && *dryRun {
		log.Fatal("--create-missing-markets requires --dry-run=false")
	}

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	frRepo := hrepo.NewHorizonFragmentsRepository(db)

	st := &stats{}

	// We scan all fragments (tenantId==global), ordered by __name__ to allow paging.
	col := db.Collection("horizon_fragments")
	q := col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).Limit(*pageSize)
	if strings.TrimSpace(*startAfter) != "" {
		q = q.StartAfter(strings.TrimSpace(*startAfter))
	}

	missingDerivedMarkets := map[string]int{} // marketPath -> count
	seenDerivedMarkets := map[string]struct{}{}

	lastDocID := ""
	start := time.Now()
	for {
		iter := q.Documents(ctx)
		nThisPage := 0
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Fatalf("iteration failed: %v", err)
			}
			nThisPage++
			lastDocID = doc.Ref.ID
			st.Scanned++
			if *limit > 0 && st.Scanned > *limit {
				break
			}

			var frag hmodels.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}

			// Market fragments: classify as legacy vs v2 for reporting.
			if frag.Type == hmodels.FragmentTypeMarket || strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/market/") {
				parts := strings.Split(strings.Trim(strings.TrimSpace(frag.CanonicalPath), "/"), "/")
				if len(parts) >= 4 && parts[0] == "market" {
					st.V2Markets++
				} else if len(parts) == 3 && parts[0] == "market" {
					st.LegacyMarkets++
				}
			}

			// ORG/FAC/GOV: derive expected market nodes from marketSegments.
			if frag.Type == hmodels.FragmentTypeOrganization || frag.Type == hmodels.FragmentTypeFacility {
				if len(frag.MarketSegments) == 0 {
					continue
				}
				st.OrgsWithSegments++
				for _, s := range frag.MarketSegments {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					seg, ok := taxonomy.NormalizeSegmentPath(s)
					if !ok || seg == "" {
						continue
					}
					st.SegmentsTotal++
					mp, ok := taxonomy.MarketPathFromSegmentPath(seg)
					if !ok || mp == "" {
						continue
					}
					if _, ok := seenDerivedMarkets[mp]; !ok {
						seenDerivedMarkets[mp] = struct{}{}
						st.DerivedMarketsExpected++
					}
					// Check existence
					_, ferr := frRepo.GetByCanonicalPath(ctx, mp)
					if ferr == nil {
						continue
					}
					if errors.Is(ferr, hrepo.ErrFragmentNotFound) {
						missingDerivedMarkets[mp]++
						continue
					}
				}
			}
		}
		iter.Stop()

		if *limit > 0 && st.Scanned > *limit {
			break
		}
		if nThisPage == 0 {
			break
		}
		// Advance query cursor
		q = col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).StartAfter(lastDocID).Limit(*pageSize)
	}

	// Summarize missing derived markets.
	for _, c := range missingDerivedMarkets {
		st.DerivedMarketsMissing += c
	}

	log.Printf("=== horizon_segment_market_reconcile (dry-run=%v) ===", *dryRun)
	log.Printf("project=%s tenant=%s scanned=%d duration=%s", pid, tid, st.Scanned, time.Since(start).Truncate(time.Second))
	log.Printf("orgs_with_segments=%d segments_total=%d", st.OrgsWithSegments, st.SegmentsTotal)
	log.Printf("derived_markets_expected(unique)=%d derived_markets_missing(occurrences)=%d", st.DerivedMarketsExpected, st.DerivedMarketsMissing)
	log.Printf("markets: v2=%d legacy=%d", st.V2Markets, st.LegacyMarkets)

	// Print top missing derived markets by count.
	type pair struct {
		Path  string
		Count int
	}
	top := make([]pair, 0, len(missingDerivedMarkets))
	for p, c := range missingDerivedMarkets {
		top = append(top, pair{Path: p, Count: c})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Count == top[j].Count {
			return top[i].Path < top[j].Path
		}
		return top[i].Count > top[j].Count
	})
	if len(top) > 30 {
		top = top[:30]
	}
	if len(top) > 0 {
		log.Printf("--- top missing derived markets (count=org segment occurrences) ---")
		for _, p := range top {
			log.Printf("%6d  %s", p.Count, p.Path)
		}
	}

	if !*dryRun && *createMissingMarkets {
		created := 0
		for _, p := range top {
			// Create only top-N to keep safe.
			label := strings.TrimPrefix(p.Path, "/market/")
			f := &hmodels.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: p.Path,
				Type:          hmodels.FragmentTypeMarket,
				Labels:        map[string]string{"en": label},
				Status:        string(hmodels.FragmentStatusActive),
				State:         map[string]hmodels.MetricValue{},
				Governance:    hmodels.Governance{Owner: "SYSTEM", Status: string(hmodels.FragmentStatusActive)},
				Metadata: hmodels.FragmentMetadata{
					CreatedAt:       time.Now().UTC(),
					LastUpdatedAt:   time.Now().UTC(),
					EpistemicStatus: hmodels.EpistemicStatusLowConfidence,
				},
				Provenance: hmodels.Provenance{Method: "tool.segment_market_reconcile"},
			}
			if _, err := frRepo.Create(ctx, f); err != nil {
				continue
			}
			created++
		}
		st.DerivedMarketsCreated = created
		log.Printf("created_missing_markets=%d (top list only)", created)
	}

	log.Printf("done.")
}
