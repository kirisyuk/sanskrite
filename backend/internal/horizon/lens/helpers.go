package lens

import (
	"context"
	"fmt"
	"log"
	"time"
)

// splitScope разделяет scope на статические пути и QueryAddress
func splitScope(scope []string) (staticPaths []string, queryAddresses []string) {
	staticPaths = make([]string, 0, len(scope))
	queryAddresses = make([]string, 0, len(scope))
	
	for _, entry := range scope {
		if IsQueryAddress(entry) {
			queryAddresses = append(queryAddresses, entry)
		} else {
			staticPaths = append(staticPaths, entry)
		}
	}
	
	return staticPaths, queryAddresses
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

