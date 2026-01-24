package taxonomy

import "strings"

// NormalizeMarketTags normalizes market tags into canonical, expanded, singular slugs.
// Rules (best-effort):
// - lowercase
// - expand common abbreviations (e.g., "rwa" -> "real-world-asset")
// - convert to slug form (hyphens)
// - singularize common plurals (e.g., "batteries" -> "battery")
// - dedupe, preserve order
// - cap to 8 tags
func NormalizeMarketTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" {
			continue
		}
		if expanded, ok := marketTagExpansions[t]; ok && strings.TrimSpace(expanded) != "" {
			t = expanded
		}
		t = Slugify(t)
		if t == "" {
			continue
		}
		if singular, ok := marketTagSingulars[t]; ok && strings.TrimSpace(singular) != "" {
			t = singular
		}
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) >= 8 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MarketTagSearchTokens returns a dedup-search token list suitable for Firestore array-contains-any.
// It includes normalized tags plus a small set of abbreviation aliases (reverse mapping).
// The result is deduped and capped to maxTokens (Firestore limit is 10).
func MarketTagSearchTokens(tags []string, maxTokens int) []string {
	normalized := NormalizeMarketTags(tags)
	if len(normalized) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(normalized))
	for _, t := range normalized {
		if t == "" {
			continue
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
			if maxTokens > 0 && len(out) >= maxTokens {
				return out
			}
		}
		if alias, ok := marketTagAbbrevAliases[t]; ok && alias != "" {
			if _, ok := seen[alias]; !ok {
				seen[alias] = struct{}{}
				out = append(out, alias)
				if maxTokens > 0 && len(out) >= maxTokens {
					return out
				}
			}
		}
	}
	return out
}

var marketTagExpansions = map[string]string{
	// Finance/crypto
	"rwa":    "real-world-asset",
	"defi":   "decentralized-finance",
	"nft":    "non-fungible-token",
	"crypto": "digital-asset",

	// Technology
	"ai":   "artificial-intelligence",
	"ml":   "machine-learning",
	"iot":  "internet-of-things",
	"saas": "software-as-a-service",
	"paas": "platform-as-a-service",
	"iaas": "infrastructure-as-a-service",

	// Energy / materials
	"lng": "liquefied-natural-gas",
	"lpg": "liquefied-petroleum-gas",

	// Common market shorthands
	"ev": "electric-vehicle",
	"esg": "environmental-social-governance",
}

// Reverse aliases to improve dedup search hits for legacy abbreviations in tags.
var marketTagAbbrevAliases = map[string]string{
	"real-world-asset":               "rwa",
	"decentralized-finance":          "defi",
	"non-fungible-token":             "nft",
	"artificial-intelligence":        "ai",
	"machine-learning":               "ml",
	"internet-of-things":             "iot",
	"software-as-a-service":          "saas",
	"platform-as-a-service":          "paas",
	"infrastructure-as-a-service":    "iaas",
	"liquefied-natural-gas":           "lng",
	"liquefied-petroleum-gas":         "lpg",
	"electric-vehicle":                "ev",
	"environmental-social-governance": "esg",
}

var marketTagSingulars = map[string]string{
	"semiconductors": "semiconductor",
	"batteries":      "battery",
	"tokens":         "token",
	"assets":         "asset",
	"securities":     "security",
	"vehicles":       "vehicle",
	"materials":      "material",
	"services":       "service",
	"markets":        "market",
}
