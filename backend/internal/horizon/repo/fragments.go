package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var ErrFragmentNotFound = errors.New("horizon fragment not found")

// bestEffortAssembleCanonicalPathFromParts rebuilds fragment.CanonicalPath from CanonicalParts, if present.
// It is intentionally fail-open:
// - If parts are incomplete (or cannot assemble), it keeps the existing canonicalPath and logs a warning.
// - This allows gradual adoption: callers may start writing canonicalParts, while older code still writes canonicalPath.
func bestEffortAssembleCanonicalPathFromParts(fragment *models.HorizonFragment) {
	if fragment == nil || fragment.CanonicalParts == nil {
		return
	}
	parts := fragment.CanonicalParts
	built, ok := parts.BuildCanonicalPath()
	if !ok || strings.TrimSpace(built) == "" {
		log.Printf("[HorizonFragments] WARNING: canonicalParts present but incomplete; keeping canonicalPath=%q fragmentId=%q namespace=%q",
			strings.TrimSpace(fragment.CanonicalPath), strings.TrimSpace(fragment.ID), strings.TrimSpace(parts.Namespace))
		return
	}
	// Only update when changed to avoid noisy updates/exports.
	if strings.TrimSpace(fragment.CanonicalPath) != strings.TrimSpace(built) {
		fragment.CanonicalPath = strings.TrimSpace(built)
	}
}

// bestEffortBackfillCanonicalPartsFromPath parses fragment.CanonicalPath into fragment.CanonicalParts.
// This is intended for gradual migration: most of the pipeline still produces canonicalPath strings,
// but we want canonicalParts available for selective canonizers and safe, deterministic rewrites.
//
// Safety:
// - Only fills CanonicalParts when it is currently nil.
// - Fail-open: if parsing fails, keeps CanonicalParts nil and does not block writes.
func bestEffortBackfillCanonicalPartsFromPath(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	if fragment.CanonicalParts != nil {
		return
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if cp == "" {
		return
	}
	parts, err := taxonomy.ParseCanonicalPathParts(cp)
	if err != nil || parts == nil {
		log.Printf("[HorizonFragments] WARNING: failed to parse canonicalParts from canonicalPath=%q fragmentId=%q err=%v",
			cp, strings.TrimSpace(fragment.ID), err)
		return
	}
	fragment.CanonicalParts = parts
}

// HorizonFragmentsRepository репозиторий для работы с HorizonFragment
type HorizonFragmentsRepository struct {
	col           *firestore.CollectionRef
	db            *firestore.Client
	versionsRepo  *HorizonFragmentVersionsRepository // Репозиторий для версий
	storage       *gcp.GCSClient
	bucket        string
	useGCS        bool
	uploadLimiter *rate.Limiter // Rate limiter for GCS uploads
	limiterMu     sync.Mutex
	onUpsert      func(ctx context.Context, fragment *models.HorizonFragment) // Best-effort hook after create/update
	// vertexSearchSync triggers Vertex AI Search import for specific GCS URIs.
	// We pass the exact object URI to avoid Discovery Engine file expansion limits (max 1000 files per import).
	vertexSearchSync func(ctx context.Context, gcsURI string) (operationName string, err error) // Sync trigger (legacy, single URI)
	// vertexSyncBatcher batches multiple URIs to reduce API quota usage
	vertexSyncBatcher *VertexSyncBatcher
}

// NewHorizonFragmentsRepository создает новый репозиторий
func NewHorizonFragmentsRepository(db *firestore.Client) *HorizonFragmentsRepository {
	return &HorizonFragmentsRepository{
		col:          db.Collection("horizon_fragments"),
		db:           db,
		versionsRepo: NewHorizonFragmentVersionsRepository(db),
	}
}

// UpdateHighProfile updates only the `highProfile` field of a fragment (best-effort, field-level update).
// This avoids clobbering concurrent writes to other parts of the fragment during background computations.
//
// If hp is nil, the field is deleted.
func (r *HorizonFragmentsRepository) UpdateHighProfile(ctx context.Context, fragmentID string, hp *models.HighProfile) error {
	if r == nil || r.col == nil {
		return ErrInvalidDocument
	}
	id := strings.TrimSpace(fragmentID)
	if id == "" {
		return ErrInvalidDocument
	}

	now := time.Now().UTC()
	updates := []firestore.Update{
		{Path: "updatedAt", Value: now},
	}

	if hp == nil {
		updates = append(updates, firestore.Update{Path: "highProfile", Value: firestore.Delete})
	} else {
		if hp.Version == 0 {
			hp.Version = 1
		}
		if hp.UpdatedAt.IsZero() {
			hp.UpdatedAt = now
		}
		updates = append(updates, firestore.Update{Path: "highProfile", Value: hp})
	}

	_, err := r.col.Doc(id).Update(ctx, updates)
	return err
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

// WithVertexSearchSync настраивает синхронизацию с Vertex AI Search (legacy single-URI mode).
// Deprecated: use WithVertexSyncBatcher for better quota management.
func (r *HorizonFragmentsRepository) WithVertexSearchSync(syncFn func(ctx context.Context, gcsURI string) (operationName string, err error)) *HorizonFragmentsRepository {
	r.vertexSearchSync = syncFn
	return r
}

// WithVertexSyncBatcher configures batched Vertex AI Search sync.
// This is the preferred method as it batches multiple URIs into single API calls,
// significantly reducing quota usage (quota is per-request, not per-document).
func (r *HorizonFragmentsRepository) WithVertexSyncBatcher(batcher *VertexSyncBatcher) *HorizonFragmentsRepository {
	r.vertexSyncBatcher = batcher
	return r
}

// WithOnUpsert registers a best-effort callback called after Create/Update succeeds.
// Intended for side-effects like linking/enriching Entities.
func (r *HorizonFragmentsRepository) WithOnUpsert(fn func(ctx context.Context, fragment *models.HorizonFragment)) *HorizonFragmentsRepository {
	r.onUpsert = fn
	return r
}

// ExportToGCS экспортирует фрагмент в GCS для индексации без обновления в Firestore
// Используется для массового re-export существующих фрагментов
// skipSync: если true, не триггерит синхронизацию Vertex AI Search (для массового экспорта)
func (r *HorizonFragmentsRepository) ExportToGCS(ctx context.Context, fragment *models.HorizonFragment, skipSync bool) error {
	return r.uploadToGCS(ctx, fragment, skipSync)
}

// uploadToGCS экспортирует фрагмент в GCS для индексации
// skipSync: если true, не триггерит синхронизацию Vertex AI Search
func (r *HorizonFragmentsRepository) uploadToGCS(ctx context.Context, fragment *models.HorizonFragment, skipSync bool) error {
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
	// Формат совместим с Discovery Engine dataSchema=document.
	// ВАЖНО: Discovery Engine ожидает Document.content как объект (mimeType/rawBytes), а не строку.
	objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
	gcsURI := fmt.Sprintf("gs://%s/%s", r.bucket, objectPath)

	doc := map[string]interface{}{
		"id": fragment.ID,
		// IMPORTANT: ImportDocuments(dataSchema=document) is strict about top-level fields.
		// Keep only documented top-level fields (id/content/structData) to avoid parse errors.
		"content": map[string]interface{}{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]interface{}{
			"tenantId":      fragment.TenantID,
			"type":          "horizon_fragment",
			"canonicalPath": fragment.CanonicalPath,
			"entityPath":    fragment.EntityPath,
			"sectorPath":    fragment.SectorPath,
			"indexPaths":    fragment.IndexPaths,
			"status":        fragment.Governance.Status,
			"updatedAt":     fragment.UpdatedAt.Format(time.RFC3339),
			"createdAt":     fragment.CreatedAt.Format(time.RFC3339),
			"uri":           gcsURI,
		},
	}

	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal fragment to json: %w", err)
	}

	metadata := map[string]string{
		"type":     "horizon_fragment",
		"tenantId": fragment.TenantID,
	}

	log.Printf("[HorizonFragments] Exporting fragment %s (%s) to GCS for Vertex Indexing...", fragment.ID, fragment.CanonicalPath)
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, jsonData, "application/json", metadata); err != nil {
		return fmt.Errorf("failed to upload fragment to GCS: %w", err)
	}

	// Trigger Vertex sync (if not skipped)
	if !skipSync {
		// Prefer batcher over legacy single-URI sync
		if r.vertexSyncBatcher != nil {
			r.vertexSyncBatcher.Add(gcsURI)
		} else if r.vertexSearchSync != nil {
			// Legacy: single-URI sync (deprecated, high quota usage)
			go func() {
				syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if op, err := r.vertexSearchSync(syncCtx, gcsURI); err == nil {
					log.Printf("[HorizonFragments] Vertex AI Sync triggered: %s", op)
				} else {
					log.Printf("[HorizonFragments] Vertex AI Sync failed for %s: %v", gcsURI, err)
				}
			}()
		}
	}

	return nil
}

// Create создает новый HorizonFragment
func (r *HorizonFragmentsRepository) Create(ctx context.Context, fragment *models.HorizonFragment) (string, error) {
	if fragment == nil {
		return "", ErrInvalidDocument
	}
	// Best-effort: if only canonicalPath was provided, parse it into canonicalParts for future selective fixes.
	bestEffortBackfillCanonicalPartsFromPath(fragment)
	// Best-effort: if canonicalParts were provided, assemble canonicalPath before validation.
	bestEffortAssembleCanonicalPathFromParts(fragment)
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

	// Build index paths for search/filtering
	fragment.IndexPaths = models.BuildIndexPaths(fragment, nil)

	if _, err := doc.Set(ctx, fragment); err != nil {
		return "", err
	}
	tracking.RecordFragmentCreated(ctx, fragment.ID)

	// Экспорт в GCS для поиска (с синхронизацией)
	if err := r.uploadToGCS(ctx, fragment, false); err != nil {
		// Логируем, но не фейлим создание
		log.Printf("WARNING: failed to export fragment to GCS: %v", err)
	}

	// Best-effort onUpsert hook (async)
	if r.onUpsert != nil {
		f := fragment
		go func() {
			hctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r.onUpsert(hctx, f)
		}()
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

// GetMany fetches multiple fragments by IDs (best-effort).
// Missing fragments are skipped; returned map contains only successfully decoded items.
func (r *HorizonFragmentsRepository) GetMany(ctx context.Context, ids []string) (map[string]*models.HorizonFragment, error) {
	if r == nil || r.db == nil {
		return nil, ErrInvalidDocument
	}
	if len(ids) == 0 {
		return map[string]*models.HorizonFragment{}, nil
	}

	seen := map[string]struct{}{}
	refs := make([]*firestore.DocumentRef, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		refs = append(refs, r.col.Doc(id))
	}
	if len(refs) == 0 {
		return map[string]*models.HorizonFragment{}, nil
	}

	snaps, err := r.db.GetAll(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := map[string]*models.HorizonFragment{}
	for _, snap := range snaps {
		if snap == nil || !snap.Exists() {
			continue
		}
		var f models.HorizonFragment
		if err := snap.DataTo(&f); err != nil {
			continue
		}
		if f.ID == "" {
			f.ID = snap.Ref.ID
		}
		out[f.ID] = &f
	}
	return out, nil
}

// UpdateIndexPaths performs a lightweight update of only indexPaths (+ timestamps).
// Intended for derived/indexing changes (e.g., adding intent sphere coordinates) without bumping fragment versions.
func (r *HorizonFragmentsRepository) UpdateIndexPaths(ctx context.Context, id string, extra []string) error {
	if r == nil || id == "" {
		return ErrInvalidDocument
	}
	frag, err := r.Get(ctx, id)
	if err != nil || frag == nil {
		return err
	}
	merged := models.MergeIndexPaths(frag, extra)

	now := time.Now().UTC()
	_, err = r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "indexPaths", Value: merged},
		{Path: "updatedAt", Value: now},
		{Path: "metadata.lastUpdatedAt", Value: now},
	})
	return err
}

// FindByIdentifier finds fragments by a specific identifier key and value.
// For example: FindByIdentifier(ctx, "wikidata", "Q312") returns fragments with identifiers.wikidata == "Q312"
func (r *HorizonFragmentsRepository) FindByIdentifier(ctx context.Context, key, value string) ([]*models.HorizonFragment, error) {
	if key == "" || value == "" {
		return nil, ErrInvalidDocument
	}

	// Firestore query for nested map field: identifiers.{key} == value
	fieldPath := fmt.Sprintf("identifiers.%s", key)
	q := r.col.Where("tenantId", "==", "global").Where(fieldPath, "==", value).Limit(10)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	fragments := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}
		fragments = append(fragments, &fragment)
	}

	return fragments, nil
}

// FindByIdentifierAndType finds fragments by a specific identifier key/value and optional fragment type.
// This is safer than FindByIdentifier for dedup flows, because identifiers like shortName/legalName
// can collide across different fragment types (e.g., PROGRAM vs ORGANIZATION).
func (r *HorizonFragmentsRepository) FindByIdentifierAndType(ctx context.Context, key, value string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
	if key == "" || value == "" {
		return nil, ErrInvalidDocument
	}
	fieldPath := fmt.Sprintf("identifiers.%s", key)
	q := r.col.Where("tenantId", "==", "global").Where(fieldPath, "==", value)
	if fragType != "" {
		q = q.Where("type", "==", string(fragType))
	}
	q = q.Limit(10)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	fragments := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}
		fragments = append(fragments, &fragment)
	}
	return fragments, nil
}

// FindByLabel finds fragments by a specific label language and value, filtered by type.
// For example: FindByLabel(ctx, "en", "Wingtech Technology Co., Ltd.", "ORGANIZATION")
func (r *HorizonFragmentsRepository) FindByLabel(ctx context.Context, lang, value string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
	if lang == "" || value == "" {
		return nil, ErrInvalidDocument
	}

	// Firestore query for nested map field: labels.{lang} == value
	fieldPath := fmt.Sprintf("labels.%s", lang)
	q := r.col.Where("tenantId", "==", "global").Where(fieldPath, "==", value)
	if fragType != "" {
		q = q.Where("type", "==", string(fragType))
	}
	q = q.Limit(10)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	fragments := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}
		fragments = append(fragments, &fragment)
	}

	return fragments, nil
}

// FindByNormalizedLegalName finds organizations whose normalized legalName matches.
// Normalization removes legal suffixes (LLC, Inc, Ltd, etc.) and standardizes punctuation.
// This is a client-side filter because Firestore doesn't support computed field queries.
func (r *HorizonFragmentsRepository) FindByNormalizedLegalName(ctx context.Context, normalizedName string) ([]*models.HorizonFragment, error) {
	if normalizedName == "" {
		return nil, ErrInvalidDocument
	}

	// We can't query by normalized name directly in Firestore (no computed fields).
	// Strategy: query organizations that have legalName set, then filter client-side.
	// To limit scope, we try to match by the first word as a prefix heuristic.
	// This is imperfect but catches most cases without scanning all orgs.
	words := strings.Fields(normalizedName)
	if len(words) == 0 {
		return nil, nil
	}

	// Query orgs (limit to reasonable number for client-side filtering)
	q := r.col.Where("tenantId", "==", "global").Where("type", "==", string(models.FragmentTypeOrganization)).Limit(500)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var matches []*models.HorizonFragment
	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}
		legalName := strings.TrimSpace(fragment.Identifiers["legalName"])
		if legalName == "" {
			continue
		}
		fragNormalized := normalizeLegalNameForSearch(legalName)
		if fragNormalized == normalizedName {
			matches = append(matches, &fragment)
			if len(matches) >= 5 {
				break // Enough candidates
			}
		}
	}

	return matches, nil
}

// normalizeLegalNameForSearch is a local helper matching scout.NormalizeLegalName logic.
func normalizeLegalNameForSearch(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	// Remove common legal suffixes
	suffixes := []string{
		" jsc", " llc", " inc", " ltd", " co", " corp", " corporation",
		" group", " holdings", " limited", " company", " pty ltd",
		" sa", " ag", " se", " gmbh", " ab", " oy", " as", " spa",
		" nv", " bv", " sarl", " plc", " lp", " llp",
		" s.a.", " a.g.", " s.p.a.", " n.v.", " b.v.", " s.à r.l.", " p.l.c.",
		" co., ltd.", " co.,ltd.", " co., ltd", " co.,ltd",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	// Normalize punctuation and spacing
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, "&", "and")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	return name
}

// Update обновляет HorizonFragment и сохраняет версию
func (r *HorizonFragmentsRepository) Update(ctx context.Context, fragment *models.HorizonFragment) error {
	if fragment == nil || fragment.ID == "" {
		return ErrInvalidDocument
	}
	// Best-effort: if only canonicalPath was provided, parse it into canonicalParts for future selective fixes.
	bestEffortBackfillCanonicalPartsFromPath(fragment)
	// Best-effort: if canonicalParts were provided, assemble canonicalPath before validation.
	bestEffortAssembleCanonicalPathFromParts(fragment)
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
		log.Printf("[HorizonFragments] Failed to create version for fragment %s: %v", currentFragment.ID, err)
	}

	// Rebuild index paths to reflect any changes
	fragment.IndexPaths = models.BuildIndexPaths(fragment, nil)

	_, err = r.col.Doc(fragment.ID).Set(ctx, fragment)
	if err != nil {
		return err
	}
	tracking.RecordFragmentUpdated(ctx, fragment.ID)

	// Экспорт в GCS для поиска (с синхронизацией)
	if err := r.uploadToGCS(ctx, fragment, false); err != nil {
		log.Printf("WARNING: failed to export updated fragment to GCS: %v", err)
	}

	// Best-effort onUpsert hook (async)
	if r.onUpsert != nil {
		f := fragment
		go func() {
			hctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r.onUpsert(hctx, f)
		}()
	}

	return nil
}

// UpdateGovernance обновляет только Governance (для управления подписками)
func (r *HorizonFragmentsRepository) UpdateGovernance(ctx context.Context, id string, governance models.Governance) error {
	if id == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	docRef := r.col.Doc(id)
	// Use a transaction so we can best-effort backfill derived fields (indexPaths) on any write path.
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			return err
		}
		fragment.Governance = governance
		fragment.UpdatedAt = now
		fragment.Metadata.LastUpdatedAt = now
		// Best-effort: backfill canonicalParts from canonicalPath if missing (gradual migration).
		bestEffortBackfillCanonicalPartsFromPath(&fragment)
		// Best-effort: keep canonicalPath consistent with canonicalParts if present.
		bestEffortAssembleCanonicalPathFromParts(&fragment)
		fragment.IndexPaths = models.BuildIndexPaths(&fragment, nil)
		return tx.Set(docRef, fragment)
	})
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
		// Best-effort: backfill canonicalParts from canonicalPath if missing (gradual migration).
		bestEffortBackfillCanonicalPartsFromPath(&fragment)
		// Best-effort: keep canonicalPath consistent with canonicalParts if present.
		bestEffortAssembleCanonicalPathFromParts(&fragment)
		// Best-effort backfill: ensure indexPaths is present on any write-path.
		fragment.IndexPaths = models.BuildIndexPaths(&fragment, nil)
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
		// Best-effort: backfill canonicalParts from canonicalPath if missing (gradual migration).
		bestEffortBackfillCanonicalPartsFromPath(&fragment)
		// Best-effort: keep canonicalPath consistent with canonicalParts if present.
		bestEffortAssembleCanonicalPathFromParts(&fragment)
		// Best-effort backfill: ensure indexPaths is present on any write-path.
		fragment.IndexPaths = models.BuildIndexPaths(&fragment, nil)
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

// Query возвращает базовый Firestore query для HorizonFragment коллекции
// CollectionRef сам является Query, поэтому возвращаем его напрямую
func (r *HorizonFragmentsRepository) Query() firestore.Query {
	return r.col.Where("tenantId", "==", "global")
}

// SearchPrefix performs a fast prefix search in Firestore to support UI typeahead/autocomplete.
//
// Behavior:
// - If query starts with "/", search by canonicalPath prefix.
// - Else search by labels.en prefix.
// - Always scoped to tenantId=="global" and governance.status=="ACTIVE".
//
// Note: This is intentionally simple and predictable. It complements semantic search (Vertex).
func (r *HorizonFragmentsRepository) SearchPrefix(ctx context.Context, query string, limit int) ([]*models.HorizonFragment, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	base := r.col.
		Where("tenantId", "==", "global").
		Where("governance.status", "==", "ACTIVE")

	// Canonical path prefix (power-user)
	if strings.HasPrefix(q, "/") {
		prefix := q
		fsQ := base.
			Where("canonicalPath", ">=", prefix).
			Where("canonicalPath", "<", prefix+"\ufffd").
			OrderBy("canonicalPath", firestore.Asc).
			Limit(limit)
		docs, err := fsQ.Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		out := make([]*models.HorizonFragment, 0, len(docs))
		for _, d := range docs {
			var f models.HorizonFragment
			if err := d.DataTo(&f); err != nil {
				continue
			}
			out = append(out, &f)
		}
		return out, nil
	}

	// Label prefix (user-friendly)
	prefix := q
	fsQ := base.
		Where("labels.en", ">=", prefix).
		Where("labels.en", "<", prefix+"\ufffd").
		OrderBy("labels.en", firestore.Asc).
		Limit(limit)
	docs, err := fsQ.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, len(docs))
	for _, d := range docs {
		var f models.HorizonFragment
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
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

// ListByTypeAndTenant lists fragments by type and tenant ID.
func (r *HorizonFragmentsRepository) ListByTypeAndTenant(
	ctx context.Context,
	tenantID string,
	fragType models.FragmentType,
	limit int,
) ([]*models.HorizonFragment, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000 // Firestore limit
	}

	q := r.col.Where("tenantId", "==", tenantID).Where("type", "==", string(fragType)).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	fragments := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}
		fragments = append(fragments, &fragment)
	}

	return fragments, nil
}

// FindSupplyLinksByCustomer finds SupplyLink fragments where customerRef matches.
// This is a client-side filter because Firestore doesn't efficiently support queries on optional fields.
func (r *HorizonFragmentsRepository) FindSupplyLinksByCustomer(
	ctx context.Context,
	customerRef string,
	tenantID string,
	limit int,
) ([]*models.HorizonFragment, error) {
	if limit <= 0 {
		limit = 100
	}

	// Get all SupplyLinks for the tenant
	allSupplyLinks, err := r.ListByTypeAndTenant(ctx, tenantID, models.FragmentTypeSupplyLink, limit*2)
	if err != nil {
		return nil, err
	}

	// Filter client-side where CustomerRef matches
	var matches []*models.HorizonFragment
	for _, link := range allSupplyLinks {
		if link.CustomerRef != nil && *link.CustomerRef == customerRef {
			matches = append(matches, link)
			if len(matches) >= limit {
				break
			}
		}
	}

	return matches, nil
}

// Delete удаляет HorizonFragment, его файл из GCS и триггерит синхронизацию с Vertex AI Search
func (r *HorizonFragmentsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	// Получаем фрагмент перед удалением, чтобы удалить файл из GCS
	fragment, err := r.Get(ctx, id)
	if err == nil && fragment != nil {
		// Удаляем файл из GCS если настроено хранилище
		if r.useGCS && r.storage != nil {
			objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
			if err := r.storage.DeleteObject(ctx, r.bucket, objectPath); err != nil {
				// Логируем, но не фейлим удаление из Firestore
				log.Printf("WARNING: failed to delete fragment %s from GCS (%s): %v", id, objectPath, err)
			} else {
				log.Printf("[HorizonFragments] Deleted fragment %s from GCS: %s", id, objectPath)
			}

			// Trigger Vertex AI Search sync for deletion
			// In INCREMENTAL mode, deletion from GCS will eventually remove from index
			// But we trigger sync explicitly to speed it up
			gcsURI := fmt.Sprintf("gs://%s/%s", r.bucket, objectPath)
			if r.vertexSyncBatcher != nil {
				r.vertexSyncBatcher.Add(gcsURI)
			} else if r.vertexSearchSync != nil {
				go func(uri string) {
					syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if op, err := r.vertexSearchSync(syncCtx, uri); err == nil {
						log.Printf("[HorizonFragments] Vertex AI Search sync triggered for deletion: %s", op)
					} else {
						log.Printf("[HorizonFragments] Vertex AI Search sync failed for deletion %s: %v", uri, err)
					}
				}(gcsURI)
			}
		}
	}

	// Удаляем из Firestore
	_, err = r.col.Doc(id).Delete(ctx)
	return err
}

var ErrInvalidDocument = errors.New("invalid document")
