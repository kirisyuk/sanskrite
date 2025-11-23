package ontology

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/domain"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// OntologyExtractor преобразует извлеченные данные в онтологические объекты
// Это основной компонент для создания Entity, Event, Claim из различных источников
type OntologyExtractor struct {
	entitiesRepo     *repo.EntityRepository
	eventsRepo       *repo.EventRepository
	claimsRepo       *repo.ClaimRepository
	relationsRepo    *repo.RelationRepository
	sourcesRepo      *repo.SourceRepository
	entityResolver   *EntityResolver
	entityEnricher   *EntityEnricher
	trustIntegrator  *TrustIntegrator
	conflictDetector *ConflictDetector
	causalDetector   *CausalDetector
	domainService    *domain.DomainService
	logger           *log.Logger
}

// NewOntologyExtractor создает новый экстрактор онтологии
func NewOntologyExtractor(
	entitiesRepo *repo.EntityRepository,
	eventsRepo *repo.EventRepository,
	claimsRepo *repo.ClaimRepository,
	relationsRepo *repo.RelationRepository,
	sourcesRepo *repo.SourceRepository,
	entityResolver *EntityResolver,
	entityEnricher *EntityEnricher,
	trustIntegrator *TrustIntegrator,
	conflictDetector *ConflictDetector,
	causalDetector *CausalDetector,
	domainService *domain.DomainService,
	logger *log.Logger,
) *OntologyExtractor {
	return &OntologyExtractor{
		entitiesRepo:     entitiesRepo,
		eventsRepo:       eventsRepo,
		claimsRepo:       claimsRepo,
		relationsRepo:    relationsRepo,
		sourcesRepo:      sourcesRepo,
		entityResolver:   entityResolver,
		entityEnricher:   entityEnricher,
		trustIntegrator:  trustIntegrator,
		conflictDetector: conflictDetector,
		causalDetector:   causalDetector,
		domainService:    domainService,
		logger:           logger,
	}
}

// ExtractFromDocument извлекает онтологические объекты из Document
// Заменяет старую логику сохранения в Document.Attrs
func (e *OntologyExtractor) ExtractFromDocument(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedData *DocumentExtraction,
) error {
	now := time.Now().UTC()

	// Создаем или находим Source для документа
	sourceID, err := e.ensureSource(ctx, tenantID, document)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to ensure source: %v", err)
		}
		// Продолжаем без source
		sourceID = ""
	}

	// Извлекаем Entities
	entityIDs, err := e.extractEntities(ctx, tenantID, document, extractedData.Entities, sourceID, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract entities: %v", err)
		}
	}

	// Извлекаем Events
	eventIDs, err := e.extractEvents(ctx, tenantID, document, extractedData.Events, sourceID, entityIDs, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract events: %v", err)
		}
	}

	// Извлекаем Claims
	err = e.extractClaims(ctx, tenantID, document, extractedData.Claims, sourceID, entityIDs, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract claims: %v", err)
		}
	}

	if e.logger != nil {
		e.logger.Printf("[OntologyExtractor] Extracted from document %s: %d entities, %d events, %d claims",
			document.ID, len(entityIDs), len(eventIDs), len(extractedData.Claims))
	}

	return nil
}

// ExtractFromChat извлекает онтологические объекты из чат-сообщения
// Вызывается асинхронно после обработки сообщения
func (e *OntologyExtractor) ExtractFromChat(
	ctx context.Context,
	tenantID string,
	projectID string,
	interaction *repo.Interaction,
	extractedData *ChatExtraction,
) error {
	now := time.Now().UTC()

	// Создаем Source для чата
	sourceID, err := e.ensureChatSource(ctx, tenantID, projectID)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to ensure chat source: %v", err)
		}
		sourceID = ""
	}

	// Извлекаем Entities
	entityIDs, err := e.extractEntitiesFromChat(ctx, tenantID, interaction, extractedData.Entities, sourceID, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract entities from chat: %v", err)
		}
	}

	// Извлекаем Events
	eventIDs, err := e.extractEventsFromChat(ctx, tenantID, interaction, extractedData.Events, sourceID, entityIDs, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract events from chat: %v", err)
		}
	}

	// Извлекаем Claims
	err = e.extractClaimsFromChat(ctx, tenantID, interaction, extractedData.Claims, sourceID, entityIDs, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Failed to extract claims from chat: %v", err)
		}
	}

	if e.logger != nil {
		e.logger.Printf("[OntologyExtractor] Extracted from chat interaction %s: %d entities, %d events, %d claims",
			interaction.ID, len(entityIDs), len(eventIDs), len(extractedData.Claims))
	}

	return nil
}

// DocumentExtraction структура извлеченных данных из документа
type DocumentExtraction struct {
	Entities []ExtractedEntity `json:"entities"`
	Events   []ExtractedEvent  `json:"events"`
	Claims   []ExtractedClaim  `json:"claims"`
}

// ChatExtraction структура извлеченных данных из чата
type ChatExtraction struct {
	Entities []ExtractedEntity `json:"entities"`
	Events   []ExtractedEvent  `json:"events"`
	Claims   []ExtractedClaim  `json:"claims"`
}

// ExtractedEntity извлеченная сущность
type ExtractedEntity struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// ExtractedEvent извлеченное событие
type ExtractedEvent struct {
	Title   string  `json:"title"`
	DateISO string  `json:"dateISO"`
	Context *string `json:"context,omitempty"`
}

// ExtractedClaim извлеченное утверждение
type ExtractedClaim struct {
	Text          string   `json:"text"`
	Summary       *string  `json:"summary,omitempty"`
	Confidence    float64  `json:"confidence"`
	EvidenceURLs  []string `json:"evidenceUrls,omitempty"`
}

// extractEntities создает Entity объекты из извлеченных данных
func (e *OntologyExtractor) extractEntities(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedEntities []ExtractedEntity,
	sourceID string,
	now time.Time,
) (map[string]string, error) {
	entityIDs := make(map[string]string) // canonicalName -> entityID

	for _, ext := range extractedEntities {
		if ext.Value == "" {
			continue
		}

		// Нормализуем имя
		canonicalName := strings.TrimSpace(ext.Value)
		if canonicalName == "" {
			continue
		}

		// Определяем тип Entity
		entityType := e.mapExtractedTypeToEntityType(ext.Type)

		// Используем EntityResolver для поиска существующего Entity
		existing, err := e.entityResolver.ResolveEntity(ctx, tenantID, canonicalName, entityType)
		if err == nil && existing != nil {
			entityIDs[canonicalName] = existing.ID
			continue
		}

		// Создаем новый Entity
		entity := &models.Entity{
			BaseOntologyFields: models.BaseOntologyFields{
				TenantID:  tenantID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				CreatedBy: "system",
				UpdatedBy: "system",
				Labels:    map[string]string{"en": canonicalName},
				Status:    "active",
			},
			CanonicalName: canonicalName,
			Type:          entityType,
			Aliases:       []string{canonicalName}, // Начальный алиас
			Attributes: map[string]interface{}{
				"extractedFrom": "document",
				"sourceId":     document.ID,    // ID документа как источника
				"sourceType":   "document",     // тип источника
				"dataSourceId": sourceID,       // ID DataSource (если есть)
			},
		}

		entityID, err := e.entitiesRepo.Create(ctx, entity)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Failed to create entity %s: %v", canonicalName, err)
			}
			continue
		}

		entityIDs[canonicalName] = entityID

		// Обновляем entity.ID для использования в обогащении и классификации
		entity.ID = entityID

		// Автоматическое обогащение из Wikidata (асинхронно)
		if e.entityEnricher != nil {
			go func() {
				_ = e.entityEnricher.EnrichEntity(context.Background(), tenantID, entity)
			}()
		}

		// Автоматическая классификация домена
		if e.domainService != nil {
			contextText := document.FullText
			if len(contextText) > 1000 {
				contextText = contextText[:1000] // Ограничиваем для промпта
			}
			go func() {
				_ = e.domainService.ClassifyAndLinkEntity(context.Background(), tenantID, entity, contextText)
			}()
		}
	}

	return entityIDs, nil
}

// extractEvents создает Event объекты из извлеченных данных
func (e *OntologyExtractor) extractEvents(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedEvents []ExtractedEvent,
	sourceID string,
	entityIDs map[string]string,
	now time.Time,
) ([]string, error) {
	var eventIDs []string

	for _, ext := range extractedEvents {
		if ext.Title == "" || ext.DateISO == "" {
			continue
		}

		// Парсим дату
		occurredAt, err := time.Parse(time.RFC3339, ext.DateISO)
		if err != nil {
			// Пробуем другие форматы
			if t, err2 := time.Parse("2006-01-02", ext.DateISO); err2 == nil {
				occurredAt = t
			} else {
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Failed to parse event date %s: %v", ext.DateISO, err)
				}
				continue
			}
		}

		// Базовая уверенность для извлеченных событий
		baseConfidence := 0.8

		// Корректируем confidence на основе Source Trust
		if e.trustIntegrator != nil && sourceID != "" {
			baseConfidence = e.trustIntegrator.AdjustEventConfidence(ctx, tenantID, nil, sourceID, baseConfidence)
		}

		// Определяем тип источника
		sourceType := "document"
		if document.SourceID != "" {
			// Можно определить тип по DataSource, но пока используем "document"
			sourceType = "document"
		}

		// Создаем Event
		event := &models.Event{
			BaseOntologyFields: models.BaseOntologyFields{
				TenantID:  tenantID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				CreatedBy: "system",
				UpdatedBy: "system",
				Labels:    map[string]string{"en": ext.Title},
				Status:    "active",
			},
			Type:       "extracted", // Тип события из документа
			Title:      ext.Title,
			OccurredAt: &occurredAt,
			Confidence: baseConfidence,
			Provenance: models.Provenance{
				SourceIDs: []string{sourceID},
				SourceRefs: []models.SourceRef{
					{
						SourceID:   document.ID,
						SourceType: sourceType,
					},
				},
				ObservedAt: &now,
				Method:     "LLM_extraction",
			},
		}

		// Добавляем участников из упомянутых entities
		if len(entityIDs) > 0 {
			participants := make([]models.Participant, 0, len(entityIDs))
			for _, entityID := range entityIDs {
				participants = append(participants, models.Participant{
					EntityID: entityID,
					Role:     "mentioned",
				})
			}
			event.Participants = participants
		}

		eventID, err := e.eventsRepo.Create(ctx, event)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Failed to create event %s: %v", ext.Title, err)
			}
			continue
		}

		eventIDs = append(eventIDs, eventID)

		// Обновляем event.ID для использования в детекторах
		event.ID = eventID

		// Обнаружение причинно-следственных связей
		if e.causalDetector != nil {
			go func() {
				_ = e.causalDetector.DetectCausalLinksForEvent(context.Background(), tenantID, event)
			}()
		}

		// Автоматическая классификация домена
		if e.domainService != nil {
			contextText := document.FullText
			if len(contextText) > 1000 {
				contextText = contextText[:1000]
			}
			go func() {
				_ = e.domainService.ClassifyAndLinkEvent(context.Background(), tenantID, event, contextText)
			}()
		}
	}

	return eventIDs, nil
}

// extractClaims создает Claim объекты из извлеченных данных
func (e *OntologyExtractor) extractClaims(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedClaims []ExtractedClaim,
	sourceID string,
	entityIDs map[string]string,
	now time.Time,
) error {
	for _, ext := range extractedClaims {
		if ext.Text == "" {
			continue
		}

		// Для Claim нужен SubjectRef - пытаемся найти подходящий Entity
		// Если не находим, создаем Claim без SubjectRef (будет обработан позже)
		var subjectRef models.SubjectRef
		if len(entityIDs) > 0 {
			// Берем первый упомянутый entity как субъект
			for _, entityID := range entityIDs {
				subjectRef = models.SubjectRef{
					ID:   entityID,
					Kind: models.ReferenceKindEntity,
				}
				break
			}
		} else {
			// Если нет entities, создаем Claim с Literal субъектом
			subjectRef = models.SubjectRef{
				ID:   document.ID, // Используем document ID как временный идентификатор
				Kind: models.ReferenceKindLiteral,
			}
		}

		// Корректируем confidence на основе Source Trust
		adjustedConfidence := ext.Confidence
		if e.trustIntegrator != nil && sourceID != "" {
			adjustedConfidence = e.trustIntegrator.AdjustClaimConfidence(ctx, tenantID, nil, sourceID, ext.Confidence)
		}

		// Определяем тип источника
		sourceType := "document"
		if document.SourceID != "" {
			// Можно определить тип по DataSource, но пока используем "document"
			sourceType = "document"
		}

		// Создаем Claim
		claim := &models.Claim{
			BaseOntologyFields: models.BaseOntologyFields{
				TenantID:  tenantID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				CreatedBy: "system",
				UpdatedBy: "system",
				Labels:    map[string]string{"en": ext.Text},
				Status:    "active",
			},
			SubjectRef: subjectRef,
			Predicate:  "states", // Базовый предикат для извлеченных утверждений
			ObjectRef: models.ObjectRef{
				Kind:  models.ReferenceKindLiteral,
				Value: ext.Text,
			},
			Confidence: adjustedConfidence,
			Stance:    models.StanceNeutral,
			Provenance: models.ClaimProvenance{
				SourceIDs: []string{sourceID},
				SourceRefs: []models.SourceRef{
					{
						SourceID:   document.ID,
						SourceType: sourceType,
					},
				},
				Method:          models.ExtractionMethodNLP,
				ExtractedAt:     now,
				EvidenceAnchors: ext.EvidenceURLs,
			},
		}

		claimID, err := e.claimsRepo.Create(ctx, claim)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Failed to create claim: %v", err)
			}
			continue
		}

		// Обновляем claim.ID для использования в детекторе конфликтов
		claim.ID = claimID

		// Обнаружение конфликтов
		if e.conflictDetector != nil {
			if err := e.conflictDetector.DetectConflictsForClaim(ctx, tenantID, claim); err != nil {
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Failed to detect conflicts: %v", err)
				}
			}
		}

		// Автоматическая классификация домена
		if e.domainService != nil {
			contextText := document.FullText
			if len(contextText) > 1000 {
				contextText = contextText[:1000]
			}
			go func() {
				_ = e.domainService.ClassifyAndLinkClaim(context.Background(), tenantID, claim, contextText)
			}()
		}
	}

	return nil
}

// extractEntitiesFromChat создает Entity из чата
func (e *OntologyExtractor) extractEntitiesFromChat(
	ctx context.Context,
	tenantID string,
	interaction *repo.Interaction,
	extractedEntities []ExtractedEntity,
	sourceID string,
	now time.Time,
) (map[string]string, error) {
	// Аналогично extractEntities, но с контекстом чата
	return e.extractEntities(ctx, tenantID, &models.Document{
		ID:       interaction.ID,
		TenantID: tenantID,
		FullText: interaction.Content,
	}, extractedEntities, sourceID, now)
}

// extractEventsFromChat создает Event из чата
func (e *OntologyExtractor) extractEventsFromChat(
	ctx context.Context,
	tenantID string,
	interaction *repo.Interaction,
	extractedEvents []ExtractedEvent,
	sourceID string,
	entityIDs map[string]string,
	now time.Time,
) ([]string, error) {
	return e.extractEvents(ctx, tenantID, &models.Document{
		ID:       interaction.ID,
		TenantID: tenantID,
		FullText: interaction.Content,
	}, extractedEvents, sourceID, entityIDs, now)
}

// extractClaimsFromChat создает Claim из чата
func (e *OntologyExtractor) extractClaimsFromChat(
	ctx context.Context,
	tenantID string,
	interaction *repo.Interaction,
	extractedClaims []ExtractedClaim,
	sourceID string,
	entityIDs map[string]string,
	now time.Time,
) error {
	return e.extractClaims(ctx, tenantID, &models.Document{
		ID:       interaction.ID,
		TenantID: tenantID,
		FullText: interaction.Content,
	}, extractedClaims, sourceID, entityIDs, now)
}

// ensureSource создает или находит Source для документа
func (e *OntologyExtractor) ensureSource(
	ctx context.Context,
	tenantID string,
	document *models.Document,
) (string, error) {
	if e.sourcesRepo == nil {
		return "", nil
	}

	// Ищем существующий Source по URL или SourceID
	if document.URL != "" {
		allSources, err := e.sourcesRepo.ListByTenant(ctx, tenantID, 1000)
		if err == nil {
			for _, src := range allSources {
				if src.URL != nil && *src.URL == document.URL {
					return src.ID, nil
				}
			}
		}
	}

	// Создаем новый Source
	source := &models.Source{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": document.Title},
			Status:    "active",
		},
		Name:             document.Title,
		Type:             models.SourceTypeNews, // По умолчанию
		URL:              &document.URL,
		ReliabilityPrior: 0.7, // Базовая надежность
	}

	if document.DocumentType == "post" {
		source.Type = models.SourceTypeChatMessage
	}

	sourceID, err := e.sourcesRepo.Create(ctx, source)
	if err != nil {
		return "", err
	}

	return sourceID, nil
}

// ensureChatSource создает Source для чата
func (e *OntologyExtractor) ensureChatSource(
	ctx context.Context,
	tenantID, projectID string,
) (string, error) {
	if e.sourcesRepo == nil {
		return "", nil
	}

	// Ищем существующий Source для чата проекта
	sourceName := fmt.Sprintf("Chat Project %s", projectID)
	allSources, err := e.sourcesRepo.ListByTenant(ctx, tenantID, 1000)
	if err == nil {
		for _, src := range allSources {
			if src.Name == sourceName && src.Type == models.SourceTypeChatMessage {
				return src.ID, nil
			}
		}
	}

	// Создаем новый Source для чата
	source := &models.Source{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": sourceName},
			Status:    "active",
		},
		Name:             sourceName,
		Type:             models.SourceTypeChatMessage,
		ReliabilityPrior: 0.8, // Чат имеет более высокую надежность
	}

	sourceID, err := e.sourcesRepo.Create(ctx, source)
	if err != nil {
		return "", err
	}

	return sourceID, nil
}

// mapExtractedTypeToEntityType маппит извлеченный тип в EntityType
func (e *OntologyExtractor) mapExtractedTypeToEntityType(extractedType string) models.EntityType {
	extractedType = strings.ToLower(strings.TrimSpace(extractedType))
	
	typeMap := map[string]models.EntityType{
		"person":         models.EntityTypePerson,
		"organization":  models.EntityTypeOrganization,
		"location":       models.EntityTypeLocation,
		"product":        models.EntityTypeProduct,
		"event":          models.EntityTypeEventEntity,
		"document":       models.EntityTypeDocument,
		"facility":       models.EntityTypeFacility,
		"supplychain":    models.EntityTypeSupplyChainNode,
		"government":     models.EntityTypeGovernmentProject,
		"index":          models.EntityTypeIndexMetric,
	}

	if entityType, ok := typeMap[extractedType]; ok {
		return entityType
	}

	return models.EntityTypeOther
}

