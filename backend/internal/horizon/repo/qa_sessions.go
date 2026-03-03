package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrQASessionNotFound = errors.New("qa session not found")

// QASessionsRepository manages QASession documents in Firestore.
// Path: horizon_lenses/{lensId}/qa_sessions/{id}
type QASessionsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

// NewQASessionsRepository creates a new repository.
// Uses a top-level collection horizon_qa_sessions with lensId for querying,
// to avoid subcollection limitations (e.g. cross-lens queries).
func NewQASessionsRepository(db *firestore.Client) *QASessionsRepository {
	if db == nil {
		return nil
	}
	return &QASessionsRepository{
		db:  db,
		col: db.Collection("horizon_qa_sessions"),
	}
}

// Create creates a new QASession.
func (r *QASessionsRepository) Create(ctx context.Context, session *models.QASession) (string, error) {
	if session == nil {
		return "", ErrInvalidDocument
	}
	session.LensID = strings.TrimSpace(session.LensID)
	session.TenantID = strings.TrimSpace(session.TenantID)
	session.ProjectID = strings.TrimSpace(session.ProjectID)
	if session.LensID == "" || session.TenantID == "" || session.ProjectID == "" {
		return "", fmt.Errorf("lensId, tenantId, projectId are required")
	}

	now := time.Now().UTC()
	session.CreatedAt = now
	session.UpdatedAt = now
	if session.Status == "" {
		session.Status = models.QASessionStatusPending
	}

	docRef := r.col.NewDoc()
	session.ID = strings.TrimSpace(session.ID)
	if session.ID != "" {
		docRef = r.col.Doc(session.ID)
	} else {
		session.ID = docRef.ID
	}

	if _, err := docRef.Set(ctx, session); err != nil {
		return "", err
	}
	return session.ID, nil
}

// Get returns a QASession by ID.
func (r *QASessionsRepository) Get(ctx context.Context, id string) (*models.QASession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrQASessionNotFound
	}
	var session models.QASession
	if err := snap.DataTo(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

// Update updates a QASession.
func (r *QASessionsRepository) Update(ctx context.Context, session *models.QASession) error {
	if session == nil || session.ID == "" {
		return ErrInvalidDocument
	}
	session.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(session.ID).Set(ctx, session)
	return err
}

// UpdateStatus updates only the status and UpdatedAt.
func (r *QASessionsRepository) UpdateStatus(ctx context.Context, id, status string) error {
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}

// UpdateWithAnswer updates the session with the answer and performance metrics.
func (r *QASessionsRepository) UpdateWithAnswer(ctx context.Context, session *models.QASession) error {
	if session == nil || session.ID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	session.UpdatedAt = now
	session.AnsweredAt = &now
	session.Status = models.QASessionStatusAnswered
	_, err := r.col.Doc(session.ID).Set(ctx, session)
	return err
}

// ListByLens returns QASessions for a lens, most recent first.
func (r *QASessionsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*models.QASession, error) {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.col.Where("lensId", "==", lensID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.QASession, 0, len(docs))
	for _, d := range docs {
		var s models.QASession
		if err := d.DataTo(&s); err != nil {
			continue
		}
		out = append(out, &s)
	}
	return out, nil
}
