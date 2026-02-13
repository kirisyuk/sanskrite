package contracts

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

type DomainAnswerHandler struct {
	contractType string
}

type domainContractRunner interface {
	RunDomainContract(ctx context.Context, in pipeline.ContractInput) (*models.ViewDirective, error)
}

func NewDomainAnswerHandler(contractType string) *DomainAnswerHandler {
	return &DomainAnswerHandler{
		contractType: strings.TrimSpace(strings.ToLower(contractType)),
	}
}

func (h *DomainAnswerHandler) ContractType() string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(h.contractType))
}

func (h *DomainAnswerHandler) Execute(ctx context.Context, eng pipeline.Engine, in pipeline.ContractInput) (*pipeline.ContractOutput, error) {
	if in.Job == nil {
		return nil, nil
	}
	contractType := ""
	if in.Contract != nil {
		contractType = strings.TrimSpace(strings.ToLower(in.Contract.ContractType))
	}
	if contractType == "" {
		contractType = h.ContractType()
	}

	if runner, ok := eng.(domainContractRunner); ok {
		directive, err := runner.RunDomainContract(ctx, in)
		if err != nil {
			trace.Log(ctx, "contract", "contract.domain.run.error", map[string]any{
				"tenantId":     strings.TrimSpace(in.Job.TenantID),
				"projectId":    strings.TrimSpace(in.Job.ProjectID),
				"lensId":       strings.TrimSpace(in.Job.LensID),
				"jobId":        strings.TrimSpace(in.Job.ID),
				"contractType": contractType,
				"error":        err.Error(),
			})
			return nil, err
		}
		trace.Log(ctx, "contract", "contract.domain.run.ok", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": contractType,
		})
		return &pipeline.ContractOutput{ViewDirective: directive}, nil
	}

	trace.Log(ctx, "contract", "contract.domain.runner.missing", map[string]any{
		"tenantId":     strings.TrimSpace(in.Job.TenantID),
		"projectId":    strings.TrimSpace(in.Job.ProjectID),
		"lensId":       strings.TrimSpace(in.Job.LensID),
		"jobId":        strings.TrimSpace(in.Job.ID),
		"contractType": contractType,
	})
	if err := eng.ExecuteDomainExpansionPass(ctx, in.Job, in.EffectiveCanonicalPath, in.ExistingFragment); err != nil {
		return nil, err
	}
	return nil, nil
}
