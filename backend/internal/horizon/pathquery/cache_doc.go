package pathquery

import "time"

const pathCacheCollection = "horizon_path_cache"

type pathCacheDocument struct {
	CacheKey    string                 `firestore:"cacheKey"`
	Tenant      string                 `firestore:"tenant"`
	Project     string                 `firestore:"project"`
	Query       map[string]interface{} `firestore:"query"`
	PathIDs     []string               `firestore:"pathIds"`
	Diagnostics map[string]interface{} `firestore:"diagnostics"`
	GeneratedAt time.Time              `firestore:"generatedAt"`
	ExpiresAt   time.Time              `firestore:"expiresAt"`
	NodeRefs    []string               `firestore:"nodeRefs"`
}
