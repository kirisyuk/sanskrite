package policy

import (
	"os"
	"strings"
)

const (
	envUnifiedPolicyContract = "HORIZON_UNIFIED_POLICY_CONTRACT"
	envUnifiedPolicyShadow   = "HORIZON_UNIFIED_POLICY_SHADOW_MODE"
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

func policyEnvEnabled(name string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return def
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
