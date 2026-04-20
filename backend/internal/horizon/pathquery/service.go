package pathquery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/metriccore"
)

// Service centralizes path reads + optional Firestore cache (horizon_path_cache).
type Service struct {
	frags       fragmentLookup
	edges       dependencyEdges
	mat         materializedSupplyPaths
	exposure    exposurePathsLister
	cache       pathCacheBackend
	pathBuilder upstreamMaterializer
}

// NewService constructs a Service. db may be nil to disable cache persistence.
func NewService(
	frags fragmentLookup,
	edges dependencyEdges,
	mat materializedSupplyPaths,
	db *firestore.Client,
	pathBuilder upstreamMaterializer,
) *Service {
	return &Service{
		frags:       frags,
		edges:       edges,
		mat:         mat,
		cache:       newFirestorePathCache(db),
		pathBuilder: pathBuilder,
	}
}

// WithPathCacheForTest replaces the cache backend (unit tests only).
func (s *Service) WithPathCacheForTest(c pathCacheBackend) *Service {
	if s == nil {
		return nil
	}
	s.cache = c
	return s
}

// QueryPaths implements the P8 read pipeline.
func (s *Service) QueryPaths(ctx context.Context, q Query) (*Result, error) {
	nq, err := normalizeQuery(q)
	if err != nil {
		return nil, err
	}
	if s.frags == nil || s.mat == nil {
		return nil, fmt.Errorf("pathquery: fragments and materialized repositories are required")
	}
	if err := s.resolveEndpoints(ctx, &nq); err != nil {
		return nil, err
	}

	cacheKey := computeCacheKey(q, nq.FromKind, nq.FromValue, nq.ToPresent, nq.ToKind, nq.ToValue, nq.MaxHops, nq.RelationKinds)
	diag := Diagnostics{}

	if nq.CacheRead && nq.EffectiveTTL > 0 && s.cache != nil {
		doc, cerr := s.cacheGet(ctx, cacheKey)
		if cerr == nil && doc != nil && doc.ExpiresAt.After(time.Now().UTC()) && len(doc.PathIDs) > 0 {
			paths, herr := s.hydratePathsFromCache(ctx, q.Tenant, doc.PathIDs)
			if herr == nil && len(paths) > 0 {
				metriccore.IncPathqueryCacheHit()
				enrichArtifactsInPlace(paths, q.IncludeStress, q.IncludeGeo)
				return &Result{
					Paths:       paths,
					FromCache:   true,
					GeneratedAt: doc.GeneratedAt,
					CacheKey:    cacheKey,
					Diagnostics: diag,
				}, nil
			}
		}
	}
	if nq.CacheRead && nq.EffectiveTTL > 0 {
		metriccore.IncPathqueryCacheMiss()
	}

	paths, err := s.materializedForQuery(ctx, &nq, &diag)
	if err != nil {
		return nil, err
	}

	if nq.ToPresent && len(paths) == 0 {
		beforePB := diag.BFSExpandedNodes
		built := s.extendWithPathbuilder(ctx, &nq, &diag)
		if len(built) > 0 {
			metriccore.IncPathqueryBuilderInvocation()
			diag.PathbuilderRuns = 1
		}
		paths = append(paths, built...)
		_ = beforePB
	}

	paths = s.mergeExposurePathFamilies(ctx, q, &nq, paths, &diag)

	if !nq.ToPresent && len(paths) > maxOpenEndedPaths {
		diag.TruncatedResultCap = true
		paths = paths[:maxOpenEndedPaths]
	}

	enrichArtifactsInPlace(paths, q.IncludeStress, q.IncludeGeo)

	genAt := time.Now().UTC()
	if nq.CacheWrite && nq.EffectiveTTL > 0 && s.cache != nil && len(paths) > 0 {
		ids := make([]string, 0, len(paths))
		for _, p := range paths {
			if p == nil {
				continue
			}
			if strings.TrimSpace(p.ID) != "" {
				ids = append(ids, strings.TrimSpace(p.ID))
			}
		}
		if len(ids) > 0 {
			diagBytes, _ := json.Marshal(diag)
			var diagMap map[string]interface{}
			_ = json.Unmarshal(diagBytes, &diagMap)
			doc := pathCacheDocument{
				CacheKey:    cacheKey,
				Tenant:      strings.TrimSpace(q.Tenant),
				Project:     strings.TrimSpace(q.Project),
				Query:       safeQueryDebugMap(q, &nq),
				PathIDs:     ids,
				Diagnostics: diagMap,
				GeneratedAt: genAt,
				ExpiresAt:   genAt.Add(nq.EffectiveTTL),
				NodeRefs:    unionNodeRefsFromArtifacts(paths),
			}
			_ = s.cacheWrite(ctx, cacheKey, doc)
		}
	}

	return &Result{
		Paths:       paths,
		FromCache:   false,
		GeneratedAt: genAt,
		CacheKey:    cacheKey,
		Diagnostics: diag,
	}, nil
}

// InvalidatePathCache deletes a single cache entry by key (tenant-scoped document id).
func (s *Service) InvalidatePathCache(ctx context.Context, cacheKey string) error {
	return s.cacheDelete(ctx, strings.TrimSpace(cacheKey))
}

// InvalidateByNode deletes cache documents for the tenant where nodeRefs contains canonicalPath.
func (s *Service) InvalidateByNode(ctx context.Context, tenantID, canonicalPath string) error {
	return s.invalidateByNodeInternal(ctx, tenantID, canonicalPath)
}
