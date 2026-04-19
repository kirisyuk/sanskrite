package models

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestDefaultAxisSpecs_PhysicalFlowSupplyIncludesOrganizationControlEdges(t *testing.T) {
	spec := DefaultAxisSpecs()[AxisPhysicalFlowSupply]
	want := map[string]bool{
		string(ah.RelationNameProduces):     true,
		string(ah.RelationNameControlsVia):  true, // legacy CONTROLS
		string(ah.RelationNameManagedBy):    true, // legacy MANAGES (reversed)
		string(ah.RelationNameRequiresCapability): true, // legacy DEPENDS_ON(product->site)
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
	foundRequiresCapability := false
	foundLegConnects := false
	for _, spec := range profile.StrongEdges {
		switch spec.Type {
		case string(ah.RelationNameSupplies):
			foundSupplies = spec.Direction == "both"
		case string(ah.RelationNameProduces):
			foundProduces = spec.Direction == "source_to_target"
		case string(ah.RelationNameRequiresCapability):
			foundRequiresCapability = spec.Direction == "target_to_source"
		case string(ah.RelationNameLegConnects):
			foundLegConnects = spec.Direction == "both"
		}
	}
	if !foundSupplies {
		t.Fatalf("physical_flow/supply profile must traverse supplies in both directions")
	}
	if !foundProduces {
		t.Fatalf("physical_flow/supply profile must include produces source_to_target")
	}
	if !foundRequiresCapability {
		t.Fatalf("physical_flow/supply profile must include requires_capability target_to_source (legacy DEPENDS_ON product->site)")
	}
	if !foundLegConnects {
		t.Fatalf("physical_flow/supply profile must include leg_connects both (route overlay)")
	}
}

// TestDefaultPropagationProfiles_AllStrongEdgesAreKnownAHRelations is the
// guard that locks in the AH SoT contract: every entry in every default
// propagation profile MUST be a known ah.RelationName. Legacy UPPER_SNAKE
// strings will fail the lookup.
func TestDefaultPropagationProfiles_AllStrongEdgesAreKnownAHRelations(t *testing.T) {
	for axis, profile := range DefaultPropagationProfiles() {
		for i, spec := range profile.StrongEdges {
			if !ah.IsKnownRelation(ah.RelationName(spec.Type)) {
				t.Fatalf("axis %s: StrongEdges[%d].Type=%q is not a known ah.RelationName", axis, i, spec.Type)
			}
		}
		for i, tr := range profile.Transformations {
			if !ah.IsKnownRelation(ah.RelationName(tr.OnEdgeType)) {
				t.Fatalf("axis %s: Transformations[%d].OnEdgeType=%q is not a known ah.RelationName", axis, i, tr.OnEdgeType)
			}
		}
	}
}

// TestDefaultAxisSpecs_AllStrongEdgeTypesAreKnownAHRelations is the
// matching guard for the AxisSpec mirror.
func TestDefaultAxisSpecs_AllStrongEdgeTypesAreKnownAHRelations(t *testing.T) {
	for axis, spec := range DefaultAxisSpecs() {
		for i, edgeType := range spec.StrongEdgeTypes {
			if !ah.IsKnownRelation(ah.RelationName(edgeType)) {
				t.Fatalf("axis %s: StrongEdgeTypes[%d]=%q is not a known ah.RelationName", axis, i, edgeType)
			}
		}
	}
}
