package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchTargetsRepository struct {
	col *firestore.CollectionRef
}

func NewWatchTargetsRepository(db *firestore.Client) *WatchTargetsRepository {
	return &WatchTargetsRepository{col: db.Collection("horizon_watch_targets")}
}

func watchTargetDocID(target *models.WatchTarget) string {
	if target == nil {
		return ""
	}
	tenantID := strings.TrimSpace(target.TenantID)
	packID := strings.TrimSpace(target.PackID)
	userID := strings.TrimSpace(target.UserID)
	kind := strings.TrimSpace(target.Kind)
	canonicalPath := strings.ReplaceAll(strings.Trim(strings.TrimSpace(target.CanonicalPath), "/"), "/", "_")
	switch {
	case packID != "":
		return fmt.Sprintf("%s_%s_%s_%s", tenantID, packID, kind, canonicalPath)
	case userID != "":
		return fmt.Sprintf("%s_%s_%s_%s", tenantID, userID, kind, canonicalPath)
	default:
		return fmt.Sprintf("%s_%s_%s", tenantID, kind, canonicalPath)
	}
}

func (r *WatchTargetsRepository) Upsert(ctx context.Context, target *models.WatchTarget) error {
	if r == nil || r.col == nil || target == nil {
		return ErrInvalidDocument
	}
	if err := target.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if target.CreatedAt.IsZero() {
		target.CreatedAt = now
	}
	target.UpdatedAt = now
	if strings.TrimSpace(target.ID) == "" {
		target.ID = watchTargetDocID(target)
	}
	_, err := r.col.Doc(strings.TrimSpace(target.ID)).Set(ctx, target)
	return err
}

func (r *WatchTargetsRepository) Get(ctx context.Context, id string) (*models.WatchTarget, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var target models.WatchTarget
	if err := snap.DataTo(&target); err != nil {
		return nil, err
	}
	if target.ID == "" {
		target.ID = snap.Ref.ID
	}
	return &target, nil
}

func (r *WatchTargetsRepository) Delete(ctx context.Context, id string) error {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(strings.TrimSpace(id)).Update(ctx, []firestore.Update{
		{Path: "status", Value: string(models.WatchTargetStatusDeleted)},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}

func (r *WatchTargetsRepository) ListByTenant(ctx context.Context, tenantID string, includeDeleted bool, limit int) ([]*models.WatchTarget, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	q := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchTarget, 0, len(docs))
	for _, doc := range docs {
		var target models.WatchTarget
		if err := doc.DataTo(&target); err != nil {
			continue
		}
		if !includeDeleted && strings.TrimSpace(target.Status) == string(models.WatchTargetStatusDeleted) {
			continue
		}
		if target.ID == "" {
			target.ID = doc.Ref.ID
		}
		out = append(out, &target)
	}
	return out, nil
}

func (r *WatchTargetsRepository) ListByPack(ctx context.Context, tenantID, packID string) ([]*models.WatchTarget, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(packID) == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("packId", "==", strings.TrimSpace(packID)).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchTarget, 0, len(docs))
	for _, doc := range docs {
		var target models.WatchTarget
		if err := doc.DataTo(&target); err != nil {
			continue
		}
		if target.ID == "" {
			target.ID = doc.Ref.ID
		}
		out = append(out, &target)
	}
	return out, nil
}

func (r *WatchTargetsRepository) GetByCanonicalPath(ctx context.Context, tenantID, userID, packID, kind, canonicalPath string) (*models.WatchTarget, error) {
	target := &models.WatchTarget{
		TenantID:      tenantID,
		UserID:        userID,
		PackID:        packID,
		Kind:          kind,
		CanonicalPath: canonicalPath,
	}
	return r.Get(ctx, watchTargetDocID(target))
}
