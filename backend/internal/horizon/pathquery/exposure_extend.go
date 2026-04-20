package pathquery

import (
	"context"
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// exposurePathsLister lists lens-scoped exposure path artifacts (horizon_exposure_paths).
type exposurePathsLister interface {
	ListByLens(ctx context.Context, tenantID, lensID string, typ *horizonmodels.ExposureType, limit int) ([]*horizonmodels.ExposurePathArtifact, error)
}

// WithExposurePaths wires horizon_exposure_paths reads into QueryPaths (step 4 of design §4).
func (s *Service) WithExposurePaths(repo exposurePathsLister) *Service {
	if s == nil {
		return nil
	}
	s.exposure = repo
	return s
}

func exposureTouchesCanonical(exp *horizonmodels.ExposurePathArtifact, canon string) bool {
	if exp == nil || strings.TrimSpace(canon) == "" {
		return false
	}
	if strings.TrimSpace(exp.ObjectiveID) == canon || strings.TrimSpace(exp.RelatedID) == canon {
		return true
	}
	for _, nid := range exp.NodeIDs {
		if strings.TrimSpace(nid) == canon {
			return true
		}
	}
	return false
}

func exposureTouchesFragment(exp *horizonmodels.ExposurePathArtifact, fragID string) bool {
	if exp == nil || strings.TrimSpace(fragID) == "" {
		return false
	}
	for _, nid := range exp.NodeIDs {
		if strings.TrimSpace(nid) == fragID {
			return true
		}
	}
	return false
}

func exposureMatchesQuery(exp *horizonmodels.ExposurePathArtifact, nq *normalizedQuery) bool {
	if exp == nil || nq == nil {
		return false
	}
	from := strings.TrimSpace(nq.FromCanonical)
	fromHit := exposureTouchesCanonical(exp, from) || exposureTouchesFragment(exp, nq.FromFragmentID)
	if !fromHit {
		return false
	}
	if !nq.ToPresent {
		return true
	}
	to := strings.TrimSpace(nq.ToCanonical)
	return exposureTouchesCanonical(exp, to) || exposureTouchesFragment(exp, nq.ToFragmentID)
}

func (s *Service) resolveLookupCanonicals(ctx context.Context, tenantID, ref string) []string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, "/") {
		return []string{ref}
	}
	if s.frags == nil {
		return nil
	}
	frag, err := s.frags.Get(ctx, ref)
	if err != nil || frag == nil {
		return nil
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	if cp == "" {
		return nil
	}
	return []string{cp}
}

func (s *Service) collectSupplyPathArtsByNodeRef(ctx context.Context, tenantID, nodeRef string) []*horizonmodels.MaterializedArtifact {
	nodeRef = strings.TrimSpace(nodeRef)
	if nodeRef == "" || s.mat == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	try := make([]string, 0, 2)
	if tenantID != "" {
		try = append(try, tenantID)
	}
	if tenantID != "global" {
		try = append(try, "global")
	}
	var out []*horizonmodels.MaterializedArtifact
	for _, t := range try {
		arts, err := s.mat.FindByNodeRef(ctx, t, nodeRef, horizonmodels.MaterializedKindSupplyChainPath)
		if err == nil && len(arts) > 0 {
			out = append(out, arts...)
			break
		}
	}
	return out
}

// mergeExposurePathFamilies appends SUPPLY_CHAIN_PATH artifacts discovered via exposure projections
// (same tenant + lens as query). Runs after materialized + pathbuilder per design §4.
func (s *Service) mergeExposurePathFamilies(ctx context.Context, q Query, nq *normalizedQuery, paths []*horizonmodels.MaterializedArtifact, diag *Diagnostics) []*horizonmodels.MaterializedArtifact {
	if s == nil || s.exposure == nil || diag == nil || nq == nil {
		return paths
	}
	lensID := strings.TrimSpace(q.Context.LensID)
	if lensID == "" {
		return paths
	}
	tenantID := strings.TrimSpace(q.Tenant)
	items, err := s.exposure.ListByLens(ctx, tenantID, lensID, nil, 500)
	if err != nil {
		diag.Warnings = append(diag.Warnings, fmt.Sprintf("exposure_paths: %v", err))
		return paths
	}
	var matched []*horizonmodels.ExposurePathArtifact
	for _, e := range items {
		if exposureMatchesQuery(e, nq) {
			matched = append(matched, e)
		}
	}
	if len(matched) == 0 {
		return paths
	}
	diag.ExposurePathFamilies = len(matched)

	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if p == nil {
			continue
		}
		if id := strings.TrimSpace(p.ID); id != "" {
			seen[id] = struct{}{}
		}
	}

	anchors := []string{strings.TrimSpace(nq.FromCanonical)}
	if nq.ToPresent {
		anchors = append(anchors, strings.TrimSpace(nq.ToCanonical))
	}

	var extra []*horizonmodels.MaterializedArtifact
	refSeen := make(map[string]struct{})
	for _, exp := range matched {
		var refs []string
		for _, r := range []string{exp.ObjectiveID, exp.RelatedID} {
			for _, cp := range s.resolveLookupCanonicals(ctx, tenantID, r) {
				if _, ok := refSeen[cp]; ok {
					continue
				}
				refSeen[cp] = struct{}{}
				refs = append(refs, cp)
			}
		}
		for _, nid := range exp.NodeIDs {
			for _, cp := range s.resolveLookupCanonicals(ctx, tenantID, nid) {
				if _, ok := refSeen[cp]; ok {
					continue
				}
				refSeen[cp] = struct{}{}
				refs = append(refs, cp)
			}
		}
		for _, cp := range refs {
			for _, a := range s.collectSupplyPathArtsByNodeRef(ctx, tenantID, cp) {
				if a == nil {
					continue
				}
				id := strings.TrimSpace(a.ID)
				if id == "" {
					continue
				}
				if _, ok := seen[id]; ok {
					continue
				}
				extra = append(extra, a)
				seen[id] = struct{}{}
			}
		}
	}

	if len(extra) == 0 {
		return paths
	}
	extra = filterArtifactsByAnchors(extra, anchors)
	extra = filterByProductAxis(extra, q.ProductAxis, diag)
	if len(extra) > 0 {
		diag.MaterializedHits += len(extra)
	}
	out := append(paths[:len(paths):len(paths)], extra...)
	return out
}
