package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestCollectAnchorOrgsFromQueriesByDirection_DefaultBoth(t *testing.T) {
	queries := []agents.SupplyChainQuery{
		{Anchor: "/org/za/coca-cola-beverages-south-africa", Direction: ""},
		{Anchor: "/fac/za/plant-1", Direction: ""},
	}

	downOrgs, downFacs, upOrgs, upFacs := collectAnchorOrgsFromQueriesByDirection(queries, "both")
	if len(downOrgs) != 1 || len(upOrgs) != 1 {
		t.Fatalf("expected org anchor in both directions, got downOrgs=%v upOrgs=%v", downOrgs, upOrgs)
	}
	if len(downFacs) != 1 || len(upFacs) != 1 {
		t.Fatalf("expected fac anchor in both directions, got downFacs=%v upFacs=%v", downFacs, upFacs)
	}
}

func TestCollectAnchorOrgsFromQueriesByDirection_ExplicitDirectionWins(t *testing.T) {
	queries := []agents.SupplyChainQuery{
		{Anchor: "/org/za/coca-cola-beverages-south-africa", Direction: "upstream"},
		{Anchor: "/org/za/pioneer-foods", Direction: "downstream"},
	}

	downOrgs, _, upOrgs, _ := collectAnchorOrgsFromQueriesByDirection(queries, "both")
	if len(downOrgs) != 1 || downOrgs[0] != "/org/za/pioneer-foods" {
		t.Fatalf("expected explicit downstream anchor only in downOrgs, got %v", downOrgs)
	}
	if len(upOrgs) != 1 || upOrgs[0] != "/org/za/coca-cola-beverages-south-africa" {
		t.Fatalf("expected explicit upstream anchor only in upOrgs, got %v", upOrgs)
	}
}

func TestCollectAnchorOrgsFromQueriesByDirection_DepositAnchorIncluded(t *testing.T) {
	queries := []agents.SupplyChainQuery{
		{Anchor: "/deposit/cl/chuquicamata-copper-mine", Direction: "downstream"},
	}

	downOrgs, downFacs, upOrgs, upFacs := collectAnchorOrgsFromQueriesByDirection(queries, "downstream")
	if len(downOrgs) != 0 || len(upOrgs) != 0 || len(upFacs) != 0 {
		t.Fatalf("unexpected org/up anchors: downOrgs=%v upOrgs=%v upFacs=%v", downOrgs, upOrgs, upFacs)
	}
	if len(downFacs) != 1 || downFacs[0] != "/deposit/cl/chuquicamata-copper-mine" {
		t.Fatalf("expected deposit anchor in downstream fac-list, got %v", downFacs)
	}
}
