package models

import "time"

type JobTelemetry struct {
	ID          string           `json:"id" firestore:"id"`
	TenantID    string           `json:"tenantId" firestore:"tenantId"`
	ProjectID   string           `json:"projectId" firestore:"projectId"`
	LensID      string           `json:"lensId" firestore:"lensId"`
	JobID       string           `json:"jobId" firestore:"jobId"`
	JobType     string           `json:"jobType" firestore:"jobType"`
	Trigger     string           `json:"trigger" firestore:"trigger"`
	StartedAt   time.Time        `json:"startedAt" firestore:"startedAt"`
	CompletedAt *time.Time       `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
	DurationMs  int64            `json:"durationMs" firestore:"durationMs"`
	Status      string           `json:"status" firestore:"status"`
	Groups      []TelemetryGroup `json:"groups" firestore:"groups"`
	Rollup      TelemetryRollup  `json:"rollup" firestore:"rollup"`
}

type TelemetryGroup struct {
	Name       string               `json:"name" firestore:"name"`
	StartedAt  time.Time            `json:"startedAt" firestore:"startedAt"`
	DurationMs int64                `json:"durationMs" firestore:"durationMs"`
	Operations []TelemetryOperation `json:"operations" firestore:"operations"`
}

type TelemetryOperation struct {
	Kind         string  `json:"kind" firestore:"kind"`
	Name         string  `json:"name" firestore:"name"`
	Count        int     `json:"count" firestore:"count"`
	DurationMs   int64   `json:"durationMs" firestore:"durationMs"`
	Provider     string  `json:"provider,omitempty" firestore:"provider,omitempty"`
	Model        string  `json:"model,omitempty" firestore:"model,omitempty"`
	ModelAlias   string  `json:"modelAlias,omitempty" firestore:"modelAlias,omitempty"`
	InputTokens  int     `json:"inputTokens,omitempty" firestore:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty" firestore:"outputTokens,omitempty"`
	TotalTokens  int     `json:"totalTokens,omitempty" firestore:"totalTokens,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty" firestore:"costUsd,omitempty"`
	Cacheable    bool    `json:"cacheable,omitempty" firestore:"cacheable,omitempty"`
	APICalls     int     `json:"apiCalls,omitempty" firestore:"apiCalls,omitempty"`
	CacheHits    int     `json:"cacheHits,omitempty" firestore:"cacheHits,omitempty"`
}

type TelemetryRollup struct {
	TotalDurationMs  int64   `json:"totalDurationMs" firestore:"totalDurationMs"`
	LLMCalls         int     `json:"llmCalls" firestore:"llmCalls"`
	LLMInputTokens   int     `json:"llmInputTokens" firestore:"llmInputTokens"`
	LLMOutputTokens  int     `json:"llmOutputTokens" firestore:"llmOutputTokens"`
	LLMCostUSD       float64 `json:"llmCostUsd" firestore:"llmCostUsd"`
	ExternalAPICalls int     `json:"externalApiCalls" firestore:"externalApiCalls"`
	FirestoreReads   int     `json:"firestoreReads" firestore:"firestoreReads"`
	FirestoreWrites  int     `json:"firestoreWrites" firestore:"firestoreWrites"`
}
