package pathbuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplysemantics"
)

type ProductClosureMode string

const (
	ProductClosureExact      ProductClosureMode = "exact"
	ProductClosureUpstream   ProductClosureMode = "upstream"
	ProductClosureDownstream ProductClosureMode = "downstream"
	ProductClosureBoth       ProductClosureMode = "both"
)

// SupplyChainQueryOptions configure deterministic tracing over canonical SupplyLink fragments.
type SupplyChainQueryOptions struct {
	TenantID               string
	IncludeGlobal          bool
	EffectiveAt            *time.Time
	MaxDepth               int
	MaxPaths               int
	MaxLinks               int
	MaxProductClosureDepth int
	ClosureMode            ProductClosureMode
	SupplierRef            string
	CustomerRef            string
}

func DefaultSupplyChainQueryOptions() SupplyChainQueryOptions {
	return SupplyChainQueryOptions{
		IncludeGlobal:          true,
		MaxDepth:               6,
		MaxPaths:               50,
		MaxLinks:               5000,
		MaxProductClosureDepth: 6,
		ClosureMode:            ProductClosureExact,
	}
}

type SupplyChainTraceResult struct {
	TenantID         string                         `json:"tenantId,omitempty"`
	QueryProductRef  string                         `json:"queryProductRef,omitempty"`
	MatchingProducts []string                       `json:"matchingProducts,omitempty"`
	Suppliers        []string                       `json:"suppliers,omitempty"`
	Customers        []string                       `json:"customers,omitempty"`
	Paths            []*SupplyChainPath             `json:"paths,omitempty"`
	Families         []models.SupplyChainPathFamily `json:"families,omitempty"`
}

// SupplyChainQueryEngine builds deterministic traces over canonical SupplyLink fragments and
// product transformation edges. It does not use LLM output at query time.
type SupplyChainQueryEngine struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	pathBuilder   *SupplyChainPathBuilder
	cache         *pathBuilderCache
}

func NewSupplyChainQueryEngine(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
) *SupplyChainQueryEngine {
	return &SupplyChainQueryEngine{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		pathBuilder:   NewSupplyChainPathBuilder(fragmentsRepo, edgesRepo, nil),
		cache:         newPathBuilderCache(2 * time.Minute),
	}
}

func (e *SupplyChainQueryEngine) WithCacheTTL(ttl time.Duration) *SupplyChainQueryEngine {
	e.cache = newPathBuilderCache(ttl)
	return e
}

func (e *SupplyChainQueryEngine) TraceByProduct(
	ctx context.Context,
	productRef string,
	opts SupplyChainQueryOptions,
) (*SupplyChainTraceResult, error) {
	productRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(productRef))
	if productRef == "" {
		return nil, fmt.Errorf("productRef is required")
	}
	opts = normalizeSupplyChainQueryOptions(opts)
	snapshot, err := e.buildQuerySnapshot(ctx, opts)
	if err != nil {
		return nil, err
	}
	closure, err := e.resolveProductClosure(ctx, []string{productRef}, opts)
	if err != nil {
		return nil, err
	}
	return e.traceWithSnapshot(ctx, snapshot, productRef, closure, opts)
}

func (e *SupplyChainQueryEngine) TraceBetweenActors(
	ctx context.Context,
	supplierRef string,
	customerRef string,
	productRef string,
	opts SupplyChainQueryOptions,
) (*SupplyChainTraceResult, error) {
	opts.SupplierRef = supplierRef
	opts.CustomerRef = customerRef
	return e.TraceByProduct(ctx, productRef, opts)
}

type supplyChainQueryLink struct {
	Key                     string
	SupplyLinkID            string
	SupplyLinkCanonicalPath string
	SupplyLinkTenantID      string
	SupplierRef             string
	ProductRef              string
	CustomerRef             string
	SupplierFacilityRef     *string
	CustomerFacilityRef     *string
	TransportViaRefs        []string
	VesselType              string
	SegmentClass            string
	Confidence              float64
	TenantID                string
	IsTenantSpecific        bool
	Metrics                 map[string]models.MetricValue
	ValidFrom               *time.Time
	ValidTo                 *time.Time
	SupplierKeys            []string
	CustomerKeys            []string
}

type supplyChainQuerySnapshot struct {
	Links             []*supplyChainQueryLink
	ByProduct         map[string][]*supplyChainQueryLink
	BySupplier        map[string][]*supplyChainQueryLink
	ByCustomer        map[string][]*supplyChainQueryLink
	ProductDownstream map[string]map[string]struct{}
	ProductUpstream   map[string]map[string]struct{}
}

func normalizeSupplyChainQueryOptions(opts SupplyChainQueryOptions) SupplyChainQueryOptions {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 6
	}
	if opts.MaxPaths <= 0 {
		opts.MaxPaths = 50
	}
	if opts.MaxLinks <= 0 {
		opts.MaxLinks = 5000
	}
	if opts.MaxProductClosureDepth <= 0 {
		opts.MaxProductClosureDepth = 6
	}
	if opts.ClosureMode == "" {
		opts.ClosureMode = ProductClosureExact
	}
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.SupplierRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(opts.SupplierRef))
	opts.CustomerRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(opts.CustomerRef))
	return opts
}

func (e *SupplyChainQueryEngine) buildQuerySnapshot(ctx context.Context, opts SupplyChainQueryOptions) (*supplyChainQuerySnapshot, error) {
	cacheKey := fmt.Sprintf("query-index:%s|%t|%d", opts.TenantID, opts.IncludeGlobal, opts.MaxLinks)
	if e.cache != nil {
		if v, ok := e.cache.get(cacheKey); ok {
			if snapshot, ok := v.(*supplyChainQuerySnapshot); ok && snapshot != nil {
				return snapshot, nil
			}
		}
	}

	links, err := e.loadSupplyLinksForQuery(ctx, opts)
	if err != nil {
		return nil, err
	}
	snapshot := e.snapshotFromLinks(links)
	if err := e.populateProductAdjacency(ctx, snapshot, opts); err != nil {
		return nil, err
	}
	if e.cache != nil {
		e.cache.set(cacheKey, snapshot)
	}
	return snapshot, nil
}

func (e *SupplyChainQueryEngine) loadSupplyLinksForQuery(ctx context.Context, opts SupplyChainQueryOptions) ([]*models.HorizonFragment, error) {
	if e == nil || e.fragmentsRepo == nil {
		return nil, fmt.Errorf("fragments repository is required")
	}
	type tenantLoad struct {
		tenantID string
		links    []*models.HorizonFragment
	}
	loadOrder := make([]string, 0, 2)
	if opts.TenantID != "" && opts.TenantID != "global" {
		loadOrder = append(loadOrder, opts.TenantID)
		if opts.IncludeGlobal {
			loadOrder = append(loadOrder, "global")
		}
	} else {
		loadOrder = append(loadOrder, "global")
	}

	loaded := make([]tenantLoad, 0, len(loadOrder))
	for _, tenantID := range loadOrder {
		links, err := e.fragmentsRepo.ListByTypeAndTenant(ctx, tenantID, models.FragmentTypeSupplyLink, opts.MaxLinks)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, tenantLoad{tenantID: tenantID, links: links})
	}

	seen := make(map[string]*models.HorizonFragment, len(loaded)*32)
	order := make([]string, 0, len(loaded)*32)
	for _, batch := range loaded {
		for _, link := range batch.links {
			key := models.GetSupplyLinkKey(link)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = link
			order = append(order, key)
		}
	}

	out := make([]*models.HorizonFragment, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return out, nil
}

func (e *SupplyChainQueryEngine) snapshotFromLinks(links []*models.HorizonFragment) *supplyChainQuerySnapshot {
	snapshot := &supplyChainQuerySnapshot{
		Links:             make([]*supplyChainQueryLink, 0, len(links)),
		ByProduct:         make(map[string][]*supplyChainQueryLink),
		BySupplier:        make(map[string][]*supplyChainQueryLink),
		ByCustomer:        make(map[string][]*supplyChainQueryLink),
		ProductDownstream: make(map[string]map[string]struct{}),
		ProductUpstream:   make(map[string]map[string]struct{}),
	}
	for _, raw := range links {
		link := normalizeSupplyChainQueryLink(raw)
		if link == nil {
			continue
		}
		snapshot.Links = append(snapshot.Links, link)
		snapshot.ByProduct[link.ProductRef] = append(snapshot.ByProduct[link.ProductRef], link)
		for _, key := range link.SupplierKeys {
			snapshot.BySupplier[key] = append(snapshot.BySupplier[key], link)
		}
		for _, key := range link.CustomerKeys {
			snapshot.ByCustomer[key] = append(snapshot.ByCustomer[key], link)
		}
	}
	return snapshot
}

func normalizeSupplyChainQueryLink(raw *models.HorizonFragment) *supplyChainQueryLink {
	if raw == nil || raw.Type != models.FragmentTypeSupplyLink || raw.ProductRef == nil {
		return nil
	}
	productRef := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(*raw.ProductRef))
	if productRef == "" {
		return nil
	}
	supplierRef := ""
	if raw.SupplierRef != nil {
		supplierRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(*raw.SupplierRef))
	}
	customerRef := ""
	if raw.CustomerRef != nil {
		customerRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(*raw.CustomerRef))
	}

	var supplierFacilityRef *string
	if raw.SupplierFacilityRef != nil {
		if normalized := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(*raw.SupplierFacilityRef)); normalized != "" {
			supplierFacilityRef = &normalized
		}
	}
	var customerFacilityRef *string
	if raw.CustomerFacilityRef != nil {
		if normalized := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(*raw.CustomerFacilityRef)); normalized != "" {
			customerFacilityRef = &normalized
		}
	}

	transportViaRefs := make([]string, 0, len(raw.TransportViaRefs))
	for _, ref := range raw.TransportViaRefs {
		if normalized := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref)); normalized != "" {
			transportViaRefs = append(transportViaRefs, normalized)
		}
	}

	confidence := 0.5
	metrics := make(map[string]models.MetricValue)
	if raw.State != nil {
		for k, v := range raw.State {
			metrics[k] = v
		}
		if metric, ok := raw.State["confidence"]; ok {
			confidence = metric.Value
		}
	}

	segmentClass := ""
	if raw.Qualifiers != nil {
		if rawClass, ok := raw.Qualifiers["segmentClass"].(string); ok {
			segmentClass = strings.TrimSpace(rawClass)
		}
	}
	if segmentClass == "" {
		segmentClass = supplysemantics.ClassifySegment(supplierRef, productRef, "", customerRef, transportViaRefs).Class
	}
	if !supplysemantics.IsMaterializableSegmentClass(segmentClass) {
		return nil
	}

	return &supplyChainQueryLink{
		Key:                     models.GetSupplyLinkKey(raw),
		SupplyLinkID:            strings.TrimSpace(raw.ID),
		SupplyLinkCanonicalPath: strings.TrimSpace(raw.CanonicalPath),
		SupplyLinkTenantID:      strings.TrimSpace(raw.TenantID),
		SupplierRef:             supplierRef,
		ProductRef:              productRef,
		CustomerRef:             customerRef,
		SupplierFacilityRef:     supplierFacilityRef,
		CustomerFacilityRef:     customerFacilityRef,
		TransportViaRefs:        transportViaRefs,
		VesselType:              extractVesselTypeQualifier(raw.Qualifiers),
		SegmentClass:            segmentClass,
		Confidence:              confidence,
		TenantID:                strings.TrimSpace(raw.TenantID),
		IsTenantSpecific:        strings.TrimSpace(raw.TenantID) != "" && strings.TrimSpace(raw.TenantID) != "global",
		Metrics:                 metrics,
		ValidFrom:               raw.ValidFrom,
		ValidTo:                 raw.ValidTo,
		SupplierKeys:            buildSupplyChainEndpointKeys(supplierRef, supplierFacilityRef),
		CustomerKeys:            buildSupplyChainEndpointKeys(customerRef, customerFacilityRef),
	}
}

func buildSupplyChainEndpointKeys(primary string, facility *string) []string {
	seen := make(map[string]struct{}, 2)
	out := make([]string, 0, 2)
	add := func(value string) {
		value = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(primary)
	if facility != nil {
		add(*facility)
	}
	return out
}

func (e *SupplyChainQueryEngine) populateProductAdjacency(ctx context.Context, snapshot *supplyChainQuerySnapshot, opts SupplyChainQueryOptions) error {
	if snapshot == nil || e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil
	}
	productRefs := make([]string, 0, len(snapshot.ByProduct))
	for productRef := range snapshot.ByProduct {
		productRefs = append(productRefs, productRef)
	}
	fragsByPath, err := e.fragmentsRepo.BatchGetByCanonicalPaths(ctx, productRefs)
	if err != nil {
		return err
	}
	if len(fragsByPath) == 0 {
		return nil
	}
	idToPath := make(map[string]string, len(fragsByPath))
	sourceIDs := make([]string, 0, len(fragsByPath))
	for path, frag := range fragsByPath {
		if frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		idToPath[strings.TrimSpace(frag.ID)] = strings.TrimSpace(path)
		sourceIDs = append(sourceIDs, strings.TrimSpace(frag.ID))
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	tenantID := strings.TrimSpace(opts.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	downstreamEdges := make([]*models.DependencyEdge, 0)
	upstreamEdges := make([]*models.DependencyEdge, 0)
	for _, edgeType := range models.ProductTransitionEdgeTypes() {
		typedDownstream, err := e.edgesRepo.ListBySourceIDsAndType(ctx, tenantID, sourceIDs, edgeType)
		if err != nil {
			return err
		}
		downstreamEdges = append(downstreamEdges, typedDownstream...)
		typedUpstream, err := e.edgesRepo.ListByTargetIDsAndType(ctx, tenantID, sourceIDs, edgeType)
		if err != nil {
			return err
		}
		upstreamEdges = append(upstreamEdges, typedUpstream...)
	}
	relatedIDs := make(map[string]struct{}, len(downstreamEdges)+len(upstreamEdges))
	for _, edge := range downstreamEdges {
		if edge != nil {
			relatedIDs[strings.TrimSpace(edge.TargetFragmentID)] = struct{}{}
		}
	}
	for _, edge := range upstreamEdges {
		if edge != nil {
			relatedIDs[strings.TrimSpace(edge.SourceFragmentID)] = struct{}{}
		}
	}
	missingIDs := make([]string, 0, len(relatedIDs))
	for id := range relatedIDs {
		if id == "" {
			continue
		}
		if _, ok := idToPath[id]; ok {
			continue
		}
		missingIDs = append(missingIDs, id)
	}
	if len(missingIDs) > 0 {
		fragsByID, err := e.fragmentsRepo.GetMany(ctx, missingIDs)
		if err != nil {
			return err
		}
		for id, frag := range fragsByID {
			if frag == nil {
				continue
			}
			idToPath[strings.TrimSpace(id)] = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(frag.CanonicalPath))
		}
	}
	for _, edge := range downstreamEdges {
		if edge == nil {
			continue
		}
		sourcePath := idToPath[strings.TrimSpace(edge.SourceFragmentID)]
		targetPath := idToPath[strings.TrimSpace(edge.TargetFragmentID)]
		if sourcePath == "" || targetPath == "" {
			continue
		}
		ensureStringSet(snapshot.ProductDownstream, sourcePath)[targetPath] = struct{}{}
		ensureStringSet(snapshot.ProductUpstream, targetPath)[sourcePath] = struct{}{}
	}
	for _, edge := range upstreamEdges {
		if edge == nil {
			continue
		}
		sourcePath := idToPath[strings.TrimSpace(edge.SourceFragmentID)]
		targetPath := idToPath[strings.TrimSpace(edge.TargetFragmentID)]
		if sourcePath == "" || targetPath == "" {
			continue
		}
		ensureStringSet(snapshot.ProductDownstream, sourcePath)[targetPath] = struct{}{}
		ensureStringSet(snapshot.ProductUpstream, targetPath)[sourcePath] = struct{}{}
	}
	return nil
}

func ensureStringSet(dst map[string]map[string]struct{}, key string) map[string]struct{} {
	if existing, ok := dst[key]; ok {
		return existing
	}
	created := make(map[string]struct{})
	dst[key] = created
	return created
}

func (e *SupplyChainQueryEngine) resolveProductClosure(
	ctx context.Context,
	rootProducts []string,
	opts SupplyChainQueryOptions,
) (map[string]struct{}, error) {
	closure := make(map[string]struct{}, len(rootProducts))
	if len(rootProducts) == 0 {
		return closure, nil
	}
	for _, productRef := range rootProducts {
		productRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(productRef))
		if productRef != "" {
			closure[productRef] = struct{}{}
		}
	}
	if opts.ClosureMode == ProductClosureExact || e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return closure, nil
	}
	fragsByPath, err := e.fragmentsRepo.BatchGetByCanonicalPaths(ctx, rootProducts)
	if err != nil {
		return nil, err
	}
	if len(fragsByPath) == 0 {
		return closure, nil
	}
	idToPath := make(map[string]string, len(fragsByPath))
	visited := make(map[string]struct{}, len(fragsByPath))
	frontier := make([]string, 0, len(fragsByPath))
	for path, frag := range fragsByPath {
		if frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		id := strings.TrimSpace(frag.ID)
		idToPath[id] = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(path))
		visited[id] = struct{}{}
		frontier = append(frontier, id)
	}
	if len(frontier) == 0 {
		return closure, nil
	}

	for depth := 0; depth < opts.MaxProductClosureDepth && len(frontier) > 0; depth++ {
		nextIDs := make(map[string]struct{})
		if opts.ClosureMode == ProductClosureDownstream || opts.ClosureMode == ProductClosureBoth {
			for _, edgeType := range models.ProductTransitionEdgeTypes() {
				edges, err := e.edgesRepo.ListBySourceIDsAndType(ctx, "global", frontier, edgeType)
				if err != nil {
					return nil, err
				}
				for _, edge := range edges {
					if edge == nil {
						continue
					}
					targetID := strings.TrimSpace(edge.TargetFragmentID)
					if targetID != "" {
						nextIDs[targetID] = struct{}{}
					}
				}
			}
		}
		if opts.ClosureMode == ProductClosureUpstream || opts.ClosureMode == ProductClosureBoth {
			for _, edgeType := range models.ProductTransitionEdgeTypes() {
				edges, err := e.edgesRepo.ListByTargetIDsAndType(ctx, "global", frontier, edgeType)
				if err != nil {
					return nil, err
				}
				for _, edge := range edges {
					if edge == nil {
						continue
					}
					sourceID := strings.TrimSpace(edge.SourceFragmentID)
					if sourceID != "" {
						nextIDs[sourceID] = struct{}{}
					}
				}
			}
		}
		if len(nextIDs) == 0 {
			break
		}
		missing := make([]string, 0, len(nextIDs))
		nextFrontier := make([]string, 0, len(nextIDs))
		for id := range nextIDs {
			if _, ok := visited[id]; ok {
				continue
			}
			visited[id] = struct{}{}
			nextFrontier = append(nextFrontier, id)
			if _, ok := idToPath[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			fragsByID, err := e.fragmentsRepo.GetMany(ctx, missing)
			if err != nil {
				return nil, err
			}
			for id, frag := range fragsByID {
				if frag == nil {
					continue
				}
				idToPath[strings.TrimSpace(id)] = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(frag.CanonicalPath))
			}
		}
		for _, id := range nextFrontier {
			if path := idToPath[id]; path != "" {
				closure[path] = struct{}{}
			}
		}
		frontier = nextFrontier
	}
	return closure, nil
}

func (e *SupplyChainQueryEngine) traceWithSnapshot(
	ctx context.Context,
	snapshot *supplyChainQuerySnapshot,
	queryProductRef string,
	closure map[string]struct{},
	opts SupplyChainQueryOptions,
) (*SupplyChainTraceResult, error) {
	if snapshot == nil {
		return &SupplyChainTraceResult{}, nil
	}
	candidates := e.filterCandidateLinks(snapshot, closure, opts)
	paths := e.buildCandidatePaths(candidates, snapshot, opts)
	e.annotatePaths(ctx, paths)
	families := buildSupplyChainQueryFamilies(paths)
	return &SupplyChainTraceResult{
		TenantID:         opts.TenantID,
		QueryProductRef:  queryProductRef,
		MatchingProducts: sortedStringKeys(closure),
		Suppliers:        collectTraceEndpoints(paths, true),
		Customers:        collectTraceEndpoints(paths, false),
		Paths:            paths,
		Families:         families,
	}, nil
}

func (e *SupplyChainQueryEngine) filterCandidateLinks(
	snapshot *supplyChainQuerySnapshot,
	closure map[string]struct{},
	opts SupplyChainQueryOptions,
) []*supplyChainQueryLink {
	if snapshot == nil {
		return nil
	}
	candidates := make([]*supplyChainQueryLink, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		if link == nil {
			continue
		}
		if _, ok := closure[link.ProductRef]; !ok {
			continue
		}
		if opts.EffectiveAt != nil {
			if link.ValidFrom != nil && link.ValidFrom.After(*opts.EffectiveAt) {
				continue
			}
			if link.ValidTo != nil && link.ValidTo.Before(*opts.EffectiveAt) {
				continue
			}
		}
		candidates = append(candidates, link)
	}
	return candidates
}

func (e *SupplyChainQueryEngine) buildCandidatePaths(
	candidates []*supplyChainQueryLink,
	snapshot *supplyChainQuerySnapshot,
	opts SupplyChainQueryOptions,
) []*SupplyChainPath {
	if len(candidates) == 0 {
		return nil
	}
	candidateByKey := make(map[string]*supplyChainQueryLink, len(candidates))
	bySupplier := make(map[string][]*supplyChainQueryLink)
	byCustomer := make(map[string][]*supplyChainQueryLink)
	for _, link := range candidates {
		candidateByKey[link.Key] = link
		for _, key := range link.SupplierKeys {
			bySupplier[key] = append(bySupplier[key], link)
		}
		for _, key := range link.CustomerKeys {
			byCustomer[key] = append(byCustomer[key], link)
		}
	}

	startLinks := e.selectStartLinks(candidates, byCustomer, snapshot, opts)
	if len(startLinks) == 0 {
		startLinks = candidates
	}
	sort.SliceStable(startLinks, func(i, j int) bool {
		return startLinks[i].Confidence > startLinks[j].Confidence
	})

	results := make([]*SupplyChainPath, 0, minInt(len(startLinks), opts.MaxPaths))
	seen := make(map[string]struct{}, opts.MaxPaths)
	for _, start := range startLinks {
		if len(results) >= opts.MaxPaths {
			break
		}
		e.walkCandidatePaths(start, snapshot, bySupplier, opts, map[string]struct{}{}, nil, 1.0, &results, seen)
	}

	if opts.CustomerRef != "" {
		filtered := make([]*SupplyChainPath, 0, len(results))
		for _, path := range results {
			if path == nil || len(path.Segments) == 0 {
				continue
			}
			last := path.Segments[len(path.Segments)-1]
			if canonical.NormalizeCanonicalPathForPersist(last.CustomerRef) == opts.CustomerRef {
				filtered = append(filtered, path)
			}
		}
		results = filtered
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Confidence == results[j].Confidence {
			return len(results[i].Segments) < len(results[j].Segments)
		}
		return results[i].Confidence > results[j].Confidence
	})
	if len(results) > opts.MaxPaths {
		results = results[:opts.MaxPaths]
	}
	return results
}

func (e *SupplyChainQueryEngine) selectStartLinks(
	candidates []*supplyChainQueryLink,
	byCustomer map[string][]*supplyChainQueryLink,
	snapshot *supplyChainQuerySnapshot,
	opts SupplyChainQueryOptions,
) []*supplyChainQueryLink {
	if opts.SupplierRef != "" {
		filtered := make([]*supplyChainQueryLink, 0, len(candidates))
		for _, link := range candidates {
			if link != nil && containsString(link.SupplierKeys, opts.SupplierRef) {
				filtered = append(filtered, link)
			}
		}
		return filtered
	}
	starts := make([]*supplyChainQueryLink, 0, len(candidates))
	for _, link := range candidates {
		if link == nil {
			continue
		}
		if !e.hasPredecessor(link, byCustomer, snapshot) {
			starts = append(starts, link)
		}
	}
	return starts
}

func (e *SupplyChainQueryEngine) hasPredecessor(
	link *supplyChainQueryLink,
	byCustomer map[string][]*supplyChainQueryLink,
	snapshot *supplyChainQuerySnapshot,
) bool {
	for _, key := range link.SupplierKeys {
		for _, prev := range byCustomer[key] {
			if prev == nil || prev.Key == link.Key {
				continue
			}
			if !linksCanConnect(prev, link, snapshot) {
				continue
			}
			return true
		}
	}
	return false
}

func (e *SupplyChainQueryEngine) walkCandidatePaths(
	current *supplyChainQueryLink,
	snapshot *supplyChainQuerySnapshot,
	bySupplier map[string][]*supplyChainQueryLink,
	opts SupplyChainQueryOptions,
	visited map[string]struct{},
	segments []PathSegment,
	confidence float64,
	results *[]*SupplyChainPath,
	seen map[string]struct{},
) {
	if current == nil || len(*results) >= opts.MaxPaths {
		return
	}
	if _, ok := visited[current.Key]; ok {
		return
	}
	visited[current.Key] = struct{}{}
	defer delete(visited, current.Key)

	segment := queryLinkToPathSegment(current)
	segments = append(append([]PathSegment(nil), segments...), segment)
	confidence *= current.Confidence

	if len(segments) >= opts.MaxDepth {
		appendCandidatePath(segments, confidence, opts.MaxDepth, results, seen)
		return
	}

	nextLinks := make([]*supplyChainQueryLink, 0, 8)
	nextSeen := make(map[string]struct{})
	for _, key := range current.CustomerKeys {
		for _, next := range bySupplier[key] {
			if next == nil || next.Key == current.Key {
				continue
			}
			if _, ok := visited[next.Key]; ok {
				continue
			}
			if !linksCanConnect(current, next, snapshot) {
				continue
			}
			if _, ok := nextSeen[next.Key]; ok {
				continue
			}
			nextSeen[next.Key] = struct{}{}
			nextLinks = append(nextLinks, next)
		}
	}
	sort.SliceStable(nextLinks, func(i, j int) bool {
		return nextLinks[i].Confidence > nextLinks[j].Confidence
	})
	if len(nextLinks) == 0 {
		appendCandidatePath(segments, confidence, opts.MaxDepth, results, seen)
		return
	}
	for _, next := range nextLinks {
		if len(*results) >= opts.MaxPaths {
			return
		}
		e.walkCandidatePaths(next, snapshot, bySupplier, opts, visited, segments, confidence, results, seen)
	}
}

func linksCanConnect(current *supplyChainQueryLink, next *supplyChainQueryLink, snapshot *supplyChainQuerySnapshot) bool {
	if current == nil || next == nil {
		return false
	}
	if !overlapExists(current.CustomerKeys, next.SupplierKeys) {
		return false
	}
	if current.ProductRef == next.ProductRef {
		return true
	}
	if snapshot == nil {
		return false
	}
	if downstream := snapshot.ProductDownstream[current.ProductRef]; len(downstream) > 0 {
		if _, ok := downstream[next.ProductRef]; ok {
			return true
		}
	}
	return false
}

func queryLinkToPathSegment(link *supplyChainQueryLink) PathSegment {
	if link == nil {
		return PathSegment{}
	}
	return PathSegment{
		SupplyLinkID:            link.SupplyLinkID,
		SupplyLinkCanonicalPath: link.SupplyLinkCanonicalPath,
		SupplyLinkTenantID:      link.SupplyLinkTenantID,
		SupplierRef:             link.SupplierRef,
		ProductRef:              link.ProductRef,
		CustomerRef:             link.CustomerRef,
		SupplierFacilityRef:     copyStringPtr(link.SupplierFacilityRef),
		CustomerFacilityRef:     copyStringPtr(link.CustomerFacilityRef),
		TransportViaRefs:        append([]string(nil), link.TransportViaRefs...),
		VesselType:              strings.TrimSpace(link.VesselType),
		SegmentClass:            link.SegmentClass,
		Confidence:              link.Confidence,
		TenantID:                link.TenantID,
		IsTenantSpecific:        link.IsTenantSpecific,
		Metrics:                 cloneMetricMap(link.Metrics),
	}
}

func cloneMetricMap(in map[string]models.MetricValue) map[string]models.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	value := strings.TrimSpace(*in)
	return &value
}

func appendCandidatePath(
	segments []PathSegment,
	confidence float64,
	maxDepth int,
	results *[]*SupplyChainPath,
	seen map[string]struct{},
) {
	if len(segments) == 0 {
		return
	}
	keyParts := make([]string, 0, len(segments))
	for _, segment := range segments {
		keyParts = append(keyParts, strings.TrimSpace(segment.SupplyLinkCanonicalPath))
	}
	key := strings.Join(keyParts, "->")
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*results = append(*results, &SupplyChainPath{
		Segments:   append([]PathSegment(nil), segments...),
		Confidence: confidence,
		BuildMode:  "deterministic_query",
		MaxDepth:   maxDepth,
	})
}

func (e *SupplyChainQueryEngine) annotatePaths(ctx context.Context, paths []*SupplyChainPath) {
	if e == nil || e.pathBuilder == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return
	}
	for _, path := range paths {
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		_, _, legs, _ := e.pathBuilder.pathToMaterializedPayload(ctx, path)
		sufficiency := e.pathBuilder.evaluatePathSufficiency(ctx, path, legs)
		if sufficiency == nil {
			continue
		}
		path.JoinAssessmentHints = append([]models.SupplyChainJoinAssessment(nil), sufficiency.JoinAssessments...)
		path.OpenGaps = append([]string(nil), sufficiency.OpenGaps...)
	}
}

func buildSupplyChainQueryFamilies(paths []*SupplyChainPath) []models.SupplyChainPathFamily {
	if len(paths) == 0 {
		return nil
	}
	type agg struct {
		family models.SupplyChainPathFamily
		gaps   map[string]struct{}
	}
	groups := make(map[string]*agg)
	for _, path := range paths {
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		first := path.Segments[0]
		last := path.Segments[len(path.Segments)-1]
		key := fmt.Sprintf("%s|%s|%s", first.SupplierRef, last.CustomerRef, last.ProductRef)
		group := groups[key]
		if group == nil {
			group = &agg{
				family: models.SupplyChainPathFamily{
					ID:             key,
					AnchorSupplier: first.SupplierRef,
					AnchorCustomer: last.CustomerRef,
					ProductAxis:    last.ProductRef,
				},
				gaps: make(map[string]struct{}),
			}
			groups[key] = group
		}
		group.family.PathCount++
		status := inferPathStatusFromHints(path)
		switch status {
		case models.SupplyChainSufficiencyValidated:
			group.family.ValidatedPathCount++
		case models.SupplyChainSufficiencyForensicGap:
			group.family.GapPathCount++
		default:
			group.family.HeuristicPathCount++
		}
		for _, gap := range path.OpenGaps {
			gap = strings.TrimSpace(gap)
			if gap == "" {
				continue
			}
			group.gaps[gap] = struct{}{}
		}
	}
	out := make([]models.SupplyChainPathFamily, 0, len(groups))
	for _, group := range groups {
		group.family.OpenGaps = sortedStringKeys(group.gaps)
		out = append(out, group.family)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ValidatedPathCount == out[j].ValidatedPathCount {
			return out[i].PathCount > out[j].PathCount
		}
		return out[i].ValidatedPathCount > out[j].ValidatedPathCount
	})
	byID := make(map[string]models.SupplyChainPathFamily, len(out))
	for _, family := range out {
		byID[family.ID] = family
	}
	for _, path := range paths {
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		first := path.Segments[0]
		last := path.Segments[len(path.Segments)-1]
		key := fmt.Sprintf("%s|%s|%s", first.SupplierRef, last.CustomerRef, last.ProductRef)
		if family, ok := byID[key]; ok {
			f := family
			path.Family = &f
		}
	}
	return out
}

func inferPathStatusFromHints(path *SupplyChainPath) string {
	if path == nil {
		return models.SupplyChainSufficiencyHeuristic
	}
	hasGap := false
	hasHeuristic := len(path.OpenGaps) > 0
	for _, hint := range path.JoinAssessmentHints {
		switch strings.TrimSpace(hint.Status) {
		case models.SupplyChainSufficiencyForensicGap:
			hasGap = true
		case models.SupplyChainSufficiencyValidated:
		default:
			hasHeuristic = true
		}
	}
	switch {
	case hasGap:
		return models.SupplyChainSufficiencyForensicGap
	case hasHeuristic:
		return models.SupplyChainSufficiencyHeuristic
	default:
		return models.SupplyChainSufficiencyValidated
	}
}

func collectTraceEndpoints(paths []*SupplyChainPath, suppliers bool) []string {
	seen := make(map[string]struct{})
	for _, path := range paths {
		if path == nil || len(path.Segments) == 0 {
			continue
		}
		if suppliers {
			if value := strings.TrimSpace(path.Segments[0].SupplierRef); value != "" {
				seen[value] = struct{}{}
			}
			continue
		}
		if value := strings.TrimSpace(path.Segments[len(path.Segments)-1].CustomerRef); value != "" {
			seen[value] = struct{}{}
		}
	}
	return sortedStringKeys(seen)
}

func sortedStringKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func overlapExists(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
