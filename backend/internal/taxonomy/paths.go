package taxonomy

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

// Regional geo tokens allowed in /loc/{cc}/... when ISO2 is insufficient.
// This is a conservative allowlist and can be extended via env.
var regionalGeoTokens = map[string]struct{}{
	// Regional buckets (shared with /segment geo rules).
	"global":  {},
	"mena":    {},
	"emea":    {},
	"apac":    {},
	"latam":   {},
	"na":      {},
	"gcc":     {},
	"eu":      {},
	"asean":   {},
	"nordics": {},
	"benelux": {},
	"anz":     {},
	"cis":     {},
	"ssa":     {},
	"usmca":   {},
	"nafta":   {},

	// Common regional blocs / treaties.
	"brics":           {},
	"g7":              {},
	"g20":             {},
	"opec":            {},
	"oecd":            {},
	"apec":            {},
	"mercosur":        {},
	"caricom":         {},
	"au":              {}, // African Union (kept as token; avoid ISO2 conflict via context)
	"african-union":   {},
	"arab-league":     {},
	"eaeu":            {},
	"csto":            {},
	"sco":             {},
	"rcep":            {},
	"cptpp":           {},
	"bimstec":         {},
	"eac":             {},
	"comesa":          {},
	"igad":            {},
	"sadc":            {},
	"ecowas":          {},
	"pacific-islands": {},
}

// IsRegionalGeoToken returns true for allowed non-ISO2 regional geo tokens.
func IsRegionalGeoToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	_, ok := regionalGeoTokens[token]
	if ok {
		return true
	}
	// Allow runtime extensions via env var: HORIZON_REGIONAL_GEO_TOKENS="asean,brics,custom-bloc"
	if extra := strings.TrimSpace(os.Getenv("HORIZON_REGIONAL_GEO_TOKENS")); extra != "" {
		for _, part := range strings.Split(extra, ",") {
			if strings.TrimSpace(strings.ToLower(part)) == token {
				return true
			}
		}
	}
	return false
}

// RegionalGeoTokenList returns a sorted list of allowed regional geo tokens for prompts.
// Used in canonical path rules so LLM knows valid values for {geo} in /org, /gov, /loc, /market, /community, /program.
// Includes: global, int (for org only), na, plus regional blocs (gcc, eu, asean, brics, opec, etc.).
func RegionalGeoTokenList() []string {
	tokens := make([]string, 0, len(regionalGeoTokens)+2)
	for t := range regionalGeoTokens {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	return tokens
}

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
	"org": true,
	// Government / state bodies (jurisdiction-scoped; prevents cross-country name collisions).
	"gov":     true,
	"person":  true,
	"product": true,
	"program": true,
	"market":  true,
	// Executable “knowledge contract” nodes (domain/inquiry focus).
	"domain": true,
	// Informal communities (trade/professional/etc.).
	"community": true,
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
	// Strategic decision layer (domain-scoped decision points).
	"decision": true,
	// Strategic mechanisms of control (global tools of control).
	"mechanism": true,
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
		// /gov/{geo}/{slug}
		// geo: ISO3166-1 alpha2, composite (tr-sy, ae-sa-om), na, or regional (eu, asean, brics, gcc, etc.)
		// Regional tokens allow EU/ASEAN/BRICS-level bodies (European Commission, ASEAN Secretariat, etc.)
		if len(parts) != 3 {
			return fmt.Errorf("gov canonical path must have exactly 3 segments: /gov/{geo}/{slug}: %s", path)
		}
		geo := strings.ToLower(strings.TrimSpace(parts[1]))
		if geo != "na" && !reCountryAlpha2.MatchString(geo) && !isCompositeGeo(geo) && !IsRegionalGeoToken(geo) {
			return fmt.Errorf("gov canonical path has invalid geo %q (expected ISO3166-1 alpha2, composite, na, or regional token like eu/asean/brics): %s", parts[1], path)
		}
	case "org":
		// Allowed:
		// - legacy: /org/{slug}
		// - preferred: /org/{geo}/{slug}
		// geo: ISO3166-1 alpha2, composite (tr-sy, ae-sa-om), na, int, or regional (eu, asean, brics, gcc, etc.)
		// Regional tokens allow EU/ASEAN/BRICS-level orgs (European Commission, ASEAN Secretariat, etc.)
		if len(parts) == 2 {
			return nil
		}
		if len(parts) != 3 {
			return fmt.Errorf("org canonical path must be /org/{slug} (legacy) or /org/{geo}/{slug}: %s", path)
		}
		geo := strings.ToLower(strings.TrimSpace(parts[1]))
		if geo != "na" && geo != "int" && !reCountryAlpha2.MatchString(geo) && !isCompositeGeo(geo) && !IsRegionalGeoToken(geo) {
			return fmt.Errorf("org canonical path has invalid geo %q (expected ISO3166-1 alpha2, composite, na, int, or regional token like eu/asean/brics): %s", parts[1], path)
		}
	case "loc":
		// /loc/{cc}/{admin1}/{city}
		if len(parts) != 4 {
			return fmt.Errorf("loc canonical path must have exactly 4 segments: /loc/{cc}/{admin1}/{city}: %s", path)
		}
		cc := strings.ToLower(strings.TrimSpace(parts[1]))
		if !reCountryAlpha2.MatchString(cc) && !IsRegionalGeoToken(cc) && !isCompositeGeo(cc) {
			return fmt.Errorf("loc canonical path has invalid country/region %q (expected ISO3166-1 alpha2, composite, or regional token): %s", parts[1], path)
		}
	case "community":
		// /community/{geo}/{type}/{slug}
		// Legacy: /community/{geo}/{slug}
		if len(parts) != 3 && len(parts) != 4 {
			return fmt.Errorf("community canonical path must be /community/{geo}/{slug} (legacy) or /community/{geo}/{type}/{slug}: %s", path)
		}
		geo := strings.ToLower(strings.TrimSpace(parts[1]))
		if geo == "" {
			return fmt.Errorf("community canonical path has empty geo: %s", path)
		}
		if geo != "global" && !reCountryAlpha2.MatchString(geo) && !IsRegionalGeoToken(geo) && !isCompositeGeo(geo) {
			return fmt.Errorf("community canonical path has invalid geo %q (expected ISO3166-1 alpha2, composite, or regional token): %s", parts[1], path)
		}
	case "segment":
		// /segment/{domain}/{vertical}/{geo}
		// geo: ISO2 or a regional bucket: global, mena, emea, apac, latam, na, gcc, eu, asean, nordics, benelux, anz, cis, ssa, usmca, nafta
		if len(parts) != 4 {
			return fmt.Errorf("segment canonical path must have exactly 4 segments: /segment/{domain}/{vertical}/{geo}: %s", path)
		}
		geo := strings.ToLower(strings.TrimSpace(parts[3]))
		if geo != "global" && geo != "mena" && geo != "emea" && geo != "apac" && geo != "latam" && geo != "na" && geo != "gcc" && geo != "eu" && geo != "asean" && geo != "nordics" && geo != "benelux" && geo != "anz" && geo != "cis" && geo != "ssa" && geo != "usmca" && geo != "nafta" && !reCountryAlpha2.MatchString(geo) {
			return fmt.Errorf("segment canonical path has invalid geo %q (expected ISO3166-1 alpha2 or regional bucket: global, mena, emea, apac, latam, na, gcc, eu, asean, nordics, benelux, anz, cis, ssa, usmca, nafta): %s", parts[3], path)
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
	case "domain":
		// /domain/v1/{contractType}/{facetKey}/{facetValue}...
		// The path is a machine-readable contract; it must be parseable and deterministic.
		if err := validateDomainCanonicalPathParts(parts, path); err != nil {
			return err
		}
	case "decision":
		// /decision/{domain-slug}/{decision-slug}
		if len(parts) != 3 {
			return fmt.Errorf("decision canonical path must have exactly 3 segments: /decision/{domain-slug}/{decision-slug}: %s", path)
		}
	case "mechanism":
		// /mechanism/{category}/{slug}
		if len(parts) != 3 {
			return fmt.Errorf("mechanism canonical path must have exactly 3 segments: /mechanism/{category}/{slug}: %s", path)
		}
		category := strings.ToLower(strings.TrimSpace(parts[1]))
		switch category {
		case "regulatory", "economic", "technological", "contractual", "political":
			// ok
		default:
			return fmt.Errorf("mechanism canonical path has invalid category %q (expected one of: regulatory, economic, technological, contractual, political): %s", parts[1], path)
		}
	}

	return nil
}

// =============================================================================
// DOMAIN canonical paths (/domain/v1/...)
// =============================================================================

var domainContractTypes = map[string]struct{}{
	// Supply chain & industry mapping
	"supply-chain":    {},
	"value-chain":     {},
	"logistics-flow":  {},
	"trade-flow":      {},
	"sourcing-map":    {},
	"procurement-map": {},

	// Risk / geopolitics
	"risk-assessment":     {},
	"geopolitical-risk":   {},
	"bilateral-relations": {},
	"sanctions-exposure":  {},
	"conflict-dynamics":   {},

	// Markets / opportunity / competitive
	"market-opportunity":       {},
	"competitive-landscape":    {},
	"geographic-concentration": {},
	"market-entry":             {},
	"market-sizing":            {},
	"demand-forecast":          {},
	"pricing-dynamics":         {},

	// Investment / finance
	"investment-thesis":  {},
	"funding-landscape":  {},
	"exit-strategy":      {},
	"valuation-drivers":  {},
	"portfolio-exposure": {},

	// Technology / innovation
	"technology-landscape": {},
	"innovation-map":       {},
	"patent-landscape":     {},
	"rd-landscape":         {},
	"adoption-curve":       {},
	"disruption-analysis":  {},

	// Ecosystem / stakeholder / network
	"ecosystem-mapping":     {},
	"stakeholder-mapping":   {},
	"partnership-landscape": {},
	"influence-network":     {},
	"power-dynamics":        {},

	// Policy / regulation / compliance
	"regulatory-impact":       {},
	"policy-and-regulation":   {},
	"compliance-landscape":    {},
	"security-and-compliance": {},
	"licensing-requirements":  {},
	"esg-landscape":           {},

	// Scenarios / trends / forecasting
	"scenario-analysis": {},
	"macro-trends":      {},
	"megatrends":        {},
	"future-outlook":    {},

	// Sector / industry specific
	"sector-overview":    {},
	"industry-structure": {},
	"vertical-deep-dive": {},

	// Catch-all: /domain/v1/other/{custom-slug}
	// When the user's intent doesn't fit any predefined contract type,
	// the LLM can use "other" and provide a descriptive slug.
	"other": {},
}

var domainFacetKeys = map[string]struct{}{
	"geo":         {},
	"topic":       {},
	"subject":     {},
	"project":     {},
	"asset":       {},
	"material":    {},
	"application": {},
	"sector":      {},
	"industry":    {},
	"market":      {},
	"regulation":  {},
	"framework":   {},
	"time":        {},
}

// DomainContractTypes returns all supported /domain/v1 contract types in stable order.
func DomainContractTypes() []string {
	out := make([]string, 0, len(domainContractTypes))
	for ct := range domainContractTypes {
		out = append(out, ct)
	}
	sort.Strings(out)
	return out
}

func validateDomainCanonicalPathParts(parts []string, originalPath string) error {
	// parts already trimmed/split from ValidateCanonicalPath
	// Must be: ["domain","v1",contractType,(k,v)*]
	// Special case: ["domain","v1","other",{custom-slug}] — allows any slug after "other"
	if len(parts) < 3 {
		return fmt.Errorf("domain canonical path must have at least 3 segments: /domain/v1/{contractType}: %s", originalPath)
	}
	if parts[1] != "v1" {
		return fmt.Errorf("domain canonical path must use version v1: %s", originalPath)
	}
	ct := strings.ToLower(strings.TrimSpace(parts[2]))
	if ct == "" {
		return fmt.Errorf("domain canonical path has empty contractType: %s", originalPath)
	}
	if _, ok := domainContractTypes[ct]; !ok {
		return fmt.Errorf("domain canonical path has invalid contractType %q (allowlist): %s", parts[2], originalPath)
	}

	// Special handling for "other" contract type:
	// Format: /domain/v1/other/{custom-slug}[/{facetKey}/{facetValue}]*
	// The custom-slug is required (must be a valid slug: lowercase, hyphens allowed, no spaces).
	if ct == "other" {
		if len(parts) < 4 {
			return fmt.Errorf("domain canonical path with contractType 'other' must have a custom slug: /domain/v1/other/{custom-slug}: %s", originalPath)
		}
		customSlug := strings.ToLower(strings.TrimSpace(parts[3]))
		if customSlug == "" {
			return fmt.Errorf("domain canonical path with contractType 'other' has empty custom slug: %s", originalPath)
		}
		// Validate custom slug format: only lowercase alphanumeric and hyphens, no leading/trailing hyphens.
		if !isValidDomainOtherSlug(customSlug) {
			return fmt.Errorf("domain canonical path 'other' has invalid custom slug %q (must be lowercase alphanumeric with hyphens): %s", customSlug, originalPath)
		}
		// After custom slug, optional facets are allowed (standard key/value pairs).
		rest := parts[4:]
		if len(rest)%2 != 0 {
			return fmt.Errorf("domain canonical path facets must be key/value pairs (even segments after custom slug): %s", originalPath)
		}
		for i := 0; i < len(rest); i += 2 {
			k := strings.ToLower(strings.TrimSpace(rest[i]))
			v := strings.ToLower(strings.TrimSpace(rest[i+1]))
			if k == "" || v == "" {
				return fmt.Errorf("domain canonical path has empty facet key/value near %q/%q: %s", rest[i], rest[i+1], originalPath)
			}
			if _, ok := domainFacetKeys[k]; !ok {
				return fmt.Errorf("domain canonical path has invalid facet key %q (allowlist): %s", rest[i], originalPath)
			}
			if k == "geo" {
				if !isValidDomainGeoToken(v) {
					return fmt.Errorf("domain canonical path has invalid geo facet %q: %s", rest[i+1], originalPath)
				}
			}
		}
		return nil
	}

	// Standard contract types: facets are key/value pairs.
	rest := parts[3:]
	if len(rest)%2 != 0 {
		return fmt.Errorf("domain canonical path facets must be key/value pairs (even segments after contractType): %s", originalPath)
	}
	for i := 0; i < len(rest); i += 2 {
		k := strings.ToLower(strings.TrimSpace(rest[i]))
		v := strings.ToLower(strings.TrimSpace(rest[i+1]))
		if k == "" || v == "" {
			return fmt.Errorf("domain canonical path has empty facet key/value near %q/%q: %s", rest[i], rest[i+1], originalPath)
		}
		if _, ok := domainFacetKeys[k]; !ok {
			return fmt.Errorf("domain canonical path has invalid facet key %q (allowlist): %s", rest[i], originalPath)
		}
		if k == "geo" {
			if !isValidDomainGeoToken(v) {
				return fmt.Errorf("domain canonical path has invalid geo facet %q: %s", rest[i+1], originalPath)
			}
		}
	}
	return nil
}

// isValidDomainOtherSlug validates custom slugs for /domain/v1/other/{slug}.
// Rules: lowercase alphanumeric and hyphens; no leading/trailing/consecutive hyphens; min 2 chars.
func isValidDomainOtherSlug(slug string) bool {
	if len(slug) < 2 {
		return false
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return false
	}
	if strings.Contains(slug, "--") {
		return false
	}
	for _, r := range slug {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

// NormalizeDomainCanonicalPath canonicalizes the ordering of facets for /domain/v1/... paths.
//
// Goal: deterministic canonical paths for deduplication and stable identity.
// Rules:
// - The path MUST already be a valid canonical path (ValidateCanonicalPath).
// - Facet key/value pairs are normalized to lowercase.
// - Facets are re-ordered deterministically by (key, value).
// - Special case: bilateral-relations domains normalize geo facets into ONE composite token: geo/{sorted-joined}.
// - Special case: "other" contract type has custom slug at index 3, then optional facets.
func NormalizeDomainCanonicalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("domain path is empty")
	}
	if !strings.HasPrefix(path, "/domain/") {
		return "", fmt.Errorf("not a domain canonical path: %s", path)
	}
	if err := ValidateCanonicalPath(path); err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// ["domain","v1",contractType,(k,v)*] OR ["domain","v1","other",customSlug,(k,v)*]
	if len(parts) < 3 {
		return "", fmt.Errorf("domain canonical path must have at least 3 segments: %s", path)
	}
	contractType := strings.ToLower(strings.TrimSpace(parts[2]))

	type kv struct {
		k string
		v string
	}

	// Special handling for "other" contract type:
	// Format: /domain/v1/other/{custom-slug}[/{facetKey}/{facetValue}]*
	if contractType == "other" {
		if len(parts) < 4 {
			return "", fmt.Errorf("domain 'other' must have custom slug: %s", path)
		}
		customSlug := strings.ToLower(strings.TrimSpace(parts[3]))
		rest := parts[4:]
		if len(rest)%2 != 0 {
			return "", fmt.Errorf("domain canonical path facets must be key/value pairs: %s", path)
		}
		pairs := make([]kv, 0, len(rest)/2)
		for i := 0; i < len(rest); i += 2 {
			k := strings.ToLower(strings.TrimSpace(rest[i]))
			v := strings.ToLower(strings.TrimSpace(rest[i+1]))
			if k == "" || v == "" {
				continue
			}
			pairs = append(pairs, kv{k: k, v: v})
		}
		// Sort facets deterministically.
		sort.SliceStable(pairs, func(i, j int) bool {
			if pairs[i].k != pairs[j].k {
				return pairs[i].k < pairs[j].k
			}
			return pairs[i].v < pairs[j].v
		})
		var out strings.Builder
		out.WriteString("/domain/v1/other/")
		out.WriteString(customSlug)
		for _, p := range pairs {
			out.WriteString("/")
			out.WriteString(p.k)
			out.WriteString("/")
			out.WriteString(p.v)
		}
		return out.String(), nil
	}

	// Standard contract types.
	rest := parts[3:]
	if len(rest)%2 != 0 {
		return "", fmt.Errorf("domain canonical path facets must be key/value pairs: %s", path)
	}
	pairs := make([]kv, 0, len(rest)/2)
	for i := 0; i < len(rest); i += 2 {
		k := strings.ToLower(strings.TrimSpace(rest[i]))
		v := strings.ToLower(strings.TrimSpace(rest[i+1]))
		if k == "" || v == "" {
			continue
		}
		pairs = append(pairs, kv{k: k, v: v})
	}

	// Contract-specific normalization.
	if contractType == "bilateral-relations" {
		geoSet := map[string]struct{}{}
		others := make([]kv, 0, len(pairs))
		for _, p := range pairs {
			if p.k == "geo" {
				// Accept both repeated geo tokens and composite geo tokens (ae-sa).
				// Expand composite into individual ISO2s for deterministic ordering.
				if strings.Contains(p.v, "-") {
					parts := strings.Split(p.v, "-")
					for _, part := range parts {
						part = strings.ToLower(strings.TrimSpace(part))
						if reCountryAlpha2.MatchString(part) {
							geoSet[part] = struct{}{}
						}
					}
				} else if reCountryAlpha2.MatchString(p.v) {
					geoSet[p.v] = struct{}{}
				} else if isCompositeGeo(p.v) {
					// Fallback for composites that include regional tokens; keep as-is.
					geoSet[p.v] = struct{}{}
				}
				continue
			}
			others = append(others, p)
		}
		// Sort geo values and join into one composite token.
		geos := make([]string, 0, len(geoSet))
		for g := range geoSet {
			geos = append(geos, g)
		}
		sort.Strings(geos)
		compositeGeo := strings.Join(geos, "-")

		sort.SliceStable(others, func(i, j int) bool {
			if others[i].k != others[j].k {
				return others[i].k < others[j].k
			}
			return others[i].v < others[j].v
		})
		pairs = pairs[:0]
		if compositeGeo != "" {
			pairs = append(pairs, kv{k: "geo", v: compositeGeo})
		}
		pairs = append(pairs, others...)
	} else {
		// Default deterministic ordering.
		sort.SliceStable(pairs, func(i, j int) bool {
			if pairs[i].k != pairs[j].k {
				return pairs[i].k < pairs[j].k
			}
			return pairs[i].v < pairs[j].v
		})
	}

	var out strings.Builder
	out.WriteString("/domain/v1/")
	out.WriteString(contractType)
	for _, p := range pairs {
		out.WriteString("/")
		out.WriteString(p.k)
		out.WriteString("/")
		out.WriteString(p.v)
	}
	return out.String(), nil
}

func isValidDomainGeoToken(geo string) bool {
	geo = strings.ToLower(strings.TrimSpace(geo))
	if geo == "" {
		return false
	}
	// Allow same family as /loc and /org plus "int" for international scope.
	if geo == "global" || geo == "int" {
		return true
	}
	if reCountryAlpha2.MatchString(geo) {
		return true
	}
	if IsRegionalGeoToken(geo) {
		return true
	}
	if isCompositeGeo(geo) {
		return true
	}
	return false
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
	if !reCountryAlpha2.MatchString(c) && !IsRegionalGeoToken(c) {
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
// /org/{geo}/{name-slug}-{legal-form}
//
// geo is the geographic jurisdiction:
//   - Default: ISO3166-1 alpha2 (exactly 2 lowercase letters, e.g., "us", "sa", "kz")
//   - Composite: 2-3 ISO3166-1 alpha2 codes joined by "-" (e.g., "tr-sy", "ae-sa-om")
//   - Unknown: "na" if jurisdiction is unknown/invalid
//
// Legal form is extracted from orgLabel and normalized to canonical English form (lowercase).
// If no legal form detected, slug is created without legal form suffix.
//
// Legal form is extracted from orgLabel and normalized to canonical English form (lowercase).
// If no legal form detected, slug is created without legal form suffix.
func OrgCanonicalPath(countryAlpha2OrNA string, orgLabel string) string {
	cc := strings.ToLower(strings.TrimSpace(countryAlpha2OrNA))
	if !reCountryAlpha2.MatchString(cc) && !isCompositeGeo(cc) {
		cc = "na"
	}

	// Extract legal form from name
	nameWithoutForm, legalFormCanonical, _ := extractLegalFormFromName(orgLabel)

	// Create slug from name (without legal form)
	var nameSlug string
	if nameWithoutForm != "" {
		nameSlug = Slugify(nameWithoutForm)
	} else {
		nameSlug = Slugify(orgLabel)
	}

	if nameSlug == "" {
		return ""
	}

	// Fix: strip any existing legal form suffix from slug to avoid double suffixes (e.g., "-inc-inc")
	nameSlug = stripLegalFormSuffix(nameSlug)

	// Append legal form if found
	if legalFormCanonical != "" {
		nameSlug = nameSlug + "-" + legalFormCanonical
	}

	return JoinPath("org", cc, nameSlug)
}

// stripLegalFormSuffix removes any trailing legal form suffix from a slug to prevent double suffixes.
// e.g., "amkor-technology-inc" -> "amkor-technology", "canon-inc-inc" -> "canon"
func stripLegalFormSuffix(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return slug
	}
	legalForms := []string{"inc", "llc", "jsc", "pjsc", "ltd", "corp", "plc", "gmbh", "sarl", "srl", "ag", "sa", "spa", "as", "ab", "bv", "nv", "se", "co", "pty"}
	changed := true
	for changed {
		changed = false
		for _, lf := range legalForms {
			suffix := "-" + lf
			if strings.HasSuffix(slug, suffix) {
				slug = strings.TrimSuffix(slug, suffix)
				changed = true
				break
			}
		}
	}
	return slug
}

// isCompositeGeo checks if a string is a valid composite geo (e.g., "tr-sy", "ae-sa-om").
// Composite geo consists of 2-3 ISO3166-1 alpha2 codes joined by hyphens, sorted lexicographically.
// Each component must be exactly 2 lowercase letters (ISO3166-1 alpha2).
func isCompositeGeo(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, "-")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	// Each part must be valid ISO3166-1 alpha2 (exactly 2 lowercase letters)
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if !reCountryAlpha2.MatchString(part) {
			return false
		}
	}
	// Note: Lexicographic sorting is recommended but not enforced here
	// (can be normalized in a separate step if needed)
	return true
}

// IsCompositeGeoToken returns true for composite ISO2 geo tokens (e.g., "tr-sy", "ae-sa-om").
func IsCompositeGeoToken(s string) bool {
	return isCompositeGeo(strings.ToLower(strings.TrimSpace(s)))
}

// GovCanonicalPath returns a Horizon-canonical government body path:
// /gov/{geo}/{agency-slug}
//
// geo is the geographic jurisdiction:
//   - Default: ISO3166-1 alpha2 (exactly 2 lowercase letters, e.g., "us", "sa")
//   - Composite: 2-3 ISO3166-1 alpha2 codes joined by "-" for intergovernmental bodies (e.g., "ae-sa-om")
//   - Unknown: "na" if jurisdiction is unknown/invalid
func GovCanonicalPath(countryAlpha2OrNA string, agencyLabel string) string {
	cc := strings.ToLower(strings.TrimSpace(countryAlpha2OrNA))
	if !reCountryAlpha2.MatchString(cc) && !isCompositeGeo(cc) {
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
// Canonical format: /market/{geo}/{marketSlug}
// - geo: ISO3166-1 alpha2 lowercase (2 chars), composite ISO2, regional token, or "global"/"int"
// - marketSlug: lowercase slug derived from market label
//
// NOTE:
// - We intentionally default geo to "global" if missing/invalid to keep ingestion non-blocking.
// - Legacy market paths (/market/{slug} or /market/{domain}/{slug}) may still exist; prefer this for NEW markets.
func MarketCanonicalPath(geo string, marketLabel string) string {
	g := strings.ToLower(strings.TrimSpace(geo))
	if g == "" {
		g = "global"
	}
	if g != "global" && g != "int" && !reCountryAlpha2.MatchString(g) && !IsRegionalGeoToken(g) && !isCompositeGeo(g) {
		g = "global"
	}
	mSlug := Slugify(marketLabel)
	if mSlug == "" {
		return ""
	}
	return JoinPath("market", g, mSlug)
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
		return PersonCanonicalPath(canonicalName)
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
		return MarketCanonicalPath("global", canonicalName)
	case "Location":
		// We cannot canonicalize a location without at least ISO3166-1 country code.
		// Location canonicalization should be done via GeoCanonicalPath(country, admin1, city).
		return ""
	default:
		// Avoid generating non-canonical namespaces.
		return ""
	}
}

// PersonCanonicalPath returns a Horizon-canonical person path:
// /person/{honorific?}-{name-slug}
//
// Note: NO geo token in canonical path for privacy/GDPR compliance.
// Geographic data should be stored in identifiers/metadata, not in the path.
//
// Honorific/title is extracted from personName and normalized to canonical lowercase form (no dots).
// If no honorific detected, slug is created without honorific prefix.
func PersonCanonicalPath(personName string) string {
	if personName == "" {
		return ""
	}

	// Extract honorific from name
	nameWithoutHonorific, honorificCanonical, _ := extractHonorificFromName(personName)

	// Create slug from name (without honorific)
	var nameSlug string
	if nameWithoutHonorific != "" {
		nameSlug = Slugify(nameWithoutHonorific)
	} else {
		nameSlug = Slugify(personName)
	}

	if nameSlug == "" {
		return ""
	}

	// Prepend honorific if found
	if honorificCanonical != "" {
		nameSlug = honorificCanonical + "-" + nameSlug
	}

	return JoinPath("person", nameSlug)
}

// LocationCanonicalPath is legacy and intentionally conservative.
// Horizon canonical geo taxonomy is /loc/{country}/{admin1}/{city}; ISO6709/name-only inputs are insufficient.
// Prefer using GeoCanonicalPath.
func LocationCanonicalPath(name string, iso6709 string) string {
	_ = name
	_ = iso6709
	return ""
}

// =============================================================================
// Legal Form Normalization (for Organizations)
// =============================================================================

// normalizeLegalForm normalizes national legal forms to canonical English equivalents (lowercase).
// This prevents duplicates when the same entity appears in different languages (e.g., ТОО "Ромашка" = Romashka LLC).
//
// Returns: canonical form (lowercase), original form (for storage in identifiers)
// If no legal form detected, returns empty strings.
func normalizeLegalForm(legalForm string) (canonical string, original string) {
	if legalForm == "" {
		return "", ""
	}

	// Normalize input: trim, lowercase for matching
	normalized := strings.ToLower(strings.TrimSpace(legalForm))
	normalized = strings.Trim(normalized, ".,;:!?()[]{}")

	// Map of national legal forms to canonical equivalents
	// Key: normalized national form (various languages/scripts)
	// Value: canonical English form (lowercase)
	legalFormMap := map[string]string{
		// LLC equivalents
		"llc": "llc", "limited liability company": "llc",
		"тоо": "llc", "товарищество с ограниченной ответственностью": "llc",
		"ооо": "llc", "общество с ограниченной ответственностью": "llc",
		"gmbh": "llc", "gesellschaft mit beschränkter haftung": "llc",
		"sarl": "llc", "société à responsabilité limitée": "llc",
		"srl": "llc", "società a responsabilità limitata": "llc",
		"sdn bhd": "llc", "sendirian berhad": "llc",
		"sp. z o.o.": "llc", "spółka z ograniczoną odpowiedzialnością": "llc",
		"s.r.o.": "llc", "společnost s ručením omezeným": "llc",
		"kft.": "llc", "korlátolt felelősségű társaság": "llc",
		"youxian gongsi": "llc", "有限公司": "llc",

		// JSC/Joint Stock equivalents
		"jsc": "jsc", "joint stock company": "jsc",
		"ао": "jsc", "акционерное общество": "jsc",
		"pjsc": "pjsc", "public joint stock company": "pjsc", "public joint-stock company": "pjsc",
		"пао": "pjsc", "публичное акционерное общество": "pjsc",
		"cjsc": "cjsc", "closed joint stock company": "cjsc", "closed joint-stock company": "cjsc",
		"зао": "cjsc", "закрытое акционерное общество": "cjsc", // legacy, but keep for backward compat
		"ojsc": "jsc", "open joint stock company": "jsc", "open joint-stock company": "jsc",
		"ag": "jsc", "aktiengesellschaft": "jsc",
		"sa": "jsc", "société anonyme": "jsc", "sociedad anónima": "jsc",
		"spa": "jsc", "società per azioni": "jsc",
		"as": "jsc", "aksjeselskap": "jsc", "anonim şirket": "jsc",
		"ab": "jsc", "aktiebolag": "jsc",
		"kk": "jsc", "kabushiki kaisha": "jsc", "株式会社": "jsc",

		// LTD/Limited equivalents
		"ltd": "ltd", "limited": "ltd",
		"bhd": "ltd", "berhad": "ltd",
		"plc": "plc", "public limited company": "plc",

		// Corporation equivalents
		"inc": "inc", "incorporated": "inc",
		"corp": "corp", "corporation": "corp",

		// Partnership equivalents
		"llp": "llp", "limited liability partnership": "llp",
		"lp": "lp", "limited partnership": "lp",

		// Sole proprietorship
		"ип": "sp", "индивидуальный предприниматель": "sp",
		"fop": "sp", "фізична особа-підприємець": "sp",

		// State/Government enterprises
		"гуп": "sup", "государственное унитарное предприятие": "sup",
		"фгуп": "fsup", "федеральное государственное унитарное предприятие": "fsup",
		"soe": "soe", "state-owned enterprise": "soe",
	}

	// Direct match
	if canonical, ok := legalFormMap[normalized]; ok {
		return canonical, legalForm
	}

	// Try removing common suffixes/prefixes and matching again
	// Remove common punctuation and try again
	cleaned := strings.ReplaceAll(normalized, ".", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	if canonical, ok := legalFormMap[cleaned]; ok {
		return canonical, legalForm
	}

	// No match found - return empty (legal form unknown or not recognized)
	return "", ""
}

// reOrgParenthetical matches parenthetical annotations LLMs often inject into org names.
// Examples: "(formerly stc pay)", "(ex-Aramex)", "(now STC Bank)", "(fka STC Pay)", "(dba ...)"
var reOrgParenthetical = regexp.MustCompile(`(?i)\s*\((?:formerly|previously|ex-?|now|fka|dba|aka|trading as|prev\.?|f/k/a|d/b/a|a/k/a)\s[^)]*\)`)

// stripOrgParenthetical removes LLM-injected parenthetical annotations from org names.
// e.g., "STC Bank (formerly stc pay) PJSC" → "STC Bank PJSC"
func stripOrgParenthetical(name string) string {
	cleaned := reOrgParenthetical.ReplaceAllString(name, "")
	return strings.TrimSpace(cleaned)
}

// extractLegalFormFromName attempts to extract legal form from organization name.
// Returns: (nameWithoutForm, legalFormCanonical, legalFormOriginal)
// If no legal form detected, returns (originalName, "", "")
func extractLegalFormFromName(orgName string) (nameWithoutForm string, legalFormCanonical string, legalFormOriginal string) {
	if orgName == "" {
		return orgName, "", ""
	}

	// Common patterns: "Company Name LLC", "Company Name, Inc.", "ТОО \"Ромашка\"", "GmbH Company Name"
	// Try to find legal form at the end or beginning

	orgName = strings.TrimSpace(orgName)

	// Strip parenthetical annotations before legal form extraction.
	// e.g., "STC Bank (formerly stc pay) PJSC" → "STC Bank PJSC"
	// This prevents "(formerly ...)" from polluting the slug.
	cleaned := stripOrgParenthetical(orgName)
	if cleaned != "" {
		orgName = cleaned
	}

	// Try end of name first (most common pattern)
	words := strings.Fields(orgName)
	if len(words) > 0 {
		lastWord := strings.Trim(words[len(words)-1], ".,;:!?()[]{}")
		if canonical, original := normalizeLegalForm(lastWord); canonical != "" {
			// Found legal form at the end
			nameWithoutForm = strings.TrimSpace(strings.TrimSuffix(orgName, lastWord))
			nameWithoutForm = strings.Trim(nameWithoutForm, ".,;:!?()[]{}")
			return nameWithoutForm, canonical, original
		}
	}

	// Try beginning (less common, e.g., "GmbH Company Name")
	if len(words) > 0 {
		firstWord := strings.Trim(words[0], ".,;:!?()[]{}")
		if canonical, original := normalizeLegalForm(firstWord); canonical != "" {
			// Found legal form at the beginning
			nameWithoutForm = strings.TrimSpace(strings.TrimPrefix(orgName, firstWord))
			nameWithoutForm = strings.Trim(nameWithoutForm, ".,;:!?()[]{}")
			return nameWithoutForm, canonical, original
		}
	}

	// No legal form found
	return orgName, "", ""
}

// =============================================================================
// Honorific/Title Normalization (for Persons)
// =============================================================================

// normalizeHonorific normalizes honorifics/titles to canonical lowercase form (no dots).
// Returns: canonical form (lowercase), original form
// If no honorific detected, returns empty strings.
func normalizeHonorific(honorific string) (canonical string, original string) {
	if honorific == "" {
		return "", ""
	}

	normalized := strings.ToLower(strings.TrimSpace(honorific))
	normalized = strings.Trim(normalized, ".,;:!?()[]{}")

	honorificMap := map[string]string{
		// Royal titles
		"hrh": "hrh", "his royal highness": "hrh", "her royal highness": "hrh",
		"hm": "hm", "his majesty": "hm", "her majesty": "hm",
		"king": "king", "queen": "queen",
		"prince": "prince", "princess": "princess",
		"emir": "emir", "sultan": "sultan",

		// Diplomatic titles
		"he": "he", "his excellency": "he", "her excellency": "he",

		// Religious titles
		"pope":      "pope",
		"ayatollah": "ayatollah", "grand ayatollah": "grand-ayatollah",
		"imam":       "imam",
		"rabbi":      "rabbi",
		"cardinal":   "cardinal",
		"archbishop": "archbishop",

		// Gulf/Arab honorifics
		"sheikh": "sheikh", "shaikh": "sheikh", "shaykh": "sheikh",

		// Military ranks
		"gen": "gen", "general": "gen",
		"adm": "adm", "admiral": "adm",
		"col": "col", "colonel": "col",

		// Academic/Professional titles
		"dr": "dr", "doctor": "dr", "dr.": "dr",
		"prof": "prof", "professor": "prof", "prof.": "prof",

		// British titles
		"sir": "sir", "dame": "dame",
		"lord": "lord", "lady": "lady",
		"baron": "baron", "baroness": "baroness",
		"count": "count", "countess": "countess",
		"duke": "duke", "duchess": "duchess",
	}

	if canonical, ok := honorificMap[normalized]; ok {
		return canonical, honorific
	}

	// Try without dots
	cleaned := strings.ReplaceAll(normalized, ".", "")
	if canonical, ok := honorificMap[cleaned]; ok {
		return canonical, honorific
	}

	return "", ""
}

// extractHonorificFromName attempts to extract honorific from person name.
// Returns: (nameWithoutHonorific, honorificCanonical, honorificOriginal)
// If no honorific detected, returns (originalName, "", "")
func extractHonorificFromName(personName string) (nameWithoutHonorific string, honorificCanonical string, honorificOriginal string) {
	if personName == "" {
		return personName, "", ""
	}

	personName = strings.TrimSpace(personName)
	words := strings.Fields(personName)

	if len(words) == 0 {
		return personName, "", ""
	}

	// Try first word (most common pattern: "Sheikh Tahnoon", "HRH Mohammed")
	firstWord := strings.Trim(words[0], ".,;:!?()[]{}")
	if canonical, original := normalizeHonorific(firstWord); canonical != "" {
		nameWithoutHonorific = strings.TrimSpace(strings.TrimPrefix(personName, firstWord))
		nameWithoutHonorific = strings.Trim(nameWithoutHonorific, ".,;:!?()[]{}")
		return nameWithoutHonorific, canonical, original
	}

	// No honorific found
	return personName, "", ""
}
