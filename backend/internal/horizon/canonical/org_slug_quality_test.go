package canonical

import "testing"

func TestOrgSlugForbiddenPlaceholder(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{slug: "retailer", want: true},
		{slug: "freight-forwarder", want: true},
		{slug: "generic-component-manufacturer", want: true},
		{slug: "shell-plc", want: false},
		{slug: "asml", want: false},
	}
	for _, tc := range tests {
		if got := OrgSlugForbiddenPlaceholder(tc.slug); got != tc.want {
			t.Fatalf("slug=%q got=%v want=%v", tc.slug, got, tc.want)
		}
	}
}

func TestGovSlugForbiddenPlaceholder(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{slug: "ministry-of", want: true},
		{slug: "department-of", want: true},
		{slug: "bureau-of", want: true},
		{slug: "ministry-of-defence-uk", want: false},
	}
	for _, tc := range tests {
		if got := GovSlugForbiddenPlaceholder(tc.slug); got != tc.want {
			t.Fatalf("slug=%q got=%v want=%v", tc.slug, got, tc.want)
		}
	}
}
