package scout

import "testing"

func TestBudgetFanoutScaleForPressure(t *testing.T) {
	t.Run("no_pressure", func(t *testing.T) {
		if got := budgetFanoutScaleForPressure(0); got != 1.0 {
			t.Fatalf("expected 1.0, got %v", got)
		}
	})

	t.Run("near_cap", func(t *testing.T) {
		if got := budgetFanoutScaleForPressure(0.98); got != 0.55 {
			t.Fatalf("expected 0.55, got %v", got)
		}
	})

	t.Run("over_cap", func(t *testing.T) {
		if got := budgetFanoutScaleForPressure(1.4); got != 0.25 {
			t.Fatalf("expected 0.25, got %v", got)
		}
	})

	t.Run("severe_overrun", func(t *testing.T) {
		if got := budgetFanoutScaleForPressure(2.2); got != 0.15 {
			t.Fatalf("expected 0.15, got %v", got)
		}
	})
}

func TestScaleCapByBudget(t *testing.T) {
	t.Run("scales_down", func(t *testing.T) {
		got := scaleCapByBudget(20, 1, 0.35)
		if got != 7 {
			t.Fatalf("expected 7, got %d", got)
		}
	})

	t.Run("respects_min", func(t *testing.T) {
		got := scaleCapByBudget(3, 1, 0.1)
		if got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	})

	t.Run("zero_base", func(t *testing.T) {
		got := scaleCapByBudget(0, 1, 0.5)
		if got != 0 {
			t.Fatalf("expected 0, got %d", got)
		}
	})
}

func TestShouldFreezeDepthByBudgetPressure(t *testing.T) {
	if shouldFreezeDepthByBudgetPressure(1.9) {
		t.Fatalf("expected no freeze below severe threshold")
	}
	if !shouldFreezeDepthByBudgetPressure(2.0) {
		t.Fatalf("expected freeze at severe threshold")
	}
}
