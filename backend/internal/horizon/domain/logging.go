package domain

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

func logDomainContract(ctx context.Context, event string, job *models.ScoutingJob, contract *Contract, extra map[string]any) {
	event = strings.TrimSpace(event)
	if event == "" {
		event = "unknown"
	}
	data := map[string]any{}
	if job != nil {
		if v := strings.TrimSpace(job.TenantID); v != "" {
			data["tenantId"] = v
		}
		if v := strings.TrimSpace(job.ProjectID); v != "" {
			data["projectId"] = v
		}
		if v := strings.TrimSpace(job.LensID); v != "" {
			data["lensId"] = v
		}
		if v := strings.TrimSpace(job.ID); v != "" {
			data["jobId"] = v
		}
		if v := strings.TrimSpace(job.TargetCanonicalPath); v != "" {
			data["targetCanonicalPath"] = v
		}
	}
	if contract != nil {
		if v := strings.TrimSpace(contract.CanonicalPath); v != "" {
			data["domainCanonicalPath"] = v
		}
		if v := strings.TrimSpace(contract.ContractType); v != "" {
			data["contractType"] = v
		}
	}
	for k, v := range extra {
		data[k] = v
	}
	trace.Log(ctx, "domain_contract", "domain_contract."+event, data)
}
