package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type fragmentWithDegree struct {
	Fragment  *models.HorizonFragment `json:"fragment"`
	EdgeCount int                     `json:"edgeCount"`
	Truncated bool                    `json:"truncated"`
}

type typeSummary struct {
	FragmentType      string                 `json:"fragmentType"`
	TotalFragments    int                    `json:"totalFragments"`
	FilteredFragments int                    `json:"filteredFragments"`
	EdgeLimit         int                    `json:"edgeLimit"`
	MinEdgeCount      int                    `json:"minEdgeCount"`
	TruncatedCount    int                    `json:"truncatedCount"`
	TopPercent        float64                `json:"topPercent"`
	TopCount          int                    `json:"topCount"`
	Stats             map[string]float64     `json:"stats"`
	TopSamples        []fragmentWithDegree   `json:"topSamples,omitempty"`
	DurationMs        int                    `json:"durationMs"`
	Errors            []string               `json:"errors,omitempty"`
}

var (
	projectID   = flag.String("project", "", "GCP project ID (required)")
	tenantID    = flag.String("tenant", "global", "Tenant ID (default: global)")
	fragType    = flag.String("type", "all", "Fragment type: ORGANIZATION, MARKET, PRODUCT, GOV or all")
	topPercent  = flag.Float64("top-percent", 0.10, "Top percent for selection (e.g., 0.10)")
	limit       = flag.Int("limit", 0, "Max fragments per type (0 = no limit)")
	pageSize    = flag.Int("page-size", 500, "Firestore page size for fragment scans")
	progressEvery = flag.Int("progress-every", 5, "Log progress every N pages")
	edgeProgressEvery = flag.Int("edge-progress-every", 1000, "Log progress every N fragments for edge counting")
	edgeLimit   = flag.Int("edge-limit", 1000, "Max edges per direction to count (per fragment)")
	minEdge     = flag.Int("min-edge-count", 0, "Filter out fragments with fewer edges")
	maxWorkers  = flag.Int("workers", 8, "Max concurrent edge queries")
	showTop     = flag.Int("show-top", 20, "Include top N fragments in output")
	jsonOutput  = flag.Bool("json", false, "Output JSON to stdout")
	mode        = flag.String("mode", "scan-edges", "Mode: scan-edges (fast) or per-fragment (slow)")
	edgePageSize = flag.Int("edge-page-size", 2000, "Firestore page size for edge scans (scan-edges mode)")
)

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer db.Close()

	edgesRepo := repo.NewDependencyEdgesRepository(db)

	types := resolveTypes(*fragType)
	if len(types) == 0 {
		log.Fatalf("unknown type: %s", *fragType)
	}
	govOnly := isGovType(*fragType)

	results := make([]*typeSummary, 0, len(types))
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "scan-edges":
		results = analyzeTypesByEdgeScan(ctx, db, strings.TrimSpace(*tenantID), types, govOnly, *topPercent, *limit, *pageSize, *progressEvery, *edgePageSize, *edgeProgressEvery, *minEdge, *showTop)
	case "per-fragment":
		for _, t := range types {
			summary := analyzeType(ctx, db, edgesRepo, strings.TrimSpace(*tenantID), t, govOnly, *topPercent, *limit, *pageSize, *progressEvery, *edgeProgressEvery, *edgeLimit, *minEdge, *maxWorkers, *showTop)
			results = append(results, summary)
		}
	default:
		log.Fatalf("unknown mode: %s", *mode)
	}

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
		return
	}

	for _, s := range results {
		fmt.Printf("\n=== %s ===\n", s.FragmentType)
		fmt.Printf("Fragments: total=%d filtered=%d\n", s.TotalFragments, s.FilteredFragments)
		fmt.Printf("Edges: limit=%d, minEdge=%d, truncated=%d\n", s.EdgeLimit, s.MinEdgeCount, s.TruncatedCount)
		fmt.Printf("Top: percent=%.2f count=%d\n", s.TopPercent, s.TopCount)
		if len(s.Stats) > 0 {
			fmt.Printf("Stats: min=%.0f p50=%.0f p90=%.0f p95=%.0f p99=%.0f max=%.0f avg=%.2f\n",
				s.Stats["min"], s.Stats["p50"], s.Stats["p90"], s.Stats["p95"], s.Stats["p99"], s.Stats["max"], s.Stats["avg"])
		}
		fmt.Printf("Duration: %dms\n", s.DurationMs)
		if len(s.Errors) > 0 {
			fmt.Printf("Errors: %d (see JSON output for details)\n", len(s.Errors))
		}
		if len(s.TopSamples) > 0 {
			fmt.Printf("Top samples:\n")
			for _, item := range s.TopSamples {
				cp := ""
				if item.Fragment != nil {
					cp = item.Fragment.CanonicalPath
				}
				fmt.Printf("  - %s (%s) edges=%d%s\n", item.Fragment.ID, cp, item.EdgeCount, truncatedSuffix(item.Truncated))
			}
		}
	}
}

func resolveTypes(input string) []models.FragmentType {
	v := strings.ToUpper(strings.TrimSpace(input))
	switch v {
	case "ALL", "":
		return []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeMarket,
			models.FragmentTypeProduct,
		}
	case "ORG", "ORGANIZATION":
		return []models.FragmentType{models.FragmentTypeOrganization}
	case "MARKET":
		return []models.FragmentType{models.FragmentTypeMarket}
	case "PRODUCT":
		return []models.FragmentType{models.FragmentTypeProduct}
	case "GOV", "GOVERNMENT":
		return []models.FragmentType{models.FragmentTypeOrganization}
	default:
		return nil
	}
}

func isGovType(input string) bool {
	v := strings.ToUpper(strings.TrimSpace(input))
	return v == "GOV" || v == "GOVERNMENT"
}

func analyzeType(
	ctx context.Context,
	db *firestore.Client,
	edgesRepo *repo.DependencyEdgesRepository,
	tenantID string,
	fragType models.FragmentType,
	govOnly bool,
	topPercent float64,
	limit int,
	pageSize int,
	progressEvery int,
	edgeProgressEvery int,
	edgeLimit int,
	minEdge int,
	maxWorkers int,
	showTop int,
) *typeSummary {
	start := time.Now()
	summary := &typeSummary{
		FragmentType: string(fragType),
		EdgeLimit:    edgeLimit,
		MinEdgeCount: minEdge,
		TopPercent:   topPercent,
		Errors:       make([]string, 0),
		Stats:        make(map[string]float64),
	}

	fragments, err := listFragmentsPaged(ctx, db, tenantID, fragType, pageSize, progressEvery, limit)
	if err != nil {
		summary.Errors = append(summary.Errors, fmt.Sprintf("list fragments: %v", err))
		summary.DurationMs = int(time.Since(start).Milliseconds())
		return summary
	}

	// GOV filter if requested by type input (canonical path prefix)
	if govOnly && fragType == models.FragmentTypeOrganization {
		filtered := make([]*models.HorizonFragment, 0, len(fragments))
		for _, f := range fragments {
			if f != nil && strings.HasPrefix(f.CanonicalPath, "/gov/") {
				filtered = append(filtered, f)
			}
		}
		fragments = filtered
		summary.FragmentType = "GOV"
	}

	summary.TotalFragments = len(fragments)

	withDegree := make([]fragmentWithDegree, 0, len(fragments))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var processed int64

	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	jobs := make(chan *models.HorizonFragment)

	if edgeProgressEvery <= 0 {
		edgeProgressEvery = 1000
	}

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if f == nil || strings.TrimSpace(f.ID) == "" {
					continue
				}
				outEdges, err1 := edgesRepo.ListBySource(ctx, tenantID, f.ID, nil, edgeLimit)
				inEdges, err2 := edgesRepo.ListByTarget(ctx, tenantID, f.ID, nil, edgeLimit)
				if err1 != nil {
					mu.Lock()
					summary.Errors = append(summary.Errors, fmt.Sprintf("source edges %s: %v", f.ID, err1))
					mu.Unlock()
				}
				if err2 != nil {
					mu.Lock()
					summary.Errors = append(summary.Errors, fmt.Sprintf("target edges %s: %v", f.ID, err2))
					mu.Unlock()
				}

				edgeCount := len(outEdges) + len(inEdges)
				truncated := len(outEdges) >= edgeLimit || len(inEdges) >= edgeLimit
				if edgeCount < minEdge {
					if p := atomic.AddInt64(&processed, 1); p%int64(edgeProgressEvery) == 0 {
						log.Printf("Processed %d/%d fragments for type=%s", p, len(fragments), summary.FragmentType)
					}
					continue
				}
				mu.Lock()
				withDegree = append(withDegree, fragmentWithDegree{
					Fragment:  f,
					EdgeCount: edgeCount,
					Truncated: truncated,
				})
				if truncated {
					summary.TruncatedCount++
				}
				mu.Unlock()
				if p := atomic.AddInt64(&processed, 1); p%int64(edgeProgressEvery) == 0 {
					log.Printf("Processed %d/%d fragments for type=%s", p, len(fragments), summary.FragmentType)
				}
			}
		}()
	}

	for _, f := range fragments {
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	summary.FilteredFragments = len(withDegree)

	if len(withDegree) == 0 {
		summary.DurationMs = int(time.Since(start).Milliseconds())
		return summary
	}

	sort.Slice(withDegree, func(i, j int) bool {
		return withDegree[i].EdgeCount > withDegree[j].EdgeCount
	})

	topCount := int(float64(len(withDegree)) * topPercent)
	if topCount < 1 {
		topCount = 1
	}
	if topCount > len(withDegree) {
		topCount = len(withDegree)
	}
	summary.TopCount = topCount

	if showTop > 0 {
		if showTop > topCount {
			showTop = topCount
		}
		summary.TopSamples = append(summary.TopSamples, withDegree[:showTop]...)
	}

	// Stats
	counts := make([]int, 0, len(withDegree))
	for _, item := range withDegree {
		counts = append(counts, item.EdgeCount)
	}
	sort.Ints(counts)
	summary.Stats["min"] = float64(counts[0])
	summary.Stats["max"] = float64(counts[len(counts)-1])
	summary.Stats["p50"] = percentile(counts, 0.50)
	summary.Stats["p90"] = percentile(counts, 0.90)
	summary.Stats["p95"] = percentile(counts, 0.95)
	summary.Stats["p99"] = percentile(counts, 0.99)
	sum := 0
	for _, v := range counts {
		sum += v
	}
	summary.Stats["avg"] = float64(sum) / float64(len(counts))

	summary.DurationMs = int(time.Since(start).Milliseconds())
	return summary
}

func listFragmentsPaged(ctx context.Context, db *firestore.Client, tenantID string, fragType models.FragmentType, pageSize int, progressEvery int, limit int) ([]*models.HorizonFragment, error) {
	if pageSize <= 0 {
		pageSize = 500
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	if progressEvery <= 0 {
		progressEvery = 5
	}

	col := db.Collection("horizon_fragments")
	baseQuery := col.
		Where("tenantId", "==", tenantID).
		Where("type", "==", string(fragType)).
		OrderBy(firestore.DocumentID, firestore.Asc)

	var out []*models.HorizonFragment
	var lastDoc *firestore.DocumentSnapshot
	page := 0

	for {
		q := baseQuery.Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}

		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return out, err
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var fragment models.HorizonFragment
			if err := doc.DataTo(&fragment); err != nil {
				continue
			}
			out = append(out, &fragment)
			if limit > 0 && len(out) >= limit {
				return out[:limit], nil
			}
		}

		lastDoc = docs[len(docs)-1]
		page++
		if page%progressEvery == 0 {
			log.Printf("Fetched %d fragments for type=%s (pages=%d)", len(out), fragType, page)
		}
	}

	return out, nil
}

func analyzeTypesByEdgeScan(
	ctx context.Context,
	db *firestore.Client,
	tenantID string,
	types []models.FragmentType,
	govOnly bool,
	topPercent float64,
	limit int,
	pageSize int,
	progressEvery int,
	edgePageSize int,
	edgeProgressEvery int,
	minEdge int,
	showTop int,
) []*typeSummary {
	start := time.Now()
	summaries := make(map[string]*typeSummary)
	for _, t := range types {
		summaries[string(t)] = &typeSummary{
			FragmentType: string(t),
			EdgeLimit:    0,
			MinEdgeCount: minEdge,
			TopPercent:   topPercent,
			Errors:       make([]string, 0),
			Stats:        make(map[string]float64),
		}
	}

	// Load fragments for all types
	fragmentIndex := make(map[string]fragmentWithDegree)
	fragmentTypeByID := make(map[string]string)

	for _, t := range types {
		fragments, err := listFragmentsPaged(ctx, db, tenantID, t, pageSize, progressEvery, limit)
		if err != nil {
			summaries[string(t)].Errors = append(summaries[string(t)].Errors, fmt.Sprintf("list fragments: %v", err))
			continue
		}

		// GOV filter if needed
		if govOnly && t == models.FragmentTypeOrganization {
			filtered := make([]*models.HorizonFragment, 0, len(fragments))
			for _, f := range fragments {
				if f != nil && strings.HasPrefix(f.CanonicalPath, "/gov/") {
					filtered = append(filtered, f)
				}
			}
			fragments = filtered
			summaries[string(t)].FragmentType = "GOV"
		}

		summaries[string(t)].TotalFragments = len(fragments)
		for _, f := range fragments {
			if f == nil || strings.TrimSpace(f.ID) == "" {
				continue
			}
			fragmentIndex[f.ID] = fragmentWithDegree{Fragment: f}
			fragmentTypeByID[f.ID] = summaries[string(t)].FragmentType
		}
	}

	// Scan edges once and update counts
	if edgePageSize <= 0 {
		edgePageSize = 2000
	}
	if edgePageSize > 5000 {
		edgePageSize = 5000
	}
	if edgeProgressEvery <= 0 {
		edgeProgressEvery = 10000
	}

	col := db.Collection("horizon_dependency_edges")
	baseQuery := col.Where("tenantId", "==", tenantID).OrderBy(firestore.DocumentID, firestore.Asc)
	var lastDoc *firestore.DocumentSnapshot
	var processedEdges int64
	page := 0

	for {
		q := baseQuery.Limit(edgePageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}

		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			for _, t := range types {
				summaries[string(t)].Errors = append(summaries[string(t)].Errors, fmt.Sprintf("list edges: %v", err))
			}
			break
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var edge struct {
				SourceFragmentID string `firestore:"sourceFragmentId"`
				TargetFragmentID string `firestore:"targetFragmentId"`
			}
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if v, ok := fragmentIndex[edge.SourceFragmentID]; ok {
				v.EdgeCount++
				fragmentIndex[edge.SourceFragmentID] = v
			}
			if v, ok := fragmentIndex[edge.TargetFragmentID]; ok {
				v.EdgeCount++
				fragmentIndex[edge.TargetFragmentID] = v
			}
			if p := atomic.AddInt64(&processedEdges, 1); p%int64(edgeProgressEvery) == 0 {
				log.Printf("Scanned %d edges...", p)
			}
		}

		lastDoc = docs[len(docs)-1]
		page++
	}

	// Build per-type slices and stats
	perType := make(map[string][]fragmentWithDegree)
	for id, item := range fragmentIndex {
		t := fragmentTypeByID[id]
		if item.EdgeCount < minEdge {
			continue
		}
		perType[t] = append(perType[t], item)
	}

	results := make([]*typeSummary, 0, len(types))
	for _, t := range types {
		key := string(t)
		s := summaries[key]
		items := perType[s.FragmentType]
		s.FilteredFragments = len(items)
		if len(items) == 0 {
			s.DurationMs = int(time.Since(start).Milliseconds())
			results = append(results, s)
			continue
		}

		sort.Slice(items, func(i, j int) bool {
			return items[i].EdgeCount > items[j].EdgeCount
		})

		topCount := int(float64(len(items)) * topPercent)
		if topCount < 1 {
			topCount = 1
		}
		if topCount > len(items) {
			topCount = len(items)
		}
		s.TopCount = topCount
		if showTop > 0 {
			if showTop > topCount {
				showTop = topCount
			}
			s.TopSamples = append(s.TopSamples, items[:showTop]...)
		}

		counts := make([]int, 0, len(items))
		for _, item := range items {
			counts = append(counts, item.EdgeCount)
		}
		sort.Ints(counts)
		s.Stats["min"] = float64(counts[0])
		s.Stats["max"] = float64(counts[len(counts)-1])
		s.Stats["p50"] = percentile(counts, 0.50)
		s.Stats["p90"] = percentile(counts, 0.90)
		s.Stats["p95"] = percentile(counts, 0.95)
		s.Stats["p99"] = percentile(counts, 0.99)
		sum := 0
		for _, v := range counts {
			sum += v
		}
		s.Stats["avg"] = float64(sum) / float64(len(counts))
		s.DurationMs = int(time.Since(start).Milliseconds())
		results = append(results, s)
	}

	return results
}

func percentile(values []int, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return float64(values[0])
	}
	if p >= 1 {
		return float64(values[len(values)-1])
	}
	idx := int(float64(len(values)-1) * p)
	return float64(values[idx])
}

func truncatedSuffix(truncated bool) string {
	if truncated {
		return " (truncated)"
	}
	return ""
}
