package taxonomy

import (
	"fmt"
	"strings"
)

// EntityPathEntry represents a single entity path in the taxonomy.
type EntityPathEntry struct {
	Path        string
	Label       string
	Description string
}

// SectorPathEntry represents a single sector path in the taxonomy.
type SectorPathEntry struct {
	Path        string
	Label       string
	Description string
}

// EntityPathTaxonomy returns all entity path entries for LLM prompts and validation.
// Includes paths from: taxonomy_classifier, scout/taxonomy.go, product_hierarchy_builder.
// Format: /Category/Type/SubType
func EntityPathTaxonomy() []EntityPathEntry {
	return []EntityPathEntry{
		// Organization
		{"/Organization/Corporation/Public", "Public companies", ""},
		{"/Organization/Corporation/Private", "Private companies", ""},
		{"/Organization/Corporation/StateOwned", "State-owned enterprises (SOE)", ""},
		{"/Organization/Government/Agency", "Regulatory agencies, ministries", ""},
		{"/Organization/Government/Military", "Military organizations", ""},
		{"/Organization/NonProfit/NGO", "Non-profit organizations", ""},
		{"/Organization/International/IGO", "International organizations (UN, WTO, NATO)", ""},
		// Infrastructure
		{"/Infrastructure/Logistics/Seaport", "Seaports", ""},
		{"/Infrastructure/Logistics/Airport", "Airports", ""},
		{"/Infrastructure/Logistics/RailHub", "Railway hubs", ""},
		{"/Infrastructure/Logistics/Warehouse", "Warehouses", ""},
		{"/Infrastructure/Energy/PowerPlant/Nuclear", "Nuclear power plants", ""},
		{"/Infrastructure/Energy/PowerPlant/Thermal", "Thermal power plants", ""},
		{"/Infrastructure/Energy/PowerPlant/Renewable", "Renewable energy plants", ""},
		{"/Infrastructure/Energy/Grid/Substation", "Electrical substations", ""},
		{"/Infrastructure/Energy/Pipeline/Oil", "Oil pipelines", ""},
		{"/Infrastructure/Energy/Pipeline/Gas", "Gas pipelines", ""},
		{"/Infrastructure/Manufacturing/Factory", "Factories", ""},
		{"/Infrastructure/Manufacturing/Mine", "Mines", ""},
		{"/Infrastructure/Digital/DataCenter", "Data centers", ""},
		{"/Infrastructure/Digital/InternetExchange", "Internet exchange points", ""},
		// Resource
		{"/Resource/RawMaterial/Mineral", "Minerals (Lithium, Cobalt)", ""},
		{"/Resource/RawMaterial/Agricultural", "Agricultural products (Wheat, Soy)", ""},
		{"/Resource/RawMaterial/Chemical", "Chemical compounds", ""},
		{"/Resource/Energy/Fossil", "Fossil fuels (Oil, LNG)", ""},
		{"/Resource/Energy/Electricity", "Electricity", ""},
		{"/Resource/Component/Electronic", "Electronic components (Semiconductors)", ""},
		{"/Resource/Component/Mechanical", "Mechanical components", ""},
		// Person
		{"/Person/PEP", "Politically Exposed Person", ""},
		{"/Person/Executive", "Executive (CEO, CFO)", ""},
		{"/Person/Shareholder", "Shareholder (UBO)", ""},
		{"/Person/Individual", "Individual", ""},
		// Geo
		{"/Geo/Region/Country", "Country (ISO 3166-1 alpha-2: US, KZ, RU)", ""},
		{"/Geo/Region/Subdivision", "Administrative division (ISO 3166-2: US-CA, KZ-75, RU-MOW)", ""},
		{"/Geo/Region/City", "City with ISO6709 coordinates", ""},
		{"/Geo/Region/Metropolitan", "Metropolitan area / Agglomeration", ""},
		{"/Geo/Region", "Region", ""},
		// Event
		{"/Event", "Events", ""},
		// Affected zone (conflict, disaster, tension)
		{"/Geo/Zone/Conflict", "Conflict zone", "Military conflict, political tension"},
		{"/Geo/Zone/Disaster", "Disaster zone", "Natural disaster, earthquake, flood affected area"},
		{"/Geo/Zone/Humanitarian", "Humanitarian crisis zone", "Humanitarian crisis area"},
		// Deposit (from scout/taxonomy.go inferDepositPath)
		{"/Deposit/Mineral", "Mineral deposit", ""},
		{"/Deposit/Hydrocarbon", "Oil/gas/coal deposit", ""},
		{"/Deposit/PreciousMetal", "Precious metal deposit", ""},
		{"/Deposit/BatteryMetal", "Battery metal deposit (lithium, cobalt)", ""},
		{"/Deposit/BaseMetal", "Base metal deposit", ""},
		{"/Deposit/FerrousMetal", "Ferrous metal deposit", ""},
		{"/Deposit/StrategicMineral", "Strategic mineral deposit", ""},
	}
}

// SectorPathTaxonomy returns all sector path entries for LLM prompts and validation.
// Includes paths from: taxonomy_classifier, scout/taxonomy.go, product_hierarchy_builder.
// Format: /Sector/IndustryGroup/Industry/SubIndustry
func SectorPathTaxonomy() []SectorPathEntry {
	return []SectorPathEntry{
		// Energy
		{"/Energy/Fossil_Fuels/Oil_Gas_Upstream", "Exploration and extraction", ""},
		{"/Energy/Fossil_Fuels/Oil_Gas_Midstream", "Pipelines and storage", ""},
		{"/Energy/Fossil_Fuels/Oil_Gas_Downstream", "Refining", ""},
		{"/Energy/Fossil_Fuels", "Fossil fuels", ""},
		{"/Energy/Renewable/Solar", "Solar energy", ""},
		{"/Energy/Renewable/Wind", "Wind energy", ""},
		{"/Energy/Fossil", "Fossil energy (scout fallback)", ""},
		// Materials
		{"/Materials/Mining/Precious_Metals", "Precious metals", ""},
		{"/Materials/Mining/Industrial_Metals", "Industrial metals (Copper, Aluminum)", ""},
		{"/Materials/Mining/Rare_Earths", "Rare earth elements", ""},
		{"/Materials/Mining", "Mining", ""},
		{"/Materials/Chemicals/Specialty", "Specialty chemicals", ""},
		{"/Materials/Chemicals/Fertilizers", "Fertilizers", ""},
		{"/Materials/Chemicals", "Chemicals", ""},
		{"/Materials/Metals", "Metals (scout fallback)", ""},
		// Industrials
		{"/Industrials/Aerospace_Defense/Defense_Prime", "Defense contractors", ""},
		{"/Industrials/Aerospace_Defense/Components", "Components", ""},
		{"/Industrials/Machinery/Industrial_Automation", "Industrial automation", ""},
		{"/Industrials/Transportation/Marine", "Marine transportation", ""},
		{"/Industrials/Transportation/Air_Freight", "Air freight", ""},
		{"/Industrials/Transportation/Rail", "Railway transportation", ""},
		// Automotive
		{"/Automotive/Vehicles/EV", "Electric vehicles", ""},
		{"/Automotive/Components/Batteries", "Batteries", ""},
		{"/Automotive/Components", "Automotive components", ""},
		// Technology
		{"/Technology/Semiconductors/Design", "Design (Fabless)", ""},
		{"/Technology/Semiconductors/Manufacturing", "Manufacturing (Foundry)", ""},
		{"/Technology/Semiconductors/Equipment", "Equipment (ASML, etc.)", ""},
		{"/Technology/Semiconductors", "Semiconductors", ""},
		{"/Technology/Hardware/Consumer_Electronics", "Consumer electronics", ""},
		{"/Technology/Software/Enterprise", "Enterprise software", ""},
		{"/Technology/Software/Cybersecurity", "Cybersecurity", ""},
		// Healthcare
		{"/Healthcare/Pharma/Biotech", "Biotechnology", ""},
		{"/Healthcare/Pharma/Generics", "Generics", ""},
		{"/Healthcare/Equipment/Medical_Devices", "Medical devices", ""},
		// Financials (Critic checks "Financials/Capital_Markets")
		{"/Financials/Banking/Systemic", "Systemically important banks (G-SIBs)", ""},
		{"/Financials/Banking/Regional", "Regional banks", ""},
		{"/Financials/Insurance/Reinsurance", "Reinsurance", ""},
		{"/Financials/Capital_Markets/Asset_Management", "Asset management", ""},
		{"/Finance/Banking", "Banking (scout fallback)", ""},
		// Public
		{"/Public/Executive/Ministry", "Executive branch / Ministry", ""},
		{"/Public/Legislative/Parliament", "Legislative branch / Parliament", ""},
		{"/Public/Judicial/Court", "Judicial branch / Court", ""},
		{"/Public/Central_Bank", "Central bank", ""},
		// Geography
		{"/Geography", "Geographic regions", ""},
	}
}

// EntitySectorPromptForLLM returns the prompt fragment for taxonomy classification.
// Used by taxonomy_classifier to classify canonicalPath into entityPath and sectorPath.
// Preserves exact output format of the original buildClassificationPrompt.
func EntitySectorPromptForLLM(canonicalPath string) string {
	var entityLines []string
	for _, e := range EntityPathTaxonomy() {
		line := fmt.Sprintf("- %s - %s", e.Path, e.Label)
		if e.Description != "" {
			line += " " + e.Description
		}
		entityLines = append(entityLines, line)
	}

	var sectorLines []string
	for _, s := range SectorPathTaxonomy() {
		line := fmt.Sprintf("- %s - %s", s.Path, s.Label)
		if s.Description != "" {
			line += " " + s.Description
		}
		sectorLines = append(sectorLines, line)
	}

	return fmt.Sprintf(`You are a taxonomy classification agent for a risk analysis system. Your task is to classify a canonical path into entityPath and sectorPath according to Core Taxonomy v1.0.

CANONICAL PATH TO CLASSIFY:
%s

TASK: Determine the entityPath and sectorPath for this canonical path based on Core Taxonomy v1.0.

ENTITY TYPES TAXONOMY (entityPath):
The entityPath represents the physical nature of the entity. Format: /Category/Type/SubType

Available categories:
%s

INDUSTRY & SECTOR TAXONOMY (sectorPath):
The sectorPath represents the industry sector. Format: /Sector/IndustryGroup/Industry/SubIndustry

Available sectors:
%s

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "entityPath": "/Category/Type/SubType",
  "sectorPath": "/Sector/IndustryGroup/Industry/SubIndustry",
  "reasoning": "Brief explanation of the classification"
}

RULES:
1. Analyze the canonical path segments to determine the entity type
2. Use context clues from the path (e.g., "org/tsmc" suggests Organization/Corporation/Public)
3. Infer sector from entity type and path context (e.g., "fab" suggests Technology/Semiconductors/Manufacturing)
4. If uncertain, choose the most specific matching path from the taxonomy
5. Both paths must start with "/"
6. Return ONLY valid JSON, no markdown code fences or additional text

Return the classification as JSON:`,
		canonicalPath,
		strings.Join(entityLines, "\n"),
		strings.Join(sectorLines, "\n"))
}

// DefaultEntityPath returns the default entity path (from scout/taxonomy.go getDefaultEntityPath).
func DefaultEntityPath() string {
	return "/Organization/Corporation/Public"
}

// DefaultSectorPath returns the default sector path (from scout/taxonomy.go getDefaultSectorPath).
func DefaultSectorPath() string {
	return "/Technology/Semiconductors/Manufacturing"
}
