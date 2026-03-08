package canonical

import "testing"

func TestClassifyPlaceholderCanonicalPath(t *testing.T) {
	cases := []struct {
		path string
		want PlaceholderPathKind
	}{
		{"/fac/na/general-production", PlaceholderPathFacilitySynthetic},
		{"/fac/na/port-of-shanghai", PlaceholderPathFacilityUnknownGeo},
		{"/org/na/retailer", PlaceholderPathOrgSynthetic},
		{"/org/na/bureau-of-land-management", PlaceholderPathOrgUnknownJurisdiction},
		{"/gov/na/ministry-of", PlaceholderPathGovSynthetic},
		{"/gov/na/navy", PlaceholderPathGovUnknownJurisdiction},
		{"/loc/us/na/na", PlaceholderPathLocPlaceholderAdmin12},
		{"/loc/us/california/na", PlaceholderPathLocPlaceholderAdmin2},
		{"/loc/us", PlaceholderPathNone},
		{"/loc/na/energy-corridor", PlaceholderPathNone},
	}
	for _, tc := range cases {
		if got := ClassifyPlaceholderCanonicalPath(tc.path); got != tc.want {
			t.Fatalf("ClassifyPlaceholderCanonicalPath(%q)=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestNormalizePlaceholderCanonicalPath(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/loc/us/na/na", "/loc/us", true},
		{"/loc/us/california/na", "/loc/us/california", true},
		{"/fac/na/unresolved-plant", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizePlaceholderCanonicalPath(tc.path)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("NormalizePlaceholderCanonicalPath(%q)=(%q,%v) want (%q,%v)", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}
