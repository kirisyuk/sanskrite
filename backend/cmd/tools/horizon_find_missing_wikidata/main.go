package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	shttp "github.com/igorkirisyuk/sanskrite/internal/http"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	includeWithWikidata = flag.Bool("include-with-wikidata", false, "Also include fragments that already have a Wikidata ID (useful for sampling P452 industries)")
	onlyWithWikidata    = flag.Bool("only-with-wikidata", false, "Only include fragments that already have a Wikidata ID (skips missing-wikidata fragments)")
	expandIndustryDict  = flag.Bool("expand-industry-dict", false, "Build/extend industrySynonyms + industryWikidataMap in horizon_taxonomy based on observed P452 from scanned fragments")
	applyIndustryDict   = flag.Bool("apply-industry-dict", false, "If set with --expand-industry-dict, upsert merged industrySynonyms/industryWikidataMap back into horizon_taxonomy")
	industryDictMax     = flag.Int("industry-dict-max", 500, "Max number of new industry QIDs to add into industryWikidataMap per run (safety cap)")
	applyWikidataRef    = flag.Bool("apply-wikidata-ref", false, "Write Wikidata KBRef (and mirror identifiers.wikidataId) to fragment + linked entity when QID is confident")
	applyWikidataNames  = flag.Bool("apply-wikidata-names", false, "Overwrite fragment/entity names from Wikidata (label/description, plus official name if present) when QID is confident")
	wikidataScoreMin    = flag.Int("wikidata-score-min", 120, "Minimum suggestedScore required to apply Wikidata enrichment (safety threshold)")
	progressEvery       = flag.Int("progress-every", 50, "Print progress logs every N selected rows (0 disables)")
	progressScannedEvery = flag.Int("progress-scanned-every", 2000, "Print progress logs every N scanned docs even if nothing is selected (0 disables)")
	heartbeatSec        = flag.Int("heartbeat-sec", 10, "Emit heartbeat logs every N seconds while running (0 disables)")
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

	// Wikidata name enrichment preview (what we would apply / did apply)
	WikidataLabelEN       string `json:"wikidataLabelEn,omitempty"`
	WikidataDescriptionEN string `json:"wikidataDescriptionEn,omitempty"`
	WikidataOfficialName  string `json:"wikidataOfficialName,omitempty"`
	WikidataShortName     string `json:"wikidataShortName,omitempty"`

	AppliedWikidata bool `json:"appliedWikidata,omitempty"`

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
	if *progressEvery < 0 {
		*progressEvery = 0
	}
	if *progressScannedEvery < 0 {
		*progressScannedEvery = 0
	}
	if *heartbeatSec < 0 {
		*heartbeatSec = 0
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

	// Load runtime taxonomy cache as well (so core enrichment uses the same dictionary as backend).
	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(db, strings.TrimSpace(*taxonomyVersion), 10*time.Minute)
	if err := taxonomy.RefreshRuntimeTaxonomy(ctx); err != nil {
		log.Printf("WARNING: failed to refresh runtime taxonomy: %v", err)
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

	rows, scanned, lastSeen, err := scan(ctx, db, entityRepo, entityCache, wdClient, tax)
	if err != nil {
		// IMPORTANT: log.Fatalf exits immediately (no defers), so we must print resume cursor here.
		if strings.TrimSpace(lastSeen) != "" {
			log.Printf("resume cursor: start-after=%s", strings.TrimSpace(lastSeen))
		}
		log.Fatalf("FATAL: scan failed: %v", err)
	}

	if strings.EqualFold(*format, "tsv") {
		writeTSV(out, rows)
	} else {
		writeJSONL(out, rows)
	}

	if strings.TrimSpace(lastSeen) != "" {
		log.Printf("resume cursor: start-after=%s", strings.TrimSpace(lastSeen))
	}
	log.Printf("✓ Done. scanned=%d missingPrinted=%d (see log for resume cursor)", scanned, len(rows))
}

func scan(ctx context.Context, db *firestore.Client, entityRepo *coreRepo.EntityRepository, entityCache map[string]*coreModels.Entity, wdClient *shttp.WikidataClient, tax *coreModels.HorizonTaxonomy) ([]Row, int, string, error) {
	base := db.Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "in", []interface{}{string(hmodels.FragmentTypeOrganization), string(hmodels.FragmentTypePerson)}).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if *activeOnly {
		base = base.Where("governance.status", "==", string(hmodels.FragmentStatusActive))
	}

	var out []Row
	scanned := 0
	// Repos for applying changes.
	// Use the real HorizonFragmentsRepository so updates are validated, versioned, and indexPaths are rebuilt consistently.
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	taxRepo := coreRepo.NewHorizonTaxonomyRepository(db)

	// Caches to reduce Wikidata pressure (esp. when scanning 1000+)
	subjectCache := map[string]*shttp.WikidataEntityData{}
	industryCache := map[string]*shttp.WikidataEntityData{}

	// Optional: accumulate a dict expansion (qid->path and label->path) from observed P452 industries.
	dict := newIndustryDictAccumulator(tax)

	// Progress counters (to make long scans feel alive)
	selected := 0
	subjectFetches := 0
	industryItemFetches := 0

	// Heartbeat counters (to show activity even if we're blocked on network).
	var scannedAtomic int64
	var selectedAtomic int64
	var subjectFetchesAtomic int64
	var industryItemFetchesAtomic int64

	stopHeartbeat := make(chan struct{})
	if *heartbeatSec > 0 {
		go func() {
			t := time.NewTicker(time.Duration(*heartbeatSec) * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					log.Printf("heartbeat: scanned=%d selected=%d subjectCache=%d industryCache=%d observedIndustryQIDs=%d subjectFetches=%d industryItemFetches=%d",
						atomic.LoadInt64(&scannedAtomic),
						atomic.LoadInt64(&selectedAtomic),
						len(subjectCache),
						len(industryCache),
						dict.ObservedQIDs(),
						atomic.LoadInt64(&subjectFetchesAtomic),
						atomic.LoadInt64(&industryItemFetchesAtomic),
					)
				case <-stopHeartbeat:
					return
				}
			}
		}()
	}
	defer close(stopHeartbeat)

	cursor := strings.TrimSpace(*startAfterDocID)
	lastSeen := cursor
	transientErrs := 0
	const maxTransientErrs = 20
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
				// Transient Firestore/network errors are common on long scans. Retry by restarting
				// pagination from the last seen document ID, with exponential backoff.
				if isTransientFirestoreErr(err) && transientErrs < maxTransientErrs {
					transientErrs++
					backoff := time.Duration(1<<minInt(transientErrs, 5)) * time.Second // 2s,4s,8s,16s,32s cap
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					log.Printf("WARNING: transient Firestore error while scanning (retry %d/%d in %s): %v (resume cursor: start-after=%s)",
						transientErrs, maxTransientErrs, backoff, err, lastSeen)
					time.Sleep(backoff)
					// Force "continue scan" semantics even if we read < pageSize.
					pageCount = *pageSize
					break
				}
				return out, scanned, lastSeen, fmt.Errorf("failed to iterate fragments: %w", err)
			}
			pageCount++
			scanned++
			atomic.StoreInt64(&scannedAtomic, int64(scanned))
			lastSeen = doc.Ref.ID
			if scanned > *maxScan {
				break
			}
			if *progressScannedEvery > 0 && scanned%*progressScannedEvery == 0 {
				log.Printf("progress(scan): scanned=%d selected=%d lastDoc=%s subjectCache=%d industryCache=%d observedIndustryQIDs=%d subjectFetches=%d industryItemFetches=%d",
					scanned, selected, lastSeen, len(subjectCache), len(industryCache), dict.ObservedQIDs(), subjectFetches, industryItemFetches)
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
				FragmentWikidataID:  fWid,
				HasFragmentWikidata: hasFragWid,
				Reason:              "missing fragment wikidata id",
			}
			if hasFragWid {
				row.Reason = "has fragment wikidata id (included for sampling)"
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

			// Filters that depend on entity wikidata must run AFTER entity lookup.
			hasAnyWid := hasFragWid || row.HasEntityWikidata
			if *onlyWithWikidata && !hasAnyWid {
				continue
			}
			// If fragment already has Wikidata, it's not part of this report (unless explicitly included for sampling).
			if hasFragWid && !*includeWithWikidata {
				continue
			}

			// Optional: suggest best Wikidata QID for this fragment (dry-run).
			if wdClient != nil {
				// If fragment already has Wikidata and we included it for sampling, don't call search — use the existing QID.
				if hasFragWid {
					row.SuggestedQID = strings.TrimSpace(fWid)
					row.SuggestedQuery = "existing-fragment-wikidata"
					row.SuggestedScore = 999 // treat as high-confidence for sampling/apply
				} else if row.HasEntityWikidata && strings.TrimSpace(row.EntityWikidataID) != "" {
					// If linked entity has Wikidata, prefer it over search.
					row.SuggestedQID = strings.TrimSpace(row.EntityWikidataID)
					row.SuggestedQuery = "existing-entity-wikidata"
					row.SuggestedScore = 999
				} else {
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
				}

				// Industry extraction/canonicalization from Wikidata (P452) using taxonomy dictionary.
				// NOTE/TODO (core): This enrichment should move into backend core pipeline (scout/precreate or entity enrichment),
				// so new fragments are created with industryPaths automatically.
				subjectQID := strings.TrimSpace(row.SuggestedQID)
				if subjectQID == "" && hasFragWid {
					subjectQID = strings.TrimSpace(fWid)
				}
				if subjectQID == "" && row.HasEntityWikidata {
					subjectQID = strings.TrimSpace(row.EntityWikidataID)
				}
				if subjectQID != "" {
					ed := subjectCache[subjectQID]
					if ed == nil {
						if d := time.Duration(*rateLimitMS) * time.Millisecond; d > 0 {
							time.Sleep(d)
						}
						got, err := wdClient.GetEntityData(ctx, subjectQID)
						if err == nil && got != nil {
							ed = got
							subjectCache[subjectQID] = got
							subjectFetches++
							atomic.StoreInt64(&subjectFetchesAtomic, int64(subjectFetches))
						}
					}
					if ed != nil {
						beforeCache := 0
						if industryCache != nil {
							beforeCache = len(industryCache)
						}
						_, evid := canonicalizeIndustryPathsFromWikidata(ctx, wdClient, ed, tax, industryCache, time.Duration(*rateLimitMS)*time.Millisecond)
						if industryCache != nil && len(industryCache) > beforeCache {
							industryItemFetches += (len(industryCache) - beforeCache)
							atomic.StoreInt64(&industryItemFetchesAtomic, int64(industryItemFetches))
						}
						// Preview industryPaths exactly as core would apply them (conservative: QID->path mapping only).
						row.IndustryPaths = taxonomy.MapIndustryQIDsToPaths(extractWikidataQIDList(ed.PropertiesRaw, "P452"))
						// Accumulate industry dict candidates from evidence (P452).
						if dict != nil && len(evid) > 0 {
							dict.Observe(evid)
						}

						// Wikidata name enrichment preview (aggressive overwrite is allowed per user: nothing is curated manually).
						row.WikidataLabelEN = strings.TrimSpace(ed.Label)
						row.WikidataDescriptionEN = strings.TrimSpace(ed.Description)
						official, short := extractPreferredWikidataNames(ed)
						row.WikidataOfficialName = official
						row.WikidataShortName = short

						// Optionally apply (write) enrichment to fragment + linked entity, mirroring core behavior.
						doApplyIndustry := *applyIndustry && row.SuggestedScore >= *industryScoreMin
						doApplyRef := *applyWikidataRef && row.SuggestedScore >= *wikidataScoreMin
						doApplyNames := *applyWikidataNames && row.SuggestedScore >= *wikidataScoreMin
						if (doApplyIndustry || doApplyRef || doApplyNames) && ed != nil {
							applyErr := applyWikidataConservativeToFragmentAndEntity(ctx, fragmentsRepo, entityRepo, &f, ed, doApplyIndustry, doApplyRef, doApplyNames)
							if applyErr != nil {
								log.Printf("WARNING: failed to apply wikidata enrichment for %s (%s): %v", f.ID, f.CanonicalPath, applyErr)
							} else {
								row.AppliedWikidata = true
							}
						}
					}
				}
			}

			out = append(out, row)
			selected++
			atomic.StoreInt64(&selectedAtomic, int64(selected))
			if *progressEvery > 0 && selected%*progressEvery == 0 {
				log.Printf("progress: scanned=%d selected=%d lastDoc=%s subjectCache=%d industryCache=%d observedIndustryQIDs=%d subjectFetches=%d industryItemFetches=%d",
					scanned, selected, lastSeen, len(subjectCache), len(industryCache), dict.ObservedQIDs(), subjectFetches, industryItemFetches)
			}
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

	// After scan: optionally merge observed industries into taxonomy doc.
	if dict != nil && *expandIndustryDict {
		if tax == nil {
			log.Printf("WARNING: taxonomy not loaded; cannot expand industry dict")
		} else {
			addedQIDs, addedSyn, updatedAllowed := dict.MergeIntoTaxonomy(tax, *industryDictMax)
			log.Printf("industry-dict: observedQIDs=%d newQIDsAdded=%d newSynonymsAdded=%d newAllowedPathsAdded=%d",
				dict.ObservedQIDs(), addedQIDs, addedSyn, updatedAllowed)
			if *applyIndustryDict {
				if err := taxRepo.Upsert(ctx, tax); err != nil {
					log.Printf("WARNING: failed to upsert taxonomy: %v", err)
				} else {
					log.Printf("industry-dict: taxonomy upserted: version=%s", tax.Version)
				}
			}
		}
	}

	return out, scanned, lastSeen, nil
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
	// Fallback for wrapped/transport errors (common on Windows).
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "wsarecv") ||
		strings.Contains(s, "forcibly closed") ||
		strings.Contains(s, "connection was forcibly closed") ||
		strings.Contains(s, "connection attempt failed") ||
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

func applyWikidataConservativeToFragmentAndEntity(
	ctx context.Context,
	fragRepo *hrepo.HorizonFragmentsRepository,
	entityRepo *coreRepo.EntityRepository,
	f *hmodels.HorizonFragment,
	wdData *shttp.WikidataEntityData,
	applyIndustry bool,
	applyRef bool,
	applyNames bool,
) error {
	if f == nil || strings.TrimSpace(f.ID) == "" || wdData == nil {
		return fmt.Errorf("invalid fragment or wikidata data")
	}

	// 1) Apply to fragment using shared core logic (single source of truth).
	// Note: If applyRef is requested, ensure identifiers/KBRefs contain Wikidata ID first (core logic expects QID-known).
	if applyRef {
		if f.Identifiers == nil {
			f.Identifiers = map[string]string{}
		}
		if strings.TrimSpace(f.Identifiers["wikidataId"]) == "" {
			f.Identifiers["wikidataId"] = strings.TrimSpace(wdData.ID)
		}
		// Also add KBRef (best-effort); Validate() will mirror anyway.
		has := false
		for _, r := range f.KBRefs {
			if strings.EqualFold(strings.TrimSpace(r.KB), "Wikidata") {
				has = true
				break
			}
		}
		if !has && strings.TrimSpace(wdData.ID) != "" {
			f.KBRefs = append(f.KBRefs, hmodels.KBReference{KB: "Wikidata", ExternalID: strings.TrimSpace(wdData.ID), Confidence: 1.0})
		}
	}

	// Apply names/industry deterministically from WD data.
	opts := henrich.ConservativeWikidataOptions{Force: true, Overwrite: true}
	if !applyNames {
		// If names aren't requested, avoid overwriting them. Industry can still be applied.
		opts.Overwrite = false
	}
	_, _ = henrich.EnrichFragmentFromEntityData(f, convertWD(wdData), time.Now().UTC(), opts)
	if !applyIndustry {
		// Keep current industryPaths if not requested (core enrichment may have populated them).
		// Note: we only clear if enrichment changed them; easiest safe behavior is to re-load and restore.
		// Since applyIndustry=false, we just do not persist IndustryPaths changes.
		// We'll restore persisted state by reloading before write.
		cur, err := fragRepo.Get(ctx, strings.TrimSpace(f.ID))
		if err == nil && cur != nil {
			f.IndustryPaths = cur.IndustryPaths
		}
	}
	if err := fragRepo.Update(ctx, f); err != nil {
		return err
	}

	// 2) Apply to linked entity (best-effort).
	if entityRepo != nil && f.EntityID != nil && strings.TrimSpace(*f.EntityID) != "" {
		eid := strings.TrimSpace(*f.EntityID)
		ent, err := entityRepo.Get(ctx, eid)
		if err != nil || ent == nil {
			return err
		}
		if applyRef {
			// Mirror Wikidata KBRef to entity.
			upsertEntityKBRef(ent, strings.TrimSpace(wdData.ID))
		}
		if applyNames {
			// Aggressive overwrite (per requirement).
			if ent.Labels == nil {
				ent.Labels = map[string]string{}
			}
			if s := strings.TrimSpace(wdData.Label); s != "" {
				ent.Labels["en"] = s
				ent.CanonicalName = s
				if len(ent.Aliases) == 0 || ent.Aliases[0] != s {
					ent.Aliases = append([]string{s}, ent.Aliases...)
				}
			}
			if s := strings.TrimSpace(wdData.Description); s != "" {
				ent.Description = s
			}
			// Prefer official/short names if present in WD properties.
			off, sh := extractPreferredWikidataNames(wdData)
			if strings.TrimSpace(off) != "" {
				if ent.Identifiers == nil {
					ent.Identifiers = map[string]string{}
				}
				ent.Identifiers["legalName"] = strings.TrimSpace(off)
			}
			if strings.TrimSpace(sh) != "" {
				if ent.Identifiers == nil {
					ent.Identifiers = map[string]string{}
				}
				ent.Identifiers["shortName"] = strings.TrimSpace(sh)
			}
		}
		if applyIndustry {
			// Use fragment's persisted industryPaths as truth (same dictionary as core used).
			ent.IndustryPaths = f.IndustryPaths
		}
		return entityRepo.Upsert(ctx, ent)
	}
	return nil
}

// convertWD adapts http WikidataEntityData to the ontology.WikidataEntityData used by core enrichment.
func convertWD(ed *shttp.WikidataEntityData) *coreontology.WikidataEntityData {
	if ed == nil {
		return nil
	}
	return &coreontology.WikidataEntityData{
		ID:            ed.ID,
		Label:         ed.Label,
		Description:   ed.Description,
		URL:           ed.URL,
		Properties:    ed.Properties,
		PropertiesRaw: ed.PropertiesRaw,
	}
}

func upsertEntityKBRef(ent *coreModels.Entity, qid string) {
	if ent == nil {
		return
	}
	qid = strings.TrimSpace(qid)
	if qid == "" {
		return
	}
	now := time.Now().UTC()
	// Update existing.
	for i := range ent.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ent.KBRefs[i].KB), "Wikidata") {
			ent.KBRefs[i].KB = "Wikidata"
			ent.KBRefs[i].ExternalID = qid
			ent.KBRefs[i].Confidence = 1.0
			ent.KBRefs[i].LastSync = &now
			return
		}
	}
	// Append.
	ent.KBRefs = append(ent.KBRefs, coreModels.KBReference{
		KB:         "Wikidata",
		ExternalID: qid,
		Confidence: 1.0,
		LastSync:   &now,
	})
}


type IndustryEvidence struct {
	IndustryQID   string
	IndustryLabel string
	IndustryKey   string
	IndustryPath  string
}

func canonicalizeIndustryPathsFromWikidata(
	ctx context.Context,
	wdClient *shttp.WikidataClient,
	entityData *shttp.WikidataEntityData,
	tax *coreModels.HorizonTaxonomy,
	industryCache map[string]*shttp.WikidataEntityData,
	delay time.Duration,
) ([]string, []IndustryEvidence) {
	if entityData == nil {
		return nil, nil
	}
	industryQIDs := extractWikidataQIDList(entityData.PropertiesRaw, "P452")
	if len(industryQIDs) == 0 {
		return nil, nil
	}

	seen := map[string]struct{}{}
	var out []string
	var evid []IndustryEvidence

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
				evid = append(evid, IndustryEvidence{IndustryQID: qid, IndustryPath: p})
				continue
			}
		}

		// 2) Label-based mapping via synonyms (requires fetching industry item label)
		if wdClient != nil {
			indData := (*shttp.WikidataEntityData)(nil)
			if industryCache != nil {
				indData = industryCache[qid]
			}
			if indData == nil {
				if delay > 0 {
					time.Sleep(delay)
				}
				got, err := wdClient.GetEntityData(ctx, qid)
				if err == nil && got != nil {
					indData = got
					if industryCache != nil {
						industryCache[qid] = got
					}
				}
			}

			if indData != nil {
				lbl := strings.TrimSpace(indData.Label)
				key := normalizeIndustryKey(lbl)
				if tax != nil && tax.IndustrySynonyms != nil {
					if p := strings.TrimSpace(tax.IndustrySynonyms[key]); strings.HasPrefix(p, "/industry/") {
						if _, ok := seen[p]; !ok {
							seen[p] = struct{}{}
							out = append(out, p)
						}
						evid = append(evid, IndustryEvidence{IndustryQID: qid, IndustryLabel: lbl, IndustryKey: key, IndustryPath: p})
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
					evid = append(evid, IndustryEvidence{IndustryQID: qid, IndustryLabel: lbl, IndustryKey: key, IndustryPath: p})
				}
			}
		}
	}

	// Stable order
	sortStrings(out)
	return out, evid
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

type monoText struct {
	Text string
	Lang string
}

func extractPreferredWikidataNames(ed *shttp.WikidataEntityData) (officialName string, shortName string) {
	if ed == nil {
		return "", ""
	}
	official := extractMonolingualTextList(ed.PropertiesRaw, "P1448")
	short := extractMonolingualTextList(ed.PropertiesRaw, "P1813")

	officialName = pickPreferredText(official)
	shortName = pickPreferredText(short)
	return strings.TrimSpace(officialName), strings.TrimSpace(shortName)
}

func pickPreferredText(list []monoText) string {
	if len(list) == 0 {
		return ""
	}
	// Prefer English, then any non-empty.
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v.Lang), "en") && strings.TrimSpace(v.Text) != "" {
			return strings.TrimSpace(v.Text)
		}
	}
	for _, v := range list {
		if strings.TrimSpace(v.Text) != "" {
			return strings.TrimSpace(v.Text)
		}
	}
	return ""
}

func extractMonolingualTextList(props map[string]interface{}, propID string) []monoText {
	if props == nil {
		return nil
	}
	raw, ok := props[propID]
	if !ok || raw == nil {
		return nil
	}
	out := []monoText{}
	add := func(text, lang string) {
		text = strings.TrimSpace(text)
		lang = strings.TrimSpace(lang)
		if text == "" {
			return
		}
		out = append(out, monoText{Text: text, Lang: lang})
	}

	// Supported shapes:
	// - []interface{}{ map[string]interface{}{"text": "...", "language": "en"}, ... }
	// - map[string]interface{}{"text":"...","language":"en"}
	// - string (fallback)
	switch v := raw.(type) {
	case []interface{}:
		for _, it := range v {
			switch vv := it.(type) {
			case map[string]interface{}:
				t, _ := vv["text"].(string)
				l, _ := vv["language"].(string)
				if t == "" {
					if x, ok := vv["value"].(string); ok {
						t = x
					}
				}
				add(t, l)
			case string:
				add(vv, "")
			}
		}
	case map[string]interface{}:
		t, _ := v["text"].(string)
		l, _ := v["language"].(string)
		if t == "" {
			if x, ok := v["value"].(string); ok {
				t = x
			}
		}
		add(t, l)
	case string:
		add(v, "")
	}
	return out
}

// NOTE: legacy applyWikidataEnrichment and the shim fragment updater were removed.
// This tool now applies enrichment via applyWikidataConservativeToFragmentAndEntity,
// which uses the shared core enrichment logic + HorizonFragmentsRepository.Update (versioned writes).

func ensureFragmentWikidataRef(f *hmodels.HorizonFragment, qid string) {
	if f == nil {
		return
	}
	qid = strings.TrimSpace(qid)
	if qid == "" {
		return
	}
	// Mirror into identifiers
	if f.Identifiers == nil {
		f.Identifiers = map[string]string{}
	}
	f.Identifiers["wikidataId"] = qid
	// Ensure KBRefs contains Wikidata
	for i := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(f.KBRefs[i].KB), "Wikidata") {
			f.KBRefs[i].KB = "Wikidata"
			if strings.TrimSpace(f.KBRefs[i].ExternalID) == "" {
				f.KBRefs[i].ExternalID = qid
			}
			if f.KBRefs[i].Confidence < 1.0 {
				f.KBRefs[i].Confidence = 1.0
			}
			return
		}
	}
	f.KBRefs = append(f.KBRefs, hmodels.KBReference{KB: "Wikidata", ExternalID: qid, Confidence: 1.0})
}

func ensureEntityWikidataRef(e *coreModels.Entity, qid string) {
	if e == nil {
		return
	}
	qid = strings.TrimSpace(qid)
	if qid == "" {
		return
	}
	for i := range e.KBRefs {
		if strings.EqualFold(strings.TrimSpace(e.KBRefs[i].KB), "Wikidata") {
			e.KBRefs[i].KB = "Wikidata"
			if strings.TrimSpace(e.KBRefs[i].ExternalID) == "" {
				e.KBRefs[i].ExternalID = qid
			}
			if e.KBRefs[i].Confidence < 1.0 {
				e.KBRefs[i].Confidence = 1.0
			}
			return
		}
	}
	now := time.Now().UTC()
	e.KBRefs = append(e.KBRefs, coreModels.KBReference{KB: "Wikidata", ExternalID: qid, Confidence: 1.0, LastSync: &now})
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

// ============================================================================
// Industry dictionary expansion (taxonomy growth)
// ============================================================================

type industryDictAccumulator struct {
	baseTax *coreModels.HorizonTaxonomy

	// Observations
	qidCount map[string]int
	qidPath  map[string]string // best-known canonical path for that industry QID
	keyPath  map[string]string // normalized label -> path (synonym)

	allowedPathSet map[string]struct{}
}

func newIndustryDictAccumulator(tax *coreModels.HorizonTaxonomy) *industryDictAccumulator {
	if tax == nil {
		return &industryDictAccumulator{
			baseTax:        nil,
			qidCount:       map[string]int{},
			qidPath:        map[string]string{},
			keyPath:        map[string]string{},
			allowedPathSet: map[string]struct{}{},
		}
	}
	allowed := map[string]struct{}{}
	for _, p := range tax.AllowedIndustryPaths {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "/industry/") {
			allowed[p] = struct{}{}
		}
	}
	return &industryDictAccumulator{
		baseTax:         tax,
		qidCount:        map[string]int{},
		qidPath:         map[string]string{},
		keyPath:         map[string]string{},
		allowedPathSet:  allowed,
	}
}

func (d *industryDictAccumulator) Observe(evid []IndustryEvidence) {
	if d == nil || len(evid) == 0 {
		return
	}
	for _, e := range evid {
		qid := strings.TrimSpace(e.IndustryQID)
		path := strings.TrimSpace(e.IndustryPath)
		key := strings.TrimSpace(e.IndustryKey)
		if qid == "" || !strings.HasPrefix(qid, "Q") {
			continue
		}
		if !strings.HasPrefix(path, "/industry/") {
			continue
		}
		d.qidCount[qid]++
		// Keep latest seen path (we don't try to resolve conflicts yet; most should be stable).
		d.qidPath[qid] = path
		if key != "" {
			d.keyPath[key] = path
		}
	}
}

func (d *industryDictAccumulator) ObservedQIDs() int {
	if d == nil {
		return 0
	}
	return len(d.qidCount)
}

// MergeIntoTaxonomy mutates the provided taxonomy by extending:
// - IndustryWikidataMap (QID -> /industry/...)
// - IndustrySynonyms (normalized label -> /industry/...)
// - AllowedIndustryPaths (ensure all inserted paths and ancestors exist)
// Returns counts of newly added entries.
func (d *industryDictAccumulator) MergeIntoTaxonomy(tax *coreModels.HorizonTaxonomy, maxNewQIDs int) (addedQIDs, addedSynonyms, addedAllowed int) {
	if d == nil || tax == nil {
		return 0, 0, 0
	}
	if maxNewQIDs <= 0 {
		maxNewQIDs = 500
	}
	if maxNewQIDs > 2000 {
		maxNewQIDs = 2000
	}

	if tax.IndustryWikidataMap == nil {
		tax.IndustryWikidataMap = map[string]string{}
	}
	if tax.IndustrySynonyms == nil {
		tax.IndustrySynonyms = map[string]string{}
	}
	if tax.AllowedIndustryPaths == nil {
		tax.AllowedIndustryPaths = []string{}
	}

	// Sort QIDs by frequency descending, add top maxNewQIDs that are not present.
	type qidFreq struct {
		qid  string
		freq int
	}
	qids := make([]qidFreq, 0, len(d.qidCount))
	for qid, c := range d.qidCount {
		qids = append(qids, qidFreq{qid: qid, freq: c})
	}
	for i := 0; i < len(qids); i++ {
		for j := i + 1; j < len(qids); j++ {
			if qids[j].freq > qids[i].freq {
				qids[i], qids[j] = qids[j], qids[i]
			}
		}
	}

	for _, item := range qids {
		if addedQIDs >= maxNewQIDs {
			break
		}
		qid := item.qid
		if _, exists := tax.IndustryWikidataMap[qid]; exists {
			continue
		}
		path := strings.TrimSpace(d.qidPath[qid])
		if !strings.HasPrefix(path, "/industry/") {
			continue
		}
		tax.IndustryWikidataMap[qid] = path
		addedQIDs++
		addedAllowed += ensureAllowedIndustryPath(tax, path)
	}

	// Add synonyms (bounded by maxNewQIDs*3 just to prevent huge growth per run).
	maxNewSyn := maxNewQIDs * 3
	for key, path := range d.keyPath {
		if addedSynonyms >= maxNewSyn {
			break
		}
		key = strings.TrimSpace(key)
		path = strings.TrimSpace(path)
		if key == "" || !strings.HasPrefix(path, "/industry/") {
			continue
		}
		if _, exists := tax.IndustrySynonyms[key]; exists {
			continue
		}
		tax.IndustrySynonyms[key] = path
		addedSynonyms++
		addedAllowed += ensureAllowedIndustryPath(tax, path)
	}

	// Keep allowlist stable sorted (helps diff stability)
	if len(tax.AllowedIndustryPaths) > 1 {
		sortStrings(tax.AllowedIndustryPaths)
	}

	return addedQIDs, addedSynonyms, addedAllowed
}

// ensureAllowedIndustryPath adds path and its ancestors to AllowedIndustryPaths.
// Returns how many new paths were appended.
func ensureAllowedIndustryPath(tax *coreModels.HorizonTaxonomy, path string) int {
	if tax == nil {
		return 0
	}
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/industry/") {
		return 0
	}
	// Build ancestors: /industry/a, /industry/a/b, ...
	anc := industryPathWithAncestors(path, 6)
	if len(anc) == 0 {
		return 0
	}
	seen := map[string]struct{}{}
	for _, p := range tax.AllowedIndustryPaths {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "/industry/") {
			seen[p] = struct{}{}
		}
	}
	added := 0
	for _, p := range anc {
		if !strings.HasPrefix(p, "/industry/") {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		tax.AllowedIndustryPaths = append(tax.AllowedIndustryPaths, p)
		added++
	}
	return added
}

func industryPathWithAncestors(path string, maxDepth int) []string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/industry/") {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return nil
	}
	if maxDepth <= 0 {
		maxDepth = 6
	}
	if len(parts) > maxDepth {
		parts = parts[:maxDepth]
	}
	out := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		out = append(out, "/"+strings.Join(parts[:i], "/"))
	}
	return out
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


