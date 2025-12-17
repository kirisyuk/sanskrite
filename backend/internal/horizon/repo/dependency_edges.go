package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrEdgeNotFound = errors.New("dependency edge not found")

// DependencyEdgesRepository репозиторий для работы с DependencyEdge
type DependencyEdgesRepository struct {
	col *firestore.CollectionRef
}

// NewDependencyEdgesRepository создает новый репозиторий
func NewDependencyEdgesRepository(db *firestore.Client) *DependencyEdgesRepository {
	return &DependencyEdgesRepository{
		col: db.Collection("horizon_dependency_edges"),
	}
}

// Create создает новый DependencyEdge
func (r *DependencyEdgesRepository) Create(ctx context.Context, edge *models.DependencyEdge) (string, error) {
	if edge == nil {
		return "", ErrInvalidDocument
	}
	if err := edge.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	edge.Metadata.CreatedAt = now
	edge.Metadata.LastUpdatedAt = now

	doc := r.col.NewDoc()
	edge.ID = doc.ID

	if _, err := doc.Set(ctx, edge); err != nil {
		return "", err
	}
	return edge.ID, nil
}

// Get получает DependencyEdge по ID
func (r *DependencyEdgesRepository) Get(ctx context.Context, id string) (*models.DependencyEdge, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrEdgeNotFound
	}
	var edge models.DependencyEdge
	if err := snap.DataTo(&edge); err != nil {
		return nil, err
	}
	return &edge, nil
}

// GetBySourceAndTarget получает DependencyEdge по source и target
func (r *DependencyEdgesRepository) GetBySourceAndTarget(ctx context.Context, tenantID, sourceFragmentID, targetFragmentID string) (*models.DependencyEdge, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", sourceFragmentID).Where("targetFragmentId", "==", targetFragmentID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrEdgeNotFound
	}
	var edge models.DependencyEdge
	if err := docs[0].DataTo(&edge); err != nil {
		return nil, err
	}
	return &edge, nil
}

// ListBySource получает список исходящих DependencyEdge
func (r *DependencyEdgesRepository) ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", sourceFragmentID)
	if edgeType != nil {
		q = q.Where("type", "==", *edgeType)
	}
	q = q.OrderBy("properties.criticality", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		edges = append(edges, &edge)
	}
	return edges, nil
}

// ListByTarget получает список входящих DependencyEdge
func (r *DependencyEdgesRepository) ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", targetFragmentID)
	if edgeType != nil {
		q = q.Where("type", "==", *edgeType)
	}
	q = q.OrderBy("properties.criticality", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		edges = append(edges, &edge)
	}
	return edges, nil
}

// Update обновляет DependencyEdge
func (r *DependencyEdgesRepository) Update(ctx context.Context, edge *models.DependencyEdge) error {
	if edge == nil || edge.ID == "" {
		return ErrInvalidDocument
	}
	if err := edge.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	edge.Metadata.LastUpdatedAt = now

	_, err := r.col.Doc(edge.ID).Set(ctx, edge)
	return err
}

// Delete удаляет DependencyEdge
func (r *DependencyEdgesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

