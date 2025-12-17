package repo

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ScoutingJobsRepository репозиторий для работы с ScoutingJob
// Использует существующий AgentJobsRepository для хранения
type ScoutingJobsRepository struct {
	agentJobsRepo *repo.AgentJobsRepository
}

// NewScoutingJobsRepository создает новый репозиторий
func NewScoutingJobsRepository(agentJobsRepo *repo.AgentJobsRepository) *ScoutingJobsRepository {
	return &ScoutingJobsRepository{
		agentJobsRepo: agentJobsRepo,
	}
}

// Create создает новый ScoutingJob через AgentJobsRepository
func (r *ScoutingJobsRepository) Create(ctx context.Context, job *models.ScoutingJob) (string, error) {
	if job == nil {
		return "", ErrInvalidDocument
	}
	// ScoutingJob встраивает AgentJob, поэтому используем его напрямую
	agentJob := &job.AgentJob
	id, err := r.agentJobsRepo.Create(ctx, agentJob)
	if err != nil {
		return "", fmt.Errorf("failed to create scouting job: %w", err)
	}
	job.ID = id
	return id, nil
}

// Get получает ScoutingJob по ID
func (r *ScoutingJobsRepository) Get(ctx context.Context, id string) (*models.ScoutingJob, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	agentJob, err := r.agentJobsRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get scouting job: %w", err)
	}
	// Преобразуем AgentJob обратно в ScoutingJob
	// В реальной реализации нужно будет хранить дополнительные поля ScoutingJob отдельно
	// или использовать State в AgentJob для хранения специфичных полей
	scoutingJob := &models.ScoutingJob{
		AgentJob: *agentJob,
		// Дополнительные поля будут извлекаться из State или отдельной коллекции
	}
	return scoutingJob, nil
}

// Update обновляет ScoutingJob
func (r *ScoutingJobsRepository) Update(ctx context.Context, job *models.ScoutingJob) error {
	if job == nil || job.ID == "" {
		return ErrInvalidDocument
	}
	agentJob := &job.AgentJob
	return r.agentJobsRepo.Update(ctx, agentJob)
}

// ListByLens получает список ScoutingJob для Lens
func (r *ScoutingJobsRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.ScoutingJob, error) {
	// Используем State для фильтрации по lensID
	// В реальной реализации может потребоваться отдельная коллекция или индекс
	agentJobs, err := r.agentJobsRepo.ListByProject(ctx, tenantID, "", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list scouting jobs: %w", err)
	}
	var scoutingJobs []*models.ScoutingJob
	for _, agentJob := range agentJobs {
		// Фильтруем по lensID из State
		if lensIDVal, ok := agentJob.State["lensId"].(string); ok && lensIDVal == lensID {
			scoutingJob := &models.ScoutingJob{
				AgentJob: *agentJob,
			}
			scoutingJobs = append(scoutingJobs, scoutingJob)
		}
	}
	return scoutingJobs, nil
}

// ErrInvalidDocument определен в fragments.go

