package workscope

// RiskMonitorPipeline is the documented entry shape for proactive risk monitoring: a client of the same
// WorkScope → resolve → layers pipeline as simulation and watch, without introducing a parallel ontology.
//
// Future implementations should: Resolve scope → optional scout/graph enrich → materialized paths as needed →
// route overlays → OperationalHydrator → optional propagation.SimulateSignalFromSeeds → alert rules.
type RiskMonitorPipeline struct {
	Scope WorkScope
}

// NewRiskMonitorPipeline returns a pipeline with normalized scope (placeholder until handlers are wired).
func NewRiskMonitorPipeline(s WorkScope) RiskMonitorPipeline {
	return RiskMonitorPipeline{Scope: Normalize(s)}
}

// StageNames documents the intended orchestration order for risk monitoring jobs.
func (RiskMonitorPipeline) StageNames() []string {
	layers := []ExecutionLayer{
		ExecutionLayerResolve,
		ExecutionLayerGraphEnrich,
		ExecutionLayerMaterializedPaths,
		ExecutionLayerRouteOverlays,
		ExecutionLayerOperationalHydrate,
		ExecutionLayerSignalPropagation,
		ExecutionLayerRiskRules,
	}
	out := make([]string, 0, len(layers))
	for _, layer := range layers {
		out = append(out, string(layer))
	}
	return out
}
