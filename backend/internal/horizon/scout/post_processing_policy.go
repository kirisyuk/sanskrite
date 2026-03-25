package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// PostProcessingStep identifies a discrete step in the entity scouting pipeline
// so that PostProcessingPolicy can gate it centrally.
type PostProcessingStep int

const (
	StepSourcePlanning PostProcessingStep = iota
	StepResearch
	StepCritic
	StepReValidation
	StepMetrics
	StepArtifactMaterialization
	StepCascadeChildScheduling
	StepRelationChildScheduling
	StepDomainExpandPhase
)

// ResearchMode controls HOW research runs (not IF).
type ResearchMode int

const (
	ResearchModeDomainHolistic     ResearchMode = iota // Level 1: domainMode + originalQuery
	ResearchModeQueryContextual                        // Level 2: originalQuery, domainMode is general
	ResearchModeEntityTypeHolistic                     // Level 3: no context, entity-type-specific
	ResearchModeGapDriven                              // Level 4: entity well-known, specific gaps
	ResearchModeSkip                                   // Tier 2+ or planModeResearchDone
)

// PostProcessingPolicy is the unified decision point that gates every post-processing
// step for both entity-scouting and domain-expansion job paths.
type PostProcessingPolicy struct {
	PipelineTier                 int
	RecursionDepth               int
	MaxRecursionDepth            int
	PlanModeResearchDone         bool
	HasDomainExpansionPeer       bool
	IsDomainExpansionJob         bool
	CascadeBudgetScale           float64
	DomainModes                  []string
	OriginalQuery                string
	EpistemicStatus              models.EpistemicStatus
	CriticGapCount               int
	CriticalOpenCount            int
	EvidenceCoverage             float64
	RelationChildSchedulingOptIn bool // Scout-it from modal: user explicitly requested hop depth
	TargetIsMarket               bool // Target path is /market/... — allow relation children in domain expansion
}

func stateCollectionLen(raw interface{}) int {
	switch t := raw.(type) {
	case []string:
		return len(t)
	case []interface{}:
		return len(t)
	case map[string]interface{}:
		return len(t)
	default:
		return 0
	}
}

func deriveHasDomainExpansionPeer(job *models.ScoutingJob, isDomainExpansionJob bool) bool {
	if job == nil || isDomainExpansionJob {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/domain/") {
		return false
	}
	if job.State == nil {
		return false
	}
	if raw, ok := job.State["domains"]; ok && stateCollectionLen(raw) > 0 {
		return true
	}
	if raw, ok := job.State["domainModes"]; ok {
		modes := taxonomy.NormalizeDomainModes(parseStateStringSlice(raw))
		for _, m := range modes {
			if strings.TrimSpace(m) != "" && strings.TrimSpace(m) != taxonomy.DomainModeGeneral {
				return true
			}
		}
	}
	if mode := strings.TrimSpace(parseStateString(job.State["domainMode"])); mode != "" && mode != taxonomy.DomainModeGeneral {
		return true
	}
	return false
}

// NewPostProcessingPolicy builds a policy from a ScoutingJob's state.
func NewPostProcessingPolicy(job *models.ScoutingJob) *PostProcessingPolicy {
	if job == nil {
		return &PostProcessingPolicy{}
	}

	recursionDepth := 0
	maxRecursionDepth := 2
	planModeResearchDone := false
	isDomainExpansionJob := false
	originalQuery := ""
	criticalOpenCount := 0
	evidenceCoverage := 0.0
	var domainModes []string

	if job.State != nil {
		recursionDepth = parseStateInt(job.State["recursionDepth"], 0)
		maxRecursionDepth = parseStateInt(job.State["maxRecursionDepth"], 2)
		planModeResearchDone = parseStateBool(job.State["planModeResearchDone"], false)
		isDomainExpansionJob = parseStateBool(job.State["isDomainExpansionJob"], false)
		if v, ok := job.State["originalQuery"].(string); ok {
			originalQuery = strings.TrimSpace(v)
		}
		if passRaw := parseStateString(job.State[pipeline.StateKeyPipelinePass]); passRaw != "" {
			if passName, ok := normalizePipelinePassName(passRaw); ok &&
				(passName == pipeline.PassBaseEnrichment || passName == pipeline.PassStrategicProjection) {
				isDomainExpansionJob = true
			}
		}
		criticalOpenCount = parseStateInt(job.State["critical_open_count"], parseStateInt(job.State["criticalOpenCount"], 0))
		evidenceCoverage = parseStateFloatLoose(job.State["evidence_coverage"], parseStateFloatLoose(job.State["evidenceCoverage"], 0))
		if evidenceCoverage < 0 {
			evidenceCoverage = 0
		}
		if evidenceCoverage > 1 {
			evidenceCoverage = 1
		}
		domainModes = taxonomy.NormalizeDomainModes(getJobDomainModes(job))
	}
	if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/domain/") {
		isDomainExpansionJob = true
	}
	hasDomainExpansionPeer := deriveHasDomainExpansionPeer(job, isDomainExpansionJob)
	relationChildSchedulingOptIn := false
	if job.State != nil {
		relationChildSchedulingOptIn = parseStateBool(job.State["relationChildSchedulingOptIn"], false)
	}
	targetIsMarket := strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/")

	tier := 0
	if recursionDepth == 1 {
		tier = 1
	}
	if recursionDepth >= 2 {
		tier = 2
	}

	return &PostProcessingPolicy{
		PipelineTier:                 tier,
		RecursionDepth:               recursionDepth,
		MaxRecursionDepth:            maxRecursionDepth,
		PlanModeResearchDone:         planModeResearchDone,
		HasDomainExpansionPeer:       hasDomainExpansionPeer,
		IsDomainExpansionJob:         isDomainExpansionJob,
		DomainModes:                  domainModes,
		OriginalQuery:                originalQuery,
		CriticalOpenCount:            criticalOpenCount,
		EvidenceCoverage:             evidenceCoverage,
		RelationChildSchedulingOptIn: relationChildSchedulingOptIn,
		TargetIsMarket:               targetIsMarket,
	}
}

// CanSkipPlanning is a risk-aware gate for source planning skip.
// Tier-based skip is allowed only when critical gaps are closed and evidence coverage is sufficient.
func (p *PostProcessingPolicy) CanSkipPlanning() bool {
	if p == nil {
		return false
	}
	if p.PipelineTier < 2 {
		return false
	}
	if p.CriticalOpenCount > 0 {
		return false
	}
	minCoverage := 0.65
	if p.HasSpecificDomainMode() {
		minCoverage = 0.80
	}
	if p.EvidenceCoverage < minCoverage {
		return false
	}
	return true
}

// ShouldRun returns whether the given pipeline step should execute.
func (p *PostProcessingPolicy) ShouldRun(step PostProcessingStep) bool {
	if p == nil {
		return true
	}
	switch step {
	case StepSourcePlanning:
		return p.PipelineTier < 2

	case StepResearch:
		return p.PipelineTier < 2 && !p.PlanModeResearchDone

	case StepCritic:
		return p.PipelineTier <= 1

	case StepReValidation:
		return p.PipelineTier == 0

	case StepMetrics:
		return p.PipelineTier == 0

	case StepArtifactMaterialization:
		return true

	case StepCascadeChildScheduling:
		// Domain contracts: execute only within their internal logic, don't spawn entity jobs separately.
		if p.IsDomainExpansionJob {
			return false
		}
		return !p.HasDomainExpansionPeer

	case StepRelationChildScheduling:
		// Domain contracts: don't spawn entity jobs for discovered nodes.
		if p.IsDomainExpansionJob {
			return false
		}
		// Depth limit: block when past max. At max: block unless market target in domain expansion (one extra hop).
		if p.RecursionDepth > p.MaxRecursionDepth {
			return false
		}
		if p.RecursionDepth >= p.MaxRecursionDepth && !(p.HasDomainExpansionPeer && p.TargetIsMarket) {
			return false
		}
		// Scout-it from modal: user explicitly requested hop depth — allow relation children.
		if p.RelationChildSchedulingOptIn {
			return true
		}
		// Market targets in domain expansion: allow relation children so we explore participants, sub-markets.
		// Without this, markets find relations (Participates In, Has Market Segment) but never schedule child jobs.
		if p.HasDomainExpansionPeer && p.TargetIsMarket {
			return true
		}
		return !p.HasDomainExpansionPeer

	case StepDomainExpandPhase:
		return p.IsDomainExpansionJob

	default:
		return true
	}
}

// ResearchMode determines the research strategy level to use.
// When CriticGapCount > 0 and no domain/query context, prefer GapDriven so we use
// the critic's missing-info questions (ownership, subsidiaries, etc.) instead of
// generic EntityTypeHolistic which ignores them. This enables recursive scouting
// for "scout it" on entities like G42 where we need relations from web search.
func (p *PostProcessingPolicy) ResearchMode() ResearchMode {
	if p == nil {
		return ResearchModeGapDriven
	}
	if p.PipelineTier >= 2 || p.PlanModeResearchDone {
		return ResearchModeSkip
	}
	if p.EpistemicStatus == models.EpistemicStatusVerified && p.CriticGapCount < 3 && p.CriticGapCount > 0 {
		return ResearchModeGapDriven
	}

	hasSpecificDomain := false
	for _, m := range p.DomainModes {
		m = strings.TrimSpace(m)
		if m != "" && m != taxonomy.DomainModeGeneral {
			hasSpecificDomain = true
			break
		}
	}

	if hasSpecificDomain && p.OriginalQuery != "" {
		return ResearchModeDomainHolistic
	}
	if p.OriginalQuery != "" {
		return ResearchModeQueryContextual
	}
	// Scout-it on entity with no domain/query: use GapDriven when we have critic gaps
	// so research uses missing-info questions (e.g. ownership, subsidiaries) and
	// returns relations for recursive scouting. EntityTypeHolistic ignores gaps.
	if p.CriticGapCount > 0 {
		return ResearchModeGapDriven
	}
	return ResearchModeEntityTypeHolistic
}

// HasSpecificDomainMode returns true if any non-general domain mode is active.
func (p *PostProcessingPolicy) HasSpecificDomainMode() bool {
	for _, m := range p.DomainModes {
		m = strings.TrimSpace(m)
		if m != "" && m != taxonomy.DomainModeGeneral {
			return true
		}
	}
	return false
}

// PrimaryDomainMode returns the first specific (non-general) domain mode, or empty string.
func (p *PostProcessingPolicy) PrimaryDomainMode() string {
	for _, m := range p.DomainModes {
		m = strings.TrimSpace(m)
		if m != "" && m != taxonomy.DomainModeGeneral {
			return m
		}
	}
	return ""
}
