package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// DependencyEdge is a typed edge between two HorizonFragment documents.
//
// As of schema_version 2.0 the edge is aligned with the AgentHorde taxonomy:
//   - Relation is one of ah.RelationName (snake_case canonical identifier).
//   - Attributes is the AH-canonical attribute map (carrier=edge in
//     ATTRIBUTE_SPEC). FE getter helpers and policy code MUST read from
//     Attributes for any AH-promoted concept.
//   - Qualifiers is preserved as a free-form runtime/policy annotation map
//     (legacy carry-over for things that have not been promoted to a closed
//     attribute spec yet, plus runtime stamps such as policyStatus).
//
// Anything previously expressed as a dedicated DependencyType / DependencyFacet
// has been removed from this file. See migration_map.yaml + field_inventory.yaml.
type DependencyEdge struct {
	ID               string                 `json:"id" firestore:"id"`
	TenantID         string                 `json:"tenantId" firestore:"tenantId"`
	SourceFragmentID string                 `json:"sourceFragmentId" firestore:"sourceFragmentId"`
	TargetFragmentID string                 `json:"targetFragmentId" firestore:"targetFragmentId"`
	Relation         ah.RelationName        `json:"relation" firestore:"relation"`
	Properties       EdgeProperties         `json:"properties" firestore:"properties"`
	Attributes       map[string]interface{} `json:"attributes,omitempty" firestore:"attributes,omitempty"`
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

// Validate enforces the minimal structural invariants of a DependencyEdge.
//
// Phase 1 keeps this intentionally narrow: relation/endpoint kind validation
// against ah.RelationEndpointKinds is reintroduced in Phase 2 once policy code
// is rewritten on top of the generated taxonomy.
func (e *DependencyEdge) Validate() error {
	if e == nil {
		return fmt.Errorf("dependency edge is nil")
	}
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
	if e.Relation == "" {
		return fmt.Errorf("relation is required")
	}
	if !ah.IsKnownRelation(e.Relation) {
		return fmt.Errorf("unknown relation: %q", e.Relation)
	}
	if e.Properties.Criticality < 0 || e.Properties.Criticality > 1 {
		return fmt.Errorf("properties.criticality must be between 0 and 1")
	}
	if e.Properties.Amplification < 0 {
		return fmt.Errorf("properties.amplification must be non-negative")
	}
	return nil
}

// dependencyEdgeBase is a type alias used purely to break the recursive
// MarshalJSON / UnmarshalJSON loop. It is NEVER referenced outside the
// custom marshalers below.
type dependencyEdgeBase DependencyEdge

// dependencyEdgeJSONShim mirrors DependencyEdge 1:1 except it accepts
// the legacy `type` field (UPPER_SNAKE DependencyType) alongside the
// canonical `relation`. Persisted Firestore docs and test fixtures
// written before P1.04 are auto-upgraded on read via
// NormalizeWireRelationStrict.
type dependencyEdgeJSONShim struct {
	*dependencyEdgeBase
	LegacyType string `json:"type,omitempty"`
}

// UnmarshalJSON accepts both the new wire shape (`relation`) and the
// legacy shape (`type: "OWNS"`). Legacy values are translated through
// NormalizeWireRelationStrict (which delegates to graph_intent.go's
// legacyRelationAliases / migration_map.yaml). Unknown legacy strings
// fall through to a lowercase guess so caller-side validation
// (Validate, scout pipeline, migrate-cli) can still flag them.
func (e *DependencyEdge) UnmarshalJSON(data []byte) error {
	shim := dependencyEdgeJSONShim{dependencyEdgeBase: (*dependencyEdgeBase)(e)}
	if err := json.Unmarshal(data, &shim); err != nil {
		return err
	}
	if e.Relation == "" {
		legacy := strings.TrimSpace(shim.LegacyType)
		if legacy != "" {
			if canonical, ok := NormalizeWireRelationStrict(legacy); ok {
				e.Relation = canonical
			} else if rel, ok := ah.NormalizeRelation(strings.ToLower(legacy)); ok {
				e.Relation = rel
			}
		}
	}
	return nil
}

// IsActive checks whether the edge is active (effective) at the given time.
func (e *DependencyEdge) IsActive(at time.Time) bool {
	if e == nil {
		return false
	}
	if e.ValidFrom != nil && at.Before(*e.ValidFrom) {
		return false
	}
	if e.ValidTo != nil && at.After(*e.ValidTo) {
		return false
	}
	return true
}

// IsActiveNow checks whether the edge is active at the current moment.
func (e *DependencyEdge) IsActiveNow() bool {
	return e.IsActive(time.Now())
}

// IsHistorical checks whether the edge has ended (ValidTo is in the past).
func (e *DependencyEdge) IsHistorical() bool {
	return e != nil && e.ValidTo != nil && time.Now().After(*e.ValidTo)
}

// IsFuture checks whether the edge has not started yet (ValidFrom is in the future).
func (e *DependencyEdge) IsFuture() bool {
	return e != nil && e.ValidFrom != nil && time.Now().Before(*e.ValidFrom)
}

// GetEdgeModality derives the edge modality from ValidFrom/ValidTo dates,
// preferring an explicit attributes.edge_modality / qualifiers.edgeModality
// override when present. Returns one of: "actual" | "projected" | "historical".
func (e *DependencyEdge) GetEdgeModality() string {
	if e == nil {
		return "actual"
	}
	if e.Attributes != nil {
		if modality, ok := e.Attributes[AttrEdgeModality].(string); ok && modality != "" {
			return modality
		}
	}
	if e.Qualifiers != nil {
		if modality, ok := e.Qualifiers["edgeModality"].(string); ok && modality != "" {
			return modality
		}
	}
	if e.IsFuture() {
		return "projected"
	}
	if e.IsHistorical() {
		return "historical"
	}
	return "actual"
}
