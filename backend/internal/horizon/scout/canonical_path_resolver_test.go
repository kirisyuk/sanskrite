package scout

import (
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
	if !ValidateRedirectCompatibility("/market/sa/finance/banking", "/market/sa/finance/islamic-banking", FragmentTypeMarket) {
		t.Fatalf("same domain (finance) redirect should be compatible")
	}
}

func TestValidateRedirectCompatibility_MarketCrossDomainRejected(t *testing.T) {
	if ValidateRedirectCompatibility("/market/sa/finance/banking", "/market/sa/technology/banking", FragmentTypeMarket) {
		t.Fatalf("cross-domain redirect (finance->technology) should be rejected")
	}
}

func TestValidateRedirectCompatibility_NonMarketAllowed(t *testing.T) {
	if !ValidateRedirectCompatibility("/org/na/foo", "/org/us/foo-inc", FragmentTypeOrganization) {
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
		{in: "/gov/sa/sfda", want: FragmentTypeOrganization},
		{in: "/org/us/tesla", want: FragmentTypeOrganization},
		{in: "/product/raw/mineral/extraction/lithium-ore", want: FragmentTypeProduct},
		{in: "/fac/us/gigafactory-texas", want: FragmentTypeFacility},
		{in: "/loc/us/tx/austin", want: FragmentTypeGeoRegion},
		{in: "/region/na/us/tx/austin", want: FragmentTypeGeoRegion},
		{in: "/event/ces-2026", want: FragmentTypeEvent},
		{in: "/person/elon-musk", want: FragmentTypePerson},
		// Legacy collapsed types fold to parent class. Subtype info is checked
		// via InferClassSubtypeFromCanonicalPath in canonical_path_utils_domain_test.go.
		{in: "/media/telegram/v-kazani-poymut", want: FragmentTypeOrganization},
		{in: "/deposit/oil/gawar", want: FragmentTypeFacility},
		{in: "/community/global/interest/osint-researchers", want: FragmentTypeCommunity},
	}

	for _, tc := range cases {
		got := InferFragmentTypeFromCanonicalPath(tc.in)
		if got != tc.want {
			t.Fatalf("InferFragmentTypeFromCanonicalPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
