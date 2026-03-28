package pathbuilder

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestResolveCuratedRouteGeometry_MatchesGulfSingapore(t *testing.T) {
	refs := []string{
		"/fac/sa/ras-tanura-terminal",
		"/loc/ir/hormozgan/strait_of_hormuz",
		"/fac/sg/singapore-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/sa/ras-tanura-terminal": {
			CanonicalPath: "/fac/sa/ras-tanura-terminal",
			Coordinates:   &models.Coordinates{Lat: 26.65, Lon: 50.04},
			Labels:        map[string]string{"en": "Ras Tanura Terminal"},
		},
		"/loc/ir/hormozgan/strait_of_hormuz": {
			CanonicalPath: "/loc/ir/hormozgan/strait_of_hormuz",
			Coordinates:   &models.Coordinates{Lat: 26.58, Lon: 56.30},
			Labels:        map[string]string{"en": "Strait of Hormuz"},
		},
		"/fac/sg/singapore-port": {
			CanonicalPath: "/fac/sg/singapore-port",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Singapore Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_ras_tanura_singapore_mainline" {
		t.Fatalf("flowRefs=%v, want sea_ras_tanura_singapore_mainline", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 6 {
		t.Fatalf("coords=%d, want at least 6", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesSingaporeDiegoGarcia(t *testing.T) {
	refs := []string{
		"/fac/sg/port-of-singapore",
		"/fac/io/nsf-diego-garcia",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/sg/port-of-singapore": {
			CanonicalPath: "/fac/sg/port-of-singapore",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Port of Singapore"},
		},
		"/fac/io/nsf-diego-garcia": {
			CanonicalPath: "/fac/io/nsf-diego-garcia",
			Coordinates:   &models.Coordinates{Lat: -7.313, Lon: 72.411},
			Labels:        map[string]string{"en": "NSF Diego Garcia"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_singapore_diego_garcia_mainline" {
		t.Fatalf("flowRefs=%v, want sea_singapore_diego_garcia_mainline", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 6 {
		t.Fatalf("coords=%d, want at least 6", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesJebelAliSingapore(t *testing.T) {
	refs := []string{
		"/fac/ae/jebel-ali-port",
		"/fac/sg/port-of-singapore",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.01, Lon: 55.06},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
		"/fac/sg/port-of-singapore": {
			CanonicalPath: "/fac/sg/port-of-singapore",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Port of Singapore"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_jebel_ali_singapore_mainline" {
		t.Fatalf("flowRefs=%v, want sea_jebel_ali_singapore_mainline", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 6 {
		t.Fatalf("coords=%d, want at least 6", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesYokosukaSingapore(t *testing.T) {
	refs := []string{
		"/fac/jp/navsup-flc-yokosuka",
		"/fac/sg/port-of-singapore",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/jp/navsup-flc-yokosuka": {
			CanonicalPath: "/fac/jp/navsup-flc-yokosuka",
			Coordinates:   &models.Coordinates{Lat: 35.28, Lon: 139.67},
			Labels:        map[string]string{"en": "NAVSUP Fleet Logistics Center Yokosuka"},
		},
		"/fac/sg/port-of-singapore": {
			CanonicalPath: "/fac/sg/port-of-singapore",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Port of Singapore"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_yokosuka_singapore_mainline" {
		t.Fatalf("flowRefs=%v, want sea_yokosuka_singapore_mainline", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 6 {
		t.Fatalf("coords=%d, want at least 6", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesBrazilShanghaiViaCape(t *testing.T) {
	refs := []string{
		"/fac/br/ponta-da-madeira-maritime-terminal",
		"/fac/cn/port-of-shanghai",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/br/ponta-da-madeira-maritime-terminal": {
			CanonicalPath: "/fac/br/ponta-da-madeira-maritime-terminal",
			Coordinates:   &models.Coordinates{Lat: -2.56, Lon: -44.37},
			Labels:        map[string]string{"en": "Ponta da Madeira Maritime Terminal"},
		},
		"/fac/cn/port-of-shanghai": {
			CanonicalPath: "/fac/cn/port-of-shanghai",
			Coordinates:   &models.Coordinates{Lat: 30.617, Lon: 122.067},
			Labels:        map[string]string{"en": "Port of Shanghai"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_brazil_shanghai_via_cape" {
		t.Fatalf("flowRefs=%v, want sea_brazil_shanghai_via_cape", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesBaytownDiegoViaMalacca(t *testing.T) {
	refs := []string{
		"/fac/us/baytown-refinery",
		"/fac/sea/strait-of-malacca",
		"/fac/us/houston-ship-channel",
		"/fac/io/nsf-diego-garcia",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/us/baytown-refinery": {
			CanonicalPath: "/fac/us/baytown-refinery",
			Coordinates:   &models.Coordinates{Lat: 29.7355, Lon: -94.9774},
			Labels:        map[string]string{"en": "Baytown Refinery"},
		},
		"/fac/sea/strait-of-malacca": {
			CanonicalPath: "/fac/sea/strait-of-malacca",
			Coordinates:   &models.Coordinates{Lat: 3.0, Lon: 100.0},
			Labels:        map[string]string{"en": "Strait of Malacca"},
		},
		"/fac/us/houston-ship-channel": {
			CanonicalPath: "/fac/us/houston-ship-channel",
			Coordinates:   &models.Coordinates{Lat: 29.73, Lon: -95.01},
			Labels:        map[string]string{"en": "Houston Ship Channel"},
		},
		"/fac/io/nsf-diego-garcia": {
			CanonicalPath: "/fac/io/nsf-diego-garcia",
			Coordinates:   &models.Coordinates{Lat: -7.313, Lon: 72.411},
			Labels:        map[string]string{"en": "NSF Diego Garcia"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_us_gulf_diego_via_malacca" {
		t.Fatalf("flowRefs=%v, want sea_us_gulf_diego_via_malacca", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 8 {
		t.Fatalf("coords=%d, want at least 8", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesPortArthurJebelAli(t *testing.T) {
	refs := []string{
		"/fac/us/port-arthur-refinery",
		"/fac/ae/jebel-ali-port",
		"/fac/io/nsf-diego-garcia",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/us/port-arthur-refinery": {
			CanonicalPath: "/fac/us/port-arthur-refinery",
			Coordinates:   &models.Coordinates{Lat: 29.8849, Lon: -93.9399},
			Labels:        map[string]string{"en": "Port Arthur Refinery"},
		},
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.01, Lon: 55.06},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
		"/fac/io/nsf-diego-garcia": {
			CanonicalPath: "/fac/io/nsf-diego-garcia",
			Coordinates:   &models.Coordinates{Lat: -7.313, Lon: 72.411},
			Labels:        map[string]string{"en": "NSF Diego Garcia"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_us_gulf_diego_via_malacca" {
		t.Fatalf("flowRefs=%v, want sea_us_gulf_diego_via_malacca", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesMongstadBaytown(t *testing.T) {
	refs := []string{
		"/fac/no/mongstad-terminal",
		"/fac/us/houston-ship-channel",
		"/fac/us/baytown-refinery",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/no/mongstad-terminal": {
			CanonicalPath: "/fac/no/mongstad-terminal",
			Coordinates:   &models.Coordinates{Lat: 60.81, Lon: 5.02},
			Labels:        map[string]string{"en": "Mongstad Terminal"},
		},
		"/fac/us/houston-ship-channel": {
			CanonicalPath: "/fac/us/houston-ship-channel",
			Coordinates:   &models.Coordinates{Lat: 29.73, Lon: -95.01},
			Labels:        map[string]string{"en": "Houston Ship Channel"},
		},
		"/fac/us/baytown-refinery": {
			CanonicalPath: "/fac/us/baytown-refinery",
			Coordinates:   &models.Coordinates{Lat: 29.7355, Lon: -94.9774},
			Labels:        map[string]string{"en": "Baytown Refinery"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_norway_us_gulf_mainline" {
		t.Fatalf("flowRefs=%v, want sea_norway_us_gulf_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesCarajasRailAccessWithMissingGatewayFragment(t *testing.T) {
	refs := []string{
		"/fac/br/carajas-mine",
		"/fac/br/ponta-da-madeira-maritime-terminal",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/br/carajas-mine": {
			CanonicalPath: "/fac/br/carajas-mine",
			Coordinates:   &models.Coordinates{Lat: -6.054627, Lon: -50.177514},
			Labels:        map[string]string{"en": "Carajas Mine"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "rail_carajas_efc_ponta_da_madeira" {
		t.Fatalf("flowRefs=%v, want rail_carajas_efc_ponta_da_madeira", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 4 {
		t.Fatalf("coords=%d, want at least 4", len(resolved.Coordinates))
	}
}

func TestResolveCuratedRouteGeometry_MatchesShanghaiPortToDRI(t *testing.T) {
	refs := []string{
		"/fac/cn/port-of-shanghai",
		"/fac/na/shanghai-dri-facility",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/cn/port-of-shanghai": {
			CanonicalPath: "/fac/cn/port-of-shanghai",
			Coordinates:   &models.Coordinates{Lat: 30.626595, Lon: 122.063382},
			Labels:        map[string]string{"en": "Port of Shanghai"},
		},
		"/fac/na/shanghai-dri-facility": {
			CanonicalPath: "/fac/na/shanghai-dri-facility",
			Coordinates:   &models.Coordinates{Lat: 31.230416, Lon: 121.473701},
			Labels:        map[string]string{"en": "Baowu Shanghai DRI Facility"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "land_port_of_shanghai_baowu_dri" {
		t.Fatalf("flowRefs=%v, want land_port_of_shanghai_baowu_dri", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesDaqingFushunCorridor(t *testing.T) {
	refs := []string{
		"/fac/cn/daqing-oilfield",
		"/fac/cn/fushun-petrochemical-refinery",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/cn/daqing-oilfield": {
			CanonicalPath: "/fac/cn/daqing-oilfield",
			Coordinates:   &models.Coordinates{Lat: 46.587580, Lon: 125.103070},
			Labels:        map[string]string{"en": "Daqing Oilfield Company"},
		},
		"/fac/cn/fushun-petrochemical-refinery": {
			CanonicalPath: "/fac/cn/fushun-petrochemical-refinery",
			Coordinates:   &models.Coordinates{Lat: 41.873078, Lon: 123.885191},
			Labels:        map[string]string{"en": "Fushun Petrochemical Refinery"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "land_daqing_fushun_refining_corridor" {
		t.Fatalf("flowRefs=%v, want land_daqing_fushun_refining_corridor", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 4 {
		t.Fatalf("coords=%d, want at least 4", len(resolved.Coordinates))
	}
}
