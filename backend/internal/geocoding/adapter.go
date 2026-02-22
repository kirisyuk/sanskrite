package geocoding

import (
	"context"

	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// Adapter adapts http.GoogleGeocodingClient to ontology.GeocodingClientInterface.
type Adapter struct {
	client *httphandlers.GoogleGeocodingClient
}

// NewAdapter creates a new adapter.
func NewAdapter(client *httphandlers.GoogleGeocodingClient) *Adapter {
	return &Adapter{client: client}
}

// Geocode implements ontology.GeocodingClientInterface.
func (a *Adapter) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	if a == nil || a.client == nil {
		return nil, nil
	}
	resp, err := a.client.Geocode(ctx, address)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return &ontology.GeocodingResult{
		Lat:              resp.Lat,
		Lon:              resp.Lon,
		FormattedAddress: resp.FormattedAddress,
		PlaceID:          resp.PlaceID,
	}, nil
}
