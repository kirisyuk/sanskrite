package vertex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const vertexAPIBaseURL = "https://aiplatform.googleapis.com/v1beta1"

type ModelAliases struct {
	FlashLite      string
	FlashReasoning string
	Pro            string
}

type Service struct {
	vertexAI         *ai.VertexAIService
	aiExecutor       *ai.AITaskExecutor
	models           ModelAliases
	httpClient       *http.Client
	tokenSource      oauth2.TokenSource
	projectID        string
	location         string
	reasoningEngine  string
	scoutAgentEngine string
}

func NewService(projectID, location, dataStoreID string) (*Service, error) {
	mm := ai.NewAIModelManager()
	vertexSvc, err := ai.NewVertexAIService(projectID, location, mm)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(dataStoreID) != "" {
		vertexSvc = vertexSvc.WithDataStore(dataStoreID)
	}
	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}
	engine := strings.TrimSpace(os.Getenv("AGENTMESH_VERTEX_REASONING_ENGINE"))
	if engine == "" {
		return nil, fmt.Errorf("AGENTMESH_VERTEX_REASONING_ENGINE is required")
	}
	svc := &Service{
		vertexAI: vertexSvc,
		models: ModelAliases{
			FlashLite:      defaultModel("AGENTMESH_MODEL_FLASH", "vertex-gemini-3.1-flash-lite-preview"),
			FlashReasoning: defaultModel("AGENTMESH_MODEL_FLASH_REASONING", "vertex-gemini-3-flash-preview"),
			Pro:            defaultModel("AGENTMESH_MODEL_PRO", "vertex-gemini-3.1-pro-preview"),
		},
		httpClient:       &http.Client{Timeout: 45 * time.Second},
		tokenSource:      ts,
		projectID:        strings.TrimSpace(projectID),
		location:         strings.TrimSpace(location),
		reasoningEngine:  engine,
		scoutAgentEngine: strings.TrimSpace(os.Getenv("AGENTMESH_SCOUT_AGENT_ENGINE")),
	}
	if err := validateVertexModelAliases(mm, svc.models); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) WithAIExecutor(exec *ai.AITaskExecutor) *Service {
	s.aiExecutor = exec
	return s
}

func (s *Service) AIService() ai.AIService {
	return s.vertexAI
}

func (s *Service) ModelAlias(review bool) string {
	if review {
		return "pro"
	}
	return "flash-lite"
}

func (s *Service) ModelName(review bool) string {
	if review {
		return s.models.Pro
	}
	return s.models.FlashLite
}

func (s *Service) ModelNameForCapability(capability string, review bool) string {
	if review {
		return s.models.Pro
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.review":
		return s.models.Pro
	case "scout.research", "scout.bridge":
		return s.models.FlashReasoning
	case "scout.plan", "scout.orchestrate", "scout.decide", "scout.critic":
		return s.models.FlashReasoning
	default:
		return s.models.FlashLite
	}
}

func (s *Service) FallbackModelsForCapability(capability string, review bool) []string {
	if review || strings.EqualFold(strings.TrimSpace(capability), "scout.review") {
		return nil
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.plan", "scout.orchestrate", "scout.decide", "scout.research", "scout.bridge", "scout.critic":
		return compactModelFallbacks(s.models.FlashReasoning, s.models.Pro)
	default:
		return compactModelFallbacks(s.models.FlashLite, s.models.FlashReasoning, s.models.Pro)
	}
}

func (s *Service) Readiness(ctx context.Context) error {
	if s == nil || s.vertexAI == nil {
		return fmt.Errorf("vertex service is not configured")
	}
	if strings.TrimSpace(s.reasoningEngine) == "" {
		return fmt.Errorf("reasoning engine is not configured")
	}
	if _, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(s.reasoningEngine), nil); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTMESH_SCOUT_ORCHESTRATOR_MODE")), "local") {
		if strings.TrimSpace(s.scoutAgentEngine) == "" {
			return fmt.Errorf("scout agent engine is not configured")
		}
		if _, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(s.scoutAgentEngine), nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) EnsureSession(ctx context.Context, task *contracts.MeshTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(task.SessionRef) != "" {
		return task.SessionRef, nil
	}
	sessionID := sanitizeResourceID(task.ID)
	if sessionID == "" {
		sessionID = sanitizeResourceID(uuid.NewString())
	}
	resp, err := s.doVertexJSON(ctx, http.MethodPost, fmt.Sprintf("%s/sessions?sessionId=%s", resourceURL(s.reasoningEngine), url.QueryEscape(sessionID)), map[string]any{
		"displayName": fmt.Sprintf("AgentMesh %s", task.Capability),
		"userId":      task.TenantID,
		"ttl":         envOr("AGENTMESH_VERTEX_SESSION_TTL", "86400s"),
		"labels": map[string]string{
			"agentmesh": "true",
			"tenant":    sanitizeLabelValue(task.TenantID),
			"task":      sanitizeLabelValue(task.ID),
		},
	})
	if err != nil {
		return "", err
	}
	lro, err := s.waitForOperation(ctx, resp)
	if err != nil {
		return "", err
	}
	if session, ok := nestedMap(lro, "response"); ok {
		if name, _ := session["name"].(string); strings.TrimSpace(name) != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("vertex sessions.create did not return a session name")
}

func (s *Service) AppendSessionEvent(ctx context.Context, sessionRef, author, role, text string, metadata map[string]any) error {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return fmt.Errorf("sessionRef is required")
	}
	_, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(sessionRef)+":appendEvent", map[string]any{
		"author":       strings.TrimSpace(author),
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
		"invocationId": uuid.NewString(),
		"content": map[string]any{
			"role": strings.TrimSpace(role),
			"parts": []map[string]any{
				{"text": text},
			},
		},
		"eventMetadata": metadata,
	})
	return err
}

func (s *Service) BeginChatWithTools(ctx context.Context, task *contracts.MeshTask, tools []ai.ToolSchema) (*ai.ToolChatStep, error) {
	if s == nil || s.vertexAI == nil {
		return nil, fmt.Errorf("vertex service is not configured")
	}
	inputJSON, _ := json.MarshalIndent(task.Input, "", "  ")
	model := s.ModelNameForCapability(task.Capability, false)
	aiTask := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: buildPrompt(task, false, string(inputJSON)),
		AIRequirements: ai.AIRequirements{
			PreferredModel:            model,
			TextGenerationCapability:  true,
			MaxOutputSize:             capabilityMaxOutput(task.Capability, false),
			MinCapabilityScore:        capabilityMinScore(task.Capability, false),
			SearchGroundingCapability: false,
		},
		TenantID:  task.TenantID,
		SessionID: task.SessionRef,
		TraceID:   task.ID,
		UseCase:   "agentmesh",
	}
	return s.vertexAI.BeginChatWithTools(ctx, aiTask, model, tools)
}

func (s *Service) ContinueWithToolResults(ctx context.Context, sessionID string, task *contracts.MeshTask, results []ai.ToolExecutionResult) (*ai.ToolChatStep, error) {
	if s == nil || s.vertexAI == nil {
		return nil, fmt.Errorf("vertex service is not configured")
	}
	model := s.ModelNameForCapability(task.Capability, false)
	return s.vertexAI.ContinueWithToolResults(ctx, sessionID, model, results)
}

func (s *Service) ExecuteTask(ctx context.Context, task *contracts.MeshTask, review bool) (map[string]any, *contracts.CostAttribution, error) {
	if s == nil || s.vertexAI == nil {
		return nil, nil, fmt.Errorf("vertex service is not configured")
	}
	inputJSON, _ := json.MarshalIndent(task.Input, "", "  ")
	taskType := ai.TaskTypeContentGeneration
	if review {
		taskType = ai.TaskTypeStrategicValidation
	}
	searchGrounding, internalGrounding := capabilityGrounding(task.Capability, review)
	minCapabilityScore := capabilityMinScore(task.Capability, review)
	maxOutputSize := capabilityMaxOutput(task.Capability, review)
	responseSchema := responseSchemaForCapability(task.Capability, review)
	responseMimeType := ""
	if len(responseSchema) > 0 {
		responseMimeType = "application/json"
	}
	aiTask := &ai.AITask{
		TaskType:     taskType,
		InputContent: buildPrompt(task, review, string(inputJSON)),
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  s.ModelNameForCapability(task.Capability, review),
			FallbackModels:                  s.FallbackModelsForCapability(task.Capability, review),
			ResponseMimeType:                responseMimeType,
			ResponseSchema:                  responseSchema,
			TextGenerationCapability:        true,
			SearchGroundingCapability:       searchGrounding,
			InternalDataGroundingCapability: internalGrounding,
			MaxOutputSize:                   maxOutputSize,
			MinCapabilityScore:              minCapabilityScore,
		},
		TenantID:  task.TenantID,
		SessionID: task.SessionRef,
		TraceID:   task.ID,
		UseCase:   "agentmesh",
	}

	if s.aiExecutor != nil {
		return s.executeViaTaskExecutor(ctx, aiTask, review)
	}
	return s.executeDirectVertex(ctx, aiTask, review)
}

func (s *Service) executeViaTaskExecutor(ctx context.Context, aiTask *ai.AITask, review bool) (map[string]any, *contracts.CostAttribution, error) {
	result, err := s.aiExecutor.ExecuteTask(ctx, aiTask)
	if err != nil {
		return nil, nil, err
	}
	if result.Status != ai.AITaskStatusCompleted {
		return nil, nil, fmt.Errorf("AI task executor: status=%s content=%s", result.Status, result.Content)
	}
	body := map[string]any{
		"content":      result.Content,
		"model":        result.Model,
		"citations":    result.Citations,
		"grounding":    result.GroundingEvidence,
		"finishReason": result.FinishReason,
		"reviewMode":   review,
	}
	cost := &contracts.CostAttribution{
		Model:            result.Model,
		PromptTokens:     result.InputTokens,
		CompletionTokens: result.OutputTokens,
		EstimatedUSD:     result.ProcessingCost,
	}
	return body, cost, nil
}

func (s *Service) executeDirectVertex(ctx context.Context, aiTask *ai.AITask, review bool) (map[string]any, *contracts.CostAttribution, error) {
	modelName := aiTask.AIRequirements.PreferredModel
	result, err := s.vertexAI.ExecuteTask(ctx, aiTask, modelName)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{
		"content":      result.Content,
		"model":        result.Model,
		"citations":    result.Citations,
		"grounding":    result.GroundingEvidence,
		"finishReason": result.FinishReason,
		"reviewMode":   review,
	}
	cost := &contracts.CostAttribution{
		Model:            result.Model,
		PromptTokens:     result.InputTokens,
		CompletionTokens: result.OutputTokens,
		EstimatedUSD:     result.ProcessingCost,
	}
	return body, cost, nil
}

func capabilityGrounding(capability string, review bool) (bool, bool) {
	if review {
		return false, false
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.research", "scout.bridge":
		return true, true
	case "scout.plan", "scout.decide", "scout.critic", "scout.aggregate", "scout.review":
		return false, false
	default:
		return true, true
	}
}

func capabilityMinScore(capability string, review bool) int {
	if review {
		return 9
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.plan", "scout.orchestrate", "scout.decide", "scout.research", "scout.bridge", "scout.critic":
		return 8
	case "scout.path", "scout.materialize", "scout.aggregate":
		return 7
	default:
		return 7
	}
}

func capabilityMaxOutput(capability string, review bool) int {
	if review {
		return 8192
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.bridge":
		return 16384
	case "scout.research", "scout.critic", "scout.orchestrate", "scout.decide":
		return 12288
	default:
		return 8192
	}
}

func buildPrompt(task *contracts.MeshTask, review bool, inputJSON string) string {
	if !review {
		switch strings.TrimSpace(task.Capability) {
		case "scout.plan":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only. Build or refine an investigation plan.\n" +
				"Keys: goal, missionRole, focusProducts, focusGeos, focusActors, wantsPathMaterialize, maxResearchRounds, planningRationale, existingGraphPriorities, planVersion, materializationStrategy, branchDependencies, branches.\n" +
				"You MUST determine the professional role from the task itself — do not use a fixed catalog. missionRole must contain domain, primaryRole, supportingRoles, professionalQualityBar, investigationPosture, roleRationale.\n" +
				"Each branch is a concrete question you would ask a human analyst. Write it in natural language, e.g. 'Who are the main lithium carbonate exporters in Chile and how does their product reach European converters?' — NOT 'what sits between /product/x and /product/y'.\n" +
				"Each branch must contain id, question, role, productHints, geoHints, actorHints, requiresPath, dependsOn, broadScan, testCriteria, expectedInfoGain.\n" +
				"Include at least one broad-scan branch when completeness says broadScanRequired=true.\n" +
				"When investigationState or replanReason is present, reshape the plan (add/split/kill branches, update strategy) rather than repeating the prior plan.\n\nInput:\n" + inputJSON
		case "scout.orchestrate":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys stateSummary, chosenAction, branchId, whyChosen, evidenceUsed, stopCriteriaEvaluation, expectedInformationGain, confidence.\n" +
				"You are the orchestrator. sessionState is your authoritative memory.\n" +
				"Choose exactly one next action: research|bridge|critic|path|materialize|replan|stop.\n" +
				"CRITICAL decision rules:\n" +
				"- If the last 2+ actions produced no new information (qualityDelta=none), do NOT repeat the same kind of action on the same branch. Either switch strategy or stop.\n" +
				"- If internal search returned 0 hits, the next research on the same branch should use web search (grounding), not the same internal query.\n" +
				"- Stop when further work is unlikely to add useful structure to the graph. A partial but honest result is better than an expensive loop.\n" +
				"- Explain your choice in plain language: why this action, why not the alternatives.\n\nInput:\n" + inputJSON
		case "scout.decide":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys stateSummary, chosenAction, branchId, whyChosen, whyNotStop, whyNotPath, whyNotBridge, whyNotResearch, whyNotCritic, whyNotMaterialize, whyNotReplan, evidenceUsed, stopCriteriaEvaluation, expectedInformationGain, confidence.\n" +
				"You are the decision-maker. Choose the single best next action from candidateActions (where available=true). If none available, choose stop.\n" +
				"chosenAction must be one of: research, bridge, path, critic, materialize, replan, stop.\n\n" +
				"HARD RULES — violating these wastes time and money:\n" +
				"1. INFORMATION GAIN CHECK: Look at recentOutcomes. If the last 2+ actions had qualityDelta=none or the same gap persists, you MUST NOT repeat the same action on the same branch. Either try a fundamentally different approach (different branch, different action type, web search instead of internal) or stop.\n" +
				"2. ZERO-HIT AWARENESS: If the latest research outcome says retrievalHitCount=0, repeating research with similar wording will produce the same nothing. Either reformulate radically, switch to bridge/path, or stop.\n" +
				"3. STOP IS A VALID ANSWER: A partial honest result is better than an expensive loop. If you have some useful graph structure but further queries keep returning empty, stop and let the system report what it found.\n" +
				"4. THINK LIKE A HUMAN: Explain in plain language why you chose this action and why the alternatives are worse right now.\n\n" +
				"Use latestOutcome/recentOutcomes as ground truth about what previous steps actually produced.\n" +
				"stopCriteriaEvaluation is REQUIRED: array of {criterion, satisfied, why}.\n" +
				"evidenceUsed should cite concrete state elements that drove the decision.\n\nInput:\n" + inputJSON
		case "scout.research":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys summary, evidencePoints, canonicalRefs, productRefs, geoRefs, actorRefs, recommendedPathProducts, openQuestions, confidence, proposedGraphOps, gapDiagnosis, recommendedAction, dataSourceSignal.\n\n" +
				"You are researching a specific question about the supply-chain graph. Your job is to produce DOMAIN THESES — explicit claims about real-world entities and their relationships — not to paraphrase the query.\n\n" +
				"TOOLS:\n" +
				"You have 4 tools. Use them iteratively like an analyst:\n" +
				"1. search_knowledge — check internal knowledge base first. Short, specific queries.\n" +
				"2. web_search — search the web when internal KB has no data. Targeted queries, not full question restatements.\n" +
				"3. resolve_product — resolve product names to canonical paths BEFORE proposing graph operations.\n" +
				"4. lookup_transform — verify if a product-to-product transform is already known BEFORE proposing edges.\n\n" +
				"WORKFLOW: search_knowledge first → if not_found, use web_search → resolve_product for every product → lookup_transform for every relation → then produce your final JSON.\n\n" +
				"WHAT TO PRODUCE:\n" +
				"- proposedGraphOps is the PRIMARY output. Each thesis maps to graph operations: create_fragment, create_edge, confirm_edge, or mark_gap. Use ONLY canonical paths from resolve_product.\n" +
				"- summary: synthesize the strongest finding in 1-2 sentences of plain language.\n" +
				"- openQuestions: what you still don't know, phrased as questions a human would ask.\n" +
				"- dataSourceSignal: internal_grounded|internal_partial|internal_empty|no_data — be honest about where evidence came from.\n\n" +
				"WHEN TO STOP RESEARCHING:\n" +
				"- If you already have endpoints but can't connect them → diagnose missing_middle, recommend bridge.\n" +
				"- If you have a plausible chain ready for verification → diagnose missing_verification, recommend path.\n" +
				"- If claims conflict → diagnose missing_adjudication, recommend critic.\n" +
				"- Research is for OPENING the space, not for stitching known endpoints.\n\n" +
				"gapDiagnosis REQUIRED: {kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown}.\n" +
				"recommendedAction REQUIRED: {action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath}.\n" +
				"Respect canonical product control-plane semantics: do not conflate synonyms, transforms, assemblies, or generic dependencies.\n\nInput:\n" + inputJSON
		case "scout.bridge":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys branchId, endpointA, endpointB, bridgeQuestion, summary, bridgeExists, bridgeKind, missingMiddleTypes, intermediateNodes, intermediateRelationTypes, bridgeSteps, routeSketches, openQuestions, confidence, proposedGraphOps, suggestedSubscans, gapDiagnosis, recommendedAction, dataSourceSignal.\n\n" +
				"This is a MISSING-MIDDLE task. You have plausible endpoints but the runtime cannot connect them. Your job: figure out what sits between A and B.\n\n" +
				"TOOLS:\n" +
				"You have 4 tools. Use them to find the intermediate steps:\n" +
				"1. search_knowledge — check internal KB for known entities between A and B.\n" +
				"2. web_search — search the web for real-world supply chain connections, ports, facilities, companies.\n" +
				"3. resolve_product — resolve every product to its canonical path before proposing graph ops.\n" +
				"4. lookup_transform — check if product-to-product transforms are already known.\n\n" +
				"Think like a human analyst filling in a table: A → A1 → A2 → B. Name concrete entities (companies, ports, facilities, products) at each step. Use tools to find them — do not guess.\n\n" +
				"bridgeSteps REQUIRED when you can articulate any part of the route. Each step: index, stepClass, nodeRef, nodeLabel, nodeKind, fromRef, toRef, relationType, productRef, actorRef, geoRef, whyNeeded, evidenceHints, operationalStatus.\n" +
				"proposedGraphOps must turn the bridge into graph operations. Use ONLY canonical paths from resolve_product.\n" +
				"suggestedSubscans: follow-up questions as natural-language branch definitions.\n" +
				"dataSourceSignal: external_only|internal_grounded|internal_partial|no_data — be honest.\n\n" +
				"WHEN TO HAND OFF:\n" +
				"- Bridge is concrete enough to verify → recommend path.\n" +
				"- Claims conflict → recommend critic.\n" +
				"- Route not viable → say so explicitly, recommend research on a different angle.\n\n" +
				"gapDiagnosis REQUIRED: {kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown}.\n" +
				"recommendedAction REQUIRED: {action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath}.\n" +
				"Respect canonical product control-plane semantics.\n\nInput:\n" + inputJSON
		case "scout.critic":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys acceptedFindings, riskyClaims, openQuestions, materializePath, pathProducts, focusActors, focusGeos, confidence, withholdPath, withholdMaterialization, forceReplan, killBranchIds, newBranches, reprioritizeBranchIds, riskTargetBranchIds, graphOpVerdicts, branchAssessments, completenessAssessment, gapDiagnosis, recommendedAction.\n\n" +
				"You are a thesis validator, not a prose reviewer. Your job: is the evidence strong enough to act on?\n\n" +
				"CRITICAL RULES:\n" +
				"1. Only accept findings that are grounded in actual evidence (from retrieval hits, not from parametric memory).\n" +
				"2. Do NOT rubber-stamp query restatements as accepted findings.\n" +
				"3. 'Good enough for the graph' IS the quality bar. If the chain is structurally useful, say so and recommend landing it. Do not endlessly polish.\n" +
				"4. If the investigation has been looping (same gaps, same actions, no new evidence), set stopNow=true and explain that further work is low-value.\n\n" +
				"goodEnoughAssessment REQUIRED: {status, closureLevel, stopNow, persistWorkingPath, furtherWorkLowValue, reason, openNonBlockingGaps, targetBranchIds}.\n" +
				"- persistWorkingPath=true when the chain is useful enough to persist, even with open details.\n" +
				"- stopNow=true when remaining gaps are non-blocking or further queries keep returning empty.\n" +
				"- furtherWorkLowValue=true when tools have stopped meaningfully improving quality.\n\n" +
				"graphOpVerdicts REQUIRED when pendingGraphOps present: {opIndex, verdict, reason}.\n" +
				"branchAssessments preferred: {branchId, targetRelevance, pursuitDecision (press|pivot|park|drop), pursuitReason, routeHypothesis}.\n" +
				"completenessAssessment: {status, reason, alternativeRoutes, broadScanCompleted, noMaterialAlternatives}.\n" +
				"gapDiagnosis REQUIRED: {kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown}.\n" +
				"recommendedAction REQUIRED: {action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath}.\n\nInput:\n" + inputJSON
		case "scout.materialize":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys summary, skipped, downgraded, rejected, landedBranchIds (branch ids whose hypotheses were actually landed in this step), rejectedBranchIds (branches explicitly not landed), corrections. Keep landedBranchIds narrow: include only branches that this materialize step clearly landed (typically from path/source scope), not all supported branches. corrections is an array of objects: action (replace_existing | deprecate_existing | contradict_existing), artifactId, rationale, newPathStatus (optional), patchLabels (optional object of string values). Only reference artifactIds that appear in the path/materializePreview input. Do not invent artifact ids. Treat landing as a graph artifact decision, not a writing exercise. It is valid to preserve locally good graph substrate while withholding target-level conclusions. When sessionState is present, use it as the authoritative branch/hypothesis lifecycle snapshot.\n\nInput:\n" + inputJSON
		case "scout.aggregate":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys summary, findings, openQuestions, artifactIds, candidateProducts, recommendedNextStep. Use compact activeFrontier and researchDeltas to produce operator-level synthesis about graph state, path coverage, and unresolved gaps; avoid replaying full transcripts.\n\nInput:\n" + inputJSON
		case "scout.review":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys accepted, rationale, quality, issues. Review groundedness, deterministic materialization quality, bounded claims, graph-operation quality, and business usefulness from compact state.\n\nInput:\n" + inputJSON
		case "sim.population.generate":
			return "Return JSON only. Generate 20-40 heterogeneous socio-economic archetypes as an array. Each item must contain agentClass, displayName, populationWeight, traits, beliefState, resourceState, allowedActions, messageStyle, policyHints, originKind, originRefs, justification. allowedActions must be chosen only from: " + strings.Join(contractsToActionVocabulary(), ", ") + ".\n\nInput:\n" + inputJSON
		case "sim.world.step":
			return "Return JSON only with shape {\"actions\":[...]}. Each action must contain type, actorId, targetRefs, payload, justification, requiresGrounding. type must be chosen only from: " + strings.Join(contractsToActionVocabulary(), ", ") + ". Keep actions sparse and bounded. No prose.\n\nInput:\n" + inputJSON
		case "sim.review":
			return "Return JSON only with keys accepted:boolean, rationale:string, groundingCoverage:number, issues:string[]. Evaluate consistency, grounding completeness, bounded actions, and plausibility.\n\nInput:\n" + inputJSON
		}
	}
	if review {
		return "Review the task output and respond with JSON containing accepted:boolean, rationale:string, and quality:string.\n\nTask capability: " + task.Capability + "\n\nOutput to review:\n" + inputJSON
	}
	return "You are the AgentMesh worker. Produce a concise JSON object with keys summary, result, confidence, and nextActions for the requested capability.\n\nCapability: " + task.Capability + "\n\nInput:\n" + inputJSON
}

func scoutWorkspacePromptPrefix() string {
	return `You are a supply-chain intelligence analyst operating a knowledge graph.

Your workspace is not files or code — it is a live graph of companies, products, trade routes, facilities, ports, regulatory bodies, and the typed relationships between them (PRODUCES, transforms_into, supply_link, assembles_into, DEPENDS_ON, etc.). You can read what is already in the graph, propose new nodes and edges, search for evidence, and verify or reject hypotheses about how real-world supply chains connect.

How you think and communicate:
- Reason in plain language as a domain expert would. When you mention a product, say "lithium carbonate" not "/product/component/chemical/refining/lithium-carbonate". Canonical paths are internal identifiers — never use them in search queries, questions, or reasoning text.
- When you search for information, write queries the way a human analyst would type into Google: "Who are the main lithium carbonate exporters in Chile?", "What is the shipping route from Antofagasta to Rotterdam?", "Does AMG Lithium operate a conversion plant in Germany?"
- When the internal knowledge base returns nothing useful, say so honestly. Do not fabricate evidence or pretend confidence you do not have. Zero hits means you lack data, not that the data is negative.
- When you have exhausted a line of inquiry (2+ attempts with no new information), stop that line and either try a different angle or declare the gap honestly.
- Memory context is hint-only. It can suggest where to look next, but it is never evidence. Do not materialize graph facts from memory unless fresh grounded evidence in the current run supports them.

Your job is to build and improve the graph — add useful structure (nodes, edges, paths) that reflect real supply-chain relationships. You are not writing a report or a legal brief. "Good enough for the graph" is the quality bar: a structurally useful chain is better than an endlessly polished hypothesis.

Use the provided goal, environment, role, and completeness objects as your mission contract. If the role object is absent, determine the strongest professional lens from the task itself.`
}

func contractsToActionVocabulary() []string {
	return []string{
		"broadcast_message",
		"change_belief",
		"buy",
		"sell",
		"delay_purchase",
		"switch_supplier",
		"lobby",
		"issue_statement",
		"enforce_rule",
		"adjust_budget",
		"escalate",
		"do_nothing",
	}
}

func (s *Service) doVertexJSON(ctx context.Context, method, rawURL string, payload map[string]any) (map[string]any, error) {
	var bodyReader *strings.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(body))
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	token, err := s.tokenSource.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vertex endpoint %s returned %d: %v", rawURL, resp.StatusCode, out)
	}
	return out, nil
}

func (s *Service) doVertexSSE(ctx context.Context, method, rawURL string, payload map[string]any) ([]any, error) {
	var bodyReader *strings.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(body))
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	token, err := s.tokenSource.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("vertex endpoint %s returned %d: %v", rawURL, resp.StatusCode, readErr)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err == nil {
			return nil, fmt.Errorf("vertex endpoint %s returned %d: %v", rawURL, resp.StatusCode, out)
		}
		return nil, fmt.Errorf("vertex endpoint %s returned %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	events := make([]any, 0, 8)
	dataLines := make([]string, 0, 4)
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "" || data == "[DONE]" {
			return nil
		}
		var event any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("vertex stream event decode failed: %w", err)
		}
		events = append(events, event)
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		// Agent Engine may return NDJSON-style event lines even when alt=sse is requested.
		if err := flush(); err != nil {
			return nil, err
		}
		dataLines = append(dataLines, line)
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Service) waitForOperation(ctx context.Context, operation map[string]any) (map[string]any, error) {
	name, _ := operation["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("vertex long-running operation name is missing")
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		current, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(name), nil)
		if err != nil {
			return nil, err
		}
		done, _ := current["done"].(bool)
		if done {
			if errBody, ok := nestedMap(current, "error"); ok && len(errBody) > 0 {
				return nil, fmt.Errorf("vertex operation failed: %v", errBody)
			}
			return current, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vertex operation %s timed out", name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func nestedMap(in map[string]any, key string) (map[string]any, bool) {
	raw, ok := in[key]
	if !ok {
		return nil, false
	}
	out, ok := raw.(map[string]any)
	return out, ok
}

func resourceURL(resource string) string {
	resource = strings.TrimLeft(strings.TrimSpace(resource), "/")
	return vertexAPIBaseURL + "/" + resource
}

func sanitizeResourceID(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-':
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}
	value := strings.Trim(out.String(), "-")
	if value == "" {
		return "agentmesh"
	}
	if len(value) > 63 {
		value = value[:63]
	}
	return value
}

func sanitizeLabelValue(in string) string {
	value := sanitizeResourceID(in)
	if value == "" {
		return "agentmesh"
	}
	return value
}

func defaultModel(envName, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	return fallback
}

func validateVertexModelAliases(mm *ai.AIModelManager, aliases ModelAliases) error {
	values := map[string]string{
		"AGENTMESH_MODEL_FLASH":           aliases.FlashLite,
		"AGENTMESH_MODEL_FLASH_REASONING": aliases.FlashReasoning,
		"AGENTMESH_MODEL_PRO":             aliases.Pro,
	}
	for envName, modelName := range values {
		modelName = strings.TrimSpace(modelName)
		if !strings.HasPrefix(modelName, "vertex-") {
			return fmt.Errorf("%s must be a Vertex model alias, got %q", envName, modelName)
		}
		caps, err := mm.GetModelCapabilities(modelName)
		if err != nil {
			return fmt.Errorf("%s model %q is not registered: %w", envName, modelName, err)
		}
		if caps.Provider != ai.AIProviderVertexAI {
			return fmt.Errorf("%s model %q resolves to provider %s, want %s", envName, modelName, caps.Provider, ai.AIProviderVertexAI)
		}
	}
	return nil
}

func compactModelFallbacks(models ...string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
