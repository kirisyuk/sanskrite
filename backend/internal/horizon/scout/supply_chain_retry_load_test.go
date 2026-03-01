package scout

import (
	"testing"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// TestSupplyChainRelationHash_Integration verifies RelationHashForDedup is used correctly
// for supply chain / relation fan-out idempotency. Full load test with cascade requires
// Firestore and is run separately.
func TestSupplyChainRelationHash_Integration(t *testing.T) {
	// Same (source, target, relationType) must produce same hash for dedup.
	h := horizonrepo.RelationHashForDedup("/org/us/tesla", "/org/us/panasonic", "SUPPLIES")
	if h == "" {
		t.Fatal("RelationHashForDedup must return non-empty hash")
	}
	if len(h) != 16 {
		t.Fatalf("RelationHashForDedup returns 16-char hex, got len=%d", len(h))
	}
	// Different relation types -> different hashes (no duplicate child jobs).
	h2 := horizonrepo.RelationHashForDedup("/org/us/tesla", "/org/us/panasonic", "PARTNERS_WITH")
	if h == h2 {
		t.Fatal("different relation types must produce different hashes")
	}
}
