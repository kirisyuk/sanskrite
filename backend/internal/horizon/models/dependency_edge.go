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
