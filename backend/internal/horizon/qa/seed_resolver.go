package qa

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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

// SeedResolutionResult holds resolved IDs and any hints that failed to match.
type SeedResolutionResult struct {
	ResolvedIDs []string
	Failures    []horizonmodels.SeedFailure
}

// ResolveWithFailures resolves seed hints and reports failures for unmatched hints.
// For each hint that doesn't match exactly, fuzzy matching (case-insensitive substring
// on the last path segment) is attempted before recording a failure.
func (r *SeedResolver) ResolveWithFailures(seedHints []string, scopeFragments map[string]string) SeedResolutionResult {
	var result SeedResolutionResult
	if len(seedHints) == 0 || len(scopeFragments) == 0 {
		for _, h := range seedHints {
			h = strings.TrimSpace(h)
			if h != "" {
				result.Failures = append(result.Failures, horizonmodels.SeedFailure{
					Hint:   h,
					Reason: "empty scope — no fragments available",
				})
			}
		}
		return result
	}

	seen := make(map[string]bool)

	for _, hint := range seedHints {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}

		matched := false
		for canonicalPath, fragmentID := range scopeFragments {
			canonicalPath = strings.TrimSpace(canonicalPath)
			fragmentID = strings.TrimSpace(fragmentID)
			if canonicalPath == "" || fragmentID == "" {
				continue
			}
			if r.matches(hint, canonicalPath) {
				if !seen[fragmentID] {
					seen[fragmentID] = true
					result.ResolvedIDs = append(result.ResolvedIDs, fragmentID)
				}
				matched = true
			}
		}
		if matched {
			continue
		}

		// Fuzzy: case-insensitive substring on the last path segment
		fuzzyMatched := false
		for canonicalPath, fragmentID := range scopeFragments {
			canonicalPath = strings.TrimSpace(canonicalPath)
			fragmentID = strings.TrimSpace(fragmentID)
			if canonicalPath == "" || fragmentID == "" {
				continue
			}
			if r.fuzzyMatch(hint, canonicalPath) {
				if !seen[fragmentID] {
					seen[fragmentID] = true
					result.ResolvedIDs = append(result.ResolvedIDs, fragmentID)
				}
				fuzzyMatched = true
			}
		}
		if fuzzyMatched {
			continue
		}

		result.Failures = append(result.Failures, horizonmodels.SeedFailure{
			Hint:   hint,
			Reason: fmt.Sprintf("no exact or fuzzy match in %d scope fragments", len(scopeFragments)),
		})
	}

	return result
}

func (r *SeedResolver) matches(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return prefix == path || strings.HasPrefix(path, prefix+"/")
	}
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern)
	}
	if strings.HasPrefix(path, pattern+"/") {
		return true
	}
	return false
}

// fuzzyMatch checks if the hint matches the last path segment of canonicalPath
// using case-insensitive substring comparison.
func (r *SeedResolver) fuzzyMatch(hint, canonicalPath string) bool {
	lastSeg := canonicalPath
	if idx := strings.LastIndex(canonicalPath, "/"); idx >= 0 && idx < len(canonicalPath)-1 {
		lastSeg = canonicalPath[idx+1:]
	}
	hintLower := strings.ToLower(strings.TrimSpace(hint))
	// Strip any leading path structure from the hint for comparison
	hintSeg := hintLower
	if idx := strings.LastIndex(hintLower, "/"); idx >= 0 && idx < len(hintLower)-1 {
		hintSeg = hintLower[idx+1:]
	}
	hintSeg = strings.TrimSuffix(hintSeg, "/*")
	hintSeg = strings.TrimSuffix(hintSeg, "/")
	if hintSeg == "" {
		return false
	}
	return strings.Contains(strings.ToLower(lastSeg), hintSeg)
}
