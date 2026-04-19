package qa

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Engine runs the Q&A pipeline: Decompose → Simulate → Synthesize.
type Engine struct {
	decomposer        *QuestionDecomposer
	seedResolver      *SeedResolver
	leafEvidence      *LeafEvidenceCollector
	propagationEngine *propagation.PropagationEngine
	profileLookup     *propagation.ProfileLookup
	fragmentsRepo     *horizonrepo.HorizonFragmentsRepository
	artifactsRepo     *horizonrepo.MaterializedArtifactsRepository
}

// NewEngine creates a new QA Engine.
func NewEngine(
	decomposer *QuestionDecomposer,
	seedResolver *SeedResolver,
	leafEvidence *LeafEvidenceCollector,
	propagationEngine *propagation.PropagationEngine,
	profileLookup *propagation.ProfileLookup,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	artifactsRepo *horizonrepo.MaterializedArtifactsRepository,
) *Engine {
	return &Engine{
		decomposer:        decomposer,
		seedResolver:      seedResolver,
		leafEvidence:      leafEvidence,
		propagationEngine: propagationEngine,
		profileLookup:     profileLookup,
		fragmentsRepo:     fragmentsRepo,
		artifactsRepo:     artifactsRepo,
	}
}

// Run executes the full Q&A pipeline and returns a SimulationResult.
// Caller is responsible for AnswerContextBuilder and AnswerSynthesizer (Phase 3).
func (e *Engine) Run(ctx context.Context, req *QARequest) (*horizonmodels.SimulationResult, *horizonmodels.QuestionPlan, error) {
	if req == nil || strings.TrimSpace(req.Question) == "" {
		return nil, nil, fmt.Errorf("question is required")
	}
	if req.Lens == nil {
		return nil, nil, fmt.Errorf("lens is required")
	}

	// Phase 1: Decompose
	lensCtx := &DecomposeContext{
		ScopePaths:        req.Lens.Scope[:minInt(30, len(req.Lens.Scope))],
		DomainModes:       req.Lens.DomainModes,
		ActiveSignals:     req.ActiveSignals,
		ArtifactSummaries: req.ArtifactSummaries,
	}
	plan, err := e.Decompose(ctx, req.Question, lensCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("decompose: %w", err)
	}
	if plan.AnswerMode == "out_of_scope" {
		return &horizonmodels.SimulationResult{
			NodeImpacts:  nil,
			TracePaths:   nil,
			LeafEvidence: nil,
			ChainImpacts: nil,
		}, plan, nil
	}

	// Resolve scope fragments
	scopeFragments := req.Lens.ScopeFragments
	if scopeFragments == nil {
		scopeFragments = make(map[string]string)
	}

	// Phase 2: Simulate
	simResult, err := e.Simulate(ctx, plan, scopeFragments, req.Fragments)
	if err != nil {
		return nil, plan, fmt.Errorf("simulate: %w", err)
	}

	// Collect leaf evidence
	affectedIDs := make(map[string]bool)
	for _, ni := range simResult.NodeImpacts {
		affectedIDs[ni.FragmentID] = true
	}
	leafEv := e.CollectLeafEvidence(ctx, req.Fragments, affectedIDs)
	simResult.LeafEvidence = append(simResult.LeafEvidence, leafEv...)

	return simResult, plan, nil
}

// QARequest is the input for the Q&A pipeline.
type QARequest struct {
	Question          string
	Lens              *horizonmodels.Lens
	Fragments         []*horizonmodels.HorizonFragment
	ActiveSignals     []string
	ArtifactSummaries []string
}

// Decompose wraps the decomposer call: question → QuestionPlan.
func (e *Engine) Decompose(ctx context.Context, question string, lensCtx *DecomposeContext) (*horizonmodels.QuestionPlan, error) {
	return e.decomposer.Decompose(ctx, question, lensCtx)
}

// Simulate runs either simulateHypothetical or collectCurrentState based on plan.
func (e *Engine) Simulate(ctx context.Context, plan *horizonmodels.QuestionPlan, scopeFragments map[string]string, fragments []*horizonmodels.HorizonFragment) (*horizonmodels.SimulationResult, error) {
	if len(plan.HypotheticalSignals) > 0 {
		return e.simulateHypothetical(ctx, plan, scopeFragments, fragments)
	}
	return e.collectCurrentState(ctx, plan, scopeFragments, fragments)
}

// CollectLeafEvidence wraps leafEvidence.Collect.
func (e *Engine) CollectLeafEvidence(ctx context.Context, fragments []*horizonmodels.HorizonFragment, affectedIDs map[string]bool) []horizonmodels.LeafSignalEvidence {
	return e.leafEvidence.Collect(ctx, fragments, affectedIDs)
}

// ComputeChainImpacts computes path-aware impact summaries on materialized chains.
func ComputeChainImpacts(artifacts []*horizonmodels.MaterializedArtifact, nodeImpacts []horizonmodels.NodeImpact) []horizonmodels.ChainImpact {
	if len(artifacts) == 0 || len(nodeImpacts) == 0 {
		return nil
	}
	affectedPaths := make(map[string]float64)
	for _, ni := range nodeImpacts {
		cp := strings.TrimSpace(ni.CanonicalPath)
		if cp != "" {
			if ni.TotalAmplitude > affectedPaths[cp] {
				affectedPaths[cp] = ni.TotalAmplitude
			}
		}
	}
	var out []horizonmodels.ChainImpact
	for _, a := range artifacts {
		if a == nil || len(a.NodeRefs) == 0 {
			continue
		}
		var maxAmp float64
		affected := 0
		for _, ref := range a.NodeRefs {
			ref = strings.TrimSpace(ref)
			if amp, ok := affectedPaths[ref]; ok {
				affected++
				if amp > maxAmp {
					maxAmp = amp
				}
			}
		}
		if affected == 0 {
			continue
		}
		name := strings.TrimSpace(a.CanonicalKey)
		if a.Labels != nil {
			if dn, ok := a.Labels["displayName"]; ok && strings.TrimSpace(dn) != "" {
				name = strings.TrimSpace(dn)
			}
		}
		impact := horizonmodels.ChainImpact{
			ArtifactID:    a.ID,
			DisplayName:   name,
			Kind:          string(a.Kind),
			MaxAmplitude:  maxAmp,
			AffectedNodes: affected,
			TotalNodes:    len(a.NodeRefs),
		}
		if a.Kind == horizonmodels.MaterializedKindSupplyChainPath {
			if parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload); err == nil && parsed != nil {
				impact.LegCount = len(parsed.Legs)
				if parsed.Stress != nil {
					impact.PathStressScore = parsed.Stress.PathScore
					impact.BottleneckLegIndex = parsed.Stress.BottleneckLegIndex
					impact.RecoveryLatencyHours = parsed.Stress.RecoveryLatencyHrs
					impact.DominantFactors = append([]string(nil), parsed.Stress.DominantFactors...)
				}
				flowRefs := make(map[string]struct{})
				zoneRefs := make(map[string]struct{})
				affectedZoneRefs := make(map[string]struct{})
				for _, leg := range parsed.Legs {
					legAffected := false
					for _, ref := range chainLegRefs(leg) {
						if _, ok := affectedPaths[ref]; ok {
							legAffected = true
							break
						}
					}
					if legAffected {
						impact.AffectedLegs++
					}
					for _, ref := range leg.FlowRefs {
						if ref = strings.TrimSpace(ref); ref != "" {
							flowRefs[ref] = struct{}{}
						}
					}
					for _, ref := range leg.ZoneRefs {
						if ref = strings.TrimSpace(ref); ref != "" {
							zoneRefs[ref] = struct{}{}
						}
					}
					for _, ref := range leg.AffectedZoneRefs {
						if ref = strings.TrimSpace(ref); ref != "" {
							affectedZoneRefs[ref] = struct{}{}
						}
					}
				}
				impact.FlowRefs = sortedRefKeys(flowRefs)
				impact.ZoneRefs = sortedRefKeys(zoneRefs)
				impact.AffectedZoneRefs = sortedRefKeys(affectedZoneRefs)
				impact.BottleneckSummary = summarizeChainBottleneck(parsed)
			}
		}
		out = append(out, impact)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PathStressScore == out[j].PathStressScore {
			return out[i].MaxAmplitude > out[j].MaxAmplitude
		}
		return out[i].PathStressScore > out[j].PathStressScore
	})
	return out
}

func chainLegRefs(leg horizonmodels.SupplyChainPathLeg) []string {
	refs := make([]string, 0, 3+len(leg.TransportViaRefs))
	if ref := strings.TrimSpace(leg.SupplierRef); ref != "" {
		refs = append(refs, ref)
	}
	if ref := strings.TrimSpace(leg.CustomerRef); ref != "" {
		refs = append(refs, ref)
	}
	if ref := strings.TrimSpace(leg.ProductRef); ref != "" {
		refs = append(refs, ref)
	}
	for _, ref := range leg.TransportViaRefs {
		if ref = strings.TrimSpace(ref); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func sortedRefKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func summarizeChainBottleneck(parsed *horizonmodels.SupplyChainPathPayload) string {
	if parsed == nil || parsed.Stress == nil || len(parsed.Legs) == 0 {
		return ""
	}
	idx := parsed.Stress.BottleneckLegIndex
	if idx < 0 || idx >= len(parsed.Legs) {
		return ""
	}
	leg := parsed.Legs[idx]
	parts := make([]string, 0, 3)
	if supplier := strings.TrimSpace(leg.SupplierRef); supplier != "" {
		parts = append(parts, supplier)
	}
	if len(leg.TransportViaRefs) > 0 {
		parts = append(parts, strings.Join(trimmedRefs(leg.TransportViaRefs, 2), " -> "))
	}
	if customer := strings.TrimSpace(leg.CustomerRef); customer != "" {
		parts = append(parts, customer)
	}
	summary := strings.Join(parts, " -> ")
	if summary == "" {
		summary = "Leg " + strconv.Itoa(idx+1)
	}
	if parsed.Stress.PathScore > 0 {
		summary += fmt.Sprintf(" (stress %.2f)", parsed.Stress.PathScore)
	}
	return summary
}

func trimmedRefs(refs []string, max int) []string {
	if max <= 0 {
		return nil
	}
	out := make([]string, 0, max)
	for _, ref := range refs {
		if ref = strings.TrimSpace(ref); ref != "" {
			out = append(out, ref)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

// ApplyTargetFilter applies ObservePathPatterns filter to a SimulationResult.
func ApplyTargetFilter(_ context.Context, result *horizonmodels.SimulationResult, plan *horizonmodels.QuestionPlan) *horizonmodels.SimulationResult {
	if result == nil || plan == nil || len(plan.TargetFilter.ObservePathPatterns) == 0 {
		return result
	}
	patterns := plan.TargetFilter.ObservePathPatterns
	var filtered []horizonmodels.NodeImpact
	for _, ni := range result.NodeImpacts {
		if matchesAnyPattern(ni.CanonicalPath, patterns) {
			filtered = append(filtered, ni)
		}
	}
	result.NodeImpacts = filtered
	return result
}

func matchesAnyPattern(path string, patterns []string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(p, "*")) {
				return true
			}
		} else if path == p {
			return true
		}
	}
	return false
}

func (e *Engine) simulateHypothetical(
	ctx context.Context,
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) (*horizonmodels.SimulationResult, error) {
	fragByID := make(map[string]*horizonmodels.HorizonFragment)
	for _, f := range fragments {
		if f != nil && f.ID != "" {
			fragByID[f.ID] = f
		}
	}

	var allNodeImpacts []horizonmodels.NodeImpact
	allTracePaths := make([]horizonmodels.TracePath, 0)
	affectedSet := make(map[string]bool)

	idToPath := make(map[string]string)
	for path, id := range scopeFragments {
		if path != "" && id != "" {
			idToPath[id] = path
		}
	}
	for _, f := range fragments {
		if f != nil && f.ID != "" && f.CanonicalPath != "" {
			idToPath[f.ID] = f.CanonicalPath
		}
	}

	var allSeedFailures []horizonmodels.SeedFailure
	for _, hyp := range plan.HypotheticalSignals {
		resolution := e.seedResolver.ResolveWithFailures(hyp.SeedHints, scopeFragments)
		allSeedFailures = append(allSeedFailures, resolution.Failures...)
		seedIDs := resolution.ResolvedIDs
		if len(seedIDs) == 0 {
			continue
		}

		seedPaths := make([]string, 0, len(seedIDs))
		for _, id := range seedIDs {
			if p := idToPath[id]; p != "" {
				seedPaths = append(seedPaths, p)
			}
		}
		if len(seedPaths) == 0 {
			seedPaths = append(seedPaths, hyp.SeedHints...)
		}

		sig := &horizonmodels.SignalDescriptor{
			ID:        "qa_" + uuid.New().String()[:8],
			Axis:      hyp.Axis,
			Mechanism: hyp.Mechanism,
			Amplitude: hyp.Amplitude,
			Polarity:  hyp.Polarity,
			SeedPaths: seedPaths,
			Constraints: horizonmodels.SignalConstraints{
				TimeHorizon: hyp.TimeHorizon,
				Modality:    hyp.Modality,
			},
		}
		if sig.Polarity == "" {
			sig.Polarity = "risk"
		}

		sim, err := e.propagationEngine.SimulateSignalFromSeeds(ctx, sig, e.profileLookup, seedIDs)
		if err != nil {
			return nil, err
		}

		fragIDToCanon := make(map[string]string)
		for _, f := range fragments {
			if f != nil && f.ID != "" && f.CanonicalPath != "" {
				fragIDToCanon[f.ID] = f.CanonicalPath
			}
		}

		fragPathFromSeed := make(map[string][]string)
		for _, path := range sim.PropagationPaths {
			if len(path) < 2 {
				continue
			}
			lastID := path[len(path)-1]
			if _, ok := fragPathFromSeed[lastID]; !ok {
				canonPaths := make([]string, 0, len(path))
				for _, fid := range path {
					if cp := fragIDToCanon[fid]; cp != "" {
						canonPaths = append(canonPaths, cp)
					}
				}
				if len(canonPaths) > 0 {
					fragPathFromSeed[lastID] = canonPaths
				}
			}
		}

		for fragID, accList := range sim.Accumulated {
			var total float64
			minHops := -1
			axisAmps := make(map[string]float64)
			for _, a := range accList {
				if a.Amplitude > 0.05 {
					axisAmps[a.Axis] = a.Amplitude
					if a.Amplitude > total {
						total = a.Amplitude
					}
					if minHops < 0 || a.HopCount < minHops {
						minHops = a.HopCount
					}
				}
			}
			if total < 0.05 {
				continue
			}
			if minHops < 0 {
				minHops = 0
			}
			affectedSet[fragID] = true
			frag := fragByID[fragID]
			name := fragID
			canonPath := ""
			typ := ""
			if frag != nil {
				canonPath = frag.CanonicalPath
				if frag.Labels != nil {
					if en, ok := frag.Labels["en"]; ok {
						name = en
					}
				}
				typ = string(frag.Class)
			}
			allNodeImpacts = append(allNodeImpacts, horizonmodels.NodeImpact{
				FragmentID:     fragID,
				CanonicalPath:  canonPath,
				Name:           name,
				Type:           typ,
				AxisAmplitudes: axisAmps,
				TotalAmplitude: total,
				Hops:           minHops,
				PathFromSeed:   fragPathFromSeed[fragID],
			})
		}

		for _, path := range sim.PropagationPaths {
			cleanPaths := make([]string, 0, len(path))
			for _, p := range path {
				if p = strings.TrimSpace(p); p != "" {
					cleanPaths = append(cleanPaths, p)
				}
			}
			if len(cleanPaths) == 0 {
				continue
			}
			amps := make([]float64, len(cleanPaths))
			// Only last node matters for sorting by impact
			var lastAmp float64
			var lastFragID string

			for i, canonPath := range cleanPaths {
				fragID := scopeFragments[canonPath]
				if fragID == "" {
					for _, f := range fragments {
						if f != nil && f.CanonicalPath == canonPath {
							fragID = f.ID
							break
						}
					}
				}
				if fragID != "" {
					if arr, ok := sim.Accumulated[fragID]; ok {
						for _, a := range arr {
							if a.Amplitude > amps[i] {
								amps[i] = a.Amplitude
							}
						}
					}
					if i == len(cleanPaths)-1 {
						lastAmp = amps[i]
						lastFragID = fragID
					}
				}
			}

			// Only include paths that end at a significant node (one of the top impacted nodes)
			// AND have non-trivial amplitude at the end.
			if lastAmp < 0.05 {
				continue
			}
			isImpactedNode := false
			for _, ni := range allNodeImpacts {
				if ni.FragmentID == lastFragID {
					isImpactedNode = true
					break
				}
			}
			if !isImpactedNode {
				continue
			}

			allTracePaths = append(allTracePaths, horizonmodels.TracePath{
				NodeCanonicalPaths: cleanPaths,
				Axis:               hyp.Axis,
				Amplitudes:         amps,
			})
		}
	}

	sort.Slice(allNodeImpacts, func(i, j int) bool {
		return allNodeImpacts[i].TotalAmplitude > allNodeImpacts[j].TotalAmplitude
	})

	if len(allNodeImpacts) > 20 {
		allNodeImpacts = allNodeImpacts[:20]
	}

	// Sort trace paths by final amplitude and cap at 50 to avoid UI flood
	sort.Slice(allTracePaths, func(i, j int) bool {
		endI := 0.0
		if len(allTracePaths[i].Amplitudes) > 0 {
			endI = allTracePaths[i].Amplitudes[len(allTracePaths[i].Amplitudes)-1]
		}
		endJ := 0.0
		if len(allTracePaths[j].Amplitudes) > 0 {
			endJ = allTracePaths[j].Amplitudes[len(allTracePaths[j].Amplitudes)-1]
		}
		return endI > endJ
	})
	if len(allTracePaths) > 50 {
		allTracePaths = allTracePaths[:50]
	}

	_ = affectedSet

	return &horizonmodels.SimulationResult{
		NodeImpacts:        allNodeImpacts,
		TracePaths:         allTracePaths,
		LeafEvidence:       nil,
		ChainImpacts:       nil,
		SeedFailures:       allSeedFailures,
		HighlightedEdgeIDs: ExtractEdgeIDsFromTracePaths(allTracePaths, scopeFragments),
	}, nil
}

func (e *Engine) collectCurrentState(
	_ context.Context,
	plan *horizonmodels.QuestionPlan,
	_ map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) (*horizonmodels.SimulationResult, error) {
	targetAxes := plan.TargetFilter.ObserveAxes
	if len(targetAxes) == 0 {
		targetAxes = []string{"physical_flow/supply", "legal/access", "geo/conflict"}
	}
	maxNodes := plan.TargetFilter.MaxNodes
	if maxNodes <= 0 {
		maxNodes = 20
	}

	var impacts []horizonmodels.NodeImpact
	tracePaths := make([]horizonmodels.TracePath, 0)
	tracePathSeen := make(map[string]bool)

	for _, frag := range fragments {
		if frag == nil || frag.State == nil {
			continue
		}
		var total float64
		axisAmps := make(map[string]float64)
		for metricPath, mv := range frag.State {
			if mv.Value <= 0 {
				continue
			}
			axis := extractAxisFromMetricPath(metricPath)
			if axis == "" {
				continue
			}
			matches := false
			for _, a := range targetAxes {
				if axis == a || strings.HasPrefix(axis, a+"/") {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			if mv.Value > axisAmps[axis] {
				axisAmps[axis] = mv.Value
			}
			if mv.Value > total {
				total = mv.Value
			}

			for _, contrib := range mv.Provenance.Contributions {
				if len(contrib.Path) < 2 {
					continue
				}
				key := strings.Join(contrib.Path, "->")
				if tracePathSeen[key] {
					continue
				}
				tracePathSeen[key] = true
				amps := make([]float64, len(contrib.Path))
				amps[len(amps)-1] = contrib.Amplitude
				tracePaths = append(tracePaths, horizonmodels.TracePath{
					NodeCanonicalPaths: contrib.Path,
					Axis:               axis,
					Amplitudes:         amps,
				})
			}
		}
		if total <= 0 {
			continue
		}
		name := frag.CanonicalPath
		if frag.Labels != nil {
			if en, ok := frag.Labels["en"]; ok {
				name = en
			}
		}
		impacts = append(impacts, horizonmodels.NodeImpact{
			FragmentID:     frag.ID,
			CanonicalPath:  frag.CanonicalPath,
			Name:           name,
			Type:           string(frag.Class),
			AxisAmplitudes: axisAmps,
			TotalAmplitude: total,
		})
	}
	sort.Slice(impacts, func(i, j int) bool {
		return impacts[i].TotalAmplitude > impacts[j].TotalAmplitude
	})
	if len(impacts) > maxNodes {
		impacts = impacts[:maxNodes]
	}
	return &horizonmodels.SimulationResult{
		NodeImpacts:  impacts,
		TracePaths:   tracePaths,
		LeafEvidence: nil,
		ChainImpacts: nil,
	}, nil
}

func extractAxisFromMetricPath(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /metric/{polarity}/{axis_part1}/{axis_part2}/{mechanism}
	if len(parts) < 4 {
		if len(parts) == 3 {
			// Backward-compatible mapping for legacy risk metrics:
			// /metric/risk/<metric_name> -> 12-axis bucket.
			if parts[0] == "metric" {
				if axis, ok := legacyRiskMetricAxisMap[parts[2]]; ok {
					return axis
				}
			}
			return parts[2]
		}
		return ""
	}
	return parts[2] + "/" + parts[3]
}

var legacyRiskMetricAxisMap = map[string]string{
	"supply_chain_criticality": "physical_flow/supply",
	"capacity_utilization":     "operational/continuity",
	"operational_health":       "operational/continuity",
	"sanction_exposure":        "legal/compliance",
	"geopolitical_alignment":   "geo/conflict",
	"stability_index":          "info/uncertainty",
	"corruption_vulnerability": "info/uncertainty",
	"market_sentiment":         "info/reputation",
	"public_perception":        "info/reputation",
}

// ExtractEdgeIDsFromTracePaths builds consecutive-pair edge keys from trace paths.
// The edge keys are "source->target" canonical path pairs that can be matched against edge IDs.
func ExtractEdgeIDsFromTracePaths(tracePaths []horizonmodels.TracePath, scopeFragments map[string]string) []string {
	if len(tracePaths) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, tp := range tracePaths {
		for i := 0; i < len(tp.NodeCanonicalPaths)-1; i++ {
			from := strings.TrimSpace(tp.NodeCanonicalPaths[i])
			to := strings.TrimSpace(tp.NodeCanonicalPaths[i+1])
			if from == "" || to == "" {
				continue
			}
			fromID := scopeFragments[from]
			toID := scopeFragments[to]
			if fromID == "" || toID == "" {
				continue
			}
			key := fromID + "->" + toID
			reverseKey := toID + "->" + fromID
			if !seen[key] && !seen[reverseKey] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
