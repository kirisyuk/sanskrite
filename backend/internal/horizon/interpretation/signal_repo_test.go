package interpretation

import (
	"testing"
	"time"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestDescriptorFromSignalFactNormalizesLegacyAxisAlias(t *testing.T) {
	now := time.Now().UTC()
	fact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             "sig-1",
			CreatedAt:      now,
			UpdatedAt:      now,
			CanonicalPaths: []string{"/org/us/tesla"},
		},
		Kind:       "signal",
		Type:       "risk:supply_chain_disruption",
		Confidence: 0.8,
	}
	desc := descriptorFromSignalFact(fact)
	if desc == nil {
		t.Fatal("descriptorFromSignalFact() = nil")
	}
	if desc.Axis != "physical_flow/supply" {
		t.Fatalf("axis = %q, want physical_flow/supply", desc.Axis)
	}
	if desc.Mechanism != "supply_chain_disruption" {
		t.Fatalf("mechanism = %q, want supply_chain_disruption", desc.Mechanism)
	}
}
