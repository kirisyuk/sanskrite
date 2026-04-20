package pathquery

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// TestPathquery_FirestoreCache_hitMiss requires a running Firestore emulator:
//
//	firestore emulators:start --only firestore
//	export FIRESTORE_EMULATOR_HOST=127.0.0.1:8080
//
// P8.07 acceptance: miss → write → second call hits cache; InvalidateByNode clears.
func TestPathquery_FirestoreCache_hitMiss(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("set FIRESTORE_EMULATOR_HOST to run Firestore integration test")
	}
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "demo-pathquery-integration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		"/org/b": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a", "/org/b"},
		Payload:  payload,
	}
	mat := &memMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}},
		byID:   map[string]*horizonmodels.MaterializedArtifact{"m1": art},
	}
	s := NewService(fr, nil, mat, client, nil)
	q := Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:     &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
	}
	res1, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res1.FromCache {
		t.Fatal("expected miss on cold cache")
	}
	key := res1.CacheKey
	if key == "" {
		t.Fatal("empty cache key")
	}

	res2, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.FromCache {
		t.Fatal("expected second query to hit Firestore cache")
	}

	if err := s.InvalidateByNode(ctx, "t", "/org/a"); err != nil {
		t.Fatal(err)
	}
	// Doc may still exist but node-based delete should remove entries touching /org/a
	snap, err := client.Collection(pathCacheCollection).Doc(key).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Exists() {
		t.Fatal("expected cache doc removed after InvalidateByNode")
	}

	res3, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res3.FromCache {
		t.Fatal("expected miss after invalidation")
	}
}

func TestPathquery_FirestoreCache_expiredIgnored(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("set FIRESTORE_EMULATOR_HOST to run Firestore integration test")
	}
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "demo-pathquery-integration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &memMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}}}
	s := NewService(fr, nil, mat, client, nil)

	nq, err := normalizeQuery(Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"}, CacheMode: CacheModeDefault})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(ctx, &nq); err != nil {
		t.Fatal(err)
	}
	q := Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"}, CacheMode: CacheModeDefault}
	cacheKey := computeCacheKey(q, nq.FromKind, nq.FromValue, nq.ToPresent, nq.ToKind, nq.ToValue, nq.MaxHops, nq.RelationKinds)
	gen := time.Now().UTC().Add(-48 * time.Hour)
	doc := pathCacheDocument{
		CacheKey:    cacheKey,
		Tenant:      "t",
		PathIDs:     []string{"m1"},
		GeneratedAt: gen,
		ExpiresAt:   gen.Add(time.Hour),
		NodeRefs:    []string{"/org/a"},
	}
	if werr := (&firestorePathCache{db: client}).setDoc(ctx, cacheKey, doc); werr != nil {
		t.Fatal(werr)
	}
	res, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("expired cache entry should not be treated as hit")
	}
}
