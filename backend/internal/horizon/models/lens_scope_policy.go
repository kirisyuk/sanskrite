package models

import "strings"

const (
	// Scope entry prefixes.
	QueryScopePrefix    = "/query/"
	DomainScopePrefix   = "/domain/"
	ArtifactScopePrefix = "/artifact/"

	// Legacy counter key for fragment-backed automatic scope adds.
	// Kept for backwards compatibility with existing lens documents.
	ScopeAutoAddEntityCounterKey = "deferredScopeAddCount"

	// Separate budget for artifact refs so they do not consume the same quota
	// as discovered entities or deferred anchors.
	ScopeAutoAddArtifactCounterKey = "artifactScopeAddCount"
)

// MaxAutoScopeAddsPerLens caps automatic fragment-backed scope extensions per lens.
// Manual user actions (e.g. pin/scope edit) are not governed by this counter.
const MaxAutoScopeAddsPerLens = 7

// MaxAutoArtifactScopeAddsPerLens caps automatic artifact scope extensions per lens.
const MaxAutoArtifactScopeAddsPerLens = 7

func IsQueryScopeEntry(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), QueryScopePrefix)
}

func IsDomainScopeEntry(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), DomainScopePrefix)
}

func IsArtifactScopeEntry(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), ArtifactScopePrefix)
}

// IsNonFragmentScopeEntry returns true for scope entries that are valid lens members
// but are not backed by HorizonFragment documents.
func IsNonFragmentScopeEntry(path string) bool {
	return IsQueryScopeEntry(path) || IsDomainScopeEntry(path) || IsArtifactScopeEntry(path)
}

// ScopeAutoAddCounterKey returns the auto-add budget bucket for a scope entry.
// Empty key means the entry is always allowed and does not consume an auto-add budget.
func ScopeAutoAddCounterKey(path string) string {
	switch {
	case IsDomainScopeEntry(path):
		return ""
	case IsArtifactScopeEntry(path):
		return ScopeAutoAddArtifactCounterKey
	default:
		return ScopeAutoAddEntityCounterKey
	}
}
