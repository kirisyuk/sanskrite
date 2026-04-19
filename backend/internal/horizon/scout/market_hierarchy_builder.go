package scout

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// =============================================================================
// MARKET HIERARCHY POLICY
// =============================================================================
// Markets form hierarchies from broader to narrower segments.
// This policy automatically creates parent markets and HAS_MARKET_SEGMENT edges
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

// EnsureMarketHierarchy infers parent market from canonical path and creates HAS_MARKET_SEGMENT edge.
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
	if fragment == nil || fragment.Class != FragmentTypeMarket {
		return nil
	}
	if fragmentID == "" {
		return nil
	}

	// Parse canonical path:
	// - legacy: /market/{geo}/{slug}
	// - segment-first (v2): /market/{geo}/{domain}/{vertical}[/{...optional...}]
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/market/") {
		return nil
	}

	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// parts: ["market", geo, ...]
	if len(parts) < 3 {
		return nil
	}

	geo := parts[1]
	rest := parts[2:]

	parentPath := ""
	parentNameSlug := ""

	if len(rest) >= 2 {
		// L1 market: domain==vertical means this IS the domain-level market, no parent to infer
		if len(rest) == 2 && strings.TrimSpace(rest[0]) == strings.TrimSpace(rest[1]) {
			return nil
		}

		// L3+: for paths deeper than L2, parent is always the path with the last segment removed.
		// /market/{geo}/a/b/c → parent = /market/{geo}/a/b
		if len(rest) > 2 {
			parentPath = "/market/" + geo + "/" + strings.Join(rest[:len(rest)-1], "/")
			parentNameSlug = rest[len(rest)-2]
		} else {
			// L2: /market/{geo}/{domain}/{vertical} → try taxonomy parent, fallback to L1
			domain := strings.TrimSpace(rest[0])
			vertical := strings.TrimSpace(rest[1])
			if pv := taxonomy.SegmentVerticalParent(vertical); pv != "" && pv != vertical {
				parentPath = "/market/" + geo + "/" + domain + "/" + pv
				parentNameSlug = pv
			} else {
				parentPath = "/market/" + geo + "/" + domain
				parentNameSlug = domain
			}
		}
	} else {
		// legacy: /market/{geo}/{slug}
		slug := strings.TrimSpace(rest[0])
		parentSlug := inferParentMarketSlug(slug)
		if parentSlug == "" || parentSlug == slug {
			return nil
		}
		parentPath = "/market/" + geo + "/" + parentSlug
		parentNameSlug = parentSlug
	}
	if parentPath == "" || parentPath == cp {
		return nil
	}

	// Find or create parent market
	parentID, err := b.ensureParentMarket(ctx, parentPath, parentNameSlug, provenanceMethod, sourceID)
	if err != nil || parentID == "" {
		return nil // Best effort
	}

	// Update fragment with parent market path
	if fragment.ParentMarketPath == "" {
		fragment.ParentMarketPath = parentPath
	}

	// Create HAS_MARKET_SEGMENT edge: parent -> child
	if err := b.ensureHasMarketSegmentEdge(ctx, parentID, fragmentID, provenanceMethod, sourceID); err != nil {
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
func (b *MarketHierarchyBuilder) ensureParentMarket(ctx context.Context, path, slug, provenanceMethod, sourceID string) (string, error) {
	// Try to find existing
	existing, err := b.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil
	}
	if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
		return "", err
	}

		// Create minimal parent market fragment with structured label
		name := marketDisplayLabel(path)
		if name == "" {
			name = bestEffortTitleCase(strings.ReplaceAll(slug, "-", " "))
		}

		f := &models.HorizonFragment{
			CanonicalPath: path,
			Class:          FragmentTypeMarket,
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
			Method: func() string {
				if strings.TrimSpace(provenanceMethod) != "" {
					return strings.TrimSpace(provenanceMethod)
				}
				return "market_hierarchy_inferred"
			}(),
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

// ensureHasMarketSegmentEdge creates a HAS_MARKET_SEGMENT edge from parent to child if it doesn't exist.
func (b *MarketHierarchyBuilder) ensureHasMarketSegmentEdge(ctx context.Context, parentID, childID, provenanceMethod, sourceID string) error {
	// Check if edge already exists (HAS_MARKET_SEGMENT or legacy HAS_SUBMARKET)
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", parentID, childID, DependencyTypeHasMarketSegment)
	if err == nil {
		return nil
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}
	_, err = b.edgesRepo.GetBySourceTargetAndType(ctx, "global", parentID, childID, DependencyTypeHasSubmarket)
	if err == nil {
		return nil
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create HAS_MARKET_SEGMENT edge
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: parentID,
		TargetFragmentID: childID,
		Relation:             DependencyTypeHasMarketSegment,
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
