package models

import (
	"fmt"
	"strings"
	"time"
)

// RiskAppetiteProfile defines external risk tolerance for a canonical scope.
// Scope examples:
// - /market/{geo}/{domain}/{vertical}
// - /loc/{country}/{admin1}/{city}
type RiskAppetiteProfile struct {
	ID                 string             `json:"id" firestore:"id"`
	TenantID           string             `json:"tenantId" firestore:"tenantId"`
	ScopePath          string             `json:"scopePath" firestore:"scopePath"`
	Polarity           string             `json:"polarity" firestore:"polarity"` // "risk" | "opportunity"
	AggregateThreshold float64            `json:"aggregateThreshold" firestore:"aggregateThreshold"`
	AxisThresholds     map[string]float64 `json:"axisThresholds,omitempty" firestore:"axisThresholds,omitempty"`
	NeighborScopes     []string           `json:"neighborScopes,omitempty" firestore:"neighborScopes,omitempty"`
	Source             string             `json:"source,omitempty" firestore:"source,omitempty"` // "auto_default" | "analyst"
	CreatedAt          time.Time          `json:"createdAt" firestore:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt" firestore:"updatedAt"`
}

func (p *RiskAppetiteProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("profile is nil")
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.ScopePath) == "" {
		return fmt.Errorf("scopePath is required")
	}
	if p.Polarity != "risk" && p.Polarity != "opportunity" {
		return fmt.Errorf("polarity must be risk or opportunity")
	}
	if p.AggregateThreshold < 0 || p.AggregateThreshold > 1 {
		return fmt.Errorf("aggregateThreshold must be in [0,1]")
	}
	for axis, v := range p.AxisThresholds {
		if !IsValidAxis(strings.TrimSpace(axis)) {
			return fmt.Errorf("invalid axis: %s", axis)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("axisThresholds[%s] must be in [0,1]", axis)
		}
	}
	return nil
}

func DefaultRiskAppetiteProfile(tenantID, scopePath, polarity string) *RiskAppetiteProfile {
	now := time.Now().UTC()
	axis := make(map[string]float64, len(AllAxes()))
	for _, a := range AllAxes() {
		axis[a] = 0.65
	}
	return &RiskAppetiteProfile{
		TenantID:           strings.TrimSpace(tenantID),
		ScopePath:          strings.TrimSpace(scopePath),
		Polarity:           strings.TrimSpace(polarity),
		AggregateThreshold: 0.70,
		AxisThresholds:     axis,
		Source:             "auto_default",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

