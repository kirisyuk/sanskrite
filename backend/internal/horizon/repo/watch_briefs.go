package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchBriefsRepository struct {
	col *firestore.CollectionRef
}

func NewWatchBriefsRepository(db *firestore.Client) *WatchBriefsRepository {
	return &WatchBriefsRepository{col: db.Collection("horizon_watch_briefs")}
}

func (r *WatchBriefsRepository) Upsert(ctx context.Context, brief *models.WatchBrief) error {
	if r == nil || r.col == nil || brief == nil {
		return ErrInvalidDocument
	}
	if err := brief.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if brief.CreatedAt.IsZero() {
		brief.CreatedAt = now
	}
	brief.UpdatedAt = now
	if strings.TrimSpace(brief.ID) == "" {
		brief.ID = strings.TrimSpace(brief.TargetID)
	}
	_, err := r.col.Doc(strings.TrimSpace(brief.ID)).Set(ctx, brief)
	return err
}

func (r *WatchBriefsRepository) Get(ctx context.Context, id string) (*models.WatchBrief, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var brief models.WatchBrief
	if err := snap.DataTo(&brief); err != nil {
		return nil, err
	}
	if brief.ID == "" {
		brief.ID = snap.Ref.ID
	}
	return &brief, nil
}

func (r *WatchBriefsRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.WatchBrief, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchBrief, 0, len(docs))
	for _, doc := range docs {
		var brief models.WatchBrief
		if err := doc.DataTo(&brief); err != nil {
			continue
		}
		if brief.ID == "" {
			brief.ID = doc.Ref.ID
		}
		out = append(out, &brief)
	}
	return out, nil
}
