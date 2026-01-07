package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Collection: external_data_cache
//
// Universal cache for external provider responses (GeoNames, Wikidata, etc.).
// Intended as a shared infra primitive: (service + type + key) => payload.

var ErrExternalCacheNotFound = errors.New("external cache entry not found")

type ExternalDataCacheRepository struct {
	col *firestore.CollectionRef
}

type ExternalDataCacheEntry struct {
	// ID duplicates Firestore doc ID for convenience/debugging.
	ID string `firestore:"id"`

	Service string   `firestore:"service"` // e.g. "geonames"
	Type    string   `firestore:"type"`    // e.g. "geonames.search"
	Types   []string `firestore:"types"`   // inherited type tags, e.g. ["geonames","geonames.search"]

	Key     string `firestore:"key"`     // stable logical key (human readable)
	KeyHash string `firestore:"keyHash"` // sha1(key) for compactness/debug

	ContentType string `firestore:"contentType"` // "application/json"
	Payload     []byte `firestore:"payload"`     // raw JSON (or any bytes)

	CreatedAt time.Time  `firestore:"createdAt"`
	UpdatedAt time.Time  `firestore:"updatedAt"`
	ExpiresAt *time.Time `firestore:"expiresAt,omitempty"`
}

func NewExternalDataCacheRepository(db *firestore.Client) *ExternalDataCacheRepository {
	return &ExternalDataCacheRepository{col: db.Collection("external_data_cache")}
}

func (r *ExternalDataCacheRepository) Get(ctx context.Context, id string) (*ExternalDataCacheEntry, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrExternalCacheNotFound
		}
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrExternalCacheNotFound
	}
	var e ExternalDataCacheEntry
	if err := snap.DataTo(&e); err != nil {
		return nil, err
	}
	if strings.TrimSpace(e.ID) == "" {
		e.ID = snap.Ref.ID
	}
	return &e, nil
}

func (r *ExternalDataCacheRepository) Upsert(ctx context.Context, e *ExternalDataCacheEntry) error {
	if r == nil || r.col == nil || e == nil || strings.TrimSpace(e.ID) == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if strings.TrimSpace(e.KeyHash) == "" && strings.TrimSpace(e.Key) != "" {
		e.KeyHash = sha1Hex(strings.TrimSpace(e.Key))
	}
	_, err := r.col.Doc(e.ID).Set(ctx, e)
	return err
}

// MakeCacheDocID returns a deterministic Firestore doc ID for (service,type,key).
// It keeps IDs short but debuggable.
func MakeCacheDocID(service, typ, key string) string {
	svc := strings.ToLower(strings.TrimSpace(service))
	t := strings.ToLower(strings.TrimSpace(typ))
	k := strings.TrimSpace(key)
	h := sha1Hex(svc + "|" + t + "|" + k)
	// Keep doc id compact; include svc+type prefix for readability.
	if svc == "" {
		svc = "external"
	}
	if t == "" {
		t = "data"
	}
	return svc + ":" + t + ":" + h
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
