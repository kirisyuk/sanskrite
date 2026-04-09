package policy

import (
	"os"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	envScoutRuntimeMode         = "HORIZON_SCOUT_RUNTIME_MODE"
	envScoutAgenticWrites       = "HORIZON_SCOUT_AGENTIC_ALLOW_WRITES"
	envScoutAgenticSwitchback   = "HORIZON_SCOUT_AGENTIC_SWITCHBACK"
	envScoutAgenticShadowWrites = "HORIZON_SCOUT_AGENTIC_SHADOW_WRITES"
)

const (
	ScoutRuntimeClassic               = "classic_scout"
	ScoutRuntimeAgentic               = "agentic_scout"
	ScoutRuntimeShadowCompare         = "shadow_compare"
	ScoutRuntimeAgenticWithSwitchback = "agentic_primary_with_switchback"
)

type ScoutRuntimeConfig struct {
	Mode                 string
	AllowAgenticWrites   bool
	AllowShadowWrites    bool
	AllowClassicFallback bool
}

func ResolveScoutRuntimeConfig(job *horizonmodels.ScoutingJob) ScoutRuntimeConfig {
	mode := normalizeScoutRuntimeMode(strings.TrimSpace(os.Getenv(envScoutRuntimeMode)))
	if job != nil && job.State != nil {
		if raw, ok := job.State["runtimeMode"].(string); ok {
			mode = normalizeScoutRuntimeMode(raw)
		}
	}
	cfg := ScoutRuntimeConfig{
		Mode:                 mode,
		AllowAgenticWrites:   scoutRuntimeAllowsWrites(mode),
		AllowShadowWrites:    policyEnvEnabled(envScoutAgenticShadowWrites, false),
		AllowClassicFallback: mode == ScoutRuntimeAgenticWithSwitchback && policyEnvEnabled(envScoutAgenticSwitchback, true),
	}
	if policyEnvEnabled(envScoutAgenticWrites, false) {
		cfg.AllowAgenticWrites = true
	}
	if job != nil && job.State != nil {
		if raw, ok := job.State["allowAgenticWrites"].(bool); ok {
			cfg.AllowAgenticWrites = raw
		}
		if raw, ok := job.State["allowShadowWrites"].(bool); ok {
			cfg.AllowShadowWrites = raw
		}
		if raw, ok := job.State["allowClassicFallback"].(bool); ok {
			cfg.AllowClassicFallback = raw
		}
	}
	return cfg
}

func normalizeScoutRuntimeMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", ScoutRuntimeClassic:
		return ScoutRuntimeClassic
	case ScoutRuntimeAgentic:
		return ScoutRuntimeAgentic
	case ScoutRuntimeShadowCompare:
		return ScoutRuntimeShadowCompare
	case ScoutRuntimeAgenticWithSwitchback:
		return ScoutRuntimeAgenticWithSwitchback
	default:
		return ScoutRuntimeClassic
	}
}

func scoutRuntimeAllowsWrites(mode string) bool {
	switch normalizeScoutRuntimeMode(mode) {
	case ScoutRuntimeAgentic, ScoutRuntimeAgenticWithSwitchback:
		return true
	default:
		return false
	}
}
