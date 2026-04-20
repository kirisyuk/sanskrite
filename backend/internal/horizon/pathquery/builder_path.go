package pathquery

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
)

// upstreamMaterializer is the pathbuilder subset used by extendWithPathbuilder.
type upstreamMaterializer interface {
	BuildUpstreamPaths(ctx context.Context, customerRef string, opts pathbuilder.BuildPathOptions) ([]*pathbuilder.SupplyChainPath, error)
	PrepareMaterializedPathArtifactsBatch(ctx context.Context, paths []*pathbuilder.SupplyChainPath, tenantID, lensID string, canonicalKeys []string) ([]*horizonmodels.MaterializedArtifact, []string, error)
}

func pathTouchesFromCanonical(p *pathbuilder.SupplyChainPath, fromCP string) bool {
	if p == nil {
		return false
	}
	fromCP = strings.TrimSpace(fromCP)
	if fromCP == "" {
		return true
	}
	for _, seg := range p.Segments {
		if strings.TrimSpace(seg.SupplierRef) == fromCP ||
			strings.TrimSpace(seg.CustomerRef) == fromCP ||
			strings.TrimSpace(seg.ProductRef) == fromCP {
			return true
		}
	}
	return false
}

func (s *Service) extendWithPathbuilder(ctx context.Context, nq *normalizedQuery, diag *Diagnostics) []*horizonmodels.MaterializedArtifact {
	if s.pathBuilder == nil || !nq.ToPresent {
		return nil
	}
	customer := strings.TrimSpace(nq.ToCanonical)
	if !(strings.HasPrefix(customer, "/org/") || strings.HasPrefix(customer, "/fac/") || strings.HasPrefix(customer, "/gov/")) {
		return nil
	}
	if !s.bfsReachable(ctx, nq, diag) {
		return nil
	}
	opts := pathbuilder.DefaultBuildPathOptions()
	opts.TenantID = strings.TrimSpace(nq.Query.Tenant)
	opts.MaxDepth = nq.MaxHops
	opts.MaxPaths = maxOpenEndedPaths
	paths, err := s.pathBuilder.BuildUpstreamPaths(ctx, customer, opts)
	if err != nil || len(paths) == 0 {
		return nil
	}
	var filtered []*pathbuilder.SupplyChainPath
	for _, p := range paths {
		if pathTouchesFromCanonical(p, nq.FromCanonical) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		filtered = paths
	}
	if len(filtered) > maxOpenEndedPaths {
		diag.TruncatedResultCap = true
		filtered = filtered[:maxOpenEndedPaths]
	}
	tenant := strings.TrimSpace(nq.Query.Tenant)
	lens := strings.TrimSpace(nq.Query.Context.LensID)
	keys := make([]string, len(filtered))
	for i, p := range filtered {
		keys[i] = pathbuilder.BuildStructuralCanonicalKey(tenant, p.Segments)
	}
	arts, _, err := s.pathBuilder.PrepareMaterializedPathArtifactsBatch(ctx, filtered, tenant, lens, keys)
	if err != nil {
		diag.Warnings = append(diag.Warnings, "pathbuilder prepare failed: "+err.Error())
		return nil
	}
	return arts
}
