package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchPreferencesRepository struct {
	col *firestore.CollectionRef
}

func NewWatchPreferencesRepository(db *firestore.Client) *WatchPreferencesRepository {
	return &WatchPreferencesRepository{col: db.Collection("horizon_watch_preferences")}
}

func watchPreferencesDocID(tenantID, userID string) string {
	return fmt.Sprintf("%s_%s", strings.TrimSpace(tenantID), strings.TrimSpace(userID))
}

func (r *WatchPreferencesRepository) Upsert(ctx context.Context, prefs *models.WatchPreferences) error {
	if r == nil || r.col == nil || prefs == nil {
		return ErrInvalidDocument
	}
	if err := prefs.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if prefs.CreatedAt.IsZero() {
		prefs.CreatedAt = now
	}
	prefs.UpdatedAt = now
	if strings.TrimSpace(prefs.ID) == "" {
		prefs.ID = watchPreferencesDocID(prefs.TenantID, prefs.UserID)
	}
	_, err := r.col.Doc(strings.TrimSpace(prefs.ID)).Set(ctx, prefs)
	return err
}

func (r *WatchPreferencesRepository) GetByUser(ctx context.Context, tenantID, userID string) (*models.WatchPreferences, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(watchPreferencesDocID(tenantID, userID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var prefs models.WatchPreferences
	if err := snap.DataTo(&prefs); err != nil {
		return nil, err
	}
	if prefs.ID == "" {
		prefs.ID = snap.Ref.ID
	}
	return &prefs, nil
}
