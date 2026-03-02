package qa

import (
	"strings"
)

// SeedResolver resolves SeedHints (canonical path patterns) to fragment IDs.
type SeedResolver struct{}

// NewSeedResolver creates a new SeedResolver.
func NewSeedResolver() *SeedResolver {
	return &SeedResolver{}
}

// Resolve returns fragment IDs that match any of the SeedHints.
// scopeFragments maps canonicalPath -> fragmentID.
// SeedHints can be:
//   - Exact path: /org/id/pt-vale-indonesia
//   - Prefix with wildcard: /org/id/*
//   - Glob: /deposit/nickel/*
func (r *SeedResolver) Resolve(seedHints []string, scopeFragments map[string]string) []string {
	if len(seedHints) == 0 || len(scopeFragments) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for canonicalPath, fragmentID := range scopeFragments {
		canonicalPath = strings.TrimSpace(canonicalPath)
		fragmentID = strings.TrimSpace(fragmentID)
		if canonicalPath == "" || fragmentID == "" {
			continue
		}
		for _, hint := range seedHints {
			hint = strings.TrimSpace(hint)
			if hint == "" {
				continue
			}
			if r.matches(hint, canonicalPath) {
				if !seen[fragmentID] {
					seen[fragmentID] = true
					out = append(out, fragmentID)
				}
				break
			}
		}
	}
	return out
}

func (r *SeedResolver) matches(pattern, path string) bool {
	// Exact match
	if pattern == path {
		return true
	}
	// Prefix match: /org/id/* or /org/id/ matches /org/id/pt-vale
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return prefix == path || strings.HasPrefix(path, prefix+"/")
	}
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern)
	}
	// Prefix without wildcard: /org/id matches /org/id/...
	if strings.HasPrefix(path, pattern+"/") {
		return true
	}
	return false
}
