package teslabattery

import (
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	ScenarioID    = "tesla-battery-logistics-trade-shock-2026"
	ScenarioTitle = "Tesla battery logistics trade shock hot path"
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
	baseQuestion := "Новые экспортные ограничения и рост морских и сухопутных задержек на 20-30% по батарейным материалам и ячейкам для Tesla. Как отреагирует battery logistics контур Tesla в ближайшие 2-4 квартала: где возникнут bottleneck, как поведут себя Gigafactory Texas и Nevada, что произойдет с lead time, inventory, freight cost и product mix?"
	now := time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC)
	spec := HotPathSpec{
		ID:           ScenarioID,
		Title:        ScenarioTitle,
		Question:     baseQuestion,
		LocationPath: "/loc/us",
		Fragments: []FragmentSeed{
			{
				CanonicalPath: "/loc/us",
				Type:          FragmentTypeGeoRegion,
				DisplayName:   "United States",
				Description:   "Primary destination geography for Tesla North American battery logistics and trade-exposed execution.",
				EntityPath:    "/Geo/Country",
				Labels:        map[string]string{"en": "United States", "ru": "Соединенные Штаты"},
				Tags:          []string{"tesla", "battery-logistics", "trade-shock", "scenario-hot-path"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "scenarioKind": "supply_chain_policy_impact"},
			},
			{
				CanonicalPath: "/loc/jp",
				Type:          FragmentTypeGeoRegion,
				DisplayName:   "Japan",
				Description:   "Supplier geography for Panasonic battery cell lines serving Tesla-related output.",
				EntityPath:    "/Geo/Country",
				Labels:        map[string]string{"en": "Japan", "ru": "Япония"},
				Tags:          []string{"tesla", "battery-logistics", "supplier-geo"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/loc/cn",
				Type:          FragmentTypeGeoRegion,
				DisplayName:   "China",
				Description:   "Trade-exposed supplier geography for battery cells and related components.",
				EntityPath:    "/Geo/Country",
				Labels:        map[string]string{"en": "China", "ru": "Китай"},
				Tags:          []string{"tesla", "battery-logistics", "supplier-geo"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/loc/cl",
				Type:          FragmentTypeGeoRegion,
				DisplayName:   "Chile",
				Description:   "Critical minerals source geography for lithium feedstock.",
				EntityPath:    "/Geo/Country",
				Labels:        map[string]string{"en": "Chile", "ru": "Чили"},
				Tags:          []string{"tesla", "battery-logistics", "raw-materials"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/org/us/tesla",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Tesla",
				Description:   "Primary enterprise anchor for North American battery logistics, procurement, production planning, and investor expectations.",
				EntityPath:    "/Organization/Industrial/Automotive",
				Labels:        map[string]string{"en": "Tesla", "ru": "Tesla"},
				Tags:          []string{"tesla", "oem", "battery-logistics", "electric-vehicles", "energy-storage"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/org/jp/panasonic",
				Type:          FragmentTypeOrganization,
				DisplayName:   "Panasonic Energy",
				Description:   "Major battery cell supplier and line operator relevant to Tesla battery output and contingency sourcing.",
				EntityPath:    "/Organization/Industrial/Electronics",
				Labels:        map[string]string{"en": "Panasonic Energy", "ru": "Panasonic Energy"},
				Tags:          []string{"supplier", "battery-cells", "tesla", "battery-logistics"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/org/cn/catl",
				Type:          FragmentTypeOrganization,
				DisplayName:   "CATL",
				Description:   "Battery cell supplier and external capacity source whose export posture affects Tesla optionality and trade exposure.",
				EntityPath:    "/Organization/Industrial/BatteryManufacturing",
				Labels:        map[string]string{"en": "CATL", "ru": "CATL"},
				Tags:          []string{"supplier", "battery-cells", "tesla", "trade-risk"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/org/cl/sqm",
				Type:          FragmentTypeOrganization,
				DisplayName:   "SQM",
				Description:   "Critical lithium supplier relevant to upstream material continuity and refining feedstock.",
				EntityPath:    "/Organization/Industrial/Materials",
				Labels:        map[string]string{"en": "SQM", "ru": "SQM"},
				Tags:          []string{"supplier", "lithium", "battery-materials", "tesla"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/gov/us/department-of-commerce",
				Type:          FragmentTypeOrganization,
				DisplayName:   "U.S. Department of Commerce",
				Description:   "Trade policy authority shaping export control, industrial compliance, and logistics restriction posture.",
				EntityPath:    "/Organization/Government/Ministry",
				Labels:        map[string]string{"en": "U.S. Department of Commerce", "ru": "Министерство торговли США"},
				Tags:          []string{"government", "trade-policy", "export-controls", "tesla"},
				GovType:       "ministry",
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulator"},
			},
			{
				CanonicalPath: "/gov/us/customs-and-border-protection",
				Type:          FragmentTypeOrganization,
				DisplayName:   "U.S. Customs and Border Protection",
				Description:   "Operational customs authority affecting clearance times, inspections, and shipment friction.",
				EntityPath:    "/Organization/Government/Agency",
				Labels:        map[string]string{"en": "U.S. Customs and Border Protection", "ru": "Таможенно-пограничная служба США"},
				Tags:          []string{"government", "customs", "trade-policy", "tesla"},
				GovType:       "agency",
				ParentGovPath: "/gov/us/department-of-commerce",
				ParentPath:    "/gov/us/department-of-commerce",
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulator"},
			},
			{
				CanonicalPath: "/fac/us/gigafactory-nevada",
				Type:          FragmentTypeFacility,
				DisplayName:   "Tesla Gigafactory Nevada",
				Description:   "Nevada facility representing battery pack, cell, and energy storage flow sensitivity for Tesla.",
				EntityPath:    "/Facility/Industrial/Factory",
				Labels:        map[string]string{"en": "Tesla Gigafactory Nevada", "ru": "Tesla Gigafactory Nevada"},
				Tags:          []string{"tesla", "facility", "gigafactory", "battery-logistics"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "route"},
				State: map[string]horizonmodels.MetricValue{
					"/metric/logistics/inventory_days":      metric(25, "days"),
					"/metric/logistics/on_time_delivery_pct": metric(93, "percent"),
				},
			},
			{
				CanonicalPath: "/fac/us/gigafactory-texas",
				Type:          FragmentTypeFacility,
				DisplayName:   "Tesla Gigafactory Texas",
				Description:   "Texas facility representing vehicle and 4680-oriented execution sensitivity under supply and trade friction.",
				EntityPath:    "/Facility/Industrial/Factory",
				Labels:        map[string]string{"en": "Tesla Gigafactory Texas", "ru": "Tesla Gigafactory Texas"},
				Tags:          []string{"tesla", "facility", "gigafactory", "battery-logistics"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "route"},
				State: map[string]horizonmodels.MetricValue{
					"/metric/logistics/lead_time_days":      metric(19, "days"),
					"/metric/logistics/on_time_delivery_pct": metric(91, "percent"),
				},
			},
			{
				CanonicalPath: "/fac/us/tesla-lithium-refinery-texas",
				Type:          FragmentTypeFacility,
				DisplayName:   "Tesla lithium refinery Texas",
				Description:   "Refining node converting lithium feedstock into usable battery chemistry inputs and reducing dependency concentration.",
				EntityPath:    "/Facility/Industrial/Refinery",
				Labels:        map[string]string{"en": "Tesla lithium refinery Texas", "ru": "Литиевый рафинадный узел Tesla в Техасе"},
				Tags:          []string{"tesla", "facility", "refining", "lithium"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/fac/jp/panasonic-giga-nevada-lines",
				Type:          FragmentTypeFacility,
				DisplayName:   "Panasonic Nevada battery lines",
				Description:   "Supplier-operated line cluster associated with battery cell production relevant to Tesla battery flow continuity.",
				EntityPath:    "/Facility/Industrial/Factory",
				Labels:        map[string]string{"en": "Panasonic Nevada battery lines", "ru": "Линии Panasonic для Nevada"},
				Tags:          []string{"panasonic", "battery-cells", "supplier-facility"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/fac/cn/catl-ningde",
				Type:          FragmentTypeFacility,
				DisplayName:   "CATL Ningde battery base",
				Description:   "Chinese battery cell production base representing optional and trade-exposed external sourcing capacity.",
				EntityPath:    "/Facility/Industrial/Factory",
				Labels:        map[string]string{"en": "CATL Ningde battery base", "ru": "База CATL в Ниндэ"},
				Tags:          []string{"catl", "battery-cells", "supplier-facility", "trade-risk"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/fac/cl/sqm-atacama-operations",
				Type:          FragmentTypeFacility,
				DisplayName:   "SQM Atacama lithium operations",
				Description:   "Upstream lithium operations feeding the broader battery material chain.",
				EntityPath:    "/Facility/Industrial/Mine",
				Labels:        map[string]string{"en": "SQM Atacama lithium operations", "ru": "Литиевые операции SQM в Атакаме"},
				Tags:          []string{"sqm", "lithium", "upstream", "battery-materials"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "supplier"},
			},
			{
				CanonicalPath: "/fac/us/port-of-long-beach",
				Type:          FragmentTypeFacility,
				DisplayName:   "Port of Long Beach",
				Description:   "Representative maritime gateway for import-heavy battery logistics and customs friction.",
				EntityPath:    "/Facility/Infrastructure/Port",
				Labels:        map[string]string{"en": "Port of Long Beach", "ru": "Порт Лонг-Бич"},
				Tags:          []string{"port", "logistics", "battery-logistics", "trade-risk"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "route"},
			},
			{
				CanonicalPath: "/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026",
				Type:          FragmentTypeDecisionPoint,
				DisplayName:   "Restrict battery material and cell flows into the U.S.",
				Description:   "Trade decision point encoding tighter export controls, customs friction, and industrial policy stress on battery logistics.",
				EntityPath:    "/Decision/Trade/Restriction",
				Labels:        map[string]string{"en": "Restrict battery material and cell flows into the U.S.", "ru": "Ограничение потоков материалов и ячеек батарей в США"},
				Tags:          []string{"trade", "battery-logistics", "decision", "tesla"},
				Qualifiers: map[string]interface{}{
					"scenarioHotPath": ScenarioID,
					"jurisdiction":    "us",
					"restrictionType": "export_control_and_customs_delay",
				},
			},
			{
				CanonicalPath: "/mechanism/regulatory/export-controls-and-logistics-delays",
				Type:          FragmentTypeMechanism,
				DisplayName:   "Export controls and logistics delays",
				Description:   "Combined mechanism for export-control friction, customs delay, shipment uncertainty, and routing stress.",
				EntityPath:    "/Mechanism/Trade/Restriction",
				Labels:        map[string]string{"en": "Export controls and logistics delays", "ru": "Экспортные ограничения и логистические задержки"},
				Tags:          []string{"mechanism", "trade", "customs", "logistics"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "jurisdiction": "us"},
			},
			{
				CanonicalPath: "/program/us/tesla-battery-logistics-resilience-2026",
				Type:          FragmentTypeProgram,
				DisplayName:   "Tesla battery logistics resilience 2026",
				Description:   "Programmatic response for supplier switching, buffer inventory, routing, and product-priority adaptation.",
				EntityPath:    "/Program/Industrial/SupplyResilience",
				Labels:        map[string]string{"en": "Tesla battery logistics resilience 2026", "ru": "Программа устойчивости battery logistics Tesla 2026"},
				Tags:          []string{"program", "tesla", "battery-logistics", "resilience"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/market/us/industrial/critical-battery-materials",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. critical battery materials market",
				Description:   "Upstream materials exposure bucket for lithium feedstock, refining dependence, and procurement risk.",
				EntityPath:    "/Market/Industrial/BatteryMaterials",
				Labels:        map[string]string{"en": "U.S. critical battery materials market", "ru": "Рынок критических материалов для батарей в США"},
				Tags:          []string{"market", "battery-materials", "tesla", "trade-risk"},
				MarketDomain:  "industrial",
				MarketVertical:"critical-battery-materials",
				MarketChannels: []string{"b2b"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "import-dependent"},
				State: map[string]horizonmodels.MetricValue{
					"/metric/logistics/lead_time_days": metric(27, "days"),
				},
			},
			{
				CanonicalPath: "/market/us/industrial/ev-battery-cells",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. EV battery cells market",
				Description:   "Core battery cell market for Tesla production continuity, supplier concentration, and throughput stress.",
				EntityPath:    "/Market/Industrial/EVBatteryCells",
				Labels:        map[string]string{"en": "U.S. EV battery cells market", "ru": "Рынок EV battery cells в США"},
				Tags:          []string{"market", "battery-cells", "tesla", "industrial"},
				MarketDomain:  "industrial",
				MarketVertical:"ev-battery-cells",
				MarketChannels: []string{"b2b"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "import-dependent"},
				State: map[string]horizonmodels.MetricValue{
					"/metric/logistics/inventory_days": metric(22, "days"),
				},
			},
			{
				CanonicalPath: "/market/us/transportation/freight-logistics",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. freight logistics market",
				Description:   "Freight market exposure bucket for ocean, port, rail, and truck logistics serving battery flows.",
				EntityPath:    "/Market/Transportation/FreightLogistics",
				Labels:        map[string]string{"en": "U.S. freight logistics market", "ru": "Рынок грузовой логистики США"},
				Tags:          []string{"market", "freight", "logistics", "tesla"},
				MarketDomain:  "transportation",
				MarketVertical:"freight-logistics",
				MarketChannels: []string{"b2b"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "route"},
				State: map[string]horizonmodels.MetricValue{
					"/metric/logistics/freight_cost_index": metric(100, "index"),
				},
			},
			{
				CanonicalPath: "/market/us/finance/industrial-working-capital",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. industrial working capital market",
				Description:   "Working-capital and settlement stress channel for freight cost, buffer inventory, and supplier prepayment dynamics.",
				EntityPath:    "/Market/Finance/WorkingCapital",
				Labels:        map[string]string{"en": "U.S. industrial working capital market", "ru": "Рынок промышленного оборотного капитала США"},
				Tags:          []string{"market", "finance", "working-capital", "tesla"},
				MarketDomain:  "finance",
				MarketVertical:"industrial-working-capital",
				MarketChannels: []string{"capital", "b2b"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/market/us/automotive/electric-vehicles",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. electric vehicles market",
				Description:   "Downstream EV market capturing product-priority choices, allocation pressure, and delivery risk.",
				EntityPath:    "/Market/Automotive/ElectricVehicles",
				Labels:        map[string]string{"en": "U.S. electric vehicles market", "ru": "Рынок электромобилей США"},
				Tags:          []string{"market", "ev", "tesla", "downstream"},
				MarketDomain:  "automotive",
				MarketVertical:"electric-vehicles",
				MarketChannels: []string{"b2c", "b2b"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/market/us/energy/energy-storage-systems",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. energy storage systems market",
				Description:   "Downstream storage market capturing Megapack and stationary-storage allocation trade-offs.",
				EntityPath:    "/Market/Energy/EnergyStorageSystems",
				Labels:        map[string]string{"en": "U.S. energy storage systems market", "ru": "Рынок систем накопления энергии США"},
				Tags:          []string{"market", "energy-storage", "tesla", "downstream"},
				MarketDomain:  "energy",
				MarketVertical:"energy-storage-systems",
				MarketChannels: []string{"b2b", "utility"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/market/us/capital/industrial-investor-confidence",
				Type:          FragmentTypeMarket,
				DisplayName:   "U.S. industrial investor confidence",
				Description:   "Narrative and confidence channel for investor interpretation of supply resilience, margin pressure, and execution risk.",
				EntityPath:    "/Market/Capital/InvestorConfidence",
				Labels:        map[string]string{"en": "U.S. industrial investor confidence", "ru": "Инвесторская уверенность в промышленном секторе США"},
				Tags:          []string{"market", "capital", "investor-confidence", "tesla"},
				MarketDomain:  "capital",
				MarketVertical:"industrial-investor-confidence",
				MarketChannels: []string{"capital"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID, "objectQualifier": "regulated"},
			},
			{
				CanonicalPath: "/product/raw/chemical/refining/lithium-carbonate",
				Type:          FragmentTypeProduct,
				DisplayName:   "Lithium carbonate",
				Description:   "Upstream lithium chemical feedstock.",
				EntityPath:    "/Product/Raw/Chemical/Refining/LithiumCarbonate",
				Labels:        map[string]string{"en": "Lithium carbonate", "ru": "Карбонат лития"},
				Tags:          []string{"product", "lithium", "battery-materials"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/raw/chemical/refining/lithium-hydroxide",
				Type:          FragmentTypeProduct,
				DisplayName:   "Lithium hydroxide",
				Description:   "Refined lithium input for battery chemistry.",
				EntityPath:    "/Product/Raw/Chemical/Refining/LithiumHydroxide",
				Labels:        map[string]string{"en": "Lithium hydroxide", "ru": "Гидроксид лития"},
				Tags:          []string{"product", "lithium", "battery-materials"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/component/chemical/manufacturing/cathode-active-material",
				Type:          FragmentTypeProduct,
				DisplayName:   "Cathode active material",
				Description:   "Battery chemical intermediate.",
				EntityPath:    "/Product/Component/Chemical/Manufacturing/CathodeActiveMaterial",
				Labels:        map[string]string{"en": "Cathode active material", "ru": "Активный катодный материал"},
				Tags:          []string{"product", "battery-materials", "cells"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/component/electronic/manufacturing/ev-battery-cells",
				Type:          FragmentTypeProduct,
				DisplayName:   "EV battery cells",
				Description:   "Core battery cell output for EV and stationary storage manufacturing.",
				EntityPath:    "/Product/Component/Electronic/Manufacturing/EVBatteryCells",
				Labels:        map[string]string{"en": "EV battery cells", "ru": "Батарейные ячейки для EV"},
				Tags:          []string{"product", "battery-cells", "tesla"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/component/electronic/assembly/ev-battery-module",
				Type:          FragmentTypeProduct,
				DisplayName:   "EV battery module",
				Description:   "Battery module assembled from cells.",
				EntityPath:    "/Product/Component/Electronic/Assembly/EVBatteryModule",
				Labels:        map[string]string{"en": "EV battery module", "ru": "Батарейный модуль EV"},
				Tags:          []string{"product", "battery-module", "tesla"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/component/electronic/assembly/ev-battery-pack",
				Type:          FragmentTypeProduct,
				DisplayName:   "EV battery pack",
				Description:   "Battery pack assembled from modules.",
				EntityPath:    "/Product/Component/Electronic/Assembly/EVBatteryPack",
				Labels:        map[string]string{"en": "EV battery pack", "ru": "Батарейный пакет EV"},
				Tags:          []string{"product", "battery-pack", "tesla"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
			{
				CanonicalPath: "/product/finished/mechanical/assembly/electric-vehicle",
				Type:          FragmentTypeProduct,
				DisplayName:   "Electric vehicle",
				Description:   "Finished downstream product dependent on battery pack continuity.",
				EntityPath:    "/Product/Finished/Mechanical/Assembly/ElectricVehicle",
				Labels:        map[string]string{"en": "Electric vehicle", "ru": "Электромобиль"},
				Tags:          []string{"product", "electric-vehicle", "tesla"},
				Qualifiers:    map[string]interface{}{"scenarioHotPath": ScenarioID},
			},
		},
		Edges: []EdgeSeed{
			{SourcePath: "/gov/us/department-of-commerce", TargetPath: "/gov/us/customs-and-border-protection", Type: DependencyTypeAdministers, Criticality: 0.90, Amplification: 1.04, Reason: "Commerce policy posture is operationalized through customs control and clearance pressure.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/gov/us/department-of-commerce", TargetPath: "/program/us/tesla-battery-logistics-resilience-2026", Type: DependencyTypeAdministers, Criticality: 0.82, Amplification: 1.02, Reason: "Trade and industry policy response manages resilience planning.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/gov/us/department-of-commerce", TargetPath: "/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026", Type: DependencyTypeGatekeepsDecision, Criticality: 0.94, Amplification: 1.08, Reason: "Trade authority gates the restriction decision.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026", TargetPath: "/mechanism/regulatory/export-controls-and-logistics-delays", Type: DependencyTypeUsesMechanism, Criticality: 0.96, Amplification: 1.10, Reason: "Decision is executed through export-control and logistics-delay mechanisms.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/mechanism/regulatory/export-controls-and-logistics-delays", TargetPath: "/market/us/industrial/critical-battery-materials", Type: DependencyTypeAppliesTo, Criticality: 0.92, Amplification: 1.12, Reason: "Upstream material flows are directly exposed to trade friction.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/mechanism/regulatory/export-controls-and-logistics-delays", TargetPath: "/market/us/industrial/ev-battery-cells", Type: DependencyTypeAppliesTo, Criticality: 0.95, Amplification: 1.15, Reason: "Battery cell flow is a direct point of exposure.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/mechanism/regulatory/export-controls-and-logistics-delays", TargetPath: "/market/us/transportation/freight-logistics", Type: DependencyTypeAppliesTo, Criticality: 0.90, Amplification: 1.14, Reason: "Freight and customs delays propagate through logistics lanes.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/program/us/tesla-battery-logistics-resilience-2026", TargetPath: "/market/us/finance/industrial-working-capital", Type: DependencyTypeImposesCondition, Criticality: 0.84, Amplification: 1.05, Reason: "Buffer inventory, rerouting, and prepayments raise working-capital stress.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/program/us/tesla-battery-logistics-resilience-2026", TargetPath: "/market/us/automotive/electric-vehicles", Type: DependencyTypeImposesCondition, Criticality: 0.78, Amplification: 1.03, Reason: "Product mix prioritization changes EV output allocation.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/program/us/tesla-battery-logistics-resilience-2026", TargetPath: "/market/us/energy/energy-storage-systems", Type: DependencyTypeImposesCondition, Criticality: 0.80, Amplification: 1.04, Reason: "Storage product allocation responds to constrained cells and logistics.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/program/us/tesla-battery-logistics-resilience-2026", TargetPath: "/market/us/capital/industrial-investor-confidence", Type: DependencyTypeInfluences, Criticality: 0.70, Amplification: 1.02, Reason: "Narrative around resilience and execution quality affects investor confidence.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/us/tesla", TargetPath: "/loc/us", Type: DependencyTypeOperatesIn, Criticality: 0.88, Amplification: 1.00, Reason: "Tesla North American battery logistics execution is centered in the U.S.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/jp/panasonic", TargetPath: "/loc/jp", Type: DependencyTypeOperatesIn, Criticality: 0.74, Amplification: 1.00, Reason: "Panasonic remains a supplier geography anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/cn/catl", TargetPath: "/loc/cn", Type: DependencyTypeOperatesIn, Criticality: 0.76, Amplification: 1.00, Reason: "CATL is anchored in China for trade-exposed external sourcing.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/cl/sqm", TargetPath: "/loc/cl", Type: DependencyTypeOperatesIn, Criticality: 0.72, Amplification: 1.00, Reason: "SQM upstream materials anchor is in Chile.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-nevada", TargetPath: "/loc/us", Type: DependencyTypeLocatedIn, Criticality: 0.90, Amplification: 1.00, Reason: "Nevada facility location anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-texas", TargetPath: "/loc/us", Type: DependencyTypeLocatedIn, Criticality: 0.90, Amplification: 1.00, Reason: "Texas facility location anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/tesla-lithium-refinery-texas", TargetPath: "/loc/us", Type: DependencyTypeLocatedIn, Criticality: 0.82, Amplification: 1.00, Reason: "Refinery location anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/jp/panasonic-giga-nevada-lines", TargetPath: "/loc/jp", Type: DependencyTypeLocatedIn, Criticality: 0.70, Amplification: 1.00, Reason: "Panasonic line geography anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/cn/catl-ningde", TargetPath: "/loc/cn", Type: DependencyTypeLocatedIn, Criticality: 0.72, Amplification: 1.00, Reason: "CATL facility geography anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/cl/sqm-atacama-operations", TargetPath: "/loc/cl", Type: DependencyTypeLocatedIn, Criticality: 0.68, Amplification: 1.00, Reason: "SQM upstream facility geography anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/port-of-long-beach", TargetPath: "/loc/us", Type: DependencyTypeLocatedIn, Criticality: 0.82, Amplification: 1.00, Reason: "Port location anchor.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/cl/sqm-atacama-operations", TargetPath: "/product/raw/chemical/refining/lithium-carbonate", Type: DependencyTypeProduces, Criticality: 0.76, Amplification: 1.02, Reason: "SQM upstream operations supply lithium feedstock.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/tesla-lithium-refinery-texas", TargetPath: "/product/raw/chemical/refining/lithium-hydroxide", Type: DependencyTypeProduces, Criticality: 0.80, Amplification: 1.03, Reason: "Texas refinery outputs refined lithium inputs.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/jp/panasonic-giga-nevada-lines", TargetPath: "/product/component/electronic/manufacturing/ev-battery-cells", Type: DependencyTypeProduces, Criticality: 0.88, Amplification: 1.06, Reason: "Panasonic line cluster produces battery cells.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/cn/catl-ningde", TargetPath: "/product/component/electronic/manufacturing/ev-battery-cells", Type: DependencyTypeProduces, Criticality: 0.84, Amplification: 1.06, Reason: "CATL external capacity produces battery cells.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-nevada", TargetPath: "/product/component/electronic/assembly/ev-battery-pack", Type: DependencyTypeProduces, Criticality: 0.90, Amplification: 1.08, Reason: "Nevada facility outputs battery packs.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-texas", TargetPath: "/product/finished/mechanical/assembly/electric-vehicle", Type: DependencyTypeProduces, Criticality: 0.92, Amplification: 1.08, Reason: "Texas facility produces finished EV output.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/cl/sqm-atacama-operations", TargetPath: "/fac/us/tesla-lithium-refinery-texas", Type: DependencyTypeSuppliesTo, Criticality: 0.74, Amplification: 1.04, LatencyHours: 240, Reason: "Lithium feedstock flows into U.S. refining.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "productPath": "/product/raw/chemical/refining/lithium-carbonate"}},
			{SourcePath: "/fac/us/tesla-lithium-refinery-texas", TargetPath: "/fac/us/gigafactory-texas", Type: DependencyTypeSuppliesTo, Criticality: 0.82, Amplification: 1.06, LatencyHours: 72, Reason: "Refined lithium feeds Texas cell and pack operations.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "productPath": "/product/raw/chemical/refining/lithium-hydroxide"}},
			{SourcePath: "/fac/jp/panasonic-giga-nevada-lines", TargetPath: "/fac/us/gigafactory-nevada", Type: DependencyTypeSuppliesTo, Criticality: 0.90, Amplification: 1.08, LatencyHours: 120, Reason: "Panasonic battery lines feed Nevada output.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "productPath": "/product/component/electronic/manufacturing/ev-battery-cells"}},
			{SourcePath: "/fac/cn/catl-ningde", TargetPath: "/fac/us/gigafactory-texas", Type: DependencyTypeSuppliesTo, Criticality: 0.78, Amplification: 1.10, LatencyHours: 168, Reason: "CATL is an external trade-exposed source of battery cells.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID, "productPath": "/product/component/electronic/manufacturing/ev-battery-cells"}},
			{SourcePath: "/product/raw/chemical/refining/lithium-carbonate", TargetPath: "/product/raw/chemical/refining/lithium-hydroxide", Type: DependencyTypeTransformsInto, Criticality: 0.76, Amplification: 1.04, Reason: "Lithium carbonate is refined into lithium hydroxide.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/raw/chemical/refining/lithium-hydroxide", TargetPath: "/product/component/chemical/manufacturing/cathode-active-material", Type: DependencyTypeTransformsInto, Criticality: 0.80, Amplification: 1.05, Reason: "Refined lithium contributes to cathode active material.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/component/chemical/manufacturing/cathode-active-material", TargetPath: "/product/component/electronic/manufacturing/ev-battery-cells", Type: DependencyTypeUsedIn, Criticality: 0.86, Amplification: 1.06, Reason: "Cathode material is used in battery cell manufacturing.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/component/electronic/manufacturing/ev-battery-cells", TargetPath: "/product/component/electronic/assembly/ev-battery-module", Type: DependencyTypeAssemblesInto, Criticality: 0.92, Amplification: 1.08, Reason: "Cells assemble into modules.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/component/electronic/assembly/ev-battery-module", TargetPath: "/product/component/electronic/assembly/ev-battery-pack", Type: DependencyTypeAssemblesInto, Criticality: 0.94, Amplification: 1.08, Reason: "Modules assemble into packs.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/component/electronic/assembly/ev-battery-pack", TargetPath: "/product/finished/mechanical/assembly/electric-vehicle", Type: DependencyTypeUsedIn, Criticality: 0.96, Amplification: 1.10, Reason: "Battery packs are used in finished electric vehicles.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/raw/chemical/refining/lithium-carbonate", TargetPath: "/market/us/industrial/critical-battery-materials", Type: DependencyTypeTradedIn, Criticality: 0.76, Amplification: 1.02, Reason: "Lithium carbonate is traded through the critical materials market.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/component/electronic/manufacturing/ev-battery-cells", TargetPath: "/market/us/industrial/ev-battery-cells", Type: DependencyTypeTradedIn, Criticality: 0.92, Amplification: 1.07, Reason: "Battery cells are traded in the cell market.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/product/finished/mechanical/assembly/electric-vehicle", TargetPath: "/market/us/automotive/electric-vehicles", Type: DependencyTypeTradedIn, Criticality: 0.90, Amplification: 1.04, Reason: "EV output is traded in the downstream EV market.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-nevada", TargetPath: "/market/us/industrial/ev-battery-cells", Type: DependencyTypeParticipatesIn, Criticality: 0.84, Amplification: 1.03, Reason: "Nevada participates in battery cell and pack execution.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-nevada", TargetPath: "/market/us/energy/energy-storage-systems", Type: DependencyTypeParticipatesIn, Criticality: 0.86, Amplification: 1.03, Reason: "Nevada participates in storage-related execution.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/gigafactory-texas", TargetPath: "/market/us/automotive/electric-vehicles", Type: DependencyTypeParticipatesIn, Criticality: 0.88, Amplification: 1.03, Reason: "Texas is a primary EV output node.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/fac/us/port-of-long-beach", TargetPath: "/market/us/transportation/freight-logistics", Type: DependencyTypeParticipatesIn, Criticality: 0.82, Amplification: 1.02, Reason: "Port gateway participates in freight flow.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/us/tesla", TargetPath: "/market/us/finance/industrial-working-capital", Type: DependencyTypeParticipatesIn, Criticality: 0.82, Amplification: 1.02, Reason: "Tesla treasury and inventory choices directly affect working-capital stress.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/us/tesla", TargetPath: "/market/us/capital/industrial-investor-confidence", Type: DependencyTypeParticipatesIn, Criticality: 0.78, Amplification: 1.01, Reason: "Tesla directly shapes investor-confidence narrative.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/jp/panasonic", TargetPath: "/market/us/industrial/ev-battery-cells", Type: DependencyTypeParticipatesIn, Criticality: 0.78, Amplification: 1.02, Reason: "Panasonic participates in the battery cell supply chain.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/cn/catl", TargetPath: "/market/us/industrial/ev-battery-cells", Type: DependencyTypeParticipatesIn, Criticality: 0.76, Amplification: 1.03, Reason: "CATL optionality influences the cell market.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
			{SourcePath: "/org/cl/sqm", TargetPath: "/market/us/industrial/critical-battery-materials", Type: DependencyTypeParticipatesIn, Criticality: 0.70, Amplification: 1.02, Reason: "SQM participates in upstream materials supply.", Qualifiers: map[string]interface{}{"scenarioHotPath": ScenarioID}},
		},
	}
	seedPaths := seedPathsFromFragments(spec.Fragments)
	baseSignal := func(axis, signalType string, magnitude float64) *horizonmodels.SimulationShock {
		anchor := now
		return &horizonmodels.SimulationShock{
			Text:         "Trade restrictions and logistics delays on battery materials and cells into the US affect Tesla battery logistics.",
			Type:         signalType,
			Axis:         axis,
			Polarity:     "risk",
			Magnitude:    magnitude,
			LocationPath: "/loc/us",
			ActorPaths: []string{
				"/org/us/tesla",
				"/org/jp/panasonic",
				"/org/cn/catl",
				"/gov/us/department-of-commerce",
			},
			SeedPaths:  seedPaths,
			TimeAnchor: &anchor,
		}
	}
	req := func(id, title, axis, signalType string, magnitude float64, horizon string) SimulationSpec {
		return SimulationSpec{
			ID:    id,
			Title: title,
			Request: horizonmodels.SimulationRunRequest{
				Question:     baseQuestion,
				Signal:       baseSignal(axis, signalType, magnitude),
				SeedPaths:    seedPaths,
				WorldCount:   8,
				TimeHorizon:  horizon,
				TickCount:    7,
				Audience:     "c_level",
				Mode:         "scenario_pack",
				ProjectID:    strings.TrimSpace(projectID),
				DomainModes:  []string{"supply_chain", "policy_impact"},
				ArtifactKeys: []string{"tesla-battery-logistics", "battery-cells", "freight-cost"},
			},
		}
	}
	spec.Simulations = []SimulationSpec{
		req("tesla-legal", "Tesla trade/compliance contour", horizonmodels.AxisLegalCompliance, "trade_restriction", 0.74, "medium_term"),
		req("tesla-financial", "Tesla financial/settlement contour", horizonmodels.AxisFinancialSettlement, "freight_cost_spike", 0.72, "short_term"),
		req("tesla-physical", "Tesla physical flow contour", horizonmodels.AxisPhysicalFlowDemand, "supply_delay", 0.78, "medium_term"),
		req("tesla-info", "Tesla investor and uncertainty contour", horizonmodels.AxisInfoUncertainty, "policy_fog", 0.70, "short_term"),
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
		ObservedAt: time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		Provenance: horizonmodels.Provenance{
			Method: "scenario.hot-path.seed",
		},
	}
}
