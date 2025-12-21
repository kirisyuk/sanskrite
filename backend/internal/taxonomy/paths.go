package taxonomy

import (
	"fmt"
	"regexp"
	"strings"
)

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

// ValidCanonicalPathNamespace defines allowed namespace prefixes for canonical paths
var ValidCanonicalPathNamespace = map[string]bool{
	"org":     true,
	"person":  true,
	"product": true,
	"program": true,
	"market":  true,
	"fac":     true,
	"loc":     true,
	"sphere":  true,
	"event":   true,
}

// ValidateCanonicalPath validates that a canonical path matches the expected format.
// Returns an error if the path is invalid, nil otherwise.
func ValidateCanonicalPath(path string) error {
	if path == "" {
		return fmt.Errorf("canonical path cannot be empty")
	}
	if path == "/" {
		return fmt.Errorf("canonical path cannot be root path '/'")
	}

	// Must start with /
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("canonical path must start with '/': %s", path)
	}

	// Split into parts
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return fmt.Errorf("canonical path has no segments: %s", path)
	}

	// First segment must be a valid namespace
	namespace := parts[0]
	if !ValidCanonicalPathNamespace[namespace] {
		return fmt.Errorf("canonical path has invalid namespace '%s' (allowed: %v): %s",
			namespace, getValidNamespaces(), path)
	}

	// All segments must be non-empty after slugification
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("canonical path segment %d is empty: %s", i, path)
		}
		// Check that segment matches slug format (lowercase alphanumeric and hyphens)
		if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(part) {
			return fmt.Errorf("canonical path segment %d contains invalid characters: %s", i, path)
		}
	}

	return nil
}

// getValidNamespaces returns a sorted list of valid namespace names for error messages
func getValidNamespaces() []string {
	namespaces := make([]string, 0, len(ValidCanonicalPathNamespace))
	for ns := range ValidCanonicalPathNamespace {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// Slugify converts an arbitrary label into a stable, URL/path-friendly slug.
// This is used as a shared primitive between Horizon and Ontology to keep canonical paths consistent.
func Slugify(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = reNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return ""
	}
	return s
}

// JoinPath builds a canonical path using slugified segments.
// Example: JoinPath("org", "tsmc", "fab 21") -> "/org/tsmc/fab-21"
func JoinPath(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = Slugify(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

// GeoCanonicalPath returns a Horizon-canonical geo path.
// Canonical format: /loc/{country}/{admin1}/{city}
// - country: ISO3166-1 alpha2 lowercase
// - admin1: state/province code or slug lowercase; use "na" if not applicable/unknown
// - city: lowercase slug; use "na" if not applicable/unknown (country-only locations are /loc/{country}/na/na)
//
// If countryAlpha2 is missing/invalid, returns empty string (unknown location cannot be canonicalized safely).
func GeoCanonicalPath(countryAlpha2, admin1, city string) string {
	c := strings.ToLower(strings.TrimSpace(countryAlpha2))
	if !reCountryAlpha2.MatchString(c) {
		return ""
	}
	a1 := strings.ToLower(strings.TrimSpace(admin1))
	ct := strings.ToLower(strings.TrimSpace(city))
	if a1 == "" {
		a1 = "na"
	}
	if ct == "" {
		ct = "na"
	}
	a1 = Slugify(a1)
	ct = Slugify(ct)
	if a1 == "" {
		a1 = "na"
	}
	if ct == "" {
		ct = "na"
	}
	return "/" + strings.Join([]string{"loc", c, a1, ct}, "/")
}

// FacilityCanonicalPath returns a Horizon-canonical facility path.
// Preferred format (when orgSlug is known and stable): /fac/{orgSlug}/{facilitySlug}
// Fallback format: /fac/{facilitySlug}
func FacilityCanonicalPath(orgSlug string, facilityLabel string) string {
	orgSlug = Slugify(orgSlug)
	facSlug := Slugify(facilityLabel)
	if facSlug == "" {
		return ""
	}
	if orgSlug == "" {
		return JoinPath("fac", facSlug)
	}
	return JoinPath("fac", orgSlug, facSlug)
}

// ProductCanonicalPath returns a Horizon-canonical product path.
// Preferred format (when orgSlug is known and stable): /product/{orgSlug}/{productSlug}
// Fallback format: /product/{productSlug}
func ProductCanonicalPath(orgSlug string, productLabel string) string {
	orgSlug = Slugify(orgSlug)
	pSlug := Slugify(productLabel)
	if pSlug == "" {
		return ""
	}
	if orgSlug == "" {
		return JoinPath("product", pSlug)
	}
	return JoinPath("product", orgSlug, pSlug)
}

// ProgramCanonicalPath returns a Horizon-canonical program path.
// Preferred format (when jurisdiction is known): /program/{jurisdictionSlug}/{programSlug}
// Fallback format: /program/{programSlug}
func ProgramCanonicalPath(jurisdiction string, programLabel string) string {
	jSlug := Slugify(jurisdiction)
	pSlug := Slugify(programLabel)
	if pSlug == "" {
		return ""
	}
	if jSlug == "" {
		return JoinPath("program", pSlug)
	}
	return JoinPath("program", jSlug, pSlug)
}

// MarketCanonicalPath returns a Horizon-canonical market path.
// Preferred format (when domain is known): /market/{domainSlug}/{marketSlug}
// Fallback format: /market/{marketSlug}
func MarketCanonicalPath(domain string, marketLabel string) string {
	dSlug := Slugify(domain)
	mSlug := Slugify(marketLabel)
	if mSlug == "" {
		return ""
	}
	if dSlug == "" {
		return JoinPath("market", mSlug)
	}
	return JoinPath("market", dSlug, mSlug)
}

// EntityCanonicalPath returns a Horizon-first canonical path for an ontology entity.
// This mapping is intentionally opinionated and stable; Horizon taxonomy should converge on the same mapping.
func EntityCanonicalPath(entityType string, canonicalName string) string {
	switch strings.TrimSpace(entityType) {
	case "Organization":
		return JoinPath("org", canonicalName)
	case "Person":
		return JoinPath("person", canonicalName)
	case "Product":
		// Prefer a stable nested product path when org context is known elsewhere; here we may only have a label.
		return ProductCanonicalPath("", canonicalName)
	case "Facility":
		// Without a bound orgSlug, fall back to a stable single-segment facility slug.
		return FacilityCanonicalPath("", canonicalName)
	case "SupplyChainNode":
		// Horizon canonical taxonomy does not have a dedicated supply-chain namespace.
		// Represent supply chain nodes as facilities by default.
		return JoinPath("fac", canonicalName)
	case "GovernmentProject":
		// Treat government projects / initiatives as PROGRAM fragments in the Horizon taxonomy.
		return ProgramCanonicalPath("", canonicalName)
	case "Program":
		return ProgramCanonicalPath("", canonicalName)
	case "Market":
		return MarketCanonicalPath("", canonicalName)
	case "Location":
		// We cannot canonicalize a location without at least ISO3166-1 country code.
		// Location canonicalization should be done via GeoCanonicalPath(country, admin1, city).
		return ""
	default:
		// Avoid generating non-canonical namespaces.
		return ""
	}
}

// LocationCanonicalPath is legacy and intentionally conservative.
// Horizon canonical geo taxonomy is /loc/{country}/{admin1}/{city}; ISO6709/name-only inputs are insufficient.
// Prefer using GeoCanonicalPath.
func LocationCanonicalPath(name string, iso6709 string) string {
	_ = name
	_ = iso6709
	return ""
}
