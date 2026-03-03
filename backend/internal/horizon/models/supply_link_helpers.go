package models

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// BuildSupplyLinkCanonicalPath generates a canonical path for a SupplyLink fragment.
// Format: /supply-link/{tenant}/{supplier-slug}/{product-slug}/{customer-slug}
// If supplier or customer is nil, uses "natural" or "end-consumer" respectively.
func BuildSupplyLinkCanonicalPath(tenantID string, supplierRef, productRef, customerRef *string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("tenantID must be set for SupplyLink")
	}
	if productRef == nil || strings.TrimSpace(*productRef) == "" {
		return "", fmt.Errorf("productRef is required for SupplyLink")
	}

	// Extract slugs from canonical paths
	supplierSlug := "natural"
	if supplierRef != nil && strings.TrimSpace(*supplierRef) != "" {
		supplierSlug = extractSlugFromPath(*supplierRef)
		if supplierSlug == "" {
			supplierSlug = "natural"
		}
	}

	productSlug := extractSlugFromPath(*productRef)
	if productSlug == "" {
		return "", fmt.Errorf("invalid productRef: %s", *productRef)
	}

	customerSlug := "end-consumer"
	if customerRef != nil && strings.TrimSpace(*customerRef) != "" {
		customerSlug = extractSlugFromPath(*customerRef)
		if customerSlug == "" {
			customerSlug = "end-consumer"
		}
	}

	// Normalize slugs (lowercase, replace spaces/special chars with hyphens)
	supplierSlug = normalizeSlug(supplierSlug)
	productSlug = normalizeSlug(productSlug)
	customerSlug = normalizeSlug(customerSlug)

	return fmt.Sprintf("/supply-link/%s/%s/%s/%s", tenantID, supplierSlug, productSlug, customerSlug), nil
}

// NormalizeOrgRefToFacilityFallback converts an org path to the facility fallback path.
// When org is used as supplier/customer but facility is unknown: /org/X -> /fac/X-general-production.
// For geo-prefixed orgs: /org/geo/X -> /fac/geo-X-general-production (slugified).
// Returns the org path unchanged if it is already /fac/ or /deposit/; empty string if input is empty.
func NormalizeOrgRefToFacilityFallback(orgPath string) string {
	orgPath = strings.TrimSpace(orgPath)
	if orgPath == "" {
		return ""
	}
	if strings.HasPrefix(orgPath, "/fac/") || strings.HasPrefix(orgPath, "/deposit/") {
		return orgPath
	}
	if !strings.HasPrefix(orgPath, "/org/") {
		return ""
	}
	// Extract org slug: last segment of path (e.g. /org/cn/tsmc -> tsmc, /org/us/albemarle-corp -> albemarle-corp)
	slug := extractSlugFromPath(orgPath)
	if slug == "" {
		return ""
	}
	// For multi-segment paths like /org/cn/tsmc, use tsmc. For /org/geo/cn, use geo-cn or similar.
	// Path format: /org/{geo?}/{slug} — last segment is org slug.
	return taxonomy.FacilityGeneralProductionPath(slug)
}

// extractSlugFromPath extracts the last segment from a canonical path.
// E.g., "/org/albemarle" -> "albemarle", "/product/lithium-carbonate" -> "lithium-carbonate"
func extractSlugFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// Remove leading slash
	path = strings.TrimPrefix(path, "/")
	// Split by "/" and take the last segment
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// normalizeSlug normalizes a slug for use in canonical paths.
// Converts to lowercase, replaces spaces and special chars with hyphens.
func normalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove any non-alphanumeric characters except hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()
	// Remove consecutive hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	// Remove leading/trailing hyphens
	s = strings.Trim(s, "-")
	if s == "" {
		return "unknown"
	}
	return s
}

// ValidateSupplyLinkFields validates that a fragment has valid SupplyLink fields.
// This is a more detailed validation than the general Validate() method.
func ValidateSupplyLinkFields(f *HorizonFragment) error {
	if f == nil {
		return fmt.Errorf("fragment is nil")
	}
	if f.Type != FragmentTypeSupplyLink {
		return fmt.Errorf("fragment type must be SUPPLY_LINK")
	}
	// SupplyLink can be tenant-scoped OR global-scoped (global = assumption layer).
	if f.TenantID == "" {
		return fmt.Errorf("tenantID must be set for SUPPLY_LINK")
	}
	if f.ProductRef == nil || strings.TrimSpace(*f.ProductRef) == "" {
		return fmt.Errorf("productRef is required for SUPPLY_LINK")
	}
	// Validate productRef is a valid canonical path
	if !strings.HasPrefix(*f.ProductRef, "/product/") {
		return fmt.Errorf("productRef must be a product canonical path (starts with /product/)")
	}
	// SupplierRef: MUST be /fac/ or /deposit/ (extraction). NOT /org/ — use NormalizeOrgRefToFacilityFallback before validation.
	if f.SupplierRef != nil && strings.TrimSpace(*f.SupplierRef) != "" {
		p := strings.TrimSpace(*f.SupplierRef)
		if !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/deposit/") {
			return fmt.Errorf("supplierRef must be facility or deposit canonical path (starts with /fac/ or /deposit/); /org/ is not allowed")
		}
	}
	// CustomerRef: MUST be /fac/. NOT /org/ — use NormalizeOrgRefToFacilityFallback before validation.
	if f.CustomerRef != nil && strings.TrimSpace(*f.CustomerRef) != "" {
		p := strings.TrimSpace(*f.CustomerRef)
		if !strings.HasPrefix(p, "/fac/") {
			return fmt.Errorf("customerRef must be facility canonical path (starts with /fac/); /org/ is not allowed")
		}
	}
	// Validate facility refs if present
	if f.SupplierFacilityRef != nil && strings.TrimSpace(*f.SupplierFacilityRef) != "" {
		if !strings.HasPrefix(*f.SupplierFacilityRef, "/fac/") {
			return fmt.Errorf("supplierFacilityRef must be a facility canonical path (starts with /fac/)")
		}
	}
	if f.CustomerFacilityRef != nil && strings.TrimSpace(*f.CustomerFacilityRef) != "" {
		if !strings.HasPrefix(*f.CustomerFacilityRef, "/fac/") {
			return fmt.Errorf("customerFacilityRef must be a facility canonical path (starts with /fac/)")
		}
	}
	// Validate transportViaRefs if present (ports, terminals, pipelines, warehouses)
	for _, p := range f.TransportViaRefs {
		p = strings.TrimSpace(p)
		if p != "" && !strings.HasPrefix(p, "/fac/") {
			return fmt.Errorf("transportViaRefs entries must be facility canonical paths (start with /fac/)")
		}
	}
	// At least one of SupplierRef or CustomerRef must be set
	if (f.SupplierRef == nil || strings.TrimSpace(*f.SupplierRef) == "") &&
		(f.CustomerRef == nil || strings.TrimSpace(*f.CustomerRef) == "") {
		return fmt.Errorf("at least one of supplierRef or customerRef must be set for SUPPLY_LINK")
	}
	return nil
}

// IsSupplyLink checks if a fragment is a SupplyLink.
func IsSupplyLink(f *HorizonFragment) bool {
	return f != nil && f.Type == FragmentTypeSupplyLink
}

// GetSupplyLinkKey returns a stable key for a SupplyLink (for deduplication).
// Format: {tenantID}:{supplierRef}:{productRef}:{customerRef}
func GetSupplyLinkKey(f *HorizonFragment) string {
	if !IsSupplyLink(f) {
		return ""
	}
	supplier := "natural"
	if f.SupplierRef != nil {
		supplier = *f.SupplierRef
	}
	customer := "end-consumer"
	if f.CustomerRef != nil {
		customer = *f.CustomerRef
	}
	product := ""
	if f.ProductRef != nil {
		product = *f.ProductRef
	}
	return fmt.Sprintf("%s:%s:%s:%s", f.TenantID, supplier, product, customer)
}

