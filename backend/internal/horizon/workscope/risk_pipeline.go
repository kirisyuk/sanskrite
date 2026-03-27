package workscope

// RiskMonitorPipeline is the documented entry shape for proactive risk monitoring: a client of the same
// WorkScope → resolve → layers pipeline as simulation and watch, without introducing a parallel ontology.
//
// Future implementations should: Resolve scope → optional scout/graph enrich → materialized paths as needed →
// OperationalHydrator → optional propagation.SimulateSignalFromSeeds → alert rules.
type RiskMonitorPipeline struct {
	Scope WorkScope
}

// NewRiskMonitorPipeline returns a pipeline with normalized scope (placeholder until handlers are wired).
func NewRiskMonitorPipeline(s WorkScope) RiskMonitorPipeline {
	return RiskMonitorPipeline{Scope: Normalize(s)}
}

// StageNames documents the intended orchestration order for risk monitoring jobs.
func (RiskMonitorPipeline) StageNames() []string {
	return []string{
		"resolve",
		"graph_enrich",
		"materialized_paths",
		"operational_hydrate",
		"signal_propagation",
		"risk_rules",
	}
}
