package scout

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// runPolicyAudit is a naming wrapper around runPolicyCheck.
//
// The intent is to position policy enforcement as an auditor/repair mechanism rather than
// a semantic gate for correctness. Semantic correctness should be enforced pre-upsert by
// the edge materializer + direction policies.
func (e *ScoutEngine) runPolicyAudit(ctx context.Context, job *models.ScoutingJob) {
	e.runPolicyCheck(ctx, job)
}

