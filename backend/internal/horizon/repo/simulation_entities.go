package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type SimulationAgentsRepository struct {
	col *firestore.CollectionRef
}

func NewSimulationAgentsRepository(db *firestore.Client) *SimulationAgentsRepository {
	return &SimulationAgentsRepository{col: db.Collection(simulationAgentsCollection)}
}

func (r *SimulationAgentsRepository) Create(ctx context.Context, agent *horizonmodels.SimulationAgentSpec) (string, error) {
	if agent == nil {
		return "", ErrInvalidDocument
	}
	if err := agent.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}
	now := time.Now().UTC()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = now
	}
	agent.UpdatedAt = now
	doc := r.col.NewDoc()
	agent.ID = doc.ID
	if _, err := doc.Set(ctx, agent); err != nil {
		return "", err
	}
	return agent.ID, nil
}

func (r *SimulationAgentsRepository) ListByWorld(ctx context.Context, tenantID, runID, worldID string, limit int) ([]*horizonmodels.SimulationAgentSpec, error) {
	tenantID = strings.TrimSpace(tenantID)
	runID = strings.TrimSpace(runID)
	worldID = strings.TrimSpace(worldID)
	if tenantID == "" || runID == "" || worldID == "" {
		return nil, fmt.Errorf("tenantID, runID, and worldID are required")
	}
	if limit <= 0 {
		limit = 256
	}
	docs, err := r.col.
		Where("tenantId", "==", tenantID).
		Where("runId", "==", runID).
		Where("worldId", "==", worldID).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SimulationAgentSpec, 0, len(docs))
	for _, doc := range docs {
		var agent horizonmodels.SimulationAgentSpec
		if err := doc.DataTo(&agent); err != nil {
			continue
		}
		if agent.ID == "" {
			agent.ID = doc.Ref.ID
		}
		out = append(out, &agent)
	}
	return out, nil
}

type SimulationEventsRepository struct {
	col *firestore.CollectionRef
}

func NewSimulationEventsRepository(db *firestore.Client) *SimulationEventsRepository {
	return &SimulationEventsRepository{col: db.Collection(simulationEventsCollection)}
}

func (r *SimulationEventsRepository) Create(ctx context.Context, event *horizonmodels.SimulationEvent) (string, error) {
	if event == nil {
		return "", ErrInvalidDocument
	}
	if err := event.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}
	now := time.Now().UTC()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	event.UpdatedAt = now
	doc := r.col.NewDoc()
	event.ID = doc.ID
	if _, err := doc.Set(ctx, event); err != nil {
		return "", err
	}
	return event.ID, nil
}

func (r *SimulationEventsRepository) ListByWorld(ctx context.Context, tenantID, runID, worldID string, limit int) ([]*horizonmodels.SimulationEvent, error) {
	tenantID = strings.TrimSpace(tenantID)
	runID = strings.TrimSpace(runID)
	worldID = strings.TrimSpace(worldID)
	if tenantID == "" || runID == "" {
		return nil, fmt.Errorf("tenantID and runID are required")
	}
	if limit <= 0 {
		limit = 512
	}
	query := r.col.
		Where("tenantId", "==", tenantID).
		Where("runId", "==", runID)
	if worldID != "" {
		query = query.Where("worldId", "==", worldID)
	}
	query = query.OrderBy("createdAt", firestore.Asc).Limit(limit)
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SimulationEvent, 0, len(docs))
	for _, doc := range docs {
		var event horizonmodels.SimulationEvent
		if err := doc.DataTo(&event); err != nil {
			continue
		}
		if event.ID == "" {
			event.ID = doc.Ref.ID
		}
		out = append(out, &event)
	}
	return out, nil
}

type SimulationArtifactsRepository struct {
	col *firestore.CollectionRef
}

func NewSimulationArtifactsRepository(db *firestore.Client) *SimulationArtifactsRepository {
	return &SimulationArtifactsRepository{col: db.Collection(simulationArtifactsCollection)}
}

func (r *SimulationArtifactsRepository) Create(ctx context.Context, artifact *horizonmodels.SimulationArtifact) (string, error) {
	if artifact == nil {
		return "", ErrInvalidDocument
	}
	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}
	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	artifact.UpdatedAt = now
	doc := r.col.NewDoc()
	artifact.ID = doc.ID
	if _, err := doc.Set(ctx, artifact); err != nil {
		return "", err
	}
	return artifact.ID, nil
}

func (r *SimulationArtifactsRepository) Get(ctx context.Context, artifactID string) (*horizonmodels.SimulationArtifact, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(artifactID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var artifact horizonmodels.SimulationArtifact
	if err := snap.DataTo(&artifact); err != nil {
		return nil, err
	}
	if artifact.ID == "" {
		artifact.ID = snap.Ref.ID
	}
	return &artifact, nil
}

func (r *SimulationArtifactsRepository) Update(ctx context.Context, artifact *horizonmodels.SimulationArtifact) error {
	if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
		return ErrInvalidDocument
	}
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	artifact.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(artifact.ID)).Set(ctx, artifact)
	return err
}

func (r *SimulationArtifactsRepository) ListByRun(ctx context.Context, tenantID, runID string, limit int) ([]*horizonmodels.SimulationArtifact, error) {
	tenantID = strings.TrimSpace(tenantID)
	runID = strings.TrimSpace(runID)
	if tenantID == "" || runID == "" {
		return nil, fmt.Errorf("tenantID and runID are required")
	}
	if limit <= 0 {
		limit = 64
	}
	docs, err := r.col.
		Where("tenantId", "==", tenantID).
		Where("runId", "==", runID).
		OrderBy("createdAt", firestore.Asc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SimulationArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.SimulationArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		if artifact.ID == "" {
			artifact.ID = doc.Ref.ID
		}
		out = append(out, &artifact)
	}
	return out, nil
}
