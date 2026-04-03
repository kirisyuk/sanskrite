package repo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type AnalyticalAggregatesRepository struct {
	col *firestore.CollectionRef
}

func NewAnalyticalAggregatesRepository(db *firestore.Client) *AnalyticalAggregatesRepository {
	return &AnalyticalAggregatesRepository{col: db.Collection("horizon_analytical_aggregates")}
}

func (r *AnalyticalAggregatesRepository) Upsert(ctx context.Context, aggregate *models.AnalyticalAggregate) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("analytical aggregates repo not configured")
	}
	if aggregate == nil {
		return ErrInvalidDocument
	}
	aggregate.Normalize()
	if strings.TrimSpace(aggregate.TenantID) == "" || strings.TrimSpace(aggregate.ScopeRef) == "" || strings.TrimSpace(aggregate.AggregateType) == "" {
		return ErrInvalidDocument
	}
	docID := analyticalAggregateDocID(aggregate)
	aggregate.ID = docID
	_, err := r.col.Doc(docID).Set(ctx, aggregate)
	return err
}

func (r *AnalyticalAggregatesRepository) ListByScopeRefs(ctx context.Context, tenantID string, scopeRefs []string, limit int) ([]*models.AnalyticalAggregate, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("analytical aggregates repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	scopeRefs = compactUniqueScopeRefs(scopeRefs)
	if tenantID == "" || len(scopeRefs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 120
	}
	out := make([]*models.AnalyticalAggregate, 0, minAnalytical(limit, len(scopeRefs)*3))
	seen := make(map[string]struct{})
	for start := 0; start < len(scopeRefs) && len(out) < limit; start += 10 {
		end := start + 10
		if end > len(scopeRefs) {
			end = len(scopeRefs)
		}
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		docs, err := r.col.
			Where("tenantId", "==", tenantID).
			Where("scopeRef", "in", scopeRefs[start:end]).
			Limit(remaining).
			Documents(ctx).
			GetAll()
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			var aggregate models.AnalyticalAggregate
			if err := doc.DataTo(&aggregate); err != nil {
				continue
			}
			if aggregate.ID == "" {
				aggregate.ID = doc.Ref.ID
			}
			if _, ok := seen[aggregate.ID]; ok {
				continue
			}
			seen[aggregate.ID] = struct{}{}
			out = append(out, &aggregate)
		}
	}
	sortAggregates(out)
	return out, nil
}

func (r *AnalyticalAggregatesRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.AnalyticalAggregate, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("analytical aggregates repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	if tenantID == "" || lensID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 80
	}
	docs, err := r.col.
		Where("tenantId", "==", tenantID).
		Where("lensRefs", "array-contains", lensID).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.AnalyticalAggregate, 0, len(docs))
	for _, doc := range docs {
		var aggregate models.AnalyticalAggregate
		if err := doc.DataTo(&aggregate); err != nil {
			continue
		}
		if aggregate.ID == "" {
			aggregate.ID = doc.Ref.ID
		}
		out = append(out, &aggregate)
	}
	sortAggregates(out)
	return out, nil
}

func analyticalAggregateDocID(aggregate *models.AnalyticalAggregate) string {
	parts := []string{
		strings.TrimSpace(aggregate.TenantID),
		strings.TrimSpace(aggregate.ScopeRef),
		strings.TrimSpace(aggregate.AggregateType),
		strings.Join(aggregate.LensRefs, "|"),
		strings.Join(aggregate.WatchSubjectIDs, "|"),
		strings.TrimSpace(aggregate.BasisDigest),
	}
	return "aa_" + shortAnalyticalHash(strings.Join(parts, "\n"))
}

func sortAggregates(items []*models.AnalyticalAggregate) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SeverityScore == items[j].SeverityScore {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].SeverityScore > items[j].SeverityScore
	})
}
