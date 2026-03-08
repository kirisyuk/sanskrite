package models

import "time"

// SourceCadencePolicy stores configuration for adaptive polling.
// This is source policy, not runtime state.
type SourceCadencePolicy struct {
	BaseIntervalMinutes         *int     `json:"baseIntervalMinutes,omitempty" firestore:"baseIntervalMinutes,omitempty"`
	MinIntervalMinutes          *int     `json:"minIntervalMinutes,omitempty" firestore:"minIntervalMinutes,omitempty"`
	MaxIntervalMinutes          *int     `json:"maxIntervalMinutes,omitempty" firestore:"maxIntervalMinutes,omitempty"`
	EmptyBackoffMultiplier      *float64 `json:"emptyBackoffMultiplier,omitempty" firestore:"emptyBackoffMultiplier,omitempty"`
	ErrorBackoffMultiplier      *float64 `json:"errorBackoffMultiplier,omitempty" firestore:"errorBackoffMultiplier,omitempty"`
	FreshnessBoostWindowMinutes *int     `json:"freshnessBoostWindowMinutes,omitempty" firestore:"freshnessBoostWindowMinutes,omitempty"`
	LeaseDurationMinutes        *int     `json:"leaseDurationMinutes,omitempty" firestore:"leaseDurationMinutes,omitempty"`
}

// SourceRuntimeState stores mutable scheduler/execution state separately from source policy.
type SourceRuntimeState struct {
	ID       string `json:"id" firestore:"id"`
	SourceID string `json:"sourceId" firestore:"sourceId"`
	TenantID string `json:"tenantId" firestore:"tenantId"`
	Kind     string `json:"kind" firestore:"kind"`

	Scope          string `json:"scope,omitempty" firestore:"scope,omitempty"`
	ProcessingMode string `json:"processingMode,omitempty" firestore:"processingMode,omitempty"`
	OutputMode     string `json:"outputMode,omitempty" firestore:"outputMode,omitempty"`

	LastPolledAt      *time.Time `json:"lastPolledAt,omitempty" firestore:"lastPolledAt,omitempty"`
	LastFoundAt       *time.Time `json:"lastFoundAt,omitempty" firestore:"lastFoundAt,omitempty"`
	LastSuccessAt     *time.Time `json:"lastSuccessAt,omitempty" firestore:"lastSuccessAt,omitempty"`
	LastErrorAt       *time.Time `json:"lastErrorAt,omitempty" firestore:"lastErrorAt,omitempty"`
	LastCoordinatedAt *time.Time `json:"lastCoordinatedAt,omitempty" firestore:"lastCoordinatedAt,omitempty"`
	LastEnqueuedAt    *time.Time `json:"lastEnqueuedAt,omitempty" firestore:"lastEnqueuedAt,omitempty"`
	NextPollAt        *time.Time `json:"nextPollAt,omitempty" firestore:"nextPollAt,omitempty"`

	LeaseOwner *string    `json:"leaseOwner,omitempty" firestore:"leaseOwner,omitempty"`
	LeaseUntil *time.Time `json:"leaseUntil,omitempty" firestore:"leaseUntil,omitempty"`

	ConsecutiveEmptyPolls int `json:"consecutiveEmptyPolls" firestore:"consecutiveEmptyPolls"`
	ConsecutiveErrors     int `json:"consecutiveErrors" firestore:"consecutiveErrors"`

	LastStatus     *string  `json:"lastStatus,omitempty" firestore:"lastStatus,omitempty"`
	LastError      *string  `json:"lastError,omitempty" firestore:"lastError,omitempty"`
	LastYield      *float64 `json:"lastYield,omitempty" firestore:"lastYield,omitempty"`
	AvgYield       *float64 `json:"avgYield,omitempty" firestore:"avgYield,omitempty"`
	PollBackoffSec *int     `json:"pollBackoffSec,omitempty" firestore:"pollBackoffSec,omitempty"`

	TotalRuns               int `json:"totalRuns" firestore:"totalRuns"`
	TotalDocumentsProcessed int `json:"totalDocumentsProcessed" firestore:"totalDocumentsProcessed"`
	TotalDocumentsCreated   int `json:"totalDocumentsCreated" firestore:"totalDocumentsCreated"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}
