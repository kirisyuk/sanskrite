package scout

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// CanonicalPathResolver provides unified canonicalization and deduplication for canonical paths.
// This service ensures that all paths are normalized and deduplicated early in the pipeline,
// preventing duplicate fragments and inefficient processing.
type CanonicalPathResolver struct {
	fragmentsRepo      *repo.HorizonFragmentsRepository
	canonizationPolicy *policy.PolicyExecutor
	orgDeduplicator    *OrgDeduplicator
	geonamesClient     coreontology.GeoNamesClientInterface
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

	result := &ResolveResult{
		CanonicalPath:      inputPath,
		NormalizationSteps: []string{},
		Confidence:         1.0,
	}

	// Step 1: Normalize path using CanonizationPolicy
	if r.canonizationPolicy != nil {
		decision, err := r.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "canonical_path",
				"inputValue": inputPath,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			normalized, _ := decision.Params["output"].(string)
			if normalized != "" && normalized != inputPath {
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("canonization_policy: %s -> %s", inputPath, normalized))
				result.CanonicalPath = normalized
				log.Printf("%s CanonizationPolicy normalized: %s -> %s", logPrefix, inputPath, normalized)
			}
		} else if err != nil {
			log.Printf("%s WARNING: CanonizationPolicy failed: %v, using original path", logPrefix, err)
			result.Confidence *= 0.9 // Slight penalty for policy failure
		}
	}

	// Step 1.5: Strict GEO normalization for country-only paths.
	// Lens/intent often provides /loc/{slug} (e.g., /loc/uae, /loc/saudi-arabia), but Horizon canonical geo is
	// /loc/{countryAlpha2}/na/na for country-only locations.
	if fragmentType == models.FragmentTypeGeoRegion {
		if norm := normalizeGeoPathToStrict(result.CanonicalPath); norm != "" && norm != result.CanonicalPath {
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("geo_strictify: %s -> %s", result.CanonicalPath, norm))
			result.CanonicalPath = norm
		}
	}

	// Step 1.55: GeoNames-backed GEO normalization/validation for /loc paths.
	// Keep our canonical schema (/loc/{cc}/{admin1}/{city}) but verify/correct admin1/country using GeoNames.
	if fragmentType == models.FragmentTypeGeoRegion && r.geonamesClient != nil {
		if norm, step, confPenalty := normalizeGeoPathViaGeoNames(ctx, result.CanonicalPath, r.geonamesClient); norm != "" && norm != result.CanonicalPath {
			result.NormalizationSteps = append(result.NormalizationSteps, step)
			result.CanonicalPath = norm
			if confPenalty > 0 {
				result.Confidence *= (1.0 - confPenalty)
			}
		}
	}

	// Step 1.6: Best-effort PRODUCT normalization for flat /product/{slug} paths into hierarchical taxonomy.
	// We only attempt to "upgrade" flat product paths; if we cannot confidently classify, we keep the original path.
	if fragmentType == models.FragmentTypeProduct {
		if norm := normalizeFlatProductPath(ctx, result.CanonicalPath); norm != "" && norm != result.CanonicalPath {
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("product_taxonomy: %s -> %s", result.CanonicalPath, norm))
			result.CanonicalPath = norm
		}
	}

	// Step 2: Check for existing fragment by canonical path
	if r.fragmentsRepo != nil {
		existing, err := r.fragmentsRepo.GetByCanonicalPath(ctx, result.CanonicalPath)
		if err == nil && existing != nil && existing.ID != "" {
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_fragment: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
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
			if deduplicated.CanonicalPath != result.CanonicalPath {
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
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_after_dedup: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment after dedup: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
		}
	}

	return result, nil
}

// normalizeGeoPathViaGeoNames normalizes /loc paths using GeoNames (cache-first via the injected client).
//
// Supported inputs:
// - strict: /loc/{cc}/{admin1}/{city} (may correct admin1/country if a city is unambiguous)
// - loose:  /loc/{slug} (may upgrade to strict if a city is unambiguous; country-only aliases are handled elsewhere)
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
		if len(slug) == 2 && reCountryAlpha2.MatchString(strings.ToLower(slug)) {
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
		if admin == "" {
			admin = "na"
		}
		citySlug := slugify(best.Name)
		if citySlug == "" {
			citySlug = slugify(cityName)
		}
		norm := fmt.Sprintf("/loc/%s/%s/%s", cc, admin, citySlug)
		if norm != p {
			return norm, fmt.Sprintf("geonames_city_upgrade: %s -> %s", p, norm), 0.05
		}
		return "", "", 0
	}

	// Strict form: /loc/{cc}/{admin1}/{city}
	if len(parts) != 4 {
		return "", "", 0
	}
	cc := strings.ToLower(strings.TrimSpace(parts[1]))
	admin := strings.ToLower(strings.TrimSpace(parts[2]))
	city := strings.ToLower(strings.TrimSpace(parts[3]))
	if cc == "" || len(cc) != 2 || !reCountryAlpha2.MatchString(cc) {
		return "", "", 0
	}
	if admin == "" {
		admin = "na"
	}
	if city == "" {
		city = "na"
	}
	// No-op: country-only.
	if admin == "na" && city == "na" {
		return "", "", 0
	}
	// We only attempt GeoNames validation when city is present (non-na).
	if city == "na" {
		return "", "", 0
	}

	cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(city))
	best, ok := pickUnambiguousCityCandidate(ctx, client, cityName, cc, admin)
	if !ok || best == nil || strings.TrimSpace(best.CountryCode) == "" {
		return "", "", 0
	}

	bestCC := strings.ToLower(strings.TrimSpace(best.CountryCode))
	bestAdmin := strings.ToLower(strings.TrimSpace(best.AdminCode1))
	if bestAdmin == "" {
		bestAdmin = "na"
	}
	bestCity := slugify(best.Name)
	if bestCity == "" {
		bestCity = city
	}

	norm := fmt.Sprintf("/loc/%s/%s/%s", bestCC, bestAdmin, bestCity)
	if norm != p {
		return norm, fmt.Sprintf("geonames_geo_fix: %s -> %s", p, norm), 0.05
	}
	return "", "", 0
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

	// Heuristic: select the highest-population exact-name match if it dominates runner-up.
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

// normalizeGeoPathToStrict converts non-strict /loc shapes into strict /loc/{cc}/{adm1}/{city},
// applying safe country alias normalization and padding missing positions with "na".
//
// Supported:
// - /loc/{country}                   -> /loc/{cc}/na/na (when safe)
// - /loc/{cc}                        -> /loc/{cc}/na/na
// - /loc/{country}/{adm1}            -> /loc/{cc}/{adm1}/na (when safe)
// - /loc/{country}/{adm1}/{city}     -> /loc/{cc}/{adm1}/{city} (when safe)
// Returns "" if no safe normalization is possible.
func normalizeGeoPathToStrict(path string) string {
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
	if len(countryRaw) == 2 && reCountryAlpha2.MatchString(strings.ToLower(countryRaw)) {
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
		// Expected: /loc/{cc}/na/na
		if strings.HasPrefix(cp, "/loc/") {
			ccParts := strings.Split(strings.Trim(cp, "/"), "/")
			if len(ccParts) == 4 && ccParts[0] == "loc" && len(ccParts[1]) == 2 {
				cc = strings.ToLower(ccParts[1])
			}
		}
		if cc == "" {
			return ""
		}
	}

	// Strictify shape.
	switch len(parts) {
	case 2:
		return fmt.Sprintf("/loc/%s/na/na", cc)
	case 3:
		adm1 := strings.ToLower(strings.TrimSpace(parts[2]))
		if adm1 == "" {
			adm1 = "na"
		}
		return fmt.Sprintf("/loc/%s/%s/na", cc, adm1)
	case 4:
		adm1 := strings.ToLower(strings.TrimSpace(parts[2]))
		city := strings.ToLower(strings.TrimSpace(parts[3]))
		if adm1 == "" {
			adm1 = "na"
		}
		if city == "" {
			city = "na"
		}
		return fmt.Sprintf("/loc/%s/%s/%s", cc, adm1, city)
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
	// Note: canonicalizer will slugify + apply runtime taxonomy synonyms anyway.
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

	result := &ResolveResult{
		CanonicalPath:      inputPath,
		NormalizationSteps: []string{},
		Confidence:         1.0,
	}

	// Step 1: Normalize path using CanonizationPolicy (same as Resolve)
	if r.canonizationPolicy != nil {
		decision, err := r.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "canonical_path",
				"inputValue": inputPath,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			normalized, _ := decision.Params["output"].(string)
			if normalized != "" && normalized != inputPath {
				result.NormalizationSteps = append(result.NormalizationSteps,
					fmt.Sprintf("canonization_policy: %s -> %s", inputPath, normalized))
				result.CanonicalPath = normalized
				log.Printf("%s CanonizationPolicy normalized: %s -> %s", logPrefix, inputPath, normalized)
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
		if labelEN, ok := metadata["labelEN"]; ok {
			if testFragment.Labels == nil {
				testFragment.Labels = make(map[string]string)
			}
			testFragment.Labels["en"] = strings.TrimSpace(labelEN)
		}

		deduplicated, err := r.orgDeduplicator.DeduplicateOrg(ctx, testFragment)
		if err == nil && deduplicated != nil && deduplicated.ID != "" {
			if deduplicated.CanonicalPath != result.CanonicalPath {
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
			result.ExistingFragment = existing
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("found_existing_fragment: %s (id=%s)", result.CanonicalPath, existing.ID))
			log.Printf("%s Found existing fragment: %s (id=%s)", logPrefix, result.CanonicalPath, existing.ID)
		}
	}

	return result, nil
}
