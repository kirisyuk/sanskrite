package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type ProcessedContentRepository struct{ col *firestore.CollectionRef }

func NewProcessedContentRepository(db *firestore.Client) *ProcessedContentRepository {
	return &ProcessedContentRepository{col: db.Collection("processed_content")}
}

type CachedProfile struct {
	TenantID    string                    `json:"tenantId"`
	SourceKey   string                    `json:"sourceKey"`
	ContentHash string                    `json:"contentHash"`
	Profile     *models.SourceProfileData `json:"profile"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
}

func HashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Get returns cached profile if content hash matches and not older than maxAge
func (r *ProcessedContentRepository) Get(ctx context.Context, tenantID, sourceKey, contentHash string, maxAge time.Duration) (*models.SourceProfileData, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceKey", "==", sourceKey).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var c CachedProfile
	if err := docs[0].DataTo(&c); err != nil {
		return nil, err
	}
	if c.ContentHash != contentHash {
		return nil, nil
	}
	if time.Since(c.UpdatedAt) > maxAge {
		return nil, nil
	}
	return c.Profile, nil
}

// Put saves/updates cache entry
func (r *ProcessedContentRepository) Put(ctx context.Context, tenantID, sourceKey, contentHash string, profile *models.SourceProfileData) error {
	now := time.Now().UTC()
	docID := tenantID + "_" + sourceKey
	data := CachedProfile{TenantID: tenantID, SourceKey: sourceKey, ContentHash: contentHash, Profile: profile, UpdatedAt: now}
	_, err := r.col.Doc(docID).Set(ctx, data)
	return err
}
