package scout

// Contract layers in Scout.
//
// 1) Domain intent contracts:
//    - Define WHAT analytical contract the lens/job is executing
//      (e.g. ownership-structure, commonality-analysis, taxonomy domain contract types).
//    - Implemented in scout/pipeline (DomainContract + ContractHandler) and scout/contracts/*.
//
// 2) Semantic graph contracts:
//    - Define WHAT is valid in the graph model:
//      canonical paths, type↔path consistency, relation taxonomy and direction, policy validation.
//    - Implemented via taxonomy validation + policy validators + critic validators.
//
// 3) Runtime execution contracts:
//    - Define HOW pipeline stages are executed under cost/quality constraints:
//      context envelope, missingInfo relevance budget, metrics refresh conditions.
//    - Implemented in ScoutEngine orchestration and agent runtime guards.

const (
	ContractLayerDomainIntent     = "domain_intent"
	ContractLayerSemanticGraph    = "semantic_graph"
	ContractLayerRuntimeExecution = "runtime_execution"
)

const (
	RuntimeContractContextEnvelope      = "runtime.context_envelope"
	RuntimeContractMissingInfoRelevance = "runtime.missing_info_relevance"
	RuntimeContractMetricsRefresh       = "runtime.metrics_refresh"
)

const (
	SemanticContractCanonicalPath = "semantic.canonical_path"
	SemanticContractTypePath      = "semantic.type_path"
	SemanticContractEdgeDirection = "semantic.edge_direction"
)
