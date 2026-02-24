package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// FactCoordinateEnricher resolves coordinates for facts (GeoNames, Geocoding, fragment lookup).
type FactCoordinateEnricher interface {
	Enrich(ctx context.Context, f *models.OntologyFact) (bool, error)
}

// OntologyFactsRepository stores unified ontology facts and optionally syncs them to GCS + Vertex AI Search.
type OntologyFactsRepository struct {
	col              *firestore.CollectionRef
	storage          *gcp.GCSClient
	bucket           string
	useGCS           bool
	vertexSearchSync func(ctx context.Context, tenantID string) (operationName string, err error)
	coordEnricher    FactCoordinateEnricher
}

func NewOntologyFactsRepository(db *firestore.Client) *OntologyFactsRepository {
	return &OntologyFactsRepository{
		col: db.Collection("ontology_facts"),
	}
}

func (r *OntologyFactsRepository) WithStorage(storage *gcp.GCSClient, bucket string) *OntologyFactsRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && bucket != ""
	return r
}

// WithVertexSearchSync configures async Vertex AI Search import after GCS upload.
func (r *OntologyFactsRepository) WithVertexSearchSync(syncFn func(ctx context.Context, tenantID string) (operationName string, err error)) *OntologyFactsRepository {
	r.vertexSearchSync = syncFn
	return r
}

// WithCoordinateEnricher wires GeoNames/Geocoding/fragment lookup for fact coordinates (same as fragments).
func (r *OntologyFactsRepository) WithCoordinateEnricher(e FactCoordinateEnricher) *OntologyFactsRepository {
	r.coordEnricher = e
	return r
}

func (r *OntologyFactsRepository) Create(ctx context.Context, f *models.OntologyFact) (string, error) {
	if f == nil {
		return "", ErrInvalidOntologyDocument
	}
	if r.coordEnricher != nil {
		if _, err := r.coordEnricher.Enrich(ctx, f); err != nil {
			log.Printf("[OntologyFactsRepo] coordinate enrichment failed: %v", err)
		}
	}
	now := time.Now().UTC()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	if f.Version == 0 {
		f.Version = 1
	}
	if f.Status == "" {
		f.Status = "active"
	}
	if f.VectorText == "" {
		f.VectorText = f.GenerateVectorText()
	}

	doc := r.col.NewDoc()
	f.ID = doc.ID
	if _, err := doc.Set(ctx, f); err != nil {
		return "", err
	}

	// Best effort: export to GCS and trigger Vertex import
	if err := r.exportToGCSAndSync(ctx, f); err != nil {
		// Do not fail the main write
		log.Printf("[OntologyFactsRepo] WARNING: export/sync failed: %v", err)
	}

	return f.ID, nil
}

func (r *OntologyFactsRepository) Upsert(ctx context.Context, f *models.OntologyFact) error {
	if f == nil || f.ID == "" {
		return ErrInvalidOntologyDocument
	}
	if r.coordEnricher != nil {
		if _, err := r.coordEnricher.Enrich(ctx, f); err != nil {
			log.Printf("[OntologyFactsRepo] coordinate enrichment failed: %v", err)
		}
	}
	now := time.Now().UTC()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	if f.Version == 0 {
		f.Version = 1
	}
	if f.Status == "" {
		f.Status = "active"
	}
	if f.VectorText == "" {
		f.VectorText = f.GenerateVectorText()
	}

	if _, err := r.col.Doc(f.ID).Set(ctx, f); err != nil {
		return err
	}

	if err := r.exportToGCSAndSync(ctx, f); err != nil {
		log.Printf("[OntologyFactsRepo] WARNING: export/sync failed: %v", err)
	}
	return nil
}

func (r *OntologyFactsRepository) Get(ctx context.Context, id string) (*models.OntologyFact, error) {
	if id == "" {
		return nil, ErrInvalidOntologyDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var f models.OntologyFact
	if err := snap.DataTo(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListUpdatedSinceByCanonicalPath returns facts updated after 'since' and linked to the given canonicalPath.
func (r *OntologyFactsRepository) ListUpdatedSinceByCanonicalPath(ctx context.Context, tenantID string, canonicalPath string, since time.Time, limit int) ([]*models.OntologyFact, error) {
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 100
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return nil, fmt.Errorf("canonicalPath is required")
	}

	// Build query: tenantId == X AND canonicalPaths array-contains Y
	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("canonicalPaths", "array-contains", canonicalPath)

	// CRITICAL: When since is zero time, don't use updatedAt filter to avoid index issues.
	// Zero time means "get all facts", which can be slow but should work with existing index.
	// When since is provided, use the range filter with OrderBy (requires composite index).
	if !since.IsZero() {
		q = q.Where("updatedAt", ">", since).OrderBy("updatedAt", firestore.Asc)
	} else {
		// For zero time, order by updatedAt but without range filter (uses index: tenantId, canonicalPaths, updatedAt)
		q = q.OrderBy("updatedAt", firestore.Desc) // Desc to get most recent first
	}
	q = q.Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		// Log detailed error for debugging Firestore index issues
		fmt.Printf("[OntologyFacts] ListUpdatedSinceByCanonicalPath failed: tenantID=%s, canonicalPath=%s, since=%v (zero=%v), limit=%d, error=%v\n",
			tenantID, canonicalPath, since, since.IsZero(), limit, err)
		return nil, fmt.Errorf("firestore query failed (may need composite index): %w", err)
	}
	out := make([]*models.OntologyFact, 0, len(docs))
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}

// ListByCanonicalPath returns facts linked to the given canonicalPath.
// Facts can relate to multiple canonical paths (CanonicalPaths array); this query finds any fact
// where canonicalPaths array-contains the given path. Use kind to filter by fact kind (event, claim, signal, etc.).
func (r *OntologyFactsRepository) ListByCanonicalPath(ctx context.Context, tenantID string, canonicalPath string, kind string, limit int) ([]*models.OntologyFact, error) {
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 100
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return nil, fmt.Errorf("canonicalPath is required")
	}
	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("canonicalPaths", "array-contains", canonicalPath)
	if kind != "" {
		q = q.Where("kind", "==", strings.TrimSpace(kind))
	}
	q = q.OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.OntologyFact, 0, len(docs))
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}

// ListByCanonicalPathAndKind returns facts linked to the given canonicalPath with specific kind.
// This is optimized for signal queries (kind="signal").
func (r *OntologyFactsRepository) ListByCanonicalPathAndKind(ctx context.Context, tenantID string, canonicalPath string, kind string, limit int) ([]*models.OntologyFact, error) {
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 1000
	}
	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("canonicalPaths", "array-contains", canonicalPath).
		Where("kind", "==", kind).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.OntologyFact, 0, len(docs))
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}

// ListByTenantAndKind returns facts of a given kind for a tenant, ordered by createdAt desc.
// Used by RSS-first tools to list events after extraction.
func (r *OntologyFactsRepository) ListByTenantAndKind(ctx context.Context, tenantID, kind string, limit int) ([]*models.OntologyFact, error) {
	if tenantID == "" {
		tenantID = "global"
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("kind is required")
	}
	if limit <= 0 {
		limit = 100
	}
	// Use updatedAt for ordering (matches existing composite index: tenantId, kind, updatedAt)
	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", kind).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.OntologyFact, 0, len(docs))
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}

// ListByEventIDAndKind returns facts associated with a specific source event and kind.
// This is used for idempotency/dedup in signal pipelines (same event should not emit same wave repeatedly).
func (r *OntologyFactsRepository) ListByEventIDAndKind(ctx context.Context, tenantID, eventID, kind string, limit int) ([]*models.OntologyFact, error) {
	if tenantID == "" {
		tenantID = "global"
	}
	eventID = strings.TrimSpace(eventID)
	kind = strings.TrimSpace(kind)
	if eventID == "" || kind == "" {
		return nil, fmt.Errorf("eventID and kind are required")
	}
	if limit <= 0 {
		limit = 200
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("eventId", "==", eventID).
		Where("kind", "==", kind).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.OntologyFact, 0, len(docs))
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		out = append(out, &f)
	}
	return out, nil
}

// ListCanonicalPathsBySpherePath returns a deduplicated list of canonicalPaths for facts
// that contain the given exact spherePath in their spherePaths.
//
// Notes:
//   - This intentionally avoids adding extra Firestore filters (tenant/status) to not require composite indexes.
//     Filtering is done in-memory.
func (r *OntologyFactsRepository) ListCanonicalPathsBySpherePath(ctx context.Context, tenantID string, spherePath string, limit int) ([]string, error) {
	if strings.TrimSpace(spherePath) == "" {
		return nil, fmt.Errorf("spherePath is required")
	}
	if limit <= 0 {
		limit = 2000
	}
	q := r.col.Where("spherePaths", "array-contains", strings.TrimSpace(spherePath)).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, d := range docs {
		var f models.OntologyFact
		if err := d.DataTo(&f); err != nil {
			continue
		}
		if tenantID != "" && tenantID != f.TenantID {
			continue
		}
		if strings.TrimSpace(f.Status) != "" && strings.ToLower(strings.TrimSpace(f.Status)) != "active" {
			continue
		}
		for _, cp := range f.CanonicalPaths {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				continue
			}
			seen[cp] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for cp := range seen {
		out = append(out, cp)
	}
	return out, nil
}

func (r *OntologyFactsRepository) getObjectPath(f *models.OntologyFact) string {
	// Format: {tenantId}/ontology_facts/{factId}.json
	return fmt.Sprintf("%s/ontology_facts/%s.json", f.TenantID, f.ID)
}

func (r *OntologyFactsRepository) getStoragePath(f *models.OntologyFact) string {
	return fmt.Sprintf("gs://%s/%s", r.bucket, r.getObjectPath(f))
}

// vertexDocumentJSON is the JSON format expected by Discovery Engine dataSchema=document.
type vertexDocumentJSON struct {
	ID         string                 `json:"id"`
	Content    map[string]interface{} `json:"content"`
	StructData map[string]interface{} `json:"structData"`
}

func (r *OntologyFactsRepository) exportToGCSAndSync(ctx context.Context, f *models.OntologyFact) error {
	if !r.useGCS || r.storage == nil || r.bucket == "" || f == nil {
		return nil
	}
	if f.ID == "" || f.TenantID == "" {
		return nil
	}

	doc := vertexDocumentJSON{
		ID: f.ID,
		Content: map[string]interface{}{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(f.VectorText)),
		},
		StructData: map[string]interface{}{
			"type":            "ontology_fact",
			"tenantId":        f.TenantID,
			"kind":            f.Kind,
			"factType":        f.Type,
			"title":           f.Title,
			"confidence":      f.Confidence,
			"updatedAt":       f.UpdatedAt.UTC().Format(time.RFC3339),
			"updatedAtUnixMs": f.UpdatedAt.UTC().UnixMilli(),
			"canonicalPaths":  f.CanonicalPaths,
			"actorPaths":      f.ActorPaths,
			"spherePaths":     f.SpherePaths,
			"sourceIds":       f.SourceIDs,
			"metricUpdates": func() []map[string]interface{} {
				if len(f.MetricUpdates) == 0 {
					return nil
				}
				out := make([]map[string]interface{}, 0, len(f.MetricUpdates))
				for _, mu := range f.MetricUpdates {
					if strings.TrimSpace(mu.MetricName) == "" {
						continue
					}
					m := map[string]interface{}{
						"metricName": strings.TrimSpace(mu.MetricName),
						"value":      mu.Value,
						"confidence": mu.Confidence,
					}
					if mu.Unit != nil && strings.TrimSpace(*mu.Unit) != "" {
						m["unit"] = strings.TrimSpace(*mu.Unit)
					}
					if mu.ObservedAt != nil {
						m["observedAt"] = mu.ObservedAt.UTC().Format(time.RFC3339)
						m["observedAtUnixMs"] = mu.ObservedAt.UTC().UnixMilli()
					}
					out = append(out, m)
				}
				if len(out) == 0 {
					return nil
				}
				return out
			}(),
		},
	}
	if f.LocationPath != nil {
		doc.StructData["locationPath"] = *f.LocationPath
	}
	if f.EventPlace != nil && strings.TrimSpace(*f.EventPlace) != "" {
		doc.StructData["eventPlace"] = strings.TrimSpace(*f.EventPlace)
	}
	if f.EventAddress != nil && strings.TrimSpace(*f.EventAddress) != "" {
		doc.StructData["eventAddress"] = strings.TrimSpace(*f.EventAddress)
	}
	if len(f.CanonicalNames) > 0 {
		doc.StructData["canonicalNames"] = f.CanonicalNames
	}
	if f.Coordinates != nil {
		doc.StructData["coordinates"] = map[string]interface{}{
			"lat": f.Coordinates.Lat,
			"lon": f.Coordinates.Lon,
		}
	}
	if f.OccurredAt != nil {
		doc.StructData["occurredAt"] = f.OccurredAt.UTC().Format(time.RFC3339)
		doc.StructData["occurredAtUnixMs"] = f.OccurredAt.UTC().UnixMilli()
	}
	if f.ObservedAt != nil {
		doc.StructData["observedAt"] = f.ObservedAt.UTC().Format(time.RFC3339)
		doc.StructData["observedAtUnixMs"] = f.ObservedAt.UTC().UnixMilli()
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal vertex doc: %w", err)
	}

	objectPath := r.getObjectPath(f)
	meta := map[string]string{
		"tenantId": f.TenantID,
		"type":     "ontology_fact",
		"factId":   f.ID,
	}
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, body, "application/json", meta); err != nil {
		return fmt.Errorf("upload to gcs: %w", err)
	}

	_ = r.getStoragePath(f) // keep for future linkage if needed

	if r.vertexSearchSync != nil {
		go func(tenantID string) {
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := r.vertexSearchSync(syncCtx, tenantID); err != nil {
				log.Printf("[OntologyFactsRepo] vertex sync failed: %v", err)
			}
		}(f.TenantID)
	}
	return nil
}
