package models

import (
	"fmt"
	"strings"
	"time"
)

// Manual resolve queue: human-in-the-loop items (geo enrichment, future kinds).
// Collection: horizon_manual_resolve_queue

type ManualResolveQueueStatus string

const (
	ManualResolveQueueStatusOpen       ManualResolveQueueStatus = "OPEN"
	ManualResolveQueueStatusInProgress ManualResolveQueueStatus = "IN_PROGRESS"
	ManualResolveQueueStatusResolved   ManualResolveQueueStatus = "RESOLVED"
	ManualResolveQueueStatusDismissed  ManualResolveQueueStatus = "DISMISSED"
)

// EnrichmentKind values are extensible (geo is one signal among many).
const (
	EnrichmentKindGeoCoordinates = "geo_coordinates"
)

type ManualResolveQueueItem struct {
	ID        string `json:"id" firestore:"id"`
	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID    string `json:"lensId,omitempty" firestore:"lensId,omitempty"`

	FragmentID      string       `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	CanonicalPath   string       `json:"canonicalPath" firestore:"canonicalPath"`
	FragmentType    FragmentType `json:"fragmentType,omitempty" firestore:"fragmentType,omitempty"`
	EnrichmentKinds []string     `json:"enrichmentKinds" firestore:"enrichmentKinds"`

	Status   ManualResolveQueueStatus `json:"status" firestore:"status"`
	Priority int                      `json:"priority" firestore:"priority"`
	Source   string                   `json:"source,omitempty" firestore:"source,omitempty"`

	// WorkScopeDigest optional link to the scope pass that surfaced this item.
	WorkScopeDigest string `json:"workScopeDigest,omitempty" firestore:"workScopeDigest,omitempty"`

	ResolvedAt *time.Time `json:"resolvedAt,omitempty" firestore:"resolvedAt,omitempty"`
	ResolvedBy string     `json:"resolvedBy,omitempty" firestore:"resolvedBy,omitempty"`
	Notes      string     `json:"notes,omitempty" firestore:"notes,omitempty"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

func (it *ManualResolveQueueItem) Validate() error {
	if it == nil {
		return fmt.Errorf("manual resolve item is nil")
	}
	if strings.TrimSpace(it.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(it.CanonicalPath) == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	if len(it.EnrichmentKinds) == 0 {
		return fmt.Errorf("enrichmentKinds is required")
	}
	switch it.Status {
	case ManualResolveQueueStatusOpen, ManualResolveQueueStatusInProgress, ManualResolveQueueStatusResolved, ManualResolveQueueStatusDismissed, "":
	default:
		return fmt.Errorf("invalid status")
	}
	if it.Status == "" {
		it.Status = ManualResolveQueueStatusOpen
	}
	if it.Priority == 0 {
		it.Priority = 50
	}
	return nil
}
