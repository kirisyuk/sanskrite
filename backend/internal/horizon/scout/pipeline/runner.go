package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// Runner executes named pipeline passes.
// It is intentionally small and deterministic: pass selection is done by the caller.
type Runner struct {
	passes map[PassName]Pass
}

func NewRunner(passes ...Pass) *Runner {
	m := map[PassName]Pass{}
	for _, p := range passes {
		if p == nil {
			continue
		}
		m[p.Name()] = p
	}
	return &Runner{passes: m}
}

func DefaultRunner() *Runner {
	return NewRunner(
		BaseEnrichmentPass{},
		StrategicProjectionPass{},
	)
}

func (r *Runner) Run(ctx context.Context, eng Engine, passName PassName, in PassInput) error {
	if r == nil {
		r = DefaultRunner()
	}
	if eng == nil {
		return fmt.Errorf("pipeline engine is nil")
	}
	passName = PassName(strings.TrimSpace(string(passName)))
	if passName == "" {
		passName = PassBaseEnrichment
	}
	p, ok := r.passes[passName]
	if !ok || p == nil {
		return fmt.Errorf("unknown pipeline pass: %q", passName)
	}
	return p.Execute(ctx, eng, in)
}

