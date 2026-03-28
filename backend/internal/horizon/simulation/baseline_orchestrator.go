package simulation

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

// BuildSimulationBaseline composes the two baseline axes for simulation:
//  1. Operational — path-aware operational metrics (metriccore over materialized supply-chain paths).
//  2. Signal — dependency-graph propagation from shock seeds (horizon/propagation).
//
// The axes stay separate algorithms; this function only sequences them for CreateRun.
func (s *Service) BuildSimulationBaseline(ctx context.Context, tenantID, projectID string, req *horizonmodels.SimulationRunRequest, baselineRefs horizonmodels.SimulationBaselineRefs, recognition map[string]any) (operationalPreview map[string]any, signal *horizonmodels.SignalDescriptor, combinedPreview map[string]any) {
	_ = workRequestForSimulation(tenantID, firstNonEmpty(projectID, strings.TrimSpace(req.ProjectID)), baselineRefs, req)
	operationalPreview = s.buildOperationalPreview(ctx, tenantID, req, baselineRefs, recognition)
	signal, combinedPreview = s.buildBaselineSignalAndPreview(ctx, tenantID, req, baselineRefs, recognition, operationalPreview)
	return operationalPreview, signal, combinedPreview
}

func workScopeForSimulation(tenantID, projectID string, refs horizonmodels.SimulationBaselineRefs, req *horizonmodels.SimulationRunRequest) workscope.WorkScope {
	ws := workscope.FromBaselineRefs(tenantID, projectID, refs)
	if strings.TrimSpace(ws.Refs.LensID) == "" && req != nil && strings.TrimSpace(req.LensID) != "" {
		ws.Refs.LensID = strings.TrimSpace(req.LensID)
	}
	ws.Intent = workscope.WorkScopeIntent{
		ScopeKind: workscope.ScopeKindSimulationBaseline,
		Expansion: workscope.WorkScopeExpansionPolicy{
			Mode: "simulation_preflight",
		},
	}
	ws = workscope.Normalize(ws)
	return ws
}

func workRequestForSimulation(tenantID, projectID string, refs horizonmodels.SimulationBaselineRefs, req *horizonmodels.SimulationRunRequest) workscope.WorkRequest {
	scope := workScopeForSimulation(tenantID, projectID, refs, req)
	return workscope.NormalizeRequest(workscope.WorkRequest{
		Scope: scope,
		Intent: workscope.ExecutionIntent{
			EntryPoint: workscope.EntryPointSimulation,
			Action:     workscope.ActionSimulate,
			Projection: workscope.WorkScopeProjectionPolicy{
				Mode:                   "baseline_plus_projection",
				IncludePropagatedReach: true,
			},
			MutationMode:  "read_mostly",
			FreshnessMode: "refresh_if_stale",
		},
	})
}
