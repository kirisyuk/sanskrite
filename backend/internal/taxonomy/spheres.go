package taxonomy

import (
	"errors"
	"strings"
)

// SphereL1Allowlist is the closed list of L1 (top-level) sphere domains.
// L1 segments must be in this list; unknown L1 maps to /sphere/other.
var SphereL1Allowlist = []string{
	"defense", "finance", "energy", "trade", "technology", "logistics", "health",
	"legal", "governance", "security", "environment", "social", "infrastructure",
	"ai", "geopolitics", "policy", "operations", "labor", "digital", "other",
}

// SphereL2Map defines controlled L2 subdomains per L1. Unknown L2 maps to /sphere/{L1}/other.
// L3+ segments are free-form (slugified, max 5 total segments).
var SphereL2Map = map[string][]string{
	"defense":        {"naval", "air", "land", "cyber", "space", "procurement", "intelligence", "other"},
	"finance":        {"markets", "crypto", "credit", "liquidity", "fx", "insurance", "banking", "other"},
	"energy":         {"oil-gas", "power", "renewables", "mining", "markets", "other"},
	"trade":          {"customs", "tariffs", "supply-chain", "other"},
	"technology":     {"semiconductors", "consumer-electronics", "enterprise", "research", "it", "digital", "telecom", "other"},
	"logistics":      {"maritime", "air", "rail", "road", "ports", "other"},
	"health":         {"pandemic", "pharma", "medical", "other"},
	"legal":          {"litigation", "ip", "compliance", "aml", "anti-corruption", "other"},
	"governance":     {"corporate", "political", "regulatory", "other"},
	"security":       {"physical", "cyber", "crime", "terrorism", "other"},
	"environment":    {"climate", "pollution", "other"},
	"social":         {"information", "religious-affairs", "education", "disinformation", "propaganda", "censorship", "other"},
	"infrastructure": {"telecom", "cloud", "power", "other"},
	"ai":             {"models", "policy", "research", "applications", "other"},
	"geopolitics":    {"conflict", "diplomacy", "sanctions", "other"},
	"policy":         {"regulation", "trade", "sanctions", "export-controls", "customs", "tax", "licensing", "industrial-policy", "other"},
	"operations":     {"procurement", "manufacturing", "quality", "safety", "reliability", "capacity", "maintenance", "continuity", "other"},
	"labor":          {"strikes", "union", "other"},
	"digital":        {},
	"other":          {},
}

// sphereL1Set is built from SphereL1Allowlist for O(1) lookup.
var sphereL1Set = func() map[string]struct{} {
	s := make(map[string]struct{}, len(SphereL1Allowlist))
	for _, v := range SphereL1Allowlist {
		s[v] = struct{}{}
	}
	return s
}()

// sphereL2Set returns a set of valid L2 values for the given L1.
func sphereL2Set(l1 string) map[string]struct{} {
	list, ok := SphereL2Map[l1]
	if !ok || len(list) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(list))
	for _, v := range list {
		s[v] = struct{}{}
	}
	return s
}

// AllSpherePaths generates the full list of canonical L1+L2 sphere paths from
// SphereL1Allowlist and SphereL2Map. Use this instead of maintaining a separate
// hardcoded list — single source of truth.
func AllSpherePaths() []string {
	var out []string
	for _, l1 := range SphereL1Allowlist {
		out = append(out, "/sphere/"+l1)
		if l2s, ok := SphereL2Map[l1]; ok {
			for _, l2 := range l2s {
				out = append(out, "/sphere/"+l1+"/"+l2)
			}
		}
	}
	return out
}

// legacyL1ToPath maps legacy single-segment sphere paths to canonical L1/L2 paths.
// Used when input is /sphere/{x} and x is not in L1 allowlist but is a known alias.
var legacyL1ToPath = map[string]string{
	"cyber":      "/sphere/security/cyber",
	"digital":    "/sphere/technology/digital",
	"esg":        "/sphere/environment",
	"workforce":  "/sphere/labor",
	"compliance": "/sphere/legal/compliance",
}

// ValidateSpherePath validates a sphere path against L1/L2 rules.
// L1 must be in allowlist; L2 (if present) must be in L2 map for that L1.
// L3+ are accepted as-is (caller should slugify).
func ValidateSpherePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/sphere/") {
		return errors.New("sphere path must be non-empty and start with /sphere/")
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sphere" {
		return errors.New("invalid sphere path format")
	}
	if len(parts) > 5 {
		return errors.New("sphere path has at most 5 segments")
	}
	l1 := Slugify(parts[1])
	if l1 == "" {
		return errors.New("L1 segment is empty after slugify")
	}
	if _, ok := sphereL1Set[l1]; !ok {
		return errors.New("L1 segment not in allowlist: " + l1)
	}
	if len(parts) >= 3 {
		l2 := Slugify(parts[2])
		if l2 != "" {
			if l2Set := sphereL2Set(l1); len(l2Set) > 0 {
				if _, ok := l2Set[l2]; !ok {
					// L2 not in allowlist -> will normalize to /sphere/{L1}/other
					// Don't return error; NormalizeSpherePath handles gracefully
				}
			}
		}
	}
	return nil
}

// NormalizeSpherePath canonicalizes a single sphere path using L1/L2/L3+ rules.
// - L1 not in allowlist -> /sphere/other
// - L1 valid but L2 not in L2 map -> /sphere/{L1}/other
// - L3+ always accepted (slugified)
func NormalizeSpherePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/sphere/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sphere" {
		return ""
	}
	seg := make([]string, 0, 5)
	seg = append(seg, "sphere")
	for i, s := range parts[1:] {
		slug := Slugify(s)
		if slug == "" {
			continue
		}
		switch i {
		case 0:
			if _, ok := sphereL1Set[slug]; !ok {
				if mapped, ok := legacyL1ToPath[slug]; ok {
					return NormalizeSpherePath(mapped)
				}
				slug = "other"
			}
		case 1:
			l1 := seg[1]
			if l2Set := sphereL2Set(l1); len(l2Set) > 0 {
				if _, ok := l2Set[slug]; !ok {
					slug = "other"
				}
			}
		}
		seg = append(seg, slug)
		if len(seg) >= 5 {
			break
		}
	}
	if len(seg) < 2 {
		return ""
	}
	return "/" + strings.Join(seg, "/")
}

// NormalizeSpherePaths canonicalizes sphere paths into the single, unified taxonomy namespace:
//
//	/sphere/{L1}/{L2}/{L3...}
//
// L1: closed allowlist. L2: controlled per L1. L3+: free-form (slugified, max 5 segments).
// Inputs may be: canonical paths or short labels (resolved via SphereSynonyms).
// Graceful degradation: unknown L1 -> /sphere/other; unknown L2 -> /sphere/{L1}/other.
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
			p = SpherePath(p)
		}
		if strings.HasPrefix(p, "/sphere/") {
			p = NormalizeSpherePath(p)
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
		return "/sphere/security/cyber"
	case "digital":
		return "/sphere/technology/digital"
	case "it", "tech":
		return "/sphere/technology"
	case "logistics", "supply-chain", "supply_chain":
		return "/sphere/logistics"
	case "maritime", "shipping", "sea":
		return "/sphere/logistics/maritime"
	case "air", "airfreight", "air-freight":
		return "/sphere/logistics/air"
	case "rail":
		return "/sphere/logistics/rail"
	case "road", "trucking", "truck":
		return "/sphere/logistics/road"
	case "port", "ports":
		return "/sphere/logistics/ports"
	case "energy", "oil", "gas":
		return "/sphere/energy"
	case "finance", "markets", "banking":
		return "/sphere/finance"
	case "crypto", "cryptocurrency", "tokenization", "tokenisation":
		return "/sphere/finance/crypto"
	case "credit":
		return "/sphere/finance/credit"
	case "liquidity":
		return "/sphere/finance/liquidity"
	case "fx", "foreign-exchange", "foreign_exchange":
		return "/sphere/finance/fx"
	case "insurance":
		return "/sphere/finance/insurance"
	case "ops", "operations":
		return "/sphere/operations"
	case "procurement":
		return "/sphere/operations/procurement"
	case "manufacturing", "production", "factory":
		return "/sphere/operations/manufacturing"
	case "quality", "qa", "qc":
		return "/sphere/operations/quality"
	case "safety", "hse", "ehs":
		return "/sphere/operations/safety"
	case "maintenance", "downtime":
		return "/sphere/operations/maintenance"
	case "continuity", "bcp", "business-continuity", "business_continuity":
		return "/sphere/operations/continuity"
	case "workforce", "hr", "talent":
		return "/sphere/labor"
	case "labor", "labour", "union":
		return "/sphere/labor"
	case "strike", "strikes":
		return "/sphere/labor/strikes"
	case "security", "physical-security", "physical_security":
		return "/sphere/security"
	case "crime", "theft":
		return "/sphere/security/crime"
	case "terror", "terrorism":
		return "/sphere/security/terrorism"
	case "legal", "law":
		return "/sphere/legal"
	case "litigation", "lawsuit":
		return "/sphere/legal/litigation"
	case "ip", "patent", "copyright", "trademark":
		return "/sphere/legal/ip"
	case "compliance":
		return "/sphere/legal/compliance"
	case "aml", "kyc":
		return "/sphere/legal/aml"
	case "corruption", "bribery", "anti-corruption", "anti_corruption":
		return "/sphere/legal/anti-corruption"
	case "export-controls", "export_controls", "export control", "export controls", "dual-use", "dual use":
		return "/sphere/policy/export-controls"
	case "tariff", "customs":
		return "/sphere/policy/customs"
	case "tax":
		return "/sphere/policy/tax"
	case "license", "licensing":
		return "/sphere/policy/licensing"
	case "industrial-policy", "industrial policy":
		return "/sphere/policy/industrial-policy"
	case "environment":
		return "/sphere/environment"
	case "climate", "carbon", "co2":
		return "/sphere/environment/climate"
	case "pollution", "emissions":
		return "/sphere/environment/pollution"
	case "esg", "sustainability":
		return "/sphere/environment"
	case "governance", "corporate-governance", "corporate governance":
		return "/sphere/governance"
	case "health", "public-health", "public health":
		return "/sphere/health"
	case "pandemic", "covid":
		return "/sphere/health/pandemic"
	case "infrastructure":
		return "/sphere/infrastructure"
	case "telecom", "internet":
		return "/sphere/infrastructure/telecom"
	case "cloud", "datacenter", "data-center", "data center":
		return "/sphere/infrastructure/cloud"
	case "technology", "semiconductors":
		return "/sphere/technology"
	case "conflict", "war":
		return "/sphere/geopolitics/conflict"
	}
	// Open-world: /sphere/{slugified label}
	return JoinPath("sphere", l)
}
