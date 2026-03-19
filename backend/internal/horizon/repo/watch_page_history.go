package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchPageHistoryRepository struct {
	col *firestore.CollectionRef
}

func NewWatchPageHistoryRepository(db *firestore.Client) *WatchPageHistoryRepository {
	return &WatchPageHistoryRepository{col: db.Collection("horizon_watch_page_history")}
}

func (r *WatchPageHistoryRepository) Create(ctx context.Context, entry *models.WatchPageHistoryEntry) (string, error) {
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
	if _, err := doc.Set(ctx, entry); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (r *WatchPageHistoryRepository) ListRecentByLens(ctx context.Context, tenantID, lensID, entryType string, limit int) ([]*models.WatchPageHistoryEntry, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(lensID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("lensId", "==", strings.TrimSpace(lensID))
	if strings.TrimSpace(entryType) != "" {
		q = q.Where("entryType", "==", strings.TrimSpace(entryType))
	}
	q = q.OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchPageHistoryEntry, 0, len(docs))
	for _, doc := range docs {
		var entry models.WatchPageHistoryEntry
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		if strings.TrimSpace(entry.ID) == "" {
			entry.ID = doc.Ref.ID
		}
		out = append(out, &entry)
	}
	return out, nil
}
