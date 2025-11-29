package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// ============================================================================
// Ontology Capability Executors
// ============================================================================

// EntitySearchExecutor выполняет поиск сущностей с дедупликацией
type EntitySearchExecutor struct {
	entitiesRepo *repo.EntityRepository
	resolver     *ontology.EntityResolver
	logger       *log.Logger
}

// NewEntitySearchExecutor создает новый исполнитель для поиска сущностей
func NewEntitySearchExecutor(
	entitiesRepo *repo.EntityRepository,
	resolver *ontology.EntityResolver,
	logger *log.Logger,
) *EntitySearchExecutor {
	return &EntitySearchExecutor{
		entitiesRepo: entitiesRepo,
		resolver:     resolver,
		logger:       logger,
	}
}

func (e *EntitySearchExecutor) Capability() Capability {
	return CapabilityEntitySearch
}

func (e *EntitySearchExecutor) ValidateInput(input map[string]interface{}) error {
	// query может быть строкой (имя сущности) или массивом строк
	if _, ok := input["query"].(string); !ok {
		if _, ok := input["query"].([]interface{}); !ok {
			return fmt.Errorf("query is required and must be a string or array of strings")
		}
	}
	return nil
}

func (e *EntitySearchExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	// Поддерживаем как строку, так и массив строк
	var queries []string
	if queryStr, ok := stepCtx.Input["query"].(string); ok {
		queries = []string{queryStr}
	} else if queryArr, ok := stepCtx.Input["query"].([]interface{}); ok {
		queries = make([]string, 0, len(queryArr))
		for _, q := range queryArr {
			if qStr, ok := q.(string); ok {
				queries = append(queries, qStr)
			}
		}
	}

	if len(queries) == 0 {
		return nil, fmt.Errorf("no valid queries provided")
	}

	entityType := models.EntityTypeOther
	if typeStr, ok := stepCtx.Input["entityType"].(string); ok && typeStr != "" {
		entityType = models.EntityType(typeStr)
	}

	maxResults := 10
	if mr, ok := stepCtx.Input["maxResults"].(float64); ok {
		maxResults = int(mr)
	}

	if e.logger != nil {
		e.logger.Printf("[EntitySearchExecutor] Searching for %d entities: %v", len(queries), queries)
	}

	results := make([]map[string]interface{}, 0)
	deduplicated := make(map[string]bool) // ID -> уже добавлен

	for _, query := range queries {
		// Используем EntityResolver для поиска с дедупликацией
		resolved, err := e.resolver.ResolveEntity(ctx, stepCtx.TenantID, query, entityType)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[EntitySearchExecutor] Error resolving %s: %v", query, err)
			}
			continue
		}

		if resolved != nil {
			// Проверяем, не добавили ли мы уже эту сущность
			if deduplicated[resolved.ID] {
				continue
			}

			// Формируем результат
			result := map[string]interface{}{
				"id":            resolved.ID,
				"canonicalName": resolved.CanonicalName,
				"type":          string(resolved.Type),
				"aliases":       resolved.Aliases,
				"description":   resolved.Description,
				"matchedQuery":  query,
			}

			// Добавляем Wikidata ID если есть
			for _, kbRef := range resolved.KBRefs {
				if kbRef.KB == "Wikidata" {
					result["wikidataId"] = kbRef.ExternalID
					result["wikidataConfidence"] = kbRef.Confidence
					break
				}
			}

			results = append(results, result)
			deduplicated[resolved.ID] = true

			if len(results) >= maxResults {
				break
			}
		} else {
			// Сущность не найдена - можем вернуть информацию для создания новой
			results = append(results, map[string]interface{}{
				"canonicalName": query,
				"type":          string(entityType),
				"matchedQuery":  query,
				"found":         false,
			})
		}
	}

	confidence := 0.9
	if len(results) == 0 {
		confidence = 0.6
	}
	return newSuccessResult(
		map[string]interface{}{
			"entities": results,
			"count":    len(results),
		},
		fmt.Sprintf("Found %d entities (deduplicated)", len(results)),
		confidence,
	), nil
}

// EntityDeduplicateExecutor выполняет дедупликацию существующих сущностей
type EntityDeduplicateExecutor struct {
	entitiesRepo *repo.EntityRepository
	resolver     *ontology.EntityResolver
	genaiClient  *gen_combined.Client
	logger       *log.Logger
}

// NewEntityDeduplicateExecutor создает новый исполнитель для дедупликации сущностей
func NewEntityDeduplicateExecutor(
	entitiesRepo *repo.EntityRepository,
	resolver *ontology.EntityResolver,
	genaiClient *gen_combined.Client,
	logger *log.Logger,
) *EntityDeduplicateExecutor {
	return &EntityDeduplicateExecutor{
		entitiesRepo: entitiesRepo,
		resolver:     resolver,
		genaiClient:  genaiClient,
		logger:       logger,
	}
}

func (e *EntityDeduplicateExecutor) Capability() Capability {
	return CapabilityEntityDeduplicate
}

func (e *EntityDeduplicateExecutor) ValidateInput(input map[string]interface{}) error {
	// entityIds - массив ID сущностей для проверки на дубликаты
	if _, ok := input["entityIds"].([]interface{}); !ok {
		return fmt.Errorf("entityIds is required and must be an array of strings")
	}
	return nil
}

func (e *EntityDeduplicateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityIDsRaw := stepCtx.Input["entityIds"].([]interface{})
	entityIDs := make([]string, 0, len(entityIDsRaw))
	for _, id := range entityIDsRaw {
		if idStr, ok := id.(string); ok {
			entityIDs = append(entityIDs, idStr)
		}
	}

	if len(entityIDs) == 0 {
		return nil, fmt.Errorf("no entity IDs provided")
	}

	if e.logger != nil {
		e.logger.Printf("[EntityDeduplicateExecutor] Checking %d entities for duplicates", len(entityIDs))
	}

	// Загружаем все сущности
	entities := make([]*models.Entity, 0, len(entityIDs))
	for _, id := range entityIDs {
		entity, err := e.entitiesRepo.Get(ctx, id)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("[EntityDeduplicateExecutor] Error loading entity %s: %v", id, err)
			}
			continue
		}
		// Проверяем доступ
		if entity.TenantID != stepCtx.TenantID {
			continue
		}
		entities = append(entities, entity)
	}

	if len(entities) < 2 {
		return newSuccessResult(
			map[string]interface{}{
				"duplicates": []interface{}{},
				"merged":     []interface{}{},
			},
			"No duplicates found (less than 2 entities)",
			1.0,
		), nil
	}

	// Используем LLM для определения дубликатов
	duplicates, err := e.findDuplicatesWithLLM(ctx, entities)
	if err != nil {
		return nil, fmt.Errorf("failed to find duplicates: %w", err)
	}

	// Объединяем дубликаты
	merged := make([]map[string]interface{}, 0)
	for _, group := range duplicates {
		if len(group) < 2 {
			continue
		}

		// Выбираем главную сущность (первая в группе или с Wikidata)
		mainEntity := group[0]
		for _, entity := range group {
			// Предпочитаем сущность с Wikidata
			for _, kbRef := range entity.KBRefs {
				if kbRef.KB == "Wikidata" {
					mainEntity = entity
					break
				}
			}
		}

		// Объединяем алиасы и данные
		mergedAliases := make(map[string]bool)
		for _, alias := range mainEntity.Aliases {
			mergedAliases[alias] = true
		}

		// Добавляем canonicalName других сущностей как алиасы
		for _, entity := range group {
			if entity.ID == mainEntity.ID {
				continue
			}
			if !mergedAliases[entity.CanonicalName] {
				mainEntity.Aliases = append(mainEntity.Aliases, entity.CanonicalName)
				mergedAliases[entity.CanonicalName] = true
			}
			// Добавляем алиасы других сущностей
			for _, alias := range entity.Aliases {
				if !mergedAliases[alias] {
					mainEntity.Aliases = append(mainEntity.Aliases, alias)
					mergedAliases[alias] = true
				}
			}
		}

		// Обновляем главную сущность
		if err := e.entitiesRepo.Upsert(ctx, mainEntity); err != nil {
			if e.logger != nil {
				e.logger.Printf("[EntityDeduplicateExecutor] Error updating main entity %s: %v", mainEntity.ID, err)
			}
			continue
		}

		// Помечаем остальные как deprecated
		mergedIDs := make([]string, 0, len(group))
		for _, entity := range group {
			if entity.ID != mainEntity.ID {
				entity.Status = "deprecated"
				entity.Description = fmt.Sprintf("Merged into %s (%s)", mainEntity.CanonicalName, mainEntity.ID)
				if err := e.entitiesRepo.Upsert(ctx, entity); err != nil {
					if e.logger != nil {
						e.logger.Printf("[EntityDeduplicateExecutor] Error deprecating entity %s: %v", entity.ID, err)
					}
				}
				mergedIDs = append(mergedIDs, entity.ID)
			}
		}

		merged = append(merged, map[string]interface{}{
			"mainEntityId":   mainEntity.ID,
			"mainEntityName": mainEntity.CanonicalName,
			"mergedEntityIds": mergedIDs,
		})
	}

	// Сериализуем duplicates в JSON строку для Firestore (Firestore не поддерживает вложенные массивы)
	duplicatesData := make([][]string, 0, len(duplicates))
	for _, group := range duplicates {
		groupIDs := make([]string, 0, len(group))
		for _, entity := range group {
			groupIDs = append(groupIDs, entity.ID)
		}
		duplicatesData = append(duplicatesData, groupIDs)
	}
	duplicatesJSON, _ := json.Marshal(duplicatesData)

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"duplicates": string(duplicatesJSON), // JSON строка вместо вложенных массивов
			"merged":     merged,
			"mergedCount": len(merged),
		},
		Message: fmt.Sprintf("Merged %d duplicate groups", len(merged)),
	}, nil
}

// findDuplicatesWithLLM использует LLM для определения дубликатов
func (e *EntityDeduplicateExecutor) findDuplicatesWithLLM(ctx context.Context, entities []*models.Entity) ([][]*models.Entity, error) {
	if len(entities) < 2 {
		return nil, nil
	}

	// Формируем список сущностей для LLM
	entitiesList := make([]string, 0, len(entities))
	entityMap := make(map[string]*models.Entity) // ID -> Entity

	for _, entity := range entities {
		entityInfo := fmt.Sprintf("ID: %s, Name: %s", entity.ID, entity.CanonicalName)
		if len(entity.Aliases) > 0 {
			entityInfo += fmt.Sprintf(", Aliases: %s", strings.Join(entity.Aliases, ", "))
		}
		if entity.Description != "" {
			entityInfo += fmt.Sprintf(", Description: %s", entity.Description)
		}
		// Добавляем Wikidata ID если есть
		for _, kbRef := range entity.KBRefs {
			if kbRef.KB == "Wikidata" {
				entityInfo += fmt.Sprintf(", Wikidata: %s", kbRef.ExternalID)
				break
			}
		}
		entitiesList = append(entitiesList, entityInfo)
		entityMap[entity.ID] = entity
	}

	prompt := fmt.Sprintf(`You are an expert in entity deduplication. Your task is to identify duplicate entities that refer to the same real-world object.

ENTITIES TO CHECK:
%s

INSTRUCTIONS:
1. Analyze each entity and determine which ones refer to the same real-world object
2. Consider:
   - Different spellings (e.g., "Китай" vs "Кытай" = same country)
   - Different transliterations (e.g., "Nvidia" vs "Нвидиа" = same company)
   - Different name variations (e.g., "Apple Inc." vs "Apple" = same company)
   - Wikidata IDs (entities with same Wikidata ID are definitely duplicates)

3. Group duplicate entities together
4. Return JSON with groups of duplicate entity IDs

Return JSON strictly matching this schema:
{
  "groups": [
    ["entity_id_1", "entity_id_2", ...],
    ["entity_id_3", "entity_id_4", ...]
  ]
}

Each group should contain IDs of entities that refer to the same real-world object.`, strings.Join(entitiesList, "\n"))

	req := gen_combined.GenAIRequest{
		Model:           "gemini_fast",
		Prompt:          prompt,
		MaxOutputTokens: 2048,
		Temperature:     0.3,
	}

	resp, err := e.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Парсим JSON ответ
	var result struct {
		Groups [][]string `json:"groups"`
	}

	// Убираем markdown code fences если есть
	content := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Преобразуем группы ID в группы Entity
	groups := make([][]*models.Entity, 0, len(result.Groups))
	for _, groupIDs := range result.Groups {
		group := make([]*models.Entity, 0, len(groupIDs))
		for _, id := range groupIDs {
			if entity, ok := entityMap[id]; ok {
				group = append(group, entity)
			}
		}
		if len(group) >= 2 {
			groups = append(groups, group)
		}
	}

	return groups, nil
}

// EntityRelationBuildExecutor выполняет построение связей между сущностями
type EntityRelationBuildExecutor struct {
	entitiesRepo *repo.EntityRepository
	relationsRepo *repo.RelationRepository
	resolver     *ontology.EntityResolver
	logger       *log.Logger
}

// NewEntityRelationBuildExecutor создает новый исполнитель для построения связей
func NewEntityRelationBuildExecutor(
	entitiesRepo *repo.EntityRepository,
	relationsRepo *repo.RelationRepository,
	resolver *ontology.EntityResolver,
	logger *log.Logger,
) *EntityRelationBuildExecutor {
	return &EntityRelationBuildExecutor{
		entitiesRepo: entitiesRepo,
		relationsRepo: relationsRepo,
		resolver:     resolver,
		logger:      logger,
	}
}

func (e *EntityRelationBuildExecutor) Capability() Capability {
	return CapabilityEntityRelationBuild
}

func (e *EntityRelationBuildExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["entityId"].(string); !ok {
		return fmt.Errorf("entityId is required")
	}
	// maxDepth может быть float64 (из JSON/Firestore) или int
	maxDepthVal, ok := input["maxDepth"]
	if !ok {
		return fmt.Errorf("maxDepth is required")
	}
	// Проверяем различные числовые типы
	switch maxDepthVal.(type) {
	case float64, int, int64, float32:
		// Валидные числовые типы
		return nil
	default:
		return fmt.Errorf("maxDepth must be a number, got %T", maxDepthVal)
	}
}

func (e *EntityRelationBuildExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	entityID := stepCtx.Input["entityId"].(string)
	
	// maxDepth может быть разных числовых типов (из JSON/Firestore)
	var maxDepth int
	maxDepthVal, ok := stepCtx.Input["maxDepth"]
	if !ok {
		return nil, fmt.Errorf("maxDepth is required")
	}
	
	switch v := maxDepthVal.(type) {
	case float64:
		maxDepth = int(v)
	case float32:
		maxDepth = int(v)
	case int:
		maxDepth = v
	case int64:
		maxDepth = int(v)
	case int32:
		maxDepth = int(v)
	default:
		return nil, fmt.Errorf("maxDepth must be a number, got %T", maxDepthVal)
	}

	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 5 {
		maxDepth = 5 // Ограничиваем глубину для безопасности
	}

	if e.logger != nil {
		e.logger.Printf("[EntityRelationBuildExecutor] Building relations for entity %s, maxDepth=%d", entityID, maxDepth)
	}

	// Загружаем начальную сущность
	entity, err := e.entitiesRepo.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	if entity.TenantID != stepCtx.TenantID {
		return nil, fmt.Errorf("access denied")
	}

	// Строим граф связей
	relations := e.buildRelationGraph(ctx, stepCtx.TenantID, entityID, maxDepth, make(map[string]bool))

	confidence := 0.9
	if len(relations) == 0 {
		confidence = 0.7
	}
	return newSuccessResult(
		map[string]interface{}{
			"entityId":  entityID,
			"relations": relations,
			"count":     len(relations),
		},
		fmt.Sprintf("Built %d relations up to depth %d", len(relations), maxDepth),
		confidence,
	), nil
}

// buildRelationGraph строит граф связей рекурсивно
func (e *EntityRelationBuildExecutor) buildRelationGraph(
	ctx context.Context,
	tenantID string,
	entityID string,
	maxDepth int,
	visited map[string]bool,
) []map[string]interface{} {
	if maxDepth <= 0 || visited[entityID] {
		return nil
	}

	visited[entityID] = true
	relations := make([]map[string]interface{}, 0)

	// Получаем связи где entity является subject
	subjectRelations, _ := e.relationsRepo.ListBySubject(ctx, tenantID, entityID, 100)
	for _, rel := range subjectRelations {
		relations = append(relations, map[string]interface{}{
			"relationId": rel.ID,
			"subjectId":  rel.SubjectID,
			"predicate":  rel.Predicate,
			"objectId":   rel.ObjectID,
			"direction":  string(rel.Direction),
			"depth":      1,
		})

		// Рекурсивно строим связи для object
		if maxDepth > 1 {
			subRelations := e.buildRelationGraph(ctx, tenantID, rel.ObjectID, maxDepth-1, visited)
			relations = append(relations, subRelations...)
		}
	}

	// Получаем связи где entity является object
	objectRelations, _ := e.relationsRepo.ListByObject(ctx, tenantID, entityID, 100)
	for _, rel := range objectRelations {
		relations = append(relations, map[string]interface{}{
			"relationId": rel.ID,
			"subjectId":  rel.SubjectID,
			"predicate":  rel.Predicate,
			"objectId":   rel.ObjectID,
			"direction":  string(rel.Direction),
			"depth":      1,
		})

		// Рекурсивно строим связи для subject
		if maxDepth > 1 {
			subRelations := e.buildRelationGraph(ctx, tenantID, rel.SubjectID, maxDepth-1, visited)
			relations = append(relations, subRelations...)
		}
	}

	return relations
}

