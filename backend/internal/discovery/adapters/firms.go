package adapters

import (
	"encoding/json"
)

// FIRMSAdapter normalizes NASA FIRMS (regions) into flattened items.
type FIRMSAdapter struct{}

func (a *FIRMSAdapter) FeedType() string { return "nasa_firms_fires" }

func (a *FIRMSAdapter) Normalize(body []byte) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	regions, ok := raw["regions"].([]any)
	if !ok || len(regions) == 0 {
		return nil, nil
	}
	// Flatten regions into items
	var items []any
	for _, r := range regions {
		if m, ok := r.(map[string]any); ok {
			items = append(items, m)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	return map[string]any{"items": items}, nil
}
