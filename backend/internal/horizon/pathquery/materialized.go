package pathquery

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func (s *Service) materializedForQuery(ctx context.Context, nq *normalizedQuery, diag *Diagnostics) ([]*horizonmodels.MaterializedArtifact, error) {
	if s.mat == nil {
		return nil, nil
	}
	tenant := strings.TrimSpace(nq.Query.Tenant)
	fromCP := strings.TrimSpace(nq.FromCanonical)
	arts, err := s.mat.FindByNodeRef(ctx, tenant, fromCP, horizonmodels.MaterializedKindSupplyChainPath)
	if err != nil {
		return nil, err
	}
	if len(arts) == 0 && tenant != "global" {
		arts, err = s.mat.FindByNodeRef(ctx, "global", fromCP, horizonmodels.MaterializedKindSupplyChainPath)
		if err != nil {
			return nil, err
		}
	}
	anchors := []string{fromCP}
	if nq.ToPresent {
		anchors = append(anchors, strings.TrimSpace(nq.ToCanonical))
	}
	arts = filterArtifactsByAnchors(arts, anchors)
	arts = filterByProductAxis(arts, nq.Query.ProductAxis, diag)
	diag.MaterializedHits = len(arts)
	return arts, nil
}
