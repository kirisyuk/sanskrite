package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

// isNotFoundError checks if an error is a Firestore "not found" error.
func isNotFoundError(err error) bool {
	return grpcstatus.Code(err) == codes.NotFound
}

var ErrEnforcementJobNotFound = errors.New("enforcement job not found")

// EnforcementJobsRepository manages PolicyEnforcementJob documents in Firestore.
type EnforcementJobsRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewEnforcementJobsRepository creates a new EnforcementJobsRepository.
func NewEnforcementJobsRepository(client *firestore.Client) *EnforcementJobsRepository {
	return &EnforcementJobsRepository{
		col: client.Collection("horizon_enforcement_jobs"),
		db:  client,
	}
}

// Create creates a new enforcement job.
func (r *EnforcementJobsRepository) Create(ctx context.Context, job *policy.PolicyEnforcementJob) error {
	if job == nil {
		return ErrInvalidDocument
	}
	if job.ID == "" {
		return fmt.Errorf("job ID is required")
	}

	job.CreatedAt = time.Now().UTC()
	job.UpdatedAt = job.CreatedAt

	_, err := r.col.Doc(job.ID).Set(ctx, job)
	return err
}

// Get retrieves an enforcement job by ID.
func (r *EnforcementJobsRepository) Get(ctx context.Context, id string) (*policy.PolicyEnforcementJob, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil, ErrEnforcementJobNotFound
		}
		return nil, err
	}

	var job policy.PolicyEnforcementJob
	if err := snap.DataTo(&job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	if job.ID == "" {
		job.ID = snap.Ref.ID
	}

	return &job, nil
}

// Update updates an enforcement job.
func (r *EnforcementJobsRepository) Update(ctx context.Context, job *policy.PolicyEnforcementJob) error {
	if job == nil || job.ID == "" {
		return ErrInvalidDocument
	}

	job.UpdatedAt = time.Now().UTC()

	_, err := r.col.Doc(job.ID).Set(ctx, job)
	return err
}

// UpdateStatus updates only the status and result of an enforcement job.
func (r *EnforcementJobsRepository) UpdateStatus(ctx context.Context, id string, status policy.EnforcementJobStatus, result *policy.EnforcementResult) error {
	if id == "" {
		return ErrInvalidDocument
	}

	updates := []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updatedAt", Value: time.Now().UTC()},
	}

	if result != nil {
		updates = append(updates, firestore.Update{Path: "result", Value: result})
	}

	if status == policy.EnforcementStatusCompleted || status == policy.EnforcementStatusFailed {
		now := time.Now().UTC()
		updates = append(updates, firestore.Update{Path: "completedAt", Value: &now})
	}

	_, err := r.col.Doc(id).Update(ctx, updates)
	return err
}

// Delete deletes an enforcement job.
func (r *EnforcementJobsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// ListByParentJob lists all enforcement jobs for a parent job.
func (r *EnforcementJobsRepository) ListByParentJob(ctx context.Context, parentJobID string, limit int) ([]*policy.PolicyEnforcementJob, error) {
	if parentJobID == "" {
		return nil, ErrInvalidDocument
	}

	if limit <= 0 {
		limit = 100
	}

	q := r.col.Where("parentJobId", "==", parentJobID).
		OrderBy("createdAt", firestore.Desc).
		Limit(limit)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var jobs []*policy.PolicyEnforcementJob
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var job policy.PolicyEnforcementJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		if job.ID == "" {
			job.ID = doc.Ref.ID
		}
		jobs = append(jobs, &job)
	}

	return jobs, nil
}

// ListByStatus lists enforcement jobs by status.
func (r *EnforcementJobsRepository) ListByStatus(ctx context.Context, tenantID string, status policy.EnforcementJobStatus, limit int) ([]*policy.PolicyEnforcementJob, error) {
	if limit <= 0 {
		limit = 100
	}

	q := r.col.Where("tenantId", "==", tenantID).
		Where("status", "==", status).
		OrderBy("createdAt", firestore.Asc).
		Limit(limit)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var jobs []*policy.PolicyEnforcementJob
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var job policy.PolicyEnforcementJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		if job.ID == "" {
			job.ID = doc.Ref.ID
		}
		jobs = append(jobs, &job)
	}

	return jobs, nil
}

// ListPending lists pending enforcement jobs (FIFO order).
func (r *EnforcementJobsRepository) ListPending(ctx context.Context, tenantID string, limit int) ([]*policy.PolicyEnforcementJob, error) {
	return r.ListByStatus(ctx, tenantID, policy.EnforcementStatusPending, limit)
}

// CountByParentJob counts enforcement jobs by parent job ID.
func (r *EnforcementJobsRepository) CountByParentJob(ctx context.Context, parentJobID string) (total, completed, failed int, err error) {
	if parentJobID == "" {
		return 0, 0, 0, ErrInvalidDocument
	}

	jobs, err := r.ListByParentJob(ctx, parentJobID, 1000)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, job := range jobs {
		total++
		switch job.Status {
		case policy.EnforcementStatusCompleted:
			completed++
		case policy.EnforcementStatusFailed:
			failed++
		}
	}

	return total, completed, failed, nil
}

// CreateBatch creates multiple enforcement jobs in a batch.
func (r *EnforcementJobsRepository) CreateBatch(ctx context.Context, jobs []*policy.PolicyEnforcementJob) error {
	if len(jobs) == 0 {
		return nil
	}

	batch := r.db.Batch()
	now := time.Now().UTC()

	for _, job := range jobs {
		if job == nil || job.ID == "" {
			continue
		}
		job.CreatedAt = now
		job.UpdatedAt = now
		ref := r.col.Doc(job.ID)
		batch.Set(ref, job)
	}

	_, err := batch.Commit(ctx)
	return err
}

// PolicyCheckResultsRepository manages PolicyCheckResult documents.
type PolicyCheckResultsRepository struct {
	col *firestore.CollectionRef
}

// NewPolicyCheckResultsRepository creates a new PolicyCheckResultsRepository.
func NewPolicyCheckResultsRepository(client *firestore.Client) *PolicyCheckResultsRepository {
	return &PolicyCheckResultsRepository{
		col: client.Collection("horizon_policy_check_results"),
	}
}

// Save saves a policy check result.
func (r *PolicyCheckResultsRepository) Save(ctx context.Context, result *policy.PolicyCheckResult) error {
	if result == nil || result.JobID == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(result.JobID).Set(ctx, result)
	return err
}

// Get retrieves a policy check result by job ID.
func (r *PolicyCheckResultsRepository) Get(ctx context.Context, jobID string) (*policy.PolicyCheckResult, error) {
	if jobID == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(jobID).Get(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}

	var result policy.PolicyCheckResult
	if err := snap.DataTo(&result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// ListByTenant lists policy check results for a tenant.
func (r *PolicyCheckResultsRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*policy.PolicyCheckResult, error) {
	if limit <= 0 {
		limit = 50
	}

	q := r.col.Where("tenantId", "==", tenantID).
		OrderBy("checkedAt", firestore.Desc).
		Limit(limit)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var results []*policy.PolicyCheckResult
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var result policy.PolicyCheckResult
		if err := doc.DataTo(&result); err != nil {
			continue
		}
		results = append(results, &result)
	}

	return results, nil
}
