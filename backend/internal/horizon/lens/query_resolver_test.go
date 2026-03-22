package lens

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildLocationPathPrefersGeoSlug(t *testing.T) {
	if got := buildLocationPath("ae", nil); got != "/loc/ae/na/na" {
		t.Fatalf("buildLocationPath(geoSlug only)=%q, want %q", got, "/loc/ae/na/na")
	}
}

func TestBuildLocationPathUsesAdminFromHomeLocation(t *testing.T) {
	iso1 := "US"
	iso2 := "US-TX"
	got := buildLocationPath("", &models.Location{
		ISO3166_1: &iso1,
		ISO3166_2: &iso2,
	})
	if got != "/loc/us/tx/na" {
		t.Fatalf("buildLocationPath(homeLocation)=%q, want %q", got, "/loc/us/tx/na")
	}
}
