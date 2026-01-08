package taxonomy

import (
	"regexp"
	"strings"
	"sync"
)

var reProductNonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
var reProductMultiDash = regexp.MustCompile(`-+`)

// RuntimeTaxonomy is loaded from Firestore and used as a single source of truth.
// Keep it small and operational: only what we must validate/normalize at runtime.
type RuntimeTaxonomy struct {
	Version string

	AllowedSpherePaths map[string]struct{} // "/sphere/..."
	SphereSynonyms     map[string]string   // label -> "/sphere/..."

	// Industry taxonomy for canonicalization (Horizon-first, /industry/...).
	// Used by conservative enrichment paths when a Wikidata QID is already known.
	AllowedIndustryPaths map[string]struct{} // "/industry/..."
	IndustrySynonyms     map[string]string   // label -> "/industry/..." (lowercased)
	IndustryWikidataMap  map[string]string   // "Q..." -> "/industry/..."

	AllowedEventTypes map[string]struct{} // "attack", "sanction", "other", ...
	EventTypeSynonyms map[string]string   // label -> allowed eventType

	// Relation types for horizon_dependency_edges (upper snake-case, e.g. "SUPPLIES_TO").
	AllowedRelationTypes map[string]struct{}

	// Signal types for OntologyFact(kind=signal)
	AllowedSignalKinds map[string]struct{}       // "risk", "opportunity", "trend", "direction"
	SignalTypes        map[string]SignalTypeSpec // "risk:supply_disruption" -> spec
	SignalTypeSynonyms map[string]string         // alias -> canonical type

	// Product taxonomy for canonicalization
	AllowedProductCategories map[string]struct{}    // "raw", "component", "finished", "service"
	AllowedProductTypes      map[string]struct{}    // "mineral", "agricultural", "chemical", "electronic", etc.
	AllowedProductStages     map[string]struct{}    // "extraction", "refining", "processing", "manufacturing", "assembly"
	ProductStageOrder        []string               // Ordered list for algorithmic upstream/downstream propagation
	KnownProducts            map[string]ProductSpec // "lithium-carbonate" -> spec
	ProductSynonyms          map[string]string      // "Li2CO3" -> "lithium-carbonate", "battery grade lithium" -> "lithium-carbonate"
}

// SignalTypeSpec defines a signal type specification.
type SignalTypeSpec struct {
	Kind               string `json:"kind"`               // "risk", "opportunity", "trend", "direction"
	Label              string `json:"label"`              // Human-readable label
	DefaultMetric      string `json:"defaultMetric"`      // Default metric name for rollup
	PropagationProfile string `json:"propagationProfile"` // "upstream_amplified", "downstream_amplified", "jurisdiction_scoped", etc.
}

var rtMu sync.RWMutex
var rt *RuntimeTaxonomy

// builtInAllowedRelationTypes is the compiled fallback allow-list for relation types.
//
// Rationale:
// - Runtime taxonomy can drift (e.g., you deploy code with new edge types but forget to reseed Firestore).
// - We still want to materialize edges using types that the codebase understands, otherwise everything collapses to INFLUENCES.
// - Runtime taxonomy can still tighten unknown values (anything not in this set remains restricted by AllowedRelationTypes when present).
var builtInAllowedRelationTypes = map[string]struct{}{
	"SUPPLIES_TO":     {},
	"LOCATED_IN":      {},
	"OWNS":            {},
	"SUBSIDIARY_OF":   {},
	"PRODUCES":        {},
	"CONSUMES":        {},
	"OPERATES_IN":     {},
	"REGULATES":       {},
	"COMPETES_WITH":   {},
	"INFLUENCES":      {},
	"DEPENDS_ON":      {},
	"TRANSFORMS_INTO": {},
	// Governance / org-person
	"MANAGES":         {},
	"CONTROLS":        {},
	"EMPLOYED_BY":     {},
	"BOARD_MEMBER_OF": {},
	"FOUNDED":         {},
	"INVESTS_IN":      {},
	"PARTNERS_WITH":   {},
	// People/org meta-relations
	"KEY_PERSON": {},
	// OSINT-lite / governance extensions
	"AUDITED_BY":  {},
	"RELATIVE_OF": {},
	"RELATED_TO":  {},
}

func isBuiltInAllowedRelationType(s string) bool {
	_, ok := builtInAllowedRelationTypes[s]
	return ok
}

// SetRuntimeTaxonomy installs the runtime taxonomy (usually loaded from Firestore at startup).
func SetRuntimeTaxonomy(t *RuntimeTaxonomy) {
	rtMu.Lock()
	defer rtMu.Unlock()
	rt = t
}

func getRuntimeTaxonomy() *RuntimeTaxonomy {
	rtMu.RLock()
	defer rtMu.RUnlock()
	return rt
}

// GetRuntimeTaxonomy returns the current runtime taxonomy snapshot pointer.
// Callers must treat it as read-only.
func GetRuntimeTaxonomy() *RuntimeTaxonomy {
	return getRuntimeTaxonomy()
}

// MapIndustryQIDsToPaths maps Wikidata industry Q-IDs (P452) to canonical /industry/... paths
// using the runtime taxonomy dictionary (IndustryWikidataMap).
// Unknown Q-IDs are skipped (conservative behavior).
func MapIndustryQIDsToPaths(qids []string) []string {
	t := getRuntimeTaxonomy()
	if t == nil || t.IndustryWikidataMap == nil || len(t.IndustryWikidataMap) == 0 || len(qids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(qids))
	for _, q := range qids {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		p, ok := t.IndustryWikidataMap[q]
		if !ok {
			continue
		}
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "/industry/") {
			continue
		}
		// Optional allow-list enforcement: if allow-list is present, keep only allowed.
		// Accept exact match OR when any ancestor is allowed (so allow-list can be sector-level).
		if len(t.AllowedIndustryPaths) > 0 {
			if _, ok2 := t.AllowedIndustryPaths[p]; !ok2 {
				// Check ancestors: /industry/a/b/c -> /industry/a/b -> /industry/a -> /industry
				parts := strings.Split(strings.Trim(p, "/"), "/")
				allowed := false
				for i := len(parts); i >= 2; i-- { // keep at least "industry/<x>"
					anc := "/" + strings.Join(parts[:i], "/")
					if _, ok3 := t.AllowedIndustryPaths[anc]; ok3 {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeEventType returns a stable eventType string.
// Unknown/unsupported values are mapped to "other".
func NormalizeEventType(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return "other"
	}
	if t := getRuntimeTaxonomy(); t != nil {
		if t.EventTypeSynonyms != nil {
			if v, ok := t.EventTypeSynonyms[s]; ok && strings.TrimSpace(v) != "" {
				s = strings.ToLower(strings.TrimSpace(v))
			}
		}
		if len(t.AllowedEventTypes) > 0 {
			if _, ok := t.AllowedEventTypes[s]; ok {
				return s
			}
			return "other"
		}
	}
	// Fallback: allow small stable set, else other.
	switch s {
	case "attack", "cyberattack", "sanction", "blockade", "policy_change", "license_change", "partnership", "acquisition", "release", "deprecation", "pricing_change", "availability_change", "capability_change", "protest", "strike", "election", "warning", "announcement", "incident", "other":
		return s
	default:
		return "other"
	}
}

// NormalizeRelationType returns a stable Horizon dependency edge type (uppercase string).
// Unknown/unsupported values are mapped to "INFLUENCES" (safe default).
func NormalizeRelationType(in string) string {
	s := strings.ToUpper(strings.TrimSpace(in))
	if s == "" {
		return "INFLUENCES"
	}
	// Normalize legacy / common aliases.
	switch s {
	case "AUDITOR", "AUDITORS", "AUDITOR_OF", "AUDITED", "AUDIT_FIRM":
		return "AUDITED_BY"
	case "RELATIVE", "RELATIVES", "FAMILY_OF", "FAMILY_MEMBER_OF", "KIN_OF":
		return "RELATIVE_OF"
	case "RELATED", "ASSOCIATED_WITH", "ASSOCIATE_OF", "FRIEND_OF", "BUSINESS_PARTNER_OF", "CLASSMATE_OF", "ALUMNI_OF":
		return "RELATED_TO"
	case "SUBSIDIARY_OF", "OWNED_BY":
		return "SUBSIDIARY_OF"
	case "CONTROLLED_BY":
		return "CONTROLS"
	case "MANAGED_BY":
		return "MANAGES"
	case "EMPLOYS", "WORKS_FOR":
		return "EMPLOYED_BY"
	case "KEYPERSON", "KEY_PERSON_OF", "HAS_KEY_PERSON":
		// Canonical: KEY_PERSON (direction is enforced elsewhere).
		return "KEY_PERSON"
	case "FOUNDER_OF":
		return "FOUNDED"
	case "BOARD_MEMBER", "BOARD_MEMBER_AT":
		return "BOARD_MEMBER_OF"
	case "INVESTED_IN":
		return "INVESTS_IN"
	case "DEPENDS":
		return "DEPENDS_ON"
	}
	if t := getRuntimeTaxonomy(); t != nil && len(t.AllowedRelationTypes) > 0 {
		if _, ok := t.AllowedRelationTypes[s]; ok {
			return s
		}
		// If runtime taxonomy is installed but missing a built-in relation type, prefer the built-in allow-list.
		// This prevents "new types collapsing to INFLUENCES" due to stale Firestore taxonomy.
		if isBuiltInAllowedRelationType(s) {
			return s
		}
		return "INFLUENCES"
	}
	// Fallback allow-list for local runs when runtime taxonomy isn't installed.
	if isBuiltInAllowedRelationType(s) {
		return s
	}
	return "INFLUENCES"
}

// NormalizeSignalType returns a stable signal type string.
// Unknown/unsupported values are mapped to "other" (fallback).
func NormalizeSignalType(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return "other"
	}
	if t := getRuntimeTaxonomy(); t != nil {
		// Check synonyms first
		if t.SignalTypeSynonyms != nil {
			if v, ok := t.SignalTypeSynonyms[s]; ok && strings.TrimSpace(v) != "" {
				s = strings.ToLower(strings.TrimSpace(v))
			}
		}
		// Validate against allowed types
		if len(t.SignalTypes) > 0 {
			if _, ok := t.SignalTypes[s]; ok {
				return s
			}
		}
		// If taxonomy is loaded but type not found, return "other"
		if len(t.AllowedSignalKinds) > 0 {
			return "other"
		}
	}
	// Fallback: allow common patterns
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) == 2 {
			kind := strings.ToLower(strings.TrimSpace(parts[0]))
			switch kind {
			case "risk", "opportunity", "trend", "direction":
				return s
			}
		}
	}
	return "other"
}

// GetSignalTypeSpec returns the specification for a signal type.
func GetSignalTypeSpec(signalType string) *SignalTypeSpec {
	if t := getRuntimeTaxonomy(); t != nil && t.SignalTypes != nil {
		if spec, ok := t.SignalTypes[signalType]; ok {
			return &spec
		}
	}
	return nil
}

// ProductSpec defines a product specification.
type ProductSpec struct {
	Category string `json:"category"` // "raw", "component", "finished", "service"
	Type     string `json:"type"`     // "mineral", "agricultural", "chemical", "electronic", etc.
	Stage    string `json:"stage"`    // "extraction", "refining", "processing", "manufacturing", "assembly"
}

// GetProductSpec returns the specification for a product slug.
func GetProductSpec(productSlug string) *ProductSpec {
	if t := getRuntimeTaxonomy(); t != nil && t.KnownProducts != nil {
		if spec, ok := t.KnownProducts[productSlug]; ok {
			return &spec
		}
	}
	return nil
}

// NormalizeProductName normalizes a product name to canonical slug using synonyms.
func NormalizeProductName(name string) string {
	raw := strings.ToLower(strings.TrimSpace(name))
	if raw == "" {
		return ""
	}
	if t := getRuntimeTaxonomy(); t != nil && t.ProductSynonyms != nil {
		// Try exact (lowercased) first (supports keys like "li2co3", "lithium carbonate").
		if canonical, ok := t.ProductSynonyms[raw]; ok && strings.TrimSpace(canonical) != "" {
			return normalizeProductSlug(canonical)
		}
		// Then try slugified key (supports keys like "lithium-carbonate").
		slugKey := normalizeProductSlug(raw)
		if canonical, ok := t.ProductSynonyms[slugKey]; ok && strings.TrimSpace(canonical) != "" {
			return normalizeProductSlug(canonical)
		}
	}
	return normalizeProductSlug(raw)
}

// IsValidProductCategory checks if a category is valid.
func IsValidProductCategory(category string) bool {
	if category == "" {
		return false
	}
	if t := getRuntimeTaxonomy(); t != nil && t.AllowedProductCategories != nil {
		if _, ok := t.AllowedProductCategories[strings.ToLower(category)]; ok {
			return true
		}
	}
	return false
}

// IsValidProductType checks if a type is valid.
func IsValidProductType(productType string) bool {
	if productType == "" {
		return false
	}
	if t := getRuntimeTaxonomy(); t != nil && t.AllowedProductTypes != nil {
		if _, ok := t.AllowedProductTypes[strings.ToLower(productType)]; ok {
			return true
		}
	}
	return false
}

// IsValidProductStage checks if a stage is valid.
func IsValidProductStage(stage string) bool {
	if stage == "" {
		return false
	}
	if t := getRuntimeTaxonomy(); t != nil && t.AllowedProductStages != nil {
		if _, ok := t.AllowedProductStages[strings.ToLower(stage)]; ok {
			return true
		}
	}
	return false
}

// GetProductStageOrder returns the ordered list of product stages for propagation.
func GetProductStageOrder() []string {
	if t := getRuntimeTaxonomy(); t != nil && t.ProductStageOrder != nil {
		return t.ProductStageOrder
	}
	return []string{"extraction", "refining", "processing", "manufacturing", "assembly"}
}

func normalizeProductSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = reProductNonSlugChars.ReplaceAllString(s, "-")
	s = reProductMultiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
