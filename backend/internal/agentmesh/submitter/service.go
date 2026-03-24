package submitter

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
)

type Service struct {
	runtime *runtime.Service
}

func NewService(runtime *runtime.Service) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) Submit(ctx context.Context, tenantID, capability string, input map[string]any, dedupKey string) (string, error) {
	if s == nil || s.runtime == nil {
		return "", fmt.Errorf("agentmesh submitter is not configured")
	}
	task, err := s.runtime.CreateTask(ctx, &contracts.MeshTask{
		TenantID:   strings.TrimSpace(tenantID),
		Capability: strings.TrimSpace(capability),
		Input:      input,
		Lease: contracts.Lease{
			DedupKey: strings.TrimSpace(dedupKey),
		},
	}, policy.RequestContext{
		CallerAgentID: "internal",
		DataZone:      "internal",
	}, false)
	if err != nil {
		return "", err
	}
	return task.ID, nil
}
