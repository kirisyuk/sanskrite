package adapters

import (
	"encoding/json"
)

// USGSAdapter normalizes USGS GeoJSON (features array) into items.
type USGSAdapter struct{}

func (a *USGSAdapter) FeedType() string { return "usgs_earthquakes" }

func (a *USGSAdapter) Normalize(body []byte) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	features, ok := raw["features"].([]any)
	if !ok || len(features) == 0 {
		return nil, nil
	}
	return map[string]any{"items": features}, nil
}
