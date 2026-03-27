// Package workscope defines WorkScope as the universal focus-of-work input for Horizon layers
// (graph enrichment, materialized paths, operational hydration, signal propagation, simulation).
package workscope

import (
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// WorkScope is the canonical focus for pipeline stages. TenantID and ProjectID are execution
// context; Refs matches persisted SimulationBaselineRefs for API/storage compatibility.
type WorkScope struct {
	TenantID  string
	ProjectID string
	Refs      horizonmodels.SimulationBaselineRefs
	Intent    WorkScopeIntent
}

// FromBaselineRefs builds a WorkScope from API-shaped baseline refs plus tenant/project context.
func FromBaselineRefs(tenantID, projectID string, refs horizonmodels.SimulationBaselineRefs) WorkScope {
	return Normalize(WorkScope{
		TenantID:  tenantID,
		ProjectID: projectID,
		Refs:      refs,
	})
}

// ToBaselineRefs returns refs suitable for persistence or JSON responses.
func ToBaselineRefs(s WorkScope) horizonmodels.SimulationBaselineRefs {
	return Normalize(s).Refs
}

// Normalize trims strings, drops empties from slices, deduplicates, and sorts for stable ordering.
func Normalize(s WorkScope) WorkScope {
	out := WorkScope{
		TenantID:  strings.TrimSpace(s.TenantID),
		ProjectID: strings.TrimSpace(s.ProjectID),
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:         strings.TrimSpace(s.Refs.LensID),
			SnapshotDigest: strings.TrimSpace(s.Refs.SnapshotDigest),
			ScopePaths:     normalizeStringSlice(s.Refs.ScopePaths),
			FragmentIDs:    normalizeStringSlice(s.Refs.FragmentIDs),
			FragmentPaths:  normalizeStringSlice(s.Refs.FragmentPaths),
			EdgeIDs:        normalizeStringSlice(s.Refs.EdgeIDs),
			FactIDs:        normalizeStringSlice(s.Refs.FactIDs),
			ArtifactKeys:   normalizeStringSlice(s.Refs.ArtifactKeys),
			ArtifactIDs:    normalizeStringSlice(s.Refs.ArtifactIDs),
		},
		Intent: normalizeIntent(s.Intent),
	}
	return out
}

// Merge combines two scopes: context fields prefer a when non-empty; ref slices are unioned and normalized.
func Merge(a, b WorkScope) WorkScope {
	tenant := strings.TrimSpace(a.TenantID)
	if tenant == "" {
		tenant = strings.TrimSpace(b.TenantID)
	}
	project := strings.TrimSpace(a.ProjectID)
	if project == "" {
		project = strings.TrimSpace(b.ProjectID)
	}
	merged := WorkScope{
		TenantID:  tenant,
		ProjectID: project,
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:         firstNonEmpty(strings.TrimSpace(a.Refs.LensID), strings.TrimSpace(b.Refs.LensID)),
			ScopePaths:     mergeStringSlices(a.Refs.ScopePaths, b.Refs.ScopePaths),
			FragmentIDs:    mergeStringSlices(a.Refs.FragmentIDs, b.Refs.FragmentIDs),
			FragmentPaths:  mergeStringSlices(a.Refs.FragmentPaths, b.Refs.FragmentPaths),
			EdgeIDs:        mergeStringSlices(a.Refs.EdgeIDs, b.Refs.EdgeIDs),
			FactIDs:        mergeStringSlices(a.Refs.FactIDs, b.Refs.FactIDs),
			ArtifactKeys:   mergeStringSlices(a.Refs.ArtifactKeys, b.Refs.ArtifactKeys),
			ArtifactIDs:    mergeStringSlices(a.Refs.ArtifactIDs, b.Refs.ArtifactIDs),
			SnapshotDigest: firstNonEmpty(strings.TrimSpace(a.Refs.SnapshotDigest), strings.TrimSpace(b.Refs.SnapshotDigest)),
		},
		Intent: mergeIntent(a.Intent, b.Intent),
	}
	return Normalize(merged)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func normalizeStringSlice(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func mergeStringSlices(a, b []string) []string {
	return normalizeStringSlice(append(append([]string{}, a...), b...))
}
