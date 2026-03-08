package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type SourceRuntimeStatesRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

func NewSourceRuntimeStatesRepository(db *firestore.Client) *SourceRuntimeStatesRepository {
	return &SourceRuntimeStatesRepository{
		col: db.Collection("source_runtime_state"),
		db:  db,
	}
}

func (r *SourceRuntimeStatesRepository) Get(ctx context.Context, sourceID string) (*models.SourceRuntimeState, error) {
	if r == nil || r.col == nil || sourceID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(sourceID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var state models.SourceRuntimeState
	if err := snap.DataTo(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *SourceRuntimeStatesRepository) Upsert(ctx context.Context, state *models.SourceRuntimeState) error {
	if r == nil || r.col == nil || state == nil || state.SourceID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if state.ID == "" {
		state.ID = state.SourceID
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	_, err := r.col.Doc(state.SourceID).Set(ctx, state)
	return err
}

func (r *SourceRuntimeStatesRepository) Mutate(ctx context.Context, sourceID string, fn func(state *models.SourceRuntimeState) error) (*models.SourceRuntimeState, error) {
	if r == nil || r.col == nil || r.db == nil || sourceID == "" || fn == nil {
		return nil, ErrInvalidDocument
	}
	var updated models.SourceRuntimeState
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		now := time.Now().UTC()
		docRef := r.col.Doc(sourceID)
		snap, err := tx.Get(docRef)
		if err != nil && !isNotFoundErr(err) {
			return err
		}
		state := models.SourceRuntimeState{
			ID:       sourceID,
			SourceID: sourceID,
		}
		if err == nil && snap != nil && snap.Exists() {
			if derr := snap.DataTo(&state); derr != nil {
				return derr
			}
		}
		if state.ID == "" {
			state.ID = sourceID
		}
		if state.SourceID == "" {
			state.SourceID = sourceID
		}
		if state.CreatedAt.IsZero() {
			state.CreatedAt = now
		}
		if err := fn(&state); err != nil {
			return err
		}
		state.UpdatedAt = now
		updated = state
		return tx.Set(docRef, &state)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return status.Code(err) == codes.NotFound
}
