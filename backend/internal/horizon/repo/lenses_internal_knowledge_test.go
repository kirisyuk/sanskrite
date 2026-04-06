package repo

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestPreserveExistingInternalKnowledgeState(t *testing.T) {
	lens := &models.Lens{
		ID: "lens-1",
		ViewConfig: map[string]interface{}{
			"internalKnowledge": map[string]interface{}{
				"status":      "queued",
				"lastReason":  "assembly_ready",
				"temperature": "hot",
			},
			"other": "incoming",
		},
	}
	stored := &models.Lens{
		ID: "lens-1",
		ViewConfig: map[string]interface{}{
			"internalKnowledge": map[string]interface{}{
				"status":               "running",
				"temperatureLevel":     "warm",
				"pendingStageHash":     "abc123",
				"pendingStageDocCount": 28,
				"importOperation":      "operations/import-1",
			},
		},
	}

	preserveExistingInternalKnowledgeState(lens, stored)

	state := lensInternalKnowledgeViewState(lens.ViewConfig)
	if got := state["status"]; got != "running" {
		t.Fatalf("expected stored status to win, got %v", got)
	}
	if got := state["pendingStageHash"]; got != "abc123" {
		t.Fatalf("expected pending stage hash to be preserved, got %v", got)
	}
	if got := state["importOperation"]; got != "operations/import-1" {
		t.Fatalf("expected import operation to be preserved, got %v", got)
	}
	if got := lens.ViewConfig["other"]; got != "incoming" {
		t.Fatalf("expected non-internalKnowledge fields to remain untouched, got %v", got)
	}
}

func TestMergeInternalKnowledgeForLensUpdateAddsOnlyMissingIncomingKeys(t *testing.T) {
	existing := map[string]interface{}{
		"status":           "ready",
		"temperatureLevel": "full",
	}
	incoming := map[string]interface{}{
		"status":     "queued",
		"lastReason": "scope_delta_no_scout_jobs",
	}

	merged := mergeInternalKnowledgeForLensUpdate(existing, incoming)
	if got := merged["status"]; got != "ready" {
		t.Fatalf("expected existing status to be preserved, got %v", got)
	}
	if got := merged["lastReason"]; got != "scope_delta_no_scout_jobs" {
		t.Fatalf("expected missing incoming key to be added, got %v", got)
	}
}
