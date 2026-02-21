package telemetry

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// WikidataTelemetryWrapper wraps a Wikidata client and records hit/miss/error telemetry.
type WikidataTelemetryWrapper struct {
	Base ontology.WikidataClientInterface
	Repo *repo.HorizonTelemetryRepository
}

// NewWikidataTelemetryWrapper creates a wrapper that records external service telemetry.
func NewWikidataTelemetryWrapper(base ontology.WikidataClientInterface, telemetryRepo *repo.HorizonTelemetryRepository) ontology.WikidataClientInterface {
	if base == nil {
		return nil
	}
	return &WikidataTelemetryWrapper{Base: base, Repo: telemetryRepo}
}

func (w *WikidataTelemetryWrapper) Search(ctx context.Context, query string, maxResults int) (*ontology.WikidataSearchResponse, error) {
	resp, err := w.Base.Search(ctx, query, maxResults)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeError)
		return nil, err
	}
	if resp != nil && len(resp.Results) > 0 {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeMiss)
	}
	return resp, nil
}

func (w *WikidataTelemetryWrapper) GetEntityData(ctx context.Context, qID string) (*ontology.WikidataEntityData, error) {
	data, err := w.Base.GetEntityData(ctx, qID)
	if err != nil {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeError)
		return nil, err
	}
	if data != nil {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeHit)
	} else {
		RecordExternalServiceCall(ctx, w.Repo, "wikidata", OutcomeMiss)
	}
	return data, nil
}

func (w *WikidataTelemetryWrapper) ConvertToEntity(ctx context.Context, entityData *ontology.WikidataEntityData, tenantID string) (*models.Entity, error) {
	return w.Base.ConvertToEntity(ctx, entityData, tenantID)
}
