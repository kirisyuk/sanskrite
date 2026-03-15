package scout

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func newPipelineContractTestJob(id string) *models.ScoutingJob {
	return &models.ScoutingJob{
		AgentJob: coreModels.AgentJob{
			ID:        id,
			TenantID:  "tenant-test",
			ProjectID: "project-test",
			Status:    coreModels.AgentJobStatusRunning,
			State:     map[string]interface{}{},
		},
		LensID:              "lens-test",
		TargetCanonicalPath: "/org/us/acme",
	}
}

func newSupplyChainDomainPipelineContractTestJob(id string) *models.ScoutingJob {
	job := newPipelineContractTestJob(id)
	job.TargetCanonicalPath = "/domain/supply-chain/test-smartphone"
	job.State["domainModes"] = []interface{}{taxonomy.DomainModeSupplyChain}
	return job
}

func TestPipelineStageIdempotencyAndStickyFailure(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := newPipelineContractTestJob("job-stage-idempotency")

	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stagePlanning, true, "planning_done")

	first, err := engine.setPipelineStageStatus(ctx, job, pipelineRuntimeEntity, stageEntitySourceExecution, stageStatusRunning, true, "execution", true, nil)
	if err != nil {
		t.Fatalf("unexpected error starting stage: %v", err)
	}
	if first.Stage != stageExtraction {
		t.Fatalf("expected canonical stage %q, got %q", stageExtraction, first.Stage)
	}
	if first.Attempt != 1 {
		t.Fatalf("expected attempt=1, got %d", first.Attempt)
	}
	if first.IdempotencyKey == "" {
		t.Fatal("expected non-empty idempotency key")
	}

	engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stageEntitySourceExecution, true, "execution_done")

	restart, err := engine.setPipelineStageStatus(ctx, job, pipelineRuntimeEntity, stageEntitySourceExecution, stageStatusRunning, true, "execution_retry", true, nil)
	if err != nil {
		t.Fatalf("unexpected error on completed->running: %v", err)
	}
	if restart.Transition != "noop" {
		t.Fatalf("expected noop transition after completion, got %q", restart.Transition)
	}
	if restart.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("expected idempotency key to remain stable on noop restart, got %q vs %q", restart.IdempotencyKey, first.IdempotencyKey)
	}

	failErr := fmt.Errorf("forced extraction failure")
	engine.pipelineStageFail(ctx, job, pipelineRuntimeEntity, stageEntitySourceExecution, true, "execution_failed", true, failErr)
	afterFail, err := engine.setPipelineStageStatus(ctx, job, pipelineRuntimeEntity, stageEntitySourceExecution, stageStatusCompleted, true, "execution_completed_after_failure", false, nil)
	if err != nil {
		t.Fatalf("unexpected error after sticky failure: %v", err)
	}
	if afterFail.Transition != "noop" {
		t.Fatalf("expected noop transition after sticky failure, got %q", afterFail.Transition)
	}
	if afterFail.Error == "" {
		t.Fatal("expected sticky failure error to be preserved")
	}
}

func TestFinalizePipelineRunOutcomeFromBarrier(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}

	t.Run("missing required stage yields partial outcome", func(t *testing.T) {
		job := newPipelineContractTestJob("job-finalize-partial")
		engine.pipelineStageComplete(ctx, job, pipelineRuntimeEntity, stagePlanning, true, "planning_done")

		if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
			Pipeline:       pipelineRuntimeEntity,
			RequiredStages: []string{stagePlanning, stageExtraction},
			Hint:           "finalize_missing_stage",
		}); err != nil {
			t.Fatalf("finalize should not error for partial outcome: %v", err)
		}
		if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomePartialCompleted {
			t.Fatalf("expected terminalOutcome=%q, got %q", TerminalOutcomePartialCompleted, got)
		}
	})

	t.Run("failed required stage yields failed outcome", func(t *testing.T) {
		job := newPipelineContractTestJob("job-finalize-failed")
		engine.pipelineStageFail(ctx, job, pipelineRuntimeEntity, stagePlanning, true, "planning_failed", true, fmt.Errorf("planning failed"))

		if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
			Pipeline:       pipelineRuntimeEntity,
			RequiredStages: []string{stagePlanning},
			Hint:           "finalize_failed_stage",
		}); err != nil {
			t.Fatalf("finalize should not error for failed outcome: %v", err)
		}
		if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomeFailed {
			t.Fatalf("expected terminalOutcome=%q, got %q", TerminalOutcomeFailed, got)
		}
	})
}

func TestRequiredStagesForPipelinePassJob_SupplyChainDomainIncludesFastAssembly(t *testing.T) {
	job := newSupplyChainDomainPipelineContractTestJob("job-required-stages-supply-chain")

	pipelineName, requiredStages := requiredStagesForPipelinePassJob(job, pipeline.PassBaseEnrichment)
	if pipelineName != pipelineRuntimeDomainBase {
		t.Fatalf("pipelineName=%q, want %q", pipelineName, pipelineRuntimeDomainBase)
	}
	found := false
	for _, stage := range requiredStages {
		if stage == stageEntitySupplyChainFastAssembly {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("required stages %v must include %q", requiredStages, stageEntitySupplyChainFastAssembly)
	}
}

func TestPipelineStageOrder_EntityPipelineDoesNotRequireRefine(t *testing.T) {
	for _, stage := range requiredStagesForPipeline(pipelineRuntimeEntity) {
		if stage == stageEntitySupplyChainRefine {
			t.Fatalf("entity pipeline stages must not include %q", stageEntitySupplyChainRefine)
		}
	}
}

func TestFinalizePipelineRun_SupplyChainDomainRequiresAssemblySummary(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	setupJob := func(id string) *models.ScoutingJob {
		job := newSupplyChainDomainPipelineContractTestJob(id)
		for _, stage := range []string{
			stagePlanning,
			stageExtraction,
			stageCritic,
			stageResearch,
			stageRevalidation,
			stageMaterialize,
			stagePolicyAudit,
			stageEntitySupplyChainFastAssembly,
		} {
			engine.pipelineStageComplete(ctx, job, pipelineRuntimeDomainBase, stage, true, stage+"_done")
		}
		return job
	}

	t.Run("missing summary yields partial outcome", func(t *testing.T) {
		job := setupJob("job-finalize-supply-chain-summary-missing")
		if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
			Pipeline: pipelineRuntimeDomainBase,
			Hint:     "finalize_supply_chain_missing_summary",
		}); err != nil {
			t.Fatalf("finalize should not error: %v", err)
		}
		if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomePartialCompleted {
			t.Fatalf("terminalOutcome=%q, want %q when summary missing", got, TerminalOutcomePartialCompleted)
		}
	})

	t.Run("summary allows completed outcome", func(t *testing.T) {
		job := setupJob("job-finalize-supply-chain-summary-present")
		job.State["supplyChainAssembly"] = map[string]interface{}{"status": "partial"}
		if err := engine.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
			Pipeline: pipelineRuntimeDomainBase,
			Hint:     "finalize_supply_chain_with_summary",
		}); err != nil {
			t.Fatalf("finalize with summary should not error: %v", err)
		}
		if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomeCompleted {
			t.Fatalf("terminalOutcome=%q, want %q when summary exists", got, TerminalOutcomeCompleted)
		}
	})
}

func TestRequiredStagesForPipelinePassJob_MixedDomainJobSkipsFastAssemblyBarrier(t *testing.T) {
	job := newSupplyChainDomainPipelineContractTestJob("job-required-stages-mixed-domain")
	job.State["domains"] = []map[string]interface{}{
		{"contractType": "supply-chain"},
		{"contractType": "ownership-structure"},
	}

	_, requiredStages := requiredStagesForPipelinePassJob(job, pipeline.PassBaseEnrichment)
	for _, stage := range requiredStages {
		if stage == stageEntitySupplyChainFastAssembly {
			t.Fatalf("mixed-domain job must not require %q", stageEntitySupplyChainFastAssembly)
		}
	}
}
