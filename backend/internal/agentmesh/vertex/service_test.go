package vertex

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func TestModelNameForCapabilityRoutesScoutReasoningTasks(t *testing.T) {
	svc := &Service{
		models: ModelAliases{
			FlashLite:      "gemini-3.1-flash-lite-preview",
			FlashReasoning: "gemini-3-flash-preview",
			Pro:            "gemini-3.1-pro-preview",
		},
	}

	cases := []struct {
		capability string
		review     bool
		want       string
	}{
		{capability: "scout.plan", want: "gemini-3-flash-preview"},
		{capability: "scout.decide", want: "gemini-3-flash-preview"},
		{capability: "scout.bridge", want: "gemini-3-flash-preview"},
		{capability: "scout.critic", want: "gemini-3-flash-preview"},
		{capability: "scout.research", want: "gemini-3.1-flash-lite-preview"},
		{capability: "scout.aggregate", want: "gemini-3.1-flash-lite-preview"},
		{capability: "scout.review", want: "gemini-3.1-pro-preview"},
		{capability: "scout.review", review: true, want: "gemini-3.1-pro-preview"},
	}

	for _, tc := range cases {
		if got := svc.ModelNameForCapability(tc.capability, tc.review); got != tc.want {
			t.Fatalf("ModelNameForCapability(%q, review=%v) = %q, want %q", tc.capability, tc.review, got, tc.want)
		}
	}
}

func TestCapabilityGroundingUsesSearchOnlyWhereUseful(t *testing.T) {
	cases := []struct {
		capability string
		review     bool
		wantSearch bool
		wantData   bool
	}{
		{capability: "scout.research", wantSearch: true, wantData: true},
		{capability: "scout.bridge", wantSearch: true, wantData: true},
		{capability: "scout.plan", wantSearch: false, wantData: false},
		{capability: "scout.decide", wantSearch: false, wantData: false},
		{capability: "scout.critic", wantSearch: false, wantData: false},
		{capability: "scout.aggregate", wantSearch: false, wantData: false},
		{capability: "scout.review", review: true, wantSearch: false, wantData: false},
	}

	for _, tc := range cases {
		gotSearch, gotData := capabilityGrounding(tc.capability, tc.review)
		if gotSearch != tc.wantSearch || gotData != tc.wantData {
			t.Fatalf("capabilityGrounding(%q, review=%v) = (%v,%v), want (%v,%v)", tc.capability, tc.review, gotSearch, gotData, tc.wantSearch, tc.wantData)
		}
	}
}

func TestScoutDecidePromptUsesLatestOutcomeAsPrimaryTruth(t *testing.T) {
	prompt := buildPrompt(&contracts.MeshTask{Capability: "scout.decide"}, false, "{}")
	if !strings.Contains(prompt, "latestOutcome/recentOutcomes as the primary truth") {
		t.Fatalf("expected decide prompt to mention latestOutcome/recentOutcomes, got %q", prompt)
	}
	if !strings.Contains(prompt, "systemError=true") {
		t.Fatalf("expected decide prompt to mention retryable system errors, got %q", prompt)
	}
}
