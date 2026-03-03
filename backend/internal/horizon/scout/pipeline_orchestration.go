package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	telemetry "github.com/igorkirisyuk/sanskrite/internal/horizon/telemetrycollector"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	stateKeyPipelineRuntime = "pipelineRuntime"
	stateKeyPipelineIssues  = "pipelineIssues"
	stateKeyStageArtifacts  = "stageArtifacts"
)

const (
	pipelineRuntimeEntity      = "entity"
	pipelineRuntimeDomainBase  = "domain_base_enrichment"
	pipelineRuntimeDomainLayer = "domain_strategic_projection"
)

const (
	stageStatusRunning   = "running"
	stageStatusCompleted = "completed"
	stageStatusFailed    = "failed"
	stageStatusSkipped   = "skipped"
)

const (
	stagePlanning     = "planning"
	stageExtraction   = "extraction"
	stageCritic       = "critic"
	stageResearch     = "research"
	stageRevalidation = "revalidation"
	stageMaterialize  = "materialization"
	stagePolicyAudit  = "policy_audit"
	stageFinalize     = "finalize"
)

const (
	stopKindHard     = "hard"
	stopKindSoft     = "soft"
	stopKindRecovery = "recovery"
)

const (
	stageEntitySourcePlanning         = "source_planning"
	stageEntitySourceExecution        = "source_execution"
	stageEntityExtraction             = "entity_extraction"
	stageEntityResearch               = "entity_research"
	stageEntityRevalidation           = "entity_revalidation"
	stageEntityPersistence            = "entity_persistence"
	stageEntityRelationMaterialize    = "relation_materialization"
	stageEntitySupplyChainMaterialize = "supply_chain_materialization"
	stageEntitySupplyChainRefine      = "supply_chain_refine"

	stageDomainFragmentUpsert      = "domain_fragment_upsert"
	stageDomainRelationMaterialize = "domain_relation_materialization"
	stageDomainSupplyChains        = "domain_supply_chain_materialization"
	stageDomainOverlayExtraction   = "domain_overlay_extraction"
	stageDomainOverlayRelations    = "domain_overlay_relation_materialization"

	stageArtifactPersistence = "artifact_persistence"
)

type StageResult struct {
	Pipeline       string
	Stage          string
	Status         string
	Attempt        int
	IdempotencyKey string
	Required       bool
	Retryable      bool
	Error          string
	StartedAt      string
	EndedAt        string
	Transition     string
}

type pipelineStageBarrierResult struct {
	Ready     bool
	Failed    []string
	Missing   []string
	Completed []string
	Skipped   []string
}

type pipelineFinalizeInput struct {
	Pipeline       string
	RequiredStages []string
	Hint           string
	LegacyHint     string
	CanonicalPath  string
	FragmentID     string
	Confidence     float64
	ForceOutcome   string // optional: TerminalOutcomeFailed to force failed when error returned before finalize
}

func pipelineStageKey(pipelineName, stage string) string {
	return strings.TrimSpace(pipelineName) + ":" + strings.TrimSpace(stage)
}

func pipelineStageOrder(pipelineName string) []string {
	switch strings.TrimSpace(pipelineName) {
	case pipelineRuntimeDomainBase:
		return []string{
			stagePlanning,
			stageExtraction,
			stageCritic,
			stageResearch,
			stageRevalidation,
			stageMaterialize,
			stagePolicyAudit,
			stageFinalize,
		}
	case pipelineRuntimeDomainLayer:
		return []string{
			stagePlanning,
			stageExtraction,
			stageCritic,
			stageResearch,
			stageRevalidation,
			stageMaterialize,
			stagePolicyAudit,
			stageFinalize,
		}
	case pipelineRuntimeEntity:
		return []string{
			stageEntitySourcePlanning,
			stageEntitySourceExecution,
			stageEntityExtraction,
			stageCritic,
			stageEntityResearch,
			stageEntitySupplyChainRefine,
			stageRevalidation,
			stageEntityPersistence,
			stageEntityRelationMaterialize,
			stageEntitySupplyChainMaterialize,
			stageArtifactPersistence,
			stagePolicyAudit,
			stageFinalize,
		}
	default:
		return []string{
			stagePlanning,
			stageExtraction,
			stageCritic,
			stageResearch,
			stageRevalidation,
			stageMaterialize,
			stagePolicyAudit,
			stageFinalize,
		}
	}
}

func normalizePipelineStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case stagePlanning, stageEntitySourcePlanning:
		return stagePlanning
	case stageExtraction, stageEntitySourceExecution, stageEntityExtraction, stageDomainOverlayExtraction, stageDomainFragmentUpsert:
		return stageExtraction
	case stageCritic:
		return stageCritic
	case stageResearch, stageEntityResearch:
		return stageResearch
	case stageEntitySupplyChainRefine:
		return stageEntitySupplyChainRefine
	case stageRevalidation, stageEntityRevalidation:
		return stageRevalidation
	case stageMaterialize,
		stageEntityPersistence,
		stageEntityRelationMaterialize,
		stageEntitySupplyChainMaterialize,
		stageDomainRelationMaterialize,
		stageDomainSupplyChains,
		stageDomainOverlayRelations,
		stageArtifactPersistence:
		return stageMaterialize
	case stagePolicyAudit:
		return stagePolicyAudit
	case stageFinalize:
		return stageFinalize
	default:
		return ""
	}
}

func normalizeRequiredStages(stages []string) []string {
	if len(stages) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(stages))
	for _, st := range stages {
		norm := normalizePipelineStage(st)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func requiredStagesForPipeline(pipelineName string) []string {
	order := pipelineStageOrder(pipelineName)
	out := make([]string, 0, len(order))
	for _, st := range order {
		if st == stageFinalize {
			continue
		}
		out = append(out, st)
	}
	return out
}

func normalizeStopReasonCode(reason string) string {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return ""
	}
	reason = strings.ReplaceAll(reason, "-", "_")
	reason = strings.ReplaceAll(reason, " ", "_")
	reason = strings.Trim(reason, "_")
	if strings.Contains(reason, "circuit_breaker") {
		return "circuit_breaker_open"
	}
	switch {
	case strings.Contains(reason, "research_skipped_no_searchable_gaps"):
		return "research_skipped_no_searchable_gaps"
	case strings.Contains(reason, "research_policy_or_gap_skip"):
		return "research_policy_or_gap_skip"
	case strings.Contains(reason, "fresh_no_new_facts"):
		return "fresh_no_new_facts"
	case strings.Contains(reason, "budget_exhausted_partial"):
		return "budget_exhausted_partial"
	case strings.Contains(reason, "deferred_deep_research"):
		return "deferred_deep_research"
	case strings.Contains(reason, "partial_wait_timeout"):
		return "partial_wait_timeout"
	case strings.Contains(reason, "graph_truncated"):
		return "graph_truncated"
	case strings.Contains(reason, "planning_skip"), strings.Contains(reason, "source_planning_skip"):
		return "planning_policy_skip"
	}
	return reason
}

func deriveStopReasonCode(job *models.ScoutingJob, status, hint string, retryable bool) string {
	if strings.TrimSpace(status) == stageStatusFailed {
		if job != nil && job.State != nil {
			if rc := normalizeStopReasonCode(parseStateString(job.State["normalizationReasonCode"])); rc != "" {
				return rc
			}
		}
	}
	if rc := normalizeStopReasonCode(hint); rc != "" {
		return rc
	}
	switch strings.TrimSpace(status) {
	case stageStatusFailed:
		if retryable {
			return "stage_failed_retryable"
		}
		return "stage_failed"
	case stageStatusSkipped:
		return "stage_skipped"
	case stageStatusCompleted:
		return "stage_completed"
	default:
		return "unknown"
	}
}

func deriveStopKind(reasonCode, status string) string {
	reasonCode = normalizeStopReasonCode(reasonCode)
	switch reasonCode {
	case "deferred_deep_research", "artifact_coverage_expand":
		return stopKindRecovery
	case "budget_exhausted_partial", "budget_exhausted", "circuit_breaker_open", "stage_failed", "non_retryable_contract_error", "non_retryable_stage_failed", "type_required":
		return stopKindHard
	}
	if strings.TrimSpace(status) == stageStatusFailed {
		return stopKindHard
	}
	return stopKindSoft
}

func deriveRecoverable(reasonCode, stopKind string, retryable bool, criticalOpenCount int) bool {
	reasonCode = normalizeStopReasonCode(reasonCode)
	switch stopKind {
	case stopKindRecovery:
		return true
	case stopKindHard:
		if reasonCode == "budget_exhausted_partial" {
			return true
		}
		return retryable
	default:
		if reasonCode == "fresh_no_new_facts" && criticalOpenCount == 0 {
			return false
		}
		return true
	}
}

func deriveCriticalOpenCount(job *models.ScoutingJob) int {
	if job == nil || job.State == nil {
		return 0
	}
	count := parseStateInt(job.State["critical_open_count"], parseStateInt(job.State["criticalOpenCount"], 0))
	if count > 0 {
		return count
	}
	missingInfoN := 0
	switch t := job.State["missingInfo"].(type) {
	case []string:
		missingInfoN = len(t)
	case []interface{}:
		missingInfoN = len(t)
	}
	if missingInfoN > 0 {
		return missingInfoN
	}
	return 0
}

func deriveEvidenceCoverage(job *models.ScoutingJob) float64 {
	if job == nil || job.State == nil {
		return 0
	}
	coverage := parseStateFloatLoose(job.State["evidence_coverage"], parseStateFloatLoose(job.State["evidenceCoverage"], 0))
	if coverage < 0 {
		return 0
	}
	if coverage > 1 {
		return 1
	}
	return coverage
}

func deriveDomainMode(job *models.ScoutingJob) string {
	if job == nil || job.State == nil {
		return ""
	}
	modes := parseStateStringSlice(job.State["domainModes"])
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if mode != "" {
			return mode
		}
	}
	if mode := strings.TrimSpace(parseStateString(job.State["domainMode"])); mode != "" {
		return mode
	}
	return ""
}

func mapTerminalOutcomeToStageStatus(outcome string) string {
	switch strings.TrimSpace(outcome) {
	case TerminalOutcomeFailed:
		return stageStatusFailed
	case TerminalOutcomePartialCompleted:
		return stageStatusSkipped
	default:
		return stageStatusCompleted
	}
}

func ensurePipelineRuntimeState(job *models.ScoutingJob) (map[string]interface{}, map[string]interface{}) {
	if job == nil {
		return nil, nil
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	runtime, _ := job.State[stateKeyPipelineRuntime].(map[string]interface{})
	if runtime == nil {
		runtime = map[string]interface{}{
			"version": "v2",
			"stages":  map[string]interface{}{},
		}
		job.State[stateKeyPipelineRuntime] = runtime
	}
	stages, _ := runtime["stages"].(map[string]interface{})
	if stages == nil {
		stages = map[string]interface{}{}
		runtime["stages"] = stages
	}
	return runtime, stages
}

func (e *ScoutEngine) persistPipelineRuntimeState(ctx context.Context, job *models.ScoutingJob) {
	if e == nil || job == nil || e.scoutingJobsRepo == nil || job.State == nil {
		return
	}
	statePatch := map[string]interface{}{}
	if runtime, ok := job.State[stateKeyPipelineRuntime]; ok {
		statePatch[stateKeyPipelineRuntime] = runtime
	}
	if issues, ok := job.State[stateKeyPipelineIssues]; ok {
		statePatch[stateKeyPipelineIssues] = issues
	}
	if len(statePatch) == 0 {
		return
	}
	_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, statePatch)
}

func stageIsTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case stageStatusCompleted, stageStatusFailed, stageStatusSkipped:
		return true
	default:
		return false
	}
}

func stageIsSatisfiedForDAG(status string) bool {
	switch strings.TrimSpace(status) {
	case stageStatusCompleted, stageStatusSkipped:
		return true
	default:
		return false
	}
}

func indexOfStage(order []string, stage string) int {
	for i, s := range order {
		if strings.TrimSpace(s) == strings.TrimSpace(stage) {
			return i
		}
	}
	return -1
}

// ResolveResumeStage returns the first incomplete stage for checkpoint/resume.
// Used when Cloud Tasks retries: resume from this stage instead of restarting from planning.
// Returns: (firstIncompleteStage, nil) or ("", err) if job is terminal (failed with retryable=false).
func (e *ScoutEngine) ResolveResumeStage(job *models.ScoutingJob, pipelineName string) (string, error) {
	if job == nil {
		return stagePlanning, nil
	}
	_, stages := ensurePipelineRuntimeState(job)
	if stages == nil {
		return stagePlanning, nil
	}
	order := pipelineStageOrder(pipelineName)
	for _, stage := range order {
		if stage == stageFinalize {
			continue
		}
		entryKey := pipelineStageKey(pipelineName, stage)
		entry, _ := stages[entryKey].(map[string]interface{})
		if entry == nil {
			return stage, nil
		}
		status := parseStateString(entry["status"])
		switch status {
		case stageStatusCompleted, stageStatusSkipped:
			continue
		case stageStatusRunning:
			return stage, nil
		case stageStatusFailed:
			retryable := parseStateBool(entry["retryable"], true)
			if !retryable {
				return "", fmt.Errorf("stage %s failed with non-retryable error", stage)
			}
			return stage, nil
		default:
			return stage, nil
		}
	}
	return stageFinalize, nil
}

// UpdateStageArtifacts merges the given updates into job.State["stageArtifacts"].
func (e *ScoutEngine) UpdateStageArtifacts(ctx context.Context, job *models.ScoutingJob, updates map[string]interface{}) {
	if job == nil || len(updates) == 0 {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	artifacts, _ := job.State[stateKeyStageArtifacts].(map[string]interface{})
	if artifacts == nil {
		artifacts = map[string]interface{}{}
	}
	for k, v := range updates {
		artifacts[k] = v
	}
	job.State[stateKeyStageArtifacts] = artifacts
	if e != nil && e.scoutingJobsRepo != nil {
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{stateKeyStageArtifacts: artifacts})
	}
}

// GetStageArtifacts returns job.State["stageArtifacts"] as a map.
func GetStageArtifacts(job *models.ScoutingJob) map[string]interface{} {
	if job == nil || job.State == nil {
		return nil
	}
	m, _ := job.State[stateKeyStageArtifacts].(map[string]interface{})
	return m
}

func buildStageIdempotencyKey(jobID, pipelineName, stage string, attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("%s:%s:%s:%d", strings.TrimSpace(jobID), strings.TrimSpace(pipelineName), strings.TrimSpace(stage), attempt)
}

func (e *ScoutEngine) validatePipelineStageTransition(
	job *models.ScoutingJob,
	pipelineName string,
	stage string,
	status string,
) error {
	if job == nil {
		return nil
	}
	status = strings.TrimSpace(status)
	if status != stageStatusRunning &&
		status != stageStatusCompleted &&
		status != stageStatusSkipped &&
		status != stageStatusFailed {
		return nil
	}
	_, stages := ensurePipelineRuntimeState(job)
	order := pipelineStageOrder(pipelineName)
	idx := indexOfStage(order, stage)
	if idx < 0 {
		return nil
	}
	for i := 0; i < idx; i++ {
		prev := strings.TrimSpace(order[i])
		prevKey := pipelineStageKey(pipelineName, prev)
		entry, _ := stages[prevKey].(map[string]interface{})
		if entry == nil {
			if status == stageStatusSkipped {
				continue
			}
			return fmt.Errorf("stage dag violation: %s requires previous stage %s", stage, prev)
		}
		prevStatus := parseStateString(entry["status"])
		switch status {
		case stageStatusRunning, stageStatusCompleted:
			if !stageIsSatisfiedForDAG(prevStatus) {
				return fmt.Errorf("stage dag violation: %s requires previous stage %s satisfied", stage, prev)
			}
		case stageStatusFailed, stageStatusSkipped:
			if !stageIsTerminalStatus(prevStatus) {
				return fmt.Errorf("stage dag violation: %s requires previous stage %s terminal", stage, prev)
			}
		}
	}
	return nil
}

func (e *ScoutEngine) setPipelineStageStatus(
	ctx context.Context,
	job *models.ScoutingJob,
	pipelineName string,
	stage string,
	status string,
	required bool,
	hint string,
	retryable bool,
	err error,
) (StageResult, error) {
	res := StageResult{
		Pipeline: strings.TrimSpace(pipelineName),
		Stage:    strings.TrimSpace(stage),
		Status:   strings.TrimSpace(strings.ToLower(status)),
		Required: required,
	}
	if job == nil {
		res.Transition = "noop"
		return res, nil
	}
	runtime, stages := ensurePipelineRuntimeState(job)
	if runtime == nil || stages == nil {
		res.Transition = "noop"
		return res, nil
	}
	rawStage := strings.TrimSpace(stage)
	canonicalStage := normalizePipelineStage(rawStage)
	if canonicalStage == "" {
		err := fmt.Errorf("unknown pipeline stage %q", rawStage)
		res.Transition = "rejected"
		res.Error = strings.TrimSpace(err.Error())
		recordPipelineIssue(job, "stage_unknown_"+rawStage, err.Error())
		e.persistPipelineRuntimeState(ctx, job)
		return res, err
	}
	res.Stage = canonicalStage
	if res.Pipeline == "" || res.Stage == "" {
		res.Transition = "noop"
		return res, nil
	}
	if err := e.validatePipelineStageTransition(job, res.Pipeline, res.Stage, res.Status); err != nil {
		res.Transition = "rejected"
		res.Error = strings.TrimSpace(err.Error())
		recordPipelineIssue(job, "stage_dag", err.Error())
		e.persistPipelineRuntimeState(ctx, job)
		return res, err
	}

	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	entryKey := pipelineStageKey(res.Pipeline, res.Stage)
	entry, _ := stages[entryKey].(map[string]interface{})
	if entry == nil {
		entry = map[string]interface{}{}
	}

	prevStatus := parseStateString(entry["status"])
	if prevStatus == stageStatusCompleted && res.Status == stageStatusRunning {
		res.Transition = "noop"
		res.Attempt = parseStateInt(entry["attempt"], 1)
		res.IdempotencyKey = parseStateString(entry["idempotencyKey"])
		res.StartedAt = parseStateString(entry["startedAt"])
		res.EndedAt = parseStateString(entry["endedAt"])
		return res, nil
	}
	// Stage failure is sticky for a given pipeline run; once failed, only another fail update is allowed.
	if prevStatus == stageStatusFailed && res.Status != stageStatusFailed {
		res.Transition = "noop"
		res.Attempt = parseStateInt(entry["attempt"], 1)
		res.IdempotencyKey = parseStateString(entry["idempotencyKey"])
		res.StartedAt = parseStateString(entry["startedAt"])
		res.EndedAt = parseStateString(entry["endedAt"])
		res.Error = parseStateString(entry["error"])
		res.Retryable = parseStateBool(entry["retryable"], true)
		return res, nil
	}

	entry["pipeline"] = res.Pipeline
	entry["stage"] = res.Stage
	entry["substage"] = rawStage
	entry["status"] = res.Status
	if required || parseStateBool(entry["required"], false) {
		entry["required"] = true
	}
	if h := strings.TrimSpace(hint); h != "" {
		entry["hint"] = h
	}
	if parseStateString(entry["startedAt"]) == "" {
		entry["startedAt"] = nowISO
	}
	attempt := parseStateInt(entry["attempt"], 0)
	if res.Status == stageStatusRunning {
		attempt++
		if attempt < 1 {
			attempt = 1
		}
		entry["attempt"] = float64(attempt)
		entry["startedAt"] = nowISO
		entry["endedAt"] = ""
		entry["retryable"] = retryable
		entry["idempotencyKey"] = buildStageIdempotencyKey(strings.TrimSpace(job.ID), res.Pipeline, res.Stage, attempt)
	} else {
		if attempt < 1 {
			attempt = 1
			entry["attempt"] = float64(attempt)
			entry["idempotencyKey"] = buildStageIdempotencyKey(strings.TrimSpace(job.ID), res.Pipeline, res.Stage, attempt)
		}
		if stageIsTerminalStatus(res.Status) {
			entry["endedAt"] = nowISO
		}
		entry["retryable"] = retryable
	}
	if stageIsTerminalStatus(res.Status) {
		reasonCode := deriveStopReasonCode(job, res.Status, hint, retryable)
		criticalOpenCount := deriveCriticalOpenCount(job)
		stopKind := deriveStopKind(reasonCode, res.Status)
		recoverable := deriveRecoverable(reasonCode, stopKind, retryable, criticalOpenCount)
		entry["reason_code"] = reasonCode
		entry["stop_kind"] = stopKind
		entry["recoverable"] = recoverable
		entry["critical_open_count"] = float64(criticalOpenCount)
		entry["evidence_coverage"] = deriveEvidenceCoverage(job)
		entry["domain_mode"] = deriveDomainMode(job)
		entry["pipeline_tier"] = float64(parseStateInt(job.State["pipeline_tier"], parseStateInt(job.State["pipelineTier"], 0)))
		entry["recursion_depth"] = float64(parseStateInt(job.State["recursion_depth"], parseStateInt(job.State["recursionDepth"], 0)))
		if res.Status == stageStatusSkipped && criticalOpenCount > 0 {
			recordPipelineIssue(job, "terminal_skip_with_open_critical",
				fmt.Sprintf("stage=%s reason=%s critical_open_count=%d", res.Stage, reasonCode, criticalOpenCount))
		}
	}
	if err != nil {
		entry["error"] = strings.TrimSpace(err.Error())
		res.Error = strings.TrimSpace(err.Error())
	} else if res.Status == stageStatusCompleted || res.Status == stageStatusSkipped {
		entry["error"] = ""
	}

	stages[entryKey] = entry
	runtime["pipeline"] = res.Pipeline
	runtime["updatedAt"] = nowISO
	job.State[stateKeyPipelineRuntime] = runtime
	e.persistPipelineRuntimeState(ctx, job)

	res.Attempt = attempt
	res.IdempotencyKey = parseStateString(entry["idempotencyKey"])
	res.StartedAt = parseStateString(entry["startedAt"])
	res.EndedAt = parseStateString(entry["endedAt"])
	res.Retryable = parseStateBool(entry["retryable"], false)
	res.Transition = "applied"
	return res, nil
}

func (e *ScoutEngine) pipelineStageBegin(ctx context.Context, job *models.ScoutingJob, pipelineName, stage string, required bool, hint string) {
	if _, err := e.setPipelineStageStatus(ctx, job, pipelineName, stage, stageStatusRunning, required, hint, false, nil); err != nil {
		recordPipelineIssue(job, "stage_begin_"+strings.TrimSpace(stage), err.Error())
		e.persistPipelineRuntimeState(ctx, job)
	}
}

func (e *ScoutEngine) pipelineStageComplete(ctx context.Context, job *models.ScoutingJob, pipelineName, stage string, required bool, hint string) {
	if _, err := e.setPipelineStageStatus(ctx, job, pipelineName, stage, stageStatusCompleted, required, hint, false, nil); err != nil {
		recordPipelineIssue(job, "stage_complete_"+strings.TrimSpace(stage), err.Error())
		e.persistPipelineRuntimeState(ctx, job)
	}
}

func (e *ScoutEngine) pipelineStageSkip(ctx context.Context, job *models.ScoutingJob, pipelineName, stage string, required bool, hint string) {
	if _, err := e.setPipelineStageStatus(ctx, job, pipelineName, stage, stageStatusSkipped, required, hint, false, nil); err != nil {
		recordPipelineIssue(job, "stage_skip_"+strings.TrimSpace(stage), err.Error())
		e.persistPipelineRuntimeState(ctx, job)
	}
}

func (e *ScoutEngine) pipelineStageFail(ctx context.Context, job *models.ScoutingJob, pipelineName, stage string, required bool, hint string, retryable bool, err error) {
	if _, setErr := e.setPipelineStageStatus(ctx, job, pipelineName, stage, stageStatusFailed, required, hint, retryable, err); setErr != nil {
		recordPipelineIssue(job, "stage_fail_"+strings.TrimSpace(stage), setErr.Error())
		e.persistPipelineRuntimeState(ctx, job)
	}
}

func evaluatePipelineStageBarrier(job *models.ScoutingJob, pipelineName string, requiredStages []string) pipelineStageBarrierResult {
	res := pipelineStageBarrierResult{}
	requiredStages = normalizeRequiredStages(requiredStages)
	if job == nil || len(requiredStages) == 0 {
		res.Ready = true
		return res
	}
	runtime, _ := ensurePipelineRuntimeState(job)
	if runtime == nil {
		res.Ready = false
		res.Missing = append(res.Missing, requiredStages...)
		return res
	}
	stages, _ := runtime["stages"].(map[string]interface{})
	if stages == nil {
		res.Ready = false
		res.Missing = append(res.Missing, requiredStages...)
		return res
	}

	for _, stage := range requiredStages {
		stage = strings.TrimSpace(stage)
		if stage == "" {
			continue
		}
		entryKey := pipelineStageKey(strings.TrimSpace(pipelineName), stage)
		entry, _ := stages[entryKey].(map[string]interface{})
		if entry == nil {
			res.Missing = append(res.Missing, stage)
			continue
		}
		switch parseStateString(entry["status"]) {
		case stageStatusCompleted:
			res.Completed = append(res.Completed, stage)
		case stageStatusSkipped:
			res.Skipped = append(res.Skipped, stage)
		case stageStatusFailed:
			res.Failed = append(res.Failed, stage)
		default:
			res.Missing = append(res.Missing, stage)
		}
	}
	res.Ready = len(res.Failed) == 0 && len(res.Missing) == 0
	return res
}

func recordPipelineIssue(job *models.ScoutingJob, issueKey, message string) {
	if job == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	issueKey = strings.TrimSpace(issueKey)
	message = strings.TrimSpace(message)
	if issueKey == "" || message == "" {
		return
	}
	issues, _ := job.State[stateKeyPipelineIssues].(map[string]interface{})
	if issues == nil {
		issues = map[string]interface{}{}
	}
	issues[issueKey] = map[string]interface{}{
		"message":   message,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	job.State[stateKeyPipelineIssues] = issues
}

func collectPipelineDegradation(job *models.ScoutingJob) (bool, map[string]interface{}) {
	if job == nil || job.State == nil {
		return false, nil
	}
	details := map[string]interface{}{}
	degraded := false

	if issues, ok := job.State[stateKeyPipelineIssues].(map[string]interface{}); ok && len(issues) > 0 {
		details["pipelineIssues"] = issues
		degraded = true
	}

	if parseStateBool(job.State["edgeBatchFlushError"], false) {
		details["edgeBatchFlushError"] = true
		if msg := parseStateString(job.State["edgeBatchFlushErrorMessage"]); msg != "" {
			details["edgeBatchFlushErrorMessage"] = msg
		}
		degraded = true
	}

	if summary, ok := job.State["supplyChainSummary"].(map[string]interface{}); ok && summary != nil {
		edgeErrors := parseStateInt(summary["edgeErrors"], 0)
		fragmentErrors := parseStateInt(summary["fragmentErrors"], 0)
		if edgeErrors > 0 || fragmentErrors > 0 {
			details["supplyChainErrors"] = map[string]interface{}{
				"edgeErrors":     edgeErrors,
				"fragmentErrors": fragmentErrors,
			}
			degraded = true
		}
	}

	if parseStateBool(job.State["policyPersistError"], false) || parseStateBool(job.State["policyPersistDlq"], false) {
		details["policyPersistError"] = true
		degraded = true
	}

	return degraded, details
}

func requiredStagesForPipelinePass(passName pipeline.PassName) (string, []string) {
	switch passName {
	case pipeline.PassStrategicProjection:
		return pipelineRuntimeDomainLayer, requiredStagesForPipeline(pipelineRuntimeDomainLayer)
	case pipeline.PassBaseEnrichment:
		fallthrough
	default:
		return pipelineRuntimeDomainBase, requiredStagesForPipeline(pipelineRuntimeDomainBase)
	}
}

func (e *ScoutEngine) finalizePipelineRun(ctx context.Context, job *models.ScoutingJob, in pipelineFinalizeInput) error {
	if e == nil || job == nil {
		return nil
	}
	if isTerminalScoutJob(job) {
		return nil
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	pipelineName := strings.TrimSpace(in.Pipeline)
	if pipelineName == "" {
		pipelineName = pipelineRuntimeEntity
	}
	requiredStages := in.RequiredStages
	if len(requiredStages) == 0 {
		requiredStages = requiredStagesForPipeline(pipelineName)
	}
	requiredStages = normalizeRequiredStages(requiredStages)

	e.pipelineStageBegin(ctx, job, pipelineName, stageFinalize, true, "finalize")
	barrier := evaluatePipelineStageBarrier(job, pipelineName, requiredStages)
	degraded, degradeDetails := collectPipelineDegradation(job)

	outcome := TerminalOutcomeCompleted
	if strings.TrimSpace(in.ForceOutcome) == TerminalOutcomeFailed {
		outcome = TerminalOutcomeFailed
	} else {
		switch {
		case len(barrier.Failed) > 0:
			outcome = TerminalOutcomeFailed
		case len(barrier.Missing) > 0:
			// Mandatory barrier not met -> partial_failed semantics.
			outcome = TerminalOutcomePartialCompleted
		case degraded:
			outcome = TerminalOutcomePartialCompleted
		}
	}

	hint := strings.TrimSpace(in.Hint)
	if hint == "" {
		hint = "pipeline_completed"
	}
	legacyHint := strings.TrimSpace(in.LegacyHint)
	if legacyHint == "" {
		legacyHint = hint
	}

	reasonMap := map[string]interface{}{
		"pipeline": pipelineName,
		"stages": map[string]interface{}{
			"required":  requiredStages,
			"completed": barrier.Completed,
			"skipped":   barrier.Skipped,
			"missing":   barrier.Missing,
			"failed":    barrier.Failed,
		},
	}
	if len(barrier.Failed) > 0 || strings.TrimSpace(in.ForceOutcome) == TerminalOutcomeFailed {
		if len(barrier.Failed) > 0 {
			reasonMap["failed_stage"] = barrier.Failed[0]
		} else {
			reasonMap["failed_stage"] = "unknown"
		}
		if rc := parseStateString(job.State["normalizationReasonCode"]); rc != "" {
			reasonMap["reason_code"] = rc
		} else if strings.TrimSpace(in.ForceOutcome) == TerminalOutcomeFailed {
			reasonMap["reason_code"] = "non_retryable_contract_error"
		} else {
			reasonMap["reason_code"] = "stage_failed"
		}
	} else if rc := parseStateString(job.State["normalizationReasonCode"]); rc != "" {
		reasonMap["reason_code"] = rc
	}
	reasonCode := normalizeStopReasonCode(parseStateString(reasonMap["reason_code"]))
	if reasonCode == "" {
		reasonCode = deriveStopReasonCode(job, mapTerminalOutcomeToStageStatus(outcome), hint, false)
	}
	criticalOpenCount := deriveCriticalOpenCount(job)
	stopKind := deriveStopKind(reasonCode, mapTerminalOutcomeToStageStatus(outcome))
	recoverable := deriveRecoverable(reasonCode, stopKind, false, criticalOpenCount)
	evidenceCoverage := deriveEvidenceCoverage(job)
	if outcome != TerminalOutcomeFailed && criticalOpenCount > 0 && stopKind == stopKindSoft {
		recordPipelineIssue(job, "terminal_skip_with_open_critical",
			fmt.Sprintf("pipeline=%s reason=%s critical_open_count=%d", pipelineName, reasonCode, criticalOpenCount))
		// Contract assert: unresolved critical gaps can not end as fully completed.
		if outcome == TerminalOutcomeCompleted {
			outcome = TerminalOutcomePartialCompleted
		}
		recoverable = true
	}
	reasonMap["reason_code"] = reasonCode
	reasonMap["stop_kind"] = stopKind
	reasonMap["recoverable"] = recoverable
	reasonMap["critical_open_count"] = criticalOpenCount
	reasonMap["evidence_coverage"] = evidenceCoverage
	reasonMap["domain_mode"] = deriveDomainMode(job)
	reasonMap["pipeline_tier"] = parseStateInt(job.State["pipeline_tier"], parseStateInt(job.State["pipelineTier"], 0))
	reasonMap["recursion_depth"] = parseStateInt(job.State["recursion_depth"], parseStateInt(job.State["recursionDepth"], 0))
	if degraded && len(degradeDetails) > 0 {
		reasonMap["degradations"] = degradeDetails
	}

	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	job.State["terminalOutcome"] = outcome
	job.State["terminalOutcomeReasons"] = reasonMap
	job.State["terminalizedAt"] = nowISO
	e.persistPipelineRuntimeState(ctx, job)
	if e.scoutingJobsRepo != nil {
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
			"terminalOutcome":        outcome,
			"terminalOutcomeReasons": reasonMap,
			"terminalizedAt":         nowISO,
		})
	}

	canonicalPath := strings.TrimSpace(in.CanonicalPath)
	if canonicalPath == "" {
		canonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	confidence := in.Confidence
	if confidence <= 0 {
		confidence = 1.0
	}

	switch outcome {
	case TerminalOutcomeFailed:
		if err := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, hint); err != nil {
			return err
		}
	case TerminalOutcomePartialCompleted:
		if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, hint); err != nil {
			return err
		}
		job.Status = coreModels.AgentJobStatusPartial
		if e.scoutingJobsRepo != nil {
			if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
				return fmt.Errorf("failed to persist partial status: %w", err)
			}
		}
	default:
		if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, hint); err != nil {
			return err
		}
	}

	completedPayload := map[string]interface{}{
		"tenantId":            job.TenantID,
		"projectId":           job.ProjectID,
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               job.ID,
		"fragmentId":          strings.TrimSpace(in.FragmentID),
		"canonicalPath":       canonicalPath,
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"confidence":          confidence,
		"terminalOutcome":     outcome,
		"reasonMap":           reasonMap,
		"hint":                hint,
		"legacyHint":          legacyHint,
	}
	if _, err := e.enqueueScoutOutboxIntent(ctx, job, scoutOutboxTypeWSEvent, map[string]interface{}{
		"eventType": string("horizon.scout.job.completed"),
		"tenantId":  job.TenantID,
		"payload":   completedPayload,
	}); err != nil {
		recordPipelineIssue(job, "outbox_enqueue_completed", err.Error())
	}
	if _, err := e.enqueueScoutOutboxIntent(ctx, job, scoutOutboxTypeAssemblyCheck, map[string]interface{}{
		"tenantId":  job.TenantID,
		"projectId": job.ProjectID,
		"lensId":    strings.TrimSpace(job.LensID),
	}); err != nil {
		recordPipelineIssue(job, "outbox_enqueue_assembly", err.Error())
	}
	e.pipelineStageComplete(ctx, job, pipelineName, stageFinalize, true, "finalize_done")
	e.triggerScoutOutboxDispatch(job)

	if e.jobTelemetryRepo != nil {
		if telCol := telemetry.CollectorFromContext(ctx); telCol != nil {
			_ = telCol.Flush(ctx, string(outcome), e.jobTelemetryRepo.Save)
		}
	}

	return nil
}
