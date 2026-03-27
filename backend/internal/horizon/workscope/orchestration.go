package workscope

import "strings"

// ScopeKind describes what kind of work volume entered the system.
type ScopeKind string

const (
	ScopeKindLensStrict         ScopeKind = "lens_strict"
	ScopeKindLensMap            ScopeKind = "lens_map"
	ScopeKindWatchSubject       ScopeKind = "watch_subject"
	ScopeKindScoutJob           ScopeKind = "scout_job"
	ScopeKindSimulationBaseline ScopeKind = "simulation_baseline"
	ScopeKindAdHoc              ScopeKind = "adhoc"
)

// EntryPoint describes which subsystem asked the kernel to do work.
type EntryPoint string

const (
	EntryPointLens       EntryPoint = "lens"
	EntryPointWatch      EntryPoint = "watch"
	EntryPointScout      EntryPoint = "scout"
	EntryPointSimulation EntryPoint = "simulation"
	EntryPointAPI        EntryPoint = "api"
	EntryPointAdHoc      EntryPoint = "adhoc"
)

// ExecutionLayer is a universal orchestration layer name. Layer semantics live outside WorkScope;
// WorkScope only requests orchestration and projection over those layers.
type ExecutionLayer string

const (
	ExecutionLayerResolve            ExecutionLayer = "resolve"
	ExecutionLayerCanonicalize       ExecutionLayer = "canonicalize"
	ExecutionLayerGraphEnrich        ExecutionLayer = "graph_enrich"
	ExecutionLayerMaterializedPaths  ExecutionLayer = "materialized_paths"
	ExecutionLayerOperationalHydrate ExecutionLayer = "operational_hydrate"
	ExecutionLayerSignalPropagation  ExecutionLayer = "signal_propagation"
	ExecutionLayerRiskRules          ExecutionLayer = "risk_rules"
	ExecutionLayerSimulation         ExecutionLayer = "simulation"
)

// WorkScopeIntent is orchestration metadata attached to an otherwise ref-shaped scope.
// It must stay optional so legacy callers can keep passing SimulationBaselineRefs-shaped input.
type WorkScopeIntent struct {
	ScopeKind       ScopeKind
	EntryPoint      EntryPoint
	DomainModes     []string
	RequestedLayers []ExecutionLayer
	Expansion       WorkScopeExpansionPolicy
	Projection      WorkScopeProjectionPolicy
}

// WorkScopeExpansionPolicy says how aggressively a client may expand beyond its initial anchors.
type WorkScopeExpansionPolicy struct {
	Mode                     string
	NeighborHops             int
	AllowDeferredPaths       bool
	AllowGraphEnrichment     bool
	AllowMaterializedPaths   bool
	AllowPropagatedReach     bool
}

// WorkScopeProjectionPolicy says how much of the kernel output should be projected back to the caller.
type WorkScopeProjectionPolicy struct {
	Mode                   string
	IncludeResolvedNeighbors bool
	IncludePropagatedReach bool
}

// ExecutionPlan is a normalized orchestration view derived from WorkScope intent.
type ExecutionPlan struct {
	Scope  WorkScope
	Layers []ExecutionLayer
}

// BuildExecutionPlan returns the ordered layer plan for this scope. Empty RequestedLayers fall back
// to entrypoint defaults; resolve is always first because every orchestrated flow starts there.
func BuildExecutionPlan(scope WorkScope) ExecutionPlan {
	scope = Normalize(scope)
	layers := normalizeExecutionLayers(scope.Intent.RequestedLayers)
	if len(layers) == 0 {
		layers = defaultLayersForIntent(scope.Intent)
	}
	layers = normalizeExecutionLayers(append([]ExecutionLayer{ExecutionLayerResolve}, layers...))
	return ExecutionPlan{
		Scope:  scope,
		Layers: layers,
	}
}

// NewAdHocScope builds a scope for ad-hoc work outside a lens while still using the same kernel layers.
func NewAdHocScope(tenantID, projectID string, refs []string, requestedLayers ...ExecutionLayer) WorkScope {
	scope := FromBaselineRefs(tenantID, projectID, SimulationBaselineRefsForPaths(refs))
	scope.Intent = WorkScopeIntent{
		ScopeKind:       ScopeKindAdHoc,
		EntryPoint:      EntryPointAdHoc,
		RequestedLayers: requestedLayers,
		Expansion: WorkScopeExpansionPolicy{
			Mode:                   "anchored_ad_hoc",
			AllowGraphEnrichment:   true,
			AllowMaterializedPaths: true,
			AllowPropagatedReach:   true,
		},
		Projection: WorkScopeProjectionPolicy{
			Mode: "anchored_projection",
		},
	}
	return Normalize(scope)
}

// SimulationBaselineRefsForPaths is a narrow helper for ad-hoc scope construction.
func SimulationBaselineRefsForPaths(paths []string) SimulationBaselineRefs {
	return SimulationBaselineRefs{
		ScopePaths: append([]string(nil), paths...),
	}
}

func normalizeIntent(in WorkScopeIntent) WorkScopeIntent {
	out := WorkScopeIntent{
		ScopeKind:       ScopeKind(strings.TrimSpace(string(in.ScopeKind))),
		EntryPoint:      EntryPoint(strings.TrimSpace(string(in.EntryPoint))),
		DomainModes:     normalizeStringSlice(in.DomainModes),
		RequestedLayers: normalizeExecutionLayers(in.RequestedLayers),
		Expansion: WorkScopeExpansionPolicy{
			Mode:                   strings.TrimSpace(in.Expansion.Mode),
			NeighborHops:           max(0, in.Expansion.NeighborHops),
			AllowDeferredPaths:     in.Expansion.AllowDeferredPaths,
			AllowGraphEnrichment:   in.Expansion.AllowGraphEnrichment,
			AllowMaterializedPaths: in.Expansion.AllowMaterializedPaths,
			AllowPropagatedReach:   in.Expansion.AllowPropagatedReach,
		},
		Projection: WorkScopeProjectionPolicy{
			Mode:                   strings.TrimSpace(in.Projection.Mode),
			IncludeResolvedNeighbors: in.Projection.IncludeResolvedNeighbors,
			IncludePropagatedReach: in.Projection.IncludePropagatedReach,
		},
	}
	return out
}

func mergeIntent(a, b WorkScopeIntent) WorkScopeIntent {
	return normalizeIntent(WorkScopeIntent{
		ScopeKind:       ScopeKind(firstNonEmpty(strings.TrimSpace(string(a.ScopeKind)), strings.TrimSpace(string(b.ScopeKind)))),
		EntryPoint:      EntryPoint(firstNonEmpty(strings.TrimSpace(string(a.EntryPoint)), strings.TrimSpace(string(b.EntryPoint)))),
		DomainModes:     mergeStringSlices(a.DomainModes, b.DomainModes),
		RequestedLayers: normalizeExecutionLayers(append(append([]ExecutionLayer{}, a.RequestedLayers...), b.RequestedLayers...)),
		Expansion: WorkScopeExpansionPolicy{
			Mode:                   firstNonEmpty(strings.TrimSpace(a.Expansion.Mode), strings.TrimSpace(b.Expansion.Mode)),
			NeighborHops:           max(a.Expansion.NeighborHops, b.Expansion.NeighborHops),
			AllowDeferredPaths:     a.Expansion.AllowDeferredPaths || b.Expansion.AllowDeferredPaths,
			AllowGraphEnrichment:   a.Expansion.AllowGraphEnrichment || b.Expansion.AllowGraphEnrichment,
			AllowMaterializedPaths: a.Expansion.AllowMaterializedPaths || b.Expansion.AllowMaterializedPaths,
			AllowPropagatedReach:   a.Expansion.AllowPropagatedReach || b.Expansion.AllowPropagatedReach,
		},
		Projection: WorkScopeProjectionPolicy{
			Mode:                   firstNonEmpty(strings.TrimSpace(a.Projection.Mode), strings.TrimSpace(b.Projection.Mode)),
			IncludeResolvedNeighbors: a.Projection.IncludeResolvedNeighbors || b.Projection.IncludeResolvedNeighbors,
			IncludePropagatedReach: a.Projection.IncludePropagatedReach || b.Projection.IncludePropagatedReach,
		},
	})
}

func normalizeExecutionLayers(in []ExecutionLayer) []ExecutionLayer {
	order := map[ExecutionLayer]int{
		ExecutionLayerResolve:            10,
		ExecutionLayerCanonicalize:       20,
		ExecutionLayerGraphEnrich:        30,
		ExecutionLayerMaterializedPaths:  40,
		ExecutionLayerOperationalHydrate: 50,
		ExecutionLayerSignalPropagation:  60,
		ExecutionLayerRiskRules:          70,
		ExecutionLayerSimulation:         80,
	}
	seen := make(map[ExecutionLayer]struct{}, len(in))
	out := make([]ExecutionLayer, 0, len(in))
	for _, layer := range in {
		layer = ExecutionLayer(strings.TrimSpace(string(layer)))
		if layer == "" {
			continue
		}
		if _, ok := seen[layer]; ok {
			continue
		}
		seen[layer] = struct{}{}
		out = append(out, layer)
	}
	sortExecutionLayers(out, order)
	return out
}

func sortExecutionLayers(layers []ExecutionLayer, order map[ExecutionLayer]int) {
	if len(layers) < 2 {
		return
	}
	for i := 0; i < len(layers)-1; i++ {
		for j := i + 1; j < len(layers); j++ {
			li := order[layers[i]]
			lj := order[layers[j]]
			if li == 0 {
				li = 999
			}
			if lj == 0 {
				lj = 999
			}
			if lj < li || (lj == li && string(layers[j]) < string(layers[i])) {
				layers[i], layers[j] = layers[j], layers[i]
			}
		}
	}
}

func defaultLayersForIntent(intent WorkScopeIntent) []ExecutionLayer {
	switch intent.EntryPoint {
	case EntryPointScout:
		return []ExecutionLayer{
			ExecutionLayerCanonicalize,
			ExecutionLayerGraphEnrich,
		}
	case EntryPointSimulation:
		return []ExecutionLayer{
			ExecutionLayerMaterializedPaths,
			ExecutionLayerOperationalHydrate,
			ExecutionLayerSignalPropagation,
			ExecutionLayerSimulation,
		}
	case EntryPointWatch:
		return []ExecutionLayer{
			ExecutionLayerGraphEnrich,
			ExecutionLayerMaterializedPaths,
			ExecutionLayerOperationalHydrate,
			ExecutionLayerSignalPropagation,
			ExecutionLayerRiskRules,
		}
	case EntryPointLens, EntryPointAPI, EntryPointAdHoc:
		return []ExecutionLayer{
			ExecutionLayerCanonicalize,
			ExecutionLayerGraphEnrich,
			ExecutionLayerMaterializedPaths,
			ExecutionLayerOperationalHydrate,
		}
	default:
		return nil
	}
}
