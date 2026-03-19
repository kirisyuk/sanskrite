package contracts

import "time"

type TraceSpan struct {
	ID           string           `json:"id" firestore:"id"`
	TenantID     string           `json:"tenantId" firestore:"tenantId"`
	TaskID       string           `json:"taskId" firestore:"taskId"`
	ParentSpanID string           `json:"parentSpanId,omitempty" firestore:"parentSpanId,omitempty"`
	Kind         string           `json:"kind" firestore:"kind"`
	AgentID      string           `json:"agentId,omitempty" firestore:"agentId,omitempty"`
	Capability   string           `json:"capability,omitempty" firestore:"capability,omitempty"`
	Status       string           `json:"status" firestore:"status"`
	Attributes   map[string]any   `json:"attributes,omitempty" firestore:"attributes,omitempty"`
	Cost         *CostAttribution `json:"cost,omitempty" firestore:"cost,omitempty"`
	StartedAt    time.Time        `json:"startedAt" firestore:"startedAt"`
	EndedAt      *time.Time       `json:"endedAt,omitempty" firestore:"endedAt,omitempty"`
}

type LineageEdge struct {
	ID         string    `json:"id" firestore:"id"`
	FromTaskID string    `json:"fromTaskId" firestore:"fromTaskId"`
	ToTaskID   string    `json:"toTaskId" firestore:"toTaskId"`
	Relation   string    `json:"relation" firestore:"relation"`
	CreatedAt  time.Time `json:"createdAt" firestore:"createdAt"`
}

type CostAttribution struct {
	Model            string  `json:"model,omitempty" firestore:"model,omitempty"`
	Tool             string  `json:"tool,omitempty" firestore:"tool,omitempty"`
	PromptTokens     int     `json:"promptTokens,omitempty" firestore:"promptTokens,omitempty"`
	CompletionTokens int     `json:"completionTokens,omitempty" firestore:"completionTokens,omitempty"`
	EstimatedUSD     float64 `json:"estimatedUsd,omitempty" firestore:"estimatedUsd,omitempty"`
}
