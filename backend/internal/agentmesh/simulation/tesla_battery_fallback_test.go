package simulation

import (
	"slices"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/teslabattery"
)

func TestTeslaBatteryScenarioFallbackPopulationIsRich(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Tesla battery logistics under trade restrictions and logistics delays.",
			SeedPaths: teslabattery.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisFinancialSettlement,
				SeedPaths: teslabattery.SeedPaths(),
			},
		},
	}
	world := &horizonmodels.SimulationWorld{WorldIndex: 2}

	raw, ok := scenarioFallbackPopulation(run, world)
	if !ok {
		t.Fatalf("expected tesla fallback population to match")
	}
	if len(raw) < 20 {
		t.Fatalf("expected rich fallback population, got %d", len(raw))
	}
	if generatedBy, _ := raw[0]["generatedBy"].(string); generatedBy != "scenario.tesla_battery_logistics.population.dsl" {
		t.Fatalf("expected scenario DSL generated population, got %q", generatedBy)
	}
	classes := make([]string, 0, len(raw))
	for _, spec := range raw {
		if cls, _ := spec["agentClass"].(string); cls != "" {
			classes = append(classes, cls)
		}
	}
	for _, want := range []string{
		"trade_official",
		"customs_official",
		"facility_operator_nevada",
		"facility_operator_texas",
		"panasonic_line_operator",
		"catl_export_desk",
		"material_supplier_lithium",
		"lithium_refinery_operator",
		"ocean_logistics_operator",
		"ground_logistics_operator",
		"procurement_director",
		"production_planner",
		"enterprise_treasury",
		"vehicle_program_manager",
		"energy_storage_program_manager",
		"pr_agent",
		"investor_relations",
		"equity_analyst",
		"large_energy_customer",
		"competitor_demand_sink",
	} {
		if !slices.Contains(classes, want) {
			t.Fatalf("expected agent class %q in fallback population", want)
		}
	}
}

func TestTeslaScenarioDSLDirectInteractionsMutateTargets(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Tesla battery logistics under trade restrictions and logistics delays.",
			SeedPaths: teslabattery.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisLegalCompliance,
				SeedPaths: teslabattery.SeedPaths(),
			},
		},
	}
	world := &horizonmodels.SimulationWorld{WorldIndex: 1}
	actor := &horizonmodels.SimulationAgentSpec{
		ID:             "trade-1",
		AgentClass:     "trade_official",
		BeliefState:    map[string]float64{"stress": 0.30, "trust": 0.52},
		ResourceState:  map[string]float64{"budget": 0.70},
		Traits:         map[string]any{"attention": 0.80},
		AllowedActions: []string{string(horizonmodels.SimulationActionEnforceRule)},
	}
	target := &horizonmodels.SimulationAgentSpec{
		ID:             "proc-1",
		AgentClass:     "procurement_director",
		BeliefState:    map[string]float64{"stress": 0.20, "trust": 0.56},
		ResourceState:  map[string]float64{"budget": 0.62},
		Traits:         map[string]any{"attention": 0.50},
		AllowedActions: []string{string(horizonmodels.SimulationActionSwitchSupplier)},
	}
	agents := []*horizonmodels.SimulationAgentSpec{actor, target}
	action := horizonmodels.SimulationAction{
		Type:    string(horizonmodels.SimulationActionEnforceRule),
		ActorID: actor.ID,
	}

	effects, ok := scenarioDirectInteractions(run, world, agents, actor, action, 1)
	if !ok || len(effects) == 0 {
		t.Fatalf("expected direct interaction effects for trade official")
	}
	applyScenarioInteractionEffects(agents, effects)

	if target.BeliefState["stress"] <= 0.20 {
		t.Fatalf("expected target stress to increase, got %.3f", target.BeliefState["stress"])
	}
	if target.BeliefState["trust"] >= 0.56 {
		t.Fatalf("expected target trust to decrease, got %.3f", target.BeliefState["trust"])
	}
	if mapAnyFloat(target.Traits, "attention", 0) <= 0.50 {
		t.Fatalf("expected target attention to increase, got %.3f", mapAnyFloat(target.Traits, "attention", 0))
	}
}

func TestTeslaFallbackActionsVaryAcrossWorlds(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Tesla battery logistics under trade restrictions and logistics delays.",
			SeedPaths: teslabattery.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisPhysicalFlowDemand,
				SeedPaths: teslabattery.SeedPaths(),
			},
		},
	}
	worldA := &horizonmodels.SimulationWorld{WorldIndex: 0}
	worldB := &horizonmodels.SimulationWorld{WorldIndex: 3}
	agents := []*horizonmodels.SimulationAgentSpec{
		{ID: "a1", AgentClass: "procurement_director", OriginRefs: []string{"/org/us/tesla"}},
		{ID: "a2", AgentClass: "facility_operator_texas", OriginRefs: []string{"/fac/us/gigafactory-texas"}},
		{ID: "a3", AgentClass: "facility_operator_nevada", OriginRefs: []string{"/fac/us/gigafactory-nevada"}},
		{ID: "a4", AgentClass: "ocean_logistics_operator", OriginRefs: []string{"/market/us/transportation/freight-logistics"}},
		{ID: "a5", AgentClass: "energy_storage_program_manager", OriginRefs: []string{"/market/us/energy/energy-storage-systems"}},
		{ID: "a6", AgentClass: "competitor_demand_sink", OriginRefs: []string{"/market/us/industrial/ev-battery-cells"}},
	}

	actionsA, okA := scenarioFallbackActions(run, worldA, agents, 2)
	actionsB, okB := scenarioFallbackActions(run, worldB, agents, 2)
	if !okA || !okB {
		t.Fatalf("expected tesla fallback actions to match")
	}
	if len(actionsA) == 0 || len(actionsB) == 0 {
		t.Fatalf("expected non-empty actions")
	}
	same := len(actionsA) == len(actionsB)
	if same {
		for i := range actionsA {
			if actionsA[i].Type != actionsB[i].Type {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatalf("expected action plans to vary across world variants")
	}
}

func TestTeslaScenarioDSLHasMetadataAndGraphRequirements(t *testing.T) {
	dsl, err := loadScenarioDSL("tesla_battery_logistics")
	if err != nil {
		t.Fatalf("loadScenarioDSL() error: %v", err)
	}
	if dsl == nil {
		t.Fatal("expected non-nil DSL")
	}
	if dsl.Metadata.ID == "" || dsl.Metadata.Version == "" {
		t.Fatalf("expected metadata to be populated: %#v", dsl.Metadata)
	}
	if len(dsl.GraphRequirements.RequiredPaths) == 0 {
		t.Fatal("expected required graph paths")
	}
	if len(dsl.GraphRequirements.RequiredBindings) == 0 {
		t.Fatal("expected required graph bindings")
	}
}

func TestTeslaHotPathDoesNotTriggerFromIncidentalBaselineRefs(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "What happens to Pilbara iron ore exports if shipping disruptions intensify?",
			Signal: &horizonmodels.SimulationShock{
				Axis: horizonmodels.AxisPhysicalFlowSupply,
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/loc/au",
				"/org/au/bhp",
				"/fac/us/gigafactory-texas",
			},
		},
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisPhysicalFlowSupply,
			"objectCandidates": []map[string]any{
				{"path": "/loc/au", "kind": "loc", "score": 1.0},
				{"path": "/org/au/bhp", "kind": "org", "score": 0.9},
				{"path": "/product/raw/mineral/extraction/iron-ore", "kind": "product", "score": 0.8},
			},
		},
	}
	if isTeslaBatteryHotPath(run) {
		t.Fatal("expected incidental baseline refs to not trigger tesla hot-path")
	}
}
