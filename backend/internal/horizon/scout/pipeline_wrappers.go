package scout

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ExecuteDomainExpansionPass is a pipeline-facing wrapper around the internal domain expansion executor.
// It exists to allow the pipeline runner (separate package) to invoke the pass without relying on
// unexported method names (which cannot cross package boundaries in Go interface matching).
func (e *ScoutEngine) ExecuteDomainExpansionPass(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	return e.executeDomainExpansionJob(ctx, job, effectiveCanonicalPath, existingFragment)
}

// ExecuteStrategicProjectionPass is a pipeline-facing wrapper around the internal strategic overlay executor.
func (e *ScoutEngine) ExecuteStrategicProjectionPass(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	return e.executeDomainStrategicOverlayJob(ctx, job, effectiveCanonicalPath, existingFragment)
}

