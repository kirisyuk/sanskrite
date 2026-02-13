package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// parseMarketDedupScope extracts a strict MARKET dedup scope:
// - geo
// - normalized domain
// - normalized vertical
//
// Only segment-first market paths are accepted (/market/{geo}/{domain}/{vertical}).
// Legacy market paths are intentionally ignored to avoid broad false-positive merges.
func parseMarketDedupScope(canonicalPath string) (string, string, string) {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/market/") {
		return "", "", ""
	}

	geo := ""
	domain := ""
	vertical := ""

	if parts, err := taxonomy.ParseCanonicalPathParts(cp); err == nil && parts != nil && parts.Market != nil && !parts.Market.Legacy {
		geo = strings.ToLower(strings.TrimSpace(parts.Market.Geo))
		domain = taxonomy.NormalizeMarketDomain(parts.Market.Domain)
		vertical = strings.ToLower(strings.TrimSpace(parts.Market.Vertical))
	} else {
		raw := strings.Split(strings.Trim(cp, "/"), "/")
		if len(raw) >= 4 && raw[0] == "market" {
			geo = strings.ToLower(strings.TrimSpace(raw[1]))
			domain = taxonomy.NormalizeMarketDomain(raw[2])
			vertical = strings.ToLower(strings.TrimSpace(raw[3]))
		}
	}

	if geo == "" || domain == "" || vertical == "" {
		return "", "", ""
	}

	// Normalize through segment dictionary rules (aliases/synonyms) to compare in stable space.
	if seg, ok := taxonomy.SegmentCanonicalPath(domain, vertical, geo); ok {
		if normDomain, normVertical, ok := taxonomy.NormalizeSegmentToNormPair(seg); ok {
			if strings.TrimSpace(normDomain) != "" {
				domain = strings.ToLower(strings.TrimSpace(normDomain))
			}
			if strings.TrimSpace(normVertical) != "" {
				vertical = strings.ToLower(strings.TrimSpace(normVertical))
			}
		}
	}

	domain = taxonomy.NormalizeMarketDomain(domain)
	if geo == "" || domain == "" || vertical == "" {
		return "", "", ""
	}
	return geo, domain, vertical
}
