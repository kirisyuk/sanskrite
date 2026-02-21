package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeScenarioMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ScenarioModeApply},
		{in: "apply", want: ScenarioModeApply},
		{in: "preview", want: ScenarioModePreview},
		{in: " PREVIEW ", want: ScenarioModePreview},
		{in: "unknown", want: ScenarioModeApply},
	}
	for _, tt := range tests {
		if got := normalizeScenarioMode(tt.in); got != tt.want {
			t.Fatalf("normalizeScenarioMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCollectAccumulatedFromState_TracksAxisAndPolarity(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		State: map[string]horizonmodels.MetricValue{
			"/metric/risk/financial/settlement/vat_rate_increase":        {Value: 0.40},
			"/metric/risk/financial/settlement/other_mechanism":          {Value: 0.55},
			"/metric/opportunity/financial/settlement/vat_rate_increase": {Value: 0.25},
			"/metric/risk/legal/compliance/vat_rate_increase":            {Value: 0.18},
		},
	}
	out := CollectAccumulatedFromState(frag, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 accumulated signals, got %d", len(out))
	}
	seen := map[string]AccumulatedSignal{}
	for _, s := range out {
		seen[s.Axis+"|"+s.Polarity] = s
	}
	if got := seen[horizonmodels.AxisFinancialSettlement+"|risk"].Amplitude; got != 0.55 {
		t.Fatalf("unexpected risk settlement amplitude: %.2f", got)
	}
	if got := seen[horizonmodels.AxisFinancialSettlement+"|opportunity"].Amplitude; got != 0.25 {
		t.Fatalf("unexpected opportunity settlement amplitude: %.2f", got)
	}
	if got := seen[horizonmodels.AxisLegalCompliance+"|risk"].Amplitude; got != 0.18 {
		t.Fatalf("unexpected legal compliance amplitude: %.2f", got)
	}
}

func TestMergeAccumulatedMaps_PrefersLargerAmplitude(t *testing.T) {
	dst := map[string]map[string]AccumulatedSignal{
		"f1": {
			"financial/settlement|risk": {Axis: "financial/settlement", Polarity: "risk", Amplitude: 0.30, HopCount: 2},
		},
	}
	src := map[string][]AccumulatedSignal{
		"f1": {
			{Axis: "financial/settlement", Polarity: "risk", Amplitude: 0.42, HopCount: 3},
			{Axis: "legal/compliance", Polarity: "risk", Amplitude: 0.20, HopCount: 1},
		},
	}
	mergeAccumulatedMaps(dst, src)
	if got := dst["f1"]["financial/settlement|risk"].Amplitude; got != 0.42 {
		t.Fatalf("expected merged amplitude 0.42, got %.2f", got)
	}
	if _, ok := dst["f1"]["legal/compliance|risk"]; !ok {
		t.Fatalf("expected legal/compliance entry after merge")
	}
}

func TestNormalizeFragmentIDs_DedupAndTrim(t *testing.T) {
	got := normalizeFragmentIDs([]string{"  f1 ", "", "f2", "f1", "  ", "f3"})
	if len(got) != 3 {
		t.Fatalf("expected 3 ids, got %d: %v", len(got), got)
	}
	if got[0] != "f1" || got[1] != "f2" || got[2] != "f3" {
		t.Fatalf("unexpected ids order/content: %v", got)
	}
}
