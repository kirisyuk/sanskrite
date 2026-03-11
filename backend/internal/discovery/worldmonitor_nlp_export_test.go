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
