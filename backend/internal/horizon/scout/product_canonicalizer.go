package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// =============================================================================
// PRODUCT CANONICALIZATION POLICY
// =============================================================================
// Products are canonicalized using hierarchical taxonomy:
// /product/{category}/{type}/{stage}/{slug}
//
// Categories: raw, component, finished, service
// Types: mineral, agricultural, chemical, electronic, mechanical, energy
// Stages: extraction, refining, processing, manufacturing, assembly
//
// This enables algorithmic upstream/downstream propagation without edges.

// ProductCanonicalizer handles product canonicalization using taxonomy.
type ProductCanonicalizer struct{}

// NewProductCanonicalizer creates a new product canonicalizer.
func NewProductCanonicalizer() *ProductCanonicalizer {
	return &ProductCanonicalizer{}
}

// CanonicalizeResult contains the result of product canonicalization.
type CanonicalizeResult struct {
	CanonicalPath string // /product/{category}/{type}/{stage}/{slug}
	Category      string // "raw", "component", "finished", "service"
	Type          string // "mineral", "agricultural", "chemical", etc.
	Stage         string // "extraction", "refining", "processing", etc.
	Slug          string // Product slug (e.g., "lithium-carbonate")
	Confidence    float64 // 0.0-1.0
	Reason        string  // Explanation of classification
}

// CanonicalizeProduct canonicalizes a product name into hierarchical canonical path.
// Priority:
// 1. Check known products in taxonomy
// 2. Normalize via synonyms
// 3. Infer category/type/stage from name patterns
// 4. Fallback to flat /product/{slug} if taxonomy unavailable
func (c *ProductCanonicalizer) CanonicalizeProduct(ctx context.Context, productName string) (*CanonicalizeResult, error) {
	if productName == "" {
		return nil, fmt.Errorf("product name is required")
	}

	// Step 1: Normalize name via synonyms
	normalizedName := taxonomy.NormalizeProductName(productName) // returns slug (guaranteed)
	if normalizedName == "" {
		normalizedName = Slugify(productName)
	}

	// Step 2: Check known products in taxonomy
	if spec := taxonomy.GetProductSpec(normalizedName); spec != nil {
		slug := normalizedName
		path := fmt.Sprintf("/product/%s/%s/%s/%s", spec.Category, spec.Type, spec.Stage, slug)
		return &CanonicalizeResult{
			CanonicalPath: path,
			Category:      spec.Category,
			Type:          spec.Type,
			Stage:         spec.Stage,
			Slug:          slug,
			Confidence:    1.0,
			Reason:        "known product in taxonomy",
		}, nil
	}

	// Step 3: Try to infer from name patterns
	result := c.inferFromPatterns(productName, normalizedName)
	if result != nil {
		return result, nil
	}

	// Step 4: Fallback to flat structure if taxonomy unavailable
	slug := normalizedName
	if slug == "" {
		slug = Slugify(productName)
	}
	return &CanonicalizeResult{
		CanonicalPath: "/product/" + slug,
		Category:      "",
		Type:          "",
		Stage:         "",
		Slug:          slug,
		Confidence:    0.5,
		Reason:        "fallback: taxonomy unavailable or unknown product",
	}, nil
}

// inferFromPatterns tries to infer category/type/stage from product name patterns.
func (c *ProductCanonicalizer) inferFromPatterns(originalName, normalizedName string) *CanonicalizeResult {
	name := strings.ToLower(originalName)
	slug := normalizedName
	if slug == "" {
		slug = Slugify(originalName)
	}

	// Pattern matching for common product types
	var category, productType, stage string

	// Raw materials patterns
	if strings.Contains(name, "ore") || strings.Contains(name, "mineral") || strings.Contains(name, "raw") {
		category = "raw"
		if strings.Contains(name, "lithium") || strings.Contains(name, "cobalt") || strings.Contains(name, "nickel") ||
			strings.Contains(name, "copper") || strings.Contains(name, "aluminum") || strings.Contains(name, "rare earth") {
			productType = "mineral"
			if strings.Contains(name, "ore") {
				stage = "extraction"
			} else {
				stage = "refining"
			}
		} else if strings.Contains(name, "wheat") || strings.Contains(name, "soy") || strings.Contains(name, "corn") ||
			strings.Contains(name, "rice") || strings.Contains(name, "agricultural") {
			productType = "agricultural"
			stage = "extraction"
		} else if strings.Contains(name, "oil") || strings.Contains(name, "gas") || strings.Contains(name, "lng") ||
			strings.Contains(name, "crude") || strings.Contains(name, "petroleum") {
			productType = "energy"
			stage = "extraction"
		}
	}

	// Component patterns
	if category == "" {
		if strings.Contains(name, "component") || strings.Contains(name, "part") || strings.Contains(name, "module") ||
			strings.Contains(name, "semiconductor") || strings.Contains(name, "wafer") || strings.Contains(name, "chip") ||
			strings.Contains(name, "battery") && (strings.Contains(name, "cell") || strings.Contains(name, "cathode") || strings.Contains(name, "anode")) {
			category = "component"
			if strings.Contains(name, "semiconductor") || strings.Contains(name, "wafer") || strings.Contains(name, "chip") ||
				strings.Contains(name, "electronic") {
				productType = "electronic"
				stage = "manufacturing"
			} else if strings.Contains(name, "battery") {
				productType = "electronic"
				if strings.Contains(name, "cathode") || strings.Contains(name, "anode") {
					stage = "processing"
				} else {
					stage = "manufacturing"
				}
			} else {
				productType = "mechanical"
				stage = "manufacturing"
			}
		}
	}

	// Finished goods patterns
	if category == "" {
		if strings.Contains(name, "phone") || strings.Contains(name, "smartphone") || strings.Contains(name, "laptop") ||
			strings.Contains(name, "computer") || strings.Contains(name, "vehicle") || strings.Contains(name, "car") ||
			strings.Contains(name, "ev") || strings.Contains(name, "electric vehicle") {
			category = "finished"
			if strings.Contains(name, "phone") || strings.Contains(name, "smartphone") || strings.Contains(name, "laptop") ||
				strings.Contains(name, "computer") {
				productType = "electronic"
			} else {
				productType = "mechanical"
			}
			stage = "assembly"
		}
	}

	// Chemical patterns
	if category == "" && (strings.Contains(name, "carbonate") || strings.Contains(name, "hydroxide") ||
		strings.Contains(name, "sulfate") || strings.Contains(name, "chemical") || strings.Contains(name, "compound")) {
		category = "raw"
		productType = "chemical"
		if strings.Contains(name, "lithium") {
			stage = "refining"
		} else {
			stage = "processing"
		}
	}

	// If we inferred something, validate against taxonomy
	if category != "" && productType != "" && stage != "" {
		// Validate against runtime taxonomy
		if taxonomy.IsValidProductCategory(category) &&
			taxonomy.IsValidProductType(productType) &&
			taxonomy.IsValidProductStage(stage) {
			path := fmt.Sprintf("/product/%s/%s/%s/%s", category, productType, stage, slug)
			return &CanonicalizeResult{
				CanonicalPath: path,
				Category:      category,
				Type:          productType,
				Stage:         stage,
				Slug:          slug,
				Confidence:    0.8,
				Reason:        fmt.Sprintf("inferred from patterns: category=%s, type=%s, stage=%s", category, productType, stage),
			}
		}
	}

	return nil
}

// BuildProductCanonicalPath builds a canonical path from components.
func BuildProductCanonicalPath(category, productType, stage, slug string) string {
	if category == "" || productType == "" || stage == "" || slug == "" {
		// Fallback to flat structure
		if slug != "" {
			return "/product/" + slug
		}
		return ""
	}
	return fmt.Sprintf("/product/%s/%s/%s/%s", category, productType, stage, slug)
}

// ParseProductCanonicalPath parses a product canonical path into components.
func ParseProductCanonicalPath(path string) (category, productType, stage, slug string, err error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/product/") {
		return "", "", "", "", fmt.Errorf("invalid product path prefix: %s", path)
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", "", fmt.Errorf("invalid product path format: %s", path)
	}

	// Flat structure: /product/{slug}
	if len(parts) == 2 {
		return "", "", "", parts[1], nil
	}

	// Hierarchical structure: /product/{category}/{type}/{stage}/{slug}
	if len(parts) == 5 {
		return parts[1], parts[2], parts[3], parts[4], nil
	}

	return "", "", "", "", fmt.Errorf("invalid product path format: expected 2 or 5 parts, got %d", len(parts))
}

