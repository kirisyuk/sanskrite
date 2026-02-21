package telemetry

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/http"
)

// OpenCorporatesTelemetryWrapper wraps an OpenCorporates client and records hit/miss/error telemetry.
type OpenCorporatesTelemetryWrapper struct {
	Base enrich.OpenCorporatesClientInterface
	Repo *repo.HorizonTelemetryRepository
}

// NewOpenCorporatesTelemetryWrapper creates a wrapper that records external service telemetry.
func NewOpenCorporatesTelemetryWrapper(base enrich.OpenCorporatesClientInterface, telemetryRepo *repo.HorizonTelemetryRepository) enrich.OpenCorporatesClientInterface {
	if base == nil {
		return nil
	}
	return &OpenCorporatesTelemetryWrapper{Base: base, Repo: telemetryRepo}
}

func (w *OpenCorporatesTelemetryWrapper) SearchCompanies(ctx context.Context, query string, perPage int) (*http.OpenCorporatesSearchResponse, error) {
	resp, err := w.Base.SearchCompanies(ctx, query, perPage)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "opencorporates", OutcomeError)
		return nil, err
	}
	if resp != nil && len(resp.Results.Companies) > 0 {
		RecordExternalServiceCall(ctx, w.Repo, "opencorporates", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "opencorporates", OutcomeMiss)
	}
	return resp, nil
}
