package scout

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

type policyPersistTask struct {
	Key         string
	JobID       string
	TenantID    string
	Result      *policy.PolicyCheckResult
	Attempt     int
	NextAttempt time.Time
	LastError   string
	EnqueuedAt  time.Time
	UpdatedAt   time.Time
}

const (
	policyPersistRetryMaxAttempts = 5
)

func policyPersistBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := 3 * time.Second
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= 2*time.Minute {
			return 2 * time.Minute
		}
	}
	if backoff < 3*time.Second {
		return 3 * time.Second
	}
	return backoff
}

func (e *ScoutEngine) enqueuePolicyPersistTask(job *models.ScoutingJob, result *policy.PolicyCheckResult) {
	if e == nil || e.policyCheckResultsRepo == nil || e.scoutingJobsRepo == nil || result == nil {
		return
	}
	jobID := strings.TrimSpace(result.JobID)
	if jobID == "" && job != nil {
		jobID = strings.TrimSpace(job.ID)
	}
	if jobID == "" {
		return
	}
	now := time.Now().UTC()
	key := "policy_result:" + jobID
	task := &policyPersistTask{
		Key:         key,
		JobID:       jobID,
		TenantID:    strings.TrimSpace(result.TenantID),
		Result:      result,
		Attempt:     0,
		NextAttempt: now,
		EnqueuedAt:  now,
		UpdatedAt:   now,
	}

	e.policyPersistQueueMu.Lock()
	if e.policyPersistQueue == nil {
		e.policyPersistQueue = make(map[string]*policyPersistTask)
	}
	if existing, ok := e.policyPersistQueue[key]; ok && existing != nil {
		// Idempotent queue contract: keep the newest payload, preserve retry counters.
		existing.Result = result
		existing.UpdatedAt = now
		existing.TenantID = strings.TrimSpace(result.TenantID)
		if existing.NextAttempt.Before(now) {
			existing.NextAttempt = now
		}
		task = existing
	} else {
		e.policyPersistQueue[key] = task
	}
	e.policyPersistQueueMu.Unlock()

	e.updateJobPolicyPersistState(jobID, func(j *models.ScoutingJob) {
		if j.State == nil {
			j.State = map[string]interface{}{}
		}
		j.State["policyPersistQueued"] = true
		j.State["policyPersistAttempts"] = float64(task.Attempt)
		j.State["policyPersistQueuedAt"] = task.EnqueuedAt.Format(time.RFC3339Nano)
		j.State["policyPersistDlq"] = false
	})
	e.ensurePolicyPersistWorker()
}

func (e *ScoutEngine) ensurePolicyPersistWorker() {
	if e == nil {
		return
	}
	e.policyPersistQueueMu.Lock()
	if e.policyPersistWorkerOn {
		e.policyPersistQueueMu.Unlock()
		return
	}
	e.policyPersistWorkerOn = true
	e.policyPersistQueueMu.Unlock()
	go e.runPolicyPersistWorker()
}

func (e *ScoutEngine) runPolicyPersistWorker() {
	if e == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	idleCycles := 0

	for range ticker.C {
		due := e.collectDuePolicyPersistTasks()
		if len(due) == 0 {
			idleCycles++
			if idleCycles >= 120 { // ~4 minutes idle -> stop worker, restart on next enqueue.
				e.policyPersistQueueMu.Lock()
				if len(e.policyPersistQueue) == 0 {
					e.policyPersistWorkerOn = false
					e.policyPersistQueueMu.Unlock()
					return
				}
				e.policyPersistQueueMu.Unlock()
				idleCycles = 0
			}
			continue
		}
		idleCycles = 0
		for _, task := range due {
			e.processPolicyPersistTask(task)
		}
	}
}

func (e *ScoutEngine) collectDuePolicyPersistTasks() []*policyPersistTask {
	if e == nil {
		return nil
	}
	now := time.Now().UTC()
	e.policyPersistQueueMu.Lock()
	defer e.policyPersistQueueMu.Unlock()
	if len(e.policyPersistQueue) == 0 {
		return nil
	}
	out := make([]*policyPersistTask, 0, len(e.policyPersistQueue))
	for _, t := range e.policyPersistQueue {
		if t == nil {
			continue
		}
		if t.NextAttempt.After(now) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (e *ScoutEngine) processPolicyPersistTask(task *policyPersistTask) {
	if e == nil || task == nil || e.policyCheckResultsRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	err := e.policyCheckResultsRepo.Save(ctx, task.Result)
	cancel()

	if err == nil {
		e.policyPersistQueueMu.Lock()
		delete(e.policyPersistQueue, task.Key)
		e.policyPersistQueueMu.Unlock()
		e.updateJobPolicyPersistState(task.JobID, func(j *models.ScoutingJob) {
			if j.State == nil {
				j.State = map[string]interface{}{}
			}
			j.State["policyPersistQueued"] = false
			j.State["policyPersistDlq"] = false
			j.State["policyPersistError"] = false
			j.State["policyPersistAttempts"] = float64(task.Attempt + 1)
			j.State["policyPersistSucceededAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		})
		return
	}

	task.Attempt++
	task.LastError = strings.TrimSpace(err.Error())
	task.UpdatedAt = time.Now().UTC()
	if task.Attempt >= policyPersistRetryMaxAttempts {
		e.policyPersistQueueMu.Lock()
		delete(e.policyPersistQueue, task.Key)
		if e.policyPersistDLQ == nil {
			e.policyPersistDLQ = make(map[string]*policyPersistTask)
		}
		e.policyPersistDLQ[task.Key] = task
		e.policyPersistQueueMu.Unlock()
		log.Printf("[ScoutEngine] policy persist moved to DLQ: key=%s attempts=%d err=%s",
			task.Key, task.Attempt, task.LastError)
		e.updateJobPolicyPersistState(task.JobID, func(j *models.ScoutingJob) {
			if j.State == nil {
				j.State = map[string]interface{}{}
			}
			j.State["policyPersistQueued"] = false
			j.State["policyPersistDlq"] = true
			j.State["policyPersistError"] = true
			j.State["policyPersistErrorAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			j.State["policyPersistAttempts"] = float64(task.Attempt)
			j.State["policyPersistLastError"] = task.LastError
			j.State["terminalOutcome"] = TerminalOutcomeCompletedWithPolicyPersistError
			if j.Status == coreModels.AgentJobStatusCompleted {
				j.Status = coreModels.AgentJobStatusPartial
			}
		})
		return
	}

	backoff := policyPersistBackoff(task.Attempt)
	task.NextAttempt = time.Now().UTC().Add(backoff)
	e.policyPersistQueueMu.Lock()
	e.policyPersistQueue[task.Key] = task
	e.policyPersistQueueMu.Unlock()
	e.updateJobPolicyPersistState(task.JobID, func(j *models.ScoutingJob) {
		if j.State == nil {
			j.State = map[string]interface{}{}
		}
		j.State["policyPersistQueued"] = true
		j.State["policyPersistAttempts"] = float64(task.Attempt)
		j.State["policyPersistLastError"] = task.LastError
		j.State["policyPersistNextAttemptAt"] = task.NextAttempt.Format(time.RFC3339Nano)
	})
}

func (e *ScoutEngine) updateJobPolicyPersistState(jobID string, mutate func(job *models.ScoutingJob)) {
	if e == nil || e.scoutingJobsRepo == nil || mutate == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := e.scoutingJobsRepo.Get(ctx, jobID)
	if err != nil || job == nil {
		if err != nil {
			log.Printf("[ScoutEngine] policy persist state update skipped: get job=%s err=%v", jobID, err)
		}
		return
	}
	mutate(job)
	if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
		log.Printf("[ScoutEngine] policy persist state update failed: job=%s err=%v", jobID, err)
	}
}

func (e *ScoutEngine) policyPersistQueueSnapshot() map[string]interface{} {
	if e == nil {
		return nil
	}
	e.policyPersistQueueMu.Lock()
	defer e.policyPersistQueueMu.Unlock()
	return map[string]interface{}{
		"queued": len(e.policyPersistQueue),
		"dlq":    len(e.policyPersistDLQ),
	}
}

func (e *ScoutEngine) formatPolicyPersistQueueSnapshot() string {
	if e == nil {
		return ""
	}
	snap := e.policyPersistQueueSnapshot()
	if snap == nil {
		return ""
	}
	return fmt.Sprintf("policyPersistQueue{queued=%v dlq=%v}", snap["queued"], snap["dlq"])
}
