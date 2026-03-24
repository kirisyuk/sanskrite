package simulation

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/kzvat"
)

func scenarioFallbackPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]map[string]any, bool) {
	if isSLBRussiaHotPath(run) {
		if raw, ok := scenarioDSLPopulation(run, world); ok {
			if extra := slbRussiaDerivedPopulationExtras(run); len(extra) > 0 {
				return append(raw, extra...), true
			}
			return raw, true
		}
		return slbRussiaFallbackPopulation(run, world)
	}
	if isTeslaBatteryHotPath(run) {
		if raw, ok := scenarioDSLPopulation(run, world); ok {
			return raw, true
		}
		return teslaBatteryFallbackPopulation(run, world)
	}
	if isSpaceSatelliteHotPath(run) {
		if raw, ok := scenarioDSLPopulation(run, world); ok {
			return raw, true
		}
		return spaceSatelliteFallbackPopulation(run, world)
	}
	if !isKZVATHotPath(run) {
		return nil, false
	}
	if raw, ok := scenarioDSLPopulation(run, world); ok {
		return raw, true
	}
	variant := worldVariant(world)
	out := make([]map[string]any, 0, 96)

	add := func(agentClass, displayName string, weight float64, originRefs []string, allowedActions []string, attention, riskBias, trust, stress, budget float64, messageStyle string, policyHints map[string]any) {
		out = append(out, map[string]any{
			"agentClass":       agentClass,
			"displayName":      displayName,
			"originKind":       "scenario_archetype",
			"originRefs":       compactStrings(originRefs),
			"generatedBy":      "scenario.kzvat.population",
			"justification":    "Kazakhstan VAT 16% hot-path archetype",
			"populationWeight": weight,
			"traits": map[string]any{
				"attention": attention,
				"riskBias":  riskBias,
				"variant":   variant,
			},
			"beliefState": map[string]any{
				"trust":  trust,
				"stress": stress,
			},
			"resourceState": map[string]any{
				"budget": budget,
			},
			"allowedActions": allowedActions,
			"messageStyle":   messageStyle,
			"policyHints":    policyHints,
		})
	}

	add("regulator", "MinFin policy desk", 1.6, []string{"/gov/kz/ministry-of-finance"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.92, 0.66, 0.72, 0.38, 0.75, "institutional", map[string]any{"role": "policy_design"})
	add("regulator", "State revenue enforcement desk", 1.5, []string{"/gov/kz/state-revenue-committee"}, []string{
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.71, 0.68, 0.44, 0.70, "bureaucratic", map[string]any{"role": "enforcement"})

	for i := 0; i < 4; i++ {
		add("retailer", fmt.Sprintf("Retail chain desk %d", i+1), 1.35+0.08*float64(i), []string{"/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionBroadcastMessage),
		}, 0.70+0.03*float64(i), 0.48+0.04*float64(i), 0.46, 0.42+0.05*float64(i), 0.52+0.04*float64(i), "commercial", map[string]any{"market": "retail"})
	}
	for i := 0; i < 2; i++ {
		add("large_enterprise_retail", fmt.Sprintf("Large retail treasury %d", i+1), 1.62+0.06*float64(i), []string{"/market/kz/consumer/retail", "/market/kz/finance/working-capital"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionSell),
		}, 0.84+0.02*float64(i), 0.60+0.03*float64(i), 0.42, 0.48+0.03*float64(i), 0.74+0.03*float64(i), "enterprise-commercial", map[string]any{"segment": "large-retail"})
	}
	for i := 0; i < 4; i++ {
		add("importer", fmt.Sprintf("Importer-distributor desk %d", i+1), 1.32+0.06*float64(i), []string{"/market/kz/industrial/import-distribution"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDelayPurchase),
		}, 0.76+0.02*float64(i), 0.58+0.04*float64(i), 0.40, 0.48+0.04*float64(i), 0.44+0.04*float64(i), "operational", map[string]any{"market": "imports"})
	}
	for i := 0; i < 2; i++ {
		add("large_enterprise_importer", fmt.Sprintf("Large importer treasury %d", i+1), 1.58+0.05*float64(i), []string{"/market/kz/industrial/import-distribution", "/market/kz/finance/working-capital"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionIssueStatement),
		}, 0.86+0.02*float64(i), 0.62+0.03*float64(i), 0.40, 0.52+0.03*float64(i), 0.70+0.03*float64(i), "enterprise-import", map[string]any{"segment": "large-import"})
	}
	for i := 0; i < 2; i++ {
		add("large_enterprise_manufacturer", fmt.Sprintf("Large manufacturer CFO %d", i+1), 1.46+0.04*float64(i), []string{"/market/kz/consumer/food", "/market/kz/industrial/import-distribution"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionIssueStatement),
		}, 0.80+0.02*float64(i), 0.58+0.03*float64(i), 0.44, 0.46+0.03*float64(i), 0.68+0.03*float64(i), "enterprise-manufacturing", map[string]any{"segment": "large-manufacturing"})
	}
	for i := 0; i < 1; i++ {
		add("enterprise_treasury", fmt.Sprintf("Enterprise treasury council %d", i+1), 1.52, []string{"/market/kz/finance/working-capital", "/program/kz/vat-reform-2026"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.90, 0.60, 0.46, 0.48, 0.76, "corporate-finance", map[string]any{"segment": "enterprise-finance"})
	}
	for i := 0; i < 3; i++ {
		add("bank", fmt.Sprintf("Working capital bank desk %d", i+1), 1.20+0.04*float64(i), []string{"/market/kz/finance/working-capital"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
			string(horizonmodels.SimulationActionLobby),
		}, 0.68+0.02*float64(i), 0.44+0.03*float64(i), 0.52, 0.34+0.03*float64(i), 0.60+0.03*float64(i), "financial", map[string]any{"market": "working-capital"})
	}
	for i := 0; i < 3; i++ {
		add("supplier", fmt.Sprintf("Domestic supplier desk %d", i+1), 1.18+0.04*float64(i), []string{"/market/kz/consumer/food", "/market/kz/industrial/import-distribution"}, []string{
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDelayPurchase),
		}, 0.66+0.03*float64(i), 0.48+0.03*float64(i), 0.45, 0.40+0.04*float64(i), 0.46+0.03*float64(i), "operational", map[string]any{"role": "supplier"})
	}
	for i := 0; i < 2; i++ {
		add("wholesaler", fmt.Sprintf("Wholesale intermediary %d", i+1), 1.14+0.04*float64(i), []string{"/market/kz/industrial/import-distribution", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.70+0.02*float64(i), 0.52+0.03*float64(i), 0.40, 0.42+0.04*float64(i), 0.44+0.04*float64(i), "trade", map[string]any{"role": "wholesale"})
	}
	for i := 0; i < 2; i++ {
		add("payment_rail", fmt.Sprintf("Payment rail desk %d", i+1), 1.02+0.03*float64(i), []string{"/market/kz/finance/working-capital"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
			string(horizonmodels.SimulationActionBroadcastMessage),
		}, 0.76+0.02*float64(i), 0.40+0.03*float64(i), 0.54, 0.32+0.03*float64(i), 0.58+0.02*float64(i), "payments", map[string]any{"role": "payments"})
	}
	for i := 0; i < 2; i++ {
		add("regional_official", fmt.Sprintf("Regional official %d", i+1), 1.06+0.02*float64(i), []string{"/loc/kz", "/program/kz/vat-reform-2026"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionEnforceRule),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.82, 0.50+0.04*float64(i), 0.58, 0.38+0.03*float64(i), 0.52, "official", map[string]any{"role": "regional-execution"})
	}
	for i := 0; i < 3; i++ {
		add("logistics_operator", fmt.Sprintf("Logistics operator %d", i+1), 1.10+0.03*float64(i), []string{"/market/kz/industrial/import-distribution", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.74+0.02*float64(i), 0.50+0.03*float64(i), 0.44, 0.42+0.04*float64(i), 0.46+0.03*float64(i), "logistics", map[string]any{"role": "distribution"})
	}
	for i := 0; i < 2; i++ {
		add("warehouse_operator", fmt.Sprintf("Warehouse operator %d", i+1), 1.02+0.03*float64(i), []string{"/market/kz/industrial/import-distribution", "/market/kz/consumer/food"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.68+0.03*float64(i), 0.48+0.03*float64(i), 0.42, 0.40+0.03*float64(i), 0.44+0.03*float64(i), "inventory", map[string]any{"role": "warehouse"})
	}
	for i := 0; i < 2; i++ {
		add("restaurant_operator", fmt.Sprintf("Restaurant operator %d", i+1), 0.98+0.03*float64(i), []string{"/market/kz/consumer/food", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.70+0.02*float64(i), 0.58+0.03*float64(i), 0.38, 0.46+0.03*float64(i), 0.36+0.02*float64(i), "hospitality", map[string]any{"role": "horeca"})
	}
	for i := 0; i < 2; i++ {
		add("tax_advisor", fmt.Sprintf("Tax advisor %d", i+1), 0.92+0.02*float64(i), []string{"/program/kz/vat-reform-2026", "/gov/kz/state-revenue-committee"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.86, 0.42+0.02*float64(i), 0.52, 0.28+0.03*float64(i), 0.52, "expert", map[string]any{"role": "tax-interpretation"})
	}
	for i := 0; i < 2; i++ {
		add("municipal_buyer", fmt.Sprintf("Municipal procurement desk %d", i+1), 1.00+0.03*float64(i), []string{"/loc/kz", "/program/kz/vat-reform-2026"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.72+0.02*float64(i), 0.46+0.03*float64(i), 0.50, 0.38+0.03*float64(i), 0.48+0.03*float64(i), "procurement", map[string]any{"role": "public-procurement"})
	}
	for i := 0; i < 2; i++ {
		add("budget_revenue_officer", fmt.Sprintf("Budget revenue desk %d", i+1), 1.24+0.03*float64(i), []string{"/market/kz/public-sector/budget-revenue", "/gov/kz/ministry-of-finance"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.88, 0.54+0.03*float64(i), 0.56, 0.40+0.03*float64(i), 0.68+0.03*float64(i), "fiscal", map[string]any{"role": "budget-revenue"})
	}
	for i := 0; i < 2; i++ {
		add("investment_climate_officer", fmt.Sprintf("Investment climate desk %d", i+1), 1.08+0.02*float64(i), []string{"/market/kz/finance/investment-climate", "/loc/kz"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionChangeBelief),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.84, 0.44+0.02*float64(i), 0.48, 0.34+0.03*float64(i), 0.58, "investment", map[string]any{"role": "investment-climate"})
	}
	for i := 0; i < 3; i++ {
		add("internal_producer", fmt.Sprintf("Domestic producer cluster %d", i+1), 1.20+0.04*float64(i), []string{"/market/kz/industrial/internal-production", "/market/kz/consumer/food"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.74+0.02*float64(i), 0.56+0.03*float64(i), 0.44, 0.42+0.03*float64(i), 0.54+0.03*float64(i), "domestic-production", map[string]any{"role": "internal-production"})
	}
	for i := 0; i < 2; i++ {
		add("healthcare_public_operator", fmt.Sprintf("Public healthcare network %d", i+1), 1.10+0.03*float64(i), []string{"/market/kz/healthcare/healthcare-public", "/market/kz/public-sector/budget-revenue"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.78+0.02*float64(i), 0.50+0.03*float64(i), 0.50, 0.42+0.03*float64(i), 0.48+0.02*float64(i), "public-health", map[string]any{"role": "healthcare-public"})
	}
	for i := 0; i < 2; i++ {
		add("healthcare_private_operator", fmt.Sprintf("Private healthcare chain %d", i+1), 1.02+0.03*float64(i), []string{"/market/kz/healthcare/healthcare-private", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.76+0.02*float64(i), 0.52+0.03*float64(i), 0.42, 0.40+0.03*float64(i), 0.50+0.02*float64(i), "private-health", map[string]any{"role": "healthcare-private"})
	}
	for i := 0; i < 2; i++ {
		add("engineering_infrastructure_operator", fmt.Sprintf("Engineering infrastructure operator %d", i+1), 1.12+0.03*float64(i), []string{"/market/kz/construction/engineering-services", "/market/kz/public-sector/budget-revenue"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.74+0.02*float64(i), 0.54+0.03*float64(i), 0.46, 0.44+0.03*float64(i), 0.52+0.02*float64(i), "infrastructure", map[string]any{"role": "engineering-infrastructure"})
	}
	for i := 0; i < 2; i++ {
		add("education_system_operator", fmt.Sprintf("Education system operator %d", i+1), 1.04+0.03*float64(i), []string{"/market/kz/education/education-system", "/market/kz/public-sector/budget-revenue"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.76+0.02*float64(i), 0.48+0.02*float64(i), 0.52, 0.38+0.03*float64(i), 0.50+0.02*float64(i), "education", map[string]any{"role": "education-system"})
	}
	for i := 0; i < 4; i++ {
		add("consumer_urban_low_income", fmt.Sprintf("Urban low-income household %d", i+1), 1.56+0.05*float64(i), []string{"/market/kz/consumer/food", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.60+0.02*float64(i), 0.66+0.03*float64(i), 0.34, 0.56+0.03*float64(i), 0.24+0.02*float64(i), "consumer-urban", map[string]any{"segment": "urban-low-income", "channel": "modern-trade"})
	}
	for i := 0; i < 3; i++ {
		add("consumer_rural_low_income", fmt.Sprintf("Rural low-income household %d", i+1), 1.48+0.05*float64(i), []string{"/market/kz/consumer/food", "/community/kz/trade/bazaar-traders"}, []string{
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.50+0.02*float64(i), 0.70+0.02*float64(i), 0.30, 0.58+0.03*float64(i), 0.20+0.02*float64(i), "consumer-rural", map[string]any{"segment": "rural-low-income", "channel": "bazaar"})
	}
	for i := 0; i < 4; i++ {
		add("consumer_family_mid_income", fmt.Sprintf("Family mid-income household %d", i+1), 1.24+0.04*float64(i), []string{"/market/kz/consumer/retail", "/market/kz/consumer/food"}, []string{
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.58+0.02*float64(i), 0.50+0.02*float64(i), 0.40, 0.42+0.03*float64(i), 0.36+0.03*float64(i), "consumer-family", map[string]any{"segment": "family-mid-income", "channel": "mixed"})
	}
	for i := 0; i < 3; i++ {
		add("consumer_ecommerce_native", fmt.Sprintf("E-commerce native consumer %d", i+1), 1.08+0.03*float64(i), []string{"/market/kz/consumer/retail", "/community/kz/trade/bazaar-traders"}, []string{
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.84+0.02*float64(i), 0.42+0.02*float64(i), 0.38, 0.34+0.03*float64(i), 0.42+0.03*float64(i), "consumer-digital", map[string]any{"segment": "ecommerce-native", "channel": "digital"})
	}
	for i := 0; i < 3; i++ {
		add("consumer_bazaar_sensitive", fmt.Sprintf("Bazaar-sensitive consumer %d", i+1), 1.18+0.04*float64(i), []string{"/community/kz/trade/bazaar-traders", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.70+0.02*float64(i), 0.56+0.02*float64(i), 0.32, 0.46+0.03*float64(i), 0.28+0.02*float64(i), "consumer-bazaar", map[string]any{"segment": "bazaar-sensitive", "channel": "informal"})
	}
	for i := 0; i < 2; i++ {
		add("consumer_affluent", fmt.Sprintf("Affluent household %d", i+1), 0.92+0.04*float64(i), []string{"/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.46+0.02*float64(i), 0.28+0.02*float64(i), 0.50, 0.20+0.02*float64(i), 0.60+0.03*float64(i), "consumer-affluent", map[string]any{"segment": "affluent"})
	}
	for i := 0; i < 2; i++ {
		add("cross_border_shopper", fmt.Sprintf("Cross-border shopper %d", i+1), 0.98+0.04*float64(i), []string{"/market/kz/consumer/retail", "/market/kz/industrial/import-distribution"}, []string{
			string(horizonmodels.SimulationActionSwitchSupplier),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDelayPurchase),
		}, 0.74+0.03*float64(i), 0.48+0.03*float64(i), 0.34, 0.42+0.04*float64(i), 0.34+0.02*float64(i), "mobile-consumer", map[string]any{"role": "cross-border-demand"})
	}
	for i := 0; i < 5; i++ {
		add("small_trader", fmt.Sprintf("Bazaar trader %d", i+1), 1.10+0.06*float64(i), []string{"/community/kz/trade/bazaar-traders"}, []string{
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionDoNothing),
			string(horizonmodels.SimulationActionLobby),
		}, 0.72+0.03*float64(i), 0.50+0.04*float64(i), 0.28, 0.46+0.03*float64(i), 0.32+0.03*float64(i), "informal", map[string]any{"segment": "informal-trade"})
	}
	for i := 0; i < 2; i++ {
		add("marketplace_seller", fmt.Sprintf("Marketplace seller %d", i+1), 1.02+0.03*float64(i), []string{"/market/kz/consumer/retail", "/community/kz/trade/bazaar-traders"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.80+0.02*float64(i), 0.52+0.03*float64(i), 0.32, 0.40+0.03*float64(i), 0.30+0.03*float64(i), "digital-informal", map[string]any{"role": "marketplace"})
	}
	for i := 0; i < 2; i++ {
		add("ecommerce_platform", fmt.Sprintf("E-commerce platform %d", i+1), 1.00+0.03*float64(i), []string{"/market/kz/consumer/retail", "/community/kz/trade/bazaar-traders"}, []string{
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.88, 0.50+0.03*float64(i), 0.38, 0.36+0.03*float64(i), 0.42+0.03*float64(i), "platform", map[string]any{"role": "digital-channel"})
	}
	for i := 0; i < 2; i++ {
		add("association", fmt.Sprintf("Business association %d", i+1), 1.05+0.03*float64(i), []string{"/program/kz/vat-reform-2026"}, []string{
			string(horizonmodels.SimulationActionLobby),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionEscalate),
		}, 0.82, 0.58+0.03*float64(i), 0.44, 0.40+0.03*float64(i), 0.55, "advocacy", map[string]any{"role": "business-lobby"})
	}
	for i := 0; i < 2; i++ {
		add("agri_producer", fmt.Sprintf("Agricultural producer %d", i+1), 1.04+0.03*float64(i), []string{"/market/kz/consumer/food"}, []string{
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.68+0.03*float64(i), 0.54+0.03*float64(i), 0.44, 0.40+0.03*float64(i), 0.46+0.03*float64(i), "producer", map[string]any{"role": "food-supply"})
	}
	for i := 0; i < 2; i++ {
		add("fuel_distributor", fmt.Sprintf("Fuel distributor %d", i+1), 0.98+0.03*float64(i), []string{"/market/kz/industrial/import-distribution", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionSell),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
		}, 0.70+0.02*float64(i), 0.56+0.03*float64(i), 0.42, 0.44+0.03*float64(i), 0.50+0.03*float64(i), "input-cost", map[string]any{"role": "fuel"})
	}
	for i := 0; i < 2; i++ {
		add("analyst", fmt.Sprintf("Sell-side analyst %d", i+1), 0.90+0.03*float64(i), []string{"/market/kz/finance/working-capital", "/loc/kz"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.84, 0.46+0.02*float64(i), 0.48, 0.30+0.03*float64(i), 0.46, "analytical", map[string]any{"role": "analyst"})
	}
	for i := 0; i < 3; i++ {
		add("media", fmt.Sprintf("Media channel %d", i+1), 0.96+0.04*float64(i), []string{"/loc/kz"}, []string{
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionEscalate),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.88+0.02*float64(i), 0.54+0.04*float64(i), 0.40, 0.34+0.03*float64(i), 0.48, "narrative", map[string]any{"role": "media"})
	}
	for i := 0; i < 2; i++ {
		add("influencer", fmt.Sprintf("Consumer influencer %d", i+1), 0.94+0.03*float64(i), []string{"/loc/kz", "/market/kz/consumer/retail"}, []string{
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionEscalate),
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.92, 0.62+0.03*float64(i), 0.30, 0.44+0.03*float64(i), 0.36, "viral", map[string]any{"role": "influencer"})
	}
	for i := 0; i < 2; i++ {
		add("opposition_politician", fmt.Sprintf("Opposition voice %d", i+1), 0.90+0.03*float64(i), []string{"/loc/kz", "/decision/taxation/raise-vat-to-16pct-kazakhstan-2026"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionEscalate),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.90, 0.66+0.03*float64(i), 0.30, 0.48+0.03*float64(i), 0.44, "political", map[string]any{"role": "opposition"})
	}

	if variant == 1 || variant == 4 {
		add("media", "Regional TV debate desk", 1.08, []string{"/loc/kz"}, []string{
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionEscalate),
		}, 0.94, 0.68, 0.38, 0.48, 0.42, "amplified", map[string]any{"role": "high-amplification-media"})
	}
	if variant == 2 || variant == 3 {
		add("small_trader", "Cross-border shuttle trader", 1.16, []string{"/community/kz/trade/bazaar-traders", "/market/kz/industrial/import-distribution"}, []string{
			string(horizonmodels.SimulationActionBuy),
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionDoNothing),
		}, 0.76, 0.62, 0.26, 0.54, 0.28, "informal", map[string]any{"role": "cross-border-trade"})
	}
	if variant == 5 {
		add("tax_advisor", "VAT explainer roundtable", 1.04, []string{"/program/kz/vat-reform-2026", "/loc/kz"}, []string{
			string(horizonmodels.SimulationActionIssueStatement),
			string(horizonmodels.SimulationActionBroadcastMessage),
			string(horizonmodels.SimulationActionChangeBelief),
		}, 0.94, 0.46, 0.48, 0.34, 0.46, "explainer", map[string]any{"role": "interpretation-wave"})
	}
	if variant == 6 {
		add("municipal_buyer", "Emergency procurement desk", 1.08, []string{"/loc/kz", "/market/kz/consumer/food"}, []string{
			string(horizonmodels.SimulationActionAdjustBudget),
			string(horizonmodels.SimulationActionDelayPurchase),
			string(horizonmodels.SimulationActionIssueStatement),
		}, 0.80, 0.54, 0.46, 0.42, 0.44, "urgent-procurement", map[string]any{"role": "emergency-buying"})
	}
	return out, true
}

func scenarioFallbackActions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) ([]horizonmodels.SimulationAction, bool) {
	if isSLBRussiaHotPath(run) {
		return slbRussiaFallbackActions(run, world, agents, step)
	}
	if isTeslaBatteryHotPath(run) {
		return teslaBatteryFallbackActions(run, world, agents, step)
	}
	if isSpaceSatelliteHotPath(run) {
		return spaceSatelliteFallbackActions(run, world, agents, step)
	}
	if !isKZVATHotPath(run) {
		return nil, false
	}
	axis := ""
	if run != nil && run.Request.Signal != nil {
		axis = strings.TrimSpace(run.Request.Signal.Axis)
	}
	variant := worldVariant(world)
	out := make([]horizonmodels.SimulationAction, 0, min(len(agents), 48))
	for idx, agent := range agents {
		if !kzVATAgentParticipates(agent, axis, variant, step, idx) {
			continue
		}
		actionType, delta, targets := kzVATActionPlan(agent, axis, variant, step, idx)
		if actionType == "" {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:        actionType,
			ActorID:     agent.ID,
			TargetRefs:  compactStrings(targets),
			GeneratedBy: "scenario.kzvat.fallback",
			Payload: map[string]any{
				"delta": round3(delta),
			},
		}
		if actionType == string(horizonmodels.SimulationActionDoNothing) {
			action.Payload = nil
		}
		if actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionEscalate) {
			action.Justification = "scenario-aware public response to VAT regime change"
		}
		out = append(out, action)
		if len(out) >= 48 {
			break
		}
	}
	return out, true
}

func scenarioBootstrapAdjustments(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) (float64, float64, float64, float64, float64, bool) {
	if isSLBRussiaHotPath(run) {
		return slbRussiaBootstrapAdjustments(run, world)
	}
	if isTeslaBatteryHotPath(run) {
		return teslaBatteryBootstrapAdjustments(run, world)
	}
	if isSpaceSatelliteHotPath(run) {
		return spaceSatelliteBootstrapAdjustments(run, world)
	}
	if !isKZVATHotPath(run) {
		return 0, 0, 0, 0, 0, false
	}
	axis := ""
	if run != nil && run.Request.Signal != nil {
		axis = strings.TrimSpace(run.Request.Signal.Axis)
	}
	variant := worldVariant(world)
	risk, price, narrative, policy, supply := 0.0, 0.0, 0.0, 0.0, 0.0
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		risk += 0.03
		policy += 0.06
		supply += 0.03
	case horizonmodels.AxisFinancialSettlement:
		risk += 0.04
		price += 0.12
		supply += 0.09
	case horizonmodels.AxisPhysicalFlowDemand:
		risk += 0.02
		price += 0.09
		narrative += 0.01
		supply += 0.04
	case horizonmodels.AxisInfoUncertainty:
		risk += 0.10
		price += 0.07
		narrative += 0.12
		policy += 0.07
		supply += 0.10
	}
	switch variant {
	case 0:
		price += 0.02
		policy += 0.02
	case 1:
		risk += 0.06
		narrative += 0.08
		policy += 0.10
	case 2:
		risk += 0.04
		price += 0.08
		supply += 0.09
	case 3:
		narrative += 0.05
		price += 0.04
		supply += 0.06
	case 4:
		risk += 0.07
		narrative += 0.06
		policy += 0.05
		supply += 0.08
	case 5:
		risk += 0.05
		narrative += 0.09
		policy += 0.04
	case 6:
		risk += 0.08
		price += 0.06
		supply += 0.11
	}
	return risk, price, narrative, policy, supply, true
}

func isKZVATHotPath(run *horizonmodels.SimulationRun) bool {
	if run == nil {
		return false
	}
	question := strings.ToLower(strings.TrimSpace(run.Request.Question))
	if strings.Contains(question, "казахстан") && strings.Contains(question, "ндс") {
		return true
	}
	if runHasAnyScenarioPath(run, "/loc/kz") &&
		runHasAnyScenarioPath(run,
			"/mechanism/regulatory/vat-rate-increase",
			"/program/kz/vat-reform-2026",
			"/decision/taxation/raise-vat-to-16pct-kazakhstan-2026",
		) {
		return true
	}
	return runHasScenarioPathSet(run, kzvat.SeedPaths(), 2)
}

func worldVariant(world *horizonmodels.SimulationWorld) int {
	if world == nil {
		return 0
	}
	if world.WorldIndex < 0 {
		return 0
	}
	return world.WorldIndex % 7
}

func kzVATAgentParticipates(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) bool {
	if agent == nil {
		return false
	}
	cls := strings.TrimSpace(agent.AgentClass)
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "regulator", "regional_official", "tax_advisor", "association", "opposition_politician", "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury", "retailer", "importer", "municipal_buyer", "ecommerce_platform", "media", "analyst", "budget_revenue_officer", "investment_climate_officer", "healthcare_public_operator", "healthcare_private_operator", "engineering_infrastructure_operator", "education_system_operator", "internal_producer":
			return true
		case "consumer_bazaar_sensitive", "small_trader":
			return variant >= 3 || step >= 2
		default:
			return step >= 3 && (idx+variant)%4 == 0
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury", "importer", "retailer", "bank", "payment_rail", "logistics_operator", "warehouse_operator", "supplier", "wholesaler", "fuel_distributor", "agri_producer", "analyst", "budget_revenue_officer", "investment_climate_officer", "internal_producer", "healthcare_public_operator", "healthcare_private_operator", "engineering_infrastructure_operator", "education_system_operator":
			return true
		case "consumer_family_mid_income", "consumer_urban_low_income", "consumer_rural_low_income":
			return step >= 2 || variant >= 2
		default:
			return step >= 4 && idx%5 == 0
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "consumer_urban_low_income", "consumer_rural_low_income", "consumer_family_mid_income", "consumer_ecommerce_native", "consumer_bazaar_sensitive", "consumer_affluent", "cross_border_shopper", "small_trader", "marketplace_seller", "ecommerce_platform", "retailer", "large_enterprise_retail", "importer", "large_enterprise_importer", "restaurant_operator", "supplier", "wholesaler", "logistics_operator", "warehouse_operator", "agri_producer", "internal_producer", "healthcare_public_operator", "healthcare_private_operator", "engineering_infrastructure_operator", "education_system_operator":
			return true
		case "association", "influencer", "media":
			return step >= 1
		default:
			return step >= 4 && (idx+variant)%6 == 0
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "media", "influencer", "opposition_politician", "association", "tax_advisor", "regulator", "regional_official", "large_enterprise_retail", "large_enterprise_importer", "enterprise_treasury", "retailer", "importer", "ecommerce_platform", "consumer_ecommerce_native", "consumer_bazaar_sensitive", "budget_revenue_officer", "investment_climate_officer", "healthcare_public_operator", "healthcare_private_operator", "engineering_infrastructure_operator", "education_system_operator", "internal_producer":
			return true
		case "logistics_operator", "payment_rail", "municipal_buyer":
			return step >= 1
		default:
			return step >= 3 && (idx+variant)%5 == 0
		}
	default:
		return true
	}
}

func kzVATActionPlan(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) (string, float64, []string) {
	if agent == nil {
		return "", 0, nil
	}
	cls := strings.TrimSpace(agent.AgentClass)
	targets := compactStrings(agent.OriginRefs)
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "regulator":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionEnforceRule), 0.06 + 0.01*float64(variant), []string{"/market/kz/consumer/retail", "/market/kz/industrial/import-distribution"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05 + 0.01*float64(variant), targets
		case "association":
			if step >= 2 || variant == 1 {
				return string(horizonmodels.SimulationActionLobby), 0.08 + 0.01*float64(step%2), []string{"/gov/kz/ministry-of-finance"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, targets
		case "retailer", "importer":
			if (idx+step+variant)%3 == 0 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.05 + 0.01*float64(variant), targets
			}
			return string(horizonmodels.SimulationActionLobby), 0.04 + 0.01*float64(step%2), []string{"/program/kz/vat-reform-2026"}
		case "budget_revenue_officer":
			if step >= 2 {
				return string(horizonmodels.SimulationActionChangeBelief), 0.05, []string{"/market/kz/public-sector/budget-revenue", "/gov/kz/ministry-of-finance"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/public-sector/budget-revenue"}
		case "investment_climate_officer":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/finance/investment-climate"}
		case "healthcare_public_operator", "education_system_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, targets
		case "healthcare_private_operator":
			return string(horizonmodels.SimulationActionLobby), 0.05, []string{"/gov/kz/ministry-of-finance"}
		case "engineering_infrastructure_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/market/kz/construction/engineering-services"}
		case "internal_producer":
			return string(horizonmodels.SimulationActionLobby), 0.05, []string{"/market/kz/industrial/internal-production", "/gov/kz/ministry-of-finance"}
		case "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury":
			switch (step + variant + idx) % 3 {
			case 0:
				return string(horizonmodels.SimulationActionLobby), 0.07 + 0.01*float64(step%2), []string{"/gov/kz/ministry-of-finance", "/program/kz/vat-reform-2026"}
			case 1:
				return string(horizonmodels.SimulationActionIssueStatement), 0.06, []string{"/loc/kz"}
			default:
				return string(horizonmodels.SimulationActionAdjustBudget), 0.06 + 0.01*float64(variant%2), targets
			}
		case "tax_advisor":
			if step >= 2 {
				return string(horizonmodels.SimulationActionChangeBelief), 0.05, []string{"/program/kz/vat-reform-2026", "/gov/kz/state-revenue-committee"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/loc/kz"}
		case "municipal_buyer", "restaurant_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05 + 0.01*float64(step%2), targets
		case "logistics_operator", "warehouse_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04 + 0.01*float64(variant%2), targets
		case "ecommerce_platform":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/consumer/retail"}
		case "opposition_politician":
			if step >= 2 || variant == 6 {
				return string(horizonmodels.SimulationActionEscalate), 0.07 + 0.01*float64(step%2), []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
		case "agri_producer", "fuel_distributor":
			return string(horizonmodels.SimulationActionLobby), 0.05, []string{"/gov/kz/ministry-of-finance"}
		case "consumer_urban_low_income", "consumer_rural_low_income", "consumer_family_mid_income":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/loc/kz"}
		case "consumer_bazaar_sensitive":
			if step >= 2 {
				return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/community/kz/trade/bazaar-traders"}
			}
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "consumer_ecommerce_native":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/market/kz/consumer/retail"}
		case "consumer_affluent":
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "small_trader":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/community/kz/trade/bazaar-traders"}
			}
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "media":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.06 + 0.01*float64(variant), []string{"/loc/kz"}
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "importer":
			switch (step + variant + idx) % 3 {
			case 0:
				return string(horizonmodels.SimulationActionAdjustBudget), 0.08 + 0.01*float64(variant), targets
			case 1:
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.05 + 0.01*float64(step%2), []string{"/market/kz/industrial/import-distribution"}
			default:
				return string(horizonmodels.SimulationActionSell), 0.04 + 0.01*float64(variant), targets
			}
		case "budget_revenue_officer":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/public-sector/budget-revenue"}
		case "investment_climate_officer":
			if step >= 2 {
				return string(horizonmodels.SimulationActionChangeBelief), 0.04, []string{"/market/kz/finance/investment-climate"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/market/kz/finance/investment-climate"}
		case "internal_producer":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.06, []string{"/market/kz/industrial/internal-production"}
			}
			return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/industrial/internal-production"}
		case "healthcare_public_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/healthcare/healthcare-public"}
		case "healthcare_private_operator":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/healthcare/healthcare-private"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/healthcare/healthcare-private"}
		case "engineering_infrastructure_operator":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/market/kz/construction/engineering-services"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/construction/engineering-services"}
		case "education_system_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04, []string{"/market/kz/education/education-system"}
		case "retailer":
			if (step+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.06 + 0.01*float64(variant), targets
			}
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04 + 0.01*float64(step%2), targets
		case "large_enterprise_retail":
			switch (step + variant + idx) % 3 {
			case 0:
				return string(horizonmodels.SimulationActionAdjustBudget), 0.09 + 0.01*float64(step%2), targets
			case 1:
				return string(horizonmodels.SimulationActionSell), 0.06, []string{"/market/kz/consumer/retail"}
			default:
				return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/consumer/retail"}
			}
		case "large_enterprise_importer":
			switch (step + variant + idx) % 3 {
			case 0:
				return string(horizonmodels.SimulationActionAdjustBudget), 0.10, targets
			case 1:
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.07, []string{"/market/kz/industrial/import-distribution"}
			default:
				return string(horizonmodels.SimulationActionSell), 0.06, targets
			}
		case "large_enterprise_manufacturer":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.08, targets
			}
			return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/consumer/food"}
		case "enterprise_treasury":
			if step >= 2 {
				return string(horizonmodels.SimulationActionLobby), 0.06, []string{"/gov/kz/ministry-of-finance"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.08, []string{"/market/kz/finance/working-capital"}
		case "bank":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05 + 0.01*float64((variant+step)%3), []string{"/market/kz/finance/working-capital"}
		case "payment_rail":
			if variant == 2 || variant == 4 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/finance/working-capital"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04 + 0.01*float64(step%2), []string{"/market/kz/finance/working-capital"}
		case "logistics_operator":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/market/kz/industrial/import-distribution"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "warehouse_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "restaurant_operator":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04 + 0.01*float64(step%2), []string{"/market/kz/consumer/food"}
		case "tax_advisor":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/program/kz/vat-reform-2026"}
		case "municipal_buyer":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/loc/kz"}
		case "supplier", "wholesaler":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionSell), 0.04 + 0.01*float64(variant%2), targets
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "association":
			return string(horizonmodels.SimulationActionLobby), 0.06, []string{"/gov/kz/ministry-of-finance"}
		case "analyst":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/loc/kz"}
		case "agri_producer", "fuel_distributor":
			return string(horizonmodels.SimulationActionSell), 0.05 + 0.01*float64(variant%2), targets
		case "media":
			if variant == 1 || variant == 4 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
		case "cross_border_shopper":
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/market/kz/industrial/import-distribution"}
		case "consumer_urban_low_income", "consumer_rural_low_income":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04 + 0.01*float64(step%2), targets
		case "consumer_family_mid_income":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04, targets
		case "consumer_ecommerce_native":
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.04, []string{"/market/kz/consumer/retail"}
		case "consumer_bazaar_sensitive":
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/community/kz/trade/bazaar-traders"}
		case "consumer_affluent":
			return string(horizonmodels.SimulationActionBuy), 0.03, targets
		case "ecommerce_platform":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/consumer/retail"}
		case "opposition_politician":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
		case "consumer_low_income", "consumer_mid_income":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.03 + 0.01*float64(idx%2), targets
		case "consumer_high_income":
			return string(horizonmodels.SimulationActionBuy), 0.03, targets
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "healthcare_private_operator":
			return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/healthcare/healthcare-private"}
		case "healthcare_public_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04, []string{"/market/kz/healthcare/healthcare-public"}
		case "engineering_infrastructure_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04, []string{"/market/kz/construction/engineering-services"}
		case "education_system_operator":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.03, []string{"/market/kz/education/education-system"}
		case "internal_producer":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/industrial/internal-production"}
		case "consumer_urban_low_income":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.06, targets
			}
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/community/kz/trade/bazaar-traders", "/market/kz/consumer/retail"}
		case "consumer_rural_low_income":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.06, []string{"/community/kz/trade/bazaar-traders"}
			}
			return string(horizonmodels.SimulationActionDelayPurchase), 0.05, targets
		case "consumer_family_mid_income":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
			}
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04, targets
		case "consumer_ecommerce_native":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/market/kz/consumer/retail"}
			}
			return string(horizonmodels.SimulationActionBuy), 0.04, targets
		case "consumer_bazaar_sensitive":
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.06, []string{"/community/kz/trade/bazaar-traders", "/market/kz/consumer/retail"}
		case "consumer_affluent":
			return string(horizonmodels.SimulationActionBuy), 0.04, targets
		case "consumer_low_income":
			if variant == 3 || variant == 4 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.05 + 0.01*float64(step%2), targets
			}
			return string(horizonmodels.SimulationActionBuy), 0.03 + 0.01*float64(idx%2), targets
		case "consumer_mid_income":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.04, targets
			}
			return string(horizonmodels.SimulationActionBuy), 0.03, targets
		case "consumer_high_income":
			return string(horizonmodels.SimulationActionBuy), 0.04, targets
		case "small_trader":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionBuy), 0.05 + 0.01*float64(variant-2), []string{"/market/kz/consumer/retail"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/community/kz/trade/bazaar-traders"}
		case "marketplace_seller":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/consumer/retail"}
			}
			return string(horizonmodels.SimulationActionBuy), 0.04, []string{"/market/kz/consumer/retail"}
		case "ecommerce_platform":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionBroadcastMessage), 0.06, []string{"/market/kz/consumer/retail", "/community/kz/trade/bazaar-traders"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/consumer/retail"}
		case "retailer":
			if variant == 2 {
				return string(horizonmodels.SimulationActionSell), 0.03, targets
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "large_enterprise_retail":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/consumer/retail"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, targets
		case "restaurant_operator":
			if variant >= 4 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.05, []string{"/market/kz/consumer/food"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.04, targets
		case "importer":
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.04 + 0.01*float64(variant%2), targets
		case "large_enterprise_importer":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.07, []string{"/market/kz/industrial/import-distribution"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, targets
		case "large_enterprise_manufacturer":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, targets
		case "supplier", "wholesaler", "logistics_operator", "warehouse_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "agri_producer":
			return string(horizonmodels.SimulationActionSell), 0.05, []string{"/market/kz/consumer/food"}
		case "fuel_distributor":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/industrial/import-distribution"}
		case "cross_border_shopper":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.05, []string{"/market/kz/industrial/import-distribution"}
			}
			return string(horizonmodels.SimulationActionBuy), 0.04, targets
		case "influencer":
			if step >= 2 {
				return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "association":
			if step >= 3 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "analyst":
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "enterprise_treasury":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, []string{"/market/kz/finance/working-capital"}
		case "media", "opposition_politician":
			return string(horizonmodels.SimulationActionDoNothing), 0, targets
		case "municipal_buyer":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04, []string{"/loc/kz"}
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "budget_revenue_officer":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/public-sector/budget-revenue"}
		case "investment_climate_officer":
			if step >= 2 {
				return string(horizonmodels.SimulationActionChangeBelief), 0.05, []string{"/market/kz/finance/investment-climate", "/loc/kz"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/kz/finance/investment-climate"}
		case "healthcare_public_operator", "healthcare_private_operator", "engineering_infrastructure_operator", "education_system_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, targets
		case "internal_producer":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/market/kz/industrial/internal-production"}
		case "media":
			if step >= 2 && (variant == 1 || variant == 4) {
				return string(horizonmodels.SimulationActionEscalate), 0.08 + 0.01*float64(step%2), []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.06 + 0.01*float64(variant), []string{"/loc/kz"}
		case "association":
			if step >= 1 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.06, []string{"/gov/kz/ministry-of-finance"}
			}
			return string(horizonmodels.SimulationActionLobby), 0.05, []string{"/program/kz/vat-reform-2026"}
		case "analyst":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
		case "influencer":
			if variant == 1 || variant == 4 {
				return string(horizonmodels.SimulationActionEscalate), 0.07, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
		case "opposition_politician":
			return string(horizonmodels.SimulationActionEscalate), 0.08 + 0.01*float64(variant%2), []string{"/loc/kz"}
		case "tax_advisor":
			if step >= 2 {
				return string(horizonmodels.SimulationActionChangeBelief), 0.05, []string{"/loc/kz", "/program/kz/vat-reform-2026"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
		case "regulator":
			if variant == 4 && step >= 2 {
				return string(horizonmodels.SimulationActionEnforceRule), 0.05, []string{"/market/kz/consumer/retail"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, targets
		case "regional_official":
			if step >= 2 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/loc/kz"}
		case "retailer", "importer":
			if variant == 1 && step >= 3 {
				return string(horizonmodels.SimulationActionEscalate), 0.07, []string{"/loc/kz"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, targets
		case "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury":
			if step >= 2 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.06, []string{"/loc/kz", "/program/kz/vat-reform-2026"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
		case "supplier", "wholesaler", "payment_rail":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, targets
		case "logistics_operator", "warehouse_operator", "ecommerce_platform":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, targets
		case "restaurant_operator", "municipal_buyer":
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, targets
		case "agri_producer", "fuel_distributor":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, targets
		case "consumer_ecommerce_native", "consumer_bazaar_sensitive":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
		case "consumer_urban_low_income", "consumer_rural_low_income", "consumer_family_mid_income":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.04, []string{"/loc/kz"}
		case "small_trader":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/community/kz/trade/bazaar-traders"}
		}
	}

	switch cls {
	case "association":
		return string(horizonmodels.SimulationActionLobby), 0.05, []string{"/gov/kz/ministry-of-finance"}
	case "analyst":
		return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/loc/kz"}
	case "media":
		return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
	case "influencer":
		return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/loc/kz"}
	case "opposition_politician":
		return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/kz"}
	case "retailer", "importer", "large_enterprise_retail", "large_enterprise_importer", "large_enterprise_manufacturer", "enterprise_treasury":
		return string(horizonmodels.SimulationActionAdjustBudget), 0.04, targets
	case "supplier", "wholesaler", "payment_rail", "logistics_operator", "warehouse_operator", "restaurant_operator", "municipal_buyer", "agri_producer", "fuel_distributor":
		return string(horizonmodels.SimulationActionAdjustBudget), 0.04, targets
	case "tax_advisor":
		return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/program/kz/vat-reform-2026"}
	case "consumer_low_income", "consumer_mid_income", "consumer_urban_low_income", "consumer_rural_low_income", "consumer_family_mid_income":
		return string(horizonmodels.SimulationActionDelayPurchase), 0.03, targets
	case "consumer_high_income", "consumer_affluent":
		return string(horizonmodels.SimulationActionBuy), 0.03, targets
	case "cross_border_shopper":
		return string(horizonmodels.SimulationActionSwitchSupplier), 0.04, []string{"/market/kz/industrial/import-distribution"}
	case "small_trader", "marketplace_seller", "ecommerce_platform", "consumer_ecommerce_native", "consumer_bazaar_sensitive":
		return string(horizonmodels.SimulationActionBuy), 0.04, targets
	case "regional_official":
		return string(horizonmodels.SimulationActionIssueStatement), 0.04, []string{"/loc/kz"}
	default:
		return string(horizonmodels.SimulationActionDoNothing), 0, targets
	}
}
