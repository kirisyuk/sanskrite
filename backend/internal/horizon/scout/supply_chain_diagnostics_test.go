package scout

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestDiagnoseSupplyChainAssembly_PassesForConvergingAssembly(t *testing.T) {
	artifact := &models.MaterializedArtifact{
		ID:           "assembly-1",
		TenantID:     "global",
		Kind:         models.MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: "/supply-chain-assembly/global/test-smartphone",
		PathStatus:   "partial",
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Payload: models.BuildSupplyChainAssemblyGraphPayloadDetailed(models.SupplyChainAssemblyGraphPayloadOptions{
			AnchorSet:       []string{"/product/device/electronics/smartphone"},
			SourceAnchors:   []string{"/product/device/electronics/smartphone"},
			ResolvedAnchors: []string{"/product/device/electronics/smartphone"},
			AnchorProducts:  []string{"/product/device/electronics/smartphone"},
			ProductNodes: []models.SupplyChainAssemblyNode{
				{Type: "product", Ref: "/product/component/battery-pack"},
				{Type: "product", Ref: "/product/component/application-processor"},
				{Type: "product", Ref: "/product/device/electronics/smartphone"},
			},
			ActorNodes: []models.SupplyChainAssemblyNode{
				{Type: "fac", Ref: "/fac/global/assembly-plant"},
			},
			SupplyEdges: []models.SupplyChainAssemblySupplyEdge{
				{SupplierRef: "/org/global/battery-maker", ProductRef: "/product/component/battery-pack", CustomerRef: "/fac/global/assembly-plant", SupplyLinkCanonicalPath: "/supply-link/global/battery", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-battery", TransportViaRefs: []string{"/fac/cn/shenzhen/port-yantian"}},
				{SupplierRef: "/org/global/chip-fab", ProductRef: "/product/component/application-processor", CustomerRef: "/fac/global/assembly-plant", SupplyLinkCanonicalPath: "/supply-link/global/chip", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-chip"},
			},
			TransformEdges: []models.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/component/battery-pack", ToProductRef: "/product/device/electronics/smartphone", Relation: "TRANSFORMS_INTO", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-battery"},
				{FromProductRef: "/product/component/application-processor", ToProductRef: "/product/device/electronics/smartphone", Relation: "TRANSFORMS_INTO", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-chip"},
			},
			AssemblyEdges: []models.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/component/battery-pack", ToProductRef: "/product/device/electronics/smartphone", Relation: "ASSEMBLES_INTO", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-battery"},
				{FromProductRef: "/product/component/application-processor", ToProductRef: "/product/device/electronics/smartphone", Relation: "ASSEMBLES_INTO", BranchClass: "component_chain", PriorityTier: "primary", PrimaryBranchID: "branch-chip"},
			},
			TopRoutes:          []string{"/fac/cn/shenzhen/port-yantian"},
			TopProductFamilies: []string{"battery pack", "application processor"},
			PrimaryBranches: []models.SupplyChainAssemblyBranchSummary{
				{ID: "branch-battery", BranchClass: "component_chain", PriorityTier: "primary", Title: "battery pack branch", ProductRefs: []string{"/product/component/battery-pack"}, ActorRefs: []string{"/org/global/battery-maker", "/fac/global/assembly-plant"}, RouteRefs: []string{"/fac/cn/shenzhen/port-yantian"}, DecisionValueScore: 0.88},
				{ID: "branch-chip", BranchClass: "component_chain", PriorityTier: "primary", Title: "application processor branch", ProductRefs: []string{"/product/component/application-processor"}, ActorRefs: []string{"/org/global/chip-fab", "/fac/global/assembly-plant"}, DecisionValueScore: 0.81},
			},
			Coverage:        &models.SupplyChainAssemblyCoverage{ProductsN: 3, ActorsN: 1, TransformationsN: 2, DerivedPathsN: 2, PrimaryBranchesN: 2, PrimaryProductsCoveredN: 2, RouteBoundPrimaryEdgesN: 1},
			OpenGaps:        []string{"display_branch_missing"},
			DerivedPathKeys: []string{"/supply-chain/global/path-battery", "/supply-chain/global/path-chip"},
		}),
	}
	summary := map[string]interface{}{
		"status":             "partial",
		"productsN":          float64(3),
		"actorsN":            float64(1),
		"transformsN":        float64(2),
		"derivedPathsN":      float64(2),
		"openGapsN":          float64(1),
		"primaryBranchesN":   float64(2),
		"secondaryBranchesN": float64(0),
		"supportBranchesN":   float64(0),
		"artifactKey":        artifact.CanonicalKey,
		"anchorProducts": []interface{}{
			"/product/device/electronics/smartphone",
		},
	}
	pathArtifacts := []*models.MaterializedArtifact{
		{Kind: models.MaterializedKindSupplyChainPath, CanonicalKey: "/supply-chain/global/path-battery"},
		{Kind: models.MaterializedKindSupplyChainPath, CanonicalKey: "/supply-chain/global/path-chip"},
	}

	report, err := DiagnoseSupplyChainAssembly(artifact, pathArtifacts, summary, DefaultSupplyChainAssemblyDiagnosticOptions())
	if err != nil {
		t.Fatalf("DiagnoseSupplyChainAssembly() error = %v", err)
	}
	if !report.Pass {
		t.Fatalf("report.Pass=false, errors=%v", report.Errors)
	}
	if got := report.AnchorFanin["/product/device/electronics/smartphone"]; got != 2 {
		t.Fatalf("anchor fan-in=%d, want 2", got)
	}
	if report.PrimaryBranchesN != 2 {
		t.Fatalf("report.PrimaryBranchesN=%d, want 2", report.PrimaryBranchesN)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings=%v, want none", report.Warnings)
	}
}

func TestDiagnoseSupplyChainAssembly_FailsWhenDerivedPathMissing(t *testing.T) {
	artifact := &models.MaterializedArtifact{
		TenantID:     "global",
		Kind:         models.MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: "/supply-chain-assembly/global/test-smartphone",
		PathStatus:   "complete",
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Payload: models.BuildSupplyChainAssemblyGraphPayload(
			[]string{"/product/device/electronics/smartphone"},
			[]models.SupplyChainAssemblyNode{
				{Type: "product", Ref: "/product/component/battery-pack"},
				{Type: "product", Ref: "/product/device/electronics/smartphone"},
			},
			nil,
			[]models.SupplyChainAssemblySupplyEdge{
				{SupplierRef: "/org/global/battery-maker", ProductRef: "/product/component/battery-pack", CustomerRef: "/org/global/assembler", SupplyLinkCanonicalPath: "/supply-link/global/battery"},
			},
			[]models.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/component/battery-pack", ToProductRef: "/product/device/electronics/smartphone", Relation: "TRANSFORMS_INTO"},
			},
			nil,
			nil,
			&models.SupplyChainAssemblyCoverage{ProductsN: 2, TransformationsN: 1, DerivedPathsN: 1},
			nil,
			[]string{"/supply-chain/global/path-battery"},
			nil,
		),
	}

	report, err := DiagnoseSupplyChainAssembly(artifact, nil, nil, DefaultSupplyChainAssemblyDiagnosticOptions())
	if err != nil {
		t.Fatalf("DiagnoseSupplyChainAssembly() error = %v", err)
	}
	if report.Pass {
		t.Fatalf("report.Pass=true, want false")
	}
	if len(report.MissingDerivedPathKeys) != 1 || report.MissingDerivedPathKeys[0] != "/supply-chain/global/path-battery" {
		t.Fatalf("missing derived path keys=%v", report.MissingDerivedPathKeys)
	}
}

func TestDiagnoseSupplyChainAssembly_FailsWhenNoPrimaryBranches(t *testing.T) {
	artifact := &models.MaterializedArtifact{
		TenantID:     "global",
		Kind:         models.MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: "/supply-chain-assembly/global/test-retail",
		PathStatus:   "partial",
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Payload: models.BuildSupplyChainAssemblyGraphPayloadDetailed(models.SupplyChainAssemblyGraphPayloadOptions{
			AnchorSet:       []string{"/org/kz/meloman-marwin"},
			SourceAnchors:   []string{"/org/kz/meloman-marwin"},
			ResolvedAnchors: []string{"/org/kz/meloman-marwin"},
			ActorNodes: []models.SupplyChainAssemblyNode{
				{Type: "org", Ref: "/org/kz/meloman-marwin"},
				{Type: "fac", Ref: "/fac/kz/astana/distribution-center"},
			},
			TopDistributionNodes: []string{"/fac/kz/astana/distribution-center"},
			Coverage:             &models.SupplyChainAssemblyCoverage{ActorsN: 2, SupportBranchesN: 1},
			SuppressedSupportBranches: []models.SupplyChainAssemblyBranchSummary{
				{ID: "branch-packaging", BranchClass: "packaging_support", PriorityTier: "supporting", ActorRefs: []string{"/fac/kz/astana/distribution-center"}},
			},
		}),
	}

	report, err := DiagnoseSupplyChainAssembly(artifact, nil, nil, DefaultSupplyChainAssemblyDiagnosticOptions())
	if err != nil {
		t.Fatalf("DiagnoseSupplyChainAssembly() error = %v", err)
	}
	if report.Pass {
		t.Fatalf("report.Pass=true, want false")
	}
	if report.PrimaryBranchesN != 0 {
		t.Fatalf("report.PrimaryBranchesN=%d, want 0", report.PrimaryBranchesN)
	}
}
