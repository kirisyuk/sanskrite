package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestSupplyChainStrictValidatorAllowsDistributionFacilityProducingPhysicalGoods(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/kz/almaty-distribution-center",
		TargetCanonicalPath: "/product/finished/electronics/mobile/smartphones",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/kz/almaty-distribution-center",
		Class:          ah.FragmentClassSite,
		Labels:        map[string]string{"en": "Almaty Distribution Center"},
		Identifiers: map[string]string{
			"facilityType": "distribution",
			"facilityKind": "warehouse",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Class:          ah.FragmentClassProduct,
	}

	err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
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
		Class:          ah.FragmentClassSite,
		Labels:        map[string]string{"en": "DHL Leipzig Hub"},
		Identifiers: map[string]string{
			"facilityType": "logistics",
			"facilityKind": "terminal",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/service/logistics/parcel-sorting",
		Class:          ah.FragmentClassProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestSupplyChainStrictValidatorAllowsWarehouseByInference(t *testing.T) {
	validator := NewSupplyChainStrictValidator()
	edge := &ChainEdge{
		SourceCanonicalPath: "/fac/kz/almaty-distribution-center",
		TargetCanonicalPath: "/product/finished/electronics/mobile/smartphones",
		RelationType:        "PRODUCES",
	}
	source := &models.HorizonFragment{
		CanonicalPath: "/fac/kz/almaty-distribution-center",
		Class:          ah.FragmentClassSite,
		Labels:        map[string]string{"en": "Almaty Distribution Center"},
		Identifiers:   map[string]string{},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Class:          ah.FragmentClassProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
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
		Class:          ah.FragmentClassSite,
		Labels:        map[string]string{"en": "Samsung Gumi Plant"},
		Identifiers: map[string]string{
			"facilityType": "manufacturing",
			"facilityKind": "factory",
		},
	}
	target := &models.HorizonFragment{
		CanonicalPath: "/product/finished/electronics/mobile/smartphones",
		Class:          ah.FragmentClassProduct,
	}

	if err := validator.ValidateEdgeAgainstFragments(edge, source, target, "job-1", 0); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
