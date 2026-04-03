package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type AnalyticalPrimitivesRepository struct {
	col *firestore.CollectionRef
}

func NewAnalyticalPrimitivesRepository(db *firestore.Client) *AnalyticalPrimitivesRepository {
	return &AnalyticalPrimitivesRepository{col: db.Collection("horizon_analytical_primitives")}
}

func (r *AnalyticalPrimitivesRepository) Upsert(ctx context.Context, primitive *models.AnalyticalPrimitive) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("analytical primitives repo not configured")
	}
	if primitive == nil {
		return ErrInvalidDocument
	}
	primitive.Normalize()
	if strings.TrimSpace(primitive.TenantID) == "" || strings.TrimSpace(primitive.ScopeRef) == "" || strings.TrimSpace(primitive.PrimitiveType) == "" {
		return ErrInvalidDocument
	}
	docID := analyticalPrimitiveDocID(primitive)
	primitive.ID = docID
	_, err := r.col.Doc(docID).Set(ctx, primitive)
	return err
}

func (r *AnalyticalPrimitivesRepository) ListByScopeRefs(ctx context.Context, tenantID string, scopeRefs []string, limit int) ([]*models.AnalyticalPrimitive, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("analytical primitives repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	scopeRefs = compactUniqueScopeRefs(scopeRefs)
	if tenantID == "" || len(scopeRefs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}

	out := make([]*models.AnalyticalPrimitive, 0, minAnalytical(limit, len(scopeRefs)*8))
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
			var primitive models.AnalyticalPrimitive
			if err := doc.DataTo(&primitive); err != nil {
				continue
			}
			if primitive.ID == "" {
				primitive.ID = doc.Ref.ID
			}
			if _, ok := seen[primitive.ID]; ok {
				continue
			}
			seen[primitive.ID] = struct{}{}
			out = append(out, &primitive)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SeverityScore == out[j].SeverityScore {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].SeverityScore > out[j].SeverityScore
	})
	return out, nil
}

func (r *AnalyticalPrimitivesRepository) ListByImpactedRefs(ctx context.Context, tenantID string, refs []string, limit int) ([]*models.AnalyticalPrimitive, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("analytical primitives repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	refs = compactUniqueScopeRefs(refs)
	if tenantID == "" || len(refs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 240
	}
	out := make([]*models.AnalyticalPrimitive, 0, limit)
	seen := make(map[string]struct{})
	for _, field := range []struct {
		name  string
		array bool
	}{
		{name: "scopeRef"},
		{name: "basisRefs", array: true},
		{name: "productRefs", array: true},
		{name: "nodeRefs", array: true},
		{name: "routeRefs", array: true},
		{name: "zoneRefs", array: true},
		{name: "affectedRefs", array: true},
	} {
		if len(out) >= limit {
			break
		}
		items, err := r.listByFieldRefs(ctx, tenantID, field.name, refs, limit-len(out), field.array)
		if err != nil {
			return nil, err
		}
		for _, primitive := range items {
			if primitive == nil || strings.TrimSpace(primitive.ID) == "" {
				continue
			}
			if _, ok := seen[primitive.ID]; ok {
				continue
			}
			seen[primitive.ID] = struct{}{}
			out = append(out, primitive)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SeverityScore == out[j].SeverityScore {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].SeverityScore > out[j].SeverityScore
	})
	return out, nil
}

func (r *AnalyticalPrimitivesRepository) ListByIDs(ctx context.Context, ids []string) ([]*models.AnalyticalPrimitive, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("analytical primitives repo not configured")
	}
	ids = compactUniqueScopeRefs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]*models.AnalyticalPrimitive, 0, len(ids))
	for _, id := range ids {
		doc, err := r.col.Doc(id).Get(ctx)
		if err != nil {
			continue
		}
		var primitive models.AnalyticalPrimitive
		if err := doc.DataTo(&primitive); err != nil {
			continue
		}
		if primitive.ID == "" {
			primitive.ID = doc.Ref.ID
		}
		out = append(out, &primitive)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SeverityScore == out[j].SeverityScore {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].SeverityScore > out[j].SeverityScore
	})
	return out, nil
}

func (r *AnalyticalPrimitivesRepository) listByFieldRefs(ctx context.Context, tenantID, field string, refs []string, limit int, array bool) ([]*models.AnalyticalPrimitive, error) {
	if limit <= 0 {
		return nil, nil
	}
	chunkSize := 10
	if array {
		chunkSize = 10
	}
	out := make([]*models.AnalyticalPrimitive, 0, limit)
	for start := 0; start < len(refs) && len(out) < limit; start += chunkSize {
		end := start + chunkSize
		if end > len(refs) {
			end = len(refs)
		}
		query := r.col.Where("tenantId", "==", tenantID)
		if array {
			query = query.Where(field, "array-contains-any", refs[start:end])
		} else {
			query = query.Where(field, "in", refs[start:end])
		}
		docs, err := query.Limit(limit - len(out)).Documents(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			var primitive models.AnalyticalPrimitive
			if err := doc.DataTo(&primitive); err != nil {
				continue
			}
			if primitive.ID == "" {
				primitive.ID = doc.Ref.ID
			}
			out = append(out, &primitive)
		}
	}
	return out, nil
}

func analyticalPrimitiveDocID(primitive *models.AnalyticalPrimitive) string {
	parts := []string{
		strings.TrimSpace(primitive.TenantID),
		strings.TrimSpace(primitive.ScopeRef),
		strings.TrimSpace(primitive.PrimitiveType),
		strings.TrimSpace(primitive.SubjectRef),
		strings.Join(primitive.ProductRefs, "|"),
	}
	if strings.TrimSpace(primitive.BasisDigest) != "" {
		parts = append(parts, strings.TrimSpace(primitive.BasisDigest))
	}
	return "ap_" + shortAnalyticalHash(strings.Join(parts, "\n"))
}

func compactUniqueScopeRefs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
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

func shortAnalyticalHash(in string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(in)))
	return hex.EncodeToString(sum[:10])
}

func minAnalytical(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func analyticalStaleAt(from time.Time, ttl time.Duration) *time.Time {
	if from.IsZero() || ttl <= 0 {
		return nil
	}
	value := from.UTC().Add(ttl)
	return &value
}
