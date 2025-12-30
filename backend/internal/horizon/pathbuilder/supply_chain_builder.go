package pathbuilder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// SupplyChainPathBuilder builds supply chain paths from SupplyLink segments.
type SupplyChainPathBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	artifactsRepo *repo.MaterializedArtifactsRepository
}

// NewSupplyChainPathBuilder creates a new SupplyChainPathBuilder.
func NewSupplyChainPathBuilder(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	artifactsRepo *repo.MaterializedArtifactsRepository,
) *SupplyChainPathBuilder {
	return &SupplyChainPathBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		artifactsRepo: artifactsRepo,
	}
}

// BuildPathOptions configures path building behavior.
type BuildPathOptions struct {
	// TenantID for tenant-specific paths (empty = global only)
	TenantID string
	// MaxDepth limits the maximum depth of the path
	MaxDepth int
	// RequireProductTransformation if true, requires TRANSFORMS_INTO between products
	RequireProductTransformation bool
	// EffectiveAt filters links by time validity (nil = no time filter)
	EffectiveAt *time.Time
	// MinConfidence filters links by minimum confidence (0..1)
	MinConfidence float64
}

// DefaultBuildPathOptions returns default options.
func DefaultBuildPathOptions() BuildPathOptions {
	return BuildPathOptions{
		MaxDepth:                     6,
		RequireProductTransformation: false,
		MinConfidence:                0.0,
	}
}

// PathSegment represents a single segment in a supply chain path.
type PathSegment struct {
	// SupplyLink fragment ID (if this is a SupplyLink segment)
	SupplyLinkID string
	// Supplier organization/facility canonical path
	SupplierRef string
	// Product canonical path
	ProductRef string
	// Customer organization/facility canonical path
	CustomerRef string
	// Optional facility refs
	SupplierFacilityRef *string
	CustomerFacilityRef *string
	// Confidence for this segment (0..1)
	Confidence float64
	// TenantID of the link ("global" or tenant)
	TenantID string
	// IsTenantSpecific indicates if this is tenant-specific data
	IsTenantSpecific bool
}

// SupplyChainPath represents a complete supply chain path.
type SupplyChainPath struct {
	// Segments ordered from upstream to downstream
	Segments []PathSegment
	// Overall confidence (propagated through chain)
	Confidence float64
	// Build metadata
	BuildMode string
	MaxDepth  int
}

// BuildUpstreamPath builds a supply chain path upstream from a given customer.
// Returns paths from suppliers to the customer, following SupplyLink segments.
func (b *SupplyChainPathBuilder) BuildUpstreamPath(
	ctx context.Context,
	customerRef string, // Canonical path of customer org/fac
	opts BuildPathOptions,
) (*SupplyChainPath, error) {
	if customerRef == "" {
		return nil, fmt.Errorf("customerRef is required")
	}

	// Normalize customerRef (ensure it's a canonical path)
	if !strings.HasPrefix(customerRef, "/org/") && !strings.HasPrefix(customerRef, "/fac/") {
		return nil, fmt.Errorf("customerRef must be an organization or facility canonical path")
	}

	// Find all SupplyLinks where customer matches
	segments, err := b.findSupplyLinksForCustomer(ctx, customerRef, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find supply links: %w", err)
	}

	if len(segments) == 0 {
		// Return empty path
		return &SupplyChainPath{
			Segments:   []PathSegment{},
			Confidence: 0.0,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Build paths recursively upstream
	var allPaths []*SupplyChainPath
	for _, segment := range segments {
		path, err := b.buildPathRecursive(ctx, segment, opts, 1, map[string]bool{})
		if err != nil {
			continue // Skip paths with errors
		}
		if path != nil {
			allPaths = append(allPaths, path)
		}
	}

	// For now, return the first path (later we can merge/rank multiple paths)
	if len(allPaths) == 0 {
		return &SupplyChainPath{
			Segments:   []PathSegment{},
			Confidence: 0.0,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Select best path (highest confidence)
	bestPath := allPaths[0]
	for _, path := range allPaths[1:] {
		if path.Confidence > bestPath.Confidence {
			bestPath = path
		}
	}

	return bestPath, nil
}

// buildPathRecursive recursively builds a path upstream from a segment.
func (b *SupplyChainPathBuilder) buildPathRecursive(
	ctx context.Context,
	currentSegment PathSegment,
	opts BuildPathOptions,
	depth int,
	visited map[string]bool,
) (*SupplyChainPath, error) {
	if depth > opts.MaxDepth {
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Check for cycles
	if currentSegment.SupplierRef != "" {
		key := fmt.Sprintf("%s:%s", currentSegment.SupplierRef, currentSegment.ProductRef)
		if visited[key] {
			return nil, fmt.Errorf("cycle detected")
		}
		visited[key] = true
		defer delete(visited, key)
	}

	// If supplier is nil or "natural", this is a source
	if currentSegment.SupplierRef == "" || currentSegment.SupplierRef == "natural" {
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Find next segments upstream (where current supplier is customer)
	nextSegments, err := b.findSupplyLinksForCustomer(ctx, currentSegment.SupplierRef, opts)
	if err != nil {
		// If no upstream links found, this is a source
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	if len(nextSegments) == 0 {
		// No upstream links, this is a source
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Optionally validate product transformation
	if opts.RequireProductTransformation {
		nextSegments = b.filterByProductTransformation(ctx, currentSegment.ProductRef, nextSegments)
	}

	// Build paths for each next segment
	var bestPath *SupplyChainPath
	bestConfidence := 0.0

	for _, nextSegment := range nextSegments {
		// Check product compatibility (optional)
		if !opts.RequireProductTransformation {
			// Still check if products are compatible (soft validation)
			if !b.areProductsCompatible(ctx, nextSegment.ProductRef, currentSegment.ProductRef) {
				// Penalize confidence but don't reject
				nextSegment.Confidence *= 0.7
			}
		}

		upstreamPath, err := b.buildPathRecursive(ctx, nextSegment, opts, depth+1, visited)
		if err != nil {
			continue
		}

		// Combine paths
		combinedSegments := append([]PathSegment{currentSegment}, upstreamPath.Segments...)
		combinedConfidence := currentSegment.Confidence * upstreamPath.Confidence

		combinedPath := &SupplyChainPath{
			Segments:   combinedSegments,
			Confidence: combinedConfidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}

		if combinedConfidence > bestConfidence {
			bestPath = combinedPath
			bestConfidence = combinedConfidence
		}
	}

	if bestPath == nil {
		// No valid upstream paths, return current segment as source
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	return bestPath, nil
}

// findSupplyLinksForCustomer finds all SupplyLink fragments where customer matches.
// Implements tenant-first, global-fallback precedence.
func (b *SupplyChainPathBuilder) findSupplyLinksForCustomer(
	ctx context.Context,
	customerRef string,
	opts BuildPathOptions,
) ([]PathSegment, error) {
	// Segment-level precedence:
	// - If tenant segments exist for this step, use them and do NOT mix with global.
	// - Otherwise, fall back to global to avoid path breaks.
	if opts.TenantID != "" && opts.TenantID != "global" {
		tenantSegments, err := b.findSupplyLinksByCustomerAndTenant(ctx, customerRef, opts.TenantID, opts)
		if err == nil && len(tenantSegments) > 0 {
			return tenantSegments, nil
		}
	}

	globalSegments, err := b.findSupplyLinksByCustomerAndTenant(ctx, customerRef, "global", opts)
	if err != nil {
		return nil, err
	}
	return globalSegments, nil
}

// findSupplyLinksByCustomerAndTenant finds SupplyLinks for a specific tenant.
func (b *SupplyChainPathBuilder) findSupplyLinksByCustomerAndTenant(
	ctx context.Context,
	customerRef string,
	tenantID string,
	opts BuildPathOptions,
) ([]PathSegment, error) {
	// Find SupplyLinks where customer matches
	links, err := b.fragmentsRepo.FindSupplyLinksByCustomer(ctx, customerRef, tenantID, 100)
	if err != nil {
		return nil, err
	}

	var segments []PathSegment
	for _, link := range links {
		// Skip if productRef is missing
		if link.ProductRef == nil || strings.TrimSpace(*link.ProductRef) == "" {
			continue
		}

		// Filter by time validity if specified
		if opts.EffectiveAt != nil {
			if link.ValidFrom != nil && link.ValidFrom.After(*opts.EffectiveAt) {
				continue
			}
			if link.ValidTo != nil && link.ValidTo.Before(*opts.EffectiveAt) {
				continue
			}
		}

		// Extract confidence from State (if available)
		confidence := 0.5 // default
		if link.State != nil {
			if confMetric, ok := link.State["confidence"]; ok {
				confidence = confMetric.Value
			}
		}

		// Skip if below minimum confidence
		if confidence < opts.MinConfidence {
			continue
		}

		supplierRef := ""
		if link.SupplierRef != nil {
			supplierRef = *link.SupplierRef
		}

		customerRefVal := ""
		if link.CustomerRef != nil {
			customerRefVal = *link.CustomerRef
		}

		segment := PathSegment{
			SupplyLinkID:        link.ID,
			SupplierRef:         supplierRef,
			ProductRef:          *link.ProductRef,
			CustomerRef:         customerRefVal,
			SupplierFacilityRef: link.SupplierFacilityRef,
			CustomerFacilityRef: link.CustomerFacilityRef,
			Confidence:          confidence,
			TenantID:            link.TenantID,
			IsTenantSpecific:    link.TenantID != "global",
		}

		segments = append(segments, segment)
	}

	return segments, nil
}

// filterByProductTransformation filters segments by TRANSFORMS_INTO relationship.
func (b *SupplyChainPathBuilder) filterByProductTransformation(
	ctx context.Context,
	downstreamProduct string,
	segments []PathSegment,
) []PathSegment {
	var filtered []PathSegment

	for _, segment := range segments {
		// Check if segment.ProductRef TRANSFORMS_INTO downstreamProduct
		hasTransformation, err := b.checkProductTransformation(ctx, segment.ProductRef, downstreamProduct)
		if err == nil && hasTransformation {
			filtered = append(filtered, segment)
		}
	}

	return filtered
}

// checkProductTransformation checks if upstreamProduct TRANSFORMS_INTO downstreamProduct.
func (b *SupplyChainPathBuilder) checkProductTransformation(
	ctx context.Context,
	upstreamProduct string,
	downstreamProduct string,
) (bool, error) {
	// Get upstream product fragment
	upstreamFrag, err := b.fragmentsRepo.GetByCanonicalPath(ctx, upstreamProduct)
	if err != nil {
		return false, err
	}

	// Get downstream product fragment
	downstreamFrag, err := b.fragmentsRepo.GetByCanonicalPath(ctx, downstreamProduct)
	if err != nil {
		return false, err
	}

	// Check for TRANSFORMS_INTO edge
	edges, err := b.edgesRepo.ListBySource(ctx, "global", upstreamFrag.ID, nil, 100)
	if err != nil {
		return false, err
	}

	for _, edge := range edges {
		if edge.Type == models.DependencyTypeTransformsInto && edge.TargetFragmentID == downstreamFrag.ID {
			return true, nil
		}
	}

	return false, nil
}

// areProductsCompatible checks if products are compatible (soft check).
func (b *SupplyChainPathBuilder) areProductsCompatible(
	ctx context.Context,
	upstreamProduct string,
	downstreamProduct string,
) bool {
	// Check for TRANSFORMS_INTO relationship
	hasTransformation, err := b.checkProductTransformation(ctx, upstreamProduct, downstreamProduct)
	if err == nil && hasTransformation {
		return true
	}

	// Products might be compatible even without explicit TRANSFORMS_INTO
	// (e.g., same product category, or inferred compatibility)
	// For now, return true (soft validation)
	return true
}

// MaterializePath saves a SupplyChainPath as a MaterializedArtifact.
func (b *SupplyChainPathBuilder) MaterializePath(
	ctx context.Context,
	path *SupplyChainPath,
	tenantID string,
	canonicalKey string,
) (string, error) {
	if path == nil {
		return "", fmt.Errorf("path is required")
	}
	if canonicalKey == "" {
		return "", fmt.Errorf("canonicalKey is required")
	}

	// Convert path segments to MaterializedArtifact payload
	nodes, edges := b.pathToMaterializedPayload(path)

	payload := models.BuildSupplyChainPathPayload(nodes, edges, map[string]interface{}{
		"buildMode": path.BuildMode,
		"maxDepth":  fmt.Sprintf("%d", path.MaxDepth),
	})

	artifact := &models.MaterializedArtifact{
		TenantID:     tenantID,
		Kind:         models.MaterializedKindSupplyChainPath,
		CanonicalKey: canonicalKey,
		Payload:      payload,
	}

	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	// Check if artifact already exists
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKey)
	if err == nil && existing != nil {
		// Update existing
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		if err := b.artifactsRepo.Upsert(ctx, artifact); err != nil {
			return "", fmt.Errorf("failed to update artifact: %w", err)
		}
		return artifact.ID, nil
	}

	// Create new
	id, err := b.artifactsRepo.Create(ctx, artifact)
	if err != nil {
		return "", fmt.Errorf("failed to create artifact: %w", err)
	}

	return id, nil
}

// pathToMaterializedPayload converts a SupplyChainPath to MaterializedArtifact payload format.
func (b *SupplyChainPathBuilder) pathToMaterializedPayload(path *SupplyChainPath) (
	[]models.SupplyChainPathNode,
	[]models.SupplyChainPathEdge,
) {
	var nodes []models.SupplyChainPathNode
	var edges []models.SupplyChainPathEdge

	nodeIndexMap := make(map[string]int) // canonicalPath -> index

	// Build nodes array and index map
	for _, segment := range path.Segments {
		// Add supplier node
		if segment.SupplierRef != "" && segment.SupplierRef != "natural" {
			if _, exists := nodeIndexMap[segment.SupplierRef]; !exists {
				nodeType := "org"
				if strings.HasPrefix(segment.SupplierRef, "/fac/") {
					nodeType = "fac"
				}
				nodes = append(nodes, models.SupplyChainPathNode{
					Type: nodeType,
					Ref:  segment.SupplierRef,
				})
				nodeIndexMap[segment.SupplierRef] = len(nodes) - 1
			}
		}

		// Add product node
		if _, exists := nodeIndexMap[segment.ProductRef]; !exists {
			nodes = append(nodes, models.SupplyChainPathNode{
				Type: "product",
				Ref:  segment.ProductRef,
			})
			nodeIndexMap[segment.ProductRef] = len(nodes) - 1
		}

		// Add customer node
		if segment.CustomerRef != "" && segment.CustomerRef != "end-consumer" {
			if _, exists := nodeIndexMap[segment.CustomerRef]; !exists {
				nodeType := "org"
				if strings.HasPrefix(segment.CustomerRef, "/fac/") {
					nodeType = "fac"
				}
				nodes = append(nodes, models.SupplyChainPathNode{
					Type: nodeType,
					Ref:  segment.CustomerRef,
				})
				nodeIndexMap[segment.CustomerRef] = len(nodes) - 1
			}
		}
	}

	// Build edges array
	for i, segment := range path.Segments {
		supplierIdx := -1
		productIdx := -1
		customerIdx := -1

		// Find indices
		if segment.SupplierRef != "" && segment.SupplierRef != "natural" {
			supplierIdx = nodeIndexMap[segment.SupplierRef]
		}
		productIdx = nodeIndexMap[segment.ProductRef]
		if segment.CustomerRef != "" && segment.CustomerRef != "end-consumer" {
			customerIdx = nodeIndexMap[segment.CustomerRef]
		}

		// Create SUPPLY_LINK edge
		if supplierIdx >= 0 && customerIdx >= 0 && productIdx >= 0 {
			edges = append(edges, models.SupplyChainPathEdge{
				Type:           "SUPPLY_LINK",
				From:           supplierIdx,
				To:             customerIdx,
				ViaProductIndex: &productIdx,
			})
		}

		// Add ontological edge between products if this is not the last segment
		if i < len(path.Segments)-1 {
			nextSegment := path.Segments[i+1]
			currentProductIdx := productIdx
			nextProductIdx := nodeIndexMap[nextSegment.ProductRef]

			// Check if products are connected via TRANSFORMS_INTO (optional)
			// For now, we'll add it as "optional" mode
			relation := "TRANSFORMS_INTO"
			mode := "optional"
			edges = append(edges, models.SupplyChainPathEdge{
				Type:     "ONTOLOGY",
				From:     currentProductIdx,
				To:       nextProductIdx,
				Relation: relation,
				FromProductIndex: &currentProductIdx,
				ToProductIndex:   &nextProductIdx,
				Mode:     mode,
			})
		}
	}

	return nodes, edges
}

