package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrInvalidProjectScenario = errors.New("invalid project scenario")

// Create creates a new project scenario
func (r *ProjectScenariosRepository) Create(ctx context.Context, s *models.ProjectScenario) (string, error) {
	if s == nil || s.TenantID == "" || s.ProjectID == "" || s.Title == "" {
		return "", ErrInvalidProjectScenario
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Status == "" {
		s.Status = "active"
	}
	doc := r.col.NewDoc()
	s.ID = doc.ID
	if _, err := doc.Set(ctx, s); err != nil {
		return "", err
	}
	return s.ID, nil
}

// Get retrieves a project scenario by ID
func (r *ProjectScenariosRepository) Get(ctx context.Context, id string) (*models.ProjectScenario, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var s models.ProjectScenario
	if err := snap.DataTo(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListByProject lists all scenarios for a project, optionally filtered by parent
func (r *ProjectScenariosRepository) ListByProject(ctx context.Context, tenantID, projectID string, parentID *string) ([]*models.ProjectScenario, error) {
	// Always load all scenarios for the project first, then filter by parentId if needed
	// This is necessary because Firestore's Where("parentId", "==", nil) doesn't match
	// documents where the field is missing (which is common for root scenarios)
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID)
	q = q.OrderBy("order", firestore.Asc).OrderBy("createdAt", firestore.Asc)
	
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	
	var scenarios []*models.ProjectScenario
	for _, d := range docs {
		var s models.ProjectScenario
		if err := d.DataTo(&s); err != nil {
			continue
		}
		
		// Filter by parentId if specified
		if parentID != nil {
			if *parentID == "" {
				// List root scenarios (no parent) - parentId is nil, empty string, or missing
				if s.ParentID == nil || *s.ParentID == "" {
					scenarios = append(scenarios, &s)
				}
			} else {
				// List children of specific parent
				if s.ParentID != nil && *s.ParentID == *parentID {
					scenarios = append(scenarios, &s)
				}
			}
		} else {
			// No filter - return all scenarios
			scenarios = append(scenarios, &s)
		}
	}
	return scenarios, nil
}

// ListByParent lists all child scenarios for a given parent scenario
func (r *ProjectScenariosRepository) ListByParent(ctx context.Context, tenantID, parentID string) ([]*models.ProjectScenario, error) {
	q := r.col.Where("tenantId", "==", tenantID).
		Where("parentId", "==", parentID).
		OrderBy("order", firestore.Asc).
		OrderBy("createdAt", firestore.Asc)
	
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	
	var scenarios []*models.ProjectScenario
	for _, d := range docs {
		var s models.ProjectScenario
		if err := d.DataTo(&s); err != nil {
			continue
		}
		scenarios = append(scenarios, &s)
	}
	return scenarios, nil
}

// Upsert updates or creates a project scenario
func (r *ProjectScenariosRepository) Upsert(ctx context.Context, s *models.ProjectScenario) error {
	if s == nil || s.ID == "" || s.TenantID == "" || s.ProjectID == "" {
		return ErrInvalidProjectScenario
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Status == "" {
		s.Status = "active"
	}
	_, err := r.col.Doc(s.ID).Set(ctx, s)
	return err
}

// Delete deletes a project scenario by ID
func (r *ProjectScenariosRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByProject deletes all scenarios for a project (for cascade delete)
func (r *ProjectScenariosRepository) DeleteByProject(ctx context.Context, tenantID, projectID string) error {
	// Get all scenarios for the project
	scenarios, err := r.ListByProject(ctx, tenantID, projectID, nil)
	if err != nil {
		return err
	}
	
	// Delete all scenarios recursively (including children)
	for _, s := range scenarios {
		if err := r.DeleteRecursive(ctx, s.ID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRecursive deletes a scenario and all its children recursively
func (r *ProjectScenariosRepository) DeleteRecursive(ctx context.Context, id string) error {
	// Get children first
	scenario, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	
	// Get all children
	children, err := r.ListByParent(ctx, scenario.TenantID, id)
	if err != nil {
		return err
	}
	
	// Delete children recursively
	for _, child := range children {
		if err := r.DeleteRecursive(ctx, child.ID); err != nil {
			return err
		}
	}
	
	// Delete this scenario
	return r.Delete(ctx, id)
}

// CountByProject returns the count of scenarios for a project
func (r *ProjectScenariosRepository) CountByProject(ctx context.Context, tenantID, projectID string) (int, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

