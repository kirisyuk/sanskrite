package lens

import (
	"fmt"
	"regexp"
	"strings"
)

// QueryAddress represents a dynamic query-based address that resolves to multiple canonical paths.
// Format: /query/{criteria}
//
// Examples:
//   /query/sphere:/sphere/logistics/maritime
//   /query/sector:/Technology/Semiconductors/**
//   /query/location:/loc/id/** AND entityType:/Infrastructure/Logistics/Seaport
//   /query/canonicalPath:/org/tsmc OR canonicalPath:/org/samsung
type QueryAddress struct {
	Raw string // Original query string

	// Criteria for filtering
	SpherePaths    []string // Array of sphere paths to match
	SectorPatterns []string // Sector path patterns (supports **)
	LocationPatterns []string // Location path patterns (supports **)
	EntityTypePatterns []string // Entity type patterns (supports **)
	CanonicalPathPatterns []string // Canonical path patterns (supports **)

	// Logical operators
	Operator string // "AND" | "OR" (default: "AND")
}

var (
	// Pattern: /query/{criteria}
	queryAddressRegex = regexp.MustCompile(`^/query/(.+)$`)

	// Pattern for individual criteria: {field}:{value}
	criteriaRegex = regexp.MustCompile(`(\w+):([^\s]+)`)
)

// IsQueryAddress checks if a string is a QueryAddress
func IsQueryAddress(address string) bool {
	return strings.HasPrefix(address, "/query/")
}

// ParseQueryAddress parses a QueryAddress string into a structured QueryAddress
func ParseQueryAddress(address string) (*QueryAddress, error) {
	address = strings.TrimSpace(address)

	if !IsQueryAddress(address) {
		return nil, fmt.Errorf("not a query address: %s", address)
	}

	matches := queryAddressRegex.FindStringSubmatch(address)
	if len(matches) < 2 {
		return nil, fmt.Errorf("invalid query address format: %s", address)
	}

	criteria := matches[1]
	qa := &QueryAddress{
		Raw:      address,
		Operator: "AND", // Default operator
	}

	// Detect operator (AND/OR)
	if strings.Contains(strings.ToUpper(criteria), " OR ") {
		qa.Operator = "OR"
		criteria = strings.ReplaceAll(criteria, " OR ", " ")
		criteria = strings.ReplaceAll(criteria, " or ", " ")
	} else if strings.Contains(strings.ToUpper(criteria), " AND ") {
		qa.Operator = "AND"
		criteria = strings.ReplaceAll(criteria, " AND ", " ")
		criteria = strings.ReplaceAll(criteria, " and ", " ")
	}

	// Parse individual criteria
	criteriaMatches := criteriaRegex.FindAllStringSubmatch(criteria, -1)
	if len(criteriaMatches) == 0 {
		return nil, fmt.Errorf("no valid criteria found in: %s", address)
	}

	for _, match := range criteriaMatches {
		if len(match) < 3 {
			continue
		}

		field := strings.ToLower(strings.TrimSpace(match[1]))
		value := strings.TrimSpace(match[2])

		if value == "" {
			continue
		}

		switch field {
		case "sphere", "spherepath", "spherepaths":
			qa.SpherePaths = append(qa.SpherePaths, value)

		case "sector", "sectorpath":
			qa.SectorPatterns = append(qa.SectorPatterns, value)

		case "location", "locationpath":
			qa.LocationPatterns = append(qa.LocationPatterns, value)

		case "entitytype", "entity_type":
			qa.EntityTypePatterns = append(qa.EntityTypePatterns, value)

		case "canonicalpath", "canonical_path", "path":
			qa.CanonicalPathPatterns = append(qa.CanonicalPathPatterns, value)

		default:
			return nil, fmt.Errorf("unknown criteria field: %s", field)
		}
	}

	// Validate: at least one criteria
	if len(qa.SpherePaths) == 0 &&
	   len(qa.SectorPatterns) == 0 &&
	   len(qa.LocationPatterns) == 0 &&
	   len(qa.EntityTypePatterns) == 0 &&
	   len(qa.CanonicalPathPatterns) == 0 {
		return nil, fmt.Errorf("no valid criteria in query address: %s", address)
	}

	return qa, nil
}

// MatchesPattern checks if a value matches a pattern (supports ** wildcard)
// Pattern examples:
//   /Technology/Semiconductors/**  → matches /Technology/Semiconductors/Design
//   /loc/id/**                      → matches /loc/id/ja/jakarta
//   /org/tsmc                       → exact match only
func MatchesPattern(value, pattern string) bool {
	value = strings.TrimSpace(value)
	pattern = strings.TrimSpace(pattern)

	if value == pattern {
		return true // Exact match
	}

	if !strings.Contains(pattern, "**") {
		return false // No wildcard, no match
	}

	// Replace ** with regex pattern
	// /Technology/Semiconductors/** → /Technology/Semiconductors/.*
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, "\\*\\*", ".*")
	regexPattern = "^" + regexPattern + "$"

	matched, err := regexp.MatchString(regexPattern, value)
	if err != nil {
		return false
	}

	return matched
}

// Validate checks if the QueryAddress is valid
func (qa *QueryAddress) Validate() error {
	if qa.Raw == "" {
		return fmt.Errorf("empty query address")
	}

	if qa.Operator != "AND" && qa.Operator != "OR" {
		return fmt.Errorf("invalid operator: %s (must be AND or OR)", qa.Operator)
	}

	// At least one criteria must be present
	if len(qa.SpherePaths) == 0 &&
	   len(qa.SectorPatterns) == 0 &&
	   len(qa.LocationPatterns) == 0 &&
	   len(qa.EntityTypePatterns) == 0 &&
	   len(qa.CanonicalPathPatterns) == 0 {
		return fmt.Errorf("at least one criteria required")
	}

	return nil
}

// String returns a human-readable representation
func (qa *QueryAddress) String() string {
	if qa == nil {
		return ""
	}
	return qa.Raw
}
