package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: horizon_audit_market_segments_ui
//
// Scans Firestore `horizon_fragments` and reports which market segment normals exist in DB
// but are NOT rendered on node cards in the frontend.
//
// Frontend behavior mirrored here (as of web/src/components/HorizonGraphNodeTypes.tsx):
// - segment badges are shown ONLY for ORG/GOV nodes:
//   - type == ORGANIZATION OR canonicalPath starts with /org/ or /gov/
// - badges are derived from marketSegmentNorms (preferred) else from raw /segment/... paths
// - badges render only if segmentIconType(normDomain, normVertical) returns a non-empty icon type
// - max 3 badges per node (dedup by icon type)
//
// Auth:
// - Uses Application Default Credentials (gcloud auth).
// - Project ID is taken from --project, env, or `gcloud config get-value project`.

type fragLite struct {
	TenantID               string                   `firestore:"tenantId"`
	ID                     string                   `firestore:"id"`
	CanonicalPath          string                   `firestore:"canonicalPath"`
	Type                  string                   `firestore:"type"`
	MarketSegments         []string                 `firestore:"marketSegments"`
	MarketSegmentNorms     []coremodels.MarketSegmentNorm `firestore:"marketSegmentNorms"`
	MarketSegmentNormPairs []string                 `firestore:"marketSegmentNormPairs"`
}

type pairStat struct {
	Count   int
	Samples []string // canonicalPath samples
	Segs    []string // segmentPath samples
}

type iconStat struct {
	Count int
}

func envProjectID() string {
	for _, k := range []string{"GCP_PROJECT_ID", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func gcloudProjectID() string {
	// Best-effort: read active gcloud project.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	// gcloud can print "(unset)" sometimes
	if v == "" || strings.Contains(v, "unset") {
		return ""
	}
	return v
}

func isOrgGovNode(ft, canonicalPath string) bool {
	t := strings.ToUpper(strings.TrimSpace(ft))
	cp := strings.TrimSpace(canonicalPath)
	if t == "ORGANIZATION" {
		return true
	}
	// Frontend also allows by canonicalPath prefix.
	return strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/")
}

func segmentGeo(segPath string) string {
	// Canonical: /segment/{domain}/{vertical}/{geo}
	parts := strings.Split(strings.Trim(strings.TrimSpace(segPath), "/"), "/")
	if len(parts) == 4 && parts[0] == "segment" {
		return strings.ToLower(strings.TrimSpace(parts[3]))
	}
	return ""
}

func addSample(st *pairStat, cp string, seg string, max int) {
	if st == nil || max <= 0 {
		return
	}
	if cp != "" && len(st.Samples) < max {
		st.Samples = append(st.Samples, cp)
	}
	if seg != "" && len(st.Segs) < max {
		st.Segs = append(st.Segs, seg)
	}
}

// segmentIconType mirrors frontend segmentIconType() heuristics.
// Returns icon type string or "" if no mapping exists (=> badge not rendered).
func segmentIconType(normDomain string, normVertical string) string {
	d := strings.ToLower(strings.TrimSpace(normDomain))
	v := strings.ToLower(strings.TrimSpace(normVertical))
	if d == "" {
		return ""
	}
	// Normalize common aliases (frontend sees both "finance" and "financial").
	d0 := d
	if d0 == "financial" {
		d0 = "finance"
	}

	// Renewables / storage: often encoded as vertical under energy.
	if d0 == "energy" && (v == "renewable-energy" ||
		v == "renewable-energy-storage" ||
		v == "renewables" ||
		strings.Contains(v, "renewable") ||
		strings.Contains(v, "solar") ||
		strings.Contains(v, "wind") ||
		strings.Contains(v, "storage") ||
		strings.Contains(v, "battery")) {
		return "MARKET_RENEWABLES"
	}

	// Accounting
	if d0 == "accounting" ||
		d0 == "bookkeeping" ||
		d0 == "audit" ||
		d0 == "auditing" ||
		strings.Contains(d0, "account") ||
		v == "accounting" ||
		v == "bookkeeping" ||
		v == "audit" ||
		v == "auditing" ||
		v == "tax" ||
		strings.Contains(v, "account") ||
		strings.Contains(v, "audit") ||
		strings.Contains(v, "tax") {
		return "MARKET_ACCOUNTING"
	}

	// Consulting / advisory
	if d0 == "consulting" ||
		d0 == "advisory" ||
		d0 == "management-consulting" ||
		strings.Contains(d0, "consult") ||
		v == "consulting" ||
		v == "advisory" ||
		v == "management-consulting" ||
		v == "professional-services" ||
		strings.Contains(v, "consult") ||
		strings.Contains(v, "advis") {
		return "MARKET_CONSULTING"
	}

	// Automotive / EV
	if d0 == "electric-vehicles" ||
		d0 == "electric-vehicle" ||
		d0 == "ev" ||
		d0 == "automotive" ||
		d0 == "vehicles" ||
		d0 == "vehicle" ||
		d0 == "auto" ||
		strings.Contains(d0, "vehicle") ||
		strings.Contains(d0, "automotive") ||
		strings.Contains(d0, "electric-vehicle") ||
		v == "electric-vehicles" ||
		v == "electric-vehicle" ||
		v == "ev" ||
		strings.Contains(v, "electric-vehicle") {
		return "MARKET_AUTOMOTIVE"
	}

	// Legal / compliance
	if d == "legal" ||
		d == "law" ||
		d == "legal-services" ||
		d == "compliance" ||
		strings.Contains(d, "legal") ||
		strings.Contains(d, "compliance") {
		return "MARKET_LEGAL"
	}

	// Trading / import-export / wholesale
	if d == "trading" ||
		d == "import-export" ||
		d == "wholesale" ||
		d == "wholesaling" ||
		strings.Contains(d, "trading") ||
		strings.Contains(d, "import") ||
		strings.Contains(d, "export") {
		return "MARKET_TRADING"
	}

	// "Commercial" is frequently used as a retail/commerce proxy.
	if d == "commercial" || d == "commerce" || d == "ecommerce" || d == "e-commerce" {
		return "MARKET_RETAIL"
	}

	// Energy / oil & gas (incl. services)
	if d == "energy" ||
		d == "oil-and-gas" ||
		d == "oil-and-gas-services" ||
		strings.Contains(d, "oil") ||
		strings.Contains(d, "gas") {
		return "MARKET_ENERGY"
	}

	// Logistics / freight / shipping
	if d0 == "logistics" ||
		d0 == "freight-forwarding" ||
		d0 == "contract-logistics" ||
		d0 == "supply-chain-management" ||
		d0 == "shipping" ||
		d0 == "transportation" ||
		d0 == "supply-chain" ||
		strings.Contains(d0, "freight") ||
		strings.Contains(d0, "logistic") {
		return "MARKET_LOGISTICS"
	}

	// Education
	if d0 == "education" ||
		d0 == "higher-education" ||
		strings.Contains(d0, "education") ||
		v == "education" ||
		v == "higher-education" ||
		strings.Contains(v, "education") ||
		strings.Contains(v, "university") ||
		strings.Contains(v, "college") ||
		strings.Contains(v, "school") ||
		strings.Contains(v, "academy") {
		return "MARKET_EDUCATION"
	}

	if d0 == "finance" {
		if v == "crypto" {
			return "MARKET_CRYPTO"
		}
		return "MARKET_FINANCE"
	}
	if d == "real-estate" {
		return "MARKET_REAL_ESTATE"
	}
	if d == "healthcare" ||
		strings.Contains(v, "hospital") ||
		strings.Contains(v, "clinic") ||
		strings.Contains(v, "healthcare") ||
		strings.Contains(v, "medical") {
		return "MARKET_HEALTHCARE"
	}
	if d == "construction" {
		return "MARKET_CONSTRUCTION"
	}
	if d == "retail" {
		return "MARKET_RETAIL"
	}
	if d == "government" || d == "public-administration" || d == "defense" {
		return "MARKET_GOVERNMENT"
	}
	if d == "entertainment" {
		if v == "casinos" || v == "gambling" {
			return "MARKET_GAMBLING"
		}
		return "MARKET_ENTERTAINMENT"
	}
	if d == "gambling" || d == "casinos" {
		return "MARKET_GAMBLING"
	}
	if d == "technology" {
		return "TECHNOLOGY"
	}
	return ""
}

func main() {
	var (
		projectID = flag.String("project", "", "GCP project ID (fallback: env, then gcloud config)")
		tenantID  = flag.String("tenant", "global", "tenantId filter (default: global)")

		pageSize   = flag.Int("page-size", 250, "Firestore page size (recommended: 100-500)")
		limit      = flag.Int("limit", 0, "Limit number of documents to scan (0 = all)")
		startAfter = flag.String("start-after", "", "Resume after this document ID (__name__)")

		dictVersion = flag.String("dict-version", "v1", "Market segment dictionary version to load (Firestore doc id)")
		sampleN     = flag.Int("sample", 3, "Samples per bucket (canonical paths)")
		topN        = flag.Int("top", 40, "Top N unmapped normPairs to print")
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
	if *pageSize <= 0 {
		*pageSize = 250
	}
	if *pageSize > 1000 {
		*pageSize = 1000
	}
	if *sampleN < 0 {
		*sampleN = 0
	}
	if *topN <= 0 {
		*topN = 40
	}

	ctx := context.Background()
	fsClient, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer fsClient.Close()

	// Load market segment dictionary into memory (so taxonomy.NormalizeMarketSegments matches backend behavior).
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient, strings.TrimSpace(*dictVersion), 20*time.Minute)
	if err := taxonomy.RefreshMarketSegmentDictionary(ctx); err != nil {
		log.Printf("WARNING: failed to load market segment dictionary: %v (will use built-in rules)", err)
	}

	log.Printf("Starting market segment UI audit (project=%s tenant=%s pageSize=%d limit=%d dict=%s)...",
		pid, tid, *pageSize, *limit, strings.TrimSpace(*dictVersion))

	col := fsClient.Collection("horizon_fragments")
	var lastDocID string
	if strings.TrimSpace(*startAfter) != "" {
		lastDocID = strings.TrimSpace(*startAfter)
	}

	// Aggregates
	var scanned int
	var withAnySeg int
	var withNorms int
	var eligibleWithSeg int
	var eligibleWithAnyIcon int
	var eligibleWithNoIcon int
	var eligibleTruncated int
	var nonEligibleWithSeg int

	mappedPairs := map[string]*pairStat{}
	unmappedPairs := map[string]*pairStat{}
	iconCounts := map[string]*iconStat{}
	channelCounts := map[string]int{}
	geoCounts := map[string]int{}

	// Focused checks for user-provided verticals (best-effort: substring match).
	targetVerticals := []string{"consumer-electronics", "healthcare", "manufacturing"}
	targetHits := map[string]int{}
	targetEligibleHidden := map[string]int{} // eligible nodes where this target appears but no icon mapping

	for {
		q := col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).Limit(*pageSize)
		if strings.TrimSpace(lastDocID) != "" {
			q = q.StartAfter(lastDocID)
		}
		// Field mask: keep it small.
		q = q.Select("tenantId", "id", "canonicalPath", "type", "marketSegments", "marketSegmentNorms", "marketSegmentNormPairs")

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				log.Fatalf("Firestore iteration failed after scanned=%d: %v", scanned, err)
			}
			pageDocs = append(pageDocs, doc)
		}
		iter.Stop()

		if len(pageDocs) == 0 {
			break
		}
		lastDocID = pageDocs[len(pageDocs)-1].Ref.ID

		for _, doc := range pageDocs {
			if doc == nil || !doc.Exists() {
				continue
			}
			if *limit > 0 && scanned >= *limit {
				break
			}
			scanned++

			var fr fragLite
			if err := doc.DataTo(&fr); err != nil {
				continue
			}
			if strings.TrimSpace(fr.ID) == "" {
				fr.ID = doc.Ref.ID
			}
			cp := strings.TrimSpace(fr.CanonicalPath)
			ft := strings.TrimSpace(fr.Type)
			eligible := isOrgGovNode(ft, cp)

			// Prefer stored norms; fallback compute from raw marketSegments (to cover older docs).
			norms := fr.MarketSegmentNorms
			if len(norms) == 0 && len(fr.MarketSegments) > 0 {
				norms, _, _, _ = taxonomy.NormalizeMarketSegments(fr.MarketSegments)
			}

			if len(norms) > 0 || len(fr.MarketSegments) > 0 {
				withAnySeg++
				if eligible {
					eligibleWithSeg++
				} else {
					nonEligibleWithSeg++
				}
			}
			if len(fr.MarketSegmentNorms) > 0 {
				withNorms++
			}

			// Collect per-fragment icon types for truncation detection.
			iconSet := map[string]struct{}{}
			// Also track if fragment has ANY mapped icon.
			hasMappedIcon := false

			for _, n := range norms {
				pair := strings.TrimSpace(n.NormPair)
				if pair == "" {
					continue
				}
				segPath := strings.TrimSpace(n.SegmentPath)

				// Geo stats (not shown in node badges).
				if g := segmentGeo(segPath); g != "" {
					geoCounts[g]++
				}
				// Channel stats (not shown in node badges).
				if ch := strings.ToLower(strings.TrimSpace(n.Channel)); ch != "" {
					channelCounts[ch]++
				}

				// Target vertical hits.
				for _, tv := range targetVerticals {
					if tv == "" {
						continue
					}
					// Best-effort: check in raw segmentPath or in normPair.
					if strings.Contains(segPath, "/"+tv+"/") || strings.Contains(pair, tv) {
						targetHits[tv]++
					}
				}

				iconType := segmentIconType(n.NormDomain, n.NormVertical)
				if iconType != "" {
					hasMappedIcon = true
					iconSet[iconType] = struct{}{}
					if iconCounts[iconType] == nil {
						iconCounts[iconType] = &iconStat{}
					}
					iconCounts[iconType].Count++

					if eligible {
						st := mappedPairs[pair]
						if st == nil {
							st = &pairStat{}
							mappedPairs[pair] = st
						}
						st.Count++
						addSample(st, cp, segPath, *sampleN)
					}
				} else {
					if eligible {
						st := unmappedPairs[pair]
						if st == nil {
							st = &pairStat{}
							unmappedPairs[pair] = st
						}
						st.Count++
						addSample(st, cp, segPath, *sampleN)

						// Target verticals: count eligible-but-hidden occurrences.
						for _, tv := range targetVerticals {
							if tv == "" {
								continue
							}
							if strings.Contains(segPath, "/"+tv+"/") || strings.Contains(pair, tv) {
								targetEligibleHidden[tv]++
							}
						}
					}
				}
			}

			if eligible && (len(norms) > 0 || len(fr.MarketSegments) > 0) {
				if hasMappedIcon {
					eligibleWithAnyIcon++
				} else {
					eligibleWithNoIcon++
				}
				if len(iconSet) > 3 {
					eligibleTruncated++
				}
			}

			if scanned%2000 == 0 {
				log.Printf("Progress: scanned=%d last=%s eligibleWithSeg=%d eligibleNoIcon=%d truncated=%d",
					scanned, lastDocID, eligibleWithSeg, eligibleWithNoIcon, eligibleTruncated)
			}
		}

		if *limit > 0 && scanned >= *limit {
			break
		}
	}

	// Sort and print report.
	type kv struct {
		K string
		V int
	}
	toSorted := func(m map[string]int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{K: k, V: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].V != out[j].V {
				return out[i].V > out[j].V
			}
			return out[i].K < out[j].K
		})
		return out
	}
	toSortedPairs := func(m map[string]*pairStat) []struct {
		Pair string
		Stat *pairStat
	} {
		out := make([]struct {
			Pair string
			Stat *pairStat
		}, 0, len(m))
		for k, st := range m {
			out = append(out, struct {
				Pair string
				Stat *pairStat
			}{Pair: k, Stat: st})
		}
		sort.Slice(out, func(i, j int) bool {
			ai, aj := 0, 0
			if out[i].Stat != nil {
				ai = out[i].Stat.Count
			}
			if out[j].Stat != nil {
				aj = out[j].Stat.Count
			}
			if ai != aj {
				return ai > aj
			}
			return out[i].Pair < out[j].Pair
		})
		return out
	}

	fmt.Printf("\n=== Horizon market segments UI audit ===\n")
	fmt.Printf("project=%s tenant=%s dictVersion=%s pageSize=%d limit=%d\n", pid, tid, strings.TrimSpace(*dictVersion), *pageSize, *limit)
	fmt.Printf("scanned=%d\n", scanned)
	fmt.Printf("\n")
	fmt.Printf("fragments_with_any_segments_or_norms=%d\n", withAnySeg)
	fmt.Printf("fragments_with_stored_marketSegmentNorms=%d\n", withNorms)
	fmt.Printf("\n")
	fmt.Printf("eligible_nodes_with_segments(ORG/GOV)=%d\n", eligibleWithSeg)
	fmt.Printf("eligible_nodes_with_any_mappable_icon=%d\n", eligibleWithAnyIcon)
	fmt.Printf("eligible_nodes_with_segments_but_NO_icons(=> node badges show nothing)=%d\n", eligibleWithNoIcon)
	fmt.Printf("eligible_nodes_truncated(>3 unique icon types)=%d\n", eligibleTruncated)
	fmt.Printf("non_eligible_nodes_with_segments(frontend hides badges by design)=%d\n", nonEligibleWithSeg)
	fmt.Printf("\n")

	fmt.Printf("== Target verticals (best-effort substring match) ==\n")
	for _, tv := range targetVerticals {
		fmt.Printf("%-20s hits=%d eligible_hidden=%d\n", tv, targetHits[tv], targetEligibleHidden[tv])
	}
	fmt.Printf("\n")

	fmt.Printf("== Top unmapped normPairs on eligible nodes (frontend has no icon mapping) ==\n")
	unm := toSortedPairs(unmappedPairs)
	for i := 0; i < len(unm) && i < *topN; i++ {
		it := unm[i]
		fmt.Printf("%6d  %s\n", it.Stat.Count, it.Pair)
		for _, s := range it.Stat.Segs {
			fmt.Printf("        seg: %s\n", s)
		}
		for _, s := range it.Stat.Samples {
			fmt.Printf("        cp : %s\n", s)
		}
	}
	if len(unm) == 0 {
		fmt.Printf("(none)\n")
	}
	fmt.Printf("\n")

	fmt.Printf("== Top mapped icon types (overall, from marketSegmentNorms) ==\n")
	iconList := make([]kv, 0, len(iconCounts))
	for k, st := range iconCounts {
		if st == nil {
			continue
		}
		iconList = append(iconList, kv{K: k, V: st.Count})
	}
	sort.Slice(iconList, func(i, j int) bool {
		if iconList[i].V != iconList[j].V {
			return iconList[i].V > iconList[j].V
		}
		return iconList[i].K < iconList[j].K
	})
	for i := 0; i < len(iconList) && i < 30; i++ {
		fmt.Printf("%6d  %s\n", iconList[i].V, iconList[i].K)
	}
	if len(iconList) == 0 {
		fmt.Printf("(none)\n")
	}
	fmt.Printf("\n")

	fmt.Printf("== Channels present in norms (not rendered in node badges) ==\n")
	for i, it := range toSorted(channelCounts) {
		if i >= 25 {
			break
		}
		fmt.Printf("%6d  %s\n", it.V, it.K)
	}
	if len(channelCounts) == 0 {
		fmt.Printf("(none)\n")
	}
	fmt.Printf("\n")

	fmt.Printf("== Segment geo distribution (not rendered in node badges) ==\n")
	for i, it := range toSorted(geoCounts) {
		if i >= 25 {
			break
		}
		fmt.Printf("%6d  %s\n", it.V, it.K)
	}
	if len(geoCounts) == 0 {
		fmt.Printf("(none)\n")
	}
	fmt.Printf("\n")
}

