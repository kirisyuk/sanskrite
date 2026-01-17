package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

// Smart deduplication tool that:
// 1. Finds duplicates by obvious signals (Wikidata ID, shortName, legalName)
// 2. Groups them into clusters
// 3. Selects canonical fragment in each cluster (prefers /loc/ for GEO_REGION)
// 4. Fixes canonical paths (adds /loc/ prefix for GEO_REGION if missing)
// 5. Merges data from duplicates into canonical
// 6. Reassigns edges to canonical
// 7. Deletes duplicate fragments
// 8. Deduplicates resulting edges

var (
	projectID          = flag.String("project", "", "GCP project ID (required)")
	apply              = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun             = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")
	tenantID           = flag.String("tenant", "global", "Tenant ID (default: global)")
	verbose            = flag.Bool("verbose", false, "Verbose logging")
	typeFilter         = flag.String("type", "", "Filter by fragment type (GEO_REGION, ORGANIZATION, FACILITY, etc.)")
	maxFragments       = flag.Int("max-fragments", 0, "Stop after loading this many fragments (0 = no limit)")
	unionClusters      = flag.Bool("union-clusters", true, "Build transitive clusters via union-find across signals (recommended; helps catch chain duplicates). Set false to skip aliasPath-based transitive clustering.")
	skipVertexSync     = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync (faster, run bulk sync separately)")
	skipEdgePass       = flag.Bool("skip-edge-pass", false, "Skip global edge direction fix + symmetric normalization + edge dedup pass (safer for targeted fragment-only dedup runs)")
	pageSize           = flag.Int("page-size", 500, "Firestore page size for loading fragments (pagination). Lower = safer, higher = fewer round-trips")
	startAfterDocID    = flag.String("start-after", "", "Resume cursor: start loading fragments after this Firestore document ID (__name__)")
	pageTimeoutSec     = flag.Int("page-timeout-s", 90, "Timeout in seconds for each Firestore page fetch while loading fragments (0 disables)")
	enableProductAlias = flag.Bool("enable-product-alias", false, "Enable PRODUCT alias clustering (model/SKU heuristic). Default OFF: better duplicates than false-positive merges.")
	enableOrgNameOnly  = flag.Bool("enable-org-name-only", false, "Enable ORGANIZATION name-only clustering (shortName/label[en]). Default OFF (too many false merges).")
	enableOrgFuzzyName = flag.Bool("enable-org-fuzzy-name", false, "Enable ORGANIZATION fuzzy-ish name clustering (normalized legalName + stub-rescue normalized label/legal). Default OFF.")
	fixEdgesOnly       = flag.Bool("fix-edges-only", false, "Only fix/normalize edges (direction + symmetric normalization + dedup). Skips fragment dedup/merge even if duplicates are found.")
	canonicalizeEdges  = flag.Bool("edge-canonicalize", true, "Rewrite mis-modeled edges into canonical types (e.g., MARKET OWNS MARKET -> HAS_SUBMARKET, ORG DEPENDS_ON MARKET -> PARTICIPATES_IN).")
)

// Used to print a reliable resume cursor on Ctrl+C / SIGTERM and on load failures.
// NOTE: atomic.Value must be Store()'d before first Load().
var globalLastSeen atomic.Value // string

// DuplicateCluster represents a group of fragments that are duplicates of each other
type DuplicateCluster struct {
	MatchType   string                    // "wikidata", "shortName", "legalName", "labelEN", "normalizedLegal", "geoCity"
	MatchValue  string                    // The value that matched
	Fragments   []*models.HorizonFragment // All fragments in this cluster
	CanonicalID string                    // ID of the fragment to keep
}

type Stats struct {
	TotalFragments      int
	ClustersFound       int
	FragmentsMerged     int
	FragmentsDeleted    int
	PathsFixed          int
	EdgesReassigned     int
	EdgesDeleted        int
	EdgesDeduplicated   int
	EdgesDirectionFixed int
	EdgesCanonicalized  int
}

// =============================================================================
// ORGANIZATION dedup safety helpers (name-based signals are high risk)
// =============================================================================

// orgGroupishHint returns true if the fragment name/path suggests a holding/group/international brand
// which is often a distinct entity from an operating company.
// We intentionally do NOT include common corporate suffixes like Inc/LLC/Ltd/Company here.
func orgGroupishHint(f *models.HorizonFragment) bool {
	if f == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(f.CanonicalPath)) + " " +
		strings.ToLower(strings.TrimSpace(f.Labels["en"])) + " " +
		strings.ToLower(strings.TrimSpace(f.Identifiers["legalName"])) + " " +
		strings.ToLower(strings.TrimSpace(f.Identifiers["shortName"]))
	// Group-ish tokens that frequently indicate a different legal/top-level entity.
	toks := []string{
		"group",
		"holding",
		"holdings",
		"international",
		"collection",
	}
	for _, t := range toks {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// orgConflictingWikidataIDs checks if a set of fragments contains multiple distinct non-empty Wikidata IDs.
// If so, we should NOT merge them (strong evidence they are different entities).
func orgConflictingWikidataIDs(frags []*models.HorizonFragment) (conflict bool, ids []string) {
	seen := map[string]struct{}{}
	for _, f := range frags {
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		qid := strings.TrimSpace(extractWikidataID(f))
		if qid == "" {
			continue
		}
		seen[qid] = struct{}{}
	}
	if len(seen) <= 1 {
		return false, nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return true, out
}

// shouldSkipOrgNameCluster applies conservative guards for ORG clusters that are created from name-ish signals.
func shouldSkipOrgNameCluster(matchType string, frags []*models.HorizonFragment) (bool, string) {
	if len(frags) <= 1 {
		return true, "too_few"
	}
	// Only apply to ORGANIZATION clusters.
	for _, f := range frags {
		if f != nil && f.Type != models.FragmentTypeOrganization {
			return false, ""
		}
	}

	// Never merge if there are conflicting Wikidata IDs.
	if conflict, ids := orgConflictingWikidataIDs(frags); conflict {
		return true, fmt.Sprintf("conflicting_wikidata_ids=%v", ids)
	}

	// For fuzzy/name-only signals, avoid "groupish" vs "non-groupish" merges unless Wikidata is identical
	// (handled above). This prevents common false positives like:
	//   Hyundai Motor Group vs Hyundai Motor
	//   Casinos Austria International vs Casinos Austria
	groupish := 0
	nonGroupish := 0
	for _, f := range frags {
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		if orgGroupishHint(f) {
			groupish++
		} else {
			nonGroupish++
		}
	}
	if groupish > 0 && nonGroupish > 0 {
		// Only enforce for the riskier name-derived signals.
		switch matchType {
		case "shortName", "labelEN", "normalizedLegal", "orgNormalizedName", "orgSlugAlias":
			return true, fmt.Sprintf("groupish_mismatch(groupish=%d nonGroupish=%d)", groupish, nonGroupish)
		default:
		}
	}

	return false, ""
}

// extractOrgOrGovSlug extracts last segment slug from:
// - /org/{iso2}/{slug}
// - /org/{slug} (legacy)
// - /gov/{iso2}/{slug}
func extractOrgOrGovSlug(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(p, "/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	ns := parts[0]
	if ns != "org" && ns != "gov" {
		return ""
	}
	// /org/{slug}
	if ns == "org" && len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	// /org/{iso2}/{slug} or /gov/{iso2}/{slug}
	if len(parts) >= 3 {
		return strings.ToLower(strings.TrimSpace(parts[2]))
	}
	return ""
}

// extractLegacyOrgSlugAliases returns slug aliases from identifiers that store historical paths.
// We only care about legacy /org/{slug} aliases (merged_paths / legacy_org_canonical_path).
func extractLegacyOrgSlugAliases(f *models.HorizonFragment) []string {
	if f == nil || f.Type != models.FragmentTypeOrganization || f.Identifiers == nil {
		return nil
	}
	var out []string
	for _, p := range extractAliasCanonicalPathsForDedup(f) {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "/org/") {
			continue
		}
		parts := strings.Split(strings.Trim(p, "/"), "/")
		// Only legacy: /org/{slug}
		if len(parts) == 2 && parts[0] == "org" {
			slug := strings.ToLower(strings.TrimSpace(parts[1]))
			if slug != "" {
				out = append(out, slug)
			}
		}
	}
	return out
}

func isSafeOrgSlugAlias(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	// Too long and too generic slugs are risky for cross-country merges.
	if len(slug) > 10 {
		return false
	}
	// Require at least one letter.
	hasLetter := false
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') {
			hasLetter = true
		}
	}
	if !hasLetter {
		return false
	}
	// Avoid a few ultra-generic aliases.
	switch slug {
	case "bank", "group", "holding", "holdings", "fund", "company", "corp", "inc", "llc":
		return false
	}
	return true
}

func normalizeLegalKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}
	if *dryRun {
		*apply = false
	}
	if *pageSize <= 0 {
		*pageSize = 500
	}
	if *pageSize > 2000 {
		*pageSize = 2000 // safety cap
	}
	if *pageTimeoutSec < 0 {
		*pageTimeoutSec = 0
	}

	ctx := context.Background()

	log.Printf("=== Horizon Smart Deduplication Tool ===")
	log.Printf("project=%s tenant=%s apply=%v typeFilter=%q", *projectID, *tenantID, *apply, *typeFilter)
	if strings.TrimSpace(*startAfterDocID) != "" {
		log.Printf("resume cursor: start-after=%s", strings.TrimSpace(*startAfterDocID))
	}
	if *maxFragments > 0 {
		log.Printf("max-fragments: %d", *maxFragments)
	}
	log.Printf("pagination: page-size=%d page-timeout-s=%d", *pageSize, *pageTimeoutSec)

	// Always be able to print a resume cursor even if the process is interrupted (Ctrl+C / SIGTERM).
	globalLastSeen.Store(strings.TrimSpace(*startAfterDocID))
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

	// Initialize Firestore
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init GCS client: %v", err)
	}

	bucketName := getEnvDefault("HORIZON_FRAGMENTS_BUCKET", fmt.Sprintf("%s-horizon-fragments", *projectID))
	log.Printf("GCS bucket: %s", bucketName)

	fragmentsRepo := repo.NewHorizonFragmentsRepository(db).
		WithStorage(gcsClient, bucketName)

	// Optionally configure Vertex AI Search sync
	if !*skipVertexSync {
		vertexStoreID := getEnvDefault("VERTEX_SEARCH_DATA_STORE_ID", "rag-documents-store")
		vertexLocation := getEnvDefault("VERTEX_SEARCH_LOCATION", "global")
		vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexLocation, vertexStoreID, "")
		if vertexSearchClient.IsConfigured() {
			log.Printf("Vertex AI Search configured: store=%s location=%s", vertexStoreID, vertexLocation)
			fragmentsRepo = fragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				return vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
					GCSPath:            gcsURI,
					ReconciliationMode: "INCREMENTAL",
					DataSchema:         "document",
				})
			})
		} else {
			log.Printf("WARNING: Vertex AI Search not fully configured, skipping sync")
		}
	} else {
		log.Printf("Vertex AI Search sync DISABLED (--skip-vertex-sync). Run bulk sync separately after completion.")
	}
	edgesRepo := repo.NewDependencyEdgesRepository(db)

	stats := &Stats{}

	// Step 1: Load all fragments
	log.Printf("\n=== Step 1: Loading fragments ===")
	fragments, err := loadAllFragments(ctx, db, *tenantID, *typeFilter)
	if err != nil {
		if v := globalLastSeen.Load(); v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				log.Printf("resume cursor: start-after=%s", strings.TrimSpace(s))
			}
		}
		log.Fatalf("FATAL: failed to load fragments: %v", err)
	}
	stats.TotalFragments = len(fragments)
	log.Printf("✓ Loaded %d fragments", len(fragments))

	// Step 2: Build indexes for duplicate detection
	log.Printf("\n=== Step 2: Building indexes ===")
	wikidataIndex := make(map[string][]*models.HorizonFragment)               // wikidataID -> fragments
	shortNameIndex := make(map[string][]*models.HorizonFragment)              // type:shortName -> fragments
	legalNameIndex := make(map[string][]*models.HorizonFragment)              // type:legalName -> fragments
	labelENIndex := make(map[string][]*models.HorizonFragment)                // type:label_en -> fragments (Tier 2)
	normalizedLegalIndex := make(map[string][]*models.HorizonFragment)        // type:normalized_legal -> fragments
	orgNormalizedNameIndex := make(map[string][]*models.HorizonFragment)      // type:normalized(label/legal) -> org fragments (LOW-signal stub rescue)
	facilityNormalizedNameIndex := make(map[string][]*models.HorizonFragment) // facility_key -> facility fragments (normalized name; scoped by /fac/{org}/)
	geoCityIndex := make(map[string][]*models.HorizonFragment)                // city_name -> geo fragments
	productNameIndex := make(map[string][]*models.HorizonFragment)            // product_name -> product fragments
	productAliasIndex := make(map[string][]*models.HorizonFragment)           // product_alias_key -> product fragments (label/short/legal + legacy slug aliases)
	personPhoneticIndex := make(map[string][]*models.HorizonFragment)         // phonetic_key -> person fragments (catches Tahnoon/Tahnoun, Khalid/Halid)

	for _, f := range fragments {
		// Index by Wikidata ID
		if wikidataID := extractWikidataID(f); wikidataID != "" {
			wikidataIndex[wikidataID] = append(wikidataIndex[wikidataID], f)
		}

		// Index by shortName (type-scoped)
		if shortName := strings.TrimSpace(f.Identifiers["shortName"]); shortName != "" {
			// For ORG: shortName is name-only and very collision-prone; default OFF.
			if f.Type == models.FragmentTypeOrganization {
				// When explicitly enabled, allow both /org/* and /gov/* to participate (still ISO2-scoped).
				if *enableOrgNameOnly {
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					if iso2 != "na" {
						key := fmt.Sprintf("%s:%s:%s", f.Type, iso2, strings.ToLower(shortName))
						shortNameIndex[key] = append(shortNameIndex[key], f)
					}
				}
			} else {
				key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(shortName))
				shortNameIndex[key] = append(shortNameIndex[key], f)
			}
		}

		// Index by legalName (type-scoped)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			// For ORG: only within known ISO2. Include /gov/* here because exact legalName is high-confidence,
			// and selectCanonical() already prefers /org/* as the stable canonical namespace.
			if f.Type == models.FragmentTypeOrganization {
				iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
				if iso2 != "na" {
					key := fmt.Sprintf("%s:%s:%s", f.Type, iso2, strings.ToLower(legalName))
					legalNameIndex[key] = append(legalNameIndex[key], f)
				}
			} else {
				key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(legalName))
				legalNameIndex[key] = append(legalNameIndex[key], f)
			}
		}

		// Index by label[en] (type-scoped) — Tier 2 (high confidence)
		if labelEN := strings.TrimSpace(f.Labels["en"]); labelEN != "" {
			// For ORG: label is name-only and very collision-prone; default OFF.
			if f.Type == models.FragmentTypeOrganization {
				// When explicitly enabled, allow both /org/* and /gov/* to participate (still ISO2-scoped).
				if *enableOrgNameOnly {
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					if iso2 != "na" {
						key := fmt.Sprintf("%s:%s:%s", f.Type, iso2, strings.ToLower(labelEN))
						labelENIndex[key] = append(labelENIndex[key], f)
					}
				}
			} else {
				key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(labelEN))
				labelENIndex[key] = append(labelENIndex[key], f)
			}
		}

		// Index by normalized legalName (removes JSC, LLC, Inc, etc.)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			// For ORG: this is fuzzy-ish; default OFF.
			if f.Type == models.FragmentTypeOrganization {
				if !isGovCanonicalPath(f.CanonicalPath) && *enableOrgFuzzyName {
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					if iso2 != "na" {
						normalized := normalizeLegalName(legalName)
						if normalized != "" {
							key := fmt.Sprintf("%s:%s:%s", f.Type, iso2, normalized)
							normalizedLegalIndex[key] = append(normalizedLegalIndex[key], f)
						}
					}
				}
			} else {
				normalized := normalizeLegalName(legalName)
				if normalized != "" {
					key := fmt.Sprintf("%s:%s", f.Type, normalized)
					normalizedLegalIndex[key] = append(normalizedLegalIndex[key], f)
				}
			}
		}

		// FACILITY-only: normalized name index (stopwords/punctuation) scoped by /fac/{org}/ prefix.
		// This catches common duplicates like:
		//   "Four Points by Sheraton Riyadh Khaldia" vs "four points sheraton riyadh khaldia"
		// Cross-org matching: facilities with same normalized name are likely the same,
		// even if they are under different parent orgs (e.g., /fac/marriott/... vs /fac/khaldia-towers-company/...).
		// The normalization function requires at least 3 meaningful tokens to reduce false positives.
		if f.Type == models.FragmentTypeFacility {
			aliasSet := map[string]struct{}{}
			addFacilityAlias := func(s string, src string) {
				s = strings.TrimSpace(s)
				if s == "" {
					return
				}
				k := normalizeFacilityAliasKey(s)
				if k == "" {
					return
				}
				// No org scoping: if normalized name is specific enough, match across orgs
				key := fmt.Sprintf("%s:%s", f.Type, k)
				aliasSet[key] = struct{}{}
				// DEBUG for specific IDs
				if f.ID == "anz6w0uDOjDlSorQLA6S" || f.ID == "756btZKk76bzLQfNp7XM" {
					log.Printf("  DEBUG FACILITY INDEX: id=%s src=%s input=%q key=%q", f.ID, src, s, key)
				}
			}
			addFacilityAlias(f.Labels["en"], "label.en")
			addFacilityAlias(f.Identifiers["shortName"], "shortName")
			addFacilityAlias(f.Identifiers["legalName"], "legalName")
			// Also index last slug as a name-ish alias (unslugify).
			if pn := extractFacilityNameFromPath(f.CanonicalPath); pn != "" {
				addFacilityAlias(strings.ReplaceAll(pn, "-", " "), "canonicalSlug")
			}
			for k := range aliasSet {
				facilityNormalizedNameIndex[k] = append(facilityNormalizedNameIndex[k], f)
			}
		}

		// ORG-only: index normalized label/legal for LOW-signal stub rescue (e.g., "softbank group" → "softbank")
		// Safety is enforced when creating clusters (must include at least one high-signal org + one low-signal stub).
		if f.Type == models.FragmentTypeOrganization {
			// Default OFF: still name-based; also never run for /gov/* and never when country is unknown.
			if isGovCanonicalPath(f.CanonicalPath) || !*enableOrgFuzzyName {
				continue
			}
			iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
			if iso2 == "na" {
				continue
			}
			labelEN := strings.TrimSpace(f.Labels["en"])
			legalName := strings.TrimSpace(f.Identifiers["legalName"])
			seed := legalName
			if seed == "" {
				seed = labelEN
			}
			if seed != "" {
				n := normalizeLegalName(seed)
				if n != "" {
					key := fmt.Sprintf("%s:%s:%s", f.Type, iso2, n)
					orgNormalizedNameIndex[key] = append(orgNormalizedNameIndex[key], f)
				}
			}
		}

		// Index GEO_REGION by city name (last part of path)
		if f.Type == models.FragmentTypeGeoRegion {
			cityName := extractCityFromPath(f.CanonicalPath)
			if cityName != "" {
				geoCityIndex[cityName] = append(geoCityIndex[cityName], f)
			}
		}

		// Index PRODUCT by canonical slug (SAFE):
		// - Flat canonical: /product/{slug}
		// - Taxonomy canonical: /product/{category}/{type}/{stage}/{slug}
		//
		// IMPORTANT: Do NOT include legacy org-nested paths (/product/{org}/{slug})
		// and do NOT use merged_paths here. Both cause massive false-positive merges like:
		//   "crm": /product/zoho/crm vs unrelated products that happened to have merged_paths containing "/product/.../crm".
		if f.Type == models.FragmentTypeProduct {
			if slug := extractCanonicalProductSlugForDedup(f.CanonicalPath); slug != "" {
				productNameIndex[slug] = append(productNameIndex[slug], f)
			}

			// Additional PRODUCT alias index:
			// - labels.en / identifiers.shortName / identifiers.legalName
			// - plus unslugified last segment of canonicalPath
			// This catches cases like:
			//   label: "M1 Abrams" vs other.shortName: "M1 Abrams"
			// even if canonicalPath slugs differ (/product/m1-abrams vs /product/general-dynamics/abrams-tank).
			aliasSet := map[string]struct{}{}
			addAlias := func(s string) {
				s = strings.TrimSpace(s)
				if s == "" {
					return
				}
				k := normalizeProductAliasKey(s)
				if k == "" {
					return
				}
				aliasSet[k] = struct{}{}
			}
			addAlias(f.Labels["en"])
			addAlias(f.Identifiers["shortName"])
			addAlias(f.Identifiers["legalName"])
			// Also index last slug as a name-ish alias (unslugify).
			if pn := extractProductNameFromPath(f.CanonicalPath); pn != "" {
				addAlias(strings.ReplaceAll(pn, "-", " "))
			}
			for k := range aliasSet {
				productAliasIndex[k] = append(productAliasIndex[k], f)
			}
		}

		// PERSON: phonetic index for Arabic/transliteration variations (Tahnoon/Tahnoun, Khalid/Halid)
		if f.Type == "PERSON" {
			phoneticSet := map[string]struct{}{}
			addPersonPhonetic := func(s string) {
				s = strings.TrimSpace(s)
				if s == "" {
					return
				}
				k := normalizePhoneticKey(s)
				if k == "" {
					return
				}
				key := fmt.Sprintf("PERSON:%s", k)
				phoneticSet[key] = struct{}{}
			}
			addPersonPhonetic(f.Labels["en"])
			addPersonPhonetic(f.Identifiers["shortName"])
			addPersonPhonetic(f.Identifiers["legalName"])
			// Also index from canonical path slug
			if slug := extractPersonNameFromPath(f.CanonicalPath); slug != "" {
				addPersonPhonetic(strings.ReplaceAll(slug, "-", " "))
			}
			for k := range phoneticSet {
				personPhoneticIndex[k] = append(personPhoneticIndex[k], f)
			}
		}
	}

	// Index GEO_REGION by canonical path (for org-as-country matching)
	geoByPath := make(map[string]*models.HorizonFragment)
	for _, f := range fragments {
		if f.Type == models.FragmentTypeGeoRegion {
			geoByPath[f.CanonicalPath] = f
		}
	}

	// Index by canonicalPath (type-scoped) — useful for alias canonicalPath clustering
	byCanonicalPath := make(map[string][]*models.HorizonFragment) // type:canonicalPath -> fragments
	for _, f := range fragments {
		if strings.TrimSpace(f.CanonicalPath) == "" {
			continue
		}
		k := fmt.Sprintf("%s:%s", f.Type, f.CanonicalPath)
		byCanonicalPath[k] = append(byCanonicalPath[k], f)
	}

	// ORG/GOV slug indexes (for resolving legacy alias /org/{slug} → canonical /org/{iso2}/{slug} and /gov/{iso2}/{slug}).
	// This helps catch cases like:
	//   /org/public-investment-fund with identifiers.merged_paths="/org/pif"
	//   should merge into /org/sa/pif and/or /gov/sa/pif even if /org/pif doesn't exist as a standalone fragment.
	orgGovSlugIndex := make(map[string][]*models.HorizonFragment)   // slug -> fragments with /org/{iso2}/{slug} or /gov/{iso2}/{slug} (and legacy /org/{slug})
	orgAliasSlugIndex := make(map[string][]*models.HorizonFragment) // slug -> fragments that declare alias via merged_paths / legacy_org_canonical_path (legacy /org/{slug})
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		if slug := extractOrgOrGovSlug(f.CanonicalPath); slug != "" {
			orgGovSlugIndex[slug] = append(orgGovSlugIndex[slug], f)
		}
		for _, slug := range extractLegacyOrgSlugAliases(f) {
			if !isSafeOrgSlugAlias(slug) {
				continue
			}
			orgAliasSlugIndex[slug] = append(orgAliasSlugIndex[slug], f)
		}
	}

	log.Printf("✓ Wikidata index: %d unique IDs", len(wikidataIndex))
	log.Printf("✓ ShortName index: %d unique keys", len(shortNameIndex))
	log.Printf("✓ LegalName index: %d unique keys", len(legalNameIndex))
	log.Printf("✓ Label[en] index: %d unique keys", len(labelENIndex))
	log.Printf("✓ Normalized legal index: %d unique keys", len(normalizedLegalIndex))
	log.Printf("✓ ORG normalized name index: %d unique keys", len(orgNormalizedNameIndex))
	log.Printf("✓ FACILITY normalized name index: %d unique keys", len(facilityNormalizedNameIndex))
	log.Printf("✓ GEO city index: %d unique cities", len(geoCityIndex))
	log.Printf("✓ GEO by path index: %d unique paths", len(geoByPath))
	log.Printf("✓ CanonicalPath index: %d unique keys", len(byCanonicalPath))
	log.Printf("✓ PRODUCT name index: %d unique names", len(productNameIndex))
	log.Printf("✓ PRODUCT alias index: %d unique aliases", len(productAliasIndex))
	log.Printf("✓ PERSON phonetic index: %d unique keys", len(personPhoneticIndex))

	// Build fragment type cache for edge direction fixing (Step 6)
	fragmentTypes := make(map[string]models.FragmentType)
	for _, f := range fragments {
		fragmentTypes[f.ID] = f.Type
	}
	log.Printf("✓ Fragment type cache: %d entries", len(fragmentTypes))

	// Step 3: Build duplicate clusters
	log.Printf("\n=== Step 3: Finding duplicate clusters ===")
	clusters := make([]*DuplicateCluster, 0)
	processedFragmentIDs := make(map[string]bool)
	idToFrag := make(map[string]*models.HorizonFragment, len(fragments))
	for _, f := range fragments {
		if f != nil && strings.TrimSpace(f.ID) != "" {
			idToFrag[f.ID] = f
		}
	}

	// Priority 0: Organizations that are actually countries (type normalization)
	// Example: /org/usa with legalName="United States of America" → merge with /loc/us/na/na
	orgAsCountryClusters := 0
	for _, f := range fragments {
		if processedFragmentIDs[f.ID] {
			continue
		}
		if f.Type != models.FragmentTypeOrganization {
			continue
		}
		// Never treat /gov/* as "org-as-country": it's a different namespace and should not be merged into GEO_REGION.
		if isGovCanonicalPath(f.CanonicalPath) {
			continue
		}

		// Check if any name matches a known country
		namesToCheck := []string{
			strings.ToLower(strings.TrimSpace(f.Identifiers["legalName"])),
			strings.ToLower(strings.TrimSpace(f.Labels["en"])),
			strings.ToLower(strings.TrimSpace(f.Identifiers["shortName"])),
		}

		for _, name := range namesToCheck {
			if name == "" {
				continue
			}
			if scout.IsKnownCountryName(name) {
				geoPath := scout.GetCountryCanonicalPath(name)
				if geoPath != "" {
					// Find existing GEO_REGION with this path
					if geoFrag, exists := geoByPath[geoPath]; exists && geoFrag.ID != f.ID && !processedFragmentIDs[geoFrag.ID] {
						cluster := &DuplicateCluster{
							MatchType:   "orgAsCountry",
							MatchValue:  fmt.Sprintf("%s → %s", f.CanonicalPath, geoPath),
							Fragments:   []*models.HorizonFragment{geoFrag, f},
							CanonicalID: geoFrag.ID, // Always prefer the GEO_REGION
						}
						clusters = append(clusters, cluster)
						processedFragmentIDs[f.ID] = true
						processedFragmentIDs[geoFrag.ID] = true
						orgAsCountryClusters++
						break
					}
				}
			}
		}
	}
	log.Printf("  Found %d org-as-country clusters", orgAsCountryClusters)

	// Priority 0.5: Non-canonical GEO paths → merge with canonical /loc/{cc}/na/na
	// Example: /loc/uae → /loc/ae/na/na, /loc/saudi-arabia → /loc/sa/na/na
	// CAUTION: Avoid false positives like /loc/georgia (US state) vs /loc/ge/na/na (country Georgia)
	nonCanonicalGeoClusters := 0
	for _, f := range fragments {
		if processedFragmentIDs[f.ID] {
			continue
		}
		if f.Type != models.FragmentTypeGeoRegion {
			continue
		}
		// Check if path is non-canonical (not /loc/{cc}/{admin1}/{city})
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		if len(parts) == 2 && parts[0] == "loc" {
			// This is /loc/{something} — check if {something} is a country name/alias
			countrySlug := strings.ReplaceAll(parts[1], "-", " ")
			countrySlug = strings.ReplaceAll(countrySlug, "_", " ")
			canonicalPath := scout.GetCountryCanonicalPath(countrySlug)
			if canonicalPath != "" && canonicalPath != f.CanonicalPath {
				// Find existing canonical GEO_REGION
				if geoFrag, exists := geoByPath[canonicalPath]; exists && geoFrag.ID != f.ID && !processedFragmentIDs[geoFrag.ID] {
					// SAFETY: Only merge if the non-canonical fragment is "empty" (no label/legalName set)
					// OR if labels/legalName explicitly match a country name.
					// This prevents merging /loc/georgia (US state) with /loc/ge/na/na (Georgia country).
					nonCanonLabel := strings.ToLower(strings.TrimSpace(f.Labels["en"]))
					nonCanonLegal := strings.ToLower(strings.TrimSpace(f.Identifiers["legalName"]))
					isEmpty := nonCanonLabel == "" && nonCanonLegal == ""
					labelMatchesCountry := scout.IsKnownCountryName(nonCanonLabel) || scout.IsKnownCountryName(nonCanonLegal)
					// Also check if label is just the slug (e.g., "uae", "saudi-arabia")
					labelIsSlug := nonCanonLabel == strings.ToLower(parts[1]) || nonCanonLabel == countrySlug

					if isEmpty || labelMatchesCountry || labelIsSlug {
						cluster := &DuplicateCluster{
							MatchType:   "nonCanonicalGeo",
							MatchValue:  fmt.Sprintf("%s → %s", f.CanonicalPath, canonicalPath),
							Fragments:   []*models.HorizonFragment{geoFrag, f},
							CanonicalID: geoFrag.ID, // Always prefer the canonical path
						}
						clusters = append(clusters, cluster)
						processedFragmentIDs[f.ID] = true
						processedFragmentIDs[geoFrag.ID] = true
						nonCanonicalGeoClusters++
					} else if *verbose {
						log.Printf("  SKIPPED potential false-positive: %s (label=%q) vs %s", f.CanonicalPath, nonCanonLabel, canonicalPath)
					}
				}
			}
		}
	}
	log.Printf("  Found %d non-canonical GEO clusters", nonCanonicalGeoClusters)

	// Priority 0.75: Canonical-path aliases (merged_paths / legacy_org_canonical_path) — same type only
	// Example: /org/riyadh-air declared as legacy_org_canonical_path by canonical /org/riyadh_air
	// Example: /org/pif declared as merged_paths by canonical /org/public-investment-fund
	aliasClusters := 0
	{
		if unionClusters != nil && !*unionClusters {
			if *verbose {
				log.Printf("  Skipping aliasPath transitive clustering (--union-clusters=false)")
			}
		} else {
			uf := newUnionFind()
			ids := make([]string, 0, len(fragments))
			for _, f := range fragments {
				if f == nil {
					continue
				}
				if processedFragmentIDs[f.ID] {
					continue
				}
				if strings.TrimSpace(f.ID) == "" {
					continue
				}
				uf.add(f.ID)
				ids = append(ids, f.ID)
			}

			// Union pairs connected by alias canonicalPath references.
			for _, f := range fragments {
				if f == nil {
					continue
				}
				if processedFragmentIDs[f.ID] {
					continue
				}
				// Keep this conservative: org-specific alias identifiers are high-signal for ORGs,
				// but can be noisy for other types (we have separate, safer dedup signals for them).
				if f.Type != models.FragmentTypeOrganization {
					continue
				}
				for _, alias := range extractAliasCanonicalPathsForDedup(f) {
					k := fmt.Sprintf("%s:%s", f.Type, alias) // same type only
					for _, other := range byCanonicalPath[k] {
						if other == nil || processedFragmentIDs[other.ID] || other.ID == f.ID {
							continue
						}
						uf.union(f.ID, other.ID)
					}
				}
			}

			// Materialize groups into clusters.
			for _, group := range uf.groups(ids) {
				if len(group) <= 1 {
					continue
				}
				var frs []*models.HorizonFragment
				for _, id := range group {
					if f := idToFrag[id]; f != nil {
						frs = append(frs, f)
					}
				}
				if len(frs) <= 1 {
					continue
				}
				cluster := &DuplicateCluster{
					MatchType:  "aliasPath",
					MatchValue: fmt.Sprintf("%s:%s", frs[0].Type, frs[0].ID),
					Fragments:  frs,
				}
				cluster.CanonicalID = selectCanonical(frs)
				clusters = append(clusters, cluster)
				// Mark only duplicates as processed, so the canonical can still absorb
				// additional duplicates found by other signals later (transitive merges).
				for _, f := range frs {
					if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
						processedFragmentIDs[f.ID] = true
					}
				}
				aliasClusters++
			}
		}
	}
	log.Printf("  Found %d aliasPath-based clusters", aliasClusters)

	// Priority 0.775: ORG legacy slug aliases (/org/{slug} in merged_paths) -> canonical /org/{iso2}/{slug} or /gov/{iso2}/{slug}.
	// This is conservative and guarded:
	// - slug must be short-ish and not generic
	// - legalName must not conflict across candidates
	// - conflicting Wikidata IDs forbid merge
	orgSlugAliasClusters := 0
	for slug, claimers := range orgAliasSlugIndex {
		slug = strings.ToLower(strings.TrimSpace(slug))
		if !isSafeOrgSlugAlias(slug) {
			continue
		}
		// Candidate set: anything already living under .../{slug} plus the fragments that claim this alias.
		cands := make([]*models.HorizonFragment, 0, len(orgGovSlugIndex[slug])+len(claimers))
		cands = append(cands, orgGovSlugIndex[slug]...)
		cands = append(cands, claimers...)
		if len(cands) <= 1 {
			continue
		}
		// Unique by ID and skip processed.
		seenID := map[string]struct{}{}
		unprocessed := make([]*models.HorizonFragment, 0, len(cands))
		for _, f := range cands {
			if f == nil {
				continue
			}
			id := strings.TrimSpace(f.ID)
			if id == "" || processedFragmentIDs[id] {
				continue
			}
			if _, ok := seenID[id]; ok {
				continue
			}
			seenID[id] = struct{}{}
			unprocessed = append(unprocessed, f)
		}
		if len(unprocessed) <= 1 {
			continue
		}

		// LegalName must not conflict (ignore empties).
		lnSet := map[string]struct{}{}
		for _, f := range unprocessed {
			if f == nil || f.Type != models.FragmentTypeOrganization {
				continue
			}
			ln := normalizeLegalKey(f.Identifiers["legalName"])
			if ln == "" {
				continue
			}
			lnSet[ln] = struct{}{}
		}
		if len(lnSet) > 1 {
			if *verbose {
				log.Printf("  SKIP ORG cluster (orgSlugAlias): slug=%s reason=conflicting_legal_names", slug)
			}
			continue
		}
		if skip, reason := shouldSkipOrgNameCluster("orgSlugAlias", unprocessed); skip {
			if *verbose {
				log.Printf("  SKIP ORG cluster (orgSlugAlias): slug=%s reason=%s", slug, reason)
			}
			continue
		}

		cluster := &DuplicateCluster{
			MatchType:  "orgSlugAlias",
			MatchValue: slug,
			Fragments:  unprocessed,
		}
		cluster.CanonicalID = selectCanonical(unprocessed)
		clusters = append(clusters, cluster)
		for _, f := range unprocessed {
			if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
				processedFragmentIDs[f.ID] = true
			}
		}
		orgSlugAliasClusters++
	}
	log.Printf("  Found %d orgSlugAlias-based clusters", orgSlugAliasClusters)

	// Priority 0.8: Organization path normalization (- vs _) — same type only
	// Example: /org/riyadh_air vs /org/riyadh-air
	orgPathNormClusters := 0
	{
		orgNorm := make(map[string][]*models.HorizonFragment)
		for _, f := range fragments {
			if f == nil {
				continue
			}
			if processedFragmentIDs[f.ID] {
				continue
			}
			if f.Type != models.FragmentTypeOrganization {
				continue
			}
			nk := normalizeOrgCanonicalPathForDedup(f.CanonicalPath)
			if nk == "" {
				continue
			}
			orgNorm[nk] = append(orgNorm[nk], f)
		}
		for key, frs := range orgNorm {
			if len(frs) <= 1 {
				continue
			}
			cluster := &DuplicateCluster{
				MatchType:  "orgPathNorm",
				MatchValue: key,
				Fragments:  frs,
			}
			cluster.CanonicalID = selectCanonical(frs)
			clusters = append(clusters, cluster)
			for _, f := range frs {
				if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
					processedFragmentIDs[f.ID] = true
				}
			}
			orgPathNormClusters++
		}
	}
	log.Printf("  Found %d orgPathNorm-based clusters", orgPathNormClusters)

	// Priority 1: Wikidata ID matches (highest confidence)
	for wikidataID, frags := range wikidataIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "wikidata",
					MatchValue: wikidataID,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
			}
		}
	}
	log.Printf("  Found %d Wikidata-based clusters", len(clusters))

	// Priority 2: ShortName matches (same type)
	// CAUTION: For GEO_REGION, only group if same country or at least one has non-canonical path
	shortNameClusters := 0
	for key, frags := range shortNameIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				// For GEO_REGION: filter out fragments with different countries
				fragType := unprocessed[0].Type
				if fragType == models.FragmentTypeGeoRegion {
					unprocessed = filterGeoByCountry(unprocessed)
				}
			}
			if len(unprocessed) > 1 {
				// Extra safety for ORGANIZATION: name-only signals are risky.
				if unprocessed[0].Type == models.FragmentTypeOrganization {
					if skip, reason := shouldSkipOrgNameCluster("shortName", unprocessed); skip {
						if *verbose {
							log.Printf("  SKIP ORG cluster (shortName): key=%s reason=%s", key, reason)
						}
						continue
					}
				}
				cluster := &DuplicateCluster{
					MatchType:  "shortName",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				shortNameClusters++
			}
		}
	}
	log.Printf("  Found %d shortName-based clusters", shortNameClusters)

	// Priority 3: LegalName matches (same type)
	legalNameClusters := 0
	for key, frags := range legalNameIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				// For GEO_REGION: filter out fragments with different countries
				fragType := unprocessed[0].Type
				if fragType == models.FragmentTypeGeoRegion {
					unprocessed = filterGeoByCountry(unprocessed)
				}
			}
			if len(unprocessed) > 1 {
				// Extra safety for ORGANIZATION: even exact legalName can collide; never merge across distinct Wikidata IDs.
				if unprocessed[0].Type == models.FragmentTypeOrganization {
					if skip, reason := shouldSkipOrgNameCluster("legalName", unprocessed); skip {
						if *verbose {
							log.Printf("  SKIP ORG cluster (legalName): key=%s reason=%s", key, reason)
						}
						continue
					}
				}
				cluster := &DuplicateCluster{
					MatchType:  "legalName",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				legalNameClusters++
			}
		}
	}
	log.Printf("  Found %d legalName-based clusters", legalNameClusters)

	// Priority 4: Label[en] matches (same type) — catches duplicates with same display name
	labelENClusters := 0
	for key, frags := range labelENIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				// For GEO_REGION: filter out fragments with different countries
				fragType := unprocessed[0].Type
				if fragType == models.FragmentTypeGeoRegion {
					unprocessed = filterGeoByCountry(unprocessed)
				}
			}
			if len(unprocessed) > 1 {
				// Extra safety for ORGANIZATION: label-only signals are risky.
				if unprocessed[0].Type == models.FragmentTypeOrganization {
					if skip, reason := shouldSkipOrgNameCluster("labelEN", unprocessed); skip {
						if *verbose {
							log.Printf("  SKIP ORG cluster (labelEN): key=%s reason=%s", key, reason)
						}
						continue
					}
				}
				cluster := &DuplicateCluster{
					MatchType:  "labelEN",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				labelENClusters++
			}
		}
	}
	log.Printf("  Found %d label[en]-based clusters", labelENClusters)

	// Priority 4.5: FACILITY normalized name matches (scoped by /fac/{org}/ when available)
	// NOTE: We allow canonicals from earlier clusters to participate here for transitive merges.
	// Example: A is canonical in shortName cluster {A,B}, but A also matches C via facilityNormName.
	// We want {A,B,C} to all merge. So we only skip fragments that were marked as DUPLICATES, not canonicals.
	facilityNormNameClusters := 0
	for key, frags := range facilityNormalizedNameIndex {
		if len(frags) <= 1 {
			continue
		}
		var unprocessed []*models.HorizonFragment
		for _, f := range frags {
			if f == nil {
				continue
			}
			// Allow fragment even if processed, as long as it might bridge clusters.
			// Safety: we'll dedupe clusters later anyway.
			unprocessed = append(unprocessed, f)
		}
		if len(unprocessed) <= 1 {
			continue
		}

		// Extra safety: require at least one non-stub (some signal like legalName/shortName or ACTIVE governance)
		nonStub := 0
		for _, f := range unprocessed {
			if strings.TrimSpace(f.Identifiers["legalName"]) != "" ||
				strings.TrimSpace(f.Identifiers["shortName"]) != "" ||
				strings.EqualFold(strings.TrimSpace(f.Governance.Status), "ACTIVE") {
				nonStub++
			}
		}
		if nonStub == 0 {
			continue
		}

		cluster := &DuplicateCluster{
			MatchType:  "facilityNormName",
			MatchValue: key,
			Fragments:  unprocessed,
		}
		cluster.CanonicalID = selectCanonical(unprocessed)
		clusters = append(clusters, cluster)
		for _, f := range unprocessed {
			if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
				processedFragmentIDs[f.ID] = true
			}
		}
		facilityNormNameClusters++
	}
	log.Printf("  Found %d facilityNormName-based clusters", facilityNormNameClusters)

	// Priority 5: Normalized legalName matches (removes JSC, LLC, Inc, etc.)
	// NOTE: Skip for GEO_REGION — normalization is too aggressive (e.g., "City of London" → "London")
	normalizedLegalClusters := 0
	for key, frags := range normalizedLegalIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				// Skip GEO_REGION — normalization removes "City of", "State of", etc.
				fragType := unprocessed[0].Type
				if fragType == models.FragmentTypeGeoRegion {
					continue // Don't create cluster for GEO_REGION with normalized names
				}
				// Extra safety for ORGANIZATION: normalized legal can collapse group vs operating company.
				if fragType == models.FragmentTypeOrganization {
					if skip, reason := shouldSkipOrgNameCluster("normalizedLegal", unprocessed); skip {
						if *verbose {
							log.Printf("  SKIP ORG cluster (normalizedLegal): key=%s reason=%s", key, reason)
						}
						continue
					}
				}
				cluster := &DuplicateCluster{
					MatchType:  "normalizedLegal",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				normalizedLegalClusters++
			}
		}
	}
	log.Printf("  Found %d normalizedLegal-based clusters", normalizedLegalClusters)

	// Priority 5.5: ORG normalized name (label/legal) — LOW-signal stub rescue (guarded)
	// Example: /org/softbank-group (label="softbank group", no identifiers) should merge into /org/softbank (legalName="SoftBank Group Corp.")
	orgNormalizedClusters := 0
	for key, frags := range orgNormalizedNameIndex {
		if len(frags) <= 1 {
			continue
		}
		var unprocessed []*models.HorizonFragment
		for _, f := range frags {
			if f == nil || processedFragmentIDs[f.ID] {
				continue
			}
			unprocessed = append(unprocessed, f)
		}
		if len(unprocessed) <= 1 {
			continue
		}

		// SAFETY: require at least one "high-signal" org and at least one "low-signal" stub
		high := 0
		low := 0
		for _, f := range unprocessed {
			if isLowSignalOrgStubForDedup(f) {
				low++
			} else {
				high++
			}
		}
		if high == 0 || low == 0 {
			continue
		}

		// Extra safety: do not merge if Wikidata conflicts or groupish mismatch.
		if skip, reason := shouldSkipOrgNameCluster("orgNormalizedName", unprocessed); skip {
			if *verbose {
				log.Printf("  SKIP ORG cluster (orgNormalizedName): key=%s reason=%s", key, reason)
			}
			continue
		}

		cluster := &DuplicateCluster{
			MatchType:  "orgNormalizedName",
			MatchValue: key,
			Fragments:  unprocessed,
		}
		cluster.CanonicalID = selectCanonical(unprocessed)
		clusters = append(clusters, cluster)
		for _, f := range unprocessed {
			if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
				processedFragmentIDs[f.ID] = true
			}
		}
		orgNormalizedClusters++
	}
	log.Printf("  Found %d orgNormalizedName-based clusters", orgNormalizedClusters)

	// Priority 6: GEO city name matches — MUST have same country
	geoCityClusters := 0
	for cityName, frags := range geoCityIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				// CRITICAL: Filter by country — same city name in different countries are NOT duplicates
				unprocessed = filterGeoByCountry(unprocessed)
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "geoCity",
					MatchValue: cityName,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				geoCityClusters++
			}
		}
	}
	log.Printf("  Found %d geoCity-based clusters", geoCityClusters)

	// Priority 7: PRODUCT name matches (last part of path)
	// This catches /product/lithium vs /product/raw/mineral/extraction/lithium
	productNameClusters := 0
	for productName, frags := range productNameIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "productName",
					MatchValue: productName,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				productNameClusters++
			}
		}
	}
	log.Printf("  Found %d productName-based clusters", productNameClusters)

	// Priority 8: PRODUCT alias matches (label/short/legal + slug aliases) for "model-like" products.
	// DEFAULT OFF: better duplicates than false-positive merges.
	// Enable explicitly with --enable-product-alias.
	productAliasClusters := 0
	if *enableProductAlias {
		// SAFETY: Only apply to aliases that contain digits and at least two tokens (e.g., "m1 abrams", "f 16").
		// This avoids false positives for generic names like "abrams" or "tank".
		for aliasKey, frags := range productAliasIndex {
			if len(frags) <= 1 {
				continue
			}
			if !isSpecificProductAliasKey(aliasKey) {
				continue
			}
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "productAlias",
					MatchValue: aliasKey,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				productAliasClusters++
			}
		}
		log.Printf("  Found %d productAlias-based clusters", productAliasClusters)
	} else {
		log.Printf("  Found %d productAlias-based clusters (disabled; use --enable-product-alias)", productAliasClusters)
	}

	// Priority 7: PERSON phonetic matches (catches Tahnoon/Tahnoun, Khalid/Halid)
	personPhoneticClusters := 0
	for key, frags := range personPhoneticIndex {
		if len(frags) <= 1 {
			continue
		}
		var unprocessed []*models.HorizonFragment
		for _, f := range frags {
			if !processedFragmentIDs[f.ID] {
				unprocessed = append(unprocessed, f)
			}
		}
		if len(unprocessed) > 1 {
			cluster := &DuplicateCluster{
				MatchType:  "personPhonetic",
				MatchValue: key,
				Fragments:  unprocessed,
			}
			cluster.CanonicalID = selectCanonical(unprocessed)
			clusters = append(clusters, cluster)
			for _, f := range unprocessed {
				processedFragmentIDs[f.ID] = true
			}
			personPhoneticClusters++
		}
	}
	log.Printf("  Found %d personPhonetic-based clusters", personPhoneticClusters)

	stats.ClustersFound = len(clusters)
	log.Printf("✓ Total duplicate clusters: %d", len(clusters))

	if len(clusters) == 0 {
		log.Printf("\nNo duplicates found!")
		if !*fixEdgesOnly {
			return
		}
	}

	// Step 4: Process each cluster
	log.Printf("\n=== Step 4: Processing clusters ===")
	if *fixEdgesOnly {
		log.Printf("SKIP: fragment merge step disabled (--fix-edges-only)")
		goto FIX_EDGES
	}
	for i, cluster := range clusters {
		if i%100 == 0 {
			log.Printf("  Progress: %d/%d clusters processed...", i, len(clusters))
		}

		if *verbose {
			log.Printf("\n---- Cluster %d/%d [%s: %s] ----", i+1, len(clusters), cluster.MatchType, cluster.MatchValue)
		}

		// Find canonical fragment
		var canonical *models.HorizonFragment
		var duplicates []*models.HorizonFragment
		for _, f := range cluster.Fragments {
			if f.ID == cluster.CanonicalID {
				canonical = f
			} else {
				duplicates = append(duplicates, f)
			}
		}

		if canonical == nil {
			log.Printf("  ERROR: canonical fragment not found for cluster %s", cluster.MatchValue)
			continue
		}

		if *verbose {
			log.Printf("  Canonical: %s (%s)", canonical.CanonicalPath, canonical.ID)
			for _, dup := range duplicates {
				log.Printf("  Duplicate: %s (%s)", dup.CanonicalPath, dup.ID)
			}
		}

		// Fix canonical path for GEO_REGION if needed
		pathFixed := false
		if canonical.Type == models.FragmentTypeGeoRegion {
			if !strings.HasPrefix(canonical.CanonicalPath, "/loc/") {
				oldPath := canonical.CanonicalPath
				canonical.CanonicalPath = fixGeoPath(canonical.CanonicalPath)
				pathFixed = true
				stats.PathsFixed++
				if *verbose {
					log.Printf("  Path fixed: %s -> %s", oldPath, canonical.CanonicalPath)
				}
			}
		}

		if !*apply {
			if *verbose {
				log.Printf("  [DRY-RUN] Would merge %d duplicates into canonical", len(duplicates))
			}
			stats.FragmentsMerged += len(duplicates)
			stats.FragmentsDeleted += len(duplicates)
			continue
		}

		// 4a. Merge data from duplicates into canonical
		changed := pathFixed
		for _, dup := range duplicates {
			if mergeFragmentData(canonical, dup) {
				changed = true
			}
		}
		if changed {
			if err := fragmentsRepo.Update(ctx, canonical); err != nil {
				log.Printf("  ERROR: failed to update canonical: %v", err)
				continue
			}
			if *verbose {
				log.Printf("  ✓ Merged data into canonical")
			}
		}
		stats.FragmentsMerged += len(duplicates)

		// 4b. Reassign edges from duplicates to canonical
		for _, dup := range duplicates {
			reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, dup.ID, canonical.ID)
			if err != nil {
				log.Printf("  ERROR: failed to reassign edges from %s: %v", dup.ID, err)
				continue
			}
			stats.EdgesReassigned += reassigned
			stats.EdgesDeleted += deleted
			if *verbose && (reassigned > 0 || deleted > 0) {
				log.Printf("  ✓ Edges from %s: %d reassigned, %d deleted", dup.ID, reassigned, deleted)
			}
		}

		// 4c. Delete duplicate fragments
		for _, dup := range duplicates {
			if err := fragmentsRepo.Delete(ctx, dup.ID); err != nil {
				log.Printf("  ERROR: failed to delete duplicate %s: %v", dup.ID, err)
				continue
			}
			stats.FragmentsDeleted++
		}
		if *verbose {
			log.Printf("  ✓ Deleted %d duplicate fragments", len(duplicates))
		}
	}

FIX_EDGES:
	// Step 5: Fix edge directions (reverse edges that point in wrong direction)
	// IMPORTANT: This can create duplicates; we run symmetric normalization + dedup afterwards.
	if *skipEdgePass {
		log.Printf("\n=== Step 5: Fixing edge directions ===")
		log.Printf("SKIPPED (--skip-edge-pass): not modifying edges globally")
		log.Printf("\n=== Step 6: Normalizing & deduplicating edges ===")
		log.Printf("SKIPPED (--skip-edge-pass): not modifying edges globally")
		goto SUMMARY
	}
	log.Printf("\n=== Step 5: Fixing edge directions ===")
	if *apply {
		fixed, canonicalized, err := fixEdgeDirections(ctx, db, edgesRepo, fragmentTypes, *tenantID, *canonicalizeEdges)
		if err != nil {
			log.Printf("ERROR: edge direction fix failed: %v", err)
		} else {
			stats.EdgesDirectionFixed = fixed
			stats.EdgesCanonicalized = canonicalized
			log.Printf("✓ Fixed direction of %d edges", fixed)
			if *canonicalizeEdges {
				log.Printf("✓ Canonicalized %d edges", canonicalized)
			}
		}
	} else {
		log.Printf("[DRY-RUN] Would fix edge directions")
	}

	// Step 6: Normalize symmetric edges + deduplicate edges globally.
	// - Symmetric types can exist as A->B and B->A; normalize to canonical ordering to prevent future dupes.
	// - Direction fixing can create duplicates; dedup must run AFTER.
	log.Printf("\n=== Step 6: Normalizing & deduplicating edges ===")
	if *apply {
		normed, err := normalizeSymmetricEdges(ctx, db, edgesRepo, *tenantID)
		if err != nil {
			log.Printf("ERROR: symmetric edge normalization failed: %v", err)
		} else if *verbose && normed > 0 {
			log.Printf("✓ Normalized %d symmetric edges", normed)
		}

		deduped, err := deduplicateEdges(ctx, db, edgesRepo, *tenantID)
		if err != nil {
			log.Printf("ERROR: edge deduplication failed: %v", err)
		} else {
			stats.EdgesDeduplicated = deduped
			log.Printf("✓ Deduplicated %d edges", deduped)
		}
	} else {
		log.Printf("[DRY-RUN] Would normalize symmetric edges and deduplicate edges after merge")
	}

	// Print summary
SUMMARY:
	log.Printf("\n=== Summary ===")
	log.Printf("Total fragments scanned: %d", stats.TotalFragments)
	log.Printf("Duplicate clusters found: %d", stats.ClustersFound)
	log.Printf("Fragments merged: %d", stats.FragmentsMerged)
	log.Printf("Fragments deleted: %d", stats.FragmentsDeleted)
	log.Printf("GEO paths fixed: %d", stats.PathsFixed)
	log.Printf("Edges reassigned: %d", stats.EdgesReassigned)
	log.Printf("Edges deleted (duplicates/self-loops): %d", stats.EdgesDeleted)
	log.Printf("Edges deduplicated: %d", stats.EdgesDeduplicated)
	log.Printf("Edges direction fixed: %d", stats.EdgesDirectionFixed)
	log.Printf("Edges canonicalized: %d", stats.EdgesCanonicalized)
	if !*apply {
		log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
	}
}

func loadAllFragments(ctx context.Context, db *firestore.Client, tenantID, typeFilter string) ([]*models.HorizonFragment, error) {
	col := db.Collection("horizon_fragments")
	base := col.Where("tenantId", "==", tenantID)
	if typeFilter != "" {
		base = base.Where("type", "==", typeFilter)
	}
	// Pagination by DocumentID to avoid Firestore query timeouts.
	base = base.OrderBy(firestore.DocumentID, firestore.Asc)

	var fragments []*models.HorizonFragment
	loadedIDs := make(map[string]bool)
	count := 0

	lastSeen := strings.TrimSpace(*startAfterDocID)
	for {
		pageQ := base.Limit(*pageSize)
		if lastSeen != "" {
			pageQ = pageQ.StartAfter(lastSeen)
		}

		pageCtx := ctx
		var cancel context.CancelFunc
		if *pageTimeoutSec > 0 {
			pageCtx, cancel = context.WithTimeout(ctx, time.Duration(*pageTimeoutSec)*time.Second)
		}
		docs, err := getDocsWithRetry(pageCtx, pageQ, 3)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			globalLastSeen.Store(lastSeen)
			return nil, fmt.Errorf("failed to fetch fragments page (start-after=%s): %w", lastSeen, err)
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			if doc == nil {
				continue
			}
			lastSeen = doc.Ref.ID
			globalLastSeen.Store(lastSeen)

			var f models.HorizonFragment
			if err := doc.DataTo(&f); err != nil {
				log.Printf("WARNING: failed to parse fragment %s: %v", doc.Ref.ID, err)
				continue
			}
			if strings.TrimSpace(f.ID) == "" {
				f.ID = doc.Ref.ID
			}
			fragments = append(fragments, &f)
			loadedIDs[f.ID] = true
			count++

			if count%1000 == 0 {
				log.Printf("  ... loaded %d fragments", count)
			}
			if *maxFragments > 0 && count >= *maxFragments {
				globalLastSeen.Store(lastSeen)
				log.Printf("  ... reached max-fragments=%d, stopping early (resume cursor: start-after=%s)", *maxFragments, lastSeen)
				return fragments, nil
			}
		}

		if len(docs) < *pageSize {
			break
		}
	}

	// DEBUG: Check if specific known IDs were missed by the query (Firestore eventual-consistency or missing index).
	debugIDs := []string{"anz6w0uDOjDlSorQLA6S", "756btZKk76bzLQfNp7XM"}
	for _, id := range debugIDs {
		if loadedIDs[id] {
			log.Printf("  DEBUG: doc %s WAS loaded by query (OK)", id)
			continue
		}
		// Try direct doc fetch
		snap, err := col.Doc(id).Get(ctx)
		if err != nil {
			log.Printf("  DEBUG: doc %s not found via Doc().Get(): %v", id, err)
			continue
		}
		if !snap.Exists() {
			log.Printf("  DEBUG: doc %s exists=false", id)
			continue
		}
		var f models.HorizonFragment
		if err := snap.DataTo(&f); err != nil {
			log.Printf("  DEBUG: doc %s failed to parse: %v", id, err)
			continue
		}
		if f.ID == "" {
			f.ID = snap.Ref.ID
		}
		// Check why it was missed
		matchesTenant := strings.TrimSpace(f.TenantID) == tenantID
		matchesType := typeFilter == "" || string(f.Type) == typeFilter
		log.Printf("  DEBUG: doc %s exists but MISSED by query: tenantId=%q (match=%v) type=%q (match=%v)",
			id, f.TenantID, matchesTenant, f.Type, matchesType)
		// If it matches filters, add it (workaround for eventual consistency)
		if matchesTenant && matchesType {
			fragments = append(fragments, &f)
			log.Printf("  DEBUG: added %s via direct fetch (workaround)", id)
		}
	}

	return fragments, nil
}

// extractWikidataID extracts Wikidata ID from fragment's KBRefs or Identifiers
func extractWikidataID(f *models.HorizonFragment) string {
	// Check KBRefs
	for _, ref := range f.KBRefs {
		if strings.EqualFold(ref.KB, "Wikidata") || strings.EqualFold(ref.KB, "wikidata") {
			return strings.TrimSpace(ref.ExternalID)
		}
	}
	// Check Identifiers
	if wid := strings.TrimSpace(f.Identifiers["wikidataId"]); wid != "" {
		return wid
	}
	if wid := strings.TrimSpace(f.Identifiers["wikidata"]); wid != "" {
		return wid
	}
	return ""
}

// =============================================================================
// Alias/path normalization helpers (for safe, universal dedup patterns)
// =============================================================================

// extractAliasCanonicalPathsForDedup extracts canonicalPath-like aliases from identifiers that
// are known to store historical/merged canonical paths.
// We treat these as a SAFE same-type signal only (never cross-type).
func extractAliasCanonicalPathsForDedup(f *models.HorizonFragment) []string {
	if f == nil || f.Identifiers == nil {
		return nil
	}
	keys := []string{"merged_paths", "legacy_org_canonical_path", "legacy-org-canonical-path"}
	seen := map[string]struct{}{}
	var out []string
	for _, k := range keys {
		raw := strings.TrimSpace(f.Identifiers[k])
		if raw == "" {
			continue
		}
		for _, tok := range splitPathTokens(raw) {
			if !strings.HasPrefix(tok, "/") {
				continue
			}
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			out = append(out, tok)
		}
	}
	return out
}

func splitPathTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', ';', '|', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
}

// normalizeOrgCanonicalPathForDedup normalizes org canonical paths by treating '_' and '-' as equivalent.
// This is used only for grouping candidates; canonical selection is still score-based.
func normalizeOrgCanonicalPathForDedup(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/org/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// Legacy: /org/{slug} → treat as /org/na/{slug} for conservative grouping.
	if len(parts) == 2 && parts[0] == "org" {
		slug := strings.ToLower(strings.TrimSpace(parts[1]))
		if slug == "" {
			return ""
		}
		slug = strings.ReplaceAll(slug, "_", "-")
		return "/org/na/" + slug
	}
	// Canonical: /org/{iso2}/{slug}
	if len(parts) >= 3 && parts[0] == "org" {
		iso2 := extractOrgOrGovISO2ForDedupKey(cp)
		if iso2 == "" {
			iso2 = "na"
		}
		slug := strings.ToLower(strings.TrimSpace(parts[2]))
		if slug == "" {
			return ""
		}
		slug = strings.ReplaceAll(slug, "_", "-")
		return "/org/" + iso2 + "/" + slug
	}
	return ""
}

// isGovCanonicalPath returns true if path is in /gov/* namespace.
func isGovCanonicalPath(path string) bool {
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "/gov/")
}

// extractOrgOrGovISO2ForDedupKey extracts ISO2 from:
// - /org/{iso2}/{slug}
// - /gov/{iso2}/{slug}
// - legacy /org/{slug} (returns "na")
//
// Returns "na" when unknown/invalid.
func extractOrgOrGovISO2ForDedupKey(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "na"
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return "na"
	}
	ns := parts[0]
	switch ns {
	case "org", "gov":
		// Legacy: /org/{slug}
		if ns == "org" && len(parts) == 2 {
			return "na"
		}
		if len(parts) >= 3 {
			return normalizeISO2ForDedupKey(parts[1])
		}
	}
	return "na"
}

func normalizeISO2ForDedupKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "na" {
		return "na"
	}
	if len(s) == 2 && s >= "aa" && s <= "zz" {
		return s
	}
	return "na"
}

// isLowSignalOrgStubForDedup identifies low-quality ORG fragments that commonly appear as duplicates
// from lightweight extraction (e.g., label-only "softbank group" with empty status).
// We only use this as a guard for fuzzy-ish normalized-name clustering.
func isLowSignalOrgStubForDedup(f *models.HorizonFragment) bool {
	if f == nil || f.Type != models.FragmentTypeOrganization {
		return false
	}
	legalName := strings.TrimSpace(f.Identifiers["legalName"])
	shortName := strings.TrimSpace(f.Identifiers["shortName"])
	// If we have strong identifiers, treat as not-a-stub.
	if legalName != "" || shortName != "" || extractWikidataID(f) != "" {
		return false
	}
	// Lifecycle/quality signals that commonly indicate an auto-generated stub.
	lifecycleWeak := strings.TrimSpace(f.Status) == "" ||
		strings.EqualFold(strings.TrimSpace(f.Governance.Status), "DORMANT") ||
		strings.EqualFold(strings.TrimSpace(f.Governance.Status), "ARCHIVED")
	if !lifecycleWeak {
		return false
	}
	// Extra weak signal: generic/incorrect taxonomy placement
	if strings.EqualFold(strings.TrimSpace(f.SectorPath), "/Geography") {
		return true
	}
	// Fallback: label-only + weak lifecycle is already a strong stub pattern.
	return true
}

// unionFind is a small DSU helper to merge transitive alias/path links into a single cluster.
type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

func (u *unionFind) add(x string) {
	if x == "" {
		return
	}
	if _, ok := u.parent[x]; ok {
		return
	}
	u.parent[x] = x
	u.rank[x] = 0
}

func (u *unionFind) find(x string) string {
	if x == "" {
		return ""
	}
	p, ok := u.parent[x]
	if !ok {
		u.add(x)
		return x
	}
	if p == x {
		return x
	}
	u.parent[x] = u.find(p)
	return u.parent[x]
}

func (u *unionFind) union(a, b string) {
	ra := u.find(a)
	rb := u.find(b)
	if ra == "" || rb == "" || ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		u.parent[ra] = rb
		return
	}
	if u.rank[ra] > u.rank[rb] {
		u.parent[rb] = ra
		return
	}
	u.parent[rb] = ra
	u.rank[ra]++
}

func (u *unionFind) groups(ids []string) [][]string {
	buckets := make(map[string][]string)
	for _, id := range ids {
		if id == "" {
			continue
		}
		root := u.find(id)
		if root == "" {
			continue
		}
		buckets[root] = append(buckets[root], id)
	}
	out := make([][]string, 0, len(buckets))
	for _, g := range buckets {
		out = append(out, g)
	}
	return out
}

// selectCanonical chooses the best fragment to be the canonical one
// For GEO_REGION: strongly prefers fragments with /loc/ prefix
func selectCanonical(fragments []*models.HorizonFragment) string {
	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fragments[0].ID
	}

	// For GEO_REGION: first check if any have /loc/ prefix
	fragType := fragments[0].Type
	if fragType == models.FragmentTypeGeoRegion {
		for _, f := range fragments {
			if strings.HasPrefix(f.CanonicalPath, "/loc/") {
				// Found one with /loc/ - use scoring among /loc/ fragments only
				var locFragments []*models.HorizonFragment
				for _, ff := range fragments {
					if strings.HasPrefix(ff.CanonicalPath, "/loc/") {
						locFragments = append(locFragments, ff)
					}
				}
				return selectCanonicalByScore(locFragments)
			}
		}
	}

	// Default: score-based selection
	return selectCanonicalByScore(fragments)
}

// selectCanonicalByScore chooses canonical based on data quality score
func selectCanonicalByScore(fragments []*models.HorizonFragment) string {
	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fragments[0].ID
	}

	type scored struct {
		fragment *models.HorizonFragment
		score    int
	}
	var scoredFragments []scored

	for _, f := range fragments {
		score := 0

		// Prefer fragments with Wikidata ID (+100)
		if extractWikidataID(f) != "" {
			score += 100
		}
		// Prefer fragments that already have canonical industryPaths (+60).
		// This is a strong signal that the fragment was enriched/curated and should be canonical in its duplicate cluster.
		if len(f.IndustryPaths) > 0 {
			score += 60
		}
		// Extra boost when BOTH Wikidata and industryPaths are present (+40).
		// This helps pick the most "grounded" canonical when clustering by soft signals.
		if extractWikidataID(f) != "" && len(f.IndustryPaths) > 0 {
			score += 40
		}

		// Prefer proper /loc/{cc}/{admin1}/{city} format where {cc} is ISO alpha-2
		// This is the canonical form; /loc/geo/..., /loc/country/..., etc. are legacy
		if strings.HasPrefix(f.CanonicalPath, "/loc/") {
			parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
			if len(parts) >= 2 {
				// Check if second part is ISO alpha-2 country code (2 lowercase letters)
				isISO2 := len(parts[1]) == 2 && parts[1] >= "aa" && parts[1] <= "zz"
				if isISO2 && len(parts) == 4 {
					score += 100 // Canonical /loc/{cc}/{admin1}/{city} format — highest priority!
				} else if isISO2 && len(parts) >= 3 {
					score += 80 // /loc/{cc}/{admin1}
				} else if isISO2 {
					score += 60 // /loc/{cc}
				} else if len(parts) >= 4 {
					score += 30 // Legacy /loc/geo/country/usa etc.
				} else {
					score += 10 // Other /loc/ paths
				}
			}
		}

		// For PRODUCT: prefer longer/more specific paths (taxonomy paths)
		// Prefer taxonomy paths, and de-prioritize legacy org-nested paths (/product/{org}/{slug})
		// which are not part of current product taxonomy.
		if strings.HasPrefix(f.CanonicalPath, "/product/") {
			parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
			switch len(parts) {
			case 5:
				score += 100 // Canonical taxonomy: /product/{category}/{type}/{stage}/{slug}
			case 4:
				score += 80 // Older/partial taxonomy (still more structured than flat)
			case 2:
				score += 60 // Flat fallback: /product/{slug}
			case 3:
				score += 30 // Legacy org-nested: /product/{org}/{slug} (deprioritize)
			default:
				if len(parts) > 5 {
					score += 70 // Uncommon deeper paths; treat as structured but not canonical
				} else {
					score += 10
				}
			}
		}

		// For ORGANIZATION: prefer higher-quality canonical nodes over low-signal LLM stubs.
		if f.Type == models.FragmentTypeOrganization {
			// Prefer canonical /org/{iso2}/{slug} over legacy /org/{slug} and /gov/{iso2}/{slug}.
			// Rationale: /gov/* is a different namespace and frequently appears as an ingestion artifact for org-like entities.
			// We can merge data from /gov into /org, but the stable canonical for ORG should live under /org/*.
			cp := strings.TrimSpace(f.CanonicalPath)
			if strings.HasPrefix(cp, "/org/") {
				parts := strings.Split(strings.Trim(cp, "/"), "/")
				if len(parts) >= 3 {
					score += 60 // /org/{iso2}/{slug} (strong preference: stable canonical namespace)
				} else if len(parts) == 2 {
					score += 0 // legacy /org/{slug} (deprioritize; merge into ISO2 canonical if available)
				}
			} else if strings.HasPrefix(cp, "/gov/") {
				// Slight penalty — still can win if much richer, but usually should merge into /org canonical.
				score -= 5
			}

			switch strings.ToUpper(strings.TrimSpace(f.Governance.Status)) {
			case "ACTIVE":
				score += 40
			case "DORMANT":
				score += 5
			case "ARCHIVED":
				score += 0
			default:
				score += 2
			}
			if f.EntityID != nil && strings.TrimSpace(*f.EntityID) != "" {
				score += 20
			}
			if strings.TrimSpace(f.Identifiers["legalName"]) != "" {
				score += 10
			}
			if strings.TrimSpace(f.Identifiers["shortName"]) != "" {
				score += 5
			}
			// If this fragment declares alias canonical paths, it is more likely the canonical.
			if strings.TrimSpace(f.Identifiers["merged_paths"]) != "" || strings.TrimSpace(f.Identifiers["legacy_org_canonical_path"]) != "" {
				score += 8
			}
			if strings.TrimSpace(f.Status) == "" {
				score -= 3
			}
		}

		// Prefer verified status (+30)
		if f.Metadata.EpistemicStatus == models.EpistemicStatusVerified {
			score += 30
		}

		// Prefer more complete identifiers (+5 per identifier)
		score += len(f.Identifiers) * 5

		// Prefer more KBRefs (+10 per ref)
		score += len(f.KBRefs) * 10

		// Prefer older fragments (more established) (+10)
		if !f.Metadata.CreatedAt.IsZero() {
			age := time.Since(f.Metadata.CreatedAt).Hours() / 24
			if age > 7 {
				score += 10
			}
		}

		scoredFragments = append(scoredFragments, scored{f, score})
	}

	// Sort by score descending
	sort.Slice(scoredFragments, func(i, j int) bool {
		if scoredFragments[i].score != scoredFragments[j].score {
			return scoredFragments[i].score > scoredFragments[j].score
		}
		// Tie-breaker: older CreatedAt
		return scoredFragments[i].fragment.Metadata.CreatedAt.Before(scoredFragments[j].fragment.Metadata.CreatedAt)
	})

	if *verbose {
		log.Printf("    Canonical selection scores:")
		for _, sf := range scoredFragments {
			log.Printf("      %s (%s): score=%d", sf.fragment.CanonicalPath, sf.fragment.ID, sf.score)
		}
	}

	return scoredFragments[0].fragment.ID
}

// filterGeoByCountry filters GEO_REGION fragments to only include those with same country.
// If fragments have different country codes in their canonical paths, they are NOT duplicates.
// Example: /loc/us/de/dover and /loc/gb/england/dover are different cities, not duplicates.
func filterGeoByCountry(fragments []*models.HorizonFragment) []*models.HorizonFragment {
	if len(fragments) <= 1 {
		return fragments
	}

	// Group by normalized country code (handles both ISO2 and ISO3)
	byCountry := make(map[string][]*models.HorizonFragment)

	for _, f := range fragments {
		cc := extractNormalizedCountryCode(f.CanonicalPath)
		if cc != "" {
			byCountry[cc] = append(byCountry[cc], f)
		} else {
			// Unknown country — put in separate bucket to avoid false positives
			byCountry["_unknown_"+f.ID] = append(byCountry["_unknown_"+f.ID], f)
		}
	}

	// If all have the same country, return all
	if len(byCountry) == 1 {
		return fragments
	}

	// Multiple countries detected — this is likely false positive!
	// Only return the largest group (excluding unknowns)
	var largest []*models.HorizonFragment
	for cc, group := range byCountry {
		if strings.HasPrefix(cc, "_unknown_") {
			continue // Skip unknown fragments
		}
		if len(group) > len(largest) {
			largest = group
		}
	}

	// If no canonical groups found, return empty (all unknowns — can't determine duplicates safely)
	if len(largest) == 0 {
		return nil
	}

	if *verbose && len(byCountry) > 1 {
		log.Printf("    GEO country filter: split %d fragments across %d countries, keeping %d",
			len(fragments), len(byCountry), len(largest))
	}

	return largest
}

// extractNormalizedCountryCode extracts and normalizes country code from GEO path.
// Handles both ISO2 (/loc/us/...) and ISO3 (/loc/usa/...) formats.
func extractNormalizedCountryCode(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}

	cc := strings.ToLower(parts[1])
	if cc == "na" || cc == "" {
		return ""
	}

	// ISO2 code (2 letters)
	if len(cc) == 2 && cc >= "aa" && cc <= "zz" {
		return cc
	}

	// ISO3 code — convert to ISO2
	if iso2 := iso3ToIso2(cc); iso2 != "" {
		return iso2
	}

	// Other patterns (e.g., "city", "geo", "country") — check if next part is country
	if len(parts) >= 3 {
		next := strings.ToLower(parts[2])
		if len(next) == 2 && next >= "aa" && next <= "zz" {
			return next
		}
		if iso2 := iso3ToIso2(next); iso2 != "" {
			return iso2
		}
	}

	return ""
}

// iso3ToIso2 converts ISO 3166-1 alpha-3 to alpha-2 codes
var iso3ToIso2Map = map[string]string{
	"usa": "us", "gbr": "gb", "deu": "de", "fra": "fr", "jpn": "jp",
	"chn": "cn", "ind": "in", "bra": "br", "rus": "ru", "aus": "au",
	"can": "ca", "mex": "mx", "ita": "it", "esp": "es", "kor": "kr",
	"nld": "nl", "tur": "tr", "sau": "sa", "are": "ae", "isr": "il",
	"che": "ch", "swe": "se", "nor": "no", "dnk": "dk", "fin": "fi",
	"pol": "pl", "aut": "at", "bel": "be", "grc": "gr", "prt": "pt",
	"irl": "ie", "nzl": "nz", "sgp": "sg", "hkg": "hk", "twn": "tw",
	"mys": "my", "idn": "id", "tha": "th", "vnm": "vn", "phl": "ph",
	"arg": "ar", "chl": "cl", "col": "co", "per": "pe", "ven": "ve",
	"zaf": "za", "egy": "eg", "nga": "ng", "ken": "ke", "mar": "ma",
	"pak": "pk", "bgd": "bd", "ukr": "ua", "kaz": "kz", "irn": "ir",
	"irq": "iq", "kwt": "kw", "qat": "qa", "omn": "om", "bhr": "bh",
	"jor": "jo", "lbn": "lb", "syr": "sy", "yem": "ye", "afg": "af",
}

func iso3ToIso2(iso3 string) string {
	return iso3ToIso2Map[strings.ToLower(iso3)]
}

// fixGeoPath adds /loc/ prefix to geo paths that don't have it
func fixGeoPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/loc/unknown"
	}

	// Already has /loc/ prefix
	if strings.HasPrefix(path, "/loc/") {
		return path
	}

	// Remove /region/ prefix if present and add /loc/
	if strings.HasPrefix(path, "/region/") {
		return "/loc/" + strings.TrimPrefix(path, "/region/")
	}

	// Just add /loc/ prefix
	if strings.HasPrefix(path, "/") {
		return "/loc" + path
	}
	return "/loc/" + path
}

// mergeFragmentData merges data from source into target (fill-only, doesn't overwrite)
func mergeFragmentData(target, source *models.HorizonFragment) bool {
	changed := false

	// Helper: decide which MetricValue to keep for the same metric key.
	// Policy (best-effort):
	// - prefer higher confidence
	// - then prefer newer observedAt (if present)
	// - then prefer richer provenance (more sourceIds)
	// - then prefer non-empty unit
	// - finally prefer non-zero value (helps keep "real" metrics when the other is a stub)
	chooseMetric := func(cur, in models.MetricValue) (models.MetricValue, bool) {
		curUnit := ""
		inUnit := ""
		if cur.Unit != nil {
			curUnit = strings.TrimSpace(*cur.Unit)
		}
		if in.Unit != nil {
			inUnit = strings.TrimSpace(*in.Unit)
		}
		// If current is "empty", take incoming.
		if cur.Confidence <= 0 && cur.Value == 0 && curUnit == "" && len(cur.Provenance.SourceIDs) == 0 && cur.ObservedAt.IsZero() {
			return in, true
		}
		// If incoming is empty, keep current.
		if in.Confidence <= 0 && in.Value == 0 && inUnit == "" && len(in.Provenance.SourceIDs) == 0 && in.ObservedAt.IsZero() {
			return cur, false
		}

		// Confidence wins.
		if in.Confidence > cur.Confidence+1e-9 {
			return in, true
		}
		if cur.Confidence > in.Confidence+1e-9 {
			return cur, false
		}

		// More recent observation wins (if both present).
		if !cur.ObservedAt.IsZero() || !in.ObservedAt.IsZero() {
			if cur.ObservedAt.IsZero() && !in.ObservedAt.IsZero() {
				return in, true
			}
			if !cur.ObservedAt.IsZero() && in.ObservedAt.IsZero() {
				return cur, false
			}
			if in.ObservedAt.After(cur.ObservedAt) {
				return in, true
			}
			if cur.ObservedAt.After(in.ObservedAt) {
				return cur, false
			}
		}

		// Provenance density wins.
		curN := 0
		inN := 0
		if cur.Provenance.SourceIDs != nil {
			curN = len(cur.Provenance.SourceIDs)
		}
		if in.Provenance.SourceIDs != nil {
			inN = len(in.Provenance.SourceIDs)
		}
		if inN > curN {
			return in, true
		}
		if curN > inN {
			return cur, false
		}

		// Unit presence wins.
		if curUnit == "" && inUnit != "" {
			return in, true
		}

		// Non-zero value wins (very last-resort).
		if cur.Value == 0 && in.Value != 0 {
			return in, true
		}

		return cur, false
	}

	// Merge Labels (fill missing)
	if target.Labels == nil {
		target.Labels = make(map[string]string)
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Merge Identifiers (fill missing)
	if target.Identifiers == nil {
		target.Identifiers = make(map[string]string)
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// Merge KBRefs (add missing)
	existingRefs := make(map[string]bool)
	for _, ref := range target.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		existingRefs[key] = true
	}
	for _, ref := range source.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		if !existingRefs[key] {
			target.KBRefs = append(target.KBRefs, ref)
			changed = true
		}
	}

	// Merge Description (fill if empty)
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}

	// Merge Tags (union)
	tagSet := make(map[string]bool)
	for _, t := range target.Tags {
		tagSet[t] = true
	}
	for _, t := range source.Tags {
		if !tagSet[t] {
			target.Tags = append(target.Tags, t)
			tagSet[t] = true
			changed = true
		}
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

	// Merge State metrics: union keys, prefer "richer" per-key MetricValue.
	if len(source.State) > 0 {
		if target.State == nil {
			target.State = make(map[string]models.MetricValue)
		}
		for k, mv := range source.State {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			cur, ok := target.State[key]
			if !ok {
				target.State[key] = mv
				changed = true
				continue
			}
			chosen, did := chooseMetric(cur, mv)
			if did {
				target.State[key] = chosen
				changed = true
			}
		}
	}

	// Track merged canonical paths
	if strings.TrimSpace(source.CanonicalPath) != "" {
		if target.Identifiers["merged_paths"] == "" {
			target.Identifiers["merged_paths"] = source.CanonicalPath
			changed = true
		} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
			target.Identifiers["merged_paths"] += "," + source.CanonicalPath
			changed = true
		}
	}

	return changed
}

// reassignEdges moves edges from oldFragmentID to newFragmentID
func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, oldID, newID string) (reassigned, deleted int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")
	tenantID := "global"

	// Process source edges
	q1 := edgesCol.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", oldID)
	docs1, err := q1.Documents(ctx).GetAll()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range docs1 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}

		// Normalize symmetric edges so A->B and B->A are treated as the same key.
		ns, nt, _, _ := scout.NormalizeSymmetricEdge(newID, edge.TargetFragmentID, edge.Type)

		// Check if this would become a self-loop
		if scout.IsSelfLoop(ns, nt) {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Check if edge already exists
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, ns, nt, edge.Type)
		if err == nil && existing != nil {
			// Merge edge metrics/qualifiers into the existing edge before deleting to avoid losing data.
			merged := false
			if existing.Metrics == nil && edge.Metrics != nil {
				existing.Metrics = map[string]models.MetricValue{}
			}
			if edge.Metrics != nil {
				if existing.Metrics == nil {
					existing.Metrics = map[string]models.MetricValue{}
				}
				for mk, mv := range edge.Metrics {
					kk := strings.TrimSpace(mk)
					if kk == "" {
						continue
					}
					if cur, ok := existing.Metrics[kk]; ok {
						// Prefer higher confidence; then more recent observedAt.
						// (Keep conservative: if equal, don't churn.)
						if mv.Confidence > cur.Confidence+1e-9 || (!mv.ObservedAt.IsZero() && cur.ObservedAt.IsZero()) || mv.ObservedAt.After(cur.ObservedAt) {
							existing.Metrics[kk] = mv
							merged = true
						}
					} else {
						existing.Metrics[kk] = mv
						merged = true
					}
				}
			}
			if edge.Qualifiers != nil {
				if existing.Qualifiers == nil {
					existing.Qualifiers = map[string]interface{}{}
				}
				for qk, qv := range edge.Qualifiers {
					kk := strings.TrimSpace(qk)
					if kk == "" {
						continue
					}
					if _, ok := existing.Qualifiers[kk]; ok {
						continue // don't overwrite
					}
					existing.Qualifiers[kk] = qv
					merged = true
				}
			}
			// If we merged anything, update existing edge.
			if merged {
				_ = edgesRepo.Update(ctx, existing) // best-effort
			}
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Reassign
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	// Process target edges
	q2 := edgesCol.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", oldID)
	docs2, err := q2.Documents(ctx).GetAll()
	if err != nil {
		return reassigned, deleted, err
	}
	for _, d := range docs2 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}

		// Normalize symmetric edges so A->B and B->A are treated as the same key.
		ns, nt, _, _ := scout.NormalizeSymmetricEdge(edge.SourceFragmentID, newID, edge.Type)

		// Check if this would become a self-loop
		if scout.IsSelfLoop(ns, nt) {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Check if edge already exists
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, ns, nt, edge.Type)
		if err == nil && existing != nil {
			// Merge edge metrics/qualifiers into the existing edge before deleting to avoid losing data.
			merged := false
			if edge.Metrics != nil {
				if existing.Metrics == nil {
					existing.Metrics = map[string]models.MetricValue{}
				}
				for mk, mv := range edge.Metrics {
					kk := strings.TrimSpace(mk)
					if kk == "" {
						continue
					}
					if cur, ok := existing.Metrics[kk]; ok {
						if mv.Confidence > cur.Confidence+1e-9 || (!mv.ObservedAt.IsZero() && cur.ObservedAt.IsZero()) || mv.ObservedAt.After(cur.ObservedAt) {
							existing.Metrics[kk] = mv
							merged = true
						}
					} else {
						existing.Metrics[kk] = mv
						merged = true
					}
				}
			}
			if edge.Qualifiers != nil {
				if existing.Qualifiers == nil {
					existing.Qualifiers = map[string]interface{}{}
				}
				for qk, qv := range edge.Qualifiers {
					kk := strings.TrimSpace(qk)
					if kk == "" {
						continue
					}
					if _, ok := existing.Qualifiers[kk]; ok {
						continue
					}
					existing.Qualifiers[kk] = qv
					merged = true
				}
			}
			if merged {
				_ = edgesRepo.Update(ctx, existing) // best-effort
			}
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Reassign
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	return reassigned, deleted, nil
}

// mergeEdgeRich merges src edge metrics/qualifiers into dst, preferring richer MetricValues per-key.
// It returns true if dst was modified.
func mergeEdgeRich(dst, src *models.DependencyEdge) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false

	// Helper: decide which MetricValue to keep for the same metric key (richer wins).
	chooseMetric := func(cur, in models.MetricValue) (models.MetricValue, bool) {
		curUnit := ""
		inUnit := ""
		if cur.Unit != nil {
			curUnit = strings.TrimSpace(*cur.Unit)
		}
		if in.Unit != nil {
			inUnit = strings.TrimSpace(*in.Unit)
		}
		// If current is "empty", take incoming.
		if cur.Confidence <= 0 && cur.Value == 0 && curUnit == "" && len(cur.Provenance.SourceIDs) == 0 && cur.ObservedAt.IsZero() {
			return in, true
		}
		// If incoming is empty, keep current.
		if in.Confidence <= 0 && in.Value == 0 && inUnit == "" && len(in.Provenance.SourceIDs) == 0 && in.ObservedAt.IsZero() {
			return cur, false
		}

		// Confidence wins.
		if in.Confidence > cur.Confidence+1e-9 {
			return in, true
		}
		if cur.Confidence > in.Confidence+1e-9 {
			return cur, false
		}

		// More recent observation wins (if both present).
		if !cur.ObservedAt.IsZero() || !in.ObservedAt.IsZero() {
			if cur.ObservedAt.IsZero() && !in.ObservedAt.IsZero() {
				return in, true
			}
			if !cur.ObservedAt.IsZero() && in.ObservedAt.IsZero() {
				return cur, false
			}
			if in.ObservedAt.After(cur.ObservedAt) {
				return in, true
			}
			if cur.ObservedAt.After(in.ObservedAt) {
				return cur, false
			}
		}

		// Provenance density wins.
		curN := 0
		inN := 0
		if cur.Provenance.SourceIDs != nil {
			curN = len(cur.Provenance.SourceIDs)
		}
		if in.Provenance.SourceIDs != nil {
			inN = len(in.Provenance.SourceIDs)
		}
		if inN > curN {
			return in, true
		}
		if curN > inN {
			return cur, false
		}

		// Unit presence wins.
		if curUnit == "" && inUnit != "" {
			return in, true
		}

		// Non-zero value wins (very last-resort).
		if cur.Value == 0 && in.Value != 0 {
			return in, true
		}

		return cur, false
	}

	// Metrics: union keys, prefer richer per-key.
	if len(src.Metrics) > 0 {
		if dst.Metrics == nil {
			dst.Metrics = map[string]models.MetricValue{}
			changed = true
		}
		for k, mv := range src.Metrics {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			cur, ok := dst.Metrics[key]
			if !ok {
				dst.Metrics[key] = mv
				changed = true
				continue
			}
			chosen, did := chooseMetric(cur, mv)
			if did {
				dst.Metrics[key] = chosen
				changed = true
				continue
			}
			// Even if we didn't replace, try to union provenance SourceIDs to avoid losing evidence.
			if len(mv.Provenance.SourceIDs) > 0 && len(cur.Provenance.SourceIDs) > 0 {
				seen := map[string]struct{}{}
				out := make([]string, 0, len(cur.Provenance.SourceIDs)+len(mv.Provenance.SourceIDs))
				for _, s := range cur.Provenance.SourceIDs {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					if _, ok := seen[s]; ok {
						continue
					}
					seen[s] = struct{}{}
					out = append(out, s)
				}
				for _, s := range mv.Provenance.SourceIDs {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					if _, ok := seen[s]; ok {
						continue
					}
					seen[s] = struct{}{}
					out = append(out, s)
				}
				if len(out) > len(cur.Provenance.SourceIDs) {
					cur.Provenance.SourceIDs = out
					dst.Metrics[key] = cur
					changed = true
				}
			}
		}
	}

	// Qualifiers: union keys, do not overwrite.
	if len(src.Qualifiers) > 0 {
		if dst.Qualifiers == nil {
			dst.Qualifiers = map[string]interface{}{}
			changed = true
		}
		for k, v := range src.Qualifiers {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if _, ok := dst.Qualifiers[key]; ok {
				continue
			}
			dst.Qualifiers[key] = v
			changed = true
		}
	}

	return changed
}

// deduplicateEdges removes duplicate edges.
// IMPORTANT: uses symmetric-aware EdgeKey so symmetric relations are deduplicated
// even if stored as A->B and B->A.
func deduplicateEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID string) (int, error) {
	// Streamed dedup (no need to store all edges).
	// Keep the earliest-created edge for each EdgeKey; delete the rest.
	// IMPORTANT: before deleting, merge metrics/qualifiers into the kept edge to avoid losing rich data.
	const pageSize = 500
	bestByKey := make(map[string]*models.DependencyEdge) // key -> best edge
	toDelete := make([]string, 0, 1024)
	toUpdate := make(map[string]*models.DependencyEdge) // edgeID -> merged edge (kept)

	var lastDoc *firestore.DocumentSnapshot
	for {
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}
		docs, err := getDocsWithRetry(ctx, q, 3)
		if err != nil {
			return 0, err
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}
			if strings.TrimSpace(edge.ID) == "" {
				continue
			}

			key := scout.EdgeKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
			if key == "" {
				continue
			}

			best := bestByKey[key]
			if best == nil {
				// First seen
				cpy := edge
				bestByKey[key] = &cpy
				continue
			}

			// Keep earliest by CreatedAt (fallback to LastUpdatedAt when CreatedAt is zero).
			bestAt := best.Metadata.CreatedAt
			if bestAt.IsZero() {
				bestAt = best.Metadata.LastUpdatedAt
			}
			curAt := edge.Metadata.CreatedAt
			if curAt.IsZero() {
				curAt = edge.Metadata.LastUpdatedAt
			}

			if !curAt.IsZero() && (bestAt.IsZero() || curAt.Before(bestAt)) {
				// Current is better (older) → merge previous best into current, delete previous best, keep current
				if mergeEdgeRich(&edge, best) {
					cpy := edge
					bestByKey[key] = &cpy
					toUpdate[cpy.ID] = &cpy
				} else {
					cpy := edge
					bestByKey[key] = &cpy
				}
				toDelete = append(toDelete, best.ID)
			} else {
				// Current is duplicate → merge current into best, delete current
				if mergeEdgeRich(best, &edge) {
					toUpdate[best.ID] = best
				}
				toDelete = append(toDelete, edge.ID)
			}
		}

		lastDoc = docs[len(docs)-1]
		if len(docs) < pageSize {
			break
		}
	}

	// Persist merged kept edges before deletion (best-effort; failures should not block dedup).
	for _, e := range toUpdate {
		if e == nil || strings.TrimSpace(e.ID) == "" {
			continue
		}
		_ = edgesRepo.Update(ctx, e)
	}

	deleted := 0
	for _, id := range toDelete {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := edgesRepo.Delete(ctx, id); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

// normalizeSymmetricEdges rewrites edges for symmetric types into canonical orientation
// (lexicographically smaller fragment ID as source) to prevent future duplication.
func normalizeSymmetricEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID string) (int, error) {
	const pageSize = 500
	updated := 0
	totalChecked := 0
	var lastDoc *firestore.DocumentSnapshot

	for {
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}
		docs, err := getDocsWithRetry(ctx, q, 3)
		if err != nil {
			return updated, err
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}
			ns, nt, _, swapped := scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
			if !swapped {
				continue
			}
			edge.SourceFragmentID = ns
			edge.TargetFragmentID = nt
			if err := edgesRepo.Update(ctx, &edge); err == nil {
				updated++
			}
		}

		totalChecked += len(docs)
		if totalChecked%5000 == 0 && *verbose {
			log.Printf("  Symmetric normalization progress: checked=%d updated=%d", totalChecked, updated)
		}

		lastDoc = docs[len(docs)-1]
		if len(docs) < pageSize {
			break
		}
	}
	return updated, nil
}

func getEnvDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// normalizeLegalName removes common suffixes like JSC, LLC, Inc, etc.
func normalizeLegalName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))

	// Common suffixes to remove
	suffixes := []string{
		" jsc", " llc", " inc", " inc.", " ltd", " ltd.", " limited",
		" corp", " corp.", " corporation", " co", " co.", " company",
		" plc", " ag", " gmbh", " sa", " sas", " bv", " nv", " ab",
		" oy", " as", " a/s", " kft", " zrt", " sp. z o.o.", " sp.zo.o",
		" pty", " pty.", " pty ltd", " pte", " pte.", " pte ltd",
		" holding", " holdings", " group", " international",
	}

	for _, suffix := range suffixes {
		name = strings.TrimSuffix(name, suffix)
	}

	// Remove punctuation and extra spaces
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")

	return strings.TrimSpace(name)
}

// =============================================================================
// FACILITY normalized-name helpers (conservative)
// =============================================================================

// extractFacilityNameFromPath returns the last slug segment from /fac/{org}/{slug}.
func extractFacilityNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	slug := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if slug == "" || slug == "fac" || slug == "na" {
		return ""
	}
	return slug
}

// normalizeFacilityAliasKey normalizes facility names for dedupe matching:
// - lowercases
// - strips punctuation to spaces (keeps letters/digits)
// - removes stopwords (e.g., "by") to bridge small formatting differences
// - collapses whitespace
//
// IMPORTANT: Keep conservative to avoid false positives.
func normalizeFacilityAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Convert to "letters/digits/spaces only"
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}

	toks := strings.Fields(b.String())
	if len(toks) == 0 {
		return ""
	}

	stop := map[string]struct{}{
		"by": {}, "the": {}, "of": {}, "and": {}, "at": {},
		// Generic lodging noise words (kept minimal; can be extended carefully)
		"hotel": {}, "hotels": {}, "resort": {}, "inn": {}, "suites": {}, "suite": {},
	}
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if _, ok := stop[t]; ok {
			continue
		}
		out = append(out, t)
	}

	// Safety: require a minimally descriptive key.
	if len(out) < 3 {
		return ""
	}
	return strings.Join(out, " ")
}

// extractCityFromPath extracts the city name from a canonical path
// e.g., "/loc/kz/astana/astana" -> "astana"
// e.g., "/loc/us/tx/austin" -> "austin"
// e.g., "/region/astana" -> "astana"
func extractCityFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}

	// For /loc/ paths: validate format /loc/{cc}/{admin1}/{city}
	if parts[0] == "loc" {
		if len(parts) < 2 {
			return ""
		}
		cc := parts[1]
		// Must be ISO alpha-2 (2 lowercase letters), but NOT "na" (placeholder)
		if len(cc) != 2 || cc < "aa" || cc > "zz" || cc == "na" {
			return "" // Malformed path like /loc/grapevine/tx/us or /loc/na/us/nv — skip
		}
		// ONLY index 4-part paths: /loc/{cc}/{admin1}/{city}
		// Skip 5+ part paths — they are sub-regions (e.g., /loc/cn/gd/shenzhen/futian)
		if len(parts) != 4 {
			return "" // Not canonical 4-part format
		}
		// City is the 4th part (index 3) if it's not "na"
		city := parts[3]
		if city != "na" && city != "" {
			return strings.ToLower(city)
		}
		return ""
	}

	// Legacy /region/ paths: get last non-"na" part
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.ToLower(parts[i])
		if part != "na" && part != "" && part != "loc" && part != "region" {
			return part
		}
	}

	return ""
}

// extractProductNameFromPath extracts product name from canonical path.
// Examples:
//
//	/product/lithium → "lithium"
//	/product/raw/mineral/extraction/lithium → "lithium"
//	/product/electronics/semiconductor/gpu → "gpu"
func extractProductNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return ""
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}

	// Get last part as product name
	lastPart := strings.ToLower(parts[len(parts)-1])
	if lastPart == "" || lastPart == "product" {
		return ""
	}

	return lastPart
}

// extractCanonicalProductSlugForDedup returns the product slug ONLY for canonical product namespaces:
// - /product/{slug}
// - /product/{category}/{type}/{stage}/{slug}
//
// Returns "" for legacy org-nested paths like /product/{org}/{slug} to avoid false-positive merges.
func extractCanonicalProductSlugForDedup(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "product" {
		return ""
	}
	if len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	if len(parts) == 5 {
		return strings.ToLower(strings.TrimSpace(parts[4]))
	}
	return ""
}

// normalizeProductAliasKey normalizes product name aliases for dedupe matching.
// - lowercases
// - removes punctuation (keeps letters/digits/spaces)
// - collapses whitespace
//
// Example:
//
//	"F-16 Fighting Falcon" -> "f 16 fighting falcon"
//	"M1 Abrams" -> "m1 abrams"
func normalizeProductAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	var prevClass string // "letter" | "digit" | "other" | ""
	for _, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') // latin + basic cyrillic
		isDigit := r >= '0' && r <= '9'
		if isLetter || isDigit {
			curClass := "letter"
			if isDigit {
				curClass = "digit"
			}
			// Split letter<->digit boundaries to unify "F16" and "F-16" into "f 16".
			if (prevClass == "letter" && curClass == "digit") || (prevClass == "digit" && curClass == "letter") {
				if !prevSpace {
					b.WriteByte(' ')
				}
				prevSpace = true
			}
			b.WriteRune(r)
			prevSpace = false
			prevClass = curClass
			continue
		}
		// treat everything else as separator
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
		prevClass = "other"
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	return strings.TrimSpace(out)
}

func getDocsWithRetry(ctx context.Context, q firestore.Query, attempts int) ([]*firestore.DocumentSnapshot, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		docs, err := q.Documents(ctx).GetAll()
		if err == nil {
			return docs, nil
		}
		lastErr = err
		// Simple backoff; Firestore may return Unavailable/timeout on large scans.
		time.Sleep(time.Duration(300*(i+1)) * time.Millisecond)
	}
	return nil, lastErr
}

// isSpecificProductAliasKey is a safety gate for PRODUCT alias deduplication:
// require at least one digit and at least 2 tokens, and at least one letter.
func isSpecificProductAliasKey(aliasKey string) bool {
	aliasKey = normalizeProductAliasKey(aliasKey)
	if aliasKey == "" {
		return false
	}
	if len(aliasKey) < 5 {
		return false
	}
	hasDigit := false
	hasLetter := false
	for _, r := range aliasKey {
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
		if (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') {
			hasLetter = true
		}
	}
	if !hasDigit || !hasLetter {
		return false
	}
	if len(strings.Fields(aliasKey)) < 2 {
		return false
	}
	return true
}

// normalizePhoneticKey normalizes Arabic/transliterated names for phonetic matching.
// Handles common transliteration variations:
//   - ou/oo/uu/u → o (Tahnoun/Tahnoon/Tahnun → tahno...)
//   - ee/e/i/y → i (Mohammed/Muhammad → mohamid, Yasser/Yasir → yasir)
//   - kh → h (Khalid → halid)
//   - doubled consonants → single (Mohammed → mohamed)
//   - removes non-letter characters
//   - removes common Arabic particles (al/el/bin/bint/ibn/ben/abu/banu) when enough tokens remain
//
// Example:
//
//	"Tahnoon bin Zayed Al Nahyan" → "tahon bin zaid al nahian"
//	"Tahnoun bin Zayed Al Nahyan" → "tahon bin zaid al nahian"
func normalizePhoneticKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Step 1: Replace common transliteration variations
	replacements := []struct{ from, to string }{
		{"oo", "o"},
		{"ou", "o"},
		{"uu", "o"},
		{"ee", "i"},
		{"ie", "i"},
		{"ei", "i"},
		{"y", "i"},
		{"e", "i"},
		{"u", "o"},
		{"kh", "h"},
		{"ph", "f"},
		{"qu", "k"},
		{"aa", "a"},
	}
	for _, r := range replacements {
		name = strings.ReplaceAll(name, r.from, r.to)
	}

	// Step 2: Remove non-letters, keep spaces
	var b strings.Builder
	b.Grow(len(name))
	var prevChar rune = ' '
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			// Remove doubled consonants (except vowels)
			vowels := "aeiou"
			if r == prevChar && !strings.ContainsRune(vowels, r) {
				continue
			}
			b.WriteRune(r)
			prevChar = r
		} else if r == ' ' && prevChar != ' ' {
			b.WriteRune(r)
			prevChar = ' '
		}
	}

	result := strings.TrimSpace(b.String())

	// Safety: require at least 3 tokens to avoid overly generic matches
	tokens := strings.Fields(result)
	if len(tokens) < 3 {
		return ""
	}

	// Step 3: Remove common Arabic particles if we still have >=3 tokens
	particles := map[string]struct{}{
		"al": {}, "el": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abu": {}, "banu": {},
	}
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := particles[t]; ok {
			continue
		}
		filtered = append(filtered, t)
	}
	if len(filtered) >= 3 {
		return strings.Join(filtered, " ")
	}

	return result
}

// extractPersonNameFromPath extracts the person name from a canonical path.
// Example: /person/tahnoon-bin-zayed-al-nahyan → "tahnoon-bin-zayed-al-nahyan"
func extractPersonNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/person/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

// fixEdgeDirections reverses edges that point in the wrong direction
// Uses pagination (100 edges per page) and pre-built fragment type cache for efficiency
func fixEdgeDirections(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, fragmentTypes map[string]models.FragmentType, tenantID string, canonicalizeTypes bool) (int, int, error) {
	log.Printf("Fixing edge directions with pagination (page size: 100)...")
	log.Printf("Edge canonicalization enabled: %t", canonicalizeTypes)
	log.Printf("Using pre-loaded fragment type cache with %d entries", len(fragmentTypes))

	const pageSize = 100
	reversed := 0
	canonicalized := 0
	totalChecked := 0
	var lastDoc *firestore.DocumentSnapshot

	// Best-effort type resolver: when horizon_smart_dedup is run with --type filter,
	// fragmentTypes cache can be incomplete, but edge direction fixing must remain global.
	typeCache := fragmentTypes
	if typeCache == nil {
		typeCache = make(map[string]models.FragmentType)
	}
	resolveType := func(id string) models.FragmentType {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		if t, ok := typeCache[id]; ok && t != "" {
			return t
		}
		snap, err := db.Collection("horizon_fragments").Doc(id).Get(ctx)
		if err != nil || snap == nil || !snap.Exists() {
			return ""
		}
		if v, ok := snap.Data()["type"].(string); ok {
			t := models.FragmentType(strings.TrimSpace(v))
			if t != "" {
				typeCache[id] = t
				return t
			}
		}
		return ""
	}

	for {
		// Build paginated query
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}

		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return reversed, canonicalized, fmt.Errorf("failed to get edges page: %w", err)
		}

		if len(docs) == 0 {
			break // No more pages
		}

		// Process this page
		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}

			sourceType := resolveType(edge.SourceFragmentID)
			targetType := resolveType(edge.TargetFragmentID)

			if sourceType == "" || targetType == "" {
				continue // Skip if we can't determine types (orphaned edge or deleted fragment)
			}

			// Canonical representation: SUBSIDIARY_OF is normalized into OWNS (parent -> subsidiary).
			if edge.Type == models.DependencyTypeSubsidiaryOf {
				oldSource := edge.SourceFragmentID            // subsidiary
				edge.SourceFragmentID = edge.TargetFragmentID // parent
				edge.TargetFragmentID = oldSource
				edge.Type = models.DependencyTypeOwns
				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to normalize SUBSIDIARY_OF edge %s: %v", edge.ID, err)
					continue
				}
				canonicalized++
				continue
			}

			// Canonical representation: HAS_KEY_PERSON is normalized into KEY_PERSON (person -> org).
			if edge.Type == models.DependencyTypeHasKeyPerson {
				// Historical variants:
				// - ORG -> PERSON (inverse)
				// - PERSON -> ORG (already correct but legacy type)
				//
				// Canonical: KEY_PERSON with PERSON -> ORG.
				if sourceType == models.FragmentTypeOrganization && targetType == models.FragmentTypePerson {
					oldSource := edge.SourceFragmentID            // org
					edge.SourceFragmentID = edge.TargetFragmentID // person
					edge.TargetFragmentID = oldSource
					// Swap cached endpoint types as well
					sourceType, targetType = targetType, sourceType
				}
				edge.Type = models.DependencyTypeKeyPerson
				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to normalize HAS_KEY_PERSON edge %s: %v", edge.ID, err)
					continue
				}
				if *verbose {
					log.Printf("  ✓ Normalized HAS_KEY_PERSON → KEY_PERSON: %s → %s", edge.SourceFragmentID, edge.TargetFragmentID)
				}
				canonicalized++
				continue
			}

			// Canonical representation: WORKS_FOR is legacy; normalize to EMPLOYED_BY.
			if edge.Type == models.DependencyTypeWorksFor {
				edge.Type = models.DependencyTypeEmployedBy
				canonicalized++
				// Keep processing below: direction might still need to be reversed.
			}

			// Canonicalize certain mis-modeled edges where reversing is not sufficient.
			// Example: PRODUCT OWNS ORGANIZATION -> ORGANIZATION PRODUCES PRODUCT (not ORG OWNS PRODUCT).
			if canonicalizeTypes {
				if newType, shouldSwap, changed, reason := scout.CanonicalizeEdge(edge.Type, sourceType, targetType); changed {
					oldType := edge.Type
					edge.Type = newType
					if shouldSwap {
						oldSource := edge.SourceFragmentID
						edge.SourceFragmentID = edge.TargetFragmentID
						edge.TargetFragmentID = oldSource
						sourceType, targetType = targetType, sourceType
					}
					// Re-apply symmetric normalization after swap/type-change (noop for non-symmetric).
					edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)

					if edge.Qualifiers == nil {
						edge.Qualifiers = map[string]interface{}{}
					}
					edge.Qualifiers["canonicalized"] = true
					edge.Qualifiers["canonicalizedFromEdgeType"] = string(oldType)
					edge.Qualifiers["canonicalizationReason"] = reason
					edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
					if shouldSwap {
						edge.Qualifiers["normalizedDirection"] = true
					}

					if err := edgesRepo.Update(ctx, &edge); err != nil {
						log.Printf("  ERROR: failed to canonicalize edge %s: %v", edge.ID, err)
						continue
					}
					if *verbose {
						log.Printf("  ✓ Canonicalized edge %s: %s -> %s [%s -> %s]",
							edge.ID, oldType, edge.Type, edge.TargetFragmentID, edge.SourceFragmentID)
					}
					canonicalized++
					continue
				}
			}

			if scout.ShouldReverseEdge(edge.Type, sourceType, targetType) {
				// Reverse the edge
				oldSource := edge.SourceFragmentID
				edge.SourceFragmentID = edge.TargetFragmentID
				edge.TargetFragmentID = oldSource
				// Swap cached types
				sourceType, targetType = targetType, sourceType

				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to reverse edge %s: %v", edge.ID, err)
					continue
				}

				if *verbose {
					log.Printf("  ✓ Reversed edge %s: %s (%s) -> %s (%s) [%s]",
						edge.ID, edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Type)
				}
				reversed++
			}
		}

		totalChecked += len(docs)
		if totalChecked%1000 == 0 || len(docs) < pageSize {
			log.Printf("  Progress: checked %d edges, reversed %d, canonicalized %d so far...", totalChecked, reversed, canonicalized)
		}

		lastDoc = docs[len(docs)-1]

		if len(docs) < pageSize {
			break // Last page
		}
	}

	log.Printf("  Completed: checked %d edges total", totalChecked)
	return reversed, canonicalized, nil
}
