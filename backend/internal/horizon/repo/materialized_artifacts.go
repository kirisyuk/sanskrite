package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

var ErrMaterializedArtifactNotFound = errors.New("materialized artifact not found")

// MaterializedArtifactsRepository репозиторий для работы с MaterializedArtifact
type MaterializedArtifactsRepository struct {
	col               *firestore.CollectionRef
	db                *firestore.Client
	storage           *gcp.GCSClient
	bucket            string
	useGCS            bool
	uploadLimiter     *rate.Limiter
	vertexSyncBatcher *VertexSyncBatcher
	productDictionary *productdict.Runtime
}

// NewMaterializedArtifactsRepository создает новый репозиторий
func NewMaterializedArtifactsRepository(db *firestore.Client) *MaterializedArtifactsRepository {
	return &MaterializedArtifactsRepository{
		col: db.Collection("horizon_materialized_artifacts"),
		db:  db,
	}
}

// WithStorage configures GCS storage for exporting materialized artifacts.
func (r *MaterializedArtifactsRepository) WithStorage(storage *gcp.GCSClient, bucket string) *MaterializedArtifactsRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && bucket != ""
	if r.useGCS {
		r.uploadLimiter = rate.NewLimiter(rate.Limit(10), 10)
	}
	return r
}

// WithVertexSyncBatcher configures batched Vertex AI Search sync for materialized artifacts.
func (r *MaterializedArtifactsRepository) WithVertexSyncBatcher(batcher *VertexSyncBatcher) *MaterializedArtifactsRepository {
	r.vertexSyncBatcher = batcher
	return r
}

func (r *MaterializedArtifactsRepository) WithProductDictionary(dict *productdict.Runtime) *MaterializedArtifactsRepository {
	r.productDictionary = dict
	return r
}

// uploadToGCS exports the artifact to GCS for Vertex AI Search indexing.
func (r *MaterializedArtifactsRepository) uploadToGCS(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) error {
	if !r.useGCS || artifact == nil {
		return nil
	}
	if r.uploadLimiter != nil {
		if err := r.uploadLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	content := artifact.GenerateVectorText()

	objectPath := fmt.Sprintf("%s/supply_chain_artifacts/%s.json", artifact.TenantID, artifact.ID)
	gcsURI := fmt.Sprintf("gs://%s/%s", r.bucket, objectPath)

	doc := map[string]interface{}{
		"id": artifact.ID,
		"content": map[string]interface{}{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]interface{}{
			"tenantId":         artifact.TenantID,
			"type":             "supply_chain",
			"kind":             string(artifact.Kind),
			"displayName":      artifact.DisplayName,
			"canonicalKey":     artifact.CanonicalKey,
			"labels":           artifact.Labels,
			"nodeRefs":         artifact.NodeRefs,
			"geoRefs":          artifact.GeoRefs,
			"flowRefs":         artifact.FlowRefs,
			"zoneRefs":         artifact.ZoneRefs,
			"affectedZoneRefs": artifact.AffectedZoneRefs,
			"pathStatus":       artifact.PathStatus,
			"status":           artifact.Status,
			"updatedAt":        artifact.UpdatedAt.Format(time.RFC3339),
			"uri":              gcsURI,
		},
	}

	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal artifact to json: %w", err)
	}

	metadata := map[string]string{
		"type":     "supply_chain",
		"tenantId": artifact.TenantID,
	}

	log.Printf("[MaterializedArtifacts] Exporting artifact %s (%s) to GCS for Vertex Indexing...", artifact.ID, artifact.CanonicalKey)
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, jsonData, "application/json", metadata); err != nil {
		return fmt.Errorf("failed to upload artifact to GCS: %w", err)
	}

	if r.vertexSyncBatcher != nil {
		r.vertexSyncBatcher.Add(gcsURI)
	}

	return nil
}

// Create создает новый MaterializedArtifact
func (r *MaterializedArtifactsRepository) Create(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) (string, error) {
	if artifact == nil {
		return "", fmt.Errorf("artifact is nil")
	}
	if r.productDictionary != nil {
		if _, err := r.productDictionary.NormalizeArtifact(ctx, artifact); err != nil {
			log.Printf("[MaterializedArtifacts] WARNING: product dictionary normalization failed on create artifact=%s key=%s err=%v", strings.TrimSpace(artifact.ID), strings.TrimSpace(artifact.CanonicalKey), err)
		}
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)

	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	artifact.UpdatedAt = now
	if artifact.Status == "" {
		artifact.Status = "active"
	}
	if artifact.CreatedBy == "" {
		artifact.CreatedBy = "system"
	}
	if artifact.UpdatedBy == "" {
		artifact.UpdatedBy = "system"
	}

	doc := r.col.NewDoc()
	artifact.ID = doc.ID

	if _, err := doc.Set(ctx, artifact); err != nil {
		return "", fmt.Errorf("firestore set failed: %w", err)
	}

	if r.useGCS {
		if err := r.uploadToGCS(ctx, artifact); err != nil {
			log.Printf("[MaterializedArtifacts] GCS upload failed for %s: %v", artifact.ID, err)
		}
	}

	return artifact.ID, nil
}

// Get получает MaterializedArtifact по ID
func (r *MaterializedArtifactsRepository) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") {
			return nil, ErrMaterializedArtifactNotFound
		}
		return nil, fmt.Errorf("firestore get failed: %w", err)
	}

	var artifact horizonmodels.MaterializedArtifact
	if err := snap.DataTo(&artifact); err != nil {
		return nil, fmt.Errorf("data unmarshal failed: %w", err)
	}

	return &artifact, nil
}

// GetByCanonicalKey получает MaterializedArtifact по TenantID + Kind + CanonicalKey.
// This is the primary idempotency key for overlays.
func (r *MaterializedArtifactsRepository) GetByCanonicalKey(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, canonicalKey string) (*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if !kind.Valid() {
		return nil, fmt.Errorf("kind is required")
	}
	if canonicalKey == "" {
		return nil, fmt.Errorf("canonicalKey is required")
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", string(kind)).
		Where("canonicalKey", "==", canonicalKey).
		Limit(1)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	if len(docs) == 0 {
		return nil, ErrMaterializedArtifactNotFound
	}

	var artifact horizonmodels.MaterializedArtifact
	if err := docs[0].DataTo(&artifact); err != nil {
		return nil, fmt.Errorf("data unmarshal failed: %w", err)
	}

	return &artifact, nil
}

// GetByCanonicalKeyAnyKind looks up a MaterializedArtifact by canonical key without
// requiring a specific kind. Used when the artifact kind is not known in advance
// (e.g., resolving /artifact/ scope entries in lens).
func (r *MaterializedArtifactsRepository) GetByCanonicalKeyAnyKind(ctx context.Context, tenantID string, canonicalKey string) (*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if canonicalKey == "" {
		return nil, fmt.Errorf("canonicalKey is required")
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("canonicalKey", "==", canonicalKey).
		Limit(1)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	if len(docs) == 0 {
		return nil, ErrMaterializedArtifactNotFound
	}

	var artifact horizonmodels.MaterializedArtifact
	if err := docs[0].DataTo(&artifact); err != nil {
		return nil, fmt.Errorf("data unmarshal failed: %w", err)
	}

	return &artifact, nil
}

// Update обновляет существующий MaterializedArtifact
func (r *MaterializedArtifactsRepository) Update(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.ID == "" {
		return fmt.Errorf("id is required")
	}
	if r.productDictionary != nil {
		if _, err := r.productDictionary.NormalizeArtifact(ctx, artifact); err != nil {
			log.Printf("[MaterializedArtifacts] WARNING: product dictionary normalization failed on update artifact=%s key=%s err=%v", strings.TrimSpace(artifact.ID), strings.TrimSpace(artifact.CanonicalKey), err)
		}
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)

	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	artifact.UpdatedAt = time.Now().UTC()
	if artifact.UpdatedBy == "" {
		artifact.UpdatedBy = "system"
	}

	if _, err := r.col.Doc(artifact.ID).Set(ctx, artifact); err != nil {
		return fmt.Errorf("firestore set failed: %w", err)
	}

	if r.useGCS {
		if err := r.uploadToGCS(ctx, artifact); err != nil {
			log.Printf("[MaterializedArtifacts] GCS upload failed for %s: %v", artifact.ID, err)
		}
	}

	return nil
}

// BatchGetByCanonicalKeys fetches multiple artifacts by (tenantID, kind, canonicalKeys).
// Returns map[canonicalKey]*Artifact. Firestore "in" query limit is 30, so keys are chunked.
func (r *MaterializedArtifactsRepository) BatchGetByCanonicalKeys(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, canonicalKeys []string) (map[string]*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" || !kind.Valid() || len(canonicalKeys) == 0 {
		return nil, nil
	}
	result := make(map[string]*horizonmodels.MaterializedArtifact)
	const chunkSize = 30 // Firestore "in" query limit
	for i := 0; i < len(canonicalKeys); i += chunkSize {
		end := i + chunkSize
		if end > len(canonicalKeys) {
			end = len(canonicalKeys)
		}
		chunk := canonicalKeys[i:end]
		q := r.col.
			Where("tenantId", "==", tenantID).
			Where("kind", "==", string(kind)).
			Where("canonicalKey", "in", chunk).
			Limit(len(chunk) * 2) // allow multiple docs per key (shouldn't happen)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, fmt.Errorf("batch get by canonical keys failed: %w", err)
		}
		for _, doc := range docs {
			var a horizonmodels.MaterializedArtifact
			if err := doc.DataTo(&a); err != nil {
				continue
			}
			if a.CanonicalKey != "" {
				result[a.CanonicalKey] = &a
			}
		}
	}
	return result, nil
}

// BatchUpsert creates or updates multiple MaterializedArtifacts in a single Firestore batch.
// Artifacts must have TenantID, Kind, CanonicalKey set. For updates, ID must be set.
// Returns created/updated IDs. Firestore batch limit is 500 ops.
func (r *MaterializedArtifactsRepository) BatchUpsert(ctx context.Context, artifacts []*horizonmodels.MaterializedArtifact) ([]string, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(artifacts))
	now := time.Now().UTC()
	const batchLimit = 500
	for i := 0; i < len(artifacts); i += batchLimit {
		end := i + batchLimit
		if end > len(artifacts) {
			end = len(artifacts)
		}
		batch := artifacts[i:end]
		fb := r.db.Batch()
		for _, a := range batch {
			if a == nil {
				continue
			}
			if r.productDictionary != nil {
				if _, err := r.productDictionary.NormalizeArtifact(ctx, a); err != nil {
					log.Printf("[MaterializedArtifacts] WARNING: product dictionary normalization failed on batch upsert artifact=%s key=%s err=%v", strings.TrimSpace(a.ID), strings.TrimSpace(a.CanonicalKey), err)
				}
			}
			horizonmodels.PopulateMaterializedArtifactIndexes(a)
			if err := a.Validate(); err != nil {
				return ids, fmt.Errorf("artifact validation failed: %w", err)
			}
			if a.UpdatedAt.IsZero() {
				a.UpdatedAt = now
			}
			if a.UpdatedBy == "" {
				a.UpdatedBy = "system"
			}
			if a.ID == "" {
				a.CreatedAt = now
				a.UpdatedAt = now
				if a.CreatedBy == "" {
					a.CreatedBy = "system"
				}
				if a.Status == "" {
					a.Status = "active"
				}
				doc := r.col.NewDoc()
				a.ID = doc.ID
				fb.Set(doc, a)
				ids = append(ids, a.ID)
			} else {
				fb.Set(r.col.Doc(a.ID), a)
				ids = append(ids, a.ID)
			}
		}
		if _, err := fb.Commit(ctx); err != nil {
			return ids, fmt.Errorf("batch commit failed: %w", err)
		}
		for _, a := range batch {
			if a != nil && r.useGCS {
				if err := r.uploadToGCS(ctx, a); err != nil {
					log.Printf("[MaterializedArtifacts] GCS upload failed for %s: %v", a.ID, err)
				}
			}
		}
	}
	return ids, nil
}

// Upsert создает или обновляет MaterializedArtifact по CanonicalKey
func (r *MaterializedArtifactsRepository) Upsert(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.CanonicalKey == "" {
		return fmt.Errorf("canonicalKey is required for upsert")
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	if artifact.TenantID == "" {
		return fmt.Errorf("tenantID is required")
	}
	if !artifact.Kind.Valid() {
		return fmt.Errorf("kind is required")
	}

	existing, err := r.GetByCanonicalKey(ctx, artifact.TenantID, artifact.Kind, artifact.CanonicalKey)
	if err != nil && err != ErrMaterializedArtifactNotFound {
		return fmt.Errorf("check existing failed: %w", err)
	}

	if existing != nil {
		// Update existing
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		artifact.CreatedBy = existing.CreatedBy
		return r.Update(ctx, artifact)
	}

	// Create new
	_, err = r.Create(ctx, artifact)
	return err
}

// ListByTenantAndKind получает список MaterializedArtifact по TenantID и Kind
func (r *MaterializedArtifactsRepository) ListByTenantAndKind(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", string(kind)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue // Skip invalid docs
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

// ListByLens получает список MaterializedArtifact по LensID
func (r *MaterializedArtifactsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if lensID == "" {
		return nil, fmt.Errorf("lensID is required")
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.
		Where("lensId", "==", lensID).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

// FindByNodeRef returns all MaterializedArtifacts where nodeRefs contains the given canonical path.
// Uses Firestore array-contains query. Requires composite index on (tenantId, kind, nodeRefs).
func (r *MaterializedArtifactsRepository) FindByNodeRef(ctx context.Context, tenantID string, nodeCanonicalPath string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" || nodeCanonicalPath == "" {
		return nil, nil
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", string(kind)).
		Where("nodeRefs", "array-contains", nodeCanonicalPath).
		Limit(50)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

// FindByNodeRefAnyTenant returns all MaterializedArtifacts where nodeRefs contains the given
// canonical path, regardless of tenant. Used when global graph merges must retarget overlays
// that may live in tenant-specific spaces.
func (r *MaterializedArtifactsRepository) FindByNodeRefAnyTenant(ctx context.Context, nodeCanonicalPath string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if nodeCanonicalPath == "" || !kind.Valid() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}

	q := r.col.
		Where("kind", "==", string(kind)).
		Where("nodeRefs", "array-contains", nodeCanonicalPath).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		if strings.TrimSpace(artifact.ID) == "" {
			artifact.ID = doc.Ref.ID
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

func (r *MaterializedArtifactsRepository) FindByNodeRefsAny(ctx context.Context, tenantID string, nodeCanonicalPaths []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAny(ctx, tenantID, "nodeRefs", nodeCanonicalPaths, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByFlowRefsAny(ctx context.Context, tenantID string, flowRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAny(ctx, tenantID, "flowRefs", flowRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByGeoRefsAnyTenant(ctx context.Context, geoRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAnyTenant(ctx, "geoRefs", geoRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByGeoRefsAny(ctx context.Context, tenantID string, geoRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAny(ctx, tenantID, "geoRefs", geoRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByFlowRefsAnyTenant(ctx context.Context, flowRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAnyTenant(ctx, "flowRefs", flowRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByZoneRefsAny(ctx context.Context, tenantID string, zoneRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAny(ctx, tenantID, "zoneRefs", zoneRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByZoneRefsAnyTenant(ctx context.Context, zoneRefs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAnyTenant(ctx, "zoneRefs", zoneRefs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAny(ctx, tenantID, "affectedZoneRefs", refs, kind, limit)
}

func (r *MaterializedArtifactsRepository) FindByAffectedZoneRefsAnyTenant(ctx context.Context, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return r.findByArrayFieldAnyTenant(ctx, "affectedZoneRefs", refs, kind, limit)
}

func (r *MaterializedArtifactsRepository) findByArrayFieldAny(ctx context.Context, tenantID, field string, values []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" || field == "" {
		return nil, nil
	}
	values = uniqueQueryValues(values)
	if len(values) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	const chunkSize = 30
	result := make([]*horizonmodels.MaterializedArtifact, 0, min(limit, len(values)))
	seen := make(map[string]struct{}, limit)
	for i := 0; i < len(values) && len(result) < limit; i += chunkSize {
		end := i + chunkSize
		if end > len(values) {
			end = len(values)
		}
		q := r.col.
			Where("tenantId", "==", tenantID).
			Where("kind", "==", string(kind)).
			Where(field, "array-contains-any", values[i:end]).
			Limit(limit)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, fmt.Errorf("firestore query failed for %s: %w", field, err)
		}
		for _, doc := range docs {
			if len(result) >= limit {
				break
			}
			var artifact horizonmodels.MaterializedArtifact
			if err := doc.DataTo(&artifact); err != nil {
				continue
			}
			horizonmodels.PopulateMaterializedArtifactIndexes(&artifact)
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			result = append(result, &artifact)
		}
	}
	return result, nil
}

func (r *MaterializedArtifactsRepository) findByArrayFieldAnyTenant(ctx context.Context, field string, values []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if field == "" {
		return nil, nil
	}
	values = uniqueQueryValues(values)
	if len(values) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	const chunkSize = 30
	result := make([]*horizonmodels.MaterializedArtifact, 0, min(limit, len(values)))
	seen := make(map[string]struct{}, limit)
	for i := 0; i < len(values) && len(result) < limit; i += chunkSize {
		end := i + chunkSize
		if end > len(values) {
			end = len(values)
		}
		q := r.col.
			Where("kind", "==", string(kind)).
			Where(field, "array-contains-any", values[i:end]).
			Limit(limit)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, fmt.Errorf("firestore query failed for %s: %w", field, err)
		}
		for _, doc := range docs {
			if len(result) >= limit {
				break
			}
			var artifact horizonmodels.MaterializedArtifact
			if err := doc.DataTo(&artifact); err != nil {
				continue
			}
			horizonmodels.PopulateMaterializedArtifactIndexes(&artifact)
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			result = append(result, &artifact)
		}
	}
	return result, nil
}

func uniqueQueryValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Delete удаляет MaterializedArtifact
func (r *MaterializedArtifactsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}

	if _, err := r.col.Doc(id).Delete(ctx); err != nil {
		return fmt.Errorf("firestore delete failed: %w", err)
	}

	return nil
}

// DeleteByLens deletes all materialized artifacts for a given lensId (best-effort).
// NOTE: This is used for cascade cleanup when a lens is deleted.
func (r *MaterializedArtifactsRepository) DeleteByLens(ctx context.Context, lensID string) (int, error) {
	if r == nil || r.col == nil || r.db == nil {
		return 0, fmt.Errorf("materialized artifacts repo not configured")
	}
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return 0, fmt.Errorf("lensID is required")
	}

	const batchSize = 200
	const maxBatches = 50 // hard cap to avoid long deletes in request path
	deleted := 0

	for i := 0; i < maxBatches; i++ {
		docs, err := r.col.Where("lensId", "==", lensID).Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return deleted, fmt.Errorf("firestore query failed: %w", err)
		}
		if len(docs) == 0 {
			return deleted, nil
		}
		b := r.db.Batch()
		for _, d := range docs {
			b.Delete(d.Ref)
		}
		if _, err := b.Commit(ctx); err != nil {
			return deleted, fmt.Errorf("firestore batch delete failed: %w", err)
		}
		deleted += len(docs)
		if len(docs) < batchSize {
			return deleted, nil
		}
	}

	return deleted, nil
}

// DeleteByTenantAndLens deletes all materialized artifacts for a given (tenantId, lensId) pair (best-effort).
// This is used for cascade cleanup to avoid cross-tenant deletions on id collisions.
func (r *MaterializedArtifactsRepository) DeleteByTenantAndLens(ctx context.Context, tenantID, lensID string) (int, error) {
	if r == nil || r.col == nil || r.db == nil {
		return 0, fmt.Errorf("materialized artifacts repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	if tenantID == "" || lensID == "" {
		return 0, fmt.Errorf("tenantID and lensID are required")
	}

	const batchSize = 200
	const maxBatches = 50 // hard cap to avoid long deletes in request path
	deleted := 0

	for i := 0; i < maxBatches; i++ {
		docs, err := r.col.Where("tenantId", "==", tenantID).Where("lensId", "==", lensID).Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return deleted, fmt.Errorf("firestore query failed: %w", err)
		}
		if len(docs) == 0 {
			return deleted, nil
		}
		b := r.db.Batch()
		for _, d := range docs {
			b.Delete(d.Ref)
		}
		if _, err := b.Commit(ctx); err != nil {
			return deleted, fmt.Errorf("firestore batch delete failed: %w", err)
		}
		deleted += len(docs)
		if len(docs) < batchSize {
			return deleted, nil
		}
	}

	return deleted, nil
}
