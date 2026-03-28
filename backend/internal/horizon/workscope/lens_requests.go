package workscope

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	StateKeyWorkScopeDigest          = "workScopeDigest"
	StateKeyWorkRequest              = "workRequest"
	StateKeyWorkScopeKind            = "workScopeKind"
	StateKeyExecutionEntryPoint      = "executionEntryPoint"
	StateKeyExecutionAction          = "executionAction"
	StateKeyScopeProjectionMode      = "scopeProjectionMode"
	StateKeyScopeMutationMode        = "scopeMutationMode"
	StateKeyScopeAutoExpandAllowed   = "scopeAutoExpandAllowed"
	StateKeyScopeAutoAddAllowedPaths = "scopeAutoAddAllowedPaths"
)

// FromLensStrictWorkScope builds a strict lens scope without neighbor expansion.
func FromLensStrictWorkScope(tenantID, projectID, lensID string, canonicalScopePaths []string) WorkScope {
	scope := FromBaselineRefs(strings.TrimSpace(tenantID), strings.TrimSpace(projectID), horizonmodels.SimulationBaselineRefs{
		LensID:     strings.TrimSpace(lensID),
		ScopePaths: append([]string(nil), canonicalScopePaths...),
	})
	scope.Intent = WorkScopeIntent{
		ScopeKind: ScopeKindLensStrict,
		Expansion: WorkScopeExpansionPolicy{
			Mode:               "lens_strict",
			AllowDeferredPaths: true,
		},
	}
	return Normalize(scope)
}

// NewLensGraphEnrichmentRequest builds a graph-enrichment request over a strict lens scope.
// mutationMode controls whether discovered entities may expand the lens scope at runtime.
func NewLensGraphEnrichmentRequest(scope WorkScope, mutationMode string) WorkRequest {
	scope = Normalize(scope)
	mode := normalizeLensMutationMode(mutationMode)
	return NormalizeRequest(WorkRequest{
		Scope: scope,
		Intent: ExecutionIntent{
			EntryPoint: EntryPointLens,
			Action:     ActionEnrichGraph,
			Projection: WorkScopeProjectionPolicy{
				Mode: ProjectionModeScopeOnly,
			},
			MutationMode:  mode,
			FreshnessMode: defaultFreshnessModeForAction(ActionEnrichGraph),
		},
	})
}

func normalizeLensMutationMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case MutationModePersistWorldKeepScope,
		MutationModePersistWorldScopeRefsOnly,
		MutationModePersistWorldExpandScope:
		return strings.TrimSpace(raw)
	default:
		return MutationModePersistWorldScopeRefsOnly
	}
}

// AllowedScopeAutoAddPaths returns the canonical paths that may be auto-added back to lens scope.
func AllowedScopeAutoAddPaths(request WorkRequest) []string {
	request = NormalizeRequest(request)
	switch strings.TrimSpace(request.Intent.MutationMode) {
	case MutationModePersistWorldScopeRefsOnly:
		return append([]string(nil), request.Scope.Refs.ScopePaths...)
	default:
		return nil
	}
}

// RequestAllowsScopeAutoAdd reports whether this request may add the supplied path back to lens scope.
func RequestAllowsScopeAutoAdd(request WorkRequest, canonicalPath string) bool {
	request = NormalizeRequest(request)
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return false
	}
	switch strings.TrimSpace(request.Intent.MutationMode) {
	case MutationModePersistWorldKeepScope:
		return false
	case MutationModePersistWorldScopeRefsOnly:
		for _, allowed := range AllowedScopeAutoAddPaths(request) {
			if strings.TrimSpace(allowed) == canonicalPath {
				return true
			}
		}
		return false
	case MutationModePersistWorldExpandScope, MutationModePersistChanges, "":
		return true
	default:
		return true
	}
}

// ApplyRequestToState annotates runtime state so downstream layers can enforce scope mutation policy.
func ApplyRequestToState(state map[string]interface{}, request WorkRequest) map[string]interface{} {
	request = NormalizeRequest(request)
	if state == nil {
		state = make(map[string]interface{})
	}
	scopeDigest := Digest(ResolvedFromScope(request.Scope))
	state[StateKeyWorkScopeDigest] = scopeDigest
	state[StateKeyWorkScopeKind] = string(request.Scope.Intent.ScopeKind)
	state[StateKeyExecutionEntryPoint] = string(request.Intent.EntryPoint)
	state[StateKeyExecutionAction] = string(request.Intent.Action)
	state[StateKeyScopeProjectionMode] = strings.TrimSpace(request.Intent.Projection.Mode)
	state[StateKeyScopeMutationMode] = strings.TrimSpace(request.Intent.MutationMode)
	state[StateKeyScopeAutoExpandAllowed] = strings.TrimSpace(request.Intent.MutationMode) == MutationModePersistWorldExpandScope
	if allowed := AllowedScopeAutoAddPaths(request); len(allowed) > 0 {
		state[StateKeyScopeAutoAddAllowedPaths] = allowed
	} else {
		delete(state, StateKeyScopeAutoAddAllowedPaths)
	}
	state[StateKeyWorkRequest] = map[string]interface{}{
		"scopeDigest":     scopeDigest,
		"scopeKind":       string(request.Scope.Intent.ScopeKind),
		"entryPoint":      string(request.Intent.EntryPoint),
		"action":          string(request.Intent.Action),
		"projectionMode":  strings.TrimSpace(request.Intent.Projection.Mode),
		"mutationMode":    strings.TrimSpace(request.Intent.MutationMode),
		"freshnessMode":   strings.TrimSpace(request.Intent.FreshnessMode),
		"requestedLayers": executionLayerStrings(request.Intent.RequestedLayers),
	}
	return state
}

// StateAllowsScopeAutoAdd decodes lens scope auto-add policy from persisted job state.
// Legacy state without explicit policy remains permissive for backward compatibility.
func StateAllowsScopeAutoAdd(state map[string]interface{}, canonicalPath string) bool {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if len(state) == 0 || canonicalPath == "" {
		return canonicalPath != ""
	}
	mode := strings.TrimSpace(stateString(state[StateKeyScopeMutationMode]))
	switch mode {
	case MutationModePersistWorldKeepScope:
		return false
	case MutationModePersistWorldScopeRefsOnly:
		for _, allowed := range stateStringSlice(state[StateKeyScopeAutoAddAllowedPaths]) {
			if strings.TrimSpace(allowed) == canonicalPath {
				return true
			}
		}
		return false
	case MutationModePersistWorldExpandScope:
		return true
	case "", MutationModePersistChanges:
		return stateBool(state[StateKeyScopeAutoExpandAllowed], true)
	default:
		return stateBool(state[StateKeyScopeAutoExpandAllowed], true)
	}
}

func executionLayerStrings(layers []ExecutionLayer) []string {
	if len(layers) == 0 {
		return nil
	}
	out := make([]string, 0, len(layers))
	for _, layer := range layers {
		if s := strings.TrimSpace(string(layer)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stateString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}

func stateStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return normalizeStringSlice(t)
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := stateString(item); s != "" {
				out = append(out, s)
			}
		}
		return normalizeStringSlice(out)
	default:
		if s := stateString(v); s != "" {
			return []string{s}
		}
		return nil
	}
}

func stateBool(v interface{}, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.TrimSpace(strings.ToLower(t)) {
		case "1", "true", "yes", "y":
			return true
		case "0", "false", "no", "n":
			return false
		default:
			return def
		}
	default:
		return def
	}
}
