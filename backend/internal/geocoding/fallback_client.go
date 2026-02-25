package geocoding

import (
	"context"
	"log"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// FallbackClient tries the primary geocoder first, falls back to secondary on miss/error.
// Designed for Nominatim (free) as primary and Google Geocoding (paid) as fallback.
type FallbackClient struct {
	primary   ontology.GeocodingClientInterface
	fallback  ontology.GeocodingClientInterface
	logger    *log.Logger
}

// NewFallbackClient creates a geocoding client that tries primary first, then fallback.
// If primary is nil, only fallback is used (and vice-versa).
func NewFallbackClient(primary, fallback ontology.GeocodingClientInterface, logger *log.Logger) ontology.GeocodingClientInterface {
	if primary == nil && fallback == nil {
		return nil
	}
	if primary == nil {
		return fallback
	}
	if fallback == nil {
		return primary
	}
	return &FallbackClient{primary: primary, fallback: fallback, logger: logger}
}

// Geocode tries the primary geocoder. On nil result or error, falls back to secondary.
func (c *FallbackClient) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	result, err := c.primary.Geocode(ctx, address)
	if err == nil && result != nil {
		return result, nil
	}

	if err != nil && c.logger != nil {
		c.logger.Printf("geocoding primary error for %q: %v, trying fallback", address, err)
	}

	return c.fallback.Geocode(ctx, address)
}
