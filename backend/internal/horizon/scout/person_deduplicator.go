package scout

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// PersonDeduplicator handles person deduplication using Wikidata, exact name matching,
// and phonetic matching for Arabic/transliterated names.
type PersonDeduplicator struct {
	fragmentsRepo  *repo.HorizonFragmentsRepository
	wikidataClient coreontology.WikidataClientInterface
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
func (d *PersonDeduplicator) DeduplicatePerson(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return nil, nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypePerson {
		return nil, nil
	}

	// =========================================================================
	// Tier 1: Wikidata ID match
	// =========================================================================
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID != "" {
		existing, err := d.findByWikidataID(ctx, wikidataID)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			fmt.Printf("[PersonDeduplicator] Found by Wikidata ID %s: %s\n", wikidataID, existing.CanonicalPath)
			return existing, nil
		}
	}

	// =========================================================================
	// Tier 1: Exact legalName match
	// =========================================================================
	if legalName := strings.TrimSpace(fragment.Identifiers["legalName"]); legalName != "" {
		existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", legalName, models.FragmentTypePerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
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
		existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", shortName, models.FragmentTypePerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
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
		existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", labelEN, models.FragmentTypePerson)
		if err == nil && len(existing) > 0 {
			for _, e := range existing {
				if e.ID != "" && e.ID != fragment.ID {
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
			fmt.Printf("[PersonDeduplicator] Found by phonetic match %q: %s\n", phoneticKey, existing.CanonicalPath)
			return existing, nil
		}
	}

	return nil, nil
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
	persons, err := d.fragmentsRepo.ListByTypeAndTenant(ctx, string(models.FragmentTypePerson), "global", 10000)
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
	existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "wikidataId", wikidataID, models.FragmentTypePerson)
	if err == nil && len(existing) > 0 {
		return existing[0], nil
	}

	// Also check KBRefs
	// Note: This would require a more complex query; for now rely on identifiers
	return nil, nil
}

// NormalizePhoneticKey normalizes Arabic/transliterated names for phonetic matching.
// Handles common transliteration variations:
//   - ou/oo/u → o (Tahnoun/Tahnoon/Tahnun → tahno...)
//   - ee/i/y → i (Mohammed/Muhammad → mohamid)
//   - kh → h (Khalid → halid)
//   - doubled consonants → single (Mohammed → mohamed)
//   - removes non-letter characters
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
		{"ee", "i"},
		{"kh", "h"},
		{"ph", "f"},
		{"qu", "k"},
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
	if len(strings.Fields(result)) < 3 {
		return ""
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

