package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrLensArtifactNotFound = errors.New("lens artifact not found")

// LensArtifactsRepository manages LensArtifact documents in Firestore.
//
// Collection: horizon_lens_artifacts
//
// Required Firestore composite indexes:
//  1. (lensId ASC, addedAt DESC) — list artifacts by lens, ordered by time
//  2. (lensId ASC, type ASC, addedAt DESC) — list fragments or edges only
//  3. (lensId ASC, processedForStrategicAt ASC) — find unprocessed artifacts
//  4. (lensId ASC, canonicalPath ASC) — dedup fragments by path
//  5. (lensId ASC, edgeId ASC) — dedup edges
//  6. (fragmentId ASC) — reverse lookup: which lenses contain this fragment
//  7. (tenantId ASC, lensId ASC, addedAt DESC) — tenant-scoped queries
type LensArtifactsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

// NewLensArtifactsRepository creates a new repository.
func NewLensArtifactsRepository(db *firestore.Client) *LensArtifactsRepository {
	return &LensArtifactsRepository{
		db:  db,
		col: db.Collection("horizon_lens_artifacts"),
	}
}

// Create creates a new LensArtifact.
func (r *LensArtifactsRepository) Create(ctx context.Context, artifact *models.LensArtifact) (string, error) {
	if artifact == nil {
		return "", ErrInvalidDocument
	}
	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	if artifact.AddedAt.IsZero() {
		artifact.AddedAt = now
	}
	artifact.UpdatedAt = now

	doc := r.col.NewDoc()
	artifact.ID = doc.ID

	if _, err := doc.Set(ctx, artifact); err != nil {
		return "", err
	}
	return artifact.ID, nil
}

// Get retrieves a LensArtifact by ID.
func (r *LensArtifactsRepository) Get(ctx context.Context, id string) (*models.LensArtifact, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrLensArtifactNotFound
		}
		return nil, err
	}
	var artifact models.LensArtifact
	if err := snap.DataTo(&artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

// Update updates a LensArtifact.
func (r *LensArtifactsRepository) Update(ctx context.Context, artifact *models.LensArtifact) error {
	if artifact == nil || artifact.ID == "" {
		return ErrInvalidDocument
	}
	artifact.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(artifact.ID).Set(ctx, artifact)
	return err
}

// Delete deletes a LensArtifact by ID.
func (r *LensArtifactsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByLens deletes all artifacts for a given lens.
// Used when lens is deleted.
func (r *LensArtifactsRepository) DeleteByLens(ctx context.Context, lensID string) error {
	if lensID == "" {
		return ErrInvalidDocument
	}

	// Batch delete in chunks of 500 (Firestore limit)
	const batchSize = 500
	for {
		docs, err := r.col.Where("lensId", "==", lensID).Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}

		batch := r.db.Batch()
		for _, doc := range docs {
			batch.Delete(doc.Ref)
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// QUERY METHODS
// ============================================================================

// ListByLens retrieves all artifacts for a lens, ordered by addedAt DESC.
// Uses index: (lensId ASC, addedAt DESC)
func (r *LensArtifactsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*models.LensArtifact, error) {
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 500
	}

	q := r.col.Where("lensId", "==", lensID).OrderBy("addedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	artifacts := make([]*models.LensArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		artifacts = append(artifacts, &a)
	}
	return artifacts, nil
}

// ListByLensPaginated retrieves artifacts with cursor-based pagination.
// Uses index: (lensId ASC, addedAt DESC)
func (r *LensArtifactsRepository) ListByLensPaginated(ctx context.Context, lensID string, limit int, cursor *time.Time) ([]*models.LensArtifact, *time.Time, error) {
	if lensID == "" {
		return nil, nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.Where("lensId", "==", lensID).OrderBy("addedAt", firestore.Desc)
	if cursor != nil {
		q = q.StartAfter(*cursor)
	}
	q = q.Limit(limit + 1) // fetch one extra to detect next page

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, nil, err
	}

	artifacts := make([]*models.LensArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		artifacts = append(artifacts, &a)
	}

	var nextCursor *time.Time
	if len(artifacts) > limit {
		lastArtifact := artifacts[limit-1]
		nextCursor = &lastArtifact.AddedAt
		artifacts = artifacts[:limit]
	}

	return artifacts, nextCursor, nil
}

// ListFragmentsByLens retrieves only FRAGMENT artifacts.
// Uses index: (lensId ASC, type ASC, addedAt DESC)
func (r *LensArtifactsRepository) ListFragmentsByLens(ctx context.Context, lensID string, limit int) ([]*models.LensArtifact, error) {
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 500
	}

	q := r.col.Where("lensId", "==", lensID).
		Where("type", "==", string(models.LensArtifactTypeFragment)).
		OrderBy("addedAt", firestore.Desc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	artifacts := make([]*models.LensArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		artifacts = append(artifacts, &a)
	}
	return artifacts, nil
}

// ListEdgesByLens retrieves only EDGE artifacts.
// Uses index: (lensId ASC, type ASC, addedAt DESC)
func (r *LensArtifactsRepository) ListEdgesByLens(ctx context.Context, lensID string, limit int) ([]*models.LensArtifact, error) {
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 500
	}

	q := r.col.Where("lensId", "==", lensID).
		Where("type", "==", string(models.LensArtifactTypeEdge)).
		OrderBy("addedAt", firestore.Desc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	artifacts := make([]*models.LensArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		artifacts = append(artifacts, &a)
	}
	return artifacts, nil
}

// ListUnprocessedForStrategic retrieves artifacts not yet processed by Phase 2.
// Uses index: (lensId ASC, processedForStrategicAt ASC)
func (r *LensArtifactsRepository) ListUnprocessedForStrategic(ctx context.Context, lensID string, limit int) ([]*models.LensArtifact, error) {
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 500
	}

	// Firestore doesn't support "is null" directly, so we query where processedForStrategicAt doesn't exist
	// This works because Firestore only indexes documents where the field exists
	q := r.col.Where("lensId", "==", lensID).
		OrderBy("processedForStrategicAt", firestore.Asc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	artifacts := make([]*models.LensArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		// Double-check nil (Firestore quirk with null values)
		if a.ProcessedForStrategicAt == nil {
			artifacts = append(artifacts, &a)
		}
	}
	return artifacts, nil
}

// ============================================================================
// DEDUPLICATION / UPSERT METHODS
// ============================================================================

// GetByCanonicalPath finds a fragment artifact by lens and canonical path.
// Uses index: (lensId ASC, canonicalPath ASC)
func (r *LensArtifactsRepository) GetByCanonicalPath(ctx context.Context, lensID, canonicalPath string) (*models.LensArtifact, error) {
	if lensID == "" || canonicalPath == "" {
		return nil, ErrInvalidDocument
	}

	docs, err := r.col.Where("lensId", "==", lensID).
		Where("canonicalPath", "==", canonicalPath).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	var a models.LensArtifact
	if err := docs[0].DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetByEdgeID finds an edge artifact by lens and edge ID.
// Uses index: (lensId ASC, edgeId ASC)
func (r *LensArtifactsRepository) GetByEdgeID(ctx context.Context, lensID, edgeID string) (*models.LensArtifact, error) {
	if lensID == "" || edgeID == "" {
		return nil, ErrInvalidDocument
	}

	docs, err := r.col.Where("lensId", "==", lensID).
		Where("edgeId", "==", edgeID).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	var a models.LensArtifact
	if err := docs[0].DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpsertFragment creates or updates a fragment artifact.
// Deduplicates by (lensId, canonicalPath).
func (r *LensArtifactsRepository) UpsertFragment(ctx context.Context, artifact *models.LensArtifact) (string, error) {
	if artifact == nil || artifact.Type != models.LensArtifactTypeFragment {
		return "", fmt.Errorf("invalid fragment artifact")
	}

	existing, err := r.GetByCanonicalPath(ctx, artifact.LensID, artifact.CanonicalPath)
	if err != nil {
		return "", err
	}

	if existing != nil {
		// Update existing
		existing.FragmentID = artifact.FragmentID
		existing.FragmentType = artifact.FragmentType
		existing.SourceJobID = artifact.SourceJobID
		existing.DisplayLabel = artifact.DisplayLabel
		existing.UpdatedAt = time.Now().UTC()
		// Reset strategic processing if content changed
		existing.ProcessedForStrategicAt = nil
		if err := r.Update(ctx, existing); err != nil {
			return "", err
		}
		return existing.ID, nil
	}

	// Create new
	return r.Create(ctx, artifact)
}

// UpsertEdge creates or updates an edge artifact.
// Deduplicates by (lensId, edgeId).
func (r *LensArtifactsRepository) UpsertEdge(ctx context.Context, artifact *models.LensArtifact) (string, error) {
	if artifact == nil || artifact.Type != models.LensArtifactTypeEdge {
		return "", fmt.Errorf("invalid edge artifact")
	}

	existing, err := r.GetByEdgeID(ctx, artifact.LensID, artifact.EdgeID)
	if err != nil {
		return "", err
	}

	if existing != nil {
		// Update existing
		existing.EdgeType = artifact.EdgeType
		existing.SourcePath = artifact.SourcePath
		existing.TargetPath = artifact.TargetPath
		existing.SourceFragID = artifact.SourceFragID
		existing.TargetFragID = artifact.TargetFragID
		existing.SourceJobID = artifact.SourceJobID
		existing.UpdatedAt = time.Now().UTC()
		existing.ProcessedForStrategicAt = nil
		if err := r.Update(ctx, existing); err != nil {
			return "", err
		}
		return existing.ID, nil
	}

	return r.Create(ctx, artifact)
}

// ============================================================================
// REVERSE LOOKUP
// ============================================================================

// ListLensesByFragment finds all lenses that contain a given fragment.
// Uses index: (fragmentId ASC)
func (r *LensArtifactsRepository) ListLensesByFragment(ctx context.Context, fragmentID string, limit int) ([]string, error) {
	if fragmentID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}

	docs, err := r.col.Where("fragmentId", "==", fragmentID).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	lensIDSet := make(map[string]struct{})
	for _, d := range docs {
		var a models.LensArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		lensIDSet[a.LensID] = struct{}{}
	}

	lensIDs := make([]string, 0, len(lensIDSet))
	for id := range lensIDSet {
		lensIDs = append(lensIDs, id)
	}
	return lensIDs, nil
}

// ============================================================================
// BATCH OPERATIONS
// ============================================================================

// MarkProcessedForStrategic marks artifacts as processed by Phase 2.
func (r *LensArtifactsRepository) MarkProcessedForStrategic(ctx context.Context, artifactIDs []string) error {
	if len(artifactIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	const batchSize = 500

	for i := 0; i < len(artifactIDs); i += batchSize {
		end := i + batchSize
		if end > len(artifactIDs) {
			end = len(artifactIDs)
		}
		batch := r.db.Batch()
		for _, id := range artifactIDs[i:end] {
			batch.Update(r.col.Doc(id), []firestore.Update{
				{Path: "processedForStrategicAt", Value: now},
				{Path: "updatedAt", Value: now},
			})
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CountByLens returns the total count of artifacts for a lens.
func (r *LensArtifactsRepository) CountByLens(ctx context.Context, lensID string) (int, error) {
	if lensID == "" {
		return 0, ErrInvalidDocument
	}

	// Firestore doesn't have a direct count, so we use aggregation or iterate
	// For now, simple approach: fetch all IDs
	docs, err := r.col.Where("lensId", "==", lensID).Select().Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

// ============================================================================
// CONNECTIVITY-BASED PAGINATION FOR LLM
// ============================================================================

// ListByLensConnectivityPaginated retrieves artifacts grouped by connectivity.
// More connected entities appear on the same page for better LLM context coherence.
// Returns artifacts in pages where entities within a page are more connected to each other.
func (r *LensArtifactsRepository) ListByLensConnectivityPaginated(ctx context.Context, lensID string, pageSize int, pageNumber int) (*models.LensArtifactPage, error) {
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	// Fetch all artifacts for connectivity analysis
	allArtifacts, err := r.ListByLens(ctx, lensID, 2000)
	if err != nil {
		return nil, err
	}

	if len(allArtifacts) == 0 {
		return &models.LensArtifactPage{
			Artifacts:      []*models.LensArtifact{},
			PageNumber:     0,
			TotalPages:     0,
			TotalArtifacts: 0,
		}, nil
	}

	// Build connectivity graph
	pages := groupByConnectivity(allArtifacts, pageSize)
	totalPages := len(pages)

	if pageNumber < 0 {
		pageNumber = 0
	}
	if pageNumber >= totalPages {
		pageNumber = totalPages - 1
	}

	page := pages[pageNumber]
	var nextCursor string
	if pageNumber < totalPages-1 {
		nextCursor = fmt.Sprintf("%d", pageNumber+1)
	}

	return &models.LensArtifactPage{
		Artifacts:      page,
		PageNumber:     pageNumber,
		TotalPages:     totalPages,
		TotalArtifacts: len(allArtifacts),
		NextCursor:     nextCursor,
	}, nil
}

// groupByConnectivity groups artifacts into pages based on edge connectivity.
// Fragments connected by edges appear on the same page.
func groupByConnectivity(artifacts []*models.LensArtifact, pageSize int) [][]*models.LensArtifact {
	// Separate fragments and edges
	fragments := make([]*models.LensArtifact, 0)
	edges := make([]*models.LensArtifact, 0)
	fragByID := make(map[string]*models.LensArtifact)

	for _, a := range artifacts {
		if a.IsFragment() {
			fragments = append(fragments, a)
			fragByID[a.FragmentID] = a
		} else if a.IsEdge() {
			edges = append(edges, a)
		}
	}

	// Build adjacency list for fragments
	adjList := make(map[string][]string) // fragmentID -> connected fragmentIDs
	for _, e := range edges {
		if e.SourceFragID != "" && e.TargetFragID != "" {
			adjList[e.SourceFragID] = append(adjList[e.SourceFragID], e.TargetFragID)
			adjList[e.TargetFragID] = append(adjList[e.TargetFragID], e.SourceFragID)
		}
	}

	// Sort fragments by connectivity (most connected first)
	sort.Slice(fragments, func(i, j int) bool {
		return len(adjList[fragments[i].FragmentID]) > len(adjList[fragments[j].FragmentID])
	})

	// Greedy clustering: assign fragments to pages, keeping connected ones together
	visited := make(map[string]bool)
	var pages [][]*models.LensArtifact
	var currentPage []*models.LensArtifact

	for _, frag := range fragments {
		if visited[frag.FragmentID] {
			continue
		}

		// BFS to find connected component (up to page size)
		queue := []string{frag.FragmentID}
		component := []*models.LensArtifact{}

		for len(queue) > 0 && len(component)+len(currentPage) < pageSize {
			id := queue[0]
			queue = queue[1:]

			if visited[id] {
				continue
			}
			visited[id] = true

			if f, ok := fragByID[id]; ok {
				component = append(component, f)
				for _, neighbor := range adjList[id] {
					if !visited[neighbor] {
						queue = append(queue, neighbor)
					}
				}
			}
		}

		// Add component to current page or start new page
		if len(currentPage)+len(component) > pageSize && len(currentPage) > 0 {
			pages = append(pages, currentPage)
			currentPage = make([]*models.LensArtifact, 0)
		}
		currentPage = append(currentPage, component...)
	}

	if len(currentPage) > 0 {
		pages = append(pages, currentPage)
	}

	// Add edges to pages (edge goes to page where both endpoints are, or first endpoint)
	fragToPage := make(map[string]int)
	for pageIdx, page := range pages {
		for _, a := range page {
			fragToPage[a.FragmentID] = pageIdx
		}
	}

	for _, e := range edges {
		srcPage, srcOk := fragToPage[e.SourceFragID]
		tgtPage, tgtOk := fragToPage[e.TargetFragID]

		targetPage := 0
		if srcOk && tgtOk && srcPage == tgtPage {
			targetPage = srcPage
		} else if srcOk {
			targetPage = srcPage
		} else if tgtOk {
			targetPage = tgtPage
		}

		if targetPage < len(pages) {
			pages[targetPage] = append(pages[targetPage], e)
		} else if len(pages) > 0 {
			pages[len(pages)-1] = append(pages[len(pages)-1], e)
		} else {
			pages = append(pages, []*models.LensArtifact{e})
		}
	}

	return pages
}

// ============================================================================
// HELPER: Update canonical path (for redirect handling)
// ============================================================================

// UpdateCanonicalPath updates the canonical path of a fragment artifact.
// Called when a canonical path redirect occurs.
func (r *LensArtifactsRepository) UpdateCanonicalPath(ctx context.Context, lensID, oldPath, newPath string) error {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if lensID == "" || oldPath == "" || newPath == "" {
		return ErrInvalidDocument
	}

	existing, err := r.GetByCanonicalPath(ctx, lensID, oldPath)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil // nothing to update
	}

	existing.CanonicalPath = newPath
	existing.UpdatedAt = time.Now().UTC()
	existing.ProcessedForStrategicAt = nil // reset processing flag
	return r.Update(ctx, existing)
}
