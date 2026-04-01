package models

import (
	"fmt"
	"strings"
	"time"
)

type MonitoringDigestScopeType string

const (
	MonitoringDigestScopeLens      MonitoringDigestScopeType = "lens"
	MonitoringDigestScopePortfolio MonitoringDigestScopeType = "portfolio"
)

type MonitoringDigest struct {
	ID               string           `json:"id" firestore:"id"`
	TenantID         string           `json:"tenantId" firestore:"tenantId"`
	ScopeType        string           `json:"scopeType" firestore:"scopeType"`
	ScopeID          string           `json:"scopeId" firestore:"scopeId"`
	ScopeLabel       string           `json:"scopeLabel,omitempty" firestore:"scopeLabel,omitempty"`
	DateKey          string           `json:"dateKey" firestore:"dateKey"`
	WindowStart      time.Time        `json:"windowStart" firestore:"windowStart"`
	WindowEnd        time.Time        `json:"windowEnd" firestore:"windowEnd"`
	Headline         string           `json:"headline" firestore:"headline"`
	Summary          string           `json:"summary,omitempty" firestore:"summary,omitempty"`
	RiskPosture      string           `json:"riskPosture,omitempty" firestore:"riskPosture,omitempty"`
	PriorityPoints   []map[string]any `json:"priorityPoints,omitempty" firestore:"priorityPoints,omitempty"`
	AffectedSubjects []string         `json:"affectedSubjects,omitempty" firestore:"affectedSubjects,omitempty"`
	CoverageGaps     []string         `json:"coverageGaps,omitempty" firestore:"coverageGaps,omitempty"`
	SignalCount      int              `json:"signalCount" firestore:"signalCount"`
	SourceCount      int              `json:"sourceCount" firestore:"sourceCount"`
	SubjectCount     int              `json:"subjectCount" firestore:"subjectCount"`
	Metadata         map[string]any   `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt        time.Time        `json:"createdAt" firestore:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt" firestore:"updatedAt"`
}

func (d *MonitoringDigest) Validate() error {
	if d == nil {
		return fmt.Errorf("digest is required")
	}
	if strings.TrimSpace(d.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	switch strings.TrimSpace(d.ScopeType) {
	case string(MonitoringDigestScopeLens), string(MonitoringDigestScopePortfolio):
	default:
		return fmt.Errorf("invalid scopeType")
	}
	if strings.TrimSpace(d.ScopeID) == "" {
		return fmt.Errorf("scopeId is required")
	}
	if strings.TrimSpace(d.DateKey) == "" {
		return fmt.Errorf("dateKey is required")
	}
	if strings.TrimSpace(d.Headline) == "" {
		return fmt.Errorf("headline is required")
	}
	return nil
}
