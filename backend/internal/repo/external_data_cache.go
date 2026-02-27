package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	redis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Collection: external_data_cache
//
// Universal cache for external provider responses (GeoNames, Wikidata, etc.).
// Intended as a shared infra primitive: (service + type + key) => payload.

var ErrExternalCacheNotFound = errors.New("external cache entry not found")

type ExternalDataCacheRepository struct {
	col        *firestore.CollectionRef
	redisCache *redis.Client
}

const edcRedisPrefix = "h:edc:"
const edcRedisTimeout = 70 * time.Millisecond

func (r *ExternalDataCacheRepository) WithRedis(c *redis.Client) {
	if r != nil {
		r.redisCache = c
	}
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
	if r.redisCache != nil {
		if entry := r.getFromRedis(ctx, id); entry != nil {
			return entry, nil
		}
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
	r.setInRedis(ctx, &e)
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
	if err == nil {
		r.setInRedis(ctx, e)
	}
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

// getFromRedis attempts a fast Redis L1 lookup. Returns nil on miss or error.
func (r *ExternalDataCacheRepository) getFromRedis(ctx context.Context, id string) *ExternalDataCacheEntry {
	if r == nil || r.redisCache == nil {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, edcRedisTimeout)
	defer cancel()
	val, err := r.redisCache.Get(rctx, edcRedisPrefix+id).Bytes()
	if err != nil || len(val) == 0 {
		return nil
	}
	var e ExternalDataCacheEntry
	if err := json.Unmarshal(val, &e); err != nil {
		return nil
	}
	if e.ExpiresAt != nil && time.Now().UTC().After(*e.ExpiresAt) {
		return nil
	}
	return &e
}

// setInRedis writes an entry to Redis L1 (best-effort, errors ignored).
// TTL is derived from ExpiresAt; entries without expiry get 30 days.
func (r *ExternalDataCacheRepository) setInRedis(ctx context.Context, e *ExternalDataCacheEntry) {
	if r == nil || r.redisCache == nil || e == nil || strings.TrimSpace(e.ID) == "" {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	ttl := 30 * 24 * time.Hour
	if e.ExpiresAt != nil {
		remaining := time.Until(*e.ExpiresAt)
		if remaining > 0 {
			ttl = remaining
		} else {
			return
		}
	}
	rctx, cancel := context.WithTimeout(ctx, edcRedisTimeout)
	defer cancel()
	_ = r.redisCache.Set(rctx, edcRedisPrefix+e.ID, b, ttl).Err()
}
