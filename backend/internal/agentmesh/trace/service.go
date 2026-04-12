package trace

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type Service struct {
	store     *repo.Store
	logger    *log.Logger
	tracer    oteltrace.Tracer
	spanMu    sync.Mutex
	otelSpans map[string]oteltrace.Span
}

func NewService(store *repo.Store, logger *log.Logger) *Service {
	return &Service{
		store:     store,
		logger:    logger,
		tracer:    otel.Tracer("agentmesh/runtime"),
		otelSpans: map[string]oteltrace.Span{},
	}
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
	if s.tracer != nil {
		_, otelSpan := s.tracer.Start(ctx, kind,
			oteltrace.WithAttributes(traceAttributes(span, attrs)...),
		)
		s.spanMu.Lock()
		s.otelSpans[span.ID] = otelSpan
		s.spanMu.Unlock()
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
	s.finishOTelSpan(span, status, cost, extra)
	if s.logger != nil {
		s.logger.Printf("[agentmesh.trace] finish span=%s task=%s status=%s", span.ID, span.TaskID, status)
	}
	return nil
}

func (s *Service) finishOTelSpan(span *contracts.TraceSpan, statusText string, cost *contracts.CostAttribution, extra map[string]any) {
	if s == nil || span == nil {
		return
	}
	s.spanMu.Lock()
	otelSpan := s.otelSpans[span.ID]
	delete(s.otelSpans, span.ID)
	s.spanMu.Unlock()
	if otelSpan == nil {
		return
	}
	attrs := traceAttributes(span, extra)
	if cost != nil {
		attrs = append(attrs,
			attribute.String("agentmesh.model", cost.Model),
			attribute.Int("agentmesh.prompt_tokens", cost.PromptTokens),
			attribute.Int("agentmesh.completion_tokens", cost.CompletionTokens),
			attribute.Float64("agentmesh.estimated_usd", cost.EstimatedUSD),
		)
	}
	if len(attrs) > 0 {
		otelSpan.SetAttributes(attrs...)
	}
	if statusText == "failed" || statusText == "rejected" {
		otelSpan.SetStatus(codes.Error, statusText)
	} else {
		otelSpan.SetStatus(codes.Ok, statusText)
	}
	otelSpan.End()
}

func traceAttributes(span *contracts.TraceSpan, attrs map[string]any) []attribute.KeyValue {
	out := []attribute.KeyValue{
		attribute.String("agentmesh.span_id", span.ID),
		attribute.String("agentmesh.task_id", span.TaskID),
		attribute.String("agentmesh.tenant_id", span.TenantID),
		attribute.String("agentmesh.agent_id", span.AgentID),
		attribute.String("agentmesh.capability", span.Capability),
		attribute.String("agentmesh.kind", span.Kind),
		attribute.String("agentmesh.status", span.Status),
	}
	for key, value := range attrs {
		attrKey := "agentmesh." + key
		switch v := value.(type) {
		case string:
			out = append(out, attribute.String(attrKey, v))
		case bool:
			out = append(out, attribute.Bool(attrKey, v))
		case int:
			out = append(out, attribute.Int(attrKey, v))
		case int64:
			out = append(out, attribute.Int64(attrKey, v))
		case float64:
			out = append(out, attribute.Float64(attrKey, v))
		default:
			out = append(out, attribute.String(attrKey, stringifyValue(v)))
		}
	}
	return out
}

func stringifyValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
