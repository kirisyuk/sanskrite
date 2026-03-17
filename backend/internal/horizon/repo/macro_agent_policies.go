package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const macroAgentPoliciesCollection = "horizon_macro_agent_policies"

type MacroAgentPoliciesRepository struct {
	col *firestore.CollectionRef
}

func NewMacroAgentPoliciesRepository(db *firestore.Client) *MacroAgentPoliciesRepository {
	return &MacroAgentPoliciesRepository{col: db.Collection(macroAgentPoliciesCollection)}
}

func macroAgentPolicyDocID(policy *models.MacroAgentPolicy) string {
	if policy == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(policy.TenantID),
		strings.TrimSpace(policy.ProjectID),
		strings.TrimSpace(policy.LensID),
		strings.TrimSpace(policy.AgentClass),
		strings.Trim(strings.TrimSpace(firstNonEmptyMacroPolicy(policy.ArtifactCanonicalKey, policy.AnchorCanonicalPath)), "/"),
	}
	safe := strings.ReplaceAll(strings.Join(parts, "_"), "/", "_")
	safe = strings.ReplaceAll(safe, " ", "_")
	if strings.TrimSpace(safe) == "" {
		return ""
	}
	return safe
}

func (r *MacroAgentPoliciesRepository) Upsert(ctx context.Context, policy *models.MacroAgentPolicy) error {
	if r == nil || r.col == nil || policy == nil {
		return ErrInvalidDocument
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now
	if strings.TrimSpace(policy.ID) == "" {
		policy.ID = macroAgentPolicyDocID(policy)
	}
	_, err := r.col.Doc(strings.TrimSpace(policy.ID)).Set(ctx, policy)
	return err
}

func (r *MacroAgentPoliciesRepository) Get(ctx context.Context, id string) (*models.MacroAgentPolicy, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var policy models.MacroAgentPolicy
	if err := snap.DataTo(&policy); err != nil {
		return nil, err
	}
	if strings.TrimSpace(policy.ID) == "" {
		policy.ID = snap.Ref.ID
	}
	return &policy, nil
}

func (r *MacroAgentPoliciesRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.MacroAgentPolicy, error) {
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
	out := make([]*models.MacroAgentPolicy, 0, len(docs))
	for _, doc := range docs {
		var policy models.MacroAgentPolicy
		if err := doc.DataTo(&policy); err != nil {
			continue
		}
		if strings.TrimSpace(policy.ID) == "" {
			policy.ID = doc.Ref.ID
		}
		out = append(out, &policy)
	}
	return out, nil
}

func (r *MacroAgentPoliciesRepository) ListActiveByTenant(ctx context.Context, tenantID string, limit int) ([]*models.MacroAgentPolicy, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.
		Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("status", "==", string(models.MacroAgentStatusActive)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.MacroAgentPolicy, 0, len(docs))
	for _, doc := range docs {
		var policy models.MacroAgentPolicy
		if err := doc.DataTo(&policy); err != nil {
			continue
		}
		if strings.TrimSpace(policy.ID) == "" {
			policy.ID = doc.Ref.ID
		}
		out = append(out, &policy)
	}
	return out, nil
}

func (r *MacroAgentPoliciesRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.MacroAgentPolicy, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(lensID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	docs, err := r.col.
		Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("lensId", "==", strings.TrimSpace(lensID)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.MacroAgentPolicy, 0, len(docs))
	for _, doc := range docs {
		var policy models.MacroAgentPolicy
		if err := doc.DataTo(&policy); err != nil {
			continue
		}
		if strings.TrimSpace(policy.ID) == "" {
			policy.ID = doc.Ref.ID
		}
		out = append(out, &policy)
	}
	return out, nil
}

func firstNonEmptyMacroPolicy(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (r *MacroAgentPoliciesRepository) Delete(ctx context.Context, id string) error {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(strings.TrimSpace(id)).Delete(ctx)
	return err
}

func (r *MacroAgentPoliciesRepository) ExplainIdentity(policy *models.MacroAgentPolicy) string {
	if policy == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", strings.TrimSpace(policy.AgentClass), strings.TrimSpace(policy.AnchorCanonicalPath), strings.TrimSpace(policy.ArtifactCanonicalKey))
}
