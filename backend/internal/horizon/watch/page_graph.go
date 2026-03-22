package watch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type lensDiagnosticRelation struct {
	EdgeID               string
	SeedFragmentID       string
	SourceFragmentID     string
	TargetFragmentID     string
	RelatedFragmentID    string
	RelatedCanonicalPath string
	RelatedLabel         string
	RelatedType          string
	Type                 string
	WhyRelevant          string
	Hop                  int
	Criticality          float64
}

type lensDiagnosticGraph struct {
	Seeds            []*horizonmodels.HorizonFragment
	SeedIDs          []string
	Nodes            map[string]*horizonmodels.HorizonFragment
	NodeMinHops      map[string]int
	NodeReasons      map[string]string
	NodeBackings     map[string][]string
	DirectRelations  []lensDiagnosticRelation
	ContextRelations []lensDiagnosticRelation
	EdgeTypeCounts   map[string]int
	ZonePaths        []string
	RoutePaths       []string
	ProductPaths     []string
	GeoPaths         []string
}

func (s *Service) collectLensDiagnosticGraph(
	ctx context.Context,
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	substrate []supplyChainSubstrate,
	assemblies []lensAssemblyArtifact,
) *lensDiagnosticGraph {
	if s == nil || s.fragmentsRepo == nil {
		return nil
	}

	seedPaths := s.collectLensDiagnosticSeedPaths(lens, base, substrate, assemblies)
	seeds := s.resolveLensDiagnosticSeeds(ctx, seedPaths)
	if len(seeds) == 0 {
		return nil
	}

	graph := &lensDiagnosticGraph{
		Seeds:            seeds,
		SeedIDs:          make([]string, 0, len(seeds)),
		Nodes:            map[string]*horizonmodels.HorizonFragment{},
		NodeMinHops:      map[string]int{},
		NodeReasons:      map[string]string{},
		NodeBackings:     map[string][]string{},
		DirectRelations:  []lensDiagnosticRelation{},
		ContextRelations: []lensDiagnosticRelation{},
		EdgeTypeCounts:   map[string]int{},
	}
	for _, seed := range seeds {
		if seed == nil || strings.TrimSpace(seed.ID) == "" {
			continue
		}
		graph.SeedIDs = append(graph.SeedIDs, seed.ID)
		graph.Nodes[seed.ID] = seed
		graph.NodeMinHops[seed.ID] = 0
		graph.NodeReasons[seed.ID] = "Seed object inside the lens scope."
		graph.NodeBackings[seed.ID] = compactUnique(append(graph.NodeBackings[seed.ID], seed.ID))
	}

	s.expandLensDiagnosticGraph(ctx, graph, 2)

	graph.ZonePaths = compactUnique(graph.ZonePaths)
	graph.RoutePaths = compactUnique(graph.RoutePaths)
	graph.ProductPaths = compactUnique(graph.ProductPaths)
	graph.GeoPaths = compactUnique(graph.GeoPaths)
	graph.SeedIDs = compactUnique(graph.SeedIDs)
	return graph
}

func (s *Service) collectLensDiagnosticSeedPaths(
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	substrate []supplyChainSubstrate,
	assemblies []lensAssemblyArtifact,
) []string {
	paths := []string{}
	for _, item := range substrate {
		paths = append(paths,
			strings.TrimSpace(item.AnchorSupplier),
			strings.TrimSpace(item.AnchorCustomer),
		)
	}
	for _, assembly := range assemblies {
		if assembly.Parsed == nil {
			continue
		}
		paths = append(paths, assembly.Parsed.AnchorSet...)
		paths = append(paths, assembly.Parsed.ResolvedAnchors...)
		paths = append(paths, firstNStrings(assembly.Parsed.TopDistributionNodes, 4)...)
		paths = append(paths, firstNStrings(assembly.Parsed.TopSuppliers, 4)...)
	}
	if len(compactUnique(paths)) == 0 {
		if lens != nil {
			for _, path := range lens.Scope {
				if isWatchScopeTargetPath(path) {
					paths = append(paths, path)
				}
			}
		}
		if base != nil && base.LensAttention != nil {
			for _, subject := range base.LensAttention.Subjects {
				if subject == nil {
					continue
				}
				paths = append(paths, strings.TrimSpace(subject.AnchorCanonicalPath))
			}
		}
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		switch {
		case strings.HasPrefix(path, "/org/"),
			strings.HasPrefix(path, "/fac/"),
			strings.HasPrefix(path, "/deposit/"):
			filtered = append(filtered, path)
		}
		if len(filtered) >= 6 {
			break
		}
	}
	return compactUnique(filtered)
}

func (s *Service) resolveLensDiagnosticSeeds(ctx context.Context, paths []string) []*horizonmodels.HorizonFragment {
	if s == nil || s.fragmentsRepo == nil {
		return nil
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range compactUnique(paths) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		if _, ok := seen[frag.ID]; ok {
			continue
		}
		seen[frag.ID] = struct{}{}
		out = append(out, frag)
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func (s *Service) expandLensDiagnosticGraph(ctx context.Context, graph *lensDiagnosticGraph, maxHops int) {
	if s == nil || s.edgesRepo == nil || s.fragmentsRepo == nil || graph == nil || maxHops <= 0 {
		return
	}
	type frontierItem struct {
		id   string
		hops int
		seed string
	}
	queue := make([]frontierItem, 0, len(graph.Seeds))
	visited := map[string]int{}
	for _, seed := range graph.Seeds {
		if seed == nil || strings.TrimSpace(seed.ID) == "" {
			continue
		}
		queue = append(queue, frontierItem{id: seed.ID, hops: 0, seed: seed.ID})
		visited[seed.ID] = 0
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.hops >= maxHops {
			continue
		}
		outgoing, _ := s.edgesRepo.ListBySource(ctx, "global", current.id, nil, 40)
		incoming, _ := s.edgesRepo.ListByTarget(ctx, "global", current.id, nil, 40)
		candidateEdges := make([]*horizonmodels.DependencyEdge, 0, len(outgoing)+len(incoming))
		neighborIDs := make([]string, 0, len(outgoing)+len(incoming))
		for _, edge := range append(outgoing, incoming...) {
			if edge == nil || !edge.IsActiveNow() || edge.IsQuarantined() {
				continue
			}
			neighborID := strings.TrimSpace(edge.TargetFragmentID)
			if neighborID == current.id {
				neighborID = strings.TrimSpace(edge.SourceFragmentID)
			}
			if neighborID == "" || neighborID == current.id {
				continue
			}
			candidateEdges = append(candidateEdges, edge)
			neighborIDs = append(neighborIDs, neighborID)
		}
		neighbors, _ := s.fragmentsRepo.GetMany(ctx, neighborIDs)
		for _, edge := range candidateEdges {
			neighborID := strings.TrimSpace(edge.TargetFragmentID)
			if neighborID == current.id {
				neighborID = strings.TrimSpace(edge.SourceFragmentID)
			}
			neighbor := neighbors[neighborID]
			if neighbor == nil {
				continue
			}
			hop := current.hops + 1
			graph.Nodes[neighbor.ID] = neighbor
			if prev, ok := graph.NodeMinHops[neighbor.ID]; !ok || hop < prev {
				graph.NodeMinHops[neighbor.ID] = hop
			}
			graph.NodeBackings[neighbor.ID] = compactUnique(append(graph.NodeBackings[neighbor.ID], edge.ID, neighbor.ID))
			relation := buildLensDiagnosticRelation(graph.Nodes[current.id], neighbor, edge, current.seed, hop)
			if relation.RelatedFragmentID == "" {
				continue
			}
			graph.EdgeTypeCounts[relation.Type]++
			graph.NodeReasons[neighbor.ID] = firstNonEmpty(graph.NodeReasons[neighbor.ID], relation.WhyRelevant)
			graph.captureLensDiagnosticPath(neighbor.CanonicalPath)
			if hop == 1 {
				if isLensDiagnosticContextRelation(relation.Type) {
					graph.ContextRelations = append(graph.ContextRelations, relation)
				} else {
					graph.DirectRelations = append(graph.DirectRelations, relation)
				}
			}
			if prev, ok := visited[neighbor.ID]; !ok || hop < prev {
				visited[neighbor.ID] = hop
				queue = append(queue, frontierItem{id: neighbor.ID, hops: hop, seed: current.seed})
			}
		}
	}
	sortLensDiagnosticRelations(graph.DirectRelations)
	sortLensDiagnosticRelations(graph.ContextRelations)
	if len(graph.DirectRelations) > 18 {
		graph.DirectRelations = graph.DirectRelations[:18]
	}
	if len(graph.ContextRelations) > 12 {
		graph.ContextRelations = graph.ContextRelations[:12]
	}
}

func buildLensDiagnosticRelation(
	center *horizonmodels.HorizonFragment,
	neighbor *horizonmodels.HorizonFragment,
	edge *horizonmodels.DependencyEdge,
	seedID string,
	hop int,
) lensDiagnosticRelation {
	centerLabel := lensDiagnosticFragmentLabel(center)
	relatedLabel := lensDiagnosticFragmentLabel(neighbor)
	relation := lensDiagnosticRelation{
		EdgeID:               strings.TrimSpace(edge.ID),
		SeedFragmentID:       strings.TrimSpace(seedID),
		SourceFragmentID:     strings.TrimSpace(edge.SourceFragmentID),
		TargetFragmentID:     strings.TrimSpace(edge.TargetFragmentID),
		RelatedFragmentID:    strings.TrimSpace(neighbor.ID),
		RelatedCanonicalPath: strings.TrimSpace(neighbor.CanonicalPath),
		RelatedLabel:         relatedLabel,
		RelatedType:          strings.TrimSpace(string(neighbor.Type)),
		Type:                 strings.TrimSpace(string(edge.Type)),
		Hop:                  hop,
		Criticality:          edge.Properties.Criticality,
	}
	relation.WhyRelevant = describeLensDiagnosticRelation(centerLabel, relation)
	return relation
}

func sortLensDiagnosticRelations(in []lensDiagnosticRelation) {
	sort.SliceStable(in, func(i, j int) bool {
		leftPriority := lensDiagnosticRelationPriority(in[i].Type)
		rightPriority := lensDiagnosticRelationPriority(in[j].Type)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		if in[i].Hop != in[j].Hop {
			return in[i].Hop < in[j].Hop
		}
		if in[i].Criticality != in[j].Criticality {
			return in[i].Criticality > in[j].Criticality
		}
		return in[i].RelatedLabel < in[j].RelatedLabel
	})
}

func (g *lensDiagnosticGraph) captureLensDiagnosticPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	switch {
	case strings.HasPrefix(path, "/zone/"):
		g.ZonePaths = append(g.ZonePaths, path)
	case strings.HasPrefix(path, "/route/"), strings.HasPrefix(path, "/flow/"):
		g.RoutePaths = append(g.RoutePaths, path)
	case strings.HasPrefix(path, "/product/"):
		g.ProductPaths = append(g.ProductPaths, path)
	case strings.HasPrefix(path, "/geo/"), strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/market/"):
		g.GeoPaths = append(g.GeoPaths, path)
	}
}

func buildGraphReachItems(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageScopeItem {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	type candidate struct {
		frag *horizonmodels.HorizonFragment
		hop  int
	}
	candidates := make([]candidate, 0, len(graph.Nodes))
	for id, frag := range graph.Nodes {
		if frag == nil || strings.TrimSpace(id) == "" {
			continue
		}
		hop, ok := graph.NodeMinHops[id]
		if !ok || hop <= 0 {
			continue
		}
		candidates = append(candidates, candidate{frag: frag, hop: hop})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].hop != candidates[j].hop {
			return candidates[i].hop < candidates[j].hop
		}
		leftPriority := canonicalPathImpactPriority(candidates[i].frag.CanonicalPath)
		rightPriority := canonicalPathImpactPriority(candidates[j].frag.CanonicalPath)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return lensDiagnosticFragmentLabel(candidates[i].frag) < lensDiagnosticFragmentLabel(candidates[j].frag)
	})
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(candidates), 12))
	for _, item := range candidates {
		label := lensDiagnosticFragmentLabel(item.frag)
		out = append(out, horizonmodels.WatchPageScopeItem{
			ID:            "graph_reach:" + strings.TrimSpace(item.frag.ID),
			Label:         label,
			CanonicalPath: strings.TrimSpace(item.frag.CanonicalPath),
			Kind:          "graph_reach",
			Summary:       firstNonEmpty(graph.NodeReasons[item.frag.ID], fmt.Sprintf("Reached %d hop(s) away from the lens seed graph.", item.hop)),
			BackingIDs:    compactUnique(graph.NodeBackings[item.frag.ID]),
			GeneratedAt:   now,
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func buildGraphProductItems(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageScopeItem {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	type agg struct {
		path            string
		label           string
		score           float64
		consumers       []string
		producers       []string
		transformedInto []string
		transformedFrom []string
		backings        []string
	}
	products := map[string]*agg{}
	ensure := func(frag *horizonmodels.HorizonFragment) *agg {
		if frag == nil || !strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/product/") {
			return nil
		}
		path := strings.TrimSpace(frag.CanonicalPath)
		entry := products[path]
		if entry == nil {
			entry = &agg{
				path:  path,
				label: lensDiagnosticFragmentLabel(frag),
			}
			products[path] = entry
		}
		return entry
	}
	for _, frag := range graph.Nodes {
		if entry := ensure(frag); entry != nil {
			entry.score += 1
			entry.backings = compactUnique(append(entry.backings, frag.ID))
		}
	}
	lookup := map[string]*horizonmodels.HorizonFragment{}
	for id, frag := range graph.Nodes {
		lookup[id] = frag
	}
	for _, relation := range append(append([]lensDiagnosticRelation{}, graph.DirectRelations...), graph.ContextRelations...) {
		if !isLensSupplyRelevantRelation(relation.Type) {
			continue
		}
		related := lookup[relation.RelatedFragmentID]
		if related == nil {
			continue
		}
		product := ensure(related)
		if product == nil {
			continue
		}
		product.score += maxFloat(0.6, 2.5-float64(maxInt(1, relation.Hop))/2)
		product.backings = compactUnique(append(product.backings, relation.EdgeID, relation.RelatedFragmentID))
		switch relation.Type {
		case string(horizonmodels.DependencyTypeConsumes):
			if actor := graph.Nodes[relation.SourceFragmentID]; actor != nil {
				product.consumers = compactUnique(append(product.consumers, lensDiagnosticFragmentLabel(actor)))
			}
		case string(horizonmodels.DependencyTypeProduces):
			if actor := graph.Nodes[relation.SourceFragmentID]; actor != nil {
				product.producers = compactUnique(append(product.producers, lensDiagnosticFragmentLabel(actor)))
			}
		case string(horizonmodels.DependencyTypeTransformsInto), string(horizonmodels.DependencyTypeAssemblesInto):
			if downstream := graph.Nodes[relation.TargetFragmentID]; downstream != nil && downstream.ID != related.ID {
				product.transformedInto = compactUnique(append(product.transformedInto, lensDiagnosticFragmentLabel(downstream)))
			}
			if upstream := graph.Nodes[relation.SourceFragmentID]; upstream != nil && upstream.ID != related.ID {
				product.transformedFrom = compactUnique(append(product.transformedFrom, lensDiagnosticFragmentLabel(upstream)))
			}
		}
	}
	keys := make([]string, 0, len(products))
	for key := range products {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := products[keys[i]]
		right := products[keys[j]]
		if left.score != right.score {
			return left.score > right.score
		}
		return left.label < right.label
	})
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(keys), 8))
	for _, key := range keys {
		entry := products[key]
		parts := []string{}
		if len(entry.producers) > 0 {
			parts = append(parts, "Produced by "+strings.Join(firstNStrings(entry.producers, 2), ", "))
		}
		if len(entry.consumers) > 0 {
			parts = append(parts, "Consumed by "+strings.Join(firstNStrings(entry.consumers, 3), ", "))
		}
		if len(entry.transformedInto) > 0 {
			parts = append(parts, "Transforms into "+strings.Join(firstNStrings(entry.transformedInto, 2), ", "))
		}
		if len(entry.transformedFrom) > 0 {
			parts = append(parts, "Derived from "+strings.Join(firstNStrings(entry.transformedFrom, 2), ", "))
		}
		out = append(out, horizonmodels.WatchPageScopeItem{
			ID:            "graph_product:" + strings.Trim(strings.TrimPrefix(entry.path, "/"), "/"),
			Label:         entry.label,
			CanonicalPath: entry.path,
			Kind:          "product",
			Summary:       strings.Join(parts, " • "),
			BackingIDs:    compactUnique(entry.backings),
			GeneratedAt:   now,
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func buildGraphRiskItems(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageRiskItem {
	if graph == nil {
		return nil
	}
	out := make([]horizonmodels.WatchPageRiskItem, 0, 4)
	if len(graph.DirectRelations) > 0 {
		relation := graph.DirectRelations[0]
		out = append(out, horizonmodels.WatchPageRiskItem{
			ID:            "graph-risk:direct:" + strings.TrimSpace(firstNonEmpty(relation.EdgeID, relation.RelatedFragmentID)),
			Title:         firstNonEmpty(relation.RelatedLabel, relation.RelatedCanonicalPath),
			Summary:       relation.WhyRelevant,
			CanonicalPath: relation.RelatedCanonicalPath,
			Mechanisms:    compactUnique([]string{strings.ToLower(relation.Type), "direct_dependency"}),
			BackingIDs:    compactUnique([]string{relation.EdgeID, relation.RelatedFragmentID, relation.SeedFragmentID}),
			GeneratedAt:   now,
		})
	}
	if len(graph.ProductPaths) > 0 {
		productPath := strings.TrimSpace(graph.ProductPaths[0])
		out = append(out, horizonmodels.WatchPageRiskItem{
			ID:            "graph-risk:product:" + strings.Trim(strings.TrimPrefix(productPath, "/"), "/"),
			Title:         labelFromCanonicalPath(productPath),
			Summary:       "This product sits on one of the tightest dependency chains exposed by the lens graph and downstream consumer network.",
			CanonicalPath: productPath,
			Mechanisms:    []string{"product_exposure", "downstream_consumption"},
			BackingIDs:    []string{productPath},
			GeneratedAt:   now,
		})
	}
	if len(graph.ZonePaths) > 0 {
		zonePath := strings.TrimSpace(graph.ZonePaths[0])
		out = append(out, horizonmodels.WatchPageRiskItem{
			ID:            "graph-risk:zone:" + strings.Trim(strings.TrimPrefix(zonePath, "/"), "/"),
			Title:         labelFromCanonicalPath(zonePath),
			Summary:       "Geographic tension is clustered here and feeds directly into the operational graph surfaced by the lens.",
			CanonicalPath: zonePath,
			Mechanisms:    []string{"geographic_tension", "path_concentration"},
			BackingIDs:    []string{zonePath},
			GeneratedAt:   now,
		})
	}
	return out
}

func buildGraphImpactSets(
	graph *lensDiagnosticGraph,
	now time.Time,
	limit int,
) ([]horizonmodels.WatchPageAffectedItem, []horizonmodels.WatchPageAffectedItem) {
	if graph == nil {
		return nil, nil
	}
	direct := map[string]*pageImpactAgg{}
	indirect := map[string]*pageImpactAgg{}
	add := func(bucket map[string]*pageImpactAgg, path, label, reason string, score float64, backing ...string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		entry := bucket[path]
		if entry == nil {
			entry = &pageImpactAgg{
				path:  path,
				label: strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(path))),
			}
			bucket[path] = entry
		}
		entry.score += score
		entry.reason = firstNonEmpty(entry.reason, strings.TrimSpace(reason))
		entry.backing = compactUnique(append(entry.backing, backing...))
	}
	for id, frag := range graph.Nodes {
		if frag == nil {
			continue
		}
		path := strings.TrimSpace(frag.CanonicalPath)
		if path == "" || canonicalPathImpactPriority(path) == 0 {
			continue
		}
		if hop, ok := graph.NodeMinHops[id]; ok && hop > 0 {
			reason := firstNonEmpty(graph.NodeReasons[id], "linked into the lens dependency graph.")
			if hop == 1 {
				add(direct, path, lensDiagnosticFragmentLabel(frag), reason, 3.0, graph.NodeBackings[id]...)
			} else {
				add(indirect, path, lensDiagnosticFragmentLabel(frag), reason, maxFloat(0.8, 3.0-float64(hop)/2), graph.NodeBackings[id]...)
			}
		}
	}
	return rankedAffectedItems(direct, now, limit), rankedAffectedItems(indirect, now, limit)
}

func buildGraphSignals(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageSignalItem {
	if graph == nil {
		return nil
	}
	out := make([]horizonmodels.WatchPageSignalItem, 0, 6)
	for _, item := range buildGraphProductItems(graph, now) {
		if strings.TrimSpace(item.Summary) == "" {
			continue
		}
		out = append(out, horizonmodels.WatchPageSignalItem{
			ID:             "graph-signal:product:" + strings.Trim(strings.TrimPrefix(item.CanonicalPath, "/"), "/"),
			Kind:           "graph_product_chain",
			Title:          item.Label + " downstream exposure",
			Summary:        item.Summary,
			CanonicalPaths: compactUnique([]string{item.CanonicalPath}),
			SourceLabel:    "Lens graph trace",
			SourceKind:     "PRODUCT_CHAIN",
			BackingIDs:     compactUnique(item.BackingIDs),
			GeneratedAt:    now,
		})
		if len(out) >= 3 {
			break
		}
	}
	for _, relation := range graph.DirectRelations {
		if !isLensGraphSignalRelation(graph, relation) {
			continue
		}
		out = append(out, horizonmodels.WatchPageSignalItem{
			ID:             "graph-signal:" + strings.TrimSpace(firstNonEmpty(relation.EdgeID, relation.RelatedFragmentID)),
			Kind:           "graph_relation",
			Title:          lensDiagnosticSignalTitle(relation),
			Summary:        relation.WhyRelevant,
			CanonicalPaths: compactUnique([]string{relation.RelatedCanonicalPath}),
			SourceLabel:    "Lens graph trace",
			SourceKind:     strings.TrimSpace(firstNonEmpty(relation.Type, "graph")),
			BackingIDs:     compactUnique([]string{relation.EdgeID, relation.RelatedFragmentID, relation.SeedFragmentID}),
			GeneratedAt:    now,
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildGraphScenarios(
	graph *lensDiagnosticGraph,
	base []horizonmodels.WatchPageScenarioItem,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	direct []horizonmodels.WatchPageAffectedItem,
	indirect []horizonmodels.WatchPageAffectedItem,
	now time.Time,
) []horizonmodels.WatchPageScenarioItem {
	out := append([]horizonmodels.WatchPageScenarioItem(nil), base...)
	if graph == nil {
		return out
	}
	topProductLabel, topProductPath := firstScenarioProduct(products, graph)
	topPathTitle, topPathTarget := firstScenarioSupplyPath(supplyPaths, graph)
	topDirectLabel, topDirectPath := firstScenarioOperationalTarget(direct)
	topIndirectLabel, topIndirectPath := firstScenarioAffected(indirect)
	if len(out) == 0 {
		if topProductLabel != "" {
			out = append(out, horizonmodels.WatchPageScenarioItem{
				ID:          "graph-scenario:product_shortfall",
				Title:       topProductLabel + " shortfall",
				Description: "Stress-test whether the currently constrained product chain keeps tightening and starts starving downstream consumers and transformed products.",
				TargetPaths: compactUnique([]string{topProductPath, topPathTarget}),
				BackingIDs:  compactUnique([]string{topProductPath, topPathTarget}),
				GeneratedAt: now,
			})
		}
		if topDirectLabel != "" {
			out = append(out, horizonmodels.WatchPageScenarioItem{
				ID:          "graph-scenario:direct_disruption",
				Title:       topDirectLabel + " operational disruption",
				Description: "Test what happens if the facility or distribution node that is hit first cannot absorb inbound flows and the constraint spills into the rest of the lens.",
				TargetPaths: compactUnique([]string{topDirectPath, topPathTarget}),
				BackingIDs:  compactUnique([]string{topDirectPath, topPathTarget}),
				GeneratedAt: now,
			})
		}
		if topPathTitle != "" || topIndirectLabel != "" {
			title := topPathTitle
			description := "Test whether the currently stressed route or geography forces rerouting, delays, and second-order shortages across downstream consumers."
			targetPaths := compactUnique([]string{topPathTarget, topIndirectPath})
			backings := compactUnique([]string{topPathTarget, topIndirectPath})
			if title == "" {
				title = topIndirectLabel
				description = "Model second-order disruption if current pressure spreads beyond the first-hit nodes into downstream warehouses, retailers, and transformed-product chains."
			}
			out = append(out, horizonmodels.WatchPageScenarioItem{
				ID:          "graph-scenario:downstream_spread",
				Title:       title + " downstream spread",
				Description: description,
				TargetPaths: targetPaths,
				BackingIDs:  backings,
				GeneratedAt: now,
			})
		}
		return out
	}
	for i := range out {
		title := strings.TrimSpace(out[i].Title)
		switch title {
		case "Supply Chain Disruption":
			if topProductLabel != "" {
				out[i].Title = topProductLabel + " shortfall"
			}
		case "Governance Stress":
			if topDirectLabel != "" {
				out[i].Title = topDirectLabel + " operational disruption"
			}
		case "Regulatory Impact":
			if topPathTitle != "" {
				out[i].Title = topPathTitle + " route escalation"
			} else if topIndirectLabel != "" {
				out[i].Title = topIndirectLabel + " downstream spread"
			}
		}
		if strings.TrimSpace(out[i].Description) == "" {
			out[i].Description = "Scenario contract derived from the current lens graph, stressed supply paths, and downstream dependency surface."
		}
		out[i].BackingIDs = compactUnique(append(out[i].BackingIDs, topProductPath, topPathTarget, topDirectPath, topIndirectPath))
		out[i].GeneratedAt = now
	}
	return out
}

func buildGraphMapFragments(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageMapFragment {
	if graph == nil {
		return nil
	}
	out := []horizonmodels.WatchPageMapFragment{}
	if len(graph.ZonePaths) > 0 {
		paths := firstNStrings(graph.ZonePaths, 3)
		out = append(out, horizonmodels.WatchPageMapFragment{
			ID:             "graph-map:zones",
			Title:          "Tension zones",
			Caption:        "These zones sit closest to the stressed dependency graph and represent where pressure is concentrating geographically.",
			CanonicalPaths: compactUnique(paths),
			LayerHints:     []string{"tension", "impact"},
			ScopeType:      "graph",
			BackingIDs:     compactUnique(paths),
			GeneratedAt:    now,
		})
	}
	if len(graph.RoutePaths) > 0 {
		paths := firstNStrings(graph.RoutePaths, 3)
		out = append(out, horizonmodels.WatchPageMapFragment{
			ID:             "graph-map:routes",
			Title:          "Dependency routes",
			Caption:        "These routes or flows connect the lens seed scope to downstream exposure and show where operational pressure is carried.",
			CanonicalPaths: compactUnique(paths),
			LayerHints:     []string{"route", "dependency"},
			ScopeType:      "graph",
			BackingIDs:     compactUnique(paths),
			GeneratedAt:    now,
		})
	}
	if len(graph.GeoPaths) > 0 {
		paths := firstNStrings(graph.GeoPaths, 3)
		out = append(out, horizonmodels.WatchPageMapFragment{
			ID:             "graph-map:geo",
			Title:          "Operating geography",
			Caption:        "These geographies form the broader operating context around the lens and help explain where external pressure enters the system.",
			CanonicalPaths: compactUnique(paths),
			LayerHints:     []string{"geography", "context"},
			ScopeType:      "graph",
			BackingIDs:     compactUnique(paths),
			GeneratedAt:    now,
		})
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func buildGraphEvidenceItems(graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageEvidenceItem {
	if graph == nil {
		return nil
	}
	out := []horizonmodels.WatchPageEvidenceItem{}
	supplyRelations := filterSupplyRelevantRelations(graph.DirectRelations)
	if len(supplyRelations) > 0 {
		paths := []string{}
		backings := []string{}
		for _, relation := range firstNRelations(supplyRelations, 4) {
			paths = append(paths, relation.RelatedCanonicalPath)
			backings = append(backings, relation.EdgeID, relation.RelatedFragmentID)
		}
		out = append(out, horizonmodels.WatchPageEvidenceItem{
			ID:             "graph-evidence:relations",
			Kind:           "graph_trace",
			Title:          "Lens supply graph trace",
			Summary:        fmt.Sprintf("%d direct supply-relevant graph relations are currently backing this diagnosis.", len(supplyRelations)),
			CanonicalPaths: compactUnique(paths),
			SourceRefs:     compactUnique(backings),
			GeneratedAt:    now,
		})
	}
	return out
}

func mergeScopeItems(primary, secondary []horizonmodels.WatchPageScopeItem, limit int) []horizonmodels.WatchPageScopeItem {
	indexByKey := map[string]int{}
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageScopeItem) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.CanonicalPath, item.ID))
			if key == "" {
				continue
			}
			if idx, ok := indexByKey[key]; ok {
				existing := out[idx]
				if strings.TrimSpace(existing.Label) == "" && strings.TrimSpace(item.Label) != "" {
					existing.Label = strings.TrimSpace(item.Label)
				}
				if strings.TrimSpace(existing.Kind) == "" && strings.TrimSpace(item.Kind) != "" {
					existing.Kind = strings.TrimSpace(item.Kind)
				}
				if strings.TrimSpace(item.Summary) != "" && (strings.TrimSpace(existing.Summary) == "" || len(strings.TrimSpace(item.Summary)) > len(strings.TrimSpace(existing.Summary))) {
					existing.Summary = strings.TrimSpace(item.Summary)
				}
				existing.SourceRefs = compactUnique(append(existing.SourceRefs, item.SourceRefs...))
				existing.BackingIDs = compactUnique(append(existing.BackingIDs, item.BackingIDs...))
				out[idx] = existing
				continue
			}
			indexByKey[key] = len(out)
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(secondary)
	if limit <= 0 || len(out) < limit {
		appendItems(primary)
	}
	return out
}

func mergeRiskItems(primary, secondary []horizonmodels.WatchPageRiskItem, limit int) []horizonmodels.WatchPageRiskItem {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageRiskItem, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageRiskItem) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.CanonicalPath, item.ID, item.Title))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(secondary)
	if limit <= 0 || len(out) < limit {
		appendItems(primary)
	}
	return out
}

func mergeAffectedItems(primary, secondary []horizonmodels.WatchPageAffectedItem, limit int) []horizonmodels.WatchPageAffectedItem {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageAffectedItem, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageAffectedItem) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.CanonicalPath, item.ID))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			item.Rank = len(out) + 1
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(primary)
	if limit <= 0 || len(out) < limit {
		appendItems(secondary)
	}
	return out
}

func mergeMapFragments(primary, secondary []horizonmodels.WatchPageMapFragment, limit int) []horizonmodels.WatchPageMapFragment {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageMapFragment, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageMapFragment) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.ID, firstString(item.CanonicalPaths)))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(secondary)
	if limit <= 0 || len(out) < limit {
		appendItems(primary)
	}
	return out
}

func mergeEvidenceItems(primary, secondary []horizonmodels.WatchPageEvidenceItem, limit int) []horizonmodels.WatchPageEvidenceItem {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageEvidenceItem, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageEvidenceItem) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.ID, item.Title))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(secondary)
	if limit <= 0 || len(out) < limit {
		appendItems(primary)
	}
	return out
}

func mergeSignals(primary, secondary []horizonmodels.WatchPageSignalItem, limit int) []horizonmodels.WatchPageSignalItem {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageSignalItem, 0, minInt(len(primary)+len(secondary), maxInt(1, limit)))
	appendItems := func(items []horizonmodels.WatchPageSignalItem) {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.ID, item.Title))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return
			}
		}
	}
	appendItems(primary)
	if limit <= 0 || len(out) < limit {
		appendItems(secondary)
	}
	return out
}

func firstNRelations(items []lensDiagnosticRelation, limit int) []lensDiagnosticRelation {
	if limit <= 0 || len(items) <= limit {
		return append([]lensDiagnosticRelation(nil), items...)
	}
	return append([]lensDiagnosticRelation(nil), items[:limit]...)
}

func lensDiagnosticFragmentLabel(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if value := strings.TrimSpace(fragment.Labels["en"]); value != "" {
		return value
	}
	for _, value := range fragment.Labels {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	if value := strings.TrimSpace(fragment.Identifiers["legalName"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(fragment.Identifiers["shortName"]); value != "" {
		return value
	}
	return labelFromCanonicalPath(fragment.CanonicalPath)
}

func isLensDiagnosticContextRelation(edgeType string) bool {
	switch strings.TrimSpace(edgeType) {
	case string(horizonmodels.DependencyTypeRegulates),
		string(horizonmodels.DependencyTypeAssures),
		string(horizonmodels.DependencyTypeAuditedBy),
		string(horizonmodels.DependencyTypeLocatedIn),
		string(horizonmodels.DependencyTypeOperatesIn),
		"BASED_IN",
		"HEADQUARTERED_IN",
		string(horizonmodels.DependencyTypeParticipatesIn),
		string(horizonmodels.DependencyTypeTradedIn):
		return true
	default:
		return false
	}
}

func describeLensDiagnosticRelation(centerLabel string, relation lensDiagnosticRelation) string {
	other := firstNonEmpty(relation.RelatedLabel, labelFromCanonicalPath(relation.RelatedCanonicalPath), "this object")
	switch relation.Type {
	case string(horizonmodels.DependencyTypeSuppliesTo):
		return fmt.Sprintf("%s supplies into the current lens graph, so downstream availability depends on that link.", other)
	case string(horizonmodels.DependencyTypeDependsOn), "REQUIRES", "USES":
		return fmt.Sprintf("The current lens graph depends on %s, making it an immediate operational dependency.", other)
	case string(horizonmodels.DependencyTypeControls), string(horizonmodels.DependencyTypeManages), string(horizonmodels.DependencyTypeOwns), string(horizonmodels.DependencyTypeInvestsIn):
		return fmt.Sprintf("%s sits in the control chain around %s.", other, firstNonEmpty(centerLabel, "the current seed"))
	case string(horizonmodels.DependencyTypeRegulates), string(horizonmodels.DependencyTypeAssures), string(horizonmodels.DependencyTypeAuditedBy):
		return fmt.Sprintf("%s shapes the operating conditions and oversight around %s.", other, firstNonEmpty(centerLabel, "the current seed"))
	case string(horizonmodels.DependencyTypeLocatedIn), string(horizonmodels.DependencyTypeOperatesIn), "BASED_IN", "HEADQUARTERED_IN":
		return fmt.Sprintf("%s defines the geographic operating context around %s.", other, firstNonEmpty(centerLabel, "the current seed"))
	case string(horizonmodels.DependencyTypeParticipatesIn), string(horizonmodels.DependencyTypeTradedIn):
		return fmt.Sprintf("%s places the lens graph inside the market context where pressure is transmitted.", other)
	case string(horizonmodels.DependencyTypeConsumes):
		return fmt.Sprintf("%s directly consumes a product already inside the lens graph.", other)
	case string(horizonmodels.DependencyTypeProduces):
		return fmt.Sprintf("%s produces one of the products carrying current exposure.", other)
	case string(horizonmodels.DependencyTypeTransformsInto):
		return fmt.Sprintf("%s appears in the transformed downstream product chain surfaced by the lens.", other)
	case string(horizonmodels.DependencyTypeAssemblesInto):
		return fmt.Sprintf("%s appears in the assembled downstream product chain surfaced by the lens.", other)
	case string(horizonmodels.DependencyTypeAffects):
		return fmt.Sprintf("%s is a direct affected node or zone feeding pressure into the lens.", other)
	default:
		return fmt.Sprintf("%s is part of the immediate relationship logic around %s.", other, firstNonEmpty(centerLabel, "the current seed"))
	}
}

func lensDiagnosticRelationPriority(edgeType string) int {
	switch strings.TrimSpace(edgeType) {
	case string(horizonmodels.DependencyTypeSuppliesTo):
		return 100
	case string(horizonmodels.DependencyTypeDependsOn):
		return 99
	case string(horizonmodels.DependencyTypeProduces):
		return 97
	case string(horizonmodels.DependencyTypeConsumes):
		return 96
	case string(horizonmodels.DependencyTypeTransformsInto):
		return 95
	case string(horizonmodels.DependencyTypeAssemblesInto):
		return 95
	case string(horizonmodels.DependencyTypeAffects):
		return 94
	case string(horizonmodels.DependencyTypeRegulates):
		return 90
	case string(horizonmodels.DependencyTypeLocatedIn), string(horizonmodels.DependencyTypeOperatesIn):
		return 86
	default:
		return 70
	}
}

func lensDiagnosticSignalTitle(relation lensDiagnosticRelation) string {
	other := firstNonEmpty(relation.RelatedLabel, labelFromCanonicalPath(relation.RelatedCanonicalPath), "related node")
	switch relation.Type {
	case string(horizonmodels.DependencyTypeSuppliesTo):
		return other + " supply dependency"
	case string(horizonmodels.DependencyTypeDependsOn):
		return "Operational dependency on " + other
	case string(horizonmodels.DependencyTypeConsumes):
		return other + " consumer dependency"
	case string(horizonmodels.DependencyTypeAffects):
		return "Pressure originating in " + other
	case string(horizonmodels.DependencyTypeTransformsInto):
		return other + " transformed output chain"
	case string(horizonmodels.DependencyTypeAssemblesInto):
		return other + " assembled output chain"
	default:
		return other + " is shaping the current diagnosis"
	}
}

func isLensSupplyRelevantRelation(edgeType string) bool {
	switch strings.TrimSpace(edgeType) {
	case string(horizonmodels.DependencyTypeSuppliesTo),
		string(horizonmodels.DependencyTypeProduces),
		string(horizonmodels.DependencyTypeConsumes),
		string(horizonmodels.DependencyTypeTransformsInto),
		string(horizonmodels.DependencyTypeAssemblesInto),
		string(horizonmodels.DependencyTypeDependsOn),
		string(horizonmodels.DependencyTypeExtractsFrom),
		string(horizonmodels.DependencyTypeAffects),
		string(horizonmodels.DependencyTypeLocatedIn),
		string(horizonmodels.DependencyTypeOperatesIn),
		string(horizonmodels.DependencyTypeParticipatesIn),
		string(horizonmodels.DependencyTypeTradedIn):
		return true
	default:
		return false
	}
}

func filterSupplyRelevantRelations(in []lensDiagnosticRelation) []lensDiagnosticRelation {
	out := make([]lensDiagnosticRelation, 0, len(in))
	for _, item := range in {
		if isLensSupplyRelevantRelation(item.Type) {
			out = append(out, item)
		}
	}
	return out
}

func isLensGraphSignalRelation(graph *lensDiagnosticGraph, relation lensDiagnosticRelation) bool {
	if graph == nil || !isLensSupplyRelevantRelation(relation.Type) {
		return false
	}
	path := strings.TrimSpace(relation.RelatedCanonicalPath)
	switch relation.Type {
	case string(horizonmodels.DependencyTypeConsumes), string(horizonmodels.DependencyTypeTransformsInto), string(horizonmodels.DependencyTypeAssemblesInto):
		return true
	case string(horizonmodels.DependencyTypeAffects), string(horizonmodels.DependencyTypeDependsOn):
		return canonicalPathImpactPriority(path) >= 2
	case string(horizonmodels.DependencyTypeSuppliesTo):
		return strings.HasPrefix(path, "/product/") || strings.HasPrefix(path, "/route/") || strings.HasPrefix(path, "/flow/")
	default:
		return strings.HasPrefix(path, "/route/") ||
			strings.HasPrefix(path, "/flow/") ||
			strings.HasPrefix(path, "/zone/") ||
			strings.HasPrefix(path, "/market/") ||
			strings.HasPrefix(path, "/product/")
	}
}

func firstScenarioProduct(products []horizonmodels.WatchPageScopeItem, graph *lensDiagnosticGraph) (string, string) {
	for _, item := range products {
		path := strings.TrimSpace(item.CanonicalPath)
		if !strings.HasPrefix(path, "/product/") {
			continue
		}
		return strings.TrimSpace(firstNonEmpty(item.Label, labelFromCanonicalPath(path))), path
	}
	if graph != nil && len(graph.ProductPaths) > 0 {
		path := firstString(graph.ProductPaths)
		return labelFromCanonicalPath(path), path
	}
	return "", ""
}

func firstScenarioSupplyPath(paths []horizonmodels.WatchPageSupplyPath, graph *lensDiagnosticGraph) (string, string) {
	for _, item := range paths {
		title := strings.TrimSpace(item.Title)
		target := strings.TrimSpace(firstNonEmpty(firstString(item.RouteRefs), firstString(item.AffectedZoneRefs), item.AnchorCustomer, item.AnchorSupplier))
		if title != "" || target != "" {
			return strings.TrimSpace(firstNonEmpty(title, labelFromCanonicalPath(target))), target
		}
	}
	if graph != nil {
		if path := firstString(graph.RoutePaths); path != "" {
			return labelFromCanonicalPath(path), path
		}
		if path := firstString(graph.ZonePaths); path != "" {
			return labelFromCanonicalPath(path), path
		}
	}
	return "", ""
}

func firstScenarioAffected(items []horizonmodels.WatchPageAffectedItem) (string, string) {
	for _, item := range items {
		path := strings.TrimSpace(item.CanonicalPath)
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/market/") || strings.HasPrefix(path, "/product/") {
			return strings.TrimSpace(firstNonEmpty(item.Label, labelFromCanonicalPath(path))), path
		}
	}
	for _, item := range items {
		path := strings.TrimSpace(item.CanonicalPath)
		if path == "" {
			continue
		}
		return strings.TrimSpace(firstNonEmpty(item.Label, labelFromCanonicalPath(path))), path
	}
	return "", ""
}

func firstScenarioOperationalTarget(items []horizonmodels.WatchPageAffectedItem) (string, string) {
	for _, item := range items {
		path := strings.TrimSpace(item.CanonicalPath)
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/market/") {
			return strings.TrimSpace(firstNonEmpty(item.Label, labelFromCanonicalPath(path))), path
		}
	}
	return firstScenarioAffected(items)
}

func firstString(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func lensDiagnosticSeedCount(graph *lensDiagnosticGraph) int {
	if graph == nil {
		return 0
	}
	return len(graph.Seeds)
}

func lensDiagnosticNodeCount(graph *lensDiagnosticGraph) int {
	if graph == nil {
		return 0
	}
	return len(graph.Nodes)
}

func lensDiagnosticDirectRelationCount(graph *lensDiagnosticGraph) int {
	if graph == nil {
		return 0
	}
	return len(graph.DirectRelations)
}

func lensDiagnosticPropagationCount(graph *lensDiagnosticGraph) int {
	return 0
}
