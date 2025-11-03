package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// EntitiesAgent агент для поиска информации об entities и обнаружения связей
type EntitiesAgent struct {
	id               string
	name             string
	displayName      string
	description      string
	genaiClient      *gen_combined.Client
	logger           *log.Logger
	usageMetricsRepo interface {
		CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int) (string, error)
	}
	modelManager interface {
		CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error)
	}
}

// NewEntitiesAgent создает новый Entities Agent
func NewEntitiesAgent(genaiClient *gen_combined.Client, logger *log.Logger) *EntitiesAgent {
	return &EntitiesAgent{
		id:          "entities",
		name:        "Entities Research Agent",
		displayName: "Entity Research",
		description: "Searches for information about entities and discovers relations",
		genaiClient: genaiClient,
		logger:      logger,
	}
}

// WithUsageMetrics sets the usage metrics repository and model manager
func (a *EntitiesAgent) WithUsageMetrics(repo interface {
	CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int) (string, error)
}, modelMgr interface {
	CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error)
}) *EntitiesAgent {
	a.usageMetricsRepo = repo
	a.modelManager = modelMgr
	return a
}

func (a *EntitiesAgent) ID() string {
	return a.id
}

func (a *EntitiesAgent) Name() string {
	return a.name
}

func (a *EntitiesAgent) DisplayName() string {
	return a.displayName
}

func (a *EntitiesAgent) Description() string {
	return a.description
}

func (a *EntitiesAgent) Skills() []string {
	return []string{"web_search", "entity_extraction", "relation_discovery"}
}

func (a *EntitiesAgent) Type() string {
	return "internal"
}

func (a *EntitiesAgent) IsExternal() bool {
	return false
}

func (a *EntitiesAgent) Health(ctx context.Context) (*AgentHealth, error) {
	return &AgentHealth{
		Status:    "healthy",
		Message:   "EntitiesAgent is operational",
		LastCheck: time.Now(),
	}, nil
}

func (a *EntitiesAgent) TaskSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"context": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tenantId":  map[string]interface{}{"type": "string"},
					"projectId": map[string]interface{}{"type": "string"},
					"userId":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"tenantId", "projectId", "userId"},
			},
			"entities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":           map[string]interface{}{"type": "string"},
						"name":         map[string]interface{}{"type": "string"},
						"type":         map[string]interface{}{"type": "string"},
						"knownAliases": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					},
					"required": []string{"name", "type"},
				},
			},
			"maxDepth": map[string]interface{}{
				"type":    "integer",
				"minimum": 1,
				"maximum": 5,
				"default": 2,
			},
			"includeDetails": map[string]interface{}{
				"type":    "boolean",
				"default": true,
			},
			"metadata": map[string]interface{}{
				"type": "object",
			},
		},
		"required": []string{"context", "entities"},
	}
}

func (a *EntitiesAgent) ResultSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entities": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "object"},
			},
			"relations": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "object"},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"explanation": map[string]interface{}{"type": "string"},
			"sources": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "object"},
			},
		},
		"required": []string{"entities", "relations", "confidence", "explanation", "sources"},
	}
}

func (a *EntitiesAgent) Execute(ctx context.Context, taskJSON []byte) ([]byte, error) {
	return a.ExecuteWithStream(ctx, taskJSON, func(Update) {})
}

func (a *EntitiesAgent) ExecuteWithStream(
	ctx context.Context,
	taskJSON []byte,
	onUpdate func(Update),
) ([]byte, error) {
	startTime := time.Now()
	var task EntitiesTask

	if err := json.Unmarshal(taskJSON, &task); err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task unmarshal failed: %v", err)
		}
		return nil, fmt.Errorf("invalid task schema: %w", err)
	}

	// Валидация projectId
	if task.Context.ProjectID == "" {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task validation failed: projectId is required")
		}
		return nil, fmt.Errorf("projectId is required")
	}

	// Валидация entities
	if len(task.Entities) == 0 {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task validation failed: at least one entity is required")
		}
		return nil, fmt.Errorf("at least one entity is required")
	}

	taskID := generateTaskID()
	maxDepth := task.MaxDepth
	if maxDepth == 0 {
		maxDepth = 2
	}
	if maxDepth > 5 {
		maxDepth = 5
	}
	includeDetails := task.IncludeDetails
	if !includeDetails {
		includeDetails = true // default true
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s started: entities=%d, maxDepth=%d, includeDetails=%v, projectId=%s",
			taskID, len(task.Entities), maxDepth, includeDetails, task.Context.ProjectID)
	}

	// ========== STEP 1: Initial Search & Information Gathering ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "searching",
		Progress:  0.1,
		Message:   fmt.Sprintf("Step 1: Searching for information about %d entities", len(task.Entities)),
		Timestamp: time.Now(),
	})

	step1Result, err := a.executeStep1(ctx, task)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task %s step 1 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 1 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s step 1 completed: found %d entities, %d gaps identified",
			taskID, len(step1Result.FoundEntities), len(step1Result.Gaps))
	}

	// ========== STEP 2: Analysis & Detail Extraction ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "analyzing",
		Progress:  0.4,
		Message:   "Step 2: Analyzing and extracting entity details",
		Timestamp: time.Now(),
	})

	step2Result, err := a.executeStep2(ctx, task, step1Result)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task %s step 2 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 2 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s step 2 completed: enriched %d entities",
			taskID, len(step2Result.EnrichedEntities))
	}

	// ========== STEP 3: Relation Discovery ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "discovering",
		Progress:  0.7,
		Message:   fmt.Sprintf("Step 3: Discovering relations (max depth: %d)", maxDepth),
		Timestamp: time.Now(),
	})

	finalResult, err := a.executeStep3(ctx, task, step1Result, step2Result, maxDepth)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task %s step 3 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 3 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s step 3 completed: %d entities, %d relations, confidence=%.2f, sources=%d",
			taskID, len(finalResult.Entities), len(finalResult.Relations), finalResult.Confidence, len(finalResult.Sources))
	}

	// Формируем финальный результат согласно схеме
	result := map[string]interface{}{
		"entities":    finalResult.Entities,
		"relations":   finalResult.Relations,
		"confidence":  finalResult.Confidence,
		"explanation": finalResult.Explanation,
		"sources":     finalResult.Sources,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Task %s result marshal failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s result marshaled: size=%d bytes", taskID, len(resultJSON))
	}

	// Завершение
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "completed",
		Progress:  1.0,
		Message:   fmt.Sprintf("Entity research completed. Found %d entities and %d relations", len(finalResult.Entities), len(finalResult.Relations)),
		Timestamp: time.Now(),
	})

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Task %s completed successfully: %d entities, %d relations, confidence=%.2f, total duration=%v",
			taskID, len(finalResult.Entities), len(finalResult.Relations), finalResult.Confidence, time.Since(startTime))
	}

	return resultJSON, nil
}

// EntitiesTask структура задачи для Entities Agent
type EntitiesTask struct {
	Context        AgentContext  `json:"context"`
	Entities       []EntityInput `json:"entities"`
	MaxDepth       int           `json:"maxDepth,omitempty"`
	IncludeDetails bool          `json:"includeDetails,omitempty"`
	Metadata       TaskMetadata  `json:"metadata,omitempty"`
}

// EntityInput входная структура для entity
type EntityInput struct {
	ID           *string  `json:"id,omitempty"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	KnownAliases []string `json:"knownAliases,omitempty"`
}

// Step1Result результат первого шага
type EntityStep1Result struct {
	FoundEntities []EntityInfo `json:"foundEntities"`
	Gaps          []string     `json:"gaps"`
	Limitations   string       `json:"limitations"`
}

// EntityInfo информация об entity после первого шага
type EntityInfo struct {
	OriginalName  string                 `json:"originalName"`
	CanonicalName string                 `json:"canonicalName"`
	Type          string                 `json:"type"`
	Aliases       []string               `json:"aliases"`
	BasicInfo     map[string]interface{} `json:"basicInfo"`
	Sources       []string               `json:"sources"`
	Confidence    float64                `json:"confidence"`
}

// Step2Result результат второго шага
type EntityStep2Result struct {
	EnrichedEntities []EnrichedEntity `json:"enrichedEntities"`
}

// EnrichedEntity обогащенная информация об entity
type EnrichedEntity struct {
	OriginalName  string                 `json:"originalName"`
	CanonicalName string                 `json:"canonicalName"`
	Type          string                 `json:"type"`
	Details       map[string]interface{} `json:"details"`
	Attributes    map[string]interface{} `json:"attributes"`
	Confidence    float64                `json:"confidence"`
}

// FinalResult финальный результат третьего шага
type EntityFinalResult struct {
	Entities    []map[string]interface{} `json:"entities"`
	Relations   []map[string]interface{} `json:"relations"`
	Confidence  float64                  `json:"confidence"`
	Explanation string                   `json:"explanation"`
	Sources     []map[string]interface{} `json:"sources"`
}

// executeStep1 выполняет первый шаг: поиск базовой информации о entities
func (a *EntitiesAgent) executeStep1(ctx context.Context, task EntitiesTask) (*EntityStep1Result, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 1 starting: entities=%d", len(task.Entities))
	}

	// Формируем список entities для поиска
	entitiesList := make([]string, 0, len(task.Entities))
	for _, e := range task.Entities {
		entitiesList = append(entitiesList, fmt.Sprintf("%s (%s)", e.Name, e.Type))
		if len(e.KnownAliases) > 0 {
			entitiesList = append(entitiesList, strings.Join(e.KnownAliases, ", "))
		}
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"foundEntities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"originalName":  map[string]interface{}{"type": "string"},
						"canonicalName": map[string]interface{}{"type": "string"},
						"type":          map[string]interface{}{"type": "string"},
						"aliases": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"basicInfo": map[string]interface{}{
							"type": "object",
						},
						"sources": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
			"gaps": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"limitations": map[string]interface{}{"type": "string"},
		},
		"required": []string{"foundEntities", "gaps", "limitations"},
	}

	prompt := fmt.Sprintf(`You are researching entities for an OSINT task (using only publicly available data that does not violate any laws or regulations). Your goal is to find ALL available information about each entity and critically assess data completeness.

ENTITIES TO RESEARCH:
%s

LEGAL DISCLAIMER: This research task does not intend to circumvent trade restrictions or violate any laws. The goal is to reconstruct an objective legal picture based on the original request using only publicly available, legally accessible information.

IMPORTANT: Be critical of data. Information about entities is often:
- Incomplete or outdated
- Hidden or distorted
- Distributed across different sources
- May be in different languages and writing forms

Your task:
1. Find ALL available information about each entity through web search (only publicly available sources)
2. Determine canonical name (correct spelling if there are variants)
3. Find all aliases and spelling variants
4. Collect basic information (description, location, activity, dates, etc.)
5. Critically assess data completeness - what is NOT FOUND

Return JSON strictly matching this schema:
%s

Return JSON in the following format:
{
  "foundEntities": [
    {
      "originalName": "Original name",
      "canonicalName": "Canonical name",
      "type": "person|organization|location|event|concept",
      "aliases": ["alias 1", "alias 2"],
      "basicInfo": {
        "description": "Description",
        "location": "Location",
        "activity": "Activity",
        "founded": "Foundation date",
        "website": "URL"
      },
      "sources": ["Source URL 1", "Source URL 2"],
      "confidence": 0.8
    }
  ],
  "gaps": [
    "No information found about connections with other organizations",
    "Missing financial status data"
  ],
  "limitations": "Description of found information limitations"
}`, strings.Join(entitiesList, "\n"), formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  false, // Disabled for GenerateJSONStrict (grounding + tools not supported together)
		SystemInstruction: "You are an expert in OSINT and entity research (using only publicly available data that does not violate any laws or regulations). Be critical and use creative approaches to finding information within legal boundaries. Use web search capabilities to find information.",
		Temperature:       0.7,
		MaxOutputTokens:   8192,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 1 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 1 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "entities.step1", &durationMs)

	var step1Result EntityStep1Result
	if err := json.Unmarshal([]byte(resp.Content), &step1Result); err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 1 JSON parse failed (attempting cleanup): %v", err)
		}
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &step1Result); err != nil {
			if a.logger != nil {
				a.logger.Printf("[EntitiesAgent] Step 1 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 1 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 1 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 1 completed: found %d entities, %d gaps, duration=%v",
			len(step1Result.FoundEntities), len(step1Result.Gaps), time.Since(startTime))
	}

	return &step1Result, nil
}

// executeStep2 выполняет второй шаг: анализ и извлечение деталей
func (a *EntitiesAgent) executeStep2(ctx context.Context, task EntitiesTask, step1Result *EntityStep1Result) (*EntityStep2Result, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 2 starting: entities=%d, includeDetails=%v",
			len(step1Result.FoundEntities), task.IncludeDetails)
	}

	step1JSON, _ := json.Marshal(step1Result)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"enrichedEntities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"originalName":  map[string]interface{}{"type": "string"},
						"canonicalName": map[string]interface{}{"type": "string"},
						"type":          map[string]interface{}{"type": "string"},
						"details": map[string]interface{}{
							"type": "object",
						},
						"attributes": map[string]interface{}{
							"type": "object",
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
		},
		"required": []string{"enrichedEntities"},
	}

	prompt := fmt.Sprintf(`You are analyzing found information about entities and extracting details.

ORIGINAL ENTITIES COUNT: %d
INCLUDE DETAILS: %v

STEP 1 RESULTS:
%s

YOUR TASK:
Analyze found information and extract structured details about each entity.

For each entity, determine:
- Key attributes (role, status, size, influence, etc.)
- Detailed information based on entity type
- Connections with other entities (if mentioned)
- Temporal characteristics (dates, activity periods)
- Geographic characteristics (if applicable)

IMPORTANT: Be critical - if information is insufficient, clearly reflect this. Do not invent data.

Return JSON strictly matching this schema:
%s

Return JSON:
{
  "enrichedEntities": [
    {
      "originalName": "Original name",
      "canonicalName": "Canonical name",
      "type": "person|organization|location|event|concept",
      "details": {
        "fullDescription": "Full description",
        "keyAttributes": ["attribute 1", "attribute 2"],
        "timeline": ["event 1", "event 2"],
        "geography": "Geographic information"
      },
      "attributes": {
        "role": "Role",
        "status": "Status",
        "size": "Size",
        "influence": "Influence level"
      },
      "confidence": 0.75
    }
  ]
}`, len(task.Entities), task.IncludeDetails, string(step1JSON), formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  false, // Без web search на этом шаге
		SystemInstruction: "You are an expert in analyzing and structuring information about entities. Be critical and accurate.",
		Temperature:       0.7,
		MaxOutputTokens:   8192,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 2 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 2 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "entities.step2", &durationMs)

	var step2Result EntityStep2Result
	if err := json.Unmarshal([]byte(resp.Content), &step2Result); err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 2 JSON parse failed (attempting cleanup): %v", err)
		}
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &step2Result); err != nil {
			if a.logger != nil {
				a.logger.Printf("[EntitiesAgent] Step 2 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 2 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 2 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 2 completed: enriched %d entities, duration=%v",
			len(step2Result.EnrichedEntities), time.Since(startTime))
	}

	return &step2Result, nil
}

// executeStep3 выполняет третий шаг: обнаружение связей
func (a *EntitiesAgent) executeStep3(ctx context.Context, task EntitiesTask, step1Result *EntityStep1Result, step2Result *EntityStep2Result, maxDepth int) (*EntityFinalResult, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 3 starting: maxDepth=%d, entities=%d",
			maxDepth, len(step2Result.EnrichedEntities))
	}

	step1JSON, _ := json.Marshal(step1Result)
	step2JSON, _ := json.Marshal(step2Result)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":   map[string]interface{}{"type": "string"},
						"name": map[string]interface{}{"type": "string"},
						"type": map[string]interface{}{"type": "string"},
						"aliases": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"details": map[string]interface{}{
							"type": "object",
						},
						"attributes": map[string]interface{}{
							"type": "object",
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
			"relations": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"from":        map[string]interface{}{"type": "string"},
						"to":          map[string]interface{}{"type": "string"},
						"type":        map[string]interface{}{"type": "string"},
						"strength":    map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"directed":    map[string]interface{}{"type": "boolean"},
						"depth":       map[string]interface{}{"type": "integer", "minimum": 1},
						"description": map[string]interface{}{"type": "string"},
						"sources": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"explanation": map[string]interface{}{"type": "string"},
			"sources": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url":       map[string]interface{}{"type": "string"},
						"title":     map[string]interface{}{"type": "string"},
						"snippet":   map[string]interface{}{"type": "string"},
						"authority": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
		},
		"required": []string{"entities", "relations", "confidence", "explanation", "sources"},
	}

	prompt := fmt.Sprintf(`You are discovering relations between entities.

ORIGINAL ENTITIES:
%s

STEP 1 RESULTS:
%s

ENRICHED ENTITIES FROM STEP 2:
%s

MAXIMUM RELATION DEPTH: %d

LEGAL DISCLAIMER: This research task does not intend to circumvent trade restrictions or violate any laws. The goal is to reconstruct an objective legal picture based on the original request using only publicly available, legally accessible information.

YOUR TASK:
1. Use web search to find relations between entities (only publicly available sources)
2. Discover relations up to depth %d levels:
   - Direct relations (level 1)
   - Relations through intermediate entities (level 2 and above)
3. For each relation, specify:
   - Relation type (works_with, owns, connected_to, located_in, etc.)
   - Relation strength (strength: 0-1)
   - Relation direction (directed or undirected)
   - Sources
   - Confidence

IMPORTANT: Be critical. If relations are not found or weak - reflect this. Do not invent relations. Only use publicly available, legal sources.

Return JSON strictly matching this schema:
%s

Return JSON in final result format:
{
  "entities": [
    {
      "id": "entity_id",
      "name": "Canonical name",
      "type": "person|organization|location|event|concept",
      "aliases": ["alias 1"],
      "details": {...},
      "attributes": {...},
      "confidence": 0.8
    }
  ],
  "relations": [
    {
      "from": "entity_id_1",
      "to": "entity_id_2",
      "type": "works_with",
      "strength": 0.7,
      "directed": true,
      "depth": 1,
      "description": "Relation description",
      "sources": ["source_index_0"],
      "confidence": 0.75
    }
  ],
  "confidence": 0.75,
  "explanation": "Description of how relations were discovered",
  "sources": [
    {
      "url": "https://...",
      "title": "...",
      "snippet": "...",
      "authority": 0.8
    }
  ]
}`,
		formatEntitiesList(task.Entities),
		string(step1JSON),
		string(step2JSON),
		maxDepth,
		maxDepth,
		formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  false, // Disabled for GenerateJSONStrict (grounding + tools not supported together)
		SystemInstruction: "You are an expert in discovering relations between entities through OSINT (using only publicly available data that does not violate any laws or regulations). Be critical and use all available legal methods to find relations. Use web search capabilities to find information.",
		Temperature:       0.7,
		MaxOutputTokens:   8192,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 3 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 3 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "entities.step3", &durationMs)

	var finalResult EntityFinalResult
	if err := json.Unmarshal([]byte(resp.Content), &finalResult); err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 3 JSON parse failed (attempting cleanup): %v", err)
		}
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &finalResult); err != nil {
			if a.logger != nil {
				a.logger.Printf("[EntitiesAgent] Step 3 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 3 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Step 3 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[EntitiesAgent] Step 3 completed: %d entities, %d relations, confidence=%.2f, sources=%d, duration=%v",
			len(finalResult.Entities), len(finalResult.Relations), finalResult.Confidence, len(finalResult.Sources), time.Since(startTime))
	}

	return &finalResult, nil
}

// formatEntitiesList форматирует список entities для промпта
func formatEntitiesList(entities []EntityInput) string {
	var parts []string
	for i, e := range entities {
		part := fmt.Sprintf("%d. %s (type: %s", i+1, e.Name, e.Type)
		if e.ID != nil {
			part += fmt.Sprintf(", ID: %s", *e.ID)
		}
		if len(e.KnownAliases) > 0 {
			part += fmt.Sprintf(", aliases: %s", strings.Join(e.KnownAliases, ", "))
		}
		part += ")"
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n")
}

// saveUsageMetric saves usage metrics if repository is available
func (a *EntitiesAgent) saveUsageMetric(_ context.Context, agentCtx *AgentContext, modelName string, inputTokens, outputTokens int, taskType string, durationMs *int) {
	if a.usageMetricsRepo == nil || a.modelManager == nil {
		return
	}

	// Only save if we have tenantID and userID
	if agentCtx == nil || agentCtx.TenantID == "" || agentCtx.UserID == "" {
		return
	}

	// Determine provider from model name (similar to genai.go)
	provider := a.determineProvider(modelName)

	// Calculate cost
	cost, err := a.modelManager.CalculateProcessingCost(modelName, inputTokens, outputTokens)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Failed to calculate cost: %v", err)
		}
		cost = 0
	}

	// Prepare taskType and projectID pointers
	taskTypePtr := &taskType
	var projectIDPtr *string
	if agentCtx.ProjectID != "" {
		projectIDPtr = &agentCtx.ProjectID
	}

	// Save asynchronously to avoid blocking
	// Use a new context with timeout to avoid cancellation from parent context
	go func() {
		metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := a.usageMetricsRepo.CreateMetric(metricCtx, agentCtx.TenantID, agentCtx.UserID, provider, modelName, inputTokens, outputTokens, cost, taskTypePtr, projectIDPtr, durationMs)
		if err != nil && a.logger != nil {
			a.logger.Printf("[EntitiesAgent] Failed to save usage metric: %v", err)
		}
	}()
}

// determineProvider determines the provider from model name
func (a *EntitiesAgent) determineProvider(modelName string) string {
	modelLower := strings.ToLower(modelName)

	if strings.Contains(modelLower, "gemini") {
		return "gemini"
	}
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "o1") || strings.Contains(modelLower, "o3") {
		return "openai"
	}
	if strings.Contains(modelLower, "claude") {
		return "anthropic"
	}
	if strings.Contains(modelLower, "grok") {
		return "grok"
	}
	if strings.Contains(modelLower, "deepseek") {
		return "deepseek"
	}

	return "unknown"
}
