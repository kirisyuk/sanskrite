package models

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reISO6709Pair = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)/?$`)

// MetricValue представляет гибкую метрику с описанием, типом и метаданными
type MetricValue struct {
	Value       float64      `json:"value" firestore:"value"`
	Unit        *string      `json:"unit,omitempty" firestore:"unit,omitempty"`
	ErrorMargin *ErrorMargin `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
	Confidence  float64      `json:"confidence" firestore:"confidence"` // 0..1
	ObservedAt  time.Time    `json:"observedAt" firestore:"observedAt"`
	Provenance  Provenance   `json:"provenance" firestore:"provenance"`
}

// ErrorMargin представляет интервал ошибки для метрики
type ErrorMargin struct {
	Low   *float64 `json:"low,omitempty" firestore:"low,omitempty"`
	High  *float64 `json:"high,omitempty" firestore:"high,omitempty"`
	Value *float64 `json:"value,omitempty" firestore:"value,omitempty"` // для симметричного интервала
}

// Location представляет местоположение с ISO стандартами
type Location struct {
	Name      string  `json:"name,omitempty" firestore:"name,omitempty"`
	ISO6709   *string `json:"iso6709,omitempty" firestore:"iso6709,omitempty"`     // координаты ISO 6709
	ISO3166_1 *string `json:"iso3166_1,omitempty" firestore:"iso3166_1,omitempty"` // ISO 3166-1 alpha-2 (страна)
	ISO3166_2 *string `json:"iso3166_2,omitempty" firestore:"iso3166_2,omitempty"` // ISO 3166-2 (субъект)
}

// locationJSON is used for flexible unmarshalling (LLMs often return "country" instead of "iso3166_1").
type locationJSON struct {
	Name          string  `json:"name,omitempty"`
	ISO6709       *string `json:"iso6709,omitempty"`
	ISO3166_1     *string `json:"iso3166_1,omitempty"`
	ISO3166_2     *string `json:"iso3166_2,omitempty"`
	Country       *string `json:"country,omitempty"`        // LLM alias
	CountryCode   *string `json:"countryCode,omitempty"`    // LLM alias
	CountryAlpha2 *string `json:"countryAlpha2,omitempty"`  // ontology extractor style
}

// UnmarshalJSON accepts both canonical fields and common LLM aliases (country, countryCode, countryAlpha2).
func (l *Location) UnmarshalJSON(data []byte) error {
	var raw locationJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.Name = strings.TrimSpace(raw.Name)
	l.ISO6709 = raw.ISO6709
	l.ISO3166_2 = raw.ISO3166_2
	// ISO3166_1: prefer canonical, then aliases
	if raw.ISO3166_1 != nil && strings.TrimSpace(*raw.ISO3166_1) != "" {
		v := strings.TrimSpace(*raw.ISO3166_1)
		l.ISO3166_1 = &v
	} else if raw.CountryAlpha2 != nil && strings.TrimSpace(*raw.CountryAlpha2) != "" {
		v := strings.TrimSpace(*raw.CountryAlpha2)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	} else if raw.CountryCode != nil && strings.TrimSpace(*raw.CountryCode) != "" {
		v := strings.TrimSpace(*raw.CountryCode)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	} else if raw.Country != nil && strings.TrimSpace(*raw.Country) != "" {
		v := strings.TrimSpace(*raw.Country)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	}
	return nil
}

// Coordinates is a typed geographic coordinate.
type Coordinates struct {
	Lat float64 `json:"lat" firestore:"lat"`
	Lon float64 `json:"lon" firestore:"lon"`
}

// IsValidCoordinates validates lat/lon ranges and guards against NaN/Inf.
func IsValidCoordinates(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// ParseISO6709Coordinates parses "+lat-lon/" style ISO6709 coordinates.
func ParseISO6709Coordinates(raw string) (float64, float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, 0, false
	}
	m := reISO6709Pair.FindStringSubmatch(s)
	if len(m) != 3 {
		return 0, 0, false
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	if err != nil {
		return 0, 0, false
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
	if err != nil {
		return 0, 0, false
	}
	if !IsValidCoordinates(lat, lon) {
		return 0, 0, false
	}
	return lat, lon, true
}

// KBReference представляет ссылку на внешнюю базу знаний
type KBReference struct {
	KB         string     `json:"kb" firestore:"kb"` // Wikidata, DBpedia, etc.
	ExternalID string     `json:"externalId" firestore:"externalId"`
	Confidence float64    `json:"confidence" firestore:"confidence"` // 0..1
	LastSync   *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
}

// Provenance представляет прослеживаемость источника данных
type Provenance struct {
	SourceIDs     []string    `json:"sourceIds" firestore:"sourceIds"` // для обратной совместимости
	SourceRefs    []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	DataSourceIDs []string    `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"` // DataSource IDs
	DocumentIDs   []string    `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`     // Document IDs
	// Contributions is the bounded, structured provenance for this metric path on this node.
	// It is the primary contract for tracing how a value arrived (risk-architecture.md "Top-K provenance").
	Contributions []Contribution `json:"contributions,omitempty" firestore:"contributions,omitempty"`
	ObservedAt    *time.Time     `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	Method        string         `json:"method,omitempty" firestore:"method,omitempty"` // extracted, curated, imported
	// GroundingContractVersion is the version of internal-first grounding provenance contract.
	// Example: "v1"
	GroundingContractVersion string `json:"groundingContractVersion,omitempty" firestore:"groundingContractVersion,omitempty"`
	// OriginMode classifies where evidence came from: internal_only | external_only | mixed.
	OriginMode string `json:"originMode,omitempty" firestore:"originMode,omitempty"`
	// InternalEvidence stores deterministic IDs/URIs proving internal datastore grounding.
	InternalEvidence *GroundingInternalEvidence `json:"internalEvidence,omitempty" firestore:"internalEvidence,omitempty"`
	// ExternalEvidence stores web citations/URLs used by grounded generation.
	ExternalEvidence *GroundingExternalEvidence `json:"externalEvidence,omitempty" firestore:"externalEvidence,omitempty"`
	// ResolutionStats captures grounding-aware materialization counters.
	ResolutionStats *GroundingResolutionStats `json:"resolutionStats,omitempty" firestore:"resolutionStats,omitempty"`
}

// SourceRef представляет ссылку на источник данных
type SourceRef struct {
	SourceID   string  `json:"sourceId" firestore:"sourceId"`                         // DataSource ID или Document ID
	SourceType string  `json:"sourceType" firestore:"sourceType"`                     // "dataSource", "document", "web", "rss", "telegram_channel"
	DocumentID *string `json:"documentId,omitempty" firestore:"documentId,omitempty"` // Если SourceType = "dataSource", может быть Document
}

// GroundingInternalEvidence captures internal datastore evidence used by grounded calls.
type GroundingInternalEvidence struct {
	FragmentIDs   []string `json:"fragmentIds,omitempty" firestore:"fragmentIds,omitempty"`
	DatastoreURIs []string `json:"datastoreUris,omitempty" firestore:"datastoreUris,omitempty"`
	SourceIDs     []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	DocumentIDs   []string `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`
}

// GroundingExternalEvidence captures external web evidence used by grounded calls.
type GroundingExternalEvidence struct {
	WebURLs   []string `json:"webUrls,omitempty" firestore:"webUrls,omitempty"`
	Citations []string `json:"citations,omitempty" firestore:"citations,omitempty"`
}

// GroundingResolutionStats captures grounding-aware materialization outcomes.
type GroundingResolutionStats struct {
	RelationsResolvedByGrounding int `json:"relationsResolvedByGrounding,omitempty" firestore:"relationsResolvedByGrounding,omitempty"`
	RelationsCreatedNew          int `json:"relationsCreatedNew,omitempty" firestore:"relationsCreatedNew,omitempty"`
	DedupRedirectsByGrounding    int `json:"dedupRedirectsByGrounding,omitempty" firestore:"dedupRedirectsByGrounding,omitempty"`
}

// Contribution represents a single source contribution to a metric at a node.
// It stores the source signal, source event, propagation path, and the post-decay amplitude.
// This matches risk-architecture.md (Lazy Interpretation input contract).
type Contribution struct {
	SignalID      string    `json:"signalId" firestore:"signalId"`
	SourceEventID string    `json:"sourceEventId" firestore:"sourceEventId"`
	Path          []string  `json:"path" firestore:"path"` // propagation path (canonical paths preferred)
	Amplitude     float64   `json:"amplitude" firestore:"amplitude"`
	Hops          int       `json:"hops" firestore:"hops"`
	ObservedAt    time.Time `json:"observedAt" firestore:"observedAt"`
}

// Governance управляет жизненным циклом HorizonFragment
type Governance struct {
	Owner              string  `json:"owner" firestore:"owner"`                                               // Всегда "SYSTEM"
	ActiveSubscribers  int     `json:"activeSubscribers" firestore:"activeSubscribers"`                       // Количество активных Lens
	Status             string  `json:"status" firestore:"status"`                                             // "ACTIVE", "DORMANT", "ARCHIVED"
	CreatedByProjectID *string `json:"createdByProjectId,omitempty" firestore:"createdByProjectId,omitempty"` // Проект, оплативший создание (для биллинга)
}

// FragmentType тип HorizonFragment
type FragmentType string

const (
	FragmentTypeOrganization FragmentType = "ORGANIZATION"
	FragmentTypeFacility     FragmentType = "FACILITY"
	FragmentTypeGeoRegion    FragmentType = "GEO_REGION"
	// FragmentTypeDomain is a contract type identifier (domain is config, not a graph node).
	// Canonical path format: /domain/v1/{contractType}/{facetKey}/{facetValue}...
	// Examples:
	// - /domain/v1/supply-chain/geo/us/application/ev/material/lithium
	// - /domain/v1/bilateral-relations/subject/uae-saudi-arabia
	FragmentTypeDomain  FragmentType = "DOMAIN"
	FragmentTypeSphere  FragmentType = "SPHERE"
	FragmentTypeEvent   FragmentType = "EVENT"
	FragmentTypeProduct FragmentType = "PRODUCT"
	FragmentTypeProgram FragmentType = "PROGRAM"
	FragmentTypeMarket  FragmentType = "MARKET"
	FragmentTypePerson  FragmentType = "PERSON"
	// FragmentTypeMediaChannel represents a "last-mile" media endpoint (e.g., a specific public Telegram channel),
	// not a media holding/company. This helps model narratives and distribution channels separately from owners.
	FragmentTypeMediaChannel FragmentType = "MEDIA_CHANNEL"
	// FragmentTypeDecisionPoint represents a strategic decision point (domain-scoped).
	// Canonical path format: /decision/{domain-slug}/{decision-slug}
	FragmentTypeDecisionPoint FragmentType = "DECISION_POINT"
	// FragmentTypeMechanism represents a control mechanism (global, reusable).
	// Canonical path format: /mechanism/{category}/{slug}
	FragmentTypeMechanism  FragmentType = "MECHANISM"
	FragmentTypeSupplyLink FragmentType = "SUPPLY_LINK" // Tenant-specific supply chain link
	// FragmentTypeTrack represents movable objects with frequently updated position/state.
	// Canonical path format: /track/{kind}/{geo}/{slug}-{id8}
	// - kind: air | sea | ground
	// - geo: ISO3166-1 alpha2 lowercase (2 chars) OR "global"
	FragmentTypeTrack FragmentType = "TRACK"
	// FragmentTypeDeposit represents a natural resource deposit (oil field, mine, basin, etc.).
	// Canonical path format: /deposit/{resource-slug}/{deposit-slug}
	// Example: /deposit/crude-oil/permian-basin, /deposit/lithium/atacama-salar
	FragmentTypeDeposit FragmentType = "DEPOSIT"
	// FragmentTypeCommunity represents an informal social group without legal structure.
	// Examples: trade networks (hawala), activist groups, criminal networks, diaspora communities.
	// Unlike ORGANIZATION, communities have no formal legal registration.
	// Unlike MEDIA_CHANNEL, communities are social entities that *use* channels.
	// Canonical path format: /community/{geo}/{type}/{slug}
	// - geo: ISO3166-1 alpha2 lowercase (2 chars) OR "global"
	// - type: trade | activist | professional | criminal | religious | ethnic | interest
	// - slug: lowercase-slug derived from community name
	// Examples:
	// - /community/ae/trade/hawala-dubai-network
	// - /community/ru/criminal/kazan-synthetic-dealers
	// - /community/by/activist/nexta-supporters
	// - /community/global/professional/osint-researchers
	FragmentTypeCommunity FragmentType = "COMMUNITY"
)

// EpistemicStatus статус эпистемической достоверности
type EpistemicStatus string

const (
	EpistemicStatusVerified      EpistemicStatus = "VERIFIED"
	EpistemicStatusRejected      EpistemicStatus = "REJECTED"
	EpistemicStatusLowConfidence EpistemicStatus = "LOW_CONFIDENCE"
)

// FragmentStatus статус жизненного цикла HorizonFragment
type FragmentStatus string

const (
	FragmentStatusActive   FragmentStatus = "ACTIVE"
	FragmentStatusDormant  FragmentStatus = "DORMANT"
	FragmentStatusArchived FragmentStatus = "ARCHIVED"
)

// ExtractionMethod метод извлечения данных
type ExtractionMethod string

const (
	ExtractionMethodNLP     ExtractionMethod = "NLP"
	ExtractionMethodHuman   ExtractionMethod = "human"
	ExtractionMethodAPI     ExtractionMethod = "API"
	ExtractionMethodKBAlign ExtractionMethod = "KBAlign"
)

// AgentRole represents the role of an agent in a decision context.
// Used in DecisionPoint entities to describe how an agent participates in a decision.
type AgentRole string

const (
	// AgentRoleGatekeeper can block/enable access (export control, licensing).
	AgentRoleGatekeeper AgentRole = "gatekeeper"
	// AgentRoleApprover must approve before action (regulatory approval).
	AgentRoleApprover AgentRole = "approver"
	// AgentRoleInfluencer can affect but not block (lobbying, standards bodies).
	AgentRoleInfluencer AgentRole = "influencer"
	// AgentRoleEnforcer ensures compliance (audit, inspection).
	AgentRoleEnforcer AgentRole = "enforcer"
	// AgentRoleArbiter resolves disputes (courts, arbitration).
	AgentRoleArbiter AgentRole = "arbiter"
)

// ValidAgentRoles is a set of all valid agent roles for validation.
var ValidAgentRoles = map[AgentRole]bool{
	AgentRoleGatekeeper: true,
	AgentRoleApprover:   true,
	AgentRoleInfluencer: true,
	AgentRoleEnforcer:   true,
	AgentRoleArbiter:    true,
}

// IsValidAgentRole checks if the given role is a valid AgentRole.
func IsValidAgentRole(role string) bool {
	return ValidAgentRoles[AgentRole(role)]
}

// MechanismCategory represents the category/type of a control mechanism.
type MechanismCategory string

const (
	// MechanismCategoryRegulatory - Export control, licensing, certification.
	MechanismCategoryRegulatory MechanismCategory = "regulatory"
	// MechanismCategoryEconomic - Pricing power, capacity allocation.
	MechanismCategoryEconomic MechanismCategory = "economic"
	// MechanismCategoryTechnological - IP lock-in, standards control.
	MechanismCategoryTechnological MechanismCategory = "technological"
	// MechanismCategoryContractual - Exclusivity, long-term agreements.
	MechanismCategoryContractual MechanismCategory = "contractual"
	// MechanismCategoryPolitical - Sanctions, diplomatic pressure.
	MechanismCategoryPolitical MechanismCategory = "political"
)

// ValidMechanismCategories is a set of all valid mechanism categories.
var ValidMechanismCategories = map[MechanismCategory]bool{
	MechanismCategoryRegulatory:    true,
	MechanismCategoryEconomic:      true,
	MechanismCategoryTechnological: true,
	MechanismCategoryContractual:   true,
	MechanismCategoryPolitical:     true,
}

// IsValidMechanismCategory checks if the given category is valid.
func IsValidMechanismCategory(category string) bool {
	return ValidMechanismCategories[MechanismCategory(category)]
}

// ControlAccessType represents the type of control in a CONTROLS_ACCESS relation.
type ControlAccessType string

const (
	// ControlAccessTypeBlock - veto power, can completely block.
	ControlAccessTypeBlock ControlAccessType = "block"
	// ControlAccessTypeEnable - grant access, can enable.
	ControlAccessTypeEnable ControlAccessType = "enable"
	// ControlAccessTypeDelay - bureaucratic delay, slows down.
	ControlAccessTypeDelay ControlAccessType = "delay"
	// ControlAccessTypeMonitor - surveillance, observes but doesn't block.
	ControlAccessTypeMonitor ControlAccessType = "monitor"
)

// ValidControlAccessTypes is a set of all valid control access types.
var ValidControlAccessTypes = map[ControlAccessType]bool{
	ControlAccessTypeBlock:   true,
	ControlAccessTypeEnable:  true,
	ControlAccessTypeDelay:   true,
	ControlAccessTypeMonitor: true,
}

// IsValidControlAccessType checks if the given type is valid.
func IsValidControlAccessType(t string) bool {
	return ValidControlAccessTypes[ControlAccessType(t)]
}

// ConditionType represents the type of condition in an IMPOSES_CONDITION relation.
type ConditionType string

const (
	// ConditionTypeCost - tariffs, fees, economic burden.
	ConditionTypeCost ConditionType = "cost"
	// ConditionTypeCompliance - ESG, standards, regulatory compliance.
	ConditionTypeCompliance ConditionType = "compliance"
	// ConditionTypeTechnical - technical specifications, compatibility requirements.
	ConditionTypeTechnical ConditionType = "technical"
)

// ValidConditionTypes is a set of all valid condition types.
var ValidConditionTypes = map[ConditionType]bool{
	ConditionTypeCost:       true,
	ConditionTypeCompliance: true,
	ConditionTypeTechnical:  true,
}

// IsValidConditionType checks if the given type is valid.
func IsValidConditionType(t string) bool {
	return ValidConditionTypes[ConditionType(t)]
}
