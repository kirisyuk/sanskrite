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
// MARKET HIERARCHY POLICY
// =============================================================================
// Markets form hierarchies from broader to narrower segments.
// This policy automatically creates parent markets and HAS_SUBMARKET edges
// when sub-markets are detected.
//
// Examples:
// - /market/sa/event-marketing -> parent /market/sa/marketing
// - /market/global/ev-batteries -> parent /market/global/batteries
// - /market/de/commercial-banking -> parent /market/de/banking

// MarketHierarchyBuilder auto-creates market parent hierarchy.
type MarketHierarchyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewMarketHierarchyBuilder creates a new market hierarchy builder.
func NewMarketHierarchyBuilder(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *MarketHierarchyBuilder {
	return &MarketHierarchyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// MarketSlugPrefixes defines common prefixes that indicate a sub-market relationship.
// The key is the prefix, the value is the parent segment to remove.
var MarketSlugPrefixes = []string{
	"digital-", "online-", "mobile-", "retail-", "commercial-", "corporate-",
	"b2b-", "b2c-", "enterprise-", "sme-", "small-business-",
	"consumer-", "industrial-", "professional-",
	"event-", "content-", "social-", "influencer-", "performance-",
	"investment-", "private-", "islamic-",
}

// MarketSlugSuffixes defines common suffixes that can be removed to get parent market.
var MarketSlugSuffixes = []string{
	"-services", "-solutions", "-products", "-platforms",
	"-segment", "-sector", "-industry", "-market",
}

// EnsureMarketHierarchy infers parent market from canonical path and creates HAS_SUBMARKET edge.
// Best-effort: does not block on errors.
func (b *MarketHierarchyBuilder) EnsureMarketHierarchy(
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
	if fragmentID == "" {
		return nil
	}

	// Parse canonical path: /market/{geo}/{slug}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/market/") {
		return nil
	}

	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// parts: ["market", geo, slug]
	if len(parts) != 3 {
		return nil
	}

	geo := parts[1]
	slug := parts[2]

	// Try to infer parent market slug
	parentSlug := inferParentMarketSlug(slug)
	if parentSlug == "" || parentSlug == slug {
		return nil
	}

	// Generate parent canonical path
	parentPath := "/market/" + geo + "/" + parentSlug
	if parentPath == cp {
		return nil
	}

	// Find or create parent market
	parentID, err := b.ensureParentMarket(ctx, parentPath, parentSlug, geo, provenanceMethod, sourceID)
	if err != nil || parentID == "" {
		return nil // Best effort
	}

	// Update fragment with parent market path
	if fragment.ParentMarketPath == "" {
		fragment.ParentMarketPath = parentPath
	}

	// Create HAS_SUBMARKET edge: parent -> child
	if err := b.ensureHasSubmarketEdge(ctx, parentID, fragmentID, provenanceMethod, sourceID); err != nil {
		return nil // Best effort
	}

	return nil
}

// inferParentMarketSlug tries to extract parent market slug from sub-market slug.
func inferParentMarketSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}

	// Try each prefix pattern
	for _, prefix := range MarketSlugPrefixes {
		if strings.HasPrefix(slug, prefix) {
			parent := strings.TrimPrefix(slug, prefix)
			parent = strings.TrimSpace(parent)
			if len(parent) >= 2 { // Minimum meaningful name
				return parent
			}
		}
	}

	// Try each suffix pattern
	for _, suffix := range MarketSlugSuffixes {
		if strings.HasSuffix(slug, suffix) {
			parent := strings.TrimSuffix(slug, suffix)
			parent = strings.TrimSpace(parent)
			if len(parent) >= 2 {
				return parent
			}
		}
	}

	// Try splitting on hyphen and removing last segment
	// e.g., "event-marketing" -> "marketing"
	if idx := strings.Index(slug, "-"); idx > 0 && idx < len(slug)-1 {
		// Get the part after the first hyphen
		rest := slug[idx+1:]
		if len(rest) >= 3 {
			return rest
		}
	}

	return ""
}

// ensureParentMarket finds or creates the parent market fragment.
func (b *MarketHierarchyBuilder) ensureParentMarket(ctx context.Context, path, slug, geo, provenanceMethod, sourceID string) (string, error) {
	// Try to find existing
	existing, err := b.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil
	}
	if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
		return "", err
	}

	// Create minimal parent market fragment
	// Convert slug to human-readable name
	name := strings.ReplaceAll(slug, "-", " ")
	name = strings.Title(name) //nolint:staticcheck // Title is deprecated but works fine here

	f := &models.HorizonFragment{
		CanonicalPath: path,
		Type:          models.FragmentTypeMarket,
		Labels: map[string]string{
			"en": name,
		},
		Status: string(models.FragmentStatusActive),
		State:  map[string]models.MetricValue{},
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Provenance: models.Provenance{
			Method: "market_hierarchy_inferred",
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceID) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceID)}
			}(),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       time.Now().UTC(),
			LastUpdatedAt:   time.Now().UTC(),
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
	}

	id, err := b.fragmentsRepo.Create(ctx, f)
	if err != nil {
		return "", err
	}

	return id, nil
}

// ensureHasSubmarketEdge creates a HAS_SUBMARKET edge from parent to child if it doesn't exist.
func (b *MarketHierarchyBuilder) ensureHasSubmarketEdge(ctx context.Context, parentID, childID, provenanceMethod, sourceID string) error {
	// Check if edge already exists
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", parentID, childID, models.DependencyTypeHasSubmarket)
	if err == nil {
		return nil // Already exists
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create HAS_SUBMARKET edge
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: parentID,
		TargetFragmentID: childID,
		Type:             models.DependencyTypeHasSubmarket,
		Properties: models.EdgeProperties{
			Criticality:   0.5,
			Amplification: 1.0,
			LatencyHours:  0,
		},
		Qualifiers: map[string]interface{}{
			"reason":   "market.hierarchy.inferred",
			"inferred": true,
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
			CreatedAt:     time.Now().UTC(),
			LastUpdatedAt: time.Now().UTC(),
		},
	}

	_, err = b.edgesRepo.Create(ctx, edge)
	return err
}
