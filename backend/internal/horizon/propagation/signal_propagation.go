package propagation

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
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
// This implements bounded propagation according to risk-architecture.md.
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
// This matches risk-architecture.md conceptual signature: propagate(signal, seedNodes, profile).
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

	// Queue for BFS: (fragmentID, amplitude, hops, path)
	type queueItem struct {
		fragmentID string
		amplitude  float64
		hops       int
		pathIDs    []string
		pathCanon  []string
		axis       string // Current axis (may transform)
	}

	queue := make([]queueItem, 0, len(seedFragmentIDs))
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
			propagationPaths = append(propagationPaths, append([]string{}, item.pathCanon...))
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

		for _, neighbor := range neighbors {
			// Calculate damped amplitude (profile damping + strong-edge weight + edge physics)
			breakdown := e.calculateDampedAmplitudeWithBreakdown(item.amplitude, neighbor.edge, profile)
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

			// Add to queue
			queue = append(queue, queueItem{
				fragmentID: neighbor.fragmentID,
				amplitude:  nextAmplitude,
				hops:       item.hops + 1,
				pathIDs:    append(append([]string{}, item.pathIDs...), neighbor.fragmentID),
				pathCanon:  append(append([]string{}, item.pathCanon...), ""), // fill lazily
				axis:       nextAxis,
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

	type queueItem struct {
		fragmentID string
		amplitude  float64
		hops       int
		pathIDs    []string
		pathCanon  []string
		axis       string
	}

	visited := make(map[string]float64) // key(nodeID|axis) -> max amplitude seen
	affectedFragments := make(map[string]bool)
	propagationPaths := make([][]string, 0)
	acc := make(map[string]map[string]AccumulatedSignal) // fragmentID -> (axis|polarity -> signal)

	queue := make([]queueItem, 0, len(seedFragmentIDs))
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

		frag, err := e.fragmentsRepo.Get(ctx, item.fragmentID)
		if err != nil || frag == nil {
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

		if len(item.pathCanon) == len(item.pathIDs) && len(item.pathCanon) > 0 && strings.TrimSpace(item.pathCanon[len(item.pathCanon)-1]) == "" {
			item.pathCanon[len(item.pathCanon)-1] = strings.TrimSpace(frag.CanonicalPath)
		}
		affectedFragments[item.fragmentID] = true
		propagationPaths = append(propagationPaths, append([]string{}, item.pathCanon...))
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

		neighbors, err := e.getNeighborsViaStrongEdges(ctx, frag, profile)
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
		for _, neighbor := range neighbors {
			breakdown := e.calculateDampedAmplitudeWithBreakdown(item.amplitude, neighbor.edge, profile)
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
			queue = append(queue, queueItem{
				fragmentID: neighbor.fragmentID,
				amplitude:  nextAmplitude,
				hops:       item.hops + 1,
				pathIDs:    append(append([]string{}, item.pathIDs...), neighbor.fragmentID),
				pathCanon:  append(append([]string{}, item.pathCanon...), ""),
				axis:       nextAxis,
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

	// Get all outgoing edges
	outgoingEdges, err := e.edgesRepo.ListBySource(ctx, frag.TenantID, frag.ID, nil, 1000)
	if err != nil {
		return nil, err
	}

	// Get all incoming edges
	incomingEdges, err := e.edgesRepo.ListByTarget(ctx, frag.TenantID, frag.ID, nil, 1000)
	if err != nil {
		return nil, err
	}

	neighbors := make([]neighborInfo, 0)

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
			neighbors = append(neighbors, neighborInfo{
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
			neighbors = append(neighbors, neighborInfo{
				fragmentID:         edge.SourceFragmentID,
				edge:               edge,
				spec:               spec,
				traversalDirection: "target_to_source",
			})
		}
	}

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

func (e *PropagationEngine) calculateDampedAmplitudeWithBreakdown(
	inputAmplitude float64,
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

	// Base damping (risk-architecture.md "edge physics")
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
