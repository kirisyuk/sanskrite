package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const signalRetroAttachQueueCollection = "horizon_signal_retro_attach_queue"

type SignalRetroAttachQueueRepository struct {
	col *firestore.CollectionRef
}

func NewSignalRetroAttachQueueRepository(db *firestore.Client) *SignalRetroAttachQueueRepository {
	if db == nil {
		return nil
	}
	return &SignalRetroAttachQueueRepository{col: db.Collection(signalRetroAttachQueueCollection)}
}

func BuildSignalRetroAttachQueueID(tenantID, canonicalPath string) string {
	base := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(canonicalPath)
	sum := sha256.Sum256([]byte(base))
	return "sraq_" + hex.EncodeToString(sum[:12])
}

func (r *SignalRetroAttachQueueRepository) UpsertObservation(ctx context.Context, item *horizonmodels.SignalRetroAttachQueueItem, maturityWindow time.Duration) error {
	if r == nil || r.col == nil || item == nil {
		return ErrInvalidDocument
	}
	if maturityWindow <= 0 {
		maturityWindow = 3 * time.Minute
	}
	if err := item.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if strings.TrimSpace(item.ID) == "" {
		item.ID = BuildSignalRetroAttachQueueID(item.TenantID, item.CanonicalPath)
	}
	docRef := r.col.Doc(item.ID)
	snap, err := docRef.Get(ctx)
	if err == nil && snap.Exists() {
		var prev horizonmodels.SignalRetroAttachQueueItem
		if uerr := snap.DataTo(&prev); uerr == nil {
			if strings.TrimSpace(item.FragmentID) == "" {
				item.FragmentID = strings.TrimSpace(prev.FragmentID)
			}
			if item.FragmentClass == "" {
				item.FragmentClass = prev.FragmentClass
			}
			if item.FirstSeenAt.IsZero() || (!prev.FirstSeenAt.IsZero() && prev.FirstSeenAt.Before(item.FirstSeenAt)) {
				item.FirstSeenAt = prev.FirstSeenAt
			}
			item.ObservedCount += prev.ObservedCount
			item.AttemptCount = prev.AttemptCount
			item.LastError = strings.TrimSpace(prev.LastError)
			if prev.Status == horizonmodels.SignalRetroAttachStatusProcessing {
				item.Status = horizonmodels.SignalRetroAttachStatusPending
			}
		}
	}
	item.Status = horizonmodels.SignalRetroAttachStatusPending
	item.LastSeenAt = now
	item.ReadyAt = now.Add(maturityWindow)
	item.UpdatedAt = now
	_, err = docRef.Set(ctx, item)
	if err != nil {
		return fmt.Errorf("retro attach queue upsert: %w", err)
	}
	return nil
}

func (r *SignalRetroAttachQueueRepository) ListReady(ctx context.Context, limit int, now time.Time) ([]*horizonmodels.SignalRetroAttachQueueItem, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 32
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	docs, err := r.col.Limit(200).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.SignalRetroAttachQueueItem, 0, len(docs))
	for _, doc := range docs {
		var item horizonmodels.SignalRetroAttachQueueItem
		if err := doc.DataTo(&item); err != nil {
			continue
		}
		if item.ID == "" {
			item.ID = doc.Ref.ID
		}
		if item.Status != horizonmodels.SignalRetroAttachStatusPending && item.Status != horizonmodels.SignalRetroAttachStatusFailed {
			continue
		}
		if item.ReadyAt.After(now) {
			continue
		}
		out = append(out, &item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReadyAt.Equal(out[j].ReadyAt) {
			return out[i].LastSeenAt.Before(out[j].LastSeenAt)
		}
		return out[i].ReadyAt.Before(out[j].ReadyAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *SignalRetroAttachQueueRepository) UpdateStatus(ctx context.Context, id string, status horizonmodels.SignalRetroAttachStatus, attemptCount int, lastResult, lastError string) error {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(strings.TrimSpace(id)).Set(ctx, map[string]any{
		"status":       string(status),
		"attemptCount": attemptCount,
		"lastResult":   strings.TrimSpace(lastResult),
		"lastError":    strings.TrimSpace(lastError),
		"updatedAt":    time.Now().UTC(),
	}, firestore.MergeAll)
	return err
}
