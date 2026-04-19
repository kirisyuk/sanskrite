package ah

import "testing"

// TestGeneratedShape locks in the high-level shape of the generated taxonomy
// so structural surprises in the SoT (e.g. an enum disappearing) are caught
// at backend test time rather than at runtime. Numeric thresholds are loose
// on purpose; the goal is to flag *catastrophic* shrinkage, not to enumerate.
func TestGeneratedShape(t *testing.T) {
	if SchemaVersion == "" {
		t.Fatal("SchemaVersion is empty; codegen header is broken")
	}

	if got := len(FragmentClasses); got < 10 {
		t.Fatalf("FragmentClasses shrank dramatically: got %d, want >= 10", got)
	}
	if _, ok := StructuralFragmentClasses[FragmentClassOrg]; !ok {
		t.Fatal("StructuralFragmentClasses missing FragmentClassOrg")
	}
	if _, ok := DecisionLayerFragmentClasses[FragmentClassDecision]; !ok {
		t.Fatal("DecisionLayerFragmentClasses missing FragmentClassDecision")
	}
	if _, ok := DecisionLayerFragmentClasses[FragmentClassMechanism]; !ok {
		t.Fatal("DecisionLayerFragmentClasses missing FragmentClassMechanism")
	}

	if got := len(RelationNames); got < 30 {
		t.Fatalf("RelationNames shrank dramatically: got %d, want >= 30", got)
	}
	for _, n := range RelationNames {
		if _, ok := RelationAllowedLayers[n]; !ok {
			t.Errorf("RelationAllowedLayers missing entry for %s", n)
		}
	}

	// Decision layer relations must round-trip into RelationLayerSection bucket.
	for _, want := range []RelationName{
		RelationNameGatekeeps, RelationNameWields, RelationNameEnactedVia,
		RelationNameImposes, RelationNameInfluences,
	} {
		got, ok := RelationLayerSection[want]
		if !ok {
			t.Errorf("RelationLayerSection missing %s", want)
			continue
		}
		if got != LayerSectionDecisionLayer {
			t.Errorf("%s expected layer section %s, got %s", want, LayerSectionDecisionLayer, got)
		}
	}

	if rows := RelationEndpointKinds[RelationNameSupplies]; len(rows) == 0 {
		t.Error("RelationEndpointKinds is missing rows for `supplies`")
	}

	if got := len(AssuranceTypeValues); got < 3 {
		t.Errorf("AssuranceTypeValues shrank: got %d, want >= 3", got)
	}
	if got := len(DecisionSubtypeValues); got < 3 {
		t.Errorf("DecisionSubtypeValues shrank: got %d, want >= 3", got)
	}
	if got := len(InfluenceKindValues); got < 3 {
		t.Errorf("InfluenceKindValues shrank: got %d, want >= 3", got)
	}

	if got := len(AttributeSpecs); got < 30 {
		t.Errorf("AttributeSpecs shrank: got %d, want >= 30", got)
	}

	// Spot-check a few critical attribute keys exist as constants.
	for _, want := range []string{
		AttrKeyProductFragmentId,
		AttrKeyRouteFragmentId,
		AttrKeyGatedByDecisionFragmentId,
		AttrKeyInfluencedByDecisionFragmentId,
		AttrKeyInfluenceKind,
		AttrKeyAssuranceType,
		AttrKeyMobileAssetKind,
		AttrKeyDisruptionKind,
		AttrKeySchemaVersion,
	} {
		if want == "" {
			t.Error("attribute key constant is empty string")
		}
	}

	if len(Incoterms2020) < 5 {
		t.Errorf("Incoterms2020 shrank: got %d, want >= 5", len(Incoterms2020))
	}

	if _, ok := SanctionsRegimes["BIS_EAR_ENTITY_LIST"]; !ok {
		t.Error("SanctionsRegimes is missing BIS_EAR_ENTITY_LIST")
	}
}
