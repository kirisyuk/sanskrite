package taxonomy

import (
	"regexp"
	"strings"
	"sync"
)

var reProductNonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
var reProductMultiDash = regexp.MustCompile(`-+`)

// RuntimeTaxonomy contains optional runtime dictionaries loaded from Firestore.
// IMPORTANT:
// - Validation/normalization contracts are code-defined (see runtime_contracts.go).
// - Runtime taxonomy is used only for enrichment dictionaries and non-contract metadata.
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

	// Metric units allow-list (used as a hint for prompts/validation).
	// IMPORTANT: Currency and index-like units are intentionally NOT allow-listed.
	AllowedMetricUnits map[string]struct{}

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

	// Natural resource deposits (/deposit/{resource-slug}/{deposit-slug})
	AllowedDepositResources map[string]struct{} // "crude-oil", "lithium", "copper", etc.

	// Lifecycle stages for FACILITY, DEPOSIT, ORGANIZATION
	AllowedLifecycleStages map[string]struct{} // "construction", "operational", "closed", etc.
	LifecycleStageAliases  map[string]string   // "exploration" -> "concept", "production" -> "operational"

	// Edge metrics (numerical data on edges)
	AllowedEdgeMetrics    map[string][]string // edge type -> allowed metric names
	AllowedEdgeModalities map[string]struct{} // "actual", "projected", "historical", "potential"

	// Service taxonomy (/product/service/{domain}/{subdomain}/{slug})
	AllowedServiceDomains    map[string]struct{}    // "it", "financial", "legal", etc.
	AllowedServiceSubdomains map[string][]string    // domain -> subdomains
	KnownServices            map[string]ServiceSpec // "custom-software-development" -> spec
	ServiceSynonyms          map[string]string      // "software development" -> "custom-software-development"

	// Optional: product models (for specific SKU/model tracking)
	KnownProductModels map[string]ProductModelSpec // "cat-390f" -> spec

	// --- Market segment normalization (Firestore-backed dictionary) ---
	SegmentChannelDomains   map[string]struct{}        // domain values treated as "channel" (b2b/b2c/public-sector/etc)
	SegmentDomainAliases    map[string]string          // domain -> normalized domain (e.g. "gov" -> "public-sector")
	SegmentVerticalAliases  map[string]string          // vertical -> normalized vertical (e.g. "retail-banking" -> "banking")
	SegmentVerticalFamilies map[string]string          // vertical -> normalized domain (family), used when domain is channel-like
	SegmentNormRules        map[string]SegmentNormRule // explicit overrides keyed by "/segment/{domain}/{vertical}"
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

// builtInAllowedRelationTypes is the code-defined allow-list for relation contracts.
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
	"AFFECTS":         {},
	"TRANSFORMS_INTO": {},
	"EXTRACTS_FROM":   {}, // FACILITY → DEPOSIT
	// Market participation
	"PARTICIPATES_IN": {},
	"DOMINATES":       {},
	// Market hierarchy
	"HAS_MARKET_SEGMENT": {},
	// Government hierarchy
	"ADMINISTERS": {},
	// Governance / org-person
	"MANAGES":         {},
	"CONTROLS":        {},
	"CONTROLS_ACCESS": {},
	"EMPLOYED_BY":     {},
	"BOARD_MEMBER_OF": {},
	"FOUNDED":         {},
	"INVESTS_IN":      {},
	"PARTNERS_WITH":   {},
	// Strategic decision layer
	"IMPOSES_CONDITION":  {},
	"GATEKEEPS_DECISION": {},
	"USES_MECHANISM":     {},
	"APPLIES_TO":         {},
	// Domain-to-strategic entities (Phase 2 overlay)
	"SURFACES": {},
	// People/org meta-relations
	"KEY_PERSON": {},
	// Assurance / oversight (Third Line)
	"ASSURES": {},
	// OSINT-lite / governance extensions
	"AUDITED_BY":  {},
	"RELATIVE_OF": {},
	"RELATED_TO":  {},
	// Community relations
	"OPERATES_VIA":    {},
	"MEMBER_OF":       {},
	"AFFILIATED_WITH": {},
	"HAS_SUBGROUP":    {},
	// Corporate transitions
	"RENAMED_TO":         {},
	"REBRANDED_TO":       {},
	"LEGAL_SUCCESSOR_OF": {},
	"MERGED_INTO":        {},
	"SPUN_OFF_FROM":      {},
	"ACQUIRED_BY":        {},
	// Domain contracts
	"FOCUSES_ON":     {},
	"INVOLVES_ACTOR": {},
	"IN_GEO_SCOPE":   {},
	"ABOUT":          {},
	"RELATED_DOMAIN": {},
	"BROADENS_TO":    {},
	"NARROWS_TO":     {},
	// Legacy types (for migration compatibility)
	"WORKS_FOR":      {},
	"HAS_KEY_PERSON": {},
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
// using the code-defined industry contract dictionary.
// Unknown Q-IDs are skipped (conservative behavior).
func MapIndustryQIDsToPaths(qids []string) []string {
	if len(builtInIndustryWikidataMap) == 0 || len(qids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(qids))
	for _, q := range qids {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		p, ok := builtInIndustryWikidataMap[q]
		if !ok {
			continue
		}
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "/industry/") {
			continue
		}
		// Optional allow-list enforcement: if allow-list is present, keep only allowed.
		// Accept exact match OR when any ancestor is allowed (so allow-list can be sector-level).
		if len(builtInAllowedIndustryPaths) > 0 {
			if _, ok2 := builtInAllowedIndustryPaths[p]; !ok2 {
				// Check ancestors: /industry/a/b/c -> /industry/a/b -> /industry/a -> /industry
				parts := strings.Split(strings.Trim(p, "/"), "/")
				allowed := false
				for i := len(parts); i >= 2; i-- { // keep at least "industry/<x>"
					anc := "/" + strings.Join(parts[:i], "/")
					if _, ok3 := builtInAllowedIndustryPaths[anc]; ok3 {
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
	if v, ok := builtInEventTypeSynonyms[s]; ok && strings.TrimSpace(v) != "" {
		s = strings.ToLower(strings.TrimSpace(v))
	}
	if _, ok := builtInAllowedEventTypes[s]; ok {
		return s
	}
	return "other"
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
	case "ASSURED_BY", "CERTIFIED_BY", "CERTIFIES", "INSPECTED_BY", "INSPECTS":
		return "ASSURES"
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
	case "RENAMED", "RENAMED_AS", "NAME_CHANGED_TO", "NAME_CHANGE", "RENAME_TO":
		return "RENAMED_TO"
	case "REBRANDED", "REBRAND", "REBRANDED_AS", "BRAND_CHANGED_TO":
		return "REBRANDED_TO"
	case "SUCCESSOR_OF", "LEGAL_SUCCESSOR", "SUCCEEDED_BY", "SUCCEEDS", "SUCCESSOR":
		return "LEGAL_SUCCESSOR_OF"
	case "MERGED", "MERGED_TO", "MERGED_WITH", "MERGER_INTO":
		return "MERGED_INTO"
	case "SPINOFF_FROM", "SPIN_OFF_FROM", "SPUN_OUT_OF", "CARVED_OUT_FROM", "DEMERGED_FROM":
		return "SPUN_OFF_FROM"
	case "ACQUIRED", "BOUGHT_BY", "TAKEN_OVER_BY", "ACQUISITION_BY":
		return "ACQUIRED_BY"
	}
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
	if v, ok := builtInSignalTypeSynonyms[s]; ok && strings.TrimSpace(v) != "" {
		s = strings.ToLower(strings.TrimSpace(v))
	}
	if _, ok := builtInSignalTypes[s]; ok {
		return s
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) == 2 {
			kind := strings.ToLower(strings.TrimSpace(parts[0]))
			if _, ok := builtInAllowedSignalKinds[kind]; ok {
				return s
			}
		}
	}
	return "other"
}

// GetSignalTypeSpec returns the specification for a signal type.
func GetSignalTypeSpec(signalType string) *SignalTypeSpec {
	if spec, ok := builtInSignalTypes[strings.ToLower(strings.TrimSpace(signalType))]; ok {
		cp := spec
		return &cp
	}
	return nil
}

// ProductSpec defines a product specification.
type ProductSpec struct {
	Category string `json:"category"` // "raw", "component", "finished", "service"
	Type     string `json:"type"`     // "mineral", "agricultural", "chemical", "electronic", etc.
	Stage    string `json:"stage"`    // "extraction", "refining", "processing", "manufacturing", "assembly"
}

// ServiceSpec defines a service specification for /product/service/{domain}/{subdomain}/{slug}.
type ServiceSpec struct {
	Domain    string `json:"domain"`    // "it", "financial", "legal", etc.
	Subdomain string `json:"subdomain"` // "software", "cloud", "banking", etc.
}

// ProductModelSpec defines a specific product model/SKU.
type ProductModelSpec struct {
	GenericSlug  string `json:"genericSlug"`  // parent product slug (e.g., "excavator")
	Manufacturer string `json:"manufacturer"` // manufacturer org slug (e.g., "caterpillar")
}

// GetProductSpec returns the specification for a product slug.
func GetProductSpec(productSlug string) *ProductSpec {
	slug := strings.ToLower(strings.TrimSpace(productSlug))
	if slug == "" {
		return nil
	}
	if spec, ok := builtInKnownProducts[slug]; ok {
		cp := spec
		return &cp
	}
	return nil
}

// NormalizeProductName normalizes a product name to canonical slug using synonyms.
func NormalizeProductName(name string) string {
	raw := strings.ToLower(strings.TrimSpace(name))
	if raw == "" {
		return ""
	}
	// Try exact (lowercased) first (supports keys like "li2co3", "lithium carbonate").
	if canonical, ok := builtInProductSynonyms[raw]; ok && strings.TrimSpace(canonical) != "" {
		return normalizeProductSlug(canonical)
	}
	// Then try slugified key (supports keys like "lithium-carbonate").
	slugKey := normalizeProductSlug(raw)
	if canonical, ok := builtInProductSynonyms[slugKey]; ok && strings.TrimSpace(canonical) != "" {
		return normalizeProductSlug(canonical)
	}
	return normalizeProductSlug(raw)
}

// IsValidProductCategory checks if a category is valid.
func IsValidProductCategory(category string) bool {
	if category == "" {
		return false
	}
	cat := strings.ToLower(category)
	_, ok := builtInAllowedProductCategories[cat]
	return ok
}

// IsValidProductType checks if a type is valid.
func IsValidProductType(productType string) bool {
	if productType == "" {
		return false
	}
	pt := strings.ToLower(productType)
	_, ok := builtInAllowedProductTypes[pt]
	return ok
}

// IsValidProductStage checks if a stage is valid.
func IsValidProductStage(stage string) bool {
	if stage == "" {
		return false
	}
	st := strings.ToLower(stage)
	_, ok := builtInAllowedProductStages[st]
	return ok
}

// GetProductStageOrder returns the ordered list of product stages for propagation.
func GetProductStageOrder() []string {
	out := make([]string, len(builtInProductStageOrder))
	copy(out, builtInProductStageOrder)
	return out
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

// --- Deposit Resources ---

// IsValidDepositResource checks if a deposit resource slug is valid.
func IsValidDepositResource(resource string) bool {
	if resource == "" {
		return false
	}
	r := strings.ToLower(strings.TrimSpace(resource))
	_, ok := builtInAllowedDepositResources[r]
	return ok
}

// --- Lifecycle Stages ---

// NormalizeLifecycleStage normalizes a lifecycle stage to canonical form using aliases.
func NormalizeLifecycleStage(stage string) string {
	s := strings.ToLower(strings.TrimSpace(stage))
	if s == "" {
		return ""
	}
	if canonical, ok := builtInLifecycleStageAliases[s]; ok && canonical != "" {
		return canonical
	}
	if _, ok := builtInAllowedLifecycleStages[s]; ok {
		return s
	}
	return s // Preserve unknown token for downstream handling.
}

// IsValidLifecycleStage checks if a lifecycle stage is valid (canonical or alias).
func IsValidLifecycleStage(stage string) bool {
	s := strings.ToLower(strings.TrimSpace(stage))
	if s == "" {
		return false
	}
	if _, ok := builtInLifecycleStageAliases[s]; ok {
		return true
	}
	_, ok := builtInAllowedLifecycleStages[s]
	return ok
}

// --- Edge Modalities ---

// IsValidEdgeModality checks if an edge modality is valid.
func IsValidEdgeModality(modality string) bool {
	m := strings.ToLower(strings.TrimSpace(modality))
	if m == "" {
		return false
	}
	_, ok := builtInAllowedEdgeModalities[m]
	return ok
}

// --- Edge Metrics ---

// GetAllowedEdgeMetrics returns the allowed metric names for an edge type.
func GetAllowedEdgeMetrics(edgeType string) []string {
	et := strings.ToUpper(strings.TrimSpace(edgeType))
	if metrics, ok := builtInAllowedEdgeMetrics[et]; ok {
		out := make([]string, len(metrics))
		copy(out, metrics)
		return out
	}
	return nil
}

// IsValidEdgeMetric checks if a metric name is valid for a given edge type.
func IsValidEdgeMetric(edgeType, metricName string) bool {
	et := strings.ToUpper(strings.TrimSpace(edgeType))
	mn := strings.ToLower(strings.TrimSpace(metricName))
	if et == "" || mn == "" {
		return false
	}
	if metrics, ok := builtInAllowedEdgeMetrics[et]; ok {
		for _, m := range metrics {
			if strings.ToLower(m) == mn {
				return true
			}
		}
		return false
	}
	return false
}

// --- Service Taxonomy ---

// IsValidServiceDomain checks if a service domain is valid.
func IsValidServiceDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	_, ok := builtInAllowedServiceDomains[d]
	return ok
}

// IsValidServiceSubdomain checks if a service subdomain is valid for the given domain.
func IsValidServiceSubdomain(domain, subdomain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	s := strings.ToLower(strings.TrimSpace(subdomain))
	if d == "" || s == "" {
		return false
	}
	if subs, ok := builtInAllowedServiceSubdomains[d]; ok {
		for _, sub := range subs {
			if sub == s {
				return true
			}
		}
		return false
	}
	return s == "other"
}

// GetServiceSpec returns the specification for a service slug.
func GetServiceSpec(serviceSlug string) *ServiceSpec {
	slug := strings.ToLower(strings.TrimSpace(serviceSlug))
	if spec, ok := builtInKnownServices[slug]; ok {
		cp := spec
		return &cp
	}
	return nil
}

// NormalizeServiceName normalizes a service name to canonical slug using synonyms.
func NormalizeServiceName(name string) string {
	raw := strings.ToLower(strings.TrimSpace(name))
	if raw == "" {
		return ""
	}
	if canonical, ok := builtInServiceSynonyms[raw]; ok && canonical != "" {
		return normalizeProductSlug(canonical)
	}
	return normalizeProductSlug(raw)
}

// --- Unit Validation ---

// reCurrencyUnit matches pattern-based currency/index units: lowercase [a-z0-9_], len 2..32.
var reCurrencyUnit = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

// IsValidMetricUnit checks if a metric unit is valid.
// Physical units use allowlist; currency/index units use pattern matching.
func IsValidMetricUnit(unit string, isCurrencyMetric bool) bool {
	u := strings.ToLower(strings.TrimSpace(unit))
	if u == "" {
		return true // unit is optional
	}
	// For currency metrics, use pattern validation
	if isCurrencyMetric {
		return reCurrencyUnit.MatchString(u)
	}
	if _, ok := builtInAllowedMetricUnits[u]; ok {
		return true
	}
	// Fail-open for unknown physical units that still look like a token.
	return reCurrencyUnit.MatchString(u)
}

// IsCurrencyMetric returns true if the metric name typically uses currency units.
func IsCurrencyMetric(metricName string) bool {
	m := strings.ToLower(strings.TrimSpace(metricName))
	switch m {
	case "investment_amount", "round_size_total", "contract_value", "compensation", "revenue", "market_cap", "valuation":
		return true
	}
	return strings.Contains(m, "_amount") || strings.Contains(m, "_value") || strings.Contains(m, "_cost")
}
