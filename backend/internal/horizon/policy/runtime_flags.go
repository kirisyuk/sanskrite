package policy

import (
	"os"
	"strings"
)

const (
	envUnifiedPolicyContract   = "HORIZON_UNIFIED_POLICY_CONTRACT"
	envUnifiedPolicyShadow     = "HORIZON_UNIFIED_POLICY_SHADOW_MODE"
	envScoutComposablePipeline = "HORIZON_SCOUT_COMPOSABLE_PIPELINE"
)

func policyUnifiedContractEnabled() bool {
	return policyEnvEnabled(envUnifiedPolicyContract, true)
}

func policyUnifiedShadowModeEnabled() bool {
	return policyEnvEnabled(envUnifiedPolicyShadow, false)
}

// UnifiedPolicyContractEnabled reports whether unified policy contract is active in runtime.
func UnifiedPolicyContractEnabled() bool {
	return policyUnifiedContractEnabled()
}

// UnifiedPolicyShadowModeEnabled reports whether shadow compare mode is enabled.
func UnifiedPolicyShadowModeEnabled() bool {
	return policyUnifiedShadowModeEnabled()
}

func scoutComposablePipelineEnabled() bool {
	return policyEnvEnabled(envScoutComposablePipeline, true)
}

// ScoutComposablePipelineEnabled reports whether Scout uses composable Critic/Research pipelines by domain mode.
// When false, Scout uses the monolithic validation and enrichment path (fallback/rollback).
func ScoutComposablePipelineEnabled() bool {
	return scoutComposablePipelineEnabled()
}

func policyEnvEnabled(name string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return def
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
