package telemetry

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// GeoNamesTelemetryWrapper wraps a GeoNames client and records hit/miss/error telemetry.
type GeoNamesTelemetryWrapper struct {
	Base  ontology.GeoNamesClientInterface
	Repo  *repo.HorizonTelemetryRepository
}

// NewGeoNamesTelemetryWrapper creates a wrapper that records external service telemetry.
func NewGeoNamesTelemetryWrapper(base ontology.GeoNamesClientInterface, telemetryRepo *repo.HorizonTelemetryRepository) ontology.GeoNamesClientInterface {
	if base == nil {
		return nil
	}
	return &GeoNamesTelemetryWrapper{Base: base, Repo: telemetryRepo}
}

func (w *GeoNamesTelemetryWrapper) Search(ctx context.Context, query string, maxResults int, opts ontology.GeoNamesSearchOptions) (*ontology.GeoNamesSearchResponse, error) {
	resp, err := w.Base.Search(ctx, query, maxResults, opts)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeError)
		return nil, err
	}
	if resp != nil && len(resp.Results) > 0 {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeMiss)
	}
	return resp, nil
}

func (w *GeoNamesTelemetryWrapper) GetPlace(ctx context.Context, geonameID int) (*ontology.GeoNamesPlace, error) {
	place, err := w.Base.GetPlace(ctx, geonameID)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeError)
		return nil, err
	}
	if place != nil {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeMiss)
	}
	return place, nil
}

func (w *GeoNamesTelemetryWrapper) GetHierarchy(ctx context.Context, geonameID int) (*ontology.GeoNamesHierarchyResponse, error) {
	resp, err := w.Base.GetHierarchy(ctx, geonameID)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeError)
		return nil, err
	}
	if resp != nil && len(resp.Results) > 0 {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "geonames", OutcomeMiss)
	}
	return resp, nil
}
