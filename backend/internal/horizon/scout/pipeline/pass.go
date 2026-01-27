package pipeline

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// PassName identifies a pipeline pass to execute for a job.
// We intentionally avoid the term "phase" here.
type PassName string

const (
	// PassBaseEnrichment runs the domain expansion / base enrichment logic.
	PassBaseEnrichment PassName = "base_enrichment"
	// PassStrategicProjection runs the strategic projection (decision/mechanism layer) logic.
	PassStrategicProjection PassName = "strategic_projection"

	// StateKeyPipelinePass is stored in ScoutingJob.State to select which pass to run.
	// Example: job.State["pipelinePass"] = "strategic_projection".
	StateKeyPipelinePass = "pipelinePass"
)

// Engine is the minimal contract the pipeline runner needs from the Scout engine.
// It is defined in this package to avoid import cycles.
//
// NOTE: method names are intentionally unexported (lowercase) so the implementation
// can be satisfied by existing unexported methods on *scout.ScoutEngine.
type Engine interface {
	ExecuteDomainExpansionPass(ctx context.Context, job *models.ScoutingJob, effectiveCanonicalPath string, existingFragment *models.HorizonFragment) error
	ExecuteStrategicProjectionPass(ctx context.Context, job *models.ScoutingJob, effectiveCanonicalPath string, existingFragment *models.HorizonFragment) error
}

type PassInput struct {
	Job                *models.ScoutingJob
	EffectiveCanonicalPath string
	ExistingFragment    *models.HorizonFragment
}

type Pass interface {
	Name() PassName
	Execute(ctx context.Context, eng Engine, in PassInput) error
}

