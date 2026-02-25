package scout

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// =============================================================================
// MARKET GEO HIERARCHY (REGIONAL → GLOBAL)
// =============================================================================
// Some markets are geo-scoped slices of a broader global market.
// Example:
//   /market/cn/lithium   ⊂  /market/global/lithium
//
// This builder ensures a 1-hop geo parent exists and links:
//   /market/global/... HAS_MARKET_SEGMENT /market/{geo}/...
//
// Notes:
// - This is deterministic and best-effort (fail-open).
// - It intentionally only does 1 hop upward (regional → global), no recursion.
// - It does NOT touch fragment.ParentMarketPath (reserved for vertical/semantic parents).

type MarketGeoHierarchyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

func NewMarketGeoHierarchyBuilder(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *MarketGeoHierarchyBuilder {
	return &MarketGeoHierarchyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// EnsureMarketGeoHierarchy ensures a global parent market exists for a regional market and links it via HAS_MARKET_SEGMENT.
// Best-effort: does not block on errors.
func (b *MarketGeoHierarchyBuilder) EnsureMarketGeoHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
	provenanceMethod string,
	sourceID string,
) error {
	if b == nil || b.fragmentsRepo == nil || b.edgesRepo == nil {
		return nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeMarket {
		return nil
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return nil
	}

	cp := strings.TrimSpace(fragment.CanonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/market/") {
		return nil
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// parts: ["market", geo, ...]
	if len(parts) < 3 {
		return nil
	}

	geo := strings.TrimSpace(parts[1])
	if geo == "" || strings.EqualFold(geo, "global") {
		return nil
	}
	rest := parts[2:]
	if len(rest) == 0 {
		return nil
	}

	globalPath := "/market/global/" + strings.Join(rest, "/")
	if strings.TrimSpace(globalPath) == "" || globalPath == cp {
		return nil
	}

	// Find or create the global parent market fragment
	globalID, err := b.ensureMarket(ctx, globalPath, b.deriveGlobalLabel(fragment, globalPath, rest), provenanceMethod, sourceID)
	if err != nil || strings.TrimSpace(globalID) == "" {
		return nil
	}

	// Create HAS_MARKET_SEGMENT edge: global -> regional
	_ = b.ensureGeoHasSubmarketEdge(ctx, globalID, fragmentID, provenanceMethod, sourceID, geo)
	return nil
}

func (b *MarketGeoHierarchyBuilder) deriveGlobalLabel(fragment *models.HorizonFragment, path string, rest []string) string {
	// Prefer explicit label when it's already generic enough; otherwise use structured market label.
	if fragment != nil && fragment.Labels != nil {
		if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
			// Avoid copying region-specific labels like "China lithium market" as-is; keep it conservative:
			// if label already contains "global", keep it, else fall back to slug-based label.
			if strings.Contains(strings.ToLower(v), "global") {
				return v
			}
		}
	}
	if lbl := marketDisplayLabel(path); lbl != "" {
		return lbl
	}
	if len(rest) >= 2 {
		// v2-ish paths: /market/{geo}/{domain}/{vertical}[...]
		// Use vertical as a human-friendly label.
		return bestEffortTitleCase(rest[1])
	}
	return bestEffortTitleCase(rest[len(rest)-1])
}

func (b *MarketGeoHierarchyBuilder) ensureMarket(ctx context.Context, path, label, provenanceMethod, sourceID string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}

	existing, err := b.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
		return strings.TrimSpace(existing.ID), nil
	}
	if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
		return "", err
	}

	// Create minimal global market fragment
	if strings.TrimSpace(label) == "" {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) > 0 {
			label = parts[len(parts)-1]
		}
		label = bestEffortTitleCase(label)
	}
	now := time.Now().UTC()
	f := &models.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: path,
		Type:          models.FragmentTypeMarket,
		Labels: map[string]string{
			"en": strings.TrimSpace(label),
		},
		Status: string(models.FragmentStatusActive),
		State:  map[string]models.MetricValue{},
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Provenance: models.Provenance{
			Method: func() string {
				if strings.TrimSpace(provenanceMethod) != "" {
					return strings.TrimSpace(provenanceMethod)
				}
				return "market_geo_hierarchy_inferred"
			}(),
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceID) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceID)}
			}(),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
	}

	id, err := b.fragmentsRepo.Create(ctx, f)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(id), nil
}

func (b *MarketGeoHierarchyBuilder) ensureGeoHasSubmarketEdge(ctx context.Context, globalID, regionalID, provenanceMethod, sourceID, childGeo string) error {
	globalID = strings.TrimSpace(globalID)
	regionalID = strings.TrimSpace(regionalID)
	if globalID == "" || regionalID == "" {
		return nil
	}

	// Check if edge already exists (HAS_MARKET_SEGMENT or legacy HAS_SUBMARKET)
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", globalID, regionalID, models.DependencyTypeHasMarketSegment)
	if err == nil {
		return nil
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}
	_, err = b.edgesRepo.GetBySourceTargetAndType(ctx, "global", globalID, regionalID, models.DependencyTypeHasSubmarket)
	if err == nil {
		return nil
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	now := time.Now().UTC()
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: globalID,
		TargetFragmentID: regionalID,
		Type:             models.DependencyTypeHasMarketSegment,
		Properties: models.EdgeProperties{
			Criticality:   0.35,
			Amplification: 1.0,
			LatencyHours:  0,
		},
		Qualifiers: map[string]interface{}{
			"reason":        "market.geo.hierarchy.inferred",
			"inferred":      true,
			"inferredKind":  "geo",
			"regionalGeo":   strings.TrimSpace(childGeo),
			"sourceSystem":  "market_geo_hierarchy_builder",
			"oneHopUpwards": true,
		},
		Provenance: models.Provenance{
			Method: strings.TrimSpace(provenanceMethod),
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceID) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceID)}
			}(),
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	}

	_, err = b.edgesRepo.Create(ctx, edge)
	return err
}

