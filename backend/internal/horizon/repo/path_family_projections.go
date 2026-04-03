package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const pathFamilyProjectionCollection = "horizon_path_family_projection"

type PathFamilyProjectionsRepository struct {
	col *firestore.CollectionRef
}

func NewPathFamilyProjectionsRepository(db *firestore.Client) *PathFamilyProjectionsRepository {
	return &PathFamilyProjectionsRepository{col: db.Collection(pathFamilyProjectionCollection)}
}

func (r *PathFamilyProjectionsRepository) Upsert(ctx context.Context, projection *models.PathFamilyProjection) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("path family projections repo not configured")
	}
	if projection == nil {
		return ErrInvalidDocument
	}
	projection.Normalize()
	if projection.TenantID == "" || projection.FamilyID == "" {
		return ErrInvalidDocument
	}
	projection.ID = pathFamilyProjectionDocID(projection.TenantID, projection.FamilyID)
	_, err := r.col.Doc(projection.ID).Set(ctx, projection)
	return err
}

func (r *PathFamilyProjectionsRepository) Get(ctx context.Context, tenantID, familyID string) (*models.PathFamilyProjection, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("path family projections repo not configured")
	}
	snap, err := r.col.Doc(pathFamilyProjectionDocID(tenantID, familyID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var projection models.PathFamilyProjection
	if err := snap.DataTo(&projection); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projection.ID) == "" {
		projection.ID = snap.Ref.ID
	}
	return &projection, nil
}

func pathFamilyProjectionDocID(tenantID, familyID string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(familyID)))
	return "pfp_" + hex.EncodeToString(sum[:10])
}
