package models

import "time"

// UnitGraphSnapshot stores a lens-scoped L1 unit graph projection.
// It is intentionally compact: one document per (tenant,lens,domainPath).
type UnitGraphSnapshot struct {
	ID                  string          `json:"id" firestore:"id"`
	TenantID            string          `json:"tenantId" firestore:"tenantId"`
	ProjectID           string          `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID              string          `json:"lensId" firestore:"lensId"`
	DomainCanonicalPath string          `json:"domainCanonicalPath,omitempty" firestore:"domainCanonicalPath,omitempty"`
	Edges               []UnitGraphEdge `json:"edges" firestore:"edges"`
	SourceJobID         string          `json:"sourceJobId,omitempty" firestore:"sourceJobId,omitempty"`
	CreatedAt           time.Time       `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt" firestore:"updatedAt"`
}

// UnitDiveResultArtifact stores a lens-scoped result of a unit-level dive execution (P5).
// RequestID remains inside Result and is duplicated as a query-friendly top-level field.
type UnitDiveResultArtifact struct {
	ID                  string         `json:"id" firestore:"id"`
	TenantID            string         `json:"tenantId" firestore:"tenantId"`
	ProjectID           string         `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID              string         `json:"lensId" firestore:"lensId"`
	DomainCanonicalPath string         `json:"domainCanonicalPath,omitempty" firestore:"domainCanonicalPath,omitempty"`
	RequestID           string         `json:"requestId" firestore:"requestId"`
	Result              UnitDiveResult `json:"result" firestore:"result"`
	SourceJobID         string         `json:"sourceJobId,omitempty" firestore:"sourceJobId,omitempty"`
	CreatedAt           time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt" firestore:"updatedAt"`
}
