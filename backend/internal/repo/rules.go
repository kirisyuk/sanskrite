package repo

import (
	"context"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type RulesRepository struct{ col *firestore.CollectionRef }

func NewRulesRepository(db *firestore.Client) *RulesRepository {
	return &RulesRepository{col: db.Collection("rag_rules")}
}

func (r *RulesRepository) Upsert(ctx context.Context, rule *models.Rule) error {
	if rule == nil || rule.ID == "" || rule.TenantID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	_, err := r.col.Doc(rule.ID).Set(ctx, rule)
	return err
}

func (r *RulesRepository) Create(ctx context.Context, rule *models.Rule) (string, error) {
	if rule == nil || rule.TenantID == "" {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	doc := r.col.NewDoc()
	rule.ID = doc.ID
	if _, err := doc.Set(ctx, rule); err != nil {
		return "", err
	}
	return doc.ID, nil
}

// ListByScope returns rules filtered by scope level/id (simple client-side filtering for now)
func (r *RulesRepository) ListByScope(ctx context.Context, tenantID string, scope map[string]any) ([]*models.Rule, error) {
	docs, err := r.col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var res []*models.Rule
	for _, d := range docs {
		var ru models.Rule
		if err := d.DataTo(&ru); err != nil {
			continue
		}
		match := true
		for k, v := range scope {
			if ru.Scope == nil {
				match = false
				break
			}
			if rv, ok := ru.Scope[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if match {
			res = append(res, &ru)
		}
	}
	// sort by priority desc, then updatedAt desc
	sort.Slice(res, func(i, j int) bool {
		if res[i].Priority == res[j].Priority {
			return res[i].UpdatedAt.After(res[j].UpdatedAt)
		}
		return res[i].Priority > res[j].Priority
	})
	return res, nil
}

func (r *RulesRepository) Update(ctx context.Context, id string, patch map[string]any) error {
	if id == "" {
		return ErrInvalidDocument
	}
	patch["updatedAt"] = time.Now().UTC()
	_, err := r.col.Doc(id).Set(ctx, patch, firestore.MergeAll)
	return err
}

func (r *RulesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}
