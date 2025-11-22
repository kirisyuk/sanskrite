package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// EntityResolver разрешает дубликаты и находит существующие entities
type EntityResolver struct {
	entitiesRepo *repo.EntityRepository
	emb          ai.EmbeddingProvider
	genaiClient  *gen_combined.Client
	logger       *log.Logger
}

// NewEntityResolver создает новый resolver для entities
func NewEntityResolver(
	entitiesRepo *repo.EntityRepository,
	emb ai.EmbeddingProvider,
	genaiClient *gen_combined.Client,
	logger *log.Logger,
) *EntityResolver {
	return &EntityResolver{
		entitiesRepo: entitiesRepo,
		emb:          emb,
		genaiClient:  genaiClient,
		logger:       logger,
	}
}

// ResolveEntity находит существующий Entity или возвращает nil если нужно создать новый
// Использует несколько стратегий:
// 1. Точное совпадение canonicalName
// 2. Совпадение в aliases
// 3. Семантическое сходство через embeddings (если доступно)
func (r *EntityResolver) ResolveEntity(
	ctx context.Context,
	tenantID string,
	canonicalName string,
	entityType models.EntityType,
) (*models.Entity, error) {
	if canonicalName == "" {
		return nil, nil
	}

	// Нормализуем имя
	normalized := r.normalizeName(canonicalName)

	// Стратегия 1: Точное совпадение canonicalName
	existing, err := r.entitiesRepo.FindByCanonicalName(ctx, tenantID, normalized)
	if err == nil && existing != nil {
		// Проверяем тип - если совпадает, возвращаем существующий
		if existing.Type == entityType || entityType == models.EntityTypeOther {
			if r.logger != nil {
				r.logger.Printf("[EntityResolver] Found exact match: %s -> %s", canonicalName, existing.ID)
			}
			return existing, nil
		}
		// Если тип не совпадает, продолжаем поиск
	}

	// Стратегия 2: Поиск по aliases
	allEntities, err := r.entitiesRepo.ListByTenant(ctx, tenantID, 1000)
	if err == nil {
		for _, entity := range allEntities {
			// Проверяем тип
			if entity.Type != entityType && entityType != models.EntityTypeOther {
				continue
			}

			// Проверяем canonicalName (case-insensitive)
			if strings.EqualFold(r.normalizeName(entity.CanonicalName), normalized) {
				if r.logger != nil {
					r.logger.Printf("[EntityResolver] Found match by canonicalName: %s -> %s", canonicalName, entity.ID)
				}
				return entity, nil
			}

			// Проверяем aliases
			for _, alias := range entity.Aliases {
				if strings.EqualFold(r.normalizeName(alias), normalized) {
					if r.logger != nil {
						r.logger.Printf("[EntityResolver] Found match by alias: %s -> %s", canonicalName, entity.ID)
					}
					// Добавляем новый алиас если его еще нет
					if !r.containsAlias(entity.Aliases, canonicalName) {
						entity.Aliases = append(entity.Aliases, canonicalName)
						_ = r.entitiesRepo.Upsert(ctx, entity)
					}
					return entity, nil
				}
			}
		}
	}

	// Стратегия 3: Использование LLM для разрешения сущности
	// LLM может понять, что "Нвидиа" и "Nvidia" - это одна компания
	// И вернуть либо existingEntityId, либо wikidataId для известных сущностей
	if r.genaiClient != nil && len(allEntities) > 0 {
		llmMatch := r.resolveWithLLM(ctx, canonicalName, entityType, allEntities)
		if llmMatch != nil {
			if r.logger != nil {
				r.logger.Printf("[EntityResolver] Found LLM match: %s -> %s", canonicalName, llmMatch.ID)
			}
			// Добавляем алиас
			if !r.containsAlias(llmMatch.Aliases, canonicalName) {
				llmMatch.Aliases = append(llmMatch.Aliases, canonicalName)
				_ = r.entitiesRepo.Upsert(ctx, llmMatch)
			}
			return llmMatch, nil
		}
	}

	// Стратегия 4: Семантическое сходство через embeddings (если доступно)
	if r.emb != nil && len(allEntities) > 0 {
		// Создаем embedding для нового имени
		embeddings, err := r.emb.EmbedTexts(ctx, []string{normalized})
		if err == nil && len(embeddings) > 0 {
			newEmbedding := embeddings[0]

			// Ищем наиболее похожие entities
			bestMatch := r.findSemanticMatch(ctx, allEntities, newEmbedding, entityType, 0.85) // Порог схожести 0.85
			if bestMatch != nil {
				if r.logger != nil {
					r.logger.Printf("[EntityResolver] Found semantic match: %s -> %s", canonicalName, bestMatch.ID)
				}
				// Добавляем алиас
				if !r.containsAlias(bestMatch.Aliases, canonicalName) {
					bestMatch.Aliases = append(bestMatch.Aliases, canonicalName)
					_ = r.entitiesRepo.Upsert(ctx, bestMatch)
				}
				return bestMatch, nil
			}
		}
	}

	// Стратегия 5: Транслитерация (fallback если LLM не помог)
	if len(allEntities) > 0 {
		transliterated := r.transliterate(normalized)
		for _, entity := range allEntities {
			if entity.Type != entityType && entityType != models.EntityTypeOther {
				continue
			}

			// Проверяем canonicalName после транслитерации
			entityNormalized := r.normalizeName(entity.CanonicalName)
			entityTransliterated := r.transliterate(entityNormalized)
			if strings.EqualFold(transliterated, entityTransliterated) {
				if r.logger != nil {
					r.logger.Printf("[EntityResolver] Found transliteration match: %s -> %s", canonicalName, entity.ID)
				}
				// Добавляем алиас
				if !r.containsAlias(entity.Aliases, canonicalName) {
					entity.Aliases = append(entity.Aliases, canonicalName)
					_ = r.entitiesRepo.Upsert(ctx, entity)
				}
				return entity, nil
			}

			// Проверяем aliases после транслитерации
			for _, alias := range entity.Aliases {
				aliasNormalized := r.normalizeName(alias)
				aliasTransliterated := r.transliterate(aliasNormalized)
				if strings.EqualFold(transliterated, aliasTransliterated) {
					if r.logger != nil {
						r.logger.Printf("[EntityResolver] Found transliteration match by alias: %s -> %s", canonicalName, entity.ID)
					}
					// Добавляем новый алиас
					if !r.containsAlias(entity.Aliases, canonicalName) {
						entity.Aliases = append(entity.Aliases, canonicalName)
						_ = r.entitiesRepo.Upsert(ctx, entity)
					}
					return entity, nil
				}
			}
		}
	}

	// Не найдено - нужно создать новый
	return nil, nil
}

// normalizeName нормализует имя для сравнения
func (r *EntityResolver) normalizeName(name string) string {
	// Убираем лишние пробелы, приводим к lowercase
	normalized := strings.ToLower(strings.TrimSpace(name))
	// Убираем множественные пробелы
	normalized = strings.Join(strings.Fields(normalized), " ")
	// Убираем диакритики и специальные символы для лучшего сравнения
	normalized = r.removeDiacritics(normalized)
	return normalized
}

// transliterate транслитерирует кириллицу в латиницу
func (r *EntityResolver) transliterate(text string) string {
	translitMap := map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
		'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
		'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
		'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
		'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
		'А': "A", 'Б': "B", 'В': "V", 'Г': "G", 'Д': "D", 'Е': "E", 'Ё': "Yo",
		'Ж': "Zh", 'З': "Z", 'И': "I", 'Й': "Y", 'К': "K", 'Л': "L", 'М': "M",
		'Н': "N", 'О': "O", 'П': "P", 'Р': "R", 'С': "S", 'Т': "T", 'У': "U",
		'Ф': "F", 'Х': "H", 'Ц': "Ts", 'Ч': "Ch", 'Ш': "Sh", 'Щ': "Sch",
		'Ъ': "", 'Ы': "Y", 'Ь': "", 'Э': "E", 'Ю': "Yu", 'Я': "Ya",
	}

	var result strings.Builder
	for _, char := range text {
		if replacement, ok := translitMap[char]; ok {
			result.WriteString(replacement)
		} else {
			result.WriteRune(char)
		}
	}
	return result.String()
}

// removeDiacritics убирает диакритики из текста (упрощенная версия)
func (r *EntityResolver) removeDiacritics(text string) string {
	// Убираем основные диакритики (для латиницы)
	replacements := map[string]string{
		"á": "a", "à": "a", "â": "a", "ä": "a", "ã": "a",
		"é": "e", "è": "e", "ê": "e", "ë": "e",
		"í": "i", "ì": "i", "î": "i", "ï": "i",
		"ó": "o", "ò": "o", "ô": "o", "ö": "o", "õ": "o",
		"ú": "u", "ù": "u", "û": "u", "ü": "u",
		"ý": "y", "ÿ": "y",
		"ñ": "n", "ç": "c",
		"Á": "A", "À": "A", "Â": "A", "Ä": "A", "Ã": "A",
		"É": "E", "È": "E", "Ê": "E", "Ë": "E",
		"Í": "I", "Ì": "I", "Î": "I", "Ï": "I",
		"Ó": "O", "Ò": "O", "Ô": "O", "Ö": "O", "Õ": "O",
		"Ú": "U", "Ù": "U", "Û": "U", "Ü": "U",
		"Ý": "Y", "Ñ": "N", "Ç": "C",
	}

	result := text
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	return result
}

// containsAlias проверяет, содержит ли список алиасов данное имя
func (r *EntityResolver) containsAlias(aliases []string, name string) bool {
	normalized := r.normalizeName(name)
	for _, alias := range aliases {
		if strings.EqualFold(r.normalizeName(alias), normalized) {
			return true
		}
	}
	return false
}

// findSemanticMatch находит наиболее семантически похожую entity через embeddings
func (r *EntityResolver) findSemanticMatch(
	ctx context.Context,
	entities []*models.Entity,
	targetEmbedding []float64,
	entityType models.EntityType,
	threshold float64,
) *models.Entity {
	if len(targetEmbedding) == 0 {
		return nil
	}

	var bestMatch *models.Entity
	bestScore := 0.0

	// Создаем embeddings для всех entities на лету
	// В будущем можно кэшировать или хранить в Entity
	entityTexts := make([]string, 0, len(entities))
	entityMap := make(map[int]*models.Entity)
	index := 0

	for _, entity := range entities {
		// Проверяем тип
		if entity.Type != entityType && entityType != models.EntityTypeOther {
			continue
		}

		// Формируем текст для embedding: canonicalName + aliases
		text := entity.CanonicalName
		if len(entity.Aliases) > 0 {
			text += " " + strings.Join(entity.Aliases, " ")
		}
		entityTexts = append(entityTexts, text)
		entityMap[index] = entity
		index++
	}

	if len(entityTexts) == 0 {
		return nil
	}

	// Получаем embeddings для всех entities
	embeddings, err := r.emb.EmbedTexts(ctx, entityTexts)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("[EntityResolver] Failed to get embeddings for semantic match: %v", err)
		}
		return nil
	}

	// Вычисляем косинусное сходство
	for i, entityEmbedding := range embeddings {
		if len(entityEmbedding) != len(targetEmbedding) {
			continue
		}

		score := r.cosineSimilarity(targetEmbedding, entityEmbedding)
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = entityMap[i]
		}
	}

	if bestMatch != nil && r.logger != nil {
		r.logger.Printf("[EntityResolver] Semantic match score: %.3f", bestScore)
	}

	return bestMatch
}

// cosineSimilarity вычисляет косинусное сходство между двумя векторами
func (r *EntityResolver) cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// resolveWithLLM использует LLM для разрешения сущности
// LLM может понять, что "Нвидиа" и "Nvidia" - это одна компания
// LLM возвращает canonicalName существующей entity или wikidataId
// Система сама находит entity по этим данным
func (r *EntityResolver) resolveWithLLM(
	ctx context.Context,
	canonicalName string,
	entityType models.EntityType,
	existingEntities []*models.Entity,
) *models.Entity {
	// Формируем список существующих entities для контекста (без внутренних ID)
	entitiesList := make([]string, 0, len(existingEntities))
	nameToEntity := make(map[string]*models.Entity) // canonicalName -> Entity
	wikidataToEntity := make(map[string]*models.Entity) // Wikidata ID -> Entity
	
	for _, entity := range existingEntities {
		if entity.Type != entityType && entityType != models.EntityTypeOther {
			continue
		}
		
		// Формируем описание entity без внутреннего ID
		entityInfo := fmt.Sprintf("- Name: %s", entity.CanonicalName)
		if len(entity.Aliases) > 0 {
			entityInfo += fmt.Sprintf(", Aliases: %s", strings.Join(entity.Aliases, ", "))
		}
		// Добавляем Wikidata ID если есть
		for _, kbRef := range entity.KBRefs {
			if kbRef.KB == "Wikidata" {
				entityInfo += fmt.Sprintf(", Wikidata: %s", kbRef.ExternalID)
				wikidataToEntity[kbRef.ExternalID] = entity
			}
		}
		entitiesList = append(entitiesList, entityInfo)
		
		// Индексируем по canonicalName (normalized)
		normalizedName := r.normalizeName(entity.CanonicalName)
		nameToEntity[normalizedName] = entity
		// Также индексируем по aliases
		for _, alias := range entity.Aliases {
			normalizedAlias := r.normalizeName(alias)
			if _, exists := nameToEntity[normalizedAlias]; !exists {
				nameToEntity[normalizedAlias] = entity
			}
		}
	}

	if len(entitiesList) == 0 {
		return nil
	}

	// JSON Schema для ответа LLM
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"existingEntityName": map[string]interface{}{
				"type":        "string",
				"description": "Canonical name существующей entity, если искомая сущность относится к одной из них (например, 'Нвидиа' -> 'Nvidia'). Используйте точное имя из списка существующих entities.",
			},
			"wikidataId": map[string]interface{}{
				"type":        "string",
				"description": "Wikidata Q-ID для известных сущностей (например, 'Q9686' для Nvidia), если сущность не найдена в существующих но является известной",
			},
			"confidence": map[string]interface{}{
				"type":        "number",
				"description": "Уверенность в ответе (0.0-1.0)",
				"minimum":     0.0,
				"maximum":     1.0,
			},
			"reasoning": map[string]interface{}{
				"type":        "string",
				"description": "Краткое объяснение решения",
			},
		},
	}

	prompt := fmt.Sprintf(`You are an expert in entity resolution. Your task is to determine if a given entity name refers to an existing entity in the system, or identify its Wikidata ID.

ENTITY TO RESOLVE:
Name: "%s"
Type: %s

EXISTING ENTITIES IN SYSTEM:
%s

INSTRUCTIONS:
1. Check if the entity name refers to one of the existing entities (considering different spellings, transliterations, aliases)
   - Example: "Нвидиа" (Cyrillic) refers to "Nvidia" (Latin) - same company
   - Example: "Apple Inc." and "Apple" refer to the same entity
   - Example: "Microsoft Corp" and "Microsoft Corporation" refer to the same entity

2. If you find a match, return the EXACT canonical name from the existing entities list (e.g., "Nvidia", not "Нвидиа")

3. If no match found but the entity is well-known, return its Wikidata Q-ID (e.g., "Q9686" for Nvidia)

4. If the entity is not found and not well-known, return null for both fields

IMPORTANT: Use the exact canonical name from the existing entities list, not the input name.

Return JSON strictly matching the schema.`, canonicalName, entityType, strings.Join(entitiesList, "\n"))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_fast",
		GroundingEnabled:  false,
		SystemInstruction: "You are an expert in entity resolution and disambiguation. You understand different languages, transliterations, and naming variations. Be precise and return only high-confidence matches. Always use the exact canonical name from the existing entities list.",
		Temperature:       0.3,
		MaxOutputTokens:   512,
	}

	resp, err := r.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("[EntityResolver] LLM resolution failed: %v", err)
		}
		return nil
	}

	// Парсим ответ
	var result struct {
		ExistingEntityName *string `json:"existingEntityName"`
		WikidataID         *string `json:"wikidataId"`
		Confidence         float64 `json:"confidence"`
		Reasoning          string  `json:"reasoning"`
	}

	// Пытаемся распарсить JSON
	content := resp.Content
	// Убираем markdown code blocks если есть
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var cleaned []string
		inCodeBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if !inCodeBlock {
				cleaned = append(cleaned, line)
			}
		}
		content = strings.Join(cleaned, "\n")
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		if r.logger != nil {
			r.logger.Printf("[EntityResolver] Failed to parse LLM response: %v, content: %s", err, content)
		}
		return nil
	}

	// Проверяем уверенность
	if result.Confidence < 0.7 {
		if r.logger != nil {
			r.logger.Printf("[EntityResolver] LLM confidence too low: %.2f", result.Confidence)
		}
		return nil
	}

	// Если найден existingEntityName, ищем entity по имени
	if result.ExistingEntityName != nil && *result.ExistingEntityName != "" {
		normalizedName := r.normalizeName(*result.ExistingEntityName)
		if entity, ok := nameToEntity[normalizedName]; ok {
			if r.logger != nil {
				r.logger.Printf("[EntityResolver] LLM matched '%s' to existing entity '%s' (reasoning: %s)", canonicalName, *result.ExistingEntityName, result.Reasoning)
			}
			return entity
		}
		// Если не нашли по normalized name, пробуем точное совпадение
		for _, entity := range existingEntities {
			if entity.Type == entityType || entityType == models.EntityTypeOther {
				if strings.EqualFold(entity.CanonicalName, *result.ExistingEntityName) {
					if r.logger != nil {
						r.logger.Printf("[EntityResolver] LLM matched '%s' to existing entity '%s' (reasoning: %s)", canonicalName, *result.ExistingEntityName, result.Reasoning)
					}
					return entity
				}
			}
		}
	}

	// Если найден Wikidata ID, ищем entity по Wikidata ID
	if result.WikidataID != nil && *result.WikidataID != "" {
		if entity, ok := wikidataToEntity[*result.WikidataID]; ok {
			if r.logger != nil {
				r.logger.Printf("[EntityResolver] LLM found Wikidata ID %s, matched to existing entity '%s' (reasoning: %s)", *result.WikidataID, entity.CanonicalName, result.Reasoning)
			}
			return entity
		}
		// Если не нашли по Wikidata ID, логируем для будущего использования
		if r.logger != nil {
			r.logger.Printf("[EntityResolver] LLM found Wikidata ID: %s (reasoning: %s) - will be used during enrichment", *result.WikidataID, result.Reasoning)
		}
	}

	return nil
}

// MergeEntities объединяет два Entity (в будущем для продвинутого resolution)
func (r *EntityResolver) MergeEntities(
	ctx context.Context,
	primary *models.Entity,
	secondary *models.Entity,
) error {
	// Объединяем aliases
	allAliases := make(map[string]bool)
	for _, alias := range primary.Aliases {
		allAliases[alias] = true
	}
	for _, alias := range secondary.Aliases {
		if !allAliases[alias] {
			primary.Aliases = append(primary.Aliases, alias)
		}
	}

	// Объединяем KBRefs
	kbRefsMap := make(map[string]*models.KBReference)
	for _, ref := range primary.KBRefs {
		kbRefsMap[ref.KB+":"+ref.ExternalID] = &ref
	}
	for _, ref := range secondary.KBRefs {
		key := ref.KB + ":" + ref.ExternalID
		if _, exists := kbRefsMap[key]; !exists {
			primary.KBRefs = append(primary.KBRefs, ref)
		}
	}

	// Объединяем Attributes
	if primary.Attributes == nil {
		primary.Attributes = make(map[string]interface{})
	}
	for k, v := range secondary.Attributes {
		if _, exists := primary.Attributes[k]; !exists {
			primary.Attributes[k] = v
		}
	}

	// Обновляем primary
	primary.UpdatedAt = primary.UpdatedAt // Обновим время
	if err := r.entitiesRepo.Upsert(ctx, primary); err != nil {
		return fmt.Errorf("failed to update primary entity: %w", err)
	}

	// Помечаем secondary как deprecated
	secondary.Status = "deprecated"
	secondary.Attributes["mergedInto"] = primary.ID
	if err := r.entitiesRepo.Upsert(ctx, secondary); err != nil {
		return fmt.Errorf("failed to deprecate secondary entity: %w", err)
	}

	if r.logger != nil {
		r.logger.Printf("[EntityResolver] Merged entity %s into %s", secondary.ID, primary.ID)
	}

	return nil
}

