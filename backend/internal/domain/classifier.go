package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DomainClassifier классифицирует объекты к доменам через LLM
type DomainClassifier struct {
	hierarchyService *DomainHierarchyService
	conceptsRepo     *repo.ConceptRepository
	genaiClient      *gen_combined.Client
	executor         *ai.AITaskExecutor
	cache            map[string]*CachedClassification // кэш классификаций
	logger           *log.Logger
}

// NewDomainClassifier создает новый классификатор доменов
func NewDomainClassifier(
	hierarchyService *DomainHierarchyService,
	conceptsRepo *repo.ConceptRepository,
	genaiClient *gen_combined.Client,
	executor *ai.AITaskExecutor,
	logger *log.Logger,
) *DomainClassifier {
	return &DomainClassifier{
		hierarchyService: hierarchyService,
		conceptsRepo:     conceptsRepo,
		genaiClient:      genaiClient,
		executor:         executor,
		cache:            make(map[string]*CachedClassification),
		logger:           logger,
	}
}

// ClassifyEntity определяет домены для Entity через LLM
func (c *DomainClassifier) ClassifyEntity(
	ctx context.Context,
	tenantID string,
	entity *models.Entity,
	contextText string,
) ([]*DomainClassification, error) {
	// Проверяем кэш
	cacheKey := c.buildCacheKey(entity.ID, "Entity", entity)
	if cached, ok := c.cache[cacheKey]; ok {
		if c.logger != nil {
			c.logger.Printf("[DomainClassifier] Using cached classification for Entity %s", entity.ID)
		}
		return cached.Classifications, nil
	}

	// Получаем доступные домены
	availableDomains, err := c.getAvailableDomains(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get available domains: %w", err)
	}

	// Формируем промпт для LLM
	prompt := c.buildEntityClassificationPrompt(entity, contextText, availableDomains)
	schema := c.buildEntityClassificationSchema()

	// Используем AITaskExecutor с верификатором для проверки JSON десериализации
	if c.executor == nil {
		// Fallback к старому способу
		opts := gen_combined.Options{
			ModelOverride:     "gemini_fast",
			GroundingEnabled:  false,
			SystemInstruction: "You are an expert domain classifier. Classify entities into business domains based on their characteristics, activities, and context. Use structured JSON output only.",
			Temperature:       0.3,
			MaxOutputTokens:   2048,
		}
		resp, err := c.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
		if err != nil {
			return nil, fmt.Errorf("LLM classification failed: %w", err)
		}
		var result struct {
			PrimaryDomain struct {
				DomainPath []string `json:"domainPath"`
				Confidence float64  `json:"confidence"`
				Reasoning  string   `json:"reasoning"`
			} `json:"primaryDomain"`
			SecondaryDomains []struct {
				DomainPath []string `json:"domainPath"`
				Confidence float64  `json:"confidence"`
			} `json:"secondaryDomains,omitempty"`
		}
		if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
			return nil, fmt.Errorf("failed to parse LLM response: %w", err)
		}
		return c.processClassificationResult(&result, ctx, tenantID)
	}

	validateFunc := func(data map[string]interface{}) error {
		// Проверяем только десериализацию JSON
		return nil
	}
	verifier := ai.NewGenericVerifier(validateFunc)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt + "\n\nReturn JSON strictly matching the schema. Do not include markdown code fences.",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            2048,
			MinContextWindow:         4000,
		},
		Verifier: verifier,
	}

	parsed, err := c.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Преобразуем результат
	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	// Преобразуем map в структуру
	var result struct {
		PrimaryDomain struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
			Reasoning  string   `json:"reasoning"`
		} `json:"primaryDomain"`
		SecondaryDomains []struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
		} `json:"secondaryDomains,omitempty"`
	}

	if primary, ok := resultMap["primaryDomain"].(map[string]interface{}); ok {
		if path, ok := primary["domainPath"].([]interface{}); ok {
			result.PrimaryDomain.DomainPath = make([]string, len(path))
			for i, v := range path {
				if s, ok := v.(string); ok {
					result.PrimaryDomain.DomainPath[i] = s
				}
			}
		}
		if conf, ok := primary["confidence"].(float64); ok {
			result.PrimaryDomain.Confidence = conf
		}
		if reason, ok := primary["reasoning"].(string); ok {
			result.PrimaryDomain.Reasoning = reason
		}
	}
	if secondary, ok := resultMap["secondaryDomains"].([]interface{}); ok {
		result.SecondaryDomains = make([]struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
		}, len(secondary))
		for i, sec := range secondary {
			if secMap, ok := sec.(map[string]interface{}); ok {
				if path, ok := secMap["domainPath"].([]interface{}); ok {
					result.SecondaryDomains[i].DomainPath = make([]string, len(path))
					for j, v := range path {
						if s, ok := v.(string); ok {
							result.SecondaryDomains[i].DomainPath[j] = s
						}
					}
				}
				if conf, ok := secMap["confidence"].(float64); ok {
					result.SecondaryDomains[i].Confidence = conf
				}
			}
		}
	}

	return c.processClassificationResult(&result, ctx, tenantID)
}

func (c *DomainClassifier) processClassificationResult(result *struct {
	PrimaryDomain struct {
		DomainPath []string `json:"domainPath"`
		Confidence float64  `json:"confidence"`
		Reasoning  string   `json:"reasoning"`
	} `json:"primaryDomain"`
	SecondaryDomains []struct {
		DomainPath []string `json:"domainPath"`
		Confidence float64  `json:"confidence"`
	} `json:"secondaryDomains,omitempty"`
}, ctx context.Context, tenantID string) ([]*DomainClassification, error) {

	// Преобразуем в DomainClassification
	classifications := []*DomainClassification{}

	// Primary domain
	if len(result.PrimaryDomain.DomainPath) > 0 {
		domainConcept, err := c.hierarchyService.GetDomainConcept(ctx, tenantID, result.PrimaryDomain.DomainPath)
		if err == nil {
			classifications = append(classifications, &DomainClassification{
				DomainConceptID: domainConcept.ID,
				DomainPath:      result.PrimaryDomain.DomainPath,
				Confidence:      result.PrimaryDomain.Confidence,
				Relevance:       "primary",
				Reasoning:       result.PrimaryDomain.Reasoning,
			})
		}
	}

	// Secondary domains
	for _, sec := range result.SecondaryDomains {
		if len(sec.DomainPath) > 0 {
			domainConcept, err := c.hierarchyService.GetDomainConcept(ctx, tenantID, sec.DomainPath)
			if err == nil {
				classifications = append(classifications, &DomainClassification{
					DomainConceptID: domainConcept.ID,
					DomainPath:      sec.DomainPath,
					Confidence:      sec.Confidence,
					Relevance:       "secondary",
				})
			}
		}
	}

	return classifications, nil
}

// executeClassificationTask выполняет LLM запрос для классификации
func (c *DomainClassifier) executeClassificationTask(ctx context.Context, prompt string, schema map[string]interface{}) (map[string]interface{}, error) {
	validateFunc := func(data map[string]interface{}) error {
		return nil
	}
	verifier := ai.NewGenericVerifier(validateFunc)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt + "\n\nReturn JSON strictly matching the schema. Do not include markdown code fences.",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            2048,
			MinContextWindow:         4000,
		},
		Verifier: verifier,
	}

	parsed, err := c.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}

	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	return resultMap, nil
}

// ClassifyEvent определяет домены для Event через LLM
func (c *DomainClassifier) ClassifyEvent(
	ctx context.Context,
	tenantID string,
	event *models.Event,
	contextText string,
) ([]*DomainClassification, error) {
	// Проверяем кэш
	cacheKey := c.buildCacheKey(event.ID, "Event", event)
	if cached, ok := c.cache[cacheKey]; ok {
		if c.logger != nil {
			c.logger.Printf("[DomainClassifier] Using cached classification for Event %s", event.ID)
		}
		return cached.Classifications, nil
	}

	// Получаем доступные домены
	availableDomains, err := c.getAvailableDomains(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get available domains: %w", err)
	}

	// Формируем промпт для LLM
	prompt := c.buildEventClassificationPrompt(event, contextText, availableDomains)
	schema := c.buildEventClassificationSchema()

	resultMap, err := c.executeClassificationTask(ctx, prompt, schema)
	if err != nil {
		return nil, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Преобразуем map в структуру
	var result struct {
		Domains []struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
			Reasoning  string   `json:"reasoning"`
			Relevance  string   `json:"relevance"`
		} `json:"domains"`
	}

	if domains, ok := resultMap["domains"].([]interface{}); ok {
		result.Domains = make([]struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
			Reasoning  string   `json:"reasoning"`
			Relevance  string   `json:"relevance"`
		}, len(domains))
		for i, dom := range domains {
			if domMap, ok := dom.(map[string]interface{}); ok {
				if path, ok := domMap["domainPath"].([]interface{}); ok {
					result.Domains[i].DomainPath = make([]string, len(path))
					for j, v := range path {
						if s, ok := v.(string); ok {
							result.Domains[i].DomainPath[j] = s
						}
					}
				}
				if conf, ok := domMap["confidence"].(float64); ok {
					result.Domains[i].Confidence = conf
				}
				if reason, ok := domMap["reasoning"].(string); ok {
					result.Domains[i].Reasoning = reason
				}
				if relevance, ok := domMap["relevance"].(string); ok {
					result.Domains[i].Relevance = relevance
				}
			}
		}
	}

	// Преобразуем в DomainClassification
	classifications := []*DomainClassification{}
	for _, dom := range result.Domains {
		if len(dom.DomainPath) > 0 {
			domainConcept, err := c.hierarchyService.GetDomainConcept(ctx, tenantID, dom.DomainPath)
			if err == nil {
				classifications = append(classifications, &DomainClassification{
					DomainConceptID: domainConcept.ID,
					DomainPath:      dom.DomainPath,
					Confidence:      dom.Confidence,
					Relevance:       dom.Relevance,
					Reasoning:       dom.Reasoning,
				})
			}
		}
	}

	// Кэшируем результат
	c.cache[cacheKey] = &CachedClassification{
		Classifications: classifications,
		CachedAt:        time.Now().Unix(),
	}

	return classifications, nil
}

// ClassifyClaim определяет домены для Claim через LLM
func (c *DomainClassifier) ClassifyClaim(
	ctx context.Context,
	tenantID string,
	claim *models.Claim,
	contextText string,
) ([]*DomainClassification, error) {
	// Проверяем кэш
	cacheKey := c.buildCacheKey(claim.ID, "Claim", claim)
	if cached, ok := c.cache[cacheKey]; ok {
		if c.logger != nil {
			c.logger.Printf("[DomainClassifier] Using cached classification for Claim %s", claim.ID)
		}
		return cached.Classifications, nil
	}

	// Получаем доступные домены
	availableDomains, err := c.getAvailableDomains(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get available domains: %w", err)
	}

	// Формируем промпт для LLM
	prompt := c.buildClaimClassificationPrompt(claim, contextText, availableDomains)
	schema := c.buildClaimClassificationSchema()

	resultMap, err := c.executeClassificationTask(ctx, prompt, schema)
	if err != nil {
		return nil, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Преобразуем map в структуру
	var result struct {
		PrimaryDomain struct {
			DomainPath []string `json:"domainPath"`
			Confidence float64  `json:"confidence"`
			Reasoning  string   `json:"reasoning"`
		} `json:"primaryDomain"`
	}

	if primary, ok := resultMap["primaryDomain"].(map[string]interface{}); ok {
		if path, ok := primary["domainPath"].([]interface{}); ok {
			result.PrimaryDomain.DomainPath = make([]string, len(path))
			for i, v := range path {
				if s, ok := v.(string); ok {
					result.PrimaryDomain.DomainPath[i] = s
				}
			}
		}
		if conf, ok := primary["confidence"].(float64); ok {
			result.PrimaryDomain.Confidence = conf
		}
		if reason, ok := primary["reasoning"].(string); ok {
			result.PrimaryDomain.Reasoning = reason
		}
	}

	// Преобразуем в DomainClassification
	classifications := []*DomainClassification{}

	if len(result.PrimaryDomain.DomainPath) > 0 {
		domainConcept, err := c.hierarchyService.GetDomainConcept(ctx, tenantID, result.PrimaryDomain.DomainPath)
		if err == nil {
			classifications = append(classifications, &DomainClassification{
				DomainConceptID: domainConcept.ID,
				DomainPath:      result.PrimaryDomain.DomainPath,
				Confidence:      result.PrimaryDomain.Confidence,
				Relevance:       "primary",
				Reasoning:       result.PrimaryDomain.Reasoning,
			})
		}
	}

	// Кэшируем результат
	c.cache[cacheKey] = &CachedClassification{
		Classifications: classifications,
		CachedAt:        time.Now().Unix(),
	}

	return classifications, nil
}

// getAvailableDomains возвращает список доступных доменов для промпта
func (c *DomainClassifier) getAvailableDomains(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	allConcepts, err := c.conceptsRepo.ListByTenantAndKind(ctx, tenantID, models.ConceptKindDomain, 1000)
	if err != nil {
		return nil, err
	}

	domains := []map[string]interface{}{}
	for _, concept := range allConcepts {
		path, err := c.hierarchyService.GetDomainPath(ctx, concept.ID)
		if err != nil {
			continue
		}
		domains = append(domains, map[string]interface{}{
			"path": path,
			"id":   concept.ID,
		})
	}

	return domains, nil
}

// buildCacheKey создает ключ для кэша
func (c *DomainClassifier) buildCacheKey(objectID, objectKind string, obj interface{}) string {
	// Простой хэш на основе ID и kind
	return fmt.Sprintf("%s:%s", objectKind, objectID)
}

// buildEntityClassificationPrompt создает промпт для классификации Entity
func (c *DomainClassifier) buildEntityClassificationPrompt(
	entity *models.Entity,
	contextText string,
	availableDomains []map[string]interface{},
) string {
	domainsJSON, _ := json.Marshal(availableDomains)

	return fmt.Sprintf(`Classify the following entity into business domains.

ENTITY:
- Canonical Name: %s
- Type: %s
- Description: %s
- Attributes: %s

CONTEXT:
%s

AVAILABLE DOMAINS:
%s

Return JSON with primary domain and optional secondary domains. Each domain must be a path array like ["Economic", "Financial", "Banking"].

Schema:
{
  "primaryDomain": {
    "domainPath": ["string"],
    "confidence": 0.0-1.0,
    "reasoning": "string"
  },
  "secondaryDomains": [
    {
      "domainPath": ["string"],
      "confidence": 0.0-1.0
    }
  ]
}`,
		entity.CanonicalName,
		entity.Type,
		entity.Description,
		c.formatAttributes(entity.Attributes),
		contextText,
		string(domainsJSON),
	)
}

// buildEventClassificationPrompt создает промпт для классификации Event
func (c *DomainClassifier) buildEventClassificationPrompt(
	event *models.Event,
	contextText string,
	availableDomains []map[string]interface{},
) string {
	domainsJSON, _ := json.Marshal(availableDomains)

	return fmt.Sprintf(`Classify the following event into business domains.

EVENT:
- Title: %s
- Type: %s
- Description: %s
- Occurred At: %s
- Participants: %d

CONTEXT:
%s

AVAILABLE DOMAINS:
%s

Return JSON with domains array. Each domain must have path, confidence, relevance (primary/secondary/tertiary), and reasoning.`,
		event.Title,
		event.Type,
		event.Description,
		c.formatTime(event.OccurredAt),
		len(event.Participants),
		contextText,
		string(domainsJSON),
	)
}

// buildClaimClassificationPrompt создает промпт для классификации Claim
func (c *DomainClassifier) buildClaimClassificationPrompt(
	claim *models.Claim,
	contextText string,
	availableDomains []map[string]interface{},
) string {
	domainsJSON, _ := json.Marshal(availableDomains)

	return fmt.Sprintf(`Classify the following claim into business domains.

CLAIM:
- Subject: %s (%s)
- Predicate: %s
- Object: %s
- Confidence: %.2f

CONTEXT:
%s

AVAILABLE DOMAINS:
%s

Return JSON with primary domain. Domain must be a path array like ["Economic", "Financial", "Banking"].

Schema:
{
  "primaryDomain": {
    "domainPath": ["string"],
    "confidence": 0.0-1.0,
    "reasoning": "string"
  }
}`,
		claim.SubjectRef.ID,
		claim.SubjectRef.Kind,
		claim.Predicate,
		c.formatObjectRef(claim.ObjectRef),
		claim.Confidence,
		contextText,
		string(domainsJSON),
	)
}

// buildEntityClassificationSchema создает JSON схему для Entity классификации
func (c *DomainClassifier) buildEntityClassificationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"primaryDomain": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domainPath": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					"reasoning":  map[string]interface{}{"type": "string"},
				},
				"required": []string{"domainPath", "confidence", "reasoning"},
			},
			"secondaryDomains": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"domainPath": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
					"required": []string{"domainPath", "confidence"},
				},
			},
		},
		"required": []string{"primaryDomain"},
	}
}

// buildEventClassificationSchema создает JSON схему для Event классификации
func (c *DomainClassifier) buildEventClassificationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"domains": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"domainPath": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"reasoning":  map[string]interface{}{"type": "string"},
						"relevance": map[string]interface{}{
							"type": "string",
							"enum": []string{"primary", "secondary", "tertiary"},
						},
					},
					"required": []string{"domainPath", "confidence", "relevance"},
				},
			},
		},
		"required": []string{"domains"},
	}
}

// buildClaimClassificationSchema создает JSON схему для Claim классификации
func (c *DomainClassifier) buildClaimClassificationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"primaryDomain": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domainPath": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					"reasoning":  map[string]interface{}{"type": "string"},
				},
				"required": []string{"domainPath", "confidence", "reasoning"},
			},
		},
		"required": []string{"primaryDomain"},
	}
}

// formatAttributes форматирует атрибуты для промпта
func (c *DomainClassifier) formatAttributes(attrs map[string]interface{}) string {
	if len(attrs) == 0 {
		return "None"
	}
	attrsJSON, _ := json.Marshal(attrs)
	return string(attrsJSON)
}

// formatTime форматирует время для промпта
func (c *DomainClassifier) formatTime(t *time.Time) string {
	if t == nil {
		return "Unknown"
	}
	return t.Format(time.RFC3339)
}

// formatObjectRef форматирует ObjectRef для промпта
func (c *DomainClassifier) formatObjectRef(obj models.ObjectRef) string {
	if obj.Kind == models.ReferenceKindLiteral {
		return fmt.Sprintf("%v", obj.Value)
	}
	if obj.ID != nil {
		return *obj.ID
	}
	return "Unknown"
}

