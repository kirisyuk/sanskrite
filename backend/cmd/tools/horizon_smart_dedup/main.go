package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

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
	projectID       = flag.String("project", "", "GCP project ID (required)")
	apply           = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun          = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")
	tenantID        = flag.String("tenant", "global", "Tenant ID (default: global)")
	verbose         = flag.Bool("verbose", false, "Verbose logging")
	typeFilter      = flag.String("type", "", "Filter by fragment type (GEO_REGION, ORGANIZATION, FACILITY, etc.)")
	skipVertexSync  = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync (faster, run bulk sync separately)")
	enableProductAlias = flag.Bool("enable-product-alias", false, "Enable PRODUCT alias clustering (model/SKU heuristic). Default OFF: better duplicates than false-positive merges.")
)

// DuplicateCluster represents a group of fragments that are duplicates of each other
type DuplicateCluster struct {
	MatchType   string                    // "wikidata", "shortName", "legalName", "labelEN", "normalizedLegal", "geoCity"
	MatchValue  string                    // The value that matched
	Fragments   []*models.HorizonFragment // All fragments in this cluster
	CanonicalID string                    // ID of the fragment to keep
}

type Stats struct {
	TotalFragments     int
	ClustersFound      int
	FragmentsMerged    int
	FragmentsDeleted   int
	PathsFixed         int
	EdgesReassigned    int
	EdgesDeleted       int
	EdgesDeduplicated  int
	EdgesDirectionFixed int
}

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()

	log.Printf("=== Horizon Smart Deduplication Tool ===")
	log.Printf("project=%s tenant=%s apply=%v typeFilter=%q", *projectID, *tenantID, *apply, *typeFilter)

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
		log.Fatalf("FATAL: failed to load fragments: %v", err)
	}
	stats.TotalFragments = len(fragments)
	log.Printf("✓ Loaded %d fragments", len(fragments))

	// Step 2: Build indexes for duplicate detection
	log.Printf("\n=== Step 2: Building indexes ===")
	wikidataIndex := make(map[string][]*models.HorizonFragment)        // wikidataID -> fragments
	shortNameIndex := make(map[string][]*models.HorizonFragment)       // type:shortName -> fragments
	legalNameIndex := make(map[string][]*models.HorizonFragment)       // type:legalName -> fragments
	labelENIndex := make(map[string][]*models.HorizonFragment)         // type:label_en -> fragments (Tier 2)
	normalizedLegalIndex := make(map[string][]*models.HorizonFragment) // type:normalized_legal -> fragments
	orgNormalizedNameIndex := make(map[string][]*models.HorizonFragment) // type:normalized(label/legal) -> org fragments (LOW-signal stub rescue)
	geoCityIndex := make(map[string][]*models.HorizonFragment)         // city_name -> geo fragments
	productNameIndex := make(map[string][]*models.HorizonFragment)     // product_name -> product fragments
	productAliasIndex := make(map[string][]*models.HorizonFragment)    // product_alias_key -> product fragments (label/short/legal + legacy slug aliases)

	for _, f := range fragments {
		// Index by Wikidata ID
		if wikidataID := extractWikidataID(f); wikidataID != "" {
			wikidataIndex[wikidataID] = append(wikidataIndex[wikidataID], f)
		}

		// Index by shortName (type-scoped)
		if shortName := strings.TrimSpace(f.Identifiers["shortName"]); shortName != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(shortName))
			shortNameIndex[key] = append(shortNameIndex[key], f)
		}

		// Index by legalName (type-scoped) — Tier 1 (100% confidence for orgs)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(legalName))
			legalNameIndex[key] = append(legalNameIndex[key], f)
		}

		// Index by label[en] (type-scoped) — Tier 2 (high confidence)
		if labelEN := strings.TrimSpace(f.Labels["en"]); labelEN != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(labelEN))
			labelENIndex[key] = append(labelENIndex[key], f)
		}

		// Index by normalized legalName (removes JSC, LLC, Inc, etc.)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			normalized := normalizeLegalName(legalName)
			if normalized != "" {
				key := fmt.Sprintf("%s:%s", f.Type, normalized)
				normalizedLegalIndex[key] = append(normalizedLegalIndex[key], f)
			}
		}

		// ORG-only: index normalized label/legal for LOW-signal stub rescue (e.g., "softbank group" → "softbank")
		// Safety is enforced when creating clusters (must include at least one high-signal org + one low-signal stub).
		if f.Type == models.FragmentTypeOrganization {
			labelEN := strings.TrimSpace(f.Labels["en"])
			legalName := strings.TrimSpace(f.Identifiers["legalName"])
			seed := legalName
			if seed == "" {
				seed = labelEN
			}
			if seed != "" {
				n := normalizeLegalName(seed)
				if n != "" {
					key := fmt.Sprintf("%s:%s", f.Type, n)
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

	log.Printf("✓ Wikidata index: %d unique IDs", len(wikidataIndex))
	log.Printf("✓ ShortName index: %d unique keys", len(shortNameIndex))
	log.Printf("✓ LegalName index: %d unique keys", len(legalNameIndex))
	log.Printf("✓ Label[en] index: %d unique keys", len(labelENIndex))
	log.Printf("✓ Normalized legal index: %d unique keys", len(normalizedLegalIndex))
	log.Printf("✓ ORG normalized name index: %d unique keys", len(orgNormalizedNameIndex))
	log.Printf("✓ GEO city index: %d unique cities", len(geoCityIndex))
	log.Printf("✓ GEO by path index: %d unique paths", len(geoByPath))
	log.Printf("✓ CanonicalPath index: %d unique keys", len(byCanonicalPath))
	log.Printf("✓ PRODUCT name index: %d unique names", len(productNameIndex))
	log.Printf("✓ PRODUCT alias index: %d unique aliases", len(productAliasIndex))

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
	log.Printf("  Found %d aliasPath-based clusters", aliasClusters)

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

	stats.ClustersFound = len(clusters)
	log.Printf("✓ Total duplicate clusters: %d", len(clusters))

	if len(clusters) == 0 {
		log.Printf("\nNo duplicates found!")
		return
	}

	// Step 4: Process each cluster
	log.Printf("\n=== Step 4: Processing clusters ===")
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

	// Step 5: Fix edge directions (reverse edges that point in wrong direction)
	// IMPORTANT: This can create duplicates; we run symmetric normalization + dedup afterwards.
	log.Printf("\n=== Step 5: Fixing edge directions ===")
	if *apply {
		fixed, err := fixEdgeDirections(ctx, db, edgesRepo, fragmentTypes, *tenantID)
		if err != nil {
			log.Printf("ERROR: edge direction fix failed: %v", err)
		} else {
			stats.EdgesDirectionFixed = fixed
			log.Printf("✓ Fixed/normalized direction of %d edges", fixed)
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
	if !*apply {
		log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
	}
}

func loadAllFragments(ctx context.Context, db *firestore.Client, tenantID, typeFilter string) ([]*models.HorizonFragment, error) {
	q := db.Collection("horizon_fragments").Where("tenantId", "==", tenantID)

	if typeFilter != "" {
		q = q.Where("type", "==", typeFilter)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var fragments []*models.HorizonFragment
	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate fragments: %w", err)
		}

		var f models.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			log.Printf("WARNING: failed to parse fragment %s: %v", doc.Ref.ID, err)
			continue
		}
		if strings.TrimSpace(f.ID) == "" {
			f.ID = doc.Ref.ID
		}
		fragments = append(fragments, &f)
		count++

		if count%1000 == 0 {
			log.Printf("  ... loaded %d fragments", count)
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

// normalizeOrgCanonicalPathForDedup normalizes /org/{slug} by treating '_' and '-' as equivalent.
// This is used only for grouping candidates; canonical selection is still score-based.
func normalizeOrgCanonicalPathForDedup(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/org/") {
		return ""
	}
	slug := strings.TrimSpace(strings.TrimPrefix(cp, "/org/"))
	if slug == "" {
		return ""
	}
	slug = strings.ToLower(slug)
	slug = strings.ReplaceAll(slug, "_", "-")
	return "/org/" + slug
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

// extractCountryCodeFromPath extracts ISO alpha-2 country code from /loc/{cc}/... path.
// Returns empty string if path is not in canonical format or country code is not ISO alpha-2.
func extractCountryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "loc" {
		cc := parts[1]
		// Check if it's a valid ISO alpha-2 code (2 lowercase letters)
		if len(cc) == 2 && cc >= "aa" && cc <= "zz" {
			return cc
		}
	}
	return ""
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

	// Track merged canonical paths
	if target.Identifiers["merged_paths"] == "" {
		target.Identifiers["merged_paths"] = source.CanonicalPath
	} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
		target.Identifiers["merged_paths"] += "," + source.CanonicalPath
	}
	changed = true

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

// deduplicateEdges removes duplicate edges.
// IMPORTANT: uses symmetric-aware EdgeKey so symmetric relations are deduplicated
// even if stored as A->B and B->A.
func deduplicateEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID string) (int, error) {
	// Streamed dedup (no need to store all edges).
	// Keep the earliest-created edge for each EdgeKey; delete the rest.
	const pageSize = 500
	bestByKey := make(map[string]*models.DependencyEdge) // key -> best edge
	toDelete := make([]string, 0, 1024)

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
				// Current is better (older) → delete previous best, keep current
				toDelete = append(toDelete, best.ID)
				cpy := edge
				bestByKey[key] = &cpy
			} else {
				// Current is duplicate → delete current
				toDelete = append(toDelete, edge.ID)
			}
		}

		lastDoc = docs[len(docs)-1]
		if len(docs) < pageSize {
			break
		}
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
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	
	// Remove punctuation and extra spaces
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	
	return strings.TrimSpace(name)
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
//   /product/lithium → "lithium"
//   /product/raw/mineral/extraction/lithium → "lithium"
//   /product/electronics/semiconductor/gpu → "gpu"
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
//   "F-16 Fighting Falcon" -> "f 16 fighting falcon"
//   "M1 Abrams" -> "m1 abrams"
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

// fixEdgeDirections reverses edges that point in the wrong direction
// Uses pagination (100 edges per page) and pre-built fragment type cache for efficiency
func fixEdgeDirections(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, fragmentTypes map[string]models.FragmentType, tenantID string) (int, error) {
	log.Printf("Fixing edge directions with pagination (page size: 100)...")
	log.Printf("Using pre-loaded fragment type cache with %d entries", len(fragmentTypes))

	const pageSize = 100
	fixed := 0
	totalChecked := 0
	var lastDoc *firestore.DocumentSnapshot

	for {
		// Build paginated query
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}

		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return fixed, fmt.Errorf("failed to get edges page: %w", err)
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

			sourceType := fragmentTypes[edge.SourceFragmentID]
			targetType := fragmentTypes[edge.TargetFragmentID]

			if sourceType == "" || targetType == "" {
				continue // Skip if we can't determine types (orphaned edge or deleted fragment)
			}

			// Canonical representation: SUBSIDIARY_OF is normalized into OWNS (parent -> subsidiary).
			if edge.Type == models.DependencyTypeSubsidiaryOf {
				oldSource := edge.SourceFragmentID // subsidiary
				edge.SourceFragmentID = edge.TargetFragmentID // parent
				edge.TargetFragmentID = oldSource
				edge.Type = models.DependencyTypeOwns
				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to normalize SUBSIDIARY_OF edge %s: %v", edge.ID, err)
					continue
				}
				fixed++
				continue
			}

			if scout.ShouldReverseEdge(edge.Type, sourceType, targetType) {
				// Reverse the edge
				oldSource := edge.SourceFragmentID
				edge.SourceFragmentID = edge.TargetFragmentID
				edge.TargetFragmentID = oldSource

				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to reverse edge %s: %v", edge.ID, err)
					continue
				}

				if *verbose {
					log.Printf("  ✓ Reversed edge %s: %s (%s) -> %s (%s) [%s]",
						edge.ID, edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Type)
				}
				fixed++
			}
		}

		totalChecked += len(docs)
		if totalChecked%1000 == 0 || len(docs) < pageSize {
			log.Printf("  Progress: checked %d edges, fixed %d so far...", totalChecked, fixed)
		}

		lastDoc = docs[len(docs)-1]

		if len(docs) < pageSize {
			break // Last page
		}
	}

	log.Printf("  Completed: checked %d edges total", totalChecked)
	return fixed, nil
}

