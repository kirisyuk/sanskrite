package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchDigestsRepository struct {
	col *firestore.CollectionRef
}

func NewWatchDigestsRepository(db *firestore.Client) *WatchDigestsRepository {
	return &WatchDigestsRepository{col: db.Collection("horizon_monitoring_digests")}
}

func monitoringDigestDocID(digest *models.MonitoringDigest) string {
	if digest == nil {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s_%s",
		strings.TrimSpace(digest.TenantID),
		strings.TrimSpace(digest.ScopeType),
		strings.TrimSpace(digest.ScopeID),
		strings.TrimSpace(digest.DateKey),
	)
}

func (r *WatchDigestsRepository) Upsert(ctx context.Context, digest *models.MonitoringDigest) error {
	if r == nil || r.col == nil || digest == nil {
		return ErrInvalidDocument
	}
	if err := digest.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if digest.CreatedAt.IsZero() {
		digest.CreatedAt = now
	}
	digest.UpdatedAt = now
	if strings.TrimSpace(digest.ID) == "" {
		digest.ID = monitoringDigestDocID(digest)
	}
	_, err := r.col.Doc(strings.TrimSpace(digest.ID)).Set(ctx, digest)
	return err
}

func (r *WatchDigestsRepository) Get(ctx context.Context, id string) (*models.MonitoringDigest, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var digest models.MonitoringDigest
	if err := snap.DataTo(&digest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(digest.ID) == "" {
		digest.ID = snap.Ref.ID
	}
	return &digest, nil
}

func (r *WatchDigestsRepository) GetByScopeAndDate(ctx context.Context, tenantID, scopeType, scopeID, dateKey string) (*models.MonitoringDigest, error) {
	digest := &models.MonitoringDigest{
		TenantID:  strings.TrimSpace(tenantID),
		ScopeType: strings.TrimSpace(scopeType),
		ScopeID:   strings.TrimSpace(scopeID),
		DateKey:   strings.TrimSpace(dateKey),
	}
	return r.Get(ctx, monitoringDigestDocID(digest))
}

func (r *WatchDigestsRepository) ListRecentByScope(ctx context.Context, tenantID, scopeType, scopeID string, limit int) ([]*models.MonitoringDigest, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(scopeType) == "" || strings.TrimSpace(scopeID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 30
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("scopeType", "==", strings.TrimSpace(scopeType)).
		Where("scopeId", "==", strings.TrimSpace(scopeID)).
		OrderBy("windowEnd", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.MonitoringDigest, 0, len(docs))
	for _, doc := range docs {
		var digest models.MonitoringDigest
		if err := doc.DataTo(&digest); err != nil {
			continue
		}
		if strings.TrimSpace(digest.ID) == "" {
			digest.ID = doc.Ref.ID
		}
		out = append(out, &digest)
	}
	return out, nil
}
