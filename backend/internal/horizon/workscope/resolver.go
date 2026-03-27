package workscope

import (
	"context"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// MaterializedArtifactsPort is the subset of materialized artifact storage needed to resolve scope.
type MaterializedArtifactsPort interface {
	ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error)
}

// Resolver expands WorkScope into ResolvedWorkSet (artifact ID union matches operational hydrator defaults).
type Resolver struct {
	materialized MaterializedArtifactsPort
}

// NewResolver builds a resolver backed by the materialized artifacts repository.
func NewResolver(m MaterializedArtifactsPort) *Resolver {
	if m == nil {
		return &Resolver{}
	}
	return &Resolver{materialized: m}
}

// Resolve performs deterministic expansion: explicit artifact IDs plus ListByLens when LensID is set.
func (r *Resolver) Resolve(ctx context.Context, scope WorkScope) (ResolvedWorkSet, error) {
	out := ResolvedFromScope(scope)
	if r == nil || r.materialized == nil {
		out.MaterializedArtifactIDs = sortedCopy(out.MaterializedArtifactIDs)
		return out, nil
	}
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, id := range out.MaterializedArtifactIDs {
		add(id)
	}
	if strings.TrimSpace(out.LensID) != "" {
		// Match operational hydrator: ListByLens errors are ignored; explicit artifact IDs still apply.
		artifacts, err := r.materialized.ListByLens(ctx, strings.TrimSpace(out.LensID), MaterializedListByLensLimitDefault)
		if err == nil {
			for _, a := range artifacts {
				if a == nil {
					continue
				}
				key := strings.TrimSpace(a.ID)
				if key == "" {
					key = strings.TrimSpace(a.CanonicalKey)
				}
				add(key)
			}
		}
	}
	sort.Strings(ids)
	out.MaterializedArtifactIDs = ids
	return out, nil
}
