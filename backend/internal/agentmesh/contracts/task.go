package contracts

import (
	"fmt"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskStatusPending        TaskStatus = "pending"
	TaskStatusRunning        TaskStatus = "running"
	TaskStatusAwaitingReview TaskStatus = "awaiting_review"
	TaskStatusSucceeded      TaskStatus = "succeeded"
	TaskStatusFailed         TaskStatus = "failed"
	TaskStatusCancelled      TaskStatus = "cancelled"
	TaskStatusDegraded       TaskStatus = "degraded"
)

type RetryClass string

const (
	RetryClassNone      RetryClass = "none"
	RetryClassTransient RetryClass = "transient"
	RetryClassPermanent RetryClass = "permanent"
)

type BudgetPolicy struct {
	MaxModelCalls int     `json:"maxModelCalls,omitempty" firestore:"maxModelCalls,omitempty"`
	MaxToolCalls  int     `json:"maxToolCalls,omitempty" firestore:"maxToolCalls,omitempty"`
	MaxSpendUSD   float64 `json:"maxSpendUsd,omitempty" firestore:"maxSpendUsd,omitempty"`
}

type UsageCounters struct {
	ModelCalls        int     `json:"modelCalls,omitempty" firestore:"modelCalls,omitempty"`
	ToolCalls         int     `json:"toolCalls,omitempty" firestore:"toolCalls,omitempty"`
	EstimatedSpendUSD float64 `json:"estimatedSpendUsd,omitempty" firestore:"estimatedSpendUsd,omitempty"`
}

type Lease struct {
	Owner       string    `json:"owner,omitempty" firestore:"owner,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty" firestore:"expiresAt,omitempty"`
	HeartbeatAt time.Time `json:"heartbeatAt,omitempty" firestore:"heartbeatAt,omitempty"`
	Attempt     int       `json:"attempt,omitempty" firestore:"attempt,omitempty"`
	DedupKey    string    `json:"dedupKey,omitempty" firestore:"dedupKey,omitempty"`
}

type MeshSubtask struct {
	ID           string     `json:"id" firestore:"id"`
	ParentTaskID string     `json:"parentTaskId" firestore:"parentTaskId"`
	AgentID      string     `json:"agentId" firestore:"agentId"`
	Capability   string     `json:"capability" firestore:"capability"`
	Status       TaskStatus `json:"status" firestore:"status"`
	CreatedAt    time.Time  `json:"createdAt" firestore:"createdAt"`
}

type MeshTask struct {
	ID                string         `json:"id" firestore:"id"`
	TenantID          string         `json:"tenantId" firestore:"tenantId"`
	RequestedAgentID  string         `json:"requestedAgentId,omitempty" firestore:"requestedAgentId,omitempty"`
	RoutedAgentID     string         `json:"routedAgentId,omitempty" firestore:"routedAgentId,omitempty"`
	ReviewAgentID     string         `json:"reviewAgentId,omitempty" firestore:"reviewAgentId,omitempty"`
	Capability        string         `json:"capability" firestore:"capability"`
	Status            TaskStatus     `json:"status" firestore:"status"`
	RetryClass        RetryClass     `json:"retryClass" firestore:"retryClass"`
	Input             map[string]any `json:"input,omitempty" firestore:"input,omitempty"`
	Output            map[string]any `json:"output,omitempty" firestore:"output,omitempty"`
	ReviewOutput      map[string]any `json:"reviewOutput,omitempty" firestore:"reviewOutput,omitempty"`
	ReviewAccepted    *bool          `json:"reviewAccepted,omitempty" firestore:"reviewAccepted,omitempty"`
	ApprovalRequired  bool           `json:"approvalRequired,omitempty" firestore:"approvalRequired,omitempty"`
	ApprovalActor     string         `json:"approvalActor,omitempty" firestore:"approvalActor,omitempty"`
	ApprovalRationale string         `json:"approvalRationale,omitempty" firestore:"approvalRationale,omitempty"`
	SessionRef        string         `json:"sessionRef,omitempty" firestore:"sessionRef,omitempty"`
	MemoryBankRef     string         `json:"memoryBankRef,omitempty" firestore:"memoryBankRef,omitempty"`
	ParentTaskID      string         `json:"parentTaskId,omitempty" firestore:"parentTaskId,omitempty"`
	Artifacts         []string       `json:"artifacts,omitempty" firestore:"artifacts,omitempty"`
	Subtasks          []MeshSubtask  `json:"subtasks,omitempty" firestore:"subtasks,omitempty"`
	Budget            BudgetPolicy   `json:"budget,omitempty" firestore:"budget,omitempty"`
	Usage             UsageCounters  `json:"usage,omitempty" firestore:"usage,omitempty"`
	Lease             Lease          `json:"lease,omitempty" firestore:"lease,omitempty"`
	LastError         string         `json:"lastError,omitempty" firestore:"lastError,omitempty"`
	CreatedAt         time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt" firestore:"updatedAt"`
	CompletedAt       *time.Time     `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
}

func (t *MeshTask) ValidateForCreate() error {
	if t == nil {
		return fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(t.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(t.Capability) == "" {
		return fmt.Errorf("capability is required")
	}
	if t.Status == "" {
		t.Status = TaskStatusPending
	}
	if t.RetryClass == "" {
		t.RetryClass = RetryClassTransient
	}
	if t.Input == nil {
		t.Input = map[string]any{}
	}
	return nil
}
