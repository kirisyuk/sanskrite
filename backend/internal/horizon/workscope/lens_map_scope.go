package workscope

import (
	"context"
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// LensScopePathsWithResolvedQueries expands lens.Scope the same way as the world-map geo-objects endpoint:
// /query/... entries are replaced with lens.ResolvedQueries when present; result is deduped in first-seen order.
// Paths are not canonicalized here — the caller runs the scout/taxonomy resolver if needed (see horizon HTTP handlers).
func LensScopePathsWithResolvedQueries(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	rawScope := append([]string(nil), lens.Scope...)
	scopePaths := make([]string, 0, len(rawScope)+8)
	seen := map[string]struct{}{}
	for _, s := range rawScope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "/query/") && lens.ResolvedQueries != nil {
			if resolved := lens.ResolvedQueries[s]; len(resolved) > 0 {
				for _, rp := range resolved {
					rp = strings.TrimSpace(rp)
					if rp == "" {
						continue
					}
					if _, ok := seen[rp]; ok {
						continue
					}
					seen[rp] = struct{}{}
					scopePaths = append(scopePaths, rp)
				}
				continue
			}
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		scopePaths = append(scopePaths, s)
	}
	return scopePaths
}

// MapLensFragmentsPort is the fragment subset needed for map-style 1-hop expansion.
type MapLensFragmentsPort interface {
	ListByCanonicalPaths(ctx context.Context, paths []string) ([]*horizonmodels.HorizonFragment, error)
	GetMany(ctx context.Context, ids []string) (map[string]*horizonmodels.HorizonFragment, error)
}

// MapLensEdgesPort is the edge subset used for map 1-hop neighbors (same tenant as listGeoObjectsForLens).
type MapLensEdgesPort interface {
	ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *horizonmodels.DependencyType, limit int) ([]*horizonmodels.DependencyEdge, error)
	ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *horizonmodels.DependencyType, limit int) ([]*horizonmodels.DependencyEdge, error)
}

const defaultMapEdgeListLimit = 200

// CollectMapOneHopNeighborFragmentIDs returns dependency-graph neighbors of scope fragments that are not
// themselves scope members (parity with listGeoObjectsForLens).
func CollectMapOneHopNeighborFragmentIDs(ctx context.Context, edgeTenantID string, scopeFragments []*horizonmodels.HorizonFragment, edges MapLensEdgesPort) []string {
	if edges == nil || len(scopeFragments) == 0 {
		return nil
	}
	edgeTenantID = strings.TrimSpace(edgeTenantID)
	if edgeTenantID == "" {
		edgeTenantID = "global"
	}
	scopeIDs := make(map[string]struct{}, len(scopeFragments))
	for _, f := range scopeFragments {
		if f != nil && strings.TrimSpace(f.ID) != "" {
			scopeIDs[strings.TrimSpace(f.ID)] = struct{}{}
		}
	}
	neighborIDs := make(map[string]struct{}, 64)
	for _, f := range scopeFragments {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		fid := strings.TrimSpace(f.ID)
		outgoing, _ := edges.ListBySource(ctx, edgeTenantID, fid, nil, defaultMapEdgeListLimit)
		for _, e := range outgoing {
			if e == nil {
				continue
			}
			tid := strings.TrimSpace(e.TargetFragmentID)
			if tid != "" {
				if _, inScope := scopeIDs[tid]; !inScope {
					neighborIDs[tid] = struct{}{}
				}
			}
		}
		incoming, _ := edges.ListByTarget(ctx, edgeTenantID, fid, nil, defaultMapEdgeListLimit)
		for _, e := range incoming {
			if e == nil {
				continue
			}
			sid := strings.TrimSpace(e.SourceFragmentID)
			if sid != "" {
				if _, inScope := scopeIDs[sid]; !inScope {
					neighborIDs[sid] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(neighborIDs))
	for id := range neighborIDs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// FromLensMapWorkScope builds a WorkScope from canonical paths that represent the lens map substrate
// (after query expansion + path canonicalization). LensID keeps artifact/hydration list-by-lens behavior.
func FromLensMapWorkScope(tenantID, projectID, lensID string, canonicalScopePaths []string) WorkScope {
	return Normalize(FromBaselineRefs(strings.TrimSpace(tenantID), strings.TrimSpace(projectID), horizonmodels.SimulationBaselineRefs{
		LensID:     strings.TrimSpace(lensID),
		ScopePaths: append([]string(nil), canonicalScopePaths...),
	}))
}

// MergeMapNeighborWorkScope unions map 1-hop neighbor paths and fragment IDs into an existing scope.
func MergeMapNeighborWorkScope(base WorkScope, neighborCanonicalPaths []string, neighborFragmentIDs []string) WorkScope {
	base = Normalize(base)
	return Merge(base, FromBaselineRefs(base.TenantID, base.ProjectID, horizonmodels.SimulationBaselineRefs{
		ScopePaths:  neighborCanonicalPaths,
		FragmentIDs: neighborFragmentIDs,
	}))
}

// ResolveLensMapWorkScope builds the effective work scope for “lens as on the map”: canonical paths plus
// one-hop graph neighbors (paths + fragment IDs). canonicalScopePaths should match geo-objects after resolver.
func ResolveLensMapWorkScope(ctx context.Context, lens *horizonmodels.Lens, edgeTenantID string, canonicalScopePaths []string, frags MapLensFragmentsPort, edges MapLensEdgesPort) (WorkScope, error) {
	if lens == nil {
		return WorkScope{}, fmt.Errorf("lens is nil")
	}
	if frags == nil {
		return WorkScope{}, fmt.Errorf("fragments port is nil")
	}
	tenantID := strings.TrimSpace(lens.TenantID)
	projectID := strings.TrimSpace(lens.ProjectID)
	lensID := strings.TrimSpace(lens.ID)
	base := FromLensMapWorkScope(tenantID, projectID, lensID, canonicalScopePaths)
	scopeFragments, err := frags.ListByCanonicalPaths(ctx, canonicalScopePaths)
	if err != nil {
		return WorkScope{}, err
	}
	neighborIDs := CollectMapOneHopNeighborFragmentIDs(ctx, edgeTenantID, scopeFragments, edges)
	if len(neighborIDs) == 0 {
		return base, nil
	}
	nbMap, err := frags.GetMany(ctx, neighborIDs)
	if err != nil {
		return base, err
	}
	neighborPaths := make([]string, 0, len(nbMap))
	for _, f := range nbMap {
		if f == nil {
			continue
		}
		if p := strings.TrimSpace(f.CanonicalPath); p != "" {
			neighborPaths = append(neighborPaths, p)
		}
	}
	return MergeMapNeighborWorkScope(base, neighborPaths, neighborIDs), nil
}
