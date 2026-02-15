package main

import (
	"context"
	"errors"
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
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	projectID                 = flag.String("project", "", "GCP project ID (required)")
	apply                     = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun                    = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")
	tenantID                  = flag.String("tenant", "global", "Tenant ID (default: global)")
	verbose                   = flag.Bool("verbose", false, "Verbose logging")
	typeFilter                = flag.String("type", "", "Filter by fragment type (GEO_REGION, ORGANIZATION, FACILITY, etc.)")
	maxFragments              = flag.Int("max-fragments", 0, "Stop after loading this many fragments (0 = no limit)")
	unionClusters             = flag.Bool("union-clusters", true, "Build transitive clusters via union-find across signals (recommended; helps catch chain duplicates). Set false to skip aliasPath-based transitive clustering.")
	skipVertexSync            = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync (faster, run bulk sync separately)")
	skipEdgePass              = flag.Bool("skip-edge-pass", false, "Skip global edge direction fix + symmetric normalization + edge dedup pass (safer for targeted fragment-only dedup runs)")
	pageSize                  = flag.Int("page-size", 500, "Firestore page size for loading fragments (pagination). Lower = safer, higher = fewer round-trips")
	startAfterDocID           = flag.String("start-after", "", "Resume cursor: start loading fragments after this Firestore document ID (__name__)")
	pageTimeoutSec            = flag.Int("page-timeout-s", 90, "Timeout in seconds for each Firestore page fetch while loading fragments (0 disables)")
	enableProductAlias        = flag.Bool("enable-product-alias", true, "Enable PRODUCT alias clustering (model/SKU heuristic). Default ON (requested for product-heavy dedup runs).")
	enableOrgNameOnly         = flag.Bool("enable-org-name-only", false, "Enable ORGANIZATION name-only clustering (shortName/label[en]). Default OFF (too many false merges).")
	enableOrgFuzzyName        = flag.Bool("enable-org-fuzzy-name", false, "Enable ORGANIZATION fuzzy-ish name clustering (normalized legalName + stub-rescue normalized label/legal). Default OFF.")
	skipOrgLegacyCountryCheck = flag.Bool("skip-org-legacy-country-check", false, "Skip LOCATED_IN country check for legacy /org/{company} → /org/{geo}/{company}-{legalForm} (faster, higher risk).")
	fixEdgesOnly              = flag.Bool("fix-edges-only", false, "Only fix/normalize edges (direction + symmetric normalization + dedup). Skips fragment dedup/merge even if duplicates are found.")
	canonicalizeEdges         = flag.Bool("edge-canonicalize", true, "Rewrite mis-modeled edges into canonical types (e.g., MARKET OWNS MARKET -> HAS_SUBMARKET, ORG DEPENDS_ON MARKET -> PARTICIPATES_IN).")
	skipGeoNames              = flag.Bool("skip-geonames", false, "Skip GeoNames-based /loc normalization (enabled by default).")
	marketCleanupOnly         = flag.Bool("market-cleanup-only", false, "Only run Step 7 (market geo-mismatch cleanup + domain normalization). Skips all fragment dedup and edge fixing.")
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
	TotalFragments           int
	ClustersFound            int
	FragmentsMerged          int
	FragmentsDeleted         int
	PathsFixed               int
	EdgesReassigned          int
	EdgesDeleted             int
	EdgesDeduplicated        int
	EdgesDirectionFixed      int
	EdgesCanonicalized       int
	MarketGeoMismatchCleaned int
	MarketDomainNormalized   int
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

	// Skip name-based ORG merges when namespaces mix (e.g., /org vs /gov).
	orgCount := 0
	govCount := 0
	for _, f := range frags {
		if f == nil {
			continue
		}
		switch canonicalNamespace(f) {
		case "org":
			orgCount++
		case "gov":
			govCount++
		}
	}
	if orgCount > 0 && govCount > 0 {
		return true, "mixed_org_gov_namespaces"
	}

	// Skip name-based ORG merges when legal forms conflict (semantic distinction).
	legalForms := map[string]struct{}{}
	for _, f := range frags {
		lf := extractOrgLegalForm(f)
		if lf == "" {
			continue
		}
		legalForms[lf] = struct{}{}
	}
	if len(legalForms) > 1 {
		return true, fmt.Sprintf("conflicting_legal_forms=%v", keysSorted(legalForms))
	}

	return false, ""
}

// orgLegacyNoGeoNameKey builds a normalized name key for legacy /org/* (no geo) dedup.
// We only use the first available strong name signal to avoid merging unrelated entities.
func orgLegacyNoGeoNameKey(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	name := strings.TrimSpace(f.Identifiers["legalName"])
	if name == "" {
		name = strings.TrimSpace(f.Identifiers["shortName"])
	}
	if name == "" {
		name = strings.TrimSpace(f.Labels["en"])
	}
	if name == "" {
		return ""
	}
	return normalizeLegalName(name)
}

// orgLegacyNoGeoNamesConsistent ensures all non-empty name fields normalize to a single value.
func orgLegacyNoGeoNamesConsistent(frags []*models.HorizonFragment) bool {
	if len(frags) == 0 {
		return false
	}
	type getter func(*models.HorizonFragment) string
	fields := []getter{
		func(f *models.HorizonFragment) string { return f.Identifiers["legalName"] },
		func(f *models.HorizonFragment) string { return f.Identifiers["shortName"] },
		func(f *models.HorizonFragment) string { return f.Labels["en"] },
	}
	hasAny := false
	for _, get := range fields {
		values := map[string]struct{}{}
		for _, f := range frags {
			if f == nil {
				continue
			}
			v := normalizeLegalName(get(f))
			if v == "" {
				continue
			}
			values[v] = struct{}{}
			if len(values) > 1 {
				return false
			}
		}
		if len(values) == 1 {
			hasAny = true
		}
	}
	return hasAny
}

// shouldSkipOrgLegacyNoGeoCluster applies conservative guards for legacy /org/* (no geo) merges.
func shouldSkipOrgLegacyNoGeoCluster(frags []*models.HorizonFragment) (bool, string) {
	if conflict, ids := orgConflictingWikidataIDs(frags); conflict {
		return true, fmt.Sprintf("conflicting_wikidata_ids=%v", ids)
	}

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
		return true, fmt.Sprintf("groupish_mismatch(groupish=%d nonGroupish=%d)", groupish, nonGroupish)
	}

	return false, ""
}

func canonicalPartsFromPath(path string) *taxonomy.CanonicalPathParts {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil {
		return nil
	}
	return parts
}

func canonicalPartsForFragment(f *models.HorizonFragment) *taxonomy.CanonicalPathParts {
	if f == nil {
		return nil
	}
	if f.CanonicalParts != nil {
		return f.CanonicalParts
	}
	return canonicalPartsFromPath(f.CanonicalPath)
}

func canonicalNamespace(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if parts := canonicalPartsForFragment(f); parts != nil {
		return strings.ToLower(strings.TrimSpace(parts.Namespace))
	}
	p := strings.TrimSpace(f.CanonicalPath)
	if p == "" || !strings.HasPrefix(p, "/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func canonicalNamespaceFamilyFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if parts := canonicalPartsFromPath(path); parts != nil {
		ns := strings.ToLower(strings.TrimSpace(parts.Namespace))
		switch ns {
		case "gov":
			return "org"
		case "region":
			return "loc"
		default:
			return ns
		}
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	ns := strings.ToLower(strings.TrimSpace(parts[0]))
	switch ns {
	case "gov":
		return "org"
	case "region":
		return "loc"
	default:
		return ns
	}
}

func expectedNamespaceFamilyForType(t models.FragmentType) string {
	prefix := strings.TrimSpace(policy.ExpectedCanonicalPathPrefix(t))
	if prefix == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(prefix, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func fragmentNamespaceFamilyForDedup(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if fam := canonicalNamespaceFamilyFromPath(f.CanonicalPath); fam != "" {
		return fam
	}
	return expectedNamespaceFamilyForType(f.Type)
}

func isTypePathCompatibleForDedup(f *models.HorizonFragment) bool {
	if f == nil {
		return false
	}
	expected := expectedNamespaceFamilyForType(f.Type)
	if expected == "" {
		return true
	}
	actual := canonicalNamespaceFamilyFromPath(f.CanonicalPath)
	if actual == "" {
		return true
	}
	return actual == expected
}

func formatFamilyCounts(families map[string][]*models.HorizonFragment) string {
	if len(families) == 0 {
		return ""
	}
	parts := make([]string, 0, len(families))
	for fam, frs := range families {
		name := fam
		if strings.TrimSpace(name) == "" {
			name = "unknown"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", name, len(frs)))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// filterClusterByNamespaceFamily enforces same canonical namespace family inside a dedup cluster.
// This avoids unsafe merges like /product/* with /deposit/* or /person/* with /org/*.
func filterClusterByNamespaceFamily(fragments []*models.HorizonFragment) ([]*models.HorizonFragment, bool, string) {
	if len(fragments) <= 1 {
		return fragments, false, ""
	}

	byFamily := make(map[string][]*models.HorizonFragment)
	for _, f := range fragments {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		fam := fragmentNamespaceFamilyForDedup(f)
		if fam == "" {
			fam = "unknown"
		}
		byFamily[fam] = append(byFamily[fam], f)
	}
	if len(byFamily) <= 1 {
		return fragments, false, ""
	}

	// Keep only the dominant family when unambiguous; otherwise skip the cluster.
	bestFamily := ""
	bestCount := 0
	tied := false
	for fam, frs := range byFamily {
		if len(frs) > bestCount {
			bestFamily = fam
			bestCount = len(frs)
			tied = false
			continue
		}
		if len(frs) == bestCount {
			tied = true
		}
	}

	counts := formatFamilyCounts(byFamily)
	if bestCount <= 1 || tied {
		return nil, true, fmt.Sprintf("mixed_namespace_family_ambiguous(%s)", counts)
	}
	return byFamily[bestFamily], true, fmt.Sprintf("mixed_namespace_family_filtered(keep=%s;%s)", bestFamily, counts)
}

func extractOrgLegalForm(f *models.HorizonFragment) string {
	parts := canonicalPartsForFragment(f)
	if parts == nil || parts.Org == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts.Org.LegalForm))
}

func keysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func applyCanonicalPathUpdate(f *models.HorizonFragment, newPath string) bool {
	if f == nil {
		return false
	}
	newPath = strings.TrimSpace(newPath)
	if newPath == "" {
		return false
	}
	if strings.TrimSpace(f.CanonicalPath) == newPath {
		return false
	}
	f.CanonicalPath = newPath
	if parts, err := taxonomy.ParseCanonicalPathParts(newPath); err == nil && parts != nil {
		f.CanonicalParts = parts
	}
	return true
}

func isCanonicalLocPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/loc/") {
		return false
	}
	return taxonomy.ValidateCanonicalPath(path) == nil
}

// extractOrgOrGovSlug extracts the last slug segment from /org or /gov paths.
// For /org, it reconstructs the slug including legal-form suffix when available.
func extractOrgOrGovSlug(path string) string {
	cp := canonicalPartsFromPath(path)
	if cp != nil {
		switch cp.Namespace {
		case "org":
			if cp.Org == nil {
				return ""
			}
			return strings.ToLower(strings.TrimSpace(buildOrgSlug(cp.Org)))
		case "gov":
			if cp.Gov == nil {
				return ""
			}
			return strings.ToLower(strings.TrimSpace(cp.Gov.Slug))
		default:
			return ""
		}
	}

	// Fallback: best-effort parsing for legacy/invalid paths.
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
	if ns == "org" && len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	if len(parts) >= 3 {
		return strings.ToLower(strings.TrimSpace(parts[2]))
	}
	return ""
}

func buildOrgSlug(parts *taxonomy.OrgPathParts) string {
	if parts == nil {
		return ""
	}
	base := strings.TrimSpace(parts.BaseSlug)
	if base == "" {
		return ""
	}
	if lf := strings.TrimSpace(parts.LegalForm); lf != "" {
		return base + "-" + lf
	}
	return base
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

	// Hardcode GeoNames username as requested.
	_ = os.Setenv("GEONAMES_USERNAME", "kirisyuk")

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

	// Core policies (canonization + validation) — reuse system rules from Firestore
	canonRulesRepo := repo.NewCanonizationRulesRepository(db)
	canonPolicy := policy.NewCanonizationPolicy(canonRulesRepo)
	canonExecutor := policy.NewPolicyExecutor(canonPolicy, nil, nil)

	validationRulesRepo := repo.NewValidationRulesRepository(db)
	validationPolicy := policy.NewValidationPolicy(validationRulesRepo)
	validationExecutor := policy.NewPolicyExecutor(validationPolicy, nil, nil)

	edgesRepo := repo.NewDependencyEdgesRepository(db).
		WithValidationPolicy(validationExecutor).
		WithFragmentsRepo(fragmentsRepo)

	// GeoNames client (cached) for /loc normalization
	var geoNamesClient coreontology.GeoNamesClientInterface
	if !*skipGeoNames {
		geonamesBaseClient := httphandlers.GetGeoNamesClientFromEnv()
		if geonamesBaseClient == nil {
			log.Printf("WARNING: GEONAMES_USERNAME not set; skipping GeoNames normalization")
		} else {
			geonamesAdapter := geonames.NewAdapter(geonamesBaseClient)
			externalCacheRepo := coreRepo.NewExternalDataCacheRepository(db)
			geoNamesClient = geonames.NewCachedClient(geonamesAdapter, externalCacheRepo, geonames.CacheOptions{
				Enabled: true,
				TTL:     7 * 24 * time.Hour,
			})
		}
	}

	stats := &Stats{}

	// Fast path: --market-cleanup-only skips all fragment dedup/edge fixing
	if *marketCleanupOnly {
		log.Printf("\n=== Market cleanup only mode (--market-cleanup-only) ===")
		geoMismatch, domainNorm := cleanMarketEdgesAndDomains(ctx, db, edgesRepo, *tenantID, *apply, *verbose)
		stats.MarketGeoMismatchCleaned = geoMismatch
		stats.MarketDomainNormalized = domainNorm
		log.Printf("\n=== Summary (market cleanup only) ===")
		log.Printf("Market geo-mismatch edges cleaned: %d", stats.MarketGeoMismatchCleaned)
		log.Printf("Market domains normalized: %d", stats.MarketDomainNormalized)
		if !*apply {
			log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
		}
		return
	}

	// Fast path: --fix-edges-only skips fragment loading, dedup, and merge; only fixes edges
	if *fixEdgesOnly {
		log.Printf("\n=== Fix edges only mode (--fix-edges-only) ===")
		log.Printf("Skipping fragment loading and deduplication; only fixing edge directions + canonicalization + dedup")
		fragmentTypes := make(map[string]models.FragmentType) // empty; fixEdgeDirections fetches types on demand
		if *skipEdgePass {
			log.Printf("SKIPPED (--skip-edge-pass): not modifying edges")
		} else {
			log.Printf("\n=== Step 5: Fixing edge directions ===")
			if *apply {
				fixed, canonicalized, err := fixEdgeDirections(ctx, db, edgesRepo, fragmentTypes, *tenantID, *canonicalizeEdges, validationExecutor, canonExecutor)
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
				log.Printf("[DRY-RUN] Would normalize symmetric edges and deduplicate edges")
			}
			log.Printf("\n=== Step 7: Market geo-mismatch & domain normalization ===")
			geoMismatch, domainNorm := cleanMarketEdgesAndDomains(ctx, db, edgesRepo, *tenantID, *apply, *verbose)
			stats.MarketGeoMismatchCleaned = geoMismatch
			stats.MarketDomainNormalized = domainNorm
		}
		log.Printf("\n=== Summary (fix edges only) ===")
		log.Printf("Edges direction fixed: %d", stats.EdgesDirectionFixed)
		log.Printf("Edges canonicalized: %d", stats.EdgesCanonicalized)
		log.Printf("Edges deduplicated: %d", stats.EdgesDeduplicated)
		log.Printf("Market geo-mismatch edges cleaned: %d", stats.MarketGeoMismatchCleaned)
		log.Printf("Market domains normalized: %d", stats.MarketDomainNormalized)
		if !*apply {
			log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
		}
		return
	}

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

	// Step 1.5: Build profile cache (fragments with profiles get priority)
	log.Printf("\n=== Step 1.5: Building profile cache ===")
	profileCache := buildProfileCache(ctx, db, fragments)
	log.Printf("✓ Found %d fragments with profiles", len(profileCache))

	// Step 1.6: Remove placeholder PRODUCT fragments ending with /na
	log.Printf("\n=== Step 1.6: Removing placeholder PRODUCT fragments (/na) ===")
	kept := fragments[:0]
	removed := 0
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeProduct || !isProductPathEndingWithNA(f.CanonicalPath) {
			kept = append(kept, f)
			continue
		}
		removed++
		delete(profileCache, f.ID)
		if *verbose {
			log.Printf("  PRODUCT remove: %s (%s)", f.CanonicalPath, f.ID)
		}
		if *apply {
			deletedEdges, err := deleteEdgesForFragment(ctx, db, edgesRepo, *tenantID, f.ID)
			if err != nil {
				log.Printf("  ERROR: failed to delete edges for %s: %v", f.ID, err)
			} else {
				stats.EdgesDeleted += deletedEdges
			}
			if err := fragmentsRepo.Delete(ctx, f.ID); err != nil {
				log.Printf("  ERROR: failed to delete product %s: %v", f.ID, err)
			} else {
				stats.FragmentsDeleted++
			}
		} else {
			stats.FragmentsDeleted++
		}
	}
	fragments = kept
	log.Printf("✓ Removed %d product fragments ending with /na", removed)

	// Step 2: Build indexes for duplicate detection
	log.Printf("\n=== Step 2: Building indexes ===")
	wikidataIndex := make(map[string][]*models.HorizonFragment)               // wikidataID -> fragments
	shortNameIndex := make(map[string][]*models.HorizonFragment)              // type:shortName -> fragments
	legalNameIndex := make(map[string][]*models.HorizonFragment)              // type:legalName -> fragments
	labelENIndex := make(map[string][]*models.HorizonFragment)                // type:label_en -> fragments (Tier 2)
	normalizedLegalIndex := make(map[string][]*models.HorizonFragment)        // type:normalized_legal -> fragments
	normalizedLegalNAIndex := make(map[string][]*models.HorizonFragment)      // type:ns:na:normalized -> ORG fragments with unknown jurisdiction (for cross-match into iso2-scoped clusters)
	orgNormalizedNameIndex := make(map[string][]*models.HorizonFragment)      // type:normalized(label/legal) -> org fragments (LOW-signal stub rescue)
	facilityNormalizedNameIndex := make(map[string][]*models.HorizonFragment) // facility_key -> facility fragments (normalized name; scoped by /fac/{org}/)
	geoCityIndex := make(map[string][]*models.HorizonFragment)                // city_name -> geo fragments
	productNameIndex := make(map[string][]*models.HorizonFragment)            // product_name -> product fragments
	productAliasIndex := make(map[string][]*models.HorizonFragment)           // product_alias_key -> product fragments (label/short/legal + legacy slug aliases)
	personPhoneticIndex := make(map[string][]*models.HorizonFragment)         // phonetic_key -> person fragments (catches Tahnoon/Tahnoun, Khalid/Halid)

	for _, f := range fragments {
		if f == nil {
			continue
		}
		if !isTypePathCompatibleForDedup(f) {
			if *verbose {
				log.Printf("  SKIP indexing (type/path mismatch): id=%s type=%s path=%s", f.ID, f.Type, f.CanonicalPath)
			}
			continue
		}

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
					ns := canonicalNamespace(f)
					if ns == "" {
						continue
					}
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					if iso2 != "na" {
						key := fmt.Sprintf("%s:%s:%s:%s", f.Type, ns, iso2, strings.ToLower(shortName))
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
			// For ORG: only within known ISO2, scoped by namespace to avoid cross-namespace merges.
			if f.Type == models.FragmentTypeOrganization {
				ns := canonicalNamespace(f)
				if ns == "" {
					continue
				}
				iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
				if iso2 != "na" {
					key := fmt.Sprintf("%s:%s:%s:%s", f.Type, ns, iso2, strings.ToLower(legalName))
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
					ns := canonicalNamespace(f)
					if ns == "" {
						continue
					}
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					if iso2 != "na" {
						key := fmt.Sprintf("%s:%s:%s:%s", f.Type, ns, iso2, strings.ToLower(labelEN))
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
				if *enableOrgFuzzyName {
					ns := canonicalNamespace(f)
					if ns == "" {
						continue
					}
					iso2 := extractOrgOrGovISO2ForDedupKey(f.CanonicalPath)
					normalized := normalizeLegalName(legalName)
					if normalized != "" {
						if iso2 != "na" {
							key := fmt.Sprintf("%s:%s:%s:%s", f.Type, ns, iso2, normalized)
							normalizedLegalIndex[key] = append(normalizedLegalIndex[key], f)
						} else {
							// Index "na" ORG fragments separately — they'll be merged into
							// iso2-scoped clusters during cluster building (Priority 5).
							naKey := fmt.Sprintf("%s:%s:na:%s", f.Type, ns, normalized)
							normalizedLegalNAIndex[naKey] = append(normalizedLegalNAIndex[naKey], f)
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
			// Default OFF: still name-based; keep namespace-scoped to avoid cross-namespace merges.
			if !*enableOrgFuzzyName {
				continue
			}
			ns := canonicalNamespace(f)
			if ns == "" {
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
					key := fmt.Sprintf("%s:%s:%s:%s", f.Type, ns, iso2, n)
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
		// - Service canonical: /product/service/{domain}/{subdomain}/{slug}
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

	// ORG slug indexes (for resolving legacy alias /org/{slug} → canonical /org/{geo}/{slug}).
	// This helps catch cases like:
	//   /org/public-investment-fund with identifiers.merged_paths="/org/pif"
	//   should merge into /org/sa/pif even if /org/pif doesn't exist as a standalone fragment.
	orgSlugIndex := make(map[string][]*models.HorizonFragment)      // slug -> org fragments with /org/{geo}/{slug} (and legacy /org/{slug})
	orgAliasSlugIndex := make(map[string][]*models.HorizonFragment) // slug -> fragments that declare alias via merged_paths / legacy_org_canonical_path (legacy /org/{slug})
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		if canonicalNamespace(f) != "org" {
			continue
		}
		if slug := extractOrgOrGovSlug(f.CanonicalPath); slug != "" {
			orgSlugIndex[slug] = append(orgSlugIndex[slug], f)
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
	log.Printf("✓ Normalized legal NA index: %d unique keys (org fragments with unknown jurisdiction)", len(normalizedLegalNAIndex))
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
							CanonicalID: selectCanonical([]*models.HorizonFragment{geoFrag, f}, profileCache),
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
		if !isCanonicalLocPath(f.CanonicalPath) && len(parts) == 2 && parts[0] == "loc" {
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
				ns := canonicalNamespace(f)
				if ns == "" {
					continue
				}
				for _, alias := range extractAliasCanonicalPathsForDedup(f) {
					if aliasParts := canonicalPartsFromPath(alias); aliasParts != nil {
						if strings.ToLower(strings.TrimSpace(aliasParts.Namespace)) != ns {
							continue
						}
					}
					k := fmt.Sprintf("%s:%s", f.Type, alias) // same type only
					for _, other := range byCanonicalPath[k] {
						if other == nil || processedFragmentIDs[other.ID] || other.ID == f.ID {
							continue
						}
						if canonicalNamespace(other) != ns {
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
				cluster.CanonicalID = selectCanonical(frs, profileCache)
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

	// Priority 0.775: ORG legacy slug aliases (/org/{slug} in merged_paths) -> canonical /org/{geo}/{slug}.
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
		// Candidate set: anything already living under /org/.../{slug} plus the fragments that claim this alias.
		cands := make([]*models.HorizonFragment, 0, len(orgSlugIndex[slug])+len(claimers))
		cands = append(cands, orgSlugIndex[slug]...)
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
		cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
			cluster.CanonicalID = selectCanonical(frs, profileCache)
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

	// Priority 0.825: Legacy /org/{company} vs /org/{company}-{legalForm} (no geo)
	// Merge only when all name signals agree (ignoring legal form) and country signals don't conflict.
	orgLegacyNoGeoClusters := 0
	{
		legacyNoGeoIndex := make(map[string][]*models.HorizonFragment) // baseName -> legacy /org/* (no geo)
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
			if canonicalNamespace(f) != "org" {
				continue
			}
			cp := canonicalPartsFromPath(f.CanonicalPath)
			if cp == nil || cp.Org == nil {
				continue
			}
			if !cp.Org.Legacy && strings.TrimSpace(cp.Org.Geo) != "" {
				continue // has geo segment
			}
			baseName := extractOrgBaseName(f.CanonicalPath)
			if baseName == "" {
				continue
			}
			legacyNoGeoIndex[baseName] = append(legacyNoGeoIndex[baseName], f)
		}

		for baseName, frs := range legacyNoGeoIndex {
			nameGroups := make(map[string][]*models.HorizonFragment)
			for _, f := range frs {
				if f == nil {
					continue
				}
				if processedFragmentIDs[f.ID] {
					continue
				}
				nameKey := orgLegacyNoGeoNameKey(f)
				if nameKey == "" {
					continue
				}
				nameGroups[nameKey] = append(nameGroups[nameKey], f)
			}

			for nameKey, frs := range nameGroups {
				// Unique by ID and skip processed
				seenID := map[string]struct{}{}
				unprocessed := make([]*models.HorizonFragment, 0, len(frs))
				for _, f := range frs {
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

				if !orgLegacyNoGeoNamesConsistent(unprocessed) {
					if *verbose {
						log.Printf("  SKIP ORG cluster (orgLegacyNoGeoLegalForm): baseName=%s reason=names_conflict", baseName)
					}
					continue
				}

				if skip, reason := shouldSkipOrgLegacyNoGeoCluster(unprocessed); skip {
					if *verbose {
						log.Printf("  SKIP ORG cluster (orgLegacyNoGeoLegalForm): baseName=%s reason=%s", baseName, reason)
					}
					continue
				}

				// If LOCATED_IN countries exist, they must be consistent.
				legacyCountry := ""
				countryConflict := false
				for _, f := range unprocessed {
					c, ok := legacyOrgCountryFromEdges(ctx, edgesRepo, *tenantID, f.ID, idToFrag)
					if !ok {
						continue
					}
					if legacyCountry == "" {
						legacyCountry = c
						continue
					}
					if legacyCountry != c {
						countryConflict = true
						break
					}
				}
				if countryConflict {
					if *verbose {
						log.Printf("  SKIP ORG cluster (orgLegacyNoGeoLegalForm): baseName=%s reason=conflicting_located_in", baseName)
					}
					continue
				}

				cluster := &DuplicateCluster{
					MatchType:  "orgLegacyNoGeoLegalForm",
					MatchValue: fmt.Sprintf("%s:%s", baseName, nameKey),
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
						processedFragmentIDs[f.ID] = true
					}
				}
				orgLegacyNoGeoClusters++
			}
		}
	}
	log.Printf("  Found %d orgLegacyNoGeoLegalForm-based clusters", orgLegacyNoGeoClusters)

	// Priority 0.85: Legacy /org/{company} vs /org/{geo}/{company}-{legalForm} deduplication
	// Matches legacy paths like /org/tesla with canonical paths like /org/us/tesla-inc
	// when base company name matches (ignoring legal form).
	//
	// IMPORTANT SAFETY:
	// - Do NOT merge /org/{base} with geo-scoped /org/{geo}/{base} unless the legacy fragment
	//   has explicit LOCATED_IN edges whose country tokens are all the same, and match the geo.
	// - This prevents cross-country branch merges (e.g., /org/kpmg vs /org/tr/kpmg vs /org/kz/kpmg).
	// - Can be skipped with --skip-org-legacy-country-check (faster, higher risk).
	orgLegacyBaseNameClusters := 0
	{
		start := time.Now()
		checkedLegacy := 0
		// Index by base company name (without legal form)
		legacyBaseNameIndex := make(map[string][]*models.HorizonFragment)    // baseName -> legacy /org/{company} fragments
		canonicalBaseNameIndex := make(map[string][]*models.HorizonFragment) // baseName:iso2 -> canonical /org/{geo}/{company}-{legalForm} fragments

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
			if canonicalNamespace(f) != "org" {
				continue
			}

			cp := canonicalPartsFromPath(f.CanonicalPath)
			if cp == nil || cp.Org == nil {
				continue
			}

			// Extract base name (without legal form)
			baseName := extractOrgBaseName(f.CanonicalPath)
			if baseName == "" {
				continue
			}

			// Check if this is a legacy path (/org/{company}) or canonical path (/org/{geo}/{company}-{legalForm})
			if cp.Org.Legacy || strings.TrimSpace(cp.Org.Geo) == "" {
				// Legacy path: /org/{company}
				legacyBaseNameIndex[baseName] = append(legacyBaseNameIndex[baseName], f)
			} else {
				// Canonical path: /org/{geo}/{company}-{legalForm}
				geo := strings.ToLower(strings.TrimSpace(cp.Org.Geo))
				if isISO2CountryToken(geo) {
					key := fmt.Sprintf("%s:%s", baseName, geo)
					canonicalBaseNameIndex[key] = append(canonicalBaseNameIndex[key], f)
				}
			}
		}

		// Find matches where base name is the same AND legacy has consistent LOCATED_IN country
		for baseName, legacyFrags := range legacyBaseNameIndex {
			for _, legacy := range legacyFrags {
				if legacy == nil || strings.TrimSpace(legacy.ID) == "" || processedFragmentIDs[legacy.ID] {
					continue
				}
				checkedLegacy++
				if checkedLegacy%500 == 0 {
					log.Printf("  orgLegacyBaseName progress: checked=%d clusters=%d elapsed=%s", checkedLegacy, orgLegacyBaseNameClusters, time.Since(start).Round(time.Second))
				}
				legacyCountry := ""
				if !*skipOrgLegacyCountryCheck {
					c, ok := legacyOrgCountryFromEdges(ctx, edgesRepo, *tenantID, legacy.ID, idToFrag)
					if !ok {
						if *verbose {
							log.Printf("  SKIP ORG cluster (orgLegacyBaseName): baseName=%s legacy=%s reason=no_single_country_located_in", baseName, legacy.CanonicalPath)
						}
						continue
					}
					legacyCountry = c
				}

				key := fmt.Sprintf("%s:%s", baseName, legacyCountry)
				canonicalFrags := canonicalBaseNameIndex[key]
				if len(canonicalFrags) == 0 {
					continue
				}

				// Combine legacy and canonical fragments (same ISO2)
				cands := make([]*models.HorizonFragment, 0, 1+len(canonicalFrags))
				cands = append(cands, legacy)
				cands = append(cands, canonicalFrags...)

				// Unique by ID and skip processed
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

				// Safety checks: skip if conflicting Wikidata IDs or legal names
				if skip, reason := shouldSkipOrgNameCluster("orgLegacyBaseName", unprocessed); skip {
					if *verbose {
						log.Printf("  SKIP ORG cluster (orgLegacyBaseName): baseName=%s reason=%s", baseName, reason)
					}
					continue
				}

				matchValue := baseName
				if legacyCountry != "" {
					matchValue = fmt.Sprintf("%s:%s", baseName, legacyCountry)
				}
				cluster := &DuplicateCluster{
					MatchType:  "orgLegacyBaseName",
					MatchValue: matchValue,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					if f != nil && f.ID != "" && f.ID != cluster.CanonicalID {
						processedFragmentIDs[f.ID] = true
					}
				}
				orgLegacyBaseNameClusters++
			}
		}
		log.Printf("  orgLegacyBaseName scan done: checked=%d clusters=%d elapsed=%s", checkedLegacy, orgLegacyBaseNameClusters, time.Since(start).Round(time.Second))
	}
	log.Printf("  Found %d orgLegacyBaseName-based clusters", orgLegacyBaseNameClusters)

	// Priority 1: Wikidata ID matches (highest confidence)
	wikidataClusters := 0
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				wikidataClusters++
			}
		}
	}
	log.Printf("  Found %d Wikidata-based clusters", wikidataClusters)

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
				} else if fragType == models.FragmentTypeMarket {
					// MARKET name-only matches are very noisy across geos/domains.
					// Keep only scope-consistent groups; otherwise skip.
					filtered, skip, reason := filterMarketByScope(unprocessed)
					if skip {
						if *verbose {
							log.Printf("  SKIP MARKET cluster (shortName): key=%s reason=%s", key, reason)
						}
						continue
					}
					unprocessed = filtered
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
				} else if fragType == models.FragmentTypeMarket {
					filtered, skip, reason := filterMarketByScope(unprocessed)
					if skip {
						if *verbose {
							log.Printf("  SKIP MARKET cluster (legalName): key=%s reason=%s", key, reason)
						}
						continue
					}
					unprocessed = filtered
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
				} else if fragType == models.FragmentTypeMarket {
					filtered, skip, reason := filterMarketByScope(unprocessed)
					if skip {
						if *verbose {
							log.Printf("  SKIP MARKET cluster (labelEN): key=%s reason=%s", key, reason)
						}
						continue
					}
					unprocessed = filtered
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
		cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
		// Also pull in "na" (unknown jurisdiction) fragments whose normalized name matches.
		// Key format: "ORGANIZATION:org:sa:stc bank" → extract type, ns, normalized → look up "ORGANIZATION:org:na:stc bank".
		allFrags := frags
		if parts := strings.SplitN(key, ":", 4); len(parts) == 4 {
			naKey := fmt.Sprintf("%s:%s:na:%s", parts[0], parts[1], parts[3])
			if naFrags, ok := normalizedLegalNAIndex[naKey]; ok && len(naFrags) > 0 {
				allFrags = make([]*models.HorizonFragment, 0, len(frags)+len(naFrags))
				allFrags = append(allFrags, frags...)
				allFrags = append(allFrags, naFrags...)
			}
		}

		if len(allFrags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range allFrags {
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				normalizedLegalClusters++
			}
		}
	}
	// Also check "na" groups that didn't match any iso2-scoped group (e.g., multiple legacy orgs with same name).
	for naKey, naFrags := range normalizedLegalNAIndex {
		if len(naFrags) <= 1 {
			continue
		}
		var unprocessed []*models.HorizonFragment
		for _, f := range naFrags {
			if !processedFragmentIDs[f.ID] {
				unprocessed = append(unprocessed, f)
			}
		}
		if len(unprocessed) <= 1 {
			continue
		}
		fragType := unprocessed[0].Type
		if fragType == models.FragmentTypeOrganization {
			if skip, reason := shouldSkipOrgNameCluster("normalizedLegal", unprocessed); skip {
				if *verbose {
					log.Printf("  SKIP ORG cluster (normalizedLegal/na): key=%s reason=%s", naKey, reason)
				}
				continue
			}
		}
		cluster := &DuplicateCluster{
			MatchType:  "normalizedLegal",
			MatchValue: naKey,
			Fragments:  unprocessed,
		}
		cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
		clusters = append(clusters, cluster)
		for _, f := range unprocessed {
			processedFragmentIDs[f.ID] = true
		}
		normalizedLegalClusters++
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
		cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
				// Safety: productName slug-only matches are risky when all candidates are
				// already hierarchical/service products (different stage/type can share slug).
				// Allow only clusters anchored by at least one flat/legacy product path.
				if !hasFlatProductAnchor(unprocessed) {
					if *verbose {
						log.Printf("  SKIP PRODUCT cluster (productName): key=%s reason=no_flat_anchor", productName)
					}
					continue
				}
				cluster := &DuplicateCluster{
					MatchType:  "productName",
					MatchValue: productName,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
				cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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
			cluster.CanonicalID = selectCanonical(unprocessed, profileCache)
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

		clusterFragments := cluster.Fragments
		if filtered, changed, reason := filterClusterByNamespaceFamily(clusterFragments); changed {
			if len(filtered) <= 1 {
				if *verbose {
					log.Printf("  SKIP cluster: reason=%s", reason)
				}
				continue
			}
			clusterFragments = filtered
			if *verbose {
				log.Printf("  FILTER cluster: reason=%s", reason)
			}
		}

		// Special case: MARKET hierarchy is not dedup.
		// If cluster paths form parent/child prefixes, keep nodes separate and enforce HAS_SUBMARKET edges.
		if hierarchyPairs, hasHierarchy := collectMarketHierarchyPairs(clusterFragments); hasHierarchy {
			byPath := make(map[string][]*models.HorizonFragment)
			for _, f := range clusterFragments {
				if f == nil || strings.TrimSpace(f.ID) == "" {
					continue
				}
				cp := strings.TrimSpace(f.CanonicalPath)
				if cp == "" {
					continue
				}
				byPath[cp] = append(byPath[cp], f)
			}

			canonicalByPath := make(map[string]*models.HorizonFragment, len(byPath))
			for cp, group := range byPath {
				if len(group) == 0 {
					continue
				}
				cid := selectCanonical(group, profileCache)
				for _, f := range group {
					if f != nil && f.ID == cid {
						canonicalByPath[cp] = f
						break
					}
				}
			}

			createdOrPlanned := 0
			for _, pair := range hierarchyPairs {
				parent := canonicalByPath[pair[0]]
				child := canonicalByPath[pair[1]]
				if parent == nil || child == nil {
					continue
				}
				created, err := ensureHasSubmarketEdge(ctx, edgesRepo, *tenantID, parent.ID, child.ID, *apply)
				if err != nil {
					log.Printf("  ERROR: failed to ensure HAS_SUBMARKET %s -> %s: %v", parent.ID, child.ID, err)
					continue
				}
				if created {
					createdOrPlanned++
					if *verbose {
						if *apply {
							log.Printf("  ✓ Ensured HAS_SUBMARKET: %s -> %s", parent.CanonicalPath, child.CanonicalPath)
						} else {
							log.Printf("  [DRY-RUN] Would ensure HAS_SUBMARKET: %s -> %s", parent.CanonicalPath, child.CanonicalPath)
						}
					}
				}
			}
			if *verbose {
				log.Printf("  SKIP market hierarchy merge: pairs=%d edges=%d", len(hierarchyPairs), createdOrPlanned)
			}
			continue
		}

		// Generic hierarchy guard: if canonical paths in the same namespace form strict parent/child hierarchy,
		// do not merge this cluster. These are usually taxonomy nodes, not duplicates.
		if namespace, hierarchyPairs, hasHierarchy := collectNamespaceHierarchyPairs(clusterFragments, 2); hasHierarchy {
			if *verbose {
				log.Printf("  SKIP %s hierarchy merge: pairs=%d", namespace, len(hierarchyPairs))
			}
			continue
		}

		// Find canonical fragment
		var canonical *models.HorizonFragment
		var duplicates []*models.HorizonFragment
		canonicalID := strings.TrimSpace(cluster.CanonicalID)
		if canonicalID == "" {
			canonicalID = selectCanonical(clusterFragments, profileCache)
		}
		for _, f := range clusterFragments {
			if f.ID == canonicalID {
				canonical = f
			} else {
				duplicates = append(duplicates, f)
			}
		}
		if canonical == nil {
			duplicates = nil
			canonicalID = selectCanonical(clusterFragments, profileCache)
			for _, f := range clusterFragments {
				if f.ID == canonicalID {
					canonical = f
				} else {
					duplicates = append(duplicates, f)
				}
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

		// Fix canonical path using core canonization policy (if available)
		pathFixed := false
		if canonExecutor != nil {
			if normalized := normalizeCanonicalPathWithPolicy(ctx, canonExecutor, canonical.CanonicalPath); normalized != "" && normalized != canonical.CanonicalPath {
				oldPath := canonical.CanonicalPath
				if applyCanonicalPathUpdate(canonical, normalized) {
					pathFixed = true
					stats.PathsFixed++
					if *verbose {
						log.Printf("  Path fixed (policy): %s -> %s", oldPath, canonical.CanonicalPath)
					}
				}
			}
		}

		// Fix canonical path for GEO_REGION if needed (legacy geo fallback)
		if canonical.Type == models.FragmentTypeGeoRegion {
			if geoNamesClient != nil {
				if norm, step, _ := scout.NormalizeGeoPathViaGeoNames(ctx, canonical.CanonicalPath, geoNamesClient); norm != "" && norm != canonical.CanonicalPath {
					oldPath := canonical.CanonicalPath
					if applyCanonicalPathUpdate(canonical, norm) {
						pathFixed = true
						stats.PathsFixed++
						if *verbose {
							log.Printf("  Path fixed (geonames): %s -> %s (%s)", oldPath, canonical.CanonicalPath, step)
						}
					}
				}
			}
			if !isCanonicalLocPath(canonical.CanonicalPath) {
				oldPath := canonical.CanonicalPath
				if fixed := fixGeoPath(canonical.CanonicalPath); fixed != "" {
					if applyCanonicalPathUpdate(canonical, fixed) {
						pathFixed = true
						stats.PathsFixed++
						if *verbose {
							log.Printf("  Path fixed: %s -> %s", oldPath, canonical.CanonicalPath)
						}
					}
				}
			}
		}

		// Fix canonical path for DOMAIN if needed (normalize facet ordering)
		if canonical.Type == models.FragmentTypeDomain {
			if normalized, err := taxonomy.NormalizeDomainCanonicalPath(canonical.CanonicalPath); err == nil && normalized != "" && normalized != canonical.CanonicalPath {
				oldPath := canonical.CanonicalPath
				if applyCanonicalPathUpdate(canonical, normalized) {
					pathFixed = true
					stats.PathsFixed++
					if *verbose {
						log.Printf("  Path fixed: %s -> %s", oldPath, canonical.CanonicalPath)
					}
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
			reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, *tenantID, dup.ID, canonical.ID)
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
		fixed, canonicalized, err := fixEdgeDirections(ctx, db, edgesRepo, fragmentTypes, *tenantID, *canonicalizeEdges, validationExecutor, canonExecutor)
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

	// Step 7: Market geo-mismatch cleanup + market domain normalization
	log.Printf("\n=== Step 7: Market geo-mismatch & domain normalization ===")
	{
		geoMismatch, domainNorm := cleanMarketEdgesAndDomains(ctx, db, edgesRepo, *tenantID, *apply, *verbose)
		stats.MarketGeoMismatchCleaned = geoMismatch
		stats.MarketDomainNormalized = domainNorm
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
	log.Printf("Market geo-mismatch edges cleaned: %d", stats.MarketGeoMismatchCleaned)
	log.Printf("Market domains normalized: %d", stats.MarketDomainNormalized)
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

// buildProfileCache builds a cache of fragment IDs that have profiles.
// Returns a map[fragmentID]bool for fast lookup.
// Core-first: we scan profiles collection once and map to current fragments.
func buildProfileCache(ctx context.Context, db *firestore.Client, fragments []*models.HorizonFragment) map[string]bool {
	cache := make(map[string]bool)
	if db == nil || len(fragments) == 0 {
		return cache
	}

	// Build a set of known fragment IDs to avoid keeping unrelated profiles.
	fragmentIDSet := make(map[string]struct{}, len(fragments))
	for _, f := range fragments {
		if f != nil && strings.TrimSpace(f.ID) != "" {
			fragmentIDSet[f.ID] = struct{}{}
		}
	}
	if len(fragmentIDSet) == 0 {
		return cache
	}

	iter := db.Collection("horizon_fragment_profiles").
		Select("fragmentId", "status").
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("WARNING: failed to scan profiles: %v", err)
			break
		}
		var profile models.FragmentProfile
		if err := doc.DataTo(&profile); err != nil {
			continue
		}
		fragID := strings.TrimSpace(profile.FragmentID)
		if fragID == "" {
			continue
		}
		if _, ok := fragmentIDSet[fragID]; ok {
			// Treat any existing profile as "created" for priority purposes.
			cache[fragID] = true
		}
	}

	return cache
}

// normalizeCanonicalPathWithPolicy uses the core canonization policy to normalize a canonical path.
// Returns empty string when no normalization is available.
func normalizeCanonicalPathWithPolicy(ctx context.Context, executor *policy.PolicyExecutor, path string) string {
	if executor == nil {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	decision, err := executor.Execute(ctx, policy.PolicyInput{
		Kind: "canonization",
		Context: map[string]interface{}{
			"inputType":  "canonical_path",
			"inputValue": path,
		},
	})
	if err != nil {
		return ""
	}
	if decision.Action != "normalize_to" {
		return ""
	}
	if output, ok := decision.Params["output"].(string); ok {
		return strings.TrimSpace(output)
	}
	return ""
}

// normalizeRelationTypeWithPolicy uses the core canonization policy to normalize edge types.
func normalizeRelationTypeWithPolicy(ctx context.Context, executor *policy.PolicyExecutor, edgeType models.DependencyType) string {
	if executor == nil {
		return ""
	}
	in := strings.TrimSpace(string(edgeType))
	if in == "" {
		return ""
	}
	decision, err := executor.Execute(ctx, policy.PolicyInput{
		Kind: "canonization",
		Context: map[string]interface{}{
			"inputType":  "relation_type",
			"inputValue": in,
		},
	})
	if err != nil {
		return ""
	}
	if decision.Action != "normalize_to" {
		return ""
	}
	if output, ok := decision.Params["output"].(string); ok {
		return strings.TrimSpace(output)
	}
	return ""
}

func isISO2CountryToken(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 2 {
		return false
	}
	return s >= "aa" && s <= "zz"
}

func extractCountryFromLocPath(path string) string {
	cp := canonicalPartsFromPath(path)
	if cp == nil || cp.Namespace != "loc" || cp.Loc == nil {
		return ""
	}
	c := strings.ToLower(strings.TrimSpace(cp.Loc.Country))
	if !isISO2CountryToken(c) {
		return ""
	}
	return c
}

// legacyOrgCountryFromEdges returns a single ISO2 country inferred from LOCATED_IN edges.
// It returns ok=false if there is no country signal or if multiple countries are present.
func legacyOrgCountryFromEdges(ctx context.Context, edgesRepo *repo.DependencyEdgesRepository, tenantID, legacyID string, idToFrag map[string]*models.HorizonFragment) (country string, ok bool) {
	if edgesRepo == nil || strings.TrimSpace(legacyID) == "" {
		return "", false
	}
	edgeType := models.DependencyTypeLocatedIn
	edges, err := edgesRepo.ListBySource(ctx, tenantID, legacyID, &edgeType, 500)
	if err != nil {
		return "", false
	}
	countries := map[string]struct{}{}
	for _, ed := range edges {
		if ed == nil {
			continue
		}
		targetID := strings.TrimSpace(ed.TargetFragmentID)
		if targetID == "" {
			continue
		}
		target := idToFrag[targetID]
		if target == nil || target.Type != models.FragmentTypeGeoRegion {
			continue
		}
		c := extractCountryFromLocPath(target.CanonicalPath)
		if c == "" {
			continue
		}
		countries[c] = struct{}{}
		if len(countries) > 1 {
			return "", false
		}
	}
	if len(countries) != 1 {
		return "", false
	}
	for c := range countries {
		return c, true
	}
	return "", false
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

// extractOrgBaseName extracts the base company name from an org canonical path,
// removing legal form suffix (e.g., "tesla-inc" -> "tesla", "tesla" -> "tesla").
// Returns empty string if path is invalid or cannot extract base name.
func extractOrgBaseName(canonicalPath string) string {
	cp := canonicalPartsFromPath(canonicalPath)
	if cp == nil || cp.Namespace != "org" || cp.Org == nil {
		return ""
	}

	// Use BaseSlug if available (already without legal form)
	if baseSlug := strings.TrimSpace(cp.Org.BaseSlug); baseSlug != "" {
		return strings.ToLower(baseSlug)
	}

	// Fallback: extract from full slug by removing legal form
	fullSlug := buildOrgSlug(cp.Org)
	if fullSlug == "" {
		return ""
	}

	// Remove legal form suffix if present
	baseName := stripLegalFormSuffixFromSlug(fullSlug)
	return strings.ToLower(strings.TrimSpace(baseName))
}

// stripLegalFormSuffixFromSlug removes legal form suffix from a slug.
// This is a local implementation similar to taxonomy.stripLegalFormSuffix.
func stripLegalFormSuffixFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return slug
	}

	// Strip slugified parenthetical annotations first, e.g.:
	//   "stc-bank-formerly-stc-pay-pjsc" → "stc-bank-pjsc"
	// Handles patterns like "-formerly-...", "-previously-...", "-now-..."
	slug = stripSlugParenthetical(slug)

	// Common legal forms (same list as in taxonomy.stripLegalFormSuffix)
	legalForms := []string{"inc", "llc", "jsc", "pjsc", "ltd", "corp", "plc", "gmbh", "sarl", "srl", "ag", "sa", "spa", "as", "ab", "bv", "nv", "se", "co", "pty", "cjsc", "llp", "lp", "sp", "sup", "fsup", "soe"}

	// Sort by length (longest first) to match longer forms first (e.g., "pjsc" before "jsc")
	sort.Slice(legalForms, func(i, j int) bool {
		return len(legalForms[i]) > len(legalForms[j])
	})

	changed := true
	for changed {
		changed = false
		for _, lf := range legalForms {
			suffix := "-" + lf
			if strings.HasSuffix(slug, suffix) {
				slug = strings.TrimSuffix(slug, suffix)
				slug = strings.TrimSuffix(slug, "-")
				changed = true
				break
			}
		}
	}

	return slug
}

// stripSlugParenthetical removes slugified parenthetical annotations from slugs.
// These arise from LLM-generated names like "STC Bank (formerly stc pay)" that get
// slugified to "stc-bank-formerly-stc-pay-pjsc".
//
// Detected patterns: -formerly-..., -previously-..., -now-..., -aka-..., -fka-..., -dba-...
// The annotation runs from the trigger word to the end of the slug or to the last legal form token.
func stripSlugParenthetical(slug string) string {
	triggers := []string{"-formerly-", "-previously-", "-now-", "-aka-", "-fka-", "-dba-"}
	for _, t := range triggers {
		idx := strings.Index(slug, t)
		if idx < 0 {
			continue
		}
		// Everything before the trigger is the "clean" part.
		// Everything after might end with a legal form suffix — we need to keep that.
		before := slug[:idx]
		after := slug[idx+len(t):]
		// Check if there's a legal form at the very end of 'after'.
		legalTail := extractTrailingLegalForm(after)
		if legalTail != "" {
			return before + "-" + legalTail
		}
		return before
	}
	return slug
}

// extractTrailingLegalForm returns the legal form suffix at the very end of a slug segment, if any.
// E.g., "stc-pay-pjsc" → "pjsc", "stc-pay" → "".
func extractTrailingLegalForm(slug string) string {
	legalForms := []string{"pjsc", "cjsc", "ojsc", "gmbh", "sarl", "corp", "pllc", "fsup", "llc", "jsc", "ltd", "plc", "inc", "srl", "spa", "sup", "soe", "llp", "pty", "ag", "sa", "as", "ab", "bv", "nv", "se", "co", "lp", "sp"}
	for _, lf := range legalForms {
		suffix := "-" + lf
		if strings.HasSuffix(slug, suffix) {
			return lf
		}
	}
	return ""
}

// normalizeOrgCanonicalPathForDedup normalizes org canonical paths by treating '_' and '-' as equivalent.
// This is used only for grouping candidates; canonical selection is still score-based.
func normalizeOrgCanonicalPathForDedup(canonicalPath string) string {
	cp := canonicalPartsFromPath(canonicalPath)
	if cp == nil || cp.Namespace != "org" || cp.Org == nil {
		return ""
	}
	slug := buildOrgSlug(cp.Org)
	if slug == "" {
		return ""
	}
	slug = strings.ReplaceAll(strings.ToLower(slug), "_", "-")
	// Strip slugified parenthetical annotations (e.g., "-formerly-stc-pay") before comparison.
	slug = stripSlugParenthetical(slug)
	geo := strings.TrimSpace(cp.Org.Geo)
	if cp.Org.Legacy || geo == "" {
		geo = "na"
	} else {
		geo = normalizeOrgGovGeoForDedupKey(geo)
		if geo == "" {
			geo = "na"
		}
	}
	return "/org/" + geo + "/" + slug
}

// isGovCanonicalPath returns true if path is in /gov/* namespace.
func isGovCanonicalPath(path string) bool {
	cp := canonicalPartsFromPath(path)
	if cp != nil {
		return strings.EqualFold(strings.TrimSpace(cp.Namespace), "gov")
	}
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "/gov/")
}

// extractOrgOrGovISO2ForDedupKey extracts geo token from:
// - /org/{geo}/{slug}
// - /gov/{geo}/{slug}
// - legacy /org/{slug} (returns "na")
//
// Returns "na" when unknown/invalid.
func extractOrgOrGovISO2ForDedupKey(path string) string {
	cp := canonicalPartsFromPath(path)
	if cp != nil {
		switch cp.Namespace {
		case "org":
			if cp.Org == nil {
				return "na"
			}
			geo := strings.TrimSpace(cp.Org.Geo)
			if cp.Org.Legacy || geo == "" {
				return "na"
			}
			return normalizeOrgGovGeoForDedupKey(geo)
		case "gov":
			if cp.Gov == nil {
				return "na"
			}
			return normalizeOrgGovGeoForDedupKey(cp.Gov.Geo)
		default:
			return "na"
		}
	}

	// Fallback: best-effort parsing for legacy/invalid paths.
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
		if ns == "org" && len(parts) == 2 {
			return "na"
		}
		if len(parts) >= 3 {
			return normalizeOrgGovGeoForDedupKey(parts[1])
		}
	}
	return "na"
}

func normalizeOrgGovGeoForDedupKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "na" {
		return "na"
	}
	if s == "int" {
		return "int"
	}
	if len(s) == 2 && s >= "aa" && s <= "zz" {
		return s
	}
	if taxonomy.IsCompositeGeoToken(s) {
		return s
	}
	if taxonomy.IsRegionalGeoToken(s) {
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
// profileCache: map of fragment IDs that have profiles (for priority)
func selectCanonical(fragments []*models.HorizonFragment, profileCache map[string]bool) string {
	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fragments[0].ID
	}

	// Absolute priority: if any fragment has a profile, only consider those.
	if profileCache != nil {
		withProfile := make([]*models.HorizonFragment, 0, len(fragments))
		for _, f := range fragments {
			if f == nil || strings.TrimSpace(f.ID) == "" {
				continue
			}
			if profileCache[f.ID] {
				withProfile = append(withProfile, f)
			}
		}
		if len(withProfile) > 0 {
			fragments = withProfile
		}
	}

	fragType := fragments[0].Type
	// For PRODUCT: if a cluster mixes flat and full canonical paths, prefer full canonical paths.
	// Example: /product/robotaxi-service vs /product/service/logistics/ride-hailing/robotaxi-service
	if fragType == models.FragmentTypeProduct {
		if preferred := preferFullProductPaths(fragments); len(preferred) > 0 {
			fragments = preferred
		}
	}

	// For GEO_REGION: first check if any have /loc/ prefix
	if fragType == models.FragmentTypeGeoRegion {
		// Prefer canonical /loc/{cc}/{admin1}/{city} when available.
		var canonicalLocs []*models.HorizonFragment
		var locFragments []*models.HorizonFragment
		for _, f := range fragments {
			if strings.HasPrefix(f.CanonicalPath, "/loc/") {
				locFragments = append(locFragments, f)
				if isCanonicalLocPath(f.CanonicalPath) {
					canonicalLocs = append(canonicalLocs, f)
				}
			}
		}
		if len(canonicalLocs) > 0 {
			return selectCanonicalByScore(canonicalLocs, profileCache)
		}
		if len(locFragments) > 0 {
			return selectCanonicalByScore(locFragments, profileCache)
		}
	}

	// Default: score-based selection
	return selectCanonicalByScore(fragments, profileCache)
}

func preferFullProductPaths(fragments []*models.HorizonFragment) []*models.HorizonFragment {
	if len(fragments) <= 1 {
		return nil
	}
	flat := make([]*models.HorizonFragment, 0, len(fragments))
	full := make([]*models.HorizonFragment, 0, len(fragments))
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeProduct {
			continue
		}
		if isFlatProductPathForCanonicalSelection(f) {
			flat = append(flat, f)
		} else {
			full = append(full, f)
		}
	}
	if len(flat) > 0 && len(full) > 0 {
		return full
	}
	return nil
}

func isFlatProductPathForCanonicalSelection(f *models.HorizonFragment) bool {
	if f == nil {
		return false
	}
	cp := canonicalPartsForFragment(f)
	if cp != nil && cp.Namespace == "product" && cp.Product != nil {
		shape := strings.ToLower(strings.TrimSpace(cp.Product.Shape))
		if shape == "flat" {
			return true
		}
		if shape == "org_scoped_flat" {
			// /product/{category}/{slug} may be parsed as org_scoped_flat but is still taxonomy-like,
			// so it should outrank legacy /product/{slug} in canonical selection.
			return !isProductTaxonomyHead(cp.Product.OrgSlug)
		}
		return false
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(f.CanonicalPath), "/"), "/")
	if len(parts) < 2 || parts[0] != "product" {
		return false
	}
	// Legacy fallback when parser can't parse canonical parts.
	// /product/{slug} is flat.
	if len(parts) == 2 {
		return true
	}
	// /product/{orgOrCategory}/{slug} is usually flat (org-scoped), except taxonomy-like heads.
	if len(parts) == 3 {
		return !isProductTaxonomyHead(parts[1])
	}
	return false
}

func isProductTaxonomyHead(seg string) bool {
	switch strings.ToLower(strings.TrimSpace(seg)) {
	case "raw", "component", "finished", "service":
		return true
	default:
		return false
	}
}

func splitCanonicalPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func canonicalNamespaceFromPath(path string) string {
	parts := splitCanonicalPath(path)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func collectHierarchyPairsForNamespace(fragments []*models.HorizonFragment, namespace string, minDepth int) ([][2]string, bool) {
	if len(fragments) <= 1 {
		return nil, false
	}
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	unique := map[string]struct{}{}
	for _, f := range fragments {
		if f == nil {
			continue
		}
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp == "" || !strings.HasPrefix(cp, "/") {
			continue
		}
		parts := splitCanonicalPath(cp)
		if len(parts) < minDepth {
			continue
		}
		if namespace != "" && strings.ToLower(strings.TrimSpace(parts[0])) != namespace {
			continue
		}
		unique[cp] = struct{}{}
	}
	if len(unique) <= 1 {
		return nil, false
	}

	paths := make([]string, 0, len(unique))
	for p := range unique {
		paths = append(paths, p)
	}

	hierarchyDetected := false
	pairSet := map[string][2]string{}
	for _, child := range paths {
		bestParent := ""
		bestDepth := -1
		for _, parent := range paths {
			if parent == child {
				continue
			}
			if !strings.HasPrefix(child, parent+"/") {
				continue
			}
			hierarchyDetected = true
			depth := len(splitCanonicalPath(parent))
			if depth > bestDepth {
				bestDepth = depth
				bestParent = parent
			}
		}
		if bestParent == "" {
			continue
		}
		key := bestParent + "->" + child
		pairSet[key] = [2]string{bestParent, child}
	}
	if !hierarchyDetected || len(pairSet) == 0 {
		return nil, hierarchyDetected
	}

	keys := make([]string, 0, len(pairSet))
	for k := range pairSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, pairSet[k])
	}
	return pairs, true
}

func collectNamespaceHierarchyPairs(fragments []*models.HorizonFragment, minDepth int) (string, [][2]string, bool) {
	if len(fragments) <= 1 {
		return "", nil, false
	}
	uniqueNS := map[string]struct{}{}
	for _, f := range fragments {
		if f == nil {
			continue
		}
		ns := canonicalNamespaceFromPath(f.CanonicalPath)
		if ns == "" {
			continue
		}
		uniqueNS[ns] = struct{}{}
	}
	if len(uniqueNS) != 1 {
		return "", nil, false
	}
	namespace := ""
	for ns := range uniqueNS {
		namespace = ns
	}
	// MARKET hierarchy is handled in a dedicated block where HAS_SUBMARKET edges are enforced.
	if namespace == "market" {
		return "", nil, false
	}
	pairs, ok := collectHierarchyPairsForNamespace(fragments, namespace, minDepth)
	return namespace, pairs, ok
}

// detectDirectMarketHierarchy returns true when all market paths in a cluster fit:
// parent path + exactly one extra segment for each child.
func detectDirectMarketHierarchy(fragments []*models.HorizonFragment) (parentPath string, childPaths map[string]struct{}, ok bool) {
	if len(fragments) <= 1 {
		return "", nil, false
	}
	unique := map[string]struct{}{}
	for _, f := range fragments {
		if f == nil {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/market/") {
			return "", nil, false
		}
		parts := splitCanonicalPath(f.CanonicalPath)
		if len(parts) < 4 || parts[0] != "market" {
			return "", nil, false
		}
		unique[strings.TrimSpace(f.CanonicalPath)] = struct{}{}
	}
	if len(unique) <= 1 {
		return "", nil, false
	}

	shortest := ""
	shortLen := 0
	tie := false
	for p := range unique {
		n := len(splitCanonicalPath(p))
		if shortest == "" || n < shortLen {
			shortest = p
			shortLen = n
			tie = false
			continue
		}
		if n == shortLen && p != shortest {
			tie = true
		}
	}
	if shortest == "" || tie {
		return "", nil, false
	}

	children := map[string]struct{}{}
	parentParts := splitCanonicalPath(shortest)
	for p := range unique {
		if p == shortest {
			continue
		}
		pp := splitCanonicalPath(p)
		if len(pp) != len(parentParts)+1 {
			return "", nil, false
		}
		for i := range parentParts {
			if pp[i] != parentParts[i] {
				return "", nil, false
			}
		}
		children[p] = struct{}{}
	}
	if len(children) == 0 {
		return "", nil, false
	}
	return shortest, children, true
}

// collectMarketHierarchyPairs returns nearest parent->child canonicalPath pairs for market paths in the cluster.
// A hierarchy is detected when at least one path is a strict prefix of another.
func collectMarketHierarchyPairs(fragments []*models.HorizonFragment) ([][2]string, bool) {
	return collectHierarchyPairsForNamespace(fragments, "market", 4)
}

func ensureHasSubmarketEdge(ctx context.Context, edgesRepo *repo.DependencyEdgesRepository, tenantID, parentID, childID string, apply bool) (bool, error) {
	parentID = strings.TrimSpace(parentID)
	childID = strings.TrimSpace(childID)
	if parentID == "" || childID == "" || parentID == childID {
		return false, nil
	}

	existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, parentID, childID, models.DependencyTypeHasSubmarket)
	if err == nil && existing != nil {
		return false, nil
	}
	if err != nil && !errors.Is(err, repo.ErrEdgeNotFound) {
		return false, err
	}

	if !apply {
		return true, nil
	}

	now := time.Now().UTC()
	_, err = edgesRepo.Create(ctx, &models.DependencyEdge{
		TenantID:         tenantID,
		SourceFragmentID: parentID,
		TargetFragmentID: childID,
		Type:             models.DependencyTypeHasSubmarket,
		Properties: models.EdgeProperties{
			Criticality:   0.6,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"inferred": true,
			"reason":   "market.parent_child.hierarchy.inferred_by_dedup",
		},
		Provenance: models.Provenance{
			Method: "horizon_smart_dedup_market_hierarchy",
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// selectCanonicalByScore chooses canonical based on data quality score
// profileCache: map of fragment IDs that have profiles (for priority)
func selectCanonicalByScore(fragments []*models.HorizonFragment, profileCache map[string]bool) string {
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

		// HIGHEST PRIORITY: Prefer fragments with profiles (+200)
		// This ensures that fragments with profiles are always chosen over those without
		if profileCache != nil && profileCache[f.ID] {
			score += 200
		}

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

		// Prefer proper /loc/{cc}/{admin1}/{city} format (ISO2/composite/regional cc).
		// This is the canonical form; other /loc/* shapes are legacy.
		if isCanonicalLocPath(f.CanonicalPath) {
			score += 100 // Canonical /loc/{cc}/{admin1}/{city} format — highest priority!
		} else if strings.HasPrefix(f.CanonicalPath, "/loc/") {
			score += 30 // Legacy /loc/* variants
		}

		// For PRODUCT: prefer canonical taxonomy shapes (hierarchical/service) over legacy/flat.
		if cp := canonicalPartsFromPath(f.CanonicalPath); cp != nil && cp.Namespace == "product" && cp.Product != nil {
			pp := cp.Product
			switch pp.Shape {
			case "hierarchical":
				score += 100 // /product/{category}/{type}/{stage}/{slug}
				if len(pp.Tail) > 0 {
					score += 5 * minInt(len(pp.Tail), 3) // extra specificity (model/SKU)
				}
			case "service":
				score += 90 // /product/service/{domain}/{subdomain}/{slug}
				if len(pp.Tail) > 0 || strings.TrimSpace(pp.ServiceOffering) != "" {
					score += 5
				}
			case "flat":
				score += 60 // /product/{slug}
			case "org_scoped_flat":
				score += 30 // Legacy /product/{org}/{slug}
			default:
				score += 10
			}
		} else if strings.HasPrefix(f.CanonicalPath, "/product/") {
			parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
			switch len(parts) {
			case 5:
				score += 100
			case 4:
				score += 80
			case 2:
				score += 60
			case 3:
				score += 30
			default:
				if len(parts) > 5 {
					score += 70
				} else {
					score += 10
				}
			}
		}

		// For ORGANIZATION: prefer higher-quality canonical nodes over low-signal LLM stubs.
		if f.Type == models.FragmentTypeOrganization {
			// Prefer geo-scoped canonical paths over legacy flat paths (namespace-aware).
			if cp := canonicalPartsFromPath(f.CanonicalPath); cp != nil {
				switch cp.Namespace {
				case "org":
					if cp.Org != nil && !cp.Org.Legacy && strings.TrimSpace(cp.Org.Geo) != "" {
						score += 60 // /org/{geo}/{slug}
					}
				case "gov":
					if cp.Gov != nil && strings.TrimSpace(cp.Gov.Geo) != "" {
						score += 60 // /gov/{geo}/{slug}
					}
				}
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

// filterMarketByScope applies conservative scope checks for MARKET name-based clusters.
// Name-only equality is too broad for markets; we only allow merges when:
// - all candidates have a resolvable market geo token,
// - all candidates share the same geo token,
// - all canonical (non-legacy) candidates share the same normalized market domain.
func filterMarketByScope(fragments []*models.HorizonFragment) ([]*models.HorizonFragment, bool, string) {
	if len(fragments) <= 1 {
		return fragments, false, ""
	}

	geos := map[string]struct{}{}
	domains := map[string]struct{}{}
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeMarket {
			continue
		}
		cp := canonicalPartsForFragment(f)
		if cp == nil || cp.Namespace != "market" || cp.Market == nil {
			continue
		}
		mp := cp.Market
		geo := strings.ToLower(strings.TrimSpace(mp.Geo))
		if geo != "" && geo != "na" {
			geos[geo] = struct{}{}
		}
		if !mp.Legacy {
			domain := taxonomy.NormalizeMarketDomain(strings.ToLower(strings.TrimSpace(mp.Domain)))
			if domain != "" {
				domains[domain] = struct{}{}
			}
		}
	}

	// If we cannot establish geo scope, skip to avoid broad false merges.
	if len(geos) == 0 {
		return nil, true, "market_geo_unknown"
	}
	if len(geos) > 1 {
		return nil, true, fmt.Sprintf("market_geo_conflict=%v", keysSorted(geos))
	}
	if len(domains) > 1 {
		return nil, true, fmt.Sprintf("market_domain_conflict=%v", keysSorted(domains))
	}
	return fragments, false, ""
}

// hasFlatProductAnchor returns true when at least one PRODUCT path in a cluster
// is flat/legacy. This is used to keep slug-only productName dedup conservative.
func hasFlatProductAnchor(fragments []*models.HorizonFragment) bool {
	for _, f := range fragments {
		if f == nil || f.Type != models.FragmentTypeProduct {
			continue
		}
		cp := canonicalPartsForFragment(f)
		if cp == nil || cp.Namespace != "product" || cp.Product == nil {
			continue
		}
		shape := strings.ToLower(strings.TrimSpace(cp.Product.Shape))
		if shape == "flat" || shape == "org_scoped_flat" {
			return true
		}
	}
	return false
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
	if cp := canonicalPartsFromPath(path); cp != nil && cp.Namespace == "loc" && cp.Loc != nil {
		return normalizeGeoTokenForDedup(strings.TrimSpace(cp.Loc.Country))
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}
	return normalizeGeoTokenForDedup(parts[1])
}

func normalizeGeoTokenForDedup(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || token == "na" {
		return ""
	}
	if len(token) == 2 && token >= "aa" && token <= "zz" {
		return token
	}
	if taxonomy.IsCompositeGeoToken(token) || taxonomy.IsRegionalGeoToken(token) {
		return token
	}
	if iso2 := iso3ToIso2(token); iso2 != "" {
		return iso2
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

// fixGeoPath attempts to normalize a geo path into canonical /loc/{cc}/na/na when possible.
// Returns empty string when it cannot safely canonicalize.
func fixGeoPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	// Already canonical
	if isCanonicalLocPath(path) {
		return path
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "loc" {
		// /loc/{iso3}/... → normalize to ISO2 when possible
		if len(parts) >= 4 {
			cc := strings.ToLower(strings.TrimSpace(parts[1]))
			if iso2 := iso3ToIso2(cc); iso2 != "" {
				parts[1] = iso2
				candidate := "/" + strings.Join(parts, "/")
				if isCanonicalLocPath(candidate) {
					return candidate
				}
			}
		}
		// /loc/{something} — try to resolve {something} as a country name/alias.
		if len(parts) == 2 {
			countrySlug := strings.ReplaceAll(parts[1], "-", " ")
			countrySlug = strings.ReplaceAll(countrySlug, "_", " ")
			if canonical := scout.GetCountryCanonicalPath(countrySlug); canonical != "" {
				return canonical
			}
		}
		return ""
	}

	// /region/{something} legacy
	if strings.HasPrefix(path, "/region/") {
		slug := strings.TrimSpace(strings.TrimPrefix(path, "/region/"))
		slug = strings.ReplaceAll(slug, "-", " ")
		slug = strings.ReplaceAll(slug, "_", " ")
		if canonical := scout.GetCountryCanonicalPath(slug); canonical != "" {
			return canonical
		}
		return ""
	}

	// Generic "/{something}" fallback: try country resolution.
	if strings.HasPrefix(path, "/") {
		slug := strings.TrimSpace(strings.TrimPrefix(path, "/"))
		if slug == "" || strings.Contains(slug, "/") {
			return ""
		}
		slug = strings.ReplaceAll(slug, "-", " ")
		slug = strings.ReplaceAll(slug, "_", " ")
		if canonical := scout.GetCountryCanonicalPath(slug); canonical != "" {
			return canonical
		}
	}

	return ""
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

// deleteEdgesForFragment removes all edges where fragment is source or target.
func deleteEdgesForFragment(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID, fragmentID string) (deleted int, err error) {
	if db == nil || edgesRepo == nil || strings.TrimSpace(fragmentID) == "" {
		return 0, nil
	}
	edgesCol := db.Collection("horizon_dependency_edges")

	q1 := edgesCol.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", fragmentID)
	docs1, err := q1.Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}
	for _, d := range docs1 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		if strings.TrimSpace(edge.ID) == "" {
			continue
		}
		if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
			deleted++
		}
	}

	q2 := edgesCol.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", fragmentID)
	docs2, err := q2.Documents(ctx).GetAll()
	if err != nil {
		return deleted, err
	}
	for _, d := range docs2 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		if strings.TrimSpace(edge.ID) == "" {
			continue
		}
		if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
			deleted++
		}
	}

	return deleted, nil
}

// reassignEdges moves edges from oldFragmentID to newFragmentID
func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID, oldID, newID string) (reassigned, deleted int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")

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

// normalizeLegalName removes common suffixes like JSC, PJSC, LLC, Inc, etc.,
// strips parenthetical annotations like "(formerly ...)", and normalizes spacing/punctuation.
// Delegates to scout.NormalizeLegalName for consistency with the ingestion-time deduplicator.
// Additional suffixes (holding, group, international) are stripped on top for the batch tool's
// more aggressive dedup context (these are intentionally excluded from the ingestion-time normalizer
// to avoid false merges on the hot path).
func normalizeLegalName(name string) string {
	// Use the canonical normalizer (strips legal suffixes + parenthetical annotations).
	normalized := scout.NormalizeLegalName(name)
	if normalized == "" {
		return ""
	}

	// Additional aggressive suffixes for batch dedup (not safe for real-time ingestion).
	extraSuffixes := []string{
		" holding", " holdings", " group", " international",
		" sas", " a/s", " kft", " zrt",
		" sp z oo", " spzoo",
		" pty", " pte", " pte ltd",
	}
	for _, suffix := range extraSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			normalized = strings.TrimSuffix(normalized, suffix)
			break
		}
	}

	return strings.TrimSpace(normalized)
}

// =============================================================================
// FACILITY normalized-name helpers (conservative)
// =============================================================================

// extractFacilityNameFromPath returns the last slug segment from /fac/{org}/{slug}.
func extractFacilityNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if cp := canonicalPartsFromPath(path); cp != nil && cp.Namespace == "fac" && cp.Fac != nil {
		slug := strings.ToLower(strings.TrimSpace(cp.Fac.FacilitySlug))
		if slug == "" || slug == "fac" || slug == "na" {
			return ""
		}
		return slug
	}
	if !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "fac" {
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
		cc = strings.ToLower(strings.TrimSpace(cc))
		// Must be ISO alpha-2, composite, or regional token (but NOT "na")
		if cc == "na" || (len(cc) != 2 && !taxonomy.IsCompositeGeoToken(cc) && !taxonomy.IsRegionalGeoToken(cc)) {
			return "" // Malformed path like /loc/grapevine/tx/us or /loc/na/us/nv — skip
		}
		if len(cc) == 2 && (cc < "aa" || cc > "zz") {
			return ""
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
	if path == "" {
		return ""
	}
	if cp := canonicalPartsFromPath(path); cp != nil && cp.Namespace == "product" && cp.Product != nil {
		pp := cp.Product
		switch pp.Shape {
		case "hierarchical":
			if pp.Slug != "" {
				return strings.ToLower(pp.Slug)
			}
		case "service":
			if pp.ServiceSlug != "" {
				return strings.ToLower(pp.ServiceSlug)
			}
			if pp.Slug != "" {
				return strings.ToLower(pp.Slug)
			}
		case "flat":
			if pp.Slug != "" {
				return strings.ToLower(pp.Slug)
			}
		case "org_scoped_flat":
			// Legacy org-scoped product paths are noisy; skip for name extraction.
			return ""
		}
	}
	if !strings.HasPrefix(path, "/product/") {
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

// isProductPathEndingWithNA returns true if canonical path ends with /na (case-insensitive).
func isProductPathEndingWithNA(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "product" {
		return false
	}
	last := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	return last == "na"
}

// extractCanonicalProductSlugForDedup returns the product slug ONLY for canonical product namespaces:
// - /product/{slug}
// - /product/{category}/{type}/{stage}/{slug}
//
// Returns "" for legacy org-nested paths like /product/{org}/{slug} to avoid false-positive merges.
func extractCanonicalProductSlugForDedup(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if cp := canonicalPartsFromPath(path); cp != nil && cp.Namespace == "product" && cp.Product != nil {
		pp := cp.Product
		switch pp.Shape {
		case "hierarchical":
			return strings.ToLower(strings.TrimSpace(pp.Slug))
		case "service":
			return strings.ToLower(strings.TrimSpace(pp.ServiceSlug))
		case "flat":
			return strings.ToLower(strings.TrimSpace(pp.Slug))
		case "org_scoped_flat":
			// Legacy org-scoped paths are excluded to avoid false positives.
			return ""
		default:
			return ""
		}
	}
	if !strings.HasPrefix(path, "/product/") {
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
	if path == "" {
		return ""
	}
	if cp := canonicalPartsFromPath(path); cp != nil && cp.Namespace == "person" && cp.Person != nil {
		return strings.TrimSpace(cp.Person.Slug)
	}
	if !strings.HasPrefix(path, "/person/") {
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
func fixEdgeDirections(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, fragmentTypes map[string]models.FragmentType, tenantID string, canonicalizeTypes bool, validationExecutor *policy.PolicyExecutor, canonExecutor *policy.PolicyExecutor) (int, int, error) {
	log.Printf("Fixing edge directions with pagination (page size: 100)...")
	log.Printf("Edge canonicalization enabled: %t", canonicalizeTypes)
	log.Printf("Using pre-loaded fragment type cache with %d entries", len(fragmentTypes))

	const pageSize = 100
	reversed := 0
	canonicalized := 0
	totalChecked := 0
	var lastDoc *firestore.DocumentSnapshot
	logPrefix := "[horizon_smart_dedup:fixEdgeDirections]"

	// Best-effort type resolver: when horizon_smart_dedup is run with --type filter,
	// fragmentTypes cache can be incomplete, but edge direction fixing must remain global.
	typeCache := fragmentTypes
	if typeCache == nil {
		typeCache = make(map[string]models.FragmentType)
	}
	pathCache := make(map[string]string, len(typeCache))
	needPath := canonicalizeTypes || validationExecutor != nil
	resolveEndpoint := func(id string) (models.FragmentType, string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return "", ""
		}
		cachedType := typeCache[id]
		cachedPath := strings.TrimSpace(pathCache[id])
		if cachedType != "" && (!needPath || cachedPath != "") {
			if cachedPath != "" {
				if reconciled, changed := edgesemantics.ReconcileTypeWithCanonicalPath(cachedType, cachedPath); changed {
					cachedType = reconciled
					typeCache[id] = reconciled
				}
			}
			return cachedType, cachedPath
		}

		snap, err := db.Collection("horizon_fragments").Doc(id).Get(ctx)
		if err != nil || snap == nil || !snap.Exists() {
			return cachedType, cachedPath
		}
		data := snap.Data()
		if cachedType == "" {
			if v, ok := data["type"].(string); ok {
				cachedType = models.FragmentType(strings.TrimSpace(v))
			}
		}
		if cachedPath == "" {
			if v, ok := data["canonicalPath"].(string); ok {
				cachedPath = strings.TrimSpace(v)
			}
		}
		if cachedPath != "" && cachedType != "" {
			if reconciled, changed := edgesemantics.ReconcileTypeWithCanonicalPath(cachedType, cachedPath); changed {
				cachedType = reconciled
			}
		}
		if cachedType != "" {
			typeCache[id] = cachedType
		}
		if cachedPath != "" {
			pathCache[id] = cachedPath
		}
		return cachedType, cachedPath
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

			sourceType, sourcePath := resolveEndpoint(edge.SourceFragmentID)
			targetType, targetPath := resolveEndpoint(edge.TargetFragmentID)

			if sourceType == "" || targetType == "" {
				continue // Skip if we can't determine types (orphaned edge or deleted fragment)
			}

			edgeChanged := false
			edgeCanonicalized := false
			policyStatus := models.PolicyStatusClean
			policyReason := ""
			policyPenalty := 0.0
			policyEdgeModality := ""
			canonFromEdgeType := models.DependencyType("")
			normSource, normTarget, normType, symSwapped := scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
			if symSwapped {
				edge.SourceFragmentID = normSource
				edge.TargetFragmentID = normTarget
				edge.Type = normType
				sourceType, targetType = targetType, sourceType
				sourcePath, targetPath = targetPath, sourcePath
				edgeChanged = true
			}

			// Core canonization: normalize relation type
			if canonExecutor != nil {
				if normalized := normalizeRelationTypeWithPolicy(ctx, canonExecutor, edge.Type); normalized != "" && normalized != string(edge.Type) {
					canonFromEdgeType = edge.Type
					edge.Type = models.DependencyType(normalized)
					edgeChanged = true
					edgeCanonicalized = true
				}
			}

			// Core edge canonicalization: path-aware matrix shared with runtime ingestion/repo.
			if canonicalizeTypes {
				rawRelationType := strings.TrimSpace(string(edge.Type))
				if edge.Qualifiers != nil {
					if v, ok := edge.Qualifiers["rawRelationType"].(string); ok && strings.TrimSpace(v) != "" {
						rawRelationType = strings.TrimSpace(v)
					}
				}
				canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
					EdgeType:          edge.Type,
					SourceType:        sourceType,
					TargetType:        targetType,
					SourcePath:        sourcePath,
					TargetPath:        targetPath,
					RawRelationType:   rawRelationType,
					OwnershipEvidence: models.HasOwnershipEvidence("", edge.Qualifiers, edge.Metrics),
				})
				if canon.Changed {
					origType := edge.Type
					if canon.OriginalType != "" {
						canonFromEdgeType = canon.OriginalType
					} else if canonFromEdgeType == "" {
						canonFromEdgeType = origType
					}
					edge.Type = canon.EdgeType
					if canon.ShouldSwap {
						edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
						sourceType, targetType = targetType, sourceType
						sourcePath, targetPath = targetPath, sourcePath
						reversed++
					}
					edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
					if edge.Qualifiers == nil {
						edge.Qualifiers = map[string]interface{}{}
					}
					edge.Qualifiers["canonicalized"] = true
					edge.Qualifiers["canonicalizedFromEdgeType"] = string(canonFromEdgeType)
					edge.Qualifiers["canonicalizationReason"] = canon.Reason
					edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
					if canon.ShouldSwap {
						edge.Qualifiers["normalizedDirection"] = true
					}
					edgeChanged = true
					edgeCanonicalized = true
				}
				if canon.PolicyPenalty > 0 {
					policyStatus = policy.MergePolicyStatus(policyStatus, canon.PolicyStatus)
					if canon.PolicyPenalty > policyPenalty {
						policyPenalty = canon.PolicyPenalty
					}
					if strings.TrimSpace(canon.PolicyReason) != "" {
						policyReason = strings.TrimSpace(canon.PolicyReason)
					}
					if edge.Properties.Criticality > 0 && canon.ConfidenceMult > 0 && canon.ConfidenceMult < 1 {
						edge.Properties.Criticality *= canon.ConfidenceMult
					}
				}
				if canon.PolicyReason == "transition_unclassified" {
					policy.LogPolicyEvent("edge_canonicalization_policy", policy.PolicyLogStageFallback, map[string]interface{}{
						"trace_id":       "na",
						"tenant_id":      edge.TenantID,
						"job_id":         "na",
						"edge_id":        edge.ID,
						"source_type":    sourceType,
						"target_type":    targetType,
						"relation_type":  edge.Type,
						"action":         policy.ActionAllowWithPenalty,
						"policy_status":  policyStatus,
						"policy_reason":  canon.PolicyReason,
						"policy_penalty": policyPenalty,
						"decision_path":  "horizon_smart_dedup_fix_edge_directions",
						"confidence":     canon.ConfidenceMult,
					})
				}
			}

			// Core validation: use unified action-contract and best-effort apply handlers.
			if validationExecutor != nil {
				decision, err := validationExecutor.Execute(ctx, policy.PolicyInput{
					Kind: "edge",
					Edge: &edge,
					Context: map[string]interface{}{
						"validationKind":      "edge",
						"sourceFragmentID":    edge.SourceFragmentID,
						"targetFragmentID":    edge.TargetFragmentID,
						"sourceType":          string(sourceType),
						"targetType":          string(targetType),
						"sourceCanonicalPath": sourcePath,
						"targetCanonicalPath": targetPath,
						"relationType":        string(edge.Type),
					},
				})
				if err != nil {
					policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageError, map[string]interface{}{
						"trace_id":       "na",
						"tenant_id":      edge.TenantID,
						"job_id":         "na",
						"edge_id":        edge.ID,
						"source_type":    sourceType,
						"target_type":    targetType,
						"relation_type":  edge.Type,
						"action":         "validate_edge",
						"policy_status":  "na",
						"policy_reason":  err.Error(),
						"policy_penalty": 0,
						"decision_path":  "horizon_smart_dedup_fix_edge_directions",
						"error_class":    "policy_execution_error",
						"confidence":     0,
					})
				} else {
					decision = policy.NormalizeValidationDecision(decision)
					reason := ""
					if rv, _ := decision.Params["reason"].(string); strings.TrimSpace(rv) != "" {
						reason = strings.TrimSpace(rv)
					}
					policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageDecision, map[string]interface{}{
						"trace_id":       "na",
						"tenant_id":      edge.TenantID,
						"job_id":         "na",
						"edge_id":        edge.ID,
						"source_type":    sourceType,
						"target_type":    targetType,
						"relation_type":  edge.Type,
						"action":         decision.Action,
						"policy_status":  policyStatus,
						"policy_reason":  reason,
						"policy_penalty": policyPenalty,
						"decision_path":  decision.DecisionPath,
						"confidence":     decision.Confidence,
					})

					prevSource := edge.SourceFragmentID
					prevTarget := edge.TargetFragmentID
					prevType := edge.Type
					prevCriticality := edge.Properties.Criticality

					policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
						Reverse: func() {
							edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
							sourceType, targetType = targetType, sourceType
							sourcePath, targetPath = targetPath, sourcePath
							reversed++
						},
						RewriteType: func(newType string, _ string) {
							newEdgeType := models.DependencyType(strings.TrimSpace(newType))
							if newEdgeType == "" || newEdgeType == edge.Type {
								return
							}
							if canonFromEdgeType == "" {
								canonFromEdgeType = edge.Type
							}
							edge.Type = newEdgeType
						},
						ApplyPenalty: func(penalty float64, _ string) {
							if edge.Properties.Criticality > 0 {
								edge.Properties.Criticality *= (1.0 - penalty)
							}
						},
						SetPolicyStatus: func(status, reason string, penalty float64) {
							policyStatus = policy.MergePolicyStatus(policyStatus, status)
							if strings.TrimSpace(reason) != "" {
								policyReason = strings.TrimSpace(reason)
							}
							if penalty > policyPenalty {
								policyPenalty = penalty
							}
						},
					})

					if modality, _ := decision.Params["edgeModality"].(string); strings.TrimSpace(modality) != "" {
						policyEdgeModality = strings.TrimSpace(modality)
					}

					if edge.SourceFragmentID != prevSource || edge.TargetFragmentID != prevTarget || edge.Type != prevType {
						edgeChanged = true
					}
					if edge.Type != prevType {
						edgeCanonicalized = true
					}
					if edge.Properties.Criticality != prevCriticality {
						edgeChanged = true
					}
				}
			}

			if policyEdgeModality != "" {
				if edge.Qualifiers == nil {
					edge.Qualifiers = map[string]interface{}{}
				}
				if _, ok := edge.Qualifiers["edgeModality"]; !ok {
					edge.Qualifiers["edgeModality"] = policyEdgeModality
					edgeChanged = true
				}
			}
			if policyStatus != models.PolicyStatusClean || policyPenalty > 0 || strings.TrimSpace(policyReason) != "" {
				edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
				edgeChanged = true
			}
			if canonFromEdgeType != "" {
				edge.SetPolicyOriginalType(canonFromEdgeType)
				edgeChanged = true
			}

			if edgeChanged {
				edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to update edge %s: %v", edge.ID, err)
					continue
				}
				if edgeCanonicalized {
					canonicalized++
				}
				if *verbose {
					log.Printf("  ✓ Updated edge %s: %s -> %s (%s)", edge.ID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
				}
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

// =============================================================================
// Step 7: Market geo-mismatch cleanup + market domain normalization
// =============================================================================
//
// 7a) For each ORG fragment, check its PARTICIPATES_IN edges to MARKET fragments.
//     If the market's geo doesn't match the org's geo AND isn't "global"/"int" → delete the edge.
//
// 7b) For each MARKET fragment with non-canonical domain (e.g., /market/sa/financials/fintech),
//     rewrite the canonical path to use the normalized domain (/market/sa/finance/fintech),
//     and merge with existing fragment at the canonical path if one exists.

func cleanMarketEdgesAndDomains(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID string, apply bool, verbose bool) (geoMismatchCleaned int, domainNormalized int) {
	fragmentsRepo := repo.NewHorizonFragmentsRepository(db)
	participatesIn := models.DependencyTypeParticipatesIn

	// -------------------------------------------------------------------------
	// 7a: Market geo-mismatch cleanup
	// -------------------------------------------------------------------------
	log.Printf("  Step 7a: Checking org→market geo-mismatch edges...")

	// Load all ORG fragments (only need ID + canonical path)
	orgCol := db.Collection("horizon_fragments")
	orgQuery := orgCol.Where("tenantId", "==", tenantID).Where("type", "==", string(models.FragmentTypeOrganization))
	orgIter := orgQuery.Documents(ctx)
	defer orgIter.Stop()

	orgChecked := 0
	for {
		snap, err := orgIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("  ERROR loading org fragment: %v", err)
			break
		}
		if snap == nil || !snap.Exists() {
			continue
		}
		orgID := snap.Ref.ID
		cpRaw, _ := snap.Data()["canonicalPath"].(string)
		orgGeo := extractGeoFromCanonicalPath(cpRaw)
		if orgGeo == "" || orgGeo == "na" {
			// Also try jurisdictionIso2 identifier
			if ids, ok := snap.Data()["identifiers"].(map[string]interface{}); ok {
				if iso2, ok := ids["jurisdictionIso2"].(string); ok && len(strings.TrimSpace(iso2)) == 2 {
					orgGeo = strings.ToLower(strings.TrimSpace(iso2))
				}
			}
		}
		if orgGeo == "" || orgGeo == "na" {
			continue // Can't validate without known org geo
		}
		// Global/international orgs can participate in ANY regional market — skip them.
		if orgGeo == "global" || orgGeo == "int" {
			continue
		}
		// Regional/composite geo tokens operate across multiple countries,
		// so strict geo-mismatch check is not meaningful.
		if taxonomy.IsRegionalGeoToken(orgGeo) || taxonomy.IsCompositeGeoToken(orgGeo) {
			continue
		}

		orgChecked++
		if orgChecked%2000 == 0 {
			log.Printf("  7a progress: checked %d orgs, cleaned %d edges", orgChecked, geoMismatchCleaned)
		}

		// Get PARTICIPATES_IN edges from this org
		edges, err := edgesRepo.ListBySource(ctx, tenantID, orgID, &participatesIn, 100)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
				continue
			}
			// Look up target (market) fragment canonical path
			targetSnap, err := db.Collection("horizon_fragments").Doc(edge.TargetFragmentID).Get(ctx)
			if err != nil || targetSnap == nil || !targetSnap.Exists() {
				continue
			}
			targetCP, _ := targetSnap.Data()["canonicalPath"].(string)
			if !strings.HasPrefix(targetCP, "/market/") {
				continue // Not a market
			}
			marketGeo := extractGeoFromCanonicalPath(targetCP)
			if marketGeo == "" || marketGeo == "global" || marketGeo == "int" {
				continue // Global market — always OK
			}
			if marketGeo == orgGeo {
				continue // Same geo — OK
			}
			// Composite market geo: allow if org country is one of composite members.
			// Example: ORG geo=ma is valid for market geo=ma-sa.
			if compositeGeoContainsCountry(marketGeo, orgGeo) {
				continue
			}
			// Regional market: allow if org's country is a member of that region.
			// E.g. BH org → /market/gcc/… is OK because Bahrain is a GCC member.
			if taxonomy.IsCountryInRegion(marketGeo, orgGeo) {
				continue
			}

			// Mismatch: org(sa) → market(kz) — delete this edge
			if verbose {
				log.Printf("  7a: GEO MISMATCH org=%s (geo=%s) → market=%s (geo=%s) — %s edge %s",
					cpRaw, orgGeo, targetCP, marketGeo,
					func() string {
						if apply {
							return "DELETING"
						}
						return "would delete"
					}(), edge.ID)
			}
			if apply {
				if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
					if verbose {
						log.Printf("  7a: ERROR deleting edge %s: %v", edge.ID, err)
					}
					continue
				}
			}
			geoMismatchCleaned++
		}
	}
	log.Printf("  7a: Checked %d orgs, cleaned %d geo-mismatch edges", orgChecked, geoMismatchCleaned)

	// -------------------------------------------------------------------------
	// 7b: Market domain normalization
	// -------------------------------------------------------------------------
	log.Printf("  Step 7b: Normalizing market fragment domains...")

	marketCol := db.Collection("horizon_fragments")
	marketQuery := marketCol.Where("tenantId", "==", tenantID).Where("type", "==", string(models.FragmentTypeMarket))
	marketIter := marketQuery.Documents(ctx)
	defer marketIter.Stop()

	marketChecked := 0
	for {
		snap, err := marketIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("  ERROR loading market fragment: %v", err)
			break
		}
		if snap == nil || !snap.Exists() {
			continue
		}
		marketID := snap.Ref.ID
		cpRaw, _ := snap.Data()["canonicalPath"].(string)
		cpRaw = strings.TrimSpace(cpRaw)
		if cpRaw == "" || !strings.HasPrefix(cpRaw, "/market/") {
			continue
		}

		parts := strings.Split(strings.Trim(cpRaw, "/"), "/")
		// /market/{geo}/{domain}/{vertical} — need at least 4 parts
		if len(parts) < 4 || parts[0] != "market" {
			continue
		}
		rawDomain := parts[2]
		normDomain := taxonomy.NormalizeMarketDomain(rawDomain)
		if normDomain == rawDomain {
			continue // Already canonical
		}

		// Build new canonical path
		parts[2] = normDomain
		newCP := "/" + strings.Join(parts, "/")

		marketChecked++
		if verbose {
			log.Printf("  7b: DOMAIN NORMALIZE %s → %s (%s)", cpRaw, newCP,
				func() string {
					if apply {
						return "APPLYING"
					}
					return "dry-run"
				}())
		}

		if !apply {
			domainNormalized++
			continue
		}

		// Check if a fragment with the canonical path already exists → merge into it
		existing, err := fragmentsRepo.GetByCanonicalPath(ctx, newCP)
		if err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" && existing.ID != marketID {
			// Merge: reassign edges from current to existing, then delete current
			if verbose {
				log.Printf("  7b: MERGE %s (%s) → existing %s (%s)", cpRaw, marketID, newCP, existing.ID)
			}
			// Reassign inbound edges
			inEdges, _ := edgesRepo.ListByTarget(ctx, tenantID, marketID, nil, 500)
			for _, e := range inEdges {
				if e == nil {
					continue
				}
				// Check if the same edge already exists to the target
				if _, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, e.SourceFragmentID, existing.ID, e.Type); err == nil {
					// Already exists — just delete the old edge
					_ = edgesRepo.Delete(ctx, e.ID)
					continue
				}
				e.TargetFragmentID = existing.ID
				_ = edgesRepo.Update(ctx, e)
			}
			// Reassign outbound edges
			outEdges, _ := edgesRepo.ListBySource(ctx, tenantID, marketID, nil, 500)
			for _, e := range outEdges {
				if e == nil {
					continue
				}
				if _, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, existing.ID, e.TargetFragmentID, e.Type); err == nil {
					_ = edgesRepo.Delete(ctx, e.ID)
					continue
				}
				e.SourceFragmentID = existing.ID
				_ = edgesRepo.Update(ctx, e)
			}
			// Delete the non-canonical market fragment
			_ = fragmentsRepo.Delete(ctx, marketID)
		} else {
			// No existing fragment at canonical path — update through repository to keep
			// canonicalParts/indexPaths/validation lifecycle consistent.
			current, err := fragmentsRepo.Get(ctx, marketID)
			if err != nil {
				if verbose {
					log.Printf("  7b: ERROR loading market %s for rename: %v", marketID, err)
				}
				continue
			}
			current.CanonicalPath = newCP
			if err := fragmentsRepo.Update(ctx, current); err != nil {
				if verbose {
					log.Printf("  7b: ERROR renaming %s: %v", marketID, err)
				}
				continue
			}
		}
		domainNormalized++
	}
	log.Printf("  7b: Normalized %d market domains", domainNormalized)

	return
}

// extractGeoFromCanonicalPath extracts the geo (2nd segment) from canonical paths like
// /org/{geo}/..., /market/{geo}/..., /gov/{geo}/..., /loc/{geo}/...
// Only returns valid geo tokens: 2-letter ISO2 codes, regional tokens (gcc, eu, asean, etc.),
// "global", or "int". Returns "" for legacy paths like /org/repsol where 2nd segment is not a geo.
func extractGeoFromCanonicalPath(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	// Validate: must be 2-letter ISO2, known regional token, "global", or "int"
	if geo == "global" || geo == "int" || geo == "na" {
		return geo
	}
	if len(geo) == 2 && geo[0] >= 'a' && geo[0] <= 'z' && geo[1] >= 'a' && geo[1] <= 'z' {
		return geo // Valid ISO2
	}
	// Composite and regional tokens are taxonomy-driven.
	if taxonomy.IsCompositeGeoToken(geo) || taxonomy.IsRegionalGeoToken(geo) {
		return geo
	}
	return "" // Not a valid geo (e.g., /org/repsol → "repsol" is not a geo)
}

// compositeGeoContainsCountry checks whether a composite geo token (e.g., "ma-sa")
// includes a country ISO2 code (e.g., "ma").
func compositeGeoContainsCountry(compositeGeo, countryISO2 string) bool {
	compositeGeo = strings.ToLower(strings.TrimSpace(compositeGeo))
	countryISO2 = strings.ToLower(strings.TrimSpace(countryISO2))
	if !taxonomy.IsCompositeGeoToken(compositeGeo) || !isISO2CountryToken(countryISO2) {
		return false
	}
	for _, token := range strings.Split(compositeGeo, "-") {
		if strings.TrimSpace(token) == countryISO2 {
			return true
		}
	}
	return false
}
