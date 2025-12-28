package scout

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// =============================================================================
// ORGANIZATION HIERARCHY POLICY
// =============================================================================
// Organizations often have parent-subsidiary relationships that can be inferred
// from naming patterns. This policy automatically creates parent organizations
// and OWNS edges when subsidiaries are detected.
//
// Examples:
// - "Tesla Batteries" -> parent "Tesla"
// - "Apple Japan LLC" -> parent "Apple"
// - "Samsung Electronics America" -> parent "Samsung Electronics" -> parent "Samsung"

// OrgHierarchyBuilder auto-creates organization parent hierarchy.
type OrgHierarchyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewOrgHierarchyBuilder creates a new org hierarchy builder.
func NewOrgHierarchyBuilder(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *OrgHierarchyBuilder {
	return &OrgHierarchyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// SubsidiarySuffixes defines patterns that indicate a subsidiary relationship.
var SubsidiarySuffixes = []string{
	" Japan", " China", " India", " Korea", " America", " Americas",
	" Europe", " Asia", " Africa", " EMEA", " APAC", " LATAM",
	" International", " Global", " Worldwide",
	" Batteries", " Energy", " Motors", " Electronics", " Technologies",
	" Manufacturing", " Services", " Solutions", " Systems", " Products",
	" Division", " Group", " Holdings", " Ventures", " Capital",
	" LLC", " Inc", " Ltd", " JSC", " Corp", " Co",
}

// EnsureOrgHierarchy infers parent organization from naming patterns and creates OWNS edge.
func (b *OrgHierarchyBuilder) EnsureOrgHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
	provenanceMethod string,
	sourceID string,
) error {
	if b == nil || b.fragmentsRepo == nil || b.edgesRepo == nil {
		return nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return nil
	}
	if fragmentID == "" {
		return nil
	}

	// Try to infer parent organization from name
	orgName := strings.TrimSpace(fragment.Labels["en"])
	if orgName == "" {
		orgName = strings.TrimSpace(fragment.Identifiers["shortName"])
	}
	if orgName == "" {
		orgName = strings.TrimSpace(fragment.Identifiers["legalName"])
	}
	if orgName == "" {
		return nil
	}

	parentName := b.inferParentName(orgName)
	if parentName == "" || strings.EqualFold(parentName, orgName) {
		return nil
	}

	// Generate parent canonical path
	parentPath := b.generateOrgPath(parentName)
	if parentPath == "" || parentPath == fragment.CanonicalPath {
		return nil
	}

	// Find or create parent organization
	parentID, err := b.ensureParentOrg(ctx, parentPath, parentName, provenanceMethod, sourceID)
	if err != nil || parentID == "" {
		return nil // Best effort
	}

	// Create OWNS edge: parent -> child
	if err := b.ensureOwnsEdge(ctx, parentID, fragmentID, provenanceMethod, sourceID); err != nil {
		return nil // Best effort
	}

	return nil
}

// inferParentName tries to extract parent organization name from subsidiary name.
func (b *OrgHierarchyBuilder) inferParentName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Try each suffix pattern
	for _, suffix := range SubsidiarySuffixes {
		if strings.HasSuffix(name, suffix) {
			parent := strings.TrimSuffix(name, suffix)
			parent = strings.TrimSpace(parent)
			if len(parent) >= 2 { // Minimum meaningful name
				return parent
			}
		}
	}

	// Try regex patterns for common formats
	// "Company Name (Region)" -> "Company Name"
	if idx := strings.LastIndex(name, "("); idx > 0 {
		parent := strings.TrimSpace(name[:idx])
		if len(parent) >= 2 {
			return parent
		}
	}

	// "Parent - Subsidiary" -> "Parent"
	if parts := strings.SplitN(name, " - ", 2); len(parts) == 2 {
		parent := strings.TrimSpace(parts[0])
		if len(parent) >= 2 {
			return parent
		}
	}

	return ""
}

// generateOrgPath creates a canonical path for an organization name.
func (b *OrgHierarchyBuilder) generateOrgPath(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase, replace spaces with hyphens
	slug := strings.ToLower(name)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if slug == "" {
		return ""
	}

	return "/org/" + slug
}

// ensureParentOrg finds or creates the parent organization fragment.
func (b *OrgHierarchyBuilder) ensureParentOrg(ctx context.Context, path, name, _ string, _ string) (string, error) {
	// provenanceMethod and sourceID are unused but kept for API consistency
	// Try to find existing
	existing, err := b.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil
	}
	if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
		return "", err
	}

	// Create minimal parent org fragment
	f := &models.HorizonFragment{
		CanonicalPath: path,
		Type:          models.FragmentTypeOrganization,
		Labels: map[string]string{
			"en": name,
		},
		Identifiers: map[string]string{
			"shortName": name,
		},
		Status: string(models.FragmentStatusActive),
		State:  map[string]models.MetricValue{},
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       time.Now().UTC(),
			LastUpdatedAt:   time.Now().UTC(),
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
	}
	// IndexPaths will be built by fragmentsRepo.Create()

	id, err := b.fragmentsRepo.Create(ctx, f)
	if err != nil {
		return "", err
	}

	return id, nil
}

// ensureOwnsEdge creates an OWNS edge from parent to child if it doesn't exist.
func (b *OrgHierarchyBuilder) ensureOwnsEdge(ctx context.Context, parentID, childID, provenanceMethod, sourceID string) error {
	// Check if edge already exists
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", parentID, childID, models.DependencyTypeOwns)
	if err == nil {
		return nil // Already exists
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create OWNS edge
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: parentID,
		TargetFragmentID: childID,
		Type:             models.DependencyTypeOwns,
		Properties: models.EdgeProperties{
			Criticality:   0.7,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"reason":   "org.hierarchy.inferred",
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

func (b *OrgHierarchyBuilder) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("OrgHierarchyBuilder(repo=%v, edges=%v)", b.fragmentsRepo != nil, b.edgesRepo != nil)
}
