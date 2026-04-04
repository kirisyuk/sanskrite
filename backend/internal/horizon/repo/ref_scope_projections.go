package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const refScopeProjectionCollection = "horizon_ref_scope_projection"

type RefScopeProjectionsRepository struct {
	col *firestore.CollectionRef
}

func NewRefScopeProjectionsRepository(db *firestore.Client) *RefScopeProjectionsRepository {
	return &RefScopeProjectionsRepository{col: db.Collection(refScopeProjectionCollection)}
}

func (r *RefScopeProjectionsRepository) Upsert(ctx context.Context, projection *models.RefScopeProjection) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("ref scope projections repo not configured")
	}
	if projection == nil {
		return ErrInvalidDocument
	}
	projection.Normalize()
	if projection.TenantID == "" || projection.Ref == "" {
		return ErrInvalidDocument
	}
	projection.ID = refScopeProjectionDocID(projection.TenantID, projection.Ref)
	_, err := r.col.Doc(projection.ID).Set(ctx, projection)
	return err
}

func (r *RefScopeProjectionsRepository) Get(ctx context.Context, tenantID, ref string) (*models.RefScopeProjection, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("ref scope projections repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	ref = strings.TrimSpace(ref)
	if tenantID == "" || ref == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(refScopeProjectionDocID(tenantID, ref)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var projection models.RefScopeProjection
	if err := snap.DataTo(&projection); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projection.ID) == "" {
		projection.ID = snap.Ref.ID
	}
	return &projection, nil
}

func (r *RefScopeProjectionsRepository) BatchGet(ctx context.Context, tenantID string, refs []string) ([]*models.RefScopeProjection, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("ref scope projections repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	refs = compactUniqueScopeRefs(refs)
	if tenantID == "" || len(refs) == 0 {
		return nil, nil
	}
	out := make([]*models.RefScopeProjection, 0, len(refs))
	for _, ref := range refs {
		item, err := r.Get(ctx, tenantID, ref)
		if err != nil || item == nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Ref < out[j].Ref
	})
	return out, nil
}

func (r *RefScopeProjectionsRepository) ListByRefAnyTenant(ctx context.Context, ref string, limit int) ([]*models.RefScopeProjection, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("ref scope projections repo not configured")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("ref", "==", ref).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.RefScopeProjection, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		var projection models.RefScopeProjection
		if err := doc.DataTo(&projection); err != nil {
			continue
		}
		if strings.TrimSpace(projection.ID) == "" {
			projection.ID = doc.Ref.ID
		}
		projection.Normalize()
		if projection.Ref == "" {
			continue
		}
		out = append(out, &projection)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID == out[j].TenantID {
			return out[i].Ref < out[j].Ref
		}
		return out[i].TenantID < out[j].TenantID
	})
	return out, nil
}

func refScopeProjectionDocID(tenantID, ref string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(ref)))
	return "rsp_" + hex.EncodeToString(sum[:10])
}
