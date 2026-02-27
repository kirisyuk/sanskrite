package lens

import "testing"

func TestNewPlannerTierBudget_PrioritizesHighTier(t *testing.T) {
	b := newPlannerTierBudget(
		2,
		[]string{"high", "normal", "low"},
		[]string{"/org/nl/nexperia", "/person/kim-jong-un", "/market/global/technology/semiconductors"},
		nil,
		false,
	)
	if b == nil {
		t.Fatalf("expected non-nil tier budget")
	}
	if !b.canSchedule("high", 0, 2) {
		t.Fatalf("expected first high-tier slot")
	}
	b.markScheduled("high")
	if !b.canSchedule("high", 1, 2) {
		t.Fatalf("expected second high-tier slot")
	}
	b.markScheduled("high")
	if b.canSchedule("normal", 2, 2) {
		t.Fatalf("expected no slot after reaching max root jobs")
	}
}

func TestInferPlannerPriorityTier_SupplyChain(t *testing.T) {
	if got := inferPlannerPriorityTier("/org/nl/nexperia", false, true); got != "high" {
		t.Fatalf("expected high for org in supply chain, got %q", got)
	}
	if got := inferPlannerPriorityTier("/market/global/technology/ev-batteries", false, true); got != "normal" {
		t.Fatalf("expected normal for market in supply chain, got %q", got)
	}
	if got := inferPlannerPriorityTier("/unknown/x", false, true); got != "low" {
		t.Fatalf("expected low fallback, got %q", got)
	}
	if got := inferPlannerPriorityTier("/domain/v1/supply-chain/geo/de", true, true); got != "high" {
		t.Fatalf("expected high for deferred domain path, got %q", got)
	}
}
