package scout

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// TestContractNormalizer_NonRetryableContractError verifies that PreCreateValidator block
// yields Retryable=false and appropriate ReasonCode.
func TestContractNormalizer_NonRetryableContractError(t *testing.T) {
	ctx := context.Background()
	n := &ContractNormalizer{
		PreCreateValidator: &blockingPreCreateValidator{reason: "test_block"},
	}

	base := &models.HorizonFragment{
		CanonicalPath: "/org/us/acme",
		Class:          FragmentTypeOrganization,
		Governance:    models.Governance{Owner: "SYSTEM", Status: "ACTIVE"},
		TenantID:      "global",
	}

	result := n.Normalize(ctx, base, nil, "/org/us/acme")
	if result.Error == nil {
		t.Fatal("expected Normalize to return error when PreCreateValidator blocks")
	}
	if result.Retryable {
		t.Fatalf("expected Retryable=false for pre-create block, got true")
	}
	if result.ReasonCode == "" {
		t.Fatalf("expected non-empty ReasonCode, got %q", result.ReasonCode)
	}
}

type blockingPreCreateValidator struct{ reason string }

func (b *blockingPreCreateValidator) ValidateAndProcess(_ context.Context, _ *models.HorizonFragment) (*ValidationResult, error) {
	return &ValidationResult{IsValid: false, BlockReason: b.reason}, nil
}

// TestPipelineStageFail_RetryableFlag verifies pipelineStageFail stores retryable correctly.
func TestPipelineStageFail_RetryableFlag(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := newPipelineContractTestJob("job-retryable-flag")
	job.State = map[string]interface{}{}

	// Complete prior stages so we can fail materialization (DAG requires previous stages terminal).
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stagePlanning, true, "planning_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageExtraction, true, "extraction_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageCritic, true, "critic_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageResearch, true, "research_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageRevalidation, true, "revalidation_done")

	engine.pipelineStageFail(ctx, job, pipelineRuntimeEntity, stageMaterialize, true, "contract_error", false, fmt.Errorf("type is required"))

	_, stages := ensurePipelineRuntimeState(job)
	if stages == nil {
		t.Fatal("expected stages in pipelineRuntime")
	}
	matKey := pipelineStageKey(pipelineRuntimeEntity, stageMaterialize)
	mat, _ := stages[matKey].(map[string]interface{})
	if mat == nil {
		t.Fatalf("expected materialization stage entry (key=%q)", matKey)
	}
	if v, ok := mat["retryable"].(bool); !ok || v {
		t.Fatalf("expected retryable=false for contract error, got %v", mat["retryable"])
	}
}

// TestFinalizePipelineRun_ReasonCode verifies terminalOutcomeReasons includes failed_stage and reason_code.
func TestFinalizePipelineRun_ReasonCode(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := newPipelineContractTestJob("job-reason-code")
	job.State = map[string]interface{}{
		"normalizationReasonCode": ReasonCodeTypeRequired,
		"critical_open_count":     0.0,
		"evidence_coverage":       0.75,
	}
	// Complete prior stages, then fail materialization.
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stagePlanning, true, "planning_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageExtraction, true, "extraction_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageCritic, true, "critic_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageResearch, true, "research_done")
	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageRevalidation, true, "revalidation_done")
	engine.pipelineStageFail(ctx, job, pipelineRuntimeEntity, stageMaterialize, true, "contract_error", false, fmt.Errorf("type is required"))

	if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
		Pipeline:       pipelineRuntimeEntity,
		RequiredStages: requiredStagesForPipeline(pipelineRuntimeEntity),
		Hint:           "finalize_contract_fail",
	}); err != nil {
		t.Fatalf("finalize should not error: %v", err)
	}
	if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomeFailed {
		t.Fatalf("expected terminalOutcome=failed, got %q", got)
	}
	reasons, _ := job.State["terminalOutcomeReasons"].(map[string]interface{})
	if reasons == nil {
		t.Fatal("expected terminalOutcomeReasons")
	}
	if got := parseStateString(reasons["failed_stage"]); got != stageMaterialize {
		t.Fatalf("expected failed_stage=%q, got %q", stageMaterialize, got)
	}
	if got := parseStateString(reasons["reason_code"]); got != ReasonCodeTypeRequired && got != "stage_failed" {
		t.Fatalf("expected reason_code=type_required or stage_failed, got %q", got)
	}
	if got := parseStateString(reasons["stop_kind"]); got != stopKindHard {
		t.Fatalf("expected stop_kind=%q, got %q", stopKindHard, got)
	}
	if recoverable, ok := reasons["recoverable"].(bool); !ok || recoverable {
		t.Fatalf("expected recoverable=false for hard failed outcome, got %v", reasons["recoverable"])
	}
	if got := parseStateInt(reasons["critical_open_count"], -1); got != 0 {
		t.Fatalf("expected critical_open_count=0, got %d", got)
	}
}

// TestFinalizePipelineRun_ForceOutcome verifies ForceOutcome=TerminalOutcomeFailed.
func TestFinalizePipelineRun_ForceOutcome(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := newPipelineContractTestJob("job-force-failed")
	job.State = map[string]interface{}{}

	if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
		Pipeline:     pipelineRuntimeEntity,
		Hint:         "non-retryable contract error",
		ForceOutcome: TerminalOutcomeFailed,
	}); err != nil {
		t.Fatalf("finalize should not error: %v", err)
	}
	if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomeFailed {
		t.Fatalf("expected terminalOutcome=failed, got %q", got)
	}
	reasons, _ := job.State["terminalOutcomeReasons"].(map[string]interface{})
	if reasons == nil {
		t.Fatal("expected terminalOutcomeReasons")
	}
	if got := parseStateString(reasons["reason_code"]); got != "non_retryable_contract_error" {
		t.Fatalf("expected reason_code=non_retryable_contract_error, got %q", got)
	}
	if got := parseStateString(reasons["stop_kind"]); got != stopKindHard {
		t.Fatalf("expected stop_kind=%q, got %q", stopKindHard, got)
	}
}

func TestFinalizePipelineRun_OpenCriticalForcesPartial(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := newPipelineContractTestJob("job-open-critical")
	job.State = map[string]interface{}{
		"critical_open_count": 2.0,
		"evidence_coverage":   0.2,
	}

	if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
		Pipeline:       pipelineRuntimeEntity,
		RequiredStages: []string{},
		Hint:           "pipeline_completed",
	}); err != nil {
		t.Fatalf("finalize should not error: %v", err)
	}
	if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomePartialCompleted {
		t.Fatalf("expected terminalOutcome=%q, got %q", TerminalOutcomePartialCompleted, got)
	}
	reasons, _ := job.State["terminalOutcomeReasons"].(map[string]interface{})
	if reasons == nil {
		t.Fatal("expected terminalOutcomeReasons")
	}
	if got := parseStateInt(reasons["critical_open_count"], 0); got < 1 {
		t.Fatalf("expected critical_open_count>0, got %d", got)
	}
	if got := parseStateString(reasons["stop_kind"]); got != stopKindSoft {
		t.Fatalf("expected stop_kind=%q for pipeline_completed hint, got %q", stopKindSoft, got)
	}
	if recoverable, ok := reasons["recoverable"].(bool); !ok || !recoverable {
		t.Fatalf("expected recoverable=true when open critical gaps remain, got %v", reasons["recoverable"])
	}
}
