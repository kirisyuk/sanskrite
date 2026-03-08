package models

import "time"

// QuestionPlan is the structured output of QuestionDecomposer.
type QuestionPlan struct {
	NormalizedQuestion  string               `json:"normalizedQuestion" firestore:"normalizedQuestion"`
	AnswerMode          string               `json:"answerMode" firestore:"answerMode"` // impact_assessment | blocker_analysis | alternative_sourcing | risk_forecast | dependency_audit | out_of_scope
	TimeHorizon         string               `json:"timeHorizon" firestore:"timeHorizon"`
	HypotheticalSignals []HypotheticalSignal `json:"hypotheticalSignals,omitempty" firestore:"hypotheticalSignals,omitempty"`
	TargetFilter        TargetFilter         `json:"targetFilter,omitempty" firestore:"targetFilter,omitempty"`
	ContextHints        []string             `json:"contextHints,omitempty" firestore:"contextHints,omitempty"`
}

// HypotheticalSignal describes a signal to inject for what-if simulation.
type HypotheticalSignal struct {
	Axis        string   `json:"axis" firestore:"axis"`
	Mechanism   string   `json:"mechanism,omitempty" firestore:"mechanism,omitempty"`
	Amplitude   float64  `json:"amplitude" firestore:"amplitude"`
	Polarity    string   `json:"polarity" firestore:"polarity"`
	SeedHints   []string `json:"seedHints,omitempty" firestore:"seedHints,omitempty"`
	TimeHorizon string   `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	Modality    string   `json:"modality,omitempty" firestore:"modality,omitempty"`
}

// TargetFilter specifies what nodes to observe in the answer.
type TargetFilter struct {
	ObservePathPatterns []string `json:"observePathPatterns,omitempty" firestore:"observePathPatterns,omitempty"`
	ObserveAxes         []string `json:"observeAxes,omitempty" firestore:"observeAxes,omitempty"`
	MaxNodes            int      `json:"maxNodes,omitempty" firestore:"maxNodes,omitempty"`
}

// SimulationResult holds the output of shadow propagation or current state collection.
type SimulationResult struct {
	NodeImpacts        []NodeImpact         `json:"nodeImpacts" firestore:"nodeImpacts"`
	TracePaths         []TracePath          `json:"tracePaths" firestore:"tracePaths"`
	LeafEvidence       []LeafSignalEvidence `json:"leafEvidence" firestore:"leafEvidence"`
	ChainImpacts       []ChainImpact        `json:"chainImpacts" firestore:"chainImpacts"`
	HighlightedEdgeIDs []string             `json:"highlightedEdgeIds,omitempty" firestore:"highlightedEdgeIds,omitempty"`
	SeedFailures       []SeedFailure        `json:"seedFailures,omitempty" firestore:"seedFailures,omitempty"`
}

// SeedFailure records a seed hint that could not be resolved.
type SeedFailure struct {
	Hint   string `json:"hint" firestore:"hint"`
	Reason string `json:"reason" firestore:"reason"`
}

// NodeImpact describes per-node impact from propagation.
type NodeImpact struct {
	FragmentID     string             `json:"fragmentId" firestore:"fragmentId"`
	CanonicalPath  string             `json:"canonicalPath" firestore:"canonicalPath"`
	Name           string             `json:"name" firestore:"name"`
	Type           string             `json:"type" firestore:"type"`
	AxisAmplitudes map[string]float64 `json:"axisAmplitudes" firestore:"axisAmplitudes"`
	TotalAmplitude float64            `json:"totalAmplitude" firestore:"totalAmplitude"`
	Hops           int                `json:"hops" firestore:"hops"`
	PathFromSeed   []string           `json:"pathFromSeed,omitempty" firestore:"pathFromSeed,omitempty"`
}

// TracePath is a propagation trace for visualization.
type TracePath struct {
	NodeCanonicalPaths []string  `json:"nodeCanonicalPaths" firestore:"nodeCanonicalPaths"`
	Axis               string    `json:"axis" firestore:"axis"`
	Amplitudes         []float64 `json:"amplitudes" firestore:"amplitudes"`
	EdgeTypes          []string  `json:"edgeTypes,omitempty" firestore:"edgeTypes,omitempty"`
}

// LeafSignalEvidence is physical signal evidence at terminal nodes.
type LeafSignalEvidence struct {
	FragmentID    string    `json:"fragmentId" firestore:"fragmentId"`
	CanonicalPath string    `json:"canonicalPath" firestore:"canonicalPath"`
	Name          string    `json:"name" firestore:"name"`
	SignalType    string    `json:"signalType" firestore:"signalType"`
	Axis          string    `json:"axis" firestore:"axis"`
	Amplitude     float64   `json:"amplitude" firestore:"amplitude"`
	Modality      string    `json:"modality" firestore:"modality"`
	ObservedAt    time.Time `json:"observedAt" firestore:"observedAt"`
	Summary       string    `json:"summary" firestore:"summary"`
}

// ChainImpact describes impact on a materialized chain.
type ChainImpact struct {
	ArtifactID    string  `json:"artifactId" firestore:"artifactId"`
	DisplayName   string  `json:"displayName" firestore:"displayName"`
	Kind          string  `json:"kind" firestore:"kind"`
	MaxAmplitude  float64 `json:"maxAmplitude" firestore:"maxAmplitude"`
	AffectedNodes int     `json:"affectedNodes" firestore:"affectedNodes"`
	TotalNodes    int     `json:"totalNodes" firestore:"totalNodes"`
}

// SimulationResultSummary is a Firestore-safe summary (not full trace).
type SimulationResultSummary struct {
	AffectedNodeCount int           `json:"affectedNodeCount" firestore:"affectedNodeCount"`
	TopNodeImpacts    []NodeImpact  `json:"topNodeImpacts" firestore:"topNodeImpacts"`
	LeafEvidenceCount int           `json:"leafEvidenceCount" firestore:"leafEvidenceCount"`
	ChainImpacts      []ChainImpact `json:"chainImpacts" firestore:"chainImpacts"`
	SimulatedAxes     []string      `json:"simulatedAxes" firestore:"simulatedAxes"`
}

// QAAnswer is the synthesized answer with evidence and highlights.
type QAAnswer struct {
	// --- Business Layer (Client Facing) ---
	ExecutiveSummary   string         `json:"executiveSummary" firestore:"executiveSummary"` // One-line "TL;DR"
	RiskLevel          string         `json:"riskLevel" firestore:"riskLevel"`               // CRITICAL, HIGH, MODERATE, LOW, NEGLIGIBLE
	RiskScenario       string         `json:"riskScenario" firestore:"riskScenario"`         // Narrative: Cause -> Effect chain
	Recommendations    []string       `json:"recommendations" firestore:"recommendations"`   // Actionable next steps
	FirstOrderEffects  []QAEffect     `json:"firstOrderEffects,omitempty" firestore:"firstOrderEffects,omitempty"`
	SecondOrderEffects []QAEffect     `json:"secondOrderEffects,omitempty" firestore:"secondOrderEffects,omitempty"`
	Watchpoints        []QAWatchpoint `json:"watchpoints,omitempty" firestore:"watchpoints,omitempty"`
	Assumptions        []string       `json:"assumptions,omitempty" firestore:"assumptions,omitempty"`

	// --- Legacy/Technical Layer (Hidden/Debug) ---
	Text                   string           `json:"text" firestore:"text"`
	Confidence             float64          `json:"confidence" firestore:"confidence"`
	BusinessRelevanceScore float64          `json:"businessRelevanceScore,omitempty" firestore:"businessRelevanceScore,omitempty"` // 0-1: how well answer addresses the question
	Evidence               []AnswerEvidence `json:"evidence" firestore:"evidence"`
	HighlightedNodeIDs     []string         `json:"highlightedNodeIds" firestore:"highlightedNodeIds"`
	HighlightedEdgeIDs     []string         `json:"highlightedEdgeIds" firestore:"highlightedEdgeIds"`
	TracePaths             []TracePath      `json:"tracePaths" firestore:"tracePaths"`
	ImpactBreakdown        []ImpactItem     `json:"impactBreakdown,omitempty" firestore:"impactBreakdown,omitempty"`
	Alternatives           []QAAlternative  `json:"alternatives,omitempty" firestore:"alternatives,omitempty"`
	DataGaps               []QADataGap      `json:"dataGaps,omitempty" firestore:"dataGaps,omitempty"`
	AnswerMode             string           `json:"answerMode" firestore:"answerMode"`
	TimeHorizon            string           `json:"timeHorizon" firestore:"timeHorizon"`
	SimulatedAxes          []string         `json:"simulatedAxes" firestore:"simulatedAxes"`
}

// AnswerEvidence backs a claim in the answer.
type AnswerEvidence struct {
	Type          string  `json:"type" firestore:"type"`
	CanonicalPath string  `json:"canonicalPath" firestore:"canonicalPath"`
	Summary       string  `json:"summary" firestore:"summary"`
	Confidence    float64 `json:"confidence" firestore:"confidence"`
	SourceRef     string  `json:"sourceRef,omitempty" firestore:"sourceRef,omitempty"`
}

// ImpactItem describes impact on a single entity.
type ImpactItem struct {
	Entity     string  `json:"entity" firestore:"entity"`
	EntityName string  `json:"entityName" firestore:"entityName"`
	Axis       string  `json:"axis" firestore:"axis"`
	Amplitude  float64 `json:"amplitude" firestore:"amplitude"`
	Mechanism  string  `json:"mechanism" firestore:"mechanism"`
	Hops       int     `json:"hops" firestore:"hops"`
}

// QAAlternative is an actionable alternative suggestion for QA answers.
type QAAlternative struct {
	Entity      string `json:"entity" firestore:"entity"`
	EntityName  string `json:"entityName" firestore:"entityName"`
	Description string `json:"description" firestore:"description"`
	TradeOff    string `json:"tradeOff" firestore:"tradeOff"`
}

// QAEffect is a business-facing effect within a what-if scenario.
type QAEffect struct {
	Summary          string   `json:"summary" firestore:"summary"`
	Horizon          string   `json:"horizon,omitempty" firestore:"horizon,omitempty"`
	Severity         string   `json:"severity,omitempty" firestore:"severity,omitempty"`
	AffectedEntities []string `json:"affectedEntities,omitempty" firestore:"affectedEntities,omitempty"`
	Mechanism        string   `json:"mechanism,omitempty" firestore:"mechanism,omitempty"`
}

// QAWatchpoint is an operational or market signal that should be monitored after the shock.
type QAWatchpoint struct {
	Signal         string `json:"signal" firestore:"signal"`
	WhyItMatters   string `json:"whyItMatters,omitempty" firestore:"whyItMatters,omitempty"`
	ExpectedWithin string `json:"expectedWithin,omitempty" firestore:"expectedWithin,omitempty"`
}

// QADataGap describes missing data that would strengthen the QA answer.
type QADataGap struct {
	Description  string `json:"description" firestore:"description"`
	WhyItMatters string `json:"whyItMatters" firestore:"whyItMatters"`
}

// QASession tracks a question-answer exchange within a lens.
// Stored in Firestore: horizon_lenses/{lensId}/qa_sessions/{id}
type QASession struct {
	ID        string `json:"id" firestore:"id"`
	LensID    string `json:"lensId" firestore:"lensId"`
	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId" firestore:"projectId"`

	RawQuestion        string `json:"rawQuestion" firestore:"rawQuestion"`
	NormalizedQuestion string `json:"normalizedQuestion" firestore:"normalizedQuestion"`
	IntentID           string `json:"intentId,omitempty" firestore:"intentId,omitempty"`

	Plan             *QuestionPlan            `json:"plan,omitempty" firestore:"plan,omitempty"`
	SimulationResult *SimulationResultSummary `json:"simulationResult,omitempty" firestore:"simulationResult,omitempty"`
	Answer           *QAAnswer                `json:"answer,omitempty" firestore:"answer,omitempty"`
	AnsweredAt       *time.Time               `json:"answeredAt,omitempty" firestore:"answeredAt,omitempty"`

	Status    string    `json:"status" firestore:"status"` // pending, decomposing, simulating, synthesizing, answered, failed
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	DecomposeLatencyMs  int `json:"decomposeLatencyMs,omitempty" firestore:"decomposeLatencyMs,omitempty"`
	SimulateLatencyMs   int `json:"simulateLatencyMs,omitempty" firestore:"simulateLatencyMs,omitempty"`
	SynthesizeLatencyMs int `json:"synthesizeLatencyMs,omitempty" firestore:"synthesizeLatencyMs,omitempty"`
	TotalLatencyMs      int `json:"totalLatencyMs,omitempty" firestore:"totalLatencyMs,omitempty"`
}

const (
	QASessionStatusPending      = "pending"
	QASessionStatusDecomposing  = "decomposing"
	QASessionStatusSimulating   = "simulating"
	QASessionStatusSynthesizing = "synthesizing"
	QASessionStatusAnswered     = "answered"
	QASessionStatusFailed       = "failed"
)
