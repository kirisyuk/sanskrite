package pipeline

import "context"

// BaseEnrichmentPass runs base domain enrichment (formerly "domain expansion").
// Implementation currently delegates to the existing ScoutEngine domain expansion executor.
// This keeps the system stable while we converge on a single pipeline runner.
type BaseEnrichmentPass struct{}

func (BaseEnrichmentPass) Name() PassName { return PassBaseEnrichment }

func (BaseEnrichmentPass) Execute(ctx context.Context, eng Engine, in PassInput) error {
	return eng.ExecuteDomainExpansionPass(ctx, in.Job, in.EffectiveCanonicalPath, in.ExistingFragment)
}

// StrategicProjectionPass runs strategic projection (decision/mechanism layer overlay).
// Implementation currently delegates to the existing ScoutEngine overlay executor.
type StrategicProjectionPass struct{}

func (StrategicProjectionPass) Name() PassName { return PassStrategicProjection }

func (StrategicProjectionPass) Execute(ctx context.Context, eng Engine, in PassInput) error {
	return eng.ExecuteStrategicProjectionPass(ctx, in.Job, in.EffectiveCanonicalPath, in.ExistingFragment)
}

