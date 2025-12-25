package ontology

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// WikidataClientInterface интерфейс для работы с Wikidata (избегаем циклического импорта)
type WikidataClientInterface interface {
	Search(ctx context.Context, query string, maxResults int) (*WikidataSearchResponse, error)
	GetEntityData(ctx context.Context, qID string) (*WikidataEntityData, error)
	ConvertToEntity(ctx context.Context, entityData *WikidataEntityData, tenantID string) (*models.Entity, error)
}

// WikidataSearchResponse ответ от поиска Wikidata
type WikidataSearchResponse struct {
	Query   string                 `json:"query"`
	Results []WikidataSearchResult `json:"results"`
}

// WikidataSearchResult результат поиска
type WikidataSearchResult struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Label       string `json:"label"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// WikidataEntityData данные Entity из Wikidata
type WikidataEntityData struct {
	ID            string                 `json:"id"`
	Label         string                 `json:"label"`
	Description   string                 `json:"description"`
	URL           string                 `json:"url"`
	Properties    map[string]string      `json:"properties"`
	PropertiesRaw map[string]interface{} `json:"propertiesRaw,omitempty"`
}

// EntityEnricher обогащает Entity данными из внешних источников (Wikidata)
type EntityEnricher struct {
	entitiesRepo   *repo.EntityRepository
	wikidataClient WikidataClientInterface
	logger         *log.Logger
}

// NewEntityEnricher создает новый обогатитель entities
func NewEntityEnricher(
	entitiesRepo *repo.EntityRepository,
	wikidataClient WikidataClientInterface,
	logger *log.Logger,
) *EntityEnricher {
	return &EntityEnricher{
		entitiesRepo:   entitiesRepo,
		wikidataClient: wikidataClient,
		logger:         logger,
	}
}

// EnrichEntity обогащает Entity данными из Wikidata
func (e *EntityEnricher) EnrichEntity(
	ctx context.Context,
	tenantID string,
	entity *models.Entity,
) error {
	return e.enrichEntity(ctx, tenantID, entity, false)
}

// EnrichEntityForce обогащает Entity данными из Wikidata, игнорируя TTL (для ручного обновления).
func (e *EntityEnricher) EnrichEntityForce(
	ctx context.Context,
	tenantID string,
	entity *models.Entity,
) error {
	return e.enrichEntity(ctx, tenantID, entity, true)
}

func (e *EntityEnricher) enrichEntity(
	ctx context.Context,
	tenantID string,
	entity *models.Entity,
	force bool,
) error {
	// Проверяем, не обогащен ли уже Entity
	if !force && len(entity.KBRefs) > 0 {
		// Проверяем, есть ли уже Wikidata ссылка
		for _, ref := range entity.KBRefs {
			if ref.KB == "Wikidata" && ref.LastSync != nil {
				// Проверяем, не устарели ли данные (синхронизируем раз в неделю)
				if time.Since(*ref.LastSync) < 7*24*time.Hour {
					return nil // Данные свежие
				}
			}
		}
	}

	// Ищем Entity в Wikidata
	searchResponse, err := e.wikidataClient.Search(ctx, entity.CanonicalName, 5)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[EntityEnricher] Failed to search Wikidata for %s: %v", entity.CanonicalName, err)
		}
		return fmt.Errorf("wikidata search failed: %w", err)
	}

	if searchResponse == nil || len(searchResponse.Results) == 0 {
		// Entity не найден в Wikidata
		return nil
	}

	// Берем первый результат (наиболее релевантный)
	topResult := searchResponse.Results[0]

	// Получаем полные данные Entity из Wikidata
	wikidataEntityData, err := e.wikidataClient.GetEntityData(ctx, topResult.ID)
	if err != nil {
		// Если не удалось получить полные данные, используем данные из поиска
		wikidataEntityData = &WikidataEntityData{
			ID:          topResult.ID,
			Label:       topResult.Label,
			Description: topResult.Description,
			URL:         topResult.URL,
			Properties:  make(map[string]string),
			PropertiesRaw: make(map[string]interface{}),
		}
	}

	if wikidataEntityData == nil {
		return nil
	}

	// Конвертируем Wikidata данные в наш Entity формат
	enrichedEntity, err := e.wikidataClient.ConvertToEntity(ctx, wikidataEntityData, tenantID)
	if err != nil {
		return fmt.Errorf("failed to convert wikidata entity: %w", err)
	}

	// Объединяем данные
	e.mergeEntityData(entity, enrichedEntity)

	// Обновляем KBRefs
	now := time.Now().UTC()
	
	// Ищем ExternalID из enrichedEntity или используем ID из поиска
	externalID := topResult.ID
	if len(enrichedEntity.KBRefs) > 0 {
		externalID = enrichedEntity.KBRefs[0].ExternalID
	}
	
	kbRef := models.KBReference{
		KB:         "Wikidata",
		ExternalID: externalID,
		Confidence: 0.9, // Высокая уверенность для Wikidata
		LastSync:   &now,
	}

	// Добавляем или обновляем KBRef
	found := false
	for i, ref := range entity.KBRefs {
		if ref.KB == "Wikidata" {
			entity.KBRefs[i] = kbRef
			found = true
			break
		}
	}
	if !found {
		entity.KBRefs = append(entity.KBRefs, kbRef)
	}

	// Обновляем Entity
	entity.UpdatedAt = time.Now().UTC()
	entity.UpdatedBy = "system"

	if err := e.entitiesRepo.Upsert(ctx, entity); err != nil {
		return fmt.Errorf("failed to update entity: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[EntityEnricher] Enriched entity %s with Wikidata data", entity.ID)
	}

	return nil
}

// mergeEntityData объединяет данные из обогащенного Entity в существующий
func (e *EntityEnricher) mergeEntityData(existing, enriched *models.Entity) {
	// Объединяем Aliases
	aliasMap := make(map[string]bool)
	for _, alias := range existing.Aliases {
		aliasMap[alias] = true
	}
	for _, alias := range enriched.Aliases {
		if !aliasMap[alias] {
			existing.Aliases = append(existing.Aliases, alias)
		}
	}

	// Объединяем Attributes
	if existing.Attributes == nil {
		existing.Attributes = make(map[string]interface{})
	}
	for k, v := range enriched.Attributes {
		if _, exists := existing.Attributes[k]; !exists {
			existing.Attributes[k] = v
		}
	}

	// Объединяем Identifiers
	if existing.Identifiers == nil {
		existing.Identifiers = make(map[string]string)
	}
	for k, v := range enriched.Identifiers {
		if _, exists := existing.Identifiers[k]; !exists {
			existing.Identifiers[k] = v
		}
	}

	// Обновляем Description если пустая
	if existing.Description == "" && enriched.Description != "" {
		existing.Description = enriched.Description
	}

	// Обновляем HomeLocation если пустая
	if existing.HomeLocation == nil && enriched.HomeLocation != nil {
		existing.HomeLocation = enriched.HomeLocation
	}
}

