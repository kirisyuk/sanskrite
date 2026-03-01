package lens

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// ArtifactScopePrefix is the canonical prefix for MaterializedArtifact scope entries.
const ArtifactScopePrefix = "/artifact/"

// IsArtifactScopeEntry returns true if the scope entry references a MaterializedArtifact.
func IsArtifactScopeEntry(entry string) bool {
	return strings.HasPrefix(strings.TrimSpace(entry), ArtifactScopePrefix)
}

// ArtifactKeyFromScopeEntry extracts the artifact canonical key from a scope entry.
func ArtifactKeyFromScopeEntry(entry string) string {
	entry = strings.TrimSpace(entry)
	if !strings.HasPrefix(entry, ArtifactScopePrefix) {
		return ""
	}
	raw := strings.TrimPrefix(entry, ArtifactScopePrefix)
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return strings.TrimSpace(raw)
}

// classifyScope splits scope entries into three groups: static canonical paths,
// query addresses, and artifact references. Artifact references use the /artifact/ prefix
// and point to MaterializedArtifact canonical keys.
func classifyScope(scope []string) (staticPaths, queryAddresses, artifactRefs []string) {
	staticPaths = make([]string, 0, len(scope))
	queryAddresses = make([]string, 0, len(scope))
	artifactRefs = make([]string, 0)

	for _, entry := range scope {
		switch {
		case IsQueryAddress(entry):
			queryAddresses = append(queryAddresses, entry)
		case IsArtifactScopeEntry(entry):
			artifactRefs = append(artifactRefs, entry)
		default:
			staticPaths = append(staticPaths, entry)
		}
	}

	return staticPaths, queryAddresses, artifactRefs
}

// splitScope разделяет scope на статические пути и QueryAddress.
// Deprecated: use classifyScope which also handles /artifact/ entries.
func splitScope(scope []string) (staticPaths []string, queryAddresses []string) {
	s, q, artifacts := classifyScope(scope)
	s = append(s, artifacts...)
	return s, q
}

// resolveQueryAddresses резолвит QueryAddress в canonical paths с логированием ошибок
// Возвращает map[queryAddress][]canonicalPaths и список ошибок
func resolveQueryAddresses(
	ctx context.Context,
	queryResolver *QueryResolver,
	tenantID string,
	queryAddresses []string,
) (resolvedQueries map[string][]string, errors []error) {
	resolvedQueries = make(map[string][]string)
	errors = make([]error, 0)
	
	for _, queryAddr := range queryAddresses {
		startTime := time.Now()
		
		qa, err := ParseQueryAddress(queryAddr)
		if err != nil {
			log.Printf("[QueryAddress] Failed to parse query address %q: %v", queryAddr, err)
			errors = append(errors, fmt.Errorf("parse query address %q: %w", queryAddr, err))
			continue
		}
		
		resolvedPaths, err := queryResolver.Resolve(ctx, tenantID, qa)
		resolveDuration := time.Since(startTime)
		
		if err != nil {
			log.Printf("[QueryAddress] Failed to resolve query address %q: %v (took %v)", queryAddr, err, resolveDuration)
			errors = append(errors, fmt.Errorf("resolve query address %q: %w", queryAddr, err))
			continue
		}
		
		resolvedQueries[queryAddr] = resolvedPaths
		log.Printf("[QueryAddress] Resolved %q to %d paths in %v", queryAddr, len(resolvedPaths), resolveDuration)
	}
	
	return resolvedQueries, errors
}

