package models

import "testing"

func TestDetectFacilitySemanticFromFragment_ExplicitObjectQualifier(t *testing.T) {
	frag := &HorizonFragment{
		Type:          FragmentTypeFacility,
		CanonicalPath: "/fac/sa/east-west-corridor",
		Identifiers:   map[string]string{"objectQualifier": "route"},
	}
	if got := DetectFacilitySemanticFromFragment(frag); got != FacilitySemanticRoute {
		t.Fatalf("DetectFacilitySemanticFromFragment()=%q, want %q", got, FacilitySemanticRoute)
	}
}

func TestDetectFacilitySemantic_Hints(t *testing.T) {
	if got := DetectFacilitySemantic("/fac/us/vendor-gateway", map[string]interface{}{"facilityRole": "procurement hub"}, nil, nil); got != FacilitySemanticProcurement {
		t.Fatalf("procurement hint semantic=%q, want %q", got, FacilitySemanticProcurement)
	}
	if got := DetectFacilitySemantic("/fac/de/supplier-park", nil, map[string]string{"supplyChainRole": "supplier"}, nil); got != FacilitySemanticSupply {
		t.Fatalf("supply hint semantic=%q, want %q", got, FacilitySemanticSupply)
	}
}
