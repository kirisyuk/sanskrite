package scout

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestJobAllowsDeferredScopeAdd_usesWorkscopePolicy(t *testing.T) {
	scope := workscope.FromLensStrictWorkScope("tenant", "project", "lens-1", []string{"/org/a"})
	request := workscope.NewLensGraphEnrichmentRequest(scope, workscope.MutationModePersistWorldScopeRefsOnly)
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: workscope.ApplyRequestToState(nil, request),
		},
	}
	if !jobAllowsDeferredScopeAdd(job, "/org/a") {
		t.Fatalf("expected scoped anchor to be addable")
	}
	if jobAllowsDeferredScopeAdd(job, "/org/b") {
		t.Fatalf("unexpected auto-add outside requested scope")
	}
}

func TestJobAllowsDeferredScopeAdd_legacyStateStaysPermissive(t *testing.T) {
	job := &horizonmodels.ScoutingJob{}
	if !jobAllowsDeferredScopeAdd(job, "/org/a") {
		t.Fatalf("legacy job should remain permissive")
	}
}
