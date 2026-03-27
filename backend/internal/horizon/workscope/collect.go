package workscope

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
)

// CollectMaterializedArtifacts loads artifacts by ResolvedWorkSet.MaterializedArtifactIDs and applies
// the same simscope profile filter used by operational hydration and simulation baselines.
func CollectMaterializedArtifacts(ctx context.Context, m MaterializedArtifactsPort, resolved ResolvedWorkSet, profile simscope.Profile) []*horizonmodels.MaterializedArtifact {
	if m == nil {
		return nil
	}
	out := make([]*horizonmodels.MaterializedArtifact, 0, max(64, len(resolved.MaterializedArtifactIDs)))
	seen := map[string]struct{}{}
	appendArtifact := func(artifact *horizonmodels.MaterializedArtifact) {
		if artifact == nil {
			return
		}
		key := strings.TrimSpace(artifact.ID)
		if key == "" {
			key = strings.TrimSpace(artifact.CanonicalKey)
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, artifact)
	}
	for _, artifactID := range resolved.MaterializedArtifactIDs {
		artifactID = strings.TrimSpace(artifactID)
		if artifactID == "" {
			continue
		}
		artifact, err := m.Get(ctx, artifactID)
		if err == nil && artifact != nil {
			appendArtifact(artifact)
		}
	}
	if len(profile.FocusRefs) == 0 && len(profile.DomainProductTokens) == 0 {
		return out
	}
	return simscope.FilterArtifacts(out, profile)
}
