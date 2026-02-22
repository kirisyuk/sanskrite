package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestDetectFacilityQualifierSemanticFromFragment(t *testing.T) {
	cases := []struct {
		name string
		frag *models.HorizonFragment
		want facilityQualifierSemantic
	}{
		{
			name: "explicit object qualifier route",
			frag: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/sa/east-west-corridor",
				Identifiers:   map[string]string{"objectQualifier": "route"},
			},
			want: facilityQualifierSemanticRoute,
		},
		{
			name: "route qualifier",
			frag: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/ae/jebel-ali-corridor",
				Qualifiers:    map[string]interface{}{"route": true},
			},
			want: facilityQualifierSemanticRoute,
		},
		{
			name: "procurement qualifier",
			frag: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/us/vendor-gateway",
				Qualifiers:    map[string]interface{}{"facilityRole": "procurement hub"},
			},
			want: facilityQualifierSemanticProcurement,
		},
		{
			name: "supply qualifier",
			frag: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/de/supplier-park",
				Identifiers:   map[string]string{"supplyChainRole": "supplier"},
			},
			want: facilityQualifierSemanticSupply,
		},
		{
			name: "no qualifier",
			frag: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/de/manufacturing-plant",
			},
			want: facilityQualifierSemanticNone,
		},
	}

	for _, tc := range cases {
		got := detectFacilityQualifierSemanticFromFragment(tc.frag)
		if got != tc.want {
			t.Fatalf("%s: got=%q want=%q", tc.name, got, tc.want)
		}
	}
}

func TestShouldSkipSameFacilityQualifierRecursion(t *testing.T) {
	routeSource := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/global/red-sea-corridor",
		Qualifiers:    map[string]interface{}{"route": true},
	}
	routeTarget := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/global/suez-route",
		Qualifiers:    map[string]interface{}{"route_path": []interface{}{"/loc/eg/suez"}},
	}
	if !shouldSkipSameFacilityQualifierRecursion(routeSource, routeTarget.CanonicalPath, routeTarget.Type, routeTarget) {
		t.Fatalf("expected route->route recursion to be skipped")
	}

	supplySource := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/cn/supplier-cluster",
		Identifiers:   map[string]string{"supplyChainRole": "supplier"},
	}
	procurementTarget := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/us/procurement-center",
		Qualifiers:    map[string]interface{}{"role": "procurement"},
	}
	if shouldSkipSameFacilityQualifierRecursion(supplySource, procurementTarget.CanonicalPath, procurementTarget.Type, procurementTarget) {
		t.Fatalf("did not expect supply->procurement recursion skip")
	}

	supplyTarget := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/pl/downstream-supplier",
		Qualifiers:    map[string]interface{}{"supplyRole": "supplier"},
	}
	if !shouldSkipSameFacilityQualifierRecursion(supplySource, supplyTarget.CanonicalPath, supplyTarget.Type, supplyTarget) {
		t.Fatalf("expected supply->supply recursion to be skipped")
	}
}

func TestIsFacilityQualifierBusinessRelation_Route(t *testing.T) {
	if isFacilityQualifierBusinessRelation(
		facilityQualifierSemanticRoute,
		string(models.DependencyTypeManages),
		"/person/jane-doe",
		models.FragmentTypePerson,
	) {
		t.Fatalf("route fac should not keep person/governance relation for business scouting")
	}
	if !isFacilityQualifierBusinessRelation(
		facilityQualifierSemanticRoute,
		string(models.DependencyTypeDependsOn),
		"/org/sg/maersk",
		models.FragmentTypeOrganization,
	) {
		t.Fatalf("route fac should keep business dependency to logistics org")
	}
}

func TestFilterMissingInfoByFacilityQualifier_DropsPersonNoise(t *testing.T) {
	in := []string{
		"Who are the siblings of /fac/global/red-sea-corridor?",
		"Which logistics operators are critical for /fac/global/red-sea-corridor?",
	}
	out := filterMissingInfoByFacilityQualifier(in, facilityQualifierSemanticRoute)
	if len(out) != 1 {
		t.Fatalf("expected 1 business question after filtering, got %d", len(out))
	}
	if out[0] != "Which logistics operators are critical for /fac/global/red-sea-corridor?" {
		t.Fatalf("unexpected remaining question: %q", out[0])
	}
}
