package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrFrameNotFound = errors.New("frame not found")

// FramesRepository репозиторий для работы с Frame
type FramesRepository struct {
	col *firestore.CollectionRef
}

// NewFramesRepository создает новый репозиторий
func NewFramesRepository(db *firestore.Client) *FramesRepository {
	return &FramesRepository{
		col: db.Collection("horizon_frames"),
	}
}

// Create создает новый Frame
func (r *FramesRepository) Create(ctx context.Context, frame *models.Frame) (string, error) {
	if frame == nil {
		return "", ErrInvalidDocument
	}
	if err := frame.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	frame.CreatedAt = now

	doc := r.col.NewDoc()
	frame.ID = doc.ID

	if _, err := doc.Set(ctx, frame); err != nil {
		return "", err
	}
	return frame.ID, nil
}

// Get получает Frame по ID
func (r *FramesRepository) Get(ctx context.Context, id string) (*models.Frame, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrFrameNotFound
	}
	var frame models.Frame
	if err := snap.DataTo(&frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

// ListByScoutRequestID получает список Frame по ScoutRequestID
func (r *FramesRepository) ListByScoutRequestID(ctx context.Context, tenantID, scoutRequestID string, limit int) ([]*models.Frame, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("scoutRequestId", "==", scoutRequestID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var frames []*models.Frame
	for _, d := range docs {
		var frame models.Frame
		if err := d.DataTo(&frame); err != nil {
			continue
		}
		frames = append(frames, &frame)
	}
	return frames, nil
}

// ListByTargetCanonicalPath получает историю Frame для целевого узла
func (r *FramesRepository) ListByTargetCanonicalPath(ctx context.Context, tenantID, targetCanonicalPath string, limit int) ([]*models.Frame, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("targetCanonicalPath", "==", targetCanonicalPath).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var frames []*models.Frame
	for _, d := range docs {
		var frame models.Frame
		if err := d.DataTo(&frame); err != nil {
			continue
		}
		frames = append(frames, &frame)
	}
	return frames, nil
}

// Delete удаляет Frame
func (r *FramesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

