package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrInvalidAgentJob = errors.New("invalid agent job")

// Create создает новый AgentJob
func (r *AgentJobsRepository) Create(ctx context.Context, job *models.AgentJob) (string, error) {
	if job == nil || job.TenantID == "" || job.ProjectID == "" || job.AgentID == "" {
		return "", ErrInvalidAgentJob
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.Status == "" {
		job.Status = models.AgentJobStatusPending
	}
	if job.Steps == nil {
		job.Steps = []models.AgentStep{}
	}
	doc := r.col.NewDoc()
	job.ID = doc.ID
	_, err := doc.Set(ctx, job)
	return job.ID, err
}

// Get получает AgentJob по ID
func (r *AgentJobsRepository) Get(ctx context.Context, id string) (*models.AgentJob, error) {
	if id == "" {
		return nil, ErrInvalidAgentJob
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var job models.AgentJob
	if err := snap.DataTo(&job); err != nil {
		return nil, err
	}
	job.ID = snap.Ref.ID // Set ID from Firestore document ID
	return &job, nil
}

// Update обновляет AgentJob
func (r *AgentJobsRepository) Update(ctx context.Context, job *models.AgentJob) error {
	if job == nil || job.ID == "" {
		return ErrInvalidAgentJob
	}
	job.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(job.ID).Set(ctx, job)
	return err
}

// UpdateStatus обновляет только статус и прогресс
func (r *AgentJobsRepository) UpdateStatus(ctx context.Context, id string, status models.AgentJobStatus, progress float64) error {
	if id == "" {
		return ErrInvalidAgentJob
	}
	updates := []firestore.Update{
		{Path: "status", Value: status},
		{Path: "progress", Value: progress},
		{Path: "updatedAt", Value: time.Now().UTC()},
	}
	if status == models.AgentJobStatusCompleted || status == models.AgentJobStatusFailed || status == models.AgentJobStatusPartial {
		now := time.Now().UTC()
		updates = append(updates, firestore.Update{Path: "completedAt", Value: now})
	}
	_, err := r.col.Doc(id).Update(ctx, updates)
	return err
}

// UpdateStep обновляет шаг в job
func (r *AgentJobsRepository) UpdateStep(ctx context.Context, jobID string, stepID string, step models.AgentStep) error {
	if jobID == "" || stepID == "" {
		return ErrInvalidAgentJob
	}
	job, err := r.Get(ctx, jobID)
	if err != nil {
		return err
	}
	// Найти и обновить шаг
	found := false
	for i := range job.Steps {
		if job.Steps[i].ID == stepID {
			job.Steps[i] = step
			found = true
			break
		}
	}
	if !found {
		// Добавить новый шаг
		job.Steps = append(job.Steps, step)
	}
	return r.Update(ctx, job)
}

// ListByProject возвращает список jobs для проекта
// Индекс: (tenantId, projectId, createdAt DESC)
func (r *AgentJobsRepository) ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]*models.AgentJob, error) {
	if tenantID == "" || projectID == "" {
		return nil, ErrInvalidAgentJob
	}
	q := r.col.Where("tenantId", "==", tenantID).
		Where("projectId", "==", projectID).
		OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	jobs := make([]*models.AgentJob, 0, len(docs))
	for _, doc := range docs {
		var job models.AgentJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		job.ID = doc.Ref.ID // Set ID from Firestore document ID
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// ListByStatus возвращает список jobs по статусу
// Индекс: (tenantId, status, createdAt DESC)
func (r *AgentJobsRepository) ListByStatus(ctx context.Context, tenantID string, status models.AgentJobStatus, limit int) ([]*models.AgentJob, error) {
	if tenantID == "" {
		return nil, ErrInvalidAgentJob
	}
	q := r.col.Where("tenantId", "==", tenantID).
		Where("status", "==", status).
		OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	jobs := make([]*models.AgentJob, 0, len(docs))
	for _, doc := range docs {
		var job models.AgentJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		job.ID = doc.Ref.ID // Set ID from Firestore document ID
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// ListRunning возвращает список выполняющихся jobs для проекта
func (r *AgentJobsRepository) ListRunning(ctx context.Context, tenantID, projectID string) ([]*models.AgentJob, error) {
	return r.ListByProjectAndStatus(ctx, tenantID, projectID, models.AgentJobStatusRunning, 0)
}

// ListByProjectAndStatus возвращает список jobs для проекта по статусу
// Индекс: (tenantId, projectId, status, createdAt DESC)
func (r *AgentJobsRepository) ListByProjectAndStatus(ctx context.Context, tenantID, projectID string, status models.AgentJobStatus, limit int) ([]*models.AgentJob, error) {
	if tenantID == "" || projectID == "" {
		return nil, ErrInvalidAgentJob
	}
	q := r.col.Where("tenantId", "==", tenantID).
		Where("projectId", "==", projectID).
		Where("status", "==", status).
		OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	jobs := make([]*models.AgentJob, 0, len(docs))
	for _, doc := range docs {
		var job models.AgentJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		job.ID = doc.Ref.ID // Set ID from Firestore document ID
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// Delete удаляет AgentJob
func (r *AgentJobsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidAgentJob
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

