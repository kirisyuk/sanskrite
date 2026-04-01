package geocoding

import (
	"context"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// FallbackClient tries the primary geocoder first, falls back to secondary on miss/error.
// Runtime is wired with Google Geocoding as primary and Nominatim as fallback.
type FallbackClient struct {
	primary  ontology.GeocodingClientInterface
	fallback ontology.GeocodingClientInterface
	logger   *log.Logger
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
		ensureGeocodingProvider(result, ontology.GeocodingProviderGoogle)
		return result, nil
	}

	if err != nil && c.logger != nil {
		c.logger.Printf("geocoding primary error for %q: %v, trying fallback", address, err)
	}

	result, err = c.fallback.Geocode(ctx, address)
	if err != nil || result == nil {
		return result, err
	}
	ensureGeocodingProvider(result, ontology.GeocodingProviderNominatim)
	return result, nil
}

func ensureGeocodingProvider(result *ontology.GeocodingResult, fallback string) {
	if result == nil {
		return
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = strings.TrimSpace(fallback)
	}
}
