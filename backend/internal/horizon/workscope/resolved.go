package workscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// ResolvedWorkSet is a deterministic expansion of WorkScope for layer inputs and idempotency.
// MaterializedArtifactIDs unions explicit artifact IDs with IDs from ListByLens when LensID is set.
type ResolvedWorkSet struct {
	TenantID                string   `json:"tenantId,omitempty"`
	ProjectID               string   `json:"projectId,omitempty"`
	LensID                  string   `json:"lensId,omitempty"`
	ScopePaths              []string `json:"scopePaths,omitempty"`
	FragmentIDs             []string `json:"fragmentIds,omitempty"`
	FragmentPaths           []string `json:"fragmentPaths,omitempty"`
	EdgeIDs                 []string `json:"edgeIds,omitempty"`
	FactIDs                 []string `json:"factIds,omitempty"`
	ArtifactKeys            []string `json:"artifactKeys,omitempty"`
	MaterializedArtifactIDs []string `json:"materializedArtifactIds,omitempty"`
	SnapshotDigest          string   `json:"snapshotDigest,omitempty"`
}

// ResolvedFromScope copies normalized ref fields; MaterializedArtifactIDs starts as explicit ArtifactIDs from refs (no ListByLens).
func ResolvedFromScope(s WorkScope) ResolvedWorkSet {
	s = Normalize(s)
	return ResolvedWorkSet{
		TenantID:                s.TenantID,
		ProjectID:               s.ProjectID,
		LensID:                  s.Refs.LensID,
		ScopePaths:              append([]string{}, s.Refs.ScopePaths...),
		FragmentIDs:             append([]string{}, s.Refs.FragmentIDs...),
		FragmentPaths:           append([]string{}, s.Refs.FragmentPaths...),
		EdgeIDs:                 append([]string{}, s.Refs.EdgeIDs...),
		FactIDs:                 append([]string{}, s.Refs.FactIDs...),
		ArtifactKeys:            append([]string{}, s.Refs.ArtifactKeys...),
		MaterializedArtifactIDs: append([]string{}, s.Refs.ArtifactIDs...),
		SnapshotDigest:          s.Refs.SnapshotDigest,
	}
}

// Digest returns a stable SHA-256 hex digest for idempotency keys (canonical JSON).
func Digest(r ResolvedWorkSet) string {
	b, err := json.Marshal(canonicalResolved(r))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canonicalResolved(r ResolvedWorkSet) resolvedCanonical {
	return resolvedCanonical{
		TenantID:                r.TenantID,
		ProjectID:               r.ProjectID,
		LensID:                  r.LensID,
		ScopePaths:              sortedCopy(r.ScopePaths),
		FragmentIDs:             sortedCopy(r.FragmentIDs),
		FragmentPaths:           sortedCopy(r.FragmentPaths),
		EdgeIDs:                 sortedCopy(r.EdgeIDs),
		FactIDs:                 sortedCopy(r.FactIDs),
		ArtifactKeys:            sortedCopy(r.ArtifactKeys),
		MaterializedArtifactIDs: sortedCopy(r.MaterializedArtifactIDs),
		SnapshotDigest:          r.SnapshotDigest,
	}
}

type resolvedCanonical struct {
	TenantID                string   `json:"tenantId,omitempty"`
	ProjectID               string   `json:"projectId,omitempty"`
	LensID                  string   `json:"lensId,omitempty"`
	ScopePaths              []string `json:"scopePaths,omitempty"`
	FragmentIDs             []string `json:"fragmentIds,omitempty"`
	FragmentPaths           []string `json:"fragmentPaths,omitempty"`
	EdgeIDs                 []string `json:"edgeIds,omitempty"`
	FactIDs                 []string `json:"factIds,omitempty"`
	ArtifactKeys            []string `json:"artifactKeys,omitempty"`
	MaterializedArtifactIDs []string `json:"materializedArtifactIds,omitempty"`
	SnapshotDigest          string   `json:"snapshotDigest,omitempty"`
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
