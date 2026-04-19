package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type SignalRetroAttachStatus string

const (
	SignalRetroAttachStatusPending    SignalRetroAttachStatus = "pending"
	SignalRetroAttachStatusProcessing SignalRetroAttachStatus = "processing"
	SignalRetroAttachStatusAttached   SignalRetroAttachStatus = "attached"
	SignalRetroAttachStatusIgnored    SignalRetroAttachStatus = "ignored"
	SignalRetroAttachStatusFailed     SignalRetroAttachStatus = "failed"
)

type SignalRetroAttachQueueItem struct {
	ID            string                  `json:"id" firestore:"id"`
	TenantID      string                  `json:"tenantId" firestore:"tenantId"`
	FragmentID      string                  `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	CanonicalPath   string                  `json:"canonicalPath" firestore:"canonicalPath"`
	FragmentClass   ah.FragmentClass        `json:"fragmentClass,omitempty" firestore:"fragmentClass,omitempty"`
	FragmentSubtype string                  `json:"fragmentSubtype,omitempty" firestore:"fragmentSubtype,omitempty"`
	Source          string                  `json:"source,omitempty" firestore:"source,omitempty"`
	Status        SignalRetroAttachStatus `json:"status,omitempty" firestore:"status,omitempty"`
	ObservedCount int                     `json:"observedCount,omitempty" firestore:"observedCount,omitempty"`
	AttemptCount  int                     `json:"attemptCount,omitempty" firestore:"attemptCount,omitempty"`
	FirstSeenAt   time.Time               `json:"firstSeenAt" firestore:"firstSeenAt"`
	LastSeenAt    time.Time               `json:"lastSeenAt" firestore:"lastSeenAt"`
	ReadyAt       time.Time               `json:"readyAt" firestore:"readyAt"`
	LastError     string                  `json:"lastError,omitempty" firestore:"lastError,omitempty"`
	LastResult    string                  `json:"lastResult,omitempty" firestore:"lastResult,omitempty"`
	UpdatedAt     time.Time               `json:"updatedAt" firestore:"updatedAt"`
}

func (i *SignalRetroAttachQueueItem) Validate() error {
	if i == nil {
		return fmt.Errorf("retro attach queue item is nil")
	}
	i.TenantID = strings.TrimSpace(i.TenantID)
	i.FragmentID = strings.TrimSpace(i.FragmentID)
	i.CanonicalPath = strings.TrimSpace(i.CanonicalPath)
	i.Source = strings.TrimSpace(i.Source)
	i.LastError = strings.TrimSpace(i.LastError)
	i.LastResult = strings.TrimSpace(i.LastResult)
	if i.TenantID == "" {
		i.TenantID = "global"
	}
	if i.CanonicalPath == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	switch i.Status {
	case SignalRetroAttachStatusPending,
		SignalRetroAttachStatusProcessing,
		SignalRetroAttachStatusAttached,
		SignalRetroAttachStatusIgnored,
		SignalRetroAttachStatusFailed,
		"":
	default:
		return fmt.Errorf("invalid status %q", i.Status)
	}
	if i.Status == "" {
		i.Status = SignalRetroAttachStatusPending
	}
	now := time.Now().UTC()
	if i.FirstSeenAt.IsZero() {
		i.FirstSeenAt = now
	}
	if i.LastSeenAt.IsZero() {
		i.LastSeenAt = i.FirstSeenAt
	}
	if i.ReadyAt.IsZero() {
		i.ReadyAt = i.LastSeenAt
	}
	if i.UpdatedAt.IsZero() {
		i.UpdatedAt = now
	}
	if i.ObservedCount <= 0 {
		i.ObservedCount = 1
	}
	return nil
}
