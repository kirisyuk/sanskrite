package models

import (
	"sort"
	"strings"
)

// MaxIndexPaths is a safety cap to keep HorizonFragment documents bounded.
// The intent is algorithmic filtering/scoping, not perfect recall.
const MaxIndexPaths = 80

// BuildIndexPaths deterministically builds a bounded set of index paths for a fragment.
//
// It merges:
// - canonicalPath (as-is)
// - /industry/... (+ ancestors)
// - /entity + entityPath (+ ancestors)
// - location paths from HomeLocation (as /loc/{country}[/{level1}[/{level2}]])
// - /tag/{slug} for Tags
// - /id/{key}/{slug(value)} for Identifiers
// - extra paths (e.g., sphere paths from intent/facts)
func BuildIndexPaths(f *HorizonFragment, extra []string) []string {
	if f == nil {
		return nil
	}

	// Priority buckets to keep important coordinates even when we hit cap.
	canonical := []string{}
	location := []string{}
	spheres := []string{}
	industry := []string{}
	segments := []string{}
	entity := []string{}
	identifiers := []string{}
	tags := []string{}
	other := []string{}

	cp := strings.TrimSpace(f.CanonicalPath)
	if cp != "" {
		canonical = append(canonical, cp)
	}

	// Industry paths (canonical) → /industry/... (+ ancestors)
	for _, ip := range f.IndustryPaths {
		ip = strings.TrimSpace(ip)
		if !strings.HasPrefix(ip, "/industry/") {
			continue
		}
		industry = append(industry, withAncestors(ip, 6)...)
	}

	// Market segments (canonical) → /segment/... (+ ancestors)
	for _, sp := range f.MarketSegments {
		sp = strings.TrimSpace(sp)
		if !strings.HasPrefix(sp, "/segment/") {
			continue
		}
		segments = append(segments, withAncestors(sp, 4)...)
	}

	// Entity path (guide taxonomy) → index prefix /entity/...
	ep := strings.TrimSpace(f.EntityPath)
	if ep != "" {
		entity = append(entity, prefixedWithAncestors("/entity", ep, 6)...)
	}

	// Location (from ISO codes) → canonical /loc/...
	location = append(location, buildLocationIndexPaths(f)...)

	// Tags → /tag/{slug}
	for _, t := range f.Tags {
		s := slugToken(t)
		if s == "" {
			continue
		}
		tags = append(tags, "/tag/"+s)
	}

	// Identifiers → /id/{key}/{slug(value)}
	for k, v := range f.Identifiers {
		key := slugToken(k)
		val := slugToken(v)
		if key == "" || val == "" {
			continue
		}
		identifiers = append(identifiers, "/id/"+key+"/"+val)
	}

	// SpherePaths from fragment (first-class field)
	for _, p := range f.SpherePaths {
		p = strings.TrimSpace(p)
		if p != "" && strings.HasPrefix(p, "/sphere/") {
			spheres = append(spheres, p)
		}
	}

	// Org size (coarse bucket) → /orgsize/{value}
	if s := slugToken(strings.TrimSpace(f.OrgSize)); s != "" {
		other = append(other, "/orgsize/"+s)
	}

	// Extras: if it's a sphere canonical path, treat as sphere coordinate; else keep as "other".
	for _, p := range extra {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "/sphere/") {
			spheres = append(spheres, p)
		} else if strings.HasPrefix(p, "/loc/") {
			location = append(location, p)
		} else if strings.HasPrefix(p, "/tag/") {
			tags = append(tags, p)
		} else if strings.HasPrefix(p, "/id/") {
			identifiers = append(identifiers, p)
		} else if strings.HasPrefix(p, "/industry/") {
			industry = append(industry, p)
		} else if strings.HasPrefix(p, "/entity/") {
			entity = append(entity, p)
		} else {
			other = append(other, p)
		}
	}

	return NormalizeIndexPaths(MaxIndexPaths, canonical, location, spheres, industry, segments, entity, identifiers, tags, other)
}

// NormalizeIndexPaths deduplicates, normalizes, and caps index paths deterministically.
func NormalizeIndexPaths(capN int, buckets ...[]string) []string {
	if capN <= 0 {
		capN = MaxIndexPaths
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, minInt(capN, 32))

	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		// Keep paths in canonical-ish format.
		if !strings.HasPrefix(p, "/") {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Within each bucket, keep stable ordering (lexicographic), then append in priority order.
	for _, b := range buckets {
		if len(out) >= capN {
			break
		}
		if len(b) == 0 {
			continue
		}
		cp := make([]string, 0, len(b))
		for _, x := range b {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			cp = append(cp, x)
		}
		sort.Strings(cp)
		for _, p := range cp {
			if len(out) >= capN {
				break
			}
			add(p)
		}
	}

	return out
}

// MergeIndexPaths merges existing index paths with extra ones using the same cap and normalization rules.
func MergeIndexPaths(fragment *HorizonFragment, extra []string) []string {
	if fragment == nil {
		return nil
	}
	// Rebuild from fragment fields (ensures canonical/core coordinates are present), then add existing+extra.
	more := make([]string, 0, len(fragment.IndexPaths)+len(extra))
	more = append(more, fragment.IndexPaths...)
	more = append(more, extra...)
	return BuildIndexPaths(fragment, more)
}

func prefixedWithAncestors(prefix, rawPath string, maxDepth int) []string {
	prefix = strings.TrimSpace(prefix)
	rawPath = strings.TrimSpace(rawPath)
	if prefix == "" || !strings.HasPrefix(prefix, "/") || rawPath == "" {
		return nil
	}

	// Ensure rawPath looks like "/A/B/C".
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) == 0 {
		return nil
	}
	if maxDepth <= 0 {
		maxDepth = 6
	}
	if len(parts) > maxDepth {
		parts = parts[:maxDepth]
	}

	out := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		out = append(out, prefix+"/"+strings.Join(parts[:i], "/"))
	}
	return out
}

// withAncestors expands a canonical path like "/industry/a/b/c" into:
// ["/industry/a", "/industry/a/b", "/industry/a/b/c"] up to maxDepth segments.
func withAncestors(path string, maxDepth int) []string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return nil
	}
	if maxDepth <= 0 {
		maxDepth = 6
	}
	if len(parts) > maxDepth {
		parts = parts[:maxDepth]
	}
	out := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		out = append(out, "/"+strings.Join(parts[:i], "/"))
	}
	return out
}

func buildLocationIndexPaths(f *HorizonFragment) []string {
	if f == nil {
		return nil
	}

	cc := strings.ToLower(strings.TrimSpace(f.GeoSlug))
	if cc == "" && f.HomeLocation != nil && f.HomeLocation.ISO3166_1 != nil {
		cc = strings.ToLower(strings.TrimSpace(*f.HomeLocation.ISO3166_1))
	}
	if cc == "" {
		return nil
	}

	admin1 := "na"
	if f.HomeLocation != nil && f.HomeLocation.ISO3166_2 != nil {
		iso := strings.TrimSpace(*f.HomeLocation.ISO3166_2)
		// Common format: "US-CA" → admin1 = "ca"
		if idx := strings.Index(iso, "-"); idx >= 0 && idx+1 < len(iso) {
			admin1 = strings.ToLower(strings.TrimSpace(iso[idx+1:]))
		}
	}
	admin1 = slugToken(admin1)
	if admin1 == "" {
		admin1 = "na"
	}

	out := []string{"/loc/" + cc}
	if admin1 != "na" {
		out = append(out, "/loc/"+cc+"/"+admin1)
	}
	return out
}

func slugToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Keep only [a-z0-9-], replace separators with '-'.
	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		// Treat everything else as separator.
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
