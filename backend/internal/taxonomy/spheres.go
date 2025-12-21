package taxonomy

import "strings"

// NormalizeSpherePaths canonicalizes sphere paths into the single, unified taxonomy namespace:
//
//	/sphere/{domain}/{subdomain...}
//
// Inputs may be:
// - already canonical paths ("/sphere/cyber", "/sphere/logistics/maritime")
// - short labels ("cyber", "geopolitics", "social", "information")
//
// The function is conservative: empty/invalid inputs are dropped, results are deduplicated.
func NormalizeSpherePaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))

	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || p == "/" {
			return
		}
		if !strings.HasPrefix(p, "/sphere/") {
			// Treat as label; resolve via runtime taxonomy if available.
			if t := getRuntimeTaxonomy(); t != nil && t.SphereSynonyms != nil {
				key := strings.ToLower(strings.TrimSpace(p))
				if v, ok := t.SphereSynonyms[key]; ok && strings.TrimSpace(v) != "" {
					p = strings.TrimSpace(v)
				} else {
					p = SpherePath(p)
				}
			} else {
				p = SpherePath(p)
			}
		}
		if strings.HasPrefix(p, "/sphere/") {
			// Normalize segments
			parts := strings.Split(strings.Trim(p, "/"), "/")
			if len(parts) < 2 || parts[0] != "sphere" {
				return
			}
			seg := make([]string, 0, len(parts))
			seg = append(seg, "sphere")
			for _, s := range parts[1:] {
				s = Slugify(s)
				if s == "" {
					continue
				}
				seg = append(seg, s)
			}
			if len(seg) < 2 {
				return
			}
			p = "/" + strings.Join(seg, "/")
		}
		// Enforce allowlist if runtime taxonomy is installed.
		if t := getRuntimeTaxonomy(); t != nil && t.AllowedSpherePaths != nil && len(t.AllowedSpherePaths) > 0 {
			if _, ok := t.AllowedSpherePaths[p]; !ok {
				p = "/sphere/other"
			}
		}
		if p == "" || p == "/" {
			return
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, v := range in {
		add(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SpherePath maps a human label into a canonical sphere path.
// This is intentionally small and stable; callers may also provide full /sphere/... paths directly.
func SpherePath(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return ""
	}
	// Common synonyms
	switch l {
	case "ai", "ml", "machine-learning", "machine_learning", "machinelearning":
		return "/sphere/ai"
	case "llm", "foundation-models", "foundation_models", "foundationmodels":
		return "/sphere/ai/models"
	case "geo", "geopolitic", "geopolitical", "geopolitics":
		return "/sphere/geopolitics"
	case "policy", "regulation", "regulatory":
		return "/sphere/policy"
	case "social", "society", "socium":
		return "/sphere/social"
	case "info", "information", "information-space", "information_space", "media":
		return "/sphere/social/information"
	case "cyber", "cybersecurity", "infosec":
		return "/sphere/cyber"
	case "digital", "it", "tech":
		return "/sphere/digital"
	case "logistics", "supply-chain", "supply_chain":
		return "/sphere/logistics"
	case "maritime", "shipping", "sea":
		return "/sphere/logistics/maritime"
	case "energy", "oil", "gas":
		return "/sphere/energy"
	case "finance", "markets", "banking":
		return "/sphere/finance"
	case "crypto", "cryptocurrency", "tokenization", "tokenisation":
		return "/sphere/finance/crypto"
	}
	// Open-world: /sphere/{slugified label}
	return JoinPath("sphere", l)
}
