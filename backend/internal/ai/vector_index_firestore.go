package ai

import (
	"context"
	"sort"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// FirestoreVectorIndex provides KNN search abstraction.
// Uses native Firestore vector search when available, falls back to in-app KNN.
type FirestoreVectorIndex struct {
	chunks *repo.ChunksRepository
	client *firestore.Client
}

func NewFirestoreVectorIndex(chunks *repo.ChunksRepository) *FirestoreVectorIndex {
	return &FirestoreVectorIndex{chunks: chunks}
}

// WithClient sets the Firestore client for native vector search
func (i *FirestoreVectorIndex) WithClient(client *firestore.Client) *FirestoreVectorIndex {
	i.client = client
	return i
}

func (i *FirestoreVectorIndex) Upsert(ctx context.Context, id string, vector []float64, metadata map[string]any) error {
	// Embeddings are persisted with chunks; index management is declarative via firestore.indexes.json
	return nil
}

func (i *FirestoreVectorIndex) Query(ctx context.Context, vector []float64, topK int, filters map[string]any) ([]string, error) {
	// Expected filters: tenantId (string), sourceIds ([]string)
	if i.chunks == nil {
		return []string{}, nil
	}
	tenantID, _ := filters["tenantId"].(string)
	var sourceIDs []string
	if v, ok := filters["sourceIds"]; ok {
		switch t := v.(type) {
		case []string:
			sourceIDs = t
		case []any:
			for _, it := range t {
				if s, ok := it.(string); ok {
					sourceIDs = append(sourceIDs, s)
				}
			}
		}
	}

	// Try native Firestore vector search first (if client is available)
	if i.client != nil {
		return i.queryNative(ctx, vector, topK, tenantID, sourceIDs)
	}

	// Fallback to in-app KNN search
	return i.queryInApp(ctx, vector, topK, tenantID, sourceIDs)
}

// queryNative uses Firestore's native FindNearest vector search
// Requires Firestore SDK v1.15.0+ and vector field configured in Firestore
func (i *FirestoreVectorIndex) queryNative(ctx context.Context, vector []float64, topK int, tenantID string, sourceIDs []string) ([]string, error) {
	col := i.client.Collection("rag_chunks")

	// Build query with filters
	q := col.Query
	if tenantID != "" {
		q = q.Where("tenantId", "==", tenantID)
	}
	if len(sourceIDs) > 0 {
		// Firestore IN supports up to 10 values
		if len(sourceIDs) <= 10 {
			q = q.Where("sourceId", "in", sourceIDs)
		} else {
			// For more than 10, fall back to in-app search
			return i.queryInApp(ctx, vector, topK, tenantID, sourceIDs)
		}
	}

	// Use FindNearest for native vector search
	// FindNearest returns VectorQuery, which we execute to get documents
	vectorQuery := q.FindNearest("embedding", vector, topK, firestore.DistanceMeasureCosine, nil)

	// Execute the vector query
	docs, err := vectorQuery.Documents(ctx).GetAll()
	if err != nil {
		// If native search fails (e.g., vector field not configured), fall back to in-app
		return i.queryInApp(ctx, vector, topK, tenantID, sourceIDs)
	}

	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.Ref.ID)
	}
	return ids, nil
}

// queryInApp performs KNN search in application memory (fallback)
func (i *FirestoreVectorIndex) queryInApp(ctx context.Context, vector []float64, topK int, tenantID string, sourceIDs []string) ([]string, error) {
	// fetch a reasonable candidate set
	candidates, err := i.chunks.ListBySourcesRecent(ctx, tenantID, sourceIDs, 500)
	if err != nil {
		return nil, err
	}
	type scored struct {
		id    string
		score float64
	}
	var items []scored
	for _, c := range candidates {
		if len(c.Embedding) == 0 {
			continue
		}
		s := CosineSimilarity(vector, c.Embedding)
		items = append(items, scored{id: c.ID, score: s})
	}
	// Sort by score descending
	sort.Slice(items, func(a, b int) bool {
		return items[a].score > items[b].score
	})
	if topK > 0 && len(items) > topK {
		items = items[:topK]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.id)
	}
	return out, nil
}
