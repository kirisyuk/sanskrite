package telemetry

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// NominatimTelemetryWrapper wraps a Nominatim geocoding client and records hit/miss/error telemetry.
type NominatimTelemetryWrapper struct {
	Base ontology.GeocodingClientInterface
	Repo *repo.HorizonTelemetryRepository
}

// NewNominatimTelemetryWrapper creates a wrapper that records external service telemetry for Nominatim.
func NewNominatimTelemetryWrapper(base ontology.GeocodingClientInterface, telemetryRepo *repo.HorizonTelemetryRepository) ontology.GeocodingClientInterface {
	if base == nil {
		return nil
	}
	return &NominatimTelemetryWrapper{Base: base, Repo: telemetryRepo}
}

// Geocode implements ontology.GeocodingClientInterface.
func (w *NominatimTelemetryWrapper) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	result, err := w.Base.Geocode(ctx, address)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "nominatim", OutcomeError)
		return nil, err
	}
	if result != nil {
		RecordExternalServiceCall(ctx, w.Repo, "nominatim", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "nominatim", OutcomeMiss)
	}
	return result, nil
}
