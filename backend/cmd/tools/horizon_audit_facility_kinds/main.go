package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: horizon_audit_facility_kinds
//
// Scans Firestore `horizon_fragments` and reports FACILITY subtype coverage:
// - what facilityKind enum values exist in code
// - what is actually populated in DB
// - where classification is missed by stored value and/or path heuristic
//
// "Stored" classification = identifiers["facilityKind"] (normalized).
// "Heuristic" classification = taxonomy.FacilityKindFromCanonicalPath(canonicalPath).
//
// Auth:
// - Uses ADC (gcloud auth application-default login).
// - Project ID is taken from --project, env, or `gcloud config get-value project`.

type countRow struct {
	Key   string
	Count int
}

type sampleCounter struct {
	Count   int
	Samples []string
}

var slugTokenRe = regexp.MustCompile(`[a-z0-9]+`)

func envProjectID() string {
	for _, k := range []string{"GCP_PROJECT_ID", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func gcloudProjectID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "" || strings.Contains(strings.ToLower(v), "unset") {
		return ""
	}
	return v
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func asMap(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return t
	case map[string]string:
		out := make(map[string]interface{}, len(t))
		for k, vv := range t {
			out[k] = vv
		}
		return out
	default:
		return nil
	}
}

func firstMapString(m map[string]interface{}, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := asString(v)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func labelEN(data map[string]interface{}) string {
	labels := asMap(data["labels"])
	if labels == nil {
		return ""
	}
	return asString(labels["en"])
}

func kindLabel(k taxonomy.FacilityKind) string {
	switch k {
	case taxonomy.FacilityKindUnknown:
		return "unknown"
	case taxonomy.FacilityKindOther:
		return "other"
	default:
		return string(k)
	}
}

func storedKindBucket(raw string, norm taxonomy.FacilityKind) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "missing"
	}
	if norm == taxonomy.FacilityKindUnknown {
		return "missing"
	}
	if norm == taxonomy.FacilityKindOther {
		if strings.EqualFold(raw, "other") {
			return "other"
		}
		return "other_invalid_raw"
	}
	return string(norm)
}

func isConcreteKind(k taxonomy.FacilityKind) bool {
	return k != taxonomy.FacilityKindUnknown && k != taxonomy.FacilityKindOther
}

func isStoredConcrete(raw string, norm taxonomy.FacilityKind) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	return isConcreteKind(norm)
}

func pct(part, total int) string {
	if total <= 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(part)*100.0/float64(total))
}

func addCount(m map[string]int, key string, delta int) {
	if key == "" {
		return
	}
	m[key] += delta
}

func addSample(m map[string]*sampleCounter, key, sample string, maxSamples int) {
	if key == "" {
		return
	}
	b, ok := m[key]
	if !ok {
		b = &sampleCounter{}
		m[key] = b
	}
	b.Count++
	sample = strings.TrimSpace(sample)
	if sample != "" && maxSamples > 0 && len(b.Samples) < maxSamples {
		b.Samples = append(b.Samples, sample)
	}
}

func topRows(m map[string]int, top int) []countRow {
	rows := make([]countRow, 0, len(m))
	for k, v := range m {
		rows = append(rows, countRow{Key: k, Count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Count > rows[j].Count
	})
	if top > 0 && len(rows) > top {
		return rows[:top]
	}
	return rows
}

func slugFromCanonicalPath(cp string) string {
	cp = strings.ToLower(strings.TrimSpace(cp))
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] != "fac" {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func tokenizeSlug(slug string) []string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	raw := slugTokenRe.FindAllString(slug, -1)
	if len(raw) == 0 {
		return nil
	}
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "near": {},
		"north": {}, "south": {}, "east": {}, "west": {}, "upper": {}, "lower": {},
		"new": {}, "old": {}, "main": {}, "central": {}, "global": {}, "international": {},
		"facility": {}, "facilities": {}, "site": {}, "complex": {}, "zone": {}, "area": {}, "unit": {},
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		if len(tok) < 3 {
			continue
		}
		if _, ok := stop[tok]; ok {
			continue
		}
		isNumeric := true
		for _, r := range tok {
			if r < '0' || r > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func samplePath(cp, name string) string {
	cp = strings.TrimSpace(cp)
	name = strings.TrimSpace(name)
	if name == "" {
		return cp
	}
	return fmt.Sprintf("%s (%s)", cp, name)
}

func printTopMap(title string, m map[string]int, top int, total int) {
	fmt.Println(title)
	rows := topRows(m, top)
	if len(rows) == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, r := range rows {
		if total > 0 {
			fmt.Printf("  %-24s %8d  (%s)\n", r.Key, r.Count, pct(r.Count, total))
		} else {
			fmt.Printf("  %-24s %8d\n", r.Key, r.Count)
		}
	}
}

func printTopSamples(title string, m map[string]*sampleCounter, top int) {
	fmt.Println(title)
	if len(m) == 0 {
		fmt.Println("  (none)")
		return
	}
	rows := make([]countRow, 0, len(m))
	for k, v := range m {
		rows = append(rows, countRow{Key: k, Count: v.Count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Count > rows[j].Count
	})
	if top > 0 && len(rows) > top {
		rows = rows[:top]
	}
	for _, r := range rows {
		fmt.Printf("  %s: %d\n", r.Key, r.Count)
		b := m[r.Key]
		for _, s := range b.Samples {
			fmt.Printf("    - %s\n", s)
		}
	}
}

func main() {
	var (
		projectID  = flag.String("project", "", "GCP project ID (fallback: env, then gcloud config)")
		tenantID   = flag.String("tenant", "global", "tenantId filter (default: global)")
		pageSize   = flag.Int("page-size", 500, "Firestore page size (default: 500, max: 1000)")
		limit      = flag.Int("limit", 0, "Max docs to scan from horizon_fragments (0 = all)")
		startAfter = flag.String("start-after", "", "Resume scan after document ID (__name__)")
		topN       = flag.Int("top", 20, "Top N rows in detailed sections")
		sampleN    = flag.Int("sample", 3, "Sample canonical paths per bucket")
	)
	flag.Parse()

	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = envProjectID()
	}
	if pid == "" {
		pid = gcloudProjectID()
	}
	if pid == "" {
		log.Fatal("project id not provided; set --project or env GOOGLE_CLOUD_PROJECT (or configure gcloud project)")
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
	if *topN <= 0 {
		*topN = 20
	}
	if *sampleN <= 0 {
		*sampleN = 3
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	client, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer client.Close()

	col := client.Collection("horizon_fragments")
	base := col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).Limit(ps)

	var last *firestore.DocumentSnapshot
	if sa := strings.TrimSpace(*startAfter); sa != "" {
		snap, err := col.Doc(sa).Get(ctx)
		if err != nil {
			log.Fatalf("failed to resolve --start-after doc %q: %v", sa, err)
		}
		last = snap
	}

	var (
		scanned              int
		facilitiesProcessed  int
		facByType            int
		facByPath            int
		typePathMismatchA    int // type=FACILITY but path !/fac/
		typePathMismatchB    int // path /fac/ but type!=FACILITY
		storedConcreteTotal  int
		heurConcreteTotal    int
		bothConcreteMatch    int
		bothConcreteMismatch int
		storedOnlyConcrete   int
		heurOnlyConcrete     int
		bothMiss             int
	)

	storedKindCounts := make(map[string]int)
	heurKindCounts := make(map[string]int)
	storedRawCounts := make(map[string]int)
	storedInvalidRaw := make(map[string]int)
	matrixCounts := make(map[string]int)

	mismatchSamples := make(map[string]*sampleCounter)
	heurOnlySamples := make(map[string]*sampleCounter)
	storedOnlySamples := make(map[string]*sampleCounter)
	bothMissSamples := make(map[string]*sampleCounter)

	heurOnlyByKind := make(map[string]int)   // heuristic concrete, stored not concrete
	storedOnlyByKind := make(map[string]int) // stored concrete, heuristic not concrete
	bothMissTokenCounts := make(map[string]int)
	bothMissTokenSample := make(map[string]*sampleCounter)

	storedOnlyTokenByKind := make(map[string]map[string]int)                   // kind -> token -> count
	storedOnlyTokenByKindSamples := make(map[string]map[string]*sampleCounter) // kind -> token -> sample

	stop := false
	for {
		q := base
		if last != nil {
			q = q.StartAfter(last)
		}
		q = q.Select("id", "type", "canonicalPath", "labels", "identifiers")
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("firestore query failed after scanned=%d: %v", scanned, err)
		}
		if len(docs) == 0 {
			break
		}
		last = docs[len(docs)-1]

		for _, d := range docs {
			if *limit > 0 && scanned >= *limit {
				stop = true
				break
			}
			scanned++
			data := d.Data()
			cp := asString(data["canonicalPath"])
			typ := strings.ToUpper(asString(data["type"]))
			name := labelEN(data)
			sample := samplePath(cp, name)

			isFacType := typ == "FACILITY"
			isFacPath := strings.HasPrefix(strings.ToLower(strings.TrimSpace(cp)), "/fac/")
			if isFacType {
				facByType++
			}
			if isFacPath {
				facByPath++
			}
			if isFacType && !isFacPath {
				typePathMismatchA++
			}
			if isFacPath && !isFacType {
				typePathMismatchB++
			}
			if !isFacType && !isFacPath {
				continue
			}
			facilitiesProcessed++

			ident := asMap(data["identifiers"])
			rawStored := firstMapString(ident, "facilityKind", "facility_kind")
			normStored := taxonomy.NormalizeFacilityKind(rawStored)
			heur := taxonomy.FacilityKindFromCanonicalPath(cp)

			storedBucket := storedKindBucket(rawStored, normStored)
			heurBucket := kindLabel(heur)
			addCount(storedKindCounts, storedBucket, 1)
			addCount(heurKindCounts, heurBucket, 1)
			addCount(matrixCounts, fmt.Sprintf("%s -> %s", storedBucket, heurBucket), 1)

			if strings.TrimSpace(rawStored) != "" {
				addCount(storedRawCounts, strings.ToLower(strings.TrimSpace(rawStored)), 1)
				if normStored == taxonomy.FacilityKindOther && !strings.EqualFold(strings.TrimSpace(rawStored), "other") {
					addCount(storedInvalidRaw, strings.ToLower(strings.TrimSpace(rawStored)), 1)
				}
			}

			storedConcrete := isStoredConcrete(rawStored, normStored)
			heurConcrete := isConcreteKind(heur)
			if storedConcrete {
				storedConcreteTotal++
			}
			if heurConcrete {
				heurConcreteTotal++
			}

			slug := slugFromCanonicalPath(cp)
			if storedConcrete && heurConcrete {
				if normStored == heur {
					bothConcreteMatch++
				} else {
					bothConcreteMismatch++
					key := fmt.Sprintf("%s != %s", string(normStored), string(heur))
					addSample(mismatchSamples, key, sample, *sampleN)
				}
				continue
			}
			if storedConcrete && !heurConcrete {
				storedOnlyConcrete++
				k := string(normStored)
				addCount(storedOnlyByKind, k, 1)
				addSample(storedOnlySamples, k, sample, *sampleN)
				for _, tok := range tokenizeSlug(slug) {
					km, ok := storedOnlyTokenByKind[k]
					if !ok {
						km = make(map[string]int)
						storedOnlyTokenByKind[k] = km
					}
					km[tok]++
					sm, ok := storedOnlyTokenByKindSamples[k]
					if !ok {
						sm = make(map[string]*sampleCounter)
						storedOnlyTokenByKindSamples[k] = sm
					}
					addSample(sm, tok, sample, *sampleN)
				}
				continue
			}
			if !storedConcrete && heurConcrete {
				heurOnlyConcrete++
				k := string(heur)
				addCount(heurOnlyByKind, k, 1)
				addSample(heurOnlySamples, k, sample, *sampleN)
				continue
			}

			// !storedConcrete && !heurConcrete
			bothMiss++
			addSample(bothMissSamples, "both_miss", sample, *sampleN*3)
			for _, tok := range tokenizeSlug(slug) {
				bothMissTokenCounts[tok]++
				addSample(bothMissTokenSample, tok, sample, *sampleN)
			}
		}

		if stop {
			break
		}
	}

	fmt.Printf("=== Facility Kind Audit ===\n")
	fmt.Printf("project=%s tenant=%s scanned_docs=%d facilities_analyzed=%d\n", pid, tid, scanned, facilitiesProcessed)
	fmt.Printf("fac_by_type=%d fac_by_path=%d type=FACILITY_without_/fac/=%d path_/fac/_without_type=FACILITY=%d\n",
		facByType, facByPath, typePathMismatchA, typePathMismatchB)
	fmt.Println()

	kinds := make([]string, 0, len(taxonomy.FacilityKinds()))
	for _, k := range taxonomy.FacilityKinds() {
		if k == taxonomy.FacilityKindUnknown {
			continue
		}
		kinds = append(kinds, string(k))
	}
	fmt.Printf("Facility kinds in code (%d): %s\n", len(kinds), strings.Join(kinds, ", "))
	fmt.Println()

	printTopMap("Stored facilityKind distribution (normalized):", storedKindCounts, 0, facilitiesProcessed)
	fmt.Println()
	printTopMap("Heuristic distribution (from canonicalPath slug):", heurKindCounts, 0, facilitiesProcessed)
	fmt.Println()

	fmt.Println("Coverage summary:")
	fmt.Printf("  stored concrete kind      : %d (%s)\n", storedConcreteTotal, pct(storedConcreteTotal, facilitiesProcessed))
	fmt.Printf("  heuristic concrete kind   : %d (%s)\n", heurConcreteTotal, pct(heurConcreteTotal, facilitiesProcessed))
	fmt.Printf("  both concrete + match     : %d (%s)\n", bothConcreteMatch, pct(bothConcreteMatch, facilitiesProcessed))
	fmt.Printf("  both concrete + mismatch  : %d (%s)\n", bothConcreteMismatch, pct(bothConcreteMismatch, facilitiesProcessed))
	fmt.Printf("  stored concrete only      : %d (%s)\n", storedOnlyConcrete, pct(storedOnlyConcrete, facilitiesProcessed))
	fmt.Printf("  heuristic concrete only   : %d (%s)\n", heurOnlyConcrete, pct(heurOnlyConcrete, facilitiesProcessed))
	fmt.Printf("  both non-concrete (miss)  : %d (%s)\n", bothMiss, pct(bothMiss, facilitiesProcessed))
	fmt.Println()

	printTopMap("Stored vs heuristic matrix (top):", matrixCounts, *topN, facilitiesProcessed)
	fmt.Println()

	printTopSamples("Concrete mismatch samples (stored != heuristic):", mismatchSamples, *topN)
	fmt.Println()

	printTopMap("Heuristic concrete, stored missing/other (likely LLM/ingest misses):", heurOnlyByKind, *topN, heurOnlyConcrete)
	printTopSamples("Samples for heuristic concrete, stored missing/other:", heurOnlySamples, *topN)
	fmt.Println()

	printTopMap("Stored concrete, heuristic miss by stored kind:", storedOnlyByKind, *topN, storedOnlyConcrete)
	printTopSamples("Samples for stored concrete, heuristic miss:", storedOnlySamples, *topN)
	fmt.Println()

	if len(storedOnlyTokenByKind) > 0 {
		fmt.Println("Candidate slug tokens for heuristic extension (from stored concrete + heuristic miss):")
		kindsSorted := make([]string, 0, len(storedOnlyTokenByKind))
		for k := range storedOnlyTokenByKind {
			kindsSorted = append(kindsSorted, k)
		}
		sort.Strings(kindsSorted)
		for _, k := range kindsSorted {
			fmt.Printf("  kind=%s\n", k)
			rows := topRows(storedOnlyTokenByKind[k], *topN)
			if len(rows) == 0 {
				fmt.Println("    (no tokens)")
				continue
			}
			for _, r := range rows {
				fmt.Printf("    %-20s %6d\n", r.Key, r.Count)
				if sm := storedOnlyTokenByKindSamples[k][r.Key]; sm != nil {
					for _, s := range sm.Samples {
						fmt.Printf("      - %s\n", s)
					}
				}
			}
		}
		fmt.Println()
	}

	printTopMap("Both miss candidate tokens (stored missing/other + heuristic unknown/other):", bothMissTokenCounts, *topN, bothMiss)
	printTopSamples("Both miss sample paths:", bothMissSamples, 1)
	fmt.Println()

	printTopMap("Raw facilityKind values in DB (top):", storedRawCounts, *topN, facilitiesProcessed)
	fmt.Println()
	printTopMap("Raw facilityKind values not normalized to known enum (top):", storedInvalidRaw, *topN, facilitiesProcessed)
}
