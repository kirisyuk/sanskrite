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
)

// SignalPropagationResult represents the result of signal propagation.
type SignalPropagationResult struct {
	AffectedFragments []string           `json:"affectedFragments"`
	PropagationPaths  [][]string         `json:"propagationPaths"`
	PropagationTime   time.Duration      `json:"propagationTime"`
	Metrics           map[string]float64 `json:"metrics"`
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
		queue = append(queue, queueItem{
			fragmentID: fragID,
			amplitude:  signal.Amplitude,
			hops:       0,
			pathIDs:    []string{fragID},
			pathCanon:  []string{""}, // fill lazily when fragment is loaded
			axis:       signal.Axis,
		})
	}

	// BFS propagation
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		// Threshold stop
		if item.amplitude < MinAmplitudeThreshold {
			continue
		}

		// Cycle guard: skip if already visited with equal-or-stronger amplitude
		visitKey := item.fragmentID + "|" + item.axis
		if maxAmp, ok := visited[visitKey]; ok && maxAmp >= item.amplitude {
			continue
		}
		visited[visitKey] = item.amplitude

		// Get fragment
		frag, err := e.fragmentsRepo.Get(ctx, item.fragmentID)
		if err != nil {
			continue
		}

		// Fill canonical path for the last hop if missing
		if len(item.pathCanon) == len(item.pathIDs) && len(item.pathCanon) > 0 && strings.TrimSpace(item.pathCanon[len(item.pathCanon)-1]) == "" {
			item.pathCanon[len(item.pathCanon)-1] = strings.TrimSpace(frag.CanonicalPath)
		}

		// Update node metric
		metricPath := e.buildMetricPath(signal.Polarity, item.axis, signal.Mechanism)
		e.updateNodeMetric(frag, metricPath, item.amplitude, signal, item.pathCanon, item.hops)

		// Save fragment
		if err := e.fragmentsRepo.Update(ctx, frag); err == nil {
			affectedFragments[item.fragmentID] = true
			propagationPaths = append(propagationPaths, append([]string{}, item.pathCanon...))
			e.fireFragmentChangedHook(ctx, item.fragmentID)
		}

		// Hop limit: we still update the current node at max hops, but we do not expand further.
		if item.hops >= profile.DefaultMaxHops {
			continue
		}

		// Find neighbors via strong edges
		neighbors, err := e.getNeighborsViaStrongEdges(ctx, frag, profile)
		if err != nil {
			continue
		}

		for _, neighbor := range neighbors {
			// Calculate damped amplitude (profile damping + strong-edge weight + edge physics)
			nextAmplitude := e.calculateDampedAmplitude(item.amplitude, neighbor.edge, profile)

			// Apply axis transformation (axis + factor)
			nextAxis, factor := e.applyTransformation(item.axis, neighbor.edge, profile)
			if factor != 0 && factor != 1 {
				nextAmplitude *= factor
			}
			if nextAmplitude > 1.0 {
				nextAmplitude = 1.0
			}

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

	log.Printf("%s done signalId=%s axis=%s polarity=%s updated=%d paths=%d dur=%s",
		signalsys.LogPropagate,
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
		len(affectedList),
		len(propagationPaths),
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
	fragmentID string
	edge       *horizonmodels.DependencyEdge
	spec       *horizonmodels.StrongEdgeSpec
}

// getNeighborsViaStrongEdges finds neighbors connected via strong edges for the given axis.
func (e *PropagationEngine) getNeighborsViaStrongEdges(
	ctx context.Context,
	frag *horizonmodels.HorizonFragment,
	profile *horizonmodels.PropagationProfile,
) ([]neighborInfo, error) {
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
		spec := strongSpecs[edge.Type]
		if spec == nil {
			continue
		}
		// direction controls traversal relative to edge.Source -> edge.Target
		if spec.Direction == "source_to_target" || spec.Direction == "both" {
			neighbors = append(neighbors, neighborInfo{
				fragmentID: edge.TargetFragmentID,
				edge:       edge,
				spec:       spec,
			})
		}
	}

	// Process incoming edges
	for _, edge := range incomingEdges {
		spec := strongSpecs[edge.Type]
		if spec == nil {
			continue
		}
		// direction controls traversal relative to edge.Source -> edge.Target
		if spec.Direction == "target_to_source" || spec.Direction == "both" {
			neighbors = append(neighbors, neighborInfo{
				fragmentID: edge.SourceFragmentID,
				edge:       edge,
				spec:       spec,
			})
		}
	}

	return neighbors, nil
}

// calculateDampedAmplitude calculates the amplitude after passing through an edge.
func (e *PropagationEngine) calculateDampedAmplitude(
	inputAmplitude float64,
	edge *horizonmodels.DependencyEdge,
	profile *horizonmodels.PropagationProfile,
) float64 {
	// Base damping (risk-architecture.md "edge physics")
	baseDamping := profile.DefaultDamping
	effectiveDamping := baseDamping + (1-baseDamping)*edge.Properties.Criticality*0.5

	ampFactor := edge.Properties.Amplification
	if ampFactor == 0 {
		ampFactor = 1.0
	}

	// Strong-edge weight
	weight := 1.0
	for _, spec := range profile.StrongEdges {
		if spec.Type == string(edge.Type) {
			weight = spec.Weight
			break
		}
	}

	out := inputAmplitude
	out *= effectiveDamping
	out *= weight
	out *= ampFactor

	// Substitutability reduces signal (easy to bypass = weaker propagation)
	if sub := edge.GetSubstitutability(); sub > 0 {
		out *= (1 - sub*0.5)
	}

	// Transmission semantics: the more flow concentrated on this edge and the harder
	// it is to reroute, the stronger the propagated systemic signal.
	if share := edge.GetThroughputShare(); share > 0 {
		out *= 1 + share*0.35
	}
	if pen := edge.GetReroutePenalty(); pen > 0 {
		out *= 1 + pen*0.30
	}
	if cap := edge.GetCapacityConstraint(); cap > 0 {
		out *= 1 + cap*0.25
	}
	if deltaH := edge.GetRerouteLatencyDeltaHours(); deltaH > 0 {
		latencyFactor := math.Min(deltaH/240.0, 1.0) // saturates around +10 days
		out *= 1 + latencyFactor*0.20
	}
	if deltaPct := edge.GetRerouteCostDeltaPct(); deltaPct > 0 {
		costFactor := math.Min(deltaPct/100.0, 1.0) // 100%+ cost increase saturates
		out *= 1 + costFactor*0.20
	}

	return math.Min(out, 1.0)
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
