package kzvat

import (
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	ScenarioID    = "kz-vat-16pct-2026"
	ScenarioTitle = "Kazakhstan VAT 16% regulatory hot path"
)

type FragmentSeed struct {
	CanonicalPath  string
	Type           FragmentType
	DisplayName    string
	Description    string
	EntityPath     string
	Labels         map[string]string
	Tags           []string
	GovType        string
	ParentGovPath  string
	ParentPath     string
	MarketDomain   string
	MarketVertical string
	MarketChannels []string
	CommunityType  string
	Qualifiers     map[string]interface{}
	State          map[string]horizonmodels.MetricValue
}

type EdgeSeed struct {
	SourcePath    string
	TargetPath    string
	Type          DependencyType
	Criticality   float64
	Amplification float64
	LatencyHours  int
	Reason        string
	Qualifiers    map[string]interface{}
	Metrics       map[string]horizonmodels.MetricValue
}

type SimulationSpec struct {
	ID      string
	Title   string
	Request horizonmodels.SimulationRunRequest
}

type HotPathSpec struct {
	ID           string
	Title        string
	Question     string
	LocationPath string
	Fragments    []FragmentSeed
	Edges        []EdgeSeed
	Simulations  []SimulationSpec
}

func Spec(projectID string) HotPathSpec {
	seedPaths := SeedPaths()
	question := "Повышение НДС до 16% в Казахстане. Какие сценарии наиболее вероятны, кто и как отреагирует, и через какие механизмы эффект распространится по рынкам и институтам?"

	return HotPathSpec{
		ID:           ScenarioID,
		Title:        ScenarioTitle,
		Question:     question,
		LocationPath: "/loc/kz",
		Fragments: []FragmentSeed{
			{
				CanonicalPath: "/loc/kz",
				Type:          FragmentTypeGeoRegion,
				DisplayName:   "Kazakhstan",
				Description:   "Country anchor for Kazakhstan policy-impact runs and macro calibration assumptions for VAT scenarios.",
				EntityPath:    "/Geo/Country",
				Labels: map[string]string{
					"en": "Kazakhstan",
					"ru": "Казахстан",
				},
				Tags: []string{"kazakhstan", "policy-impact", "vat", "macro-anchor"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"scenarioKind":    "policy_impact",
				},
				State: map[string]horizonmodels.MetricValue{
					"/metric/policy/vat_rate_pct":                 metric(12.0, "percent"),
					"/metric/policy/baseline_gdp_bln_kzt":         metric(140000.0, "bln_kzt"),
					"/metric/policy/baseline_vat_revenue_bln_kzt": metric(4200.0, "bln_kzt"),
				},
			},
			{
				CanonicalPath: "/gov/kz/ministry-of-finance",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Ministry of Finance of Kazakhstan",
				Description:   "Primary policy authority for Kazakhstan VAT design, budget framing, and tax reform steering.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels: map[string]string{
					"en": "Ministry of Finance of Kazakhstan",
					"ru": "Министерство финансов Республики Казахстан",
				},
				Tags:    []string{"kazakhstan", "government", "tax", "vat", "policy-impact"},
				GovType: "ministry",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/gov/kz/state-revenue-committee",
				Type:          FragmentTypeOrganization,
				DisplayName:   "State Revenue Committee of Kazakhstan",
				Description:   "Tax administration and enforcement body executing VAT collection, compliance, and audit posture.",
				EntityPath:    "/Organization/Government/Agency",
				Labels: map[string]string{
					"en": "State Revenue Committee of Kazakhstan",
					"ru": "Комитет государственных доходов Республики Казахстан",
				},
				Tags:          []string{"kazakhstan", "government", "tax-administration", "vat", "policy-impact"},
				GovType:       "committee",
				ParentGovPath: "/gov/kz/ministry-of-finance",
				ParentPath:    "/gov/kz/ministry-of-finance",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/gov/kz/ministry-of-national-economy",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Ministry of National Economy of Kazakhstan",
				Description:   "Macro-policy and investment climate authority responsible for business environment, economic predictability, and investment posture.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels: map[string]string{
					"en": "Ministry of National Economy of Kazakhstan",
					"ru": "Министерство национальной экономики Республики Казахстан",
				},
				Tags:    []string{"kazakhstan", "government", "macro-policy", "investment-climate", "policy-impact"},
				GovType: "ministry",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/gov/kz/ministry-of-health",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Ministry of Health of Kazakhstan",
				Description:   "Public health authority responsible for healthcare budgets, procurement continuity, and service resilience.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels: map[string]string{
					"en": "Ministry of Health of Kazakhstan",
					"ru": "Министерство здравоохранения Республики Казахстан",
				},
				Tags:    []string{"kazakhstan", "government", "healthcare", "public-services", "policy-impact"},
				GovType: "ministry",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/gov/kz/ministry-of-industry-and-construction",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Ministry of Industry and Construction of Kazakhstan",
				Description:   "Industrial and infrastructure authority linked to domestic production, construction, and engineering services capacity.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels: map[string]string{
					"en": "Ministry of Industry and Construction of Kazakhstan",
					"ru": "Министерство промышленности и строительства Республики Казахстан",
				},
				Tags:    []string{"kazakhstan", "government", "industry", "infrastructure", "policy-impact"},
				GovType: "ministry",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/gov/kz/ministry-of-education",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Education authority of Kazakhstan",
				Description:   "Public education authority responsible for education budget continuity, procurement, and service resilience.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels: map[string]string{
					"en": "Education authority of Kazakhstan",
					"ru": "Органы управления образованием Казахстана",
				},
				Tags:    []string{"kazakhstan", "government", "education", "public-services", "policy-impact"},
				GovType: "ministry",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulator",
				},
			},
			{
				CanonicalPath: "/decision/taxation/raise-vat-to-16pct-kazakhstan-2026",
				Type:          FragmentTypeDecisionPoint,
				DisplayName:   "Raise VAT to 16% in Kazakhstan",
				Description:   "Decision point for the 2026 Kazakhstan VAT increase scenario used as the policy anchor in the hot path.",
				EntityPath:    "/Decision/Taxation/RateChange",
				Labels: map[string]string{
					"en": "Raise VAT to 16% in Kazakhstan",
					"ru": "Повышение НДС до 16% в Казахстане",
				},
				Tags: []string{"kazakhstan", "vat", "decision", "policy-impact"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"jurisdiction":    "kz",
					"rate_from_pct":   12,
					"rate_to_pct":     16,
					"effective_date":  "2026-01-01",
				},
			},
			{
				CanonicalPath: "/mechanism/regulatory/vat-rate-increase",
				Type:          FragmentTypeMechanism,
				DisplayName:   "VAT rate increase",
				Description:   "Canonical regulatory mechanism representing an upward VAT rate change and its tax incidence channel.",
				EntityPath:    "/Mechanism/Regulatory/Tax",
				Labels: map[string]string{
					"en": "VAT rate increase",
					"ru": "Повышение ставки НДС",
				},
				Tags: []string{"mechanism", "regulatory", "vat", "tax"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"jurisdiction":    "kz",
				},
			},
			{
				CanonicalPath: "/program/kz/vat-reform-2026",
				Type:          FragmentTypeProgram,
				DisplayName:   "Kazakhstan VAT reform 2026",
				Description:   "Administrative execution program for VAT transition, taxpayer communication, and compliance rollout in Kazakhstan.",
				EntityPath:    "/Program/Fiscal/TaxAdministration",
				Labels: map[string]string{
					"en": "Kazakhstan VAT reform 2026",
					"ru": "Реформа НДС Казахстан 2026",
				},
				Tags: []string{"kazakhstan", "vat", "program", "tax-administration", "policy-impact"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/program/kz/investment-climate-stability",
				Type:          FragmentTypeProgram,
				DisplayName:   "Kazakhstan investment climate stability program",
				Description:   "Programmatic response to preserve investor confidence, domestic expansion, and macro-policy predictability during the VAT transition.",
				EntityPath:    "/Program/Economic/InvestmentClimate",
				Labels: map[string]string{
					"en": "Kazakhstan investment climate stability program",
					"ru": "Программа устойчивости инвестиционного климата Казахстана",
				},
				Tags: []string{"kazakhstan", "program", "investment-climate", "macro-policy", "policy-impact"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/program/kz/domestic-production-continuity",
				Type:          FragmentTypeProgram,
				DisplayName:   "Kazakhstan domestic production continuity program",
				Description:   "Programmatic support for internal production continuity, domestic value added, and GDP resilience under tax transition pressure.",
				EntityPath:    "/Program/Industrial/ProductionContinuity",
				Labels: map[string]string{
					"en": "Kazakhstan domestic production continuity program",
					"ru": "Программа устойчивости внутреннего производства Казахстана",
				},
				Tags: []string{"kazakhstan", "program", "internal-production", "gdp", "policy-impact"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/program/kz/public-services-resilience",
				Type:          FragmentTypeProgram,
				DisplayName:   "Kazakhstan public services resilience program",
				Description:   "Program for maintaining healthcare, education, and engineering service continuity during fiscal and procurement pressure.",
				EntityPath:    "/Program/PublicServices/Resilience",
				Labels: map[string]string{
					"en": "Kazakhstan public services resilience program",
					"ru": "Программа устойчивости государственных сервисов Казахстана",
				},
				Tags: []string{"kazakhstan", "program", "healthcare", "education", "infrastructure", "policy-impact"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/consumer/food",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan food market",
				Description:   "Consumer food market exposure bucket for VAT pass-through and demand elasticity in Kazakhstan.",
				EntityPath:    "/Market/Consumer/Food",
				Labels: map[string]string{
					"en": "Kazakhstan food market",
					"ru": "Рынок продовольствия Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "consumer", "food", "policy-impact"},
				MarketDomain:   "consumer",
				MarketVertical: "food",
				MarketChannels: []string{"b2c"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/consumer/retail",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan retail market",
				Description:   "Retail market exposure bucket for price pass-through, working-capital pressure, and consumer demand adaptation.",
				EntityPath:    "/Market/Consumer/Retail",
				Labels: map[string]string{
					"en": "Kazakhstan retail market",
					"ru": "Розничный рынок Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "retail", "consumer", "policy-impact"},
				MarketDomain:   "consumer",
				MarketVertical: "retail",
				MarketChannels: []string{"b2c"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/industrial/import-distribution",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan import distribution market",
				Description:   "Import-heavy distribution market used to model VAT settlement pressure, inventory financing, and cost pass-through.",
				EntityPath:    "/Market/Industrial/ImportDistribution",
				Labels: map[string]string{
					"en": "Kazakhstan import distribution market",
					"ru": "Рынок импортной дистрибуции Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "imports", "distribution", "policy-impact"},
				MarketDomain:   "industrial",
				MarketVertical: "import-distribution",
				MarketChannels: []string{"b2b"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "import-dependent",
				},
			},
			{
				CanonicalPath: "/market/kz/finance/working-capital",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan working capital market",
				Description:   "Financing and settlement pressure bucket for VAT credit timing, liquidity, and short-term working-capital adaptation.",
				EntityPath:    "/Market/Finance/WorkingCapital",
				Labels: map[string]string{
					"en": "Kazakhstan working capital market",
					"ru": "Рынок оборотного капитала Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "finance", "working-capital", "policy-impact"},
				MarketDomain:   "finance",
				MarketVertical: "working-capital",
				MarketChannels: []string{"b2b"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/public-sector/budget-revenue",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan budget revenue base",
				Description:   "Public revenue base capturing VAT collection, budget inflow sensitivity, and fiscal headroom under the tax transition.",
				EntityPath:    "/Market/Public/BudgetRevenue",
				Labels: map[string]string{
					"en": "Kazakhstan budget revenue base",
					"ru": "База бюджетных доходов Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "public-finance", "budget-revenue", "policy-impact"},
				MarketDomain:   "public-finance",
				MarketVertical: "budget-revenue",
				MarketChannels: []string{"state"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
				State: map[string]horizonmodels.MetricValue{
					"/metric/fiscal/vat_revenue_share_pct": metric(18.0, "percent"),
				},
			},
			{
				CanonicalPath: "/market/kz/finance/investment-climate",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan investment climate",
				Description:   "Domestic and foreign investor confidence channel exposed to tax predictability, compliance friction, and transition credibility.",
				EntityPath:    "/Market/Capital/InvestmentClimate",
				Labels: map[string]string{
					"en": "Kazakhstan investment climate",
					"ru": "Инвестиционный климат Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "capital", "investment-climate", "policy-impact"},
				MarketDomain:   "capital",
				MarketVertical: "investment-climate",
				MarketChannels: []string{"capital"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/industrial/internal-production",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan internal production base",
				Description:   "Internal production channel used as a proxy for domestic value added, manufacturing continuity, and GDP sensitivity.",
				EntityPath:    "/Market/Industrial/InternalProduction",
				Labels: map[string]string{
					"en": "Kazakhstan internal production base",
					"ru": "Внутреннее производство Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "industrial", "internal-production", "gdp", "policy-impact"},
				MarketDomain:   "industrial",
				MarketVertical: "internal-production",
				MarketChannels: []string{"b2b"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
				State: map[string]horizonmodels.MetricValue{
					"/metric/macro/internal_value_added_share_pct": metric(61.0, "percent"),
				},
			},
			{
				CanonicalPath: "/market/kz/healthcare/healthcare-public",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan public healthcare system",
				Description:   "Public healthcare segment exposed to procurement cost shifts, fiscal pressure, and medical goods pass-through.",
				EntityPath:    "/Market/Public/HealthcarePublic",
				Labels: map[string]string{
					"en": "Kazakhstan public healthcare system",
					"ru": "Государственное здравоохранение Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "healthcare", "public-services", "policy-impact"},
				MarketDomain:   "public-services",
				MarketVertical: "healthcare-public",
				MarketChannels: []string{"state"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/healthcare/healthcare-private",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan private healthcare sector",
				Description:   "Private healthcare and pharmacy segment exposed to price pass-through, imports, and affordability shifts.",
				EntityPath:    "/Market/Private/HealthcarePrivate",
				Labels: map[string]string{
					"en": "Kazakhstan private healthcare sector",
					"ru": "Частный сектор здравоохранения Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "healthcare", "private-services", "policy-impact"},
				MarketDomain:   "private-services",
				MarketVertical: "healthcare-private",
				MarketChannels: []string{"b2c"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/construction/engineering-services",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan engineering infrastructure services",
				Description:   "Engineering and municipal infrastructure operations exposed to procurement costs, imported components, and fiscal capex discipline.",
				EntityPath:    "/Market/Infrastructure/EngineeringServices",
				Labels: map[string]string{
					"en": "Kazakhstan engineering infrastructure services",
					"ru": "Инженерная инфраструктура Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "infrastructure", "engineering", "policy-impact"},
				MarketDomain:   "infrastructure",
				MarketVertical: "engineering-services",
				MarketChannels: []string{"state", "b2b"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/market/kz/education/education-system",
				Type:          FragmentTypeMarket,
				DisplayName:   "Kazakhstan education system",
				Description:   "Education services segment used to model budgetary trade-offs, procurement inflation, and long-term human capital sensitivity.",
				EntityPath:    "/Market/Public/EducationSystem",
				Labels: map[string]string{
					"en": "Kazakhstan education system",
					"ru": "Система образования Казахстана",
				},
				Tags:           []string{"kazakhstan", "market", "education", "public-services", "policy-impact"},
				MarketDomain:   "public-services",
				MarketVertical: "education-system",
				MarketChannels: []string{"state"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"objectQualifier": "regulated",
				},
			},
			{
				CanonicalPath: "/community/kz/trade/bazaar-traders",
				Type:          FragmentTypeCommunity,
				DisplayName:   "Kazakhstan bazaar traders",
				Description:   "Informal and semi-formal small-trader community used to model evasion, cash migration, and channel substitution.",
				EntityPath:    "/Community/Trade/InformalRetail",
				Labels: map[string]string{
					"en": "Kazakhstan bazaar traders",
					"ru": "Базарные торговцы Казахстана",
				},
				Tags:          []string{"kazakhstan", "community", "trade", "sme", "informal-sector", "policy-impact"},
				CommunityType: "trade",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
		},
		Edges: []EdgeSeed{
			{
				SourcePath:    "/gov/kz/ministry-of-finance",
				TargetPath:    "/gov/kz/state-revenue-committee",
				Type:          DependencyTypeAdministers,
				Criticality:   0.92,
				Amplification: 1.05,
				Reason:        "Administrative authority chain for VAT policy execution",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-national-economy",
				TargetPath:    "/program/kz/investment-climate-stability",
				Type:          DependencyTypeAdministers,
				Criticality:   0.82,
				Amplification: 1.03,
				Reason:        "National economy ministry owns the investment climate stabilization response.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-industry-and-construction",
				TargetPath:    "/program/kz/domestic-production-continuity",
				Type:          DependencyTypeAdministers,
				Criticality:   0.80,
				Amplification: 1.03,
				Reason:        "Industry ministry owns domestic production continuity and engineering service resilience.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-health",
				TargetPath:    "/program/kz/public-services-resilience",
				Type:          DependencyTypeAdministers,
				Criticality:   0.79,
				Amplification: 1.02,
				Reason:        "Health ministry co-administers the public services resilience response.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-education",
				TargetPath:    "/program/kz/public-services-resilience",
				Type:          DependencyTypeAdministers,
				Criticality:   0.77,
				Amplification: 1.02,
				Reason:        "Education authority co-administers public services continuity under fiscal pressure.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-finance",
				TargetPath:    "/decision/taxation/raise-vat-to-16pct-kazakhstan-2026",
				Type:          DependencyTypeGatekeepsDecision,
				Criticality:   0.96,
				Amplification: 1.08,
				Reason:        "Finance ministry gates the VAT rate decision",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"rate_from_pct":   12,
					"rate_to_pct":     16,
					"effective_date":  "2026-01-01",
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/ministry-of-finance",
				TargetPath:    "/program/kz/vat-reform-2026",
				Type:          DependencyTypeAdministers,
				Criticality:   0.90,
				Amplification: 1.03,
				Reason:        "VAT transition program is managed through finance administration",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/state-revenue-committee",
				TargetPath:    "/program/kz/vat-reform-2026",
				Type:          DependencyTypeAdministers,
				Criticality:   0.88,
				Amplification: 1.04,
				Reason:        "State Revenue Committee executes compliance and taxpayer rollout",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/decision/taxation/raise-vat-to-16pct-kazakhstan-2026",
				TargetPath:    "/mechanism/regulatory/vat-rate-increase",
				Type:          DependencyTypeUsesMechanism,
				Criticality:   0.95,
				Amplification: 1.10,
				Reason:        "Decision materializes through a VAT rate increase mechanism",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"rate_from_pct":   12,
					"rate_to_pct":     16,
					"effective_date":  "2026-01-01",
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/mechanism/regulatory/vat-rate-increase",
				Type:          DependencyTypeUsesMechanism,
				Criticality:   0.83,
				Amplification: 1.02,
				Reason:        "Administrative program executes the rate change mechanism",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/program/kz/investment-climate-stability",
				TargetPath:    "/market/kz/finance/investment-climate",
				Type:          DependencyTypeInfluences,
				Criticality:   0.78,
				Amplification: 1.05,
				Reason:        "Investment climate program shapes predictability and business confidence under tax change.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "expectations-management",
				},
			},
			{
				SourcePath:    "/program/kz/domestic-production-continuity",
				TargetPath:    "/market/kz/industrial/internal-production",
				Type:          DependencyTypeInfluences,
				Criticality:   0.80,
				Amplification: 1.06,
				Reason:        "Production continuity program influences domestic output and GDP resilience.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "production-support",
				},
			},
			{
				SourcePath:    "/program/kz/public-services-resilience",
				TargetPath:    "/market/kz/healthcare/healthcare-public",
				Type:          DependencyTypeInfluences,
				Criticality:   0.74,
				Amplification: 1.04,
				Reason:        "Public services resilience program supports public healthcare continuity.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "service-continuity",
				},
			},
			{
				SourcePath:    "/program/kz/public-services-resilience",
				TargetPath:    "/market/kz/education/education-system",
				Type:          DependencyTypeInfluences,
				Criticality:   0.72,
				Amplification: 1.04,
				Reason:        "Public services resilience program supports education continuity.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "service-continuity",
				},
			},
			{
				SourcePath:    "/program/kz/public-services-resilience",
				TargetPath:    "/market/kz/construction/engineering-services",
				Type:          DependencyTypeInfluences,
				Criticality:   0.75,
				Amplification: 1.05,
				Reason:        "Public services resilience program supports engineering service reliability and maintenance continuity.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "service-continuity",
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.86,
				Amplification: 1.06,
				Reason:        "National scope of the VAT increase",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"jurisdiction":    "kz",
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/consumer/food",
				Type:          DependencyTypeImposesCondition,
				Criticality:   0.84,
				Amplification: 1.07,
				Reason:        "Food retail faces partial pass-through and consumer price sensitivity",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"pass_through_estimate":  0.60,
					"tax_base_coverage":      "partial",
					"sector_sensitivity":     1.10,
					"elasticity_hint":        -0.45,
					"observability":          "proxy",
					"measurement_lag_months": 1,
					"measurement_confidence": 0.72,
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/consumer/retail",
				Type:          DependencyTypeImposesCondition,
				Criticality:   0.90,
				Amplification: 1.10,
				Reason:        "Retail sector carries higher pass-through and pricing pressure",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"pass_through_estimate":  0.85,
					"tax_base_coverage":      "full",
					"sector_sensitivity":     1.18,
					"elasticity_hint":        -0.35,
					"observability":          "direct",
					"measurement_lag_months": 1,
					"measurement_confidence": 0.80,
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/market/kz/industrial/import-distribution",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.88,
				Amplification: 1.12,
				Reason:        "Import distribution takes direct settlement and inventory-cost pressure",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"pass_through_estimate":  0.78,
					"tax_base_coverage":      "full",
					"sector_sensitivity":     1.22,
					"channel_type":           "settlement",
					"observability":          "proxy",
					"measurement_lag_months": 1,
					"measurement_confidence": 0.74,
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/market/kz/finance/working-capital",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.82,
				Amplification: 1.09,
				Reason:        "Liquidity and VAT credit timing affect working-capital demand",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"pass_through_estimate":  0.72,
					"sector_sensitivity":     1.08,
					"channel_type":           "settlement",
					"observability":          "proxy",
					"measurement_lag_months": 2,
					"measurement_confidence": 0.66,
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/market/kz/public-sector/budget-revenue",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.88,
				Amplification: 1.10,
				Reason:        "VAT rate increase directly changes budget revenue expectations and fiscal intake volatility.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "fiscal-intake",
					"measurement_lag_months": 1,
					"measurement_confidence": 0.79,
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/finance/investment-climate",
				Type:          DependencyTypeInfluences,
				Criticality:   0.81,
				Amplification: 1.07,
				Reason:        "Transition credibility and compliance burden influence investor confidence and tax predictability perception.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "expectations",
					"measurement_lag_months": 2,
					"measurement_confidence": 0.67,
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/market/kz/industrial/internal-production",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.82,
				Amplification: 1.08,
				Reason:        "Domestic production absorbs part of the tax pass-through through input costs, pricing, and output adjustments.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "production",
					"measurement_lag_months": 2,
					"measurement_confidence": 0.71,
				},
			},
			{
				SourcePath:    "/market/kz/industrial/internal-production",
				TargetPath:    "/market/kz/public-sector/budget-revenue",
				Type:          DependencyTypeInfluences,
				Criticality:   0.78,
				Amplification: 1.05,
				Reason:        "Internal production affects taxable value added and the broader fiscal base.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "gdp-to-revenue",
				},
			},
			{
				SourcePath:    "/market/kz/industrial/internal-production",
				TargetPath:    "/market/kz/finance/investment-climate",
				Type:          DependencyTypeInfluences,
				Criticality:   0.74,
				Amplification: 1.04,
				Reason:        "Domestic production continuity shapes investor sentiment around capacity, resilience, and returns.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "confidence",
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/healthcare/healthcare-public",
				Type:          DependencyTypeImposesCondition,
				Criticality:   0.77,
				Amplification: 1.05,
				Reason:        "Public healthcare procurement and medicines budgets face cost pressure under the new VAT regime.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "public-procurement",
					"measurement_lag_months": 2,
					"measurement_confidence": 0.68,
				},
			},
			{
				SourcePath:    "/mechanism/regulatory/vat-rate-increase",
				TargetPath:    "/market/kz/healthcare/healthcare-private",
				Type:          DependencyTypeAppliesTo,
				Criticality:   0.75,
				Amplification: 1.06,
				Reason:        "Private healthcare and pharmacies face direct pass-through from imported equipment, supplies, and services.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "consumer-price",
					"measurement_lag_months": 1,
					"measurement_confidence": 0.70,
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/construction/engineering-services",
				Type:          DependencyTypeImposesCondition,
				Criticality:   0.79,
				Amplification: 1.07,
				Reason:        "Infrastructure operators face procurement inflation and capex reprioritization under higher VAT.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "capex",
					"measurement_lag_months": 2,
					"measurement_confidence": 0.69,
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/market/kz/education/education-system",
				Type:          DependencyTypeImposesCondition,
				Criticality:   0.73,
				Amplification: 1.04,
				Reason:        "Education budgets and service delivery experience procurement and wage-tradeoff pressure during the transition.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "budget-tradeoff",
					"measurement_lag_months": 3,
					"measurement_confidence": 0.63,
				},
			},
			{
				SourcePath:    "/market/kz/public-sector/budget-revenue",
				TargetPath:    "/market/kz/healthcare/healthcare-public",
				Type:          DependencyTypeInfluences,
				Criticality:   0.72,
				Amplification: 1.03,
				Reason:        "Budget revenue conditions shape public healthcare fiscal room and procurement capacity.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "fiscal-allocation",
				},
			},
			{
				SourcePath:    "/market/kz/public-sector/budget-revenue",
				TargetPath:    "/market/kz/education/education-system",
				Type:          DependencyTypeInfluences,
				Criticality:   0.70,
				Amplification: 1.03,
				Reason:        "Fiscal intake conditions affect education budget headroom and service continuity.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "fiscal-allocation",
				},
			},
			{
				SourcePath:    "/market/kz/public-sector/budget-revenue",
				TargetPath:    "/market/kz/construction/engineering-services",
				Type:          DependencyTypeInfluences,
				Criticality:   0.74,
				Amplification: 1.04,
				Reason:        "Revenue headroom influences engineering maintenance, infrastructure upgrades, and service reliability.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "fiscal-allocation",
				},
			},
			{
				SourcePath:    "/program/kz/vat-reform-2026",
				TargetPath:    "/community/kz/trade/bazaar-traders",
				Type:          DependencyTypeInfluences,
				Criticality:   0.79,
				Amplification: 1.06,
				LatencyHours:  24 * 45,
				Reason:        "Informal traders adapt through evasion, assortment shifts, and cash migration",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath":        ScenarioID,
					"channel_type":           "evasion",
					"lag_quarters":           2,
					"sector_sensitivity":     1.15,
					"observability":          "modeled",
					"measurement_lag_months": 3,
					"measurement_confidence": 0.58,
				},
			},
			{
				SourcePath:    "/gov/kz/state-revenue-committee",
				TargetPath:    "/market/kz/consumer/retail",
				Type:          DependencyTypeRegulates,
				Criticality:   0.86,
				Amplification: 1.05,
				Reason:        "Retail is directly regulated through VAT administration and enforcement",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/state-revenue-committee",
				TargetPath:    "/market/kz/industrial/import-distribution",
				Type:          DependencyTypeRegulates,
				Criticality:   0.84,
				Amplification: 1.04,
				Reason:        "Importers face VAT settlement, customs, and compliance controls",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "direct",
				},
			},
			{
				SourcePath:    "/gov/kz/state-revenue-committee",
				TargetPath:    "/community/kz/trade/bazaar-traders",
				Type:          DependencyTypeRegulates,
				Criticality:   0.72,
				Amplification: 0.96,
				Reason:        "Small traders are affected through audits, registration, and reporting friction",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"observability":   "proxy",
				},
			},
			{
				SourcePath:    "/community/kz/trade/bazaar-traders",
				TargetPath:    "/market/kz/consumer/retail",
				Type:          DependencyTypeParticipatesIn,
				Criticality:   0.68,
				Amplification: 0.97,
				Reason:        "Informal traders compete in retail channels and absorb part of demand migration",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"channel_type":    "substitution",
				},
			},
			{
				SourcePath:    "/market/kz/consumer/food",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.74,
				Amplification: 1.00,
				Reason:        "Food market operates in Kazakhstan national geo scope",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/consumer/retail",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.74,
				Amplification: 1.00,
				Reason:        "Retail market operates in Kazakhstan national geo scope",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/industrial/import-distribution",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.74,
				Amplification: 1.00,
				Reason:        "Import distribution market operates in Kazakhstan national geo scope",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/finance/working-capital",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.70,
				Amplification: 1.00,
				Reason:        "Working-capital financing pressure is national in scope",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/public-sector/budget-revenue",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.70,
				Amplification: 1.00,
				Reason:        "Budget revenue base is national in scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/finance/investment-climate",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.68,
				Amplification: 1.00,
				Reason:        "Investment climate is evaluated at the national Kazakhstan scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/industrial/internal-production",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.72,
				Amplification: 1.00,
				Reason:        "Internal production base operates in the Kazakhstan national economy.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/healthcare/healthcare-public",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.66,
				Amplification: 1.00,
				Reason:        "Public healthcare system is national in scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/healthcare/healthcare-private",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.64,
				Amplification: 1.00,
				Reason:        "Private healthcare segment is national in scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/construction/engineering-services",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.67,
				Amplification: 1.00,
				Reason:        "Engineering infrastructure services are national in scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/market/kz/education/education-system",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.65,
				Amplification: 1.00,
				Reason:        "Education system is national in scope.",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
			{
				SourcePath:    "/community/kz/trade/bazaar-traders",
				TargetPath:    "/loc/kz",
				Type:          DependencyTypeOperatesIn,
				Criticality:   0.66,
				Amplification: 1.00,
				Reason:        "Bazaar trader community is Kazakhstan-scoped",
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
				},
			},
		},
		Simulations: []SimulationSpec{
			{
				ID:    "legal-compliance",
				Title: "Compliance and institutional response",
				Request: simulationRequest(projectID, question, seedPaths, &horizonmodels.SimulationShock{
					Text:         "Повышение НДС до 16% в Казахстане: регуляторное исполнение, налоговое администрирование и ответ бизнеса.",
					Type:         "regulatory",
					Axis:         horizonmodels.AxisLegalCompliance,
					Polarity:     "risk",
					Magnitude:    0.60,
					LocationPath: "/loc/kz",
					ActorPaths: []string{
						"/gov/kz/ministry-of-finance",
						"/gov/kz/state-revenue-committee",
					},
					SeedPaths: seedPaths,
				}),
			},
			{
				ID:    "financial-settlement",
				Title: "Cashflow and settlement response",
				Request: simulationRequest(projectID, question, seedPaths, &horizonmodels.SimulationShock{
					Text:         "Повышение НДС до 16% в Казахстане: влияние на цены закупки, расчёты, оборотный капитал и импортёров.",
					Type:         "price",
					Axis:         horizonmodels.AxisFinancialSettlement,
					Polarity:     "risk",
					Magnitude:    0.56,
					LocationPath: "/loc/kz",
					SeedPaths:    seedPaths,
				}),
			},
			{
				ID:    "demand-shift",
				Title: "Demand and channel-shift response",
				Request: simulationRequest(projectID, question, seedPaths, &horizonmodels.SimulationShock{
					Text:         "Повышение НДС до 16% в Казахстане: реакция потребителей, розницы и смещение спроса в альтернативные каналы.",
					Type:         "demand",
					Axis:         horizonmodels.AxisPhysicalFlowDemand,
					Polarity:     "risk",
					Magnitude:    0.48,
					LocationPath: "/loc/kz",
					SeedPaths:    seedPaths,
				}),
			},
			{
				ID:    "narrative-volatility",
				Title: "Narrative and lobbying response",
				Request: simulationRequest(projectID, question, seedPaths, &horizonmodels.SimulationShock{
					Text:         "Повышение НДС до 16% в Казахстане: публичная реакция, лоббизм, информационная неопределённость и давление на регулятора.",
					Type:         "regulatory",
					Axis:         horizonmodels.AxisInfoUncertainty,
					Polarity:     "risk",
					Magnitude:    0.44,
					LocationPath: "/loc/kz",
					SeedPaths:    seedPaths,
				}),
			},
		},
	}
}

func SeedPaths() []string {
	return []string{
		"/loc/kz",
		"/gov/kz/ministry-of-finance",
		"/gov/kz/state-revenue-committee",
		"/gov/kz/ministry-of-national-economy",
		"/gov/kz/ministry-of-health",
		"/gov/kz/ministry-of-industry-and-construction",
		"/gov/kz/ministry-of-education",
		"/decision/taxation/raise-vat-to-16pct-kazakhstan-2026",
		"/mechanism/regulatory/vat-rate-increase",
		"/program/kz/vat-reform-2026",
		"/program/kz/investment-climate-stability",
		"/program/kz/domestic-production-continuity",
		"/program/kz/public-services-resilience",
		"/market/kz/consumer/food",
		"/market/kz/consumer/retail",
		"/market/kz/industrial/import-distribution",
		"/market/kz/finance/working-capital",
		"/market/kz/public-sector/budget-revenue",
		"/market/kz/finance/investment-climate",
		"/market/kz/industrial/internal-production",
		"/market/kz/healthcare/healthcare-public",
		"/market/kz/healthcare/healthcare-private",
		"/market/kz/construction/engineering-services",
		"/market/kz/education/education-system",
		"/community/kz/trade/bazaar-traders",
	}
}

func metric(value float64, unit string) horizonmodels.MetricValue {
	unit = strings.TrimSpace(unit)
	var unitPtr *string
	if unit != "" {
		unitCopy := unit
		unitPtr = &unitCopy
	}
	return horizonmodels.MetricValue{
		Value:      value,
		Unit:       unitPtr,
		Confidence: 0.72,
		ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Provenance: horizonmodels.Provenance{
			Method: "scenario.hot-path.seed",
		},
	}
}

func simulationRequest(projectID, question string, seedPaths []string, shock *horizonmodels.SimulationShock) horizonmodels.SimulationRunRequest {
	return horizonmodels.SimulationRunRequest{
		Question:     question,
		Signal:       shock,
		SeedPaths:    append([]string(nil), seedPaths...),
		WorldCount:   8,
		TimeHorizon:  horizonmodels.SignalTimeHorizonShortTerm,
		TickCount:    6,
		Audience:     "c_level",
		Mode:         "policy_hot_path",
		ProjectID:    strings.TrimSpace(projectID),
		DomainModes:  []string{"policy_impact"},
		ArtifactKeys: nil,
	}
}
