package scout

import (
	"context"
	"fmt"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// ExecuteDirectedSupplyChainJob runs only the directed exploration runtime for an existing lens/job.
// It intentionally bypasses the full scout planning/research pipeline so smoke and operator tools
// can exercise hot-path/upstream/downstream expansion directly against a known anchor.
func (e *ScoutEngine) ExecuteDirectedSupplyChainJob(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	if e == nil || job == nil {
		return fmt.Errorf("directed exploration job is nil")
	}
	if !isDirectedSupplyChainExplorationJob(job) {
		return fmt.Errorf("job %s is not a directed supply-chain exploration job", job.ID)
	}
	rt := newDirectedSupplyChainExplorationRuntime(job)
	if !rt.ensureQueries(ctx, e, job) {
		return fmt.Errorf("no directed exploration queries could be built for %s", job.TargetCanonicalPath)
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	delete(job.State, "directedExplorationCompleted")
	delete(job.State, "directedExplorationToolRuns")
	job.Status = coremodels.AgentJobStatusRunning
	job.Progress = 0.15
	job.CurrentState = horizonmodels.ScoutingJobStateExecution
	job.UpdatedAt = time.Now().UTC()
	if e.scoutingJobsRepo != nil && job.ID != "" {
		_ = e.scoutingJobsRepo.Update(ctx, job)
	}

	e.runDirectedSupplyChainExploration(ctx, job)
	e.persistLensArtifactsFromJobState(ctx, job)

	now := time.Now().UTC()
	job.Status = coremodels.AgentJobStatusCompleted
	job.Progress = 1.0
	job.CurrentState = horizonmodels.ScoutingJobStateVerified
	job.CompletedAt = &now
	job.UpdatedAt = now
	if e.scoutingJobsRepo != nil && job.ID != "" {
		_ = e.scoutingJobsRepo.Update(ctx, job)
	}
	return nil
}
