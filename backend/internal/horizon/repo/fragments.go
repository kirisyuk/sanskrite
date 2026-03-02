package repo

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	redis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var ErrFragmentNotFound = errors.New("horizon fragment not found")

// bestEffortAssembleCanonicalPathFromParts rebuilds fragment.CanonicalPath from CanonicalParts, if present.
// It is intentionally fail-open:
// - If parts are incomplete (or cannot assemble), it tries to re-parse from canonicalPath and self-heal.
// - This fixes zone/domain/supply-link fragments where RawPathParts was lost after Firestore deserialization.
func bestEffortAssembleCanonicalPathFromParts(fragment *models.HorizonFragment) {
	if fragment == nil || fragment.CanonicalParts == nil {
		return
	}
	parts := fragment.CanonicalParts
	built, ok := parts.BuildCanonicalPath()
	if !ok || strings.TrimSpace(built) == "" {
		// Self-heal: re-parse from canonicalPath and replace broken CanonicalParts.
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if cp != "" {
			if fresh, err := taxonomy.ParseCanonicalPathParts(cp); err == nil && fresh != nil {
				if rebuilt, ok2 := fresh.BuildCanonicalPath(); ok2 && strings.TrimSpace(rebuilt) != "" {
					fragment.CanonicalParts = fresh
					built = rebuilt
					ok = true
				}
			}
		}
		if !ok {
			fragment.CanonicalParts = nil // Clear broken parts so next cycle will backfill from path
			return
		}
	}
	// Only update when changed to avoid noisy updates/exports.
	if ok && strings.TrimSpace(fragment.CanonicalPath) != strings.TrimSpace(built) {
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

// PrepareFragmentForValidation runs the same best-effort normalization steps as Create/Update
// before validation. Used by ContractNormalizer to ensure fragments are ready for Validate().
// Safe to call from scout package.
func PrepareFragmentForValidation(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	bestEffortBackfillCanonicalPartsFromPath(fragment)
	bestEffortAssembleCanonicalPathFromParts(fragment)
	canonical.NormalizeFragmentForPersist(fragment)
	bestEffortReconcileFragmentTypeFromPath(fragment)
}

// bestEffortReconcileFragmentTypeFromPath keeps fragment.Type aligned with canonical path namespace.
// This guarantees deterministic type<->path mapping at the repository boundary.
func bestEffortReconcileFragmentTypeFromPath(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	reconciled, changed := edgesemantics.ReconcileTypeWithCanonicalPath(
		fragment.Type,
		strings.TrimSpace(fragment.CanonicalPath),
	)
	if !changed || reconciled == "" {
		return
	}
	prev := fragment.Type
	fragment.Type = reconciled
	log.Printf("[HorizonFragments] INFO: reconciled fragment type from canonicalPath: fragmentId=%q canonicalPath=%q from=%q to=%q",
		strings.TrimSpace(fragment.ID), strings.TrimSpace(fragment.CanonicalPath), prev, reconciled)
}

func canonicalPathDocID(tenantID, canonicalPath string) string {
	raw := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(canonicalPath)
	sum := sha1.Sum([]byte(raw))
	return "cp_" + hex.EncodeToString(sum[:10])
}

// HorizonFragmentsRepository репозиторий для работы с HorizonFragment
type HorizonFragmentsRepository struct {
	col           *firestore.CollectionRef
	db            *firestore.Client
	versionsRepo  *HorizonFragmentVersionsRepository // Репозиторий для версий
	edgesRepo     *DependencyEdgesRepository
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
	// vertexSearchDelete removes a single fragment document from Vertex AI Search index by document ID.
	// Used on fragment delete to avoid stale search hits.
	vertexSearchDelete func(ctx context.Context, fragmentID string) error

	redisCache *redis.Client
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

// WithEdgesRepo attaches DependencyEdgesRepository for best-effort edge cleanup on delete.
func (r *HorizonFragmentsRepository) WithEdgesRepo(edgesRepo *DependencyEdgesRepository) *HorizonFragmentsRepository {
	r.edgesRepo = edgesRepo
	return r
}

// WithRedis sets a shared Redis client for L1 fragment caching (GetByCanonicalPath).
func (r *HorizonFragmentsRepository) WithRedis(c *redis.Client) *HorizonFragmentsRepository {
	r.redisCache = c
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

// WithVertexSearchDelete configures immediate delete from Vertex AI Search index on fragment deletion.
func (r *HorizonFragmentsRepository) WithVertexSearchDelete(deleteFn func(ctx context.Context, fragmentID string) error) *HorizonFragmentsRepository {
	r.vertexSearchDelete = deleteFn
	return r
}

// WithOnUpsert registers a best-effort callback called after Create/Update succeeds.
// Intended for side-effects like linking/enriching Entities.
func (r *HorizonFragmentsRepository) WithOnUpsert(fn func(ctx context.Context, fragment *models.HorizonFragment)) *HorizonFragmentsRepository {
	r.onUpsert = fn
	return r
}

func (r *HorizonFragmentsRepository) runOnUpsertAsync(fragment *models.HorizonFragment) {
	if r == nil || r.onUpsert == nil || fragment == nil {
		return
	}
	f := fragment
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[HorizonFragments] ERROR: onUpsert hook panic recovered: fragmentId=%s canonicalPath=%s panic=%v stack=%s",
					strings.TrimSpace(f.ID), strings.TrimSpace(f.CanonicalPath), rec, string(debug.Stack()))
			}
		}()
		hctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r.onUpsert(hctx, f)
	}()
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
			"tenantId":          fragment.TenantID,
			"type":              "horizon_fragment",
			"fragmentType":      fragment.Type,
			"canonicalPath":     fragment.CanonicalPath,
			"displayName":       fragmentDisplayName(fragment),
			"labelEn":           strings.TrimSpace(fragment.Labels["en"]),
			"aliases":           fragmentAliasesForStructData(fragment, 12),
			"legalName":         firstNonEmptyIdentifier(fragment.Identifiers, "legalName"),
			"shortName":         firstNonEmptyIdentifier(fragment.Identifiers, "shortName"),
			"identifiers":       compactIdentifiers(fragment.Identifiers, 24),
			"entityPath":        fragment.EntityPath,
			"sectorPath":        fragment.SectorPath,
			"spherePaths":       nonEmptyStrings(fragment.SpherePaths),
			"indexPaths":        fragment.IndexPaths,
			"parentPath":        strings.TrimSpace(fragment.ParentPath),
			"parentGovPath":     strings.TrimSpace(fragment.ParentGovPath),
			"parentMarketPath":  strings.TrimSpace(fragment.ParentMarketPath),
			"affiliatedOrgPath": strings.TrimSpace(fragment.AffiliatedOrgPath),
			"neighborhoodPaths": fragmentNeighborhoodPaths(fragment),
			"marketDomain":      strings.TrimSpace(fragment.MarketDomain),
			"marketVertical":    strings.TrimSpace(fragment.MarketVertical),
			"govType":           strings.TrimSpace(fragment.GovType),
			"communityType":     strings.TrimSpace(fragment.CommunityType),
			"radiusKm":          fragment.RadiusKm,
			"coordinates":       coordinatesToStructData(fragment.Coordinates),
			"homeLocation":      locationToStructData(fragment.HomeLocation),
			"horizonMonitor":    horizonMonitorToStructData(fragment.EffectiveHorizonMonitor()),
			"track":             trackToStructData(fragment.Track),
			"status":            fragment.Governance.Status,
			"updatedAt":         fragment.UpdatedAt.Format(time.RFC3339),
			"createdAt":         fragment.CreatedAt.Format(time.RFC3339),
			"uri":               gcsURI,
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
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("[HorizonFragments] ERROR: vertexSearchSync panic recovered for uri=%s panic=%v stack=%s", gcsURI, rec, string(debug.Stack()))
					}
				}()
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

func fragmentDisplayName(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
		return v
	}
	for _, v := range fragment.Labels {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	if v := firstNonEmptyIdentifier(fragment.Identifiers, "legalName"); v != "" {
		return v
	}
	if v := firstNonEmptyIdentifier(fragment.Identifiers, "shortName"); v != "" {
		return v
	}
	cp := strings.Trim(strings.TrimSpace(fragment.CanonicalPath), "/")
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func firstNonEmptyIdentifier(ids map[string]string, key string) string {
	if len(ids) == 0 {
		return ""
	}
	return strings.TrimSpace(ids[key])
}

func compactIdentifiers(ids map[string]string, limit int) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 24
	}
	keys := make([]string, 0, len(ids))
	for k, v := range ids {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = strings.TrimSpace(ids[k])
	}
	return out
}

func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fragmentAliasesForStructData(fragment *models.HorizonFragment, limit int) []string {
	if fragment == nil {
		return nil
	}
	if limit <= 0 {
		limit = 12
	}
	primary := strings.TrimSpace(fragment.Labels["en"])
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || v == primary {
			return
		}
		seen[v] = struct{}{}
	}
	for _, v := range fragment.Labels {
		add(v)
	}
	add(firstNonEmptyIdentifier(fragment.Identifiers, "legalName"))
	add(firstNonEmptyIdentifier(fragment.Identifiers, "shortName"))
	for _, entry := range fragment.NameHistory {
		add(entry.Name)
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func fragmentNeighborhoodPaths(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	set := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		set[v] = struct{}{}
	}
	add(fragment.ParentPath)
	add(fragment.ParentGovPath)
	add(fragment.ParentMarketPath)
	add(fragment.AffiliatedOrgPath)
	if fragment.SupplierRef != nil {
		add(*fragment.SupplierRef)
	}
	if fragment.CustomerRef != nil {
		add(*fragment.CustomerRef)
	}
	if fragment.ProductRef != nil {
		add(*fragment.ProductRef)
	}
	if fragment.SupplierFacilityRef != nil {
		add(*fragment.SupplierFacilityRef)
	}
	if fragment.CustomerFacilityRef != nil {
		add(*fragment.CustomerFacilityRef)
	}
	for _, v := range fragment.TransportViaRefs {
		add(v)
	}
	for _, v := range fragment.CommunityPlatforms {
		add(v)
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func coordinatesToStructData(c *models.Coordinates) map[string]interface{} {
	if c == nil || !models.IsValidCoordinates(c.Lat, c.Lon) {
		return nil
	}
	return map[string]interface{}{
		"lat": c.Lat,
		"lon": c.Lon,
	}
}

func locationToStructData(loc *models.Location) map[string]interface{} {
	if loc == nil {
		return nil
	}
	out := map[string]interface{}{}
	if v := strings.TrimSpace(loc.Name); v != "" {
		out["name"] = v
	}
	if loc.ISO3166_1 != nil && strings.TrimSpace(*loc.ISO3166_1) != "" {
		out["iso3166_1"] = strings.TrimSpace(*loc.ISO3166_1)
	}
	if loc.ISO3166_2 != nil && strings.TrimSpace(*loc.ISO3166_2) != "" {
		out["iso3166_2"] = strings.TrimSpace(*loc.ISO3166_2)
	}
	if loc.ISO6709 != nil && strings.TrimSpace(*loc.ISO6709) != "" {
		out["iso6709"] = strings.TrimSpace(*loc.ISO6709)
		if lat, lon, ok := models.ParseISO6709Coordinates(*loc.ISO6709); ok {
			out["lat"] = lat
			out["lon"] = lon
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func horizonMonitorToStructData(wm *models.HorizonMonitorFragmentData) map[string]interface{} {
	if wm == nil {
		return nil
	}
	out := map[string]interface{}{}
	if wm.ObjectKind != "" {
		out["objectKind"] = string(wm.ObjectKind)
	}
	if v := strings.TrimSpace(wm.SourceObjectID); v != "" {
		out["sourceObjectId"] = v
	}
	if wm.Coordinates != nil {
		out["coordinates"] = map[string]interface{}{
			"lat": wm.Coordinates.Lat,
			"lon": wm.Coordinates.Lon,
		}
	}
	if len(wm.Countries) > 0 {
		out["countries"] = wm.Countries
	}
	if len(wm.Parties) > 0 {
		out["parties"] = wm.Parties
	}
	if v := strings.TrimSpace(wm.Status); v != "" {
		out["status"] = v
	}
	if v := strings.TrimSpace(wm.Operator); v != "" {
		out["operator"] = v
	}
	if v := strings.TrimSpace(wm.FacilityType); v != "" {
		out["facilityType"] = v
	}
	if v := strings.TrimSpace(wm.MarketType); v != "" {
		out["marketType"] = v
	}
	if v := strings.TrimSpace(wm.PipelineType); v != "" {
		out["pipelineType"] = v
	}
	if v := strings.TrimSpace(wm.PortType); v != "" {
		out["portType"] = v
	}
	if v := strings.TrimSpace(wm.Mineral); v != "" {
		out["mineral"] = v
	}
	if v := strings.TrimSpace(wm.Capacity); v != "" {
		out["capacity"] = v
	}
	if wm.CapacityTbps != nil {
		out["capacityTbps"] = *wm.CapacityTbps
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trackToStructData(tr *models.TrackFragmentData) map[string]interface{} {
	if tr == nil {
		return nil
	}
	out := map[string]interface{}{
		"lat":        tr.Lat,
		"lon":        tr.Lon,
		"lastSeenAt": tr.LastSeenAt.Format(time.RFC3339),
	}
	if tr.Kind != "" {
		out["kind"] = string(tr.Kind)
	}
	if v := strings.TrimSpace(tr.ObjectType); v != "" {
		out["objectType"] = v
	}
	if v := strings.TrimSpace(tr.ExternalTrackID); v != "" {
		out["externalTrackId"] = v
	}
	if v := strings.TrimSpace(tr.SourceID); v != "" {
		out["sourceId"] = v
	}
	if tr.HeadingDeg != nil {
		out["headingDeg"] = *tr.HeadingDeg
	}
	if tr.SpeedKts != nil {
		out["speedKts"] = *tr.SpeedKts
	}
	if tr.AltitudeM != nil {
		out["altitudeM"] = *tr.AltitudeM
	}
	if tr.DepthM != nil {
		out["depthM"] = *tr.DepthM
	}
	if tr.Confidence != 0 {
		out["confidence"] = tr.Confidence
	}
	if v := strings.TrimSpace(tr.GeoHash); v != "" {
		out["geoHash"] = v
	}
	return out
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
	// Best-effort: shared canonical-path normalization at repository boundary.
	canonical.NormalizeFragmentForPersist(fragment)
	// Keep fragment type deterministic with canonicalPath namespace.
	bestEffortReconcileFragmentTypeFromPath(fragment)
	if err := fragment.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if cp != "" && !strings.HasPrefix(cp, "/supply-link/") {
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			return "", fmt.Errorf("canonical path validation error: %w", err)
		}
	}
	if cp != "" {
		// Best-effort de-dup at repository boundary:
		// if canonicalPath already exists, reuse existing fragment ID instead of creating a duplicate.
		if existing, err := r.GetByCanonicalPath(ctx, cp); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
			log.Printf("[HorizonFragments] Reusing existing fragment for canonicalPath=%q id=%s", cp, strings.TrimSpace(existing.ID))
			return strings.TrimSpace(existing.ID), nil
		}
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
	}
	if fragment.Metadata.LastUpdatedAt.IsZero() {
		fragment.Metadata.LastUpdatedAt = now
	}
	// New fragments should not look "freshly verified" unless verification really happened.
	if fragment.Metadata.EpistemicStatus == "" {
		fragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
	}

	doc := r.col.NewDoc()
	if cp != "" {
		tenantKey := strings.TrimSpace(fragment.TenantID)
		if fragment.Type != models.FragmentTypeSupplyLink {
			tenantKey = "global"
		}
		if tenantKey == "" {
			tenantKey = "global"
		}
		doc = r.col.Doc(canonicalPathDocID(tenantKey, cp))
	}
	fragment.ID = doc.ID

	// Build index paths for search/filtering
	fragment.IndexPaths = models.BuildIndexPaths(fragment, nil)

	if cp != "" {
		if _, err := doc.Create(ctx, fragment); err != nil {
			if status.Code(err) == codes.AlreadyExists {
				if existing, gerr := r.GetByCanonicalPath(ctx, cp); gerr == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
					log.Printf("[HorizonFragments] Reusing existing fragment after collision for canonicalPath=%q id=%s", cp, strings.TrimSpace(existing.ID))
					return strings.TrimSpace(existing.ID), nil
				}
				if existing, gerr := r.Get(ctx, doc.ID); gerr == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
					log.Printf("[HorizonFragments] Reusing deterministic fragment after collision for canonicalPath=%q id=%s", cp, strings.TrimSpace(existing.ID))
					return strings.TrimSpace(existing.ID), nil
				}
			}
			return "", err
		}
	} else {
		if _, err := doc.Set(ctx, fragment); err != nil {
			return "", err
		}
	}
	tracking.RecordFragmentCreated(ctx, fragment.ID)
	r.invalidateFragmentRedis(ctx, cp)

	// Экспорт в GCS для поиска (с синхронизацией)
	if err := r.uploadToGCS(ctx, fragment, false); err != nil {
		// Логируем, но не фейлим создание
		log.Printf("WARNING: failed to export fragment to GCS: %v", err)
	}

	// Best-effort onUpsert hook (async, panic-safe)
	r.runOnUpsertAsync(fragment)

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
	cp := strings.TrimSpace(canonicalPath)
	if frag := r.getFragmentFromRedis(ctx, cp); frag != nil {
		return frag, nil
	}
	q := r.col.Where("tenantId", "==", "global").Where("canonicalPath", "==", cp)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrFragmentNotFound
	}
	preferredID := canonicalPathDocID("global", cp)
	var best *models.HorizonFragment
	bestRank := 3
	bestID := ""
	for _, doc := range docs {
		if doc == nil || !doc.Exists() {
			continue
		}
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			return nil, err
		}
		if strings.TrimSpace(fragment.ID) == "" {
			fragment.ID = strings.TrimSpace(doc.Ref.ID)
		}
		rank := 2
		if strings.TrimSpace(doc.Ref.ID) == preferredID {
			rank = 0
		} else if !fragment.CreatedAt.IsZero() {
			rank = 1
		}
		if best == nil ||
			rank < bestRank ||
			(rank == bestRank && !fragment.CreatedAt.IsZero() && (best.CreatedAt.IsZero() || fragment.CreatedAt.Before(best.CreatedAt))) ||
			(rank == bestRank && fragment.CreatedAt.Equal(best.CreatedAt) && strings.TrimSpace(doc.Ref.ID) < bestID) {
			frag := fragment
			best = &frag
			bestRank = rank
			bestID = strings.TrimSpace(doc.Ref.ID)
		}
	}
	if best == nil {
		return nil, ErrFragmentNotFound
	}
	if len(docs) > 1 {
		log.Printf("[HorizonFragments] WARNING: duplicate canonicalPath detected canonicalPath=%q matches=%d selectedId=%s", cp, len(docs), strings.TrimSpace(best.ID))
	}
	r.setFragmentInRedis(ctx, cp, best)
	return best, nil
}

// ListByCanonicalPathPrefix returns fragments whose canonicalPath starts with prefix.
// Used for abbreviation resolution: list fragments in same geo to find abbrev/full pairs.
func (r *HorizonFragmentsRepository) ListByCanonicalPathPrefix(ctx context.Context, prefix string, limit int) ([]*models.HorizonFragment, error) {
	if r == nil || r.col == nil || prefix == "" {
		return nil, nil
	}
	p := strings.TrimSpace(prefix)
	if !strings.HasSuffix(p, "/") {
		p = p + "/"
	}
	if limit <= 0 {
		limit = 300
	}
	if limit > 500 {
		limit = 500
	}
	q := r.col.
		Where("tenantId", "==", "global").
		Where("canonicalPath", ">=", p).
		Where("canonicalPath", "<", p+"\ufffd").
		OrderBy("canonicalPath", firestore.Asc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || !doc.Exists() {
			continue
		}
		var f models.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		if strings.TrimSpace(f.ID) == "" {
			f.ID = strings.TrimSpace(doc.Ref.ID)
		}
		out = append(out, &f)
	}
	return out, nil
}

// FindByTags returns fragments that match any of the provided tags (best-effort).
// Note: uses array-contains-any and is capped by limit.
func (r *HorizonFragmentsRepository) FindByTags(ctx context.Context, tags []string, limit int) ([]*models.HorizonFragment, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if len(tags) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	q := r.col.Where("tags", "array-contains-any", tags).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}
	out := make([]*models.HorizonFragment, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || !doc.Exists() {
			continue
		}
		var f models.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		if f.ID == "" {
			f.ID = doc.Ref.ID
		}
		out = append(out, &f)
	}
	return out, nil
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
		fragNormalized := naming.NormalizeLegalName(legalName)
		if fragNormalized == normalizedName {
			matches = append(matches, &fragment)
			if len(matches) >= 5 {
				break // Enough candidates
			}
		}
	}

	return matches, nil
}

// UpdateOptions configures optional behavior for fragment updates.
type UpdateOptions struct {
	// SkipVersionSnapshot disables pre-update version snapshot creation.
	// Useful for high-frequency metric-only updates (e.g. propagation).
	SkipVersionSnapshot bool
	// SkipIndexExport disables GCS/Vertex export for this update.
	// Useful for internal state updates that do not affect search indexing.
	SkipIndexExport bool
}

// Update обновляет HorizonFragment и сохраняет версию.
func (r *HorizonFragmentsRepository) Update(ctx context.Context, fragment *models.HorizonFragment) error {
	return r.updateWithOptions(ctx, fragment, UpdateOptions{})
}

// UpdateFast applies a lightweight fragment update optimized for high-frequency state changes.
// It preserves validation and hooks but skips version snapshot + search export.
func (r *HorizonFragmentsRepository) UpdateFast(ctx context.Context, fragment *models.HorizonFragment) error {
	return r.updateWithOptions(ctx, fragment, UpdateOptions{
		SkipVersionSnapshot: true,
		SkipIndexExport:     true,
	})
}

func (r *HorizonFragmentsRepository) updateWithOptions(ctx context.Context, fragment *models.HorizonFragment, opts UpdateOptions) error {
	if fragment == nil || fragment.ID == "" {
		return ErrInvalidDocument
	}
	// Best-effort: if only canonicalPath was provided, parse it into canonicalParts for future selective fixes.
	bestEffortBackfillCanonicalPartsFromPath(fragment)
	// Best-effort: if canonicalParts were provided, assemble canonicalPath before validation.
	bestEffortAssembleCanonicalPathFromParts(fragment)
	// Best-effort: shared canonical-path normalization at repository boundary.
	canonical.NormalizeFragmentForPersist(fragment)
	// Keep fragment type deterministic with canonicalPath namespace.
	bestEffortReconcileFragmentTypeFromPath(fragment)
	if err := fragment.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	fragment.UpdatedAt = now
	if fragment.Version < 0 {
		fragment.Version = 0
	}
	fragment.Version++
	fragment.Metadata.LastUpdatedAt = now

	if !opts.SkipVersionSnapshot {
		// Получаем текущую версию фрагмента для сохранения
		currentFragment, err := r.Get(ctx, fragment.ID)
		if err != nil {
			return fmt.Errorf("failed to get current fragment: %w", err)
		}

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
	}

	// Rebuild index paths to reflect any changes
	fragment.IndexPaths = models.BuildIndexPaths(fragment, nil)

	_, err := r.col.Doc(fragment.ID).Set(ctx, fragment)
	if err != nil {
		return err
	}
	tracking.RecordFragmentUpdated(ctx, fragment.ID)
	r.invalidateFragmentRedis(ctx, strings.TrimSpace(fragment.CanonicalPath))

	// Экспорт в GCS для поиска (с синхронизацией)
	if !opts.SkipIndexExport {
		if err := r.uploadToGCS(ctx, fragment, false); err != nil {
			log.Printf("WARNING: failed to export updated fragment to GCS: %v", err)
		}
	}

	// Best-effort onUpsert hook (async, panic-safe)
	r.runOnUpsertAsync(fragment)

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
		// Best-effort: shared canonical-path normalization at repository boundary.
		canonical.NormalizeFragmentForPersist(&fragment)
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
		// Best-effort: shared canonical-path normalization at repository boundary.
		canonical.NormalizeFragmentForPersist(&fragment)
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
		// Best-effort: shared canonical-path normalization at repository boundary.
		canonical.NormalizeFragmentForPersist(&fragment)
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

// GeoListFilter holds optional filters for geo-object listing.
type GeoListFilter struct {
	Types         []models.FragmentType
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

func (f *GeoListFilter) matchesType(t models.FragmentType) bool {
	if f == nil || len(f.Types) == 0 {
		return true
	}
	for _, ft := range f.Types {
		if ft == t {
			return true
		}
	}
	return false
}

func (f *GeoListFilter) matchesCreatedAt(t time.Time) bool {
	if f == nil {
		return true
	}
	if f.CreatedAfter != nil && t.Before(*f.CreatedAfter) {
		return false
	}
	if f.CreatedBefore != nil && t.After(*f.CreatedBefore) {
		return false
	}
	return true
}

// ListGeoBound returns global fragments with resolved valid coordinates for map rendering.
// Coordinates are resolved in best-effort order:
//  1. fragment.coordinates
//  2. fragment.track
//  3. fragment.horizonMonitor/worldMonitor coordinates
//  4. homeLocation.iso6709
//
// If pathPrefix is not empty, only canonical paths with that prefix are scanned.
// If filter is non-nil, results are narrowed by fragment type and/or creation date range.
func (r *HorizonFragmentsRepository) ListGeoBound(
	ctx context.Context,
	limit int,
	pathPrefix string,
	filter *GeoListFilter,
) ([]*models.HorizonFragment, error) {
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	scanLimit := limit * 4
	if scanLimit < limit+200 {
		scanLimit = limit + 200
	}
	if scanLimit > 20000 {
		scanLimit = 20000
	}

	prefix := strings.TrimSpace(pathPrefix)
	var fsQ firestore.Query
	if prefix != "" {
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		fsQ = r.col.
			Where("tenantId", "==", "global").
			Where("canonicalPath", ">=", prefix).
			Where("canonicalPath", "<", prefix+"\ufffd").
			OrderBy("canonicalPath", firestore.Asc).
			Limit(scanLimit)
	} else {
		fsQ = r.col.
			Where("tenantId", "==", "global").
			Where("governance.status", "==", "ACTIVE").
			OrderBy("updatedAt", firestore.Desc).
			Limit(scanLimit)
	}

	docs, err := fsQ.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	capHint := len(docs)
	if capHint > limit {
		capHint = limit
	}
	out := make([]*models.HorizonFragment, 0, capHint)
	seen := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		var fragment models.HorizonFragment
		if err := d.DataTo(&fragment); err != nil {
			continue
		}
		if fragment.ID == "" {
			continue
		}
		if _, exists := seen[fragment.ID]; exists {
			continue
		}
		if !filter.matchesType(fragment.Type) {
			continue
		}
		if !filter.matchesCreatedAt(fragment.CreatedAt) {
			continue
		}
		if lat, lon, ok := resolveFragmentCoordinates(&fragment); ok {
			fragment.Coordinates = &models.Coordinates{Lat: lat, Lon: lon}
		} else {
			continue
		}
		seen[fragment.ID] = struct{}{}
		out = append(out, &fragment)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func resolveFragmentCoordinates(fragment *models.HorizonFragment) (float64, float64, bool) {
	if fragment == nil {
		return 0, 0, false
	}
	if fragment.Coordinates != nil && models.IsValidCoordinates(fragment.Coordinates.Lat, fragment.Coordinates.Lon) {
		return fragment.Coordinates.Lat, fragment.Coordinates.Lon, true
	}
	if fragment.Track != nil && models.IsValidCoordinates(fragment.Track.Lat, fragment.Track.Lon) {
		return fragment.Track.Lat, fragment.Track.Lon, true
	}
	if wm := fragment.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil &&
		models.IsValidCoordinates(wm.Coordinates.Lat, wm.Coordinates.Lon) {
		return wm.Coordinates.Lat, wm.Coordinates.Lon, true
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO6709 != nil {
		if lat, lon, ok := models.ParseISO6709Coordinates(*fragment.HomeLocation.ISO6709); ok {
			return lat, lon, true
		}
	}
	return 0, 0, false
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
		// Delete from Vertex index first to avoid stale search hits.
		// If index deletion fails, abort delete to prevent Firestore/Vertex divergence.
		if r.vertexSearchDelete != nil {
			fragmentID := strings.TrimSpace(fragment.ID)
			if fragmentID == "" {
				fragmentID = strings.TrimSpace(id)
			}
			deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			vErr := r.vertexSearchDelete(deleteCtx, fragmentID)
			cancel()
			if vErr != nil {
				return fmt.Errorf("failed to delete fragment %s from vertex index: %w", fragmentID, vErr)
			}
			log.Printf("[HorizonFragments] Deleted fragment %s from Vertex index", fragmentID)
		}

		// Best-effort: delete dependency edges referencing this fragment.
		if r.edgesRepo != nil {
			tenantID := strings.TrimSpace(fragment.TenantID)
			if tenantID == "" {
				tenantID = "global"
			}
			edgesByID := map[string]struct{}{}
			srcEdges, srcErr := r.edgesRepo.ListBySource(ctx, tenantID, fragment.ID, nil, 5000)
			if srcErr != nil {
				log.Printf("[HorizonFragments] WARNING: failed to list source edges for fragment %s: %v", id, srcErr)
			} else {
				for _, e := range srcEdges {
					if e != nil && strings.TrimSpace(e.ID) != "" {
						edgesByID[e.ID] = struct{}{}
					}
				}
			}
			tgtEdges, tgtErr := r.edgesRepo.ListByTarget(ctx, tenantID, fragment.ID, nil, 5000)
			if tgtErr != nil {
				log.Printf("[HorizonFragments] WARNING: failed to list target edges for fragment %s: %v", id, tgtErr)
			} else {
				for _, e := range tgtEdges {
					if e != nil && strings.TrimSpace(e.ID) != "" {
						edgesByID[e.ID] = struct{}{}
					}
				}
			}
			deleted := 0
			for edgeID := range edgesByID {
				if err := r.edgesRepo.Delete(ctx, edgeID); err != nil {
					log.Printf("[HorizonFragments] WARNING: failed to delete edge %s for fragment %s: %v", edgeID, id, err)
					continue
				}
				deleted++
			}
			if deleted > 0 {
				log.Printf("[HorizonFragments] Deleted %d edges for fragment %s", deleted, id)
			}
		}

		// Удаляем файл из GCS если настроено хранилище
		if r.useGCS && r.storage != nil {
			objectPath := fmt.Sprintf("%s/horizon_fragments/%s.json", fragment.TenantID, fragment.ID)
			if err := r.storage.DeleteObject(ctx, r.bucket, objectPath); err != nil {
				// Логируем, но не фейлим удаление из Firestore
				log.Printf("WARNING: failed to delete fragment %s from GCS (%s): %v", id, objectPath, err)
			} else {
				log.Printf("[HorizonFragments] Deleted fragment %s from GCS: %s", id, objectPath)
			}

			// NOTE: Do NOT add the deleted file's URI to vertexSyncBatcher.
			// ImportDocuments expects files to exist in GCS; passing a deleted file URI causes
			// INVALID_RESOURCE_ID ("Given gcs input path matched no files").
		}
	}

	// Удаляем из Firestore
	_, err = r.col.Doc(id).Delete(ctx)
	if err == nil && fragment != nil {
		r.invalidateFragmentRedis(ctx, strings.TrimSpace(fragment.CanonicalPath))
	}
	return err
}

var ErrInvalidDocument = errors.New("invalid document")

const fragRedisPrefix = "h:frag:cp:"
const fragRedisTTL = 5 * time.Minute
const fragRedisTimeout = 70 * time.Millisecond

func fragRedisKey(canonicalPath string) string {
	sum := sha1.Sum([]byte(canonicalPath))
	return fragRedisPrefix + hex.EncodeToString(sum[:])
}

func (r *HorizonFragmentsRepository) getFragmentFromRedis(ctx context.Context, canonicalPath string) *models.HorizonFragment {
	if r == nil || r.redisCache == nil || canonicalPath == "" {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, fragRedisTimeout)
	defer cancel()
	val, err := r.redisCache.Get(rctx, fragRedisKey(canonicalPath)).Bytes()
	if err != nil || len(val) == 0 {
		return nil
	}
	var frag models.HorizonFragment
	if err := json.Unmarshal(val, &frag); err != nil {
		return nil
	}
	return &frag
}

func (r *HorizonFragmentsRepository) setFragmentInRedis(ctx context.Context, canonicalPath string, frag *models.HorizonFragment) {
	if r == nil || r.redisCache == nil || canonicalPath == "" || frag == nil {
		return
	}
	b, err := json.Marshal(frag)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, fragRedisTimeout)
	defer cancel()
	_ = r.redisCache.Set(rctx, fragRedisKey(canonicalPath), b, fragRedisTTL).Err()
}

func (r *HorizonFragmentsRepository) invalidateFragmentRedis(ctx context.Context, canonicalPath string) {
	if r == nil || r.redisCache == nil || canonicalPath == "" {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, fragRedisTimeout)
	defer cancel()
	_ = r.redisCache.Del(rctx, fragRedisKey(canonicalPath)).Err()
}

// FindByAlternativePath finds fragments that have the given path in their AlternativePaths array.
// Uses Firestore array-contains for efficient lookup.
func (r *HorizonFragmentsRepository) FindByAlternativePath(ctx context.Context, altPath string) (*models.HorizonFragment, error) {
	if altPath == "" {
		return nil, nil
	}
	docs, err := r.col.Where("alternativePaths", "array-contains", altPath).Limit(1).Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var frag models.HorizonFragment
	if err := docs[0].DataTo(&frag); err != nil {
		return nil, err
	}
	return &frag, nil
}

// AppendDedupAliases appends alternative names and paths to an existing fragment.
// Uses Firestore ArrayUnion to atomically add without duplicates.
// This is a best-effort operation; errors are logged but not propagated.
func (r *HorizonFragmentsRepository) AppendDedupAliases(ctx context.Context, fragmentID string, altNames []string, altPaths []string) {
	if r == nil || fragmentID == "" {
		return
	}
	if len(altNames) == 0 && len(altPaths) == 0 {
		return
	}
	var updates []firestore.Update
	if len(altNames) > 0 {
		ifaces := make([]interface{}, len(altNames))
		for i, n := range altNames {
			ifaces[i] = n
		}
		updates = append(updates, firestore.Update{
			Path:  "alternativeNames",
			Value: firestore.ArrayUnion(ifaces...),
		})
	}
	if len(altPaths) > 0 {
		ifaces := make([]interface{}, len(altPaths))
		for i, p := range altPaths {
			ifaces[i] = p
		}
		updates = append(updates, firestore.Update{
			Path:  "alternativePaths",
			Value: firestore.ArrayUnion(ifaces...),
		})
	}
	if _, err := r.col.Doc(fragmentID).Update(ctx, updates); err != nil {
		fmt.Printf("[FragmentsRepo] AppendDedupAliases warn: id=%s err=%v\n", fragmentID, err)
	}
}
