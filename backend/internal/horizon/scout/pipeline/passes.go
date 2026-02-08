package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

// BaseEnrichmentPass runs base domain enrichment (formerly "domain expansion").
// Implementation currently delegates to the existing ScoutEngine domain expansion executor.
// This keeps the system stable while we converge on a single pipeline runner.
type BaseEnrichmentPass struct{}

func (BaseEnrichmentPass) Name() PassName { return PassBaseEnrichment }

func (BaseEnrichmentPass) Execute(ctx context.Context, eng Engine, in PassInput) error {
	if eng == nil {
		if in.Job != nil {
			trace.Log(ctx, "pipeline", "pipeline.base_enrichment.engine.nil", map[string]any{
				"tenantId":  strings.TrimSpace(in.Job.TenantID),
				"projectId": strings.TrimSpace(in.Job.ProjectID),
				"lensId":    strings.TrimSpace(in.Job.LensID),
				"jobId":     strings.TrimSpace(in.Job.ID),
			})
		}
		return fmt.Errorf("base enrichment pass: engine is nil")
	}
	if reg := eng.ContractRegistry(); reg != nil {
		if input, err := eng.BuildContractInput(ctx, in); err == nil && input != nil {
			contractType := ""
			if input.Contract != nil {
				contractType = strings.TrimSpace(input.Contract.ContractType)
			}
			if contractType != "" {
				if handler := reg.Get(contractType); handler != nil {
					trace.Log(ctx, "pipeline", "pipeline.contract.execute.start", map[string]any{
						"tenantId":     strings.TrimSpace(in.Job.TenantID),
						"projectId":    strings.TrimSpace(in.Job.ProjectID),
						"lensId":       strings.TrimSpace(in.Job.LensID),
						"jobId":        strings.TrimSpace(in.Job.ID),
						"contractType": contractType,
					})
					out, execErr := handler.Execute(ctx, eng, *input)
					if execErr != nil {
						trace.Log(ctx, "pipeline", "pipeline.contract.execute.error", map[string]any{
							"tenantId":     strings.TrimSpace(in.Job.TenantID),
							"projectId":    strings.TrimSpace(in.Job.ProjectID),
							"lensId":       strings.TrimSpace(in.Job.LensID),
							"jobId":        strings.TrimSpace(in.Job.ID),
							"contractType": contractType,
							"error":        execErr.Error(),
						})
						return execErr
					}
					if out != nil && out.ViewDirective != nil {
						if err := eng.PersistViewDirective(ctx, in.Job, out.ViewDirective); err != nil {
							trace.Log(ctx, "pipeline", "pipeline.contract.persist_view_directive.error", map[string]any{
								"tenantId":     strings.TrimSpace(in.Job.TenantID),
								"projectId":    strings.TrimSpace(in.Job.ProjectID),
								"lensId":       strings.TrimSpace(in.Job.LensID),
								"jobId":        strings.TrimSpace(in.Job.ID),
								"contractType": contractType,
								"error":        err.Error(),
							})
						}
					}
					trace.Log(ctx, "pipeline", "pipeline.contract.execute.ok", map[string]any{
						"tenantId":     strings.TrimSpace(in.Job.TenantID),
						"projectId":    strings.TrimSpace(in.Job.ProjectID),
						"lensId":       strings.TrimSpace(in.Job.LensID),
						"jobId":        strings.TrimSpace(in.Job.ID),
						"contractType": contractType,
					})
					return nil
				}
				trace.Log(ctx, "pipeline", "pipeline.contract.handler.missing", map[string]any{
					"tenantId":     strings.TrimSpace(in.Job.TenantID),
					"projectId":    strings.TrimSpace(in.Job.ProjectID),
					"lensId":       strings.TrimSpace(in.Job.LensID),
					"jobId":        strings.TrimSpace(in.Job.ID),
					"contractType": contractType,
				})
			}
			if contractType == "" {
				trace.Log(ctx, "pipeline", "pipeline.contract.type.empty", map[string]any{
					"tenantId":  strings.TrimSpace(in.Job.TenantID),
					"projectId": strings.TrimSpace(in.Job.ProjectID),
					"lensId":    strings.TrimSpace(in.Job.LensID),
					"jobId":     strings.TrimSpace(in.Job.ID),
				})
			}
		} else if err != nil {
			if in.Job != nil {
				trace.Log(ctx, "pipeline", "pipeline.contract.input.error", map[string]any{
					"tenantId":  strings.TrimSpace(in.Job.TenantID),
					"projectId": strings.TrimSpace(in.Job.ProjectID),
					"lensId":    strings.TrimSpace(in.Job.LensID),
					"jobId":     strings.TrimSpace(in.Job.ID),
					"error":     err.Error(),
				})
			}
		}
	}
	if in.Job != nil {
		trace.Log(ctx, "pipeline", "pipeline.base_enrichment.fallback.domain_expansion", map[string]any{
			"tenantId":  strings.TrimSpace(in.Job.TenantID),
			"projectId": strings.TrimSpace(in.Job.ProjectID),
			"lensId":    strings.TrimSpace(in.Job.LensID),
			"jobId":     strings.TrimSpace(in.Job.ID),
		})
	}
	return eng.ExecuteDomainExpansionPass(ctx, in.Job, in.EffectiveCanonicalPath, in.ExistingFragment)
}

// StrategicProjectionPass runs strategic projection (decision/mechanism layer overlay).
// Implementation currently delegates to the existing ScoutEngine overlay executor.
type StrategicProjectionPass struct{}

func (StrategicProjectionPass) Name() PassName { return PassStrategicProjection }

func (StrategicProjectionPass) Execute(ctx context.Context, eng Engine, in PassInput) error {
	if eng == nil {
		if in.Job != nil {
			trace.Log(ctx, "pipeline", "pipeline.strategic_projection.engine.nil", map[string]any{
				"tenantId":  strings.TrimSpace(in.Job.TenantID),
				"projectId": strings.TrimSpace(in.Job.ProjectID),
				"lensId":    strings.TrimSpace(in.Job.LensID),
				"jobId":     strings.TrimSpace(in.Job.ID),
			})
		}
		return fmt.Errorf("strategic projection pass: engine is nil")
	}
	return eng.ExecuteStrategicProjectionPass(ctx, in.Job, in.EffectiveCanonicalPath, in.ExistingFragment)
}
