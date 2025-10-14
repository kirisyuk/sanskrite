package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type DocumentsRepository struct{ col *firestore.CollectionRef }

func NewDocumentsRepository(db *firestore.Client) *DocumentsRepository {
	return &DocumentsRepository{col: db.Collection("rag_documents")}
}

func (r *DocumentsRepository) Upsert(ctx context.Context, d *models.Document) error {
	if d == nil || d.ID == "" || d.SourceID == "" || d.TenantID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.IngestedAt.IsZero() {
		d.IngestedAt = now
	}
	d.UpdatedAt = now
	_, err := r.col.Doc(d.ID).Set(ctx, d)
	return err
}

func (r *DocumentsRepository) Create(ctx context.Context, d *models.Document) (string, error) {
	if d == nil || d.SourceID == "" || d.TenantID == "" {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.IngestedAt = now
	d.UpdatedAt = now
	doc := r.col.NewDoc()
	d.ID = doc.ID
	if _, err := doc.Set(ctx, d); err != nil {
		return "", err
	}
	return doc.ID, nil
}

func (r *DocumentsRepository) GetByID(ctx context.Context, id string) (*models.Document, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var d models.Document
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DocumentsRepository) GetByExternalID(ctx context.Context, tenantID, sourceID, externalID string) (*models.Document, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).Where("externalId", "==", externalID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var d models.Document
	if err := docs[0].DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DocumentsRepository) ListBySource(ctx context.Context, tenantID, sourceID string, limit int) ([]*models.Document, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).OrderBy("originPublishedAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.Document, 0, len(docs))
	for _, d := range docs {
		var doc models.Document
		if err := d.DataTo(&doc); err != nil {
			continue
		}
		res = append(res, &doc)
	}
	return res, nil
}

// ListBySources returns latest documents for a set of source IDs with optional filters
func (r *DocumentsRepository) ListBySources(ctx context.Context, tenantID string, sourceIDs []string, limit int, language, documentType *string) ([]*models.Document, error) {
	if len(sourceIDs) == 0 {
		return []*models.Document{}, nil
	}
	// Firestore IN supports up to 10 values; batch if needed
	batchSize := 10
	var results []*models.Document
	for i := 0; i < len(sourceIDs); i += batchSize {
		end := i + batchSize
		if end > len(sourceIDs) {
			end = len(sourceIDs)
		}
		batch := sourceIDs[i:end]
		q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "in", batch).OrderBy("originPublishedAt", firestore.Desc)
		if limit > 0 {
			q = q.Limit(limit)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			var doc models.Document
			if err := d.DataTo(&doc); err != nil {
				continue
			}
			if language != nil && *language != "" && doc.Language != *language {
				continue
			}
			if documentType != nil && *documentType != "" && doc.DocumentType != *documentType {
				continue
			}
			results = append(results, &doc)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
