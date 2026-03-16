package recommendations

type coverageFamily struct {
	ID      string
	Label   string
	Terms   []string
	MinRefs int
}

type strategicCoverageProfile struct {
	ID                  string
	Version             string
	DomainContractHint  string
	HeadlineTemplate    string
	ReasonTemplate      string
	TriggerTerms        []string
	TriggerPathPrefixes []string
	ExpectedFamilies    []coverageFamily
	MinMatchedFamilies  int
	MinRemoteEndpoints  int
	PriorityHint        string
}

var strategicCoverageProfiles = []strategicCoverageProfile{
	{
		ID:                  "helium_supply_chain_v1",
		Version:             "v1",
		DomainContractHint:  "/domain/v1/supply-chain/product/helium",
		HeadlineTemplate:    "Strategic downstream helium exposure is underlinked",
		ReasonTemplate:      "The graph recognizes helium-linked disruption but does not yet reach enough downstream cryogenic and imaging dependencies.",
		TriggerTerms:        []string{"helium", "ras laffan", "qatarenergy", "cryogenic"},
		TriggerPathPrefixes: []string{"/product/raw/gas/helium", "/product/raw/industrial-gas/helium"},
		ExpectedFamilies: []coverageFamily{
			{ID: "mri_imaging", Label: "MRI / imaging equipment", Terms: []string{"mri", "magnetic resonance imaging", "medical imaging", "scanner"}},
			{ID: "nmr_research", Label: "NMR / lab instrumentation", Terms: []string{"nmr", "spectrometer", "nuclear magnetic resonance"}},
			{ID: "semiconductor", Label: "Semiconductor toolchain", Terms: []string{"semiconductor", "chip", "lithography", "fab"}},
			{ID: "cryogenics", Label: "Cryogenics / low-temperature systems", Terms: []string{"cryogenic", "cryogenics", "cooling system", "superconducting"}},
		},
		MinMatchedFamilies: 2,
		MinRemoteEndpoints: 2,
		PriorityHint:       "critical",
	},
	{
		ID:                  "lng_supply_chain_v1",
		Version:             "v1",
		DomainContractHint:  "/domain/v1/supply-chain/product/lng",
		HeadlineTemplate:    "Strategic LNG downstream exposure is underlinked",
		ReasonTemplate:      "The graph contains LNG disruption signals but does not yet cover enough downstream energy and logistics dependencies.",
		TriggerTerms:        []string{"lng", "liquefied natural gas", "ras laffan", "regasification", "carrier"},
		TriggerPathPrefixes: []string{"/product/raw/hydrocarbon/liquefaction/lng", "/product/finished/fuel/liquefaction/lng"},
		ExpectedFamilies: []coverageFamily{
			{ID: "regasification", Label: "Regasification terminals", Terms: []string{"regasification", "import terminal", "lng terminal"}},
			{ID: "power_generation", Label: "Power generation", Terms: []string{"power plant", "generation", "utility", "electricity"}},
			{ID: "industrial_feedstock", Label: "Industrial feedstock users", Terms: []string{"petrochemical", "fertilizer", "industrial gas", "feedstock"}},
			{ID: "carrier_operators", Label: "Carrier operators", Terms: []string{"lng carrier", "shipping", "tanker", "operator"}},
		},
		MinMatchedFamilies: 2,
		MinRemoteEndpoints: 2,
		PriorityHint:       "critical",
	},
	{
		ID:                  "bab_el_mandeb_chokepoint_v1",
		Version:             "v1",
		DomainContractHint:  "/domain/v1/supply-chain/chokepoint/bab-el-mandeb",
		HeadlineTemplate:    "Strategic Bab el-Mandeb consequences are underlinked",
		ReasonTemplate:      "The graph sees chokepoint disruption but downstream maritime route consequences remain shallow.",
		TriggerTerms:        []string{"bab el-mandeb", "red sea", "southern red sea", "houthi"},
		TriggerPathPrefixes: []string{"/loc/int/na/bab-el-mandeb", "/loc/int/na/red-sea"},
		ExpectedFamilies: []coverageFamily{
			{ID: "suez", Label: "Suez corridor", Terms: []string{"suez", "canal", "mediterranean"}},
			{ID: "container_shipping", Label: "Container shipping", Terms: []string{"container", "liner", "shipping"}},
			{ID: "bulk_tanker", Label: "Bulk / tanker routes", Terms: []string{"bulk", "tanker", "crude", "cargo"}},
			{ID: "maritime_latency", Label: "Maritime latency / route risk", Terms: []string{"latency", "route disruption", "detour", "cape of good hope"}},
		},
		MinMatchedFamilies: 2,
		MinRemoteEndpoints: 2,
		PriorityHint:       "critical",
	},
}
