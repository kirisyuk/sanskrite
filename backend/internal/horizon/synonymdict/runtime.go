// Package synonymdict provides read-only access to the generic synonym
// dictionary backing org/facility/vessel_class/market/site aliases.
//
// The collection is `horizon_synonym_pairs` and is deliberately separate
// from product-specific (`horizon_product_synonym_pairs`) and
// government-specific (`horizon_gov_synonym_pairs`) synonyms: those two
// enforce `/product/...` and `/gov/...` prefixes respectively, whereas this
// one accepts any canonical path and so can carry TSMC acronyms,
// "Tesla, Inc." legal forms, and vessel-class plurals under one roof.
//
// The Python agenthorde mirror lives in
// agents/shared/agenthorde_common/agenthorde_common/canonicalizer/dictionaries.py
// (DictionaryRuntime.resolve_generic_synonym).
package synonymdict

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	defaultCollection          = "horizon_synonym_pairs"
	defaultCandidateCollection = "horizon_synonym_candidates"
)

// Runtime holds generic alias → canonical path mappings.
type Runtime struct {
	db                  *firestore.Client
	collection          string
	candidateCollection string
	tenantID            string

	mu            sync.RWMutex
	loaded        bool
	lastRefresh   time.Time
	aliasToCanon  map[string]string
	refreshPeriod time.Duration
}

// NewRuntime constructs a synonym dictionary runtime bound to the shared
// `horizon_synonym_pairs` collection. `db` is optional: when nil, only
// SeedAlias-fed mappings are served.
func NewRuntime(db *firestore.Client) *Runtime {
	return &Runtime{
		db:                  db,
		collection:          defaultCollection,
		candidateCollection: defaultCandidateCollection,
		tenantID:            "global",
		aliasToCanon:        map[string]string{},
		refreshPeriod:       5 * time.Minute,
	}
}

// SeedAlias registers an in-memory mapping. Intended for tests and one-off
// bootstrap paths where Firestore is unavailable.
func (r *Runtime) SeedAlias(alias, canonicalPath string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a := strings.TrimSpace(alias)
	c := strings.TrimSpace(canonicalPath)
	if a == "" || c == "" {
		return
	}
	r.aliasToCanon[strings.ToLower(a)] = c
	r.loaded = true
}

// Resolve returns the canonical path for alias (a raw surface label or an
// alias path). An empty return indicates the alias is unknown.
func (r *Runtime) Resolve(ctx context.Context, alias string) (canonical string, matched bool, err error) {
	if r == nil {
		return "", false, nil
	}
	if strings.TrimSpace(alias) == "" {
		return "", false, nil
	}
	if err := r.refreshIfNeeded(ctx); err != nil {
		return "", false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.aliasToCanon[strings.ToLower(strings.TrimSpace(alias))]; ok && c != "" {
		return c, true, nil
	}
	return "", false, nil
}

// Snapshot returns a copy of the current alias → canonical map.
func (r *Runtime) Snapshot() map[string]string {
	if r == nil {
		return map[string]string{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.aliasToCanon))
	for k, v := range r.aliasToCanon {
		out[k] = v
	}
	return out
}

func (r *Runtime) refreshIfNeeded(ctx context.Context) error {
	if r.db == nil {
		return nil
	}
	r.mu.Lock()
	if r.loaded && time.Since(r.lastRefresh) < r.refreshPeriod {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	col := r.db.Collection(r.collection)
	q := col.Where("tenantId", "==", r.tenantID)
	iter := q.Documents(ctx)
	next := map[string]string{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		var row struct {
			AliasSurface  string `firestore:"aliasSurface"`
			AliasPath     string `firestore:"aliasPath"`
			CanonicalPath string `firestore:"canonicalPath"`
			Active        bool   `firestore:"active"`
		}
		if err := doc.DataTo(&row); err != nil {
			continue
		}
		// Active flag: Firestore bool defaults to false; treat missing as
		// active to match the seed-and-forget workflow.
		alias := strings.TrimSpace(firstNonEmpty(row.AliasSurface, row.AliasPath))
		canon := strings.TrimSpace(row.CanonicalPath)
		if alias == "" || canon == "" {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(canon); err != nil {
			continue
		}
		next[strings.ToLower(alias)] = canon
	}
	r.mu.Lock()
	r.aliasToCanon = next
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func candidateDocID(tenantID, alias string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(tenantID)) + "\x00" + strings.ToLower(strings.TrimSpace(alias))))
	return hex.EncodeToString(sum[:])
}

// RecordUnknownCandidate persists an unresolved surface for later human
// review. Mirrors govdict.RecordUnknownGovCandidate.
func (r *Runtime) RecordUnknownCandidate(ctx context.Context, tenantID, alias, observedPath, fragmentID, sourceKind string) error {
	if r == nil || r.db == nil {
		return nil
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return nil
	}
	if _, matched, err := r.Resolve(ctx, alias); err != nil || matched {
		return err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = r.tenantID
	}
	docID := candidateDocID(tenantID, alias)
	doc := r.db.Collection(r.candidateCollection).Doc(docID)
	now := firestore.ServerTimestamp
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		exists := err == nil && snap != nil && snap.Exists()
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "notfound") {
			return err
		}
		update := map[string]interface{}{
			"tenantId":      tenantID,
			"aliasSurface":  alias,
			"status":        "new",
			"active":        true,
			"evidenceCount": firestore.Increment(1),
			"updatedAt":     now,
			"lastSeenAt":    now,
		}
		if op := strings.TrimSpace(observedPath); op != "" {
			update["observedPath"] = op
		}
		if sk := strings.TrimSpace(sourceKind); sk != "" {
			update["sourceKinds"] = firestore.ArrayUnion(sk)
		}
		if fid := strings.TrimSpace(fragmentID); fid != "" {
			update["exampleFragmentIds"] = firestore.ArrayUnion(fid)
		}
		if !exists {
			update["createdAt"] = now
			update["firstSeenAt"] = now
		}
		tx.Set(doc, update, firestore.MergeAll)
		return nil
	})
}

// RecordUnknownCandidateAsync is the fire-and-forget variant used from hot paths.
func (r *Runtime) RecordUnknownCandidateAsync(tenantID, alias, observedPath, fragmentID, sourceKind string) {
	if r == nil || r.db == nil {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[synonymdict] candidate hook panic: %v stack=%s", rec, string(debug.Stack()))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = r.RecordUnknownCandidate(ctx, tenantID, alias, observedPath, fragmentID, sourceKind)
	}()
}
