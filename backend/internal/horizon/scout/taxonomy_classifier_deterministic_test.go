package scout

import "testing"

func TestClassifyTaxonomyDeterministic(t *testing.T) {
	tests := []struct {
		name           string
		canonicalPath  string
		wantEntityPath string
		wantSectorPath string
		wantOK         bool
	}{
		// --- GOV ---
		{"gov ministry", "/gov/kz/ministry-of-finance", "/Organization/Government/Agency", "/Public/Executive/Ministry", true},
		{"gov military", "/gov/us/department-of-defense", "/Organization/Government/Military", "/Industrials/Aerospace_Defense/Defense_Prime", true},
		{"gov central bank", "/gov/kz/central-bank", "/Organization/Government/Agency", "/Public/Central_Bank", true},
		{"gov parliament", "/gov/tr/parliament", "/Organization/Government/Agency", "/Public/Legislative/Parliament", true},
		{"gov court", "/gov/us/supreme-court", "/Organization/Government/Agency", "/Public/Judicial/Court", true},

		// --- ORG ---
		{"org private", "/org/us/apple-inc", "/Organization/Corporation/Private", "/Industrials/Machinery/Industrial_Automation", true},
		{"org state owned", "/org/az/socar", "/Organization/Corporation/StateOwned", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true},
		{"org energy", "/org/ru/gazprom", "/Organization/Corporation/StateOwned", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true},
		{"org tech", "/org/us/cybersecurity-corp", "/Organization/Corporation/Private", "/Technology/Software/Enterprise", true},
		{"org bank", "/org/kz/bank-centerkredit", "/Organization/Corporation/Private", "/Financials/Banking/Systemic", true},

		// --- FAC ---
		{"fac pipeline", "/fac/botas/turkstream-pipeline", "/Infrastructure/Energy/Pipeline/Gas", "/Energy/Fossil_Fuels/Oil_Gas_Midstream", true},
		{"fac refinery", "/fac/aramco/ras-tanura-refinery", "/Infrastructure/Manufacturing/Factory", "/Energy/Fossil_Fuels/Oil_Gas_Downstream", true},
		{"fac terminal", "/fac/na/lng-terminal-aliaga", "/Infrastructure/Logistics/Warehouse", "/Energy/Fossil_Fuels/Oil_Gas_Midstream", true},
		{"fac mine", "/fac/bhp/olympic-dam-mine", "/Infrastructure/Manufacturing/Mine", "/Materials/Mining/Industrial_Metals", true},
		{"fac port", "/fac/na/ceyhan-seaport", "/Infrastructure/Logistics/Seaport", "/Industrials/Transportation/Marine", true},
		{"fac factory", "/fac/tsmc/fab-21", "/Infrastructure/Manufacturing/Factory", "/Technology/Semiconductors/Manufacturing", true},
		{"fac solar", "/fac/na/solar-park", "/Infrastructure/Energy/PowerPlant/Renewable", "/Energy/Renewable/Solar", true},
		{"fac generic", "/fac/na/unknown-facility", "/Infrastructure/Manufacturing/Factory", "/Industrials/Machinery/Industrial_Automation", true},

		// --- PRODUCT ---
		{"product raw mineral", "/product/raw/mineral/extraction/lithium-carbonate", "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true},
		{"product raw agricultural", "/product/raw/agricultural/processing/wheat", "/Resource/RawMaterial/Agricultural", "/Materials/Chemicals/Fertilizers", true},
		{"product raw energy", "/product/raw/energy/extraction/crude-oil", "/Resource/Energy/Fossil", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true},
		{"product raw chemical", "/product/raw/chemical/refining/methanol", "/Resource/RawMaterial/Mineral", "/Materials/Chemicals/Specialty", true},
		{"product component electronic", "/product/component/electronic/manufacturing/gpu-chip", "/Resource/Component/Electronic", "/Technology/Semiconductors/Design", true},
		{"product component mechanical", "/product/component/mechanical/assembly/gear-train", "/Resource/Component/Mechanical", "/Industrials/Machinery/Industrial_Automation", true},
		{"product finished", "/product/finished/electronic/assembly/smartphone", "/Resource/Component/Electronic", "/Technology/Hardware/Consumer_Electronics", true},
		{"product service", "/product/service/software/na/cloud-hosting", "/Organization/Corporation/Private", "/Technology/Software/Enterprise", true},

		// --- LOC ---
		{"loc country", "/loc/tr/na/na", "/Geo/Region/Country", "/Geography", true},
		{"loc city", "/loc/us/ca/san-francisco", "/Geo/Region/Country", "/Geography", true},

		// --- MARKET (entityPath by geo scope) ---
		{"market energy global", "/market/global/energy/oil-gas", "/Market/Geo/Global", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true},
		{"market semiconductor", "/market/global/semiconductor", "/Market/Geo/Global", "/Technology/Semiconductors/Manufacturing", true},
		{"market finance country", "/market/kz/finance/banking", "/Market/Geo/Country", "/Financials/Banking/Systemic", true},
		{"market automotive", "/market/de/automotive/ev", "/Market/Geo/Country", "/Automotive/Vehicles/EV", true},
		{"market generic", "/market/global/professional-services/consulting", "/Market/Geo/Global", "/Industrials/Machinery/Industrial_Automation", true},
		{"market ict", "/market/no/technology/ict", "/Market/Geo/Global", "/Technology/Software/Enterprise", true},
		{"market regional", "/market/brics/energy/oil-gas", "/Market/Geo/Regional", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true},

		// --- PERSON ---
		{"person", "/person/elon-musk", "/Person/Executive", "/Financials/Capital_Markets/Asset_Management", true},

		// --- DEPOSIT ---
		{"deposit", "/deposit/cl/atacama-lithium", "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true},

		// --- MECHANISM ---
		{"mechanism", "/mechanism/tax/vat-rate", "/Organization/Government/Agency", "/Public/Executive/Ministry", true},

		// --- DECISION ---
		{"decision", "/decision/kz/vat-rate-2025", "/Organization/Government/Agency", "/Public/Executive/Ministry", true},

		// --- PROGRAM ---
		{"program", "/program/kz/social-housing", "/Organization/Government/Agency", "/Public/Executive/Ministry", true},

		// --- DOMAIN (should not classify) ---
		{"domain skip", "/domain/v1/supply-chain/geo/tr", "", "", false},

		// --- SPHERE (should not classify) ---
		{"sphere skip", "/sphere/energy/fossil", "", "", false},

		// --- Short path ---
		{"short path", "/x", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep, sp, ok := ClassifyTaxonomyDeterministic(tt.canonicalPath)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if ep != tt.wantEntityPath {
				t.Errorf("entityPath = %q, want %q", ep, tt.wantEntityPath)
			}
			if sp != tt.wantSectorPath {
				t.Errorf("sectorPath = %q, want %q", sp, tt.wantSectorPath)
			}
		})
	}
}
