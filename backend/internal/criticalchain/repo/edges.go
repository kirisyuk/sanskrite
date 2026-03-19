package repo

import (
	"context"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
)

const edgesCollection = "criticalchain_edges"

// EdgesRepository manages criticalchain_edges (ProcessingStep, Route).
type EdgesRepository struct {
	col *firestore.CollectionRef
}

// NewEdgesRepository creates a new EdgesRepository.
func NewEdgesRepository(db *firestore.Client) *EdgesRepository {
	return &EdgesRepository{
		col: db.Collection(edgesCollection),
	}
}

// ListRoutes returns all routes, optionally filtered by material.
func (r *EdgesRepository) ListRoutes(ctx context.Context) ([]*models.Route, error) {
	iter := r.col.Where("kind", "==", string(models.EdgeKindRoute)).Documents(ctx)
	defer iter.Stop()
	var out []*models.Route
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var edge models.EdgeDocument
		if err := doc.DataTo(&edge); err != nil {
			log.Printf("[CriticalChain] skip edge %s: %v", doc.Ref.ID, err)
			continue
		}
		if edge.Route != nil {
			out = append(out, edge.Route)
		}
	}
	return out, nil
}

// ListProcessingSteps returns processing steps for a material (optional).
func (r *EdgesRepository) ListProcessingSteps(ctx context.Context) ([]*models.ProcessingStep, error) {
	iter := r.col.Where("kind", "==", string(models.EdgeKindProcessingStep)).Documents(ctx)
	defer iter.Stop()
	var out []*models.ProcessingStep
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var edge models.EdgeDocument
		if err := doc.DataTo(&edge); err != nil {
			log.Printf("[CriticalChain] skip edge %s: %v", doc.Ref.ID, err)
			continue
		}
		if edge.ProcessingStep != nil {
			out = append(out, edge.ProcessingStep)
		}
	}
	return out, nil
}

// SetRoute upserts a route.
func (r *EdgesRepository) SetRoute(ctx context.Context, route *models.Route) error {
	if route.ID == "" {
		return errInvalidID
	}
	edge := models.EdgeDocument{
		ID:        route.ID,
		Kind:      models.EdgeKindRoute,
		Route:     route,
		UpdatedAt: route.UpdatedAt,
	}
	_, err := r.col.Doc(route.ID).Set(ctx, edge)
	return err
}

// SetProcessingStep upserts a processing step.
func (r *EdgesRepository) SetProcessingStep(ctx context.Context, step *models.ProcessingStep) error {
	id := step.From + "->" + step.To
	edge := models.EdgeDocument{
		ID:             id,
		Kind:           models.EdgeKindProcessingStep,
		ProcessingStep: step,
		UpdatedAt:      time.Now(),
	}
	_, err := r.col.Doc(id).Set(ctx, edge)
	return err
}
