package scout

import (
	"context"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"time"
)

// ExpandProductMaterialsOptions configures the expand materials operation.
type ExpandProductMaterialsOptions struct {
	// UpstreamDepth is the number of levels to explore upstream (default: 3)
	UpstreamDepth int `json:"upstreamDepth,omitempty"`
	// DownstreamDepth is the number of levels to explore downstream (default: 3)
	DownstreamDepth int `json:"downstreamDepth,omitempty"`
	// IncludeProducers if true, also fetches producer organizations from Wikidata (default: true)
	IncludeProducers bool `json:"includeProducers,omitempty"`
}

// DefaultExpandProductMaterialsOptions returns default options.
func DefaultExpandProductMaterialsOptions() ExpandProductMaterialsOptions {
	return ExpandProductMaterialsOptions{
		UpstreamDepth:    3,
		DownstreamDepth:  3,
		IncludeProducers: true,
	}
}

// ExpandProductMaterialsResult contains the result of the expand operation.
type ExpandProductMaterialsResult struct {
	// ProductID is the ID of the product that was expanded
	ProductID string `json:"productId"`
	// ProductPath is the canonical path of the product
	ProductPath string `json:"productPath"`
	// UpstreamCount is the number of upstream materials found/created
	UpstreamCount int `json:"upstreamCount"`
	// DownstreamCount is the number of downstream products found/created
	DownstreamCount int `json:"downstreamCount"`
	// ProducersCount is the number of producer organizations found/created
	ProducersCount int `json:"producersCount"`
	// EdgesCount is the number of edges created
	EdgesCount int `json:"edgesCount"`
	// Duration is the time taken for the operation
	Duration time.Duration `json:"duration"`
	// WikidataID is the Wikidata Q-ID used for enrichment (if any)
	WikidataID string `json:"wikidataId,omitempty"`
	// Errors contains any non-fatal errors encountered
	Errors []string `json:"errors,omitempty"`
}

// ExpandProductMaterials expands upstream and downstream materials for a product fragment.
// This is a synchronous operation that uses Wikidata to discover value chain relationships.
//
// The operation:
// 1. Gets the product fragment by ID or canonical path
// 2. Resolves Wikidata Q-ID (from identifiers or by searching)
// 3. Fetches upstream materials (P10565 "is produced from", P186 "material used")
// 4. Fetches downstream products (via SPARQL reverse lookup)
// 5. Creates transforms_into edges between products
// 6. Optionally creates PRODUCES edges from manufacturer organizations
//
// Returns statistics about created fragments and edges.
func (e *ScoutEngine) ExpandProductMaterials(
	ctx context.Context,
	productIDOrPath string,
	opts ExpandProductMaterialsOptions,
) (*ExpandProductMaterialsResult, error) {
	startTime := time.Now()
	result := &ExpandProductMaterialsResult{
		Errors: []string{},
	}

	if e == nil {
		return nil, fmt.Errorf("scout engine is nil")
	}
	if e.productHierarchyBuilder == nil {
		return nil, fmt.Errorf("product hierarchy builder not configured")
	}
	if e.fragmentsRepo == nil {
		return nil, fmt.Errorf("fragments repository not configured")
	}

	productIDOrPath = strings.TrimSpace(productIDOrPath)
	if productIDOrPath == "" {
		return nil, fmt.Errorf("product ID or canonical path is required")
	}

	// Apply defaults
	if opts.UpstreamDepth <= 0 {
		opts.UpstreamDepth = 3
	}
	if opts.DownstreamDepth <= 0 {
		opts.DownstreamDepth = 3
	}

	// Get the product fragment
	var fragment *models.HorizonFragment
	var err error

	if strings.HasPrefix(productIDOrPath, "/product/") {
		// It's a canonical path
		fragment, err = e.fragmentsRepo.GetByCanonicalPath(ctx, productIDOrPath)
	} else {
		// Try as ID first
		fragment, err = e.fragmentsRepo.Get(ctx, productIDOrPath)
	}
	if err != nil {
		return nil, fmt.Errorf("product not found: %w", err)
	}
	if fragment == nil {
		return nil, fmt.Errorf("product not found: %s", productIDOrPath)
	}
	if fragment.Class != ah.FragmentClassProduct {
		return nil, fmt.Errorf("fragment is not a product (type=%s)", fragment.Class)
	}

	result.ProductID = fragment.ID
	result.ProductPath = fragment.CanonicalPath

	// Count fragments before operation
	beforeUpstream, _ := e.countUpstreamProducts(ctx, fragment.ID)
	beforeDownstream, _ := e.countDownstreamProducts(ctx, fragment.ID)
	beforeProducers, _ := e.countProducers(ctx, fragment.ID)

	// Call the hierarchy builder
	provenanceMethod := "manual_expand_materials"
	sourceID := fmt.Sprintf("expand:%s", fragment.ID)

	err = e.productHierarchyBuilder.EnsureProductHierarchy(
		ctx,
		fragment.ID,
		fragment,
		provenanceMethod,
		sourceID,
	)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("hierarchy build: %v", err))
	}

	// Get Wikidata ID from fragment (may have been resolved during hierarchy build)
	if fragment.Identifiers != nil {
		result.WikidataID = fragment.Identifiers["wikidataId"]
	}

	// Count fragments after operation
	afterUpstream, _ := e.countUpstreamProducts(ctx, fragment.ID)
	afterDownstream, _ := e.countDownstreamProducts(ctx, fragment.ID)
	afterProducers, _ := e.countProducers(ctx, fragment.ID)

	result.UpstreamCount = afterUpstream - beforeUpstream
	result.DownstreamCount = afterDownstream - beforeDownstream
	result.ProducersCount = afterProducers - beforeProducers
	result.EdgesCount = result.UpstreamCount + result.DownstreamCount + result.ProducersCount
	result.Duration = time.Since(startTime)

	// Normalize negative counts (shouldn't happen, but just in case)
	if result.UpstreamCount < 0 {
		result.UpstreamCount = 0
	}
	if result.DownstreamCount < 0 {
		result.DownstreamCount = 0
	}
	if result.ProducersCount < 0 {
		result.ProducersCount = 0
	}

	return result, nil
}

// countUpstreamProducts counts products that have product-transition edges pointing to the given fragment.
func (e *ScoutEngine) countUpstreamProducts(ctx context.Context, fragmentID string) (int, error) {
	if e.edgesRepo == nil {
		return 0, nil
	}
	edges, err := e.edgesRepo.ListByTarget(ctx, "global", fragmentID, nil, 1000)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, edge := range edges {
		if IsProductTransitionEdgeType(edge.Relation) {
			count++
		}
	}
	return count, nil
}

// countDownstreamProducts counts products that this fragment has product-transition edges to.
func (e *ScoutEngine) countDownstreamProducts(ctx context.Context, fragmentID string) (int, error) {
	if e.edgesRepo == nil {
		return 0, nil
	}
	edges, err := e.edgesRepo.ListBySource(ctx, "global", fragmentID, nil, 1000)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, edge := range edges {
		if IsProductTransitionEdgeType(edge.Relation) {
			count++
		}
	}
	return count, nil
}

// countProducers counts organizations that have PRODUCES edge to the given fragment.
func (e *ScoutEngine) countProducers(ctx context.Context, fragmentID string) (int, error) {
	if e.edgesRepo == nil {
		return 0, nil
	}
	edges, err := e.edgesRepo.ListByTarget(ctx, "global", fragmentID, nil, 1000)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, edge := range edges {
		if edge.Relation == ah.RelationNameProduces {
			count++
		}
	}
	return count, nil
}
