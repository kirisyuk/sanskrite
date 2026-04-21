package scout

import (
	"context"
	"errors"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"time"
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
	// Early canonicalization: operate on normalized variable-depth /loc shape.
	if norm := normalizeGeoPathToStrict(leafCanonicalPath); norm != "" {
		leafCanonicalPath = norm
	}
	parts := strings.Split(strings.Trim(leafCanonicalPath, "/"), "/")
	if len(parts) < 2 || len(parts) > 4 || parts[0] != "loc" {
		return nil
	}
	if strings.TrimSpace(parts[1]) == "" {
		return nil
	}

	// Build parent chain by progressively trimming one geo level:
	// /loc/a/b/c -> /loc/a/b -> /loc/a
	parents := make([]string, 0, 2)
	for i := len(parts) - 1; i >= 2; i-- {
		parent := "/" + strings.Join(parts[:i], "/")
		parents = append(parents, parent)
	}
	if len(parents) == 0 {
		// Country-level node has no parent.
		return nil
	}

	// Ensure all parent fragments exist and capture their IDs.
	parentIDs := make(map[string]string, len(parents))
	for _, parentPathRaw := range parents {
		parentPath := strings.TrimSpace(parentPathRaw)
		if parentPath == "" {
			continue
		}
		// Reuse common canonicalization for safety and de-dup.
		if norm := normalizeGeoPathToStrict(parentPath); norm != "" {
			parentPath = norm
		}
		if parentPath == leafCanonicalPath {
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
		parentParts := strings.Split(strings.Trim(parentPath, "/"), "/")
		if len(parentParts) < 2 || len(parentParts) > 4 || parentParts[0] != "loc" {
			continue
		}
		label := parentParts[len(parentParts)-1]
		if strings.TrimSpace(label) == "" || strings.EqualFold(strings.TrimSpace(label), "na") {
			label = parentParts[1]
		}
		label = strings.ReplaceAll(label, "-", " ")
		f := &models.HorizonFragment{
			CanonicalPath: parentPath,
			Class:          ah.FragmentClassGeo,
			Labels: map[string]string{
				"en": strings.TrimSpace(label),
			},
			Identifiers: map[string]string{
				"shortName": strings.TrimSpace(label),
			},
			Status: string(models.FragmentStatusActive),
			State:  map[string]models.MetricValue{},
			Governance: models.Governance{
				Owner: "SYSTEM",
				// Status will be defaulted by repo on create; still set to keep structData consistent.
				Status: string(models.FragmentStatusActive),
			},
			Metadata: models.FragmentMetadata{
				CreatedAt:       time.Now().UTC(),
				LastUpdatedAt:   time.Now().UTC(),
				LastVerifiedAt:  time.Now().UTC(),
				EpistemicStatus: models.EpistemicStatusLowConfidence,
			},
		}
		// IndexPaths will be built by fragmentsRepo.Create()
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
	for _, parentPathRaw := range parents {
		parentPath := strings.TrimSpace(parentPathRaw)
		if norm := normalizeGeoPathToStrict(parentPath); norm != "" {
			parentPath = norm
		}
		parentID := strings.TrimSpace(parentIDs[parentPath])
		if parentID == "" || childID == "" {
			break
		}
		if parentID == childID {
			continue
		}

		// Skip if edge already exists.
		if _, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", childID, parentID, ah.RelationNameIncorporatedIn); err == nil {
			// already linked
		} else if errors.Is(err, repo.ErrEdgeNotFound) {
			edge := &models.DependencyEdge{
				TenantID:         "global",
				SourceFragmentID: childID,
				TargetFragmentID: parentID,
				Relation:             ah.RelationNameIncorporatedIn,
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
