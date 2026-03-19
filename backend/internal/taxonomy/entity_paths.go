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

// EntityPathTaxonomy returns all entity path entries for LLM prompts and validation.
// Format: /Category/Type/SubType
func EntityPathTaxonomy() []EntityPathEntry {
	return []EntityPathEntry{
		{"/Organization/Corporation/Public", "Public companies", ""},
		{"/Organization/Corporation/Private", "Private companies", ""},
		{"/Organization/Corporation/StateOwned", "State-owned enterprises (SOE)", ""},
		{"/Organization/Government/Agency", "Regulatory agencies, ministries", ""},
		{"/Organization/Government/Military", "Military organizations", ""},
		{"/Organization/NonProfit/NGO", "Non-profit organizations", ""},
		{"/Organization/International/IGO", "International organizations (UN, WTO, NATO)", ""},
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
		{"/Resource/RawMaterial/Mineral", "Minerals (Lithium, Cobalt)", ""},
		{"/Resource/RawMaterial/Agricultural", "Agricultural products (Wheat, Soy)", ""},
		{"/Resource/RawMaterial/Chemical", "Chemical compounds", ""},
		{"/Resource/Energy/Fossil", "Fossil fuels (Oil, LNG)", ""},
		{"/Resource/Energy/Electricity", "Electricity", ""},
		{"/Resource/Component/Electronic", "Electronic components (Semiconductors)", ""},
		{"/Resource/Component/Mechanical", "Mechanical components", ""},
		{"/Person/PEP", "Politically Exposed Person", ""},
		{"/Person/Executive", "Executive (CEO, CFO)", ""},
		{"/Person/Shareholder", "Shareholder (UBO)", ""},
		{"/Person/Individual", "Individual", ""},
		{"/Geo/Region/Country", "Country (ISO 3166-1 alpha-2: US, KZ, RU)", ""},
		{"/Geo/Region/Subdivision", "Administrative division (ISO 3166-2: US-CA, KZ-75, RU-MOW)", ""},
		{"/Geo/Region/City", "City with ISO6709 coordinates", ""},
		{"/Geo/Region/Metropolitan", "Metropolitan area / Agglomeration", ""},
		{"/Geo/Region", "Region", ""},
		{"/Event", "Events", ""},
		{"/Geo/Zone/Conflict", "Conflict zone", "Military conflict, political tension"},
		{"/Geo/Zone/Disaster", "Disaster zone", "Natural disaster, earthquake, flood affected area"},
		{"/Geo/Zone/Humanitarian", "Humanitarian crisis zone", "Humanitarian crisis area"},
		{"/Deposit/Mineral", "Mineral deposit", ""},
		{"/Deposit/Hydrocarbon", "Oil/gas/coal deposit", ""},
		{"/Deposit/PreciousMetal", "Precious metal deposit", ""},
		{"/Deposit/BatteryMetal", "Battery metal deposit (lithium, cobalt)", ""},
		{"/Deposit/BaseMetal", "Base metal deposit", ""},
		{"/Deposit/FerrousMetal", "Ferrous metal deposit", ""},
		{"/Deposit/StrategicMineral", "Strategic mineral deposit", ""},
		{"/Market/Geo/Global", "Global or international market (/market/global, /market/no, /market/int)", ""},
		{"/Market/Geo/Country", "Country-level market (/market/{iso2}/...)", ""},
		{"/Market/Geo/Regional", "Regional market (/market/brics, /market/gcc, /market/eu, ...)", ""},
		{"/Market/Domain/Industry", "Market segment / industry domain (fallback)", ""},
		{"/Track/Air/Aircraft", "Aircraft (ICAO24, flight callsign)", ""},
		{"/Track/Air/MilitaryAircraft", "Military aircraft", ""},
		{"/Track/Sea/Vessel", "Vessel (MMSI, IMO)", ""},
		{"/Track/Sea/BulkCarrier", "Bulk carrier", ""},
		{"/Track/Sea/Tanker", "Tanker (oil, LNG, chemicals)", ""},
		{"/Track/Ground/Vehicle", "Ground vehicle or convoy", ""},
	}
}

// EntityPathPromptForLLM returns the prompt fragment for entity-path classification.
func EntityPathPromptForLLM(canonicalPath string) string {
	var entityLines []string
	for _, e := range EntityPathTaxonomy() {
		line := fmt.Sprintf("- %s - %s", e.Path, e.Label)
		if e.Description != "" {
			line += " " + e.Description
		}
		entityLines = append(entityLines, line)
	}

	return fmt.Sprintf(`You are a taxonomy classification agent for a risk analysis system. Your task is to classify a canonical path into entityPath according to Core Taxonomy v1.0.

CANONICAL PATH TO CLASSIFY:
%s

TASK: Determine the entityPath for this canonical path based on Core Taxonomy v1.0.

ENTITY TYPES TAXONOMY (entityPath):
The entityPath represents the physical nature of the entity. Format: /Category/Type/SubType

Available categories:
%s

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "entityPath": "/Category/Type/SubType",
  "reasoning": "Brief explanation of the classification"
}

RULES:
1. Analyze the canonical path segments to determine the entity type.
2. Use context clues from the path (e.g. "org/tsmc" suggests Organization/Corporation/Public).
3. If uncertain, choose the most specific matching entityPath from the taxonomy.
4. entityPath must start with "/".
5. Return ONLY valid JSON, no markdown code fences or additional text.

Return the classification as JSON:`,
		canonicalPath,
		strings.Join(entityLines, "\n"))
}

// DefaultEntityPath returns the default entity path.
func DefaultEntityPath() string {
	return "/Organization/Corporation/Public"
}
