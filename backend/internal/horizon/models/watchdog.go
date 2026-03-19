package models

import (
	"fmt"
	"time"
)

// WatchdogConfig представляет конфигурацию фонового мониторинга HorizonFragment
type WatchdogConfig struct {
	ID                string         `json:"id" firestore:"id"`
	TenantID          string         `json:"tenantId" firestore:"tenantId"`
	SubjectID         string         `json:"subjectId,omitempty" firestore:"subjectId,omitempty"`
	TargetFragmentID  string         `json:"targetFragmentId" firestore:"targetFragmentId"`
	InterestTitle     string         `json:"interestTitle,omitempty" firestore:"interestTitle,omitempty"`
	InterestQuery     string         `json:"interestQuery,omitempty" firestore:"interestQuery,omitempty"`
	QueryOverrides    []string       `json:"queryOverrides,omitempty" firestore:"queryOverrides,omitempty"`
	PageFraming       map[string]any `json:"pageFraming,omitempty" firestore:"pageFraming,omitempty"`
	ScanInterval      string         `json:"scanInterval" firestore:"scanInterval"`               // ISO 8601 duration (PT1H)
	SourceIDs         []string       `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"` // DataSource IDs для мониторинга
	UseProjectSources bool           `json:"useProjectSources" firestore:"useProjectSources"`     // Использовать все Sources проекта
	TriggerThreshold  float64        `json:"triggerThreshold" firestore:"triggerThreshold"`       // Порог изменений (0..1)
	Status            string         `json:"status" firestore:"status"`                           // RUNNING, PAUSED
	LastScanAt        *time.Time     `json:"lastScanAt,omitempty" firestore:"lastScanAt,omitempty"`
	NextScanAt        *time.Time     `json:"nextScanAt,omitempty" firestore:"nextScanAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

// WatchdogStatus статус Watchdog
type WatchdogStatus string

const (
	WatchdogStatusRunning WatchdogStatus = "RUNNING"
	WatchdogStatusPaused  WatchdogStatus = "PAUSED"
)

// Validate проверяет валидность WatchdogConfig
func (w *WatchdogConfig) Validate() error {
	if w.TargetFragmentID == "" {
		return fmt.Errorf("targetFragmentId is required")
	}
	if w.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if w.ScanInterval == "" {
		return fmt.Errorf("scanInterval is required")
	}
	if w.TriggerThreshold < 0 || w.TriggerThreshold > 1 {
		return fmt.Errorf("triggerThreshold must be between 0 and 1")
	}
	if w.Status != string(WatchdogStatusRunning) && w.Status != string(WatchdogStatusPaused) {
		return fmt.Errorf("status must be RUNNING or PAUSED")
	}
	return nil
}
