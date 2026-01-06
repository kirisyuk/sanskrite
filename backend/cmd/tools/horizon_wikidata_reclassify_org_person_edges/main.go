package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

// Goal: reclassify generic Person<->Org edges (INFLUENCES/DEPENDS_ON/...) into
// governance relations (EMPLOYED_BY / MANAGES / BOARD_MEMBER_OF / FOUNDED / OWNS)
// using deterministic Wikidata evidence (WDQS).

var (
	projectID   = flag.String("project", "", "GCP project ID (required)")
	tenantID    = flag.String("tenant", "global", "Tenant ID (default: global)")
	limit       = flag.Int("limit", 100, "Max number of output rows to emit (scanning may exceed this due to filters)")
	maxScan     = flag.Int("max-scan", 50000, "Max number of edges to scan before stopping (safety cap)")
	pageSize    = flag.Int("page-size", 500, "Firestore page size")
	startAfter  = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	edgeTypes   = flag.String("edge-types", "INFLUENCES,DEPENDS_ON", "Comma-separated list of edge types to consider for reclassification")

	scoreMin  = flag.Int("score-min", 150, "Minimum score required to apply reclassification")
	marginMin = flag.Int("margin-min", 25, "Minimum margin between best and 2nd-best to apply (avoid ambiguous rewrites)")

	apply     = flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	outPath   = flag.String("out", "", "Optional output JSONL file path (default: stdout)")
	progressN = flag.Int("progress-every", 100, "Print progress every N scanned edges (0 disables)")

	onlyPersonOrg   = flag.Bool("only-person-org", false, "Only emit rows for PERSON<->ORGANIZATION edges")
	requireQIDs     = flag.Bool("require-qids", false, "Only emit rows when BOTH endpoints have Wikidata QIDs")
	requireEvidence = flag.Bool("require-evidence", false, "Only emit rows when WDQS finds evidence matches")

	wdqsRateLimitMS = flag.Int("wdqs-rate-limit-ms", 250, "Delay between WDQS calls in milliseconds (global limiter)")
)

type Row struct {
	EdgeID      string `json:"edgeId"`
	OldType     string `json:"oldType"`
	NewType     string `json:"newType,omitempty"`
	WasReversed bool   `json:"wasReversed,omitempty"`
	Applied     bool   `json:"applied,omitempty"`

	// Decision metrics (explainable fuzzy logic)
	ScoreMin  int `json:"scoreMin"`
	MarginMin int `json:"marginMin"`
	Margin    int `json:"margin,omitempty"`

	SourceFragmentID string `json:"sourceFragmentId"`
	TargetFragmentID string `json:"targetFragmentId"`
	SourceType       string `json:"sourceType,omitempty"`
	TargetType       string `json:"targetType,omitempty"`

	PersonFragmentID string `json:"personFragmentId,omitempty"`
	OrgFragmentID    string `json:"orgFragmentId,omitempty"`
	PersonQID        string `json:"personQid,omitempty"`
	OrgQID           string `json:"orgQid,omitempty"`

	Matches []string         `json:"matches,omitempty"`
	Candidates []CandidateExplain `json:"candidates,omitempty"`
	Scores     map[string]int     `json:"scores,omitempty"`
	Best    *ScoredCandidate `json:"best,omitempty"`
	Second  *ScoredCandidate `json:"second,omitempty"`
	Reason  string           `json:"reason"`

	ScannedAt time.Time `json:"scannedAt"`
}

type ScoredCandidate struct {
	Relation string `json:"relation"`
	Score    int    `json:"score"`
}

type CandidateExplain struct {
	Relation string   `json:"relation"`
	Score    int      `json:"score"`
	Why      []string `json:"why,omitempty"`
}

// WDQS relation keys (internal evidence tags).
const (
	matchEmployedBy = "P108(person->org)"
	matchCEO        = "P169(org->person)"
	matchDirector   = "P1037(org->person)"
	matchBoard      = "P3320(org->person)"
	matchFoundedBy  = "P112(org->person)"
	matchOwnedBy    = "P127(org->person)"
)

// A candidate relation in Horizon terms, with a score.
type candidate struct {
	typ   hmodels.DependencyType
	score int
	why   []string
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *limit <= 0 {
		*limit = 100
	}
	if *maxScan <= 0 {
		*maxScan = 50000
	}
	if *pageSize <= 0 {
		*pageSize = 500
	}
	if *pageSize > 2000 {
		*pageSize = 2000
	}
	if *wdqsRateLimitMS < 0 {
		*wdqsRateLimitMS = 0
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	var out *os.File
	if strings.TrimSpace(*outPath) == "" {
		out = os.Stdout
	} else {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("FATAL: failed to open output file: %v", err)
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)

	edgeTypesList := parseEdgeTypes(*edgeTypes)
	if len(edgeTypesList) == 0 {
		log.Fatal("--edge-types resulted in empty list")
	}

	log.Printf("=== Horizon: Wikidata reclassify org-person edges ===")
	log.Printf("project=%s tenant=%s limit=%d maxScan=%d pageSize=%d apply=%v edgeTypes=%v scoreMin=%d marginMin=%d onlyPersonOrg=%v requireQIDs=%v requireEvidence=%v",
		*projectID, *tenantID, *limit, *maxScan, *pageSize, *apply, edgeTypesList, *scoreMin, *marginMin, *onlyPersonOrg, *requireQIDs, *requireEvidence)

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fragmentsRepo)

	fragCache := newFragmentCache(fragmentsRepo)
	wdqs := newWDQSPairClassifier(time.Duration(*wdqsRateLimitMS) * time.Millisecond)

	rows, scanned, updated, err := scanAndReclassify(ctx, db, edgesRepo, fragCache, wdqs, edgeTypesList, enc)
	if err != nil {
		log.Fatalf("FATAL: failed: %v", err)
	}
	log.Printf("✓ Done. scanned=%d emitted=%d updated=%d", scanned, rows, updated)
}

func scanAndReclassify(
	ctx context.Context,
	db *firestore.Client,
	edgesRepo *hrepo.DependencyEdgesRepository,
	fragCache *fragmentCache,
	wdqs *wdqsPairClassifier,
	edgeTypes []hmodels.DependencyType,
	enc *json.Encoder,
) (emitted int, scanned int, updated int, err error) {
	if db == nil || edgesRepo == nil || fragCache == nil || wdqs == nil {
		return 0, 0, 0, fmt.Errorf("misconfigured deps")
	}

	edgeTypesAny := make([]interface{}, 0, len(edgeTypes))
	for _, t := range edgeTypes {
		edgeTypesAny = append(edgeTypesAny, string(t))
	}

	base := db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "in", edgeTypesAny).
		OrderBy(firestore.DocumentID, firestore.Asc)

	cursor := strings.TrimSpace(*startAfter)
	lastSeen := cursor

	var scannedAtomic int64
	var updatedAtomic int64
	var emittedAtomic int64

	stopHeartbeat := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				log.Printf("heartbeat: scanned=%d emitted=%d updated=%d fragmentCache=%d wdqsCache=%d",
					atomic.LoadInt64(&scannedAtomic),
					atomic.LoadInt64(&emittedAtomic),
					atomic.LoadInt64(&updatedAtomic),
					fragCache.size(),
					wdqs.cacheSize(),
				)
			case <-stopHeartbeat:
				return
			}
		}
	}()
	defer close(stopHeartbeat)

	for emitted < *limit && scanned < *maxScan {
		pageQ := base.Limit(*pageSize)
		if lastSeen != "" {
			pageQ = pageQ.StartAfter(lastSeen)
		}
		iter := pageQ.Documents(ctx)
		pageCount := 0
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return emitted, scanned, updated, err
			}
			pageCount++
			scanned++
			atomic.StoreInt64(&scannedAtomic, int64(scanned))
			lastSeen = doc.Ref.ID
			if scanned > *maxScan {
				break
			}

			var edge hmodels.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}
			row := processEdge(ctx, edgesRepo, fragCache, wdqs, &edge)
			if row != nil {
				// Optional output filters (keep scanning until we collect --limit rows).
				if *onlyPersonOrg && (strings.TrimSpace(row.PersonFragmentID) == "" || strings.TrimSpace(row.OrgFragmentID) == "") {
					continue
				}
				if *requireQIDs && (strings.TrimSpace(row.PersonQID) == "" || strings.TrimSpace(row.OrgQID) == "") {
					continue
				}
				if *requireEvidence && len(row.Matches) == 0 {
					continue
				}

				_ = enc.Encode(row)
				emitted++
				atomic.StoreInt64(&emittedAtomic, int64(emitted))
				if row.Applied {
					updated++
					atomic.StoreInt64(&updatedAtomic, int64(updated))
				}
			}

			if *progressN > 0 && scanned%*progressN == 0 {
				log.Printf("progress: scanned=%d emitted=%d updated=%d lastDoc=%s", scanned, emitted, updated, lastSeen)
			}
			if emitted >= *limit || scanned >= *maxScan {
				break
			}
		}
		iter.Stop()
		if pageCount < *pageSize {
			break
		}
	}
	if lastSeen != "" {
		log.Printf("resume cursor: start-after=%s", lastSeen)
	}
	return emitted, scanned, updated, nil
}

func processEdge(ctx context.Context, edgesRepo *hrepo.DependencyEdgesRepository, fragCache *fragmentCache, wdqs *wdqsPairClassifier, edge *hmodels.DependencyEdge) *Row {
	now := time.Now().UTC()
	if edge == nil {
		return nil
	}
	tenant := strings.TrimSpace(edge.TenantID)
	if tenant == "" {
		tenant = strings.TrimSpace(*tenantID)
	}

	row := &Row{
		EdgeID:           strings.TrimSpace(edge.ID),
		OldType:          string(edge.Type),
		ScoreMin:         *scoreMin,
		MarginMin:        *marginMin,
		SourceFragmentID: strings.TrimSpace(edge.SourceFragmentID),
		TargetFragmentID: strings.TrimSpace(edge.TargetFragmentID),
		ScannedAt:        now,
		Reason:           "skipped",
	}
	if row.EdgeID == "" || row.SourceFragmentID == "" || row.TargetFragmentID == "" {
		row.Reason = "invalid edge ids"
		return row
	}

	srcFrag, err1 := fragCache.get(ctx, row.SourceFragmentID)
	dstFrag, err2 := fragCache.get(ctx, row.TargetFragmentID)
	if err1 != nil || err2 != nil || srcFrag == nil || dstFrag == nil {
		row.Reason = "missing fragments"
		return row
	}
	row.SourceType = string(srcFrag.Type)
	row.TargetType = string(dstFrag.Type)

	// We only handle Person<->Organization edges.
	var personFrag, orgFrag *hmodels.HorizonFragment
	if srcFrag.Type == hmodels.FragmentTypePerson && dstFrag.Type == hmodels.FragmentTypeOrganization {
		personFrag = srcFrag
		orgFrag = dstFrag
	} else if srcFrag.Type == hmodels.FragmentTypeOrganization && dstFrag.Type == hmodels.FragmentTypePerson {
		personFrag = dstFrag
		orgFrag = srcFrag
	} else {
		row.Reason = "not person<->org edge"
		return row
	}
	row.PersonFragmentID = strings.TrimSpace(personFrag.ID)
	row.OrgFragmentID = strings.TrimSpace(orgFrag.ID)
	row.PersonQID = extractFragmentWikidataID(personFrag)
	row.OrgQID = extractFragmentWikidataID(orgFrag)
	if row.PersonQID == "" || row.OrgQID == "" {
		row.Reason = "missing qid(s)"
		return row
	}

	matches, err := wdqs.matchPair(ctx, row.PersonQID, row.OrgQID)
	if err != nil {
		row.Reason = "wdqs error: " + err.Error()
		return row
	}
	row.Matches = matches
	if len(matches) == 0 {
		row.Reason = "no wikidata evidence for pair"
		return row
	}

	cands := scoreCandidates(matches)
	best, second := top2(cands)
	row.Scores = scoresToMap(cands)
	row.Candidates = candidatesToExplain(cands)
	if best != nil {
		row.Best = &ScoredCandidate{Relation: string(best.typ), Score: best.score}
		row.NewType = string(best.typ)
	}
	if second != nil {
		row.Second = &ScoredCandidate{Relation: string(second.typ), Score: second.score}
	}
	if best == nil {
		row.Reason = "no candidate produced"
		return row
	}
	margin := 999
	if second != nil {
		margin = best.score - second.score
	}
	row.Margin = margin
	if best.score < *scoreMin {
		row.Reason = fmt.Sprintf("best score below threshold: %d < %d", best.score, *scoreMin)
		return row
	}
	if margin < *marginMin {
		row.Reason = fmt.Sprintf("ambiguous: margin %d < %d", margin, *marginMin)
		return row
	}

	// Decide edge endpoints for new type.
	newType := best.typ
	newSourceID := strings.TrimSpace(edge.SourceFragmentID)
	newTargetID := strings.TrimSpace(edge.TargetFragmentID)
	newSourceType := srcFrag.Type
	newTargetType := dstFrag.Type

	// If relation expects PERSON as source, ensure direction.
	// We use the hardcoded direction policies from scout (same as pipeline).
	valid, shouldReverse, _ := scout.ValidateEdgeDirection(newType, newSourceType, newTargetType)
	if !valid && shouldReverse {
		newSourceID, newTargetID = newTargetID, newSourceID
		newSourceType, newTargetType = newTargetType, newSourceType
		row.WasReversed = true
	}

	// Additional safety: if still invalid, do not apply.
	valid2, _, _ := scout.ValidateEdgeDirection(newType, newSourceType, newTargetType)
	if !valid2 {
		row.Reason = "direction policy mismatch after normalization"
		return row
	}

	// If it already has desired type+direction, skip.
	if edge.Type == newType && strings.TrimSpace(edge.SourceFragmentID) == newSourceID && strings.TrimSpace(edge.TargetFragmentID) == newTargetID {
		row.Reason = "already classified"
		return row
	}

	row.Reason = "eligible"
	if !*apply {
		return row
	}

	// Apply update in-place: mutate edge type and (optional) swap endpoints.
	oldType := edge.Type
	edge.Type = newType
	edge.SourceFragmentID = newSourceID
	edge.TargetFragmentID = newTargetID
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers["reclassifiedFrom"] = string(oldType)
	edge.Qualifiers["reclassifiedAt"] = now
	edge.Qualifiers["reclassifiedBy"] = "tool:horizon_wikidata_reclassify_org_person_edges"
	edge.Qualifiers["wikidata"] = map[string]interface{}{
		"personQid": row.PersonQID,
		"orgQid":    row.OrgQID,
		"matches":   matches,
		"scores":    row.Scores,
		"candidates": row.Candidates,
		"best":      row.Best,
		"margin":    row.Margin,
		"scoreMin":  row.ScoreMin,
		"marginMin": row.MarginMin,
	}
	edge.Provenance.Method = "wikidata.relation_classifier.v1"
	edge.Provenance.SourceIDs = appendUnique(edge.Provenance.SourceIDs, "wikidata")
	edge.TenantID = tenant

	if err := edgesRepo.Update(ctx, edge); err != nil {
		row.Applied = false
		row.Reason = "apply failed: " + err.Error()
		return row
	}
	row.Applied = true
	row.Reason = "applied"
	return row
}

// scoreCandidates maps WDQS matches into Horizon DependencyType candidates.
//
// This is intentionally deterministic. It can be tuned by adjusting weights and/or
// adding new match keys (future: config-driven).
func scoreCandidates(matches []string) []candidate {
	has := map[string]bool{}
	for _, m := range matches {
		has[strings.TrimSpace(m)] = true
	}
	var out []candidate

	// EMPLOYED_BY (strong): person employer -> org
	if has[matchEmployedBy] {
		out = append(out, candidate{typ: hmodels.DependencyTypeEmployedBy, score: 185, why: []string{matchEmployedBy}})
	}
	// BOARD_MEMBER_OF (strong)
	if has[matchBoard] {
		out = append(out, candidate{typ: hmodels.DependencyTypeBoardMemberOf, score: 175, why: []string{matchBoard}})
	}
	// FOUNDED (strong)
	if has[matchFoundedBy] {
		out = append(out, candidate{typ: hmodels.DependencyTypeFounded, score: 170, why: []string{matchFoundedBy}})
	}
	// OWNS (strong) - org owned by person => person OWNS org
	if has[matchOwnedBy] {
		out = append(out, candidate{typ: hmodels.DependencyTypeOwns, score: 180, why: []string{matchOwnedBy}})
	}
	// MANAGES (strong-ish): CEO/director
	if has[matchCEO] || has[matchDirector] {
		score := 0
		var why []string
		if has[matchCEO] {
			score += 165
			why = append(why, matchCEO)
		}
		if has[matchDirector] {
			score += 120
			why = append(why, matchDirector)
		}
		// Damp double-counting (CEO+director likely the same high-level relation).
		if score > 190 {
			score = 190
		}
		out = append(out, candidate{typ: hmodels.DependencyTypeManages, score: score, why: why})
	}

	// Prefer deterministic order for stability.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].score > out[i].score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func scoresToMap(cands []candidate) map[string]int {
	if len(cands) == 0 {
		return nil
	}
	m := map[string]int{}
	for _, c := range cands {
		m[string(c.typ)] = c.score
	}
	return m
}

func candidatesToExplain(cands []candidate) []CandidateExplain {
	if len(cands) == 0 {
		return nil
	}
	out := make([]CandidateExplain, 0, len(cands))
	for _, c := range cands {
		out = append(out, CandidateExplain{
			Relation: string(c.typ),
			Score:    c.score,
			Why:      c.why,
		})
	}
	return out
}

func top2(cands []candidate) (best *candidate, second *candidate) {
	if len(cands) == 0 {
		return nil, nil
	}
	best = &cands[0]
	if len(cands) >= 2 {
		second = &cands[1]
	}
	return best, second
}

func parseEdgeTypes(s string) []hmodels.DependencyType {
	parts := strings.Split(s, ",")
	out := make([]hmodels.DependencyType, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, hmodels.DependencyType(p))
	}
	return out
}

func extractFragmentWikidataID(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	for _, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			if v := strings.TrimSpace(ref.ExternalID); strings.HasPrefix(v, "Q") {
				return v
			}
		}
	}
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["wikidataId"]); strings.HasPrefix(v, "Q") {
			return v
		}
		if v := strings.TrimSpace(f.Identifiers["wikidata"]); strings.HasPrefix(v, "Q") {
			return v
		}
	}
	return ""
}

func appendUnique(list []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return list
	}
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), s) {
			return list
		}
	}
	return append(list, s)
}

// =============================================================================
// Fragment cache (reduces Firestore reads)
// =============================================================================

type fragmentCache struct {
	repo *hrepo.HorizonFragmentsRepository
	mu   sync.RWMutex
	m    map[string]*hmodels.HorizonFragment
}

func newFragmentCache(repo *hrepo.HorizonFragmentsRepository) *fragmentCache {
	return &fragmentCache{repo: repo, m: map[string]*hmodels.HorizonFragment{}}
}

func (c *fragmentCache) get(ctx context.Context, id string) (*hmodels.HorizonFragment, error) {
	if c == nil || c.repo == nil {
		return nil, fmt.Errorf("fragment cache not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("empty id")
	}
	c.mu.RLock()
	if v, ok := c.m[id]; ok && v != nil {
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()
	f, err := c.repo.Get(ctx, id)
	if err != nil || f == nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[id] = f
	c.mu.Unlock()
	return f, nil
}

func (c *fragmentCache) size() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

// =============================================================================
// WDQS pair classifier (deterministic evidence via SPARQL)
// =============================================================================

type wdqsPairClassifier struct {
	httpClient *http.Client
	limiter    *rate.Limiter

	mu    sync.RWMutex
	cache map[string][]string // "personQID|orgQID" -> match keys
}

func newWDQSPairClassifier(minDelay time.Duration) *wdqsPairClassifier {
	if minDelay < 0 {
		minDelay = 0
	}
	lim := (*rate.Limiter)(nil)
	if minDelay > 0 {
		lim = rate.NewLimiter(rate.Every(minDelay), 1)
	}
	return &wdqsPairClassifier{
		httpClient: &http.Client{Timeout: 25 * time.Second},
		limiter:    lim,
		cache:      map[string][]string{},
	}
}

func (c *wdqsPairClassifier) cacheSize() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

func (c *wdqsPairClassifier) matchPair(ctx context.Context, personQID, orgQID string) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("wdqs classifier is nil")
	}
	personQID = strings.TrimSpace(personQID)
	orgQID = strings.TrimSpace(orgQID)
	if !strings.HasPrefix(personQID, "Q") || !strings.HasPrefix(orgQID, "Q") {
		return nil, fmt.Errorf("invalid qids: person=%q org=%q", personQID, orgQID)
	}
	key := personQID + "|" + orgQID

	c.mu.RLock()
	if v, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	if c.limiter != nil {
		_ = c.limiter.Wait(ctx)
	}
	matches, err := c.queryPair(ctx, personQID, orgQID)
	if err != nil {
		return nil, err
	}
	// stable order
	sortStrings(matches)
	c.mu.Lock()
	c.cache[key] = matches
	c.mu.Unlock()
	return matches, nil
}

func (c *wdqsPairClassifier) queryPair(ctx context.Context, personQID, orgQID string) ([]string, error) {
	// We intentionally keep the query small and bounded.
	// Using wdt: direct properties (truthy statements).
	//
	// Notes:
	// - Some relations (e.g., CEO) are often present as org->person (P169).
	// - We map them into Horizon types and then normalize direction via policies.
	sparql := fmt.Sprintf(`
SELECT DISTINCT ?match WHERE {
  VALUES (?match ?s ?p ?o) {
    ("%s" wd:%s wdt:P108 wd:%s)
    ("%s" wd:%s wdt:P169 wd:%s)
    ("%s" wd:%s wdt:P1037 wd:%s)
    ("%s" wd:%s wdt:P3320 wd:%s)
    ("%s" wd:%s wdt:P112 wd:%s)
    ("%s" wd:%s wdt:P127 wd:%s)
  }
  ?s ?p ?o .
}
`, matchEmployedBy, personQID, orgQID,
		matchCEO, orgQID, personQID,
		matchDirector, orgQID, personQID,
		matchBoard, orgQID, personQID,
		matchFoundedBy, orgQID, personQID,
		matchOwnedBy, orgQID, personQID,
	)

	endpoint := "https://query.wikidata.org/sparql"
	u, _ := url.Parse(endpoint)
	u.RawQuery = url.Values{
		"format": []string{"json"},
		"query":  []string{sparql},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "Sanskrite/1.0 (https://sanskrite.com)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wdqs query failed: status=%d", resp.StatusCode)
	}

	var parsed struct {
		Results struct {
			Bindings []struct {
				Match struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"match"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(parsed.Results.Bindings))
	for _, b := range parsed.Results.Bindings {
		v := strings.TrimSpace(b.Match.Value)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func sortStrings(a []string) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}


