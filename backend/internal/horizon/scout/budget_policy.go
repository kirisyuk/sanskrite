package scout

import (
	"os"
	"strconv"
	"strings"
)

// BudgetPolicy defines daily guardrails for Horizon Scout execution.
// All limits are per UTC day.
type BudgetPolicy struct {
	Enabled bool

	// USD budgets (daily)
	CascadeDailyMaxUSD float64 // default 1.00
	LensDailyMaxUSD    float64 // default 8.00

	// Token budgets (daily). These are enforced as fallback safety even if cost attribution fails.
	// Default values are derived from a conservative pricing envelope (gemini-3-flash-preview output $3/1M).
	CascadeDailyMaxOutputTokens int // default derived from CascadeDailyMaxUSD
	LensDailyMaxOutputTokens    int // default derived from LensDailyMaxUSD
}

func DefaultBudgetPolicy() BudgetPolicy {
	p := BudgetPolicy{
		Enabled:                     true,
		CascadeDailyMaxUSD:          1.00,
		LensDailyMaxUSD:             8.00,
		CascadeDailyMaxOutputTokens: 0,
		LensDailyMaxOutputTokens:    0,
	}
	// Conservative default token caps based on gemini-3-flash-preview output pricing ($3 / 1M tokens).
	// tokensPerUSD = 1_000_000 / 3 = 333_333.33
	p.CascadeDailyMaxOutputTokens = int(p.CascadeDailyMaxUSD * (1000000.0 / 3.0))
	p.LensDailyMaxOutputTokens = int(p.LensDailyMaxUSD * (1000000.0 / 3.0))
	if p.CascadeDailyMaxOutputTokens < 1000 {
		p.CascadeDailyMaxOutputTokens = 1000
	}
	if p.LensDailyMaxOutputTokens < 1000 {
		p.LensDailyMaxOutputTokens = 1000
	}
	return p
}

// BudgetPolicyFromEnv reads env vars and falls back to defaults.
//
// Env vars:
// - HORIZON_BUDGETS_ENABLED (default "1")
// - HORIZON_CASCADE_DAILY_MAX_USD (default 1.00)
// - HORIZON_LENS_DAILY_MAX_USD (default 8.00)
// - HORIZON_CASCADE_DAILY_MAX_OUTPUT_TOKENS (default derived)
// - HORIZON_LENS_DAILY_MAX_OUTPUT_TOKENS (default derived)
func BudgetPolicyFromEnv() BudgetPolicy {
	p := DefaultBudgetPolicy()

	if v := strings.TrimSpace(os.Getenv("HORIZON_BUDGETS_ENABLED")); v != "" {
		p.Enabled = !(v == "0" || strings.EqualFold(v, "false"))
	}
	if v := strings.TrimSpace(os.Getenv("HORIZON_CASCADE_DAILY_MAX_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			p.CascadeDailyMaxUSD = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("HORIZON_LENS_DAILY_MAX_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			p.LensDailyMaxUSD = f
		}
	}
	// Re-derive token caps from possibly updated USD budgets.
	p.CascadeDailyMaxOutputTokens = int(p.CascadeDailyMaxUSD * (1000000.0 / 3.0))
	p.LensDailyMaxOutputTokens = int(p.LensDailyMaxUSD * (1000000.0 / 3.0))

	if v := strings.TrimSpace(os.Getenv("HORIZON_CASCADE_DAILY_MAX_OUTPUT_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.CascadeDailyMaxOutputTokens = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("HORIZON_LENS_DAILY_MAX_OUTPUT_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.LensDailyMaxOutputTokens = n
		}
	}
	if p.CascadeDailyMaxOutputTokens < 0 {
		p.CascadeDailyMaxOutputTokens = 0
	}
	if p.LensDailyMaxOutputTokens < 0 {
		p.LensDailyMaxOutputTokens = 0
	}
	return p
}
