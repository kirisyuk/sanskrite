package pathquery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// pathCacheBackend abstracts horizon_path_cache persistence (Firestore or test double).
type pathCacheBackend interface {
	getDoc(ctx context.Context, cacheKey string) (*pathCacheDocument, error)
	setDoc(ctx context.Context, cacheKey string, doc pathCacheDocument) error
	deleteDoc(ctx context.Context, cacheKey string) error
	deleteByNode(ctx context.Context, tenantID, canonicalPath string) error
}

type firestorePathCache struct {
	db *firestore.Client
}

func newFirestorePathCache(db *firestore.Client) pathCacheBackend {
	if db == nil {
		return nil
	}
	return &firestorePathCache{db: db}
}

func (f *firestorePathCache) getDoc(ctx context.Context, cacheKey string) (*pathCacheDocument, error) {
	if f == nil || f.db == nil || cacheKey == "" {
		return nil, nil
	}
	snap, err := f.db.Collection(pathCacheCollection).Doc(cacheKey).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, nil
	}
	var doc pathCacheDocument
	if err := snap.DataTo(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (f *firestorePathCache) setDoc(ctx context.Context, cacheKey string, doc pathCacheDocument) error {
	if f == nil || f.db == nil || cacheKey == "" {
		return nil
	}
	_, err := f.db.Collection(pathCacheCollection).Doc(cacheKey).Set(ctx, doc)
	return err
}

func (f *firestorePathCache) deleteDoc(ctx context.Context, cacheKey string) error {
	if f == nil || f.db == nil || cacheKey == "" {
		return nil
	}
	_, err := f.db.Collection(pathCacheCollection).Doc(cacheKey).Delete(ctx)
	return err
}

func (f *firestorePathCache) deleteByNode(ctx context.Context, tenantID, canonicalPath string) error {
	if f == nil || f.db == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	cp := strings.TrimSpace(canonicalPath)
	if tenantID == "" || cp == "" {
		return fmt.Errorf("tenant and canonicalPath required")
	}
	q := f.db.Collection(pathCacheCollection).Where("tenant", "==", tenantID).Where("nodeRefs", "array-contains", cp)
	iter := q.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		if _, err := doc.Ref.Delete(ctx); err != nil {
			return err
		}
	}
	return nil
}

// memPathCache is an in-memory pathCacheBackend for unit tests.
type memPathCache struct {
	mu   sync.RWMutex
	docs map[string]pathCacheDocument
}

func newMemPathCache() *memPathCache {
	return &memPathCache{docs: make(map[string]pathCacheDocument)}
}

func (m *memPathCache) getDoc(ctx context.Context, cacheKey string) (*pathCacheDocument, error) {
	if m == nil || cacheKey == "" {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.docs[cacheKey]; ok {
		cp := d
		return &cp, nil
	}
	return nil, nil
}

func (m *memPathCache) setDoc(ctx context.Context, cacheKey string, doc pathCacheDocument) error {
	if m == nil || cacheKey == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.docs == nil {
		m.docs = make(map[string]pathCacheDocument)
	}
	m.docs[cacheKey] = doc
	return nil
}

func (m *memPathCache) deleteDoc(ctx context.Context, cacheKey string) error {
	if m == nil || cacheKey == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, cacheKey)
	return nil
}

func (m *memPathCache) deleteByNode(ctx context.Context, tenantID, canonicalPath string) error {
	if m == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	cp := strings.TrimSpace(canonicalPath)
	if tenantID == "" || cp == "" {
		return fmt.Errorf("tenant and canonicalPath required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, d := range m.docs {
		if strings.TrimSpace(d.Tenant) != tenantID {
			continue
		}
		for _, nr := range d.NodeRefs {
			if strings.TrimSpace(nr) == cp {
				delete(m.docs, k)
				break
			}
		}
	}
	return nil
}
