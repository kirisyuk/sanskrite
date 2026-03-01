package repo

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestRelationHashForDedup_Consistency(t *testing.T) {
	h1 := RelationHashForDedup("/org/us/acme", "/org/us/panasonic", "SUPPLIES")
	h2 := RelationHashForDedup("/org/us/acme", "/org/us/panasonic", "SUPPLIES")
	if h1 != h2 {
		t.Fatalf("RelationHashForDedup must be deterministic: %q != %q", h1, h2)
	}
	h3 := RelationHashForDedup("/org/us/acme", "/org/us/panasonic", "PARTNERS_WITH")
	if h1 == h3 {
		t.Fatalf("different relation types must produce different hashes: %q == %q", h1, h3)
	}
}

func TestBuildScoutJobDedupKey_CascadeRelationHash(t *testing.T) {
	base := &models.ScoutingJob{
		AgentJob: coreModels.AgentJob{
			TenantID:  "t1",
			ProjectID: "p1",
			AgentID:   "scout",
			State:     map[string]interface{}{},
		},
		LensID:              "l1",
		TargetCanonicalPath: "/org/us/child1",
	}

	job1 := *base
	job1.State = map[string]interface{}{
		"cascadeRootJobId": "cascade-123",
		"relationHash":     RelationHashForDedup("/org/us/root", "/org/us/child1", "SUPPLIES"),
	}
	job2 := *base
	job2.State = map[string]interface{}{
		"cascadeRootJobId": "cascade-123",
		"relationHash":     RelationHashForDedup("/org/us/root", "/org/us/child1", "PARTNERS_WITH"),
	}
	key1 := buildScoutJobDedupKey(&job1)
	key2 := buildScoutJobDedupKey(&job2)
	if key1 == key2 {
		t.Fatalf("different relations to same target must produce different dedup keys: %q == %q", key1, key2)
	}
	if !strings.Contains(key1, "relationHash=") {
		t.Fatalf("cascade child with relationHash must include it in key: %q", key1)
	}
}

func TestBuildScoutJobDedupKey_UsesIdentityWhenPresent(t *testing.T) {
	job := &models.ScoutingJob{
		TargetCanonicalPath: "/product/avatr-12",
		AgentJob:            models.ScoutingJob{}.AgentJob,
	}
	job.TenantID = "t1"
	job.ProjectID = "p1"
	job.AgentID = "scout"
	job.LensID = "l1"
	job.State = map[string]interface{}{
		"targetIdentityKey": "product|slug=avatr-12",
	}

	key := buildScoutJobDedupKey(job)
	if !strings.Contains(key, "identity=product|slug=avatr-12") {
		t.Fatalf("expected identity in dedup key, got: %s", key)
	}
	if strings.Contains(key, "target=/product/avatr-12") {
		t.Fatalf("dedup key should prefer identity over target path, got: %s", key)
	}
}
