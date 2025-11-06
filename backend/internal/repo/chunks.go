package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type ChunksRepository struct{ col *firestore.CollectionRef }

func NewChunksRepository(db *firestore.Client) *ChunksRepository {
	return &ChunksRepository{col: db.Collection("rag_chunks")}
}

func (r *ChunksRepository) Upsert(ctx context.Context, c *models.Chunk) error {
	if c == nil || c.ID == "" || c.DocID == "" || c.SourceID == "" || c.TenantID == "" {
		return ErrInvalidDocument
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := r.col.Doc(c.ID).Set(ctx, c)
	return err
}

func (r *ChunksRepository) Create(ctx context.Context, c *models.Chunk) (string, error) {
	if c == nil || c.DocID == "" || c.SourceID == "" || c.TenantID == "" {
		return "", ErrInvalidDocument
	}
	c.CreatedAt = time.Now().UTC()
	doc := r.col.NewDoc()
	c.ID = doc.ID
	if _, err := doc.Set(ctx, c); err != nil {
		return "", err
	}
	return doc.ID, nil
}

func (r *ChunksRepository) ListByDoc(ctx context.Context, tenantID, docID string) ([]*models.Chunk, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("docId", "==", docID).OrderBy("position", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.Chunk, 0, len(docs))
	for _, d := range docs {
		var c models.Chunk
		if err := d.DataTo(&c); err != nil {
			continue
		}
		res = append(res, &c)
	}
	return res, nil
}

// ListBySourcesRecent returns recent chunks for a set of sources (batched IN)
func (r *ChunksRepository) ListBySourcesRecent(ctx context.Context, tenantID string, sourceIDs []string, limit int) ([]*models.Chunk, error) {
	if len(sourceIDs) == 0 {
		return []*models.Chunk{}, nil
	}
	batchSize := 10
	var results []*models.Chunk
	for i := 0; i < len(sourceIDs); i += batchSize {
		end := i + batchSize
		if end > len(sourceIDs) {
			end = len(sourceIDs)
		}
		batch := sourceIDs[i:end]
		// Public chunks are global (tenantId may be empty), so filter only by sourceId and order by recency.
		q := r.col.Where("sourceId", "in", batch).OrderBy("createdAt", firestore.Desc)
		if limit > 0 {
			q = q.Limit(limit)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			var c models.Chunk
			if err := d.DataTo(&c); err != nil {
				continue
			}
			// For internal (tenant-scoped) chunks, enforce tenant match
			if strings.TrimSpace(c.TenantID) != "" && tenantID != c.TenantID {
				continue
			}
			results = append(results, &c)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// GetByIDs returns chunks by document IDs (batched lookups)
func (r *ChunksRepository) GetByIDs(ctx context.Context, ids []string) ([]*models.Chunk, error) {
	if len(ids) == 0 {
		return []*models.Chunk{}, nil
	}
	var res []*models.Chunk
	// Firestore allows only 10 in array-contains-any/in; use direct doc gets
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		d, err := r.col.Doc(id).Get(ctx)
		if err != nil {
			continue
		}
		var c models.Chunk
		if err := d.DataTo(&c); err != nil {
			continue
		}
		res = append(res, &c)
	}
	return res, nil
}

// DeleteByDoc deletes all chunks for a given document
func (r *ChunksRepository) DeleteByDoc(ctx context.Context, tenantID, docID string) error {
	if tenantID == "" || docID == "" {
		return ErrInvalidDocument
	}
	chunks, err := r.ListByDoc(ctx, tenantID, docID)
	if err != nil {
		return err
	}
	for _, c := range chunks {
		if _, err := r.col.Doc(c.ID).Delete(ctx); err != nil {
			// Log but continue - best effort deletion
			continue
		}
	}
	return nil
}

// Delete deletes a single chunk by ID
func (r *ChunksRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}
