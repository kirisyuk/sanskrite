// Package adapters provides feed-type-specific normalizers for Horizon Monitor ingestion.
// Plan §8: OpenSky, USGS, UCDP, FIRMS, AIS — transform external API formats into our core.
package adapters

import "sync"

// FeedAdapter normalizes a raw feed body into items/events for the Horizon Monitor pipeline.
type FeedAdapter interface {
	// FeedType returns the adapter's feed type (e.g. "opensky_flights", "usgs_earthquakes").
	FeedType() string
	// Normalize converts raw body into a map with "items" or "events" key, or nil to use default parsing.
	Normalize(body []byte) (map[string]any, error)
}

var (
	registry   = make(map[string]FeedAdapter)
	registryMu sync.RWMutex
)

// Register adds an adapter for feed type.
func Register(a FeedAdapter) {
	if a == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[a.FeedType()] = a
}

// Get returns the adapter for feed type, or nil.
func Get(feedType string) FeedAdapter {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[feedType]
}

// Normalize uses the adapter for feedType if registered; otherwise returns nil (use default).
func Normalize(feedType string, body []byte) (map[string]any, error) {
	a := Get(feedType)
	if a == nil {
		return nil, nil
	}
	return a.Normalize(body)
}

func init() {
	Register(&OpenSkyAdapter{})
	Register(&USGSAdapter{})
	Register(&UCDPAdapter{})
	Register(&FIRMSAdapter{})
	Register(&AISAdapter{})
}
