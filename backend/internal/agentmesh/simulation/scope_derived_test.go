package simulation

import (
	"slices"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestScopeDerivedPopulationExtrasIncludeShouldersAndProducts(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisPhysicalFlowSupply,
			"objectCandidates": []map[string]any{
				{"path": "/loc/ru", "kind": "geo", "score": 1.0},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/fac/in/dorf-ketal-taloja-plant",
				"/fac/sg/jurong-logistics-hub",
				"/product/finished/electronic/manufacturing/telemetry-tools",
				"/product/finished/mechanical/assembly/well-completion-systems",
			},
		},
	}
	items := scopeDerivedPopulationExtras(run)
	classes := make([]string, 0, len(items))
	for _, item := range items {
		if class, _ := item["agentClass"].(string); class != "" {
			classes = append(classes, class)
		}
	}
	for _, want := range []string{
		"regional_supply_shoulder_cn",
		"regional_supply_shoulder_in",
		"regional_supply_shoulder_sg",
		"product_risk_manager_telemetry-tools",
		"product_risk_manager_well-completion-systems",
	} {
		if !slices.Contains(classes, want) {
			t.Fatalf("expected %q in scope-derived population, got %v", want, classes)
		}
	}
}

func TestScopeDerivedMonitoringOverlayIncludesProductAndShoulderSignals(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisLogisticsCapacity,
			"objectCandidates": []map[string]any{
				{"path": "/loc/ru", "kind": "geo", "score": 1.0},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/product/finished/electronic/manufacturing/telemetry-tools",
			},
		},
	}
	items := scopeDerivedMonitoringOverlay(run)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, _ := item["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	for _, want := range []string{"shoulder_load_cn", "product_availability_telemetry-tools"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("expected %q in scope-derived monitoring, got %v", want, ids)
		}
	}
}

func TestScopeDerivedSignalsPreferArtifactMetricPreview(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisPhysicalFlowSupply,
			"objectCandidates": []map[string]any{
				{"path": "/loc/ru", "kind": "geo", "score": 1.0},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/product/finished/electronic/manufacturing/telemetry-tools",
			},
		},
		BaselinePreview: map[string]any{
			"artifactTopShoulders": []map[string]any{
				{"code": "cn", "riskScore": 1.4, "pathCount": 5, "maxStress": 0.88, "refs": []string{"/fac/cn/jereh-yantai-manufacturing-center"}},
			},
			"artifactTopProducts": []map[string]any{
				{"path": "/product/finished/electronic/manufacturing/telemetry-tools", "riskScore": 1.2, "pathCount": 4, "maxStress": 0.91},
			},
		},
	}
	shoulders := scopeDerivedShoulders(run)
	if len(shoulders) == 0 || shoulders[0].Severity < 1.39 {
		t.Fatalf("expected metric-backed shoulder severity, got %#v", shoulders)
	}
	products := scopeDerivedProducts(run)
	if len(products) == 0 || products[0].Severity < 1.19 {
		t.Fatalf("expected metric-backed product severity, got %#v", products)
	}
}
