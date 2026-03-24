package simulation

import (
	"slices"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/kzvat"
)

func TestKZVATScenarioFallbackPopulationIsRich(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Повышение НДС до 16% в Казахстане. Какие сценарии и кто и как отреагирует.",
			SeedPaths: kzvat.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisFinancialSettlement,
				SeedPaths: kzvat.SeedPaths(),
			},
		},
	}
	world := &horizonmodels.SimulationWorld{WorldIndex: 2}

	raw, ok := scenarioFallbackPopulation(run, world)
	if !ok {
		t.Fatalf("expected kzvat fallback population to match")
	}
	if len(raw) < 30 {
		t.Fatalf("expected rich fallback population, got %d", len(raw))
	}
	if generatedBy, _ := raw[0]["generatedBy"].(string); generatedBy != "scenario.kzvat.population.dsl" {
		t.Fatalf("expected scenario DSL generated population, got %q", generatedBy)
	}
	classes := make([]string, 0, len(raw))
	for _, spec := range raw {
		if cls, _ := spec["agentClass"].(string); cls != "" {
			classes = append(classes, cls)
		}
	}
	for _, want := range []string{
		"regulator",
		"logistics_operator",
		"warehouse_operator",
		"restaurant_operator",
		"tax_advisor",
		"municipal_buyer",
		"ecommerce_platform",
		"opposition_politician",
		"agri_producer",
		"fuel_distributor",
		"consumer_urban_low_income",
		"consumer_rural_low_income",
		"consumer_family_mid_income",
		"consumer_ecommerce_native",
		"consumer_bazaar_sensitive",
		"consumer_affluent",
		"large_enterprise_retail",
		"large_enterprise_importer",
		"large_enterprise_manufacturer",
		"enterprise_treasury",
		"budget_revenue_officer",
		"investment_climate_officer",
		"internal_producer",
		"healthcare_public_operator",
		"healthcare_private_operator",
		"engineering_infrastructure_operator",
		"education_system_operator",
	} {
		if !slices.Contains(classes, want) {
			t.Fatalf("expected agent class %q in fallback population", want)
		}
	}
}

func TestKZVATScenarioDSLDirectInteractionsMutateTargets(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Повышение НДС до 16% в Казахстане. Какие сценарии и кто и как отреагирует.",
			SeedPaths: kzvat.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisLegalCompliance,
				SeedPaths: kzvat.SeedPaths(),
			},
		},
	}
	world := &horizonmodels.SimulationWorld{WorldIndex: 1}
	actor := &horizonmodels.SimulationAgentSpec{
		ID:             "importer-1",
		AgentClass:     "importer",
		BeliefState:    map[string]float64{"stress": 0.40, "trust": 0.40},
		ResourceState:  map[string]float64{"budget": 0.40},
		Traits:         map[string]any{"attention": 0.70},
		AllowedActions: []string{string(horizonmodels.SimulationActionLobby)},
	}
	target := &horizonmodels.SimulationAgentSpec{
		ID:             "reg-1",
		AgentClass:     "regulator",
		BeliefState:    map[string]float64{"stress": 0.20, "trust": 0.60},
		ResourceState:  map[string]float64{"budget": 0.70},
		Traits:         map[string]any{"attention": 0.50},
		AllowedActions: []string{string(horizonmodels.SimulationActionIssueStatement)},
	}
	agents := []*horizonmodels.SimulationAgentSpec{actor, target}
	action := horizonmodels.SimulationAction{
		Type:    string(horizonmodels.SimulationActionLobby),
		ActorID: actor.ID,
	}

	effects, ok := scenarioDirectInteractions(run, world, agents, actor, action, 1)
	if !ok || len(effects) == 0 {
		t.Fatalf("expected direct interaction effects for importer lobby")
	}
	applyScenarioInteractionEffects(agents, effects)

	if target.BeliefState["stress"] <= 0.20 {
		t.Fatalf("expected target stress to increase, got %.3f", target.BeliefState["stress"])
	}
	if target.BeliefState["trust"] >= 0.60 {
		t.Fatalf("expected target trust to decrease, got %.3f", target.BeliefState["trust"])
	}
	if mapAnyFloat(target.Traits, "attention", 0) <= 0.50 {
		t.Fatalf("expected target attention to increase, got %.3f", mapAnyFloat(target.Traits, "attention", 0))
	}
}

func TestKZVATScenarioFallbackActionsVaryAcrossWorlds(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Повышение НДС до 16% в Казахстане. Какие сценарии и кто и как отреагирует.",
			SeedPaths: kzvat.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisPhysicalFlowDemand,
				SeedPaths: kzvat.SeedPaths(),
			},
		},
	}
	worldA := &horizonmodels.SimulationWorld{WorldIndex: 0}
	worldB := &horizonmodels.SimulationWorld{WorldIndex: 3}
	agents := []*horizonmodels.SimulationAgentSpec{
		{ID: "a1", AgentClass: "consumer_low_income", OriginRefs: []string{"/market/kz/consumer/food"}},
		{ID: "a2", AgentClass: "consumer_mid_income", OriginRefs: []string{"/market/kz/consumer/retail"}},
		{ID: "a3", AgentClass: "small_trader", OriginRefs: []string{"/community/kz/trade/bazaar-traders"}},
		{ID: "a4", AgentClass: "retailer", OriginRefs: []string{"/market/kz/consumer/retail"}},
		{ID: "a5", AgentClass: "importer", OriginRefs: []string{"/market/kz/industrial/import-distribution"}},
		{ID: "a6", AgentClass: "media", OriginRefs: []string{"/loc/kz"}},
	}

	actionsA, okA := scenarioFallbackActions(run, worldA, agents, 2)
	actionsB, okB := scenarioFallbackActions(run, worldB, agents, 2)
	if !okA || !okB {
		t.Fatalf("expected kzvat fallback actions to match")
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
