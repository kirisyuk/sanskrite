package taxonomy

import "testing"

func TestNormalizeBusinessTagPath(t *testing.T) {
	got, ok := NormalizeBusinessTagPath("biz/function/Regulation")
	if !ok {
		t.Fatalf("expected valid tag")
	}
	if got != "/biz/function/regulation" {
		t.Fatalf("unexpected normalized tag: %s", got)
	}

	if _, ok := NormalizeBusinessTagPath("/biz/unknown/value"); ok {
		t.Fatalf("expected invalid family to be rejected")
	}
}

func TestNormalizeFragmentTagsWithBusiness_Market(t *testing.T) {
	out := NormalizeFragmentTagsWithBusiness(
		[]string{"RWA", "/biz/function/Regulation", "ai"},
		"MARKET",
		"/market/us/finance/banking",
		[]string{"/segment/finance/banking/us"},
		"",
		"finance",
	)

	has := map[string]bool{}
	for _, t := range out {
		has[t] = true
	}

	if !has["real-world-asset"] {
		t.Fatalf("expected normalized market tag real-world-asset")
	}
	if !has["artificial-intelligence"] {
		t.Fatalf("expected normalized market tag artificial-intelligence")
	}
	if !has["/biz/function/regulation"] {
		t.Fatalf("expected normalized business tag to be preserved")
	}
	if !has["/biz/value-pool/financial-services"] {
		t.Fatalf("expected derived finance business value-pool tag")
	}
}

func TestNormalizeFragmentTagsWithBusiness_OrgSizeReplaceability(t *testing.T) {
	out := NormalizeFragmentTagsWithBusiness(
		nil,
		"ORGANIZATION",
		"/org/us/example-inc",
		[]string{"/segment/industrial/automotive/us"},
		"mega",
		"",
	)
	has := map[string]bool{}
	for _, t := range out {
		has[t] = true
	}
	if !has["/biz/replaceability/low"] {
		t.Fatalf("expected replaceability tag for mega org")
	}
	if !has["/biz/risk-exposure/industrial-capacity"] {
		t.Fatalf("expected industrial risk exposure tag")
	}
}
