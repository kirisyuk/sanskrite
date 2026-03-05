package scout

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/merge"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CanonicalPathResolverStats is a lightweight in-memory counter snapshot for observability.
// It intentionally avoids introducing external metrics deps; higher-level services may scrape/log it.
type CanonicalPathResolverStats struct {
	ResolveCalls               int64
	CanonizationPolicyRewrites int64
	GeoStrictified             int64
	GeoNamesRewrites           int64
	ProductTaxonomyRewrites    int64
	FoundExistingByPath        int64
	OrgDedupHits               int64
	FoundExistingAfterDedup    int64
	RedirectHits               int64
	RedirectWrites             int64
}

// CanonicalPathResolver provides unified canonicalization and deduplication for canonical paths.
// This service ensures that all paths are normalized and deduplicated early in the pipeline,
// preventing duplicate fragments and inefficient processing.
type CanonicalPathResolver struct {
	fragmentsRepo      *repo.HorizonFragmentsRepository
	canonizationPolicy *policy.PolicyExecutor
	orgDeduplicator    *OrgDeduplicator
	geonamesClient     coreontology.GeoNamesClientInterface
	redirectsRepo      *repo.CanonicalPathRedirectsRepository
	telemetryRepo      *repo.HorizonTelemetryRepository

	// Stats counters (atomic; best-effort only).
	resolveCalls               int64
	canonizationPolicyRewrites int64
	geoStrictified             int64
	geoNamesRewrites           int64
	productTaxonomyRewrites    int64
	foundExistingByPath        int64
	orgDedupHits               int64
	foundExistingAfterDedup    int64
	redirectHits               int64
	redirectWrites             int64
}

// NewCanonicalPathResolver creates a new canonical path resolver.
func NewCanonicalPathResolver(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	canonizationPolicy *policy.PolicyExecutor,
	orgDeduplicator *OrgDeduplicator,
) *CanonicalPathResolver {
	return &CanonicalPathResolver{
		fragmentsRepo:      fragmentsRepo,
		canonizationPolicy: canonizationPolicy,
		orgDeduplicator:    orgDeduplicator,
	}
}

// WithGeoNamesClient wires GeoNames into canonical path resolution (used for deterministic /loc normalization).
func (r *CanonicalPathResolver) WithGeoNamesClient(client coreontology.GeoNamesClientInterface) *CanonicalPathResolver {
	if r == nil {
		return r
	}
	r.geonamesClient = client
	return r
}

// WithCanonicalPathRedirectsRepo wires a Firestore-backed redirect cache.
func (r *CanonicalPathResolver) WithCanonicalPathRedirectsRepo(repo *repo.CanonicalPathRedirectsRepository) *CanonicalPathResolver {
	if r == nil {
		return r
	}
	r.redirectsRepo = repo
	return r
}

// WithTelemetryRepo wires a unified telemetry sink (best-effort).
func (r *CanonicalPathResolver) WithTelemetryRepo(repo *repo.HorizonTelemetryRepository) *CanonicalPathResolver {
	if r == nil {
		return r
	}
	r.telemetryRepo = repo
	return r
}

// ResolveResult contains the result of canonical path resolution.
type ResolveResult struct {
	CanonicalPath      string                  // Final canonicalized and deduplicated path
	ExistingFragment   *models.HorizonFragment // Non-nil if an existing fragment was found (deduplication)
	NormalizationSteps []string                // Log of normalization steps applied
	Confidence         float64                 // Confidence in the resolution (0.0-1.0)
}

// Resolve resolves a canonical path by:
// 1. Normalizing the path using CanonizationPolicy
// 2. Checking for existing fragments (deduplication)
// 3. Returning the final canonical path and any existing fragment found
//
// This should be called early in the pipeline (e.g., in Lens Service before creating scouting jobs,
// in Intent Resolver after getting anchors from LLM, etc.) to ensure all subsequent processing
// operates on canonicalized and deduplicated paths.
func (r *CanonicalPathResolver) Resolve(
	ctx context.Context,
	inputPath string,
	fragmentType models.FragmentType,
) (*ResolveResult, error) {
	logPrefix := "[CanonicalPathResolver]"

	if r == nil {
		return &ResolveResult{
			CanonicalPath: strings.TrimSpace(inputPath),
			Confidence:    0.5, // Low confidence if resolver not configured
		}, nil
	}

	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	atomic.AddInt64(&r.resolveCalls, 1)

	result := &ResolveResult{
		CanonicalPath:      inputPath,
		NormalizationSteps: []string{},
		Confidence:         1.0,
	}

	// Step 0: Use known redirect mappings from previous pipeline runs (fast path).
	redirectHits := r.applyRedirects(ctx, result)

	// Step 1: Normalize path using CanonizationPolicy
	if r.canonizationPolicy != nil {
		basePath := strings.TrimSpace(result.CanonicalPath)
		decision, err := r.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "canonical_path",
				"inputValue": basePath,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			normalized, _ := decision.Params["output"].(string)
			if normalized != "" && normalized != basePath {
				atomic.AddInt64(&r.canonizationPolicyRewrites, 1)
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("canonization_policy: %s -> %s", basePath, normalized))
				result.CanonicalPath = normalized
				log.Printf("%s CanonizationPolicy normalized: %s -> %s", logPrefix, basePath, normalized)
			}
		} else if err != nil {
			log.Printf("%s WARNING: CanonizationPolicy failed: %v, using original path", logPrefix, err)
			result.Confidence *= 0.9 // Slight penalty for policy failure
		}
	}

	// Step 1.5: Normalize GEO paths into canonical variable-depth /loc hierarchy.
	if fragmentType == models.FragmentTypeGeoRegion {
		if norm := normalizeGeoPathToStrict(result.CanonicalPath); norm != "" && norm != result.CanonicalPath {
			atomic.AddInt64(&r.geoStrictified, 1)
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("geo_normalize: %s -> %s", result.CanonicalPath, norm))
			result.CanonicalPath = norm
		}
	}

	// Step 1.55: GeoNames-backed GEO normalization/validation for /loc paths.
	if fragmentType == models.FragmentTypeGeoRegion && r.geonamesClient != nil {
		if norm, step, confPenalty := normalizeGeoPathViaGeoNames(ctx, result.CanonicalPath, r.geonamesClient); norm != "" && norm != result.CanonicalPath {
			atomic.AddInt64(&r.geoNamesRewrites, 1)
			result.NormalizationSteps = append(result.NormalizationSteps, step)
			result.CanonicalPath = norm
			if confPenalty > 0 {
				result.Confidence *= (1.0 - confPenalty)
			}
		}
	}

	// Step 1.6: Best-effort PRODUCT normalization to a stable shape.
	// Handles:
	// - flat /product/{slug} -> hierarchical taxonomy when possible
	// - legacy/malformed product paths (e.g. /product/{x}/{y}/{slug}) -> stable canonical fallback
	if fragmentType == models.FragmentTypeProduct {
		if norm := normalizeProductPathForResolve(ctx, result.CanonicalPath); norm != "" && norm != result.CanonicalPath {
			atomic.AddInt64(&r.productTaxonomyRewrites, 1)
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("product_taxonomy: %s -> %s", result.CanonicalPath, norm))
			result.CanonicalPath = norm
		}
	}

	// Step 1.7: Best-effort namespace/type coercion for obvious mismatches.
	// Example: type=ORGANIZATION with path=/product/... should be rewritten to /org/na/{slug}.
	if fixed, changed := coerceCanonicalPathForType(result.CanonicalPath, fragmentType, ""); changed {
		result.NormalizationSteps = append(result.NormalizationSteps,
			fmt.Sprintf("type_prefix_coercion: %s -> %s", result.CanonicalPath, fixed))
		result.CanonicalPath = fixed
	}

	// Step 2: Check for existing fragment by canonical path
	if r.fragmentsRepo != nil {
		existing, err := r.fragmentsRepo.GetByCanonicalPath(ctx, result.CanonicalPath)
		if err == nil && existing != nil && existing.ID != "" {
			atomic.AddInt64(&r.foundExistingByPath, 1)
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_fragment: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
			return result, nil
		}
		// Product cross-shape dedup fallback:
		// if exact path misses, probe stable product path variants sharing the same identity slug.
		if fragmentType == models.FragmentTypeProduct {
			for _, variantPath := range productCanonicalPathVariants(ctx, result.CanonicalPath) {
				variantPath = strings.TrimSpace(variantPath)
				if variantPath == "" || variantPath == strings.TrimSpace(result.CanonicalPath) {
					continue
				}
				candidate, verr := r.fragmentsRepo.GetByCanonicalPath(ctx, variantPath)
				if verr != nil || candidate == nil || strings.TrimSpace(candidate.ID) == "" {
					continue
				}
				if candidate.Type != "" && candidate.Type != models.FragmentTypeProduct {
					continue
				}
				atomic.AddInt64(&r.foundExistingByPath, 1)
				result.ExistingFragment = candidate
				if cp := strings.TrimSpace(candidate.CanonicalPath); cp != "" && cp != result.CanonicalPath {
					result.NormalizationSteps = append(result.NormalizationSteps,
						fmt.Sprintf("product_variant_match: %s -> %s (id=%s)", result.CanonicalPath, cp, candidate.ID))
					result.CanonicalPath = cp
				}
				log.Printf("%s Product variant match: %s -> %s (id=%s)", logPrefix, inputPath, result.CanonicalPath, candidate.ID)
				return result, nil
			}
		}
	}

	// Step 2.5: Abbreviation resolution — redirect abbrev paths to full-name fragments in same geo.
	// Prevents creating duplicate fragments like /gov/ae/adafsa when /gov/ae/abu-dhabi-agriculture-and-food-safety-authority exists.
	if r.fragmentsRepo != nil && (strings.HasPrefix(result.CanonicalPath, "/gov/") || strings.HasPrefix(result.CanonicalPath, "/org/")) {
		abbrevPath := result.CanonicalPath
		if resolvedPath, existingFrag, ok := merge.ResolveAbbreviationToFull(ctx, abbrevPath, r.fragmentsRepo); ok && existingFrag != nil && existingFrag.ID != "" {
			atomic.AddInt64(&r.foundExistingByPath, 1)
			result.CanonicalPath = resolvedPath
			result.ExistingFragment = existingFrag
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("abbreviation_resolution: %s -> %s (id=%s)", abbrevPath, resolvedPath, existingFrag.ID))
			log.Printf("%s Abbreviation resolution: %s -> %s (id=%s)", logPrefix, abbrevPath, resolvedPath, existingFrag.ID)
			return result, nil
		}
	}

	// Step 3: For organizations, try deduplication by name/Wikidata ID
	if fragmentType == models.FragmentTypeOrganization && r.orgDeduplicator != nil {
		// Create a minimal fragment for deduplication check
		testFragment := &models.HorizonFragment{
			CanonicalPath: result.CanonicalPath,
			Type:          fragmentType,
		}

		// Extract organization name from canonical path for deduplication
		// Supported formats:
		// - legacy:   /org/{slug}
		// - preferred:/org/{iso2|na}/{slug}
		if strings.HasPrefix(result.CanonicalPath, "/org/") {
			parts := strings.Split(strings.Trim(result.CanonicalPath, "/"), "/")
			if len(parts) >= 2 {
				slug := ""
				if len(parts) >= 3 {
					slug = parts[2]
				} else {
					slug = parts[1]
				}
				// Try to infer shortName from slug (reverse slugify-ish) to improve dedup hits.
				// Important: OrgDeduplicator matches shortName exactly (repo FindByIdentifier),
				// so we want a readable title-cased string, not the raw slug.
				shortName := strings.ReplaceAll(slug, "-", " ")
				shortName = strings.ReplaceAll(shortName, "_", " ")
				shortName = strings.TrimSpace(shortName)
				if shortName != "" && shortName == strings.ToLower(shortName) {
					shortName = strings.Title(shortName)
				}
				if testFragment.Identifiers == nil {
					testFragment.Identifiers = make(map[string]string)
				}
				if shortName != "" {
					testFragment.Identifiers["shortName"] = shortName
				} else {
					// Fallback to raw slug if we couldn't humanize it (should be rare).
					testFragment.Identifiers["shortName"] = slug
				}
			}
		}

		deduplicated, err := r.orgDeduplicator.DeduplicateOrg(ctx, testFragment)
		if err == nil && deduplicated != nil && deduplicated.ID != "" {
			if !isDedupReplacementCompatible(fragmentType, result.CanonicalPath, deduplicated) {
				log.Printf("%s Org deduplication candidate rejected (incompatible): requestedType=%s requestedPath=%s candidateType=%s candidatePath=%s candidateID=%s",
					logPrefix, fragmentType, result.CanonicalPath, deduplicated.Type, deduplicated.CanonicalPath, deduplicated.ID)
			} else if deduplicated.CanonicalPath != result.CanonicalPath {
				atomic.AddInt64(&r.orgDedupHits, 1)
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("org_deduplication: %s -> %s (id=%s)", result.CanonicalPath, deduplicated.CanonicalPath, deduplicated.ID))
				result.CanonicalPath = deduplicated.CanonicalPath
				result.ExistingFragment = deduplicated
				log.Printf("%s Org deduplication found existing: %s -> %s (id=%s)", logPrefix, inputPath, deduplicated.CanonicalPath, deduplicated.ID)
				return result, nil
			}
		}
	}

	// Step 4: Check for existing fragment by the final canonical path (after deduplication)
	if result.ExistingFragment == nil && r.fragmentsRepo != nil {
		existing, err := r.fragmentsRepo.GetByCanonicalPath(ctx, result.CanonicalPath)
		if err == nil && existing != nil && existing.ID != "" {
			atomic.AddInt64(&r.foundExistingAfterDedup, 1)
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_after_dedup: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment after dedup: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
		}
	}

	// Best-effort: record observed redirect so future runs skip redundant work.
	redirectWrites, redirectStable, redirectVariability := r.recordRedirect(ctx, inputPath, result.CanonicalPath, fragmentType, "canonical_path_resolver", 5)
	r.recordTelemetry(ctx, fragmentType, redirectHits, redirectWrites, redirectStable, redirectVariability)

	return result, nil
}

// Stats returns a best-effort snapshot of in-memory counters.
func (r *CanonicalPathResolver) Stats() CanonicalPathResolverStats {
	if r == nil {
		return CanonicalPathResolverStats{}
	}
	return CanonicalPathResolverStats{
		ResolveCalls:               atomic.LoadInt64(&r.resolveCalls),
		CanonizationPolicyRewrites: atomic.LoadInt64(&r.canonizationPolicyRewrites),
		GeoStrictified:             atomic.LoadInt64(&r.geoStrictified),
		GeoNamesRewrites:           atomic.LoadInt64(&r.geoNamesRewrites),
		ProductTaxonomyRewrites:    atomic.LoadInt64(&r.productTaxonomyRewrites),
		FoundExistingByPath:        atomic.LoadInt64(&r.foundExistingByPath),
		OrgDedupHits:               atomic.LoadInt64(&r.orgDedupHits),
		FoundExistingAfterDedup:    atomic.LoadInt64(&r.foundExistingAfterDedup),
		RedirectHits:               atomic.LoadInt64(&r.redirectHits),
		RedirectWrites:             atomic.LoadInt64(&r.redirectWrites),
	}
}

// normalizeGeoPathViaGeoNames normalizes /loc paths using GeoNames.
//
// Supported inputs:
// - /loc/{country}
// - /loc/{country}/{level1}
// - /loc/{country}/{level1}/{level2}
//
// Returns:
// - normalized path (or "" if no change)
// - a human-readable normalization step string
// - confidence penalty in [0..1] (0 means no penalty)
func normalizeGeoPathViaGeoNames(ctx context.Context, path string, client coreontology.GeoNamesClientInterface) (string, string, float64) {
	p := strings.TrimSpace(path)
	if client == nil || p == "" || !strings.HasPrefix(p, "/loc/") {
		return "", "", 0
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return "", "", 0
	}

	// Loose form: /loc/{slug}. Try resolving as an unambiguous CITY name.
	if len(parts) == 2 {
		slug := strings.TrimSpace(parts[1])
		if slug == "" {
			return "", "", 0
		}
		// Avoid interfering with country alias normalization logic (handled earlier).
		if taxonomy.IsISO2CountryCode(slug) {
			return "", "", 0
		}
		// If it looks like a known country name, also skip (handled earlier).
		countrySlug := strings.ToLower(strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(slug)))
		if IsKnownCountryName(countrySlug) {
			return "", "", 0
		}

		cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(slug))
		best, ok := pickUnambiguousCityCandidate(ctx, client, cityName, "", "")
		if !ok || best == nil || strings.TrimSpace(best.CountryCode) == "" {
			return "", "", 0
		}
		cc := strings.ToLower(strings.TrimSpace(best.CountryCode))
		admin := strings.ToLower(strings.TrimSpace(best.AdminCode1))
		citySlug := slugify(best.Name)
		if citySlug == "" {
			citySlug = slugify(cityName)
		}
		norm := taxonomy.GeoCanonicalPath(cc, admin, citySlug)
		if norm != p {
			return norm, fmt.Sprintf("geonames_city_upgrade: %s -> %s", p, norm), 0.05
		}
		return "", "", 0
	}

	// /loc/{cc}/{level1} can be either admin or city. Try city upgrade first (non-destructive).
	if len(parts) == 3 && taxonomy.IsISO2CountryCode(parts[1]) {
		cc := strings.ToLower(strings.TrimSpace(parts[1]))
		token := strings.ToLower(strings.TrimSpace(parts[2]))
		if token == "" || token == "na" {
			return "", "", 0
		}
		cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(token))
		best, ok := pickUnambiguousCityCandidate(ctx, client, cityName, cc, "")
		if !ok || best == nil || strings.TrimSpace(best.CountryCode) == "" {
			return "", "", 0
		}
		bestCC := strings.ToLower(strings.TrimSpace(best.CountryCode))
		bestAdmin := strings.ToLower(strings.TrimSpace(best.AdminCode1))
		bestCity := slugify(best.Name)
		if bestCity == "" {
			bestCity = token
		}
		norm := taxonomy.GeoCanonicalPath(bestCC, bestAdmin, bestCity)
		if norm != p {
			return norm, fmt.Sprintf("geonames_city_upgrade: %s -> %s", p, norm), 0.05
		}
		return "", "", 0
	}

	// Deep form: /loc/{cc}/{level1}/{level2}. Treat last segment as city candidate.
	if len(parts) != 4 {
		return "", "", 0
	}
	cc := strings.ToLower(strings.TrimSpace(parts[1]))
	level1 := strings.ToLower(strings.TrimSpace(parts[2]))
	city := strings.ToLower(strings.TrimSpace(parts[3]))
	if !taxonomy.IsISO2CountryCode(cc) {
		return "", "", 0
	}
	if city == "" || city == "na" {
		return "", "", 0
	}

	cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(city))
	best, ok := pickUnambiguousCityCandidate(ctx, client, cityName, cc, level1)
	if !ok || best == nil || strings.TrimSpace(best.CountryCode) == "" {
		return "", "", 0
	}

	bestCC := strings.ToLower(strings.TrimSpace(best.CountryCode))
	bestAdmin := strings.ToLower(strings.TrimSpace(best.AdminCode1))
	bestCity := slugify(best.Name)
	if bestCity == "" {
		bestCity = city
	}

	norm := taxonomy.GeoCanonicalPath(bestCC, bestAdmin, bestCity)
	if norm != p {
		return norm, fmt.Sprintf("geonames_geo_fix: %s -> %s", p, norm), 0.05
	}
	return "", "", 0
}

// NormalizeGeoPathViaGeoNames exposes GeoNames-based /loc normalization for tools.
func NormalizeGeoPathViaGeoNames(ctx context.Context, path string, client coreontology.GeoNamesClientInterface) (string, string, float64) {
	return normalizeGeoPathViaGeoNames(ctx, path, client)
}

func pickUnambiguousCityCandidate(ctx context.Context, client coreontology.GeoNamesClientInterface, cityName, cc, admin string) (*coreontology.GeoNamesPlace, bool) {
	cityName = strings.TrimSpace(cityName)
	if client == nil || cityName == "" {
		return nil, false
	}

	admin = strings.TrimSpace(admin)
	adminToken := normalizeGeoToken(admin)

	// 1) Try within provided country/admin first (deterministic fix of admin1 mismatch).
	if strings.TrimSpace(cc) != "" {
		opts := coreontology.GeoNamesSearchOptions{
			Country:      strings.ToUpper(strings.TrimSpace(cc)),
			FeatureClass: "P",
			NameEquals:   true,
		}
		// Only pass adminCode1 when it *looks like a code* (e.g., "CA" for US, "07" for DE).
		// Many of our existing /loc paths store ADM1 as a name slug (e.g., "north-rhine-westphalia"),
		// which would kill recall if we pass it as adminCode1.
		if adminToken != "" && strings.ToLower(admin) != "na" && looksLikeGeoNamesAdminCode(admin) {
			opts.AdminCode1 = strings.ToUpper(admin)
		}
		resp, err := client.Search(ctx, cityName, 10, opts)
		if err == nil && resp != nil && len(resp.Results) > 0 {
			results := resp.Results

			// If admin was provided but we didn't pass it as adminCode1, try to filter by admin name/code match.
			if adminToken != "" && strings.ToLower(admin) != "na" && strings.TrimSpace(opts.AdminCode1) == "" {
				filtered := make([]coreontology.GeoNamesPlace, 0, len(results))
				for _, r := range results {
					if adminToken == normalizeGeoToken(r.AdminCode1) || adminToken == normalizeGeoToken(r.AdminName1) {
						filtered = append(filtered, r)
					}
				}
				// If filter yields nothing, keep original list to allow "ADM1 correction" cases.
				if len(filtered) > 0 {
					results = filtered
				}
			}

			// If adminCode1 is explicitly provided, this should already be deterministic.
			if strings.TrimSpace(opts.AdminCode1) != "" {
				// Prefer exact slug match.
				want := slugify(cityName)
				for _, r := range results {
					if slugify(r.Name) == want && strings.EqualFold(r.CountryCode, cc) {
						return &r, true
					}
				}
				return &results[0], true
			}

			// Country-only scope may still be ambiguous (e.g., Springfield in US).
			if len(results) == 1 {
				return &results[0], true
			}

			// If all candidates point to the same adminCode1, accept.
			adminSet := map[string]struct{}{}
			for _, r := range results {
				a := strings.ToUpper(strings.TrimSpace(r.AdminCode1))
				if a == "" {
					a = "NA"
				}
				adminSet[a] = struct{}{}
				if len(adminSet) > 1 {
					break
				}
			}
			if len(adminSet) == 1 {
				return &results[0], true
			}

			// Ambiguous within country: only accept if one candidate dominates by population.
			best, ok := pickDominantByPopulation(results, cityName)
			if ok {
				return best, true
			}
			return nil, false
		}
	}

	// 2) Fallback: global city search (may fix wrong country, but only if unambiguous).
	resp, err := client.Search(ctx, cityName, 10, coreontology.GeoNamesSearchOptions{
		FeatureClass: "P",
		NameEquals:   true,
	})
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return nil, false
	}
	if len(resp.Results) == 1 {
		return &resp.Results[0], true
	}

	// Heuristic: highest-population exact-name match (see docs/horizon/scout/HEURISTICS_REGISTRY.md)
	if best, ok := pickDominantByPopulation(resp.Results, cityName); ok {
		return best, true
	}
	return nil, false
}

func pickDominantByPopulation(results []coreontology.GeoNamesPlace, cityName string) (*coreontology.GeoNamesPlace, bool) {
	if len(results) == 0 {
		return nil, false
	}
	want := slugify(cityName)
	cands := make([]coreontology.GeoNamesPlace, 0, len(results))
	for _, r := range results {
		if slugify(r.Name) != want {
			continue
		}
		// Keep only populated places; tiny villages are too ambiguous.
		if r.Population <= 0 {
			continue
		}
		cands = append(cands, r)
	}
	if len(cands) == 0 {
		return nil, false
	}
	// Sort by population descending (small N, do simple scan).
	best := cands[0]
	secondPop := int64(0)
	for _, r := range cands {
		if r.Population > best.Population {
			secondPop = best.Population
			best = r
			continue
		}
		if r.Population > secondPop && r.Population <= best.Population {
			secondPop = r.Population
		}
	}
	// Require a meaningful population and dominance.
	if best.Population < 100000 {
		return nil, false
	}
	if secondPop == 0 {
		return &best, true
	}
	if float64(best.Population) >= math.Max(5.0*float64(secondPop), float64(secondPop)+500000) {
		return &best, true
	}
	return nil, false
}

// normalizeGeoPathToStrict converts non-strict /loc shapes into canonical variable-depth /loc paths.
//
// Supported:
// - /loc/{country}                   -> /loc/{cc} (when safe)
// - /loc/{cc}                        -> /loc/{cc}
// - /loc/{country}/{level1}          -> /loc/{cc}/{level1} (when safe)
// - /loc/{country}/{adm1}/{city}     -> /loc/{cc}/{adm1}/{city} (when safe)
// Returns "" if no safe normalization is possible.
func normalizeGeoPathToStrict(path string) string {
	// Shared baseline normalization used across repo/scout.
	// Keep scout-specific fallback below for country-name inputs (e.g., /loc/morocco).
	if norm := canonical.NormalizeGeoPathToStrict(path); norm != "" {
		return norm
	}

	p := strings.TrimSpace(path)
	if !strings.HasPrefix(p, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}

	// Normalize country segment (parts[1]) to ISO alpha2 when possible.
	countryRaw := strings.TrimSpace(parts[1])
	cc := ""
	if taxonomy.IsISO2CountryCode(countryRaw) {
		cc = strings.ToLower(countryRaw)
	} else if taxonomy.IsRegionalGeoToken(countryRaw) {
		cc = strings.ToLower(countryRaw)
	} else {
		// Convert slug-ish to name-ish for lookup.
		countrySlug := strings.ReplaceAll(countryRaw, "-", " ")
		countrySlug = strings.ReplaceAll(countrySlug, "_", " ")
		countrySlug = strings.ToLower(strings.TrimSpace(countrySlug))
		if countrySlug == "" {
			return ""
		}
		// Safety: avoid known ambiguous country names when we have no extra context.
		if isAmbiguousCountrySlug(countrySlug) {
			return ""
		}
		if !IsKnownCountryName(countrySlug) {
			return ""
		}
		cp := GetCountryCanonicalPath(countrySlug)
		// Expected: /loc/{cc}
		if strings.HasPrefix(cp, "/loc/") {
			ccParts := strings.Split(strings.Trim(cp, "/"), "/")
			if len(ccParts) >= 2 && ccParts[0] == "loc" && len(ccParts[1]) == 2 {
				cc = strings.ToLower(ccParts[1])
			}
		}
		if cc == "" {
			return ""
		}
	}

	// Normalize shape.
	switch len(parts) {
	case 2:
		return taxonomy.GeoCanonicalPath(cc, "", "")
	case 3:
		level1 := strings.ToLower(strings.TrimSpace(parts[2]))
		return taxonomy.GeoCanonicalPath(cc, level1, "")
	case 4:
		level1 := strings.ToLower(strings.TrimSpace(parts[2]))
		level2 := strings.ToLower(strings.TrimSpace(parts[3]))
		return taxonomy.GeoCanonicalPath(cc, level1, level2)
	default:
		// Unknown legacy shapes (e.g., /loc/island/fj/viti-levu) - do not touch.
		return ""
	}
}

func isAmbiguousCountrySlug(countrySlug string) bool {
	switch strings.ToLower(strings.TrimSpace(countrySlug)) {
	case "georgia":
		return true
	default:
		return false
	}
}

func looksLikeGeoNamesAdminCode(admin string) bool {
	admin = strings.ToUpper(strings.TrimSpace(admin))
	if admin == "" {
		return false
	}
	// US-like: 2 letters.
	if len(admin) == 2 && admin[0] >= 'A' && admin[0] <= 'Z' && admin[1] >= 'A' && admin[1] <= 'Z' {
		return true
	}
	// Many countries: numeric codes ("01", "07", "10", ...). Allow 1-3 digits.
	if len(admin) >= 1 && len(admin) <= 3 {
		for i := 0; i < len(admin); i++ {
			if admin[i] < '0' || admin[i] > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// normalizeGeoToken helps compare admin names/codes robustly (e.g., "abu-dhabi" vs "abudhabi").
func normalizeGeoToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Replace separators then strip all non-alnum.
	s = strings.NewReplacer("-", "", "_", "", " ", "").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeFlatProductPath upgrades a flat product canonical path (/product/{slug}) to a hierarchical taxonomy path
// when classification is possible. Returns "" if no safe normalization is possible.
func normalizeFlatProductPath(ctx context.Context, path string) string {
	p := strings.TrimSpace(path)
	if !strings.HasPrefix(p, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// Only normalize flat structure: product/{slug}
	if len(parts) != 2 || parts[0] != "product" {
		return ""
	}
	slug := strings.TrimSpace(parts[1])
	if slug == "" {
		return ""
	}

	// Use a human-ish label as input to canonicalizer (helps pattern matching).
	// Note: canonicalizer will slugify + apply code-defined taxonomy synonyms.
	name := strings.ReplaceAll(slug, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		name = slug
	}

	c := NewProductCanonicalizer()
	out, err := c.CanonicalizeProduct(ctx, name)
	if err != nil || out == nil || strings.TrimSpace(out.CanonicalPath) == "" {
		return ""
	}
	// If canonicalizer couldn't classify, it will typically return the same flat path.
	return strings.TrimSpace(out.CanonicalPath)
}

// ResolveBatch resolves multiple canonical paths in batch.
// Returns a map from input path to ResolveResult.
func (r *CanonicalPathResolver) ResolveBatch(
	ctx context.Context,
	inputPaths []string,
	fragmentType models.FragmentType,
) (map[string]*ResolveResult, error) {
	results := make(map[string]*ResolveResult, len(inputPaths))

	for _, path := range inputPaths {
		resolved, err := r.Resolve(ctx, path, fragmentType)
		if err != nil {
			log.Printf("[CanonicalPathResolver] Failed to resolve path %q: %v", path, err)
			// Continue with other paths, but mark this one as failed
			results[path] = &ResolveResult{
				CanonicalPath: path,
				Confidence:    0.0,
			}
			continue
		}
		results[path] = resolved
	}

	return results, nil
}

// ResolveWithMetadata resolves a canonical path with additional metadata for better deduplication.
// This is useful when we have more information about the entity (e.g., from LLM extraction).
func (r *CanonicalPathResolver) ResolveWithMetadata(
	ctx context.Context,
	inputPath string,
	fragmentType models.FragmentType,
	metadata map[string]string, // e.g., {"shortName": "Tesla", "legalName": "Tesla Inc.", "wikidataId": "Q478214"}
) (*ResolveResult, error) {
	logPrefix := "[CanonicalPathResolver]"

	if r == nil {
		return &ResolveResult{
			CanonicalPath: strings.TrimSpace(inputPath),
			Confidence:    0.5,
		}, nil
	}

	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	atomic.AddInt64(&r.resolveCalls, 1)

	result := &ResolveResult{
		CanonicalPath:      inputPath,
		NormalizationSteps: []string{},
		Confidence:         1.0,
	}

	// Step 0: Use known redirect mappings from previous pipeline runs (fast path).
	redirectHits := r.applyRedirects(ctx, result)

	// Step 1: Normalize path using CanonizationPolicy (same as Resolve)
	if r.canonizationPolicy != nil {
		basePath := strings.TrimSpace(result.CanonicalPath)
		decision, err := r.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "canonical_path",
				"inputValue": basePath,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			normalized, _ := decision.Params["output"].(string)
			if normalized != "" && normalized != basePath {
				atomic.AddInt64(&r.canonizationPolicyRewrites, 1)
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("canonization_policy: %s -> %s", basePath, normalized))
				result.CanonicalPath = normalized
				log.Printf("%s CanonizationPolicy normalized: %s -> %s", logPrefix, basePath, normalized)
			}
		}
	}

	// Step 1.2: Government body correction (org path with gov-like label).
	if fragmentType == models.FragmentTypeOrganization {
		label := strings.TrimSpace(metadata["legalName"])
		if label == "" {
			label = strings.TrimSpace(metadata["shortName"])
		}
		if label != "" && strings.HasPrefix(result.CanonicalPath, "/org/") && looksLikeGovLabel(label) {
			iso2 := inferISO2FromLabel(label, nil)
			if iso2 == "" {
				iso2 = "na"
			}
			if govPath := taxonomy.GovCanonicalPath(iso2, label); govPath != "" && govPath != result.CanonicalPath {
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("gov_path_upgrade: %s -> %s", result.CanonicalPath, govPath))
				result.CanonicalPath = govPath
			}
		}
	}

	// Step 1.25: Best-effort PRODUCT normalization to a stable shape.
	if fragmentType == models.FragmentTypeProduct {
		nameHints := make([]string, 0, 3)
		if s := strings.TrimSpace(metadata["shortName"]); s != "" {
			nameHints = append(nameHints, s)
		}
		if s := strings.TrimSpace(metadata["legalName"]); s != "" {
			nameHints = append(nameHints, s)
		}
		if s := strings.TrimSpace(metadata["labelEN"]); s != "" {
			nameHints = append(nameHints, s)
		}
		for _, variant := range productCanonicalPathVariants(ctx, result.CanonicalPath, nameHints...) {
			variant = strings.TrimSpace(variant)
			if variant == "" || variant == strings.TrimSpace(result.CanonicalPath) {
				continue
			}
			atomic.AddInt64(&r.productTaxonomyRewrites, 1)
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("product_taxonomy: %s -> %s", result.CanonicalPath, variant))
			result.CanonicalPath = variant
			break
		}
	}

	// Step 1.3: Market deduplication using tags (metadata-driven).
	if fragmentType == models.FragmentTypeMarket && r.fragmentsRepo != nil && len(metadata) > 0 {
		rawTags := parseTagMetadata(metadata["tags"])
		normalizedTags := taxonomy.NormalizeMarketTags(rawTags)
		if len(normalizedTags) >= 5 {
			geo, sector, vertical := parseMarketGeoSector(result.CanonicalPath)
			if geo != "" && sector != "" && vertical != "" {
				searchTokens := taxonomy.MarketTagSearchTokens(normalizedTags, 10)
				if len(searchTokens) > 0 {
					candidates, err := r.fragmentsRepo.FindByTags(ctx, searchTokens, 50)
					if err == nil && len(candidates) > 0 {
						tagSet := map[string]struct{}{}
						for _, t := range normalizedTags {
							tagSet[t] = struct{}{}
						}
						var best *models.HorizonFragment
						bestOverlap := 0
						for _, cand := range candidates {
							if cand == nil || cand.ID == "" {
								continue
							}
							if cand.Type != models.FragmentTypeMarket && !strings.HasPrefix(strings.TrimSpace(cand.CanonicalPath), "/market/") {
								continue
							}
							cGeo, cSector, cVertical := parseMarketGeoSector(cand.CanonicalPath)
							if cGeo == "" || cSector == "" || cVertical == "" {
								continue
							}
							// Strict scope match: same geo + domain + vertical.
							if cGeo != geo || cSector != sector || cVertical != vertical {
								continue
							}
							candTags := taxonomy.NormalizeMarketTags(cand.Tags)
							if len(candTags) == 0 {
								continue
							}
							overlap := 0
							for _, t := range candTags {
								if _, ok := tagSet[t]; ok {
									overlap++
								}
							}
							if overlap >= 5 && overlap > bestOverlap {
								best = cand
								bestOverlap = overlap
							}
						}
						if best != nil && strings.TrimSpace(best.CanonicalPath) != "" {
							result.NormalizationSteps = append(result.NormalizationSteps,
								fmt.Sprintf("market_tag_dedup: %s -> %s (overlap=%d)", result.CanonicalPath, best.CanonicalPath, bestOverlap))
							result.CanonicalPath = strings.TrimSpace(best.CanonicalPath)
							result.ExistingFragment = best
							log.Printf("%s Market deduplication found existing: %s -> %s (id=%s, overlap=%d)", logPrefix, inputPath, best.CanonicalPath, best.ID, bestOverlap)
							return result, nil
						}
					}
				}
			}
		}
	}

	// Step 2: For organizations with metadata, try deduplication first (before checking by path)
	if fragmentType == models.FragmentTypeOrganization && r.orgDeduplicator != nil && len(metadata) > 0 {
		testFragment := &models.HorizonFragment{
			CanonicalPath: result.CanonicalPath,
			Type:          fragmentType,
			Identifiers:   make(map[string]string),
		}

		// Populate identifiers from metadata
		if shortName, ok := metadata["shortName"]; ok {
			testFragment.Identifiers["shortName"] = strings.TrimSpace(shortName)
		}
		if legalName, ok := metadata["legalName"]; ok {
			testFragment.Identifiers["legalName"] = strings.TrimSpace(legalName)
		}
		if wikidataId, ok := metadata["wikidataId"]; ok {
			testFragment.Identifiers["wikidataId"] = strings.TrimSpace(wikidataId)
		}
		for _, key := range orgIdentityAnchorPriority {
			if key == "wikidataId" {
				continue
			}
			if v, ok := metadata[key]; ok {
				if vv := strings.TrimSpace(v); vv != "" {
					testFragment.Identifiers[key] = vv
				}
			}
		}
		if labelEN, ok := metadata["labelEN"]; ok {
			if testFragment.Labels == nil {
				testFragment.Labels = make(map[string]string)
			}
			testFragment.Labels["en"] = strings.TrimSpace(labelEN)
		}

		deduplicated, err := r.orgDeduplicator.DeduplicateOrg(ctx, testFragment)
		if err == nil && deduplicated != nil && deduplicated.ID != "" {
			if !isDedupReplacementCompatible(fragmentType, result.CanonicalPath, deduplicated) {
				log.Printf("%s Org deduplication (with metadata) candidate rejected (incompatible): requestedType=%s requestedPath=%s candidateType=%s candidatePath=%s candidateID=%s",
					logPrefix, fragmentType, result.CanonicalPath, deduplicated.Type, deduplicated.CanonicalPath, deduplicated.ID)
			} else if deduplicated.CanonicalPath != result.CanonicalPath {
				atomic.AddInt64(&r.orgDedupHits, 1)
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("org_deduplication_with_metadata: %s -> %s (id=%s)", result.CanonicalPath, deduplicated.CanonicalPath, deduplicated.ID))
				result.CanonicalPath = deduplicated.CanonicalPath
				result.ExistingFragment = deduplicated
				log.Printf("%s Org deduplication (with metadata) found existing: %s -> %s (id=%s)", logPrefix, inputPath, deduplicated.CanonicalPath, deduplicated.ID)
				return result, nil
			}
		}
	}

	// Step 3: Check for existing fragment by canonical path
	if r.fragmentsRepo != nil {
		existing, err := r.fragmentsRepo.GetByCanonicalPath(ctx, result.CanonicalPath)
		if err == nil && existing != nil && existing.ID != "" {
			atomic.AddInt64(&r.foundExistingByPath, 1)
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_fragment: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
		}
		if result.ExistingFragment == nil && fragmentType == models.FragmentTypeProduct {
			nameHints := []string{
				strings.TrimSpace(metadata["shortName"]),
				strings.TrimSpace(metadata["legalName"]),
				strings.TrimSpace(metadata["labelEN"]),
			}
			for _, variantPath := range productCanonicalPathVariants(ctx, result.CanonicalPath, nameHints...) {
				variantPath = strings.TrimSpace(variantPath)
				if variantPath == "" || variantPath == strings.TrimSpace(result.CanonicalPath) {
					continue
				}
				candidate, verr := r.fragmentsRepo.GetByCanonicalPath(ctx, variantPath)
				if verr != nil || candidate == nil || strings.TrimSpace(candidate.ID) == "" {
					continue
				}
				if candidate.Type != "" && candidate.Type != models.FragmentTypeProduct {
					continue
				}
				atomic.AddInt64(&r.foundExistingByPath, 1)
				result.ExistingFragment = candidate
				if cp := strings.TrimSpace(candidate.CanonicalPath); cp != "" && cp != result.CanonicalPath {
					result.NormalizationSteps = append(result.NormalizationSteps,
						fmt.Sprintf("product_variant_match: %s -> %s (id=%s)", result.CanonicalPath, cp, candidate.ID))
					result.CanonicalPath = cp
				}
				log.Printf("%s Product variant match (metadata): %s -> %s (id=%s)", logPrefix, inputPath, result.CanonicalPath, candidate.ID)
				break
			}
		}
	}

	// Best-effort: record observed redirect so future runs skip redundant work.
	redirectWrites, redirectStable, redirectVariability := r.recordRedirect(ctx, inputPath, result.CanonicalPath, fragmentType, "canonical_path_resolver", 5)
	r.recordTelemetry(ctx, fragmentType, redirectHits, redirectWrites, redirectStable, redirectVariability)

	return result, nil
}

func parseTagMetadata(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	split := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '|', '\n', '\t':
			return true
		}
		return false
	})
	if len(split) == 1 {
		if strings.Contains(split[0], " ") {
			split = strings.Fields(split[0])
		}
	}
	out := make([]string, 0, len(split))
	for _, s := range split {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseMarketGeoSector(canonicalPath string) (string, string, string) {
	return parseMarketDedupScope(canonicalPath)
}

// RecordRedirect stores a canonical path rewrite for future fast-path resolution.
// Heuristic: warmup threshold 5 observations (see docs/horizon/scout/HEURISTICS_REGISTRY.md)
func (r *CanonicalPathResolver) RecordRedirect(ctx context.Context, fromPath string, toPath string, fragmentType models.FragmentType, reason string) (bool, bool, bool) {
	return r.recordRedirect(ctx, fromPath, toPath, fragmentType, reason, 5)
}

// RecordDeterministicRedirect stores a canonical path redirect that becomes immediately stable.
// Use for deterministic pipeline decisions (dedup, org upgrade, scope rewrite) where the redirect
// is authoritative and should be applied on the next Resolve() call without warmup.
func (r *CanonicalPathResolver) RecordDeterministicRedirect(ctx context.Context, fromPath string, toPath string, fragmentType models.FragmentType, reason string) (bool, bool, bool) {
	return r.recordRedirect(ctx, fromPath, toPath, fragmentType, reason, 1)
}

// extractMarketDomain extracts the domain segment from a MARKET canonical path.
// Format: /market/{geo}/{domain}[/{vertical}...]. Returns empty string if not a valid market path.
func extractMarketDomain(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/market/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// parts[0]=market, parts[1]=geo, parts[2]=domain
	if len(parts) < 3 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[2]))
}

// ValidateRedirectCompatibility checks if a redirect from sourcePath to targetPath is semantically
// compatible for the given fragment type. For MARKET paths, cross-domain redirects (e.g. finance→technology)
// are rejected to prevent stateful feedback loops that produce ontologically invalid outputs.
func ValidateRedirectCompatibility(sourcePath, targetPath string, fragmentType models.FragmentType) bool {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	if sourcePath == "" || targetPath == "" {
		return false
	}
	if fragmentType != models.FragmentTypeMarket {
		return true
	}
	srcDomain := extractMarketDomain(sourcePath)
	tgtDomain := extractMarketDomain(targetPath)
	if srcDomain == "" || tgtDomain == "" {
		return true
	}
	return srcDomain == tgtDomain
}

func (r *CanonicalPathResolver) recordRedirect(ctx context.Context, fromPath string, toPath string, fragmentType models.FragmentType, reason string, warmupThreshold int) (bool, bool, bool) {
	if r == nil || r.redirectsRepo == nil {
		return false, false, false
	}
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if fromPath == "" || toPath == "" || fromPath == toPath {
		return false, false, false
	}
	if !ValidateRedirectCompatibility(fromPath, toPath, fragmentType) {
		log.Printf("[CanonicalPathResolver] redirect rejected: cross-domain incompatible %s -> %s (fragmentType=%s)",
			fromPath, toPath, fragmentType)
		return false, false, false
	}
	redirect, becameStable, variability, err := r.redirectsRepo.RecordObservation(
		ctx,
		"global",
		fromPath,
		toPath,
		reason,
		string(fragmentType),
		warmupThreshold,
		7*24*time.Hour,
	)
	if err == nil && redirect != nil {
		atomic.AddInt64(&r.redirectWrites, 1)
		return true, becameStable, variability
	}
	return false, false, false
}

func (r *CanonicalPathResolver) applyRedirects(ctx context.Context, result *ResolveResult) int64 {
	if r == nil || r.redirectsRepo == nil || result == nil {
		return 0
	}
	current := strings.TrimSpace(result.CanonicalPath)
	if current == "" {
		return 0
	}
	seen := map[string]struct{}{current: {}}
	steps := make([]string, 0, 2)
	for i := 0; i < 4; i++ {
		redirect, err := r.redirectsRepo.Get(ctx, "global", current)
		if err != nil || redirect == nil {
			break
		}
		if redirect.StableUntil.IsZero() || redirect.StableUntil.Before(time.Now().UTC()) {
			break
		}
		target := strings.TrimSpace(redirect.StableTarget)
		if target == "" || target == current {
			break
		}
		if _, ok := seen[target]; ok {
			break
		}
		ft := models.FragmentType(strings.TrimSpace(redirect.FragmentType))
		if !ValidateRedirectCompatibility(current, target, ft) {
			log.Printf("[CanonicalPathResolver] redirect skipped: cross-domain incompatible %s -> %s (fragmentType=%s)",
				current, target, redirect.FragmentType)
			break
		}
		steps = append(steps, fmt.Sprintf("redirect_cache: %s -> %s", current, target))
		current = target
		seen[current] = struct{}{}
	}
	if current != strings.TrimSpace(result.CanonicalPath) {
		atomic.AddInt64(&r.redirectHits, int64(len(steps)))
		result.CanonicalPath = current
		result.NormalizationSteps = append(result.NormalizationSteps, steps...)
	}
	return int64(len(steps))
}

func (r *CanonicalPathResolver) recordTelemetry(ctx context.Context, fragmentType models.FragmentType, redirectHits int64, redirectWrites bool, redirectStable bool, redirectVariability bool) {
	if r == nil || r.telemetryRepo == nil {
		return
	}
	dimensions := map[string]string{}
	if fragmentType != "" {
		dimensions["fragmentType"] = string(fragmentType)
	}
	counters := map[string]int64{
		"resolve_calls": 1,
	}
	if redirectHits > 0 {
		counters["redirect_hits"] = redirectHits
	}
	if redirectWrites {
		counters["redirect_writes"] = 1
	}
	if redirectStable {
		counters["redirect_stable"] = 1
	}
	if redirectVariability {
		counters["redirect_variability"] = 1
	}
	_ = r.telemetryRepo.Increment(ctx, "global", "canonical_path_resolver", "redirect_cache", time.Hour, dimensions, counters)
}
