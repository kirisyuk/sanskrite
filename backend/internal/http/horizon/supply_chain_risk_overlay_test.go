package horizon

import (
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestBuildSupplyChainPathRiskOverlay_ExtractsMechanismsAndLegDetails(t *testing.T) {
	unitDays := "days"
	artifact := &horizonmodels.MaterializedArtifact{
		ID:           "path-1",
		TenantID:     "global",
		Kind:         horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey: "/supply-chain/global/fac_ae_jebel-ali-logistics-center/product_component_mechanical_manufacturing_oilfield-spare-parts/fac_ru_slb-tyumen-factory",
		DisplayName:  "Jebel Ali spare parts -> Tyumen",
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassSite, Ref: "/fac/ae/jebel-ali-logistics-center"},
				{Class: ah.FragmentClassProduct, Ref: "/product/component/mechanical/manufacturing/oilfield-spare-parts"},
				{Class: ah.FragmentClassSite, Ref: "/fac/ru/slb-tyumen-factory"},
			},
			[]horizonmodels.SupplyChainPathEdge{
				{
					Relation:                ah.RelationNameSupplies,
					From:                    0,
					To:                      2,
					ViaProductIndex:         overlayIntPtr(1),
					ProductNodeIndex:        overlayIntPtr(1),
					ProductRef:              "/product/component/mechanical/manufacturing/oilfield-spare-parts",
					SupplyLinkCanonicalPath: "/supply-link/global/spares-1",
					SegmentIndex:            overlayIntPtr(0),
					CarriesProduct: true,
					Metrics: map[string]horizonmodels.MetricValue{
						"lead_time_days": {Value: 30, Unit: &unitDays},
						"inventory_cover_days_at_destination": {Value: 7, Unit: &unitDays},
						"customs_clearance_days": {Value: 11, Unit: &unitDays},
						"route_fragility": {Value: 0.78},
						"route_rerouteability": {Value: 0.22},
						"service_criticality": {Value: 0.81},
						"supply_chain_criticality": {Value: 0.91},
					},
					Qualifiers: map[string]interface{}{
						"role":                   "product_flow",
						"primaryRiskMechanism":   "inventory_vulnerability",
						"riskFlags":              []interface{}{"inventory_vulnerability", "low_substitutability", "sole_source_lock_in"},
						"service_contract_required": true,
						"export_license_required":   true,
					},
				},
			},
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/fac/ae/jebel-ali-logistics-center",
					CustomerRef:             "/fac/ru/slb-tyumen-factory",
					ProductRef:              "/product/component/mechanical/manufacturing/oilfield-spare-parts",
					SupplyLinkCanonicalPath: "/supply-link/global/spares-1",
					TransportViaRefs:        []string{"/fac/ae/jebel-ali-port", "/fac/eg/suez-canal", "/fac/ru/novorossiysk-seaport"},
					Stress: &horizonmodels.SupplyChainLegStress{
						Fragility:       0.74,
						Rerouteability:  0.24,
						StressScore:     0.79,
						DominantFactors: []string{"route_fragility", "customs_friction"},
					},
				},
			},
			&horizonmodels.SupplyChainPathStress{
				PathScore:          0.81,
				Fragility:          0.74,
				Rerouteability:     0.24,
				BottleneckLegIndex: 0,
				DominantFactors:    []string{"route_fragility", "inventory_vulnerability"},
			},
			nil,
		),
	}

	overlay := buildSupplyChainPathRiskOverlay(artifact)
	if overlay == nil {
		t.Fatal("overlay is nil")
	}
	if overlay.PrimaryMechanism == "" {
		t.Fatal("primary mechanism empty")
	}
	if overlay.Criticality < 0.75 {
		t.Fatalf("criticality=%v want >= 0.75", overlay.Criticality)
	}
	if overlay.BottleneckLegIndex != 0 {
		t.Fatalf("bottleneckLegIndex=%d want 0", overlay.BottleneckLegIndex)
	}
	if len(overlay.Legs) != 1 {
		t.Fatalf("legs=%d want 1", len(overlay.Legs))
	}
	if !overlay.Legs[0].Bottleneck {
		t.Fatal("expected bottleneck leg")
	}
	if len(overlay.Bottlenecks) == 0 {
		t.Fatal("expected bottleneck nodes")
	}
	if len(overlay.DriverMetrics) == 0 {
		t.Fatal("expected driver metrics")
	}
	if !strings.Contains(strings.ToLower(overlay.Summary), "bottleneck") && !strings.Contains(strings.ToLower(overlay.Summary), "exposed") {
		t.Fatalf("summary=%q", overlay.Summary)
	}
}

func TestBuildSupplyChainObjectAndLensRiskOverlay_AggregateTopProducts(t *testing.T) {
	artifactA := &horizonmodels.MaterializedArtifact{
		ID:             "path-a",
		TenantID:       "global",
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey:   "/supply-chain/global/fac_ae_jebel-ali-logistics-center/product_component_mechanical_manufacturing_oilfield-spare-parts/fac_ru_slb-tyumen-factory",
		DisplayName:    "Spare parts path",
		PathStressScore: 0.74,
	}
	artifactA.RiskOverlay = &horizonmodels.SupplyChainPathRiskOverlay{
		Scope:            "path",
		Summary:          "Spare parts path is highly exposed because route fragility and inventory vulnerability are both elevated.",
		PrimaryMechanism: "inventory_vulnerability",
		Criticality:      0.91,
		StressScore:      0.74,
		Products: []horizonmodels.SupplyChainOverlayProduct{
			{Ref: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Label: "Oilfield spare parts", Score: 0.91, LinkCount: 2, PathCount: 1},
		},
		Bottlenecks: []horizonmodels.SupplyChainOverlayNode{
			{Ref: "/fac/eg/suez-canal", Label: "Suez Canal", Role: "transport", Score: 0.83, PathCount: 1},
		},
		DriverMetrics: []horizonmodels.SupplyChainOverlayMetric{
			{Key: "lead_time_days", Label: "lead time", Value: 30, Scope: "link", Importance: 0.88},
		},
		Legs: []horizonmodels.SupplyChainLegRiskOverlay{
			{
				SegmentIndex:   0,
				ProductRef:     "/product/component/mechanical/manufacturing/oilfield-spare-parts",
				ProductLabel:   "Oilfield spare parts",
				SupplierRef:    "/fac/ae/jebel-ali-logistics-center",
				CustomerRef:    "/fac/ru/slb-tyumen-factory",
				TransportViaRefs: []string{"/fac/eg/suez-canal"},
				Criticality:    0.91,
			},
		},
	}

	artifactB := &horizonmodels.MaterializedArtifact{
		ID:             "path-b",
		TenantID:       "global",
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey:   "/supply-chain/global/fac_ae_mi-swaco-jebel-ali-plant/product_finished_chemical_manufacturing_drilling-fluid-chemicals/fac_ru_rosneft-general-operations",
		DisplayName:    "Chemicals path",
		PathStressScore: 0.68,
	}
	artifactB.RiskOverlay = &horizonmodels.SupplyChainPathRiskOverlay{
		Scope:            "path",
		Summary:          "Chemicals path is exposed through customs friction and route fragility.",
		PrimaryMechanism: "route_fragility",
		Criticality:      0.84,
		StressScore:      0.68,
		Products: []horizonmodels.SupplyChainOverlayProduct{
			{Ref: "/product/finished/chemical/manufacturing/drilling-fluid-chemicals", Label: "Drilling fluid chemicals", Score: 0.84, LinkCount: 1, PathCount: 1},
		},
		Bottlenecks: []horizonmodels.SupplyChainOverlayNode{
			{Ref: "/fac/ru/novorossiysk-seaport", Label: "Novorossiysk Seaport", Role: "transport", Score: 0.79, PathCount: 1},
		},
		DriverMetrics: []horizonmodels.SupplyChainOverlayMetric{
			{Key: "customs_clearance_days", Label: "customs clearance", Value: 9, Scope: "link", Importance: 0.72},
		},
		Legs: []horizonmodels.SupplyChainLegRiskOverlay{
			{
				SegmentIndex:   0,
				ProductRef:     "/product/finished/chemical/manufacturing/drilling-fluid-chemicals",
				ProductLabel:   "Drilling fluid chemicals",
				SupplierRef:    "/fac/ae/mi-swaco-jebel-ali-plant",
				CustomerRef:    "/fac/ru/rosneft-general-operations",
				TransportViaRefs: []string{"/fac/ru/novorossiysk-seaport"},
				Criticality:    0.84,
			},
		},
	}

	center := &horizonmodels.HorizonFragment{
		ID:            "frag-tyumen",
		CanonicalPath: "/fac/ru/slb-tyumen-factory",
		Labels:        map[string]string{"en": "SLB Tyumen Factory"},
	}
	fragmentMap := map[string]*horizonmodels.HorizonFragment{
		center.ID: center,
	}

	objectOverlay := buildSupplyChainObjectRiskOverlay(center, []*horizonmodels.MaterializedArtifact{artifactA, artifactB}, fragmentMap)
	if objectOverlay == nil {
		t.Fatal("object overlay is nil")
	}
	if objectOverlay.PrimaryMechanism == "" {
		t.Fatal("object primary mechanism empty")
	}
	if len(objectOverlay.TopProducts) == 0 {
		t.Fatal("expected top products")
	}
	if len(objectOverlay.TopPaths) == 0 {
		t.Fatal("expected top paths")
	}
	if strings.TrimSpace(objectOverlay.Summary) == "" {
		t.Fatal("object summary empty")
	}

	lensOverlay := buildSupplyChainLensRiskOverlay([]*horizonmodels.MaterializedArtifact{artifactA, artifactB})
	if lensOverlay == nil {
		t.Fatal("lens overlay is nil")
	}
	if lensOverlay.TotalPaths != 2 {
		t.Fatalf("totalPaths=%d want 2", lensOverlay.TotalPaths)
	}
	if len(lensOverlay.TopProducts) == 0 || len(lensOverlay.TopBottlenecks) == 0 {
		t.Fatalf("lens overlay missing products/bottlenecks: %+v", lensOverlay)
	}
	if strings.TrimSpace(lensOverlay.Summary) == "" {
		t.Fatal("lens summary empty")
	}
}

func TestAnnotateSupplyChainArtifactRiskOverlay_NormalizesNilLikeMechanisms(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		ID:         "path-nil",
		TenantID:   "global",
		Kind:       horizonmodels.MaterializedKindSupplyChainPath,
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		RiskOverlay: &horizonmodels.SupplyChainPathRiskOverlay{
			Scope:            "path",
			Summary:          "Oilfield drilling equipment carries moderate route exposure led by <nil>.",
			PrimaryMechanism: "<nil>",
			Products: []horizonmodels.SupplyChainOverlayProduct{
				{Ref: "/product/component/mechanical/manufacturing/oilfield-drilling-equipment", Label: "Oilfield drilling equipment", Score: 0.7},
			},
			Legs: []horizonmodels.SupplyChainLegRiskOverlay{
				{
					SegmentIndex:     0,
					Summary:          "Oilfield drilling equipment is highly exposed because <nil> is elevated on this leg.",
					PrimaryMechanism: "<nil>",
					ProductRef:       "/product/component/mechanical/manufacturing/oilfield-drilling-equipment",
					ProductLabel:     "Oilfield drilling equipment",
				},
			},
		},
	}

	annotateSupplyChainArtifactRiskOverlay(artifact)

	if artifact.RiskOverlay == nil {
		t.Fatal("risk overlay is nil")
	}
	if artifact.RiskOverlay.PrimaryMechanism != "" {
		t.Fatalf("primary mechanism=%q want empty", artifact.RiskOverlay.PrimaryMechanism)
	}
	if len(artifact.RiskOverlay.Legs) == 0 || artifact.RiskOverlay.Legs[0].PrimaryMechanism != "" {
		t.Fatalf("leg primary mechanism=%q want empty", artifact.RiskOverlay.Legs[0].PrimaryMechanism)
	}
	if strings.Contains(strings.ToLower(artifact.RiskOverlay.Summary), "<nil>") {
		t.Fatalf("summary still contains nil token: %q", artifact.RiskOverlay.Summary)
	}
	if strings.Contains(strings.ToLower(artifact.RiskOverlay.Legs[0].Summary), "<nil>") {
		t.Fatalf("leg summary still contains nil token: %q", artifact.RiskOverlay.Legs[0].Summary)
	}
}

func overlayIntPtr(v int) *int { return &v }
