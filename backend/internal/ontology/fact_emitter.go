package ontology

import (
	"context"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// FactEmitter is a small shared helper that:
// - normalizes/sanitizes an OntologyFact
// - persists it to Firestore (via OntologyFactsRepository)
// - optionally publishes a lightweight realtime event for UI feeds (horizon.* namespace)
//
// This allows multiple producers (sources pipeline, Scout, human tools, LLM-backed services) to
// emit ontology facts consistently without duplicating boilerplate.
type FactEmitter struct {
	repo     *repo.OntologyFactsRepository
	realtime *realtime.Publisher
}

func NewFactEmitter(factsRepo *repo.OntologyFactsRepository, pub *realtime.Publisher) *FactEmitter {
	return &FactEmitter{
		repo:     factsRepo,
		realtime: pub,
	}
}

type EmitFactOptions struct {
	// Context for realtime event payloads.
	TenantID      string
	ProjectID     string
	LensID        string
	SessionID     string
	CorrelationID string

	// Whether to publish a realtime event.
	PublishFeedEvent bool

	// Who is emitting this fact.
	Actor string // default: "system"
}

func (e *FactEmitter) Emit(ctx context.Context, f *models.OntologyFact, opts EmitFactOptions) (string, error) {
	if e == nil || e.repo == nil || f == nil {
		return "", repo.ErrInvalidOntologyDocument
	}

	now := time.Now().UTC()

	// Normalize BaseOntologyFields (non-destructive defaults)
	if strings.TrimSpace(f.TenantID) == "" {
		f.TenantID = strings.TrimSpace(opts.TenantID)
	}
	if strings.TrimSpace(f.TenantID) == "" {
		f.TenantID = "global"
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	if f.Version == 0 {
		f.Version = 1
	}
	if strings.TrimSpace(f.CreatedBy) == "" {
		f.CreatedBy = strings.TrimSpace(opts.Actor)
	}
	if strings.TrimSpace(f.CreatedBy) == "" {
		f.CreatedBy = "system"
	}
	if strings.TrimSpace(f.UpdatedBy) == "" {
		f.UpdatedBy = f.CreatedBy
	}
	if strings.TrimSpace(f.Status) == "" {
		f.Status = "active"
	}

	// Minimal required fields sanity.
	f.Kind = strings.TrimSpace(f.Kind)
	f.Type = strings.TrimSpace(f.Type)
	f.Title = strings.TrimSpace(f.Title)
	f.Summary = strings.TrimSpace(f.Summary)
	if f.ObservedAt == nil {
		f.ObservedAt = &now
	}

	if f.VectorText == "" {
		f.VectorText = f.GenerateVectorText()
	}

	id, err := e.repo.Create(ctx, f)
	if err != nil {
		return "", err
	}

	if opts.PublishFeedEvent && e.realtime != nil {
		tenantID := strings.TrimSpace(opts.TenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(f.TenantID)
		}
		if tenantID == "" {
			tenantID = "global"
		}

		payload := map[string]interface{}{
			"tenantId":      tenantID,
			"projectId":     strings.TrimSpace(opts.ProjectID),
			"lensId":        strings.TrimSpace(opts.LensID),
			"sessionId":     strings.TrimSpace(opts.SessionID),
			"correlationId": strings.TrimSpace(opts.CorrelationID),
			"ts":            now.Format(time.RFC3339Nano),

			"factId":         id,
			"kind":           f.Kind,
			"type":           f.Type,
			"title":          f.Title,
			"confidence":     f.Confidence,
			"canonicalPaths": f.CanonicalPaths,
			"actorPaths":     f.ActorPaths,
			"spherePaths":    f.SpherePaths,
		}
		if f.LocationPath != nil && strings.TrimSpace(*f.LocationPath) != "" {
			payload["locationPath"] = strings.TrimSpace(*f.LocationPath)
		}
		if f.OccurredAt != nil {
			payload["occurredAt"] = f.OccurredAt.UTC().Format(time.RFC3339)
		}
		if f.ObservedAt != nil {
			payload["observedAt"] = f.ObservedAt.UTC().Format(time.RFC3339)
		}

		// Best effort: do not fail fact persistence on realtime publish.
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.ontology.fact.created"), tenantID, payload)
	}

	return id, nil
}
