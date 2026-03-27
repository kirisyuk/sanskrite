// Manual resolve queue persistence (horizon_manual_resolve_queue).
// Indexes: web/firestore.indexes.json → firebase deploy --only firestore:indexes (or equivalent gcloud composite create).
package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const manualResolveQueueCollection = "horizon_manual_resolve_queue"

type ManualResolveQueueRepository struct {
	col *firestore.CollectionRef
}

func NewManualResolveQueueRepository(db *firestore.Client) *ManualResolveQueueRepository {
	if db == nil {
		return nil
	}
	return &ManualResolveQueueRepository{col: db.Collection(manualResolveQueueCollection)}
}

// BuildManualResolveQueueID stable id per tenant + canonical path (kinds merged on upsert).
func BuildManualResolveQueueID(tenantID, canonicalPath string) string {
	base := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(canonicalPath)
	sum := sha256.Sum256([]byte(base))
	return "mrq_" + hex.EncodeToString(sum[:12])
}

func mergeStringSlicesUnique(a, b []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Upsert creates or updates a queue item; enrichment kinds are unioned with any existing document.
func (r *ManualResolveQueueRepository) Upsert(ctx context.Context, item *horizonmodels.ManualResolveQueueItem) error {
	if r == nil || r.col == nil || item == nil {
		return ErrInvalidDocument
	}
	if err := item.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if strings.TrimSpace(item.ID) == "" {
		item.ID = BuildManualResolveQueueID(item.TenantID, item.CanonicalPath)
	}
	docRef := r.col.Doc(strings.TrimSpace(item.ID))
	snap, err := docRef.Get(ctx)
	if err == nil && snap.Exists() {
		var prev horizonmodels.ManualResolveQueueItem
		if uerr := snap.DataTo(&prev); uerr == nil {
			item.EnrichmentKinds = mergeStringSlicesUnique(prev.EnrichmentKinds, item.EnrichmentKinds)
			if item.Priority < prev.Priority {
				item.Priority = prev.Priority
			}
			if !prev.CreatedAt.IsZero() {
				item.CreatedAt = prev.CreatedAt
			}
			if prev.Status == horizonmodels.ManualResolveQueueStatusResolved || prev.Status == horizonmodels.ManualResolveQueueStatusDismissed {
				// Do not resurrect terminal rows from automated passes unless caller explicitly reopened.
				return nil
			}
		}
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	_, err = docRef.Set(ctx, item)
	if err != nil {
		return fmt.Errorf("manual resolve queue upsert: %w", err)
	}
	return nil
}

func (r *ManualResolveQueueRepository) Get(ctx context.Context, id string) (*horizonmodels.ManualResolveQueueItem, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var item horizonmodels.ManualResolveQueueItem
	if err := snap.DataTo(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListByTenantAndStatus returns items for operator UI (newest first).
func (r *ManualResolveQueueRepository) ListByTenantAndStatus(ctx context.Context, tenantID string, status horizonmodels.ManualResolveQueueStatus, limit int) ([]*horizonmodels.ManualResolveQueueItem, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("status", "==", string(status)).
		OrderBy("createdAt", firestore.Desc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.ManualResolveQueueItem, 0, len(docs))
	for _, d := range docs {
		var it horizonmodels.ManualResolveQueueItem
		if err := d.DataTo(&it); err != nil {
			continue
		}
		out = append(out, &it)
	}
	return out, nil
}

// ListByTenantLensAndStatus lists items scoped to a lens (e.g. lens assembly queue).
func (r *ManualResolveQueueRepository) ListByTenantLensAndStatus(ctx context.Context, tenantID, lensID string, status horizonmodels.ManualResolveQueueStatus, limit int) ([]*horizonmodels.ManualResolveQueueItem, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).
		Where("lensId", "==", strings.TrimSpace(lensID)).
		Where("status", "==", string(status)).
		OrderBy("createdAt", firestore.Desc).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*horizonmodels.ManualResolveQueueItem, 0, len(docs))
	for _, d := range docs {
		var it horizonmodels.ManualResolveQueueItem
		if err := d.DataTo(&it); err != nil {
			continue
		}
		out = append(out, &it)
	}
	return out, nil
}
