package domain

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

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

	// Investment / finance / ownership / governance
	"investment-thesis":   SteeringProfileInvestmentFinance,
	"funding-landscape":   SteeringProfileInvestmentFinance,
	"exit-strategy":       SteeringProfileInvestmentFinance,
	"valuation-drivers":   SteeringProfileInvestmentFinance,
	"portfolio-exposure":  SteeringProfileInvestmentFinance,
	"ownership-structure": SteeringProfileInvestmentFinance,
	"equity":              SteeringProfileInvestmentFinance,

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

// SteeringProfileForContractType returns the steering profile for a contract type (exported for callers outside this package).
func SteeringProfileForContractType(contractType string) SteeringProfileName {
	return steeringProfileForContractType(contractType)
}

// profileToDomainMode maps steering profile to domain mode (supply_chain→supply_chain, regulatory_exposure→strategic, etc.).
var profileToDomainMode = map[SteeringProfileName]string{
	SteeringProfileGeneral:                 taxonomy.DomainModeGeneral,
	SteeringProfileSupplyChain:             taxonomy.DomainModeSupplyChain,
	SteeringProfileRegulatoryExposure:      taxonomy.DomainModeStrategic,
	SteeringProfileCompetitiveLandscape:    taxonomy.DomainModeMarket,
	SteeringProfileGeographicConcentration: taxonomy.DomainModeMarket,
	SteeringProfileInvestmentFinance:       taxonomy.DomainModeGovernance,
	SteeringProfileTechnologyInnovation:    taxonomy.DomainModeGeneral,
	SteeringProfileEcosystemStakeholder:    taxonomy.DomainModeGeneral,
	SteeringProfileScenarioTrends:          taxonomy.DomainModeStrategic,
	SteeringProfileSectorIndustry:          taxonomy.DomainModeSupplyChain,
}

// DomainModeForSteeringProfile returns the domain mode for a steering profile name.
func DomainModeForSteeringProfile(profile SteeringProfileName) string {
	if mode, ok := profileToDomainMode[profile]; ok {
		return mode
	}
	return taxonomy.DomainModeGeneral
}

// contractTypesWithMultipleModes maps contract types that imply multiple domain modes.
// partnership-landscape needs both supply chain (SUPPLIES_TO, PARTNERS_WITH) and governance (OWNS, PARTNERS_WITH).
func contractTypesWithMultipleModes(ct string) []string {
	switch ct {
	case "partnership-landscape":
		return []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance}
	default:
		return nil
	}
}

// DomainModesForContractTypes returns deduplicated domain modes for the given contract types.
// Uses steeringProfileForContractType and DomainModeForSteeringProfile; order preserved.
// Special case: partnership-landscape yields both supply_chain and governance modes.
func DomainModesForContractTypes(contractTypes []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(contractTypes)*2)
	for _, ct := range contractTypes {
		ct = strings.TrimSpace(strings.ToLower(ct))
		if ct == "" {
			continue
		}
		if modes := contractTypesWithMultipleModes(ct); len(modes) > 0 {
			for _, m := range modes {
				if _, ok := seen[m]; !ok {
					seen[m] = struct{}{}
					out = append(out, m)
				}
			}
			continue
		}
		profile := steeringProfileForContractType(ct)
		mode := DomainModeForSteeringProfile(profile)
		if _, ok := seen[mode]; !ok {
			seen[mode] = struct{}{}
			out = append(out, mode)
		}
	}
	return taxonomy.NormalizeDomainModes(out)
}
