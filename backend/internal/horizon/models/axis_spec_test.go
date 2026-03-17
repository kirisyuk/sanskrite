package models

import "testing"

func TestDefaultAxisSpecs_PhysicalFlowSupplyIncludesOrganizationControlEdges(t *testing.T) {
	spec := DefaultAxisSpecs()[AxisPhysicalFlowSupply]
	want := map[string]bool{
		"PRODUCES":    true,
		"CONTROLS":    true,
		"MANAGES":     true,
		"OPERATES_IN": true,
	}
	got := map[string]bool{}
	for _, edgeType := range spec.StrongEdgeTypes {
		got[edgeType] = true
	}
	for edgeType := range want {
		if !got[edgeType] {
			t.Fatalf("physical_flow/supply strong edges missing %s: %v", edgeType, spec.StrongEdgeTypes)
		}
	}
}

func TestDefaultPropagationProfiles_PhysicalFlowSupplyIncludesBidirectionalSupplierTraversal(t *testing.T) {
	profile := DefaultPropagationProfiles()[AxisPhysicalFlowSupply]
	foundSupplies := false
	foundProduces := false
	foundOperatesIn := false
	for _, spec := range profile.StrongEdges {
		switch spec.Type {
		case "SUPPLIES_TO":
			foundSupplies = spec.Direction == "both"
		case "PRODUCES":
			foundProduces = spec.Direction == "source_to_target"
		case "OPERATES_IN":
			foundOperatesIn = spec.Direction == "source_to_target"
		}
	}
	if !foundSupplies {
		t.Fatalf("physical_flow/supply profile must traverse SUPPLIES_TO in both directions")
	}
	if !foundProduces {
		t.Fatalf("physical_flow/supply profile must include PRODUCES source_to_target")
	}
	if !foundOperatesIn {
		t.Fatalf("physical_flow/supply profile must include OPERATES_IN source_to_target")
	}
}
