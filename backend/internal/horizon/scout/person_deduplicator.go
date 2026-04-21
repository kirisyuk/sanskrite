package scout

import (
	"context"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
)

// PersonDeduplicatorStats is a lightweight in-memory counter snapshot for observability.
type PersonDeduplicatorStats struct {
	Calls            int64
	MatchWikidataID  int64
	MatchLegalName   int64
	MatchShortName   int64
	MatchLabelEN     int64
	MatchPhonetic    int64
	MatchTokenSubset int64
}

// PersonDeduplicator handles person deduplication using Wikidata, exact name matching,
// and phonetic matching for Arabic/transliterated names.
type PersonDeduplicator struct {
	fragmentsRepo  *repo.HorizonFragmentsRepository
	wikidataClient coreontology.WikidataClientInterface

	calls            int64
	matchWikidataID  int64
	matchLegalName   int64
	matchShortName   int64
	matchLabelEN     int64
	matchPhonetic    int64
	matchTokenSubset int64
}

// NewPersonDeduplicator creates a new person deduplicator.
func NewPersonDeduplicator(fragmentsRepo *repo.HorizonFragmentsRepository, client coreontology.WikidataClientInterface) *PersonDeduplicator {
	return &PersonDeduplicator{
		fragmentsRepo:  fragmentsRepo,
		wikidataClient: client,
	}
}

// DeduplicatePerson attempts to find an existing person fragment that matches
// the given fragment. If found, returns the existing fragment; otherwise returns nil.
//
// Matching priority:
// Tier 1 (100% confidence):
//  1. Wikidata ID match
//  2. Exact legalName match (case-insensitive)
//
// Tier 2 (high confidence):
//  3. Exact shortName match
//  4. Exact label[en] match
//
// Tier 3 (phonetic matching for Arabic/transliterated names):
//  5. Phonetic name match (catches Tahnoon/Tahnoun, Khalid/Halid, Mohammed/Muhammad)
//
// Tier 3.5 (token-subset matching for missing middle names):
//  6. Token-subset match - one name's tokens are a subset of another's
//     (catches "Hamed bin Mohamed Al Suwaidi" vs "Hamed bin Mohamed Khalifa Al Suwaidi")
func (d *PersonDeduplicator) DeduplicatePerson(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return nil, nil
	}
	if fragment == nil || fragment.Class != ah.FragmentClassPerson {
		return nil, nil
	}
	atomic.AddInt64(&d.calls, 1)

	// =========================================================================
	// Tier 1: Wikidata ID match
	// =========================================================================
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID != "" {
		existing, err := d.findByWikidataID(ctx, wikidataID)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			atomic.AddInt64(&d.matchWikidataID, 1)
			fmt.Printf("[PersonDeduplicator] Found by Wikidata ID %s: %s\n", wikidataID, existing.CanonicalPath)
			return existing, nil
		}
	}

	// =========================================================================
	// Tier 1: Exact legalName match
	// =========================================================================
	if legalName := strings.TrimSpace(fragment.Identifiers["legalName"]); legalName != "" {
		existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", legalName, ah.FragmentClassPerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
					atomic.AddInt64(&d.matchLegalName, 1)
					fmt.Printf("[PersonDeduplicator] Found by legalName %q: %s\n", legalName, e.CanonicalPath)
					return e, nil
				}
			}
		}
	}

	// =========================================================================
	// Tier 2: Exact shortName match
	// =========================================================================
	if shortName := strings.TrimSpace(fragment.Identifiers["shortName"]); shortName != "" {
		existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", shortName, ah.FragmentClassPerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
					atomic.AddInt64(&d.matchShortName, 1)
					fmt.Printf("[PersonDeduplicator] Found by shortName %q: %s\n", shortName, e.CanonicalPath)
					return e, nil
				}
			}
		}
	}

	// =========================================================================
	// Tier 2: Exact label[en] match
	// =========================================================================
	if labelEN := strings.TrimSpace(fragment.Labels["en"]); labelEN != "" {
		existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", labelEN, ah.FragmentClassPerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
					atomic.AddInt64(&d.matchLabelEN, 1)
					fmt.Printf("[PersonDeduplicator] Found by label[en] %q: %s\n", labelEN, e.CanonicalPath)
					return e, nil
				}
			}
		}
	}

	// =========================================================================
	// Tier 3: Phonetic matching for Arabic/transliterated names
	// =========================================================================
	// This catches variations like Tahnoon/Tahnoun, Khalid/Halid, Mohammed/Muhammad
	phoneticKey := d.buildPhoneticKey(fragment)
	if phoneticKey != "" {
		existing, err := d.findByPhoneticKey(ctx, phoneticKey, fragment.ID)
		if err == nil && existing != nil {
			atomic.AddInt64(&d.matchPhonetic, 1)
			fmt.Printf("[PersonDeduplicator] Found by phonetic match %q: %s\n", phoneticKey, existing.CanonicalPath)
			return existing, nil
		}
	}

	// =========================================================================
	// Tier 3.5: Token-subset matching for missing middle names
	// =========================================================================
	// This catches cases like:
	// - "Hamed bin Mohamed Al Suwaidi" vs "Hamed bin Mohamed Khalifa Al Suwaidi"
	// One name has a middle name/honorific that the other doesn't.
	// Requires at least 4 tokens and matching first name to avoid false positives.
	nameTokens := d.buildNameTokenSet(fragment)
	if len(nameTokens) >= 4 {
		existing, err := d.findByTokenSubset(ctx, fragment, fragment.ID)
		if err == nil && existing != nil {
			atomic.AddInt64(&d.matchTokenSubset, 1)
			fmt.Printf("[PersonDeduplicator] Found by token-subset match: %s\n", existing.CanonicalPath)
			return existing, nil
		}
	}

	return nil, nil
}

// Stats returns a best-effort snapshot of in-memory counters.
func (d *PersonDeduplicator) Stats() PersonDeduplicatorStats {
	if d == nil {
		return PersonDeduplicatorStats{}
	}
	return PersonDeduplicatorStats{
		Calls:            atomic.LoadInt64(&d.calls),
		MatchWikidataID:  atomic.LoadInt64(&d.matchWikidataID),
		MatchLegalName:   atomic.LoadInt64(&d.matchLegalName),
		MatchShortName:   atomic.LoadInt64(&d.matchShortName),
		MatchLabelEN:     atomic.LoadInt64(&d.matchLabelEN),
		MatchPhonetic:    atomic.LoadInt64(&d.matchPhonetic),
		MatchTokenSubset: atomic.LoadInt64(&d.matchTokenSubset),
	}
}

// buildPhoneticKey creates a phonetic key from the fragment's names.
// Returns empty string if the key is too generic (less than 3 tokens).
func (d *PersonDeduplicator) buildPhoneticKey(fragment *models.HorizonFragment) string {
	// Try different name sources
	names := []string{
		fragment.Labels["en"],
		fragment.Identifiers["legalName"],
		fragment.Identifiers["shortName"],
	}

	// Also try canonical path slug
	if strings.HasPrefix(fragment.CanonicalPath, "/person/") {
		parts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(parts) >= 2 {
			names = append(names, strings.ReplaceAll(parts[len(parts)-1], "-", " "))
		}
	}

	// Find the longest valid phonetic key
	var bestKey string
	for _, name := range names {
		key := NormalizePhoneticKey(name)
		if key != "" && len(key) > len(bestKey) {
			bestKey = key
		}
	}

	return bestKey
}

// findByPhoneticKey searches for existing person fragments that match the phonetic key.
func (d *PersonDeduplicator) findByPhoneticKey(ctx context.Context, phoneticKey string, excludeID string) (*models.HorizonFragment, error) {
	if phoneticKey == "" {
		return nil, nil
	}

	// Get all PERSON fragments and check phonetic match
	// Note: This is a scan operation; for production, consider adding a phonetic index field to fragments
	persons, err := d.fragmentsRepo.ListByTypeAndTenant(ctx, string(ah.FragmentClassPerson), "global", 10000)
	if err != nil {
		return nil, err
	}

	for _, p := range persons {
		if p.ID == excludeID {
			continue
		}

		// Check phonetic match
		candidateKey := d.buildPhoneticKey(p)
		if candidateKey == phoneticKey {
			return p, nil
		}
	}

	return nil, nil
}

// findByWikidataID searches for a person fragment by Wikidata ID.
func (d *PersonDeduplicator) findByWikidataID(ctx context.Context, wikidataID string) (*models.HorizonFragment, error) {
	if wikidataID == "" {
		return nil, nil
	}

	// Search by identifier
	existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "wikidataId", wikidataID, ah.FragmentClassPerson)
	if err == nil && len(existing) > 0 {
		return existing[0], nil
	}

	// Also check KBRefs
	// Note: This would require a more complex query; for now rely on identifiers
	return nil, nil
}

// NormalizePhoneticKey normalizes Arabic/transliterated names for phonetic matching.
// Handles common transliteration variations:
//   - ou/oo/uu/u → o (Tahnoun/Tahnoon/Tahnun → tahno...)
//   - ee/e/i/y → i (Mohammed/Muhammad → mohamid, Yasser/Yasir → yasir)
//   - kh → h (Khalid → halid)
//   - doubled consonants → single (Mohammed → mohamed)
//   - removes non-letter characters
//   - removes common Arabic particles (al/el/bin/bint/ibn/ben/abu/banu) when enough tokens remain
//
// Returns empty string if the result has less than 3 tokens (too generic).
//
// Example:
//
//	"Tahnoon bin Zayed Al Nahyan" → "tahon bin zaid al nahian"
//	"Tahnoun bin Zayed Al Nahyan" → "tahon bin zaid al nahian"
func NormalizePhoneticKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Step 1: Replace common transliteration variations
	replacements := []struct{ from, to string }{
		{"oo", "o"},
		{"ou", "o"},
		{"uu", "o"},
		{"ee", "i"},
		{"ie", "i"},
		{"ei", "i"},
		{"y", "i"},
		{"e", "i"},
		{"u", "o"},
		{"kh", "h"},
		{"ph", "f"},
		{"qu", "k"},
		{"aa", "a"},
	}
	for _, r := range replacements {
		name = strings.ReplaceAll(name, r.from, r.to)
	}

	// Step 2: Remove non-letters, keep spaces
	var b strings.Builder
	b.Grow(len(name))
	var prevChar rune = ' '
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			// Remove doubled consonants (except vowels)
			vowels := "aeiou"
			if r == prevChar && !strings.ContainsRune(vowels, r) {
				continue
			}
			b.WriteRune(r)
			prevChar = r
		} else if r == ' ' && prevChar != ' ' {
			b.WriteRune(r)
			prevChar = ' '
		}
	}

	result := strings.TrimSpace(b.String())

	// Safety: require at least 3 tokens to avoid overly generic matches
	tokens := strings.Fields(result)
	if len(tokens) < 3 {
		return ""
	}

	// Step 3: Remove common Arabic particles if we still have >=3 tokens
	particles := map[string]struct{}{
		"al": {}, "el": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abu": {}, "banu": {},
	}
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := particles[t]; ok {
			continue
		}
		filtered = append(filtered, t)
	}
	if len(filtered) >= 3 {
		return strings.Join(filtered, " ")
	}

	return result
}

// NormalizePersonName normalizes a person name for comparison.
// This is a simpler normalization than phonetic - just lowercases and removes extra spaces.
func NormalizePersonName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Remove non-letter/digit characters except spaces
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}

	return strings.TrimSpace(b.String())
}

// buildNameTokenSet creates a set of normalized tokens from the fragment's names.
// Used for token-subset matching. Applies phonetic normalization to each token.
func (d *PersonDeduplicator) buildNameTokenSet(fragment *models.HorizonFragment) map[string]struct{} {
	if fragment == nil {
		return nil
	}

	names := gatherPersonNames(fragment)

	// Build unified token set with phonetic normalization
	tokenSet := make(map[string]struct{})
	for _, name := range names {
		tokens := normalizeNameToTokens(name)
		for _, t := range tokens {
			if t != "" {
				tokenSet[t] = struct{}{}
			}
		}
	}

	return tokenSet
}

// gatherPersonNames collects all name sources from a person fragment.
func gatherPersonNames(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	names := []string{
		fragment.Labels["en"],
		fragment.Identifiers["legalName"],
		fragment.Identifiers["shortName"],
	}
	// Also try canonical path slug
	if strings.HasPrefix(fragment.CanonicalPath, "/person/") {
		parts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(parts) >= 2 {
			names = append(names, strings.ReplaceAll(parts[len(parts)-1], "-", " "))
		}
	}
	return names
}

// extractFirstName extracts the first core token (first name) from a person fragment.
// This is used for anchoring token-subset matches.
func extractFirstName(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}

	particles := map[string]struct{}{
		"al": {}, "il": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abu": {}, "banu": {},
		"sheikh": {}, "dr": {}, "captain": {}, "general": {}, "prince": {}, "princess": {},
	}

	names := gatherPersonNames(fragment)
	for _, name := range names {
		tokens := normalizeNameToTokens(name)
		for _, t := range tokens {
			if t == "" {
				continue
			}
			// Skip particles and titles
			if _, isParticle := particles[t]; isParticle {
				continue
			}
			return t // Return first non-particle token
		}
	}
	return ""
}

// normalizeNameToTokens normalizes a name and returns phonetically-normalized tokens.
// Removes Arabic particles and applies phonetic normalization.
func normalizeNameToTokens(name string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}

	// Apply phonetic replacements
	replacements := []struct{ from, to string }{
		{"oo", "o"},
		{"ou", "o"},
		{"uu", "o"},
		{"ee", "i"},
		{"ie", "i"},
		{"ei", "i"},
		{"y", "i"},
		{"e", "i"},
		{"u", "o"},
		{"kh", "h"},
		{"ph", "f"},
		{"qu", "k"},
		{"aa", "a"},
	}
	for _, r := range replacements {
		name = strings.ReplaceAll(name, r.from, r.to)
	}

	// Remove non-letters, keep spaces
	var b strings.Builder
	b.Grow(len(name))
	prevChar := ' '
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			// Remove doubled consonants
			vowels := "aeiou"
			if r == prevChar && !strings.ContainsRune(vowels, r) {
				continue
			}
			b.WriteRune(r)
			prevChar = r
		} else if r == ' ' && prevChar != ' ' {
			b.WriteRune(r)
			prevChar = ' '
		}
	}

	result := strings.TrimSpace(b.String())
	tokens := strings.Fields(result)

	// Filter out common Arabic particles (but keep them in the returned set for matching)
	// We only filter for the "core" tokens check, not the actual matching
	return tokens
}

// findByTokenSubset searches for existing person fragments where tokens are subset-related.
// Returns a match if:
// - Both names have at least 4 core tokens (non-particles)
// - First name (first non-particle token) matches exactly
// - All core tokens of the shorter name appear in the longer name
// - The difference (extra tokens in larger) is at most 2
func (d *PersonDeduplicator) findByTokenSubset(ctx context.Context, inputFragment *models.HorizonFragment, excludeID string) (*models.HorizonFragment, error) {
	inputTokens := d.buildNameTokenSet(inputFragment)
	// Require at least 4 tokens to avoid false positives on short names
	if len(inputTokens) < 4 {
		return nil, nil
	}

	// Extract first name for anchoring
	inputFirstName := extractFirstName(inputFragment)
	if inputFirstName == "" {
		return nil, nil
	}

	// Get all PERSON fragments
	persons, err := d.fragmentsRepo.ListByTypeAndTenant(ctx, "global", ah.FragmentClassPerson, 10000)
	if err != nil {
		return nil, err
	}

	// Arabic particles to ignore when counting core tokens
	particles := map[string]struct{}{
		"al": {}, "il": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abu": {}, "banu": {},
	}

	// Extract core tokens (non-particles) from input
	inputCore := extractCoreTokens(inputTokens, particles)
	if len(inputCore) < 3 {
		return nil, nil
	}

	for _, p := range persons {
		if p.ID == excludeID {
			continue
		}

		candidateTokens := d.buildNameTokenSet(p)
		// Require at least 4 tokens for candidates too
		if len(candidateTokens) < 4 {
			continue
		}

		// First name must match
		candidateFirstName := extractFirstName(p)
		if candidateFirstName == "" || candidateFirstName != inputFirstName {
			continue
		}

		candidateCore := extractCoreTokens(candidateTokens, particles)
		if len(candidateCore) < 3 {
			continue
		}

		// Check if one is a subset of the other
		if isTokenSubsetMatchNoFirstCheck(inputCore, candidateCore) {
			return p, nil
		}
	}

	return nil, nil
}

// extractCoreTokens extracts non-particle tokens from a token set.
func extractCoreTokens(tokens map[string]struct{}, particles map[string]struct{}) []string {
	core := make([]string, 0, len(tokens))
	for t := range tokens {
		if _, isParticle := particles[t]; !isParticle {
			core = append(core, t)
		}
	}
	// Sort for deterministic ordering
	sort.Strings(core)
	return core
}

// isTokenSubsetMatch checks if one set of core tokens is a near-subset of another.
// This version requires first token match (for use by exported wrapper).
func isTokenSubsetMatch(a, b []string) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}

	// First token must match (sorted alphabetically, so this is approximate)
	if a[0] != b[0] {
		return false
	}

	return isTokenSubsetMatchNoFirstCheck(a, b)
}

// isTokenSubsetMatchNoFirstCheck checks token subset without first-token check.
// Used internally when first name comparison is done separately.
// Match criteria:
// - Both sets must have at least 3 tokens
// - All tokens from the smaller set must appear in the larger set
// - The difference (extra tokens in larger) must be at most 2
// This catches: "hamed mohamid sowaidi" vs "hamed mohamid halifa sowaidi"
func isTokenSubsetMatchNoFirstCheck(a, b []string) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}

	// Ensure a is the smaller set
	smaller, larger := a, b
	if len(a) > len(b) {
		smaller, larger = b, a
	}

	// Build set from larger
	largerSet := make(map[string]struct{}, len(larger))
	for _, t := range larger {
		largerSet[t] = struct{}{}
	}

	// Count how many tokens from smaller appear in larger
	matchCount := 0
	for _, t := range smaller {
		if _, ok := largerSet[t]; ok {
			matchCount++
		}
	}

	// All tokens from smaller must be in larger
	if matchCount < len(smaller) {
		return false
	}

	// The difference must be at most 2 (allow 1-2 extra middle names/honorifics)
	diff := len(larger) - len(smaller)
	if diff > 2 {
		return false
	}

	// Additional safety: at least 3 matching core tokens
	if matchCount < 3 {
		return false
	}

	return true
}

// IsTokenSubsetMatchExported is an exported wrapper for isTokenSubsetMatch.
// Used by deduplication scripts.
func IsTokenSubsetMatchExported(a, b []string) bool {
	return isTokenSubsetMatch(a, b)
}
