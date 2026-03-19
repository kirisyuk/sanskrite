package pipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

var entsogCountryPattern = regexp.MustCompile(`\(([A-Z]{2})\)`)

func ensureEUGasHub() *models.Facility {
	return &models.Facility{
		ID:            "criticalchain_eu_gas_hub",
		CanonicalPath: "/fac/eu/eu-gas-pipelines-hub",
		Name:          "EU Gas Pipelines Hub",
		FacilityKind:  taxonomy.FacilityKindPipeline,
		FacilityRole:  taxonomy.FacilityRoleLogistics,
		Country:       "EU",
		Coordinates:   &latlng.LatLng{Latitude: 50.0, Longitude: 10.0},
		MaterialID:    "natural_gas",
		Stage:         2,
		RiskScore:     2.5,
		UpdatedAt:     time.Now(),
	}
}

func enrichENTSOGFacility(fac *models.Facility, raw map[string]interface{}) {
	if fac == nil {
		return
	}
	if fac.Country == "" || fac.Country == "EU" {
		if code := extractENTSOGCountryCode(raw); code != "" {
			fac.Country = code
		}
	}
	if fac.Coordinates == nil {
		if center, ok := entsogCountryCenters[fac.Country]; ok {
			fac.Coordinates = &latlng.LatLng{
				Latitude:  center[0],
				Longitude: center[1],
			}
		}
	}
}

func buildENTSOGRoute(hub, fac *models.Facility, raw map[string]interface{}) *models.Route {
	if hub == nil || fac == nil || hub.Coordinates == nil || fac.Coordinates == nil {
		return nil
	}
	pointKey := strings.TrimSpace(stringVal(raw, "pointKey"))
	if pointKey == "" {
		pointKey = fac.ID
	}
	return &models.Route{
		ID:   fmt.Sprintf("route_entsog_%s", sanitizeID(pointKey)),
		Type: models.RouteTypePipeline,
		Waypoints: []*latlng.LatLng{
			hub.Coordinates,
			fac.Coordinates,
		},
		FromFacility:  hub.ID,
		ToFacility:    fac.ID,
		TypicalVolume: floatVal(raw, "value"),
		VolumeUnit:    stringVal(raw, "unit"),
		RiskScore:     2.5,
		UpdatedAt:     time.Now(),
	}
}

func extractENTSOGCountryCode(raw map[string]interface{}) string {
	for _, key := range []string{"pointLabel", "operatorLabel"} {
		label := stringVal(raw, key)
		if matches := entsogCountryPattern.FindStringSubmatch(label); len(matches) == 2 {
			return matches[1]
		}
	}
	operatorKey := strings.TrimSpace(stringVal(raw, "operatorKey"))
	if len(operatorKey) >= 2 {
		return strings.ToUpper(operatorKey[:2])
	}
	return ""
}

func sanitizeID(s string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", "(", "", ")", "", ":", "_")
	return replacer.Replace(strings.TrimSpace(strings.ToLower(s)))
}

func stringVal(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok && v != nil {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		case int:
			return strconv.Itoa(x)
		case float64:
			return strings.TrimSpace(strconv.FormatFloat(x, 'f', -1, 64))
		}
	}
	return ""
}

func floatVal(m map[string]interface{}, k string) float64 {
	if v, ok := m[k]; ok && v != nil {
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		case string:
			f, _ := strconv.ParseFloat(x, 64)
			return f
		}
	}
	return 0
}

var entsogCountryCenters = map[string][2]float64{
	"AT": {47.6, 14.1},
	"BE": {50.8, 4.5},
	"BG": {42.7, 25.2},
	"CH": {46.8, 8.3},
	"CZ": {49.8, 15.5},
	"DE": {51.2, 10.4},
	"DK": {56.2, 9.5},
	"EE": {58.7, 25.0},
	"ES": {40.2, -3.5},
	"FI": {64.5, 26.0},
	"FR": {46.2, 2.2},
	"GB": {54.5, -2.5},
	"GR": {39.1, 22.9},
	"HR": {45.4, 16.4},
	"HU": {47.1, 19.4},
	"IE": {53.2, -8.2},
	"IT": {42.8, 12.5},
	"LT": {55.2, 23.9},
	"LU": {49.8, 6.1},
	"LV": {56.9, 24.6},
	"NL": {52.1, 5.3},
	"NO": {61.0, 8.0},
	"PL": {52.1, 19.4},
	"PT": {39.6, -8.0},
	"RO": {45.9, 24.9},
	"SE": {62.0, 15.0},
	"SI": {46.1, 14.8},
	"SK": {48.7, 19.7},
	"TR": {39.0, 35.2},
	"UA": {49.0, 31.0},
}
