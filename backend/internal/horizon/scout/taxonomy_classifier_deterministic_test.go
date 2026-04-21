package scout

import "testing"

func TestClassifyTaxonomyDeterministic(t *testing.T) {
	tests := []struct {
		name           string
		canonicalPath  string
		wantEntityPath string
		wantOK         bool
	}{
		// --- GOV ---
		{"gov ministry", "/gov/kz/ministry-of-finance", "/Organization/Government/Agency", true},
		{"gov military", "/gov/us/department-of-defense", "/Organization/Government/Military", true},
		{"gov central bank", "/gov/kz/central-bank", "/Organization/Government/Agency", true},
		{"gov parliament", "/gov/tr/parliament", "/Organization/Government/Agency", true},
		{"gov court", "/gov/us/supreme-court", "/Organization/Government/Agency", true},

		// --- ORG ---
		{"org private", "/org/us/apple-inc", "/Organization/Corporation/Private", true},
		{"org state owned", "/org/az/socar", "/Organization/Corporation/StateOwned", true},
		{"org energy", "/org/ru/gazprom", "/Organization/Corporation/StateOwned", true},
		{"org tech", "/org/us/cybersecurity-corp", "/Organization/Corporation/Private", true},
		{"org bank", "/org/kz/bank-centerkredit", "/Organization/Corporation/Private", true},

		// --- FAC ---
		{"fac pipeline", "/fac/tr/turkstream-pipeline", "/Infrastructure/Energy/Pipeline/Gas", true},
		{"fac refinery", "/fac/sa/ras-tanura-refinery", "/Infrastructure/Manufacturing/Factory", true},
		{"fac terminal", "/fac/na/lng-terminal-aliaga", "/Infrastructure/Logistics/Warehouse", true},
		{"fac mine", "/fac/au/olympic-dam-mine", "/Infrastructure/Manufacturing/Mine", true},
		{"fac port", "/fac/na/ceyhan-seaport", "/Infrastructure/Logistics/Seaport", true},
		{"fac factory", "/fac/tw/fab-21", "/Infrastructure/Manufacturing/Factory", true},
		{"fac solar", "/fac/na/solar-park", "/Infrastructure/Energy/PowerPlant/Renewable", true},
		{"fac generic", "/fac/na/unknown-facility", "/Infrastructure/Manufacturing/Factory", true},

		// --- PRODUCT ---
		{"product raw mineral", "/product/raw/mineral/extraction/lithium-carbonate", "/Resource/RawMaterial/Mineral", true},
		{"product raw agricultural", "/product/raw/agricultural/processing/wheat", "/Resource/RawMaterial/Agricultural", true},
		{"product raw energy", "/product/raw/energy/extraction/crude-oil", "/Resource/Energy/Fossil", true},
		{"product raw chemical", "/product/raw/chemical/refining/methanol", "/Resource/RawMaterial/Mineral", true},
		{"product component electronic", "/product/component/electronic/manufacturing/gpu-chip", "/Resource/Component/Electronic", true},
		{"product component mechanical", "/product/component/mechanical/assembly/gear-train", "/Resource/Component/Mechanical", true},
		{"product finished", "/product/finished/electronic/assembly/smartphone", "/Resource/Component/Electronic", true},
		{"product service", "/product/service/software/na/cloud-hosting", "/Organization/Corporation/Private", true},

		// --- LOC ---
		{"loc country", "/loc/tr/na/na", "/Geo/Region/Country", true},
		{"loc city", "/loc/us/ca/san-francisco", "/Geo/Region/Country", true},

		// --- MARKET (entityPath by geo scope) ---
		{"market energy global", "/market/global/energy/oil-gas", "/Market/Geo/Global", true},
		{"market semiconductor", "/market/global/semiconductor", "/Market/Geo/Global", true},
		{"market finance country", "/market/kz/finance/banking", "/Market/Geo/Country", true},
		{"market automotive", "/market/de/automotive/ev", "/Market/Geo/Country", true},
		{"market generic", "/market/global/professional-services/consulting", "/Market/Geo/Global", true},
		{"market ict", "/market/no/technology/ict", "/Market/Geo/Global", true},
		{"market regional", "/market/brics/energy/oil-gas", "/Market/Geo/Regional", true},

		// --- PERSON ---
		{"person", "/person/elon-musk", "/Person/Executive", true},

		// --- deposit ---
		{"deposit", "/deposit/cl/atacama-lithium", "/Resource/RawMaterial/Mineral", true},

		// --- MECHANISM ---
		{"mechanism", "/mechanism/tax/vat-rate", "/Organization/Government/Agency", true},

		// --- DECISION ---
		{"decision", "/decision/kz/vat-rate-2025", "/Organization/Government/Agency", true},

		// --- PROGRAM ---
		{"program", "/program/kz/social-housing", "/Organization/Government/Agency", true},

		// --- DOMAIN (should not classify) ---
		{"domain skip", "/domain/v1/supply-chain/geo/tr", "", false},

		// --- SPHERE (should not classify) ---
		{"sphere skip", "/sphere/energy/fossil", "", false},

		// --- Short path ---
		{"short path", "/x", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep, ok := ClassifyTaxonomyDeterministic(tt.canonicalPath)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if ep != tt.wantEntityPath {
				t.Errorf("entityPath = %q, want %q", ep, tt.wantEntityPath)
			}
		})
	}
}
