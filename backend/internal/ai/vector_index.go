package ai

import "context"

// VectorIndex abstracts vector storage and KNN search
type VectorIndex interface {
	// Upsert inserts or replaces a vector for an entity ID with metadata
	Upsert(ctx context.Context, id string, vector []float64, metadata map[string]any) error
	// Query searches topK nearest with optional filters
	Query(ctx context.Context, vector []float64, topK int, filters map[string]any) ([]string, error)
}
