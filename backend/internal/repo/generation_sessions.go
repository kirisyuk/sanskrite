package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// GenerationSessionsRepository manages generation sessions in Firestore
type GenerationSessionsRepository struct {
	col *firestore.CollectionRef
}

// NewGenerationSessionsRepository creates a new repository
func NewGenerationSessionsRepository(db *firestore.Client) *GenerationSessionsRepository {
	return &GenerationSessionsRepository{col: db.Collection("generation_sessions")}
}

// Create creates a new generation session
func (r *GenerationSessionsRepository) Create(ctx context.Context, session *models.GenerationSession) (string, error) {
	if session == nil {
		return "", errors.New("session is nil")
	}

	now := time.Now().UTC()
	session.CreatedAt = now
	session.UpdatedAt = now

	doc := r.col.NewDoc()
	session.ID = doc.ID

	if _, err := doc.Set(ctx, session); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a generation session by ID
func (r *GenerationSessionsRepository) Get(ctx context.Context, id string) (*models.GenerationSession, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var session models.GenerationSession
	if err := snap.DataTo(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

// Update updates a generation session
func (r *GenerationSessionsRepository) Update(ctx context.Context, session *models.GenerationSession) error {
	if session == nil || session.ID == "" {
		return errors.New("invalid session")
	}

	session.UpdatedAt = time.Now().UTC()

	_, err := r.col.Doc(session.ID).Set(ctx, session)
	return err
}

// UpdateFields updates specific fields of a session
func (r *GenerationSessionsRepository) UpdateFields(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return errors.New("session ID is required")
	}

	// Always update timestamp
	updates["updatedAt"] = time.Now().UTC()

	_, err := r.col.Doc(id).Set(ctx, updates, firestore.MergeAll)
	return err
}

// Delete deletes a generation session
func (r *GenerationSessionsRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// ListByArtifact lists all sessions for an artifact
func (r *GenerationSessionsRepository) ListByArtifact(ctx context.Context, artifactID string) ([]*models.GenerationSession, error) {
	docs, err := r.col.Where("artifactId", "==", artifactID).
		OrderBy("createdAt", firestore.Desc).
		Documents(ctx).GetAll()

	if err != nil {
		return nil, err
	}

	sessions := make([]*models.GenerationSession, 0, len(docs))
	for _, doc := range docs {
		var session models.GenerationSession
		if err := doc.DataTo(&session); err != nil {
			continue
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

// GetActive retrieves the active session for an artifact (if any)
func (r *GenerationSessionsRepository) GetActive(ctx context.Context, artifactID string) (*models.GenerationSession, error) {
	docs, err := r.col.Where("artifactId", "==", artifactID).
		Where("status", "in", []string{"running", "pending"}).
		OrderBy("createdAt", firestore.Desc).
		Limit(1).
		Documents(ctx).GetAll()

	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, nil // No active session
	}

	var session models.GenerationSession
	if err := docs[0].DataTo(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

// UpdateProgress updates session progress
func (r *GenerationSessionsRepository) UpdateProgress(ctx context.Context, id string, progress float64, currentSection *string) error {
	updates := map[string]interface{}{
		"progress":  progress,
		"updatedAt": time.Now().UTC(),
	}

	if currentSection != nil {
		updates["currentSection"] = *currentSection
	}

	_, err := r.col.Doc(id).Set(ctx, updates, firestore.MergeAll)
	return err
}

// AddSectionResult adds a section result to the session
func (r *GenerationSessionsRepository) AddSectionResult(ctx context.Context, id string, sectionID string, result models.SectionResult) error {
	// Use Firestore map update syntax
	updates := []firestore.Update{
		{
			Path:  "sectionResults." + sectionID,
			Value: result,
		},
		{
			Path:  "updatedAt",
			Value: time.Now().UTC(),
		},
	}

	_, err := r.col.Doc(id).Update(ctx, updates)
	return err
}

// AddError adds an error to the session
func (r *GenerationSessionsRepository) AddError(ctx context.Context, id string, genError models.GenerationError) error {
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{
			Path:  "errors",
			Value: firestore.ArrayUnion(genError),
		},
		{
			Path:  "updatedAt",
			Value: time.Now().UTC(),
		},
	})
	return err
}

// MarkCompleted marks a session as completed
func (r *GenerationSessionsRepository) MarkCompleted(ctx context.Context, id string) error {
	return r.UpdateFields(ctx, id, map[string]interface{}{
		"status":   "completed",
		"progress": 1.0,
	})
}

// MarkFailed marks a session as failed
func (r *GenerationSessionsRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	return r.UpdateFields(ctx, id, map[string]interface{}{
		"status": "failed",
		"errors": firestore.ArrayUnion(models.GenerationError{
			Message:   errorMsg,
			Timestamp: time.Now().UTC(),
		}),
	})
}

// MarkCancelled marks a session as cancelled
func (r *GenerationSessionsRepository) MarkCancelled(ctx context.Context, id string) error {
	return r.UpdateFields(ctx, id, map[string]interface{}{
		"status": "cancelled",
	})
}
