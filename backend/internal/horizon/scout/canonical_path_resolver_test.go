package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeGeoPathToStrict(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "/loc/uae", want: "/loc/ae/na/na"},
		{in: "/loc/us", want: "/loc/us/na/na"},
		{in: "/loc/saudi-arabia", want: "/loc/sa/na/na"},
		{in: "/loc/us/tx", want: "/loc/us/tx/na"},
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

func TestInferFragmentTypeFromCanonicalPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "/gov/sa/sfda", want: string(models.FragmentTypeOrganization)},
		{in: "/org/us/tesla", want: string(models.FragmentTypeOrganization)},
		{in: "/product/raw/mineral/extraction/lithium-ore", want: string(models.FragmentTypeProduct)},
		{in: "/fac/tesla/gigafactory-texas", want: string(models.FragmentTypeFacility)},
		{in: "/loc/us/tx/austin", want: string(models.FragmentTypeGeoRegion)},
		{in: "/region/na/us/tx/austin", want: string(models.FragmentTypeGeoRegion)},
		{in: "/event/ces-2026", want: string(models.FragmentTypeEvent)},
		{in: "/person/elon-musk", want: string(models.FragmentTypePerson)},
		{in: "/media/telegram/v-kazani-poymut", want: string(models.FragmentTypeMediaChannel)},
		{in: "/deposit/oil/gawar", want: string(models.FragmentTypeDeposit)},
		{in: "/community/global/interest/osint-researchers", want: string(models.FragmentTypeCommunity)},
	}

	for _, tc := range cases {
		got := InferFragmentTypeFromCanonicalPath(tc.in)
		if string(got) != tc.want {
			t.Fatalf("InferFragmentTypeFromCanonicalPath(%q) = %q, want %q", tc.in, string(got), tc.want)
		}
	}
}

