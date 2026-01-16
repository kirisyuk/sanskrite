package taxonomy

import (
	"strings"
)

// MarketPathFromSegmentPath projects a canonical /segment/... path into a canonical /market/... path (v2).
//
// Segment-first principle:
// - /segment/{domain}/{vertical}/{geo} is the identity of a market coordinate.
// - /market is a materialized view of segment coordinates for graph traversal, metrics, and hierarchy.
//
// Mapping rules (best-effort, deterministic):
// - Normalize the segment path to canonical form.
// - Apply the MarketSegmentDictionary normalization (aliases, channel domains) to produce a stable (normDomain, normVertical) pair.
// - Produce:
//   - /market/{geo}/{normDomain}/{normVertical} when normVertical != ""
//   - /market/{geo}/{normDomain} when normVertical == ""
//
// Returns ("", false) if the segment cannot be normalized safely.
func MarketPathFromSegmentPath(segmentPath string) (string, bool) {
	seg, ok := NormalizeSegmentPath(segmentPath)
	if !ok || seg == "" {
		return "", false
	}
	parts := strings.Split(strings.Trim(seg, "/"), "/")
	// segment/{domain}/{vertical}/{geo}
	if len(parts) != 4 || parts[0] != "segment" {
		return "", false
	}
	geo := strings.ToLower(strings.TrimSpace(parts[3]))
	normDomain, normVertical, ok := NormalizeSegmentToNormPair(seg)
	if !ok || strings.TrimSpace(normDomain) == "" {
		return "", false
	}
	if strings.TrimSpace(normVertical) != "" {
		return "/market/" + geo + "/" + normDomain + "/" + normVertical, true
	}
	return "/market/" + geo + "/" + normDomain, true
}

// SegmentPathFromMarketPath attempts to map a v2 market path (/market/{geo}/{domain}/{vertical}) back to a /segment path.
//
// NOTE:
// - This only supports the v2 shape with explicit domain/vertical.
// - Legacy freeform markets (/market/{geo}/{slug}) are intentionally NOT mapped here (ambiguous).
func SegmentPathFromMarketPath(marketPath string) (string, bool) {
	p := strings.TrimSpace(marketPath)
	if p == "" {
		return "", false
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "market" {
		return "", false
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	domain := strings.ToLower(strings.TrimSpace(parts[2]))
	vertical := strings.ToLower(strings.TrimSpace(parts[3]))
	if geo == "" || domain == "" || vertical == "" {
		return "", false
	}
	seg, ok := SegmentCanonicalPath(domain, vertical, geo)
	if !ok || seg == "" {
		return "", false
	}
	return seg, true
}

// NormalizeSegmentToNormPair normalizes a canonical /segment/... path into a stable (normDomain, normVertical) pair.
// It applies the runtime MarketSegmentDictionary rules (aliases, channel domains, explicit overrides) deterministically.
func NormalizeSegmentToNormPair(segmentPath string) (normDomain string, normVertical string, ok bool) {
	seg, ok2 := NormalizeSegmentPath(segmentPath)
	if !ok2 || seg == "" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(seg, "/"), "/")
	if len(parts) != 4 || parts[0] != "segment" {
		return "", "", false
	}
	domain := normalizeSegToken(parts[1])
	vertical := normalizeSegToken(parts[2])

	channelLike, domainAliases, verticalAliases, verticalFamily, explicit := runtimeSegmentRules()

	// Apply aliases (domain, then vertical).
	if v, ok := domainAliases[domain]; ok && strings.TrimSpace(v) != "" {
		domain = normalizeSegToken(v)
	}
	if v, ok := verticalAliases[vertical]; ok && strings.TrimSpace(v) != "" {
		vertical = normalizeSegToken(v)
	}

	// Apply vertical synonyms (dictionary-backed) before explicit rules.
	if d := GetMarketSegmentDictionary(); d != nil && len(d.VerticalSynonyms) > 0 {
		if v, ok := d.VerticalSynonyms[vertical]; ok && strings.TrimSpace(v) != "" {
			vertical = normalizeSegToken(v)
		}
	}

	// Explicit override key: /segment/{domain}/{vertical} (after aliases/synonyms).
	key := "/segment/" + domain + "/" + vertical
	if rule, ok := explicit[key]; ok && strings.TrimSpace(rule.NormDomain) != "" {
		nd := normalizeSegToken(rule.NormDomain)
		nv := normalizeSegToken(rule.NormVertical)
		if strings.TrimSpace(nd) == "" {
			return "", "", false
		}
		return nd, nv, true
	}

	channel := ""
	isChannel := false
	if _, ok := channelLike[domain]; ok {
		isChannel = true
		channel = domain
		_ = channel // keep parity with NormalizeMarketSegments logic
	}

	if isChannel {
		// Channel-like domain: vertical often carries the actual market signal.
		if fam, ok := verticalFamily[vertical]; ok && strings.TrimSpace(fam) != "" {
			return normalizeSegToken(fam), vertical, true
		}
		// Fallback: treat vertical as domain-like.
		return vertical, "", true
	}

	// Regular domain/vertical.
	return domain, vertical, true
}

// SegmentVerticalParent returns the best-effort parent vertical for a given vertical.
// Returns "" if unknown.
func SegmentVerticalParent(vertical string) string {
	v := normalizeSegToken(vertical)
	if v == "" {
		return ""
	}
	// Dictionary-backed hierarchy (preferred)
	if d := GetMarketSegmentDictionary(); d != nil && len(d.VerticalParents) > 0 {
		if p, ok := d.VerticalParents[v]; ok && strings.TrimSpace(p) != "" {
			return normalizeSegToken(p)
		}
	}
	// Heuristic fallback: for hyphenated verticals, assume suffix is broader.
	// Example: "event-marketing" -> "marketing".
	if idx := strings.Index(v, "-"); idx > 0 && idx < len(v)-1 {
		parent := strings.TrimSpace(v[idx+1:])
		if len(parent) >= 3 {
			return normalizeSegToken(parent)
		}
	}
	return ""
}
