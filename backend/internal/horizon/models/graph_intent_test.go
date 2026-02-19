package models

import (
	"testing"
)

func TestGraphIntentPlanResponseNormalizeAndValidate_NodeTypeValidation(t *testing.T) {
	plan := GraphIntentPlanResponse{
		Version:                GraphIntentVersionV1,
		NormalizedEnglishQuery: "Build governance structure",
		IntentClass:            GraphIntentClassCreate,
		Confidence:             0.8,
		Create: GraphIntentCreateBlock{
			Nodes: []GraphIntentNodeSpec{
				{
					TempID: "n1",
					Label:  "Unknown",
					Type:   "NOT_A_REAL_NODE_TYPE",
				},
			},
		},
	}

	if err := plan.NormalizeAndValidate(); err == nil {
		t.Fatalf("expected validation error for invalid node type")
	}
}

func TestGraphIntentPlanResponseMapToActions(t *testing.T) {
	renameTo := string(DependencyTypeRenamedTo)
	forward := "forward"
	newLabel := "Renamed Entity"
	newType := string(FragmentTypeOrganization)
	plan := GraphIntentPlanResponse{
		Version:                GraphIntentVersionV1,
		NormalizedEnglishQuery: "Build and adjust the graph",
		IntentClass:            GraphIntentClassMixed,
		Confidence:             0.9,
		Create: GraphIntentCreateBlock{
			Groupings: []GraphIntentGrouping{
				{
					GroupingID:   "grp1",
					GroupingKind: GraphIntentGroupingSocietalStructure,
					Title:        "Societal map",
					Entities: []GraphIntentNodeSpec{
						{TempID: "n1", Label: "Group A", Type: string(FragmentTypeCommunity)},
						{TempID: "n2", Label: "Person B", Type: string(FragmentTypePerson)},
					},
					Relations: []GraphIntentEdgeSpec{
						{TempID: "e1", FromTempID: "n2", ToTempID: "n1", RelationType: string(DependencyTypeMemberOf), Direction: forward},
					},
				},
			},
		},
		Update: GraphIntentUpdateBlock{
			NodePatches: []GraphIntentNodePatch{
				{
					PatchID: "np1",
					NodeID:  "node-1",
					Label:   &newLabel,
					Type:    &newType,
				},
			},
			EdgePatches: []GraphIntentEdgePatch{
				{
					PatchID:      "ep1",
					EdgeID:       "edge-1",
					RelationType: &renameTo,
				},
			},
			RelationRewrite: []GraphIntentRelationRewrite{
				{
					RewriteID: "rw1",
					Match: GraphIntentRelationMatch{
						RelationType: string(DependencyTypeDependsOn),
					},
					ReplaceWith: GraphIntentRelationReplacement{
						RelationType: string(DependencyTypeSuppliesTo),
						Direction:    forward,
					},
				},
			},
		},
		Transform: GraphIntentTransformBlock{
			DeriveChains: []GraphIntentDeriveChain{
				{
					ChainID:    "c1",
					FromNodeID: "A",
					ToNodeID:   "B",
					Constraints: GraphIntentChainConstraints{
						AllowedRelationTypes: []string{string(DependencyTypeSuppliesTo)},
						MaxHops:              4,
					},
				},
			},
			Restructure: []GraphIntentRestructure{
				{
					RestructureID: "r1",
					ScopeNodeIDs:  []string{"A", "B"},
					Mode:          "pack",
				},
			},
		},
	}

	if err := plan.NormalizeAndValidate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	actionPlan := plan.MapToActions()
	if actionPlan == nil {
		t.Fatalf("MapToActions returned nil")
	}
	if len(actionPlan.Actions) != 9 {
		t.Fatalf("expected 9 actions, got %d", len(actionPlan.Actions))
	}

	expectedKinds := []string{
		"create_grouping",
		"create_node",
		"create_node",
		"create_edge",
		"update_node",
		"update_edge",
		"rewrite_relation",
		"derive_chain",
		"restructure_graph",
	}
	for i, kind := range expectedKinds {
		if actionPlan.Actions[i].Kind != kind {
			t.Fatalf("action[%d] kind mismatch: got=%s want=%s", i, actionPlan.Actions[i].Kind, kind)
		}
	}
}

func TestGraphIntentSchemasDraft07_ContainSocietalAndRelationEnums(t *testing.T) {
	planSchema := GraphIntentPlanResponseSchemaDraft07()
	if planSchema["$schema"] != graphIntentDraft07SchemaURI {
		t.Fatalf("unexpected $schema: %v", planSchema["$schema"])
	}

	defs, ok := planSchema["definitions"].(map[string]interface{})
	if !ok {
		t.Fatalf("definitions are missing or invalid")
	}

	grouping, ok := defs["grouping"].(map[string]interface{})
	if !ok {
		t.Fatalf("grouping definition is missing")
	}
	groupingProps, ok := grouping["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("grouping properties are missing")
	}
	groupingKind, ok := groupingProps["groupingKind"].(map[string]interface{})
	if !ok {
		t.Fatalf("groupingKind schema is missing")
	}
	if !stringInAnySlice("societal_structure", groupingKind["enum"]) {
		t.Fatalf("societal_structure is missing from groupingKind enum")
	}

	edgeSpec, ok := defs["edgeSpec"].(map[string]interface{})
	if !ok {
		t.Fatalf("edgeSpec definition is missing")
	}
	edgeProps, ok := edgeSpec["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("edgeSpec properties are missing")
	}
	relationType, ok := edgeProps["relationType"].(map[string]interface{})
	if !ok {
		t.Fatalf("edgeSpec relationType schema is missing")
	}
	if !stringInAnySlice(string(DependencyTypeRegulates), relationType["enum"]) {
		t.Fatalf("REGULATES is missing from relationType enum")
	}
	if !stringInAnySlice(GraphIntentRelationTypeAlignsWith, relationType["enum"]) {
		t.Fatalf("ALIGNS_WITH is missing from relationType enum")
	}
}

func TestGraphIntentApplyRequestNormalizeAndValidate_RequiresIdempotencyKey(t *testing.T) {
	req := GraphIntentApplyRequest{
		Version: GraphIntentVersionV1,
		Plan: GraphIntentPlanResponse{
			Version:                GraphIntentVersionV1,
			NormalizedEnglishQuery: "Build map",
			IntentClass:            GraphIntentClassCreate,
			Confidence:             0.5,
			Create:                 GraphIntentCreateBlock{},
			Update:                 GraphIntentUpdateBlock{},
			Transform:              GraphIntentTransformBlock{},
		},
	}
	if err := req.NormalizeAndValidate(); err == nil {
		t.Fatalf("expected idempotencyKey validation error")
	}
}

func stringInAnySlice(needle string, raw interface{}) bool {
	list, ok := raw.([]string)
	if ok {
		for _, item := range list {
			if item == needle {
				return true
			}
		}
		return false
	}
	anyList, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range anyList {
		if s, ok := item.(string); ok && s == needle {
			return true
		}
	}
	return false
}
