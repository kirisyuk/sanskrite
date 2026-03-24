package simulation

import (
	"slices"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestSLBRussiaFallbackPopulationIncludesDerivedShouldersAndProducts(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "Secondary sanctions tighten around SLB-related equipment flowing into Russia through third-country hubs.",
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisPhysicalFlowSupply,
				SeedPaths: []string{"/domain/v1/supply-chain/geo/ru/subject/slb"},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/domain/v1/supply-chain/geo/ru/subject/slb",
				"/org/us/slb",
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/fac/cn/qingdao-smith-bits-factory",
				"/fac/sg/jurong-logistics-hub",
				"/fac/in/dorf-ketal-taloja-plant",
				"/product/finished/electronic/manufacturing/telemetry-tools",
				"/product/finished/mechanical/assembly/well-completion-systems",
				"/product/finished/chemical/manufacturing/specialty-drilling-chemicals",
			},
		},
	}
	world := &horizonmodels.SimulationWorld{WorldIndex: 0}

	raw, ok := slbRussiaFallbackPopulation(run, world)
	if !ok {
		t.Fatalf("expected slb fallback population to match")
	}
	classes := make([]string, 0, len(raw))
	for _, item := range raw {
		if class, _ := item["agentClass"].(string); class != "" {
			classes = append(classes, class)
		}
	}
	for _, want := range []string{
		"regional_supply_shoulder_cn",
		"regional_supply_shoulder_sg",
		"regional_supply_shoulder_in",
		"product_risk_manager_telemetry-tools",
		"product_risk_manager_well-completion-systems",
		"product_risk_manager_specialty-drilling-chemicals",
	} {
		if !slices.Contains(classes, want) {
			t.Fatalf("expected derived class %q in fallback population", want)
		}
	}
}

func TestSLBScenarioMonitoringOverlayIncludesDerivedSignals(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "Secondary sanctions tighten around SLB-related equipment flowing into Russia through third-country hubs.",
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/domain/v1/supply-chain/geo/ru/subject/slb",
				"/org/us/slb",
				"/fac/sg/jurong-logistics-hub",
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/product/finished/electronic/manufacturing/telemetry-tools",
			},
		},
	}

	items := scenarioMonitoringOverlay(run)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, _ := item["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	for _, want := range []string{
		"shoulder_load_sg",
		"shoulder_load_cn",
		"product_availability_telemetry-tools",
	} {
		if !slices.Contains(ids, want) {
			t.Fatalf("expected monitoring signal %q, got %v", want, ids)
		}
	}
}
