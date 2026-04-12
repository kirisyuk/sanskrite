package vertex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func (s *Service) ScoutAgentEngine() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.scoutAgentEngine)
}

func (s *Service) RunScoutOrchestrator(ctx context.Context, task *contracts.MeshTask, input map[string]any, memoryContext []MemoryRecord) (map[string]any, *contracts.CostAttribution, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("vertex service is not configured")
	}
	agentEngine := strings.TrimSpace(s.scoutAgentEngine)
	if agentEngine == "" {
		return nil, nil, fmt.Errorf("AGENTMESH_SCOUT_AGENT_ENGINE is required when AGENTMESH_SCOUT_ORCHESTRATOR_MODE=vertex")
	}
	if task == nil {
		return nil, nil, fmt.Errorf("task is nil")
	}
	taskInput := cloneMap(input)
	taskInput["memoryContext"] = memoryContext
	taskInput["memoryPolicy"] = map[string]any{
		"use":                    "hint_only",
		"notEvidence":            true,
		"requiresFreshGrounding": true,
		"rule":                   "Do not materialize graph facts from memory unless fresh grounded evidence supports the fact.",
	}
	taskInput["sessionRef"] = strings.TrimSpace(task.SessionRef)
	taskInput["memoryBankRef"] = strings.TrimSpace(task.MemoryBankRef)
	scoutSessionID := sanitizeResourceID(firstNonEmpty(task.ID, task.SessionRef))
	if scoutSessionID == "" {
		return nil, nil, fmt.Errorf("scout session id is required")
	}
	if err := s.ensureScoutAgentSession(ctx, agentEngine, firstNonEmpty(task.TenantID, "global"), scoutSessionID); err != nil {
		return nil, nil, err
	}

	turnBudget := scoutTurnBudget(taskInput)
	currentInput := cloneMap(taskInput)
	var totalCost *contracts.CostAttribution
	var lastOutput map[string]any
	for turn := 1; turn <= turnBudget; turn++ {
		turnInput := cloneMap(currentInput)
		runtimePolicy := cloneMap(mapFromAny(turnInput["runtimePolicy"]))
		runtimePolicy["turnAttempt"] = turn
		runtimePolicy["turnBudget"] = turnBudget
		turnInput["runtimePolicy"] = runtimePolicy

		output, cost, err := s.runScoutTurn(ctx, agentEngine, task, scoutSessionID, turnInput, memoryContext, turn > 1)
		totalCost = mergeCosts(totalCost, cost)
		if err != nil {
			return nil, totalCost, err
		}
		lastOutput = output
		if isScoutTerminalOutput(output) {
			return output, totalCost, nil
		}
		currentInput = buildScoutContinuationInput(taskInput, output, turn+1, turnBudget)
	}
	if len(lastOutput) == 0 {
		return nil, totalCost, fmt.Errorf("scout orchestrator did not return a snapshot within %d turns", turnBudget)
	}
	degraded := degradeScoutOutputForBudget(lastOutput, task, turnBudget)
	return degraded, totalCost, nil
}

func (s *Service) runScoutTurn(ctx context.Context, agentEngine string, task *contracts.MeshTask, scoutSessionID string, taskInput map[string]any, memoryContext []MemoryRecord, continuation bool) (map[string]any, *contracts.CostAttribution, error) {
	payload := buildScoutQueryPayload(task, scoutSessionID, taskInput, continuation)
	response, err := s.doVertexSSE(ctx, http.MethodPost, resourceURL(agentEngine)+":streamQuery?alt=sse", payload)
	if err == nil {
		output := extractAgentEngineStreamOutput(response)
		ensureScoutOutputEnvelope(output, task, memoryContext)
		validateErr := validateScoutEnvelope(output)
		if validateErr == nil {
			return output, costFromAgentEngineStreamEvents(response), nil
		}
		err = fmt.Errorf("invalid scout envelope: %w", validateErr)
	}
	repairInput := cloneMap(taskInput)
	runtimePolicy := cloneMap(mapFromAny(repairInput["runtimePolicy"]))
	runtimePolicy["escalationRequested"] = true
	runtimePolicy["escalationReason"] = err.Error()
	runtimePolicy["forceModel"] = "pro"
	runtimePolicy["fallbackMode"] = "vertex_upward_only"
	runtimePolicy["repairAttempt"] = 2
	repairInput["runtimePolicy"] = runtimePolicy
	repairPayload := buildScoutQueryPayload(task, scoutSessionID, repairInput, continuation)
	repairResponse, repairErr := s.doVertexSSE(ctx, http.MethodPost, resourceURL(agentEngine)+":streamQuery?alt=sse", repairPayload)
	if repairErr != nil {
		return nil, mergeCosts(costFromAgentEngineStreamEvents(response), costFromAgentEngineStreamEvents(repairResponse)), fmt.Errorf("scout orchestrator failed initial attempt (%v) and repair attempt (%w)", err, repairErr)
	}
	output := extractAgentEngineStreamOutput(repairResponse)
	ensureScoutOutputEnvelope(output, task, memoryContext)
	if validateErr := validateScoutEnvelope(output); validateErr != nil {
		return nil, mergeCosts(costFromAgentEngineStreamEvents(response), costFromAgentEngineStreamEvents(repairResponse)), fmt.Errorf("scout orchestrator failed after upward repair attempt: %w", validateErr)
	}
	return output, mergeCosts(costFromAgentEngineStreamEvents(response), costFromAgentEngineStreamEvents(repairResponse)), nil
}

func buildScoutQueryPayload(task *contracts.MeshTask, scoutSessionID string, taskInput map[string]any, continuation bool) map[string]any {
	promptBytes, _ := json.Marshal(taskInput)
	message := "Run the AgentMesh Scout orchestrator and return exactly one ScoutTaskSnapshot JSON object with canonical state.status and action.\n\n"
	if continuation {
		message = "Continue the current AgentMesh Scout orchestrator session and advance the task from the existing state/action. Return exactly one ScoutTaskSnapshot JSON object with canonical state.status and action.\n\n"
	}
	return map[string]any{
		"classMethod": "async_stream_query",
		"input": map[string]any{
			"user_id":    firstNonEmpty(task.TenantID, "global"),
			"session_id": scoutSessionID,
			"message":    message + string(promptBytes),
		},
	}
}

func scoutTurnBudget(taskInput map[string]any) int {
	if budget := intFromAny(mapFromAny(taskInput["runtimePolicy"])["turnBudget"]); budget > 0 {
		return budget
	}
	return 12
}

func isScoutTerminalOutput(output map[string]any) bool {
	action := mapFromAny(output["action"])
	if terminal, ok := action["terminal"].(bool); ok {
		return terminal
	}
	switch stringFromAny(mapFromAny(output["state"])["status"]) {
	case "awaiting_review", "completed", "degraded", "failed":
		return true
	default:
		return false
	}
}

func buildScoutContinuationInput(baseInput, snapshot map[string]any, nextTurn, turnBudget int) map[string]any {
	next := cloneMap(baseInput)
	runtimePolicy := cloneMap(mapFromAny(next["runtimePolicy"]))
	runtimePolicy["continuationRequested"] = true
	runtimePolicy["continuationAttempt"] = nextTurn
	runtimePolicy["turnBudget"] = turnBudget
	runtimePolicy["previousActionType"] = stringFromAny(mapFromAny(snapshot["action"])["type"])
	runtimePolicy["previousActionReason"] = stringFromAny(mapFromAny(snapshot["action"])["reason"])
	runtimePolicy["previousStateStatus"] = stringFromAny(mapFromAny(snapshot["state"])["status"])
	next["runtimePolicy"] = runtimePolicy
	if summary := stringFromAny(snapshot["reasoningSummary"]); summary != "" {
		next["previousReasoningSummary"] = summary
	}
	if taskRefs := mapFromAny(snapshot["taskRefs"]); len(taskRefs) > 0 {
		next["previousTaskRefs"] = cloneMap(taskRefs)
	}
	return next
}

func degradeScoutOutputForBudget(output map[string]any, task *contracts.MeshTask, turnBudget int) map[string]any {
	degraded := cloneMap(output)
	ensureScoutOutputEnvelope(degraded, task, nil)
	state := cloneMap(mapFromAny(degraded["state"]))
	state["status"] = "degraded"
	state["stopReason"] = firstNonEmpty(stringFromAny(state["stopReason"]), fmt.Sprintf("scout_turn_budget_exhausted_after_%d_turns", turnBudget))
	degraded["state"] = state
	action := cloneMap(mapFromAny(degraded["action"]))
	action["type"] = "degrade"
	action["terminal"] = true
	action["reason"] = fmt.Sprintf("scout orchestrator did not reach a terminal state within %d turns", turnBudget)
	degraded["action"] = action
	runtimeDiagnosis, _ := degraded["runtimeDiagnosis"].([]any)
	degraded["runtimeDiagnosis"] = append(runtimeDiagnosis, map[string]any{
		"code":     "scout_turn_budget_exhausted",
		"severity": "warning",
		"message":  fmt.Sprintf("Scout orchestrator returned only non-terminal snapshots for %d turns", turnBudget),
	})
	degraded["reviewAccepted"] = false
	return degraded
}

func extractAgentEngineOutput(response map[string]any) map[string]any {
	for _, key := range []string{"scout_snapshot", "scout_envelope"} {
		if snapshot, ok := response[key].(map[string]any); ok && len(snapshot) > 0 {
			return snapshot
		}
	}
	if actions, ok := response["actions"].(map[string]any); ok {
		stateDelta := mapFromAny(firstNonNil(actions["state_delta"], actions["stateDelta"]))
		if len(stateDelta) > 0 {
			for _, key := range []string{"scout_snapshot", "scout_envelope"} {
				if snapshot, ok := stateDelta[key].(map[string]any); ok && len(snapshot) > 0 {
					return snapshot
				}
			}
		}
	}
	for _, key := range []string{"output", "result", "response"} {
		if value, ok := response[key].(map[string]any); ok && len(value) > 0 {
			if nested := extractAgentEngineOutput(value); len(nested) > 0 {
				return nested
			}
		}
	}
	if candidates, ok := response["candidates"].([]any); ok {
		for _, candidate := range candidates {
			if candidateMap, ok := candidate.(map[string]any); ok {
				if parsed := extractAgentEngineOutput(candidateMap); len(parsed) > 0 {
					return parsed
				}
			}
		}
	}
	if looksLikeScoutEnvelope(response) {
		return response
	}
	return nil
}

func ensureScoutOutputEnvelope(output map[string]any, task *contracts.MeshTask, memoryContext []MemoryRecord) {
	if output == nil || task == nil {
		return
	}
	ensureMapKey(output, "plan")
	ensureMapKey(output, "state")
	ensureMapKey(output, "action")
	ensureMapKey(output, "review")
	ensureMapKey(output, "taskRefs")
	ensureArrayKey(output, "reasoningTrace")
	ensureArrayKey(output, "runtimeDiagnosis")
	ensureArrayKey(output, "evidenceLedger")
	ensureArrayKey(output, "uncertaintyLedger")
	ensureArrayKey(output, "decisionLedger")
	output["sessionRef"] = strings.TrimSpace(task.SessionRef)
	output["memoryBankRef"] = strings.TrimSpace(task.MemoryBankRef)
	if _, ok := output["reasoningSummary"].(string); !ok {
		output["reasoningSummary"] = ""
	}
	if len(memoryContext) > 0 {
		output["memoryContext"] = memoryContext
	}
	normalizeScoutLifecycle(output)
	if _, ok := output["reviewAccepted"].(bool); !ok {
		output["reviewAccepted"] = inferScoutReviewAccepted(output)
	}
}

func (s *Service) ensureScoutAgentSession(ctx context.Context, agentEngine, userID, sessionID string) error {
	if s == nil {
		return fmt.Errorf("vertex service is not configured")
	}
	sessionURL := resourceURL(agentEngine) + "/sessions/" + sessionID
	if _, err := s.doVertexJSON(ctx, http.MethodGet, sessionURL, nil); err == nil {
		return nil
	}
	resp, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(agentEngine)+"/sessions?sessionId="+sessionID, map[string]any{
		"userId": userID,
	})
	if err != nil {
		if _, getErr := s.doVertexJSON(ctx, http.MethodGet, sessionURL, nil); getErr == nil {
			return nil
		}
		return fmt.Errorf("create scout agent session: %w", err)
	}
	if done, ok := resp["done"].(bool); ok && !done {
		if _, waitErr := s.waitForOperation(ctx, resp); waitErr != nil {
			return fmt.Errorf("wait scout agent session: %w", waitErr)
		}
	}
	return nil
}

func extractAgentEngineStreamOutput(events []any) map[string]any {
	if len(events) == 0 {
		return nil
	}
	for _, event := range events {
		if output := extractAgentEngineOutputFromAny(event); len(output) > 0 {
			return output
		}
	}
	var fragments []string
	for _, event := range events {
		collectTextFragments(event, &fragments)
	}
	if len(fragments) == 0 {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.Join(fragments, ""))), &parsed); err == nil {
		if output := extractAgentEngineOutputFromAny(parsed); len(output) > 0 {
			return output
		}
	}
	return nil
}

func extractAgentEngineOutputFromAny(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		if output := extractAgentEngineOutput(v); len(output) > 0 {
			return output
		}
		for _, nested := range v {
			if output := extractAgentEngineOutputFromAny(nested); len(output) > 0 {
				return output
			}
		}
	case []any:
		for _, item := range v {
			if output := extractAgentEngineOutputFromAny(item); len(output) > 0 {
				return output
			}
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if !strings.HasPrefix(trimmed, "{") {
			return nil
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return extractAgentEngineOutputFromAny(parsed)
		}
	}
	return nil
}

func collectTextFragments(value any, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if strings.EqualFold(key, "text") {
				if str, ok := nested.(string); ok && strings.TrimSpace(str) != "" {
					*out = append(*out, str)
				}
				continue
			}
			collectTextFragments(nested, out)
		}
	case []any:
		for _, item := range v {
			collectTextFragments(item, out)
		}
	}
}

func costFromAgentEngineStreamEvents(events []any) *contracts.CostAttribution {
	for _, event := range events {
		if usage := findUsageMap(event); len(usage) > 0 {
			return &contracts.CostAttribution{
				Model:            firstNonEmpty(stringFromAny(usage["model"]), stringFromAny(usage["model_name"])),
				PromptTokens:     intFromAny(firstNonNil(usage["promptTokens"], usage["prompt_token_count"])),
				CompletionTokens: intFromAny(firstNonNil(usage["completionTokens"], usage["candidatesTokenCount"], usage["completion_token_count"])),
				EstimatedUSD:     floatFromAny(firstNonNil(usage["estimatedUsd"], usage["estimated_usd"])),
			}
		}
	}
	return nil
}

func findUsageMap(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"usage", "usageMetadata"} {
			if usage, ok := v[key].(map[string]any); ok && len(usage) > 0 {
				return usage
			}
		}
		for _, nested := range v {
			if usage := findUsageMap(nested); len(usage) > 0 {
				return usage
			}
		}
	case []any:
		for _, item := range v {
			if usage := findUsageMap(item); len(usage) > 0 {
				return usage
			}
		}
	}
	return nil
}

func mergeCosts(left, right *contracts.CostAttribution) *contracts.CostAttribution {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	default:
		return &contracts.CostAttribution{
			Model:            firstNonEmpty(right.Model, left.Model),
			PromptTokens:     left.PromptTokens + right.PromptTokens,
			CompletionTokens: left.CompletionTokens + right.CompletionTokens,
			EstimatedUSD:     left.EstimatedUSD + right.EstimatedUSD,
		}
	}
}

func looksLikeScoutEnvelope(candidate map[string]any) bool {
	if len(candidate) == 0 {
		return false
	}
	_, hasPlan := candidate["plan"].(map[string]any)
	_, hasState := candidate["state"].(map[string]any)
	_, hasAction := candidate["action"].(map[string]any)
	return hasPlan && hasState && hasAction
}

func validateScoutEnvelope(output map[string]any) error {
	if len(output) == 0 {
		return fmt.Errorf("scout_snapshot is empty")
	}
	normalizeScoutLifecycle(output)
	for _, key := range []string{"plan", "state", "action", "taskRefs"} {
		if _, ok := output[key].(map[string]any); !ok {
			return fmt.Errorf("%s must be an object", key)
		}
	}
	for _, key := range []string{"reasoningTrace", "runtimeDiagnosis", "evidenceLedger", "uncertaintyLedger", "decisionLedger"} {
		if _, ok := output[key].([]any); !ok {
			return fmt.Errorf("%s must be an array", key)
		}
	}
	state := mapFromAny(output["state"])
	status := stringFromAny(state["status"])
	if !isAllowedScoutStateStatus(status) {
		return fmt.Errorf("state.status must be one of running|blocked|awaiting_review|completed|degraded|failed")
	}
	action := mapFromAny(output["action"])
	actionType := stringFromAny(action["type"])
	if !isAllowedScoutActionType(actionType) {
		return fmt.Errorf("action.type must be one of continue|spawn|request_review|complete|degrade|fail|stop|escalate")
	}
	if _, ok := action["terminal"].(bool); !ok {
		return fmt.Errorf("action.terminal must be a boolean")
	}
	if !isValidScoutLifecycle(status, actionType, action["terminal"].(bool)) {
		return fmt.Errorf("state.status=%q is incompatible with action.type=%q", status, actionType)
	}
	review := mapFromAny(output["review"])
	if len(review) > 0 {
		if accepted, ok := review["accepted"]; ok {
			if _, ok := accepted.(bool); !ok {
				return fmt.Errorf("review.accepted must be a boolean when present")
			}
		}
		if rationale, ok := review["rationale"]; ok {
			if _, ok := rationale.(string); !ok {
				return fmt.Errorf("review.rationale must be a string when present")
			}
		}
	}
	if err := validateReasoningTrace(output["reasoningTrace"].([]any)); err != nil {
		return err
	}
	return nil
}

func normalizeScoutLifecycle(output map[string]any) {
	if len(output) == 0 {
		return
	}
	state := cloneMap(mapFromAny(output["state"]))
	action := cloneMap(mapFromAny(output["action"]))
	if len(state) == 0 || len(action) == 0 {
		return
	}
	if normalized := normalizeScoutStateStatus(stringFromAny(state["status"])); normalized != "" {
		state["status"] = normalized
		output["state"] = state
	}
	if normalized := normalizeScoutActionType(stringFromAny(action["type"])); normalized != "" {
		action["type"] = normalized
		output["action"] = action
	}
	if _, ok := action["terminal"].(bool); !ok {
		action["terminal"] = inferScoutTerminalFlag(stringFromAny(state["status"]), stringFromAny(action["type"]))
		output["action"] = action
	}
}

func normalizeScoutStateStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "in_progress", "ongoing", "active", "working", "processing", "researching":
		return "running"
	case "blocked", "waiting", "paused", "stalled", "needs_escalation", "escalation_required":
		return "blocked"
	case "awaiting_review", "needs_review", "requires_review", "under_review", "ready_for_review", "awaiting_human_review":
		return "awaiting_review"
	case "completed", "complete", "done", "finished", "success", "succeeded":
		return "completed"
	case "degraded", "partial", "partial_success", "incomplete", "best_effort", "degraded_result":
		return "degraded"
	case "failed", "failure", "error", "errored", "aborted":
		return "failed"
	default:
		return strings.TrimSpace(status)
	}
}

func normalizeScoutActionType(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "continue", "resume", "proceed", "research", "explore":
		return "continue"
	case "spawn", "delegate", "dispatch", "branch", "fan_out":
		return "spawn"
	case "request_review", "review", "await_review", "needs_review", "approve":
		return "request_review"
	case "complete", "finish", "done", "return":
		return "complete"
	case "degrade", "partial", "best_effort", "fallback":
		return "degrade"
	case "fail", "error", "abort":
		return "fail"
	case "stop", "halt", "end":
		return "stop"
	case "escalate", "raise", "upgrade", "handoff", "needs_escalation":
		return "escalate"
	default:
		return strings.TrimSpace(action)
	}
}

func inferScoutTerminalFlag(status, action string) bool {
	normalizedStatus := normalizeScoutStateStatus(status)
	normalizedAction := normalizeScoutActionType(action)
	return isValidScoutLifecycle(normalizedStatus, normalizedAction, true)
}

func ensureMapKey(out map[string]any, key string) {
	if _, ok := out[key].(map[string]any); !ok {
		out[key] = map[string]any{}
	}
}

func ensureArrayKey(out map[string]any, key string) {
	if _, ok := out[key].([]any); !ok {
		out[key] = []any{}
	}
}

func inferScoutReviewAccepted(output map[string]any) bool {
	review := mapFromAny(output["review"])
	if accepted, ok := review["accepted"].(bool); ok {
		return accepted
	}
	state := mapFromAny(output["state"])
	action := mapFromAny(output["action"])
	return stringFromAny(state["status"]) == "completed" && stringFromAny(action["type"]) == "complete"
}

func isAllowedScoutStateStatus(status string) bool {
	switch status {
	case "running", "blocked", "awaiting_review", "completed", "degraded", "failed":
		return true
	default:
		return false
	}
}

func isAllowedScoutActionType(action string) bool {
	switch action {
	case "continue", "spawn", "request_review", "complete", "degrade", "fail", "stop", "escalate":
		return true
	default:
		return false
	}
}

func isValidScoutLifecycle(status, action string, terminal bool) bool {
	switch status {
	case "running":
		return !terminal && (action == "continue" || action == "spawn")
	case "blocked":
		return !terminal && (action == "escalate" || action == "continue" || action == "spawn")
	case "awaiting_review":
		return terminal && action == "request_review"
	case "completed":
		return terminal && (action == "complete" || action == "stop")
	case "degraded":
		return terminal && (action == "degrade" || action == "escalate" || action == "stop")
	case "failed":
		return terminal && action == "fail"
	default:
		return false
	}
}

func validateReasoningTrace(items []any) error {
	for idx, item := range items {
		step, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("reasoningTrace[%d] must be an object", idx)
		}
		for _, key := range []string{"phase", "branchId", "decision", "why", "nextAction"} {
			if _, ok := step[key].(string); !ok {
				return fmt.Errorf("reasoningTrace[%d].%s must be a string", idx, key)
			}
		}
		for _, key := range []string{"evidenceRefs", "uncertaintyRefs"} {
			if _, ok := step[key].([]any); !ok {
				return fmt.Errorf("reasoningTrace[%d].%s must be an array", idx, key)
			}
		}
		switch step["confidence"].(type) {
		case float64, int:
		default:
			return fmt.Errorf("reasoningTrace[%d].confidence must be numeric", idx)
		}
	}
	return nil
}
