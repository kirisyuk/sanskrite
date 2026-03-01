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

// docIDWithRelationHash produces a doc ID. When relationHash is non-empty, it distinguishes
// (targetPath, relationType) for relation-driven child jobs.
func (r *CascadeVisitedRepository) docIDWithRelationHash(tenantID, cascadeID, canonicalPath, relationHash string) string {
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	relationHash = strings.TrimSpace(relationHash)
	key := canonicalPath
	if relationHash != "" {
		key = canonicalPath + "|" + relationHash
	}
	sum := sha1.Sum([]byte(key))
	h := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s_%s_%s", tenantID, cascadeID, h)
}

// TryMarkVisitedWithRelation is like TryMarkVisited but includes relationHash for relation-driven children.
// When relationHash is non-empty, the key distinguishes (targetPath, relationType) to avoid
// duplicate jobs for different relations to the same target.
func (r *CascadeVisitedRepository) TryMarkVisitedWithRelation(
	ctx context.Context,
	tenantID string,
	projectID string,
	cascadeID string,
	canonicalPath string,
	relationHash string,
	jobID string,
	depth int,
) (alreadyVisited bool, err error) {
	if r == nil || r.col == nil {
		return false, nil
	}
	id := r.docIDWithRelationHash(tenantID, cascadeID, canonicalPath, relationHash)
	return r.tryMarkVisitedByID(ctx, tenantID, projectID, cascadeID, canonicalPath, id, jobID, depth)
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
	return r.TryMarkVisitedWithRelation(ctx, tenantID, projectID, cascadeID, canonicalPath, "", jobID, depth)
}

func (r *CascadeVisitedRepository) tryMarkVisitedByID(
	ctx context.Context,
	tenantID string,
	projectID string,
	cascadeID string,
	canonicalPath string,
	docID string,
	jobID string,
	depth int,
) (alreadyVisited bool, err error) {
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	jobID = strings.TrimSpace(jobID)
	if tenantID == "" || cascadeID == "" || canonicalPath == "" {
		return false, nil
	}

	now := time.Now().UTC()
	doc := r.col.Doc(docID)

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
	return r.DeleteVisitedWithRelation(ctx, tenantID, cascadeID, canonicalPath, "")
}

// DeleteVisitedWithRelation removes the marker, including relationHash when provided.
func (r *CascadeVisitedRepository) DeleteVisitedWithRelation(ctx context.Context, tenantID, cascadeID, canonicalPath, relationHash string) error {
	if r == nil || r.col == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	if tenantID == "" || cascadeID == "" || canonicalPath == "" {
		return nil
	}
	id := r.docIDWithRelationHash(tenantID, cascadeID, canonicalPath, relationHash)
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}



