package adapters

import (
	"encoding/json"
	"fmt"
)

// OpenSkyAdapter normalizes OpenSky states array (array of arrays) into objects.
// Plan §8.8: indices 0=icao24, 1=callsign, 2=origin_country, 5=lon, 6=lat, 7=baro_altitude, 9=velocity, 10=true_track.
type OpenSkyAdapter struct{}

func (a *OpenSkyAdapter) FeedType() string { return "opensky_flights" }

func (a *OpenSkyAdapter) Normalize(body []byte) (map[string]any, error) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var arr []any
	switch v := raw.(type) {
	case []any:
		arr = v
	case map[string]any:
		if s, ok := v["states"].([]any); ok {
			arr = s
		}
	}
	if len(arr) == 0 {
		return nil, nil
	}
	items := make([]any, 0, len(arr))
	for _, v := range arr {
		row, ok := v.([]any)
		if !ok || len(row) < 11 {
			continue
		}
		obj := map[string]any{
			"icao24":        strAt(row, 0),
			"callsign":      strAt(row, 1),
			"origin_country": strAt(row, 2),
			"longitude":     numAt(row, 5),
			"latitude":      numAt(row, 6),
			"baro_altitude": numAt(row, 7),
			"velocity":      numAt(row, 9),
			"true_track":    numAt(row, 10),
		}
		items = append(items, obj)
	}
	if len(items) == 0 {
		return nil, nil
	}
	return map[string]any{"items": items}, nil
}

func strAt(row []any, i int) string {
	if i >= len(row) || row[i] == nil {
		return ""
	}
	return fmt.Sprint(row[i])
}

func numAt(row []any, i int) float64 {
	if i >= len(row) || row[i] == nil {
		return 0
	}
	switch v := row[i].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}
