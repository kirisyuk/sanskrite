package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrFragmentVersionNotFound = errors.New("horizon fragment version not found")

// HorizonFragmentVersionsRepository репозиторий для работы с версиями HorizonFragment
type HorizonFragmentVersionsRepository struct {
	col *firestore.CollectionRef
}

// NewHorizonFragmentVersionsRepository создает новый репозиторий
func NewHorizonFragmentVersionsRepository(db *firestore.Client) *HorizonFragmentVersionsRepository {
	return &HorizonFragmentVersionsRepository{
		col: db.Collection("horizon_fragment_versions"),
	}
}

// HorizonFragmentVersion представляет версию HorizonFragment
type HorizonFragmentVersion struct {
	VersionID     string                 `json:"versionId" firestore:"versionId"`
	FragmentID    string                 `json:"fragmentId" firestore:"fragmentId"`
	TenantID      string                 `json:"tenantId" firestore:"tenantId"`
	State         map[string]models.MetricValue `json:"state" firestore:"state"`
	SourceFrameID string                 `json:"sourceFrameId" firestore:"sourceFrameId"`
	Timestamp     time.Time              `json:"timestamp" firestore:"timestamp"`
	CreatedAt     time.Time              `json:"createdAt" firestore:"createdAt"`
}

// Create создает новую версию HorizonFragment
func (r *HorizonFragmentVersionsRepository) Create(ctx context.Context, version *HorizonFragmentVersion) (string, error) {
	if version == nil {
		return "", ErrInvalidDocument
	}
	if version.FragmentID == "" {
		return "", fmt.Errorf("fragmentId is required")
	}

	now := time.Now().UTC()
	if version.Timestamp.IsZero() {
		version.Timestamp = now
	}
	version.CreatedAt = now

	doc := r.col.NewDoc()
	version.VersionID = doc.ID

	if _, err := doc.Set(ctx, version); err != nil {
		return "", err
	}
	return version.VersionID, nil
}

// ListByFragment получает список версий для фрагмента
func (r *HorizonFragmentVersionsRepository) ListByFragment(ctx context.Context, tenantID, fragmentID string, limit, offset int) ([]*HorizonFragmentVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("fragmentId", "==", fragmentID).OrderBy("timestamp", firestore.Desc).Limit(limit).Offset(offset)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var versions []*HorizonFragmentVersion
	for _, d := range docs {
		var version HorizonFragmentVersion
		if err := d.DataTo(&version); err != nil {
			continue
		}
		versions = append(versions, &version)
	}
	return versions, nil
}

// GetByVersionID получает версию по ID
func (r *HorizonFragmentVersionsRepository) GetByVersionID(ctx context.Context, versionID string) (*HorizonFragmentVersion, error) {
	if versionID == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(versionID).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrFragmentVersionNotFound
	}
	var version HorizonFragmentVersion
	if err := snap.DataTo(&version); err != nil {
		return nil, err
	}
	return &version, nil
}

