package repo

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const compiledWatchSubjectsCollection = "horizon_watch_subject_projection"

type CompiledWatchSubjectsRepository struct {
	col *firestore.CollectionRef
}

func NewCompiledWatchSubjectsRepository(db *firestore.Client) *CompiledWatchSubjectsRepository {
	return &CompiledWatchSubjectsRepository{col: db.Collection(compiledWatchSubjectsCollection)}
}

func (r *CompiledWatchSubjectsRepository) Upsert(ctx context.Context, compiled *models.CompiledWatchSubject) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("compiled watch subjects repo not configured")
	}
	if compiled == nil {
		return ErrInvalidDocument
	}
	compiled.Normalize()
	if compiled.WatchSubjectID == "" || compiled.TenantID == "" {
		return ErrInvalidDocument
	}
	compiled.ID = strings.TrimSpace(compiled.WatchSubjectID)
	_, err := r.col.Doc(compiled.ID).Set(ctx, compiled)
	return err
}

func (r *CompiledWatchSubjectsRepository) Get(ctx context.Context, watchSubjectID string) (*models.CompiledWatchSubject, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("compiled watch subjects repo not configured")
	}
	watchSubjectID = strings.TrimSpace(watchSubjectID)
	if watchSubjectID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(watchSubjectID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var compiled models.CompiledWatchSubject
	if err := snap.DataTo(&compiled); err != nil {
		return nil, err
	}
	if strings.TrimSpace(compiled.ID) == "" {
		compiled.ID = snap.Ref.ID
	}
	return &compiled, nil
}
