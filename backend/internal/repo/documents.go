package repo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"golang.org/x/time/rate"
)

type DocumentsRepository struct {
	col              *firestore.CollectionRef
	db               *firestore.Client // Firestore client for batch operations
	storage          *gcp.GCSClient
	bucket           string
	useGCS           bool
	uploadLimiter    *rate.Limiter                                                                // Rate limiter for GCS uploads to prevent 429 errors
	limiterMu        sync.Mutex                                                                   // Mutex to protect limiter initialization
	vertexSearchSync func(ctx context.Context, gcsURI string) (operationName string, err error) // Опциональная функция синхронизации с Vertex AI Search; передавать конкретный gs:// URI, не wildcard
	bigquerySync     *gcp.BigQueryClient                                                          // Опциональная синхронизация с BigQuery
}

func NewDocumentsRepository(db *firestore.Client) *DocumentsRepository {
	return &DocumentsRepository{
		col:    db.Collection("rag_documents"),
		db:     db,
		useGCS: false,
	}
}

// Collection returns the Firestore collection reference (for queries)
func (r *DocumentsRepository) Collection() *firestore.CollectionRef {
	return r.col
}

// WithStorage configures the repository to use GCS for storing document content
// Rate limiter is set to 10 requests per second (100ms between requests) to prevent GCS 429 errors
func (r *DocumentsRepository) WithStorage(storage *gcp.GCSClient, bucket string) *DocumentsRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && bucket != ""
	// Initialize rate limiter: 10 requests per second (allows burst of 10)
	// This prevents hitting GCS rate limits when processing many documents in quick succession
	if r.useGCS {
		r.uploadLimiter = rate.NewLimiter(rate.Limit(10), 10)
	}
	return r
}

// WithVertexSearchSync настраивает автоматическую синхронизацию с Vertex AI Search
// syncFn вызывается с конкретным gs:// URI файла (не wildcard), чтобы не превышать лимит Discovery Engine (1000 файлов на запрос).
func (r *DocumentsRepository) WithVertexSearchSync(syncFn func(ctx context.Context, gcsURI string) (operationName string, err error)) *DocumentsRepository {
	r.vertexSearchSync = syncFn
	return r
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *DocumentsRepository) WithBigQuerySync(bq *gcp.BigQueryClient) *DocumentsRepository {
	r.bigquerySync = bq
	return r
}

// getObjectPath returns the GCS object path for a document
func (r *DocumentsRepository) getObjectPath(d *models.Document) string {
	// Format: {tenantId}/{sourceId}/{docId}.txt
	return fmt.Sprintf("%s/%s/%s.txt", d.TenantID, d.SourceID, d.ID)
}

// getStoragePath returns the full gs:// path for a document
func (r *DocumentsRepository) getStoragePath(d *models.Document) string {
	return fmt.Sprintf("gs://%s/%s", r.bucket, r.getObjectPath(d))
}

// uploadContent uploads document content to GCS and returns the storage path
// Если файл уже существует, он будет перезаписан (GCS UploadBytes перезаписывает по умолчанию)
// Это означает, что обновления документов автоматически синхронизируются с Vertex AI Search
// Rate limiting is applied to prevent GCS 429 errors when processing many documents
func (r *DocumentsRepository) uploadContent(ctx context.Context, d *models.Document) (*string, error) {
	if !r.useGCS || d.FullText == "" {
		return nil, nil
	}
	// Apply rate limiting to prevent GCS 429 errors
	if r.uploadLimiter != nil {
		if err := r.uploadLimiter.Wait(ctx); err != nil {
			// If context is cancelled, return error
			return nil, fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}
	objectPath := r.getObjectPath(d)
	contentType := "text/plain"
	if d.ContentType != "" {
		contentType = d.ContentType
	}
	metadata := map[string]string{
		"tenantId":   d.TenantID,
		"sourceId":   d.SourceID,
		"documentId": d.ID,
		"title":      d.Title,
	}
	// Extract projectId from SourceID (format: project:projectId) or Attrs
	if d.SourceID != "" && strings.HasPrefix(d.SourceID, "project:") {
		projectID := strings.TrimPrefix(d.SourceID, "project:")
		metadata["projectId"] = projectID
	} else if d.Attrs != nil {
		if pid, ok := d.Attrs["projectId"].(string); ok && pid != "" {
			metadata["projectId"] = pid
		}
	}
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, []byte(d.FullText), contentType, metadata); err != nil {
		return nil, fmt.Errorf("failed to upload document content to GCS: %w", err)
	}
	storagePath := r.getStoragePath(d)
	return &storagePath, nil
}

// deleteContent deletes document content from GCS
func (r *DocumentsRepository) deleteContent(ctx context.Context, d *models.Document) error {
	if !r.useGCS || d.StoragePath == nil {
		return nil
	}
	// Extract object path from gs://bucket/path
	gsPath := *d.StoragePath
	if !strings.HasPrefix(gsPath, "gs://") {
		return nil
	}
	// Remove gs://bucket/ prefix
	objectPath := strings.TrimPrefix(gsPath, fmt.Sprintf("gs://%s/", r.bucket))
	if err := r.storage.DeleteObject(ctx, r.bucket, objectPath); err != nil {
		// Log but don't fail - best effort deletion
		return fmt.Errorf("failed to delete document content from GCS: %w", err)
	}
	return nil
}

func (r *DocumentsRepository) Upsert(ctx context.Context, d *models.Document) error {
	// Allow empty TenantID for public sources (documents are global)
	if d == nil || d.ID == "" || d.SourceID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.IngestedAt.IsZero() {
		d.IngestedAt = now
	}
	d.UpdatedAt = now

	// Upload content to GCS if enabled and FullText is present
	if d.FullText != "" {
		storagePath, err := r.uploadContent(ctx, d)
		if err != nil {
			return err
		}
		if storagePath != nil {
			d.StoragePath = storagePath
			if r.vertexSearchSync != nil {
				gcsURI := *storagePath
				go func(uri string) {
					syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					operationName, err := r.vertexSearchSync(syncCtx, uri)
					if err == nil && operationName != "" {
						log.Printf("[DocumentsRepo] Vertex AI Search sync triggered: operation=%s", operationName)
					}
				}(gcsURI)
			}
		}
	}

	_, err := r.col.Doc(d.ID).Set(ctx, d)
	if err != nil {
		return err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(doc *models.Document) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, doc)
		}(d)
	}

	return nil
}

func (r *DocumentsRepository) Create(ctx context.Context, d *models.Document) (string, error) {
	// Allow empty TenantID for public sources (documents are global)
	if d == nil || d.SourceID == "" {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.IngestedAt = now
	d.UpdatedAt = now
	doc := r.col.NewDoc()
	d.ID = doc.ID

	// Upload content to GCS if enabled and FullText is present
	if d.FullText != "" {
		storagePath, err := r.uploadContent(ctx, d)
		if err != nil {
			return "", err
		}
		if storagePath != nil {
			d.StoragePath = storagePath
			if r.vertexSearchSync != nil {
				gcsURI := *storagePath
				go func(uri string) {
					syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					operationName, err := r.vertexSearchSync(syncCtx, uri)
					if err == nil && operationName != "" {
						log.Printf("[DocumentsRepo] Vertex AI Search sync triggered: operation=%s", operationName)
					}
				}(gcsURI)
			}
		}
	}

	if _, err := doc.Set(ctx, d); err != nil {
		return "", err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(doc *models.Document) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, doc)
		}(d)
	}

	return doc.ID, nil
}

func (r *DocumentsRepository) GetByID(ctx context.Context, id string) (*models.Document, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var d models.Document
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DocumentsRepository) GetByExternalID(ctx context.Context, tenantID, sourceID, externalID string) (*models.Document, error) {
	// For public sources (empty tenantID), query by sourceId and externalId only
	// For tenant-scoped sources, filter by tenantId, sourceId, and externalId
	var q firestore.Query
	if tenantID == "" {
		// Public source: query by sourceId and externalId only (documents have TenantID="")
		q = r.col.Where("sourceId", "==", sourceID).Where("externalId", "==", externalID).Limit(1)
	} else {
		// Tenant-scoped source: filter by tenantId, sourceId, and externalId
		q = r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).Where("externalId", "==", externalID).Limit(1)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var d models.Document
	if err := docs[0].DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ---- CoreDocs (project-level attachments) ----

var ErrInvalidCoreDoc = errors.New("invalid core doc")

type CoreDocsRepo struct {
	col              *firestore.CollectionRef
	bigquerySync     *gcp.BigQueryClient                                                  // Опциональная синхронизация с BigQuery
	indexingCallback func(ctx context.Context, tenantID string, cd *models.CoreDoc) error // Опциональная индексация в Vertex AI Search
}

func NewCoreDocsRepo(db *firestore.Client) *CoreDocsRepo {
	return &CoreDocsRepo{col: db.Collection("core_docs")}
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *CoreDocsRepo) WithBigQuerySync(bq *gcp.BigQueryClient) *CoreDocsRepo {
	r.bigquerySync = bq
	return r
}

// WithIndexingCallback настраивает автоматическую индексацию в Vertex AI Search
// callback будет вызван асинхронно после создания/обновления CoreDoc
func (r *CoreDocsRepo) WithIndexingCallback(callback func(ctx context.Context, tenantID string, cd *models.CoreDoc) error) *CoreDocsRepo {
	r.indexingCallback = callback
	return r
}

func (r *CoreDocsRepo) Create(ctx context.Context, d *models.CoreDoc) (string, error) {
	if d == nil || d.TenantID == "" || d.ProjectID == "" || d.Filename == "" {
		return "", ErrInvalidCoreDoc
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	doc := r.col.NewDoc()
	d.ID = doc.ID
	if _, err := doc.Set(ctx, d); err != nil {
		return "", err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	// Log before goroutine to ensure it's captured
	if r.bigquerySync != nil {
		log.Printf("[CoreDocsRepo] Triggering BigQuery sync for CoreDoc %s (%s) in Create (bigquerySync=%v)", d.ID, d.Filename, r.bigquerySync != nil)
		docCopy := d // Capture doc for goroutine
		go func(doc *models.CoreDoc) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[CoreDocsRepo] PANIC in BigQuery sync goroutine for CoreDoc %s: %v", doc.ID, r)
				}
			}()
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			log.Printf("[CoreDocsRepo] Starting BigQuery sync goroutine for CoreDoc %s", doc.ID)
			r.syncToBigQuery(syncCtx, doc)
			log.Printf("[CoreDocsRepo] Completed BigQuery sync goroutine for CoreDoc %s", doc.ID)
		}(docCopy)
	} else {
		log.Printf("[CoreDocsRepo] WARNING: bigquerySync is nil, skipping sync for CoreDoc %s (%s)", d.ID, d.Filename)
	}

	// Индексация в Vertex AI Search асинхронно (best effort, не блокируем основной поток)
	shouldIndex := false
	// Default rule: index only when we have extracted text (static docs after parsing)
	if d.ExtractedText != nil && strings.TrimSpace(*d.ExtractedText) != "" {
		shouldIndex = true
	}
	// Special-case: dynamic artifacts can be indexed from sections even if ExtractedText is empty
	if !shouldIndex && d.DocType != nil && *d.DocType == "dynamic_artifact" && d.ArtifactConfig != nil && len(d.ArtifactConfig.Structure) > 0 {
		shouldIndex = true
	}
	if r.indexingCallback != nil && shouldIndex {
		docCopy := d // Capture doc for goroutine
		tenantID := d.TenantID
		go func(doc *models.CoreDoc) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[CoreDocsRepo] PANIC in indexing goroutine for CoreDoc %s: %v", doc.ID, r)
				}
			}()
			indexCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			log.Printf("[CoreDocsRepo] Starting indexing goroutine for CoreDoc %s", doc.ID)
			if err := r.indexingCallback(indexCtx, tenantID, doc); err != nil {
				log.Printf("[CoreDocsRepo] Failed to index CoreDoc %s: %v", doc.ID, err)
			} else {
				log.Printf("[CoreDocsRepo] Successfully indexed CoreDoc %s", doc.ID)
			}
		}(docCopy)
	}

	return doc.ID, nil
}

func (r *CoreDocsRepo) Upsert(ctx context.Context, d *models.CoreDoc) error {
	if d == nil || d.ID == "" || d.TenantID == "" || d.ProjectID == "" || d.Filename == "" {
		return ErrInvalidCoreDoc
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	_, err := r.col.Doc(d.ID).Set(ctx, d)
	if err != nil {
		return err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	// Log before goroutine to ensure it's captured
	if r.bigquerySync != nil {
		log.Printf("[CoreDocsRepo] Triggering BigQuery sync for CoreDoc %s (%s) in Upsert (bigquerySync=%v)", d.ID, d.Filename, r.bigquerySync != nil)
		docCopy := d // Capture doc for goroutine
		go func(doc *models.CoreDoc) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[CoreDocsRepo] PANIC in BigQuery sync goroutine for CoreDoc %s: %v", doc.ID, r)
				}
			}()
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			log.Printf("[CoreDocsRepo] Starting BigQuery sync goroutine for CoreDoc %s", doc.ID)
			r.syncToBigQuery(syncCtx, doc)
			log.Printf("[CoreDocsRepo] Completed BigQuery sync goroutine for CoreDoc %s", doc.ID)
		}(docCopy)
	} else {
		log.Printf("[CoreDocsRepo] WARNING: bigquerySync is nil, skipping sync for CoreDoc %s (%s)", d.ID, d.Filename)
	}

	// Индексация в Vertex AI Search асинхронно (best effort, не блокируем основной поток)
	shouldIndex := false
	// Default rule: index only when we have extracted text (static docs after parsing)
	if d.ExtractedText != nil && strings.TrimSpace(*d.ExtractedText) != "" {
		shouldIndex = true
	}
	// Special-case: dynamic artifacts can be indexed from sections even if ExtractedText is empty
	if !shouldIndex && d.DocType != nil && *d.DocType == "dynamic_artifact" && d.ArtifactConfig != nil && len(d.ArtifactConfig.Structure) > 0 {
		shouldIndex = true
	}
	if r.indexingCallback != nil && shouldIndex {
		docCopy := d // Capture doc for goroutine
		tenantID := d.TenantID
		go func(doc *models.CoreDoc) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[CoreDocsRepo] PANIC in indexing goroutine for CoreDoc %s: %v", doc.ID, r)
				}
			}()
			indexCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			log.Printf("[CoreDocsRepo] Starting indexing goroutine for CoreDoc %s", doc.ID)
			if err := r.indexingCallback(indexCtx, tenantID, doc); err != nil {
				log.Printf("[CoreDocsRepo] Failed to index CoreDoc %s: %v", doc.ID, err)
			} else {
				log.Printf("[CoreDocsRepo] Successfully indexed CoreDoc %s", doc.ID)
			}
		}(docCopy)
	}

	return nil
}

func (r *CoreDocsRepo) Get(ctx context.Context, id string) (*models.CoreDoc, error) {
	if id == "" {
		return nil, ErrInvalidCoreDoc
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var out models.CoreDoc
	if err := snap.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *CoreDocsRepo) ListByProject(ctx context.Context, tenantID, projectID string, limit int, excludeDocumentType *string) ([]*models.CoreDoc, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID)
	q = q.OrderBy("createdAt", firestore.Desc)
	// Increase limit if we need to filter, to account for excluded items
	queryLimit := limit
	if excludeDocumentType != nil && *excludeDocumentType != "" && limit > 0 {
		// Request more documents to account for filtering
		queryLimit = limit * 2
	}
	if queryLimit > 0 {
		q = q.Limit(queryLimit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.CoreDoc, 0, len(docs))
	for _, d := range docs {
		var cd models.CoreDoc
		if err := d.DataTo(&cd); err != nil {
			continue
		}
		// Filter out excluded documentType on client side
		// Check documentType from Firestore document data (may not be in struct)
		if excludeDocumentType != nil && *excludeDocumentType != "" {
			if docType, ok := d.Data()["documentType"].(string); ok && docType == *excludeDocumentType {
				continue
			}
		}
		res = append(res, &cd)
		// Stop if we've reached the desired limit
		if limit > 0 && len(res) >= limit {
			break
		}
	}
	return res, nil
}

func (r *CoreDocsRepo) Delete(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, ErrInvalidCoreDoc
	}
	_, err := r.col.Doc(id).Delete(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *DocumentsRepository) ListBySource(ctx context.Context, tenantID, sourceID string, limit int) ([]*models.Document, error) {
	// For public sources (empty tenantID), query by sourceId only
	// Documents from public sources have TenantID="" and are visible to all tenants
	// For tenant-scoped sources, filter by both tenantId and sourceId
	var q firestore.Query
	if tenantID == "" {
		// Public source: query by sourceId only (documents have TenantID="")
		q = r.col.Where("sourceId", "==", sourceID).OrderBy("originPublishedAt", firestore.Desc)
	} else {
		// Tenant-scoped source: filter by both tenantId and sourceId
		q = r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).OrderBy("originPublishedAt", firestore.Desc)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.Document, 0, len(docs))
	for _, d := range docs {
		var doc models.Document
		if err := d.DataTo(&doc); err != nil {
			continue
		}
		res = append(res, &doc)
	}
	return res, nil
}

// ListByStatus returns documents with a specific status, optionally filtered by sourceID
func (r *DocumentsRepository) ListByStatus(ctx context.Context, tenantID, sourceID, status string, limit int) ([]*models.Document, error) {
	var q firestore.Query
	if tenantID == "" {
		// Public source: query by sourceId only
		if sourceID != "" {
			q = r.col.Where("sourceId", "==", sourceID).Where("status", "==", status).OrderBy("createdAt", firestore.Desc)
		} else {
			q = r.col.Where("status", "==", status).OrderBy("createdAt", firestore.Desc)
		}
	} else {
		// Tenant-scoped source: filter by tenantId
		if sourceID != "" {
			q = r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).Where("status", "==", status).OrderBy("createdAt", firestore.Desc)
		} else {
			q = r.col.Where("tenantId", "==", tenantID).Where("status", "==", status).OrderBy("createdAt", firestore.Desc)
		}
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.Document, 0, len(docs))
	for _, d := range docs {
		var doc models.Document
		if err := d.DataTo(&doc); err != nil {
			continue
		}
		res = append(res, &doc)
	}
	return res, nil
}

// ListBySources returns latest documents for a set of source IDs with optional filters
func (r *DocumentsRepository) ListBySources(ctx context.Context, tenantID string, sourceIDs []string, limit int, language, documentType *string) ([]*models.Document, error) {
	if len(sourceIDs) == 0 {
		return []*models.Document{}, nil
	}
	// Firestore IN supports up to 10 values; batch if needed
	batchSize := 10
	var results []*models.Document
	for i := 0; i < len(sourceIDs); i += batchSize {
		end := i + batchSize
		if end > len(sourceIDs) {
			end = len(sourceIDs)
		}
		batch := sourceIDs[i:end]
		q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "in", batch).OrderBy("originPublishedAt", firestore.Desc)
		if limit > 0 {
			q = q.Limit(limit)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			var doc models.Document
			if err := d.DataTo(&doc); err != nil {
				continue
			}
			if language != nil && *language != "" && doc.Language != *language {
				continue
			}
			if documentType != nil && *documentType != "" && doc.DocumentType != *documentType {
				continue
			}
			results = append(results, &doc)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Delete deletes a document by ID and its content from GCS if present
func (r *DocumentsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	// Get document first to delete content from GCS and trigger sync
	doc, err := r.GetByID(ctx, id)
	if err == nil && doc != nil {
		_ = r.deleteContent(ctx, doc) // Best effort deletion
		// Sync on delete would require full tenant reconcile (wildcard) which exceeds Discovery Engine 1000-file limit.
		// Skip sync; index may be stale until next full reindex.
	}
	_, err = r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteWithStorage deletes a document and its content from GCS (explicit version)
func (r *DocumentsRepository) DeleteWithStorage(ctx context.Context, d *models.Document) error {
	if d == nil || d.ID == "" {
		return ErrInvalidDocument
	}
	_ = r.deleteContent(ctx, d) // Best effort deletion
	// Sync on delete would require full tenant reconcile; skip to avoid 1000-file limit.
	_, err := r.col.Doc(d.ID).Delete(ctx)
	return err
}

// DeleteBySource deletes all documents for a source and their content from GCS
func (r *DocumentsRepository) DeleteBySource(ctx context.Context, tenantID, sourceID string) error {
	if tenantID == "" || sourceID == "" {
		return ErrInvalidDocument
	}
	// List all documents for this source
	docs, err := r.ListBySource(ctx, tenantID, sourceID, 0) // 0 = no limit
	if err != nil {
		return err
	}
	// Delete each document (which will also delete content from GCS)
	for _, doc := range docs {
		_ = r.DeleteWithStorage(ctx, doc) // Best effort deletion
	}
	return nil
}

// MarkTenantDocumentsAsIndexed обновляет VertexSearchIndexedAt для всех документов tenant, которые имеют StoragePath но еще не помечены как проиндексированные
func (r *DocumentsRepository) MarkTenantDocumentsAsIndexed(ctx context.Context, tenantID string) error {
	now := time.Now().UTC()
	// Query documents by tenant (filter storagePath in memory to avoid composite index requirements)
	q := r.col.Where("tenantId", "==", tenantID)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("failed to query documents: %w", err)
	}

	batch := r.db.Batch()
	batchCount := 0
	updatedCount := 0
	for _, docSnap := range docs {
		var d models.Document
		if err := docSnap.DataTo(&d); err != nil {
			continue
		}
		if d.StoragePath == nil || *d.StoragePath == "" {
			continue
		}
		// Only update if VertexSearchIndexedAt is nil or zero
		if d.VertexSearchIndexedAt == nil || d.VertexSearchIndexedAt.IsZero() {
			batch.Update(docSnap.Ref, []firestore.Update{
				{Path: "vertexSearchIndexedAt", Value: now},
				{Path: "updatedAt", Value: now},
			})
			batchCount++
			updatedCount++

			// Firestore batch limit is 500 operations
			if batchCount >= 500 {
				if _, err := batch.Commit(ctx); err != nil {
					return fmt.Errorf("failed to commit batch: %w", err)
				}
				batch = r.db.Batch()
				batchCount = 0
			}
		}
	}

	if batchCount > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit final batch: %w", err)
		}
	}

	if updatedCount > 0 {
		log.Printf("[DocumentsRepo] Marked %d documents as indexed for tenant %s", updatedCount, tenantID)
	}
	return nil
}

// GetByCoreDocID finds a document by CoreDocID stored in Attrs
func (r *DocumentsRepository) GetByCoreDocID(ctx context.Context, tenantID, coreDocID string) (*models.Document, error) {
	if tenantID == "" || coreDocID == "" {
		return nil, ErrInvalidDocument
	}
	// Query documents where attrs.coreDocId == coreDocID using Firestore nested field query
	// Firestore supports dot notation for nested fields: "attrs.coreDocId"
	q := r.col.Where("tenantId", "==", tenantID).Where("attrs.coreDocId", "==", coreDocID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("[DocumentsRepo] GetByCoreDocID query error: tenantID=%s, coreDocID=%s, error=%v", tenantID, coreDocID, err)
		return nil, err
	}
	if len(docs) == 0 {
		log.Printf("[DocumentsRepo] GetByCoreDocID: no documents found for tenantID=%s, coreDocID=%s", tenantID, coreDocID)
		// Fallback: try without nested field query (in case attrs.coreDocId is not indexed)
		// Get all documents for tenant and filter in memory
		q2 := r.col.Where("tenantId", "==", tenantID).Limit(500)
		allDocs, err2 := q2.Documents(ctx).GetAll()
		if err2 == nil {
			log.Printf("[DocumentsRepo] GetByCoreDocID fallback: found %d documents for tenant, searching in memory", len(allDocs))
			// Логируем первые несколько coreDocId для отладки
			foundCoreDocIds := make([]string, 0, 10)
			for i, d := range allDocs {
				var doc models.Document
				if err := d.DataTo(&doc); err != nil {
					continue
				}
				if doc.Attrs != nil {
					if v, ok := doc.Attrs["coreDocId"].(string); ok && v != "" {
						if v == coreDocID {
							log.Printf("[DocumentsRepo] GetByCoreDocID fallback: found document %s with coreDocId %s", doc.ID, coreDocID)
							return &doc, nil
						}
						// Собираем первые 10 coreDocId для логирования
						if len(foundCoreDocIds) < 10 {
							foundCoreDocIds = append(foundCoreDocIds, v)
						}
					}
				}
				// Логируем первые 3 документа для отладки
				if i < 3 && doc.Attrs != nil {
					attrsKeys := make([]string, 0, len(doc.Attrs))
					for k := range doc.Attrs {
						attrsKeys = append(attrsKeys, k)
					}
					log.Printf("[DocumentsRepo] GetByCoreDocID fallback: document %s, Attrs keys: %v", doc.ID, attrsKeys)
				}
			}
			if len(foundCoreDocIds) > 0 {
				log.Printf("[DocumentsRepo] GetByCoreDocID fallback: sample coreDocIds found: %v (looking for: %s)", foundCoreDocIds, coreDocID)
			} else {
				log.Printf("[DocumentsRepo] GetByCoreDocID fallback: no documents with coreDocId in Attrs found")
			}
		}
		return nil, ErrInvalidDocument
	}
	var doc models.Document
	if err := docs[0].DataTo(&doc); err != nil {
		return nil, err
	}
	log.Printf("[DocumentsRepo] GetByCoreDocID: found document %s with coreDocId %s", doc.ID, coreDocID)
	return &doc, nil
}

// syncToBigQuery синхронизирует метаданные документа в BigQuery
func (r *DocumentsRepository) syncToBigQuery(ctx context.Context, d *models.Document) {
	if r.bigquerySync == nil || d == nil {
		return
	}

	storagePath := ""
	if d.StoragePath != nil {
		storagePath = *d.StoragePath
	}

	row := map[string]interface{}{
		"id":           d.ID,
		"tenantId":     d.TenantID,
		"sourceId":     d.SourceID,
		"externalId":   d.ExternalID,
		"title":        d.Title,
		"url":          d.URL,
		"documentPath": d.DocumentPath,
		"storagePath":  storagePath,
		"status":       d.Status,
		"contentType":  d.ContentType,
		"documentType": d.DocumentType,
		"language":     d.Language,
		"contentHash":  d.ContentHash,
		"ingestedAt":   d.IngestedAt,
		"createdAt":    d.CreatedAt,
		"updatedAt":    d.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "documents_metadata", row); err != nil {
		// Логируем ошибку для диагностики (best effort, не прерываем выполнение)
		errStr := err.Error()
		if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "already exists") || strings.Contains(errStr, "UNIQUE") {
			// Документ уже существует - это нормально
			return
		}
		log.Printf("[DocumentsRepo] Failed to sync Document %s to BigQuery: %v", d.ID, err)
	} else {
		log.Printf("[DocumentsRepo] Successfully synced Document %s to BigQuery", d.ID)
	}
}

// syncToBigQuery синхронизирует метаданные CoreDoc в BigQuery
func (r *CoreDocsRepo) syncToBigQuery(ctx context.Context, d *models.CoreDoc) {
	log.Printf("[CoreDocsRepo] syncToBigQuery called for CoreDoc %s (%s), bigquerySync=%v", d.ID, d.Filename, r.bigquerySync != nil)
	if r.bigquerySync == nil || d == nil {
		if r.bigquerySync == nil {
			log.Printf("[CoreDocsRepo] WARNING: bigquerySync is nil for CoreDoc %s", d.ID)
		}
		return
	}

	storagePath := ""
	if d.StoragePath != nil {
		storagePath = *d.StoragePath
	}

	description := ""
	if d.Description != nil {
		description = *d.Description
	}

	extractedText := ""
	if d.ExtractedText != nil {
		extractedText = *d.ExtractedText
	}

	parseStatus := ""
	if d.ParseStatus != nil {
		parseStatus = *d.ParseStatus
	}

	docType := ""
	if d.DocType != nil {
		docType = *d.DocType
	}

	createdBy := ""
	if d.CreatedBy != nil {
		createdBy = *d.CreatedBy
	}

	var sizeBytes *int64
	if d.SizeBytes != nil {
		sizeBytes = d.SizeBytes
	}

	var pageCount *int
	if d.PageCount != nil {
		pageCount = d.PageCount
	}

	row := map[string]interface{}{
		"id":            d.ID,
		"tenantId":      d.TenantID,
		"projectId":     d.ProjectID,
		"filename":      d.Filename,
		"contentType":   d.ContentType,
		"description":   description,
		"sizeBytes":     sizeBytes,
		"storagePath":   storagePath,
		"extractedText": extractedText,
		"parseStatus":   parseStatus,
		"parseMeta":     d.ParseMeta,
		"pageCount":     pageCount,
		"docType":       docType,
		"createdBy":     createdBy,
		"createdAt":     d.CreatedAt,
		"updatedAt":     d.UpdatedAt,
	}

	// Use UpsertRow to handle both insert and update (avoids duplicate errors)
	if err := r.bigquerySync.UpsertRow(ctx, "core_docs_metadata", row); err != nil {
		// Логируем ошибку для диагностики (best effort, не прерываем выполнение)
		log.Printf("[CoreDocsRepo] Failed to sync CoreDoc %s (%s) to BigQuery: %v", d.ID, d.Filename, err)
	} else {
		log.Printf("[CoreDocsRepo] Successfully synced CoreDoc %s (%s) to BigQuery", d.ID, d.Filename)
	}
}
