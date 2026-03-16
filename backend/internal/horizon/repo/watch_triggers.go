package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const watchTriggersCollection = "horizon_watch_triggers"

type WatchTriggersRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewWatchTriggersRepository(db *firestore.Client) *WatchTriggersRepository {
	return &WatchTriggersRepository{db: db, col: db.Collection(watchTriggersCollection)}
}

func BuildWatchTriggerID(dedupeKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(dedupeKey)))
	return "wtrg_" + hex.EncodeToString(sum[:12])
}

func (r *WatchTriggersRepository) CreateOrRefresh(ctx context.Context, trigger *models.WatchTrigger) (*models.WatchTrigger, bool, error) {
	if r == nil || r.col == nil || trigger == nil {
		return nil, false, ErrInvalidDocument
	}
	if strings.TrimSpace(trigger.ID) == "" {
		trigger.ID = BuildWatchTriggerID(trigger.DedupeKey)
	}
	if err := trigger.Validate(); err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	created := false
	ref := r.col.Doc(strings.TrimSpace(trigger.ID))
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if err == nil && snap.Exists() {
			var existing models.WatchTrigger
			if err := snap.DataTo(&existing); err != nil {
				return err
			}
			existing.TriggerReason = strings.TrimSpace(trigger.TriggerReason)
			existing.CanonicalPaths = normalizeStrings(trigger.CanonicalPaths)
			existing.SourceIDs = normalizeStrings(trigger.SourceIDs)
			existing.Polarity = strings.TrimSpace(trigger.Polarity)
			existing.SignalStrength = trigger.SignalStrength
			existing.Confidence = trigger.Confidence
			existing.SemanticWatermark = strings.TrimSpace(trigger.SemanticWatermark)
			if trigger.MatchMode != "" {
				existing.MatchMode = trigger.MatchMode
			}
			existing.Metadata = trigger.Metadata
			existing.UpdatedAt = now
			return tx.Set(ref, &existing)
		}
		if trigger.CreatedAt.IsZero() {
			trigger.CreatedAt = now
		}
		trigger.UpdatedAt = now
		trigger.CanonicalPaths = normalizeStrings(trigger.CanonicalPaths)
		trigger.SourceIDs = normalizeStrings(trigger.SourceIDs)
		trigger.RunRefs = normalizeStrings(trigger.RunRefs)
		trigger.JobRefs = normalizeStrings(trigger.JobRefs)
		created = true
		return tx.Set(ref, trigger)
	})
	if err != nil {
		return nil, false, err
	}
	out, err := r.Get(ctx, trigger.ID)
	return out, created, err
}

func (r *WatchTriggersRepository) Get(ctx context.Context, id string) (*models.WatchTrigger, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var trigger models.WatchTrigger
	if err := snap.DataTo(&trigger); err != nil {
		return nil, err
	}
	if strings.TrimSpace(trigger.ID) == "" {
		trigger.ID = snap.Ref.ID
	}
	return &trigger, nil
}

func (r *WatchTriggersRepository) ListBySubject(ctx context.Context, subjectID string, limit int) ([]*models.WatchTrigger, error) {
	if r == nil || r.col == nil || strings.TrimSpace(subjectID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("subjectId", "==", strings.TrimSpace(subjectID)).OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchTrigger, 0, len(docs))
	for _, doc := range docs {
		var trigger models.WatchTrigger
		if err := doc.DataTo(&trigger); err != nil {
			continue
		}
		if strings.TrimSpace(trigger.ID) == "" {
			trigger.ID = doc.Ref.ID
		}
		out = append(out, &trigger)
	}
	return out, nil
}

func (r *WatchTriggersRepository) UpdateProcessing(ctx context.Context, trigger *models.WatchTrigger) error {
	if r == nil || r.col == nil || trigger == nil || strings.TrimSpace(trigger.ID) == "" {
		return ErrInvalidDocument
	}
	trigger.CanonicalPaths = normalizeStrings(trigger.CanonicalPaths)
	trigger.SourceIDs = normalizeStrings(trigger.SourceIDs)
	trigger.RunRefs = normalizeStrings(trigger.RunRefs)
	trigger.JobRefs = normalizeStrings(trigger.JobRefs)
	if err := trigger.Validate(); err != nil {
		return err
	}
	trigger.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(trigger.ID)).Set(ctx, trigger)
	return err
}
