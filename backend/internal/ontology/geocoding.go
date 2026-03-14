package ontology

import "context"

const (
	GeocodingProviderGoogle    = "google_geocoding"
	GeocodingProviderNominatim = "nominatim"
)

// GeocodingClientInterface is an interface for address-to-coordinates geocoding.
// Placed in ontology package to avoid cyclic imports.
type GeocodingClientInterface interface {
	// Geocode converts an address string to lat/lon. Returns nil if address not found.
	Geocode(ctx context.Context, address string) (*GeocodingResult, error)
}

// GeocodingResult represents a geocoded location.
type GeocodingResult struct {
	Lat              float64
	Lon              float64
	FormattedAddress string
	PlaceID          string
	Provider         string
}
