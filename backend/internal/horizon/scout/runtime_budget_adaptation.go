package scout

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// runtimeBudgetSnapshot captures daily budget usage vs caps for adaptive runtime throttling.
type runtimeBudgetSnapshot struct {
	DayUTC string

	CascadeSpendUSD float64
	CascadeCapUSD   float64
	CascadeOut      int
	CascadeOutCap   int

	LensSpendUSD float64
	LensCapUSD   float64
	LensOut      int
	LensOutCap   int

	MaxPressure float64
}

func budgetRatio(used, cap float64) float64 {
	if cap <= 0 {
		return 0
	}
	if used <= 0 {
		return 0
	}
	return used / cap
}

func budgetFanoutScaleForPressure(pressure float64) float64 {
	switch {
	case pressure <= 0:
		return 1.0
	case pressure <= 0.50:
		return 1.0
	case pressure <= 0.75:
		return 0.85
	case pressure <= 0.90:
		return 0.70
	case pressure <= 1.00:
		return 0.55
	case pressure <= 1.10:
		return 0.45
	case pressure <= 1.25:
		return 0.35
	case pressure <= 1.50:
		return 0.25
	default:
		return 0.15
	}
}

func scaleCapByBudget(baseCap, minCap int, factor float64) int {
	if baseCap <= 0 {
		return 0
	}
	if minCap < 0 {
		minCap = 0
	}
	if factor <= 0 {
		factor = 0.15
	}
	scaled := int(math.Ceil(float64(baseCap) * factor))
	if scaled < minCap {
		scaled = minCap
	}
	if scaled > baseCap {
		scaled = baseCap
	}
	return scaled
}

func shouldFreezeDepthByBudgetPressure(pressure float64) bool {
	// Keep the pipeline alive under normal overrun by reducing fan-out first.
	// Freeze depth only when the overrun is severe.
	return pressure >= 2.0
}

func (s *runtimeBudgetSnapshot) summary() string {
	if s == nil {
		return "budget=unavailable"
	}
	return fmt.Sprintf(
		"day=%s pressure=%.2f cascadeUSD=%.4f/%.4f cascadeOut=%d/%d lensUSD=%.4f/%.4f lensOut=%d/%d",
		strings.TrimSpace(s.DayUTC),
		s.MaxPressure,
		s.CascadeSpendUSD, s.CascadeCapUSD,
		s.CascadeOut, s.CascadeOutCap,
		s.LensSpendUSD, s.LensCapUSD,
		s.LensOut, s.LensOutCap,
	)
}

// computeRuntimeBudgetAdaptation returns a fan-out scale factor in [0.15..1.0] based on daily budget pressure.
// Best-effort: if budget data is unavailable, ok=false and callers should continue without adaptation.
func (e *ScoutEngine) computeRuntimeBudgetAdaptation(
	ctx context.Context,
	tenantID string,
	lensID string,
	cascadeID string,
) (scale float64, snap *runtimeBudgetSnapshot, ok bool) {
	if e == nil || !e.budgetPolicy.Enabled || e.spendLedger == nil {
		return 1.0, nil, false
	}
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	cascadeID = strings.TrimSpace(cascadeID)
	if tenantID == "" {
		return 1.0, nil, false
	}

	day := time.Now().UTC().Format("2006-01-02")
	out := &runtimeBudgetSnapshot{
		DayUTC: day,

		CascadeCapUSD: e.budgetPolicy.CascadeDailyMaxUSD,
		CascadeOutCap: e.budgetPolicy.CascadeDailyMaxOutputTokens,

		LensCapUSD: e.budgetPolicy.LensDailyMaxUSD,
		LensOutCap: e.budgetPolicy.LensDailyMaxOutputTokens,
	}

	hasData := false
	if cascadeID != "" {
		if spend, _, outToks, err := e.spendLedger.GetCascadeSpendUSD(ctx, tenantID, cascadeID, day); err == nil {
			out.CascadeSpendUSD = spend
			out.CascadeOut = outToks
			hasData = true
		}
	}
	if lensID != "" {
		if spend, _, outToks, err := e.spendLedger.GetLensDaySpendUSD(ctx, tenantID, lensID, day); err == nil {
			out.LensSpendUSD = spend
			out.LensOut = outToks
			hasData = true
		}
	}
	if !hasData {
		return 1.0, nil, false
	}

	pressure := 0.0
	updatePressure := func(v float64) {
		if v > pressure {
			pressure = v
		}
	}
	updatePressure(budgetRatio(out.CascadeSpendUSD, out.CascadeCapUSD))
	updatePressure(budgetRatio(float64(out.CascadeOut), float64(out.CascadeOutCap)))
	updatePressure(budgetRatio(out.LensSpendUSD, out.LensCapUSD))
	updatePressure(budgetRatio(float64(out.LensOut), float64(out.LensOutCap)))
	out.MaxPressure = pressure

	return budgetFanoutScaleForPressure(pressure), out, true
}
