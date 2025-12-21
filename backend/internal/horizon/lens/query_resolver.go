package lens

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// QueryResolver resolves QueryAddress to a list of canonical paths
type QueryResolver struct {
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
}

// NewQueryResolver creates a new QueryResolver
func NewQueryResolver(fragmentsRepo *horizonrepo.HorizonFragmentsRepository) *QueryResolver {
	return &QueryResolver{
		fragmentsRepo: fragmentsRepo,
	}
}

// Resolve resolves a QueryAddress to a list of canonical paths
// Returns (canonicalPaths, error)
func (r *QueryResolver) Resolve(ctx context.Context, tenantID string, queryAddress *QueryAddress) ([]string, error) {
	startTime := time.Now()
	
	if queryAddress == nil {
		return nil, fmt.Errorf("nil query address")
	}

	if err := queryAddress.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query address: %w", err)
	}

	if tenantID == "" {
		tenantID = "global" // HorizonFragments are always global
	}

	// Build Firestore query based on criteria
	query, err := r.buildQuery(tenantID, queryAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	// Execute query
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Extract canonical paths and apply pattern matching
	canonicalPaths := []string{}
	seen := map[string]bool{}
	matchedCount := 0
	skippedCount := 0

	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			skippedCount++
			continue // Skip invalid documents
		}

		// Apply pattern matching filters (for wildcards)
		if !r.matchesQueryAddress(&fragment, queryAddress) {
			skippedCount++
			continue
		}

		// Deduplicate
		if !seen[fragment.CanonicalPath] {
			seen[fragment.CanonicalPath] = true
			canonicalPaths = append(canonicalPaths, fragment.CanonicalPath)
			matchedCount++
		}
	}

	duration := time.Since(startTime)
	log.Printf("[QueryResolver] Resolved query %q: %d paths found (matched %d, skipped %d) in %v", 
		queryAddress.Raw, len(canonicalPaths), matchedCount, skippedCount, duration)

	return canonicalPaths, nil
}

// buildQuery builds a Firestore query from QueryAddress criteria
func (r *QueryResolver) buildQuery(tenantID string, qa *QueryAddress) (firestore.Query, error) {
	// NOTE: SpherePaths filtering is not supported for HorizonFragment queries
	// SpherePaths exist only in Frame/OntologyFact, not in HorizonFragment
	// For now, sphere filtering will be done in-memory after fetching fragments
	
	// NOTE: SpherePaths filtering is not supported for HorizonFragment queries
	// SpherePaths exist only in Frame/OntologyFact, not in HorizonFragment
	// For now, sphere filtering will be done in-memory after fetching fragments
	
	baseQuery := r.fragmentsRepo.Query().
		Where("tenantId", "==", tenantID).
		Where("governance.status", "==", "ACTIVE") // Only active fragments

	// IMPORTANT: Firestore has limitations on queries:
	// - Only ONE array-contains per query
	// - No OR operator support (must use multiple queries)
	// - Inequality filters can only be on one field

	// Strategy: Use most restrictive filter in Firestore, apply pattern matching in-memory

	// NOTE: SpherePaths are not stored in HorizonFragment, so sphere filtering
	// will be done entirely in-memory after fetching fragments

	// Priority 2: SectorPath (if no wildcard, use range query)
	if len(qa.SectorPatterns) > 0 {
		pattern := qa.SectorPatterns[0]
		if strings.HasSuffix(pattern, "/**") {
			// Range query for prefix match
			prefix := strings.TrimSuffix(pattern, "/**")
			baseQuery = baseQuery.
				Where("sectorPath", ">=", prefix).
				Where("sectorPath", "<", prefix+"\ufffd")
			return baseQuery, nil
		} else if !containsWildcard(pattern) {
			// Exact match
			baseQuery = baseQuery.Where("sectorPath", "==", pattern)
			return baseQuery, nil
		}
	}

	// Priority 3: LocationPath (if no wildcard, use range query)
	if len(qa.LocationPatterns) > 0 {
		pattern := qa.LocationPatterns[0]
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			countryCode := extractCountryCode(prefix)
			if countryCode != "" {
				baseQuery = baseQuery.
					Where("homeLocation.iso3166_1", "==", countryCode)
				return baseQuery, nil
			}
		} else if !containsWildcard(pattern) {
			// Exact match
			countryCode := extractCountryCode(pattern)
			if countryCode != "" {
				baseQuery = baseQuery.Where("homeLocation.iso3166_1", "==", countryCode)
				return baseQuery, nil
			}
		}
	}

	// Priority 4: EntityPath (if no wildcard, use range query)
	if len(qa.EntityTypePatterns) > 0 {
		pattern := qa.EntityTypePatterns[0]
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			baseQuery = baseQuery.
				Where("entityPath", ">=", prefix).
				Where("entityPath", "<", prefix+"\ufffd")
			return baseQuery, nil
		} else if !containsWildcard(pattern) {
			baseQuery = baseQuery.Where("entityPath", "==", pattern)
			return baseQuery, nil
		}
	}

	// Priority 5: CanonicalPath (if no wildcard, use range query)
	if len(qa.CanonicalPathPatterns) > 0 {
		pattern := qa.CanonicalPathPatterns[0]
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			baseQuery = baseQuery.
				Where("canonicalPath", ">=", prefix).
				Where("canonicalPath", "<", prefix+"\ufffd")
			return baseQuery, nil
		} else if !containsWildcard(pattern) {
			baseQuery = baseQuery.Where("canonicalPath", "==", pattern)
			return baseQuery, nil
		}
	}

	// Fallback: no Firestore filtering, return all active fragments
	// Pattern matching will be done in-memory
	return baseQuery, nil
}

// buildLocationPath строит locationPath из HomeLocation
func buildLocationPath(homeLocation *models.Location) string {
	if homeLocation == nil || homeLocation.ISO3166_1 == nil {
		return ""
	}
	
	locationPath := "/loc/" + strings.ToLower(*homeLocation.ISO3166_1)
	if homeLocation.ISO3166_2 != nil && *homeLocation.ISO3166_2 != "" {
		parts := strings.Split(*homeLocation.ISO3166_2, "-")
		if len(parts) > 1 {
			locationPath += "/" + strings.ToLower(parts[1])
		}
	}
	return locationPath
}

// criterionMatcher функция для проверки соответствия критерию
type criterionMatcher func(fragment *models.HorizonFragment, patterns []string) bool

// matchesQueryAddress checks if a fragment matches the QueryAddress criteria
func (r *QueryResolver) matchesQueryAddress(fragment *models.HorizonFragment, qa *QueryAddress) bool {
	if fragment == nil || qa == nil {
		return false
	}

	// Определяем matchers для каждого типа критерия
	matchers := map[string]criterionMatcher{
		"sphere": func(f *models.HorizonFragment, patterns []string) bool {
			if len(patterns) == 0 {
				return true // No criteria = match
			}
			// NOTE: SpherePaths are not stored in HorizonFragment
			// They exist only in Frame/OntologyFact
			// For now, sphere filtering always returns false (no match)
			// TODO: Implement sphere filtering by querying related Frames or OntologyFacts
			return false
		},
		"sector": func(f *models.HorizonFragment, patterns []string) bool {
			if len(patterns) == 0 {
				return true
			}
			for _, pattern := range patterns {
				if MatchesPattern(f.SectorPath, pattern) {
					return true
				}
			}
			return false
		},
		"location": func(f *models.HorizonFragment, patterns []string) bool {
			if len(patterns) == 0 {
				return true
			}
			locationPath := buildLocationPath(f.HomeLocation)
			if locationPath == "" {
				return false // No location data
			}
			for _, pattern := range patterns {
				if MatchesPattern(locationPath, pattern) {
					return true
				}
			}
			return false
		},
		"entityType": func(f *models.HorizonFragment, patterns []string) bool {
			if len(patterns) == 0 {
				return true
			}
			for _, pattern := range patterns {
				if MatchesPattern(f.EntityPath, pattern) {
					return true
				}
			}
			return false
		},
		"canonicalPath": func(f *models.HorizonFragment, patterns []string) bool {
			if len(patterns) == 0 {
				return true
			}
			for _, pattern := range patterns {
				if MatchesPattern(f.CanonicalPath, pattern) {
					return true
				}
			}
			return false
		},
	}

	// Выполняем проверки для каждого критерия
	results := map[string]bool{
		"sphere":        matchers["sphere"](fragment, qa.SpherePaths),
		"sector":        matchers["sector"](fragment, qa.SectorPatterns),
		"location":      matchers["location"](fragment, qa.LocationPatterns),
		"entityType":    matchers["entityType"](fragment, qa.EntityTypePatterns),
		"canonicalPath": matchers["canonicalPath"](fragment, qa.CanonicalPathPatterns),
	}

	// Применяем оператор (AND/OR)
	if qa.Operator == "OR" {
		return results["sphere"] || results["sector"] || results["location"] || results["entityType"] || results["canonicalPath"]
	}
	// Default: AND - все критерии должны совпадать (или отсутствовать)
	return results["sphere"] && results["sector"] && results["location"] && results["entityType"] && results["canonicalPath"]
}

// Helper functions

func containsWildcard(s string) bool {
	return strings.Contains(s, "*")
}

func extractCountryCode(locationPath string) string {
	// /loc/id/... → "id"
	parts := strings.Split(strings.Trim(locationPath, "/"), "/")
	if len(parts) >= 2 && parts[0] == "loc" {
		return strings.ToLower(parts[1])
	}
	return ""
}
