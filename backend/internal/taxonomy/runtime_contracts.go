package taxonomy

// Code-defined runtime contracts.
// These are the authoritative rule sets used by validators/normalizers.

func setOf(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

var builtInAllowedEventTypes = setOf(
	"attack", "cyberattack", "conflict", "cyber", "sanction", "blockade",
	"policy_change", "license_change", "partnership", "acquisition", "release", "deprecation",
	"pricing_change", "availability_change", "capability_change", "protest", "disaster",
	"diplomatic", "economic", "terrorism", "health", "environmental", "military", "crime",
	"infrastructure", "tech", "strike", "election", "warning", "announcement", "incident", "other",
)

// Industry QID mapping contract is code-defined.
// Can be expanded in code as curated mappings become available.
var builtInAllowedIndustryPaths = setOf()
var builtInIndustryWikidataMap = map[string]string{}

var builtInEventTypeSynonyms = map[string]string{
	"drone_attack":    "attack",
	"missile_attack":  "attack",
	"labor_strike":    "strike",
	"ransomware":      "cyberattack",
	"ddos":            "cyberattack",
	"cyber attack":    "cyber",
	"sanctions":       "sanction",
	"regulation":      "policy_change",
	"policy":          "policy_change",
	"licensing":       "license_change",
	"license":         "license_change",
	"launch":          "release",
	"released":        "release",
	"deprecated":      "deprecation",
	"pricing":         "pricing_change",
	"price":           "pricing_change",
	"internet outage": "infrastructure",
	"power outage":    "infrastructure",
	"civil unrest":    "protest",
	"missile launch":  "military",
}

var builtInAllowedSignalKinds = setOf("risk", "opportunity", "trend", "direction")

var builtInSignalTypes = map[string]SignalTypeSpec{
	"risk:internet_outage":     {Kind: "risk", Label: "Internet Outage", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:military_flight":     {Kind: "risk", Label: "Military Flight Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "upstream_amplified"},
	"risk:military_vessel":     {Kind: "risk", Label: "Military Vessel Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "upstream_amplified"},
	"risk:protest":             {Kind: "risk", Label: "Civil Protest Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:ais_disruption":      {Kind: "risk", Label: "AIS Disruption", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
	"risk:satellite_fire":      {Kind: "risk", Label: "Satellite Fire Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:conflict_event":      {Kind: "risk", Label: "Conflict Event", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:aviation_disruption": {Kind: "risk", Label: "Aviation Disruption", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
	"risk:maritime_hazard":     {Kind: "risk", Label: "Maritime Hazard", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
	"risk:climate_anomaly":     {Kind: "risk", Label: "Climate Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:service_outage":      {Kind: "risk", Label: "Service Outage", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	"risk:temporal_anomaly":    {Kind: "risk", Label: "Temporal Baseline Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
}

var builtInSignalTypeSynonyms = map[string]string{
	"internet_outage":     "risk:internet_outage",
	"military_flight":     "risk:military_flight",
	"military_vessel":     "risk:military_vessel",
	"protest":             "risk:protest",
	"ais_disruption":      "risk:ais_disruption",
	"satellite_fire":      "risk:satellite_fire",
	"conflict_event":      "risk:conflict_event",
	"aviation_disruption": "risk:aviation_disruption",
	"maritime_hazard":     "risk:maritime_hazard",
	"climate_anomaly":     "risk:climate_anomaly",
	"service_outage":      "risk:service_outage",
	"temporal_anomaly":    "risk:temporal_anomaly",
}

var builtInProductStageOrder = []string{
	"extraction",
	"refining",
	"smelting",
	"liquefaction",
	"cracking",
	"processing",
	"manufacturing",
	"assembly",
}

var builtInProductCategoriesOrdered = []string{"raw", "component", "finished", "service"}

var builtInProductTypesOrdered = []string{
	"mineral",
	"agricultural",
	"chemical",
	"electronic",
	"mechanical",
	"energy",
	"hydrocarbon",
	"fuel",
	"petrochemical",
	"healthcare",
	"consumer",
	"metal",
	"magnetic",
	"goods", // generic goods (general-cargo, etc.)
}

var builtInProductStagesOrdered = []string{
	"extraction",
	"refining",
	"smelting",
	"liquefaction",
	"cracking",
	"processing",
	"manufacturing",
	"assembly",
	"medical-supplies", // healthcare products
	"delivery",         // logistics stage
	"distribution",     // logistics
	"export",           // trade stage
	"service",          // service subdomain (e.g. healthcare/service)
}

var builtInAllowedProductCategories = setOf(builtInProductCategoriesOrdered...)
var builtInAllowedProductTypes = setOf(builtInProductTypesOrdered...)
var builtInAllowedProductStages = setOf(builtInProductStagesOrdered...)

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ProductContractCategories returns code-defined product categories.
func ProductContractCategories() []string {
	return cloneStrings(builtInProductCategoriesOrdered)
}

// ProductContractTypes returns code-defined product types.
func ProductContractTypes() []string {
	return cloneStrings(builtInProductTypesOrdered)
}

// ProductContractStages returns code-defined product stages.
func ProductContractStages() []string {
	return cloneStrings(builtInProductStagesOrdered)
}

var builtInKnownProducts = map[string]ProductSpec{
	"lithium-ore":         {Category: "raw", Type: "mineral", Stage: "extraction"},
	"lithium-carbonate":   {Category: "raw", Type: "chemical", Stage: "refining"},
	"lithium-hydroxide":   {Category: "raw", Type: "chemical", Stage: "refining"},
	"cobalt-ore":          {Category: "raw", Type: "mineral", Stage: "extraction"},
	"nickel-ore":          {Category: "raw", Type: "mineral", Stage: "extraction"},
	"cathode-material":    {Category: "component", Type: "electronic", Stage: "processing"},
	"battery-cell":        {Category: "component", Type: "electronic", Stage: "manufacturing"},
	"ev-battery-cell":     {Category: "component", Type: "electronic", Stage: "manufacturing"},
	"electric-vehicle":    {Category: "finished", Type: "mechanical", Stage: "assembly"},
	"semiconductor-wafer": {Category: "component", Type: "electronic", Stage: "manufacturing"},
	"power-mosfet":        {Category: "component", Type: "electronic", Stage: "manufacturing"},
	"crude-oil":           {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
	"natural-gas":         {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
	"condensate":          {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
	"ngl":                 {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
	"lng":                 {Category: "raw", Type: "hydrocarbon", Stage: "liquefaction"},
	"gasoline":            {Category: "finished", Type: "fuel", Stage: "refining"},
	"diesel":              {Category: "finished", Type: "fuel", Stage: "refining"},
	"jet-fuel":            {Category: "finished", Type: "fuel", Stage: "refining"},
	"naphtha":             {Category: "component", Type: "petrochemical", Stage: "refining"},
	"ethylene":            {Category: "component", Type: "petrochemical", Stage: "cracking"},
	"propylene":           {Category: "component", Type: "petrochemical", Stage: "cracking"},
	"iron-ore":            {Category: "raw", Type: "mineral", Stage: "extraction"},
	"copper-ore":          {Category: "raw", Type: "mineral", Stage: "extraction"},
	"copper-concentrate":  {Category: "raw", Type: "mineral", Stage: "processing"},
	"refined-copper":      {Category: "component", Type: "mineral", Stage: "refining"},
	"gold-ore":            {Category: "raw", Type: "mineral", Stage: "extraction"},
	"gold-bullion":        {Category: "component", Type: "mineral", Stage: "refining"},
	"rare-earth-ore":      {Category: "raw", Type: "mineral", Stage: "extraction"},
	"neodymium-oxide":     {Category: "component", Type: "mineral", Stage: "refining"},
	"uranium-ore":         {Category: "raw", Type: "mineral", Stage: "extraction"},
	"yellowcake":          {Category: "component", Type: "mineral", Stage: "refining"},
}

var builtInProductSynonyms = map[string]string{
	"li2co3":                   "lithium-carbonate",
	"lithium carbonate":        "lithium-carbonate",
	"battery grade lithium":    "lithium-carbonate",
	"lithium hydroxide":        "lithium-hydroxide",
	"lioh":                     "lithium-hydroxide",
	"lithium ore":              "lithium-ore",
	"spodumene":                "lithium-ore",
	"cobalt ore":               "cobalt-ore",
	"nickel ore":               "nickel-ore",
	"ncm":                      "cathode-material",
	"nca":                      "cathode-material",
	"cathode":                  "cathode-material",
	"battery cell":             "battery-cell",
	"ev battery":               "ev-battery-cell",
	"electric vehicle battery": "ev-battery-cell",
	"power mosfet":             "power-mosfet",
	"mosfet":                   "power-mosfet",
	"wafer":                    "semiconductor-wafer",
	"silicon wafer":            "semiconductor-wafer",
	"ev":                       "electric-vehicle",
	"electric car":             "electric-vehicle",
	"electric vehicle":         "electric-vehicle",
	"oil":                      "crude-oil",
	"petroleum":                "crude-oil",
	"gas":                      "natural-gas",
	"petrol":                   "gasoline",
	"kerosene":                 "jet-fuel",
	"avgas":                    "jet-fuel",
	"iron":                     "iron-ore",
	"copper":                   "copper-ore",
	"gold":                     "gold-ore",
	"u3o8":                     "yellowcake",
	"ree":                      "rare-earth-ore",
}

var builtInAllowedDepositResources = setOf(
	"oil", "lithium-ore", "other",
	"crude-oil", "natural-gas", "oil-gas", "coal", "oil-sands", "shale-oil",
	"copper", "zinc", "nickel", "aluminum", "tin", "lead",
	"gold", "silver", "platinum", "palladium",
	"lithium", "cobalt",
	"iron-ore", "manganese", "chromium",
	"rare-earth", "uranium", "tungsten", "molybdenum",
	"potash", "phosphate", "sulfur",
)

var builtInAllowedCommunityTypes = setOf(
	"trade", "activist", "professional", "criminal", "religious", "ethnic",
	"interest", "dynasty", "tribe", "representatives", "industry-association", "key-community",
)

var builtInAllowedMarketDomains = setOf(
	"finance", "technology", "healthcare", "energy", "materials",
	"industrial", "consumer", "telecom", "real-estate",
	"professional-services", "public-sector", "utilities",
	"transportation", "agriculture", "defense", "education",
	"media", "entertainment", "hospitality", "retail",
	"automotive", "aerospace", "mining", "oil-gas",
	"pharmaceuticals", "chemicals", "construction",
)

var builtInAllowedLifecycleStages = setOf(
	"concept", "planning", "permitting", "financing",
	"construction", "commissioning", "operational", "expansion",
	"suspended", "decommissioning", "closed",
)

var builtInLifecycleStageAliases = map[string]string{
	"exploration":     "concept",
	"appraisal":       "planning",
	"development":     "construction",
	"production":      "operational",
	"shut_in":         "suspended",
	"shut-in":         "suspended",
	"abandonment":     "decommissioning",
	"pre_feasibility": "concept",
	"pre-feasibility": "concept",
	"feasibility":     "planning",
	"startup":         "commissioning",
	"restructuring":   "suspended",
	"liquidation":     "decommissioning",
	"dissolved":       "closed",
	"operating":       "operational",
	"producing":       "operational",
	"abandoned":       "closed",
	"active":          "operational",
	"idle":            "suspended",
	"mothballed":      "suspended",
}

var builtInAllowedEdgeMetrics = map[string][]string{
	"OWNS":            {"ownership_percent", "voting_percent", "voting_rights_percent"},
	"SUBSIDIARY_OF":   {"ownership_percent"},
	"INVESTS_IN":      {"investment_amount", "round_size_total"},
	"SUPPLIES_TO":     {"annual_volume", "contract_value", "contract_duration_years"},
	"PRODUCES":        {"annual_capacity", "annual_output"},
	"CONSUMES":        {"annual_consumption"},
	"EXTRACTS_FROM":   {"annual_extraction"},
	"EMPLOYED_BY":     {"tenure_years", "compensation"},
	"BOARD_MEMBER_OF": {"tenure_years"},
	"OPERATES_IN":     {"market_share"},
	"COMPETES_WITH":   {"relative_market_share"},
}

var builtInAllowedEdgeModalities = setOf("actual", "projected", "historical", "potential")

var builtInAllowedServiceDomains = setOf(
	"it", "financial", "finance", "legal", "consulting", "logistics", "engineering", "media",
	"telecom", "education", "healthcare", "construction", "security", "hr", "real-estate", "other",
)

var builtInAllowedServiceSubdomains = map[string][]string{
	"it":           {"software", "software-development", "cloud", "cloud-services", "hosting", "consulting", "integration", "support", "security", "cybersecurity", "data", "other"},
	"financial":    {"banking", "insurance", "payments", "investment", "audit", "tax", "crypto"},
	"finance":      {"banking", "insurance", "payments", "investment", "audit", "tax", "crypto", "other"},
	"legal":        {"corporate", "litigation", "compliance", "ip", "sanctions", "immigration"},
	"consulting":   {"management", "strategy", "operations", "hr", "marketing"},
	"logistics":    {"freight", "warehousing", "customs", "courier", "cold-chain"},
	"engineering":  {"design", "rd", "testing", "certification", "maintenance"},
	"media":        {"advertising", "pr", "content", "broadcasting", "production"},
	"telecom":      {"mobile", "fixed", "internet", "satellite", "voip"},
	"education":    {"training", "certification", "academic", "other"},
	"healthcare":   {"medical", "pharma", "clinical", "other"},
	"construction": {"building", "civil", "engineering", "other"},
	"security":     {"physical", "cyber", "investigation", "other"},
	"hr":           {"recruitment", "staffing", "payroll", "other"},
	"real-estate":  {"brokerage", "property-management", "other"},
	"other":        {"other"},
}

var builtInKnownServices = map[string]ServiceSpec{
	"custom-software-development": {Domain: "it", Subdomain: "software"},
	"cloud-hosting":               {Domain: "it", Subdomain: "cloud"},
	"saas-platform":               {Domain: "it", Subdomain: "software"},
	"payment-processing":          {Domain: "financial", Subdomain: "payments"},
	"external-audit":              {Domain: "financial", Subdomain: "audit"},
	"sanctions-compliance":        {Domain: "legal", Subdomain: "compliance"},
	"air-freight":                 {Domain: "logistics", Subdomain: "freight"},
	"sea-freight":                 {Domain: "logistics", Subdomain: "freight"},
	"management-consulting":       {Domain: "consulting", Subdomain: "management"},
	"strategy-consulting":         {Domain: "consulting", Subdomain: "strategy"},
}

var builtInServiceSynonyms = map[string]string{
	"software development": "custom-software-development",
	"app development":      "custom-software-development",
	"web hosting":          "cloud-hosting",
	"aws":                  "cloud-hosting",
	"gcp":                  "cloud-hosting",
	"azure":                "cloud-hosting",
	"payment gateway":      "payment-processing",
	"sanctions screening":  "sanctions-compliance",
	"aml":                  "sanctions-compliance",
	"airfreight":           "air-freight",
	"ocean freight":        "sea-freight",
}

var builtInAllowedMetricUnits = setOf(
	"usd", "eur", "rub", "units", "units_per_day", "units_per_year", "days", "hours", "ratio", "index_point", "code",
	"percent", "years", "count",
	"bbl", "mbbl", "mmbbl", "bcf", "tcf", "mcf", "boe", "bpd", "mbpd", "mcf_per_day", "mmcf_per_day",
	"metric_tons", "kt", "mt", "tons", "oz", "moz", "kg", "g", "lbs", "carats",
	"metric_tons_per_year", "oz_per_year", "kt_per_year",
	// Supply chain / energy / batteries
	"gwh", "mwh", "kwh", "gwh_per_year", "mwh_per_year", "kwh_per_year",
	"cells",
)
