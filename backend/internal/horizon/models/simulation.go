package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SimulationRunStatus string

const (
	SimulationRunStatusPending     SimulationRunStatus = "PENDING"
	SimulationRunStatusQueued      SimulationRunStatus = "QUEUED"
	SimulationRunStatusRunning     SimulationRunStatus = "RUNNING"
	SimulationRunStatusAggregating SimulationRunStatus = "AGGREGATING"
	SimulationRunStatusReviewing   SimulationRunStatus = "REVIEWING"
	SimulationRunStatusSucceeded   SimulationRunStatus = "SUCCEEDED"
	SimulationRunStatusFailed      SimulationRunStatus = "FAILED"
	SimulationRunStatusDegraded    SimulationRunStatus = "DEGRADED"
	SimulationRunStatusCancelled   SimulationRunStatus = "CANCELLED"
	SimulationRunStatusExpired     SimulationRunStatus = "EXPIRED"
)

type SimulationWorldStatus string

const (
	SimulationWorldStatusPending   SimulationWorldStatus = "PENDING"
	SimulationWorldStatusRunning   SimulationWorldStatus = "RUNNING"
	SimulationWorldStatusSucceeded SimulationWorldStatus = "SUCCEEDED"
	SimulationWorldStatusFailed    SimulationWorldStatus = "FAILED"
	SimulationWorldStatusDegraded  SimulationWorldStatus = "DEGRADED"
)

type SimulationArtifactKind string

const (
	SimulationArtifactKindBundle       SimulationArtifactKind = "bundle"
	SimulationArtifactKindWorldSummary SimulationArtifactKind = "world_summary"
	SimulationArtifactKindReview       SimulationArtifactKind = "review"
	SimulationArtifactKindTrace        SimulationArtifactKind = "trace"
)

type SimulationShock struct {
	Text         string     `json:"text,omitempty" firestore:"text,omitempty"`
	Type         string     `json:"type,omitempty" firestore:"type,omitempty"`
	Axis         string     `json:"axis,omitempty" firestore:"axis,omitempty"`
	Polarity     string     `json:"polarity,omitempty" firestore:"polarity,omitempty"`
	Magnitude    float64    `json:"magnitude,omitempty" firestore:"magnitude,omitempty"`
	LocationPath string     `json:"locationPath,omitempty" firestore:"locationPath,omitempty"`
	ActorPaths   []string   `json:"actorPaths,omitempty" firestore:"actorPaths,omitempty"`
	SeedPaths    []string   `json:"seedPaths,omitempty" firestore:"seedPaths,omitempty"`
	TimeAnchor   *time.Time `json:"timeAnchor,omitempty" firestore:"timeAnchor,omitempty"`
}

type SimulationRunRequest struct {
	Question     string           `json:"question" firestore:"question"`
	Signal       *SimulationShock `json:"signal,omitempty" firestore:"signal,omitempty"`
	LensID       string           `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	SeedPaths    []string         `json:"seedPaths,omitempty" firestore:"seedPaths,omitempty"`
	ArtifactKeys []string         `json:"artifactKeys,omitempty" firestore:"artifactKeys,omitempty"`
	WorldCount   int              `json:"worldCount,omitempty" firestore:"worldCount,omitempty"`
	TimeHorizon  string           `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	TickCount    int              `json:"tickCount,omitempty" firestore:"tickCount,omitempty"`
	Audience     string           `json:"audience,omitempty" firestore:"audience,omitempty"`
	Mode         string           `json:"mode,omitempty" firestore:"mode,omitempty"`
	ProjectID    string           `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	DomainModes  []string         `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
}

func (r *SimulationRunRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("simulation request is nil")
	}
	r.Question = strings.TrimSpace(r.Question)
	r.LensID = strings.TrimSpace(r.LensID)
	r.ProjectID = strings.TrimSpace(r.ProjectID)
	r.Audience = strings.TrimSpace(r.Audience)
	r.Mode = strings.TrimSpace(r.Mode)
	r.TimeHorizon = strings.TrimSpace(r.TimeHorizon)
	r.SeedPaths = compactSimulationStrings(r.SeedPaths)
	r.ArtifactKeys = compactSimulationStrings(r.ArtifactKeys)
	r.DomainModes = compactSimulationStrings(r.DomainModes)
	if r.Question == "" {
		return fmt.Errorf("question is required")
	}
	if r.WorldCount <= 0 {
		r.WorldCount = 8
	}
	if r.WorldCount > 32 {
		r.WorldCount = 32
	}
	if r.TickCount <= 0 {
		r.TickCount = 6
	}
	if r.TickCount > 24 {
		r.TickCount = 24
	}
	if r.Mode == "" {
		r.Mode = "default"
	}
	if r.Signal != nil {
		if err := r.Signal.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SimulationShock) Validate() error {
	if s == nil {
		return nil
	}
	s.Text = strings.TrimSpace(s.Text)
	s.Type = strings.TrimSpace(strings.ToLower(s.Type))
	s.Axis = strings.TrimSpace(s.Axis)
	s.Polarity = strings.TrimSpace(strings.ToLower(s.Polarity))
	s.LocationPath = strings.TrimSpace(s.LocationPath)
	s.ActorPaths = compactSimulationStrings(s.ActorPaths)
	s.SeedPaths = compactSimulationStrings(s.SeedPaths)
	if s.Magnitude <= 0 {
		s.Magnitude = 0.35
	}
	if s.Magnitude > 1 {
		s.Magnitude = 1
	}
	if s.Polarity == "" {
		s.Polarity = "risk"
	}
	if s.Polarity != "risk" && s.Polarity != "opportunity" {
		return fmt.Errorf("signal polarity must be risk or opportunity")
	}
	if s.Axis != "" && !IsValidAxis(s.Axis) {
		return fmt.Errorf("signal axis is invalid: %s", s.Axis)
	}
	if s.Text == "" && s.Type == "" && s.Axis == "" {
		return fmt.Errorf("signal text, type, or axis is required")
	}
	return nil
}

type SimulationExecutionConfig struct {
	WorldCount  int      `json:"worldCount" firestore:"worldCount"`
	TickCount   int      `json:"tickCount" firestore:"tickCount"`
	TimeHorizon string   `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	Audience    string   `json:"audience,omitempty" firestore:"audience,omitempty"`
	Mode        string   `json:"mode,omitempty" firestore:"mode,omitempty"`
	DomainModes []string `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
}

type SimulationBaselineRefs struct {
	LensID         string   `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	ScopePaths     []string `json:"scopePaths,omitempty" firestore:"scopePaths,omitempty"`
	FragmentIDs    []string `json:"fragmentIds,omitempty" firestore:"fragmentIds,omitempty"`
	FragmentPaths  []string `json:"fragmentPaths,omitempty" firestore:"fragmentPaths,omitempty"`
	EdgeIDs        []string `json:"edgeIds,omitempty" firestore:"edgeIds,omitempty"`
	FactIDs        []string `json:"factIds,omitempty" firestore:"factIds,omitempty"`
	ArtifactKeys   []string `json:"artifactKeys,omitempty" firestore:"artifactKeys,omitempty"`
	ArtifactIDs    []string `json:"artifactIds,omitempty" firestore:"artifactIds,omitempty"`
	SnapshotDigest string   `json:"snapshotDigest,omitempty" firestore:"snapshotDigest,omitempty"`
}

type SimulationProgress struct {
	TotalWorlds     int `json:"totalWorlds" firestore:"totalWorlds"`
	CompletedWorlds int `json:"completedWorlds" firestore:"completedWorlds"`
	FailedWorlds    int `json:"failedWorlds" firestore:"failedWorlds"`
	CompletedTicks  int `json:"completedTicks" firestore:"completedTicks"`
	TotalTicks      int `json:"totalTicks" firestore:"totalTicks"`
}

type SimulationFailureInfo struct {
	Message    string `json:"message,omitempty" firestore:"message,omitempty"`
	RetryClass string `json:"retryClass,omitempty" firestore:"retryClass,omitempty"`
	TaskID     string `json:"taskId,omitempty" firestore:"taskId,omitempty"`
}

type SimulationReviewSummary struct {
	Status            string   `json:"status,omitempty" firestore:"status,omitempty"`
	GroundingCoverage float64  `json:"groundingCoverage,omitempty" firestore:"groundingCoverage,omitempty"`
	Rationale         string   `json:"rationale,omitempty" firestore:"rationale,omitempty"`
	Issues            []string `json:"issues,omitempty" firestore:"issues,omitempty"`
}

type SimulationSummary struct {
	OutcomeDistribution map[string]float64 `json:"outcomeDistribution,omitempty" firestore:"outcomeDistribution,omitempty"`
	KeyMechanisms       []string           `json:"keyMechanisms,omitempty" firestore:"keyMechanisms,omitempty"`
	Sensitivity         map[string]float64 `json:"sensitivity,omitempty" firestore:"sensitivity,omitempty"`
	GroundingCoverage   float64            `json:"groundingCoverage,omitempty" firestore:"groundingCoverage,omitempty"`
	BundleArtifactID    string             `json:"bundleArtifactId,omitempty" firestore:"bundleArtifactId,omitempty"`
}

type SimulationRun struct {
	ID               string                    `json:"id" firestore:"id"`
	TenantID         string                    `json:"tenantId" firestore:"tenantId"`
	ProjectID        string                    `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	Status           string                    `json:"status" firestore:"status"`
	Request          SimulationRunRequest      `json:"request" firestore:"request"`
	Execution        SimulationExecutionConfig `json:"execution" firestore:"execution"`
	Recognition      map[string]any            `json:"recognition,omitempty" firestore:"recognition,omitempty"`
	BaselineRefs     SimulationBaselineRefs    `json:"baselineRefs,omitempty" firestore:"baselineRefs,omitempty"`
	BaselineSignal   *SignalDescriptor         `json:"baselineSignal,omitempty" firestore:"baselineSignal,omitempty"`
	BaselinePreview  map[string]any            `json:"baselinePreview,omitempty" firestore:"baselinePreview,omitempty"`
	Progress         SimulationProgress        `json:"progress,omitempty" firestore:"progress,omitempty"`
	Summary          SimulationSummary         `json:"summary,omitempty" firestore:"summary,omitempty"`
	Review           SimulationReviewSummary   `json:"review,omitempty" firestore:"review,omitempty"`
	ArtifactIDs      []string                  `json:"artifactIds,omitempty" firestore:"artifactIds,omitempty"`
	AgentMeshTaskIDs []string                  `json:"agentmeshTaskIds,omitempty" firestore:"agentmeshTaskIds,omitempty"`
	SourceRefs       []SourceRef               `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	Failure          *SimulationFailureInfo    `json:"failure,omitempty" firestore:"failure,omitempty"`
	ExpiresAt        time.Time                 `json:"expiresAt" firestore:"expiresAt"`
	CreatedAt        time.Time                 `json:"createdAt" firestore:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt" firestore:"updatedAt"`
}

func (r *SimulationRun) Validate() error {
	if r == nil {
		return fmt.Errorf("simulation run is nil")
	}
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if err := r.Request.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.Status) == "" {
		r.Status = string(SimulationRunStatusPending)
	}
	if r.Execution.WorldCount <= 0 {
		r.Execution.WorldCount = r.Request.WorldCount
	}
	if r.Execution.TickCount <= 0 {
		r.Execution.TickCount = r.Request.TickCount
	}
	r.Execution.DomainModes = compactSimulationStrings(r.Execution.DomainModes)
	r.ArtifactIDs = compactSimulationStrings(r.ArtifactIDs)
	r.AgentMeshTaskIDs = compactSimulationStrings(r.AgentMeshTaskIDs)
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}
	return nil
}

type SimulationWorld struct {
	ID                 string             `json:"id" firestore:"id"`
	TenantID           string             `json:"tenantId" firestore:"tenantId"`
	RunID              string             `json:"runId" firestore:"runId"`
	Status             string             `json:"status" firestore:"status"`
	WorldIndex         int                `json:"worldIndex" firestore:"worldIndex"`
	RandomSeed         int64              `json:"randomSeed" firestore:"randomSeed"`
	BootstrapDigest    string             `json:"bootstrapDigest,omitempty" firestore:"bootstrapDigest,omitempty"`
	PopulationDigest   string             `json:"populationDigest,omitempty" firestore:"populationDigest,omitempty"`
	StepCount          int                `json:"stepCount,omitempty" firestore:"stepCount,omitempty"`
	OutcomeVector      map[string]float64 `json:"outcomeVector,omitempty" firestore:"outcomeVector,omitempty"`
	DominantMechanisms []string           `json:"dominantMechanisms,omitempty" firestore:"dominantMechanisms,omitempty"`
	Uncertainty        map[string]float64 `json:"uncertainty,omitempty" firestore:"uncertainty,omitempty"`
	BaselineState      map[string]any     `json:"baselineState,omitempty" firestore:"baselineState,omitempty"`
	AgentState         map[string]any     `json:"agentState,omitempty" firestore:"agentState,omitempty"`
	MarketState        map[string]any     `json:"marketState,omitempty" firestore:"marketState,omitempty"`
	InstitutionState   map[string]any     `json:"institutionState,omitempty" firestore:"institutionState,omitempty"`
	MessageBus         []map[string]any   `json:"messageBus,omitempty" firestore:"messageBus,omitempty"`
	ProtocolTraces     []map[string]any   `json:"protocolTraces,omitempty" firestore:"protocolTraces,omitempty"`
	Transactions       []map[string]any   `json:"transactions,omitempty" firestore:"transactions,omitempty"`
	RelationshipState  map[string]any     `json:"relationshipState,omitempty" firestore:"relationshipState,omitempty"`
	DerivedSignals     []SignalDescriptor `json:"derivedSignals,omitempty" firestore:"derivedSignals,omitempty"`
	StepMetrics        []map[string]any   `json:"stepMetrics,omitempty" firestore:"stepMetrics,omitempty"`
	SourceRefs         []SourceRef        `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BaselineRefs       []string           `json:"baselineRefs,omitempty" firestore:"baselineRefs,omitempty"`
	AgentMeshTaskIDs   []string           `json:"agentmeshTaskIds,omitempty" firestore:"agentmeshTaskIds,omitempty"`
	ExpiresAt          time.Time          `json:"expiresAt" firestore:"expiresAt"`
	CreatedAt          time.Time          `json:"createdAt" firestore:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt" firestore:"updatedAt"`
}

func (w *SimulationWorld) Validate() error {
	if w == nil {
		return fmt.Errorf("simulation world is nil")
	}
	if strings.TrimSpace(w.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(w.RunID) == "" {
		return fmt.Errorf("runId is required")
	}
	if strings.TrimSpace(w.Status) == "" {
		w.Status = string(SimulationWorldStatusPending)
	}
	if w.ExpiresAt.IsZero() {
		w.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}
	w.DominantMechanisms = compactSimulationStrings(w.DominantMechanisms)
	w.BaselineRefs = compactSimulationStrings(w.BaselineRefs)
	w.AgentMeshTaskIDs = compactSimulationStrings(w.AgentMeshTaskIDs)
	return nil
}

type SimulationAgentSpec struct {
	ID               string             `json:"id" firestore:"id"`
	TenantID         string             `json:"tenantId" firestore:"tenantId"`
	RunID            string             `json:"runId" firestore:"runId"`
	WorldID          string             `json:"worldId" firestore:"worldId"`
	Status           string             `json:"status" firestore:"status"`
	AgentClass       string             `json:"agentClass" firestore:"agentClass"`
	DisplayName      string             `json:"displayName" firestore:"displayName"`
	OriginKind       string             `json:"originKind,omitempty" firestore:"originKind,omitempty"`
	OriginRefs       []string           `json:"originRefs,omitempty" firestore:"originRefs,omitempty"`
	Justification    string             `json:"justification,omitempty" firestore:"justification,omitempty"`
	GeneratedBy      string             `json:"generatedBy,omitempty" firestore:"generatedBy,omitempty"`
	SchemaVersion    string             `json:"schemaVersion,omitempty" firestore:"schemaVersion,omitempty"`
	PopulationWeight float64            `json:"populationWeight" firestore:"populationWeight"`
	Traits           map[string]any     `json:"traits,omitempty" firestore:"traits,omitempty"`
	BeliefState      map[string]float64 `json:"beliefState,omitempty" firestore:"beliefState,omitempty"`
	ResourceState    map[string]float64 `json:"resourceState,omitempty" firestore:"resourceState,omitempty"`
	AllowedActions   []string           `json:"allowedActions,omitempty" firestore:"allowedActions,omitempty"`
	MessageStyle     string             `json:"messageStyle,omitempty" firestore:"messageStyle,omitempty"`
	PolicyHints      map[string]any     `json:"policyHints,omitempty" firestore:"policyHints,omitempty"`
	SourceRefs       []SourceRef        `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BaselineRefs     []string           `json:"baselineRefs,omitempty" firestore:"baselineRefs,omitempty"`
	AgentMeshTaskIDs []string           `json:"agentmeshTaskIds,omitempty" firestore:"agentmeshTaskIds,omitempty"`
	ExpiresAt        time.Time          `json:"expiresAt" firestore:"expiresAt"`
	CreatedAt        time.Time          `json:"createdAt" firestore:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt" firestore:"updatedAt"`
}

func (a *SimulationAgentSpec) Validate() error {
	if a == nil {
		return fmt.Errorf("simulation agent is nil")
	}
	if strings.TrimSpace(a.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(a.RunID) == "" {
		return fmt.Errorf("runId is required")
	}
	if strings.TrimSpace(a.WorldID) == "" {
		return fmt.Errorf("worldId is required")
	}
	if strings.TrimSpace(a.AgentClass) == "" {
		return fmt.Errorf("agentClass is required")
	}
	if strings.TrimSpace(a.DisplayName) == "" {
		return fmt.Errorf("displayName is required")
	}
	if a.PopulationWeight <= 0 {
		a.PopulationWeight = 1
	}
	a.AllowedActions = compactSimulationStrings(a.AllowedActions)
	a.OriginRefs = compactSimulationStrings(a.OriginRefs)
	a.BaselineRefs = compactSimulationStrings(a.BaselineRefs)
	a.AgentMeshTaskIDs = compactSimulationStrings(a.AgentMeshTaskIDs)
	if a.SchemaVersion == "" {
		a.SchemaVersion = "sim.agent.v1"
	}
	if a.ExpiresAt.IsZero() {
		a.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}
	return nil
}

type SimulationActionType string

const (
	SimulationActionBroadcastMessage SimulationActionType = "broadcast_message"
	SimulationActionChangeBelief     SimulationActionType = "change_belief"
	SimulationActionBuy              SimulationActionType = "buy"
	SimulationActionSell             SimulationActionType = "sell"
	SimulationActionDelayPurchase    SimulationActionType = "delay_purchase"
	SimulationActionSwitchSupplier   SimulationActionType = "switch_supplier"
	SimulationActionReserveInventory SimulationActionType = "reserve_inventory"
	SimulationActionRerouteShipment  SimulationActionType = "reroute_shipment"
	SimulationActionExpediteShipment SimulationActionType = "expedite_shipment"
	SimulationActionAllocateCapacity SimulationActionType = "allocate_capacity"
	SimulationActionReprioritizeFlow SimulationActionType = "reprioritize_flow"
	SimulationActionRequestExemption SimulationActionType = "request_exemption"
	SimulationActionHedgeCost        SimulationActionType = "hedge_cost"
	SimulationActionAdjustGuidance   SimulationActionType = "adjust_guidance"
	SimulationActionLobby            SimulationActionType = "lobby"
	SimulationActionIssueStatement   SimulationActionType = "issue_statement"
	SimulationActionEnforceRule      SimulationActionType = "enforce_rule"
	SimulationActionAdjustBudget     SimulationActionType = "adjust_budget"
	SimulationActionEscalate         SimulationActionType = "escalate"
	SimulationActionDoNothing        SimulationActionType = "do_nothing"
)

type SimulationActionDescriptor struct {
	Type           string   `json:"type"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	RequiresTarget bool     `json:"requiresTarget"`
	PayloadKeys    []string `json:"payloadKeys,omitempty"`
}

func SimulationActionCatalog() map[string]SimulationActionDescriptor {
	return map[string]SimulationActionDescriptor{
		string(SimulationActionBroadcastMessage): {Type: string(SimulationActionBroadcastMessage), Category: "narrative", Description: "Push a message into the world message bus.", PayloadKeys: []string{"delta"}},
		string(SimulationActionChangeBelief):     {Type: string(SimulationActionChangeBelief), Category: "narrative", Description: "Update beliefs, sentiment, or confidence of a target group.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionBuy):              {Type: string(SimulationActionBuy), Category: "commercial", Description: "Increase demand or place a purchase decision.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionSell):             {Type: string(SimulationActionSell), Category: "commercial", Description: "Release supply or monetize inventory/output.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionDelayPurchase):    {Type: string(SimulationActionDelayPurchase), Category: "commercial", Description: "Delay near-term demand or procurement.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionSwitchSupplier):   {Type: string(SimulationActionSwitchSupplier), Category: "sourcing", Description: "Reallocate sourcing toward an alternate supplier or route.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionReserveInventory): {Type: string(SimulationActionReserveInventory), Category: "inventory", Description: "Reserve critical inventory for priority programs.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionRerouteShipment):  {Type: string(SimulationActionRerouteShipment), Category: "logistics", Description: "Move shipments to an alternate port, lane, or mode.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionExpediteShipment): {Type: string(SimulationActionExpediteShipment), Category: "logistics", Description: "Pay or prioritize to accelerate shipment movement.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionAllocateCapacity): {Type: string(SimulationActionAllocateCapacity), Category: "operations", Description: "Allocate scarce manufacturing or logistics capacity to a target program.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionReprioritizeFlow): {Type: string(SimulationActionReprioritizeFlow), Category: "operations", Description: "Shift product mix or flow priority across programs.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionRequestExemption): {Type: string(SimulationActionRequestExemption), Category: "policy", Description: "Request a regulatory carve-out, exception, or waiver.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionHedgeCost):        {Type: string(SimulationActionHedgeCost), Category: "finance", Description: "Use treasury instruments or contracts to cushion cost volatility.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionAdjustGuidance):   {Type: string(SimulationActionAdjustGuidance), Category: "investor", Description: "Adjust external guidance or forward-looking communication.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionLobby):            {Type: string(SimulationActionLobby), Category: "policy", Description: "Apply pressure on a regulator, policy owner, or institutional actor.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionIssueStatement):   {Type: string(SimulationActionIssueStatement), Category: "policy", Description: "Publish or deliver an official position statement.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionEnforceRule):      {Type: string(SimulationActionEnforceRule), Category: "policy", Description: "Tighten compliance, inspection, or rule execution.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionAdjustBudget):     {Type: string(SimulationActionAdjustBudget), Category: "finance", Description: "Rebalance spend, working capital, or budget envelopes.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionEscalate):         {Type: string(SimulationActionEscalate), Category: "narrative", Description: "Escalate a dispute, incident, or public conflict.", RequiresTarget: true, PayloadKeys: []string{"delta"}},
		string(SimulationActionDoNothing):        {Type: string(SimulationActionDoNothing), Category: "neutral", Description: "Intentionally hold position and take no visible step."},
	}
}

func ValidSimulationActionTypes() []string {
	return []string{
		string(SimulationActionBroadcastMessage),
		string(SimulationActionChangeBelief),
		string(SimulationActionBuy),
		string(SimulationActionSell),
		string(SimulationActionDelayPurchase),
		string(SimulationActionSwitchSupplier),
		string(SimulationActionReserveInventory),
		string(SimulationActionRerouteShipment),
		string(SimulationActionExpediteShipment),
		string(SimulationActionAllocateCapacity),
		string(SimulationActionReprioritizeFlow),
		string(SimulationActionRequestExemption),
		string(SimulationActionHedgeCost),
		string(SimulationActionAdjustGuidance),
		string(SimulationActionLobby),
		string(SimulationActionIssueStatement),
		string(SimulationActionEnforceRule),
		string(SimulationActionAdjustBudget),
		string(SimulationActionEscalate),
		string(SimulationActionDoNothing),
	}
}

type SimulationAction struct {
	Type              string         `json:"type" firestore:"type"`
	ActorID           string         `json:"actorId" firestore:"actorId"`
	TargetRefs        []string       `json:"targetRefs,omitempty" firestore:"targetRefs,omitempty"`
	Payload           map[string]any `json:"payload,omitempty" firestore:"payload,omitempty"`
	Justification     string         `json:"justification,omitempty" firestore:"justification,omitempty"`
	GeneratedBy       string         `json:"generatedBy,omitempty" firestore:"generatedBy,omitempty"`
	RequiresGrounding bool           `json:"requiresGrounding,omitempty" firestore:"requiresGrounding,omitempty"`
}

func (a *SimulationAction) Validate() error {
	if a == nil {
		return fmt.Errorf("simulation action is nil")
	}
	a.Type = strings.TrimSpace(a.Type)
	a.ActorID = strings.TrimSpace(a.ActorID)
	a.TargetRefs = compactSimulationStrings(a.TargetRefs)
	if a.Type == "" {
		return fmt.Errorf("action type is required")
	}
	if !containsSimulationString(ValidSimulationActionTypes(), a.Type) {
		return fmt.Errorf("unsupported action type: %s", a.Type)
	}
	if a.ActorID == "" {
		return fmt.Errorf("actorId is required")
	}
	if descriptor, ok := SimulationActionCatalog()[a.Type]; ok && descriptor.RequiresTarget && len(a.TargetRefs) == 0 {
		return fmt.Errorf("action type %s requires targetRefs", a.Type)
	}
	return nil
}

type SimulationArtifact struct {
	ID                  string         `json:"id" firestore:"id"`
	TenantID            string         `json:"tenantId" firestore:"tenantId"`
	RunID               string         `json:"runId" firestore:"runId"`
	WorldID             string         `json:"worldId,omitempty" firestore:"worldId,omitempty"`
	Status              string         `json:"status" firestore:"status"`
	Kind                string         `json:"kind" firestore:"kind"`
	DisplayName         string         `json:"displayName,omitempty" firestore:"displayName,omitempty"`
	Payload             map[string]any `json:"payload,omitempty" firestore:"payload,omitempty"`
	AgentMeshTaskID     string         `json:"agentmeshTaskId,omitempty" firestore:"agentmeshTaskId,omitempty"`
	AgentMeshArtifactID string         `json:"agentmeshArtifactId,omitempty" firestore:"agentmeshArtifactId,omitempty"`
	SourceRefs          []SourceRef    `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BaselineRefs        []string       `json:"baselineRefs,omitempty" firestore:"baselineRefs,omitempty"`
	ExpiresAt           time.Time      `json:"expiresAt" firestore:"expiresAt"`
	CreatedAt           time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (a *SimulationArtifact) Validate() error {
	if a == nil {
		return fmt.Errorf("simulation artifact is nil")
	}
	if strings.TrimSpace(a.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(a.RunID) == "" {
		return fmt.Errorf("runId is required")
	}
	if strings.TrimSpace(a.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if strings.TrimSpace(a.Status) == "" {
		a.Status = "active"
	}
	if a.ExpiresAt.IsZero() {
		a.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}
	a.BaselineRefs = compactSimulationStrings(a.BaselineRefs)
	return nil
}

type SimulationEvent struct {
	ID               string         `json:"id" firestore:"id"`
	TenantID         string         `json:"tenantId" firestore:"tenantId"`
	RunID            string         `json:"runId" firestore:"runId"`
	WorldID          string         `json:"worldId,omitempty" firestore:"worldId,omitempty"`
	Status           string         `json:"status" firestore:"status"`
	EventType        string         `json:"eventType" firestore:"eventType"`
	StepIndex        int            `json:"stepIndex,omitempty" firestore:"stepIndex,omitempty"`
	OriginKind       string         `json:"originKind,omitempty" firestore:"originKind,omitempty"`
	OriginRefs       []string       `json:"originRefs,omitempty" firestore:"originRefs,omitempty"`
	Justification    string         `json:"justification,omitempty" firestore:"justification,omitempty"`
	GeneratedBy      string         `json:"generatedBy,omitempty" firestore:"generatedBy,omitempty"`
	SchemaVersion    string         `json:"schemaVersion,omitempty" firestore:"schemaVersion,omitempty"`
	Payload          map[string]any `json:"payload,omitempty" firestore:"payload,omitempty"`
	SourceRefs       []SourceRef    `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BaselineRefs     []string       `json:"baselineRefs,omitempty" firestore:"baselineRefs,omitempty"`
	AgentMeshTaskIDs []string       `json:"agentmeshTaskIds,omitempty" firestore:"agentmeshTaskIds,omitempty"`
	ExpiresAt        time.Time      `json:"expiresAt" firestore:"expiresAt"`
	CreatedAt        time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (e *SimulationEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("simulation event is nil")
	}
	if strings.TrimSpace(e.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Errorf("runId is required")
	}
	if strings.TrimSpace(e.EventType) == "" {
		return fmt.Errorf("eventType is required")
	}
	if strings.TrimSpace(e.Status) == "" {
		e.Status = "active"
	}
	e.OriginRefs = compactSimulationStrings(e.OriginRefs)
	e.BaselineRefs = compactSimulationStrings(e.BaselineRefs)
	e.AgentMeshTaskIDs = compactSimulationStrings(e.AgentMeshTaskIDs)
	if e.SchemaVersion == "" {
		e.SchemaVersion = "sim.event.v1"
	}
	if e.ExpiresAt.IsZero() {
		e.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}
	return nil
}

type SimulationBundle struct {
	ScenarioMetadata    map[string]any     `json:"scenarioMetadata,omitempty"`
	GraphValidation     map[string]any     `json:"graphValidation,omitempty"`
	ActionProfile       map[string]any     `json:"actionProfile,omitempty"`
	ProtocolSummary     []map[string]any   `json:"protocolSummary,omitempty"`
	TransactionSummary  []map[string]any   `json:"transactionSummary,omitempty"`
	DisciplinedSignals  []map[string]any   `json:"disciplinedSignals,omitempty"`
	FunctionExposures   []map[string]any   `json:"functionExposures,omitempty"`
	DecisionDemands     []map[string]any   `json:"decisionDemands,omitempty"`
	MonitoringSignals   []map[string]any   `json:"monitoringSignals,omitempty"`
	RoleReadouts        map[string]any     `json:"roleReadouts,omitempty"`
	ResultOverlays      map[string]any     `json:"resultOverlays,omitempty"`
	ExecutiveMemo       map[string]any     `json:"executiveMemo,omitempty"`
	RunSummary          map[string]any     `json:"runSummary,omitempty"`
	WorldSummaries      []map[string]any   `json:"worldSummaries,omitempty"`
	OutcomeDistribution map[string]float64 `json:"outcomeDistribution,omitempty"`
	Counterfactuals     []map[string]any   `json:"counterfactuals,omitempty"`
	Sensitivity         map[string]float64 `json:"sensitivity,omitempty"`
	KeyMechanisms       []string           `json:"keyMechanisms,omitempty"`
	ActorReactions      []map[string]any   `json:"actorReactions,omitempty"`
	MechanismChains     []map[string]any   `json:"mechanismChains,omitempty"`
	ExecutiveNarrative  []string           `json:"executiveNarrative,omitempty"`
	TechnicalNarrative  []string           `json:"technicalNarrative,omitempty"`
	TopTrajectories     []map[string]any   `json:"topTrajectories,omitempty"`
	TraceRefs           []string           `json:"traceRefs,omitempty"`
	ArtifactRefs        []string           `json:"artifactRefs,omitempty"`
	GroundingCoverage   float64            `json:"groundingCoverage,omitempty"`
}

func (b *SimulationBundle) Digest() string {
	if b == nil {
		return ""
	}
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func SimulationSnapshotDigest(parts ...any) string {
	rawParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		switch v := part.(type) {
		case string:
			v = strings.TrimSpace(v)
			if v != "" {
				rawParts = append(rawParts, v)
			}
		default:
			raw, err := json.Marshal(v)
			if err == nil && len(raw) > 0 {
				rawParts = append(rawParts, string(raw))
			}
		}
	}
	sort.Strings(rawParts)
	sum := sha256.Sum256([]byte(strings.Join(rawParts, "\n")))
	return hex.EncodeToString(sum[:])
}

func compactSimulationStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsSimulationString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}
