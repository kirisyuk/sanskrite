package manualresolve

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFragmentLikelyNeedsGeoCoordinates(t *testing.T) {
	if FragmentLikelyNeedsGeoCoordinates(nil) {
		t.Fatal("nil")
	}
	if !FragmentLikelyNeedsGeoCoordinates(&horizonmodels.HorizonFragment{CanonicalPath: "/org/us/acme"}) {
		t.Fatal("org path")
	}
	if !FragmentLikelyNeedsGeoCoordinates(&horizonmodels.HorizonFragment{CanonicalPath: "/x", Type: horizonmodels.FragmentTypeFacility}) {
		t.Fatal("facility type")
	}
	if FragmentLikelyNeedsGeoCoordinates(&horizonmodels.HorizonFragment{CanonicalPath: "/product/foo", Type: horizonmodels.FragmentTypeProduct}) {
		t.Fatal("product should not default to geo queue")
	}
}
