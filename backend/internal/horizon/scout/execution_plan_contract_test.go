package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestDefaultExecutionPlanContract_SupplyChainMaxRecursionDepth(t *testing.T) {
	// Supply chain mode should use MaxRecursionDepth 5 (SupplyChainMaxTraversalDepth)
	p := defaultExecutionPlanContract([]string{taxonomy.DomainModeSupplyChain}, nil)
	if p.MaxRecursionDepth != SupplyChainMaxTraversalDepth {
		t.Errorf("supply_chain mode: MaxRecursionDepth = %d, want %d", p.MaxRecursionDepth, SupplyChainMaxTraversalDepth)
	}
	if p.Mode != "supply_chain" {
		t.Errorf("supply_chain mode: Mode = %q, want supply_chain", p.Mode)
	}
}

func TestDefaultExecutionPlanContract_GeneralModeDepth(t *testing.T) {
	p := defaultExecutionPlanContract(nil, nil)
	if p.MaxRecursionDepth != 2 {
		t.Errorf("general mode: MaxRecursionDepth = %d, want 2", p.MaxRecursionDepth)
	}
}
