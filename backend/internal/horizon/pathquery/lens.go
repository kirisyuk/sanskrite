package pathquery

import (
	"context"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

type supplyChainLensContext struct {
	NodeRefs         []string
	FlowRefs         []string
	ZoneRefs         []string
	AffectedZoneRefs []string
	ArtifactKeys     []string
}

func buildSupplyChainLensContext(lens *horizonmodels.Lens) supplyChainLensContext {
	out := supplyChainLensContext{}
	if lens == nil {
		return out
	}
	out.NodeRefs = mergeUniqueStrings(out.NodeRefs, workscope.LensSupplyChainNodeRefs(lens))
	for artKey, ref := range lens.ScopeArtifacts {
		if strings.TrimSpace(artKey) != "" {
			out.ArtifactKeys = append(out.ArtifactKeys, artKey)
		}
		out.NodeRefs = mergeUniqueStrings(out.NodeRefs, ref.NodeRefs)
		out.FlowRefs = mergeUniqueStrings(out.FlowRefs, ref.FlowRefs)
		out.ZoneRefs = mergeUniqueStrings(out.ZoneRefs, ref.ZoneRefs)
		out.AffectedZoneRefs = mergeUniqueStrings(out.AffectedZoneRefs, ref.AffectedZoneRefs)
	}
	sort.Strings(out.ArtifactKeys)
	return out
}

func scoreSupplyChainOverlap(artifact *horizonmodels.MaterializedArtifact, ctx supplyChainLensContext) (float64, []string) {
	if artifact == nil {
		return 0, nil
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	nodeMatches := intersectCount(artifact.NodeRefs, ctx.NodeRefs)
	flowMatches := intersectCount(artifact.FlowRefs, ctx.FlowRefs)
	zoneMatches := intersectCount(artifact.ZoneRefs, ctx.ZoneRefs)
	affectedMatches := intersectCount(artifact.AffectedZoneRefs, ctx.AffectedZoneRefs)
	score := float64(nodeMatches)*1.0 + float64(flowMatches)*0.75 + float64(zoneMatches)*0.6 + float64(affectedMatches)*0.9
	if score <= 0 {
		return 0, nil
	}
	matched := make([]string, 0, 8)
	matched = append(matched, firstIntersect(artifact.NodeRefs, ctx.NodeRefs, 4)...)
	matched = append(matched, firstIntersect(artifact.FlowRefs, ctx.FlowRefs, 2)...)
	matched = append(matched, firstIntersect(artifact.ZoneRefs, ctx.ZoneRefs, 2)...)
	matched = append(matched, firstIntersect(artifact.AffectedZoneRefs, ctx.AffectedZoneRefs, 2)...)
	return score, mergeUniqueStrings(nil, matched)
}

func describeSupplyChainDiscovery(source string, matched []string) string {
	switch source {
	case "node_overlap":
		return "discovered from overlapping graph entities"
	case "flow_overlap":
		return "discovered from overlapping route bindings"
	case "zone_overlap":
		return "discovered from overlapping zone bindings"
	case "affected_zone_overlap":
		return "discovered from overlapping affected-zone bindings"
	default:
		if len(matched) > 0 {
			return "discovered from lens context overlap"
		}
		return ""
	}
}

func intersectCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	count := 0
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := set[value]; ok {
			count++
		}
	}
	return count
}

func firstIntersect(left, right []string, limit int) []string {
	if len(left) == 0 || len(right) == 0 || limit <= 0 {
		return nil
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, limit)
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := set[value]; ok {
			out = append(out, value)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func sortSupplyChainArtifacts(artifacts []*horizonmodels.MaterializedArtifact) {
	sort.SliceStable(artifacts, func(i, j int) bool {
		left := artifacts[i]
		right := artifacts[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.TracePriority != right.TracePriority {
			return left.TracePriority > right.TracePriority
		}
		if left.IsPinned != right.IsPinned {
			return left.IsPinned
		}
		if left.OverlapScore != right.OverlapScore {
			return left.OverlapScore > right.OverlapScore
		}
		leftStress := artifactPathStressScore(left)
		rightStress := artifactPathStressScore(right)
		if leftStress != rightStress {
			return leftStress > rightStress
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})
}

func artifactPathStressScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil && parsed.Stress != nil {
		return parsed.Stress.PathScore
	}
	return 0
}

// ExtractPreferredAssemblyPathKeys returns direct path keys from an assembly graph artifact, else derived keys.
func ExtractPreferredAssemblyPathKeys(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainAssemblyGraph {
		return nil
	}
	parsed, err := horizonmodels.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return nil
	}
	keys := append([]string(nil), parsed.DirectPathKeys...)
	if len(keys) == 0 {
		keys = append(keys, parsed.DerivedPathKeys...)
	}
	return mergeUniqueStrings(nil, keys)
}

// CollectSupplyChainsForLens mirrors HTTP collectSupplyChainsForLens without trace annotation side effects.
func CollectSupplyChainsForLens(ctx context.Context, mat LensMaterializedRepo, lens *horizonmodels.Lens, limit int) ([]*horizonmodels.MaterializedArtifact, []*horizonmodels.MaterializedArtifact, error) {
	if mat == nil || lens == nil {
		return nil, nil, nil
	}
	tenantID := strings.TrimSpace(lens.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	contextRefs := buildSupplyChainLensContext(lens)
	selected := make(map[string]*horizonmodels.MaterializedArtifact, limit*2)
	scopedIDs := make(map[string]struct{}, limit)

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
		if pinned {
			scopedIDs[copyArtifact.ID] = struct{}{}
		}
	}

	legacyScoped, err := mat.ListByLens(ctx, lens.ID, limit)
	if err != nil {
		return nil, nil, err
	}
	for _, artifact := range legacyScoped {
		appendArtifact(artifact, true, "scoped", nil, 1)
	}
	for _, artKey := range contextRefs.ArtifactKeys {
		artifact, err := mat.GetByCanonicalKeyAnyKind(ctx, tenantID, artKey)
		if err == nil && artifact != nil {
			if artifact.Kind == horizonmodels.MaterializedKindSupplyChainAssemblyGraph {
				pinnedPathKeys := ExtractPreferredAssemblyPathKeys(artifact)
				for _, pathKey := range pinnedPathKeys {
					pathArtifact, err := mat.GetByCanonicalKeyAnyKind(ctx, tenantID, pathKey)
					if err == nil && pathArtifact != nil {
						appendArtifact(pathArtifact, true, "scoped", nil, 1)
					}
				}
				continue
			}
			appendArtifact(artifact, true, "scoped", nil, 1)
		}
	}

	discoveredLimit := limit * 2
	discoveredQueries := []struct {
		refs   []string
		source string
		find   func([]string) ([]*horizonmodels.MaterializedArtifact, error)
	}{
		{
			refs:   contextRefs.NodeRefs,
			source: "node_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return mat.FindByNodeRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.FlowRefs,
			source: "flow_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return mat.FindByFlowRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.ZoneRefs,
			source: "zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return mat.FindByZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.AffectedZoneRefs,
			source: "affected_zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return mat.FindByAffectedZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
	}

	if len(scopedIDs) == 0 {
		for _, query := range discoveredQueries {
			if len(query.refs) == 0 {
				continue
			}
			artifacts, err := query.find(query.refs)
			if err != nil {
				return nil, nil, err
			}
			for _, artifact := range artifacts {
				score, matched := scoreSupplyChainOverlap(artifact, contextRefs)
				if score <= 0 {
					continue
				}
				appendArtifact(artifact, false, query.source, matched, score)
			}
		}
	}

	scoped := make([]*horizonmodels.MaterializedArtifact, 0, len(scopedIDs))
	discovered := make([]*horizonmodels.MaterializedArtifact, 0, len(selected))
	for _, artifact := range selected {
		if _, ok := scopedIDs[artifact.ID]; ok {
			scoped = append(scoped, artifact)
			continue
		}
		discovered = append(discovered, artifact)
	}
	if len(scoped) > 0 {
		sortSupplyChainArtifacts(scoped)
		if len(scoped) > limit {
			scoped = scoped[:limit]
		}
		return scoped, nil, nil
	}
	sortSupplyChainArtifacts(scoped)
	sortSupplyChainArtifacts(discovered)
	if len(scoped) > limit {
		scoped = scoped[:limit]
	}
	remaining := limit - len(scoped)
	if remaining < 0 {
		remaining = 0
	}
	if len(discovered) > remaining {
		discovered = discovered[:remaining]
	}
	return scoped, discovered, nil
}
