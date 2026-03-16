package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const watchSubjectsCollection = "horizon_watch_subjects"

type WatchSubjectsRepository struct {
	col *firestore.CollectionRef
}

func NewWatchSubjectsRepository(db *firestore.Client) *WatchSubjectsRepository {
	return &WatchSubjectsRepository{col: db.Collection(watchSubjectsCollection)}
}

func BuildWatchSubjectID(tenantID, projectID, lensID, packID, userID string, kind models.WatchSubjectKind, anchorCanonicalPath string) string {
	base := strings.Join([]string{
		strings.TrimSpace(tenantID),
		strings.TrimSpace(projectID),
		strings.TrimSpace(lensID),
		strings.TrimSpace(packID),
		strings.TrimSpace(userID),
		string(kind),
		strings.TrimSpace(anchorCanonicalPath),
	}, "|")
	sum := sha256.Sum256([]byte(base))
	return "wsub_" + hex.EncodeToString(sum[:12])
}

func normalizeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func (r *WatchSubjectsRepository) Upsert(ctx context.Context, subject *models.WatchSubject) error {
	if r == nil || r.col == nil || subject == nil {
		return ErrInvalidDocument
	}
	subject.DomainModes = normalizeStrings(subject.DomainModes)
	subject.Roles = normalizeStrings(subject.Roles)
	subject.LensBindings = normalizeStrings(subject.LensBindings)
	subject.UniversePaths = normalizeStrings(subject.UniversePaths)
	if err := subject.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if subject.CreatedAt.IsZero() {
		subject.CreatedAt = now
	}
	subject.UpdatedAt = now
	if strings.TrimSpace(subject.ID) == "" {
		subject.ID = BuildWatchSubjectID(
			subject.TenantID,
			subject.ProjectID,
			subject.LensID,
			subject.PackID,
			subject.UserID,
			subject.SubjectKind,
			subject.AnchorCanonicalPath,
		)
	}
	_, err := r.col.Doc(strings.TrimSpace(subject.ID)).Set(ctx, subject)
	return err
}

func (r *WatchSubjectsRepository) Get(ctx context.Context, id string) (*models.WatchSubject, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var subject models.WatchSubject
	if err := snap.DataTo(&subject); err != nil {
		return nil, err
	}
	if strings.TrimSpace(subject.ID) == "" {
		subject.ID = snap.Ref.ID
	}
	return &subject, nil
}

func (r *WatchSubjectsRepository) GetByIdentity(ctx context.Context, tenantID, projectID, lensID, packID, userID string, kind models.WatchSubjectKind, anchorCanonicalPath string) (*models.WatchSubject, error) {
	return r.Get(ctx, BuildWatchSubjectID(tenantID, projectID, lensID, packID, userID, kind, anchorCanonicalPath))
}

func (r *WatchSubjectsRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.WatchSubject, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchSubject, 0, len(docs))
	for _, doc := range docs {
		var subject models.WatchSubject
		if err := doc.DataTo(&subject); err != nil {
			continue
		}
		if strings.TrimSpace(subject.ID) == "" {
			subject.ID = doc.Ref.ID
		}
		out = append(out, &subject)
	}
	return out, nil
}

func (r *WatchSubjectsRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.WatchSubject, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(lensID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("lensBindings", "array-contains", strings.TrimSpace(lensID)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchSubject, 0, len(docs))
	for _, doc := range docs {
		var subject models.WatchSubject
		if err := doc.DataTo(&subject); err != nil {
			continue
		}
		if strings.TrimSpace(subject.ID) == "" {
			subject.ID = doc.Ref.ID
		}
		out = append(out, &subject)
	}
	return out, nil
}

func (r *WatchSubjectsRepository) ListByAnchorCanonicalPath(ctx context.Context, tenantID, canonicalPath string, limit int) ([]*models.WatchSubject, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(canonicalPath) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	tenants := []string{strings.TrimSpace(tenantID)}
	if strings.TrimSpace(tenantID) != "global" {
		tenants = append(tenants, "global")
	}
	docs, err := r.col.Where("tenantId", "in", tenants).
		Where("anchorCanonicalPath", "==", strings.TrimSpace(canonicalPath)).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchSubject, 0, len(docs))
	for _, doc := range docs {
		var subject models.WatchSubject
		if err := doc.DataTo(&subject); err != nil {
			continue
		}
		if strings.TrimSpace(subject.ID) == "" {
			subject.ID = doc.Ref.ID
		}
		out = append(out, &subject)
	}
	return out, nil
}

func (r *WatchSubjectsRepository) ListByUniversePath(ctx context.Context, tenantID, canonicalPath string, limit int) ([]*models.WatchSubject, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(canonicalPath) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	tenants := []string{strings.TrimSpace(tenantID)}
	if strings.TrimSpace(tenantID) != "global" {
		tenants = append(tenants, "global")
	}
	docs, err := r.col.Where("tenantId", "in", tenants).
		Where("universePaths", "array-contains", strings.TrimSpace(canonicalPath)).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchSubject, 0, len(docs))
	for _, doc := range docs {
		var subject models.WatchSubject
		if err := doc.DataTo(&subject); err != nil {
			continue
		}
		if strings.TrimSpace(subject.ID) == "" {
			subject.ID = doc.Ref.ID
		}
		out = append(out, &subject)
	}
	return out, nil
}

func (r *WatchSubjectsRepository) UpdateStatus(ctx context.Context, id string, status models.WatchSubjectStatus) error {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}
	switch status {
	case models.WatchSubjectStatusActive, models.WatchSubjectStatusPaused, models.WatchSubjectStatusDeleted:
	default:
		return fmt.Errorf("invalid status")
	}
	_, err := r.col.Doc(strings.TrimSpace(id)).Update(ctx, []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}
