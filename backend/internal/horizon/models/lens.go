package models

import (
	"fmt"
	"time"
)

// Lens представляет пользовательскую конфигурацию мониторинга и анализа
type Lens struct {
	ID          string                 `json:"id" firestore:"id"`
	TenantID    string                 `json:"tenantId" firestore:"tenantId"`
	ProjectID   string                 `json:"projectId" firestore:"projectId"`
	Name        string                 `json:"name" firestore:"name"`
	Scope       []string               `json:"scope" firestore:"scope"` // Массив canonicalPath
	ViewConfig  map[string]interface{} `json:"viewConfig" firestore:"viewConfig"`
	AlertConfig map[string]interface{} `json:"alertConfig" firestore:"alertConfig"`
	Status      string                 `json:"status" firestore:"status"` // ACTIVE, ARCHIVED
	CreatedAt   time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

// LensStatus статус Lens
type LensStatus string

const (
	LensStatusActive   LensStatus = "ACTIVE"
	LensStatusArchived LensStatus = "ARCHIVED"
)

// Validate проверяет валидность Lens
func (l *Lens) Validate() error {
	if l.ProjectID == "" {
		return fmt.Errorf("projectId is required")
	}
	if l.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if l.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(l.Scope) == 0 {
		return fmt.Errorf("scope must contain at least one canonicalPath")
	}
	if l.Status != string(LensStatusActive) && l.Status != string(LensStatusArchived) {
		return fmt.Errorf("status must be ACTIVE or ARCHIVED")
	}
	return nil
}

