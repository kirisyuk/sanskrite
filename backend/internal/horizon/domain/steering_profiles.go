package domain

import "strings"

type SteeringProfileName string

const (
	SteeringProfileGeneral                 SteeringProfileName = "general"
	SteeringProfileSupplyChain             SteeringProfileName = "supply_chain"
	SteeringProfileRegulatoryExposure      SteeringProfileName = "regulatory_exposure"
	SteeringProfileCompetitiveLandscape    SteeringProfileName = "competitive_landscape"
	SteeringProfileGeographicConcentration SteeringProfileName = "geographic_concentration"
	SteeringProfileInvestmentFinance       SteeringProfileName = "investment_finance"
	SteeringProfileTechnologyInnovation    SteeringProfileName = "technology_innovation"
	SteeringProfileEcosystemStakeholder    SteeringProfileName = "ecosystem_stakeholder"
	SteeringProfileScenarioTrends          SteeringProfileName = "scenario_trends"
	SteeringProfileSectorIndustry          SteeringProfileName = "sector_industry"
)

var profileByContractType = map[string]SteeringProfileName{
	// Supply chain / industry mapping
	"supply-chain":    SteeringProfileSupplyChain,
	"value-chain":     SteeringProfileSupplyChain,
	"logistics-flow":  SteeringProfileSupplyChain,
	"trade-flow":      SteeringProfileSupplyChain,
	"sourcing-map":    SteeringProfileSupplyChain,
	"procurement-map": SteeringProfileSupplyChain,

	// Risk / geopolitics / regulatory
	"risk-assessment":         SteeringProfileRegulatoryExposure,
	"geopolitical-risk":       SteeringProfileRegulatoryExposure,
	"bilateral-relations":     SteeringProfileRegulatoryExposure,
	"sanctions-exposure":      SteeringProfileRegulatoryExposure,
	"conflict-dynamics":       SteeringProfileRegulatoryExposure,
	"regulatory-impact":       SteeringProfileRegulatoryExposure,
	"policy-and-regulation":   SteeringProfileRegulatoryExposure,
	"compliance-landscape":    SteeringProfileRegulatoryExposure,
	"security-and-compliance": SteeringProfileRegulatoryExposure,
	"licensing-requirements":  SteeringProfileRegulatoryExposure,
	"esg-landscape":           SteeringProfileRegulatoryExposure,

	// Markets / opportunity / competitive
	"market-opportunity":       SteeringProfileCompetitiveLandscape,
	"competitive-landscape":    SteeringProfileCompetitiveLandscape,
	"market-entry":             SteeringProfileCompetitiveLandscape,
	"market-sizing":            SteeringProfileCompetitiveLandscape,
	"demand-forecast":          SteeringProfileCompetitiveLandscape,
	"pricing-dynamics":         SteeringProfileCompetitiveLandscape,
	"geographic-concentration": SteeringProfileGeographicConcentration,

	// Investment / finance
	"investment-thesis":  SteeringProfileInvestmentFinance,
	"funding-landscape":  SteeringProfileInvestmentFinance,
	"exit-strategy":      SteeringProfileInvestmentFinance,
	"valuation-drivers":  SteeringProfileInvestmentFinance,
	"portfolio-exposure": SteeringProfileInvestmentFinance,

	// Technology / innovation
	"technology-landscape": SteeringProfileTechnologyInnovation,
	"innovation-map":       SteeringProfileTechnologyInnovation,
	"patent-landscape":     SteeringProfileTechnologyInnovation,
	"rd-landscape":         SteeringProfileTechnologyInnovation,
	"adoption-curve":       SteeringProfileTechnologyInnovation,
	"disruption-analysis":  SteeringProfileTechnologyInnovation,

	// Ecosystem / stakeholder / network
	"ecosystem-mapping":     SteeringProfileEcosystemStakeholder,
	"stakeholder-mapping":   SteeringProfileEcosystemStakeholder,
	"partnership-landscape": SteeringProfileEcosystemStakeholder,
	"influence-network":     SteeringProfileEcosystemStakeholder,
	"power-dynamics":        SteeringProfileEcosystemStakeholder,

	// Scenarios / trends / forecasting
	"scenario-analysis": SteeringProfileScenarioTrends,
	"macro-trends":      SteeringProfileScenarioTrends,
	"megatrends":        SteeringProfileScenarioTrends,
	"future-outlook":    SteeringProfileScenarioTrends,

	// Sector / industry
	"sector-overview":    SteeringProfileSectorIndustry,
	"industry-structure": SteeringProfileSectorIndustry,
	"vertical-deep-dive": SteeringProfileSectorIndustry,
}

func steeringProfileForContractType(contractType string) SteeringProfileName {
	ct := strings.TrimSpace(strings.ToLower(contractType))
	if ct == "" {
		return SteeringProfileGeneral
	}
	if p, ok := profileByContractType[ct]; ok {
		return p
	}
	return SteeringProfileGeneral
}
