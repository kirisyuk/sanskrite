package taxonomy

import (
	"regexp"
	"strings"
)

var reISO2OrGlobal = regexp.MustCompile(`^(global|[a-z]{2})$`)

// SegmentCanonicalPath returns a canonical market segment path:
// /segment/{domain}/{vertical}/{geo}
//
// - domain/vertical are slugified (lowercase a-z0-9-)
// - geo must be ISO3166-1 alpha2 lowercase (2 chars) or "global"
//
// Returns ("", false) if the path cannot be normalized safely.
func SegmentCanonicalPath(domain, vertical, geo string) (string, bool) {
	d := Slugify(domain)
	v := Slugify(vertical)
	g := strings.ToLower(strings.TrimSpace(geo))
	if g == "" {
		g = "global"
	}
	if !reISO2OrGlobal.MatchString(g) {
		return "", false
	}
	if d == "" || v == "" {
		return "", false
	}
	return "/segment/" + d + "/" + v + "/" + g, true
}

// NormalizeSegmentPath takes an arbitrary input and tries to normalize it into:
// /segment/{domain}/{vertical}/{geo}
//
// Accepted input shapes (best-effort):
// - "/segment/domain/vertical/geo"
// - "segment/domain/vertical/geo"
// - "/segment/domain/vertical/retail/geo" (extra parts are merged into vertical: "vertical-retail")
// - "/segment/domain/vertical" (geo defaults to "global")
func NormalizeSegmentPath(in string) (string, bool) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", false
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) == 0 {
		return "", false
	}
	if parts[0] != "segment" {
		// Allow users/LLMs to omit the namespace: "finance/banking/kz" -> "/segment/finance/banking/kz"
		parts = append([]string{"segment"}, parts...)
	}
	// Ensure we have at least: segment + domain + vertical
	if len(parts) < 3 {
		return "", false
	}

	// Geo is the last segment if valid; otherwise default to global.
	geo := "global"
	if len(parts) >= 4 {
		last := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		if reISO2OrGlobal.MatchString(last) {
			geo = last
			parts = parts[:len(parts)-1]
		} else {
			// No valid geo at the end → default global, keep full parts for vertical merge.
		}
	}

	// Now parts are: ["segment", domain, ...verticalParts]
	domain := parts[1]
	verticalParts := parts[2:]
	vertical := strings.Join(verticalParts, "-")
	return SegmentCanonicalPath(domain, vertical, geo)
}

// NormalizeSegmentPaths normalizes a list of segment paths, deduplicating and dropping invalid entries.
func NormalizeSegmentPaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))

	var add func(s string)
	add = func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// Allow comma-separated convenience.
		if strings.Contains(s, ",") {
			for _, p := range strings.Split(s, ",") {
				add(p)
			}
			return
		}
		n, ok := NormalizeSegmentPath(s)
		if !ok || n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}

	for _, s := range in {
		add(s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeOrgSize returns a canonical org size bucket.
// Canonical values: "sme" | "le" | "enterprise" | "mega" | "na"
//
// Unknown/empty values are mapped to "na" (unless input is empty, then returns "").
func NormalizeOrgSize(in string) string {
	raw := strings.TrimSpace(strings.ToLower(in))
	if raw == "" {
		return ""
	}
	switch raw {
	case "sme", "small", "small-medium", "small_medium", "sm", "mid", "midsize", "mid-size", "medium":
		return "sme"
	case "le", "large", "large-enterprise", "large_enterprise":
		return "le"
	case "enterprise", "ent":
		return "enterprise"
	case "mega", "very-large", "very_large", "bigtech", "conglomerate":
		return "mega"
	case "na", "n/a", "unknown", "unspecified":
		return "na"
	default:
		// Common patterns: "SME/LE", "SME / LE"
		if strings.Contains(raw, "sme") {
			return "sme"
		}
		if strings.Contains(raw, "le") || strings.Contains(raw, "large") {
			return "le"
		}
		return "na"
	}
}


