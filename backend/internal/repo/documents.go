package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type DocumentsRepository struct {
	col              *firestore.CollectionRef
	storage          *gcp.GCSClient
	bucket           string
	useGCS           bool
	vertexSearchSync func(ctx context.Context, tenantID string) // Опциональная функция синхронизации с Vertex AI Search
	bigquerySync     *gcp.BigQueryClient                        // Опциональная синхронизация с BigQuery
}

func NewDocumentsRepository(db *firestore.Client) *DocumentsRepository {
	return &DocumentsRepository{col: db.Collection("rag_documents"), useGCS: false}
}

// WithStorage configures the repository to use GCS for storing document content
func (r *DocumentsRepository) WithStorage(storage *gcp.GCSClient, bucket string) *DocumentsRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && bucket != ""
	return r
}

// WithVertexSearchSync настраивает автоматическую синхронизацию с Vertex AI Search
// syncFn будет вызвана асинхронно после успешной загрузки документа в GCS
func (r *DocumentsRepository) WithVertexSearchSync(syncFn func(ctx context.Context, tenantID string)) *DocumentsRepository {
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
func (r *DocumentsRepository) uploadContent(ctx context.Context, d *models.Document) (*string, error) {
	if !r.useGCS || d.FullText == "" {
		return nil, nil
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
	if d == nil || d.ID == "" || d.SourceID == "" || d.TenantID == "" {
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
			// Триггерим синхронизацию с Vertex AI Search асинхронно (не блокируем основной поток)
			if r.vertexSearchSync != nil {
				go func(tenantID string) {
					// Используем background context с таймаутом
					syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					r.vertexSearchSync(syncCtx, tenantID)
				}(d.TenantID)
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
			// Триггерим синхронизацию с Vertex AI Search асинхронно (не блокируем основной поток)
			if r.vertexSearchSync != nil {
				go func(tenantID string) {
					// Используем background context с таймаутом
					syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					r.vertexSearchSync(syncCtx, tenantID)
				}(d.TenantID)
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
	col          *firestore.CollectionRef
	bigquerySync *gcp.BigQueryClient // Опциональная синхронизация с BigQuery
}

func NewCoreDocsRepo(db *firestore.Client) *CoreDocsRepo {
	return &CoreDocsRepo{col: db.Collection("core_docs")}
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *CoreDocsRepo) WithBigQuerySync(bq *gcp.BigQueryClient) *CoreDocsRepo {
	r.bigquerySync = bq
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
	if r.bigquerySync != nil {
		go func(doc *models.CoreDoc) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, doc)
		}(d)
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
	if r.bigquerySync != nil {
		go func(doc *models.CoreDoc) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, doc)
		}(d)
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
		// Триггерим синхронизацию после удаления (чтобы удалить из Vertex AI Search)
		if r.vertexSearchSync != nil {
			go func(tenantID string) {
				syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				r.vertexSearchSync(syncCtx, tenantID)
			}(doc.TenantID)
		}
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
	// Триггерим синхронизацию после удаления (чтобы удалить из Vertex AI Search)
	if r.vertexSearchSync != nil {
		go func(tenantID string) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r.vertexSearchSync(syncCtx, tenantID)
		}(d.TenantID)
	}
	_, err := r.col.Doc(d.ID).Delete(ctx)
	return err
}

// DeleteBySource deletes all documents for a source and their content from GCS
// Note: chunks should be deleted separately using ChunksRepository.DeleteByDoc
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

// GetByCoreDocID finds a document by CoreDocID stored in Attrs
func (r *DocumentsRepository) GetByCoreDocID(ctx context.Context, tenantID, coreDocID string) (*models.Document, error) {
	if tenantID == "" || coreDocID == "" {
		return nil, ErrInvalidDocument
	}
	// Query documents where attrs.coreDocId == coreDocID
	q := r.col.Where("tenantId", "==", tenantID).Limit(100)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		var doc models.Document
		if err := d.DataTo(&doc); err != nil {
			continue
		}
		if doc.Attrs != nil {
			if v, ok := doc.Attrs["coreDocId"].(string); ok && v == coreDocID {
				return &doc, nil
			}
		}
	}
	return nil, ErrInvalidDocument
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
		"createdAt":   d.CreatedAt,
		"updatedAt":   d.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "documents_metadata", row); err != nil {
		// Логируем ошибку, но не прерываем выполнение (best effort)
		// В production можно использовать структурированное логирование
		_ = err // Игнорируем ошибку для асинхронной синхронизации
	}
}

// syncToBigQuery синхронизирует метаданные CoreDoc в BigQuery
func (r *CoreDocsRepo) syncToBigQuery(ctx context.Context, d *models.CoreDoc) {
	if r.bigquerySync == nil || d == nil {
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
		"id":           d.ID,
		"tenantId":     d.TenantID,
		"projectId":    d.ProjectID,
		"filename":     d.Filename,
		"contentType":  d.ContentType,
		"description":  description,
		"sizeBytes":    sizeBytes,
		"storagePath":  storagePath,
		"extractedText": extractedText,
		"parseStatus":  parseStatus,
		"parseMeta":    d.ParseMeta,
		"pageCount":    pageCount,
		"docType":      docType,
		"createdBy":    createdBy,
		"createdAt":    d.CreatedAt,
		"updatedAt":    d.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "core_docs_metadata", row); err != nil {
		// Логируем ошибку, но не прерываем выполнение (best effort)
		_ = err // Игнорируем ошибку для асинхронной синхронизации
	}
}
