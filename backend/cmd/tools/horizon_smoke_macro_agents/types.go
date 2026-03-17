package main

import (
	"time"

	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	modeLegacyTrigger     = "trigger"
	modeLegacyCoverageGap = "coverage-gap"
	modeGroundedRun       = "grounded-run"
	modeAnalyzeChain      = "analyze-chain"
	modeReplayReport      = "replay-report"
	modeLensE2E           = "lens-e2e"
	modeFullE2E           = "full-e2e"
	modeScanCandidates    = "scan-candidates"

	stageAppearance      = "appearance"
	stageSignalAdmission = "signal_admission"
	stageFarEndImpact    = "far_end_impact"
	stageInterpretable   = "interpretability"
	stageMeaningful      = "meaningfulness"

	verdictPass = "pass"
	verdictWarn = "warn"
	verdictFail = "fail"
)

type toolOptions struct {
	ProjectID             string
	TenantID              string
	CanonicalPath         string
	CanonicalContains     string
	SubjectID             string
	LensID                string
	RunID                 string
	ProposalID            string
	FactID                string
	AxisPrefix            string
	Mode                  string
	TriggerReason         string
	QueryOverride         string
	OutPath               string
	LookbackDays          int
	Limit                 int
	TopEndpoints          int
	TopInterpretations    int
	RequireMaterialChange bool
	PersistPropagation    bool
	EnablePropagationTrace bool
	PropagationTraceSteps int
	TextOnly              bool
	ScanFacts             int
}

type E2EReport struct {
	GeneratedAt           time.Time                    `json:"generatedAt"`
	Mode                  string                       `json:"mode"`
	Subject               *SubjectSummary              `json:"subject,omitempty"`
	Runtime               *RuntimeSummary              `json:"runtime,omitempty"`
	Scan                  *CandidateScanSummary        `json:"scan,omitempty"`
	Grounding             *GroundingSummary            `json:"grounding,omitempty"`
	Queries               []string                     `json:"queries,omitempty"`
	Runs                  []*RunSummary                `json:"runs,omitempty"`
	Proposals             []*ProposalSummary           `json:"proposals,omitempty"`
	AcceptedSignals       []*AcceptedSignalSummary     `json:"acceptedSignals,omitempty"`
	PropagationAnalysis   *PropagationAnalysisSummary  `json:"propagationAnalysis,omitempty"`
	InterpretationAnalysis *InterpretationSummary      `json:"interpretationAnalysis,omitempty"`
	Criteria              []*StageVerdict              `json:"criteria,omitempty"`
	Verdict               *FinalVerdict                `json:"verdict,omitempty"`
}

type SubjectSummary struct {
	ID                   string         `json:"id"`
	SubjectKind          string         `json:"subjectKind"`
	DisplayLabel         string         `json:"displayLabel"`
	AnchorCanonicalPath  string         `json:"anchorCanonicalPath"`
	LensID               string         `json:"lensId,omitempty"`
	MacroAgentPolicyID   string         `json:"macroAgentPolicyId,omitempty"`
	MacroAgentClass      string         `json:"macroAgentClass,omitempty"`
	UniversePaths        []string       `json:"universePaths,omitempty"`
	ArtifactSummary      map[string]any `json:"artifactSummary,omitempty"`
	EscapedOutsideLens   []string       `json:"escapedOutsideLens,omitempty"`
}

type RuntimeSummary struct {
	TriggerID       string   `json:"triggerId,omitempty"`
	TriggerDecision string   `json:"triggerDecision,omitempty"`
	TriggerReason   string   `json:"triggerReason,omitempty"`
	RunIDs          []string `json:"runIds,omitempty"`
	ReportRunIDs    []string `json:"reportRunIds,omitempty"`
	FreshExecution  bool     `json:"freshExecution"`
	NoopReason      string   `json:"noopReason,omitempty"`
	MaterialChange  bool     `json:"materialChange"`
}

type GroundingSummary struct {
	Model                string         `json:"model,omitempty"`
	CitationsCount       int            `json:"citationsCount"`
	WebURLs              []string       `json:"webUrls,omitempty"`
	DatastoreFragmentIDs []string       `json:"datastoreFragmentIds,omitempty"`
	HasWebResults        bool           `json:"hasWebResults"`
	HasDatastoreResults  bool           `json:"hasDatastoreResults"`
	QueryFamilies        []any          `json:"queryFamilies,omitempty"`
	SearchWindow         map[string]any `json:"searchWindow,omitempty"`
}

type RunSummary struct {
	ID                  string         `json:"id"`
	Status              string         `json:"status"`
	TriggerType         string         `json:"triggerType"`
	TriggerReason       string         `json:"triggerReason"`
	Queries             []string       `json:"queries,omitempty"`
	ProducedProposalIDs []string       `json:"producedProposalIds,omitempty"`
	Trace               map[string]any `json:"trace,omitempty"`
	StartedAt           *time.Time     `json:"startedAt,omitempty"`
	CompletedAt         *time.Time     `json:"completedAt,omitempty"`
}

type ProposalSummary struct {
	ID                   string                    `json:"id"`
	ProposalType         string                    `json:"proposalType"`
	SignalKey            string                    `json:"signalKey,omitempty"`
	TargetCanonicalPaths []string                  `json:"targetCanonicalPaths"`
	Confidence           float64                   `json:"confidence"`
	Explanation          string                    `json:"explanation,omitempty"`
	Decision             string                    `json:"decision,omitempty"`
	ReasonCodes          []string                  `json:"reasonCodes,omitempty"`
	AppliedArtifacts     []string                  `json:"appliedArtifacts,omitempty"`
	SourceRefs           []horizonmodels.SourceRef `json:"sourceRefs,omitempty"`
	Payload              map[string]any            `json:"payload,omitempty"`
	CreatedAt            time.Time                 `json:"createdAt"`
}

type AcceptedSignalSummary struct {
	Fact               *coremodels.OntologyFact         `json:"fact,omitempty"`
	ProposalID         string                           `json:"proposalId,omitempty"`
	CanonicalPath      string                           `json:"canonicalPath"`
	Signal             *horizonmodels.SignalDescriptor  `json:"signal,omitempty"`
	SeedFragmentIDs    []string                         `json:"seedFragmentIds,omitempty"`
}

type PropagationSignalSummary struct {
	FactID             string                                     `json:"factId,omitempty"`
	ProposalID         string                                     `json:"proposalId,omitempty"`
	CanonicalPath      string                                     `json:"canonicalPath,omitempty"`
	Simulation         *horizonmodels.SimulationResult            `json:"simulation,omitempty"`
	Raw                *propagation.SignalSimulationResult        `json:"raw,omitempty"`
	EscapedEndpoints   []horizonmodels.NodeImpact                 `json:"escapedEndpoints,omitempty"`
	TopEndpoints       []horizonmodels.NodeImpact                 `json:"topEndpoints,omitempty"`
	Diagnostics        *PropagationDiagnosticsSummary             `json:"diagnostics,omitempty"`
}

type PropagationAnalysisSummary struct {
	Signals              []*PropagationSignalSummary `json:"signals,omitempty"`
	MeaningfulFarEnds    int                         `json:"meaningfulFarEnds"`
	NonSeedEndpoints     int                         `json:"nonSeedEndpoints"`
	TraceWithEdges       int                         `json:"traceWithEdges"`
}

type NodeInterpretationSummary struct {
	FragmentID      string                              `json:"fragmentId"`
	CanonicalPath   string                              `json:"canonicalPath"`
	Status          string                              `json:"status"`
	Reason          string                              `json:"reason,omitempty"`
	Interpretation  *horizoninterp.RiskInterpretation   `json:"interpretation,omitempty"`
	SupportingEvidence []string                         `json:"supportingEvidence,omitempty"`
}

type InterpretationSummary struct {
	SeedInterpretation      *NodeInterpretationSummary   `json:"seedInterpretation,omitempty"`
	EndpointInterpretations []*NodeInterpretationSummary `json:"endpointInterpretations,omitempty"`
}

type StageVerdict struct {
	Name   string         `json:"name"`
	Status string         `json:"status"`
	Reason string         `json:"reason,omitempty"`
	Stats  map[string]any `json:"stats,omitempty"`
}

type FinalVerdict struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type CandidateScanSummary struct {
	ScannedFacts       int      `json:"scannedFacts"`
	CandidateSignals   int      `json:"candidateSignals"`
	WinningFactID      string   `json:"winningFactId,omitempty"`
	WinningCanonicalPath string `json:"winningCanonicalPath,omitempty"`
	WinningReason      string   `json:"winningReason,omitempty"`
	TopCandidatePaths  []string `json:"topCandidatePaths,omitempty"`
}

type PropagationDiagnosticsSummary struct {
	Axis            string                 `json:"axis,omitempty"`
	Profile         map[string]any         `json:"profile,omitempty"`
	ExpandedSeedIDs []string               `json:"expandedSeedIds,omitempty"`
	SeedDiagnostics []*SeedGraphDiagnostic `json:"seedDiagnostics,omitempty"`
}

type SeedGraphDiagnostic struct {
	FragmentID            string   `json:"fragmentId"`
	CanonicalPath         string   `json:"canonicalPath,omitempty"`
	Type                  string   `json:"type,omitempty"`
	OutgoingEdges         int      `json:"outgoingEdges"`
	IncomingEdges         int      `json:"incomingEdges"`
	StrongOutgoingEdges   int      `json:"strongOutgoingEdges"`
	StrongIncomingEdges   int      `json:"strongIncomingEdges"`
	StrongNeighborCount   int      `json:"strongNeighborCount"`
	MatchingOutgoingTypes []string `json:"matchingOutgoingTypes,omitempty"`
	MatchingIncomingTypes []string `json:"matchingIncomingTypes,omitempty"`
	StrongNeighborPaths   []string `json:"strongNeighborPaths,omitempty"`
}
