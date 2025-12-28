package policy

import (
	"context"
	"fmt"
	"log"
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
	Kind     string                 `json:"kind"`     // "propagation", "scout", "canonization", etc.
	Context  map[string]interface{} `json:"context"` // Arbitrary context for the decision
	Fragment *horizonmodels.HorizonFragment `json:"fragment,omitempty"` // Optional: relevant fragment
	Edge     *horizonmodels.DependencyEdge  `json:"edge,omitempty"`     // Optional: relevant edge
	Fact     *coremodels.OntologyFact        `json:"fact,omitempty"`     // Optional: relevant fact
}

// PolicyDecision represents the result of a policy decision.
type PolicyDecision struct {
	Action      string                 `json:"action"`      // e.g., "propagate", "skip", "normalize_to", "synthesize_signal"
	Params      map[string]interface{} `json:"params"`     // Action-specific parameters

	// Telemetry
	DecisionPath string  `json:"decisionPath"` // "rule:exact_match" | "rule:pattern" | "llm:fallback"
	RuleID       *string `json:"ruleId,omitempty"` // If rule-based: which rule
	Confidence   float64 `json:"confidence"`       // 0..1
	LatencyMs    int     `json:"latencyMs"`      // Decision latency
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
	logPrefix := fmt.Sprintf("[PolicyExecutor:%s]", e.policy.Name())
	start := time.Now()

	log.Printf("%s Execute called: kind=%s", logPrefix, input.Kind)
	if input.Fragment != nil {
		log.Printf("%s Input fragment: id=%s, type=%s, canonicalPath=%s", 
			logPrefix, input.Fragment.ID, input.Fragment.Type, input.Fragment.CanonicalPath)
	}
	if input.Edge != nil {
		log.Printf("%s Input edge: id=%s, sourceID=%s, targetID=%s, type=%s", 
			logPrefix, input.Edge.ID, input.Edge.SourceFragmentID, input.Edge.TargetFragmentID, input.Edge.Type)
	}
	if len(input.Context) > 0 {
		log.Printf("%s Input context: %+v", logPrefix, input.Context)
	}

	// Try deterministic rules first
	decision, err := e.policy.Decide(ctx, input)
	if err != nil {
		log.Printf("%s ERROR: policy.Decide failed: %v", logPrefix, err)
		decision.LatencyMs = int(time.Since(start).Milliseconds())
		decision.DecisionPath = "rule:error"
		if e.telemetry != nil {
			_ = e.telemetry.Record(decision, e.policy.Name())
		}
		return decision, err
	}

	if decision.DecisionPath != "llm:fallback" {
		// Rule-based decision
		decision.LatencyMs = int(time.Since(start).Milliseconds())
		log.Printf("%s Decision: action=%s, path=%s, confidence=%.2f, latency=%dms", 
			logPrefix, decision.Action, decision.DecisionPath, decision.Confidence, decision.LatencyMs)
		if decision.RuleID != nil {
			log.Printf("%s Rule ID: %s", logPrefix, *decision.RuleID)
		}
		if len(decision.Params) > 0 {
			log.Printf("%s Decision params: %+v", logPrefix, decision.Params)
		}
		if e.telemetry != nil {
			if err := e.telemetry.Record(decision, e.policy.Name()); err != nil {
				log.Printf("%s ERROR: failed to record telemetry: %v", logPrefix, err)
			}
		}
		return decision, nil
	}

	// LLM fallback (if executor is available)
	if e.llmExecutor != nil {
		log.Printf("%s Attempting LLM fallback", logPrefix)
		prompt := e.buildLLMPrompt(input)
		llmDecision, tokens, err := e.llmExecutor.Execute(ctx, prompt, input)
		if err == nil {
			llmDecision.DecisionPath = "llm:fallback"
			llmDecision.LatencyMs = int(time.Since(start).Milliseconds())
			llmDecision.LLMTokens = &tokens
			log.Printf("%s LLM decision: action=%s, confidence=%.2f, tokens=%d, latency=%dms", 
				logPrefix, llmDecision.Action, llmDecision.Confidence, tokens, llmDecision.LatencyMs)
			if e.telemetry != nil {
				if err := e.telemetry.Record(llmDecision, e.policy.Name()); err != nil {
					log.Printf("%s ERROR: failed to record LLM telemetry: %v", logPrefix, err)
				}
			}
			return llmDecision, nil
		}
		log.Printf("%s ERROR: LLM fallback failed: %v", logPrefix, err)
	}

	// Fallback to default decision if both rule and LLM fail
	decision.LatencyMs = int(time.Since(start).Milliseconds())
	decision.DecisionPath = "rule:default"
	log.Printf("%s Using default decision: action=%s, confidence=%.2f", 
		logPrefix, decision.Action, decision.Confidence)
	if e.telemetry != nil {
		if err := e.telemetry.Record(decision, e.policy.Name()); err != nil {
			log.Printf("%s ERROR: failed to record default telemetry: %v", logPrefix, err)
		}
	}
	return decision, err
}

// buildLLMPrompt constructs a prompt for LLM fallback (to be overridden by specific policies).
func (e *PolicyExecutor) buildLLMPrompt(input PolicyInput) string {
	return "You are a policy decision agent for Horizon. Make a decision based on the input context."
}

