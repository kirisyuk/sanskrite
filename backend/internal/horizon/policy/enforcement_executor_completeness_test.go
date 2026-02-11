package policy

import (
	"context"
	"testing"
)

func TestCreateEnforcementJobs_CreatesEnrichEdgeTaskForCompletenessViolation(t *testing.T) {
	exec := &EnforcementExecutor{}
	violations := []PolicyViolation{
		{
			ID:            "v1",
			Type:          ViolationMissingField,
			Rule:          "edge_completeness_owns_percent",
			EdgeID:        "edge-1",
			AutoFixable:   false,
			Description:   "OWNS edge missing ownership percentage",
			ExpectedValue: "metrics.ownership_percent",
		},
	}

	jobs, err := exec.CreateEnforcementJobs(context.Background(), "tenant-1", "job-1", violations)
	if err != nil {
		t.Fatalf("CreateEnforcementJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 enforcement job, got %d", len(jobs))
	}
	if jobs[0].Task.Type != TaskEnrichEdge {
		t.Fatalf("expected task type %s, got %s", TaskEnrichEdge, jobs[0].Task.Type)
	}
	if jobs[0].Task.TargetID != "edge-1" {
		t.Fatalf("expected target edge-1, got %s", jobs[0].Task.TargetID)
	}
}
