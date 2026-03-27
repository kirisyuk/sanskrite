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

func TestScopeDerivedShouldersDoNotInventNewCountriesWhenArtifactPreviewExists(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisPhysicalFlowSupply,
			"objectCandidates": []map[string]any{
				{"path": "/domain/v1/supply-chain/geo/kz/product/toys", "kind": "domain", "score": 2.0},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/fac/cn/lego-jiaxing-factory",
				"/fac/de/port-of-hamburg",
				"/fac/us/port-of-oakland",
				"/fac/kz/khorgos-gateway",
			},
		},
		BaselinePreview: map[string]any{
			"artifactTopShoulders": []map[string]any{
				{"code": "cn", "riskScore": 1.4, "pathCount": 6, "maxStress": 0.82, "refs": []string{"/fac/cn/lego-jiaxing-factory"}},
				{"code": "ru", "riskScore": 0.6, "pathCount": 2, "maxStress": 0.31, "refs": []string{"/fac/ru/detskiy-mir-moscow-dc"}},
			},
		},
	}
	shoulders := scopeDerivedShoulders(run)
	got := make([]string, 0, len(shoulders))
	for _, shoulder := range shoulders {
		got = append(got, shoulder.CountryCode)
	}
	if !slices.Equal(got, []string{"cn", "ru"}) {
		t.Fatalf("expected only artifact-backed shoulders [cn ru], got %v", got)
	}
}

func TestRecognitionPrimaryGeoUsesDomainGeoPath(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		Recognition: map[string]any{
			"objectCandidates": []map[string]any{
				{"path": "/domain/v1/supply-chain/geo/kz/product/toys", "kind": "domain", "score": 2.0},
				{"path": "/market/kz/consumer-goods/toys", "kind": "market", "score": 1.5},
			},
		},
	}
	if got := recognitionPrimaryGeo(run); got != "kz" {
		t.Fatalf("expected primary geo kz, got %q", got)
	}
}

func TestScopeDerivedProductsSkipsLogisticsPseudoProducts(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisLogisticsCapacity,
			"objectCandidates": []map[string]any{
				{"path": "/domain/v1/supply-chain/geo/kz/product/toys", "kind": "domain", "score": 2.0},
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			ScopePaths: []string{
				"/market/kz/consumer-goods/toys",
				"/product/finished/mechanical/assembly/toys",
			},
		},
		BaselinePreview: map[string]any{
			"artifactTopProducts": []map[string]any{
				{"path": "/product/finished/mechanical/assembly/container-transshipment", "riskScore": 1.2, "pathCount": 8, "maxStress": 0.48},
				{"path": "/product/finished/mechanical/assembly/toys", "riskScore": 1.1, "pathCount": 7, "maxStress": 0.44},
			},
		},
	}
	products := scopeDerivedProducts(run)
	if len(products) == 0 {
		t.Fatal("expected products")
	}
	for _, product := range products {
		if product.ProductPath == "/product/finished/mechanical/assembly/container-transshipment" {
			t.Fatalf("expected logistics pseudo-product to be filtered out: %#v", products)
		}
	}
}
