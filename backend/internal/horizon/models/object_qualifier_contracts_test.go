package models

import "testing"

func TestNormalizeObjectQualifier_Alias(t *testing.T) {
	if got := NormalizeObjectQualifier("supply-chain"); got != ObjectQualifierSupply {
		t.Fatalf("NormalizeObjectQualifier(supply-chain)=%q want=%q", got, ObjectQualifierSupply)
	}
	if got := NormalizeObjectQualifier("owner_operator"); got != ObjectQualifierOwnerOperator {
		t.Fatalf("NormalizeObjectQualifier(owner_operator)=%q want=%q", got, ObjectQualifierOwnerOperator)
	}
}

func TestIsAllowedObjectQualifierForType(t *testing.T) {
	if !IsAllowedObjectQualifierForType(FragmentTypeFacility, "/fac/global/red-sea-corridor", ObjectQualifierRoute) {
		t.Fatalf("route must be allowed for facility")
	}
	if IsAllowedObjectQualifierForType(FragmentTypeFacility, "/fac/global/red-sea-corridor", ObjectQualifierHolding) {
		t.Fatalf("holding must not be allowed for facility")
	}
	if !IsAllowedObjectQualifierForType(FragmentTypeOrganization, "/gov/sa/sfda", ObjectQualifierRegulator) {
		t.Fatalf("regulator must be allowed for /gov/ path")
	}
}

func TestAllObjectQualifierStrings_ContainsCoreContract(t *testing.T) {
	all := AllObjectQualifierStrings()
	if len(all) == 0 {
		t.Fatalf("AllObjectQualifierStrings() must not be empty")
	}
	hasRoute := false
	hasSupply := false
	hasProcurement := false
	for _, item := range all {
		switch item {
		case string(ObjectQualifierRoute):
			hasRoute = true
		case string(ObjectQualifierSupply):
			hasSupply = true
		case string(ObjectQualifierProcurement):
			hasProcurement = true
		}
	}
	if !hasRoute || !hasSupply || !hasProcurement {
		t.Fatalf("global object qualifier enum must contain route/supply/procurement, got=%v", all)
	}
}
