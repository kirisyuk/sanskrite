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
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// OntologyExtractor преобразует извлеченные данные в онтологические объекты
// Это основной компонент для создания Entity, Event, Claim из различных источников
type OntologyExtractor struct {
	entitiesRepo       *repo.EntityRepository
	eventsRepo         *repo.EventRepository
	claimsRepo         *repo.ClaimRepository
	relationsRepo      *repo.RelationRepository
	sourcesRepo        *repo.SourceRepository
	factsRepo          *repo.OntologyFactsRepository
	entityResolver     *EntityResolver
	entityEnricher     *EntityEnricher
	trustIntegrator    *TrustIntegrator
	conflictDetector   *ConflictDetector
	causalDetector     *CausalDetector
	domainService      *domain.DomainService
	canonizationPolicy CanonicalPathNormalizer
	logger             *log.Logger
}

// NewOntologyExtractor создает новый экстрактор онтологии
func NewOntologyExtractor(
	entitiesRepo *repo.EntityRepository,
	eventsRepo *repo.EventRepository,
	claimsRepo *repo.ClaimRepository,
	relationsRepo *repo.RelationRepository,
	sourcesRepo *repo.SourceRepository,
	factsRepo *repo.OntologyFactsRepository,
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
		factsRepo:        factsRepo,
		entityResolver:   entityResolver,
		entityEnricher:   entityEnricher,
		trustIntegrator:  trustIntegrator,
		conflictDetector: conflictDetector,
		causalDetector:   causalDetector,
		domainService:    domainService,
		logger:           logger,
	}
}

// CanonicalPathNormalizer normalizes canonical paths (unified with Horizon CanonizationPolicy).
type CanonicalPathNormalizer interface {
	NormalizePath(ctx context.Context, path string) (string, error)
}

// WithCanonizationPolicy wires a path normalizer (e.g. CanonizationPolicy adapter) for unified normalization.
func (e *OntologyExtractor) WithCanonizationPolicy(p CanonicalPathNormalizer) *OntologyExtractor {
	e.canonizationPolicy = p
	return e
}

// WithFactsRepo updates the facts repository (e.g. after wiring coordinate enricher).
func (e *OntologyExtractor) WithFactsRepo(r *repo.OntologyFactsRepository) *OntologyExtractor {
	e.factsRepo = r
	return e
}

// normalizeCanonicalPath applies CanonizationPolicy if wired; otherwise returns path unchanged.
func (e *OntologyExtractor) normalizeCanonicalPath(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || e.canonizationPolicy == nil {
		return path
	}
	normalized, err := e.canonizationPolicy.NormalizePath(ctx, path)
	if err != nil || strings.TrimSpace(normalized) == "" {
		return path
	}
	return strings.TrimSpace(normalized)
}

// ExtractFromDocumentContext извлекает онтологические объекты из DocumentContext
// (NormalizedDocument без персистентного Document). Используется для facts-only пути.
func (e *OntologyExtractor) ExtractFromDocumentContext(
	ctx context.Context,
	tenantID string,
	dc *DocumentContext,
	extractedData *DocumentExtraction,
) error {
	doc := &models.Document{
		ID:                dc.ID,
		SourceID:          dc.SourceID,
		URL:               dc.URL,
		Title:             dc.Title,
		DocumentType:      dc.DocumentType,
		FullText:          dc.FullText,
		OriginPublishedAt: dc.OriginPublishedAt,
	}
	return e.ExtractFromDocument(ctx, tenantID, doc, extractedData)
}

// ExtractFromBatchExtraction creates ontology facts (events) from a batch extraction result.
// Used by canvas pipeline: merged extraction from multiple documents with SourceIDs per event.
// Uses a synthetic document for entity/event creation; ext.SourceIDs is used for fact provenance.
// Returns created OntologyFact IDs for downstream SignalPipeline.
func (e *OntologyExtractor) ExtractFromBatchExtraction(
	ctx context.Context,
	tenantID string,
	extractedData *DocumentExtraction,
	batchID string,
) ([]string, error) {
	if extractedData == nil || len(extractedData.Events) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	syntheticDoc := &models.Document{
		ID:           batchID,
		SourceID:     "",
		URL:          "",
		Title:        "canvas_batch",
		DocumentType: "article",
		FullText:     "",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	sourceID := ""
	if len(extractedData.Events) > 0 && len(extractedData.Events[0].SourceIDs) > 0 {
		sourceID = extractedData.Events[0].SourceIDs[0]
	}
	entityIDs, err := e.extractEntities(ctx, tenantID, syntheticDoc, extractedData.Entities, sourceID, now)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Batch extractEntities failed: %v", err)
		}
	}
	var factIDs []string
	_, err = e.extractEvents(ctx, tenantID, syntheticDoc, extractedData.Events, sourceID, entityIDs, now, &factIDs)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[OntologyExtractor] Batch extractEvents failed: %v", err)
		}
		return factIDs, err
	}
	if e.logger != nil {
		e.logger.Printf("[OntologyExtractor] Batch extracted %d entities, %d events, %d facts", len(entityIDs), len(extractedData.Events), len(factIDs))
	}
	return factIDs, nil
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
	eventIDs, err := e.extractEvents(ctx, tenantID, document, extractedData.Events, sourceID, entityIDs, now, nil)
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

	// Emit metric facts (fast-path friendly) if present
	if len(extractedData.Metrics) > 0 {
		if err := e.emitMetricFacts(ctx, tenantID, document, extractedData.Metrics, sourceID, entityIDs, now); err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Failed to emit metric facts: %v", err)
			}
		}
	}

	if e.logger != nil {
		e.logger.Printf("[OntologyExtractor] Extracted from document %s: %d entities, %d events, %d claims, %d metrics",
			document.ID, len(entityIDs), len(eventIDs), len(extractedData.Claims), len(extractedData.Metrics))
	}

	return nil
}

// entityRef holds a resolved entity ID and its normalized taxonomy path.
type entityRef struct {
	ID            string
	CanonicalPath string
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
	Entities []ExtractedEntity       `json:"entities"`
	Events   []ExtractedEvent        `json:"events"`
	Claims   []ExtractedClaim        `json:"claims"`
	Metrics  []ExtractedMetricUpdate `json:"metrics,omitempty"`
}

// ChatExtraction структура извлеченных данных из чата
type ChatExtraction struct {
	Entities []ExtractedEntity       `json:"entities"`
	Events   []ExtractedEvent        `json:"events"`
	Claims   []ExtractedClaim        `json:"claims"`
	Metrics  []ExtractedMetricUpdate `json:"metrics,omitempty"`
}

// ExtractedEntity извлеченная сущность
type ExtractedEntity struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// ExtractedEventParticipant участник события с ролью
type ExtractedEventParticipant struct {
	EntityName string `json:"entityName"` // имя сущности (будет сопоставлено с Entity)
	Role       string `json:"role"`       // agent, target, mediator, mentioned, etc.
}

// ExtractedLocation is a minimal location payload extracted by LLM.
// Place (name) and Address are returned by LLM; Coordinates are resolved via Google Geocoding.
type ExtractedLocation struct {
	Name          *string `json:"name,omitempty"`   // place name (e.g. "Dubai airport", "Port of Houston")
	Address       *string `json:"address,omitempty"` // full address for geocoding (e.g. "123 Main St, Houston, TX, USA")
	ISO6709       *string `json:"iso6709,omitempty"`
	CountryAlpha2 *string `json:"countryAlpha2,omitempty"` // ISO3166-1 alpha2 lowercase
	Admin1        *string `json:"admin1,omitempty"`        // optional state/province code or slug lowercase
	City          *string `json:"city,omitempty"`          // optional city slug lowercase
}

// ExtractedEvent извлеченное событие
type ExtractedEvent struct {
	Title        string                      `json:"title"`
	DateISO      string                      `json:"dateISO"`
	Context      *string                     `json:"context,omitempty"`
	Summary      *string                     `json:"summary,omitempty"`
	Location     *ExtractedLocation          `json:"location,omitempty"`
	Participants []ExtractedEventParticipant `json:"participants,omitempty"` // участники с ролями
	SpherePaths  []string                    `json:"spherePaths,omitempty"`
	EventType    string                      `json:"eventType,omitempty"`
	// SourceIDs from batch extraction: DataSource IDs that contributed to this event.
	// When set, used for OntologyFact.SourceIDs instead of document.SourceID.
	SourceIDs []string `json:"sourceIds,omitempty"`
}

// ExtractedClaim извлеченное утверждение
type ExtractedClaim struct {
	Text              string   `json:"text"`
	Summary           *string  `json:"summary,omitempty"`
	Confidence        float64  `json:"confidence"`
	EvidenceURLs      []string `json:"evidenceUrls,omitempty"`
	SubjectEntityName *string  `json:"subjectEntityName,omitempty"`
	SpherePaths       []string `json:"spherePaths,omitempty"`
}

// ExtractedMetricUpdate is a typed numeric metric extracted by LLM (open-world with preferred vocabulary).
// observedAt is kept as string for the extraction stage; parsing is handled in the extractor.
type ExtractedMetricUpdate struct {
	MetricName        string  `json:"metricName"`
	Value             float64 `json:"value"`
	Unit              *string `json:"unit,omitempty"`
	Confidence        float64 `json:"confidence"`
	ObservedAt        *string `json:"observedAt,omitempty"`
	SubjectEntityName *string `json:"subjectEntityName,omitempty"`
}

// extractEntities создает Entity объекты из извлеченных данных
func (e *OntologyExtractor) extractEntities(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedEntities []ExtractedEntity,
	sourceID string,
	now time.Time,
) (map[string]entityRef, error) {
	entityIDs := make(map[string]entityRef) // canonicalName -> entityRef

	// Entity types that don't produce valid canonical paths - skip (taxonomy alignment)
	skipTypes := map[models.EntityType]bool{
		models.EntityTypeLocation: true, models.EntityTypeDocument: true, models.EntityTypeOther: true,
	}

	// First pass: resolve/create organizations early, so facilities can bind to a stable org path (for geo).
	orgNameToOrgPath := make(map[string]string) // lower-canonicalName -> org canonical path (e.g. /org/{geo}/{slug})
	for _, ext := range extractedEntities {
		if strings.TrimSpace(ext.Value) == "" {
			continue
		}
		canonicalName := strings.TrimSpace(ext.Value)
		entityType := e.mapExtractedTypeToEntityType(ext.Type)
		if skipTypes[entityType] {
			continue
		}
		if entityType != models.EntityTypeOrganization {
			continue
		}

		existing, err := e.entityResolver.ResolveEntity(ctx, tenantID, canonicalName, entityType)
		if err == nil && existing != nil {
			cp := ""
			if len(existing.CanonicalPaths) > 0 {
				cp = existing.CanonicalPaths[0]
			}
			if cp == "" {
				cp = taxonomy.EntityCanonicalPath(string(existing.Type), existing.CanonicalName)
				if strings.TrimSpace(cp) != "" && strings.TrimSpace(cp) != "/" {
					existing.CanonicalPaths = []string{cp}
					existing.VectorText = existing.Description
					_ = e.entitiesRepo.Upsert(ctx, existing)
				}
			}
			entityIDs[canonicalName] = entityRef{ID: existing.ID, CanonicalPath: cp}
		} else {
			cPath := taxonomy.EntityCanonicalPath(string(entityType), canonicalName)
			if cPath == "" || cPath == "/" {
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Skipping entity %s (type %s): invalid canonical path", canonicalName, entityType)
				}
				continue
			}
			cPath = e.normalizeCanonicalPath(ctx, cPath)

			// Validate canonical path format
			if err := taxonomy.ValidateCanonicalPath(cPath); err != nil {
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Invalid canonical path '%s' for entity %s: %v", cPath, canonicalName, err)
				}
				continue
			}

			// Check for duplicate canonical path
			duplicate, err := e.entitiesRepo.FindByCanonicalPath(ctx, tenantID, cPath)
			if err == nil && duplicate != nil {
				// Found duplicate - use existing entity instead of creating new one
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Canonical path collision: '%s' already exists for entity %s (ID: %s), reusing existing entity for '%s'",
						cPath, duplicate.CanonicalName, duplicate.ID, canonicalName)
				}
				// Add canonical name as alias to existing entity if not present
				hasAlias := false
				for _, alias := range duplicate.Aliases {
					if strings.EqualFold(alias, canonicalName) {
						hasAlias = true
						break
					}
				}
				if !hasAlias {
					duplicate.Aliases = append(duplicate.Aliases, canonicalName)
					_ = e.entitiesRepo.Upsert(ctx, duplicate)
				}
				entityIDs[canonicalName] = entityRef{ID: duplicate.ID, CanonicalPath: cPath}
				continue
			}

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
					CanonicalPaths: []string{cPath},
				},
				CanonicalName: canonicalName,
				Type:          entityType,
				Aliases:       []string{canonicalName},
				Attributes: map[string]interface{}{
					"extractedFrom": "document",
					"sourceId":      document.ID,
					"sourceType":    "document",
					"dataSourceId":  sourceID,
				},
			}
			entityID, err := e.entitiesRepo.Create(ctx, entity)
			if err != nil {
				if e.logger != nil {
					e.logger.Printf("[OntologyExtractor] Failed to create entity %s: %v", canonicalName, err)
				}
				continue
			}
			entityIDs[canonicalName] = entityRef{ID: entityID, CanonicalPath: cPath}
			entity.ID = entityID
		}

		// Store org path for facility binding (derive geo from /org/{geo}/{slug})
		if ref, ok := entityIDs[canonicalName]; ok {
			cp := strings.TrimSpace(ref.CanonicalPath)
			if strings.HasPrefix(cp, "/org/") && cp != "" {
				orgNameToOrgPath[strings.ToLower(canonicalName)] = cp
			}
		}
	}

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

		// Skip types that don't fit taxonomy (no valid canonical path)
		if skipTypes[entityType] {
			continue
		}

		// Organizations were handled in the first pass.
		if entityType == models.EntityTypeOrganization {
			continue
		}

		// Используем EntityResolver для поиска существующего Entity
		existing, err := e.entityResolver.ResolveEntity(ctx, tenantID, canonicalName, entityType)
		if err == nil && existing != nil {
			// Ensure canonical path alignment
			cp := ""
			if len(existing.CanonicalPaths) > 0 {
				cp = existing.CanonicalPaths[0]
			}
			if cp == "" {
				cp = taxonomy.EntityCanonicalPath(string(existing.Type), existing.CanonicalName)
				if strings.TrimSpace(cp) != "" && strings.TrimSpace(cp) != "/" {
					existing.CanonicalPaths = []string{cp}
					existing.VectorText = existing.Description // keep old text if any; will be refined later
					_ = e.entitiesRepo.Upsert(ctx, existing)   // best-effort
				}
			}
			entityIDs[canonicalName] = entityRef{ID: existing.ID, CanonicalPath: cp}
			continue
		}

		// Создаем новый Entity
		cPath := ""
		if entityType == models.EntityTypeFacility {
			// Prefer /fac/{geo}/{facilitySlug} when facility name starts with a resolved org name (derive geo from org path).
			lv := strings.ToLower(canonicalName)
			bestOrgName := ""
			bestGeo := ""
			for orgNameLower, orgPath := range orgNameToOrgPath {
				if orgPath == "" {
					continue
				}
				if strings.HasPrefix(lv, orgNameLower) {
					// Ensure boundary (space/punct) so "meta" doesn't match "metallurgy".
					if len(lv) == len(orgNameLower) {
						continue
					}
					next := lv[len(orgNameLower)]
					if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
						continue
					}
					if len(orgNameLower) > len(bestOrgName) {
						bestOrgName = orgNameLower
						bestGeo = extractGeoFromOrgPath(orgPath)
					}
				}
			}
			if bestGeo != "" {
				rest := strings.TrimSpace(canonicalName[len(bestOrgName):])
				cPath = taxonomy.FacilityCanonicalPath(bestGeo, rest)
			}
			if cPath == "" {
				cPath = taxonomy.FacilityCanonicalPath("na", canonicalName)
			}
		} else {
			cPath = taxonomy.EntityCanonicalPath(string(entityType), canonicalName)
		}
		cPath = e.normalizeCanonicalPath(ctx, cPath)

		if cPath == "" || cPath == "/" {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Skipping entity %s (type %s): invalid canonical path", canonicalName, entityType)
			}
			continue
		}

		// Validate canonical path format
		if err := taxonomy.ValidateCanonicalPath(cPath); err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Invalid canonical path '%s' for entity %s: %v", cPath, canonicalName, err)
			}
			continue
		}

		// Check for duplicate canonical path
		duplicate, err := e.entitiesRepo.FindByCanonicalPath(ctx, tenantID, cPath)
		if err == nil && duplicate != nil {
			// Found duplicate - use existing entity instead of creating new one
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Canonical path collision: '%s' already exists for entity %s (ID: %s), reusing existing entity for '%s'",
					cPath, duplicate.CanonicalName, duplicate.ID, canonicalName)
			}
			// Add canonical name as alias to existing entity if not present
			hasAlias := false
			for _, alias := range duplicate.Aliases {
				if strings.EqualFold(alias, canonicalName) {
					hasAlias = true
					break
				}
			}
			if !hasAlias {
				duplicate.Aliases = append(duplicate.Aliases, canonicalName)
				_ = e.entitiesRepo.Upsert(ctx, duplicate)
			}
			entityIDs[canonicalName] = entityRef{ID: duplicate.ID, CanonicalPath: cPath}
			continue
		}

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
				CanonicalPaths: []string{cPath},
			},
			CanonicalName: canonicalName,
			Type:          entityType,
			Aliases:       []string{canonicalName}, // Начальный алиас
			Attributes: map[string]interface{}{
				"extractedFrom": "document",
				"sourceId":      document.ID, // ID документа как источника
				"sourceType":    "document",  // тип источника
				"dataSourceId":  sourceID,    // ID DataSource (если есть)
			},
		}

		entityID, err := e.entitiesRepo.Create(ctx, entity)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[OntologyExtractor] Failed to create entity %s: %v", canonicalName, err)
			}
			continue
		}

		entityIDs[canonicalName] = entityRef{ID: entityID, CanonicalPath: cPath}

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

// extractEvents создает Event объекты из извлеченных данных.
// If factIDsOut is non-nil, appends created OntologyFact IDs to it (for batch pipeline).
func (e *OntologyExtractor) extractEvents(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	extractedEvents []ExtractedEvent,
	sourceID string,
	entityIDs map[string]entityRef,
	now time.Time,
	factIDsOut *[]string,
) ([]string, error) {
	var eventIDs []string

	for _, ext := range extractedEvents {
		// Prefer core DataSource ID for provenance (this is what Scout/Watchdog can consume).
		// For batch extraction, ext.SourceIDs takes precedence.
		dataSourceID := strings.TrimSpace(document.SourceID)
		if dataSourceID == "" {
			dataSourceID = strings.TrimSpace(sourceID)
		}
		eventSourceIDs := []string{dataSourceID}
		if len(ext.SourceIDs) > 0 {
			eventSourceIDs = ext.SourceIDs
		}

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

		// Create Event
		eventType := taxonomy.NormalizeEventType(ext.EventType)
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
			Type:       eventType, // normalized event type from taxonomy (allowlist + other)
			Title:      ext.Title,
			OccurredAt: &occurredAt,
			OutcomeSummary: func() string {
				if ext.Summary != nil && strings.TrimSpace(*ext.Summary) != "" {
					return strings.TrimSpace(*ext.Summary)
				}
				if ext.Context != nil && strings.TrimSpace(*ext.Context) != "" {
					return strings.TrimSpace(*ext.Context)
				}
				return ""
			}(),
			Confidence: baseConfidence,
			Provenance: models.Provenance{
				SourceIDs: eventSourceIDs,
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

		// Location (best effort): place name, address (for geocoding), structured geo
		if ext.Location != nil {
			locName := ""
			if ext.Location.Name != nil {
				locName = strings.TrimSpace(*ext.Location.Name)
			}
			if locName != "" || ext.Location.Address != nil || ext.Location.ISO6709 != nil || ext.Location.CountryAlpha2 != nil || ext.Location.Admin1 != nil || ext.Location.City != nil {
				event.Location = &models.Location{
					Name:          locName,
					Address:       ext.Location.Address,
					ISO6709:       ext.Location.ISO6709,
					CountryAlpha2: ext.Location.CountryAlpha2,
					Admin1:        ext.Location.Admin1,
					City:          ext.Location.City,
				}
			}
		}

		// Добавляем участников из извлеченных данных или упомянутых entities
		if len(ext.Participants) > 0 {
			// Используем участников из извлеченных данных с ролями
			participants := make([]models.Participant, 0, len(ext.Participants))
			for _, p := range ext.Participants {
				// Ищем Entity по имени
				ref, found := entityIDs[p.EntityName]
				if !found {
					// Пробуем найти по алиасам или нормализованному имени
					canonicalName := strings.TrimSpace(p.EntityName)
					ref, found = entityIDs[canonicalName]
					if !found {
						// Пропускаем, если entity не найдена
						continue
					}
				}

				// Определяем роль (по умолчанию "mentioned", если не указана)
				role := strings.TrimSpace(p.Role)
				if role == "" {
					role = "mentioned"
				}

				participants = append(participants, models.Participant{
					EntityID: ref.ID,
					Role:     role,
				})
			}
			event.Participants = participants
		} else if len(entityIDs) > 0 {
			// Fallback: если участники не извлечены, используем все упомянутые entities с ролью "mentioned"
			participants := make([]models.Participant, 0, len(entityIDs))
			for _, ref := range entityIDs {
				participants = append(participants, models.Participant{
					EntityID: ref.ID,
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

		// Emit unified fact frame (ontology_facts) for watchdog/search
		if e.factsRepo != nil {
			observedAt := &now
			if document.OriginPublishedAt != nil && !document.OriginPublishedAt.IsZero() {
				observedAt = document.OriginPublishedAt
			}
			sourceIDs := []string{dataSourceID}
			if len(ext.SourceIDs) > 0 {
				sourceIDs = ext.SourceIDs
			}
			f := &models.OntologyFact{
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
				Kind:       "event",
				Type:       event.Type,
				Title:      event.Title,
				Summary:    event.OutcomeSummary,
				OccurredAt: event.OccurredAt,
				ObservedAt: observedAt,
				EventID:    &eventID,
				SourceIDs:  sourceIDs,
				SourceRefs: []models.SourceRef{{SourceID: document.ID, SourceType: "document"}},
				Confidence: event.Confidence,
			}

			// spherePaths (impact domains) from extraction
			if len(ext.SpherePaths) > 0 {
				f.SpherePaths = taxonomy.NormalizeSpherePaths(ext.SpherePaths)
			}

			// actorPaths from participants
			for _, p := range event.Participants {
				for _, ref := range entityIDs {
					if ref.ID == p.EntityID && ref.CanonicalPath != "" && ref.CanonicalPath != "/" {
						f.ActorPaths = append(f.ActorPaths, ref.CanonicalPath)
						break
					}
				}
			}

			// locationPath from event.Location if present
			if event.Location != nil {
				country := ""
				admin1 := ""
				city := ""
				if event.Location.CountryAlpha2 != nil {
					country = strings.TrimSpace(*event.Location.CountryAlpha2)
				}
				if event.Location.Admin1 != nil {
					admin1 = strings.TrimSpace(*event.Location.Admin1)
				}
				if event.Location.City != nil {
					city = strings.TrimSpace(*event.Location.City)
				}
				// Canonical Horizon geo taxonomy: /loc/{country}[/{level1}[/{level2}]].
				if strings.TrimSpace(country) != "" {
					locPath := taxonomy.GeoCanonicalPath(country, admin1, city)
					if locPath != "" && locPath != "/" {
						f.LocationPath = &locPath
					}
				}
				// EventPlace, EventAddress, EventISO6709 for geocoding (Google Geo resolves Address/Place to Coordinates)
				if event.Location.Name != "" {
					f.EventPlace = &event.Location.Name
				}
				if event.Location.Address != nil && strings.TrimSpace(*event.Location.Address) != "" {
					f.EventAddress = event.Location.Address
				}
				if event.Location.ISO6709 != nil && strings.TrimSpace(*event.Location.ISO6709) != "" {
					f.EventISO6709 = event.Location.ISO6709
				}
			}

			// canonicalPaths unify the frame: include actors + location
			f.CanonicalPaths = append([]string{}, f.ActorPaths...)
			if f.LocationPath != nil {
				f.CanonicalPaths = append(f.CanonicalPaths, *f.LocationPath)
			}
			f.VectorText = f.GenerateVectorText()

			factID, createErr := e.factsRepo.Create(ctx, f)
			if createErr == nil && factID != "" && factIDsOut != nil {
				*factIDsOut = append(*factIDsOut, factID)
			}
		}

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
	entityIDs map[string]entityRef,
	now time.Time,
) error {
	for _, ext := range extractedClaims {
		dataSourceID := strings.TrimSpace(document.SourceID)
		if dataSourceID == "" {
			dataSourceID = strings.TrimSpace(sourceID)
		}

		if ext.Text == "" {
			continue
		}

		// Для Claim нужен SubjectRef - пытаемся найти подходящий Entity
		// Если не находим, создаем Claim без SubjectRef (будет обработан позже)
		var subjectRef models.SubjectRef
		if ext.SubjectEntityName != nil && strings.TrimSpace(*ext.SubjectEntityName) != "" {
			if ref, ok := entityIDs[strings.TrimSpace(*ext.SubjectEntityName)]; ok && ref.ID != "" {
				subjectRef = models.SubjectRef{ID: ref.ID, Kind: models.ReferenceKindEntity}
			}
		}
		if subjectRef.ID == "" && len(entityIDs) > 0 {
			// Fallback: берем первый упомянутый entity как субъект
			for _, ref := range entityIDs {
				subjectRef = models.SubjectRef{ID: ref.ID, Kind: models.ReferenceKindEntity}
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
			Stance:     models.StanceNeutral,
			Provenance: models.ClaimProvenance{
				SourceIDs: []string{dataSourceID},
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

		// Emit unified fact frame for watchdog/search
		if e.factsRepo != nil {
			f := &models.OntologyFact{
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
				Kind:  "claim",
				Type:  claim.Predicate,
				Title: ext.Text,
				Summary: func() string {
					if ext.Summary != nil {
						return strings.TrimSpace(*ext.Summary)
					}
					return ""
				}(),
				ObservedAt: &now,
				ClaimID:    &claimID,
				SourceIDs:  []string{dataSourceID},
				SourceRefs: []models.SourceRef{{SourceID: document.ID, SourceType: "document"}},
				Confidence: claim.Confidence,
			}

			// spherePaths (impact domains) from extraction
			if len(ext.SpherePaths) > 0 {
				f.SpherePaths = taxonomy.NormalizeSpherePaths(ext.SpherePaths)
			}

			// Best-effort canonical link: prefer extracted subject, else first entity.
			if ext.SubjectEntityName != nil {
				if ref, ok := entityIDs[strings.TrimSpace(*ext.SubjectEntityName)]; ok {
					if ref.CanonicalPath != "" && ref.CanonicalPath != "/" {
						f.ActorPaths = []string{ref.CanonicalPath}
						f.CanonicalPaths = []string{ref.CanonicalPath}
					}
				}
			}
			if len(f.CanonicalPaths) == 0 {
				for _, ref := range entityIDs {
					if ref.CanonicalPath != "" && ref.CanonicalPath != "/" {
						f.ActorPaths = []string{ref.CanonicalPath}
						f.CanonicalPaths = []string{ref.CanonicalPath}
						break
					}
				}
			}
			f.VectorText = f.GenerateVectorText()
			_, _ = e.factsRepo.Create(ctx, f)
		}

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

func (e *OntologyExtractor) emitMetricFacts(
	ctx context.Context,
	tenantID string,
	document *models.Document,
	metrics []ExtractedMetricUpdate,
	sourceID string,
	entityIDs map[string]entityRef,
	now time.Time,
) error {
	if e.factsRepo == nil || len(metrics) == 0 {
		return nil
	}

	// Prefer core DataSource ID for provenance (same as events/claims)
	dataSourceID := strings.TrimSpace(document.SourceID)
	if dataSourceID == "" {
		dataSourceID = strings.TrimSpace(sourceID)
	}

	parseObservedAt := func(s *string) *time.Time {
		if s == nil {
			return nil
		}
		raw := strings.TrimSpace(*s)
		if raw == "" {
			return nil
		}
		// Try RFC3339 first, then YYYY-MM-DD.
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			tt := t.UTC()
			return &tt
		}
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			tt := t.UTC()
			return &tt
		}
		return nil
	}

	for _, mu := range metrics {
		name := strings.TrimSpace(mu.MetricName)
		if name == "" {
			continue
		}

		// Resolve subject entity to canonicalPath (best effort)
		var subj entityRef
		if mu.SubjectEntityName != nil {
			if ref, ok := entityIDs[strings.TrimSpace(*mu.SubjectEntityName)]; ok {
				subj = ref
			}
		}
		if subj.ID == "" {
			// fallback: first entity in doc
			for _, ref := range entityIDs {
				subj = ref
				break
			}
		}
		if subj.CanonicalPath == "" || subj.CanonicalPath == "/" {
			// Cannot bind metric to taxonomy; skip (avoid polluting global metric space)
			continue
		}

		obs := parseObservedAt(mu.ObservedAt)
		if obs == nil {
			obs = &now
		}

		conf := mu.Confidence
		if conf <= 0 {
			conf = 0.8
		}

		fact := &models.OntologyFact{
			BaseOntologyFields: models.BaseOntologyFields{
				TenantID:  tenantID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				CreatedBy: "system",
				UpdatedBy: "system",
				Labels:    map[string]string{"en": fmt.Sprintf("%s: %.6f", name, mu.Value)},
				Status:    "active",
				CanonicalPaths: []string{
					subj.CanonicalPath,
				},
				ActorPaths: []string{
					subj.CanonicalPath,
				},
			},
			Kind:       "metric",
			Type:       name,
			Title:      fmt.Sprintf("%s = %.6f", name, mu.Value),
			Summary:    "",
			Confidence: conf,
			ObservedAt: obs,
			SourceIDs:  []string{dataSourceID},
			SourceRefs: []models.SourceRef{{SourceID: document.ID, SourceType: "document"}},
			MetricUpdates: []models.FactMetricUpdate{
				{
					MetricName: name,
					Value:      mu.Value,
					Unit:       mu.Unit,
					Confidence: conf,
					ObservedAt: obs,
				},
			},
		}
		fact.VectorText = fact.GenerateVectorText()
		_, _ = e.factsRepo.Create(ctx, fact)
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
) (map[string]entityRef, error) {
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
	entityIDs map[string]entityRef,
	now time.Time,
) ([]string, error) {
	return e.extractEvents(ctx, tenantID, &models.Document{
		ID:       interaction.ID,
		TenantID: tenantID,
		FullText: interaction.Content,
	}, extractedEvents, sourceID, entityIDs, now, nil)
}

// extractClaimsFromChat создает Claim из чата
func (e *OntologyExtractor) extractClaimsFromChat(
	ctx context.Context,
	tenantID string,
	interaction *repo.Interaction,
	extractedClaims []ExtractedClaim,
	sourceID string,
	entityIDs map[string]entityRef,
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
	// For public sources (empty tenantID), ListByTenant will find global sources (TenantID="")
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
		"person":             models.EntityTypePerson,
		"organization":       models.EntityTypeOrganization,
		"location":           models.EntityTypeLocation,
		"product":            models.EntityTypeProduct,
		"program":            models.EntityTypeProgram,
		"market":             models.EntityTypeMarket,
		"event":              models.EntityTypeEventEntity,
		"document":           models.EntityTypeDocument,
		"facility":           models.EntityTypeFacility,
		"supplychain":        models.EntityTypeSupplyChainNode,
		"supplychainnode":    models.EntityTypeSupplyChainNode,
		"government":         models.EntityTypeGovernmentProject,
		"governmentproject":  models.EntityTypeGovernmentProject,
		"index":              models.EntityTypeIndexMetric,
	}

	if entityType, ok := typeMap[extractedType]; ok {
		return entityType
	}

	return models.EntityTypeOther
}

// extractGeoFromOrgPath returns the geo segment from /org/{geo}/{slug} or "na" for legacy /org/{slug}.
func extractGeoFromOrgPath(orgPath string) string {
	orgPath = strings.TrimSpace(orgPath)
	if orgPath == "" || !strings.HasPrefix(orgPath, "/org/") {
		return "na"
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(orgPath, "/"), "/"), "/")
	if len(parts) >= 3 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return "na"
}
