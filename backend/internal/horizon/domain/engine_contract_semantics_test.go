package domain

import (
	"reflect"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestExpectedRelationsForContractType_GovernanceContracts(t *testing.T) {
	tests := []string{"ownership-structure", "control-map"}
	for _, contractType := range tests {
		t.Run(contractType, func(t *testing.T) {
			got := expectedRelationsForContractType(contractType)
			if !reflect.DeepEqual(got, taxonomy.GovernanceEdgeTypes) {
				t.Fatalf("expectedRelationsForContractType(%q) = %v, want %v", contractType, got, taxonomy.GovernanceEdgeTypes)
			}
		})
	}
}

func TestDefaultFilterRelTypes_GovernanceContracts(t *testing.T) {
	tests := []string{"ownership-structure", "control-map"}
	for _, contractType := range tests {
		t.Run(contractType, func(t *testing.T) {
			got := defaultFilterRelTypes(contractType)
			if !reflect.DeepEqual(got, taxonomy.GovernanceEdgeTypes) {
				t.Fatalf("defaultFilterRelTypes(%q) = %v, want %v", contractType, got, taxonomy.GovernanceEdgeTypes)
			}
		})
	}
}
