package repo

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
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
// Если lensID пустой, возвращает все ScoutingJob для tenant (фильтрованные по AgentID="scout")
func (r *ScoutingJobsRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.ScoutingJob, error) {
	// Получаем все AgentJob для tenant с AgentID="scout"
	// Используем ListByStatus для получения всех jobs по разным статусам, затем фильтруем по AgentID
	allStatuses := []coremodels.AgentJobStatus{
		coremodels.AgentJobStatusPending,
		coremodels.AgentJobStatusRunning,
		coremodels.AgentJobStatusCompleted,
		coremodels.AgentJobStatusFailed,
		coremodels.AgentJobStatusPartial,
	}
	
	var allAgentJobs []*coremodels.AgentJob
	for _, status := range allStatuses {
		jobs, err := r.agentJobsRepo.ListByStatus(ctx, tenantID, status, limit*3) // Увеличиваем лимит для фильтрации
		if err != nil {
			continue // Пропускаем ошибки, продолжаем с другими статусами
		}
		allAgentJobs = append(allAgentJobs, jobs...)
	}
	
	var scoutingJobs []*models.ScoutingJob
	for _, agentJob := range allAgentJobs {
		// Фильтруем только ScoutingJob (AgentID="scout")
		if agentJob.AgentID != "scout" {
			continue
		}
		
		// Преобразуем AgentJob в ScoutingJob
		scoutingJob := &models.ScoutingJob{
			AgentJob: *agentJob,
		}
		
		// Извлекаем дополнительные поля из State
		if targetPath, ok := agentJob.State["targetCanonicalPath"].(string); ok {
			scoutingJob.TargetCanonicalPath = targetPath
		}
		if currentState, ok := agentJob.State["currentState"].(string); ok {
			scoutingJob.CurrentState = models.ScoutingJobState(currentState)
		}
		if lensIDFromState, ok := agentJob.State["lensId"].(string); ok {
			scoutingJob.LensID = lensIDFromState
		}
		
		// Если lensID указан, фильтруем по нему
		if lensID != "" {
			if scoutingJob.LensID != lensID {
				continue
			}
		}
		
		scoutingJobs = append(scoutingJobs, scoutingJob)
		if len(scoutingJobs) >= limit {
			break
		}
	}
	return scoutingJobs, nil
}

// ErrInvalidDocument определен в fragments.go

