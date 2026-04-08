package horizon

import (
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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

func TestIsLensChatLogisticsQueryRecognizesRussianPrompt(t *testing.T) {
	if !isLensChatLogisticsQuery("Какие маршруты поставок лука есть в Казахстане?") {
		t.Fatal("expected russian supply-route question to be treated as logistics")
	}
	variants := buildLensChatQueryVariants("Какие маршруты поставок лука есть в Казахстане?", nil, "", nil)
	joined := ""
	for _, variant := range variants {
		joined += variant + "\n"
	}
	if !strings.Contains(joined, "продукты поставщики покупатели маршруты логистика") {
		t.Fatalf("expected russian logistics expansion in query variants, got %q", joined)
	}
}

func TestLensChatSystemInstructionKeepsProductSpecificGroundingGuardrail(t *testing.T) {
	instruction := lensChatSystemInstruction()
	if !strings.Contains(instruction, "do not collapse the answer into a broader product family") {
		t.Fatalf("expected product-specific anti-broadening guardrail, got %q", instruction)
	}
	if !strings.Contains(instruction, "Do not claim that product-specific information is missing") {
		t.Fatalf("expected product-specific missing-data guardrail, got %q", instruction)
	}
}

func TestBuildLensChatSynthesisPromptPreservesNarrowGroundedSlice(t *testing.T) {
	prompt := buildLensChatSynthesisPrompt(
		&models.Lens{ID: "lens-1", Name: "Kazakhstan vegetables"},
		"Какие точки происхождения лука есть в Казахстане?",
		nil,
		[]lensChatInternalHit{{
			ID:    "hit-1",
			Title: "Assembly graph: vegetables (+12)",
			Kind:  "route_artifact",
			Score: 0.9,
			StructData: map[string]any{
				"canonicalPaths": []string{"/fac/kz/zhambyl-region-onion-farm", "/product/agricultural/crop/fresh/onions"},
				"questionHints":  []string{"/product/finished/mechanical/assembly/onions"},
			},
		}},
		[]lensChatPreparedEvidenceDoc{{
			ID:             "doc-1",
			Kind:           "route_artifact",
			Title:          "Assembly graph: vegetables (+12)",
			Content:        "Facilities: /fac/kz/zhambyl-region-onion-farm, /fac/kz/shengeldinsky-onion-storage",
			CanonicalPaths: []string{"/fac/kz/zhambyl-region-onion-farm", "/fac/kz/shengeldinsky-onion-storage"},
		}},
		[]string{"/fac/kz/zhambyl-region-onion-farm", "/fac/kz/shengeldinsky-onion-storage"},
	)
	if !strings.Contains(prompt, "Preserve the narrowest grounded slice that answers the question") {
		t.Fatalf("expected narrow-slice guardrail in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Do not claim that information is missing if the evidence pack already contains") {
		t.Fatalf("expected anti-missing-data guardrail in prompt, got %q", prompt)
	}
}
