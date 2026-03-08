package propagation

import "testing"

func TestAuditTraceBreakpointsSummarizesReasonsAndFamilies(t *testing.T) {
	trace := []SignalPropagationTraceEvent{
		{
			Outcome:      "dropped",
			Reason:       "damped_below_threshold",
			EdgeType:     "DEPENDS_ON",
			Channel:      "economic",
			Scope:        "cross_border",
			Spillover:    "cross_border_economic",
			ToPath:       "/fac/cn/lng-intensive-plant",
			AmplitudeOut: 0.008,
			Hops:         2,
		},
		{
			Outcome:      "dropped",
			Reason:       "transfer_skip",
			EdgeType:     "LOCATED_IN",
			Channel:      "geopolitical",
			Scope:        "cross_border",
			Spillover:    "cross_border_geopolitical",
			ToPath:       "/market/de/energy/lng",
			AmplitudeOut: 0.020,
			Hops:         1,
		},
		{
			Outcome:      "dropped",
			Reason:       "hop_limit",
			EdgeType:     "SUPPLIES_TO",
			Channel:      "economic",
			Scope:        "cross_border",
			Spillover:    "cross_border_economic",
			ToPath:       "/deposit/qa/natural-gas/north-field",
			AmplitudeOut: 0.110,
			Hops:         4,
		},
	}

	summary := AuditTraceBreakpoints(trace, 4)
	if summary.TotalDrops != 3 {
		t.Fatalf("TotalDrops = %d, want 3", summary.TotalDrops)
	}
	if summary.ThresholdDrops != 1 {
		t.Fatalf("ThresholdDrops = %d, want 1", summary.ThresholdDrops)
	}
	if summary.TransferSkips != 1 {
		t.Fatalf("TransferSkips = %d, want 1", summary.TransferSkips)
	}
	if summary.HopLimitDrops != 1 {
		t.Fatalf("HopLimitDrops = %d, want 1", summary.HopLimitDrops)
	}
	if summary.PathBreakpoints == 0 || len(summary.TopFamilies) == 0 {
		t.Fatalf("expected path breakpoint families, got %+v", summary)
	}
	if len(summary.TopReasons) == 0 {
		t.Fatalf("expected reason summaries, got %+v", summary)
	}
}
