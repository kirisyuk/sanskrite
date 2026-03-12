package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchPacksRepository struct {
	col *firestore.CollectionRef
}

func NewWatchPacksRepository(db *firestore.Client) *WatchPacksRepository {
	return &WatchPacksRepository{col: db.Collection("horizon_watch_packs")}
}

func (r *WatchPacksRepository) Upsert(ctx context.Context, pack *models.WatchPack) error {
	if r == nil || r.col == nil || pack == nil {
		return ErrInvalidDocument
	}
	if err := pack.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if pack.CreatedAt.IsZero() {
		pack.CreatedAt = now
	}
	pack.UpdatedAt = now
	id := strings.TrimSpace(pack.ID)
	if id == "" {
		id = strings.TrimSpace(pack.TenantID) + "_" + strings.ReplaceAll(strings.TrimSpace(pack.Name), " ", "_")
		pack.ID = id
	}
	_, err := r.col.Doc(id).Set(ctx, pack)
	return err
}

func (r *WatchPacksRepository) Get(ctx context.Context, id string) (*models.WatchPack, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var pack models.WatchPack
	if err := snap.DataTo(&pack); err != nil {
		return nil, err
	}
	if pack.ID == "" {
		pack.ID = snap.Ref.ID
	}
	return &pack, nil
}

func (r *WatchPacksRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.WatchPack, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list watch packs: %w", err)
	}
	out := make([]*models.WatchPack, 0, len(docs))
	for _, doc := range docs {
		var pack models.WatchPack
		if err := doc.DataTo(&pack); err != nil {
			continue
		}
		if pack.ID == "" {
			pack.ID = doc.Ref.ID
		}
		out = append(out, &pack)
	}
	return out, nil
}
