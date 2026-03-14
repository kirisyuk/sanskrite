package scout

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestSupplyChainStrictValidatorRejectsDistributionFacilityProducingPhysicalGoods(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/kz/almaty-distribution-center",
		TargetCanonicalPath: "/product/finished/electronics/mobile/smartphones",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/kz/almaty-distribution-center",
		Type:          models.FragmentTypeFacility,
		Labels:        map[string]string{"en": "Almaty Distribution Center"},
		Identifiers: map[string]string{
			"facilityType": "distribution",
			"facilityKind": "warehouse",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Type:          models.FragmentTypeProduct,
	}

	err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "produces_source_facility_non_producing_role", "/fac/kz/almaty-distribution-center") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupplyChainStrictValidatorAllowsDistributionFacilityProducingService(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/de/dhl-leipzig-hub",
		TargetCanonicalPath: "/product/service/logistics/parcel-sorting",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/de/dhl-leipzig-hub",
		Type:          models.FragmentTypeFacility,
		Labels:        map[string]string{"en": "DHL Leipzig Hub"},
		Identifiers: map[string]string{
			"facilityType": "logistics",
			"facilityKind": "terminal",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/service/logistics/parcel-sorting",
		Type:          models.FragmentTypeProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestSupplyChainStrictValidatorRejectsWarehouseByInference(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/kz/almaty-distribution-center",
		TargetCanonicalPath: "/product/finished/electronics/mobile/smartphones",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/kz/almaty-distribution-center",
		Type:          models.FragmentTypeFacility,
		Labels:        map[string]string{"en": "Almaty Distribution Center"},
		Identifiers:   map[string]string{},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Type:          models.FragmentTypeProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSupplyChainStrictValidatorAllowsManufacturingFacilityProducingPhysicalGoods(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/kr/samsung-gumi-plant",
		TargetCanonicalPath: "/product/finished/electronics/mobile/smartphones",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/kr/samsung-gumi-plant",
		Type:          models.FragmentTypeFacility,
		Labels:        map[string]string{"en": "Samsung Gumi Plant"},
		Identifiers: map[string]string{
			"facilityType": "manufacturing",
			"facilityKind": "factory",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Type:          models.FragmentTypeProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
