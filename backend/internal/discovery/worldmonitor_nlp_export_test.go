package discovery

import (
	"testing"

	discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"
)

func TestBuildHorizonMonitorWeakLabelAnalysis(t *testing.T) {
	out := BuildHorizonMonitorWeakLabel(discoverynlp.DocumentInput{
		SourceHost: "warontherocks.com",
		Title:      "Analysis: what the Red Sea crisis means for global shipping",
		Summary:    "Weekly roundup and outlook for maritime disruption.",
	})
	if out == nil {
		t.Fatalf("expected weak label")
	}
	if out.IsEvent {
		t.Fatalf("expected non-event/analysis classification, got %+v", out)
	}
	if out.EventClass == "" {
		t.Fatalf("expected event class, got %+v", out)
	}
	if out.ShouldOnlySignal {
		t.Fatalf("analysis weak label must not request signal-only propagation, got %+v", out)
	}
}

func TestBuildHorizonMonitorWeakLabelAnalysisFromSyndicatedPublisherAlias(t *testing.T) {
	out := BuildHorizonMonitorWeakLabel(discoverynlp.DocumentInput{
		SourceHost: "news.google.com",
		Title:      "Lebanese Should Not Despair - Middle East Institute",
		Summary:    "Lebanese Should Not Despair  Middle East Institute",
	})
	if out == nil {
		t.Fatalf("expected weak label")
	}
	if out.IsEvent || out.ShouldCreateZone || out.ShouldOnlySignal {
		t.Fatalf("syndicated think-tank text must not be treated as event, got %+v", out)
	}
	if out.EventClass != "analysis" {
		t.Fatalf("expected analysis class for syndicated think-tank text, got %+v", out)
	}
}

func TestMergeArticleAssessments_RuleGuardWinsOnWeakModelEvent(t *testing.T) {
	rule := &discoverynlp.ArticleAssessment{
		IsEvent:          false,
		EventClass:       "analysis",
		EventConfidence:  0.82,
		AnalysisScore:    0.9,
		ShouldCreateZone: false,
		RejectionReason:  "analysis_roundup",
		Reasons:          []string{"rule:analysis"},
		Raw:              map[string]interface{}{"backend": "rules"},
	}
	model := &discoverynlp.ArticleAssessment{
		IsEvent:          true,
		EventClass:       "real_event",
		EventConfidence:  0.53,
		ShouldCreateZone: true,
		Reasons:          []string{"model:real_event"},
		Raw:              map[string]interface{}{"backend": "sklearn"},
	}
	out := mergeArticleAssessments(discoverynlp.DocumentInput{SourceHost: "example.com"}, rule, model)
	if out == nil {
		t.Fatal("expected merged assessment")
	}
	if out.IsEvent || out.ShouldCreateZone {
		t.Fatalf("expected rule guard to keep non-event decision, got %+v", out)
	}
	if out.EventClass != "analysis" {
		t.Fatalf("expected analysis class to survive, got %+v", out)
	}
}

func TestMergeArticleAssessments_ModelVetoWinsOnAnalysisHeavyHost(t *testing.T) {
	rule := &discoverynlp.ArticleAssessment{
		IsEvent:             true,
		EventClass:          "real_event",
		EventConfidence:     0.82,
		ConcreteOutageScore: 0.12,
		ShouldCreateZone:    true,
		Reasons:             []string{"rule:real_event"},
	}
	model := &discoverynlp.ArticleAssessment{
		IsEvent:             false,
		EventClass:          "analysis",
		EventConfidence:     0.89,
		ConcreteOutageScore: 0.10,
		ShouldCreateZone:    false,
		RejectionReason:     "analysis_roundup",
		Reasons:             []string{"model:analysis"},
	}
	out := mergeArticleAssessments(discoverynlp.DocumentInput{SourceHost: "warontherocks.com"}, rule, model)
	if out == nil {
		t.Fatal("expected merged assessment")
	}
	if out.IsEvent || out.ShouldCreateZone || out.ShouldOnlySignal {
		t.Fatalf("expected analysis-heavy host veto to suppress event path, got %+v", out)
	}
	if out.EventClass != "analysis" {
		t.Fatalf("expected analysis class after model veto, got %+v", out)
	}
}

func TestShouldEscalateArticleToLLM_ConcreteCyberOutage(t *testing.T) {
	doc := discoverynlp.DocumentInput{
		SourceHost: "reuters.com",
		Title:      "Cyberattack forces major Australian port operator offline in Sydney",
		Summary:    "Port operations remain suspended after ransomware attack disrupted container handling.",
	}
	base := &discoverynlp.ArticleAssessment{
		IsEvent:             true,
		EventClass:          "real_event",
		EventConfidence:     0.86,
		ConcreteOutageScore: 0.86,
		ShouldCreateZone:    true,
	}
	if !shouldEscalateArticleToLLM(doc, horizonMonitorNewsClassification{Category: "cyber", Level: "high"}, base) {
		t.Fatalf("expected concrete cyber outage to escalate to LLM adjudication")
	}
}

func TestMergeArticleAssessmentWithLLM_PromotesHighConfidenceConcreteEvent(t *testing.T) {
	base := &discoverynlp.ArticleAssessment{
		IsEvent:             false,
		EventClass:          "analysis",
		EventConfidence:     0.62,
		AnalysisScore:       0.72,
		ConcreteOutageScore: 0.44,
		ShouldCreateZone:    false,
		RejectionReason:     "analysis_roundup",
	}
	llm := &horizonMonitorLLMArticleAdjudication{
		SchemaVersion:      "v1",
		IsEvent:            true,
		EventClass:         "real_event",
		EventConfidence:    0.96,
		ShouldCreateZone:   true,
		ConditionType:      "cyber_disruption",
		Place:              "Sydney",
		CountryCode:        "AU",
		GeoConfidence:      0.95,
		Reason:             "direct cyber outage",
	}
	out := mergeArticleAssessmentWithLLM(discoverynlp.DocumentInput{SourceHost: "reuters.com"}, base, llm)
	if out == nil {
		t.Fatal("expected merged assessment")
	}
	if !out.IsEvent || !out.ShouldCreateZone {
		t.Fatalf("expected llm to promote concrete event, got %+v", out)
	}
	if got := out.Raw["llmPlace"]; got != "Sydney" {
		t.Fatalf("expected llm geo hint to be carried, got %#v", got)
	}
}

func TestMergeArticleAssessmentWithLLM_RejectsAnalysisAndDropsGeoHints(t *testing.T) {
	base := &discoverynlp.ArticleAssessment{
		IsEvent:          false,
		EventClass:       "noise",
		EventConfidence:  0.74,
		ShouldCreateZone: false,
		RejectionReason:  "non_event_text",
	}
	llm := &horizonMonitorLLMArticleAdjudication{
		SchemaVersion:      "v1",
		IsEvent:            false,
		EventClass:         "analysis",
		EventConfidence:    0.95,
		ShouldCreateZone:   false,
		Place:              "Red Sea",
		CountryCode:        "SA",
		GeoConfidence:      0.80,
		Reason:             "analysis only",
	}
	out := mergeArticleAssessmentWithLLM(discoverynlp.DocumentInput{SourceHost: "foreignpolicy.com"}, base, llm)
	if out == nil {
		t.Fatal("expected merged assessment")
	}
	if out.IsEvent || out.ShouldCreateZone {
		t.Fatalf("expected analysis veto to hold, got %+v", out)
	}
	if _, ok := out.Raw["llmPlace"]; ok {
		t.Fatalf("non-event llm geo hint must not be trusted, got %+v", out.Raw)
	}
}
