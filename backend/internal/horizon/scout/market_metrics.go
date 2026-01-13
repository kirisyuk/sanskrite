package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// Canonical market metric keys (preferred).
const (
	MetricMarketTAMUSD          = "/metric/market/tam_usd"
	MetricMarketCAGRPercent     = "/metric/market/cagr_percent"
	MetricMarketHHI             = "/metric/market/hhi"
	MetricMarketCR4Percent      = "/metric/market/cr4_percent"
	MetricMarketSupplyChainRisk = "/metric/market/supply_chain_risk"
)

// Legacy market metric keys that may be emitted by older prompts.
const (
	legacyMarketTAMUSD          = "market_tam_usd"
	legacyMarketCAGRPercent     = "market_cagr_percent"
	legacyMarketHHI             = "market_hhi"
	legacyMarketCR4Percent      = "market_cr4_percent"
	legacyMarketSupplyChainRisk = "market_supply_chain_risk"
)

func strPtr(s string) *string { return &s }

// normalizeMarketMetricKeys canonicalizes MARKET sizing/concentration metric keys in fragment.State.
//
// Goal:
// - Keep market metrics stable across Scout/Research/MetricsAgent steps
// - Avoid silent "loss" due to inconsistent key naming
// - Provide predictable keys for Critic/missingInfo enforcement
//
// This is best-effort and intentionally conservative:
// - If canonical key already exists, legacy key is dropped.
// - If unit is missing, we fill a sensible default (usd/percent/index).
func normalizeMarketMetricKeys(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	if fragment.State == nil || len(fragment.State) == 0 {
		return
	}
	// Only apply to MARKET nodes (by type or path prefix).
	if fragment.Type != models.FragmentTypeMarket && !strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/market/") {
		return
	}

	move := func(from, to string, defaultUnit *string) {
		mv, ok := fragment.State[from]
		if !ok {
			return
		}
		// If canonical already exists, drop legacy.
		if _, exists := fragment.State[to]; exists {
			delete(fragment.State, from)
			return
		}
		// Best-effort: fill default unit if missing.
		if mv.Unit == nil && defaultUnit != nil && strings.TrimSpace(*defaultUnit) != "" {
			mv.Unit = defaultUnit
		}
		fragment.State[to] = mv
		delete(fragment.State, from)
	}

	// Legacy -> canonical moves.
	move(legacyMarketTAMUSD, MetricMarketTAMUSD, strPtr("usd"))
	move(legacyMarketCAGRPercent, MetricMarketCAGRPercent, strPtr("percent"))
	move(legacyMarketHHI, MetricMarketHHI, strPtr("index"))
	move(legacyMarketCR4Percent, MetricMarketCR4Percent, strPtr("percent"))
	move(legacyMarketSupplyChainRisk, MetricMarketSupplyChainRisk, nil)

	// If canonical keys are present but missing unit, fill defaults.
	if mv, ok := fragment.State[MetricMarketTAMUSD]; ok && mv.Unit == nil {
		mv.Unit = strPtr("usd")
		fragment.State[MetricMarketTAMUSD] = mv
	}
	if mv, ok := fragment.State[MetricMarketCAGRPercent]; ok && mv.Unit == nil {
		mv.Unit = strPtr("percent")
		fragment.State[MetricMarketCAGRPercent] = mv
	}
	if mv, ok := fragment.State[MetricMarketCR4Percent]; ok && mv.Unit == nil {
		mv.Unit = strPtr("percent")
		fragment.State[MetricMarketCR4Percent] = mv
	}
	if mv, ok := fragment.State[MetricMarketHHI]; ok && mv.Unit == nil {
		mv.Unit = strPtr("index")
		fragment.State[MetricMarketHHI] = mv
	}
}
