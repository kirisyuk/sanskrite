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
	onCreate      func(ctx context.Context, job *models.ScoutingJob) error
}

// NewScoutingJobsRepository создает новый репозиторий
func NewScoutingJobsRepository(agentJobsRepo *repo.AgentJobsRepository) *ScoutingJobsRepository {
	return &ScoutingJobsRepository{
		agentJobsRepo: agentJobsRepo,
	}
}

// WithOnCreate installs an optional hook invoked after a job is created.
// Used by the server to enqueue Cloud Tasks, while keeping tools/tests able to write jobs directly.
func (r *ScoutingJobsRepository) WithOnCreate(hook func(ctx context.Context, job *models.ScoutingJob) error) *ScoutingJobsRepository {
	r.onCreate = hook
	return r
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
	if r.onCreate != nil {
		if err := r.onCreate(ctx, job); err != nil {
			return "", fmt.Errorf("scouting job created but enqueue hook failed: %w", err)
		}
	}
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
	r.populateFromState(scoutingJob)
	return scoutingJob, nil
}

func (r *ScoutingJobsRepository) populateFromState(job *models.ScoutingJob) {
	if job == nil || job.State == nil {
		return
	}
	if job.TargetCanonicalPath == "" {
		if v, ok := job.State["targetCanonicalPath"].(string); ok {
			job.TargetCanonicalPath = v
		}
	}
	if job.LensID == "" {
		if v, ok := job.State["lensId"].(string); ok {
			job.LensID = v
		}
	}
	if job.CurrentState == "" {
		if v, ok := job.State["currentState"].(string); ok {
			job.CurrentState = models.ScoutingJobState(v)
		}
	}
	if job.ScoutingStrategy == "" {
		if v, ok := job.State["scoutingStrategy"].(string); ok {
			job.ScoutingStrategy = v
		}
	}
	// Best-effort: SourceIDs may be persisted either as top-level field or in state.
	if len(job.SourceIDs) == 0 {
		if v, ok := job.State["sourceIds"]; ok {
			switch t := v.(type) {
			case []string:
				job.SourceIDs = t
			case []interface{}:
				out := make([]string, 0, len(t))
				for _, it := range t {
					if s, ok2 := it.(string); ok2 {
						out = append(out, s)
					}
				}
				if len(out) > 0 {
					job.SourceIDs = out
				}
			}
		}
	}
}

// ListByProjectAndLens lists scout jobs for a given project and (optionally) lensId.
// This is preferred over ListByLens because it relies on a more commonly provisioned index:
// (tenantId, projectId, createdAt DESC).
func (r *ScoutingJobsRepository) ListByProjectAndLens(ctx context.Context, tenantID, projectID, lensID string, limit int) ([]*models.ScoutingJob, error) {
	if r == nil || r.agentJobsRepo == nil {
		return nil, fmt.Errorf("scouting jobs repo not configured")
	}
	if tenantID == "" || projectID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	// Pull more than requested and filter client-side.
	agentJobs, err := r.agentJobsRepo.ListByProject(ctx, tenantID, projectID, limit*4)
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScoutingJob, 0, limit)
	for _, aj := range agentJobs {
		if aj == nil || aj.AgentID != "scout" {
			continue
		}
		sj := &models.ScoutingJob{AgentJob: *aj}
		r.populateFromState(sj)
		if lensID != "" && sj.LensID != lensID {
			continue
		}
		out = append(out, sj)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
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
		r.populateFromState(scoutingJob)

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
