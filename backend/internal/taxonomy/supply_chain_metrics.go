package taxonomy

import "strings"

// Supply chain metric keys and allowed units.
// Used by SupplyLink.State, SegmentMetrics, and parser normalization.

// SupplyChainMetricKeys defines canonical metric names for supply chain edges.
var SupplyChainMetricKeys = []string{
	"volume", "share", "contract_value", "lead_time_days", "on_time_delivery", "confidence",
}

// SupplyChainMetricAllowedUnits maps metric key to allowed unit tokens.
// LLM chooses unit by context (batteries→gwh, lithium→metric_tons, oil→bbl).
var SupplyChainMetricAllowedUnits = map[string][]string{
	"volume": {
		"gwh_per_year", "mwh_per_year", "kwh_per_year",
		"metric_tons_per_year", "kt_per_year", "oz_per_year",
		"bpd", "mbpd", "mcf_per_day", "mmcf_per_day",
		"units_per_year", "cells",
	},
	"share":            {"percent", "ratio"},
	"contract_value":   {"usd", "eur", "rub"},
	"lead_time_days":   {"days"},
	"on_time_delivery": {"ratio"},
	"confidence":      {}, // ratio 0-1, no unit
}

// supplyChainUnitSynonyms normalizes common unit variants to canonical form.
var supplyChainUnitSynonyms = map[string]string{
	"gwh/year": "gwh_per_year", "gwh/yr": "gwh_per_year", "gwh per year": "gwh_per_year",
	"mwh/year": "mwh_per_year", "mwh/yr": "mwh_per_year",
	"kt/year": "kt_per_year", "kt/yr": "kt_per_year", "kt per year": "kt_per_year",
	"mt": "metric_tons", "tonnes": "metric_tons", "tonne": "metric_tons",
	"tons/year": "metric_tons_per_year", "tpy": "metric_tons_per_year",
}

// NormalizeSupplyChainMetricUnit normalizes unit for SupplyLink.State.
// Applies synonyms (e.g. "GWh/year" → "gwh_per_year), validates via IsValidMetricUnit.
// Returns normalized unit; if invalid, returns raw lowercase for fail-open (caller may store raw_unit).
func NormalizeSupplyChainMetricUnit(unit string, isCurrency bool) string {
	u := strings.ToLower(strings.TrimSpace(unit))
	if u == "" {
		return ""
	}
	if canon, ok := supplyChainUnitSynonyms[u]; ok {
		u = canon
	}
	if IsValidMetricUnit(u, isCurrency) {
		return u
	}
	return u // fail-open: return as-is for partial save
}
