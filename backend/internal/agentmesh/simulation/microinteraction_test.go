package simulation

import (
	"context"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyMicrointeractionLayerCreatesProtocolAndTransaction(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		ID:       "run-slb-fin",
		TenantID: "system",
		Status:   string(horizonmodels.SimulationRunStatusRunning),
		Request: horizonmodels.SimulationRunRequest{
			Question: "SLB Russia financial stress",
			Mode:     "slb-russia-financial",
			Signal: &horizonmodels.SimulationShock{
				Axis:         horizonmodels.AxisFinancialSettlement,
				LocationPath: "/loc/ru",
				SeedPaths:    []string{"/domain/v1/supply-chain/geo/ru/subject/slb"},
				ActorPaths:   []string{"/org/us/slb"},
				Type:         "sanctions_shock",
				Polarity:     "risk",
				Magnitude:    0.7,
			},
			WorldCount: 1,
			TickCount:  6,
		},
		Execution: horizonmodels.SimulationExecutionConfig{
			WorldCount: 1,
			TickCount:  6,
			Mode:       "slb-russia-financial",
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/org/us/slb",
				"/market/ru/energy/oilfield-services",
				"/fac/ae/jebel-ali-logistics-center",
			},
		},
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	world := &horizonmodels.SimulationWorld{
		ID:                "world-1",
		TenantID:          "system",
		RunID:             run.ID,
		WorldIndex:        0,
		Status:            string(horizonmodels.SimulationWorldStatusRunning),
		AgentState:        map[string]any{},
		ProtocolTraces:    []map[string]any{},
		Transactions:      []map[string]any{},
		RelationshipState: map[string]any{},
	}
	rawAgents := fallbackPopulation(run, world)
	if len(rawAgents) == 0 {
		t.Fatalf("expected scenario population")
	}
	agents := make([]*horizonmodels.SimulationAgentSpec, 0, len(rawAgents))
	for idx, raw := range rawAgents {
		agent := &horizonmodels.SimulationAgentSpec{
			ID:               "agent-" + string(rune('a'+idx)),
			TenantID:         run.TenantID,
			RunID:            run.ID,
			WorldID:          world.ID,
			Status:           "active",
			AgentClass:       firstString(raw, "agentClass"),
			DisplayName:      firstString(raw, "displayName"),
			OriginRefs:       stringSliceValue(raw["originRefs"]),
			PopulationWeight: floatValue(raw, "populationWeight", 1),
			Traits:           mapValue(raw["traits"]),
			BeliefState:      floatMapValue(raw["beliefState"]),
			ResourceState:    floatMapValue(raw["resourceState"]),
			AllowedActions:   stringSliceValue(raw["allowedActions"]),
			ExpiresAt:        run.ExpiresAt,
		}
		agents = append(agents, agent)
	}
	var actor *horizonmodels.SimulationAgentSpec
	for _, agent := range agents {
		if agent.AgentClass == "ofs_settlement_bank" {
			actor = agent
			break
		}
	}
	if actor == nil {
		t.Fatalf("expected ofs_settlement_bank")
	}
	action := horizonmodels.SimulationAction{
		Type:       string(horizonmodels.SimulationActionHedgeCost),
		ActorID:    actor.ID,
		TargetRefs: actor.OriginRefs,
		Payload:    map[string]any{"delta": 0.07},
	}
	summary := applyMicrointeractionLayer(run, world, agents, actor, action, 0, nil)
	if summary.protocolCount == 0 {
		t.Fatalf("expected protocol traces to be created")
	}
	if len(world.ProtocolTraces) == 0 {
		t.Fatalf("expected world protocol traces")
	}
	if len(world.Transactions) == 0 {
		t.Fatalf("expected world transactions")
	}
}

func TestGenerateStepActionsPrefersScenarioFallbackForHotPath(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		ID:       "run-slb-physical",
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "SLB Russia sanctions routing shock",
			Mode:     "slb-russia-physical",
			Signal: &horizonmodels.SimulationShock{
				Axis:         horizonmodels.AxisPhysicalFlowSupply,
				LocationPath: "/loc/ru",
				SeedPaths:    []string{"/domain/v1/supply-chain/geo/ru/subject/slb"},
				ActorPaths:   []string{"/org/us/slb"},
				Type:         "sanctions_shock",
				Polarity:     "risk",
				Magnitude:    0.8,
			},
			WorldCount: 1,
			TickCount:  6,
		},
		Execution: horizonmodels.SimulationExecutionConfig{
			WorldCount: 1,
			TickCount:  6,
			Mode:       "slb-russia-physical",
		},
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	world := &horizonmodels.SimulationWorld{
		ID:         "world-1",
		TenantID:   "system",
		RunID:      run.ID,
		WorldIndex: 0,
		Status:     string(horizonmodels.SimulationWorldStatusRunning),
	}
	rawAgents := fallbackPopulation(run, world)
	agents := make([]*horizonmodels.SimulationAgentSpec, 0, len(rawAgents))
	for idx, raw := range rawAgents {
		agents = append(agents, &horizonmodels.SimulationAgentSpec{
			ID:             "agent-" + string(rune('a'+idx)),
			TenantID:       run.TenantID,
			RunID:          run.ID,
			WorldID:        world.ID,
			Status:         "active",
			AgentClass:     firstString(raw, "agentClass"),
			DisplayName:    firstString(raw, "displayName"),
			AllowedActions: stringSliceValue(raw["allowedActions"]),
			OriginRefs:     stringSliceValue(raw["originRefs"]),
			ExpiresAt:      run.ExpiresAt,
		})
	}
	processor := &Processor{}
	actions, err := processor.generateStepActions(context.Background(), &runtime.Service{}, run, world, agents, 0)
	if err != nil {
		t.Fatalf("generateStepActions returned error: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("expected scenario actions")
	}
	if actions[0].GeneratedBy != "scenario.slb_russia_sanctions.fallback" {
		t.Fatalf("expected scenario-first step actions, got %+v", actions[0])
	}
}

func TestProcessProtocolQueuesResolvesReviewAndUpdatesTransaction(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		ID:       "run-slb-legal",
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "SLB Russia sanctions review queue",
			Mode:     "slb-russia-legal",
			Signal: &horizonmodels.SimulationShock{
				Axis:         horizonmodels.AxisLegalCompliance,
				LocationPath: "/loc/ru",
				SeedPaths:    []string{"/domain/v1/supply-chain/geo/ru/subject/slb"},
				ActorPaths:   []string{"/org/us/slb"},
				Type:         "sanctions_shock",
				Polarity:     "risk",
				Magnitude:    0.8,
			},
		},
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisLegalCompliance,
			"objectCandidates": []map[string]any{
				{"path": "/org/us/slb", "kind": "org", "score": 1.0},
				{"path": "/domain/v1/policy-impact/geo/ru/topic/sanctions", "kind": "domain", "score": 0.9},
				{"path": "/loc/ru", "kind": "loc", "score": 0.8},
			},
		},
	}
	world := &horizonmodels.SimulationWorld{
		ID:                "world-queue",
		TenantID:          "system",
		RunID:             run.ID,
		WorldIndex:        0,
		Status:            string(horizonmodels.SimulationWorldStatusRunning),
		AgentState:        map[string]any{},
		ProtocolTraces:    []map[string]any{},
		Transactions:      []map[string]any{},
		RelationshipState: map[string]any{},
	}
	actor := &horizonmodels.SimulationAgentSpec{
		ID:             "slb-proc",
		AgentClass:     "slb_russia_procurement_lead",
		OriginRefs:     []string{"/org/us/slb"},
		BeliefState:    map[string]float64{"stress": 0.52, "trust": 0.44},
		ResourceState:  map[string]float64{"budget": 0.54},
		AllowedActions: []string{string(horizonmodels.SimulationActionRequestExemption)},
	}
	target := &horizonmodels.SimulationAgentSpec{
		ID:             "slb-comp",
		AgentClass:     "sanctions_compliance_officer",
		OriginRefs:     []string{"/domain/v1/policy-impact/geo/ru/topic/sanctions"},
		BeliefState:    map[string]float64{"stress": 0.58, "trust": 0.50},
		ResourceState:  map[string]float64{"budget": 0.56},
		AllowedActions: []string{string(horizonmodels.SimulationActionRequestExemption)},
	}
	agents := []*horizonmodels.SimulationAgentSpec{actor, target}
	action := horizonmodels.SimulationAction{
		Type:       string(horizonmodels.SimulationActionRequestExemption),
		ActorID:    actor.ID,
		TargetRefs: []string{"/domain/v1/policy-impact/geo/ru/topic/sanctions"},
		Payload:    map[string]any{"delta": 0.08},
	}
	summary := applyMicrointeractionLayer(run, world, agents, actor, action, 0, nil)
	if summary.protocolCount == 0 || len(world.Transactions) == 0 {
		t.Fatalf("expected initial protocol and transaction")
	}
	queueSummary := processProtocolQueues(run, world, agents, 2)
	if queueSummary.protocolCount == 0 {
		t.Fatalf("expected inbox/review processing")
	}
	queueSummary = processProtocolQueues(run, world, agents, 3)
	if queueSummary.protocolCount == 0 {
		t.Fatalf("expected review decision processing")
	}
	if countOpenReviewQueue(world) != 0 {
		t.Fatalf("expected review queue to be resolved")
	}
	tx := world.Transactions[0]
	status := stringValue(tx, "status")
	if status == "" || status == "proposed" || status == "pending_review" {
		t.Fatalf("expected transaction to advance after review, got %q", status)
	}
}

func TestTransactionOperationalLoadKeepsSupplyStressNonZero(t *testing.T) {
	world := &horizonmodels.SimulationWorld{
		Transactions: []map[string]any{
			{"status": "in_execution", "queueStatus": "responded", "frictionScore": 0.5},
			{"status": "pending_review", "queueStatus": "review_pending", "frictionScore": 0.7},
			{"status": "rerouted", "queueStatus": "responded", "frictionScore": 0.4},
		},
	}
	summary := transactionOperationalLoad(world, 2)
	if summary.stateDelta.supply <= 0 {
		t.Fatalf("expected positive supply operational load, got %+v", summary.stateDelta)
	}
	if summary.stateDelta.price <= 0 {
		t.Fatalf("expected positive price operational load, got %+v", summary.stateDelta)
	}
}

func TestExecutionPressureFloorReflectsOpenOperationalQueues(t *testing.T) {
	world := &horizonmodels.SimulationWorld{
		AgentState: map[string]any{
			"queues": map[string]any{
				"review": []map[string]any{
					{"resolved": false},
					{"resolved": false},
				},
			},
		},
		Transactions: []map[string]any{
			{"status": "in_execution"},
			{"status": "pending_review"},
			{"status": "delayed"},
		},
	}
	floor := executionPressureFloor(world, horizonmodels.AxisPhysicalFlowSupply)
	if floor <= 0 {
		t.Fatalf("expected positive execution floor, got %.3f", floor)
	}
}
