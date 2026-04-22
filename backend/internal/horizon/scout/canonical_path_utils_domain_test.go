package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestInferClassSubtypeFromCanonicalPath_Legacy(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantClass   ah.FragmentClass
		wantSubtype string
	}{
		{
			name:        "domain folds to evidence/domain",
			path:        "/domain/v1/supply-chain/geo/us/material/lithium",
			wantClass:   ah.FragmentClassEvidence,
			wantSubtype: "domain",
		},
		{
			name:        "decision -> decision",
			path:        "/decision/ai-chips/euv-export-control",
			wantClass:   ah.FragmentClassDecision,
			wantSubtype: "",
		},
		{
			name:        "mechanism -> mechanism",
			path:        "/mechanism/regulatory/export-control",
			wantClass:   ah.FragmentClassMechanism,
			wantSubtype: "",
		},
		{
			name:        "project -> project",
			path:        "/project/global/oil-for-food",
			wantClass:   ah.FragmentClassProject,
			wantSubtype: "",
		},
		{
			name:        "media -> org/media_channel",
			path:        "/media/cnbc",
			wantClass:   ah.FragmentClassOrg,
			wantSubtype: "media_channel",
		},
		{
			name:        "deposit -> site/deposit",
			path:        "/deposit/lithium/atacama-salar",
			wantClass:   ah.FragmentClassSite,
			wantSubtype: "deposit",
		},
		{
			name:        "supply-link -> site/supply_link",
			path:        "/supply-link/cu-andes",
			wantClass:   ah.FragmentClassSite,
			wantSubtype: "supply_link",
		},
		{
			name:        "zone -> geo/affected_zone",
			path:        "/zone/strait-of-hormuz",
			wantClass:   ah.FragmentClassGeo,
			wantSubtype: "affected_zone",
		},
		{
			name:        "gov -> org/gov",
			path:        "/gov/us/treasury/ofac",
			wantClass:   ah.FragmentClassOrg,
			wantSubtype: "gov",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InferClassSubtypeFromCanonicalPath(tc.path)
			if got.Class != tc.wantClass {
				t.Fatalf("class = %q, want %q", got.Class, tc.wantClass)
			}
			if got.Subtype != tc.wantSubtype {
				t.Fatalf("subtype = %q, want %q", got.Subtype, tc.wantSubtype)
			}
			if InferFragmentTypeFromCanonicalPath(tc.path) != tc.wantClass {
				t.Fatalf("InferFragmentTypeFromCanonicalPath drift")
			}
		})
	}
}
