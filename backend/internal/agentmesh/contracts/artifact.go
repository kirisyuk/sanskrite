package contracts

import "time"

type ArtifactRef struct {
	ID          string            `json:"id" firestore:"id"`
	TenantID    string            `json:"tenantId" firestore:"tenantId"`
	TaskID      string            `json:"taskId" firestore:"taskId"`
	Kind        string            `json:"kind" firestore:"kind"`
	Bucket      string            `json:"bucket" firestore:"bucket"`
	Object      string            `json:"object" firestore:"object"`
	ContentType string            `json:"contentType,omitempty" firestore:"contentType,omitempty"`
	Hash        string            `json:"hash,omitempty" firestore:"hash,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"createdAt" firestore:"createdAt"`
}

type EvidenceBundle struct {
	Primary *ArtifactRef  `json:"primary,omitempty" firestore:"primary,omitempty"`
	Related []ArtifactRef `json:"related,omitempty" firestore:"related,omitempty"`
}

type CheckpointRef struct {
	ArtifactRef
	Stage string `json:"stage,omitempty" firestore:"stage,omitempty"`
}
