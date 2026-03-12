package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type RiskJobsRepository struct {
	col      *firestore.CollectionRef
	onCreate func(ctx context.Context, job *horizonmodels.RiskJob) error
}

func NewRiskJobsRepository(client *firestore.Client) *RiskJobsRepository {
	return &RiskJobsRepository{
		col: client.Collection("horizon_risk_jobs"),
	}
}

func (r *RiskJobsRepository) WithOnCreate(hook func(ctx context.Context, job *horizonmodels.RiskJob) error) *RiskJobsRepository {
	r.onCreate = hook
	return r
}

func (r *RiskJobsRepository) Create(ctx context.Context, job *horizonmodels.RiskJob) (string, bool, error) {
	if r == nil || r.col == nil || job == nil {
		return "", false, ErrInvalidDocument
	}
	job.NormalizeDefaults()
	if err := job.Validate(); err != nil {
		return "", false, err
	}

	if key := strings.TrimSpace(job.DedupKey); key != "" {
		existingID, err := r.findActiveByDedupKey(ctx, key)
		if err == nil && existingID != "" {
			job.ID = existingID
			return existingID, false, nil
		}
	}

	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now

	doc := r.col.NewDoc()
	job.ID = doc.ID
	if _, err := doc.Set(ctx, job); err != nil {
		return "", false, fmt.Errorf("create risk job: %w", err)
	}
	if r.onCreate != nil {
		if err := r.onCreate(ctx, job); err != nil {
			return "", true, err
		}
	}
	return job.ID, true, nil
}

func (r *RiskJobsRepository) Get(ctx context.Context, id string) (*horizonmodels.RiskJob, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var out horizonmodels.RiskJob
	if err := snap.DataTo(&out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		out.ID = snap.Ref.ID
	}
	out.NormalizeDefaults()
	return &out, nil
}

func (r *RiskJobsRepository) Update(ctx context.Context, job *horizonmodels.RiskJob) error {
	if r == nil || r.col == nil || job == nil || strings.TrimSpace(job.ID) == "" {
		return ErrInvalidDocument
	}
	job.NormalizeDefaults()
	if err := job.Validate(); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(job.ID)).Set(ctx, job)
	return err
}

func (r *RiskJobsRepository) findActiveByDedupKey(ctx context.Context, dedupKey string) (string, error) {
	dedupKey = strings.TrimSpace(dedupKey)
	if dedupKey == "" {
		return "", nil
	}
	docs, err := r.col.Where("dedupKey", "==", dedupKey).Limit(10).Documents(ctx).GetAll()
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		var j horizonmodels.RiskJob
		if err := d.DataTo(&j); err != nil {
			continue
		}
		state := j.CurrentState
		if state == horizonmodels.RiskJobStatePending || state == horizonmodels.RiskJobStateRunning {
			return d.Ref.ID, nil
		}
	}
	return "", nil
}

func (r *RiskJobsRepository) ListRecentByTargetCanonicalPath(ctx context.Context, tenantID, canonicalPath string, limit int) ([]*horizonmodels.RiskJob, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(canonicalPath) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 25
	}
	tenants := []string{strings.TrimSpace(tenantID)}
	if strings.TrimSpace(tenantID) != "global" {
		tenants = append(tenants, "global")
	}
	docs, err := r.col.
		Where("tenantId", "in", tenants).
		Where("targetCanonicalPath", "==", strings.TrimSpace(canonicalPath)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.RiskJob, 0, len(docs))
	for _, d := range docs {
		var job horizonmodels.RiskJob
		if err := d.DataTo(&job); err != nil {
			continue
		}
		if strings.TrimSpace(job.ID) == "" {
			job.ID = d.Ref.ID
		}
		job.NormalizeDefaults()
		out = append(out, &job)
	}
	return out, nil
}
