package repo

import (
	"context"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/govdict"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/mechanismdict"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// WithMechanismDictionary enables /mechanism/ path canonicalization in materialized artifact payloads.
func (r *MaterializedArtifactsRepository) WithMechanismDictionary(dict *mechanismdict.Runtime) *MaterializedArtifactsRepository {
	if r == nil {
		return r
	}
	r.mechanismDictionary = dict
	return r
}

// WithGovDictionary enables /gov/ path canonicalization in materialized artifact payloads.
func (r *MaterializedArtifactsRepository) WithGovDictionary(dict *govdict.Runtime) *MaterializedArtifactsRepository {
	if r == nil {
		return r
	}
	r.govDictionary = dict
	return r
}

func (r *MaterializedArtifactsRepository) normalizeMechanismGovPathsInArtifact(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) (bool, error) {
	if r == nil || artifact == nil {
		return false, nil
	}
	if r.mechanismDictionary == nil && r.govDictionary == nil {
		return false, nil
	}
	candidates := collectMechanismGovPathCandidates(artifact)
	if len(candidates) == 0 {
		return false, nil
	}
	sort.Strings(candidates)
	replacements := map[string]string{}
	for _, path := range candidates {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "/mechanism/") && r.mechanismDictionary != nil {
			canonical, matched, err := r.mechanismDictionary.CanonicalizeMechanismPath(ctx, path)
			if err != nil {
				return false, err
			}
			if matched && canonical != "" && canonical != path {
				replacements[path] = canonical
			}
			continue
		}
		if strings.HasPrefix(path, "/gov/") && r.govDictionary != nil {
			canonical, matched, err := r.govDictionary.CanonicalizeGovPath(ctx, path)
			if err != nil {
				return false, err
			}
			if matched && canonical != "" && canonical != path {
				replacements[path] = canonical
			}
		}
	}
	if len(replacements) == 0 {
		return false, nil
	}
	keys := make([]string, 0, len(replacements))
	for old := range replacements {
		keys = append(keys, old)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	changed := false
	for _, oldPath := range keys {
		newPath := replacements[oldPath]
		if horizonmodels.ReplaceCanonicalPathInArtifact(artifact, oldPath, newPath) {
			changed = true
		}
	}
	if changed {
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	}
	return changed, nil
}

func collectMechanismGovPathCandidates(a *horizonmodels.MaterializedArtifact) []string {
	if a == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if strings.HasPrefix(s, "/mechanism/") || strings.HasPrefix(s, "/gov/") {
			seen[s] = struct{}{}
		}
	}
	for _, ref := range a.NodeRefs {
		add(ref)
	}
	add(a.ProductAxis)
	if a.Labels != nil {
		for _, k := range []string{"rootEntity", "anchorSupplier", "anchorCustomer", "productAxis", "geoFrom", "geoTo"} {
			add(a.Labels[k])
		}
	}
	switch a.Kind {
	case horizonmodels.MaterializedKindRegulatoryImpactMap, horizonmodels.MaterializedKindRegulatoryImpactTransmissionPath,
		horizonmodels.MaterializedKindRegulatoryTimeline, horizonmodels.MaterializedKindRegulatoryCascadeMap,
		horizonmodels.MaterializedKindRegulatoryImpactScenario,
		horizonmodels.MaterializedKindGovernanceChain, horizonmodels.MaterializedKindEcosystemMap:
		if parsed, err := horizonmodels.ParseRelationsGraphPayload(a.Payload); err == nil && parsed != nil {
			add(parsed.RootEntity)
			for _, n := range parsed.Nodes {
				add(n.Ref)
			}
		}
	case horizonmodels.MaterializedKindSupplyChainPath, horizonmodels.MaterializedKindServiceChainPath:
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload); err == nil && parsed != nil {
			for _, n := range parsed.Nodes {
				add(n.Ref)
			}
			for _, e := range parsed.Edges {
				add(e.ProductRef)
				add(e.SupplyLinkCanonicalPath)
			}
			for _, leg := range parsed.Legs {
				add(leg.ProductRef)
				add(leg.SupplierRef)
				add(leg.CustomerRef)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}
