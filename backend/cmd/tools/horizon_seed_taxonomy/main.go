package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Seeds Horizon taxonomy into Firestore as a single source of truth.
// Usage:
//
//	go run ./cmd/tools/horizon_seed_taxonomy --project <gcp-project> --version v1
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	version := flag.String("version", "v1", "taxonomy version doc id (default v1)")
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	taxRepo := repo.NewHorizonTaxonomyRepository(fsClient.Client())

	now := time.Now().UTC()
	tax := &models.HorizonTaxonomy{
		Version:   *version,
		UpdatedAt: now,
		CanonicalPrefixes: []string{
			"/gov/",
			"/org/",
			"/fac/",
			"/loc/",
			"/person/",
			"/product/",
			"/program/",
			"/market/",
			"/segment/",
			"/event/",
			"/sphere/",
			"/industry/",
			"/media/",
			"/deposit/", // Natural resource deposits
		},
		// Generated from taxonomy.SphereL1Allowlist + SphereL2Map (single source of truth).
		AllowedSpherePaths: taxonomy.AllSpherePaths(),
		SphereSynonyms: map[string]string{
			"ai":                   "/sphere/ai",
			"ml":                   "/sphere/ai",
			"machinelearning":      "/sphere/ai",
			"models":               "/sphere/ai/models",
			"llm":                  "/sphere/ai/models",
			"policy":               "/sphere/policy",
			"regulation":           "/sphere/policy/regulation",
			"geo":                  "/sphere/geopolitics",
			"geopolitical":         "/sphere/geopolitics",
			"geopolitics":          "/sphere/geopolitics",
			"conflict":             "/sphere/geopolitics/conflict",
			"war":                  "/sphere/geopolitics/conflict",
			"diplomacy":            "/sphere/geopolitics/diplomacy",
			"society":              "/sphere/social",
			"socium":               "/sphere/social",
			"social":               "/sphere/social",
			"info":                 "/sphere/social/information",
			"information":          "/sphere/social/information",
			"media":                "/sphere/social/information",
			"disinfo":              "/sphere/social/information/disinformation",
			"misinformation":       "/sphere/social/information/disinformation",
			"propaganda":           "/sphere/social/information/propaganda",
			"censorship":           "/sphere/social/information/censorship",
			"cyber":                "/sphere/security/cyber",
			"cybersecurity":        "/sphere/security/cyber",
			"infosec":              "/sphere/security/cyber",
			"ransomware":           "/sphere/security/cyber",
			"cyber ops":            "/sphere/security/cyber",
			"digital":              "/sphere/technology/digital",
			"it":                   "/sphere/technology/it",
			"tech":                 "/sphere/technology",
			"technology":           "/sphere/technology",
			"semiconductors":       "/sphere/technology/semiconductors",
			"defense":              "/sphere/defense",
			"defence":              "/sphere/defense",
			"logistics":            "/sphere/logistics",
			"supply_chain":         "/sphere/logistics",
			"shipping":             "/sphere/logistics/maritime",
			"maritime":             "/sphere/logistics/maritime",
			"air":                  "/sphere/logistics/air",
			"airfreight":           "/sphere/logistics/air",
			"rail":                 "/sphere/logistics/rail",
			"road":                 "/sphere/logistics/road",
			"trucking":             "/sphere/logistics/road",
			"ports":                "/sphere/logistics/ports",
			"port":                 "/sphere/logistics/ports",
			"energy":               "/sphere/energy",
			"oil":                  "/sphere/energy",
			"gas":                  "/sphere/energy",
			"power":                "/sphere/energy/power",
			"electricity":          "/sphere/energy/power",
			"renewables":           "/sphere/energy/renewables",
			"finance":              "/sphere/finance",
			"markets":              "/sphere/finance",
			"crypto":               "/sphere/finance/crypto",
			"cryptocurrency":       "/sphere/finance/crypto",
			"credit":               "/sphere/finance/credit",
			"liquidity":            "/sphere/finance/liquidity",
			"fx":                   "/sphere/finance/fx",
			"foreign exchange":     "/sphere/finance/fx",
			"insurance":            "/sphere/finance/insurance",
			"ops":                  "/sphere/operations",
			"operations":           "/sphere/operations",
			"procurement":          "/sphere/operations/procurement",
			"manufacturing":        "/sphere/operations/manufacturing",
			"production":           "/sphere/operations/manufacturing",
			"quality":              "/sphere/operations/quality",
			"qa":                   "/sphere/operations/quality",
			"qc":                   "/sphere/operations/quality",
			"safety":               "/sphere/operations/safety",
			"hse":                  "/sphere/operations/safety",
			"ehs":                  "/sphere/operations/safety",
			"reliability":          "/sphere/operations/reliability",
			"capacity":             "/sphere/operations/capacity",
			"throughput":           "/sphere/operations/capacity",
			"maintenance":          "/sphere/operations/maintenance",
			"downtime":             "/sphere/operations/maintenance",
			"continuity":           "/sphere/operations/continuity",
			"bcp":                  "/sphere/operations/continuity",
			"business continuity":  "/sphere/operations/continuity",
			"workforce":            "/sphere/labor",
			"hr":                   "/sphere/labor",
			"labor":                "/sphere/labor",
			"labour":               "/sphere/labor",
			"union":                "/sphere/labor/union",
			"strike":               "/sphere/labor/strikes",
			"strikes":              "/sphere/labor/strikes",
			"security":             "/sphere/security",
			"physical security":    "/sphere/security/physical",
			"crime":                "/sphere/security/crime",
			"theft":                "/sphere/security/crime",
			"terrorism":            "/sphere/security/terrorism",
			"legal":                "/sphere/legal",
			"litigation":           "/sphere/legal/litigation",
			"lawsuit":              "/sphere/legal/litigation",
			"ip":                   "/sphere/legal/ip",
			"patent":               "/sphere/legal/ip",
			"copyright":            "/sphere/legal/ip",
			"compliance":           "/sphere/legal/compliance",
			"aml":                  "/sphere/legal/aml",
			"kyc":                  "/sphere/legal/aml",
			"anti-corruption":      "/sphere/legal/anti-corruption",
			"corruption":           "/sphere/legal/anti-corruption",
			"bribery":              "/sphere/legal/anti-corruption",
			"export controls":      "/sphere/policy/export-controls",
			"export-control":       "/sphere/policy/export-controls",
			"dual use":             "/sphere/policy/export-controls",
			"tariff":               "/sphere/policy/customs",
			"customs":              "/sphere/policy/customs",
			"tax":                  "/sphere/policy/tax",
			"licensing":            "/sphere/policy/licensing",
			"license":              "/sphere/policy/licensing",
			"industrial policy":    "/sphere/policy/industrial-policy",
			"environment":          "/sphere/environment",
			"climate":              "/sphere/environment/climate",
			"carbon":               "/sphere/environment/climate",
			"co2":                  "/sphere/environment/climate",
			"pollution":            "/sphere/environment/pollution",
			"emissions":            "/sphere/environment/pollution",
			"esg":                  "/sphere/environment",
			"sustainability":       "/sphere/environment",
			"governance":           "/sphere/governance",
			"corporate governance": "/sphere/governance/corporate",
			"political governance": "/sphere/governance/political",
			"health":               "/sphere/health",
			"pandemic":             "/sphere/health/pandemic",
			"covid":                "/sphere/health/pandemic",
			"infrastructure":       "/sphere/infrastructure",
			"telecom":              "/sphere/infrastructure/telecom",
			"internet":             "/sphere/infrastructure/telecom",
			"cloud":                "/sphere/infrastructure/cloud",
			"datacenter":           "/sphere/infrastructure/cloud",
			"religious":            "/sphere/social/religious-affairs",
			"education":            "/sphere/social/education",
		},
		AllowedEventTypes: []string{
			"attack",
			"cyberattack",
			"conflict",
			"cyber",
			"sanction",
			"blockade",
			"policy_change",
			"license_change",
			"partnership",
			"acquisition",
			"release",
			"deprecation",
			"pricing_change",
			"availability_change",
			"capability_change",
			"protest",
			"disaster",
			"diplomatic",
			"economic",
			"terrorism",
			"health",
			"environmental",
			"military",
			"crime",
			"infrastructure",
			"tech",
			"strike",
			"election",
			"warning",
			"announcement",
			"incident",
			"other",
		},
		EventTypeSynonyms: map[string]string{
			"drone_attack":    "attack",
			"missile_attack":  "attack",
			"strike":          "strike",
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
		},
		AllowedRelationTypes: []string{
			"SUPPLIES_TO",
			"LOCATED_IN",
			"OWNS",
			"PRODUCES",
			"CONSUMES",
			"OPERATES_IN",
			"REGULATES",
			"COMPETES_WITH",
			"INFLUENCES",
			"DEPENDS_ON",
			"SUBSIDIARY_OF",
			"TRANSFORMS_INTO",
			"EXTRACTS_FROM", // FACILITY → DEPOSIT
			// Market participation
			"PARTICIPATES_IN",
			"DOMINATES",
			// Market hierarchy
			"HAS_MARKET_SEGMENT",
			// Government hierarchy
			"ADMINISTERS",
			// Governance / org-person
			"MANAGES",
			"CONTROLS",
			"EMPLOYED_BY",
			"BOARD_MEMBER_OF",
			"FOUNDED",
			"INVESTS_IN",
			"PARTNERS_WITH",
			"KEY_PERSON",
			// Assurance / oversight
			"ASSURES",
			// OSINT-lite / governance extensions
			"AUDITED_BY",
			"RELATIVE_OF",
			"RELATED_TO",
		},
		AllowedSignalKinds: []string{
			"risk",
			"opportunity",
			"trend",
			"direction",
		},
		SignalTypes: map[string]models.SignalTypeSpecData{
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
		},
		SignalTypeSynonyms: map[string]string{
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
		},
		AllowedMetricUnits: []string{
			// General
			"usd", "eur", "rub", "units", "units_per_day", "days", "hours", "ratio", "index_point", "code",
			// Edge metrics
			"percent", "years", "count",
			// O&G units (volume)
			"bbl", "mbbl", "mmbbl", // barrels
			"bcf", "tcf", "mcf", // cubic feet (gas)
			"boe", // barrels of oil equivalent
			// O&G units (rate)
			"bpd", "mbpd", // barrels per day
			"mcf_per_day", "mmcf_per_day", // cubic feet per day
			// Mining units (mass)
			"metric_tons", "kt", "mt", // metric tons (thousands, millions)
			"tons",      // short tons (US)
			"oz", "moz", // troy ounces (precious metals)
			"kg", "g", "lbs",
			"carats",
			// Mining units (rate)
			"metric_tons_per_year", "oz_per_year", "kt_per_year",
			// NOTE: Currency/index units are pattern-validated (lowercase [a-z0-9_], len 2..32)
			// rather than allowlisted — allows any ticker like "usdt", "inr", "brent", "cpi_ru", etc.
		},

		// Product taxonomy
		AllowedProductCategories: []string{"raw", "component", "finished", "service"},
		AllowedProductTypes: []string{
			"mineral",
			"agricultural",
			"chemical",
			"electronic",
			"mechanical",
			"energy",
			// Common supply-chain product types that appear in extraction logs.
			"metal",
			"magnetic",
			"hydrocarbon",   // crude oil, condensate, NGL
			"fuel",          // refined fuels (gasoline, diesel, jet fuel)
			"petrochemical", // ethylene, propylene, polymers
			"healthcare",    // pharma, medical devices
			"consumer",      // consumer goods
		},
		AllowedProductStages: []string{
			"extraction",
			"refining",
			"smelting", // metallurgical conversion (ore/oxide -> metal/alloy)
			"processing",
			"manufacturing",
			"assembly",
			"liquefaction", // LNG production
			"cracking",     // petrochemical cracking
		},
		ProductStageOrder: []string{
			"extraction",
			"refining",
			"smelting",
			"liquefaction",
			"cracking",
			"processing",
			"manufacturing",
			"assembly",
		},
		KnownProducts: map[string]models.ProductSpecData{
			// Battery materials
			"lithium-ore":       {Category: "raw", Type: "mineral", Stage: "extraction"},
			"lithium-carbonate": {Category: "raw", Type: "chemical", Stage: "refining"},
			"lithium-hydroxide": {Category: "raw", Type: "chemical", Stage: "refining"},
			"cobalt-ore":        {Category: "raw", Type: "mineral", Stage: "extraction"},
			"nickel-ore":        {Category: "raw", Type: "mineral", Stage: "extraction"},
			"cathode-material":  {Category: "component", Type: "electronic", Stage: "processing"},
			"battery-cell":      {Category: "component", Type: "electronic", Stage: "manufacturing"},
			"ev-battery-cell":   {Category: "component", Type: "electronic", Stage: "manufacturing"},
			"electric-vehicle":  {Category: "finished", Type: "mechanical", Stage: "assembly"},
			// Semiconductors
			"semiconductor-wafer": {Category: "component", Type: "electronic", Stage: "manufacturing"},
			// O&G Upstream
			"crude-oil":   {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
			"natural-gas": {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
			"condensate":  {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
			"ngl":         {Category: "raw", Type: "hydrocarbon", Stage: "extraction"},
			// O&G Midstream/Downstream
			"lng":       {Category: "raw", Type: "hydrocarbon", Stage: "liquefaction"},
			"gasoline":  {Category: "finished", Type: "fuel", Stage: "refining"},
			"diesel":    {Category: "finished", Type: "fuel", Stage: "refining"},
			"jet-fuel":  {Category: "finished", Type: "fuel", Stage: "refining"},
			"naphtha":   {Category: "component", Type: "petrochemical", Stage: "refining"},
			"ethylene":  {Category: "component", Type: "petrochemical", Stage: "cracking"},
			"propylene": {Category: "component", Type: "petrochemical", Stage: "cracking"},
			// Mining products
			"iron-ore":           {Category: "raw", Type: "mineral", Stage: "extraction"},
			"copper-ore":         {Category: "raw", Type: "mineral", Stage: "extraction"},
			"copper-concentrate": {Category: "raw", Type: "mineral", Stage: "processing"},
			"refined-copper":     {Category: "component", Type: "mineral", Stage: "refining"},
			"gold-ore":           {Category: "raw", Type: "mineral", Stage: "extraction"},
			"gold-bullion":       {Category: "component", Type: "mineral", Stage: "refining"},
			"rare-earth-ore":     {Category: "raw", Type: "mineral", Stage: "extraction"},
			"neodymium-oxide":    {Category: "component", Type: "mineral", Stage: "refining"},
			"uranium-ore":        {Category: "raw", Type: "mineral", Stage: "extraction"},
			"yellowcake":         {Category: "component", Type: "mineral", Stage: "refining"},
		},
		ProductSynonyms: map[string]string{
			// Battery materials
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
			"wafer":                    "semiconductor-wafer",
			"silicon wafer":            "semiconductor-wafer",
			"ev":                       "electric-vehicle",
			"electric car":             "electric-vehicle",
			"electric vehicle":         "electric-vehicle",
			// O&G
			"oil":       "crude-oil",
			"petroleum": "crude-oil",
			"gas":       "natural-gas",
			"petrol":    "gasoline",
			"kerosene":  "jet-fuel",
			"avgas":     "jet-fuel",
			// Mining
			"iron":   "iron-ore",
			"copper": "copper-ore",
			"gold":   "gold-ore",
			"u3o8":   "yellowcake",
			"ree":    "rare-earth-ore",
		},

		// Industry taxonomy (/industry/...) — v1 bootstrap (curated seed).
		AllowedIndustryPaths: []string{
			"/industry/energy",
			"/industry/energy/oil-and-gas",
			"/industry/energy/oil-and-gas/upstream",
			"/industry/energy/oil-and-gas/midstream",
			"/industry/energy/oil-and-gas/downstream",
			"/industry/energy/power",
			"/industry/energy/renewables",
			"/industry/materials",
			"/industry/materials/mining",
			"/industry/materials/chemicals",
			"/industry/industrials",
			"/industry/industrials/transportation",
			"/industry/industrials/transportation/airlines",
			"/industry/industrials/transportation/rail",
			"/industry/industrials/transportation/maritime",
			"/industry/industrials/aerospace-and-defense",
			"/industry/industrials/construction",
			"/industry/technology",
			"/industry/technology/software",
			"/industry/technology/hardware",
			"/industry/technology/cloud",
			"/industry/technology/semiconductors",
			"/industry/communications",
			"/industry/financials",
			"/industry/financials/banking",
			"/industry/financials/asset-management",
			"/industry/financials/insurance",
			"/industry/consumer",
			"/industry/consumer/retail",
			"/industry/consumer/retail/grocery",
			"/industry/healthcare",
			"/industry/public",
			"/industry/public/government",
			"/industry/public/military",
			"/industry/agriculture",
			"/industry/real-estate",
			"/industry/utilities",
			"/industry/other",
		},
		IndustrySynonyms: map[string]string{
			// Keys are normalized (lowercase + punctuation stripped) by the resolver.
			"oil and gas":                 "/industry/energy/oil-and-gas",
			"petroleum industry":          "/industry/energy/oil-and-gas",
			"natural gas":                 "/industry/energy/oil-and-gas",
			"lng":                         "/industry/energy/oil-and-gas",
			"electricity":                 "/industry/energy/power",
			"power industry":              "/industry/energy/power",
			"renewable energy":            "/industry/energy/renewables",
			"mining":                      "/industry/materials/mining",
			"chemical industry":           "/industry/materials/chemicals",
			"transport":                   "/industry/industrials/transportation",
			"transportation":              "/industry/industrials/transportation",
			"airline industry":            "/industry/industrials/transportation/airlines",
			"rail transport":              "/industry/industrials/transportation/rail",
			"maritime transport":          "/industry/industrials/transportation/maritime",
			"aerospace":                   "/industry/industrials/aerospace-and-defense",
			"defense industry":            "/industry/industrials/aerospace-and-defense",
			"software industry":           "/industry/technology/software",
			"enterprise software":         "/industry/technology/software",
			"semiconductor industry":      "/industry/technology/semiconductors",
			"computer hardware industry":  "/industry/technology/hardware",
			"computer hardware":           "/industry/technology/hardware",
			"peripheral":                  "/industry/technology/hardware",
			"cloud computing":             "/industry/technology/cloud",
			"telecommunications industry": "/industry/communications",
			"telecommunications":          "/industry/communications",
			"banking":                     "/industry/financials/banking",
			"asset management":            "/industry/financials/asset-management",
			"insurance":                   "/industry/financials/insurance",
			"retail":                      "/industry/consumer/retail",
			"grocery store":               "/industry/consumer/retail/grocery",
			"healthcare":                  "/industry/healthcare",
			"government":                  "/industry/public/government",
			"military":                    "/industry/public/military",
			"agriculture":                 "/industry/agriculture",
			"real estate":                 "/industry/real-estate",
			"utilities":                   "/industry/utilities",
			"construction industry":       "/industry/industrials/construction",
			"construction":                "/industry/industrials/construction",
			"petroleum gas industry":      "/industry/energy/oil-and-gas",
		},
		IndustryWikidataMap: map[string]string{
			// Start empty-ish; expand iteratively after reviewing output.
		},

		// Natural resource deposits (/deposit/{resource-slug}/{deposit-slug})
		AllowedDepositResources: []string{
			// Hydrocarbons
			"crude-oil", "natural-gas", "oil-gas", "coal", "oil-sands", "shale-oil",
			// Base metals
			"copper", "zinc", "nickel", "aluminum", "tin", "lead",
			// Precious metals
			"gold", "silver", "platinum", "palladium",
			// Battery metals
			"lithium", "cobalt",
			// Ferrous
			"iron-ore", "manganese", "chromium",
			// Strategic/Rare
			"rare-earth", "uranium", "tungsten", "molybdenum",
			// Industrial minerals
			"potash", "phosphate", "sulfur",
		},

		// Lifecycle stages for FACILITY, DEPOSIT, ORGANIZATION
		AllowedLifecycleStages: []string{
			// Early stages
			"concept",    // pre-feasibility
			"planning",   // feasibility study
			"permitting", // licensing
			"financing",  // fundraising
			// Construction
			"construction",  // development drilling
			"commissioning", // ramp-up
			// Operational
			"operational", // running (default)
			"expansion",   // operating + building additional capacity
			// Stoppage
			"suspended", // care & maintenance, shut-in
			// Closure
			"decommissioning", // abandonment
			"closed",          // dissolved, depleted, demolished
		},
		LifecycleStageAliases: map[string]string{
			// O&G industry
			"exploration": "concept",
			"appraisal":   "planning",
			"development": "construction",
			"production":  "operational",
			"shut_in":     "suspended",
			"shut-in":     "suspended",
			"abandonment": "decommissioning",
			// Mining industry
			"pre_feasibility": "concept",
			"pre-feasibility": "concept",
			"feasibility":     "planning",
			// Organizations
			"startup":       "commissioning",
			"restructuring": "suspended",
			"liquidation":   "decommissioning",
			"dissolved":     "closed",
			// Common
			"operating":  "operational",
			"active":     "operational",
			"idle":       "suspended",
			"mothballed": "suspended",
		},

		// Edge metrics (numerical data on edges)
		AllowedEdgeMetrics: map[string][]string{
			"OWNS":            {"ownership_percent", "voting_percent"},
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
		},
		AllowedEdgeModalities: []string{
			"actual",     // relationship exists and is active (default)
			"projected",  // relationship is planned (target in construction)
			"historical", // relationship existed in the past
			"potential",  // relationship is possible but not confirmed
		},
		KnownProductModels:      map[string]models.ProductModelSpecData{},
		SegmentChannelDomains:   []string{},
		SegmentDomainAliases:    map[string]string{},
		SegmentVerticalAliases:  map[string]string{},
		SegmentVerticalFamilies: map[string]string{},
		SegmentNormRules:        map[string]models.SegmentNormRuleData{},

		// Service taxonomy (/product/service/{domain}/{subdomain}/{slug})
		AllowedServiceDomains: []string{
			"it",           // IT services, software, cloud, hosting
			"financial",    // banking, insurance, payments, investment
			"legal",        // law, compliance, audit, notary
			"consulting",   // management, strategy, advisory
			"logistics",    // transport, warehousing, customs
			"engineering",  // R&D, design, architecture
			"media",        // advertising, PR, content, broadcasting
			"telecom",      // communications, internet, mobile
			"education",    // training, certification, academic
			"healthcare",   // medical, pharma, clinical
			"construction", // building, civil engineering
			"security",     // physical, cyber, investigation
			"hr",           // recruitment, staffing, payroll
			"real-estate",  // brokerage, property management
			"other",        // fallback when domain is unclear
		},
		AllowedServiceSubdomains: map[string][]string{
			"it":           {"software", "cloud", "hosting", "consulting", "integration", "support", "security", "data"},
			"financial":    {"banking", "insurance", "payments", "investment", "audit", "tax", "crypto"},
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
		},
		KnownServices: map[string]models.ServiceSpecData{
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
		},
		ServiceSynonyms: map[string]string{
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
		},
	}

	if err := taxRepo.Upsert(ctx, tax); err != nil {
		log.Fatalf("taxonomy upsert failed: %v", err)
	}

	log.Printf("Seeded horizon taxonomy: project=%s version=%s updatedAt=%s", *projectID, *version, now.Format(time.RFC3339))
}
