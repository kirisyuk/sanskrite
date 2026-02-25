package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestBuildObjectTypeTaskSpecializationBlock_FacilityRouteFocus(t *testing.T) {
	got := buildObjectTypeTaskSpecializationBlock("/fac/global/red-sea-corridor", models.FragmentTypeFacility, "")
	for _, want := range []string{"route-qualified fac", "waypoint coordinates", "suppliers/logistics operators/customers"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Fatalf("facility specialization block missing %q", want)
		}
	}
}

func TestBuildObjectTypeTaskSpecializationBlockWithHints_FacilitySupplyFocus(t *testing.T) {
	got := buildObjectTypeTaskSpecializationBlockWithHints(
		"/fac/de/neutral-facility",
		models.FragmentTypeFacility,
		"",
		map[string]interface{}{"supplyRole": "supplier hub"},
		nil,
		nil,
	)
	for _, want := range []string{"supply-qualified fac", "upstream suppliers", "downstream customers"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Fatalf("facility supply specialization block missing %q", want)
		}
	}
}

func TestBuildObjectTypeTaskSpecializationBlockWithHints_FacilityProcurementFocus(t *testing.T) {
	got := buildObjectTypeTaskSpecializationBlockWithHints(
		"/fac/us/vendor-center",
		models.FragmentTypeFacility,
		"",
		nil,
		map[string]string{"facilityRole": "procurement"},
		nil,
	)
	for _, want := range []string{"procurement-qualified fac", "vendors", "alternative supplier regions"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Fatalf("facility procurement specialization block missing %q", want)
		}
	}
}

func TestBuildExtractionSpecializationModulesBlock_ContainsCoreSupplyChain(t *testing.T) {
	got := buildExtractionSpecializationModulesBlock(
		[]string{taxonomy.SpecialtyManagement},
		[]string{taxonomy.DomainModeSupplyChain},
	)
	for _, want := range []string{"CORE + SPECIALIZATION MODULES", "requestedDomainModes", "MODULE: supply_chain", "MODULE: management"} {
		if !strings.Contains(got, want) {
			t.Fatalf("specialization modules block missing %q", want)
		}
	}
}

func TestBuildObjectQualifierContractBlock_Facility(t *testing.T) {
	got := buildObjectQualifierContractBlockWithHints(
		"/fac/global/red-sea-corridor",
		models.FragmentTypeFacility,
		map[string]interface{}{"route": true},
		nil,
		nil,
	)
	for _, want := range []string{
		"OBJECT QUALIFIER CONTRACT (STRICT)",
		"objectQualifier CLOSED ENUM",
		"Runtime-aligned facility lane detection: route.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("contract block missing %q\ngot:\n%s", want, got)
		}
	}
}
