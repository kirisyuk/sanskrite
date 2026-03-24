package propagation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type signalStateKey struct {
	FragmentID string
	Axis       string
}

type signalStateNode struct {
	Key       signalStateKey
	Fragment  *horizonmodels.HorizonFragment
	Hops      int
	PathIDs   []string
	PathCanon []string
}

type signalStateTransition struct {
	To        signalStateKey
	Weight    float64
	Edge      *horizonmodels.DependencyEdge
	Direction string
	Meta      transferTraceMeta
	Factors   map[string]float64
	Hops      int
}

func (e *PropagationEngine) simulateSignalTypedRWR(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
	seedFragmentIDs []string,
	cfg SignalKernelConfig,
) (*SignalSimulationResult, error) {
	startTime := time.Now()
	traceCollector := newSignalTraceCollector(signalTraceConfigFromContext(ctx))
	graph, seedStates, err := e.buildSignalStateGraph(ctx, signal, profileLookup, seedFragmentIDs, cfg, traceCollector)
	if err != nil {
		return nil, err
	}
	if len(seedStates) == 0 {
		return &SignalSimulationResult{
			AffectedFragments: []string{},
			PropagationPaths:  [][]string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           mergeMetricMaps(map[string]float64{}, signalKernelModeMetric(cfg.Mode)),
			Accumulated:       map[string][]AccumulatedSignal{},
		}, nil
	}
	selected, scores := runTypedRandomWalk(signal, graph, seedStates, cfg)
	result := buildTypedRWRSimulationResult(signal, cfg, graph, selected, scores, traceCollector, time.Since(startTime))
	result.Metrics = mergeMetricMaps(result.Metrics, signalKernelModeMetric(cfg.Mode))
	return result, nil
}

func (e *PropagationEngine) propagateSignalTypedRWR(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
	seedFragmentIDs []string,
	cfg SignalKernelConfig,
) (*SignalPropagationResult, error) {
	startTime := time.Now()
	traceCollector := newSignalTraceCollector(signalTraceConfigFromContext(ctx))
	graph, seedStates, err := e.buildSignalStateGraph(ctx, signal, profileLookup, seedFragmentIDs, cfg, traceCollector)
	if err != nil {
		return nil, err
	}
	if len(seedStates) == 0 {
		return &SignalPropagationResult{
			AffectedFragments: []string{},
			PropagationPaths:  [][]string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           mergeMetricMaps(map[string]float64{}, signalKernelModeMetric(cfg.Mode)),
		}, nil
	}
	selected, scores := runTypedRandomWalk(signal, graph, seedStates, cfg)
	affectedSet := make(map[string]struct{}, len(selected))
	paths := make([][]string, 0, len(selected))
	for _, state := range selected {
		node := graph.Nodes[state]
		if node == nil || node.Fragment == nil {
			continue
		}
		amp := typedRWRAmplitude(signal, scores[state], graph.MaxScore)
		if amp < MinAmplitudeThreshold {
			continue
		}
		e.updateNodeMetric(node.Fragment, e.buildMetricPath(signal.Polarity, state.Axis, signal.Mechanism), amp, signal, node.PathCanon, node.Hops)
		if err := e.fragmentsRepo.UpdateFast(ctx, node.Fragment); err == nil {
			affectedSet[node.Fragment.ID] = struct{}{}
			paths = append(paths, append([]string{}, node.PathCanon...))
			e.fireFragmentChangedHook(ctx, node.Fragment.ID)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   node.Fragment.ID,
				ToPath:       strings.TrimSpace(node.Fragment.CanonicalPath),
				AxisOut:      state.Axis,
				AmplitudeOut: amp,
				Hops:         node.Hops,
				Outcome:      "node_updated",
				Reason:       "typed_rwr_ranked",
				Factors: map[string]float64{
					"rwr_score": scores[state],
				},
			})
		}
	}
	affected := make([]string, 0, len(affectedSet))
	for id := range affectedSet {
		affected = append(affected, id)
	}
	sort.Strings(affected)
	res := &SignalPropagationResult{
		AffectedFragments: affected,
		PropagationPaths:  mergePathLists(paths, nil),
		PropagationTime:   time.Since(startTime),
		Metrics: map[string]float64{
			"nodes_updated":        float64(len(affected)),
			"paths_traced":         float64(len(paths)),
			"typed_rwr_selected":   float64(len(selected)),
			"typed_rwr_max_score":  graph.MaxScore,
			"typed_rwr_iterations": float64(cfg.MaxIterations),
		},
	}
	res.Metrics = mergeMetricMaps(res.Metrics, signalKernelModeMetric(cfg.Mode))
	res.Trace, res.TraceDropped = traceCollector.snapshot()
	if len(res.Trace) > 0 {
		res.Metrics["trace_steps"] = float64(len(res.Trace))
	}
	if res.TraceDropped > 0 {
		res.Metrics["trace_dropped"] = float64(res.TraceDropped)
	}
	return res, nil
}

type signalStateGraph struct {
	Nodes       map[signalStateKey]*signalStateNode
	Transitions map[signalStateKey][]signalStateTransition
	MaxScore    float64
}

func (e *PropagationEngine) buildSignalStateGraph(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
	seedFragmentIDs []string,
	cfg SignalKernelConfig,
	traceCollector *signalTraceCollector,
) (*signalStateGraph, []signalStateKey, error) {
	if err := requireProfileLookup(profileLookup); err != nil {
		return nil, nil, err
	}
	baseProfile, err := profileLookup.GetForSignal(signal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get propagation profile: %w", err)
	}
	maxHops := baseProfile.DefaultMaxHops + cfg.ExpansionHops
	if maxHops < 1 {
		maxHops = 1
	}
	fragmentCache := make(map[string]*horizonmodels.HorizonFragment, len(seedFragmentIDs)*4)
	neighborsCache := make(map[string][]neighborInfo)
	graph := &signalStateGraph{
		Nodes:       make(map[signalStateKey]*signalStateNode, len(seedFragmentIDs)*4),
		Transitions: make(map[signalStateKey][]signalStateTransition, len(seedFragmentIDs)*4),
	}
	seedStates := make([]signalStateKey, 0, len(seedFragmentIDs))
	adapter := e.resolveDomainAdapter(ctx, nil)
	if len(seedFragmentIDs) > 0 {
		if seedFrag, err := e.fragmentsRepo.Get(ctx, seedFragmentIDs[0]); err == nil && seedFrag != nil {
			adapter = e.resolveDomainAdapter(ctx, seedFrag)
			fragmentCache[seedFrag.ID] = seedFrag
		}
	}
	type queueItem struct {
		state       signalStateKey
		hops        int
		pathIDs     []string
		pathCanon   []string
		artifactKey string
	}
	queue := make([]queueItem, 0, len(seedFragmentIDs))
	bestHops := make(map[signalStateKey]int, len(seedFragmentIDs)*4)
	for _, fragID := range seedFragmentIDs {
		frag := e.getFragmentForTrace(ctx, fragID, fragmentCache)
		if frag == nil {
			continue
		}
		state := signalStateKey{FragmentID: fragID, Axis: strings.TrimSpace(signal.Axis)}
		seedStates = append(seedStates, state)
		graph.Nodes[state] = &signalStateNode{
			Key:       state,
			Fragment:  frag,
			Hops:      0,
			PathIDs:   []string{frag.ID},
			PathCanon: []string{strings.TrimSpace(frag.CanonicalPath)},
		}
		bestHops[state] = 0
		queue = append(queue, queueItem{state: state, pathIDs: []string{frag.ID}, pathCanon: []string{strings.TrimSpace(frag.CanonicalPath)}})
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			ToFragment:   frag.ID,
			ToPath:       strings.TrimSpace(frag.CanonicalPath),
			AxisOut:      state.Axis,
			AmplitudeOut: signal.Amplitude,
			Hops:         0,
			Outcome:      "seed",
		})
	}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.hops >= maxHops {
			continue
		}
		node := graph.Nodes[item.state]
		if node == nil || node.Fragment == nil {
			continue
		}
		profile, err := profileLookup.Get(item.state.Axis)
		if err != nil {
			continue
		}
		cacheKey := item.state.FragmentID + "|" + item.state.Axis
		neighbors, ok := neighborsCache[cacheKey]
		if !ok {
			loaded, err := e.getNeighborsViaStrongEdges(ctx, node.Fragment, profile)
			if err != nil {
				continue
			}
			neighbors = loaded
			neighborsCache[cacheKey] = loaded
		}
		outgoingAmplitude := signal.Amplitude
		if IsChokepointFragment(node.Fragment) {
			outgoingAmplitude = TransitAmplify(outgoingAmplitude, item.state.Axis)
		}
		for _, neighbor := range neighbors {
			nextAxis, transition, ok := e.buildTypedRWRTransition(ctx, signal, adapter, node, neighbor, outgoingAmplitude, item, profile, fragmentCache, traceCollector)
			if !ok {
				continue
			}
			nextState := signalStateKey{FragmentID: neighbor.fragmentID, Axis: nextAxis}
			graph.Transitions[item.state] = append(graph.Transitions[item.state], transition)
			if prev, seen := bestHops[nextState]; seen && prev <= item.hops+1 {
				continue
			}
			targetFrag := e.getFragmentForTrace(ctx, neighbor.fragmentID, fragmentCache)
			if targetFrag == nil {
				continue
			}
			pathIDs := append(append([]string{}, item.pathIDs...), targetFrag.ID)
			pathCanon := append(append([]string{}, item.pathCanon...), strings.TrimSpace(targetFrag.CanonicalPath))
			graph.Nodes[nextState] = &signalStateNode{
				Key:       nextState,
				Fragment:  targetFrag,
				Hops:      item.hops + 1,
				PathIDs:   pathIDs,
				PathCanon: pathCanon,
			}
			bestHops[nextState] = item.hops + 1
			queue = append(queue, queueItem{
				state:       nextState,
				hops:        item.hops + 1,
				pathIDs:     pathIDs,
				pathCanon:   pathCanon,
				artifactKey: EdgeArtifactKey(neighbor.edge),
			})
		}
	}
	return graph, seedStates, nil
}

func (e *PropagationEngine) buildTypedRWRTransition(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	adapter DomainModelAdapter,
	fromNode *signalStateNode,
	neighbor neighborInfo,
	outgoingAmplitude float64,
	item struct {
		state       signalStateKey
		hops        int
		pathIDs     []string
		pathCanon   []string
		artifactKey string
	},
	profile *horizonmodels.PropagationProfile,
	fragmentCache map[string]*horizonmodels.HorizonFragment,
	traceCollector *signalTraceCollector,
) (string, signalStateTransition, bool) {
	breakdown := e.calculateDampedAmplitudeWithBreakdown(outgoingAmplitude, fromNode.Key.Axis, fromNode.Fragment, neighbor.edge, profile)
	nextAmplitude := breakdown.Amplitude
	traceFactors := cloneFactorMap(breakdown.Factors)
	nextAxis, transformFactor := e.applyTransformation(fromNode.Key.Axis, neighbor.edge, profile)
	traceFactors = mergeFactorMaps(traceFactors, map[string]float64{"axis_transform_factor": transformFactor}, "")
	if transformFactor != 0 && transformFactor != 1 {
		nextAmplitude *= transformFactor
	}
	if adapter != nil {
		beforeHook := nextAmplitude
		hookSignal := &horizonmodels.SignalDescriptor{
			Axis:              nextAxis,
			Mechanism:         signal.Mechanism,
			Amplitude:         nextAmplitude,
			Polarity:          signal.Polarity,
			RegulatoryContext: signal.RegulatoryContext,
		}
		if hook, err := adapter.TransferHook(ctx, hookSignal, neighbor.edge, fromNode.Fragment); err == nil && hook != nil {
			traceFactors = mergeFactorMaps(traceFactors, hook.DebugFactors, "adapter.")
			if hook.Skip {
				meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), fromNode.Fragment, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					FromFragment: fromNode.Fragment.ID,
					ToFragment:   neighbor.fragmentID,
					FromPath:     meta.FromPath,
					ToPath:       meta.ToPath,
					FromGeo:      meta.FromGeo,
					ToGeo:        meta.ToGeo,
					EdgeID:       neighbor.edge.ID,
					EdgeType:     string(neighbor.edge.Type),
					Direction:    neighbor.traversalDirection,
					Channel:      meta.Channel,
					Scope:        meta.Scope,
					Spillover:    meta.Spillover,
					AxisIn:       fromNode.Key.Axis,
					AxisOut:      nextAxis,
					AmplitudeIn:  outgoingAmplitude,
					AmplitudeOut: nextAmplitude,
					Hops:         item.hops + 1,
					Outcome:      "dropped",
					Reason:       "transfer_skip",
					Factors:      traceFactors,
				})
				return "", signalStateTransition{}, false
			}
			if hook.AdjustedAmplitude > 0 {
				if beforeHook > 0 {
					traceFactors = mergeFactorMaps(traceFactors, map[string]float64{"adapter_adjusted_factor": hook.AdjustedAmplitude / beforeHook}, "")
				}
				nextAmplitude = clamp01(hook.AdjustedAmplitude)
			}
			if axis := strings.TrimSpace(hook.AdjustedAxis); axis != "" && horizonmodels.IsValidAxis(axis) {
				nextAxis = axis
			}
		}
	}
	neighborArtifactKey := EdgeArtifactKey(neighbor.edge)
	coherence := WaveguideCoherenceBonus(item.artifactKey, neighborArtifactKey)
	if coherence != 1.0 {
		nextAmplitude *= coherence
	}
	targetFrag := e.getFragmentForTrace(ctx, neighbor.fragmentID, fragmentCache)
	if targetFrag == nil {
		return "", signalStateTransition{}, false
	}
	meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), fromNode.Fragment, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
	if nextAmplitude < MinAmplitudeThreshold {
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			FromFragment: fromNode.Fragment.ID,
			ToFragment:   neighbor.fragmentID,
			FromPath:     meta.FromPath,
			ToPath:       meta.ToPath,
			FromGeo:      meta.FromGeo,
			ToGeo:        meta.ToGeo,
			EdgeID:       neighbor.edge.ID,
			EdgeType:     string(neighbor.edge.Type),
			Direction:    neighbor.traversalDirection,
			Channel:      meta.Channel,
			Scope:        meta.Scope,
			Spillover:    meta.Spillover,
			AxisIn:       fromNode.Key.Axis,
			AxisOut:      nextAxis,
			AmplitudeIn:  outgoingAmplitude,
			AmplitudeOut: nextAmplitude,
			Hops:         item.hops + 1,
			Outcome:      "dropped",
			Reason:       "damped_below_threshold",
			Factors:      traceFactors,
		})
		return "", signalStateTransition{}, false
	}
	weight := typedRWRTransitionWeight(signal, nextAxis, targetFrag, neighbor.edge, meta, nextAmplitude)
	traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
		"waveguide_coherence": coherence,
		"typed_rwr_weight":    weight,
	}, "")
	traceCollector.add(SignalPropagationTraceEvent{
		SignalID:     signal.ID,
		FromFragment: fromNode.Fragment.ID,
		ToFragment:   targetFrag.ID,
		FromPath:     meta.FromPath,
		ToPath:       meta.ToPath,
		FromGeo:      meta.FromGeo,
		ToGeo:        meta.ToGeo,
		EdgeID:       neighbor.edge.ID,
		EdgeType:     string(neighbor.edge.Type),
		Direction:    neighbor.traversalDirection,
		Channel:      meta.Channel,
		Scope:        meta.Scope,
		Spillover:    meta.Spillover,
		AxisIn:       fromNode.Key.Axis,
		AxisOut:      nextAxis,
		AmplitudeIn:  outgoingAmplitude,
		AmplitudeOut: nextAmplitude,
		Hops:         item.hops + 1,
		Outcome:      "propagated",
		Reason:       "typed_rwr_graph",
		Factors:      traceFactors,
	})
	return nextAxis, signalStateTransition{
		To:        signalStateKey{FragmentID: targetFrag.ID, Axis: nextAxis},
		Weight:    weight,
		Edge:      neighbor.edge,
		Direction: neighbor.traversalDirection,
		Meta:      meta,
		Factors:   traceFactors,
		Hops:      item.hops + 1,
	}, true
}

func runTypedRandomWalk(signal *horizonmodels.SignalDescriptor, graph *signalStateGraph, seedStates []signalStateKey, cfg SignalKernelConfig) ([]signalStateKey, map[signalStateKey]float64) {
	if graph == nil || len(seedStates) == 0 {
		return nil, map[signalStateKey]float64{}
	}
	seedVec := make(map[signalStateKey]float64, len(seedStates))
	seedWeight := 1.0 / float64(len(seedStates))
	for _, state := range seedStates {
		seedVec[state] += seedWeight
	}
	scores := make(map[signalStateKey]float64, len(seedVec))
	for k, v := range seedVec {
		scores[k] = v
	}
	for i := 0; i < cfg.MaxIterations; i++ {
		nextScores := make(map[signalStateKey]float64, len(scores)+len(seedVec))
		for state, seedProb := range seedVec {
			nextScores[state] += cfg.RestartProbability * seedProb
		}
		for state, score := range scores {
			transitions := graph.Transitions[state]
			if len(transitions) == 0 {
				nextScores[state] += (1 - cfg.RestartProbability) * score
				continue
			}
			totalWeight := 0.0
			for _, tr := range transitions {
				totalWeight += maxFloat(0, tr.Weight)
			}
			if totalWeight <= 0 {
				nextScores[state] += (1 - cfg.RestartProbability) * score
				continue
			}
			for _, tr := range transitions {
				w := maxFloat(0, tr.Weight)
				if w == 0 {
					continue
				}
				nextScores[tr.To] += (1 - cfg.RestartProbability) * score * (w / totalWeight)
			}
		}
		scores = nextScores
	}
	type rankedState struct {
		Key   signalStateKey
		Score float64
		Rank  float64
	}
	ranked := make([]rankedState, 0, len(scores))
	seedSet := make(map[signalStateKey]struct{}, len(seedStates))
	for _, state := range seedStates {
		seedSet[state] = struct{}{}
	}
	graph.MaxScore = 0
	for state, score := range scores {
		if score > graph.MaxScore {
			graph.MaxScore = score
		}
		if score < cfg.MinTailScore {
			if _, isSeed := seedSet[state]; !isSeed || !cfg.IncludeSeedNodes {
				continue
			}
		}
		node := graph.Nodes[state]
		rank := score
		if node != nil {
			rank = typedRWRStateRank(signal, node, score)
		}
		ranked = append(ranked, rankedState{Key: state, Score: score, Rank: rank})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Rank == ranked[j].Rank {
			if ranked[i].Key.Axis == ranked[j].Key.Axis {
				return ranked[i].Key.FragmentID < ranked[j].Key.FragmentID
			}
			return ranked[i].Key.Axis < ranked[j].Key.Axis
		}
		return ranked[i].Rank > ranked[j].Rank
	})
	selected := make([]signalStateKey, 0, len(seedStates)+cfg.MaxTailNodes)
	tailCount := 0
	for _, item := range ranked {
		_, isSeed := seedSet[item.Key]
		if isSeed {
			if cfg.IncludeSeedNodes {
				selected = append(selected, item.Key)
			}
			continue
		}
		if item.Score < cfg.MinTailScore {
			continue
		}
		selected = append(selected, item.Key)
		tailCount++
		if tailCount >= cfg.MaxTailNodes {
			break
		}
	}
	return selected, scores
}

func buildTypedRWRSimulationResult(
	signal *horizonmodels.SignalDescriptor,
	cfg SignalKernelConfig,
	graph *signalStateGraph,
	selected []signalStateKey,
	scores map[signalStateKey]float64,
	traceCollector *signalTraceCollector,
	duration time.Duration,
) *SignalSimulationResult {
	affected := make([]string, 0, len(selected))
	paths := make([][]string, 0, len(selected))
	acc := make(map[string][]AccumulatedSignal, len(selected))
	seenFragments := map[string]struct{}{}
	for _, state := range selected {
		node := graph.Nodes[state]
		if node == nil || node.Fragment == nil {
			continue
		}
		amp := typedRWRAmplitude(signal, scores[state], graph.MaxScore)
		if amp < MinAmplitudeThreshold {
			continue
		}
		if _, ok := seenFragments[node.Fragment.ID]; !ok {
			seenFragments[node.Fragment.ID] = struct{}{}
			affected = append(affected, node.Fragment.ID)
		}
		paths = append(paths, append([]string{}, node.PathCanon...))
		acc[node.Fragment.ID] = append(acc[node.Fragment.ID], AccumulatedSignal{
			Axis:      state.Axis,
			Amplitude: amp,
			Polarity:  signal.Polarity,
			HopCount:  node.Hops,
		})
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			ToFragment:   node.Fragment.ID,
			ToPath:       strings.TrimSpace(node.Fragment.CanonicalPath),
			AxisOut:      state.Axis,
			AmplitudeOut: amp,
			Hops:         node.Hops,
			Outcome:      "node_updated",
			Reason:       "typed_rwr_ranked",
			Factors: map[string]float64{
				"rwr_score": scores[state],
			},
		})
	}
	sort.Strings(affected)
	res := &SignalSimulationResult{
		AffectedFragments: affected,
		PropagationPaths:  mergePathLists(paths, nil),
		PropagationTime:   duration,
		Metrics: map[string]float64{
			"nodes_updated":        float64(len(affected)),
			"paths_traced":         float64(len(paths)),
			"typed_rwr_selected":   float64(len(selected)),
			"typed_rwr_max_score":  graph.MaxScore,
			"typed_rwr_iterations": float64(cfg.MaxIterations),
		},
		Accumulated: acc,
	}
	res.Trace, res.TraceDropped = traceCollector.snapshot()
	if len(res.Trace) > 0 {
		res.Metrics["trace_steps"] = float64(len(res.Trace))
	}
	if res.TraceDropped > 0 {
		res.Metrics["trace_dropped"] = float64(res.TraceDropped)
	}
	return res
}

func typedRWRTransitionWeight(signal *horizonmodels.SignalDescriptor, nextAxis string, target *horizonmodels.HorizonFragment, edge *horizonmodels.DependencyEdge, meta transferTraceMeta, nextAmplitude float64) float64 {
	overlay := BuildPropagationNodeOverlay(target)
	family := classifyEdgeFamily(nextAxis, target, edge)
	weight := nextAmplitude
	weight *= 0.70 + overlay.OperationalScore*0.35 + overlay.CriticalityScore*0.25 + overlay.BridgeScore*0.20
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		if overlay.PrimaryRole == "chokepoint" || overlay.PrimaryRole == "energy_asset" || overlay.PrimaryRole == "logistics_hub" || overlay.PrimaryRole == "manufacturing_operator" {
			weight *= 1.22
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		if overlay.PrimaryRole == "logistics_hub" || overlay.PrimaryRole == "chokepoint" || overlay.BusinessRole == "shipper" {
			weight *= 1.24
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		if overlay.PrimaryRole == "energy_asset" || overlay.PrimaryRole == "manufacturing_operator" || overlay.PrimaryRole == "market_aggregator" {
			weight *= 1.20
		}
	case horizonmodels.AxisOperationalContinuity:
		if overlay.PrimaryRole == "manufacturing_operator" || overlay.PrimaryRole == "logistics_hub" {
			weight *= 1.18
		}
	}
	if strings.TrimSpace(meta.Scope) == "cross_border" {
		weight *= 1.08
	}
	if family == "chokepoint_logistics" || family == "energy_supply_chain" || family == "facility_dependency" {
		weight *= 1.10
	}
	canonicalPath := strings.TrimSpace(target.CanonicalPath)
	if strings.HasPrefix(canonicalPath, "/sphere/") {
		weight *= 0.32
	}
	if strings.HasPrefix(canonicalPath, "/product/") {
		weight *= 0.58
	}
	if isGenericLocPath(canonicalPath) {
		weight *= 0.44
	}
	return clampTypedWeight(weight)
}

func typedRWRStateRank(signal *horizonmodels.SignalDescriptor, node *signalStateNode, score float64) float64 {
	if node == nil || node.Fragment == nil {
		return score
	}
	overlay := BuildPropagationNodeOverlay(node.Fragment)
	rank := score * (0.72 + overlay.OperationalScore*0.36 + overlay.CriticalityScore*0.30 + overlay.BridgeScore*0.24)
	canonicalPath := strings.TrimSpace(node.Fragment.CanonicalPath)
	businessAxis := false
	if signal != nil {
		switch strings.TrimSpace(signal.Axis) {
		case horizonmodels.AxisGeoConflict,
			horizonmodels.AxisLogisticsCapacity,
			horizonmodels.AxisLogisticsLatency,
			horizonmodels.AxisPhysicalFlowSupply,
			horizonmodels.AxisPhysicalFlowDemand,
			horizonmodels.AxisOperationalContinuity:
			businessAxis = true
		}
	}
	if businessAxis {
		switch overlay.PrimaryRole {
		case "chokepoint", "energy_asset", "logistics_hub", "manufacturing_operator":
			rank *= 1.24
		case "market_aggregator":
			rank *= 1.12
		case "regulator":
			rank *= 0.78
		case "geo_theater", "generic_node":
			rank *= 0.42
		}
		if strings.HasPrefix(canonicalPath, "/loc/") && !IsChokepointFragment(node.Fragment) {
			rank *= 0.30
		}
		if strings.HasPrefix(canonicalPath, "/sphere/") {
			rank *= 0.18
		}
		if strings.HasPrefix(canonicalPath, "/product/") {
			rank *= 0.46
		}
		if isGenericLocPath(canonicalPath) {
			rank *= 0.34
		}
		if strings.HasPrefix(canonicalPath, "/fac/") {
			rank *= 1.16
		}
		if strings.HasPrefix(canonicalPath, "/market/") {
			rank *= 1.10
		}
	}
	return rank
}

func typedRWRAmplitude(signal *horizonmodels.SignalDescriptor, score, maxScore float64) float64 {
	if signal == nil || score <= 0 {
		return 0
	}
	if maxScore <= 0 {
		maxScore = score
	}
	norm := clamp01(score / maxScore)
	return clamp01(signal.Amplitude * math.Sqrt(norm))
}

func clampTypedWeight(v float64) float64 {
	if v < 0.01 {
		return 0.01
	}
	if v > 2.5 {
		return 2.5
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
