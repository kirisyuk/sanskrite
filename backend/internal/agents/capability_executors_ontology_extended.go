package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// newSuccessResult и newFailedResult определены в capability_executors_ontology.go

// ============================================================================
// Extended Ontology Capability Executors
// ============================================================================
// Note: min(), newSuccessResult(), and newFailedResult() are defined in capability_executors.go

// EntityEnrichExecutor обогащает сущность данными из Wikidata
type EntityEnrichExecutor struct {
	entitiesRepo   *repo.EntityRepository
	enricher       *ontology.EntityEnricher
	logger         *log.Logger
}

// NewEntityEnrichExecutor создает новый исполнитель для обогащения сущностей
func NewEntityEnrichExecutor(
	entitiesRepo *repo.EntityRepository,
	enricher *ontology.EntityEnricher,
	logger *log.Logger,
) *EntityEnrichExecutor {
	return &EntityEnrichExecutor{
		entitiesRepo: entitiesRepo,
		enricher:     enricher,
		logger:       logger,
	}
}

func (e *EntityEnrichExecutor) Capability() Capability {
	return CapabilityEntityEnrich
}

func (e *EntityEnrichExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["entityId"].(string); !ok {
		return fmt.Errorf("entityId is required")
	}
	return nil
}

func (e *EntityEnrichExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityID := stepCtx.Input["entityId"].(string)

	entity, err := e.entitiesRepo.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	if entity.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("entity belongs to different tenant")
	}

	if e.logger != nil {
		e.logger.Printf("[EntityEnrichExecutor] Enriching entity %s (%s)", entityID, entity.CanonicalName)
	}

	// Обогащаем сущность
	if err := e.enricher.EnrichEntity(ctx, stepCtx.TenantID, entity); err != nil {
		return nil, fmt.Errorf("failed to enrich entity: %w", err)
	}

	// Обновляем сущность
	if err := e.entitiesRepo.Upsert(ctx, entity); err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	confidence := 0.85
	if getWikidataID(entity) == "" {
		confidence = 0.6 // Ниже если Wikidata ID не найден
	}
	return newSuccessResult(
		map[string]interface{}{
			"entityId":    entity.ID,
			"canonicalName": entity.CanonicalName,
			"wikidataId":   getWikidataID(entity),
			"aliases":     entity.Aliases,
		},
		fmt.Sprintf("Enriched entity %s with Wikidata data", entity.CanonicalName),
		confidence,
	), nil
}

func getWikidataID(entity *models.Entity) string {
	for _, ref := range entity.KBRefs {
		if ref.KB == "Wikidata" {
			return ref.ExternalID
		}
	}
	return ""
}

// EntityExtractExecutor извлекает сущности из текста
type EntityExtractExecutor struct {
	entitiesRepo *repo.EntityRepository
	resolver    *ontology.EntityResolver
	genaiClient *gen_combined.Client
	logger      *log.Logger
}

// NewEntityExtractExecutor создает новый исполнитель для извлечения сущностей
func NewEntityExtractExecutor(
	entitiesRepo *repo.EntityRepository,
	resolver *ontology.EntityResolver,
	genaiClient *gen_combined.Client,
	logger *log.Logger,
) *EntityExtractExecutor {
	return &EntityExtractExecutor{
		entitiesRepo: entitiesRepo,
		resolver:     resolver,
		genaiClient:  genaiClient,
		logger:       logger,
	}
}

func (e *EntityExtractExecutor) Capability() Capability {
	return CapabilityEntityExtract
}

func (e *EntityExtractExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["text"].(string); !ok {
		return fmt.Errorf("text is required")
	}
	return nil
}

func (e *EntityExtractExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	text := stepCtx.Input["text"].(string)
	entityType := "" // optional
	if et, ok := stepCtx.Input["entityType"].(string); ok {
		entityType = et
	}

	if e.logger != nil {
		e.logger.Printf("[EntityExtractExecutor] Extracting entities from text (length: %d)", len(text))
	}

	// Используем LLM для извлечения сущностей
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"type": map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
					},
					"required": []string{"name", "type"},
				},
			},
		},
		"required": []string{"entities"},
	}

	prompt := fmt.Sprintf(`Extract all entities (people, organizations, locations, products, etc.) from the following text.
Return a JSON object with an "entities" array. Each entity should have:
- name: the entity name
- type: entity type (Person, Organization, Location, Product, etc.)
- description: brief description if available

Text:
%s`, text)

	if entityType != "" {
		prompt += fmt.Sprintf("\n\nFocus on entities of type: %s", entityType)
	}

	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to extract entities: %w", err)
	}

	var extracted struct {
		Entities []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description,omitempty"`
		} `json:"entities"`
	}

	// Пробуем распарсить из Content или RawJSON
	jsonContent := result.Content
	if jsonContent == "" && result.RawJSON != "" {
		jsonContent = result.RawJSON
	}

	// Убираем markdown code fences если есть
	jsonContent = strings.TrimSpace(jsonContent)
	jsonContent = strings.TrimPrefix(jsonContent, "```json")
	jsonContent = strings.TrimPrefix(jsonContent, "```")
	jsonContent = strings.TrimSuffix(jsonContent, "```")
	jsonContent = strings.TrimSpace(jsonContent)

	if jsonContent == "" {
		return nil, fmt.Errorf("empty JSON response from LLM")
	}

	if err := json.Unmarshal([]byte(jsonContent), &extracted); err != nil {
		if e.logger != nil {
			e.logger.Printf("[EntityExtractExecutor] Failed to parse JSON: %v, content: %s", err, jsonContent[:min(200, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to parse extracted entities: %w", err)
	}

	now := time.Now().UTC()
	createdEntities := make([]map[string]interface{}, 0, len(extracted.Entities))

	for _, ext := range extracted.Entities {
		if ext.Name == "" {
			continue
		}

		// Определяем тип сущности
		entityTypeEnum := mapStringToEntityType(ext.Type)

		// Используем resolver для поиска существующей сущности
		existing, err := e.resolver.ResolveEntity(ctx, stepCtx.TenantID, ext.Name, entityTypeEnum)
		if err == nil && existing != nil {
			createdEntities = append(createdEntities, map[string]interface{}{
				"entityId":      existing.ID,
				"canonicalName": existing.CanonicalName,
				"type":          string(existing.Type),
				"status":        "existing",
			})
			continue
		}

		// Создаем новую сущность
		entity := &models.Entity{
			BaseOntologyFields: models.BaseOntologyFields{
				TenantID:  stepCtx.TenantID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				CreatedBy: stepCtx.UserID,
				UpdatedBy: stepCtx.UserID,
				Labels:    map[string]string{"en": ext.Name},
				Status:    "active",
			},
			CanonicalName: ext.Name,
			Type:          entityTypeEnum,
			Aliases:       []string{ext.Name},
		}
		if ext.Description != "" {
			entity.Description = ext.Description
		}

		entityID, err := e.entitiesRepo.Create(ctx, entity)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[EntityExtractExecutor] Failed to create entity %s: %v", ext.Name, err)
			}
			continue
		}

		createdEntities = append(createdEntities, map[string]interface{}{
			"entityId":      entityID,
			"canonicalName": entity.CanonicalName,
			"type":          string(entity.Type),
			"status":        "created",
		})
	}

	confidence := 0.8
	if len(createdEntities) == 0 {
		confidence = 0.5
	}
	return newSuccessResult(
		map[string]interface{}{
			"entities": createdEntities,
			"count":    len(createdEntities),
		},
		fmt.Sprintf("Extracted %d entities from text", len(createdEntities)),
		confidence,
	), nil
}

func mapStringToEntityType(typeStr string) models.EntityType {
	switch strings.ToLower(typeStr) {
	case "person":
		return models.EntityTypePerson
	case "organization", "org":
		return models.EntityTypeOrganization
	case "location", "place":
		return models.EntityTypeLocation
	case "product":
		return models.EntityTypeProduct
	case "document":
		return models.EntityTypeDocument
	case "facility":
		return models.EntityTypeFacility
	default:
		return models.EntityTypeOther
	}
}

// EntityHierarchyBuildExecutor строит иерархию сущностей (поиск подчиненных)
type EntityHierarchyBuildExecutor struct {
	entitiesRepo *repo.EntityRepository
	relationsRepo *repo.RelationRepository
	logger       *log.Logger
}

// NewEntityHierarchyBuildExecutor создает новый исполнитель для построения иерархии
func NewEntityHierarchyBuildExecutor(
	entitiesRepo *repo.EntityRepository,
	relationsRepo *repo.RelationRepository,
	logger *log.Logger,
) *EntityHierarchyBuildExecutor {
	return &EntityHierarchyBuildExecutor{
		entitiesRepo: entitiesRepo,
		relationsRepo: relationsRepo,
		logger:       logger,
	}
}

func (e *EntityHierarchyBuildExecutor) Capability() Capability {
	return CapabilityEntityHierarchyBuild
}

func (e *EntityHierarchyBuildExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["entityId"].(string); !ok {
		return fmt.Errorf("entityId is required")
	}
	return nil
}

func (e *EntityHierarchyBuildExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityID := stepCtx.Input["entityId"].(string)
	maxDepth := 3 // по умолчанию
	if md, ok := stepCtx.Input["maxDepth"].(float64); ok {
		maxDepth = int(md)
	} else if md, ok := stepCtx.Input["maxDepth"].(int); ok {
		maxDepth = md
	}

	if e.logger != nil {
		e.logger.Printf("[EntityHierarchyBuildExecutor] Building hierarchy for entity %s, maxDepth=%d", entityID, maxDepth)
	}

	hierarchy := e.buildHierarchy(ctx, stepCtx.TenantID, entityID, maxDepth, make(map[string]bool))

	nodeCount := countHierarchyNodes(hierarchy)
	confidence := 0.85
	if nodeCount == 0 {
		confidence = 0.6
	}
	return newSuccessResult(
		map[string]interface{}{
			"entityId":  entityID,
			"hierarchy": hierarchy,
			"count":     nodeCount,
		},
		fmt.Sprintf("Built hierarchy with %d nodes", nodeCount),
		confidence,
	), nil
}

func (e *EntityHierarchyBuildExecutor) buildHierarchy(ctx context.Context, tenantID, entityID string, maxDepth int, visited map[string]bool) map[string]interface{} {
	if maxDepth <= 0 || visited[entityID] {
		return nil
	}

	visited[entityID] = true

	entity, err := e.entitiesRepo.Get(ctx, entityID)
	if err != nil {
		return nil
	}

	result := map[string]interface{}{
		"entityId":      entity.ID,
		"canonicalName": entity.CanonicalName,
		"type":          string(entity.Type),
		"children":      []interface{}{},
	}

	// Ищем связи типа "contains", "partOf", "locatedIn" и т.д.
	hierarchyPredicates := []string{"contains", "partOf", "locatedIn", "subsidiaryOf", "parentOf"}
	
	// Ищем связи где entity является subject (содержит другие)
	subjectRelations, _ := e.relationsRepo.ListBySubject(ctx, tenantID, entityID, 100)
	for _, rel := range subjectRelations {
		if contains(hierarchyPredicates, rel.Predicate) && maxDepth > 1 {
			childHierarchy := e.buildHierarchy(ctx, tenantID, rel.ObjectID, maxDepth-1, visited)
			if childHierarchy != nil {
				children := result["children"].([]interface{})
				result["children"] = append(children, childHierarchy)
			}
		}
	}

	// Ищем связи где entity является object (часть другого)
	objectRelations, _ := e.relationsRepo.ListByObject(ctx, tenantID, entityID, 100)
	for _, rel := range objectRelations {
		if contains(hierarchyPredicates, rel.Predicate) {
			result["parentId"] = rel.SubjectID
			break // Обычно один родитель
		}
	}

	return result
}

func countHierarchyNodes(hierarchy map[string]interface{}) int {
	if hierarchy == nil {
		return 0
	}
	count := 1
	if children, ok := hierarchy["children"].([]interface{}); ok {
		for _, child := range children {
			if childMap, ok := child.(map[string]interface{}); ok {
				count += countHierarchyNodes(childMap)
			}
		}
	}
	return count
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ConflictDetectExecutor обнаруживает конфликты между claims
type ConflictDetectExecutor struct {
	conflictDetector *ontology.ConflictDetector
	claimsRepo       *repo.ClaimRepository
	logger           *log.Logger
}

// NewConflictDetectExecutor создает новый исполнитель для обнаружения конфликтов
func NewConflictDetectExecutor(
	conflictDetector *ontology.ConflictDetector,
	claimsRepo *repo.ClaimRepository,
	logger *log.Logger,
) *ConflictDetectExecutor {
	return &ConflictDetectExecutor{
		conflictDetector: conflictDetector,
		claimsRepo:       claimsRepo,
		logger:           logger,
	}
}

func (e *ConflictDetectExecutor) Capability() Capability {
	return CapabilityConflictDetect
}

func (e *ConflictDetectExecutor) ValidateInput(input map[string]interface{}) error {
	// Может быть claimId или entityId
	if _, ok := input["claimId"].(string); !ok {
		if _, ok := input["entityId"].(string); !ok {
			return fmt.Errorf("claimId or entityId is required")
		}
	}
	return nil
}

func (e *ConflictDetectExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	var claim *models.Claim
	var err error

	if claimID, ok := stepCtx.Input["claimId"].(string); ok {
		claim, err = e.claimsRepo.Get(ctx, claimID)
		if err != nil {
			return nil, fmt.Errorf("failed to get claim: %w", err)
		}
	} else if entityID, ok := stepCtx.Input["entityId"].(string); ok {
		// Находим claims для сущности
		claims, err := e.claimsRepo.ListBySubject(ctx, stepCtx.TenantID, entityID, 10)
		if err != nil || len(claims) == 0 {
			return newSuccessResult(
				map[string]interface{}{
					"conflicts": []interface{}{},
					"count":     0,
				},
				"No claims found for entity",
				0.9,
			), nil
		}
		claim = claims[0] // Берем первый claim
	} else {
		return nil, fmt.Errorf("claimId or entityId is required")
	}

	if claim.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("claim belongs to different tenant")
	}

	if e.logger != nil {
		e.logger.Printf("[ConflictDetectExecutor] Detecting conflicts for claim %s", claim.ID)
	}

	// Обнаруживаем конфликты
	if err := e.conflictDetector.DetectConflictsForClaim(ctx, stepCtx.TenantID, claim); err != nil {
		return nil, fmt.Errorf("failed to detect conflicts: %w", err)
	}

	// Получаем uncertainty models для этого claim
	uncertainties, _ := e.conflictDetector.GetUncertaintiesForClaim(ctx, stepCtx.TenantID, claim.ID)

	conflicts := make([]interface{}, 0, len(uncertainties))
	for _, unc := range uncertainties {
		confidence := 0.0
		if unc.Representation.Confidence != nil {
			confidence = *unc.Representation.Confidence
		}
		conflicts = append(conflicts, map[string]interface{}{
			"uncertaintyId": unc.ID,
			"claimId":       claim.ID,
			"confidence":    confidence,
			"description":   unc.Description,
		})
	}

	confidence := 0.85
	if len(conflicts) == 0 {
		confidence = 0.7
	}
	return newSuccessResult(
		map[string]interface{}{
			"claimId":   claim.ID,
			"conflicts": conflicts,
			"count":     len(conflicts),
		},
		fmt.Sprintf("Detected %d conflicts", len(conflicts)),
		confidence,
	), nil
}

// CausalLinkDetectExecutor обнаруживает причинно-следственные связи
type CausalLinkDetectExecutor struct {
	causalDetector *ontology.CausalDetector
	eventsRepo     *repo.EventRepository
	logger         *log.Logger
}

// NewCausalLinkDetectExecutor создает новый исполнитель для обнаружения причинно-следственных связей
func NewCausalLinkDetectExecutor(
	causalDetector *ontology.CausalDetector,
	eventsRepo *repo.EventRepository,
	logger *log.Logger,
) *CausalLinkDetectExecutor {
	return &CausalLinkDetectExecutor{
		causalDetector: causalDetector,
		eventsRepo:     eventsRepo,
		logger:         logger,
	}
}

func (e *CausalLinkDetectExecutor) Capability() Capability {
	return CapabilityCausalLinkDetect
}

func (e *CausalLinkDetectExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["eventId"].(string); !ok {
		return fmt.Errorf("eventId is required")
	}
	return nil
}

func (e *CausalLinkDetectExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	eventID := stepCtx.Input["eventId"].(string)

	event, err := e.eventsRepo.Get(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	if event.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("event belongs to different tenant")
	}

	if e.logger != nil {
		e.logger.Printf("[CausalLinkDetectExecutor] Detecting causal links for event %s", eventID)
	}

	// Обнаруживаем причинно-следственные связи
	if err := e.causalDetector.DetectCausalLinksForEvent(ctx, stepCtx.TenantID, event); err != nil {
		return nil, fmt.Errorf("failed to detect causal links: %w", err)
	}

	// Получаем созданные causal links
	causalLinks, _ := e.causalDetector.GetCausalLinksForEvent(ctx, stepCtx.TenantID, eventID)

	links := make([]interface{}, 0, len(causalLinks))
	for _, link := range causalLinks {
		mechanism := ""
		if link.Evidence.Method != nil {
			mechanism = *link.Evidence.Method
		}
		links = append(links, map[string]interface{}{
			"causalLinkId": link.ID,
			"causeEventId": link.CauseRef.ID,
			"effectEventId": link.EffectRef.ID,
			"strength":     link.Strength,
			"probability":  link.Probability,
			"mechanism":    mechanism,
		})
	}

	confidence := 0.85
	if len(links) == 0 {
		confidence = 0.7
	}
	return newSuccessResult(
		map[string]interface{}{
			"eventId": eventID,
			"links":   links,
			"count":   len(links),
		},
		fmt.Sprintf("Detected %d causal links", len(links)),
		confidence,
	), nil
}

// RelationFindExecutor находит существующие связи
type RelationFindExecutor struct {
	relationsRepo *repo.RelationRepository
	logger        *log.Logger
}

// NewRelationFindExecutor создает новый исполнитель для поиска связей
func NewRelationFindExecutor(
	relationsRepo *repo.RelationRepository,
	logger *log.Logger,
) *RelationFindExecutor {
	return &RelationFindExecutor{
		relationsRepo: relationsRepo,
		logger:        logger,
	}
}

func (e *RelationFindExecutor) Capability() Capability {
	return CapabilityRelationFind
}

func (e *RelationFindExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["entityId"].(string); !ok {
		return fmt.Errorf("entityId is required")
	}
	return nil
}

func (e *RelationFindExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityID := stepCtx.Input["entityId"].(string)
	predicate := "" // optional
	if p, ok := stepCtx.Input["predicate"].(string); ok {
		predicate = p
	}
	direction := "" // optional: "subject", "object", "both"
	if d, ok := stepCtx.Input["direction"].(string); ok {
		direction = d
	}

	if e.logger != nil {
		e.logger.Printf("[RelationFindExecutor] Finding relations for entity %s", entityID)
	}

	var relations []*models.Relation

	if direction == "object" || direction == "" {
		// По умолчанию ищем связи где entity является object
		objectRels, _ := e.relationsRepo.ListByObject(ctx, stepCtx.TenantID, entityID, 100)
		relations = append(relations, objectRels...)
	}

	if direction == "subject" || direction == "both" || direction == "" {
		// Ищем связи где entity является subject
		subjectRels, _ := e.relationsRepo.ListBySubject(ctx, stepCtx.TenantID, entityID, 100)
		relations = append(relations, subjectRels...)
	}

	// Фильтруем по predicate если указан
	if predicate != "" {
		filtered := make([]*models.Relation, 0)
		for _, rel := range relations {
			if rel.Predicate == predicate {
				filtered = append(filtered, rel)
			}
		}
		relations = filtered
	}

	// Удаляем дубликаты
	seen := make(map[string]bool)
	uniqueRelations := make([]*models.Relation, 0)
	for _, rel := range relations {
		if !seen[rel.ID] {
			seen[rel.ID] = true
			uniqueRelations = append(uniqueRelations, rel)
		}
	}

	result := make([]interface{}, 0, len(uniqueRelations))
	for _, rel := range uniqueRelations {
		result = append(result, map[string]interface{}{
			"relationId": rel.ID,
			"subjectId":  rel.SubjectID,
			"predicate":  rel.Predicate,
			"objectId":   rel.ObjectID,
			"direction":  string(rel.Direction),
			"confidence": rel.Confidence,
		})
	}

	confidence := 0.9
	if len(result) == 0 {
		confidence = 0.7
	}
	return newSuccessResult(
		map[string]interface{}{
			"entityId":  entityID,
			"relations": result,
			"count":     len(result),
		},
		fmt.Sprintf("Found %d relations", len(result)),
		confidence,
	), nil
}

// EntityValidateExecutor валидирует сущность
type EntityValidateExecutor struct {
	entitiesRepo *repo.EntityRepository
	logger       *log.Logger
}

// NewEntityValidateExecutor создает новый исполнитель для валидации сущностей
func NewEntityValidateExecutor(
	entitiesRepo *repo.EntityRepository,
	logger *log.Logger,
) *EntityValidateExecutor {
	return &EntityValidateExecutor{
		entitiesRepo: entitiesRepo,
		logger:       logger,
	}
}

func (e *EntityValidateExecutor) Capability() Capability {
	return CapabilityEntityValidate
}

func (e *EntityValidateExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["entityId"].(string); !ok {
		return fmt.Errorf("entityId is required")
	}
	return nil
}

func (e *EntityValidateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityID := stepCtx.Input["entityId"].(string)

	entity, err := e.entitiesRepo.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	if entity.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("entity belongs to different tenant")
	}

	if e.logger != nil {
		e.logger.Printf("[EntityValidateExecutor] Validating entity %s", entityID)
	}

	issues := make([]string, 0)
	score := 100.0

	// Проверка обязательных полей
	if entity.CanonicalName == "" {
		issues = append(issues, "canonicalName is required")
		score -= 20
	}

	if entity.Type == "" {
		issues = append(issues, "type is required")
		score -= 10
	}

	// Проверка качества данных
	if len(entity.Aliases) == 0 {
		issues = append(issues, "no aliases provided")
		score -= 5
	}

	if len(entity.KBRefs) == 0 {
		issues = append(issues, "no knowledge base references")
		score -= 10
	}

	if entity.Description == "" {
		issues = append(issues, "no description")
		score -= 5
	}

	if len(entity.Attributes) == 0 {
		issues = append(issues, "no attributes")
		score -= 5
	}

	if score < 0 {
		score = 0
	}

	confidence := score / 100.0
	executionStatus := models.ExecutionStatusSuccess
	if len(issues) > 0 {
		executionStatus = models.ExecutionStatusPartial
	}
	
	return &CapabilityResult{
		Success: len(issues) == 0,
		Output: map[string]interface{}{
			"entityId": entityID,
			"score":    score,
			"issues":   issues,
			"count":    len(issues),
		},
		Message:         fmt.Sprintf("Validation score: %.1f, issues: %d", score, len(issues)),
		ExecutionStatus: executionStatus,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          fmt.Sprintf("Entity validated with score %.1f, %d issues found", score, len(issues)),
	}, nil
}

// EntityMergeAttributesExecutor объединяет атрибуты сущностей
type EntityMergeAttributesExecutor struct {
	entitiesRepo *repo.EntityRepository
	logger       *log.Logger
}

// NewEntityMergeAttributesExecutor создает новый исполнитель для объединения атрибутов
func NewEntityMergeAttributesExecutor(
	entitiesRepo *repo.EntityRepository,
	logger *log.Logger,
) *EntityMergeAttributesExecutor {
	return &EntityMergeAttributesExecutor{
		entitiesRepo: entitiesRepo,
		logger:       logger,
	}
}

func (e *EntityMergeAttributesExecutor) Capability() Capability {
	return CapabilityEntityMergeAttributes
}

func (e *EntityMergeAttributesExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["targetEntityId"].(string); !ok {
		return fmt.Errorf("targetEntityId is required")
	}
	if _, ok := input["sourceEntityIds"].([]interface{}); !ok {
		return fmt.Errorf("sourceEntityIds is required and must be an array")
	}
	return nil
}

func (e *EntityMergeAttributesExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	targetEntityID := stepCtx.Input["targetEntityId"].(string)
	sourceIDsRaw := stepCtx.Input["sourceEntityIds"].([]interface{})

	targetEntity, err := e.entitiesRepo.Get(ctx, targetEntityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target entity: %w", err)
	}

	if targetEntity.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("target entity belongs to different tenant")
	}

	if e.logger != nil {
		e.logger.Printf("[EntityMergeAttributesExecutor] Merging attributes from %d sources into %s", len(sourceIDsRaw), targetEntityID)
	}

	// Инициализируем атрибуты если их нет
	if targetEntity.Attributes == nil {
		targetEntity.Attributes = make(map[string]interface{})
	}

	mergedCount := 0
	sourceEntityIDs := make([]string, 0, len(sourceIDsRaw))

	for _, idRaw := range sourceIDsRaw {
		sourceID, ok := idRaw.(string)
		if !ok {
			continue
		}

		sourceEntity, err := e.entitiesRepo.Get(ctx, sourceID)
		if err != nil {
			continue
		}

		if sourceEntity.TenantID != stepCtx.TenantID {
			continue
		}

		sourceEntityIDs = append(sourceEntityIDs, sourceID)

		// Объединяем атрибуты
		for key, value := range sourceEntity.Attributes {
			if _, exists := targetEntity.Attributes[key]; !exists {
				targetEntity.Attributes[key] = value
				mergedCount++
			}
		}

		// Объединяем алиасы
		aliasSet := make(map[string]bool)
		for _, alias := range targetEntity.Aliases {
			aliasSet[alias] = true
		}
		for _, alias := range sourceEntity.Aliases {
			if !aliasSet[alias] {
				targetEntity.Aliases = append(targetEntity.Aliases, alias)
				aliasSet[alias] = true
			}
		}
	}

	// Обновляем целевую сущность
	if err := e.entitiesRepo.Upsert(ctx, targetEntity); err != nil {
		return nil, fmt.Errorf("failed to update target entity: %w", err)
	}

	confidence := 0.9
	if mergedCount == 0 {
		confidence = 0.7
	}
	return newSuccessResult(
		map[string]interface{}{
			"targetEntityId":  targetEntityID,
			"sourceEntityIds": sourceEntityIDs,
			"mergedAttributes": mergedCount,
			"mergedAliases":    len(targetEntity.Aliases),
		},
		fmt.Sprintf("Merged %d attributes from %d sources", mergedCount, len(sourceEntityIDs)),
		confidence,
	), nil
}

