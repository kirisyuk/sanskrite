package propagation

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	policyMetricPrefix = "/metric/policy/macro/"

	MetricBudgetRevenueDeltaPct        = policyMetricPrefix + "budget_revenue_delta_pct"
	MetricBudgetRevenueDeltaBlnKZT     = policyMetricPrefix + "budget_revenue_delta_bln_kzt"
	MetricFiscalBalanceDeltaPctGDP     = policyMetricPrefix + "fiscal_balance_delta_pct_gdp"
	MetricGDPRealDeltaPct              = policyMetricPrefix + "gdp_real_delta_pct"
	MetricCPIDeltaPct                  = policyMetricPrefix + "cpi_delta_pct"
	MetricHouseholdRealIncomeDeltaPct  = policyMetricPrefix + "household_real_income_delta_pct"
	MetricConsumptionRealDeltaPct      = policyMetricPrefix + "consumption_real_delta_pct"
	MetricBusinessMarginDeltaPct       = policyMetricPrefix + "business_margin_delta_pct"
	MetricInvestmentDeltaPct           = policyMetricPrefix + "investment_delta_pct"
	MetricComplianceRateDeltaPct       = policyMetricPrefix + "compliance_rate_delta_pct"
	MetricShadowEconomyShareDeltaPct   = policyMetricPrefix + "shadow_economy_share_delta_pct"
	MetricTransmissionIntensityIndex   = policyMetricPrefix + "transmission_intensity_index"
	defaultMacroPriceBasketShare       = 0.45
	defaultMacroPassThrough            = 0.75
	defaultMacroDemandElasticity       = -0.35
	defaultMacroComplianceElasticity   = 0.14
	defaultMacroShadowSensitivity      = 0.18
	defaultMacroWageIndexationRate     = 0.30
	defaultMacroInvestmentSensitivity  = 0.45
	defaultMacroConsumptionTaxBaseCoef = 0.80
)

var macroEconomicalMetricSpecs = []PolicyMetricSpec{
	{MetricPath: MetricBudgetRevenueDeltaPct, Unit: "percent", Description: "Change in VAT-related budget revenue relative to baseline.", BaselineKey: "baseline_vat_revenue_bln_kzt"},
	{MetricPath: MetricBudgetRevenueDeltaBlnKZT, Unit: "bln_kzt", Description: "Absolute VAT-related budget revenue delta.", BaselineKey: "baseline_vat_revenue_bln_kzt"},
	{MetricPath: MetricFiscalBalanceDeltaPctGDP, Unit: "percent_of_gdp", Description: "Estimated fiscal balance delta as % of GDP.", BaselineKey: "baseline_gdp_bln_kzt"},
	{MetricPath: MetricGDPRealDeltaPct, Unit: "percent", Description: "Estimated real GDP delta."},
	{MetricPath: MetricCPIDeltaPct, Unit: "percent", Description: "Estimated CPI delta from VAT pass-through."},
	{MetricPath: MetricHouseholdRealIncomeDeltaPct, Unit: "percent", Description: "Estimated household real income delta."},
	{MetricPath: MetricConsumptionRealDeltaPct, Unit: "percent", Description: "Estimated real consumption delta."},
	{MetricPath: MetricBusinessMarginDeltaPct, Unit: "percent", Description: "Estimated business margin delta."},
	{MetricPath: MetricInvestmentDeltaPct, Unit: "percent", Description: "Estimated investment delta."},
	{MetricPath: MetricComplianceRateDeltaPct, Unit: "percent", Description: "Estimated tax compliance rate delta."},
	{MetricPath: MetricShadowEconomyShareDeltaPct, Unit: "percent", Description: "Estimated shadow economy share delta."},
	{MetricPath: MetricTransmissionIntensityIndex, Unit: "index", Description: "Signal transmission intensity for scenario->graph mapping."},
}

// MacroEconomicalEngine is the first policy-impact engine focused on macroeconomic outcomes.
type MacroEconomicalEngine struct{}

func NewMacroEconomicalEngine() *MacroEconomicalEngine { return &MacroEconomicalEngine{} }

func (e *MacroEconomicalEngine) EngineID() string { return PolicyImpactEngineMacroEconomicalV1 }

func (e *MacroEconomicalEngine) MetricSpecs() []PolicyMetricSpec {
	out := make([]PolicyMetricSpec, len(macroEconomicalMetricSpecs))
	copy(out, macroEconomicalMetricSpecs)
	return out
}

func (e *MacroEconomicalEngine) Encode(ctx context.Context, scenario ScenarioInput) ([]*horizonmodels.SignalDescriptor, error) {
	_ = ctx
	delta, ok := policyImpactVatDelta(scenario.Parameters)
	if !ok {
		return nil, fmt.Errorf("macro engine encode: vat delta is missing (expected vat_rate_from/vat_rate_to or vat_rate_delta)")
	}
	amplitudeBase := clamp01(math.Abs(delta) / 10.0) // 4pp -> 0.40
	if amplitudeBase == 0 {
		return []*horizonmodels.SignalDescriptor{}, nil
	}

	passThrough := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroPassThrough, "pass_through_rate", "passThroughRate", "pass_through_estimate"), 0.30, 1.00)
	demandElasticity := firstNumericOrDefault(scenario.Parameters, defaultMacroDemandElasticity, "demand_elasticity", "demandElasticity", "elasticity_hint")
	complianceElasticity := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroComplianceElasticity, "compliance_elasticity", "complianceElasticity"), 0.02, 0.45)
	shadowSensitivity := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroShadowSensitivity, "shadow_sensitivity", "shadowSensitivity"), 0.02, 0.65)

	polarity := "risk"
	mechanism := "vat_rate_increase"
	if delta < 0 {
		polarity = "opportunity"
		mechanism = "vat_rate_decrease"
	}
	seedPaths := normalizeCanonicalPaths(scenario.TargetPaths)
	locationPath := policyImpactLocationPath(scenario.Parameters)
	if locationPath == "" {
		locationPath = defaultPolicyImpactLocation
	}
	scenarioID := strings.TrimSpace(scenario.ScenarioID)
	if scenarioID == "" {
		scenarioID = e.EngineID()
	}
	now := time.Now().UTC()
	build := func(suffix, axis string, amp float64) *horizonmodels.SignalDescriptor {
		return &horizonmodels.SignalDescriptor{
			ID:           scenarioID + ":" + suffix,
			CreatedAt:    now,
			Axis:         axis,
			Mechanism:    mechanism,
			Amplitude:    clamp01(amp),
			Polarity:     polarity,
			SeedPaths:    append([]string(nil), seedPaths...),
			LocationPath: locationPath,
			Constraints: horizonmodels.SignalConstraints{
				TimeHorizon: horizonmodels.SignalTimeHorizonShortTerm,
				Timeframe:   horizonmodels.SignalTimeHorizonShortTerm,
				Modality:    horizonmodels.SignalModalityProjected,
			},
			Confidence: 0.70,
			ObservedAt: now,
		}
	}

	settlementAmp := amplitudeBase * clamp(0.75+passThrough*0.25, 0.55, 1.00)
	demandAmp := amplitudeBase * clamp(0.55+math.Abs(demandElasticity)*0.25, 0.45, 0.95)
	complianceAmp := amplitudeBase * clamp(0.45+complianceElasticity*0.35, 0.35, 0.85)
	uncertaintyAmp := amplitudeBase * clamp(0.35+shadowSensitivity*0.45, 0.25, 0.80)

	return []*horizonmodels.SignalDescriptor{
		build("settlement", horizonmodels.AxisFinancialSettlement, settlementAmp),
		build("demand", horizonmodels.AxisPhysicalFlowDemand, demandAmp),
		build("compliance", horizonmodels.AxisLegalCompliance, complianceAmp),
		build("uncertainty", horizonmodels.AxisInfoUncertainty, uncertaintyAmp),
	}, nil
}

func (e *MacroEconomicalEngine) TransferHook(ctx context.Context, signal *horizonmodels.SignalDescriptor, edge *horizonmodels.DependencyEdge, node *horizonmodels.HorizonFragment) (*TransferResult, error) {
	_ = ctx
	_ = node
	if signal == nil || edge == nil {
		return nil, nil
	}

	factor := 1.0
	adjustedAxis := ""
	if v, ok := qualifierFloat(edge.Qualifiers, "pass_through_estimate", "passThroughEstimate"); ok {
		factor *= clamp(v, 0.25, 1.35)
	}
	if v, ok := qualifierFloat(edge.Qualifiers, "tax_base_coverage", "taxBaseCoverage"); ok {
		factor *= clamp(v, 0.30, 1.25)
	}
	if v, ok := qualifierFloat(edge.Qualifiers, "elasticity_hint", "demandElasticity"); ok && strings.TrimSpace(signal.Axis) == horizonmodels.AxisPhysicalFlowDemand {
		factor *= clamp(1.0+math.Abs(v)*0.20, 0.75, 1.55)
	}
	if v, ok := qualifierFloat(edge.Qualifiers, "sector_sensitivity", "sectorSensitivity"); ok {
		factor *= clamp(v, 0.50, 1.60)
	}

	channel := strings.ToLower(strings.TrimSpace(qualifierString(edge.Qualifiers, "channel_type", "channelType")))
	switch channel {
	case "evasion":
		if strings.TrimSpace(signal.Axis) == horizonmodels.AxisFinancialSettlement || strings.TrimSpace(signal.Axis) == horizonmodels.AxisLegalCompliance {
			adjustedAxis = horizonmodels.AxisInfoUncertainty
		}
		factor *= 0.75
	case "substitution":
		if strings.TrimSpace(signal.Axis) == horizonmodels.AxisPhysicalFlowDemand {
			factor *= 1.10
		}
	case "behavioral":
		if strings.TrimSpace(signal.Axis) == horizonmodels.AxisPhysicalFlowDemand {
			adjustedAxis = horizonmodels.AxisInfoReputation
		}
		factor *= 0.95
	}

	switch edge.Type {
	case horizonmodels.DependencyTypeAppliesTo, horizonmodels.DependencyTypeImposesCondition, horizonmodels.DependencyTypeControlsAccess:
		factor *= 1.05
	case horizonmodels.DependencyTypeDependsOn:
		factor *= 0.95
	case horizonmodels.DependencyTypeRegulates:
		factor *= 1.03
	}

	adjustedAmplitude := clamp01(signal.Amplitude * factor)
	if adjustedAmplitude < 0.01 {
		return &TransferResult{Skip: true}, nil
	}
	return &TransferResult{
		AdjustedAmplitude: adjustedAmplitude,
		AdjustedAxis:      strings.TrimSpace(adjustedAxis),
	}, nil
}

func (e *MacroEconomicalEngine) Decode(ctx context.Context, node *horizonmodels.HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error) {
	_ = ctx
	_ = node

	if len(accumulated) == 0 {
		return []BusinessMetric{}, nil
	}
	axes := aggregateMacroAxes(accumulated)
	transmissionIntensity := clamp(
		0.45+axes.settlement*0.90+axes.demand*0.70+axes.uncertainty*0.50+axes.compliance*0.40+axes.reputation*0.20,
		0.35,
		2.50,
	)

	vatDelta, hasVatDelta := policyImpactVatDelta(scenario.Parameters)
	if !hasVatDelta {
		vatDelta = (axes.settlement + axes.demand + axes.compliance) * 2.0
	}
	vatFrom, vatTo, hasBounds := policyImpactVatRateBounds(scenario.Parameters)
	if !hasBounds {
		vatFrom = firstNumericOrDefault(scenario.Parameters, 12.0, "vat_rate_from", "vatRateFrom", "vat_from")
		vatTo = vatFrom + vatDelta
	}

	passThrough := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroPassThrough, "pass_through_rate", "passThroughRate", "pass_through_estimate"), 0.30, 1.00)
	priceBasketShare := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroPriceBasketShare, "price_basket_share", "priceBasketShare", "cpi_taxable_share"), 0.10, 0.95)
	demandElasticity := firstNumericOrDefault(scenario.Parameters, defaultMacroDemandElasticity, "demand_elasticity", "demandElasticity", "elasticity_hint")
	complianceElasticity := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroComplianceElasticity, "compliance_elasticity", "complianceElasticity"), 0.02, 0.45)
	shadowSensitivity := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroShadowSensitivity, "shadow_sensitivity", "shadowSensitivity"), 0.02, 0.65)
	wageIndexationRate := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroWageIndexationRate, "wage_indexation_rate", "wageIndexationRate"), 0.00, 1.00)
	investmentSensitivity := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroInvestmentSensitivity, "investment_sensitivity", "investmentSensitivity"), 0.10, 1.20)
	consumptionTaxBaseCoef := clamp(firstNumericOrDefault(scenario.Parameters, defaultMacroConsumptionTaxBaseCoef, "consumption_tax_base_coef", "consumptionTaxBaseCoef"), 0.20, 1.30)

	cpiDeltaPct := vatDelta * passThrough * priceBasketShare * transmissionIntensity
	householdRealIncomeDeltaPct := -(cpiDeltaPct * (1.0 - wageIndexationRate))
	consumptionRealDeltaPct := demandElasticity * cpiDeltaPct * transmissionIntensity
	complianceRateDeltaPct := -(vatDelta * complianceElasticity * transmissionIntensity)
	shadowEconomyShareDeltaPct := math.Max(0.0, vatDelta*shadowSensitivity*transmissionIntensity) + math.Max(0.0, -complianceRateDeltaPct*0.35)
	businessMarginDeltaPct := -(cpiDeltaPct*(1.0-passThrough))*0.80 - shadowEconomyShareDeltaPct*0.08
	investmentDeltaPct := businessMarginDeltaPct*investmentSensitivity + consumptionRealDeltaPct*0.20
	gdpRealDeltaPct := consumptionRealDeltaPct*0.55 + investmentDeltaPct*0.25 - shadowEconomyShareDeltaPct*0.10

	rateEffectPct := 0.0
	if vatFrom > 0 && vatTo > 0 {
		rateEffectPct = ((vatTo / vatFrom) - 1.0) * 100.0
	}
	taxBaseEffectPct := consumptionRealDeltaPct * consumptionTaxBaseCoef
	complianceEffectPct := complianceRateDeltaPct
	shadowEffectPct := -shadowEconomyShareDeltaPct * 0.35
	budgetRevenueDeltaPct := rateEffectPct + taxBaseEffectPct + complianceEffectPct + shadowEffectPct

	baselineVATRevenueBln := firstNumericOrDefault(
		scenario.Parameters,
		0.0,
		"baseline_vat_revenue_bln_kzt",
		"baselineVatRevenueBlnKZT",
		"baseline_tax_revenue_bln_kzt",
	)
	budgetRevenueDeltaBln := baselineVATRevenueBln * budgetRevenueDeltaPct / 100.0
	baselineGDPBln := firstNumericOrDefault(scenario.Parameters, 0.0, "baseline_gdp_bln_kzt", "baselineGDPBlnKZT")
	fiscalBalanceDeltaPctGDP := 0.0
	if baselineGDPBln > 0 {
		fiscalBalanceDeltaPctGDP = budgetRevenueDeltaBln / baselineGDPBln * 100.0
	}

	confidence := macroConfidence(axes, baselineVATRevenueBln, baselineGDPBln)
	out := []BusinessMetric{
		{MetricPath: MetricTransmissionIntensityIndex, Value: transmissionIntensity, Unit: "index", Confidence: confidence},
		{MetricPath: MetricCPIDeltaPct, Value: cpiDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricHouseholdRealIncomeDeltaPct, Value: householdRealIncomeDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricConsumptionRealDeltaPct, Value: consumptionRealDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricBusinessMarginDeltaPct, Value: businessMarginDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricInvestmentDeltaPct, Value: investmentDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricGDPRealDeltaPct, Value: gdpRealDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricComplianceRateDeltaPct, Value: complianceRateDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricShadowEconomyShareDeltaPct, Value: shadowEconomyShareDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricBudgetRevenueDeltaPct, Value: budgetRevenueDeltaPct, Unit: "percent", Confidence: confidence},
		{MetricPath: MetricBudgetRevenueDeltaBlnKZT, Value: budgetRevenueDeltaBln, Unit: "bln_kzt", Confidence: confidence},
		{MetricPath: MetricFiscalBalanceDeltaPctGDP, Value: fiscalBalanceDeltaPctGDP, Unit: "percent_of_gdp", Confidence: confidence},
	}
	return out, nil
}

type macroAxesState struct {
	settlement  float64
	demand      float64
	uncertainty float64
	reputation  float64
	compliance  float64
}

func aggregateMacroAxes(in []AccumulatedSignal) macroAxesState {
	var out macroAxesState
	for _, s := range in {
		amp := clamp01(math.Abs(s.Amplitude))
		switch strings.TrimSpace(s.Axis) {
		case horizonmodels.AxisFinancialSettlement:
			out.settlement += amp
		case horizonmodels.AxisPhysicalFlowDemand:
			out.demand += amp
		case horizonmodels.AxisInfoUncertainty:
			out.uncertainty += amp
		case horizonmodels.AxisInfoReputation:
			out.reputation += amp
		case horizonmodels.AxisLegalCompliance:
			out.compliance += amp
		}
	}
	out.settlement = clamp(out.settlement, 0.0, 1.5)
	out.demand = clamp(out.demand, 0.0, 1.5)
	out.uncertainty = clamp(out.uncertainty, 0.0, 1.5)
	out.reputation = clamp(out.reputation, 0.0, 1.5)
	out.compliance = clamp(out.compliance, 0.0, 1.5)
	return out
}

func firstNumericOrDefault(params map[string]interface{}, defaultValue float64, keys ...string) float64 {
	if v, ok := firstNumeric(params, keys...); ok {
		return v
	}
	return defaultValue
}

func macroConfidence(axes macroAxesState, baselineVATRevenueBln float64, baselineGDPBln float64) float64 {
	coverage := clamp(
		axes.settlement*0.28+axes.demand*0.24+axes.uncertainty*0.20+axes.compliance*0.20+axes.reputation*0.08,
		0.0,
		1.0,
	)
	conf := 0.42 + coverage*0.30
	if baselineVATRevenueBln > 0 {
		conf += 0.08
	}
	if baselineGDPBln > 0 {
		conf += 0.05
	}
	return clamp(conf, 0.35, 0.90)
}
