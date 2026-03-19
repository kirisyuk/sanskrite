package trace

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
)

type Service struct {
	store  *repo.Store
	logger *log.Logger
}

func NewService(store *repo.Store, logger *log.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) StartSpan(ctx context.Context, tenantID, taskID, agentID, capability, kind string, attrs map[string]any) (*contracts.TraceSpan, error) {
	span := &contracts.TraceSpan{
		ID:         uuid.NewString(),
		TenantID:   tenantID,
		TaskID:     taskID,
		AgentID:    agentID,
		Capability: capability,
		Kind:       kind,
		Status:     "running",
		Attributes: attrs,
		StartedAt:  time.Now().UTC(),
	}
	if s.store != nil {
		if err := s.store.SaveTrace(ctx, span); err != nil {
			return nil, err
		}
	}
	if s.logger != nil {
		s.logger.Printf("[agentmesh.trace] start span=%s task=%s agent=%s capability=%s kind=%s", span.ID, taskID, agentID, capability, kind)
	}
	return span, nil
}

func (s *Service) FinishSpan(ctx context.Context, span *contracts.TraceSpan, status string, cost *contracts.CostAttribution, extra map[string]any) error {
	if span == nil {
		return nil
	}
	now := time.Now().UTC()
	span.EndedAt = &now
	span.Status = status
	span.Cost = cost
	if len(extra) > 0 {
		if span.Attributes == nil {
			span.Attributes = map[string]any{}
		}
		for k, v := range extra {
			span.Attributes[k] = v
		}
	}
	if s.store != nil {
		if err := s.store.SaveTrace(ctx, span); err != nil {
			return err
		}
	}
	if s.logger != nil {
		s.logger.Printf("[agentmesh.trace] finish span=%s task=%s status=%s", span.ID, span.TaskID, status)
	}
	return nil
}
