package scout

import (
	"context"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"os"
	"strings"
	"unicode"
)

func isProductModelAliasDedupEnabled() bool {
	// Default OFF: better to keep duplicates than merge unrelated products on heuristic.
	// Enable explicitly when you want aggressive model/SKU alias dedup:
	//   HORIZON_ENABLE_PRODUCT_MODEL_ALIAS_DEDUP=true
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HORIZON_ENABLE_PRODUCT_MODEL_ALIAS_DEDUP")))
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

func isProductWikidataSearchDedupEnabled() bool {
	// Default OFF: avoid false merges due to fuzzy Wikidata search.
	// Enable explicitly if you want aggressive product dedup:
	//   HORIZON_ENABLE_PRODUCT_WIKIDATA_SEARCH_DEDUP=true
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HORIZON_ENABLE_PRODUCT_WIKIDATA_SEARCH_DEDUP")))
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

// =============================================================================
// PRODUCT DEDUPLICATION POLICY
// =============================================================================
// Products are frequently mentioned under different names, formulas, CAS numbers.
// This policy defines how to identify and merge duplicates using Wikidata.
//
// Deduplication Priority:
// Tier 1 (100% confidence):
//   1. Wikidata ID match
//   2. CAS Registry Number match (for chemical substances)
//   3. Chemical formula match (for raw materials with same category/type)
//
// Tier 2 (high confidence):
//   4. Exact name match + same category/type/stage
//   5. Normalized name match (synonyms from taxonomy)

// ProductDeduplicator handles product deduplication using Wikidata and identifiers.
type ProductDeduplicator struct {
	fragmentsRepo  *repo.HorizonFragmentsRepository
	wikidataClient coreontology.WikidataClientInterface
	extractor      *ProductWikidataExtractor
}

// NewProductDeduplicator creates a new product deduplicator.
func NewProductDeduplicator(fragmentsRepo *repo.HorizonFragmentsRepository, client coreontology.WikidataClientInterface) *ProductDeduplicator {
	return &ProductDeduplicator{
		fragmentsRepo:  fragmentsRepo,
		wikidataClient: client,
		extractor:      NewProductWikidataExtractor(client),
	}
}

// DeduplicateProduct attempts to find an existing product fragment that matches
// the given fragment. If found, returns the existing fragment; otherwise returns
// the original fragment unchanged.
//
// Matching priority (Tier 1 = 100% confidence, Tier 2 = high confidence):
// Tier 1:
//  1. Wikidata ID match (from KBRefs or Identifiers)
//  2. CAS Registry Number match (for chemical substances)
//  3. Chemical formula match (for raw materials with category=raw, type=chemical)
//
// Tier 2:
//  4. Exact name match + same category/type/stage
//  5. Normalized name match (synonyms from taxonomy)
func (d *ProductDeduplicator) DeduplicateProduct(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return fragment, nil
	}
	if fragment == nil || fragment.Class != ah.FragmentClassProduct {
		return fragment, nil
	}

	// =========================================================================
	// Tier 1: Highest confidence — auto-merge
	// =========================================================================

	// Strategy 1: Match by Wikidata ID
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID != "" {
		existing, err := d.findByWikidataID(ctx, wikidataID)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			return existing, nil
		}
	}

	// Strategy 2: Match by CAS Registry Number
	casNumber := strings.TrimSpace(fragment.Identifiers["casNumber"])
	if casNumber != "" {
		existing, err := d.findByCASNumber(ctx, casNumber)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			return existing, nil
		}
	}

	// Strategy 3: Match by Chemical Formula (for raw chemical materials)
	chemicalFormula := strings.TrimSpace(fragment.Identifiers["chemicalFormula"])
	if chemicalFormula != "" {
		// Only match by formula if it's a raw chemical material
		// Parse canonical path to check category/type
		category, productType, _, _ := parseProductCanonicalPath(fragment.CanonicalPath)
		if category == "raw" && productType == "chemical" {
			existing, err := d.findByChemicalFormula(ctx, chemicalFormula, category, productType)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				return existing, nil
			}
		}
	}

	// =========================================================================
	// Tier 2: High confidence — auto-merge with logging
	// =========================================================================

	// Strategy 4: Exact name match + same category/type/stage
	labelEN := strings.TrimSpace(fragment.Labels["en"])
	if labelEN != "" {
		category, productType, stage, _ := parseProductCanonicalPath(fragment.CanonicalPath)
		if category != "" && productType != "" && stage != "" {
			existing, err := d.findByExactNameAndTaxonomy(ctx, labelEN, category, productType, stage)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				return existing, nil
			}
		}
	}

	// Strategy 4b: Exact alias match for "model-like" products (contains digits).
	// This is a safe heuristic for defense/tech model designations and similar SKUs:
	// - "M1 Abrams" vs "M1 Abrams Main Battle Tank"
	// - "F-16 Fighting Falcon" vs "F16 Fighting Falcon"
	//
	// We intentionally do NOT apply this broadly to generic names without digits to avoid false merges.
	if isProductModelAliasDedupEnabled() {
		for _, name := range candidateProductAliasNames(fragment) {
			if !isSpecificModelAlias(name) {
				continue
			}
			for _, v := range modelAliasVariants(name) {
				// Try matching against existing label[en]
				if existing, err := d.findByLabelExact(ctx, v); err == nil && existing != nil && existing.ID != fragment.ID {
					return existing, nil
				}
				// Try matching against existing identifiers.shortName / legalName
				if existing, err := d.findByIdentifierExact(ctx, "shortName", v); err == nil && existing != nil && existing.ID != fragment.ID {
					return existing, nil
				}
				if existing, err := d.findByIdentifierExact(ctx, "legalName", v); err == nil && existing != nil && existing.ID != fragment.ID {
					return existing, nil
				}
			}
		}
	}

	// Strategy 4c: Cross-shape canonical-path variant lookup.
	// This bridges flat vs hierarchical/legacy product paths:
	// - /product/{slug}
	// - /product/{category}/{type}/{stage}/{slug}
	// - /product/{orgSlug}/{slug} (legacy)
	// - malformed legacy tails like /product/{x}/{y}/{slug}
	if existing, err := d.findByCanonicalPathVariants(ctx, fragment); err == nil && existing != nil && existing.ID != fragment.ID {
		return existing, nil
	}

	// Strategy 5: Try to resolve Wikidata ID via API if we have a client
	if isProductWikidataSearchDedupEnabled() && d.wikidataClient != nil && wikidataID == "" {
		resolvedID := d.resolveWikidataID(ctx, fragment)
		if resolvedID != "" {
			existing, err := d.findByWikidataID(ctx, resolvedID)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				return existing, nil
			}
			// Store resolved Wikidata ID in the fragment for future use
			if fragment.Identifiers == nil {
				fragment.Identifiers = make(map[string]string)
			}
			fragment.Identifiers["wikidataId"] = resolvedID
		}
	}

	return fragment, nil
}

// findByWikidataID searches for an existing fragment with the given Wikidata ID.
func (d *ProductDeduplicator) findByWikidataID(ctx context.Context, wikidataID string) (*models.HorizonFragment, error) {
	if wikidataID == "" {
		return nil, nil
	}

	// Search by identifier field (returns slice)
	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, "wikidataId", wikidataID)
	if err == nil && len(existing) > 0 {
		// Filter by product type
		for _, frag := range existing {
			if frag.Class == ah.FragmentClassProduct {
				return frag, nil
			}
		}
	}

	return nil, nil
}

// findByCASNumber searches for an existing product with the same CAS number.
func (d *ProductDeduplicator) findByCASNumber(ctx context.Context, casNumber string) (*models.HorizonFragment, error) {
	if casNumber == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, "casNumber", casNumber)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag.Class == ah.FragmentClassProduct {
				return frag, nil
			}
		}
	}

	return nil, nil
}

// findByChemicalFormula searches for products with the same chemical formula and category/type.
func (d *ProductDeduplicator) findByChemicalFormula(ctx context.Context, formula, category, productType string) (*models.HorizonFragment, error) {
	if formula == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, "chemicalFormula", formula)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag.Class != ah.FragmentClassProduct {
				continue
			}
			// Check category and type match
			fragCategory, fragType, _, _ := parseProductCanonicalPath(frag.CanonicalPath)
			if fragCategory == category && fragType == productType {
				return frag, nil
			}
		}
	}

	return nil, nil
}

// findByExactNameAndTaxonomy searches for products with exact name and matching taxonomy.
func (d *ProductDeduplicator) findByExactNameAndTaxonomy(ctx context.Context, name, category, productType, stage string) (*models.HorizonFragment, error) {
	if name == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", name, ah.FragmentClassProduct)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			fragCategory, fragType, fragStage, _ := parseProductCanonicalPath(frag.CanonicalPath)
			if fragCategory == category && fragType == productType && fragStage == stage {
				return frag, nil
			}
		}
	}

	return nil, nil
}

func (d *ProductDeduplicator) findByLabelExact(ctx context.Context, name string) (*models.HorizonFragment, error) {
	name = strings.TrimSpace(name)
	if name == "" || d == nil || d.fragmentsRepo == nil {
		return nil, nil
	}
	existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", name, ah.FragmentClassProduct)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag != nil && frag.Class == ah.FragmentClassProduct {
				return frag, nil
			}
		}
	}
	return nil, nil
}

func (d *ProductDeduplicator) findByIdentifierExact(ctx context.Context, key, value string) (*models.HorizonFragment, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" || d == nil || d.fragmentsRepo == nil {
		return nil, nil
	}
	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, key, value)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag != nil && frag.Class == ah.FragmentClassProduct {
				return frag, nil
			}
		}
	}
	return nil, nil
}

func (d *ProductDeduplicator) findByCanonicalPathVariants(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil || fragment == nil {
		return nil, nil
	}
	nameHints := []string{
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
		strings.TrimSpace(fragment.Identifiers["legalName"]),
	}
	current := strings.TrimSpace(fragment.CanonicalPath)
	for _, cp := range productCanonicalPathVariants(ctx, current, nameHints...) {
		cp = strings.TrimSpace(cp)
		if cp == "" || cp == current {
			continue
		}
		existing, err := d.fragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || existing == nil || strings.TrimSpace(existing.ID) == "" {
			continue
		}
		if existing.Class != "" && existing.Class != ah.FragmentClassProduct {
			continue
		}
		return existing, nil
	}
	return nil, nil
}

func candidateProductAliasNames(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
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
	add(fragment.Labels["en"])
	add(fragment.Identifiers["shortName"])
	add(fragment.Identifiers["legalName"])
	// Some product fragments may store the name only in labels without identifiers.
	return keysToSortedSlice(seen)
}

func keysToSortedSlice(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Deterministic ordering for stability (useful in tests/logging)
	// We avoid importing sort at file scope; use simple insertion sort due to small size.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1] > out[j] {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

func isSpecificModelAlias(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	// Must contain at least one digit.
	hasDigit := false
	hasLetter := false
	for _, r := range name {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	if !hasDigit || !hasLetter {
		return false
	}
	// Require at least two tokens to avoid merging generic codes like "M1".
	if len(strings.Fields(name)) < 2 {
		// Allow "F16" style single-token model aliases when they contain both letter+digit.
		// We'll generate variants for matching (e.g., "F16" -> "F-16" / "F 16") later.
		if hasDigit && hasLetter {
			return true
		}
		return false
	}
	return true
}

func modelAliasVariants(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
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
	add(name)
	add(insertSpaceBetweenLetterDigit(name))
	add(insertHyphenBetweenLetterDigit(name))
	return keysToSortedSlice(seen)
}

func insertSpaceBetweenLetterDigit(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	var prev rune
	hasPrev := false
	for _, r := range s {
		if hasPrev {
			if (unicode.IsLetter(prev) && unicode.IsDigit(r)) || (unicode.IsDigit(prev) && unicode.IsLetter(r)) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
		prev = r
		hasPrev = true
	}
	return b.String()
}

func insertHyphenBetweenLetterDigit(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	var prev rune
	hasPrev := false
	for _, r := range s {
		if hasPrev {
			if unicode.IsLetter(prev) && unicode.IsDigit(r) {
				b.WriteRune('-')
			}
		}
		b.WriteRune(r)
		prev = r
		hasPrev = true
	}
	return b.String()
}

// resolveWikidataID attempts to find a Wikidata ID for the product using the API.
func (d *ProductDeduplicator) resolveWikidataID(ctx context.Context, fragment *models.HorizonFragment) string {
	if d.extractor == nil {
		return ""
	}

	// Try to search by product name
	name := strings.TrimSpace(fragment.Labels["en"])
	if name == "" {
		name = strings.TrimSpace(fragment.Identifiers["shortName"])
	}
	if name == "" {
		return ""
	}

	// Search Wikidata for products matching this name
	qID, err := d.extractor.SearchProduct(ctx, name)
	if err != nil || qID == "" {
		return ""
	}

	return qID
}

// parseProductCanonicalPath parses a product canonical path into components.
// Returns category, type, stage, slug.
func parseProductCanonicalPath(path string) (category, productType, stage, slug string) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/product/") {
		return "", "", "", ""
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Flat structure: /product/{slug}
	if len(parts) == 2 {
		return "", "", "", parts[1]
	}

	// Hierarchical structure: /product/{category}/{type}/{stage}/{slug}
	if len(parts) == 5 {
		return parts[1], parts[2], parts[3], parts[4]
	}

	return "", "", "", ""
}
