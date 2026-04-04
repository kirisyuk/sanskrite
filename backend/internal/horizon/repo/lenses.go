package repo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/lensresearch"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrLensNotFound = errors.New("lens not found")

// LensesRepository репозиторий для работы с Lens
type LensesRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

// NewLensesRepository создает новый репозиторий
func NewLensesRepository(db *firestore.Client) *LensesRepository {
	return &LensesRepository{
		db:  db,
		col: db.Collection("horizon_lenses"),
	}
}

// AutoScopeAddResult describes the outcome of automatic scope expansion.
type AutoScopeAddResult struct {
	Added                 bool
	AlreadyInScope        bool
	LimitReached          bool
	DeferredScopeAddCount int
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

func (r *LensesRepository) FindByScopeAnyTenant(ctx context.Context, canonicalPath string, limit int) ([]*models.Lens, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("scope", "array-contains", canonicalPath).Limit(limit)
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

// UpdateInternalKnowledgeState updates only the internalKnowledge view-config payload.
// This avoids rewriting the entire lens document for warmup status transitions.
func (r *LensesRepository) UpdateInternalKnowledgeState(ctx context.Context, lensID string, state map[string]any) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	if state == nil {
		state = map[string]any{}
	}
	_, err := r.col.Doc(lensID).Update(ctx, []firestore.Update{
		{Path: "viewConfig.internalKnowledge", Value: state},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}

// PatchInternalKnowledgeStateFields updates individual fields inside viewConfig.internalKnowledge
// without replacing the whole payload. This is useful for low-cost queued/running transitions.
func (r *LensesRepository) PatchInternalKnowledgeStateFields(ctx context.Context, lensID string, fields map[string]any) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	updates := make([]firestore.Update, 0, len(fields)+1)
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		updates = append(updates, firestore.Update{
			Path:  "viewConfig.internalKnowledge." + key,
			Value: value,
		})
	}
	updates = append(updates, firestore.Update{Path: "updatedAt", Value: time.Now().UTC()})
	if len(updates) == 1 {
		return nil
	}
	_, err := r.col.Doc(lensID).Update(ctx, updates)
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

// RemoveScopeCanonicalPaths removes canonical paths from lens.scope using array remove.
// Used for orphan cleanup when scope paths have no corresponding fragment.
func (r *LensesRepository) RemoveScopeCanonicalPaths(ctx context.Context, lensID string, canonicalPaths []string) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	if len(canonicalPaths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	removeArgs := make([]interface{}, 0, len(canonicalPaths))
	for _, p := range canonicalPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		removeArgs = append(removeArgs, p)
	}
	if len(removeArgs) == 0 {
		return nil
	}
	updates := map[string]interface{}{
		"scope":     firestore.ArrayRemove(removeArgs...),
		"updatedAt": time.Now().UTC(),
	}
	_, err := r.col.Doc(lensID).Set(ctx, updates, firestore.MergeAll)
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

// AddScopeCanonicalPathAutoLimited appends one fragment-backed canonical path to lens.scope
// when below maxCount. Use AddScopeCanonicalPathAutoManaged when the caller may pass
// /domain/ or /artifact/ scope entries.
func (r *LensesRepository) AddScopeCanonicalPathAutoLimited(
	ctx context.Context,
	lensID, canonicalPath string,
	maxCount int,
) (AutoScopeAddResult, error) {
	return r.addScopeCanonicalPathAutoLimitedWithCounter(ctx, lensID, canonicalPath, maxCount, models.ScopeAutoAddEntityCounterKey)
}

// AddScopeCanonicalPathAutoManaged appends one scope entry using the correct auto-add policy:
// domain entries are always allowed, artifact refs use their own budget, and fragment-backed
// entities use the legacy deferredScopeAddCount budget.
func (r *LensesRepository) AddScopeCanonicalPathAutoManaged(
	ctx context.Context,
	lensID, canonicalPath string,
	maxCount int,
) (AutoScopeAddResult, error) {
	counterKey := models.ScopeAutoAddCounterKey(canonicalPath)
	if counterKey == "" {
		return r.addScopeCanonicalPathUnbounded(ctx, lensID, canonicalPath)
	}
	return r.addScopeCanonicalPathAutoLimitedWithCounter(ctx, lensID, canonicalPath, maxCount, counterKey)
}

func (r *LensesRepository) addScopeCanonicalPathUnbounded(
	ctx context.Context,
	lensID, canonicalPath string,
) (AutoScopeAddResult, error) {
	out := AutoScopeAddResult{}
	if lensID == "" || strings.TrimSpace(canonicalPath) == "" {
		return out, ErrInvalidDocument
	}
	if r == nil || r.db == nil {
		return out, ErrInvalidDocument
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	docRef := r.col.Doc(lensID)

	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		if !snap.Exists() {
			return ErrLensNotFound
		}

		var lens models.Lens
		if err := snap.DataTo(&lens); err != nil {
			return err
		}

		for _, s := range lens.Scope {
			if strings.TrimSpace(s) == canonicalPath {
				out.AlreadyInScope = true
				return nil
			}
		}

		if err := tx.Set(docRef, map[string]interface{}{
			"scope":     firestore.ArrayUnion(canonicalPath),
			"updatedAt": time.Now().UTC(),
		}, firestore.MergeAll); err != nil {
			return err
		}
		out.Added = true
		return nil
	})
	return out, err
}

func (r *LensesRepository) addScopeCanonicalPathAutoLimitedWithCounter(
	ctx context.Context,
	lensID, canonicalPath string,
	maxCount int,
	counterKey string,
) (AutoScopeAddResult, error) {
	out := AutoScopeAddResult{}
	if lensID == "" || strings.TrimSpace(canonicalPath) == "" || maxCount <= 0 || strings.TrimSpace(counterKey) == "" {
		return out, ErrInvalidDocument
	}
	if r == nil || r.db == nil {
		return out, ErrInvalidDocument
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	counterKey = strings.TrimSpace(counterKey)
	docRef := r.col.Doc(lensID)

	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		if !snap.Exists() {
			return ErrLensNotFound
		}

		var lens models.Lens
		if err := snap.DataTo(&lens); err != nil {
			return err
		}

		currentCount := scopeAutoAddCount(lens.ViewConfig, counterKey)
		out.DeferredScopeAddCount = currentCount
		for _, s := range lens.Scope {
			if strings.TrimSpace(s) == canonicalPath {
				out.AlreadyInScope = true
				return nil
			}
		}
		if currentCount >= maxCount {
			out.LimitReached = true
			return nil
		}

		if lens.ViewConfig == nil {
			lens.ViewConfig = map[string]interface{}{}
		}
		lens.ViewConfig[counterKey] = currentCount + 1
		if err := tx.Set(docRef, map[string]interface{}{
			"scope":      firestore.ArrayUnion(canonicalPath),
			"viewConfig": lens.ViewConfig,
			"updatedAt":  time.Now().UTC(),
		}, firestore.MergeAll); err != nil {
			return err
		}
		out.Added = true
		out.DeferredScopeAddCount = currentCount + 1
		return nil
	})
	return out, err
}

// IncrementDeferredScopeAddCount increments viewConfig.deferredScopeAddCount atomically.
// This avoids full document rewrites when Scout appends scope entries concurrently.
func (r *LensesRepository) IncrementDeferredScopeAddCount(ctx context.Context, lensID string, delta int64) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	if delta == 0 {
		return nil
	}
	_, err := r.col.Doc(lensID).Update(ctx, []firestore.Update{
		{Path: "viewConfig.deferredScopeAddCount", Value: firestore.Increment(delta)},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}

func scopeAutoAddCount(viewConfig map[string]interface{}, key string) int {
	if viewConfig == nil {
		return 0
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	raw, ok := viewConfig[key]
	if !ok {
		return 0
	}
	switch n := raw.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return parsed
		}
	}
	return 0
}

// UpsertScopeArtifact atomically sets a single key in the lens scopeArtifacts map.
// Uses Firestore dot-notation so only the specified key is touched without overwriting
// the entire scopeArtifacts map.
func (r *LensesRepository) UpsertScopeArtifact(ctx context.Context, lensID, artKey string, ref models.ScopeArtifactRef) error {
	if lensID == "" || artKey == "" {
		return ErrInvalidDocument
	}
	key := sanitizeFirestoreKey(artKey)
	_, err := r.col.Doc(lensID).Set(ctx, map[string]interface{}{
		"scopeArtifacts." + key: ref,
		"updatedAt":             time.Now().UTC(),
	}, firestore.MergeAll)
	return err
}

// RemoveScopeArtifact removes a single key from lens scopeArtifacts map.
func (r *LensesRepository) RemoveScopeArtifact(ctx context.Context, lensID, artKey string) error {
	if lensID == "" || artKey == "" {
		return ErrInvalidDocument
	}
	lens, err := r.Get(ctx, lensID)
	if err != nil {
		return err
	}
	key := sanitizeFirestoreKey(artKey)
	if lens.ScopeArtifacts == nil {
		return nil
	}
	delete(lens.ScopeArtifacts, key)
	_, err = r.col.Doc(lensID).Set(ctx, map[string]interface{}{
		"scopeArtifacts": lens.ScopeArtifacts,
		"updatedAt":      time.Now().UTC(),
	}, firestore.MergeAll)
	return err
}

// MergeResearchObjects merges recovered research object refs into lens.researchObjects without duplicates.
func (r *LensesRepository) MergeResearchObjects(ctx context.Context, lensID string, incoming map[string]models.LensResearchObjectRef) error {
	if lensID == "" {
		return ErrInvalidDocument
	}
	if len(incoming) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return ErrInvalidDocument
	}
	docRef := r.col.Doc(lensID)
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		if !snap.Exists() {
			return ErrLensNotFound
		}

		var lens models.Lens
		if err := snap.DataTo(&lens); err != nil {
			return err
		}
		merged := lensresearch.MergeResearchObjects(lens.ResearchObjects, incoming)
		if len(merged) == 0 {
			return nil
		}
		return tx.Set(docRef, map[string]interface{}{
			"researchObjects": merged,
			"updatedAt":       time.Now().UTC(),
		}, firestore.MergeAll)
	})
}

// sanitizeFirestoreKey replaces dots in keys (Firestore uses dots as path separators in field paths).
func sanitizeFirestoreKey(key string) string {
	return strings.ReplaceAll(strings.TrimSpace(key), ".", "_")
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
