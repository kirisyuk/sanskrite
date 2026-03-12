package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchFeedEntriesRepository struct {
	col *firestore.CollectionRef
}

func NewWatchFeedEntriesRepository(db *firestore.Client) *WatchFeedEntriesRepository {
	return &WatchFeedEntriesRepository{col: db.Collection("horizon_watch_feed_entries")}
}

func (r *WatchFeedEntriesRepository) Create(ctx context.Context, entry *models.WatchFeedEntry) (string, error) {
	if r == nil || r.col == nil || entry == nil {
		return "", ErrInvalidDocument
	}
	if err := entry.Validate(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	doc := r.col.NewDoc()
	entry.ID = doc.ID
	_, err := doc.Set(ctx, entry)
	return entry.ID, err
}

func (r *WatchFeedEntriesRepository) ListByTarget(ctx context.Context, tenantID, targetID string, limit int) ([]*models.WatchFeedEntry, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(targetID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("targetId", "==", strings.TrimSpace(targetID)).OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchFeedEntry, 0, len(docs))
	for _, doc := range docs {
		var entry models.WatchFeedEntry
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		if entry.ID == "" {
			entry.ID = doc.Ref.ID
		}
		out = append(out, &entry)
	}
	return out, nil
}
