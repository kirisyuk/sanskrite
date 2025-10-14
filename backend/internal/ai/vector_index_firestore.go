package ai

import (
	"context"
	"sort"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// FirestoreVectorIndex provides KNN search abstraction.
// For now it performs an in-app KNN over recent chunks until native Firestore Vector client calls are available.
type FirestoreVectorIndex struct{ chunks *repo.ChunksRepository }

func NewFirestoreVectorIndex(chunks *repo.ChunksRepository) *FirestoreVectorIndex {
	return &FirestoreVectorIndex{chunks: chunks}
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
	// optional filter hints (not applied here, but parsed for future vector backends)
	_ = filters["language"]
	_ = filters["documentType"]
	_ = filters["since"]
	_ = filters["until"]

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
	sort.Slice(items, func(a, b int) bool { return items[a].score > items[b].score })
	if topK > 0 && len(items) > topK {
		items = items[:topK]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.id)
	}
	return out, nil
}
