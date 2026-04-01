package workscope

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ScopeKind describes what kind of world slice entered the kernel.
type ScopeKind string

const (
	ScopeKindLensStrict         ScopeKind = "lens_strict"
	ScopeKindLensMap            ScopeKind = "lens_map"
	ScopeKindLensResearch       ScopeKind = "lens_research"
	ScopeKindWatchSubject       ScopeKind = "watch_subject"
	ScopeKindScoutJob           ScopeKind = "scout_job"
	ScopeKindSimulationBaseline ScopeKind = "simulation_baseline"
	ScopeKindAdHoc              ScopeKind = "adhoc"
)

// EntryPoint describes which subsystem issued the current work request.
type EntryPoint string

const (
	EntryPointLens       EntryPoint = "lens"
	EntryPointWatch      EntryPoint = "watch"
	EntryPointScout      EntryPoint = "scout"
	EntryPointSimulation EntryPoint = "simulation"
	EntryPointAPI        EntryPoint = "api"
	EntryPointAdHoc      EntryPoint = "adhoc"
)

const (
	ProjectionModeScopeOnly           = "scope_only"
	ProjectionModeScopePlusNeighbors  = "scope_plus_neighbors"
	ProjectionModeScopePlusPropagated = "scope_plus_propagated"
	ProjectionModeAnchoredProjection  = "anchored_projection"
)

// ActionKind describes what the caller wants to do now over a scope.
type ActionKind string

const (
	ActionInspect        ActionKind = "inspect"
	ActionEnrichGraph    ActionKind = "enrich_graph"
	ActionHydrateMetrics ActionKind = "hydrate_metrics"
	ActionTraceSignal    ActionKind = "trace_signal"
	ActionDeriveRisk     ActionKind = "derive_risk"
	ActionSimulate       ActionKind = "simulate"
)

const (
	MutationModeReadMostly                = "read_mostly"
	MutationModeRefreshIfNeeded           = "refresh_if_needed"
	MutationModePersistChanges            = "persist_changes"
	MutationModePersistWorldKeepScope     = "persist_world_keep_scope"
	MutationModePersistWorldScopeRefsOnly = "persist_world_scope_refs_only"
	MutationModePersistWorldExpandScope   = "persist_world_expand_scope"
)

const (
	FreshnessModeUseCurrentState = "use_current_state"
	FreshnessModeRefreshIfStale  = "refresh_if_stale"
)

// ExecutionLayer is a universal orchestration layer name. Layer semantics live outside WorkScope.
type ExecutionLayer string

const (
	ExecutionLayerResolve            ExecutionLayer = "resolve"
	ExecutionLayerCanonicalize       ExecutionLayer = "canonicalize"
	ExecutionLayerGraphEnrich        ExecutionLayer = "graph_enrich"
	ExecutionLayerMaterializedPaths  ExecutionLayer = "materialized_paths"
	ExecutionLayerRouteOverlays      ExecutionLayer = "route_overlays"
	ExecutionLayerOperationalHydrate ExecutionLayer = "operational_hydrate"
	ExecutionLayerSignalPropagation  ExecutionLayer = "signal_propagation"
	ExecutionLayerRiskRules          ExecutionLayer = "risk_rules"
	ExecutionLayerSimulation         ExecutionLayer = "simulation"
)

// WorkScopeIntent is scope semantics only: shape, boundary, and scope-local expansion rules.
// It deliberately excludes ad-hoc execution semantics such as simulation, hydration, or risk actions.
type WorkScopeIntent struct {
	ScopeKind ScopeKind
	Expansion WorkScopeExpansionPolicy
}

// WorkScopeExpansionPolicy says how the scope itself may expand beyond anchors.
type WorkScopeExpansionPolicy struct {
	Mode               string
	NeighborHops       int
	AllowDeferredPaths bool
}

// WorkScopeProjectionPolicy says how the kernel result should be projected back for this request.
type WorkScopeProjectionPolicy struct {
	Mode                     string
	IncludeResolvedNeighbors bool
	IncludePropagatedReach   bool
}

// ExecutionIntent captures the current action over a scope.
type ExecutionIntent struct {
	EntryPoint      EntryPoint
	Action          ActionKind
	DomainModes     []string
	RequestedLayers []ExecutionLayer
	Projection      WorkScopeProjectionPolicy
	MutationMode    string
	FreshnessMode   string
}

// WorkRequest is the universal kernel request shape: stable scope + ephemeral action intent.
type WorkRequest struct {
	Scope  WorkScope
	Intent ExecutionIntent
}

// ExecutionPlan is a normalized orchestration view derived from WorkRequest.
// Layers are ordered by dependency preference, not by ownership of semantic truth.
type ExecutionPlan struct {
	Request WorkRequest
	Layers  []ExecutionLayer
}

// BuildExecutionPlan is a compatibility bridge for legacy callers that only have a WorkScope.
// It derives a default request from the scope and then plans the layers.
func BuildExecutionPlan(scope WorkScope) ExecutionPlan {
	return BuildExecutionPlanForRequest(DefaultRequestForScope(scope))
}

// BuildExecutionPlanForRequest returns the ordered layer plan for this request.
// Empty RequestedLayers fall back to action defaults; resolve is always first.
func BuildExecutionPlanForRequest(request WorkRequest) ExecutionPlan {
	request = NormalizeRequest(request)
	layers := normalizeExecutionLayers(request.Intent.RequestedLayers)
	if len(layers) == 0 {
		layers = defaultLayersForAction(request.Intent.Action)
	}
	layers = normalizeExecutionLayers(append([]ExecutionLayer{ExecutionLayerResolve}, layers...))
	return ExecutionPlan{
		Request: request,
		Layers:  layers,
	}
}

// DefaultRequestForScope derives a stable default action for a scope when a caller has not specified one.
func DefaultRequestForScope(scope WorkScope) WorkRequest {
	scope = Normalize(scope)
	return NormalizeRequest(WorkRequest{
		Scope: scope,
		Intent: ExecutionIntent{
			EntryPoint: defaultEntryPointForScope(scope.Intent.ScopeKind),
			Action:     defaultActionForScope(scope.Intent.ScopeKind),
			Projection: defaultProjectionForScope(scope.Intent.ScopeKind),
		},
	})
}

// NormalizeRequest normalizes both the stable scope and the ephemeral execution intent.
func NormalizeRequest(request WorkRequest) WorkRequest {
	return WorkRequest{
		Scope:  Normalize(request.Scope),
		Intent: normalizeExecutionIntent(request.Intent),
	}
}

// NewAdHocScope builds a scope for ad-hoc work outside a lens while still using the same kernel.
func NewAdHocScope(tenantID, projectID string, refs []string) WorkScope {
	scope := FromBaselineRefs(tenantID, projectID, SimulationBaselineRefsForPaths(refs))
	scope.Intent = WorkScopeIntent{
		ScopeKind: ScopeKindAdHoc,
		Expansion: WorkScopeExpansionPolicy{
			Mode: "anchored_ad_hoc",
		},
	}
	return Normalize(scope)
}

// NewAdHocRequest builds a default ad-hoc request over the supplied anchors.
func NewAdHocRequest(tenantID, projectID string, refs []string, action ActionKind, requestedLayers ...ExecutionLayer) WorkRequest {
	scope := NewAdHocScope(tenantID, projectID, refs)
	return NormalizeRequest(WorkRequest{
		Scope: scope,
		Intent: ExecutionIntent{
			EntryPoint:      EntryPointAdHoc,
			Action:          action,
			RequestedLayers: requestedLayers,
			Projection: WorkScopeProjectionPolicy{
				Mode: ProjectionModeAnchoredProjection,
			},
			MutationMode:  defaultMutationModeForAction(action),
			FreshnessMode: defaultFreshnessModeForAction(action),
		},
	})
}

// SimulationBaselineRefsForPaths is a narrow helper for ad-hoc scope construction.
func SimulationBaselineRefsForPaths(paths []string) horizonmodels.SimulationBaselineRefs {
	return horizonmodels.SimulationBaselineRefs{
		ScopePaths: append([]string(nil), paths...),
	}
}

func normalizeIntent(in WorkScopeIntent) WorkScopeIntent {
	return WorkScopeIntent{
		ScopeKind: ScopeKind(strings.TrimSpace(string(in.ScopeKind))),
		Expansion: WorkScopeExpansionPolicy{
			Mode:               strings.TrimSpace(in.Expansion.Mode),
			NeighborHops:       max(0, in.Expansion.NeighborHops),
			AllowDeferredPaths: in.Expansion.AllowDeferredPaths,
		},
	}
}

func mergeIntent(a, b WorkScopeIntent) WorkScopeIntent {
	return normalizeIntent(WorkScopeIntent{
		ScopeKind: ScopeKind(firstNonEmpty(strings.TrimSpace(string(a.ScopeKind)), strings.TrimSpace(string(b.ScopeKind)))),
		Expansion: WorkScopeExpansionPolicy{
			Mode:               firstNonEmpty(strings.TrimSpace(a.Expansion.Mode), strings.TrimSpace(b.Expansion.Mode)),
			NeighborHops:       max(a.Expansion.NeighborHops, b.Expansion.NeighborHops),
			AllowDeferredPaths: a.Expansion.AllowDeferredPaths || b.Expansion.AllowDeferredPaths,
		},
	})
}

func normalizeExecutionIntent(in ExecutionIntent) ExecutionIntent {
	return ExecutionIntent{
		EntryPoint:      EntryPoint(strings.TrimSpace(string(in.EntryPoint))),
		Action:          ActionKind(strings.TrimSpace(string(in.Action))),
		DomainModes:     normalizeStringSlice(in.DomainModes),
		RequestedLayers: normalizeExecutionLayers(in.RequestedLayers),
		Projection: WorkScopeProjectionPolicy{
			Mode:                     strings.TrimSpace(in.Projection.Mode),
			IncludeResolvedNeighbors: in.Projection.IncludeResolvedNeighbors,
			IncludePropagatedReach:   in.Projection.IncludePropagatedReach,
		},
		MutationMode:  strings.TrimSpace(in.MutationMode),
		FreshnessMode: strings.TrimSpace(in.FreshnessMode),
	}
}

func normalizeExecutionLayers(in []ExecutionLayer) []ExecutionLayer {
	order := map[ExecutionLayer]int{
		ExecutionLayerResolve:            10,
		ExecutionLayerCanonicalize:       20,
		ExecutionLayerGraphEnrich:        30,
		ExecutionLayerMaterializedPaths:  40,
		ExecutionLayerRouteOverlays:      45,
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

func defaultEntryPointForScope(kind ScopeKind) EntryPoint {
	switch kind {
	case ScopeKindWatchSubject:
		return EntryPointWatch
	case ScopeKindScoutJob:
		return EntryPointScout
	case ScopeKindSimulationBaseline:
		return EntryPointSimulation
	case ScopeKindAdHoc:
		return EntryPointAdHoc
	case ScopeKindLensStrict, ScopeKindLensMap, ScopeKindLensResearch:
		return EntryPointLens
	default:
		return EntryPointAPI
	}
}

func defaultActionForScope(kind ScopeKind) ActionKind {
	switch kind {
	case ScopeKindWatchSubject:
		return ActionDeriveRisk
	case ScopeKindScoutJob:
		return ActionEnrichGraph
	case ScopeKindSimulationBaseline:
		return ActionSimulate
	default:
		return ActionInspect
	}
}

func defaultProjectionForScope(kind ScopeKind) WorkScopeProjectionPolicy {
	switch kind {
	case ScopeKindLensMap:
		return WorkScopeProjectionPolicy{
			Mode:                     ProjectionModeScopePlusNeighbors,
			IncludeResolvedNeighbors: true,
		}
	case ScopeKindWatchSubject, ScopeKindSimulationBaseline:
		return WorkScopeProjectionPolicy{
			Mode:                   ProjectionModeScopePlusPropagated,
			IncludePropagatedReach: true,
		}
	default:
		return WorkScopeProjectionPolicy{
			Mode: ProjectionModeScopeOnly,
		}
	}
}

func defaultLayersForAction(action ActionKind) []ExecutionLayer {
	switch action {
	case ActionEnrichGraph:
		return []ExecutionLayer{
			ExecutionLayerCanonicalize,
			ExecutionLayerGraphEnrich,
		}
	case ActionHydrateMetrics:
		return []ExecutionLayer{
			ExecutionLayerMaterializedPaths,
			ExecutionLayerRouteOverlays,
			ExecutionLayerOperationalHydrate,
		}
	case ActionTraceSignal:
		return []ExecutionLayer{
			ExecutionLayerSignalPropagation,
		}
	case ActionDeriveRisk:
		return []ExecutionLayer{
			ExecutionLayerMaterializedPaths,
			ExecutionLayerRouteOverlays,
			ExecutionLayerOperationalHydrate,
			ExecutionLayerSignalPropagation,
			ExecutionLayerRiskRules,
		}
	case ActionSimulate:
		return []ExecutionLayer{
			ExecutionLayerMaterializedPaths,
			ExecutionLayerRouteOverlays,
			ExecutionLayerOperationalHydrate,
			ExecutionLayerSignalPropagation,
			ExecutionLayerSimulation,
		}
	case ActionInspect:
		fallthrough
	default:
		return []ExecutionLayer{
			ExecutionLayerCanonicalize,
		}
	}
}

func defaultMutationModeForAction(action ActionKind) string {
	switch action {
	case ActionEnrichGraph:
		return MutationModePersistChanges
	case ActionHydrateMetrics:
		return MutationModeRefreshIfNeeded
	case ActionSimulate, ActionTraceSignal, ActionDeriveRisk, ActionInspect:
		fallthrough
	default:
		return MutationModeReadMostly
	}
}

func defaultFreshnessModeForAction(action ActionKind) string {
	switch action {
	case ActionHydrateMetrics, ActionSimulate, ActionDeriveRisk:
		return FreshnessModeRefreshIfStale
	default:
		return FreshnessModeUseCurrentState
	}
}
