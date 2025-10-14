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
type DataSourcesRepository struct{ col *firestore.CollectionRef }
type SourceProfilesRepository struct{ col *firestore.CollectionRef }

func NewUsersRepository(db *firestore.Client) *UsersRepository {
	return &UsersRepository{col: db.Collection("core_users")}
}

func NewProjectsRepository(db *firestore.Client) *ProjectsRepository {
	return &ProjectsRepository{col: db.Collection("core_projects")}
}

func NewDataSourcesRepository(db *firestore.Client) *DataSourcesRepository {
	return &DataSourcesRepository{col: db.Collection("core_datasources")}
}

func NewSourceProfilesRepository(db *firestore.Client) *SourceProfilesRepository {
	return &SourceProfilesRepository{col: db.Collection("core_source_profiles")}
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
	if sp == nil || sp.ID == "" || sp.DataSourceID == "" || sp.TenantID == "" {
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
	// Simple scan by tenant; Firestore requires composite index for equality on two fields, use client-side filter for now
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
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
	// Fetch all tenant profiles and filter
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var res []*models.SourceProfile
	for _, d := range docs {
		var sp models.SourceProfile
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		if _, ok := ids[sp.DataSourceID]; ok {
			res = append(res, &sp)
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
	// Firestore doesn't support array-contains OR field equals in single query
	// So we need to fetch all sources for tenant and filter in memory
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var sources []*models.DataSource
	var debugInfo []string
	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			continue
		}
		// Debug: log all sources to understand filtering
		projID := "nil"
		if ds.ProjectID != nil {
			projID = *ds.ProjectID
		}
		debugInfo = append(debugInfo, fmt.Sprintf("id=%s,kind=%s,projID=%s,related=%v", ds.ID, ds.Kind, projID, ds.RelatedProjectIDs))

		// Include if projectId matches or projectId is in relatedProjectIds
		if (ds.ProjectID != nil && *ds.ProjectID == projectID) || contains(ds.RelatedProjectIDs, projectID) {
			sources = append(sources, &ds)
		}
	}

	// Log for debugging
	if len(sources) == 0 && len(debugInfo) > 0 {
		fmt.Printf("[DataSourcesRepository] ListByProject: found 0 sources for project %s among %d total sources: %v\n", projectID, len(debugInfo), debugInfo)
	}

	return sources, nil
}

// DeleteByProject deletes sources where projectId == id, or removes id from relatedProjectIds
func (r *DataSourcesRepository) DeleteByProject(ctx context.Context, projectID string) error {
	// Fetch all sources that reference this project
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			continue
		}

		// If primary project, delete the source
		if ds.ProjectID != nil && *ds.ProjectID == projectID {
			if _, err := r.col.Doc(doc.Ref.ID).Delete(ctx); err != nil {
				// Log but continue
				continue
			}
		} else if contains(ds.RelatedProjectIDs, projectID) {
			// Remove from relatedProjectIds
			ds.RelatedProjectIDs = remove(ds.RelatedProjectIDs, projectID)
			if err := r.Upsert(ctx, &ds); err != nil {
				// Log but continue
				continue
			}
		}
	}

	return nil
}

// helper functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func remove(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
