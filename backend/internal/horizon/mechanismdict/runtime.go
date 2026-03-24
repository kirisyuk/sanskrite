package mechanismdict

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

const defaultMechanismSynonymCollection = "horizon_mechanism_synonym_pairs"
const defaultMechanismCandidateCollection = "horizon_mechanism_synonym_candidates"

// Runtime holds mechanism synonym pairs (alias → canonical /mechanism/... path), loaded from Firestore.
type Runtime struct {
	db            *firestore.Client
	collection    string
	tenantID      string
	mu            sync.RWMutex
	loaded        bool
	lastRefresh   time.Time
	aliasToCanon  map[string]string
	refreshPeriod time.Duration
}

// NewRuntime constructs a mechanism dictionary runtime (tenant "global" by default).
func NewRuntime(db *firestore.Client) *Runtime {
	return &Runtime{
		db:            db,
		collection:    defaultMechanismSynonymCollection,
		tenantID:      "global",
		aliasToCanon:  map[string]string{},
		refreshPeriod: 5 * time.Minute,
	}
}

// WithCollection overrides the Firestore collection name.
func (r *Runtime) WithCollection(name string) *Runtime {
	if r == nil {
		return r
	}
	if strings.TrimSpace(name) != "" {
		r.collection = strings.TrimSpace(name)
	}
	return r
}

// SeedAlias registers an in-memory alias (used in tests or bootstrap).
func (r *Runtime) SeedAlias(aliasPath, canonicalPath string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a := strings.TrimSpace(aliasPath)
	c := strings.TrimSpace(canonicalPath)
	if a == "" || c == "" {
		return
	}
	r.aliasToCanon[strings.ToLower(a)] = c
	r.loaded = true
}

// CanonicalizeMechanismPath returns the canonical mechanism path when a synonym match exists.
func (r *Runtime) CanonicalizeMechanismPath(ctx context.Context, path string) (canonical string, matched bool, err error) {
	_ = ctx
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/mechanism/") {
		return path, false, nil
	}
	if r == nil {
		return path, false, nil
	}
	if err := r.refreshIfNeeded(ctx); err != nil {
		return path, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.aliasToCanon[strings.ToLower(path)]; ok && c != "" {
		return c, true, nil
	}
	return path, false, nil
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
			AliasPath     string `firestore:"aliasPath"`
			CanonicalPath string `firestore:"canonicalPath"`
		}
		if err := doc.DataTo(&row); err != nil {
			continue
		}
		a := strings.TrimSpace(row.AliasPath)
		c := strings.TrimSpace(row.CanonicalPath)
		if a == "" || c == "" {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(c); err != nil {
			continue
		}
		next[strings.ToLower(a)] = c
	}

	r.mu.Lock()
	r.aliasToCanon = next
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()
	return nil
}

func mechanismCandidateDocID(tenantID, path string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(tenantID)) + "\x00" + strings.ToLower(strings.TrimSpace(path))))
	return hex.EncodeToString(sum[:])
}

// RecordUnknownMechanismCandidate persists an unresolved /mechanism/ path for human review (control-plane parity with product candidates).
func (r *Runtime) RecordUnknownMechanismCandidate(ctx context.Context, tenantID, observedPath, fragmentID, sourceKind string) error {
	if r == nil || r.db == nil {
		return nil
	}
	path := strings.TrimSpace(observedPath)
	if path == "" || !strings.HasPrefix(path, "/mechanism/") {
		return nil
	}
	if _, matched, err := r.CanonicalizeMechanismPath(ctx, path); err != nil || matched {
		return err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = r.tenantID
	}
	docID := mechanismCandidateDocID(tenantID, path)
	doc := r.db.Collection(defaultMechanismCandidateCollection).Doc(docID)
	now := firestore.ServerTimestamp
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		exists := err == nil && snap != nil && snap.Exists()
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "notfound") {
			return err
		}
		update := map[string]interface{}{
			"tenantId":      tenantID,
			"observedPath":  path,
			"status":        "new",
			"active":        true,
			"evidenceCount": firestore.Increment(1),
			"updatedAt":     now,
			"lastSeenAt":    now,
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

// RecordUnknownMechanismCandidateAsync is best-effort; must not block persist paths.
func (r *Runtime) RecordUnknownMechanismCandidateAsync(tenantID, observedPath, fragmentID, sourceKind string) {
	if r == nil || r.db == nil {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[mechanismdict] candidate hook panic: %v stack=%s", rec, string(debug.Stack()))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = r.RecordUnknownMechanismCandidate(ctx, tenantID, observedPath, fragmentID, sourceKind)
	}()
}
