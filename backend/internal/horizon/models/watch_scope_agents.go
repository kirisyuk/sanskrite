package models

import (
	"fmt"
	"strings"
	"time"
)

type ScopeAgentRole string

const (
	ScopeAgentRoleMonitor  ScopeAgentRole = "monitor"
	ScopeAgentRoleScout    ScopeAgentRole = "scout"
	ScopeAgentRoleRisk     ScopeAgentRole = "risk"
	ScopeAgentRoleForecast ScopeAgentRole = "forecast"
)

type ScopeAgentStatus string

const (
	ScopeAgentStatusRunning ScopeAgentStatus = "RUNNING"
	ScopeAgentStatusPaused  ScopeAgentStatus = "PAUSED"
)

type ScopeAgentRunStatus string

const (
	ScopeAgentRunStatusPending   ScopeAgentRunStatus = "PENDING"
	ScopeAgentRunStatusRunning   ScopeAgentRunStatus = "RUNNING"
	ScopeAgentRunStatusCompleted ScopeAgentRunStatus = "COMPLETED"
	ScopeAgentRunStatusFailed    ScopeAgentRunStatus = "FAILED"
	ScopeAgentRunStatusCancelled ScopeAgentRunStatus = "CANCELLED"
)

type AgentProposalType string

const (
	AgentProposalTypeSignalCandidate   AgentProposalType = "signal_candidate"
	AgentProposalTypeSignalBoost       AgentProposalType = "signal_boost"
	AgentProposalTypeEdgeCandidate     AgentProposalType = "edge_candidate"
	AgentProposalTypeEvidenceBundle    AgentProposalType = "evidence_bundle"
	AgentProposalTypeCoverageGap       AgentProposalType = "coverage_gap"
	AgentProposalTypeScenarioProposal  AgentProposalType = "scenario_proposal"
	AgentProposalTypeForecastCandidate AgentProposalType = "forecast_candidate"
)

type AgentProposalStatus string

const (
	AgentProposalStatusPending  AgentProposalStatus = "PENDING"
	AgentProposalStatusAccepted AgentProposalStatus = "ACCEPTED"
	AgentProposalStatusRejected AgentProposalStatus = "REJECTED"
	AgentProposalStatusMerged   AgentProposalStatus = "MERGED"
)

type ArbitrationDecisionType string

const (
	ArbitrationDecisionAccept ArbitrationDecisionType = "accepted"
	ArbitrationDecisionReject ArbitrationDecisionType = "rejected"
	ArbitrationDecisionMerge  ArbitrationDecisionType = "merged"
)

type WatchTargetStatus string

const (
	WatchTargetStatusActive  WatchTargetStatus = "ACTIVE"
	WatchTargetStatusPaused  WatchTargetStatus = "PAUSED"
	WatchTargetStatusDeleted WatchTargetStatus = "DELETED"
)

type WatchMode string

const (
	WatchModeGlobal   WatchMode = "global"
	WatchModeMy       WatchMode = "my_watch"
	WatchModeCombined WatchMode = "combined"
)

type LLMPolicy struct {
	Provider              string   `json:"provider,omitempty" firestore:"provider,omitempty"`
	PrimaryModel          string   `json:"primaryModel,omitempty" firestore:"primaryModel,omitempty"`
	EscalationModel       string   `json:"escalationModel,omitempty" firestore:"escalationModel,omitempty"`
	FallbackModels        []string `json:"fallbackModels,omitempty" firestore:"fallbackModels,omitempty"`
	PreferredVertexModel  string   `json:"preferredVertexModel,omitempty" firestore:"preferredVertexModel,omitempty"`
	UseSearchGrounding    bool     `json:"useSearchGrounding" firestore:"useSearchGrounding"`
	UseDatastoreGrounding bool     `json:"useDatastoreGrounding" firestore:"useDatastoreGrounding"`
	BatchWindowSeconds    int      `json:"batchWindowSeconds,omitempty" firestore:"batchWindowSeconds,omitempty"`
	MaxBatchSize          int      `json:"maxBatchSize,omitempty" firestore:"maxBatchSize,omitempty"`
}

type WatchPack struct {
	ID          string    `json:"id" firestore:"id"`
	TenantID    string    `json:"tenantId" firestore:"tenantId"`
	Name        string    `json:"name" firestore:"name"`
	Description string    `json:"description,omitempty" firestore:"description,omitempty"`
	Status      string    `json:"status" firestore:"status"`
	CreatedAt   time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt" firestore:"updatedAt"`
}

func (p *WatchPack) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = string(WatchTargetStatusActive)
	}
	return nil
}

type ScopeAgentConfig struct {
	ID                 string         `json:"id" firestore:"id"`
	TenantID           string         `json:"tenantId" firestore:"tenantId"`
	PackID             string         `json:"packId,omitempty" firestore:"packId,omitempty"`
	WatchTargetID      string         `json:"watchTargetId,omitempty" firestore:"watchTargetId,omitempty"`
	TargetFragmentID   string         `json:"targetFragmentId,omitempty" firestore:"targetFragmentId,omitempty"`
	Name               string         `json:"name" firestore:"name"`
	ScopeCanonicalPath string         `json:"scopeCanonicalPath" firestore:"scopeCanonicalPath"`
	ScopeType          string         `json:"scopeType" firestore:"scopeType"`
	DomainModes        []string       `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
	Roles              []string       `json:"roles,omitempty" firestore:"roles,omitempty"`
	Priority           int            `json:"priority" firestore:"priority"`
	Status             string         `json:"status" firestore:"status"`
	ExpansionPolicy    map[string]any `json:"expansionPolicy,omitempty" firestore:"expansionPolicy,omitempty"`
	Schedule           map[string]any `json:"schedule,omitempty" firestore:"schedule,omitempty"`
	LLMPolicy          LLMPolicy      `json:"llmPolicy" firestore:"llmPolicy"`
	Metadata           map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (c *ScopeAgentConfig) Validate() error {
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(c.ScopeCanonicalPath) == "" {
		return fmt.Errorf("scopeCanonicalPath is required")
	}
	if strings.TrimSpace(c.ScopeType) == "" {
		return fmt.Errorf("scopeType is required")
	}
	if strings.TrimSpace(c.Status) == "" {
		c.Status = string(ScopeAgentStatusRunning)
	}
	if c.Priority == 0 {
		c.Priority = 50
	}
	return nil
}

type ScopeAgentState struct {
	ID                   string         `json:"id" firestore:"id"`
	TenantID             string         `json:"tenantId" firestore:"tenantId"`
	ConfigID             string         `json:"configId" firestore:"configId"`
	WatchTargetID        string         `json:"watchTargetId,omitempty" firestore:"watchTargetId,omitempty"`
	Baseline             map[string]any `json:"baseline,omitempty" firestore:"baseline,omitempty"`
	LastAcceptedForecast map[string]any `json:"lastAcceptedForecast,omitempty" firestore:"lastAcceptedForecast,omitempty"`
	OpenWatchpoints      []string       `json:"openWatchpoints,omitempty" firestore:"openWatchpoints,omitempty"`
	CoverageGaps         []string       `json:"coverageGaps,omitempty" firestore:"coverageGaps,omitempty"`
	PendingWakeReasons   []string       `json:"pendingWakeReasons,omitempty" firestore:"pendingWakeReasons,omitempty"`
	DebounceUntil        *time.Time     `json:"debounceUntil,omitempty" firestore:"debounceUntil,omitempty"`
	LastGroundedRunAt    *time.Time     `json:"lastGroundedRunAt,omitempty" firestore:"lastGroundedRunAt,omitempty"`
	LastRunID            string         `json:"lastRunId,omitempty" firestore:"lastRunId,omitempty"`
	UpdatedAt            time.Time      `json:"updatedAt" firestore:"updatedAt"`
	CreatedAt            time.Time      `json:"createdAt" firestore:"createdAt"`
}

func (s *ScopeAgentState) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(s.ConfigID) == "" {
		return fmt.Errorf("configId is required")
	}
	return nil
}

type ScopeAgentRun struct {
	ID                  string         `json:"id" firestore:"id"`
	TenantID            string         `json:"tenantId" firestore:"tenantId"`
	ConfigID            string         `json:"configId" firestore:"configId"`
	WatchTargetID       string         `json:"watchTargetId,omitempty" firestore:"watchTargetId,omitempty"`
	Status              string         `json:"status" firestore:"status"`
	TriggerType         string         `json:"triggerType" firestore:"triggerType"`
	TriggerReason       string         `json:"triggerReason,omitempty" firestore:"triggerReason,omitempty"`
	BatchKey            string         `json:"batchKey,omitempty" firestore:"batchKey,omitempty"`
	AgentJobID          string         `json:"agentJobId,omitempty" firestore:"agentJobId,omitempty"`
	PrimaryModel        string         `json:"primaryModel,omitempty" firestore:"primaryModel,omitempty"`
	EscalationModel     string         `json:"escalationModel,omitempty" firestore:"escalationModel,omitempty"`
	Queries             []string       `json:"queries,omitempty" firestore:"queries,omitempty"`
	ProducedProposalIDs []string       `json:"producedProposalIds,omitempty" firestore:"producedProposalIds,omitempty"`
	Trace               map[string]any `json:"trace,omitempty" firestore:"trace,omitempty"`
	CreatedAt           time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt" firestore:"updatedAt"`
	StartedAt           *time.Time     `json:"startedAt,omitempty" firestore:"startedAt,omitempty"`
	CompletedAt         *time.Time     `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
}

func (r *ScopeAgentRun) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(r.ConfigID) == "" {
		return fmt.Errorf("configId is required")
	}
	if strings.TrimSpace(r.Status) == "" {
		r.Status = string(ScopeAgentRunStatusPending)
	}
	if strings.TrimSpace(r.TriggerType) == "" {
		r.TriggerType = "watchdog"
	}
	return nil
}

type AgentProposal struct {
	ID                   string         `json:"id" firestore:"id"`
	TenantID             string         `json:"tenantId" firestore:"tenantId"`
	ConfigID             string         `json:"configId" firestore:"configId"`
	RunID                string         `json:"runId,omitempty" firestore:"runId,omitempty"`
	WatchTargetID        string         `json:"watchTargetId,omitempty" firestore:"watchTargetId,omitempty"`
	ProposalType         string         `json:"proposalType" firestore:"proposalType"`
	TargetCanonicalPaths []string       `json:"targetCanonicalPaths" firestore:"targetCanonicalPaths"`
	SignalKey            string         `json:"signalKey,omitempty" firestore:"signalKey,omitempty"`
	DedupKey             string         `json:"dedupKey,omitempty" firestore:"dedupKey,omitempty"`
	Payload              map[string]any `json:"payload,omitempty" firestore:"payload,omitempty"`
	SourceRefs           []SourceRef    `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	Confidence           float64        `json:"confidence" firestore:"confidence"`
	Explanation          string         `json:"explanation,omitempty" firestore:"explanation,omitempty"`
	Status               string         `json:"status" firestore:"status"`
	TimeWindowStart      *time.Time     `json:"timeWindowStart,omitempty" firestore:"timeWindowStart,omitempty"`
	TimeWindowEnd        *time.Time     `json:"timeWindowEnd,omitempty" firestore:"timeWindowEnd,omitempty"`
	CreatedAt            time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (p *AgentProposal) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.ConfigID) == "" {
		return fmt.Errorf("configId is required")
	}
	if strings.TrimSpace(p.ProposalType) == "" {
		return fmt.Errorf("proposalType is required")
	}
	if len(p.TargetCanonicalPaths) == 0 {
		return fmt.Errorf("targetCanonicalPaths are required")
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = string(AgentProposalStatusPending)
	}
	return nil
}

type ArbitrationDecision struct {
	ID               string    `json:"id" firestore:"id"`
	TenantID         string    `json:"tenantId" firestore:"tenantId"`
	ProposalID       string    `json:"proposalId" firestore:"proposalId"`
	Decision         string    `json:"decision" firestore:"decision"`
	MergedInto       string    `json:"mergedInto,omitempty" firestore:"mergedInto,omitempty"`
	ReasonCodes      []string  `json:"reasonCodes,omitempty" firestore:"reasonCodes,omitempty"`
	AppliedArtifacts []string  `json:"appliedArtifacts,omitempty" firestore:"appliedArtifacts,omitempty"`
	CreatedAt        time.Time `json:"createdAt" firestore:"createdAt"`
}

func (d *ArbitrationDecision) Validate() error {
	if strings.TrimSpace(d.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(d.ProposalID) == "" {
		return fmt.Errorf("proposalId is required")
	}
	if strings.TrimSpace(d.Decision) == "" {
		return fmt.Errorf("decision is required")
	}
	return nil
}

type WatchTarget struct {
	ID            string         `json:"id" firestore:"id"`
	TenantID      string         `json:"tenantId" firestore:"tenantId"`
	UserID        string         `json:"userId,omitempty" firestore:"userId,omitempty"`
	PackID        string         `json:"packId,omitempty" firestore:"packId,omitempty"`
	Kind          string         `json:"kind" firestore:"kind"`
	Label         string         `json:"label" firestore:"label"`
	CanonicalPath string         `json:"canonicalPath" firestore:"canonicalPath"`
	FragmentID    string         `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	DomainModes   []string       `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
	Roles         []string       `json:"roles,omitempty" firestore:"roles,omitempty"`
	Priority      int            `json:"priority" firestore:"priority"`
	Status        string         `json:"status" firestore:"status"`
	Source        string         `json:"source,omitempty" firestore:"source,omitempty"`
	AgentConfigID string         `json:"agentConfigId,omitempty" firestore:"agentConfigId,omitempty"`
	LastBriefID   string         `json:"lastBriefId,omitempty" firestore:"lastBriefId,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (t *WatchTarget) Validate() error {
	if strings.TrimSpace(t.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(t.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if strings.TrimSpace(t.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if strings.TrimSpace(t.CanonicalPath) == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	if strings.TrimSpace(t.Status) == "" {
		t.Status = string(WatchTargetStatusActive)
	}
	if t.Priority == 0 {
		t.Priority = 50
	}
	return nil
}

type WatchBrief struct {
	ID                 string         `json:"id" firestore:"id"`
	TenantID           string         `json:"tenantId" firestore:"tenantId"`
	TargetID           string         `json:"targetId" firestore:"targetId"`
	CanonicalPath      string         `json:"canonicalPath" firestore:"canonicalPath"`
	Label              string         `json:"label" firestore:"label"`
	ScopeType          string         `json:"scopeType" firestore:"scopeType"`
	Headline           string         `json:"headline" firestore:"headline"`
	Summary            string         `json:"summary" firestore:"summary"`
	Hotness            float64        `json:"hotness" firestore:"hotness"`
	Confidence         float64        `json:"confidence" firestore:"confidence"`
	RiskPosture        string         `json:"riskPosture,omitempty" firestore:"riskPosture,omitempty"`
	DominantThemes     []string       `json:"dominantThemes,omitempty" firestore:"dominantThemes,omitempty"`
	TopAffectedTargets []string       `json:"topAffectedTargets,omitempty" firestore:"topAffectedTargets,omitempty"`
	TopSignals         []string       `json:"topSignals,omitempty" firestore:"topSignals,omitempty"`
	TopEvidence        []string       `json:"topEvidence,omitempty" firestore:"topEvidence,omitempty"`
	Explanation        string         `json:"explanation,omitempty" firestore:"explanation,omitempty"`
	Forecast           map[string]any `json:"forecast,omitempty" firestore:"forecast,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (b *WatchBrief) Validate() error {
	if strings.TrimSpace(b.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(b.TargetID) == "" {
		return fmt.Errorf("targetId is required")
	}
	if strings.TrimSpace(b.CanonicalPath) == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	if strings.TrimSpace(b.Label) == "" {
		return fmt.Errorf("label is required")
	}
	return nil
}

type WatchFeedEntry struct {
	ID             string         `json:"id" firestore:"id"`
	TenantID       string         `json:"tenantId" firestore:"tenantId"`
	TargetID       string         `json:"targetId" firestore:"targetId"`
	BriefID        string         `json:"briefId,omitempty" firestore:"briefId,omitempty"`
	EntryType      string         `json:"entryType" firestore:"entryType"`
	Title          string         `json:"title" firestore:"title"`
	Summary        string         `json:"summary,omitempty" firestore:"summary,omitempty"`
	CanonicalPaths []string       `json:"canonicalPaths,omitempty" firestore:"canonicalPaths,omitempty"`
	ProposalID     string         `json:"proposalId,omitempty" firestore:"proposalId,omitempty"`
	DecisionID     string         `json:"decisionId,omitempty" firestore:"decisionId,omitempty"`
	RiskJobID      string         `json:"riskJobId,omitempty" firestore:"riskJobId,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"createdAt" firestore:"createdAt"`
}

func (e *WatchFeedEntry) Validate() error {
	if strings.TrimSpace(e.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(e.TargetID) == "" {
		return fmt.Errorf("targetId is required")
	}
	if strings.TrimSpace(e.EntryType) == "" {
		return fmt.Errorf("entryType is required")
	}
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

type WatchPreferences struct {
	ID                  string         `json:"id" firestore:"id"`
	TenantID            string         `json:"tenantId" firestore:"tenantId"`
	UserID              string         `json:"userId" firestore:"userId"`
	Mode                string         `json:"mode" firestore:"mode"`
	TimeHorizon         string         `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	ConfidenceFloor     float64        `json:"confidenceFloor" firestore:"confidenceFloor"`
	OnlyWithImpactPaths bool           `json:"onlyWithImpactPaths" firestore:"onlyWithImpactPaths"`
	OnlyTrustedSources  bool           `json:"onlyTrustedSources" firestore:"onlyTrustedSources"`
	Filters             map[string]any `json:"filters,omitempty" firestore:"filters,omitempty"`
	CreatedAt           time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (p *WatchPreferences) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("userId is required")
	}
	if strings.TrimSpace(p.Mode) == "" {
		p.Mode = string(WatchModeCombined)
	}
	if p.ConfidenceFloor < 0 || p.ConfidenceFloor > 1 {
		return fmt.Errorf("confidenceFloor must be between 0 and 1")
	}
	return nil
}
