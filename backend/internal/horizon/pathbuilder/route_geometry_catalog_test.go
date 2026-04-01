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

func TestResolveCuratedRouteGeometry_MatchesSingaporeGenoaViaSuez(t *testing.T) {
	refs := []string{
		"/fac/sg/port-of-singapore",
		"/fac/it/port-of-genoa",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/sg/port-of-singapore": {
			CanonicalPath: "/fac/sg/port-of-singapore",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Port of Singapore"},
		},
		"/fac/it/port-of-genoa": {
			CanonicalPath: "/fac/it/port-of-genoa",
			Coordinates:   &models.Coordinates{Lat: 44.404833, Lon: 8.917847},
			Labels:        map[string]string{"en": "Port of Genoa"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_singapore_genoa_via_suez" {
		t.Fatalf("flowRefs=%v, want sea_singapore_genoa_via_suez", resolved.FlowRefs)
	}
	if len(resolved.Coordinates) < 8 {
		t.Fatalf("coords=%d, want at least 8", len(resolved.Coordinates))
	}
}

func TestNormalizeMaritimeOrderedRefs_ReordersLegacySeaWaypoints(t *testing.T) {
	refs := []string{
		"/fac/id/banten-processing-site",
		"/fac/id/port-of-tanjung-priok",
		"/fac/it/port-of-genoa",
		"/fac/sg/port-of-singapore",
		"/fac/it/hcp-tannery",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/id/banten-processing-site": {
			CanonicalPath: "/fac/id/banten-processing-site",
			Coordinates:   &models.Coordinates{Lat: -6.405817, Lon: 106.064018},
			Labels:        map[string]string{"en": "Banten Processing Site"},
		},
		"/fac/id/port-of-tanjung-priok": {
			CanonicalPath: "/fac/id/port-of-tanjung-priok",
			Coordinates:   &models.Coordinates{Lat: -6.104444, Lon: 106.880556},
			Labels:        map[string]string{"en": "Port of Tanjung Priok"},
		},
		"/fac/sg/port-of-singapore": {
			CanonicalPath: "/fac/sg/port-of-singapore",
			Coordinates:   &models.Coordinates{Lat: 1.266, Lon: 103.855},
			Labels:        map[string]string{"en": "Port of Singapore"},
		},
		"/fac/it/port-of-genoa": {
			CanonicalPath: "/fac/it/port-of-genoa",
			Coordinates:   &models.Coordinates{Lat: 44.404833, Lon: 8.917847},
			Labels:        map[string]string{"en": "Port of Genoa"},
		},
		"/fac/it/hcp-tannery": {
			CanonicalPath: "/fac/it/hcp-tannery",
			Coordinates:   &models.Coordinates{Lat: 43.870197, Lon: 10.804033},
			Labels:        map[string]string{"en": "HCP Tannery"},
		},
	}

	got := normalizeMaritimeOrderedRefs(refs, fragsByPath)
	want := []string{
		"/fac/id/banten-processing-site",
		"/fac/id/port-of-tanjung-priok",
		"/fac/sg/port-of-singapore",
		"/fac/it/port-of-genoa",
		"/fac/it/hcp-tannery",
	}
	if len(got) != len(want) {
		t.Fatalf("ordered=%v, want %v", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("ordered=%v, want %v", got, want)
		}
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
		"/fac/int/strait-of-malacca",
		"/fac/us/houston-ship-channel",
		"/fac/io/nsf-diego-garcia",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/us/baytown-refinery": {
			CanonicalPath: "/fac/us/baytown-refinery",
			Coordinates:   &models.Coordinates{Lat: 29.7355, Lon: -94.9774},
			Labels:        map[string]string{"en": "Baytown Refinery"},
		},
		"/fac/int/strait-of-malacca": {
			CanonicalPath: "/fac/int/strait-of-malacca",
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

func TestResolveCuratedRouteGeometry_MatchesJebelAliSohar(t *testing.T) {
	refs := []string{
		"/fac/ae/jebel-ali-port",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.074282, Lon: 55.188539},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_jebel_ali_sohar_feeder" {
		t.Fatalf("flowRefs=%v, want sea_jebel_ali_sohar_feeder", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesHamburgSohar(t *testing.T) {
	refs := []string{
		"/fac/de/port-of-hamburg",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/de/port-of-hamburg": {
			CanonicalPath: "/fac/de/port-of-hamburg",
			Coordinates:   &models.Coordinates{Lat: 53.509430, Lon: 9.965477},
			Labels:        map[string]string{"en": "Port of Hamburg"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_hamburg_sohar_via_suez" {
		t.Fatalf("flowRefs=%v, want sea_hamburg_sohar_via_suez", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesFelixstoweSohar(t *testing.T) {
	refs := []string{
		"/fac/gb/port-of-felixstowe",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/gb/port-of-felixstowe": {
			CanonicalPath: "/fac/gb/port-of-felixstowe",
			Coordinates:   &models.Coordinates{Lat: 51.955696, Lon: 1.312976},
			Labels:        map[string]string{"en": "Port of Felixstowe"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_felixstowe_sohar_via_suez" {
		t.Fatalf("flowRefs=%v, want sea_felixstowe_sohar_via_suez", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesSuezSohar(t *testing.T) {
	refs := []string{
		"/fac/eg/suez-terminal",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/eg/suez-terminal": {
			CanonicalPath: "/fac/eg/suez-terminal",
			Coordinates:   &models.Coordinates{Lat: 29.966790, Lon: 32.549828},
			Labels:        map[string]string{"en": "Suez Terminal"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_suez_sohar_mainline" {
		t.Fatalf("flowRefs=%v, want sea_suez_sohar_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesHamburgSuez(t *testing.T) {
	refs := []string{
		"/fac/de/port-of-hamburg",
		"/fac/eg/suez-terminal",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/de/port-of-hamburg": {
			CanonicalPath: "/fac/de/port-of-hamburg",
			Coordinates:   &models.Coordinates{Lat: 53.509430, Lon: 9.965477},
			Labels:        map[string]string{"en": "Port of Hamburg"},
		},
		"/fac/eg/suez-terminal": {
			CanonicalPath: "/fac/eg/suez-terminal",
			Coordinates:   &models.Coordinates{Lat: 29.966790, Lon: 32.549828},
			Labels:        map[string]string{"en": "Suez Terminal"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_hamburg_suez_via_gibraltar" {
		t.Fatalf("flowRefs=%v, want sea_hamburg_suez_via_gibraltar", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesFelixstoweSuez(t *testing.T) {
	refs := []string{
		"/fac/gb/port-of-felixstowe",
		"/fac/eg/suez-terminal",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/gb/port-of-felixstowe": {
			CanonicalPath: "/fac/gb/port-of-felixstowe",
			Coordinates:   &models.Coordinates{Lat: 51.955696, Lon: 1.312976},
			Labels:        map[string]string{"en": "Port of Felixstowe"},
		},
		"/fac/eg/suez-terminal": {
			CanonicalPath: "/fac/eg/suez-terminal",
			Coordinates:   &models.Coordinates{Lat: 29.966790, Lon: 32.549828},
			Labels:        map[string]string{"en": "Suez Terminal"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_felixstowe_suez_via_gibraltar" {
		t.Fatalf("flowRefs=%v, want sea_felixstowe_suez_via_gibraltar", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesSuezJebelAli(t *testing.T) {
	refs := []string{
		"/fac/eg/suez-terminal",
		"/fac/ae/jebel-ali-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/eg/suez-terminal": {
			CanonicalPath: "/fac/eg/suez-terminal",
			Coordinates:   &models.Coordinates{Lat: 29.966790, Lon: 32.549828},
			Labels:        map[string]string{"en": "Suez Terminal"},
		},
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.074282, Lon: 55.188539},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_suez_jebel_ali_mainline" {
		t.Fatalf("flowRefs=%v, want sea_suez_jebel_ali_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_DoesNotSingleEndMatchUSGulfJebelAli(t *testing.T) {
	refs := []string{
		"/fac/ae/jebel-ali-manufacturing-center",
		"/fac/ae/jebel-ali-port-terminal",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/ae/jebel-ali-manufacturing-center": {
			CanonicalPath: "/fac/ae/jebel-ali-manufacturing-center",
			Labels:        map[string]string{"en": "Jebel Ali Manufacturing Center"},
		},
		"/fac/ae/jebel-ali-port-terminal": {
			CanonicalPath: "/fac/ae/jebel-ali-port-terminal",
			Coordinates:   &models.Coordinates{Lat: 25.023611, Lon: 55.040278},
			Labels:        map[string]string{"en": "Jebel Ali Port Terminal"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if len(resolved.Coordinates) != 0 || resolved.Source != "" || len(resolved.FlowRefs) != 0 {
		t.Fatalf("resolved=%+v, want no curated match", resolved)
	}
}

func TestResolveCuratedRouteGeometry_MatchesShanghaiSohar(t *testing.T) {
	refs := []string{
		"/fac/cn/port-of-shanghai",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/cn/port-of-shanghai": {
			CanonicalPath: "/fac/cn/port-of-shanghai",
			Coordinates:   &models.Coordinates{Lat: 30.626595, Lon: 122.063382},
			Labels:        map[string]string{"en": "Port of Shanghai"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_shanghai_sohar_via_malacca" {
		t.Fatalf("flowRefs=%v, want sea_shanghai_sohar_via_malacca", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesShanghaiMalacca(t *testing.T) {
	refs := []string{
		"/fac/cn/port-of-shanghai",
		"/fac/int/strait-of-malacca",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/cn/port-of-shanghai": {
			CanonicalPath: "/fac/cn/port-of-shanghai",
			Coordinates:   &models.Coordinates{Lat: 30.626595, Lon: 122.063382},
			Labels:        map[string]string{"en": "Port of Shanghai"},
		},
		"/fac/int/strait-of-malacca": {
			CanonicalPath: "/fac/int/strait-of-malacca",
			Coordinates:   &models.Coordinates{Lat: 4.662590, Lon: 99.545097},
			Labels:        map[string]string{"en": "Strait of Malacca"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_shanghai_malacca_mainline" {
		t.Fatalf("flowRefs=%v, want sea_shanghai_malacca_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesYokohamaSohar(t *testing.T) {
	refs := []string{
		"/fac/jp/port-of-yokohama",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/jp/port-of-yokohama": {
			CanonicalPath: "/fac/jp/port-of-yokohama",
			Coordinates:   &models.Coordinates{Lat: 35.450000, Lon: 139.646111},
			Labels:        map[string]string{"en": "Port of Yokohama"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_yokohama_sohar_via_malacca" {
		t.Fatalf("flowRefs=%v, want sea_yokohama_sohar_via_malacca", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesYokohamaMalacca(t *testing.T) {
	refs := []string{
		"/fac/jp/port-of-yokohama",
		"/fac/int/strait-of-malacca",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/jp/port-of-yokohama": {
			CanonicalPath: "/fac/jp/port-of-yokohama",
			Coordinates:   &models.Coordinates{Lat: 35.450000, Lon: 139.646111},
			Labels:        map[string]string{"en": "Port of Yokohama"},
		},
		"/fac/int/strait-of-malacca": {
			CanonicalPath: "/fac/int/strait-of-malacca",
			Coordinates:   &models.Coordinates{Lat: 4.662590, Lon: 99.545097},
			Labels:        map[string]string{"en": "Strait of Malacca"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_yokohama_malacca_mainline" {
		t.Fatalf("flowRefs=%v, want sea_yokohama_malacca_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesMalaccaJebelAli(t *testing.T) {
	refs := []string{
		"/fac/int/strait-of-malacca",
		"/fac/ae/jebel-ali-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/int/strait-of-malacca": {
			CanonicalPath: "/fac/int/strait-of-malacca",
			Coordinates:   &models.Coordinates{Lat: 4.662590, Lon: 99.545097},
			Labels:        map[string]string{"en": "Strait of Malacca"},
		},
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.074282, Lon: 55.188539},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_malacca_jebel_ali_mainline" {
		t.Fatalf("flowRefs=%v, want sea_malacca_jebel_ali_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_MatchesMalaccaSohar(t *testing.T) {
	refs := []string{
		"/fac/int/strait-of-malacca",
		"/fac/om/sohar-port",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/int/strait-of-malacca": {
			CanonicalPath: "/fac/int/strait-of-malacca",
			Coordinates:   &models.Coordinates{Lat: 4.662590, Lon: 99.545097},
			Labels:        map[string]string{"en": "Strait of Malacca"},
		},
		"/fac/om/sohar-port": {
			CanonicalPath: "/fac/om/sohar-port",
			Coordinates:   &models.Coordinates{Lat: 24.484851, Lon: 56.612114},
			Labels:        map[string]string{"en": "Sohar Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if resolved.Source != "curated_flow_catalog" {
		t.Fatalf("source=%q, want curated_flow_catalog", resolved.Source)
	}
	if len(resolved.FlowRefs) != 1 || resolved.FlowRefs[0] != "sea_malacca_sohar_mainline" {
		t.Fatalf("flowRefs=%v, want sea_malacca_sohar_mainline", resolved.FlowRefs)
	}
}

func TestResolveCuratedRouteGeometry_DoesNotFallbackToUSGulfWhenOnlyJebelEndpointAliasIsKnown(t *testing.T) {
	refs := []string{
		"/fac/jp/hiroshima-plant",
		"/fac/ae/jebel-ali-terminal",
	}
	fragsByPath := map[string]*models.HorizonFragment{
		"/fac/jp/hiroshima-plant": {
			CanonicalPath: "/fac/jp/hiroshima-plant",
			Coordinates:   &models.Coordinates{Lat: 34.3853, Lon: 132.4553},
			Labels:        map[string]string{"en": "Hiroshima Plant"},
		},
		"/fac/ae/jebel-ali-port": {
			CanonicalPath: "/fac/ae/jebel-ali-port",
			Coordinates:   &models.Coordinates{Lat: 25.074282, Lon: 55.188539},
			Labels:        map[string]string{"en": "Jebel Ali Port"},
		},
	}

	resolved := resolveCuratedRouteGeometry(refs, fragsByPath)
	if len(resolved.Coordinates) != 0 || resolved.Source != "" {
		t.Fatalf("resolved=%+v, want no curated match", resolved)
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

func TestMaritimeBackboneReuse_EuropeToSuezUsesSharedTrunk(t *testing.T) {
	hamburg := composeCatalogCoordinates(
		maritimeHamburgToGibraltarIngress(),
		maritimeGibraltarToSuezTrunk(),
	)
	felixstowe := composeCatalogCoordinates(
		maritimeFelixstoweToGibraltarIngress(),
		maritimeGibraltarToSuezTrunk(),
	)
	wantTail := maritimeGibraltarToSuezTrunk()
	if !routeHasSuffix(hamburg, wantTail) {
		t.Fatalf("hamburg route missing shared Gibraltar->Suez trunk: %v", hamburg)
	}
	if !routeHasSuffix(felixstowe, wantTail) {
		t.Fatalf("felixstowe route missing shared Gibraltar->Suez trunk: %v", felixstowe)
	}
}

func TestMaritimeBackboneReuse_MalaccaToArabianSeaUsesSharedTrunk(t *testing.T) {
	shanghai := composeCatalogCoordinates(
		maritimeShanghaiToMalaccaMainline(),
		maritimeMalaccaToHormuzTrunk(),
		maritimeHormuzToSoharFeeder(),
	)
	yokohama := composeCatalogCoordinates(
		maritimeYokohamaToMalaccaMainline(),
		maritimeMalaccaToHormuzTrunk(),
		maritimeHormuzToSoharFeeder(),
	)
	wantTail := composeCatalogCoordinates(
		maritimeMalaccaToHormuzTrunk(),
		maritimeHormuzToSoharFeeder(),
	)
	if !routeHasSuffix(shanghai, wantTail) {
		t.Fatalf("shanghai route missing shared Malacca->Sohar trunk: %v", shanghai)
	}
	if !routeHasSuffix(yokohama, wantTail) {
		t.Fatalf("yokohama route missing shared Malacca->Sohar trunk: %v", yokohama)
	}
}

func routeHasSuffix(route []models.Coordinates, suffix []models.Coordinates) bool {
	if len(suffix) == 0 || len(route) < len(suffix) {
		return false
	}
	offset := len(route) - len(suffix)
	for idx := range suffix {
		if !sameCoord(route[offset+idx], suffix[idx]) {
			return false
		}
	}
	return true
}

func sameCoord(a, b models.Coordinates) bool {
	return absRouteDelta(a.Lat-b.Lat) < 1e-7 && absRouteDelta(a.Lon-b.Lon) < 1e-7
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
