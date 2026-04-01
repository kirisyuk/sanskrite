package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WatchPortfoliosRepository struct {
	col *firestore.CollectionRef
}

func NewWatchPortfoliosRepository(db *firestore.Client) *WatchPortfoliosRepository {
	return &WatchPortfoliosRepository{col: db.Collection("horizon_watch_portfolios")}
}

func watchPortfolioDocID(portfolio *models.WatchPortfolio) string {
	if portfolio == nil {
		return ""
	}
	name := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(portfolio.Name)), " ", "_")
	if strings.TrimSpace(portfolio.UserID) != "" {
		return fmt.Sprintf("%s_%s_%s", strings.TrimSpace(portfolio.TenantID), strings.TrimSpace(portfolio.UserID), name)
	}
	return fmt.Sprintf("%s_%s", strings.TrimSpace(portfolio.TenantID), name)
}

func (r *WatchPortfoliosRepository) Upsert(ctx context.Context, portfolio *models.WatchPortfolio) error {
	if r == nil || r.col == nil || portfolio == nil {
		return ErrInvalidDocument
	}
	if err := portfolio.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if portfolio.CreatedAt.IsZero() {
		portfolio.CreatedAt = now
	}
	portfolio.UpdatedAt = now
	for i := range portfolio.Entries {
		if portfolio.Entries[i].CreatedAt.IsZero() {
			portfolio.Entries[i].CreatedAt = now
		}
		portfolio.Entries[i].UpdatedAt = now
	}
	if strings.TrimSpace(portfolio.ID) == "" {
		portfolio.ID = watchPortfolioDocID(portfolio)
	}
	_, err := r.col.Doc(strings.TrimSpace(portfolio.ID)).Set(ctx, portfolio)
	return err
}

func (r *WatchPortfoliosRepository) Get(ctx context.Context, id string) (*models.WatchPortfolio, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var portfolio models.WatchPortfolio
	if err := snap.DataTo(&portfolio); err != nil {
		return nil, err
	}
	if strings.TrimSpace(portfolio.ID) == "" {
		portfolio.ID = snap.Ref.ID
	}
	return &portfolio, nil
}

func (r *WatchPortfoliosRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.WatchPortfolio, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("updatedAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.WatchPortfolio, 0, len(docs))
	for _, doc := range docs {
		var portfolio models.WatchPortfolio
		if err := doc.DataTo(&portfolio); err != nil {
			continue
		}
		if strings.TrimSpace(portfolio.ID) == "" {
			portfolio.ID = doc.Ref.ID
		}
		out = append(out, &portfolio)
	}
	return out, nil
}
