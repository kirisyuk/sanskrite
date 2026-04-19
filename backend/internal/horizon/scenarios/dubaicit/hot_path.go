package dubaicit

import (
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	ScenarioID    = "dubai-corporate-tax-10pct"
	ScenarioTitle = "Dubai corporate income tax 10% regulatory hot path"
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
	question := "Если effective corporate income tax pressure в Дубае повышается до 10%, как это распространится по free-zone business services, capital inflow, compliance execution и community channels?"
	spec := HotPathSpec{
		ID:           ScenarioID,
		Title:        ScenarioTitle,
		Question:     question,
		LocationPath: "/loc/ae/01/dubai",
		Fragments: []FragmentSeed{
			{CanonicalPath: "/loc/ae", Type: FragmentTypeGeoRegion, DisplayName: "United Arab Emirates", Description: "Country anchor for UAE fiscal and investor response.", EntityPath: "/Geo/Country", Labels: map[string]string{"en": "United Arab Emirates", "ru": "ОАЭ"}, Tags: []string{"uae", "policy-impact", "tax"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/loc/ae/01/dubai", Type: FragmentTypeGeoRegion, DisplayName: "Dubai", Description: "Dubai emirate anchor for the scenario-specific tax scope.", EntityPath: "/Geo/City", Labels: map[string]string{"en": "Dubai", "ru": "Дубай"}, Tags: []string{"dubai", "policy-impact", "tax"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/gov/ae/ministry-of-finance", Type: FragmentTypeOrganization, DisplayName: "UAE Ministry of Finance", Description: "Federal fiscal authority shaping tax policy design.", EntityPath: "/Organization/Government/Ministry", Labels: map[string]string{"en": "UAE Ministry of Finance", "ru": "Министерство финансов ОАЭ"}, Tags: []string{"uae", "government", "tax"}, GovType: "ministry", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulator"}},
			{CanonicalPath: "/gov/ae/federal-tax-authority", Type: FragmentTypeOrganization, DisplayName: "Federal Tax Authority", Description: "Tax administration and enforcement body responsible for execution.", EntityPath: "/Organization/Government/Agency", Labels: map[string]string{"en": "Federal Tax Authority", "ru": "Федеральное налоговое управление ОАЭ"}, Tags: []string{"uae", "government", "tax-administration"}, GovType: "agency", ParentGovPath: "/gov/ae/ministry-of-finance", ParentPath: "/gov/ae/ministry-of-finance", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulator"}},
			{CanonicalPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", Type: FragmentTypeDecisionPoint, DisplayName: "Raise Dubai corporate income tax to 10%", Description: "Scenario decision anchor for an increase in effective corporate tax pressure in Dubai.", EntityPath: "/Decision/Taxation/RateChange", Labels: map[string]string{"en": "Raise Dubai corporate income tax to 10%", "ru": "Повышение эффективного КПН в Дубае до 10%"}, Tags: []string{"dubai", "decision", "corporate-tax"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "jurisdiction": "ae", "rate_from_pct": 9, "rate_to_pct": 10, "effective_date": "2026-01-01"}},
			{CanonicalPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", Type: FragmentTypeMechanism, DisplayName: "Corporate income tax rate increase", Description: "Canonical tax mechanism carrying higher compliance and pass-through pressure.", EntityPath: "/Mechanism/Regulatory/Tax", Labels: map[string]string{"en": "Corporate income tax rate increase", "ru": "Повышение ставки корпоративного налога"}, Tags: []string{"mechanism", "regulatory", "corporate-tax"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "jurisdiction": "ae"}},
			{CanonicalPath: "/program/ae/corporate-tax-compliance-rollout", Type: FragmentTypeProgram, DisplayName: "Corporate tax compliance rollout", Description: "Administrative program translating the tax change into reporting, audits, and enforcement.", EntityPath: "/Program/Fiscal/Compliance", Labels: map[string]string{"en": "Corporate tax compliance rollout", "ru": "Программа запуска корпоративного налогового комплаенса"}, Tags: []string{"uae", "program", "tax-compliance"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/program/ae/free-zone-retention-program", Type: FragmentTypeProgram, DisplayName: "Free-zone retention program", Description: "Mitigation program intended to retain firms and capital inside Dubai free zones.", EntityPath: "/Program/Economic/Retention", Labels: map[string]string{"en": "Free-zone retention program", "ru": "Программа удержания компаний во free zones"}, Tags: []string{"dubai", "program", "investment"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/market/ae/professional-services/free-zone-business-services", Type: FragmentTypeMarket, DisplayName: "Dubai free-zone business services", Description: "Market bucket for advisory, legal, incorporation, and service firms exposed to tax sensitivity.", EntityPath: "/Market/ProfessionalServices/BusinessServices", Labels: map[string]string{"en": "Dubai free-zone business services", "ru": "Рынок услуг для free zones в Дубае"}, Tags: []string{"dubai", "market", "free-zone-services"}, MarketDomain: "professional-services", MarketVertical: "free-zone-business-services", MarketChannels: []string{"b2b"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/market/ae/capital/foreign-direct-investment", Type: FragmentTypeMarket, DisplayName: "Dubai foreign direct investment", Description: "Capital-allocation channel reflecting business entry, expansion, and relocation decisions.", EntityPath: "/Market/Capital/ForeignDirectInvestment", Labels: map[string]string{"en": "Dubai foreign direct investment", "ru": "Канал прямых иностранных инвестиций в Дубае"}, Tags: []string{"dubai", "market", "fdi"}, MarketDomain: "capital", MarketVertical: "foreign-direct-investment", MarketChannels: []string{"capital"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}, State: map[string]horizonmodels.MetricValue{"/metric/capital/fdi_pipeline_bln_usd": metric(14.5, "bln_usd")}},
			{CanonicalPath: "/org/ae/dubai-international-financial-centre", Type: FragmentTypeOrganization, DisplayName: "Dubai International Financial Centre", Description: "Institutional anchor for internationally oriented firms and tax-sensitive corporate structures.", EntityPath: "/Organization/Services/FinancialCentre", Labels: map[string]string{"en": "Dubai International Financial Centre", "ru": "Dubai International Financial Centre"}, Tags: []string{"dubai", "organization", "financial-centre"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"}},
			{CanonicalPath: "/org/ae/dubai-chamber-of-commerce", Type: FragmentTypeOrganization, DisplayName: "Dubai Chamber of Commerce", Description: "Representative formal body aggregating corporate response and lobbying signals.", EntityPath: "/Organization/Association/Chamber", Labels: map[string]string{"en": "Dubai Chamber of Commerce", "ru": "Торгово-промышленная палата Дубая"}, Tags: []string{"dubai", "organization", "business-community"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"}},
			{CanonicalPath: "/community/ae/industry-association/dubai-family-business-network", Type: FragmentTypeCommunity, DisplayName: "Dubai family business network", Description: "Community layer capturing informal coordination, lobbying, and relocation sentiment across major business families.", EntityPath: "/Community/IndustryAssociation/FamilyBusiness", Labels: map[string]string{"en": "Dubai family business network", "ru": "Сеть семейных бизнесов Дубая"}, Tags: []string{"dubai", "community", "family-business"}, CommunityType: "industry-association", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{CanonicalPath: "/media/linkedin/dubai-business-founders", Type: FragmentTypeMediaChannel, DisplayName: "Dubai business founders on LinkedIn", Description: "Public narrative channel for relocation, tax planning, and founder sentiment.", EntityPath: "/Media/LinkedIn/Channel", Labels: map[string]string{"en": "Dubai business founders on LinkedIn", "ru": "Dubai business founders в LinkedIn"}, Tags: []string{"dubai", "media", "founders"}, Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
		},
		Edges: []EdgeSeed{
			{SourcePath: "/gov/ae/ministry-of-finance", TargetPath: "/gov/ae/federal-tax-authority", Type: DependencyTypeAdministers, Criticality: 0.84, Amplification: 1.02, Reason: "The ministry directs tax administration execution.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/gov/ae/ministry-of-finance", TargetPath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", Type: DependencyTypeGatekeepsDecision, Criticality: 0.92, Amplification: 1.05, Reason: "The finance ministry gates the fiscal decision.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/gov/ae/federal-tax-authority", TargetPath: "/program/ae/corporate-tax-compliance-rollout", Type: DependencyTypeAdministers, Criticality: 0.86, Amplification: 1.03, Reason: "FTA executes the compliance rollout.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/decision/taxation/raise-dubai-corporate-income-tax-to-10pct", TargetPath: "/mechanism/regulatory/corporate-income-tax-rate-increase", Type: DependencyTypeUsesMechanism, Criticality: 0.95, Amplification: 1.08, Reason: "The decision materializes through the corporate tax rate mechanism.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetPath: "/loc/ae/01/dubai", Type: DependencyTypeInGeoScope, Criticality: 0.90, Amplification: 1.02, Reason: "The scenario is geographically scoped to Dubai.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetPath: "/market/ae/professional-services/free-zone-business-services", Type: DependencyTypeAppliesTo, Criticality: 0.88, Amplification: 1.06, Reason: "Free-zone service firms face direct compliance and pricing pressure.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "tax_base_coverage": "full", "sector_sensitivity": "high", "pass_through_estimate": 0.74}},
			{SourcePath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetPath: "/market/ae/capital/foreign-direct-investment", Type: DependencyTypeAffects, Criticality: 0.86, Amplification: 1.05, Reason: "Higher tax pressure affects capital-allocation and entity-location decisions.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "pass_through_estimate": 0.62, "lag_quarters": 2, "channel_type": "behavioral"}},
			{SourcePath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetPath: "/org/ae/dubai-international-financial-centre", Type: DependencyTypeImposesCondition, Criticality: 0.90, Amplification: 1.07, Reason: "DIFC-linked structures face tighter tax and reporting conditions.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "rate_from": 9, "rate_to": 10, "effective_date": "2026-01-01", "pass_through_estimate": 0.78}},
			{SourcePath: "/mechanism/regulatory/corporate-income-tax-rate-increase", TargetPath: "/org/ae/dubai-chamber-of-commerce", Type: DependencyTypeInfluences, Criticality: 0.72, Amplification: 1.01, Reason: "Business chambers amplify lobbying and guidance responses.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "channel_type": "behavioral", "lag_quarters": 1}},
			{SourcePath: "/program/ae/corporate-tax-compliance-rollout", TargetPath: "/org/ae/dubai-international-financial-centre", Type: DependencyTypeImposesCondition, Criticality: 0.82, Amplification: 1.04, Reason: "Execution program raises filing and audit discipline.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "measurement_lag_months": 3}},
			{SourcePath: "/program/ae/free-zone-retention-program", TargetPath: "/market/ae/capital/foreign-direct-investment", Type: DependencyTypeAffects, Criticality: 0.74, Amplification: 0.97, Reason: "Retention incentives partly offset capital flight.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "pass_through_estimate": 0.45, "lag_quarters": 2}},
			{SourcePath: "/community/ae/industry-association/dubai-family-business-network", TargetPath: "/org/ae/dubai-chamber-of-commerce", Type: DependencyTypeAffiliatedWith, Criticality: 0.71, Amplification: 1.00, Reason: "The family-business network coordinates through formal chamber structures.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/community/ae/industry-association/dubai-family-business-network", TargetPath: "/media/linkedin/dubai-business-founders", Type: DependencyTypeOperatesVia, Criticality: 0.69, Amplification: 1.00, Reason: "Narrative coordination and founder sentiment flow through LinkedIn channels.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/community/ae/industry-association/dubai-family-business-network", TargetPath: "/market/ae/capital/foreign-direct-investment", Type: DependencyTypeAffects, Criticality: 0.67, Amplification: 1.01, Reason: "Family-business relocation sentiment affects new investment decisions.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "pass_through_estimate": 0.55, "lag_quarters": 2}},
			{SourcePath: "/community/ae/industry-association/dubai-family-business-network", TargetPath: "/market/ae/professional-services/free-zone-business-services", Type: DependencyTypeParticipatesIn, Criticality: 0.72, Amplification: 1.01, Reason: "The network directly participates in the free-zone services demand base.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/ae/dubai-international-financial-centre", TargetPath: "/market/ae/capital/foreign-direct-investment", Type: DependencyTypeParticipatesIn, Criticality: 0.83, Amplification: 1.02, Reason: "DIFC is a core participant in Dubai FDI flows.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/ae/dubai-chamber-of-commerce", TargetPath: "/market/ae/professional-services/free-zone-business-services", Type: DependencyTypeParticipatesIn, Criticality: 0.76, Amplification: 1.01, Reason: "The chamber shapes business-services throughput and guidance.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/ae/dubai-international-financial-centre", TargetPath: "/loc/ae/01/dubai", Type: DependencyTypeOperatesIn, Criticality: 0.78, Amplification: 1.00, Reason: "DIFC operates in Dubai.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/market/ae/professional-services/free-zone-business-services", TargetPath: "/loc/ae/01/dubai", Type: DependencyTypeOperatesIn, Criticality: 0.73, Amplification: 1.00, Reason: "Free-zone business services are Dubai-scoped.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/market/ae/capital/foreign-direct-investment", TargetPath: "/loc/ae/01/dubai", Type: DependencyTypeOperatesIn, Criticality: 0.75, Amplification: 1.00, Reason: "FDI channel is anchored in Dubai.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/community/ae/industry-association/dubai-family-business-network", TargetPath: "/loc/ae/01/dubai", Type: DependencyTypeOperatesIn, Criticality: 0.70, Amplification: 1.00, Reason: "The community is Dubai-scoped.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
		},
	}
	seedPaths := seedPathsFromFragments(spec.Fragments)
	spec.Simulations = []SimulationSpec{
		simulation(projectID, question, seedPaths, "dubai-legal", "Dubai compliance contour", horizonmodels.AxisLegalCompliance, "regulatory", 0.62),
		simulation(projectID, question, seedPaths, "dubai-financial", "Dubai cashflow contour", horizonmodels.AxisFinancialSettlement, "price", 0.56),
		simulation(projectID, question, seedPaths, "dubai-capital", "Dubai capital allocation contour", horizonmodels.AxisPhysicalFlowDemand, "capital_shift", 0.51),
		simulation(projectID, question, seedPaths, "dubai-narrative", "Dubai narrative and lobbying contour", horizonmodels.AxisInfoUncertainty, "policy_fog", 0.47),
	}
	return spec
}

func SeedPaths() []string {
	return seedPathsFromFragments(Spec("").Fragments)
}

func seedPathsFromFragments(fragments []FragmentSeed) []string {
	out := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		out = append(out, fragment.CanonicalPath)
	}
	return out
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
		Provenance: horizonmodels.Provenance{Method: "scenario.hot-path.seed"},
	}
}

func simulation(projectID, question string, seedPaths []string, id, title, axis, shockType string, magnitude float64) SimulationSpec {
	return SimulationSpec{
		ID:    id,
		Title: title,
		Request: horizonmodels.SimulationRunRequest{
			Question: question,
			Signal: &horizonmodels.SimulationShock{
				Text:         "Dubai corporate tax pressure rises to 10% and affects compliance, capital allocation, and business relocation channels.",
				Type:         shockType,
				Axis:         axis,
				Polarity:     "risk",
				Magnitude:    magnitude,
				LocationPath: "/loc/ae/01/dubai",
				ActorPaths: []string{
					"/gov/ae/ministry-of-finance",
					"/gov/ae/federal-tax-authority",
					"/org/ae/dubai-international-financial-centre",
				},
				SeedPaths: seedPaths,
			},
			SeedPaths:   append([]string(nil), seedPaths...),
			WorldCount:  8,
			TimeHorizon: horizonmodels.SignalTimeHorizonShortTerm,
			TickCount:   6,
			Audience:    "c_level",
			Mode:        "policy_hot_path",
			ProjectID:   strings.TrimSpace(projectID),
			DomainModes: []string{"policy_impact"},
		},
	}
}
