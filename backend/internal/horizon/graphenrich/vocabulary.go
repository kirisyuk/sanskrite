package graphenrich

import (
	"fmt"
	"sort"
	"strings"
)

// SupportedMarketDomains returns the canonical coarse domains used by graph enrichment.
// These are the domains prompts and downstream propagation should prefer.
func SupportedMarketDomains() []string {
	seen := map[string]struct{}{}
	for _, profile := range domainProfiles {
		if v := strings.TrimSpace(profile.Domain); v != "" {
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// SupportedVerticalsByDomain returns graphenrich-known verticals grouped by canonical domain.
func SupportedVerticalsByDomain() map[string][]string {
	tmp := map[string]map[string]struct{}{}
	for _, profile := range domainProfiles {
		domain := strings.TrimSpace(profile.Domain)
		vertical := strings.TrimSpace(profile.Vertical)
		if domain == "" || vertical == "" {
			continue
		}
		if tmp[domain] == nil {
			tmp[domain] = map[string]struct{}{}
		}
		tmp[domain][vertical] = struct{}{}
	}
	out := make(map[string][]string, len(tmp))
	for domain, set := range tmp {
		items := make([]string, 0, len(set))
		for vertical := range set {
			items = append(items, vertical)
		}
		sort.Strings(items)
		out[domain] = items
	}
	return out
}

func SupportedMarketDomainsForPrompt() string {
	return strings.Join(SupportedMarketDomains(), "|")
}

func DomainVerticalHintsForPrompt() string {
	byDomain := SupportedVerticalsByDomain()
	domains := SupportedMarketDomains()
	parts := make([]string, 0, len(domains))
	for _, domain := range domains {
		verticals := byDomain[domain]
		if len(verticals) == 0 {
			continue
		}
		if len(verticals) > 5 {
			verticals = verticals[:5]
		}
		parts = append(parts, fmt.Sprintf("%s=%s", domain, strings.Join(verticals, ",")))
	}
	return strings.Join(parts, "; ")
}
