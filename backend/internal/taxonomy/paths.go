package taxonomy

import (
	"fmt"
	"regexp"
	"strings"
)

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

// Basic Cyrillic -> Latin transliteration (best-effort).
// Rationale: canonical paths are restricted to ASCII slugs (a-z0-9-), but user inputs and KB labels
// frequently include Cyrillic. Without transliteration, Slugify() would drop those characters and
// produce empty/invalid slugs (e.g. "/org" instead of "/org/v-kazani-poymut").
var cyrillicTranslitMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func transliterateCyrillicToLatin(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if repl, ok := cyrillicTranslitMap[r]; ok {
			b.WriteString(repl)
			continue
		}
		// Keep ASCII as-is; everything else will be filtered by Slugify regex.
		if r <= 0x7f {
			b.WriteRune(r)
		} else {
			// Drop unknown non-ascii runes (keeps behavior deterministic).
			// NOTE: If we need broader coverage (e.g. Kazakh, Arabic), extend transliteration tables.
		}
	}
	return b.String()
}

// ValidCanonicalPathNamespace defines allowed namespace prefixes for canonical paths
var ValidCanonicalPathNamespace = map[string]bool{
	"org":     true,
	// Government / state bodies (jurisdiction-scoped; prevents cross-country name collisions).
	"gov":     true,
	"person":  true,
	"product": true,
	"program": true,
	"market":  true,
	// Market segments (ORG-level classification; may be materialized later).
	"segment": true,
	"fac":     true,
	"loc":     true,
	"sphere":  true,
	"event":   true,
	// Media endpoints (e.g., specific Telegram channels).
	"media": true,
	// Natural resource deposits (e.g., oil field, mine).
	"deposit": true,
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
	// Must have at least namespace + one segment ("/org" is not a valid concrete anchor).
	if len(parts) < 2 {
		return fmt.Errorf("canonical path must have at least 2 segments: %s", path)
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

	// Namespace-specific shape constraints (keep deterministic and conservative).
	//
	// IMPORTANT:
	// - We keep backward compatibility for legacy /org/{slug} paths.
	// - We enforce stricter shapes for /gov and /segment to avoid ambiguous merges.
	switch namespace {
	case "gov":
		// /gov/{iso2|na}/{slug}
		if len(parts) != 3 {
			return fmt.Errorf("gov canonical path must have exactly 3 segments: /gov/{iso2}/{slug}: %s", path)
		}
		cc := strings.ToLower(strings.TrimSpace(parts[1]))
		if !reCountryAlpha2.MatchString(cc) {
			return fmt.Errorf("gov canonical path has invalid country code %q (expected ISO3166-1 alpha2 or 'na'): %s", parts[1], path)
		}
	case "org":
		// Allowed:
		// - legacy: /org/{slug}
		// - preferred: /org/{iso2|na}/{slug}
		if len(parts) == 2 {
			return nil
		}
		if len(parts) != 3 {
			return fmt.Errorf("org canonical path must be /org/{slug} (legacy) or /org/{iso2}/{slug}: %s", path)
		}
		cc := strings.ToLower(strings.TrimSpace(parts[1]))
		if !reCountryAlpha2.MatchString(cc) {
			return fmt.Errorf("org canonical path has invalid country code %q (expected ISO3166-1 alpha2 or 'na'): %s", parts[1], path)
		}
	case "segment":
		// /segment/{domain}/{vertical}/{geo}
		// geo: ISO2 or "global"
		if len(parts) != 4 {
			return fmt.Errorf("segment canonical path must have exactly 4 segments: /segment/{domain}/{vertical}/{geo}: %s", path)
		}
		geo := strings.ToLower(strings.TrimSpace(parts[3]))
		if geo != "global" && !reCountryAlpha2.MatchString(geo) {
			return fmt.Errorf("segment canonical path has invalid geo %q (expected ISO3166-1 alpha2 or 'global'): %s", parts[3], path)
		}
	case "deposit":
		// Allowed:
		// - preferred: /deposit/{resource-slug}/{deposit-slug}
		// - legacy fallback: /deposit/{deposit-slug}
		//
		// IMPORTANT: We intentionally do NOT enforce resource allow-lists here to avoid breaking ingestion
		// when taxonomy drifts or LLM outputs a new resource type.
		if len(parts) < 2 {
			return fmt.Errorf("deposit canonical path must have at least 2 segments: /deposit/{slug} (legacy) or /deposit/{resource}/{slug}: %s", path)
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
	// Best-effort transliteration to avoid empty slugs for Cyrillic inputs.
	s = transliterateCyrillicToLatin(s)
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

// OrgCanonicalPath returns a Horizon-canonical organization path:
// /org/{iso2|na}/{slug}
//
// iso2 should be the country of origin / primary jurisdiction. If missing/invalid, "na" is used.
func OrgCanonicalPath(countryAlpha2OrNA string, orgLabel string) string {
	cc := strings.ToLower(strings.TrimSpace(countryAlpha2OrNA))
	if !reCountryAlpha2.MatchString(cc) {
		cc = "na"
	}
	slug := Slugify(orgLabel)
	if slug == "" {
		return ""
	}
	return JoinPath("org", cc, slug)
}

// GovCanonicalPath returns a Horizon-canonical government body path:
// /gov/{iso2|na}/{slug}
//
// If country is missing/invalid, "na" is used.
func GovCanonicalPath(countryAlpha2OrNA string, agencyLabel string) string {
	cc := strings.ToLower(strings.TrimSpace(countryAlpha2OrNA))
	if !reCountryAlpha2.MatchString(cc) {
		cc = "na"
	}
	slug := Slugify(agencyLabel)
	if slug == "" {
		return ""
	}
	return JoinPath("gov", cc, slug)
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
		// Organization canonical paths are jurisdiction-scoped to reduce cross-country name collisions.
		// If jurisdiction is unknown at this stage, use "na".
		return OrgCanonicalPath("na", canonicalName)
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
