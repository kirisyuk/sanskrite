package main

import "strings"

const (
	domainOilGas           = "oil-gas"
	domainWholesaleRetail  = "wholesale-retail"
	domainSpaceSatellites  = "space-satellites"
	domainCriticalMaterial = "critical-materials"
)

type casebookPair struct {
	Domain       string
	Chain        string
	FromPath     string
	ToPath       string
	RelationKind string
	Reason       string
}

type casebookSynonym struct {
	Domain        string
	CanonicalPath string
	SynonymPath   string
	Reason        string
}

type casebookDeactivate struct {
	Domain   string
	FromPath string
	ToPath   string
	Reason   string
}

var casebookSynonyms = []casebookSynonym{
	{
		Domain:        domainOilGas,
		CanonicalPath: "/product/raw/gas/extraction/crude-helium",
		SynonymPath:   "/product/finished/mechanical/assembly/crude-helium",
		Reason:        "Normalize legacy assembly-like crude helium path into raw gas extraction canonical path.",
	},
	{
		Domain:        domainWholesaleRetail,
		CanonicalPath: "/product/raw/agricultural/harvesting/bananas",
		SynonymPath:   "/product/finished/mechanical/assembly/bananas",
		Reason:        "Normalize legacy bananas assembly path into harvested agricultural product canonical path.",
	},
	{
		Domain:        domainWholesaleRetail,
		CanonicalPath: "/product/raw/agricultural/processing/paddy-rice",
		SynonymPath:   "/product/finished/mechanical/assembly/paddy-rice",
		Reason:        "Normalize legacy paddy rice assembly path into agricultural commodity canonical path.",
	},
	{
		Domain:        domainWholesaleRetail,
		CanonicalPath: "/product/raw/agricultural/processing/paddy-rice",
		SynonymPath:   "/product/raw/agricultural/extraction/paddy-rice",
		Reason:        "Prefer a single canonical paddy rice commodity path across extraction/processing variants.",
	},
	{
		Domain:        domainWholesaleRetail,
		CanonicalPath: "/product/raw/agricultural/processing/coffee-bean",
		SynonymPath:   "/product/finished/mechanical/assembly/coffee-beans",
		Reason:        "Normalize coffee beans plural assembly path into raw agricultural coffee bean canonical path.",
	},
	{
		Domain:        domainWholesaleRetail,
		CanonicalPath: "/product/raw/agricultural/processing/coffee-bean",
		SynonymPath:   "/product/finished/mechanical/assembly/green-coffee-beans",
		Reason:        "Normalize green coffee beans assembly path into raw agricultural coffee bean canonical path.",
	},
	{
		Domain:        domainCriticalMaterial,
		CanonicalPath: "/product/raw/mineral/extraction/rare-earth-concentrate",
		SynonymPath:   "/product/finished/mechanical/assembly/rare-earth-concentrate",
		Reason:        "Normalize legacy rare-earth concentrate assembly path into mineral concentrate canonical path.",
	},
	{
		Domain:        domainCriticalMaterial,
		CanonicalPath: "/product/raw/mineral/processing/lithium-concentrate",
		SynonymPath:   "/product/finished/mechanical/assembly/lithium-concentrate",
		Reason:        "Normalize legacy lithium concentrate assembly path into mineral processing canonical path.",
	},
	{
		Domain:        domainCriticalMaterial,
		CanonicalPath: "/product/raw/mineral/processing/calcined-spodumene",
		SynonymPath:   "/product/finished/mechanical/assembly/calcined-spodumene",
		Reason:        "Normalize legacy calcined spodumene assembly path into mineral processing canonical path.",
	},
}

var casebookPairs = []casebookPair{
	{
		Domain:       domainOilGas,
		Chain:        "gas-processing",
		FromPath:     "/product/raw/hydrocarbon/extraction/natural-gas",
		ToPath:       "/product/finished/hydrocarbon/processing/sweet-gas",
		RelationKind: "transformation",
		Reason:       "Canonical gas sweetening hop from sour/raw natural gas into treated sweet gas.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "lng",
		FromPath:     "/product/raw/hydrocarbon/extraction/natural-gas",
		ToPath:       "/product/finished/mechanical/assembly/liquefied-natural-gas-lng",
		RelationKind: "transformation",
		Reason:       "Canonical liquefaction hop from natural gas into LNG.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "helium",
		FromPath:     "/product/raw/gas/extraction/crude-helium",
		ToPath:       "/product/finished/mechanical/assembly/helium",
		RelationKind: "transformation",
		Reason:       "Canonical purification hop from crude helium into industrial helium.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "oil-refining",
		FromPath:     "/product/raw/hydrocarbon/extraction/crude-oil",
		ToPath:       "/product/component/petrochemical/refining/naphtha",
		RelationKind: "transformation",
		Reason:       "Canonical refinery output hop from crude oil into naphtha cut.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "oil-refining",
		FromPath:     "/product/raw/hydrocarbon/extraction/crude-oil",
		ToPath:       "/product/finished/fuel/refining/diesel-fuel",
		RelationKind: "transformation",
		Reason:       "Canonical refinery output hop from crude oil into diesel fuel.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "oil-refining",
		FromPath:     "/product/raw/hydrocarbon/extraction/crude-oil",
		ToPath:       "/product/finished/fuel/refining/jet-fuel",
		RelationKind: "transformation",
		Reason:       "Canonical refinery output hop from crude oil into jet fuel.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "petrochemicals",
		FromPath:     "/product/component/petrochemical/refining/naphtha",
		ToPath:       "/product/component/petrochemical/cracking/ethylene",
		RelationKind: "transformation",
		Reason:       "Canonical steam-cracking hop from naphtha into ethylene.",
	},
	{
		Domain:       domainOilGas,
		Chain:        "petrochemicals",
		FromPath:     "/product/component/petrochemical/refining/naphtha",
		ToPath:       "/product/finished/mechanical/assembly/benzene",
		RelationKind: "transformation",
		Reason:       "Canonical aromatics hop from naphtha reformate into benzene.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "fresh-produce",
		FromPath:     "/product/raw/agricultural/harvesting/bananas",
		ToPath:       "/product/finished/agricultural/processing/packed-bananas",
		RelationKind: "transformation",
		Reason:       "Canonical produce packing hop from raw bananas into packed retail bananas.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "coffee",
		FromPath:     "/product/raw/agricultural/processing/coffee-bean",
		ToPath:       "/product/finished/consumer/processing/roasted-coffee",
		RelationKind: "transformation",
		Reason:       "Canonical roasting hop from raw coffee bean commodity into roasted coffee.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "rice",
		FromPath:     "/product/raw/agricultural/processing/paddy-rice",
		ToPath:       "/product/finished/agricultural/processing/brown-rice",
		RelationKind: "transformation",
		Reason:       "Canonical milling hop from paddy rice into brown rice.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "rice",
		FromPath:     "/product/raw/agricultural/processing/paddy-rice",
		ToPath:       "/product/finished/agricultural/processing/milled-rice",
		RelationKind: "transformation",
		Reason:       "Canonical milling hop from paddy rice into milled rice.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "rice",
		FromPath:     "/product/finished/agricultural/processing/jasmine-rice",
		ToPath:       "/product/finished/agricultural/processing/packaged-rice",
		RelationKind: "transformation",
		Reason:       "Canonical retail-packaging hop from processed jasmine rice into packaged rice.",
	},
	{
		Domain:       domainWholesaleRetail,
		Chain:        "beverages",
		FromPath:     "/product/component/goods/manufacturing/pet-preforms",
		ToPath:       "/product/finished/consumer/manufacturing/bottled-water",
		RelationKind: "transformation",
		Reason:       "Canonical packaging/manufacturing hop from PET preforms into bottled water SKU class.",
	},
	{
		Domain:       domainSpaceSatellites,
		Chain:        "launch-systems",
		FromPath:     "/product/finished/mechanical/assembly/liquid-rocket-engines",
		ToPath:       "/product/finished/mechanical/assembly/launch-vehicles",
		RelationKind: "assembly",
		Reason:       "Canonical assembly hop from rocket engine module into launch vehicle.",
	},
	{
		Domain:       domainSpaceSatellites,
		Chain:        "spacecraft",
		FromPath:     "/product/component/mechanical/manufacturing/pressurized-cargo-module",
		ToPath:       "/product/finished/mechanical/assembly/cygnus-spacecraft",
		RelationKind: "assembly",
		Reason:       "Canonical assembly hop from pressurized cargo module into Cygnus spacecraft.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/mineral/extraction/lithium-ore",
		ToPath:       "/product/raw/mineral/processing/lithium-concentrate",
		RelationKind: "transformation",
		Reason:       "Canonical beneficiation hop from lithium ore into lithium concentrate.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/mineral/extraction/lithium-ore",
		ToPath:       "/product/raw/mineral/extraction/spodumene-concentrate",
		RelationKind: "transformation",
		Reason:       "Canonical beneficiation hop from lithium ore into spodumene concentrate.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/mineral/extraction/spodumene-concentrate",
		ToPath:       "/product/raw/mineral/processing/calcined-spodumene",
		RelationKind: "transformation",
		Reason:       "Canonical calcination hop from spodumene concentrate into calcined spodumene.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/mineral/extraction/spodumene-concentrate",
		ToPath:       "/product/raw/chemical/refining/lithium-carbonate",
		RelationKind: "transformation",
		Reason:       "Canonical chemical refining hop from spodumene concentrate into lithium carbonate.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/chemical/refining/lithium-carbonate",
		ToPath:       "/product/raw/chemical/refining/lithium-hydroxide",
		RelationKind: "transformation",
		Reason:       "Canonical conversion hop from lithium carbonate into lithium hydroxide.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/raw/chemical/refining/lithium-hydroxide",
		ToPath:       "/product/component/chemical/processing/pcam",
		RelationKind: "transformation",
		Reason:       "Canonical cathode precursor chemistry hop from lithium hydroxide into pCAM.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/component/chemical/processing/pcam",
		ToPath:       "/product/component/chemical/manufacturing/cathode-active-material",
		RelationKind: "transformation",
		Reason:       "Canonical finishing hop from pCAM into cathode active material.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/component/chemical/manufacturing/cathode-active-material",
		ToPath:       "/product/component/electronic/manufacturing/ev-battery-cells",
		RelationKind: "transformation",
		Reason:       "Canonical electrode-to-cell hop from cathode active material into EV battery cells.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "lithium-battery",
		FromPath:     "/product/component/electronic/manufacturing/ev-battery-cells",
		ToPath:       "/product/component/electronic/assembly/ev-battery-module",
		RelationKind: "assembly",
		Reason:       "Canonical module assembly hop from EV battery cells into EV battery module.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "rare-earths",
		FromPath:     "/product/raw/mineral/extraction/rare-earth-ore",
		ToPath:       "/product/raw/mineral/extraction/rare-earth-concentrate",
		RelationKind: "transformation",
		Reason:       "Canonical beneficiation hop from rare earth ore into rare earth concentrate despite legacy taxonomy placement.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "rare-earths",
		FromPath:     "/product/raw/mineral/extraction/rare-earth-concentrate",
		ToPath:       "/product/finished/mechanical/assembly/neodymium-praseodymium-oxide",
		RelationKind: "transformation",
		Reason:       "Canonical separation/refining hop from rare earth concentrate into NdPr oxide.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "rare-earths",
		FromPath:     "/product/finished/mechanical/assembly/neodymium-praseodymium-oxide",
		ToPath:       "/product/finished/mechanical/assembly/permanent-magnet",
		RelationKind: "transformation",
		Reason:       "Canonical magnet chemistry hop from NdPr oxide into permanent magnet feed/product class.",
	},
	{
		Domain:       domainCriticalMaterial,
		Chain:        "rare-earths",
		FromPath:     "/product/component/magnetic/manufacturing/ndfeb-permanent-magnet",
		ToPath:       "/product/component/magnetic/manufacturing/sintered-ndfeb-magnets",
		RelationKind: "transformation",
		Reason:       "Canonical sintering hop from NdFeB magnet precursor/product into sintered NdFeB magnets.",
	},
}

var casebookDeactivations = []casebookDeactivate{
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/crushed-lithium-ore",
		ToPath:   "/product/raw/chemical/refining/lithium-hydroxide",
		Reason:   "Shortcut skips concentrate and calcination/refining stages; crushed ore is not a one-hop input to lithium hydroxide.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/finished/mechanical/assembly/lithium-concentrate",
		Reason:   "Legacy pair uses a backwards-normalized lithium concentrate path; canonical pair now uses mineral processing lithium concentrate path.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/spodumene-concentrate",
		ToPath:   "/product/finished/mechanical/assembly/lithium",
		Reason:   "Broad lithium output skips canonical refining intermediates and uses a non-specific legacy path.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/spodumene-concentrate",
		ToPath:   "/product/raw/chemical/refining/lithium-hydroxide",
		Reason:   "Shortcut skips the newly canonicalized calcined spodumene stage for the spodumene refining route.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/finished/mechanical/assembly/calcined-spodumene",
		Reason:   "Shortcut skips spodumene concentrate as the nearest canonical beneficiation stage.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/finished/mechanical/assembly/lithium",
		Reason:   "Shortcut is too broad and skips direct concentrate/refining stages.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/raw/chemical/processing/lithium-sulfate-solution",
		Reason:   "Shortcut skips canonical concentrate stage for ore beneficiation.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/raw/chemical/refining/lithium-carbonate",
		Reason:   "Shortcut violates one-hop rule; lithium ore should first resolve to concentrate.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/lithium-ore",
		ToPath:   "/product/raw/chemical/refining/lithium-hydroxide",
		Reason:   "Shortcut violates one-hop rule; lithium hydroxide is downstream of carbonate/concentrate stages.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/spodumene-concentrate",
		ToPath:   "/product/finished/mechanical/assembly/calcined-spodumene",
		Reason:   "Legacy pair uses a backwards-normalized calcined spodumene path; canonical pair now uses mineral processing calcined spodumene path.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/chemical/refining/lithium-carbonate",
		ToPath:   "/product/finished/mechanical/assembly/lithium-ion-battery",
		Reason:   "Shortcut skips cathode precursor, active material, and cell manufacturing stages.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/component/electronic/manufacturing/ev-battery-cells",
		ToPath:   "/product/finished/mechanical/assembly/passenger-vehicle",
		Reason:   "Shortcut skips battery module/pack and vehicle subsystem assembly stages.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/rare-earth-ore",
		ToPath:   "/product/component/mechanical/manufacturing/heavy-mining-equipment",
		Reason:   "Directional garbage; mining equipment is an input to extraction, not a refined rare-earth output.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/raw/mineral/extraction/rare-earth-ore",
		ToPath:   "/product/finished/mechanical/assembly/neodymium-praseodymium-oxide",
		Reason:   "Shortcut skips rare-earth concentrate as the nearest canonical separation stage.",
	},
	{
		Domain:   domainOilGas,
		FromPath: "/product/finished/mechanical/assembly/crude-helium",
		ToPath:   "/product/finished/mechanical/assembly/helium",
		Reason:   "Legacy pair uses an alias-like crude helium path; canonical pair now uses raw gas extraction path.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/mechanical/assembly/bananas",
		ToPath:   "/product/finished/agricultural/processing/packed-bananas",
		Reason:   "Legacy pair uses a backwards-normalized banana source path; canonical pair now uses harvested bananas path.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/agricultural/processing/packed-bananas",
		ToPath:   "/product/raw/agricultural/harvesting/bananas",
		Reason:   "Reverse residual from earlier repair run; packed bananas should not resolve downstream into raw bananas.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/mechanical/assembly/coffee-beans",
		ToPath:   "/product/finished/mechanical/assembly/coffee",
		Reason:   "Legacy pair uses a backwards-normalized coffee bean path; canonical pair now uses raw coffee bean to roasted coffee.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/consumer/processing/roasted-coffee",
		ToPath:   "/product/raw/agricultural/processing/coffee-bean",
		Reason:   "Reverse residual from earlier repair run; roasted coffee should not resolve downstream into raw coffee beans.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/mechanical/assembly/paddy-rice",
		ToPath:   "/product/finished/agricultural/processing/brown-rice",
		Reason:   "Legacy pair uses a backwards-normalized paddy rice path; canonical pair now uses raw agricultural paddy rice path.",
	},
	{
		Domain:   domainWholesaleRetail,
		FromPath: "/product/finished/mechanical/assembly/paddy-rice",
		ToPath:   "/product/finished/agricultural/processing/milled-rice",
		Reason:   "Legacy pair uses a backwards-normalized paddy rice path; canonical pair now uses raw agricultural paddy rice path.",
	},
	{
		Domain:   domainCriticalMaterial,
		FromPath: "/product/finished/mechanical/assembly/rare-earth-concentrate",
		ToPath:   "/product/finished/mechanical/assembly/neodymium-praseodymium-oxide",
		Reason:   "Legacy pair uses a backwards-normalized concentrate path; canonical pair now uses raw mineral concentrate path.",
	},
}

func supportedDomains() []string {
	return []string{
		domainOilGas,
		domainWholesaleRetail,
		domainSpaceSatellites,
		domainCriticalMaterial,
	}
}

func isSupportedDomain(domain string) bool {
	for _, candidate := range supportedDomains() {
		if domain == candidate {
			return true
		}
	}
	return false
}

func parseDomainFilter(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, domain := range strings.Split(raw, ",") {
		domain = strings.TrimSpace(strings.ToLower(domain))
		if domain == "" {
			continue
		}
		out[domain] = struct{}{}
	}
	return out
}

func domainSelected(filter map[string]struct{}, domain string) bool {
	if len(filter) == 0 {
		return true
	}
	_, ok := filter[domain]
	return ok
}
