// Package scout provides session-level graph caching for ScoutEngine.
package scout

import (
	"context"
	"strings"
	"sync"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// SessionGraph provides an in-memory cache for fragments discovered/created during a single job session.
// This solves:
// 1) Vertex Search indexing delay: newly created fragments aren't immediately searchable.
// 2) Duplicate creation: multiple relations targeting the same entity within one job.
// 3) Path update tracking: when canonical path is refined, we track the old→new mapping.
//
// Thread-safe: all methods are protected by a mutex for concurrent access.
type SessionGraph struct {
	mu sync.RWMutex

	// byPath maps canonical path → fragment (latest version)
	byPath map[string]*models.HorizonFragment

	// byName maps normalized name → list of fragments (for name-based lookup with jurisdiction filtering)
	byName map[string][]*models.HorizonFragment

	// pathAliases maps old canonical path → new canonical path (when path is updated)
	pathAliases map[string]string

	// byID maps fragment ID → fragment (for quick ID lookups)
	byID map[string]*models.HorizonFragment

	// skeletonCache caches buildGraphSkeletonContext results per canonical path.
	// Avoids redundant DB-heavy skeleton rebuilds within the same job session.
	skeletonCache map[string]string
}

// NewSessionGraph creates a new empty session graph.
func NewSessionGraph() *SessionGraph {
	return &SessionGraph{
		byPath:        make(map[string]*models.HorizonFragment),
		byName:        make(map[string][]*models.HorizonFragment),
		pathAliases:   make(map[string]string),
		byID:          make(map[string]*models.HorizonFragment),
		skeletonCache: make(map[string]string),
	}
}

// Add adds or updates a fragment in the session graph.
// If the fragment's canonical path changed, it updates the alias mapping.
func (sg *SessionGraph) Add(frag *models.HorizonFragment) {
	if frag == nil || frag.CanonicalPath == "" {
		return
	}
	sg.mu.Lock()
	defer sg.mu.Unlock()

	cp := strings.TrimSpace(frag.CanonicalPath)

	// Check if we already have this fragment by ID with a different path
	if frag.ID != "" {
		if old, ok := sg.byID[frag.ID]; ok && old.CanonicalPath != cp {
			// Remove old name index entries before updating
			sg.removeFragmentFromNameIndex(old)
			// Path changed: record the alias
			oldPath := strings.TrimSpace(old.CanonicalPath)
			if oldPath != "" && oldPath != cp {
				sg.pathAliases[oldPath] = cp
				// Remove old path mapping
				delete(sg.byPath, oldPath)
			}
		} else if old, ok := sg.byID[frag.ID]; ok {
			// Same path, but update name index in case labels/identifiers changed
			sg.removeFragmentFromNameIndex(old)
		}
	}

	// Update path mapping
	sg.byPath[cp] = frag

	// Update ID mapping
	if frag.ID != "" {
		sg.byID[frag.ID] = frag
	}

	// Update name index
	for _, name := range sg.extractNames(frag) {
		normName := sg.normalizeName(name)
		if normName == "" {
			continue
		}
		// Check if already in list
		found := false
		for i, existing := range sg.byName[normName] {
			if existing.ID == frag.ID || existing.CanonicalPath == cp {
				// Update existing entry
				sg.byName[normName][i] = frag
				found = true
				break
			}
		}
		if !found {
			sg.byName[normName] = append(sg.byName[normName], frag)
		}
	}
}

// GetByPath returns a fragment by canonical path, following aliases if needed.
func (sg *SessionGraph) GetByPath(path string) *models.HorizonFragment {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	// Direct lookup
	if frag, ok := sg.byPath[path]; ok {
		return frag
	}

	// Check if this is an old path with an alias
	if newPath, ok := sg.pathAliases[path]; ok {
		return sg.byPath[newPath]
	}

	return nil
}

// GetByID returns a fragment by ID.
func (sg *SessionGraph) GetByID(id string) *models.HorizonFragment {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return sg.byID[id]
}

// GetByName returns fragments matching the name, optionally filtered by jurisdiction.
// If jurisdiction is empty, returns all matches.
// jurisdiction should be a 2-letter country code (e.g., "sa", "in", "us").
func (sg *SessionGraph) GetByName(name string, jurisdiction string) []*models.HorizonFragment {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	normName := sg.normalizeName(name)
	if normName == "" {
		return nil
	}

	fragments := sg.byName[normName]
	if len(fragments) == 0 {
		return nil
	}

	jurisdiction = strings.ToLower(strings.TrimSpace(jurisdiction))
	if jurisdiction == "" {
		// Return all matches
		result := make([]*models.HorizonFragment, len(fragments))
		copy(result, fragments)
		return result
	}

	// Filter by jurisdiction
	var filtered []*models.HorizonFragment
	for _, frag := range fragments {
		fragJurisdiction := extractJurisdiction(frag.CanonicalPath)
		if fragJurisdiction == jurisdiction || fragJurisdiction == "" || fragJurisdiction == "global" {
			filtered = append(filtered, frag)
		}
	}
	return filtered
}

// GetByNamePreferJurisdiction returns the best match for name, preferring the given jurisdiction.
// Priority:
// 1) Exact jurisdiction match
// 2) Global/empty jurisdiction
// 3) Any match (fallback)
func (sg *SessionGraph) GetByNamePreferJurisdiction(name string, preferredJurisdiction string) *models.HorizonFragment {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	normName := sg.normalizeName(name)
	if normName == "" {
		return nil
	}

	fragments := sg.byName[normName]
	if len(fragments) == 0 {
		return nil
	}

	preferredJurisdiction = strings.ToLower(strings.TrimSpace(preferredJurisdiction))

	var exactMatch *models.HorizonFragment
	var globalMatch *models.HorizonFragment
	var anyMatch *models.HorizonFragment

	for _, frag := range fragments {
		fragJurisdiction := extractJurisdiction(frag.CanonicalPath)

		if preferredJurisdiction != "" && fragJurisdiction == preferredJurisdiction {
			exactMatch = frag
			break // Best possible match
		}
		if fragJurisdiction == "" || fragJurisdiction == "global" {
			if globalMatch == nil {
				globalMatch = frag
			}
		}
		if anyMatch == nil {
			anyMatch = frag
		}
	}

	if exactMatch != nil {
		return exactMatch
	}
	if globalMatch != nil {
		return globalMatch
	}
	return anyMatch
}

// ResolvePath resolves a path, following any aliases to the current canonical path.
func (sg *SessionGraph) ResolvePath(path string) string {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	// Follow alias chain (up to 10 hops to prevent infinite loops)
	for i := 0; i < 10; i++ {
		if newPath, ok := sg.pathAliases[path]; ok {
			path = newPath
		} else {
			break
		}
	}
	return path
}

// RecordPathChange explicitly records a path alias (old → new).
// Useful when canonical path is updated outside of Add().
func (sg *SessionGraph) RecordPathChange(oldPath, newPath string) {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}

	sg.mu.Lock()
	defer sg.mu.Unlock()

	sg.pathAliases[oldPath] = newPath

	// Move fragment from old to new path if present
	if frag, ok := sg.byPath[oldPath]; ok {
		delete(sg.byPath, oldPath)
		frag.CanonicalPath = newPath
		sg.byPath[newPath] = frag
	}
}

// AllPaths returns all canonical paths currently in the session graph.
func (sg *SessionGraph) AllPaths() []string {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	paths := make([]string, 0, len(sg.byPath))
	for path := range sg.byPath {
		paths = append(paths, path)
	}
	return paths
}

// AllFragments returns all fragments currently in the session graph.
func (sg *SessionGraph) AllFragments() []*models.HorizonFragment {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	fragments := make([]*models.HorizonFragment, 0, len(sg.byPath))
	for _, frag := range sg.byPath {
		fragments = append(fragments, frag)
	}
	return fragments
}

// Size returns the number of fragments in the session graph.
func (sg *SessionGraph) Size() int {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return len(sg.byPath)
}

// Clear removes all entries from the session graph.
func (sg *SessionGraph) Clear() {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	sg.byPath = make(map[string]*models.HorizonFragment)
	sg.byName = make(map[string][]*models.HorizonFragment)
	sg.pathAliases = make(map[string]string)
	sg.byID = make(map[string]*models.HorizonFragment)
	sg.skeletonCache = make(map[string]string)
}

// extractNames returns searchable names from a fragment.
func (sg *SessionGraph) extractNames(frag *models.HorizonFragment) []string {
	if frag == nil {
		return nil
	}
	var names []string

	// Labels
	if frag.Labels != nil {
		if en := strings.TrimSpace(frag.Labels["en"]); en != "" {
			names = append(names, en)
		}
	}

	// Identifiers
	if frag.Identifiers != nil {
		if short := strings.TrimSpace(frag.Identifiers["shortName"]); short != "" {
			names = append(names, short)
		}
		if legal := strings.TrimSpace(frag.Identifiers["legalName"]); legal != "" {
			names = append(names, legal)
		}
	}

	return names
}

// removeFragmentFromNameIndex removes a fragment from the name index.
// Assumes sg.mu is already held by the caller.
func (sg *SessionGraph) removeFragmentFromNameIndex(frag *models.HorizonFragment) {
	if frag == nil {
		return
	}
	for _, name := range sg.extractNames(frag) {
		normName := sg.normalizeName(name)
		if normName == "" {
			continue
		}
		list := sg.byName[normName]
		if len(list) == 0 {
			continue
		}
		newList := list[:0]
		for _, existing := range list {
			if existing == nil {
				continue
			}
			if frag.ID != "" && existing.ID == frag.ID {
				continue
			}
			if existing.CanonicalPath == frag.CanonicalPath {
				continue
			}
			newList = append(newList, existing)
		}
		if len(newList) == 0 {
			delete(sg.byName, normName)
		} else {
			sg.byName[normName] = newList
		}
	}
}

// normalizeName normalizes a name for index lookup.
func (sg *SessionGraph) normalizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	// Remove common suffixes that cause false mismatches
	for _, suffix := range []string{" inc", " inc.", " corp", " corp.", " ltd", " ltd.", " llc", " plc", " co", " co."} {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			name = strings.TrimSpace(name)
		}
	}
	return name
}

// extractJurisdiction extracts the jurisdiction (country code) from a canonical path.
// Examples:
// - /org/sa/aramco → sa
// - /org/global/tesla → global
// - /gov/us/treasury → us
// - /loc/in/delhi/na → in
// Returns empty string if jurisdiction cannot be determined.
func extractJurisdiction(canonicalPath string) string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return ""
	}

	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) < 2 {
		return ""
	}

	prefix := strings.ToLower(parts[0])
	switch prefix {
	case "org", "gov", "fac", "loc":
		// Second part is jurisdiction
		return strings.ToLower(parts[1])
	case "decision", "mechanism":
		// /decision/{domain-slug}/{decision-slug} or /mechanism/{category}/{slug}
		// These are global by nature
		return "global"
	case "domain", "market", "product", "industry", "segment":
		// Domain-level paths are global
		return "global"
	default:
		return ""
	}
}

// GetSkeletonContext returns the cached skeleton context for a canonical path.
func (sg *SessionGraph) GetSkeletonContext(canonicalPath string) (string, bool) {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	v, ok := sg.skeletonCache[canonicalPath]
	return v, ok
}

// SetSkeletonContext stores a skeleton context string for the given canonical path.
func (sg *SessionGraph) SetSkeletonContext(canonicalPath, context string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.skeletonCache[canonicalPath] = context
}

// --- Context key for SessionGraph ---

type sessionGraphKey struct{}

// WithSessionGraph attaches a SessionGraph to a context.
func WithSessionGraph(ctx context.Context, sg *SessionGraph) context.Context {
	return context.WithValue(ctx, sessionGraphKey{}, sg)
}

// SessionGraphFromContext retrieves the SessionGraph from context, or nil if not present.
func SessionGraphFromContext(ctx context.Context) *SessionGraph {
	if sg, ok := ctx.Value(sessionGraphKey{}).(*SessionGraph); ok {
		return sg
	}
	return nil
}
