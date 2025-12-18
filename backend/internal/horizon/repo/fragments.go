package repo

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrFragmentNotFound = errors.New("horizon fragment not found")

// HorizonFragmentsRepository репозиторий для работы с HorizonFragment
type HorizonFragmentsRepository struct {
	col              *firestore.CollectionRef
	db               *firestore.Client
	versionsRepo     *HorizonFragmentVersionsRepository // Репозиторий для версий
	storage          *gcp.GCSClient
	bucket           string
	useGCS           bool
	uploadLimiter    *rate.Limiter                                                                // Rate limiter for GCS uploads
	limiterMu        sync.Mutex
	vertexSearchSync func(ctx context.Context, tenantID string) (operationName string, err error) // Sync trigger
}

// NewHorizonFragmentsRepository создает новый репозиторий
func NewHorizonFragmentsRepository(db *firestore.Client) *HorizonFragmentsRepository {
	return &HorizonFragmentsRepository{
		col:          db.Collection("horizon_fragments"),
		db:           db,
		versionsRepo: NewHorizonFragmentVersionsRepository(db),
	}
}

// WithStorage настраивает GCS хранилище для экспорта фрагментов
func (r *HorizonFragmentsRepository) WithStorage(storage *gcp.GCSClient, bucket string) *HorizonFragmentsRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && bucket != ""
	if r.useGCS {
		r.uploadLimiter = rate.NewLimiter(rate.Limit(10), 10)
	}
	return r
}

// WithVertexSearchSync настраивает синхронизацию с Vertex AI Search
func (r *HorizonFragmentsRepository) WithVertexSearchSync(syncFn func(ctx context.Context, tenantID string) (operationName string, err error)) *HorizonFragmentsRepository {
	r.vertexSearchSync = syncFn
	return r
}

// uploadToGCS экспортирует фрагмент в GCS для индексации
func (r *HorizonFragmentsRepository) uploadToGCS(ctx context.Context, fragment *models.HorizonFragment) error {
	if !r.useGCS {
		return nil
	}
	if r.uploadLimiter != nil {
		if err := r.uploadLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	// Генерируем расширенный текстовый контент
	content := fragment.GenerateVectorText()

	// Формируем JSON документ для Vertex AI Search
	// Используем схему, совместимую с "Unstructured with metadata" или "Structured"
	// Vertex AI Search автоматически парсит JSON файлы
	doc := map[string]interface{}{
		"id":      fragment.ID,
		"title":   fragment.CanonicalPath,
		"content": content, // Основной текст для поиска
		"structData": map[string]interface{}{
			"type":          "horizon_fragment",
			"tenantId":      fragment.TenantID,
			"canonicalPath": fragment.CanonicalPath,
			"entityPath":    fragment.EntityPath,
			"sectorPath":    fragment.SectorPath,
			"status":        fragment.Governance.Status,
			"updatedAt":     fragment.UpdatedAt,
		},
	}

	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal fragment to json: %w", err)
	}

	// Сохраняем как JSON в папку horizon_fragments
	// Путь: global/horizon_fragments/{id}.json
	objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
	
	metadata := map[string]string{
		"type":     "horizon_fragment",
		"tenantId": fragment.TenantID,
	}

	log.Printf("[HorizonFragments] Exporting fragment %s (%s) to GCS for Vertex Indexing...", fragment.ID, fragment.CanonicalPath)
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, jsonData, "application/json", metadata); err != nil {
		return fmt.Errorf("failed to upload fragment to GCS: %w", err)
	}

	// Триггерим синхронизацию
	if r.vertexSearchSync != nil {
		go func(tenantID string) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if op, err := r.vertexSearchSync(syncCtx, tenantID); err == nil {
				log.Printf("[HorizonFragments] Vertex AI Sync triggered: %s", op)
			}
		}(fragment.TenantID)
	}

	return nil
}

// Create создает новый HorizonFragment
func (r *HorizonFragmentsRepository) Create(ctx context.Context, fragment *models.HorizonFragment) (string, error) {
	if fragment == nil {
		return "", ErrInvalidDocument
	}
	if err := fragment.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	fragment.CreatedAt = now
	fragment.UpdatedAt = now
	fragment.Version = 1
	fragment.TenantID = "global" // Всегда глобальный

	// Инициализация Governance если не установлено
	if fragment.Governance.Owner == "" {
		fragment.Governance.Owner = "SYSTEM"
		fragment.Governance.Status = string(models.FragmentStatusActive)
		fragment.Governance.ActiveSubscribers = 0
	}

	// Инициализация Metadata
	if fragment.Metadata.CreatedAt.IsZero() {
		fragment.Metadata.CreatedAt = now
		fragment.Metadata.LastUpdatedAt = now
		fragment.Metadata.LastVerifiedAt = now
		fragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
	}

	doc := r.col.NewDoc()
	fragment.ID = doc.ID

	if _, err := doc.Set(ctx, fragment); err != nil {
		return "", err
	}

	// Экспорт в GCS для поиска
	if err := r.uploadToGCS(ctx, fragment); err != nil {
		// Логируем, но не фейлим создание
		log.Printf("WARNING: failed to export fragment to GCS: %v", err)
	}

	return fragment.ID, nil
}

// Get получает HorizonFragment по ID
func (r *HorizonFragmentsRepository) Get(ctx context.Context, id string) (*models.HorizonFragment, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrFragmentNotFound
	}
	var fragment models.HorizonFragment
	if err := snap.DataTo(&fragment); err != nil {
		return nil, err
	}
	return &fragment, nil
}

// GetByCanonicalPath получает HorizonFragment по canonicalPath (всегда глобальный)
func (r *HorizonFragmentsRepository) GetByCanonicalPath(ctx context.Context, canonicalPath string) (*models.HorizonFragment, error) {
	if canonicalPath == "" {
		return nil, ErrInvalidDocument
	}
	q := r.col.Where("tenantId", "==", "global").Where("canonicalPath", "==", canonicalPath).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrFragmentNotFound
	}
	var fragment models.HorizonFragment
	if err := docs[0].DataTo(&fragment); err != nil {
		return nil, err
	}
	return &fragment, nil
}

// Update обновляет HorizonFragment и сохраняет версию
func (r *HorizonFragmentsRepository) Update(ctx context.Context, fragment *models.HorizonFragment) error {
	if fragment == nil || fragment.ID == "" {
		return ErrInvalidDocument
	}
	if err := fragment.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Получаем текущую версию фрагмента для сохранения
	currentFragment, err := r.Get(ctx, fragment.ID)
	if err != nil {
		return fmt.Errorf("failed to get current fragment: %w", err)
	}

	now := time.Now().UTC()
	fragment.UpdatedAt = now
	fragment.Version++
	fragment.Metadata.LastUpdatedAt = now

	// Сохраняем версию перед обновлением
	version := &HorizonFragmentVersion{
		FragmentID:    currentFragment.ID,
		TenantID:      currentFragment.TenantID,
		State:         currentFragment.State,
		SourceFrameID: currentFragment.Metadata.SourceFrameID,
		Timestamp:     now,
	}
	if _, err := r.versionsRepo.Create(ctx, version); err != nil {
		// Логируем ошибку, но продолжаем обновление фрагмента
		// Версионирование не должно блокировать обновление
		_ = err
	}

	_, err = r.col.Doc(fragment.ID).Set(ctx, fragment)
	if err != nil {
		return err
	}

	// Экспорт в GCS для поиска
	if err := r.uploadToGCS(ctx, fragment); err != nil {
		log.Printf("WARNING: failed to export updated fragment to GCS: %v", err)
	}

	return nil
}

// UpdateGovernance обновляет только Governance (для управления подписками)
func (r *HorizonFragmentsRepository) UpdateGovernance(ctx context.Context, id string, governance models.Governance) error {
	if id == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "governance", Value: governance},
		{Path: "updatedAt", Value: now},
		{Path: "metadata.lastUpdatedAt", Value: now},
	})
	return err
}

// IncrementActiveSubscribers увеличивает счетчик активных подписчиков
func (r *HorizonFragmentsRepository) IncrementActiveSubscribers(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	docRef := r.col.Doc(id)
	// Используем транзакцию для атомарного увеличения
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			return err
		}
		fragment.Governance.ActiveSubscribers++
		if fragment.Governance.ActiveSubscribers > 0 && fragment.Governance.Status == string(models.FragmentStatusDormant) {
			fragment.Governance.Status = string(models.FragmentStatusActive)
		}
		fragment.UpdatedAt = now
		fragment.Metadata.LastUpdatedAt = now
		return tx.Set(docRef, fragment)
	})
	return err
}

// DecrementActiveSubscribers уменьшает счетчик активных подписчиков
func (r *HorizonFragmentsRepository) DecrementActiveSubscribers(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	docRef := r.col.Doc(id)
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			return err
		}
		if fragment.Governance.ActiveSubscribers > 0 {
			fragment.Governance.ActiveSubscribers--
		}
		if fragment.Governance.ActiveSubscribers == 0 {
			fragment.Governance.Status = string(models.FragmentStatusDormant)
		}
		fragment.UpdatedAt = now
		fragment.Metadata.LastUpdatedAt = now
		return tx.Set(docRef, fragment)
	})
	return err
}

// ListByStatus получает список HorizonFragment по статусу
func (r *HorizonFragmentsRepository) ListByStatus(ctx context.Context, status string, limit int) ([]*models.HorizonFragment, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", "global").Where("governance.status", "==", status).OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var fragments []*models.HorizonFragment
	for _, d := range docs {
		var fragment models.HorizonFragment
		if err := d.DataTo(&fragment); err != nil {
			continue
		}
		fragments = append(fragments, &fragment)
	}
	return fragments, nil
}

// ListByCanonicalPaths получает список HorizonFragment по массиву canonicalPath
func (r *HorizonFragmentsRepository) ListByCanonicalPaths(ctx context.Context, canonicalPaths []string) ([]*models.HorizonFragment, error) {
	if len(canonicalPaths) == 0 {
		return []*models.HorizonFragment{}, nil
	}
	// Firestore не поддерживает IN для больших массивов, делаем батчи по 10
	var fragments []*models.HorizonFragment
	batchSize := 10
	for i := 0; i < len(canonicalPaths); i += batchSize {
		end := i + batchSize
		if end > len(canonicalPaths) {
			end = len(canonicalPaths)
		}
		batch := canonicalPaths[i:end]
		q := r.col.Where("tenantId", "==", "global").Where("canonicalPath", "in", batch)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			var fragment models.HorizonFragment
			if err := d.DataTo(&fragment); err != nil {
				continue
			}
			fragments = append(fragments, &fragment)
		}
	}
	return fragments, nil
}

// SearchByVectorText выполняет семантический поиск по vectorEmbedding
// Требует наличия векторного индекса в Firestore
func (r *HorizonFragmentsRepository) SearchByVectorText(ctx context.Context, queryEmbedding []float64, limit int) ([]*models.HorizonFragment, error) {
	if limit <= 0 {
		limit = 10
	}
	// Firestore Vector Search будет реализован через специальный запрос
	// Пока возвращаем ошибку, так как требует настройки индекса
	return nil, fmt.Errorf("vector search not yet implemented, requires Firestore vector index")
}

// Delete удаляет HorizonFragment
func (r *HorizonFragmentsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

var ErrInvalidDocument = errors.New("invalid document")

