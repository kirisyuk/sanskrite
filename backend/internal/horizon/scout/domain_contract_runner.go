package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

type domainContractEventPublisher struct {
	engine *ScoutEngine
}

func (p domainContractEventPublisher) Publish(ctx context.Context, eventType string, data map[string]interface{}) {
	if p.engine == nil {
		return
	}
	p.engine.publish(ctx, realtime.EventType(strings.TrimSpace(eventType)), data)
}

// RunDomainContract executes DomainContractEngine via the existing Scout infrastructure.
// This is used by pipeline ContractHandler adapters for domain contract types.
func (e *ScoutEngine) RunDomainContract(ctx context.Context, in pipeline.ContractInput) (*models.ViewDirective, error) {
	if e == nil {
		return nil, fmt.Errorf("scout engine is nil")
	}
	if in.Job == nil {
		return nil, fmt.Errorf("contract input job is nil")
	}
	job := in.Job
	trace.Log(ctx, "domain_contract", "domain_contract.runner.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"effectivePathInput":  strings.TrimSpace(in.EffectiveCanonicalPath),
	})
	if e.fragmentsRepo == nil || e.edgesRepo == nil {
		trace.Log(ctx, "domain_contract", "domain_contract.runner.error", map[string]any{
			"tenantId":  strings.TrimSpace(job.TenantID),
			"projectId": strings.TrimSpace(job.ProjectID),
			"lensId":    strings.TrimSpace(job.LensID),
			"jobId":     strings.TrimSpace(job.ID),
			"error":     "domain contract requires fragments and edges repositories",
		})
		return nil, fmt.Errorf("domain contract requires fragments and edges repositories")
	}

	effectiveCanonicalPath := strings.TrimSpace(in.EffectiveCanonicalPath)
	if effectiveCanonicalPath == "" {
		effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	effectiveCanonicalPath = NormalizeRuntimeDomainCanonicalPath(effectiveCanonicalPath)

	contract := horizondomain.Contract{}
	if in.Contract != nil {
		contract = horizondomain.Contract{
			CanonicalPath:      NormalizeRuntimeDomainCanonicalPath(strings.TrimSpace(in.Contract.CanonicalPath)),
			ContractType:       strings.TrimSpace(strings.ToLower(in.Contract.ContractType)),
			Title:              strings.TrimSpace(in.Contract.Title),
			Description:        strings.TrimSpace(in.Contract.Description),
			Facets:             in.Contract.Facets,
			FirstOrderEntities: append([]string(nil), in.Contract.FirstOrderEntities...),
			RelatedDomains:     append([]string(nil), in.Contract.RelatedDomains...),
			AffectedDomains:    append([]string(nil), in.Contract.AffectedDomains...),
		}
	} else if c := e.bestEffortFindDomainContract(job, effectiveCanonicalPath); c != nil {
		contract = horizondomain.Contract{
			CanonicalPath:      NormalizeRuntimeDomainCanonicalPath(strings.TrimSpace(c.CanonicalPath)),
			ContractType:       strings.TrimSpace(strings.ToLower(c.ContractType)),
			Title:              strings.TrimSpace(c.Title),
			Description:        strings.TrimSpace(c.Description),
			Facets:             c.Facets,
			FirstOrderEntities: append([]string(nil), c.FirstOrderEntities...),
			RelatedDomains:     append([]string(nil), c.RelatedDomains...),
			AffectedDomains:    append([]string(nil), c.AffectedDomains...),
		}
	}

	if contract.CanonicalPath == "" {
		contract.CanonicalPath = effectiveCanonicalPath
	}
	if contract.CanonicalPath == "" {
		contract.CanonicalPath = NormalizeRuntimeDomainCanonicalPath(strings.TrimSpace(job.TargetCanonicalPath))
	}
	contract.CanonicalPath = NormalizeRuntimeDomainCanonicalPath(contract.CanonicalPath)

	if contract.CanonicalPath != "" && contract.ContractType == "" {
		parts := strings.Split(strings.Trim(contract.CanonicalPath, "/"), "/")
		if len(parts) >= 3 && strings.EqualFold(parts[0], "domain") && strings.EqualFold(parts[1], "v1") {
			contract.ContractType = strings.TrimSpace(strings.ToLower(parts[2]))
		}
	}

	if job.State != nil {
		for _, key := range []string{"temporalStart", "temporalHorizonStart", "fromDate"} {
			raw := parseStateString(job.State[key])
			if raw == "" {
				continue
			}
			if t := parseOptionalISODate(&raw); t != nil {
				contract.TemporalStart = t
				break
			}
		}
		for _, key := range []string{"temporalEnd", "temporalHorizonEnd", "toDate"} {
			raw := parseStateString(job.State[key])
			if raw == "" {
				continue
			}
			if t := parseOptionalISODate(&raw); t != nil {
				contract.TemporalEnd = t
				break
			}
		}
		if polarity := parseStateStringSlice(job.State["polarity"]); len(polarity) > 0 {
			contract.Polarity = polarity
		}
	}

	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	prevInline, hadInline := job.State["inlineDomainContractRun"]
	prevSkipAuto, hadSkipAuto := job.State["skipAutoStrategicOverlay"]
	job.State["inlineDomainContractRun"] = true
	job.State["skipAutoStrategicOverlay"] = true
	defer func() {
		if hadInline {
			job.State["inlineDomainContractRun"] = prevInline
		} else {
			delete(job.State, "inlineDomainContractRun")
		}
		if hadSkipAuto {
			job.State["skipAutoStrategicOverlay"] = prevSkipAuto
		} else {
			delete(job.State, "skipAutoStrategicOverlay")
		}
	}()

	domainEngine := horizondomain.NewEngine(
		e.lensArtifactsRepo,
		e.fragmentsRepo,
		e.edgesRepo,
		e.scoutingJobsRepo,
		e.domainAnswersRepo,
		e.aiExecutor,
		domainContractEventPublisher{engine: e},
	)

	runInlineStrategicCollect := shouldRunInlineStrategicCollect(job)
	var executeStrategicCollect func(ctx context.Context, groundingEvidence *ai.GroundingEvidence) error
	if runInlineStrategicCollect {
		executeStrategicCollect = func(ctx context.Context, _ *ai.GroundingEvidence) error {
			return e.executeDomainStrategicOverlayJob(ctx, job, effectiveCanonicalPath, in.ExistingFragment)
		}
	}
	trace.Log(ctx, "domain_contract", "domain_contract.runner.strategic_collect.policy", map[string]any{
		"tenantId":               strings.TrimSpace(job.TenantID),
		"projectId":              strings.TrimSpace(job.ProjectID),
		"lensId":                 strings.TrimSpace(job.LensID),
		"jobId":                  strings.TrimSpace(job.ID),
		"contractType":           strings.TrimSpace(contract.ContractType),
		"inlineStrategicCollect": runInlineStrategicCollect,
	})
	if shouldBypassDomainAnswerAssembly(job, &contract) {
		trace.Log(ctx, "domain_contract", "domain_contract.runner.fast_path_only", map[string]any{
			"tenantId":     strings.TrimSpace(job.TenantID),
			"projectId":    strings.TrimSpace(job.ProjectID),
			"lensId":       strings.TrimSpace(job.LensID),
			"jobId":        strings.TrimSpace(job.ID),
			"contractType": strings.TrimSpace(contract.ContractType),
			"graphBuildMode": supplyChainGraphBuildMode(
				getSupplyChainExecutionPlanFromJobState(job, contract.CanonicalPath),
				job,
			),
			"effectivePath": strings.TrimSpace(effectiveCanonicalPath),
		})
		if err := e.executeDomainExpansionJob(ctx, job, effectiveCanonicalPath, in.ExistingFragment); err != nil {
			trace.Log(ctx, "domain_contract", "domain_contract.runner.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"jobId":               strings.TrimSpace(job.ID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"effectivePath":       strings.TrimSpace(effectiveCanonicalPath),
				"contractType":        strings.TrimSpace(contract.ContractType),
				"error":               err.Error(),
			})
			return nil, err
		}
		if err := e.finalizeHotPathOnlyDomainContract(ctx, job, effectiveCanonicalPath); err != nil {
			trace.Log(ctx, "domain_contract", "domain_contract.runner.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"jobId":               strings.TrimSpace(job.ID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"effectivePath":       strings.TrimSpace(effectiveCanonicalPath),
				"contractType":        strings.TrimSpace(contract.ContractType),
				"error":               err.Error(),
			})
			return nil, err
		}
		return nil, nil
	}

	runRes, err := domainEngine.Run(ctx, horizondomain.RunInput{
		Job:               job,
		Contract:          contract,
		GroundingEvidence: groundingEvidenceFromJobState(job),
		ExecuteBaseCollect: func(ctx context.Context, _ *ai.GroundingEvidence) error {
			return e.executeDomainExpansionJob(ctx, job, effectiveCanonicalPath, in.ExistingFragment)
		},
		ExecuteStrategicCollect: executeStrategicCollect,
	})
	if err != nil {
		trace.Log(ctx, "domain_contract", "domain_contract.runner.error", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"effectivePath":       strings.TrimSpace(effectiveCanonicalPath),
			"contractType":        strings.TrimSpace(contract.ContractType),
			"error":               err.Error(),
		})
		return nil, err
	}
	if runRes == nil {
		trace.Log(ctx, "domain_contract", "domain_contract.runner.nil_result", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"effectivePath":       strings.TrimSpace(effectiveCanonicalPath),
			"contractType":        strings.TrimSpace(contract.ContractType),
		})
		return nil, nil
	}
	snapshotID := ""
	readiness := ""
	if runRes.Snapshot != nil {
		snapshotID = strings.TrimSpace(runRes.Snapshot.ID)
		readiness = strings.TrimSpace(runRes.Snapshot.ReadinessStatus)
	}
	trace.Log(ctx, "domain_contract", "domain_contract.runner.completed", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"effectivePath":       strings.TrimSpace(effectiveCanonicalPath),
		"contractType":        strings.TrimSpace(contract.ContractType),
		"snapshotId":          snapshotID,
		"readinessStatus":     readiness,
	})
	return runRes.ViewDirective, nil
}

func shouldBypassDomainAnswerAssembly(job *models.ScoutingJob, contract *horizondomain.Contract) bool {
	if job == nil || contract == nil || !agents.IsOperationalChainContractType(contract.ContractType) {
		return false
	}
	plan := getSupplyChainExecutionPlanFromJobState(job, contract.CanonicalPath)
	return supplyChainGraphBuildMode(plan, job) == agents.SupplyChainGraphBuildModeHotPathOnly
}

func (e *ScoutEngine) finalizeHotPathOnlyDomainContract(ctx context.Context, job *models.ScoutingJob, effectiveCanonicalPath string) error {
	if e == nil || job == nil || isTerminalScoutJob(job) {
		return nil
	}
	pipelineName, requiredStages := requiredStagesForPipelinePassJob(job, pipeline.PassBaseEnrichment)
	return e.finalizePipelineRun(ctx, job, pipelineFinalizeInput{
		Pipeline:       pipelineName,
		RequiredStages: requiredStages,
		Hint:           "pipeline_pass_hot_path_only",
		LegacyHint:     "pipeline_pass_hot_path_only",
		CanonicalPath:  strings.TrimSpace(effectiveCanonicalPath),
		Confidence:     1.0,
	})
}

func shouldRunInlineStrategicCollect(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	for _, key := range []string{
		"enableInlineStrategicCollect",
		"runInlineStrategicCollect",
		"enableStrategicOverlay",
		"enableStrategicCollect",
	} {
		if v, ok := job.State[key]; ok && parseStateBool(v, false) {
			return true
		}
	}
	return false
}
