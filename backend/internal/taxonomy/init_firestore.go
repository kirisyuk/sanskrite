package taxonomy

import (
	"context"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

var globalCacheMu = &sync.Mutex{}
var globalCache *FirestoreRuntimeTaxonomyCache

// InitRuntimeTaxonomyFromFirestore loads taxonomy doc from Firestore and installs it as runtime taxonomy.
// Deprecated in favor of cached loader: InitRuntimeTaxonomyCacheFromFirestore + RefreshRuntimeTaxonomy.
func InitRuntimeTaxonomyFromFirestore(ctx context.Context, db *firestore.Client, version string) error {
	if db == nil {
		return nil
	}
	if strings.TrimSpace(version) == "" {
		version = "v1"
	}
	InitRuntimeTaxonomyCacheFromFirestore(db, version, 5*time.Minute)
	return RefreshRuntimeTaxonomy(ctx)
}

// InitRuntimeTaxonomyCacheFromFirestore initializes the global cached loader.
func InitRuntimeTaxonomyCacheFromFirestore(db *firestore.Client, version string, ttl time.Duration) {
	if db == nil {
		return
	}
	if strings.TrimSpace(version) == "" {
		version = "v1"
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	globalCache = NewFirestoreRuntimeTaxonomyCache(db, version, ttl)
}

// RefreshRuntimeTaxonomy loads taxonomy through cache and installs it as runtime taxonomy.
func RefreshRuntimeTaxonomy(ctx context.Context) error {
	globalCacheMu.Lock()
	c := globalCache
	globalCacheMu.Unlock()
	if c == nil {
		return nil
	}
	rt, err := c.Get(ctx)
	if err != nil {
		return err
	}
	if rt != nil {
		SetRuntimeTaxonomy(rt)
	}
	return nil
}

// StartRuntimeTaxonomyAutoRefresh starts periodic refresh in background.
func StartRuntimeTaxonomyAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	globalCacheMu.Lock()
	c := globalCache
	globalCacheMu.Unlock()
	if c == nil {
		return
	}
	c.StartAutoRefresh(ctx, interval, onError)
}
