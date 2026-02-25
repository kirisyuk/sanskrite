package telemetry

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// GeocodingTelemetryWrapper wraps a Geocoding client and records hit/miss/error telemetry.
type GeocodingTelemetryWrapper struct {
	Base ontology.GeocodingClientInterface
	Repo *repo.HorizonTelemetryRepository
}

// NewGeocodingTelemetryWrapper creates a wrapper that records external service telemetry.
func NewGeocodingTelemetryWrapper(base ontology.GeocodingClientInterface, telemetryRepo *repo.HorizonTelemetryRepository) ontology.GeocodingClientInterface {
	if base == nil {
		return nil
	}
	return &GeocodingTelemetryWrapper{Base: base, Repo: telemetryRepo}
}

// Geocode implements ontology.GeocodingClientInterface.
func (w *GeocodingTelemetryWrapper) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	result, err := w.Base.Geocode(ctx, address)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "google_geocoding", OutcomeError)
		return nil, err
	}
	if result != nil {
		RecordExternalServiceCall(ctx, w.Repo, "google_geocoding", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "google_geocoding", OutcomeMiss)
	}
	return result, nil
}
