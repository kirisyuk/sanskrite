package policy

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestResolveScoutRuntimeConfigDefaultsToClassic(t *testing.T) {
	t.Setenv(envScoutRuntimeMode, "")
	t.Setenv(envScoutAgenticWrites, "")
	t.Setenv(envScoutAgenticSwitchback, "")
	t.Setenv(envScoutAgenticShadowWrites, "")

	cfg := ResolveScoutRuntimeConfig(nil)
	if cfg.Mode != ScoutRuntimeClassic {
		t.Fatalf("mode = %q, want %q", cfg.Mode, ScoutRuntimeClassic)
	}
	if cfg.AllowAgenticWrites {
		t.Fatalf("AllowAgenticWrites = true, want false")
	}
	if cfg.AllowShadowWrites {
		t.Fatalf("AllowShadowWrites = true, want false")
	}
	if cfg.AllowClassicFallback {
		t.Fatalf("AllowClassicFallback = true, want false")
	}
}

func TestResolveScoutRuntimeConfigReadsSwitchbackEnv(t *testing.T) {
	t.Setenv(envScoutRuntimeMode, ScoutRuntimeAgenticWithSwitchback)
	t.Setenv(envScoutAgenticWrites, "")
	t.Setenv(envScoutAgenticSwitchback, "1")
	t.Setenv(envScoutAgenticShadowWrites, "")

	cfg := ResolveScoutRuntimeConfig(nil)
	if cfg.Mode != ScoutRuntimeAgenticWithSwitchback {
		t.Fatalf("mode = %q, want %q", cfg.Mode, ScoutRuntimeAgenticWithSwitchback)
	}
	if !cfg.AllowAgenticWrites {
		t.Fatalf("AllowAgenticWrites = false, want true")
	}
	if !cfg.AllowClassicFallback {
		t.Fatalf("AllowClassicFallback = false, want true")
	}
}

func TestResolveScoutRuntimeConfigJobStateOverridesEnv(t *testing.T) {
	t.Setenv(envScoutRuntimeMode, ScoutRuntimeClassic)
	t.Setenv(envScoutAgenticWrites, "0")
	t.Setenv(envScoutAgenticSwitchback, "0")
	t.Setenv(envScoutAgenticShadowWrites, "0")

	job := &horizonmodels.ScoutingJob{}
	job.State = map[string]any{
		"runtimeMode":          ScoutRuntimeShadowCompare,
		"allowAgenticWrites":   true,
		"allowShadowWrites":    true,
		"allowClassicFallback": true,
	}

	cfg := ResolveScoutRuntimeConfig(job)
	if cfg.Mode != ScoutRuntimeShadowCompare {
		t.Fatalf("mode = %q, want %q", cfg.Mode, ScoutRuntimeShadowCompare)
	}
	if !cfg.AllowAgenticWrites {
		t.Fatalf("AllowAgenticWrites = false, want true")
	}
	if !cfg.AllowShadowWrites {
		t.Fatalf("AllowShadowWrites = false, want true")
	}
	if !cfg.AllowClassicFallback {
		t.Fatalf("AllowClassicFallback = false, want true")
	}
}
