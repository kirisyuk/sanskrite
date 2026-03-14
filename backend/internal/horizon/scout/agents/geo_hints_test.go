package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyInferredHomeLocationHint_FillsFacilityPlaceFromName(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/il/salt-of-the-earth-eilat",
		Labels:        map[string]string{"en": "Salt Of The Earth Eilat"},
	}

	applyInferredHomeLocationHint(fragment)

	if fragment.HomeLocation == nil || strings.TrimSpace(fragment.HomeLocation.Name) != "Eilat" {
		t.Fatalf("expected Eilat homeLocation, got %+v", fragment.HomeLocation)
	}
	if fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) != "IL" {
		t.Fatalf("expected IL iso2, got %+v", fragment.HomeLocation)
	}
}

func TestApplyInferredHomeLocationHint_FillsOrgPlaceFromSlug(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/cn/jabil-circuit-wuxi",
		Labels:        map[string]string{"en": "Jabil Circuit Wuxi"},
	}

	applyInferredHomeLocationHint(fragment)

	if fragment.HomeLocation == nil || strings.TrimSpace(fragment.HomeLocation.Name) != "Wuxi" {
		t.Fatalf("expected Wuxi homeLocation, got %+v", fragment.HomeLocation)
	}
	if fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) != "CN" {
		t.Fatalf("expected CN iso2, got %+v", fragment.HomeLocation)
	}
}

func TestApplyInferredHomeLocationHint_DoesNotOverrideExistingPlace(t *testing.T) {
	iso2 := "SA"
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/sa/al-rajhi-bank-pjsc",
		Labels:        map[string]string{"en": "Al Rajhi Bank Pjsc"},
		HomeLocation: &models.Location{
			Name:      "Riyadh",
			ISO3166_1: &iso2,
		},
	}

	applyInferredHomeLocationHint(fragment)

	if fragment.HomeLocation == nil || strings.TrimSpace(fragment.HomeLocation.Name) != "Riyadh" {
		t.Fatalf("expected existing Riyadh to be preserved, got %+v", fragment.HomeLocation)
	}
}
