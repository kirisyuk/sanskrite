package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	gcputil "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"google.golang.org/api/iterator"
)

type Store struct {
	client       *firestore.Client
	agents       *firestore.CollectionRef
	capabilities *firestore.CollectionRef
	tasks        *firestore.CollectionRef
	artifacts    *firestore.CollectionRef
	traces       *firestore.CollectionRef
	policies     *firestore.CollectionRef
	runtimeLocks *firestore.CollectionRef
	gcs          *gcputil.GCSClient
}

func NewStore(client *firestore.Client, gcs *gcputil.GCSClient) *Store {
	return &Store{
		client:       client,
		agents:       client.Collection("agentmesh_agents"),
		capabilities: client.Collection("agentmesh_capabilities"),
		tasks:        client.Collection("agentmesh_tasks"),
		artifacts:    client.Collection("agentmesh_artifacts"),
		traces:       client.Collection("agentmesh_traces"),
		policies:     client.Collection("agentmesh_policies"),
		runtimeLocks: client.Collection("agentmesh_runtime_locks"),
		gcs:          gcs,
	}
}

func (s *Store) SaveAgent(ctx context.Context, agent *contracts.AgentSpec) error {
	if s == nil || agent == nil {
		return fmt.Errorf("store or agent is nil")
	}
	if err := agent.Validate(); err != nil {
		return err
	}
	_, err := s.agents.Doc(strings.TrimSpace(agent.ID)).Set(ctx, agent)
	return err
}

func (s *Store) SaveCapability(ctx context.Context, capability contracts.CapabilitySpec) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	name := strings.TrimSpace(capability.Name)
	if name == "" {
		return fmt.Errorf("capability name is required")
	}
	_, err := s.capabilities.Doc(name).Set(ctx, capability)
	return err
}

func (s *Store) CreateTask(ctx context.Context, task *contracts.MeshTask) (string, error) {
	if s == nil || task == nil {
		return "", fmt.Errorf("store or task is nil")
	}
	if err := task.ValidateForCreate(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	doc := s.tasks.NewDoc()
	task.ID = doc.ID
	if strings.TrimSpace(task.Lease.DedupKey) == "" {
		task.Lease.DedupKey = doc.ID
	}
	_, err := doc.Set(ctx, task)
	return task.ID, err
}

func (s *Store) FindTaskByDedupKey(ctx context.Context, tenantID, dedupKey string) (*contracts.MeshTask, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	dedupKey = strings.TrimSpace(dedupKey)
	if tenantID == "" || dedupKey == "" {
		return nil, nil
	}
	iter := s.tasks.
		Where("tenantId", "==", tenantID).
		Where("lease.dedupKey", "==", dedupKey).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()
	snap, err := iter.Next()
	if err != nil {
		if err == iterator.Done {
			return nil, nil
		}
		return nil, err
	}
	var task contracts.MeshTask
	if err := snap.DataTo(&task); err != nil {
		return nil, err
	}
	if task.ID == "" {
		task.ID = snap.Ref.ID
	}
	return &task, nil
}

func (s *Store) GetTask(ctx context.Context, taskID string) (*contracts.MeshTask, error) {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("taskID is required")
	}
	snap, err := s.tasks.Doc(strings.TrimSpace(taskID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var task contracts.MeshTask
	if err := snap.DataTo(&task); err != nil {
		return nil, err
	}
	if task.ID == "" {
		task.ID = snap.Ref.ID
	}
	return &task, nil
}

func (s *Store) ClaimTask(ctx context.Context, taskID, leaseOwner string, leaseDuration time.Duration) (*contracts.MeshTask, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	taskID = strings.TrimSpace(taskID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if taskID == "" || leaseOwner == "" {
		return nil, fmt.Errorf("taskID and leaseOwner are required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	var claimed contracts.MeshTask
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(taskID)
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		if err := snap.DataTo(&claimed); err != nil {
			return err
		}
		if claimed.ID == "" {
			claimed.ID = snap.Ref.ID
		}
		now := time.Now().UTC()
		if !CanClaimLease(claimed.Status, claimed.Lease, now) {
			return fmt.Errorf("task %s is not claimable", taskID)
		}
		lease := NewLease(claimed.Lease, leaseOwner, now, leaseDuration)
		claimed.Status = contracts.TaskStatusRunning
		claimed.Lease = lease
		claimed.UpdatedAt = now
		return tx.Set(ref, claimed)
	})
	if err != nil {
		return nil, err
	}
	return &claimed, nil
}

func (s *Store) HeartbeatLease(ctx context.Context, taskID, leaseOwner string, leaseDuration time.Duration) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	taskID = strings.TrimSpace(taskID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if taskID == "" || leaseOwner == "" {
		return fmt.Errorf("taskID and leaseOwner are required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(taskID)
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		now := time.Now().UTC()
		if strings.TrimSpace(task.Lease.Owner) != leaseOwner {
			return fmt.Errorf("lease owner mismatch")
		}
		task.Lease.ExpiresAt = now.Add(leaseDuration)
		task.Lease.HeartbeatAt = now
		task.UpdatedAt = now
		return tx.Set(ref, task)
	})
}

func (s *Store) UpdateTaskRuntimeState(ctx context.Context, taskID, leaseOwner, sessionRef, memoryBankRef string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		if strings.TrimSpace(sessionRef) != "" {
			task.SessionRef = strings.TrimSpace(sessionRef)
		}
		if strings.TrimSpace(memoryBankRef) != "" {
			task.MemoryBankRef = strings.TrimSpace(memoryBankRef)
		}
		task.UpdatedAt = now
		return tx.Set(ref, task)
	})
}

func (s *Store) UpdateTaskUsage(ctx context.Context, taskID, leaseOwner string, usage contracts.UsageCounters) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		task.Usage = usage
		task.UpdatedAt = now
		return tx.Set(ref, task)
	})
}

func (s *Store) AwaitReview(ctx context.Context, taskID, leaseOwner string, output, reviewOutput map[string]any, accepted bool, artifactIDs []string, approvalRequired bool, rationale string, usage contracts.UsageCounters) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		task.Status = contracts.TaskStatusAwaitingReview
		task.Output = output
		task.ReviewOutput = reviewOutput
		task.ReviewAccepted = boolPtr(accepted)
		task.ApprovalRequired = approvalRequired
		task.ApprovalRationale = strings.TrimSpace(rationale)
		task.Artifacts = artifactIDs
		task.Usage = usage
		task.Lease = contracts.Lease{DedupKey: task.Lease.DedupKey, Attempt: task.Lease.Attempt}
		task.UpdatedAt = now
		return tx.Set(ref, task)
	})
}

func (s *Store) CompleteTask(ctx context.Context, taskID, leaseOwner string, status contracts.TaskStatus, output map[string]any, artifactIDs []string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if status == "" {
		status = contracts.TaskStatusSucceeded
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		task.Status = status
		task.Output = output
		if review, ok := output["review"].(map[string]any); ok {
			task.ReviewOutput = review
		}
		if accepted, ok := output["reviewAccepted"].(bool); ok {
			task.ReviewAccepted = boolPtr(accepted)
		}
		task.Artifacts = artifactIDs
		task.Lease = contracts.Lease{DedupKey: task.Lease.DedupKey, Attempt: task.Lease.Attempt}
		task.UpdatedAt = now
		task.CompletedAt = &now
		return tx.Set(ref, task)
	})
}

func (s *Store) ResolveReview(ctx context.Context, taskID, actor, rationale string, approved bool) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if task.Status != contracts.TaskStatusAwaitingReview {
			return fmt.Errorf("task %s is not awaiting review", taskID)
		}
		task.ApprovalActor = strings.TrimSpace(actor)
		task.ApprovalRationale = strings.TrimSpace(rationale)
		task.ReviewAccepted = boolPtr(approved)
		task.UpdatedAt = now
		task.CompletedAt = &now
		if approved {
			task.Status = contracts.TaskStatusSucceeded
		} else {
			task.Status = contracts.TaskStatusFailed
			task.LastError = strings.TrimSpace(rationale)
			task.RetryClass = contracts.RetryClassPermanent
		}
		return tx.Set(ref, task)
	})
}

func (s *Store) CancelTask(ctx context.Context, taskID, actor, reason string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if task.Status == contracts.TaskStatusSucceeded || task.Status == contracts.TaskStatusFailed || task.Status == contracts.TaskStatusCancelled {
			return nil
		}
		task.Status = contracts.TaskStatusCancelled
		task.ApprovalActor = strings.TrimSpace(actor)
		task.ApprovalRationale = strings.TrimSpace(reason)
		task.LastError = strings.TrimSpace(reason)
		task.Lease = contracts.Lease{DedupKey: task.Lease.DedupKey, Attempt: task.Lease.Attempt}
		task.UpdatedAt = now
		task.CompletedAt = &now
		return tx.Set(ref, task)
	})
}

func (s *Store) RetryTask(ctx context.Context, taskID, leaseOwner string, retryClass contracts.RetryClass, message string, usage contracts.UsageCounters) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		task.Status = contracts.TaskStatusPending
		task.LastError = strings.TrimSpace(message)
		task.RetryClass = retryClass
		task.Usage = usage
		task.Lease = contracts.Lease{DedupKey: task.Lease.DedupKey, Attempt: task.Lease.Attempt}
		task.UpdatedAt = now
		return tx.Set(ref, task)
	})
}

func (s *Store) FailTask(ctx context.Context, taskID, leaseOwner string, retryClass contracts.RetryClass, message string, degraded bool) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.tasks.Doc(strings.TrimSpace(taskID))
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var task contracts.MeshTask
		if err := snap.DataTo(&task); err != nil {
			return err
		}
		if strings.TrimSpace(task.Lease.Owner) != strings.TrimSpace(leaseOwner) {
			return fmt.Errorf("lease owner mismatch")
		}
		task.LastError = strings.TrimSpace(message)
		task.RetryClass = retryClass
		if degraded {
			task.Status = contracts.TaskStatusDegraded
		} else {
			task.Status = contracts.TaskStatusFailed
		}
		task.Lease = contracts.Lease{DedupKey: task.Lease.DedupKey, Attempt: task.Lease.Attempt}
		task.UpdatedAt = now
		task.CompletedAt = &now
		return tx.Set(ref, task)
	})
}

func boolPtr(v bool) *bool {
	return &v
}

func (s *Store) SaveTrace(ctx context.Context, span *contracts.TraceSpan) error {
	if s == nil || span == nil {
		return fmt.Errorf("store or span is nil")
	}
	if strings.TrimSpace(span.ID) == "" {
		ref := s.traces.NewDoc()
		span.ID = ref.ID
	}
	_, err := s.traces.Doc(span.ID).Set(ctx, span)
	return err
}

func (s *Store) SavePolicySnapshot(ctx context.Context, name string, payload map[string]any) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("policy name is required")
	}
	body := map[string]any{
		"name":      name,
		"payload":   payload,
		"updatedAt": time.Now().UTC(),
	}
	_, err := s.policies.Doc(name).Set(ctx, body)
	return err
}

func (s *Store) SaveArtifactPayload(ctx context.Context, bucket string, artifact *contracts.ArtifactRef, payload any) error {
	if s == nil || artifact == nil {
		return fmt.Errorf("store or artifact is nil")
	}
	if s.gcs == nil {
		return fmt.Errorf("gcs client is not configured")
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	artifact.Hash = hex.EncodeToString(sum[:])
	artifact.ContentType = "application/json"
	artifact.Bucket = bucket
	if strings.TrimSpace(artifact.Object) == "" {
		artifact.Object = path.Join(strings.TrimSpace(artifact.TenantID), "tasks", strings.TrimSpace(artifact.TaskID), fmt.Sprintf("%s.json", strings.TrimSpace(artifact.ID)))
	}
	if artifact.Metadata == nil {
		artifact.Metadata = map[string]string{}
	}
	artifact.Metadata["sha256"] = artifact.Hash
	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	if err := s.gcs.UploadBytes(ctx, bucket, artifact.Object, raw, artifact.ContentType, artifact.Metadata); err != nil {
		return err
	}
	_, err = s.artifacts.Doc(strings.TrimSpace(artifact.ID)).Set(ctx, artifact)
	return err
}
