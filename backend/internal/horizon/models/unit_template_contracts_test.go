package models

import "testing"

func TestApplyUnitTemplatePairByQualifier_RouteDefault(t *testing.T) {
	src, dst, key, adjusted := ApplyUnitTemplatePairByQualifier(
		ObjectQualifierRoute,
		DependencyTypeSuppliesTo,
		UnitCodeUnclassified,
		UnitCodeUnclassified,
	)
	if !adjusted {
		t.Fatalf("expected adjustment for unclassified route pair")
	}
	if key != UnitTemplateRouteFlow {
		t.Fatalf("template key=%q want=%q", key, UnitTemplateRouteFlow)
	}
	if src != UnitCodeLogistics || dst != UnitCodeLogistics {
		t.Fatalf("pair=%q->%q want logistics->logistics", src, dst)
	}
}

func TestApplyUnitTemplatePairByQualifier_SupplyKeepsAllowed(t *testing.T) {
	src, dst, key, adjusted := ApplyUnitTemplatePairByQualifier(
		ObjectQualifierSupply,
		DependencyTypeDependsOn,
		UnitCodeCoreOps,
		UnitCodeCoreOps,
	)
	if adjusted {
		t.Fatalf("did not expect adjustment for allowed supply pair")
	}
	if key != UnitTemplateSupplyFlow {
		t.Fatalf("template key=%q want=%q", key, UnitTemplateSupplyFlow)
	}
	if src != UnitCodeCoreOps || dst != UnitCodeCoreOps {
		t.Fatalf("pair=%q->%q want core_ops->core_ops", src, dst)
	}
}

func TestUnitTemplateContractForQualifier_Route(t *testing.T) {
	contract, ok := UnitTemplateContractForQualifier(ObjectQualifierRoute)
	if !ok {
		t.Fatalf("expected route unit template contract")
	}
	if contract.Key != UnitTemplateRouteFlow {
		t.Fatalf("contract key=%q, want %q", contract.Key, UnitTemplateRouteFlow)
	}
	if contract.DefaultPair.Source != UnitCodeLogistics || contract.DefaultPair.Target != UnitCodeLogistics {
		t.Fatalf("default pair=%q->%q, want logistics->logistics", contract.DefaultPair.Source, contract.DefaultPair.Target)
	}
}
