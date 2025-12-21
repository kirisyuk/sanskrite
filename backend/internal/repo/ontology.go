package repo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// validateCanonicalPath validates canonical path format without importing taxonomy package
// to avoid circular dependencies. This is a simplified version that checks basic format.
func validateCanonicalPath(path string) error {
	if path == "" {
		return fmt.Errorf("canonical path cannot be empty")
	}
	if path == "/" {
		return fmt.Errorf("canonical path cannot be root path '/'")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("canonical path must start with '/': %s", path)
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return fmt.Errorf("canonical path has no segments: %s", path)
	}
	// Check that all segments are valid slugs (lowercase alphanumeric and hyphens)
	slugRegex := regexp.MustCompile(`^[a-z0-9-]+$`)
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("canonical path segment %d is empty: %s", i, path)
		}
		if !slugRegex.MatchString(part) {
			return fmt.Errorf("canonical path segment %d contains invalid characters: %s", i, path)
		}
	}
	return nil
}

var ErrInvalidOntologyDocument = errors.New("invalid ontology document")

// --- Entity Repository ---

type EntityRepository struct {
	col          *firestore.CollectionRef
	bigquerySync *gcp.BigQueryClient // Опциональная синхронизация с BigQuery
}

func NewEntityRepository(db *firestore.Client) *EntityRepository {
	return &EntityRepository{col: db.Collection("ontology_entities")}
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *EntityRepository) WithBigQuerySync(bq *gcp.BigQueryClient) *EntityRepository {
	r.bigquerySync = bq
	return r
}

func (r *EntityRepository) Create(ctx context.Context, e *models.Entity) (string, error) {
	// Allow empty TenantID for global/public sources
	if e == nil {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Status == "" {
		e.Status = "active"
	}

	// Validate canonical paths if present
	if len(e.CanonicalPaths) > 0 {
		for _, cp := range e.CanonicalPaths {
			if err := validateCanonicalPath(cp); err != nil {
				return "", fmt.Errorf("invalid canonical path '%s': %w", cp, err)
			}
		}
	}

	doc := r.col.NewDoc()
	e.ID = doc.ID
	if _, err := doc.Set(ctx, e); err != nil {
		return "", err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(entity *models.Entity) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, entity)
		}(e)
	}

	return e.ID, nil
}

func (r *EntityRepository) Upsert(ctx context.Context, e *models.Entity) error {
	// Allow empty TenantID for global/public sources
	if e == nil || e.ID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Status == "" {
		e.Status = "active"
	}

	// Validate canonical paths if present
	if len(e.CanonicalPaths) > 0 {
		for _, cp := range e.CanonicalPaths {
			if err := validateCanonicalPath(cp); err != nil {
				return fmt.Errorf("invalid canonical path '%s': %w", cp, err)
			}
		}
	}

	_, err := r.col.Doc(e.ID).Set(ctx, e)
	if err != nil {
		return err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(entity *models.Entity) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, entity)
		}(e)
	}

	return nil
}

func (r *EntityRepository) Get(ctx context.Context, id string) (*models.Entity, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var e models.Entity
	if err := snap.DataTo(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *EntityRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Entity, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Entity
	for _, d := range docs {
		var e models.Entity
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *EntityRepository) ListByTenantAndType(ctx context.Context, tenantID string, entityType models.EntityType, limit int) ([]*models.Entity, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("type", "==", string(entityType)).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Entity
	for _, d := range docs {
		var e models.Entity
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *EntityRepository) FindByCanonicalName(ctx context.Context, tenantID, canonicalName string) (*models.Entity, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("canonicalName", "==", canonicalName).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var e models.Entity
	if err := docs[0].DataTo(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// FindByCanonicalPath finds an entity by its canonical path.
// Returns the first entity found with the given canonical path, or nil if not found.
func (r *EntityRepository) FindByCanonicalPath(ctx context.Context, tenantID, canonicalPath string) (*models.Entity, error) {
	if canonicalPath == "" || canonicalPath == "/" {
		return nil, nil
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("canonicalPaths", "array-contains", canonicalPath).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var e models.Entity
	if err := docs[0].DataTo(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// FindDuplicatesByCanonicalPath finds all entities with the same canonical path.
// Useful for detecting and merging duplicates.
func (r *EntityRepository) FindDuplicatesByCanonicalPath(ctx context.Context, tenantID, canonicalPath string) ([]*models.Entity, error) {
	if canonicalPath == "" || canonicalPath == "/" {
		return nil, nil
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("canonicalPaths", "array-contains", canonicalPath)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Entity
	for _, d := range docs {
		var e models.Entity
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// MergeDuplicatesByCanonicalPath merges duplicate entities that share the same canonical path.
// The oldest entity (by CreatedAt) is kept as the primary, and others are marked as deprecated.
// Aliases from duplicate entities are merged into the primary entity.
func (r *EntityRepository) MergeDuplicatesByCanonicalPath(ctx context.Context, tenantID, canonicalPath string) error {
	duplicates, err := r.FindDuplicatesByCanonicalPath(ctx, tenantID, canonicalPath)
	if err != nil {
		return err
	}
	if len(duplicates) <= 1 {
		return nil // No duplicates to merge
	}

	// Find the oldest entity (primary)
	var primary *models.Entity
	for _, e := range duplicates {
		if primary == nil || e.CreatedAt.Before(primary.CreatedAt) {
			primary = e
		}
	}

	// Merge aliases and mark others as deprecated
	aliasMap := make(map[string]bool)
	for _, alias := range primary.Aliases {
		aliasMap[alias] = true
	}

	for _, dup := range duplicates {
		if dup.ID == primary.ID {
			continue
		}

		// Add canonical name and aliases to primary
		if !aliasMap[dup.CanonicalName] {
			primary.Aliases = append(primary.Aliases, dup.CanonicalName)
			aliasMap[dup.CanonicalName] = true
		}
		for _, alias := range dup.Aliases {
			if !aliasMap[alias] {
				primary.Aliases = append(primary.Aliases, alias)
				aliasMap[alias] = true
			}
		}

		// Mark duplicate as deprecated
		dup.Status = "deprecated"
		if err := r.Upsert(ctx, dup); err != nil {
			return err
		}
	}

	// Update primary entity with merged aliases
	return r.Upsert(ctx, primary)
}

// ListBySource находит все entities, созданные из указанного источника
// Ищет по Attributes["sourceId"] и опционально Attributes["sourceType"]
func (r *EntityRepository) ListBySource(ctx context.Context, tenantID, sourceID, sourceType string, limit int) ([]*models.Entity, error) {
	if limit <= 0 {
		limit = 100
	}
	var q firestore.Query
	if sourceType != "" {
		q = r.col.Where("tenantId", "==", tenantID).Where("attributes.sourceId", "==", sourceID).Where("attributes.sourceType", "==", sourceType).OrderBy("createdAt", firestore.Desc).Limit(limit)
	} else {
		q = r.col.Where("tenantId", "==", tenantID).Where("attributes.sourceId", "==", sourceID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Entity
	for _, d := range docs {
		var e models.Entity
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// syncToBigQuery синхронизирует метаданные Entity в BigQuery
func (r *EntityRepository) syncToBigQuery(ctx context.Context, e *models.Entity) {
	if r.bigquerySync == nil || e == nil {
		return
	}

	// Extract sourceId and sourceType from Attributes if present
	sourceID := ""
	sourceType := ""
	if e.Attributes != nil {
		if sid, ok := e.Attributes["sourceId"].(string); ok {
			sourceID = sid
		}
		if st, ok := e.Attributes["sourceType"].(string); ok {
			sourceType = st
		}
	}

	homeLocationName := ""
	if e.HomeLocation != nil {
		homeLocationName = e.HomeLocation.Name
	}

	row := map[string]interface{}{
		"id":            e.ID,
		"tenantId":      e.TenantID,
		"canonicalName": e.CanonicalName,
		"type":          string(e.Type),
		"aliases":       e.Aliases,
		"sourceId":      sourceID,
		"sourceType":    sourceType,
		"homeLocation":  homeLocationName,
		"status":        e.Status,
		"version":       e.Version,
		"createdBy":     e.CreatedBy,
		"updatedBy":     e.UpdatedBy,
		"description":   e.Description,
		"tags":          e.Tags,
		"createdAt":     e.CreatedAt,
		"updatedAt":     e.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "ontology_entities", row); err != nil {
		// Логируем ошибку, но не прерываем выполнение (best effort)
		_ = err
	}
}

func (r *EntityRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Event Repository ---

type EventRepository struct {
	col          *firestore.CollectionRef
	bigquerySync *gcp.BigQueryClient // Опциональная синхронизация с BigQuery
}

func NewEventRepository(db *firestore.Client) *EventRepository {
	return &EventRepository{col: db.Collection("ontology_events")}
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *EventRepository) WithBigQuerySync(bq *gcp.BigQueryClient) *EventRepository {
	r.bigquerySync = bq
	return r
}

func (r *EventRepository) Create(ctx context.Context, e *models.Event) (string, error) {
	// Allow empty TenantID for global/public sources
	if e == nil {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Status == "" {
		e.Status = "active"
	}
	doc := r.col.NewDoc()
	e.ID = doc.ID
	if _, err := doc.Set(ctx, e); err != nil {
		return "", err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(event *models.Event) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, event)
		}(e)
	}

	return e.ID, nil
}

func (r *EventRepository) Upsert(ctx context.Context, e *models.Event) error {
	// Allow empty TenantID for global/public sources
	if e == nil || e.ID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Status == "" {
		e.Status = "active"
	}
	_, err := r.col.Doc(e.ID).Set(ctx, e)
	if err != nil {
		return err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(event *models.Event) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, event)
		}(e)
	}

	return nil
}

func (r *EventRepository) Get(ctx context.Context, id string) (*models.Event, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var e models.Event
	if err := snap.DataTo(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *EventRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Event
	for _, d := range docs {
		var e models.Event
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *EventRepository) ListByTenantAndType(ctx context.Context, tenantID, eventType string, limit int) ([]*models.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("type", "==", eventType).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Event
	for _, d := range docs {
		var e models.Event
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// ListBySource находит все events, созданные из указанного источника
// Ищет по Provenance.SourceRefs (array-contains для sourceId)
func (r *EventRepository) ListBySource(ctx context.Context, tenantID, sourceID, sourceType string, limit int) ([]*models.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	// Firestore не поддерживает прямые запросы по вложенным массивам объектов
	// Поэтому используем client-side фильтрацию после получения по tenantId
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit * 2) // Получаем больше, чтобы после фильтрации осталось достаточно
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Event
	for _, d := range docs {
		var e models.Event
		if err := d.DataTo(&e); err != nil {
			continue
		}
		// Фильтруем по sourceRefs
		for _, ref := range e.Provenance.SourceRefs {
			if ref.SourceID == sourceID && (sourceType == "" || ref.SourceType == sourceType) {
				out = append(out, &e)
				break
			}
		}
		if len(out) >= limit && limit > 0 {
			break
		}
	}
	if len(out) > limit && limit > 0 {
		out = out[:limit]
	}
	return out, nil
}

// syncToBigQuery синхронизирует метаданные Event в BigQuery
func (r *EventRepository) syncToBigQuery(ctx context.Context, e *models.Event) {
	if r.bigquerySync == nil || e == nil {
		return
	}

	// Extract sourceId and sourceType from Provenance.SourceRefs
	sourceID := ""
	sourceType := ""
	if len(e.Provenance.SourceRefs) > 0 {
		sourceID = e.Provenance.SourceRefs[0].SourceID
		sourceType = e.Provenance.SourceRefs[0].SourceType
	}

	locationName := ""
	if e.Location != nil {
		locationName = e.Location.Name
	}

	participantsCount := len(e.Participants)

	row := map[string]interface{}{
		"id":                e.ID,
		"tenantId":          e.TenantID,
		"type":              e.Type,
		"title":             e.Title,
		"occurredAt":        e.OccurredAt,
		"occurredStart":     e.OccurredStart,
		"occurredEnd":       e.OccurredEnd,
		"location":          locationName,
		"participantsCount": participantsCount,
		"outcomeSummary":    e.OutcomeSummary,
		"sourceId":          sourceID,
		"sourceType":        sourceType,
		"confidence":        e.Confidence,
		"status":            e.Status,
		"version":           e.Version,
		"createdBy":         e.CreatedBy,
		"description":       e.Description,
		"createdAt":         e.CreatedAt,
		"updatedAt":         e.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "ontology_events", row); err != nil {
		// Логируем ошибку, но не прерываем выполнение (best effort)
		_ = err
	}
}

func (r *EventRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Relation Repository ---

type RelationRepository struct{ col *firestore.CollectionRef }

func NewRelationRepository(db *firestore.Client) *RelationRepository {
	return &RelationRepository{col: db.Collection("ontology_relations")}
}

func (r *RelationRepository) Create(ctx context.Context, rel *models.Relation) (string, error) {
	if rel == nil || rel.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if rel.CreatedAt.IsZero() {
		rel.CreatedAt = now
	}
	rel.UpdatedAt = now
	if rel.Version == 0 {
		rel.Version = 1
	}
	if rel.Status == "" {
		rel.Status = "active"
	}
	doc := r.col.NewDoc()
	rel.ID = doc.ID
	if _, err := doc.Set(ctx, rel); err != nil {
		return "", err
	}
	return rel.ID, nil
}

func (r *RelationRepository) Upsert(ctx context.Context, rel *models.Relation) error {
	if rel == nil || rel.ID == "" || rel.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if rel.CreatedAt.IsZero() {
		rel.CreatedAt = now
	}
	rel.UpdatedAt = now
	if rel.Version == 0 {
		rel.Version = 1
	}
	if rel.Status == "" {
		rel.Status = "active"
	}
	_, err := r.col.Doc(rel.ID).Set(ctx, rel)
	return err
}

func (r *RelationRepository) Get(ctx context.Context, id string) (*models.Relation, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var rel models.Relation
	if err := snap.DataTo(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *RelationRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Relation, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Relation
	for _, d := range docs {
		var rel models.Relation
		if err := d.DataTo(&rel); err != nil {
			continue
		}
		out = append(out, &rel)
	}
	return out, nil
}

func (r *RelationRepository) ListBySubject(ctx context.Context, tenantID, subjectID string, limit int) ([]*models.Relation, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("subjectId", "==", subjectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Relation
	for _, d := range docs {
		var rel models.Relation
		if err := d.DataTo(&rel); err != nil {
			continue
		}
		out = append(out, &rel)
	}
	return out, nil
}

func (r *RelationRepository) ListByObject(ctx context.Context, tenantID, objectID string, limit int) ([]*models.Relation, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("objectId", "==", objectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Relation
	for _, d := range docs {
		var rel models.Relation
		if err := d.DataTo(&rel); err != nil {
			continue
		}
		out = append(out, &rel)
	}
	return out, nil
}

func (r *RelationRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Claim Repository ---

type ClaimRepository struct {
	col          *firestore.CollectionRef
	bigquerySync *gcp.BigQueryClient // Опциональная синхронизация с BigQuery
}

func NewClaimRepository(db *firestore.Client) *ClaimRepository {
	return &ClaimRepository{col: db.Collection("ontology_claims")}
}

// WithBigQuerySync настраивает автоматическую синхронизацию метаданных с BigQuery
func (r *ClaimRepository) WithBigQuerySync(bq *gcp.BigQueryClient) *ClaimRepository {
	r.bigquerySync = bq
	return r
}

func (r *ClaimRepository) Create(ctx context.Context, c *models.Claim) (string, error) {
	// Allow empty TenantID for global/public sources
	if c == nil {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Status == "" {
		c.Status = "active"
	}
	doc := r.col.NewDoc()
	c.ID = doc.ID
	if _, err := doc.Set(ctx, c); err != nil {
		return "", err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(claim *models.Claim) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, claim)
		}(c)
	}

	return c.ID, nil
}

func (r *ClaimRepository) Upsert(ctx context.Context, c *models.Claim) error {
	// Allow empty TenantID for global/public sources
	if c == nil || c.ID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Status == "" {
		c.Status = "active"
	}
	_, err := r.col.Doc(c.ID).Set(ctx, c)
	if err != nil {
		return err
	}

	// Синхронизация с BigQuery асинхронно (best effort, не блокируем основной поток)
	if r.bigquerySync != nil {
		go func(claim *models.Claim) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r.syncToBigQuery(syncCtx, claim)
		}(c)
	}

	return nil
}

func (r *ClaimRepository) Get(ctx context.Context, id string) (*models.Claim, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var c models.Claim
	if err := snap.DataTo(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ClaimRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Claim, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Claim
	for _, d := range docs {
		var c models.Claim
		if err := d.DataTo(&c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

func (r *ClaimRepository) ListBySubject(ctx context.Context, tenantID, subjectID string, limit int) ([]*models.Claim, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("subjectRef.id", "==", subjectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Claim
	for _, d := range docs {
		var c models.Claim
		if err := d.DataTo(&c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

// ListBySource находит все claims, созданные из указанного источника
// Ищет по Provenance.SourceRefs (array-contains для sourceId)
func (r *ClaimRepository) ListBySource(ctx context.Context, tenantID, sourceID, sourceType string, limit int) ([]*models.Claim, error) {
	if limit <= 0 {
		limit = 100
	}
	// Firestore не поддерживает прямые запросы по вложенным массивам объектов
	// Поэтому используем client-side фильтрацию после получения по tenantId
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc)
	if limit > 0 {
		q = q.Limit(limit * 2) // Получаем больше, чтобы после фильтрации осталось достаточно
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Claim
	for _, d := range docs {
		var c models.Claim
		if err := d.DataTo(&c); err != nil {
			continue
		}
		// Фильтруем по sourceRefs
		for _, ref := range c.Provenance.SourceRefs {
			if ref.SourceID == sourceID && (sourceType == "" || ref.SourceType == sourceType) {
				out = append(out, &c)
				break
			}
		}
		if len(out) >= limit && limit > 0 {
			break
		}
	}
	if len(out) > limit && limit > 0 {
		out = out[:limit]
	}
	return out, nil
}

// syncToBigQuery синхронизирует метаданные Claim в BigQuery
func (r *ClaimRepository) syncToBigQuery(ctx context.Context, c *models.Claim) {
	if r.bigquerySync == nil || c == nil {
		return
	}

	// Extract sourceId and sourceType from Provenance.SourceRefs
	sourceID := ""
	sourceType := ""
	if len(c.Provenance.SourceRefs) > 0 {
		sourceID = c.Provenance.SourceRefs[0].SourceID
		sourceType = c.Provenance.SourceRefs[0].SourceType
	}

	objectID := ""
	if c.ObjectRef.ID != nil {
		objectID = *c.ObjectRef.ID
	}

	row := map[string]interface{}{
		"id":          c.ID,
		"tenantId":    c.TenantID,
		"subjectId":   c.SubjectRef.ID,
		"subjectKind": string(c.SubjectRef.Kind),
		"predicate":   c.Predicate,
		"objectKind":  string(c.ObjectRef.Kind),
		"objectId":    objectID,
		"stance":      string(c.Stance),
		"confidence":  c.Confidence,
		"sourceId":    sourceID,
		"sourceType":  sourceType,
		"method":      string(c.Provenance.Method),
		"extractedAt": c.Provenance.ExtractedAt,
		"status":      c.Status,
		"version":     c.Version,
		"createdBy":   c.CreatedBy,
		"description": c.Description,
		"createdAt":   c.CreatedAt,
		"updatedAt":   c.UpdatedAt,
	}

	if err := r.bigquerySync.InsertRow(ctx, "ontology_claims", row); err != nil {
		// Логируем ошибку, но не прерываем выполнение (best effort)
		_ = err
	}
}

func (r *ClaimRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// TODO: Add BigQuery sync for remaining ontology artifacts:
// - Concept (concepts/domains/processes)
// - Relation (static relationships between entities/concepts)
// - MetricState (quantitative metrics)
// - Trend (aggregated dynamics)
// - CausalLink (causal relationships)
// - Scenario (possible future developments)
// - ActionableItem (actionable recommendations)
// - Source (data sources)
// - Trust (trust models for sources)
// - UncertaintyModel (uncertainty tracking)
// These artifacts are not automatically extracted from documents but may be created manually or derived.
// When implementing, follow the same pattern as Entity/Event/Claim repositories.

// --- Concept Repository ---

type ConceptRepository struct{ col *firestore.CollectionRef }

func NewConceptRepository(db *firestore.Client) *ConceptRepository {
	return &ConceptRepository{col: db.Collection("ontology_concepts")}
}

func (r *ConceptRepository) Create(ctx context.Context, c *models.Concept) (string, error) {
	if c == nil || c.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Status == "" {
		c.Status = "active"
	}
	doc := r.col.NewDoc()
	c.ID = doc.ID
	if _, err := doc.Set(ctx, c); err != nil {
		return "", err
	}
	return c.ID, nil
}

func (r *ConceptRepository) Upsert(ctx context.Context, c *models.Concept) error {
	if c == nil || c.ID == "" || c.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Status == "" {
		c.Status = "active"
	}
	_, err := r.col.Doc(c.ID).Set(ctx, c)
	return err
}

func (r *ConceptRepository) Get(ctx context.Context, id string) (*models.Concept, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var c models.Concept
	if err := snap.DataTo(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConceptRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Concept, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Concept
	for _, d := range docs {
		var c models.Concept
		if err := d.DataTo(&c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

func (r *ConceptRepository) ListByTenantAndKind(ctx context.Context, tenantID string, kind models.ConceptKind, limit int) ([]*models.Concept, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("kind", "==", string(kind)).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Concept
	for _, d := range docs {
		var c models.Concept
		if err := d.DataTo(&c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

func (r *ConceptRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- MetricState Repository ---

type MetricStateRepository struct{ col *firestore.CollectionRef }

func NewMetricStateRepository(db *firestore.Client) *MetricStateRepository {
	return &MetricStateRepository{col: db.Collection("ontology_metric_states")}
}

func (r *MetricStateRepository) Create(ctx context.Context, m *models.MetricState) (string, error) {
	if m == nil || m.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Status == "" {
		m.Status = "active"
	}
	doc := r.col.NewDoc()
	m.ID = doc.ID
	if _, err := doc.Set(ctx, m); err != nil {
		return "", err
	}
	return m.ID, nil
}

func (r *MetricStateRepository) Upsert(ctx context.Context, m *models.MetricState) error {
	if m == nil || m.ID == "" || m.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Status == "" {
		m.Status = "active"
	}
	_, err := r.col.Doc(m.ID).Set(ctx, m)
	return err
}

func (r *MetricStateRepository) Get(ctx context.Context, id string) (*models.MetricState, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var m models.MetricState
	if err := snap.DataTo(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *MetricStateRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.MetricState, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.MetricState
	for _, d := range docs {
		var m models.MetricState
		if err := d.DataTo(&m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	return out, nil
}

func (r *MetricStateRepository) ListBySubject(ctx context.Context, tenantID, subjectID string, limit int) ([]*models.MetricState, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("subjectRef.id", "==", subjectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.MetricState
	for _, d := range docs {
		var m models.MetricState
		if err := d.DataTo(&m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	return out, nil
}

func (r *MetricStateRepository) ListByMetricName(ctx context.Context, tenantID, metricName string, limit int) ([]*models.MetricState, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("metricName", "==", metricName).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.MetricState
	for _, d := range docs {
		var m models.MetricState
		if err := d.DataTo(&m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	return out, nil
}

func (r *MetricStateRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Trend Repository ---

type TrendRepository struct{ col *firestore.CollectionRef }

func NewTrendRepository(db *firestore.Client) *TrendRepository {
	return &TrendRepository{col: db.Collection("ontology_trends")}
}

func (r *TrendRepository) Create(ctx context.Context, t *models.Trend) (string, error) {
	if t == nil || t.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Version == 0 {
		t.Version = 1
	}
	if t.Status == "" {
		t.Status = "active"
	}
	doc := r.col.NewDoc()
	t.ID = doc.ID
	if _, err := doc.Set(ctx, t); err != nil {
		return "", err
	}
	return t.ID, nil
}

func (r *TrendRepository) Upsert(ctx context.Context, t *models.Trend) error {
	if t == nil || t.ID == "" || t.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Version == 0 {
		t.Version = 1
	}
	if t.Status == "" {
		t.Status = "active"
	}
	_, err := r.col.Doc(t.ID).Set(ctx, t)
	return err
}

func (r *TrendRepository) Get(ctx context.Context, id string) (*models.Trend, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var t models.Trend
	if err := snap.DataTo(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TrendRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Trend, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Trend
	for _, d := range docs {
		var t models.Trend
		if err := d.DataTo(&t); err != nil {
			continue
		}
		out = append(out, &t)
	}
	return out, nil
}

func (r *TrendRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- CausalLink Repository ---

type CausalLinkRepository struct{ col *firestore.CollectionRef }

func NewCausalLinkRepository(db *firestore.Client) *CausalLinkRepository {
	return &CausalLinkRepository{col: db.Collection("ontology_causal_links")}
}

func (r *CausalLinkRepository) Create(ctx context.Context, cl *models.CausalLink) (string, error) {
	if cl == nil || cl.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if cl.CreatedAt.IsZero() {
		cl.CreatedAt = now
	}
	cl.UpdatedAt = now
	if cl.Version == 0 {
		cl.Version = 1
	}
	if cl.Status == "" {
		cl.Status = "active"
	}
	doc := r.col.NewDoc()
	cl.ID = doc.ID
	if _, err := doc.Set(ctx, cl); err != nil {
		return "", err
	}
	return cl.ID, nil
}

func (r *CausalLinkRepository) Upsert(ctx context.Context, cl *models.CausalLink) error {
	if cl == nil || cl.ID == "" || cl.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if cl.CreatedAt.IsZero() {
		cl.CreatedAt = now
	}
	cl.UpdatedAt = now
	if cl.Version == 0 {
		cl.Version = 1
	}
	if cl.Status == "" {
		cl.Status = "active"
	}
	_, err := r.col.Doc(cl.ID).Set(ctx, cl)
	return err
}

func (r *CausalLinkRepository) Get(ctx context.Context, id string) (*models.CausalLink, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var cl models.CausalLink
	if err := snap.DataTo(&cl); err != nil {
		return nil, err
	}
	return &cl, nil
}

func (r *CausalLinkRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.CausalLink, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.CausalLink
	for _, d := range docs {
		var cl models.CausalLink
		if err := d.DataTo(&cl); err != nil {
			continue
		}
		out = append(out, &cl)
	}
	return out, nil
}

func (r *CausalLinkRepository) ListByCause(ctx context.Context, tenantID, causeID string, limit int) ([]*models.CausalLink, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("causeRef.id", "==", causeID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.CausalLink
	for _, d := range docs {
		var cl models.CausalLink
		if err := d.DataTo(&cl); err != nil {
			continue
		}
		out = append(out, &cl)
	}
	return out, nil
}

func (r *CausalLinkRepository) ListByEffect(ctx context.Context, tenantID, effectID string, limit int) ([]*models.CausalLink, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("effectRef.id", "==", effectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.CausalLink
	for _, d := range docs {
		var cl models.CausalLink
		if err := d.DataTo(&cl); err != nil {
			continue
		}
		out = append(out, &cl)
	}
	return out, nil
}

func (r *CausalLinkRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Scenario Repository ---

type ScenarioRepository struct{ col *firestore.CollectionRef }

func NewScenarioRepository(db *firestore.Client) *ScenarioRepository {
	return &ScenarioRepository{col: db.Collection("ontology_scenarios")}
}

func (r *ScenarioRepository) Create(ctx context.Context, s *models.Scenario) (string, error) {
	if s == nil || s.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Status == "" {
		s.Status = "active"
	}
	doc := r.col.NewDoc()
	s.ID = doc.ID
	if _, err := doc.Set(ctx, s); err != nil {
		return "", err
	}
	return s.ID, nil
}

func (r *ScenarioRepository) Upsert(ctx context.Context, s *models.Scenario) error {
	if s == nil || s.ID == "" || s.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Status == "" {
		s.Status = "active"
	}
	_, err := r.col.Doc(s.ID).Set(ctx, s)
	return err
}

func (r *ScenarioRepository) Get(ctx context.Context, id string) (*models.Scenario, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var s models.Scenario
	if err := snap.DataTo(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *ScenarioRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Scenario, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Scenario
	for _, d := range docs {
		var s models.Scenario
		if err := d.DataTo(&s); err != nil {
			continue
		}
		out = append(out, &s)
	}
	return out, nil
}

func (r *ScenarioRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- ActionableItem Repository ---

type ActionableItemRepository struct{ col *firestore.CollectionRef }

func NewActionableItemRepository(db *firestore.Client) *ActionableItemRepository {
	return &ActionableItemRepository{col: db.Collection("ontology_actionable_items")}
}

func (r *ActionableItemRepository) Create(ctx context.Context, ai *models.ActionableItem) (string, error) {
	if ai == nil || ai.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if ai.CreatedAt.IsZero() {
		ai.CreatedAt = now
	}
	ai.UpdatedAt = now
	if ai.Version == 0 {
		ai.Version = 1
	}
	if ai.Status == "" {
		ai.Status = models.ActionableStatusProposed
	}
	doc := r.col.NewDoc()
	ai.ID = doc.ID
	if _, err := doc.Set(ctx, ai); err != nil {
		return "", err
	}
	return ai.ID, nil
}

func (r *ActionableItemRepository) Upsert(ctx context.Context, ai *models.ActionableItem) error {
	if ai == nil || ai.ID == "" || ai.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if ai.CreatedAt.IsZero() {
		ai.CreatedAt = now
	}
	ai.UpdatedAt = now
	if ai.Version == 0 {
		ai.Version = 1
	}
	if ai.Status == "" {
		ai.Status = models.ActionableStatusProposed
	}
	_, err := r.col.Doc(ai.ID).Set(ctx, ai)
	return err
}

func (r *ActionableItemRepository) Get(ctx context.Context, id string) (*models.ActionableItem, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var ai models.ActionableItem
	if err := snap.DataTo(&ai); err != nil {
		return nil, err
	}
	return &ai, nil
}

func (r *ActionableItemRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.ActionableItem, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.ActionableItem
	for _, d := range docs {
		var ai models.ActionableItem
		if err := d.DataTo(&ai); err != nil {
			continue
		}
		out = append(out, &ai)
	}
	return out, nil
}

func (r *ActionableItemRepository) ListByStatus(ctx context.Context, tenantID string, status models.ActionableStatus, limit int) ([]*models.ActionableItem, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("status", "==", string(status)).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.ActionableItem
	for _, d := range docs {
		var ai models.ActionableItem
		if err := d.DataTo(&ai); err != nil {
			continue
		}
		out = append(out, &ai)
	}
	return out, nil
}

func (r *ActionableItemRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Source Repository ---

type SourceRepository struct{ col *firestore.CollectionRef }

func NewSourceRepository(db *firestore.Client) *SourceRepository {
	return &SourceRepository{col: db.Collection("ontology_sources")}
}

func (r *SourceRepository) Create(ctx context.Context, s *models.Source) (string, error) {
	// Allow empty TenantID for global/public sources
	if s == nil {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Status == "" {
		s.Status = "active"
	}
	doc := r.col.NewDoc()
	s.ID = doc.ID
	if _, err := doc.Set(ctx, s); err != nil {
		return "", err
	}
	return s.ID, nil
}

func (r *SourceRepository) Upsert(ctx context.Context, s *models.Source) error {
	if s == nil || s.ID == "" || s.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Status == "" {
		s.Status = "active"
	}
	_, err := r.col.Doc(s.ID).Set(ctx, s)
	return err
}

func (r *SourceRepository) Get(ctx context.Context, id string) (*models.Source, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var s models.Source
	if err := snap.DataTo(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SourceRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Source, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Source
	for _, d := range docs {
		var s models.Source
		if err := d.DataTo(&s); err != nil {
			continue
		}
		out = append(out, &s)
	}
	return out, nil
}

func (r *SourceRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Trust Repository ---

type TrustRepository struct{ col *firestore.CollectionRef }

func NewTrustRepository(db *firestore.Client) *TrustRepository {
	return &TrustRepository{col: db.Collection("ontology_trusts")}
}

func (r *TrustRepository) Create(ctx context.Context, t *models.Trust) (string, error) {
	if t == nil || t.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Version == 0 {
		t.Version = 1
	}
	if t.Status == "" {
		t.Status = "active"
	}
	doc := r.col.NewDoc()
	t.ID = doc.ID
	if _, err := doc.Set(ctx, t); err != nil {
		return "", err
	}
	return t.ID, nil
}

func (r *TrustRepository) Upsert(ctx context.Context, t *models.Trust) error {
	if t == nil || t.ID == "" || t.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Version == 0 {
		t.Version = 1
	}
	if t.Status == "" {
		t.Status = "active"
	}
	_, err := r.col.Doc(t.ID).Set(ctx, t)
	return err
}

func (r *TrustRepository) Get(ctx context.Context, id string) (*models.Trust, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var t models.Trust
	if err := snap.DataTo(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TrustRepository) GetBySourceID(ctx context.Context, tenantID, sourceID string) (*models.Trust, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var t models.Trust
	if err := docs[0].DataTo(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TrustRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.Trust, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.Trust
	for _, d := range docs {
		var t models.Trust
		if err := d.DataTo(&t); err != nil {
			continue
		}
		out = append(out, &t)
	}
	return out, nil
}

func (r *TrustRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- UncertaintyModel Repository ---

type UncertaintyModelRepository struct{ col *firestore.CollectionRef }

func NewUncertaintyModelRepository(db *firestore.Client) *UncertaintyModelRepository {
	return &UncertaintyModelRepository{col: db.Collection("ontology_uncertainty_models")}
}

func (r *UncertaintyModelRepository) Create(ctx context.Context, um *models.UncertaintyModel) (string, error) {
	if um == nil || um.TenantID == "" {
		return "", ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if um.CreatedAt.IsZero() {
		um.CreatedAt = now
	}
	um.UpdatedAt = now
	if um.Version == 0 {
		um.Version = 1
	}
	if um.Status == "" {
		um.Status = models.UncertaintyStatusOpen
	}
	doc := r.col.NewDoc()
	um.ID = doc.ID
	if _, err := doc.Set(ctx, um); err != nil {
		return "", err
	}
	return um.ID, nil
}

func (r *UncertaintyModelRepository) Upsert(ctx context.Context, um *models.UncertaintyModel) error {
	if um == nil || um.ID == "" || um.TenantID == "" {
		return ErrInvalidOntologyDocument
	}
	now := time.Now().UTC()
	if um.CreatedAt.IsZero() {
		um.CreatedAt = now
	}
	um.UpdatedAt = now
	if um.Version == 0 {
		um.Version = 1
	}
	if um.Status == "" {
		um.Status = models.UncertaintyStatusOpen
	}
	_, err := r.col.Doc(um.ID).Set(ctx, um)
	return err
}

func (r *UncertaintyModelRepository) Get(ctx context.Context, id string) (*models.UncertaintyModel, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var um models.UncertaintyModel
	if err := snap.DataTo(&um); err != nil {
		return nil, err
	}
	return &um, nil
}

func (r *UncertaintyModelRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.UncertaintyModel, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.UncertaintyModel
	for _, d := range docs {
		var um models.UncertaintyModel
		if err := d.DataTo(&um); err != nil {
			continue
		}
		out = append(out, &um)
	}
	return out, nil
}

func (r *UncertaintyModelRepository) ListByStatus(ctx context.Context, tenantID string, status models.UncertaintyStatus, limit int) ([]*models.UncertaintyModel, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("status", "==", string(status)).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.UncertaintyModel
	for _, d := range docs {
		var um models.UncertaintyModel
		if err := d.DataTo(&um); err != nil {
			continue
		}
		out = append(out, &um)
	}
	return out, nil
}

func (r *UncertaintyModelRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}
