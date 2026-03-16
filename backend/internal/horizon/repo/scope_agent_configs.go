package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type ScopeAgentConfigsRepository struct {
	col *firestore.CollectionRef
}

func NewScopeAgentConfigsRepository(db *firestore.Client) *ScopeAgentConfigsRepository {
	return &ScopeAgentConfigsRepository{col: db.Collection("horizon_scope_agent_configs")}
}

func scopeAgentConfigDocID(cfg *models.ScopeAgentConfig) string {
	if cfg == nil {
		return ""
	}
	tenantID := strings.TrimSpace(cfg.TenantID)
	scopeType := strings.TrimSpace(cfg.ScopeType)
	scope := strings.ReplaceAll(strings.Trim(strings.TrimSpace(cfg.ScopeCanonicalPath), "/"), "/", "_")
	if packID := strings.TrimSpace(cfg.PackID); packID != "" {
		return fmt.Sprintf("%s_%s_%s_%s", tenantID, packID, scopeType, scope)
	}
	return fmt.Sprintf("%s_%s_%s", tenantID, scopeType, scope)
}

func (r *ScopeAgentConfigsRepository) Upsert(ctx context.Context, cfg *models.ScopeAgentConfig) error {
	if r == nil || r.col == nil || cfg == nil {
		return ErrInvalidDocument
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = scopeAgentConfigDocID(cfg)
	}
	_, err := r.col.Doc(strings.TrimSpace(cfg.ID)).Set(ctx, cfg)
	return err
}

func (r *ScopeAgentConfigsRepository) Get(ctx context.Context, id string) (*models.ScopeAgentConfig, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var cfg models.ScopeAgentConfig
	if err := snap.DataTo(&cfg); err != nil {
		return nil, err
	}
	if cfg.ID == "" {
		cfg.ID = snap.Ref.ID
	}
	return &cfg, nil
}

func (r *ScopeAgentConfigsRepository) ListActiveByCanonicalPath(ctx context.Context, tenantID, canonicalPath string) ([]*models.ScopeAgentConfig, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(canonicalPath) == "" {
		return nil, ErrInvalidDocument
	}
	tenants := []string{strings.TrimSpace(tenantID)}
	if strings.TrimSpace(tenantID) != "global" {
		tenants = append(tenants, "global")
	}
	docs, err := r.col.
		Where("tenantId", "in", tenants).
		Where("scopeCanonicalPath", "==", strings.TrimSpace(canonicalPath)).
		Where("status", "==", string(models.ScopeAgentStatusRunning)).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentConfig, 0, len(docs))
	for _, doc := range docs {
		var cfg models.ScopeAgentConfig
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}
		if cfg.ID == "" {
			cfg.ID = doc.Ref.ID
		}
		out = append(out, &cfg)
	}
	return out, nil
}

func (r *ScopeAgentConfigsRepository) ListActiveBySubjectID(ctx context.Context, tenantID, subjectID string) ([]*models.ScopeAgentConfig, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, ErrInvalidDocument
	}
	tenants := []string{strings.TrimSpace(tenantID)}
	if strings.TrimSpace(tenantID) != "global" {
		tenants = append(tenants, "global")
	}
	docs, err := r.col.
		Where("tenantId", "in", tenants).
		Where("subjectId", "==", strings.TrimSpace(subjectID)).
		Where("status", "==", string(models.ScopeAgentStatusRunning)).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentConfig, 0, len(docs))
	for _, doc := range docs {
		var cfg models.ScopeAgentConfig
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}
		if cfg.ID == "" {
			cfg.ID = doc.Ref.ID
		}
		out = append(out, &cfg)
	}
	return out, nil
}

func (r *ScopeAgentConfigsRepository) ListByPack(ctx context.Context, tenantID, packID string) ([]*models.ScopeAgentConfig, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(packID) == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("packId", "==", strings.TrimSpace(packID)).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentConfig, 0, len(docs))
	for _, doc := range docs {
		var cfg models.ScopeAgentConfig
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}
		if cfg.ID == "" {
			cfg.ID = doc.Ref.ID
		}
		out = append(out, &cfg)
	}
	return out, nil
}

func (r *ScopeAgentConfigsRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.ScopeAgentConfig, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentConfig, 0, len(docs))
	for _, doc := range docs {
		var cfg models.ScopeAgentConfig
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}
		if cfg.ID == "" {
			cfg.ID = doc.Ref.ID
		}
		out = append(out, &cfg)
	}
	return out, nil
}
