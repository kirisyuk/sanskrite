package propagation

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

const (
	// MinAmplitudeThreshold is the minimum amplitude below which propagation stops.
	MinAmplitudeThreshold = 0.05
	// TopKContributions is the maximum number of contributions stored per (node, metricPath).
	TopKContributions = 10
	// MaxHopsForWeakAmplitude is the hop count beyond which weak signals are dropped (tail noise).
	MaxHopsForWeakAmplitude = 4
	// WeakAmplitudeThreshold is the amplitude below which distant signals (hops >= MaxHopsForWeakAmplitude) are dropped.
	WeakAmplitudeThreshold = 0.25
	// propagationPrefetchWindow controls how many pending frontier nodes are best-effort prefetched in one batch.
	propagationPrefetchWindow = 64
)

// SignalPropagationResult represents the result of signal propagation.
type SignalPropagationResult struct {
	AffectedFragments []string                      `json:"affectedFragments"`
	PropagationPaths  [][]string                    `json:"propagationPaths"`
	PropagationTime   time.Duration                 `json:"propagationTime"`
	Metrics           map[string]float64            `json:"metrics"`
	Trace             []SignalPropagationTraceEvent `json:"trace,omitempty"`
	TraceDropped      int                           `json:"traceDropped,omitempty"`
}

// SignalSimulationResult represents non-persistent propagation output.
// It keeps the same traversal semantics as PropagateSignalFromSeeds, but does not write fragment.State.
type SignalSimulationResult struct {
	AffectedFragments []string                       `json:"affectedFragments"`
	PropagationPaths  [][]string                     `json:"propagationPaths"`
	PropagationTime   time.Duration                  `json:"propagationTime"`
	Metrics           map[string]float64             `json:"metrics"`
	Accumulated       map[string][]AccumulatedSignal `json:"accumulated,omitempty"` // fragmentID -> accumulated proxy signals
	Trace             []SignalPropagationTraceEvent  `json:"trace,omitempty"`
	TraceDropped      int                            `json:"traceDropped,omitempty"`
}

// PropagateSignal propagates a signal through the graph using axis-based profiles.
// This implements bounded propagation according to docs/notes/risk-architecture.md.
func (e *PropagationEngine) PropagateSignal(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
) (*SignalPropagationResult, error) {
	seedFragmentIDs, err := e.findSeedNodes(ctx, signal)
	if err != nil {
		return nil, err
	}
	return e.PropagateSignalFromSeeds(ctx, signal, profileLookup, seedFragmentIDs)
}

// PropagateSignalFromSeeds propagates a signal starting from explicit seed fragment IDs.
// This matches docs/notes/risk-architecture.md conceptual signature: propagate(signal, seedNodes, profile).
func (e *PropagationEngine) PropagateSignalFromSeeds(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
	seedFragmentIDs []string,
) (*SignalPropagationResult, error) {
	if signal == nil {
		return nil, fmt.Errorf("signal is nil")
	}

	if err := signal.Validate(); err != nil {
		return nil, fmt.Errorf("invalid signal: %w", err)
	}

	kernelCfg := signalKernelConfigFromContext(ctx)
	switch kernelCfg.Mode {
	case SignalKernelModeTypedRWR:
		return e.propagateSignalTypedRWR(ctx, signal, profileLookup, seedFragmentIDs, kernelCfg)
	case SignalKernelModeHybrid:
		boundedCtx := WithSignalKernelConfig(ctx, SignalKernelConfig{Mode: SignalKernelModeBounded})
		baseRes, err := e.PropagateSignalFromSeeds(boundedCtx, signal, profileLookup, seedFragmentIDs)
		if err != nil {
			return nil, err
		}
		tailCfg := kernelCfg
		tailCfg.Mode = SignalKernelModeTypedRWR
		tailCtx := WithSignalKernelConfig(ctx, tailCfg)
		tailRes, err := e.PropagateSignalFromSeeds(tailCtx, signal, profileLookup, seedFragmentIDs)
		if err != nil {
			return baseRes, nil
		}
		return mergeSignalPropagationResults(baseRes, tailRes), nil
	}

	startTime := time.Now()
	traceCollector := newSignalTraceCollector(signalTraceConfigFromContext(ctx))

	// Get propagation profile for this axis
	profile, err := profileLookup.GetForSignal(signal)
	if err != nil {
		return nil, fmt.Errorf("failed to get propagation profile: %w", err)
	}

	if len(seedFragmentIDs) == 0 {
		return &SignalPropagationResult{
			AffectedFragments: []string{},
			PropagationPaths:  [][]string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           map[string]float64{},
			Trace:             nil,
			TraceDropped:      0,
		}, nil
	}

	log.Printf("%s start signalId=%s axis=%s polarity=%s amp=%.3f seeds=%d",
		signalsys.LogPropagate,
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
		signal.Amplitude,
		len(seedFragmentIDs),
	)

	// Initialize propagation state
	visited := make(map[string]float64) // key(nodeID|axis) -> max amplitude seen
	affectedFragments := make(map[string]bool)
	propagationPaths := make([][]string, 0)
	const maxRecordedPaths = 5000 // Safety cap (increased to capture full trace, filtering happens in Engine)

	// Queue for BFS: (fragmentID, amplitude, hops, path, artifactKey)
	type queueItem struct {
		fragmentID  string
		amplitude   float64
		hops        int
		pathIDs     []string
		pathCanon   []string
		axis        string // Current axis (may transform)
		artifactKey string // Chain waveguide context (empty = free space)
	}
	type frontierState struct {
		amplitude float64
		hops      int
	}

	queue := make([]queueItem, 0, len(seedFragmentIDs))
	frontier := make(map[string]frontierState, len(seedFragmentIDs)*2)
	for _, fragID := range seedFragmentIDs {
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			ToFragment:   fragID,
			AxisOut:      signal.Axis,
			AmplitudeOut: signal.Amplitude,
			Hops:         0,
			Outcome:      "seed",
		})
		queue = append(queue, queueItem{
			fragmentID: fragID,
			amplitude:  signal.Amplitude,
			hops:       0,
			pathIDs:    []string{fragID},
			pathCanon:  []string{""}, // fill lazily when fragment is loaded
			axis:       signal.Axis,
		})
		frontier[fragID+"|"+signal.Axis] = frontierState{amplitude: signal.Amplitude, hops: 0}
	}
	var adapter DomainModelAdapter
	if len(seedFragmentIDs) > 0 {
		if seedFrag, err := e.fragmentsRepo.Get(ctx, seedFragmentIDs[0]); err == nil && seedFrag != nil {
			adapter = e.resolveDomainAdapter(ctx, seedFrag)
		}
	}
	if adapter == nil {
		adapter = e.resolveDomainAdapter(ctx, nil)
	}
	fragmentCache := make(map[string]*horizonmodels.HorizonFragment, len(seedFragmentIDs)*2)
	neighborsCache := make(map[string][]neighborInfo, len(seedFragmentIDs)*2)

	// BFS propagation
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		frontierKey := item.fragmentID + "|" + item.axis
		if best, ok := frontier[frontierKey]; ok {
			if best.amplitude > item.amplitude || (best.amplitude == item.amplitude && best.hops < item.hops) {
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					ToFragment:   item.fragmentID,
					AxisOut:      item.axis,
					AmplitudeOut: item.amplitude,
					Hops:         item.hops,
					Outcome:      "dropped",
					Reason:       "stale_frontier",
				})
				continue
			}
			delete(frontier, frontierKey)
		}

		// Threshold stop
		if item.amplitude < MinAmplitudeThreshold {
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "below_threshold",
			})
			continue
		}

		// Cycle guard: skip if already visited with equal-or-stronger amplitude
		visitKey := item.fragmentID + "|" + item.axis
		if maxAmp, ok := visited[visitKey]; ok && maxAmp >= item.amplitude {
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "visited_stronger",
			})
			continue
		}
		visited[visitKey] = item.amplitude

		// Skip distant weak signals (tail noise)
		if item.hops >= MaxHopsForWeakAmplitude && item.amplitude < WeakAmplitudeThreshold {
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "distant_weak",
			})
			continue
		}

		// Get fragment (cache-first)
		frag := fragmentCache[item.fragmentID]
		if frag == nil {
			prefetchIDs := make([]string, 0, propagationPrefetchWindow)
			prefetchIDs = append(prefetchIDs, item.fragmentID)
			for i := 0; i < len(queue) && len(prefetchIDs) < propagationPrefetchWindow; i++ {
				nextID := strings.TrimSpace(queue[i].fragmentID)
				if nextID != "" {
					prefetchIDs = append(prefetchIDs, nextID)
				}
			}
			e.prefetchFragmentsByID(ctx, fragmentCache, prefetchIDs)
			frag = fragmentCache[item.fragmentID]
		}
		if frag == nil {
			loadedFrag, err := e.fragmentsRepo.Get(ctx, item.fragmentID)
			if err != nil {
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					ToFragment:   item.fragmentID,
					AxisOut:      item.axis,
					AmplitudeOut: item.amplitude,
					Hops:         item.hops,
					Outcome:      "dropped",
					Reason:       "fragment_load_failed",
				})
				continue
			}
			frag = loadedFrag
			fragmentCache[item.fragmentID] = frag
		}

		// Fill canonical path for the last hop if missing
		if len(item.pathCanon) == len(item.pathIDs) && len(item.pathCanon) > 0 && strings.TrimSpace(item.pathCanon[len(item.pathCanon)-1]) == "" {
			item.pathCanon[len(item.pathCanon)-1] = strings.TrimSpace(frag.CanonicalPath)
		}

		// Update node metric
		metricPath := e.buildMetricPath(signal.Polarity, item.axis, signal.Mechanism)
		e.updateNodeMetric(frag, metricPath, item.amplitude, signal, item.pathCanon, item.hops)

		// Save fragment
		if err := e.fragmentsRepo.UpdateFast(ctx, frag); err == nil {
			affectedFragments[item.fragmentID] = true
			if len(propagationPaths) < maxRecordedPaths {
				propagationPaths = append(propagationPaths, append([]string{}, item.pathCanon...))
			}
			e.fireFragmentChangedHook(ctx, item.fragmentID)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "node_updated",
			})
		}

		// Hop limit: we still update the current node at max hops, but we do not expand further.
		if item.hops >= profile.DefaultMaxHops {
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "hop_limit",
			})
			continue
		}

		// Find neighbors via strong edges
		neighbors, ok := neighborsCache[item.fragmentID]
		if !ok {
			loadedNeighbors, err := e.getNeighborsViaStrongEdges(ctx, frag, profile)
			if err != nil {
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					ToFragment:   item.fragmentID,
					AxisOut:      item.axis,
					AmplitudeOut: item.amplitude,
					Hops:         item.hops,
					Outcome:      "dropped",
					Reason:       "neighbor_lookup_failed",
				})
				continue
			}
			neighbors = loadedNeighbors
			neighborsCache[item.fragmentID] = loadedNeighbors
		}

		// Chokepoint transit amplification: signal passing through a chokepoint is strengthened.
		outgoingAmplitude := item.amplitude
		if IsChokepointFragment(frag) {
			outgoingAmplitude = TransitAmplify(outgoingAmplitude, item.axis)
		}

		for _, neighbor := range neighbors {
			// Calculate damped amplitude (profile damping + strong-edge weight + edge physics)
			breakdown := e.calculateDampedAmplitudeWithBreakdown(outgoingAmplitude, item.axis, frag, neighbor.edge, profile)
			nextAmplitude := breakdown.Amplitude
			traceFactors := cloneFactorMap(breakdown.Factors)

			// Apply axis transformation (axis + factor)
			nextAxis, transformFactor := e.applyTransformation(item.axis, neighbor.edge, profile)
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"axis_transform_factor": transformFactor,
			}, "")
			if transformFactor != 0 && transformFactor != 1 {
				nextAmplitude *= transformFactor
			}
			if nextAmplitude > 1.0 {
				traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
					"amplitude_pre_clamp": nextAmplitude,
					"clamp_to_one":        1.0,
				}, "")
				nextAmplitude = 1.0
			}
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"after_axis_transform": nextAmplitude,
			}, "")
			if adapter != nil {
				beforeHook := nextAmplitude
				hookSignal := &horizonmodels.SignalDescriptor{
					Axis:      nextAxis,
					Mechanism: signal.Mechanism,
					Amplitude: nextAmplitude,
					Polarity:  signal.Polarity,
				}
				if hook, err := adapter.TransferHook(ctx, hookSignal, neighbor.edge, frag); err == nil && hook != nil {
					traceFactors = mergeFactorMaps(traceFactors, hook.DebugFactors, "adapter.")
					if hook.Skip {
						meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), frag, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
						traceCollector.add(SignalPropagationTraceEvent{
							SignalID:     signal.ID,
							FromFragment: item.fragmentID,
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
							AxisIn:       item.axis,
							AxisOut:      nextAxis,
							AmplitudeIn:  item.amplitude,
							AmplitudeOut: nextAmplitude,
							Hops:         item.hops + 1,
							Outcome:      "dropped",
							Reason:       "transfer_skip",
							Factors:      traceFactors,
						})
						continue
					}
					if hook.AdjustedAmplitude > 0 {
						if beforeHook > 0 {
							traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
								"adapter_adjusted_factor": hook.AdjustedAmplitude / beforeHook,
							}, "")
						}
						nextAmplitude = clamp01(hook.AdjustedAmplitude)
					}
					if axis := strings.TrimSpace(hook.AdjustedAxis); axis != "" && horizonmodels.IsValidAxis(axis) {
						nextAxis = axis
					}
				}
			}
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"after_adapter": nextAmplitude,
			}, "")

			// Waveguide coherence bonus: signal staying on-chain gets a small boost.
			neighborArtifactKey := EdgeArtifactKey(neighbor.edge)
			coherence := WaveguideCoherenceBonus(item.artifactKey, neighborArtifactKey)
			if coherence != 1.0 {
				nextAmplitude *= coherence
				if nextAmplitude > 1.0 {
					nextAmplitude = 1.0
				}
			}
			traceFactors["waveguide_coherence"] = coherence
			traceFactors["artifact_key"] = 0
			if neighborArtifactKey != "" {
				traceFactors["artifact_key"] = 1
			}

			meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), frag, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
			if nextAmplitude < MinAmplitudeThreshold {
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					FromFragment: item.fragmentID,
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
					AxisIn:       item.axis,
					AxisOut:      nextAxis,
					AmplitudeIn:  item.amplitude,
					AmplitudeOut: nextAmplitude,
					Hops:         item.hops + 1,
					Outcome:      "dropped",
					Reason:       "damped_below_threshold",
					Factors:      traceFactors,
				})
				continue
			}

			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				FromFragment: item.fragmentID,
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
				AxisIn:       item.axis,
				AxisOut:      nextAxis,
				AmplitudeIn:  item.amplitude,
				AmplitudeOut: nextAmplitude,
				Hops:         item.hops + 1,
				Outcome:      "propagated",
				Factors:      traceFactors,
			})

			// Add to queue — propagate artifact key for chain-direction coherence
			nextKey := neighbor.fragmentID + "|" + nextAxis
			if best, ok := frontier[nextKey]; ok {
				if best.amplitude > nextAmplitude || (best.amplitude == nextAmplitude && best.hops <= item.hops+1) {
					traceCollector.add(SignalPropagationTraceEvent{
						SignalID:     signal.ID,
						FromFragment: item.fragmentID,
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
						AxisIn:       item.axis,
						AxisOut:      nextAxis,
						AmplitudeIn:  item.amplitude,
						AmplitudeOut: nextAmplitude,
						Hops:         item.hops + 1,
						Outcome:      "dropped",
						Reason:       "frontier_weaker_duplicate",
						Factors:      traceFactors,
					})
					continue
				}
			}
			frontier[nextKey] = frontierState{amplitude: nextAmplitude, hops: item.hops + 1}
			queue = append(queue, queueItem{
				fragmentID:  neighbor.fragmentID,
				amplitude:   nextAmplitude,
				hops:        item.hops + 1,
				pathIDs:     append(append([]string{}, item.pathIDs...), neighbor.fragmentID),
				pathCanon:   append(append([]string{}, item.pathCanon...), ""), // fill lazily
				axis:        nextAxis,
				artifactKey: neighborArtifactKey,
			})
		}
	}

	// Convert to lists
	affectedList := make([]string, 0, len(affectedFragments))
	for id := range affectedFragments {
		affectedList = append(affectedList, id)
	}

	res := &SignalPropagationResult{
		AffectedFragments: affectedList,
		PropagationPaths:  propagationPaths,
		PropagationTime:   time.Since(startTime),
		Metrics: map[string]float64{
			"nodes_updated": float64(len(affectedList)),
			"paths_traced":  float64(len(propagationPaths)),
		},
	}
	res.Trace, res.TraceDropped = traceCollector.snapshot()
	if len(res.Trace) > 0 {
		res.Metrics["trace_steps"] = float64(len(res.Trace))
	}
	if res.TraceDropped > 0 {
		res.Metrics["trace_dropped"] = float64(res.TraceDropped)
	}

	log.Printf("%s done signalId=%s axis=%s polarity=%s updated=%d paths=%d trace=%d dropped=%d dur=%s",
		signalsys.LogPropagate,
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
		len(affectedList),
		len(propagationPaths),
		len(res.Trace),
		res.TraceDropped,
		res.PropagationTime.String(),
	)

	return res, nil
}

// SimulateSignalFromSeeds runs the same bounded propagation traversal as PropagateSignalFromSeeds
// but without persisting any metric updates to fragment.State.
func (e *PropagationEngine) SimulateSignalFromSeeds(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	profileLookup *ProfileLookup,
	seedFragmentIDs []string,
) (*SignalSimulationResult, error) {
	if signal == nil {
		return nil, fmt.Errorf("signal is nil")
	}
	if err := signal.Validate(); err != nil {
		return nil, fmt.Errorf("invalid signal: %w", err)
	}

	kernelCfg := signalKernelConfigFromContext(ctx)
	switch kernelCfg.Mode {
	case SignalKernelModeTypedRWR:
		return e.simulateSignalTypedRWR(ctx, signal, profileLookup, seedFragmentIDs, kernelCfg)
	case SignalKernelModeHybrid:
		boundedCtx := WithSignalKernelConfig(ctx, SignalKernelConfig{Mode: SignalKernelModeBounded})
		baseRes, err := e.SimulateSignalFromSeeds(boundedCtx, signal, profileLookup, seedFragmentIDs)
		if err != nil {
			return nil, err
		}
		tailCfg := kernelCfg
		tailCfg.Mode = SignalKernelModeTypedRWR
		tailCtx := WithSignalKernelConfig(ctx, tailCfg)
		tailRes, err := e.SimulateSignalFromSeeds(tailCtx, signal, profileLookup, seedFragmentIDs)
		if err != nil {
			return baseRes, nil
		}
		return mergeSignalSimulationResults(baseRes, tailRes), nil
	}

	startTime := time.Now()
	traceCollector := newSignalTraceCollector(signalTraceConfigFromContext(ctx))
	profile, err := profileLookup.GetForSignal(signal)
	if err != nil {
		return nil, fmt.Errorf("failed to get propagation profile: %w", err)
	}
	if len(seedFragmentIDs) == 0 {
		return &SignalSimulationResult{
			AffectedFragments: []string{},
			PropagationPaths:  [][]string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           map[string]float64{},
			Accumulated:       map[string][]AccumulatedSignal{},
			Trace:             nil,
			TraceDropped:      0,
		}, nil
	}
	log.Printf("%s preview-start signalId=%s axis=%s polarity=%s amp=%.3f seeds=%d",
		signalsys.LogPropagate,
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
		signal.Amplitude,
		len(seedFragmentIDs),
	)
	debugSignalID := strings.TrimSpace(os.Getenv("HORIZON_PROPAGATION_DEBUG_SIGNAL_ID"))
	debugEnabled := debugSignalID != "" && debugSignalID == strings.TrimSpace(signal.ID)
	debugLog := func(format string, args ...any) {
		if debugEnabled {
			log.Printf("[horizon.signal.debug] "+format, args...)
		}
	}
	if debugEnabled {
		debugLog("preview-seeds seedIds=%s", strings.Join(seedFragmentIDs, ","))
	}

	type queueItem struct {
		fragmentID  string
		amplitude   float64
		hops        int
		pathIDs     []string
		pathCanon   []string
		axis        string
		artifactKey string
	}
	type frontierState struct {
		amplitude float64
		hops      int
	}

	visited := make(map[string]float64) // key(nodeID|axis) -> max amplitude seen
	affectedFragments := make(map[string]bool)
	propagationPaths := make([][]string, 0)
	const maxRecordedPaths = 500
	acc := make(map[string]map[string]AccumulatedSignal) // fragmentID -> (axis|polarity -> signal)

	queue := make([]queueItem, 0, len(seedFragmentIDs))
	frontier := make(map[string]frontierState, len(seedFragmentIDs)*2)
	for _, fragID := range seedFragmentIDs {
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			ToFragment:   fragID,
			AxisOut:      signal.Axis,
			AmplitudeOut: signal.Amplitude,
			Hops:         0,
			Outcome:      "seed",
		})
		queue = append(queue, queueItem{
			fragmentID: fragID,
			amplitude:  signal.Amplitude,
			hops:       0,
			pathIDs:    []string{fragID},
			pathCanon:  []string{""},
			axis:       signal.Axis,
		})
		frontier[fragID+"|"+signal.Axis] = frontierState{amplitude: signal.Amplitude, hops: 0}
	}

	var adapter DomainModelAdapter
	if len(seedFragmentIDs) > 0 {
		if seedFrag, err := e.fragmentsRepo.Get(ctx, seedFragmentIDs[0]); err == nil && seedFrag != nil {
			adapter = e.resolveDomainAdapter(ctx, seedFrag)
		}
	}
	if adapter == nil {
		adapter = e.resolveDomainAdapter(ctx, nil)
	}
	fragmentCache := make(map[string]*horizonmodels.HorizonFragment, len(seedFragmentIDs)*2)
	neighborsCache := make(map[string][]neighborInfo, len(seedFragmentIDs)*2)

	track := func(fragmentID, axis, polarity string, amplitude float64, hops int) {
		fragmentID = strings.TrimSpace(fragmentID)
		axis = strings.TrimSpace(axis)
		polarity = strings.TrimSpace(polarity)
		if fragmentID == "" || axis == "" {
			return
		}
		if acc[fragmentID] == nil {
			acc[fragmentID] = make(map[string]AccumulatedSignal)
		}
		key := axis + "|" + polarity
		cur, ok := acc[fragmentID][key]
		if !ok || amplitude > cur.Amplitude {
			acc[fragmentID][key] = AccumulatedSignal{
				Axis:      axis,
				Amplitude: amplitude,
				Polarity:  polarity,
				HopCount:  hops,
			}
			return
		}
		if hops < cur.HopCount {
			cur.HopCount = hops
			acc[fragmentID][key] = cur
		}
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		frontierKey := item.fragmentID + "|" + item.axis
		if best, ok := frontier[frontierKey]; ok {
			if best.amplitude > item.amplitude || (best.amplitude == item.amplitude && best.hops < item.hops) {
				debugLog("drop reason=stale_frontier fragmentId=%s axis=%s amp=%.3f best=%.3f hops=%d bestHops=%d", item.fragmentID, item.axis, item.amplitude, best.amplitude, item.hops, best.hops)
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					ToFragment:   item.fragmentID,
					AxisOut:      item.axis,
					AmplitudeOut: item.amplitude,
					Hops:         item.hops,
					Outcome:      "dropped",
					Reason:       "stale_frontier",
				})
				continue
			}
			delete(frontier, frontierKey)
		}

		if item.amplitude < MinAmplitudeThreshold {
			debugLog("drop reason=below_threshold fragmentId=%s axis=%s amp=%.3f hops=%d", item.fragmentID, item.axis, item.amplitude, item.hops)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "below_threshold",
			})
			continue
		}

		visitKey := item.fragmentID + "|" + item.axis
		if maxAmp, ok := visited[visitKey]; ok && maxAmp >= item.amplitude {
			debugLog("drop reason=visited_stronger fragmentId=%s axis=%s amp=%.3f prev=%.3f hops=%d", item.fragmentID, item.axis, item.amplitude, maxAmp, item.hops)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "visited_stronger",
			})
			continue
		}
		visited[visitKey] = item.amplitude

		// Skip distant weak signals (tail noise)
		if item.hops >= MaxHopsForWeakAmplitude && item.amplitude < WeakAmplitudeThreshold {
			debugLog("drop reason=distant_weak fragmentId=%s axis=%s amp=%.3f hops=%d", item.fragmentID, item.axis, item.amplitude, item.hops)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "distant_weak",
			})
			continue
		}

		frag := fragmentCache[item.fragmentID]
		var err error
		if frag == nil {
			prefetchIDs := make([]string, 0, propagationPrefetchWindow)
			prefetchIDs = append(prefetchIDs, item.fragmentID)
			for i := 0; i < len(queue) && len(prefetchIDs) < propagationPrefetchWindow; i++ {
				nextID := strings.TrimSpace(queue[i].fragmentID)
				if nextID != "" {
					prefetchIDs = append(prefetchIDs, nextID)
				}
			}
			e.prefetchFragmentsByID(ctx, fragmentCache, prefetchIDs)
			frag = fragmentCache[item.fragmentID]
		}
		if frag == nil {
			frag, err = e.fragmentsRepo.Get(ctx, item.fragmentID)
		}
		if err != nil || frag == nil {
			debugLog("drop reason=fragment_load_failed fragmentId=%s axis=%s hops=%d err=%v", item.fragmentID, item.axis, item.hops, err)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "fragment_load_failed",
			})
			continue
		}
		fragmentCache[item.fragmentID] = frag
		debugLog("visit fragmentId=%s path=%s axis=%s amp=%.3f hops=%d", item.fragmentID, strings.TrimSpace(frag.CanonicalPath), item.axis, item.amplitude, item.hops)

		if len(item.pathCanon) == len(item.pathIDs) && len(item.pathCanon) > 0 && strings.TrimSpace(item.pathCanon[len(item.pathCanon)-1]) == "" {
			item.pathCanon[len(item.pathCanon)-1] = strings.TrimSpace(frag.CanonicalPath)
		}
		affectedFragments[item.fragmentID] = true
		if len(propagationPaths) < maxRecordedPaths {
			propagationPaths = append(propagationPaths, append([]string{}, item.pathCanon...))
		}
		track(item.fragmentID, item.axis, signal.Polarity, item.amplitude, item.hops)
		traceCollector.add(SignalPropagationTraceEvent{
			SignalID:     signal.ID,
			ToFragment:   item.fragmentID,
			AxisOut:      item.axis,
			AmplitudeOut: item.amplitude,
			Hops:         item.hops,
			Outcome:      "node_updated",
		})

		if item.hops >= profile.DefaultMaxHops {
			debugLog("drop reason=hop_limit fragmentId=%s path=%s axis=%s amp=%.3f hops=%d", item.fragmentID, strings.TrimSpace(frag.CanonicalPath), item.axis, item.amplitude, item.hops)
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				ToFragment:   item.fragmentID,
				AxisOut:      item.axis,
				AmplitudeOut: item.amplitude,
				Hops:         item.hops,
				Outcome:      "dropped",
				Reason:       "hop_limit",
			})
			continue
		}

		outgoingAmplitude := item.amplitude
		if IsChokepointFragment(frag) {
			outgoingAmplitude = TransitAmplify(outgoingAmplitude, item.axis)
		}

		neighbors, ok := neighborsCache[item.fragmentID]
		if !ok {
			loadedNeighbors, err := e.getNeighborsViaStrongEdges(ctx, frag, profile)
			if err != nil {
				debugLog("drop reason=neighbor_lookup_failed fragmentId=%s path=%s axis=%s hops=%d err=%v", item.fragmentID, strings.TrimSpace(frag.CanonicalPath), item.axis, item.hops, err)
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					ToFragment:   item.fragmentID,
					AxisOut:      item.axis,
					AmplitudeOut: item.amplitude,
					Hops:         item.hops,
					Outcome:      "dropped",
					Reason:       "neighbor_lookup_failed",
				})
				continue
			}
			neighbors = loadedNeighbors
			neighborsCache[item.fragmentID] = loadedNeighbors
		}
		debugLog("neighbors fragmentId=%s path=%s axis=%s count=%d", item.fragmentID, strings.TrimSpace(frag.CanonicalPath), item.axis, len(neighbors))

		for _, neighbor := range neighbors {
			breakdown := e.calculateDampedAmplitudeWithBreakdown(outgoingAmplitude, item.axis, frag, neighbor.edge, profile)
			nextAmplitude := breakdown.Amplitude
			traceFactors := cloneFactorMap(breakdown.Factors)
			nextAxis, transformFactor := e.applyTransformation(item.axis, neighbor.edge, profile)
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"axis_transform_factor": transformFactor,
			}, "")
			if transformFactor != 0 && transformFactor != 1 {
				nextAmplitude *= transformFactor
			}
			if nextAmplitude > 1.0 {
				traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
					"amplitude_pre_clamp": nextAmplitude,
					"clamp_to_one":        1.0,
				}, "")
				nextAmplitude = 1.0
			}
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"after_axis_transform": nextAmplitude,
			}, "")
			if adapter != nil {
				beforeHook := nextAmplitude
				hookSignal := &horizonmodels.SignalDescriptor{
					Axis:      nextAxis,
					Mechanism: signal.Mechanism,
					Amplitude: nextAmplitude,
					Polarity:  signal.Polarity,
				}
				if hook, err := adapter.TransferHook(ctx, hookSignal, neighbor.edge, frag); err == nil && hook != nil {
					traceFactors = mergeFactorMaps(traceFactors, hook.DebugFactors, "adapter.")
					if hook.Skip {
						debugLog("edge-drop reason=transfer_skip from=%s toFragment=%s edgeType=%s axisIn=%s axisOut=%s ampIn=%.3f ampOut=%.3f", strings.TrimSpace(frag.CanonicalPath), neighbor.fragmentID, neighbor.edge.Type, item.axis, nextAxis, item.amplitude, nextAmplitude)
						meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), frag, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
						traceCollector.add(SignalPropagationTraceEvent{
							SignalID:     signal.ID,
							FromFragment: item.fragmentID,
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
							AxisIn:       item.axis,
							AxisOut:      nextAxis,
							AmplitudeIn:  item.amplitude,
							AmplitudeOut: nextAmplitude,
							Hops:         item.hops + 1,
							Outcome:      "dropped",
							Reason:       "transfer_skip",
							Factors:      traceFactors,
						})
						continue
					}
					if hook.AdjustedAmplitude > 0 {
						if beforeHook > 0 {
							traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
								"adapter_adjusted_factor": hook.AdjustedAmplitude / beforeHook,
							}, "")
						}
						nextAmplitude = clamp01(hook.AdjustedAmplitude)
					}
					if axis := strings.TrimSpace(hook.AdjustedAxis); axis != "" && horizonmodels.IsValidAxis(axis) {
						nextAxis = axis
					}
				}
			}
			traceFactors = mergeFactorMaps(traceFactors, map[string]float64{
				"after_adapter": nextAmplitude,
			}, "")

			neighborArtifactKey := EdgeArtifactKey(neighbor.edge)
			coherence := WaveguideCoherenceBonus(item.artifactKey, neighborArtifactKey)
			if coherence != 1.0 {
				nextAmplitude *= coherence
				if nextAmplitude > 1.0 {
					nextAmplitude = 1.0
				}
			}
			traceFactors["waveguide_coherence"] = coherence

			meta := e.computeTransferTraceMeta(ctx, traceCollector.isEnabled(), frag, neighbor.fragmentID, neighbor.edge.Type, nextAxis, fragmentCache)
			if nextAmplitude < MinAmplitudeThreshold {
				debugLog("edge-drop reason=damped_below_threshold from=%s toFragment=%s edgeType=%s axisIn=%s axisOut=%s ampIn=%.3f ampOut=%.3f", strings.TrimSpace(frag.CanonicalPath), neighbor.fragmentID, neighbor.edge.Type, item.axis, nextAxis, item.amplitude, nextAmplitude)
				traceCollector.add(SignalPropagationTraceEvent{
					SignalID:     signal.ID,
					FromFragment: item.fragmentID,
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
					AxisIn:       item.axis,
					AxisOut:      nextAxis,
					AmplitudeIn:  item.amplitude,
					AmplitudeOut: nextAmplitude,
					Hops:         item.hops + 1,
					Outcome:      "dropped",
					Reason:       "damped_below_threshold",
					Factors:      traceFactors,
				})
				continue
			}
			traceCollector.add(SignalPropagationTraceEvent{
				SignalID:     signal.ID,
				FromFragment: item.fragmentID,
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
				AxisIn:       item.axis,
				AxisOut:      nextAxis,
				AmplitudeIn:  item.amplitude,
				AmplitudeOut: nextAmplitude,
				Hops:         item.hops + 1,
				Outcome:      "propagated",
				Factors:      traceFactors,
			})
			debugLog("enqueue from=%s toFragment=%s edgeType=%s axisIn=%s axisOut=%s ampIn=%.3f ampOut=%.3f nextHop=%d", strings.TrimSpace(frag.CanonicalPath), neighbor.fragmentID, neighbor.edge.Type, item.axis, nextAxis, item.amplitude, nextAmplitude, item.hops+1)
			nextKey := neighbor.fragmentID + "|" + nextAxis
			if best, ok := frontier[nextKey]; ok {
				if best.amplitude > nextAmplitude || (best.amplitude == nextAmplitude && best.hops <= item.hops+1) {
					debugLog("edge-drop reason=frontier_weaker_duplicate from=%s toFragment=%s edgeType=%s axisIn=%s axisOut=%s ampIn=%.3f ampOut=%.3f", strings.TrimSpace(frag.CanonicalPath), neighbor.fragmentID, neighbor.edge.Type, item.axis, nextAxis, item.amplitude, nextAmplitude)
					traceCollector.add(SignalPropagationTraceEvent{
						SignalID:     signal.ID,
						FromFragment: item.fragmentID,
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
						AxisIn:       item.axis,
						AxisOut:      nextAxis,
						AmplitudeIn:  item.amplitude,
						AmplitudeOut: nextAmplitude,
						Hops:         item.hops + 1,
						Outcome:      "dropped",
						Reason:       "frontier_weaker_duplicate",
						Factors:      traceFactors,
					})
					continue
				}
			}
			frontier[nextKey] = frontierState{amplitude: nextAmplitude, hops: item.hops + 1}
			queue = append(queue, queueItem{
				fragmentID:  neighbor.fragmentID,
				amplitude:   nextAmplitude,
				hops:        item.hops + 1,
				pathIDs:     append(append([]string{}, item.pathIDs...), neighbor.fragmentID),
				pathCanon:   append(append([]string{}, item.pathCanon...), ""),
				axis:        nextAxis,
				artifactKey: neighborArtifactKey,
			})
		}
	}

	affectedList := make([]string, 0, len(affectedFragments))
	for id := range affectedFragments {
		affectedList = append(affectedList, id)
	}
	accumulated := make(map[string][]AccumulatedSignal, len(acc))
	for fragmentID, m := range acc {
		if len(m) == 0 {
			continue
		}
		out := make([]AccumulatedSignal, 0, len(m))
		for _, v := range m {
			out = append(out, v)
		}
		accumulated[fragmentID] = out
	}

	res := &SignalSimulationResult{
		AffectedFragments: affectedList,
		PropagationPaths:  propagationPaths,
		PropagationTime:   time.Since(startTime),
		Metrics: map[string]float64{
			"nodes_updated": float64(len(affectedList)),
			"paths_traced":  float64(len(propagationPaths)),
		},
		Accumulated: accumulated,
	}
	res.Trace, res.TraceDropped = traceCollector.snapshot()
	if len(res.Trace) > 0 {
		res.Metrics["trace_steps"] = float64(len(res.Trace))
	}
	if res.TraceDropped > 0 {
		res.Metrics["trace_dropped"] = float64(res.TraceDropped)
	}
	log.Printf("%s preview-done signalId=%s axis=%s polarity=%s updated=%d paths=%d trace=%d dropped=%d dur=%s",
		signalsys.LogPropagate,
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
		len(affectedList),
		len(propagationPaths),
		len(res.Trace),
		res.TraceDropped,
		res.PropagationTime.String(),
	)
	return res, nil
}

// findSeedNodes finds seed nodes for signal injection.
func (e *PropagationEngine) findSeedNodes(ctx context.Context, signal *horizonmodels.SignalDescriptor) ([]string, error) {
	seedIDs := make([]string, 0)

	// Use SeedPaths if available
	if len(signal.SeedPaths) > 0 {
		for _, path := range signal.SeedPaths {
			frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err == nil && frag != nil {
				seedIDs = append(seedIDs, frag.ID)
			}
		}
		return seedIDs, nil
	}

	// Otherwise use LocationPath + edges
	if signal.LocationPath != "" {
		locationFrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, signal.LocationPath)
		if err == nil && locationFrag != nil {
			operatesInType := horizonmodels.DependencyTypeOperatesIn
			locatedInType := horizonmodels.DependencyTypeLocatedIn
			operatesInEdges, _ := e.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &operatesInType, 100)
			locatedInEdges, _ := e.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &locatedInType, 100)

			seen := make(map[string]bool)
			for _, edge := range operatesInEdges {
				if !seen[edge.SourceFragmentID] {
					seedIDs = append(seedIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
			}
			for _, edge := range locatedInEdges {
				if !seen[edge.SourceFragmentID] {
					seedIDs = append(seedIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
			}
		}
	}

	return seedIDs, nil
}

// neighborInfo represents a neighbor node and edge.
type neighborInfo struct {
	fragmentID         string
	edge               *horizonmodels.DependencyEdge
	spec               *horizonmodels.StrongEdgeSpec
	traversalDirection string // source_to_target | target_to_source
}

// getNeighborsViaStrongEdges finds neighbors connected via strong edges for the given axis.
func (e *PropagationEngine) getNeighborsViaStrongEdges(
	ctx context.Context,
	frag *horizonmodels.HorizonFragment,
	profile *horizonmodels.PropagationProfile,
) ([]neighborInfo, error) {
	if frag == nil || profile == nil {
		return nil, nil
	}
	mode := normalizePropagationMode(profile.PropagationMode)
	allowSourceToTarget := propagationModeAllowsTraversal(mode, "source_to_target")
	allowTargetToSource := propagationModeAllowsTraversal(mode, "target_to_source")

	// Build set of strong edge types -> spec
	strongSpecs := make(map[horizonmodels.DependencyType]*horizonmodels.StrongEdgeSpec)
	for _, spec := range profile.StrongEdges {
		s := spec
		strongSpecs[horizonmodels.DependencyType(spec.Type)] = &s
	}

	debugSignalID := strings.TrimSpace(os.Getenv("HORIZON_PROPAGATION_DEBUG_SIGNAL_ID"))
	debugEnabled := debugSignalID != ""
	debugLog := func(format string, args ...any) {
		if debugEnabled {
			log.Printf("[horizon.signal.debug] "+format, args...)
		}
	}
	debugLog("neighbors-start path=%s fragmentId=%s", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID))

	var outgoingEdges []*horizonmodels.DependencyEdge
	var incomingEdges []*horizonmodels.DependencyEdge
	var outgoingErr error
	var incomingErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		outgoingEdges, outgoingErr = e.edgesRepo.ListBySource(ctx, frag.TenantID, frag.ID, nil, 1000)
	}()
	go func() {
		defer wg.Done()
		incomingEdges, incomingErr = e.edgesRepo.ListByTarget(ctx, frag.TenantID, frag.ID, nil, 1000)
	}()
	wg.Wait()
	if outgoingErr != nil {
		debugLog("neighbors-outgoing-error path=%s fragmentId=%s err=%v", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), outgoingErr)
		return nil, outgoingErr
	}
	if incomingErr != nil {
		debugLog("neighbors-incoming-error path=%s fragmentId=%s err=%v", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), incomingErr)
		return nil, incomingErr
	}
	debugLog("neighbors-outgoing-ok path=%s fragmentId=%s count=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), len(outgoingEdges))
	debugLog("neighbors-incoming-ok path=%s fragmentId=%s count=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), len(incomingEdges))

	neighbors := make([]neighborInfo, 0)
	seen := make(map[string]struct{}, len(outgoingEdges)+len(incomingEdges))
	addNeighbor := func(item neighborInfo) {
		if item.edge == nil || strings.TrimSpace(item.fragmentID) == "" {
			return
		}
		key := strings.TrimSpace(item.fragmentID) + "|" + strings.TrimSpace(item.traversalDirection) + "|" + strings.TrimSpace(string(item.edge.Type))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		neighbors = append(neighbors, item)
	}

	// Process outgoing edges
	for _, edge := range outgoingEdges {
		if edge == nil || edge.IsQuarantined() || !edge.IsActiveNow() {
			continue
		}
		spec := strongSpecs[edge.Type]
		if spec == nil {
			continue
		}
		if !allowSourceToTarget {
			continue
		}
		direction := normalizeStrongEdgeDirection(spec.Direction)
		if direction == "" {
			continue
		}
		// direction controls traversal relative to edge.Source -> edge.Target
		if direction == "source_to_target" || direction == "both" {
			addNeighbor(neighborInfo{
				fragmentID:         edge.TargetFragmentID,
				edge:               edge,
				spec:               spec,
				traversalDirection: "source_to_target",
			})
		}
	}

	// Process incoming edges
	for _, edge := range incomingEdges {
		if edge == nil || edge.IsQuarantined() || !edge.IsActiveNow() {
			continue
		}
		spec := strongSpecs[edge.Type]
		if spec == nil {
			continue
		}
		if !allowTargetToSource {
			continue
		}
		direction := normalizeStrongEdgeDirection(spec.Direction)
		if direction == "" {
			continue
		}
		// direction controls traversal relative to edge.Source -> edge.Target
		if direction == "target_to_source" || direction == "both" {
			addNeighbor(neighborInfo{
				fragmentID:         edge.SourceFragmentID,
				edge:               edge,
				spec:               spec,
				traversalDirection: "target_to_source",
			})
		}
	}

	if len(neighbors) > 0 {
		// Prefer explicit strong edges whenever they already exist.
		// Virtual supply-link overlay stays as a fallback only for sparse nodes.
		debugLog("neighbors-virtual-skipped path=%s fragmentId=%s type=%s direct=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), frag.Type, len(neighbors))
		debugLog("neighbors-done path=%s fragmentId=%s total=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), len(neighbors))
		return neighbors, nil
	}

	virtualNeighbors, err := e.getSupplyLinkVirtualNeighbors(ctx, frag, strongSpecs, allowSourceToTarget, allowTargetToSource)
	if err != nil {
		debugLog("neighbors-virtual-error path=%s fragmentId=%s err=%v", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), err)
		return nil, err
	}
	debugLog("neighbors-virtual-ok path=%s fragmentId=%s count=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), len(virtualNeighbors))
	for _, neighbor := range virtualNeighbors {
		addNeighbor(neighbor)
	}
	debugLog("neighbors-done path=%s fragmentId=%s total=%d", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID), len(neighbors))

	return neighbors, nil
}

func normalizePropagationMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "upstream", "downstream", "both":
		return mode
	default:
		return "both"
	}
}

func normalizeStrongEdgeDirection(direction string) string {
	direction = strings.ToLower(strings.TrimSpace(direction))
	switch direction {
	case "source_to_target", "target_to_source", "both":
		return direction
	default:
		return ""
	}
}

func propagationModeAllowsTraversal(mode, traversalDirection string) bool {
	mode = normalizePropagationMode(mode)
	switch strings.ToLower(strings.TrimSpace(traversalDirection)) {
	case "source_to_target":
		return mode == "downstream" || mode == "both"
	case "target_to_source":
		return mode == "upstream" || mode == "both"
	default:
		return false
	}
}

func (e *PropagationEngine) prefetchFragmentsByID(
	ctx context.Context,
	cache map[string]*horizonmodels.HorizonFragment,
	ids []string,
) {
	if e == nil || e.fragmentsRepo == nil || len(ids) == 0 {
		return
	}
	missing := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if cache != nil {
			if frag := cache[id]; frag != nil {
				continue
			}
		}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return
	}
	frags, err := e.fragmentsRepo.GetMany(ctx, missing)
	if err != nil {
		return
	}
	for id, frag := range frags {
		if strings.TrimSpace(id) == "" || frag == nil {
			continue
		}
		cache[id] = frag
	}
}

func (e *PropagationEngine) calculateDampedAmplitudeWithBreakdown(
	inputAmplitude float64,
	currentAxis string,
	fromFrag *horizonmodels.HorizonFragment,
	edge *horizonmodels.DependencyEdge,
	profile *horizonmodels.PropagationProfile,
) amplitudeBreakdown {
	if edge == nil || profile == nil {
		return amplitudeBreakdown{
			Amplitude: 0,
			Factors: map[string]float64{
				"input_amplitude": inputAmplitude,
				"invalid_input":   1.0,
			},
		}
	}

	factors := map[string]float64{
		"input_amplitude": inputAmplitude,
	}

	// Base damping (docs/notes/risk-architecture.md "edge physics")
	baseDamping := profile.DefaultDamping
	effectiveDamping := baseDamping + (1-baseDamping)*edge.Properties.Criticality*0.5
	factors["profile_default_damping"] = baseDamping
	factors["effective_damping"] = effectiveDamping

	ampFactor := edge.Properties.Amplification
	if ampFactor == 0 {
		ampFactor = 1.0
	}
	factors["edge_amplification"] = ampFactor

	// Strong-edge weight
	weight := 1.0
	for _, spec := range profile.StrongEdges {
		if spec.Type == string(edge.Type) {
			weight = spec.Weight
			break
		}
	}
	factors["strong_edge_weight"] = weight

	out := inputAmplitude
	out *= effectiveDamping
	out *= weight
	out *= ampFactor
	factors["factor_damping_amp_weight"] = effectiveDamping * weight * ampFactor

	// Substitutability reduces signal (easy to bypass = weaker propagation)
	subFactor := 1.0
	if sub := edge.GetSubstitutability(); sub > 0 {
		subFactor = (1 - sub*0.5)
		out *= subFactor
	}
	factors["substitutability_factor"] = subFactor

	// Transmission semantics: the more flow concentrated on this edge and the harder
	// it is to reroute, the stronger the propagated systemic signal.
	throughputFactor := 1.0
	if share := edge.GetThroughputShare(); share > 0 {
		throughputFactor = 1 + share*0.35
		out *= throughputFactor
	}
	factors["throughput_factor"] = throughputFactor
	reroutePenaltyFactor := 1.0
	if pen := edge.GetReroutePenalty(); pen > 0 {
		reroutePenaltyFactor = 1 + pen*0.30
		out *= reroutePenaltyFactor
	}
	factors["reroute_penalty_factor"] = reroutePenaltyFactor
	capacityFactor := 1.0
	if cap := edge.GetCapacityConstraint(); cap > 0 {
		capacityFactor = 1 + cap*0.25
		out *= capacityFactor
	}
	factors["capacity_constraint_factor"] = capacityFactor
	latencyFactor := 1.0
	if deltaH := edge.GetRerouteLatencyDeltaHours(); deltaH > 0 {
		latencyNorm := math.Min(deltaH/240.0, 1.0) // saturates around +10 days
		latencyFactor = 1 + latencyNorm*0.20
		out *= latencyFactor
	}
	factors["latency_factor"] = latencyFactor
	costFactor := 1.0
	if deltaPct := edge.GetRerouteCostDeltaPct(); deltaPct > 0 {
		costNorm := math.Min(deltaPct/100.0, 1.0) // 100%+ cost increase saturates
		costFactor = 1 + costNorm*0.20
		out *= costFactor
	}
	factors["cost_factor"] = costFactor

	familyPhysics := ApplyEdgeFamilyPhysics(strings.TrimSpace(currentAxis), fromFrag, edge, DefaultEdgeFamilyPhysicsProfile())
	out *= familyPhysics.Multiplier
	factors["edge_family_multiplier"] = familyPhysics.Multiplier
	if familyPhysics.Family != "" {
		factors["edge_family_"+strings.ReplaceAll(familyPhysics.Family, "/", "_")] = 1.0
	}

	spatialFactor := spatialBindingFactor(edge, strings.TrimSpace(currentAxis))
	out *= spatialFactor
	factors["spatial_binding_factor"] = spatialFactor

	// Waveguide: edges belonging to a materialized artifact (chain) experience reduced damping.
	wgFactor := WaveguideBoostForEdge(edge, profile)
	out *= wgFactor
	factors["waveguide_factor"] = wgFactor

	factors["amplitude_after_physics_pre_clamp"] = out
	if inputAmplitude > 0 {
		factors["physics_factor_total"] = out / inputAmplitude
	}
	if out > 1.0 {
		factors["physics_clamp_to_one"] = 1.0
		out = 1.0
	}
	factors["amplitude_after_physics"] = out
	return amplitudeBreakdown{
		Amplitude: out,
		Factors:   factors,
	}
}

// applyTransformation applies axis transformation if edge matches transformation rule.
func (e *PropagationEngine) applyTransformation(
	currentAxis string,
	edge *horizonmodels.DependencyEdge,
	profile *horizonmodels.PropagationProfile,
) (string, float64) {
	for _, trans := range profile.Transformations {
		if trans.OnEdgeType == string(edge.Type) {
			return trans.ToAxis, trans.Factor
		}
	}
	return currentAxis, 1.0
}

// buildMetricPath builds the canonical metric path.
func (e *PropagationEngine) buildMetricPath(polarity, axis, mechanism string) string {
	path := "/metric/" + polarity + "/" + axis
	if mechanism != "" {
		path += "/" + mechanism
	}
	return path
}

// updateNodeMetric updates a metric on a node with bounded provenance.
func (e *PropagationEngine) updateNodeMetric(
	frag *horizonmodels.HorizonFragment,
	metricPath string,
	amplitude float64,
	signal *horizonmodels.SignalDescriptor,
	path []string,
	hops int,
) {
	if frag.State == nil {
		frag.State = make(map[string]horizonmodels.MetricValue)
	}

	// Get existing metric or create new
	existing, exists := frag.State[metricPath]
	if !exists {
		existing = horizonmodels.MetricValue{
			Value:      0.0,
			Confidence: signal.Confidence,
			ObservedAt: signal.ObservedAt,
			Provenance: horizonmodels.Provenance{
				ObservedAt: &signal.ObservedAt,
				Method:     "signal_propagation",
			},
		}
	}

	// Update value (use max of existing and new amplitude)
	if amplitude > existing.Value {
		existing.Value = amplitude
	}
	if signal.Confidence > existing.Confidence {
		existing.Confidence = signal.Confidence
	}
	existing.ObservedAt = signal.ObservedAt

	// Update provenance (bounded to top-K)
	existing.Provenance.Method = "signal_propagation"
	existing.Provenance.ObservedAt = &signal.ObservedAt
	existing.Provenance.SourceIDs = append(existing.Provenance.SourceIDs, strings.TrimSpace(signal.ID))
	if len(existing.Provenance.SourceIDs) > TopKContributions {
		existing.Provenance.SourceIDs = existing.Provenance.SourceIDs[len(existing.Provenance.SourceIDs)-TopKContributions:]
	}

	c := horizonmodels.Contribution{
		SignalID:      strings.TrimSpace(signal.ID),
		SourceEventID: strings.TrimSpace(signal.FactID),
		Path:          append([]string{}, path...),
		Amplitude:     amplitude,
		Hops:          hops,
		ObservedAt:    signal.ObservedAt,
	}
	existing.Provenance.Contributions = append(existing.Provenance.Contributions, c)
	sort.Slice(existing.Provenance.Contributions, func(i, j int) bool {
		return existing.Provenance.Contributions[i].Amplitude > existing.Provenance.Contributions[j].Amplitude
	})
	if len(existing.Provenance.Contributions) > TopKContributions {
		existing.Provenance.Contributions = existing.Provenance.Contributions[:TopKContributions]
	}

	frag.State[metricPath] = existing
}

type transferTraceMeta struct {
	FromPath  string
	ToPath    string
	FromGeo   string
	ToGeo     string
	Channel   string
	Scope     string
	Spillover string
}

func (e *PropagationEngine) computeTransferTraceMeta(
	ctx context.Context,
	traceEnabled bool,
	fromFrag *horizonmodels.HorizonFragment,
	toFragmentID string,
	edgeType horizonmodels.DependencyType,
	axis string,
	fragmentCache map[string]*horizonmodels.HorizonFragment,
) transferTraceMeta {
	if !traceEnabled {
		return transferTraceMeta{}
	}
	var toFrag *horizonmodels.HorizonFragment
	if e != nil {
		toFrag = e.getFragmentForTrace(ctx, toFragmentID, fragmentCache)
	}
	channel, scope, spillover, fromGeo, toGeo := classifySignalTransfer(axis, edgeType, fromFrag, toFrag)
	return transferTraceMeta{
		FromPath:  fragmentCanonicalPath(fromFrag),
		ToPath:    fragmentCanonicalPath(toFrag),
		FromGeo:   fromGeo,
		ToGeo:     toGeo,
		Channel:   channel,
		Scope:     scope,
		Spillover: spillover,
	}
}

func (e *PropagationEngine) getFragmentForTrace(ctx context.Context, fragmentID string, cache map[string]*horizonmodels.HorizonFragment) *horizonmodels.HorizonFragment {
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" || e == nil || e.fragmentsRepo == nil {
		return nil
	}
	if cache != nil {
		if frag := cache[fragmentID]; frag != nil {
			return frag
		}
	}
	frag, err := e.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || frag == nil {
		return nil
	}
	if cache != nil {
		cache[fragmentID] = frag
	}
	return frag
}

func spatialBindingFactor(edge *horizonmodels.DependencyEdge, axis string) float64 {
	if edge == nil || edge.Qualifiers == nil {
		return 1.0
	}
	factor := 1.0
	zoneRefs := qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierZoneRefs])
	affectedZoneRefs := qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierAffectedZoneRefs])
	spatialSemantics := qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierSpatialSemantics])
	pathStress, _ := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierPathStressScore])
	pathFragility, _ := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierPathFragility])
	pathRerouteability, _ := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierPathRerouteability])
	pathZonePressure, _ := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierPathZonePressure])
	pathAffectedPressure, _ := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierPathAffectedPressure])
	if len(zoneRefs) > 0 {
		factor *= 1 + math.Min(float64(len(zoneRefs)), 4)*0.03
		if axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency || axis == horizonmodels.AxisPhysicalFlowSupply {
			if containsAnyRef(zoneRefs, "logistics_chokepoint") {
				factor *= 1.10
			}
			if containsAnyRef(zoneRefs, "energy_hub") {
				factor *= 1.06
			}
		}
	}
	if len(affectedZoneRefs) > 0 {
		if axis == horizonmodels.AxisGeoConflict || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency || axis == horizonmodels.AxisPhysicalFlowSupply {
			factor *= 1 + math.Min(float64(len(affectedZoneRefs)), 4)*0.05
		}
	}
	if containsAnyRef(spatialSemantics, "chokepoint_exposed") && (axis == horizonmodels.AxisGeoConflict || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency) {
		factor *= 1.08
	}
	if bindingConfidence, ok := qualifierFloatValue(edge.Qualifiers[horizonmodels.QualifierBindingConfidence]); ok && bindingConfidence > 0 {
		factor *= 1 + math.Min(bindingConfidence, 1.0)*0.12
	}
	if pathStress > 0 {
		factor *= 1 + math.Min(pathStress, 1.0)*0.15
	}
	if pathFragility > 0 {
		factor *= 1 + math.Min(pathFragility, 1.0)*0.08
	}
	if pathZonePressure > 0 && (axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency || axis == horizonmodels.AxisPhysicalFlowSupply) {
		factor *= 1 + math.Min(pathZonePressure, 1.0)*0.08
	}
	if pathAffectedPressure > 0 && (axis == horizonmodels.AxisGeoConflict || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency || axis == horizonmodels.AxisPhysicalFlowSupply) {
		factor *= 1 + math.Min(pathAffectedPressure, 1.0)*0.10
	}
	if pathRerouteability > 0 {
		factor *= 1 - math.Min(pathRerouteability, 1.0)*0.05
	}
	return factor
}

func qualifierStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		return []string{s}
	default:
		return nil
	}
}

func containsAnyRef(items []string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	for _, item := range items {
		if strings.Contains(strings.ToLower(strings.TrimSpace(item)), token) {
			return true
		}
	}
	return false
}

func qualifierFloatValue(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
