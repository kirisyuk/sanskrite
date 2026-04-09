package vertex

import (
	"context"
	"encoding/json"
	"fmt"
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
	vertexAI        *ai.VertexAIService
	models          ModelAliases
	httpClient      *http.Client
	tokenSource     oauth2.TokenSource
	projectID       string
	location        string
	reasoningEngine string
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
	return &Service{
		vertexAI: vertexSvc,
		models: ModelAliases{
			FlashLite:      defaultModel("AGENTMESH_MODEL_FLASH", "gemini-3.1-flash-lite-preview"),
			FlashReasoning: defaultModel("AGENTMESH_MODEL_FLASH_REASONING", "gemini-3-flash-preview"),
			Pro:            defaultModel("AGENTMESH_MODEL_PRO", "gemini-3.1-pro-preview"),
		},
		httpClient:      &http.Client{Timeout: 45 * time.Second},
		tokenSource:     ts,
		projectID:       strings.TrimSpace(projectID),
		location:        strings.TrimSpace(location),
		reasoningEngine: engine,
	}, nil
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
	case "scout.plan", "scout.decide", "scout.bridge", "scout.critic":
		return s.models.FlashReasoning
	default:
		return s.models.FlashLite
	}
}

func (s *Service) Readiness(ctx context.Context) error {
	if s == nil || s.vertexAI == nil {
		return fmt.Errorf("vertex service is not configured")
	}
	if strings.TrimSpace(s.reasoningEngine) == "" {
		return fmt.Errorf("reasoning engine is not configured")
	}
	_, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(s.reasoningEngine), nil)
	return err
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

func (s *Service) EnsureMemoryBank(_ context.Context, task *contracts.MeshTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(task.MemoryBankRef) != "" {
		return task.MemoryBankRef, nil
	}
	return s.reasoningEngine + "/memories", nil
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

func (s *Service) GenerateMemoriesFromSession(ctx context.Context, task *contracts.MeshTask) error {
	if task == nil || strings.TrimSpace(task.SessionRef) == "" {
		return fmt.Errorf("task with sessionRef is required")
	}
	_, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(s.reasoningEngine)+"/memories:generate", map[string]any{
		"vertexSessionSource": map[string]any{
			"session": task.SessionRef,
		},
		"scope": map[string]string{
			"tenant": task.TenantID,
		},
		"metadata": map[string]string{
			"capability": task.Capability,
			"taskId":     task.ID,
		},
	})
	return err
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
	req := &ai.AITask{
		TaskType:     taskType,
		InputContent: buildPrompt(task, review, string(inputJSON)),
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  s.ModelNameForCapability(task.Capability, review),
			TextGenerationCapability:        true,
			SearchGroundingCapability:       searchGrounding,
			InternalDataGroundingCapability: internalGrounding,
			MaxOutputSize:                   8192,
		},
		TenantID:  task.TenantID,
		SessionID: task.SessionRef,
		TraceID:   task.ID,
		UseCase:   "agentmesh",
	}
	modelName := s.ModelNameForCapability(task.Capability, review)
	result, err := s.vertexAI.ExecuteTask(ctx, req, modelName)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{
		"content":    result.Content,
		"model":      result.Model,
		"citations":  result.Citations,
		"grounding":  result.GroundingEvidence,
		"reviewMode": review,
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

func buildPrompt(task *contracts.MeshTask, review bool, inputJSON string) string {
	if !review {
		switch strings.TrimSpace(task.Capability) {
		case "scout.plan":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only. Build or refine an investigation plan with keys goal, missionRole, focusProducts, focusGeos, focusActors, wantsPathMaterialize, maxResearchRounds, planningRationale, existingGraphPriorities, planVersion, materializationStrategy, branchDependencies, branches. Use goal + environment + role + completeness as explicit contracts for what the investigation is trying to learn, what kind of graph workspace it operates in, which top-grade professional lens should dominate, and whether a broad scan is required before route narrowing. You MUST determine the professional role from the task itself rather than selecting from a fixed catalog. If the incoming role object is empty or weak, ignore it and define the role yourself. missionRole must contain domain, primaryRole, supportingRoles, professionalQualityBar, investigationPosture, roleRationale. Each branch item must contain id, question, role, productHints, geoHints, actorHints, requiresPath, dependsOn (branch ids), broadScan, testCriteria, expectedInfoGain. Include at least one broad-scan branch when completeness says broadScanRequired=true unless such a branch already exists. Each branchDependencies item must contain fromBranchId and toBranchId meaning toBranchId depends on fromBranchId. When investigationState or replanReason is present, reshape the plan (add/split/kill branches, update strategy) rather than repeating the prior plan.\n\nInput:\n" + inputJSON
		case "scout.decide":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys stateSummary, chosenAction, branchId, whyChosen, whyNotStop, whyNotPath, whyNotBridge, whyNotResearch, whyNotCritic, whyNotMaterialize, whyNotReplan, evidenceUsed, stopCriteriaEvaluation, expectedInformationGain, confidence. " +
				"You are the orchestration decider for a graph-filling swarm. Your job is to choose the next method, not to do the research yourself. " +
				"IMPORTANT: this runtime is for filling and improving the graph with useful chain/path structure, not for writing a courtroom-perfect proof. If the current chain is already good enough to persist and more tool use is unlikely to improve graph quality materially, choose path or stop accordingly. " +
				"You must choose exactly one action from the supplied candidateActions where available=true. If none are available, choose stop and explain why. " +
				"Use latestOutcome/recentOutcomes as the primary truth about what the previous attempts actually produced. Do not guess progress from prose if latestOutcome already tells you whether the last step materially improved graph quality, failed systemically, or produced a path-worthy bridge. " +
				"Use the candidateActions plus investigationState/activeFrontier/latestOutcome to decide whether the next best move is research, bridge, path, critic, materialize, replan, or stop. " +
				"Distinguish clearly between: opening the space (research), connecting known endpoints (bridge), verifying a concrete chain (path), adjudicating conflicting claims (critic), landing artifacts (materialize), changing strategy (replan), and stopping because the result is already good enough. " +
				"If latestOutcome says systemError=true and retryable=true, treat that as a failed tool call rather than a knowledge gap. Retry only if the same move is still the best option and the retry budget is not exhausted. " +
				"If latestOutcome says qualityDelta=none and the same gap remains, do not keep repeating the same action unless one reformulation/retry is still clearly justified. " +
				"If you choose research after a bridge/path failure, explain specifically why another research turn is still likely to improve graph quality more than bridge/path/stop. " +
				"If you do NOT choose stop, explain why the current evidence does not yet satisfy the stop criteria. If you do NOT choose path, explain why the current chain is not yet the right one to persist. " +
				"stopCriteriaEvaluation is REQUIRED. It must be an array of objects {criterion, satisfied, why}. Evaluate at least: current_best_chain_is_useful_for_graph_fill, materially_different_routes_have_been_scanned_enough, remaining_gaps_are_non_blocking_or_low_value, next_tool_call_is_likely_to_improve_quality. " +
				"evidenceUsed should cite the concrete state elements that drove the decision, such as latestOutcome summary/delta, latest bridge summary, path continuity status, latest critic goodEnoughAssessment, or branch assessments. " +
				"chosenAction must be one of: research, bridge, path, critic, materialize, replan, stop.\n\nInput:\n" + inputJSON
		case "scout.research":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys summary, evidencePoints, canonicalRefs, productRefs, geoRefs, actorRefs, recommendedPathProducts, openQuestions, confidence, proposedGraphOps, gapDiagnosis, recommendedAction. " +
				"CRITICAL: produce DOMAIN THESES, not a paraphrase of the query. " +
				"Honor the supplied professional role: reason like that investigator would, using the role to tighten what evidence matters and what gaps are unacceptable. " +
				"Each thesis must be an explicit claim about the world that can be true/false and should be grounded by evidencePoints. " +
				"summary must be a concise synthesis of strongest thesis, not the original user question. Treat entities, typed edges, and metadata as the primary objects of reasoning. " +
				"proposedGraphOps is the PRIMARY output: each major thesis must map to at least one graph operation. " +
				"Each op has: kind (create_fragment|create_edge|confirm_edge|mark_gap), subjectPath (canonical path), objectPath (for edge), edgeType (domain-appropriate relation), rationale (why this op follows from evidence). " +
				"Boundary of research: use research only to gather or broaden substrate, enumerate route classes, or test factual claims. Research is for opening the space, not for stitching already-known endpoints together. If you already have endpoints or route fragments but cannot connect them, that is NOT a research gap; diagnose missing_middle or a more specific middle-layer gap and recommend bridge. If deterministic tracing now has enough scoped products/entities, diagnose missing_verification and recommend path. If the main issue is adjudicating competing claims, diagnose missing_adjudication and recommend critic. If one route is found but you still need to know whether materially different alternatives remain, diagnose missing_completeness and recommend research or critic accordingly. " +
				"When the branch is broadScan=true, do not rush into one route. First enumerate materially different plausible routes or say explicitly that none remain. Those findings belong in summary/openQuestions even if they are not yet graph-landable. " +
				"If evidence is weak: do NOT pretend certainty. Put unresolved items into openQuestions and mark gaps via mark_gap ops. Respect canonical product control-plane semantics: do not conflate synonyms, transforms, assemblies, or generic dependencies. " +
				"gapDiagnosis is REQUIRED. It must be an object with keys kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown. Allowed research-side kinds are missing_substrate, missing_completeness, or no_gap. If endpoints are already known and the issue is what lies between them, use missing_middle, missing_transform_chain, missing_institutional_layer, missing_regulatory_layer, or missing_ownership_layer. In those cases nextAction should normally be bridge. Only keep nextAction=research if the true blocker is named-entity discovery inside the middle layer rather than structural stitching; if you do that, say so explicitly in why. " +
				"recommendedAction is REQUIRED. It must be an object with keys action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath. Use it to say what neighboring method the runtime should try next if this research direction did not fully close the gap. " +
				"Do NOT repeat the query text verbatim in summary/evidencePoints/openQuestions unless quoting a source. " +
				"Use goal + environment + activeFrontier + planningContext; if pendingGraphOps is provided, avoid duplicates and prefer advancing or challenging existing ops.\n\nInput:\n" + inputJSON
		case "scout.bridge":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys branchId, endpointA, endpointB, bridgeQuestion, summary, bridgeExists, bridgeKind, missingMiddleTypes, intermediateNodes, intermediateRelationTypes, bridgeSteps, routeSketches, openQuestions, confidence, proposedGraphOps, suggestedSubscans, gapDiagnosis, recommendedAction. " +
				"This is a missing-middle task: you already have plausible endpoints or route fragments, but the runtime cannot connect them. Answer the human question 'what is between them?' in graph-native form. " +
				"Bridge is the universal contract for structural connection questions: what is between A and B, how A transforms into B, which institutional/distributor/ownership/regulatory layer sits between the endpoints, and whether a viable bridge exists at all. " +
				"bridgeExists should be true only if a plausible real-world bridge likely exists. bridgeKind should describe the route class such as refining, trader bridge, logistics corridor, port chain, toll conversion, distributor, customs route, or contract chain. " +
				"missingMiddleTypes should name the types of intermediate layers required. intermediateNodes should list concrete or canonical intermediate nodes when known. intermediateRelationTypes should name the typed links that would connect the route. routeSketches should summarize materially different bridge variants. " +
				"bridgeSteps is REQUIRED when bridgeExists=true or when you can articulate the route. It is the systematic write-out of the answer. Each bridgeSteps item should describe one row in the implied chain with: index, stepClass, nodeRef, nodeLabel, nodeKind, fromRef, toRef, relationType, productRef, actorRef, geoRef, whyNeeded, evidenceHints, operationalStatus. Use it even if some refs are still unknown. " +
				"Think like a human analyst writing the missing middle into a table: A -> A1 -> A2 -> B, with typed links and notes about what each step does. proposedGraphOps must turn the bridge hypothesis into graph-native candidate artifacts using canonical paths where possible. If proposedGraphOps is incomplete, bridgeSteps must still be detailed enough for the runtime to derive artifacts. If the current route is not viable, say so explicitly and use suggestedSubscans to point the runtime toward a better continuation. " +
				"Boundary of bridge: use bridge when the endpoints or route fragments are already plausible but the missing middle is unknown. Do NOT use bridge to do a broad scan of the whole space. If the route is not viable, diagnose the failure explicitly and recommend targeted research or a pivot branch. If the bridge is now concrete enough for deterministic verification, diagnose missing_verification and recommend path. If bridge findings only need validation against competing claims, diagnose missing_adjudication and recommend critic. " +
				"Each suggestedSubscans item must follow the normal branch shape: id, question, role, productHints, geoHints, actorHints, requiresPath, dependsOn, broadScan, testCriteria, expectedInfoGain. " +
				"gapDiagnosis is REQUIRED. It must be an object with keys kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown. Bridge-side kinds should usually be missing_middle, missing_transform_chain, missing_institutional_layer, missing_regulatory_layer, missing_ownership_layer, missing_verification, or no_gap. If bridge still cannot name the middle entities but has clarified the route class, keep kind in the missing-middle family and set nextAction=research only when the next task is entity discovery inside that middle layer; explain that explicitly in why. " +
				"recommendedAction is REQUIRED. It must be an object with keys action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath. This is where you tell the runtime which neighboring method should run next after the bridge analysis. " +
				"Do not collapse synonyms into transforms or transforms into generic dependencies. Respect canonical product control-plane semantics.\n\nInput:\n" + inputJSON
		case "scout.critic":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys acceptedFindings, riskyClaims, openQuestions, materializePath, pathProducts, focusActors, focusGeos, confidence, withholdPath, withholdMaterialization, forceReplan, killBranchIds, newBranches (same shape as plan branches), reprioritizeBranchIds (ordered highest-first), riskTargetBranchIds, graphOpVerdicts, branchAssessments, completenessAssessment, gapDiagnosis, recommendedAction. " +
				"Act as a thesis validator, not a stylistic reviewer. acceptedFindings should contain only grounded theses. riskyClaims should contain explicit contradictory/weak theses. " +
				"Do NOT rubber-stamp query restatements as accepted findings. " +
				"Boundary of critic: critic decides whether the current graph-native findings are safe, premature, contradictory, or incomplete, and must name the next best method when the current one did not close the task. Do not do fresh broad research inside critic; instead route the runtime toward research, bridge, path, materialize, replan, or stop. Critic should distinguish between missing_verification, missing_adjudication, missing_completeness, and missing_middle rather than collapsing everything into 'needs more research'. " +
				"This runtime is a graph-filling swarm, not a courtroom brief. Good enough matters. If the chain is already sufficiently useful for graph/path purposes, say so explicitly instead of polishing forever. " +
				"goodEnoughAssessment is REQUIRED. It must contain status, closureLevel, stopNow, persistWorkingPath, furtherWorkLowValue, reason, openNonBlockingGaps, targetBranchIds. Use it to answer: is the current result already good enough for this runtime's graph-filling purpose, should the system persist a working path now, and are the remaining gaps too low-value to justify more turns? " +
				"Set persistWorkingPath=true when the route or branch is already structurally useful enough that a deterministic path attempt should fix the current best chain in graph artifacts, even if some commercial details remain open. " +
				"Set stopNow=true only when the current result is already useful enough for this mission and the remaining gaps are non-blocking or low-value for this runtime tier. If the tools have stopped meaningfully improving quality, say that in furtherWorkLowValue/reason. " +
				"graphOpVerdicts is REQUIRED when pendingGraphOps is provided: for each pending op index return {opIndex, verdict (accepted_local|accepted_target|rejected|needs_research|needs_target_binding), reason}. " +
				"Use accepted_local when a graph fact is locally strong and worth fixing even though the end-to-end target path is unresolved. Use accepted_target when the op is good and sufficiently bound to the user objective. Use needs_target_binding when the local fact is plausible but still lacks target linkage. Rejected is for contradicted/unverifiable claims. needs_research is for missing substrate. " +
				"branchAssessments is strongly preferred when branches are present: each item should contain branchId, targetRelevance (0..1), pursuitDecision (press|pivot|park|drop), pursuitReason, routeHypothesis, alternativeRoute, suggestedBranchIds. Use press when this branch is still a strong route to the objective, pivot when local facts are useful but the route should shift, park when the branch is locally valid but currently low-value to the objective, and drop when the branch is both weak and low-value. " +
				"completenessAssessment should contain status (pending|in_progress|needs_follow_on|satisfied), reason, alternativeRoutes, missingAngles, broadScanCompleted, noMaterialAlternatives. Use it to answer whether the investigation has already scanned for major alternative routes or should keep going. " +
				"gapDiagnosis is REQUIRED. It must be an object with keys kind, summary, why, questionClass, nextAction, branchId, endpointA, endpointB, endpointsKnown. Critic-side kinds should usually be missing_adjudication, missing_verification, missing_completeness, missing_middle, or no_gap. If critic diagnoses a missing-middle kind and endpoints are already known, nextAction should usually be bridge. Use nextAction=research only if the best next move is explicit entity discovery within the already-known middle layer, and explain that in why. " +
				"recommendedAction is REQUIRED. It must be an object with keys action, branchId, reason, failureMode, question, endpointA, endpointB, productHints, geoHints, actorHints, requiresPath. Use it to state the next best runtime move after validation. " +
				"If riskyClaims non-empty and targets are clear, include riskTargetBranchIds; if unclear, leave targets empty and set withholdMaterialization=true. Critique graph-native claims and operations, not prose quality.\n\nInput:\n" + inputJSON
		case "scout.materialize":
			return scoutWorkspacePromptPrefix() + "\n\nReturn JSON only with keys summary, skipped, downgraded, rejected, landedBranchIds (branch ids whose hypotheses were actually landed in this step), rejectedBranchIds (branches explicitly not landed), corrections. Keep landedBranchIds narrow: include only branches that this materialize step clearly landed (typically from path/source scope), not all supported branches. corrections is an array of objects: action (replace_existing | deprecate_existing | contradict_existing), artifactId, rationale, newPathStatus (optional), patchLabels (optional object of string values). Only reference artifactIds that appear in the path/materializePreview input. Do not invent artifact ids. Treat landing as a graph artifact decision, not a writing exercise. It is valid to preserve locally good graph substrate while withholding target-level conclusions.\n\nInput:\n" + inputJSON
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
	return "You are operating inside the Horizon graph workspace, not a file/code workspace. The environment is a graph of canonical entities, typed relations, metadata-bearing fragments, candidate gaps, and materialized supply-chain artifacts. Treat canonical paths as stable identities. Use the provided goal, environment, role, and completeness objects as the first-class mission and workspace contract. If the role object is absent or weak, determine the strongest professional lens yourself from the task. Produce graph-native reasoning and decisions: what entities/relations are supported, what graph operations should happen next, what paths can be verified, what broad-scan alternatives exist, and what gaps must remain unresolved."
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

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
