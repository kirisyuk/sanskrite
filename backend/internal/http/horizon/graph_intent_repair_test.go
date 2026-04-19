package horizon

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestRepairGraphIntentPlanWithResolvers_RewritesProductPathFromLabel(t *testing.T) {
	handler := &HorizonHandlers{}
	lens := &horizonmodels.Lens{TenantID: "global"}
	plan := &horizonmodels.GraphIntentPlanResponse{
		Version:                horizonmodels.GraphIntentVersionV1,
		NormalizedEnglishQuery: "Add battery cell supplier",
		IntentClass:            horizonmodels.GraphIntentClassCreate,
		Confidence:             0.9,
		Create: horizonmodels.GraphIntentCreateBlock{
			Nodes: []horizonmodels.GraphIntentNodeSpec{
				{
					TempID:            "n1",
					Label:             "Battery Cell",
					Type:              string(FragmentTypeProduct),
					CanonicalPathHint: "/product/component/energy/storage/battery-cell",
				},
			},
		},
	}

	handler.repairGraphIntentPlanWithResolvers(context.Background(), lens, horizonmodels.GraphIntentSnapshot{}, "Add battery cell supplier", plan)

	got := plan.Create.Nodes[0].CanonicalPathHint
	want := "/product/component/electronic/manufacturing/battery-cell"
	if got != want {
		t.Fatalf("expected repaired product path %q, got %q", want, got)
	}
}

func TestRepairGraphIntentPlanWithResolvers_ReusesSnapshotObjectPath(t *testing.T) {
	handler := &HorizonHandlers{}
	lens := &horizonmodels.Lens{TenantID: "global"}
	snapshot := horizonmodels.GraphIntentSnapshot{
		Nodes: []horizonmodels.GraphIntentSnapshotNode{
			{
				NodeID:        "cp_mercedes",
				CanonicalPath: "/org/de/mercedes-benz-group",
				Label:         "Mercedes Benz Group",
				Type:          string(FragmentTypeOrganization),
			},
		},
	}
	plan := &horizonmodels.GraphIntentPlanResponse{
		Version:                horizonmodels.GraphIntentVersionV1,
		NormalizedEnglishQuery: "Update Mercedes",
		IntentClass:            horizonmodels.GraphIntentClassCreate,
		Confidence:             0.8,
		Create: horizonmodels.GraphIntentCreateBlock{
			Nodes: []horizonmodels.GraphIntentNodeSpec{
				{
					TempID:            "n1",
					Label:             "Mercedes Benz Group",
					Type:              string(FragmentTypeOrganization),
					CanonicalPathHint: "/org/de/mercedes-benz",
				},
			},
		},
	}

	handler.repairGraphIntentPlanWithResolvers(context.Background(), lens, snapshot, "Update Mercedes", plan)

	got := plan.Create.Nodes[0].CanonicalPathHint
	want := "/org/de/mercedes-benz-group"
	if got != want {
		t.Fatalf("expected snapshot canonical path %q, got %q", want, got)
	}
}

func TestRepairGraphIntentPlanWithResolvers_NormalizesMalformedDomainPath(t *testing.T) {
	handler := &HorizonHandlers{}
	lens := &horizonmodels.Lens{TenantID: "global"}
	plan := &horizonmodels.GraphIntentPlanResponse{
		Version:                horizonmodels.GraphIntentVersionV1,
		NormalizedEnglishQuery: "Update risk metrics for Mercedes-Benz Group by including battery cell supply dependency",
		IntentClass:            horizonmodels.GraphIntentClassCreate,
		Confidence:             0.8,
		Create: horizonmodels.GraphIntentCreateBlock{
			Nodes: []horizonmodels.GraphIntentNodeSpec{
				{
					TempID:            "n1",
					Label:             "Battery Cell Supply Dependency",
					// Legacy DOMAIN wire token (no AH FragmentClass equivalent).
					Type: "DOMAIN",
					CanonicalPathHint: "/domain/v1/supply-chain/geo/de/battery-cells",
				},
			},
		},
	}

	handler.repairGraphIntentPlanWithResolvers(context.Background(), lens, horizonmodels.GraphIntentSnapshot{}, plan.NormalizedEnglishQuery, plan)

	got := plan.Create.Nodes[0].CanonicalPathHint
	want := "/domain/v1/supply-chain/geo/de"
	if got != want {
		t.Fatalf("expected repaired domain path %q, got %q", want, got)
	}
}
