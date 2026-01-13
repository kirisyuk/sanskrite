package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: horizon_list_market_segments
//
// Scans Firestore collection `horizon_fragments` with pagination and extracts all market segments present in the DB.
// Outputs:
// - unique segment canonical paths (/segment/{domain}/{vertical}/{geo})
// - human-readable label derived from the path (Domain / Vertical / Geo)
// - frequency counts
//
// Auth:
// - Uses Application Default Credentials (gcloud auth).
// - Project ID is taken from --project or env (GCP_PROJECT_ID / GOOGLE_CLOUD_PROJECT).
//
// Example:
//   go run ./cmd/tools/horizon_list_market_segments --tenant=global --page-size=500 --out=./market_segments_report.txt

type fragLite struct {
	TenantID       string   `firestore:"tenantId"`
	ID             string   `firestore:"id"`
	CanonicalPath  string   `firestore:"canonicalPath"`
	Type           string   `firestore:"type"`
	MarketSegments []string `firestore:"marketSegments"`
	IndexPaths     []string `firestore:"indexPaths"`
}

type segStat struct {
	Path  string
	Label string
	Count int
}

func envProjectID() string {
	if v := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")); v != "" {
		return v
	}
	return ""
}

func humanizeSegmentPath(seg string) string {
	// Canonical: /segment/{domain}/{vertical}/{geo}
	parts := strings.Split(strings.Trim(seg, "/"), "/")
	if len(parts) < 4 || parts[0] != "segment" {
		return ""
	}
	domain := parts[1]
	vertical := parts[2]
	geo := parts[3]
	toTitle := func(s string) string {
		s = strings.ReplaceAll(s, "-", " ")
		s = strings.ReplaceAll(s, "_", " ")
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return ""
		}
		// Title-case each word, but keep common acronyms (rd, ai, ev) upper.
		ws := strings.Fields(s)
		for i := range ws {
			w := ws[i]
			switch w {
			case "rd", "ai", "ev", "ml", "saas", "it":
				ws[i] = strings.ToUpper(w)
			default:
				ws[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		return strings.Join(ws, " ")
	}
	geoDisp := strings.ToUpper(strings.TrimSpace(geo))
	if strings.ToLower(geo) == "global" {
		geoDisp = "Global"
	}
	return fmt.Sprintf("%s / %s / %s", toTitle(domain), toTitle(vertical), geoDisp)
}

func main() {
	var (
		projectID     = flag.String("project", "", "GCP project ID (fallback: env GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT)")
		tenantID      = flag.String("tenant", "global", "tenantId filter (default: global)")
		pageSize      = flag.Int("page-size", 500, "Firestore page size (default: 500, max: 1000)")
		outPath       = flag.String("out", "", "output file path (default: stdout only)")
		maxDocs       = flag.Int("max-docs", 0, "max documents to scan (0 = no limit)")
		includeIndex  = flag.Bool("include-indexpaths", false, "also extract /segment/* from indexPaths as fallback (off by default)")
		onlyOrgAndFac = flag.Bool("only-org-facility", true, "only consider fragments of type ORGANIZATION or FACILITY")
	)
	flag.Parse()

	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = envProjectID()
	}
	if pid == "" {
		log.Fatal("project id not provided; set --project or env GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT")
	}

	tid := strings.TrimSpace(*tenantID)
	if tid == "" {
		tid = "global"
	}

	ps := *pageSize
	if ps <= 0 {
		ps = 500
	}
	if ps > 1000 {
		ps = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	client, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer client.Close()

	var out *bufio.Writer
	var f *os.File
	if strings.TrimSpace(*outPath) != "" {
		fp := strings.TrimSpace(*outPath)
		ff, err := os.Create(fp)
		if err != nil {
			log.Fatalf("failed to create output file %q: %v", fp, err)
		}
		f = ff
		defer f.Close()
		out = bufio.NewWriter(f)
		defer out.Flush()
	} else {
		out = bufio.NewWriter(os.Stdout)
		defer out.Flush()
	}

	col := client.Collection("horizon_fragments")
	q := col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).Limit(ps)

	segCounts := make(map[string]int, 4096)
	segLabel := make(map[string]string, 4096)

	// Stats
	var scanned int
	var withSegments int
	var withMarketSegments int
	var withIndexSegments int

	var last *firestore.DocumentSnapshot

	for {
		qq := q
		if last != nil {
			qq = qq.StartAfter(last)
		}

		// Field mask: minimize transfer.
		fields := []string{"tenantId", "id", "canonicalPath", "type", "marketSegments"}
		if *includeIndex {
			fields = append(fields, "indexPaths")
		}
		qq = qq.Select(fields...)

		docs, err := qq.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("firestore query failed (after scanned=%d): %v", scanned, err)
		}
		if len(docs) == 0 {
			break
		}
		last = docs[len(docs)-1]

		for _, d := range docs {
			var fr fragLite
			if err := d.DataTo(&fr); err != nil {
				continue
			}
			scanned++
			if *maxDocs > 0 && scanned > *maxDocs {
				break
			}

			ft := strings.ToUpper(strings.TrimSpace(fr.Type))
			if *onlyOrgAndFac {
				if ft != "ORGANIZATION" && ft != "FACILITY" {
					continue
				}
			}

			foundAny := false
			foundMarket := false
			foundIndex := false

			for _, s := range fr.MarketSegments {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if norm, ok := taxonomy.NormalizeSegmentPath(s); ok {
					s = norm
				}
				if !strings.HasPrefix(s, "/segment/") {
					continue
				}
				foundAny = true
				foundMarket = true
				segCounts[s]++
				if _, ok := segLabel[s]; !ok {
					segLabel[s] = humanizeSegmentPath(s)
				}
			}

			if *includeIndex {
				for _, ip := range fr.IndexPaths {
					ip = strings.TrimSpace(ip)
					if !strings.HasPrefix(ip, "/segment/") {
						continue
					}
					// indexPaths can include ancestors (/segment/domain, /segment/domain/vertical) – normalize to 4 segments
					if norm, ok := taxonomy.NormalizeSegmentPath(ip); ok {
						ip = norm
					}
					if !strings.HasPrefix(ip, "/segment/") {
						continue
					}
					foundAny = true
					foundIndex = true
					segCounts[ip]++
					if _, ok := segLabel[ip]; !ok {
						segLabel[ip] = humanizeSegmentPath(ip)
					}
				}
			}

			if foundAny {
				withSegments++
			}
			if foundMarket {
				withMarketSegments++
			}
			if foundIndex {
				withIndexSegments++
			}
		}

		if *maxDocs > 0 && scanned > *maxDocs {
			break
		}
	}

	// Sort segments by frequency desc, then path asc.
	stats := make([]segStat, 0, len(segCounts))
	for p, c := range segCounts {
		stats = append(stats, segStat{Path: p, Label: segLabel[p], Count: c})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Path < stats[j].Path
	})

	fmt.Fprintf(out, "=== Horizon marketSegments scan ===\n")
	fmt.Fprintf(out, "project=%s tenant=%s pageSize=%d includeIndexPaths=%v onlyOrgFacility=%v maxDocs=%d\n",
		pid, tid, ps, *includeIndex, *onlyOrgAndFac, *maxDocs)
	fmt.Fprintf(out, "scanned=%d\n", scanned)
	fmt.Fprintf(out, "fragments_with_any_segments=%d\n", withSegments)
	fmt.Fprintf(out, "fragments_with_marketSegments=%d\n", withMarketSegments)
	fmt.Fprintf(out, "fragments_with_indexPath_segments=%d\n", withIndexSegments)
	fmt.Fprintf(out, "unique_segments=%d\n", len(stats))
	fmt.Fprintf(out, "\n")

	for _, s := range stats {
		lbl := strings.TrimSpace(s.Label)
		if lbl == "" {
			lbl = "(unparsed)"
		}
		fmt.Fprintf(out, "%7d  %-44s  %s\n", s.Count, s.Path, lbl)
	}
}
