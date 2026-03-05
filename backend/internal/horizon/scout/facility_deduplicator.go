package scout

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// FacilityDeduplicatorStats is a lightweight in-memory counter snapshot for observability.
type FacilityDeduplicatorStats struct {
	Calls           int64
	HitExactPath    int64
	HitAltPath      int64
	HitExactLabel   int64
	HitExactShort   int64
	HitNormalizedName int64
}

// FacilityDeduplicator handles facility deduplication by canonical path, alias path,
// exact name match, and geo-scoped normalized name match.
//
// Matching priority:
// Tier 1: Exact canonical path, alternative path (alias)
// Tier 2: Exact label[en] or shortName match (same geo)
// Tier 3: Normalized name match within same geo (3+ meaningful tokens)
type FacilityDeduplicator struct {
	fragmentsRepo *repo.HorizonFragmentsRepository

	calls             int64
	hitExactPath      int64
	hitAltPath        int64
	hitExactLabel     int64
	hitExactShort     int64
	hitNormalizedName int64
}

// NewFacilityDeduplicator creates a new facility deduplicator.
func NewFacilityDeduplicator(fragmentsRepo *repo.HorizonFragmentsRepository) *FacilityDeduplicator {
	return &FacilityDeduplicator{
		fragmentsRepo: fragmentsRepo,
	}
}

// DeduplicateFacility attempts to find an existing facility fragment that matches
// the given fragment. If found, returns the existing fragment; otherwise returns
// the original fragment unchanged.
func (d *FacilityDeduplicator) DeduplicateFacility(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return fragment, nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeFacility {
		return fragment, nil
	}
	atomic.AddInt64(&d.calls, 1)

	// Tier 1: Exact canonical path
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if cp != "" {
		if existing, err := d.fragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitExactPath, 1)
			fmt.Printf("[FacilityDeduplicator] Found by exact path: %s\n", cp)
			return existing, nil
		}
		// Alternative path (alias)
		if existing, err := d.fragmentsRepo.FindByAlternativePath(ctx, cp); err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID && existing.Type == models.FragmentTypeFacility {
			atomic.AddInt64(&d.hitAltPath, 1)
			fmt.Printf("[FacilityDeduplicator] Found by alt path: %s -> %s\n", cp, existing.CanonicalPath)
			return existing, nil
		}
	}

	geo := extractFacilityGeoFromPath(cp)
	if geo == "" || geo == "na" {
		return fragment, nil
	}

	// Tier 2: Exact label or shortName match (filter by same geo)
	labelEN := strings.TrimSpace(fragment.Labels["en"])
	if labelEN != "" {
		cands, err := d.fragmentsRepo.FindByLabel(ctx, "en", labelEN, models.FragmentTypeFacility)
		if err == nil {
			for _, c := range cands {
				if c == nil || c.ID == fragment.ID {
					continue
				}
				if extractFacilityGeoFromPath(c.CanonicalPath) == geo {
					atomic.AddInt64(&d.hitExactLabel, 1)
					fmt.Printf("[FacilityDeduplicator] Found by label %q (geo=%s): %s\n", labelEN, geo, c.CanonicalPath)
					return c, nil
				}
			}
		}
	}
	shortName := strings.TrimSpace(fragment.Identifiers["shortName"])
	if shortName != "" {
		cands, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", shortName, models.FragmentTypeFacility)
		if err == nil {
			for _, c := range cands {
				if c == nil || c.ID == fragment.ID {
					continue
				}
				if extractFacilityGeoFromPath(c.CanonicalPath) == geo {
					atomic.AddInt64(&d.hitExactShort, 1)
					fmt.Printf("[FacilityDeduplicator] Found by shortName %q (geo=%s): %s\n", shortName, geo, c.CanonicalPath)
					return c, nil
				}
			}
		}
	}

	// Tier 3: Normalized name match within same geo (requires 3+ meaningful tokens)
	normKey := normalizeFacilityAliasKey(labelEN)
	if normKey == "" {
		normKey = normalizeFacilityAliasKey(shortName)
	}
	if normKey == "" {
		if slug := extractFacilityNameFromPath(cp); slug != "" {
			normKey = normalizeFacilityAliasKey(strings.ReplaceAll(slug, "-", " "))
		}
	}
	if normKey != "" {
		prefix := "/fac/" + geo + "/"
		cands, err := d.fragmentsRepo.ListByCanonicalPathPrefix(ctx, prefix, 200)
		if err == nil {
			for _, c := range cands {
				if c == nil || c.Type != models.FragmentTypeFacility || c.ID == fragment.ID {
					continue
				}
				cLabel := strings.TrimSpace(c.Labels["en"])
				cShort := strings.TrimSpace(c.Identifiers["shortName"])
				cSlug := extractFacilityNameFromPath(c.CanonicalPath)
				for _, name := range []string{cLabel, cShort, strings.ReplaceAll(cSlug, "-", " ")} {
					if name == "" {
						continue
					}
					if normalizeFacilityAliasKey(name) == normKey {
						atomic.AddInt64(&d.hitNormalizedName, 1)
						fmt.Printf("[FacilityDeduplicator] Found by normalized name (geo=%s): %s\n", geo, c.CanonicalPath)
						return c, nil
					}
				}
			}
		}
	}

	return fragment, nil
}

// Stats returns a snapshot of deduplication statistics.
func (d *FacilityDeduplicator) Stats() FacilityDeduplicatorStats {
	if d == nil {
		return FacilityDeduplicatorStats{}
	}
	return FacilityDeduplicatorStats{
		Calls:             atomic.LoadInt64(&d.calls),
		HitExactPath:      atomic.LoadInt64(&d.hitExactPath),
		HitAltPath:        atomic.LoadInt64(&d.hitAltPath),
		HitExactLabel:     atomic.LoadInt64(&d.hitExactLabel),
		HitExactShort:     atomic.LoadInt64(&d.hitExactShort),
		HitNormalizedName: atomic.LoadInt64(&d.hitNormalizedName),
	}
}

// extractFacilityGeoFromPath returns geo from /fac/{geo}/{slug}.
func extractFacilityGeoFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	g := strings.ToLower(strings.TrimSpace(parts[1]))
	if g == "" || g == "na" {
		return ""
	}
	if !taxonomy.IsValidGeoForOrgGovFac(g) {
		return ""
	}
	return g
}

// extractFacilityNameFromPath returns the last slug segment from /fac/{geo}/{slug}.
func extractFacilityNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	slug := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if slug == "" || slug == "fac" || slug == "na" {
		return ""
	}
	return slug
}

// normalizeFacilityAliasKey normalizes facility names for dedupe matching:
// - lowercases, strips punctuation, removes stopwords, collapses whitespace.
// Returns empty string if fewer than 3 meaningful tokens (avoids false positives).
func normalizeFacilityAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	toks := strings.Fields(b.String())
	stop := map[string]struct{}{
		"by": {}, "the": {}, "of": {}, "and": {}, "at": {},
		"hotel": {}, "hotels": {}, "resort": {}, "inn": {}, "suites": {}, "suite": {},
	}
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if _, ok := stop[t]; ok {
			continue
		}
		out = append(out, t)
	}
	if len(out) < 3 {
		return ""
	}
	return strings.Join(out, " ")
}
