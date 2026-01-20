package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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

	namespacesFlag = flag.String("namespaces", "all", "Comma-separated canonical namespaces to include (e.g., org,community). Use 'all' for no filter.")
	pathPrefixesFlag = flag.String("canonical-path-prefixes", "", "Comma-separated canonicalPath prefixes to include (e.g., /org/na,/community/ru). Empty = no filter.")

	pageSize   = flag.Int("page-size", 500, "Page size for Firestore pagination (recommended: 100-500)")
	limit      = flag.Int("limit", 0, "Limit number of matching fragments to backfill (0 = unlimited)")
	maxScan    = flag.Int("max-scan", 0, "Max documents to scan before stopping (0 = unlimited)")
	startAfter = flag.String("start-after", "", "Resume cursor: RFC3339 updatedAt timestamp (requires --start-after-id)")
	startAfterID = flag.String("start-after-id", "", "Resume cursor: Firestore document ID (requires --start-after)")
	cursorFile = flag.String("cursor-file", "", "Optional cursor file for resume/persist")
	saveCursorEveryPages   = flag.Int("save-cursor-every-pages", 1, "Persist cursor after every N pages when --cursor-file is set (minimum 1)")
	scanProgressEveryPages = flag.Int("scan-progress-every-pages", 10, "Log scan progress every N pages (0 disables)")

	dryRun          = flag.Bool("dry-run", true, "Dry run mode: only report would-change counts (no writes)")
	overwrite       = flag.Bool("overwrite", false, "Overwrite existing canonicalParts (default: only fill when missing)")
	touchTimestamps = flag.Bool("touch-timestamps", false, "When updating canonicalParts, also update updatedAt and metadata.lastUpdatedAt")

	sleepPerPageMs = flag.Int("sleep-per-page-ms", 200, "Sleep between pages (ms) to reduce Firestore load")
	updateQPS      = flag.Float64("update-qps", 10, "Max updates per second (0 = no throttle)")
	previewCount   = flag.Int("preview", 0, "Print up to N candidate samples (canonicalPath + names) for inspection")
)

type stats struct {
	Scanned    int
	Candidates int
	WouldWrite int
	Wrote      int
	Skipped    int
	Errors     int
	SkippedEmptyPath     int
	SkippedPrefix        int
	SkippedNamespace     int
	SkippedHasParts      int
}

type previewItem struct {
	ID            string
	CanonicalPath string
	Name          string
	LegalName     string
	ShortName     string
	UpdatedAt     time.Time
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
		*pageSize = 500
	}
	if *saveCursorEveryPages <= 0 {
		*saveCursorEveryPages = 1
	}
	if *scanProgressEveryPages < 0 {
		*scanProgressEveryPages = 0
	}

	nsSet, err := parseNamespaces(*namespacesFlag)
	if err != nil {
		log.Fatalf("FATAL: invalid --namespaces: %v", err)
	}
	prefixes := parsePrefixes(*pathPrefixesFlag)

	var cursor cursorState
	if strings.TrimSpace(*startAfter) == "" && strings.TrimSpace(*startAfterID) == "" && strings.TrimSpace(*cursorFile) != "" {
		if st, err := readCursorFile(*cursorFile); err == nil {
			cursor = *st
			if !cursor.LastUpdatedAt.IsZero() && strings.TrimSpace(cursor.LastDocID) != "" {
				*startAfter = cursor.LastUpdatedAt.UTC().Format(time.RFC3339)
				*startAfterID = strings.TrimSpace(cursor.LastDocID)
				log.Printf("loaded cursor from %s: updatedAt=%s id=%s", *cursorFile, *startAfter, *startAfterID)
			}
		}
	}

	startAfterTime, err := parseStartAfter(*startAfter, *startAfterID)
	if err != nil {
		log.Fatalf("FATAL: invalid start-after cursor: %v", err)
	}

	log.Printf("=== Horizon: canonicalParts backfill ===")
	log.Printf("project=%s tenant=%s dryRun=%v namespaces=%s prefixes=%s pageSize=%d limit=%d maxScan=%d startAfter=%s startAfterId=%s overwrite=%v touchTimestamps=%v",
		strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), *dryRun,
		strings.Join(sortedKeys(nsSet), ","), strings.Join(prefixes, ","), *pageSize, *limit, *maxScan,
		strings.TrimSpace(*startAfter), strings.TrimSpace(*startAfterID), *overwrite, *touchTimestamps,
	)

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer db.Close()

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

	var st stats
	var pages int
	lastCursorUpdatedAt := startAfterTime
	lastCursorID := strings.TrimSpace(*startAfterID)
	preview := make([]previewItem, 0, maxInt(0, *previewCount))

	for {
		if *maxScan > 0 && st.Scanned >= *maxScan {
			break
		}
		if *limit > 0 && st.Candidates >= *limit {
			break
		}

		col := db.Collection("horizon_fragments")
		q := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			OrderBy("updatedAt", firestore.Asc).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if !lastCursorUpdatedAt.IsZero() && strings.TrimSpace(lastCursorID) != "" {
			q = q.StartAfter(lastCursorUpdatedAt, lastCursorID)
		}

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				st.Errors++
				log.Printf("ERROR: page iteration failed after scanned=%d: %v", st.Scanned, err)
				return
			}
			pageDocs = append(pageDocs, doc)
		}
		iter.Stop()

		if len(pageDocs) == 0 {
			break
		}
		pages++

		for _, doc := range pageDocs {
			if doc == nil || !doc.Exists() {
				continue
			}
			if *maxScan > 0 && st.Scanned >= *maxScan {
				break
			}
			if *limit > 0 && st.Candidates >= *limit {
				break
			}

			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				st.Errors++
				log.Printf("WARNING: failed to decode fragment %s: %v", doc.Ref.ID, err)
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}

			st.Scanned++
			lastCursorUpdatedAt = docUpdatedAt(doc, &frag)
			lastCursorID = doc.Ref.ID

			cp := strings.TrimSpace(frag.CanonicalPath)
			if cp == "" {
				st.SkippedEmptyPath++
				st.Skipped++
				continue
			}
			if !matchesAnyPrefix(cp, prefixes) {
				st.SkippedPrefix++
				st.Skipped++
				continue
			}
			ns := namespaceFromPath(cp)
			if len(nsSet) > 0 {
				if _, ok := nsSet[ns]; !ok {
					st.SkippedNamespace++
					st.Skipped++
					continue
				}
			}

			if !*overwrite && frag.CanonicalParts != nil {
				st.SkippedHasParts++
				st.Skipped++
				continue
			}

			parts, err := taxonomy.ParseCanonicalPathParts(cp)
			if err != nil || parts == nil {
				st.Errors++
				log.Printf("WARNING: failed to parse canonicalParts (id=%s path=%s err=%v)", frag.ID, cp, err)
				continue
			}

			st.Candidates++
			st.WouldWrite++
			if *previewCount > 0 && len(preview) < *previewCount {
				preview = append(preview, previewItem{
					ID:            strings.TrimSpace(frag.ID),
					CanonicalPath: cp,
					Name:          bestName(&frag),
					LegalName:     strings.TrimSpace(frag.Identifiers["legalName"]),
					ShortName:     strings.TrimSpace(frag.Identifiers["shortName"]),
					UpdatedAt:     lastCursorUpdatedAt,
				})
			}
			if *dryRun {
				continue
			}

			if tick != nil {
				<-tick
			}

			updates := []firestore.Update{
				{Path: "canonicalParts", Value: parts},
			}
			if *touchTimestamps {
				now := time.Now().UTC()
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
		}

		if strings.TrimSpace(*cursorFile) != "" && lastCursorID != "" && *saveCursorEveryPages > 0 && pages%*saveCursorEveryPages == 0 {
			cursor := &cursorState{
				LastUpdatedAt: lastCursorUpdatedAt,
				LastDocID:     lastCursorID,
				Scanned:       st.Scanned,
				Candidates:    st.Candidates,
				WouldWrite:    st.WouldWrite,
				Wrote:         st.Wrote,
				Skipped:       st.Skipped,
				Errors:        st.Errors,
			}
			_ = writeCursorFile(*cursorFile, cursor)
		}

		if *scanProgressEveryPages > 0 && pages%*scanProgressEveryPages == 0 {
			log.Printf("Progress: scanned=%d candidates=%d wouldWrite=%d wrote=%d skipped=%d errors=%d cursor=%s",
				st.Scanned, st.Candidates, st.WouldWrite, st.Wrote, st.Skipped, st.Errors, lastCursorID)
		}

		if *limit > 0 && st.Candidates >= *limit {
			break
		}
		if *maxScan > 0 && st.Scanned >= *maxScan {
			break
		}
		if len(pageDocs) < *pageSize {
			break
		}
		if *sleepPerPageMs > 0 {
			time.Sleep(time.Duration(*sleepPerPageMs) * time.Millisecond)
		}
	}

	log.Println("\n=== Backfill Summary ===")
	log.Printf("Scanned:    %d", st.Scanned)
	log.Printf("Candidates: %d", st.Candidates)
	log.Printf("WouldWrite: %d", st.WouldWrite)
	log.Printf("Wrote:      %d", st.Wrote)
	log.Printf("Skipped:    %d", st.Skipped)
	log.Printf("SkippedEmptyPath: %d", st.SkippedEmptyPath)
	log.Printf("SkippedPrefix:    %d", st.SkippedPrefix)
	log.Printf("SkippedNamespace: %d", st.SkippedNamespace)
	log.Printf("SkippedHasParts:  %d", st.SkippedHasParts)
	log.Printf("Errors:     %d", st.Errors)
	log.Printf("DryRun:     %v", *dryRun)
	if lastCursorID != "" {
		log.Printf("Cursor:     updatedAt=%s id=%s", lastCursorUpdatedAt.UTC().Format(time.RFC3339), lastCursorID)
	}
	if len(preview) > 0 {
		log.Println("\n=== Preview Candidates ===")
		for i, item := range preview {
			log.Printf("%d) %s | name=%q legal=%q short=%q updatedAt=%s id=%s",
				i+1,
				item.CanonicalPath,
				item.Name,
				item.LegalName,
				item.ShortName,
				item.UpdatedAt.UTC().Format(time.RFC3339),
				item.ID,
			)
		}
	}
	if *dryRun {
		log.Println("\nRun with --dry-run=false to apply changes.")
	}
}

func parseNamespaces(raw string) (map[string]struct{}, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "all" {
		return map[string]struct{}{}, nil
	}
	allowed := map[string]struct{}{
		"org": {}, "gov": {}, "product": {}, "community": {}, "loc": {}, "segment": {}, "market": {},
		"fac": {}, "program": {}, "person": {}, "media": {}, "deposit": {}, "sphere": {}, "event": {},
	}
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		ns := strings.TrimSpace(strings.ToLower(part))
		if ns == "" {
			continue
		}
		if _, ok := allowed[ns]; !ok {
			return nil, fmt.Errorf("unknown namespace: %q", ns)
		}
		out[ns] = struct{}{}
	}
	return out, nil
}

func parseStartAfter(ts string, id string) (time.Time, error) {
	ts = strings.TrimSpace(ts)
	id = strings.TrimSpace(id)
	if ts == "" && id == "" {
		return time.Time{}, nil
	}
	if ts == "" || id == "" {
		return time.Time{}, fmt.Errorf("both --start-after and --start-after-id are required for resume")
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func namespaceFromPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func parsePrefixes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func matchesAnyPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func docUpdatedAt(doc *firestore.DocumentSnapshot, frag *models.HorizonFragment) time.Time {
	if doc != nil {
		if v, err := doc.DataAt("updatedAt"); err == nil {
			if t, ok := v.(time.Time); ok {
				return t
			}
		}
	}
	if frag != nil && !frag.UpdatedAt.IsZero() {
		return frag.UpdatedAt
	}
	return time.Time{}
}

func bestName(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if v := strings.TrimSpace(f.Labels["en"]); v != "" {
		return v
	}
	for _, v := range f.Labels {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if v := strings.TrimSpace(f.Identifiers["legalName"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(f.Identifiers["shortName"]); v != "" {
		return v
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return []string{"all"}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i] > out[j] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
