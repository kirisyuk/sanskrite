package policy

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// FirestoreTelemetryWriter writes policy telemetry to Firestore.
type FirestoreTelemetryWriter struct {
	col *firestore.CollectionRef
}

// NewFirestoreTelemetryWriter creates a new FirestoreTelemetryWriter.
func NewFirestoreTelemetryWriter(client *firestore.Client) *FirestoreTelemetryWriter {
	return &FirestoreTelemetryWriter{
		col: client.Collection("horizon_policy_telemetry"),
	}
}

// PolicyTelemetryRecord represents a single telemetry record.
type PolicyTelemetryRecord struct {
	PolicyName   string                 `json:"policyName" firestore:"policyName"`
	Decision     PolicyDecision         `json:"decision" firestore:"decision"`
	Timestamp    time.Time              `json:"timestamp" firestore:"timestamp"`
	TenantID     string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	ProjectID    string                 `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID       string                 `json:"lensId,omitempty" firestore:"lensId,omitempty"`
}

// Record records a policy decision.
func (w *FirestoreTelemetryWriter) Record(decision PolicyDecision, policyName string) error {
	ctx := context.Background()
	record := PolicyTelemetryRecord{
		PolicyName: policyName,
		Decision:   decision,
		Timestamp:  time.Now().UTC(),
	}
	_, err := w.col.NewDoc().Set(ctx, record)
	return err
}

// RecordWithContext records a policy decision with additional context.
func (w *FirestoreTelemetryWriter) RecordWithContext(ctx context.Context, decision PolicyDecision, policyName, tenantID, projectID, lensID string) error {
	record := PolicyTelemetryRecord{
		PolicyName: policyName,
		Decision:   decision,
		Timestamp:  time.Now().UTC(),
		TenantID:   tenantID,
		ProjectID:  projectID,
		LensID:     lensID,
	}
	_, err := w.col.NewDoc().Set(ctx, record)
	return err
}

