package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// Regression lock: canonical JSON for ResolvedWorkSet serialization must stay stable for idempotency keys.
func TestDigest_baselineContractGolden(t *testing.T) {
	ws := Normalize(WorkScope{
		TenantID:  "tenant-a",
		ProjectID: "proj-1",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:     "lens-1",
			ScopePaths: []string{"/geo/US", "/product/foo"},
		},
	})
	d := Digest(ResolvedFromScope(ws))
	const want = "588fbde09cdf34da296bed3565e3939ba30a0464b0ed71a6d1dcdbff3f7d08dd"
	if d != want {
		t.Fatalf("digest drift: got %s want %s (update want only if canonical contract intentionally changes)", d, want)
	}
}
