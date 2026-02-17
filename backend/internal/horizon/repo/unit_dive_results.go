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

var ErrUnitDiveResultNotFound = errors.New("unit dive result not found")

type UnitDiveResultsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewUnitDiveResultsRepository(db *firestore.Client) *UnitDiveResultsRepository {
	if db == nil {
		return nil
	}
	return &UnitDiveResultsRepository{
		db:  db,
		col: db.Collection("unit_dive_results"),
	}
}

func (r *UnitDiveResultsRepository) BuildResultID(lensID, requestID string) string {
	l := strings.TrimSpace(strings.ToLower(lensID))
	req := strings.TrimSpace(strings.ToLower(requestID))
	if l == "" || req == "" {
		return ""
	}
	sum := sha1.Sum([]byte(l + "|" + req))
	return "udr_" + hex.EncodeToString(sum[:])
}

func (r *UnitDiveResultsRepository) Upsert(ctx context.Context, artifact *models.UnitDiveResultArtifact) (string, error) {
	if artifact == nil {
		return "", ErrInvalidDocument
	}
	artifact.TenantID = strings.TrimSpace(artifact.TenantID)
	artifact.ProjectID = strings.TrimSpace(artifact.ProjectID)
	artifact.LensID = strings.TrimSpace(artifact.LensID)
	artifact.DomainCanonicalPath = strings.TrimSpace(artifact.DomainCanonicalPath)
	artifact.RequestID = strings.TrimSpace(artifact.RequestID)
	artifact.Result.RequestID = strings.TrimSpace(artifact.Result.RequestID)
	if artifact.RequestID == "" {
		artifact.RequestID = artifact.Result.RequestID
	}
	if artifact.TenantID == "" || artifact.LensID == "" || artifact.RequestID == "" {
		return "", fmt.Errorf("tenantId, lensId and requestId are required")
	}

	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	artifact.UpdatedAt = now

	id := strings.TrimSpace(artifact.ID)
	if id == "" {
		id = r.BuildResultID(artifact.LensID, artifact.RequestID)
	}
	if id == "" {
		return "", fmt.Errorf("failed to build unit dive result id")
	}
	artifact.ID = id

	if _, err := r.col.Doc(id).Set(ctx, artifact); err != nil {
		return "", err
	}
	return id, nil
}

func (r *UnitDiveResultsRepository) Get(ctx context.Context, id string) (*models.UnitDiveResultArtifact, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrUnitDiveResultNotFound
		}
		return nil, err
	}
	var out models.UnitDiveResultArtifact
	if err := doc.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *UnitDiveResultsRepository) GetByLensAndRequestID(ctx context.Context, lensID, requestID string) (*models.UnitDiveResultArtifact, error) {
	lensID = strings.TrimSpace(lensID)
	requestID = strings.TrimSpace(requestID)
	if lensID == "" || requestID == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("lensId", "==", lensID).Where("requestId", "==", requestID).OrderBy("updatedAt", firestore.Desc).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrUnitDiveResultNotFound
	}
	var out models.UnitDiveResultArtifact
	if err := docs[0].DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *UnitDiveResultsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*models.UnitDiveResultArtifact, error) {
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
	out := make([]*models.UnitDiveResultArtifact, 0, len(docs))
	for _, d := range docs {
		var item models.UnitDiveResultArtifact
		if err := d.DataTo(&item); err != nil {
			continue
		}
		out = append(out, &item)
	}
	return out, nil
}
