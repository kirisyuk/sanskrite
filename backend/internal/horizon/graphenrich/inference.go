package graphenrich

import (
	"fmt"
	"math"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const graphEnrichQualifierKey = "graphEnrich"
const graphEnrichVersion = "v3"

type scoredEvidence struct {
	Value           string
	Score           float64
	Confidence      float64
	Reasons         []string
	EvidenceSources []string
	Competing       []string
}

type scoredPathEvidence struct {
	Value           string
	Score           float64
	Confidence      float64
	Reasons         []string
	EvidenceSources []string
}

type fragmentHeuristicReport struct {
	GeoFeatureType scoredEvidence
	GovType        scoredEvidence
	MarketDomain   scoredEvidence
	MarketVertical scoredEvidence
	IndustryPaths  []scoredPathEvidence
	Chokepoint     scoredEvidence
}

type edgeHeuristicReport struct {
	Facet            scoredEvidence
	RouteConstrained scoredEvidence
	DomainProfile    scoredEvidence
}

type weightedRule struct {
	Token  string
	Weight float64
}

type weightedCandidate struct {
	Value    string
	Positive []weightedRule
	Negative []weightedRule
	Sources  []string
}

type evidenceField struct {
	Name   string
	Weight float64
	Text   string
}

type FragmentContext struct {
	NearbyFragments []*horizonmodels.HorizonFragment
	IncomingEdges   []*horizonmodels.DependencyEdge
	OutgoingEdges   []*horizonmodels.DependencyEdge
	RouteHints      []string
	ZoneHints       []string
	PathHints       []string
}

type candidateAccumulator struct {
	score   float64
	reasons []string
	sources map[string]struct{}
}

type marketProfile struct {
	Domain       string
	Vertical     string
	IndustryPath string
	Positive     []weightedRule
	Negative     []weightedRule
}

type profileAlias struct {
	Positive []weightedRule
	Negative []weightedRule
}

type edgeFacetProfile struct {
	Facet    horizonmodels.DependencyFacet
	Positive []weightedRule
	Negative []weightedRule
}

var domainProfiles = []marketProfile{
	{Domain: "energy", Vertical: "oil-and-gas", IndustryPath: "/industry/energy/hydrocarbons/oil-and-gas/upstream", Positive: []weightedRule{{"oil", 1.3}, {"crude", 1.3}, {"gas", 1.2}, {"lng", 1.8}, {"refinery", 1.8}, {"terminal", 1.0}, {"pipeline", 1.6}, {"petrochemical", 1.4}, {"offtake", 1.0}, {"export", 0.6}}, Negative: []weightedRule{{"software", 0.7}, {"retail", 0.5}}},
	{Domain: "energy", Vertical: "lng", IndustryPath: "/industry/energy/hydrocarbons/lng/lng-infrastructure", Positive: []weightedRule{{"lng", 2.2}, {"liquefaction", 1.8}, {"regasification", 1.8}, {"lng terminal", 1.9}, {"lng train", 1.6}, {"lng export", 1.6}}, Negative: []weightedRule{{"power grid", 0.8}}},
	{Domain: "energy", Vertical: "power-and-utilities", IndustryPath: "/industry/energy/power-and-utilities/power-generation", Positive: []weightedRule{{"grid", 1.4}, {"substation", 1.3}, {"utility", 1.3}, {"power plant", 1.7}, {"generation", 1.0}, {"transmission", 1.2}, {"distribution network", 1.1}, {"electricity", 1.4}}, Negative: []weightedRule{{"bank", 0.5}}},
	{Domain: "energy", Vertical: "chemicals-and-petrochemicals", IndustryPath: "/industry/materials/chemicals/petrochemicals/petrochemical-production", Positive: []weightedRule{{"petrochemical", 1.9}, {"chemical plant", 1.7}, {"ammonia", 1.5}, {"methanol", 1.5}, {"urea", 1.5}, {"ethylene", 1.4}, {"propylene", 1.4}, {"cracker", 1.6}, {"fertilizer", 1.1}}, Negative: []weightedRule{{"software", 0.7}}},
	{Domain: "technology", Vertical: "semiconductors", IndustryPath: "/industry/technology/electronics/semiconductors/semiconductor-manufacturing", Positive: []weightedRule{{"semiconductor", 2.0}, {"wafer", 1.8}, {"foundry", 1.8}, {"chip", 1.4}, {"fab", 2.1}, {"lithography", 1.6}, {"packaging", 1.0}, {"osat", 1.4}}, Negative: []weightedRule{{"fabric", 0.9}, {"grocery", 0.6}}},
	{Domain: "technology", Vertical: "cloud-and-software", IndustryPath: "/industry/technology/software/cloud/cloud-services", Positive: []weightedRule{{"cloud", 1.9}, {"data center", 1.6}, {"datacenter", 1.6}, {"software", 1.5}, {"saas", 1.6}, {"compute", 1.2}, {"server", 1.1}}, Negative: []weightedRule{{"port", 0.5}, {"refinery", 0.7}}},
	{Domain: "technology", Vertical: "electronics-assembly", IndustryPath: "/industry/technology/electronics/electronics-manufacturing/electronics-assembly", Positive: []weightedRule{{"electronics", 1.7}, {"ems", 1.6}, {"pcb", 1.4}, {"assembly", 1.1}, {"connector", 1.0}, {"module", 1.0}, {"component supplier", 1.1}}, Negative: []weightedRule{{"grocery", 0.5}}},
	{Domain: "industrial", Vertical: "shipping", IndustryPath: "/industry/industrial/transportation/maritime/shipping-services", Positive: []weightedRule{{"shipping", 1.8}, {"maritime", 1.7}, {"container", 1.4}, {"vessel", 1.3}, {"fleet", 1.0}, {"bulk carrier", 1.4}, {"charter", 1.0}}, Negative: []weightedRule{{"airline", 0.5}}},
	{Domain: "industrial", Vertical: "logistics", IndustryPath: "/industry/industrial/transportation/logistics/logistics-services", Positive: []weightedRule{{"logistics", 1.9}, {"warehouse", 1.4}, {"distribution center", 1.6}, {"depot", 1.1}, {"freight", 1.2}, {"cargo", 1.0}, {"fulfillment", 1.3}, {"intermodal", 1.4}}, Negative: []weightedRule{{"grocery", 0.4}}},
	{Domain: "industrial", Vertical: "port-services", IndustryPath: "/industry/industrial/transportation/logistics/port-operations", Positive: []weightedRule{{"port", 1.8}, {"seaport", 1.8}, {"harbor", 1.6}, {"harbour", 1.6}, {"terminal", 1.0}, {"berth", 1.2}, {"container yard", 1.3}}, Negative: []weightedRule{{"airport", 0.6}}},
	{Domain: "industrial", Vertical: "aviation", IndustryPath: "/industry/industrial/transportation/aviation/airport-operations", Positive: []weightedRule{{"airport", 1.9}, {"air base", 1.5}, {"cargo terminal", 1.5}, {"runway", 1.3}, {"air freight", 1.5}}, Negative: []weightedRule{{"seaport", 0.8}}},
	{Domain: "industrial", Vertical: "automotive", IndustryPath: "/industry/industrial/manufacturing/automotive/automotive-manufacturing", Positive: []weightedRule{{"automotive", 1.8}, {"vehicle", 1.2}, {"assembly plant", 1.6}, {"gigafactory", 1.3}, {"oem", 1.0}, {"parts plant", 1.2}}, Negative: []weightedRule{{"shipping", 0.4}}},
	{Domain: "industrial", Vertical: "construction-and-building-materials", IndustryPath: "/industry/industrial/construction/building-materials/building-materials-production", Positive: []weightedRule{{"cement", 1.7}, {"clinker", 1.5}, {"building materials", 1.7}, {"construction", 1.2}, {"steel rebar", 1.4}, {"glass plant", 1.3}, {"ceramics", 1.2}}, Negative: []weightedRule{{"software", 0.7}}},
	{Domain: "industrial", Vertical: "aerospace", IndustryPath: "/industry/aerospace-defense/aerospace/aerospace-manufacturing/aerospace-manufacturing", Positive: []weightedRule{{"aerospace", 1.8}, {"airframe", 1.4}, {"aircraft", 1.4}, {"mro", 1.4}, {"engine shop", 1.3}, {"maintenance repair", 1.3}}, Negative: []weightedRule{{"shipping", 0.6}}},
	{Domain: "materials", Vertical: "mining", IndustryPath: "/industry/materials/mining/mining/mining-operations", Positive: []weightedRule{{"mine", 1.9}, {"mining", 1.8}, {"ore", 1.2}, {"concentrator", 1.3}, {"smelter", 1.6}, {"lithium", 1.3}, {"copper", 1.1}, {"nickel", 1.1}}, Negative: []weightedRule{{"software", 0.5}}},
	{Domain: "materials", Vertical: "metals", IndustryPath: "/industry/materials/metals/steel/steel-production", Positive: []weightedRule{{"steel", 1.7}, {"aluminum", 1.6}, {"aluminium", 1.6}, {"smelter", 1.2}, {"rolling mill", 1.4}, {"metals", 1.0}}, Negative: []weightedRule{{"retail", 0.5}}},
	{Domain: "healthcare", Vertical: "pharmaceuticals", IndustryPath: "/industry/healthcare/pharmaceuticals/pharmaceuticals/pharmaceutical-manufacturing", Positive: []weightedRule{{"pharma", 1.9}, {"pharmaceutical", 1.9}, {"api plant", 1.5}, {"biotech", 1.3}, {"vaccine", 1.2}, {"drug", 1.0}}, Negative: []weightedRule{{"warehouse", 0.4}}},
	{Domain: "healthcare", Vertical: "health-services", IndustryPath: "/industry/healthcare/providers/hospitals/hospital-services", Positive: []weightedRule{{"hospital", 2.0}, {"clinic", 1.6}, {"medical center", 1.6}, {"health system", 1.2}}, Negative: []weightedRule{{"factory", 0.4}}},
	{Domain: "telecom", Vertical: "telecommunications", IndustryPath: "/industry/telecom/networks/telecommunications/telecom-services", Positive: []weightedRule{{"telecom", 1.9}, {"fiber", 1.4}, {"broadband", 1.4}, {"mobile network", 1.7}, {"cell tower", 1.4}, {"satellite", 1.1}}, Negative: []weightedRule{{"bank", 0.4}}},
	{Domain: "telecom", Vertical: "network-infrastructure", IndustryPath: "/industry/telecom/infrastructure/network-infrastructure/network-infrastructure", Positive: []weightedRule{{"submarine cable", 1.9}, {"landing station", 1.6}, {"towerco", 1.7}, {"backbone", 1.4}, {"internet exchange", 1.6}, {"ix", 0.8}, {"network infrastructure", 1.6}}, Negative: []weightedRule{{"shipping", 0.6}}},
	{Domain: "finance", Vertical: "banking", IndustryPath: "/industry/finance/banking/commercial-banking/banking-services", Positive: []weightedRule{{"bank", 2.0}, {"lender", 1.3}, {"credit", 1.2}, {"treasury", 1.1}, {"loan", 1.0}}, Negative: []weightedRule{{"river bank", 1.4}, {"blood bank", 1.3}}},
	{Domain: "finance", Vertical: "payments", IndustryPath: "/industry/finance/payments/payments/payment-services", Positive: []weightedRule{{"payment", 2.0}, {"card issuer", 1.5}, {"acquirer", 1.5}, {"merchant services", 1.5}, {"fintech", 1.0}, {"wallet", 1.0}}, Negative: []weightedRule{{"shipment", 0.5}}},
	{Domain: "finance", Vertical: "insurance", IndustryPath: "/industry/finance/insurance/insurance/insurance-services", Positive: []weightedRule{{"insurance", 2.0}, {"insurer", 1.8}, {"reinsurance", 1.8}, {"underwriter", 1.4}}, Negative: []weightedRule{{"manufacturing", 0.4}}},
	{Domain: "finance", Vertical: "market-infrastructure", IndustryPath: "/industry/finance/market-infrastructure/clearing-and-settlement/market-infrastructure", Positive: []weightedRule{{"exchange", 1.6}, {"clearing", 1.8}, {"settlement", 1.8}, {"custody", 1.4}, {"central securities depository", 1.8}, {"ccp", 1.4}, {"trade finance", 1.4}}, Negative: []weightedRule{{"shipping exchange", 0.8}}},
	{Domain: "consumer", Vertical: "retail", IndustryPath: "/industry/consumer/retail/general-retail/retail-operations", Positive: []weightedRule{{"retail", 1.9}, {"store", 1.2}, {"supermarket", 1.8}, {"hypermarket", 1.8}, {"grocery", 1.7}, {"shopping center", 1.4}}, Negative: []weightedRule{{"data store", 0.9}, {"port", 0.5}}},
	{Domain: "consumer", Vertical: "ecommerce", IndustryPath: "/industry/consumer/retail/ecommerce/ecommerce-operations", Positive: []weightedRule{{"ecommerce", 2.0}, {"e-commerce", 2.0}, {"marketplace", 1.5}, {"fulfillment", 1.2}, {"last mile", 1.0}}, Negative: []weightedRule{{"capital markets", 0.8}}},
	{Domain: "consumer", Vertical: "hospitality-and-leisure", IndustryPath: "/industry/consumer/services/hospitality/hospitality-services", Positive: []weightedRule{{"hotel", 1.8}, {"resort", 1.7}, {"hospitality", 1.8}, {"restaurant", 1.2}, {"leisure", 1.0}, {"casino", 1.2}}, Negative: []weightedRule{{"data center", 0.8}}},
	{Domain: "agri-food", Vertical: "agriculture", IndustryPath: "/industry/agriculture/farming/agriculture/agricultural-production", Positive: []weightedRule{{"agriculture", 1.9}, {"farm", 1.5}, {"crop", 1.3}, {"grain", 1.3}, {"fertilizer", 1.6}, {"seed", 1.0}}, Negative: []weightedRule{{"seed round", 1.2}}},
	{Domain: "agri-food", Vertical: "food-processing", IndustryPath: "/industry/agriculture/food-processing/food-processing/food-manufacturing", Positive: []weightedRule{{"food", 1.2}, {"dairy", 1.4}, {"meat", 1.2}, {"processing plant", 1.3}, {"milling", 1.2}, {"beverage", 1.3}}, Negative: []weightedRule{{"data processing", 1.2}}},
	{Domain: "defense", Vertical: "defense-industrial", IndustryPath: "/industry/aerospace-defense/defense/defense-industrial/defense-manufacturing", Positive: []weightedRule{{"defense", 1.8}, {"naval base", 1.5}, {"air base", 1.3}, {"arsenal", 1.3}, {"missile", 1.0}, {"munitions", 1.3}, {"anti-drone", 1.0}, {"ordnance", 1.2}}, Negative: []weightedRule{{"civil defense", 0.7}}},
	{Domain: "real-estate", Vertical: "commercial-real-estate", IndustryPath: "/industry/real-estate/commercial/commercial-real-estate/property-operations", Positive: []weightedRule{{"real estate", 1.8}, {"property", 1.0}, {"commercial park", 1.2}, {"industrial park", 1.1}, {"reit", 1.5}}, Negative: []weightedRule{{"estate tax", 1.0}}},
	{Domain: "professional-services", Vertical: "business-services", IndustryPath: "/industry/services/professional-services/business-services/business-services", Positive: []weightedRule{{"consulting", 1.6}, {"audit", 1.5}, {"outsourcing", 1.5}, {"verification", 1.4}, {"inspection", 1.3}, {"certification", 1.4}}, Negative: []weightedRule{{"factory", 0.6}}},
	{Domain: "public-sector", Vertical: "government-services", IndustryPath: "/industry/public-sector/government-services/public-administration/public-administration", Positive: []weightedRule{{"public authority", 1.5}, {"municipal", 1.3}, {"government service", 1.6}, {"customs", 1.6}, {"tax authority", 1.6}}, Negative: []weightedRule{{"commercial bank", 0.7}}},
	{Domain: "education", Vertical: "education-services", IndustryPath: "/industry/services/education/education-services/education-services", Positive: []weightedRule{{"university", 1.8}, {"college", 1.7}, {"school", 1.5}, {"education", 1.4}, {"training center", 1.2}}, Negative: []weightedRule{{"school bus", 0.8}}},
	{Domain: "media", Vertical: "media-and-broadcasting", IndustryPath: "/industry/media/broadcasting/media-services/media-services", Positive: []weightedRule{{"media", 1.5}, {"broadcast", 1.7}, {"publisher", 1.4}, {"newsroom", 1.2}, {"streaming", 1.3}}, Negative: []weightedRule{{"medical", 0.8}}},
}

var domainProfileAliases = map[string]profileAlias{
	"energy/oil-and-gas": {
		Positive: []weightedRule{{"petroleo", 1.3}, {"petroleos", 1.3}, {"gaz", 1.1}, {"gNL", 1.7}, {"raffinerie", 1.6}, {"raffineria", 1.6}, {"нефть", 1.2}, {"газ", 1.2}, {"спг", 1.8}, {"нпз", 1.7}, {"терминал", 0.8}, {"трубопровод", 1.4}},
	},
	"energy/lng": {
		Positive: []weightedRule{{"gas natural licuado", 1.9}, {"gaz naturel liquefie", 1.9}, {"regasificacion", 1.7}, {"regazeification", 1.7}, {"сжиженный природный газ", 2.0}, {"регазификация", 1.7}},
	},
	"energy/power-and-utilities": {
		Positive: []weightedRule{{"centrale electrique", 1.5}, {"red electrica", 1.5}, {"subestacion", 1.4}, {"электростанция", 1.6}, {"подстанция", 1.4}, {"электросеть", 1.3}},
	},
	"energy/chemicals-and-petrochemicals": {
		Positive: []weightedRule{{"petrochimie", 1.8}, {"petroquimica", 1.8}, {"refineria", 0.8}, {"аммиак", 1.3}, {"метанол", 1.3}, {"карбамид", 1.3}, {"удобрение", 1.1}},
	},
	"technology/semiconductors": {
		Positive: []weightedRule{{"semiconducteur", 1.8}, {"semiconductor fab", 1.8}, {"fundicion", 1.6}, {"oblea", 1.4}, {"полупроводник", 1.8}, {"фаб", 1.9}, {"микросхем", 1.3}, {"литография", 1.4}},
	},
	"technology/electronics-assembly": {
		Positive: []weightedRule{{"ensamblaje", 1.2}, {"assemblage electronique", 1.4}, {"ems provider", 1.4}, {"контрактная сборка", 1.4}, {"электронный модуль", 1.0}},
	},
	"technology/cloud-and-software": {
		Positive: []weightedRule{{"centre de donnees", 1.5}, {"centro de datos", 1.5}, {"облачн", 1.4}, {"дата-центр", 1.4}, {"дата центр", 1.4}},
	},
	"industrial/shipping": {
		Positive: []weightedRule{{"maritimo", 1.5}, {"maritime", 0.6}, {"naviera", 1.5}, {"судоход", 1.5}, {"контейнеровоз", 1.3}, {"перевозчик", 0.9}},
	},
	"industrial/logistics": {
		Positive: []weightedRule{{"logistique", 1.7}, {"logistica", 1.7}, {"entrepot", 1.3}, {"almacen", 1.2}, {"склад", 1.3}, {"распределительный центр", 1.5}, {"фулфилмент", 1.2}},
	},
	"industrial/port-services": {
		Positive: []weightedRule{{"puerto", 1.7}, {"porto", 1.7}, {"port authority", 1.2}, {"autoridad portuaria", 1.6}, {"harbor master", 1.1}, {"порт", 1.7}, {"морской порт", 1.8}, {"гавань", 1.3}},
	},
	"industrial/aviation": {
		Positive: []weightedRule{{"aeropuerto", 1.8}, {"aeroport", 1.8}, {"terminal de carga", 1.5}, {"airport cargo", 1.3}, {"аэропорт", 1.8}, {"грузовой терминал", 1.4}},
	},
	"industrial/aerospace": {
		Positive: []weightedRule{{"aeronautica", 1.6}, {"maintenance repair overhaul", 1.5}, {"авиастроение", 1.6}, {"ремонт авиационной техники", 1.4}},
	},
	"materials/mining": {
		Positive: []weightedRule{{"mina", 1.8}, {"mina de cobre", 1.6}, {"mine site", 1.3}, {"рудник", 1.8}, {"месторождение", 1.3}, {"обогатительная фабрика", 1.5}},
	},
	"materials/metals": {
		Positive: []weightedRule{{"acier", 1.5}, {"acero", 1.5}, {"aluminio", 1.4}, {"сталелитейный", 1.5}, {"алюминий", 1.4}, {"прокатный стан", 1.3}},
	},
	"healthcare/pharmaceuticals": {
		Positive: []weightedRule{{"фармацевт", 1.8}, {"фарма", 1.6}, {"farmaceutica", 1.7}, {"pharmaceutique", 1.7}, {"вакцин", 1.1}},
	},
	"healthcare/health-services": {
		Positive: []weightedRule{{"hospital", 0.5}, {"clinique", 1.4}, {"clinica", 1.4}, {"медицинский центр", 1.5}, {"больница", 1.8}},
	},
	"telecom/telecommunications": {
		Positive: []weightedRule{{"telecomunicaciones", 1.8}, {"telecommunications", 0.4}, {"fibre", 1.2}, {"волоконно-оптическ", 1.3}, {"сотовая сеть", 1.4}},
	},
	"telecom/network-infrastructure": {
		Positive: []weightedRule{{"cable submarino", 1.8}, {"cable sous-marin", 1.8}, {"landing point", 1.2}, {"интернет-обмен", 1.4}, {"подводный кабель", 1.8}, {"магистральная сеть", 1.3}},
	},
	"finance/banking": {
		Positive: []weightedRule{{"banco", 1.8}, {"banque", 1.8}, {"казначейств", 1.0}, {"кредит", 1.0}, {"банк", 1.8}},
		Negative: []weightedRule{{"river bank", 1.2}},
	},
	"finance/payments": {
		Positive: []weightedRule{{"pagos", 1.8}, {"paiements", 1.8}, {"merchant acquiring", 1.4}, {"платеж", 1.8}, {"эквайринг", 1.5}},
	},
	"finance/insurance": {
		Positive: []weightedRule{{"aseguradora", 1.8}, {"assurance", 1.2}, {"strahov", 1.2}, {"страхован", 1.8}, {"перестрахован", 1.5}},
	},
	"finance/market-infrastructure": {
		Positive: []weightedRule{{"clearing house", 1.6}, {"compensation", 1.4}, {"settlement", 0.4}, {"депозитар", 1.6}, {"клиринг", 1.7}, {"расчетн", 1.3}, {"trade finance", 1.3}},
	},
	"consumer/retail": {
		Positive: []weightedRule{{"retail store", 1.3}, {"supermercado", 1.8}, {"hypermarché", 1.8}, {"magasin", 1.1}, {"ритейл", 1.8}, {"супермаркет", 1.8}},
	},
	"consumer/ecommerce": {
		Positive: []weightedRule{{"comercio electronico", 1.9}, {"commerce electronique", 1.9}, {"marketplace seller", 1.3}, {"электронная коммерция", 1.9}},
	},
	"consumer/hospitality-and-leisure": {
		Positive: []weightedRule{{"hosteleria", 1.6}, {"hotelero", 1.4}, {"restauration", 1.2}, {"гостиница", 1.7}, {"ресторан", 1.1}},
	},
	"agri-food/agriculture": {
		Positive: []weightedRule{{"agricultura", 1.8}, {"agriculture", 0.4}, {"fertilizante", 1.5}, {"зерно", 1.4}, {"фермер", 1.2}, {"удобрени", 1.5}},
	},
	"agri-food/food-processing": {
		Positive: []weightedRule{{"procesamiento de alimentos", 1.5}, {"transformation alimentaire", 1.5}, {"molin", 1.1}, {"пищевое производство", 1.5}, {"холодовая цепь", 1.2}},
	},
	"defense/defense-industrial": {
		Positive: []weightedRule{{"defensa", 1.7}, {"ministerio de defensa", 1.6}, {"arsenal naval", 1.5}, {"военно-морская база", 1.6}, {"министерство обороны", 1.6}, {"оборонн", 1.4}},
	},
	"real-estate/commercial-real-estate": {
		Positive: []weightedRule{{"immobilier", 1.6}, {"inmobiliario", 1.6}, {"reit", 1.2}, {"недвижимость", 1.6}, {"бизнес-парк", 1.1}},
	},
	"professional-services/business-services": {
		Positive: []weightedRule{{"consultoria", 1.5}, {"conseil", 1.4}, {"certificacion", 1.3}, {"сертификац", 1.3}, {"консалтинг", 1.5}},
	},
	"public-sector/government-services": {
		Positive: []weightedRule{{"aduanas", 1.5}, {"douanes", 1.5}, {"customs authority", 1.4}, {"налоговая", 1.6}, {"таможня", 1.6}, {"public authority", 1.1}},
	},
	"education/education-services": {
		Positive: []weightedRule{{"universidad", 1.7}, {"universite", 1.7}, {"школа", 1.4}, {"университет", 1.7}, {"учебный центр", 1.2}},
	},
	"media/media-and-broadcasting": {
		Positive: []weightedRule{{"difusion", 1.4}, {"radiodiffusion", 1.4}, {"издатель", 1.3}, {"телеканал", 1.5}, {"редакция", 1.2}},
	},
}

var geoFeatureProfiles = []weightedCandidate{
	{Value: "strait", Positive: []weightedRule{{"strait", 2.0}, {"hormuz", 1.8}, {"malacca", 1.8}, {"mandeb", 1.8}, {"bosporus", 1.8}}},
	{Value: "canal", Positive: []weightedRule{{"canal", 2.0}, {"suez", 1.6}, {"panama canal", 1.8}}},
	{Value: "pass", Positive: []weightedRule{{"pass", 1.8}, {"mountain pass", 1.6}, {"tunnel", 0.7}}},
	{Value: "border-crossing", Positive: []weightedRule{{"border crossing", 2.0}, {"checkpoint", 1.4}, {"customs post", 1.4}}},
	{Value: "pipeline-corridor", Positive: []weightedRule{{"pipeline", 2.0}, {"corridor", 0.8}, {"gasoducto", 1.5}, {"pipe", 0.7}}, Negative: []weightedRule{{"fabrication", 1.0}}},
	{Value: "airspace", Positive: []weightedRule{{"airspace", 2.0}, {"flight information region", 1.5}}},
	{Value: "port", Positive: []weightedRule{{"port", 1.8}, {"terminal", 0.8}, {"harbor", 1.6}, {"harbour", 1.6}, {"airport", 1.6}, {"seaport", 1.8}}},
	{Value: "city", Positive: []weightedRule{{"city", 1.8}, {"municipality", 1.2}, {"metro", 1.0}}},
	{Value: "region", Positive: []weightedRule{{"province", 1.4}, {"region", 1.6}, {"state", 1.2}, {"oblast", 1.4}, {"governorate", 1.4}}},
	{Value: "country", Positive: []weightedRule{{"country", 1.2}, {"national", 0.7}}},
}

var govTypeProfiles = []weightedCandidate{
	{Value: "ministry", Positive: []weightedRule{{"ministry-of-", 2.0}, {"ministry of ", 2.0}}},
	{Value: "department", Positive: []weightedRule{{"department-of-", 2.0}, {"department of ", 2.0}}},
	{Value: "authority", Positive: []weightedRule{{"authority", 1.8}}},
	{Value: "agency", Positive: []weightedRule{{"agency", 1.8}}},
	{Value: "service", Positive: []weightedRule{{"service", 1.6}}},
	{Value: "bureau", Positive: []weightedRule{{"bureau", 1.8}}},
	{Value: "office", Positive: []weightedRule{{"office", 1.5}}},
	{Value: "commission", Positive: []weightedRule{{"commission", 1.8}}},
	{Value: "council", Positive: []weightedRule{{"council", 1.7}}},
	{Value: "committee", Positive: []weightedRule{{"committee", 1.7}}},
	{Value: "regulator", Positive: []weightedRule{{"regulator", 1.9}, {"regulatory", 1.0}}},
	{Value: "government", Positive: []weightedRule{{"government", 1.4}, {"cabinet", 1.0}, {"state of ", 0.8}}},
}

var edgeFacetProfiles = []edgeFacetProfile{
	{Facet: horizonmodels.FacetLogistics, Positive: []weightedRule{{"transport_via", 1.8}, {"shipping", 1.5}, {"port", 1.3}, {"terminal", 1.0}, {"strait", 1.2}, {"canal", 1.2}, {"pipeline", 1.1}, {"freight", 1.2}, {"route", 1.0}, {"cargo", 1.0}}, Negative: []weightedRule{{"regulator", 0.9}}},
	{Facet: horizonmodels.FacetInfrastructure, Positive: []weightedRule{{"grid", 1.5}, {"substation", 1.3}, {"pipeline", 1.3}, {"terminal", 0.8}, {"network", 1.0}, {"power", 1.1}, {"fiber", 1.0}}, Negative: []weightedRule{{"merchant", 0.8}}},
	{Facet: horizonmodels.FacetSupply, Positive: []weightedRule{{"supplies_to", 1.8}, {"produces", 1.3}, {"extracts_from", 1.3}, {"product", 0.8}, {"feedstock", 1.1}, {"input", 0.8}}, Negative: []weightedRule{{"regulatory", 0.8}}},
	{Facet: horizonmodels.FacetDistribution, Positive: []weightedRule{{"market", 1.2}, {"distribution", 1.4}, {"retail", 1.1}, {"channel", 1.0}, {"merchant", 1.0}}, Negative: []weightedRule{{"pipeline", 0.6}}},
	{Facet: horizonmodels.FacetRegulatory, Positive: []weightedRule{{"regulator", 1.9}, {"/gov/", 1.7}, {"tax", 1.2}, {"license", 1.0}, {"sanction", 1.2}, {"duty", 1.0}, {"vat", 1.2}, {"compliance", 1.1}}, Negative: []weightedRule{{"container", 0.7}}},
	{Facet: horizonmodels.FacetPayment, Positive: []weightedRule{{"payment", 2.0}, {"bank", 1.2}, {"credit", 1.1}, {"invoice", 1.1}, {"settlement", 1.3}}, Negative: []weightedRule{{"pipeline", 0.5}}},
}

func AnalyzeFragmentHeuristics(fragment *horizonmodels.HorizonFragment) fragmentHeuristicReport {
	return AnalyzeFragmentHeuristicsWithContext(fragment, FragmentContext{})
}

func AnalyzeFragmentHeuristicsWithContext(fragment *horizonmodels.HorizonFragment, ctx FragmentContext) fragmentHeuristicReport {
	fields := collectFragmentEvidence(fragment)
	fields = append(fields, collectFragmentContextEvidence(ctx)...)
	report := fragmentHeuristicReport{}
	report.GeoFeatureType = scoreWeightedCandidates(fields, geoFeatureProfiles, fragmentStructuralGeoFeatureBoosts(fragment), 0.15)
	report.GovType = scoreWeightedCandidates(fields, govTypeProfiles, fragmentStructuralGovBoosts(fragment), 0.2)
	report.MarketDomain, report.MarketVertical, report.IndustryPaths = scoreMarketProfiles(fragment, fields)
	report.Chokepoint = scoreChokepoint(fields, report.GeoFeatureType)
	return report
}

func AnalyzeEdgeHeuristics(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) edgeHeuristicReport {
	fields := collectEdgeEvidence(edge, sourcePath, targetPath)
	report := edgeHeuristicReport{}
	report.Facet = scoreEdgeFacet(fields, edge, sourcePath, targetPath)
	report.RouteConstrained = scoreRouteConstrained(edge, sourcePath, targetPath)
	report.DomainProfile = scoreEdgeDomainProfile(fields)
	return report
}

func applyFragmentReport(fragment *horizonmodels.HorizonFragment, report fragmentHeuristicReport) bool {
	if fragment == nil {
		return false
	}
	changed := false
	if strings.TrimSpace(fragment.GeoFeatureType) == "" && report.GeoFeatureType.Value != "" && report.GeoFeatureType.Confidence >= 0.32 {
		fragment.GeoFeatureType = report.GeoFeatureType.Value
		changed = true
	}
	if strings.TrimSpace(fragment.GovType) == "" && report.GovType.Value != "" && report.GovType.Confidence >= 0.34 {
		fragment.GovType = report.GovType.Value
		changed = true
	}
	if strings.TrimSpace(fragment.MarketDomain) == "" && report.MarketDomain.Value != "" && report.MarketDomain.Confidence >= 0.34 {
		fragment.MarketDomain = report.MarketDomain.Value
		changed = true
	}
	if strings.TrimSpace(fragment.MarketVertical) == "" && report.MarketVertical.Value != "" && report.MarketVertical.Confidence >= 0.33 {
		fragment.MarketVertical = report.MarketVertical.Value
		changed = true
	}
	if len(fragment.IndustryPaths) == 0 && len(report.IndustryPaths) > 0 {
		paths := make([]string, 0, len(report.IndustryPaths))
		for _, item := range report.IndustryPaths {
			if item.Value == "" || item.Confidence < 0.34 {
				continue
			}
			paths = append(paths, item.Value)
		}
		paths = horizonmodels.NormalizeIndexPaths(16, paths)
		if len(paths) > 0 {
			fragment.IndustryPaths = paths
			changed = true
		}
	}
	if report.Chokepoint.Value == "true" && report.Chokepoint.Confidence >= 0.38 && !hasTag(fragment.Tags, "chokepoint") {
		fragment.Tags = append(fragment.Tags, "chokepoint")
		changed = true
	}
	if writeFragmentHeuristicReport(fragment, report) {
		changed = true
	}
	return changed
}

func writeFragmentHeuristicReport(fragment *horizonmodels.HorizonFragment, report fragmentHeuristicReport) bool {
	if fragment == nil {
		return false
	}
	if fragment.Qualifiers == nil {
		fragment.Qualifiers = map[string]interface{}{}
	}
	current, _ := fragment.Qualifiers[graphEnrichQualifierKey].(map[string]interface{})
	if current == nil {
		current = map[string]interface{}{}
	}
	next := cloneStringAnyMap(current)
	next["version"] = graphEnrichVersion
	next["fragment"] = map[string]interface{}{
		"geoFeatureType": scoredEvidenceMap(report.GeoFeatureType),
		"govType":        scoredEvidenceMap(report.GovType),
		"marketDomain":   scoredEvidenceMap(report.MarketDomain),
		"marketVertical": scoredEvidenceMap(report.MarketVertical),
		"industryPaths":  scoredPathEvidenceMaps(report.IndustryPaths),
		"chokepoint":     scoredEvidenceMap(report.Chokepoint),
	}
	if fmt.Sprint(current) == fmt.Sprint(next) {
		return false
	}
	fragment.Qualifiers[graphEnrichQualifierKey] = next
	return true
}

func writeEdgeHeuristicReport(edge *horizonmodels.DependencyEdge, report edgeHeuristicReport) bool {
	if edge == nil {
		return false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	current, _ := edge.Qualifiers[graphEnrichQualifierKey].(map[string]interface{})
	if current == nil {
		current = map[string]interface{}{}
	}
	next := cloneStringAnyMap(current)
	next["version"] = graphEnrichVersion
	next["edge"] = map[string]interface{}{
		"facet":            scoredEvidenceMap(report.Facet),
		"routeConstrained": scoredEvidenceMap(report.RouteConstrained),
		"domainProfile":    scoredEvidenceMap(report.DomainProfile),
	}
	if fmt.Sprint(current) == fmt.Sprint(next) {
		return false
	}
	edge.Qualifiers[graphEnrichQualifierKey] = next
	return true
}

func scoredEvidenceMap(in scoredEvidence) map[string]interface{} {
	if in.Value == "" && in.Confidence <= 0 && len(in.Reasons) == 0 && len(in.Competing) == 0 {
		return nil
	}
	out := map[string]interface{}{
		"value":      in.Value,
		"score":      round2(in.Score),
		"confidence": round3(in.Confidence),
		"bucket":     confidenceBucket(in.Confidence),
	}
	if len(in.Reasons) > 0 {
		out["reasons"] = in.Reasons
	}
	if len(in.EvidenceSources) > 0 {
		out["sources"] = in.EvidenceSources
	}
	if len(in.Competing) > 0 {
		out["competing"] = in.Competing
	}
	return out
}

func scoredPathEvidenceMaps(in []scoredPathEvidence) []map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(in))
	for _, item := range in {
		out = append(out, map[string]interface{}{
			"value":      item.Value,
			"score":      round2(item.Score),
			"confidence": round3(item.Confidence),
			"bucket":     confidenceBucket(item.Confidence),
			"reasons":    item.Reasons,
			"sources":    item.EvidenceSources,
		})
	}
	return out
}

func collectFragmentEvidence(fragment *horizonmodels.HorizonFragment) []evidenceField {
	if fragment == nil {
		return nil
	}
	fields := make([]evidenceField, 0, 24)
	appendField := func(name string, weight float64, values ...string) {
		joined := strings.ToLower(strings.TrimSpace(strings.Join(values, " ")))
		if joined == "" {
			return
		}
		fields = append(fields, evidenceField{Name: name, Weight: weight, Text: joined})
	}
	appendField("canonical_path", 1.6, fragment.CanonicalPath)
	appendField("description", 1.2, fragment.Description)
	for lang, label := range fragment.Labels {
		appendField("label:"+lang, 1.5, label)
	}
	appendField("tags", 1.0, strings.Join(fragment.Tags, " "))
	appendField("entity_path", 1.0, fragment.EntityPath)
	appendField("sector_path", 1.0, fragment.SectorPath)
	appendField("parent_path", 0.9, fragment.ParentPath, fragment.ParentGovPath, fragment.ParentMarketPath)
	appendField("market_segments", 1.3, strings.Join(fragment.MarketSegments, " "))
	appendField("market_norm_domains", 1.3, strings.Join(fragment.MarketSegmentNormDomains, " "))
	appendField("market_norm_verticals", 1.3, strings.Join(fragment.MarketSegmentNormVerticals, " "))
	appendField("industry_paths", 1.25, strings.Join(fragment.IndustryPaths, " "))
	appendField("sphere_paths", 0.9, strings.Join(fragment.SpherePaths, " "))
	appendField("index_paths", 0.85, strings.Join(fragment.IndexPaths, " "))
	if fragment.HomeLocation != nil {
		appendField("home_location", 0.9,
			fragment.HomeLocation.Name,
			valueOrEmpty(fragment.HomeLocation.ISO3166_1),
			valueOrEmpty(fragment.HomeLocation.ISO3166_2),
			valueOrEmpty(fragment.HomeLocation.ISO6709),
		)
	}
	if fragment.Coordinates != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)), "/loc/") {
		appendField("geo_presence", 0.2, "coordinates")
	}
	if fragment.Identifiers != nil {
		appendField("identifiers", 0.8,
			fragment.Identifiers["facilityKind"],
			fragment.Identifiers["facilityType"],
			fragment.Identifiers["supplyChainRole"],
			fragment.Identifiers["marketDomain"],
			fragment.Identifiers["marketVertical"],
		)
	}
	if fragment.Type == horizonmodels.FragmentTypeSupplyLink {
		appendField("supply_refs", 1.2,
			valueOrEmpty(fragment.SupplierRef),
			valueOrEmpty(fragment.CustomerRef),
			valueOrEmpty(fragment.ProductRef),
			valueOrEmpty(fragment.SupplierFacilityRef),
			valueOrEmpty(fragment.CustomerFacilityRef),
			strings.Join(fragment.TransportViaRefs, " "),
		)
	}
	return fields
}

func collectFragmentContextEvidence(ctx FragmentContext) []evidenceField {
	fields := make([]evidenceField, 0, 24)
	appendField := func(name string, weight float64, values ...string) {
		joined := strings.ToLower(strings.TrimSpace(strings.Join(values, " ")))
		if joined == "" {
			return
		}
		fields = append(fields, evidenceField{Name: name, Weight: weight, Text: joined})
	}
	for _, frag := range ctx.NearbyFragments {
		if frag == nil {
			continue
		}
		text := fragmentText(frag)
		switch frag.Type {
		case horizonmodels.FragmentTypeOrganization:
			appendField("nearby_org", 1.2, text, frag.CanonicalPath)
		case horizonmodels.FragmentTypeProduct:
			appendField("nearby_product", 1.25, text, frag.CanonicalPath)
		case horizonmodels.FragmentTypeMarket:
			appendField("nearby_market", 1.0, text, frag.CanonicalPath)
		case horizonmodels.FragmentTypeGeoRegion:
			appendField("nearby_geo", 0.95, text, frag.CanonicalPath)
		case horizonmodels.FragmentTypeAffectedZone:
			appendField("nearby_affected_zone", 0.9, text, frag.CanonicalPath)
		default:
			appendField("nearby_fragment", 0.8, text, frag.CanonicalPath)
		}
	}
	appendEdgeEvidence := func(prefix string, edge *horizonmodels.DependencyEdge) {
		if edge == nil {
			return
		}
		appendField(prefix+"_edge_type", 0.9, string(edge.Type), string(edge.GetFacet()))
		if edge.Qualifiers != nil {
			parts := make([]string, 0, len(edge.Qualifiers)*2)
			for k, v := range edge.Qualifiers {
				parts = append(parts, strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(fmt.Sprint(v))))
			}
			appendField(prefix+"_edge_qualifiers", 0.85, strings.Join(parts, " "))
			appendField(prefix+"_edge_routes", 1.0,
				strings.Join(qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierTransportVia]), " "),
				strings.Join(qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierFlowRefs]), " "),
				strings.Join(qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierZoneRefs]), " "),
				strings.Join(qualifierStringSlice(edge.Qualifiers[horizonmodels.QualifierAffectedZoneRefs]), " "),
			)
		}
	}
	for _, edge := range ctx.IncomingEdges {
		appendEdgeEvidence("incoming", edge)
	}
	for _, edge := range ctx.OutgoingEdges {
		appendEdgeEvidence("outgoing", edge)
	}
	appendField("route_hints", 1.15, strings.Join(ctx.RouteHints, " "))
	appendField("zone_hints", 1.0, strings.Join(ctx.ZoneHints, " "))
	appendField("path_hints", 0.95, strings.Join(ctx.PathHints, " "))
	return fields
}

func effectiveDomainProfiles() []marketProfile {
	out := make([]marketProfile, 0, len(domainProfiles))
	for _, profile := range domainProfiles {
		enriched := profile
		key := profile.Domain + "/" + profile.Vertical
		if alias, ok := domainProfileAliases[key]; ok {
			enriched.Positive = append(append([]weightedRule(nil), profile.Positive...), alias.Positive...)
			enriched.Negative = append(append([]weightedRule(nil), profile.Negative...), alias.Negative...)
		}
		out = append(out, enriched)
	}
	return out
}

func collectEdgeEvidence(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) []evidenceField {
	if edge == nil {
		return nil
	}
	fields := []evidenceField{{Name: "edge_type", Weight: 1.4, Text: strings.ToLower(strings.TrimSpace(string(edge.Type)))}, {Name: "source_path", Weight: 1.1, Text: strings.ToLower(strings.TrimSpace(sourcePath))}, {Name: "target_path", Weight: 1.1, Text: strings.ToLower(strings.TrimSpace(targetPath))}}
	if edge.Qualifiers != nil {
		parts := make([]string, 0, len(edge.Qualifiers)+2)
		for k, v := range edge.Qualifiers {
			parts = append(parts, strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(fmt.Sprint(v))))
		}
		fields = append(fields, evidenceField{Name: "qualifiers", Weight: 1.2, Text: strings.Join(parts, " ")})
		if via := normalizeTransportVia(edge.Qualifiers); len(via) > 0 {
			fields = append(fields, evidenceField{Name: "transport_via", Weight: 1.5, Text: strings.ToLower(strings.Join(via, " "))})
		}
	}
	if edge.Metrics != nil {
		metricParts := make([]string, 0, len(edge.Metrics))
		for k := range edge.Metrics {
			metricParts = append(metricParts, strings.ToLower(strings.TrimSpace(k)))
		}
		fields = append(fields, evidenceField{Name: "metrics", Weight: 0.8, Text: strings.Join(metricParts, " ")})
	}
	return fields
}

func fragmentStructuralGeoFeatureBoosts(fragment *horizonmodels.HorizonFragment) map[string]float64 {
	boosts := map[string]float64{}
	if fragment == nil {
		return boosts
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if strings.HasPrefix(cp, "/loc/") {
		switch depth := geoDepth(cp); {
		case depth >= 4:
			boosts["city"] += 0.8
		case depth == 3:
			boosts["region"] += 0.7
		case depth == 2:
			boosts["country"] += 0.7
		}
	}
	if strings.Contains(cp, "strait-of-") || strings.Contains(cp, "canal") || hasTag(fragment.Tags, "chokepoint") {
		boosts["strait"] += 0.5
		boosts["canal"] += 0.4
	}
	if kind := strings.ToLower(strings.TrimSpace(fragment.Identifiers["facilityKind"])); kind != "" {
		switch kind {
		case "airport":
			boosts["port"] += 0.8
		case "port", "terminal":
			boosts["port"] += 0.9
		case "pipeline", "pipeline-terminal":
			boosts["pipeline-corridor"] += 0.9
		}
	}
	return boosts
}

func fragmentStructuralGovBoosts(fragment *horizonmodels.HorizonFragment) map[string]float64 {
	boosts := map[string]float64{}
	if fragment == nil {
		return boosts
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if strings.HasPrefix(cp, "/gov/") {
		boosts["government"] += 0.2
		if strings.Contains(cp, "/central-bank") || strings.Contains(cp, "/treasury") {
			boosts["regulator"] += 0.5
		}
	}
	return boosts
}

func scoreWeightedCandidates(fields []evidenceField, candidates []weightedCandidate, structuralBoosts map[string]float64, minScore float64) scoredEvidence {
	acc := map[string]*candidateAccumulator{}
	for _, candidate := range candidates {
		acc[candidate.Value] = &candidateAccumulator{sources: map[string]struct{}{}}
	}
	for _, field := range fields {
		text := field.Text
		for _, candidate := range candidates {
			current := acc[candidate.Value]
			for _, rule := range candidate.Positive {
				if rule.Token != "" && strings.Contains(text, strings.ToLower(rule.Token)) {
					current.score += rule.Weight * field.Weight
					current.reasons = append(current.reasons, fmt.Sprintf("%s:%s(+%.2f)", field.Name, rule.Token, rule.Weight*field.Weight))
					current.sources[field.Name] = struct{}{}
				}
			}
			for _, rule := range candidate.Negative {
				if rule.Token != "" && strings.Contains(text, strings.ToLower(rule.Token)) {
					current.score -= rule.Weight * field.Weight
					current.reasons = append(current.reasons, fmt.Sprintf("%s:%s(-%.2f)", field.Name, rule.Token, rule.Weight*field.Weight))
					current.sources[field.Name] = struct{}{}
				}
			}
		}
	}
	for value, bonus := range structuralBoosts {
		if cur := acc[value]; cur != nil && bonus != 0 {
			cur.score += bonus
			cur.reasons = append(cur.reasons, fmt.Sprintf("structural(+%.2f)", bonus))
			cur.sources["structure"] = struct{}{}
		}
	}
	type row struct {
		value string
		score float64
		acc   *candidateAccumulator
	}
	rows := make([]row, 0, len(acc))
	for value, item := range acc {
		if item.score > minScore {
			rows = append(rows, row{value: value, score: item.score, acc: item})
		}
	}
	if len(rows) == 0 {
		return scoredEvidence{}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	top := rows[0]
	runnerUp := 0.0
	if len(rows) > 1 {
		runnerUp = rows[1].score
	}
	conf := boundedConfidence(top.score, runnerUp)
	competing := make([]string, 0, 2)
	for _, item := range rows[1:minInt(len(rows), 3)] {
		competing = append(competing, item.value)
	}
	return scoredEvidence{
		Value:           top.value,
		Score:           top.score,
		Confidence:      conf,
		Reasons:         top.acc.reasons[:minInt(len(top.acc.reasons), 6)],
		EvidenceSources: mapKeysSorted(top.acc.sources),
		Competing:       competing,
	}
}

func scoreMarketProfiles(fragment *horizonmodels.HorizonFragment, fields []evidenceField) (scoredEvidence, scoredEvidence, []scoredPathEvidence) {
	profiles := effectiveDomainProfiles()
	acc := map[string]*candidateAccumulator{}
	verticalAcc := map[string]*candidateAccumulator{}
	industryAcc := map[string]*candidateAccumulator{}
	for _, profile := range profiles {
		if _, ok := acc[profile.Domain]; !ok {
			acc[profile.Domain] = &candidateAccumulator{sources: map[string]struct{}{}}
		}
		if _, ok := verticalAcc[profile.Vertical]; !ok {
			verticalAcc[profile.Vertical] = &candidateAccumulator{sources: map[string]struct{}{}}
		}
		if _, ok := industryAcc[profile.IndustryPath]; !ok {
			industryAcc[profile.IndustryPath] = &candidateAccumulator{sources: map[string]struct{}{}}
		}
	}
	for _, field := range fields {
		text := field.Text
		for _, profile := range profiles {
			for _, rule := range profile.Positive {
				if rule.Token != "" && strings.Contains(text, strings.ToLower(rule.Token)) {
					delta := rule.Weight * field.Weight
					acc[profile.Domain].score += delta
					verticalAcc[profile.Vertical].score += delta
					industryAcc[profile.IndustryPath].score += delta
					reason := fmt.Sprintf("%s:%s(+%.2f)", field.Name, rule.Token, delta)
					acc[profile.Domain].reasons = append(acc[profile.Domain].reasons, reason)
					verticalAcc[profile.Vertical].reasons = append(verticalAcc[profile.Vertical].reasons, reason)
					industryAcc[profile.IndustryPath].reasons = append(industryAcc[profile.IndustryPath].reasons, reason)
					acc[profile.Domain].sources[field.Name] = struct{}{}
					verticalAcc[profile.Vertical].sources[field.Name] = struct{}{}
					industryAcc[profile.IndustryPath].sources[field.Name] = struct{}{}
				}
			}
			for _, rule := range profile.Negative {
				if rule.Token != "" && strings.Contains(text, strings.ToLower(rule.Token)) {
					delta := rule.Weight * field.Weight
					acc[profile.Domain].score -= delta
					verticalAcc[profile.Vertical].score -= delta
					industryAcc[profile.IndustryPath].score -= delta
					reason := fmt.Sprintf("%s:%s(-%.2f)", field.Name, rule.Token, delta)
					acc[profile.Domain].reasons = append(acc[profile.Domain].reasons, reason)
					verticalAcc[profile.Vertical].reasons = append(verticalAcc[profile.Vertical].reasons, reason)
					industryAcc[profile.IndustryPath].reasons = append(industryAcc[profile.IndustryPath].reasons, reason)
					acc[profile.Domain].sources[field.Name] = struct{}{}
					verticalAcc[profile.Vertical].sources[field.Name] = struct{}{}
					industryAcc[profile.IndustryPath].sources[field.Name] = struct{}{}
				}
			}
		}
	}
	applyMarketStructuralPriors(fragment, acc, verticalAcc, industryAcc)
	domainResult := topAccumulator(acc, 0.2)
	verticalResult := topAccumulator(verticalAcc, 0.25)
	if domainResult.Value != "" {
		verticalResult = topVerticalForDomain(domainResult.Value, verticalAcc)
	}
	industryResults := topIndustryPaths(industryAcc, 0.4)
	return domainResult, verticalResult, industryResults
}

func applyMarketStructuralPriors(fragment *horizonmodels.HorizonFragment, domains, verticals, industries map[string]*candidateAccumulator) {
	if fragment == nil {
		return
	}
	boost := func(domain, vertical, industry string, score float64, reason string) {
		if domain != "" {
			if cur := domains[domain]; cur != nil {
				cur.score += score
				cur.reasons = append(cur.reasons, reason)
				cur.sources["structure"] = struct{}{}
			}
		}
		if vertical != "" {
			if cur := verticals[vertical]; cur != nil {
				cur.score += score
				cur.reasons = append(cur.reasons, reason)
				cur.sources["structure"] = struct{}{}
			}
		}
		if industry != "" {
			if cur := industries[industry]; cur != nil {
				cur.score += score
				cur.reasons = append(cur.reasons, reason)
				cur.sources["structure"] = struct{}{}
			}
		}
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	text := fragmentText(fragment)
	kind := strings.ToLower(strings.TrimSpace(fragment.Identifiers["facilityKind"]))
	supplyRole := strings.ToLower(strings.TrimSpace(fragment.Identifiers["supplyChainRole"]))
	if strings.HasPrefix(cp, "/market/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 4 {
			boost(parts[2], parts[3], "", 2.2, "market-path(+2.20)")
		}
	}
	for _, seg := range fragment.MarketSegments {
		if norm, ok := taxonomy.NormalizeSegmentPath(seg); ok {
			parts := strings.Split(strings.Trim(norm, "/"), "/")
			if len(parts) >= 4 && parts[0] == "segment" {
				boost(parts[1], parts[2], "", 2.4, "segment-path(+2.40)")
			}
		}
	}
	if strings.Contains(text, "port") && (strings.Contains(text, "shipping") || strings.Contains(text, "maritime") || strings.Contains(text, "container")) {
		boost("industrial", "port-services", "/industry/industrial/transportation/logistics/port-operations", 1.6, "port-shipping-prior(+1.60)")
	}
	if strings.Contains(text, "fab") && (strings.Contains(text, "semiconductor") || strings.Contains(text, "wafer") || strings.Contains(text, "foundry") || strings.Contains(text, "chip")) {
		boost("technology", "semiconductors", "/industry/technology/electronics/semiconductors/semiconductor-manufacturing", 2.0, "fab-semiconductor-prior(+2.00)")
	}
	if strings.Contains(text, "terminal") && (strings.Contains(text, "lng") || strings.Contains(text, "gas") || strings.Contains(text, "crude") || strings.Contains(text, "export")) {
		boost("energy", "oil-and-gas", "/industry/energy/hydrocarbons/oil-and-gas/upstream", 1.5, "terminal-energy-prior(+1.50)")
	}
	if strings.Contains(text, "store") || strings.Contains(text, "supermarket") || strings.Contains(text, "hypermarket") || strings.Contains(text, "grocery") {
		boost("consumer", "retail", "/industry/consumer/retail/general-retail/retail-operations", 1.7, "retail-prior(+1.70)")
	}
	if strings.Contains(text, "bank") || strings.Contains(text, "payment") || strings.Contains(text, "insurer") || strings.Contains(text, "insurance") {
		if strings.Contains(text, "payment") {
			boost("finance", "payments", "/industry/finance/payments/payments/payment-services", 1.9, "finance-payments-prior(+1.90)")
		}
		if strings.Contains(text, "insurer") || strings.Contains(text, "insurance") {
			boost("finance", "insurance", "/industry/finance/insurance/insurance/insurance-services", 1.8, "finance-insurance-prior(+1.80)")
		}
		boost("finance", "banking", "/industry/finance/banking/commercial-banking/banking-services", 1.3, "finance-banking-prior(+1.30)")
	}
	if strings.Contains(text, "warehouse") || strings.Contains(text, "distribution center") || strings.Contains(text, "fulfillment") || supplyRole == "distribution" {
		boost("industrial", "logistics", "/industry/industrial/transportation/logistics/logistics-services", 1.5, "logistics-prior(+1.50)")
	}
	if kind == "airport" || strings.Contains(text, "airport") || strings.Contains(text, "cargo terminal") {
		boost("industrial", "aviation", "/industry/industrial/transportation/aviation/airport-operations", 1.5, "airport-prior(+1.50)")
	}
	if kind == "pipeline" || strings.Contains(text, "pipeline") {
		boost("energy", "oil-and-gas", "/industry/energy/hydrocarbons/oil-and-gas/upstream", 1.3, "pipeline-prior(+1.30)")
	}
	if strings.Contains(text, "mine") || strings.Contains(text, "smelter") || strings.Contains(text, "lithium") || strings.Contains(text, "copper") || strings.Contains(text, "nickel") {
		boost("materials", "mining", "/industry/materials/mining/mining/mining-operations", 1.6, "mining-prior(+1.60)")
	}
	if strings.Contains(text, "farm") || strings.Contains(text, "fertilizer") || strings.Contains(text, "grain") || strings.Contains(text, "food processing") {
		boost("agri-food", "agriculture", "/industry/agriculture/farming/agriculture/agricultural-production", 1.4, "agri-prior(+1.40)")
	}
	if strings.Contains(text, "defense") || strings.Contains(text, "naval base") || strings.Contains(text, "air base") || strings.Contains(text, "arsenal") {
		boost("defense", "defense-industrial", "/industry/aerospace-defense/defense/defense-industrial/defense-manufacturing", 1.5, "defense-prior(+1.50)")
	}
}

func scoreEdgeFacet(fields []evidenceField, edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) scoredEvidence {
	acc := map[string]*candidateAccumulator{}
	for _, profile := range edgeFacetProfiles {
		acc[string(profile.Facet)] = &candidateAccumulator{sources: map[string]struct{}{}}
	}
	for _, field := range fields {
		text := field.Text
		for _, profile := range edgeFacetProfiles {
			cur := acc[string(profile.Facet)]
			for _, rule := range profile.Positive {
				if strings.Contains(text, strings.ToLower(rule.Token)) {
					delta := rule.Weight * field.Weight
					cur.score += delta
					cur.reasons = append(cur.reasons, fmt.Sprintf("%s:%s(+%.2f)", field.Name, rule.Token, delta))
					cur.sources[field.Name] = struct{}{}
				}
			}
			for _, rule := range profile.Negative {
				if strings.Contains(text, strings.ToLower(rule.Token)) {
					delta := rule.Weight * field.Weight
					cur.score -= delta
					cur.reasons = append(cur.reasons, fmt.Sprintf("%s:%s(-%.2f)", field.Name, rule.Token, delta))
					cur.sources[field.Name] = struct{}{}
				}
			}
		}
	}
	if len(normalizeTransportVia(edge.Qualifiers)) > 0 {
		acc[string(horizonmodels.FacetLogistics)].score += 1.1
		acc[string(horizonmodels.FacetLogistics)].reasons = append(acc[string(horizonmodels.FacetLogistics)].reasons, "transport-via(+1.10)")
		acc[string(horizonmodels.FacetLogistics)].sources["structure"] = struct{}{}
	}
	if edge != nil {
		switch edge.Type {
		case horizonmodels.DependencyTypeSuppliesTo, horizonmodels.DependencyTypeProduces, horizonmodels.DependencyTypeExtractsFrom:
			acc[string(horizonmodels.FacetSupply)].score += 1.1
			acc[string(horizonmodels.FacetSupply)].reasons = append(acc[string(horizonmodels.FacetSupply)].reasons, "edge-type-supply(+1.10)")
			acc[string(horizonmodels.FacetSupply)].sources["structure"] = struct{}{}
		case horizonmodels.DependencyTypeDependsOn:
			if strings.Contains(sourcePath+" "+targetPath, "/gov/") {
				acc[string(horizonmodels.FacetRegulatory)].score += 0.7
				acc[string(horizonmodels.FacetRegulatory)].reasons = append(acc[string(horizonmodels.FacetRegulatory)].reasons, "gov-endpoint(+0.70)")
				acc[string(horizonmodels.FacetRegulatory)].sources["structure"] = struct{}{}
			}
		}
	}
	return topAccumulator(acc, 0.2)
}

func scoreRouteConstrained(edge *horizonmodels.DependencyEdge, sourcePath, targetPath string) scoredEvidence {
	score := 0.0
	reasons := make([]string, 0, 6)
	sources := map[string]struct{}{}
	if via := normalizeTransportVia(edge.Qualifiers); len(via) > 0 {
		score += 1.6
		reasons = append(reasons, "transport_via_present(+1.60)")
		sources["transport_via"] = struct{}{}
	}
	text := strings.ToLower(strings.Join([]string{sourcePath, targetPath}, " "))
	for _, token := range []weightedRule{{"strait", 0.9}, {"canal", 0.9}, {"port", 0.5}, {"terminal", 0.5}, {"pipeline", 0.7}, {"airport", 0.5}, {"shipping", 0.7}} {
		if strings.Contains(text, token.Token) {
			score += token.Weight
			reasons = append(reasons, fmt.Sprintf("path:%s(+%.2f)", token.Token, token.Weight))
			sources["paths"] = struct{}{}
		}
	}
	conf := sigmoid(score / 2.3)
	value := "false"
	if score >= 1.2 {
		value = "true"
	}
	return scoredEvidence{Value: value, Score: score, Confidence: conf, Reasons: reasons[:minInt(len(reasons), 6)], EvidenceSources: mapKeysSorted(sources)}
}

func scoreEdgeDomainProfile(fields []evidenceField) scoredEvidence {
	profiles := effectiveDomainProfiles()
	acc := map[string]*candidateAccumulator{}
	for _, profile := range profiles {
		if _, ok := acc[profile.Domain]; !ok {
			acc[profile.Domain] = &candidateAccumulator{sources: map[string]struct{}{}}
		}
	}
	for _, field := range fields {
		for _, profile := range profiles {
			for _, rule := range profile.Positive {
				if strings.Contains(field.Text, strings.ToLower(rule.Token)) {
					cur := acc[profile.Domain]
					delta := 0.5 * rule.Weight * field.Weight
					cur.score += delta
					cur.reasons = append(cur.reasons, fmt.Sprintf("%s:%s(+%.2f)", field.Name, rule.Token, delta))
					cur.sources[field.Name] = struct{}{}
				}
			}
		}
	}
	return topAccumulator(acc, 0.1)
}

func scoreChokepoint(fields []evidenceField, geo scoredEvidence) scoredEvidence {
	score := 0.0
	reasons := make([]string, 0, 6)
	sources := map[string]struct{}{}
	if geo.Value == "strait" || geo.Value == "canal" || geo.Value == "pass" || geo.Value == "pipeline-corridor" {
		score += 1.6
		reasons = append(reasons, "geo-feature-chokepoint(+1.60)")
		sources["geoFeatureType"] = struct{}{}
	}
	for _, field := range fields {
		for _, token := range []weightedRule{{"hormuz", 1.0}, {"mandeb", 1.0}, {"malacca", 1.0}, {"suez", 1.0}, {"bosporus", 1.0}, {"canal", 0.8}, {"strait", 0.8}, {"pass", 0.6}} {
			if strings.Contains(field.Text, token.Token) {
				score += token.Weight * field.Weight
				reasons = append(reasons, fmt.Sprintf("%s:%s(+%.2f)", field.Name, token.Token, token.Weight*field.Weight))
				sources[field.Name] = struct{}{}
			}
		}
	}
	value := "false"
	if score >= 1.2 {
		value = "true"
	}
	return scoredEvidence{Value: value, Score: score, Confidence: sigmoid(score / 2.0), Reasons: reasons[:minInt(len(reasons), 6)], EvidenceSources: mapKeysSorted(sources)}
}

func topAccumulator(acc map[string]*candidateAccumulator, minScore float64) scoredEvidence {
	type row struct {
		value string
		score float64
		acc   *candidateAccumulator
	}
	rows := make([]row, 0, len(acc))
	for value, item := range acc {
		if item.score > minScore {
			rows = append(rows, row{value: value, score: item.score, acc: item})
		}
	}
	if len(rows) == 0 {
		return scoredEvidence{}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	runnerUp := 0.0
	if len(rows) > 1 {
		runnerUp = rows[1].score
	}
	competing := make([]string, 0, 2)
	for _, item := range rows[1:minInt(len(rows), 3)] {
		competing = append(competing, item.value)
	}
	return scoredEvidence{Value: rows[0].value, Score: rows[0].score, Confidence: boundedConfidence(rows[0].score, runnerUp), Reasons: rows[0].acc.reasons[:minInt(len(rows[0].acc.reasons), 6)], EvidenceSources: mapKeysSorted(rows[0].acc.sources), Competing: competing}
}

func topVerticalForDomain(domain string, verticalAcc map[string]*candidateAccumulator) scoredEvidence {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return topAccumulator(verticalAcc, 0.25)
	}
	allowed := map[string]struct{}{}
	for _, profile := range effectiveDomainProfiles() {
		if profile.Domain == domain {
			allowed[profile.Vertical] = struct{}{}
		}
	}
	filtered := map[string]*candidateAccumulator{}
	for value, item := range verticalAcc {
		if _, ok := allowed[value]; ok {
			filtered[value] = item
		}
	}
	return topAccumulator(filtered, 0.25)
}

func topIndustryPaths(acc map[string]*candidateAccumulator, minScore float64) []scoredPathEvidence {
	type row struct {
		value string
		score float64
		acc   *candidateAccumulator
	}
	rows := make([]row, 0, len(acc))
	for value, item := range acc {
		if item.score > minScore {
			rows = append(rows, row{value: value, score: item.score, acc: item})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	rows = rows[:minInt(len(rows), 3)]
	out := make([]scoredPathEvidence, 0, len(rows))
	for _, item := range rows {
		out = append(out, scoredPathEvidence{Value: item.value, Score: item.score, Confidence: sigmoid(item.score / 3.0), Reasons: item.acc.reasons[:minInt(len(item.acc.reasons), 5)], EvidenceSources: mapKeysSorted(item.acc.sources)})
	}
	return out
}

func boundedConfidence(top, runnerUp float64) float64 {
	margin := top - runnerUp
	base := sigmoid(top / 3.0)
	sep := sigmoid(margin / 1.3)
	return round3(math.Max(0.05, math.Min(0.99, 0.45*base+0.55*sep)))
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func confidenceBucket(v float64) string {
	switch {
	case v >= 0.82:
		return "high"
	case v >= 0.58:
		return "medium"
	case v > 0:
		return "low"
	default:
		return ""
	}
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mapKeysSorted(in map[string]struct{}) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func qualifierStringSlice(v interface{}) []string {
	switch raw := v.(type) {
	case []string:
		return compactNonEmptyStrings(raw)
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return compactNonEmptyStrings(out)
	default:
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return []string{s}
		}
		return nil
	}
}

func compactNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
