package scoutbridge

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	agentmeshcontracts "github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	agentmeshsubmitter "github.com/igorkirisyuk/sanskrite/internal/agentmesh/submitter"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type AgenticService struct {
	submitter *agentmeshsubmitter.Service
	meshStore *agentmeshrepo.Store
	agentJobs *corerepo.AgentJobsRepository
	workflow  *horizonscout.ScoutWorkflow
	logger    *log.Logger
}

func NewAgenticService(
	submitter *agentmeshsubmitter.Service,
	meshStore *agentmeshrepo.Store,
	agentJobs *corerepo.AgentJobsRepository,
	scoutingJobs *horizonrepo.ScoutingJobsRepository,
	logger *log.Logger,
) *AgenticService {
	if logger == nil {
		logger = log.Default()
	}
	return &AgenticService{
		submitter: submitter,
		meshStore: meshStore,
		agentJobs: agentJobs,
		workflow:  horizonscout.NewScoutWorkflow(agentJobs).WithScoutingJobsRepo(scoutingJobs),
		logger:    logger,
	}
}

type Executor struct {
	classic *horizonscout.ScoutEngine
	agentic *AgenticService
	logger  *log.Logger
}

func NewExecutor(classic *horizonscout.ScoutEngine, agentic *AgenticService, logger *log.Logger) *Executor {
	if logger == nil {
		logger = log.Default()
	}
	return &Executor{classic: classic, agentic: agentic, logger: logger}
}

func (e *Executor) ExecuteScoutingJob(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	cfg := horizonpolicy.ResolveScoutRuntimeConfig(job)
	switch cfg.Mode {
	case horizonpolicy.ScoutRuntimeAgentic:
		if e.agentic == nil {
			return fmt.Errorf("agentic scout runtime is not configured")
		}
		_, err := e.agentic.RunAndWait(ctx, job, false, cfg.AllowAgenticWrites)
		return err
	case horizonpolicy.ScoutRuntimeShadowCompare:
		if e.classic == nil {
			return fmt.Errorf("classic scout runtime is not configured")
		}
		if err := e.classic.ExecuteScoutingJob(ctx, job); err != nil {
			return err
		}
		if e.agentic == nil {
			return nil
		}
		if _, err := e.agentic.RunAndWait(ctx, job, true, cfg.AllowShadowWrites); err != nil && e.logger != nil {
			e.logger.Printf("[scoutbridge] shadow compare agentic run failed job=%s err=%v", strings.TrimSpace(job.ID), err)
		}
		return nil
	case horizonpolicy.ScoutRuntimeAgenticWithSwitchback:
		if e.agentic == nil {
			if e.classic == nil {
				return fmt.Errorf("no scouting runtime is configured")
			}
			return e.classic.ExecuteScoutingJob(ctx, job)
		}
		if _, err := e.agentic.RunAndWait(ctx, job, false, cfg.AllowAgenticWrites); err != nil {
			if cfg.AllowClassicFallback && e.classic != nil {
				if e.logger != nil {
					e.logger.Printf("[scoutbridge] agentic failed, switching back to classic job=%s err=%v", strings.TrimSpace(job.ID), err)
				}
				return e.classic.ExecuteScoutingJob(ctx, job)
			}
			return err
		}
		return nil
	default:
		if e.classic == nil {
			return fmt.Errorf("classic scout runtime is not configured")
		}
		return e.classic.ExecuteScoutingJob(ctx, job)
	}
}

func (s *AgenticService) RunAndWait(ctx context.Context, job *horizonmodels.ScoutingJob, compareOnly bool, allowWrites bool) (*agentmeshcontracts.MeshTask, error) {
	if s == nil || s.submitter == nil || s.meshStore == nil {
		return nil, fmt.Errorf("agentic scout bridge is not configured")
	}
	if job == nil {
		return nil, fmt.Errorf("scouting job is nil")
	}
	input := map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"scoutingJobId":       strings.TrimSpace(job.ID),
		"query":               strings.TrimSpace(job.UserQuery),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"compareOnly":         compareOnly,
		"allowWrites":         allowWrites,
	}
	if input["query"] == "" && job.State != nil {
		if raw, ok := job.State["originalQuery"].(string); ok {
			input["query"] = strings.TrimSpace(raw)
		}
	}
	taskID, err := s.submitter.Submit(ctx, strings.TrimSpace(job.TenantID), "scout.plan", input, "scout.plan:"+strings.TrimSpace(job.ID))
	if err != nil {
		return nil, err
	}
	if err := s.updateJobState(ctx, job.ID, map[string]any{
		"agenticScout.taskId":      taskID,
		"agenticScout.compareOnly": compareOnly,
		"agenticScout.allowWrites": allowWrites,
		"agenticScout.mode":        ternary(compareOnly, "shadow_compare", "agentic_scout"),
		"agenticScout.submittedAt": time.Now().UTC().Format(time.RFC3339),
	}); err != nil && s.logger != nil {
		s.logger.Printf("[scoutbridge] update state failed job=%s err=%v", job.ID, err)
	}
	task, err := s.waitForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.applyResultToJob(ctx, job, task, compareOnly); err != nil {
		return task, err
	}
	return task, nil
}

func (s *AgenticService) waitForTask(ctx context.Context, taskID string) (*agentmeshcontracts.MeshTask, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		task, err := s.meshStore.GetTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			switch task.Status {
			case agentmeshcontracts.TaskStatusSucceeded, agentmeshcontracts.TaskStatusDegraded, agentmeshcontracts.TaskStatusFailed, agentmeshcontracts.TaskStatusCancelled, agentmeshcontracts.TaskStatusAwaitingReview:
				return task, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *AgenticService) applyResultToJob(ctx context.Context, job *horizonmodels.ScoutingJob, task *agentmeshcontracts.MeshTask, compareOnly bool) error {
	if job == nil || task == nil {
		return fmt.Errorf("job and mesh task are required")
	}
	fields := map[string]any{
		"agenticScout.lastTaskStatus": strings.TrimSpace(string(task.Status)),
		"agenticScout.completedAt":    time.Now().UTC().Format(time.RFC3339),
		"agenticScout.reviewAccepted": task.ReviewAccepted != nil && *task.ReviewAccepted,
		"agenticScout.output":         task.Output,
	}
	if err := s.updateJobState(ctx, job.ID, fields); err != nil {
		return err
	}
	if compareOnly {
		return nil
	}
	if task.Status == agentmeshcontracts.TaskStatusSucceeded || task.Status == agentmeshcontracts.TaskStatusDegraded {
		return s.workflow.TransitionState(ctx, job.ID, horizonmodels.ScoutingJobStateVerified)
	}
	return fmt.Errorf("agentic scout task %s failed with status=%s error=%s", task.ID, task.Status, strings.TrimSpace(task.LastError))
}

func (s *AgenticService) updateJobState(ctx context.Context, jobID string, fields map[string]any) error {
	if strings.TrimSpace(jobID) == "" || s.agentJobs == nil || len(fields) == 0 {
		return nil
	}
	return s.agentJobs.UpdateState(ctx, jobID, fields)
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
