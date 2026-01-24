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

var ErrLensNotFound = errors.New("lens not found")

// LensesRepository репозиторий для работы с Lens
type LensesRepository struct {
	col *firestore.CollectionRef
}

// NewLensesRepository создает новый репозиторий
func NewLensesRepository(db *firestore.Client) *LensesRepository {
	return &LensesRepository{
		col: db.Collection("horizon_lenses"),
	}
}

// Create создает новый Lens
func (r *LensesRepository) Create(ctx context.Context, lens *models.Lens) (string, error) {
	if lens == nil {
		return "", ErrInvalidDocument
	}
	if err := lens.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	lens.CreatedAt = now
	lens.UpdatedAt = now

	doc := r.col.NewDoc()
	lens.ID = doc.ID

	if _, err := doc.Set(ctx, lens); err != nil {
		return "", err
	}
	return lens.ID, nil
}

// Get получает Lens по ID
func (r *LensesRepository) Get(ctx context.Context, id string) (*models.Lens, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrLensNotFound
	}
	var lens models.Lens
	if err := snap.DataTo(&lens); err != nil {
		return nil, err
	}
	return &lens, nil
}

// ListByProject получает список Lens для проекта
func (r *LensesRepository) ListByProject(ctx context.Context, tenantID, projectID string, status *string, limit int) ([]*models.Lens, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID)
	if status != nil && *status != "" {
		q = q.Where("status", "==", *status)
	}
	q = q.OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var lenses []*models.Lens
	for _, d := range docs {
		var lens models.Lens
		if err := d.DataTo(&lens); err != nil {
			continue
		}
		lenses = append(lenses, &lens)
	}
	return lenses, nil
}

// ListActive получает список активных Lens
func (r *LensesRepository) ListActive(ctx context.Context, tenantID string, limit int) ([]*models.Lens, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("status", "==", string(models.LensStatusActive)).OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var lenses []*models.Lens
	for _, d := range docs {
		var lens models.Lens
		if err := d.DataTo(&lens); err != nil {
			continue
		}
		lenses = append(lenses, &lens)
	}
	return lenses, nil
}

// FindByScope получает Lens, которые включают указанный canonicalPath в scope
func (r *LensesRepository) FindByScope(ctx context.Context, tenantID, canonicalPath string, limit int) ([]*models.Lens, error) {
	if limit <= 0 {
		limit = 100
	}
	// Используем array-contains для поиска в scope
	q := r.col.Where("tenantId", "==", tenantID).Where("scope", "array-contains", canonicalPath).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var lenses []*models.Lens
	for _, d := range docs {
		var lens models.Lens
		if err := d.DataTo(&lens); err != nil {
			continue
		}
		lenses = append(lenses, &lens)
	}
	return lenses, nil
}

// Update обновляет Lens
func (r *LensesRepository) Update(ctx context.Context, lens *models.Lens) error {
	if lens == nil || lens.ID == "" {
		return ErrInvalidDocument
	}
	if err := lens.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	lens.UpdatedAt = now

	_, err := r.col.Doc(lens.ID).Set(ctx, lens)
	return err
}

// Delete удаляет Lens
func (r *LensesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// AddScopeCanonicalPaths appends canonical paths to lens.scope using array union.
// This is intentionally partial (does not validate full Lens), to support incremental workflows (e.g., domain expansion).
func (r *LensesRepository) AddScopeCanonicalPaths(ctx context.Context, lensID string, canonicalPaths []string) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	if len(canonicalPaths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	unionArgs := make([]interface{}, 0, len(canonicalPaths))
	for _, p := range canonicalPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unionArgs = append(unionArgs, p)
	}
	if len(unionArgs) == 0 {
		return nil
	}
	updates := map[string]interface{}{
		"scope":     firestore.ArrayUnion(unionArgs...),
		"updatedAt": time.Now().UTC(),
	}
	_, err := r.col.Doc(lensID).Set(ctx, updates, firestore.MergeAll)
	return err
}

// AddExposurePathKey appends a canonical exposure key string to the lens document using array union.
// This is intentionally partial (does not validate full Lens), to support incremental workflows.
func (r *LensesRepository) AddExposurePathKey(ctx context.Context, lensID string, rawKey string) (string, error) {
	if lensID == "" {
		return "", ErrInvalidDocument
	}
	canon, _, err := models.CanonicalizeExposureKey(rawKey)
	if err != nil {
		return "", err
	}
	updates := map[string]interface{}{
		"exposurePaths": firestore.ArrayUnion(canon),
		"updatedAt":     time.Now().UTC(),
	}
	_, err = r.col.Doc(lensID).Set(ctx, updates, firestore.MergeAll)
	return canon, err
}

