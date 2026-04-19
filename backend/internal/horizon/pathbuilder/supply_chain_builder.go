package pathbuilder

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplysemantics"
)

// pathBuilderCacheEntry holds a cached value with expiry.
type pathBuilderCacheEntry struct {
	value    interface{}
	expireAt time.Time
}

// pathBuilderCache is a simple in-memory cache with TTL for reduce DB queries during path building.
type pathBuilderCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]pathBuilderCacheEntry
}

func newPathBuilderCache(ttl time.Duration) *pathBuilderCache {
	c := &pathBuilderCache{ttl: ttl, items: make(map[string]pathBuilderCacheEntry)}
	if ttl > 0 {
		go c.cleanupLoop()
	}
	return c
}

func (c *pathBuilderCache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, e := range c.items {
			if e.expireAt.Before(now) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

func (c *pathBuilderCache) get(key string) (interface{}, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || (c.ttl > 0 && time.Now().After(e.expireAt)) {
		return nil, false
	}
	return e.value, true
}

func (c *pathBuilderCache) set(key string, value interface{}) {
	c.mu.Lock()
	c.items[key] = pathBuilderCacheEntry{value: value, expireAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// SupplyChainPathBuilder builds supply chain paths from SupplyLink segments.
type SupplyChainPathBuilder struct {
	fragmentsRepo            *repo.HorizonFragmentsRepository
	edgesRepo                *repo.DependencyEdgesRepository
	artifactsRepo            *repo.MaterializedArtifactsRepository
	analyticalPrimitivesRepo *repo.AnalyticalPrimitivesRepository
	analyticalAggregatesRepo *repo.AnalyticalAggregatesRepository
	refScopeProjectionsRepo  *repo.RefScopeProjectionsRepository
	pathFamilyProjectionsRepo *repo.PathFamilyProjectionsRepository
	compiledWatchRepo        *repo.CompiledWatchSubjectsRepository
	watchSubjectsRepo        *repo.WatchSubjectsRepository
	artifactKind             models.MaterializedKind
	cache                    *pathBuilderCache
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
		artifactKind:  models.MaterializedKindSupplyChainPath,
		cache:         newPathBuilderCache(5 * time.Minute),
	}
}

// WithCacheTTL overrides the default cache TTL (0 = disabled).
func (b *SupplyChainPathBuilder) WithCacheTTL(ttl time.Duration) *SupplyChainPathBuilder {
	b.cache = newPathBuilderCache(ttl)
	return b
}

func (b *SupplyChainPathBuilder) WithArtifactKind(kind models.MaterializedKind) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	if kind.Valid() {
		b.artifactKind = kind
	}
	return b
}

func (b *SupplyChainPathBuilder) WithAnalyticalPrimitivesRepository(r *repo.AnalyticalPrimitivesRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.analyticalPrimitivesRepo = r
	return b
}

func (b *SupplyChainPathBuilder) WithAnalyticalAggregatesRepository(r *repo.AnalyticalAggregatesRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.analyticalAggregatesRepo = r
	return b
}

func (b *SupplyChainPathBuilder) WithRefScopeProjectionsRepository(r *repo.RefScopeProjectionsRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.refScopeProjectionsRepo = r
	return b
}

func (b *SupplyChainPathBuilder) WithPathFamilyProjectionsRepository(r *repo.PathFamilyProjectionsRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.pathFamilyProjectionsRepo = r
	return b
}

func (b *SupplyChainPathBuilder) WithCompiledWatchSubjectsRepository(r *repo.CompiledWatchSubjectsRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.compiledWatchRepo = r
	return b
}

func (b *SupplyChainPathBuilder) WithWatchSubjectsRepository(r *repo.WatchSubjectsRepository) *SupplyChainPathBuilder {
	if b == nil {
		return b
	}
	b.watchSubjectsRepo = r
	return b
}

func (b *SupplyChainPathBuilder) materializedKind() models.MaterializedKind {
	if b != nil && b.artifactKind.Valid() {
		return b.artifactKind
	}
	return models.MaterializedKindSupplyChainPath
}

// BuildPathOptions configures path building behavior.
type BuildPathOptions struct {
	// TenantID for tenant-specific paths (empty = global only)
	TenantID string
	// MaxDepth limits the maximum depth of the path
	MaxDepth int
	// MaxBranching limits segments explored per step (top N by confidence). 0 = no limit.
	MaxBranching int
	// MaxPaths limits how many distinct paths to return (top by confidence). 0 = 10.
	MaxPaths int
	// RequireProductTransformation if true, requires a canonical product-transition edge between products.
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
		MaxBranching:                 5,
		MaxPaths:                     10,
		RequireProductTransformation: true,
		MinConfidence:                0.0,
	}
}

// PathSegment represents a single segment in a supply chain path.
type PathSegment struct {
	// SupplyLink fragment ID (if this is a SupplyLink segment)
	SupplyLinkID string
	// Stable canonical path of the originating SupplyLink fragment.
	SupplyLinkCanonicalPath string
	// RouteVariantKey distinguishes distinct observed route variants for one coarse supply-link.
	RouteVariantKey string
	// TenantID of the originating SupplyLink fragment.
	SupplyLinkTenantID string
	// Supplier organization/facility/deposit canonical path
	SupplierRef string
	// Product canonical path
	ProductRef string
	// Customer organization/facility canonical path
	CustomerRef string
	// Optional facility refs
	SupplierFacilityRef *string
	CustomerFacilityRef *string
	// TransportViaRefs: ordered facility paths product passes through (ports, terminals, pipelines, warehouses)
	TransportViaRefs []string
	// VesselType is optional maritime transport evidence carried with the segment.
	VesselType string
	// SegmentClass distinguishes real external product flow from internal support/self-loop junk.
	SegmentClass string
	// Confidence for this segment (0..1)
	Confidence float64
	// TenantID of the link ("global" or tenant)
	TenantID string
	// IsTenantSpecific indicates if this is tenant-specific data
	IsTenantSpecific bool
	// Metrics from SupplyLink.State (volume, share, etc.)
	Metrics map[string]models.MetricValue
}

// SupplyChainPath represents a complete supply chain path.
type SupplyChainPath struct {
	// Segments ordered from upstream to downstream
	Segments []PathSegment
	// Overall confidence (propagated through chain)
	Confidence float64
	// JoinAssessmentHints carries constrained join-validation results produced upstream in scout.
	JoinAssessmentHints []models.SupplyChainJoinAssessment
	// OpenGaps carries unresolved chain-level gaps discovered before materialization.
	OpenGaps []string
	// Family carries chain-set metadata shared across related alternative paths.
	Family *models.SupplyChainPathFamily
	// Build metadata
	BuildMode string
	MaxDepth  int
}

// SupplyChainMaterializationStage names the reusable sub-stages that turn a structural path into a persisted artifact.
type SupplyChainMaterializationStage string

const (
	SupplyChainMaterializationStageTopology      SupplyChainMaterializationStage = "materialized_path_topology"
	SupplyChainMaterializationStageRouteOverlays SupplyChainMaterializationStage = "route_overlays"
	SupplyChainMaterializationStageStress        SupplyChainMaterializationStage = "path_stress"
	SupplyChainMaterializationStageSufficiency   SupplyChainMaterializationStage = "path_sufficiency"
	SupplyChainMaterializationStageArtifact      SupplyChainMaterializationStage = "artifact_projection"
)

// SupplyChainMaterializationDraft is the reusable handoff between structural path materialization and persistence.
type SupplyChainMaterializationDraft struct {
	Nodes          []models.SupplyChainPathNode
	Edges          []models.SupplyChainPathEdge
	Legs           []models.SupplyChainPathLeg
	Stress         *models.SupplyChainPathStress
	Sufficiency    *models.SupplyChainPathSufficiency
	BindingSummary models.SupplyChainPathBindingSummary
}

// MaterializationStageNames documents the preferred sub-stage order inside structural path materialization.
func (SupplyChainPathBuilder) MaterializationStageNames() []string {
	return []string{
		string(SupplyChainMaterializationStageTopology),
		string(SupplyChainMaterializationStageRouteOverlays),
		string(SupplyChainMaterializationStageStress),
		string(SupplyChainMaterializationStageSufficiency),
		string(SupplyChainMaterializationStageArtifact),
	}
}

// BuildUpstreamPaths builds supply chain paths upstream from a given customer.
// Returns multiple paths (top by confidence) from suppliers to the customer, following SupplyLink segments.
// A real supply chain is a graph; returning multiple paths gives a fuller picture than a single path.
func (b *SupplyChainPathBuilder) BuildUpstreamPaths(
	ctx context.Context,
	customerRef string, // Canonical path of customer org/fac
	opts BuildPathOptions,
) ([]*SupplyChainPath, error) {
	if customerRef == "" {
		return nil, fmt.Errorf("customerRef is required")
	}
	customerRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(customerRef))
	if !strings.HasPrefix(customerRef, "/fac/") && !strings.HasPrefix(customerRef, "/org/") && !strings.HasPrefix(customerRef, "/gov/") {
		return nil, fmt.Errorf("customerRef must be facility, org, or gov canonical path (/fac/... or /org/... or /gov/...)")
	}

	// Find all SupplyLinks where customer matches
	segments, err := b.findSupplyLinksForCustomer(ctx, customerRef, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find supply links: %w", err)
	}

	if len(segments) == 0 {
		return []*SupplyChainPath{}, nil
	}

	// Build paths recursively upstream (with pseudo-cycle guard: no duplicate ProductRef in path)
	var allPaths []*SupplyChainPath
	for _, segment := range segments {
		path, err := b.buildPathRecursive(ctx, segment, opts, 1, map[string]bool{}, map[string]bool{})
		if err != nil {
			continue // Skip paths with errors
		}
		if path != nil {
			allPaths = append(allPaths, path)
		}
	}

	if len(allPaths) == 0 {
		return []*SupplyChainPath{}, nil
	}

	// Sort by confidence descending and take top MaxPaths
	sort.Slice(allPaths, func(i, j int) bool { return allPaths[i].Confidence > allPaths[j].Confidence })
	maxPaths := opts.MaxPaths
	if maxPaths <= 0 {
		maxPaths = 10
	}
	if len(allPaths) > maxPaths {
		allPaths = allPaths[:maxPaths]
	}

	return allPaths, nil
}

// BuildUpstreamPath builds a single supply chain path upstream (highest confidence).
// Prefer BuildUpstreamPaths for a fuller supply chain view.
func (b *SupplyChainPathBuilder) BuildUpstreamPath(
	ctx context.Context,
	customerRef string,
	opts BuildPathOptions,
) (*SupplyChainPath, error) {
	paths, err := b.BuildUpstreamPaths(ctx, customerRef, opts)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return &SupplyChainPath{
			Segments:   []PathSegment{},
			Confidence: 0.0,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}
	return paths[0], nil
}

// buildPathRecursive recursively builds a path upstream from a segment.
// visited tracks supplier+product for cycle detection; visitedProducts guards against pseudo-cycles (same product twice in path).
func (b *SupplyChainPathBuilder) buildPathRecursive(
	ctx context.Context,
	currentSegment PathSegment,
	opts BuildPathOptions,
	depth int,
	visited map[string]bool,
	visitedProducts map[string]bool,
) (*SupplyChainPath, error) {
	if depth > opts.MaxDepth {
		return &SupplyChainPath{
			Segments:   []PathSegment{currentSegment},
			Confidence: currentSegment.Confidence,
			BuildMode:  "tenant_first_global_fallback",
			MaxDepth:   opts.MaxDepth,
		}, nil
	}

	// Pseudo-cycle guard: product must not appear more than once in a single path
	if currentSegment.ProductRef != "" {
		if visitedProducts[currentSegment.ProductRef] {
			return nil, fmt.Errorf("pseudo-cycle: product %s already in path", currentSegment.ProductRef)
		}
		visitedProducts[currentSegment.ProductRef] = true
		defer delete(visitedProducts, currentSegment.ProductRef)
	}

	// Check for cycles (supplier+product pair)
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

	// Branching limit: take top N by confidence to avoid exponential blowup
	if opts.MaxBranching > 0 && len(nextSegments) > opts.MaxBranching {
		sort.Slice(nextSegments, func(i, j int) bool { return nextSegments[i].Confidence > nextSegments[j].Confidence })
		nextSegments = nextSegments[:opts.MaxBranching]
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

		// Pseudo-cycle: skip next segments that would introduce a duplicate product
		if nextSegment.ProductRef != "" && visitedProducts[nextSegment.ProductRef] {
			continue
		}
		upstreamPath, err := b.buildPathRecursive(ctx, nextSegment, opts, depth+1, visited, visitedProducts)
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
// Implements tenant-first, global-fallback precedence.
func (b *SupplyChainPathBuilder) findSupplyLinksForCustomer(
	ctx context.Context,
	customerRef string,
	opts BuildPathOptions,
) ([]PathSegment, error) {
	customerRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(customerRef))
	if customerRef == "" {
		return nil, nil
	}
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
	cacheKey := "supplyLinks:" + customerRef + "|" + tenantID
	if b.cache != nil {
		if v, ok := b.cache.get(cacheKey); ok {
			if segs, ok := v.([]PathSegment); ok {
				return segs, nil
			}
		}
	}

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
			supplierRef = canonical.NormalizeCanonicalPathForPersist(*link.SupplierRef)
		}

		customerRefVal := ""
		if link.CustomerRef != nil {
			customerRefVal = canonical.NormalizeCanonicalPathForPersist(*link.CustomerRef)
		}
		productRefVal := canonical.NormalizeCanonicalPathForPersist(*link.ProductRef)
		if productRefVal == "" {
			continue
		}

		metrics := make(map[string]models.MetricValue)
		if link.State != nil {
			for k, v := range link.State {
				metrics[k] = v
			}
		}
		segmentClass := ""
		if link.Qualifiers != nil {
			if rawClass, ok := link.Qualifiers["segmentClass"].(string); ok {
				segmentClass = strings.TrimSpace(rawClass)
			}
		}
		if segmentClass == "" {
			segmentClass = supplysemantics.ClassifySegment(supplierRef, productRefVal, "", customerRefVal, link.TransportViaRefs).Class
		}
		if !supplysemantics.IsMaterializableSegmentClass(segmentClass) {
			continue
		}
		var supplierFacilityRef *string
		if link.SupplierFacilityRef != nil {
			if normalized := canonical.NormalizeCanonicalPathForPersist(*link.SupplierFacilityRef); normalized != "" {
				nv := normalized
				supplierFacilityRef = &nv
			}
		}
		var customerFacilityRef *string
		if link.CustomerFacilityRef != nil {
			if normalized := canonical.NormalizeCanonicalPathForPersist(*link.CustomerFacilityRef); normalized != "" {
				nv := normalized
				customerFacilityRef = &nv
			}
		}

		segment := PathSegment{
			SupplyLinkID:            link.ID,
			SupplyLinkCanonicalPath: strings.TrimSpace(link.CanonicalPath),
			SupplyLinkTenantID:      strings.TrimSpace(link.TenantID),
			SupplierRef:             supplierRef,
			ProductRef:              productRefVal,
			CustomerRef:             customerRefVal,
			SupplierFacilityRef:     supplierFacilityRef,
			CustomerFacilityRef:     customerFacilityRef,
			TransportViaRefs:        link.TransportViaRefs,
			VesselType:              extractVesselTypeQualifier(link.Qualifiers),
			SegmentClass:            segmentClass,
			Confidence:              confidence,
			TenantID:                link.TenantID,
			IsTenantSpecific:        link.TenantID != "global",
			Metrics:                 metrics,
		}

		segments = append(segments, segment)
	}

	if b.cache != nil {
		b.cache.set(cacheKey, segments)
	}

	return segments, nil
}

// filterByProductTransformation filters segments by product transition relationship.
func (b *SupplyChainPathBuilder) filterByProductTransformation(
	ctx context.Context,
	downstreamProduct string,
	segments []PathSegment,
) []PathSegment {
	var filtered []PathSegment

	for _, segment := range segments {
		// Check if segment.ProductRef reaches downstreamProduct via canonical product transition
		hasTransformation, err := b.checkProductTransformation(ctx, segment.ProductRef, downstreamProduct)
		if err == nil && hasTransformation {
			filtered = append(filtered, segment)
		}
	}

	return filtered
}

// checkProductTransformation checks if upstreamProduct has a canonical product transition to downstreamProduct.
func (b *SupplyChainPathBuilder) checkProductTransformation(
	ctx context.Context,
	upstreamProduct string,
	downstreamProduct string,
) (bool, error) {
	cacheKey := "product-transition:" + upstreamProduct + "|" + downstreamProduct
	if b.cache != nil {
		if v, ok := b.cache.get(cacheKey); ok {
			if vb, ok := v.(bool); ok {
				return vb, nil
			}
		}
	}

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

	// Check for product transition edge
	edges, err := b.edgesRepo.ListBySource(ctx, "global", upstreamFrag.ID, nil, 100)
	if err != nil {
		return false, err
	}

	found := false
	for _, edge := range edges {
		if IsProductTransitionEdgeType(edge.Relation) && edge.TargetFragmentID == downstreamFrag.ID {
			found = true
			break
		}
	}

	if b.cache != nil {
		b.cache.set(cacheKey, found)
	}
	return found, nil
}

// areProductsCompatible checks if products are compatible (soft check).
func (b *SupplyChainPathBuilder) areProductsCompatible(
	ctx context.Context,
	upstreamProduct string,
	downstreamProduct string,
) bool {
	// Check for product transition relationship
	hasTransformation, err := b.checkProductTransformation(ctx, upstreamProduct, downstreamProduct)
	if err == nil && hasTransformation {
		return true
	}

	if strings.TrimSpace(upstreamProduct) != "" && strings.TrimSpace(upstreamProduct) == strings.TrimSpace(downstreamProduct) {
		return true
	}
	return false
}

// buildChainDisplayName generates a human-readable name from path segments.
// Single segment:  "CATL → Lithium Battery Cell → BMW AG"
// Multi-segment:   "CATL → ... → BMW AG (Lithium Battery Cell)"
func buildChainDisplayName(segments []PathSegment) string {
	if len(segments) == 0 {
		return ""
	}

	rootSupplier := slugToTitle(lastPathSegment(segments[0].SupplierRef))
	leafProduct := slugToTitle(lastPathSegment(segments[len(segments)-1].ProductRef))
	leafCustomer := slugToTitle(lastPathSegment(segments[len(segments)-1].CustomerRef))

	if rootSupplier == "" || leafCustomer == "" {
		return ""
	}

	if len(segments) == 1 {
		if leafProduct != "" {
			return rootSupplier + " → " + leafProduct + " → " + leafCustomer
		}
		return rootSupplier + " → " + leafCustomer
	}

	if leafProduct != "" {
		return rootSupplier + " → ... → " + leafCustomer + " (" + leafProduct + ")"
	}
	return rootSupplier + " → ... → " + leafCustomer
}

// buildChainLabels extracts structured labels from path segments.
func buildChainLabels(segments []PathSegment) map[string]string {
	if len(segments) == 0 {
		return nil
	}

	labels := map[string]string{
		"anchorSupplier": segments[0].SupplierRef,
		"anchorCustomer": segments[len(segments)-1].CustomerRef,
		"productAxis":    segments[len(segments)-1].ProductRef,
		"segmentCount":   fmt.Sprintf("%d", len(segments)),
	}

	if segments[0].SupplierFacilityRef != nil {
		geo := extractGeoFromPath(*segments[0].SupplierFacilityRef)
		if geo != "" {
			labels["geoFrom"] = geo
		}
	}

	last := segments[len(segments)-1]
	if last.CustomerFacilityRef != nil {
		geo := extractGeoFromPath(*last.CustomerFacilityRef)
		if geo != "" {
			labels["geoTo"] = geo
		}
	}

	return labels
}

// collectNodeRefs gathers unique canonical paths of all participants.
func collectNodeRefs(segments []PathSegment) []string {
	seen := map[string]bool{}
	var refs []string

	add := func(ref string) {
		if ref == "" || ref == "natural" || ref == "end-consumer" || seen[ref] {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}

	for _, seg := range segments {
		add(seg.SupplierRef)
		add(seg.ProductRef)
		add(seg.CustomerRef)
		if seg.SupplierFacilityRef != nil {
			add(*seg.SupplierFacilityRef)
		}
		if seg.CustomerFacilityRef != nil {
			add(*seg.CustomerFacilityRef)
		}
		for _, t := range seg.TransportViaRefs {
			add(t)
		}
	}

	sort.Strings(refs)
	return refs
}

// BuildStructuralCanonicalKey builds a canonical key based on structural identity:
// /supply-chain/{tenant}/{rootSupplierSlug}/{leafProductSlug}/{leafCustomerSlug}/{pathSignature}
func BuildStructuralCanonicalKey(tenantID string, segments []PathSegment) string {
	if len(segments) == 0 {
		return ""
	}
	rootSupplier := canonicalSlug(segments[0].SupplierRef)
	leafProduct := canonicalSlug(segments[len(segments)-1].ProductRef)
	leafCustomer := canonicalSlug(segments[len(segments)-1].CustomerRef)
	signature := structuralPathSignature(segments)
	if rootSupplier == "" || leafProduct == "" || leafCustomer == "" || signature == "" {
		return ""
	}
	return fmt.Sprintf("/supply-chain/%s/%s/%s/%s/%s", tenantID, rootSupplier, leafProduct, leafCustomer, signature)
}

func structuralPathSignature(segments []PathSegment) string {
	if len(segments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !supplysemantics.IsMaterializableSegmentClass(segment.SegmentClass) &&
			strings.TrimSpace(segment.SupplyLinkCanonicalPath) == "" &&
			strings.TrimSpace(segment.SupplierRef) == "" &&
			strings.TrimSpace(segment.ProductRef) == "" &&
			strings.TrimSpace(segment.CustomerRef) == "" {
			continue
		}
		parts = append(parts, structuralSegmentSignature(segment))
	}
	if len(parts) == 0 {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.Join(parts, "||")))
	return fmt.Sprintf("sig-%016x", h.Sum64())
}

func structuralSegmentSignature(segment PathSegment) string {
	linkPath := strings.TrimSpace(segment.SupplyLinkCanonicalPath)
	if linkPath != "" {
		return linkPath
	}
	return strings.Join([]string{
		strings.TrimSpace(segment.SupplierRef),
		strings.TrimSpace(segment.ProductRef),
		strings.TrimSpace(segment.CustomerRef),
		strings.TrimSpace(segment.SegmentClass),
	}, "|")
}

func payloadStructuralSignature(artifact *models.MaterializedArtifact) string {
	if artifact == nil || artifact.Payload == nil {
		return ""
	}
	parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil || len(parsed.Legs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(parsed.Legs))
	for _, leg := range parsed.Legs {
		parts = append(parts, strings.Join([]string{
			strings.TrimSpace(leg.SupplyLinkCanonicalPath),
			strings.TrimSpace(leg.SupplierRef),
			strings.TrimSpace(leg.ProductRef),
			strings.TrimSpace(leg.CustomerRef),
		}, "|"))
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.Join(parts, "||")))
	return fmt.Sprintf("sig-%016x", h.Sum64())
}

func lastPathSegment(canonicalPath string) string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func canonicalSlug(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	slug := strings.Trim(strings.ReplaceAll(ref, "/", "_"), "_")
	return slug
}

func slugToTitle(slug string) string {
	if slug == "" {
		return ""
	}
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func extractGeoFromPath(facilityPath string) string {
	// /fac/{country-iso}/{slug} -> /loc/{country-iso}
	facilityPath = strings.TrimSpace(facilityPath)
	if !strings.HasPrefix(facilityPath, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(facilityPath, "/fac/"), "/")
	if len(parts) >= 1 && len(parts[0]) == 2 {
		return "/loc/" + parts[0]
	}
	return ""
}

// mergeStringSlices returns the union of two string slices, sorted.
func mergeStringSlices(a, b []string) []string {
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		seen[s] = true
	}
	merged := make([]string, 0, len(seen))
	for s := range seen {
		merged = append(merged, s)
	}
	sort.Strings(merged)
	return merged
}

// BuildMaterializationDraft projects a structural SupplyChainPath into the reusable draft shared by
// artifact persistence, rebuilds, previews, and route-overlay aggregation.
func (b *SupplyChainPathBuilder) BuildMaterializationDraft(ctx context.Context, path *SupplyChainPath) *SupplyChainMaterializationDraft {
	if path == nil {
		return nil
	}
	nodes, edges, legs, stress := b.pathToMaterializedPayload(ctx, path)
	sufficiency := b.evaluatePathSufficiency(ctx, path, legs)
	payload := models.BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, path.Family, buildOperationalChainArtifactNotes(path, legs, stress, b.materializedKind()))
	parsedPayload, _ := models.ParseSupplyChainPathPayload(payload)
	return &SupplyChainMaterializationDraft{
		Nodes:          nodes,
		Edges:          edges,
		Legs:           legs,
		Stress:         stress,
		Sufficiency:    sufficiency,
		BindingSummary: models.SummarizeSupplyChainPathBindings(parsedPayload),
	}
}

func buildMaterializedArtifactFromDraft(
	path *SupplyChainPath,
	draft *SupplyChainMaterializationDraft,
	tenantID string,
	lensID string,
	canonicalKey string,
	kind models.MaterializedKind,
) *models.MaterializedArtifact {
	if path == nil || draft == nil || canonicalKey == "" {
		return nil
	}
	payload := models.BuildSupplyChainPathPayloadDetailed(
		draft.Nodes,
		draft.Edges,
		draft.Legs,
		draft.Stress,
		draft.Sufficiency,
		path.Family,
		buildOperationalChainArtifactNotes(path, draft.Legs, draft.Stress, kind),
	)
	displayName := buildChainDisplayName(path.Segments)
	labels := buildChainLabels(path.Segments)
	artifact := &models.MaterializedArtifact{
		TenantID:         tenantID,
		LensID:           strings.TrimSpace(lensID),
		Kind:             kind,
		CanonicalKey:     canonicalKey,
		Payload:          payload,
		PathStatus:       models.EffectivePathStatusFromSufficiency(draft.Sufficiency),
		DisplayName:      displayName,
		Labels:           labels,
		NodeRefs:         collectNodeRefs(path.Segments),
		GeoRefs:          draft.BindingSummary.GeoRefs,
		FlowRefs:         draft.BindingSummary.FlowRefs,
		ZoneRefs:         draft.BindingSummary.ZoneRefs,
		AffectedZoneRefs: draft.BindingSummary.AffectedZoneRefs,
		AnchorSupplier:   strings.TrimSpace(labels["anchorSupplier"]),
		AnchorCustomer:   strings.TrimSpace(labels["anchorCustomer"]),
		ProductAxis:      strings.TrimSpace(labels["productAxis"]),
	}
	models.PopulateMaterializedArtifactIndexes(artifact)
	return artifact
}

func mergeMaterializedPathArtifacts(existing, incoming *models.MaterializedArtifact) (*models.MaterializedArtifact, error) {
	if incoming == nil {
		return existing, nil
	}
	if existing == nil {
		return incoming, nil
	}
	existingSignature := payloadStructuralSignature(existing)
	incomingSignature := payloadStructuralSignature(incoming)
	if existingSignature != "" && incomingSignature != "" && existingSignature != incomingSignature {
		replacement := *incoming
		replacement.ID = existing.ID
		if strings.TrimSpace(existing.LensID) != "" {
			replacement.LensID = strings.TrimSpace(existing.LensID)
		}
		replacement.CreatedAt = existing.CreatedAt
		replacement.CreatedBy = existing.CreatedBy
		return &replacement, nil
	}
	merged := *incoming
	if strings.TrimSpace(existing.LensID) != "" {
		merged.LensID = strings.TrimSpace(existing.LensID)
	}
	merged.Payload = models.MergePayloads(existing.Payload, incoming.Payload)
	merged.NodeRefs = mergeStringSlices(existing.NodeRefs, incoming.NodeRefs)
	merged.ID = existing.ID
	merged.CreatedAt = existing.CreatedAt
	merged.CreatedBy = existing.CreatedBy
	if existing.PathStatus == "validated" && models.PayloadNodesChanged(existing.Payload, merged.Payload) {
		merged.PathStatus = "hypothesis"
	} else if existing.PathStatus != "" {
		merged.PathStatus = existing.PathStatus
	}
	if parsedMerged, perr := models.ParseSupplyChainPathPayload(merged.Payload); perr == nil && parsedMerged != nil && parsedMerged.Sufficiency != nil {
		merged.PathStatus = models.EffectivePathStatusFromSufficiency(parsedMerged.Sufficiency)
	}
	models.PopulateMaterializedArtifactIndexes(&merged)
	if err := merged.Validate(); err != nil {
		return nil, wrapSupplyChainArtifactValidationError(existing.CanonicalKey, nil, fmt.Errorf("validation error after merge: %w", err))
	}
	return &merged, nil
}

// PrepareMaterializedPathArtifactsBatch turns one or more structural paths into unique artifacts keyed by
// structural canonical key. Distinct route variants for the same structural path are merged into one artifact payload.
func (b *SupplyChainPathBuilder) PrepareMaterializedPathArtifactsBatch(
	ctx context.Context,
	paths []*SupplyChainPath,
	tenantID string,
	lensID string,
	canonicalKeys []string,
) ([]*models.MaterializedArtifact, []string, error) {
	if len(paths) == 0 || len(canonicalKeys) == 0 {
		return nil, nil, nil
	}
	if len(paths) != len(canonicalKeys) {
		return nil, nil, fmt.Errorf("paths and canonicalKeys must have same length")
	}
	existingMap := map[string]*models.MaterializedArtifact{}
	if b != nil && b.artifactsRepo != nil {
		batchExisting, err := b.artifactsRepo.BatchGetByCanonicalKeys(ctx, tenantID, b.materializedKind(), canonicalKeys)
		if err != nil {
			return nil, nil, fmt.Errorf("batch get failed: %w", err)
		}
		existingMap = batchExisting
	}
	orderedKeys := make([]string, 0, len(canonicalKeys))
	artifactsByKey := make(map[string]*models.MaterializedArtifact, len(canonicalKeys))
	for i, path := range paths {
		if path == nil {
			continue
		}
		key := strings.TrimSpace(canonicalKeys[i])
		if key == "" {
			continue
		}
		if err := validatePathSegmentsForMaterialization(key, path); err != nil {
			return nil, nil, err
		}
		draft := b.BuildMaterializationDraft(ctx, path)
		artifact := buildMaterializedArtifactFromDraft(path, draft, tenantID, lensID, key, b.materializedKind())
		if artifact == nil {
			continue
		}
		if current := artifactsByKey[key]; current != nil {
			merged, err := mergeMaterializedPathArtifacts(current, artifact)
			if err != nil {
				return nil, nil, err
			}
			artifactsByKey[key] = merged
			continue
		}
		if existing := existingMap[key]; existing != nil {
			merged, err := mergeMaterializedPathArtifacts(existing, artifact)
			if err != nil {
				return nil, nil, err
			}
			artifact = merged
		}
		artifactsByKey[key] = artifact
		orderedKeys = append(orderedKeys, key)
	}
	artifacts := make([]*models.MaterializedArtifact, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		artifact := artifactsByKey[key]
		if artifact == nil {
			continue
		}
		if err := artifact.Validate(); err != nil {
			return nil, nil, wrapSupplyChainArtifactValidationError(key, nil, fmt.Errorf("validation error: %w", err))
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, orderedKeys, nil
}

// MaterializePath saves a SupplyChainPath as a MaterializedArtifact.
// If an artifact with the same canonical key already exists, payloads are merged (union of nodes/edges).
func (b *SupplyChainPathBuilder) MaterializePath(
	ctx context.Context,
	path *SupplyChainPath,
	tenantID string,
	lensID string,
	canonicalKey string,
) (string, error) {
	if path == nil {
		return "", fmt.Errorf("path is required")
	}
	if canonicalKey == "" {
		return "", fmt.Errorf("canonicalKey is required")
	}
	if err := validatePathSegmentsForMaterialization(canonicalKey, path); err != nil {
		return "", err
	}
	draft := b.BuildMaterializationDraft(ctx, path)
	artifact := buildMaterializedArtifactFromDraft(path, draft, tenantID, lensID, canonicalKey, b.materializedKind())
	if artifact == nil {
		return "", fmt.Errorf("failed to build artifact draft")
	}

	// Check if artifact already exists — merge instead of overwrite
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, b.materializedKind(), canonicalKey)
	if err == nil && existing != nil {
		artifact, err = mergeMaterializedPathArtifacts(existing, artifact)
		if err != nil {
			return "", err
		}
		if err := b.artifactsRepo.Update(ctx, artifact); err != nil {
			return "", fmt.Errorf("failed to update artifact: %w", err)
		}
		return artifact.ID, nil
	}

	if err := artifact.Validate(); err != nil {
		return "", wrapSupplyChainArtifactValidationError(canonicalKey, path, fmt.Errorf("validation error: %w", err))
	}

	id, err := b.artifactsRepo.Create(ctx, artifact)
	if err != nil {
		return "", fmt.Errorf("failed to create artifact: %w", err)
	}

	return id, nil
}

func validatePathSegmentsForMaterialization(canonicalKey string, path *SupplyChainPath) error {
	if path == nil {
		return nil
	}
	unresolved := unresolvedSupplyLinkSegments(path)
	if len(unresolved) == 0 {
		return nil
	}
	return fmt.Errorf(
		"validation error key=%s unresolved supply-link refs=%s",
		strings.TrimSpace(canonicalKey),
		strings.Join(unresolved, "; "),
	)
}

func unresolvedSupplyLinkSegments(path *SupplyChainPath) []string {
	if path == nil || len(path.Segments) == 0 {
		return nil
	}
	const maxSamples = 3
	out := make([]string, 0, maxSamples)
	for idx, segment := range path.Segments {
		if strings.TrimSpace(segment.SupplyLinkCanonicalPath) != "" {
			continue
		}
		if strings.TrimSpace(segment.SupplierRef) == "" &&
			strings.TrimSpace(segment.ProductRef) == "" &&
			strings.TrimSpace(segment.CustomerRef) == "" {
			continue
		}
		viaSummary := "-"
		if len(segment.TransportViaRefs) > 0 {
			viaSummary = strings.Join(segment.TransportViaRefs, ",")
		}
		out = append(out, fmt.Sprintf(
			"leg[%d]=%s -> %s -> %s via=%s",
			idx,
			strings.TrimSpace(segment.SupplierRef),
			strings.TrimSpace(segment.ProductRef),
			strings.TrimSpace(segment.CustomerRef),
			viaSummary,
		))
		if len(out) >= maxSamples {
			break
		}
	}
	return out
}

func wrapSupplyChainArtifactValidationError(canonicalKey string, path *SupplyChainPath, err error) error {
	if err == nil {
		return nil
	}
	details := unresolvedSupplyLinkSegments(path)
	if len(details) == 0 {
		return fmt.Errorf("key=%s: %w", strings.TrimSpace(canonicalKey), err)
	}
	return fmt.Errorf("key=%s unresolved supply-link refs=%s: %w",
		strings.TrimSpace(canonicalKey),
		strings.Join(details, "; "),
		err,
	)
}

// MaterializePathsBatch saves multiple SupplyChainPaths as MaterializedArtifacts in a single batch.
// Uses BatchGetByCanonicalKeys and BatchUpsert for efficiency. Returns artifact IDs in same order as paths.
func (b *SupplyChainPathBuilder) MaterializePathsBatch(
	ctx context.Context,
	paths []*SupplyChainPath,
	tenantID string,
	lensID string,
	canonicalKeys []string,
) ([]string, error) {
	if b.artifactsRepo == nil || len(paths) == 0 || len(canonicalKeys) == 0 {
		return nil, nil
	}
	artifacts, artifactKeys, err := b.PrepareMaterializedPathArtifactsBatch(ctx, paths, tenantID, lensID, canonicalKeys)
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	ids, err := b.artifactsRepo.BatchUpsert(ctx, artifacts)
	if err != nil {
		return nil, err
	}
	idsByKey := make(map[string]string, len(artifactKeys))
	for idx, key := range artifactKeys {
		if idx < len(ids) {
			idsByKey[key] = ids[idx]
			if idx < len(artifacts) && artifacts[idx] != nil {
				artifacts[idx].ID = ids[idx]
			}
		}
	}
	b.persistAnalyticalPrimitives(ctx, artifacts)
	out := make([]string, 0, len(canonicalKeys))
	for _, key := range canonicalKeys {
		out = append(out, idsByKey[strings.TrimSpace(key)])
	}
	return out, nil
}

func (b *SupplyChainPathBuilder) persistAnalyticalPrimitives(ctx context.Context, artifacts []*models.MaterializedArtifact) {
	if b == nil || b.analyticalPrimitivesRepo == nil || len(artifacts) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, artifact := range artifacts {
		persisted := make([]*models.AnalyticalPrimitive, 0, 8)
		for _, primitive := range analytical.BuildPathPrimitivesFromArtifact(artifact, artifact.EvidenceFactIDs, "pathbuilder_materialization", now) {
			if primitive == nil {
				continue
			}
			if err := b.analyticalPrimitivesRepo.Upsert(ctx, primitive); err != nil {
				log.Printf("[SupplyChainPathBuilder] analytical primitive upsert failed artifact=%s key=%s primitive=%s err=%v",
					strings.TrimSpace(artifact.ID),
					strings.TrimSpace(artifact.CanonicalKey),
					strings.TrimSpace(primitive.PrimitiveType),
					err,
				)
				continue
			}
			persisted = append(persisted, primitive)
		}
		if len(persisted) == 0 {
			continue
		}
		_ = analytical.PersistArtifactProjection(ctx, b.refScopeProjectionsRepo, b.pathFamilyProjectionsRepo, analytical.ArtifactProjectionInput{
			Artifact:       artifact,
			AggregateTypes: []string{"weak_points_summary"},
		})
		if b.analyticalAggregatesRepo != nil {
			_, err := analytical.PersistScopeAggregates(ctx, b.analyticalAggregatesRepo, analytical.AggregateScopeInput{
				TenantID:     strings.TrimSpace(artifact.TenantID),
				ScopeType:    "path_artifact",
				ScopeRef:     strings.TrimSpace(artifact.CanonicalKey),
				Layer:        "baseline",
				SourceClass:  "pathbuilder_materialization",
				SubjectRef:   strings.TrimSpace(firstNonEmptyString(artifact.ProductAxis, artifact.AnchorCustomer, artifact.AnchorSupplier)),
				LensRefs:     compactNonEmptyStrings([]string{artifact.LensID}),
				RefreshClass: "immediate",
				Primitives:   persisted,
				ComputedAt:   now,
			})
			if err != nil {
				log.Printf("[SupplyChainPathBuilder] analytical aggregate upsert failed artifact=%s key=%s err=%v",
					strings.TrimSpace(artifact.ID),
					strings.TrimSpace(artifact.CanonicalKey),
					err,
				)
			}
		}
		if b.watchSubjectsRepo != nil && b.analyticalAggregatesRepo != nil {
			_ = analytical.RefreshWatchSubjectAggregates(
				ctx,
				b.analyticalPrimitivesRepo,
				b.analyticalAggregatesRepo,
				b.watchSubjectsRepo,
				strings.TrimSpace(artifact.TenantID),
				analyticalChangedRefsFromPrimitives(persisted),
				now,
			)
			subjects, _ := analytical.ListImpactedWatchSubjects(ctx, b.watchSubjectsRepo, strings.TrimSpace(artifact.TenantID), analyticalChangedRefsFromPrimitives(persisted))
			for _, subject := range subjects {
				_, _ = analytical.PersistCompiledWatchSubject(
					ctx,
					b.compiledWatchRepo,
					b.refScopeProjectionsRepo,
					subject,
					compactNonEmptyStrings([]string{artifact.FamilyID}),
					compactNonEmptyStrings([]string{artifact.CanonicalKey}),
				)
			}
		}
	}
}

func analyticalChangedRefsFromPrimitives(primitives []*models.AnalyticalPrimitive) []string {
	refs := make([]string, 0, len(primitives)*8)
	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		refs = append(refs, strings.TrimSpace(primitive.ScopeRef))
		refs = append(refs, primitive.BasisRefs...)
		refs = append(refs, primitive.ProductRefs...)
		refs = append(refs, primitive.NodeRefs...)
		refs = append(refs, primitive.RouteRefs...)
		refs = append(refs, primitive.ZoneRefs...)
		refs = append(refs, primitive.AffectedRefs...)
	}
	return compactNonEmptyStrings(refs)
}

func compactNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// FindChainsByNodeRef finds all MaterializedArtifacts (supply chain paths) where the given
// canonical path is a participant. Delegates to the underlying repository.
func (b *SupplyChainPathBuilder) FindChainsByNodeRef(ctx context.Context, tenantID string, nodeRef string) ([]*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil {
		return nil, nil
	}
	return b.artifactsRepo.FindByNodeRef(ctx, tenantID, nodeRef, b.materializedKind())
}

// GetChainByCanonicalKey returns a MaterializedArtifact by its structural canonical key.
// Returns (nil, nil) when repository is not wired or nothing is found.
func (b *SupplyChainPathBuilder) GetChainByCanonicalKey(ctx context.Context, tenantID, canonicalKey string) (*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil {
		return nil, nil
	}
	return b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, b.materializedKind(), canonicalKey)
}

// BatchGetChainsByCanonicalKeys returns existing supply-chain artifacts by structural canonical key.
func (b *SupplyChainPathBuilder) BatchGetChainsByCanonicalKeys(ctx context.Context, tenantID string, canonicalKeys []string) (map[string]*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil || len(canonicalKeys) == 0 {
		return map[string]*models.MaterializedArtifact{}, nil
	}
	return b.artifactsRepo.BatchGetByCanonicalKeys(ctx, tenantID, b.materializedKind(), canonicalKeys)
}

// RebuildFromSupplyLinks rebuilds an existing MaterializedArtifact by re-traversing SupplyLinks
// upstream from the chain's leaf customer. Merges any newly discovered nodes/edges into the artifact.
func (b *SupplyChainPathBuilder) RebuildFromSupplyLinks(ctx context.Context, artifact *models.MaterializedArtifact, leafCustomerRef string) error {
	if artifact == nil || leafCustomerRef == "" {
		return nil
	}

	maxDepth := 6
	if artifact.Payload != nil {
		if notes, ok := artifact.Payload["notes"].(map[string]interface{}); ok {
			if md, ok := notes["maxDepth"].(string); ok {
				var v int
				if _, scanErr := fmt.Sscanf(md, "%d", &v); scanErr == nil && v > 0 {
					maxDepth = v
				}
			}
		}
	}

	opts := BuildPathOptions{
		MaxDepth:                     maxDepth,
		MaxBranching:                 5,
		MaxPaths:                     12,
		RequireProductTransformation: true,
	}

	paths, err := b.BuildUpstreamPaths(ctx, leafCustomerRef, opts)
	if err != nil || len(paths) == 0 {
		return err
	}

	bestPath := selectArtifactRebuildPath(artifact, paths, leafCustomerRef)
	if bestPath == nil {
		bestPath = paths[0]
	}
	nodes, edges, legs, stress := b.pathToMaterializedPayload(ctx, bestPath)
	rebuildPath := *bestPath
	rebuildPath.BuildMode = "rebuild_from_supply_links"
	rebuildPath.MaxDepth = maxDepth
	sufficiency := b.evaluatePathSufficiency(ctx, bestPath, legs)
	newPayload := models.BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, rebuildPath.Family, buildOperationalChainArtifactNotes(&rebuildPath, legs, stress, artifact.Kind))

	newNodeRefs := collectNodeRefs(bestPath.Segments)
	artifact.Payload = newPayload
	artifact.NodeRefs = newNodeRefs
	artifact.CanonicalKey = BuildStructuralCanonicalKey(firstNonEmpty(strings.TrimSpace(artifact.TenantID), "global"), bestPath.Segments)
	artifact.DisplayName = buildChainDisplayName(bestPath.Segments)
	artifact.Labels = buildChainLabels(bestPath.Segments)
	models.PopulateMaterializedArtifactIndexes(artifact)
	artifact.UpdatedAt = time.Now().UTC()
	if sufficiency != nil {
		artifact.PathStatus = models.EffectivePathStatusFromSufficiency(sufficiency)
	}

	return b.artifactsRepo.Update(ctx, artifact)
}

func selectArtifactRebuildPath(artifact *models.MaterializedArtifact, paths []*SupplyChainPath, leafCustomerRef string) *SupplyChainPath {
	if len(paths) == 0 {
		return nil
	}
	expectedSupplier := strings.TrimSpace(firstNonEmpty(
		artifact.AnchorSupplier,
		labelValue(artifact, "anchorSupplier"),
	))
	expectedCustomer := strings.TrimSpace(firstNonEmpty(
		artifact.AnchorCustomer,
		labelValue(artifact, "anchorCustomer"),
		leafCustomerRef,
	))
	expectedProduct := strings.TrimSpace(firstNonEmpty(
		artifact.ProductAxis,
		labelValue(artifact, "productAxis"),
		labelValue(artifact, "anchorProduct"),
	))
	expectedLegs := 0
	if parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		expectedLegs = len(parsed.Legs)
	}

	best := paths[0]
	bestScore := -1.0
	for _, path := range paths {
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		score := path.Confidence
		first := path.Segments[0]
		last := path.Segments[len(path.Segments)-1]
		if expectedSupplier != "" && strings.EqualFold(strings.TrimSpace(first.SupplierRef), expectedSupplier) {
			score += 3
		}
		if expectedCustomer != "" && strings.EqualFold(strings.TrimSpace(last.CustomerRef), expectedCustomer) {
			score += 2
		}
		if expectedProduct != "" && strings.EqualFold(strings.TrimSpace(last.ProductRef), expectedProduct) {
			score += 2
		}
		if expectedLegs > 0 && len(path.Segments) == expectedLegs {
			score += 1
		}
		if score > bestScore {
			bestScore = score
			best = path
		}
	}
	return best
}

func labelValue(artifact *models.MaterializedArtifact, key string) string {
	if artifact == nil || artifact.Labels == nil {
		return ""
	}
	return strings.TrimSpace(artifact.Labels[key])
}

// UpdatePathStatus updates the PathStatus of an existing MaterializedArtifact (hypothesis → validated after Critic).
func (b *SupplyChainPathBuilder) UpdatePathStatus(
	ctx context.Context,
	tenantID string,
	canonicalKey string,
	pathStatus string,
) error {
	if canonicalKey == "" || pathStatus == "" {
		return fmt.Errorf("canonicalKey and pathStatus are required")
	}
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, b.materializedKind(), canonicalKey)
	if err != nil || existing == nil {
		return fmt.Errorf("artifact not found: %w", err)
	}
	if parsed, err := models.ParseSupplyChainPathPayload(existing.Payload); err == nil && parsed != nil && parsed.Sufficiency != nil {
		if pathStatus == "validated" {
			existing.PathStatus = models.EffectivePathStatusFromSufficiency(parsed.Sufficiency)
		} else {
			existing.PathStatus = pathStatus
		}
	} else {
		existing.PathStatus = pathStatus
	}
	existing.UpdatedAt = time.Now().UTC()
	return b.artifactsRepo.Update(ctx, existing)
}

// PreviewSufficiency evaluates path continuity without persisting the artifact.
func (b *SupplyChainPathBuilder) PreviewSufficiency(ctx context.Context, path *SupplyChainPath) (*models.SupplyChainPathSufficiency, error) {
	if path == nil {
		return nil, fmt.Errorf("path is required")
	}
	_, _, legs, _ := b.pathToMaterializedPayload(ctx, path)
	return b.evaluatePathSufficiency(ctx, path, legs), nil
}

// pathToMaterializedPayload converts a SupplyChainPath to MaterializedArtifact payload format.
func (b *SupplyChainPathBuilder) pathToMaterializedPayload(ctx context.Context, path *SupplyChainPath) (
	[]models.SupplyChainPathNode,
	[]models.SupplyChainPathEdge,
	[]models.SupplyChainPathLeg,
	*models.SupplyChainPathStress,
) {
	var nodes []models.SupplyChainPathNode
	var edges []models.SupplyChainPathEdge
	var legs []models.SupplyChainPathLeg
	segments := collapseAbstractNetworkSegments(path.Segments)

	nodeIndexMap := make(map[string]int) // canonicalPath -> index

	// Build nodes array and index map
	for _, segment := range segments {
		if !supplysemantics.IsMaterializableSegmentClass(segment.SegmentClass) {
			continue
		}
		// Add supplier node
		if segment.SupplierRef != "" && segment.SupplierRef != "natural" {
			if _, exists := nodeIndexMap[segment.SupplierRef]; !exists {
				nodeType := "org"
				if strings.HasPrefix(segment.SupplierRef, "/fac/") {
					nodeType = "fac"
				} else if strings.HasPrefix(segment.SupplierRef, "/deposit/") {
					nodeType = "deposit"
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
				} else if strings.HasPrefix(segment.CustomerRef, "/deposit/") {
					nodeType = "deposit"
				}
				nodes = append(nodes, models.SupplyChainPathNode{
					Type: nodeType,
					Ref:  segment.CustomerRef,
				})
				nodeIndexMap[segment.CustomerRef] = len(nodes) - 1
			}
		}

		for _, viaRef := range segment.TransportViaRefs {
			viaRef = strings.TrimSpace(viaRef)
			if viaRef == "" {
				continue
			}
			if _, exists := nodeIndexMap[viaRef]; exists {
				continue
			}
			nodeType := "fac"
			if strings.HasPrefix(viaRef, "/deposit/") {
				nodeType = "deposit"
			}
			nodes = append(nodes, models.SupplyChainPathNode{
				Type: nodeType,
				Ref:  viaRef,
			})
			nodeIndexMap[viaRef] = len(nodes) - 1
		}
	}

	binder := pathbinding.NewEngine(b.fragmentsRepo, b.edgesRepo)
	stressEval := pathstress.NewEvaluator()

	// Build edges array
	for i, segment := range segments {
		if !supplysemantics.IsMaterializableSegmentClass(segment.SegmentClass) {
			continue
		}
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

		normalizedViaRefs := NormalizeLegTransportViaRefs(
			ctx,
			b.fragmentsRepo,
			segment.SupplierRef,
			segment.TransportViaRefs,
			segment.CustomerRef,
		)
		binding := binder.BindSegment(ctx, pathbinding.SegmentInput{
			SupplierRef:       segment.SupplierRef,
			CustomerRef:       segment.CustomerRef,
			ProductRef:        segment.ProductRef,
			TransportViaRefs:  normalizedViaRefs,
			SegmentConfidence: segment.Confidence,
		})
		normalizedViaRefs = PruneAbstractNetworkViaRefs(binding.Mode, binding.FlowRefs, normalizedViaRefs)
		resolvedRoute := ResolveSupplyChainRouteGeometryDetailed(
			ctx,
			b.fragmentsRepo,
			append(append([]string{segment.SupplierRef}, normalizedViaRefs...), segment.CustomerRef),
		)
		primaryFlowRefs, alternateFlowRefs := selectPrimaryAndAlternateFlowRefs(
			binding.FlowRefs,
			binding.AlternateFlowRefs,
			resolvedRoute.FlowRefs,
			binding.Mode,
		)
		orderedRouteRefs := models.NormalizeOrderedRouteRefs(
			append(append([]string{segment.SupplierRef}, normalizedViaRefs...), segment.CustomerRef),
		)
		primaryOverlay := models.SupplyChainRouteOverlay{
			RouteVariantKey:     strings.TrimSpace(firstNonEmpty(segment.RouteVariantKey, models.BuildSupplyLinkRouteVariantKey(normalizedViaRefs))),
			RouteProfileKey:     strings.TrimSpace(models.BuildRouteGeometryProfileKey(orderedRouteRefs)),
			OrderedRefs:         append([]string(nil), orderedRouteRefs...),
			TransportViaRefs:    append([]string(nil), normalizedViaRefs...),
			RouteCoordinates:    append([]models.Coordinates(nil), resolvedRoute.Coordinates...),
			RouteGeometrySource: strings.TrimSpace(resolvedRoute.Source),
			Mode:                binding.Mode,
			FlowRefs:            append([]string(nil), primaryFlowRefs...),
			AlternateFlowRefs:   append([]string(nil), alternateFlowRefs...),
			GeoRefs:             append([]string(nil), binding.GeoRefs...),
			ZoneRefs:            append([]string(nil), binding.ZoneRefs...),
			AffectedZoneRefs:    append([]string(nil), binding.AffectedZoneRefs...),
			SpatialSemantics:    append([]string(nil), binding.SpatialSemantics...),
			VesselType:          strings.TrimSpace(segment.VesselType),
			BindingConfidence:   binding.BindingConfidence,
			SegmentConfidence:   segment.Confidence,
			Status:              "primary",
		}
		leg := models.SupplyChainPathLeg{
			Index:                   i,
			SupplierRef:             segment.SupplierRef,
			CustomerRef:             segment.CustomerRef,
			ProductRef:              segment.ProductRef,
			SupplyLinkCanonicalPath: strings.TrimSpace(segment.SupplyLinkCanonicalPath),
			SupplyLinkID:            strings.TrimSpace(segment.SupplyLinkID),
			SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(segment.SupplyLinkTenantID, segment.TenantID)),
			RouteVariantKey:         strings.TrimSpace(firstNonEmpty(segment.RouteVariantKey, models.BuildSupplyLinkRouteVariantKey(normalizedViaRefs))),
			TransportViaRefs:        append([]string(nil), normalizedViaRefs...),
			RouteCoordinates:        append([]models.Coordinates(nil), resolvedRoute.Coordinates...),
			RouteGeometrySource:     strings.TrimSpace(resolvedRoute.Source),
			VesselType:              strings.TrimSpace(segment.VesselType),
			Mode:                    binding.Mode,
			GeoRefs:                 append([]string(nil), binding.GeoRefs...),
			FlowRefs:                append([]string(nil), primaryFlowRefs...),
			AlternateFlowRefs:       append([]string(nil), alternateFlowRefs...),
			ZoneRefs:                append([]string(nil), binding.ZoneRefs...),
			AffectedZoneRefs:        append([]string(nil), binding.AffectedZoneRefs...),
			SpatialSemantics:        append([]string(nil), binding.SpatialSemantics...),
			RouteOverlays:           []models.SupplyChainRouteOverlay{primaryOverlay},
			BindingConfidence:       binding.BindingConfidence,
			SegmentConfidence:       segment.Confidence,
		}
		if supplierIdx >= 0 {
			idx := supplierIdx
			leg.SupplierNodeIndex = &idx
		}
		if customerIdx >= 0 {
			idx := customerIdx
			leg.CustomerNodeIndex = &idx
		}
		if productIdx >= 0 {
			idx := productIdx
			leg.ProductNodeIndex = &idx
		}
		for _, viaRef := range segment.TransportViaRefs {
			if viaIdx, ok := nodeIndexMap[viaRef]; ok {
				leg.TransportViaNodeIndices = append(leg.TransportViaNodeIndices, viaIdx)
			}
		}
		leg.Stress = stressEval.EvaluateLeg(leg)

		// Create SUPPLY_LINK edge
		if supplierIdx >= 0 && customerIdx >= 0 && productIdx >= 0 {
			segmentIdx := i
			edges = append(edges, models.SupplyChainPathEdge{
				Type:                    "SUPPLY_LINK",
				From:                    supplierIdx,
				To:                      customerIdx,
				ViaProductIndex:         &productIdx,
				ProductNodeIndex:        &productIdx,
				ProductRef:              segment.ProductRef,
				SupplyLinkID:            segment.SupplyLinkID,
				SupplyLinkCanonicalPath: strings.TrimSpace(segment.SupplyLinkCanonicalPath),
				SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(segment.SupplyLinkTenantID, segment.TenantID)),
				SegmentIndex:            &segmentIdx,
				CarriesProduct:          true,
				Role:                    "product_flow",
				Metrics:                 cloneMetricValues(segment.Metrics),
				Qualifiers:              buildPathEdgeQualifiers(segment, leg, "product_flow", 0),
			})
		}
		if supplierIdx >= 0 && len(segment.TransportViaRefs) > 0 {
			previousIdx := supplierIdx
			for viaOrder, viaRef := range segment.TransportViaRefs {
				viaRef = strings.TrimSpace(viaRef)
				if viaRef == "" {
					continue
				}
				viaIdx, ok := nodeIndexMap[viaRef]
				if !ok {
					continue
				}
				segmentIdx := i
				edges = append(edges, models.SupplyChainPathEdge{
					Type:                    "LOGISTICS",
					From:                    previousIdx,
					To:                      viaIdx,
					Mode:                    "required",
					ProductNodeIndex:        &productIdx,
					ProductRef:              segment.ProductRef,
					SupplyLinkID:            segment.SupplyLinkID,
					SupplyLinkCanonicalPath: strings.TrimSpace(segment.SupplyLinkCanonicalPath),
					SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(segment.SupplyLinkTenantID, segment.TenantID)),
					SegmentIndex:            &segmentIdx,
					CarriesProduct:          true,
					Role:                    "logistics_leg",
					Metrics:                 cloneMetricValues(segment.Metrics),
					Qualifiers:              buildPathEdgeQualifiers(segment, leg, "logistics_leg", viaOrder),
				})
				previousIdx = viaIdx
			}
			if customerIdx >= 0 && previousIdx != customerIdx {
				segmentIdx := i
				edges = append(edges, models.SupplyChainPathEdge{
					Type:                    "LOGISTICS",
					From:                    previousIdx,
					To:                      customerIdx,
					Mode:                    "required",
					ProductNodeIndex:        &productIdx,
					ProductRef:              segment.ProductRef,
					SupplyLinkID:            segment.SupplyLinkID,
					SupplyLinkCanonicalPath: strings.TrimSpace(segment.SupplyLinkCanonicalPath),
					SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(segment.SupplyLinkTenantID, segment.TenantID)),
					SegmentIndex:            &segmentIdx,
					CarriesProduct:          true,
					Role:                    "logistics_leg",
					Metrics:                 cloneMetricValues(segment.Metrics),
					Qualifiers:              buildPathEdgeQualifiers(segment, leg, "logistics_leg", len(segment.TransportViaRefs)),
				})
			}
		}

		// Add ontological edge between products if this is not the last segment
		if i < len(segments)-1 {
			nextSegment := segments[i+1]
			currentProductIdx := productIdx
			nextProductIdx := nodeIndexMap[nextSegment.ProductRef]

			// Check if products are connected via canonical product transition (optional)
			// For now, we'll add it as "optional" mode
			relation := string(ProductToProductEdgeTypeFromPaths(segment.ProductRef, nextSegment.ProductRef))
			mode := "optional"
			edges = append(edges, models.SupplyChainPathEdge{
				Type:             "ONTOLOGY",
				From:             currentProductIdx,
				To:               nextProductIdx,
				ProductNodeIndex: &currentProductIdx,
				ProductRef:       segment.ProductRef,
				SegmentIndex:     intPtr(i),
				CarriesProduct:   true,
				Role:             "product_transformation",
				Relation:         relation,
				FromProductIndex: &currentProductIdx,
				ToProductIndex:   &nextProductIdx,
				Mode:             mode,
			})
		}

		legs = append(legs, leg)
	}

	return nodes, edges, legs, stressEval.EvaluatePath(legs)
}

func cloneMetricValues(in map[string]models.MetricValue) map[string]models.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildPathEdgeQualifiers(segment PathSegment, leg models.SupplyChainPathLeg, role string, legOrder int) map[string]interface{} {
	qualifiers := map[string]interface{}{
		"role":              role,
		"segmentConfidence": segment.Confidence,
		"tenantId":          strings.TrimSpace(segment.TenantID),
		"isTenantSpecific":  segment.IsTenantSpecific,
		"legOrder":          legOrder,
	}
	if strings.TrimSpace(leg.Mode) != "" {
		qualifiers[QualifierRouteMode] = strings.TrimSpace(leg.Mode)
	}
	if strings.TrimSpace(leg.RouteVariantKey) != "" {
		qualifiers["route_variant_key"] = strings.TrimSpace(leg.RouteVariantKey)
	}
	if strings.TrimSpace(leg.RouteGeometrySource) != "" {
		qualifiers["route_geometry_source"] = strings.TrimSpace(leg.RouteGeometrySource)
	}
	if len(leg.GeoRefs) > 0 {
		qualifiers[QualifierGeoRefs] = append([]string(nil), leg.GeoRefs...)
	}
	if len(leg.FlowRefs) > 0 {
		qualifiers[QualifierFlowRefs] = append([]string(nil), leg.FlowRefs...)
	}
	if len(leg.AlternateFlowRefs) > 0 {
		qualifiers[QualifierAlternateFlowRefs] = append([]string(nil), leg.AlternateFlowRefs...)
	}
	if len(leg.ZoneRefs) > 0 {
		qualifiers[QualifierZoneRefs] = append([]string(nil), leg.ZoneRefs...)
	}
	if len(leg.AffectedZoneRefs) > 0 {
		qualifiers[QualifierAffectedZoneRefs] = append([]string(nil), leg.AffectedZoneRefs...)
	}
	if len(leg.SpatialSemantics) > 0 {
		qualifiers[QualifierSpatialSemantics] = append([]string(nil), leg.SpatialSemantics...)
	}
	if leg.BindingConfidence > 0 {
		qualifiers[QualifierBindingConfidence] = leg.BindingConfidence
	}
	if leg.Stress != nil {
		if leg.Stress.StressScore > 0 {
			qualifiers[QualifierPathStressScore] = leg.Stress.StressScore
		}
		if leg.Stress.Fragility > 0 {
			qualifiers[QualifierPathFragility] = leg.Stress.Fragility
		}
		if leg.Stress.Rerouteability > 0 {
			qualifiers[QualifierPathRerouteability] = leg.Stress.Rerouteability
		}
		if leg.Stress.ZonePressure > 0 {
			qualifiers[QualifierPathZonePressure] = leg.Stress.ZonePressure
		}
		if leg.Stress.AffectedZonePressure > 0 {
			qualifiers[QualifierPathAffectedPressure] = leg.Stress.AffectedZonePressure
		}
		if leg.Stress.RecoveryLatencyHrs > 0 {
			qualifiers[QualifierRecoveryLatencyHours] = leg.Stress.RecoveryLatencyHrs
		}
		if len(leg.Stress.DominantFactors) > 0 {
			qualifiers["dominantFactors"] = append([]string(nil), leg.Stress.DominantFactors...)
		}
	}
	if len(segment.TransportViaRefs) > 0 {
		qualifiers["transportViaCount"] = len(segment.TransportViaRefs)
		qualifiers[QualifierTransportVia] = append([]string(nil), segment.TransportViaRefs...)
	}
	if strings.TrimSpace(leg.VesselType) != "" {
		qualifiers["vesselType"] = strings.TrimSpace(leg.VesselType)
	}
	if strings.TrimSpace(segment.ProductRef) != "" {
		qualifiers["productRef"] = strings.TrimSpace(segment.ProductRef)
	}
	if strings.TrimSpace(segment.SupplyLinkID) != "" {
		qualifiers["supplyLinkId"] = strings.TrimSpace(segment.SupplyLinkID)
	}
	if strings.TrimSpace(segment.SupplyLinkCanonicalPath) != "" {
		qualifiers["supplyLinkCanonicalPath"] = strings.TrimSpace(segment.SupplyLinkCanonicalPath)
	}
	return qualifiers
}

func intPtr(v int) *int {
	return &v
}

func extractVesselTypeQualifier(qualifiers map[string]interface{}) string {
	if len(qualifiers) == 0 {
		return ""
	}
	for _, key := range []string{"vesselType", "vessel_type"} {
		if raw, ok := qualifiers[key].(string); ok {
			if value := strings.TrimSpace(raw); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func selectPrimaryAndAlternateFlowRefs(bindingFlowRefs, bindingAlternateFlowRefs, resolvedFlowRefs []string, mode string) ([]string, []string) {
	primary := prioritizeFlowRefsByMode(uniqueTrimmedStringsPreserveOrder(resolvedFlowRefs), mode)
	if len(primary) == 0 {
		return prioritizeFlowRefsByMode(uniqueTrimmedStringsPreserveOrder(bindingFlowRefs), mode), uniqueTrimmedStringsPreserveOrder(bindingAlternateFlowRefs)
	}
	alternate := append([]string(nil), bindingAlternateFlowRefs...)
	for _, ref := range bindingFlowRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" || stringSliceContains(primary, ref) {
			continue
		}
		alternate = append(alternate, ref)
	}
	return primary, uniqueTrimmedStringsPreserveOrder(alternate)
}

func stringSliceContains(items []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == needle {
			return true
		}
	}
	return false
}

func uniqueTrimmedStringsPreserveOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func prioritizeFlowRefsByMode(values []string, mode string) []string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || len(values) < 2 {
		return values
	}
	prefix := mode + "_"
	preferred := make([]string, 0, len(values))
	other := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), prefix) {
			preferred = append(preferred, value)
			continue
		}
		other = append(other, value)
	}
	if len(preferred) == 0 {
		return values
	}
	return append(preferred, other...)
}

func buildSupplyChainArtifactNotes(path *SupplyChainPath, legs []models.SupplyChainPathLeg, stress *models.SupplyChainPathStress) map[string]interface{} {
	return buildOperationalChainArtifactNotes(path, legs, stress, models.MaterializedKindSupplyChainPath)
}

func buildOperationalChainArtifactNotes(path *SupplyChainPath, legs []models.SupplyChainPathLeg, stress *models.SupplyChainPathStress, kind models.MaterializedKind) map[string]interface{} {
	mode := "supply_chain"
	if kind == models.MaterializedKindServiceChainPath {
		mode = "service_chain"
	}
	profile := contractsemantics.ProfileForDomainMode(mode)
	notes := map[string]interface{}{
		"buildMode":          path.BuildMode,
		"maxDepth":           fmt.Sprintf("%d", path.MaxDepth),
		"confidence":         path.Confidence,
		"graphLayer":         string(profile.PrimaryLayer),
		"allowedEdges":       dependencyTypesToStrings(profile.AllowedEdges),
		"pathCarriedEdges":   dependencyTypesToStrings(profile.PathCarriedEdges),
		"overlayEdges":       dependencyTypesToStrings(profile.OverlayEdges),
		"complementaryModes": append([]string(nil), profile.ComplementaryModes...),
		"artifactKind":       string(kind),
		"domainMode":         mode,
		"legCount":           len(legs),
	}
	if stress != nil {
		notes["pathStressScore"] = stress.PathScore
		notes["bottleneckLegIndex"] = stress.BottleneckLegIndex
		notes["recoveryLatencyHours"] = stress.RecoveryLatencyHrs
	}
	return notes
}
