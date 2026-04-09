package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestVertexAIServiceLiveSmoke is a live integration smoke test for VertexAIService.
// Requires Application Default Credentials (gcloud auth application-default login).
// Skipped in CI unless VERTEX_AI_LIVE_TEST=1 is set.
func TestVertexAIServiceLiveSmoke(t *testing.T) {
	if os.Getenv("VERTEX_AI_LIVE_TEST") != "1" {
		t.Skip("set VERTEX_AI_LIVE_TEST=1 to run live Vertex AI tests")
	}

	projectID := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if projectID == "" {
		projectID = "nv-horizon"
	}
	location := "us-central1"
	dataStoreID := "horizon-fragments-store"

	mm := NewAIModelManager()

	svc, err := NewVertexAIService(projectID, location, mm)
	if err != nil {
		t.Fatalf("NewVertexAIService: %v", err)
	}
	svc.WithDataStore(dataStoreID)

	if !svc.IsConfigured() {
		t.Fatal("service is not configured")
	}
	if !svc.HasDataStore() {
		t.Fatal("datastore not configured")
	}

	t.Logf("VertexAIService: project=%s location=%s dataStore=%s", projectID, location, svc.DataStorePath())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Test 1: Simple generation (no grounding)
	t.Run("SimpleGeneration", func(t *testing.T) {
		task := &AITask{
			TaskType:     TaskTypeContentGeneration,
			InputContent: "What is 2+2? Reply with just the number.",
			AIRequirements: AIRequirements{
				TextGenerationCapability: true,
				MaxOutputSize:            100,
			},
		}

		result, err := svc.ExecuteTask(ctx, task, "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ExecuteTask (simple): %v", err)
		}
		t.Logf("Simple result: %q (model=%s)", result.Content, result.Model)
		if !strings.Contains(result.Content, "4") {
			t.Errorf("expected '4' in response, got: %s", result.Content)
		}
	})

	// Test 2: Google Search grounding only
	t.Run("GoogleSearchGrounding", func(t *testing.T) {
		task := &AITask{
			TaskType:     TaskTypeSearch,
			InputContent: "What is the current population of Saudi Arabia?",
			AIRequirements: AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true,
				MaxOutputSize:             500,
			},
		}

		result, err := svc.ExecuteTask(ctx, task, "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ExecuteTask (google grounding): %v", err)
		}
		t.Logf("Google Search result: %q", truncate(result.Content, 200))
		t.Logf("Citations: %d", len(result.Citations))
		for i, c := range result.Citations {
			t.Logf("  citation[%d]: %s — %s", i, c.Title, c.URL)
		}
	})

	// Test 3: Vertex AI Search grounding (datastore)
	t.Run("VertexAISearchGrounding", func(t *testing.T) {
		task := &AITask{
			TaskType:     TaskTypeKnowledgeSynthesis,
			InputContent: "What organizations related to oil and energy exist in Saudi Arabia? List their canonical paths if you find any.",
			AIRequirements: AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true, // Also enable Google Search
				MaxOutputSize:             1000,
			},
		}

		result, err := svc.ExecuteTask(ctx, task, "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ExecuteTask (vertex search grounding): %v", err)
		}
		t.Logf("Vertex AI Search result: %q", truncate(result.Content, 300))
		t.Logf("Citations: %d", len(result.Citations))
		datastoreCitations := 0
		for i, c := range result.Citations {
			source := "web"
			if strings.HasPrefix(c.Title, "[DATASTORE]") {
				source = "DATASTORE"
				datastoreCitations++
			}
			t.Logf("  citation[%d] [%s]: %s — %s", i, source, c.Title, c.URL)
		}
		t.Logf("Datastore citations: %d / %d total", datastoreCitations, len(result.Citations))
	})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func TestNormalizeVertexModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "vertex-gemini-3-flash-preview", want: "gemini-3-flash-preview"},
		{in: "vertex-gemini-2.5-pro", want: "gemini-2.5-pro"},
		{in: "gemini-2.5-flash", want: "gemini-2.5-flash"},
		{in: "  vertex-gemini-3-pro-preview  ", want: "gemini-3-pro-preview"},
	}

	for _, tc := range tests {
		got := normalizeVertexModelID(tc.in)
		if got != tc.want {
			t.Fatalf("normalizeVertexModelID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSelectVertexLocationForModel(t *testing.T) {
	tests := []struct {
		name     string
		location string
		model    string
		want     string
	}{
		{
			name:     "gemini3 preview forced to global",
			location: "us-central1",
			model:    "gemini-3-flash-preview",
			want:     "global",
		},
		{
			name:     "gemini3.1 preview forced to global",
			location: "us-central1",
			model:    "gemini-3.1-flash-lite-preview",
			want:     "global",
		},
		{
			name:     "gemini2 keeps configured location",
			location: "us-central1",
			model:    "gemini-2.5-pro",
			want:     "us-central1",
		},
		{
			name:     "empty configured location falls back",
			location: "",
			model:    "gemini-2.5-pro",
			want:     "us-central1",
		},
	}

	for _, tc := range tests {
		got := selectVertexLocationForModel(tc.location, tc.model)
		if got != tc.want {
			t.Fatalf("%s: selectVertexLocationForModel(%q, %q) = %q, want %q",
				tc.name, tc.location, tc.model, got, tc.want)
		}
	}
}
