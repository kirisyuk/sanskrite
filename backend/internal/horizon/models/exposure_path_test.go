package models

import "testing"

func TestCanonicalizeExposureKey_BuildRoundTrip(t *testing.T) {
	k, err := BuildExposureKey(ExposureTypeSupplyChainRisk, "fragment", "F123", "", "", "P1")
	if err != nil {
		t.Fatalf("BuildExposureKey err: %v", err)
	}
	c, parts, err := CanonicalizeExposureKey(k)
	if err != nil {
		t.Fatalf("CanonicalizeExposureKey err: %v", err)
	}
	if c != k {
		t.Fatalf("expected canonical == input\ninput: %s\ncanon: %s", k, c)
	}
	if parts.Type != ExposureTypeSupplyChainRisk || parts.ObjectiveKind != "fragment" || parts.ObjectiveID != "F123" || parts.PathID != "P1" {
		t.Fatalf("unexpected parts: %+v", *parts)
	}
}

func TestCanonicalizeExposureKey_TolerantInput(t *testing.T) {
	in := "SUPPLY_CHAIN_RISK/obj/Fragment/F123/rel/FRAGMENT/R9/path/p-0001"
	c, parts, err := CanonicalizeExposureKey(in)
	if err != nil {
		t.Fatalf("CanonicalizeExposureKey err: %v", err)
	}
	want := "/exposure/SUPPLY_CHAIN_RISK/objective/fragment/F123/related/fragment/R9/path/p-0001"
	if c != want {
		t.Fatalf("canon mismatch\nwant: %s\ngot:  %s", want, c)
	}
	if parts.PrefixKey() != "/exposure/SUPPLY_CHAIN_RISK/objective/fragment/F123/related/fragment/R9" {
		t.Fatalf("prefix mismatch: %s", parts.PrefixKey())
	}
}

func TestCanonicalizeExposureKey_RejectsBad(t *testing.T) {
	_, _, err := CanonicalizeExposureKey("/exposure/UNKNOWN/objective/x/y")
	if err == nil {
		t.Fatalf("expected error for unknown exposure type")
	}
	_, _, err = CanonicalizeExposureKey("/exposure/SUPPLY_CHAIN_RISK/objective/x")
	if err == nil {
		t.Fatalf("expected error for missing objective id")
	}
}


