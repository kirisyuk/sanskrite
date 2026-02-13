package wikidata

import (
	"context"

	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// Adapter адаптирует http.WikidataClient к ontology.WikidataClientInterface
// Размещен в отдельном пакете для избежания циклического импорта
type Adapter struct {
	client *httphandlers.WikidataClient
}

// NewAdapter создает новый адаптер
func NewAdapter(client *httphandlers.WikidataClient) *Adapter {
	return &Adapter{client: client}
}

// Search выполняет поиск в Wikidata
func (a *Adapter) Search(ctx context.Context, query string, maxResults int) (*ontology.WikidataSearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}

	// Преобразуем результаты
	results := make([]ontology.WikidataSearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = ontology.WikidataSearchResult{
			ID:          r.ID,
			Title:       r.Title,
			Label:       r.Label,
			Description: r.Description,
			URL:         r.URL,
		}
	}

	return &ontology.WikidataSearchResponse{
		Query:   resp.Query,
		Results: results,
	}, nil
}

// GetEntityData получает полные данные Entity из Wikidata
func (a *Adapter) GetEntityData(ctx context.Context, qID string) (*ontology.WikidataEntityData, error) {
	data, err := a.client.GetEntityData(ctx, qID)
	if err != nil {
		return nil, err
	}

	return &ontology.WikidataEntityData{
		ID:            data.ID,
		Label:         data.Label,
		Description:   data.Description,
		URL:           data.URL,
		Properties:    data.Properties,
		PropertiesRaw: data.PropertiesRaw,
		Aliases:       data.Aliases,
	}, nil
}

// ConvertToEntity конвертирует Wikidata данные в Entity
func (a *Adapter) ConvertToEntity(ctx context.Context, entityData *ontology.WikidataEntityData, tenantID string) (*models.Entity, error) {
	// Преобразуем в http.WikidataEntityData
	httpData := &httphandlers.WikidataEntityData{
		ID:            entityData.ID,
		Label:         entityData.Label,
		Description:   entityData.Description,
		URL:           entityData.URL,
		Properties:    entityData.Properties,
		PropertiesRaw: entityData.PropertiesRaw,
	}

	return a.client.ConvertToEntity(ctx, httpData, tenantID)
}

