package scout

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// CanonicalPathResolver provides unified canonicalization and deduplication for canonical paths.
// This service ensures that all paths are normalized and deduplicated early in the pipeline,
// preventing duplicate fragments and inefficient processing.
type CanonicalPathResolver struct {
	fragmentsRepo      *repo.HorizonFragmentsRepository
	canonizationPolicy *policy.PolicyExecutor
	orgDeduplicator    *OrgDeduplicator
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
		if norm := normalizeGeoCountryAliasPath(result.CanonicalPath); norm != "" && norm != result.CanonicalPath {
			result.NormalizationSteps = append(result.NormalizationSteps,
				fmt.Sprintf("geo_country_alias: %s -> %s", result.CanonicalPath, norm))
			result.CanonicalPath = norm
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
		// Format: /org/{slug} -> extract slug as potential shortName
		if strings.HasPrefix(result.CanonicalPath, "/org/") {
			parts := strings.Split(strings.Trim(result.CanonicalPath, "/"), "/")
			if len(parts) >= 2 {
				slug := parts[1]
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

// normalizeGeoCountryAliasPath converts non-strict geo country paths into strict /loc/{cc}/na/na.
// Examples:
// - /loc/uae -> /loc/ae/na/na
// - /loc/saudi-arabia -> /loc/sa/na/na
// - /loc/ae -> /loc/ae/na/na
// Returns "" if no safe normalization is possible.
func normalizeGeoCountryAliasPath(path string) string {
	p := strings.TrimSpace(path)
	if !strings.HasPrefix(p, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// loc/{country}/{admin1}/{city} is already strict
	if len(parts) == 4 && parts[0] == "loc" {
		return p
	}
	// loc/{slug} country-only alias
	if len(parts) != 2 || parts[0] != "loc" {
		return ""
	}
	slug := strings.TrimSpace(parts[1])
	if slug == "" {
		return ""
	}

	// If it's already an ISO alpha2, normalize to /na/na.
	if len(slug) == 2 && reCountryAlpha2.MatchString(strings.ToLower(slug)) {
		return "/loc/" + strings.ToLower(slug) + "/na/na"
	}

	// Try mapping common country names/aliases.
	countrySlug := strings.ReplaceAll(slug, "-", " ")
	countrySlug = strings.ReplaceAll(countrySlug, "_", " ")
	countrySlug = strings.ToLower(strings.TrimSpace(countrySlug))
	if countrySlug == "" {
		return ""
	}
	// Safety: only normalize known country names (avoid Georgia, etc.).
	if !IsKnownCountryName(countrySlug) {
		return ""
	}
	if canon := GetCountryCanonicalPath(countrySlug); canon != "" {
		return canon
	}
	return ""
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
