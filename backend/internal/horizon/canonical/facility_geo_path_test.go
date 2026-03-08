package canonical

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestInferFacilityCanonicalPathFromGeo(t *testing.T) {
	us := "US"
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/na/tustin-facility",
		Labels:        map[string]string{"en": "Tustin Facility"},
		HomeLocation:  &models.Location{ISO3166_1: &us},
	}
	path, ok := InferFacilityCanonicalPathFromGeo(frag)
	if !ok {
		t.Fatalf("expected upgrade")
	}
	if path != "/fac/us/tustin-facility" {
		t.Fatalf("unexpected path: %s", path)
	}
}
