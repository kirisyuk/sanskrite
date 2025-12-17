package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrWatchdogConfigNotFound = errors.New("watchdog config not found")

// WatchdogConfigsRepository репозиторий для работы с WatchdogConfig
type WatchdogConfigsRepository struct {
	col *firestore.CollectionRef
}

// NewWatchdogConfigsRepository создает новый репозиторий
func NewWatchdogConfigsRepository(db *firestore.Client) *WatchdogConfigsRepository {
	return &WatchdogConfigsRepository{
		col: db.Collection("horizon_watchdog_configs"),
	}
}

// Create создает новый WatchdogConfig
func (r *WatchdogConfigsRepository) Create(ctx context.Context, config *models.WatchdogConfig) (string, error) {
	if config == nil {
		return "", ErrInvalidDocument
	}
	if err := config.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	config.CreatedAt = now
	config.UpdatedAt = now

	doc := r.col.NewDoc()
	config.ID = doc.ID

	if _, err := doc.Set(ctx, config); err != nil {
		return "", err
	}
	return config.ID, nil
}

// Get получает WatchdogConfig по ID
func (r *WatchdogConfigsRepository) Get(ctx context.Context, id string) (*models.WatchdogConfig, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrWatchdogConfigNotFound
	}
	var config models.WatchdogConfig
	if err := snap.DataTo(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// GetByTargetFragmentID получает WatchdogConfig по targetFragmentID
func (r *WatchdogConfigsRepository) GetByTargetFragmentID(ctx context.Context, tenantID, targetFragmentID string) (*models.WatchdogConfig, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", targetFragmentID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrWatchdogConfigNotFound
	}
	var config models.WatchdogConfig
	if err := docs[0].DataTo(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// ListScheduled получает список WatchdogConfig, готовых к сканированию
func (r *WatchdogConfigsRepository) ListScheduled(ctx context.Context, tenantID string, limit int) ([]*models.WatchdogConfig, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC()
	q := r.col.Where("tenantId", "==", tenantID).Where("status", "==", string(models.WatchdogStatusRunning)).Where("nextScanAt", "<=", now).OrderBy("nextScanAt", firestore.Asc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var configs []*models.WatchdogConfig
	for _, d := range docs {
		var config models.WatchdogConfig
		if err := d.DataTo(&config); err != nil {
			continue
		}
		configs = append(configs, &config)
	}
	return configs, nil
}

// Update обновляет WatchdogConfig
func (r *WatchdogConfigsRepository) Update(ctx context.Context, config *models.WatchdogConfig) error {
	if config == nil || config.ID == "" {
		return ErrInvalidDocument
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	config.UpdatedAt = now

	_, err := r.col.Doc(config.ID).Set(ctx, config)
	return err
}

// UpdateScanTime обновляет время последнего и следующего сканирования
func (r *WatchdogConfigsRepository) UpdateScanTime(ctx context.Context, id string, lastScanAt, nextScanAt time.Time) error {
	if id == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "lastScanAt", Value: lastScanAt},
		{Path: "nextScanAt", Value: nextScanAt},
		{Path: "updatedAt", Value: now},
	})
	return err
}

// Delete удаляет WatchdogConfig
func (r *WatchdogConfigsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

