package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrUnitGraphSnapshotNotFound = errors.New("unit graph snapshot not found")

type UnitGraphSnapshotsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewUnitGraphSnapshotsRepository(db *firestore.Client) *UnitGraphSnapshotsRepository {
	if db == nil {
		return nil
	}
	return &UnitGraphSnapshotsRepository{
		db:  db,
		col: db.Collection("unit_graph_snapshots"),
	}
}

func (r *UnitGraphSnapshotsRepository) BuildSnapshotID(tenantID, lensID, domainCanonicalPath string) string {
	t := strings.TrimSpace(strings.ToLower(tenantID))
	l := strings.TrimSpace(strings.ToLower(lensID))
	d := strings.TrimSpace(strings.ToLower(domainCanonicalPath))
	if t == "" || l == "" {
		return ""
	}
	sum := sha1.Sum([]byte(t + "|" + l + "|" + d))
	return "ugs_" + hex.EncodeToString(sum[:])
}

func (r *UnitGraphSnapshotsRepository) Upsert(ctx context.Context, snapshot *models.UnitGraphSnapshot) (string, error) {
	if snapshot == nil {
		return "", ErrInvalidDocument
	}
	snapshot.TenantID = strings.TrimSpace(snapshot.TenantID)
	snapshot.LensID = strings.TrimSpace(snapshot.LensID)
	snapshot.ProjectID = strings.TrimSpace(snapshot.ProjectID)
	snapshot.DomainCanonicalPath = strings.TrimSpace(snapshot.DomainCanonicalPath)
	if snapshot.TenantID == "" || snapshot.LensID == "" {
		return "", fmt.Errorf("tenantId and lensId are required")
	}
	if snapshot.Edges == nil {
		snapshot.Edges = []models.UnitGraphEdge{}
	}
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now

	id := strings.TrimSpace(snapshot.ID)
	if id == "" {
		id = r.BuildSnapshotID(snapshot.TenantID, snapshot.LensID, snapshot.DomainCanonicalPath)
	}
	if id == "" {
		return "", fmt.Errorf("failed to build unit graph snapshot id")
	}
	snapshot.ID = id
	if _, err := r.col.Doc(id).Set(ctx, snapshot); err != nil {
		return "", err
	}
	return id, nil
}

func (r *UnitGraphSnapshotsRepository) Get(ctx context.Context, id string) (*models.UnitGraphSnapshot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrUnitGraphSnapshotNotFound
		}
		return nil, err
	}
	var out models.UnitGraphSnapshot
	if err := doc.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *UnitGraphSnapshotsRepository) GetByLensAndDomain(ctx context.Context, lensID, domainCanonicalPath string) (*models.UnitGraphSnapshot, error) {
	lensID = strings.TrimSpace(lensID)
	domainCanonicalPath = strings.TrimSpace(domainCanonicalPath)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	q := r.col.Where("lensId", "==", lensID)
	if domainCanonicalPath != "" {
		q = q.Where("domainCanonicalPath", "==", domainCanonicalPath)
	}
	docs, err := q.OrderBy("updatedAt", firestore.Desc).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrUnitGraphSnapshotNotFound
	}
	var out models.UnitGraphSnapshot
	if err := docs[0].DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *UnitGraphSnapshotsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*models.UnitGraphSnapshot, error) {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 20
	}
	q := r.col.Where("lensId", "==", lensID).OrderBy("updatedAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.UnitGraphSnapshot, 0, len(docs))
	for _, d := range docs {
		var item models.UnitGraphSnapshot
		if err := d.DataTo(&item); err != nil {
			continue
		}
		out = append(out, &item)
	}
	return out, nil
}
