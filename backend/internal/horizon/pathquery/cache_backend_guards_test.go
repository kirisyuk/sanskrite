package pathquery

import (
	"context"
	"testing"
)

func TestFirestorePathCache_nilOrEmptyGuards(t *testing.T) {
	ctx := context.Background()
	var nilCache *firestorePathCache
	if _, err := nilCache.getDoc(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	empty := &firestorePathCache{db: nil}
	if _, err := empty.getDoc(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := empty.setDoc(ctx, "", pathCacheDocument{}); err != nil {
		t.Fatal(err)
	}
	if err := empty.deleteDoc(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := empty.deleteByNode(ctx, "t", "/x"); err != nil {
		t.Fatal(err)
	}
}

func TestMemPathCache_deleteDocGuards(t *testing.T) {
	ctx := context.Background()
	var m *memPathCache
	_ = m.deleteDoc(ctx, "k")
	n := newMemPathCache()
	if err := n.deleteDoc(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
