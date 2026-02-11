package models

import (
	"fmt"
	"strings"
	"time"
)

type RiskJobState string

const (
	RiskJobStatePending   RiskJobState = "PENDING"
	RiskJobStateRunning   RiskJobState = "RUNNING"
	RiskJobStateCompleted RiskJobState = "COMPLETED"
	RiskJobStateFailed    RiskJobState = "FAILED"
	RiskJobStatePoisoned  RiskJobState = "POISONED"
	RiskJobStateCancelled RiskJobState = "CANCELLED"
)

type RiskStage string

const (
	RiskStageIdentify    RiskStage = "IDENTIFY"
	RiskStageEscalate    RiskStage = "ESCALATE"
	RiskStageApply       RiskStage = "APPLICABILITY"
	RiskStageSeed        RiskStage = "SEED"
	RiskStagePropagate   RiskStage = "PROPAGATE"
	RiskStageRollup      RiskStage = "ROLLUP"
	RiskStageInterpret   RiskStage = "INTERPRET"
	RiskStageAlert       RiskStage = "ALERT"
	RiskStageTerminalEnd RiskStage = "TERMINAL"
)

type RiskStageDecision string

const (
	RiskStageDecisionPass RiskStageDecision = "pass"
	RiskStageDecisionSkip RiskStageDecision = "skip"
	RiskStageDecisionFail RiskStageDecision = "fail"
)

type RiskReasonCode string

const (
	RiskReasonSemanticUnknown       RiskReasonCode = "E_SEMANTIC_SCENARIO_UNKNOWN"
	RiskReasonStageInputInvalid     RiskReasonCode = "E_STAGE_INPUT_INVALID"
	RiskReasonStageTimeout          RiskReasonCode = "E_STAGE_TIMEOUT"
	RiskReasonDependencyUnavailable RiskReasonCode = "E_DEPENDENCY_UNAVAILABLE"
	RiskReasonNoExposurePath        RiskReasonCode = "E_APPLICABILITY_NO_EXPOSURE_PATH"
	RiskReasonPropagationPolicyMiss RiskReasonCode = "E_PROPAGATION_POLICY_MISSING"
	RiskReasonAlertEvalFailed       RiskReasonCode = "E_ALERT_POLICY_EVAL_FAILED"

	RiskReasonSparseGraphPenalty RiskReasonCode = "W_SPARSE_GRAPH_PENALTY_APPLIED"
	RiskReasonEscalationLLMSkip  RiskReasonCode = "W_ESCALATION_LLM_SKIPPED"
	RiskReasonRollupFallback     RiskReasonCode = "W_ROLLUP_FALLBACK_USED"

	RiskReasonStageSkippedNoSignals RiskReasonCode = "I_STAGE_SKIPPED_NO_SIGNALS"
	RiskReasonAlertSuppressed       RiskReasonCode = "I_ALERT_SUPPRESSED_BY_APPETITE"
)

type RiskStageResult struct {
	Stage       RiskStage         `json:"stage" firestore:"stage"`
	StartedAt   time.Time         `json:"startedAt" firestore:"startedAt"`
	FinishedAt  time.Time         `json:"finishedAt" firestore:"finishedAt"`
	Decision    RiskStageDecision `json:"decision" firestore:"decision"`
	ReasonCodes []string          `json:"reasonCodes,omitempty" firestore:"reasonCodes,omitempty"`
	Attempt     int               `json:"attempt" firestore:"attempt"`
	InputHash   string            `json:"inputHash,omitempty" firestore:"inputHash,omitempty"`
	OutputHash  string            `json:"outputHash,omitempty" firestore:"outputHash,omitempty"`
	WorkerVer   string            `json:"workerVersion,omitempty" firestore:"workerVersion,omitempty"`
}

type RiskJob struct {
	ID string `json:"id" firestore:"id"`

	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId" firestore:"projectId"`
	LensID    string `json:"lensId,omitempty" firestore:"lensId,omitempty"`

	TriggerType string `json:"triggerType" firestore:"triggerType"` // watchdog|event|feedback|manual
	TriggerRef  string `json:"triggerRef,omitempty" firestore:"triggerRef,omitempty"`
	DedupKey    string `json:"dedupKey,omitempty" firestore:"dedupKey,omitempty"`

	TargetCanonicalPath string `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`
	Polarity            string `json:"polarity" firestore:"polarity"` // risk|opportunity

	CurrentStage RiskStage    `json:"currentStage" firestore:"currentStage"`
	CurrentState RiskJobState `json:"currentState" firestore:"currentState"`
	Attempt      int          `json:"attempt" firestore:"attempt"`

	State map[string]any `json:"state,omitempty" firestore:"state,omitempty"`

	TraceRef string `json:"traceRef,omitempty" firestore:"traceRef,omitempty"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	TerminalReason string            `json:"terminalReason,omitempty" firestore:"terminalReason,omitempty"`
	StageHistory   []RiskStageResult `json:"stageHistory,omitempty" firestore:"stageHistory,omitempty"`
}

func (j *RiskJob) NormalizeDefaults() {
	if j == nil {
		return
	}
	if strings.TrimSpace(j.Polarity) == "" {
		j.Polarity = "risk"
	}
	j.Polarity = strings.ToLower(strings.TrimSpace(j.Polarity))
	if j.Polarity != "risk" && j.Polarity != "opportunity" {
		j.Polarity = "risk"
	}
	if j.CurrentStage == "" {
		j.CurrentStage = RiskStageIdentify
	}
	if j.CurrentState == "" {
		j.CurrentState = RiskJobStatePending
	}
	if j.State == nil {
		j.State = map[string]any{}
	}
}

func (j *RiskJob) Validate() error {
	if j == nil {
		return fmt.Errorf("risk job is nil")
	}
	if strings.TrimSpace(j.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(j.ProjectID) == "" {
		return fmt.Errorf("projectId is required")
	}
	if strings.TrimSpace(j.TargetCanonicalPath) == "" {
		return fmt.Errorf("targetCanonicalPath is required")
	}
	if strings.TrimSpace(j.TriggerType) == "" {
		return fmt.Errorf("triggerType is required")
	}
	p := strings.ToLower(strings.TrimSpace(j.Polarity))
	if p != "risk" && p != "opportunity" {
		return fmt.Errorf("polarity must be risk|opportunity")
	}
	return nil
}

func IsRiskTransitionAllowed(from, to RiskJobState) bool {
	if from == to {
		return true
	}
	switch from {
	case RiskJobStatePending:
		return to == RiskJobStateRunning || to == RiskJobStateCancelled
	case RiskJobStateRunning:
		return to == RiskJobStateCompleted || to == RiskJobStateFailed || to == RiskJobStatePoisoned || to == RiskJobStateCancelled
	case RiskJobStateFailed:
		return to == RiskJobStateRunning || to == RiskJobStatePoisoned
	default:
		return false
	}
}

func NextRiskStage(cur RiskStage) RiskStage {
	switch cur {
	case RiskStageIdentify:
		return RiskStageEscalate
	case RiskStageEscalate:
		return RiskStageApply
	case RiskStageApply:
		return RiskStageSeed
	case RiskStageSeed:
		return RiskStagePropagate
	case RiskStagePropagate:
		return RiskStageRollup
	case RiskStageRollup:
		return RiskStageInterpret
	case RiskStageInterpret:
		return RiskStageAlert
	case RiskStageAlert:
		return RiskStageTerminalEnd
	default:
		return RiskStageTerminalEnd
	}
}
