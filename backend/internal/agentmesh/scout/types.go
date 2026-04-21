package scout

type InvestigationRequest struct {
	TenantID            string            `json:"tenantId,omitempty"`
	ProjectID           string            `json:"projectId,omitempty"`
	LensID              string            `json:"lensId,omitempty"`
	ScoutingJobID       string            `json:"scoutingJobId,omitempty"`
	Query               string            `json:"query,omitempty"`
	TargetCanonicalPath string            `json:"targetCanonicalPath,omitempty"`
	CompareOnly         bool              `json:"compareOnly,omitempty"`
	AllowWrites         bool              `json:"allowWrites,omitempty"`
	Metadata            map[string]any    `json:"metadata,omitempty"`
	Goal                GoalFrame         `json:"goal,omitempty"`
	Environment         EnvironmentFrame  `json:"environment,omitempty"`
	Role                RoleFrame         `json:"role,omitempty"`
	Completeness        CompletenessFrame `json:"completeness,omitempty"`
}

// GoalFrame is the explicit mission contract for the investigation.
// It prevents the runtime from treating the task as generic text generation.
type GoalFrame struct {
	Objective           string   `json:"objective,omitempty"`
	PrimaryQuestion     string   `json:"primaryQuestion,omitempty"`
	UserIntent          string   `json:"userIntent,omitempty"`
	TargetCanonicalPath string   `json:"targetCanonicalPath,omitempty"`
	WriteDisposition    string   `json:"writeDisposition,omitempty"`
	OutputContract      []string `json:"outputContract,omitempty"`
	SuccessCriteria     []string `json:"successCriteria,omitempty"`
	StopConditions      []string `json:"stopConditions,omitempty"`
}

// EnvironmentFrame describes the actual workspace the agent operates in:
// a graph of entities, typed relations, and materialized artifacts.
type EnvironmentFrame struct {
	WorkspaceKind     string           `json:"workspaceKind,omitempty"`
	UnitOfWork        string           `json:"unitOfWork,omitempty"`
	PrimaryObjects    []string         `json:"primaryObjects,omitempty"`
	EntitySemantics   []string         `json:"entitySemantics,omitempty"`
	EdgeSemantics     []string         `json:"edgeSemantics,omitempty"`
	ArtifactSemantics []string         `json:"artifactSemantics,omitempty"`
	AvailableActions  []string         `json:"availableActions,omitempty"`
	KnownContextRefs  []string         `json:"knownContextRefs,omitempty"`
	MetadataFields    []string         `json:"metadataFields,omitempty"`
	TargetEntity      map[string]any   `json:"targetEntity,omitempty"`
	ObservedRelations []map[string]any `json:"observedRelations,omitempty"`
}

// RoleFrame defines the professional lens the investigator should operate under.
// It is not cosmetic prompt flavor: it shapes what counts as strong evidence,
// which actions are preferred, and what quality bar should be enforced.
type RoleFrame struct {
	Domain                 string   `json:"domain,omitempty"`
	PrimaryRole            string   `json:"primaryRole,omitempty"`
	SupportingRoles        []string `json:"supportingRoles,omitempty"`
	ProfessionalQualityBar string   `json:"professionalQualityBar,omitempty"`
	InvestigationPosture   string   `json:"investigationPosture,omitempty"`
	RoleRationale          string   `json:"roleRationale,omitempty"`
}

// CompletenessFrame tracks whether the investigation has performed a broad scan
// and whether major alternative routes have been sufficiently explored.
type CompletenessFrame struct {
	BroadScanRequired      bool     `json:"broadScanRequired,omitempty"`
	BroadScanQuestion      string   `json:"broadScanQuestion,omitempty"`
	BroadScanCompleted     bool     `json:"broadScanCompleted,omitempty"`
	ExplorationStatus      string   `json:"explorationStatus,omitempty"`
	KnownAlternativeRoutes []string `json:"knownAlternativeRoutes,omitempty"`
	MissingAngles          []string `json:"missingAngles,omitempty"`
	NoMaterialAlternatives bool     `json:"noMaterialAlternatives,omitempty"`
	CompletionRationale    string   `json:"completionRationale,omitempty"`
}

type InvestigationPlan struct {
	Goal                    string                `json:"goal,omitempty"`
	MissionRole             RoleFrame             `json:"missionRole,omitempty"`
	FocusProducts           []string              `json:"focusProducts,omitempty"`
	FocusGeos               []string              `json:"focusGeos,omitempty"`
	FocusActors             []string              `json:"focusActors,omitempty"`
	WantsPathMaterialize    bool                  `json:"wantsPathMaterialize,omitempty"`
	MaxResearchRounds       int                   `json:"maxResearchRounds,omitempty"`
	PlanningRationale       []string              `json:"planningRationale,omitempty"`
	ExistingGraphPriorities []string              `json:"existingGraphPriorities,omitempty"`
	Branches                []InvestigationBranch `json:"branches,omitempty"`
	// PlanVersion increments when the orchestrator reframes / merges a new plan (living artifact).
	PlanVersion int `json:"planVersion,omitempty"`
	// MaterializationStrategy guides progressive landing (e.g. path_first, evidence_first, no_writes).
	MaterializationStrategy string `json:"materializationStrategy,omitempty"`
	// BranchDependencies: ToBranchID depends on FromBranchID completing first.
	BranchDependencies []BranchDependency `json:"branchDependencies,omitempty"`
}

// BranchDependency expresses investigation ordering constraints (plan graph edge).
type BranchDependency struct {
	FromBranchID string `json:"fromBranchId,omitempty"`
	ToBranchID   string `json:"toBranchId,omitempty"`
}

type InvestigationBranch struct {
	ID           string   `json:"id,omitempty"`
	Question     string   `json:"question,omitempty"`
	Role         string   `json:"role,omitempty"`
	ProductHints []string `json:"productHints,omitempty"`
	GeoHints     []string `json:"geoHints,omitempty"`
	ActorHints   []string `json:"actorHints,omitempty"`
	RequiresPath bool     `json:"requiresPath,omitempty"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	TestCriteria []string `json:"testCriteria,omitempty"`
	BroadScan    bool     `json:"broadScan,omitempty"`
	// ExpectedInfoGain is planner-estimated marginal value of running this branch (0..1).
	ExpectedInfoGain float64 `json:"expectedInfoGain,omitempty"`
}

type InvestigationBranchStatus string

const (
	BranchStatusProposed   InvestigationBranchStatus = "proposed"
	BranchStatusActive     InvestigationBranchStatus = "active"
	BranchStatusBlocked    InvestigationBranchStatus = "blocked"
	BranchStatusSupported  InvestigationBranchStatus = "supported"
	BranchStatusWeak       InvestigationBranchStatus = "weak"
	BranchStatusCandidate  InvestigationBranchStatus = "candidate_needs_review"
	BranchStatusContradict InvestigationBranchStatus = "contradicted"
	BranchStatusDone       InvestigationBranchStatus = "done"
	BranchStatusRejected   InvestigationBranchStatus = "rejected"
)

type HypothesisStatus string

const (
	HypothesisProposed                HypothesisStatus = "proposed"
	HypothesisUnderInvestigation      HypothesisStatus = "under_investigation"
	HypothesisSupported               HypothesisStatus = "supported"
	HypothesisWeaklySupported         HypothesisStatus = "weakly_supported"
	HypothesisContradicted            HypothesisStatus = "contradicted"
	HypothesisConfirmedMaterializable HypothesisStatus = "confirmed_materializable"
	HypothesisRejected                HypothesisStatus = "rejected"
)

type BranchPursuitDecision string

const (
	BranchPursuitPress BranchPursuitDecision = "press"
	BranchPursuitPivot BranchPursuitDecision = "pivot"
	BranchPursuitPark  BranchPursuitDecision = "park"
	BranchPursuitDrop  BranchPursuitDecision = "drop"
)

type BranchAssessment struct {
	BranchID           string                `json:"branchId,omitempty"`
	TargetRelevance    float64               `json:"targetRelevance,omitempty"`
	PursuitDecision    BranchPursuitDecision `json:"pursuitDecision,omitempty"`
	PursuitReason      string                `json:"pursuitReason,omitempty"`
	RouteHypothesis    string                `json:"routeHypothesis,omitempty"`
	AlternativeRoute   string                `json:"alternativeRoute,omitempty"`
	SuggestedBranchIDs []string              `json:"suggestedBranchIds,omitempty"`
}

type InvestigationHypothesis struct {
	ID         string           `json:"id,omitempty"`
	BranchID   string           `json:"branchId,omitempty"`
	Statement  string           `json:"statement,omitempty"`
	Status     HypothesisStatus `json:"status,omitempty"`
	Confidence float64          `json:"confidence,omitempty"`
	Evidence   []string         `json:"evidence,omitempty"`
	// TestCriteria are explicit checks the branch must satisfy before strong landing.
	TestCriteria []string `json:"testCriteria,omitempty"`
	// ExpectedInfoGain is planner-estimated usefulness of this branch (0..1).
	ExpectedInfoGain float64 `json:"expectedInfoGain,omitempty"`
	// RefinementCount tracks how often this hypothesis was revised after critic/replan.
	RefinementCount int `json:"refinementCount,omitempty"`
	// LocalReady is true when the branch has graph-quality local substrate worth fixing
	// even if the end-to-end target path is still unresolved.
	LocalReady bool `json:"localReady,omitempty"`
	// TargetBound is true when local substrate is also sufficiently connected to the user goal
	// to participate in target-level path landing/materialization.
	TargetBound bool `json:"targetBound,omitempty"`
	// TargetRelevance is the runtime's estimate that this branch can still move the objective forward.
	TargetRelevance float64 `json:"targetRelevance,omitempty"`
	// PursuitDecision encodes whether this branch should be pressed, pivoted away from, parked, or dropped.
	PursuitDecision BranchPursuitDecision `json:"pursuitDecision,omitempty"`
	// PursuitReason explains why the current branch decision is appropriate.
	PursuitReason string `json:"pursuitReason,omitempty"`
	// RouteHypothesis is the current best route/objective-binding interpretation for this branch.
	RouteHypothesis string `json:"routeHypothesis,omitempty"`
	// AlternativeRoute is the current best alternate route when the branch should pivot away from current claims.
	AlternativeRoute string `json:"alternativeRoute,omitempty"`
	// SuggestedBranchIDs are the runtime's preferred follow-on branches from this branch's current state.
	SuggestedBranchIDs []string `json:"suggestedBranchIds,omitempty"`
	// MaterializationReady is true when critic/state allow graph writes for this thread.
	MaterializationReady bool `json:"materializationReady,omitempty"`
	// PriorConfidence is used for marginal information-gain / stall detection.
	PriorConfidence float64 `json:"priorConfidence,omitempty"`
}

type BranchHistoryEntry struct {
	Turn          int      `json:"turn,omitempty"`
	Action        string   `json:"action,omitempty"`
	Note          string   `json:"note,omitempty"`
	OpenQuestions []string `json:"openQuestions,omitempty"`
}

type BranchRuntimeState struct {
	Branch             InvestigationBranch       `json:"branch,omitempty"`
	Status             InvestigationBranchStatus `json:"status,omitempty"`
	ResearchRounds     int                       `json:"researchRounds,omitempty"`
	ReformulationCount int                       `json:"reformulationCount,omitempty"`
	ConsecutiveGapHits int                       `json:"consecutiveGapHits,omitempty"`
	LastConfidence     float64                   `json:"lastConfidence,omitempty"`
	OpenQuestions      []string                  `json:"openQuestions,omitempty"`
	Dependencies       []string                  `json:"dependencies,omitempty"`
	History            []BranchHistoryEntry      `json:"history,omitempty"`
	// MarginalGainStreak counts consecutive low-delta confidence rounds (convergence).
	MarginalGainStreak int `json:"marginalGainStreak,omitempty"`
}

// OpenQuestionEntry is a first-class open question (fit-gap: structured open questions).
type OpenQuestionEntry struct {
	ID                 string   `json:"id,omitempty"`
	Text               string   `json:"text,omitempty"`
	BlockedByBranchIDs []string `json:"blockedByBranchIds,omitempty"`
	Source             string   `json:"source,omitempty"`
}

type ReasoningEvidenceStatus string

const (
	EvidenceObserved   ReasoningEvidenceStatus = "observed"
	EvidenceInferred   ReasoningEvidenceStatus = "inferred"
	EvidenceUnverified ReasoningEvidenceStatus = "unverified"
)

type RuntimeDiagnosis struct {
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
}

type ReasoningBeat struct {
	Turn                   int                     `json:"turn,omitempty"`
	Phase                  string                  `json:"phase,omitempty"`
	Title                  string                  `json:"title,omitempty"`
	QuestionWeAreAnswering string                  `json:"questionWeAreAnswering,omitempty"`
	WhatWeLookedAt         []string                `json:"whatWeLookedAt,omitempty"`
	WhatWeFound            []string                `json:"whatWeFound,omitempty"`
	WhatWeDidNotFind       []string                `json:"whatWeDidNotFind,omitempty"`
	Inference              string                  `json:"inference,omitempty"`
	Decision               string                  `json:"decision,omitempty"`
	DecisionWhy            string                  `json:"decisionWhy,omitempty"`
	BranchIDs              []string                `json:"branchIds,omitempty"`
	GraphOpRefs            []string                `json:"graphOpRefs,omitempty"`
	EvidenceStatus         ReasoningEvidenceStatus `json:"evidenceStatus,omitempty"`
	Diagnostics            []RuntimeDiagnosis      `json:"diagnostics,omitempty"`
}

type ReasoningTrace struct {
	Beats            []ReasoningBeat    `json:"beats,omitempty"`
	RuntimeDiagnosis []RuntimeDiagnosis `json:"runtimeDiagnosis,omitempty"`
	Summary          string             `json:"summary,omitempty"`
}

type InvestigationState struct {
	Request                 InvestigationRequest                `json:"request,omitempty"`
	Goal                    GoalFrame                           `json:"goal,omitempty"`
	Environment             EnvironmentFrame                    `json:"environment,omitempty"`
	Role                    RoleFrame                           `json:"role,omitempty"`
	Completeness            CompletenessFrame                   `json:"completeness,omitempty"`
	Plan                    InvestigationPlan                   `json:"plan,omitempty"`
	Turn                    int                                 `json:"turn,omitempty"`
	MaxTurns                int                                 `json:"maxTurns,omitempty"`
	NeedsReplan             bool                                `json:"needsReplan,omitempty"`
	ReplanReason            string                              `json:"replanReason,omitempty"`
	StopReason              string                              `json:"stopReason,omitempty"`
	BridgeAttempts          int                                 `json:"bridgeAttempts,omitempty"`
	PathAttempts            int                                 `json:"pathAttempts,omitempty"`
	MaterializeAttempts     int                                 `json:"materializeAttempts,omitempty"`
	RepeatedGapCount        int                                 `json:"repeatedGapCount,omitempty"`
	StalledTurns            int                                 `json:"stalledTurns,omitempty"`
	OpenQuestions           []string                            `json:"openQuestions,omitempty"`
	StructuredOpenQuestions []OpenQuestionEntry                 `json:"structuredOpenQuestions,omitempty"`
	BranchOrder             []string                            `json:"branchOrder,omitempty"`
	Branches                map[string]*BranchRuntimeState      `json:"branches,omitempty"`
	Hypotheses              map[string]*InvestigationHypothesis `json:"hypotheses,omitempty"`
	ResearchResults         []ResearchResult                    `json:"researchResults,omitempty"`
	BridgeResults           []BridgeResult                      `json:"bridgeResults,omitempty"`
	LatestBridge            BridgeResult                        `json:"latestBridge,omitempty"`
	LatestCritic            CriticResult                        `json:"latestCritic,omitempty"`
	PathResult              PathResult                          `json:"pathResult,omitempty"`
	MaterializeResult       MaterializeResult                   `json:"materializeResult,omitempty"`
	LatestGapDiagnosis      GapDiagnosis                        `json:"latestGapDiagnosis,omitempty"`
	GapDiagnoses            []GapDiagnosis                      `json:"gapDiagnoses,omitempty"`
	LatestRecommendedAction ActionRecommendation                `json:"latestRecommendedAction,omitempty"`
	RecommendedActions      []ActionRecommendation              `json:"recommendedActions,omitempty"`
	LatestDecision          DecisionResult                      `json:"latestDecision,omitempty"`
	Decisions               []DecisionResult                    `json:"decisions,omitempty"`
	LatestOutcome           ActionOutcome                       `json:"latestOutcome,omitempty"`
	Outcomes                []ActionOutcome                     `json:"outcomes,omitempty"`
	LastSystemErrorKey      string                              `json:"lastSystemErrorKey,omitempty"`
	SystemErrorCount        int                                 `json:"systemErrorCount,omitempty"`
	ActionLog               []string                            `json:"actionLog,omitempty"`
	// PlanVersion tracks merges from reframing (aligned with Plan.PlanVersion).
	PlanVersion int `json:"planVersion,omitempty"`
	// ReplanTriggerCount counts replan events (epistemic control signal).
	ReplanTriggerCount int `json:"replanTriggerCount,omitempty"`
	// ReframeCount bounds query/plan reframing (reframe-once rule extended to small cap).
	ReframeCount int `json:"reframeCount,omitempty"`
	// SessionRef is the shared Vertex session used for investigation memory (when configured).
	SessionRef string `json:"sessionRef,omitempty"`
	// CurrentBottleneck is a single dominant blocker for the next decision turn.
	CurrentBottleneck string `json:"currentBottleneck,omitempty"`
	// ConsecutiveZeroGainTurns counts sequential turns where qualityDelta=none.
	ConsecutiveZeroGainTurns int `json:"consecutiveZeroGainTurns,omitempty"`
	// ConsecutiveZeroHitTurns counts sequential retrieval turns with hitCount=0.
	ConsecutiveZeroHitTurns int `json:"consecutiveZeroHitTurns,omitempty"`
	// TotalRetrievalHits accumulates total KB hits across all research/bridge turns.
	TotalRetrievalHits int `json:"totalRetrievalHits,omitempty"`
	// WebSearchUsed indicates whether external (Google Search) grounding was triggered.
	WebSearchUsed bool `json:"webSearchUsed,omitempty"`
	// LastCompactedTurn tracks when active-state compaction was last applied.
	LastCompactedTurn int `json:"lastCompactedTurn,omitempty"`
	// PendingGraphOps accumulates all proposed graph operations across turns.
	// Status lifecycle: proposed -> accepted -> verified -> landed | rejected.
	PendingGraphOps []ProposedGraphOp `json:"pendingGraphOps,omitempty"`
	// Patches accumulates all graph diffs across investigation turns.
	Patches []GraphPatch `json:"patches,omitempty"`
	// ReasoningTrace is operator-facing explainable reasoning trail (not raw CoT).
	ReasoningTrace []ReasoningBeat `json:"reasoningTrace,omitempty"`
	// RuntimeDiagnosis captures architecture/runtime issues diagnosed during execution.
	RuntimeDiagnosis []RuntimeDiagnosis `json:"runtimeDiagnosis,omitempty"`
}

type InvestigationBottleneck string

const (
	BottleneckMissingSubstrate    InvestigationBottleneck = "missing_substrate"
	BottleneckUnresolvedConflict  InvestigationBottleneck = "unresolved_contradiction"
	BottleneckMissingMiddle       InvestigationBottleneck = "missing_middle_gap"
	BottleneckPathContinuityGap   InvestigationBottleneck = "path_continuity_gap"
	BottleneckTargetBindingGap    InvestigationBottleneck = "target_binding_gap"
	BottleneckMaterializationHold InvestigationBottleneck = "materialization_block"
	BottleneckLowConfidenceClaim  InvestigationBottleneck = "low_confidence_claim"
	BottleneckNoHighValueAction   InvestigationBottleneck = "no_high_value_action"
)

type FrontierBranchSnapshot struct {
	BranchID             string                    `json:"branchId,omitempty"`
	Status               InvestigationBranchStatus `json:"status,omitempty"`
	ResearchRounds       int                       `json:"researchRounds,omitempty"`
	LastConfidence       float64                   `json:"lastConfidence,omitempty"`
	Role                 string                    `json:"role,omitempty"`
	BroadScan            bool                      `json:"broadScan,omitempty"`
	LocalReady           bool                      `json:"localReady,omitempty"`
	TargetBound          bool                      `json:"targetBound,omitempty"`
	TargetRelevance      float64                   `json:"targetRelevance,omitempty"`
	PursuitDecision      BranchPursuitDecision     `json:"pursuitDecision,omitempty"`
	RouteHypothesis      string                    `json:"routeHypothesis,omitempty"`
	SuggestedBranchIDs   []string                  `json:"suggestedBranchIds,omitempty"`
	MaterializationReady bool                      `json:"materializationReady,omitempty"`
	ExpectedInfoGain     float64                   `json:"expectedInfoGain,omitempty"`
	Question             string                    `json:"question,omitempty"`
	ProductHints         []string                  `json:"productHints,omitempty"`
	OpenQuestions        []string                  `json:"openQuestions,omitempty"`
}

type ActiveFrontierSnapshot struct {
	Turn             int                      `json:"turn,omitempty"`
	Bottleneck       InvestigationBottleneck  `json:"bottleneck,omitempty"`
	TopBranches      []FrontierBranchSnapshot `json:"topBranches,omitempty"`
	OpenQuestions    []string                 `json:"openQuestions,omitempty"`
	RecentActions    []string                 `json:"recentActions,omitempty"`
	PathContinuity   string                   `json:"pathContinuity,omitempty"`
	ReplanReason     string                   `json:"replanReason,omitempty"`
	CandidateActions []string                 `json:"candidateActions,omitempty"`
}

type ResearchDelta struct {
	BranchID          string               `json:"branchId,omitempty"`
	Confidence        float64              `json:"confidence,omitempty"`
	CanonicalRefs     []string             `json:"canonicalRefs,omitempty"`
	EvidencePoints    []string             `json:"evidencePoints,omitempty"`
	OpenQuestions     []string             `json:"openQuestions,omitempty"`
	NextImplication   string               `json:"nextImplication,omitempty"`
	ProposedOps       []ProposedGraphOp    `json:"proposedGraphOps,omitempty"`
	GapDiagnosis      GapDiagnosis         `json:"gapDiagnosis,omitempty"`
	RecommendedAction ActionRecommendation `json:"recommendedAction,omitempty"`
}

type CriticDelta struct {
	AcceptedFindings        []string               `json:"acceptedFindings,omitempty"`
	RiskyClaims             []string               `json:"riskyClaims,omitempty"`
	OpenQuestions           []string               `json:"openQuestions,omitempty"`
	MaterializePath         bool                   `json:"materializePath,omitempty"`
	PathProducts            []string               `json:"pathProducts,omitempty"`
	FocusActors             []string               `json:"focusActors,omitempty"`
	FocusGeos               []string               `json:"focusGeos,omitempty"`
	Confidence              float64                `json:"confidence,omitempty"`
	WithholdPath            bool                   `json:"withholdPath,omitempty"`
	WithholdMaterialization bool                   `json:"withholdMaterialization,omitempty"`
	ForceReplan             bool                   `json:"forceReplan,omitempty"`
	KillBranchIDs           []string               `json:"killBranchIds,omitempty"`
	NewBranches             []InvestigationBranch  `json:"newBranches,omitempty"`
	ReprioritizeBranchIDs   []string               `json:"reprioritizeBranchIds,omitempty"`
	RiskTargetBranchIDs     []string               `json:"riskTargetBranchIds,omitempty"`
	GraphOpVerdicts         []GraphOpVerdict       `json:"graphOpVerdicts,omitempty"`
	BranchAssessments       []BranchAssessment     `json:"branchAssessments,omitempty"`
	CompletenessAssessment  CompletenessAssessment `json:"completenessAssessment,omitempty"`
	GoodEnoughAssessment    GoodEnoughAssessment   `json:"goodEnoughAssessment,omitempty"`
	GapDiagnosis            GapDiagnosis           `json:"gapDiagnosis,omitempty"`
	RecommendedAction       ActionRecommendation   `json:"recommendedAction,omitempty"`
}

type MaterializeDelta struct {
	Summary           string   `json:"summary,omitempty"`
	Skipped           []string `json:"skipped,omitempty"`
	Downgraded        []string `json:"downgraded,omitempty"`
	Rejected          []string `json:"rejected,omitempty"`
	LandedBranchIDs   []string `json:"landedBranchIds,omitempty"`
	RejectedBranchIDs []string `json:"rejectedBranchIds,omitempty"`
}

// GapDiagnosis is the explicit diagnosis of what is missing right now.
// It separates "something is missing" from "which method should run next".
type GapDiagnosis struct {
	Kind           string `json:"kind,omitempty"`          // missing_substrate|missing_middle|missing_transform_chain|missing_institutional_layer|missing_regulatory_layer|missing_ownership_layer|missing_verification|missing_adjudication|missing_completeness|no_gap
	Summary        string `json:"summary,omitempty"`       // short operator-facing diagnosis
	Why            string `json:"why,omitempty"`           // why this is the right diagnosis
	QuestionClass  string `json:"questionClass,omitempty"` // space_scan|bridge_query|verification|adjudication|completeness_scan
	NextAction     string `json:"nextAction,omitempty"`    // research|bridge|path|critic|materialize|replan|stop
	BranchID       string `json:"branchId,omitempty"`
	EndpointA      string `json:"endpointA,omitempty"`
	EndpointB      string `json:"endpointB,omitempty"`
	EndpointsKnown bool   `json:"endpointsKnown,omitempty"`
}

// ActionRecommendation is the machine-readable next-step contract. It lets a
// task say which neighboring method should run next when the current one did
// not close the gap.
type ActionRecommendation struct {
	Action       string   `json:"action,omitempty"` // research|bridge|path|critic|materialize|replan|stop
	BranchID     string   `json:"branchId,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	FailureMode  string   `json:"failureMode,omitempty"`
	Question     string   `json:"question,omitempty"`
	EndpointA    string   `json:"endpointA,omitempty"`
	EndpointB    string   `json:"endpointB,omitempty"`
	ProductHints []string `json:"productHints,omitempty"`
	GeoHints     []string `json:"geoHints,omitempty"`
	ActorHints   []string `json:"actorHints,omitempty"`
	RequiresPath bool     `json:"requiresPath,omitempty"`
}

type DecisionCandidate struct {
	Action          string   `json:"action,omitempty"` // research|bridge|path|critic|materialize|replan|stop
	BranchID        string   `json:"branchId,omitempty"`
	Available       bool     `json:"available,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Question        string   `json:"question,omitempty"`
	EndpointA       string   `json:"endpointA,omitempty"`
	EndpointB       string   `json:"endpointB,omitempty"`
	ProductHints    []string `json:"productHints,omitempty"`
	GeoHints        []string `json:"geoHints,omitempty"`
	ActorHints      []string `json:"actorHints,omitempty"`
	DataSourceHint  string   `json:"dataSourceHint,omitempty"` // describes what data sources this action uses; helps orchestrator reason about expected return
}

// ActionOutcome is the single source-of-truth packet describing what the last
// executed method actually produced. It is what the orchestration decider
// should reason over instead of reverse-engineering progress from many fields.
type ActionOutcome struct {
	Turn                 int                  `json:"turn,omitempty"`
	Action               string               `json:"action,omitempty"`
	BranchID             string               `json:"branchId,omitempty"`
	OutcomeClass         string               `json:"outcomeClass,omitempty"` // expanded_substrate|structured_bridge|adjudicated_state|validated_chain|materialized_graph|strategy_update|system_error|no_progress
	QualityDelta         string               `json:"qualityDelta,omitempty"` // material|incremental|none|negative
	Summary              string               `json:"summary,omitempty"`
	GraphUseful          bool                 `json:"graphUseful,omitempty"`
	SystemError          bool                 `json:"systemError,omitempty"`
	Retryable            bool                 `json:"retryable,omitempty"`
	RetryAttempt         int                  `json:"retryAttempt,omitempty"`
	ErrorMessage         string               `json:"errorMessage,omitempty"`
	PathContinuity       string               `json:"pathContinuity,omitempty"`
	BridgeExists         bool                 `json:"bridgeExists,omitempty"`
	ProducedGraphOps     int                  `json:"producedGraphOps,omitempty"`
	ProducedArtifacts    int                  `json:"producedArtifacts,omitempty"`
	OpenQuestions        []string             `json:"openQuestions,omitempty"`
	EvidenceRefs         []string             `json:"evidenceRefs,omitempty"`
	GapDiagnosis         GapDiagnosis         `json:"gapDiagnosis,omitempty"`
	RecommendedAction    ActionRecommendation `json:"recommendedAction,omitempty"`
	GoodEnoughAssessment GoodEnoughAssessment `json:"goodEnoughAssessment,omitempty"`
	RetrievalHitCount    int                  `json:"retrievalHitCount,omitempty"`
	ToolCallCount        int                  `json:"toolCallCount,omitempty"`
	// DataSourceSignal is a machine-readable signal describing what happened at the data layer.
	// internal_empty  — internal KB returned zero hits; external search is the only option.
	// internal_partial — internal KB returned hits but evidence was weak or ungrounded.
	// internal_grounded — internal KB returned hits with strong grounding evidence.
	// external_only — action used only web-grounded reasoning (e.g. bridge), internal KB not queried.
	// no_data — no useful data from any source.
	DataSourceSignal string `json:"dataSourceSignal,omitempty"`
}

type StopCriterionEvaluation struct {
	Criterion string `json:"criterion,omitempty"`
	Satisfied bool   `json:"satisfied,omitempty"`
	Why       string `json:"why,omitempty"`
}

// DecisionResult is the explicit orchestration decision packet for one turn.
// It makes next-action selection inspectable instead of burying it in mixed
// heuristics and downstream side effects.
type DecisionResult struct {
	StateSummary            string                    `json:"stateSummary,omitempty"`
	ChosenAction            string                    `json:"chosenAction,omitempty"`
	BranchID                string                    `json:"branchId,omitempty"`
	WhyChosen               string                    `json:"whyChosen,omitempty"`
	WhyNotStop              string                    `json:"whyNotStop,omitempty"`
	WhyNotPath              string                    `json:"whyNotPath,omitempty"`
	WhyNotBridge            string                    `json:"whyNotBridge,omitempty"`
	WhyNotResearch          string                    `json:"whyNotResearch,omitempty"`
	WhyNotCritic            string                    `json:"whyNotCritic,omitempty"`
	WhyNotMaterialize       string                    `json:"whyNotMaterialize,omitempty"`
	WhyNotReplan            string                    `json:"whyNotReplan,omitempty"`
	EvidenceUsed            []string                  `json:"evidenceUsed,omitempty"`
	StopCriteria            []StopCriterionEvaluation `json:"stopCriteriaEvaluation,omitempty"`
	ExpectedInformationGain float64                   `json:"expectedInformationGain,omitempty"`
	Confidence              float64                   `json:"confidence,omitempty"`
	Candidates              []DecisionCandidate       `json:"candidates,omitempty"`
	ExecutedAction          string                    `json:"executedAction,omitempty"`
	ExecutedBranchID        string                    `json:"executedBranchId,omitempty"`
	ValidationStatus        string                    `json:"validationStatus,omitempty"` // accepted|fallback
	ValidationReason        string                    `json:"validationReason,omitempty"`
}

// ProposedGraphOp is the fundamental unit of reasoning output — a concrete,
// verifiable proposal to modify the graph (digital twin).
type ProposedGraphOp struct {
	Kind        string  `json:"kind"`                 // create_fragment, create_edge, confirm_edge, mark_gap
	SubjectPath string  `json:"subjectPath"`          // canonical path of the entity/product
	ObjectPath  string  `json:"objectPath,omitempty"` // for edges: target canonical path
	EdgeType    string  `json:"edgeType,omitempty"`   // transforms_into, assembles_into, supply_link, DEPENDS_ON
	Rationale   string  `json:"rationale,omitempty"`  // evidence basis for this proposal
	Confidence  float64 `json:"confidence"`           // computed, not self-assessed
	Status      string  `json:"status"`               // proposed, accepted_local, accepted, verified_local, verified, landed, rejected
	LocalReady  bool    `json:"localReady,omitempty"`
	TargetBound bool    `json:"targetBound,omitempty"`
	SourceTurn  int     `json:"sourceTurn,omitempty"` // turn that produced this op
	BranchID    string  `json:"branchId,omitempty"`   // originating branch
}

// GraphOpVerdict is a critic's assessment of a single ProposedGraphOp.
type GraphOpVerdict struct {
	OpIndex int    `json:"opIndex"`
	Verdict string `json:"verdict"` // accepted_local, accepted, accepted_target, rejected, needs_research, needs_target_binding
	Reason  string `json:"reason,omitempty"`
}

type ResearchResult struct {
	BranchID                string               `json:"branchId,omitempty"`
	Summary                 string               `json:"summary,omitempty"`
	EvidencePoints          []string             `json:"evidencePoints,omitempty"`
	CanonicalRefs           []string             `json:"canonicalRefs,omitempty"`
	ProductRefs             []string             `json:"productRefs,omitempty"`
	GeoRefs                 []string             `json:"geoRefs,omitempty"`
	ActorRefs               []string             `json:"actorRefs,omitempty"`
	RecommendedPathProducts []string             `json:"recommendedPathProducts,omitempty"`
	OpenQuestions           []string             `json:"openQuestions,omitempty"`
	Confidence              float64              `json:"confidence,omitempty"`
	TestCriteria            []string             `json:"testCriteria,omitempty"`
	ProposedOps             []ProposedGraphOp    `json:"proposedGraphOps,omitempty"`
	GapDiagnosis            GapDiagnosis         `json:"gapDiagnosis,omitempty"`
	RecommendedAction       ActionRecommendation `json:"recommendedAction,omitempty"`
	DataSourceSignal        string               `json:"dataSourceSignal,omitempty"` // model's own assessment: internal_empty|internal_partial|internal_grounded|no_data
}

type BridgeResult struct {
	BranchID                  string                `json:"branchId,omitempty"`
	EndpointA                 string                `json:"endpointA,omitempty"`
	EndpointB                 string                `json:"endpointB,omitempty"`
	BridgeQuestion            string                `json:"bridgeQuestion,omitempty"`
	Summary                   string                `json:"summary,omitempty"`
	BridgeExists              bool                  `json:"bridgeExists,omitempty"`
	BridgeKind                string                `json:"bridgeKind,omitempty"`
	MissingMiddleTypes        []string              `json:"missingMiddleTypes,omitempty"`
	IntermediateNodes         []string              `json:"intermediateNodes,omitempty"`
	IntermediateRelationTypes []string              `json:"intermediateRelationTypes,omitempty"`
	BridgeSteps               []BridgeStep          `json:"bridgeSteps,omitempty"`
	RouteSketches             []string              `json:"routeSketches,omitempty"`
	OpenQuestions             []string              `json:"openQuestions,omitempty"`
	Confidence                float64               `json:"confidence,omitempty"`
	ProposedOps               []ProposedGraphOp     `json:"proposedGraphOps,omitempty"`
	SuggestedSubscans         []InvestigationBranch `json:"suggestedSubscans,omitempty"`
	GapDiagnosis              GapDiagnosis          `json:"gapDiagnosis,omitempty"`
	RecommendedAction         ActionRecommendation  `json:"recommendedAction,omitempty"`
	DataSourceSignal          string                `json:"dataSourceSignal,omitempty"` // model's own assessment: external_only|internal_partial|internal_grounded|no_data
}

// BridgeStep is the articulation layer between a textual missing-middle answer
// and concrete graph-native artifacts. It lets the model "write out" what sits
// between A and B as ordered rows the runtime can turn into candidate ops.
type BridgeStep struct {
	Index             int      `json:"index,omitempty"`
	StepClass         string   `json:"stepClass,omitempty"`    // node|edge|transform|logistics|contract|port|processor|market_link
	NodeRef           string   `json:"nodeRef,omitempty"`      // canonical path if known
	NodeLabel         string   `json:"nodeLabel,omitempty"`    // free-text label if canonical path unknown
	NodeKind          string   `json:"nodeKind,omitempty"`     // facility|port|org|product|market|route|contract|intermediary
	FromRef           string   `json:"fromRef,omitempty"`      // canonical/source ref
	ToRef             string   `json:"toRef,omitempty"`        // canonical/destination ref
	RelationType      string   `json:"relationType,omitempty"` // supply_link|transforms_into|DEPENDS_ON|OPERATES_IN...
	ProductRef        string   `json:"productRef,omitempty"`
	ActorRef          string   `json:"actorRef,omitempty"`
	GeoRef            string   `json:"geoRef,omitempty"`
	WhyNeeded         string   `json:"whyNeeded,omitempty"`
	EvidenceHints     []string `json:"evidenceHints,omitempty"`
	OperationalStatus string   `json:"operationalStatus,omitempty"` // operational|announced|pilot|unclear
}

type CriticResult struct {
	AcceptedFindings []string `json:"acceptedFindings,omitempty"`
	RiskyClaims      []string `json:"riskyClaims,omitempty"`
	OpenQuestions    []string `json:"openQuestions,omitempty"`
	MaterializePath  bool     `json:"materializePath,omitempty"`
	PathProducts     []string `json:"pathProducts,omitempty"`
	FocusActors      []string `json:"focusActors,omitempty"`
	FocusGeos        []string `json:"focusGeos,omitempty"`
	Confidence       float64  `json:"confidence,omitempty"`
	// Epistemic controls (fit-gap §6 critic as control, not reviewer-only).
	WithholdPath            bool                  `json:"withholdPath,omitempty"`
	WithholdMaterialization bool                  `json:"withholdMaterialization,omitempty"`
	ForceReplan             bool                  `json:"forceReplan,omitempty"`
	KillBranchIDs           []string              `json:"killBranchIds,omitempty"`
	NewBranches             []InvestigationBranch `json:"newBranches,omitempty"`
	ReprioritizeBranchIDs   []string              `json:"reprioritizeBranchIds,omitempty"`
	// RiskTargetBranchIDs: branches whose supported claims are challenged by riskyClaims (epistemic targeting).
	RiskTargetBranchIDs    []string               `json:"riskTargetBranchIds,omitempty"`
	GraphOpVerdicts        []GraphOpVerdict       `json:"graphOpVerdicts,omitempty"`
	BranchAssessments      []BranchAssessment     `json:"branchAssessments,omitempty"`
	CompletenessAssessment CompletenessAssessment `json:"completenessAssessment,omitempty"`
	GoodEnoughAssessment   GoodEnoughAssessment   `json:"goodEnoughAssessment,omitempty"`
	GapDiagnosis           GapDiagnosis           `json:"gapDiagnosis,omitempty"`
	RecommendedAction      ActionRecommendation   `json:"recommendedAction,omitempty"`
}

type CompletenessAssessment struct {
	Status                 string   `json:"status,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	AlternativeRoutes      []string `json:"alternativeRoutes,omitempty"`
	MissingAngles          []string `json:"missingAngles,omitempty"`
	BroadScanCompleted     bool     `json:"broadScanCompleted,omitempty"`
	NoMaterialAlternatives bool     `json:"noMaterialAlternatives,omitempty"`
}

type GoodEnoughAssessment struct {
	Status              string   `json:"status,omitempty"` // insufficient|local_good_enough|route_good_enough|business_good_enough
	ClosureLevel        string   `json:"closureLevel,omitempty"`
	StopNow             bool     `json:"stopNow,omitempty"`
	PersistWorkingPath  bool     `json:"persistWorkingPath,omitempty"`
	FurtherWorkLowValue bool     `json:"furtherWorkLowValue,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	OpenNonBlockingGaps []string `json:"openNonBlockingGaps,omitempty"`
	TargetBranchIDs     []string `json:"targetBranchIds,omitempty"`
}

type PathResult struct {
	QueriedProducts         []string         `json:"queriedProducts,omitempty"`
	TraceDigests            []map[string]any `json:"traceDigests,omitempty"`
	MaterializedArtifactIDs []string         `json:"materializedArtifactIds,omitempty"`
	AssemblyArtifactID      string           `json:"assemblyArtifactId,omitempty"`
	OpenGaps                []string         `json:"openGaps,omitempty"`
	Confidence              float64          `json:"confidence,omitempty"`
	// ContinuityStatus is path landing state machine output (fit-gap §7).
	ContinuityStatus string           `json:"continuityStatus,omitempty"`
	PathHypotheses   []PathHypothesis `json:"pathHypotheses,omitempty"`
	// SourceBranchIDs: investigation branches this path step was scoped to (orchestrator + path agree).
	SourceBranchIDs   []string             `json:"sourceBranchIds,omitempty"`
	GapDiagnosis      GapDiagnosis         `json:"gapDiagnosis,omitempty"`
	RecommendedAction ActionRecommendation `json:"recommendedAction,omitempty"`
}

// PathHypothesis captures per-anchor path/binding hypotheses for replanning.
type PathHypothesis struct {
	ProductRef    string `json:"productRef,omitempty"`
	BindingStatus string `json:"bindingStatus,omitempty"`
	GapSummary    string `json:"gapSummary,omitempty"`
}

type MaterializeResult struct {
	ArtifactIDs []string `json:"artifactIds,omitempty"`
	Persisted   bool     `json:"persisted,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Created     []string `json:"created,omitempty"`
	Updated     []string `json:"updated,omitempty"`
	Skipped     []string `json:"skipped,omitempty"`
	Downgraded  []string `json:"downgraded,omitempty"`
	Rejected    []string `json:"rejected,omitempty"`
	// CorrectionsApplied records correction_proposals outcomes (fit-gap §8).
	CorrectionsApplied []MaterializeCorrectionRecord `json:"correctionsApplied,omitempty"`
	// LandedBranchIDs: branches whose hypotheses were actually landed by this materialize step.
	LandedBranchIDs []string `json:"landedBranchIds,omitempty"`
	// RejectedBranchIDs: branches explicitly not landed (policy / evidence).
	RejectedBranchIDs []string `json:"rejectedBranchIds,omitempty"`
}

// MaterializeCorrectionRecord is one applied or attempted graph correction.
type MaterializeCorrectionRecord struct {
	Action     string `json:"action,omitempty"`
	ArtifactID string `json:"artifactId,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
	Applied    bool   `json:"applied,omitempty"`
	Error      string `json:"error,omitempty"`
}

type AggregateResult struct {
	Summary             string           `json:"summary,omitempty"`
	Findings            []string         `json:"findings,omitempty"`
	OpenQuestions       []string         `json:"openQuestions,omitempty"`
	ArtifactIDs         []string         `json:"artifactIds,omitempty"`
	TraceDigests        []map[string]any `json:"traceDigests,omitempty"`
	CandidateProducts   []string         `json:"candidateProducts,omitempty"`
	RecommendedNextStep string           `json:"recommendedNextStep,omitempty"`
	CompareOnly         bool             `json:"compareOnly,omitempty"`
}

type ReviewResult struct {
	Accepted  bool     `json:"accepted"`
	Rationale string   `json:"rationale,omitempty"`
	Quality   string   `json:"quality,omitempty"`
	Issues    []string `json:"issues,omitempty"`
}

// ToolResponse is the uniform envelope every scout tool returns.
// The model always sees the same shape: a machine-readable status,
// a natural-language explanation, and tool-specific structured data.
type ToolResponse struct {
	Status      string `json:"status"`      // found | not_found | partial | error
	Explanation string `json:"explanation"`  // deterministic NL summary of what happened and what to do next
	Data        any    `json:"data"`         // tool-specific payload
}

// GraphPatch is the uniform "diff" envelope wrapping proposed graph operations.
// Regardless of which step produced it (research, bridge, path), the patch
// has the same shape: ops + human-readable summary + unresolved identities.
type GraphPatch struct {
	BranchID   string            `json:"branchId"`
	SourceStep string            `json:"sourceStep"` // research | bridge | path | materialize
	Turn       int               `json:"turn"`
	Ops        []ProposedGraphOp `json:"ops"`
	Summary    string            `json:"summary"`    // NL description of what this patch does to the graph
	Confidence float64           `json:"confidence"`
	Unresolved []string          `json:"unresolved"` // products/relations needing candidate registration
}

// ToolCallLog records a single tool invocation for stats and debugging.
type ToolCallLog struct {
	ToolName   string `json:"toolName"`
	Args       string `json:"args,omitempty"`
	Status     string `json:"status"`
	DurationMs int64  `json:"durationMs,omitempty"`
	HitCount   int    `json:"hitCount,omitempty"`
}
