package metriccore

import (
	"context"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

// collectArtifactsFromResolved loads materialized artifacts by IDs produced by workscope.Resolver
// (explicit artifact IDs plus lens expansion), then applies the same profile filter as legacy collectArtifacts.
func (h *OperationalHydrator) collectArtifactsFromResolved(ctx context.Context, resolved workscope.ResolvedWorkSet, profile simscope.Profile) []*horizonmodels.MaterializedArtifact {
	if h == nil || h.materialized == nil {
		return nil
	}
	return workscope.CollectMaterializedArtifacts(ctx, h.materialized, resolved, profile)
}
