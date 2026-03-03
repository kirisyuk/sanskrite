package scout

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// IsTerminal returns true if the scouting job status is a terminal state
// (completed, failed, or partial).
func IsTerminal(status string) bool {
	switch status {
	case "completed", "failed", "partial":
		return true
	}
	return false
}

// WaitForJobTerminal polls the scouting job until it reaches a terminal state
// or the context is cancelled / timeout exceeded.
func WaitForJobTerminal(
	ctx context.Context,
	scoutingJobs *repo.ScoutingJobsRepository,
	jobID string,
	pollInterval time.Duration,
	timeout time.Duration,
) (*models.ScoutingJob, error) {
	if scoutingJobs == nil {
		return nil, fmt.Errorf("scouting jobs repository is nil")
	}
	if jobID == "" {
		return nil, fmt.Errorf("job ID is empty")
	}
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadline := time.Now().Add(timeout)
	for {
		job, err := scoutingJobs.Get(ctx, jobID)
		if err != nil {
			return nil, fmt.Errorf("failed to get job %s: %w", jobID, err)
		}
		if IsTerminal(string(job.Status)) {
			return job, nil
		}

		if time.Now().After(deadline) {
			return job, fmt.Errorf("timeout waiting for job %s (status=%s)", jobID, job.Status)
		}

		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
