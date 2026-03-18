package scout

import (
	"context"
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestShouldBypassDomainAnswerAssembly_HotPathOnlySupplyChain(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"supplyChainGraphBuildMode": agents.SupplyChainGraphBuildModeHotPathOnly,
				"domains": []map[string]interface{}{
					{
						"canonicalPath": "/domain/v1/supply-chain/geo/us/product/iphone/subject/apple-inc",
						"contractType":  "supply-chain",
						"supplyChainExecutionPlan": map[string]interface{}{
							"graphBuildMode": agents.SupplyChainGraphBuildModeHotPathOnly,
							"queries": []map[string]interface{}{
								{"anchor": "/org/us/apple-inc", "anchorType": "org", "direction": "both", "depth": 4, "reason": "test"},
							},
						},
					},
				},
			},
		},
	}
	contract := &hmodels.Contract{
		CanonicalPath: "/domain/v1/supply-chain/geo/us/product/iphone/subject/apple-inc",
		ContractType:  "supply-chain",
	}
	if !shouldBypassDomainAnswerAssembly(job, contract) {
		t.Fatalf("expected hot_path_only supply-chain job to bypass domain answer assembly")
	}
}

func TestShouldBypassDomainAnswerAssembly_DefaultGraphBuildDoesNotBypass(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"supplyChainGraphBuildMode": agents.SupplyChainGraphBuildModeHotPathPlusGraph,
			},
		},
	}
	contract := &hmodels.Contract{
		CanonicalPath: "/domain/v1/supply-chain/geo/us/product/iphone/subject/apple-inc",
		ContractType:  "supply-chain",
	}
	if shouldBypassDomainAnswerAssembly(job, contract) {
		t.Fatalf("expected hot_path_plus_graph supply-chain job to keep domain answer assembly")
	}
}

func TestFinalizeHotPathOnlyDomainContract_CompletesJob(t *testing.T) {
	ctx := context.Background()
	engine := &ScoutEngine{}
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			ID:        "job-hot-path-finalize",
			TenantID:  "tenant-test",
			ProjectID: "project-test",
			Status:    coremodels.AgentJobStatusRunning,
			State: map[string]interface{}{
				"domainModes":               []interface{}{"supply_chain"},
				"supplyChainPlanMode":       true,
				"supplyChainGraphBuildMode": agents.SupplyChainGraphBuildModeHotPathOnly,
			},
		},
		TargetCanonicalPath: "/domain/v1/supply-chain/geo/us/product/iphone/subject/apple-inc",
	}
	pipelineName, requiredStages := requiredStagesForPipelinePassJob(job, pipeline.PassBaseEnrichment)
	for _, stage := range requiredStages {
		switch stage {
		case stageCritic, stageRevalidation, stagePolicyAudit:
			engine.pipelineStageSkip(ctx, job, pipelineName, stage, true, stage+"_skip")
		default:
			engine.pipelineStageComplete(ctx, job, pipelineName, stage, true, stage+"_done")
		}
	}
	job.State["supplyChainAssembly"] = map[string]interface{}{"status": "partial"}

	if err := engine.finalizeHotPathOnlyDomainContract(ctx, job, job.TargetCanonicalPath); err != nil {
		t.Fatalf("finalizeHotPathOnlyDomainContract returned error: %v", err)
	}
	if got := parseStateString(job.State["terminalOutcome"]); got != TerminalOutcomeCompleted {
		t.Fatalf("terminalOutcome=%q, want %q", got, TerminalOutcomeCompleted)
	}
	if job.Status != coremodels.AgentJobStatusCompleted {
		t.Fatalf("status=%q, want %q", job.Status, coremodels.AgentJobStatusCompleted)
	}
	if job.CurrentState != models.ScoutingJobStateVerified {
		t.Fatalf("currentState=%q, want %q", job.CurrentState, models.ScoutingJobStateVerified)
	}
}
