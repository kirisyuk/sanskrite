package pathbuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	artifactsRepo *repo.MaterializedArtifactsRepository
	cache         *pathBuilderCache
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
		cache:         newPathBuilderCache(5 * time.Minute),
	}
}

// WithCacheTTL overrides the default cache TTL (0 = disabled).
func (b *SupplyChainPathBuilder) WithCacheTTL(ttl time.Duration) *SupplyChainPathBuilder {
	b.cache = newPathBuilderCache(ttl)
	return b
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
	cacheKey := "transforms:" + upstreamProduct + "|" + downstreamProduct
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

	// Check for TRANSFORMS_INTO edge
	edges, err := b.edgesRepo.ListBySource(ctx, "global", upstreamFrag.ID, nil, 100)
	if err != nil {
		return false, err
	}

	found := false
	for _, edge := range edges {
		if edge.Type == models.DependencyTypeTransformsInto && edge.TargetFragmentID == downstreamFrag.ID {
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
	// Check for TRANSFORMS_INTO relationship
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
// /supply-chain/{tenant}/{rootSupplierSlug}/{leafProductSlug}/{leafCustomerSlug}
func BuildStructuralCanonicalKey(tenantID string, segments []PathSegment) string {
	if len(segments) == 0 {
		return ""
	}
	rootSupplier := canonicalSlug(segments[0].SupplierRef)
	leafProduct := canonicalSlug(segments[len(segments)-1].ProductRef)
	leafCustomer := canonicalSlug(segments[len(segments)-1].CustomerRef)
	if rootSupplier == "" || leafProduct == "" || leafCustomer == "" {
		return ""
	}
	return fmt.Sprintf("/supply-chain/%s/%s/%s/%s", tenantID, rootSupplier, leafProduct, leafCustomer)
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

	nodes, edges, legs, stress := b.pathToMaterializedPayload(ctx, path)

	sufficiency := b.evaluatePathSufficiency(ctx, path, legs)
	payload := models.BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, path.Family, buildSupplyChainArtifactNotes(path, legs, stress))

	displayName := buildChainDisplayName(path.Segments)
	labels := buildChainLabels(path.Segments)
	nodeRefs := collectNodeRefs(path.Segments)
	parsedPayload, _ := models.ParseSupplyChainPathPayload(payload)
	bindingSummary := models.SummarizeSupplyChainPathBindings(parsedPayload)

	artifact := &models.MaterializedArtifact{
		TenantID:         tenantID,
		LensID:           strings.TrimSpace(lensID),
		Kind:             models.MaterializedKindSupplyChainPath,
		CanonicalKey:     canonicalKey,
		Payload:          payload,
		PathStatus:       models.EffectivePathStatusFromSufficiency(sufficiency),
		DisplayName:      displayName,
		Labels:           labels,
		NodeRefs:         nodeRefs,
		GeoRefs:          bindingSummary.GeoRefs,
		FlowRefs:         bindingSummary.FlowRefs,
		ZoneRefs:         bindingSummary.ZoneRefs,
		AffectedZoneRefs: bindingSummary.AffectedZoneRefs,
		AnchorSupplier:   strings.TrimSpace(labels["anchorSupplier"]),
		AnchorCustomer:   strings.TrimSpace(labels["anchorCustomer"]),
		ProductAxis:      strings.TrimSpace(labels["productAxis"]),
	}
	models.PopulateMaterializedArtifactIndexes(artifact)

	// Check if artifact already exists — merge instead of overwrite
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKey)
	if err == nil && existing != nil {
		merged := models.MergePayloads(existing.Payload, artifact.Payload)
		artifact.Payload = merged
		artifact.NodeRefs = mergeStringSlices(existing.NodeRefs, artifact.NodeRefs)
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		artifact.CreatedBy = existing.CreatedBy

		// Downgrade validated status if new nodes appeared
		if existing.PathStatus == "validated" && models.PayloadNodesChanged(existing.Payload, merged) {
			artifact.PathStatus = "hypothesis"
		} else if existing.PathStatus != "" {
			artifact.PathStatus = existing.PathStatus
		}

		// Regenerate display from merged data
		artifact.DisplayName = displayName
		artifact.Labels = labels
		if parsedMerged, perr := models.ParseSupplyChainPathPayload(artifact.Payload); perr == nil && parsedMerged != nil && parsedMerged.Sufficiency != nil {
			artifact.PathStatus = models.EffectivePathStatusFromSufficiency(parsedMerged.Sufficiency)
		}

		if err := artifact.Validate(); err != nil {
			return "", fmt.Errorf("validation error after merge: %w", err)
		}

		if err := b.artifactsRepo.Update(ctx, artifact); err != nil {
			return "", fmt.Errorf("failed to update artifact: %w", err)
		}
		return artifact.ID, nil
	}

	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	id, err := b.artifactsRepo.Create(ctx, artifact)
	if err != nil {
		return "", fmt.Errorf("failed to create artifact: %w", err)
	}

	return id, nil
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
	if len(paths) != len(canonicalKeys) {
		return nil, fmt.Errorf("paths and canonicalKeys must have same length")
	}
	existingMap, err := b.artifactsRepo.BatchGetByCanonicalKeys(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKeys)
	if err != nil {
		return nil, fmt.Errorf("batch get failed: %w", err)
	}
	artifacts := make([]*models.MaterializedArtifact, 0, len(paths))
	for i, path := range paths {
		if path == nil {
			continue
		}
		key := canonicalKeys[i]
		if key == "" {
			continue
		}
		nodes, edges, legs, stress := b.pathToMaterializedPayload(ctx, path)
		sufficiency := b.evaluatePathSufficiency(ctx, path, legs)
		payload := models.BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, path.Family, buildSupplyChainArtifactNotes(path, legs, stress))
		displayName := buildChainDisplayName(path.Segments)
		labels := buildChainLabels(path.Segments)
		nodeRefs := collectNodeRefs(path.Segments)
		parsedPayload, _ := models.ParseSupplyChainPathPayload(payload)
		bindingSummary := models.SummarizeSupplyChainPathBindings(parsedPayload)
		artifact := &models.MaterializedArtifact{
			TenantID:         tenantID,
			LensID:           strings.TrimSpace(lensID),
			Kind:             models.MaterializedKindSupplyChainPath,
			CanonicalKey:     key,
			Payload:          payload,
			PathStatus:       models.EffectivePathStatusFromSufficiency(sufficiency),
			DisplayName:      displayName,
			Labels:           labels,
			NodeRefs:         nodeRefs,
			GeoRefs:          bindingSummary.GeoRefs,
			FlowRefs:         bindingSummary.FlowRefs,
			ZoneRefs:         bindingSummary.ZoneRefs,
			AffectedZoneRefs: bindingSummary.AffectedZoneRefs,
			AnchorSupplier:   strings.TrimSpace(labels["anchorSupplier"]),
			AnchorCustomer:   strings.TrimSpace(labels["anchorCustomer"]),
			ProductAxis:      strings.TrimSpace(labels["productAxis"]),
		}
		models.PopulateMaterializedArtifactIndexes(artifact)
		if existing := existingMap[key]; existing != nil {
			merged := models.MergePayloads(existing.Payload, artifact.Payload)
			artifact.Payload = merged
			artifact.NodeRefs = mergeStringSlices(existing.NodeRefs, artifact.NodeRefs)
			artifact.ID = existing.ID
			artifact.CreatedAt = existing.CreatedAt
			artifact.CreatedBy = existing.CreatedBy
			if existing.PathStatus == "validated" && models.PayloadNodesChanged(existing.Payload, merged) {
				artifact.PathStatus = "hypothesis"
			} else if existing.PathStatus != "" {
				artifact.PathStatus = existing.PathStatus
			}
			artifact.DisplayName = displayName
			artifact.Labels = labels
			if parsedMerged, perr := models.ParseSupplyChainPathPayload(artifact.Payload); perr == nil && parsedMerged != nil && parsedMerged.Sufficiency != nil {
				artifact.PathStatus = models.EffectivePathStatusFromSufficiency(parsedMerged.Sufficiency)
			}
		}
		artifacts = append(artifacts, artifact)
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	for _, a := range artifacts {
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("validation error: %w", err)
		}
	}
	return b.artifactsRepo.BatchUpsert(ctx, artifacts)
}

// FindChainsByNodeRef finds all MaterializedArtifacts (supply chain paths) where the given
// canonical path is a participant. Delegates to the underlying repository.
func (b *SupplyChainPathBuilder) FindChainsByNodeRef(ctx context.Context, tenantID string, nodeRef string) ([]*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil {
		return nil, nil
	}
	return b.artifactsRepo.FindByNodeRef(ctx, tenantID, nodeRef, models.MaterializedKindSupplyChainPath)
}

// GetChainByCanonicalKey returns a MaterializedArtifact by its structural canonical key.
// Returns (nil, nil) when repository is not wired or nothing is found.
func (b *SupplyChainPathBuilder) GetChainByCanonicalKey(ctx context.Context, tenantID, canonicalKey string) (*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil {
		return nil, nil
	}
	return b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKey)
}

// BatchGetChainsByCanonicalKeys returns existing supply-chain artifacts by structural canonical key.
func (b *SupplyChainPathBuilder) BatchGetChainsByCanonicalKeys(ctx context.Context, tenantID string, canonicalKeys []string) (map[string]*models.MaterializedArtifact, error) {
	if b.artifactsRepo == nil || len(canonicalKeys) == 0 {
		return map[string]*models.MaterializedArtifact{}, nil
	}
	return b.artifactsRepo.BatchGetByCanonicalKeys(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKeys)
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
		MaxPaths:                     1,
		RequireProductTransformation: true,
	}

	paths, err := b.BuildUpstreamPaths(ctx, leafCustomerRef, opts)
	if err != nil || len(paths) == 0 {
		return err
	}

	bestPath := paths[0]
	nodes, edges, legs, stress := b.pathToMaterializedPayload(ctx, bestPath)
	rebuildPath := *bestPath
	rebuildPath.BuildMode = "rebuild_from_supply_links"
	rebuildPath.MaxDepth = maxDepth
	sufficiency := b.evaluatePathSufficiency(ctx, bestPath, legs)
	newPayload := models.BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, rebuildPath.Family, buildSupplyChainArtifactNotes(&rebuildPath, legs, stress))

	merged := models.MergePayloads(artifact.Payload, newPayload)
	newNodeRefs := collectNodeRefs(bestPath.Segments)

	artifact.Payload = merged
	artifact.NodeRefs = mergeStringSlices(artifact.NodeRefs, newNodeRefs)
	models.PopulateMaterializedArtifactIndexes(artifact)
	artifact.UpdatedAt = time.Now().UTC()

	if artifact.PathStatus == "validated" && models.PayloadNodesChanged(artifact.Payload, merged) {
		artifact.PathStatus = "hypothesis"
	}
	if parsedMerged, err := models.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsedMerged != nil && parsedMerged.Sufficiency != nil {
		artifact.PathStatus = models.EffectivePathStatusFromSufficiency(parsedMerged.Sufficiency)
	}

	return b.artifactsRepo.Update(ctx, artifact)
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
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainPath, canonicalKey)
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

	nodeIndexMap := make(map[string]int) // canonicalPath -> index

	// Build nodes array and index map
	for _, segment := range path.Segments {
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
	for i, segment := range path.Segments {
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

		binding := binder.BindSegment(ctx, pathbinding.SegmentInput{
			SupplierRef:       segment.SupplierRef,
			CustomerRef:       segment.CustomerRef,
			ProductRef:        segment.ProductRef,
			TransportViaRefs:  segment.TransportViaRefs,
			SegmentConfidence: segment.Confidence,
		})
		leg := models.SupplyChainPathLeg{
			Index:                   i,
			SupplierRef:             segment.SupplierRef,
			CustomerRef:             segment.CustomerRef,
			ProductRef:              segment.ProductRef,
			SupplyLinkCanonicalPath: strings.TrimSpace(segment.SupplyLinkCanonicalPath),
			SupplyLinkID:            strings.TrimSpace(segment.SupplyLinkID),
			SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(segment.SupplyLinkTenantID, segment.TenantID)),
			TransportViaRefs:        append([]string(nil), segment.TransportViaRefs...),
			Mode:                    binding.Mode,
			GeoRefs:                 append([]string(nil), binding.GeoRefs...),
			FlowRefs:                append([]string(nil), binding.FlowRefs...),
			AlternateFlowRefs:       append([]string(nil), binding.AlternateFlowRefs...),
			ZoneRefs:                append([]string(nil), binding.ZoneRefs...),
			AffectedZoneRefs:        append([]string(nil), binding.AffectedZoneRefs...),
			SpatialSemantics:        append([]string(nil), binding.SpatialSemantics...),
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
		if i < len(path.Segments)-1 {
			nextSegment := path.Segments[i+1]
			currentProductIdx := productIdx
			nextProductIdx := nodeIndexMap[nextSegment.ProductRef]

			// Check if products are connected via TRANSFORMS_INTO (optional)
			// For now, we'll add it as "optional" mode
			relation := "TRANSFORMS_INTO"
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
		qualifiers[models.QualifierRouteMode] = strings.TrimSpace(leg.Mode)
	}
	if len(leg.GeoRefs) > 0 {
		qualifiers[models.QualifierGeoRefs] = append([]string(nil), leg.GeoRefs...)
	}
	if len(leg.FlowRefs) > 0 {
		qualifiers[models.QualifierFlowRefs] = append([]string(nil), leg.FlowRefs...)
	}
	if len(leg.AlternateFlowRefs) > 0 {
		qualifiers[models.QualifierAlternateFlowRefs] = append([]string(nil), leg.AlternateFlowRefs...)
	}
	if len(leg.ZoneRefs) > 0 {
		qualifiers[models.QualifierZoneRefs] = append([]string(nil), leg.ZoneRefs...)
	}
	if len(leg.AffectedZoneRefs) > 0 {
		qualifiers[models.QualifierAffectedZoneRefs] = append([]string(nil), leg.AffectedZoneRefs...)
	}
	if len(leg.SpatialSemantics) > 0 {
		qualifiers[models.QualifierSpatialSemantics] = append([]string(nil), leg.SpatialSemantics...)
	}
	if leg.BindingConfidence > 0 {
		qualifiers[models.QualifierBindingConfidence] = leg.BindingConfidence
	}
	if leg.Stress != nil {
		if leg.Stress.StressScore > 0 {
			qualifiers[models.QualifierPathStressScore] = leg.Stress.StressScore
		}
		if leg.Stress.Fragility > 0 {
			qualifiers[models.QualifierPathFragility] = leg.Stress.Fragility
		}
		if leg.Stress.Rerouteability > 0 {
			qualifiers[models.QualifierPathRerouteability] = leg.Stress.Rerouteability
		}
		if leg.Stress.ZonePressure > 0 {
			qualifiers[models.QualifierPathZonePressure] = leg.Stress.ZonePressure
		}
		if leg.Stress.AffectedZonePressure > 0 {
			qualifiers[models.QualifierPathAffectedPressure] = leg.Stress.AffectedZonePressure
		}
		if leg.Stress.RecoveryLatencyHrs > 0 {
			qualifiers[models.QualifierRecoveryLatencyHours] = leg.Stress.RecoveryLatencyHrs
		}
		if len(leg.Stress.DominantFactors) > 0 {
			qualifiers["dominantFactors"] = append([]string(nil), leg.Stress.DominantFactors...)
		}
	}
	if len(segment.TransportViaRefs) > 0 {
		qualifiers["transportViaCount"] = len(segment.TransportViaRefs)
		qualifiers[models.QualifierTransportVia] = append([]string(nil), segment.TransportViaRefs...)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildSupplyChainArtifactNotes(path *SupplyChainPath, legs []models.SupplyChainPathLeg, stress *models.SupplyChainPathStress) map[string]interface{} {
	profile := contractsemantics.ProfileForDomainMode("supply_chain")
	notes := map[string]interface{}{
		"buildMode":          path.BuildMode,
		"maxDepth":           fmt.Sprintf("%d", path.MaxDepth),
		"confidence":         path.Confidence,
		"graphLayer":         string(profile.PrimaryLayer),
		"allowedEdges":       dependencyTypesToStrings(profile.AllowedEdges),
		"pathCarriedEdges":   dependencyTypesToStrings(profile.PathCarriedEdges),
		"overlayEdges":       dependencyTypesToStrings(profile.OverlayEdges),
		"complementaryModes": append([]string(nil), profile.ComplementaryModes...),
		"artifactKind":       string(models.MaterializedKindSupplyChainPath),
		"legCount":           len(legs),
	}
	if stress != nil {
		notes["pathStressScore"] = stress.PathScore
		notes["bottleneckLegIndex"] = stress.BottleneckLegIndex
		notes["recoveryLatencyHours"] = stress.RecoveryLatencyHrs
	}
	return notes
}
