package horizon

import (
	"reflect"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestNormalizeScopeScoutMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "governance", want: taxonomy.DomainModeGovernance},
		{in: "supply_chain", want: taxonomy.DomainModeSupplyChain},
		{in: "GENERAL", want: taxonomy.DomainModeGeneral},
		{in: "unknown", want: taxonomy.DomainModeGeneral},
	}

	for _, tt := range tests {
		if got := normalizeScopeScoutMode(tt.in); got != tt.want {
			t.Fatalf("normalizeScopeScoutMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSpecialtiesForScopeScoutMode(t *testing.T) {
	if got := specialtiesForScopeScoutMode("governance"); !reflect.DeepEqual(got, []string{taxonomy.SpecialtyManagement, taxonomy.SpecialtyEquity}) {
		t.Fatalf("governance specialties = %v", got)
	}
	if got := specialtiesForScopeScoutMode("supply_chain"); !reflect.DeepEqual(got, []string{taxonomy.SpecialtySupplyChain, taxonomy.SpecialtyProductsAndServices}) {
		t.Fatalf("supply_chain specialties = %v", got)
	}
	if got := specialtiesForScopeScoutMode("general"); len(got) != 0 {
		t.Fatalf("general specialties = %v, want empty", got)
	}
}
