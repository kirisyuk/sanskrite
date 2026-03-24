package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	simulationRunsCollection      = "horizon_simulation_runs"
	simulationWorldsCollection    = "horizon_simulation_worlds"
	simulationAgentsCollection    = "horizon_simulation_agents"
	simulationEventsCollection    = "horizon_simulation_events"
	simulationArtifactsCollection = "horizon_simulation_artifacts"
)

type SimulationRunsRepository struct {
	col *firestore.CollectionRef
}

func NewSimulationRunsRepository(db *firestore.Client) *SimulationRunsRepository {
	return &SimulationRunsRepository{col: db.Collection(simulationRunsCollection)}
}

func (r *SimulationRunsRepository) Create(ctx context.Context, run *horizonmodels.SimulationRun) (string, error) {
	if run == nil {
		return "", ErrInvalidDocument
	}
	if err := run.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	doc := r.col.NewDoc()
	run.ID = doc.ID
	if _, err := doc.Set(ctx, run); err != nil {
		return "", err
	}
	return run.ID, nil
}

func (r *SimulationRunsRepository) Get(ctx context.Context, runID string) (*horizonmodels.SimulationRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(runID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var run horizonmodels.SimulationRun
	if err := snap.DataTo(&run); err != nil {
		return nil, err
	}
	if run.ID == "" {
		run.ID = snap.Ref.ID
	}
	return &run, nil
}

func (r *SimulationRunsRepository) Update(ctx context.Context, run *horizonmodels.SimulationRun) error {
	if run == nil || strings.TrimSpace(run.ID) == "" {
		return ErrInvalidDocument
	}
	if err := run.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	run.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(run.ID)).Set(ctx, run)
	return err
}

func (r *SimulationRunsRepository) MergePatch(ctx context.Context, runID string, patch map[string]any) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ErrInvalidDocument
	}
	if len(patch) == 0 {
		return nil
	}
	patch["updatedAt"] = time.Now().UTC()
	_, err := r.col.Doc(runID).Set(ctx, patch, firestore.MergeAll)
	return err
}

func (r *SimulationRunsRepository) ListRecentByTenant(ctx context.Context, tenantID string, limit int) ([]*horizonmodels.SimulationRun, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if limit <= 0 {
		limit = 25
	}
	docs, err := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SimulationRun, 0, len(docs))
	for _, doc := range docs {
		var run horizonmodels.SimulationRun
		if err := doc.DataTo(&run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = doc.Ref.ID
		}
		out = append(out, &run)
	}
	return out, nil
}

type SimulationWorldsRepository struct {
	col *firestore.CollectionRef
}

func NewSimulationWorldsRepository(db *firestore.Client) *SimulationWorldsRepository {
	return &SimulationWorldsRepository{col: db.Collection(simulationWorldsCollection)}
}

func (r *SimulationWorldsRepository) Create(ctx context.Context, world *horizonmodels.SimulationWorld) (string, error) {
	if world == nil {
		return "", ErrInvalidDocument
	}
	if err := world.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}
	now := time.Now().UTC()
	if world.CreatedAt.IsZero() {
		world.CreatedAt = now
	}
	world.UpdatedAt = now
	doc := r.col.NewDoc()
	world.ID = doc.ID
	if _, err := doc.Set(ctx, world); err != nil {
		return "", err
	}
	return world.ID, nil
}

func (r *SimulationWorldsRepository) Get(ctx context.Context, worldID string) (*horizonmodels.SimulationWorld, error) {
	worldID = strings.TrimSpace(worldID)
	if worldID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(worldID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var world horizonmodels.SimulationWorld
	if err := snap.DataTo(&world); err != nil {
		return nil, err
	}
	if world.ID == "" {
		world.ID = snap.Ref.ID
	}
	return &world, nil
}

func (r *SimulationWorldsRepository) Update(ctx context.Context, world *horizonmodels.SimulationWorld) error {
	if world == nil || strings.TrimSpace(world.ID) == "" {
		return ErrInvalidDocument
	}
	if err := world.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	world.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(world.ID)).Set(ctx, world)
	return err
}

func (r *SimulationWorldsRepository) ListByRun(ctx context.Context, tenantID, runID string, limit int) ([]*horizonmodels.SimulationWorld, error) {
	tenantID = strings.TrimSpace(tenantID)
	runID = strings.TrimSpace(runID)
	if tenantID == "" || runID == "" {
		return nil, fmt.Errorf("tenantID and runID are required")
	}
	if limit <= 0 {
		limit = 256
	}
	docs, err := r.col.
		Where("tenantId", "==", tenantID).
		Where("runId", "==", runID).
		OrderBy("worldIndex", firestore.Asc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SimulationWorld, 0, len(docs))
	for _, doc := range docs {
		var world horizonmodels.SimulationWorld
		if err := doc.DataTo(&world); err != nil {
			continue
		}
		if world.ID == "" {
			world.ID = doc.Ref.ID
		}
		out = append(out, &world)
	}
	return out, nil
}

func (r *SimulationWorldsRepository) CountByRunAndStatus(ctx context.Context, tenantID, runID, status string) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	runID = strings.TrimSpace(runID)
	status = strings.TrimSpace(status)
	if tenantID == "" || runID == "" || status == "" {
		return 0, fmt.Errorf("tenantID, runID, and status are required")
	}
	iter := r.col.
		Where("tenantId", "==", tenantID).
		Where("runId", "==", runID).
		Where("status", "==", status).
		Documents(ctx)
	defer iter.Stop()
	count := 0
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		count++
	}
}
