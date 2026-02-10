package repo

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

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
