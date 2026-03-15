package pathbuilder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type SupplyChainAssemblyOptions struct {
	TenantID                        string
	ProjectID                       string
	LensID                          string
	AnchorSet                       []string
	RawNodeRefs                     []string
	RawNodeLabels                   map[string]string
	RawEdges                        []SupplyChainAssemblyRawEdge
	SourceAnchors                   []string
	BackgroundContinuationScheduled bool
	BackgroundContinuationJobIDs    []string
	GeoFilters                      []string
	MaxDepth                        int
	MaxPaths                        int
	MaxLinks                        int
	MaxProductNodes                 int
	MaxActorNodes                   int
	MaxTransformsPerProduct         int
	MaxSuppliersPerProduct          int
	MaxDerivedPaths                 int
}

type SupplyChainAssemblyRawEdge struct {
	Source   string
	Target   string
	EdgeType string
}

func DefaultSupplyChainAssemblyOptions() SupplyChainAssemblyOptions {
	return SupplyChainAssemblyOptions{
		MaxDepth:                6,
		MaxPaths:                12,
		MaxLinks:                5000,
		MaxProductNodes:         80,
		MaxActorNodes:           120,
		MaxTransformsPerProduct: 12,
		MaxSuppliersPerProduct:  20,
		MaxDerivedPaths:         12,
	}
}

type SupplyChainAssemblyResult struct {
	Artifact                        *models.MaterializedArtifact
	DerivedPathKeys                 []string
	AnchorSet                       []string
	ResolvedAnchors                 []string
	UnresolvedAnchors               []string
	AnchorProducts                  []string
	OpenGaps                        []string
	Coverage                        *models.SupplyChainAssemblyCoverage
	Status                          string
	Stage                           string
	SourceMode                      string
	Provisional                     bool
	SkipReason                      string
	TracedPathsCount                int
	BackgroundContinuationScheduled bool
	BackgroundContinuationJobIDs    []string
}

type SupplyChainAssemblyBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	artifactsRepo *repo.MaterializedArtifactsRepository
	pathBuilder   *SupplyChainPathBuilder
	queryEngine   *SupplyChainQueryEngine
}

func NewSupplyChainAssemblyBuilder(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	pathBuilder *SupplyChainPathBuilder,
) *SupplyChainAssemblyBuilder {
	var artifactsRepo *repo.MaterializedArtifactsRepository
	if pathBuilder != nil {
		artifactsRepo = pathBuilder.artifactsRepo
	}
	return &SupplyChainAssemblyBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		artifactsRepo: artifactsRepo,
		pathBuilder:   pathBuilder,
		queryEngine:   NewSupplyChainQueryEngine(fragmentsRepo, edgesRepo),
	}
}

type assemblyContribution struct {
	canonicalKey string
	parsed       *models.SupplyChainPathPayload
}

func (b *SupplyChainAssemblyBuilder) BuildAndMaterialize(ctx context.Context, opts SupplyChainAssemblyOptions) (*SupplyChainAssemblyResult, error) {
	if b == nil || b.fragmentsRepo == nil || b.edgesRepo == nil || b.pathBuilder == nil || b.artifactsRepo == nil || b.queryEngine == nil {
		return nil, fmt.Errorf("supply chain assembly builder repos are not initialized")
	}
	opts = normalizeSupplyChainAssemblyOptions(opts)

	traceOpts := DefaultSupplyChainQueryOptions()
	traceOpts.TenantID = opts.TenantID
	traceOpts.IncludeGlobal = true
	traceOpts.MaxDepth = opts.MaxDepth
	traceOpts.MaxPaths = opts.MaxPaths
	traceOpts.MaxLinks = opts.MaxLinks
	traceOpts.MaxProductClosureDepth = opts.MaxDepth
	traceOpts.ClosureMode = ProductClosureUpstream

	snapshot, err := b.queryEngine.buildQuerySnapshot(ctx, traceOpts)
	if err != nil {
		return nil, err
	}
	resolvedAnchors, unresolvedAnchors := resolveAssemblyAnchors(opts.AnchorSet, snapshot, opts.RawNodeRefs)
	if len(resolvedAnchors) == 0 {
		return &SupplyChainAssemblyResult{Status: "empty", SkipReason: "no_final_product_anchor"}, nil
	}
	productAnchors := filterAssemblyProductRefs(resolvedAnchors)

	existingArtifacts, err := b.loadExistingPathArtifacts(ctx, opts.TenantID, resolvedAnchors)
	if err != nil {
		return nil, err
	}
	hasRawGraph := len(opts.RawNodeRefs) > 0 || len(opts.RawEdges) > 0
	if len(snapshot.Links) == 0 && len(existingArtifacts) == 0 && !hasRawGraph {
		return &SupplyChainAssemblyResult{
			Status:            "empty",
			SkipReason:        "no_materialized_supply_links",
			AnchorSet:         resolvedAnchors,
			ResolvedAnchors:   resolvedAnchors,
			UnresolvedAnchors: unresolvedAnchors,
			AnchorProducts:    productAnchors,
		}, nil
	}

	contributions := make([]assemblyContribution, 0, len(existingArtifacts)+opts.MaxDerivedPaths)
	seenContribution := make(map[string]struct{})
	derivedPathKeys := make([]string, 0, opts.MaxDerivedPaths)
	for _, artifact := range existingArtifacts {
		if artifact == nil {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		key := strings.TrimSpace(artifact.CanonicalKey)
		if key == "" {
			continue
		}
		if _, ok := seenContribution[key]; ok {
			continue
		}
		seenContribution[key] = struct{}{}
		derivedPathKeys = append(derivedPathKeys, key)
		contributions = append(contributions, assemblyContribution{canonicalKey: key, parsed: parsed})
	}

	tracedPaths := make([]*SupplyChainPath, 0, opts.MaxDerivedPaths)
	for _, anchor := range productAnchors {
		traceResult, err := b.queryEngine.TraceByProduct(ctx, anchor, traceOpts)
		if err != nil || traceResult == nil {
			continue
		}
		for _, path := range traceResult.Paths {
			if path == nil || len(path.Segments) == 0 {
				continue
			}
			tracedPaths = append(tracedPaths, path)
			if len(tracedPaths) >= opts.MaxDerivedPaths {
				break
			}
		}
		if len(tracedPaths) >= opts.MaxDerivedPaths {
			break
		}
	}
	if len(tracedPaths) > 0 {
		keys := make([]string, 0, len(tracedPaths))
		pathsToMaterialize := make([]*SupplyChainPath, 0, len(tracedPaths))
		for _, path := range tracedPaths {
			key := strings.TrimSpace(BuildStructuralCanonicalKey(traceOpts.TenantIDOrGlobal(), path.Segments))
			if key == "" {
				continue
			}
			if _, ok := seenContribution[key]; ok {
				continue
			}
			nodes, edges, legs, stress := b.pathBuilder.pathToMaterializedPayload(ctx, path)
			parsed := &models.SupplyChainPathPayload{
				Version:     1,
				Nodes:       nodes,
				Edges:       edges,
				Legs:        legs,
				Stress:      stress,
				Sufficiency: b.pathBuilder.evaluatePathSufficiency(ctx, path, legs),
				Family:      path.Family,
				Notes:       buildSupplyChainArtifactNotes(path, legs, stress),
			}
			contributions = append(contributions, assemblyContribution{canonicalKey: key, parsed: parsed})
			pathsToMaterialize = append(pathsToMaterialize, path)
			keys = append(keys, key)
			seenContribution[key] = struct{}{}
		}
		if len(pathsToMaterialize) > 0 {
			if _, err := b.pathBuilder.MaterializePathsBatch(ctx, pathsToMaterialize, traceOpts.TenantIDOrGlobal(), opts.LensID, keys); err != nil {
				return nil, err
			}
			derivedPathKeys = append(derivedPathKeys, keys...)
		}
	}
	derivedPathKeys = uniqueSortedStrings(derivedPathKeys)

	if len(contributions) == 0 && !hasRawGraph {
		return &SupplyChainAssemblyResult{
			Status:            "empty",
			SkipReason:        "assembly_no_reachable_graph",
			AnchorSet:         resolvedAnchors,
			ResolvedAnchors:   resolvedAnchors,
			UnresolvedAnchors: unresolvedAnchors,
			AnchorProducts:    productAnchors,
		}, nil
	}

	payload, labels, nodeRefs, coverage, openGaps := buildAssemblyPayloadFromInputs(resolvedAnchors, contributions, opts)
	if payload == nil || coverage == nil || (coverage.ProductsN == 0 && coverage.ActorsN == 0) {
		return &SupplyChainAssemblyResult{
			Status:            "empty",
			SkipReason:        "assembly_no_reachable_graph",
			AnchorSet:         resolvedAnchors,
			ResolvedAnchors:   resolvedAnchors,
			UnresolvedAnchors: unresolvedAnchors,
			AnchorProducts:    productAnchors,
			OpenGaps:          openGaps,
		}, nil
	}
	payload["derivedPathKeys"] = derivedPathKeys
	if len(unresolvedAnchors) > 0 {
		payload["unresolvedAnchors"] = append([]string(nil), unresolvedAnchors...)
	}
	status := "complete"
	if coverage.Truncated || len(openGaps) > 0 {
		status = "partial"
	}
	sourceMode := determineAssemblySourceMode(len(contributions) > 0, hasRawGraph)
	stage := "refined"
	provisional := false
	if sourceMode != "supply_links" {
		stage = "fast"
		provisional = true
	}
	payload["stage"] = stage
	payload["provisional"] = provisional
	payload["sourceMode"] = sourceMode
	if opts.BackgroundContinuationScheduled {
		payload["backgroundContinuationScheduled"] = true
	}
	if len(opts.BackgroundContinuationJobIDs) > 0 {
		payload["backgroundContinuationJobIds"] = append([]string(nil), opts.BackgroundContinuationJobIDs...)
	}
	if coverage != nil {
		coverage.DerivedPathsN = len(derivedPathKeys)
	}
	canonicalKey := buildSupplyChainAssemblyCanonicalKey(traceOpts.TenantIDOrGlobal(), resolvedAnchors, opts.GeoFilters)
	displayName := buildSupplyChainAssemblyDisplayName(resolvedAnchors)
	artifact := &models.MaterializedArtifact{
		TenantID:     traceOpts.TenantIDOrGlobal(),
		ProjectID:    opts.ProjectID,
		LensID:       opts.LensID,
		Kind:         models.MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: canonicalKey,
		Payload:      payload,
		Status:       "active",
		PathStatus:   status,
		DisplayName:  displayName,
		Labels:       labels,
		NodeRefs:     nodeRefs,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		CreatedBy:    "supply_chain_assembly_builder",
		UpdatedBy:    "supply_chain_assembly_builder",
	}
	models.PopulateMaterializedArtifactIndexes(artifact)
	if existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, artifact.TenantID, artifact.Kind, artifact.CanonicalKey); err == nil && existing != nil {
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		artifact.CreatedBy = existing.CreatedBy
	}
	if err := b.artifactsRepo.Upsert(ctx, artifact); err != nil {
		return nil, err
	}
	if artifact.ID == "" {
		if stored, err := b.artifactsRepo.GetByCanonicalKey(ctx, artifact.TenantID, artifact.Kind, artifact.CanonicalKey); err == nil && stored != nil {
			artifact.ID = stored.ID
		}
	}
	return &SupplyChainAssemblyResult{
		Artifact:                        artifact,
		DerivedPathKeys:                 derivedPathKeys,
		AnchorSet:                       resolvedAnchors,
		ResolvedAnchors:                 resolvedAnchors,
		UnresolvedAnchors:               unresolvedAnchors,
		AnchorProducts:                  productAnchors,
		OpenGaps:                        openGaps,
		Coverage:                        coverage,
		Status:                          status,
		Stage:                           stage,
		SourceMode:                      sourceMode,
		Provisional:                     provisional,
		TracedPathsCount:                len(tracedPaths),
		BackgroundContinuationScheduled: opts.BackgroundContinuationScheduled,
		BackgroundContinuationJobIDs:    append([]string(nil), opts.BackgroundContinuationJobIDs...),
	}, nil
}

func normalizeSupplyChainAssemblyOptions(opts SupplyChainAssemblyOptions) SupplyChainAssemblyOptions {
	defaults := DefaultSupplyChainAssemblyOptions()
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaults.MaxDepth
	}
	if opts.MaxPaths <= 0 {
		opts.MaxPaths = defaults.MaxPaths
	}
	if opts.MaxLinks <= 0 {
		opts.MaxLinks = defaults.MaxLinks
	}
	if opts.MaxProductNodes <= 0 {
		opts.MaxProductNodes = defaults.MaxProductNodes
	}
	if opts.MaxActorNodes <= 0 {
		opts.MaxActorNodes = defaults.MaxActorNodes
	}
	if opts.MaxTransformsPerProduct <= 0 {
		opts.MaxTransformsPerProduct = defaults.MaxTransformsPerProduct
	}
	if opts.MaxSuppliersPerProduct <= 0 {
		opts.MaxSuppliersPerProduct = defaults.MaxSuppliersPerProduct
	}
	if opts.MaxDerivedPaths <= 0 {
		opts.MaxDerivedPaths = defaults.MaxDerivedPaths
	}
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	if opts.TenantID == "" {
		opts.TenantID = "global"
	}
	opts.AnchorSet = uniqueSortedStrings(opts.AnchorSet)
	if len(opts.SourceAnchors) == 0 {
		opts.SourceAnchors = append([]string(nil), opts.AnchorSet...)
	}
	opts.SourceAnchors = uniqueSortedStrings(opts.SourceAnchors)
	opts.RawNodeRefs = uniqueSortedStrings(opts.RawNodeRefs)
	opts.GeoFilters = uniqueSortedStrings(opts.GeoFilters)
	return opts
}

func (opts SupplyChainQueryOptions) TenantIDOrGlobal() string {
	if strings.TrimSpace(opts.TenantID) == "" {
		return "global"
	}
	return strings.TrimSpace(opts.TenantID)
}

func resolveAssemblyAnchors(values []string, snapshot *supplyChainQuerySnapshot, rawNodeRefs []string) ([]string, []string) {
	candidateRefs := collectAssemblyCandidateRefs(snapshot, rawNodeRefs)
	out := make([]string, 0, len(values))
	unresolved := make([]string, 0)
	seen := map[string]struct{}{}
	for _, raw := range values {
		raw = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(raw))
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "/") {
			if _, ok := seen[raw]; !ok {
				seen[raw] = struct{}{}
				out = append(out, raw)
			}
			continue
		}
		slug := strings.TrimSpace(strings.ToLower(taxonomy.Slugify(raw)))
		if slug == "" {
			continue
		}
		matched := false
		for _, ref := range candidateRefs {
			match := strings.Contains(strings.ToLower(ref), "/"+slug) || strings.Contains(strings.ToLower(ref), slug)
			if !match {
				continue
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			out = append(out, ref)
			matched = true
		}
		if !matched {
			unresolved = append(unresolved, raw)
		}
	}
	return uniqueSortedStrings(out), uniqueSortedStrings(unresolved)
}

func collectAssemblyCandidateRefs(snapshot *supplyChainQuerySnapshot, rawNodeRefs []string) []string {
	refs := make([]string, 0)
	if snapshot != nil {
		for productRef := range snapshot.ByProduct {
			refs = append(refs, productRef)
		}
		for _, link := range snapshot.Links {
			if link == nil {
				continue
			}
			refs = append(refs,
				link.SupplierRef,
				link.CustomerRef,
				link.ProductRef,
			)
			refs = append(refs, link.TransportViaRefs...)
		}
	}
	refs = append(refs, rawNodeRefs...)
	return uniqueSortedStrings(refs)
}

func filterAssemblyProductRefs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(value))
		if strings.HasPrefix(value, "/product/") {
			out = append(out, value)
		}
	}
	return uniqueSortedStrings(out)
}

func (b *SupplyChainAssemblyBuilder) loadExistingPathArtifacts(ctx context.Context, tenantID string, anchorRefs []string) ([]*models.MaterializedArtifact, error) {
	if b == nil || b.artifactsRepo == nil || len(anchorRefs) == 0 {
		return nil, nil
	}
	seen := map[string]*models.MaterializedArtifact{}
	loadForTenant := func(tid string) error {
		for _, anchor := range anchorRefs {
			artifacts, err := b.artifactsRepo.FindByNodeRef(ctx, tid, anchor, models.MaterializedKindSupplyChainPath)
			if err != nil {
				return err
			}
			for _, artifact := range artifacts {
				if artifact == nil || strings.TrimSpace(artifact.CanonicalKey) == "" {
					continue
				}
				if _, ok := seen[artifact.CanonicalKey]; ok {
					continue
				}
				seen[artifact.CanonicalKey] = artifact
			}
		}
		return nil
	}
	if tenantID != "" && tenantID != "global" {
		if err := loadForTenant(tenantID); err != nil {
			return nil, err
		}
	}
	if err := loadForTenant("global"); err != nil {
		return nil, err
	}
	out := make([]*models.MaterializedArtifact, 0, len(seen))
	for _, artifact := range seen {
		out = append(out, artifact)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].CanonicalKey) < strings.TrimSpace(out[j].CanonicalKey)
	})
	return out, nil
}

func buildAssemblyPayloadFromContributions(
	anchors []string,
	contributions []assemblyContribution,
	opts SupplyChainAssemblyOptions,
) (map[string]interface{}, map[string]string, []string, *models.SupplyChainAssemblyCoverage, []string) {
	return buildAssemblyPayloadFromInputs(anchors, contributions, opts)
}

func buildAssemblyPayloadFromInputs(
	anchors []string,
	contributions []assemblyContribution,
	opts SupplyChainAssemblyOptions,
) (map[string]interface{}, map[string]string, []string, *models.SupplyChainAssemblyCoverage, []string) {
	productNodes := make(map[string]models.SupplyChainAssemblyNode)
	actorNodes := make(map[string]models.SupplyChainAssemblyNode)
	supplyEdges := make(map[string]models.SupplyChainAssemblySupplyEdge)
	transformEdges := make(map[string]models.SupplyChainAssemblyTransformEdge)
	assemblyCandidates := make(map[string]map[string]struct{})
	routeBindings := make(map[string]models.SupplyChainAssemblyRouteBinding)
	actorEdges := make(map[string]models.SupplyChainAssemblyTransformEdge)
	openGapSet := make(map[string]struct{})
	supplierSet := make(map[string]struct{})
	truncated := false
	connectivity := map[string]map[string]struct{}{}

	connect := func(left, right string) {
		left = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(left))
		right = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(right))
		if left == "" || right == "" || left == right {
			return
		}
		if connectivity[left] == nil {
			connectivity[left] = map[string]struct{}{}
		}
		if connectivity[right] == nil {
			connectivity[right] = map[string]struct{}{}
		}
		connectivity[left][right] = struct{}{}
		connectivity[right][left] = struct{}{}
	}

	addProductNode := func(ref string) bool {
		ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
		if ref == "" {
			return false
		}
		if _, ok := productNodes[ref]; ok {
			return true
		}
		if len(productNodes) >= opts.MaxProductNodes {
			truncated = true
			openGapSet["graph_truncated:product_nodes"] = struct{}{}
			return false
		}
		productNodes[ref] = models.SupplyChainAssemblyNode{Type: "product", Ref: ref, Name: inferAssemblyNodeName(ref)}
		return true
	}
	addActorNode := func(ref string, role string) bool {
		ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
		if ref == "" {
			return false
		}
		if _, ok := actorNodes[ref]; ok {
			return true
		}
		if len(actorNodes) >= opts.MaxActorNodes {
			truncated = true
			openGapSet["graph_truncated:actor_nodes"] = struct{}{}
			return false
		}
		actorNodes[ref] = models.SupplyChainAssemblyNode{Type: inferAssemblyActorType(ref), Ref: ref, Name: inferAssemblyNodeName(ref), Role: role}
		return true
	}
	addTransform := func(fromProduct, toProduct, relation, status string) {
		fromProduct = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(fromProduct))
		toProduct = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(toProduct))
		if fromProduct == "" || toProduct == "" || fromProduct == toProduct {
			return
		}
		if !addProductNode(fromProduct) || !addProductNode(toProduct) {
			return
		}
		key := fromProduct + "->" + toProduct + "|" + strings.TrimSpace(relation)
		if _, ok := transformEdges[key]; ok {
			return
		}
		if downstream := assemblyCandidates[toProduct]; len(downstream) >= opts.MaxTransformsPerProduct && relation == "TRANSFORMS_INTO" {
			truncated = true
			openGapSet["graph_truncated:transform_edges"] = struct{}{}
			return
		}
		transformEdges[key] = models.SupplyChainAssemblyTransformEdge{
			FromProductRef: fromProduct,
			ToProductRef:   toProduct,
			Relation:       relation,
			Status:         strings.TrimSpace(status),
		}
		connect(fromProduct, toProduct)
		if assemblyCandidates[toProduct] == nil {
			assemblyCandidates[toProduct] = map[string]struct{}{}
		}
		assemblyCandidates[toProduct][fromProduct] = struct{}{}
	}
	addActorEdge := func(sourceRef, targetRef, relation, status string) {
		sourceRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(sourceRef))
		targetRef = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(targetRef))
		if sourceRef == "" || targetRef == "" || sourceRef == targetRef {
			return
		}
		if !addActorNode(sourceRef, "") || !addActorNode(targetRef, "") {
			return
		}
		key := sourceRef + "=>" + targetRef + "|" + strings.TrimSpace(relation)
		actorEdges[key] = models.SupplyChainAssemblyTransformEdge{
			FromProductRef: sourceRef,
			ToProductRef:   targetRef,
			Relation:       strings.TrimSpace(relation),
			Status:         strings.TrimSpace(status),
		}
		connect(sourceRef, targetRef)
	}

	for _, ref := range opts.RawNodeRefs {
		ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "/product/") {
			addProductNode(ref)
			continue
		}
		addActorNode(ref, "")
	}
	for _, edge := range opts.RawEdges {
		source := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(edge.Source))
		target := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(edge.Target))
		edgeType := strings.ToUpper(strings.TrimSpace(edge.EdgeType))
		if source == "" || target == "" {
			continue
		}
		switch edgeType {
		case "TRANSFORMS_INTO":
			addTransform(source, target, "TRANSFORMS_INTO", "heuristic")
		case "PRODUCES":
			if addActorNode(source, "producer") && addProductNode(target) {
				connect(source, target)
			}
		case "OWNS", "CONTROLS":
			addActorEdge(source, target, edgeType, "heuristic")
		case "SUPPLIES_TO":
			sourceIsProduct := strings.HasPrefix(source, "/product/")
			targetIsProduct := strings.HasPrefix(target, "/product/")
			switch {
			case sourceIsProduct && targetIsProduct:
				addTransform(source, target, "TRANSFORMS_INTO", "heuristic")
			case sourceIsProduct:
				if addProductNode(source) && addActorNode(target, "customer") {
					connect(source, target)
				}
			case targetIsProduct:
				if addActorNode(source, "supplier") && addProductNode(target) {
					connect(source, target)
				}
			default:
				addActorEdge(source, target, "SUPPLIES_TO", "heuristic")
			}
		case "STORES", "HANDLES":
			if strings.HasPrefix(target, "/product/") {
				if addActorNode(source, strings.ToLower(edgeType)) && addProductNode(target) {
					connect(source, target)
				}
			} else {
				addActorEdge(source, target, edgeType, "heuristic")
			}
		default:
			if strings.HasPrefix(source, "/product/") && strings.HasPrefix(target, "/product/") {
				addTransform(source, target, edgeType, "heuristic")
				continue
			}
			if strings.HasPrefix(source, "/product/") {
				if addProductNode(source) && addActorNode(target, "") {
					connect(source, target)
				}
				continue
			}
			if strings.HasPrefix(target, "/product/") {
				if addActorNode(source, "") && addProductNode(target) {
					connect(source, target)
				}
				continue
			}
			addActorEdge(source, target, edgeType, "heuristic")
		}
	}

	for _, contribution := range contributions {
		if contribution.parsed == nil {
			continue
		}
		status := ""
		if contribution.parsed.Sufficiency != nil {
			status = strings.TrimSpace(contribution.parsed.Sufficiency.Status)
			for _, gap := range contribution.parsed.Sufficiency.OpenGaps {
				gap = strings.TrimSpace(gap)
				if gap != "" {
					openGapSet[gap] = struct{}{}
				}
			}
		}
		for _, node := range contribution.parsed.Nodes {
			ref := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(node.Ref))
			switch strings.ToLower(strings.TrimSpace(node.Type)) {
			case "product":
				addProductNode(ref)
			default:
				addActorNode(ref, "")
			}
		}
		for _, leg := range contribution.parsed.Legs {
			if !addProductNode(leg.ProductRef) {
				continue
			}
			supplierOK := addActorNode(leg.SupplierRef, "supplier")
			customerOK := addActorNode(leg.CustomerRef, "customer")
			if !supplierOK || !customerOK {
				continue
			}
			if len(supplyEdges) >= opts.MaxProductNodes*opts.MaxSuppliersPerProduct {
				truncated = true
				openGapSet["graph_truncated:supply_edges"] = struct{}{}
				continue
			}
			key := strings.Join([]string{
				canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.SupplierRef)),
				canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.ProductRef)),
				canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.CustomerRef)),
				strings.TrimSpace(leg.SupplyLinkCanonicalPath),
			}, "|")
			supplyEdges[key] = models.SupplyChainAssemblySupplyEdge{
				SupplierRef:             canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.SupplierRef)),
				ProductRef:              canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.ProductRef)),
				CustomerRef:             canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.CustomerRef)),
				SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				SupplyLinkID:            strings.TrimSpace(leg.SupplyLinkID),
				TransportViaRefs:        uniqueSortedStrings(append([]string(nil), leg.TransportViaRefs...)),
				Confidence:              leg.SegmentConfidence,
				Status:                  status,
			}
			connect(leg.SupplierRef, leg.ProductRef)
			connect(leg.ProductRef, leg.CustomerRef)
			if supplier := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.SupplierRef)); supplier != "" {
				supplierSet[supplier] = struct{}{}
			}
			if len(leg.TransportViaRefs) > 0 {
				routeBindings[key] = models.SupplyChainAssemblyRouteBinding{
					ProductRef:              canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(leg.ProductRef)),
					SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
					ViaRefs:                 uniqueSortedStrings(append([]string(nil), leg.TransportViaRefs...)),
				}
				for _, via := range leg.TransportViaRefs {
					addActorNode(via, "route")
				}
			}
		}
		for _, edge := range contribution.parsed.Edges {
			if strings.TrimSpace(edge.Type) == "ONTOLOGY" && strings.EqualFold(strings.TrimSpace(edge.Relation), "TRANSFORMS_INTO") {
				fromProduct := strings.TrimSpace(edge.ProductRef)
				if fromProduct == "" && edge.FromProductIndex != nil && *edge.FromProductIndex >= 0 && *edge.FromProductIndex < len(contribution.parsed.Nodes) {
					fromProduct = contribution.parsed.Nodes[*edge.FromProductIndex].Ref
				}
				toProduct := ""
				if edge.ToProductIndex != nil && *edge.ToProductIndex >= 0 && *edge.ToProductIndex < len(contribution.parsed.Nodes) {
					toProduct = contribution.parsed.Nodes[*edge.ToProductIndex].Ref
				}
				addTransform(fromProduct, toProduct, "TRANSFORMS_INTO", status)
			}
		}
		for idx := 0; idx < len(contribution.parsed.Legs)-1; idx++ {
			left := contribution.parsed.Legs[idx]
			right := contribution.parsed.Legs[idx+1]
			if canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(left.ProductRef)) == canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(right.ProductRef)) {
				continue
			}
			addTransform(left.ProductRef, right.ProductRef, "TRANSFORMS_INTO", status)
		}
	}

	assemblyEdges := make(map[string]models.SupplyChainAssemblyTransformEdge)
	for downstream, upstreams := range assemblyCandidates {
		if len(upstreams) < 2 {
			continue
		}
		count := 0
		for upstream := range upstreams {
			if count >= opts.MaxTransformsPerProduct {
				truncated = true
				openGapSet["graph_truncated:assembly_edges"] = struct{}{}
				break
			}
			key := upstream + "=>" + downstream
			assemblyEdges[key] = models.SupplyChainAssemblyTransformEdge{
				FromProductRef: upstream,
				ToProductRef:   downstream,
				Relation:       "ASSEMBLES_INTO",
				Status:         "inferred",
			}
			connect(upstream, downstream)
			count++
		}
	}

	productNodeList := make([]models.SupplyChainAssemblyNode, 0, len(productNodes))
	for _, node := range productNodes {
		productNodeList = append(productNodeList, node)
	}
	sort.SliceStable(productNodeList, func(i, j int) bool { return productNodeList[i].Ref < productNodeList[j].Ref })

	actorNodeList := make([]models.SupplyChainAssemblyNode, 0, len(actorNodes))
	for _, node := range actorNodes {
		actorNodeList = append(actorNodeList, node)
	}
	sort.SliceStable(actorNodeList, func(i, j int) bool { return actorNodeList[i].Ref < actorNodeList[j].Ref })

	supplyEdgeList := make([]models.SupplyChainAssemblySupplyEdge, 0, len(supplyEdges))
	for _, edge := range supplyEdges {
		supplyEdgeList = append(supplyEdgeList, edge)
	}
	sort.SliceStable(supplyEdgeList, func(i, j int) bool {
		if supplyEdgeList[i].ProductRef == supplyEdgeList[j].ProductRef {
			if supplyEdgeList[i].SupplierRef == supplyEdgeList[j].SupplierRef {
				return supplyEdgeList[i].CustomerRef < supplyEdgeList[j].CustomerRef
			}
			return supplyEdgeList[i].SupplierRef < supplyEdgeList[j].SupplierRef
		}
		return supplyEdgeList[i].ProductRef < supplyEdgeList[j].ProductRef
	})

	transformEdgeList := make([]models.SupplyChainAssemblyTransformEdge, 0, len(transformEdges))
	for _, edge := range transformEdges {
		transformEdgeList = append(transformEdgeList, edge)
	}
	sort.SliceStable(transformEdgeList, func(i, j int) bool {
		if transformEdgeList[i].ToProductRef == transformEdgeList[j].ToProductRef {
			return transformEdgeList[i].FromProductRef < transformEdgeList[j].FromProductRef
		}
		return transformEdgeList[i].ToProductRef < transformEdgeList[j].ToProductRef
	})

	assemblyEdgeList := make([]models.SupplyChainAssemblyTransformEdge, 0, len(assemblyEdges))
	for _, edge := range assemblyEdges {
		assemblyEdgeList = append(assemblyEdgeList, edge)
	}
	sort.SliceStable(assemblyEdgeList, func(i, j int) bool {
		if assemblyEdgeList[i].ToProductRef == assemblyEdgeList[j].ToProductRef {
			return assemblyEdgeList[i].FromProductRef < assemblyEdgeList[j].FromProductRef
		}
		return assemblyEdgeList[i].ToProductRef < assemblyEdgeList[j].ToProductRef
	})

	routeBindingList := make([]models.SupplyChainAssemblyRouteBinding, 0, len(routeBindings))
	for _, binding := range routeBindings {
		routeBindingList = append(routeBindingList, binding)
	}
	sort.SliceStable(routeBindingList, func(i, j int) bool {
		if routeBindingList[i].ProductRef == routeBindingList[j].ProductRef {
			return routeBindingList[i].SupplyLinkCanonicalPath < routeBindingList[j].SupplyLinkCanonicalPath
		}
		return routeBindingList[i].ProductRef < routeBindingList[j].ProductRef
	})

	for _, edge := range actorEdges {
		connect(edge.FromProductRef, edge.ToProductRef)
	}

	anchorComponents := buildAssemblyAnchorComponents(anchors, productNodes, actorNodes, connectivity)
	openGaps := sortedStringKeys(openGapSet)
	coverage := &models.SupplyChainAssemblyCoverage{
		ProductsN:        len(productNodeList),
		ActorsN:          len(actorNodeList),
		SuppliersN:       len(supplierSet),
		TransformationsN: len(transformEdgeList),
		DerivedPathsN:    len(contributions),
		UnresolvedLeaves: len(openGaps),
		Truncated:        truncated,
	}
	notes := map[string]interface{}{
		"graphLayer":        "assembly_graph",
		"artifactKind":      string(models.MaterializedKindSupplyChainAssemblyGraph),
		"contributionCount": len(contributions),
		"anchorSet":         anchors,
		"truncated":         truncated,
		"openGapsCount":     len(openGaps),
	}
	payload := models.BuildSupplyChainAssemblyGraphPayloadDetailed(models.SupplyChainAssemblyGraphPayloadOptions{
		AnchorSet:        anchors,
		SourceAnchors:    firstNonEmptyAnchors(opts.SourceAnchors, anchors),
		ResolvedAnchors:  anchors,
		AnchorProducts:   filterAssemblyProductRefs(anchors),
		AnchorComponents: anchorComponents,
		ProductNodes:     productNodeList,
		ActorNodes:       actorNodeList,
		SupplyEdges:      supplyEdgeList,
		TransformEdges:   transformEdgeList,
		AssemblyEdges:    assemblyEdgeList,
		RouteBindings:    routeBindingList,
		Coverage:         coverage,
		OpenGaps:         openGaps,
		Notes:            notes,
	})
	labels := map[string]string{
		"anchorRef":          firstStringOrEmpty(anchors),
		"anchorProduct":      firstStringOrEmpty(filterAssemblyProductRefs(anchors)),
		"productAxis":        firstStringOrEmpty(filterAssemblyProductRefs(anchors)),
		"finalProductFamily": firstStringOrEmpty(filterAssemblyProductRefs(anchors)),
		"artifactKind":       string(models.MaterializedKindSupplyChainAssemblyGraph),
		"productsN":          fmt.Sprintf("%d", coverage.ProductsN),
		"actorsN":            fmt.Sprintf("%d", coverage.ActorsN),
		"transformsN":        fmt.Sprintf("%d", coverage.TransformationsN),
	}
	nodeRefs := mergeAssemblyStrings(collectAssemblyPayloadNodeRefs(productNodeList, actorNodeList), collectAssemblyRouteRefs(routeBindingList))
	return payload, labels, nodeRefs, coverage, openGaps
}

func determineAssemblySourceMode(hasContributions bool, hasRawGraph bool) string {
	switch {
	case hasContributions && hasRawGraph:
		return "mixed"
	case hasContributions:
		return "supply_links"
	case hasRawGraph:
		return "chain_context"
	default:
		return ""
	}
}

func firstNonEmptyAnchors(primary []string, fallback []string) []string {
	if len(primary) > 0 {
		return uniqueSortedStrings(primary)
	}
	return uniqueSortedStrings(fallback)
}

func buildAssemblyAnchorComponents(
	anchors []string,
	productNodes map[string]models.SupplyChainAssemblyNode,
	actorNodes map[string]models.SupplyChainAssemblyNode,
	connectivity map[string]map[string]struct{},
) []models.SupplyChainAssemblyAnchorComponent {
	if len(anchors) == 0 {
		return nil
	}
	components := make([]models.SupplyChainAssemblyAnchorComponent, 0, len(anchors))
	visited := map[string]struct{}{}
	for _, anchor := range uniqueSortedStrings(anchors) {
		if _, ok := visited[anchor]; ok {
			continue
		}
		component := models.SupplyChainAssemblyAnchorComponent{
			Anchors: []string{anchor},
			Status:  "partial",
		}
		queue := []string{anchor}
		seen := map[string]struct{}{anchor: {}}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			visited[current] = struct{}{}
			if _, ok := productNodes[current]; ok {
				component.ProductRefs = append(component.ProductRefs, current)
			}
			if _, ok := actorNodes[current]; ok {
				component.ActorRefs = append(component.ActorRefs, current)
			}
			for next := range connectivity[current] {
				if _, ok := seen[next]; ok {
					continue
				}
				seen[next] = struct{}{}
				queue = append(queue, next)
				for _, otherAnchor := range anchors {
					otherAnchor = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(otherAnchor))
					if otherAnchor != "" && otherAnchor == next && !containsAssemblyString(component.Anchors, otherAnchor) {
						component.Anchors = append(component.Anchors, otherAnchor)
					}
				}
			}
		}
		component.Anchors = uniqueSortedStrings(component.Anchors)
		component.ProductRefs = uniqueSortedStrings(component.ProductRefs)
		component.ActorRefs = uniqueSortedStrings(component.ActorRefs)
		if len(component.ProductRefs) > 0 || len(component.ActorRefs) > 0 {
			component.Status = "complete"
		}
		components = append(components, component)
	}
	sort.SliceStable(components, func(i, j int) bool {
		return firstStringOrEmpty(components[i].Anchors) < firstStringOrEmpty(components[j].Anchors)
	})
	return components
}

func containsAssemblyString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func buildSupplyChainAssemblyCanonicalKey(tenantID string, anchors []string, geoFilters []string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(tenantID)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(uniqueSortedStrings(anchors), ",")))
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(uniqueSortedStrings(geoFilters), ",")))
	sum := fmt.Sprintf("%x", h.Sum(nil))
	return fmt.Sprintf("/supply-chain-assembly/%s/%s", strings.TrimSpace(firstNonEmptyString(tenantID, "global")), sum[:16])
}

func buildSupplyChainAssemblyDisplayName(anchors []string) string {
	if len(anchors) == 0 {
		return "Supply chain assembly graph"
	}
	if len(anchors) == 1 {
		return "Assembly graph: " + inferAssemblyNodeName(anchors[0])
	}
	return fmt.Sprintf("Assembly graph: %s (+%d)", inferAssemblyNodeName(anchors[0]), len(anchors)-1)
}

func inferAssemblyActorType(ref string) string {
	ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
	switch {
	case strings.HasPrefix(ref, "/org/"):
		return "org"
	case strings.HasPrefix(ref, "/gov/"):
		return "gov"
	case strings.HasPrefix(ref, "/deposit/"):
		return "deposit"
	default:
		return "fac"
	}
}

func inferAssemblyNodeName(ref string) string {
	ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
	if ref == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 0 {
		return ref
	}
	return strings.ReplaceAll(parts[len(parts)-1], "-", " ")
}

func collectAssemblyPayloadNodeRefs(productNodes []models.SupplyChainAssemblyNode, actorNodes []models.SupplyChainAssemblyNode) []string {
	refs := make([]string, 0, len(productNodes)+len(actorNodes))
	for _, node := range productNodes {
		if strings.TrimSpace(node.Ref) != "" {
			refs = append(refs, strings.TrimSpace(node.Ref))
		}
	}
	for _, node := range actorNodes {
		if strings.TrimSpace(node.Ref) != "" {
			refs = append(refs, strings.TrimSpace(node.Ref))
		}
	}
	return uniqueSortedStrings(refs)
}

func collectAssemblyRouteRefs(bindings []models.SupplyChainAssemblyRouteBinding) []string {
	refs := make([]string, 0, len(bindings)*2)
	for _, binding := range bindings {
		refs = append(refs, binding.ViaRefs...)
	}
	return uniqueSortedStrings(refs)
}

func firstStringOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
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

func mergeAssemblyStrings(base []string, extra []string) []string {
	combined := make([]string, 0, len(base)+len(extra))
	combined = append(combined, base...)
	combined = append(combined, extra...)
	return uniqueSortedStrings(combined)
}
