package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type inMemoryFragmentsRepo struct {
	items map[string]*horizonmodels.HorizonFragment
}

func (r *inMemoryFragmentsRepo) Get(_ context.Context, id string) (*horizonmodels.HorizonFragment, error) {
	if r == nil || r.items == nil {
		return nil, nil
	}
	return r.items[id], nil
}

func (r *inMemoryFragmentsRepo) GetByCanonicalPath(_ context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
	if r == nil || r.items == nil {
		return nil, nil
	}
	for _, frag := range r.items {
		if frag != nil && frag.CanonicalPath == canonicalPath {
			return frag, nil
		}
	}
	return nil, nil
}

func (r *inMemoryFragmentsRepo) Update(_ context.Context, frag *horizonmodels.HorizonFragment) error {
	if r.items == nil {
		r.items = map[string]*horizonmodels.HorizonFragment{}
	}
	r.items[frag.ID] = frag
	return nil
}

func (r *inMemoryFragmentsRepo) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func TestCreateEnforcementJobs_SkipsFixLegalFormForNonOrgPath(t *testing.T) {
	repo := &inMemoryFragmentsRepo{
		items: map[string]*horizonmodels.HorizonFragment{
			"f1": {ID: "f1", CanonicalPath: "/person/john-doe"},
		},
	}
	exec := NewEnforcementExecutor(repo, nil, nil)

	violations := []PolicyViolation{
		{
			ID:          "v1",
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskFixLegalForm,
				TargetID: "f1",
				Params: map[string]interface{}{
					"currentPath": "/person/john-doe",
					"correctPath": "/person/john-doe-llc",
					"legalForm":   "llc",
				},
			},
		},
	}

	jobs, err := exec.CreateEnforcementJobs(context.Background(), "tenant", "parent-job", violations)
	if err != nil {
		t.Fatalf("CreateEnforcementJobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %d", len(jobs))
	}
}

func TestExecuteFixLegalForm_SkipsNonOrgPath(t *testing.T) {
	repo := &inMemoryFragmentsRepo{
		items: map[string]*horizonmodels.HorizonFragment{
			"f2": {ID: "f2", CanonicalPath: "/person/john-doe"},
		},
	}
	exec := NewEnforcementExecutor(repo, nil, nil)

	result, err := exec.executeFixLegalForm(context.Background(), EnforcementTask{
		Type:     TaskFixLegalForm,
		TargetID: "f2",
		Params: map[string]interface{}{
			"currentPath": "/person/john-doe",
			"correctPath": "/person/john-doe-llc",
			"legalForm":   "llc",
		},
	})
	if err != nil {
		t.Fatalf("executeFixLegalForm failed: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %+v", result)
	}
	if result.Action != "fix_legal_form_skipped" {
		t.Fatalf("expected fix_legal_form_skipped action, got %q", result.Action)
	}
}
