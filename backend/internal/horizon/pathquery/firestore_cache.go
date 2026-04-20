package pathquery

import (
	"context"
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func (s *Service) cacheGet(ctx context.Context, cacheKey string) (*pathCacheDocument, error) {
	if s.cache == nil || cacheKey == "" {
		return nil, nil
	}
	return s.cache.getDoc(ctx, cacheKey)
}

func (s *Service) cacheWrite(ctx context.Context, cacheKey string, doc pathCacheDocument) error {
	if s.cache == nil || cacheKey == "" {
		return nil
	}
	return s.cache.setDoc(ctx, cacheKey, doc)
}

func (s *Service) cacheDelete(ctx context.Context, cacheKey string) error {
	if s.cache == nil || cacheKey == "" {
		return nil
	}
	return s.cache.deleteDoc(ctx, cacheKey)
}

func (s *Service) hydratePathsFromCache(ctx context.Context, tenantID string, ids []string) ([]*horizonmodels.MaterializedArtifact, error) {
	if s.mat == nil {
		return nil, fmt.Errorf("materialized repo not configured")
	}
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		a, err := s.mat.Get(ctx, id)
		if err != nil || a == nil {
			continue
		}
		if strings.TrimSpace(a.TenantID) != strings.TrimSpace(tenantID) && strings.TrimSpace(a.TenantID) != "global" && tenantID != "global" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Service) invalidateByNodeInternal(ctx context.Context, tenantID, canonicalPath string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.deleteByNode(ctx, tenantID, canonicalPath)
}

func safeQueryDebugMap(q Query, nq *normalizedQuery) map[string]interface{} {
	return map[string]interface{}{
		"tenant":        strings.TrimSpace(q.Tenant),
		"project":       strings.TrimSpace(q.Project),
		"fromKind":      int(nq.FromKind),
		"fromValue":     nq.FromValue,
		"toPresent":     nq.ToPresent,
		"productAxis":   strings.TrimSpace(q.ProductAxis),
		"maxHops":       nq.MaxHops,
		"lensId":        strings.TrimSpace(q.Context.LensID),
		"fromCanonical": nq.FromCanonical,
		"toCanonical":   nq.ToCanonical,
	}
}
