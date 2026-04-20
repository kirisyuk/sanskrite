package pathquery

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func traceContainsString(haystack []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range haystack {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

// CollectFragmentTraceSupplyChains mirrors HTTP collectFragmentTraceSupplyChains (selection + merge only).
func CollectFragmentTraceSupplyChains(ctx context.Context, mat FragmentTraceMaterialized, lenses LensGetter, frag *horizonmodels.HorizonFragment, lensID string) ([]*horizonmodels.MaterializedArtifact, error) {
	if mat == nil || frag == nil {
		return nil, nil
	}
	tenantID := strings.TrimSpace(frag.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	selected := make(map[string]*horizonmodels.MaterializedArtifact)

	appendArtifact := func(artifact *horizonmodels.MaterializedArtifact, pinned bool, source string, matched []string, overlapScore float64) {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			return
		}
		horizonmodels.PopulateSupplyChainPathIndexes(artifact)
		if existing := selected[artifact.ID]; existing != nil {
			if pinned && !existing.IsPinned {
				existing.IsPinned = true
				existing.DiscoverySource = "scoped"
				existing.DiscoveryReason = "attached to lens scope"
			}
			if overlapScore > existing.OverlapScore {
				existing.OverlapScore = overlapScore
				existing.MatchedRefs = append([]string(nil), matched...)
				if !existing.IsPinned {
					existing.DiscoverySource = source
					existing.DiscoveryReason = describeSupplyChainDiscovery(source, matched)
				}
			}
			return
		}
		copyArtifact := *artifact
		copyArtifact.IsPinned = pinned
		copyArtifact.MatchedRefs = append([]string(nil), matched...)
		copyArtifact.OverlapScore = overlapScore
		if pinned {
			copyArtifact.DiscoverySource = "scoped"
			copyArtifact.DiscoveryReason = "attached to lens scope"
		} else {
			copyArtifact.DiscoverySource = source
			copyArtifact.DiscoveryReason = describeSupplyChainDiscovery(source, matched)
		}
		selected[copyArtifact.ID] = &copyArtifact
	}

	if cp != "" {
		if items, err := mat.FindByNodeRef(ctx, tenantID, cp, horizonmodels.MaterializedKindSupplyChainPath); err == nil {
			for _, artifact := range items {
				appendArtifact(artifact, false, "node_overlap", []string{cp}, 1.0)
			}
		}
		if horizonmodels.ClassSubtypeAffectedZone.MatchesFragment(frag) {
			if items, err := mat.FindByAffectedZoneRefsAny(ctx, tenantID, []string{cp}, horizonmodels.MaterializedKindSupplyChainPath, 50); err == nil {
				for _, artifact := range items {
					appendArtifact(artifact, false, "affected_zone_overlap", []string{cp}, 0.9)
				}
			}
		}
	}

	if lensID != "" && lenses != nil {
		if lens, err := lenses.Get(ctx, lensID); err == nil && lens != nil {
			scoped, discovered, err := CollectSupplyChainsForLens(ctx, mat, lens, 100)
			if err == nil {
				for _, artifact := range append(scoped, discovered...) {
					if artifact == nil {
						continue
					}
					horizonmodels.PopulateSupplyChainPathIndexes(artifact)
					if cp != "" {
						if traceContainsString(artifact.NodeRefs, cp) || traceContainsString(artifact.AffectedZoneRefs, cp) || traceContainsString(artifact.ZoneRefs, cp) {
							appendArtifact(artifact, artifact.IsPinned, artifact.DiscoverySource, artifact.MatchedRefs, artifact.OverlapScore)
						}
					}
				}
			}
		}
	}

	out := make([]*horizonmodels.MaterializedArtifact, 0, len(selected))
	for _, artifact := range selected {
		out = append(out, artifact)
	}
	return out, nil
}

// SearchSupplyChainAnchors is the shared candidate discovery used by POST supply-chains/search.
func SearchSupplyChainAnchors(ctx context.Context, mat SupplyChainSearchMaterialized, lenses LensGetter, tenantID string, anchors []string, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if mat == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if strings.TrimSpace(lensID) != "" && lenses != nil {
		lens, err := lenses.Get(ctx, strings.TrimSpace(lensID))
		if err == nil && lens != nil {
			batchLimit := limit * 3
			if batchLimit < 50 {
				batchLimit = 50
			}
			scoped, discovered, cerr := CollectSupplyChainsForLens(ctx, mat, lens, batchLimit)
			if cerr == nil {
				return filterArtifactsByAnchors(append(scoped, discovered...), anchors), nil
			}
		}
	}
	if hasSupplyLinkAnchor(anchors) {
		artifacts, err := mat.ListByTenantAndKind(ctx, tenantID, horizonmodels.MaterializedKindSupplyChainPath, 10000)
		if err != nil {
			return nil, err
		}
		if len(artifacts) == 0 && tenantID != "global" {
			artifacts, err = mat.ListByTenantAndKind(ctx, "global", horizonmodels.MaterializedKindSupplyChainPath, 10000)
			if err != nil {
				return nil, err
			}
		}
		return filterArtifactsByAnchors(artifacts, anchors), nil
	}
	artifacts, err := mat.FindByNodeRef(ctx, tenantID, anchors[0], horizonmodels.MaterializedKindSupplyChainPath)
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 && tenantID != "global" {
		artifacts, err = mat.FindByNodeRef(ctx, "global", anchors[0], horizonmodels.MaterializedKindSupplyChainPath)
		if err != nil {
			return nil, err
		}
	}
	return filterArtifactsByAnchors(artifacts, anchors), nil
}
