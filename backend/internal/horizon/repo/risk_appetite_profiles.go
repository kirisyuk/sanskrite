package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrRiskAppetiteProfileNotFound = errors.New("risk appetite profile not found")

type RiskAppetiteProfilesRepository struct {
	col *firestore.CollectionRef
}

func NewRiskAppetiteProfilesRepository(client *firestore.Client) *RiskAppetiteProfilesRepository {
	return &RiskAppetiteProfilesRepository{
		col: client.Collection("horizon_risk_appetite_profiles"),
	}
}

func appetiteDocID(tenantID, scopePath, polarity string) string {
	id := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(polarity) + "|" + strings.TrimSpace(scopePath)
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, ":", "_")
	return id
}

func (r *RiskAppetiteProfilesRepository) Get(ctx context.Context, tenantID, scopePath, polarity string) (*models.RiskAppetiteProfile, error) {
	docID := appetiteDocID(tenantID, scopePath, polarity)
	snap, err := r.col.Doc(docID).Get(ctx)
	if err != nil || !snap.Exists() {
		return nil, ErrRiskAppetiteProfileNotFound
	}
	var p models.RiskAppetiteProfile
	if err := snap.DataTo(&p); err != nil {
		return nil, err
	}
	p.ID = docID
	return &p, nil
}

func (r *RiskAppetiteProfilesRepository) Upsert(ctx context.Context, profile *models.RiskAppetiteProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now().UTC()
	}
	profile.UpdatedAt = time.Now().UTC()
	docID := appetiteDocID(profile.TenantID, profile.ScopePath, profile.Polarity)
	profile.ID = docID
	_, err := r.col.Doc(docID).Set(ctx, profile)
	return err
}

