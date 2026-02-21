package lens

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestEffectiveDomainModesForLens_PrefersDomainModes(t *testing.T) {
	lens := &models.Lens{
		DomainModes: []string{"supply_chain", "governance"},
		ViewConfig: map[string]interface{}{
			"domains": []agents.DomainResolution{
				{ContractType: "market-opportunity"},
			},
		},
	}
	got := effectiveDomainModesForLens(lens)
	if len(got) != 2 {
		t.Fatalf("expected 2 modes from DomainModes, got %v", got)
	}
	if got[0] != "supply_chain" || got[1] != "governance" {
		t.Fatalf("expected [supply_chain governance], got %v", got)
	}
}

func TestEffectiveDomainModesForLens_FallbackFromViewConfig(t *testing.T) {
	lens := &models.Lens{
		DomainModes: nil,
		ViewConfig: map[string]interface{}{
			"domains": []agents.DomainResolution{
				{ContractType: "supply-chain"},
			},
		},
	}
	got := effectiveDomainModesForLens(lens)
	if len(got) != 1 {
		t.Fatalf("expected 1 mode from ViewConfig fallback, got %v", got)
	}
	if got[0] != "supply_chain" {
		t.Fatalf("expected [supply_chain], got %v", got)
	}
}

func TestEffectiveDomainModesForLens_EmptyWhenNoSource(t *testing.T) {
	lens := &models.Lens{
		DomainModes: nil,
		ViewConfig:  nil,
	}
	got := effectiveDomainModesForLens(lens)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestEffectiveDomainModesForLens_FromViewConfigMap(t *testing.T) {
	lens := &models.Lens{
		ViewConfig: map[string]interface{}{
			"domains": []map[string]interface{}{
				{"contractType": "market-opportunity"},
			},
		},
	}
	got := effectiveDomainModesForLens(lens)
	if len(got) != 1 {
		t.Fatalf("expected 1 mode from ViewConfig map, got %v", got)
	}
	if got[0] != "market" {
		t.Fatalf("expected [market], got %v", got)
	}
}
