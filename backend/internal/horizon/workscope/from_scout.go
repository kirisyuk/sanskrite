package workscope

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// FromScoutingJobParams builds a normalized WorkScope from scouting job parameters:
// WorkScopeRefs, LensID, TargetCanonicalPath, and SeedHints are unioned into scope paths and context.
func FromScoutingJobParams(p *horizonmodels.ScoutingJobParams) WorkScope {
	if p == nil {
		return WorkScope{}
	}
	refs := p.WorkScopeRefs
	if strings.TrimSpace(refs.LensID) == "" {
		refs.LensID = strings.TrimSpace(p.LensID)
	}
	if tp := strings.TrimSpace(p.TargetCanonicalPath); tp != "" {
		refs.ScopePaths = append(refs.ScopePaths, tp)
	}
	for _, h := range p.SeedHints {
		if s := strings.TrimSpace(h); s != "" {
			refs.ScopePaths = append(refs.ScopePaths, s)
		}
	}
	scope := FromBaselineRefs(strings.TrimSpace(p.TenantID), strings.TrimSpace(p.ProjectID), refs)
	scope.Intent = WorkScopeIntent{
		ScopeKind: ScopeKindScoutJob,
		Expansion: WorkScopeExpansionPolicy{
			Mode: "scout_enrichment",
		},
	}
	return Normalize(scope)
}

// RequestFromScoutingJobParams builds the default graph-enrichment request over a scout scope.
func RequestFromScoutingJobParams(p *horizonmodels.ScoutingJobParams) WorkRequest {
	scope := FromScoutingJobParams(p)
	return NormalizeRequest(WorkRequest{
		Scope: scope,
		Intent: ExecutionIntent{
			EntryPoint: EntryPointScout,
			Action:     ActionEnrichGraph,
			Projection: WorkScopeProjectionPolicy{
				Mode: "scope_only",
			},
			MutationMode:  defaultMutationModeForAction(ActionEnrichGraph),
			FreshnessMode: defaultFreshnessModeForAction(ActionEnrichGraph),
		},
	})
}
