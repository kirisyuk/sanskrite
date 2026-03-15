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
		Payload: models.BuildSupplyChainAssemblyGraphPayload(
			[]string{"/product/device/electronics/smartphone"},
			[]models.SupplyChainAssemblyNode{
				{Type: "product", Ref: "/product/component/battery-pack"},
				{Type: "product", Ref: "/product/component/application-processor"},
				{Type: "product", Ref: "/product/device/electronics/smartphone"},
			},
			[]models.SupplyChainAssemblyNode{
				{Type: "org", Ref: "/org/global/assembler"},
			},
			[]models.SupplyChainAssemblySupplyEdge{
				{SupplierRef: "/org/global/battery-maker", ProductRef: "/product/component/battery-pack", CustomerRef: "/org/global/assembler", SupplyLinkCanonicalPath: "/supply-link/global/battery"},
				{SupplierRef: "/org/global/chip-fab", ProductRef: "/product/component/application-processor", CustomerRef: "/org/global/assembler", SupplyLinkCanonicalPath: "/supply-link/global/chip"},
			},
			[]models.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/component/battery-pack", ToProductRef: "/product/device/electronics/smartphone", Relation: "TRANSFORMS_INTO"},
				{FromProductRef: "/product/component/application-processor", ToProductRef: "/product/device/electronics/smartphone", Relation: "TRANSFORMS_INTO"},
			},
			[]models.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/component/battery-pack", ToProductRef: "/product/device/electronics/smartphone", Relation: "ASSEMBLES_INTO"},
				{FromProductRef: "/product/component/application-processor", ToProductRef: "/product/device/electronics/smartphone", Relation: "ASSEMBLES_INTO"},
			},
			nil,
			&models.SupplyChainAssemblyCoverage{ProductsN: 3, ActorsN: 1, TransformationsN: 2, DerivedPathsN: 2},
			[]string{"display_branch_missing"},
			[]string{"/supply-chain/global/path-battery", "/supply-chain/global/path-chip"},
			nil,
		),
	}
	summary := map[string]interface{}{
		"status":        "partial",
		"productsN":     float64(3),
		"actorsN":       float64(1),
		"transformsN":   float64(2),
		"derivedPathsN": float64(2),
		"openGapsN":     float64(1),
		"artifactKey":   artifact.CanonicalKey,
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
