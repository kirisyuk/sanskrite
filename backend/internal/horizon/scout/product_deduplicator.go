package scout

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

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
	if fragment == nil || fragment.Type != models.FragmentTypeProduct {
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

	// Strategy 5: Try to resolve Wikidata ID via API if we have a client
	if d.wikidataClient != nil && wikidataID == "" {
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
			if frag.Type == models.FragmentTypeProduct {
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
			if frag.Type == models.FragmentTypeProduct {
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
			if frag.Type != models.FragmentTypeProduct {
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

	existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", name, models.FragmentTypeProduct)
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

