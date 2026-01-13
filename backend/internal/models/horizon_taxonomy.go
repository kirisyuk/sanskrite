package models

import "time"

// HorizonTaxonomy is the single source-of-truth taxonomy document stored in Firestore.
// It is intentionally explicit (no inference) to keep ontology stable over time.
type HorizonTaxonomy struct {
	ID        string    `json:"id" firestore:"id"`
	Version   string    `json:"version" firestore:"version"` // e.g. "v1"
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Canonical namespaces/prefixes (Horizon-first).
	CanonicalPrefixes []string `json:"canonicalPrefixes,omitempty" firestore:"canonicalPrefixes,omitempty"`

	// Spheres of influence (/sphere/...)
	AllowedSpherePaths []string          `json:"allowedSpherePaths,omitempty" firestore:"allowedSpherePaths,omitempty"`
	SphereSynonyms     map[string]string `json:"sphereSynonyms,omitempty" firestore:"sphereSynonyms,omitempty"` // label -> /sphere/... path

	// Event type vocabulary (eventType) used for ontology_facts where kind=event.
	AllowedEventTypes []string          `json:"allowedEventTypes,omitempty" firestore:"allowedEventTypes,omitempty"`
	EventTypeSynonyms map[string]string `json:"eventTypeSynonyms,omitempty" firestore:"eventTypeSynonyms,omitempty"` // label -> allowed eventType

	// Relation types for horizon_dependency_edges.
	AllowedRelationTypes []string `json:"allowedRelationTypes,omitempty" firestore:"allowedRelationTypes,omitempty"`

	// Signal types for OntologyFact(kind=signal)
	AllowedSignalKinds []string                      `json:"allowedSignalKinds,omitempty" firestore:"allowedSignalKinds,omitempty"` // "risk", "opportunity", "trend", "direction"
	SignalTypes        map[string]SignalTypeSpecData `json:"signalTypes,omitempty" firestore:"signalTypes,omitempty"`               // "risk:supply_disruption" -> spec
	SignalTypeSynonyms map[string]string             `json:"signalTypeSynonyms,omitempty" firestore:"signalTypeSynonyms,omitempty"` // alias -> canonical type

	// Optional: metric vocabulary and constraints (kept as data, used by prompts/validators as needed).
	AllowedMetricUnits []string `json:"allowedMetricUnits,omitempty" firestore:"allowedMetricUnits,omitempty"`

	// Product taxonomy for canonicalization (/product/{category}/{type}/{stage}/{slug})
	AllowedProductCategories []string                   `json:"allowedProductCategories,omitempty" firestore:"allowedProductCategories,omitempty"` // "raw", "component", "finished", "service"
	AllowedProductTypes      []string                   `json:"allowedProductTypes,omitempty" firestore:"allowedProductTypes,omitempty"`           // "mineral", "agricultural", "chemical", "electronic", etc.
	AllowedProductStages     []string                   `json:"allowedProductStages,omitempty" firestore:"allowedProductStages,omitempty"`         // "extraction", "refining", "processing", "manufacturing", "assembly"
	ProductStageOrder        []string                   `json:"productStageOrder,omitempty" firestore:"productStageOrder,omitempty"`               // Ordered list for algorithmic propagation
	KnownProducts            map[string]ProductSpecData `json:"knownProducts,omitempty" firestore:"knownProducts,omitempty"`                       // "lithium-carbonate" -> spec
	ProductSynonyms          map[string]string          `json:"productSynonyms,omitempty" firestore:"productSynonyms,omitempty"`                   // "Li2CO3" -> "lithium-carbonate", "battery grade lithium" -> "lithium-carbonate"

	// Industry taxonomy (/industry/...) — canonical, multi-valued classification for ORG/PERSON/PRODUCT.
	// This is intended to be more strict than free-form sectorPath.
	AllowedIndustryPaths []string          `json:"allowedIndustryPaths,omitempty" firestore:"allowedIndustryPaths,omitempty"`
	IndustrySynonyms     map[string]string `json:"industrySynonyms,omitempty" firestore:"industrySynonyms,omitempty"`       // normalized label -> /industry/... path
	IndustryWikidataMap  map[string]string `json:"industryWikidataMap,omitempty" firestore:"industryWikidataMap,omitempty"` // Wikidata QID -> /industry/... path

	// Natural resource deposits (/deposit/{resource-slug}/{deposit-slug})
	AllowedDepositResources []string `json:"allowedDepositResources,omitempty" firestore:"allowedDepositResources,omitempty"`

	// Lifecycle stages for FACILITY, DEPOSIT, ORGANIZATION
	AllowedLifecycleStages []string          `json:"allowedLifecycleStages,omitempty" firestore:"allowedLifecycleStages,omitempty"`
	LifecycleStageAliases  map[string]string `json:"lifecycleStageAliases,omitempty" firestore:"lifecycleStageAliases,omitempty"` // industry-specific alias -> canonical

	// Edge metrics (numerical data on edges)
	AllowedEdgeMetrics    map[string][]string `json:"allowedEdgeMetrics,omitempty" firestore:"allowedEdgeMetrics,omitempty"`       // edge type -> allowed metric names
	AllowedEdgeModalities []string            `json:"allowedEdgeModalities,omitempty" firestore:"allowedEdgeModalities,omitempty"` // "actual", "projected", "historical", "potential"

	// Service taxonomy (/product/service/{domain}/{subdomain}/{slug})
	AllowedServiceDomains    []string                   `json:"allowedServiceDomains,omitempty" firestore:"allowedServiceDomains,omitempty"`
	AllowedServiceSubdomains map[string][]string        `json:"allowedServiceSubdomains,omitempty" firestore:"allowedServiceSubdomains,omitempty"` // domain -> subdomains
	KnownServices            map[string]ServiceSpecData `json:"knownServices,omitempty" firestore:"knownServices,omitempty"`
	ServiceSynonyms          map[string]string          `json:"serviceSynonyms,omitempty" firestore:"serviceSynonyms,omitempty"` // alias -> canonical slug

	// Optional: product models (for specific SKU/model tracking)
	KnownProductModels map[string]ProductModelSpecData `json:"knownProductModels,omitempty" firestore:"knownProductModels,omitempty"` // model-slug -> spec

	// --- Market segment normalization dictionary (used by backend + UI) ---
	// SegmentChannelDomains are domain values treated as "channel" (e.g. b2b/b2c/public-sector).
	SegmentChannelDomains []string `json:"segmentChannelDomains,omitempty" firestore:"segmentChannelDomains,omitempty"`
	// SegmentDomainAliases maps a raw domain -> normalized domain.
	SegmentDomainAliases map[string]string `json:"segmentDomainAliases,omitempty" firestore:"segmentDomainAliases,omitempty"`
	// SegmentVerticalAliases maps a raw vertical -> normalized vertical.
	SegmentVerticalAliases map[string]string `json:"segmentVerticalAliases,omitempty" firestore:"segmentVerticalAliases,omitempty"`
	// SegmentVerticalFamilies maps a vertical -> normalized domain (family) when the segment uses a channel-like domain.
	SegmentVerticalFamilies map[string]string `json:"segmentVerticalFamilies,omitempty" firestore:"segmentVerticalFamilies,omitempty"`
	// SegmentNormRules provides explicit overrides keyed by "/segment/{domain}/{vertical}" (geo ignored).
	SegmentNormRules map[string]SegmentNormRuleData `json:"segmentNormRules,omitempty" firestore:"segmentNormRules,omitempty"`
}

// ProductSpecData is the Firestore representation of ProductSpec.
type ProductSpecData struct {
	Category string `json:"category" firestore:"category"` // "raw", "component", "finished", "service"
	Type     string `json:"type" firestore:"type"`         // "mineral", "agricultural", "chemical", "electronic", etc.
	Stage    string `json:"stage" firestore:"stage"`       // "extraction", "refining", "processing", "manufacturing", "assembly"
}

// SignalTypeSpecData is the Firestore representation of SignalTypeSpec.
type SignalTypeSpecData struct {
	Kind               string `json:"kind" firestore:"kind"`                             // "risk", "opportunity", "trend", "direction"
	Label              string `json:"label" firestore:"label"`                           // Human-readable label
	DefaultMetric      string `json:"defaultMetric" firestore:"defaultMetric"`           // Default metric name for rollup
	PropagationProfile string `json:"propagationProfile" firestore:"propagationProfile"` // "upstream_amplified", "downstream_amplified", "jurisdiction_scoped", etc.
}

// ServiceSpecData is the Firestore representation for services (/product/service/{domain}/{subdomain}/{slug}).
type ServiceSpecData struct {
	Domain    string `json:"domain" firestore:"domain"`       // "it", "financial", "legal", etc.
	Subdomain string `json:"subdomain" firestore:"subdomain"` // "software", "cloud", "banking", etc.
}

// ProductModelSpecData is the Firestore representation for specific product models/SKUs.
type ProductModelSpecData struct {
	GenericSlug  string `json:"genericSlug" firestore:"genericSlug"`   // parent product slug (e.g., "excavator")
	Manufacturer string `json:"manufacturer" firestore:"manufacturer"` // manufacturer org slug (e.g., "caterpillar")
}

// SegmentNormRuleData is the Firestore representation of SegmentNormRule.
type SegmentNormRuleData struct {
	NormDomain   string `json:"normDomain" firestore:"normDomain"`
	NormVertical string `json:"normVertical,omitempty" firestore:"normVertical,omitempty"`
	Channel      string `json:"channel,omitempty" firestore:"channel,omitempty"`
}
