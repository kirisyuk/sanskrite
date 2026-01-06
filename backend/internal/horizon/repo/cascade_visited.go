package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CascadeVisit records that a canonicalPath was already scheduled/visited within a cascade.
// This is used as a guardrail against cycles / avalanche fanout in recursive scouting.
type CascadeVisit struct {
	TenantID       string    `firestore:"tenantId"`
	ProjectID      string    `firestore:"projectId,omitempty"`
	CascadeID      string    `firestore:"cascadeId"`
	CanonicalPath  string    `firestore:"canonicalPath"`
	FirstSeenAt    time.Time `firestore:"firstSeenAt"`
	LastSeenAt     time.Time `firestore:"lastSeenAt"`
	FirstJobID     string    `firestore:"firstJobId"`
	LastJobID      string    `firestore:"lastJobId"`
	FirstDepth     int       `firestore:"firstDepth"`
	LastDepth      int       `firestore:"lastDepth"`
	ScheduledCount int       `firestore:"scheduledCount"`
}

// CascadeVisitedRepository stores cascade-level visited set entries.
// Collection: horizon_cascade_visits
// Document ID is derived from (tenantId, cascadeId, canonicalPath hash) to avoid slash issues.
type CascadeVisitedRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

func NewCascadeVisitedRepository(db *firestore.Client) *CascadeVisitedRepository {
	if db == nil {
		return &CascadeVisitedRepository{}
	}
	return &CascadeVisitedRepository{
		col: db.Collection("horizon_cascade_visits"),
		db:  db,
	}
}

func (r *CascadeVisitedRepository) docID(tenantID, cascadeID, canonicalPath string) string {
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	sum := sha1.Sum([]byte(canonicalPath))
	// 8 bytes (16 hex chars) is enough for low collision risk at our scale.
	h := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s_%s_%s", tenantID, cascadeID, h)
}

// TryMarkVisited tries to create a visited marker for (cascadeId, canonicalPath).
// Returns alreadyVisited=true if marker existed.
//
// IMPORTANT: Use this as a scheduling guardrail. If scheduling later fails, caller can
// DeleteVisited to avoid blocking quality.
func (r *CascadeVisitedRepository) TryMarkVisited(
	ctx context.Context,
	tenantID string,
	projectID string,
	cascadeID string,
	canonicalPath string,
	jobID string,
	depth int,
) (alreadyVisited bool, err error) {
	if r == nil || r.col == nil {
		return false, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	jobID = strings.TrimSpace(jobID)
	if tenantID == "" || cascadeID == "" || canonicalPath == "" {
		return false, nil
	}

	now := time.Now().UTC()
	id := r.docID(tenantID, cascadeID, canonicalPath)
	doc := r.col.Doc(id)

	visit := CascadeVisit{
		TenantID:       tenantID,
		ProjectID:      projectID,
		CascadeID:      cascadeID,
		CanonicalPath:  canonicalPath,
		FirstSeenAt:    now,
		LastSeenAt:     now,
		FirstJobID:     jobID,
		LastJobID:      jobID,
		FirstDepth:     depth,
		LastDepth:      depth,
		ScheduledCount: 1,
	}

	_, err = doc.Create(ctx, visit)
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			// Best-effort bump lastSeenAt + count for observability.
			_, _ = doc.Set(ctx, map[string]interface{}{
				"lastSeenAt":     now,
				"lastJobId":      jobID,
				"lastDepth":      depth,
				"scheduledCount": firestore.Increment(1),
			}, firestore.MergeAll)
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// DeleteVisited removes the marker for (cascadeId, canonicalPath). Best-effort.
func (r *CascadeVisitedRepository) DeleteVisited(ctx context.Context, tenantID, cascadeID, canonicalPath string) error {
	if r == nil || r.col == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	if tenantID == "" || cascadeID == "" || canonicalPath == "" {
		return nil
	}
	id := r.docID(tenantID, cascadeID, canonicalPath)
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}


