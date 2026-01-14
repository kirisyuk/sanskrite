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

var ErrLensItemNotFound = errors.New("lens item not found")

// LensItemsRepository репозиторий для работы с LensItem
type LensItemsRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewLensItemsRepository создает новый репозиторий
func NewLensItemsRepository(db *firestore.Client) *LensItemsRepository {
	return &LensItemsRepository{
		col: db.Collection("horizon_lens_items"),
		db:  db,
	}
}

// Create создает новый LensItem
func (r *LensItemsRepository) Create(ctx context.Context, item *models.LensItem) (string, error) {
	if item == nil {
		return "", ErrInvalidDocument
	}
	if err := item.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now

	doc := r.col.NewDoc()
	item.ID = doc.ID

	if _, err := doc.Set(ctx, item); err != nil {
		return "", err
	}
	return item.ID, nil
}

// Get возвращает LensItem по ID
func (r *LensItemsRepository) Get(ctx context.Context, id string) (*models.LensItem, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrLensItemNotFound
	}
	var item models.LensItem
	if err := snap.DataTo(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

// Update применяет частичное обновление к LensItem по ID используя merge семантику
func (r *LensItemsRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return ErrInvalidDocument
	}
	if len(updates) == 0 {
		return nil
	}
	// ensure server-side updated CreatedAt is not accidentally overwritten
	delete(updates, "id")
	delete(updates, "tenantId")
	delete(updates, "projectId")
	delete(updates, "lensId")
	delete(updates, "createdAt")

	// For nested objects like metadata, we need to merge them properly
	if metadata, ok := updates["metadata"].(map[string]interface{}); ok {
		// Get current item to merge metadata properly
		current, err := r.Get(ctx, id)
		if err == nil && current != nil && current.Metadata != nil {
			// Merge current metadata with updates
			currentMeta := map[string]interface{}{}
			if current.Metadata.Pinned != nil {
				currentMeta["pinned"] = *current.Metadata.Pinned
			}
			if len(current.Metadata.Notes) > 0 {
				currentMeta["notes"] = current.Metadata.Notes
			}
			if current.Metadata.Visibility != nil {
				currentMeta["visibility"] = *current.Metadata.Visibility
			}
			if current.Metadata.Color != nil {
				currentMeta["color"] = *current.Metadata.Color
			}
			// Apply updates to current metadata
			for k, v := range metadata {
				currentMeta[k] = v
			}
			updates["metadata"] = currentMeta
		}
	}

	// Always update UpdatedAt
	updates["updatedAt"] = time.Now().UTC()

	_, err := r.col.Doc(id).Set(ctx, updates, firestore.MergeAll)
	return err
}

// DeleteByID удаляет LensItem по ID
func (r *LensItemsRepository) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByProject удаляет все LensItems для проекта
func (r *LensItemsRepository) DeleteByProject(ctx context.Context, projectID string) error {
	docs, err := r.col.Where("projectId", "==", projectID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

// DeleteByLens удаляет все LensItems для линзы
func (r *LensItemsRepository) DeleteByLens(ctx context.Context, lensID string) error {
	docs, err := r.col.Where("lensId", "==", lensID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

// DeleteByTenantAndLens deletes all LensItems for a given (tenantId, lensId).
// This is used for cascade cleanup to avoid cross-tenant deletions on id collisions.
func (r *LensItemsRepository) DeleteByTenantAndLens(ctx context.Context, tenantID, lensID string) (int, error) {
	if r == nil || r.col == nil || r.db == nil {
		return 0, ErrInvalidDocument
	}
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	if tenantID == "" || lensID == "" {
		return 0, ErrInvalidDocument
	}
	const batchSize = 200
	const maxBatches = 50
	deleted := 0
	for i := 0; i < maxBatches; i++ {
		docs, err := r.col.Where("tenantId", "==", tenantID).Where("lensId", "==", lensID).Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return deleted, err
		}
		if len(docs) == 0 {
			return deleted, nil
		}
		b := r.db.Batch()
		for _, d := range docs {
			b.Delete(d.Ref)
		}
		if _, err := b.Commit(ctx); err != nil {
			return deleted, err
		}
		deleted += len(docs)
		if len(docs) < batchSize {
			return deleted, nil
		}
	}
	return deleted, nil
}

// ListByProject возвращает LensItems для проекта, отсортированные по createdAt asc
func (r *LensItemsRepository) ListByProject(ctx context.Context, projectID string) ([]*models.LensItem, error) {
	q := r.col.Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var items []*models.LensItem
	for _, d := range docs {
		var item models.LensItem
		if err := d.DataTo(&item); err != nil {
			continue
		}
		items = append(items, &item)
	}
	return items, nil
}

// ListByLens возвращает LensItems для линзы, отсортированные по createdAt asc
func (r *LensItemsRepository) ListByLens(ctx context.Context, lensID string) ([]*models.LensItem, error) {
	q := r.col.Where("lensId", "==", lensID).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var items []*models.LensItem
	for _, d := range docs {
		var item models.LensItem
		if err := d.DataTo(&item); err != nil {
			continue
		}
		items = append(items, &item)
	}
	return items, nil
}

// ListByLensAndKind возвращает LensItems для линзы определенного kind, отсортированные по createdAt asc
func (r *LensItemsRepository) ListByLensAndKind(ctx context.Context, lensID, kind string) ([]*models.LensItem, error) {
	q := r.col.Where("lensId", "==", lensID).Where("kind", "==", kind).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var items []*models.LensItem
	for _, d := range docs {
		var item models.LensItem
		if err := d.DataTo(&item); err != nil {
			continue
		}
		items = append(items, &item)
	}
	return items, nil
}







