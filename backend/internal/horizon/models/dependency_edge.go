package models

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// DependencyEdge представляет связь между HorizonFragment с метриками влияния
type DependencyEdge struct {
	ID               string                 `json:"id" firestore:"id"`
	TenantID         string                 `json:"tenantId" firestore:"tenantId"`
	SourceFragmentID string                 `json:"sourceFragmentId" firestore:"sourceFragmentId"`
	TargetFragmentID string                 `json:"targetFragmentId" firestore:"targetFragmentId"`
	Type             DependencyType         `json:"type" firestore:"type"`
	Properties       EdgeProperties         `json:"properties" firestore:"properties"`
	Qualifiers       map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	Provenance       Provenance             `json:"provenance" firestore:"provenance"`
	ValidFrom        *time.Time             `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo          *time.Time             `json:"validTo,omitempty" firestore:"validTo,omitempty"`
	Metadata         EdgeMetadata           `json:"metadata" firestore:"metadata"`

	// Metrics — edge-specific numerical metrics with units, provenance, etc.
	// Examples: ownership_percent, investment_amount, annual_volume, contract_value.
	// Uses the same MetricValue structure as fragment State for consistency.
	Metrics map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
}

// DependencyType тип зависимости
type DependencyType string

const (
	DependencyTypeSuppliesTo DependencyType = "SUPPLIES_TO"
	DependencyTypeLocatedIn  DependencyType = "LOCATED_IN"
	DependencyTypeOwns       DependencyType = "OWNS"
	// DependencyTypeSubsidiaryOf captures "X is owned/controlled by Y" (inverse of OWNS).
	// NOTE: In scouting pipeline, SUBSIDIARY_OF is normalized into OWNS (parent -> subsidiary),
	// but we keep this type for taxonomy/prompt compatibility.
	DependencyTypeSubsidiaryOf DependencyType = "SUBSIDIARY_OF"
	DependencyTypeProduces     DependencyType = "PRODUCES"
	// DependencyTypeConsumes captures "X consumes/uses product Y".
	// v1 usage: hypothesis-driven Product IO materialization (PRODUCT-only).
	DependencyTypeConsumes     DependencyType = "CONSUMES"
	DependencyTypeOperatesIn   DependencyType = "OPERATES_IN"
	DependencyTypeRegulates    DependencyType = "REGULATES"
	DependencyTypeCompetesWith DependencyType = "COMPETES_WITH"
	DependencyTypeDependsOn    DependencyType = "DEPENDS_ON"
	// DependencyTypeInfluences is used for abstract "wave dimensions" like /sphere/... or /loc/...
	// where the source node represents a domain and the target is a concrete fragment.
	DependencyTypeInfluences DependencyType = "INFLUENCES"
	// DependencyTypeTransformsInto represents value chain transformation: Product X transforms into Product Y.
	// Used for generic value chain relationships (e.g., lithium-ore → lithium-carbonate → cathode-material → battery-cell).
	// Source: upstream product (earlier stage), Target: downstream product (later stage).
	DependencyTypeTransformsInto DependencyType = "TRANSFORMS_INTO"
	// DependencyTypeExtractsFrom represents facility extracting resources from a deposit.
	// Direction: FACILITY → DEPOSIT (e.g., mine extracts from ore deposit, platform extracts from oil field).
	// More precise than LOCATED_IN, captures the operational relationship.
	DependencyTypeExtractsFrom DependencyType = "EXTRACTS_FROM"

	// --- Market participation ---
	// PARTICIPATES_IN: org/facility participates in a market (player relationship).
	DependencyTypeParticipatesIn DependencyType = "PARTICIPATES_IN"
	// TRADED_IN: product is traded/price-discovered in a market.
	// Direction: PRODUCT -> MARKET. This is not the same as market participation by an actor.
	DependencyTypeTradedIn DependencyType = "TRADED_IN"
	// DOMINATES: org/facility dominates a market (leader relationship).
	DependencyTypeDominates DependencyType = "DOMINATES"
	// HAS_MARKET_SEGMENT: market hierarchy (parent market has segment = child/narrower market).
	// Child path = parent path + exactly one segment. Direction: broader → narrower.
	// Example: /market/sa/marketing HAS_MARKET_SEGMENT /market/sa/marketing/event-marketing
	DependencyTypeHasMarketSegment DependencyType = "HAS_MARKET_SEGMENT"
	// DependencyTypeHasSubmarket is deprecated. Use HAS_MARKET_SEGMENT. Kept in enum for backward compatibility.
	DependencyTypeHasSubmarket DependencyType = "HAS_SUBMARKET"

	// --- Government hierarchy ---
	// ADMINISTERS: government administrative hierarchy (superior body → subordinate body).
	// Example: /gov/sa/ministry-of-health ADMINISTERS /gov/sa/sfda
	// Direction: superior → subordinate (like OWNS but for administrative relationships).
	DependencyTypeAdministers DependencyType = "ADMINISTERS"

	// --- Governance / control / org-person relationships ---
	// MANAGES: formal management relationship (CEO, director, operator) — not necessarily ownership.
	DependencyTypeManages DependencyType = "MANAGES"
	// CONTROLS: control without necessarily direct equity ownership (voting control, state control, contractual control).
	DependencyTypeControls DependencyType = "CONTROLS"
	// CONTROLS_ACCESS: strategic gatekeeping (block/enable/delay/monitor) over an existing relation.
	DependencyTypeControlsAccess DependencyType = "CONTROLS_ACCESS"
	// EMPLOYED_BY: employment relationship (person is employed by an organization).
	DependencyTypeEmployedBy DependencyType = "EMPLOYED_BY"
	// BOARD_MEMBER_OF: person is a board member of an organization.
	DependencyTypeBoardMemberOf DependencyType = "BOARD_MEMBER_OF"
	// FOUNDED: founder relationship (person or org founded an org/program).
	DependencyTypeFounded DependencyType = "FOUNDED"
	// INVESTS_IN: investment relationship (org/person invests in org/project/program/product).
	DependencyTypeInvestsIn DependencyType = "INVESTS_IN"
	// PARTNERS_WITH: symmetric partnership relationship (peer collaboration).
	DependencyTypePartnersWith DependencyType = "PARTNERS_WITH"

	// --- Strategic decision layer ---
	// IMPOSES_CONDITION: subject forces object to comply with rules/costs/technical constraints.
	DependencyTypeImposesCondition DependencyType = "IMPOSES_CONDITION"
	// GATEKEEPS_DECISION: actor controls a strategic decision point (strong gatekeeping).
	DependencyTypeGatekeepsDecision DependencyType = "GATEKEEPS_DECISION"
	// USES_MECHANISM: agent possesses a mechanism of control.
	DependencyTypeUsesMechanism DependencyType = "USES_MECHANISM"
	// APPLIES_TO: mechanism applies to entity or relation scope.
	DependencyTypeAppliesTo DependencyType = "APPLIES_TO"

	// --- Canonical consolidated types (TZ §6.1) ---
	// ASSOCIATED_WITH: canonical for symmetric/associative links (replaces RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH as output).
	DependencyTypeAssociatedWith DependencyType = "ASSOCIATED_WITH"
	// CORPORATE_TRANSITION: canonical for renames, successions, M&A (replaces RENAMED_TO, REBRANDED_TO, LEGAL_SUCCESSOR_OF, etc.).
	DependencyTypeCorporateTransition DependencyType = "CORPORATE_TRANSITION"
	// DOMAIN_RELATION: canonical for domain↔domain links (replaces RELATED_DOMAIN, BROADENS_TO, NARROWS_TO as output).
	DependencyTypeDomainRelation DependencyType = "DOMAIN_RELATION"

	// --- Legacy types for migration (deprecated as LLM/output types; canonicalizer maps to types above) ---
	// WORKS_FOR: legacy employment (prefer EMPLOYED_BY). Person works for org.
	DependencyTypeWorksFor DependencyType = "WORKS_FOR"
	// KEY_PERSON: legacy key person relationship. Should be PERSON → ORG.
	DependencyTypeKeyPerson DependencyType = "KEY_PERSON"
	// HAS_KEY_PERSON: legacy inverse (ORG has key person). Should be normalized to KEY_PERSON (PERSON → ORG).
	DependencyTypeHasKeyPerson DependencyType = "HAS_KEY_PERSON"

	// --- Assurance / oversight (Third Line: independent verification) ---
	// ASSURES: assurer provides independent assurance to assuree. Qualifier assurance_type: audit | certification | regulatory | inspection.
	// Direction: assurer (ORGANIZATION) → assuree (ORGANIZATION/FACILITY/PROGRAM).
	// Subsumes: audit (AUDITED_BY inverse), certification, regulatory approval, inspection.
	DependencyTypeAssures DependencyType = "ASSURES"

	// --- OSINT-lite / governance extensions ---
	// AUDITED_BY: external auditor relationship (company -> audit firm). Legacy; prefer ASSURES(assurance_type:audit) for new data.
	DependencyTypeAuditedBy DependencyType = "AUDITED_BY"
	// RELATIVE_OF: symmetric person-person relationship (public relatives).
	DependencyTypeRelativeOf DependencyType = "RELATIVE_OF"
	// RELATED_TO: symmetric person-person relationship (publicly known associates: friends/partners/classmates).
	DependencyTypeRelatedTo DependencyType = "RELATED_TO"

	// --- Community relationships ---
	// OPERATES_VIA: entity uses a channel/platform for communication or operations.
	// Direction: COMMUNITY/ORGANIZATION → MEDIA_CHANNEL
	// Example: /community/ru/criminal/kazan-dealers OPERATES_VIA /media/telegram/kazan-zakaz
	DependencyTypeOperatesVia DependencyType = "OPERATES_VIA"
	// MEMBER_OF: person is a member of a community (not employment).
	// Direction: PERSON → COMMUNITY
	// Example: /person/john-doe MEMBER_OF /community/global/professional/osint-researchers
	DependencyTypeMemberOf DependencyType = "MEMBER_OF"
	// AFFILIATED_WITH: community or person has affiliation with a formal organization.
	// Direction: COMMUNITY/PERSON → ORGANIZATION
	// Example: /community/ae/trade/hawala-network AFFILIATED_WITH /org/ae/exchange-house
	DependencyTypeAffiliatedWith DependencyType = "AFFILIATED_WITH"

	// --- DOMAIN relationships (executable knowledge contracts) ---
	// Domain ↔ Entity
	DependencyTypeFocusesOn     DependencyType = "FOCUSES_ON"
	DependencyTypeInvolvesActor DependencyType = "INVOLVES_ACTOR"
	DependencyTypeInGeoScope    DependencyType = "IN_GEO_SCOPE"
	DependencyTypeAbout         DependencyType = "ABOUT"
	// Domain → Strategic entities (Phase 2 overlay results).
	// SURFACES: domain "surfaces" (crystallizes) decision points and mechanisms during strategic analysis.
	DependencyTypeSurfaces DependencyType = "SURFACES"
	// Domain ↔ Domain
	DependencyTypeRelatedDomain DependencyType = "RELATED_DOMAIN" // symmetric
	DependencyTypeBroadensTo    DependencyType = "BROADENS_TO"
	DependencyTypeNarrowsTo     DependencyType = "NARROWS_TO"
	DependencyTypeAffects       DependencyType = "AFFECTS"
	// TRIGGERED_BY: AFFECTED_ZONE was caused/triggered by an EVENT (e.g. earthquake zone by earthquake event).
	// Direction: AFFECTED_ZONE → EVENT
	DependencyTypeTriggeredBy DependencyType = "TRIGGERED_BY"

	// --- Community hierarchy ---
	// HAS_SUBGROUP: community hierarchy (parent group → child/narrower group).
	// Used for dynasties, tribes, clans, professional networks with internal structure.
	// Direction: broader community → narrower sub-community/branch/clan.
	// Examples:
	//   /community/sa/dynasty/house-of-saud HAS_SUBGROUP /community/sa/dynasty/sudairi-seven
	//   /community/af/tribe/pashtun HAS_SUBGROUP /community/af/tribe/durrani
	//   /community/kz/tribe/senior-zhuz HAS_SUBGROUP /community/kz/tribe/uysun
	DependencyTypeHasSubgroup DependencyType = "HAS_SUBGROUP"

	// --- Corporate transition edges ---
	// RENAMED_TO: legal name rename (typically same legal entity identity).
	DependencyTypeRenamedTo DependencyType = "RENAMED_TO"
	// REBRANDED_TO: brand rename/repositioning.
	DependencyTypeRebrandedTo DependencyType = "REBRANDED_TO"
	// LEGAL_SUCCESSOR_OF: new legal entity is successor of a previous one.
	DependencyTypeLegalSuccessorOf DependencyType = "LEGAL_SUCCESSOR_OF"
	// MERGED_INTO: source legal entity merged into target.
	DependencyTypeMergedInto DependencyType = "MERGED_INTO"
	// SPUN_OFF_FROM: source legal entity was spun off from target.
	DependencyTypeSpunOffFrom DependencyType = "SPUN_OFF_FROM"
	// ACQUIRED_BY: source legal entity is acquired by target.
	DependencyTypeAcquiredBy DependencyType = "ACQUIRED_BY"
)

// IsTransitionEdgeType reports whether an edge type represents a corporate transition.
func IsTransitionEdgeType(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyTypeCorporateTransition,
		DependencyTypeRenamedTo,
		DependencyTypeRebrandedTo,
		DependencyTypeLegalSuccessorOf,
		DependencyTypeMergedInto,
		DependencyTypeSpunOffFrom,
		DependencyTypeAcquiredBy:
		return true
	default:
		return false
	}
}

var transitionIntentTokens = []string{
	"RENAME",
	"REBRAND",
	"SUCCESSOR",
	"MERGED",
	"MERGER",
	"SPIN",
	"SPUN",
	"ACQUISITION",
	"ACQUIRED",
	"TAKEOVER",
	"FKA",
	"FORMERLY",
}

// IsTransitionLikeRelationType reports whether a raw relation string looks like a corporate transition intent.
// It accepts both canonical transition edge types and fuzzy transition-like tokens.
func IsTransitionLikeRelationType(rawRelationType string) bool {
	s := strings.ToUpper(strings.TrimSpace(rawRelationType))
	if s == "" {
		return false
	}
	if IsTransitionEdgeType(DependencyType(s)) {
		return true
	}
	for _, token := range transitionIntentTokens {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

// =============================================================================
// DEPENDENCY FACETS (qualifiers for DEPENDS_ON edges)
// =============================================================================
// DependencyFacet describes the nature/aspect of a DEPENDS_ON relationship.
// This is stored in Qualifiers["facet"] on DependencyEdge.
// Facets enable more granular analysis without proliferating edge types.

// DependencyFacet is a semantic qualifier for DEPENDS_ON edges.
type DependencyFacet string

const (
	// FacetCommunication: dependency on communication channels/platforms.
	// Example: drug market depends on Telegram channels for coordination.
	FacetCommunication DependencyFacet = "communication"

	// FacetInfrastructure: dependency on physical or digital infrastructure.
	// Example: e-commerce depends on cloud hosting, logistics depends on ports.
	FacetInfrastructure DependencyFacet = "infrastructure"

	// FacetPayment: dependency on payment/financial mechanisms.
	// Example: illicit market depends on crypto, hawala network depends on cash corridors.
	FacetPayment DependencyFacet = "payment"

	// FacetDistribution: dependency on distribution networks/channels.
	// Example: drug market depends on local dealer networks.
	FacetDistribution DependencyFacet = "distribution"

	// FacetLogistics: dependency on logistics/transportation.
	// Example: supply chain depends on shipping routes, ports, carriers.
	FacetLogistics DependencyFacet = "logistics"

	// FacetSupply: dependency on supply of materials/goods.
	// Example: manufacturer depends on raw material suppliers.
	FacetSupply DependencyFacet = "supply"

	// FacetLabor: dependency on labor/workforce.
	// Example: factory depends on skilled labor pool.
	FacetLabor DependencyFacet = "labor"

	// FacetLegitimization: dependency on legitimization mechanisms (for illicit activities).
	// Example: money laundering depends on shell companies, front businesses.
	FacetLegitimization DependencyFacet = "legitimization"

	// FacetEnforcement: dependency on enforcement/protection mechanisms.
	// Example: criminal network depends on corrupt officials, private security.
	FacetEnforcement DependencyFacet = "enforcement"

	// FacetRegulatory: dependency on regulatory environment/approvals.
	// Example: pharma company depends on FDA approval process.
	FacetRegulatory DependencyFacet = "regulatory"
)

// Transmission qualifier keys for systemic transmission modeling.
const (
	QualifierThroughputShare        = "throughput_share" // 0..1 share of flow routed through this edge
	QualifierReroutePenalty         = "reroute_penalty"  // 0..1 substitution/reroute friction
	QualifierRerouteLatencyDeltaHrs = "reroute_latency_delta_hours"
	QualifierRerouteCostDeltaPct    = "reroute_cost_delta_pct"
	QualifierCapacityConstraint     = "capacity_constraint" // 0..1 where 1 means severe capacity bottleneck

	// Transmission contract metadata (soft mandatory in best-effort mode).
	QualifierTransmissionContractVersion = "transmission_contract_version"
	QualifierTransmissionContractStatus  = "transmission_contract_status"
	QualifierTransmissionContractMode    = "transmission_contract_mode"
	QualifierTransmissionContractMissing = "transmission_contract_missing"

	TransmissionContractVersionV1 = "v1"

	TransmissionContractStatusComplete = "complete"
	TransmissionContractStatusPartial  = "partial"
	TransmissionContractStatusMissing  = "missing"

	TransmissionContractModeExplicit = "explicit"
	TransmissionContractModeInferred = "inferred"
)

// Supply chain / governance relationship nature (SUPPLIES_TO, OWNS).
const (
	QualifierIntegrationType = "integration_type" // "vertical" | "partnership" | "arm's-length"

	IntegrationTypeVertical    = "vertical"     // owned/controlled within same group (e.g., BYD subsidiaries)
	IntegrationTypePartnership = "partnership"  // JV, strategic alliance, long-term partnership
	IntegrationTypeArmsLength  = "arm's-length" // spot/short-term commercial, pure financial
)

// QualifierAtFacility is used on TRANSFORMS_INTO edges to indicate where the product transformation occurs.
// Value: canonical path of the facility (e.g. /fac/us/silver-peak).
// Semantics: products transform; the facility is the qualifier (where it happens).
const QualifierAtFacility = "at_facility"

// QualifierTransportVia is used on SUPPLIES_TO (and SupplyLink) to indicate transport route facilities.
// Value: array of canonical paths (ports, terminals, pipelines, warehouses, etc.).
// Semantics: facilities that qualify the transport method; product passes through these.
// Same canonicalization and deduplication rules apply as for other fragments.
const QualifierTransportVia = "transport_via"

// Path-centric spatial binding qualifiers shared by supply path artifacts and runtime propagation edges.
const (
	QualifierRouteMode            = "route_mode"
	QualifierGeoRefs              = "geo_refs"
	QualifierFlowRefs             = "flow_refs"
	QualifierAlternateFlowRefs    = "alternate_flow_refs"
	QualifierZoneRefs             = "zone_refs"
	QualifierAffectedZoneRefs     = "affected_zone_refs"
	QualifierSpatialSemantics     = "spatial_semantics"
	QualifierBindingConfidence    = "binding_confidence"
	QualifierPathStressScore      = "path_stress_score"
	QualifierPathFragility        = "path_fragility"
	QualifierPathRerouteability   = "path_rerouteability"
	QualifierPathZonePressure     = "path_zone_pressure"
	QualifierPathAffectedPressure = "path_affected_pressure"
	QualifierRecoveryLatencyHours = "recovery_latency_hours"
)

// EdgeProperties свойства ребра зависимости
type EdgeProperties struct {
	Criticality   float64 `json:"criticality" firestore:"criticality"`     // 0..1
	Amplification float64 `json:"amplification" firestore:"amplification"` // Коэффициент усиления влияния
	LatencyHours  int     `json:"latencyHours" firestore:"latencyHours"`   // Задержка распространения влияния в часах
}

// EdgeMetadata метаданные ребра
type EdgeMetadata struct {
	SourceFrameID string    `json:"sourceFrameId" firestore:"sourceFrameId"`
	CreatedAt     time.Time `json:"createdAt" firestore:"createdAt"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt" firestore:"lastUpdatedAt"`
}

// Validate проверяет валидность DependencyEdge
func (e *DependencyEdge) Validate() error {
	if e.SourceFragmentID == "" {
		return fmt.Errorf("sourceFragmentId is required")
	}
	if e.TargetFragmentID == "" {
		return fmt.Errorf("targetFragmentId is required")
	}
	if e.SourceFragmentID == e.TargetFragmentID {
		return fmt.Errorf("sourceFragmentId and targetFragmentId must be different")
	}
	if e.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if e.Type == "" {
		return fmt.Errorf("type is required")
	}
	if e.Properties.Criticality < 0 || e.Properties.Criticality > 1 {
		return fmt.Errorf("properties.criticality must be between 0 and 1")
	}
	if e.Properties.Amplification < 0 {
		return fmt.Errorf("properties.amplification must be non-negative")
	}

	// Validate influence qualifiers if this is an INFLUENCES edge
	if e.Type == DependencyTypeInfluences {
		qualifiers, err := ExtractInfluenceQualifiers(e)
		if err == nil && qualifiers != nil {
			if err := ValidateInfluenceQualifiers(qualifiers); err != nil {
				return fmt.Errorf("invalid influence qualifiers: %w", err)
			}
		}
	}

	return nil
}

// IsActive checks whether the edge is active (effective) at the given time.
// Returns true if the edge's validity window includes the given time.
// - If ValidFrom is set and time is before it, the edge hasn't started yet.
// - If ValidTo is set and time is after it, the edge has ended.
// - Otherwise, the edge is active.
func (e *DependencyEdge) IsActive(at time.Time) bool {
	if e.ValidFrom != nil && at.Before(*e.ValidFrom) {
		return false // edge hasn't started yet
	}
	if e.ValidTo != nil && at.After(*e.ValidTo) {
		return false // edge has ended
	}
	return true
}

// IsActiveNow checks whether the edge is active at the current moment.
func (e *DependencyEdge) IsActiveNow() bool {
	return e.IsActive(time.Now())
}

// IsHistorical checks whether the edge has ended (ValidTo is in the past).
// Historical edges are not deleted but are filtered from real-time propagation.
func (e *DependencyEdge) IsHistorical() bool {
	return e.ValidTo != nil && time.Now().After(*e.ValidTo)
}

// IsFuture checks whether the edge hasn't started yet (ValidFrom is in the future).
// Future edges represent projected/planned relationships.
func (e *DependencyEdge) IsFuture() bool {
	return e.ValidFrom != nil && time.Now().Before(*e.ValidFrom)
}

// GetEdgeModality derives the edge modality from ValidFrom/ValidTo dates.
// Returns: "actual", "projected", or "historical".
// This is used for propagation filtering and UI display.
func (e *DependencyEdge) GetEdgeModality() string {
	// Check explicit qualifier first
	if e.Qualifiers != nil {
		if modality, ok := e.Qualifiers["edgeModality"].(string); ok && modality != "" {
			return modality
		}
	}
	// Derive from dates
	if e.IsFuture() {
		return "projected"
	}
	if e.IsHistorical() {
		return "historical"
	}
	return "actual"
}

// =============================================================================
// DEPENDENCY FACET HELPERS
// =============================================================================

// GetFacet extracts the dependency facet from edge qualifiers.
// Returns empty string if no facet is set.
func (e *DependencyEdge) GetFacet() DependencyFacet {
	if e.Qualifiers == nil {
		return ""
	}
	if facet, ok := e.Qualifiers["facet"].(string); ok {
		return DependencyFacet(facet)
	}
	return ""
}

// SetFacet sets the dependency facet in edge qualifiers.
func (e *DependencyEdge) SetFacet(facet DependencyFacet) {
	if e.Qualifiers == nil {
		e.Qualifiers = make(map[string]interface{})
	}
	e.Qualifiers["facet"] = string(facet)
}

// GetSubstitutability extracts the substitutability score from edge qualifiers.
// Returns 0.0 if not set. Range: 0..1 (0 = irreplaceable, 1 = easily substituted).
func (e *DependencyEdge) GetSubstitutability() float64 {
	if e.Qualifiers == nil {
		return 0.0
	}
	if v, ok := e.Qualifiers["substitutability"].(float64); ok {
		return v
	}
	return 0.0
}

// SetSubstitutability sets the substitutability score in edge qualifiers.
func (e *DependencyEdge) SetSubstitutability(v float64) {
	if e.Qualifiers == nil {
		e.Qualifiers = make(map[string]interface{})
	}
	e.Qualifiers["substitutability"] = v
}

// GetThroughputShare extracts share of flow transmitted through this dependency.
// Range: 0..1. Returns 0 when unknown.
func (e *DependencyEdge) GetThroughputShare() float64 {
	if e == nil {
		return 0
	}
	v := e.getQualifierFloat(
		QualifierThroughputShare,
		"flow_share",
		"shipment_share",
		"route_share",
	)
	return clamp01(v)
}

// GetReroutePenalty extracts reroute friction score.
// Range: 0..1. Returns 0 when unknown.
func (e *DependencyEdge) GetReroutePenalty() float64 {
	if e == nil {
		return 0
	}
	v := e.getQualifierFloat(
		QualifierReroutePenalty,
		"reroute_penalty_score",
		"substitution_penalty",
	)
	return clamp01(v)
}

// GetCapacityConstraint extracts capacity bottleneck score.
// Range: 0..1. Returns 0 when unknown.
func (e *DependencyEdge) GetCapacityConstraint() float64 {
	if e == nil {
		return 0
	}
	v := e.getQualifierFloat(
		QualifierCapacityConstraint,
		"capacity_utilization",
		"congestion_score",
	)
	// Some sources provide capacity_utilization as 0..100.
	if v > 1 && v <= 100 {
		v = v / 100
	}
	return clamp01(v)
}

// GetRerouteLatencyDeltaHours extracts additional latency expected when rerouting.
// Returns hours (>=0). Returns 0 when unknown.
func (e *DependencyEdge) GetRerouteLatencyDeltaHours() float64 {
	if e == nil {
		return 0
	}
	v := e.getQualifierFloat(
		QualifierRerouteLatencyDeltaHrs,
		"latency_delta_hours",
		"reroute_delay_hours",
	)
	if v < 0 {
		return 0
	}
	return v
}

// GetRerouteCostDeltaPct extracts reroute cost increase percent.
// Returns percent in [0..+inf). Returns 0 when unknown.
func (e *DependencyEdge) GetRerouteCostDeltaPct() float64 {
	if e == nil {
		return 0
	}
	v := e.getQualifierFloat(
		QualifierRerouteCostDeltaPct,
		"cost_delta_pct",
		"reroute_cost_percent",
	)
	if v < 0 {
		return 0
	}
	return v
}

func (e *DependencyEdge) getQualifierFloat(keys ...string) float64 {
	if e == nil {
		return 0
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if e.Qualifiers != nil {
			if v, ok := parseFloatAny(e.Qualifiers[key]); ok {
				return v
			}
		}
		if e.Metrics != nil {
			if mv, ok := e.Metrics[key]; ok {
				return mv.Value
			}
		}
	}
	return 0
}

func parseFloatAny(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		s = strings.TrimSuffix(s, "%")
		s = strings.ReplaceAll(s, ",", "")
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// HasAnyTransmissionQualifier checks whether edge has at least one transmission-related qualifier/metric.
func (e *DependencyEdge) HasAnyTransmissionQualifier() bool {
	if e == nil {
		return false
	}
	keys := []string{
		QualifierThroughputShare,
		QualifierReroutePenalty,
		QualifierRerouteLatencyDeltaHrs,
		QualifierRerouteCostDeltaPct,
		QualifierCapacityConstraint,
		"substitutability",
		"flow_share",
		"shipment_share",
		"route_share",
		"reroute_penalty_score",
		"substitution_penalty",
		"latency_delta_hours",
		"reroute_delay_hours",
		"cost_delta_pct",
		"reroute_cost_percent",
		"capacity_utilization",
		"congestion_score",
	}
	for _, key := range keys {
		if e.Qualifiers != nil {
			if _, ok := parseFloatAny(e.Qualifiers[key]); ok {
				return true
			}
		}
		if e.Metrics != nil {
			if _, ok := e.Metrics[key]; ok {
				return true
			}
		}
	}
	return false
}

// IsTransmissionSensitive returns true for DEPENDS_ON edges that likely need transmission semantics.
func (e *DependencyEdge) IsTransmissionSensitive() bool {
	if e == nil || e.Type != DependencyTypeDependsOn {
		return false
	}
	facet := e.GetFacet()
	if facet == "" {
		// Unknown facet is still treated as sensitive when transmission qualifiers already exist.
		return e.HasAnyTransmissionQualifier()
	}
	switch facet {
	case FacetLogistics, FacetInfrastructure, FacetSupply, FacetDistribution:
		return true
	default:
		return e.HasAnyTransmissionQualifier()
	}
}

// AllDependencyFacets returns all valid dependency facets.
func AllDependencyFacets() []DependencyFacet {
	return []DependencyFacet{
		FacetCommunication,
		FacetInfrastructure,
		FacetPayment,
		FacetDistribution,
		FacetLogistics,
		FacetSupply,
		FacetLabor,
		FacetLegitimization,
		FacetEnforcement,
		FacetRegulatory,
	}
}

// IsValidFacet checks if a facet string is a valid DependencyFacet.
func IsValidFacet(s string) bool {
	for _, f := range AllDependencyFacets() {
		if string(f) == s {
			return true
		}
	}
	return false
}
