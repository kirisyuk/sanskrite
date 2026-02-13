package models

import "time"

const (
	DomainReadinessReady         = "ready"
	DomainReadinessReadyWithGaps = "ready_with_gaps"
)

type DomainContractSnapshot struct {
	CanonicalPath          string              `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	ContractType           string              `json:"contractType" firestore:"contractType"`
	Title                  string              `json:"title,omitempty" firestore:"title,omitempty"`
	Description            string              `json:"description,omitempty" firestore:"description,omitempty"`
	Facets                 map[string][]string `json:"facets,omitempty" firestore:"facets,omitempty"`
	TemporalHorizonStart   string              `json:"temporalHorizonStart,omitempty" firestore:"temporalHorizonStart,omitempty"`
	TemporalHorizonEnd     string              `json:"temporalHorizonEnd,omitempty" firestore:"temporalHorizonEnd,omitempty"`
	Polarity               []string            `json:"polarity,omitempty" firestore:"polarity,omitempty"`
	CrossFacetExpectations []string            `json:"crossFacetExpectations,omitempty" firestore:"crossFacetExpectations,omitempty"`
}

type DomainGateCheck struct {
	CheckID        string   `json:"checkId" firestore:"checkId"`
	Status         string   `json:"status" firestore:"status"`
	Priority       string   `json:"priority" firestore:"priority"`
	GapDescription string   `json:"gapDescription,omitempty" firestore:"gapDescription,omitempty"`
	Affected       []string `json:"affected,omitempty" firestore:"affected,omitempty"`
	EnrichmentHint string   `json:"enrichmentHint,omitempty" firestore:"enrichmentHint,omitempty"`
}

type DomainGateSummary struct {
	OverallStatus  string            `json:"overallStatus" firestore:"overallStatus"`
	TotalChecks    int               `json:"totalChecks" firestore:"totalChecks"`
	Passed         int               `json:"passed" firestore:"passed"`
	Failed         int               `json:"failed" firestore:"failed"`
	Partial        int               `json:"partial" firestore:"partial"`
	Checks         []DomainGateCheck `json:"checks,omitempty" firestore:"checks,omitempty"`
	FailedCheckIDs []string          `json:"failedCheckIds,omitempty" firestore:"failedCheckIds,omitempty"`
}

type CriticalDependency struct {
	EdgeID              string   `json:"edgeId,omitempty" firestore:"edgeId,omitempty"`
	SourceFragmentID    string   `json:"sourceFragmentId,omitempty" firestore:"sourceFragmentId,omitempty"`
	TargetFragmentID    string   `json:"targetFragmentId,omitempty" firestore:"targetFragmentId,omitempty"`
	SourceCanonicalPath string   `json:"sourceCanonicalPath,omitempty" firestore:"sourceCanonicalPath,omitempty"`
	TargetCanonicalPath string   `json:"targetCanonicalPath,omitempty" firestore:"targetCanonicalPath,omitempty"`
	RelationType        string   `json:"relationType,omitempty" firestore:"relationType,omitempty"`
	FacetKey            string   `json:"facetKey,omitempty" firestore:"facetKey,omitempty"`
	TopDependencyScore  float64  `json:"topDependencyScore" firestore:"topDependencyScore"`
	Confidence          float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	SourceRefs          []string `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
}

type CriticalNode struct {
	FragmentID        string  `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	CanonicalPath     string  `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	Criticality       float64 `json:"criticality" firestore:"criticality"`
	CriticalityReason string  `json:"criticalityReason,omitempty" firestore:"criticalityReason,omitempty"`
	Confidence        float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type ConcentrationEvidence struct {
	MarketCanonicalPath string   `json:"marketCanonicalPath,omitempty" firestore:"marketCanonicalPath,omitempty"`
	TAM                 *float64 `json:"tam,omitempty" firestore:"tam,omitempty"`
	CAGR                *float64 `json:"cagr,omitempty" firestore:"cagr,omitempty"`
	CR4                 *float64 `json:"cr4,omitempty" firestore:"cr4,omitempty"`
	HHI                 *float64 `json:"hhi,omitempty" firestore:"hhi,omitempty"`
	Period              string   `json:"period,omitempty" firestore:"period,omitempty"`
	Unit                string   `json:"unit,omitempty" firestore:"unit,omitempty"`
	DataGap             string   `json:"dataGap,omitempty" firestore:"dataGap,omitempty"`
}

type ControlExposure struct {
	MechanismType string   `json:"mechanismType,omitempty" firestore:"mechanismType,omitempty"`
	Severity      string   `json:"severity,omitempty" firestore:"severity,omitempty"`
	Confidence    float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	EdgeIDs       []string `json:"edgeIds,omitempty" firestore:"edgeIds,omitempty"`
	Path          []string `json:"path,omitempty" firestore:"path,omitempty"`
}

type ImpactPath struct {
	DependencyEdgeID string   `json:"dependencyEdgeId,omitempty" firestore:"dependencyEdgeId,omitempty"`
	NodePaths        []string `json:"nodePaths,omitempty" firestore:"nodePaths,omitempty"`
	EdgeIDs          []string `json:"edgeIds,omitempty" firestore:"edgeIds,omitempty"`
	FacetKey         string   `json:"facetKey,omitempty" firestore:"facetKey,omitempty"`
	Criticality      float64  `json:"criticality" firestore:"criticality"`
}

type DataGap struct {
	GapType          string `json:"gapType" firestore:"gapType"`
	AffectedFacet    string `json:"affectedFacet,omitempty" firestore:"affectedFacet,omitempty"`
	AffectedNodePath string `json:"affectedNodePath,omitempty" firestore:"affectedNodePath,omitempty"`
	AffectedEdgeID   string `json:"affectedEdgeId,omitempty" firestore:"affectedEdgeId,omitempty"`
	Hint             string `json:"hint,omitempty" firestore:"hint,omitempty"`
	Priority         string `json:"priority,omitempty" firestore:"priority,omitempty"`
}

type DomainThesis struct {
	Text       string  `json:"text" firestore:"text"`
	Confidence float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type Alternative struct {
	Title         string  `json:"title,omitempty" firestore:"title,omitempty"`
	Description   string  `json:"description,omitempty" firestore:"description,omitempty"`
	Feasibility   string  `json:"feasibility,omitempty" firestore:"feasibility,omitempty"`
	CapacityDelta string  `json:"capacityDelta,omitempty" firestore:"capacityDelta,omitempty"`
	LatencyDelta  string  `json:"latencyDelta,omitempty" firestore:"latencyDelta,omitempty"`
	CostDelta     string  `json:"costDelta,omitempty" firestore:"costDelta,omitempty"`
	TimeToSwitch  string  `json:"timeToSwitch,omitempty" firestore:"timeToSwitch,omitempty"`
	Confidence    float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type ObjectiveCheck struct {
	OntologyConformance string  `json:"ontologyConformance,omitempty" firestore:"ontologyConformance,omitempty"`
	PolicyConformance   string  `json:"policyConformance,omitempty" firestore:"policyConformance,omitempty"`
	ProvenanceQuality   string  `json:"provenanceQuality,omitempty" firestore:"provenanceQuality,omitempty"`
	BiasAssessment      string  `json:"biasAssessment,omitempty" firestore:"biasAssessment,omitempty"`
	Completeness        string  `json:"completeness,omitempty" firestore:"completeness,omitempty"`
	Confidence          float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type FacetSummary struct {
	NodeCount         int     `json:"nodeCount" firestore:"nodeCount"`
	CriticalDepsCount int     `json:"criticalDepsCount" firestore:"criticalDepsCount"`
	ImpactPathsCount  int     `json:"impactPathsCount" firestore:"impactPathsCount"`
	QualityScore      float64 `json:"qualityScore" firestore:"qualityScore"`
	ConnectivityScore float64 `json:"connectivityScore" firestore:"connectivityScore"`
}

type DomainAnswerSnapshot struct {
	ID                       string                     `json:"id" firestore:"id"`
	TenantID                 string                     `json:"tenantId" firestore:"tenantId"`
	ProjectID                string                     `json:"projectId" firestore:"projectId"`
	LensID                   string                     `json:"lensId" firestore:"lensId"`
	DomainID                 string                     `json:"domainId" firestore:"domainId"`
	DomainCanonicalPath      string                     `json:"domainCanonicalPath,omitempty" firestore:"domainCanonicalPath,omitempty"`
	DomainContract           DomainContractSnapshot     `json:"domainContract" firestore:"domainContract"`
	AssembledAt              time.Time                  `json:"assembledAt" firestore:"assembledAt"`
	ReadinessStatus          string                     `json:"readinessStatus" firestore:"readinessStatus"`
	EnrichmentCycles         int                        `json:"enrichmentCycles" firestore:"enrichmentCycles"`
	Gate                     DomainGateSummary          `json:"gate" firestore:"gate"`
	CriticalDependencies     []CriticalDependency       `json:"criticalDependencies,omitempty" firestore:"criticalDependencies,omitempty"`
	CriticalNodes            []CriticalNode             `json:"criticalNodes,omitempty" firestore:"criticalNodes,omitempty"`
	ConcentrationEvidence    []ConcentrationEvidence    `json:"concentrationEvidence,omitempty" firestore:"concentrationEvidence,omitempty"`
	ControlExposure          []ControlExposure          `json:"controlExposure,omitempty" firestore:"controlExposure,omitempty"`
	ImpactPaths              []ImpactPath               `json:"impactPaths,omitempty" firestore:"impactPaths,omitempty"`
	DataGaps                 []DataGap                  `json:"dataGaps,omitempty" firestore:"dataGaps,omitempty"`
	Thesis                   DomainThesis               `json:"thesis" firestore:"thesis"`
	Alternatives             []Alternative              `json:"alternatives,omitempty" firestore:"alternatives,omitempty"`
	ObjectiveCheck           ObjectiveCheck             `json:"objectiveCheck" firestore:"objectiveCheck"`
	FacetSummary             map[string]FacetSummary    `json:"facetSummary,omitempty" firestore:"facetSummary,omitempty"`
	HighlightedNodeIDs       []string                   `json:"highlightedNodeIds,omitempty" firestore:"highlightedNodeIds,omitempty"`
	HighlightedEdgeIDs       []string                   `json:"highlightedEdgeIds,omitempty" firestore:"highlightedEdgeIds,omitempty"`
	GroundingContractVersion string                     `json:"groundingContractVersion,omitempty" firestore:"groundingContractVersion,omitempty"`
	OriginMode               string                     `json:"originMode,omitempty" firestore:"originMode,omitempty"`
	InternalEvidence         *GroundingInternalEvidence `json:"internalEvidence,omitempty" firestore:"internalEvidence,omitempty"`
	ExternalEvidence         *GroundingExternalEvidence `json:"externalEvidence,omitempty" firestore:"externalEvidence,omitempty"`
	ResolutionStats          *GroundingResolutionStats  `json:"resolutionStats,omitempty" firestore:"resolutionStats,omitempty"`
	CreatedAt                time.Time                  `json:"createdAt" firestore:"createdAt"`
	UpdatedAt                time.Time                  `json:"updatedAt" firestore:"updatedAt"`
}
