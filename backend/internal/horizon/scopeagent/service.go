package scopeagent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	defaultScopeAgentProjectID = "horizon-watch-bootstrap-global"
)

type Service struct {
	configsRepo       *horizonrepo.ScopeAgentConfigsRepository
	statesRepo        *horizonrepo.ScopeAgentStatesRepository
	runsRepo          *horizonrepo.ScopeAgentRunsRepository
	proposalsRepo     *horizonrepo.AgentProposalsRepository
	decisionsRepo     *horizonrepo.ArbitrationDecisionsRepository
	targetsRepo       *horizonrepo.WatchTargetsRepository
	briefsRepo        *horizonrepo.WatchBriefsRepository
	feedRepo          *horizonrepo.WatchFeedEntriesRepository
	agentJobsRepo     *corerepo.AgentJobsRepository
	watchdogRepo      *horizonrepo.WatchdogConfigsRepository
	fragmentsRepo     *horizonrepo.HorizonFragmentsRepository
	riskJobsRepo      *horizonrepo.RiskJobsRepository
	factsRepo         *corerepo.OntologyFactsRepository
	dataSourcesRepo   *corerepo.DataSourcesRepository
	aiExecutor        *ai.AITaskExecutor
	propagationEngine *horizonprop.PropagationEngine
	profileLookup     *horizonprop.ProfileLookup
	batchDispatcher   func(context.Context, string) error
}

type Batch struct {
	Key             string   `json:"key"`
	TenantID        string   `json:"tenantId"`
	PrimaryModel    string   `json:"primaryModel"`
	EscalationModel string   `json:"escalationModel,omitempty"`
	DomainModes     []string `json:"domainModes,omitempty"`
	Roles           []string `json:"roles,omitempty"`
	RunIDs          []string `json:"runIds"`
}

type groundedBatchResponse struct {
	Proposals []groundedProposal `json:"proposals"`
}

type groundedProposal struct {
	WatchTargetID        string   `json:"watchTargetId,omitempty"`
	ProposalType         string   `json:"proposalType"`
	TargetCanonicalPaths []string `json:"targetCanonicalPaths"`
	Polarity             string   `json:"polarity,omitempty"`
	SignalType           string   `json:"signalType,omitempty"`
	Title                string   `json:"title"`
	Summary              string   `json:"summary"`
	Explanation          string   `json:"explanation"`
	Confidence           float64  `json:"confidence"`
	Impact               float64  `json:"impact,omitempty"`
	Likelihood           float64  `json:"likelihood,omitempty"`
	EscalationLevel      string   `json:"escalationLevel,omitempty"`
	MetricName           string   `json:"metricName,omitempty"`
	MetricValue          float64  `json:"metricValue,omitempty"`
	TimeHorizon          string   `json:"timeHorizon,omitempty"`
	Watchpoints          []string `json:"watchpoints,omitempty"`
	TriggerConditions    []string `json:"triggerConditions,omitempty"`
}

type batchExecutionReport struct {
	Batch               Batch     `json:"batch"`
	AgentJobID          string    `json:"agentJobId,omitempty"`
	ProducedProposalIDs []string  `json:"producedProposalIds,omitempty"`
	Noop                bool      `json:"noop"`
	CompletedAt         time.Time `json:"completedAt"`
}

func NewService(
	configsRepo *horizonrepo.ScopeAgentConfigsRepository,
	statesRepo *horizonrepo.ScopeAgentStatesRepository,
	runsRepo *horizonrepo.ScopeAgentRunsRepository,
	proposalsRepo *horizonrepo.AgentProposalsRepository,
	decisionsRepo *horizonrepo.ArbitrationDecisionsRepository,
	targetsRepo *horizonrepo.WatchTargetsRepository,
	briefsRepo *horizonrepo.WatchBriefsRepository,
	feedRepo *horizonrepo.WatchFeedEntriesRepository,
	agentJobsRepo *corerepo.AgentJobsRepository,
	watchdogRepo *horizonrepo.WatchdogConfigsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	riskJobsRepo *horizonrepo.RiskJobsRepository,
	factsRepo *corerepo.OntologyFactsRepository,
	dataSourcesRepo *corerepo.DataSourcesRepository,
	aiExecutor *ai.AITaskExecutor,
	propagationEngine *horizonprop.PropagationEngine,
	profileLookup *horizonprop.ProfileLookup,
) *Service {
	return &Service{
		configsRepo:       configsRepo,
		statesRepo:        statesRepo,
		runsRepo:          runsRepo,
		proposalsRepo:     proposalsRepo,
		decisionsRepo:     decisionsRepo,
		targetsRepo:       targetsRepo,
		briefsRepo:        briefsRepo,
		feedRepo:          feedRepo,
		agentJobsRepo:     agentJobsRepo,
		watchdogRepo:      watchdogRepo,
		fragmentsRepo:     fragmentsRepo,
		riskJobsRepo:      riskJobsRepo,
		factsRepo:         factsRepo,
		dataSourcesRepo:   dataSourcesRepo,
		aiExecutor:        aiExecutor,
		propagationEngine: propagationEngine,
		profileLookup:     profileLookup,
	}
}

func (s *Service) SetScenarioRuntime(engine *horizonprop.PropagationEngine, profileLookup *horizonprop.ProfileLookup) {
	if s == nil {
		return
	}
	s.propagationEngine = engine
	s.profileLookup = profileLookup
}

func (s *Service) WithBatchDispatcher(fn func(context.Context, string) error) *Service {
	if s == nil {
		return s
	}
	s.batchDispatcher = fn
	return s
}

func (s *Service) EnsureRuntimeForTarget(ctx context.Context, target *horizonmodels.WatchTarget) (*horizonmodels.ScopeAgentConfig, error) {
	if s == nil || s.configsRepo == nil || target == nil {
		return nil, nil
	}
	if strings.TrimSpace(target.CanonicalPath) == "" {
		return nil, fmt.Errorf("watch target canonicalPath is required")
	}
	if strings.TrimSpace(target.FragmentID) == "" && s.fragmentsRepo != nil {
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, target.CanonicalPath)
		if err == nil && fragment != nil {
			target.FragmentID = strings.TrimSpace(fragment.ID)
		}
	}
	cfg := &horizonmodels.ScopeAgentConfig{
		TenantID:           strings.TrimSpace(firstNonEmpty(target.TenantID, "global")),
		PackID:             strings.TrimSpace(target.PackID),
		WatchTargetID:      strings.TrimSpace(target.ID),
		TargetFragmentID:   strings.TrimSpace(target.FragmentID),
		Name:               fmt.Sprintf("%s focused scope agent", strings.TrimSpace(target.Label)),
		ScopeCanonicalPath: strings.TrimSpace(target.CanonicalPath),
		ScopeType:          normalizeScopeType(strings.TrimSpace(firstNonEmpty(target.Kind, "fragment"))),
		DomainModes:        compactUnique(target.DomainModes),
		Roles:              normalizeRoles(target.Roles),
		Priority:           target.Priority,
		Status:             string(horizonmodels.ScopeAgentStatusRunning),
		ExpansionPolicy: map[string]any{
			"focusMode":      "canonical_scope",
			"allowOverlaps":  true,
			"centralArbiter": true,
		},
		Schedule: map[string]any{
			"debounceSeconds":    180,
			"batchWindowSeconds": 180,
		},
		LLMPolicy: defaultLLMPolicy(),
		Metadata: map[string]any{
			"provisionedBy": "watch_service",
			"watchTargetId": target.ID,
		},
	}
	if err := s.configsRepo.Upsert(ctx, cfg); err != nil {
		return nil, err
	}
	state := s.getExistingState(ctx, cfg)
	if state.Baseline == nil {
		state.Baseline = map[string]any{"hotness": 0.15, "confidence": 0.30}
	}
	if len(state.OpenWatchpoints) == 0 {
		state.OpenWatchpoints = compactUnique(target.DomainModes)
	}
	if len(state.CoverageGaps) == 0 {
		state.CoverageGaps = []string{"historical_baseline", "scenario_envelope"}
	}
	state.WatchTargetID = target.ID
	if err := s.statesRepo.Upsert(ctx, state); err != nil {
		return nil, err
	}
	target.AgentConfigID = cfg.ID
	if s.targetsRepo != nil {
		_ = s.targetsRepo.Upsert(ctx, target)
	}
	if s.watchdogRepo != nil && strings.TrimSpace(target.FragmentID) != "" {
		if err := s.ensureWatchdogConfig(ctx, cfg.TenantID, target.FragmentID); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func (s *Service) WakeByCanonicalPath(ctx context.Context, tenantID, canonicalPath, triggerType, triggerReason string, trace map[string]any) ([]*horizonmodels.ScopeAgentRun, error) {
	if s == nil || s.configsRepo == nil || s.runsRepo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return nil, nil
	}
	configs, err := s.configsRepo.ListActiveByCanonicalPath(ctx, tenantID, canonicalPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]*horizonmodels.ScopeAgentRun, 0, len(configs))
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		debounceUntil := now.Add(time.Duration(maxInt(cfg.LLMPolicy.BatchWindowSeconds, 60)) * time.Second)
		if s.statesRepo != nil {
			state, err := s.statesRepo.GetByConfigID(ctx, cfg.ID)
			if err == nil && state != nil && state.DebounceUntil != nil && state.DebounceUntil.After(now) {
				_ = s.statesRepo.MergeWakeReason(ctx, cfg.ID, triggerReason, state.DebounceUntil)
				continue
			}
		}
		run := &horizonmodels.ScopeAgentRun{
			TenantID:        cfg.TenantID,
			ConfigID:        cfg.ID,
			WatchTargetID:   cfg.WatchTargetID,
			Status:          string(horizonmodels.ScopeAgentRunStatusPending),
			TriggerType:     firstNonEmpty(triggerType, "watchdog"),
			TriggerReason:   triggerReason,
			PrimaryModel:    firstNonEmpty(cfg.LLMPolicy.PreferredVertexModel, cfg.LLMPolicy.PrimaryModel),
			EscalationModel: firstNonEmpty(cfg.LLMPolicy.EscalationModel, "gemini-3-pro-preview"),
			Trace:           trace,
		}
		if _, err := s.runsRepo.Create(ctx, run); err != nil {
			return out, err
		}
		if s.statesRepo != nil {
			state := s.getExistingState(ctx, cfg)
			state.PendingWakeReasons = compactUnique(append(state.PendingWakeReasons, triggerReason))
			state.DebounceUntil = &debounceUntil
			state.LastRunID = run.ID
			_ = s.statesRepo.Upsert(ctx, state)
		}
		out = append(out, run)
	}
	if len(out) > 0 && s.batchDispatcher != nil {
		if err := s.batchDispatcher(ctx, tenantID); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Service) PlanDueBatches(ctx context.Context, tenantID string, limit int) ([]Batch, error) {
	if s == nil || s.runsRepo == nil || s.configsRepo == nil {
		return nil, nil
	}
	runs, err := s.runsRepo.ListPending(ctx, tenantID, limit)
	if err != nil {
		return nil, err
	}
	grouped := map[string]*Batch{}
	for _, run := range runs {
		if run == nil {
			continue
		}
		cfg, err := s.configsRepo.Get(ctx, run.ConfigID)
		if err != nil || cfg == nil {
			continue
		}
		key := batchKey(cfg)
		batch := grouped[key]
		if batch == nil {
			batch = &Batch{
				Key:             key,
				TenantID:        cfg.TenantID,
				PrimaryModel:    firstNonEmpty(cfg.LLMPolicy.PreferredVertexModel, cfg.LLMPolicy.PrimaryModel),
				EscalationModel: cfg.LLMPolicy.EscalationModel,
				DomainModes:     append([]string(nil), cfg.DomainModes...),
				Roles:           append([]string(nil), cfg.Roles...),
			}
			grouped[key] = batch
		}
		if cfg.LLMPolicy.MaxBatchSize > 0 && len(batch.RunIDs) >= cfg.LLMPolicy.MaxBatchSize {
			continue
		}
		run.BatchKey = key
		_ = s.runsRepo.Update(ctx, run)
		batch.RunIDs = append(batch.RunIDs, run.ID)
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Batch, 0, len(keys))
	for _, key := range keys {
		out = append(out, *grouped[key])
	}
	return out, nil
}

func (s *Service) RunDueBatches(ctx context.Context, tenantID string, limit int) ([]batchExecutionReport, error) {
	if s == nil || s.aiExecutor == nil {
		return nil, nil
	}
	batches, err := s.PlanDueBatches(ctx, tenantID, limit)
	if err != nil {
		return nil, err
	}
	reports := make([]batchExecutionReport, 0, len(batches))
	for _, batch := range batches {
		report, err := s.runBatch(ctx, batch)
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (s *Service) ArbitrateProposal(ctx context.Context, proposal *horizonmodels.AgentProposal) (*horizonmodels.ArbitrationDecision, error) {
	if s == nil || s.proposalsRepo == nil || s.decisionsRepo == nil || proposal == nil {
		return nil, nil
	}
	if strings.TrimSpace(proposal.DedupKey) == "" {
		proposal.DedupKey = proposalDedupKey(proposal)
	}
	existing, err := s.proposalsRepo.FindByDedupKey(ctx, proposal.TenantID, proposal.DedupKey)
	if err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
		decision := &horizonmodels.ArbitrationDecision{
			TenantID:    proposal.TenantID,
			ProposalID:  existing.ID,
			Decision:    string(horizonmodels.ArbitrationDecisionMerge),
			MergedInto:  existing.ID,
			ReasonCodes: []string{"duplicate_dedup_key"},
		}
		if _, err := s.decisionsRepo.Create(ctx, decision); err != nil {
			return nil, err
		}
		proposal.Status = string(horizonmodels.AgentProposalStatusMerged)
		return decision, nil
	}
	if _, err := s.proposalsRepo.Create(ctx, proposal); err != nil {
		return nil, err
	}
	proposal.Status = string(horizonmodels.AgentProposalStatusAccepted)
	_ = s.proposalsRepo.Update(ctx, proposal)

	appliedArtifacts, err := s.applyAcceptedProposal(ctx, proposal)
	if err != nil {
		proposal.Status = string(horizonmodels.AgentProposalStatusRejected)
		_ = s.proposalsRepo.Update(ctx, proposal)
		decision := &horizonmodels.ArbitrationDecision{
			TenantID:    proposal.TenantID,
			ProposalID:  proposal.ID,
			Decision:    string(horizonmodels.ArbitrationDecisionReject),
			ReasonCodes: []string{"apply_failed"},
		}
		_, _ = s.decisionsRepo.Create(ctx, decision)
		return nil, err
	}

	decision := &horizonmodels.ArbitrationDecision{
		TenantID:         proposal.TenantID,
		ProposalID:       proposal.ID,
		Decision:         string(horizonmodels.ArbitrationDecisionAccept),
		ReasonCodes:      []string{"validated_against_central_keyspace"},
		AppliedArtifacts: appliedArtifacts,
	}
	if _, err := s.decisionsRepo.Create(ctx, decision); err != nil {
		return nil, err
	}
	return decision, nil
}

func (s *Service) runBatch(ctx context.Context, batch Batch) (batchExecutionReport, error) {
	report := batchExecutionReport{Batch: batch}
	if len(batch.RunIDs) == 0 {
		report.Noop = true
		report.CompletedAt = time.Now().UTC()
		return report, nil
	}
	runContexts, projectID, err := s.loadRunContexts(ctx, batch.RunIDs)
	if err != nil {
		return report, err
	}
	jobID := ""
	if s.agentJobsRepo != nil {
		job := &coremodels.AgentJob{
			TenantID:  strings.TrimSpace(firstNonEmpty(batch.TenantID, "global")),
			ProjectID: strings.TrimSpace(firstNonEmpty(projectID, defaultScopeAgentProjectID)),
			UserID:    "system",
			AgentID:   "scope_agent_batch_runner",
			Title:     fmt.Sprintf("Scope agent batch %s", batch.PrimaryModel),
			Status:    coremodels.AgentJobStatusRunning,
			Task: map[string]any{
				"batchKey":         batch.Key,
				"runIds":           batch.RunIDs,
				"primaryModel":     batch.PrimaryModel,
				"escalationModel":  batch.EscalationModel,
				"domainModes":      batch.DomainModes,
				"roles":            batch.Roles,
				"dispatchStrategy": "batched_grounded_scope_agent",
			},
			Steps:    []coremodels.AgentStep{},
			AutoMode: true,
			PlanMode: coremodels.PlanModeAuto,
		}
		if createdID, err := s.agentJobsRepo.Create(ctx, job); err == nil {
			jobID = createdID
			report.AgentJobID = createdID
		}
	}
	task, err := s.buildBatchTask(batch, runContexts)
	if err != nil {
		return report, err
	}
	s.markRunsExecuting(ctx, runContexts)
	parsed, result, err := s.aiExecutor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		s.finalizeRuns(ctx, runContexts, jobID, string(horizonmodels.ScopeAgentRunStatusFailed), nil, map[string]any{"error": err.Error()})
		if jobID != "" {
			_ = s.agentJobsRepo.UpdateStatus(ctx, jobID, coremodels.AgentJobStatusFailed, 1.0)
		}
		return report, err
	}
	response, ok := parsed.(groundedBatchResponse)
	if !ok {
		if typed, ok2 := parsed.(*groundedBatchResponse); ok2 && typed != nil {
			response = *typed
		} else {
			return report, fmt.Errorf("unexpected grounded batch response type %T", parsed)
		}
	}

	groundingRefs, groundingMeta := buildGroundingRefs(result)
	produced := []string{}
	now := time.Now().UTC()
	for _, gp := range response.Proposals {
		runCtx := pickRunContext(runContexts, gp.WatchTargetID, gp.TargetCanonicalPaths)
		if runCtx == nil {
			continue
		}
		payload := map[string]any{
			"title":             gp.Title,
			"summary":           gp.Summary,
			"polarity":          normalizePolarity(gp.Polarity),
			"signalType":        normalizeSignalType(gp.SignalType, gp.Polarity),
			"impact":            clamp01(gp.Impact),
			"likelihood":        clamp01(gp.Likelihood),
			"escalationLevel":   firstNonEmpty(gp.EscalationLevel, horizonmodels.EscalationLevelWatch),
			"timeHorizon":       firstNonEmpty(gp.TimeHorizon, horizonmodels.SignalTimeHorizonShortTerm),
			"metricName":        firstNonEmpty(gp.MetricName, "watch/pressure"),
			"metricValue":       metricValueForProposal(gp),
			"watchpoints":       compactUnique(gp.Watchpoints),
			"triggerConditions": compactUnique(gp.TriggerConditions),
			"generatedAt":       now.Format(time.RFC3339),
			"grounding":         groundingMeta,
		}
		proposal := &horizonmodels.AgentProposal{
			TenantID:             runCtx.Config.TenantID,
			ConfigID:             runCtx.Config.ID,
			RunID:                runCtx.Run.ID,
			WatchTargetID:        runCtx.Target.ID,
			ProposalType:         strings.TrimSpace(firstNonEmpty(gp.ProposalType, string(horizonmodels.AgentProposalTypeSignalCandidate))),
			TargetCanonicalPaths: fallbackCanonicalPaths(gp.TargetCanonicalPaths, runCtx.Target.CanonicalPath),
			SignalKey:            normalizeSignalType(gp.SignalType, gp.Polarity),
			Payload:              payload,
			SourceRefs:           append([]horizonmodels.SourceRef(nil), groundingRefs...),
			Confidence:           clamp01(gp.Confidence),
			Explanation:          strings.TrimSpace(firstNonEmpty(gp.Explanation, gp.Summary)),
			TimeWindowStart:      runCtx.Run.StartedAt,
			TimeWindowEnd:        &now,
		}
		decision, err := s.ArbitrateProposal(ctx, proposal)
		if err != nil {
			return report, err
		}
		if decision != nil && strings.TrimSpace(decision.Decision) == string(horizonmodels.ArbitrationDecisionAccept) {
			produced = append(produced, proposal.ID)
		}
	}
	if len(produced) == 0 {
		s.emitNoopFeedEntries(ctx, runContexts, groundingMeta)
		report.Noop = true
	}
	report.ProducedProposalIDs = produced
	report.CompletedAt = now
	s.finalizeRuns(ctx, runContexts, jobID, string(horizonmodels.ScopeAgentRunStatusCompleted), produced, groundingMeta)
	if jobID != "" {
		_ = s.agentJobsRepo.UpdateStatus(ctx, jobID, coremodels.AgentJobStatusCompleted, 1.0)
	}
	return report, nil
}

type runContext struct {
	Run      *horizonmodels.ScopeAgentRun
	Config   *horizonmodels.ScopeAgentConfig
	Target   *horizonmodels.WatchTarget
	Fragment *horizonmodels.HorizonFragment
	RiskJobs []*horizonmodels.RiskJob
	Facts    []*coremodels.OntologyFact
}

func (s *Service) loadRunContexts(ctx context.Context, runIDs []string) ([]*runContext, string, error) {
	out := make([]*runContext, 0, len(runIDs))
	projectID := ""
	for _, runID := range runIDs {
		run, err := s.runsRepo.Get(ctx, runID)
		if err != nil || run == nil {
			continue
		}
		cfg, err := s.configsRepo.Get(ctx, run.ConfigID)
		if err != nil || cfg == nil {
			continue
		}
		target, err := s.targetsRepo.Get(ctx, cfg.WatchTargetID)
		if err != nil || target == nil {
			continue
		}
		var fragment *horizonmodels.HorizonFragment
		if s.fragmentsRepo != nil {
			if strings.TrimSpace(cfg.TargetFragmentID) != "" {
				fragment, _ = s.fragmentsRepo.Get(ctx, cfg.TargetFragmentID)
			}
			if fragment == nil {
				fragment, _ = s.fragmentsRepo.GetByCanonicalPath(ctx, cfg.ScopeCanonicalPath)
			}
		}
		if projectID == "" && fragment != nil && fragment.Governance.CreatedByProjectID != nil {
			projectID = strings.TrimSpace(*fragment.Governance.CreatedByProjectID)
		}
		riskJobs := []*horizonmodels.RiskJob{}
		if s.riskJobsRepo != nil {
			riskJobs, _ = s.riskJobsRepo.ListRecentByTargetCanonicalPath(ctx, cfg.TenantID, cfg.ScopeCanonicalPath, 4)
		}
		facts := []*coremodels.OntologyFact{}
		if s.factsRepo != nil {
			facts, _ = s.factsRepo.ListByCanonicalPath(ctx, "global", cfg.ScopeCanonicalPath, "", 6)
		}
		out = append(out, &runContext{
			Run:      run,
			Config:   cfg,
			Target:   target,
			Fragment: fragment,
			RiskJobs: riskJobs,
			Facts:    facts,
		})
	}
	return out, projectID, nil
}

func (s *Service) buildBatchTask(batch Batch, contexts []*runContext) (*ai.AITask, error) {
	payloads := make([]map[string]any, 0, len(contexts))
	fallbacks := []string{}
	for _, ctxItem := range contexts {
		if ctxItem == nil {
			continue
		}
		if len(ctxItem.Config.LLMPolicy.FallbackModels) > 0 {
			fallbacks = append(fallbacks, ctxItem.Config.LLMPolicy.FallbackModels...)
		}
		riskItems := make([]map[string]any, 0, len(ctxItem.RiskJobs))
		for _, job := range ctxItem.RiskJobs {
			if job == nil {
				continue
			}
			riskItems = append(riskItems, map[string]any{
				"state":      string(job.CurrentState),
				"stage":      string(job.CurrentStage),
				"polarity":   job.Polarity,
				"updatedAt":  job.UpdatedAt.Format(time.RFC3339),
				"triggerRef": job.TriggerRef,
			})
		}
		factItems := make([]map[string]any, 0, len(ctxItem.Facts))
		for _, fact := range ctxItem.Facts {
			if fact == nil {
				continue
			}
			factItems = append(factItems, map[string]any{
				"id":         fact.ID,
				"kind":       fact.Kind,
				"type":       fact.Type,
				"title":      fact.Title,
				"summary":    fact.Summary,
				"confidence": fact.Confidence,
				"updatedAt":  fact.UpdatedAt.Format(time.RFC3339),
			})
		}
		payloads = append(payloads, map[string]any{
			"runId":          ctxItem.Run.ID,
			"watchTargetId":  ctxItem.Target.ID,
			"label":          ctxItem.Target.Label,
			"canonicalPath":  ctxItem.Target.CanonicalPath,
			"scopeType":      ctxItem.Config.ScopeType,
			"domainModes":    ctxItem.Config.DomainModes,
			"roles":          ctxItem.Config.Roles,
			"fragmentId":     ctxItem.Target.FragmentID,
			"riskJobs":       riskItems,
			"facts":          factItems,
			"queries":        queriesForRunContext(ctxItem),
			"pendingTrigger": ctxItem.Run.TriggerReason,
		})
	}
	inputJSON, err := json.Marshal(map[string]any{
		"batchKey":     batch.Key,
		"primaryModel": batch.PrimaryModel,
		"targets":      payloads,
	})
	if err != nil {
		return nil, err
	}
	instructions := `You are a focused Horizon scope-agent batch runner.
Return strict JSON with shape {"proposals":[...]}.
Each proposal must refer to one concrete watched target and only include material, actionable outputs.
Do not emit empty boilerplate proposals.
For each proposal:
- proposalType must be one of signal_candidate, signal_boost, forecast_candidate, evidence_bundle, coverage_gap
- targetCanonicalPaths must include the watched canonical path
- confidence, impact, likelihood must be 0..1
- signalType should be risk:* or opportunity:* when proposalType is signal_candidate or signal_boost
- metricName should normally be watch/pressure or watch/forecast_pressure
- metricValue should be 0..1
- watchpoints should be concrete conditions that would change the assessment
If nothing material is found, return {"proposals":[]}.`
	model := batch.PrimaryModel
	if strings.HasPrefix(model, "vertex-") {
		model = strings.TrimSpace(model)
	}
	return &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: instructions + "\n\nBATCH_CONTEXT_JSON:\n" + string(inputJSON),
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  model,
			FallbackModels:                  compactUnique(append([]string{batch.EscalationModel, "gemini-3-flash-preview", "gemini-2.5-flash", "gemini-2.5-pro"}, fallbacks...)),
			MinContextWindow:                24000,
			MaxOutputSize:                   4000,
			MinCapabilityScore:              7,
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: strings.HasPrefix(model, "vertex-"),
		},
		Verifier:  ai.NewGenericVerifier(func(r groundedBatchResponse) error { return validateGroundedBatchResponse(r) }),
		TenantID:  batch.TenantID,
		UserID:    "system",
		Project:   defaultScopeAgentProjectID,
		UseCase:   "horizon.scope_agent.batch",
		CascadeID: "scope_agent_batch",
	}, nil
}

func validateGroundedBatchResponse(resp groundedBatchResponse) error {
	if len(resp.Proposals) > 64 {
		return fmt.Errorf("too many proposals")
	}
	for _, proposal := range resp.Proposals {
		if strings.TrimSpace(proposal.ProposalType) == "" {
			return fmt.Errorf("proposalType is required")
		}
		if len(proposal.TargetCanonicalPaths) == 0 {
			return fmt.Errorf("targetCanonicalPaths are required")
		}
		if strings.TrimSpace(proposal.Title) == "" {
			return fmt.Errorf("title is required")
		}
		if proposal.Confidence < 0 || proposal.Confidence > 1 {
			return fmt.Errorf("confidence out of range")
		}
	}
	return nil
}

func buildGroundingRefs(result *ai.AITaskResult) ([]horizonmodels.SourceRef, map[string]any) {
	refs := []horizonmodels.SourceRef{}
	meta := map[string]any{}
	if result == nil {
		return refs, meta
	}
	meta["model"] = result.Model
	meta["citations"] = len(result.Citations)
	for _, citation := range result.Citations {
		if strings.TrimSpace(citation.URL) == "" {
			continue
		}
		refs = append(refs, horizonmodels.SourceRef{SourceID: strings.TrimSpace(citation.URL), SourceType: coremodels.SourceTypeWeb})
	}
	if result.GroundingEvidence != nil {
		webURLs := make([]string, 0, len(result.GroundingEvidence.WebSources))
		for _, citation := range result.GroundingEvidence.WebSources {
			if strings.TrimSpace(citation.URL) == "" {
				continue
			}
			refs = append(refs, horizonmodels.SourceRef{SourceID: strings.TrimSpace(citation.URL), SourceType: coremodels.SourceTypeWeb})
			webURLs = append(webURLs, strings.TrimSpace(citation.URL))
		}
		meta["webUrls"] = compactUnique(webURLs)
		meta["datastoreFragmentIds"] = compactUnique(result.GroundingEvidence.DatastoreFragmentIDs)
		meta["hasDatastoreResults"] = result.GroundingEvidence.HasDatastoreResults
		meta["hasWebResults"] = result.GroundingEvidence.HasWebResults
	}
	return dedupeSourceRefs(refs), meta
}

func (s *Service) applyAcceptedProposal(ctx context.Context, proposal *horizonmodels.AgentProposal) ([]string, error) {
	applied := []string{}
	now := time.Now().UTC()
	quasiSourceID := ""
	if s.dataSourcesRepo != nil {
		id, err := s.dataSourcesRepo.EnsureLLMSearchQuasiSource(ctx)
		if err == nil {
			quasiSourceID = strings.TrimSpace(id)
		}
	}
	for _, canonicalPath := range proposal.TargetCanonicalPaths {
		canonicalPath = strings.TrimSpace(canonicalPath)
		if canonicalPath == "" {
			continue
		}
		if s.fragmentsRepo != nil {
			if fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath); err == nil && fragment != nil {
				if fragment.State == nil {
					fragment.State = map[string]horizonmodels.MetricValue{}
				}
				metricName := firstNonEmpty(stringValueFromMap(proposal.Payload, "metricName"), "watch/pressure")
				metricValue := metricValueFromPayload(proposal.Payload, proposal.Confidence)
				fragment.State[metricName] = horizonmodels.MetricValue{
					Value:      metricValue,
					Confidence: clamp01(proposal.Confidence),
					ObservedAt: now,
					Provenance: horizonmodels.Provenance{
						SourceIDs:  compactUnique([]string{quasiSourceID}),
						SourceRefs: proposal.SourceRefs,
						Method:     "scope_agent_arbiter",
						ObservedAt: &now,
					},
				}
				if err := s.fragmentsRepo.UpdateDerived(ctx, fragment); err == nil {
					applied = append(applied, "fragment:"+fragment.ID)
				}
			}
		}

		if s.factsRepo != nil {
			factID := factIDForProposal(proposal, canonicalPath)
			signalType := normalizeSignalType(stringValueFromMap(proposal.Payload, "signalType"), stringValueFromMap(proposal.Payload, "polarity"))
			impact := metricValueFromMap(proposal.Payload, "impact", proposal.Confidence)
			likelihood := metricValueFromMap(proposal.Payload, "likelihood", proposal.Confidence)
			escalation := firstNonEmpty(stringValueFromMap(proposal.Payload, "escalationLevel"), horizonmodels.EscalationLevelWatch)
			metricName := stringValueFromMap(proposal.Payload, "metricName")
			metricVal := metricValueFromPayload(proposal.Payload, proposal.Confidence)
			fact := &coremodels.OntologyFact{
				BaseOntologyFields: coremodels.BaseOntologyFields{
					ID:             factID,
					TenantID:       firstNonEmpty(proposal.TenantID, "global"),
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
					CreatedBy:      "scope_agent_arbiter",
					UpdatedBy:      "scope_agent_arbiter",
					Labels:         map[string]string{"en": firstNonEmpty(stringValueFromMap(proposal.Payload, "title"), "Scope agent signal")},
					Description:    firstNonEmpty(stringValueFromMap(proposal.Payload, "summary"), proposal.Explanation),
					Tags:           []string{"scope_agent", "arbiter", "proposal:" + strings.TrimSpace(proposal.ProposalType), "signal_impact:" + formatMetric(impact), "signal_likelihood:" + formatMetric(likelihood), "signal_escalation:" + escalation},
					Status:         "active",
					CanonicalPaths: []string{canonicalPath},
					VectorText:     "",
				},
				Kind:       "signal",
				Type:       signalType,
				Title:      firstNonEmpty(stringValueFromMap(proposal.Payload, "title"), "Scope agent signal"),
				Summary:    firstNonEmpty(stringValueFromMap(proposal.Payload, "summary"), proposal.Explanation),
				Confidence: clamp01(proposal.Confidence),
				ObservedAt: &now,
				SourceIDs:  compactUnique([]string{quasiSourceID}),
				SourceRefs: toCoreSourceRefs(proposal.SourceRefs),
				MetricUpdates: []coremodels.FactMetricUpdate{{
					MetricName: firstNonEmpty(metricName, "watch/pressure"),
					Value:      metricVal,
					Confidence: clamp01(proposal.Confidence),
					ObservedAt: &now,
				}},
				ExtractionLineage: &coremodels.FactExtractionLineage{
					Pipeline:     "scope_agent_arbiter",
					CurrentLayer: coremodels.FactExtractionLayerInterpretation,
					LLMEnabled:   true,
					LLMUsed:      true,
					Steps: []coremodels.FactExtractionStep{{
						Layer:         coremodels.FactExtractionLayerLLM,
						Driver:        "scope_agent_batch_runner",
						Method:        "grounded_batch",
						Deterministic: false,
						LLMUsed:       true,
					}},
				},
			}
			if err := s.factsRepo.Upsert(ctx, fact); err == nil {
				applied = append(applied, "fact:"+fact.ID)
			}
		}
	}
	if s.feedRepo != nil && strings.TrimSpace(proposal.WatchTargetID) != "" {
		title := firstNonEmpty(stringValueFromMap(proposal.Payload, "title"), "Accepted scope-agent proposal")
		summary := firstNonEmpty(stringValueFromMap(proposal.Payload, "summary"), proposal.Explanation)
		if _, err := s.feedRepo.Create(ctx, &horizonmodels.WatchFeedEntry{
			TenantID:       proposal.TenantID,
			TargetID:       proposal.WatchTargetID,
			EntryType:      "proposal_accepted",
			Title:          title,
			Summary:        summary,
			CanonicalPaths: proposal.TargetCanonicalPaths,
			ProposalID:     proposal.ID,
			Metadata: map[string]any{
				"proposalType": proposal.ProposalType,
				"confidence":   proposal.Confidence,
			},
			CreatedAt: now,
		}); err == nil {
			applied = append(applied, "feed:"+proposal.WatchTargetID)
		}
	}
	if s.briefsRepo != nil && s.targetsRepo != nil && strings.TrimSpace(proposal.WatchTargetID) != "" {
		if target, err := s.targetsRepo.Get(ctx, proposal.WatchTargetID); err == nil && target != nil {
			brief := &horizonmodels.WatchBrief{
				ID:             target.ID,
				TenantID:       target.TenantID,
				TargetID:       target.ID,
				CanonicalPath:  target.CanonicalPath,
				Label:          target.Label,
				ScopeType:      target.Kind,
				Headline:       firstNonEmpty(stringValueFromMap(proposal.Payload, "title"), target.Label+" under active watch"),
				Summary:        firstNonEmpty(stringValueFromMap(proposal.Payload, "summary"), proposal.Explanation),
				Hotness:        metricValueFromPayload(proposal.Payload, proposal.Confidence),
				Confidence:     clamp01(proposal.Confidence),
				RiskPosture:    "monitoring",
				DominantThemes: compactUnique(append(target.DomainModes, stringValueFromMap(proposal.Payload, "signalType"))),
				TopSignals:     compactUnique([]string{stringValueFromMap(proposal.Payload, "signalType")}),
				TopEvidence:    sourceIDsFromRefs(proposal.SourceRefs),
				Explanation:    proposal.Explanation,
				Forecast: map[string]any{
					"watchpoints":       sliceStringValueFromMap(proposal.Payload, "watchpoints"),
					"triggerConditions": sliceStringValueFromMap(proposal.Payload, "triggerConditions"),
					"timeHorizon":       stringValueFromMap(proposal.Payload, "timeHorizon"),
				},
				Metadata: map[string]any{
					"proposalId": proposal.ID,
					"updatedAt":  now.Format(time.RFC3339),
				},
			}
			_ = s.briefsRepo.Upsert(ctx, brief)
			target.LastBriefID = brief.ID
			_ = s.targetsRepo.Upsert(ctx, target)
			applied = append(applied, "brief:"+brief.ID)
		}
	}
	return compactUnique(applied), nil
}

func (s *Service) ensureWatchdogConfig(ctx context.Context, tenantID, fragmentID string) error {
	now := time.Now().UTC()
	nextScanAt := now.Add(-1 * time.Minute)
	lastScanAt := now.Add(-2 * time.Hour)
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	cfg, err := s.watchdogRepo.GetByTargetFragmentID(ctx, tenantID, fragmentID)
	if err != nil || cfg == nil {
		cfg = &horizonmodels.WatchdogConfig{
			TenantID:         tenantID,
			TargetFragmentID: fragmentID,
			ScanInterval:     "PT1H",
			TriggerThreshold: 0.10,
			Status:           string(horizonmodels.WatchdogStatusRunning),
			LastScanAt:       &lastScanAt,
			NextScanAt:       &nextScanAt,
		}
		_, err = s.watchdogRepo.Create(ctx, cfg)
		return err
	}
	cfg.Status = string(horizonmodels.WatchdogStatusRunning)
	cfg.ScanInterval = "PT1H"
	cfg.TriggerThreshold = 0.10
	cfg.LastScanAt = &lastScanAt
	cfg.NextScanAt = &nextScanAt
	return s.watchdogRepo.Update(ctx, cfg)
}

func (s *Service) finalizeRuns(ctx context.Context, contexts []*runContext, agentJobID, status string, producedProposalIDs []string, trace map[string]any) {
	now := time.Now().UTC()
	for _, ctxItem := range contexts {
		if ctxItem == nil || ctxItem.Run == nil {
			continue
		}
		ctxItem.Run.AgentJobID = agentJobID
		ctxItem.Run.Status = status
		ctxItem.Run.CompletedAt = &now
		ctxItem.Run.ProducedProposalIDs = compactUnique(producedProposalIDs)
		ctxItem.Run.Trace = mergeMaps(ctxItem.Run.Trace, trace)
		_ = s.runsRepo.Update(ctx, ctxItem.Run)
		if s.statesRepo != nil && ctxItem.Config != nil {
			state := s.getExistingState(ctx, ctxItem.Config)
			state.LastGroundedRunAt = &now
			state.LastRunID = ctxItem.Run.ID
			state.PendingWakeReasons = []string{}
			state.DebounceUntil = nil
			if len(producedProposalIDs) > 0 {
				state.LastAcceptedForecast = map[string]any{
					"proposalIds": producedProposalIDs,
					"updatedAt":   now.Format(time.RFC3339),
				}
			}
			_ = s.statesRepo.Upsert(ctx, state)
		}
	}
}

func (s *Service) markRunsExecuting(ctx context.Context, contexts []*runContext) {
	now := time.Now().UTC()
	for _, ctxItem := range contexts {
		if ctxItem == nil || ctxItem.Run == nil {
			continue
		}
		ctxItem.Run.Status = string(horizonmodels.ScopeAgentRunStatusRunning)
		ctxItem.Run.StartedAt = &now
		ctxItem.Run.Queries = queriesForRunContext(ctxItem)
		_ = s.runsRepo.Update(ctx, ctxItem.Run)
	}
}

func (s *Service) getExistingState(ctx context.Context, cfg *horizonmodels.ScopeAgentConfig) *horizonmodels.ScopeAgentState {
	if cfg == nil {
		return &horizonmodels.ScopeAgentState{}
	}
	state := &horizonmodels.ScopeAgentState{
		ID:            cfg.ID,
		TenantID:      cfg.TenantID,
		ConfigID:      cfg.ID,
		WatchTargetID: cfg.WatchTargetID,
	}
	if s == nil || s.statesRepo == nil || cfg == nil {
		return state
	}
	existing, err := s.statesRepo.GetByConfigID(ctx, cfg.ID)
	if err == nil && existing != nil {
		return existing
	}
	return state
}

func queriesForRunContext(ctxItem *runContext) []string {
	if ctxItem == nil || ctxItem.Target == nil || ctxItem.Config == nil {
		return nil
	}
	label := strings.TrimSpace(firstNonEmpty(ctxItem.Target.Label, ctxItem.Target.CanonicalPath))
	queries := []string{
		label,
		label + " " + strings.Join(compactUnique(ctxItem.Config.DomainModes), " "),
		label + " latest risk signals",
	}
	if reason := strings.TrimSpace(ctxItem.Run.TriggerReason); reason != "" {
		queries = append(queries, label+" "+reason)
	}
	return compactUnique(queries)
}

func (s *Service) emitNoopFeedEntries(ctx context.Context, contexts []*runContext, groundingMeta map[string]any) {
	if s == nil || s.feedRepo == nil {
		return
	}
	now := time.Now().UTC()
	for _, ctxItem := range contexts {
		if ctxItem == nil || ctxItem.Target == nil {
			continue
		}
		_, _ = s.feedRepo.Create(ctx, &horizonmodels.WatchFeedEntry{
			TenantID:       ctxItem.Target.TenantID,
			TargetID:       ctxItem.Target.ID,
			EntryType:      "no_op",
			Title:          fmt.Sprintf("%s checked with no material change", ctxItem.Target.Label),
			Summary:        "Grounded scope-agent run completed without generating accepted proposals.",
			CanonicalPaths: []string{ctxItem.Target.CanonicalPath},
			Metadata:       mergeMaps(map[string]any{"materialChange": false}, groundingMeta),
			CreatedAt:      now,
		})
	}
}

func pickRunContext(contexts []*runContext, watchTargetID string, targetPaths []string) *runContext {
	watchTargetID = strings.TrimSpace(watchTargetID)
	if watchTargetID != "" {
		for _, item := range contexts {
			if item != nil && item.Target != nil && strings.TrimSpace(item.Target.ID) == watchTargetID {
				return item
			}
		}
	}
	for _, path := range targetPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		for _, item := range contexts {
			if item != nil && item.Target != nil && strings.TrimSpace(item.Target.CanonicalPath) == path {
				return item
			}
		}
	}
	if len(contexts) > 0 {
		return contexts[0]
	}
	return nil
}

func batchKey(cfg *horizonmodels.ScopeAgentConfig) string {
	parts := append([]string(nil), cfg.DomainModes...)
	sort.Strings(parts)
	roles := append([]string(nil), cfg.Roles...)
	sort.Strings(roles)
	raw := strings.Join([]string{
		cfg.TenantID,
		cfg.LLMPolicy.PrimaryModel,
		cfg.LLMPolicy.EscalationModel,
		strings.Join(parts, ","),
		strings.Join(roles, ","),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func proposalDedupKey(proposal *horizonmodels.AgentProposal) string {
	paths := append([]string(nil), proposal.TargetCanonicalPaths...)
	sort.Strings(paths)
	raw := strings.Join([]string{
		strings.TrimSpace(proposal.TenantID),
		strings.TrimSpace(proposal.ProposalType),
		strings.TrimSpace(proposal.SignalKey),
		strings.Join(paths, ","),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func factIDForProposal(proposal *horizonmodels.AgentProposal, canonicalPath string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{proposal.ID, canonicalPath, proposal.DedupKey}, "|")))
	return "scope_agent_" + hex.EncodeToString(sum[:8])
}

func defaultLLMPolicy() horizonmodels.LLMPolicy {
	return horizonmodels.LLMPolicy{
		Provider:              "gemini",
		PrimaryModel:          "gemini-3-flash-preview",
		EscalationModel:       "gemini-3-pro-preview",
		FallbackModels:        []string{"gemini-2.5-flash", "gemini-2.5-pro"},
		PreferredVertexModel:  "vertex-gemini-3-flash-preview",
		UseSearchGrounding:    true,
		UseDatastoreGrounding: true,
		BatchWindowSeconds:    180,
		MaxBatchSize:          8,
	}
}

func normalizeRoles(values []string) []string {
	if len(values) == 0 {
		return []string{"monitor", "risk", "forecast"}
	}
	return compactUnique(values)
}

func normalizeScopeType(kind string) string {
	switch kind {
	case "country":
		return "country"
	case "market":
		return "market"
	default:
		return "entity"
	}
}

func normalizePolarity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "opportunity":
		return "opportunity"
	default:
		return "risk"
	}
}

func normalizeSignalType(signalType, polarity string) string {
	signalType = strings.TrimSpace(signalType)
	if signalType != "" && (strings.HasPrefix(signalType, "risk:") || strings.HasPrefix(signalType, "opportunity:")) {
		return signalType
	}
	polarity = normalizePolarity(polarity)
	if signalType == "" {
		return polarity + ":watch_pressure"
	}
	if strings.Contains(signalType, ":") {
		return signalType
	}
	return polarity + ":" + signalType
}

func metricValueForProposal(p groundedProposal) float64 {
	if p.MetricValue > 0 {
		return clamp01(p.MetricValue)
	}
	if p.Impact > 0 && p.Likelihood > 0 {
		return clamp01(p.Impact * p.Likelihood)
	}
	if p.Impact > 0 {
		return clamp01(p.Impact)
	}
	return clamp01(p.Confidence)
}

func metricValueFromPayload(payload map[string]any, fallback float64) float64 {
	if payload == nil {
		return clamp01(fallback)
	}
	if v, ok := payload["metricValue"]; ok {
		switch typed := v.(type) {
		case float64:
			return clamp01(typed)
		case int:
			return clamp01(float64(typed))
		}
	}
	return clamp01(fallback)
}

func metricValueFromMap(payload map[string]any, key string, fallback float64) float64 {
	if payload == nil {
		return clamp01(fallback)
	}
	if v, ok := payload[key]; ok {
		switch typed := v.(type) {
		case float64:
			return clamp01(typed)
		case int:
			return clamp01(float64(typed))
		}
	}
	return clamp01(fallback)
}

func stringValueFromMap(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func sliceStringValueFromMap(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return compactUnique(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return compactUnique(out)
	default:
		return nil
	}
}

func fallbackCanonicalPaths(paths []string, fallback string) []string {
	paths = compactUnique(paths)
	if len(paths) > 0 {
		return paths
	}
	return compactUnique([]string{fallback})
}

func sourceIDsFromRefs(refs []horizonmodels.SourceRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if s := strings.TrimSpace(ref.SourceID); s != "" {
			out = append(out, s)
		}
	}
	return compactUnique(out)
}

func dedupeSourceRefs(refs []horizonmodels.SourceRef) []horizonmodels.SourceRef {
	seen := map[string]struct{}{}
	out := make([]horizonmodels.SourceRef, 0, len(refs))
	for _, ref := range refs {
		key := strings.TrimSpace(ref.SourceType) + "|" + strings.TrimSpace(ref.SourceID)
		if key == "|" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func toCoreSourceRefs(refs []horizonmodels.SourceRef) []coremodels.SourceRef {
	out := make([]coremodels.SourceRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, coremodels.SourceRef{
			SourceID:   strings.TrimSpace(ref.SourceID),
			SourceType: strings.TrimSpace(ref.SourceType),
		})
	}
	return out
}

func formatMetric(v float64) string {
	return fmt.Sprintf("%.2f", clamp01(v))
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

func compactUnique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mergeMaps(base map[string]any, add map[string]any) map[string]any {
	if base == nil && add == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}
