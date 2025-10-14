package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type SearchEventsRepository struct{ col *firestore.CollectionRef }

func NewSearchEventsRepository(db *firestore.Client) *SearchEventsRepository {
	return &SearchEventsRepository{col: db.Collection("rag_search_events")}
}

func (r *SearchEventsRepository) Create(ctx context.Context, e *models.SearchEvent) (string, error) {
	if e == nil || e.TenantID == "" || e.ProjectID == "" {
		return "", ErrInvalidDocument
	}
	e.CreatedAt = time.Now().UTC()
	doc := r.col.NewDoc()
	e.ID = doc.ID
	if _, err := doc.Set(ctx, e); err != nil {
		return "", err
	}
	return doc.ID, nil
}

func (r *SearchEventsRepository) ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]*models.SearchEvent, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.SearchEvent, 0, len(docs))
	for _, d := range docs {
		var ev models.SearchEvent
		if err := d.DataTo(&ev); err != nil {
			continue
		}
		res = append(res, &ev)
	}
	return res, nil
}
