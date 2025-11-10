package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func isPublicSource(kind string) bool {
	switch kind {
	case "web", "rss", "telegram_channel":
		return true
	default:
		return false
	}
}

var ErrInvalidDocument = errors.New("invalid document")

type UsersRepository struct{ col *firestore.CollectionRef }
type ProjectsRepository struct{ col *firestore.CollectionRef }
type DataSourcesRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}
type SourceProfilesRepository struct{ col *firestore.CollectionRef }
type CoreDocsRepository struct{ col *firestore.CollectionRef }
type WaitlistRepository struct{ col *firestore.CollectionRef }
type MetaAnalysesRepository struct{ col *firestore.CollectionRef }
type MetaAggregateProjectsRepository struct{ col *firestore.CollectionRef }
type MetaAggregateUsersRepository struct{ col *firestore.CollectionRef }

func NewUsersRepository(db *firestore.Client) *UsersRepository {
	return &UsersRepository{col: db.Collection("core_users")}
}

func NewProjectsRepository(db *firestore.Client) *ProjectsRepository {
	return &ProjectsRepository{col: db.Collection("core_projects")}
}

func NewDataSourcesRepository(db *firestore.Client) *DataSourcesRepository {
	return &DataSourcesRepository{col: db.Collection("core_datasources"), db: db}
}

func NewSourceProfilesRepository(db *firestore.Client) *SourceProfilesRepository {
	return &SourceProfilesRepository{col: db.Collection("core_source_profiles")}
}

func NewCoreDocsRepository(db *firestore.Client) *CoreDocsRepository {
	return &CoreDocsRepository{col: db.Collection("core_docs")}
}

func NewWaitlistRepository(db *firestore.Client) *WaitlistRepository {
	return &WaitlistRepository{col: db.Collection("core_waitlist")}
}

func NewMetaAnalysesRepository(db *firestore.Client) *MetaAnalysesRepository {
	return &MetaAnalysesRepository{col: db.Collection("meta_analyses")}
}

func NewMetaAggregateProjectsRepository(db *firestore.Client) *MetaAggregateProjectsRepository {
	return &MetaAggregateProjectsRepository{col: db.Collection("meta_aggregate_project")}
}

func NewMetaAggregateUsersRepository(db *firestore.Client) *MetaAggregateUsersRepository {
	return &MetaAggregateUsersRepository{col: db.Collection("meta_aggregate_user")}
}

// --- Meta Analyses ---

func (r *MetaAnalysesRepository) Create(ctx context.Context, e *models.MetaAnalysisEvent) (string, error) {
	if e == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	e.CreatedAt = now
	doc := r.col.NewDoc()
	e.ID = doc.ID
	if _, err := doc.Set(ctx, e); err != nil {
		return "", err
	}
	return e.ID, nil
}

func (r *MetaAnalysesRepository) ListByProject(ctx context.Context, projectID string, limit int) ([]*models.MetaAnalysisEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.MetaAnalysisEvent
	for _, d := range docs {
		var e models.MetaAnalysisEvent
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *MetaAnalysesRepository) ListByUser(ctx context.Context, tenantID, userID string, limit int) ([]*models.MetaAnalysisEvent, error) {
	if limit <= 0 {
		limit = 500
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("userId", "==", userID).OrderBy("createdAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.MetaAnalysisEvent
	for _, d := range docs {
		var e models.MetaAnalysisEvent
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *MetaAnalysesRepository) DeleteByProject(ctx context.Context, projectID string) error {
	q := r.col.Where("projectId", "==", projectID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

// --- Meta Aggregates Project ---

func (r *MetaAggregateProjectsRepository) Upsert(ctx context.Context, a *models.MetaAggregateProject) error {
	if a == nil || a.ProjectID == "" {
		return ErrInvalidDocument
	}
	a.ID = a.ProjectID
	if a.Version == "" {
		a.Version = "coach.v1"
	}
	a.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(a.ID).Set(ctx, a)
	return err
}

func (r *MetaAggregateProjectsRepository) Get(ctx context.Context, projectID string) (*models.MetaAggregateProject, error) {
	snap, err := r.col.Doc(projectID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var a models.MetaAggregateProject
	if err := snap.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *MetaAggregateProjectsRepository) Delete(ctx context.Context, projectID string) error {
	_, err := r.col.Doc(projectID).Delete(ctx)
	return err
}

// --- Meta Aggregates User ---

func (r *MetaAggregateUsersRepository) Upsert(ctx context.Context, a *models.MetaAggregateUser) error {
	if a == nil || a.UserID == "" || a.TenantID == "" {
		return ErrInvalidDocument
	}
	a.ID = a.TenantID + "_" + a.UserID
	if a.Version == "" {
		a.Version = "coach.v1"
	}
	a.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(a.ID).Set(ctx, a)
	return err
}

func (r *MetaAggregateUsersRepository) Get(ctx context.Context, tenantID, userID string) (*models.MetaAggregateUser, error) {
	id := tenantID + "_" + userID
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var a models.MetaAggregateUser
	if err := snap.DataTo(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *MetaAggregateUsersRepository) Delete(ctx context.Context, tenantID, userID string) error {
	id := tenantID + "_" + userID
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// --- Users ---
func (r *UsersRepository) Upsert(ctx context.Context, u *models.User) error {
	if u == nil || u.ID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	if u.SchemaVersion == "" {
		u.SchemaVersion = "1.0"
	}
	// Use composite key: tenantId_userId
	docID := u.TenantID + "_" + u.ID
	_, err := r.col.Doc(docID).Set(ctx, u)
	return err
}

// --- Waitlist ---
func (r *WaitlistRepository) Upsert(ctx context.Context, e *models.WaitlistEntry) error {
	if e == nil || e.Email == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.Status == "" {
		e.Status = "pending"
	}
	e.UpdatedAt = now
	id := e.ID
	if id == "" {
		id = e.Email
		e.ID = id
	}
	_, err := r.col.Doc(id).Set(ctx, e)
	return err
}

func (r *WaitlistRepository) Get(ctx context.Context, email string) (*models.WaitlistEntry, error) {
	if email == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(email).Get(ctx)
	if err != nil {
		return nil, err
	}
	var e models.WaitlistEntry
	if err := snap.DataTo(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *WaitlistRepository) List(ctx context.Context, status *string) ([]*models.WaitlistEntry, error) {
	q := r.col.Query
	if status != nil && *status != "" {
		q = q.Where("status", "==", *status)
	}
	docs, err := q.OrderBy("createdAt", firestore.Desc).Limit(500).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*models.WaitlistEntry
	for _, d := range docs {
		var e models.WaitlistEntry
		if err := d.DataTo(&e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

func (r *UsersRepository) Get(ctx context.Context, id string) (*models.User, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var u models.User
	if err := snap.DataTo(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUID scans users to find by Firebase UID (across tenants)
// Note: Firestore lacks cross-collection secondary index here; this is acceptable for low volumes.
func (r *UsersRepository) GetByUID(ctx context.Context, uid string) (*models.User, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		var u models.User
		if err := d.DataTo(&u); err != nil {
			continue
		}
		// Backward compatibility: documents created before firestore tags may have PascalCase field names
		if u.ID == "" {
			data := d.Data()
			if idRaw, ok := data["ID"].(string); ok {
				u.ID = idRaw
			}
			if tRaw, ok := data["TenantID"].(string); ok {
				u.TenantID = tRaw
			}
			if eRaw, ok := data["Email"].(string); ok {
				u.Email = eRaw
			}
			if sv, ok := data["SchemaVersion"].(string); ok {
				u.SchemaVersion = sv
			}
			// Minimal timestamps if present
			if ca, ok := data["CreatedAt"].(time.Time); ok {
				u.CreatedAt = ca
			}
			if ua, ok := data["UpdatedAt"].(time.Time); ok {
				u.UpdatedAt = ua
			}
			// If we successfully recovered ID, consider migrating document to lowerCamel
			if u.ID != "" && u.TenantID != "" {
				_ = r.Upsert(ctx, &u)
			}
		}
		if u.ID == uid {
			return &u, nil
		}
	}
	return nil, errors.New("user not found")
}

func (r *UsersRepository) GetByIDAndTenant(ctx context.Context, userID, tenantID string) (*models.User, error) {
	docID := tenantID + "_" + userID
	snap, err := r.col.Doc(docID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var u models.User
	if err := snap.DataTo(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

// --- Projects ---
func (r *ProjectsRepository) Create(ctx context.Context, p *models.Project) (string, error) {
	if p == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	p.LastInteractionAt = &now
	p.Pk = p.TenantID // Partition key
	doc := r.col.NewDoc()
	p.ID = doc.ID
	if _, err := doc.Set(ctx, p); err != nil {
		return "", err
	}
	return doc.ID, nil
}

func (r *ProjectsRepository) Upsert(ctx context.Context, p *models.Project) error {
	if p == nil || p.ID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Pk == "" {
		p.Pk = p.TenantID
	}
	_, err := r.col.Doc(p.ID).Set(ctx, p)
	return err
}

func (r *ProjectsRepository) Get(ctx context.Context, id string) (*models.Project, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var p models.Project
	if err := snap.DataTo(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProjectsRepository) List(ctx context.Context, tenantID string, view *string) ([]*models.Project, error) {
	// With firestore tags set to lowerCamel, use lowerCamel paths
	query := r.col.Where("tenantId", "==", tenantID)
	if view != nil {
		query = query.Where("view", "==", *view)
	}
	// Order by lastInteractionAt desc for Discovery chats
	query = query.OrderBy("lastInteractionAt", firestore.Desc).Limit(100)

	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	projects := make([]*models.Project, 0, len(docs))
	for _, doc := range docs {
		var p models.Project
		if err := doc.DataTo(&p); err != nil {
			continue
		}
		projects = append(projects, &p)
	}
	return projects, nil
}

func (r *ProjectsRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

func (r *ProjectsRepository) UpdateLastInteraction(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "lastInteractionAt", Value: now},
		{Path: "updatedAt", Value: now},
	})
	return err
}

func (r *ProjectsRepository) GetByVerifiedCode(ctx context.Context, code string) (*models.Project, error) {
	if code == "" {
		return nil, ErrInvalidDocument
	}
	q := r.col.Where("verifiedCode", "==", code).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var p models.Project
	if err := docs[0].DataTo(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// --- DataSources ---
func (r *DataSourcesRepository) Create(ctx context.Context, ds *models.DataSource) (string, error) {
	if ds == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	ds.CreatedAt = now
	ds.UpdatedAt = now
	// Public sources (web/rss/telegram_channel) are stored globally
	if isPublicSource(ds.Kind) {
		ds.TenantID = ""
		ds.Pk = "global"
	} else {
		ds.Pk = ds.TenantID
	}
	if ds.FirstSeen.IsZero() {
		ds.FirstSeen = now
	}
	ds.LastSeen = now
	doc := r.col.NewDoc()
	ds.ID = doc.ID
	if _, err := doc.Set(ctx, ds); err != nil {
		return "", err
	}
	return doc.ID, nil
}

// --- SourceProfiles ---
func (r *SourceProfilesRepository) Upsert(ctx context.Context, sp *models.SourceProfile) error {
	if sp == nil || sp.ID == "" || sp.DataSourceID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if sp.CreatedAt.IsZero() {
		sp.CreatedAt = now
	}
	sp.UpdatedAt = now
	if sp.SchemaVersion == "" {
		sp.SchemaVersion = "1.0"
	}
	_, err := r.col.Doc(sp.ID).Set(ctx, sp)
	return err
}

func (r *SourceProfilesRepository) GetByDataSource(ctx context.Context, tenantID, dataSourceID string) (*models.SourceProfile, error) {
	// Prefer global profile
	if docsGlobal, err := r.col.Where("tenantId", "==", "").Documents(ctx).GetAll(); err == nil {
		for _, d := range docsGlobal {
			var sp models.SourceProfile
			if err := d.DataTo(&sp); err != nil {
				continue
			}
			if sp.DataSourceID == dataSourceID {
				return &sp, nil
			}
		}
	}
	// Fallback to tenant-scoped
	docsTenant, err2 := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err2 != nil {
		return nil, err2
	}
	for _, d := range docsTenant {
		var sp models.SourceProfile
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		if sp.DataSourceID == dataSourceID {
			return &sp, nil
		}
	}
	return nil, fmt.Errorf("source profile not found")
}

// DeleteTenantScopedForSource removes tenant-scoped profile for a given dataSourceID (used during migration to global profiles)
func (r *SourceProfilesRepository) DeleteTenantScopedForSource(ctx context.Context, tenantID, dataSourceID string) error {
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		var sp models.SourceProfile
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		if sp.DataSourceID == dataSourceID {
			_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
		}
	}
	return nil
}

func (r *SourceProfilesRepository) ListByProject(ctx context.Context, tenantID, projectID string, sourcesRepo *DataSourcesRepository) ([]*models.SourceProfile, error) {
	// Load sources by project then map to profiles
	sources, err := sourcesRepo.ListByProject(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return []*models.SourceProfile{}, nil
	}
	// Build set of dataSourceIDs
	ids := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		ids[s.ID] = struct{}{}
	}
	// Fetch tenant-specific and global profiles and filter by DataSourceID
	var res []*models.SourceProfile
	// Tenant-scoped
	if docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll(); err == nil {
		for _, d := range docs {
			var sp models.SourceProfile
			if err := d.DataTo(&sp); err != nil {
				continue
			}
			if _, ok := ids[sp.DataSourceID]; ok {
				res = append(res, &sp)
			}
		}
	}
	// Global
	if docs, err := r.col.Where("tenantId", "==", "").Documents(ctx).GetAll(); err == nil {
		for _, d := range docs {
			var sp models.SourceProfile
			if err := d.DataTo(&sp); err != nil {
				continue
			}
			if _, ok := ids[sp.DataSourceID]; ok {
				res = append(res, &sp)
			}
		}
	}
	return res, nil
}

// DeleteByProject deletes source profiles for all data sources belonging to a project
func (r *SourceProfilesRepository) DeleteByProject(ctx context.Context, tenantID, projectID string, sourcesRepo *DataSourcesRepository) error {
	// Load sources tied to this project (including related)
	sources, err := sourcesRepo.ListByProject(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		ids[s.ID] = struct{}{}
	}

	// Scan profiles by tenant and delete those pointing to the collected dataSourceIds
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		var sp models.SourceProfile
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		if _, ok := ids[sp.DataSourceID]; ok {
			_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
		}
	}
	return nil
}

func (r *DataSourcesRepository) Upsert(ctx context.Context, ds *models.DataSource) error {
	if ds == nil || ds.ID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if ds.CreatedAt.IsZero() {
		ds.CreatedAt = now
	}
	ds.UpdatedAt = now
	if isPublicSource(ds.Kind) {
		ds.TenantID = ""
		ds.Pk = "global"
	} else if ds.Pk == "" {
		ds.Pk = ds.TenantID
	}
	ds.LastSeen = now
	_, err := r.col.Doc(ds.ID).Set(ctx, ds)
	return err
}

func (r *DataSourcesRepository) Get(ctx context.Context, id string) (*models.DataSource, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var ds models.DataSource
	if err := snap.DataTo(&ds); err != nil {
		return nil, err
	}
	return &ds, nil
}

// FindByCanonicalSource finds a datasource by tenant + canonical source identifier (e.g., normalized URL or @username)
func (r *DataSourcesRepository) FindByCanonicalSource(ctx context.Context, tenantID, canonical string) (*models.DataSource, error) {
	// Search across all datasources (globals + tenant-scoped)
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var candidates []string
	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			continue
		}
		// Check if canonical matches URI or ChannelUsername
		if ds.Details.URI != nil && *ds.Details.URI == canonical {
			fmt.Printf("[FindByCanonicalSource] Found match by URI: canonical=%s, id=%s\n", canonical, ds.ID)
			return &ds, nil
		}
		if ds.Details.ChannelUsername != nil && *ds.Details.ChannelUsername == canonical {
			fmt.Printf("[FindByCanonicalSource] Found match by ChannelUsername: canonical=%s, id=%s\n", canonical, ds.ID)
			return &ds, nil
		}
		// Collect candidates for debugging
		if ds.Details.ChannelUsername != nil {
			candidates = append(candidates, fmt.Sprintf("id=%s,channel=%s", ds.ID, *ds.Details.ChannelUsername))
		}
		if ds.Details.URI != nil {
			candidates = append(candidates, fmt.Sprintf("id=%s,uri=%s", ds.ID, *ds.Details.URI))
		}
	}

	fmt.Printf("[FindByCanonicalSource] No match for canonical=%s among %d sources: %v\n", canonical, len(candidates), candidates)
	return nil, fmt.Errorf("datasource not found")
}

// ListByProject returns datasources for a project (primary or related)
func (r *DataSourcesRepository) ListByProject(ctx context.Context, tenantID, projectID string) ([]*models.DataSource, error) {
	// Resolve via rag_source_projects linking table
	spRepo := NewSourceProjectsRepository(r.db)
	status := "active"
	links, err := spRepo.ListByProject(ctx, tenantID, projectID, &status)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return []*models.DataSource{}, nil
	}
	// Build set for fast lookup
	ids := make(map[string]struct{}, len(links))
	for _, l := range links {
		ids[l.SourceID] = struct{}{}
	}
	// Fetch all datasources (global + tenant) and filter by ids
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var sources []*models.DataSource
	for _, d := range docs {
		var ds models.DataSource
		if err := d.DataTo(&ds); err != nil {
			continue
		}
		if _, ok := ids[ds.ID]; ok {
			sources = append(sources, &ds)
		}
	}
	return sources, nil
}

// ListActiveByKinds returns active datasources filtered by kinds (using Firestore IN on kind)
func (r *DataSourcesRepository) ListActiveByKinds(ctx context.Context, kinds []string) ([]*models.DataSource, error) {
	if len(kinds) == 0 {
		return []*models.DataSource{}, nil
	}
	// Firestore IN supports up to 10 values
	q := r.col.Where("status", "==", "active").Where("kind", "in", kinds)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var res []*models.DataSource
	for _, d := range docs {
		var ds models.DataSource
		if err := d.DataTo(&ds); err != nil {
			continue
		}
		res = append(res, &ds)
	}
	return res, nil
}

// DeleteByProject deletes sources where projectId == id, or removes id from relatedProjectIds
func (r *DataSourcesRepository) DeleteByProject(ctx context.Context, projectID string) error {
	// No-op in new model: linkage is managed in rag_source_projects
	return nil
}

// (legacy helpers removed)
