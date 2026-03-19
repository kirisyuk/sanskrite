package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchPageSnapshotsRepository struct {
	col *firestore.CollectionRef
}

func NewWatchPageSnapshotsRepository(db *firestore.Client) *WatchPageSnapshotsRepository {
	return &WatchPageSnapshotsRepository{col: db.Collection("horizon_watch_page_snapshots")}
}

func watchPageSnapshotDocID(tenantID, lensID string) string {
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	if tenantID == "" || lensID == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s", tenantID, lensID)
}

func (r *WatchPageSnapshotsRepository) Upsert(ctx context.Context, snapshot *models.WatchPageSnapshot) error {
	if r == nil || r.col == nil || snapshot == nil {
		return ErrInvalidDocument
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	if strings.TrimSpace(snapshot.ID) == "" {
		snapshot.ID = watchPageSnapshotDocID(snapshot.TenantID, snapshot.LensID)
	}
	_, err := r.col.Doc(strings.TrimSpace(snapshot.ID)).Set(ctx, snapshot)
	return err
}

func (r *WatchPageSnapshotsRepository) GetByLens(ctx context.Context, tenantID, lensID string) (*models.WatchPageSnapshot, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(lensID) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(watchPageSnapshotDocID(tenantID, lensID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var snapshot models.WatchPageSnapshot
	if err := snap.DataTo(&snapshot); err != nil {
		return nil, err
	}
	if strings.TrimSpace(snapshot.ID) == "" {
		snapshot.ID = snap.Ref.ID
	}
	return &snapshot, nil
}
