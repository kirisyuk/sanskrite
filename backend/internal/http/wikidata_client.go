package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// WikidataClient handles communication with Wikidata API
type WikidataClient struct {
	httpClient     *http.Client
	sparqlEndpoint string
	apiEndpoint    string
	searchEndpoint string
}

// WikidataSearchResult represents a single search result from Wikidata
type WikidataSearchResult struct {
	ID          string `json:"id"`          // Q-identifier (e.g., "Q42")
	Title       string `json:"title"`       // Wikidata title
	Label       string `json:"label"`       // Human-readable label
	Description string `json:"description"` // Description
	URL         string `json:"url"`         // Wikidata URL
}

// WikidataSearchResponse represents the response from Wikidata search API
type WikidataSearchResponse struct {
	Query        string                 `json:"query"`
	Results      []WikidataSearchResult `json:"results"`
	ResponseTime float64                `json:"response_time,omitempty"`
}

// WikidataEntityData represents full entity data from Wikidata
type WikidataEntityData struct {
	ID            string                 `json:"id"`
	Label         string                 `json:"label"`
	Description   string                 `json:"description"`
	URL           string                 `json:"url"`
	Type          string                 `json:"type,omitempty"`          // Instance of (e.g., "Q5" for human)
	TypeLabel     string                 `json:"typeLabel,omitempty"`     // Human-readable type (e.g., "human")
	Properties    map[string]string      `json:"properties"`              // Property ID -> Formatted Value
	PropertiesRaw map[string]interface{} `json:"propertiesRaw,omitempty"` // Property ID -> Raw Value (for complex structures)
}

// NewWikidataClient creates a new Wikidata API client
func NewWikidataClient() *WikidataClient {
	return &WikidataClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		sparqlEndpoint: "https://query.wikidata.org/sparql",
		apiEndpoint:    "https://www.wikidata.org/w/api.php",
		searchEndpoint: "https://www.wikidata.org/w/api.php",
	}
}

// GetWikidataClientFromEnv creates a Wikidata client (no API key needed for public API)
func GetWikidataClientFromEnv() *WikidataClient {
	return NewWikidataClient()
}

// Search performs a text search on Wikidata and returns results with full entity data
func (c *WikidataClient) Search(ctx context.Context, query string, maxResults int) (*WikidataSearchResponse, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}

	startTime := time.Now()

	// Step 1: Search for entities
	searchResults, err := c.searchEntities(ctx, query, maxResults)
	if err != nil {
		return nil, fmt.Errorf("wikidata search failed: %w", err)
	}

	// Step 2: Load full entity data for top results (limit to 20 for performance)
	entitiesToLoad := maxResults
	if entitiesToLoad > 20 {
		entitiesToLoad = 20
	}

	var results []WikidataSearchResult
	for i, searchItem := range searchResults {
		if i >= entitiesToLoad {
			break
		}

		// Load full entity data
		entityData, err := c.getEntityData(ctx, searchItem.ID)
		if err != nil {
			// If loading fails, use search result as fallback
			results = append(results, WikidataSearchResult{
				ID:          searchItem.ID,
				Title:       searchItem.Title,
				Label:       searchItem.Label,
				Description: searchItem.Description,
				URL:         searchItem.URL,
			})
			continue
		}

		// Build description from entity data
		description := entityData.Description
		if description == "" {
			description = searchItem.Description
		}

		results = append(results, WikidataSearchResult{
			ID:          entityData.ID,
			Title:       entityData.Label,
			Label:       entityData.Label,
			Description: description,
			URL:         entityData.URL,
		})
	}

	responseTime := time.Since(startTime).Seconds()

	return &WikidataSearchResponse{
		Query:        query,
		Results:      results,
		ResponseTime: responseTime,
	}, nil
}

// searchEntities performs a text search on Wikidata API
func (c *WikidataClient) searchEntities(ctx context.Context, searchQuery string, limit int) ([]WikidataSearchResult, error) {
	params := url.Values{}
	params.Set("action", "wbsearchentities")
	params.Set("search", searchQuery)
	params.Set("language", "en")
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.searchEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", "Sanskrite/1.0 (https://sanskrite.com)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("wikidata search failed with status %d: %s", resp.StatusCode, string(body))
	}

	var searchResult struct {
		Search []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Label       string `json:"label"`
			Description string `json:"description"`
			URL         string `json:"url"`
		} `json:"search"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	var results []WikidataSearchResult
	for _, item := range searchResult.Search {
		docURL := item.URL
		if docURL == "" {
			docURL = fmt.Sprintf("https://www.wikidata.org/wiki/%s", item.ID)
		}

		results = append(results, WikidataSearchResult{
			ID:          item.ID,
			Title:       item.Title,
			Label:       item.Label,
			Description: item.Description,
			URL:         docURL,
		})
	}

	return results, nil
}

// GetEntityData retrieves full entity data from Wikidata Entity API (public method)
func (c *WikidataClient) GetEntityData(ctx context.Context, qID string) (*WikidataEntityData, error) {
	return c.getEntityData(ctx, qID)
}

// getEntityData retrieves full entity data from Wikidata Entity API (internal)
func (c *WikidataClient) getEntityData(ctx context.Context, qID string) (*WikidataEntityData, error) {
	entityURL := fmt.Sprintf("https://www.wikidata.org/wiki/Special:EntityData/%s.json", qID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entityURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Sanskrite/1.0 (https://sanskrite.com)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("entity fetch failed with status %d", resp.StatusCode)
	}

	var entityData struct {
		Entities map[string]struct {
			ID     string `json:"id"`
			Labels map[string]struct {
				Value string `json:"value"`
			} `json:"labels"`
			Descriptions map[string]struct {
				Value string `json:"value"`
			} `json:"descriptions"`
			Claims map[string][]struct {
				Mainsnak struct {
					Datavalue struct {
						Value interface{} `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
			Sitelinks map[string]struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&entityData); err != nil {
		return nil, fmt.Errorf("failed to decode entity response: %w", err)
	}

	for entityID, entity := range entityData.Entities {
		// Get English label and description
		label := ""
		if enLabel, ok := entity.Labels["en"]; ok {
			label = enLabel.Value
		} else if len(entity.Labels) > 0 {
			for _, l := range entity.Labels {
				label = l.Value
				break
			}
		}

		description := ""
		if enDesc, ok := entity.Descriptions["en"]; ok {
			description = enDesc.Value
		} else if len(entity.Descriptions) > 0 {
			for _, d := range entity.Descriptions {
				description = d.Value
				break
			}
		}

		// Extract key properties
		// Priority properties (always extracted): P231, P274, P31, P279, P452, P366, P10565, P186
		// Plus: P1448 (official name), P1813 (short name) for name normalization.
		priorityProps := map[string]bool{
			"P231":  true, // CAS Registry Number
			"P274":  true, // Chemical formula
			"P31":   true, // Instance of
			"P279":  true, // Subclass of
			"P452":  true, // Industry
			"P366":  true, // Has use
			"P10565": true, // Is produced from
			"P186":  true, // Material used
			"P1448": true, // Official name
			"P1813": true, // Short name
		}

		properties := make(map[string]string)
		propertiesRaw := make(map[string]interface{})
		propCount := 0
		
		// First pass: extract priority properties (all values for multi-value props)
		for propID, claims := range entity.Claims {
			if !priorityProps[propID] {
				continue
			}
			if len(claims) == 0 {
				continue
			}
			
			// For multi-value properties, collect all values.
			// IMPORTANT: P452 (industry) is multi-valued for many organizations and is required for industry taxonomy mapping.
			if propID == "P31" || propID == "P279" || propID == "P366" || propID == "P10565" || propID == "P186" || propID == "P452" || propID == "P176" || propID == "P1448" || propID == "P1813" {
				var values []string
				var rawValues []interface{}
				for _, claim := range claims {
					if claim.Mainsnak.Datavalue.Value != nil {
						value := claim.Mainsnak.Datavalue.Value
						if entityID := c.extractEntityIDFromValue(value); entityID != "" {
							values = append(values, entityID)
							rawValues = append(rawValues, entityID)
						} else {
							valueStr := fmt.Sprintf("%v", value)
							values = append(values, valueStr)
							rawValues = append(rawValues, value)
						}
					}
				}
				if len(values) > 0 {
					properties[propID] = values[0] // First value for backward compatibility
					propertiesRaw[propID] = rawValues // All values for product extractor
				}
			} else {
				// Single-value properties: take first claim
				claim := claims[0]
				if claim.Mainsnak.Datavalue.Value != nil {
					value := claim.Mainsnak.Datavalue.Value
					if entityID := c.extractEntityIDFromValue(value); entityID != "" {
						properties[propID] = entityID
						propertiesRaw[propID] = entityID
					} else {
						valueStr := fmt.Sprintf("%v", value)
						properties[propID] = valueStr
						propertiesRaw[propID] = value
					}
				}
			}
			propCount++
		}

		// Second pass: extract other properties (limit to 20 total)
		for propID, claims := range entity.Claims {
			if priorityProps[propID] {
				continue // Already extracted
			}
			if propCount >= 20 {
				break
			}
			if len(claims) == 0 {
				continue
			}
			// Берем первое значение claim для каждого свойства
			claim := claims[0]
			if claim.Mainsnak.Datavalue.Value != nil {
				// Извлекаем значение в зависимости от типа
				value := claim.Mainsnak.Datavalue.Value

				// Для entity-value извлекаем ID
				if entityID := c.extractEntityIDFromValue(value); entityID != "" {
					properties[propID] = entityID
					propertiesRaw[propID] = entityID
				} else {
					// Для других типов сохраняем как есть
					valueStr := fmt.Sprintf("%v", value)
					properties[propID] = valueStr
					propertiesRaw[propID] = value
				}

				propCount++
			}
		}

		// Определяем тип сущности (instance of - P31)
		entityType := ""
		typeLabel := ""
		if instanceOfClaims, ok := entity.Claims["P31"]; ok && len(instanceOfClaims) > 0 {
			if instanceOfClaim := instanceOfClaims[0]; instanceOfClaim.Mainsnak.Datavalue.Value != nil {
				entityType = c.extractEntityIDFromValue(instanceOfClaim.Mainsnak.Datavalue.Value)
				if entityType != "" {
					typeLabel = c.getEntityTypeLabel(entityType)
				}
			}
		}

		// Get Wikipedia URL if available
		docURL := fmt.Sprintf("https://www.wikidata.org/wiki/%s", entityID)
		if enwiki, ok := entity.Sitelinks["enwiki"]; ok {
			docURL = enwiki.URL
		}

		return &WikidataEntityData{
			ID:            entityID,
			Label:         label,
			Description:   description,
			URL:           docURL,
			Type:          entityType,
			TypeLabel:     typeLabel,
			Properties:    properties,
			PropertiesRaw: propertiesRaw,
		}, nil
	}

	return nil, fmt.Errorf("entity %s not found", qID)
}

// ============================================================================
// Преобразование данных Wikidata в онтологические объекты
// ============================================================================

// ConvertToEntity преобразует данные Wikidata в Entity (онтологическая модель)
// tenantID должен быть "global" или "system" для Wikidata данных
func (c *WikidataClient) ConvertToEntity(ctx context.Context, entityData *WikidataEntityData, tenantID string) (*models.Entity, error) {
	if entityData == nil {
		return nil, fmt.Errorf("entityData is nil")
	}

	if tenantID == "" {
		tenantID = "global" // По умолчанию глобальный для Wikidata
	}

	now := time.Now().UTC()

	// Определяем тип сущности из Wikidata (instance of - P31)
	entityType := models.EntityTypeOther
	if instanceOf, ok := entityData.PropertiesRaw["P31"]; ok {
		// Извлекаем Q-ID из значения
		if typeID := c.extractEntityIDFromValue(instanceOf); typeID != "" {
			entityType = c.mapWikidataTypeToEntityType(typeID)
		} else if typeStr, ok := instanceOf.(string); ok && strings.HasPrefix(typeStr, "Q") {
			// Fallback: если значение уже строка
			entityType = c.mapWikidataTypeToEntityType(typeStr)
		}
	}

	// Создаем KB reference для Wikidata
	kbRefs := []models.KBReference{
		{
			KB:         "Wikidata",
			ExternalID: entityData.ID,
			Confidence: 1.0,
			LastSync:   &now,
		},
	}

	// Извлекаем атрибуты из свойств Wikidata
	attributes := make(map[string]interface{})
	for propID, value := range entityData.PropertiesRaw {
		// Пропускаем служебные свойства
		if propID == "P31" { // instance of уже обработан
			continue
		}
		attributes[propID] = value
	}

	// Извлекаем идентификаторы (например, ORCID, ISIN и т.д.)
	identifiers := make(map[string]string)
	// Маппинг известных свойств Wikidata на схемы идентификаторов
	identifierProps := map[string]string{
		"P496":  "ORCID",    // ORCID iD
		"P213":  "ISNI",     // ISNI
		"P214":  "VIAF",     // VIAF ID
		"P227":  "GND",      // GND ID
		"P244":  "LC",       // Library of Congress authority ID
		"P2427": "GRID",     // GRID ID
		"P3500": "Ringgold", // Ringgold ID
	}
	for propID, scheme := range identifierProps {
		if value, ok := entityData.Properties[propID]; ok {
			identifiers[scheme] = value
		}
	}

	// Извлекаем координаты для локаций (P625)
	var homeLocation *models.Location
	if coords, ok := entityData.PropertiesRaw["P625"]; ok {
		// P625 - coordinate location, формат: {"latitude": X, "longitude": Y}
		if coordsMap, ok := coords.(map[string]interface{}); ok {
			if lat, latOk := coordsMap["latitude"]; latOk {
				if lon, lonOk := coordsMap["longitude"]; lonOk {
					iso6709 := fmt.Sprintf("%+g%+g/", lat, lon)
					homeLocation = &models.Location{
						Name:    entityData.Label,
						ISO6709: &iso6709,
					}
				}
			}
		}
	}

	entity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			ID:          entityData.ID,
			TenantID:    tenantID,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
			CreatedBy:   "system",
			UpdatedBy:   "system",
			Labels:      map[string]string{"en": entityData.Label},
			Description: entityData.Description,
			Tags:        []string{},
			Status:      "active",
		},
		CanonicalName: entityData.Label,
		Type:          entityType,
		KBRefs:        kbRefs,
		Identifiers:   identifiers,
		Attributes:    attributes,
		HomeLocation:  homeLocation,
		Governance: &models.Governance{
			DataQuality: models.DataQualityHigh, // Wikidata имеет высокое качество данных
		},
	}

	return entity, nil
}

// mapWikidataTypeToEntityType маппит Q-идентификаторы Wikidata на типы Entity
func (c *WikidataClient) mapWikidataTypeToEntityType(qID string) models.EntityType {
	typeMap := map[string]models.EntityType{
		"Q5":        models.EntityTypePerson,       // human
		"Q43229":    models.EntityTypeOrganization, // organization
		"Q2424752":  models.EntityTypeProduct,      // product
		"Q515":      models.EntityTypeLocation,     // city
		"Q6256":     models.EntityTypeLocation,     // country
		"Q17334923": models.EntityTypeLocation,     // location
		"Q1190554":  models.EntityTypeEventEntity,  // occurrence
		"Q49848":    models.EntityTypeDocument,     // document
		"Q13226383": models.EntityTypeFacility,     // facility
	}
	if entityType, ok := typeMap[qID]; ok {
		return entityType
	}
	return models.EntityTypeOther
}

// getEntityTypeLabel возвращает читаемое название типа из Wikidata
func (c *WikidataClient) getEntityTypeLabel(qID string) string {
	labelMap := map[string]string{
		"Q5":        "Person",
		"Q43229":    "Organization",
		"Q2424752":  "Product",
		"Q515":      "City",
		"Q6256":     "Country",
		"Q17334923": "Location",
		"Q1190554":  "Event",
		"Q49848":    "Document",
		"Q13226383": "Facility",
	}
	if label, ok := labelMap[qID]; ok {
		return label
	}
	return "Other"
}

// ConvertToRelations преобразует связи Wikidata в Relation объекты
func (c *WikidataClient) ConvertToRelations(ctx context.Context, entityData *WikidataEntityData, tenantID string) ([]*models.Relation, error) {
	if entityData == nil {
		return nil, fmt.Errorf("entityData is nil")
	}

	if tenantID == "" {
		tenantID = "global"
	}

	now := time.Now().UTC()
	var relations []*models.Relation

	// Преобразуем свойства Wikidata в Relation объекты
	for propID, value := range entityData.PropertiesRaw {
		// Пропускаем служебные свойства
		if propID == "P31" { // instance of обрабатывается отдельно
			continue
		}

		// Получаем предикат из словаря Wikidata properties
		predicate := c.getPredicateLabel(propID)

		// Извлекаем Q-идентификатор из значения
		objectID := c.extractEntityIDFromValue(value)

		if objectID != "" {
			relation := &models.Relation{
				BaseOntologyFields: models.BaseOntologyFields{
					ID:        fmt.Sprintf("%s-%s-%s", entityData.ID, propID, objectID),
					TenantID:  tenantID,
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
					CreatedBy: "system",
					UpdatedBy: "system",
					Labels:    map[string]string{"en": predicate},
					Status:    "active",
				},
				SubjectID:  entityData.ID,
				Predicate:  predicate,
				ObjectID:   objectID,
				Direction:  models.RelationDirectionSubjectToObject,
				Confidence: 1.0,
				Provenance: models.Provenance{
					SourceIDs:  []string{"wikidata"},
					ObservedAt: &now,
					Method:     "KBAlign",
				},
			}
			relations = append(relations, relation)
		}
	}

	return relations, nil
}

// extractEntityIDFromValue извлекает Q-идентификатор из значения Wikidata
func (c *WikidataClient) extractEntityIDFromValue(value interface{}) string {
	// Wikidata entity-value имеет структуру: {"entity-type": "item", "id": "Q42", "numeric-id": 42}
	if valueMap, ok := value.(map[string]interface{}); ok {
		if entityType, ok := valueMap["entity-type"].(string); ok && entityType == "item" {
			if id, ok := valueMap["id"].(string); ok && strings.HasPrefix(id, "Q") {
				return id
			}
		}
		// Альтернативный формат: просто строка с Q-ID
		if id, ok := valueMap["id"].(string); ok && strings.HasPrefix(id, "Q") {
			return id
		}
	}
	// Если значение - строка с Q-ID
	if idStr, ok := value.(string); ok && strings.HasPrefix(idStr, "Q") {
		return idStr
	}
	return ""
}

// getPredicateLabel возвращает читаемое название предиката из Wikidata property ID
func (c *WikidataClient) getPredicateLabel(propID string) string {
	// Основные свойства Wikidata
	predicateMap := map[string]string{
		"P31":   "instanceOf",
		"P569":  "dateOfBirth",
		"P570":  "dateOfDeath",
		"P19":   "placeOfBirth",
		"P20":   "placeOfDeath",
		"P106":  "occupation",
		"P27":   "countryOfCitizenship",
		"P17":   "country",
		"P36":   "capital",
		"P1082": "population",
		"P625":  "coordinateLocation",
		"P571":  "inception",
		"P159":  "headquartersLocation",
		"P112":  "foundedBy",
		"P169":  "chiefExecutiveOfficer",
		"P1086": "atomicNumber",
		"P246":  "elementSymbol",
		"P166":  "awardReceived",
	}
	if predicate, ok := predicateMap[propID]; ok {
		return predicate
	}
	return fmt.Sprintf("wikidata:%s", propID) // Fallback к ID свойства
}
