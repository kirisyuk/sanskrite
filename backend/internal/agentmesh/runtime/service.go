package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/tasks"
	meshtrace "github.com/igorkirisyuk/sanskrite/internal/agentmesh/trace"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
)

type Service struct {
	store          *repo.Store
	control        *controlplane.Service
	vertex         *vertex.Service
	traces         *meshtrace.Service
	dispatcher     *tasks.Dispatcher
	artifactBucket string
	logger         *log.Logger
}

func NewService(store *repo.Store, control *controlplane.Service, vertexSvc *vertex.Service, traces *meshtrace.Service, dispatcher *tasks.Dispatcher, artifactBucket string, logger *log.Logger) *Service {
	return &Service{
		store:          store,
		control:        control,
		vertex:         vertexSvc,
		traces:         traces,
		dispatcher:     dispatcher,
		artifactBucket: strings.TrimSpace(artifactBucket),
		logger:         logger,
	}
}

func (s *Service) CreateTask(ctx context.Context, task *contracts.MeshTask, req policy.RequestContext, skipEnqueue bool) (*contracts.MeshTask, error) {
	if task != nil && strings.TrimSpace(task.Lease.DedupKey) != "" {
		existing, err := s.store.FindTaskByDedupKey(ctx, task.TenantID, task.Lease.DedupKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	agent, err := s.control.PrepareTask(task, req)
	if err != nil {
		return nil, err
	}
	if task.Budget.MaxModelCalls == 0 {
		task.Budget.MaxModelCalls = 3
	}
	if task.Budget.MaxToolCalls == 0 {
		task.Budget.MaxToolCalls = 8
	}
	if task.Budget.MaxSpendUSD == 0 {
		task.Budget.MaxSpendUSD = 1.5
	}
	task.RoutedAgentID = agent.ID
	task.ReviewAgentID = "mesh.reviewer"
	task.Status = contracts.TaskStatusPending
	id, err := s.store.CreateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	task.ID = id
	if !skipEnqueue && s.dispatcher != nil {
		if err := s.dispatcher.EnqueueTask(ctx, id); err != nil {
			return nil, err
		}
	}
	return task, nil
}

func (s *Service) ExecuteTask(ctx context.Context, taskID, leaseOwner string) error {
	task, err := s.store.ClaimTask(ctx, taskID, leaseOwner, 2*time.Minute)
	if err != nil {
		current, getErr := s.store.GetTask(ctx, taskID)
		if getErr == nil && current != nil {
			switch current.Status {
			case contracts.TaskStatusSucceeded, contracts.TaskStatusFailed, contracts.TaskStatusCancelled, contracts.TaskStatusAwaitingReview, contracts.TaskStatusDegraded:
				return nil
			}
		}
		return err
	}
	span, err := s.traces.StartSpan(ctx, task.TenantID, task.ID, task.RoutedAgentID, task.Capability, "task.execute", map[string]any{
		"leaseOwner": leaseOwner,
	})
	if err != nil {
		return err
	}
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()
	go s.leaseHeartbeatLoop(heartbeatCtx, task.ID, leaseOwner, 2*time.Minute)

	usage := task.Usage

	task.SessionRef, err = s.vertex.EnsureSession(ctx, task)
	if err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", nil, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	task.MemoryBankRef, err = s.vertex.EnsureMemoryBank(ctx, task)
	if err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", nil, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	if err := s.store.UpdateTaskRuntimeState(ctx, task.ID, leaseOwner, task.SessionRef, task.MemoryBankRef); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", nil, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	if err := s.vertex.AppendSessionEvent(ctx, task.SessionRef, "user", "user", stringify(task.Input), map[string]any{
		"taskId":     task.ID,
		"capability": task.Capability,
		"phase":      "input",
	}); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", nil, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}

	output, cost, err := s.vertex.ExecuteTask(ctx, task, false)
	if err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, classifyRetry(err), err, usage)
	}
	usage.ModelCalls++
	if cost != nil {
		usage.EstimatedSpendUSD += cost.EstimatedUSD
	}
	if err := s.enforceBudget(task.Budget, usage); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassPermanent, err, usage)
	}
	if err := s.store.UpdateTaskUsage(ctx, task.ID, leaseOwner, usage); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	if err := s.vertex.AppendSessionEvent(ctx, task.SessionRef, task.RoutedAgentID, "model", stringify(output), map[string]any{
		"taskId": task.ID,
		"phase":  "worker-output",
	}); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}

	artifactID := uuid.NewString()
	artifact := &contracts.ArtifactRef{
		ID:       artifactID,
		TenantID: task.TenantID,
		TaskID:   task.ID,
		Kind:     "task-output",
		Metadata: map[string]string{"agentId": task.RoutedAgentID},
	}
	if err := s.store.SaveArtifactPayload(ctx, s.artifactBucket, artifact, output); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}

	reviewSpan, err := s.traces.StartSpan(ctx, task.TenantID, task.ID, task.ReviewAgentID, "mesh.review", "task.review", nil)
	if err != nil {
		return err
	}
	reviewTask := *task
	reviewTask.Input = map[string]any{
		"output":     output,
		"artifact":   artifact,
		"capability": task.Capability,
	}
	reviewOutput, reviewCost, err := s.vertex.ExecuteTask(ctx, &reviewTask, true)
	if err != nil {
		_ = s.traces.FinishSpan(ctx, reviewSpan, "failed", reviewCost, map[string]any{"error": err.Error()})
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, classifyRetry(err), err, usage)
	}
	usage.ModelCalls++
	if reviewCost != nil {
		usage.EstimatedSpendUSD += reviewCost.EstimatedUSD
	}
	if err := s.enforceBudget(task.Budget, usage); err != nil {
		_ = s.traces.FinishSpan(ctx, reviewSpan, "failed", reviewCost, map[string]any{"error": err.Error()})
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassPermanent, err, usage)
	}
	if err := s.store.UpdateTaskUsage(ctx, task.ID, leaseOwner, usage); err != nil {
		_ = s.traces.FinishSpan(ctx, reviewSpan, "failed", reviewCost, map[string]any{"error": err.Error()})
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	accepted, rationale := interpretReview(reviewOutput)
	_ = s.traces.FinishSpan(ctx, reviewSpan, ternary(accepted, "accepted", "rejected"), reviewCost, map[string]any{"rationale": rationale})
	if err := s.vertex.AppendSessionEvent(ctx, task.SessionRef, task.ReviewAgentID, "model", stringify(reviewOutput), map[string]any{
		"taskId": task.ID,
		"phase":  "review-output",
	}); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}
	if err := s.vertex.GenerateMemoriesFromSession(ctx, task); err != nil {
		_ = s.traces.FinishSpan(ctx, span, "failed", cost, map[string]any{"error": err.Error()})
		return s.retryOrFail(ctx, task, leaseOwner, contracts.RetryClassTransient, err, usage)
	}

	finalOutput := map[string]any{
		"result":          output,
		"artifactId":      artifactID,
		"sessionRef":      task.SessionRef,
		"memoryBankRef":   task.MemoryBankRef,
		"review":          reviewOutput,
		"reviewAccepted":  accepted,
		"reviewRationale": rationale,
		"usage":           usage,
	}
	task.ReviewOutput = reviewOutput
	task.Output = finalOutput
	task.ReviewAccepted = &accepted
	if accepted {
		if task.ApprovalRequired {
			_ = s.traces.FinishSpan(ctx, span, "awaiting_review", cost, map[string]any{"artifactId": artifactID, "reviewRationale": rationale})
			return s.store.AwaitReview(ctx, task.ID, leaseOwner, finalOutput, reviewOutput, accepted, []string{artifactID}, true, rationale, usage)
		}
		_ = s.traces.FinishSpan(ctx, span, "succeeded", cost, map[string]any{"artifactId": artifactID})
		return s.store.CompleteTask(ctx, task.ID, leaseOwner, contracts.TaskStatusSucceeded, finalOutput, []string{artifactID})
	}
	_ = s.traces.FinishSpan(ctx, span, "degraded", cost, map[string]any{"artifactId": artifactID, "reviewRationale": rationale})
	return s.store.FailTask(ctx, task.ID, leaseOwner, contracts.RetryClassPermanent, rationale, true)
}

func classifyRetry(err error) contracts.RetryClass {
	if err == nil {
		return contracts.RetryClassNone
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "invalid"), strings.Contains(msg, "signature"):
		return contracts.RetryClassPermanent
	default:
		return contracts.RetryClassTransient
	}
}

func interpretReview(review map[string]any) (bool, string) {
	if accepted, ok := review["accepted"].(bool); ok {
		rationale, _ := review["rationale"].(string)
		if strings.TrimSpace(rationale) == "" {
			rationale = "review completed"
		}
		return accepted, rationale
	}
	raw, _ := json.Marshal(review)
	text := strings.ToLower(string(raw))
	if strings.Contains(text, `"accepted":true`) || strings.Contains(text, `"accepted": true`) {
		return true, "review accepted"
	}
	if strings.Contains(text, `"accepted":false`) || strings.Contains(text, `"accepted": false`) {
		return false, "review rejected"
	}
	return strings.Contains(text, "accept"), string(raw)
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func (s *Service) Readiness() error {
	if s.store == nil || s.control == nil || s.vertex == nil {
		return fmt.Errorf("runtime is not fully configured")
	}
	return s.vertex.Readiness(context.Background())
}

func (s *Service) ResolveReview(ctx context.Context, taskID, actor, rationale string, approved bool) error {
	return s.store.ResolveReview(ctx, taskID, actor, rationale, approved)
}

func (s *Service) CancelTask(ctx context.Context, taskID, actor, reason string) error {
	return s.store.CancelTask(ctx, taskID, actor, reason)
}

func (s *Service) leaseHeartbeatLoop(ctx context.Context, taskID, leaseOwner string, leaseDuration time.Duration) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.store.HeartbeatLease(context.Background(), taskID, leaseOwner, leaseDuration); err != nil && s.logger != nil {
				s.logger.Printf("lease heartbeat failed: taskId=%s owner=%s err=%v", taskID, leaseOwner, err)
			}
		}
	}
}

func (s *Service) retryOrFail(ctx context.Context, task *contracts.MeshTask, leaseOwner string, retryClass contracts.RetryClass, err error, usage contracts.UsageCounters) error {
	if retryClass == contracts.RetryClassTransient {
		maxAttempts := 5
		if task != nil && task.Lease.Attempt >= maxAttempts {
			return s.store.FailTask(ctx, task.ID, leaseOwner, contracts.RetryClassPermanent, err.Error(), false)
		}
		if retryErr := s.store.RetryTask(ctx, task.ID, leaseOwner, retryClass, err.Error(), usage); retryErr != nil {
			return retryErr
		}
		return err
	}
	return s.store.FailTask(ctx, task.ID, leaseOwner, retryClass, err.Error(), false)
}

func (s *Service) enforceBudget(budget contracts.BudgetPolicy, usage contracts.UsageCounters) error {
	if budget.MaxModelCalls > 0 && usage.ModelCalls > budget.MaxModelCalls {
		return fmt.Errorf("model call budget exceeded")
	}
	if budget.MaxToolCalls > 0 && usage.ToolCalls > budget.MaxToolCalls {
		return fmt.Errorf("tool call budget exceeded")
	}
	if budget.MaxSpendUSD > 0 && usage.EstimatedSpendUSD > budget.MaxSpendUSD {
		return fmt.Errorf("spend budget exceeded")
	}
	return nil
}

func stringify(v any) string {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return string(raw)
}
