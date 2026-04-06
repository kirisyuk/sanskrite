package risk

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	horizonfeedback "github.com/igorkirisyuk/sanskrite/internal/horizon/feedback"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonsignals "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	logRiskPipeline = "HZN_GROUNDING [horizon.risk.pipeline]"
)

type PipelineConfig struct {
	MaxAttempts    int
	StageTimeout   time.Duration
	WorkerVer      string
	EnablePipeline bool
	EnableStage    map[horizonmodels.RiskStage]bool
}

func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		MaxAttempts:    5,
		StageTimeout:   90 * time.Second,
		WorkerVer:      "risk-pipeline-v1",
		EnablePipeline: true,
		EnableStage: map[horizonmodels.RiskStage]bool{
			horizonmodels.RiskStageIdentify:  true,
			horizonmodels.RiskStageEscalate:  true,
			horizonmodels.RiskStageApply:     true,
			horizonmodels.RiskStageSeed:      true,
			horizonmodels.RiskStagePropagate: true,
			horizonmodels.RiskStageRollup:    true,
			horizonmodels.RiskStageInterpret: true,
			horizonmodels.RiskStageAlert:     true,
		},
	}
}

type Pipeline struct {
	jobsRepo  *horizonrepo.RiskJobsRepository
	scoutRepo *horizonrepo.ScoutingJobsRepository
	telemetry *horizonrepo.HorizonTelemetryRepository

	factsRepo         *corerepo.OntologyFactsRepository
	fragmentsRepo     *horizonrepo.HorizonFragmentsRepository
	edgesRepo         *horizonrepo.DependencyEdgesRepository
	exposurePathsRepo *horizonrepo.ExposurePathsRepository
	landingSessions   *horizonrepo.SignalLandingSessionsRepository
	landingHits       *horizonrepo.SignalLandingHitsRepository
	landingSummaries  *horizonrepo.SignalLandingSummariesRepository

	propagationEngine *horizonprop.PropagationEngine
	profileLookup     *horizonprop.ProfileLookup
	appetiteResolver  *AppetiteResolver

	interpreter *horizoninterp.Interpreter
	feedback    horizonfeedback.Emitter
	realtimePub *realtime.Publisher

	config PipelineConfig
}

func NewPipeline(
	jobsRepo *horizonrepo.RiskJobsRepository,
	factsRepo *corerepo.OntologyFactsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	exposurePathsRepo *horizonrepo.ExposurePathsRepository,
	propagationEngine *horizonprop.PropagationEngine,
	profileLookup *horizonprop.ProfileLookup,
	appetiteResolver *AppetiteResolver,
	interpreter *horizoninterp.Interpreter,
	realtimePub *realtime.Publisher,
	cfg PipelineConfig,
) *Pipeline {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.StageTimeout <= 0 {
		cfg.StageTimeout = 90 * time.Second
	}
	if strings.TrimSpace(cfg.WorkerVer) == "" {
		cfg.WorkerVer = "risk-pipeline-v1"
	}
	if cfg.EnableStage == nil {
		cfg.EnableStage = DefaultPipelineConfig().EnableStage
	}
	return &Pipeline{
		jobsRepo:          jobsRepo,
		factsRepo:         factsRepo,
		fragmentsRepo:     fragmentsRepo,
		edgesRepo:         edgesRepo,
		exposurePathsRepo: exposurePathsRepo,
		propagationEngine: propagationEngine,
		profileLookup:     profileLookup,
		appetiteResolver:  appetiteResolver,
		interpreter:       interpreter,
		realtimePub:       realtimePub,
		config:            cfg,
	}
}

func (p *Pipeline) WithFeedbackEmitter(emitter horizonfeedback.Emitter) *Pipeline {
	p.feedback = emitter
	return p
}

func (p *Pipeline) WithScoutingJobsRepo(scoutRepo *horizonrepo.ScoutingJobsRepository) *Pipeline {
	p.scoutRepo = scoutRepo
	return p
}

func (p *Pipeline) WithTelemetryRepo(telemetry *horizonrepo.HorizonTelemetryRepository) *Pipeline {
	p.telemetry = telemetry
	return p
}

func (p *Pipeline) WithSignalLandingRepos(
	sessions *horizonrepo.SignalLandingSessionsRepository,
	hits *horizonrepo.SignalLandingHitsRepository,
	summaries *horizonrepo.SignalLandingSummariesRepository,
) *Pipeline {
	p.landingSessions = sessions
	p.landingHits = hits
	p.landingSummaries = summaries
	return p
}

func (p *Pipeline) RunJob(ctx context.Context, jobID string) (*horizonmodels.RiskJob, error) {
	if p == nil || p.jobsRepo == nil {
		return nil, fmt.Errorf("risk pipeline not initialized")
	}
	if !p.config.EnablePipeline {
		return nil, fmt.Errorf("risk pipeline disabled")
	}

	job, err := p.jobsRepo.Get(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("risk job not found")
	}
	p.logf(job, "", "info", "phase=run_start state=%s stage=%s attempt=%d", job.CurrentState, job.CurrentStage, job.Attempt)
	if isRiskTerminal(job.CurrentState) {
		p.logf(job, "", "info", "phase=run_skip_terminal state=%s stage=%s", job.CurrentState, job.CurrentStage)
		return job, nil
	}

	job.Attempt++
	if job.Attempt > p.config.MaxAttempts {
		job.CurrentState = horizonmodels.RiskJobStatePoisoned
		job.TerminalReason = string(horizonmodels.RiskReasonStageTimeout)
		if err := p.jobsRepo.Update(ctx, job); err != nil {
			return job, err
		}
		if err := p.publishJobState(ctx, "horizon.risk.job.failed", job); err != nil {
			return job, err
		}
		return job, fmt.Errorf("risk job exceeded max attempts")
	}

	if !horizonmodels.IsRiskTransitionAllowed(job.CurrentState, horizonmodels.RiskJobStateRunning) {
		return nil, fmt.Errorf("illegal state transition %s -> %s", job.CurrentState, horizonmodels.RiskJobStateRunning)
	}
	job.CurrentState = horizonmodels.RiskJobStateRunning
	if job.CurrentStage == "" {
		job.CurrentStage = horizonmodels.RiskStageIdentify
	}
	if err := p.jobsRepo.Update(ctx, job); err != nil {
		return nil, err
	}
	if err := p.publishJobState(ctx, "horizon.risk.job.state", job); err != nil {
		return nil, err
	}

	for !isRiskTerminal(job.CurrentState) && job.CurrentStage != horizonmodels.RiskStageTerminalEnd {
		stage := job.CurrentStage
		if !p.isStageEnabled(stage) {
			now := time.Now().UTC()
			job.StageHistory = append(job.StageHistory, horizonmodels.RiskStageResult{
				Stage:       stage,
				StartedAt:   now,
				FinishedAt:  now,
				Decision:    horizonmodels.RiskStageDecisionSkip,
				ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)},
				Attempt:     job.Attempt,
				WorkerVer:   p.config.WorkerVer,
			})
			job.CurrentStage = horizonmodels.NextRiskStage(stage)
			if err := p.jobsRepo.Update(ctx, job); err != nil {
				return job, err
			}
			p.logf(job, stage, "info", "phase=stage_skipped reason=%s", horizonmodels.RiskReasonStageSkippedNoSignals)
			continue
		}

		p.logf(job, stage, "info", "phase=stage_start")
		if err := p.publishStageEvent(ctx, "horizon.risk.stage.started", job, stage, nil); err != nil {
			return job, err
		}
		startedAt := time.Now().UTC()
		stageCtx, cancel := context.WithTimeout(ctx, p.config.StageTimeout)
		out, stageErr := p.execStage(stageCtx, job, stage)
		cancel()
		if out == nil {
			out = failStage(horizonmodels.RiskReasonDependencyUnavailable)
		}

		result := horizonmodels.RiskStageResult{
			Stage:       stage,
			StartedAt:   startedAt,
			FinishedAt:  time.Now().UTC(),
			Decision:    out.Decision,
			ReasonCodes: append([]string{}, out.ReasonCodes...),
			Attempt:     job.Attempt,
			WorkerVer:   p.config.WorkerVer,
		}
		job.StageHistory = append(job.StageHistory, result)
		if out.StatePatch != nil {
			mergeRiskState(job.State, out.StatePatch)
		}

		if stageErr != nil || out.Decision == horizonmodels.RiskStageDecisionFail {
			job.CurrentState = horizonmodels.RiskJobStateFailed
			if len(out.ReasonCodes) > 0 {
				job.TerminalReason = strings.TrimSpace(out.ReasonCodes[0])
			} else {
				job.TerminalReason = string(horizonmodels.RiskReasonDependencyUnavailable)
			}
			if job.Attempt >= p.config.MaxAttempts {
				job.CurrentState = horizonmodels.RiskJobStatePoisoned
			}
			if err := p.jobsRepo.Update(ctx, job); err != nil {
				return job, err
			}
			p.logf(job, stage, "error", "phase=stage_failed decision=%s reason=%s err=%v", out.Decision, job.TerminalReason, stageErr)
			p.emitGroundingErrorTelemetry(ctx, job, "run.stage_failed", strings.TrimSpace(job.TerminalReason), "stage_execution_error", false)
			if err := p.publishStageEvent(ctx, "horizon.risk.stage.failed", job, stage, out.StatePatch); err != nil {
				return job, err
			}
			if err := p.publishJobState(ctx, "horizon.risk.job.failed", job); err != nil {
				return job, err
			}
			if stageErr != nil {
				return job, stageErr
			}
			return job, fmt.Errorf("stage %s failed", stage)
		}

		job.CurrentStage = horizonmodels.NextRiskStage(stage)
		if job.CurrentStage == horizonmodels.RiskStageTerminalEnd {
			job.CurrentState = horizonmodels.RiskJobStateCompleted
		}
		if err := p.jobsRepo.Update(ctx, job); err != nil {
			return job, err
		}
		p.logf(job, stage, "info", "phase=stage_completed decision=%s", out.Decision)
		if err := p.publishStageEvent(ctx, "horizon.risk.stage.completed", job, stage, out.StatePatch); err != nil {
			return job, err
		}
		if err := p.publishJobState(ctx, "horizon.risk.job.state", job); err != nil {
			return job, err
		}
	}

	if job.CurrentState == horizonmodels.RiskJobStateCompleted {
		p.logf(job, "", "info", "phase=run_done state=%s stage=%s attempt=%d", job.CurrentState, job.CurrentStage, job.Attempt)
		if err := p.publishJobState(ctx, "horizon.risk.job.completed", job); err != nil {
			return job, err
		}
	}
	return job, nil
}

type stageOutput struct {
	Decision    horizonmodels.RiskStageDecision
	ReasonCodes []string
	StatePatch  map[string]any
}

func (p *Pipeline) execStage(ctx context.Context, job *horizonmodels.RiskJob, stage horizonmodels.RiskStage) (*stageOutput, error) {
	switch stage {
	case horizonmodels.RiskStageIdentify:
		return p.stageIdentify(ctx, job)
	case horizonmodels.RiskStageEscalate:
		return p.stageEscalate(ctx, job)
	case horizonmodels.RiskStageApply:
		return p.stageApplicability(ctx, job)
	case horizonmodels.RiskStageSeed:
		return p.stageSeed(ctx, job)
	case horizonmodels.RiskStagePropagate:
		return p.stagePropagate(ctx, job)
	case horizonmodels.RiskStageRollup:
		return p.stageRollup(ctx, job)
	case horizonmodels.RiskStageInterpret:
		return p.stageInterpret(ctx, job)
	case horizonmodels.RiskStageAlert:
		return p.stageAlert(ctx, job)
	default:
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("unknown stage: %s", stage)
	}
}

func (p *Pipeline) stageIdentify(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.fragmentsRepo == nil || p.factsRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("identify deps unavailable")
	}
	var fragment *horizonmodels.HorizonFragment
	var err error
	identifyMode := "path"

	targetFragmentID := ""
	if riskIdentifyFragmentIDEnabled() {
		targetFragmentID = stateString(job.State, riskStateKeyTargetFragmentID)
		if targetFragmentID == "" {
			targetFragmentID = stateString(job.State, riskStateKeyFragmentID)
		}
	}
	if targetFragmentID != "" {
		fragment, err = p.fragmentsRepo.Get(ctx, targetFragmentID)
		if err != nil {
			p.logf(job, horizonmodels.RiskStageIdentify, "warn", "phase=identify_fragment_lookup_failed fragmentId=%s err=%v",
				strings.TrimSpace(targetFragmentID), err)
			p.emitGroundingErrorTelemetry(ctx, job, "identify.fragment_lookup", "identify_fragment_lookup_failed", "repo_read_error", true)
		} else if fragment != nil {
			identifyMode = "fragmentId"
		}
	}
	if fragment == nil {
		targetPath := normalizeRiskTargetPath(strings.TrimSpace(job.TargetCanonicalPath))
		if targetPath == "" {
			return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("missing targetCanonicalPath")
		}
		fragment, err = p.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		if (err != nil || fragment == nil) && targetPath != strings.TrimSpace(job.TargetCanonicalPath) {
			fragment, err = p.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(job.TargetCanonicalPath))
			if err == nil && fragment != nil {
				identifyMode = "fallback"
			}
		}
		if err != nil || fragment == nil {
			return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("target fragment not found: %w", err)
		}
	}

	facts, err := p.factsRepo.ListByCanonicalPathAndKind(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(fragment.CanonicalPath), "signal", 300)
	if err != nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
	}
	if len(facts) == 0 && strings.TrimSpace(job.TenantID) != "global" {
		facts, err = p.factsRepo.ListByCanonicalPathAndKind(ctx, "global", strings.TrimSpace(fragment.CanonicalPath), "signal", 300)
		if err != nil {
			return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
		}
	}

	signalIDs := make([]string, 0, len(facts))
	for _, f := range facts {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		if !factMatchesPolarity(f, job.Polarity) {
			continue
		}
		signalIDs = append(signalIDs, strings.TrimSpace(f.ID))
	}
	patch := map[string]any{
		riskStateKeyFragmentID:          strings.TrimSpace(fragment.ID),
		riskStateKeyTargetFragmentID:    strings.TrimSpace(fragment.ID),
		riskStateKeyTargetCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		"targetPath":                    strings.TrimSpace(fragment.CanonicalPath),
		riskStateKeySignalIDs:           signalIDs,
		riskStateKeySignalCount:         len(signalIDs),
		riskStateKeyIdentifiedAtUTC:     time.Now().UTC().Format(time.RFC3339Nano),
		riskStateKeyIdentifyMode:        strings.TrimSpace(identifyMode),
	}
	p.emitRiskCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_risk_identify_resolution_total", map[string]string{
		"mode": strings.TrimSpace(identifyMode),
	}, map[string]int64{"count": 1})
	if len(signalIDs) == 0 {
		refreshOutcome := "still_empty"
		if p.shouldTriggerScoutRefresh(job) {
			if refreshID, refreshErr := p.enqueueScoutRefresh(ctx, job, fragment); refreshErr != nil {
				p.logf(job, horizonmodels.RiskStageIdentify, "warn", "phase=identify_scout_refresh_enqueue_failed fragmentId=%s err=%v",
					strings.TrimSpace(fragment.ID), refreshErr)
				p.emitGroundingErrorTelemetry(ctx, job, "identify.enqueue_scout_refresh", "identify_scout_refresh_enqueue_failed", "enqueue_error", true)
			} else {
				refreshOutcome = "resolved"
				patch[riskStateKeyScoutRefreshMarker] = true
				patch[riskStateKeyScoutRefreshAtUTC] = time.Now().UTC().Format(time.RFC3339Nano)
				patch[riskStateKeyScoutRefreshDepth] = stateInt(job.State, riskStateKeyScoutRefreshDepth) + 1
				patch[riskStateKeyScoutRefreshReason] = "identify_no_signals"
				patch["scoutRefreshJobId"] = strings.TrimSpace(refreshID)
			}
		}
		p.emitRiskCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_risk_scout_refresh_total", map[string]string{
			"outcome": strings.TrimSpace(refreshOutcome),
		}, map[string]int64{"count": 1})
		return &stageOutput{
			Decision:    horizonmodels.RiskStageDecisionSkip,
			ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)},
			StatePatch:  patch,
		}, nil
	}
	return &stageOutput{Decision: horizonmodels.RiskStageDecisionPass, StatePatch: patch}, nil
}

func (p *Pipeline) stageEscalate(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	signalFacts, err := p.loadSignalFactsFromState(ctx, job)
	if err != nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
	}
	if len(signalFacts) == 0 {
		return &stageOutput{Decision: horizonmodels.RiskStageDecisionSkip, ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)}}, nil
	}

	maxLevel := horizonmodels.EscalationLevelNone
	maxLevelRank := escalationRank(maxLevel)
	maxConfidence := 0.0
	buckets := map[string]int{"intent": 0, "capability": 0, "preparation": 0, "corroboration": 0}

	for _, fact := range signalFacts {
		sig := signalDescriptorFromFact(fact)
		if rank := escalationRank(sig.Constraints.EscalationLevel); rank > maxLevelRank {
			maxLevel = sig.Constraints.EscalationLevel
			maxLevelRank = rank
		}
		if sig.Constraints.EscalationConfidence > maxConfidence {
			maxConfidence = sig.Constraints.EscalationConfidence
		}
		for k, v := range sig.Constraints.EvidenceBuckets {
			buckets[k] += v
		}
	}
	if maxLevel == "" {
		maxLevel = horizonmodels.EscalationLevelNone
	}
	return &stageOutput{
		Decision: horizonmodels.RiskStageDecisionPass,
		StatePatch: map[string]any{
			"escalationLevel":      maxLevel,
			"escalationConfidence": maxConfidence,
			"evidenceBuckets":      buckets,
		},
	}, nil
}

func (p *Pipeline) stageApplicability(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.fragmentsRepo == nil || p.edgesRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("applicability deps unavailable")
	}
	fragmentID := stateString(job.State, riskStateKeyFragmentID)
	if fragmentID == "" {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("missing %s in state", riskStateKeyFragmentID)
	}
	target, err := p.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || target == nil {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("target fragment not found: %w", err)
	}

	signalFacts, err := p.loadSignalFactsFromState(ctx, job)
	if err != nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
	}
	if len(signalFacts) == 0 {
		return &stageOutput{Decision: horizonmodels.RiskStageDecisionSkip, ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)}}, nil
	}

	evaluator := horizonsignals.NewExternalApplicabilityEvaluator().
		WithFragmentsRepo(p.fragmentsRepo).
		WithEdgesRepo(p.edgesRepo)
	if p.exposurePathsRepo != nil {
		evaluator = evaluator.WithExposurePathsRepo(p.exposurePathsRepo)
	}
	if p.landingSessions != nil {
		evaluator = evaluator.WithSignalLandingSessionsRepo(p.landingSessions)
	}
	if p.landingHits != nil {
		evaluator = evaluator.WithSignalLandingHitsRepo(p.landingHits)
	}
	if p.landingSummaries != nil {
		evaluator = evaluator.WithSignalLandingSummariesRepo(p.landingSummaries)
	}

	applicable := make([]string, 0, len(signalFacts))
	rejected := make([]string, 0, len(signalFacts))
	sparsePenalty := false
	for _, f := range signalFacts {
		sig := signalDescriptorFromFact(f)
		decision := evaluator.Evaluate(ctx, sig, target)
		if decision.ConfidencePenalty < 1.0 {
			sparsePenalty = true
		}
		if decision.Applicable {
			applicable = append(applicable, strings.TrimSpace(f.ID))
		} else {
			rejected = append(rejected, strings.TrimSpace(f.ID))
		}
	}

	if len(applicable) == 0 {
		return &stageOutput{
			Decision:    horizonmodels.RiskStageDecisionFail,
			ReasonCodes: []string{string(horizonmodels.RiskReasonNoExposurePath)},
			StatePatch: map[string]any{
				"applicableSignalIds": applicable,
				"rejectedSignalIds":   rejected,
			},
		}, nil
	}
	reasons := make([]string, 0, 1)
	if sparsePenalty {
		reasons = append(reasons, string(horizonmodels.RiskReasonSparseGraphPenalty))
	}
	return &stageOutput{
		Decision:    horizonmodels.RiskStageDecisionPass,
		ReasonCodes: reasons,
		StatePatch: map[string]any{
			"applicableSignalIds": applicable,
			"rejectedSignalIds":   rejected,
		},
	}, nil
}

func (p *Pipeline) stageSeed(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.fragmentsRepo == nil || p.edgesRepo == nil || p.factsRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("seed deps unavailable")
	}
	ids := stateStringSlice(job.State, "applicableSignalIds")
	if len(ids) == 0 {
		return &stageOutput{Decision: horizonmodels.RiskStageDecisionSkip, ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)}}, nil
	}

	seedInjector := horizonsignals.NewSeedInjector(p.fragmentsRepo, p.edgesRepo)
	seedBySignal := make(map[string][]string, len(ids))
	seedSet := map[string]struct{}{}
	stageErrs := make([]string, 0)
	for _, id := range ids {
		fact, err := p.factsRepo.Get(ctx, id)
		if err != nil {
			stageErrs = append(stageErrs, fmt.Sprintf("fact_get:%s:%v", strings.TrimSpace(id), err))
			continue
		}
		if fact == nil {
			stageErrs = append(stageErrs, fmt.Sprintf("fact_nil:%s", strings.TrimSpace(id)))
			continue
		}
		sig := signalDescriptorFromFact(fact)
		seeds, err := seedInjector.Inject(ctx, sig)
		if err != nil {
			stageErrs = append(stageErrs, fmt.Sprintf("seed_inject:%s:%v", strings.TrimSpace(id), err))
			continue
		}
		seedBySignal[id] = seeds
		for _, s := range seeds {
			s = strings.TrimSpace(s)
			if s != "" {
				seedSet[s] = struct{}{}
			}
		}
	}
	if len(stageErrs) > 0 {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("seed stage errors: %s", strings.Join(stageErrs, "; "))
	}

	allSeeds := make([]string, 0, len(seedSet))
	for s := range seedSet {
		allSeeds = append(allSeeds, s)
	}
	sort.Strings(allSeeds)
	if len(allSeeds) == 0 {
		return &stageOutput{
			Decision:    horizonmodels.RiskStageDecisionFail,
			ReasonCodes: []string{string(horizonmodels.RiskReasonStageInputInvalid)},
			StatePatch:  map[string]any{"seedBySignal": seedBySignal},
		}, nil
	}

	return &stageOutput{
		Decision: horizonmodels.RiskStageDecisionPass,
		StatePatch: map[string]any{
			"seedBySignal": seedBySignal,
			"seedIds":      allSeeds,
			"seedCount":    len(allSeeds),
		},
	}, nil
}

func (p *Pipeline) stagePropagate(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.propagationEngine == nil || p.profileLookup == nil || p.factsRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("propagation deps unavailable")
	}
	applicable := stateStringSlice(job.State, "applicableSignalIds")
	if len(applicable) == 0 {
		return &stageOutput{Decision: horizonmodels.RiskStageDecisionSkip, ReasonCodes: []string{string(horizonmodels.RiskReasonStageSkippedNoSignals)}}, nil
	}

	seedBySignal := stateStringSliceMap(job.State, "seedBySignal")
	affectedSet := map[string]struct{}{}
	pathCount := 0
	allPaths := make([][]string, 0, 32)
	stageErrs := make([]string, 0)

	for _, signalID := range applicable {
		fact, err := p.factsRepo.Get(ctx, signalID)
		if err != nil {
			stageErrs = append(stageErrs, fmt.Sprintf("fact_get:%s:%v", strings.TrimSpace(signalID), err))
			continue
		}
		if fact == nil {
			stageErrs = append(stageErrs, fmt.Sprintf("fact_nil:%s", strings.TrimSpace(signalID)))
			continue
		}
		sig := signalDescriptorFromFact(fact)
		seeds := seedBySignal[signalID]
		if len(seeds) == 0 {
			seeds = stateStringSlice(job.State, "seedIds")
		}
		result, err := p.propagationEngine.PropagateSignalFromSeeds(ctx, sig, p.profileLookup, seeds)
		if err != nil {
			stageErrs = append(stageErrs, fmt.Sprintf("propagate:%s:%v", strings.TrimSpace(signalID), err))
			continue
		}
		if result == nil {
			stageErrs = append(stageErrs, fmt.Sprintf("propagate_nil:%s", strings.TrimSpace(signalID)))
			continue
		}
		for _, id := range result.AffectedFragments {
			id = strings.TrimSpace(id)
			if id != "" {
				affectedSet[id] = struct{}{}
			}
		}
		for _, path := range result.PropagationPaths {
			if len(path) > 0 {
				allPaths = append(allPaths, path)
			}
		}
		pathCount += len(result.PropagationPaths)
	}
	if len(stageErrs) > 0 {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("propagation stage errors: %s", strings.Join(stageErrs, "; "))
	}

	affected := make([]string, 0, len(affectedSet))
	for id := range affectedSet {
		affected = append(affected, id)
	}
	sort.Strings(affected)

	proofs := p.buildProofOfImpact(ctx, affected, allPaths, applicable)

	return &stageOutput{
		Decision: horizonmodels.RiskStageDecisionPass,
		StatePatch: map[string]any{
			"affectedFragmentIds":  affected,
			"propagationPathCount": pathCount,
			"propagationPaths":     allPaths,
			"proofOfImpact":        proofs,
		},
	}, nil
}

func (p *Pipeline) buildProofOfImpact(ctx context.Context, affected []string, paths [][]string, sourceSignalIDs []string) []map[string]any {
	if p == nil || p.fragmentsRepo == nil || len(affected) == 0 {
		return nil
	}
	evidence := p.collectSignalEvidence(ctx, sourceSignalIDs)
	proofs := make([]map[string]any, 0, 16)
	for _, fid := range affected {
		frag, err := p.fragmentsRepo.Get(ctx, fid)
		if err != nil || frag == nil {
			continue
		}
		cp := strings.TrimSpace(frag.CanonicalPath)
		if !(frag.Type == horizonmodels.FragmentTypeOrganization || strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/")) {
			continue
		}
		entityPaths := make([][]string, 0, 3)
		for _, pth := range paths {
			if pathContainsFragment(pth, fid) {
				entityPaths = append(entityPaths, pth)
				if len(entityPaths) >= 3 {
					break
				}
			}
		}
		pathConfidence := estimateProofPathConfidence(entityPaths, len(sourceSignalIDs), evidence.SourceRefCount)
		uncertaintyFlags := make([]string, 0, 3)
		if len(entityPaths) == 0 {
			uncertaintyFlags = append(uncertaintyFlags, "no_direct_path_evidence")
		}
		if pathConfidence < 0.55 {
			uncertaintyFlags = append(uncertaintyFlags, "sparse_graph")
		}
		if evidence.Modality == horizonmodels.SignalModalityRhetoric || evidence.Modality == "" {
			uncertaintyFlags = append(uncertaintyFlags, "semantic_weak_mode")
		}
		proofs = append(proofs, map[string]any{
			"proofVersion":           "risk_proof_v2",
			"entityFragmentId":       strings.TrimSpace(frag.ID),
			"entityPath":             cp,
			"sourceSignalIds":        append([]string{}, sourceSignalIDs...),
			"sourceRefs":             evidence.SourceRefs,
			"transmissionPaths":      entityPaths,
			"transmissionMechanisms": evidence.Mechanisms,
			"pathConfidence":         pathConfidence,
			"signalSemantics": map[string]any{
				"likelihood":  evidence.Likelihood,
				"impact":      evidence.Impact,
				"modality":    evidence.Modality,
				"timeHorizon": evidence.TimeHorizon,
				"escalation":  evidence.Escalation,
			},
			"freshness": map[string]any{
				"latestObservedAtUtc": evidence.LatestObservedAt,
				"latestOccurredAtUtc": evidence.LatestOccurredAt,
				"latestFactUpdatedAt": evidence.LatestFactUpdatedAt,
			},
			"uncertaintyFlags": uncertaintyFlags,
		})
	}
	return proofs
}

type proofSignalEvidence struct {
	Likelihood          float64
	Impact              float64
	Modality            string
	TimeHorizon         string
	Escalation          string
	Mechanisms          []string
	SourceRefs          []map[string]string
	SourceRefCount      int
	LatestObservedAt    string
	LatestOccurredAt    string
	LatestFactUpdatedAt string
}

func (p *Pipeline) collectSignalEvidence(ctx context.Context, sourceSignalIDs []string) proofSignalEvidence {
	out := proofSignalEvidence{
		Likelihood:  0.0,
		Impact:      0.0,
		Modality:    horizonmodels.SignalModalityPotential,
		TimeHorizon: horizonmodels.SignalTimeHorizonShortTerm,
		Escalation:  horizonmodels.EscalationLevelNone,
		Mechanisms:  []string{},
		SourceRefs:  []map[string]string{},
	}
	if p == nil || p.factsRepo == nil || len(sourceSignalIDs) == 0 {
		return out
	}

	mechanismSet := map[string]struct{}{}
	sourceRefSeen := map[string]struct{}{}
	var latestObserved time.Time
	var latestOccurred time.Time
	var latestUpdated time.Time

	for _, signalID := range sourceSignalIDs {
		signalID = strings.TrimSpace(signalID)
		if signalID == "" {
			continue
		}
		fact, err := p.factsRepo.Get(ctx, signalID)
		if err != nil || fact == nil {
			continue
		}
		sig := signalDescriptorFromFact(fact)
		if sig.Constraints.Likelihood > out.Likelihood {
			out.Likelihood = sig.Constraints.Likelihood
		}
		if sig.Constraints.Impact > out.Impact {
			out.Impact = sig.Constraints.Impact
		}
		out.Modality = strongerSignalModality(out.Modality, sig.Constraints.Modality)
		out.TimeHorizon = strongerTimeHorizon(out.TimeHorizon, sig.Constraints.TimeHorizon)
		if escalationRank(sig.Constraints.EscalationLevel) > escalationRank(out.Escalation) {
			out.Escalation = strings.TrimSpace(sig.Constraints.EscalationLevel)
		}

		mechanism := strings.TrimSpace(sig.Axis)
		if mm := strings.TrimSpace(sig.Mechanism); mm != "" {
			mechanism = mechanism + ":" + mm
		}
		if mechanism != "" {
			mechanismSet[mechanism] = struct{}{}
		}

		if fact.ObservedAt != nil {
			if latestObserved.IsZero() || fact.ObservedAt.After(latestObserved) {
				latestObserved = fact.ObservedAt.UTC()
			}
		}
		if fact.OccurredAt != nil {
			if latestOccurred.IsZero() || fact.OccurredAt.After(latestOccurred) {
				latestOccurred = fact.OccurredAt.UTC()
			}
		}
		if latestUpdated.IsZero() || fact.UpdatedAt.After(latestUpdated) {
			latestUpdated = fact.UpdatedAt.UTC()
		}

		for _, sid := range fact.SourceIDs {
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			k := sid + "::" + coremodels.SourceTypeOntologyFact
			if _, ok := sourceRefSeen[k]; ok {
				continue
			}
			sourceRefSeen[k] = struct{}{}
			out.SourceRefs = append(out.SourceRefs, map[string]string{
				"sourceId":   sid,
				"sourceType": coremodels.SourceTypeOntologyFact,
			})
		}
		for _, ref := range fact.SourceRefs {
			sourceID := strings.TrimSpace(ref.SourceID)
			sourceType := strings.TrimSpace(ref.SourceType)
			if sourceID == "" {
				continue
			}
			if sourceType == "" {
				sourceType = coremodels.SourceTypeDocument
			}
			k := sourceID + "::" + sourceType
			if _, ok := sourceRefSeen[k]; ok {
				continue
			}
			sourceRefSeen[k] = struct{}{}
			out.SourceRefs = append(out.SourceRefs, map[string]string{
				"sourceId":   sourceID,
				"sourceType": sourceType,
			})
		}
	}

	for m := range mechanismSet {
		out.Mechanisms = append(out.Mechanisms, m)
	}
	sort.Strings(out.Mechanisms)
	sort.Slice(out.SourceRefs, func(i, j int) bool {
		if out.SourceRefs[i]["sourceType"] == out.SourceRefs[j]["sourceType"] {
			return out.SourceRefs[i]["sourceId"] < out.SourceRefs[j]["sourceId"]
		}
		return out.SourceRefs[i]["sourceType"] < out.SourceRefs[j]["sourceType"]
	})
	out.SourceRefCount = len(out.SourceRefs)
	if !latestObserved.IsZero() {
		out.LatestObservedAt = latestObserved.Format(time.RFC3339Nano)
	}
	if !latestOccurred.IsZero() {
		out.LatestOccurredAt = latestOccurred.Format(time.RFC3339Nano)
	}
	if !latestUpdated.IsZero() {
		out.LatestFactUpdatedAt = latestUpdated.Format(time.RFC3339Nano)
	}
	return out
}

func estimateProofPathConfidence(paths [][]string, signalCount, sourceRefCount int) float64 {
	if len(paths) == 0 {
		base := 0.35
		if signalCount >= 3 {
			base += 0.08
		}
		return clamp01(base)
	}
	totalHops := 0
	for _, p := range paths {
		if len(p) > 1 {
			totalHops += len(p) - 1
		}
	}
	avgHops := float64(totalHops) / float64(len(paths))
	score := 0.45 + 0.12*float64(minInt(len(paths), 3))
	score += 0.05 * float64(minInt(signalCount, 4))
	if sourceRefCount > 0 {
		score += 0.04
	}
	score -= 0.03 * avgHops
	return clamp01(score)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func pathContainsFragment(path []string, fragmentID string) bool {
	fragmentID = strings.TrimSpace(fragmentID)
	for _, id := range path {
		if strings.TrimSpace(id) == fragmentID {
			return true
		}
	}
	return false
}

func (p *Pipeline) stageRollup(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.fragmentsRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("rollup deps unavailable")
	}
	fragmentID := stateString(job.State, riskStateKeyFragmentID)
	if fragmentID == "" {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("missing %s", riskStateKeyFragmentID)
	}
	fragment, err := p.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || fragment == nil {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("fragment not found: %w", err)
	}

	polarity := strings.ToLower(strings.TrimSpace(job.Polarity))
	if polarity == "" {
		polarity = "risk"
	}
	prefix := "/metric/" + polarity + "/"
	bestMetric := ""
	bestValue := 0.0
	bestConf := 0.0
	for metricPath, mv := range fragment.State {
		if !strings.HasPrefix(strings.TrimSpace(metricPath), prefix) {
			continue
		}
		if mv.Value >= bestValue {
			bestMetric = strings.TrimSpace(metricPath)
			bestValue = mv.Value
			bestConf = mv.Confidence
		}
	}
	if bestMetric == "" {
		return &stageOutput{
			Decision:    horizonmodels.RiskStageDecisionSkip,
			ReasonCodes: []string{string(horizonmodels.RiskReasonRollupFallback)},
			StatePatch: map[string]any{
				"rollupMetricName": "",
				"rollupValue":      0.0,
				"rollupConfidence": 0.0,
				"tailMode":         false,
				"tailReason":       "rollup_fallback",
			},
		}, nil
	}

	likelihood := 0.0
	impact := bestValue
	modality := horizonmodels.SignalModalityPotential
	escalation := strings.TrimSpace(stateString(job.State, "escalationLevel"))
	signalFacts, err := p.loadSignalFactsFromState(ctx, job)
	if err != nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
	}
	for _, f := range signalFacts {
		s := signalDescriptorFromFact(f)
		if s.Constraints.Likelihood > likelihood {
			likelihood = s.Constraints.Likelihood
		}
		if s.Constraints.Impact > impact {
			impact = s.Constraints.Impact
		}
		modality = strongerSignalModality(modality, s.Constraints.Modality)
		if escalationRank(s.Constraints.EscalationLevel) > escalationRank(escalation) {
			escalation = strings.TrimSpace(s.Constraints.EscalationLevel)
		}
	}
	tail := assessRiskTailScenario(bestMetric, likelihood, impact, escalation, modality)
	proofCount := 0
	if arr, ok := job.State["proofOfImpact"].([]map[string]any); ok {
		proofCount = len(arr)
	} else if arrAny, ok := job.State["proofOfImpact"].([]any); ok {
		proofCount = len(arrAny)
	}

	return &stageOutput{
		Decision: horizonmodels.RiskStageDecisionPass,
		StatePatch: map[string]any{
			"rollupMetricName": bestMetric,
			"rollupValue":      bestValue,
			"rollupConfidence": bestConf,
			"tailLikelihood":   likelihood,
			"tailImpact":       impact,
			"tailMode":         tail.Enabled,
			"tailReason":       tail.Reason,
			"tailModality":     tail.Modality,
			"tailEscalation":   tail.Escalation,
			"proofCount":       proofCount,
		},
	}, nil
}

func (p *Pipeline) stageInterpret(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.interpreter == nil || p.fragmentsRepo == nil {
		return &stageOutput{Decision: horizonmodels.RiskStageDecisionSkip, ReasonCodes: []string{string(horizonmodels.RiskReasonDependencyUnavailable)}}, nil
	}
	fragmentID := stateString(job.State, riskStateKeyFragmentID)
	if fragmentID == "" {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("missing %s", riskStateKeyFragmentID)
	}
	fragment, err := p.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || fragment == nil {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("fragment not found: %w", err)
	}

	req := &horizoninterp.InterpretationRequest{
		FragmentID:    strings.TrimSpace(fragment.ID),
		CanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		NodeLabel:     strings.TrimSpace(fragment.Labels["en"]),
		Polarity:      strings.ToLower(strings.TrimSpace(job.Polarity)),
		Priority:      horizoninterp.TriggerPriorityHigh,
		TriggerType:   horizoninterp.TriggerTypeRapidChange,
		TriggerValue:  stateFloat(job.State, "rollupValue"),
		CreatedAt:     time.Now().UTC(),
		Status:        "processing",
	}
	out, err := p.interpreter.Interpret(ctx, req)
	if err != nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), err
	}
	if out == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("interpretation output is nil")
	}
	return &stageOutput{
		Decision: horizonmodels.RiskStageDecisionPass,
		StatePatch: map[string]any{
			"interpretationSummary":     strings.TrimSpace(out.Summary),
			"interpretationConfidence":  out.Confidence,
			"interpretationGeneratedAt": out.GeneratedAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

func (p *Pipeline) stageAlert(ctx context.Context, job *horizonmodels.RiskJob) (*stageOutput, error) {
	if p.fragmentsRepo == nil {
		return failStage(horizonmodels.RiskReasonDependencyUnavailable), fmt.Errorf("alert deps unavailable")
	}
	fragmentID := stateString(job.State, riskStateKeyFragmentID)
	if fragmentID == "" {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("missing %s", riskStateKeyFragmentID)
	}
	fragment, err := p.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || fragment == nil {
		return failStage(horizonmodels.RiskReasonStageInputInvalid), fmt.Errorf("fragment not found: %w", err)
	}

	threshold := 0.70
	neighbors := []string{}
	if p.appetiteResolver != nil {
		if profile, ns, err := p.appetiteResolver.ResolveForFragment(ctx, fragment, strings.ToLower(strings.TrimSpace(job.Polarity))); err == nil && profile != nil {
			threshold = profile.AggregateThreshold
			neighbors = ns
		}
	}
	baseThreshold := threshold
	rollupValue := stateFloat(job.State, "rollupValue")
	escalation := strings.TrimSpace(stateString(job.State, "escalationLevel"))
	tailMode := stateBool(job.State, "tailMode")
	tailLikelihood := clamp01(stateFloat(job.State, "tailLikelihood"))
	tailImpact := clamp01(stateFloat(job.State, "tailImpact"))
	tailAdjustedThreshold := 0.0
	if strings.ToLower(strings.TrimSpace(job.Polarity)) == "risk" && tailMode {
		adjusted := math.Max(0.35, threshold*0.75)
		if tailImpact >= 0.90 && tailLikelihood <= 0.45 {
			adjusted = math.Max(0.30, threshold*0.70)
		}
		threshold = adjusted
		tailAdjustedThreshold = adjusted
	}
	escalationGate := escalation == horizonmodels.EscalationLevelWarning || escalation == horizonmodels.EscalationLevelCritical
	if strings.ToLower(strings.TrimSpace(job.Polarity)) == "risk" && tailMode && tailImpact >= 0.92 {
		escalationGate = true
	}
	if strings.ToLower(strings.TrimSpace(job.Polarity)) == "opportunity" {
		escalationGate = true
	}
	dispatchReady := rollupValue >= threshold && escalationGate
	alertLevel := deriveAlertLevel(dispatchReady, rollupValue, escalation)
	reasons := []string{}
	if !dispatchReady {
		reasons = append(reasons, string(horizonmodels.RiskReasonAlertSuppressed))
	}
	proofs := annotateProofDispatch(job.State, dispatchReady, alertLevel)
	return &stageOutput{
		Decision:    horizonmodels.RiskStageDecisionPass,
		ReasonCodes: reasons,
		StatePatch: map[string]any{
			"appetiteThreshold":     threshold,
			"appetiteBaseThreshold": baseThreshold,
			"tailAdjustedThreshold": tailAdjustedThreshold,
			"appetiteNeighbors":     neighbors,
			"dispatchReady":         dispatchReady,
			"alertLevel":            alertLevel,
			"proofOfImpact":         proofs,
		},
	}, nil
}

func deriveAlertLevel(dispatchReady bool, rollupValue float64, escalation string) string {
	if !dispatchReady {
		return "none"
	}
	if strings.TrimSpace(escalation) == horizonmodels.EscalationLevelCritical || rollupValue >= 0.90 {
		return "critical"
	}
	if strings.TrimSpace(escalation) == horizonmodels.EscalationLevelWarning || rollupValue >= 0.75 {
		return "warning"
	}
	return "watch"
}

func (p *Pipeline) loadSignalFactsFromState(ctx context.Context, job *horizonmodels.RiskJob) ([]*coremodels.OntologyFact, error) {
	if p == nil {
		return nil, fmt.Errorf("pipeline is nil")
	}
	if p.factsRepo == nil {
		return nil, fmt.Errorf("facts repo is nil")
	}
	if job == nil {
		return nil, fmt.Errorf("risk job is nil")
	}
	ids := stateStringSlice(job.State, "applicableSignalIds")
	if len(ids) == 0 {
		ids = stateStringSlice(job.State, riskStateKeySignalIDs)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]*coremodels.OntologyFact, 0, len(ids))
	errs := make([]string, 0)
	for _, id := range ids {
		fact, err := p.factsRepo.Get(ctx, strings.TrimSpace(id))
		if err != nil {
			errs = append(errs, fmt.Sprintf("fact_get:%s:%v", strings.TrimSpace(id), err))
			continue
		}
		if fact == nil {
			errs = append(errs, fmt.Sprintf("fact_nil:%s", strings.TrimSpace(id)))
			continue
		}
		out = append(out, fact)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("load signal facts errors: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

func (p *Pipeline) publishJobState(ctx context.Context, eventType string, job *horizonmodels.RiskJob) error {
	if p == nil || p.realtimePub == nil || job == nil {
		return nil
	}
	payload := map[string]any{
		"schemaVersion":       "risk.ws.v1",
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"polarity":            strings.TrimSpace(job.Polarity),
		"state":               string(job.CurrentState),
		"stage":               string(job.CurrentStage),
		"attempt":             job.Attempt,
		"terminalReason":      strings.TrimSpace(job.TerminalReason),
		"ts":                  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.realtimePub.PublishJSON(ctx, realtime.EventType(eventType), strings.TrimSpace(job.TenantID), payload); err != nil {
		p.logf(job, "", "error", "phase=ws_publish_failed event=%s err=%v", eventType, err)
		p.emitGroundingErrorTelemetry(ctx, job, "publish.job_state", "ws_publish_failed", "realtime_publish_error", false)
		return err
	}
	return nil
}

func (p *Pipeline) publishStageEvent(ctx context.Context, eventType string, job *horizonmodels.RiskJob, stage horizonmodels.RiskStage, payload map[string]any) error {
	if p == nil || p.realtimePub == nil || job == nil {
		return nil
	}
	out := map[string]any{
		"schemaVersion":       "risk.ws.v1",
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"polarity":            strings.TrimSpace(job.Polarity),
		"stage":               string(stage),
		"ts":                  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if payload != nil {
		out["payload"] = payload
	}
	if err := p.realtimePub.PublishJSON(ctx, realtime.EventType(eventType), strings.TrimSpace(job.TenantID), out); err != nil {
		p.logf(job, stage, "error", "phase=ws_publish_failed event=%s err=%v", eventType, err)
		p.emitGroundingErrorTelemetry(ctx, job, "publish.stage_event", "ws_publish_failed", "realtime_publish_error", false)
		return err
	}
	return nil
}

func (p *Pipeline) logf(job *horizonmodels.RiskJob, stage horizonmodels.RiskStage, level string, format string, args ...any) {
	if p == nil {
		return
	}
	tenantID := ""
	projectID := ""
	lensID := ""
	jobID := ""
	target := ""
	attempt := 0
	if job != nil {
		tenantID = strings.TrimSpace(job.TenantID)
		projectID = strings.TrimSpace(job.ProjectID)
		lensID = strings.TrimSpace(job.LensID)
		jobID = strings.TrimSpace(job.ID)
		target = strings.TrimSpace(job.TargetCanonicalPath)
		attempt = job.Attempt
	}
	stageStr := strings.TrimSpace(string(stage))
	if stageStr == "" && job != nil {
		stageStr = strings.TrimSpace(string(job.CurrentStage))
	}
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s level=%s tenant=%s project=%s lens=%s jobId=%s stage=%s attempt=%d target=%s %s",
		logRiskPipeline, strings.TrimSpace(level), tenantID, projectID, lensID, jobID, stageStr, attempt, target, msg)
}

func (p *Pipeline) isStageEnabled(stage horizonmodels.RiskStage) bool {
	if p == nil {
		return false
	}
	enabled, ok := p.config.EnableStage[stage]
	if !ok {
		return true
	}
	return enabled
}

func failStage(reason horizonmodels.RiskReasonCode) *stageOutput {
	return &stageOutput{Decision: horizonmodels.RiskStageDecisionFail, ReasonCodes: []string{string(reason)}}
}

func isRiskTerminal(s horizonmodels.RiskJobState) bool {
	return s == horizonmodels.RiskJobStateCompleted ||
		s == horizonmodels.RiskJobStateFailed ||
		s == horizonmodels.RiskJobStatePoisoned ||
		s == horizonmodels.RiskJobStateCancelled
}

func mergeRiskState(dst, patch map[string]any) {
	if dst == nil || patch == nil {
		return
	}
	for k, v := range patch {
		dst[k] = v
	}
}

func (p *Pipeline) emitRiskCounter(ctx context.Context, tenantID, metric string, dimensions map[string]string, counters map[string]int64) {
	if p == nil || p.telemetry == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if err := p.telemetry.Increment(ctx, tenantID, "risk_pipeline", strings.TrimSpace(metric), time.Hour, dimensions, counters); err != nil {
		log.Printf("%s telemetry_increment_failed tenant=%s metric=%s err=%v", logRiskPipeline, tenantID, strings.TrimSpace(metric), err)
	}
}

func (p *Pipeline) emitGroundingErrorTelemetry(ctx context.Context, job *horizonmodels.RiskJob, phase, reasonCode, errorClass string, bestEffort bool) {
	if p == nil {
		return
	}
	tenantID := ""
	if job != nil {
		tenantID = strings.TrimSpace(job.TenantID)
		if job.State == nil {
			job.State = map[string]any{}
		}
		if s := strings.TrimSpace(phase); s != "" {
			job.State[riskStateKeyLastErrorPhase] = s
		}
		if s := strings.TrimSpace(reasonCode); s != "" {
			job.State[riskStateKeyLastErrorReasonCode] = s
		}
		if s := strings.TrimSpace(errorClass); s != "" {
			job.State[riskStateKeyLastErrorClass] = s
		}
		job.State[riskStateKeyLastErrorAtUTC] = time.Now().UTC().Format(time.RFC3339Nano)
		if bestEffort {
			job.State[riskStateKeyBestEffortCount] = stateInt(job.State, riskStateKeyBestEffortCount) + 1
		}
	}
	p.emitRiskCounter(ctx, tenantID, "horizon_grounding_errors_total", map[string]string{
		"component":   "risk_pipeline",
		"phase":       strings.TrimSpace(phase),
		"error_class": strings.TrimSpace(errorClass),
		"reason_code": strings.TrimSpace(reasonCode),
	}, map[string]int64{"count": 1})
	if bestEffort {
		p.emitRiskCounter(ctx, tenantID, "horizon_grounding_best_effort_total", map[string]string{
			"component":   "risk_pipeline",
			"phase":       strings.TrimSpace(phase),
			"reason_code": strings.TrimSpace(reasonCode),
		}, map[string]int64{"count": 1})
	}
}

func (p *Pipeline) shouldTriggerScoutRefresh(job *horizonmodels.RiskJob) bool {
	if p == nil || job == nil || p.scoutRepo == nil {
		return false
	}
	if !riskNoSignalsScoutRefreshEnabled() {
		return false
	}
	if stateBool(job.State, riskStateKeyScoutRefreshMarker) {
		return false
	}
	if stateInt(job.State, riskStateKeyScoutRefreshDepth) >= 1 {
		return false
	}
	lastAt := parseRFC3339Any(stateString(job.State, riskStateKeyScoutRefreshAtUTC))
	if lastAt.IsZero() {
		return true
	}
	cooldownMinutes := 180
	if raw := strings.TrimSpace(os.Getenv(envRiskScoutRefreshCooldownM)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cooldownMinutes = n
		}
	}
	return time.Since(lastAt) >= time.Duration(cooldownMinutes)*time.Minute
}

func (p *Pipeline) enqueueScoutRefresh(ctx context.Context, job *horizonmodels.RiskJob, fragment *horizonmodels.HorizonFragment) (string, error) {
	if p == nil || p.scoutRepo == nil {
		return "", fmt.Errorf("scout refresh repo unavailable")
	}
	if job == nil || fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
		return "", fmt.Errorf("invalid refresh target")
	}
	scoutJob := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(job.TenantID),
			ProjectID: strings.TrimSpace(job.ProjectID),
			Status:    "pending",
			AgentID:   "scout",
			Task: map[string]interface{}{
				"targetCanonicalPath": strings.TrimSpace(fragment.CanonicalPath),
				"scoutingStrategy":    string(horizonmodels.ScoutingStrategySnapshot),
			},
			State: map[string]interface{}{
				"targetCanonicalPath":           strings.TrimSpace(fragment.CanonicalPath),
				"targetFragmentId":              strings.TrimSpace(fragment.ID),
				"lensId":                        strings.TrimSpace(job.LensID),
				"currentState":                  string(horizonmodels.ScoutingJobStatePending),
				"triggerOrigin":                 "risk_identify_no_signals",
				"riskTriggeredScoutRefresh":     true,
				"riskScoutRefreshDepth":         stateInt(job.State, riskStateKeyScoutRefreshDepth) + 1,
				"skipRiskBackPropagation":       true,
				"riskParentJobId":               strings.TrimSpace(job.ID),
				"riskParentTargetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			},
		},
		LensID:              strings.TrimSpace(job.LensID),
		TargetCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			ProjectID:   strings.TrimSpace(job.ProjectID),
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}
	id, err := p.scoutRepo.Create(ctx, scoutJob)
	if err != nil {
		return "", err
	}
	p.logf(job, horizonmodels.RiskStageIdentify, "info", "phase=identify_scout_refresh_enqueued target=%s scoutJobId=%s fragmentId=%s",
		strings.TrimSpace(fragment.CanonicalPath), strings.TrimSpace(id), strings.TrimSpace(fragment.ID))
	return id, nil
}

func normalizeRiskTargetPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Keep domain path facet ordering deterministic.
	if strings.HasPrefix(path, "/domain/") {
		if norm, err := taxonomy.NormalizeDomainCanonicalPath(path); err == nil && strings.TrimSpace(norm) != "" {
			path = strings.TrimSpace(norm)
		}
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {
		lastIdx := len(parts) - 1
		leaf := strings.TrimSpace(parts[lastIdx])
		if leaf != "" {
			normalizedLeaf := taxonomy.Slugify(naming.NormalizeLegalName(strings.ReplaceAll(leaf, "-", " ")))
			if normalizedLeaf != "" {
				parts[lastIdx] = normalizedLeaf
				path = "/" + strings.Join(parts, "/")
			}
		}
	}
	if err := taxonomy.ValidateCanonicalPath(path); err == nil {
		return path
	}
	return strings.TrimSpace(path)
}

func parseRFC3339Any(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func stateInt(state map[string]any, key string) int {
	if state == nil {
		return 0
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return 0
	}
	switch t := raw.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		v, _ := strconv.Atoi(strings.TrimSpace(t))
		return v
	default:
		return 0
	}
}

func stateString(state map[string]any, key string) string {
	if state == nil {
		return ""
	}
	v, ok := state[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func stateStringSlice(state map[string]any, key string) []string {
	if state == nil {
		return nil
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return nil
	}
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	switch t := raw.(type) {
	case []string:
		for _, s := range t {
			add(s)
		}
	case []any:
		for _, it := range t {
			if s, ok := it.(string); ok {
				add(s)
			}
		}
	case string:
		for _, s := range strings.Split(t, ",") {
			add(s)
		}
	}
	return out
}

func stateStringSliceMap(state map[string]any, key string) map[string][]string {
	out := map[string][]string{}
	if state == nil {
		return out
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return out
	}
	switch m := raw.(type) {
	case map[string][]string:
		for k, v := range m {
			out[k] = append([]string{}, v...)
		}
	case map[string]any:
		for k, v := range m {
			switch arr := v.(type) {
			case []string:
				out[k] = append([]string{}, arr...)
			case []any:
				tmp := make([]string, 0, len(arr))
				for _, it := range arr {
					if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
						tmp = append(tmp, strings.TrimSpace(s))
					}
				}
				out[k] = tmp
			}
		}
	}
	return out
}

func stateFloat(state map[string]any, key string) float64 {
	if state == nil {
		return 0
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return 0
	}
	switch t := raw.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		v, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return v
	default:
		return 0
	}
}

func stateBool(state map[string]any, key string) bool {
	if state == nil {
		return false
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return false
	}
	switch t := raw.(type) {
	case bool:
		return t
	case string:
		t = strings.ToLower(strings.TrimSpace(t))
		return t == "1" || t == "true" || t == "yes"
	case float64:
		return t > 0
	case int:
		return t > 0
	default:
		return false
	}
}

func annotateProofDispatch(state map[string]any, dispatchReady bool, alertLevel string) []map[string]any {
	if state == nil {
		return nil
	}
	raw, ok := state["proofOfImpact"]
	if !ok || raw == nil {
		return nil
	}
	out := make([]map[string]any, 0, 8)
	appendProof := func(m map[string]any) {
		if m == nil {
			return
		}
		cp := make(map[string]any, len(m)+2)
		for k, v := range m {
			cp[k] = v
		}
		cp["dispatchReady"] = dispatchReady
		cp["alertLevel"] = alertLevel
		out = append(out, cp)
	}
	switch t := raw.(type) {
	case []map[string]any:
		for _, it := range t {
			appendProof(it)
		}
	case []any:
		for _, it := range t {
			if m, ok := it.(map[string]any); ok {
				appendProof(m)
			}
		}
	}
	return out
}

func escalationRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case horizonmodels.EscalationLevelCritical:
		return 3
	case horizonmodels.EscalationLevelWarning:
		return 2
	case horizonmodels.EscalationLevelWatch:
		return 1
	default:
		return 0
	}
}

func strongerSignalModality(a, b string) string {
	rank := func(v string) int {
		switch strings.TrimSpace(v) {
		case horizonmodels.SignalModalityRhetoric:
			return 0
		case horizonmodels.SignalModalityPotential:
			return 1
		case horizonmodels.SignalModalityProjected:
			return 2
		case horizonmodels.SignalModalityEscalation:
			return 3
		case horizonmodels.SignalModalityDisruption:
			return 4
		case horizonmodels.SignalModalityConfirmed:
			return 5
		case horizonmodels.SignalModalityActual:
			return 6
		default:
			return 1
		}
	}
	if rank(b) > rank(a) {
		return strings.TrimSpace(b)
	}
	return strings.TrimSpace(a)
}

func strongerTimeHorizon(a, b string) string {
	rank := func(v string) int {
		switch strings.TrimSpace(v) {
		case horizonmodels.SignalTimeHorizonImmediate:
			return 4
		case horizonmodels.SignalTimeHorizonShortTerm:
			return 3
		case horizonmodels.SignalTimeHorizonMediumTerm:
			return 2
		case horizonmodels.SignalTimeHorizonLongTerm:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return strings.TrimSpace(b)
	}
	return strings.TrimSpace(a)
}

type riskTailAssessment struct {
	Enabled    bool
	Likelihood float64
	Impact     float64
	Modality   string
	Escalation string
	Reason     string
}

func assessRiskTailScenario(metricPath string, likelihood, impact float64, escalation, modality string) riskTailAssessment {
	axis := axisFromMetricPathForTail(metricPath)
	likelihood = clamp01(likelihood)
	impact = clamp01(impact)
	modality = strings.TrimSpace(modality)
	if modality == "" {
		modality = horizonmodels.SignalModalityPotential
	}
	escalation = strings.TrimSpace(escalation)
	if escalation == "" {
		escalation = horizonmodels.EscalationLevelNone
	}
	if !isTailSensitiveAxis(axis) {
		return riskTailAssessment{
			Enabled:    false,
			Likelihood: likelihood,
			Impact:     impact,
			Modality:   modality,
			Escalation: escalation,
			Reason:     "axis_not_tail_sensitive",
		}
	}
	if impact >= 0.85 && likelihood <= 0.60 {
		return riskTailAssessment{Enabled: true, Likelihood: likelihood, Impact: impact, Modality: modality, Escalation: escalation, Reason: "low_probability_high_impact"}
	}
	if impact >= 0.92 {
		return riskTailAssessment{Enabled: true, Likelihood: likelihood, Impact: impact, Modality: modality, Escalation: escalation, Reason: "catastrophic_impact_guardrail"}
	}
	if (escalation == horizonmodels.EscalationLevelWarning || escalation == horizonmodels.EscalationLevelCritical) && impact >= 0.75 {
		return riskTailAssessment{Enabled: true, Likelihood: likelihood, Impact: impact, Modality: modality, Escalation: escalation, Reason: "escalation_and_high_impact"}
	}
	return riskTailAssessment{
		Enabled:    false,
		Likelihood: likelihood,
		Impact:     impact,
		Modality:   modality,
		Escalation: escalation,
		Reason:     "normal_mode",
	}
}

func axisFromMetricPathForTail(metricPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(metricPath), "/"), "/")
	if len(parts) < 4 || parts[0] != "metric" {
		return ""
	}
	return parts[2] + "/" + parts[3]
}

func isTailSensitiveAxis(axis string) bool {
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisGeoConflict,
		horizonmodels.AxisLogisticsCapacity,
		horizonmodels.AxisLogisticsLatency,
		horizonmodels.AxisPhysicalFlowSupply,
		horizonmodels.AxisOperationalContinuity,
		horizonmodels.AxisFinancialSettlement,
		horizonmodels.AxisLegalAccess:
		return true
	default:
		return false
	}
}

func signalDescriptorFromFact(f *coremodels.OntologyFact) *horizonmodels.SignalDescriptor {
	s := &horizonmodels.SignalDescriptor{
		ID:          strings.TrimSpace(f.ID),
		FactID:      eventIDValue(f),
		CreatedAt:   f.CreatedAt,
		Amplitude:   clamp01(parseSignalTagFloat(f.Tags, "signal_amplitude:", f.Confidence)),
		Confidence:  clamp01(f.Confidence),
		ObservedAt:  f.UpdatedAt,
		SeedPaths:   append([]string{}, f.CanonicalPaths...),
		ActorPaths:  append([]string{}, f.ActorPaths...),
		SpherePaths: append([]string{}, f.SpherePaths...),
		Constraints: horizonmodels.SignalConstraints{
			Impact:               clamp01(parseSignalTagFloat(f.Tags, "signal_impact:", -1)),
			Likelihood:           clamp01(parseSignalTagFloat(f.Tags, "signal_likelihood:", -1)),
			Modality:             parseSignalTagString(f.Tags, "signal_modality:"),
			TimeHorizon:          parseSignalTagString(f.Tags, "signal_horizon:"),
			Timeframe:            parseSignalTagString(f.Tags, "signal_horizon:"),
			EscalationLevel:      parseSignalTagString(f.Tags, "signal_escalation:"),
			EscalationConfidence: clamp01(parseSignalTagFloat(f.Tags, "signal_confidence:", f.Confidence)),
			EvidenceBuckets:      map[string]int{},
		},
	}
	if f.ObservedAt != nil {
		s.ObservedAt = *f.ObservedAt
	}
	if f.LocationPath != nil {
		s.LocationPath = strings.TrimSpace(*f.LocationPath)
	}

	parts := strings.Split(strings.TrimSpace(f.Type), ":")
	if len(parts) > 0 {
		s.Polarity = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		s.Axis = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		s.Mechanism = strings.TrimSpace(parts[2])
	}
	if s.Polarity == "" {
		s.Polarity = "risk"
	}
	if s.Axis == "" {
		s.Axis = horizonmodels.AxisInfoUncertainty
	}
	if s.Constraints.Impact < 0 {
		s.Constraints.Impact = s.Amplitude
	}
	if s.Constraints.Likelihood < 0 {
		s.Constraints.Likelihood = s.Confidence
	}
	if strings.TrimSpace(s.Constraints.Modality) == "" {
		s.Constraints.Modality = horizonmodels.SignalModalityPotential
	}
	if strings.TrimSpace(s.Constraints.TimeHorizon) == "" {
		s.Constraints.TimeHorizon = horizonmodels.SignalTimeHorizonShortTerm
		s.Constraints.Timeframe = horizonmodels.SignalTimeHorizonShortTerm
	}
	return s
}

func parseSignalTagFloat(tags []string, prefix string, fallback float64) float64 {
	prefix = strings.TrimSpace(prefix)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			v := strings.TrimSpace(strings.TrimPrefix(t, prefix))
			f, err := strconv.ParseFloat(v, 64)
			if err == nil {
				return f
			}
		}
	}
	return fallback
}

func parseSignalTagString(tags []string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix))
		}
	}
	return ""
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func eventIDValue(f *coremodels.OntologyFact) string {
	if f == nil || f.EventID == nil {
		return ""
	}
	return strings.TrimSpace(*f.EventID)
}

func factMatchesPolarity(f *coremodels.OntologyFact, polarity string) bool {
	if f == nil {
		return false
	}
	polarity = strings.ToLower(strings.TrimSpace(polarity))
	if polarity == "" {
		polarity = "risk"
	}
	if typ := strings.TrimSpace(f.Type); typ != "" {
		parts := strings.Split(typ, ":")
		if len(parts) > 0 && strings.ToLower(strings.TrimSpace(parts[0])) == polarity {
			return true
		}
	}
	for _, t := range f.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if strings.HasPrefix(t, "signal_polarity:") && strings.TrimSpace(strings.TrimPrefix(t, "signal_polarity:")) == polarity {
			return true
		}
	}
	return false
}

func init() {
	log.Printf("%s loaded", logRiskPipeline)
}
