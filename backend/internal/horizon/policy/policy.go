package policy

import (
	"context"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// Policy is the unified interface for all Horizon decision-making policies.
type Policy interface {
	// Name returns the policy identifier (e.g., "propagation", "scout", "canonization").
	Name() string

	// Decide makes a decision based on input context.
	// Returns decision + metadata (which path was taken, confidence, etc.)
	Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error)

	// Explain returns human-readable explanation for a decision (for debugging/audit).
	Explain(decision PolicyDecision) string
}

// PolicyInput contains context for policy decision-making.
type PolicyInput struct {
	Kind     string                         `json:"kind"`               // "propagation", "scout", "canonization", etc.
	Context  map[string]interface{}         `json:"context"`            // Arbitrary context for the decision
	Fragment *horizonmodels.HorizonFragment `json:"fragment,omitempty"` // Optional: relevant fragment
	Edge     *horizonmodels.DependencyEdge  `json:"edge,omitempty"`     // Optional: relevant edge
	Fact     *coremodels.OntologyFact       `json:"fact,omitempty"`     // Optional: relevant fact
}

// PolicyDecision represents the result of a policy decision.
type PolicyDecision struct {
	Action string                 `json:"action"` // e.g., "propagate", "skip", "normalize_to", "synthesize_signal"
	Params map[string]interface{} `json:"params"` // Action-specific parameters

	// Telemetry
	DecisionPath string  `json:"decisionPath"`        // "rule:exact_match" | "rule:pattern" | "llm:fallback"
	RuleID       *string `json:"ruleId,omitempty"`    // If rule-based: which rule
	Confidence   float64 `json:"confidence"`          // 0..1
	LatencyMs    int     `json:"latencyMs"`           // Decision latency
	LLMTokens    *int    `json:"llmTokens,omitempty"` // If LLM was used: token count
}

// TelemetryWriter records policy decisions for analytics.
type TelemetryWriter interface {
	Record(decision PolicyDecision, policyName string) error
}

// PolicyExecutor executes policies with telemetry and LLM fallback support.
type PolicyExecutor struct {
	policy      Policy
	telemetry   TelemetryWriter
	llmExecutor LLMExecutor // Optional: for fallback decisions
}

// LLMExecutor executes LLM-based policy decisions.
type LLMExecutor interface {
	Execute(ctx context.Context, prompt string, input PolicyInput) (PolicyDecision, int, error) // Returns decision, token count, error
}

// NewPolicyExecutor creates a new PolicyExecutor.
func NewPolicyExecutor(policy Policy, telemetry TelemetryWriter, llmExecutor LLMExecutor) *PolicyExecutor {
	return &PolicyExecutor{
		policy:      policy,
		telemetry:   telemetry,
		llmExecutor: llmExecutor,
	}
}

// Execute executes the policy with telemetry recording.
func (e *PolicyExecutor) Execute(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	start := time.Now()
	fields := map[string]interface{}{
		"trace_id":       "na",
		"tenant_id":      "na",
		"job_id":         "na",
		"action":         "",
		"policy_status":  "na",
		"policy_reason":  "",
		"policy_penalty": 0,
		"decision_path":  "executor",
		"confidence":     0,
	}
	if input.Edge != nil {
		fields["edge_id"] = input.Edge.ID
		fields["source_id"] = input.Edge.SourceFragmentID
		fields["target_id"] = input.Edge.TargetFragmentID
		fields["relation_type"] = input.Edge.Type
	}
	if input.Fragment != nil {
		fields["fragment_id"] = input.Fragment.ID
		fields["source_type"] = input.Fragment.Type
		fields["origin_mode"] = input.Fragment.Provenance.OriginMode
		fields["tenant_id"] = input.Fragment.TenantID
	}
	if v, ok := input.Context["tenantID"].(string); ok && v != "" {
		fields["tenant_id"] = v
	}
	if v, ok := input.Context["tenantId"].(string); ok && v != "" {
		fields["tenant_id"] = v
	}
	if v, ok := input.Context["jobID"].(string); ok && v != "" {
		fields["job_id"] = v
	}
	if v, ok := input.Context["jobId"].(string); ok && v != "" {
		fields["job_id"] = v
	}
	if v, ok := input.Context["sourceType"].(string); ok && v != "" {
		fields["source_type"] = v
	}
	if v, ok := input.Context["targetType"].(string); ok && v != "" {
		fields["target_type"] = v
	}
	if v, ok := input.Context["relationType"].(string); ok && v != "" {
		fields["relation_type"] = v
	}
	LogPolicyEvent(e.policy.Name(), PolicyLogStageInput, fields)

	// Try deterministic rules first
	decision, err := e.policy.Decide(ctx, input)
	if err != nil {
		decision.LatencyMs = int(time.Since(start).Milliseconds())
		decision.DecisionPath = "rule:error"
		LogPolicyEvent(e.policy.Name(), PolicyLogStageError, map[string]interface{}{
			"trace_id":       fields["trace_id"],
			"tenant_id":      fields["tenant_id"],
			"job_id":         fields["job_id"],
			"fragment_id":    fields["fragment_id"],
			"edge_id":        fields["edge_id"],
			"action":         decision.Action,
			"policy_status":  "na",
			"policy_reason":  err.Error(),
			"policy_penalty": 0,
			"decision_path":  decision.DecisionPath,
			"error_class":    "policy_decide_error",
			"confidence":     decision.Confidence,
		})
		if e.telemetry != nil {
			_ = e.telemetry.Record(decision, e.policy.Name())
		}
		return decision, err
	}

	if decision.DecisionPath != "llm:fallback" {
		// Rule-based decision
		decision.LatencyMs = int(time.Since(start).Milliseconds())
		reason := decisionReason(decision, "")
		penalty, _ := decision.Params["penalty"].(float64)
		LogPolicyEvent(e.policy.Name(), PolicyLogStageDecision, map[string]interface{}{
			"trace_id":       fields["trace_id"],
			"tenant_id":      fields["tenant_id"],
			"job_id":         fields["job_id"],
			"fragment_id":    fields["fragment_id"],
			"edge_id":        fields["edge_id"],
			"source_type":    fields["source_type"],
			"target_type":    fields["target_type"],
			"relation_type":  fields["relation_type"],
			"action":         decision.Action,
			"policy_status":  "na",
			"policy_reason":  reason,
			"policy_penalty": penalty,
			"decision_path":  decision.DecisionPath,
			"rule_id":        decision.RuleID,
			"confidence":     decision.Confidence,
			"origin_mode":    fields["origin_mode"],
		})
		if e.telemetry != nil {
			if err := e.telemetry.Record(decision, e.policy.Name()); err != nil {
				LogPolicyEvent(e.policy.Name(), PolicyLogStageError, map[string]interface{}{
					"trace_id":       fields["trace_id"],
					"tenant_id":      fields["tenant_id"],
					"job_id":         fields["job_id"],
					"action":         "telemetry_record",
					"policy_status":  "na",
					"policy_reason":  err.Error(),
					"policy_penalty": 0,
					"decision_path":  decision.DecisionPath,
					"error_class":    "telemetry_record_error",
					"confidence":     decision.Confidence,
				})
			}
		}
		return decision, nil
	}

	// LLM fallback (if executor is available)
	if e.llmExecutor != nil {
		LogPolicyEvent(e.policy.Name(), PolicyLogStageFallback, map[string]interface{}{
			"trace_id":       fields["trace_id"],
			"tenant_id":      fields["tenant_id"],
			"job_id":         fields["job_id"],
			"action":         "llm_fallback",
			"policy_status":  "na",
			"policy_reason":  "",
			"policy_penalty": 0,
			"decision_path":  "llm:fallback",
			"confidence":     decision.Confidence,
		})
		prompt := e.buildLLMPrompt(input)
		llmDecision, tokens, err := e.llmExecutor.Execute(ctx, prompt, input)
		if err == nil {
			llmDecision.DecisionPath = "llm:fallback"
			llmDecision.LatencyMs = int(time.Since(start).Milliseconds())
			llmDecision.LLMTokens = &tokens
			reason := decisionReason(llmDecision, "")
			penalty, _ := llmDecision.Params["penalty"].(float64)
			LogPolicyEvent(e.policy.Name(), PolicyLogStageDecision, map[string]interface{}{
				"trace_id":       fields["trace_id"],
				"tenant_id":      fields["tenant_id"],
				"job_id":         fields["job_id"],
				"fragment_id":    fields["fragment_id"],
				"edge_id":        fields["edge_id"],
				"source_type":    fields["source_type"],
				"target_type":    fields["target_type"],
				"relation_type":  fields["relation_type"],
				"action":         llmDecision.Action,
				"policy_status":  "na",
				"policy_reason":  reason,
				"policy_penalty": penalty,
				"decision_path":  llmDecision.DecisionPath,
				"confidence":     llmDecision.Confidence,
				"origin_mode":    fields["origin_mode"],
			})
			if e.telemetry != nil {
				if err := e.telemetry.Record(llmDecision, e.policy.Name()); err != nil {
					LogPolicyEvent(e.policy.Name(), PolicyLogStageError, map[string]interface{}{
						"trace_id":       fields["trace_id"],
						"tenant_id":      fields["tenant_id"],
						"job_id":         fields["job_id"],
						"action":         "llm_telemetry_record",
						"policy_status":  "na",
						"policy_reason":  err.Error(),
						"policy_penalty": 0,
						"decision_path":  llmDecision.DecisionPath,
						"error_class":    "telemetry_record_error",
						"confidence":     llmDecision.Confidence,
					})
				}
			}
			return llmDecision, nil
		}
		LogPolicyEvent(e.policy.Name(), PolicyLogStageError, map[string]interface{}{
			"trace_id":       fields["trace_id"],
			"tenant_id":      fields["tenant_id"],
			"job_id":         fields["job_id"],
			"action":         "llm_fallback",
			"policy_status":  "na",
			"policy_reason":  err.Error(),
			"policy_penalty": 0,
			"decision_path":  "llm:fallback",
			"error_class":    "llm_fallback_error",
			"confidence":     decision.Confidence,
		})
	}

	// Fallback to default decision if both rule and LLM fail
	decision.LatencyMs = int(time.Since(start).Milliseconds())
	decision.DecisionPath = "rule:default"
	reason := decisionReason(decision, "default_decision")
	penalty, _ := decision.Params["penalty"].(float64)
	LogPolicyEvent(e.policy.Name(), PolicyLogStageResult, map[string]interface{}{
		"trace_id":       fields["trace_id"],
		"tenant_id":      fields["tenant_id"],
		"job_id":         fields["job_id"],
		"fragment_id":    fields["fragment_id"],
		"edge_id":        fields["edge_id"],
		"source_type":    fields["source_type"],
		"target_type":    fields["target_type"],
		"relation_type":  fields["relation_type"],
		"action":         decision.Action,
		"policy_status":  "na",
		"policy_reason":  reason,
		"policy_penalty": penalty,
		"decision_path":  decision.DecisionPath,
		"confidence":     decision.Confidence,
		"origin_mode":    fields["origin_mode"],
	})
	if e.telemetry != nil {
		if err := e.telemetry.Record(decision, e.policy.Name()); err != nil {
			LogPolicyEvent(e.policy.Name(), PolicyLogStageError, map[string]interface{}{
				"trace_id":       fields["trace_id"],
				"tenant_id":      fields["tenant_id"],
				"job_id":         fields["job_id"],
				"action":         "default_telemetry_record",
				"policy_status":  "na",
				"policy_reason":  err.Error(),
				"policy_penalty": 0,
				"decision_path":  decision.DecisionPath,
				"error_class":    "telemetry_record_error",
				"confidence":     decision.Confidence,
			})
		}
	}
	return decision, err
}

// buildLLMPrompt constructs a prompt for LLM fallback (to be overridden by specific policies).
func (e *PolicyExecutor) buildLLMPrompt(_ PolicyInput) string {
	return "You are a policy decision agent for Horizon. Make a decision based on the input context."
}
