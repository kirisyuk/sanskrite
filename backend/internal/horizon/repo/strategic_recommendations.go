package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const strategicRecommendationsCollection = "horizon_strategic_recommendations"

var ErrStrategicRecommendationNotFound = errors.New("strategic recommendation not found")

type StrategicRecommendationsListFilter struct {
	Status    string
	ProjectID string
	LensID    string
	Limit     int
}

type StrategicRecommendationsRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewStrategicRecommendationsRepository(db *firestore.Client) *StrategicRecommendationsRepository {
	return &StrategicRecommendationsRepository{
		db:  db,
		col: db.Collection(strategicRecommendationsCollection),
	}
}

func StrategicRecommendationID(dedupeKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(dedupeKey)))
	return "srec_" + hex.EncodeToString(sum[:12])
}

func NormalizeStrategicRecommendationTargetFamilies(families []string) []string {
	normalized := make([]string, 0, len(families))
	seen := map[string]struct{}{}
	for _, raw := range families {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		s = strings.Join(strings.Fields(s), " ")
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		normalized = append(normalized, s)
	}
	sort.Strings(normalized)
	return normalized
}

func BuildStrategicRecommendationDedupeKey(tenantID, subjectCanonicalPath string, recommendationType models.StrategicRecommendationType, families []string) string {
	return strings.Join([]string{
		strings.TrimSpace(tenantID),
		strings.TrimSpace(subjectCanonicalPath),
		string(recommendationType),
		strings.Join(NormalizeStrategicRecommendationTargetFamilies(families), ","),
	}, "|")
}

func (r *StrategicRecommendationsRepository) Get(ctx context.Context, id string) (*models.StrategicRecommendation, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return nil, ErrStrategicRecommendationNotFound
		}
		return nil, err
	}
	var rec models.StrategicRecommendation
	if err := snap.DataTo(&rec); err != nil {
		return nil, err
	}
	rec.ID = snap.Ref.ID
	return &rec, nil
}

func (r *StrategicRecommendationsRepository) ListByTenant(ctx context.Context, tenantID string, filter StrategicRecommendationsListFilter) ([]*models.StrategicRecommendation, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID))
	if status := strings.TrimSpace(filter.Status); status != "" {
		q = q.Where("status", "==", strings.ToLower(status))
	}
	if projectID := strings.TrimSpace(filter.ProjectID); projectID != "" {
		q = q.Where("projectId", "==", projectID)
	}
	if lensID := strings.TrimSpace(filter.LensID); lensID != "" {
		q = q.Where("lensId", "==", lensID)
	}
	docs, err := q.OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.StrategicRecommendation, 0, len(docs))
	for _, doc := range docs {
		var rec models.StrategicRecommendation
		if err := doc.DataTo(&rec); err != nil {
			continue
		}
		rec.ID = doc.Ref.ID
		out = append(out, &rec)
	}
	return out, nil
}

func (r *StrategicRecommendationsRepository) CountPendingByTenant(ctx context.Context, tenantID string) (int, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return 0, ErrInvalidDocument
	}
	statuses := []string{
		string(models.StrategicRecommendationStatusPending),
		string(models.StrategicRecommendationStatusApproved),
		string(models.StrategicRecommendationStatusExecuting),
	}
	total := 0
	for _, status := range statuses {
		docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).
			Where("status", "==", status).
			Documents(ctx).
			GetAll()
		if err != nil {
			return 0, err
		}
		total += len(docs)
	}
	return total, nil
}

func (r *StrategicRecommendationsRepository) Update(ctx context.Context, rec *models.StrategicRecommendation) error {
	if r == nil || r.col == nil || rec == nil {
		return ErrInvalidDocument
	}
	if err := rec.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = StrategicRecommendationID(rec.DedupeKey)
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	rec.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(rec.ID)).Set(ctx, rec)
	return err
}

func (r *StrategicRecommendationsRepository) UpdateStatus(ctx context.Context, id string, mutate func(rec *models.StrategicRecommendation) error) (*models.StrategicRecommendation, error) {
	if r == nil || r.db == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	var updated *models.StrategicRecommendation
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := r.col.Doc(strings.TrimSpace(id))
		snap, err := tx.Get(ref)
		if err != nil {
			if grpcstatus.Code(err) == codes.NotFound {
				return ErrStrategicRecommendationNotFound
			}
			return err
		}
		var rec models.StrategicRecommendation
		if err := snap.DataTo(&rec); err != nil {
			return err
		}
		rec.ID = snap.Ref.ID
		if mutate != nil {
			if err := mutate(&rec); err != nil {
				return err
			}
		}
		rec.UpdatedAt = time.Now().UTC()
		if err := rec.Validate(); err != nil {
			return err
		}
		if err := tx.Set(ref, &rec); err != nil {
			return err
		}
		copyRec := rec
		updated = &copyRec
		return nil
	})
	return updated, err
}

func shouldReactivateStrategicRecommendation(existing, incoming *models.StrategicRecommendation) bool {
	if existing == nil || incoming == nil {
		return false
	}
	if incoming.Confidence > existing.Confidence+0.15 {
		return true
	}
	if len(incoming.ExpectedMissingTargets) != len(existing.ExpectedMissingTargets) {
		return true
	}
	for i := range incoming.ExpectedMissingTargets {
		if incoming.ExpectedMissingTargets[i] != existing.ExpectedMissingTargets[i] {
			return true
		}
	}
	return false
}

func (r *StrategicRecommendationsRepository) CreateOrRefresh(ctx context.Context, rec *models.StrategicRecommendation) (*models.StrategicRecommendation, bool, error) {
	if r == nil || r.db == nil || rec == nil {
		return nil, false, ErrInvalidDocument
	}
	if strings.TrimSpace(rec.DedupeKey) == "" {
		return nil, false, fmt.Errorf("dedupeKey is required")
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = StrategicRecommendationID(rec.DedupeKey)
	}
	now := time.Now().UTC()
	rec.ExpectedMissingTargets = NormalizeStrategicRecommendationTargetFamilies(rec.ExpectedMissingTargets)
	created := false
	var result *models.StrategicRecommendation
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := r.col.Doc(strings.TrimSpace(rec.ID))
		snap, err := tx.Get(ref)
		if err != nil && grpcstatus.Code(err) != codes.NotFound {
			return err
		}
		if err != nil && grpcstatus.Code(err) == codes.NotFound {
			rec.CreatedAt = now
			rec.UpdatedAt = now
			if rec.Status == "" {
				rec.Status = models.StrategicRecommendationStatusPending
			}
			if err := rec.Validate(); err != nil {
				return err
			}
			if err := tx.Set(ref, rec); err != nil {
				return err
			}
			copyRec := *rec
			result = &copyRec
			created = true
			return nil
		}

		var existing models.StrategicRecommendation
		if err := snap.DataTo(&existing); err != nil {
			return err
		}
		existing.ID = snap.Ref.ID
		if existing.CreatedAt.IsZero() {
			existing.CreatedAt = now
		}
		existing.ProjectID = firstNonEmpty(existing.ProjectID, rec.ProjectID)
		existing.LensID = firstNonEmpty(existing.LensID, rec.LensID)
		existing.SubjectType = rec.SubjectType
		existing.SubjectCanonicalPath = rec.SubjectCanonicalPath
		existing.RecommendationType = rec.RecommendationType
		existing.Priority = rec.Priority
		if rec.Confidence > existing.Confidence {
			existing.Confidence = rec.Confidence
		}
		if strings.TrimSpace(rec.Headline) != "" {
			existing.Headline = rec.Headline
		}
		if strings.TrimSpace(rec.Reason) != "" {
			existing.Reason = rec.Reason
		}
		if len(rec.Evidence) > 0 {
			existing.Evidence = rec.Evidence
		}
		existing.ExpectedMissingTargets = rec.ExpectedMissingTargets
		existing.CurrentCoverageStats = rec.CurrentCoverageStats
		existing.ProposedAction = rec.ProposedAction
		existing.DomainContractHint = firstNonEmpty(existing.DomainContractHint, rec.DomainContractHint)
		existing.DedupeKey = rec.DedupeKey
		if existing.Status == models.StrategicRecommendationStatusDismissed ||
			existing.Status == models.StrategicRecommendationStatusCompleted ||
			existing.Status == models.StrategicRecommendationStatusFailed ||
			existing.Status == models.StrategicRecommendationStatusStale {
			if shouldReactivateStrategicRecommendation(&existing, rec) {
				existing.Status = models.StrategicRecommendationStatusPending
				existing.Execution = models.StrategicRecommendationExecution{}
			}
		}
		existing.UpdatedAt = now
		if err := existing.Validate(); err != nil {
			return err
		}
		if err := tx.Set(ref, &existing); err != nil {
			return err
		}
		copyRec := existing
		result = &copyRec
		return nil
	})
	return result, created, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}
