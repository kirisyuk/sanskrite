package services

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestBuildSectionPromptIncludesGraphEvidencePack(t *testing.T) {
	svc := &ArtifactExecutorService{}
	section := models.ArtifactSection{
		ID:     "what_this_network_is",
		Title:  "What This Network Is",
		Prompt: "Explain what kind of industrial system this is.",
	}
	context := &GatheredContext{
		GraphContexts: []GraphContext{
			{
				Kind:    "lens_supply_chain_report",
				Subject: "Visible Supply Chain",
				Summary: "Graph-native substrate for an industrial network.",
				Sections: map[string]GraphSectionContext{
					"what_this_network_is": {
						ID:                 "what_this_network_is",
						Title:              "What This Network Is",
						Description:        "Preamble section",
						Prompt:             "Use only visible facilities and product streams.",
						DeterministicDraft: "The visible network links manufacturing to launch and operations.",
						FactsJSON:          "{\n  \"visiblePaths\": 12\n}",
					},
				},
			},
		},
	}

	prompt := svc.buildSectionPrompt(section, context, models.GenerationSettings{PreferredLanguage: "en"}, nil, nil)
	expected := []string{
		"Graph-Native Evidence",
		"Visible Supply Chain",
		"Section Evidence Pack: What This Network Is",
		"The visible network links manufacturing to launch and operations.",
		"```json",
		"\"visiblePaths\": 12",
	}
	for _, item := range expected {
		if !strings.Contains(prompt, item) {
			t.Fatalf("expected prompt to contain %q", item)
		}
	}
}
