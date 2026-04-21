package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestNormalizeGeoPathToStrict(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "/loc/uae", want: "/loc/ae"},
		{in: "/loc/us", want: "/loc/us"},
		{in: "/loc/saudi-arabia", want: "/loc/sa"},
		{in: "/loc/us/tx", want: "/loc/us/tx"},
		{in: "/loc/us/tx/austin", want: "/loc/us/tx/austin"},
		{in: "/loc/georgia", want: ""}, // ambiguous (country vs US state) -> refuse to guess
		{in: "/loc/not-a-country", want: ""},
		{in: "/org/tesla", want: ""},
	}

	for _, tc := range cases {
		got := normalizeGeoPathToStrict(tc.in)
		if got != tc.want {
			t.Fatalf("normalizeGeoPathToStrict(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateRedirectCompatibility_MarketSameDomain(t *testing.T) {
	if !ValidateRedirectCompatibility("/market/sa/finance/banking", "/market/sa/finance/islamic-banking", ah.FragmentClassMarket) {
		t.Fatalf("same domain (finance) redirect should be compatible")
	}
}

func TestValidateRedirectCompatibility_MarketCrossDomainRejected(t *testing.T) {
	if ValidateRedirectCompatibility("/market/sa/finance/banking", "/market/sa/technology/banking", ah.FragmentClassMarket) {
		t.Fatalf("cross-domain redirect (finance->technology) should be rejected")
	}
}

func TestValidateRedirectCompatibility_NonMarketAllowed(t *testing.T) {
	if !ValidateRedirectCompatibility("/org/na/foo", "/org/us/foo-inc", ah.FragmentClassOrg) {
		t.Fatalf("non-MARKET redirect should be allowed")
	}
}

func TestExtractMarketDomain(t *testing.T) {
	if got := extractMarketDomain("/market/sa/finance/banking"); got != "finance" {
		t.Fatalf("extractMarketDomain(/market/sa/finance/banking)=%q, want finance", got)
	}
	if got := extractMarketDomain("/market/sa/technology/banking"); got != "technology" {
		t.Fatalf("extractMarketDomain(/market/sa/technology/banking)=%q, want technology", got)
	}
	if got := extractMarketDomain("/org/us/tesla"); got != "" {
		t.Fatalf("extractMarketDomain(/org/us/tesla)=%q, want empty", got)
	}
}

func TestInferFragmentTypeFromCanonicalPath(t *testing.T) {
	cases := []struct {
		in   string
		want FragmentType
	}{
		{in: "/gov/sa/sfda", want: ah.FragmentClassOrg},
		{in: "/org/us/tesla", want: ah.FragmentClassOrg},
		{in: "/product/raw/mineral/extraction/lithium-ore", want: ah.FragmentClassProduct},
		{in: "/fac/us/gigafactory-texas", want: ah.FragmentClassSite},
		{in: "/loc/us/tx/austin", want: ah.FragmentClassGeo},
		{in: "/region/na/us/tx/austin", want: ah.FragmentClassGeo},
		{in: "/event/ces-2026", want: ah.FragmentClassEvidence},
		{in: "/person/elon-musk", want: ah.FragmentClassPerson},
		// Legacy collapsed types fold to parent class. Subtype info is checked
		// via InferClassSubtypeFromCanonicalPath in canonical_path_utils_domain_test.go.
		{in: "/media/telegram/v-kazani-poymut", want: ah.FragmentClassOrg},
		{in: "/deposit/oil/gawar", want: ah.FragmentClassSite},
		{in: "/community/global/interest/osint-researchers", want: ah.FragmentClassCommunity},
	}

	for _, tc := range cases {
		got := InferFragmentTypeFromCanonicalPath(tc.in)
		if got != tc.want {
			t.Fatalf("InferFragmentTypeFromCanonicalPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
