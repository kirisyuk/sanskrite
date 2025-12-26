package scout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// GeoHierarchyBuilder best-effort helper that ensures intermediate /loc/* GEO_REGION fragments exist
// and links them with LOCATED_IN edges (child -> parent).
//
// It is intentionally conservative: failures must not break Scout job completion.
type GeoHierarchyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

func NewGeoHierarchyBuilder(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *GeoHierarchyBuilder {
	return &GeoHierarchyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// EnsureAndLinkGeoHierarchy creates missing parent geo fragments and links:
//   /loc/a/b/c  -> /loc/a/b  -> /loc/a
//
// provenanceMethod/sourceID are written into edge provenance/qualifiers when provided.
func (b *GeoHierarchyBuilder) EnsureAndLinkGeoHierarchy(
	ctx context.Context,
	leafFragmentID string,
	leafCanonicalPath string,
	provenanceMethod string,
	provenanceSourceID string,
) error {
	if b == nil || b.fragmentsRepo == nil || b.edgesRepo == nil {
		return nil
	}
	leafCanonicalPath = strings.TrimSpace(leafCanonicalPath)
	if leafFragmentID == "" || leafCanonicalPath == "" {
		return nil
	}
	if !strings.HasPrefix(leafCanonicalPath, "/loc/") {
		return nil
	}

	// Build parent chain by canonicalPath prefixes.
	trimmed := strings.Trim(strings.TrimPrefix(leafCanonicalPath, "/loc/"), "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		// Nothing to link (e.g., /loc/us)
		return nil
	}

	// Ensure all parent fragments exist and capture their IDs.
	parentIDs := make(map[string]string, len(parts))
	for i := 1; i < len(parts); i++ {
		parentPath := "/loc/" + strings.Join(parts[:i], "/")
		parentPath = strings.TrimSpace(parentPath)
		if parentPath == "" {
			continue
		}

		existing, err := b.fragmentsRepo.GetByCanonicalPath(ctx, parentPath)
		if err == nil && existing != nil && existing.ID != "" {
			parentIDs[parentPath] = existing.ID
			continue
		}
		if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
			// Unexpected error: do not fail pipeline.
			return nil
		}

		// Create a minimal GEO_REGION fragment.
		label := parts[i-1]
		label = strings.ReplaceAll(label, "-", " ")
		f := &models.HorizonFragment{
			CanonicalPath: parentPath,
			Type:          models.FragmentTypeGeoRegion,
			Labels: map[string]string{
				"en": strings.TrimSpace(label),
			},
			Status: string(models.FragmentStatusActive),
			State:  map[string]models.MetricValue{},
			Governance: models.Governance{
				Owner: "SYSTEM",
				// Status will be defaulted by repo on create; still set to keep structData consistent.
				Status: string(models.FragmentStatusActive),
			},
			Metadata: models.FragmentMetadata{
				CreatedAt:      time.Now().UTC(),
				LastUpdatedAt:  time.Now().UTC(),
				LastVerifiedAt: time.Now().UTC(),
				EpistemicStatus: models.EpistemicStatusLowConfidence,
			},
		}
		id, err := b.fragmentsRepo.Create(ctx, f)
		if err != nil {
			// Best-effort only.
			return nil
		}
		parentIDs[parentPath] = id
	}

	// Link child -> parent edges using LOCATED_IN.
	// Start from leaf, then each parent to its parent.
	childID := leafFragmentID
	childPath := leafCanonicalPath
	for i := len(parts) - 1; i >= 1; i-- {
		parentPath := "/loc/" + strings.Join(parts[:i], "/")
		parentID := strings.TrimSpace(parentIDs[parentPath])
		if parentID == "" || childID == "" {
			break
		}

		// Skip if edge already exists.
		if _, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", childID, parentID, models.DependencyTypeLocatedIn); err == nil {
			// already linked
		} else if errors.Is(err, repo.ErrEdgeNotFound) {
			edge := &models.DependencyEdge{
				TenantID:         "global",
				SourceFragmentID: childID,
				TargetFragmentID: parentID,
				Type:             models.DependencyTypeLocatedIn,
				Properties: models.EdgeProperties{
					Criticality:   0.6,
					Amplification: 1.0,
					LatencyHours:  24,
				},
				Qualifiers: map[string]interface{}{
					"reason":              "geo.hierarchy",
					"sourceCanonicalPath": strings.TrimSpace(childPath),
					"targetCanonicalPath": strings.TrimSpace(parentPath),
				},
				Provenance: models.Provenance{
					Method:    strings.TrimSpace(provenanceMethod),
					SourceIDs: func() []string { if strings.TrimSpace(provenanceSourceID) == "" { return nil }; return []string{strings.TrimSpace(provenanceSourceID)} }(),
				},
				Metadata: models.EdgeMetadata{
					CreatedAt:     time.Now().UTC(),
					LastUpdatedAt: time.Now().UTC(),
				},
			}
			_, _ = b.edgesRepo.Create(ctx, edge) // best-effort
		} else {
			// Unexpected read error; best-effort only.
			return nil
		}

		// Move up the chain.
		childID = parentID
		childPath = parentPath
	}

	return nil
}

func (b *GeoHierarchyBuilder) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("GeoHierarchyBuilder(repo=%v, edges=%v)", b.fragmentsRepo != nil, b.edgesRepo != nil)
}


