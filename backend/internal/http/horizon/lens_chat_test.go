package horizon

import (
	"testing"
	"time"
)

func TestClassifyLensChatAnalyticalIntentCurrentSignal(t *testing.T) {
	intent := classifyLensChatAnalyticalIntent("What disruptions are affecting Saudi Arabia right now and why do they matter downstream?")
	if intent != "current_signal" {
		t.Fatalf("expected current_signal, got %q", intent)
	}
}

func TestRerankLensChatHitsPrioritizesFreshSignalLandingForCurrentSignal(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hits := []lensChatInternalHit{
		{
			ID:    "overview",
			Title: "Saudi Arabia Oil Export Diversification",
			Kind:  "overview",
			Score: 0.95,
			StructData: map[string]any{
				"kind":           "overview",
				"title":          "Saudi Arabia Oil Export Diversification",
				"canonicalPaths": []string{"/loc/sa", "/fac/sa/east-west-pipeline", "/fac/int/bab-el-mandeb-strait"},
				"questionHints":  []string{"Saudi Arabia oil export diversification"},
				"corpusId":       "lens-rich",
			},
		},
		{
			ID:    "signal",
			Title: "Fresh signal around /loc/int/strait-of-hormuz",
			Kind:  "signal_landing_summary",
			Score: 0.72,
			StructData: map[string]any{
				"kind":           "signal_landing_summary",
				"title":          "Fresh signal around /loc/int/strait-of-hormuz",
				"canonicalPaths": []string{"/loc/sa", "/loc/int/strait-of-hormuz", "/loc/ir"},
				"eventCoreRefs":  []string{"/loc/int/strait-of-hormuz", "/loc/ir"},
				"haloRefs":       []string{"/product/raw/hydrocarbon/liquefaction/lng"},
				"questionHints":  []string{"Hormuz disruptions", "Saudi Arabia energy"},
				"updatedAt":      now,
				"corpusId":       "lens-rich",
			},
		},
	}

	ranked := rerankLensChatHits(
		"What disruptions are affecting Saudi Arabia right now and why do they matter downstream?",
		hits,
		[]string{"/loc/sa"},
		"lens-rich",
		2,
	)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(ranked))
	}
	if ranked[0].ID != "signal" {
		t.Fatalf("expected fresh signal hit first, got %q", ranked[0].ID)
	}
}
