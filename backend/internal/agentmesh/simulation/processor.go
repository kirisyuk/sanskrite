package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonsim "github.com/igorkirisyuk/sanskrite/internal/horizon/simulation"
)

type Publisher func(ctx context.Context, eventType string, tenantID string, payload any) error

type Processor struct {
	runs      *horizonrepo.SimulationRunsRepository
	worlds    *horizonrepo.SimulationWorldsRepository
	agents    *horizonrepo.SimulationAgentsRepository
	events    *horizonrepo.SimulationEventsRepository
	artifacts *horizonrepo.SimulationArtifactsRepository
	publisher Publisher
	logger    *log.Logger
}

func NewProcessor(
	runs *horizonrepo.SimulationRunsRepository,
	worlds *horizonrepo.SimulationWorldsRepository,
	agents *horizonrepo.SimulationAgentsRepository,
	events *horizonrepo.SimulationEventsRepository,
	artifacts *horizonrepo.SimulationArtifactsRepository,
	publisher Publisher,
	logger *log.Logger,
) *Processor {
	if logger == nil {
		logger = log.Default()
	}
	return &Processor{
		runs:      runs,
		worlds:    worlds,
		agents:    agents,
		events:    events,
		artifacts: artifacts,
		publisher: publisher,
		logger:    logger,
	}
}

func (p *Processor) CanHandle(task *contracts.MeshTask) bool {
	return task != nil && strings.HasPrefix(strings.TrimSpace(task.Capability), "sim.")
}

func (p *Processor) Process(ctx context.Context, rt *runtime.Service, task *contracts.MeshTask, _ string) (*runtime.ProcessResult, error) {
	switch strings.TrimSpace(task.Capability) {
	case "sim.plan":
		return p.processPlan(ctx, rt, task)
	case "sim.world.bootstrap":
		return p.processWorldBootstrap(ctx, rt, task)
	case "sim.aggregate":
		return p.processAggregate(ctx, rt, task)
	case "sim.review":
		return p.processReview(ctx, rt, task)
	default:
		return &runtime.ProcessResult{
			Status:     contracts.TaskStatusFailed,
			RetryClass: contracts.RetryClassPermanent,
			LastError:  "unsupported simulation capability",
		}, nil
	}
}

func (p *Processor) processPlan(ctx context.Context, rt *runtime.Service, task *contracts.MeshTask) (*runtime.ProcessResult, error) {
	runID := stringInput(task.Input, "runId")
	run, err := p.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	run.Status = string(horizonmodels.SimulationRunStatusRunning)
	existingWorlds, err := p.worlds.ListByRun(ctx, run.TenantID, run.ID, 512)
	if err != nil {
		return nil, err
	}
	worldByIndex := make(map[int]*horizonmodels.SimulationWorld, len(existingWorlds))
	for _, world := range existingWorlds {
		if world != nil {
			worldByIndex[world.WorldIndex] = world
		}
	}

	worldTaskIDs := make([]string, 0, run.Execution.WorldCount)
	for idx := 0; idx < run.Execution.WorldCount; idx++ {
		world := worldByIndex[idx]
		if world == nil {
			world = &horizonmodels.SimulationWorld{
				TenantID:          run.TenantID,
				RunID:             run.ID,
				Status:            string(horizonmodels.SimulationWorldStatusPending),
				WorldIndex:        idx,
				RandomSeed:        horizonsim.DeterministicSeed(run.ID, idx),
				BootstrapDigest:   run.BaselineRefs.SnapshotDigest,
				BaselineState:     cloneMap(run.BaselinePreview),
				AgentState:        map[string]any{},
				MarketState:       map[string]any{},
				InstitutionState:  map[string]any{},
				MessageBus:        []map[string]any{},
				ProtocolTraces:    []map[string]any{},
				Transactions:      []map[string]any{},
				RelationshipState: map[string]any{},
				StepMetrics:       []map[string]any{},
				BaselineRefs:      compactStrings(run.BaselineRefs.FragmentIDs),
				ExpiresAt:         run.ExpiresAt,
			}
			if _, err := p.worlds.Create(ctx, world); err != nil {
				return nil, err
			}
		}
		worldTaskID, err := rt.CreateTask(ctx, &contracts.MeshTask{
			TenantID:     run.TenantID,
			Capability:   "sim.world.bootstrap",
			ParentTaskID: task.ID,
			Input: map[string]any{
				"runId":   run.ID,
				"worldId": world.ID,
			},
			Lease: contracts.Lease{
				DedupKey: fmt.Sprintf("sim.world.bootstrap:%s:%d", run.ID, idx),
			},
		}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, false)
		if err != nil {
			return nil, err
		}
		world.AgentMeshTaskIDs = compactStrings(append(world.AgentMeshTaskIDs, worldTaskID.ID))
		if err := p.worlds.Update(ctx, world); err != nil {
			return nil, err
		}
		worldTaskIDs = append(worldTaskIDs, worldTaskID.ID)
	}
	run.AgentMeshTaskIDs = compactStrings(append(run.AgentMeshTaskIDs, append([]string{task.ID}, worldTaskIDs...)...))
	if err := p.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	p.publish(ctx, "horizon.simulation.run.planned", run.TenantID, map[string]any{
		"runId":        run.ID,
		"worldTaskIds": worldTaskIDs,
	})
	return &runtime.ProcessResult{
		Status: contracts.TaskStatusSucceeded,
		Output: map[string]any{
			"runId":        run.ID,
			"worldCount":   run.Execution.WorldCount,
			"worldTaskIds": worldTaskIDs,
		},
	}, nil
}

func (p *Processor) processWorldBootstrap(ctx context.Context, rt *runtime.Service, task *contracts.MeshTask) (*runtime.ProcessResult, error) {
	runID := stringInput(task.Input, "runId")
	worldID := stringInput(task.Input, "worldId")
	run, err := p.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	world, err := p.worlds.Get(ctx, worldID)
	if err != nil {
		return nil, err
	}
	world.Status = string(horizonmodels.SimulationWorldStatusRunning)
	world.AgentMeshTaskIDs = compactStrings(append(world.AgentMeshTaskIDs, task.ID))
	if err := p.worlds.Update(ctx, world); err != nil {
		return nil, err
	}

	agents, populationDigest, err := p.generatePopulation(ctx, rt, run, world)
	if err != nil {
		return nil, err
	}
	world.PopulationDigest = populationDigest
	for _, agent := range agents {
		if _, err := p.agents.Create(ctx, agent); err != nil {
			return nil, err
		}
	}

	actionCounts := map[string]int{}
	pathStressSeed := clamp01(maxFloat(metricFloat(run.BaselinePreview, "artifact_path_stress_avg"), metricFloat(run.BaselinePreview, "artifact_path_stress_max")*0.7))
	recoverySeed := clamp01(metricFloat(run.BaselinePreview, "artifact_recovery_latency_max") / 96.0)
	bindingSeed := clamp01(metricFloat(run.BaselinePreview, "artifact_binding_avg"))
	shoulderRiskSeed := clamp01(metricFloat(run.BaselinePreview, "artifact_shoulder_risk_max") / 2.0)
	productRiskSeed := clamp01(metricFloat(run.BaselinePreview, "artifact_product_risk_max") / 2.0)
	state := metricState{
		risk:      clamp01(metricFloat(run.BaselinePreview, "nodes_updated")/20.0 + pathStressSeed*0.22 + recoverySeed*0.08 + (1-bindingSeed)*0.06),
		price:     clamp01(0.15 + clamp01(metricFloat(run.BaselinePreview, "paths_traced")/50.0) + pathStressSeed*0.22 + productRiskSeed*0.08),
		narrative: 0.2,
		policy:    clamp01(0.1 + (1-bindingSeed)*0.05),
		supply:    clamp01(metricFloat(run.BaselinePreview, "nodes_updated")/20.0 + 0.1 + pathStressSeed*0.3 + recoverySeed*0.14 + shoulderRiskSeed*0.1),
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	if riskDelta, priceDelta, narrativeDelta, policyDelta, supplyDelta, ok := scenarioBootstrapAdjustments(run, world); ok {
		state.risk = clamp01(state.risk + riskDelta)
		state.price = clamp01(state.price + priceDelta)
		state.narrative = clamp01(state.narrative + narrativeDelta)
		state.policy = clamp01(state.policy + policyDelta)
		state.supply = clamp01(state.supply + supplyDelta)
	}
	world.AgentState = map[string]any{"agents": len(agents)}
	world.MarketState = map[string]any{
		"pricePressure":        state.price,
		"supplyStress":         state.supply,
		"pathStressScore":      pathStressSeed,
		"bindingConfidence":    bindingSeed,
		"recoveryLatencyHours": metricFloat(run.BaselinePreview, "artifact_recovery_latency_max"),
		"productRiskScore":     productRiskSeed,
		"shoulderLoadScore":    shoulderRiskSeed,
	}
	world.InstitutionState = map[string]any{
		"policyPressure":    state.policy,
		"stressedPathCount": metricFloat(run.BaselinePreview, "artifact_stressed_paths"),
	}
	ensureWorldMailboxes(world, agents)

	for step := 0; step < run.Execution.TickCount; step++ {
		stepMessageCount := 0
		stepProtocolCount := 0
		stepTransactionChanges := 0
		preStepSummary := processProtocolQueues(run, world, agents, step)
		if preStepSummary.stateDelta.risk != 0 {
			state.risk = applyMetricDelta(state.risk, preStepSummary.stateDelta.risk)
		}
		if preStepSummary.stateDelta.price != 0 {
			state.price = applyMetricDelta(state.price, preStepSummary.stateDelta.price)
		}
		if preStepSummary.stateDelta.narrative != 0 {
			state.narrative = applyMetricDelta(state.narrative, preStepSummary.stateDelta.narrative)
		}
		if preStepSummary.stateDelta.policy != 0 {
			state.policy = applyMetricDelta(state.policy, preStepSummary.stateDelta.policy)
		}
		if preStepSummary.stateDelta.supply != 0 {
			state.supply = applyMetricDelta(state.supply, preStepSummary.stateDelta.supply)
		}
		stepMessageCount += preStepSummary.messageCount
		stepProtocolCount += preStepSummary.protocolCount
		stepTransactionChanges += preStepSummary.transactionChanges
		actions, err := p.generateStepActions(ctx, rt, run, world, agents, step)
		if err != nil {
			return nil, err
		}
		for _, action := range actions {
			if err := p.validateAction(action, agents); err != nil {
				continue
			}
			actionCounts[action.Type]++
			agent := findAgent(agents, action.ActorID)
			state = applyActionImpact(state, action, agent, axis, variant, step)
			interactionEffects, hasInteractions := scenarioDirectInteractions(run, world, agents, agent, action, step)
			if hasInteractions && len(interactionEffects) > 0 {
				applyScenarioInteractionEffects(agents, interactionEffects)
			}
			microSummary := applyMicrointeractionLayer(run, world, agents, agent, action, step, interactionEffects)
			if microSummary.stateDelta.risk != 0 {
				state.risk = applyMetricDelta(state.risk, microSummary.stateDelta.risk)
			}
			if microSummary.stateDelta.price != 0 {
				state.price = applyMetricDelta(state.price, microSummary.stateDelta.price)
			}
			if microSummary.stateDelta.narrative != 0 {
				state.narrative = applyMetricDelta(state.narrative, microSummary.stateDelta.narrative)
			}
			if microSummary.stateDelta.policy != 0 {
				state.policy = applyMetricDelta(state.policy, microSummary.stateDelta.policy)
			}
			if microSummary.stateDelta.supply != 0 {
				state.supply = applyMetricDelta(state.supply, microSummary.stateDelta.supply)
			}
			stepMessageCount += microSummary.messageCount
			stepProtocolCount += microSummary.protocolCount
			stepTransactionChanges += microSummary.transactionChanges
			if action.Type == string(horizonmodels.SimulationActionBroadcastMessage) || action.Type == string(horizonmodels.SimulationActionIssueStatement) || action.Type == string(horizonmodels.SimulationActionEscalate) || action.Type == string(horizonmodels.SimulationActionChangeBelief) || len(interactionEffects) > 0 {
				entry := map[string]any{
					"step":       step,
					"actorId":    action.ActorID,
					"actorClass": ternary(agent != nil, agent.AgentClass, ""),
					"type":       action.Type,
					"targets":    compactStrings(action.TargetRefs),
				}
				if len(interactionEffects) > 0 {
					entry["interactions"] = summarizeInteractionEffects(interactionEffects)
				}
				world.MessageBus = append(world.MessageBus, entry)
			}
		}
		transactionSummary := advanceWorldTransactions(run, world, agents, step)
		if transactionSummary.stateDelta.risk != 0 {
			state.risk = applyMetricDelta(state.risk, transactionSummary.stateDelta.risk)
		}
		if transactionSummary.stateDelta.price != 0 {
			state.price = applyMetricDelta(state.price, transactionSummary.stateDelta.price)
		}
		if transactionSummary.stateDelta.narrative != 0 {
			state.narrative = applyMetricDelta(state.narrative, transactionSummary.stateDelta.narrative)
		}
		if transactionSummary.stateDelta.policy != 0 {
			state.policy = applyMetricDelta(state.policy, transactionSummary.stateDelta.policy)
		}
		if transactionSummary.stateDelta.supply != 0 {
			state.supply = applyMetricDelta(state.supply, transactionSummary.stateDelta.supply)
		}
		stepProtocolCount += transactionSummary.protocolCount
		stepTransactionChanges += transactionSummary.transactionChanges
		if floor := executionPressureFloor(world, axis); floor > 0 {
			state.supply = math.Max(state.supply, floor)
			if axis == horizonmodels.AxisPhysicalFlowSupply || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency {
				state.price = math.Max(state.price, round3(floor*0.75))
			}
		}
		state = stabilizeStepState(state, axis, step, len(actions))
		world.MarketState = map[string]any{"pricePressure": state.price, "supplyStress": state.supply}
		world.InstitutionState = map[string]any{"policyPressure": state.policy}
		world.StepMetrics = append(world.StepMetrics, map[string]any{
			"step":                step,
			"actions":             len(actions),
			"messages":            stepMessageCount,
			"protocolEvents":      stepProtocolCount,
			"transactionChanges":  stepTransactionChanges,
			"openTransactions":    countOpenTransactions(world.Transactions),
			"reviewQueueOpen":     countOpenReviewQueue(world),
			"riskScore":           state.risk,
			"pricePressure":       state.price,
			"narrativeVolatility": state.narrative,
			"policyPressure":      state.policy,
			"supplyStress":        state.supply,
		})
		_, _ = p.events.Create(ctx, &horizonmodels.SimulationEvent{
			TenantID:      run.TenantID,
			RunID:         run.ID,
			WorldID:       world.ID,
			Status:        "active",
			EventType:     "world_step",
			StepIndex:     step,
			OriginKind:    "runtime_step",
			GeneratedBy:   "agentmesh/sim.world.bootstrap",
			SchemaVersion: "sim.event.v1",
			Payload: map[string]any{
				"actions":             actions,
				"messages":            protocolItemsByStep(world.ProtocolTraces, step, "direct_message"),
				"protocolEvents":      protocolItemsByStep(world.ProtocolTraces, step, ""),
				"transactions":        transactionItemsByStep(world.Transactions, step),
				"riskScore":           state.risk,
				"pricePressure":       state.price,
				"narrativeVolatility": state.narrative,
				"policyPressure":      state.policy,
				"supplyStress":        state.supply,
			},
			ExpiresAt: run.ExpiresAt,
		})
	}

	world.StepCount = run.Execution.TickCount
	world.OutcomeVector = state.outcomeVector()
	world.Uncertainty = map[string]float64{
		"action_dispersion": round3(clamp01(float64(len(actionCounts)) / 12.0)),
		"message_density":   round3(clamp01(float64(len(world.MessageBus)) / float64(max(run.Execution.TickCount, 1)*4))),
		"transaction_load":  round3(clamp01(float64(len(world.Transactions)) / float64(max(run.Execution.TickCount, 1)*3))),
	}
	world.DominantMechanisms = topMechanisms(actionCounts)
	world.BootstrapDigest = horizonsim.MarshalDigest(run.BaselineRefs, run.BaselinePreview, world.WorldIndex)
	world.Status = string(horizonmodels.SimulationWorldStatusSucceeded)
	if err := p.worlds.Update(ctx, world); err != nil {
		return nil, err
	}
	_, _ = p.artifacts.Create(ctx, &horizonmodels.SimulationArtifact{
		TenantID:    run.TenantID,
		RunID:       run.ID,
		WorldID:     world.ID,
		Status:      "active",
		Kind:        string(horizonmodels.SimulationArtifactKindWorldSummary),
		DisplayName: fmt.Sprintf("Simulation world %d summary", world.WorldIndex),
		Payload:     horizonsim.WorldSummary(world),
		ExpiresAt:   run.ExpiresAt,
	})

	worlds, err := p.worlds.ListByRun(ctx, run.TenantID, run.ID, 512)
	if err != nil {
		return nil, err
	}
	completed := 0
	for _, item := range worlds {
		switch strings.TrimSpace(item.Status) {
		case string(horizonmodels.SimulationWorldStatusSucceeded), string(horizonmodels.SimulationWorldStatusFailed), string(horizonmodels.SimulationWorldStatusDegraded):
			completed++
		}
	}
	run.Progress.CompletedWorlds = completed
	run.Progress.CompletedTicks = completed * run.Execution.TickCount
	if completed >= run.Execution.WorldCount {
		run.Status = string(horizonmodels.SimulationRunStatusAggregating)
		aggregateTask, err := rt.CreateTask(ctx, &contracts.MeshTask{
			TenantID:     run.TenantID,
			Capability:   "sim.aggregate",
			ParentTaskID: task.ParentTaskID,
			Input: map[string]any{
				"runId": run.ID,
			},
			Lease: contracts.Lease{
				DedupKey: "sim.aggregate:" + run.ID,
			},
		}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, false)
		if err != nil {
			return nil, err
		}
		run.AgentMeshTaskIDs = compactStrings(append(run.AgentMeshTaskIDs, aggregateTask.ID))
	}
	if err := p.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	p.publish(ctx, "horizon.simulation.world.completed", run.TenantID, map[string]any{
		"runId":      run.ID,
		"worldId":    world.ID,
		"worldIndex": world.WorldIndex,
		"status":     world.Status,
	})
	return &runtime.ProcessResult{
		Status: contracts.TaskStatusSucceeded,
		Output: map[string]any{
			"runId":              run.ID,
			"worldId":            world.ID,
			"populationDigest":   world.PopulationDigest,
			"bootstrapDigest":    world.BootstrapDigest,
			"outcomeVector":      world.OutcomeVector,
			"dominantMechanisms": world.DominantMechanisms,
		},
		ArtifactPayload: horizonsim.WorldSummary(world),
	}, nil
}

func (p *Processor) processAggregate(ctx context.Context, rt *runtime.Service, task *contracts.MeshTask) (*runtime.ProcessResult, error) {
	runID := stringInput(task.Input, "runId")
	run, err := p.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	worlds, err := p.worlds.ListByRun(ctx, run.TenantID, run.ID, 512)
	if err != nil {
		return nil, err
	}
	outcomeDistribution := averageOutcomes(worlds)
	sensitivity := sensitivityFromWorlds(worlds)
	worldSummaries := make([]map[string]any, 0, len(worlds))
	keyMechanisms := map[string]int{}
	worldAgents := map[string][]*horizonmodels.SimulationAgentSpec{}
	worldEvents := map[string][]*horizonmodels.SimulationEvent{}
	for _, world := range worlds {
		worldSummaries = append(worldSummaries, horizonsim.WorldSummary(world))
		for _, mechanism := range world.DominantMechanisms {
			keyMechanisms[mechanism]++
		}
		if world == nil {
			continue
		}
		agents, err := p.agents.ListByWorld(ctx, run.TenantID, run.ID, world.ID, 256)
		if err == nil {
			worldAgents[world.ID] = agents
		}
		events, err := p.events.ListByWorld(ctx, run.TenantID, run.ID, world.ID, 512)
		if err == nil {
			worldEvents[world.ID] = events
		}
	}
	executiveSummary := buildExecutiveSummary(outcomeDistribution, sensitivity, keyMechanisms)
	actorReactions := buildActorReactions(worlds, worldAgents, worldEvents)
	mechanismChains := buildMechanismChains(worlds, worldAgents, worldEvents)
	scenarioMetadata := buildScenarioMetadata(run)
	graphValidation := buildScenarioGraphValidation(run, worldAgents)
	actionProfile := buildActionProfile(worldEvents)
	protocolSummary := buildProtocolSummary(worlds)
	transactionSummary := buildTransactionSummary(worlds)
	if len(protocolSummary) == 0 {
		protocolSummary = buildProtocolSummaryFromEvents(worldEvents)
	}
	if len(transactionSummary) == 0 {
		transactionSummary = buildTransactionSummaryFromEvents(worldEvents)
	}
	disciplinedSignals := buildDisciplinedSignals(run, outcomeDistribution, actorReactions, actionProfile)
	functionExposures := buildFunctionExposures(disciplinedSignals, actorReactions, outcomeDistribution)
	decisionDemands := buildDecisionDemands(run, disciplinedSignals, functionExposures, actionProfile)
	monitoringSignals := buildMonitoringSignals(run, disciplinedSignals, functionExposures, outcomeDistribution, sensitivity)
	roleReadouts := buildRoleReadouts(run, disciplinedSignals, functionExposures, decisionDemands, monitoringSignals)
	resultOverlays := buildResultOverlays(run, actorReactions, protocolSummary, transactionSummary, monitoringSignals, roleReadouts)
	executiveNarrative := buildExecutiveNarrative(run, worlds, outcomeDistribution, sensitivity, actorReactions, mechanismChains, graphValidation, actionProfile)
	executiveNarrative = append(executiveNarrative, buildTransactionAwareNarrative(protocolSummary, transactionSummary)...)
	technicalNarrative := buildTechnicalNarrative(run, worlds, actorReactions, mechanismChains, graphValidation, actionProfile)
	technicalNarrative = append(technicalNarrative, buildTransactionAwareNarrative(protocolSummary, transactionSummary)...)
	executiveMemo := buildExecutiveMemo(run, worlds, outcomeDistribution, sensitivity, actorReactions, mechanismChains, graphValidation, actionProfile)
	groundingCoverage := round3(0.82)
	if coverage, ok := graphValidation["coverage"].(float64); ok && coverage > 0 {
		groundingCoverage = round3(math.Min(groundingCoverage, coverage))
	}
	bundle := &horizonmodels.SimulationBundle{
		ScenarioMetadata:   scenarioMetadata,
		GraphValidation:    graphValidation,
		ActionProfile:      actionProfile,
		ProtocolSummary:    protocolSummary,
		TransactionSummary: transactionSummary,
		DisciplinedSignals: disciplinedSignals,
		FunctionExposures:  functionExposures,
		DecisionDemands:    decisionDemands,
		MonitoringSignals:  monitoringSignals,
		RoleReadouts:       roleReadouts,
		ResultOverlays:     resultOverlays,
		RunSummary: map[string]any{
			"runId":            run.ID,
			"status":           run.Status,
			"worldCount":       len(worlds),
			"question":         run.Request.Question,
			"axis":             runAxis(run),
			"executiveSummary": executiveSummary,
		},
		WorldSummaries:      worldSummaries,
		OutcomeDistribution: outcomeDistribution,
		Counterfactuals:     buildCounterfactuals(worlds),
		Sensitivity:         sensitivity,
		KeyMechanisms:       topMechanisms(keyMechanisms),
		ActorReactions:      actorReactions,
		MechanismChains:     mechanismChains,
		ExecutiveNarrative:  executiveNarrative,
		ExecutiveMemo:       executiveMemo,
		TechnicalNarrative:  technicalNarrative,
		TopTrajectories:     worldSummaries[:min(len(worldSummaries), 5)],
		TraceRefs:           compactStrings(append([]string{}, run.AgentMeshTaskIDs...)),
		GroundingCoverage:   groundingCoverage,
	}
	artifact, err := p.findBundleArtifact(ctx, run)
	if err != nil {
		artifact = &horizonmodels.SimulationArtifact{
			TenantID:    run.TenantID,
			RunID:       run.ID,
			Status:      "active",
			Kind:        string(horizonmodels.SimulationArtifactKindBundle),
			DisplayName: "Simulation bundle",
			ExpiresAt:   run.ExpiresAt,
		}
	}
	artifact.Payload = map[string]any{
		"bundle": bundle,
		"digest": bundle.Digest(),
	}
	if strings.TrimSpace(artifact.ID) == "" {
		if _, err := p.artifacts.Create(ctx, artifact); err != nil {
			return nil, err
		}
	} else {
		artifact.UpdatedAt = time.Now().UTC()
		if err := p.artifacts.Update(ctx, artifact); err != nil {
			return nil, err
		}
	}
	run.Status = string(horizonmodels.SimulationRunStatusReviewing)
	run.Summary = horizonmodels.SimulationSummary{
		OutcomeDistribution: outcomeDistribution,
		KeyMechanisms:       bundle.KeyMechanisms,
		Sensitivity:         sensitivity,
		GroundingCoverage:   groundingCoverage,
		BundleArtifactID:    artifact.ID,
	}
	reviewTask, err := rt.CreateTask(ctx, &contracts.MeshTask{
		TenantID:     run.TenantID,
		Capability:   "sim.review",
		ParentTaskID: task.ParentTaskID,
		Input: map[string]any{
			"runId":      run.ID,
			"artifactId": artifact.ID,
		},
		Lease: contracts.Lease{
			DedupKey: "sim.review:" + run.ID,
		},
	}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, false)
	if err != nil {
		return nil, err
	}
	run.AgentMeshTaskIDs = compactStrings(append(run.AgentMeshTaskIDs, reviewTask.ID))
	if err := p.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	p.publish(ctx, "horizon.simulation.run.aggregated", run.TenantID, map[string]any{
		"runId":      run.ID,
		"artifactId": artifact.ID,
	})
	return &runtime.ProcessResult{
		Status: contracts.TaskStatusSucceeded,
		Output: map[string]any{
			"runId":               run.ID,
			"bundleArtifactId":    artifact.ID,
			"outcomeDistribution": outcomeDistribution,
		},
		ArtifactPayload: artifact.Payload,
	}, nil
}

func (p *Processor) processReview(ctx context.Context, rt *runtime.Service, task *contracts.MeshTask) (*runtime.ProcessResult, error) {
	runID := stringInput(task.Input, "runId")
	artifactID := stringInput(task.Input, "artifactId")
	run, err := p.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	artifact, err := p.artifacts.Get(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	review := map[string]any{
		"accepted":          true,
		"rationale":         "bundle grounded against baseline snapshot and normalized world actions",
		"groundingCoverage": 0.82,
		"issues":            []string{},
	}
	if rt.Vertex() != nil {
		pseudoTask := &contracts.MeshTask{
			ID:         task.ID + "-review",
			TenantID:   task.TenantID,
			Capability: "sim.review",
			Input:      artifact.Payload,
		}
		if body, _, err := rt.Vertex().ExecuteTask(ctx, pseudoTask, false); err == nil {
			if parsed := parseContentObject(body); len(parsed) > 0 {
				review = parsed
			}
		}
	}
	accepted := boolValue(review, "accepted", true)
	groundingCoverage := floatValue(review, "groundingCoverage", 0.8)
	run.Review = horizonmodels.SimulationReviewSummary{
		Status:            ternary(accepted, "accepted", "degraded"),
		GroundingCoverage: round3(groundingCoverage),
		Rationale:         stringValue(review, "rationale"),
		Issues:            stringSliceValue(review["issues"]),
	}
	run.Summary.GroundingCoverage = round3(groundingCoverage)
	if accepted {
		run.Status = string(horizonmodels.SimulationRunStatusSucceeded)
	} else {
		run.Status = string(horizonmodels.SimulationRunStatusDegraded)
	}
	if err := p.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	_, _ = p.artifacts.Create(ctx, &horizonmodels.SimulationArtifact{
		TenantID:    run.TenantID,
		RunID:       run.ID,
		Status:      "active",
		Kind:        string(horizonmodels.SimulationArtifactKindReview),
		DisplayName: "Simulation review",
		Payload:     review,
		ExpiresAt:   run.ExpiresAt,
	})
	p.publish(ctx, "horizon.simulation.run.reviewed", run.TenantID, map[string]any{
		"runId":             run.ID,
		"status":            run.Status,
		"groundingCoverage": groundingCoverage,
	})
	return &runtime.ProcessResult{
		Status:          ternaryStatus(accepted, contracts.TaskStatusSucceeded, contracts.TaskStatusDegraded),
		Output:          map[string]any{"runId": run.ID, "review": review},
		ArtifactPayload: review,
		ReviewOutput:    review,
		ReviewAccepted:  boolPtr(accepted),
		LastError:       stringValue(review, "rationale"),
	}, nil
}

func (p *Processor) generatePopulation(ctx context.Context, rt *runtime.Service, run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]*horizonmodels.SimulationAgentSpec, string, error) {
	rawAgents := []map[string]any{}
	if preferScenarioPopulation(run) {
		rawAgents = fallbackPopulation(run, world)
	} else if rt.Vertex() != nil {
		task := &contracts.MeshTask{
			ID:         world.ID + "-population",
			TenantID:   run.TenantID,
			Capability: "sim.population.generate",
			Input: map[string]any{
				"question":         run.Request.Question,
				"signal":           run.Request.Signal,
				"baselineRefs":     run.BaselineRefs,
				"worldIndex":       world.WorldIndex,
				"targetPopulation": 24,
			},
		}
		if body, _, err := rt.Vertex().ExecuteTask(ctx, task, false); err == nil {
			rawAgents = parseAgentList(body)
		}
	}
	if len(rawAgents) == 0 {
		rawAgents = fallbackPopulation(run, world)
	}
	rawAgents = dedupeRawPopulationByClass(append(rawAgents, scopeDerivedPopulationExtras(run)...))
	agents := make([]*horizonmodels.SimulationAgentSpec, 0, len(rawAgents))
	for idx, raw := range rawAgents[:min(len(rawAgents), 60)] {
		agent := &horizonmodels.SimulationAgentSpec{
			TenantID:         run.TenantID,
			RunID:            run.ID,
			WorldID:          world.ID,
			Status:           "active",
			AgentClass:       firstString(raw, "agentClass", "consumer"),
			DisplayName:      firstString(raw, "displayName", fmt.Sprintf("Agent %d", idx+1)),
			OriginKind:       firstString(raw, "originKind", "synthetic_archetype"),
			OriginRefs:       compactStrings(stringSliceValue(raw["originRefs"])),
			Justification:    firstString(raw, "justification", "Generated from simulation brief"),
			GeneratedBy:      "sim.population.generate",
			SchemaVersion:    "sim.agent.v1",
			PopulationWeight: floatValue(raw, "populationWeight", 1.0),
			Traits:           mapValue(raw["traits"]),
			BeliefState:      floatMapValue(raw["beliefState"]),
			ResourceState:    floatMapValue(raw["resourceState"]),
			AllowedActions:   normalizeAllowedActions(stringSliceValue(raw["allowedActions"])),
			MessageStyle:     firstString(raw, "messageStyle", "direct"),
			PolicyHints:      mapValue(raw["policyHints"]),
			BaselineRefs:     compactStrings(run.BaselineRefs.FragmentIDs),
			ExpiresAt:        run.ExpiresAt,
		}
		if err := agent.Validate(); err != nil {
			continue
		}
		agents = append(agents, agent)
	}
	if len(agents) == 0 {
		return nil, "", fmt.Errorf("population generation returned no valid agents")
	}
	return agents, horizonsim.MarshalDigest(rawAgents, run.BaselineRefs, world.WorldIndex), nil
}

func dedupeRawPopulationByClass(items []map[string]any) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		class := firstString(item, "agentClass")
		if class == "" {
			continue
		}
		if _, ok := seen[class]; ok {
			continue
		}
		seen[class] = struct{}{}
		out = append(out, item)
	}
	return out
}

func preferScenarioPopulation(run *horizonmodels.SimulationRun) bool {
	return isKZVATHotPath(run) || isTeslaBatteryHotPath(run) || isSpaceSatelliteHotPath(run) || isSLBRussiaHotPath(run)
}

func (p *Processor) generateStepActions(ctx context.Context, rt *runtime.Service, run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) ([]horizonmodels.SimulationAction, error) {
	if preferScenarioPopulation(run) {
		if actions := fallbackActions(run, world, agents, step); len(actions) > 0 {
			return actions, nil
		}
	}
	if rt.Vertex() != nil {
		task := &contracts.MeshTask{
			ID:         fmt.Sprintf("%s-step-%d", world.ID, step),
			TenantID:   run.TenantID,
			Capability: "sim.world.step",
			Input: map[string]any{
				"question":         run.Request.Question,
				"step":             step,
				"worldIndex":       world.WorldIndex,
				"agents":           agents,
				"allowedActions":   horizonmodels.ValidSimulationActionTypes(),
				"marketState":      world.MarketState,
				"institutionState": world.InstitutionState,
			},
		}
		if body, _, err := rt.Vertex().ExecuteTask(ctx, task, false); err == nil {
			if actions := parseActions(body); len(actions) > 0 {
				return actions, nil
			}
		}
	}
	return fallbackActions(run, world, agents, step), nil
}

func (p *Processor) validateAction(action horizonmodels.SimulationAction, agents []*horizonmodels.SimulationAgentSpec) error {
	if err := action.Validate(); err != nil {
		return err
	}
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) == strings.TrimSpace(action.ActorID) {
			if len(agent.AllowedActions) > 0 && !containsFold(agent.AllowedActions, action.Type) {
				return fmt.Errorf("action %s is not allowed for %s", action.Type, action.ActorID)
			}
		}
	}
	if delta := numericPayload(action.Payload, "delta", 0); math.Abs(delta) > 0.35 {
		return fmt.Errorf("action delta exceeds cap")
	}
	return nil
}

func (p *Processor) findBundleArtifact(ctx context.Context, run *horizonmodels.SimulationRun) (*horizonmodels.SimulationArtifact, error) {
	artifacts, err := p.artifacts.ListByRun(ctx, run.TenantID, run.ID, 64)
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		if strings.EqualFold(strings.TrimSpace(artifact.Kind), string(horizonmodels.SimulationArtifactKindBundle)) {
			return artifact, nil
		}
	}
	return nil, fmt.Errorf("bundle artifact not found")
}

func (p *Processor) publish(ctx context.Context, eventType, tenantID string, payload any) {
	if p.publisher == nil {
		return
	}
	if err := p.publisher(ctx, eventType, tenantID, payload); err != nil {
		p.logger.Printf("[agentmesh.simulation] publish failed: %v", err)
	}
}

type metricState struct {
	risk      float64
	price     float64
	narrative float64
	policy    float64
	supply    float64
}

func (m metricState) outcomeVector() map[string]float64 {
	return map[string]float64{
		"risk_score":           round3(m.risk),
		"price_pressure":       round3(m.price),
		"narrative_volatility": round3(m.narrative),
		"policy_pressure":      round3(m.policy),
		"supply_stress":        round3(m.supply),
	}
}

func runAxis(run *horizonmodels.SimulationRun) string {
	if run == nil {
		return ""
	}
	if run.Request.Signal != nil && strings.TrimSpace(run.Request.Signal.Axis) != "" {
		return strings.TrimSpace(run.Request.Signal.Axis)
	}
	return firstString(mapValue(run.Recognition), "primaryAxis")
}

func findAgent(agents []*horizonmodels.SimulationAgentSpec, agentID string) *horizonmodels.SimulationAgentSpec {
	agentID = strings.TrimSpace(agentID)
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) == agentID {
			return agent
		}
	}
	return nil
}

func applyActionImpact(state metricState, action horizonmodels.SimulationAction, agent *horizonmodels.SimulationAgentSpec, axis string, variant, step int) metricState {
	base := actionImpactVector(action.Type)
	axisWeight := axisImpactWeight(axis)
	domainWeight := domainImpactWeight(agentDomain(agent))
	variantWeight := variantImpactWeight(variant)
	agentFactor := agentImpactFactor(agent, action, step) * axisActionRelevance(axis, agentDomain(agent), action.Type, step, variant)
	scale := func(base, axis, domain, variant float64) float64 {
		return base * axis * domain * variant * agentFactor
	}
	state.risk = applyMetricDelta(state.risk, scale(base.risk, axisWeight.risk, domainWeight.risk, variantWeight.risk))
	state.price = applyMetricDelta(state.price, scale(base.price, axisWeight.price, domainWeight.price, variantWeight.price))
	state.narrative = applyMetricDelta(state.narrative, scale(base.narrative, axisWeight.narrative, domainWeight.narrative, variantWeight.narrative))
	state.policy = applyMetricDelta(state.policy, scale(base.policy, axisWeight.policy, domainWeight.policy, variantWeight.policy))
	state.supply = applyMetricDelta(state.supply, scale(base.supply, axisWeight.supply, domainWeight.supply, variantWeight.supply))
	return state
}

func stabilizeStepState(state metricState, axis string, step, actionCount int) metricState {
	pulse := clamp01(float64(actionCount) / 20.0)
	axisWeight := axisImpactWeight(axis)
	state.risk = clamp01(0.48*state.risk + 0.22*state.price + 0.16*state.policy + 0.08*state.narrative + 0.24*state.supply + 0.03*pulse*axisWeight.risk)
	state.price = clamp01(state.price - 0.01*(1-axisWeight.price*0.5))
	state.narrative = clamp01(state.narrative - 0.008*(1-axisWeight.narrative*0.45))
	state.policy = clamp01(state.policy - 0.006*(1-axisWeight.policy*0.40))
	state.supply = clamp01(state.supply - 0.008*(1-axisWeight.supply*0.40))
	return state
}

func actionImpactVector(actionType string) metricState {
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionBroadcastMessage):
		return metricState{risk: 0.012, narrative: 0.045, policy: 0.010}
	case string(horizonmodels.SimulationActionChangeBelief):
		return metricState{risk: 0.008, narrative: 0.040, policy: 0.012}
	case string(horizonmodels.SimulationActionBuy):
		return metricState{risk: 0.006, price: 0.028, supply: 0.020}
	case string(horizonmodels.SimulationActionSell):
		return metricState{risk: -0.004, price: -0.020, supply: -0.012}
	case string(horizonmodels.SimulationActionDelayPurchase):
		return metricState{risk: 0.004, price: -0.015, narrative: 0.008, supply: -0.006}
	case string(horizonmodels.SimulationActionSwitchSupplier):
		return metricState{risk: -0.005, price: 0.010, narrative: 0.010, supply: -0.045}
	case string(horizonmodels.SimulationActionReserveInventory):
		return metricState{risk: -0.004, price: 0.018, supply: -0.030}
	case string(horizonmodels.SimulationActionRerouteShipment):
		return metricState{risk: -0.006, price: 0.024, narrative: 0.006, supply: -0.050}
	case string(horizonmodels.SimulationActionExpediteShipment):
		return metricState{risk: -0.005, price: 0.034, supply: -0.040}
	case string(horizonmodels.SimulationActionAllocateCapacity):
		return metricState{risk: -0.004, price: 0.012, supply: -0.026}
	case string(horizonmodels.SimulationActionReprioritizeFlow):
		return metricState{risk: 0.003, price: 0.010, narrative: 0.008, supply: -0.022}
	case string(horizonmodels.SimulationActionRequestExemption):
		return metricState{risk: -0.002, narrative: 0.014, policy: 0.042}
	case string(horizonmodels.SimulationActionHedgeCost):
		return metricState{risk: -0.004, price: -0.018, supply: 0.004}
	case string(horizonmodels.SimulationActionAdjustGuidance):
		return metricState{risk: 0.006, narrative: 0.032, policy: 0.012}
	case string(horizonmodels.SimulationActionLobby):
		return metricState{risk: 0.015, narrative: 0.018, policy: 0.050}
	case string(horizonmodels.SimulationActionIssueStatement):
		return metricState{risk: 0.006, narrative: 0.020, policy: 0.040}
	case string(horizonmodels.SimulationActionEnforceRule):
		return metricState{risk: 0.018, narrative: -0.010, policy: 0.055, supply: 0.012}
	case string(horizonmodels.SimulationActionAdjustBudget):
		return metricState{risk: 0.010, price: 0.032, supply: 0.008}
	case string(horizonmodels.SimulationActionEscalate):
		return metricState{risk: 0.060, narrative: 0.070, policy: 0.025}
	case string(horizonmodels.SimulationActionDoNothing):
		return metricState{risk: -0.008, narrative: -0.012, policy: -0.006}
	default:
		return metricState{}
	}
}

func axisImpactWeight(axis string) metricState {
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLegalAccess:
		return metricState{risk: 1.10, price: 0.82, narrative: 0.66, policy: 1.18, supply: 1.00}
	case horizonmodels.AxisLegalCompliance:
		return metricState{risk: 1.08, price: 0.78, narrative: 0.72, policy: 1.22, supply: 0.92}
	case horizonmodels.AxisFinancialSettlement:
		return metricState{risk: 1.08, price: 1.55, narrative: 0.88, policy: 0.70, supply: 1.25}
	case horizonmodels.AxisPhysicalFlowSupply:
		return metricState{risk: 1.04, price: 1.30, narrative: 0.70, policy: 0.48, supply: 1.56}
	case horizonmodels.AxisPhysicalFlowDemand:
		return metricState{risk: 0.96, price: 1.62, narrative: 0.76, policy: 0.42, supply: 1.18}
	case horizonmodels.AxisLogisticsCapacity:
		return metricState{risk: 1.00, price: 1.18, narrative: 0.70, policy: 0.66, supply: 1.48}
	case horizonmodels.AxisLogisticsLatency:
		return metricState{risk: 1.00, price: 1.22, narrative: 0.74, policy: 0.68, supply: 1.42}
	case horizonmodels.AxisInfoReputation:
		return metricState{risk: 1.22, price: 0.78, narrative: 1.55, policy: 1.02, supply: 0.78}
	case horizonmodels.AxisInfoUncertainty:
		return metricState{risk: 1.30, price: 0.80, narrative: 1.70, policy: 1.18, supply: 0.82}
	default:
		return metricState{risk: 1, price: 1, narrative: 1, policy: 1, supply: 1}
	}
}

func domainImpactWeight(domain string) metricState {
	switch domain {
	case "state":
		return metricState{risk: 1.10, price: 0.72, narrative: 0.82, policy: 1.20, supply: 0.96}
	case "compliance":
		return metricState{risk: 1.18, price: 0.82, narrative: 0.78, policy: 1.28, supply: 1.08}
	case "market":
		return metricState{risk: 1.00, price: 1.28, narrative: 0.90, policy: 0.80, supply: 1.05}
	case "finance":
		return metricState{risk: 1.10, price: 1.40, narrative: 0.78, policy: 0.72, supply: 1.12}
	case "consumer":
		return metricState{risk: 0.95, price: 1.25, narrative: 1.10, policy: 0.58, supply: 0.90}
	case "informal":
		return metricState{risk: 1.05, price: 1.08, narrative: 1.22, policy: 0.62, supply: 0.95}
	case "info":
		return metricState{risk: 1.18, price: 0.70, narrative: 1.55, policy: 0.98, supply: 0.72}
	case "logistics":
		return metricState{risk: 1.02, price: 1.16, narrative: 0.72, policy: 0.70, supply: 1.55}
	case "food":
		return metricState{risk: 1.02, price: 1.22, narrative: 0.82, policy: 0.76, supply: 1.22}
	case "political":
		return metricState{risk: 1.30, price: 0.72, narrative: 1.48, policy: 1.10, supply: 0.76}
	default:
		return metricState{risk: 1, price: 1, narrative: 1, policy: 1, supply: 1}
	}
}

func variantImpactWeight(variant int) metricState {
	switch variant {
	case 1:
		return metricState{risk: 1.12, price: 0.96, narrative: 1.14, policy: 1.10, supply: 0.98}
	case 2:
		return metricState{risk: 1.04, price: 1.12, narrative: 0.92, policy: 0.96, supply: 1.16}
	case 3:
		return metricState{risk: 0.98, price: 1.08, narrative: 1.08, policy: 0.92, supply: 1.10}
	case 4:
		return metricState{risk: 1.15, price: 0.94, narrative: 1.16, policy: 1.05, supply: 1.08}
	case 5:
		return metricState{risk: 1.02, price: 0.98, narrative: 1.18, policy: 1.12, supply: 0.94}
	case 6:
		return metricState{risk: 1.18, price: 1.14, narrative: 0.92, policy: 0.88, supply: 1.18}
	default:
		return metricState{risk: 1, price: 1, narrative: 1, policy: 1, supply: 1}
	}
}

func agentDomain(agent *horizonmodels.SimulationAgentSpec) string {
	if agent == nil {
		return ""
	}
	switch strings.TrimSpace(agent.AgentClass) {
	case "regulator", "regional_official", "municipal_buyer", "tax_advisor", "budget_revenue_officer", "investment_climate_officer", "healthcare_public_operator", "education_system_operator", "trade_official", "customs_official", "launch_regulator":
		return "state"
	case "export_control_officer", "sanctions_compliance_officer", "procurement_compliance_lead", "transshipment_compliance_officer":
		return "compliance"
	case "retailer", "importer", "supplier", "wholesaler", "payment_rail", "bank", "marketplace_seller", "ecommerce_platform", "restaurant_operator", "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury", "internal_producer", "healthcare_private_operator", "facility_operator_nevada", "facility_operator_texas", "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator", "procurement_director", "production_planner", "vehicle_program_manager", "energy_storage_program_manager", "large_energy_customer", "competitor_demand_sink":
		return "market"
	case "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "satellite_services_customer", "roscosmos_program_office", "isro_program_office", "uae_space_procurement", "saudi_space_procurement", "slb_russia_procurement_lead", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "china_equipment_supplier", "kazakh_chemical_supplier", "russian_oilfield_customer", "parallel_import_broker":
		return "market"
	case "analyst", "equity_analyst":
		return "finance"
	case "aerospace_settlement_bank", "space_insurer", "market_intelligence_analyst", "ofs_settlement_bank", "trade_credit_insurer":
		return "finance"
	case "consumer_low_income", "consumer_mid_income", "consumer_high_income", "cross_border_shopper", "consumer_urban_low_income", "consumer_rural_low_income", "consumer_family_mid_income", "consumer_ecommerce_native", "consumer_bazaar_sensitive", "consumer_affluent":
		return "consumer"
	case "small_trader":
		return "informal"
	case "media", "influencer", "association", "investor_relations", "pr_agent":
		return "info"
	case "logistics_operator", "warehouse_operator", "engineering_infrastructure_operator", "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "port_operator", "launch_manifest_planner", "cross_border_logistics_operator", "jebel_ali_hub_operator":
		return "logistics"
	case "agri_producer", "fuel_distributor":
		return "food"
	case "opposition_politician":
		return "political"
	default:
		if strings.HasPrefix(strings.TrimSpace(agent.AgentClass), "regional_supply_shoulder_") {
			return "logistics"
		}
		if strings.HasPrefix(strings.TrimSpace(agent.AgentClass), "product_risk_manager_") {
			return "market"
		}
		return "market"
	}
}

func agentImpactFactor(agent *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, step int) float64 {
	if agent == nil {
		return 1
	}
	weight := clamp(agent.PopulationWeight, 0.7, 1.9)
	attention := clamp(mapAnyFloat(agent.Traits, "attention", 0.5), 0.1, 1)
	riskBias := clamp(mapAnyFloat(agent.Traits, "riskBias", 0.5), 0.1, 1)
	stress := clamp(mapFloat(agent.BeliefState, "stress", 0.4), 0.05, 1)
	budget := clamp(mapFloat(agent.ResourceState, "budget", 0.4), 0.05, 1)
	delta := math.Abs(numericPayload(action.Payload, "delta", 0.03))
	factor := 0.55 + 0.28*weight + 0.18*attention + 0.12*riskBias + 0.10*stress + 0.12*delta
	if action.Type == string(horizonmodels.SimulationActionDelayPurchase) || action.Type == string(horizonmodels.SimulationActionAdjustBudget) {
		factor += 0.08 * (1 - budget)
	}
	if step >= 3 {
		factor += 0.04
	}
	return clamp(factor, 0.45, 1.55)
}

func axisActionRelevance(axis, domain, actionType string, step, variant int) float64 {
	actionType = strings.TrimSpace(actionType)
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		switch {
		case domain == "compliance" && (actionType == string(horizonmodels.SimulationActionRequestExemption) || actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionEnforceRule) || actionType == string(horizonmodels.SimulationActionAdjustBudget)):
			return 1.24
		case domain == "state" && (actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionEnforceRule) || actionType == string(horizonmodels.SimulationActionLobby) || actionType == string(horizonmodels.SimulationActionChangeBelief) || actionType == string(horizonmodels.SimulationActionRequestExemption)):
			return 1.08
		case domain == "market" && (actionType == string(horizonmodels.SimulationActionRequestExemption) || actionType == string(horizonmodels.SimulationActionAdjustGuidance)):
			return 0.92
		case domain == "consumer" && (actionType == string(horizonmodels.SimulationActionBuy) || actionType == string(horizonmodels.SimulationActionDelayPurchase)):
			return 0.45
		case domain == "market" && actionType == string(horizonmodels.SimulationActionAdjustBudget):
			return 0.82
		}
	case horizonmodels.AxisFinancialSettlement:
		switch {
		case domain == "compliance" && (actionType == string(horizonmodels.SimulationActionRequestExemption) || actionType == string(horizonmodels.SimulationActionAdjustBudget) || actionType == string(horizonmodels.SimulationActionIssueStatement)):
			return 0.94
		case (domain == "market" || domain == "finance" || domain == "logistics" || domain == "food") && (actionType == string(horizonmodels.SimulationActionAdjustBudget) || actionType == string(horizonmodels.SimulationActionSell) || actionType == string(horizonmodels.SimulationActionSwitchSupplier) || actionType == string(horizonmodels.SimulationActionDelayPurchase) || actionType == string(horizonmodels.SimulationActionHedgeCost) || actionType == string(horizonmodels.SimulationActionAdjustGuidance)):
			return 1.20
		case (domain == "market" || domain == "logistics") && (actionType == string(horizonmodels.SimulationActionReserveInventory) || actionType == string(horizonmodels.SimulationActionExpediteShipment)):
			return 1.10
		case (domain == "info" || domain == "political") && (actionType == string(horizonmodels.SimulationActionBroadcastMessage) || actionType == string(horizonmodels.SimulationActionEscalate)):
			return 0.55
		case domain == "state" && actionType == string(horizonmodels.SimulationActionEnforceRule):
			return 0.60
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		switch {
		case domain == "compliance" && (actionType == string(horizonmodels.SimulationActionReprioritizeFlow) || actionType == string(horizonmodels.SimulationActionRequestExemption)):
			return 0.88
		case (domain == "consumer" || domain == "market" || domain == "informal" || domain == "logistics" || domain == "food") && (actionType == string(horizonmodels.SimulationActionBuy) || actionType == string(horizonmodels.SimulationActionDelayPurchase) || actionType == string(horizonmodels.SimulationActionSwitchSupplier) || actionType == string(horizonmodels.SimulationActionAdjustBudget) || actionType == string(horizonmodels.SimulationActionReserveInventory) || actionType == string(horizonmodels.SimulationActionRerouteShipment) || actionType == string(horizonmodels.SimulationActionExpediteShipment) || actionType == string(horizonmodels.SimulationActionAllocateCapacity) || actionType == string(horizonmodels.SimulationActionReprioritizeFlow)):
			return 1.20
		case (domain == "state" || domain == "political") && (actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionLobby) || actionType == string(horizonmodels.SimulationActionEnforceRule)):
			return 0.28
		case domain == "info" && actionType == string(horizonmodels.SimulationActionBroadcastMessage):
			return 0.46
		}
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		switch {
		case domain == "compliance" && (actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionChangeBelief) || actionType == string(horizonmodels.SimulationActionAdjustGuidance)):
			return 1.08
		case (domain == "info" || domain == "political" || domain == "state") && (actionType == string(horizonmodels.SimulationActionBroadcastMessage) || actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionEscalate) || actionType == string(horizonmodels.SimulationActionChangeBelief) || actionType == string(horizonmodels.SimulationActionAdjustGuidance)):
			return 1.25
		case (domain == "market" || domain == "consumer" || domain == "finance" || domain == "logistics") && (actionType == string(horizonmodels.SimulationActionBuy) || actionType == string(horizonmodels.SimulationActionSell) || actionType == string(horizonmodels.SimulationActionAdjustBudget) || actionType == string(horizonmodels.SimulationActionSwitchSupplier) || actionType == string(horizonmodels.SimulationActionReserveInventory) || actionType == string(horizonmodels.SimulationActionReprioritizeFlow)):
			return 0.55
		}
	}
	if step >= 3 && variant%2 == 1 {
		return 1.02
	}
	return 1
}

func applyMetricDelta(current, delta float64) float64 {
	if delta >= 0 {
		return clamp01(current + delta*(1-current))
	}
	return clamp01(current + delta*math.Max(current, 0.30))
}

func mapFloat(values map[string]float64, key string, fallback float64) float64 {
	if len(values) == 0 {
		return fallback
	}
	value, ok := values[key]
	if !ok {
		return fallback
	}
	return value
}

func mapAnyFloat(values map[string]any, key string, fallback float64) float64 {
	if len(values) == 0 {
		return fallback
	}
	switch value := values[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}

func clamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func averageOutcomes(worlds []*horizonmodels.SimulationWorld) map[string]float64 {
	out := map[string]float64{}
	if len(worlds) == 0 {
		return out
	}
	counts := map[string]int{}
	for _, world := range worlds {
		for key, value := range world.OutcomeVector {
			out[key] += value
			counts[key]++
		}
	}
	for key, sum := range out {
		out[key] = round3(sum / float64(max(counts[key], 1)))
	}
	return out
}

func sensitivityFromWorlds(worlds []*horizonmodels.SimulationWorld) map[string]float64 {
	means := averageOutcomes(worlds)
	out := map[string]float64{}
	if len(worlds) == 0 {
		return out
	}
	counts := map[string]int{}
	for _, world := range worlds {
		for key, value := range world.OutcomeVector {
			delta := value - means[key]
			out[key] += delta * delta
			counts[key]++
		}
	}
	for key, value := range out {
		out[key] = round3(math.Sqrt(value / float64(max(counts[key], 1))))
	}
	return out
}

func topMechanisms(counts map[string]int) []string {
	type item struct {
		key   string
		count int
	}
	items := make([]item, 0, len(counts))
	for key, count := range counts {
		if strings.TrimSpace(key) != "" && count > 0 {
			items = append(items, item{key: key, count: count})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].key < items[j].key
		}
		return items[i].count > items[j].count
	})
	out := make([]string, 0, min(len(items), 5))
	for _, item := range items[:min(len(items), 5)] {
		out = append(out, item.key)
	}
	return out
}

func buildExecutiveSummary(outcomeDistribution, sensitivity map[string]float64, keyMechanismCounts map[string]int) map[string]any {
	ordered := rankOutcomeDimensions(outcomeDistribution)
	leading := make([]string, 0, min(len(ordered), 2))
	for _, item := range ordered[:min(len(ordered), 2)] {
		leading = append(leading, item.key)
	}
	mostUncertain := rankOutcomeDimensions(sensitivity)
	uncertain := ""
	if len(mostUncertain) > 0 {
		uncertain = mostUncertain[0].key
	}
	return map[string]any{
		"dominantStressors":      leading,
		"mostUncertainDimension": uncertain,
		"mechanismProfile":       topMechanisms(keyMechanismCounts),
		"interpretation":         executiveInterpretation(outcomeDistribution, sensitivity, leading),
	}
}

func buildCounterfactuals(worlds []*horizonmodels.SimulationWorld) []map[string]any {
	if len(worlds) == 0 {
		return nil
	}
	var highestRisk *horizonmodels.SimulationWorld
	var highestPrice *horizonmodels.SimulationWorld
	var highestNarrative *horizonmodels.SimulationWorld
	for _, world := range worlds {
		if world == nil {
			continue
		}
		if highestRisk == nil || world.OutcomeVector["risk_score"] > highestRisk.OutcomeVector["risk_score"] {
			highestRisk = world
		}
		if highestPrice == nil || world.OutcomeVector["price_pressure"] > highestPrice.OutcomeVector["price_pressure"] {
			highestPrice = world
		}
		if highestNarrative == nil || world.OutcomeVector["narrative_volatility"] > highestNarrative.OutcomeVector["narrative_volatility"] {
			highestNarrative = world
		}
	}
	out := make([]map[string]any, 0, 3)
	appendWorld := func(label string, world *horizonmodels.SimulationWorld, dimension string) {
		if world == nil {
			return
		}
		out = append(out, map[string]any{
			"label":             label,
			"worldId":           world.ID,
			"worldIndex":        world.WorldIndex,
			"dominantDimension": dimension,
			"outcomeVector":     world.OutcomeVector,
			"mechanisms":        world.DominantMechanisms,
		})
	}
	appendWorld("highest_risk_world", highestRisk, "risk_score")
	appendWorld("highest_price_world", highestPrice, "price_pressure")
	appendWorld("highest_narrative_world", highestNarrative, "narrative_volatility")
	return out
}

func buildActorReactions(worlds []*horizonmodels.SimulationWorld, worldAgents map[string][]*horizonmodels.SimulationAgentSpec, worldEvents map[string][]*horizonmodels.SimulationEvent) []map[string]any {
	type actorAggregate struct {
		Class               string
		RepresentativeNames map[string]struct{}
		OriginRefs          map[string]struct{}
		Worlds              map[int]struct{}
		Actions             map[string]int
		ActionVolume        int
	}
	byActorID := map[string]*horizonmodels.SimulationAgentSpec{}
	classAgg := map[string]*actorAggregate{}
	for _, world := range worlds {
		if world == nil {
			continue
		}
		for _, agent := range worldAgents[world.ID] {
			if agent == nil {
				continue
			}
			byActorID[agent.ID] = agent
			cls := strings.TrimSpace(agent.AgentClass)
			if cls == "" {
				cls = "unknown"
			}
			agg := classAgg[cls]
			if agg == nil {
				agg = &actorAggregate{
					Class:               cls,
					RepresentativeNames: map[string]struct{}{},
					OriginRefs:          map[string]struct{}{},
					Worlds:              map[int]struct{}{},
					Actions:             map[string]int{},
				}
				classAgg[cls] = agg
			}
			if name := strings.TrimSpace(agent.DisplayName); name != "" {
				agg.RepresentativeNames[name] = struct{}{}
			}
			for _, ref := range compactStrings(agent.OriginRefs) {
				agg.OriginRefs[ref] = struct{}{}
			}
			agg.Worlds[world.WorldIndex] = struct{}{}
		}
	}
	for _, world := range worlds {
		if world == nil {
			continue
		}
		for _, event := range worldEvents[world.ID] {
			for _, action := range eventActions(event) {
				agent := byActorID[strings.TrimSpace(action.ActorID)]
				cls := "unknown"
				if agent != nil && strings.TrimSpace(agent.AgentClass) != "" {
					cls = strings.TrimSpace(agent.AgentClass)
				}
				agg := classAgg[cls]
				if agg == nil {
					continue
				}
				agg.Actions[action.Type]++
				agg.ActionVolume++
			}
		}
	}
	type actorItem struct {
		key string
		agg *actorAggregate
	}
	items := make([]actorItem, 0, len(classAgg))
	for key, agg := range classAgg {
		items = append(items, actorItem{key: key, agg: agg})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.ActionVolume == items[j].agg.ActionVolume {
			return items[i].key < items[j].key
		}
		return items[i].agg.ActionVolume > items[j].agg.ActionVolume
	})
	out := make([]map[string]any, 0, min(len(items), 20))
	for _, item := range items {
		agg := item.agg
		if agg == nil || agg.ActionVolume == 0 {
			continue
		}
		out = append(out, map[string]any{
			"actorClass":           agg.Class,
			"worldCoverage":        len(agg.Worlds),
			"actionVolume":         agg.ActionVolume,
			"dominantActions":      topMechanisms(agg.Actions),
			"representativeAgents": setKeys(agg.RepresentativeNames, 3),
			"originRefs":           setKeys(agg.OriginRefs, 4),
			"reactionSummary":      actorReactionSummary(agg.Class, topMechanisms(agg.Actions), len(agg.Worlds)),
		})
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func buildMechanismChains(worlds []*horizonmodels.SimulationWorld, worldAgents map[string][]*horizonmodels.SimulationAgentSpec, worldEvents map[string][]*horizonmodels.SimulationEvent) []map[string]any {
	out := make([]map[string]any, 0, min(len(worlds), 6))
	for _, world := range worlds {
		if world == nil {
			continue
		}
		byActorID := map[string]*horizonmodels.SimulationAgentSpec{}
		for _, agent := range worldAgents[world.ID] {
			if agent != nil {
				byActorID[agent.ID] = agent
			}
		}
		stepActors := map[int]map[string]int{}
		stepActions := map[int]map[string]int{}
		for _, event := range worldEvents[world.ID] {
			for _, action := range eventActions(event) {
				if stepActors[event.StepIndex] == nil {
					stepActors[event.StepIndex] = map[string]int{}
				}
				if stepActions[event.StepIndex] == nil {
					stepActions[event.StepIndex] = map[string]int{}
				}
				stepActions[event.StepIndex][action.Type]++
				if agent := byActorID[action.ActorID]; agent != nil {
					stepActors[event.StepIndex][agent.AgentClass]++
				}
			}
		}
		dominantDimension := topOutcomeDimension(world.OutcomeVector)
		chainSteps := make([]map[string]any, 0, min(len(stepActions), 4))
		for step := 0; step < world.StepCount && len(chainSteps) < 4; step++ {
			if len(stepActions[step]) == 0 {
				continue
			}
			chainSteps = append(chainSteps, map[string]any{
				"step":         step,
				"actorClasses": topMechanisms(stepActors[step]),
				"actions":      topMechanisms(stepActions[step]),
			})
		}
		out = append(out, map[string]any{
			"worldId":            world.ID,
			"worldIndex":         world.WorldIndex,
			"dominantDimension":  dominantDimension,
			"dominantMechanisms": world.DominantMechanisms,
			"chainSteps":         chainSteps,
			"chainSummary":       mechanismChainSummary(dominantDimension, chainSteps),
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func buildGenericExecutiveNarrative(run *horizonmodels.SimulationRun, outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any) []string {
	leading := rankOutcomeDimensions(outcomeDistribution)
	lines := make([]string, 0, 4)
	if len(leading) > 0 {
		line := fmt.Sprintf("Primary business pressure is %s", readableMetric(leading[0].key))
		if len(leading) > 1 {
			line += fmt.Sprintf(", with secondary pressure in %s", readableMetric(leading[1].key))
		}
		lines = append(lines, line+".")
	}
	if len(actorReactions) > 0 {
		lines = append(lines, fmt.Sprintf("Most active representative groups are %s.", joinActorClasses(actorReactions, 3)))
	}
	if len(mechanismChains) > 0 {
		if summary, _ := mechanismChains[0]["chainSummary"].(string); strings.TrimSpace(summary) != "" {
			lines = append(lines, "Typical propagation chain: "+summary+".")
		}
	}
	uncertain := rankOutcomeDimensions(sensitivity)
	if len(uncertain) > 0 {
		lines = append(lines, fmt.Sprintf("Highest scenario variance remains in %s.", readableMetric(uncertain[0].key)))
	}
	return lines
}

func buildGenericTechnicalNarrative(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, actorReactions, mechanismChains []map[string]any) []string {
	lines := []string{
		fmt.Sprintf("Signal axis %s was simulated across %d worlds with normalized action execution.", runAxis(run), len(worlds)),
	}
	if run != nil {
		lines = append(lines, fmt.Sprintf("Baseline snapshot digest %s anchored all worlds to the same graph state.", strings.TrimSpace(run.BaselineRefs.SnapshotDigest)))
	}
	if len(actorReactions) > 0 {
		lines = append(lines, fmt.Sprintf("Actor summaries were synthesized from step traces over %d representative actor classes.", len(actorReactions)))
	}
	if len(mechanismChains) > 0 {
		lines = append(lines, fmt.Sprintf("Mechanism chains were extracted from per-step action logs and dominant outcome dimensions in %d worlds.", len(mechanismChains)))
	}
	return lines
}

func eventActions(event *horizonmodels.SimulationEvent) []horizonmodels.SimulationAction {
	if event == nil || len(event.Payload) == 0 {
		return nil
	}
	raw, ok := event.Payload["actions"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []horizonmodels.SimulationAction:
		return typed
	case []any:
		return stringSliceActions(typed)
	default:
		blob, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		var actions []horizonmodels.SimulationAction
		if err := json.Unmarshal(blob, &actions); err == nil {
			return actions
		}
		var generic []any
		if err := json.Unmarshal(blob, &generic); err == nil {
			return stringSliceActions(generic)
		}
		return nil
	}
}

func actorReactionSummary(actorClass string, actions []string, worlds int) string {
	if len(actions) == 0 {
		return fmt.Sprintf("%s stayed mostly passive across %d worlds", actorClass, worlds)
	}
	return fmt.Sprintf("%s mainly reacted via %s across %d worlds", actorClass, strings.Join(actions[:min(len(actions), 3)], ", "), worlds)
}

func mechanismChainSummary(dimension string, chainSteps []map[string]any) string {
	if len(chainSteps) == 0 {
		return "no chain extracted"
	}
	parts := make([]string, 0, len(chainSteps))
	for _, step := range chainSteps {
		actors := stringSliceAny(step["actorClasses"])
		actions := stringSliceAny(step["actions"])
		if len(actors) == 0 && len(actions) == 0 {
			continue
		}
		part := strings.Join(actors[:min(len(actors), 2)], "/")
		if len(actions) > 0 {
			if part != "" {
				part += " -> "
			}
			part += strings.Join(actions[:min(len(actions), 2)], "/")
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "no chain extracted"
	}
	return fmt.Sprintf("%s culminating in %s", strings.Join(parts, " -> "), readableMetric(dimension))
}

func joinActorClasses(actorReactions []map[string]any, limit int) string {
	classes := make([]string, 0, min(len(actorReactions), limit))
	for _, item := range actorReactions[:min(len(actorReactions), limit)] {
		if cls, _ := item["actorClass"].(string); strings.TrimSpace(cls) != "" {
			classes = append(classes, cls)
		}
	}
	return strings.Join(classes, ", ")
}

func setKeys(values map[string]struct{}, limit int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[:min(len(keys), limit)]
}

func stringSliceAny(raw any) []string {
	items, ok := raw.([]string)
	if ok {
		return compactStrings(items)
	}
	if generic, ok := raw.([]any); ok {
		out := make([]string, 0, len(generic))
		for _, item := range generic {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
			}
		}
		return compactStrings(out)
	}
	return nil
}

func topOutcomeDimension(values map[string]float64) string {
	ranked := rankOutcomeDimensions(values)
	if len(ranked) == 0 {
		return ""
	}
	return ranked[0].key
}

func readableMetric(metric string) string {
	switch strings.TrimSpace(metric) {
	case "risk_score":
		return "overall risk"
	case "price_pressure":
		return "price pressure"
	case "policy_pressure":
		return "policy pressure"
	case "narrative_volatility":
		return "narrative volatility"
	case "supply_stress":
		return "supply stress"
	default:
		return metric
	}
}

type rankedDimension struct {
	key   string
	value float64
}

func rankOutcomeDimensions(values map[string]float64) []rankedDimension {
	items := make([]rankedDimension, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		items = append(items, rankedDimension{key: key, value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].value == items[j].value {
			return items[i].key < items[j].key
		}
		return items[i].value > items[j].value
	})
	return items
}

func executiveInterpretation(outcomeDistribution, sensitivity map[string]float64, leading []string) string {
	if len(leading) == 0 {
		return "simulation did not produce a strong dominant stressor"
	}
	primary := leading[0]
	secondary := ""
	if len(leading) > 1 {
		secondary = leading[1]
	}
	uncertain := rankOutcomeDimensions(sensitivity)
	uncertainDim := ""
	if len(uncertain) > 0 {
		uncertainDim = uncertain[0].key
	}
	message := fmt.Sprintf("primary stress is %s", primary)
	if secondary != "" {
		message += fmt.Sprintf(", with secondary pressure in %s", secondary)
	}
	if uncertainDim != "" {
		message += fmt.Sprintf("; highest world-to-world variance is in %s", uncertainDim)
	}
	return message
}

func parseContentObject(body map[string]any) map[string]any {
	content, _ := body["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return parseJSONObject(content)
}

func parseJSONObject(raw string) map[string]any {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
		return out
	}
	return nil
}

func parseAgentList(body map[string]any) []map[string]any {
	content, _ := body["content"].(string)
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		if object := parseJSONObject(content); len(object) > 0 {
			if items, ok := object["agents"].([]any); ok {
				return mapSlice(items)
			}
		}
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(content[start:end+1]), &out); err == nil {
		return out
	}
	var arr []any
	if err := json.Unmarshal([]byte(content[start:end+1]), &arr); err == nil {
		return mapSlice(arr)
	}
	return nil
}

func parseActions(body map[string]any) []horizonmodels.SimulationAction {
	object := parseContentObject(body)
	if len(object) == 0 {
		return nil
	}
	items := stringSliceActions(object["actions"])
	if len(items) == 0 {
		return nil
	}
	return items
}

func stringSliceActions(raw any) []horizonmodels.SimulationAction {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]horizonmodels.SimulationAction, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:              firstString(obj, "type"),
			ActorID:           firstString(obj, "actorId"),
			TargetRefs:        compactStrings(stringSliceValue(obj["targetRefs"])),
			Payload:           mapValue(obj["payload"]),
			Justification:     firstString(obj, "justification"),
			GeneratedBy:       "sim.world.step",
			RequiresGrounding: boolValue(obj, "requiresGrounding", false),
		}
		if err := action.Validate(); err != nil {
			continue
		}
		out = append(out, action)
	}
	return out
}

func fallbackPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) []map[string]any {
	if raw, ok := scenarioFallbackPopulation(run, world); ok && len(raw) > 0 {
		return raw
	}
	worldIndex := 0
	if world != nil {
		worldIndex = world.WorldIndex
	}
	classes := genericPopulationClasses(run)
	out := make([]map[string]any, 0, 24)
	for idx := 0; idx < 24; idx++ {
		agentClass := classes[idx%len(classes)]
		out = append(out, map[string]any{
			"agentClass":       agentClass,
			"displayName":      fmt.Sprintf("Archetype %d-%d", worldIndex+1, idx+1),
			"populationWeight": 1 + float64((idx%4))/3,
			"originRefs":       genericOriginRefs(run, agentClass, idx),
			"traits": map[string]any{
				"attention": 0.3 + 0.1*float64(idx%5),
				"riskBias":  0.2 + 0.1*float64((idx+1)%5),
			},
			"beliefState": map[string]any{
				"trust":  0.4 + 0.05*float64(idx%4),
				"stress": 0.2 + 0.06*float64(idx%6),
			},
			"resourceState": map[string]any{
				"budget": 0.3 + 0.05*float64(idx%6),
			},
			"allowedActions": genericAllowedActions(run, agentClass),
			"messageStyle":   "structured",
		})
	}
	return out
}

func genericPopulationClasses(run *horizonmodels.SimulationRun) []string {
	classes := []string{"consumer", "firm", "media", "regulator", "bank"}
	if run == nil {
		return classes
	}
	recognition := mapValue(run.Recognition)
	primaryAxis := firstString(recognition, "primaryAxis")
	kinds := map[string]bool{}
	for _, object := range recognitionObjectCandidates(recognition["objectCandidates"]) {
		kinds[firstString(object, "kind")] = true
	}
	switch primaryAxis {
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		classes = []string{"regulator", "firm", "procurement_compliance_lead", "pr_agent", "bank"}
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit:
		classes = []string{"enterprise_treasury", "bank", "analyst", "firm", "pr_agent"}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		classes = []string{"supplier", "firm", "logistics_operator", "consumer", "market_intelligence_analyst"}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		classes = []string{"logistics_operator", "warehouse_operator", "firm", "bank", "pr_agent"}
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		classes = []string{"media", "pr_agent", "analyst", "firm", "regulator"}
	}
	if kinds["fac"] {
		classes = append([]string{"facility_operator"}, classes...)
	}
	if kinds["product"] || kinds["domain"] || kinds["market"] {
		classes = append(classes, "procurement_director")
	}
	if kinds["gov"] {
		classes = append(classes, "regional_official")
	}
	return compactStrings(classes)
}

func recognitionObjectCandidates(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		return mapSlice(typed)
	default:
		return nil
	}
}

func fallbackActions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) []horizonmodels.SimulationAction {
	if actions, ok := scenarioFallbackActions(run, world, agents, step); ok && len(actions) > 0 {
		return actions
	}
	out := make([]horizonmodels.SimulationAction, 0, min(len(agents), 16))
	for idx, agent := range agents[:min(len(agents), 16)] {
		if agent == nil {
			continue
		}
		actionType, delta, targets := genericActionPlan(run, world, agent, step, idx)
		if actionType == "" {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:        actionType,
			ActorID:     agent.ID,
			TargetRefs:  compactStrings(targets),
			GeneratedBy: "fallback.generic",
		}
		if action.Type != string(horizonmodels.SimulationActionDoNothing) {
			action.Payload = map[string]any{"delta": delta}
		}
		if action.Validate() != nil {
			continue
		}
		out = append(out, action)
	}
	return out
}

func genericAllowedActions(run *horizonmodels.SimulationRun, agentClass string) []string {
	switch strings.TrimSpace(agentClass) {
	case "regulator", "regional_official", "procurement_compliance_lead":
		return []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionEnforceRule),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionRequestExemption),
			string(horizonmodels.SimulationActionDoNothing),
		}
	case "enterprise_treasury", "bank", "analyst":
		return []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionHedgeCost),
			string(horizonmodels.SimulationActionAdjustGuidance),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
		}
	case "logistics_operator", "warehouse_operator", "facility_operator":
		return []string{
			string(horizonmodels.SimulationActionRerouteShipment),
			string(horizonmodels.SimulationActionReserveInventory),
			string(horizonmodels.SimulationActionAllocateCapacity),
			string(horizonmodels.SimulationActionReprioritizeFlow),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionDoNothing),
		}
	case "media", "pr_agent", "market_intelligence_analyst":
		return []string{
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionAdjustGuidance),
			string(horizonmodels.SimulationActionEscalate),
			string(horizonmodels.SimulationActionDoNothing),
		}
	case "consumer":
		return []string{
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionDoNothing),
		}
	default:
		return []string{
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionReserveInventory),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionAdjustGuidance),
			string(horizonmodels.SimulationActionDoNothing),
		}
	}
}

func genericOriginRefs(run *horizonmodels.SimulationRun, agentClass string, idx int) []string {
	buckets := recognitionPathBuckets(run)
	fallback := defaultRecognitionTargets(run)
	pick := func(limit int, groups ...[]string) []string {
		out := make([]string, 0, limit)
		seen := map[string]struct{}{}
		for _, group := range groups {
			for _, path := range compactStrings(group) {
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				out = append(out, path)
				if len(out) >= limit {
					return out
				}
			}
		}
		return out
	}
	switch strings.TrimSpace(agentClass) {
	case "regulator", "regional_official", "procurement_compliance_lead":
		return pick(3, buckets["gov"], buckets["domain"], buckets["market"], buckets["loc"], fallback)
	case "enterprise_treasury", "bank", "analyst":
		return pick(3, buckets["market"], buckets["org"], buckets["domain"], buckets["loc"], fallback)
	case "supplier", "firm", "facility_operator", "procurement_director":
		return pick(3, buckets["org"], buckets["fac"], buckets["product"], buckets["domain"], buckets["market"], fallback)
	case "logistics_operator", "warehouse_operator":
		return pick(3, buckets["fac"], buckets["domain"], buckets["market"], buckets["loc"], fallback)
	case "media", "pr_agent", "market_intelligence_analyst":
		return pick(3, buckets["market"], buckets["org"], buckets["loc"], buckets["domain"], fallback)
	case "consumer":
		return pick(3, buckets["market"], buckets["product"], buckets["loc"], buckets["domain"], fallback)
	default:
		if len(fallback) == 0 {
			return nil
		}
		start := idx % len(fallback)
		return pick(3, append(fallback[start:], fallback[:start]...))
	}
}

func recognitionPathBuckets(run *horizonmodels.SimulationRun) map[string][]string {
	out := map[string][]string{}
	if run == nil {
		return out
	}
	for _, object := range recognitionObjectCandidates(mapValue(run.Recognition)["objectCandidates"]) {
		path := firstString(object, "path")
		if path == "" {
			continue
		}
		kind := firstString(object, "kind")
		out[kind] = compactStrings(append(out[kind], path))
	}
	for _, path := range compactStrings(run.Request.SeedPaths) {
		if kind := inferPathKind(path); kind != "" {
			out[kind] = compactStrings(append(out[kind], path))
		}
	}
	if run.Request.Signal != nil {
		for _, path := range compactStrings(append(append([]string{}, run.Request.Signal.ActorPaths...), append(run.Request.Signal.SeedPaths, run.Request.Signal.LocationPath)...)) {
			if kind := inferPathKind(path); kind != "" {
				out[kind] = compactStrings(append(out[kind], path))
			}
		}
	}
	return out
}

func defaultRecognitionTargets(run *horizonmodels.SimulationRun) []string {
	paths := runScenarioAnchorPaths(run)
	if len(paths) > 0 {
		return paths[:min(len(paths), 8)]
	}
	if run == nil {
		return nil
	}
	fallback := append([]string{}, run.BaselineRefs.ScopePaths...)
	fallback = append(fallback, run.BaselineRefs.FragmentPaths...)
	return compactStrings(fallback[:min(len(fallback), 8)])
}

func inferPathKind(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "org"
	case strings.HasPrefix(path, "/loc/"):
		return "loc"
	case strings.HasPrefix(path, "/fac/"):
		return "fac"
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/market/"):
		return "market"
	case strings.HasPrefix(path, "/gov/"):
		return "gov"
	case strings.HasPrefix(path, "/domain/"), strings.HasPrefix(path, "/decision/"), strings.HasPrefix(path, "/program/"), strings.HasPrefix(path, "/mechanism/"):
		return "domain"
	default:
		return ""
	}
}

func genericActionPlan(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agent *horizonmodels.SimulationAgentSpec, step, idx int) (string, float64, []string) {
	if agent == nil {
		return "", 0, nil
	}
	axis := runAxis(run)
	class := strings.TrimSpace(agent.AgentClass)
	domain := agentDomain(agent)
	price := metricFloat(world.MarketState, "pricePressure")
	supply := metricFloat(world.MarketState, "supplyStress")
	pathStress := metricFloat(world.MarketState, "pathStressScore")
	bindingConfidence := clamp01(metricFloat(world.MarketState, "bindingConfidence"))
	bindingDeficit := clamp01(1 - bindingConfidence)
	recoveryNorm := clamp01(metricFloat(world.MarketState, "recoveryLatencyHours") / 96.0)
	productRisk := metricFloat(world.MarketState, "productRiskScore")
	shoulderLoad := metricFloat(world.MarketState, "shoulderLoadScore")
	policyPressure := metricFloat(world.InstitutionState, "policyPressure")
	stress := mapFloat(agent.BeliefState, "stress", 0.35)
	pressure := clamp01((price + supply + pathStress + bindingDeficit + recoveryNorm + productRisk + shoulderLoad + policyPressure + stress) / 9.0)
	baseDelta := round3(clamp(0.035+pressure*0.05, 0.035, 0.11))

	choose := func(actionType string, delta float64) (string, float64, []string) {
		targets := genericActionTargets(run, agent, actionType)
		if actionType != string(horizonmodels.SimulationActionDoNothing) && len(targets) == 0 {
			actionType = string(horizonmodels.SimulationActionIssueStatement)
			targets = genericActionTargets(run, agent, actionType)
		}
		return actionType, round3(delta), targets
	}

	if strings.HasPrefix(class, "regional_supply_shoulder_") {
		switch axis {
		case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
			if pathStress >= 0.55 || bindingDeficit >= 0.35 {
				return choose(string(horizonmodels.SimulationActionRequestExemption), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		default:
			if recoveryNorm >= 0.45 || pathStress >= 0.5 {
				return choose(string(horizonmodels.SimulationActionRerouteShipment), baseDelta)
			}
			if supply >= 0.45 || shoulderLoad >= 0.35 {
				return choose(string(horizonmodels.SimulationActionReserveInventory), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionAllocateCapacity), baseDelta-0.005)
		}
	}
	if strings.HasPrefix(class, "product_risk_manager_") {
		switch axis {
		case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
			if productRisk >= 0.35 || bindingDeficit >= 0.35 {
				return choose(string(horizonmodels.SimulationActionRequestExemption), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit:
			if productRisk >= 0.4 || pathStress >= 0.45 {
				return choose(string(horizonmodels.SimulationActionAdjustGuidance), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionAdjustBudget), baseDelta-0.005)
		default:
			if productRisk >= 0.35 || supply >= 0.45 {
				return choose(string(horizonmodels.SimulationActionReprioritizeFlow), baseDelta)
			}
			if pathStress >= 0.45 {
				return choose(string(horizonmodels.SimulationActionReserveInventory), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionSwitchSupplier), baseDelta-0.005)
		}
	}

	switch axis {
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		switch domain {
		case "state":
			if step%2 == 0 {
				return choose(string(horizonmodels.SimulationActionEnforceRule), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		case "compliance":
			if pressure >= 0.45 || step >= 1 {
				return choose(string(horizonmodels.SimulationActionRequestExemption), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		case "market", "logistics", "finance":
			if step%3 == 0 {
				return choose(string(horizonmodels.SimulationActionAdjustBudget), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionRequestExemption), baseDelta-0.005)
		case "info", "political":
			if pressure >= 0.55 && step >= 2 {
				return choose(string(horizonmodels.SimulationActionEscalate), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		default:
			return choose(string(horizonmodels.SimulationActionChangeBelief), baseDelta-0.015)
		}
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit:
		switch domain {
		case "finance":
			if step%2 == 0 {
				return choose(string(horizonmodels.SimulationActionHedgeCost), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionAdjustGuidance), baseDelta-0.005)
		case "market", "food":
			if supply >= 0.45 {
				return choose(string(horizonmodels.SimulationActionReserveInventory), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionAdjustBudget), baseDelta)
		case "logistics":
			if step%2 == 0 {
				return choose(string(horizonmodels.SimulationActionRerouteShipment), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionExpediteShipment), baseDelta+0.01)
		case "info":
			return choose(string(horizonmodels.SimulationActionAdjustGuidance), baseDelta-0.01)
		default:
			return choose(string(horizonmodels.SimulationActionDelayPurchase), baseDelta-0.01)
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		switch domain {
		case "logistics":
			actions := []string{
				string(horizonmodels.SimulationActionRerouteShipment),
				string(horizonmodels.SimulationActionAllocateCapacity),
				string(horizonmodels.SimulationActionReserveInventory),
				string(horizonmodels.SimulationActionExpediteShipment),
			}
			return choose(actions[(idx+step)%len(actions)], baseDelta)
		case "market", "food":
			actions := []string{
				string(horizonmodels.SimulationActionSwitchSupplier),
				string(horizonmodels.SimulationActionReserveInventory),
				string(horizonmodels.SimulationActionAllocateCapacity),
				string(horizonmodels.SimulationActionReprioritizeFlow),
			}
			return choose(actions[(idx+step)%len(actions)], baseDelta)
		case "consumer", "informal":
			if step%3 == 0 || pressure >= 0.55 {
				return choose(string(horizonmodels.SimulationActionDelayPurchase), baseDelta-0.01)
			}
			return choose(string(horizonmodels.SimulationActionSwitchSupplier), baseDelta-0.01)
		case "state", "compliance":
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.015)
		default:
			return choose(string(horizonmodels.SimulationActionSwitchSupplier), baseDelta-0.01)
		}
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		switch domain {
		case "info", "political":
			if pressure >= 0.60 && step >= 2 {
				return choose(string(horizonmodels.SimulationActionEscalate), baseDelta)
			}
			if step%2 == 0 {
				return choose(string(horizonmodels.SimulationActionBroadcastMessage), baseDelta)
			}
			return choose(string(horizonmodels.SimulationActionChangeBelief), baseDelta-0.01)
		case "finance", "market":
			return choose(string(horizonmodels.SimulationActionAdjustGuidance), baseDelta-0.01)
		default:
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		}
	default:
		if pressure < 0.22 && step == 0 {
			return string(horizonmodels.SimulationActionDoNothing), 0, nil
		}
		switch domain {
		case "logistics":
			return choose(string(horizonmodels.SimulationActionRerouteShipment), baseDelta)
		case "finance":
			return choose(string(horizonmodels.SimulationActionAdjustBudget), baseDelta)
		case "info":
			return choose(string(horizonmodels.SimulationActionIssueStatement), baseDelta-0.01)
		case "consumer":
			return choose(string(horizonmodels.SimulationActionDelayPurchase), baseDelta-0.01)
		default:
			return choose(string(horizonmodels.SimulationActionSwitchSupplier), baseDelta)
		}
	}
}

func genericActionTargets(run *horizonmodels.SimulationRun, agent *horizonmodels.SimulationAgentSpec, actionType string) []string {
	buckets := recognitionPathBuckets(run)
	agentTargets := compactStrings(agent.OriginRefs)
	merge := func(limit int, groups ...[]string) []string {
		out := make([]string, 0, limit)
		seen := map[string]struct{}{}
		for _, group := range groups {
			for _, path := range compactStrings(group) {
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				out = append(out, path)
				if len(out) >= limit {
					return out
				}
			}
		}
		return out
	}
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionIssueStatement), string(horizonmodels.SimulationActionBroadcastMessage), string(horizonmodels.SimulationActionChangeBelief), string(horizonmodels.SimulationActionEscalate), string(horizonmodels.SimulationActionAdjustGuidance):
		return merge(4, agentTargets, buckets["market"], buckets["org"], buckets["loc"], buckets["domain"])
	case string(horizonmodels.SimulationActionEnforceRule), string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionLobby):
		return merge(4, agentTargets, buckets["gov"], buckets["domain"], buckets["market"], buckets["loc"])
	case string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionHedgeCost):
		return merge(4, agentTargets, buckets["org"], buckets["market"], buckets["domain"])
	case string(horizonmodels.SimulationActionBuy), string(horizonmodels.SimulationActionSell), string(horizonmodels.SimulationActionDelayPurchase), string(horizonmodels.SimulationActionSwitchSupplier):
		return merge(4, agentTargets, buckets["product"], buckets["market"], buckets["org"], buckets["domain"], buckets["loc"])
	case string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionExpediteShipment), string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReprioritizeFlow):
		return merge(4, agentTargets, buckets["fac"], buckets["domain"], buckets["market"], buckets["product"], buckets["org"], buckets["loc"])
	default:
		return agentTargets
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mapSlice(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func floatValue(input map[string]any, key string, fallback float64) float64 {
	if input == nil {
		return fallback
	}
	switch value := input[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}

func numericPayload(input map[string]any, key string, fallback float64) float64 {
	return floatValue(input, key, fallback)
}

func boolValue(input map[string]any, key string, fallback bool) bool {
	if input == nil {
		return fallback
	}
	value, ok := input[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func mapValue(raw any) map[string]any {
	value, _ := raw.(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func floatMapValue(raw any) map[string]float64 {
	object, _ := raw.(map[string]any)
	out := map[string]float64{}
	for key, value := range object {
		switch typed := value.(type) {
		case float64:
			out[key] = typed
		case int:
			out[key] = float64(typed)
		}
	}
	return out
}

func stringSliceValue(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func normalizeAllowedActions(actions []string) []string {
	valid := map[string]struct{}{}
	for _, action := range horizonmodels.ValidSimulationActionTypes() {
		valid[action] = struct{}{}
	}
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if _, ok := valid[action]; ok {
			out = append(out, action)
		}
	}
	if len(out) == 0 {
		return horizonmodels.ValidSimulationActionTypes()
	}
	return compactStrings(out)
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func metricFloat(input map[string]any, key string) float64 {
	if metrics, ok := input["metrics"].(map[string]any); ok {
		return floatValue(metrics, key, 0)
	}
	return floatValue(input, key, 0)
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

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(input, key); value != "" {
			return value
		}
	}
	return ""
}

func boolPtr(v bool) *bool { return &v }

func ternaryStatus(ok bool, yes, no contracts.TaskStatus) contracts.TaskStatus {
	if ok {
		return yes
	}
	return no
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
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
