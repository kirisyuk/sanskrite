package models

import (
	"fmt"
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
	// DOMINATES: org/facility dominates a market (leader relationship).
	DependencyTypeDominates DependencyType = "DOMINATES"
	// HAS_SUBMARKET: market hierarchy (parent market → child/narrower market).
	// Example: /market/sa/marketing HAS_SUBMARKET /market/sa/event-marketing
	// Direction: broader market → narrower market.
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

	// --- Legacy types for migration ---
	// WORKS_FOR: legacy employment (prefer EMPLOYED_BY). Person works for org.
	DependencyTypeWorksFor DependencyType = "WORKS_FOR"
	// KEY_PERSON: legacy key person relationship. Should be PERSON → ORG.
	DependencyTypeKeyPerson DependencyType = "KEY_PERSON"
	// HAS_KEY_PERSON: legacy inverse (ORG has key person). Should be normalized to KEY_PERSON (PERSON → ORG).
	DependencyTypeHasKeyPerson DependencyType = "HAS_KEY_PERSON"

	// --- OSINT-lite / governance extensions ---
	// AUDITED_BY: external auditor relationship (company -> audit firm).
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

	// --- Community hierarchy ---
	// HAS_SUBGROUP: community hierarchy (parent group → child/narrower group).
	// Used for dynasties, tribes, clans, professional networks with internal structure.
	// Direction: broader community → narrower sub-community/branch/clan.
	// Examples:
	//   /community/sa/dynasty/house-of-saud HAS_SUBGROUP /community/sa/dynasty/sudairi-seven
	//   /community/af/tribe/pashtun HAS_SUBGROUP /community/af/tribe/durrani
	//   /community/kz/tribe/senior-zhuz HAS_SUBGROUP /community/kz/tribe/uysun
	DependencyTypeHasSubgroup DependencyType = "HAS_SUBGROUP"
)

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
