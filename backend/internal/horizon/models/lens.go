package models

import (
	"fmt"
	"time"
)

// Lens представляет пользовательскую конфигурацию мониторинга и анализа
type Lens struct {
	ID        string `json:"id" firestore:"id"`
	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId" firestore:"projectId"`
	Name      string `json:"name" firestore:"name"`

	// Scope: Mixed array of canonical paths AND query addresses
	// Examples:
	//   - Static: "/org/tsmc", "/fac/tsmc/fab-21"
	//   - Dynamic (QueryAddress): "/query/sector:/Technology/Semiconductors/**"
	Scope []string `json:"scope" firestore:"scope"`

	ViewConfig  map[string]interface{} `json:"viewConfig" firestore:"viewConfig"`
	AlertConfig map[string]interface{} `json:"alertConfig" firestore:"alertConfig"`
	Status      string                 `json:"status" firestore:"status"` // ACTIVE, ARCHIVED
	CreatedAt   time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt" firestore:"updatedAt"`

	// ОПТИМИЗАЦИЯ: Кэш для ускорения операций с Lens (Update/Delete).
	// Хранит mapping canonicalPath → fragmentID, чтобы избежать дорогих
	// composite index queries (GetByCanonicalPath) в пользу прямых key lookups (Get).
	// Обновляется автоматически при Create/Update Lens.
	ScopeFragments map[string]string `json:"scopeFragments,omitempty" firestore:"scopeFragments,omitempty"`

	// QUERY ADDRESS SUPPORT: Кэш разрешенных QueryAddress в canonicalPaths.
	// Key = QueryAddress (e.g., "/query/sector:/Technology/Semiconductors/**")
	// Value = []canonicalPath (resolved list)
	// Обновляется фоновым job (раз в день) или при Create/Update Lens.
	ResolvedQueries map[string][]string `json:"resolvedQueries,omitempty" firestore:"resolvedQueries,omitempty"`

	// Timestamp последнего разрешения QueryAddress
	LastResolved *time.Time `json:"lastResolved,omitempty" firestore:"lastResolved,omitempty"`

	// ExposurePaths: canonical exposure path keys associated with this lens.
	// Stored as strings for fast listing/filtering and to keep lens lightweight.
	// Full details live in horizon_exposure_paths artifacts collection.
	//
	// Example:
	//   /exposure/SUPPLY_CHAIN_RISK/objective/fragment/<fragmentId>/path/<pathId>
	ExposurePaths []string `json:"exposurePaths,omitempty" firestore:"exposurePaths,omitempty"`
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
	if l.Status != string(LensStatusActive) && l.Status != string(LensStatusArchived) {
		return fmt.Errorf("status must be ACTIVE or ARCHIVED")
	}
	return nil
}
