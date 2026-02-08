package scout

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

// ExecuteDomainExpansionPass is a pipeline-facing wrapper around the internal domain expansion executor.
// It exists to allow the pipeline runner (separate package) to invoke the pass without relying on
// unexported method names (which cannot cross package boundaries in Go interface matching).
func (e *ScoutEngine) ExecuteDomainExpansionPass(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	return e.executeDomainExpansionJob(ctx, job, effectiveCanonicalPath, existingFragment)
}

// ExecuteStrategicProjectionPass is a pipeline-facing wrapper around the internal strategic overlay executor.
func (e *ScoutEngine) ExecuteStrategicProjectionPass(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	return e.executeDomainStrategicOverlayJob(ctx, job, effectiveCanonicalPath, existingFragment)
}

// ContractRegistry returns the default contract registry.
func (e *ScoutEngine) ContractRegistry() pipeline.ContractRegistry {
	return contracts.DefaultRegistry()
}

// BuildContractInput prepares a contract input payload for contract handlers.
func (e *ScoutEngine) BuildContractInput(ctx context.Context, in pipeline.PassInput) (*pipeline.ContractInput, error) {
	if e == nil || in.Job == nil {
		return nil, fmt.Errorf("contract input: engine or job is nil")
	}
	contract := e.bestEffortFindDomainContract(in.Job, in.EffectiveCanonicalPath)
	return &pipeline.ContractInput{
		Job:                    in.Job,
		EffectiveCanonicalPath: in.EffectiveCanonicalPath,
		ExistingFragment:       in.ExistingFragment,
		Contract:               contract,
		FragmentsRepo:          e.fragmentsRepo,
		EdgesRepo:              e.edgesRepo,
	}, nil
}

// PersistViewDirective stores viewDirective into the lens viewConfig (best-effort).
func (e *ScoutEngine) PersistViewDirective(ctx context.Context, job *models.ScoutingJob, directive *models.ViewDirective) error {
	if e == nil || e.lensesRepo == nil || job == nil || directive == nil {
		logViewDirectiveSkip(ctx, job, "missing_engine_or_job_or_directive", nil)
		return nil
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		logViewDirectiveSkip(ctx, job, "missing_lens_id", nil)
		return nil
	}
	lens, err := e.lensesRepo.Get(ctx, lensID)
	if err != nil || lens == nil {
		logViewDirectiveSkip(ctx, job, "lens_get_failed", map[string]any{
			"lensId": lensID,
			"error":  errString(err),
		})
		return err
	}
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]interface{}{}
	}
	lens.ViewConfig["viewDirective"] = directive
	if jobID := strings.TrimSpace(job.ID); jobID != "" {
		lens.ViewConfig["viewDirectiveJobId"] = jobID
		viewByJob := extractStringMap(lens.ViewConfig["viewDirectivesByJob"])
		viewByJob[jobID] = directive
		lens.ViewConfig["viewDirectivesByJob"] = viewByJob
	}
	if query := viewDirectiveQuery(job); query != "" {
		qh := viewDirectiveQueryHash(query)
		lens.ViewConfig["viewDirectiveQuery"] = query
		lens.ViewConfig["viewDirectiveQueryHash"] = qh
		viewByQuery := extractStringMap(lens.ViewConfig["viewDirectivesByQuery"])
		viewByQuery[qh] = directive
		lens.ViewConfig["viewDirectivesByQuery"] = viewByQuery
	}
	if err := e.lensesRepo.Update(ctx, lens); err != nil {
		trace.Log(ctx, "view_directive", "view_directive.persist.error", map[string]any{
			"tenantId":          strings.TrimSpace(job.TenantID),
			"projectId":         strings.TrimSpace(job.ProjectID),
			"lensId":            lensID,
			"jobId":             strings.TrimSpace(job.ID),
			"contractType":      strings.TrimSpace(directive.ContractType),
			"viewDirectiveHash": strings.TrimSpace(viewDirectiveQueryHash(viewDirectiveQuery(job))),
			"error":             err.Error(),
		})
		return err
	}
	trace.Log(ctx, "view_directive", "view_directive.persist.ok", map[string]any{
		"tenantId":          strings.TrimSpace(job.TenantID),
		"projectId":         strings.TrimSpace(job.ProjectID),
		"lensId":            lensID,
		"jobId":             strings.TrimSpace(job.ID),
		"contractType":      strings.TrimSpace(directive.ContractType),
		"viewDirectiveHash": strings.TrimSpace(viewDirectiveQueryHash(viewDirectiveQuery(job))),
	})
	return nil
}

func viewDirectiveQuery(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	if job.State != nil {
		if v, ok := job.State["originalQuery"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if strings.TrimSpace(job.UserQuery) != "" {
		return strings.TrimSpace(job.UserQuery)
	}
	return ""
}

func viewDirectiveQueryHash(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	sum := sha1.Sum([]byte(strings.ToLower(q)))
	return hex.EncodeToString(sum[:])
}

func extractStringMap(raw interface{}) map[string]interface{} {
	if raw == nil {
		return map[string]interface{}{}
	}
	if m, ok := raw.(map[string]interface{}); ok && m != nil {
		return m
	}
	return map[string]interface{}{}
}

func logViewDirectiveSkip(ctx context.Context, job *models.ScoutingJob, reason string, extra map[string]any) {
	if job == nil {
		return
	}
	payload := map[string]any{
		"tenantId":  strings.TrimSpace(job.TenantID),
		"projectId": strings.TrimSpace(job.ProjectID),
		"lensId":    strings.TrimSpace(job.LensID),
		"jobId":     strings.TrimSpace(job.ID),
		"reason":    reason,
	}
	for k, v := range extra {
		payload[k] = v
	}
	trace.Log(ctx, "view_directive", "view_directive.persist.skip", payload)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
