package lens

import (
	"fmt"
	"strings"
)

// QueryAddress represents a dynamic query-based address that resolves to multiple canonical paths.
// Format: /query/{expression}
//
// Expression grammar (adult mode):
//   - parentheses: ( ... )
//   - operators: NOT, AND, OR with precedence NOT > AND > OR
//   - criteria: field:value where value can be unquoted (no spaces) or quoted ("...")
//
// Examples:
//   /query/sector:/Technology/Semiconductors/**
//   /query/location:/loc/id/** AND entityType:/Infrastructure/Logistics/Seaport
//   /query/(canonicalPath:/org/tsmc OR canonicalPath:/org/samsung) AND NOT sector:/Energy/**
type QueryAddress struct {
	Raw string // Original query string
	Expr Expr   // Parsed expression AST
}

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

	rawExpr := strings.TrimSpace(strings.TrimPrefix(address, "/query/"))
	if rawExpr == "" {
		return nil, fmt.Errorf("no expression found in: %s", address)
	}

	expr, err := parseQueryExpr(rawExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid query address expression: %w", err)
	}

	qa := &QueryAddress{Raw: address, Expr: expr}
	if err := qa.Validate(); err != nil {
		return nil, err
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

	// Fast path for common prefix wildcard "/x/y/**"
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return value == prefix || strings.HasPrefix(value, prefix+"/")
	}

	// Conservative fallback: treat "**" as ".*" regex.
	// This keeps backwards-compat for rare patterns like "/a/**/b".
	regexPattern := toDoubleStarRegex(pattern)
	return regexPattern.MatchString(value)
}

// Validate checks if the QueryAddress is valid
func (qa *QueryAddress) Validate() error {
	if qa.Raw == "" {
		return fmt.Errorf("empty query address")
	}
	if qa.Expr == nil {
		return fmt.Errorf("empty query expression")
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
