package adapters

import (
	"encoding/json"
)

// AISAdapter normalizes AIS snapshot (disruptions/density) into events.
// Plan §8: EventAdapter for ais_snapshot.
type AISAdapter struct{}

func (a *AISAdapter) FeedType() string { return "ais_snapshot" }

func (a *AISAdapter) Normalize(body []byte) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	// AIS: disruptions or density arrays as events
	events := make([]any, 0)
	if d, ok := raw["disruptions"].([]any); ok {
		events = append(events, d...)
	}
	if d, ok := raw["density"].([]any); ok {
		events = append(events, d...)
	}
	if len(events) == 0 {
		return nil, nil
	}
	return map[string]any{"events": events}, nil
}
