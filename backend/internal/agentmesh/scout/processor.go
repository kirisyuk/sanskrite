package scout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type Processor struct {
	agentJobs         *corerepo.AgentJobsRepository
	scoutingJobs      *horizonrepo.ScoutingJobsRepository
	workflow          *horizonscout.ScoutWorkflow
	fragments         *horizonrepo.HorizonFragmentsRepository
	edges             *horizonrepo.DependencyEdgesRepository
	artifacts         *horizonrepo.MaterializedArtifactsRepository
	productDictionary *productdict.Runtime
	queryEngine       *pathbuilder.SupplyChainQueryEngine
	pathBuilder       *pathbuilder.SupplyChainPathBuilder
	assemblyBuilder   *pathbuilder.SupplyChainAssemblyBuilder
	knowledgeBase     knowledgeBaseSearcher
	logger            *log.Logger
	runChildTaskFn    func(context.Context, *agentmeshruntime.Service, *contracts.MeshTask, string, string, map[string]any) (*contracts.MeshTask, map[string]any, error)
	runModelObjectFn  func(context.Context, *agentmeshruntime.Service, *contracts.MeshTask, map[string]any) map[string]any
}

func NewProcessor(
	agentJobs *corerepo.AgentJobsRepository,
	scoutingJobs *horizonrepo.ScoutingJobsRepository,
	fragments *horizonrepo.HorizonFragmentsRepository,
	edges *horizonrepo.DependencyEdgesRepository,
	artifacts *horizonrepo.MaterializedArtifactsRepository,
	productDictionary *productdict.Runtime,
	vertexSearch *ai.VertexAISearchClient,
	logger *log.Logger,
) *Processor {
	if logger == nil {
		logger = log.Default()
	}
	pathBuilder := pathbuilder.NewSupplyChainPathBuilder(fragments, edges, artifacts)
	assemblyBuilder := pathbuilder.NewSupplyChainAssemblyBuilder(fragments, edges, pathBuilder)
	if productDictionary != nil {
		assemblyBuilder.WithProductDictionary(productDictionary)
	}
	return &Processor{
		agentJobs:         agentJobs,
		scoutingJobs:      scoutingJobs,
		workflow:          horizonscout.NewScoutWorkflow(agentJobs).WithScoutingJobsRepo(scoutingJobs),
		fragments:         fragments,
		edges:             edges,
		artifacts:         artifacts,
		productDictionary: productDictionary,
		queryEngine:       pathbuilder.NewSupplyChainQueryEngine(fragments, edges),
		pathBuilder:       pathBuilder,
		assemblyBuilder:   assemblyBuilder,
		knowledgeBase:     newVertexSearchAdapter(vertexSearch),
		logger:            logger,
	}
}

func (p *Processor) CanHandle(task *contracts.MeshTask) bool {
	return task != nil && strings.HasPrefix(strings.TrimSpace(task.Capability), "scout.")
}

func (p *Processor) Process(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string) (*agentmeshruntime.ProcessResult, error) {
	switch strings.TrimSpace(task.Capability) {
	case "scout.plan":
		return p.processPlanBootstrap(ctx, rt, task, leaseOwner)
	case "scout.orchestrate":
		return p.processOrchestrate(ctx, rt, task, leaseOwner)
	case "scout.decide":
		return p.processDecide(ctx, rt, task)
	case "scout.research":
		return p.processResearch(ctx, rt, task)
	case "scout.bridge":
		return p.processBridge(ctx, rt, task)
	case "scout.critic":
		return p.processCritic(ctx, rt, task)
	case "scout.path":
		return p.processPath(ctx, rt, task)
	case "scout.materialize":
		return p.processMaterialize(ctx, rt, task)
	case "scout.aggregate":
		return p.processAggregate(ctx, rt, task)
	case "scout.review":
		return p.processReview(ctx, rt, task)
	default:
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: contracts.RetryClassPermanent,
			LastError:  "unsupported scouting capability",
		}, nil
	}
}

func (p *Processor) processPlanBootstrap(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string) (*agentmeshruntime.ProcessResult, error) {
	if task == nil {
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: contracts.RetryClassPermanent,
			LastError:  "task is nil",
		}, nil
	}
	p.ensureInvestigationSession(ctx, rt, task, leaseOwner)
	orchestrateInput := enrichScoutModelInput(modelTaskInput(task), task)
	child, output, err := p.runChildTask(ctx, rt, task, "scout.orchestrate", task.ID+":orchestrate", orchestrateInput)
	if err != nil {
		retryClass := contracts.RetryClassPermanent
		if isRetryableSystemError(err) {
			retryClass = contracts.RetryClassTransient
		}
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: retryClass,
			LastError:  err.Error(),
		}, nil
	}
	status := contracts.TaskStatusSucceeded
	if child != nil && child.Status != "" {
		status = child.Status
	}
	review := mapValue(output["review"])
	reviewAccepted := deriveScoutReviewAccepted(output, status)
	result := &agentmeshruntime.ProcessResult{
		Status:          status,
		Output:          output,
		ArtifactPayload: output,
	}
	if len(review) > 0 {
		result.ReviewOutput = review
	}
	if reviewAccepted != nil {
		result.ReviewAccepted = reviewAccepted
	}
	if rationale := deriveScoutTaskRationale(output); rationale != "" {
		result.LastError = rationale
	}
	return result, nil
}

func (p *Processor) processDecide(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(input, "scout.decide")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(input["request"])
	plan := decodePlan(input["plan"])
	goal := compactGoalFrame(req.Goal)
	if raw := mapValue(input["goal"]); len(raw) > 0 {
		goal = raw
	}
	environment := compactEnvironmentFrame(req.Environment)
	if raw := mapValue(input["environment"]); len(raw) > 0 {
		environment = raw
	}
	role := compactRoleFrame(req.Role)
	if raw := mapValue(input["role"]); len(raw) > 0 {
		role = raw
	}
	completeness := compactCompletenessFrame(req.Completeness)
	if raw := mapValue(input["completeness"]); len(raw) > 0 {
		completeness = raw
	}
	modelIn := enrichScoutModelInput(map[string]any{
		"request":          req,
		"goal":             goal,
		"environment":      environment,
		"role":             role,
		"completeness":     completeness,
		"plan":             compactPlanForModel(plan),
		"sessionState":     input["sessionState"],
		"latestOutcome":    input["latestOutcome"],
		"recentOutcomes":   input["recentOutcomes"],
		"activeFrontier":   input["activeFrontier"],
		"candidateActions": input["candidateActions"],
		"decisionContext":  input["decisionContext"],
		"parentSessionRef": strings.TrimSpace(task.SessionRef),
	}, task)
	parsed := p.runModelObject(ctx, rt, task, modelIn)
	if runtimeErr := modelRuntimeError(parsed); runtimeErr != "" {
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: modelRetryClass(runtimeErr),
			LastError:  runtimeErr,
		}, nil
	}
	if len(parsed) == 0 {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.decide returned empty or invalid JSON object",
		}, nil
	}
	decision := decodeDecisionResult(parsed)
	if isEmptyDecisionResult(decision) {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.decide returned schema-empty payload",
		}, nil
	}
	decision = normalizeDecisionResult(decision)
	output := decisionMap(decision)
	output["stateDelta"] = map[string]any{
		"kind":              "decision",
		"chosenAction":      decision.ChosenAction,
		"branchId":          decision.BranchID,
		"executedAction":    decision.ExecutedAction,
		"executedBranchId":  decision.ExecutedBranchID,
		"validationStatus":  decision.ValidationStatus,
		"validationReason":  decision.ValidationReason,
		"expectedInfoGain":  decision.ExpectedInformationGain,
		"decisionEvidence":  decision.EvidenceUsed,
		"stopCriteria":      decision.StopCriteria,
		"stateSummary":      decision.StateSummary,
		"whyChosen":         decision.WhyChosen,
		"recommendedAction": compactActionRecommendation(stateToRecommendedAction(decision)),
	}
	applyDecisionToState(sessionState, decision)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func shouldUseHostedScoutOrchestrator(rt *agentmeshruntime.Service) bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AGENTMESH_SCOUT_ORCHESTRATOR_MODE")))
	if mode == "local" {
		return false
	}
	return rt != nil && rt.Vertex() != nil
}

func scoutMemoryQuery(req InvestigationRequest, input map[string]any) string {
	parts := []string{
		req.ScoutingJobID,
		req.TargetCanonicalPath,
		stringValue(input, "focus"),
		stringValue(input, "query"),
	}
	if goal := compactGoalFrame(req.Goal); len(goal) > 0 {
		raw, _ := json.Marshal(goal)
		parts = append(parts, string(raw))
	}
	return strings.Join(compactStrings(parts), "\n")
}

func deriveHostedScoutTaskResult(output map[string]any) (contracts.TaskStatus, *bool, string) {
	status, rationale := deriveHostedScoutTaskStatus(output)
	return status, deriveScoutReviewAccepted(output, status), firstNonEmpty(rationale, deriveScoutTaskRationale(output))
}

func deriveHostedScoutTaskStatus(output map[string]any) (contracts.TaskStatus, string) {
	state := mapValue(output["state"])
	action := mapValue(output["action"])
	stateStatus := strings.ToLower(strings.TrimSpace(stringValue(state, "status")))
	actionType := strings.ToLower(strings.TrimSpace(stringValue(action, "type")))
	terminal, _ := action["terminal"].(bool)
	reason := firstNonEmpty(strings.TrimSpace(stringValue(action, "reason")), strings.TrimSpace(stringValue(state, "stopReason")))
	switch stateStatus {
	case "completed":
		return contracts.TaskStatusSucceeded, reason
	case "awaiting_review":
		return contracts.TaskStatusAwaitingReview, reason
	case "degraded":
		return contracts.TaskStatusDegraded, reason
	case "failed":
		return contracts.TaskStatusFailed, reason
	case "running", "blocked":
		return contracts.TaskStatusDegraded, firstNonEmpty(reason, fmt.Sprintf("hosted scout returned non-terminal snapshot: state=%s action=%s terminal=%t", firstNonEmpty(stateStatus, "unknown"), firstNonEmpty(actionType, "unknown"), terminal))
	default:
		return contracts.TaskStatusDegraded, firstNonEmpty(reason, fmt.Sprintf("hosted scout returned unsupported lifecycle: state=%s action=%s terminal=%t", firstNonEmpty(stateStatus, "unknown"), firstNonEmpty(actionType, "unknown"), terminal))
	}
}

func deriveScoutTaskRationale(output map[string]any) string {
	review := mapValue(output["review"])
	action := mapValue(output["action"])
	state := mapValue(output["state"])
	return firstNonEmpty(
		strings.TrimSpace(stringValue(action, "reason")),
		strings.TrimSpace(stringValue(review, "rationale")),
		strings.TrimSpace(stringValue(state, "stopReason")),
	)
}

func deriveScoutReviewAccepted(output map[string]any, status contracts.TaskStatus) *bool {
	if accepted, ok := output["reviewAccepted"].(bool); ok {
		return boolPtr(accepted)
	}
	review := mapValue(output["review"])
	if accepted, ok := review["accepted"].(bool); ok {
		if strings.TrimSpace(stringValue(review, "rationale")) != "" || len(review) > 1 {
			return boolPtr(accepted)
		}
	}
	switch status {
	case contracts.TaskStatusSucceeded:
		return boolPtr(true)
	case contracts.TaskStatusAwaitingReview, contracts.TaskStatusDegraded, contracts.TaskStatusFailed:
		return boolPtr(false)
	default:
		return nil
	}
}

func (p *Processor) processOrchestrate(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string) (*agentmeshruntime.ProcessResult, error) {
	req, job, err := p.resolveRequest(ctx, task)
	if err != nil {
		return nil, err
	}
	if err := p.markJobRunning(ctx, req, task.ID); err != nil && p.logger != nil {
		p.logger.Printf("[agentmesh.scout] mark running failed job=%s err=%v", req.ScoutingJobID, err)
	}

	p.ensureInvestigationSession(ctx, rt, task, leaseOwner)

	input := modelTaskInput(task)
	graphCtx := p.buildPlanningContext(ctx, req)
	if shouldUseHostedScoutOrchestrator(rt) {
		hostedInput := enrichScoutModelInput(map[string]any{
			"request":          req,
			"taskInput":        input,
			"planningContext":  compactPlanningContext(graphCtx),
			"parentSessionRef": strings.TrimSpace(task.SessionRef),
		}, task)
		memories, memErr := rt.Vertex().FetchMemories(ctx, task, scoutMemoryQuery(req, input))
		if memErr != nil && p.logger != nil {
			p.logger.Printf("[agentmesh.scout] memory fetch failed task=%s err=%v", task.ID, memErr)
		}
		output, cost, err := rt.Vertex().RunScoutOrchestrator(ctx, task, hostedInput, memories)
		if err != nil {
			review := map[string]any{
				"accepted":  false,
				"rationale": err.Error(),
				"quality":   "degraded",
				"issues":    []string{"hosted_orchestrator_failure"},
			}
			degraded := map[string]any{
				"plan":           map[string]any{"goal": firstNonEmpty(req.Query, req.TargetCanonicalPath)},
				"state":          map[string]any{"status": "degraded", "stopReason": "hosted_orchestrator_failure"},
				"action":         map[string]any{"type": "degrade", "terminal": true, "reason": err.Error()},
				"reasoningTrace": []any{},
				"runtimeDiagnosis": []any{
					map[string]any{"code": "hosted_orchestrator_failure", "severity": "error", "message": err.Error()},
				},
				"review":         review,
				"taskRefs":       map[string]any{},
				"sessionRef":     strings.TrimSpace(task.SessionRef),
				"memoryBankRef":  strings.TrimSpace(task.MemoryBankRef),
				"reviewAccepted": false,
				"evidenceLedger": []any{},
				"uncertaintyLedger": []any{
					map[string]any{"id": "runtime_failure", "topic": "hosted_orchestrator", "summary": err.Error(), "impact": "hosted_orchestrator_unavailable", "nextCheck": "retry_with_vertex"},
				},
				"decisionLedger":   []any{},
				"reasoningSummary": "Hosted Scout orchestrator failed before producing a valid task snapshot.",
				"vertexOrchestrator": map[string]any{
					"agentEngine": rt.Vertex().ScoutAgentEngine(),
					"mode":        "vertex",
					"error":       err.Error(),
				},
			}
			return &agentmeshruntime.ProcessResult{
				Status:          contracts.TaskStatusDegraded,
				Output:          degraded,
				ArtifactPayload: degraded,
				ReviewOutput:    review,
				ReviewAccepted:  boolPtr(false),
				LastError:       err.Error(),
			}, nil
		}
		if rt != nil && rt.Vertex() != nil && strings.TrimSpace(task.SessionRef) != "" {
			if generated, genErr := rt.Vertex().GenerateAndFetchMemoriesFromSession(ctx, task, scoutMemoryQuery(req, output)); genErr == nil && len(generated) > 0 {
				output["generatedMemoryRefs"] = generated
			} else if genErr != nil && p.logger != nil {
				p.logger.Printf("[agentmesh.scout] memory generation failed task=%s err=%v", task.ID, genErr)
			}
		}
		output["vertexOrchestrator"] = map[string]any{
			"agentEngine": rt.Vertex().ScoutAgentEngine(),
			"mode":        "vertex",
			"cost":        cost,
		}
		review := mapValue(output["review"])
		status, reviewAccepted, rationale := deriveHostedScoutTaskResult(output)
		result := &agentmeshruntime.ProcessResult{
			Status:          status,
			Output:          output,
			ArtifactPayload: output,
			ReviewOutput:    review,
			LastError:       rationale,
		}
		if reviewAccepted != nil {
			result.ReviewAccepted = reviewAccepted
		}
		return result, nil
	}
	seedState := decodeSessionState(input["sessionState"])
	var state *InvestigationState
	if seedState != nil {
		mergedReq := mergeInvestigationRequests(seedState.Request, req)
		mergedReq = withRuntimeFrames(mergedReq, graphCtx)
		seedState.Request = mergedReq
		seedState.Goal = mergeGoalFrames(seedState.Goal, mergedReq.Goal)
		seedState.Environment = mergeEnvironmentFrames(seedState.Environment, mergedReq.Environment)
		seedState.Role = mergeRoleFrames(seedState.Role, mergedReq.Role)
		seedState.Completeness = mergeCompletenessFrames(seedState.Completeness, mergedReq.Completeness)
		seedState.SessionRef = strings.TrimSpace(task.SessionRef)
		if seedState.Plan.MaxResearchRounds <= 0 {
			seedState.Plan.MaxResearchRounds = 2
		}
		if seedState.MaxTurns <= 0 {
			seedState.MaxTurns = max(seedState.Plan.MaxResearchRounds*max(len(seedState.Plan.Branches), 1)+8, 8)
		}
		if seedState.Branches == nil {
			seedState.Branches = map[string]*BranchRuntimeState{}
		}
		if seedState.Hypotheses == nil {
			seedState.Hypotheses = map[string]*InvestigationHypothesis{}
		}
		if len(seedState.BranchOrder) == 0 && len(seedState.Plan.Branches) > 0 {
			for _, branch := range seedState.Plan.Branches {
				branchID := strings.TrimSpace(branch.ID)
				if branchID != "" {
					seedState.BranchOrder = append(seedState.BranchOrder, branchID)
				}
			}
		}
		req = mergedReq
		state = seedState
		p.appendInvestigationEvent(ctx, rt, task, "session_resume", map[string]any{
			"turn":        state.Turn,
			"planVersion": state.Plan.PlanVersion,
			"branchCount": len(state.BranchOrder),
		})
	} else {
		req = withRuntimeFrames(req, graphCtx)
		plan := p.buildPlan(ctx, rt, task, req, graphCtx, nil)
		if err := validateMissionRole(plan); err != nil {
			return &agentmeshruntime.ProcessResult{
				Status:     contracts.TaskStatusFailed,
				RetryClass: contracts.RetryClassPermanent,
				LastError:  err.Error(),
			}, nil
		}
		req.Role = mergeRoleFrames(req.Role, plan.MissionRole)
		if len(plan.Branches) == 0 {
			plan.Branches = fallbackBranches(req, plan)
		}
		plan = ensurePlanRoleAndCompleteness(req, plan)
		state = newInvestigationState(req, plan)
		state.SessionRef = strings.TrimSpace(task.SessionRef)
		appendReasoningBeat(state, explainRequestUnderstanding(req))
		appendReasoningBeat(state, explainEnvironmentUnderstanding(state))
		appendReasoningBeat(state, explainPlanShape(state))
		p.appendInvestigationEvent(ctx, rt, task, "input", map[string]any{
			"request":         req,
			"planningContext": compactPlanningContext(graphCtx),
		})
		p.appendInvestigationEvent(ctx, rt, task, "framing", map[string]any{"planVersion": state.Plan.PlanVersion, "branchCount": len(state.BranchOrder)})
	}
	taskRefs := map[string]any{
		"decide":      []string{},
		"research":    []string{},
		"bridge":      []string{},
		"critic":      []string{},
		"path":        []string{},
		"materialize": []string{},
	}
	p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, "framing", "", "", nil)

	// --- PARALLEL FAN-OUT: run research for all ready branches concurrently in the first round ---
	p.fanOutInitialResearch(ctx, rt, task, leaseOwner, req, graphCtx, state, taskRefs)

	for state.Turn < state.MaxTurns {
		state.Turn++
		state.CurrentBottleneck = string(detectCurrentBottleneck(state))
		decisionTaskID := ""

		// --- INLINE DECISION: if the last child returned a clear recommendedAction, trust it ---
		action, branchID, skipOrchestrate := inferNextActionFromOutcome(state)
		var decision DecisionResult
		if skipOrchestrate {
			scoutReasoningLogf("orchestrate_skipped_model inferred_action=%s branch=%s child_recommended=%s dataSource=%s",
				action, branchID, state.LatestOutcome.RecommendedAction.Action, state.LatestOutcome.DataSourceSignal)
			decision = DecisionResult{
				StateSummary:     summarizeDecisionState(state),
				ChosenAction:     action,
				BranchID:         branchID,
				WhyChosen:        fmt.Sprintf("Inferred from child's recommendedAction=%s (dataSourceSignal=%s); no separate orchestrate call needed.", state.LatestOutcome.RecommendedAction.Action, state.LatestOutcome.DataSourceSignal),
				ExecutedAction:   action,
				ExecutedBranchID: branchID,
				ValidationStatus: "inferred",
				Candidates:       buildDecisionCandidates(state),
			}
			applyDecisionToState(state, decision)
		} else {
			var err error
			decision, action, branchID, err = p.orchestrateNextActionVertex(ctx, rt, task, req, graphCtx, state)
			if err != nil {
				return nil, err
			}
		}
		appendReasoningBeat(state, explainDecisionOutcome(state, decision))
		if action == "stop" {
			appendReasoningBeat(state, explainStopReason(state))
			break
		}
		activeSnapshot := buildActiveFrontierSnapshot(state)
		var childTaskID string
		var childOutput map[string]any
		appliedAuthoritativeState := false
		switch action {
		case "research":
			branchState := state.Branches[branchID]
			if branchState == nil {
				state.StalledTurns++
				p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
				break
			}
			if branchState.ResearchRounds > 0 && shouldCountAsReformulation(state.CurrentBottleneck) {
				branchState.ReformulationCount++
			}
			branchInput := map[string]any{
				"request":          req,
				"goal":             compactGoalFrame(state.Goal),
				"environment":      compactEnvironmentFrame(state.Environment),
				"role":             compactRoleFrame(state.Role),
				"completeness":     compactCompletenessFrame(state.Completeness),
				"plan":             compactPlanForModel(state.Plan),
				"sessionState":     state,
				"branch":           branchState.Branch,
				"planningContext":  compactPlanningContext(graphCtx),
				"activeFrontier":   activeSnapshot,
				"pendingGraphOps":  compactPendingOps(state.PendingGraphOps, branchID),
				"branchIndex":      state.Turn - 1,
				"submittedAt":      nowRFC3339(),
				"turn":             state.Turn,
				"parentSessionRef": strings.TrimSpace(task.SessionRef),
			}
			child, result, runErr := p.runChildTask(ctx, rt, task, "scout.research", fmt.Sprintf("%s:research:%s:%d", task.ID, branchID, branchState.ResearchRounds), branchInput)
			if runErr != nil {
				if handleRetryableChildFailure(state, "research", branchID, runErr) {
					p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
					continue
				}
				return nil, runErr
			}
			childTaskID = child.ID
			childOutput = result
			taskRefs["research"] = append(taskRefs["research"].([]string), child.ID)
			research := decodeResearchResult(result)
			appliedAuthoritativeState = applyAuthoritativeSessionState(state, result["sessionState"])
			if !appliedAuthoritativeState {
				return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
			}
			clearSystemErrorTracking(state)
			researchOutcome := outcomeFromResearch(state, branchID, research)
			if retObj := mapValue(result["retrieval"]); len(retObj) > 0 {
				researchOutcome.RetrievalHitCount = int(floatValue(retObj, "hitCount", 0))
				researchOutcome.ToolCallCount = int(floatValue(retObj, "toolCallCount", 0))
			}
			researchOutcome.DataSourceSignal = resolveDataSourceSignal(research.DataSourceSignal, researchOutcome)
			recordActionOutcome(state, researchOutcome)
			appendReasoningBeat(state, explainResearchOutcome(state, branchID, research))

		case "bridge":
			branchState := state.Branches[branchID]
			if branchState == nil {
				state.StalledTurns++
				p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
				break
			}
			bridgeContext := buildBridgeContext(state, branchID)
			child, result, runErr := p.runChildTask(ctx, rt, task, "scout.bridge", fmt.Sprintf("%s:bridge:%s:%d", task.ID, branchID, state.BridgeAttempts), map[string]any{
				"request":          req,
				"goal":             compactGoalFrame(state.Goal),
				"environment":      compactEnvironmentFrame(state.Environment),
				"role":             compactRoleFrame(state.Role),
				"completeness":     compactCompletenessFrame(state.Completeness),
				"plan":             compactPlanForModel(state.Plan),
				"sessionState":     state,
				"branch":           branchState.Branch,
				"bridgeContext":    bridgeContext,
				"activeFrontier":   activeSnapshot,
				"pendingGraphOps":  compactPendingOps(state.PendingGraphOps, branchID),
				"parentSessionRef": strings.TrimSpace(task.SessionRef),
			})
			if runErr != nil {
				if handleRetryableChildFailure(state, "bridge", branchID, runErr) {
					p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
					continue
				}
				return nil, runErr
			}
			childTaskID = child.ID
			childOutput = result
			taskRefs["bridge"] = append(taskRefs["bridge"].([]string), child.ID)
			bridge := decodeBridgeResult(result)
			appliedAuthoritativeState = applyAuthoritativeSessionState(state, result["sessionState"])
			if !appliedAuthoritativeState {
				return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
			}
			clearSystemErrorTracking(state)
			bridgeOutcome := outcomeFromBridge(state, branchID, bridge)
			if retObj := mapValue(result["retrieval"]); len(retObj) > 0 {
				bridgeOutcome.RetrievalHitCount = int(floatValue(retObj, "hitCount", 0))
				bridgeOutcome.ToolCallCount = int(floatValue(retObj, "toolCallCount", 0))
			}
			if sig := strings.TrimSpace(bridge.DataSourceSignal); sig != "" {
				bridgeOutcome.DataSourceSignal = sig
			}
			recordActionOutcome(state, bridgeOutcome)
			appendReasoningBeat(state, explainBridgeOutcome(state, branchID, bridge))
		case "critic":
			child, result, runErr := p.runChildTask(ctx, rt, task, "scout.critic", fmt.Sprintf("%s:critic:%d", task.ID, state.Turn), map[string]any{
				"request":          req,
				"goal":             compactGoalFrame(state.Goal),
				"environment":      compactEnvironmentFrame(state.Environment),
				"role":             compactRoleFrame(state.Role),
				"completeness":     compactCompletenessFrame(state.Completeness),
				"plan":             compactPlanForModel(state.Plan),
				"sessionState":     state,
				"researchDeltas":   compactResearchDeltas(state.ResearchResults, activeSnapshot.TopBranches),
				"activeFrontier":   activeSnapshot,
				"pendingGraphOps":  state.PendingGraphOps,
				"parentSessionRef": strings.TrimSpace(task.SessionRef),
			})
			if runErr != nil {
				if handleRetryableChildFailure(state, "critic", branchID, runErr) {
					p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
					continue
				}
				return nil, runErr
			}
			childTaskID = child.ID
			childOutput = result
			taskRefs["critic"] = append(taskRefs["critic"].([]string), child.ID)
			critic := decodeCriticResult(result)
			appliedSession := applyAuthoritativeSessionState(state, result["sessionState"])
			if !appliedSession {
				return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
			}
			appliedAuthoritativeState = appliedAuthoritativeState || appliedSession
			clearSystemErrorTracking(state)
			recordActionOutcome(state, outcomeFromCritic(state, branchID, critic))
			appendReasoningBeat(state, explainCriticOutcome(state, critic))
		case "path":
			pathScope := selectPathScope(state)
			child, result, runErr := p.runChildTask(ctx, rt, task, "scout.path", fmt.Sprintf("%s:path:%d", task.ID, state.PathAttempts), map[string]any{
				"request":                  req,
				"goal":                     compactGoalFrame(state.Goal),
				"environment":              compactEnvironmentFrame(state.Environment),
				"role":                     compactRoleFrame(state.Role),
				"completeness":             compactCompletenessFrame(state.Completeness),
				"plan":                     compactPlanForModel(state.Plan),
				"sessionState":             state,
				"researchDeltas":           compactResearchDeltas(state.ResearchResults, activeSnapshot.TopBranches),
				"critic":                   state.LatestCritic,
				"activeFrontier":           activeSnapshot,
				"pathContextBranchIds":     pathScope.SupportingBranchIDs,
				"pathTargetBranchIds":      pathScope.TargetBranchIDs,
				"pathScopeProducts":        pathScope.ProductScope,
				"pathScopeSelectionReason": pathScope.SelectionReason,
				"parentSessionRef":         strings.TrimSpace(task.SessionRef),
			})
			if runErr != nil {
				if handleRetryableChildFailure(state, "path", branchID, runErr) {
					p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
					continue
				}
				return nil, runErr
			}
			childTaskID = child.ID
			childOutput = result
			taskRefs["path"] = append(taskRefs["path"].([]string), child.ID)
			pathResult := decodePathResult(result)
			appliedSession := applyAuthoritativeSessionState(state, result["sessionState"])
			if !appliedSession {
				return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
			}
			appliedAuthoritativeState = appliedAuthoritativeState || appliedSession
			clearSystemErrorTracking(state)
			recordActionOutcome(state, outcomeFromPath(state, pathResult))
			appendReasoningBeat(state, explainPathOutcome(state, pathResult))
		case "materialize":
			child, result, runErr := p.runChildTask(ctx, rt, task, "scout.materialize", fmt.Sprintf("%s:materialize:%d", task.ID, state.MaterializeAttempts), map[string]any{
				"request":          req,
				"goal":             compactGoalFrame(state.Goal),
				"environment":      compactEnvironmentFrame(state.Environment),
				"role":             compactRoleFrame(state.Role),
				"completeness":     compactCompletenessFrame(state.Completeness),
				"plan":             compactPlanForModel(state.Plan),
				"sessionState":     state,
				"critic":           state.LatestCritic,
				"path":             state.PathResult,
				"activeFrontier":   activeSnapshot,
				"state":            compactInvestigationSnapshot(state),
				"parentSessionRef": strings.TrimSpace(task.SessionRef),
			})
			if runErr != nil {
				if handleRetryableChildFailure(state, "materialize", branchID, runErr) {
					p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, "", nil)
					continue
				}
				return nil, runErr
			}
			childTaskID = child.ID
			childOutput = result
			taskRefs["materialize"] = append(taskRefs["materialize"].([]string), child.ID)
			materialize := decodeMaterializeResult(result)
			appliedSession := applyAuthoritativeSessionState(state, result["sessionState"])
			if !appliedSession {
				return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
			}
			appliedAuthoritativeState = appliedAuthoritativeState || appliedSession
			clearSystemErrorTracking(state)
			recordActionOutcome(state, outcomeFromMaterialize(state, materialize))
			appendReasoningBeat(state, explainMaterializeOutcome(state, materialize))
		case "replan":
			p.applyReplan(ctx, rt, task, req, graphCtx, state)
			recordActionOutcome(state, outcomeFromReplan(state))
			appendReasoningBeat(state, explainReplanOutcome(state))
		}
		p.appendInvestigationEvent(ctx, rt, task, "turn", map[string]any{
			"turn": state.Turn, "action": action, "branchId": branchID, "stopReason": state.StopReason, "decisionTaskId": decisionTaskID,
		})
		if childOutput != nil && requiresAuthoritativeSessionStateForAction(action) && !appliedAuthoritativeState {
			return nil, fmt.Errorf("invalid child output: action %s did not return sessionState", action)
		}
		p.appendInvestigationEvent(ctx, rt, task, "state_snapshot", compactCheckpointState(state))
		p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, action, branchID, childTaskID, childOutput)
		compactStateForTurn(state, action)
	}

	aggregateTask, aggregateOutput, err := p.runChildTask(ctx, rt, task, "scout.aggregate", task.ID+":aggregate", map[string]any{
		"request":          req,
		"goal":             compactGoalFrame(state.Goal),
		"environment":      compactEnvironmentFrame(state.Environment),
		"role":             compactRoleFrame(state.Role),
		"completeness":     compactCompletenessFrame(state.Completeness),
		"plan":             compactPlanForModel(state.Plan),
		"sessionState":     state,
		"researchDeltas":   compactResearchDeltas(state.ResearchResults, buildActiveFrontierSnapshot(state).TopBranches),
		"bridge":           state.LatestBridge,
		"critic":           state.LatestCritic,
		"path":             state.PathResult,
		"materialize":      state.MaterializeResult,
		"state":            compactInvestigationSnapshot(state),
		"activeFrontier":   buildActiveFrontierSnapshot(state),
		"parentSessionRef": strings.TrimSpace(task.SessionRef),
	})
	if err != nil {
		return nil, err
	}
	aggregateResult := decodeAggregateResult(aggregateOutput)

	reviewTask, reviewOutput, err := p.runChildTask(ctx, rt, task, "scout.review", task.ID+":review", map[string]any{
		"request":          req,
		"goal":             compactGoalFrame(state.Goal),
		"environment":      compactEnvironmentFrame(state.Environment),
		"role":             compactRoleFrame(state.Role),
		"completeness":     compactCompletenessFrame(state.Completeness),
		"sessionState":     state,
		"aggregate":        aggregateResult,
		"bridge":           state.LatestBridge,
		"critic":           state.LatestCritic,
		"path":             state.PathResult,
		"state":            compactInvestigationSnapshot(state),
		"activeFrontier":   buildActiveFrontierSnapshot(state),
		"parentSessionRef": strings.TrimSpace(task.SessionRef),
	})
	if err != nil {
		return nil, err
	}
	reviewResult := decodeReviewResult(reviewOutput, aggregateResult, state.PathResult)
	appendReasoningBeat(state, explainFinalReview(state, aggregateResult, reviewResult))
	summary := summarizeReasoningTrace(state.ReasoningTrace)

	output := map[string]any{
		"request":          req,
		"plan":             state.Plan,
		"research":         state.ResearchResults,
		"critic":           state.LatestCritic,
		"path":             state.PathResult,
		"materialize":      state.MaterializeResult,
		"aggregate":        aggregateResult,
		"review":           reviewResult,
		"state":            state,
		"reasoningTrace":   state.ReasoningTrace,
		"runtimeDiagnosis": state.RuntimeDiagnosis,
		"reasoningSummary": summary,
		"sessionRef":       strings.TrimSpace(task.SessionRef),
		"taskRefs": map[string]any{
			"decide":      taskRefs["decide"],
			"research":    taskRefs["research"],
			"bridge":      taskRefs["bridge"],
			"critic":      taskRefs["critic"],
			"path":        taskRefs["path"],
			"materialize": taskRefs["materialize"],
			"aggregate":   aggregateTask.ID,
			"review":      reviewTask.ID,
		},
	}

	if err := p.persistFinalResult(ctx, req, job, output, reviewResult); err != nil && p.logger != nil {
		p.logger.Printf("[agentmesh.scout] final persist failed job=%s err=%v", req.ScoutingJobID, err)
	}
	if rt != nil && rt.Vertex() != nil && strings.TrimSpace(task.SessionRef) != "" {
		if generated, err := rt.Vertex().GenerateAndFetchMemoriesFromSession(ctx, task, scoutMemoryQuery(req, output)); err == nil && len(generated) > 0 {
			output["generatedMemoryRefs"] = generated
		}
	}

	status := contracts.TaskStatusSucceeded
	if !reviewResult.Accepted {
		status = contracts.TaskStatusDegraded
	}
	return &agentmeshruntime.ProcessResult{
		Status:          status,
		Output:          output,
		ArtifactPayload: output,
		ReviewOutput:    reviewMap(reviewResult),
		ReviewAccepted:  boolPtr(reviewResult.Accepted),
		LastError:       strings.TrimSpace(reviewResult.Rationale),
	}, nil
}

func (p *Processor) processResearch(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	inputRaw := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(inputRaw, "scout.research")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(inputRaw["request"])
	branch := decodeBranch(inputRaw["branch"])
	plan := decodePlan(inputRaw["plan"])
	planningContext := mapValue(inputRaw["planningContext"])
	goal := compactGoalFrame(req.Goal)
	if raw := mapValue(inputRaw["goal"]); len(raw) > 0 {
		goal = raw
	}
	environment := compactEnvironmentFrame(req.Environment)
	if raw := mapValue(inputRaw["environment"]); len(raw) > 0 {
		environment = raw
	}
	role := compactRoleFrame(req.Role)
	if raw := mapValue(inputRaw["role"]); len(raw) > 0 {
		role = raw
	}
	completeness := compactCompletenessFrame(req.Completeness)
	if raw := mapValue(inputRaw["completeness"]); len(raw) > 0 {
		completeness = raw
	}

	input := enrichScoutModelInput(map[string]any{
		"request":         req,
		"goal":            goal,
		"environment":     environment,
		"role":            role,
		"completeness":    completeness,
		"branch":          branch,
		"plan":            plan,
		"sessionState":    inputRaw["sessionState"],
		"planningContext": planningContext,
		"activeFrontier":  inputRaw["activeFrontier"],
		"pendingGraphOps": inputRaw["pendingGraphOps"],
		"toolInstructions": "You have 4 tools: search_knowledge (internal KB), web_search (Google), resolve_product (canonical path), lookup_transform (known relations). " +
			"Use them iteratively: check internal KB first, then web if needed, resolve products before proposing graph ops. " +
			"When done investigating, return your final JSON with all required fields. " +
			"You MUST include a dataSourceSignal field: internal_grounded, internal_partial, internal_empty, or no_data.",
	}, task)
	parsed, toolLogs := p.runModelWithTools(ctx, rt, task, input, req.TenantID)
	if runtimeErr := modelRuntimeError(parsed); runtimeErr != "" {
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: modelRetryClass(runtimeErr),
			LastError:  runtimeErr,
		}, nil
	}
	if len(parsed) == 0 {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.research returned empty or invalid JSON object",
		}, nil
	}
	delta := decodeResearchDelta(parsed)
	if isEmptyResearchDelta(delta) {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.research returned schema-empty payload",
		}, nil
	}
	research := mergeResearchDeltaIntoResult(ResearchResult{}, delta)
	research.BranchID = firstNonEmpty(branch.ID, research.BranchID)
	research.RecommendedPathProducts = compactStrings(append(research.RecommendedPathProducts, research.ProductRefs...))
	research.GapDiagnosis = normalizeGapDiagnosis(research.GapDiagnosis)
	research.RecommendedAction = normalizeActionRecommendation(research.RecommendedAction)
	research.GapDiagnosis = fillGapDiagnosisFromResearch(branch, research)
	research.RecommendedAction = fillRecommendedActionFromGapDiagnosis(research.RecommendedAction, research.GapDiagnosis)
	totalToolCalls, totalToolHits := toolCallStats(toolLogs)
	if scoutReasoningLogEnabled() {
		scoutReasoningLogf("research_branch_done task=%s branch=%s tools=%d hits=%d confidence=%.2f next_action=%s gap=%s",
			task.ID, research.BranchID, totalToolCalls, totalToolHits, research.Confidence,
			research.RecommendedAction.Action, scoutReasoningSingleLine(research.GapDiagnosis.Summary, 280))
	}
	if totalToolHits == 0 && len(compactStrings(research.EvidencePoints)) == 0 && len(compactStrings(append(research.CanonicalRefs, research.ProductRefs...))) == 0 {
		if research.Confidence == 0 || research.Confidence > 0.3 {
			research.Confidence = 0.3
		}
		research.GapDiagnosis.Kind = firstNonEmpty(research.GapDiagnosis.Kind, "missing_substrate")
		research.GapDiagnosis.NextAction = firstNonEmpty(research.GapDiagnosis.NextAction, "bridge")
		research.GapDiagnosis.Summary = firstNonEmpty(research.GapDiagnosis.Summary, "Tools returned no useful evidence. The knowledge base and web search did not yield data for this question.")
		research.GapDiagnosis.Why = firstNonEmpty(research.GapDiagnosis.Why, "Zero evidence from all sources means repeating will not help. Switch strategy or stop.")
		sessionState.ConsecutiveZeroHitTurns++
	} else {
		sessionState.ConsecutiveZeroHitTurns = 0
		sessionState.TotalRetrievalHits += totalToolHits
	}
	if research.RecommendedAction.BranchID == "" && (research.RecommendedAction.Action == "research" || research.RecommendedAction.Action == "bridge") {
		research.RecommendedAction.BranchID = research.BranchID
	}
	if research.Confidence == 0 {
		research.Confidence = 0.55
	}
	patch := GraphPatch{
		BranchID:   research.BranchID,
		SourceStep: "research",
		Turn:       sessionState.Turn,
		Ops:        research.ProposedOps,
		Summary:    research.Summary,
		Confidence: research.Confidence,
	}
	sessionState.Patches = append(sessionState.Patches, patch)
	output := researchMap(research)
	output["stateDelta"] = map[string]any{
		"kind":              "research",
		"branchId":          research.BranchID,
		"gapDiagnosis":      compactGapDiagnosis(research.GapDiagnosis),
		"recommendedAction": compactActionRecommendation(research.RecommendedAction),
		"openQuestions":     research.OpenQuestions,
		"confidence":        research.Confidence,
		"retrievalHitCount": totalToolHits,
		"toolCallCount":     totalToolCalls,
	}
	output["retrieval"] = map[string]any{
		"mode":          "tool_calling",
		"toolCallCount": totalToolCalls,
		"totalHits":     totalToolHits,
		"toolCallLog":   toolLogs,
	}
	applyResearchToState(sessionState, research.BranchID, research)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func (p *Processor) processBridge(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	inputRaw := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(inputRaw, "scout.bridge")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(inputRaw["request"])
	branch := decodeBranch(inputRaw["branch"])
	plan := decodePlan(inputRaw["plan"])
	bridgeContext := mapValue(inputRaw["bridgeContext"])
	goal := compactGoalFrame(req.Goal)
	if raw := mapValue(inputRaw["goal"]); len(raw) > 0 {
		goal = raw
	}
	environment := compactEnvironmentFrame(req.Environment)
	if raw := mapValue(inputRaw["environment"]); len(raw) > 0 {
		environment = raw
	}
	role := compactRoleFrame(req.Role)
	if raw := mapValue(inputRaw["role"]); len(raw) > 0 {
		role = raw
	}
	completeness := compactCompletenessFrame(req.Completeness)
	if raw := mapValue(inputRaw["completeness"]); len(raw) > 0 {
		completeness = raw
	}

	input := enrichScoutModelInput(map[string]any{
		"request":         req,
		"goal":            goal,
		"environment":     environment,
		"role":            role,
		"completeness":    completeness,
		"branch":          branch,
		"plan":            plan,
		"sessionState":    inputRaw["sessionState"],
		"bridgeContext":   bridgeContext,
		"activeFrontier":  inputRaw["activeFrontier"],
		"pendingGraphOps": inputRaw["pendingGraphOps"],
		"toolInstructions": "You have 4 tools: search_knowledge (internal KB), web_search (Google), resolve_product (canonical path), lookup_transform (known relations). " +
			"Use them to find what sits between endpoint A and B. Check internal KB first, then web for missing data. " +
			"Resolve products and verify transforms before proposing graph ops. " +
			"When done, return your final JSON with all required bridge fields. " +
			"You MUST include a dataSourceSignal field: external_only, internal_grounded, internal_partial, or no_data.",
	}, task)
	parsed, toolLogs := p.runModelWithTools(ctx, rt, task, input, req.TenantID)
	if runtimeErr := modelRuntimeError(parsed); runtimeErr != "" {
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: modelRetryClass(runtimeErr),
			LastError:  runtimeErr,
		}, nil
	}
	if len(parsed) == 0 {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.bridge returned empty or invalid JSON object",
		}, nil
	}
	bridge := decodeBridgeResult(parsed)
	if isEmptyBridgeResult(bridge) {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.bridge returned schema-empty payload",
		}, nil
	}
	bridge.BranchID = firstNonEmpty(branch.ID, bridge.BranchID)
	bridgeQuery := firstNonEmpty(stringValue(bridgeContext, "bridgeQuestion"), strings.TrimSpace(branch.Question), strings.TrimSpace(req.Query))
	bridge.BridgeQuestion = firstNonEmpty(bridge.BridgeQuestion, bridgeQuery)
	bridge.BridgeSteps = normalizeBridgeSteps(bridge.BridgeSteps)
	bridge.GapDiagnosis = normalizeGapDiagnosis(bridge.GapDiagnosis)
	bridge.RecommendedAction = normalizeActionRecommendation(bridge.RecommendedAction)
	bridge.GapDiagnosis = fillGapDiagnosisFromBridge(branch, bridge)
	bridge.RecommendedAction = fillRecommendedActionFromGapDiagnosis(bridge.RecommendedAction, bridge.GapDiagnosis)
	if bridge.RecommendedAction.BranchID == "" && (bridge.RecommendedAction.Action == "research" || bridge.RecommendedAction.Action == "bridge") {
		bridge.RecommendedAction.BranchID = bridge.BranchID
	}
	if len(bridge.IntermediateNodes) == 0 {
		bridge.IntermediateNodes = inferIntermediateNodesFromBridgeSteps(bridge.BridgeSteps)
	}
	if len(bridge.IntermediateRelationTypes) == 0 {
		bridge.IntermediateRelationTypes = inferRelationTypesFromBridgeSteps(bridge.BridgeSteps)
	}
	if bridge.Confidence == 0 {
		bridge.Confidence = ternaryBridgeConfidence(bridge.BridgeExists)
	}
	if len(bridge.ProposedOps) == 0 {
		bridge.ProposedOps = synthesizeBridgeOps(bridge)
	} else {
		bridge.ProposedOps = mergeBridgeOps(bridge.ProposedOps, synthesizeBridgeOps(bridge))
	}
	if len(bridge.SuggestedSubscans) == 0 {
		bridge.SuggestedSubscans = synthesizeBridgeSubscans(branch, bridge)
	}
	for i := range bridge.ProposedOps {
		if bridge.ProposedOps[i].BranchID == "" {
			bridge.ProposedOps[i].BranchID = bridge.BranchID
		}
		if bridge.ProposedOps[i].Confidence == 0 {
			bridge.ProposedOps[i].Confidence = computeGraphOpConfidence(bridge.ProposedOps[i])
		}
	}
	totalToolCalls, totalToolHits := toolCallStats(toolLogs)
	patch := GraphPatch{
		BranchID:   bridge.BranchID,
		SourceStep: "bridge",
		Turn:       sessionState.Turn,
		Ops:        bridge.ProposedOps,
		Summary:    bridge.Summary,
		Confidence: bridge.Confidence,
	}
	sessionState.Patches = append(sessionState.Patches, patch)
	output := bridgeMap(bridge)
	output["stateDelta"] = map[string]any{
		"kind":              "bridge",
		"branchId":          bridge.BranchID,
		"gapDiagnosis":      compactGapDiagnosis(bridge.GapDiagnosis),
		"recommendedAction": compactActionRecommendation(bridge.RecommendedAction),
		"openQuestions":     bridge.OpenQuestions,
		"confidence":        bridge.Confidence,
		"retrievalHitCount": totalToolHits,
		"toolCallCount":     totalToolCalls,
	}
	output["retrieval"] = map[string]any{
		"mode":          "tool_calling",
		"toolCallCount": totalToolCalls,
		"totalHits":     totalToolHits,
		"toolCallLog":   toolLogs,
	}
	applyBridgeToState(sessionState, bridge.BranchID, bridge)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func (p *Processor) processCritic(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(input, "scout.critic")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(input["request"])
	plan := decodePlan(input["plan"])
	research := decodeResearchResults(input["research"])
	if len(research) == 0 {
		research = decodeResearchDeltas(input["researchDeltas"])
	}

	modelIn := enrichScoutModelInput(map[string]any{
		"request":        req,
		"goal":           compactGoalFrame(req.Goal),
		"environment":    compactEnvironmentFrame(req.Environment),
		"role":           compactRoleFrame(req.Role),
		"completeness":   compactCompletenessFrame(req.Completeness),
		"plan":           plan,
		"sessionState":   input["sessionState"],
		"researchDeltas": encodeResearchDeltas(research),
		"activeFrontier": input["activeFrontier"],
	}, task)
	parsed := p.runModelObject(ctx, rt, task, modelIn)
	if runtimeErr := modelRuntimeError(parsed); runtimeErr != "" {
		return &agentmeshruntime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: modelRetryClass(runtimeErr),
			LastError:  runtimeErr,
		}, nil
	}
	if len(parsed) == 0 {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.critic returned empty or invalid JSON object",
		}, nil
	}
	delta := decodeCriticDelta(parsed)
	if isEmptyCriticDelta(delta) {
		return &agentmeshruntime.ProcessResult{
			Status:    contracts.TaskStatusFailed,
			LastError: "scout.critic returned schema-empty payload",
		}, nil
	}
	critic := mergeCriticDelta(CriticResult{}, delta)
	critic.GapDiagnosis = normalizeGapDiagnosis(critic.GapDiagnosis)
	critic.RecommendedAction = normalizeActionRecommendation(critic.RecommendedAction)
	critic.GapDiagnosis = fillGapDiagnosisFromCritic(plan, critic)
	critic.RecommendedAction = fillRecommendedActionFromGapDiagnosis(critic.RecommendedAction, critic.GapDiagnosis)
	output := criticMap(critic)
	output["stateDelta"] = map[string]any{
		"kind":                    "critic",
		"gapDiagnosis":            compactGapDiagnosis(critic.GapDiagnosis),
		"recommendedAction":       compactActionRecommendation(critic.RecommendedAction),
		"withholdPath":            critic.WithholdPath,
		"withholdMaterialization": critic.WithholdMaterialization,
		"killBranchIds":           critic.KillBranchIDs,
		"reprioritizeBranchIds":   critic.ReprioritizeBranchIDs,
		"riskTargetBranchIds":     critic.RiskTargetBranchIDs,
		"openQuestions":           critic.OpenQuestions,
		"goodEnoughAssessment":    compactGoodEnoughAssessment(critic.GoodEnoughAssessment),
		"completenessAssessment":  map[string]any{"status": critic.CompletenessAssessment.Status, "reason": critic.CompletenessAssessment.Reason, "broadScanCompleted": critic.CompletenessAssessment.BroadScanCompleted, "noMaterialAlternatives": critic.CompletenessAssessment.NoMaterialAlternatives},
	}
	applyCriticToState(sessionState, critic)
	verdicts := critic.GraphOpVerdicts
	if len(verdicts) == 0 {
		verdicts = synthesizeGraphOpVerdicts(sessionState, critic)
	}
	applyGraphOpVerdicts(sessionState, verdicts)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func (p *Processor) processPath(ctx context.Context, _ *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(input, "scout.path")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(input["request"])
	plan := decodePlan(input["plan"])
	research := decodeResearchResults(input["research"])
	if len(research) == 0 {
		research = decodeResearchDeltas(input["researchDeltas"])
	}
	critic := decodeCriticResult(input["critic"])
	allowPathWrites := req.AllowWrites && !critic.WithholdMaterialization
	targetBranchIDs := compactStrings(stringSliceValue(input["pathTargetBranchIds"]))
	contextBranchIDs := compactStrings(stringSliceValue(input["pathContextBranchIds"]))
	scopeProducts := compactStrings(stringSliceValue(input["pathScopeProducts"]))

	const (
		perProductTraceTimeout = 90 * time.Second
		assemblyBuildTimeout   = 2 * time.Minute
	)

	scopedResearch := research
	if len(targetBranchIDs) > 0 {
		scopedResearch = filterResearchResultsByBranches(research, targetBranchIDs)
	}
	products := []string{}
	if len(scopeProducts) > 0 {
		products = append(products, scopeProducts...)
		if len(products) < 2 {
			products = append(products, critic.PathProducts...)
		}
	} else {
		products = append(products, critic.PathProducts...)
		products = append(products, plan.FocusProducts...)
		products = append(products, collectResearchPathProducts(scopedResearch)...)
	}
	products = compactStrings(products)
	products = p.resolveProductRefs(ctx, req.TenantID, products)
	direction := inferTraceDirection(req.Query)
	traceDigests := make([]map[string]any, 0, len(products))
	artifactIDs := make([]string, 0, len(products))
	openGaps := make([]string, 0, len(products))
	rawEdges := make([]pathbuilder.SupplyChainAssemblyRawEdge, 0, 16)
	pathHypotheses := make([]PathHypothesis, 0, len(products))

	for idx, productRef := range products {
		traceCtx, cancelTrace := context.WithTimeout(ctx, perProductTraceTimeout)
		trace, err := p.queryEngine.TraceByProductFast(traceCtx, productRef, pathbuilder.SupplyChainQueryOptions{
			TenantID:      req.TenantID,
			IncludeGlobal: true,
			MaxDepth:      6,
			MaxPaths:      8,
			MaxLinks:      5000,
		})
		cancelTrace()
		if err != nil {
			openGaps = append(openGaps, fmt.Sprintf("%s: %v", productRef, err))
			pathHypotheses = append(pathHypotheses, PathHypothesis{ProductRef: productRef, BindingStatus: "error", GapSummary: err.Error()})
			continue
		}
		digest := buildTraceDigest(productRef, trace)
		traceDigests = append(traceDigests, digest)
		bindingStatus := "bound"
		gapSummary := ""
		pathGapCount := 0
		for _, path := range trace.Paths {
			openGaps = append(openGaps, append([]string{}, path.OpenGaps...)...)
			pathGapCount += len(path.OpenGaps)
		}
		if len(trace.Paths) == 0 {
			bindingStatus = "unbound"
		} else if pathGapCount > 0 {
			bindingStatus = "partial"
			g0 := compactStrings(trace.Paths[0].OpenGaps)
			if len(g0) > 3 {
				g0 = g0[:3]
			}
			gapSummary = strings.Join(g0, "; ")
		}
		pathHypotheses = append(pathHypotheses, PathHypothesis{ProductRef: productRef, BindingStatus: bindingStatus, GapSummary: gapSummary})
		if allowPathWrites && p.pathBuilder != nil && len(trace.Paths) > 0 {
			keys := make([]string, 0, len(trace.Paths))
			for pathIdx := range trace.Paths {
				keys = append(keys, fmt.Sprintf("/agentic-scout/%s/path/%s/%d", firstNonEmpty(req.TenantID, "global"), makeDeterministicKey(task.ID, productRef, fmt.Sprintf("%d", pathIdx)), pathIdx))
			}
			writeCtx, cancelWrite := context.WithTimeout(ctx, perProductTraceTimeout)
			ids, err := p.pathBuilder.MaterializePathsBatch(writeCtx, trace.Paths, firstNonEmpty(req.TenantID, "global"), req.LensID, keys)
			cancelWrite()
			if err == nil {
				artifactIDs = append(artifactIDs, compactStrings(ids)...)
			} else {
				openGaps = append(openGaps, fmt.Sprintf("%s materialize: %v", productRef, err))
			}
		}
		if p.productDictionary != nil {
			expansionCtx, cancelExpansion := context.WithTimeout(ctx, perProductTraceTimeout)
			expansion, err := p.productDictionary.ExpandProductStreamForTenant(expansionCtx, req.TenantID, productRef, direction, 4, 6)
			cancelExpansion()
			if err == nil && expansion != nil {
				for _, edge := range expansion.Edges {
					rawEdges = append(rawEdges, pathbuilder.SupplyChainAssemblyRawEdge{
						Source:   edge.FromPath,
						Target:   edge.ToPath,
						EdgeType: strings.ToUpper(edge.RelationKind),
					})
				}
				if len(expansion.Edges) > 0 && len(traceDigests) > idx {
					traceDigests[idx]["transformEdges"] = len(expansion.Edges)
				}
			}
		}
	}

	assemblyArtifactID := ""
	if allowPathWrites && p.assemblyBuilder != nil && len(products) > 0 {
		assemblyCtx, cancelAssembly := context.WithTimeout(ctx, assemblyBuildTimeout)
		assembly, err := p.assemblyBuilder.BuildAndMaterialize(assemblyCtx, pathbuilder.SupplyChainAssemblyOptions{
			TenantID:   firstNonEmpty(req.TenantID, "global"),
			ProjectID:  req.ProjectID,
			LensID:     req.LensID,
			AnchorSet:  products,
			GeoFilters: compactStrings(append([]string{}, critic.FocusGeos...)),
			RawEdges:   rawEdges,
			MaxDepth:   6,
			MaxPaths:   8,
			MaxLinks:   5000,
		})
		cancelAssembly()
		if err == nil && assembly != nil && assembly.Artifact != nil {
			assemblyArtifactID = strings.TrimSpace(assembly.Artifact.ID)
			openGaps = append(openGaps, assembly.OpenGaps...)
		} else if err != nil {
			openGaps = append(openGaps, "assembly: "+err.Error())
		}
	}

	continuity := "landed"
	switch {
	case len(products) == 0:
		continuity = "unknown"
	case len(openGaps) > 0 && len(artifactIDs) == 0 && strings.TrimSpace(assemblyArtifactID) == "":
		continuity = "broken"
	case len(openGaps) > 0:
		continuity = "partial"
	}

	result := PathResult{
		QueriedProducts:         products,
		TraceDigests:            traceDigests,
		MaterializedArtifactIDs: compactStrings(artifactIDs),
		AssemblyArtifactID:      strings.TrimSpace(assemblyArtifactID),
		OpenGaps:                compactStrings(openGaps),
		Confidence:              clamp01(0.4 + 0.1*float64(min(len(products), 4)) + 0.08*float64(min(len(traceDigests), 4))),
		ContinuityStatus:        continuity,
		PathHypotheses:          pathHypotheses,
		SourceBranchIDs:         compactStrings(ternarySlice(len(targetBranchIDs) > 0, targetBranchIDs, contextBranchIDs)),
	}
	result.GapDiagnosis = derivePathGapDiagnosis(result)
	result.RecommendedAction = derivePathRecommendedAction(result)
	result.RecommendedAction = fillRecommendedActionFromGapDiagnosis(result.RecommendedAction, result.GapDiagnosis)
	output := pathMap(result)
	output["stateDelta"] = map[string]any{
		"kind":              "path",
		"gapDiagnosis":      compactGapDiagnosis(result.GapDiagnosis),
		"recommendedAction": compactActionRecommendation(result.RecommendedAction),
		"continuityStatus":  result.ContinuityStatus,
		"openGaps":          result.OpenGaps,
		"sourceBranchIds":   result.SourceBranchIDs,
	}
	applyPathToState(sessionState, result)
	markVerifiedOpsFromPath(sessionState, result)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func (p *Processor) processMaterialize(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	sessionState, invalid := requireSessionStateInput(input, "scout.materialize")
	if invalid != nil {
		return invalid, nil
	}
	req := decodeRequest(input["request"])
	path := decodePathResult(input["path"])
	critic := decodeCriticResult(input["critic"])
	plan := decodePlan(input["plan"])
	created := compactStrings(append([]string{}, path.MaterializedArtifactIDs...))
	if strings.TrimSpace(path.AssemblyArtifactID) != "" {
		created = compactStrings(append(created, path.AssemblyArtifactID))
	}
	skipped := []string{}
	downgraded := []string{}
	rejected := []string{}
	summary := "comparison only"
	persisted := false
	allowMat := req.AllowWrites && !critic.WithholdMaterialization
	if allowMat {
		persisted = len(created) > 0
		if persisted {
			summary = fmt.Sprintf("progressive materialization created %d artifacts", len(created))
		} else {
			summary = "no artifacts created; unresolved path gaps"
		}
	} else if len(path.OpenGaps) > 0 {
		skipped = compactStrings(append(skipped, path.OpenGaps...))
	}
	if critic.WithholdMaterialization {
		skipped = compactStrings(append(skipped, "withheld_by_critic"))
	}
	if len(path.OpenGaps) > 0 {
		downgraded = compactStrings(append(downgraded, path.OpenGaps...))
	}
	if !allowMat && len(created) > 0 {
		rejected = append(rejected, "writes_disabled_or_withheld")
	}
	result := MaterializeResult{
		ArtifactIDs: created,
		Persisted:   persisted,
		Summary:     summary,
		Created:     created,
		Updated:     nil,
		Skipped:     compactStrings(skipped),
		Downgraded:  downgraded,
		Rejected:    compactStrings(rejected),
	}
	parsed := p.runModelObject(ctx, rt, task, enrichScoutModelInput(map[string]any{
		"request":            req,
		"goal":               compactGoalFrame(req.Goal),
		"environment":        compactEnvironmentFrame(req.Environment),
		"role":               compactRoleFrame(req.Role),
		"completeness":       compactCompletenessFrame(req.Completeness),
		"plan":               plan,
		"sessionState":       input["sessionState"],
		"critic":             critic,
		"path":               path,
		"materializePreview": result,
		"activeFrontier":     input["activeFrontier"],
	}, task))
	if parsed == nil {
		parsed = map[string]any{}
	}
	if len(parsed) > 0 {
		result = mergeMaterializeDelta(result, decodeMaterializeDelta(parsed))
	}
	if allowMat && p.artifacts != nil {
		for _, obj := range mapSlice(parsed["corrections"]) {
			rec := p.applyOneMaterialCorrection(ctx, req.TenantID, obj)
			result.CorrectionsApplied = append(result.CorrectionsApplied, rec)
		}
		for _, rec := range result.CorrectionsApplied {
			if rec.Applied && (rec.Action == "replace_existing" || rec.Action == "deprecate_existing" || rec.Action == "contradict_existing") {
				result.Updated = compactStrings(append(result.Updated, rec.ArtifactID))
			}
		}
	}

	output := materializeMap(result)
	output["stateDelta"] = map[string]any{
		"kind":              "materialize",
		"summary":           result.Summary,
		"persisted":         result.Persisted,
		"landedBranchIds":   result.LandedBranchIDs,
		"rejectedBranchIds": result.RejectedBranchIDs,
		"skipped":           result.Skipped,
		"downgraded":        result.Downgraded,
		"rejected":          result.Rejected,
	}
	applyMaterializeToState(sessionState, result)
	markLandedOps(sessionState, result)
	output["sessionState"] = sessionState
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func mergeMaterializeDelta(base MaterializeResult, delta MaterializeDelta) MaterializeResult {
	base.Summary = firstNonEmpty(strings.TrimSpace(delta.Summary), base.Summary)
	base.Skipped = compactStrings(append(base.Skipped, delta.Skipped...))
	base.Downgraded = compactStrings(append(base.Downgraded, delta.Downgraded...))
	base.Rejected = compactStrings(append(base.Rejected, delta.Rejected...))
	base.LandedBranchIDs = compactStrings(append(base.LandedBranchIDs, delta.LandedBranchIDs...))
	base.RejectedBranchIDs = compactStrings(append(base.RejectedBranchIDs, delta.RejectedBranchIDs...))
	return base
}

func (p *Processor) applyOneMaterialCorrection(ctx context.Context, tenantID string, obj map[string]any) MaterializeCorrectionRecord {
	action := strings.ToLower(strings.TrimSpace(stringValue(obj, "action")))
	id := strings.TrimSpace(stringValue(obj, "artifactId"))
	rationale := stringValue(obj, "rationale")
	rec := MaterializeCorrectionRecord{Action: action, ArtifactID: id, Rationale: rationale}
	if p == nil || p.artifacts == nil || id == "" || action == "" {
		return rec
	}
	art, err := p.artifacts.Get(ctx, id)
	if err != nil {
		if errors.Is(err, horizonrepo.ErrMaterializedArtifactNotFound) {
			rec.Error = "artifact_not_found"
			return rec
		}
		rec.Error = err.Error()
		return rec
	}
	if tenantID != "" && art.TenantID != "" && art.TenantID != tenantID && !strings.EqualFold(tenantID, "global") {
		rec.Error = "tenant_mismatch"
		return rec
	}
	switch action {
	case "deprecate_existing":
		art.Status = "deprecated"
		art.Labels = mergeStringMaps(art.Labels, map[string]string{"agenticScoutCorrection": rationale})
		if err := p.artifacts.Update(ctx, art); err != nil {
			rec.Error = err.Error()
			return rec
		}
		rec.Applied = true
	case "contradict_existing":
		art.PathStatus = "hypothesis"
		art.Labels = mergeStringMaps(art.Labels, map[string]string{"agenticScoutContradicted": "true"})
		if err := p.artifacts.Update(ctx, art); err != nil {
			rec.Error = err.Error()
			return rec
		}
		rec.Applied = true
	case "replace_existing":
		if ns := stringValue(obj, "newPathStatus"); ns != "" {
			art.PathStatus = ns
		}
		if raw, ok := obj["patchLabels"].(map[string]any); ok {
			patch := map[string]string{}
			for k, v := range raw {
				if s, ok := v.(string); ok {
					patch[k] = s
				}
			}
			art.Labels = mergeStringMaps(art.Labels, patch)
		}
		if err := p.artifacts.Update(ctx, art); err != nil {
			rec.Error = err.Error()
			return rec
		}
		rec.Applied = true
	default:
		rec.Error = "unknown_action"
	}
	return rec
}

func (p *Processor) processAggregate(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	req := decodeRequest(input["request"])
	plan := decodePlan(input["plan"])
	research := decodeResearchResults(input["research"])
	if len(research) == 0 {
		research = decodeResearchDeltas(input["researchDeltas"])
	}
	critic := decodeCriticResult(input["critic"])
	path := decodePathResult(input["path"])
	materialize := decodeMaterializeResult(input["materialize"])

	aggregate := fallbackAggregate(req, plan, research, critic, path, materialize)
	modelIn := enrichScoutModelInput(input, task)
	if parsed := p.runModelObject(ctx, rt, task, modelIn); len(parsed) > 0 {
		aggregate = mergeAggregateResult(aggregate, parsed)
	}
	output := aggregateMap(aggregate)
	return &agentmeshruntime.ProcessResult{
		Status:          contracts.TaskStatusSucceeded,
		Output:          output,
		ArtifactPayload: output,
	}, nil
}

func (p *Processor) processReview(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask) (*agentmeshruntime.ProcessResult, error) {
	input := modelTaskInput(task)
	aggregate := decodeAggregateResult(input["aggregate"])
	path := decodePathResult(input["path"])
	review := decodeReviewResult(nil, aggregate, path)
	modelIn := enrichScoutModelInput(input, task)
	if parsed := p.runModelObject(ctx, rt, task, modelIn); len(parsed) > 0 {
		review = mergeReviewResult(review, parsed)
	}
	output := reviewMap(review)
	status := contracts.TaskStatusSucceeded
	if !review.Accepted {
		status = contracts.TaskStatusDegraded
	}
	return &agentmeshruntime.ProcessResult{
		Status:          status,
		Output:          output,
		ArtifactPayload: output,
		ReviewOutput:    output,
		ReviewAccepted:  boolPtr(review.Accepted),
		LastError:       strings.TrimSpace(review.Rationale),
	}, nil
}

func (p *Processor) resolveRequest(ctx context.Context, task *contracts.MeshTask) (InvestigationRequest, *horizonmodels.ScoutingJob, error) {
	input := modelTaskInput(task)
	req := normalizeRequest(decodeRequest(input))
	var job *horizonmodels.ScoutingJob
	if req.ScoutingJobID != "" && p.scoutingJobs != nil {
		loaded, err := p.scoutingJobs.Get(ctx, req.ScoutingJobID)
		if err != nil {
			return req, nil, err
		}
		job = loaded
		if req.TenantID == "" && job != nil {
			req.TenantID = strings.TrimSpace(job.TenantID)
		}
		if req.ProjectID == "" && job != nil {
			req.ProjectID = strings.TrimSpace(job.ProjectID)
		}
		if req.LensID == "" && job != nil {
			req.LensID = strings.TrimSpace(job.LensID)
		}
		if req.Query == "" && job != nil {
			req.Query = strings.TrimSpace(job.UserQuery)
			if req.Query == "" && job.State != nil {
				if raw, ok := job.State["originalQuery"].(string); ok {
					req.Query = strings.TrimSpace(raw)
				}
			}
		}
		if req.TargetCanonicalPath == "" && job != nil {
			req.TargetCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
		}
		if !req.AllowWrites && job != nil && job.State != nil {
			if raw, ok := job.State["allowAgenticWrites"].(bool); ok {
				req.AllowWrites = raw
			}
		}
	}
	if req.TenantID == "" {
		req.TenantID = strings.TrimSpace(task.TenantID)
	}
	if req.Query == "" && req.TargetCanonicalPath == "" {
		return req, job, fmt.Errorf("agentic scout request requires query or targetCanonicalPath")
	}
	return req, job, nil
}

func (p *Processor) buildPlanningContext(ctx context.Context, req InvestigationRequest) map[string]any {
	out := map[string]any{
		"query":               req.Query,
		"targetCanonicalPath": req.TargetCanonicalPath,
	}
	if strings.TrimSpace(req.TargetCanonicalPath) == "" || p.fragments == nil {
		return out
	}
	fragment, err := p.fragments.GetByCanonicalPath(ctx, req.TargetCanonicalPath)
	if err != nil || fragment == nil {
		return out
	}
	out["targetFragment"] = map[string]any{
		"id":            strings.TrimSpace(fragment.ID),
		"canonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		"class":         string(fragment.Class),
		"subtype":       strings.TrimSpace(fragment.Subtype),
		"label":         firstNonEmpty(fragment.Labels["en"], fragment.Identifiers["shortName"]),
	}
	if p.edges != nil && strings.TrimSpace(fragment.ID) != "" {
		outgoing, _ := p.edges.ListBySource(ctx, req.TenantID, fragment.ID, nil, 12)
		incoming, _ := p.edges.ListByTarget(ctx, req.TenantID, fragment.ID, nil, 12)
		out["outgoingEdges"] = summarizeEdges(outgoing)
		out["incomingEdges"] = summarizeEdges(incoming)
	}
	return out
}

func (p *Processor) buildPlan(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, req InvestigationRequest, planningContext map[string]any, prior *InvestigationState) InvestigationPlan {
	planQuery := firstNonEmpty(req.Query, req.TargetCanonicalPath)
	knowledgeHits := maybeSearchKnowledgeBase(ctx, p.knowledgeBase, req.TenantID, planQuery, 6)
	input := map[string]any{
		"request":         req,
		"goal":            compactGoalFrame(req.Goal),
		"environment":     compactEnvironmentFrame(req.Environment),
		"role":            compactRoleFrame(req.Role),
		"completeness":    compactCompletenessFrame(req.Completeness),
		"planningContext": planningContext,
		"knowledgeHits":   knowledgeHits,
		"retrieval": map[string]any{
			"mode":     "internal_datastore_retrieval",
			"source":   "vertex_ai_search",
			"query":    planQuery,
			"hitCount": len(knowledgeHits),
			"hits":     knowledgeHits,
		},
	}
	if prior != nil {
		input["priorPlan"] = prior.Plan
		input["sessionState"] = prior
		input["replanReason"] = strings.TrimSpace(prior.ReplanReason)
	}
	input = enrichScoutModelInput(input, task)
	plan := fallbackPlan(req)
	planTask := task
	if task != nil && strings.TrimSpace(task.Capability) != "scout.plan" {
		copyTask := *task
		copyTask.Capability = "scout.plan"
		planTask = &copyTask
	}
	if parsed := p.runModelObject(ctx, rt, planTask, input); len(parsed) > 0 {
		plan = mergePlan(plan, parsed)
	}
	plan.MissionRole = mergeRoleFrames(req.Role, plan.MissionRole)
	if len(plan.FocusProducts) > 0 {
		plan.FocusProducts = p.resolveProductRefs(ctx, req.TenantID, plan.FocusProducts)
	}
	if plan.MaxResearchRounds <= 0 {
		plan.MaxResearchRounds = 2
	}
	if plan.PlanVersion <= 0 {
		plan.PlanVersion = 1
	}
	plan.Branches = deduplicateBranches(plan.Branches)
	if scoutReasoningLogEnabled() {
		scoutReasoningLogf("plan_ready goal=%s maxRounds=%d branch_count=%d rationale_lines=%d",
			scoutReasoningSingleLine(plan.Goal, 320), plan.MaxResearchRounds, len(plan.Branches), len(plan.PlanningRationale))
		for _, b := range plan.Branches {
			scoutReasoningLogf("plan_branch id=%s broadScan=%v question=%s hints_products=%v hints_geos=%v",
				strings.TrimSpace(b.ID), b.BroadScan, scoutReasoningSingleLine(b.Question, 360), b.ProductHints, b.GeoHints)
		}
	}
	return plan
}

// deduplicateBranches merges branches whose questions, product hints, and geo hints
// overlap significantly. The planner often generates "broad_scan" + a more specific branch
// that asks the same question in different words. Dedup keeps the more specific one and
// absorbs the other's hints.
func deduplicateBranches(branches []InvestigationBranch) []InvestigationBranch {
	if len(branches) <= 1 {
		return branches
	}
	type normalizedBranch struct {
		branch   InvestigationBranch
		words    map[string]bool
		products map[string]bool
		geos     map[string]bool
	}
	normalize := func(b InvestigationBranch) normalizedBranch {
		words := map[string]bool{}
		for _, w := range strings.Fields(strings.ToLower(b.Question)) {
			if len(w) > 3 { // skip articles, prepositions
				words[w] = true
			}
		}
		products := map[string]bool{}
		for _, p := range b.ProductHints {
			products[strings.ToLower(strings.TrimSpace(p))] = true
		}
		geos := map[string]bool{}
		for _, g := range b.GeoHints {
			geos[strings.ToLower(strings.TrimSpace(g))] = true
		}
		return normalizedBranch{branch: b, words: words, products: products, geos: geos}
	}
	overlapRatio := func(a, b map[string]bool) float64 {
		if len(a) == 0 && len(b) == 0 {
			return 0
		}
		common := 0
		for k := range a {
			if b[k] {
				common++
			}
		}
		return float64(common) / float64(max(len(a), len(b)))
	}

	normalized := make([]normalizedBranch, len(branches))
	for i, b := range branches {
		normalized[i] = normalize(b)
	}
	dropped := make([]bool, len(branches))
	for i := 0; i < len(normalized); i++ {
		if dropped[i] {
			continue
		}
		for j := i + 1; j < len(normalized); j++ {
			if dropped[j] {
				continue
			}
			wordOverlap := overlapRatio(normalized[i].words, normalized[j].words)
			if wordOverlap < 0.5 {
				continue
			}
			// High word overlap — merge the less specific one into the more specific
			keep, drop := i, j
			if len(normalized[j].words) > len(normalized[i].words) {
				keep, drop = j, i
			}
			// Absorb hints from the dropped branch
			for _, p := range normalized[drop].branch.ProductHints {
				normalized[keep].branch.ProductHints = append(normalized[keep].branch.ProductHints, p)
			}
			for _, g := range normalized[drop].branch.GeoHints {
				normalized[keep].branch.GeoHints = append(normalized[keep].branch.GeoHints, g)
			}
			for _, a := range normalized[drop].branch.ActorHints {
				normalized[keep].branch.ActorHints = append(normalized[keep].branch.ActorHints, a)
			}
			normalized[keep].branch.ProductHints = compactStrings(normalized[keep].branch.ProductHints)
			normalized[keep].branch.GeoHints = compactStrings(normalized[keep].branch.GeoHints)
			normalized[keep].branch.ActorHints = compactStrings(normalized[keep].branch.ActorHints)
			dropped[drop] = true
		}
	}
	out := make([]InvestigationBranch, 0, len(branches))
	for i, nb := range normalized {
		if !dropped[i] {
			out = append(out, nb.branch)
		}
	}
	return out
}

func newInvestigationState(req InvestigationRequest, plan InvestigationPlan) *InvestigationState {
	req = normalizeRequest(req)
	goal := req.Goal
	if strings.TrimSpace(goal.Objective) == "" {
		goal = buildGoalFrame(req)
	}
	environment := req.Environment
	if strings.TrimSpace(environment.WorkspaceKind) == "" {
		environment = buildEnvironmentFrame(req, nil)
	}
	role := req.Role
	role = mergeRoleFrames(role, plan.MissionRole)
	completeness := req.Completeness
	if strings.TrimSpace(completeness.ExplorationStatus) == "" && !completeness.BroadScanRequired {
		completeness = buildCompletenessFrame(req)
	}
	req.Goal = goal
	req.Environment = environment
	req.Role = role
	req.Completeness = completeness
	state := &InvestigationState{
		Request:                 req,
		Goal:                    goal,
		Environment:             environment,
		Role:                    role,
		Completeness:            completeness,
		Plan:                    plan,
		MaxTurns:                max(plan.MaxResearchRounds*max(len(plan.Branches), 1)+8, 8),
		OpenQuestions:           []string{},
		StructuredOpenQuestions: []OpenQuestionEntry{},
		BranchOrder:             []string{},
		Branches:                map[string]*BranchRuntimeState{},
		Hypotheses:              map[string]*InvestigationHypothesis{},
		ResearchResults:         []ResearchResult{},
		ActionLog:               []string{},
		PlanVersion:             max(plan.PlanVersion, 1),
	}
	if state.Plan.PlanVersion < 1 {
		state.Plan.PlanVersion = 1
	}
	for idx, branch := range plan.Branches {
		branchID := firstNonEmpty(branch.ID, fmt.Sprintf("branch-%d", idx+1))
		branch.ID = branchID
		state.BranchOrder = append(state.BranchOrder, branchID)
		state.Branches[branchID] = &BranchRuntimeState{
			Branch:       branch,
			Status:       BranchStatusProposed,
			History:      []BranchHistoryEntry{},
			Dependencies: []string{},
		}
		state.Hypotheses[branchID] = &InvestigationHypothesis{
			ID:               "hypothesis:" + branchID,
			BranchID:         branchID,
			Statement:        firstNonEmpty(branch.Question, plan.Goal),
			Status:           HypothesisProposed,
			ExpectedInfoGain: branch.ExpectedInfoGain,
		}
	}
	for _, dep := range plan.BranchDependencies {
		if child := state.Branches[dep.ToBranchID]; child != nil && strings.TrimSpace(dep.FromBranchID) != "" {
			child.Branch.DependsOn = compactStrings(append(child.Branch.DependsOn, dep.FromBranchID))
		}
	}
	return state
}

func chooseNextAction(state *InvestigationState) (string, string) {
	if state == nil {
		return "stop", ""
	}
	if state.StalledTurns >= 2 || state.Turn >= state.MaxTurns {
		state.StopReason = firstNonEmpty(state.StopReason, "bounded_iteration_stop")
		return "stop", ""
	}
	if state.ConsecutiveZeroGainTurns >= 3 {
		state.StopReason = firstNonEmpty(state.StopReason, "no_information_gain")
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "information_gain_exhausted",
			Severity: "info",
			Message:  fmt.Sprintf("Stopping: %d consecutive turns produced no new information (qualityDelta=none). Further queries are unlikely to help.", state.ConsecutiveZeroGainTurns),
		})
		return "stop", ""
	}
	if state.ConsecutiveZeroHitTurns >= 2 && state.TotalRetrievalHits == 0 {
		state.StopReason = firstNonEmpty(state.StopReason, "no_data_available")
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "knowledge_base_empty",
			Severity: "info",
			Message:  fmt.Sprintf("Stopping: %d consecutive retrieval turns with 0 hits and no total hits in this investigation. The knowledge base lacks data for this topic.", state.ConsecutiveZeroHitTurns),
		})
		return "stop", ""
	}
	if id := selectBroadScanBranch(state); id != "" {
		return "research", id
	}
	if shouldPersistGoodEnoughPath(state) {
		state.CurrentBottleneck = string(BottleneckPathContinuityGap)
		return "path", ""
	}
	if action, branchID := selectDiagnosedAction(state); action != "" {
		state.CurrentBottleneck = currentBottleneckFromDiagnosis(state.LatestGapDiagnosis, action)
		return action, branchID
	}
	if action, branchID := selectRecommendedAction(state); action != "" {
		return action, branchID
	}
	if hasLocallyReadyPathCandidate(state) && shouldAttemptPath(state) && !shouldAttemptBridgeScan(state) {
		return "path", ""
	}
	if id := selectBridgeBranch(state); id != "" && shouldAttemptBridgeScan(state) {
		return "bridge", id
	}
	if state.NeedsReplan {
		if id := selectBridgeBranch(state); id != "" && shouldAttemptBridgeScan(state) {
			return "bridge", id
		}
		if id := selectActionableResearchBranch(state); id != "" {
			return "research", id
		}
		return "replan", ""
	}

	// Graph-ops-driven stop: if we have done at least one research round and
	// all graph ops are resolved (landed/rejected/verified) with no remaining
	// research branches, stop.
	if len(state.ResearchResults) > 0 && len(state.PendingGraphOps) > 0 && !hasPendingUnresolvedOps(state) {
		allBranchesDone := true
		for _, id := range state.BranchOrder {
			b := state.Branches[id]
			if b != nil && branchNeedsMoreResearch(state, b) {
				allBranchesDone = false
				break
			}
		}
		if allBranchesDone && completenessAllowsStop(state) {
			state.StopReason = firstNonEmpty(state.StopReason, "all_graph_ops_resolved")
			return "stop", ""
		}
	}

	bottleneck := detectCurrentBottleneck(state)
	state.CurrentBottleneck = string(bottleneck)
	switch bottleneck {
	case BottleneckMaterializationHold:
		if shouldRunCritic(state) {
			return "critic", ""
		}
		if shouldMaterialize(state) && shouldPrioritizeMaterialize(state) {
			return "materialize", ""
		}
	case BottleneckMissingMiddle:
		if id := selectBridgeBranch(state); id != "" && shouldAttemptBridgeScan(state) {
			return "bridge", id
		}
		if shouldAttemptPath(state) {
			return "path", ""
		}
		if shouldRunCritic(state) {
			return "critic", ""
		}
	case BottleneckPathContinuityGap, BottleneckTargetBindingGap:
		if id := selectBridgeBranch(state); id != "" && shouldAttemptBridgeScan(state) {
			return "bridge", id
		}
		if shouldAttemptPath(state) {
			return "path", ""
		}
		if shouldRunCritic(state) {
			return "critic", ""
		}
	case BottleneckMissingSubstrate, BottleneckLowConfidenceClaim:
		if id := selectResearchBranchByScore(state); id != "" {
			return "research", id
		}
		if shouldRunCritic(state) {
			return "critic", ""
		}
	case BottleneckUnresolvedConflict:
		if shouldRunCritic(state) {
			return "critic", ""
		}
		if id := selectResearchBranchByScore(state); id != "" {
			return "research", id
		}
	case BottleneckNoHighValueAction:
		if !completenessAllowsStop(state) {
			if id := selectActionableResearchBranch(state); id != "" {
				return "research", id
			}
			if shouldRunCritic(state) {
				return "critic", ""
			}
		}
		state.StopReason = firstNonEmpty(state.StopReason, "no_high_value_action")
		return "stop", ""
	}
	// fallback discipline: avoid broad branch sweeps.
	if id := selectResearchBranchByScore(state); id != "" {
		return "research", id
	}
	if shouldAttemptPath(state) && shouldPrioritizePath(state) {
		return "path", ""
	}
	if shouldMaterialize(state) && shouldPrioritizeMaterialize(state) {
		return "materialize", ""
	}
	if !completenessAllowsStop(state) && shouldRunCritic(state) {
		return "critic", ""
	}
	state.StopReason = firstNonEmpty(state.StopReason, "no_action_available")
	return "stop", ""
}

// orchestrateNextActionVertex makes Vertex the primary decision core for next action
// selection. Host-side chooseNextAction remains deterministic fallback only.
func (p *Processor) orchestrateNextActionVertex(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, req InvestigationRequest, graphCtx map[string]any, state *InvestigationState) (DecisionResult, string, string, error) {
	candidates := buildDecisionCandidates(state)
	fallbackAction, fallbackBranchID := chooseNextAction(state)
	fallbackDecision := buildFallbackDecision(state, fallbackAction, fallbackBranchID, candidates)
	if state == nil || task == nil {
		applyDecisionToState(state, fallbackDecision)
		return fallbackDecision, fallbackAction, fallbackBranchID, nil
	}
	activeSnapshot := buildActiveFrontierSnapshot(state)
	modelIn := enrichScoutModelInput(map[string]any{
		"request":          req,
		"goal":             compactGoalFrame(state.Goal),
		"environment":      compactEnvironmentFrame(state.Environment),
		"role":             compactRoleFrame(state.Role),
		"completeness":     compactCompletenessFrame(state.Completeness),
		"plan":             compactPlanForModel(state.Plan),
		"planningContext":  compactPlanningContext(graphCtx),
		"sessionState":     state,
		"latestOutcome":    compactActionOutcome(state.LatestOutcome),
		"recentOutcomes":   compactActionOutcomes(state.Outcomes),
		"activeFrontier":   activeSnapshot,
		"candidateActions": compactDecisionCandidates(candidates),
		"decisionContext":  buildDecisionContext(state),
		"parentSessionRef": strings.TrimSpace(task.SessionRef),
	}, task)
	parsed := p.runModelObject(ctx, rt, task, modelIn)
	if runtimeErr := modelRuntimeError(parsed); runtimeErr != "" {
		scoutReasoningLogf("orchestrate fallback reason=model_error using=%s branch=%s err=%s", fallbackAction, fallbackBranchID, scoutReasoningSingleLine(runtimeErr, 200))
		fallbackDecision.WhyChosen = "Deterministic fallback: scout.orchestrate model decision failed."
		fallbackDecision.ValidationStatus = "fallback"
		fallbackDecision.ValidationReason = runtimeErr
		applyDecisionToState(state, fallbackDecision)
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "orchestrate_model_failed",
			Severity: "warning",
			Message:  runtimeErr,
		})
		return fallbackDecision, fallbackAction, fallbackBranchID, nil
	}
	if len(parsed) == 0 {
		scoutReasoningLogf("orchestrate fallback reason=empty_payload using=%s branch=%s", fallbackAction, fallbackBranchID)
		fallbackDecision.WhyChosen = "Deterministic fallback: scout.orchestrate returned empty payload."
		fallbackDecision.ValidationStatus = "fallback"
		fallbackDecision.ValidationReason = "empty decision payload"
		applyDecisionToState(state, fallbackDecision)
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "orchestrate_payload_empty",
			Severity: "warning",
			Message:  "scout.orchestrate returned an empty decision payload",
		})
		return fallbackDecision, fallbackAction, fallbackBranchID, nil
	}
	decision := decodeDecisionResult(parsed)
	decision = normalizeDecisionResult(decision)
	if isEmptyDecisionResult(decision) {
		scoutReasoningLogf("orchestrate fallback reason=schema_empty using=%s branch=%s", fallbackAction, fallbackBranchID)
		fallbackDecision.WhyChosen = "Deterministic fallback: scout.orchestrate returned schema-empty payload."
		fallbackDecision.ValidationStatus = "fallback"
		fallbackDecision.ValidationReason = "schema-empty decision payload"
		applyDecisionToState(state, fallbackDecision)
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "orchestrate_payload_empty",
			Severity: "warning",
			Message:  "scout.orchestrate returned schema-empty decision payload",
		})
		return fallbackDecision, fallbackAction, fallbackBranchID, nil
	}
	decision.StateSummary = firstNonEmpty(decision.StateSummary, summarizeDecisionState(state))
	decision.Candidates = candidates
	action, branchID, validationStatus, validationReason := validateDecisionChoice(decision, candidates)
	if action == "" {
		action = fallbackAction
		branchID = fallbackBranchID
		validationStatus = "fallback"
		validationReason = firstNonEmpty(validationReason, "orchestrate_decision_did_not_resolve_to_a_feasible_candidate")
	}
	decision.ExecutedAction = action
	decision.ExecutedBranchID = branchID
	decision.ValidationStatus = validationStatus
	decision.ValidationReason = validationReason
	if validationStatus == "fallback" {
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
			Code:     "orchestrate_decision_fallback",
			Severity: "warning",
			Message:  validationReason,
		})
	}
	populateDecisionAlternatives(&decision, candidates)
	applyDecisionToState(state, decision)
	scoutReasoningLogf("orchestrate decision action=%s branch=%s chosen=%s validation=%s why=%s state=%s",
		decision.ExecutedAction, decision.ExecutedBranchID, decision.ChosenAction,
		decision.ValidationStatus,
		scoutReasoningSingleLine(decision.WhyChosen, 420),
		scoutReasoningSingleLine(decision.StateSummary, 260),
	)
	return decision, action, branchID, nil
}

// fanOutInitialResearch runs research for all ready branches concurrently in the first round.
// This avoids the sequential decide→research→decide→research pattern and produces initial
// data in parallel, just as a human analyst would issue multiple searches at once.
func (p *Processor) fanOutInitialResearch(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string, req InvestigationRequest, graphCtx map[string]any, state *InvestigationState, taskRefs map[string]any) {
	if state == nil || len(state.BranchOrder) == 0 {
		return
	}
	var readyBranches []string
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		if branch == nil {
			continue
		}
		if !branchDependenciesSatisfied(state, branch) {
			continue
		}
		if branch.ResearchRounds > 0 {
			continue
		}
		readyBranches = append(readyBranches, branchID)
	}
	if len(readyBranches) <= 1 {
		return // only one or zero branches — the normal loop handles this fine
	}

	if scoutReasoningLogEnabled() {
		scoutReasoningLogf("fanout_research_parallel count=%d parent_task=%s", len(readyBranches), task.ID)
		for _, bid := range readyBranches {
			q := ""
			if bs := state.Branches[bid]; bs != nil {
				q = bs.Branch.Question
			}
			scoutReasoningLogf("fanout_research_start branch=%s question=%s", bid, scoutReasoningSingleLine(q, 360))
		}
	}

	type fanOutResult struct {
		branchID string
		child    *contracts.MeshTask
		result   map[string]any
		err      error
	}

	results := make([]fanOutResult, len(readyBranches))
	activeSnapshot := buildActiveFrontierSnapshot(state)

	var wg sync.WaitGroup
	for i, branchID := range readyBranches {
		wg.Add(1)
		go func(idx int, bid string) {
			defer wg.Done()
			branchState := state.Branches[bid]
			if branchState == nil {
				return
			}
			child, result, err := p.runChildTask(ctx, rt, task, "scout.research",
				fmt.Sprintf("%s:research:%s:%d", task.ID, bid, 0),
				map[string]any{
					"request":          req,
					"goal":             compactGoalFrame(state.Goal),
					"environment":      compactEnvironmentFrame(state.Environment),
					"role":             compactRoleFrame(state.Role),
					"completeness":     compactCompletenessFrame(state.Completeness),
					"plan":             compactPlanForModel(state.Plan),
					"sessionState":     state,
					"branch":           branchState.Branch,
					"planningContext":  compactPlanningContext(graphCtx),
					"activeFrontier":   activeSnapshot,
					"pendingGraphOps":  compactPendingOps(state.PendingGraphOps, bid),
					"branchIndex":      idx,
					"submittedAt":      nowRFC3339(),
					"turn":             state.Turn + idx + 1,
					"parentSessionRef": strings.TrimSpace(task.SessionRef),
				})
			results[idx] = fanOutResult{branchID: bid, child: child, result: result, err: err}
		}(i, branchID)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			handleRetryableChildFailure(state, "research", r.branchID, r.err)
			continue
		}
		if r.child == nil {
			continue
		}
		state.Turn++
		taskRefs["research"] = append(taskRefs["research"].([]string), r.child.ID)
		research := decodeResearchResult(r.result)
		applyAuthoritativeSessionState(state, r.result["sessionState"])
		researchOutcome := outcomeFromResearch(state, r.branchID, research)
		if retObj := mapValue(r.result["retrieval"]); len(retObj) > 0 {
			researchOutcome.RetrievalHitCount = int(floatValue(retObj, "hitCount", 0))
			researchOutcome.ToolCallCount = int(floatValue(retObj, "toolCallCount", 0))
		}
		researchOutcome.DataSourceSignal = resolveDataSourceSignal(research.DataSourceSignal, researchOutcome)
		recordActionOutcome(state, researchOutcome)
		appendReasoningBeat(state, explainResearchOutcome(state, r.branchID, research))
	}
	p.persistCheckpoint(ctx, rt, task, leaseOwner, req, state, taskRefs, "fan_out_research", "", "", nil)
}

// inferNextActionFromOutcome checks whether the last child returned a clear, unambiguous
// recommendedAction. If so, the orchestrator model call is skipped — the child model that
// actually saw the data is trusted to know what should happen next.
// Returns (action, branchID, shouldSkipOrchestrate).
func inferNextActionFromOutcome(state *InvestigationState) (string, string, bool) {
	if state == nil || state.Turn <= 1 {
		return "", "", false
	}
	outcome := normalizeActionOutcome(state.LatestOutcome)
	rec := normalizeActionRecommendation(outcome.RecommendedAction)
	if rec.Action == "" {
		return "", "", false
	}
	signal := strings.TrimSpace(outcome.DataSourceSignal)

	// Strong signals where the child's recommendation is unambiguous:
	switch {
	// research returned nothing from internal KB → child says bridge → trust it
	case outcome.Action == "research" && signal == "internal_empty" && rec.Action == "bridge":
		branchID := firstNonEmpty(rec.BranchID, outcome.BranchID)
		return "bridge", branchID, true

	// research returned nothing at all → child says stop → trust it
	case outcome.Action == "research" && (signal == "internal_empty" || signal == "no_data") && rec.Action == "stop":
		return "stop", "", true

	// bridge produced material result → child says path or materialize → trust it
	case outcome.Action == "bridge" && outcome.QualityDelta == "material" && (rec.Action == "path" || rec.Action == "materialize"):
		return rec.Action, firstNonEmpty(rec.BranchID, outcome.BranchID), true

	// any action says stop with good-enough assessment → trust it
	case rec.Action == "stop" && outcome.GoodEnoughAssessment.StopNow:
		return "stop", "", true
	}

	return "", "", false
}

// buildDecisionContext constructs the machine-readable context block sent to the orchestrator model.
// It carries runtime counters and stop criteria. The model reasons about tool selection from
// candidate dataSourceHints and outcome dataSourceSignals — no lookup table needed.
func buildDecisionContext(state *InvestigationState) map[string]any {
	latestSignal := ""
	if state != nil {
		latestSignal = strings.TrimSpace(state.LatestOutcome.DataSourceSignal)
	}
	ctx := map[string]any{
		"runtimePurpose":           "Populate and improve the graph with useful chain/path structure; do not polish like a courtroom brief.",
		"consecutiveZeroGainTurns": 0,
		"consecutiveZeroHitTurns":  0,
		"totalRetrievalHits":       0,
		"latestDataSourceSignal":   latestSignal,
		"stopCriteria": []map[string]any{
			{"criterion": "current_best_chain_is_useful_for_graph_fill", "description": "Stop or persist when the current chain is already structurally useful enough for graph/path purposes."},
			{"criterion": "materially_different_routes_have_been_scanned_enough", "description": "Do not stop too early if materially different alternatives remain unexamined."},
			{"criterion": "remaining_gaps_are_non_blocking_or_low_value", "description": "If remaining gaps are not worth this runtime tier, say so explicitly."},
			{"criterion": "next_tool_call_is_likely_to_improve_quality", "description": "If another turn is chosen, explain why it is still likely to improve graph quality materially."},
			{"criterion": "information_gain_is_positive", "description": "If the last 2+ turns produced no new information (qualityDelta=none), the system MUST stop or radically change strategy."},
		},
	}
	if state != nil {
		ctx["consecutiveZeroGainTurns"] = state.ConsecutiveZeroGainTurns
		ctx["consecutiveZeroHitTurns"] = state.ConsecutiveZeroHitTurns
		ctx["totalRetrievalHits"] = state.TotalRetrievalHits
		ctx["informationGainWarning"] = informationGainWarning(state)
	}
	return ctx
}

func buildFallbackDecision(state *InvestigationState, action, branchID string, candidates []DecisionCandidate) DecisionResult {
	decision := DecisionResult{
		StateSummary:     summarizeDecisionState(state),
		ChosenAction:     action,
		BranchID:         branchID,
		WhyChosen:        explainDerivedDecisionWhy(state, action, branchID),
		Candidates:       candidates,
		ExecutedAction:   action,
		ExecutedBranchID: branchID,
		ValidationStatus: "fallback",
		ValidationReason: "deterministic_selector",
		StopCriteria:     evaluateStopCriteria(state),
		EvidenceUsed:     decisionEvidenceUsed(state),
	}
	populateDecisionAlternatives(&decision, candidates)
	return decision
}

func explainDerivedDecisionWhy(state *InvestigationState, action, branchID string) string {
	if state == nil {
		return explainDecisionWhy(nil, action, branchID)
	}
	if recAction, recBranchID := selectRecommendedAction(state); recAction == action && firstNonEmpty(recBranchID, branchID) == firstNonEmpty(branchID, recBranchID) {
		rec := normalizeActionRecommendation(state.LatestRecommendedAction)
		return firstNonEmpty(rec.Reason, explainDecisionWhy(state, action, branchID))
	}
	if diagAction, diagBranchID := selectDiagnosedAction(state); diagAction == action && firstNonEmpty(diagBranchID, branchID) == firstNonEmpty(branchID, diagBranchID) {
		diag := normalizeGapDiagnosis(state.LatestGapDiagnosis)
		return firstNonEmpty(diag.Why, diag.Summary, explainDecisionWhy(state, action, branchID))
	}
	if action == "path" && shouldPersistGoodEnoughPath(state) {
		item := normalizeGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment)
		return firstNonEmpty(item.Reason, "Current chain is already good enough for this runtime tier; persist or verify it instead of polishing further.")
	}
	return explainDecisionWhy(state, action, branchID)
}

func populateDecisionAlternatives(decision *DecisionResult, candidates []DecisionCandidate) {
	if decision == nil {
		return
	}
	decision.WhyNotStop = decisionAlternativeReason(decision, candidates, "stop")
	decision.WhyNotPath = decisionAlternativeReason(decision, candidates, "path")
	decision.WhyNotBridge = decisionAlternativeReason(decision, candidates, "bridge")
	decision.WhyNotResearch = decisionAlternativeReason(decision, candidates, "research")
	decision.WhyNotCritic = decisionAlternativeReason(decision, candidates, "critic")
	decision.WhyNotMaterialize = decisionAlternativeReason(decision, candidates, "materialize")
	decision.WhyNotReplan = decisionAlternativeReason(decision, candidates, "replan")
}

func decisionAlternativeReason(decision *DecisionResult, candidates []DecisionCandidate, action string) string {
	if decision == nil {
		return ""
	}
	if decision.ExecutedAction == action || decision.ChosenAction == action {
		return strings.Title(action) + " is the chosen action."
	}
	for _, candidate := range candidates {
		if candidate.Action != action {
			continue
		}
		if candidate.Available {
			return firstNonEmpty(candidate.Reason, "Another action was judged higher value right now.")
		}
		return firstNonEmpty(candidate.Reason, "This action is not currently feasible.")
	}
	return "This action was not a valid candidate in the current state."
}

func decisionEvidenceUsed(state *InvestigationState) []string {
	if state == nil {
		return nil
	}
	out := []string{}
	if summary := strings.TrimSpace(state.LatestOutcome.Summary); summary != "" {
		out = append(out, "latest_outcome:"+summary)
	}
	if diag := normalizeGapDiagnosis(state.LatestGapDiagnosis); diag.Kind != "" {
		out = append(out, "gap:"+diag.Kind)
	}
	if rec := normalizeActionRecommendation(state.LatestRecommendedAction); rec.Action != "" {
		out = append(out, "recommended:"+rec.Action)
	}
	if strings.TrimSpace(state.CurrentBottleneck) != "" {
		out = append(out, "bottleneck:"+strings.TrimSpace(state.CurrentBottleneck))
	}
	return compactStrings(out)
}

func evaluateStopCriteria(state *InvestigationState) []StopCriterionEvaluation {
	if state == nil {
		return nil
	}
	currentChainUseful := shouldPersistGoodEnoughPath(state) || goodEnoughAllowsStop(state)
	alternativesScanned := completenessAllowsStop(state)
	remainingLowValue := false
	if item := normalizeGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment); item.Status != "" {
		remainingLowValue = item.FurtherWorkLowValue
	}
	nextLikelyImproves := !goodEnoughAllowsStop(state) && detectCurrentBottleneck(state) != BottleneckNoHighValueAction && state.ConsecutiveZeroGainTurns < 2
	infoGainPositive := state.ConsecutiveZeroGainTurns < 2
	dataAvailable := state.ConsecutiveZeroHitTurns < 2 || state.TotalRetrievalHits > 0
	return []StopCriterionEvaluation{
		{
			Criterion: "current_best_chain_is_useful_for_graph_fill",
			Satisfied: currentChainUseful,
			Why:       ternary(currentChainUseful, "Current chain is already structurally useful enough to persist or answer the question at this runtime tier.", "Current chain still lacks enough structural value for graph fill or business answer."),
		},
		{
			Criterion: "materially_different_routes_have_been_scanned_enough",
			Satisfied: alternativesScanned,
			Why:       ternary(alternativesScanned, "Broad scan and route completeness are sufficient for this tier.", "Materially different routes may still remain unexamined."),
		},
		{
			Criterion: "remaining_gaps_are_non_blocking_or_low_value",
			Satisfied: remainingLowValue,
			Why:       ternary(remainingLowValue, "Remaining gaps are low-value or non-blocking for this runtime tier.", "Remaining gaps are still blocking or potentially high-value."),
		},
		{
			Criterion: "next_tool_call_is_likely_to_improve_quality",
			Satisfied: nextLikelyImproves,
			Why:       ternary(nextLikelyImproves, "Another turn is still likely to improve graph quality materially.", "Another turn is unlikely to improve graph quality materially."),
		},
		{
			Criterion: "information_gain_is_positive",
			Satisfied: infoGainPositive,
			Why:       ternary(infoGainPositive, "Recent turns have produced new information.", fmt.Sprintf("Last %d consecutive turns produced no new information (qualityDelta=none).", state.ConsecutiveZeroGainTurns)),
		},
		{
			Criterion: "data_sources_have_content",
			Satisfied: dataAvailable,
			Why:       ternary(dataAvailable, "Knowledge base has returned some useful content.", fmt.Sprintf("Knowledge base returned 0 hits for %d consecutive queries (total hits=%d).", state.ConsecutiveZeroHitTurns, state.TotalRetrievalHits)),
		},
	}
}

func isRetryableSystemError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "429") ||
		strings.Contains(text, "resource exhausted") ||
		strings.Contains(text, "unavailable") ||
		strings.Contains(text, "deadlineexceeded") ||
		strings.Contains(text, "deadline exceeded") ||
		strings.Contains(text, "502") ||
		strings.Contains(text, "503") ||
		strings.Contains(text, "504")
}

func handleRetryableChildFailure(state *InvestigationState, action, branchID string, err error) bool {
	if state == nil || !isRetryableSystemError(err) {
		return false
	}
	key := strings.TrimSpace(action) + ":" + strings.TrimSpace(branchID)
	if key == ":" {
		key = strings.TrimSpace(action)
	}
	if state.LastSystemErrorKey == key {
		state.SystemErrorCount++
	} else {
		state.LastSystemErrorKey = key
		state.SystemErrorCount = 1
	}
	state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, RuntimeDiagnosis{
		Code:     "retryable_system_error",
		Severity: "warning",
		Message:  fmt.Sprintf("%s failed with retryable system error (attempt %d/3): %v", key, state.SystemErrorCount, err),
	})
	state.LatestRecommendedAction = normalizeActionRecommendation(ActionRecommendation{
		Action:    strings.TrimSpace(action),
		BranchID:  strings.TrimSpace(branchID),
		Reason:    fmt.Sprintf("Previous %s attempt failed with a retryable system error; retry once if it still looks like the best move.", key),
		Question:  lookupBranchQuestion(state, branchID),
		EndpointA: state.LatestGapDiagnosis.EndpointA,
		EndpointB: state.LatestGapDiagnosis.EndpointB,
	})
	state.RecommendedActions = append(state.RecommendedActions, state.LatestRecommendedAction)
	recordActionOutcome(state, ActionOutcome{
		Turn:              state.Turn,
		Action:            strings.TrimSpace(action),
		BranchID:          strings.TrimSpace(branchID),
		OutcomeClass:      "system_error",
		QualityDelta:      "none",
		Summary:           fmt.Sprintf("%s failed due to a retryable system error before producing new graph value.", key),
		GraphUseful:       false,
		SystemError:       true,
		Retryable:         true,
		RetryAttempt:      state.SystemErrorCount,
		ErrorMessage:      strings.TrimSpace(err.Error()),
		GapDiagnosis:      state.LatestGapDiagnosis,
		RecommendedAction: state.LatestRecommendedAction,
	})
	if state.SystemErrorCount >= 3 {
		state.StopReason = firstNonEmpty(state.StopReason, "system_error_retry_exhausted")
		return false
	}
	return true
}

func clearSystemErrorTracking(state *InvestigationState) {
	if state == nil {
		return
	}
	state.LastSystemErrorKey = ""
	state.SystemErrorCount = 0
}

func recordActionOutcome(state *InvestigationState, outcome ActionOutcome) {
	if state == nil {
		return
	}
	outcome = normalizeActionOutcome(outcome)
	if outcome.Action == "" && outcome.Summary == "" && outcome.OutcomeClass == "" {
		return
	}
	state.LatestOutcome = outcome
	state.Outcomes = append(state.Outcomes, outcome)
	trackInformationGain(state, outcome)
}

func trackInformationGain(state *InvestigationState, outcome ActionOutcome) {
	if state == nil {
		return
	}
	if outcome.QualityDelta == "none" || outcome.QualityDelta == "" {
		state.ConsecutiveZeroGainTurns++
	} else {
		state.ConsecutiveZeroGainTurns = 0
	}
}

func informationGainWarning(state *InvestigationState) string {
	if state == nil {
		return ""
	}
	parts := []string{}
	if state.ConsecutiveZeroGainTurns >= 2 {
		parts = append(parts, fmt.Sprintf("WARNING: %d consecutive turns with no information gain. Repeating similar queries is wasteful — stop or try a fundamentally different approach.", state.ConsecutiveZeroGainTurns))
	}
	if state.ConsecutiveZeroHitTurns >= 2 {
		parts = append(parts, fmt.Sprintf("WARNING: %d consecutive retrieval turns with 0 knowledge base hits. The internal KB lacks data on this topic.", state.ConsecutiveZeroHitTurns))
	}
	if state.TotalRetrievalHits == 0 && state.Turn > 2 {
		parts = append(parts, "WARNING: Zero total retrieval hits across all turns. The knowledge base has no data for this investigation.")
	}
	return strings.Join(parts, " ")
}

func shouldCountAsReformulation(bottleneck string) bool {
	switch strings.TrimSpace(bottleneck) {
	case string(BottleneckMissingMiddle), string(BottleneckPathContinuityGap), string(BottleneckTargetBindingGap):
		return true
	default:
		return false
	}
}

func summarizeDecisionState(state *InvestigationState) string {
	if state == nil {
		return ""
	}
	parts := []string{}
	if outcome := normalizeActionOutcome(state.LatestOutcome); outcome.Action != "" {
		last := "last=" + outcome.Action
		if outcome.BranchID != "" {
			last += ":" + outcome.BranchID
		}
		if outcome.OutcomeClass != "" {
			last += "/" + outcome.OutcomeClass
		}
		if outcome.QualityDelta != "" {
			last += "/" + outcome.QualityDelta
		}
		parts = append(parts, last)
		if outcome.SystemError {
			parts = append(parts, "last_system_error=true")
		}
		if outcome.PathContinuity != "" {
			parts = append(parts, "last_path="+outcome.PathContinuity)
		}
	}
	if strings.TrimSpace(state.CurrentBottleneck) != "" {
		parts = append(parts, "bottleneck="+strings.TrimSpace(state.CurrentBottleneck))
	}
	if diag := normalizeGapDiagnosis(state.LatestGapDiagnosis); diag.Kind != "" {
		parts = append(parts, "gap="+diag.Kind)
	}
	if qa := normalizeGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment); qa.Status != "" && qa.Status != "insufficient" {
		parts = append(parts, "good_enough="+qa.Status)
	}
	if strings.TrimSpace(state.PathResult.ContinuityStatus) != "" {
		parts = append(parts, "path="+strings.TrimSpace(state.PathResult.ContinuityStatus))
	}
	if len(parts) == 0 {
		return "No dominant bottleneck or closure signal recorded yet."
	}
	return strings.Join(parts, "; ")
}

func buildDecisionCandidates(state *InvestigationState) []DecisionCandidate {
	if state == nil {
		return nil
	}
	candidates := []DecisionCandidate{}
	add := func(item DecisionCandidate) {
		item = normalizeDecisionCandidate(item)
		if item.Action == "" {
			return
		}
		for _, existing := range candidates {
			if existing.Action == item.Action && existing.BranchID == item.BranchID {
				return
			}
		}
		candidates = append(candidates, item)
	}
	if outcome := normalizeActionOutcome(state.LatestOutcome); outcome.SystemError && outcome.Retryable && outcome.RetryAttempt > 0 && outcome.RetryAttempt < 3 && outcome.Action != "" {
		reason := fmt.Sprintf("The previous %s attempt failed due to a retryable system error before producing graph value. A retry is available.", outcome.Action)
		add(DecisionCandidate{
			Action:    outcome.Action,
			BranchID:  outcome.BranchID,
			Available: true,
			Reason:    reason,
			Question:  lookupBranchQuestion(state, outcome.BranchID),
			EndpointA: outcome.GapDiagnosis.EndpointA,
			EndpointB: outcome.GapDiagnosis.EndpointB,
		})
	}
	if id := selectBroadScanBranch(state); id != "" {
		branch := state.Branches[id]
		add(DecisionCandidate{
			Action:    "research",
			BranchID:  id,
			Available: true,
			Reason:    "Broad scan is still required before closure.",
			Question:  branchQuestion(branch),
		})
	}
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) {
			continue
		}
		available := branchNeedsMoreResearch(state, branch)
		reason := describeCandidateReason(state, "research", branchID)
		if !available {
			switch branch.Status {
			case BranchStatusDone, BranchStatusRejected:
				reason = "Branch is " + string(branch.Status) + "; no further research is warranted."
			case BranchStatusBlocked:
				reason = "Branch is blocked; resolve blocking condition first."
			default:
				if branch.MarginalGainStreak >= 2 {
					reason = "Marginal gain streak indicates diminishing returns."
				} else if branch.Status == BranchStatusSupported && branch.LastConfidence >= 0.75 {
					reason = "Branch already has strong support; further research is low-value."
				}
			}
		} else if shouldCountAsReformulation(state.CurrentBottleneck) && branch.ResearchRounds > 0 && branch.MarginalGainStreak >= 1 && branch.ReformulationCount >= 1 {
			available = false
			reason = "This branch already consumed its one additional reformulation after low-gain evidence. Another research pass is unlikely to improve quality materially."
		}
		add(DecisionCandidate{
			Action:       "research",
			BranchID:     branchID,
			Available:    available,
			Reason:       reason,
			Question:     branchQuestion(branch),
			ProductHints: branchProductHints(branch),
		})
	}
	if id := selectBridgeBranch(state); id != "" {
		ctx := buildBridgeContext(state, id)
		branch := state.Branches[id]
		add(DecisionCandidate{
			Action:       "bridge",
			BranchID:     id,
			Available:    shouldAttemptBridgeScan(state),
			Reason:       describeCandidateReason(state, "bridge", id),
			Question:     firstNonEmpty(stringValue(ctx, "bridgeQuestion"), branchQuestion(branch)),
			EndpointA:    stringValue(ctx, "endpointA"),
			EndpointB:    stringValue(ctx, "endpointB"),
			ProductHints: compactStrings(stringSliceValue(ctx["knownProducts"])),
		})
	} else if diag := normalizeGapDiagnosis(state.LatestGapDiagnosis); diag.EndpointsKnown && isBridgeGapKind(diag.Kind) {
		branchID := strings.TrimSpace(diag.BranchID)
		branch := state.Branches[branchID]
		add(DecisionCandidate{
			Action:    "bridge",
			BranchID:  branchID,
			Available: true,
			Reason:    firstNonEmpty(strings.TrimSpace(diag.Why), describeCandidateReason(state, "bridge", branchID)),
			Question:  firstNonEmpty(branchQuestion(branch), strings.TrimSpace(diag.Summary), "What sits between the known endpoints?"),
			EndpointA: strings.TrimSpace(diag.EndpointA),
			EndpointB: strings.TrimSpace(diag.EndpointB),
		})
	}
	add(DecisionCandidate{
		Action:    "path",
		Available: shouldPersistGoodEnoughPath(state) || shouldAttemptPath(state),
		Reason:    describeCandidateReason(state, "path", ""),
	})
	add(DecisionCandidate{
		Action:    "critic",
		Available: shouldRunCritic(state),
		Reason:    describeCandidateReason(state, "critic", ""),
	})
	add(DecisionCandidate{
		Action:    "materialize",
		Available: shouldMaterialize(state),
		Reason:    describeCandidateReason(state, "materialize", ""),
	})
	add(DecisionCandidate{
		Action:    "replan",
		Available: state.NeedsReplan,
		Reason:    describeCandidateReason(state, "replan", ""),
	})
	add(DecisionCandidate{
		Action:    "stop",
		Available: strings.TrimSpace(state.StopReason) != "" || completenessAllowsStop(state) || goodEnoughAllowsStop(state) || detectCurrentBottleneck(state) == BottleneckNoHighValueAction,
		Reason:    "Stop is valid only if the current graph result is already useful enough or additional turns are low-value.",
	})
	return candidates
}

func validateDecisionChoice(decision DecisionResult, candidates []DecisionCandidate) (string, string, string, string) {
	decision = normalizeDecisionResult(decision)
	if decision.ChosenAction == "" {
		return "", "", "fallback", "decision returned no chosenAction"
	}
	for _, candidate := range candidates {
		if candidate.Action != decision.ChosenAction {
			continue
		}
		branchID := strings.TrimSpace(decision.BranchID)
		if candidate.BranchID != "" && branchID != "" && candidate.BranchID != branchID {
			continue
		}
		if candidate.BranchID != "" && branchID == "" {
			branchID = candidate.BranchID
		}
		if !candidate.Available {
			return "", "", "fallback", fmt.Sprintf("decision chose unavailable candidate %s/%s", decision.ChosenAction, branchID)
		}
		return candidate.Action, branchID, "accepted", ""
	}
	return "", "", "fallback", fmt.Sprintf("decision chose non-candidate action %s/%s", decision.ChosenAction, decision.BranchID)
}

func applyDecisionToState(state *InvestigationState, decision DecisionResult) {
	if state == nil {
		return
	}
	decision = normalizeDecisionResult(decision)
	state.LatestDecision = decision
	state.Decisions = append(state.Decisions, decision)
}

func normalizeDecisionCandidate(item DecisionCandidate) DecisionCandidate {
	item.Action = strings.TrimSpace(item.Action)
	item.BranchID = strings.TrimSpace(item.BranchID)
	item.Reason = strings.TrimSpace(item.Reason)
	item.Question = strings.TrimSpace(item.Question)
	item.EndpointA = strings.TrimSpace(item.EndpointA)
	item.EndpointB = strings.TrimSpace(item.EndpointB)
	item.ProductHints = compactStrings(item.ProductHints)
	item.GeoHints = compactStrings(item.GeoHints)
	item.ActorHints = compactStrings(item.ActorHints)
	item.DataSourceHint = strings.TrimSpace(item.DataSourceHint)
	if item.DataSourceHint == "" {
		item.DataSourceHint = defaultDataSourceHint(item.Action)
	}
	return item
}

// defaultDataSourceHint returns a static description of what data sources an action uses.
// This helps the orchestrator reason about expected returns before executing a tool,
// preventing fruitless loops when a source is known to be empty.
func defaultDataSourceHint(action string) string {
	switch action {
	case "research":
		return "Queries internal knowledge base (Vertex AI Search). Returns zero results when the internal KB has no data for this product or supply chain. If internal KB is empty, repeated research calls are wasteful — use bridge instead."
	case "bridge":
		return "Uses web-grounded reasoning only. Does NOT query the internal knowledge base. Always returns an external perspective. Prefer bridge when research returned internal_empty or internal_partial."
	case "path":
		return "Reads from the local graph database (materialized artifacts). Does not perform external retrieval."
	case "critic":
		return "Evaluates quality of current findings using reasoning only. No external retrieval."
	case "materialize":
		return "Writes accepted pending graph operations to persistent store. No external retrieval."
	case "replan":
		return "Updates the investigation plan based on current findings. No external retrieval."
	case "stop":
		return "Terminates the session and commits current findings."
	default:
		return ""
	}
}

func normalizeDecisionResult(item DecisionResult) DecisionResult {
	item.StateSummary = strings.TrimSpace(item.StateSummary)
	item.ChosenAction = strings.TrimSpace(item.ChosenAction)
	item.BranchID = strings.TrimSpace(item.BranchID)
	item.WhyChosen = strings.TrimSpace(item.WhyChosen)
	item.WhyNotStop = strings.TrimSpace(item.WhyNotStop)
	item.WhyNotPath = strings.TrimSpace(item.WhyNotPath)
	item.WhyNotBridge = strings.TrimSpace(item.WhyNotBridge)
	item.WhyNotResearch = strings.TrimSpace(item.WhyNotResearch)
	item.WhyNotCritic = strings.TrimSpace(item.WhyNotCritic)
	item.WhyNotMaterialize = strings.TrimSpace(item.WhyNotMaterialize)
	item.WhyNotReplan = strings.TrimSpace(item.WhyNotReplan)
	item.EvidenceUsed = compactStrings(item.EvidenceUsed)
	item.ExecutedAction = strings.TrimSpace(item.ExecutedAction)
	item.ExecutedBranchID = strings.TrimSpace(item.ExecutedBranchID)
	item.ValidationStatus = strings.TrimSpace(item.ValidationStatus)
	item.ValidationReason = strings.TrimSpace(item.ValidationReason)
	for i := range item.Candidates {
		item.Candidates[i] = normalizeDecisionCandidate(item.Candidates[i])
	}
	return item
}

func isEmptyDecisionResult(item DecisionResult) bool {
	item = normalizeDecisionResult(item)
	return item.ChosenAction == "" &&
		item.StateSummary == "" &&
		item.WhyChosen == "" &&
		len(item.StopCriteria) == 0 &&
		len(item.EvidenceUsed) == 0
}

func normalizeActionOutcome(item ActionOutcome) ActionOutcome {
	item.Action = strings.TrimSpace(item.Action)
	item.BranchID = strings.TrimSpace(item.BranchID)
	item.OutcomeClass = strings.TrimSpace(strings.ToLower(item.OutcomeClass))
	item.QualityDelta = strings.TrimSpace(strings.ToLower(item.QualityDelta))
	item.Summary = strings.TrimSpace(item.Summary)
	item.ErrorMessage = strings.TrimSpace(item.ErrorMessage)
	item.PathContinuity = strings.TrimSpace(strings.ToLower(item.PathContinuity))
	item.OpenQuestions = compactStrings(item.OpenQuestions)
	item.EvidenceRefs = compactStrings(item.EvidenceRefs)
	item.GapDiagnosis = normalizeGapDiagnosis(item.GapDiagnosis)
	item.RecommendedAction = normalizeActionRecommendation(item.RecommendedAction)
	item.GoodEnoughAssessment = normalizeGoodEnoughAssessment(item.GoodEnoughAssessment)
	item.DataSourceSignal = strings.TrimSpace(strings.ToLower(item.DataSourceSignal))
	return item
}

func outcomeFromResearch(state *InvestigationState, branchID string, result ResearchResult) ActionOutcome {
	outcome := ActionOutcome{
		Turn:              state.Turn,
		Action:            "research",
		BranchID:          strings.TrimSpace(branchID),
		OutcomeClass:      "no_progress",
		QualityDelta:      "none",
		Summary:           firstNonEmpty(strings.TrimSpace(result.Summary), "Research completed without a clear new graph contribution."),
		GraphUseful:       len(result.ProposedOps) > 0 || len(result.CanonicalRefs) > 0 || len(result.ProductRefs) > 0,
		ProducedGraphOps:  len(result.ProposedOps),
		OpenQuestions:     result.OpenQuestions,
		EvidenceRefs:      compactStrings(append(append([]string{}, result.CanonicalRefs...), result.ProductRefs...)),
		GapDiagnosis:      result.GapDiagnosis,
		RecommendedAction: result.RecommendedAction,
	}
	switch {
	case outcome.GraphUseful && (result.Confidence >= 0.65 || len(result.ProposedOps) > 0 || len(result.CanonicalRefs) >= 2):
		outcome.OutcomeClass = "expanded_substrate"
		outcome.QualityDelta = "material"
	case outcome.GraphUseful || len(result.EvidencePoints) > 0 || len(result.OpenQuestions) > 0:
		outcome.OutcomeClass = "expanded_substrate"
		outcome.QualityDelta = "incremental"
	}
	return normalizeActionOutcome(outcome)
}

// resolveDataSourceSignal returns the model's own data-source assessment when available,
// falling back to a conservative host-side inference only if the model didn't return one.
// The model is trusted to evaluate what it saw; the fallback exists purely as a safety net
// for older model versions or prompt failures.
func resolveDataSourceSignal(modelSignal string, outcome ActionOutcome) string {
	if sig := strings.TrimSpace(modelSignal); sig != "" {
		return sig
	}
	switch {
	case outcome.RetrievalHitCount == 0 && !outcome.GraphUseful:
		return "internal_empty"
	case outcome.RetrievalHitCount > 0 && outcome.GraphUseful && outcome.QualityDelta == "material":
		return "internal_grounded"
	case outcome.RetrievalHitCount > 0 || outcome.GraphUseful:
		return "internal_partial"
	default:
		return "no_data"
	}
}

func outcomeFromBridge(state *InvestigationState, branchID string, result BridgeResult) ActionOutcome {
	evidence := compactStrings(append(append([]string{}, result.IntermediateNodes...), result.RouteSketches...))
	outcome := ActionOutcome{
		Turn:              state.Turn,
		Action:            "bridge",
		BranchID:          firstNonEmpty(strings.TrimSpace(branchID), strings.TrimSpace(result.BranchID)),
		OutcomeClass:      "no_progress",
		QualityDelta:      "none",
		Summary:           firstNonEmpty(strings.TrimSpace(result.Summary), "Bridge analysis did not yet articulate a useful missing-middle structure."),
		GraphUseful:       result.BridgeExists || len(result.BridgeSteps) > 0 || len(result.ProposedOps) > 0,
		BridgeExists:      result.BridgeExists,
		ProducedGraphOps:  len(result.ProposedOps),
		OpenQuestions:     result.OpenQuestions,
		EvidenceRefs:      evidence,
		GapDiagnosis:      result.GapDiagnosis,
		RecommendedAction: result.RecommendedAction,
		DataSourceSignal:  "external_only", // default; overridden below if model returns its own assessment
	}
	switch {
	case result.BridgeExists || len(result.BridgeSteps) >= 2 || len(result.ProposedOps) > 0:
		outcome.OutcomeClass = "structured_bridge"
		outcome.QualityDelta = "material"
	case len(result.RouteSketches) > 0 || len(result.SuggestedSubscans) > 0 || len(result.OpenQuestions) > 0:
		outcome.OutcomeClass = "structured_bridge"
		outcome.QualityDelta = "incremental"
	}
	return normalizeActionOutcome(outcome)
}

func outcomeFromCritic(state *InvestigationState, branchID string, result CriticResult) ActionOutcome {
	outcome := ActionOutcome{
		Turn:                 state.Turn,
		Action:               "critic",
		BranchID:             strings.TrimSpace(branchID),
		OutcomeClass:         "no_progress",
		QualityDelta:         "none",
		Summary:              firstNonEmpty(strings.TrimSpace(result.GoodEnoughAssessment.Reason), firstString(result.AcceptedFindings), "Critic evaluated the current graph state."),
		GraphUseful:          len(result.AcceptedFindings) > 0 || len(result.GraphOpVerdicts) > 0 || len(result.BranchAssessments) > 0 || result.GoodEnoughAssessment.PersistWorkingPath,
		OpenQuestions:        result.OpenQuestions,
		EvidenceRefs:         compactStrings(append(append([]string{}, result.PathProducts...), result.FocusActors...)),
		GapDiagnosis:         result.GapDiagnosis,
		RecommendedAction:    result.RecommendedAction,
		GoodEnoughAssessment: result.GoodEnoughAssessment,
	}
	switch {
	case result.GoodEnoughAssessment.StopNow || result.GoodEnoughAssessment.PersistWorkingPath || len(result.GraphOpVerdicts) > 0 || len(result.BranchAssessments) > 0:
		outcome.OutcomeClass = "adjudicated_state"
		outcome.QualityDelta = "material"
	case len(result.AcceptedFindings) > 0 || len(result.RiskyClaims) > 0 || len(result.OpenQuestions) > 0:
		outcome.OutcomeClass = "adjudicated_state"
		outcome.QualityDelta = "incremental"
	}
	return normalizeActionOutcome(outcome)
}

func outcomeFromPath(state *InvestigationState, result PathResult) ActionOutcome {
	artifacts := len(result.MaterializedArtifactIDs)
	if strings.TrimSpace(result.AssemblyArtifactID) != "" {
		artifacts++
	}
	outcome := ActionOutcome{
		Turn:              state.Turn,
		Action:            "path",
		BranchID:          firstString(result.SourceBranchIDs),
		OutcomeClass:      "no_progress",
		QualityDelta:      "none",
		Summary:           firstNonEmpty(firstString(result.OpenGaps), "Path verification did not yet strengthen the current chain."),
		GraphUseful:       len(result.TraceDigests) > 0 || artifacts > 0 || strings.EqualFold(result.ContinuityStatus, "partial"),
		PathContinuity:    result.ContinuityStatus,
		ProducedArtifacts: artifacts,
		OpenQuestions:     result.OpenGaps,
		EvidenceRefs:      result.QueriedProducts,
		GapDiagnosis:      result.GapDiagnosis,
		RecommendedAction: result.RecommendedAction,
	}
	switch strings.TrimSpace(strings.ToLower(result.ContinuityStatus)) {
	case "verified", "bound":
		outcome.OutcomeClass = "validated_chain"
		outcome.QualityDelta = "material"
	case "partial":
		outcome.OutcomeClass = "validated_chain"
		outcome.QualityDelta = "incremental"
	default:
		if len(result.TraceDigests) > 0 || artifacts > 0 {
			outcome.OutcomeClass = "validated_chain"
			outcome.QualityDelta = "material"
		}
	}
	return normalizeActionOutcome(outcome)
}

func outcomeFromMaterialize(state *InvestigationState, result MaterializeResult) ActionOutcome {
	artifacts := len(result.ArtifactIDs) + len(result.Created) + len(result.Updated)
	outcome := ActionOutcome{
		Turn:              state.Turn,
		Action:            "materialize",
		OutcomeClass:      "no_progress",
		QualityDelta:      "none",
		Summary:           firstNonEmpty(strings.TrimSpace(result.Summary), "Materialization did not land any new graph artifacts."),
		GraphUseful:       result.Persisted && artifacts > 0,
		ProducedArtifacts: artifacts,
		EvidenceRefs:      compactStrings(append([]string{}, result.ArtifactIDs...)),
	}
	if result.Persisted {
		outcome.OutcomeClass = "materialized_graph"
		outcome.QualityDelta = "material"
	}
	return normalizeActionOutcome(outcome)
}

func outcomeFromReplan(state *InvestigationState) ActionOutcome {
	return normalizeActionOutcome(ActionOutcome{
		Turn:              state.Turn,
		Action:            "replan",
		OutcomeClass:      "strategy_update",
		QualityDelta:      "incremental",
		Summary:           firstNonEmpty(strings.TrimSpace(state.ReplanReason), "Strategy was reframed to improve the investigation path."),
		GraphUseful:       true,
		RecommendedAction: state.LatestRecommendedAction,
		GapDiagnosis:      state.LatestGapDiagnosis,
	})
}

func branchQuestion(branch *BranchRuntimeState) string {
	if branch == nil {
		return ""
	}
	return strings.TrimSpace(branch.Branch.Question)
}

func branchProductHints(branch *BranchRuntimeState) []string {
	if branch == nil {
		return nil
	}
	return compactStrings(append([]string{}, branch.Branch.ProductHints...))
}

func selectDiagnosedAction(state *InvestigationState) (string, string) {
	if state == nil {
		return "", ""
	}
	diag := normalizeGapDiagnosis(state.LatestGapDiagnosis)
	if diag.Kind == "" && diag.NextAction == "" {
		return "", ""
	}
	action := firstNonEmpty(diag.NextAction, defaultActionForGapKind(diag.Kind))
	switch action {
	case "research":
		branchID := firstNonEmpty(diag.BranchID, selectResearchBranchByScore(state))
		if branchID == "" {
			return "", ""
		}
		branch := state.Branches[branchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) {
			return "", ""
		}
		return "research", branchID
	case "bridge":
		branchID := firstNonEmpty(diag.BranchID, selectBridgeBranch(state))
		if branchID == "" {
			return "", ""
		}
		branch := state.Branches[branchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) || state.BridgeAttempts >= 4 {
			return "", ""
		}
		return "bridge", branchID
	case "path":
		if shouldAttemptPath(state) {
			return "path", ""
		}
	case "critic":
		if shouldRunCritic(state) {
			return "critic", ""
		}
	case "materialize":
		if shouldMaterialize(state) {
			return "materialize", ""
		}
	case "replan":
		return "replan", ""
	case "stop":
		if completenessAllowsStop(state) {
			state.StopReason = firstNonEmpty(state.StopReason, "diagnosed_stop")
			return "stop", ""
		}
	}
	return "", ""
}

func selectRecommendedAction(state *InvestigationState) (string, string) {
	if state == nil {
		return "", ""
	}
	rec := normalizeActionRecommendation(state.LatestRecommendedAction)
	if rec.Action == "" {
		return "", ""
	}
	if !shouldHonorRecommendedAction(state, rec) {
		return "", ""
	}
	if len(state.ActionLog) > 0 && state.ActionLog[len(state.ActionLog)-1] == rec.Action {
		return "", ""
	}
	switch rec.Action {
	case "research":
		if strings.TrimSpace(rec.BranchID) == "" {
			return "", ""
		}
		branch := state.Branches[rec.BranchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) {
			return "", ""
		}
		return "research", rec.BranchID
	case "bridge":
		branchID := firstNonEmpty(rec.BranchID, selectBridgeBranch(state))
		if branchID == "" {
			return "", ""
		}
		branch := state.Branches[branchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) || state.BridgeAttempts >= 4 {
			return "", ""
		}
		return "bridge", branchID
	case "path":
		if shouldAttemptPath(state) {
			return "path", ""
		}
	case "critic":
		if shouldRunCritic(state) {
			return "critic", ""
		}
	case "materialize":
		if shouldMaterialize(state) {
			return "materialize", ""
		}
	case "replan":
		return "replan", ""
	case "stop":
		if completenessAllowsStop(state) {
			state.StopReason = firstNonEmpty(state.StopReason, "recommended_stop")
			return "stop", ""
		}
	}
	return "", ""
}

func shouldHonorRecommendedAction(state *InvestigationState, rec ActionRecommendation) bool {
	if state == nil || rec.Action == "" {
		return false
	}
	diag := normalizeGapDiagnosis(state.LatestGapDiagnosis)
	if diag.NextAction != "" && rec.Action != diag.NextAction {
		return false
	}
	if diag.NextAction != "" && diag.BranchID != "" && rec.BranchID != "" && (rec.Action == "research" || rec.Action == "bridge") && rec.BranchID != diag.BranchID {
		return false
	}
	bottleneck := detectCurrentBottleneck(state)
	if rec.Action == "research" {
		if countRecentMatchingRecommendations(state, rec, 4) >= 2 {
			switch bottleneck {
			case BottleneckPathContinuityGap, BottleneckMissingMiddle, BottleneckTargetBindingGap:
				return false
			}
		}
		if rec.BranchID != "" {
			if branch := state.Branches[rec.BranchID]; branch != nil {
				if branch.ResearchRounds >= max(state.Plan.MaxResearchRounds+1, 3) && (branchNeedsBridgeScan(state, rec.BranchID) || branchPathGapHit(state, rec.BranchID)) {
					return false
				}
			}
		}
	}
	return true
}

func currentBottleneckFromDiagnosis(diag GapDiagnosis, action string) string {
	diag = normalizeGapDiagnosis(diag)
	switch diag.Kind {
	case "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer":
		return string(BottleneckMissingMiddle)
	case "missing_verification":
		return string(BottleneckPathContinuityGap)
	case "missing_adjudication":
		return string(BottleneckUnresolvedConflict)
	case "missing_completeness", "missing_substrate":
		return string(BottleneckMissingSubstrate)
	case "no_gap":
		if action == "materialize" {
			return string(BottleneckMaterializationHold)
		}
		return string(BottleneckNoHighValueAction)
	}
	switch strings.TrimSpace(action) {
	case "bridge":
		return string(BottleneckMissingMiddle)
	case "path":
		return string(BottleneckPathContinuityGap)
	case "critic", "replan":
		return string(BottleneckUnresolvedConflict)
	case "research":
		return string(BottleneckMissingSubstrate)
	case "materialize":
		return string(BottleneckMaterializationHold)
	default:
		return ""
	}
}

func countRecentMatchingRecommendations(state *InvestigationState, rec ActionRecommendation, window int) int {
	if state == nil || rec.Action == "" || window <= 0 {
		return 0
	}
	items := state.RecommendedActions
	if len(items) == 0 {
		return 0
	}
	start := max(0, len(items)-window)
	count := 0
	for _, item := range items[start:] {
		item = normalizeActionRecommendation(item)
		if item.Action != rec.Action {
			continue
		}
		if firstNonEmpty(item.BranchID, rec.BranchID) != firstNonEmpty(rec.BranchID, item.BranchID) {
			continue
		}
		if firstNonEmpty(item.FailureMode, rec.FailureMode) != firstNonEmpty(rec.FailureMode, item.FailureMode) {
			continue
		}
		count++
	}
	return count
}

func detectCurrentBottleneck(state *InvestigationState) InvestigationBottleneck {
	if state == nil {
		return BottleneckNoHighValueAction
	}
	if shouldAttemptBridgeScan(state) && selectBridgeBranch(state) != "" {
		return BottleneckMissingMiddle
	}
	// Accepted ops waiting for path verification take priority.
	if hasPendingAcceptedOps(state) && shouldAttemptPath(state) {
		return BottleneckPathContinuityGap
	}
	if hasLocallyReadyPathCandidate(state) && shouldAttemptPath(state) {
		return BottleneckPathContinuityGap
	}
	if shouldMaterialize(state) && !state.LatestCritic.WithholdMaterialization {
		return BottleneckMaterializationHold
	}
	if state.NeedsReplan || len(state.LatestCritic.RiskyClaims) > 0 {
		return BottleneckUnresolvedConflict
	}
	if shouldAttemptPath(state) {
		if strings.EqualFold(strings.TrimSpace(state.PathResult.ContinuityStatus), "broken") || len(state.PathResult.OpenGaps) > 0 {
			return BottleneckPathContinuityGap
		}
		return BottleneckTargetBindingGap
	}
	if id := selectResearchBranchByScore(state); id != "" {
		b := state.Branches[id]
		if b != nil && b.LastConfidence < 0.55 {
			return BottleneckLowConfidenceClaim
		}
		return BottleneckMissingSubstrate
	}
	// If we have unresolved ops but no research branch, more research may be needed.
	if hasPendingUnresolvedOps(state) {
		return BottleneckMissingSubstrate
	}
	return BottleneckNoHighValueAction
}

func selectResearchBranchByScore(state *InvestigationState) string {
	if state == nil {
		return ""
	}
	type branchScore struct {
		id    string
		score float64
	}
	best := branchScore{}
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		if branch == nil || !branchDependenciesSatisfied(state, branch) || !branchNeedsMoreResearch(state, branch) {
			continue
		}
		score := branchScoreCached(state, branchID)
		if score > best.score {
			best = branchScore{id: branchID, score: score}
		}
	}
	return best.id
}

func selectActionableResearchBranch(state *InvestigationState) string {
	id := selectResearchBranchByScore(state)
	if id == "" {
		return ""
	}
	h := state.Hypotheses[id]
	if h == nil {
		return id
	}
	switch h.PursuitDecision {
	case BranchPursuitPress, BranchPursuitPivot:
		return id
	}
	if h.TargetRelevance >= 0.55 {
		return id
	}
	return ""
}

func selectBroadScanBranch(state *InvestigationState) string {
	if state == nil || !state.Completeness.BroadScanRequired || state.Completeness.BroadScanCompleted {
		return ""
	}
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		if branch == nil || !branch.Branch.BroadScan {
			continue
		}
		if branchNeedsMoreResearch(state, branch) {
			return branchID
		}
	}
	return ""
}

func completenessAllowsStop(state *InvestigationState) bool {
	if state == nil || !state.Completeness.BroadScanRequired {
		return true
	}
	if goodEnoughAllowsStop(state) {
		return true
	}
	if state.Completeness.NoMaterialAlternatives {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(state.Completeness.ExplorationStatus), "satisfied")
}

func goodEnoughAllowsStop(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	item := normalizeGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment)
	if !item.StopNow {
		return false
	}
	if item.PersistWorkingPath && shouldPersistGoodEnoughPath(state) {
		return false
	}
	return item.FurtherWorkLowValue || strings.TrimSpace(item.Status) != "insufficient"
}

func shouldPersistGoodEnoughPath(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	item := normalizeGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment)
	if !item.PersistWorkingPath {
		return false
	}
	if goodEnoughPathAlreadyCaptured(state.PathResult, item.TargetBranchIDs) {
		return false
	}
	if shouldAttemptPath(state) {
		return true
	}
	if state.PathAttempts >= 6 {
		return false
	}
	return hasPendingAcceptedOps(state) || hasLocallyReadyPathCandidate(state)
}

func goodEnoughPathAlreadyCaptured(path PathResult, targetBranchIDs []string) bool {
	if len(path.TraceDigests) == 0 && !strings.EqualFold(strings.TrimSpace(path.ContinuityStatus), "landed") {
		return false
	}
	targetBranchIDs = compactStrings(targetBranchIDs)
	if len(targetBranchIDs) == 0 {
		return true
	}
	captured := map[string]struct{}{}
	for _, branchID := range compactStrings(path.SourceBranchIDs) {
		captured[branchID] = struct{}{}
	}
	for _, branchID := range targetBranchIDs {
		if _, ok := captured[branchID]; !ok {
			return false
		}
	}
	return true
}

func branchNeedsMoreResearch(state *InvestigationState, branch *BranchRuntimeState) bool {
	if state == nil || branch == nil {
		return false
	}
	if h := state.Hypotheses[branch.Branch.ID]; h != nil {
		switch h.PursuitDecision {
		case BranchPursuitDrop, BranchPursuitPark:
			return false
		}
	}
	switch branch.Status {
	case BranchStatusDone, BranchStatusRejected, BranchStatusCandidate, BranchStatusBlocked:
		return false
	}
	if branch.MarginalGainStreak >= 2 {
		return false
	}
	maxR := max(state.Plan.MaxResearchRounds, 1)
	if branch.ResearchRounds >= maxR {
		return false
	}
	// Strong support: do not burn remaining rounds just to hit MaxResearchRounds.
	if branch.Status == BranchStatusSupported && branch.LastConfidence >= 0.75 {
		return false
	}
	return true
}

func shouldRunCritic(state *InvestigationState) bool {
	if state == nil || len(state.ResearchResults) == 0 {
		return false
	}
	if len(state.ActionLog) > 0 && state.ActionLog[len(state.ActionLog)-1] == "critic" {
		return false
	}
	return true
}

// compactPendingOps returns a branch-scoped view of pending ops for research context.
func compactPendingOps(ops []ProposedGraphOp, branchID string) []map[string]any {
	if len(ops) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, 8)
	for _, op := range ops {
		if op.Status == "landed" || op.Status == "rejected" {
			continue
		}
		if branchID != "" && op.BranchID != "" && op.BranchID != branchID {
			continue
		}
		out = append(out, map[string]any{
			"kind":        op.Kind,
			"subjectPath": op.SubjectPath,
			"objectPath":  op.ObjectPath,
			"edgeType":    op.EdgeType,
			"status":      op.Status,
			"confidence":  op.Confidence,
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// markLandedOps marks verified graph ops as landed when materialization succeeds.
func markLandedOps(state *InvestigationState, result MaterializeResult) {
	if state == nil || !result.Persisted {
		return
	}
	landedBranches := map[string]struct{}{}
	for _, id := range result.LandedBranchIDs {
		landedBranches[strings.TrimSpace(id)] = struct{}{}
	}
	for i := range state.PendingGraphOps {
		op := &state.PendingGraphOps[i]
		if op.Status != "verified" && op.Status != "accepted" {
			continue
		}
		if len(landedBranches) > 0 {
			if _, ok := landedBranches[op.BranchID]; !ok {
				continue
			}
		}
		op.Status = "landed"
	}
}

func shouldPrioritizePath(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	if len(state.ActionLog) > 0 && state.ActionLog[len(state.ActionLog)-1] == "path" {
		return false
	}
	scope := selectPathScope(state)
	if len(scope.TargetBranchIDs) == 0 {
		return false
	}
	if state.LatestCritic.MaterializePath {
		return true
	}
	for _, id := range scope.TargetBranchIDs {
		if h := state.Hypotheses[id]; h != nil {
			if h.MaterializationReady || h.TargetBound || h.PursuitDecision == BranchPursuitPress {
				return true
			}
			if h.LocalReady && h.PursuitDecision != BranchPursuitPark && h.PursuitDecision != BranchPursuitDrop && h.TargetRelevance >= 0.45 {
				return true
			}
			if h.Status == HypothesisSupported && h.TargetRelevance >= 0.55 {
				return true
			}
		}
	}
	return false
}

func shouldPrioritizeMaterialize(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	if len(state.PathResult.MaterializedArtifactIDs) == 0 && strings.TrimSpace(state.PathResult.AssemblyArtifactID) == "" {
		return false
	}
	candidates := compactStrings(state.PathResult.SourceBranchIDs)
	if len(candidates) == 0 {
		return false
	}
	for _, id := range candidates {
		if h := state.Hypotheses[id]; h != nil && h.MaterializationReady {
			return true
		}
	}
	return false
}

func applyActionRecommendationToState(state *InvestigationState, originAction, originBranchID string, rec ActionRecommendation) {
	if state == nil {
		return
	}
	_ = originAction
	rec = normalizeActionRecommendation(rec)
	if rec.Action == "" {
		return
	}
	if rec.BranchID == "" && (rec.Action == "research" || rec.Action == "bridge") {
		rec.BranchID = strings.TrimSpace(originBranchID)
	}
	state.LatestRecommendedAction = rec
	state.RecommendedActions = append(state.RecommendedActions, rec)
	if rec.Action != "research" && rec.Action != "bridge" {
		return
	}
	if rec.BranchID == "" {
		return
	}
	branch := state.Branches[rec.BranchID]
	if branch == nil {
		question := firstNonEmpty(rec.Question, "Follow the neighboring method recommended by the prior step.")
		newBranch := InvestigationBranch{
			ID:           rec.BranchID,
			Question:     question,
			Role:         strings.TrimSpace(state.Role.PrimaryRole),
			ProductHints: compactStrings(append([]string{}, rec.ProductHints...)),
			GeoHints:     compactStrings(append([]string{}, rec.GeoHints...)),
			ActorHints:   compactStrings(append([]string{}, rec.ActorHints...)),
			RequiresPath: rec.RequiresPath || rec.Action == "bridge",
		}
		if originBranchID != "" && originBranchID != rec.BranchID {
			newBranch.DependsOn = []string{originBranchID}
		}
		state.Branches[rec.BranchID] = &BranchRuntimeState{
			Branch:  normalizeBranchForMission(state.Role, newBranch),
			Status:  BranchStatusProposed,
			History: []BranchHistoryEntry{},
		}
		state.BranchOrder = append(state.BranchOrder, rec.BranchID)
		state.Hypotheses[rec.BranchID] = &InvestigationHypothesis{
			ID:               "hypothesis:" + rec.BranchID,
			BranchID:         rec.BranchID,
			Statement:        question,
			Status:           HypothesisProposed,
			PursuitDecision:  BranchPursuitPress,
			PursuitReason:    firstNonEmpty(rec.Reason, "Recommended as the neighboring method after the previous attempt."),
			ExpectedInfoGain: 0.72,
		}
		return
	}
	branch.Branch.ProductHints = compactStrings(append(branch.Branch.ProductHints, rec.ProductHints...))
	branch.Branch.GeoHints = compactStrings(append(branch.Branch.GeoHints, rec.GeoHints...))
	branch.Branch.ActorHints = compactStrings(append(branch.Branch.ActorHints, rec.ActorHints...))
	branch.Branch.RequiresPath = branch.Branch.RequiresPath || rec.RequiresPath || rec.Action == "bridge"
	if strings.TrimSpace(branch.Branch.Question) == "" {
		branch.Branch.Question = strings.TrimSpace(rec.Question)
	}
}

func applyGapDiagnosisToState(state *InvestigationState, originAction, originBranchID string, diag GapDiagnosis) {
	if state == nil {
		return
	}
	_ = originAction
	diag = normalizeGapDiagnosis(diag)
	if diag.Kind == "" && diag.NextAction == "" && diag.Summary == "" {
		return
	}
	if diag.BranchID == "" {
		diag.BranchID = strings.TrimSpace(originBranchID)
	}
	if diag.NextAction == "" {
		diag.NextAction = defaultActionForGapKind(diag.Kind)
	}
	diag = normalizeGapDiagnosis(diag)
	state.LatestGapDiagnosis = mergeGapDiagnosis(state.LatestGapDiagnosis, diag)
	if len(state.GapDiagnoses) == 0 || !sameGapDiagnosis(state.GapDiagnoses[len(state.GapDiagnoses)-1], diag) {
		state.GapDiagnoses = append(state.GapDiagnoses, diag)
	}
}

func sameGapDiagnosis(a, b GapDiagnosis) bool {
	a = normalizeGapDiagnosis(a)
	b = normalizeGapDiagnosis(b)
	return a.Kind == b.Kind &&
		a.Summary == b.Summary &&
		a.Why == b.Why &&
		a.QuestionClass == b.QuestionClass &&
		a.NextAction == b.NextAction &&
		a.BranchID == b.BranchID &&
		a.EndpointA == b.EndpointA &&
		a.EndpointB == b.EndpointB &&
		a.EndpointsKnown == b.EndpointsKnown
}

type pathScopeSelection struct {
	TargetBranchIDs     []string
	SupportingBranchIDs []string
	ProductScope        []string
	SelectionReason     string
}

func selectPathScope(state *InvestigationState) pathScopeSelection {
	scope := pathScopeSelection{
		TargetBranchIDs:     nil,
		SupportingBranchIDs: nil,
		ProductScope:        nil,
		SelectionReason:     "no_eligible_path_scope",
	}
	if state == nil {
		return scope
	}
	type candidate struct {
		id    string
		score int
	}
	criticProducts := compactStrings(state.LatestCritic.PathProducts)
	criticActors := compactStrings(state.LatestCritic.FocusActors)
	criticGeos := compactStrings(state.LatestCritic.FocusGeos)
	candidates := make([]candidate, 0, len(state.BranchOrder))
	for _, id := range state.BranchOrder {
		b := state.Branches[id]
		if b == nil {
			continue
		}
		if !branchDependenciesSatisfied(state, b) {
			continue
		}
		switch b.Status {
		case BranchStatusDone, BranchStatusRejected, BranchStatusBlocked, BranchStatusCandidate:
			continue
		}
		score := 0
		if b.Branch.RequiresPath {
			score += 3
		}
		switch b.Status {
		case BranchStatusSupported:
			score += 4
		case BranchStatusActive:
			score += 2
		}
		if h := state.Hypotheses[id]; h != nil {
			if h.MaterializationReady {
				score += 4
			}
			if h.LocalReady {
				score += 3
			}
			if h.TargetBound {
				score += 2
			}
			score += int(h.TargetRelevance * 4)
			switch h.PursuitDecision {
			case BranchPursuitPress:
				score += 3
			case BranchPursuitPivot:
				score += 1
			case BranchPursuitPark, BranchPursuitDrop:
				score -= 3
			}
		}
		branchProducts := compactStrings(append([]string{}, b.Branch.ProductHints...))
		branchActors := compactStrings(append([]string{}, b.Branch.ActorHints...))
		branchGeos := compactStrings(append([]string{}, b.Branch.GeoHints...))
		branchProducts = compactStrings(append(branchProducts, collectBranchResearchProducts(state, id)...))
		score += overlapCount(branchProducts, criticProducts) * 3
		score += overlapCount(branchActors, criticActors) * 2
		score += overlapCount(branchGeos, criticGeos) * 2
		if len(b.History) > 0 {
			last := b.History[len(b.History)-1]
			if last.Action == "research" {
				score++
			}
		}
		if score <= 0 {
			continue
		}
		candidates = append(candidates, candidate{id: id, score: score})
	}
	if len(candidates) == 0 {
		scope.SupportingBranchIDs = inferBranchesForPathSubstrate(state)
		scope.TargetBranchIDs = compactStrings(scope.SupportingBranchIDs)
		scope.ProductScope = collectScopeProducts(state, scope.TargetBranchIDs)
		scope.SelectionReason = "fallback_substrate"
		return scope
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].score > candidates[j].score
	})
	top := candidates[0].score
	targets := []string{candidates[0].id}
	for _, c := range candidates[1:] {
		if len(targets) >= 2 {
			break
		}
		if c.score+1 >= top {
			targets = append(targets, c.id)
		}
	}
	supportSet := map[string]struct{}{}
	for _, id := range targets {
		supportSet[id] = struct{}{}
		if b := state.Branches[id]; b != nil {
			for _, dep := range b.Branch.DependsOn {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				if db := state.Branches[dep]; db != nil && (db.Status == BranchStatusSupported || db.Status == BranchStatusDone) {
					supportSet[dep] = struct{}{}
				}
			}
		}
	}
	support := make([]string, 0, len(supportSet))
	for _, id := range state.BranchOrder {
		if _, ok := supportSet[id]; ok {
			support = append(support, id)
		}
	}
	scope.TargetBranchIDs = compactStrings(targets)
	scope.SupportingBranchIDs = compactStrings(support)
	scope.ProductScope = collectScopeProducts(state, scope.TargetBranchIDs)
	scope.SelectionReason = "scored_by_readiness_and_overlap"
	return scope
}

func inferBranchesForPathSubstrate(state *InvestigationState) []string {
	if state == nil {
		return nil
	}
	var out []string
	for _, bid := range state.BranchOrder {
		b := state.Branches[bid]
		if b == nil {
			continue
		}
		if b.Status == BranchStatusSupported || b.Status == BranchStatusActive {
			if b.Branch.RequiresPath || len(collectBranchResearchProducts(state, bid)) > 0 || len(b.Branch.ProductHints) > 0 {
				out = append(out, bid)
			}
		}
	}
	return compactStrings(out)
}

func inferPathAffectedBranches(state *InvestigationState, path PathResult) []string {
	if state == nil {
		return nil
	}
	products := map[string]struct{}{}
	for _, p := range path.QueriedProducts {
		p = strings.TrimSpace(p)
		if p != "" {
			products[p] = struct{}{}
		}
	}
	var out []string
	for _, bid := range state.BranchOrder {
		for _, rr := range state.ResearchResults {
			if rr.BranchID != bid {
				continue
			}
			for _, pr := range append(rr.ProductRefs, rr.RecommendedPathProducts...) {
				pr = strings.TrimSpace(pr)
				if pr != "" {
					if _, ok := products[pr]; ok {
						out = append(out, bid)
						goto nextBranch
					}
				}
			}
		}
	nextBranch:
	}
	out = compactStrings(out)
	if len(out) == 0 {
		if len(path.QueriedProducts) == 0 {
			return nil
		}
		return inferBranchesForPathSubstrate(state)
	}
	return out
}

func inferRiskTargetBranches(state *InvestigationState, critic CriticResult) []string {
	if len(critic.RiskTargetBranchIDs) > 0 {
		return compactStrings(critic.RiskTargetBranchIDs)
	}
	if len(critic.RiskyClaims) == 0 || state == nil {
		return nil
	}
	claimTokens := tokenizeForMatching(strings.Join(critic.RiskyClaims, " "))
	var supported []string
	type candidate struct {
		id    string
		score int
	}
	candidates := []candidate{}
	for _, bid := range state.BranchOrder {
		b := state.Branches[bid]
		if b != nil && b.Status == BranchStatusSupported {
			supported = append(supported, bid)
			score := 0
			questionTokens := tokenizeForMatching(b.Branch.Question + " " + strings.Join(b.OpenQuestions, " "))
			score += tokenOverlapCount(claimTokens, questionTokens) * 2
			score += overlapCount(collectBranchResearchProducts(state, bid), critic.PathProducts) * 3
			score += overlapCount(b.Branch.ActorHints, critic.FocusActors) * 2
			score += overlapCount(b.Branch.GeoHints, critic.FocusGeos) * 2
			if rr := latestResearchForBranch(state, bid); rr != nil {
				score += tokenOverlapCount(claimTokens, tokenizeForMatching(rr.Summary)) * 2
			}
			if len(b.History) > 0 && b.History[len(b.History)-1].Action == "research" {
				score++
			}
			if score > 0 {
				candidates = append(candidates, candidate{id: bid, score: score})
			}
		}
	}
	if len(supported) == 1 {
		return supported
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].score > candidates[j].score
	})
	top := candidates[0].score
	out := []string{candidates[0].id}
	for _, c := range candidates[1:] {
		if c.score+1 >= top {
			out = append(out, c.id)
		}
	}
	return compactStrings(out)
}

func inferLandedBranches(state *InvestigationState, result MaterializeResult) []string {
	if state == nil || !result.Persisted {
		return nil
	}
	var readySet = map[string]struct{}{}
	for _, bid := range compactStrings(state.PathResult.SourceBranchIDs) {
		h := state.Hypotheses[bid]
		b := state.Branches[bid]
		if h == nil || b == nil || !h.MaterializationReady {
			continue
		}
		if b.Status == BranchStatusSupported || b.Status == BranchStatusActive {
			readySet[bid] = struct{}{}
		}
	}
	if len(readySet) > 0 {
		if len(readySet) > 1 {
			return nil
		}
		out := make([]string, 0, len(readySet))
		for _, bid := range state.BranchOrder {
			if _, ok := readySet[bid]; ok {
				out = append(out, bid)
			}
		}
		return compactStrings(out)
	}
	var ready []string
	for _, bid := range state.BranchOrder {
		h := state.Hypotheses[bid]
		b := state.Branches[bid]
		if h != nil && b != nil && h.MaterializationReady && (b.Status == BranchStatusSupported || b.Status == BranchStatusActive) {
			ready = append(ready, bid)
		}
	}
	if len(ready) == 1 {
		return ready
	}
	// Conservative fallback: if targeting is uncertain, do not complete branches.
	return nil
}

func branchDependenciesSatisfied(state *InvestigationState, branch *BranchRuntimeState) bool {
	if state == nil || branch == nil {
		return true
	}
	need := max(state.Plan.MaxResearchRounds, 1)
	for _, depID := range branch.Branch.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		dep := state.Branches[depID]
		if dep == nil {
			continue
		}
		switch dep.Status {
		case BranchStatusDone, BranchStatusCandidate, BranchStatusRejected, BranchStatusSupported:
			continue
		default:
			if dep.ResearchRounds < need {
				return false
			}
		}
	}
	return true
}

func shouldAttemptPath(state *InvestigationState) bool {
	if state == nil || state.PathAttempts >= 4 {
		return false
	}
	if state.LatestCritic.WithholdPath && !hasLocallyReadyPathCandidate(state) && !hasPendingAcceptedOps(state) {
		return false
	}
	if state.LatestCritic.MaterializePath {
		return true
	}
	if hasPendingAcceptedOps(state) {
		return true
	}
	if hasLocallyReadyPathCandidate(state) {
		return true
	}
	supported := 0
	for _, hypothesis := range state.Hypotheses {
		if hypothesis == nil {
			continue
		}
		if hypothesis.Status == HypothesisSupported || hypothesis.Status == HypothesisConfirmedMaterializable || hypothesis.LocalReady {
			supported++
		}
	}
	return supported > 0 && (state.PathAttempts == 0 || state.RepeatedGapCount > 0)
}

func shouldAttemptBridgeScan(state *InvestigationState) bool {
	if state == nil || state.BridgeAttempts >= 4 {
		return false
	}
	if len(state.ActionLog) > 0 && state.ActionLog[len(state.ActionLog)-1] == "bridge" {
		return false
	}
	if len(state.PathResult.OpenGaps) > 0 {
		return selectBridgeBranch(state) != ""
	}
	switch strings.ToLower(strings.TrimSpace(state.PathResult.ContinuityStatus)) {
	case "broken", "partial":
		return selectBridgeBranch(state) != ""
	}
	if rec := normalizeActionRecommendation(state.LatestRecommendedAction); rec.Action == "bridge" {
		return selectBridgeBranch(state) != ""
	}
	for _, branchID := range state.BranchOrder {
		if branchHasBridgePressure(state, branchID) && selectBridgeBranch(state) != "" {
			return true
		}
	}
	if !hasLocallyReadySubstrate(state) {
		return false
	}
	return false
}

func fillRecommendedActionFromGapDiagnosis(rec ActionRecommendation, diag GapDiagnosis) ActionRecommendation {
	rec = normalizeActionRecommendation(rec)
	diag = normalizeGapDiagnosis(diag)
	if diag.NextAction == "" {
		return rec
	}
	if rec.Action == "" {
		rec.Action = diag.NextAction
	}
	if rec.BranchID == "" {
		rec.BranchID = diag.BranchID
	}
	rec.EndpointA = firstNonEmpty(rec.EndpointA, diag.EndpointA)
	rec.EndpointB = firstNonEmpty(rec.EndpointB, diag.EndpointB)
	rec.Reason = firstNonEmpty(rec.Reason, diag.Summary, diag.Why)
	return normalizeActionRecommendation(rec)
}

func fillGapDiagnosisFromResearch(branch InvestigationBranch, result ResearchResult) GapDiagnosis {
	diag := normalizeGapDiagnosis(result.GapDiagnosis)
	if diag.BranchID == "" {
		diag.BranchID = firstNonEmpty(result.BranchID, branch.ID)
	}
	if diag.Kind == "" {
		switch result.RecommendedAction.Action {
		case "bridge":
			diag.Kind = "missing_middle"
		case "path":
			diag.Kind = "missing_verification"
		case "critic":
			diag.Kind = "missing_adjudication"
		case "research":
			if branch.BroadScan {
				diag.Kind = "missing_completeness"
			} else {
				diag.Kind = "missing_substrate"
			}
		default:
			if branch.BroadScan {
				diag.Kind = "missing_completeness"
			} else {
				diag.Kind = "missing_substrate"
			}
		}
	}
	if diag.NextAction == "" {
		switch diag.Kind {
		case "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer":
			diag.NextAction = "bridge"
		case "missing_verification":
			diag.NextAction = "path"
		case "missing_adjudication":
			diag.NextAction = "critic"
		case "missing_completeness", "missing_substrate":
			diag.NextAction = "research"
		case "no_gap":
			diag.NextAction = "stop"
		}
	}
	if diag.Summary == "" {
		switch diag.Kind {
		case "missing_middle":
			diag.Summary = "Research found plausible ends but did not explain what sits between them."
		case "missing_completeness":
			diag.Summary = "Research surfaced some routes but has not yet scanned whether materially different alternatives remain."
		case "missing_verification":
			diag.Summary = "Research produced a candidate chain that now needs deterministic verification."
		default:
			diag.Summary = "Research needs more substrate before the route can be resolved."
		}
	}
	if diag.Why == "" {
		diag.Why = firstNonEmpty(result.Summary, "Research did not fully close the current graph gap.")
	}
	if diag.EndpointA == "" {
		diag.EndpointA = result.RecommendedAction.EndpointA
	}
	if diag.EndpointB == "" {
		diag.EndpointB = result.RecommendedAction.EndpointB
	}
	if !diag.EndpointsKnown && (diag.EndpointA != "" || diag.EndpointB != "") {
		diag.EndpointsKnown = true
	}
	return normalizeGapDiagnosis(diag)
}

func fillGapDiagnosisFromBridge(branch InvestigationBranch, result BridgeResult) GapDiagnosis {
	diag := normalizeGapDiagnosis(result.GapDiagnosis)
	if diag.BranchID == "" {
		diag.BranchID = firstNonEmpty(result.BranchID, branch.ID)
	}
	if diag.Kind == "" {
		switch result.RecommendedAction.Action {
		case "path":
			diag.Kind = "missing_verification"
		case "critic":
			diag.Kind = "missing_adjudication"
		case "research":
			if !result.BridgeExists {
				diag.Kind = "missing_substrate"
			} else {
				diag.Kind = "missing_middle"
			}
		default:
			if result.BridgeExists {
				switch strings.ToLower(strings.TrimSpace(result.BridgeKind)) {
				case "institutional", "distributor", "contract_chain":
					diag.Kind = "missing_institutional_layer"
				case "regulatory", "customs_route", "licensing":
					diag.Kind = "missing_regulatory_layer"
				case "ownership":
					diag.Kind = "missing_ownership_layer"
				case "refining", "transform", "toll_conversion":
					diag.Kind = "missing_transform_chain"
				default:
					diag.Kind = "missing_middle"
				}
			} else {
				diag.Kind = "missing_middle"
			}
		}
	}
	if diag.NextAction == "" {
		switch result.RecommendedAction.Action {
		case "path", "critic", "research", "stop":
			diag.NextAction = result.RecommendedAction.Action
		default:
			if result.BridgeExists {
				diag.NextAction = "path"
			} else {
				diag.NextAction = "research"
			}
		}
	}
	if diag.Summary == "" {
		diag.Summary = firstNonEmpty(result.Summary, "Bridge articulated the structural connection between the current endpoints.")
	}
	if diag.Why == "" {
		diag.Why = firstNonEmpty(result.BridgeQuestion, "The runtime needed a structural explanation of what sits between the current endpoints.")
	}
	diag.EndpointA = firstNonEmpty(diag.EndpointA, result.EndpointA)
	diag.EndpointB = firstNonEmpty(diag.EndpointB, result.EndpointB)
	diag.EndpointsKnown = diag.EndpointsKnown || result.EndpointA != "" || result.EndpointB != ""
	return normalizeGapDiagnosis(diag)
}

func fillGapDiagnosisFromCritic(plan InvestigationPlan, result CriticResult) GapDiagnosis {
	diag := normalizeGapDiagnosis(result.GapDiagnosis)
	if diag.Kind == "" {
		switch result.RecommendedAction.Action {
		case "bridge":
			diag.Kind = "missing_middle"
		case "path", "materialize":
			diag.Kind = "missing_verification"
		case "research":
			if result.CompletenessAssessment.Status != "" && !result.CompletenessAssessment.NoMaterialAlternatives {
				diag.Kind = "missing_completeness"
			} else {
				diag.Kind = "missing_substrate"
			}
		case "critic":
			diag.Kind = "missing_adjudication"
		case "stop":
			diag.Kind = "no_gap"
		default:
			if len(result.RiskyClaims) > 0 {
				diag.Kind = "missing_adjudication"
			}
		}
	}
	if diag.NextAction == "" {
		diag.NextAction = firstNonEmpty(result.RecommendedAction.Action, defaultActionForGapKind(diag.Kind))
	}
	if diag.Summary == "" {
		switch diag.Kind {
		case "missing_completeness":
			diag.Summary = "Critic believes the investigation has not yet ruled out materially different alternatives."
		case "missing_adjudication":
			diag.Summary = "Critic found unresolved contradictions or risky claims."
		default:
			diag.Summary = "Critic identified the dominant unresolved gap."
		}
	}
	if diag.Why == "" {
		diag.Why = firstNonEmpty(strings.Join(result.RiskyClaims, "; "), strings.Join(result.OpenQuestions, "; "), strings.Join(plan.PlanningRationale, "; "))
	}
	if diag.BranchID == "" && len(result.RiskTargetBranchIDs) > 0 {
		diag.BranchID = result.RiskTargetBranchIDs[0]
	}
	return normalizeGapDiagnosis(diag)
}

func derivePathGapDiagnosis(result PathResult) GapDiagnosis {
	diag := GapDiagnosis{
		BranchID: firstNonEmpty(firstString(result.SourceBranchIDs), ""),
	}
	switch strings.ToLower(strings.TrimSpace(result.ContinuityStatus)) {
	case "broken":
		diag.Kind = "missing_middle"
		diag.Summary = "Deterministic path tracing could not explain what sits between the current endpoints."
		diag.Why = strings.Join(limitStrings(result.OpenGaps, 3), "; ")
		diag.QuestionClass = "bridge_query"
		diag.NextAction = "bridge"
	case "partial":
		diag.Kind = "missing_middle"
		diag.Summary = "Deterministic path tracing found partial continuity but still left unresolved middle layers."
		diag.Why = strings.Join(limitStrings(result.OpenGaps, 3), "; ")
		diag.QuestionClass = "bridge_query"
		diag.NextAction = "bridge"
	case "landed":
		if len(result.MaterializedArtifactIDs) > 0 || strings.TrimSpace(result.AssemblyArtifactID) != "" {
			diag.Kind = "no_gap"
			diag.Summary = "Deterministic path tracing landed a usable chain."
			diag.QuestionClass = "verification"
			diag.NextAction = "materialize"
		} else {
			diag.Kind = "missing_adjudication"
			diag.Summary = "Deterministic path tracing produced a chain that still needs validation."
			diag.QuestionClass = "adjudication"
			diag.NextAction = "critic"
		}
	default:
		if len(result.OpenGaps) > 0 {
			diag.Kind = "missing_middle"
			diag.Summary = "Deterministic tracing ended with unresolved path gaps."
			diag.Why = strings.Join(limitStrings(result.OpenGaps, 3), "; ")
			diag.QuestionClass = "bridge_query"
			diag.NextAction = "bridge"
		} else {
			diag.Kind = "missing_substrate"
			diag.Summary = "Deterministic tracing produced no usable scope to verify."
			diag.QuestionClass = "space_scan"
			diag.NextAction = "research"
		}
	}
	return normalizeGapDiagnosis(diag)
}

func defaultActionForGapKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer":
		return "bridge"
	case "missing_verification":
		return "path"
	case "missing_adjudication":
		return "critic"
	case "missing_completeness", "missing_substrate":
		return "research"
	case "no_gap":
		return "stop"
	default:
		return ""
	}
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}

func derivePathRecommendedAction(result PathResult) ActionRecommendation {
	rec := ActionRecommendation{
		ProductHints: compactStrings(append([]string{}, result.QueriedProducts...)),
	}
	if len(result.SourceBranchIDs) > 0 {
		rec.BranchID = strings.TrimSpace(result.SourceBranchIDs[0])
	}
	switch strings.ToLower(strings.TrimSpace(result.ContinuityStatus)) {
	case "broken":
		rec.Action = "bridge"
		rec.FailureMode = "path_continuity_gap"
		rec.Reason = "Deterministic path tracing could not connect the current route and left unresolved continuity gaps."
		rec.Question = "What intermediate nodes, processors, logistics legs, or contracts sit between the current endpoints?"
		rec.RequiresPath = true
	case "partial":
		rec.Action = "bridge"
		rec.FailureMode = "partial_path_gap"
		rec.Reason = "Deterministic path tracing found some substrate but still left a missing-middle or target-binding gap."
		rec.Question = "Which missing-middle steps must be added to close the partially traced route?"
		rec.RequiresPath = true
	case "landed":
		if len(result.MaterializedArtifactIDs) > 0 || strings.TrimSpace(result.AssemblyArtifactID) != "" {
			rec.Action = "materialize"
			rec.FailureMode = "path_ready_for_landing"
			rec.Reason = "Deterministic path tracing produced artifacts that are ready for landing."
		} else {
			rec.Action = "critic"
			rec.FailureMode = "path_needs_validation"
			rec.Reason = "Path tracing produced route evidence; validate completeness and whether a materially different route remains."
		}
	default:
		if len(result.OpenGaps) > 0 {
			rec.Action = "bridge"
			rec.FailureMode = "path_unknown_with_gaps"
			rec.Reason = "Path tracing ended in an unknown continuity state with unresolved gaps."
			rec.Question = "What missing-middle structure explains the unresolved path gap?"
			rec.RequiresPath = true
		} else if len(result.TraceDigests) == 0 {
			rec.Action = "research"
			rec.FailureMode = "insufficient_path_scope"
			rec.Reason = "Deterministic tracing produced no usable traces; gather additional substrate before tracing again."
			rec.Question = "Which upstream, midstream, or market nodes are still missing before deterministic path tracing can continue?"
		}
	}
	return normalizeActionRecommendation(rec)
}

// hasPendingAcceptedOps returns true if there are accepted (critic-approved) ops
// awaiting verification by path.
func hasPendingAcceptedOps(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	for _, op := range state.PendingGraphOps {
		if op.Status == "accepted" || op.Status == "accepted_target" {
			return true
		}
	}
	return false
}

func hasLocallyReadySubstrate(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	for _, h := range state.Hypotheses {
		if h != nil && h.LocalReady {
			return true
		}
	}
	for _, op := range state.PendingGraphOps {
		switch op.Status {
		case "accepted_local", "accepted", "accepted_target", "verified_local", "verified":
			return true
		}
	}
	return false
}

func hasLocallyReadyPathCandidate(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	for branchID, h := range state.Hypotheses {
		if h == nil || !h.LocalReady {
			continue
		}
		switch h.PursuitDecision {
		case BranchPursuitPivot, BranchPursuitPark, BranchPursuitDrop:
			continue
		}
		if h.TargetBound || h.PursuitDecision == BranchPursuitPress || h.TargetRelevance >= 0.45 {
			return true
		}
		if b := state.Branches[branchID]; b != nil && b.Branch.RequiresPath {
			return true
		}
	}
	for _, op := range state.PendingGraphOps {
		if !op.LocalReady {
			continue
		}
		if op.TargetBound || op.Status == "accepted" || op.Status == "accepted_target" {
			return true
		}
	}
	return false
}

func branchNeedsBridgeScan(state *InvestigationState, branchID string) bool {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return false
	}
	branch := state.Branches[branchID]
	h := state.Hypotheses[branchID]
	if branch == nil || h == nil {
		return false
	}
	switch branch.Status {
	case BranchStatusDone, BranchStatusRejected, BranchStatusBlocked, BranchStatusCandidate:
		return false
	}
	switch h.PursuitDecision {
	case BranchPursuitPark, BranchPursuitDrop:
		return false
	}
	if !h.LocalReady && !branchHasUsablePendingOps(state, branchID) {
		return false
	}
	if !branchPathGapHit(state, branchID) && len(state.PathResult.OpenGaps) == 0 && !branchHasBridgePressure(state, branchID) {
		return false
	}
	if h.TargetBound && !branchHasBridgePressure(state, branchID) && !branchPathGapHit(state, branchID) && len(branch.OpenQuestions) == 0 {
		return false
	}
	if branch.Branch.RequiresPath {
		return true
	}
	if len(branch.OpenQuestions) > 0 {
		return true
	}
	return branchHasLocalOnlyOps(state, branchID)
}

func branchHasBridgePressure(state *InvestigationState, branchID string) bool {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return false
	}
	branch := state.Branches[branchID]
	h := state.Hypotheses[branchID]
	if branch == nil {
		return false
	}
	if rec := normalizeActionRecommendation(state.LatestRecommendedAction); rec.Action == "bridge" && firstNonEmpty(rec.BranchID, branchID) == branchID {
		return true
	}
	if h == nil || !h.LocalReady {
		return false
	}
	if !branch.Branch.RequiresPath && h.TargetRelevance < 0.45 && h.PursuitDecision != BranchPursuitPress {
		return false
	}
	roundNeed := max(state.Plan.MaxResearchRounds, 1)
	if branch.ResearchRounds < roundNeed {
		return false
	}
	if branch.MarginalGainStreak >= 1 || branch.ConsecutiveGapHits >= 1 {
		return true
	}
	if len(branch.OpenQuestions) > 0 && branch.ResearchRounds >= roundNeed+1 {
		return true
	}
	return branchHasLocalOnlyOps(state, branchID)
}

func branchHasLocalOnlyOps(state *InvestigationState, branchID string) bool {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return false
	}
	for _, op := range state.PendingGraphOps {
		if strings.TrimSpace(op.BranchID) != strings.TrimSpace(branchID) {
			continue
		}
		switch op.Status {
		case "accepted_local", "verified_local":
			return true
		}
	}
	return false
}

func selectBridgeBranch(state *InvestigationState) string {
	if state == nil {
		return ""
	}
	type candidate struct {
		id    string
		score float64
	}
	best := candidate{}
	for _, branchID := range state.BranchOrder {
		if !branchNeedsBridgeScan(state, branchID) {
			continue
		}
		score := branchScoreCached(state, branchID)
		branch := state.Branches[branchID]
		if branch != nil && branch.Branch.RequiresPath {
			score += 0.20
		}
		if branchHasLocalOnlyOps(state, branchID) {
			score += 0.18
		}
		if branchPathGapHit(state, branchID) {
			score += 0.24
		}
		if branchHasBridgePressure(state, branchID) {
			score += 0.22
		}
		if state.Hypotheses[branchID] != nil && !state.Hypotheses[branchID].TargetBound {
			score += 0.12
		}
		if score > best.score {
			best = candidate{id: branchID, score: score}
		}
	}
	if best.id == "" && len(state.PathResult.OpenGaps) > 0 {
		for _, branchID := range state.BranchOrder {
			branch := state.Branches[branchID]
			h := state.Hypotheses[branchID]
			if branch == nil {
				continue
			}
			switch branch.Status {
			case BranchStatusDone, BranchStatusRejected, BranchStatusBlocked, BranchStatusCandidate:
				continue
			}
			if h != nil {
				switch h.PursuitDecision {
				case BranchPursuitPark, BranchPursuitDrop:
					continue
				}
			}
			score := branchScoreCached(state, branchID)
			if branch.Branch.RequiresPath {
				score += 0.20
			}
			if score > best.score {
				best = candidate{id: branchID, score: score}
			}
		}
	}
	return best.id
}

func branchPathGapHit(state *InvestigationState, branchID string) bool {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return false
	}
	for _, id := range state.PathResult.SourceBranchIDs {
		if strings.TrimSpace(id) == strings.TrimSpace(branchID) && len(state.PathResult.OpenGaps) > 0 {
			return true
		}
	}
	return false
}

func shouldMaterialize(state *InvestigationState) bool {
	if state == nil || state.MaterializeAttempts >= 2 {
		return false
	}
	if state.LatestCritic.WithholdMaterialization {
		return false
	}
	if len(state.PathResult.MaterializedArtifactIDs) > len(state.MaterializeResult.ArtifactIDs) {
		return true
	}
	return strings.TrimSpace(state.PathResult.AssemblyArtifactID) != "" && len(state.MaterializeResult.ArtifactIDs) == 0
}

// computeBranchConfidence derives confidence by blending the model's assessment
// with domain-agnostic structural quality signals. The model is genuinely better
// at evaluating whether evidence supports a claim than any heuristic, but it
// should not be able to claim high confidence with zero grounding.
//
// Weights:
//   - model assessment: 0.40 (strong but not absolute)
//   - canonical refs (any path starting with /): 0.08/ref, cap 0.25
//   - evidence points: 0.07/point, cap 0.20
//   - well-formed proposed graph ops: 0.05/op, cap 0.15
//   - graph verification (path confirmed ops): +0.15
//   - convergence (multiple rounds agree): +0.10
func computeBranchConfidence(state *InvestigationState, branchID string, result ResearchResult) float64 {
	var score float64

	canonicalCount := 0
	for _, ref := range result.CanonicalRefs {
		if isCanonicalPath(ref) {
			canonicalCount++
		}
	}
	for _, ref := range result.ProductRefs {
		if isCanonicalPath(ref) {
			canonicalCount++
		}
	}
	if canonicalCount > 0 {
		score += math.Min(float64(canonicalCount)*0.08, 0.25)
	}

	evidenceCount := len(compactStrings(result.EvidencePoints))
	if evidenceCount > 0 {
		score += math.Min(float64(evidenceCount)*0.07, 0.20)
	}

	if len(result.ProposedOps) > 0 {
		wellFormed := 0
		for _, op := range result.ProposedOps {
			if isCanonicalPath(op.SubjectPath) {
				wellFormed++
			}
		}
		score += math.Min(float64(wellFormed)*0.05, 0.15)
	}

	if state != nil {
		for _, op := range state.PendingGraphOps {
			if op.BranchID == branchID && op.Status == "verified" {
				score += 0.15
				break
			}
		}
	}

	if state != nil {
		branch := state.Branches[branchID]
		if branch != nil && branch.ResearchRounds > 0 {
			h := state.Hypotheses[branchID]
			if h != nil && h.PriorConfidence > 0 && math.Abs(result.Confidence-h.PriorConfidence) < 0.1 {
				score += 0.10
			}
		}
	}

	score += result.Confidence * 0.40

	return clamp01(score)
}

// isCanonicalPath checks whether a string looks like a canonical graph path
// (e.g. /product/..., /org/..., /facility/..., /loc/..., /regulation/...).
// Domain-agnostic: any slash-prefixed structured path qualifies.
func isCanonicalPath(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 2 && s[0] == '/' && strings.Contains(s[1:], "/")
}

func applyResearchToState(state *InvestigationState, branchID string, result ResearchResult) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "research")
	state.ResearchResults = append(state.ResearchResults, result)
	branch := state.Branches[branchID]
	if branch == nil {
		state.StalledTurns++
		return
	}
	branch.ResearchRounds++

	computed := computeBranchConfidence(state, branchID, result)
	branch.LastConfidence = computed

	branch.OpenQuestions = compactStrings(append(branch.OpenQuestions, result.OpenQuestions...))
	state.OpenQuestions = compactStrings(append(state.OpenQuestions, result.OpenQuestions...))
	for _, q := range result.OpenQuestions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		oid := fmt.Sprintf("oq-%s-%d", branchID, len(state.StructuredOpenQuestions)+1)
		state.StructuredOpenQuestions = append(state.StructuredOpenQuestions, OpenQuestionEntry{
			ID:                 oid,
			Text:               q,
			BlockedByBranchIDs: []string{branchID},
			Source:             "research",
		})
	}
	if computed >= 0.75 {
		branch.Status = BranchStatusSupported
	} else if computed >= 0.45 {
		branch.Status = BranchStatusActive
	} else {
		branch.Status = BranchStatusWeak
		branch.ConsecutiveGapHits++
		state.RepeatedGapCount++
	}
	if branch.ConsecutiveGapHits >= max(state.Plan.MaxResearchRounds, 1) {
		branch.Status = BranchStatusCandidate
	}
	hypothesis := state.Hypotheses[branchID]
	if hypothesis != nil {
		hypothesis.RefinementCount++
		if hypothesis.PriorConfidence > 0 && math.Abs(computed-hypothesis.PriorConfidence) < 0.05 {
			branch.MarginalGainStreak++
		} else {
			branch.MarginalGainStreak = 0
		}
		hypothesis.PriorConfidence = computed
		if len(result.TestCriteria) > 0 {
			hypothesis.TestCriteria = compactStrings(append(hypothesis.TestCriteria, result.TestCriteria...))
		} else if len(branch.Branch.TestCriteria) > 0 {
			hypothesis.TestCriteria = compactStrings(append(hypothesis.TestCriteria, branch.Branch.TestCriteria...))
		}
		hypothesis.Confidence = computed
		hypothesis.Evidence = compactStrings(append(hypothesis.Evidence, result.EvidencePoints...))
		hypothesis.LocalReady = computed >= 0.70 && (len(result.ProposedOps) > 0 || len(result.CanonicalRefs) > 0 || len(result.ProductRefs) > 0)
		hypothesis.TargetBound = false
		hypothesis.MaterializationReady = false
		switch {
		case branch.Status == BranchStatusSupported:
			hypothesis.Status = HypothesisSupported
		case branch.Status == BranchStatusWeak:
			hypothesis.Status = HypothesisWeaklySupported
		default:
			hypothesis.Status = HypothesisUnderInvestigation
		}
	}
	if branch.MarginalGainStreak >= 2 {
		branch.Status = BranchStatusCandidate
		if hypothesis != nil {
			hypothesis.Status = HypothesisWeaklySupported
		}
		state.StopReason = firstNonEmpty(state.StopReason, "marginal_information_gain_stall")
	}
	branch.History = append(branch.History, BranchHistoryEntry{
		Turn:          state.Turn,
		Action:        "research",
		Note:          strings.TrimSpace(result.Summary),
		OpenQuestions: compactStrings(result.OpenQuestions),
	})

	for i := range result.ProposedOps {
		op := result.ProposedOps[i]
		op.BranchID = branchID
		op.SourceTurn = state.Turn
		if op.Status == "" {
			op.Status = "proposed"
		}
		op.Confidence = computeGraphOpConfidence(op)
		op.LocalReady = op.Confidence >= 0.50
		state.PendingGraphOps = append(state.PendingGraphOps, op)
	}
	if branch.Branch.BroadScan {
		state.Completeness.BroadScanCompleted = true
		state.Completeness.ExplorationStatus = "in_progress"
		state.Completeness.CompletionRationale = firstNonEmpty(strings.TrimSpace(result.Summary), state.Completeness.CompletionRationale)
		state.Completeness.KnownAlternativeRoutes = compactStrings(append(state.Completeness.KnownAlternativeRoutes, result.OpenQuestions...))
	}
	applyGapDiagnosisToState(state, "research", branchID, result.GapDiagnosis)
	applyActionRecommendationToState(state, "research", branchID, result.RecommendedAction)
	refreshBranchPursuitState(state, branchID, state.LatestCritic)
}

func applyBridgeToState(state *InvestigationState, branchID string, result BridgeResult) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "bridge")
	state.BridgeAttempts++
	result.BranchID = firstNonEmpty(branchID, result.BranchID)
	state.LatestBridge = result
	state.BridgeResults = append(state.BridgeResults, result)

	branch := state.Branches[branchID]
	hypothesis := state.Hypotheses[branchID]
	if branch == nil || hypothesis == nil {
		state.StalledTurns++
		return
	}
	branch.OpenQuestions = compactStrings(append(branch.OpenQuestions, result.OpenQuestions...))
	state.OpenQuestions = compactStrings(append(state.OpenQuestions, result.OpenQuestions...))
	for _, q := range result.OpenQuestions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		oid := fmt.Sprintf("oq-bridge-%s-%d", branchID, len(state.StructuredOpenQuestions)+1)
		state.StructuredOpenQuestions = append(state.StructuredOpenQuestions, OpenQuestionEntry{
			ID:                 oid,
			Text:               q,
			BlockedByBranchIDs: []string{branchID},
			Source:             "bridge",
		})
	}
	branch.History = append(branch.History, BranchHistoryEntry{
		Turn:          state.Turn,
		Action:        "bridge",
		Note:          strings.TrimSpace(result.Summary),
		OpenQuestions: compactStrings(result.OpenQuestions),
	})

	if result.Confidence > 0 {
		branch.LastConfidence = math.Max(branch.LastConfidence, clamp01(result.Confidence))
		hypothesis.Confidence = math.Max(hypothesis.Confidence, clamp01(result.Confidence))
	}
	hypothesis.LocalReady = hypothesis.LocalReady || result.BridgeExists || len(result.ProposedOps) > 0 || len(result.IntermediateNodes) > 0
	if strings.TrimSpace(result.Summary) != "" {
		hypothesis.RouteHypothesis = firstNonEmpty(strings.TrimSpace(result.Summary), hypothesis.RouteHypothesis)
	}
	if result.BridgeExists {
		hypothesis.TargetRelevance = math.Max(hypothesis.TargetRelevance, math.Max(0.45, clamp01(result.Confidence)))
		if len(result.IntermediateNodes) > 0 || len(result.ProposedOps) > 0 {
			hypothesis.PursuitDecision = BranchPursuitPress
			hypothesis.PursuitReason = "Bridge scan found a plausible missing-middle structure connecting the current local substrate to a target-facing route."
		}
		state.NeedsReplan = false
		state.ReplanReason = ""
		state.RepeatedGapCount = 0
	} else {
		hypothesis.TargetBound = false
		if len(result.SuggestedSubscans) > 0 {
			hypothesis.PursuitDecision = BranchPursuitPivot
			hypothesis.PursuitReason = "Bridge scan did not validate the current route, but it suggested a more plausible continuation to investigate."
		} else if hypothesis.LocalReady {
			hypothesis.PursuitDecision = BranchPursuitPark
			hypothesis.PursuitReason = "Local substrate is worth preserving, but this route is not yet bridgeable to the objective."
		}
		state.RepeatedGapCount++
	}

	for i := range result.ProposedOps {
		op := result.ProposedOps[i]
		op.BranchID = branchID
		op.SourceTurn = state.Turn
		if op.Status == "" {
			op.Status = "proposed"
		}
		if op.Confidence == 0 {
			op.Confidence = computeGraphOpConfidence(op)
		}
		op.LocalReady = op.LocalReady || op.Confidence >= 0.50
		state.PendingGraphOps = append(state.PendingGraphOps, op)
	}
	for idx, subscan := range result.SuggestedSubscans {
		id := firstNonEmpty(subscan.ID, fmt.Sprintf("bridge-%s-%d", branchID, idx+1))
		subscan.ID = id
		if state.Branches[id] != nil {
			continue
		}
		subscan.DependsOn = compactStrings(append(subscan.DependsOn, branchID))
		subscan = normalizeBranchForMission(state.Role, subscan)
		state.Branches[id] = &BranchRuntimeState{Branch: subscan, Status: BranchStatusProposed, History: []BranchHistoryEntry{}}
		state.BranchOrder = append(state.BranchOrder, id)
		state.Hypotheses[id] = &InvestigationHypothesis{
			ID:               "hypothesis:" + id,
			BranchID:         id,
			Statement:        firstNonEmpty(subscan.Question, state.Plan.Goal),
			Status:           HypothesisProposed,
			ExpectedInfoGain: subscan.ExpectedInfoGain,
			PursuitDecision:  BranchPursuitPress,
			PursuitReason:    "Bridge scan suggested this subscan as the next best continuation.",
		}
	}
	if len(result.RouteSketches) > 0 {
		state.Completeness.KnownAlternativeRoutes = compactStrings(append(state.Completeness.KnownAlternativeRoutes, result.RouteSketches...))
	}
	applyGapDiagnosisToState(state, "bridge", branchID, result.GapDiagnosis)
	applyActionRecommendationToState(state, "bridge", branchID, result.RecommendedAction)
	refreshBranchPursuitState(state, branchID, state.LatestCritic)
	if result.BridgeExists {
		hypothesis.PursuitDecision = BranchPursuitPress
		hypothesis.PursuitReason = "Bridge scan produced a plausible missing-middle continuation worth pursuing."
	}
}

func refreshBranchPursuitState(state *InvestigationState, branchID string, critic CriticResult) {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return
	}
	applyBranchAssessmentsToState(state, synthesizeBranchAssessmentsForBranches(state, critic, []string{branchID}))
}

func synthesizeBranchAssessments(state *InvestigationState, critic CriticResult) []BranchAssessment {
	if state == nil {
		return nil
	}
	return synthesizeBranchAssessmentsForBranches(state, critic, append([]string{}, state.BranchOrder...))
}

func synthesizeBranchAssessmentsForBranches(state *InvestigationState, critic CriticResult, branchIDs []string) []BranchAssessment {
	if state == nil || len(branchIDs) == 0 {
		return nil
	}
	riskTargets := map[string]struct{}{}
	for _, id := range inferRiskTargetBranches(state, critic) {
		if id = strings.TrimSpace(id); id != "" {
			riskTargets[id] = struct{}{}
		}
	}
	out := make([]BranchAssessment, 0, len(branchIDs))
	for _, branchID := range compactStrings(branchIDs) {
		branch := state.Branches[branchID]
		if branch == nil {
			continue
		}
		hypothesis := state.Hypotheses[branchID]
		localReady := branch.LastConfidence >= 0.70 || branchHasUsablePendingOps(state, branchID)
		if hypothesis != nil && hypothesis.LocalReady {
			localReady = true
		}
		relevance := computeBranchTargetRelevance(state, branchID, critic)
		suggested := suggestedBranchIDsForBranch(state, branchID, critic)
		decision := BranchPursuitPark
		reason := "Branch is locally valid but not currently the highest-value path to the objective."
		switch {
		case branch.Status == BranchStatusDone || branch.Status == BranchStatusRejected:
			decision = BranchPursuitDrop
			reason = "Branch is already closed and should not consume additional turns."
		case branch.Status == BranchStatusSupported && !hasRiskTarget(branchID, riskTargets):
			if relevance >= 0.35 {
				decision = BranchPursuitPress
				reason = "Supported branch remains a viable route toward the objective."
			} else {
				decision = BranchPursuitPark
				reason = "Supported branch should be preserved even if it is not the immediate next turn."
			}
		case branch.Status == BranchStatusWeak && !localReady && relevance < 0.30:
			decision = BranchPursuitDrop
			reason = "Branch remains weak and shows little evidence of leading to the target."
		case hasRiskTarget(branchID, riskTargets):
			if len(suggested) > 0 {
				decision = BranchPursuitPivot
				reason = "Branch produced useful signal, but critic indicates the target route should pivot toward a stronger continuation."
			} else if localReady && (relevance >= 0.45 || branch.Branch.RequiresPath) {
				decision = BranchPursuitPress
				reason = "Branch keeps producing useful local substrate and no better route is yet available, so it should be pressed further."
			} else if localReady {
				decision = BranchPursuitPark
				reason = "Local facts should be preserved, but target binding for this route is currently weak."
			} else {
				decision = BranchPursuitDrop
				reason = "Risk-targeted branch lacks enough local substrate to justify more turns."
			}
		case relevance >= 0.62 || (branch.Branch.RequiresPath && relevance >= 0.40):
			decision = BranchPursuitPress
			reason = "Branch appears highly relevant to the objective and remains worth driving further."
		case localReady && relevance >= 0.40 && len(suggested) > 0:
			decision = BranchPursuitPivot
			reason = "Local facts are good, but the best route to the objective likely continues through a neighboring branch."
		case localReady:
			decision = BranchPursuitPark
			reason = "Local facts are worth keeping, but this branch is not the best next move toward the target."
		case relevance >= 0.40:
			decision = BranchPursuitPivot
			reason = "Branch is still relevant, but should be approached through a higher-value adjacent route."
		default:
			decision = BranchPursuitDrop
			reason = "Branch does not currently justify more investigation budget."
		}
		out = append(out, BranchAssessment{
			BranchID:           branchID,
			TargetRelevance:    relevance,
			PursuitDecision:    decision,
			PursuitReason:      reason,
			RouteHypothesis:    branchRouteHypothesis(state, branchID, critic),
			AlternativeRoute:   branchAlternativeRoute(state, branchID, suggested),
			SuggestedBranchIDs: suggested,
		})
	}
	return out
}

func applyBranchAssessmentsToState(state *InvestigationState, assessments []BranchAssessment) {
	if state == nil || len(assessments) == 0 {
		return
	}
	for _, assessment := range assessments {
		branchID := strings.TrimSpace(assessment.BranchID)
		hypothesis := state.Hypotheses[branchID]
		branch := state.Branches[branchID]
		if hypothesis == nil || branch == nil {
			continue
		}
		hypothesis.TargetRelevance = clamp01(assessment.TargetRelevance)
		hypothesis.PursuitDecision = normalizeBranchPursuitDecision(assessment.PursuitDecision, hypothesis.LocalReady, hypothesis.TargetRelevance)
		hypothesis.PursuitReason = firstNonEmpty(strings.TrimSpace(assessment.PursuitReason), hypothesis.PursuitReason)
		hypothesis.RouteHypothesis = firstNonEmpty(strings.TrimSpace(assessment.RouteHypothesis), hypothesis.RouteHypothesis, branch.Branch.Question)
		hypothesis.AlternativeRoute = firstNonEmpty(strings.TrimSpace(assessment.AlternativeRoute), hypothesis.AlternativeRoute)
		hypothesis.SuggestedBranchIDs = compactStrings(append([]string{}, assessment.SuggestedBranchIDs...))
		switch hypothesis.PursuitDecision {
		case BranchPursuitDrop:
			if !hypothesis.LocalReady && branch.Status != BranchStatusDone && branch.Status != BranchStatusRejected && branch.Status != BranchStatusBlocked {
				branch.Status = BranchStatusCandidate
			}
		case BranchPursuitPress:
			if branch.Status == BranchStatusProposed && hypothesis.TargetRelevance >= 0.45 {
				branch.Status = BranchStatusActive
			}
		case BranchPursuitPivot:
			if branch.Status == BranchStatusWeak && hypothesis.TargetRelevance >= 0.35 {
				branch.Status = BranchStatusActive
			}
		}
	}
}

func synthesizeCompletenessAssessment(state *InvestigationState, critic CriticResult) CompletenessAssessment {
	assessment := CompletenessAssessment{
		Status:                 ternary(state != nil && state.Completeness.BroadScanCompleted, "in_progress", "pending"),
		Reason:                 "Broad scan and alternative-route coverage have not yet been explicitly closed.",
		AlternativeRoutes:      nil,
		MissingAngles:          nil,
		BroadScanCompleted:     state != nil && state.Completeness.BroadScanCompleted,
		NoMaterialAlternatives: false,
	}
	if state == nil || !state.Completeness.BroadScanRequired {
		assessment.Status = "satisfied"
		assessment.Reason = "Broad scan is not required for this investigation."
		assessment.NoMaterialAlternatives = true
		return assessment
	}
	alternatives := compactStrings(append([]string{}, state.Completeness.KnownAlternativeRoutes...))
	missing := compactStrings(append([]string{}, state.Completeness.MissingAngles...))
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		h := state.Hypotheses[branchID]
		if branch == nil || h == nil {
			continue
		}
		if branch.Branch.BroadScan {
			continue
		}
		switch h.PursuitDecision {
		case BranchPursuitPress, BranchPursuitPivot:
			alternatives = append(alternatives, strings.TrimSpace(branch.Branch.Question))
		case BranchPursuitPark:
			missing = append(missing, strings.TrimSpace(branch.Branch.Question))
		}
	}
	alternatives = compactStrings(alternatives)
	missing = compactStrings(missing)
	assessment.AlternativeRoutes = alternatives
	assessment.MissingAngles = missing
	switch {
	case !assessment.BroadScanCompleted:
		assessment.Status = "pending"
		assessment.Reason = "Broad scan has not yet run."
	case len(alternatives) == 0 && len(missing) == 0:
		assessment.Status = "satisfied"
		assessment.Reason = "No additional material alternative routes remain after broad scan."
		assessment.NoMaterialAlternatives = true
	default:
		assessment.Status = "needs_follow_on"
		assessment.Reason = "Broad scan found or preserved alternative routes that still need explicit follow-up or dismissal."
	}
	if strings.TrimSpace(critic.CompletenessAssessment.Reason) != "" {
		assessment.Reason = strings.TrimSpace(critic.CompletenessAssessment.Reason)
	}
	return assessment
}

func applyCompletenessAssessmentToState(state *InvestigationState, assessment CompletenessAssessment) {
	if state == nil {
		return
	}
	state.Completeness.BroadScanCompleted = state.Completeness.BroadScanCompleted || assessment.BroadScanCompleted
	state.Completeness.ExplorationStatus = firstNonEmpty(strings.TrimSpace(assessment.Status), state.Completeness.ExplorationStatus)
	state.Completeness.KnownAlternativeRoutes = compactStrings(append(state.Completeness.KnownAlternativeRoutes, assessment.AlternativeRoutes...))
	state.Completeness.MissingAngles = compactStrings(append(state.Completeness.MissingAngles, assessment.MissingAngles...))
	state.Completeness.NoMaterialAlternatives = state.Completeness.NoMaterialAlternatives || assessment.NoMaterialAlternatives
	state.Completeness.CompletionRationale = firstNonEmpty(strings.TrimSpace(assessment.Reason), state.Completeness.CompletionRationale)
}

func defaultGoodEnoughAssessment() GoodEnoughAssessment {
	return GoodEnoughAssessment{
		Status:       "insufficient",
		ClosureLevel: "insufficient",
		Reason:       "Critic has not yet committed that this result is good enough for this runtime tier.",
	}
}

func normalizeGoodEnoughAssessment(item GoodEnoughAssessment) GoodEnoughAssessment {
	item.Status = strings.TrimSpace(strings.ToLower(item.Status))
	item.ClosureLevel = strings.TrimSpace(strings.ToLower(item.ClosureLevel))
	item.Reason = strings.TrimSpace(item.Reason)
	item.OpenNonBlockingGaps = compactStrings(item.OpenNonBlockingGaps)
	item.TargetBranchIDs = compactStrings(item.TargetBranchIDs)
	return item
}

func mergeGoodEnoughAssessment(base, delta GoodEnoughAssessment) GoodEnoughAssessment {
	base = normalizeGoodEnoughAssessment(base)
	delta = normalizeGoodEnoughAssessment(delta)
	base.Status = firstNonEmpty(delta.Status, base.Status)
	base.ClosureLevel = firstNonEmpty(delta.ClosureLevel, base.ClosureLevel)
	base.StopNow = base.StopNow || delta.StopNow
	base.PersistWorkingPath = base.PersistWorkingPath || delta.PersistWorkingPath
	base.FurtherWorkLowValue = base.FurtherWorkLowValue || delta.FurtherWorkLowValue
	base.Reason = firstNonEmpty(delta.Reason, base.Reason)
	base.OpenNonBlockingGaps = compactStrings(append(base.OpenNonBlockingGaps, delta.OpenNonBlockingGaps...))
	base.TargetBranchIDs = compactStrings(append(base.TargetBranchIDs, delta.TargetBranchIDs...))
	return normalizeGoodEnoughAssessment(base)
}

func normalizeBranchPursuitDecision(decision BranchPursuitDecision, localReady bool, targetRelevance float64) BranchPursuitDecision {
	switch decision {
	case BranchPursuitPress, BranchPursuitPivot, BranchPursuitPark, BranchPursuitDrop:
		return decision
	}
	switch {
	case targetRelevance >= 0.62:
		return BranchPursuitPress
	case localReady && targetRelevance >= 0.35:
		return BranchPursuitPivot
	case localReady:
		return BranchPursuitPark
	default:
		return BranchPursuitDrop
	}
}

func hasActionablePursuitBranch(state *InvestigationState) bool {
	if state == nil {
		return false
	}
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		hypothesis := state.Hypotheses[branchID]
		if branch == nil || hypothesis == nil || !branchNeedsMoreResearch(state, branch) {
			continue
		}
		switch hypothesis.PursuitDecision {
		case BranchPursuitPress, BranchPursuitPivot:
			return true
		}
	}
	return false
}

func computeBranchTargetRelevance(state *InvestigationState, branchID string, critic CriticResult) float64 {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return 0
	}
	branch := state.Branches[branchID]
	if branch == nil {
		return 0
	}
	queryText := strings.Join([]string{
		state.Request.Query,
		state.Goal.Objective,
		state.Goal.PrimaryQuestion,
		state.Goal.TargetCanonicalPath,
		strings.Join(critic.PathProducts, " "),
		strings.Join(critic.FocusGeos, " "),
		strings.Join(critic.FocusActors, " "),
	}, " ")
	branchProducts := compactStrings(append(append([]string{}, branch.Branch.ProductHints...), collectBranchResearchProducts(state, branchID)...))
	branchText := strings.Join([]string{
		branch.Branch.Question,
		strings.Join(branchProducts, " "),
		strings.Join(branch.Branch.GeoHints, " "),
		strings.Join(branch.Branch.ActorHints, " "),
	}, " ")
	if rr := latestResearchForBranch(state, branchID); rr != nil {
		branchText = strings.Join([]string{branchText, rr.Summary, strings.Join(rr.EvidencePoints, " "), strings.Join(rr.OpenQuestions, " ")}, " ")
	}
	branchTokens := tokenizeForMatching(branchText)
	objectiveTokens := tokenizeForMatching(queryText)
	targetTokens := tokenizeForMatching(state.Goal.TargetCanonicalPath)
	criticTokens := tokenizeForMatching(strings.Join(critic.PathProducts, " ") + " " + strings.Join(critic.FocusGeos, " ") + " " + strings.Join(critic.FocusActors, " "))
	score := 0.0
	if strings.TrimSpace(queryText) == "" && branch.Branch.RequiresPath && (len(branchProducts) > 0 || branchHasUsablePendingOps(state, branchID)) {
		score += 0.45
	}
	if overlap := tokenOverlapCount(branchTokens, objectiveTokens); overlap > 0 {
		score += math.Min(float64(overlap)*0.05, 0.35)
	}
	if overlap := tokenOverlapCount(branchTokens, criticTokens); overlap > 0 {
		score += math.Min(float64(overlap)*0.06, 0.30)
	}
	if overlap := tokenOverlapCount(branchTokens, targetTokens); overlap > 0 {
		score += math.Min(float64(overlap)*0.07, 0.28)
	}
	if overlap := overlapCount(branch.Branch.GeoHints, extractCountryHints(state.Request.Query)); overlap > 0 {
		score += math.Min(float64(overlap)*0.10, 0.15)
	}
	if overlap := overlapCount(branchProducts, critic.PathProducts); overlap > 0 {
		score += math.Min(float64(overlap)*0.12, 0.20)
	}
	for _, product := range branchProducts {
		if strings.TrimSpace(product) == strings.TrimSpace(state.Goal.TargetCanonicalPath) && product != "" {
			score += 0.20
			break
		}
	}
	if branch.Branch.RequiresPath {
		score += 0.08
	}
	if h := state.Hypotheses[branchID]; h != nil && h.LocalReady {
		score += 0.05
	}
	return clamp01(score)
}

func suggestedBranchIDsForBranch(state *InvestigationState, branchID string, critic CriticResult) []string {
	if state == nil {
		return nil
	}
	out := []string{}
	for _, id := range compactStrings(critic.ReprioritizeBranchIDs) {
		if id != "" && id != branchID && state.Branches[id] != nil {
			out = append(out, id)
		}
	}
	for _, branch := range critic.NewBranches {
		id := strings.TrimSpace(branch.ID)
		if id != "" && id != branchID {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		for _, id := range state.BranchOrder {
			if id == branchID {
				continue
			}
			h := state.Hypotheses[id]
			if h != nil && h.TargetRelevance >= 0.55 {
				out = append(out, id)
			}
		}
	}
	return limitStrings(compactStrings(out), 3)
}

func branchRouteHypothesis(state *InvestigationState, branchID string, critic CriticResult) string {
	if state == nil {
		return ""
	}
	if rr := latestResearchForBranch(state, branchID); rr != nil && strings.TrimSpace(rr.Summary) != "" {
		return strings.TrimSpace(rr.Summary)
	}
	branch := state.Branches[branchID]
	if branch == nil {
		return ""
	}
	if len(critic.PathProducts) > 0 && branch.Branch.RequiresPath {
		return fmt.Sprintf("%s -> %s", strings.TrimSpace(branch.Branch.Question), strings.Join(limitStrings(critic.PathProducts, 3), " -> "))
	}
	return strings.TrimSpace(branch.Branch.Question)
}

func branchAlternativeRoute(state *InvestigationState, _ string, suggested []string) string {
	if state == nil || len(suggested) == 0 {
		return ""
	}
	next := state.Branches[suggested[0]]
	if next == nil {
		return ""
	}
	return strings.TrimSpace(next.Branch.Question)
}

func hasRiskTarget(branchID string, riskTargets map[string]struct{}) bool {
	_, ok := riskTargets[strings.TrimSpace(branchID)]
	return ok
}

// computeGraphOpConfidence scores an individual proposed graph op by its
// structural quality. Domain-agnostic: checks whether paths are canonical,
// edge type is specified, and rationale is present.
func computeGraphOpConfidence(op ProposedGraphOp) float64 {
	score := 0.0
	if isCanonicalPath(op.SubjectPath) {
		score += 0.30
	}
	if op.ObjectPath != "" && isCanonicalPath(op.ObjectPath) {
		score += 0.15
	}
	if strings.TrimSpace(op.EdgeType) != "" {
		score += 0.15
	}
	if strings.TrimSpace(op.Rationale) != "" {
		score += 0.20
	}
	if op.Kind == "confirm_edge" {
		score += 0.10
	}
	return clamp01(score)
}

func opCanBeLocallyClosed(op ProposedGraphOp) bool {
	switch strings.TrimSpace(op.Kind) {
	case "create_fragment", "mark_gap":
		return isCanonicalPath(op.SubjectPath)
	case "create_edge", "confirm_edge":
		return isCanonicalPath(op.SubjectPath) && isCanonicalPath(op.ObjectPath) && strings.TrimSpace(op.EdgeType) != ""
	default:
		return false
	}
}

func branchHasUsablePendingOps(state *InvestigationState, branchID string) bool {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return false
	}
	for _, op := range state.PendingGraphOps {
		if strings.TrimSpace(op.BranchID) != strings.TrimSpace(branchID) {
			continue
		}
		if op.Confidence >= 0.50 && opCanBeLocallyClosed(op) {
			return true
		}
	}
	return false
}

// hasPendingUnresolvedOps returns true if there are proposed/accepted ops that have
// not yet been verified, landed, or rejected.
func hasPendingUnresolvedOps(state *InvestigationState) bool {
	for _, op := range state.PendingGraphOps {
		switch op.Status {
		case "proposed", "accepted_local", "accepted":
			return true
		}
	}
	return false
}

func synthesizeGraphOpVerdicts(state *InvestigationState, critic CriticResult) []GraphOpVerdict {
	if state == nil || len(state.PendingGraphOps) == 0 {
		return nil
	}
	riskTargets := map[string]struct{}{}
	for _, id := range inferRiskTargetBranches(state, critic) {
		id = strings.TrimSpace(id)
		if id != "" {
			riskTargets[id] = struct{}{}
		}
	}
	verdicts := make([]GraphOpVerdict, 0, len(state.PendingGraphOps))
	for idx, op := range state.PendingGraphOps {
		if op.Status == "landed" || op.Status == "rejected" {
			continue
		}
		branch := state.Branches[op.BranchID]
		hyp := state.Hypotheses[op.BranchID]
		localReady := (hyp != nil && hyp.LocalReady) || (branch != nil && branch.LastConfidence >= 0.70) || opCanBeLocallyClosed(op)
		targetRisk := false
		if _, ok := riskTargets[strings.TrimSpace(op.BranchID)]; ok {
			targetRisk = true
		}
		verdict := "needs_research"
		reason := "more evidence needed before graph closure"
		switch {
		case strings.EqualFold(strings.TrimSpace(op.Kind), "mark_gap"):
			verdict = "accepted_local"
			reason = "gap is a valid local graph fact even without target binding"
		case !localReady || op.Confidence < 0.40:
			verdict = "needs_research"
		case targetRisk || critic.WithholdMaterialization || len(critic.RiskyClaims) > 0:
			verdict = "accepted_local"
			reason = "local graph fact is useful, but target binding remains unresolved"
		case op.Confidence >= 0.65:
			verdict = "accepted_target"
			reason = "graph operation is structurally strong and not blocked by critic risk targeting"
		default:
			verdict = "accepted_local"
			reason = "keep local substrate while waiting for stronger target binding"
		}
		verdicts = append(verdicts, GraphOpVerdict{
			OpIndex: idx,
			Verdict: verdict,
			Reason:  reason,
		})
	}
	return verdicts
}

// applyGraphOpVerdicts applies critic verdicts to pending graph ops.
func applyGraphOpVerdicts(state *InvestigationState, verdicts []GraphOpVerdict) {
	if state == nil || len(verdicts) == 0 {
		return
	}
	for _, v := range verdicts {
		if v.OpIndex < 0 || v.OpIndex >= len(state.PendingGraphOps) {
			continue
		}
		op := &state.PendingGraphOps[v.OpIndex]
		if op.Status == "landed" || op.Status == "rejected" {
			continue
		}
		switch v.Verdict {
		case "accepted_local":
			op.LocalReady = true
			op.TargetBound = false
			if opCanBeLocallyClosed(*op) {
				op.Status = "verified_local"
			} else {
				op.Status = "accepted_local"
			}
		case "accepted":
			op.LocalReady = true
			op.TargetBound = true
			op.Status = "accepted"
		case "accepted_target":
			op.LocalReady = true
			op.TargetBound = true
			op.Status = "accepted"
		case "needs_target_binding":
			op.LocalReady = true
			op.TargetBound = false
			if opCanBeLocallyClosed(*op) {
				op.Status = "verified_local"
			} else {
				op.Status = "accepted_local"
			}
		case "rejected":
			op.Status = "rejected"
			op.LocalReady = false
			op.TargetBound = false
		case "needs_research":
			op.Status = "proposed"
			op.TargetBound = false
		}
	}
}

// markVerifiedOpsFromPath marks accepted ops as verified when path confirms
// their products exist in the graph.
func markVerifiedOpsFromPath(state *InvestigationState, path PathResult) {
	if state == nil {
		return
	}
	queriedSet := map[string]struct{}{}
	for _, p := range path.QueriedProducts {
		queriedSet[strings.TrimSpace(p)] = struct{}{}
	}
	boundProducts := map[string]struct{}{}
	for _, h := range path.PathHypotheses {
		if h.BindingStatus == "bound" || h.BindingStatus == "partial" {
			boundProducts[strings.TrimSpace(h.ProductRef)] = struct{}{}
		}
	}
	for i := range state.PendingGraphOps {
		op := &state.PendingGraphOps[i]
		if op.Status != "accepted" && op.Status != "accepted_local" {
			continue
		}
		subj := strings.TrimSpace(op.SubjectPath)
		obj := strings.TrimSpace(op.ObjectPath)
		_, subjQueried := queriedSet[subj]
		_, objQueried := queriedSet[obj]
		_, subjBound := boundProducts[subj]
		_, objBound := boundProducts[obj]
		if (subjQueried && subjBound) || (objQueried && objBound) {
			if op.Status == "accepted" || op.TargetBound {
				op.Status = "verified"
				op.TargetBound = true
			} else {
				op.Status = "verified_local"
			}
			op.LocalReady = true
			op.Confidence = math.Min(op.Confidence+0.20, 1.0)
		}
	}
}

func applyCriticToState(state *InvestigationState, critic CriticResult) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "critic")
	state.LatestCritic = critic
	state.OpenQuestions = compactStrings(append(state.OpenQuestions, critic.OpenQuestions...))
	for _, q := range critic.OpenQuestions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		oid := fmt.Sprintf("oq-critic-%d", len(state.StructuredOpenQuestions)+1)
		state.StructuredOpenQuestions = append(state.StructuredOpenQuestions, OpenQuestionEntry{
			ID:     oid,
			Text:   q,
			Source: "critic",
		})
	}
	state.NeedsReplan = false
	state.ReplanReason = ""
	if critic.ForceReplan {
		state.NeedsReplan = true
		state.ReplanReason = "critic_force_replan"
	}
	for _, killID := range critic.KillBranchIDs {
		killID = strings.TrimSpace(killID)
		if killID == "" {
			continue
		}
		if branch := state.Branches[killID]; branch != nil {
			branch.Status = BranchStatusRejected
		}
		if hypothesis := state.Hypotheses[killID]; hypothesis != nil {
			hypothesis.Status = HypothesisRejected
			hypothesis.MaterializationReady = false
		}
	}
	for idx, nb := range critic.NewBranches {
		id := firstNonEmpty(nb.ID, fmt.Sprintf("critic-branch-%d-%d", state.Turn, idx))
		nb.ID = id
		if state.Branches[id] != nil {
			continue
		}
		state.Branches[id] = &BranchRuntimeState{Branch: nb, Status: BranchStatusProposed, History: []BranchHistoryEntry{}}
		state.BranchOrder = append(state.BranchOrder, id)
		state.Hypotheses[id] = &InvestigationHypothesis{
			ID:        "hypothesis:" + id,
			BranchID:  id,
			Statement: firstNonEmpty(nb.Question, state.Plan.Goal),
			Status:    HypothesisProposed,
		}
	}
	if len(critic.ReprioritizeBranchIDs) > 0 {
		state.BranchOrder = reprioritizeBranchOrder(state.BranchOrder, critic.ReprioritizeBranchIDs)
	}

	riskTargets := map[string]struct{}{}
	for _, id := range inferRiskTargetBranches(state, critic) {
		id = strings.TrimSpace(id)
		if id != "" {
			riskTargets[id] = struct{}{}
		}
	}
	hasRisk := len(critic.RiskyClaims) > 0
	unknownRiskTargets := hasRisk && len(riskTargets) == 0

	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		hypothesis := state.Hypotheses[branchID]
		if branch == nil || hypothesis == nil {
			continue
		}
		hasLocalSubstrate := branch.LastConfidence >= 0.70 || branchHasUsablePendingOps(state, branchID)
		if hasLocalSubstrate {
			hypothesis.LocalReady = true
		}
		if hasRisk {
			if _, hit := riskTargets[branchID]; hit {
				hypothesis.TargetBound = false
				hypothesis.MaterializationReady = false
				hypothesis.RefinementCount++
				if !hasLocalSubstrate && branch.Status == BranchStatusSupported {
					branch.Status = BranchStatusBlocked
					hypothesis.Status = HypothesisContradicted
				}
				continue
			}
		}
		if critic.WithholdMaterialization {
			hypothesis.MaterializationReady = false
			if !hypothesis.LocalReady {
				hypothesis.TargetBound = false
			}
			continue
		}
		if unknownRiskTargets {
			hypothesis.MaterializationReady = false
			if !hypothesis.LocalReady {
				hypothesis.TargetBound = false
			}
			continue
		}
		if branch.Status == BranchStatusSupported {
			hypothesis.TargetBound = true
			hypothesis.MaterializationReady = true
		} else {
			hypothesis.TargetBound = false
			hypothesis.MaterializationReady = false
		}
	}
	assessments := critic.BranchAssessments
	if len(assessments) == 0 {
		assessments = synthesizeBranchAssessments(state, critic)
	}
	critic.BranchAssessments = assessments
	state.LatestCritic.BranchAssessments = assessments
	applyBranchAssessmentsToState(state, assessments)
	completeness := critic.CompletenessAssessment
	if strings.TrimSpace(completeness.Status) == "" {
		completeness = synthesizeCompletenessAssessment(state, critic)
	}
	critic.CompletenessAssessment = completeness
	state.LatestCritic.CompletenessAssessment = completeness
	applyCompletenessAssessmentToState(state, completeness)
	critic.GoodEnoughAssessment = normalizeGoodEnoughAssessment(critic.GoodEnoughAssessment)
	if strings.TrimSpace(critic.GoodEnoughAssessment.Status) == "" &&
		strings.TrimSpace(critic.GoodEnoughAssessment.ClosureLevel) == "" &&
		strings.TrimSpace(critic.GoodEnoughAssessment.Reason) == "" &&
		!critic.GoodEnoughAssessment.StopNow &&
		!critic.GoodEnoughAssessment.PersistWorkingPath &&
		!critic.GoodEnoughAssessment.FurtherWorkLowValue &&
		len(critic.GoodEnoughAssessment.OpenNonBlockingGaps) == 0 &&
		len(critic.GoodEnoughAssessment.TargetBranchIDs) == 0 {
		critic.GoodEnoughAssessment = defaultGoodEnoughAssessment()
	}
	state.LatestCritic.GoodEnoughAssessment = critic.GoodEnoughAssessment
	if state.LatestCritic.GoodEnoughAssessment.PersistWorkingPath {
		state.LatestCritic.WithholdPath = false
	}
	if state.LatestCritic.GoodEnoughAssessment.StopNow && state.LatestCritic.GoodEnoughAssessment.FurtherWorkLowValue {
		state.StopReason = firstNonEmpty(state.StopReason, "good_enough_stop")
	}
	applyGapDiagnosisToState(state, "critic", firstString(critic.RiskTargetBranchIDs), critic.GapDiagnosis)
	applyActionRecommendationToState(state, "critic", "", critic.RecommendedAction)
	if !state.NeedsReplan && len(critic.RiskyClaims) > 0 && !hasActionablePursuitBranch(state) && !shouldAttemptPath(state) {
		state.NeedsReplan = true
		state.ReplanReason = "critic_detected_risks"
	}
}

func applyPathToState(state *InvestigationState, path PathResult) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "path")
	state.PathAttempts++
	if len(path.OpenGaps) == 0 &&
		len(path.TraceDigests) == 0 &&
		len(path.MaterializedArtifactIDs) == 0 &&
		strings.TrimSpace(path.AssemblyArtifactID) == "" &&
		(strings.TrimSpace(path.ContinuityStatus) == "" || strings.EqualFold(strings.TrimSpace(path.ContinuityStatus), "unknown")) {
		path.OpenGaps = []string{"Deterministic path tracing did not explain what sits between the current endpoints."}
		path.ContinuityStatus = "broken"
	}
	state.PathResult = path
	if len(path.OpenGaps) > 0 {
		state.RepeatedGapCount++
		state.NeedsReplan = true
		state.ReplanReason = "path_continuity_gap"
	}
	originBranchID := ""
	if len(path.SourceBranchIDs) > 0 {
		originBranchID = strings.TrimSpace(path.SourceBranchIDs[0])
	}
	applyGapDiagnosisToState(state, "path", originBranchID, path.GapDiagnosis)
	applyActionRecommendationToState(state, "path", originBranchID, path.RecommendedAction)
	affected := compactStrings(path.SourceBranchIDs)
	if len(affected) == 0 {
		affected = inferPathAffectedBranches(state, path)
	}
	affectedSet := map[string]struct{}{}
	for _, id := range affected {
		id = strings.TrimSpace(id)
		if id != "" {
			affectedSet[id] = struct{}{}
		}
	}
	continuity := strings.ToLower(strings.TrimSpace(path.ContinuityStatus))
	hasArtifacts := len(path.MaterializedArtifactIDs) > 0 || strings.TrimSpace(path.AssemblyArtifactID) != ""

	for branchID := range affectedSet {
		hypothesis := state.Hypotheses[branchID]
		if hypothesis == nil {
			continue
		}
		if hasArtifacts {
			hypothesis.Status = HypothesisConfirmedMaterializable
			hypothesis.TargetBound = true
			hypothesis.LocalReady = true
		}
		switch continuity {
		case "broken":
			hypothesis.MaterializationReady = false
		case "partial":
			if hypothesis.Status != HypothesisContradicted {
				hypothesis.Status = HypothesisWeaklySupported
			}
			hypothesis.LocalReady = true
		}
	}
}

func applyMaterializeToState(state *InvestigationState, result MaterializeResult) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "materialize")
	state.MaterializeAttempts++
	state.MaterializeResult = result
	if !result.Persisted {
		state.StalledTurns++
		return
	}
	state.StalledTurns = 0

	landed := compactStrings(result.LandedBranchIDs)
	if len(landed) == 0 {
		landed = inferLandedBranches(state, result)
	}
	landedSet := map[string]struct{}{}
	for _, id := range landed {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		landedSet[id] = struct{}{}
		if branch := state.Branches[id]; branch != nil {
			branch.Status = BranchStatusDone
		}
	}

	for _, id := range compactStrings(result.RejectedBranchIDs) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := landedSet[id]; ok {
			continue
		}
		if branch := state.Branches[id]; branch != nil && branch.Status != BranchStatusDone && branch.Status != BranchStatusRejected {
			branch.Status = BranchStatusCandidate
		}
		if hyp := state.Hypotheses[id]; hyp != nil {
			hyp.MaterializationReady = false
			hyp.TargetBound = false
		}
	}
	if len(landedSet) == 0 {
		// Persisted artifacts without confident branch mapping should not force branch completion.
		state.StalledTurns++
	}
}

func (p *Processor) applyReplan(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, req InvestigationRequest, graphCtx map[string]any, state *InvestigationState) {
	if state == nil {
		return
	}
	state.ActionLog = append(state.ActionLog, "replan")
	state.ReplanTriggerCount++
	state.NeedsReplan = false
	questionCountBefore := len(state.OpenQuestions)
	for _, branchID := range state.BranchOrder {
		branch := state.Branches[branchID]
		if branch == nil {
			continue
		}
		if branch.Status == BranchStatusWeak && branch.ConsecutiveGapHits >= max(state.Plan.MaxResearchRounds, 1) {
			branch.Status = BranchStatusCandidate
		}
	}
	questions := collectOpenQuestionTexts(state)
	for idx, q := range questions {
		if idx >= 2 {
			break
		}
		id := fmt.Sprintf("replan-%d-%d", state.Turn, idx+1)
		if _, exists := state.Branches[id]; exists {
			continue
		}
		branch := InvestigationBranch{
			ID:           id,
			Question:     q,
			Role:         strings.TrimSpace(state.Role.PrimaryRole),
			ProductHints: compactStrings(state.LatestCritic.PathProducts),
			GeoHints:     compactStrings(state.LatestCritic.FocusGeos),
			ActorHints:   compactStrings(state.LatestCritic.FocusActors),
			RequiresPath: true,
		}
		state.Branches[id] = &BranchRuntimeState{Branch: branch, Status: BranchStatusProposed, History: []BranchHistoryEntry{}}
		state.BranchOrder = append(state.BranchOrder, id)
		state.Hypotheses[id] = &InvestigationHypothesis{
			ID:        "hypothesis:" + id,
			BranchID:  id,
			Statement: q,
			Status:    HypothesisProposed,
		}
	}
	if len(questions) == questionCountBefore && strings.TrimSpace(state.ReplanReason) != "" {
		state.StalledTurns++
	}
	if state.ReframeCount < 2 && p != nil {
		p.refinePlan(ctx, rt, task, req, graphCtx, state)
	}
}

func collectOpenQuestionTexts(state *InvestigationState) []string {
	if state == nil {
		return nil
	}
	if len(state.StructuredOpenQuestions) > 0 {
		out := make([]string, 0, len(state.StructuredOpenQuestions))
		for _, oq := range state.StructuredOpenQuestions {
			if strings.TrimSpace(oq.Text) != "" {
				out = append(out, strings.TrimSpace(oq.Text))
			}
		}
		return compactStrings(out)
	}
	return compactStrings(state.OpenQuestions)
}

func reprioritizeBranchOrder(order, priority []string) []string {
	if len(priority) == 0 {
		return order
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(order)+len(priority))
	for _, id := range priority {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range order {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (p *Processor) refinePlan(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, req InvestigationRequest, graphCtx map[string]any, state *InvestigationState) {
	if p == nil || state == nil || rt == nil {
		return
	}
	fresh := p.buildPlan(ctx, rt, task, req, graphCtx, state)
	if strings.TrimSpace(fresh.MissionRole.PrimaryRole) == "" {
		fresh.MissionRole = state.Role
	}
	fresh = ensurePlanRoleAndCompleteness(InvestigationRequest{
		Query:               req.Query,
		TargetCanonicalPath: req.TargetCanonicalPath,
		Role:                state.Role,
		Completeness:        state.Completeness,
	}, fresh)
	state.Plan.Goal = firstNonEmpty(fresh.Goal, state.Plan.Goal)
	state.Plan.MissionRole = mergeRoleFrames(state.Plan.MissionRole, fresh.MissionRole)
	state.Plan.FocusProducts = compactStrings(append(state.Plan.FocusProducts, fresh.FocusProducts...))
	state.Plan.FocusGeos = compactStrings(append(state.Plan.FocusGeos, fresh.FocusGeos...))
	state.Plan.FocusActors = compactStrings(append(state.Plan.FocusActors, fresh.FocusActors...))
	state.Plan.PlanningRationale = compactStrings(append(state.Plan.PlanningRationale, fresh.PlanningRationale...))
	state.Plan.ExistingGraphPriorities = compactStrings(append(state.Plan.ExistingGraphPriorities, fresh.ExistingGraphPriorities...))
	state.Plan.WantsPathMaterialize = state.Plan.WantsPathMaterialize || fresh.WantsPathMaterialize
	state.Plan.MaterializationStrategy = firstNonEmpty(fresh.MaterializationStrategy, state.Plan.MaterializationStrategy)
	state.Plan.MaxResearchRounds = max(state.Plan.MaxResearchRounds, fresh.MaxResearchRounds)
	state.Plan.BranchDependencies = append(state.Plan.BranchDependencies, fresh.BranchDependencies...)
	state.Plan.PlanVersion++
	state.Role = mergeRoleFrames(state.Role, fresh.MissionRole)
	for _, dep := range fresh.BranchDependencies {
		if child := state.Branches[dep.ToBranchID]; child != nil && strings.TrimSpace(dep.FromBranchID) != "" {
			child.Branch.DependsOn = compactStrings(append(child.Branch.DependsOn, dep.FromBranchID))
		}
	}
	for idx, b := range fresh.Branches {
		id := firstNonEmpty(b.ID, fmt.Sprintf("frame-%d-%d", state.Turn, idx))
		b.ID = id
		if state.Branches[id] != nil {
			continue
		}
		state.Branches[id] = &BranchRuntimeState{Branch: b, Status: BranchStatusProposed, History: []BranchHistoryEntry{}}
		state.BranchOrder = append(state.BranchOrder, id)
		state.Hypotheses[id] = &InvestigationHypothesis{
			ID:        "hypothesis:" + id,
			BranchID:  id,
			Statement: firstNonEmpty(b.Question, state.Plan.Goal),
			Status:    HypothesisProposed,
		}
	}
	state.Plan.Branches = append(state.Plan.Branches, fresh.Branches...)
	state.PlanVersion = state.Plan.PlanVersion
	state.ReframeCount++
}

func appendReasoningBeat(state *InvestigationState, beat ReasoningBeat) {
	if state == nil {
		return
	}
	beat.BranchIDs = compactStrings(beat.BranchIDs)
	beat.GraphOpRefs = compactStrings(beat.GraphOpRefs)
	beat.WhatWeLookedAt = compactStrings(beat.WhatWeLookedAt)
	beat.WhatWeFound = compactStrings(beat.WhatWeFound)
	beat.WhatWeDidNotFind = compactStrings(beat.WhatWeDidNotFind)
	state.ReasoningTrace = append(state.ReasoningTrace, beat)
	if len(beat.Diagnostics) > 0 {
		state.RuntimeDiagnosis = append(state.RuntimeDiagnosis, beat.Diagnostics...)
	}
	logAppendedReasoningBeat(beat)
}

func summarizeReasoningTrace(beats []ReasoningBeat) string {
	if len(beats) == 0 {
		return ""
	}
	last := beats[len(beats)-1]
	return firstNonEmpty(
		strings.TrimSpace(last.Inference),
		strings.TrimSpace(last.DecisionWhy),
		strings.TrimSpace(last.Title),
	)
}

func explainRequestUnderstanding(req InvestigationRequest) ReasoningBeat {
	question := firstNonEmpty(strings.TrimSpace(req.Goal.PrimaryQuestion), strings.TrimSpace(req.Query), strings.TrimSpace(req.TargetCanonicalPath))
	looked := []string{}
	if strings.TrimSpace(req.Query) != "" {
		looked = append(looked, "user_query")
	}
	if strings.TrimSpace(req.TargetCanonicalPath) != "" {
		looked = append(looked, "target_canonical_path")
	}
	if strings.TrimSpace(req.Goal.Objective) != "" {
		looked = append(looked, "goal_frame")
	}
	if strings.TrimSpace(req.Environment.WorkspaceKind) != "" {
		looked = append(looked, "environment_frame")
	}
	return ReasoningBeat{
		Turn:                   0,
		Phase:                  "understand",
		Title:                  "Interpreted user request",
		QuestionWeAreAnswering: question,
		WhatWeLookedAt:         looked,
		WhatWeFound: []string{
			"workspace:" + firstNonEmpty(req.Environment.WorkspaceKind, "graph_runtime"),
			"write_disposition:" + firstNonEmpty(req.Goal.WriteDisposition, "analyze_without_writes"),
		},
		Inference:      "The runtime translated the request into an explicit investigation goal over graph entities, typed relations, and materialized artifacts.",
		Decision:       "build_plan",
		DecisionWhy:    "Need explicit branch structure before evidence collection.",
		EvidenceStatus: EvidenceInferred,
	}
}

func explainEnvironmentUnderstanding(state *InvestigationState) ReasoningBeat {
	if state == nil {
		return ReasoningBeat{}
	}
	found := []string{
		"workspace:" + firstNonEmpty(state.Environment.WorkspaceKind, "graph_runtime"),
		"unit_of_work:" + firstNonEmpty(state.Environment.UnitOfWork, "entities_relations_artifacts"),
		fmt.Sprintf("observed_relations:%d", len(state.Environment.ObservedRelations)),
	}
	return ReasoningBeat{
		Turn:                   0,
		Phase:                  "understand",
		Title:                  "Bound investigation to graph workspace",
		QuestionWeAreAnswering: firstNonEmpty(state.Goal.PrimaryQuestion, state.Request.Query, state.Request.TargetCanonicalPath),
		WhatWeLookedAt:         []string{"target_entity", "observed_relations", "canonical_control_plane"},
		WhatWeFound:            found,
		Inference:              "This runtime operates over linked entities with metadata-bearing edges and artifacts, not files or freeform documents.",
		Decision:               "reason_over_graph_state",
		DecisionWhy:            "Subsequent agents must propose and validate graph-native operations against canonical paths.",
		EvidenceStatus:         EvidenceObserved,
	}
}

func explainPlanShape(state *InvestigationState) ReasoningBeat {
	if state == nil {
		return ReasoningBeat{}
	}
	ids := compactStrings(append([]string{}, state.BranchOrder...))
	found := []string{
		fmt.Sprintf("plan_version:%d", state.Plan.PlanVersion),
		fmt.Sprintf("branch_count:%d", len(ids)),
	}
	return ReasoningBeat{
		Turn:                   state.Turn,
		Phase:                  "plan",
		Title:                  "Constructed investigation plan",
		QuestionWeAreAnswering: firstNonEmpty(state.Plan.Goal, state.Request.Query, state.Request.TargetCanonicalPath),
		WhatWeLookedAt:         []string{"planning_context", "knowledge_hits", "request"},
		WhatWeFound:            found,
		Inference:              "Plan decomposed the request into branch-level hypotheses.",
		Decision:               "start_loop",
		DecisionWhy:            "Execute branch research and critical checks iteratively.",
		BranchIDs:              ids,
		EvidenceStatus:         EvidenceInferred,
	}
}

func explainDecisionOutcome(state *InvestigationState, decision DecisionResult) ReasoningBeat {
	if state == nil {
		return ReasoningBeat{}
	}
	decision = normalizeDecisionResult(decision)
	whatFound := []string{
		fmt.Sprintf("chosen_action:%s", firstNonEmpty(decision.ChosenAction, "none")),
		fmt.Sprintf("executed_action:%s", firstNonEmpty(decision.ExecutedAction, "none")),
		fmt.Sprintf("branch:%s", firstNonEmpty(decision.ExecutedBranchID, decision.BranchID, "none")),
		fmt.Sprintf("validation:%s", firstNonEmpty(decision.ValidationStatus, "accepted")),
	}
	if strings.TrimSpace(state.CurrentBottleneck) != "" {
		whatFound = append(whatFound, "bottleneck:"+strings.TrimSpace(state.CurrentBottleneck))
	}
	diag := []RuntimeDiagnosis{}
	if strings.TrimSpace(decision.ValidationStatus) == "fallback" {
		diag = append(diag, RuntimeDiagnosis{
			Code:     "decision_fallback",
			Severity: "warning",
			Message:  firstNonEmpty(decision.ValidationReason, "Decision was normalized to a feasible runtime action."),
		})
	}
	return ReasoningBeat{
		Turn:                   state.Turn,
		Phase:                  "decision",
		Title:                  "Selected next action",
		QuestionWeAreAnswering: firstNonEmpty(state.Plan.Goal, state.Request.Query, state.Request.TargetCanonicalPath),
		WhatWeLookedAt:         []string{"investigation_state", "candidate_actions", "stop_criteria", "latest_gap_diagnosis"},
		WhatWeFound:            whatFound,
		Inference:              firstNonEmpty(decision.StateSummary, fmt.Sprintf("Current bottleneck: %s.", firstNonEmpty(state.CurrentBottleneck, "unknown"))),
		Decision:               firstNonEmpty(decision.ExecutedAction, decision.ChosenAction, "stop"),
		DecisionWhy:            firstNonEmpty(decision.WhyChosen, explainDecisionWhy(state, decision.ExecutedAction, decision.ExecutedBranchID)),
		BranchIDs:              compactStrings([]string{firstNonEmpty(decision.ExecutedBranchID, decision.BranchID)}),
		EvidenceStatus:         EvidenceInferred,
		Diagnostics:            diag,
	}
}

func explainDecisionWhy(state *InvestigationState, action, branchID string) string {
	switch action {
	case "research":
		return fmt.Sprintf("Branch %s still has unresolved questions or insufficient grounded confidence.", firstNonEmpty(branchID, "unknown"))
	case "bridge":
		return fmt.Sprintf("Branch %s has locally good substrate but still lacks a clear missing-middle link to the objective.", firstNonEmpty(branchID, "unknown"))
	case "critic":
		return "Need to challenge claims and classify proposed operations before further action."
	case "path":
		return "Need graph/path verification for accepted or path-relevant claims."
	case "materialize":
		return "Path artifacts are available and policy allows landing."
	case "replan":
		return firstNonEmpty("Replan triggered by risk/gap signal.", state.ReplanReason)
	default:
		return "No high-value action found."
	}
}

func describeCandidateReason(state *InvestigationState, action, branchID string) string {
	reason := explainDecisionWhy(state, action, branchID)
	if action == "stop" {
		return "Stop only if the current graph/path result is already useful enough and another turn is unlikely to improve quality materially."
	}
	if diag := normalizeGapDiagnosis(state.LatestGapDiagnosis); diag.Kind != "" && firstNonEmpty(diag.BranchID, branchID) == branchID {
		reason = firstNonEmpty(strings.TrimSpace(diag.Why), reason)
	}
	return reason
}

func explainResearchOutcome(state *InvestigationState, branchID string, result ResearchResult) ReasoningBeat {
	notFound := []string{}
	if len(compactStrings(result.EvidencePoints)) == 0 {
		notFound = append(notFound, "no_grounded_evidence_points")
	}
	if len(compactStrings(result.CanonicalRefs)) == 0 && len(compactStrings(result.ProductRefs)) == 0 {
		notFound = append(notFound, "no_canonical_refs")
	}
	graphRefs := make([]string, 0, len(result.ProposedOps))
	for _, op := range result.ProposedOps {
		graphRefs = append(graphRefs, fmt.Sprintf("%s:%s", strings.TrimSpace(op.Kind), strings.TrimSpace(op.SubjectPath)))
	}
	status := EvidenceObserved
	if len(notFound) > 0 {
		status = EvidenceUnverified
	}
	return ReasoningBeat{
		Turn:                   state.Turn,
		Phase:                  "research",
		Title:                  "Completed research step",
		QuestionWeAreAnswering: firstNonEmpty(result.Summary, lookupBranchQuestion(state, branchID)),
		WhatWeLookedAt:         []string{"knowledge_base", "vertex_search", "web_grounding"},
		WhatWeFound: []string{
			fmt.Sprintf("evidence_points:%d", len(compactStrings(result.EvidencePoints))),
			fmt.Sprintf("canonical_refs:%d", len(compactStrings(append(result.CanonicalRefs, result.ProductRefs...)))),
			fmt.Sprintf("proposed_ops:%d", len(result.ProposedOps)),
		},
		WhatWeDidNotFind: notFound,
		Inference:        firstNonEmpty(strings.TrimSpace(result.Summary), "Research produced incremental substrate."),
		Decision:         "handoff_to_critic",
		DecisionWhy:      "Critic must validate claims and graph operation quality before next step.",
		BranchIDs:        compactStrings([]string{branchID}),
		GraphOpRefs:      graphRefs,
		EvidenceStatus:   status,
		Diagnostics:      diagnoseRuntimeFromResearch(result),
	}
}

func explainBridgeOutcome(state *InvestigationState, branchID string, result BridgeResult) ReasoningBeat {
	notFound := []string{}
	if !result.BridgeExists {
		notFound = append(notFound, "no_plausible_bridge_confirmed")
	}
	if len(compactStrings(result.IntermediateNodes)) == 0 {
		notFound = append(notFound, "no_intermediate_nodes")
	}
	graphRefs := make([]string, 0, len(result.ProposedOps))
	for _, op := range result.ProposedOps {
		graphRefs = append(graphRefs, fmt.Sprintf("%s:%s", strings.TrimSpace(op.Kind), strings.TrimSpace(op.SubjectPath)))
	}
	status := EvidenceObserved
	if len(notFound) > 0 {
		status = EvidenceUnverified
	}
	return ReasoningBeat{
		Turn:                   state.Turn,
		Phase:                  "bridge",
		Title:                  "Attempted missing-middle bridge scan",
		QuestionWeAreAnswering: firstNonEmpty(result.BridgeQuestion, lookupBranchQuestion(state, branchID)),
		WhatWeLookedAt:         []string{"known_endpoints", "path_open_gaps", "candidate_middle_layers"},
		WhatWeFound: []string{
			fmt.Sprintf("bridge_exists:%t", result.BridgeExists),
			fmt.Sprintf("intermediate_nodes:%d", len(compactStrings(result.IntermediateNodes))),
			fmt.Sprintf("proposed_ops:%d", len(result.ProposedOps)),
		},
		WhatWeDidNotFind: notFound,
		Inference:        firstNonEmpty(strings.TrimSpace(result.Summary), "Bridge scan produced a structured route hypothesis for the missing middle."),
		Decision:         ternary(result.BridgeExists, "press_branch", "pivot_or_park"),
		DecisionWhy:      inferBridgeDecisionWhy(result),
		BranchIDs:        compactStrings([]string{branchID}),
		GraphOpRefs:      graphRefs,
		EvidenceStatus:   status,
		Diagnostics:      diagnoseRuntimeFromBridge(result),
	}
}

func explainCriticOutcome(state *InvestigationState, critic CriticResult) ReasoningBeat {
	diag := diagnoseRuntimeFromCritic(state, critic)
	return ReasoningBeat{
		Turn:           state.Turn,
		Phase:          "critic",
		Title:          "Completed critical evaluation",
		WhatWeLookedAt: []string{"research_deltas", "active_frontier", "pending_graph_ops"},
		WhatWeFound: []string{
			fmt.Sprintf("accepted_findings:%d", len(critic.AcceptedFindings)),
			fmt.Sprintf("risky_claims:%d", len(critic.RiskyClaims)),
			fmt.Sprintf("op_verdicts:%d", len(critic.GraphOpVerdicts)),
		},
		WhatWeDidNotFind: critic.OpenQuestions,
		Inference:        inferCriticInference(critic),
		Decision:         inferCriticDecision(critic),
		DecisionWhy:      inferCriticDecisionWhy(state, critic),
		BranchIDs:        compactStrings(append([]string{}, critic.RiskTargetBranchIDs...)),
		EvidenceStatus:   ternaryEvidence(len(critic.RiskyClaims) == 0),
		Diagnostics:      diag,
	}
}

func explainPathOutcome(state *InvestigationState, path PathResult) ReasoningBeat {
	graphRefs := make([]string, 0, len(path.PathHypotheses))
	for _, h := range path.PathHypotheses {
		graphRefs = append(graphRefs, fmt.Sprintf("path:%s:%s", strings.TrimSpace(h.ProductRef), strings.TrimSpace(h.BindingStatus)))
	}
	return ReasoningBeat{
		Turn:             state.Turn,
		Phase:            "path",
		Title:            "Completed path verification",
		WhatWeLookedAt:   []string{"query_engine_trace", "path_hypotheses", "open_gaps"},
		WhatWeFound:      []string{fmt.Sprintf("queried_products:%d", len(path.QueriedProducts)), fmt.Sprintf("trace_digests:%d", len(path.TraceDigests)), "continuity:" + firstNonEmpty(path.ContinuityStatus, "unknown")},
		WhatWeDidNotFind: compactStrings(path.OpenGaps),
		Inference:        inferPathInference(path),
		Decision:         "update_graph_op_verification_state",
		DecisionWhy:      "Path output determines which accepted operations are verified versus still speculative.",
		BranchIDs:        compactStrings(path.SourceBranchIDs),
		GraphOpRefs:      graphRefs,
		EvidenceStatus:   ternaryEvidence(strings.EqualFold(path.ContinuityStatus, "landed") || len(path.TraceDigests) > 0),
		Diagnostics:      diagnoseRuntimeFromPath(path),
	}
}

func explainMaterializeOutcome(state *InvestigationState, result MaterializeResult) ReasoningBeat {
	return ReasoningBeat{
		Turn:             state.Turn,
		Phase:            "materialize",
		Title:            "Completed materialization step",
		WhatWeLookedAt:   []string{"path_artifacts", "critic_policy", "materialize_preview"},
		WhatWeFound:      []string{fmt.Sprintf("persisted:%t", result.Persisted), fmt.Sprintf("artifacts:%d", len(result.ArtifactIDs)), fmt.Sprintf("landed_branches:%d", len(result.LandedBranchIDs))},
		WhatWeDidNotFind: compactStrings(append(result.Skipped, result.Rejected...)),
		Inference:        firstNonEmpty(strings.TrimSpace(result.Summary), "Materialization attempted."),
		Decision:         ternary(result.Persisted, "continue_or_finalize", "continue_research_or_path"),
		DecisionWhy:      ternary(result.Persisted, "At least one artifact/branch was landed.", "No landing happened, so substrate remains unresolved."),
		BranchIDs:        compactStrings(append(result.LandedBranchIDs, result.RejectedBranchIDs...)),
		EvidenceStatus:   ternaryEvidence(result.Persisted),
	}
}

func explainReplanOutcome(state *InvestigationState) ReasoningBeat {
	return ReasoningBeat{
		Turn:           state.Turn,
		Phase:          "replan",
		Title:          "Reframed investigation",
		WhatWeLookedAt: []string{"critic_risks", "open_questions", "path_gaps"},
		WhatWeFound:    []string{fmt.Sprintf("branch_count:%d", len(state.BranchOrder)), fmt.Sprintf("plan_version:%d", state.Plan.PlanVersion)},
		Inference:      "Plan was updated to target unresolved high-value uncertainty.",
		Decision:       "resume_with_new_branching",
		DecisionWhy:    firstNonEmpty(strings.TrimSpace(state.ReplanReason), "Replan triggered by unresolved contradictions or continuity gaps."),
		BranchIDs:      compactStrings(append([]string{}, state.BranchOrder...)),
		EvidenceStatus: EvidenceInferred,
	}
}

func explainStopReason(state *InvestigationState) ReasoningBeat {
	return ReasoningBeat{
		Turn:           state.Turn,
		Phase:          "stop",
		Title:          "Investigation loop stopped",
		WhatWeLookedAt: []string{"stop_reason", "pending_graph_ops", "stalled_turns"},
		WhatWeFound:    []string{"stop_reason:" + firstNonEmpty(state.StopReason, "unknown"), fmt.Sprintf("pending_ops:%d", len(state.PendingGraphOps))},
		Inference:      inferStopInference(state),
		Decision:       "stop",
		DecisionWhy:    firstNonEmpty(state.StopReason, "no_action_available"),
		EvidenceStatus: EvidenceObserved,
		Diagnostics:    diagnoseStop(state),
	}
}

func explainFinalReview(state *InvestigationState, aggregate AggregateResult, review ReviewResult) ReasoningBeat {
	return ReasoningBeat{
		Turn:                   state.Turn,
		Phase:                  "review",
		Title:                  "Produced operator-facing final synthesis",
		QuestionWeAreAnswering: firstNonEmpty(state.Request.Query, state.Request.TargetCanonicalPath, state.Plan.Goal),
		WhatWeLookedAt:         []string{"aggregate", "review", "final_state"},
		WhatWeFound: []string{
			fmt.Sprintf("accepted:%t", review.Accepted),
			fmt.Sprintf("findings:%d", len(aggregate.Findings)),
			fmt.Sprintf("open_questions:%d", len(aggregate.OpenQuestions)),
		},
		Inference:      firstNonEmpty(strings.TrimSpace(aggregate.Summary), strings.TrimSpace(review.Rationale)),
		Decision:       "return_result",
		DecisionWhy:    firstNonEmpty(strings.TrimSpace(review.Rationale), "Review gate completed."),
		EvidenceStatus: ternaryEvidence(review.Accepted),
	}
}

func lookupBranchQuestion(state *InvestigationState, branchID string) string {
	if state == nil {
		return ""
	}
	if b := state.Branches[strings.TrimSpace(branchID)]; b != nil {
		return strings.TrimSpace(b.Branch.Question)
	}
	return ""
}

func inferCriticInference(critic CriticResult) string {
	switch {
	case len(critic.RiskyClaims) > 0:
		return "Critic found risky claims requiring additional evidence or replanning."
	case len(critic.GraphOpVerdicts) > 0:
		return "Critic classified proposed graph operations and narrowed what can be trusted."
	default:
		return "Critic accepted current findings with limited objections."
	}
}

func inferCriticDecision(critic CriticResult) string {
	if critic.GoodEnoughAssessment.StopNow {
		return "good_enough_stop"
	}
	if critic.GoodEnoughAssessment.PersistWorkingPath {
		return "persist_working_path"
	}
	switch {
	case critic.ForceReplan:
		return "replan"
	case critic.WithholdPath:
		return "withhold_path"
	case critic.WithholdMaterialization:
		return "withhold_materialization"
	default:
		return "continue"
	}
}

func inferCriticDecisionWhy(_ *InvestigationState, critic CriticResult) string {
	if critic.GoodEnoughAssessment.StopNow {
		return firstNonEmpty(strings.TrimSpace(critic.GoodEnoughAssessment.Reason), "Current branch/result is already good enough for this runtime tier and further work is low-value.")
	}
	if critic.GoodEnoughAssessment.PersistWorkingPath {
		return firstNonEmpty(strings.TrimSpace(critic.GoodEnoughAssessment.Reason), "Current route is good enough to persist as a working path even if some details remain open.")
	}
	if critic.ForceReplan {
		return "Critic explicitly requested replanning."
	}
	if len(critic.RiskyClaims) > 0 {
		return "Risky claims remain unresolved; system should avoid overconfident landing."
	}
	if critic.WithholdMaterialization {
		return "Materialization withheld until risky uncertainty is reduced."
	}
	return "No blocking objections from critic."
}

func inferPathInference(path PathResult) string {
	switch strings.ToLower(strings.TrimSpace(path.ContinuityStatus)) {
	case "broken":
		return "Path continuity is broken; graph verification failed for key segments."
	case "partial":
		return "Path continuity is partial; some hypotheses verified, some remain gaps."
	case "landed":
		return "Path continuity landed for scoped products."
	default:
		if len(path.TraceDigests) == 0 {
			return "Path step produced no usable traces."
		}
		return "Path verification completed with mixed certainty."
	}
}

func inferBridgeDecisionWhy(result BridgeResult) string {
	if result.BridgeExists {
		return "Bridge scan found a plausible missing middle that can now be tested or materialized as graph-native hypotheses."
	}
	if len(result.SuggestedSubscans) > 0 {
		return "Current route did not bridge cleanly, so runtime should pivot into a more plausible continuation suggested by the bridge scan."
	}
	return "Current route still lacks a viable missing middle and should be preserved only as local substrate until stronger continuation appears."
}

func inferStopInference(state *InvestigationState) string {
	switch firstNonEmpty(state.StopReason, "unknown") {
	case "all_graph_ops_resolved":
		return "All graph operation hypotheses reached resolved status."
	case "bounded_iteration_stop":
		return "Turn budget boundary reached before complete closure."
	case "no_high_value_action", "no_action_available":
		return "No next step could increase expected information value."
	default:
		return "Runtime stopped due to control policy or convergence signal."
	}
}

func ternaryEvidence(ok bool) ReasoningEvidenceStatus {
	if ok {
		return EvidenceObserved
	}
	return EvidenceUnverified
}

func diagnoseRuntimeFromResearch(result ResearchResult) []RuntimeDiagnosis {
	out := []RuntimeDiagnosis{}
	if len(compactStrings(result.EvidencePoints)) == 0 {
		out = append(out, RuntimeDiagnosis{Code: "did_not_find_evidence", Severity: "warn", Message: "Research returned no grounded evidence points."})
	}
	if len(compactStrings(append(result.CanonicalRefs, result.ProductRefs...))) == 0 {
		out = append(out, RuntimeDiagnosis{Code: "search_grounding_weak", Severity: "warn", Message: "No canonical references were grounded in this step."})
	}
	return out
}

func diagnoseRuntimeFromCritic(state *InvestigationState, critic CriticResult) []RuntimeDiagnosis {
	out := []RuntimeDiagnosis{}
	if len(critic.RiskyClaims) > 0 {
		out = append(out, RuntimeDiagnosis{Code: "found_conflicting_evidence", Severity: "warn", Message: "Critic flagged risky or contradictory claims."})
	}
	rejected := 0
	for _, v := range critic.GraphOpVerdicts {
		if strings.EqualFold(v.Verdict, "rejected") {
			rejected++
		}
	}
	if rejected > 0 {
		out = append(out, RuntimeDiagnosis{Code: "critic_rejected_ops", Severity: "warn", Message: fmt.Sprintf("Critic rejected %d proposed graph operations.", rejected)})
	}
	if state != nil && detectCriticLoop(state) {
		out = append(out, RuntimeDiagnosis{Code: "critic_loop_detected", Severity: "error", Message: "Runtime entered a critic-heavy loop without progress."})
	}
	return out
}

func diagnoseRuntimeFromPath(path PathResult) []RuntimeDiagnosis {
	out := []RuntimeDiagnosis{}
	if len(path.OpenGaps) > 0 {
		out = append(out, RuntimeDiagnosis{Code: "path_could_not_verify_ops", Severity: "warn", Message: "Path verification left unresolved gaps."})
	}
	return out
}

func diagnoseRuntimeFromBridge(result BridgeResult) []RuntimeDiagnosis {
	out := []RuntimeDiagnosis{}
	if !result.BridgeExists {
		out = append(out, RuntimeDiagnosis{Code: "bridge_not_confirmed", Severity: "warn", Message: "Bridge scan could not confirm a viable missing middle between the current endpoints."})
	}
	if len(compactStrings(result.IntermediateNodes)) == 0 {
		out = append(out, RuntimeDiagnosis{Code: "bridge_missing_intermediates", Severity: "warn", Message: "Bridge scan did not return intermediate nodes to test or land."})
	}
	return out
}

func diagnoseStop(state *InvestigationState) []RuntimeDiagnosis {
	out := []RuntimeDiagnosis{}
	stop := firstNonEmpty(state.StopReason, "")
	switch stop {
	case "bounded_iteration_stop":
		out = append(out, RuntimeDiagnosis{Code: "stopped_because_bounded_iteration", Severity: "warn", Message: "Turn budget was exhausted."})
	case "no_action_available", "no_high_value_action":
		if hasPendingUnresolvedOps(state) && !goodEnoughAllowsStop(state) {
			out = append(out, RuntimeDiagnosis{Code: "stopped_because_no_consumer_for_pending_ops", Severity: "error", Message: "Runtime stopped while unresolved graph ops still existed."})
		}
	case "good_enough_stop":
		out = append(out, RuntimeDiagnosis{Code: "stopped_because_good_enough", Severity: "warn", Message: "Runtime stopped after critic judged further work low-value for this graph-filling tier."})
	}
	if detectCriticLoop(state) {
		out = append(out, RuntimeDiagnosis{Code: "critic_loop_detected", Severity: "error", Message: "Stop followed repeated critic-heavy loop behavior."})
	}
	return out
}

func detectCriticLoop(state *InvestigationState) bool {
	if state == nil || len(state.ActionLog) < 4 {
		return false
	}
	last := state.ActionLog[max(0, len(state.ActionLog)-6):]
	critic := 0
	nonCritic := 0
	for _, action := range last {
		if action == "critic" {
			critic++
		} else {
			nonCritic++
		}
	}
	return critic >= 4 && nonCritic <= 1
}

func (p *Processor) ensureInvestigationSession(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string) {
	if rt == nil || rt.Vertex() == nil || task == nil {
		return
	}
	sessionRef, err := rt.Vertex().EnsureSession(ctx, task)
	if err != nil {
		if p != nil && p.logger != nil {
			p.logger.Printf("[agentmesh.scout] ensure session failed task=%s err=%v", task.ID, err)
		}
		return
	}
	task.SessionRef = sessionRef
	memoryRef, err := rt.Vertex().EnsureMemoryBank(ctx, task)
	if err == nil && strings.TrimSpace(memoryRef) != "" {
		task.MemoryBankRef = memoryRef
	}
	if rt.Store() != nil && strings.TrimSpace(leaseOwner) != "" {
		_ = rt.Store().UpdateTaskRuntimeState(ctx, task.ID, leaseOwner, task.SessionRef, task.MemoryBankRef)
	}
}

func (p *Processor) appendInvestigationEvent(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, phase string, payload map[string]any) {
	if rt == nil || rt.Vertex() == nil || task == nil || strings.TrimSpace(task.SessionRef) == "" {
		return
	}
	body := map[string]any{"phase": phase, "payload": payload}
	raw, _ := json.Marshal(body)
	_ = rt.Vertex().AppendSessionEvent(ctx, task.SessionRef, "scout-orchestrator", "model", string(raw), map[string]any{
		"taskId": task.ID, "capability": task.Capability, "phase": phase,
	})
}

func (p *Processor) persistCheckpoint(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, leaseOwner string, req InvestigationRequest, state *InvestigationState, taskRefs map[string]any, action, branchID, childTaskID string, childOutput map[string]any) {
	if rt == nil || rt.Store() == nil || task == nil || state == nil || strings.TrimSpace(leaseOwner) == "" {
		return
	}
	checkpoint := buildCheckpointOutput(req, state, taskRefs, action, branchID, childTaskID, childOutput)
	if err := rt.Store().UpdateTaskCheckpoint(ctx, task.ID, leaseOwner, checkpoint); err != nil && p != nil && p.logger != nil {
		p.logger.Printf("[agentmesh.scout] checkpoint persist failed task=%s turn=%d err=%v", task.ID, state.Turn, err)
	}
}

func compactInvestigationSnapshot(state *InvestigationState) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	active := buildActiveFrontierSnapshot(state)
	return map[string]any{
		"turn":               state.Turn,
		"needsReplan":        state.NeedsReplan,
		"replanReason":       state.ReplanReason,
		"goal":               compactGoalFrame(state.Goal),
		"environment":        compactEnvironmentFrame(state.Environment),
		"role":               compactRoleFrame(state.Role),
		"completeness":       compactCompletenessFrame(state.Completeness),
		"activeFrontier":     active,
		"latestBridge":       compactBridgeResult(state.LatestBridge),
		"latestGapDiagnosis": compactGapDiagnosis(state.LatestGapDiagnosis),
		"latestDecision":     compactDecisionResult(state.LatestDecision),
		"latestOutcome":      compactActionOutcome(state.LatestOutcome),
		"recentOutcomes":     compactActionOutcomes(state.Outcomes),
		"runtimeDiagnosis":   compactRuntimeDiagnoses(state.RuntimeDiagnosis),
		"recommendedAction":  compactActionRecommendation(state.LatestRecommendedAction),
		"pathContinuity":     state.PathResult.ContinuityStatus,
	}
}

func buildCheckpointOutput(req InvestigationRequest, state *InvestigationState, taskRefs map[string]any, action, branchID, childTaskID string, childOutput map[string]any) map[string]any {
	out := map[string]any{
		"request":          req,
		"inProgress":       true,
		"state":            compactCheckpointState(state),
		"activeFrontier":   buildActiveFrontierSnapshot(state),
		"decision":         compactDecisionResult(state.LatestDecision),
		"reasoningTrace":   limitReasoningTrace(state.ReasoningTrace, 20),
		"reasoningSummary": summarizeReasoningTrace(state.ReasoningTrace),
		"runtimeDiagnosis": state.RuntimeDiagnosis,
		"taskRefs":         cloneTaskRefs(taskRefs),
		"sessionRef":       strings.TrimSpace(state.SessionRef),
		"checkpoint": map[string]any{
			"turn":         state.Turn,
			"action":       strings.TrimSpace(action),
			"branchId":     strings.TrimSpace(branchID),
			"childTaskId":  strings.TrimSpace(childTaskID),
			"stopReason":   strings.TrimSpace(state.StopReason),
			"replanReason": strings.TrimSpace(state.ReplanReason),
			"bottleneck":   strings.TrimSpace(state.CurrentBottleneck),
			"updatedAt":    nowRFC3339(),
		},
		"executionJournal": buildExecutionJournal(state),
	}
	if latest := compactLatestStepOutput(action, childOutput); len(latest) > 0 {
		out["latestStep"] = latest
	}
	return out
}

func compactCheckpointState(state *InvestigationState) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	return map[string]any{
		"turn":                    state.Turn,
		"maxTurns":                state.MaxTurns,
		"stopReason":              strings.TrimSpace(state.StopReason),
		"currentBottleneck":       strings.TrimSpace(state.CurrentBottleneck),
		"needsReplan":             state.NeedsReplan,
		"replanReason":            strings.TrimSpace(state.ReplanReason),
		"bridgeAttempts":          state.BridgeAttempts,
		"pathAttempts":            state.PathAttempts,
		"materializeAttempts":     state.MaterializeAttempts,
		"repeatedGapCount":        state.RepeatedGapCount,
		"stalledTurns":            state.StalledTurns,
		"actionLog":               append([]string{}, state.ActionLog...),
		"openQuestions":           limitStrings(state.OpenQuestions, 8),
		"structuredQuestions":     state.StructuredOpenQuestions,
		"goal":                    compactGoalFrame(state.Goal),
		"environment":             compactEnvironmentFrame(state.Environment),
		"role":                    compactRoleFrame(state.Role),
		"completeness":            compactCompletenessFrame(state.Completeness),
		"latestBridge":            compactBridgeResult(state.LatestBridge),
		"latestGapDiagnosis":      compactGapDiagnosis(state.LatestGapDiagnosis),
		"goodEnoughAssessment":    compactGoodEnoughAssessment(state.LatestCritic.GoodEnoughAssessment),
		"latestCritic":            compactLatestCritic(state.LatestCritic),
		"latestDecision":          compactDecisionResult(state.LatestDecision),
		"latestOutcome":           compactActionOutcome(state.LatestOutcome),
		"latestRecommendedAction": compactActionRecommendation(state.LatestRecommendedAction),
		"path":                    compactPathResult(state.PathResult),
		"materialize":             compactMaterializeResult(state.MaterializeResult),
		"pendingGraphOps":         compactPendingGraphOpsForCheckpoint(state.PendingGraphOps),
		"branchAssessments":       compactBranchAssessments(state),
		"gapDiagnoses":            compactGapDiagnoses(state.GapDiagnoses),
		"bridgeSummaries":         compactBridgeSummaries(state.BridgeResults),
		"researchSummaries":       compactResearchSummaries(state.ResearchResults),
		"recommendedActions":      compactActionRecommendations(state.RecommendedActions),
		"decisions":               compactDecisionResults(state.Decisions),
		"outcomes":                compactActionOutcomes(state.Outcomes),
		"runtimeDiagnosis":        compactRuntimeDiagnoses(state.RuntimeDiagnosis),
		"activeFrontier":          buildActiveFrontierSnapshot(state),
	}
}

func cloneTaskRefs(taskRefs map[string]any) map[string]any {
	if len(taskRefs) == 0 {
		return map[string]any{}
	}
	raw, _ := json.Marshal(taskRefs)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func compactLatestStepOutput(action string, output map[string]any) map[string]any {
	if len(output) == 0 {
		return nil
	}
	switch strings.TrimSpace(action) {
	case "research":
		result := decodeResearchResult(output)
		return map[string]any{
			"type":              "research",
			"summary":           strings.TrimSpace(result.Summary),
			"confidence":        result.Confidence,
			"productRefs":       limitStrings(result.ProductRefs, 6),
			"canonicalRefs":     limitStrings(result.CanonicalRefs, 8),
			"openQuestions":     limitStrings(result.OpenQuestions, 6),
			"gapDiagnosis":      compactGapDiagnosis(result.GapDiagnosis),
			"recommendedAction": compactActionRecommendation(result.RecommendedAction),
		}
	case "critic":
		result := decodeCriticResult(output)
		return map[string]any{
			"type":                    "critic",
			"acceptedFindings":        limitStrings(result.AcceptedFindings, 6),
			"riskyClaims":             limitStrings(result.RiskyClaims, 6),
			"withholdPath":            result.WithholdPath,
			"withholdMaterialization": result.WithholdMaterialization,
			"branchAssessments":       limitBranchAssessments(result.BranchAssessments, 6),
			"pathProducts":            limitStrings(result.PathProducts, 6),
			"gapDiagnosis":            compactGapDiagnosis(result.GapDiagnosis),
			"goodEnoughAssessment":    compactGoodEnoughAssessment(result.GoodEnoughAssessment),
			"recommendedAction":       compactActionRecommendation(result.RecommendedAction),
			"completenessAssessment": map[string]any{
				"status":                 strings.TrimSpace(result.CompletenessAssessment.Status),
				"reason":                 strings.TrimSpace(result.CompletenessAssessment.Reason),
				"alternativeRoutes":      limitStrings(result.CompletenessAssessment.AlternativeRoutes, 8),
				"missingAngles":          limitStrings(result.CompletenessAssessment.MissingAngles, 8),
				"broadScanCompleted":     result.CompletenessAssessment.BroadScanCompleted,
				"noMaterialAlternatives": result.CompletenessAssessment.NoMaterialAlternatives,
			},
		}
	case "path":
		result := decodePathResult(output)
		return compactPathResult(result)
	case "decide":
		result := decodeDecisionResult(output)
		return compactDecisionResult(result)
	case "bridge":
		result := decodeBridgeResult(output)
		return compactBridgeResult(result)
	case "materialize":
		result := decodeMaterializeResult(output)
		return compactMaterializeResult(result)
	default:
		raw, _ := json.Marshal(output)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
}

func compactActionRecommendation(item ActionRecommendation) map[string]any {
	item = normalizeActionRecommendation(item)
	if item.Action == "" {
		return nil
	}
	return map[string]any{
		"action":       item.Action,
		"branchId":     strings.TrimSpace(item.BranchID),
		"reason":       strings.TrimSpace(item.Reason),
		"failureMode":  strings.TrimSpace(item.FailureMode),
		"question":     strings.TrimSpace(item.Question),
		"endpointA":    strings.TrimSpace(item.EndpointA),
		"endpointB":    strings.TrimSpace(item.EndpointB),
		"productHints": limitStrings(item.ProductHints, 6),
		"geoHints":     limitStrings(item.GeoHints, 6),
		"actorHints":   limitStrings(item.ActorHints, 6),
		"requiresPath": item.RequiresPath,
	}
}

func compactActionOutcome(item ActionOutcome) map[string]any {
	item = normalizeActionOutcome(item)
	if item.Action == "" && item.Summary == "" && item.OutcomeClass == "" {
		return nil
	}
	return map[string]any{
		"turn":                 item.Turn,
		"action":               item.Action,
		"branchId":             item.BranchID,
		"outcomeClass":         item.OutcomeClass,
		"qualityDelta":         item.QualityDelta,
		"summary":              item.Summary,
		"graphUseful":          item.GraphUseful,
		"systemError":          item.SystemError,
		"retryable":            item.Retryable,
		"retryAttempt":         item.RetryAttempt,
		"errorMessage":         item.ErrorMessage,
		"pathContinuity":       item.PathContinuity,
		"bridgeExists":         item.BridgeExists,
		"producedGraphOps":     item.ProducedGraphOps,
		"producedArtifacts":    item.ProducedArtifacts,
		"openQuestions":        limitStrings(item.OpenQuestions, 6),
		"evidenceRefs":         limitStrings(item.EvidenceRefs, 8),
		"gapDiagnosis":         compactGapDiagnosis(item.GapDiagnosis),
		"recommendedAction":    compactActionRecommendation(item.RecommendedAction),
		"goodEnoughAssessment": compactGoodEnoughAssessment(item.GoodEnoughAssessment),
		"dataSourceSignal":     item.DataSourceSignal,
		"retrievalHitCount":    item.RetrievalHitCount,
		"toolCallCount":        item.ToolCallCount,
	}
}

func compactActionOutcomes(items []ActionOutcome) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	start := max(0, len(items)-8)
	for _, item := range items[start:] {
		if compact := compactActionOutcome(item); len(compact) > 0 {
			out = append(out, compact)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactDecisionCandidates(items []DecisionCandidate) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		item = normalizeDecisionCandidate(item)
		if item.Action == "" {
			continue
		}
		out = append(out, map[string]any{
			"action":         item.Action,
			"branchId":       item.BranchID,
			"available":      item.Available,
			"reason":         item.Reason,
			"question":       item.Question,
			"endpointA":      item.EndpointA,
			"endpointB":      item.EndpointB,
			"productHints":   limitStrings(item.ProductHints, 6),
			"geoHints":       limitStrings(item.GeoHints, 6),
			"actorHints":     limitStrings(item.ActorHints, 6),
			"dataSourceHint": item.DataSourceHint,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactStopCriteriaEvaluations(items []StopCriterionEvaluation) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Criterion) == "" {
			continue
		}
		out = append(out, map[string]any{
			"criterion": strings.TrimSpace(item.Criterion),
			"satisfied": item.Satisfied,
			"why":       strings.TrimSpace(item.Why),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactDecisionResult(item DecisionResult) map[string]any {
	item = normalizeDecisionResult(item)
	if item.ChosenAction == "" && item.ExecutedAction == "" && item.StateSummary == "" {
		return nil
	}
	return map[string]any{
		"stateSummary":            item.StateSummary,
		"chosenAction":            item.ChosenAction,
		"branchId":                item.BranchID,
		"whyChosen":               item.WhyChosen,
		"whyNotStop":              item.WhyNotStop,
		"whyNotPath":              item.WhyNotPath,
		"whyNotBridge":            item.WhyNotBridge,
		"whyNotResearch":          item.WhyNotResearch,
		"whyNotCritic":            item.WhyNotCritic,
		"whyNotMaterialize":       item.WhyNotMaterialize,
		"whyNotReplan":            item.WhyNotReplan,
		"evidenceUsed":            limitStrings(item.EvidenceUsed, 8),
		"stopCriteriaEvaluation":  compactStopCriteriaEvaluations(item.StopCriteria),
		"expectedInformationGain": item.ExpectedInformationGain,
		"confidence":              item.Confidence,
		"candidates":              compactDecisionCandidates(item.Candidates),
		"executedAction":          item.ExecutedAction,
		"executedBranchId":        item.ExecutedBranchID,
		"validationStatus":        item.ValidationStatus,
		"validationReason":        item.ValidationReason,
	}
}

func compactDecisionResults(items []DecisionResult) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	start := max(0, len(items)-8)
	for _, item := range items[start:] {
		if compact := compactDecisionResult(item); len(compact) > 0 {
			out = append(out, compact)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactRuntimeDiagnoses(items []RuntimeDiagnosis) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	start := max(0, len(items)-8)
	for _, item := range items[start:] {
		if strings.TrimSpace(item.Code) == "" && strings.TrimSpace(item.Message) == "" {
			continue
		}
		out = append(out, map[string]any{
			"code":     strings.TrimSpace(item.Code),
			"severity": strings.TrimSpace(item.Severity),
			"message":  strings.TrimSpace(item.Message),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactGapDiagnosis(item GapDiagnosis) map[string]any {
	item = normalizeGapDiagnosis(item)
	if item.Kind == "" && item.NextAction == "" && item.Summary == "" {
		return nil
	}
	return map[string]any{
		"kind":           item.Kind,
		"summary":        item.Summary,
		"why":            item.Why,
		"questionClass":  item.QuestionClass,
		"nextAction":     item.NextAction,
		"branchId":       item.BranchID,
		"endpointA":      item.EndpointA,
		"endpointB":      item.EndpointB,
		"endpointsKnown": item.EndpointsKnown,
	}
}

func compactGapDiagnoses(items []GapDiagnosis) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	start := max(0, len(items)-8)
	for _, item := range items[start:] {
		if compact := compactGapDiagnosis(item); len(compact) > 0 {
			out = append(out, compact)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactGoodEnoughAssessment(item GoodEnoughAssessment) map[string]any {
	item = normalizeGoodEnoughAssessment(item)
	if item.Status == "" && item.ClosureLevel == "" && item.Reason == "" && !item.StopNow && !item.PersistWorkingPath && !item.FurtherWorkLowValue {
		return nil
	}
	return map[string]any{
		"status":              item.Status,
		"closureLevel":        item.ClosureLevel,
		"stopNow":             item.StopNow,
		"persistWorkingPath":  item.PersistWorkingPath,
		"furtherWorkLowValue": item.FurtherWorkLowValue,
		"reason":              item.Reason,
		"openNonBlockingGaps": limitStrings(item.OpenNonBlockingGaps, 8),
		"targetBranchIds":     limitStrings(item.TargetBranchIDs, 8),
	}
}

func compactActionRecommendations(items []ActionRecommendation) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	start := max(0, len(items)-8)
	for _, item := range items[start:] {
		if compact := compactActionRecommendation(item); len(compact) > 0 {
			out = append(out, compact)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactLatestCritic(result CriticResult) map[string]any {
	return map[string]any{
		"acceptedFindings":        limitStrings(result.AcceptedFindings, 6),
		"riskyClaims":             limitStrings(result.RiskyClaims, 6),
		"openQuestions":           limitStrings(result.OpenQuestions, 6),
		"withholdPath":            result.WithholdPath,
		"withholdMaterialization": result.WithholdMaterialization,
		"pathProducts":            limitStrings(result.PathProducts, 6),
		"focusActors":             limitStrings(result.FocusActors, 6),
		"focusGeos":               limitStrings(result.FocusGeos, 6),
		"gapDiagnosis":            compactGapDiagnosis(result.GapDiagnosis),
		"goodEnoughAssessment":    compactGoodEnoughAssessment(result.GoodEnoughAssessment),
		"recommendedAction":       compactActionRecommendation(result.RecommendedAction),
		"branchAssessments":       limitBranchAssessments(result.BranchAssessments, 8),
		"completenessAssessment": map[string]any{
			"status":                 strings.TrimSpace(result.CompletenessAssessment.Status),
			"reason":                 strings.TrimSpace(result.CompletenessAssessment.Reason),
			"alternativeRoutes":      limitStrings(result.CompletenessAssessment.AlternativeRoutes, 8),
			"missingAngles":          limitStrings(result.CompletenessAssessment.MissingAngles, 8),
			"broadScanCompleted":     result.CompletenessAssessment.BroadScanCompleted,
			"noMaterialAlternatives": result.CompletenessAssessment.NoMaterialAlternatives,
		},
	}
}

func compactPathResult(result PathResult) map[string]any {
	return map[string]any{
		"queriedProducts":         limitStrings(result.QueriedProducts, 8),
		"continuityStatus":        strings.TrimSpace(result.ContinuityStatus),
		"openGaps":                limitStrings(result.OpenGaps, 8),
		"sourceBranchIds":         limitStrings(result.SourceBranchIDs, 8),
		"materializedArtifactIds": limitStrings(result.MaterializedArtifactIDs, 8),
		"assemblyArtifactId":      strings.TrimSpace(result.AssemblyArtifactID),
		"pathHypotheses":          compactPathHypotheses(result.PathHypotheses),
		"traceDigests":            limitTraceDigests(result.TraceDigests, 6),
		"gapDiagnosis":            compactGapDiagnosis(result.GapDiagnosis),
		"recommendedAction":       compactActionRecommendation(result.RecommendedAction),
	}
}

func compactBridgeResult(result BridgeResult) map[string]any {
	return map[string]any{
		"branchId":                  strings.TrimSpace(result.BranchID),
		"endpointA":                 strings.TrimSpace(result.EndpointA),
		"endpointB":                 strings.TrimSpace(result.EndpointB),
		"bridgeQuestion":            strings.TrimSpace(result.BridgeQuestion),
		"summary":                   strings.TrimSpace(result.Summary),
		"bridgeExists":              result.BridgeExists,
		"bridgeKind":                strings.TrimSpace(result.BridgeKind),
		"missingMiddleTypes":        limitStrings(result.MissingMiddleTypes, 8),
		"intermediateNodes":         limitStrings(result.IntermediateNodes, 8),
		"intermediateRelationTypes": limitStrings(result.IntermediateRelationTypes, 8),
		"bridgeSteps":               compactBridgeSteps(result.BridgeSteps),
		"routeSketches":             limitStrings(result.RouteSketches, 6),
		"openQuestions":             limitStrings(result.OpenQuestions, 6),
		"confidence":                result.Confidence,
		"suggestedSubscans":         compactBridgeSubscans(result.SuggestedSubscans),
		"gapDiagnosis":              compactGapDiagnosis(result.GapDiagnosis),
		"recommendedAction":         compactActionRecommendation(result.RecommendedAction),
	}
}

func compactMaterializeResult(result MaterializeResult) map[string]any {
	return map[string]any{
		"persisted":          result.Persisted,
		"summary":            strings.TrimSpace(result.Summary),
		"artifactIds":        limitStrings(result.ArtifactIDs, 8),
		"created":            limitStrings(result.Created, 8),
		"updated":            limitStrings(result.Updated, 8),
		"skipped":            limitStrings(result.Skipped, 8),
		"downgraded":         limitStrings(result.Downgraded, 8),
		"rejected":           limitStrings(result.Rejected, 8),
		"landedBranchIds":    limitStrings(result.LandedBranchIDs, 8),
		"rejectedBranchIds":  limitStrings(result.RejectedBranchIDs, 8),
		"correctionsApplied": result.CorrectionsApplied,
	}
}

func compactPendingGraphOpsForCheckpoint(ops []ProposedGraphOp) []map[string]any {
	if len(ops) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(ops), 16))
	for _, op := range ops {
		out = append(out, map[string]any{
			"kind":        op.Kind,
			"subjectPath": op.SubjectPath,
			"objectPath":  op.ObjectPath,
			"edgeType":    op.EdgeType,
			"status":      op.Status,
			"confidence":  op.Confidence,
			"branchId":    op.BranchID,
			"localReady":  op.LocalReady,
			"targetBound": op.TargetBound,
		})
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func compactBridgeSummaries(items []BridgeResult) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 6))
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, compactBridgeResult(items[i]))
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func compactBridgeSubscans(items []InvestigationBranch) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 6))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":           strings.TrimSpace(item.ID),
			"question":     strings.TrimSpace(item.Question),
			"role":         strings.TrimSpace(item.Role),
			"requiresPath": item.RequiresPath,
			"productHints": limitStrings(item.ProductHints, 4),
			"geoHints":     limitStrings(item.GeoHints, 4),
			"actorHints":   limitStrings(item.ActorHints, 4),
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func compactBridgeSteps(items []BridgeStep) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 10))
	for _, item := range items {
		out = append(out, map[string]any{
			"index":             item.Index,
			"stepClass":         strings.TrimSpace(item.StepClass),
			"nodeRef":           strings.TrimSpace(item.NodeRef),
			"nodeLabel":         strings.TrimSpace(item.NodeLabel),
			"nodeKind":          strings.TrimSpace(item.NodeKind),
			"fromRef":           strings.TrimSpace(item.FromRef),
			"toRef":             strings.TrimSpace(item.ToRef),
			"relationType":      strings.TrimSpace(item.RelationType),
			"productRef":        strings.TrimSpace(item.ProductRef),
			"actorRef":          strings.TrimSpace(item.ActorRef),
			"geoRef":            strings.TrimSpace(item.GeoRef),
			"whyNeeded":         strings.TrimSpace(item.WhyNeeded),
			"evidenceHints":     limitStrings(item.EvidenceHints, 4),
			"operationalStatus": strings.TrimSpace(item.OperationalStatus),
		})
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func compactBranchAssessments(state *InvestigationState) []map[string]any {
	if state == nil || len(state.BranchOrder) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(state.BranchOrder), 12))
	for _, branchID := range state.BranchOrder {
		h := state.Hypotheses[branchID]
		b := state.Branches[branchID]
		if h == nil || b == nil {
			continue
		}
		out = append(out, map[string]any{
			"branchId":           branchID,
			"status":             b.Status,
			"question":           strings.TrimSpace(b.Branch.Question),
			"localReady":         h.LocalReady,
			"targetBound":        h.TargetBound,
			"targetRelevance":    h.TargetRelevance,
			"pursuitDecision":    string(h.PursuitDecision),
			"pursuitReason":      strings.TrimSpace(h.PursuitReason),
			"routeHypothesis":    strings.TrimSpace(h.RouteHypothesis),
			"alternativeRoute":   strings.TrimSpace(h.AlternativeRoute),
			"suggestedBranchIds": compactStrings(append([]string{}, h.SuggestedBranchIDs...)),
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func compactResearchSummaries(items []ResearchResult) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		out = append(out, map[string]any{
			"branchId":                strings.TrimSpace(item.BranchID),
			"summary":                 strings.TrimSpace(item.Summary),
			"confidence":              item.Confidence,
			"productRefs":             limitStrings(item.ProductRefs, 6),
			"canonicalRefs":           limitStrings(item.CanonicalRefs, 6),
			"openQuestions":           limitStrings(item.OpenQuestions, 4),
			"recommendedPathProducts": limitStrings(item.RecommendedPathProducts, 6),
			"gapDiagnosis":            compactGapDiagnosis(item.GapDiagnosis),
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func buildExecutionJournal(state *InvestigationState) []map[string]any {
	if state == nil || len(state.ActionLog) == 0 {
		return nil
	}
	beats := limitReasoningTrace(state.ReasoningTrace, 24)
	out := make([]map[string]any, 0, len(beats))
	for _, beat := range beats {
		entry := map[string]any{
			"turn":           beat.Turn,
			"phase":          strings.TrimSpace(beat.Phase),
			"title":          strings.TrimSpace(beat.Title),
			"decision":       strings.TrimSpace(beat.Decision),
			"decisionWhy":    strings.TrimSpace(beat.DecisionWhy),
			"inference":      strings.TrimSpace(beat.Inference),
			"lookedAt":       limitStrings(beat.WhatWeLookedAt, 8),
			"found":          limitStrings(beat.WhatWeFound, 8),
			"missing":        limitStrings(beat.WhatWeDidNotFind, 6),
			"branchIds":      limitStrings(beat.BranchIDs, 6),
			"graphOpRefs":    limitStrings(beat.GraphOpRefs, 8),
			"evidenceStatus": string(beat.EvidenceStatus),
		}
		out = append(out, entry)
	}
	return out
}

func limitReasoningTrace(beats []ReasoningBeat, n int) []ReasoningBeat {
	if n <= 0 || len(beats) <= n {
		return append([]ReasoningBeat{}, beats...)
	}
	return append([]ReasoningBeat{}, beats[len(beats)-n:]...)
}

func compactPathHypotheses(items []PathHypothesis) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), 8))
	for _, item := range items {
		out = append(out, map[string]any{
			"productRef":    strings.TrimSpace(item.ProductRef),
			"bindingStatus": strings.TrimSpace(item.BindingStatus),
			"gapSummary":    strings.TrimSpace(item.GapSummary),
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func limitTraceDigests(items []map[string]any, n int) []map[string]any {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func limitBranchAssessments(items []BranchAssessment, n int) []map[string]any {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, min(len(items), n))
	for _, item := range items {
		out = append(out, map[string]any{
			"branchId":           strings.TrimSpace(item.BranchID),
			"targetRelevance":    item.TargetRelevance,
			"pursuitDecision":    string(item.PursuitDecision),
			"pursuitReason":      strings.TrimSpace(item.PursuitReason),
			"routeHypothesis":    strings.TrimSpace(item.RouteHypothesis),
			"alternativeRoute":   strings.TrimSpace(item.AlternativeRoute),
			"suggestedBranchIds": limitStrings(item.SuggestedBranchIDs, 4),
		})
		if len(out) >= n {
			break
		}
	}
	return out
}

func compactGoalFrame(goal GoalFrame) map[string]any {
	return map[string]any{
		"objective":           strings.TrimSpace(goal.Objective),
		"primaryQuestion":     strings.TrimSpace(goal.PrimaryQuestion),
		"userIntent":          strings.TrimSpace(goal.UserIntent),
		"targetCanonicalPath": strings.TrimSpace(goal.TargetCanonicalPath),
		"writeDisposition":    strings.TrimSpace(goal.WriteDisposition),
		"outputContract":      compactStrings(goal.OutputContract),
		"successCriteria":     compactStrings(goal.SuccessCriteria),
		"stopConditions":      compactStrings(goal.StopConditions),
	}
}

func compactRoleFrame(role RoleFrame) map[string]any {
	return map[string]any{
		"domain":                 strings.TrimSpace(role.Domain),
		"primaryRole":            strings.TrimSpace(role.PrimaryRole),
		"supportingRoles":        compactStrings(role.SupportingRoles),
		"professionalQualityBar": strings.TrimSpace(role.ProfessionalQualityBar),
		"investigationPosture":   strings.TrimSpace(role.InvestigationPosture),
		"roleRationale":          strings.TrimSpace(role.RoleRationale),
	}
}

func compactCompletenessFrame(frame CompletenessFrame) map[string]any {
	return map[string]any{
		"broadScanRequired":      frame.BroadScanRequired,
		"broadScanQuestion":      strings.TrimSpace(frame.BroadScanQuestion),
		"broadScanCompleted":     frame.BroadScanCompleted,
		"explorationStatus":      strings.TrimSpace(frame.ExplorationStatus),
		"knownAlternativeRoutes": compactStrings(frame.KnownAlternativeRoutes),
		"missingAngles":          compactStrings(frame.MissingAngles),
		"noMaterialAlternatives": frame.NoMaterialAlternatives,
		"completionRationale":    strings.TrimSpace(frame.CompletionRationale),
	}
}

func compactEnvironmentFrame(env EnvironmentFrame) map[string]any {
	out := map[string]any{
		"workspaceKind":     strings.TrimSpace(env.WorkspaceKind),
		"unitOfWork":        strings.TrimSpace(env.UnitOfWork),
		"primaryObjects":    compactStrings(env.PrimaryObjects),
		"entitySemantics":   compactStrings(env.EntitySemantics),
		"edgeSemantics":     compactStrings(env.EdgeSemantics),
		"artifactSemantics": compactStrings(env.ArtifactSemantics),
		"availableActions":  compactStrings(env.AvailableActions),
		"knownContextRefs":  compactStrings(env.KnownContextRefs),
		"metadataFields":    compactStrings(env.MetadataFields),
		"targetEntity":      env.TargetEntity,
	}
	if len(env.ObservedRelations) > 0 {
		out["observedRelations"] = env.ObservedRelations[:min(len(env.ObservedRelations), 6)]
	}
	return out
}

func buildActiveFrontierSnapshot(state *InvestigationState) ActiveFrontierSnapshot {
	snapshot := ActiveFrontierSnapshot{CandidateActions: []string{}}
	if state == nil {
		return snapshot
	}
	snapshot.Turn = state.Turn
	snapshot.Bottleneck = detectCurrentBottleneck(state)
	snapshot.PathContinuity = strings.TrimSpace(state.PathResult.ContinuityStatus)
	snapshot.ReplanReason = strings.TrimSpace(state.ReplanReason)
	snapshot.OpenQuestions = compactStrings(append([]string{}, state.OpenQuestions...))
	if len(snapshot.OpenQuestions) > 4 {
		snapshot.OpenQuestions = snapshot.OpenQuestions[:4]
	}
	if len(state.ActionLog) > 0 {
		from := max(0, len(state.ActionLog)-4)
		snapshot.RecentActions = append([]string{}, state.ActionLog[from:]...)
	}
	ids := topBranchIDs(state, 3)
	for _, id := range ids {
		b := state.Branches[id]
		if b == nil {
			continue
		}
		h := state.Hypotheses[id]
		item := FrontierBranchSnapshot{
			BranchID:             id,
			Status:               b.Status,
			ResearchRounds:       b.ResearchRounds,
			LastConfidence:       b.LastConfidence,
			ExpectedInfoGain:     branchExpectedInfoGain(state, id),
			Question:             b.Branch.Question,
			Role:                 b.Branch.Role,
			BroadScan:            b.Branch.BroadScan,
			ProductHints:         compactStrings(append([]string{}, b.Branch.ProductHints...)),
			OpenQuestions:        compactStrings(append([]string{}, b.OpenQuestions...)),
			LocalReady:           h != nil && h.LocalReady,
			TargetBound:          h != nil && h.TargetBound,
			MaterializationReady: h != nil && h.MaterializationReady,
		}
		if h != nil {
			item.TargetRelevance = h.TargetRelevance
			item.PursuitDecision = h.PursuitDecision
			item.RouteHypothesis = h.RouteHypothesis
			item.SuggestedBranchIDs = compactStrings(append([]string{}, h.SuggestedBranchIDs...))
		}
		if len(item.OpenQuestions) > 2 {
			item.OpenQuestions = item.OpenQuestions[:2]
		}
		snapshot.TopBranches = append(snapshot.TopBranches, item)
	}
	for _, item := range buildDecisionCandidates(state) {
		if !item.Available {
			continue
		}
		label := item.Action
		if item.BranchID != "" {
			label += ":" + item.BranchID
		}
		snapshot.CandidateActions = append(snapshot.CandidateActions, label)
	}
	if rec := compactActionRecommendation(state.LatestRecommendedAction); len(rec) > 0 {
		label := firstNonEmpty(stringValue(rec, "action"), "action")
		if branch := stringValue(rec, "branchId"); branch != "" {
			label = label + ":" + branch
		}
		snapshot.CandidateActions = compactStrings(append(snapshot.CandidateActions, label))
	}
	return snapshot
}

func compactPlanForModel(plan InvestigationPlan) map[string]any {
	out := map[string]any{
		"goal":                    strings.TrimSpace(plan.Goal),
		"missionRole":             compactRoleFrame(plan.MissionRole),
		"planVersion":             max(plan.PlanVersion, 1),
		"materializationStrategy": strings.TrimSpace(plan.MaterializationStrategy),
	}
	out["focusProducts"] = compactStrings(plan.FocusProducts)
	out["focusGeos"] = compactStrings(plan.FocusGeos)
	out["focusActors"] = compactStrings(plan.FocusActors)
	branches := make([]map[string]any, 0, min(len(plan.Branches), 4))
	for idx, b := range plan.Branches {
		if idx >= 4 {
			break
		}
		branches = append(branches, map[string]any{
			"id":           strings.TrimSpace(b.ID),
			"role":         strings.TrimSpace(b.Role),
			"question":     strings.TrimSpace(b.Question),
			"broadScan":    b.BroadScan,
			"requiresPath": b.RequiresPath,
			"productHints": compactStrings(b.ProductHints),
			"dependsOn":    compactStrings(b.DependsOn),
		})
	}
	out["branches"] = branches
	return out
}

func compactPlanningContext(graphCtx map[string]any) map[string]any {
	if len(graphCtx) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	for _, key := range []string{"query", "targetCanonicalPath", "targetFragment", "outgoingEdges", "incomingEdges"} {
		if value, ok := graphCtx[key]; ok {
			out[key] = value
		}
	}
	return out
}

func withRuntimeFrames(req InvestigationRequest, graphCtx map[string]any) InvestigationRequest {
	req = normalizeRequest(req)
	req.Goal = buildGoalFrame(req)
	req.Environment = buildEnvironmentFrame(req, graphCtx)
	req.Completeness = buildCompletenessFrame(req)
	return normalizeRequest(req)
}

func buildGoalFrame(req InvestigationRequest) GoalFrame {
	goal := normalizeGoalFrame(req.Goal)
	goal.Objective = firstNonEmpty(goal.Objective, strings.TrimSpace(req.Query), "Investigate graph structure around the target entity")
	goal.PrimaryQuestion = firstNonEmpty(goal.PrimaryQuestion, strings.TrimSpace(req.Query), strings.TrimSpace(req.TargetCanonicalPath))
	goal.UserIntent = firstNonEmpty(goal.UserIntent, strings.TrimSpace(req.Query), "study linked entities and supply-chain relations")
	goal.TargetCanonicalPath = firstNonEmpty(goal.TargetCanonicalPath, strings.TrimSpace(req.TargetCanonicalPath))
	switch {
	case req.CompareOnly:
		goal.WriteDisposition = firstNonEmpty(goal.WriteDisposition, "compare_only_no_graph_writes")
	case req.AllowWrites:
		goal.WriteDisposition = firstNonEmpty(goal.WriteDisposition, "land_verified_graph_artifacts")
	default:
		goal.WriteDisposition = firstNonEmpty(goal.WriteDisposition, "analyze_without_writes")
	}
	goal.OutputContract = compactStrings(append(goal.OutputContract,
		"graph-native findings about entities, relations, and artifacts",
		"open questions and gaps when substrate is missing",
		"path verification and materialized artifacts only when grounded",
		"proposed graph operations anchored to canonical paths",
	))
	goal.SuccessCriteria = compactStrings(append(goal.SuccessCriteria,
		"distinguish confirmed graph structure from hypotheses and candidates",
		"ground findings in canonical entities, typed relations, and metadata",
		"preserve canonical product control plane semantics for synonyms, transforms, and assemblies",
		"surface business-meaningful supply-chain dependencies or gaps",
	))
	goal.StopConditions = compactStrings(append(goal.StopConditions,
		"bounded turn budget exhausted",
		"critic withholds further landing and no higher-value graph action remains",
		"no unresolved branch or graph operation can materially improve the investigation",
	))
	return normalizeGoalFrame(goal)
}

func buildEnvironmentFrame(req InvestigationRequest, graphCtx map[string]any) EnvironmentFrame {
	env := normalizeEnvironmentFrame(req.Environment)
	env.WorkspaceKind = firstNonEmpty(env.WorkspaceKind, "horizon_graph_workspace")
	env.UnitOfWork = firstNonEmpty(env.UnitOfWork, "canonical entities, typed relations, metadata, and materialized artifacts")
	env.PrimaryObjects = compactStrings(append(env.PrimaryObjects,
		"candidate collections",
		"graph entities",
		"materialized artifacts",
		"typed dependency edges",
	))
	env.EntitySemantics = compactStrings(append(env.EntitySemantics,
		"canonicalPath is the stable identity for graph entities",
		"product identity is determined by canonical product path, not raw label",
		"unknown products or product pairs belong in candidate collections instead of ad hoc vocabularies",
		"synonym equivalence, transforms, assemblies, and generic dependencies are distinct semantics",
	))
	env.EdgeSemantics = compactStrings(append(env.EdgeSemantics,
		"ASSEMBLES_INTO captures nearest assembly steps",
		"SUPPLY_LINK and DEPENDS_ON are not substitutes for product identity or transforms",
		"TRANSFORMS_INTO captures canonical product conversion steps",
		"typed edges carry metadata and should be reasoned over as graph facts or hypotheses",
	))
	env.ArtifactSemantics = compactStrings(append(env.ArtifactSemantics,
		"materialized artifacts should carry canonical refs after normalization",
		"path and assembly artifacts represent landed graph structure, not just prose summaries",
		"unresolved evidence should stay as gaps or candidate operations, not landed artifacts",
	))
	env.AvailableActions = compactStrings(append(env.AvailableActions,
		"ask missing-middle bridge questions when endpoints exist but continuity is missing",
		"challenge or accept proposed graph operations",
		"materialize verified paths and assemblies",
		"propose graph-native operations and open gaps",
		"research grounded hypotheses about entities and relations",
		"trace supply-chain paths across canonical product steps",
	))
	env.KnownContextRefs = compactStrings(append(env.KnownContextRefs,
		firstNonEmpty(strings.TrimSpace(req.Query), ""),
		firstNonEmpty(strings.TrimSpace(req.TargetCanonicalPath), ""),
		stringValue(graphCtx, "query"),
		stringValue(graphCtx, "targetCanonicalPath"),
	))
	if target := mapValue(graphCtx["targetFragment"]); len(target) > 0 {
		env.TargetEntity = target
		if id := stringValue(target, "id"); id != "" {
			env.KnownContextRefs = compactStrings(append(env.KnownContextRefs, id))
		}
	} else if strings.TrimSpace(req.TargetCanonicalPath) != "" {
		env.TargetEntity = map[string]any{
			"canonicalPath": strings.TrimSpace(req.TargetCanonicalPath),
		}
	}
	observed := appendObservedRelations(nil, graphCtx["outgoingEdges"], "outgoing")
	observed = appendObservedRelations(observed, graphCtx["incomingEdges"], "incoming")
	env.ObservedRelations = observed
	env.MetadataFields = compactStrings(append(env.MetadataFields, collectEnvironmentMetadataFields(env.TargetEntity, env.ObservedRelations)...))
	return normalizeEnvironmentFrame(env)
}

func buildCompletenessFrame(req InvestigationRequest) CompletenessFrame {
	frame := normalizeCompletenessFrame(req.Completeness)
	query := strings.TrimSpace(firstNonEmpty(req.Query, req.TargetCanonicalPath))
	frame.BroadScanRequired = frame.BroadScanRequired || query != ""
	frame.BroadScanQuestion = firstNonEmpty(frame.BroadScanQuestion, fmt.Sprintf("Before narrowing, what are the major plausible routes, actors, and branches of reasoning relevant to: %s? After finding one route, check whether any other materially different routes remain.", query))
	frame.ExplorationStatus = firstNonEmpty(frame.ExplorationStatus, ternary(frame.BroadScanCompleted, "in_progress", "pending"))
	frame.CompletionRationale = firstNonEmpty(frame.CompletionRationale, "Do not stop after the first good route until broad scan says there are no other material alternatives or they have been explicitly deprioritized.")
	return normalizeCompletenessFrame(frame)
}

func appendObservedRelations(base []map[string]any, raw any, direction string) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		for _, item := range typed {
			if len(item) == 0 {
				continue
			}
			row := map[string]any{
				"direction": strings.TrimSpace(direction),
			}
			for k, v := range item {
				row[k] = v
			}
			base = append(base, row)
		}
	case []any:
		for _, item := range typed {
			obj, ok := item.(map[string]any)
			if !ok || len(obj) == 0 {
				continue
			}
			row := map[string]any{
				"direction": strings.TrimSpace(direction),
			}
			for k, v := range obj {
				row[k] = v
			}
			base = append(base, row)
		}
	}
	return base
}

func collectEnvironmentMetadataFields(target map[string]any, relations []map[string]any) []string {
	fields := []string{}
	for k := range target {
		fields = append(fields, k)
	}
	for _, relation := range relations {
		for k := range relation {
			fields = append(fields, k)
		}
	}
	return compactStrings(fields)
}

func compactResearchDeltas(results []ResearchResult, branches []FrontierBranchSnapshot) []map[string]any {
	if len(results) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, item := range branches {
		if id := strings.TrimSpace(item.BranchID); id != "" {
			allowed[id] = struct{}{}
		}
	}
	out := make([]map[string]any, 0, len(results))
	for i := len(results) - 1; i >= 0; i-- {
		r := results[i]
		id := strings.TrimSpace(r.BranchID)
		if len(allowed) > 0 {
			if _, ok := allowed[id]; !ok {
				continue
			}
		}
		out = append(out, map[string]any{
			"branchId":        id,
			"confidence":      r.Confidence,
			"canonicalRefs":   compactStrings(r.CanonicalRefs),
			"evidencePoints":  compactStrings(limitStrings(r.EvidencePoints, 3)),
			"openQuestions":   compactStrings(limitStrings(r.OpenQuestions, 2)),
			"nextImplication": firstNonEmpty(strings.Join(compactStrings(r.RecommendedPathProducts), ","), strings.TrimSpace(r.Summary)),
		})
		if len(out) >= 6 {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func buildBridgeContext(state *InvestigationState, branchID string) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	branch := state.Branches[branchID]
	hypothesis := state.Hypotheses[branchID]
	latest := latestResearchForBranch(state, branchID)
	knownProducts := compactStrings(append(
		append([]string{}, collectBranchResearchProducts(state, branchID)...),
		append([]string{}, state.Plan.FocusProducts...)...,
	))
	knownActors := []string{}
	knownGeos := []string{}
	knownNodes := []string{}
	if branch != nil {
		knownActors = append(knownActors, branch.Branch.ActorHints...)
		knownGeos = append(knownGeos, branch.Branch.GeoHints...)
		knownProducts = append(knownProducts, branch.Branch.ProductHints...)
	}
	if latest != nil {
		knownActors = append(knownActors, latest.ActorRefs...)
		knownGeos = append(knownGeos, latest.GeoRefs...)
		knownNodes = append(knownNodes, latest.CanonicalRefs...)
		knownNodes = append(knownNodes, latest.ProductRefs...)
	}
	knownActors = compactStrings(append(knownActors, state.LatestCritic.FocusActors...))
	knownGeos = compactStrings(append(knownGeos, state.LatestCritic.FocusGeos...))
	knownProducts = compactStrings(append(knownProducts, state.LatestCritic.PathProducts...))
	knownNodes = compactStrings(knownNodes)

	endpointA := selectBridgeEndpointA(state, branchID, knownNodes, knownProducts, knownActors, knownGeos)
	endpointB := selectBridgeEndpointB(state, branchID, knownProducts, knownActors, knownGeos)
	openGaps := append([]string{}, state.PathResult.OpenGaps...)
	if branch != nil {
		openGaps = append(openGaps, branch.OpenQuestions...)
	}
	if latest != nil {
		openGaps = append(openGaps, latest.OpenQuestions...)
	}
	bridgeQuestion := fmt.Sprintf(
		"What sits between %s and %s in a real supply-chain route here? If the route exists, identify the missing middle as intermediate entities, transforms, logistics legs, commercial links, or ports. If the route does not exist, say why and point to a better continuation.",
		firstNonEmpty(endpointA, "the known upstream node"),
		firstNonEmpty(endpointB, "the target-facing downstream node"),
	)
	context := map[string]any{
		"branchId":           strings.TrimSpace(branchID),
		"branchQuestion":     lookupBranchQuestion(state, branchID),
		"endpointA":          endpointA,
		"endpointB":          endpointB,
		"bridgeQuestion":     bridgeQuestion,
		"articulationGoal":   "Write out the missing middle as an ordered chain of intermediate steps the runtime can turn into artifacts.",
		"knownProducts":      compactStrings(knownProducts),
		"knownActors":        compactStrings(knownActors),
		"knownGeos":          compactStrings(knownGeos),
		"knownNodes":         knownNodes,
		"openGaps":           compactStrings(openGaps),
		"pathOpenGaps":       compactStrings(state.PathResult.OpenGaps),
		"pathContinuity":     strings.TrimSpace(state.PathResult.ContinuityStatus),
		"routeHypothesis":    "",
		"targetCanonicalRef": firstNonEmpty(strings.TrimSpace(state.Goal.TargetCanonicalPath), stringValue(state.Environment.TargetEntity, "canonicalPath")),
	}
	if hypothesis != nil {
		context["routeHypothesis"] = strings.TrimSpace(hypothesis.RouteHypothesis)
		context["targetRelevance"] = hypothesis.TargetRelevance
		context["localReady"] = hypothesis.LocalReady
		context["targetBound"] = hypothesis.TargetBound
	}
	return context
}

func selectBridgeEndpointA(state *InvestigationState, branchID string, knownNodes, knownProducts, knownActors, knownGeos []string) string {
	if latest := latestResearchForBranch(state, branchID); latest != nil {
		for _, value := range append(append([]string{}, latest.CanonicalRefs...), latest.ProductRefs...) {
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	for _, value := range append(append([]string{}, knownNodes...), append(knownProducts, append(knownActors, knownGeos...)...)...) {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func selectBridgeEndpointB(state *InvestigationState, _ string, knownProducts, knownActors, knownGeos []string) string {
	if state == nil {
		return ""
	}
	for _, value := range []string{
		strings.TrimSpace(state.Goal.TargetCanonicalPath),
		stringValue(state.Environment.TargetEntity, "canonicalPath"),
	} {
		if value != "" {
			return value
		}
	}
	for _, value := range append(append([]string{}, state.LatestCritic.FocusActors...), append(state.LatestCritic.FocusGeos, append(knownActors, knownGeos...)...)...) {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, value := range append(state.LatestCritic.PathProducts, knownProducts...) {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func encodeResearchDeltas(results []ResearchResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, researchDeltaMap(ResearchDelta{
			BranchID:        strings.TrimSpace(r.BranchID),
			Confidence:      r.Confidence,
			CanonicalRefs:   compactStrings(r.CanonicalRefs),
			EvidencePoints:  limitStrings(r.EvidencePoints, 3),
			OpenQuestions:   limitStrings(r.OpenQuestions, 2),
			NextImplication: firstNonEmpty(strings.Join(compactStrings(r.RecommendedPathProducts), ","), strings.TrimSpace(r.Summary)),
		}))
	}
	return out
}

func compactStateForTurn(state *InvestigationState, action string) {
	if state == nil {
		return
	}
	if state.Turn-state.LastCompactedTurn < 2 && action != "materialize" && action != "review" {
		return
	}
	compactCompletedBranches(state)
	state.OpenQuestions = compactStrings(dedupAndPruneOpenQuestions(state.OpenQuestions, 6))
	compactOutcomes(state)
	compactDecisions(state)
	compactResearchResults(state)
	state.LastCompactedTurn = state.Turn
}

func compactOutcomes(state *InvestigationState) {
	if state == nil || len(state.Outcomes) <= 4 {
		return
	}
	state.Outcomes = state.Outcomes[len(state.Outcomes)-4:]
}

func compactDecisions(state *InvestigationState) {
	if state == nil || len(state.Decisions) <= 3 {
		return
	}
	state.Decisions = state.Decisions[len(state.Decisions)-3:]
}

func compactResearchResults(state *InvestigationState) {
	if state == nil || len(state.ResearchResults) <= 4 {
		return
	}
	kept := state.ResearchResults[len(state.ResearchResults)-4:]
	for i := range kept {
		if len(kept[i].EvidencePoints) > 3 {
			kept[i].EvidencePoints = kept[i].EvidencePoints[:3]
		}
		if len(kept[i].OpenQuestions) > 3 {
			kept[i].OpenQuestions = kept[i].OpenQuestions[:3]
		}
	}
	state.ResearchResults = kept
	if len(state.BridgeResults) > 3 {
		state.BridgeResults = state.BridgeResults[len(state.BridgeResults)-3:]
	}
	if len(state.GapDiagnoses) > 4 {
		state.GapDiagnoses = state.GapDiagnoses[len(state.GapDiagnoses)-4:]
	}
	if len(state.RecommendedActions) > 3 {
		state.RecommendedActions = state.RecommendedActions[len(state.RecommendedActions)-3:]
	}
	if len(state.ReasoningTrace) > 8 {
		state.ReasoningTrace = state.ReasoningTrace[len(state.ReasoningTrace)-8:]
	}
	if len(state.RuntimeDiagnosis) > 6 {
		state.RuntimeDiagnosis = state.RuntimeDiagnosis[len(state.RuntimeDiagnosis)-6:]
	}
}

func compactCompletedBranches(state *InvestigationState) {
	if state == nil {
		return
	}
	for _, id := range state.BranchOrder {
		b := state.Branches[id]
		if b == nil {
			continue
		}
		if b.Status != BranchStatusDone && b.Status != BranchStatusRejected {
			continue
		}
		if len(b.OpenQuestions) > 0 {
			b.OpenQuestions = nil
		}
		if len(b.History) > 1 {
			last := b.History[len(b.History)-1]
			b.History = []BranchHistoryEntry{{
				Turn:   last.Turn,
				Action: "summary",
				Note:   firstNonEmpty(last.Note, string(b.Status)),
			}}
		}
		if h := state.Hypotheses[id]; h != nil && len(h.Evidence) > 2 {
			h.Evidence = h.Evidence[:2]
		}
	}
	if len(state.StructuredOpenQuestions) > 10 {
		state.StructuredOpenQuestions = state.StructuredOpenQuestions[len(state.StructuredOpenQuestions)-10:]
	}
}

func (p *Processor) runChildTask(ctx context.Context, rt *agentmeshruntime.Service, parent *contracts.MeshTask, capability, dedupKey string, input map[string]any) (*contracts.MeshTask, map[string]any, error) {
	if p != nil && p.runChildTaskFn != nil {
		return p.runChildTaskFn(ctx, rt, parent, capability, dedupKey, input)
	}
	childInput := enrichScoutModelInput(input, parent)
	child, err := rt.CreateTask(ctx, &contracts.MeshTask{
		TenantID:      parent.TenantID,
		Capability:    capability,
		ParentTaskID:  parent.ID,
		Input:         childInput,
		Budget:        parent.Budget,
		SessionRef:    strings.TrimSpace(parent.SessionRef),
		MemoryBankRef: strings.TrimSpace(parent.MemoryBankRef),
		Lease: contracts.Lease{
			DedupKey: strings.TrimSpace(dedupKey),
		},
	}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, true)
	if err != nil {
		return nil, nil, err
	}
	if err := rt.ExecuteTask(ctx, child.ID, childLeaseOwner(parent.ID, capability)); err != nil {
		return nil, nil, err
	}
	finished, err := rt.Store().GetTask(ctx, child.ID)
	if err != nil {
		return nil, nil, err
	}
	if finished == nil {
		return child, nil, fmt.Errorf("child task %s not found after execution", child.ID)
	}
	if finished.Status == contracts.TaskStatusFailed || finished.Status == contracts.TaskStatusCancelled {
		return finished, finished.Output, fmt.Errorf("child task %s failed: %s", finished.ID, strings.TrimSpace(finished.LastError))
	}
	return finished, finished.Output, nil
}

func (p *Processor) runModelObject(ctx context.Context, rt *agentmeshruntime.Service, task *contracts.MeshTask, input map[string]any) map[string]any {
	if p != nil && p.runModelObjectFn != nil {
		return p.runModelObjectFn(ctx, rt, task, input)
	}
	if rt == nil || rt.Vertex() == nil || task == nil {
		return nil
	}
	sessionRef := strings.TrimSpace(task.SessionRef)
	if sessionRef == "" && input != nil {
		if ps, ok := input["parentSessionRef"].(string); ok {
			sessionRef = strings.TrimSpace(ps)
		}
	}
	pseudoTask := &contracts.MeshTask{
		ID:         task.ID + ":" + strings.TrimSpace(task.Capability),
		TenantID:   task.TenantID,
		Capability: task.Capability,
		Input:      input,
		SessionRef: sessionRef,
	}
	body, _, err := rt.Vertex().ExecuteTask(ctx, pseudoTask, false)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("[agentmesh.scout] model execution failed capability=%s err=%v", task.Capability, err)
		}
		return map[string]any{
			"_runtimeError": err.Error(),
		}
	}
	if scoutReasoningLogEnabled() && body != nil {
		scoutReasoningLogf("scout_llm_response task=%s capability=%s finish=%s text=%s",
			task.ID, task.Capability, strings.TrimSpace(stringValue(body, "finishReason")),
			scoutReasoningSingleLine(stringValue(body, "content"), 900))
	}
	parsed := parseContentObject(body)
	if len(parsed) > 0 {
		return parsed
	}
	// Self-repair retry: tell the model the previous response was invalid/truncated JSON.
	finishReason := strings.TrimSpace(stringValue(body, "finishReason"))
	previousContent := strings.TrimSpace(stringValue(body, "content"))
	if len(previousContent) > 1600 {
		previousContent = previousContent[:1600]
	}
	repairInput := map[string]any{}
	for key, value := range input {
		repairInput[key] = value
	}
	repairInput["_repair"] = map[string]any{
		"phase":                 "json_repair_retry",
		"previousFinishReason":  finishReason,
		"previousOutputSnippet": previousContent,
		"error":                 "previous model output was not a valid JSON object",
		"instruction":           "Retry once. Return ONLY valid compact JSON object that matches capability schema. Omit optional verbose arrays first.",
	}
	repairTask := &contracts.MeshTask{
		ID:         task.ID + ":" + strings.TrimSpace(task.Capability) + ":repair1",
		TenantID:   task.TenantID,
		Capability: task.Capability,
		Input:      repairInput,
		SessionRef: sessionRef,
	}
	repairBody, _, repairErr := rt.Vertex().ExecuteTask(ctx, repairTask, false)
	if repairErr != nil {
		if p.logger != nil {
			p.logger.Printf("[agentmesh.scout] model repair failed capability=%s err=%v", task.Capability, repairErr)
		}
		return map[string]any{
			"_runtimeError": fmt.Sprintf("model output parse failed (finishReason=%s) and repair call failed: %v", finishReason, repairErr),
		}
	}
	if scoutReasoningLogEnabled() && repairBody != nil {
		scoutReasoningLogf("scout_llm_repair task=%s capability=%s finish=%s text=%s",
			task.ID, task.Capability, strings.TrimSpace(stringValue(repairBody, "finishReason")),
			scoutReasoningSingleLine(stringValue(repairBody, "content"), 900))
	}
	if repaired := parseContentObject(repairBody); len(repaired) > 0 {
		return repaired
	}
	repairFinishReason := strings.TrimSpace(stringValue(repairBody, "finishReason"))
	return map[string]any{
		"_runtimeError": fmt.Sprintf("model returned invalid JSON after repair attempt (finishReason=%s, repairFinishReason=%s)", finishReason, repairFinishReason),
	}
}

func modelRuntimeError(parsed map[string]any) string {
	return strings.TrimSpace(stringValue(parsed, "_runtimeError"))
}

func modelRetryClass(errText string) contracts.RetryClass {
	if isRetryableSystemError(fmt.Errorf("%s", strings.TrimSpace(errText))) {
		return contracts.RetryClassTransient
	}
	return contracts.RetryClassPermanent
}

func (p *Processor) resolveProductRefs(ctx context.Context, tenantID string, values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range compactStrings(values) {
		if strings.HasPrefix(value, "/product/") {
			if p.productDictionary != nil {
				if canonical, matched, err := p.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, value); err == nil && matched {
					out = append(out, canonical)
					continue
				}
			}
			out = append(out, value)
			continue
		}
		if p.productDictionary != nil {
			matches, err := p.productDictionary.SearchCanonicalProductPathsForTenant(ctx, tenantID, value, 5)
			if err == nil && len(matches) > 0 {
				out = append(out, matches[0])
				continue
			}
		}
	}
	return compactStrings(out)
}

func (p *Processor) markJobRunning(ctx context.Context, req InvestigationRequest, taskID string) error {
	if req.ScoutingJobID == "" || p.agentJobs == nil {
		return nil
	}
	fields := map[string]any{
		"agenticScout.taskId":        strings.TrimSpace(taskID),
		"agenticScout.startedAt":     nowRFC3339(),
		"agenticScout.compareOnly":   req.CompareOnly,
		"agenticScout.allowWrites":   req.AllowWrites,
		"agenticScout.targetRuntime": "agentic_scout",
	}
	if err := p.agentJobs.UpdateState(ctx, req.ScoutingJobID, fields); err != nil {
		return err
	}
	return p.workflow.TransitionState(ctx, req.ScoutingJobID, horizonmodels.ScoutingJobStateExecution)
}

func (p *Processor) persistFinalResult(ctx context.Context, req InvestigationRequest, job *horizonmodels.ScoutingJob, output map[string]any, review ReviewResult) error {
	if req.ScoutingJobID == "" || p.agentJobs == nil {
		return nil
	}
	agentJob, err := p.agentJobs.Get(ctx, req.ScoutingJobID)
	if err != nil {
		return err
	}
	if agentJob.Result == nil {
		agentJob.Result = map[string]any{}
	}
	agentJob.Result["agenticScout"] = output
	agentJob.Progress = 1
	agentJob.UpdatedAt = time.Now().UTC()
	if agentJob.State == nil {
		agentJob.State = map[string]any{}
	}
	agentJob.State["agenticScoutLastCompletedAt"] = nowRFC3339()
	agentJob.State["agenticScoutReviewAccepted"] = review.Accepted
	agentJob.State["agenticScoutReviewRationale"] = review.Rationale
	agentJob.State["agenticScoutArtifactIds"] = compactStrings(extractArtifactIDs(output))
	if err := p.agentJobs.Update(ctx, agentJob); err != nil {
		return err
	}
	if req.CompareOnly {
		return nil
	}
	if review.Accepted {
		return p.workflow.TransitionState(ctx, req.ScoutingJobID, horizonmodels.ScoutingJobStateVerified)
	}
	if job != nil {
		job.Status = coremodels.AgentJobStatusFailed
	}
	return p.workflow.TransitionState(ctx, req.ScoutingJobID, horizonmodels.ScoutingJobStateFailed)
}

type vertexSearchAdapter struct {
	client *ai.VertexAISearchClient
}

func newVertexSearchAdapter(client *ai.VertexAISearchClient) knowledgeBaseSearcher {
	if client == nil || !client.IsConfigured() {
		return nil
	}
	return &vertexSearchAdapter{client: client}
}

func (a *vertexSearchAdapter) Search(ctx context.Context, tenantID, query string, pageSize int) ([]map[string]any, error) {
	if a == nil || a.client == nil {
		return nil, nil
	}
	filter := `tenantId: ANY("global")`
	if v := strings.TrimSpace(tenantID); v != "" && !strings.EqualFold(v, "global") {
		filter = fmt.Sprintf(`tenantId: ANY("%s","global")`, strings.ReplaceAll(v, `"`, ""))
	}
	resp, err := a.client.Search(ctx, &ai.SearchRequest{
		Query:    strings.TrimSpace(query),
		PageSize: pageSize,
		Filter:   filter,
	})
	if err != nil || resp == nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(resp.Results))
	for _, result := range resp.Results {
		score := result.RelevanceScore
		if result.RankSignals != nil && result.RankSignals.SemanticSimilarityScore > score {
			score = result.RankSignals.SemanticSimilarityScore
		}
		out = append(out, map[string]any{
			"id":      strings.TrimSpace(result.Document.ID),
			"title":   strings.TrimSpace(result.Document.Title),
			"uri":     strings.TrimSpace(result.Document.URI),
			"score":   score,
			"data":    result.Document.StructData,
			"derived": result.Document.DerivedStructData,
		})
	}
	return out, nil
}

func summarizeEdges(edges []*horizonmodels.DependencyEdge) []map[string]any {
	if len(edges) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		out = append(out, map[string]any{
			"id":       strings.TrimSpace(edge.ID),
			"relation": string(edge.Relation),
			"source":   strings.TrimSpace(edge.SourceFragmentID),
			"target":   strings.TrimSpace(edge.TargetFragmentID),
		})
	}
	return out
}

func buildTraceDigest(productRef string, trace *pathbuilder.SupplyChainTraceResult) map[string]any {
	digest := map[string]any{
		"productRef": strings.TrimSpace(productRef),
		"pathCount":  0,
	}
	if trace == nil {
		return digest
	}
	digest["matchingProducts"] = compactStrings(trace.MatchingProducts)
	digest["suppliers"] = compactStrings(trace.Suppliers)
	digest["customers"] = compactStrings(trace.Customers)
	digest["pathCount"] = len(trace.Paths)
	digest["familyCount"] = len(trace.Families)
	return digest
}

func inferTraceDirection(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(query, "upstream") {
		return "upstream"
	}
	return "downstream"
}

func collectResearchPathProducts(items []ResearchResult) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.RecommendedPathProducts...)
	}
	return compactStrings(out)
}

func filterResearchResultsByBranches(items []ResearchResult, branchIDs []string) []ResearchResult {
	if len(items) == 0 || len(branchIDs) == 0 {
		return items
	}
	set := map[string]struct{}{}
	for _, id := range compactStrings(branchIDs) {
		set[id] = struct{}{}
	}
	out := make([]ResearchResult, 0, len(items))
	for _, item := range items {
		if _, ok := set[strings.TrimSpace(item.BranchID)]; ok {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return items
	}
	return out
}

func collectBranchResearchProducts(state *InvestigationState, branchID string) []string {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return nil
	}
	out := []string{}
	for _, rr := range state.ResearchResults {
		if strings.TrimSpace(rr.BranchID) != strings.TrimSpace(branchID) {
			continue
		}
		out = append(out, rr.ProductRefs...)
		out = append(out, rr.RecommendedPathProducts...)
	}
	return compactStrings(out)
}

func topBranchIDs(state *InvestigationState, limit int) []string {
	if state == nil || limit <= 0 {
		return nil
	}
	type item struct {
		id    string
		score float64
	}
	items := make([]item, 0, len(state.BranchOrder))
	for _, id := range state.BranchOrder {
		if state.Branches[id] == nil {
			continue
		}
		score := branchScoreCached(state, id)
		if score <= 0 {
			continue
		}
		items = append(items, item{id: id, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].id < items[j].id
		}
		return items[i].score > items[j].score
	})
	out := make([]string, 0, min(limit, len(items)))
	for _, it := range items {
		out = append(out, it.id)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func branchExpectedInfoGain(state *InvestigationState, branchID string) float64 {
	if state == nil {
		return 0
	}
	if h := state.Hypotheses[branchID]; h != nil && h.ExpectedInfoGain > 0 {
		return clamp01(h.ExpectedInfoGain)
	}
	if b := state.Branches[branchID]; b != nil && b.Branch.ExpectedInfoGain > 0 {
		return clamp01(b.Branch.ExpectedInfoGain)
	}
	return 0
}

func branchScoreCached(state *InvestigationState, branchID string) float64 {
	if state == nil {
		return 0
	}
	b := state.Branches[branchID]
	if b == nil {
		return 0
	}
	if !branchDependenciesSatisfied(state, b) {
		return 0
	}
	score := 0.15 + branchExpectedInfoGain(state, branchID)
	if b.Branch.BroadScan && !state.Completeness.BroadScanCompleted {
		score += 0.55
	}
	if b.Branch.RequiresPath {
		score += 0.2
	}
	if h := state.Hypotheses[branchID]; h != nil {
		score += h.TargetRelevance * 0.45
		switch h.PursuitDecision {
		case BranchPursuitPress:
			score += 0.25
		case BranchPursuitPivot:
			score += 0.18
		case BranchPursuitPark:
			score -= 0.35
		case BranchPursuitDrop:
			return 0
		}
		if h.LocalReady && !h.TargetBound {
			score += 0.08
		}
	}
	score += (1 - clamp01(b.LastConfidence)) * 0.35
	score -= float64(b.ResearchRounds) * 0.07
	score += float64(len(b.OpenQuestions)) * 0.03
	switch b.Status {
	case BranchStatusSupported:
		score -= 0.2
	case BranchStatusDone, BranchStatusRejected, BranchStatusCandidate:
		return 0
	case BranchStatusWeak:
		score += 0.15
	}
	return clamp01(score)
}

func collectScopeProducts(state *InvestigationState, branchIDs []string) []string {
	if state == nil || len(branchIDs) == 0 {
		return nil
	}
	out := []string{}
	for _, id := range compactStrings(branchIDs) {
		if b := state.Branches[id]; b != nil {
			out = append(out, b.Branch.ProductHints...)
		}
		out = append(out, collectBranchResearchProducts(state, id)...)
	}
	return compactStrings(out)
}

func latestResearchForBranch(state *InvestigationState, branchID string) *ResearchResult {
	if state == nil || strings.TrimSpace(branchID) == "" {
		return nil
	}
	for i := len(state.ResearchResults) - 1; i >= 0; i-- {
		if strings.TrimSpace(state.ResearchResults[i].BranchID) == strings.TrimSpace(branchID) {
			return &state.ResearchResults[i]
		}
	}
	return nil
}

func overlapCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, v := range compactStrings(left) {
		set[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
	}
	n := 0
	for _, v := range compactStrings(right) {
		if _, ok := set[strings.ToLower(strings.TrimSpace(v))]; ok {
			n++
		}
	}
	return n
}

func tokenizeForMatching(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '/' || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z'))
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) < 4 {
			continue
		}
		out = append(out, p)
	}
	return compactStrings(out)
}

func tokenOverlapCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, v := range compactStrings(left) {
		set[v] = struct{}{}
	}
	n := 0
	for _, v := range compactStrings(right) {
		if _, ok := set[v]; ok {
			n++
		}
	}
	return n
}

func limitStrings(items []string, n int) []string {
	items = compactStrings(items)
	if n <= 0 || len(items) <= n {
		return items
	}
	return items[:n]
}

func dedupAndPruneOpenQuestions(items []string, maxItems int) []string {
	if maxItems <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, min(len(items), maxItems))
	for _, raw := range compactStrings(items) {
		tokens := tokenizeForMatching(raw)
		key := strings.ToLower(strings.Join(tokens, "|"))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(raw))
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, raw)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func extractArtifactIDs(output map[string]any) []string {
	pathResult := decodePathResult(output["path"])
	materializeResult := decodeMaterializeResult(output["materialize"])
	return compactStrings(append(append([]string{}, pathResult.MaterializedArtifactIDs...), append(materializeResult.ArtifactIDs, pathResult.AssemblyArtifactID)...))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolPtr(v bool) *bool { return &v }

func mapValue(raw any) map[string]any {
	value, _ := raw.(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func decodeRequest(raw any) InvestigationRequest {
	switch typed := raw.(type) {
	case InvestigationRequest:
		return normalizeRequest(typed)
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var req InvestigationRequest
		_ = json.Unmarshal(rawJSON, &req)
		return normalizeRequest(req)
	default:
		return InvestigationRequest{}
	}
}

func decodePlan(raw any) InvestigationPlan {
	switch typed := raw.(type) {
	case InvestigationPlan:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var plan InvestigationPlan
		_ = json.Unmarshal(rawJSON, &plan)
		return plan
	default:
		return InvestigationPlan{}
	}
}

func decodeSessionState(raw any) *InvestigationState {
	switch typed := raw.(type) {
	case *InvestigationState:
		if typed == nil {
			return nil
		}
		return typed
	case InvestigationState:
		copyState := typed
		return &copyState
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var state InvestigationState
		if err := json.Unmarshal(rawJSON, &state); err != nil {
			return nil
		}
		return &state
	default:
		return nil
	}
}

func mergeInvestigationRequests(base InvestigationRequest, overlay InvestigationRequest) InvestigationRequest {
	base = normalizeRequest(base)
	overlay = normalizeRequest(overlay)
	base.TenantID = firstNonEmpty(overlay.TenantID, base.TenantID)
	base.ProjectID = firstNonEmpty(overlay.ProjectID, base.ProjectID)
	base.LensID = firstNonEmpty(overlay.LensID, base.LensID)
	base.ScoutingJobID = firstNonEmpty(overlay.ScoutingJobID, base.ScoutingJobID)
	base.Query = firstNonEmpty(overlay.Query, base.Query)
	base.TargetCanonicalPath = firstNonEmpty(overlay.TargetCanonicalPath, base.TargetCanonicalPath)
	base.CompareOnly = base.CompareOnly || overlay.CompareOnly
	base.AllowWrites = base.AllowWrites || overlay.AllowWrites
	if base.Metadata == nil {
		base.Metadata = map[string]any{}
	}
	for k, v := range overlay.Metadata {
		base.Metadata[k] = v
	}
	base.Goal = mergeGoalFrames(base.Goal, overlay.Goal)
	base.Environment = mergeEnvironmentFrames(base.Environment, overlay.Environment)
	base.Role = mergeRoleFrames(base.Role, overlay.Role)
	base.Completeness = mergeCompletenessFrames(base.Completeness, overlay.Completeness)
	return normalizeRequest(base)
}

func mergeGoalFrames(base GoalFrame, overlay GoalFrame) GoalFrame {
	base = normalizeGoalFrame(base)
	overlay = normalizeGoalFrame(overlay)
	base.Objective = firstNonEmpty(overlay.Objective, base.Objective)
	base.PrimaryQuestion = firstNonEmpty(overlay.PrimaryQuestion, base.PrimaryQuestion)
	base.UserIntent = firstNonEmpty(overlay.UserIntent, base.UserIntent)
	base.TargetCanonicalPath = firstNonEmpty(overlay.TargetCanonicalPath, base.TargetCanonicalPath)
	base.WriteDisposition = firstNonEmpty(overlay.WriteDisposition, base.WriteDisposition)
	base.OutputContract = compactStrings(append(base.OutputContract, overlay.OutputContract...))
	base.SuccessCriteria = compactStrings(append(base.SuccessCriteria, overlay.SuccessCriteria...))
	base.StopConditions = compactStrings(append(base.StopConditions, overlay.StopConditions...))
	return normalizeGoalFrame(base)
}

func mergeEnvironmentFrames(base EnvironmentFrame, overlay EnvironmentFrame) EnvironmentFrame {
	base = normalizeEnvironmentFrame(base)
	overlay = normalizeEnvironmentFrame(overlay)
	base.WorkspaceKind = firstNonEmpty(overlay.WorkspaceKind, base.WorkspaceKind)
	base.UnitOfWork = firstNonEmpty(overlay.UnitOfWork, base.UnitOfWork)
	base.PrimaryObjects = compactStrings(append(base.PrimaryObjects, overlay.PrimaryObjects...))
	base.EntitySemantics = compactStrings(append(base.EntitySemantics, overlay.EntitySemantics...))
	base.EdgeSemantics = compactStrings(append(base.EdgeSemantics, overlay.EdgeSemantics...))
	base.ArtifactSemantics = compactStrings(append(base.ArtifactSemantics, overlay.ArtifactSemantics...))
	base.AvailableActions = compactStrings(append(base.AvailableActions, overlay.AvailableActions...))
	base.KnownContextRefs = compactStrings(append(base.KnownContextRefs, overlay.KnownContextRefs...))
	base.MetadataFields = compactStrings(append(base.MetadataFields, overlay.MetadataFields...))
	if base.TargetEntity == nil {
		base.TargetEntity = map[string]any{}
	}
	for k, v := range overlay.TargetEntity {
		base.TargetEntity[k] = v
	}
	base.ObservedRelations = append(base.ObservedRelations, overlay.ObservedRelations...)
	return normalizeEnvironmentFrame(base)
}

func mergeCompletenessFrames(base CompletenessFrame, overlay CompletenessFrame) CompletenessFrame {
	base = normalizeCompletenessFrame(base)
	overlay = normalizeCompletenessFrame(overlay)
	base.BroadScanRequired = base.BroadScanRequired || overlay.BroadScanRequired
	base.BroadScanQuestion = firstNonEmpty(overlay.BroadScanQuestion, base.BroadScanQuestion)
	base.BroadScanCompleted = base.BroadScanCompleted || overlay.BroadScanCompleted
	base.ExplorationStatus = firstNonEmpty(overlay.ExplorationStatus, base.ExplorationStatus)
	base.KnownAlternativeRoutes = compactStrings(append(base.KnownAlternativeRoutes, overlay.KnownAlternativeRoutes...))
	base.MissingAngles = compactStrings(append(base.MissingAngles, overlay.MissingAngles...))
	base.NoMaterialAlternatives = base.NoMaterialAlternatives || overlay.NoMaterialAlternatives
	base.CompletionRationale = firstNonEmpty(overlay.CompletionRationale, base.CompletionRationale)
	return normalizeCompletenessFrame(base)
}

func stateToRecommendedAction(decision DecisionResult) ActionRecommendation {
	decision = normalizeDecisionResult(decision)
	return normalizeActionRecommendation(ActionRecommendation{
		Action:   firstNonEmpty(decision.ExecutedAction, decision.ChosenAction),
		BranchID: firstNonEmpty(decision.ExecutedBranchID, decision.BranchID),
		Reason:   firstNonEmpty(decision.WhyChosen, decision.StateSummary),
	})
}

func applyAuthoritativeSessionState(state *InvestigationState, raw any) bool {
	if state == nil {
		return false
	}
	next := decodeSessionState(raw)
	if next == nil {
		return false
	}
	if next.Branches == nil {
		next.Branches = map[string]*BranchRuntimeState{}
	}
	if next.Hypotheses == nil {
		next.Hypotheses = map[string]*InvestigationHypothesis{}
	}
	if next.ReasoningTrace == nil {
		next.ReasoningTrace = []ReasoningBeat{}
	}
	if next.RuntimeDiagnosis == nil {
		next.RuntimeDiagnosis = []RuntimeDiagnosis{}
	}
	if next.ActionLog == nil {
		next.ActionLog = []string{}
	}
	if next.SessionRef == "" {
		next.SessionRef = state.SessionRef
	}
	*state = *next
	return true
}

func requiresAuthoritativeSessionStateForAction(action string) bool {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "research", "bridge", "critic", "path", "materialize":
		return true
	default:
		return false
	}
}

func requireSessionStateInput(input map[string]any, capability string) (*InvestigationState, *agentmeshruntime.ProcessResult) {
	sessionState := decodeSessionState(input["sessionState"])
	if sessionState != nil {
		return sessionState, nil
	}
	return nil, &agentmeshruntime.ProcessResult{
		Status:    contracts.TaskStatusFailed,
		LastError: fmt.Sprintf("invalid sessionState: %s requires authoritative sessionState input", strings.TrimSpace(capability)),
	}
}

func decodeBranch(raw any) InvestigationBranch {
	switch typed := raw.(type) {
	case InvestigationBranch:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var branch InvestigationBranch
		_ = json.Unmarshal(rawJSON, &branch)
		return branch
	default:
		return InvestigationBranch{}
	}
}

func decodeRoleFrame(raw any) RoleFrame {
	switch typed := raw.(type) {
	case RoleFrame:
		return normalizeRoleFrame(typed)
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var role RoleFrame
		_ = json.Unmarshal(rawJSON, &role)
		return normalizeRoleFrame(role)
	default:
		return RoleFrame{}
	}
}

func mergeRoleFrames(base RoleFrame, overlay RoleFrame) RoleFrame {
	base = normalizeRoleFrame(base)
	overlay = normalizeRoleFrame(overlay)
	base.Domain = firstNonEmpty(overlay.Domain, base.Domain)
	base.PrimaryRole = firstNonEmpty(overlay.PrimaryRole, base.PrimaryRole)
	base.SupportingRoles = compactStrings(append(base.SupportingRoles, overlay.SupportingRoles...))
	base.ProfessionalQualityBar = firstNonEmpty(overlay.ProfessionalQualityBar, base.ProfessionalQualityBar)
	base.InvestigationPosture = firstNonEmpty(overlay.InvestigationPosture, base.InvestigationPosture)
	base.RoleRationale = firstNonEmpty(overlay.RoleRationale, base.RoleRationale)
	return normalizeRoleFrame(base)
}

func decodeActionRecommendation(raw any) ActionRecommendation {
	switch typed := raw.(type) {
	case ActionRecommendation:
		return normalizeActionRecommendation(typed)
	case map[string]any:
		return normalizeActionRecommendation(ActionRecommendation{
			Action:       stringValue(typed, "action"),
			BranchID:     stringValue(typed, "branchId"),
			Reason:       stringValue(typed, "reason"),
			FailureMode:  stringValue(typed, "failureMode"),
			Question:     stringValue(typed, "question"),
			EndpointA:    stringValue(typed, "endpointA"),
			EndpointB:    stringValue(typed, "endpointB"),
			ProductHints: stringSliceValue(typed["productHints"]),
			GeoHints:     stringSliceValue(typed["geoHints"]),
			ActorHints:   stringSliceValue(typed["actorHints"]),
			RequiresPath: boolValue(typed, "requiresPath", false),
		})
	default:
		return ActionRecommendation{}
	}
}

func decodeGapDiagnosis(raw any) GapDiagnosis {
	switch typed := raw.(type) {
	case GapDiagnosis:
		return normalizeGapDiagnosis(typed)
	case map[string]any:
		return normalizeGapDiagnosis(GapDiagnosis{
			Kind:           stringValue(typed, "kind"),
			Summary:        stringValue(typed, "summary"),
			Why:            stringValue(typed, "why"),
			QuestionClass:  stringValue(typed, "questionClass"),
			NextAction:     stringValue(typed, "nextAction"),
			BranchID:       stringValue(typed, "branchId"),
			EndpointA:      stringValue(typed, "endpointA"),
			EndpointB:      stringValue(typed, "endpointB"),
			EndpointsKnown: boolValue(typed, "endpointsKnown", false),
		})
	default:
		return GapDiagnosis{}
	}
}

func normalizeGapDiagnosis(item GapDiagnosis) GapDiagnosis {
	item.Kind = strings.TrimSpace(strings.ToLower(item.Kind))
	item.Summary = strings.TrimSpace(item.Summary)
	item.Why = strings.TrimSpace(item.Why)
	item.QuestionClass = strings.TrimSpace(strings.ToLower(item.QuestionClass))
	item.NextAction = strings.TrimSpace(strings.ToLower(item.NextAction))
	item.BranchID = strings.TrimSpace(item.BranchID)
	item.EndpointA = strings.TrimSpace(item.EndpointA)
	item.EndpointB = strings.TrimSpace(item.EndpointB)
	if item.EndpointsKnown || item.EndpointA != "" || item.EndpointB != "" {
		item.EndpointsKnown = true
	}
	if !isSupportedGapKind(item.Kind) {
		item.Kind = ""
	}
	if !isSupportedActionRecommendation(item.NextAction) {
		item.NextAction = ""
	}
	if item.QuestionClass == "" {
		switch item.Kind {
		case "missing_substrate":
			item.QuestionClass = "space_scan"
		case "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer":
			item.QuestionClass = "bridge_query"
		case "missing_verification":
			item.QuestionClass = "verification"
		case "missing_adjudication":
			item.QuestionClass = "adjudication"
		case "missing_completeness":
			item.QuestionClass = "completeness_scan"
		}
	}
	return item
}

func mergeGapDiagnosis(base, overlay GapDiagnosis) GapDiagnosis {
	overlay = normalizeGapDiagnosis(overlay)
	if overlay.Kind == "" && overlay.NextAction == "" && overlay.Summary == "" {
		return normalizeGapDiagnosis(base)
	}
	base = normalizeGapDiagnosis(base)
	base.Kind = firstNonEmpty(overlay.Kind, base.Kind)
	base.Summary = firstNonEmpty(overlay.Summary, base.Summary)
	base.Why = firstNonEmpty(overlay.Why, base.Why)
	base.QuestionClass = firstNonEmpty(overlay.QuestionClass, base.QuestionClass)
	base.NextAction = firstNonEmpty(overlay.NextAction, base.NextAction)
	base.BranchID = firstNonEmpty(overlay.BranchID, base.BranchID)
	base.EndpointA = firstNonEmpty(overlay.EndpointA, base.EndpointA)
	base.EndpointB = firstNonEmpty(overlay.EndpointB, base.EndpointB)
	base.EndpointsKnown = base.EndpointsKnown || overlay.EndpointsKnown
	return normalizeGapDiagnosis(base)
}

func isSupportedGapKind(kind string) bool {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "", "missing_substrate", "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer", "missing_verification", "missing_adjudication", "missing_completeness", "no_gap":
		return true
	default:
		return false
	}
}

func isBridgeGapKind(kind string) bool {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "missing_middle", "missing_transform_chain", "missing_institutional_layer", "missing_regulatory_layer", "missing_ownership_layer":
		return true
	default:
		return false
	}
}

func normalizeActionRecommendation(item ActionRecommendation) ActionRecommendation {
	item.Action = strings.TrimSpace(strings.ToLower(item.Action))
	item.BranchID = strings.TrimSpace(item.BranchID)
	item.Reason = strings.TrimSpace(item.Reason)
	item.FailureMode = strings.TrimSpace(item.FailureMode)
	item.Question = strings.TrimSpace(item.Question)
	item.EndpointA = strings.TrimSpace(item.EndpointA)
	item.EndpointB = strings.TrimSpace(item.EndpointB)
	item.ProductHints = compactStrings(item.ProductHints)
	item.GeoHints = compactStrings(item.GeoHints)
	item.ActorHints = compactStrings(item.ActorHints)
	if !isSupportedActionRecommendation(item.Action) {
		item.Action = ""
	}
	return item
}

func mergeActionRecommendation(base, overlay ActionRecommendation) ActionRecommendation {
	overlay = normalizeActionRecommendation(overlay)
	if overlay.Action == "" {
		return normalizeActionRecommendation(base)
	}
	base = normalizeActionRecommendation(base)
	base.Action = overlay.Action
	base.BranchID = firstNonEmpty(overlay.BranchID, base.BranchID)
	base.Reason = firstNonEmpty(overlay.Reason, base.Reason)
	base.FailureMode = firstNonEmpty(overlay.FailureMode, base.FailureMode)
	base.Question = firstNonEmpty(overlay.Question, base.Question)
	base.EndpointA = firstNonEmpty(overlay.EndpointA, base.EndpointA)
	base.EndpointB = firstNonEmpty(overlay.EndpointB, base.EndpointB)
	base.ProductHints = compactStrings(append(base.ProductHints, overlay.ProductHints...))
	base.GeoHints = compactStrings(append(base.GeoHints, overlay.GeoHints...))
	base.ActorHints = compactStrings(append(base.ActorHints, overlay.ActorHints...))
	base.RequiresPath = base.RequiresPath || overlay.RequiresPath
	return normalizeActionRecommendation(base)
}

func isSupportedActionRecommendation(action string) bool {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "", "research", "bridge", "path", "critic", "materialize", "replan", "stop":
		return true
	default:
		return false
	}
}

func decodeDecisionResult(raw any) DecisionResult {
	switch typed := raw.(type) {
	case DecisionResult:
		return typed
	case map[string]any:
		return normalizeDecisionResult(DecisionResult{
			StateSummary:            stringValue(typed, "stateSummary"),
			ChosenAction:            strings.TrimSpace(strings.ToLower(stringValue(typed, "chosenAction"))),
			BranchID:                stringValue(typed, "branchId"),
			WhyChosen:               stringValue(typed, "whyChosen"),
			WhyNotStop:              stringValue(typed, "whyNotStop"),
			WhyNotPath:              stringValue(typed, "whyNotPath"),
			WhyNotBridge:            stringValue(typed, "whyNotBridge"),
			WhyNotResearch:          stringValue(typed, "whyNotResearch"),
			WhyNotCritic:            stringValue(typed, "whyNotCritic"),
			WhyNotMaterialize:       stringValue(typed, "whyNotMaterialize"),
			WhyNotReplan:            stringValue(typed, "whyNotReplan"),
			EvidenceUsed:            compactStrings(stringSliceValue(typed["evidenceUsed"])),
			StopCriteria:            decodeStopCriteriaEvaluations(typed["stopCriteriaEvaluation"]),
			ExpectedInformationGain: floatValue(typed, "expectedInformationGain", 0),
			Confidence:              floatValue(typed, "confidence", 0),
			Candidates:              decodeDecisionCandidates(typed["candidates"]),
			ExecutedAction:          stringValue(typed, "executedAction"),
			ExecutedBranchID:        stringValue(typed, "executedBranchId"),
			ValidationStatus:        stringValue(typed, "validationStatus"),
			ValidationReason:        stringValue(typed, "validationReason"),
		})
	default:
		return DecisionResult{}
	}
}

func decodeDecisionCandidates(raw any) []DecisionCandidate {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]DecisionCandidate, 0, len(items))
	for _, item := range items {
		object := mapValue(item)
		if len(object) == 0 {
			continue
		}
		out = append(out, normalizeDecisionCandidate(DecisionCandidate{
			Action:       strings.TrimSpace(strings.ToLower(stringValue(object, "action"))),
			BranchID:     stringValue(object, "branchId"),
			Available:    boolValue(object, "available", false),
			Reason:       stringValue(object, "reason"),
			Question:     stringValue(object, "question"),
			EndpointA:    stringValue(object, "endpointA"),
			EndpointB:    stringValue(object, "endpointB"),
			ProductHints: compactStrings(stringSliceValue(object["productHints"])),
			GeoHints:     compactStrings(stringSliceValue(object["geoHints"])),
			ActorHints:   compactStrings(stringSliceValue(object["actorHints"])),
		}))
	}
	return out
}

func decodeStopCriteriaEvaluations(raw any) []StopCriterionEvaluation {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]StopCriterionEvaluation, 0, len(items))
	for _, item := range items {
		object := mapValue(item)
		if len(object) == 0 {
			continue
		}
		out = append(out, StopCriterionEvaluation{
			Criterion: strings.TrimSpace(stringValue(object, "criterion")),
			Satisfied: boolValue(object, "satisfied", false),
			Why:       strings.TrimSpace(stringValue(object, "why")),
		})
	}
	return out
}

func decodeResearchResult(raw any) ResearchResult {
	switch typed := raw.(type) {
	case ResearchResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result ResearchResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return ResearchResult{}
	}
}

func decodeResearchResults(raw any) []ResearchResult {
	switch typed := raw.(type) {
	case []ResearchResult:
		return typed
	case []any:
		out := make([]ResearchResult, 0, len(typed))
		for _, item := range typed {
			out = append(out, decodeResearchResult(item))
		}
		return out
	default:
		return nil
	}
}

func decodeBridgeResult(raw any) BridgeResult {
	switch typed := raw.(type) {
	case BridgeResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result BridgeResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return BridgeResult{}
	}
}

func isEmptyBridgeResult(result BridgeResult) bool {
	return strings.TrimSpace(result.Summary) == "" &&
		strings.TrimSpace(result.BridgeQuestion) == "" &&
		len(result.IntermediateNodes) == 0 &&
		len(result.BridgeSteps) == 0 &&
		len(result.RouteSketches) == 0 &&
		len(result.ProposedOps) == 0 &&
		len(result.OpenQuestions) == 0 &&
		len(result.SuggestedSubscans) == 0 &&
		result.Confidence == 0 &&
		!result.BridgeExists
}

func normalizeBridgeSteps(items []BridgeStep) []BridgeStep {
	if len(items) == 0 {
		return nil
	}
	out := make([]BridgeStep, 0, len(items))
	for idx, item := range items {
		item.Index = max(item.Index, idx+1)
		item.StepClass = strings.TrimSpace(item.StepClass)
		item.NodeRef = strings.TrimSpace(item.NodeRef)
		item.NodeLabel = strings.TrimSpace(item.NodeLabel)
		item.NodeKind = strings.TrimSpace(item.NodeKind)
		item.FromRef = strings.TrimSpace(item.FromRef)
		item.ToRef = strings.TrimSpace(item.ToRef)
		item.RelationType = strings.TrimSpace(item.RelationType)
		item.ProductRef = strings.TrimSpace(item.ProductRef)
		item.ActorRef = strings.TrimSpace(item.ActorRef)
		item.GeoRef = strings.TrimSpace(item.GeoRef)
		item.WhyNeeded = strings.TrimSpace(item.WhyNeeded)
		item.EvidenceHints = compactStrings(item.EvidenceHints)
		item.OperationalStatus = strings.TrimSpace(item.OperationalStatus)
		if item.StepClass == "" && item.RelationType != "" {
			item.StepClass = "edge"
		}
		if item.StepClass == "" && (item.NodeRef != "" || item.NodeLabel != "") {
			item.StepClass = "node"
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func inferIntermediateNodesFromBridgeSteps(items []BridgeStep) []string {
	if len(items) == 0 {
		return nil
	}
	out := []string{}
	for _, item := range items {
		if item.NodeRef != "" {
			out = append(out, item.NodeRef)
		}
	}
	return compactStrings(out)
}

func inferRelationTypesFromBridgeSteps(items []BridgeStep) []string {
	if len(items) == 0 {
		return nil
	}
	out := []string{}
	for _, item := range items {
		if item.RelationType != "" {
			out = append(out, item.RelationType)
		}
	}
	return compactStrings(out)
}

func synthesizeBridgeOps(result BridgeResult) []ProposedGraphOp {
	if len(result.BridgeSteps) == 0 {
		return nil
	}
	out := make([]ProposedGraphOp, 0, len(result.BridgeSteps)*2)
	for _, step := range result.BridgeSteps {
		nodeRef := firstNonEmpty(step.NodeRef, step.ActorRef, step.ProductRef, step.GeoRef)
		if nodeRef != "" && isCanonicalPath(nodeRef) {
			out = append(out, ProposedGraphOp{
				Kind:        "create_fragment",
				SubjectPath: nodeRef,
				Rationale:   firstNonEmpty(step.WhyNeeded, "Bridge articulation identified this intermediate node."),
				Status:      "proposed",
			})
		}
		if step.FromRef != "" && step.ToRef != "" && isCanonicalPath(step.FromRef) && isCanonicalPath(step.ToRef) {
			edgeType := firstNonEmpty(step.RelationType, inferBridgeRelationType(step))
			if edgeType != "" {
				out = append(out, ProposedGraphOp{
					Kind:        "create_edge",
					SubjectPath: step.FromRef,
					ObjectPath:  step.ToRef,
					EdgeType:    edgeType,
					Rationale:   firstNonEmpty(step.WhyNeeded, "Bridge articulation connected these adjacent steps."),
					Status:      "proposed",
				})
			}
		}
	}
	return dedupeProposedGraphOps(out)
}

func inferBridgeRelationType(step BridgeStep) string {
	switch strings.ToLower(strings.TrimSpace(step.StepClass)) {
	case "transform", "processor":
		return "TRANSFORMS_INTO"
	case "logistics", "port", "market_link", "contract":
		return "SUPPLY_LINK"
	default:
		return strings.TrimSpace(step.RelationType)
	}
}

func mergeBridgeOps(primary, derived []ProposedGraphOp) []ProposedGraphOp {
	return dedupeProposedGraphOps(append(append([]ProposedGraphOp{}, primary...), derived...))
}

func dedupeProposedGraphOps(items []ProposedGraphOp) []ProposedGraphOp {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ProposedGraphOp, 0, len(items))
	for _, item := range items {
		key := strings.Join([]string{
			strings.TrimSpace(item.Kind),
			strings.TrimSpace(item.SubjectPath),
			strings.TrimSpace(item.ObjectPath),
			strings.TrimSpace(item.EdgeType),
		}, "|")
		if key == "|||" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func synthesizeBridgeSubscans(branch InvestigationBranch, result BridgeResult) []InvestigationBranch {
	if !result.BridgeExists || len(result.BridgeSteps) == 0 {
		return nil
	}
	out := make([]InvestigationBranch, 0, 2)
	for idx, step := range result.BridgeSteps {
		if idx >= 2 {
			break
		}
		if strings.EqualFold(step.OperationalStatus, "operational") {
			continue
		}
		label := firstNonEmpty(step.NodeLabel, step.NodeRef, step.ProductRef, step.ActorRef, step.GeoRef, fmt.Sprintf("bridge-step-%d", idx+1))
		out = append(out, InvestigationBranch{
			ID:               fmt.Sprintf("%s-articulate-%d", firstNonEmpty(branch.ID, "bridge"), idx+1),
			Question:         fmt.Sprintf("Verify the operational status and exact role of %s in the route between %s and %s", label, firstNonEmpty(result.EndpointA, "A"), firstNonEmpty(result.EndpointB, "B")),
			Role:             strings.TrimSpace(branch.Role),
			ProductHints:     compactStrings(append([]string{}, branch.ProductHints...)),
			GeoHints:         compactStrings(append(append([]string{}, branch.GeoHints...), step.GeoRef)),
			ActorHints:       compactStrings(append(append([]string{}, branch.ActorHints...), step.ActorRef)),
			RequiresPath:     true,
			TestCriteria:     compactStrings([]string{"confirm whether the articulated bridge step is operationally real", "clarify whether this step transforms, ships, or contracts the route"}),
			ExpectedInfoGain: 0.72,
		})
	}
	return out
}

func decodeResearchDeltas(raw any) []ResearchResult {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ResearchResult, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ResearchResult{
			BranchID:       stringValue(row, "branchId"),
			Summary:        stringValue(row, "nextImplication"),
			EvidencePoints: stringSliceValue(row["evidencePoints"]),
			CanonicalRefs:  stringSliceValue(row["canonicalRefs"]),
			OpenQuestions:  stringSliceValue(row["openQuestions"]),
			Confidence:     floatValue(row, "confidence", 0),
		})
	}
	return out
}

func decodeBranches(raw any) []InvestigationBranch {
	items := mapSlice(raw)
	out := make([]InvestigationBranch, 0, len(items))
	for _, item := range items {
		out = append(out, decodeBranch(item))
	}
	return out
}

func researchDeltaMap(delta ResearchDelta) map[string]any {
	raw, _ := json.Marshal(delta)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func decodeResearchDelta(raw any) ResearchDelta {
	switch typed := raw.(type) {
	case ResearchDelta:
		return typed
	case map[string]any:
		return ResearchDelta{
			BranchID:          stringValue(typed, "branchId"),
			Confidence:        floatValue(typed, "confidence", 0),
			CanonicalRefs:     stringSliceValue(typed["canonicalRefs"]),
			EvidencePoints:    stringSliceValue(typed["evidencePoints"]),
			OpenQuestions:     stringSliceValue(typed["openQuestions"]),
			NextImplication:   stringValue(typed, "nextImplication"),
			ProposedOps:       decodeProposedGraphOps(typed["proposedGraphOps"]),
			GapDiagnosis:      decodeGapDiagnosis(typed["gapDiagnosis"]),
			RecommendedAction: decodeActionRecommendation(typed["recommendedAction"]),
		}
	default:
		return ResearchDelta{}
	}
}

func decodeProposedGraphOps(raw any) []ProposedGraphOp {
	items := mapSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]ProposedGraphOp, 0, len(items))
	for _, item := range items {
		op := ProposedGraphOp{
			Kind:        stringValue(item, "kind"),
			SubjectPath: stringValue(item, "subjectPath"),
			ObjectPath:  stringValue(item, "objectPath"),
			EdgeType:    stringValue(item, "edgeType"),
			Rationale:   stringValue(item, "rationale"),
			Status:      firstNonEmpty(stringValue(item, "status"), "proposed"),
			BranchID:    stringValue(item, "branchId"),
			SourceTurn:  int(floatValue(item, "sourceTurn", 0)),
		}
		if op.Kind == "" || op.SubjectPath == "" {
			continue
		}
		out = append(out, op)
	}
	return out
}

func decodeGraphOpVerdicts(raw any) []GraphOpVerdict {
	items := mapSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]GraphOpVerdict, 0, len(items))
	for _, item := range items {
		v := GraphOpVerdict{
			OpIndex: int(floatValue(item, "opIndex", -1)),
			Verdict: stringValue(item, "verdict"),
			Reason:  stringValue(item, "reason"),
		}
		if v.OpIndex < 0 || v.Verdict == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func decodeBranchAssessments(raw any) []BranchAssessment {
	items := mapSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]BranchAssessment, 0, len(items))
	for _, item := range items {
		assessment := BranchAssessment{
			BranchID:           stringValue(item, "branchId"),
			TargetRelevance:    clamp01(floatValue(item, "targetRelevance", 0)),
			PursuitDecision:    BranchPursuitDecision(stringValue(item, "pursuitDecision")),
			PursuitReason:      stringValue(item, "pursuitReason"),
			RouteHypothesis:    stringValue(item, "routeHypothesis"),
			AlternativeRoute:   stringValue(item, "alternativeRoute"),
			SuggestedBranchIDs: stringSliceValue(item["suggestedBranchIds"]),
		}
		if strings.TrimSpace(assessment.BranchID) == "" {
			continue
		}
		out = append(out, assessment)
	}
	return out
}

func decodeCompletenessAssessment(raw any) CompletenessAssessment {
	object, ok := raw.(map[string]any)
	if !ok {
		return CompletenessAssessment{}
	}
	return CompletenessAssessment{
		Status:                 stringValue(object, "status"),
		Reason:                 stringValue(object, "reason"),
		AlternativeRoutes:      stringSliceValue(object["alternativeRoutes"]),
		MissingAngles:          stringSliceValue(object["missingAngles"]),
		BroadScanCompleted:     boolValue(object, "broadScanCompleted", false),
		NoMaterialAlternatives: boolValue(object, "noMaterialAlternatives", false),
	}
}

func decodeGoodEnoughAssessment(raw any) GoodEnoughAssessment {
	object, ok := raw.(map[string]any)
	if !ok {
		return GoodEnoughAssessment{}
	}
	return normalizeGoodEnoughAssessment(GoodEnoughAssessment{
		Status:              stringValue(object, "status"),
		ClosureLevel:        stringValue(object, "closureLevel"),
		StopNow:             boolValue(object, "stopNow", false),
		PersistWorkingPath:  boolValue(object, "persistWorkingPath", false),
		FurtherWorkLowValue: boolValue(object, "furtherWorkLowValue", false),
		Reason:              stringValue(object, "reason"),
		OpenNonBlockingGaps: stringSliceValue(object["openNonBlockingGaps"]),
		TargetBranchIDs:     stringSliceValue(object["targetBranchIds"]),
	})
}

func decodeCriticDelta(raw any) CriticDelta {
	switch typed := raw.(type) {
	case CriticDelta:
		return typed
	case map[string]any:
		return CriticDelta{
			AcceptedFindings:        stringSliceValue(typed["acceptedFindings"]),
			RiskyClaims:             stringSliceValue(typed["riskyClaims"]),
			OpenQuestions:           stringSliceValue(typed["openQuestions"]),
			MaterializePath:         boolValue(typed, "materializePath", false),
			PathProducts:            stringSliceValue(typed["pathProducts"]),
			FocusActors:             stringSliceValue(typed["focusActors"]),
			FocusGeos:               stringSliceValue(typed["focusGeos"]),
			Confidence:              floatValue(typed, "confidence", 0),
			WithholdPath:            boolValue(typed, "withholdPath", false),
			WithholdMaterialization: boolValue(typed, "withholdMaterialization", false),
			ForceReplan:             boolValue(typed, "forceReplan", false),
			KillBranchIDs:           stringSliceValue(typed["killBranchIds"]),
			ReprioritizeBranchIDs:   stringSliceValue(typed["reprioritizeBranchIds"]),
			RiskTargetBranchIDs:     stringSliceValue(typed["riskTargetBranchIds"]),
			NewBranches:             decodeBranches(typed["newBranches"]),
			GraphOpVerdicts:         decodeGraphOpVerdicts(typed["graphOpVerdicts"]),
			BranchAssessments:       decodeBranchAssessments(typed["branchAssessments"]),
			CompletenessAssessment:  decodeCompletenessAssessment(typed["completenessAssessment"]),
			GoodEnoughAssessment:    decodeGoodEnoughAssessment(typed["goodEnoughAssessment"]),
			GapDiagnosis:            decodeGapDiagnosis(typed["gapDiagnosis"]),
			RecommendedAction:       decodeActionRecommendation(typed["recommendedAction"]),
		}
	default:
		return CriticDelta{}
	}
}

func decodeMaterializeDelta(raw any) MaterializeDelta {
	switch typed := raw.(type) {
	case MaterializeDelta:
		return typed
	case map[string]any:
		return MaterializeDelta{
			Summary:           stringValue(typed, "summary"),
			Skipped:           stringSliceValue(typed["skipped"]),
			Downgraded:        stringSliceValue(typed["downgraded"]),
			Rejected:          stringSliceValue(typed["rejected"]),
			LandedBranchIDs:   stringSliceValue(typed["landedBranchIds"]),
			RejectedBranchIDs: stringSliceValue(typed["rejectedBranchIds"]),
		}
	default:
		return MaterializeDelta{}
	}
}

func isEmptyResearchDelta(delta ResearchDelta) bool {
	return strings.TrimSpace(delta.NextImplication) == "" &&
		len(delta.EvidencePoints) == 0 &&
		len(delta.CanonicalRefs) == 0 &&
		len(delta.OpenQuestions) == 0 &&
		len(delta.ProposedOps) == 0 &&
		delta.Confidence == 0
}

func isEmptyCriticDelta(delta CriticDelta) bool {
	return len(delta.AcceptedFindings) == 0 &&
		len(delta.RiskyClaims) == 0 &&
		len(delta.OpenQuestions) == 0 &&
		len(delta.PathProducts) == 0 &&
		len(delta.FocusActors) == 0 &&
		len(delta.FocusGeos) == 0 &&
		len(delta.KillBranchIDs) == 0 &&
		len(delta.ReprioritizeBranchIDs) == 0 &&
		len(delta.RiskTargetBranchIDs) == 0 &&
		len(delta.NewBranches) == 0 &&
		len(delta.GraphOpVerdicts) == 0 &&
		len(delta.BranchAssessments) == 0 &&
		strings.TrimSpace(delta.CompletenessAssessment.Status) == "" &&
		strings.TrimSpace(delta.GoodEnoughAssessment.Status) == "" &&
		strings.TrimSpace(delta.GoodEnoughAssessment.Reason) == "" &&
		!delta.GoodEnoughAssessment.StopNow &&
		!delta.GoodEnoughAssessment.PersistWorkingPath &&
		!delta.GoodEnoughAssessment.FurtherWorkLowValue &&
		!delta.MaterializePath &&
		!delta.WithholdPath &&
		!delta.WithholdMaterialization &&
		!delta.ForceReplan &&
		delta.Confidence == 0
}

func decodeCriticResult(raw any) CriticResult {
	switch typed := raw.(type) {
	case CriticResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result CriticResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return CriticResult{}
	}
}

func decodePathResult(raw any) PathResult {
	switch typed := raw.(type) {
	case PathResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result PathResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return PathResult{}
	}
}

func decodeMaterializeResult(raw any) MaterializeResult {
	switch typed := raw.(type) {
	case MaterializeResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result MaterializeResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return MaterializeResult{}
	}
}

func decodeAggregateResult(raw any) AggregateResult {
	switch typed := raw.(type) {
	case AggregateResult:
		return typed
	case map[string]any:
		rawJSON, _ := json.Marshal(typed)
		var result AggregateResult
		_ = json.Unmarshal(rawJSON, &result)
		return result
	default:
		return AggregateResult{}
	}
}

func decodeReviewResult(raw map[string]any, aggregate AggregateResult, path PathResult) ReviewResult {
	if len(raw) == 0 {
		accepted := strings.TrimSpace(aggregate.Summary) != "" || len(path.TraceDigests) > 0
		rationale := "review accepted"
		if !accepted {
			rationale = "no grounded findings or path traces"
		}
		return ReviewResult{
			Accepted:  accepted,
			Rationale: rationale,
			Quality:   ternary(accepted, "good", "insufficient"),
			Issues:    ternaryIssues(!accepted, "No grounded findings or path traces were produced."),
		}
	}
	review := ReviewResult{
		Accepted:  boolValue(raw, "accepted", true),
		Rationale: firstNonEmpty(stringValue(raw, "rationale"), "review completed"),
		Quality:   stringValue(raw, "quality"),
		Issues:    stringSliceValue(raw["issues"]),
	}
	return review
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func ternarySlice(ok bool, yes, no []string) []string {
	if ok {
		return yes
	}
	return no
}

func ternaryIssues(cond bool, value string) []string {
	if !cond || strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}

func ternaryBridgeConfidence(exists bool) float64 {
	if exists {
		return 0.6
	}
	return 0.4
}

func researchMap(result ResearchResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func decisionMap(result DecisionResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func criticMap(result CriticResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func pathMap(result PathResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func materializeMap(result MaterializeResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func aggregateMap(result AggregateResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func reviewMap(result ReviewResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func bridgeMap(result BridgeResult) map[string]any {
	raw, _ := json.Marshal(result)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func fallbackPlan(req InvestigationRequest) InvestigationPlan {
	goal := firstNonEmpty(req.Query, req.TargetCanonicalPath)
	return InvestigationPlan{
		Goal:                 goal,
		FocusProducts:        compactStrings(extractCanonicalTokens(req.Query, "/product/")),
		FocusGeos:            compactStrings(extractCountryHints(req.Query)),
		WantsPathMaterialize: true,
		MaxResearchRounds:    2,
		PlanningRationale: []string{
			"Start from the user query and any existing graph context.",
			"Use canonical product paths where possible and materialize target-scoped supply paths when evidence is sufficient.",
		},
	}
}

func fallbackBranches(req InvestigationRequest, plan InvestigationPlan) []InvestigationBranch {
	products := compactStrings(append([]string{}, plan.FocusProducts...))
	if len(products) == 0 && strings.TrimSpace(req.TargetCanonicalPath) != "" {
		products = compactStrings([]string{req.TargetCanonicalPath})
	}
	question := firstNonEmpty(req.Query, req.TargetCanonicalPath)
	return []InvestigationBranch{
		{
			ID:           "branch-1",
			Question:     question,
			Role:         inferBranchProfessionalRole(req.Role, InvestigationBranch{Question: question, ProductHints: products, GeoHints: compactStrings(plan.FocusGeos), RequiresPath: true}),
			ProductHints: products,
			GeoHints:     compactStrings(plan.FocusGeos),
			RequiresPath: true,
		},
	}
}

func ensurePlanRoleAndCompleteness(req InvestigationRequest, plan InvestigationPlan) InvestigationPlan {
	role := mergeRoleFrames(req.Role, plan.MissionRole)
	plan.MissionRole = role
	plan.Branches = append([]InvestigationBranch{}, plan.Branches...)
	for i := range plan.Branches {
		plan.Branches[i] = normalizeBranchForMission(role, plan.Branches[i])
	}
	if req.Completeness.BroadScanRequired && !planHasBroadScan(plan) {
		plan.Branches = append([]InvestigationBranch{newBroadScanBranch(req, plan)}, plan.Branches...)
	}
	return plan
}

func normalizeBranchForMission(role RoleFrame, branch InvestigationBranch) InvestigationBranch {
	if strings.TrimSpace(branch.Role) == "" {
		branch.Role = inferBranchProfessionalRole(role, branch)
	}
	return branch
}

func inferBranchProfessionalRole(role RoleFrame, branch InvestigationBranch) string {
	return firstNonEmpty(
		strings.TrimSpace(branch.Role),
		strings.TrimSpace(role.PrimaryRole),
	)
}

func planHasBroadScan(plan InvestigationPlan) bool {
	for _, branch := range plan.Branches {
		if branch.BroadScan {
			return true
		}
	}
	return false
}

func newBroadScanBranch(req InvestigationRequest, plan InvestigationPlan) InvestigationBranch {
	role := mergeRoleFrames(req.Role, plan.MissionRole)
	return InvestigationBranch{
		ID:               "broad-scan",
		Question:         firstNonEmpty(req.Completeness.BroadScanQuestion, "What are the major plausible routes and alternative lines of inquiry before narrowing to a single route?"),
		Role:             strings.TrimSpace(role.PrimaryRole),
		ProductHints:     compactStrings(append([]string{}, plan.FocusProducts...)),
		GeoHints:         compactStrings(append([]string{}, plan.FocusGeos...)),
		ActorHints:       compactStrings(append([]string{}, plan.FocusActors...)),
		RequiresPath:     false,
		BroadScan:        true,
		ExpectedInfoGain: 0.98,
		TestCriteria: []string{
			"surface materially different plausible routes before committing to one chain",
			"say explicitly whether other major branches remain unexplored",
		},
	}
}

func mergeResearchDeltaIntoResult(base ResearchResult, delta ResearchDelta) ResearchResult {
	base.Summary = firstNonEmpty(strings.TrimSpace(delta.NextImplication), base.Summary)
	base.EvidencePoints = compactStrings(append(base.EvidencePoints, delta.EvidencePoints...))
	base.CanonicalRefs = compactStrings(append(base.CanonicalRefs, delta.CanonicalRefs...))
	base.OpenQuestions = compactStrings(append(base.OpenQuestions, delta.OpenQuestions...))
	base.Confidence = math.Max(base.Confidence, delta.Confidence)
	base.ProposedOps = append(base.ProposedOps, delta.ProposedOps...)
	base.GapDiagnosis = mergeGapDiagnosis(base.GapDiagnosis, delta.GapDiagnosis)
	base.RecommendedAction = mergeActionRecommendation(base.RecommendedAction, delta.RecommendedAction)
	return base
}

func mergeCriticDelta(base CriticResult, delta CriticDelta) CriticResult {
	base.AcceptedFindings = compactStrings(append(base.AcceptedFindings, delta.AcceptedFindings...))
	base.RiskyClaims = compactStrings(append(base.RiskyClaims, delta.RiskyClaims...))
	base.OpenQuestions = compactStrings(append(base.OpenQuestions, delta.OpenQuestions...))
	base.PathProducts = compactStrings(append(base.PathProducts, delta.PathProducts...))
	base.FocusActors = compactStrings(append(base.FocusActors, delta.FocusActors...))
	base.FocusGeos = compactStrings(append(base.FocusGeos, delta.FocusGeos...))
	base.MaterializePath = base.MaterializePath || delta.MaterializePath
	base.WithholdPath = base.WithholdPath || delta.WithholdPath
	base.WithholdMaterialization = base.WithholdMaterialization || delta.WithholdMaterialization
	base.ForceReplan = base.ForceReplan || delta.ForceReplan
	base.KillBranchIDs = compactStrings(append(base.KillBranchIDs, delta.KillBranchIDs...))
	base.ReprioritizeBranchIDs = compactStrings(append(base.ReprioritizeBranchIDs, delta.ReprioritizeBranchIDs...))
	base.RiskTargetBranchIDs = compactStrings(append(base.RiskTargetBranchIDs, delta.RiskTargetBranchIDs...))
	base.NewBranches = append(base.NewBranches, delta.NewBranches...)
	base.Confidence = math.Max(base.Confidence, delta.Confidence)
	base.GraphOpVerdicts = append(base.GraphOpVerdicts, delta.GraphOpVerdicts...)
	base.BranchAssessments = append(base.BranchAssessments, delta.BranchAssessments...)
	base.CompletenessAssessment = mergeCompletenessAssessment(base.CompletenessAssessment, delta.CompletenessAssessment)
	base.GoodEnoughAssessment = mergeGoodEnoughAssessment(base.GoodEnoughAssessment, delta.GoodEnoughAssessment)
	base.GapDiagnosis = mergeGapDiagnosis(base.GapDiagnosis, delta.GapDiagnosis)
	base.RecommendedAction = mergeActionRecommendation(base.RecommendedAction, delta.RecommendedAction)
	return base
}

func mergeCompletenessAssessment(base, delta CompletenessAssessment) CompletenessAssessment {
	base.Status = firstNonEmpty(strings.TrimSpace(delta.Status), base.Status)
	base.Reason = firstNonEmpty(strings.TrimSpace(delta.Reason), base.Reason)
	base.AlternativeRoutes = compactStrings(append(base.AlternativeRoutes, delta.AlternativeRoutes...))
	base.MissingAngles = compactStrings(append(base.MissingAngles, delta.MissingAngles...))
	base.BroadScanCompleted = base.BroadScanCompleted || delta.BroadScanCompleted
	base.NoMaterialAlternatives = base.NoMaterialAlternatives || delta.NoMaterialAlternatives
	return base
}

func fallbackAggregate(req InvestigationRequest, plan InvestigationPlan, research []ResearchResult, critic CriticResult, path PathResult, materialize MaterializeResult) AggregateResult {
	findings := compactStrings(append(append([]string{}, critic.AcceptedFindings...), collectResearchSummaries(research)...))
	summary := firstNonEmpty(strings.Join(findings, "; "), firstNonEmpty(req.Query, plan.Goal))
	if len(path.TraceDigests) > 0 {
		summary = summary + ". Traced " + fmt.Sprintf("%d product scopes", len(path.TraceDigests))
	}
	return AggregateResult{
		Summary:             strings.TrimSpace(summary),
		Findings:            findings,
		OpenQuestions:       compactStrings(append(append([]string{}, critic.OpenQuestions...), path.OpenGaps...)),
		ArtifactIDs:         compactStrings(append(materialize.ArtifactIDs, path.MaterializedArtifactIDs...)),
		TraceDigests:        path.TraceDigests,
		CandidateProducts:   compactStrings(path.QueriedProducts),
		RecommendedNextStep: ternary(req.CompareOnly, "compare_with_classic", "expand_or_confirm"),
		CompareOnly:         req.CompareOnly,
	}
}

func mergeAggregateResult(base AggregateResult, parsed map[string]any) AggregateResult {
	base.Summary = firstNonEmpty(stringValue(parsed, "summary"), base.Summary)
	base.Findings = compactStrings(append(base.Findings, stringSliceValue(parsed["findings"])...))
	base.OpenQuestions = compactStrings(append(base.OpenQuestions, stringSliceValue(parsed["openQuestions"])...))
	base.ArtifactIDs = compactStrings(append(base.ArtifactIDs, stringSliceValue(parsed["artifactIds"])...))
	base.CandidateProducts = compactStrings(append(base.CandidateProducts, stringSliceValue(parsed["candidateProducts"])...))
	base.RecommendedNextStep = firstNonEmpty(stringValue(parsed, "recommendedNextStep"), base.RecommendedNextStep)
	return base
}

func mergeReviewResult(base ReviewResult, parsed map[string]any) ReviewResult {
	base.Accepted = boolValue(parsed, "accepted", base.Accepted)
	base.Rationale = firstNonEmpty(stringValue(parsed, "rationale"), base.Rationale)
	base.Quality = firstNonEmpty(stringValue(parsed, "quality"), base.Quality)
	base.Issues = compactStrings(append(base.Issues, stringSliceValue(parsed["issues"])...))
	return base
}

func mergePlan(base InvestigationPlan, parsed map[string]any) InvestigationPlan {
	base.Goal = firstNonEmpty(stringValue(parsed, "goal"), base.Goal)
	base.MissionRole = mergeRoleFrames(base.MissionRole, decodeRoleFrame(parsed["missionRole"]))
	base.FocusProducts = compactStrings(append(base.FocusProducts, stringSliceValue(parsed["focusProducts"])...))
	base.FocusGeos = compactStrings(append(base.FocusGeos, stringSliceValue(parsed["focusGeos"])...))
	base.FocusActors = compactStrings(append(base.FocusActors, stringSliceValue(parsed["focusActors"])...))
	base.PlanningRationale = compactStrings(append(base.PlanningRationale, stringSliceValue(parsed["planningRationale"])...))
	base.ExistingGraphPriorities = compactStrings(append(base.ExistingGraphPriorities, stringSliceValue(parsed["existingGraphPriorities"])...))
	base.WantsPathMaterialize = base.WantsPathMaterialize || boolValue(parsed, "wantsPathMaterialize", false)
	base.MaterializationStrategy = firstNonEmpty(stringValue(parsed, "materializationStrategy"), base.MaterializationStrategy)
	if pv := int(floatValue(parsed, "planVersion", 0)); pv > 0 {
		base.PlanVersion = pv
	}
	for _, object := range mapSlice(parsed["branchDependencies"]) {
		base.BranchDependencies = append(base.BranchDependencies, BranchDependency{
			FromBranchID: stringValue(object, "fromBranchId"),
			ToBranchID:   stringValue(object, "toBranchId"),
		})
	}
	if rounds := int(floatValue(parsed, "maxResearchRounds", 0)); rounds > 0 {
		base.MaxResearchRounds = rounds
	}
	for _, object := range mapSlice(parsed["branches"]) {
		base.Branches = append(base.Branches, decodeBranch(object))
	}
	if len(base.Branches) > 0 {
		sort.SliceStable(base.Branches, func(i, j int) bool { return base.Branches[i].ID < base.Branches[j].ID })
	}
	return base
}

func validateMissionRole(plan InvestigationPlan) error {
	role := normalizeRoleFrame(plan.MissionRole)
	if strings.TrimSpace(role.PrimaryRole) == "" {
		return fmt.Errorf("planner did not define missionRole.primaryRole")
	}
	if strings.TrimSpace(role.InvestigationPosture) == "" {
		return fmt.Errorf("planner did not define missionRole.investigationPosture")
	}
	if strings.TrimSpace(role.RoleRationale) == "" {
		return fmt.Errorf("planner did not define missionRole.roleRationale")
	}
	return nil
}

func collectResearchSummaries(items []ResearchResult) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Summary) != "" {
			out = append(out, item.Summary)
		}
	}
	return compactStrings(out)
}

func collectResearchQuestions(items []ResearchResult) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.OpenQuestions...)
	}
	return compactStrings(out)
}

func extractCanonicalTokens(query string, prefix string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.;:!?()[]{}\"'")
		if strings.HasPrefix(field, prefix) {
			out = append(out, field)
		}
	}
	return out
}

func extractCountryHints(query string) []string {
	query = strings.ToLower(query)
	candidates := []string{
		"/loc/us", "/loc/cn", "/loc/de", "/loc/jp", "/loc/kr", "/loc/kz", "/loc/ru", "/loc/in", "/loc/fr", "/loc/gb",
	}
	labelMap := map[string]string{
		"/loc/us": "united states",
		"/loc/cn": "china",
		"/loc/de": "germany",
		"/loc/jp": "japan",
		"/loc/kr": "korea",
		"/loc/kz": "kazakhstan",
		"/loc/ru": "russia",
		"/loc/in": "india",
		"/loc/fr": "france",
		"/loc/gb": "united kingdom",
	}
	out := make([]string, 0, 4)
	for _, candidate := range candidates {
		if strings.Contains(query, labelMap[candidate]) {
			out = append(out, candidate)
		}
	}
	return out
}
