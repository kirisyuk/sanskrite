package pathbuilder

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type curatedRouteProfile struct {
	ID                          string
	FlowRefs                    []string
	Coordinates                 []models.Coordinates
	RequiredTokens              []string
	PreferredTokens             []string
	SingleEndedMinPreferredHits float64
	OriginAnchor                models.Coordinates
	OriginRadiusKm              float64
	DestinationAnchor           models.Coordinates
	DestinationRadiusKm         float64
}

func resolveCuratedRouteGeometry(
	cleanRefs []string,
	fragsByPath map[string]*models.HorizonFragment,
) ResolvedRouteGeometry {
	if len(cleanRefs) < 2 {
		return ResolvedRouteGeometry{}
	}
	tokens := collectRouteTokens(cleanRefs, fragsByPath)
	start, startOK := resolveRouteEndpoint(cleanRefs[0], fragsByPath)
	end, endOK := resolveRouteEndpoint(cleanRefs[len(cleanRefs)-1], fragsByPath)

	var best *curatedRouteProfile
	bestScore := -1.0
	reverseBest := false
	for _, profile := range curatedRouteProfiles() {
		if !routeProfileTokensMatch(tokens, profile) {
			continue
		}
		score, reverse, ok := scoreCuratedRouteProfile(profile, tokens, start, startOK, end, endOK)
		if !ok || score <= bestScore {
			continue
		}
		best = profile
		bestScore = score
		reverseBest = reverse
	}
	if best == nil || len(best.Coordinates) < 2 {
		return ResolvedRouteGeometry{}
	}
	coords := append([]models.Coordinates(nil), best.Coordinates...)
	if reverseBest {
		coords = reverseCoordinates(coords)
	}
	if startOK {
		coords = maybeAttachEndpoint(coords, start, 320, true)
	}
	if endOK {
		coords = maybeAttachEndpoint(coords, end, 320, false)
	}
	return ResolvedRouteGeometry{
		Coordinates: coords,
		FlowRefs:    append([]string(nil), best.FlowRefs...),
		Source:      "curated_flow_catalog",
	}
}

func collectRouteTokens(cleanRefs []string, fragsByPath map[string]*models.HorizonFragment) map[string]struct{} {
	out := make(map[string]struct{}, len(cleanRefs)*8)
	addTextTokens := func(raw string) {
		raw = strings.ToLower(strings.TrimSpace(raw))
		if raw == "" {
			return
		}
		replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ",", " ")
		for _, token := range strings.Fields(replacer.Replace(raw)) {
			token = strings.TrimSpace(token)
			if len(token) < 3 {
				continue
			}
			out[token] = struct{}{}
		}
	}
	for _, ref := range cleanRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		addTextTokens(ref)
		frag := fragsByPath[ref]
		if frag == nil {
			continue
		}
		for _, label := range frag.Labels {
			addTextTokens(label)
		}
		addTextTokens(frag.Description)
		addTextTokens(frag.GeoFeatureType)
		addTextTokens(frag.Identifiers["facilityType"])
		addTextTokens(frag.Identifiers["routeType"])
	}
	return out
}

func routeProfileTokensMatch(tokens map[string]struct{}, profile *curatedRouteProfile) bool {
	for _, token := range profile.RequiredTokens {
		if _, ok := tokens[token]; !ok {
			return false
		}
	}
	return true
}

func scoreCuratedRouteProfile(
	profile *curatedRouteProfile,
	tokens map[string]struct{},
	start models.Coordinates,
	startOK bool,
	end models.Coordinates,
	endOK bool,
) (float64, bool, bool) {
	preferredHits := 0.0
	for _, token := range profile.PreferredTokens {
		if _, ok := tokens[token]; ok {
			preferredHits += 1
		}
	}
	if startOK && endOK {
		directOrigin := routeDistanceKm(start, profile.OriginAnchor)
		directDestination := routeDistanceKm(end, profile.DestinationAnchor)
		reverseOrigin := routeDistanceKm(end, profile.OriginAnchor)
		reverseDestination := routeDistanceKm(start, profile.DestinationAnchor)
		if directOrigin <= profile.OriginRadiusKm && directDestination <= profile.DestinationRadiusKm {
			return preferredHits*25 + 100 - (directOrigin+directDestination)/250, false, true
		}
		if reverseOrigin <= profile.OriginRadiusKm && reverseDestination <= profile.DestinationRadiusKm {
			return preferredHits*25 + 100 - (reverseOrigin+reverseDestination)/250, true, true
		}
		return 0, false, false
	}
	if preferredHits == 0 && len(profile.PreferredTokens) > 0 {
		return 0, false, false
	}
	if startOK {
		if preferredHits < profile.SingleEndedMinPreferredHits {
			return 0, false, false
		}
		directOrigin := routeDistanceKm(start, profile.OriginAnchor)
		directDestination := routeDistanceKm(start, profile.DestinationAnchor)
		if directOrigin <= profile.OriginRadiusKm {
			return preferredHits*20 + 60 - directOrigin/250, false, true
		}
		if directDestination <= profile.DestinationRadiusKm {
			return preferredHits*20 + 60 - directDestination/250, true, true
		}
	}
	if endOK {
		if preferredHits < profile.SingleEndedMinPreferredHits {
			return 0, false, false
		}
		directOrigin := routeDistanceKm(end, profile.OriginAnchor)
		directDestination := routeDistanceKm(end, profile.DestinationAnchor)
		if directDestination <= profile.DestinationRadiusKm {
			return preferredHits*20 + 60 - directDestination/250, false, true
		}
		if directOrigin <= profile.OriginRadiusKm {
			return preferredHits*20 + 60 - directOrigin/250, true, true
		}
	}
	return 0, false, false
}

func resolveRouteEndpoint(ref string, fragsByPath map[string]*models.HorizonFragment) (models.Coordinates, bool) {
	if frag := fragsByPath[strings.TrimSpace(ref)]; frag != nil {
		return extractFragmentCoordinate(frag)
	}
	return models.Coordinates{}, false
}

func curatedRouteProfiles() []*curatedRouteProfile {
	return []*curatedRouteProfile{
		{
			ID:                  "sea_gulf_westindia",
			FlowRefs:            []string{"sea_gulf_westindia"},
			RequiredTokens:      []string{"hormuz"},
			PreferredTokens:     []string{"india", "mumbai", "jamnagar", "gujarat"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(18.95, 72.96),
			DestinationRadiusKm: 900,
			Coordinates: []models.Coordinates{
				coord(26.441, 50.1485),
				coord(26.3, 51.6),
				coord(26.4, 56.4),
				coord(25.5, 57.1),
				coord(24.0, 59.0),
				coord(22.3, 67.0),
				coord(20.0, 71.0),
				coord(19.0, 72.4),
				coord(18.95, 72.96),
			},
		},
		{
			ID:                  "sea_gulf_singapore_mainline",
			FlowRefs:            []string{"sea_gulf_singapore_mainline"},
			RequiredTokens:      []string{"hormuz"},
			PreferredTokens:     []string{"singapore", "jurong", "malacca"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 700,
			Coordinates: []models.Coordinates{
				coord(26.65, 50.04),
				coord(26.58, 56.30),
				coord(24.80, 58.90),
				coord(20.50, 63.00),
				coord(14.20, 68.40),
				coord(8.40, 73.60),
				coord(5.80, 78.90),
				coord(5.60, 84.60),
				coord(5.50, 90.50),
				coord(5.60, 95.20),
				coord(7.80, 97.80),
				coord(8.60, 99.20),
				coord(5.10, 100.40),
				coord(2.80, 102.40),
				coord(1.266, 103.855),
			},
		},
		{
			ID:                  "sea_jebel_ali_singapore_mainline",
			FlowRefs:            []string{"sea_jebel_ali_singapore_mainline"},
			RequiredTokens:      []string{"jebel", "singapore"},
			PreferredTokens:     []string{"hormuz", "malacca", "jurong"},
			OriginAnchor:        coord(25.01, 55.06),
			OriginRadiusKm:      350,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 700,
			Coordinates: []models.Coordinates{
				coord(25.01, 55.06),
				coord(26.58, 56.30),
				coord(24.80, 58.90),
				coord(20.50, 63.00),
				coord(14.20, 68.40),
				coord(8.40, 73.60),
				coord(5.80, 78.90),
				coord(5.60, 84.60),
				coord(5.50, 90.50),
				coord(5.60, 95.20),
				coord(7.80, 97.80),
				coord(8.60, 99.20),
				coord(5.10, 100.40),
				coord(2.80, 102.40),
				coord(1.266, 103.855),
			},
		},
		{
			ID:                  "sea_ras_tanura_singapore_mainline",
			FlowRefs:            []string{"sea_ras_tanura_singapore_mainline"},
			RequiredTokens:      []string{"ras", "tanura", "singapore"},
			PreferredTokens:     []string{"hormuz", "malacca", "jurong"},
			OriginAnchor:        coord(26.65, 50.04),
			OriginRadiusKm:      250,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 700,
			Coordinates: []models.Coordinates{
				coord(26.65, 50.04),
				coord(26.58, 56.30),
				coord(24.80, 58.90),
				coord(20.50, 63.00),
				coord(14.20, 68.40),
				coord(8.40, 73.60),
				coord(5.80, 78.90),
				coord(5.60, 84.60),
				coord(5.50, 90.50),
				coord(5.60, 95.20),
				coord(7.80, 97.80),
				coord(8.60, 99.20),
				coord(5.10, 100.40),
				coord(2.80, 102.40),
				coord(1.266, 103.855),
			},
		},
		{
			ID:                  "sea_yokosuka_singapore_mainline",
			FlowRefs:            []string{"sea_yokosuka_singapore_mainline"},
			RequiredTokens:      []string{"singapore"},
			PreferredTokens:     []string{"yokosuka", "navsup", "navfac", "japan", "tokyo"},
			OriginAnchor:        coord(35.28, 139.67),
			OriginRadiusKm:      550,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 700,
			Coordinates: []models.Coordinates{
				coord(35.28, 139.67),
				coord(33.80, 136.00),
				coord(31.50, 132.50),
				coord(29.00, 129.50),
				coord(26.00, 126.00),
				coord(23.00, 122.00),
				coord(20.50, 120.00),
				coord(17.00, 118.00),
				coord(13.50, 114.50),
				coord(9.80, 111.00),
				coord(6.50, 107.50),
				coord(3.50, 104.50),
				coord(1.266, 103.855),
			},
		},
		{
			ID:                  "sea_norway_us_east_coast_mainline",
			FlowRefs:            []string{"sea_norway_us_east_coast_mainline"},
			RequiredTokens:      []string{"norfolk"},
			PreferredTokens:     []string{"horten", "norway", "atlantic", "east", "coast"},
			OriginAnchor:        coord(59.42, 10.48),
			OriginRadiusKm:      500,
			DestinationAnchor:   coord(36.95, -76.33),
			DestinationRadiusKm: 500,
			Coordinates: []models.Coordinates{
				coord(59.42, 10.48),
				coord(58.40, 5.20),
				coord(57.20, 1.00),
				coord(54.20, -4.50),
				coord(50.20, -8.50),
				coord(45.00, -18.00),
				coord(41.00, -29.00),
				coord(38.00, -41.00),
				coord(36.50, -54.00),
				coord(36.60, -66.00),
				coord(36.95, -76.33),
			},
		},
		{
			ID:                  "sea_norway_us_gulf_mainline",
			FlowRefs:            []string{"sea_norway_us_gulf_mainline"},
			RequiredTokens:      []string{"houston"},
			PreferredTokens:     []string{"mongstad", "norway", "gulf", "atlantic"},
			OriginAnchor:        coord(60.81, 5.02),
			OriginRadiusKm:      700,
			DestinationAnchor:   coord(29.73, -95.01),
			DestinationRadiusKm: 500,
			Coordinates: []models.Coordinates{
				coord(60.81, 5.02),
				coord(58.80, -0.50),
				coord(54.50, -7.50),
				coord(48.00, -17.00),
				coord(41.50, -31.00),
				coord(33.50, -48.00),
				coord(24.00, -63.00),
				coord(22.50, -76.00),
				coord(24.00, -85.00),
				coord(26.50, -91.00),
				coord(29.73, -95.01),
			},
		},
		{
			ID:                  "rail_carajas_efc_ponta_da_madeira",
			FlowRefs:            []string{"rail_carajas_efc_ponta_da_madeira"},
			RequiredTokens:      []string{"carajas"},
			PreferredTokens:     []string{"ponta", "madeira", "terminal", "railway", "vale"},
			OriginAnchor:        coord(-6.054627, -50.177514),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(-2.566523, -44.379141),
			DestinationRadiusKm: 260,
			Coordinates: []models.Coordinates{
				coord(-6.054627, -50.177514),
				coord(-5.35, -49.15),
				coord(-4.95, -47.50),
				coord(-4.05, -46.20),
				coord(-3.67, -45.38),
				coord(-2.80, -44.45),
				coord(-2.566523, -44.379141),
			},
		},
		{
			ID:                  "sea_brazil_shanghai_via_cape",
			FlowRefs:            []string{"sea_brazil_shanghai_via_cape"},
			RequiredTokens:      []string{"shanghai"},
			PreferredTokens:     []string{"carajas", "madeira", "brazil", "cape", "malacca"},
			OriginAnchor:        coord(-2.56, -44.37),
			OriginRadiusKm:      600,
			DestinationAnchor:   coord(30.617, 122.067),
			DestinationRadiusKm: 1200,
			Coordinates: []models.Coordinates{
				coord(-2.56, -44.37),
				coord(-8.00, -35.00),
				coord(-16.00, -20.00),
				coord(-24.00, -2.00),
				coord(-33.90, 18.40),
				coord(-28.00, 31.00),
				coord(-20.00, 43.00),
				coord(-10.00, 57.00),
				coord(-1.00, 72.00),
				coord(4.50, 85.00),
				coord(6.00, 95.00),
				coord(4.00, 101.00),
				coord(7.00, 108.00),
				coord(14.00, 114.00),
				coord(22.00, 119.00),
				coord(30.617, 122.067),
			},
		},
		{
			ID:                  "land_port_of_shanghai_baowu_dri",
			FlowRefs:            []string{"land_port_of_shanghai_baowu_dri"},
			RequiredTokens:      []string{"shanghai"},
			PreferredTokens:     []string{"dri", "baowu", "steel", "port"},
			OriginAnchor:        coord(30.626595, 122.063382),
			OriginRadiusKm:      160,
			DestinationAnchor:   coord(31.230416, 121.473701),
			DestinationRadiusKm: 120,
			Coordinates: []models.Coordinates{
				coord(30.626595, 122.063382),
				coord(30.75, 121.90),
				coord(30.93, 121.72),
				coord(31.08, 121.58),
				coord(31.230416, 121.473701),
			},
		},
		{
			ID:                  "land_daqing_fushun_refining_corridor",
			FlowRefs:            []string{"land_daqing_fushun_refining_corridor"},
			RequiredTokens:      []string{"daqing", "fushun"},
			PreferredTokens:     []string{"crude", "petrochemical", "refinery", "oilfield"},
			OriginAnchor:        coord(46.587580, 125.103070),
			OriginRadiusKm:      140,
			DestinationAnchor:   coord(41.873078, 123.885191),
			DestinationRadiusKm: 120,
			Coordinates: []models.Coordinates{
				coord(46.587580, 125.103070),
				coord(45.803775, 126.534967),
				coord(43.817084, 125.323542),
				coord(41.805699, 123.431472),
				coord(41.873078, 123.885191),
			},
		},
		{
			ID:                  "sea_hamburg_suez_via_gibraltar",
			FlowRefs:            []string{"sea_hamburg_suez_via_gibraltar"},
			RequiredTokens:      []string{"hamburg", "suez"},
			PreferredTokens:     []string{"europe", "north", "germany", "gibraltar"},
			OriginAnchor:        coord(53.509430, 9.965477),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(29.966790, 32.549828),
			DestinationRadiusKm: 220,
			Coordinates: composeCatalogCoordinates(
				maritimeHamburgToGibraltarIngress(),
				maritimeGibraltarToSuezTrunk(),
			),
		},
		{
			ID:                  "sea_felixstowe_suez_via_gibraltar",
			FlowRefs:            []string{"sea_felixstowe_suez_via_gibraltar"},
			RequiredTokens:      []string{"felixstowe", "suez"},
			PreferredTokens:     []string{"europe", "uk", "britain", "gibraltar"},
			OriginAnchor:        coord(51.955696, 1.312976),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(29.966790, 32.549828),
			DestinationRadiusKm: 220,
			Coordinates: composeCatalogCoordinates(
				maritimeFelixstoweToGibraltarIngress(),
				maritimeGibraltarToSuezTrunk(),
			),
		},
		{
			ID:                          "sea_us_gulf_jebel_ali_via_suez",
			FlowRefs:                    []string{"sea_us_gulf_jebel_ali_via_suez"},
			RequiredTokens:              []string{"jebel"},
			PreferredTokens:             []string{"houston", "baytown", "port", "arthur", "suez", "gulf"},
			SingleEndedMinPreferredHits: 2,
			OriginAnchor:                coord(29.73, -95.01),
			OriginRadiusKm:              500,
			DestinationAnchor:           coord(25.01, 55.06),
			DestinationRadiusKm:         400,
			Coordinates: composeCatalogCoordinates(
				maritimeUSGulfToSuezAtlantic(),
				maritimeSuezToHormuzTrunk(),
				maritimeHormuzToJebelAliFeeder(),
			),
		},
		{
			ID:                  "sea_us_gulf_diego_via_malacca",
			FlowRefs:            []string{"sea_us_gulf_diego_via_malacca"},
			RequiredTokens:      []string{"diego", "garcia"},
			PreferredTokens:     []string{"houston", "baytown", "port", "arthur", "malacca", "gulf"},
			OriginAnchor:        coord(29.73, -95.01),
			OriginRadiusKm:      500,
			DestinationAnchor:   coord(-7.313, 72.411),
			DestinationRadiusKm: 180,
			Coordinates: []models.Coordinates{
				coord(29.73, -95.01),
				coord(25.50, -88.00),
				coord(22.00, -82.00),
				coord(16.50, -78.00),
				coord(9.50, -79.50),
				coord(9.10, -79.68),
				coord(12.00, -95.00),
				coord(16.00, -115.00),
				coord(18.00, -135.00),
				coord(17.00, -155.00),
				coord(13.00, -175.00),
				coord(10.00, 165.00),
				coord(8.00, 145.00),
				coord(7.00, 125.00),
				coord(6.00, 108.00),
				coord(4.50, 101.00),
				coord(2.00, 90.00),
				coord(-2.00, 79.00),
				coord(-5.00, 74.00),
				coord(-7.313, 72.411),
			},
		},
		{
			ID:                  "sea_gulf_east_china_mainline",
			FlowRefs:            []string{"sea_gulf_east_china_mainline"},
			RequiredTokens:      []string{"hormuz"},
			PreferredTokens:     []string{"malacca", "singapore", "china", "shanghai", "ningbo"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(30.617, 122.067),
			DestinationRadiusKm: 1200,
			Coordinates: []models.Coordinates{
				coord(26.65, 50.04),
				coord(26.58, 56.30),
				coord(24.80, 58.90),
				coord(20.50, 63.00),
				coord(14.20, 68.40),
				coord(8.40, 73.60),
				coord(5.80, 78.90),
				coord(5.60, 84.60),
				coord(5.50, 90.50),
				coord(5.60, 95.20),
				coord(7.80, 97.80),
				coord(5.10, 100.40),
				coord(2.20, 103.60),
				coord(6.00, 107.80),
				coord(11.00, 110.70),
				coord(16.50, 113.80),
				coord(22.80, 118.60),
				coord(28.20, 121.30),
				coord(30.617, 122.067),
			},
		},
		{
			ID:                  "sea_singapore_diego_garcia_mainline",
			FlowRefs:            []string{"sea_singapore_diego_garcia_mainline"},
			RequiredTokens:      []string{"diego", "garcia"},
			PreferredTokens:     []string{"singapore", "jurong", "sembawang", "senoko", "malacca"},
			OriginAnchor:        coord(1.266, 103.855),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(-7.313, 72.411),
			DestinationRadiusKm: 180,
			Coordinates: []models.Coordinates{
				coord(1.266, 103.855),
				coord(1.20, 103.30),
				coord(1.90, 102.20),
				coord(3.40, 100.90),
				coord(5.20, 99.80),
				coord(7.10, 98.40),
				coord(8.20, 95.40),
				coord(7.10, 92.30),
				coord(5.00, 88.10),
				coord(2.00, 83.60),
				coord(-1.20, 79.30),
				coord(-4.30, 75.40),
				coord(-6.20, 73.30),
				coord(-7.313, 72.411),
			},
		},
		{
			ID:                  "sea_gulf_diego_garcia_mainline",
			FlowRefs:            []string{"sea_gulf_diego_garcia_mainline"},
			RequiredTokens:      []string{"diego", "garcia"},
			PreferredTokens:     []string{"hormuz", "jebel", "ras", "tanura", "gulf"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(-7.313, 72.411),
			DestinationRadiusKm: 180,
			Coordinates: []models.Coordinates{
				coord(25.30, 55.10),
				coord(26.58, 56.30),
				coord(24.20, 59.20),
				coord(19.00, 63.60),
				coord(13.20, 68.10),
				coord(8.80, 73.40),
				coord(5.80, 79.50),
				coord(2.60, 77.40),
				coord(-1.40, 75.30),
				coord(-4.90, 73.80),
				coord(-7.313, 72.411),
			},
		},
		{
			ID:                  "sea_gulf_rotterdam_via_suez",
			FlowRefs:            []string{"sea_gulf_rotterdam_via_suez"},
			RequiredTokens:      []string{"hormuz", "suez"},
			PreferredTokens:     []string{"rotterdam", "europe", "mediterranean", "gibraltar"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(51.885, 4.2867),
			DestinationRadiusKm: 1200,
			Coordinates: composeCatalogCoordinates(
				maritimeGulfToSuezMainline(),
				maritimeSuezToRotterdamAtlantic(),
			),
		},
		{
			ID:                  "sea_gulf_rotterdam_via_cape",
			FlowRefs:            []string{"sea_gulf_rotterdam_via_cape"},
			RequiredTokens:      []string{"cape"},
			PreferredTokens:     []string{"hormuz", "rotterdam", "europe"},
			OriginAnchor:        coord(26.4, 56.4),
			OriginRadiusKm:      1800,
			DestinationAnchor:   coord(51.885, 4.2867),
			DestinationRadiusKm: 1200,
			Coordinates: []models.Coordinates{
				coord(26.65, 50.04),
				coord(26.58, 56.30),
				coord(24.80, 58.90),
				coord(18.50, 61.00),
				coord(9.50, 65.00),
				coord(-2.50, 60.00),
				coord(-14.00, 47.50),
				coord(-24.50, 33.00),
				coord(-34.35, 18.50),
				coord(-28.50, 12.40),
				coord(-15.00, 3.80),
				coord(-2.50, -4.60),
				coord(12.50, -16.00),
				coord(31.00, -12.80),
				coord(43.80, -6.20),
				coord(49.80, -1.60),
				coord(51.20, 2.50),
				coord(51.885, 4.2867),
			},
		},
		{
			ID:                  "sea_jebel_ali_sohar_feeder",
			FlowRefs:            []string{"sea_jebel_ali_sohar_feeder"},
			RequiredTokens:      []string{"jebel", "sohar"},
			PreferredTokens:     []string{"dubai", "oman", "gulf", "hormuz"},
			OriginAnchor:        coord(25.074282, 55.188539),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 180,
			Coordinates: composeCatalogCoordinates(
				reverseCatalogCoordinates(maritimeHormuzToJebelAliFeeder()),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_suez_jebel_ali_mainline",
			FlowRefs:            []string{"sea_suez_jebel_ali_mainline"},
			RequiredTokens:      []string{"suez", "jebel"},
			PreferredTokens:     []string{"red", "arabian", "dubai", "hormuz"},
			OriginAnchor:        coord(29.966790, 32.549828),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(25.074282, 55.188539),
			DestinationRadiusKm: 220,
			Coordinates: composeCatalogCoordinates(
				maritimeSuezToHormuzTrunk(),
				maritimeHormuzToJebelAliFeeder(),
			),
		},
		{
			ID:                  "sea_suez_sohar_mainline",
			FlowRefs:            []string{"sea_suez_sohar_mainline"},
			RequiredTokens:      []string{"suez", "sohar"},
			PreferredTokens:     []string{"oman", "red", "arabian", "hormuz"},
			OriginAnchor:        coord(29.966790, 32.549828),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeSuezToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_hamburg_sohar_via_suez",
			FlowRefs:            []string{"sea_hamburg_sohar_via_suez"},
			RequiredTokens:      []string{"hamburg", "sohar"},
			PreferredTokens:     []string{"suez", "europe", "north", "germany"},
			OriginAnchor:        coord(53.509430, 9.965477),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeHamburgToGibraltarIngress(),
				maritimeGibraltarToSuezTrunk(),
				maritimeSuezToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_felixstowe_sohar_via_suez",
			FlowRefs:            []string{"sea_felixstowe_sohar_via_suez"},
			RequiredTokens:      []string{"felixstowe", "sohar"},
			PreferredTokens:     []string{"suez", "europe", "uk", "britain"},
			OriginAnchor:        coord(51.955696, 1.312976),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeFelixstoweToGibraltarIngress(),
				maritimeGibraltarToSuezTrunk(),
				maritimeSuezToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_belawan_singapore_feeder",
			FlowRefs:            []string{"sea_belawan_singapore_feeder"},
			RequiredTokens:      []string{"belawan", "singapore"},
			PreferredTokens:     []string{"sumatra", "medan", "indonesia"},
			OriginAnchor:        coord(3.784722, 98.694722),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 220,
			Coordinates:         maritimeBelawanToSingaporeFeeder(),
		},
		{
			ID:                  "sea_tanjung_priok_singapore_feeder",
			FlowRefs:            []string{"sea_tanjung_priok_singapore_feeder"},
			RequiredTokens:      []string{"tanjung", "priok", "singapore"},
			PreferredTokens:     []string{"jakarta", "indonesia", "java"},
			OriginAnchor:        coord(-6.104444, 106.880556),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 220,
			Coordinates:         maritimeTanjungPriokToSingaporeFeeder(),
		},
		{
			ID:                  "sea_ho_chi_minh_singapore_feeder",
			FlowRefs:            []string{"sea_ho_chi_minh_singapore_feeder"},
			RequiredTokens:      []string{"minh", "singapore"},
			PreferredTokens:     []string{"vietnam", "chi", "ho", "saigon"},
			OriginAnchor:        coord(10.823099, 106.629664),
			OriginRadiusKm:      260,
			DestinationAnchor:   coord(1.266, 103.855),
			DestinationRadiusKm: 220,
			Coordinates:         maritimeHoChiMinhToSingaporeFeeder(),
		},
		{
			ID:                  "sea_singapore_marseille_via_suez",
			FlowRefs:            []string{"sea_singapore_marseille_via_suez"},
			RequiredTokens:      []string{"singapore", "marseille"},
			PreferredTokens:     []string{"suez", "mediterranean", "france"},
			OriginAnchor:        coord(1.266, 103.855),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(43.296482, 5.369780),
			DestinationRadiusKm: 260,
			Coordinates: composeCatalogCoordinates(
				reverseCatalogCoordinates(maritimeSingaporeToMalaccaFeeder()),
				maritimeMalaccaToHormuzTrunk(),
				reverseCatalogCoordinates(maritimeSuezToHormuzTrunk()),
				maritimeSuezToMarseilleMed(),
			),
		},
		{
			ID:                  "sea_singapore_genoa_via_suez",
			FlowRefs:            []string{"sea_singapore_genoa_via_suez"},
			RequiredTokens:      []string{"singapore", "genoa"},
			PreferredTokens:     []string{"suez", "mediterranean", "italy"},
			OriginAnchor:        coord(1.266, 103.855),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(44.404833, 8.917847),
			DestinationRadiusKm: 260,
			Coordinates: composeCatalogCoordinates(
				reverseCatalogCoordinates(maritimeSingaporeToMalaccaFeeder()),
				maritimeMalaccaToHormuzTrunk(),
				reverseCatalogCoordinates(maritimeSuezToHormuzTrunk()),
				maritimeSuezToGenoaMed(),
			),
		},
		{
			ID:                  "sea_singapore_rotterdam_via_suez",
			FlowRefs:            []string{"sea_singapore_rotterdam_via_suez"},
			RequiredTokens:      []string{"singapore", "rotterdam"},
			PreferredTokens:     []string{"suez", "gibraltar", "europe", "netherlands"},
			OriginAnchor:        coord(1.266, 103.855),
			OriginRadiusKm:      220,
			DestinationAnchor:   coord(51.885, 4.2867),
			DestinationRadiusKm: 260,
			Coordinates: composeCatalogCoordinates(
				reverseCatalogCoordinates(maritimeSingaporeToMalaccaFeeder()),
				maritimeMalaccaToHormuzTrunk(),
				reverseCatalogCoordinates(maritimeSuezToHormuzTrunk()),
				maritimeSuezToRotterdamAtlantic(),
			),
		},
		{
			ID:                  "sea_shanghai_malacca_mainline",
			FlowRefs:            []string{"sea_shanghai_malacca_mainline"},
			RequiredTokens:      []string{"shanghai", "malacca"},
			PreferredTokens:     []string{"china", "port", "east", "asia"},
			OriginAnchor:        coord(30.626595, 122.063382),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(5.100000, 100.400000),
			DestinationRadiusKm: 500,
			Coordinates:         maritimeShanghaiToMalaccaMainline(),
		},
		{
			ID:                  "sea_yokohama_malacca_mainline",
			FlowRefs:            []string{"sea_yokohama_malacca_mainline"},
			RequiredTokens:      []string{"yokohama", "malacca"},
			PreferredTokens:     []string{"japan", "port", "east", "asia"},
			OriginAnchor:        coord(35.450000, 139.646111),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(5.100000, 100.400000),
			DestinationRadiusKm: 500,
			Coordinates:         maritimeYokohamaToMalaccaMainline(),
		},
		{
			ID:                  "sea_malacca_jebel_ali_mainline",
			FlowRefs:            []string{"sea_malacca_jebel_ali_mainline"},
			RequiredTokens:      []string{"malacca", "jebel"},
			PreferredTokens:     []string{"arabian", "dubai", "oman", "hormuz"},
			OriginAnchor:        coord(5.100000, 100.400000),
			OriginRadiusKm:      500,
			DestinationAnchor:   coord(25.074282, 55.188539),
			DestinationRadiusKm: 220,
			Coordinates: composeCatalogCoordinates(
				maritimeMalaccaToHormuzTrunk(),
				maritimeHormuzToJebelAliFeeder(),
			),
		},
		{
			ID:                  "sea_shanghai_sohar_via_malacca",
			FlowRefs:            []string{"sea_shanghai_sohar_via_malacca"},
			RequiredTokens:      []string{"shanghai", "sohar"},
			PreferredTokens:     []string{"malacca", "china", "port", "oman"},
			OriginAnchor:        coord(30.626595, 122.063382),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeShanghaiToMalaccaMainline(),
				maritimeMalaccaToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_yokohama_sohar_via_malacca",
			FlowRefs:            []string{"sea_yokohama_sohar_via_malacca"},
			RequiredTokens:      []string{"yokohama", "sohar"},
			PreferredTokens:     []string{"malacca", "japan", "port", "oman"},
			OriginAnchor:        coord(35.450000, 139.646111),
			OriginRadiusKm:      180,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeYokohamaToMalaccaMainline(),
				maritimeMalaccaToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
		{
			ID:                  "sea_malacca_sohar_mainline",
			FlowRefs:            []string{"sea_malacca_sohar_mainline"},
			RequiredTokens:      []string{"malacca", "sohar"},
			PreferredTokens:     []string{"japan", "china", "shanghai", "yokohama", "oman"},
			OriginAnchor:        coord(5.10, 100.40),
			OriginRadiusKm:      500,
			DestinationAnchor:   coord(24.484851, 56.612114),
			DestinationRadiusKm: 240,
			Coordinates: composeCatalogCoordinates(
				maritimeMalaccaToHormuzTrunk(),
				maritimeHormuzToSoharFeeder(),
			),
		},
	}
}

func composeCatalogCoordinates(segments ...[]models.Coordinates) []models.Coordinates {
	out := make([]models.Coordinates, 0, 32)
	for _, segment := range segments {
		for _, point := range segment {
			if len(out) > 0 {
				prev := out[len(out)-1]
				if absRouteDelta(prev.Lat-point.Lat) < 1e-7 && absRouteDelta(prev.Lon-point.Lon) < 1e-7 {
					continue
				}
			}
			out = append(out, point)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func reverseCatalogCoordinates(coords []models.Coordinates) []models.Coordinates {
	if len(coords) == 0 {
		return nil
	}
	out := make([]models.Coordinates, 0, len(coords))
	for idx := len(coords) - 1; idx >= 0; idx-- {
		out = append(out, coords[idx])
	}
	return out
}

func absRouteDelta(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maritimeHamburgToGibraltarIngress() []models.Coordinates {
	return []models.Coordinates{
		coord(53.509430, 9.965477),
		coord(52.50, 4.50),
		coord(50.90, 0.50),
		coord(43.80, -6.20),
	}
}

func maritimeFelixstoweToGibraltarIngress() []models.Coordinates {
	return []models.Coordinates{
		coord(51.955696, 1.312976),
		coord(50.90, 0.50),
		coord(43.80, -6.20),
	}
}

func maritimeGibraltarToSuezTrunk() []models.Coordinates {
	return []models.Coordinates{
		coord(43.80, -6.20),
		coord(36.00, 5.60),
		coord(33.50, 29.50),
		coord(30.75, 32.35),
	}
}

func maritimeSuezToRotterdamAtlantic() []models.Coordinates {
	return []models.Coordinates{
		coord(30.75, 32.35),
		coord(33.50, 29.50),
		coord(36.10, 18.60),
		coord(36.00, 5.60),
		coord(43.80, -6.20),
		coord(49.80, -1.60),
		coord(51.20, 2.50),
		coord(51.885, 4.2867),
	}
}

func maritimeUSGulfToSuezAtlantic() []models.Coordinates {
	return []models.Coordinates{
		coord(29.73, -95.01),
		coord(27.00, -89.00),
		coord(25.20, -82.00),
		coord(28.00, -72.00),
		coord(33.00, -57.00),
		coord(36.00, -38.00),
		coord(36.00, -14.00),
		coord(36.00, 0.00),
		coord(35.20, 14.00),
		coord(31.00, 28.00),
		coord(30.75, 32.35),
	}
}

func maritimeGulfToSuezMainline() []models.Coordinates {
	return []models.Coordinates{
		coord(26.65, 50.04),
		coord(26.58, 56.30),
		coord(24.80, 58.90),
		coord(18.50, 61.00),
		coord(13.20, 49.40),
		coord(12.65, 43.45),
		coord(15.20, 41.10),
		coord(20.80, 38.80),
		coord(26.70, 35.10),
		coord(29.80, 32.55),
		coord(30.75, 32.35),
	}
}

func maritimeSuezToHormuzTrunk() []models.Coordinates {
	return []models.Coordinates{
		coord(29.966790, 32.549828),
		coord(27.90, 34.50),
		coord(22.00, 38.00),
		coord(15.20, 41.10),
		coord(12.65, 43.45),
		coord(13.20, 49.40),
		coord(18.50, 61.00),
		coord(26.58, 56.30),
	}
}

func maritimeHormuzToJebelAliFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(26.58, 56.30),
		coord(25.60, 55.85),
		coord(25.074282, 55.188539),
	}
}

func maritimeHormuzToSoharFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(26.58, 56.30),
		coord(25.80, 56.95),
		coord(24.484851, 56.612114),
	}
}

func maritimeSingaporeToMalaccaFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(1.266, 103.855),
		coord(2.80, 102.40),
		coord(5.10, 100.40),
	}
}

func maritimeBelawanToSingaporeFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(3.784722, 98.694722),
		coord(4.20, 99.30),
		coord(3.20, 101.10),
		coord(1.266, 103.855),
	}
}

func maritimeTanjungPriokToSingaporeFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(-6.104444, 106.880556),
		coord(-3.20, 106.10),
		coord(-0.20, 104.80),
		coord(1.266, 103.855),
	}
}

func maritimeHoChiMinhToSingaporeFeeder() []models.Coordinates {
	return []models.Coordinates{
		coord(10.823099, 106.629664),
		coord(8.30, 106.60),
		coord(5.10, 105.40),
		coord(2.40, 104.20),
		coord(1.266, 103.855),
	}
}

func maritimeShanghaiToMalaccaMainline() []models.Coordinates {
	return []models.Coordinates{
		coord(30.626595, 122.063382),
		coord(28.20, 121.30),
		coord(22.80, 118.60),
		coord(16.50, 113.80),
		coord(11.00, 110.70),
		coord(6.00, 107.80),
		coord(2.20, 103.60),
		coord(5.10, 100.40),
	}
}

func maritimeYokohamaToMalaccaMainline() []models.Coordinates {
	return []models.Coordinates{
		coord(35.450000, 139.646111),
		coord(32.00, 134.20),
		coord(28.00, 128.60),
		coord(22.50, 123.00),
		coord(15.50, 116.50),
		coord(9.80, 109.20),
		coord(3.20, 102.80),
		coord(5.10, 100.40),
	}
}

func maritimeSuezToMarseilleMed() []models.Coordinates {
	return []models.Coordinates{
		coord(30.75, 32.35),
		coord(31.80, 29.80),
		coord(33.30, 24.00),
		coord(35.00, 18.20),
		coord(37.40, 12.70),
		coord(40.20, 8.20),
		coord(43.296482, 5.369780),
	}
}

func maritimeSuezToGenoaMed() []models.Coordinates {
	return []models.Coordinates{
		coord(30.75, 32.35),
		coord(31.80, 29.80),
		coord(33.30, 24.00),
		coord(35.00, 18.20),
		coord(37.80, 14.00),
		coord(40.30, 11.80),
		coord(42.70, 10.10),
		coord(44.404833, 8.917847),
	}
}

func maritimeMalaccaToHormuzTrunk() []models.Coordinates {
	return []models.Coordinates{
		coord(5.10, 100.40),
		coord(7.80, 97.80),
		coord(5.60, 95.20),
		coord(5.50, 90.50),
		coord(5.60, 84.60),
		coord(5.80, 78.90),
		coord(8.40, 73.60),
		coord(14.20, 68.40),
		coord(20.50, 63.00),
		coord(24.80, 58.90),
		coord(26.58, 56.30),
	}
}

func coord(lat, lon float64) models.Coordinates {
	return models.Coordinates{Lat: lat, Lon: lon}
}
