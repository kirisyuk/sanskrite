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
// GOVERNMENT HIERARCHY POLICY
// =============================================================================
// Government bodies form administrative hierarchies.
// This policy automatically creates parent gov bodies and ADMINISTERS edges
// when subordinate agencies are detected.
//
// Examples:
// - /gov/sa/sfda -> parent /gov/sa/ministry-of-health
// - /gov/us/fda -> parent /gov/us/hhs (Health and Human Services)

// GovHierarchyBuilder auto-creates government administrative hierarchy.
type GovHierarchyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewGovHierarchyBuilder creates a new gov hierarchy builder.
func NewGovHierarchyBuilder(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *GovHierarchyBuilder {
	return &GovHierarchyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// GovTypeHierarchy defines the administrative hierarchy order (higher index = lower in hierarchy).
var GovTypeHierarchy = map[string]int{
	"government": 0,
	"ministry":   1,
	"department": 2,
	"agency":     3,
	"authority":  3,
	"regulator":  3,
	"service":    4,
	"bureau":     4,
	"office":     4,
	"committee":  3,
	"council":    3,
	"commission": 3,
}

// GovSlugPatterns maps slug patterns to likely govTypes.
var GovSlugPatterns = map[string]string{
	"ministry-of-":  "ministry",
	"department-of": "department",
	"-authority":    "authority",
	"-agency":       "agency",
	"-service":      "service",
	"-bureau":       "bureau",
	"-office":       "office",
	"-commission":   "commission",
	"-council":      "council",
	"-committee":    "committee",
	"-regulator":    "regulator",
}

// EnsureGovHierarchy infers gov type from slug patterns and updates the fragment.
// Note: Unlike Market/Org hierarchies, we don't auto-create parent gov bodies
// because the relationships are complex and country-specific.
// Best-effort: does not block on errors.
func (b *GovHierarchyBuilder) EnsureGovHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
) error {
	if b == nil || fragment == nil {
		return nil
	}

	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/gov/") {
		return nil
	}

	// Try to infer govType from slug if not already set
	if fragment.GovType == "" {
		fragment.GovType = inferGovType(cp)
	}

	return nil
}

// inferGovType tries to determine government body type from canonical path.
func inferGovType(canonicalPath string) string {
	cp := strings.ToLower(canonicalPath)

	// Extract slug from path
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	slug := parts[2]

	// Check patterns
	for pattern, govType := range GovSlugPatterns {
		if strings.Contains(slug, pattern) {
			return govType
		}
	}

	// Default heuristics
	if strings.HasPrefix(slug, "ministry") {
		return "ministry"
	}
	if strings.HasPrefix(slug, "department") {
		return "department"
	}

	return ""
}

// EnsureAdministersEdge creates an ADMINISTERS edge between gov bodies if appropriate.
// This should be called when explicit parent-child relationship is known (e.g., from LLM research).
func (b *GovHierarchyBuilder) EnsureAdministersEdge(
	ctx context.Context,
	superiorID string,
	subordinateID string,
	provenanceMethod string,
	sourceID string,
) error {
	if b == nil || b.edgesRepo == nil {
		return nil
	}
	if superiorID == "" || subordinateID == "" || superiorID == subordinateID {
		return nil
	}

	// Check if edge already exists
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", superiorID, subordinateID, models.DependencyTypeAdministers)
	if err == nil {
		return nil // Already exists
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create ADMINISTERS edge
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: superiorID,
		TargetFragmentID: subordinateID,
		Type:             models.DependencyTypeAdministers,
		Properties: models.EdgeProperties{
			Criticality:   0.6,
			Amplification: 1.0,
			LatencyHours:  0,
		},
		Qualifiers: map[string]interface{}{
			"reason": "gov.hierarchy",
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
