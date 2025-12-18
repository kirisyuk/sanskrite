package repo

import (
	"context"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ListPending возвращает список ожидающих задач Scout
func (r *ScoutingJobsRepository) ListPending(ctx context.Context, limit int) ([]*models.ScoutingJob, error) {
	agentJobs, err := r.agentJobsRepo.ListPendingByAgent(ctx, "scout", limit)
	if err != nil {
		return nil, err
	}

	var jobs []*models.ScoutingJob
	for _, aj := range agentJobs {
		scoutingJob := &models.ScoutingJob{
			AgentJob: *aj,
		}
		// Восстанавливаем специфичные поля из State
		if targetPath, ok := aj.State["targetCanonicalPath"].(string); ok {
			scoutingJob.TargetCanonicalPath = targetPath
		}
		if currentState, ok := aj.State["currentState"].(string); ok {
			scoutingJob.CurrentState = models.ScoutingJobState(currentState)
		}
		if lensID, ok := aj.State["lensId"].(string); ok {
			scoutingJob.LensID = lensID
		}
		// ВАЖНО: ScoutingJobsRepository хранит только AgentJob, поэтому стратегия тоже должна жить в State
		if strategy, ok := aj.State["scoutingStrategy"].(string); ok {
			scoutingJob.ScoutingStrategy = strategy
		}
		
		jobs = append(jobs, scoutingJob)
	}
	
	return jobs, nil
}
