package policy

import (
	"context"
	"errors"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type mergeTestFragmentsRepo struct {
	items   map[string]*horizonmodels.HorizonFragment
	getErrs map[string]error
}

func (r *mergeTestFragmentsRepo) Get(_ context.Context, id string) (*horizonmodels.HorizonFragment, error) {
	if r == nil {
		return nil, nil
	}
	if err, ok := r.getErrs[id]; ok {
		return nil, err
	}
	if r.items == nil {
		return nil, nil
	}
	return r.items[id], nil
}

func (r *mergeTestFragmentsRepo) GetByCanonicalPath(_ context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
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

func (r *mergeTestFragmentsRepo) Update(_ context.Context, frag *horizonmodels.HorizonFragment) error {
	if r.items == nil {
		r.items = map[string]*horizonmodels.HorizonFragment{}
	}
	r.items[frag.ID] = frag
	return nil
}

func (r *mergeTestFragmentsRepo) Delete(_ context.Context, id string) error {
	if r.items != nil {
		delete(r.items, id)
	}
	return nil
}

type mergeTestEnforcementRepo struct {
	existing []*PolicyEnforcementJob
	created  []*PolicyEnforcementJob
}

func (r *mergeTestEnforcementRepo) UpdateStatus(_ context.Context, _ string, _ EnforcementJobStatus, _ *EnforcementResult) error {
	return nil
}

func (r *mergeTestEnforcementRepo) ListByParentJob(_ context.Context, _ string, _ int) ([]*PolicyEnforcementJob, error) {
	return r.existing, nil
}

func (r *mergeTestEnforcementRepo) CreateBatch(_ context.Context, jobs []*PolicyEnforcementJob) error {
	r.created = append(r.created, jobs...)
	return nil
}

func TestExecuteMergeFragments_NotFoundDuplicate_SkipsAsSuccess(t *testing.T) {
	repo := &mergeTestFragmentsRepo{
		getErrs: map[string]error{
			"dup": errors.New("rpc error: code = NotFound desc = missing"),
		},
	}
	exec := NewEnforcementExecutor(repo, nil, nil)

	result, err := exec.executeMergeFragments(context.Background(), &PolicyEnforcementJob{
		Task: EnforcementTask{
			Type:     TaskMergeFragments,
			TargetID: "dup",
			Params: map[string]interface{}{
				"canonicalId": "can",
			},
		},
	})
	if err != nil {
		t.Fatalf("executeMergeFragments returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if !result.Success {
		t.Fatalf("expected success result, got %+v", result)
	}
	if result.Action != "merge_fragments_skipped" {
		t.Fatalf("expected merge_fragments_skipped action, got %q", result.Action)
	}
}

func TestCreateEnforcementJobs_DeduplicatesConflictingMergePairs(t *testing.T) {
	exec := NewEnforcementExecutor(nil, nil, nil)

	violations := []PolicyViolation{
		{
			ID:          "v1",
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskMergeFragments,
				TargetID: "frag-a",
				Params: map[string]interface{}{
					"canonicalId": "frag-b",
				},
			},
		},
		{
			ID:          "v2",
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskMergeFragments,
				TargetID: "frag-b",
				Params: map[string]interface{}{
					"canonicalId": "frag-a",
				},
			},
		},
	}

	jobs, err := exec.CreateEnforcementJobs(context.Background(), "tenant-1", "parent-1", violations)
	if err != nil {
		t.Fatalf("CreateEnforcementJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 merge job after conflict dedup, got %d", len(jobs))
	}
}

func TestCreateEnforcementJobs_SkipsMergePairIfExistingOppositePending(t *testing.T) {
	repo := &mergeTestEnforcementRepo{
		existing: []*PolicyEnforcementJob{
			{
				ID:          "old-1",
				ParentJobID: "parent-2",
				Status:      EnforcementStatusPending,
				Task: EnforcementTask{
					Type:     TaskMergeFragments,
					TargetID: "frag-a",
					Params: map[string]interface{}{
						"canonicalId": "frag-b",
					},
				},
			},
		},
	}
	exec := NewEnforcementExecutor(nil, nil, repo)

	violations := []PolicyViolation{
		{
			ID:          "v3",
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskMergeFragments,
				TargetID: "frag-b",
				Params: map[string]interface{}{
					"canonicalId": "frag-a",
				},
			},
		},
	}

	jobs, err := exec.CreateEnforcementJobs(context.Background(), "tenant-1", "parent-2", violations)
	if err != nil {
		t.Fatalf("CreateEnforcementJobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs because opposite merge pair already pending, got %d", len(jobs))
	}
}
