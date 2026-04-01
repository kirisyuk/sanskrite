package supplychainreport

import (
	"context"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

type MaterializedArtifactsPort interface {
	workscope.MaterializedArtifactsPort
	GetByCanonicalKeyAnyKind(ctx context.Context, tenantID string, canonicalKey string) (*horizonmodels.MaterializedArtifact, error)
	FindByNodeRefsAny(ctx context.Context, tenantID string, nodeCanonicalPaths []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByFlowRefsAny(ctx context.Context, tenantID string, flowRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByZoneRefsAny(ctx context.Context, tenantID string, zoneRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
}

type FragmentsPort interface {
	workscope.MapLensFragmentsPort
}

type EdgesPort interface {
	workscope.MapLensEdgesPort
}

type LensContext struct {
	NodeRefs         []string
	FlowRefs         []string
	ZoneRefs         []string
	AffectedZoneRefs []string
	ArtifactKeys     []string
}

type Input struct {
	Lens               *horizonmodels.Lens
	ScopePaths         []string
	WorkScope          workscope.WorkScope
	Resolved           workscope.ResolvedWorkSet
	AllMaterialized    []*horizonmodels.MaterializedArtifact
	Chains             []*horizonmodels.MaterializedArtifact
	ScopedChainCount   int
	DiscoveredChainCnt int
	FragmentsByPath    map[string]*horizonmodels.HorizonFragment
	CreatedAt          time.Time
}

func CollectInput(
	ctx context.Context,
	lens *horizonmodels.Lens,
	edgeTenantID string,
	materialized MaterializedArtifactsPort,
	fragments FragmentsPort,
	edges EdgesPort,
	limit int,
) (*Input, error) {
	if lens == nil || materialized == nil || fragments == nil || edges == nil {
		return nil, ErrInvalidCollectionDeps
	}
	if limit <= 0 {
		limit = 160
	}

	scopePaths := workscope.LensWorkbenchScopePaths(lens)
	workScope, err := workscope.ResolveLensMapWorkScope(ctx, lens, edgeTenantID, scopePaths, fragments, edges)
	if err != nil {
		return nil, err
	}
	resolved := workscope.ResolvedFromScope(workScope)

	scopedChains, discoveredChains, err := collectSupplyChainsForLens(ctx, lens, materialized, limit)
	if err != nil {
		return nil, err
	}
	chains := dedupeSupplyChainArtifacts(append(append([]*horizonmodels.MaterializedArtifact{}, scopedChains...), discoveredChains...))
	refs := make([]string, 0, len(resolved.ScopePaths)+len(chains)*8)
	refs = append(refs, resolved.ScopePaths...)
	for _, chain := range chains {
		if chain == nil {
			continue
		}
		horizonmodels.PopulateSupplyChainPathIndexes(chain)
		refs = append(refs, chain.NodeRefs...)
		refs = append(refs, chain.ZoneRefs...)
		refs = append(refs, chain.AffectedZoneRefs...)
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(chain.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			refs = append(refs, leg.TransportViaRefs...)
			refs = append(refs, leg.ZoneRefs...)
			refs = append(refs, leg.AffectedZoneRefs...)
		}
	}
	refs = uniqueTrimmedStrings(refs)

	fragmentsByPath := map[string]*horizonmodels.HorizonFragment{}
	if len(refs) > 0 {
		items, err := fragments.ListByCanonicalPaths(ctx, refs)
		if err == nil {
			for _, fragment := range items {
				if fragment == nil {
					continue
				}
				ref := strings.TrimSpace(fragment.CanonicalPath)
				if ref == "" {
					continue
				}
				fragmentsByPath[ref] = fragment
			}
		}
	}

	return &Input{
		Lens:               lens,
		ScopePaths:         scopePaths,
		WorkScope:          workScope,
		Resolved:           resolved,
		AllMaterialized:    nil,
		Chains:             chains,
		ScopedChainCount:   len(scopedChains),
		DiscoveredChainCnt: len(discoveredChains),
		FragmentsByPath:    fragmentsByPath,
		CreatedAt:          time.Now().UTC(),
	}, nil
}

var ErrInvalidCollectionDeps = errString("supply-chain report collection dependencies are not configured")

type errString string

func (e errString) Error() string { return string(e) }

func collectSupplyChainsForLens(
	ctx context.Context,
	lens *horizonmodels.Lens,
	materialized MaterializedArtifactsPort,
	limit int,
) ([]*horizonmodels.MaterializedArtifact, []*horizonmodels.MaterializedArtifact, error) {
	tenantID := strings.TrimSpace(lens.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	contextRefs := buildLensContext(lens)
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
					existing.DiscoveryReason = describeDiscovery(source, matched)
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
			copyArtifact.DiscoveryReason = describeDiscovery(source, matched)
		}
		selected[copyArtifact.ID] = &copyArtifact
		if pinned {
			scopedIDs[copyArtifact.ID] = struct{}{}
		}
	}

	legacyScoped, err := materialized.ListByLens(ctx, lens.ID, limit)
	if err != nil {
		return nil, nil, err
	}
	for _, artifact := range legacyScoped {
		appendArtifact(artifact, true, "scoped", nil, 1)
	}
	for _, artKey := range contextRefs.ArtifactKeys {
		artifact, err := materialized.GetByCanonicalKeyAnyKind(ctx, tenantID, artKey)
		if err != nil || artifact == nil {
			continue
		}
		if artifact.Kind == horizonmodels.MaterializedKindSupplyChainAssemblyGraph {
			for _, pathKey := range extractPreferredAssemblyPathKeys(artifact) {
				pathArtifact, err := materialized.GetByCanonicalKeyAnyKind(ctx, tenantID, pathKey)
				if err == nil && pathArtifact != nil {
					appendArtifact(pathArtifact, true, "scoped", nil, 1)
				}
			}
			continue
		}
		appendArtifact(artifact, true, "scoped", nil, 1)
	}

	discoveredLimit := limit * 2
	queries := []struct {
		refs   []string
		source string
		find   func([]string) ([]*horizonmodels.MaterializedArtifact, error)
	}{
		{
			refs:   contextRefs.NodeRefs,
			source: "node_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return materialized.FindByNodeRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.FlowRefs,
			source: "flow_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return materialized.FindByFlowRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.ZoneRefs,
			source: "zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return materialized.FindByZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.AffectedZoneRefs,
			source: "affected_zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return materialized.FindByAffectedZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
	}
	if len(scopedIDs) == 0 {
		for _, query := range queries {
			if len(query.refs) == 0 {
				continue
			}
			artifacts, err := query.find(query.refs)
			if err != nil {
				return nil, nil, err
			}
			for _, artifact := range artifacts {
				score, matched := scoreOverlap(artifact, contextRefs)
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

func buildLensContext(lens *horizonmodels.Lens) LensContext {
	out := LensContext{}
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

func extractPreferredAssemblyPathKeys(artifact *horizonmodels.MaterializedArtifact) []string {
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

func scoreOverlap(artifact *horizonmodels.MaterializedArtifact, ctx LensContext) (float64, []string) {
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

func describeDiscovery(source string, matched []string) string {
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

func intersectCount(left []string, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	index := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			index[value] = struct{}{}
		}
	}
	count := 0
	seen := map[string]struct{}{}
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, done := seen[value]; done {
			continue
		}
		seen[value] = struct{}{}
		if _, ok := index[value]; ok {
			count++
		}
	}
	return count
}

func firstIntersect(left []string, right []string, limit int) []string {
	if len(left) == 0 || len(right) == 0 || limit <= 0 {
		return nil
	}
	index := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			index[value] = struct{}{}
		}
	}
	out := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, done := seen[value]; done {
			continue
		}
		seen[value] = struct{}{}
		if _, ok := index[value]; ok {
			out = append(out, value)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func dedupeSupplyChainArtifacts(artifacts []*horizonmodels.MaterializedArtifact) []*horizonmodels.MaterializedArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(artifacts))
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		key := strings.TrimSpace(artifact.ID)
		if key == "" {
			key = strings.TrimSpace(artifact.CanonicalKey)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		horizonmodels.PopulateSupplyChainPathIndexes(artifact)
		out = append(out, artifact)
	}
	sortSupplyChainArtifacts(out)
	return out
}

func sortSupplyChainArtifacts(artifacts []*horizonmodels.MaterializedArtifact) {
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i] == nil || artifacts[j] == nil {
			return artifacts[j] != nil
		}
		if artifacts[i].IsPinned != artifacts[j].IsPinned {
			return artifacts[i].IsPinned
		}
		if artifacts[i].OverlapScore != artifacts[j].OverlapScore {
			return artifacts[i].OverlapScore > artifacts[j].OverlapScore
		}
		leftTime := artifacts[i].UpdatedAt
		rightTime := artifacts[j].UpdatedAt
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		left := strings.TrimSpace(firstNonEmpty(artifacts[i].DisplayName, artifacts[i].CanonicalKey, artifacts[i].ID))
		right := strings.TrimSpace(firstNonEmpty(artifacts[j].DisplayName, artifacts[j].CanonicalKey, artifacts[j].ID))
		return left < right
	})
}
