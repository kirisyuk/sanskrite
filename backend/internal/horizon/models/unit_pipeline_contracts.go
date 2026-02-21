package models

import "strings"

// UnitCode is a controlled unit taxonomy for L1 unit graph.
type UnitCode string

const (
	UnitCodeConsumer     UnitCode = "consumer"
	UnitCodeEnterprise   UnitCode = "enterprise"
	UnitCodeCoreOps      UnitCode = "core_ops"
	UnitCodeIT           UnitCode = "it"
	UnitCodeLogistics    UnitCode = "logistics"
	UnitCodeCorporate    UnitCode = "corporate"
	UnitCodeRND          UnitCode = "rnd"
	UnitCodeUnclassified UnitCode = "unclassified"
)

var unitCodeAliases = map[string]UnitCode{
	"network_ops":   UnitCodeCoreOps,
	"extraction":    UnitCodeCoreOps,
	"processing":    UnitCodeCoreOps,
	"merchandising": UnitCodeCoreOps,
	"product":       UnitCodeCoreOps,
	"cloud_ops":     UnitCodeCoreOps,
}

// NormalizeUnitCode returns a canonical unit code from direct value or alias.
func NormalizeUnitCode(raw string) UnitCode {
	s := strings.TrimSpace(strings.ToLower(raw))
	switch UnitCode(s) {
	case UnitCodeConsumer, UnitCodeEnterprise, UnitCodeCoreOps, UnitCodeIT, UnitCodeLogistics, UnitCodeCorporate, UnitCodeRND, UnitCodeUnclassified:
		return UnitCode(s)
	}
	if canon, ok := unitCodeAliases[s]; ok {
		return canon
	}
	return UnitCodeUnclassified
}

// OrgUnitStatus is a lightweight lifecycle marker for an organizational unit.
type OrgUnitStatus string

const (
	OrgUnitStatusActive  OrgUnitStatus = "ACTIVE"
	OrgUnitStatusDormant OrgUnitStatus = "DORMANT"
	OrgUnitStatusMerged  OrgUnitStatus = "MERGED"
	OrgUnitStatusUnknown OrgUnitStatus = "UNKNOWN"
)

// OrgUnitProfile captures an L1 unit attached to a macro organization.
type OrgUnitProfile struct {
	UnitID          string        `json:"unitId" firestore:"unitId"`
	ParentOrgRef    string        `json:"parentOrgRef" firestore:"parentOrgRef"` // /org/... or /gov/...
	UnitCode        UnitCode      `json:"unitCode" firestore:"unitCode"`
	Label           string        `json:"label" firestore:"label"`
	Status          OrgUnitStatus `json:"status,omitempty" firestore:"status,omitempty"`
	Geo             []string      `json:"geo,omitempty" firestore:"geo,omitempty"`
	LegalEntityRefs []string      `json:"legalEntityRefs,omitempty" firestore:"legalEntityRefs,omitempty"` // many-to-many
	Aliases         []string      `json:"aliases,omitempty" firestore:"aliases,omitempty"`
	Confidence      float64       `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Sources         []string      `json:"sources,omitempty" firestore:"sources,omitempty"`
}

// UnitSupplyChannel describes a broad distribution channel at L1.
type UnitSupplyChannel string

const (
	UnitSupplyChannelDirect      UnitSupplyChannel = "direct"
	UnitSupplyChannelDistributor UnitSupplyChannel = "distributor"
	UnitSupplyChannelRetail      UnitSupplyChannel = "retail"
	UnitSupplyChannelUnknown     UnitSupplyChannel = "unknown"
)

// UnitSupplyLinkQualifier keeps org-level truth and adds unit qualifiers when available.
type UnitSupplyLinkQualifier struct {
	SupplierOrgRef      string            `json:"supplierOrgRef" firestore:"supplierOrgRef"`
	CustomerOrgRef      string            `json:"customerOrgRef" firestore:"customerOrgRef"`
	SupplierUnitRef     *string           `json:"supplierUnitRef,omitempty" firestore:"supplierUnitRef,omitempty"`
	CustomerUnitRef     *string           `json:"customerUnitRef,omitempty" firestore:"customerUnitRef,omitempty"`
	BroadProductBuckets []string          `json:"broadProductBuckets,omitempty" firestore:"broadProductBuckets,omitempty"`
	Channel             UnitSupplyChannel `json:"channel,omitempty" firestore:"channel,omitempty"`
	GeoScope            []string          `json:"geoScope,omitempty" firestore:"geoScope,omitempty"`
	Confidence          float64           `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Sources             []string          `json:"sources,omitempty" firestore:"sources,omitempty"`
}

// UnitGraphDirection is used by lightweight L1 unit graph edges.
type UnitGraphDirection string

const (
	UnitGraphDirectionUpstream   UnitGraphDirection = "upstream"
	UnitGraphDirectionDownstream UnitGraphDirection = "downstream"
)

// UnitGraphEdge is a compact visual edge between unit contours.
type UnitGraphEdge struct {
	EdgeID          string             `json:"edgeId" firestore:"edgeId"`
	SupplierOrgRef  string             `json:"supplierOrgRef" firestore:"supplierOrgRef"`
	CustomerOrgRef  string             `json:"customerOrgRef" firestore:"customerOrgRef"`
	SupplierUnitRef string             `json:"supplierUnitRef" firestore:"supplierUnitRef"` // unitId or "unclassified"
	CustomerUnitRef string             `json:"customerUnitRef" firestore:"customerUnitRef"` // unitId or "unclassified"
	Direction       UnitGraphDirection `json:"direction" firestore:"direction"`
	Weight          float64            `json:"weight,omitempty" firestore:"weight,omitempty"`
	Confidence      float64            `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

// UnitDiveMode controls whether P5 dive was requested manually or by policy.
type UnitDiveMode string

const (
	UnitDiveModeManual     UnitDiveMode = "MANUAL"
	UnitDiveModeAutoPolicy UnitDiveMode = "AUTO_POLICY"
)

// UnitDiveDepth controls depth profile in P5.
type UnitDiveDepth string

const (
	UnitDiveDepthSemi UnitDiveDepth = "semi"
	UnitDiveDepthDeep UnitDiveDepth = "deep"
)

// UnitDiveReason captures why a unit-level deep dive was triggered.
type UnitDiveReason string

const (
	UnitDiveReasonUserClick       UnitDiveReason = "user_click"
	UnitDiveReasonHighCriticality UnitDiveReason = "high_criticality"
	UnitDiveReasonLowConfidence   UnitDiveReason = "low_confidence"
	UnitDiveReasonAnalystRequest  UnitDiveReason = "analyst_request"
)

// UnitDiveBudget constrains depth and expansion in P5.
type UnitDiveBudget struct {
	MaxHopsUpstream   int `json:"maxHopsUpstream,omitempty" firestore:"maxHopsUpstream,omitempty"`
	MaxHopsDownstream int `json:"maxHopsDownstream,omitempty" firestore:"maxHopsDownstream,omitempty"`
	MaxNewNodes       int `json:"maxNewNodes,omitempty" firestore:"maxNewNodes,omitempty"`
}

// UnitDiveRequest is the explicit contract required to start P5.
type UnitDiveRequest struct {
	RequestID       string         `json:"requestId" firestore:"requestId"`
	Mode            UnitDiveMode   `json:"mode" firestore:"mode"`
	SupplierOrgRef  string         `json:"supplierOrgRef" firestore:"supplierOrgRef"`
	CustomerOrgRef  string         `json:"customerOrgRef" firestore:"customerOrgRef"`
	SupplierUnitRef string         `json:"supplierUnitRef" firestore:"supplierUnitRef"`
	CustomerUnitRef string         `json:"customerUnitRef" firestore:"customerUnitRef"`
	Depth           UnitDiveDepth  `json:"depth,omitempty" firestore:"depth,omitempty"`
	Reason          UnitDiveReason `json:"reason,omitempty" firestore:"reason,omitempty"`
	Budget          UnitDiveBudget `json:"budget,omitempty" firestore:"budget,omitempty"`
}

// UnitDiveStatus is a terminal status of unit dive execution.
type UnitDiveStatus string

const (
	UnitDiveStatusCompleted UnitDiveStatus = "COMPLETED"
	UnitDiveStatusPartial   UnitDiveStatus = "PARTIAL"
	UnitDiveStatusFailed    UnitDiveStatus = "FAILED"
)

// UnitDiveResult is a compact output contract for P5.
type UnitDiveResult struct {
	RequestID             string         `json:"requestId" firestore:"requestId"`
	Status                UnitDiveStatus `json:"status" firestore:"status"`
	ResolvedLegalEntities []string       `json:"resolvedLegalEntities,omitempty" firestore:"resolvedLegalEntities,omitempty"`
	UpstreamChain         []string       `json:"upstreamChain,omitempty" firestore:"upstreamChain,omitempty"`
	DownstreamChain       []string       `json:"downstreamChain,omitempty" firestore:"downstreamChain,omitempty"`
	NewSupplyLinks        []string       `json:"newSupplyLinks,omitempty" firestore:"newSupplyLinks,omitempty"`
	Confidence            float64        `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Notes                 string         `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// UnitPipelinePhase models explicit phased execution for macro->unit->dive flow.
type UnitPipelinePhase string

const (
	UnitPipelinePhaseP0 UnitPipelinePhase = "P0" // Macro Anchor
	UnitPipelinePhaseP1 UnitPipelinePhase = "P1" // Unit Discovery
	UnitPipelinePhaseP2 UnitPipelinePhase = "P2" // Unit Mapping
	UnitPipelinePhaseP3 UnitPipelinePhase = "P3" // Critic Lite
	UnitPipelinePhaseP4 UnitPipelinePhase = "P4" // Dive Planning
	UnitPipelinePhaseP5 UnitPipelinePhase = "P5" // Unit Dive Execution
)

const (
	// StateKeyUnitPipelinePhase is stored in ScoutingJob.State.
	StateKeyUnitPipelinePhase = "unitPipelinePhase"
	// StateKeyUnitProfiles stores []OrgUnitProfile in ScoutingJob.State.
	StateKeyUnitProfiles = "unitProfiles"
	// StateKeyUnitDiveRequest stores UnitDiveRequest payload in ScoutingJob.State.
	StateKeyUnitDiveRequest = "unitDiveRequest"
	// StateKeyDeferredScopeAdd when true, scope is added only after fragment is created (max 5 per lens).
	StateKeyDeferredScopeAdd = "deferredScopeAdd"
)

// NormalizeUnitPipelinePhase canonicalizes raw phase values.
// Default is P3 because auto flow is expected to stop at Critic Lite unless dive is requested.
func NormalizeUnitPipelinePhase(raw string) UnitPipelinePhase {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(UnitPipelinePhaseP0):
		return UnitPipelinePhaseP0
	case string(UnitPipelinePhaseP1):
		return UnitPipelinePhaseP1
	case string(UnitPipelinePhaseP2):
		return UnitPipelinePhaseP2
	case string(UnitPipelinePhaseP3):
		return UnitPipelinePhaseP3
	case string(UnitPipelinePhaseP4):
		return UnitPipelinePhaseP4
	case string(UnitPipelinePhaseP5):
		return UnitPipelinePhaseP5
	default:
		return UnitPipelinePhaseP3
	}
}

// IsAutoPhase returns true for phases that can run automatically in default flow.
func (p UnitPipelinePhase) IsAutoPhase() bool {
	return p == UnitPipelinePhaseP0 ||
		p == UnitPipelinePhaseP1 ||
		p == UnitPipelinePhaseP2 ||
		p == UnitPipelinePhaseP3
}
