package workscope

import (
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// LensWorkbenchScopePaths is the unified path set derived from a lens for simulation, APIs, and supply-chain context:
// query-expanded scope entries, deferred/view-derived paths, supply-chain anchors from domain queries, and exposure paths.
func LensWorkbenchScopePaths(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	out := LensScopePathsWithResolvedQueries(lens)
	out = append(out, lensExtractDeferredScopePaths(lens)...)
	out = append(out, lensExtractViewDirectivePaths(lens)...)
	out = append(out, lensExtractSupplyChainAnchors(lens)...)
	for _, p := range lens.ExposurePaths {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return lensCompactStringsPreserveOrder(out)
}

// LensResearchObjectPaths returns the persisted wide object set recovered for the lens.
// This is the exact stored researchObjects surface, filtered to canonical path-like refs.
func LensResearchObjectPaths(lens *horizonmodels.Lens) []string {
	if lens == nil || len(lens.ResearchObjects) == 0 {
		return nil
	}
	keys := make([]string, 0, len(lens.ResearchObjects))
	for key, item := range lens.ResearchObjects {
		path := lensFirstNonEmpty(item.CanonicalPath, key)
		if lensIsResearchObjectPath(path) {
			keys = append(keys, strings.TrimSpace(path))
		}
	}
	sort.Strings(keys)
	return lensCompactStringsPreserveOrder(keys)
}

// LensResearchUniversePaths returns the broadest lens-local path set available without graph traversal:
// current workbench scope plus persisted research object refs.
func LensResearchUniversePaths(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	out := LensWorkbenchScopePaths(lens)
	out = append(out, LensResearchObjectPaths(lens)...)
	return lensCompactStringsPreserveOrder(out)
}

// LensCountryHintPaths merges workbench paths with every path from ResolvedQueries values (parity with map activity).
func LensCountryHintPaths(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	out := LensWorkbenchScopePaths(lens)
	if lens.ResolvedQueries != nil {
		for _, arr := range lens.ResolvedQueries {
			for _, p := range arr {
				if s := strings.TrimSpace(p); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return lensCompactStringsPreserveOrder(out)
}

// LensSupplyChainNodeRefs returns node-oriented refs for supply-chain overlap scoring (parity with former collectLensScopeNodeRefs).
func LensSupplyChainNodeRefs(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	refs := make([]string, 0, len(lens.Scope)+len(lens.ScopeFragments))
	for _, entry := range lens.Scope {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		switch {
		case strings.HasPrefix(entry, horizonmodels.QueryScopePrefix):
			if lens.ResolvedQueries != nil {
				refs = lensMergeUniqueSorted(refs, lens.ResolvedQueries[entry])
			}
		case strings.HasPrefix(entry, horizonmodels.ArtifactScopePrefix):
			continue
		case strings.HasPrefix(entry, horizonmodels.DomainScopePrefix):
			continue
		case strings.HasPrefix(entry, "/"):
			refs = lensMergeUniqueSorted(refs, []string{entry})
		}
	}
	for canonicalPath := range lens.ScopeFragments {
		if s := strings.TrimSpace(canonicalPath); s != "" {
			refs = lensMergeUniqueSorted(refs, []string{s})
		}
	}
	if lens.ResolvedQueries != nil {
		for _, paths := range lens.ResolvedQueries {
			refs = lensMergeUniqueSorted(refs, paths)
		}
	}
	return refs
}

func lensMergeUniqueSorted(dst, values []string) []string {
	if len(values) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, value := range dst {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func lensExtractDeferredScopePaths(lens *horizonmodels.Lens) []string {
	if lens == nil || len(lens.ViewConfig) == 0 {
		return nil
	}
	return lensCompactStringsPreserveOrder(lensStringSliceFromAny(lens.ViewConfig["deferredScopePaths"]))
}

func lensExtractViewDirectivePaths(lens *horizonmodels.Lens) []string {
	if lens == nil || len(lens.ViewConfig) == 0 {
		return nil
	}
	return lensCompactStringsPreserveOrder(lensCollectViewDirectivePathsRecursive(lens.ViewConfig))
}

func lensExtractSupplyChainAnchors(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	paths := make([]string, 0, 32)
	for _, query := range lens.DomainQueries {
		if query.SupplyChainExecutionPlan != nil {
			for _, item := range query.SupplyChainExecutionPlan.Queries {
				if lensIsCanonicalScopePath(item.Anchor) {
					paths = append(paths, strings.TrimSpace(item.Anchor))
				}
				for _, anchor := range lensCompactStringsPreserveOrder(item.Anchors) {
					if lensIsCanonicalScopePath(anchor) {
						paths = append(paths, anchor)
					}
				}
			}
			for _, overlay := range query.SupplyChainExecutionPlan.OverlayQueries {
				for _, anchor := range lensCompactStringsPreserveOrder(overlay.Anchors) {
					if lensIsCanonicalScopePath(anchor) {
						paths = append(paths, anchor)
					}
				}
			}
		}
		for _, entity := range lensCompactStringsPreserveOrder(query.FirstOrderEntities) {
			if lensIsCanonicalScopePath(entity) {
				paths = append(paths, entity)
			}
		}
	}
	return lensCompactStringsPreserveOrder(paths)
}

func lensCollectViewDirectivePathsRecursive(raw map[string]any) []string {
	if len(raw) == 0 {
		return nil
	}
	paths := make([]string, 0, 32)
	for key, value := range raw {
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "focusnodepaths":
			paths = append(paths, lensStringSliceFromAny(value)...)
		case "highlightpaths":
			if items, ok := value.([]any); ok {
				for _, item := range items {
					nodeMap := lensMapValue(item)
					paths = append(paths, lensStringSliceFromAny(nodeMap["nodePaths"])...)
					paths = append(paths, lensStringSliceFromAny(nodeMap["NodePaths"])...)
				}
			}
		case "researchplan":
			researchPlan := lensMapValue(value)
			paths = append(paths, lensStringSliceFromAny(researchPlan["missingNodes"])...)
			paths = append(paths, lensStringSliceFromAny(researchPlan["MissingNodes"])...)
			if missingEdges, ok := researchPlan["missingEdges"].([]any); ok {
				for _, rawEdge := range missingEdges {
					item := lensMapValue(rawEdge)
					paths = append(paths, lensFirstNonEmpty(lensAnyString(item["fromPath"]), lensAnyString(item["FromPath"])))
					paths = append(paths, lensFirstNonEmpty(lensAnyString(item["toPath"]), lensAnyString(item["ToPath"])))
				}
			}
			if missingEdges, ok := researchPlan["MissingEdges"].([]any); ok {
				for _, rawEdge := range missingEdges {
					item := lensMapValue(rawEdge)
					paths = append(paths, lensFirstNonEmpty(lensAnyString(item["fromPath"]), lensAnyString(item["FromPath"])))
					paths = append(paths, lensFirstNonEmpty(lensAnyString(item["toPath"]), lensAnyString(item["ToPath"])))
				}
			}
		}
		switch typed := value.(type) {
		case map[string]any:
			paths = append(paths, lensCollectViewDirectivePathsRecursive(typed)...)
		case []any:
			for _, item := range typed {
				if nested := lensMapValue(item); len(nested) > 0 {
					paths = append(paths, lensCollectViewDirectivePathsRecursive(nested)...)
				}
			}
		}
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range lensCompactStringsPreserveOrder(paths) {
		if lensIsCanonicalScopePath(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func lensStringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return lensCompactStringsPreserveOrder(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := lensAnyString(item); value != "" {
				out = append(out, value)
			}
		}
		return lensCompactStringsPreserveOrder(out)
	default:
		return nil
	}
}

func lensMapValue(raw any) map[string]any {
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func lensCompactStringsPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func lensAnyString(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func lensIsCanonicalScopePath(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "/")
}

func lensIsResearchObjectPath(value string) bool {
	value = strings.TrimSpace(value)
	if !lensIsCanonicalScopePath(value) {
		return false
	}
	switch {
	case strings.HasPrefix(value, horizonmodels.QueryScopePrefix),
		strings.HasPrefix(value, horizonmodels.DomainScopePrefix),
		strings.HasPrefix(value, horizonmodels.ArtifactScopePrefix),
		strings.HasPrefix(value, "/exposure/"):
		return false
	default:
		return true
	}
}

func lensFirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
