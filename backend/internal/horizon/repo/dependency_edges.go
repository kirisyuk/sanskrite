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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
)

var ErrEdgeNotFound = errors.New("dependency edge not found")

// =============================================================================
// Context hooks (best-effort)
// =============================================================================
//
// These hooks allow higher-level pipelines (e.g. Scout) to collect "affected" fragment IDs
// while creating edges, without extra Firestore reads or additional plumbing across many call sites.
// This is intentionally best-effort and must never change persistence semantics.

type edgeAffectCollectorKey struct{}

// WithEdgeAffectCollector attaches a collector to ctx. Repository methods may call it after successful writes.
// The collector is called with FINAL (possibly canonicalized/swapped) edge endpoints.
func WithEdgeAffectCollector(ctx context.Context, collector func(sourceFragmentID, targetFragmentID string)) context.Context {
	if ctx == nil || collector == nil {
		return ctx
	}
	return context.WithValue(ctx, edgeAffectCollectorKey{}, collector)
}

func getEdgeAffectCollector(ctx context.Context) func(string, string) {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(edgeAffectCollectorKey{}); v != nil {
		if fn, ok := v.(func(string, string)); ok {
			return fn
		}
	}
	return nil
}

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
		// Get source and target fragment types and paths for validation
		var sourceType, targetType string
		var sourcePath, targetPath string
		if r.fragmentsRepo != nil {
			if sourceFrag, err := r.fragmentsRepo.Get(ctx, edge.SourceFragmentID); err == nil && sourceFrag != nil {
				sourceType = string(sourceFrag.Type)
				sourcePath = sourceFrag.CanonicalPath
				log.Printf("%s Source fragment type: %s, path: %s", logPrefix, sourceType, sourcePath)
			}
			if targetFrag, err := r.fragmentsRepo.Get(ctx, edge.TargetFragmentID); err == nil && targetFrag != nil {
				targetType = string(targetFrag.Type)
				targetPath = targetFrag.CanonicalPath
				log.Printf("%s Target fragment type: %s, path: %s", logPrefix, targetType, targetPath)
			}
		}

		// Canonicalize certain mis-modeled edges before running validation policy.
		// Example: PRODUCT OWNS ORGANIZATION -> ORGANIZATION PRODUCES PRODUCT (not ORG OWNS PRODUCT).
		// Use path-aware canonicalization to handle community subtypes (dynasty/tribe).
		if sourceType != "" && targetType != "" {
			if newType, shouldSwap, changed, reason := models.CanonicalizeEdgeWithPaths(edge.Type, models.FragmentType(sourceType), models.FragmentType(targetType), sourcePath, targetPath); changed {
				oldType := edge.Type
				edge.Type = newType
				if shouldSwap {
					edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
					sourceType, targetType = targetType, sourceType
					sourcePath, targetPath = targetPath, sourcePath
				}
				if edge.Qualifiers == nil {
					edge.Qualifiers = map[string]interface{}{}
				}
				edge.Qualifiers["canonicalized"] = true
				edge.Qualifiers["canonicalizedFromEdgeType"] = string(oldType)
				edge.Qualifiers["canonicalizationReason"] = reason
				edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
				if shouldSwap {
					edge.Qualifiers["normalizedDirection"] = true
				}
				log.Printf("%s Canonicalized edge: %s -> %s (%s) [from %s] reason=%s",
					logPrefix, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, oldType, reason)
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

		policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
			Reverse: func() {
				edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			},
			ApplyPenalty: func(penalty float64, _ string) {
				if edge.Properties.Criticality > 0 {
					edge.Properties.Criticality *= (1.0 - penalty)
					log.Printf("%s Adjusted criticality: %.4f", logPrefix, edge.Properties.Criticality)
				}
			},
		})
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
	tracking.RecordEdgeCreated(ctx, edge.ID)

	// Best-effort: report affected endpoints to caller (if collector attached to ctx).
	if fn := getEdgeAffectCollector(ctx); fn != nil {
		fn(edge.SourceFragmentID, edge.TargetFragmentID)
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

// CountDegreesForFragmentIDs scans edges and counts degrees for the provided fragment IDs.
// This avoids per-fragment queries by scanning edges once.
func (r *DependencyEdgesRepository) CountDegreesForFragmentIDs(ctx context.Context, tenantID string, fragmentIDs map[string]struct{}, pageSize int) (map[string]int, error) {
	if pageSize <= 0 {
		pageSize = 2000
	}
	if pageSize > 5000 {
		pageSize = 5000
	}

	counts := make(map[string]int)
	if len(fragmentIDs) == 0 {
		return counts, nil
	}

	q := r.col.Where("tenantId", "==", tenantID).OrderBy(firestore.DocumentID, firestore.Asc)
	var lastDoc *firestore.DocumentSnapshot

	for {
		paged := q.Limit(pageSize)
		if lastDoc != nil {
			paged = paged.StartAfter(lastDoc)
		}

		docs, err := paged.Documents(ctx).GetAll()
		if err != nil {
			return counts, err
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if _, ok := fragmentIDs[edge.SourceFragmentID]; ok {
				counts[edge.SourceFragmentID]++
			}
			if _, ok := fragmentIDs[edge.TargetFragmentID]; ok {
				counts[edge.TargetFragmentID]++
			}
		}

		lastDoc = docs[len(docs)-1]
	}

	return counts, nil
}

// EdgeQueryOptions provides optional filtering for edge queries.
type EdgeQueryOptions struct {
	IncludeHistorical bool // If false (default), filter out edges where ValidTo < now
	IncludeFuture     bool // If false (default), filter out edges where ValidFrom > now
}

// ListBySourceWithOptions gets outgoing edges with filtering options.
// When options.IncludeHistorical is false, historical edges (ValidTo < now) are filtered out.
// When options.IncludeFuture is false, future edges (ValidFrom > now) are filtered out.
func (r *DependencyEdgesRepository) ListBySourceWithOptions(ctx context.Context, tenantID, sourceFragmentID string, edgeType *models.DependencyType, limit int, opts *EdgeQueryOptions) ([]*models.DependencyEdge, error) {
	edges, err := r.ListBySource(ctx, tenantID, sourceFragmentID, edgeType, limit)
	if err != nil {
		return nil, err
	}
	return filterEdgesByOptions(edges, opts), nil
}

// ListByTargetWithOptions gets incoming edges with filtering options.
// When options.IncludeHistorical is false, historical edges (ValidTo < now) are filtered out.
// When options.IncludeFuture is false, future edges (ValidFrom > now) are filtered out.
func (r *DependencyEdgesRepository) ListByTargetWithOptions(ctx context.Context, tenantID, targetFragmentID string, edgeType *models.DependencyType, limit int, opts *EdgeQueryOptions) ([]*models.DependencyEdge, error) {
	edges, err := r.ListByTarget(ctx, tenantID, targetFragmentID, edgeType, limit)
	if err != nil {
		return nil, err
	}
	return filterEdgesByOptions(edges, opts), nil
}

// filterEdgesByOptions applies temporal filtering based on options.
// This is done in-memory because Firestore doesn't support null-field comparisons efficiently.
func filterEdgesByOptions(edges []*models.DependencyEdge, opts *EdgeQueryOptions) []*models.DependencyEdge {
	if opts == nil {
		opts = &EdgeQueryOptions{} // Default: exclude historical and future
	}

	// If including everything, return as-is
	if opts.IncludeHistorical && opts.IncludeFuture {
		return edges
	}

	result := make([]*models.DependencyEdge, 0, len(edges))
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		// Filter historical edges
		if !opts.IncludeHistorical && edge.IsHistorical() {
			continue
		}
		// Filter future edges
		if !opts.IncludeFuture && edge.IsFuture() {
			continue
		}
		result = append(result, edge)
	}
	return result
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
	if err != nil {
		return err
	}
	tracking.RecordEdgeUpdated(ctx, edge.ID)
	return nil
}

// Delete удаляет DependencyEdge
func (r *DependencyEdgesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}
