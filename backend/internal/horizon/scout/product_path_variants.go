package scout

import (
	"context"
	"strings"
)

func productLeafSlugFromPath(path string) string {
	cp := strings.TrimSpace(path)
	if !strings.HasPrefix(cp, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 || parts[0] != "product" {
		return ""
	}
	slug := strings.TrimSpace(parts[len(parts)-1])
	if slug == "" {
		return ""
	}
	return slug
}

func productPathShapeScore(path string) int {
	cp := strings.TrimSpace(path)
	if !strings.HasPrefix(cp, "/product/") {
		return 0
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 || parts[0] != "product" {
		return 0
	}
	// Preferred canonical shapes:
	// - /product/{category}/{type}/{stage}/{slug}
	// - /product/service/{domain}/{subdomain}/{slug}
	if len(parts) == 5 {
		return 40
	}
	// Flat fallback: /product/{slug}
	if len(parts) == 2 {
		return 20
	}
	// Legacy org-scoped fallback: /product/{orgSlug}/{productSlug}
	if len(parts) == 3 {
		return 10
	}
	// Unknown/malformed product shapes.
	return 1
}

func productCanonicalPathVariants(ctx context.Context, canonicalPath string, nameHints ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if !strings.HasPrefix(path, "/product/") {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	base := strings.TrimSpace(canonicalPath)
	add(base)

	slug := productLeafSlugFromPath(base)
	if slug != "" {
		flat := "/product/" + slug
		add(flat)
		if norm := normalizeFlatProductPath(ctx, flat); strings.TrimSpace(norm) != "" {
			add(norm)
		}
	}

	canonicalizer := NewProductCanonicalizer()
	for _, hint := range nameHints {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if res, err := canonicalizer.CanonicalizeProduct(ctx, hint); err == nil && res != nil {
			add(strings.TrimSpace(res.CanonicalPath))
		}
	}

	// Deterministic order: keep strongest canonical shape first.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 {
			left := out[j-1]
			right := out[j]
			leftScore := productPathShapeScore(left)
			rightScore := productPathShapeScore(right)
			if rightScore < leftScore {
				break
			}
			if rightScore == leftScore && right >= left {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}

	return out
}

func normalizeProductPathForResolve(ctx context.Context, canonicalPath string) string {
	in := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(in, "/product/") {
		return ""
	}
	variants := productCanonicalPathVariants(ctx, in)
	if len(variants) == 0 {
		return ""
	}
	best := variants[0]
	if strings.TrimSpace(best) == "" || best == in {
		return ""
	}
	return best
}
