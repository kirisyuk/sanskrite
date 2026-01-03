package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	shttp "github.com/igorkirisyuk/sanskrite/internal/http"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID          = flag.String("project", "", "GCP project ID (required)")
	tenantID           = flag.String("tenant", "global", "Tenant ID (default: global)")
	limit              = flag.Int("limit", 100, "Max number of missing-Wikidata fragments to print")
	maxScan            = flag.Int("max-scan", 50000, "Max fragments to scan before stopping (safety cap)")
	pageSize           = flag.Int("page-size", 500, "Firestore page size for scanning (pagination). Lower = safer, higher = fewer round-trips")
	startAfterDocID    = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	activeOnly         = flag.Bool("active-only", true, "Only scan governance.status==ACTIVE fragments")
	includeEntityCheck = flag.Bool("include-entity", true, "Also check linked ontology_entities by fragment.entityId (if present)")
	suggestWikidata    = flag.Bool("suggest-wikidata", false, "If set, try to find best Wikidata QID candidates for missing fragments (dry-run suggestions)")
	taxonomyVersion    = flag.String("taxonomy", "v1", "Horizon taxonomy version to use for industry canonicalization (default: v1)")
	applyIndustry      = flag.Bool("apply-industry", false, "Apply canonical industryPaths to fragment and linked entity (requires --suggest-wikidata)")
	industryScoreMin   = flag.Int("industry-score-min", 120, "Minimum suggestedScore required to apply industryPaths (safety threshold)")
	maxCandidates      = flag.Int("max-candidates", 5, "Max Wikidata candidates to consider per fragment (when --suggest-wikidata)")
	rateLimitMS        = flag.Int("rate-limit-ms", 120, "Delay between Wikidata API calls in milliseconds (when --suggest-wikidata)")
	format             = flag.String("format", "jsonl", "Output format: jsonl | tsv")
	outPath            = flag.String("out", "", "Optional output file path (default: stdout)")
)

type Row struct {
	FragmentID       string              `json:"fragmentId"`
	Type             hmodels.FragmentType `json:"type"`
	CanonicalPath    string              `json:"canonicalPath"`
	LabelEN          string              `json:"labelEn,omitempty"`
	LegalName        string              `json:"legalName,omitempty"`
	ShortName        string              `json:"shortName,omitempty"`
	GovernanceStatus string              `json:"governanceStatus,omitempty"`
	CreatedAt        time.Time           `json:"createdAt,omitempty"`
	UpdatedAt        time.Time           `json:"updatedAt,omitempty"`

	FragmentWikidataID string `json:"fragmentWikidataId,omitempty"`
	HasFragmentWikidata bool  `json:"hasFragmentWikidata"`

	EntityID           string `json:"entityId,omitempty"`
	EntityCanonicalName string `json:"entityCanonicalName,omitempty"`
	EntityType         string `json:"entityType,omitempty"`
	EntityWikidataID    string `json:"entityWikidataId,omitempty"`
	HasEntityWikidata   bool   `json:"hasEntityWikidata"`

	// Suggested Wikidata linkage (if enabled)
	SuggestedQuery     string `json:"suggestedQuery,omitempty"`
	SuggestedQID       string `json:"suggestedQid,omitempty"`
	SuggestedLabel     string `json:"suggestedLabel,omitempty"`
	SuggestedDesc      string `json:"suggestedDescription,omitempty"`
	SuggestedURL       string `json:"suggestedUrl,omitempty"`
	SuggestedType      string `json:"suggestedType,omitempty"`
	SuggestedTypeLabel string `json:"suggestedTypeLabel,omitempty"`
	SuggestedScore     int    `json:"suggestedScore,omitempty"`
	SuggestedAlt       []WDCandidate `json:"suggestedAlternatives,omitempty"`

	IndustryPaths []string `json:"industryPaths,omitempty"`

	Reason string `json:"reason"`
}

type WDCandidate struct {
	QID       string `json:"qid"`
	Label     string `json:"label"`
	Desc      string `json:"description,omitempty"`
	URL       string `json:"url,omitempty"`
	Type      string `json:"type,omitempty"`
	TypeLabel string `json:"typeLabel,omitempty"`
	Score     int    `json:"score"`
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
		*pageSize = 2000 // safety cap; keep Firestore pressure reasonable
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

	var entityRepo *coreRepo.EntityRepository
	entityCache := map[string]*coreModels.Entity{}
	if *includeEntityCheck {
		entityRepo = coreRepo.NewEntityRepository(db)
	}

	// Load taxonomy for industry canonicalization.
	taxRepo := coreRepo.NewHorizonTaxonomyRepository(db)
	tax, taxErr := taxRepo.Get(ctx, strings.TrimSpace(*taxonomyVersion))
	if taxErr != nil {
		log.Printf("WARNING: failed to load taxonomy %q: %v (industry canonicalization will be limited)", strings.TrimSpace(*taxonomyVersion), taxErr)
	}

	var wdClient *shttp.WikidataClient
	if *suggestWikidata {
		wdClient = shttp.GetWikidataClientFromEnv()
		if *maxCandidates <= 0 {
			*maxCandidates = 5
		}
		if *maxCandidates > 20 {
			*maxCandidates = 20 // safety
		}
		if *rateLimitMS < 0 {
			*rateLimitMS = 0
		}
	}

	log.Printf("=== Horizon: Find missing Wikidata (fragments) ===")
	log.Printf("project=%s tenant=%s limit=%d maxScan=%d activeOnly=%v includeEntity=%v format=%s",
		*projectID, *tenantID, *limit, *maxScan, *activeOnly, *includeEntityCheck, *format)
	if strings.TrimSpace(*startAfterDocID) != "" {
		log.Printf("resume cursor: start-after=%s", strings.TrimSpace(*startAfterDocID))
	}
	log.Printf("pagination: page-size=%d", *pageSize)

	rows, scanned, err := scan(ctx, db, entityRepo, entityCache, wdClient, tax)
	if err != nil {
		log.Fatalf("FATAL: scan failed: %v", err)
	}

	if strings.EqualFold(*format, "tsv") {
		writeTSV(out, rows)
	} else {
		writeJSONL(out, rows)
	}

	log.Printf("✓ Done. scanned=%d missingPrinted=%d (see log for resume cursor)", scanned, len(rows))
}

func scan(ctx context.Context, db *firestore.Client, entityRepo *coreRepo.EntityRepository, entityCache map[string]*coreModels.Entity, wdClient *shttp.WikidataClient, tax *coreModels.HorizonTaxonomy) ([]Row, int, error) {
	base := db.Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "in", []interface{}{string(hmodels.FragmentTypeOrganization), string(hmodels.FragmentTypePerson)}).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if *activeOnly {
		base = base.Where("governance.status", "==", string(hmodels.FragmentStatusActive))
	}

	var out []Row
	scanned := 0
	// Repos for applying changes (no GCS/Vertex configured here).
	fragmentsRepo := coreRepo2HorizonFragmentsRepo(db)

	cursor := strings.TrimSpace(*startAfterDocID)
	lastSeen := cursor
	for len(out) < *limit && scanned < *maxScan {
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
				return out, scanned, fmt.Errorf("failed to iterate fragments: %w", err)
			}
			pageCount++
			scanned++
			lastSeen = doc.Ref.ID
			if scanned > *maxScan {
				break
			}

			var f hmodels.HorizonFragment
			if err := doc.DataTo(&f); err != nil {
				continue
			}
			if strings.TrimSpace(f.ID) == "" {
				f.ID = doc.Ref.ID
			}

			fWid := extractFragmentWikidataID(&f)
			hasFragWid := fWid != ""

			// If fragment already has Wikidata, it's not part of this report.
			if hasFragWid {
				continue
			}

			row := Row{
				FragmentID:         f.ID,
				Type:               f.Type,
				CanonicalPath:      f.CanonicalPath,
				LabelEN:            strings.TrimSpace(f.Labels["en"]),
				LegalName:          strings.TrimSpace(f.Identifiers["legalName"]),
				ShortName:          strings.TrimSpace(f.Identifiers["shortName"]),
				GovernanceStatus:   strings.TrimSpace(f.Governance.Status),
				CreatedAt:          f.CreatedAt,
				UpdatedAt:          f.UpdatedAt,
				FragmentWikidataID: fWid,
				HasFragmentWikidata: hasFragWid,
				Reason:             "missing fragment wikidata id",
			}

			// Optional: check linked Entity as well.
			if entityRepo != nil && f.EntityID != nil && strings.TrimSpace(*f.EntityID) != "" {
				eid := strings.TrimSpace(*f.EntityID)
				row.EntityID = eid
				ent := entityCache[eid]
				if ent == nil {
					e, err := entityRepo.Get(ctx, eid)
					if err == nil && e != nil {
						ent = e
						entityCache[eid] = e
					}
				}
				if ent != nil {
					row.EntityCanonicalName = strings.TrimSpace(ent.CanonicalName)
					row.EntityType = string(ent.Type)
					eWid := extractEntityWikidataID(ent)
					row.EntityWikidataID = eWid
					row.HasEntityWikidata = eWid != ""
					if row.HasEntityWikidata {
						row.Reason = "missing fragment wikidata id (entity HAS wikidata)"
					} else {
						row.Reason = "missing fragment wikidata id (entity missing/has no wikidata)"
					}
				}
			}

			// Optional: suggest best Wikidata QID for this fragment (dry-run).
			if wdClient != nil {
				best, alts, query := suggestWikidataForFragment(ctx, wdClient, &f, *maxCandidates, time.Duration(*rateLimitMS)*time.Millisecond)
				row.SuggestedQuery = query
				row.SuggestedAlt = alts
				if best != nil {
					row.SuggestedQID = best.QID
					row.SuggestedLabel = best.Label
					row.SuggestedDesc = best.Desc
					row.SuggestedURL = best.URL
					row.SuggestedType = best.Type
					row.SuggestedTypeLabel = best.TypeLabel
					row.SuggestedScore = best.Score
				}

				// Industry extraction/canonicalization from Wikidata (P452) using taxonomy dictionary.
				// NOTE/TODO (core): This enrichment should move into backend core pipeline (scout/precreate or entity enrichment),
				// so new fragments are created with industryPaths automatically.
				if row.SuggestedQID != "" {
					ed, err := wdClient.GetEntityData(ctx, row.SuggestedQID)
					if err == nil && ed != nil {
						paths := canonicalizeIndustryPathsFromWikidata(ctx, wdClient, ed, tax, time.Duration(*rateLimitMS)*time.Millisecond)
						row.IndustryPaths = paths
						// Optionally apply (write) industry paths to fragment and linked entity.
						if *applyIndustry && row.SuggestedScore >= *industryScoreMin && len(paths) > 0 {
							applyErr := applyIndustryPaths(ctx, fragmentsRepo, entityRepo, &f, paths)
							if applyErr != nil {
								log.Printf("WARNING: failed to apply industryPaths for %s (%s): %v", f.ID, f.CanonicalPath, applyErr)
							}
						}
					}
				}
			}

			out = append(out, row)
			if len(out) >= *limit {
				break
			}
		}
		iter.Stop()

		// If the page returned fewer docs than pageSize, we've reached the end.
		if pageCount < *pageSize {
			break
		}
	}

	if lastSeen != "" {
		log.Printf("resume cursor: start-after=%s", lastSeen)
	}
	return out, scanned, nil
}

// coreRepo2HorizonFragmentsRepo creates a horizon fragments repo without GCS/Vertex side effects.
// We keep this local to avoid importing backend/internal/horizon/repo as "repo" (name collision).
func coreRepo2HorizonFragmentsRepo(db *firestore.Client) *horizonFragmentsRepoShim {
	return &horizonFragmentsRepoShim{db: db}
}

// horizonFragmentsRepoShim is a minimal updater for horizon_fragments that:
// - reads the fragment
// - updates industryPaths
// - rebuilds indexPaths deterministically via models.BuildIndexPaths (triggered by Validate on Set is not guaranteed)
// We intentionally do NOT bump fragment.Version here (best-effort, lightweight backfill).
// TODO (core): move to a proper repo.Update call if we want versioned writes.
type horizonFragmentsRepoShim struct{ db *firestore.Client }

func (r *horizonFragmentsRepoShim) UpdateIndustryPaths(ctx context.Context, id string, paths []string) error {
	if r == nil || r.db == nil || strings.TrimSpace(id) == "" {
		return fmt.Errorf("invalid repo/id")
	}
	docRef := r.db.Collection("horizon_fragments").Doc(id)
	snap, err := docRef.Get(ctx)
	if err != nil {
		return err
	}
	var frag hmodels.HorizonFragment
	if err := snap.DataTo(&frag); err != nil {
		return err
	}
	frag.IndustryPaths = paths
	frag.IndexPaths = hmodels.BuildIndexPaths(&frag, nil)
	now := time.Now().UTC()
	_, err = docRef.Update(ctx, []firestore.Update{
		{Path: "industryPaths", Value: paths},
		{Path: "indexPaths", Value: frag.IndexPaths},
		{Path: "updatedAt", Value: now},
		{Path: "metadata.lastUpdatedAt", Value: now},
	})
	return err
}

func applyIndustryPaths(ctx context.Context, fragRepo *horizonFragmentsRepoShim, entityRepo *coreRepo.EntityRepository, f *hmodels.HorizonFragment, paths []string) error {
	// Persist to fragment.
	if fragRepo != nil && f != nil && strings.TrimSpace(f.ID) != "" {
		if err := fragRepo.UpdateIndustryPaths(ctx, f.ID, paths); err != nil {
			return err
		}
	}
	// Persist to linked entity (if configured).
	if entityRepo != nil && f != nil && f.EntityID != nil && strings.TrimSpace(*f.EntityID) != "" {
		eid := strings.TrimSpace(*f.EntityID)
		ent, err := entityRepo.Get(ctx, eid)
		if err != nil || ent == nil {
			return err
		}
		ent.IndustryPaths = paths
		return entityRepo.Upsert(ctx, ent)
	}
	return nil
}

func canonicalizeIndustryPathsFromWikidata(ctx context.Context, wdClient *shttp.WikidataClient, entityData *shttp.WikidataEntityData, tax *coreModels.HorizonTaxonomy, delay time.Duration) []string {
	if entityData == nil {
		return nil
	}
	industryQIDs := extractWikidataQIDList(entityData.PropertiesRaw, "P452")
	if len(industryQIDs) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var out []string

	for _, qid := range industryQIDs {
		qid = strings.TrimSpace(qid)
		if qid == "" {
			continue
		}
		// 1) Strict QID mapping if available
		if tax != nil && tax.IndustryWikidataMap != nil {
			if p := strings.TrimSpace(tax.IndustryWikidataMap[qid]); strings.HasPrefix(p, "/industry/") {
				if _, ok := seen[p]; !ok {
					seen[p] = struct{}{}
					out = append(out, p)
				}
				continue
			}
		}

		// 2) Label-based mapping via synonyms (requires fetching industry item label)
		if wdClient != nil {
			if delay > 0 {
				time.Sleep(delay)
			}
			indData, err := wdClient.GetEntityData(ctx, qid)
			if err == nil && indData != nil {
				lbl := strings.TrimSpace(indData.Label)
				key := normalizeIndustryKey(lbl)
				if tax != nil && tax.IndustrySynonyms != nil {
					if p := strings.TrimSpace(tax.IndustrySynonyms[key]); strings.HasPrefix(p, "/industry/") {
						if _, ok := seen[p]; !ok {
							seen[p] = struct{}{}
							out = append(out, p)
						}
						continue
					}
				}
				// 3) Fallback: strict but generic
				if lbl != "" {
					p := "/industry/other/" + slugify(lbl)
					if _, ok := seen[p]; !ok {
						seen[p] = struct{}{}
						out = append(out, p)
					}
				}
			}
		}
	}

	// Stable order
	sortStrings(out)
	return out
}

func extractWikidataQIDList(props map[string]interface{}, propID string) []string {
	if props == nil {
		return nil
	}
	v, ok := props[propID]
	if !ok || v == nil {
		return nil
	}
	var out []string
	switch vv := v.(type) {
	case []interface{}:
		for _, it := range vv {
			if s, ok := it.(string); ok && strings.HasPrefix(strings.TrimSpace(s), "Q") {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		if strings.HasPrefix(strings.TrimSpace(vv), "Q") {
			out = append(out, strings.TrimSpace(vv))
		}
	}
	return out
}

func normalizeIndustryKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "&", "and")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Minimal slugifier: keep [a-z0-9-]
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func sortStrings(a []string) {
	// simple insertion sort (small arrays)
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func extractFragmentWikidataID(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	// Prefer KBRefs (source of truth if present)
	for _, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			if v := strings.TrimSpace(ref.ExternalID); v != "" {
				return v
			}
		}
	}
	// Fallback: identifiers (legacy)
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["wikidataId"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(f.Identifiers["wikidata"]); v != "" {
			return v
		}
	}
	return ""
}

func extractEntityWikidataID(e *coreModels.Entity) string {
	if e == nil {
		return ""
	}
	for _, ref := range e.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			if v := strings.TrimSpace(ref.ExternalID); v != "" {
				return v
			}
		}
	}
	// Some pipelines may also store identifiers; keep as fallback.
	if e.Identifiers != nil {
		if v := strings.TrimSpace(e.Identifiers["wikidataId"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(e.Identifiers["wikidata"]); v != "" {
			return v
		}
	}
	return ""
}

func suggestWikidataForFragment(
	ctx context.Context,
	client *shttp.WikidataClient,
	f *hmodels.HorizonFragment,
	maxCandidates int,
	delay time.Duration,
) (*WDCandidate, []WDCandidate, string) {
	if client == nil || f == nil {
		return nil, nil, ""
	}
	queries := pickSearchQueriesForFragment(f)
	if len(queries) == 0 {
		return nil, nil, ""
	}

	all := make([]WDCandidate, 0, maxCandidates*len(queries))
	bestGlobalScore := -1
	var bestGlobal *WDCandidate
	bestQuery := queries[0]

	for qi, query := range queries {
		resp, err := client.Search(ctx, query, maxCandidates)
		if err != nil || resp == nil || len(resp.Results) == 0 {
			continue
		}

		for i, r := range resp.Results {
			if (qi > 0 || i > 0) && delay > 0 {
				time.Sleep(delay)
			}
			qid := strings.TrimSpace(r.ID)
			if qid == "" {
				continue
			}
			ed, err := client.GetEntityData(ctx, qid)
			// If entity fetch fails, still keep lightweight candidate.
			label := strings.TrimSpace(r.Label)
			desc := strings.TrimSpace(r.Description)
			url := strings.TrimSpace(r.URL)
			typ := ""
			typLabel := ""
			if err == nil && ed != nil {
				if label == "" {
					label = strings.TrimSpace(ed.Label)
				}
				if desc == "" {
					desc = strings.TrimSpace(ed.Description)
				}
				if url == "" {
					url = strings.TrimSpace(ed.URL)
				}
				typ = strings.TrimSpace(ed.Type)
				typLabel = strings.TrimSpace(ed.TypeLabel)
			}

			score := scoreCandidate(f, query, label, desc, typ, typLabel, ed)
			// Slight penalty for fallback queries (later attempts)
			score -= qi * 3

			c := WDCandidate{
				QID:       qid,
				Label:     label,
				Desc:      desc,
				URL:       url,
				Type:      typ,
				TypeLabel: typLabel,
				Score:     score,
			}
			all = append(all, c)

			if score > bestGlobalScore {
				bestGlobalScore = score
				tmp := c
				bestGlobal = &tmp
				bestQuery = query
			}
		}
	}

	// Sort all by score descending and keep top maxCandidates as "alternatives"
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Score > all[i].Score {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) > maxCandidates {
		all = all[:maxCandidates]
	}

	return bestGlobal, all, bestQuery
}

func pickSearchQueriesForFragment(f *hmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	// Prefer legalName for orgs; label for persons; fall back to shortName.
	legal := strings.TrimSpace(f.Identifiers["legalName"])
	label := strings.TrimSpace(f.Labels["en"])
	short := strings.TrimSpace(f.Identifiers["shortName"])

	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
	}

	// Primary queries
	if f.Type == hmodels.FragmentTypePerson {
		add(label)
		add(legal)
		add(short)
	} else {
		add(legal)
		add(label)
		add(short)
	}

	// Fallback: normalized legal name (helps when suffixes hurt search)
	if legal != "" {
		add(scout.NormalizeLegalName(legal))
	}
	if label != "" {
		add(scout.NormalizeLegalName(label))
	}

	// Fallback: unslugify canonicalPath last segment (very rough)
	if cp := strings.TrimSpace(f.CanonicalPath); strings.HasPrefix(cp, "/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 {
			last := parts[len(parts)-1]
			last = strings.ReplaceAll(last, "-", " ")
			last = strings.ReplaceAll(last, "_", " ")
			add(last)
		}
	}

	out := make([]string, 0, len(seen))
	// Preserve insertion order roughly by re-adding in same order.
	order := []string{
		label, legal, short,
		scout.NormalizeLegalName(legal), scout.NormalizeLegalName(label),
	}
	for _, s := range order {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			out = append(out, s)
			delete(seen, s)
		}
	}
	for s := range seen {
		out = append(out, s)
	}
	return out
}

func scoreCandidate(
	f *hmodels.HorizonFragment,
	query string,
	candLabel string,
	candDesc string,
	candType string,
	candTypeLabel string,
	entityData *shttp.WikidataEntityData,
) int {
	score := 0
	qn := normalizeNameForMatch(query)
	ln := normalizeNameForMatch(candLabel)

	if ln != "" && qn != "" {
		if ln == qn {
			score += 80
		} else if strings.Contains(ln, qn) || strings.Contains(qn, ln) {
			score += 55
		}
	}

	// Organization-specific: normalize legal suffixes to reduce mismatch (e.g., "Group Corp." vs "group").
	if f != nil && f.Type == hmodels.FragmentTypeOrganization {
		q2 := scout.NormalizeLegalName(query)
		l2 := scout.NormalizeLegalName(candLabel)
		if q2 != "" && l2 != "" {
			if q2 == l2 {
				score += 35
			}
		}
	}

	// Type gating: PERSON must be human; ORG must NOT be human.
	isHuman := wikidataLooksHuman(candType, entityData)
	if f != nil && f.Type == hmodels.FragmentTypePerson {
		if isHuman {
			score += 40
		} else {
			score -= 80 // strong reject
		}
	}
	if f != nil && f.Type == hmodels.FragmentTypeOrganization {
		if isHuman {
			score -= 60
		} else {
			// Prefer org-like instance-of types.
			if tid := strings.TrimSpace(candType); tid != "" {
				if isOrgTypeBoost(tid) {
					score += 15
				}
				if isOrgTypePenalty(tid) {
					score -= 15
				}
			}
			// Slight boost if description/typeLabel smells like org/company
			txt := strings.ToLower(strings.TrimSpace(candTypeLabel + " " + candDesc))
			if strings.Contains(txt, "company") || strings.Contains(txt, "organization") || strings.Contains(txt, "bank") || strings.Contains(txt, "airline") || strings.Contains(txt, "government") {
				score += 10
			}
		}
	}

	return score
}

func isOrgTypeBoost(qid string) bool {
	switch qid {
	case
		"Q43229",   // organization
		"Q783794",  // company
		"Q4830453", // business
		"Q6881511", // enterprise
		"Q215380":  // musical group (often modeled as org-like in our graph)
		return true
	default:
		return false
	}
}

func isOrgTypePenalty(qid string) bool {
	switch qid {
	case
		"Q482994",  // album
		"Q7366",    // song
		"Q571":     // book
		return true
	default:
		return false
	}
}
func wikidataLooksHuman(candType string, entityData *shttp.WikidataEntityData) bool {
	if strings.TrimSpace(candType) == "Q5" {
		return true
	}
	// Best-effort: check P31 list (PropertiesRaw["P31"]) if present.
	if entityData != nil && entityData.PropertiesRaw != nil {
		if p31, ok := entityData.PropertiesRaw["P31"]; ok {
			switch v := p31.(type) {
			case []interface{}:
				for _, it := range v {
					if s, ok := it.(string); ok && strings.TrimSpace(s) == "Q5" {
						return true
					}
				}
			case string:
				return strings.TrimSpace(v) == "Q5"
			}
		}
	}
	return false
}

func normalizeNameForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "&", "and")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func writeJSONL(out *os.File, rows []Row) {
	enc := json.NewEncoder(out)
	for _, r := range rows {
		_ = enc.Encode(r)
	}
}

func writeTSV(out *os.File, rows []Row) {
	fmt.Fprintln(out, strings.Join([]string{
		"fragmentId",
		"type",
		"canonicalPath",
		"labelEn",
		"legalName",
		"shortName",
		"governanceStatus",
		"entityId",
		"entityType",
		"entityCanonicalName",
		"fragmentWikidataId",
		"entityWikidataId",
		"reason",
	}, "\t"))
	for _, r := range rows {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			escapeTSV(r.FragmentID),
			escapeTSV(string(r.Type)),
			escapeTSV(r.CanonicalPath),
			escapeTSV(r.LabelEN),
			escapeTSV(r.LegalName),
			escapeTSV(r.ShortName),
			escapeTSV(r.GovernanceStatus),
			escapeTSV(r.EntityID),
			escapeTSV(r.EntityType),
			escapeTSV(r.EntityCanonicalName),
			escapeTSV(r.FragmentWikidataID),
			escapeTSV(r.EntityWikidataID),
			escapeTSV(r.Reason),
		)
	}
}

func escapeTSV(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}


