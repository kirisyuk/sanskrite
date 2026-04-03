package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonqa "github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscopeagent "github.com/igorkirisyuk/sanskrite/internal/horizon/scopeagent"
	horizonsignal "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	propagationAnalysisTimeout    = 180 * time.Second
	interpretationAnalysisTimeout = 75 * time.Second
	perNodeInterpretationTimeout  = 25 * time.Second
)

type toolRuntime struct {
	fsClient          *gcp.FirestoreClient
	db                *firestore.Client
	opts              toolOptions
	aiExec            *ai.AITaskExecutor
	fragmentsRepo     *horizonrepo.HorizonFragmentsRepository
	targetsRepo       *horizonrepo.WatchTargetsRepository
	subjectsRepo      *horizonrepo.WatchSubjectsRepository
	triggersRepo      *horizonrepo.WatchTriggersRepository
	watchdogRepo      *horizonrepo.WatchdogConfigsRepository
	scopeConfigsRepo  *horizonrepo.ScopeAgentConfigsRepository
	scopeStatesRepo   *horizonrepo.ScopeAgentStatesRepository
	scopeRunsRepo     *horizonrepo.ScopeAgentRunsRepository
	proposalsRepo     *horizonrepo.AgentProposalsRepository
	decisionsRepo     *horizonrepo.ArbitrationDecisionsRepository
	briefsRepo        *horizonrepo.WatchBriefsRepository
	feedRepo          *horizonrepo.WatchFeedEntriesRepository
	lensesRepo        *horizonrepo.LensesRepository
	lensArtifactsRepo *horizonrepo.LensArtifactsRepository
	edgesRepo         *horizonrepo.DependencyEdgesRepository
	materializedRepo  *horizonrepo.MaterializedArtifactsRepository
	riskJobsRepo      *horizonrepo.RiskJobsRepository
	macroPoliciesRepo *horizonrepo.MacroAgentPoliciesRepository
	scoutingJobsRepo  *horizonrepo.ScoutingJobsRepository
	agentJobsRepo     *corerepo.AgentJobsRepository
	factsRepo         *corerepo.OntologyFactsRepository
	dataSourcesRepo   *corerepo.DataSourcesRepository
	propagationEngine *horizonprop.PropagationEngine
	profileLookup     *horizonprop.ProfileLookup
	interpreter       *horizoninterp.Interpreter
	leafEvidence      *horizonqa.LeafEvidenceCollector
	seedInjector      *horizonsignal.SeedInjector
	scopeAgentService *horizonscopeagent.Service
	watchRuntime      *horizonwatchruntime.Service
}

func newToolRuntime(ctx context.Context, opts toolOptions) (*toolRuntime, error) {
	fsClient, err := gcp.NewFirestoreClient(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	db := fsClient.Client()

	rt := &toolRuntime{
		fsClient:          fsClient,
		db:                db,
		opts:              opts,
		fragmentsRepo:     horizonrepo.NewHorizonFragmentsRepository(db),
		targetsRepo:       horizonrepo.NewWatchTargetsRepository(db),
		subjectsRepo:      horizonrepo.NewWatchSubjectsRepository(db),
		triggersRepo:      horizonrepo.NewWatchTriggersRepository(db),
		watchdogRepo:      horizonrepo.NewWatchdogConfigsRepository(db),
		scopeConfigsRepo:  horizonrepo.NewScopeAgentConfigsRepository(db),
		scopeStatesRepo:   horizonrepo.NewScopeAgentStatesRepository(db),
		scopeRunsRepo:     horizonrepo.NewScopeAgentRunsRepository(db),
		proposalsRepo:     horizonrepo.NewAgentProposalsRepository(db),
		decisionsRepo:     horizonrepo.NewArbitrationDecisionsRepository(db),
		briefsRepo:        horizonrepo.NewWatchBriefsRepository(db),
		feedRepo:          horizonrepo.NewWatchFeedEntriesRepository(db),
		lensesRepo:        horizonrepo.NewLensesRepository(db),
		lensArtifactsRepo: horizonrepo.NewLensArtifactsRepository(db),
		edgesRepo:         horizonrepo.NewDependencyEdgesRepository(db),
		materializedRepo:  horizonrepo.NewMaterializedArtifactsRepository(db),
		riskJobsRepo:      horizonrepo.NewRiskJobsRepository(db),
		macroPoliciesRepo: horizonrepo.NewMacroAgentPoliciesRepository(db),
		agentJobsRepo:     corerepo.NewAgentJobsRepository(db),
		factsRepo:         corerepo.NewOntologyFactsRepository(db),
		dataSourcesRepo:   corerepo.NewDataSourcesRepository(db),
	}
	rt.scoutingJobsRepo = horizonrepo.NewScoutingJobsRepository(rt.agentJobsRepo)

	rt.aiExec = initAIExecutor(ctx, opts.ProjectID)
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(db)
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(db)
	rt.profileLookup = horizonprop.NewProfileLookup(profilesRepo)
	if err := rt.profileLookup.Initialize(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ProfileLookup init warning: %v\n", err)
	}
	rt.propagationEngine = horizonprop.NewPropagationEngine(rt.fragmentsRepo, versionsRepo, rt.edgesRepo, rt.lensesRepo, nil).
		WithProfileLookup(rt.profileLookup)
	rt.seedInjector = horizonsignal.NewSeedInjector(rt.fragmentsRepo, rt.edgesRepo).WithMaterializedRepo(rt.materializedRepo)
	rt.interpreter = horizoninterp.NewInterpreter(
		rt.aiExec,
		rt.fragmentsRepo,
		horizoninterp.NewOntologySignalFactsRepo(rt.factsRepo),
	).WithEventFactsResolver(horizoninterp.NewOntologyEventFactsResolver(rt.factsRepo))
	rt.leafEvidence = horizonqa.NewLeafEvidenceCollector(horizoninterp.NewOntologyEventFactsResolver(rt.factsRepo))

	rt.scopeAgentService = horizonscopeagent.NewService(
		rt.scopeConfigsRepo,
		rt.scopeStatesRepo,
		rt.scopeRunsRepo,
		rt.proposalsRepo,
		rt.decisionsRepo,
		rt.targetsRepo,
		rt.subjectsRepo,
		rt.triggersRepo,
		rt.briefsRepo,
		rt.feedRepo,
		rt.agentJobsRepo,
		rt.watchdogRepo,
		rt.fragmentsRepo,
		rt.materializedRepo,
		rt.riskJobsRepo,
		rt.factsRepo,
		rt.dataSourcesRepo,
		rt.aiExec,
		rt.propagationEngine,
		rt.profileLookup,
	)

	rt.watchRuntime = horizonwatchruntime.NewService(
		rt.subjectsRepo,
		rt.triggersRepo,
		rt.targetsRepo,
		rt.watchdogRepo,
		rt.scopeConfigsRepo,
		rt.lensesRepo,
		rt.lensArtifactsRepo,
		rt.fragmentsRepo,
		rt.edgesRepo,
	).WithScopeAgentRunsRepository(rt.scopeRunsRepo).
		WithMaterializedArtifactsRepository(rt.materializedRepo).
		WithSignalLandingSummariesRepository(horizonrepo.NewSignalLandingSummariesRepository(rt.db)).
		WithMacroAgentPoliciesRepository(rt.macroPoliciesRepo).
		WithAgentProposalsRepository(rt.proposalsRepo).
		WithArbitrationDecisionsRepository(rt.decisionsRepo).
		WithWatchBriefsRepository(rt.briefsRepo).
		WithWatchFeedEntriesRepository(rt.feedRepo).
		WithRiskJobsRepository(rt.riskJobsRepo).
		WithScoutingJobsRepository(rt.scoutingJobsRepo).
		WithWakeHook(func(ctx context.Context, tenantID, subjectID, triggerType, triggerReason string, trace map[string]any) ([]string, error) {
			runs, err := rt.scopeAgentService.WakeBySubjectID(ctx, tenantID, subjectID, triggerType, triggerReason, trace)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(runs))
			for _, run := range runs {
				if run != nil && strings.TrimSpace(run.ID) != "" {
					ids = append(ids, strings.TrimSpace(run.ID))
				}
			}
			return ids, nil
		})
	rt.scopeAgentService.WithAcceptedProposalHook(func(ctx context.Context, proposal *horizonmodels.AgentProposal) ([]string, error) {
		if proposal == nil || strings.TrimSpace(proposal.ProposalType) != string(horizonmodels.AgentProposalTypeCoverageGap) {
			return nil, nil
		}
		subjectID := strings.TrimSpace(fmt.Sprint(proposal.Payload["watchSubjectId"]))
		if subjectID == "" {
			return nil, nil
		}
		trigger, _, err := rt.watchRuntime.EmitAndProcessTriggerForSubject(
			ctx,
			firstNonEmpty(proposal.TenantID, "global"),
			subjectID,
			horizonmodels.WatchTriggerPropagationGap,
			firstNonEmpty(proposal.Explanation, opts.TriggerReason),
			clamp01(proposal.Confidence),
			clamp01(proposal.Confidence),
			map[string]any{
				"proposalId":      strings.TrimSpace(proposal.ID),
				"headline":        firstNonEmpty(stringValue(proposal.Payload["title"]), "Coverage gap"),
				"summary":         firstNonEmpty(stringValue(proposal.Payload["summary"]), proposal.Explanation),
				"canonicalPaths":  proposal.TargetCanonicalPaths,
				"macroAgentClass": stringValue(proposal.Payload["macroAgentClass"]),
			},
		)
		if err != nil || trigger == nil {
			return nil, err
		}
		return []string{"watch_trigger:" + strings.TrimSpace(trigger.ID)}, nil
	})
	return rt, nil
}

func (rt *toolRuntime) close() {
	if rt != nil && rt.fsClient != nil {
		_ = rt.fsClient.Close()
	}
}

func (rt *toolRuntime) runLegacyTrigger(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	subject, cfg, err := rt.resolveSubjectAndConfig(ctx, opts)
	if err != nil {
		return nil, err
	}
	before, _ := rt.scopeRunsRepo.ListByConfigID(ctx, cfg.ID, 3)
	trigger, _, err := rt.watchRuntime.EmitAndProcessTriggerForSubject(
		ctx,
		subject.TenantID,
		subject.ID,
		horizonmodels.WatchTriggerFactDelta,
		firstNonEmpty(opts.TriggerReason, "macro trigger smoke"),
		0.87,
		0.94,
		map[string]any{
			"headline":       "Macro agent trigger smoke",
			"summary":        "Synthetic fact delta to validate watchruntime -> scopeagent wake path.",
			"canonicalPaths": []string{subject.AnchorCanonicalPath},
		},
	)
	if err != nil {
		return nil, err
	}
	after, _ := rt.scopeRunsRepo.ListByConfigID(ctx, cfg.ID, 5)
	return &E2EReport{
		GeneratedAt: time.Now().UTC(),
		Mode:        modeLegacyTrigger,
		Subject:     summarizeSubject(subject, nil),
		Runtime: &RuntimeSummary{
			TriggerID:       trigger.ID,
			TriggerDecision: string(trigger.Decision),
			TriggerReason:   trigger.DecisionReason,
			MaterialChange:  len(after) > len(before),
		},
		Runs: []*RunSummary{summarizeRun(newestRun(after))},
	}, nil
}

func (rt *toolRuntime) runLegacyCoverageGap(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	subject, cfg, err := rt.resolveSubjectAndConfig(ctx, opts)
	if err != nil {
		return nil, err
	}
	before, _ := rt.triggersRepo.ListBySubject(ctx, subject.ID, 5)
	proposal := &horizonmodels.AgentProposal{
		TenantID:             subject.TenantID,
		ConfigID:             cfg.ID,
		ProposalType:         string(horizonmodels.AgentProposalTypeCoverageGap),
		TargetCanonicalPaths: []string{subject.AnchorCanonicalPath},
		Confidence:           0.81,
		Explanation:          firstNonEmpty(opts.TriggerReason, "macro coverage gap smoke"),
		Payload: map[string]any{
			"title":           "Coverage gap smoke",
			"summary":         "Synthetic coverage gap to validate follow-up propagation gap trigger.",
			"watchSubjectId":  subject.ID,
			"macroAgentClass": stringValue(subject.Metadata["macroAgentClass"]),
		},
	}
	decision, err := rt.scopeAgentService.ArbitrateProposal(ctx, proposal)
	if err != nil {
		return nil, err
	}
	time.Sleep(500 * time.Millisecond)
	after, _ := rt.triggersRepo.ListBySubject(ctx, subject.ID, 10)
	return &E2EReport{
		GeneratedAt: time.Now().UTC(),
		Mode:        modeLegacyCoverageGap,
		Subject:     summarizeSubject(subject, nil),
		Proposals: []*ProposalSummary{{
			ID:               proposal.ID,
			ProposalType:     proposal.ProposalType,
			Confidence:       proposal.Confidence,
			Explanation:      proposal.Explanation,
			Decision:         decision.Decision,
			ReasonCodes:      decision.ReasonCodes,
			AppliedArtifacts: decision.AppliedArtifacts,
			Payload:          proposal.Payload,
			CreatedAt:        proposal.CreatedAt,
		}},
		Runtime: &RuntimeSummary{
			MaterialChange: len(after) > len(before),
		},
	}, nil
}

func (rt *toolRuntime) runFullE2E(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	report, err := rt.runGrounded(ctx, opts)
	if err != nil {
		return nil, err
	}
	return rt.completeAnalysis(ctx, report, opts)
}

func (rt *toolRuntime) runLensE2E(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	subject, cfg, lensSubjects, err := rt.resolveLensPrimarySubject(ctx, opts)
	if err != nil {
		return nil, err
	}
	report, err := rt.runGroundedForResolved(ctx, opts, subject, cfg)
	if err != nil {
		return nil, err
	}
	if report.Subject != nil {
		report.Subject.EscapedOutsideLens = summarizeLensCompanions(lensSubjects, subject.ID)
	}
	return rt.completeAnalysis(ctx, report, opts)
}

func (rt *toolRuntime) runGrounded(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	subject, cfg, err := rt.resolveSubjectAndConfig(ctx, opts)
	if err != nil {
		return nil, err
	}
	return rt.runGroundedForResolved(ctx, opts, subject, cfg)
}

func (rt *toolRuntime) runGroundedForResolved(ctx context.Context, opts toolOptions, subject *horizonmodels.WatchSubject, cfg *horizonmodels.ScopeAgentConfig) (*E2EReport, error) {
	if err := rt.applyRuntimeOverrides(ctx, subject, cfg, opts); err != nil {
		return nil, err
	}
	report := &E2EReport{
		GeneratedAt: time.Now().UTC(),
		Mode:        opts.Mode,
		Subject:     summarizeSubject(subject, rt.materializedSummaryFromSubject(ctx, subject)),
	}
	beforeRuns, _ := rt.scopeRunsRepo.ListByConfigID(ctx, cfg.ID, opts.Limit*2)
	startedAt := time.Now().UTC()
	if err := rt.resetDebounceWindow(ctx, cfg); err != nil {
		return nil, err
	}
	triggerReason := opts.TriggerReason
	if opts.QueryOverride != "" {
		triggerReason = strings.TrimSpace(triggerReason + " " + opts.QueryOverride)
	}
	trigger, _, err := rt.watchRuntime.EmitAndProcessTriggerForSubject(
		ctx,
		subject.TenantID,
		subject.ID,
		horizonmodels.WatchTriggerFactDelta,
		triggerReason,
		0.90,
		0.92,
		map[string]any{
			"headline":          "Grounded macro-agent E2E",
			"summary":           "Grounded live run initiated by horizon_smoke_macro_agents.",
			"canonicalPaths":    []string{subject.AnchorCanonicalPath},
			"queryOverride":     opts.QueryOverride,
			"semanticWatermark": startedAt.Format(time.RFC3339Nano),
		},
	)
	if err != nil {
		return nil, err
	}
	reports, err := rt.scopeAgentService.RunDueBatches(ctx, subject.TenantID, opts.Limit)
	if err != nil {
		return nil, err
	}
	afterRuns, _ := rt.scopeRunsRepo.ListByConfigID(ctx, cfg.ID, maxInt(opts.Limit*3, 12))
	freshRuns := filterFreshRuns(afterRuns, beforeRuns, startedAt)

	report.Runtime = &RuntimeSummary{
		TriggerID:       trigger.ID,
		TriggerDecision: string(trigger.Decision),
		TriggerReason:   trigger.DecisionReason,
		FreshExecution:  len(freshRuns) > 0,
		MaterialChange:  false,
	}
	if len(freshRuns) == 0 {
		report.Runtime.NoopReason = "no_fresh_scope_agent_run"
	}
	report.Queries = compactUnique(collectQueries(freshRuns))
	report.Runs = summarizeRuns(freshRuns)
	report.Runtime.RunIDs = collectRunIDs(freshRuns)
	report.Runtime.ReportRunIDs = collectReportRunIDs(reports)
	report.Runtime.MaterialChange = len(findProducedProposalIDs(reports)) > 0
	report.Proposals, report.AcceptedSignals, err = rt.collectAcceptedArtifacts(ctx, subject, freshRuns, reports)
	if err != nil {
		return nil, err
	}
	report.Grounding = summarizeGrounding(freshRuns, report.Proposals, subject, cfg)
	return report, nil
}

func (rt *toolRuntime) analyzeChain(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	report := &E2EReport{
		GeneratedAt: time.Now().UTC(),
		Mode:        modeAnalyzeChain,
	}
	subject, _, err := rt.resolveSubjectAndConfig(ctx, opts)
	if err == nil && subject != nil {
		report.Subject = summarizeSubject(subject, rt.materializedSummaryFromSubject(ctx, subject))
	}
	accepted, proposals, err := rt.collectAcceptedArtifactsForReplay(ctx, opts)
	if err != nil {
		return nil, err
	}
	report.Proposals = proposals
	report.AcceptedSignals = accepted
	return rt.completeAnalysis(ctx, report, opts)
}

func (rt *toolRuntime) replayReport(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	report, err := rt.analyzeChain(ctx, opts)
	if err != nil {
		return nil, err
	}
	report.Mode = modeReplayReport
	return report, nil
}

func (rt *toolRuntime) scanCandidates(ctx context.Context, opts toolOptions) (*E2EReport, error) {
	report := &E2EReport{
		GeneratedAt: time.Now().UTC(),
		Mode:        modeScanCandidates,
		Scan:        &CandidateScanSummary{},
	}
	facts, err := rt.factsRepo.ListByTenantAndKind(ctx, firstNonEmpty(opts.TenantID, "global"), "signal", maxInt(opts.ScanFacts, opts.Limit*4))
	if err != nil {
		return nil, err
	}
	report.Scan.ScannedFacts = len(facts)
	descriptorRepo := horizoninterp.NewOntologySignalFactsRepo(rt.factsRepo)
	candidates := make([]*AcceptedSignalSummary, 0, len(facts))
	seen := map[string]struct{}{}
	for _, fact := range facts {
		if fact == nil || strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Kind) != "signal" {
			continue
		}
		signal, _ := descriptorRepo.GetSignal(ctx, fact.ID)
		if signal == nil {
			continue
		}
		if !matchesSignalAxisPrefix(signal, opts.AxisPrefix) {
			continue
		}
		seedIDs, _ := rt.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
		for _, canonicalPath := range compactUnique(fact.CanonicalPaths) {
			key := strings.TrimSpace(fact.ID) + "|" + strings.TrimSpace(canonicalPath)
			if canonicalPath == "" {
				continue
			}
			if !matchesCanonicalContains(canonicalPath, opts.CanonicalContains) {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, &AcceptedSignalSummary{
				Fact:            fact,
				CanonicalPath:   canonicalPath,
				Signal:          signal,
				SeedFragmentIDs: compactUnique(seedIDs),
			})
		}
	}
	sortAcceptedSignalsForScan(candidates)
	report.Scan.CandidateSignals = len(candidates)
	for _, item := range candidates[:minInt(len(candidates), 8)] {
		if item == nil {
			continue
		}
		report.Scan.TopCandidatePaths = append(report.Scan.TopCandidatePaths, strings.TrimSpace(item.CanonicalPath))
	}
	if len(candidates) == 0 {
		report.Criteria = evaluateCriteria(report, opts)
		report.Verdict = finalizeVerdict(report)
		return report, nil
	}

	bestIdx := -1
	bestScore := -1
	bestReason := "no_candidate_scored"
	var bestPropagation *PropagationAnalysisSummary
	var bestSubject *SubjectSummary
	scanLimit := minInt(len(candidates), maxInt(opts.Limit*3, 12))
	for idx, item := range candidates[:scanLimit] {
		subject := rt.subjectSummaryForCanonicalPath(ctx, item.CanonicalPath)
		candidateCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		prop, err := rt.analyzePropagation(candidateCtx, subject, []*AcceptedSignalSummary{item}, opts)
		cancel()
		if err != nil {
			continue
		}
		score, reason := propagationCandidateScore(prop)
		if score > bestScore {
			bestIdx = idx
			bestScore = score
			bestReason = reason
			bestPropagation = prop
			bestSubject = subject
		}
		if prop != nil && (prop.MeaningfulFarEnds > 0 || prop.NonSeedEndpoints > 0) {
			bestIdx = idx
			bestScore = score
			bestReason = reason
			bestPropagation = prop
			bestSubject = subject
			break
		}
	}
	if bestIdx < 0 {
		bestIdx = 0
		bestReason = "fallback_first_candidate"
	}
	best := candidates[bestIdx]
	report.AcceptedSignals = []*AcceptedSignalSummary{best}
	report.Subject = bestSubject
	if report.Subject == nil {
		report.Subject = rt.subjectSummaryForCanonicalPath(ctx, best.CanonicalPath)
	}
	report.Scan.WinningFactID = safeFactID(best)
	report.Scan.WinningCanonicalPath = strings.TrimSpace(best.CanonicalPath)
	report.Scan.WinningReason = bestReason
	report.PropagationAnalysis = bestPropagation
	if opts.TopInterpretations > 0 && bestPropagation != nil {
		interpCtx, cancel := context.WithTimeout(ctx, interpretationAnalysisTimeout)
		interp, err := rt.analyzeInterpretation(interpCtx, bestPropagation, opts)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return nil, err
		}
		report.InterpretationAnalysis = interp
	}
	report.Criteria = evaluateCriteria(report, opts)
	report.Verdict = finalizeVerdict(report)
	return report, nil
}

func (rt *toolRuntime) completeAnalysis(ctx context.Context, report *E2EReport, opts toolOptions) (*E2EReport, error) {
	propCtx, cancelProp := context.WithTimeout(ctx, propagationAnalysisTimeout)
	defer cancelProp()
	prop, err := rt.analyzePropagation(propCtx, report.Subject, report.AcceptedSignals, opts)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	report.PropagationAnalysis = prop
	if err != nil && report.Runtime != nil {
		report.Runtime.NoopReason = compactUnique([]string{report.Runtime.NoopReason, "propagation_timeout"})[0]
	}
	interpCtx, cancelInterp := context.WithTimeout(ctx, interpretationAnalysisTimeout)
	defer cancelInterp()
	interp, err := rt.analyzeInterpretation(interpCtx, prop, opts)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	report.InterpretationAnalysis = interp
	if err != nil && report.Runtime != nil {
		report.Runtime.NoopReason = firstNonEmpty(report.Runtime.NoopReason, "interpretation_timeout")
	}
	report.Criteria = evaluateCriteria(report, opts)
	report.Verdict = finalizeVerdict(report)
	return report, nil
}

func (rt *toolRuntime) resolveSubjectAndConfig(ctx context.Context, opts toolOptions) (*horizonmodels.WatchSubject, *horizonmodels.ScopeAgentConfig, error) {
	if opts.SubjectID != "" {
		subject, err := rt.subjectsRepo.Get(ctx, opts.SubjectID)
		if err != nil {
			return nil, nil, err
		}
		configs, err := rt.scopeConfigsRepo.ListActiveBySubjectID(ctx, opts.TenantID, subject.ID)
		if err != nil || len(configs) == 0 {
			return nil, nil, fmt.Errorf("active config not found for subject %s", subject.ID)
		}
		return subject, configs[0], nil
	}
	if opts.CanonicalPath == "" {
		return nil, nil, fmt.Errorf("canonical-path or subject-id is required")
	}
	subjects, err := rt.subjectsRepo.ListByAnchorCanonicalPath(ctx, opts.TenantID, opts.CanonicalPath, 20)
	if err != nil {
		return nil, nil, err
	}
	for _, subject := range subjects {
		if subject == nil || strings.TrimSpace(subject.ID) == "" {
			continue
		}
		configs, err := rt.scopeConfigsRepo.ListActiveBySubjectID(ctx, opts.TenantID, subject.ID)
		if err == nil && len(configs) > 0 {
			return subject, configs[0], nil
		}
	}
	return nil, nil, fmt.Errorf("macro subject/config not found for %s", opts.CanonicalPath)
}

func (rt *toolRuntime) resolveLensPrimarySubject(ctx context.Context, opts toolOptions) (*horizonmodels.WatchSubject, *horizonmodels.ScopeAgentConfig, []*horizonmodels.WatchSubject, error) {
	if strings.TrimSpace(opts.LensID) == "" {
		return nil, nil, nil, fmt.Errorf("lens-id is required")
	}
	if err := rt.watchRuntime.SyncWatchSubjectsForLens(ctx, opts.LensID); err != nil {
		return nil, nil, nil, err
	}
	subjects, err := rt.subjectsRepo.ListByLens(ctx, opts.TenantID, opts.LensID, 50)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(subjects) == 0 {
		return nil, nil, nil, fmt.Errorf("no subjects found for lens %s", opts.LensID)
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjectScore(rt, ctx, subjects[i]) > subjectScore(rt, ctx, subjects[j])
	})
	for _, subject := range subjects {
		configs, err := rt.scopeConfigsRepo.ListActiveBySubjectID(ctx, opts.TenantID, subject.ID)
		if err == nil && len(configs) > 0 {
			return subject, configs[0], subjects, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("no active configs found for lens %s", opts.LensID)
}

func (rt *toolRuntime) applyRuntimeOverrides(ctx context.Context, subject *horizonmodels.WatchSubject, cfg *horizonmodels.ScopeAgentConfig, opts toolOptions) error {
	changedSubject := false
	changedCfg := false
	if opts.LookbackDays > 0 {
		if subject.Metadata == nil {
			subject.Metadata = map[string]any{}
		}
		subject.Metadata["timeWindowPolicy"] = map[string]any{"lookbackDays": opts.LookbackDays}
		changedSubject = true
		if cfg.Metadata == nil {
			cfg.Metadata = map[string]any{}
		}
		cfg.Metadata["sourceMixPolicy"] = map[string]any{"lookbackDays": opts.LookbackDays}
		changedCfg = true
	}
	if opts.QueryOverride != "" {
		if subject.Metadata == nil {
			subject.Metadata = map[string]any{}
		}
		subject.Metadata["queryOverrides"] = compactUnique([]string{opts.QueryOverride})
		changedSubject = true
		if cfg.Metadata == nil {
			cfg.Metadata = map[string]any{}
		}
		cfg.Metadata["queryOverrides"] = compactUnique(append(parseAnyStringSlice(cfg.Metadata["queryOverrides"]), opts.QueryOverride))
		changedCfg = true
	}
	if changedSubject {
		if err := rt.subjectsRepo.Upsert(ctx, subject); err != nil {
			return err
		}
	}
	if changedCfg {
		if err := rt.scopeConfigsRepo.Upsert(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (rt *toolRuntime) resetDebounceWindow(ctx context.Context, cfg *horizonmodels.ScopeAgentConfig) error {
	if rt == nil || rt.scopeStatesRepo == nil || cfg == nil || strings.TrimSpace(cfg.ID) == "" {
		return nil
	}
	state, err := rt.scopeStatesRepo.GetByConfigID(ctx, cfg.ID)
	if err != nil || state == nil {
		return nil
	}
	state.DebounceUntil = nil
	state.PendingWakeReasons = nil
	return rt.scopeStatesRepo.Upsert(ctx, state)
}

func (rt *toolRuntime) collectAcceptedArtifacts(
	ctx context.Context,
	subject *horizonmodels.WatchSubject,
	runs []*horizonmodels.ScopeAgentRun,
	reports any,
) ([]*ProposalSummary, []*AcceptedSignalSummary, error) {
	proposalIDs := collectProducedProposalIDs(runs)
	if len(proposalIDs) == 0 {
		proposalIDs = findProducedProposalIDsTyped(reports)
	}
	return rt.collectByProposalIDs(ctx, subject, proposalIDs)
}

func (rt *toolRuntime) collectAcceptedArtifactsForReplay(ctx context.Context, opts toolOptions) ([]*AcceptedSignalSummary, []*ProposalSummary, error) {
	proposalIDs := []string{}
	if opts.ProposalID != "" {
		proposalIDs = append(proposalIDs, opts.ProposalID)
	}
	if opts.FactID != "" {
		descriptorRepo := horizoninterp.NewOntologySignalFactsRepo(rt.factsRepo)
		if fact, err := rt.factsRepo.Get(ctx, opts.FactID); err == nil && fact != nil && strings.TrimSpace(fact.Kind) == "signal" {
			signal, _ := descriptorRepo.GetSignal(ctx, fact.ID)
			if !matchesSignalAxisPrefix(signal, opts.AxisPrefix) {
				return nil, nil, nil
			}
			seedIDs, _ := rt.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
			canonicalPath := firstNonEmpty(firstCanonicalPath(fact.CanonicalPaths), opts.CanonicalPath)
			if !matchesCanonicalContains(canonicalPath, opts.CanonicalContains) {
				return nil, nil, nil
			}
			return []*AcceptedSignalSummary{{
				Fact:            fact,
				CanonicalPath:   canonicalPath,
				Signal:          signal,
				SeedFragmentIDs: compactUnique(seedIDs),
			}}, nil, nil
		}
	}
	if opts.RunID != "" {
		run, err := rt.scopeRunsRepo.Get(ctx, opts.RunID)
		if err != nil {
			return nil, nil, err
		}
		proposalIDs = append(proposalIDs, run.ProducedProposalIDs...)
	}
	subject, cfg, _ := rt.resolveSubjectAndConfig(ctx, opts)
	if len(proposalIDs) == 0 && cfg != nil && strings.TrimSpace(cfg.WatchTargetID) != "" && rt.proposalsRepo != nil {
		if recent, err := rt.proposalsRepo.ListByWatchTarget(ctx, firstNonEmpty(cfg.TenantID, opts.TenantID, "global"), cfg.WatchTargetID, maxInt(opts.Limit*3, 24)); err == nil {
			for _, proposal := range recent {
				if proposal == nil || strings.TrimSpace(proposal.ID) == "" {
					continue
				}
				proposalIDs = append(proposalIDs, proposal.ID)
			}
		}
	}
	proposals, accepted, err := rt.collectByProposalIDs(ctx, subject, compactUnique(proposalIDs))
	if len(accepted) > 0 {
		filtered := make([]*AcceptedSignalSummary, 0, len(accepted))
		for _, item := range accepted {
			if item == nil {
				continue
			}
			if !matchesSignalAxisPrefix(item.Signal, opts.AxisPrefix) {
				continue
			}
			if !matchesCanonicalContains(item.CanonicalPath, opts.CanonicalContains) {
				continue
			}
			filtered = append(filtered, item)
		}
		accepted = filtered
	}
	if err == nil && len(accepted) == 0 {
		accepted = rt.loadRecentAcceptedSignals(ctx, subject, cfg, minInt(maxInt(opts.Limit*2, 4), 12))
	}
	return accepted, proposals, err
}

func (rt *toolRuntime) collectByProposalIDs(ctx context.Context, subject *horizonmodels.WatchSubject, proposalIDs []string) ([]*ProposalSummary, []*AcceptedSignalSummary, error) {
	proposals := make([]*ProposalSummary, 0, len(proposalIDs))
	acceptedSignals := make([]*AcceptedSignalSummary, 0, len(proposalIDs))
	descriptorRepo := horizoninterp.NewOntologySignalFactsRepo(rt.factsRepo)
	for _, proposalID := range compactUnique(proposalIDs) {
		proposal, err := rt.proposalsRepo.Get(ctx, proposalID)
		if err != nil || proposal == nil {
			continue
		}
		decisions, _ := rt.decisionsRepo.ListByProposalID(ctx, proposal.ID)
		decision := newestDecision(decisions)
		summary := summarizeProposal(proposal, decision)
		proposals = append(proposals, summary)
		if decision == nil || strings.TrimSpace(decision.Decision) != string(horizonmodels.ArbitrationDecisionAccept) {
			continue
		}
		for _, canonicalPath := range proposal.TargetCanonicalPaths {
			factID := factIDForProposal(proposal, canonicalPath)
			fact, err := rt.factsRepo.Get(ctx, factID)
			if err != nil || fact == nil || strings.TrimSpace(fact.Kind) != "signal" {
				continue
			}
			signal, _ := descriptorRepo.GetSignal(ctx, fact.ID)
			seedIDs, _ := rt.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
			acceptedSignals = append(acceptedSignals, &AcceptedSignalSummary{
				Fact:            fact,
				ProposalID:      proposal.ID,
				CanonicalPath:   canonicalPath,
				Signal:          signal,
				SeedFragmentIDs: compactUnique(seedIDs),
			})
		}
	}
	return proposals, dedupeAcceptedSignals(acceptedSignals), nil
}

func (rt *toolRuntime) loadRecentAcceptedSignals(ctx context.Context, subject *horizonmodels.WatchSubject, cfg *horizonmodels.ScopeAgentConfig, limit int) []*AcceptedSignalSummary {
	if rt == nil || rt.factsRepo == nil {
		return nil
	}
	canonicalPath := ""
	if subject != nil {
		canonicalPath = strings.TrimSpace(subject.AnchorCanonicalPath)
	}
	if canonicalPath == "" && cfg != nil {
		canonicalPath = strings.TrimSpace(cfg.ScopeCanonicalPath)
	}
	if canonicalPath == "" {
		return nil
	}
	facts, err := rt.factsRepo.ListByCanonicalPath(ctx, firstNonEmpty(cfgTenantID(cfg), "global"), canonicalPath, "", maxInt(limit, 1))
	if err != nil {
		return nil
	}
	descriptorRepo := horizoninterp.NewOntologySignalFactsRepo(rt.factsRepo)
	out := make([]*AcceptedSignalSummary, 0, len(facts))
	for _, fact := range facts {
		if fact == nil || strings.TrimSpace(fact.Kind) != "signal" || strings.TrimSpace(fact.ID) == "" {
			continue
		}
		signal, _ := descriptorRepo.GetSignal(ctx, fact.ID)
		if signal == nil {
			continue
		}
		seedIDs, _ := rt.seedInjector.ResolveSeedFragmentIDs(ctx, signal)
		out = append(out, &AcceptedSignalSummary{
			Fact:            fact,
			CanonicalPath:   canonicalPath,
			Signal:          signal,
			SeedFragmentIDs: compactUnique(seedIDs),
		})
	}
	out = dedupeAcceptedSignals(out)
	sortAcceptedSignalsForAnalysis(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func cfgTenantID(cfg *horizonmodels.ScopeAgentConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.TenantID)
}

func (rt *toolRuntime) analyzePropagation(ctx context.Context, subject *SubjectSummary, accepted []*AcceptedSignalSummary, opts toolOptions) (*PropagationAnalysisSummary, error) {
	out := &PropagationAnalysisSummary{Signals: []*PropagationSignalSummary{}}
	if len(accepted) == 0 {
		return out, nil
	}
	sortAcceptedSignalsForAnalysis(accepted)
	analysisLimit := minInt(maxInt(opts.Limit, 1), 2)
	if len(accepted) > analysisLimit {
		accepted = accepted[:analysisLimit]
	}
	subjectUniverse := map[string]struct{}{}
	if subject != nil {
		for _, path := range subject.UniversePaths {
			subjectUniverse[strings.TrimSpace(path)] = struct{}{}
		}
		if subject.AnchorCanonicalPath != "" {
			subjectUniverse[strings.TrimSpace(subject.AnchorCanonicalPath)] = struct{}{}
		}
	}
	seedPathMap := map[string]string{}
	fragments := make([]*horizonmodels.HorizonFragment, 0)
	fragSeen := map[string]struct{}{}
	for _, item := range accepted {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if item == nil || item.Signal == nil {
			continue
		}
		seedIDs := rt.expandSeedFragmentIDs(ctx, item, 16)
		signalCtx := ctx
		if opts.EnablePropagationTrace {
			signalCtx = horizonprop.WithSignalTraceConfig(signalCtx, horizonprop.SignalTraceConfig{
				Enabled:  true,
				MaxSteps: opts.PropagationTraceSteps,
			})
		}
		var raw *horizonprop.SignalSimulationResult
		if opts.PersistPropagation {
			res, err := rt.propagationEngine.PropagateSignalFromSeeds(signalCtx, item.Signal, rt.profileLookup, seedIDs)
			if err != nil {
				return nil, err
			}
			raw = &horizonprop.SignalSimulationResult{
				AffectedFragments: res.AffectedFragments,
				PropagationPaths:  res.PropagationPaths,
				PropagationTime:   res.PropagationTime,
				Metrics:           res.Metrics,
				Trace:             res.Trace,
				TraceDropped:      res.TraceDropped,
			}
		} else {
			res, err := rt.propagationEngine.SimulateSignalFromSeeds(signalCtx, item.Signal, rt.profileLookup, seedIDs)
			if err != nil {
				return nil, err
			}
			raw = res
		}
		postCtx, cancelPost := context.WithTimeout(context.Background(), 30*time.Second)
		sim := rt.simulationFromRaw(postCtx, raw, item.Signal, seedIDs)
		for _, seedID := range seedIDs {
			if seedID == "" {
				continue
			}
			if frag, err := rt.fragmentsRepo.Get(postCtx, seedID); err == nil && frag != nil {
				seedPathMap[frag.ID] = frag.CanonicalPath
				if _, ok := fragSeen[frag.ID]; !ok {
					fragSeen[frag.ID] = struct{}{}
					fragments = append(fragments, frag)
				}
			}
		}
		for _, fragID := range raw.AffectedFragments {
			if fragID == "" {
				continue
			}
			if _, ok := fragSeen[fragID]; ok {
				continue
			}
			if frag, err := rt.fragmentsRepo.Get(postCtx, fragID); err == nil && frag != nil {
				fragSeen[fragID] = struct{}{}
				fragments = append(fragments, frag)
			}
		}
		leaf := rt.leafEvidence.Collect(postCtx, fragments, affectedFragmentIDSet(raw.AffectedFragments))
		sim.LeafEvidence = append(sim.LeafEvidence, leaf...)
		sim.LeafEvidence = append(sim.LeafEvidence, syntheticSeedEvidence(item)...)
		top := topNodeImpacts(sim.NodeImpacts, opts.TopEndpoints, seedIDs)
		escaped := escapedNodeImpacts(sim.NodeImpacts, subjectUniverse, seedIDs)
		diagnostics := rt.buildPropagationDiagnostics(postCtx, item, seedIDs)
		cancelPost()
		out.Signals = append(out.Signals, &PropagationSignalSummary{
			FactID:           safeFactID(item),
			ProposalID:       item.ProposalID,
			CanonicalPath:    item.CanonicalPath,
			Simulation:       sim,
			Raw:              raw,
			EscapedEndpoints: escaped,
			TopEndpoints:     top,
			Diagnostics:      diagnostics,
		})
		out.MeaningfulFarEnds += len(escaped)
		out.NonSeedEndpoints += countNonSeed(top, seedIDs)
		out.TraceWithEdges += countTracePathsWithEdges(sim.TracePaths)
	}
	return out, nil
}

func (rt *toolRuntime) expandSeedFragmentIDs(ctx context.Context, item *AcceptedSignalSummary, limit int) []string {
	seeds := compactUnique(append([]string(nil), item.SeedFragmentIDs...))
	if rt == nil || item == nil || item.Signal == nil {
		return seeds
	}
	if len(seeds) == 0 && rt.fragmentsRepo != nil && strings.TrimSpace(item.CanonicalPath) != "" {
		if frag, err := rt.fragmentsRepo.GetByCanonicalPath(ctx, item.CanonicalPath); err == nil && frag != nil && strings.TrimSpace(frag.ID) != "" {
			seeds = append(seeds, strings.TrimSpace(frag.ID))
		}
	}
	if rt.materializedRepo == nil || rt.fragmentsRepo == nil || len(item.Signal.ChainRefs) == 0 {
		return compactUnique(seeds)
	}
	refs := make([]string, 0, limit*2)
	for _, chainRef := range item.Signal.ChainRefs {
		if len(refs) >= limit {
			break
		}
		artifact, err := rt.materializedRepo.Get(ctx, strings.TrimSpace(chainRef))
		if err != nil || artifact == nil {
			continue
		}
		refs = append(refs, artifact.NodeRefs...)
		refs = append(refs, artifact.FlowRefs...)
		refs = append(refs, artifact.GeoRefs...)
		if artifact.Kind == horizonmodels.MaterializedKindSupplyChainPath {
			if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
				for _, node := range parsed.Nodes {
					refs = append(refs, strings.TrimSpace(node.Ref))
				}
				for _, leg := range parsed.Legs {
					refs = append(refs, compactUnique(append([]string{
						strings.TrimSpace(leg.SupplierRef),
						strings.TrimSpace(leg.CustomerRef),
						strings.TrimSpace(leg.ProductRef),
					}, leg.TransportViaRefs...))...)
				}
			}
		}
	}
	for _, ref := range compactUnique(refs) {
		if len(seeds) >= limit {
			break
		}
		frag, err := rt.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(ref))
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		seeds = append(seeds, strings.TrimSpace(frag.ID))
	}
	return compactUnique(seeds)
}

func (rt *toolRuntime) subjectSummaryForCanonicalPath(ctx context.Context, canonicalPath string) *SubjectSummary {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return nil
	}
	if rt != nil && rt.subjectsRepo != nil {
		if subjects, err := rt.subjectsRepo.ListByAnchorCanonicalPath(ctx, firstNonEmpty(rt.opts.TenantID, "global"), canonicalPath, 4); err == nil {
			for _, subject := range subjects {
				if subject != nil && strings.TrimSpace(subject.ID) != "" {
					return summarizeSubject(subject, rt.materializedSummaryFromSubject(ctx, subject))
				}
			}
		}
	}
	if rt != nil && rt.fragmentsRepo != nil {
		if frag, err := rt.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath); err == nil && frag != nil {
			return &SubjectSummary{
				ID:                  firstNonEmpty(strings.TrimSpace(frag.ID), canonicalPath),
				SubjectKind:         "fragment",
				DisplayLabel:        firstNonEmpty(stringValue(frag.Labels["en"]), stringValue(frag.Labels["name"]), canonicalPath),
				AnchorCanonicalPath: canonicalPath,
				UniversePaths:       compactUnique([]string{canonicalPath}),
			}
		}
	}
	return &SubjectSummary{
		ID:                  canonicalPath,
		SubjectKind:         "canonical_path",
		DisplayLabel:        canonicalPath,
		AnchorCanonicalPath: canonicalPath,
		UniversePaths:       []string{canonicalPath},
	}
}

func (rt *toolRuntime) buildPropagationDiagnostics(ctx context.Context, item *AcceptedSignalSummary, seedIDs []string) *PropagationDiagnosticsSummary {
	if rt == nil || item == nil || item.Signal == nil {
		return nil
	}
	profile, err := rt.profileLookup.GetForSignal(item.Signal)
	if err != nil || profile == nil {
		return nil
	}
	diag := &PropagationDiagnosticsSummary{
		Axis: item.Signal.Axis,
		Profile: map[string]any{
			"axisKey":         profile.AxisKey,
			"defaultMaxHops":  profile.DefaultMaxHops,
			"defaultDamping":  profile.DefaultDamping,
			"propagationMode": profile.PropagationMode,
			"strongEdges":     profile.StrongEdges,
		},
		ExpandedSeedIDs: compactUnique(append([]string(nil), seedIDs...)),
		SeedDiagnostics: make([]*SeedGraphDiagnostic, 0, len(seedIDs)),
	}
	strongSpecs := map[horizonmodels.DependencyType]horizonmodels.StrongEdgeSpec{}
	for _, spec := range profile.StrongEdges {
		strongSpecs[horizonmodels.DependencyType(spec.Type)] = spec
	}
	mode := localNormalizePropagationMode(profile.PropagationMode)
	allowSourceToTarget := localPropagationModeAllowsTraversal(mode, "source_to_target")
	allowTargetToSource := localPropagationModeAllowsTraversal(mode, "target_to_source")
	for _, seedID := range compactUnique(seedIDs) {
		frag, err := rt.fragmentsRepo.Get(ctx, seedID)
		if err != nil || frag == nil {
			continue
		}
		outgoing, _ := rt.edgesRepo.ListBySource(ctx, frag.TenantID, frag.ID, nil, 1000)
		incoming, _ := rt.edgesRepo.ListByTarget(ctx, frag.TenantID, frag.ID, nil, 1000)
		itemDiag := &SeedGraphDiagnostic{
			FragmentID:    frag.ID,
			CanonicalPath: frag.CanonicalPath,
			Type:          string(frag.Type),
		}
		neighborIDs := map[string]struct{}{}
		for _, edge := range outgoing {
			if edge == nil || edge.IsQuarantined() || !edge.IsActiveNow() {
				continue
			}
			itemDiag.OutgoingEdges++
			spec, ok := strongSpecs[edge.Type]
			if !ok || !allowSourceToTarget {
				continue
			}
			direction := localNormalizeStrongEdgeDirection(spec.Direction)
			if direction != "source_to_target" && direction != "both" {
				continue
			}
			itemDiag.StrongOutgoingEdges++
			itemDiag.MatchingOutgoingTypes = append(itemDiag.MatchingOutgoingTypes, string(edge.Type))
			if id := strings.TrimSpace(edge.TargetFragmentID); id != "" {
				neighborIDs[id] = struct{}{}
			}
		}
		for _, edge := range incoming {
			if edge == nil || edge.IsQuarantined() || !edge.IsActiveNow() {
				continue
			}
			itemDiag.IncomingEdges++
			spec, ok := strongSpecs[edge.Type]
			if !ok || !allowTargetToSource {
				continue
			}
			direction := localNormalizeStrongEdgeDirection(spec.Direction)
			if direction != "target_to_source" && direction != "both" {
				continue
			}
			itemDiag.StrongIncomingEdges++
			itemDiag.MatchingIncomingTypes = append(itemDiag.MatchingIncomingTypes, string(edge.Type))
			if id := strings.TrimSpace(edge.SourceFragmentID); id != "" {
				neighborIDs[id] = struct{}{}
			}
		}
		itemDiag.MatchingOutgoingTypes = compactUnique(itemDiag.MatchingOutgoingTypes)
		itemDiag.MatchingIncomingTypes = compactUnique(itemDiag.MatchingIncomingTypes)
		itemDiag.StrongNeighborCount = len(neighborIDs)
		neighborPaths := make([]string, 0, minInt(len(neighborIDs), 8))
		for neighborID := range neighborIDs {
			if len(neighborPaths) >= 8 {
				break
			}
			if n, err := rt.fragmentsRepo.Get(ctx, neighborID); err == nil && n != nil && strings.TrimSpace(n.CanonicalPath) != "" {
				neighborPaths = append(neighborPaths, n.CanonicalPath)
			}
		}
		sort.Strings(neighborPaths)
		itemDiag.StrongNeighborPaths = neighborPaths
		diag.SeedDiagnostics = append(diag.SeedDiagnostics, itemDiag)
	}
	return diag
}

func propagationCandidateScore(prop *PropagationAnalysisSummary) (int, string) {
	if prop == nil {
		return -1, "nil_propagation"
	}
	score := prop.MeaningfulFarEnds*100 + prop.NonSeedEndpoints*10 + prop.TraceWithEdges
	reason := "trace_only"
	switch {
	case prop.MeaningfulFarEnds > 0:
		reason = "meaningful_far_end_detected"
	case prop.NonSeedEndpoints > 0:
		reason = "non_seed_endpoints_detected"
	case prop.TraceWithEdges > 0:
		reason = "edge_trace_detected"
	default:
		if len(prop.Signals) > 0 && prop.Signals[0] != nil && prop.Signals[0].Diagnostics != nil {
			maxNeighbors := 0
			for _, item := range prop.Signals[0].Diagnostics.SeedDiagnostics {
				if item != nil && item.StrongNeighborCount > maxNeighbors {
					maxNeighbors = item.StrongNeighborCount
				}
			}
			score += maxNeighbors
			if maxNeighbors > 0 {
				reason = "strong_neighbors_without_effect"
			} else {
				reason = "no_strong_neighbors"
			}
		}
	}
	return score, reason
}

func localNormalizePropagationMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "upstream", "downstream", "both":
		return mode
	default:
		return "both"
	}
}

func localNormalizeStrongEdgeDirection(direction string) string {
	direction = strings.ToLower(strings.TrimSpace(direction))
	switch direction {
	case "source_to_target", "target_to_source", "both":
		return direction
	default:
		return ""
	}
}

func localPropagationModeAllowsTraversal(mode, traversalDirection string) bool {
	mode = localNormalizePropagationMode(mode)
	switch strings.ToLower(strings.TrimSpace(traversalDirection)) {
	case "source_to_target":
		return mode == "downstream" || mode == "both"
	case "target_to_source":
		return mode == "upstream" || mode == "both"
	default:
		return false
	}
}

func (rt *toolRuntime) analyzeInterpretation(ctx context.Context, analysis *PropagationAnalysisSummary, opts toolOptions) (*InterpretationSummary, error) {
	out := &InterpretationSummary{}
	if opts.TopInterpretations == 0 {
		return out, nil
	}
	if analysis == nil || len(analysis.Signals) == 0 {
		return out, nil
	}
	primary := analysis.Signals[0]
	if primary.Simulation == nil {
		return out, nil
	}
	fragments := rt.loadFragmentsForSimulation(ctx, primary.Simulation)
	seedImpact := firstSeedImpact(primary.Simulation.NodeImpacts)
	if seedImpact != nil {
		out.SeedInterpretation = rt.interpretNode(ctx, fragments, primary.Simulation, seedImpact, "risk")
	}
	for _, interp := range rt.interpretTopEndpoints(ctx, fragments, primary.Simulation, opts.TopInterpretations) {
		out.EndpointInterpretations = append(out.EndpointInterpretations, interp)
	}
	return out, nil
}

func (rt *toolRuntime) interpretNode(
	ctx context.Context,
	fragments []*horizonmodels.HorizonFragment,
	sim *horizonmodels.SimulationResult,
	node *horizonmodels.NodeImpact,
	polarity string,
) *NodeInterpretationSummary {
	if node == nil {
		return nil
	}
	nodeCtx, cancel := context.WithTimeout(ctx, perNodeInterpretationTimeout)
	defer cancel()
	var fragment *horizonmodels.HorizonFragment
	for _, item := range fragments {
		if item != nil && item.ID == node.FragmentID {
			fragment = item
			break
		}
	}
	leaf := leafEvidenceForNode(sim, node.FragmentID)
	interpretation, err := rt.interpreter.InterpretFromNodeImpact(nodeCtx, fragment, node, leaf, polarity)
	summary := &NodeInterpretationSummary{
		FragmentID:    node.FragmentID,
		CanonicalPath: node.CanonicalPath,
		Status:        verdictPass,
	}
	if err != nil {
		summary.Status = verdictFail
		summary.Reason = "interpreter_failed"
		return summary
	}
	summary.Interpretation = interpretation
	summary.SupportingEvidence = collectEvidenceSummaries(leaf, interpretation)
	if strings.TrimSpace(interpretation.Summary) == "" || len(interpretation.AxisBreakdown) == 0 {
		summary.Status = verdictWarn
		summary.Reason = "empty_axis_breakdown"
	}
	if len(interpretation.TopEvents) == 0 {
		summary.Status = worstStatus(summary.Status, verdictWarn)
		summary.Reason = firstNonEmpty(summary.Reason, "no_top_events")
	}
	return summary
}

func (rt *toolRuntime) interpretTopEndpoints(
	ctx context.Context,
	fragments []*horizonmodels.HorizonFragment,
	sim *horizonmodels.SimulationResult,
	limit int,
) []*NodeInterpretationSummary {
	nodeImpacts := topNodeImpacts(sim.NodeImpacts, limit, seedIDsFromNodeImpacts(sim.NodeImpacts))
	out := make([]*NodeInterpretationSummary, 0, len(nodeImpacts))
	for _, node := range nodeImpacts {
		if node.Hops <= 0 {
			continue
		}
		out = append(out, rt.interpretNode(ctx, fragments, sim, &node, "risk"))
		if len(out) >= limit {
			break
		}
	}
	return out
}
