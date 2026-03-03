package scout

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ScoutWorkflow управляет состояниями ScoutingJob
type ScoutWorkflow struct {
	agentJobsRepo  *repo.AgentJobsRepository
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository
}

// NewScoutWorkflow создает новый workflow
func NewScoutWorkflow(agentJobsRepo *repo.AgentJobsRepository) *ScoutWorkflow {
	return &ScoutWorkflow{
		agentJobsRepo: agentJobsRepo,
	}
}

// WithScoutingJobsRepo sets the ScoutingJobsRepository for firing on-complete hooks.
func (w *ScoutWorkflow) WithScoutingJobsRepo(repo *horizonrepo.ScoutingJobsRepository) *ScoutWorkflow {
	w.scoutingJobsRepo = repo
	return w
}

// TransitionState переводит ScoutingJob в новое состояние
func (w *ScoutWorkflow) TransitionState(ctx context.Context, jobID string, newState models.ScoutingJobState) error {
	job, err := w.agentJobsRepo.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	// Обновляем состояние в State для ScoutingJob
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	job.State["currentState"] = string(newState)
	job.State["lastStateTransition"] = time.Now().UTC().Format(time.RFC3339)

	// Обновляем статус AgentJob в зависимости от состояния
	isTerminal := false
	switch newState {
	case models.ScoutingJobStateVerified:
		job.Status = "completed"
		now := time.Now().UTC()
		job.CompletedAt = &now
		isTerminal = true
	case models.ScoutingJobStateRejected, models.ScoutingJobStateFailed:
		job.Status = "failed"
		now := time.Now().UTC()
		job.CompletedAt = &now
		isTerminal = true
	default:
		job.Status = "running"
	}

	if err := w.agentJobsRepo.Update(ctx, job); err != nil {
		return err
	}

	if isTerminal && w.scoutingJobsRepo != nil {
		scoutJob := &models.ScoutingJob{AgentJob: *job}
		if hookErr := w.scoutingJobsRepo.FireOnComplete(ctx, scoutJob); hookErr != nil {
			log.Printf("[ScoutWorkflow:TransitionState] on-complete hook error for job %s: %v", jobID, hookErr)
		}
	}

	return nil
}

// GetActiveJobsByPath получает активные ScoutingJob для canonicalPath
func (w *ScoutWorkflow) GetActiveJobsByPath(ctx context.Context, tenantID, canonicalPath string) ([]*models.ScoutingJob, error) {
	// Получаем все активные AgentJob для tenantID
	jobs, err := w.agentJobsRepo.ListByProject(ctx, tenantID, "", 100)
	if err != nil {
		return nil, err
	}

	var activeJobs []*models.ScoutingJob
	for _, job := range jobs {
		if job.Status != "running" && job.Status != "pending" {
			continue
		}
		// Проверяем, что это ScoutingJob и соответствует canonicalPath
		if targetPath, ok := job.State["targetCanonicalPath"].(string); ok && targetPath == canonicalPath {
			scoutingJob := &models.ScoutingJob{
				AgentJob: *job,
			}
			activeJobs = append(activeJobs, scoutingJob)
		}
	}

	return activeJobs, nil
}

