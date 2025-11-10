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
	col         *firestore.CollectionRef
	storage     *gcp.GCSClient
	bucket      string
	useGCS      bool
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
		}
	}
	
	_, err := r.col.Doc(d.ID).Set(ctx, d)
	return err
}

func (r *DocumentsRepository) Create(ctx context.Context, d *models.Document) (string, error) {
	if d == nil || d.SourceID == "" || d.TenantID == "" {
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
		}
	}
	
	if _, err := doc.Set(ctx, d); err != nil {
		return "", err
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
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).Where("externalId", "==", externalID).Limit(1)
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

type CoreDocsRepo struct{ col *firestore.CollectionRef }

func NewCoreDocsRepo(db *firestore.Client) *CoreDocsRepo {
	return &CoreDocsRepo{col: db.Collection("core_docs")}
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
	return err
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

func (r *CoreDocsRepo) ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]*models.CoreDoc, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit)
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
		res = append(res, &cd)
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
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).OrderBy("originPublishedAt", firestore.Desc)
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
	// Get document first to delete content from GCS
	doc, err := r.GetByID(ctx, id)
	if err == nil && doc != nil {
		_ = r.deleteContent(ctx, doc) // Best effort deletion
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
