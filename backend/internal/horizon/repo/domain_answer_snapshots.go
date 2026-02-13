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

var ErrDomainAnswerSnapshotNotFound = errors.New("domain answer snapshot not found")

type DomainAnswerSnapshotsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewDomainAnswerSnapshotsRepository(db *firestore.Client) *DomainAnswerSnapshotsRepository {
	if db == nil {
		return nil
	}
	return &DomainAnswerSnapshotsRepository{
		db:  db,
		col: db.Collection("domain_answer_snapshots"),
	}
}

func (r *DomainAnswerSnapshotsRepository) BuildSnapshotID(lensID, domainID string) string {
	l := strings.TrimSpace(strings.ToLower(lensID))
	d := strings.TrimSpace(strings.ToLower(domainID))
	if l == "" || d == "" {
		return ""
	}
	sum := sha1.Sum([]byte(l + "|" + d))
	return "das_" + hex.EncodeToString(sum[:])
}

func (r *DomainAnswerSnapshotsRepository) Upsert(ctx context.Context, snapshot *models.DomainAnswerSnapshot) (string, error) {
	if snapshot == nil {
		return "", ErrInvalidDocument
	}
	snapshot.LensID = strings.TrimSpace(snapshot.LensID)
	snapshot.DomainID = strings.TrimSpace(snapshot.DomainID)
	if snapshot.LensID == "" || snapshot.DomainID == "" {
		return "", fmt.Errorf("lensId and domainId are required")
	}
	if strings.TrimSpace(snapshot.ReadinessStatus) == "" {
		snapshot.ReadinessStatus = models.DomainReadinessReadyWithGaps
	}
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	if snapshot.AssembledAt.IsZero() {
		snapshot.AssembledAt = now
	}
	id := strings.TrimSpace(snapshot.ID)
	if id == "" {
		id = r.BuildSnapshotID(snapshot.LensID, snapshot.DomainID)
	}
	if id == "" {
		return "", fmt.Errorf("failed to build snapshot id")
	}
	snapshot.ID = id
	if _, err := r.col.Doc(id).Set(ctx, snapshot); err != nil {
		return "", err
	}
	return id, nil
}

func (r *DomainAnswerSnapshotsRepository) Get(ctx context.Context, id string) (*models.DomainAnswerSnapshot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrDomainAnswerSnapshotNotFound
		}
		return nil, err
	}
	var out models.DomainAnswerSnapshot
	if err := snap.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *DomainAnswerSnapshotsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*models.DomainAnswerSnapshot, error) {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 20
	}
	q := r.col.Where("lensId", "==", lensID).OrderBy("assembledAt", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.DomainAnswerSnapshot, 0, len(docs))
	for _, d := range docs {
		var item models.DomainAnswerSnapshot
		if err := d.DataTo(&item); err != nil {
			continue
		}
		out = append(out, &item)
	}
	return out, nil
}

func (r *DomainAnswerSnapshotsRepository) GetByLensAndDomainID(ctx context.Context, lensID, domainID string) (*models.DomainAnswerSnapshot, error) {
	lensID = strings.TrimSpace(lensID)
	domainID = strings.TrimSpace(domainID)
	if lensID == "" || domainID == "" {
		return nil, ErrInvalidDocument
	}
	q := r.col.Where("lensId", "==", lensID).Where("domainId", "==", domainID).OrderBy("assembledAt", firestore.Desc).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrDomainAnswerSnapshotNotFound
	}
	var out models.DomainAnswerSnapshot
	if err := docs[0].DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *DomainAnswerSnapshotsRepository) DeleteByLens(ctx context.Context, lensID string) error {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return ErrInvalidDocument
	}
	const batchSize = 500
	for {
		docs, err := r.col.Where("lensId", "==", lensID).Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return nil
		}
		batch := r.db.Batch()
		for _, d := range docs {
			batch.Delete(d.Ref)
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
}

