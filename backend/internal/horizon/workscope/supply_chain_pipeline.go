package workscope

// SupplyChainExecutionPipeline documents the intended end-to-end layer order for supply-chain work:
// taxonomy/canonicalization, graph enrichment, structural path materialization, logistics route overlays,
// then downstream metrics, propagation, and simulation clients.
type SupplyChainExecutionPipeline struct {
	Scope WorkScope
}

// NewSupplyChainExecutionPipeline returns a normalized supply-chain pipeline contract.
func NewSupplyChainExecutionPipeline(s WorkScope) SupplyChainExecutionPipeline {
	return SupplyChainExecutionPipeline{Scope: Normalize(s)}
}

// StageNames documents the preferred orchestration order for supply-chain runtime clients.
func (SupplyChainExecutionPipeline) StageNames() []string {
	layers := []ExecutionLayer{
		ExecutionLayerResolve,
		ExecutionLayerCanonicalize,
		ExecutionLayerGraphEnrich,
		ExecutionLayerMaterializedPaths,
		ExecutionLayerRouteOverlays,
		ExecutionLayerOperationalHydrate,
		ExecutionLayerSignalPropagation,
		ExecutionLayerRiskRules,
		ExecutionLayerSimulation,
	}
	out := make([]string, 0, len(layers))
	for _, layer := range layers {
		out = append(out, string(layer))
	}
	return out
}
