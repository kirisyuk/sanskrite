package repo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

var ErrEdgeNotFound = errors.New("dependency edge not found")

// DependencyEdgesRepository репозиторий для работы с DependencyEdge
type DependencyEdgesRepository struct {
	col            *firestore.CollectionRef
	validationPolicy *policy.PolicyExecutor // Optional: for edge validation
	fragmentsRepo   *HorizonFragmentsRepository // Optional: for fragment type lookup
}

// NewDependencyEdgesRepository создает новый репозиторий
func NewDependencyEdgesRepository(db *firestore.Client) *DependencyEdgesRepository {
	return &DependencyEdgesRepository{
		col: db.Collection("horizon_dependency_edges"),
	}
}

// WithValidationPolicy sets the validation policy executor.
func (r *DependencyEdgesRepository) WithValidationPolicy(executor *policy.PolicyExecutor) *DependencyEdgesRepository {
	r.validationPolicy = executor
	return r
}

// WithFragmentsRepo sets the fragments repository for type lookup.
func (r *DependencyEdgesRepository) WithFragmentsRepo(repo *HorizonFragmentsRepository) *DependencyEdgesRepository {
	r.fragmentsRepo = repo
	return r
}

// Create создает новый DependencyEdge
func (r *DependencyEdgesRepository) Create(ctx context.Context, edge *models.DependencyEdge) (string, error) {
	logPrefix := "[DependencyEdgesRepository:Create]"
	
	if edge == nil {
		return "", ErrInvalidDocument
	}
	
	log.Printf("%s Creating edge: sourceID=%s, targetID=%s, type=%s", 
		logPrefix, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)

	// Basic validation
	if err := edge.Validate(); err != nil {
		log.Printf("%s ERROR: basic validation failed: %v", logPrefix, err)
		return "", fmt.Errorf("validation error: %w", err)
	}

	// Policy-based validation if available
	if r.validationPolicy != nil {
		// Get source and target fragment types for validation
		var sourceType, targetType string
		if r.fragmentsRepo != nil {
			if sourceFrag, err := r.fragmentsRepo.Get(ctx, edge.SourceFragmentID); err == nil && sourceFrag != nil {
				sourceType = string(sourceFrag.Type)
				log.Printf("%s Source fragment type: %s", logPrefix, sourceType)
			}
			if targetFrag, err := r.fragmentsRepo.Get(ctx, edge.TargetFragmentID); err == nil && targetFrag != nil {
				targetType = string(targetFrag.Type)
				log.Printf("%s Target fragment type: %s", logPrefix, targetType)
			}
		}

		decision, err := r.validationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "edge",
			Context: map[string]interface{}{
				"validationKind":   "edge",
				"sourceFragmentID": edge.SourceFragmentID,
				"targetFragmentID": edge.TargetFragmentID,
				"sourceType":       sourceType,
				"targetType":       targetType,
				"relationType":     string(edge.Type),
			},
			Edge: edge,
		})

		if err != nil {
			log.Printf("%s ERROR: policy validation failed: %v", logPrefix, err)
			return "", fmt.Errorf("policy validation error: %w", err)
		}

		log.Printf("%s Policy decision: action=%s, path=%s, confidence=%.2f", 
			logPrefix, decision.Action, decision.DecisionPath, decision.Confidence)

		switch decision.Action {
		case "reject":
			// Don't block - treat as warning instead
			// ValidationPolicy "reject" is advisory, not a blocker
			reason, _ := decision.Params["reason"].(string)
			log.Printf("%s WARNING: ValidationPolicy would reject edge (non-blocking): %s", logPrefix, reason)
			log.Printf("%s Edge will be created with warning - can be fixed post-factum", logPrefix)
			// Continue - don't block edge creation
		case "reverse":
			shouldReverse, _ := decision.Params["shouldReverse"].(bool)
			if shouldReverse {
				log.Printf("%s REVERSING edge direction", logPrefix)
				edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			}
		case "penalty":
			penalty, _ := decision.Params["penalty"].(float64)
			reason, _ := decision.Params["reason"].(string)
			log.Printf("%s PENALTY applied: %.2f, reason: %s", logPrefix, penalty, reason)
			// Apply penalty to confidence
			if edge.Properties.Criticality > 0 {
				edge.Properties.Criticality *= (1.0 - penalty)
				log.Printf("%s Adjusted criticality: %.4f", logPrefix, edge.Properties.Criticality)
			}
		case "allow":
			log.Printf("%s ALLOWED by policy", logPrefix)
		default:
			log.Printf("%s WARNING: unknown policy action: %s, allowing", logPrefix, decision.Action)
		}
	}

	now := time.Now().UTC()
	edge.Metadata.CreatedAt = now
	edge.Metadata.LastUpdatedAt = now

	doc := r.col.NewDoc()
	edge.ID = doc.ID

	log.Printf("%s Saving edge to Firestore: id=%s", logPrefix, edge.ID)
	if _, err := doc.Set(ctx, edge); err != nil {
		log.Printf("%s ERROR: failed to save edge: %v", logPrefix, err)
		return "", err
	}
	
	log.Printf("%s Edge created successfully: id=%s", logPrefix, edge.ID)
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

// GetBySourceTargetAndType получает DependencyEdge по source, target и типу
func (r *DependencyEdgesRepository) GetBySourceTargetAndType(ctx context.Context, tenantID, sourceFragmentID, targetFragmentID string, edgeType models.DependencyType) (*models.DependencyEdge, error) {
	q := r.col.Where("tenantId", "==", tenantID).
		Where("sourceFragmentId", "==", sourceFragmentID).
		Where("targetFragmentId", "==", targetFragmentID).
		Where("type", "==", edgeType).
		Limit(1)

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
