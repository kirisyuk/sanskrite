package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrInvalidDocument = errors.New("invalid document")

type UsersRepository struct{ col *firestore.CollectionRef }
type ProjectsRepository struct{ col *firestore.CollectionRef }
type DataSourcesRepository struct{ col *firestore.CollectionRef }

func NewUsersRepository(db *firestore.Client) *UsersRepository {
	return &UsersRepository{col: db.Collection("core_users")}
}

func NewProjectsRepository(db *firestore.Client) *ProjectsRepository {
	return &ProjectsRepository{col: db.Collection("core_projects")}
}

func NewDataSourcesRepository(db *firestore.Client) *DataSourcesRepository {
	return &DataSourcesRepository{col: db.Collection("core_datasources")}
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
	ds.Pk = ds.TenantID
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

func (r *DataSourcesRepository) Upsert(ctx context.Context, ds *models.DataSource) error {
	if ds == nil || ds.ID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if ds.CreatedAt.IsZero() {
		ds.CreatedAt = now
	}
	ds.UpdatedAt = now
	if ds.Pk == "" {
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
	// Query by tenantId and sourceDetails fields
	// For web sources, canonical is stored in Details.URI (canonicalized)
	// For telegram, canonical is stored in Details.ChannelUsername
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			continue
		}
		// Check if canonical matches URI or ChannelUsername
		if ds.Details.URI != nil && *ds.Details.URI == canonical {
			return &ds, nil
		}
		if ds.Details.ChannelUsername != nil && *ds.Details.ChannelUsername == canonical {
			return &ds, nil
		}
	}

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
	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			continue
		}
		// Include if projectId matches or projectId is in relatedProjectIds
		if (ds.ProjectID != nil && *ds.ProjectID == projectID) || contains(ds.RelatedProjectIDs, projectID) {
			sources = append(sources, &ds)
		}
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
