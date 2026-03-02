package adapters

import (
	"encoding/json"
)

// UCDPAdapter normalizes UCDP GED (Result.data) into items.
type UCDPAdapter struct{}

func (a *UCDPAdapter) FeedType() string { return "ucdp_ged_events" }

func (a *UCDPAdapter) Normalize(body []byte) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	// UCDP Result structure
	res, ok := raw["Result"].(map[string]any)
	if !ok {
		return nil, nil
	}
	data, ok := res["data"].([]any)
	if !ok || len(data) == 0 {
		return nil, nil
	}
	return map[string]any{"items": data}, nil
}
