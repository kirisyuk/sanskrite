package edgesemantics

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestNormalizeFragmentClassToken_ExtendedTypesAndAliases(t *testing.T) {
	tests := []struct {
		in          string
		wantClass   ah.FragmentClass
		wantSubtype string
	}{
		{in: "GOV", wantClass: ah.FragmentClassOrg, wantSubtype: "gov"},
		{in: "GOVERNMENT_BODY", wantClass: ah.FragmentClassOrg, wantSubtype: "gov"},
		{in: "FAC", wantClass: ah.FragmentClassSite},
		{in: "LOC", wantClass: ah.FragmentClassGeo},
		{in: "MEDIA", wantClass: ah.FragmentClassOrg, wantSubtype: "media_channel"},
		{in: "DECISION", wantClass: ah.FragmentClassDecision},
		{in: "MECHANISM", wantClass: ah.FragmentClassMechanism},
		{in: "SUPPLYLINK", wantClass: ah.FragmentClassSite, wantSubtype: "supply_link"},
		{in: "NETWORK", wantClass: ah.FragmentClassCommunity},
	}

	for _, tt := range tests {
		gotClass, gotSubtype, ok := NormalizeFragmentClassToken(tt.in)
		if !ok {
			t.Fatalf("NormalizeFragmentClassToken(%q) ok=false, want true", tt.in)
		}
		if gotClass != tt.wantClass {
			t.Fatalf("NormalizeFragmentClassToken(%q) class=%q, want %q", tt.in, gotClass, tt.wantClass)
		}
		if gotSubtype != tt.wantSubtype {
			t.Fatalf("NormalizeFragmentClassToken(%q) subtype=%q, want %q", tt.in, gotSubtype, tt.wantSubtype)
		}
	}
}

func TestNormalizeFragmentClassToken_PathInferenceAndUnknownFallback(t *testing.T) {
	gotClass, _, ok := NormalizeFragmentClassToken("/mechanism/regulatory/export-control")
	if !ok || gotClass != ah.FragmentClassMechanism {
		t.Fatalf("path-based normalization should infer Mechanism, got %q (ok=%v)", gotClass, ok)
	}

	gotUnknown, _, ok := NormalizeFragmentClassToken("UNMAPPED_TYPE_TOKEN")
	if ok {
		t.Fatalf("NormalizeFragmentClassToken(unknown) ok=true, want false")
	}
	if gotUnknown != ah.FragmentClassOrg {
		t.Fatalf("unknown type fallback should be FragmentClassOrg, got %q", gotUnknown)
	}

	if _, _, ok := NormalizeFragmentClassToken("DOMAIN"); ok {
		t.Fatalf("NormalizeFragmentClassToken(DOMAIN) ok=true, want false")
	}
	if _, _, ok := NormalizeFragmentClassToken("EVENT"); ok {
		t.Fatalf("NormalizeFragmentClassToken(EVENT) ok=true, want false")
	}
}
