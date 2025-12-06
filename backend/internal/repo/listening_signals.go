package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// Create creates a new listening signal
func (r *ListeningSignalsRepository) Create(ctx context.Context, signal *models.ListeningSignal) (string, error) {
	if signal == nil || signal.Name == "" || signal.Signal == "" || signal.TenantID == "" {
		return "", ErrInvalidDocument
	}

	now := time.Now().UTC()
	signal.CreatedAt = now
	signal.UpdatedAt = now

	doc := r.col.NewDoc()
	signal.ID = doc.ID

	if _, err := doc.Set(ctx, signal); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a signal by ID
func (r *ListeningSignalsRepository) Get(ctx context.Context, id string) (*models.ListeningSignal, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var signal models.ListeningSignal
	if err := snap.DataTo(&signal); err != nil {
		return nil, err
	}

	return &signal, nil
}

// Upsert creates or updates a signal
func (r *ListeningSignalsRepository) Upsert(ctx context.Context, signal *models.ListeningSignal) error {
	if signal == nil || signal.Name == "" || signal.Signal == "" || signal.TenantID == "" {
		return ErrInvalidDocument
	}

	now := time.Now().UTC()
	if signal.CreatedAt.IsZero() {
		signal.CreatedAt = now
	}
	signal.UpdatedAt = now

	if signal.ID == "" {
		doc := r.col.NewDoc()
		signal.ID = doc.ID
	}

	_, err := r.col.Doc(signal.ID).Set(ctx, signal)
	return err
}

// ListByTenant lists all signals for a tenant (optionally filtered by project)
func (r *ListeningSignalsRepository) ListByTenant(ctx context.Context, tenantID string, projectID *string) ([]*models.ListeningSignal, error) {
	var q firestore.Query
	if projectID != nil && *projectID != "" {
		// Filter by tenant and project
		q = r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", *projectID)
	} else {
		// Filter by tenant only (includes tenant-wide signals where projectId is null)
		q = r.col.Where("tenantId", "==", tenantID)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	signals := make([]*models.ListeningSignal, 0, len(docs))
	for _, doc := range docs {
		var signal models.ListeningSignal
		if err := doc.DataTo(&signal); err != nil {
			continue
		}
		// If projectID filter is nil, also include tenant-wide signals (projectId == nil)
		if projectID == nil && signal.ProjectID != nil {
			// Skip project-specific signals when listing tenant-wide
			continue
		}
		signals = append(signals, &signal)
	}

	return signals, nil
}

// ListByProject lists signals for a specific project (includes tenant-wide signals)
func (r *ListeningSignalsRepository) ListByProject(ctx context.Context, tenantID, projectID string) ([]*models.ListeningSignal, error) {
	// Get project-specific signals
	projectSignals, err := r.ListByTenant(ctx, tenantID, &projectID)
	if err != nil {
		return nil, err
	}

	// Get tenant-wide signals (projectId == nil)
	tenantWideDocs, err := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", nil).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	tenantWideSignals := make([]*models.ListeningSignal, 0, len(tenantWideDocs))
	for _, doc := range tenantWideDocs {
		var signal models.ListeningSignal
		if err := doc.DataTo(&signal); err != nil {
			continue
		}
		tenantWideSignals = append(tenantWideSignals, &signal)
	}

	// Combine both lists
	result := make([]*models.ListeningSignal, 0, len(projectSignals)+len(tenantWideSignals))
	result = append(result, projectSignals...)
	result = append(result, tenantWideSignals...)

	return result, nil
}

// Update updates an existing signal
func (r *ListeningSignalsRepository) Update(ctx context.Context, signal *models.ListeningSignal) error {
	if signal == nil || signal.ID == "" {
		return ErrInvalidDocument
	}

	signal.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(signal.ID).Set(ctx, signal)
	return err
}

// Delete deletes a signal
func (r *ListeningSignalsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// ListActive lists only active signals for a tenant/project
func (r *ListeningSignalsRepository) ListActive(ctx context.Context, tenantID string, projectID *string) ([]*models.ListeningSignal, error) {
	all, err := r.ListByTenant(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	active := make([]*models.ListeningSignal, 0)
	for _, signal := range all {
		if signal.IsActive {
			active = append(active, signal)
		}
	}

	return active, nil
}

// ListAllActive lists ALL active signals across all tenants (for background monitoring)
func (r *ListeningSignalsRepository) ListAllActive(ctx context.Context, limit int) ([]*models.ListeningSignal, error) {
	if limit <= 0 {
		limit = 1000 // Default limit
	}

	docs, err := r.col.Where("isActive", "==", true).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	signals := make([]*models.ListeningSignal, 0, len(docs))
	for _, doc := range docs {
		var signal models.ListeningSignal
		if err := doc.DataTo(&signal); err != nil {
			continue
		}
		signals = append(signals, &signal)
	}

	return signals, nil
}

