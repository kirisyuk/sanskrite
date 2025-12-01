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

// DataAgent агент для получения данных (например, метрики, статистика)
type DataAgent struct {
	id               string
	name             string
	displayName      string
	description      string
	genaiClient      *gen_combined.Client
	logger           *log.Logger
	usageMetricsRepo interface {
		CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int, useCase *string) (string, error)
	}
	modelManager interface {
		CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error)
	}
}

// NewDataAgent создает новый Data Agent
func NewDataAgent(genaiClient *gen_combined.Client, logger *log.Logger) *DataAgent {
	return &DataAgent{
		id:          "data",
		name:        "Data Collection Agent",
		displayName: "Data Collection",
		description: "Collects and aggregates data from various sources with metadata",
		genaiClient: genaiClient,
		logger:      logger,
	}
}

// WithUsageMetrics sets the usage metrics repository and model manager
func (a *DataAgent) WithUsageMetrics(repo interface {
	CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int, useCase *string) (string, error)
}, modelMgr interface {
	CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error)
}) *DataAgent {
	a.usageMetricsRepo = repo
	a.modelManager = modelMgr
	return a
}

func (a *DataAgent) ID() string {
	return a.id
}

func (a *DataAgent) Name() string {
	return a.name
}

func (a *DataAgent) DisplayName() string {
	return a.displayName
}

func (a *DataAgent) Description() string {
	return a.description
}

func (a *DataAgent) Skills() []string {
	return []string{"data_collection", "osint", "data_aggregation", "approximation"}
}

func (a *DataAgent) Type() string {
	return "internal"
}

func (a *DataAgent) IsExternal() bool {
	return false
}

func (a *DataAgent) Health(ctx context.Context) (*AgentHealth, error) {
	return &AgentHealth{
		Status:    "healthy",
		Message:   "DataAgent is operational",
		LastCheck: time.Now(),
	}, nil
}

func (a *DataAgent) TaskSchema() map[string]interface{} {
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
			"query": map[string]interface{}{
				"type": "string",
			},
			"granularity": map[string]interface{}{
				"type":    "string",
				"enum":    []string{"monthly", "quarterly", "yearly", "annual"},
				"default": "monthly",
			},
			"timeRange": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from": map[string]interface{}{"type": "string"},
					"to":   map[string]interface{}{"type": "string"},
				},
				"required": []string{"from", "to"},
			},
			"region": map[string]interface{}{
				"type": "string",
			},
			"category": map[string]interface{}{
				"type": "string",
			},
			"metadata": map[string]interface{}{
				"type": "object",
			},
		},
		"required": []string{"context", "query", "timeRange"},
	}
}

func (a *DataAgent) ResultSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dataPoints": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "object"},
			},
			"metadata": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sources":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
					"methods":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
					"usedData":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
					"assumptions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
				},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"explanation": map[string]interface{}{"type": "string"},
		},
		"required": []string{"dataPoints", "metadata", "confidence", "explanation"},
	}
}

func (a *DataAgent) Execute(ctx context.Context, taskJSON []byte) ([]byte, error) {
	return a.ExecuteWithStream(ctx, taskJSON, func(Update) {})
}

func (a *DataAgent) ExecuteWithStream(
	ctx context.Context,
	taskJSON []byte,
	onUpdate func(Update),
) ([]byte, error) {
	startTime := time.Now()
	var task DataTask

	if err := json.Unmarshal(taskJSON, &task); err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task unmarshal failed: %v", err)
		}
		return nil, fmt.Errorf("invalid task schema: %w", err)
	}

	// Валидация projectId
	if task.Context.ProjectID == "" {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task validation failed: projectId is required")
		}
		return nil, fmt.Errorf("projectId is required")
	}

	taskID := generateTaskID()
	granularity := task.Granularity
	if granularity == "" {
		granularity = "monthly"
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s started: query=%q, region=%v, category=%v, period=%s-%s, granularity=%s, projectId=%s",
			taskID, task.Query, task.Region, task.Category, task.TimeRange.From, task.TimeRange.To, granularity, task.Context.ProjectID)
	}

	// ========== STEP 1: Initial Search & Gap Analysis ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "searching",
		Progress:  0.1,
		Message:   fmt.Sprintf("Step 1: Searching for data: %s", task.Query),
		Timestamp: time.Now(),
	})

	step1Result, err := a.executeStep1(ctx, task)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task %s step 1 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 1 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s step 1 completed: found %d data points, %d gaps identified",
			taskID, len(step1Result.FoundData), len(step1Result.Gaps))
	}

	// ========== STEP 2: Method Planning ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "analyzing",
		Progress:  0.4,
		Message:   "Step 2: Planning data collection methods",
		Timestamp: time.Now(),
	})

	step2Result, err := a.executeStep2(ctx, task, step1Result)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task %s step 2 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 2 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s step 2 completed: %d methods planned, %d assumptions identified",
			taskID, len(step2Result.Methods), len(step2Result.Assumptions))
	}

	// ========== STEP 3: Data Collection & Structuring ==========
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "collecting",
		Progress:  0.7,
		Message:   "Step 3: Collecting data using planned methods",
		Timestamp: time.Now(),
	})

	finalResult, err := a.executeStep3(ctx, task, step1Result, step2Result, granularity)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task %s step 3 failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("step 3 failed: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s step 3 completed: %d data points collected, confidence=%.2f, sources=%d",
			taskID, len(finalResult.DataPoints), finalResult.Confidence, len(finalResult.Metadata.Sources))
	}

	// Формируем финальный результат согласно схеме
	result := map[string]interface{}{
		"dataPoints": finalResult.DataPoints,
		"metadata": map[string]interface{}{
			"sources":     finalResult.Metadata.Sources,
			"methods":     finalResult.Metadata.Methods,
			"usedData":    finalResult.Metadata.UsedData,
			"assumptions": finalResult.Metadata.Assumptions,
		},
		"confidence":  finalResult.Confidence,
		"explanation": finalResult.Explanation,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Task %s result marshal failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s result marshaled: size=%d bytes", taskID, len(resultJSON))
	}

	// Завершение
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.id,
		AgentName: a.displayName,
		Stage:     "completed",
		Progress:  1.0,
		Message:   fmt.Sprintf("Data collection completed. Found %d data points", len(finalResult.DataPoints)),
		Timestamp: time.Now(),
	})

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Task %s completed successfully: %d data points, confidence=%.2f, total duration=%v",
			taskID, len(finalResult.DataPoints), finalResult.Confidence, time.Since(startTime))
	}

	return resultJSON, nil
}

// DataTask структура задачи для Data Agent
type DataTask struct {
	Context     AgentContext `json:"context"`
	Query       string       `json:"query"`
	Granularity string       `json:"granularity,omitempty"`
	TimeRange   struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"timeRange"`
	Region   *string      `json:"region,omitempty"`
	Category *string      `json:"category,omitempty"`
	Metadata TaskMetadata `json:"metadata,omitempty"`
}

// Step1Result результат первого шага
type Step1Result struct {
	FoundData   []interface{} `json:"foundData"`
	Gaps        []string      `json:"gaps"`
	Limitations string        `json:"limitations"`
	NextTask    struct {
		Query   string   `json:"query"`
		Methods []string `json:"methods"`
		Hints   string   `json:"hints"`
	} `json:"nextTask"`
}

// Step2Result результат второго шага
type Step2Result struct {
	Methods []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		DataTypes   []string `json:"dataTypes"`
		Query       string   `json:"query"`
	} `json:"methods"`
	Assumptions []struct {
		Statement string `json:"statement"`
		Impact    string `json:"impact"`
		Rationale string `json:"rationale"`
	} `json:"assumptions"`
	ExtractionStrategy string `json:"extractionStrategy"`
}

// FinalResult финальный результат третьего шага
type FinalResult struct {
	DataPoints []map[string]interface{} `json:"dataPoints"`
	Metadata   struct {
		Sources     []map[string]interface{} `json:"sources"`
		Methods     []map[string]interface{} `json:"methods"`
		UsedData    []map[string]interface{} `json:"usedData"`
		Assumptions []map[string]interface{} `json:"assumptions"`
	} `json:"metadata"`
	Confidence  float64 `json:"confidence"`
	Explanation string  `json:"explanation"`
}

// executeStep1 выполняет первый шаг: поиск данных и анализ ограничений
func (a *DataAgent) executeStep1(ctx context.Context, task DataTask) (*Step1Result, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 1 starting: query=%q, region=%v, category=%v, period=%s-%s",
			task.Query, task.Region, task.Category, task.TimeRange.From, task.TimeRange.To)
	}

	// Формируем запрос
	regionStr := ""
	if task.Region != nil {
		regionStr = *task.Region
	}
	categoryStr := ""
	if task.Category != nil {
		categoryStr = *task.Category
	}

	// JSON схема для Step 1
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"foundData": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"period":     map[string]interface{}{"type": "string"},
						"value":      map[string]interface{}{"type": "number"},
						"unit":       map[string]interface{}{"type": "string"},
						"source":     map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"note":       map[string]interface{}{"type": "string"},
					},
				},
			},
			"gaps": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"limitations": map[string]interface{}{"type": "string"},
			"nextTask": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"methods": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"hints": map[string]interface{}{"type": "string"},
				},
			},
		},
		"required": []string{"foundData", "gaps", "limitations", "nextTask"},
	}

	prompt := fmt.Sprintf(`You are researching data for an OSINT task (using only publicly available data that does not violate any laws or regulations). Your goal is to find ALL available data and critically assess its limitations.

DATA REQUEST:
%s

REGION: %s
CATEGORY: %s
PERIOD: %s - %s

LEGAL DISCLAIMER: This research task does not intend to circumvent trade restrictions or violate any laws. The goal is to reconstruct an objective legal picture based on the original request using only publicly available, legally accessible information.

IMPORTANT: Be critical of data. Official sources are often incomplete, outdated, or conceal reality. 
Direct data may be unavailable due to:
- Trade secrets
- Political restrictions
- Lack of public statistics
- Delays in data publication
- Intentional information concealment

Your task:
1. Find ALL available data for the request through web search (only publicly available sources)
2. Critically assess its completeness and reliability
3. Clearly identify WHAT IS NOT FOUND - what data is missing, which periods are empty
4. Propose a strategy for finding missing data through indirect methods (using only legal, publicly available information)

Return JSON strictly matching this schema:
%s

Return JSON in the following format:
{
  "foundData": [
    {
      "period": "2025-01",
      "value": 23.5,
      "unit": "%%",
      "source": "Source URL",
      "confidence": 0.8,
      "note": "Data description"
    }
  ],
  "gaps": [
    "Missing data for period 2025-02",
    "No data for low-alcohol beverages category"
  ],
  "limitations": "Description of found data limitations",
  "nextTask": {
    "query": "What to search for in the next step",
    "methods": ["proxy_indicator", "extrapolation"],
    "hints": "Hints for the next step"
  }
}`, task.Query, regionStr, categoryStr, task.TimeRange.From, task.TimeRange.To, formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  false, // Disabled for GenerateJSONStrict (grounding + tools not supported together)
		SystemInstruction: "You are an expert in OSINT and data analysis (using only publicly available data that does not violate any laws or regulations). Be critical and use creative approaches to finding information within legal boundaries. Use web search capabilities to find information.",
		Temperature:       0.7,
		MaxOutputTokens:   4096,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 1 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 1 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "data.step1", &durationMs)

	// Парсим JSON ответ
	var step1Result Step1Result
	if err := json.Unmarshal([]byte(resp.Content), &step1Result); err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 1 JSON parse failed (attempting cleanup): %v", err)
		}
		// Попробуем извлечь JSON из markdown или текста
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &step1Result); err != nil {
			if a.logger != nil {
				a.logger.Printf("[DataAgent] Step 1 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 1 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 1 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 1 completed: found %d data points, %d gaps, duration=%v",
			len(step1Result.FoundData), len(step1Result.Gaps), time.Since(startTime))
	}

	return &step1Result, nil
}

// executeStep2 выполняет второй шаг: планирование методов
func (a *DataAgent) executeStep2(ctx context.Context, task DataTask, step1Result *Step1Result) (*Step2Result, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 2 starting: gaps=%d, limitations=%v",
			len(step1Result.Gaps), step1Result.Limitations != "")
	}

	step1JSON, _ := json.Marshal(step1Result)

	regionStr := ""
	if task.Region != nil {
		regionStr = *task.Region
	}
	categoryStr := ""
	if task.Category != nil {
		categoryStr = *task.Category
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"methods": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":        map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
						"dataTypes": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"query": map[string]interface{}{"type": "string"},
					},
				},
			},
			"assumptions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statement": map[string]interface{}{"type": "string"},
						"impact":    map[string]interface{}{"type": "string"},
						"rationale": map[string]interface{}{"type": "string"},
					},
				},
			},
			"extractionStrategy": map[string]interface{}{"type": "string"},
		},
		"required": []string{"methods", "assumptions", "extractionStrategy"},
	}

	prompt := fmt.Sprintf(`You are analyzing data gaps and planning methods to fill them.

ORIGINAL REQUEST: %s
REGION: %s
CATEGORY: %s
PERIOD: %s - %s

STEP 1 RESULTS:
%s

YOUR TASK:
Analyze gaps and limitations. Be CREATIVE and inventive.

Official data is rarely complete. Use indirect methods:
- Proxy indicators: if direct data is unavailable, search for related indicators
- Extrapolation: use data from adjacent periods, regions, or categories
- Approximation: calculate values based on related data
- Indirect calculation: use mathematical methods to derive data

For each method, determine:
- What data to search for
- What query to use for search
- What assumptions will be required

Return JSON strictly matching this schema:
%s

Return JSON:
{
  "methods": [
    {
      "name": "proxy_indicator",
      "description": "Method description",
      "dataTypes": ["data type 1", "data type 2"],
      "query": "What to search in web search"
    }
  ],
  "assumptions": [
    {
      "statement": "Assumption statement",
      "impact": "high|medium|low",
      "rationale": "Rationale"
    }
  ],
  "extractionStrategy": "Data extraction strategy"
}`, task.Query, regionStr, categoryStr, task.TimeRange.From, task.TimeRange.To, string(step1JSON), formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality", // Можно использовать gpt_quality
		GroundingEnabled:  false,            // Без web search на этом шаге
		SystemInstruction: "You are an expert in data collection planning. Be inventive and critical of data availability.",
		Temperature:       0.8,
		MaxOutputTokens:   4096,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 2 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 2 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "data.step2", &durationMs)

	var step2Result Step2Result
	if err := json.Unmarshal([]byte(resp.Content), &step2Result); err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 2 JSON parse failed (attempting cleanup): %v", err)
		}
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &step2Result); err != nil {
			if a.logger != nil {
				a.logger.Printf("[DataAgent] Step 2 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 2 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 2 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 2 completed: %d methods, %d assumptions, duration=%v",
			len(step2Result.Methods), len(step2Result.Assumptions), time.Since(startTime))
	}

	return &step2Result, nil
}

// executeStep3 выполняет третий шаг: сбор данных по методам
func (a *DataAgent) executeStep3(ctx context.Context, task DataTask, step1Result *Step1Result, step2Result *Step2Result, granularity string) (*FinalResult, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 3 starting: granularity=%s, methods=%d, assumptions=%d",
			granularity, len(step2Result.Methods), len(step2Result.Assumptions))
	}

	step1JSON, _ := json.Marshal(step1Result)
	step2JSON, _ := json.Marshal(step2Result)

	regionStr := ""
	if task.Region != nil {
		regionStr = *task.Region
	}
	categoryStr := ""
	if task.Category != nil {
		categoryStr = *task.Category
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dataPoints": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"period":     map[string]interface{}{"type": "string"},
						"value":      map[string]interface{}{"type": "number"},
						"unit":       map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"method":     map[string]interface{}{"type": "string"},
						"sources": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
			"metadata": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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
					"methods": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":        map[string]interface{}{"type": "string"},
								"description": map[string]interface{}{"type": "string"},
								"usedFor": map[string]interface{}{
									"type":  "array",
									"items": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
					"usedData": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"type":        map[string]interface{}{"type": "string"},
								"description": map[string]interface{}{"type": "string"},
								"source":      map[string]interface{}{"type": "string"},
								"reason":      map[string]interface{}{"type": "string"},
							},
						},
					},
					"assumptions": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"statement": map[string]interface{}{"type": "string"},
								"impact":    map[string]interface{}{"type": "string"},
								"affects": map[string]interface{}{
									"type":  "array",
									"items": map[string]interface{}{"type": "string"},
								},
								"rationale": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"explanation": map[string]interface{}{"type": "string"},
		},
		"required": []string{"dataPoints", "metadata", "confidence", "explanation"},
	}

	prompt := fmt.Sprintf(`You are collecting data using methods from the previous step.

ORIGINAL REQUEST: %s
REGION: %s
CATEGORY: %s
PERIOD: %s - %s
GRANULARITY: %s

STEP 1 RESULTS:
%s

METHODS FROM STEP 2:
%s

LEGAL DISCLAIMER: This research task does not intend to circumvent trade restrictions or violate any laws. The goal is to reconstruct an objective legal picture based on the original request using only publicly available, legally accessible information.

YOUR TASK:
1. Use web search to find data according to methods from step 2 (only publicly available sources)
2. Be CREATIVE - if direct data is unavailable, use indirect methods, approximations, extrapolations (within legal boundaries)
3. Fill data gaps using methods from step 2
4. For each value, specify:
   - Which method was used
   - Data sources
   - Confidence for the value
   - Assumptions if used

IMPORTANT: Be critical. If data truly doesn't exist - use reasonable approximations based on available information. Only use publicly available, legal sources.

Return JSON strictly matching this schema:
%s

Return JSON in final result format:
{
  "dataPoints": [
    {
      "period": "2025-01",
      "value": 23.5,
      "unit": "%%",
      "confidence": 0.65,
      "method": "indirect_calculation",
      "sources": ["source_index_0", "source_index_1"]
    }
  ],
  "metadata": {
    "sources": [
      {
        "url": "https://...",
        "title": "...",
        "snippet": "...",
        "authority": 0.8
      }
    ],
    "methods": [
      {
        "name": "indirect_calculation",
        "description": "Calculation through confiscation volumes",
        "usedFor": ["2025-01"]
      }
    ],
    "usedData": [
      {
        "type": "proxy",
        "description": "Used confiscation data",
        "source": "source_index_0",
        "reason": "Direct data unavailable"
      }
    ],
    "assumptions": [
      {
        "statement": "Trend is similar to strong alcohol trend",
        "impact": "medium",
        "affects": ["2025-02"],
        "rationale": "Historical data shows correlation"
      }
    ]
  },
  "confidence": 0.65,
  "explanation": "Description of how data was obtained"
}`, task.Query, regionStr, categoryStr, task.TimeRange.From, task.TimeRange.To, granularity, string(step1JSON), string(step2JSON), formatSchemaAsText(schema))

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  false, // Disabled for GenerateJSONStrict (grounding + tools not supported together)
		SystemInstruction: "You are an expert in data collection through OSINT (using only publicly available data that does not violate any laws or regulations). Be critical, inventive, and use all available legal methods to obtain data. Use web search capabilities to find information.",
		Temperature:       0.7,
		MaxOutputTokens:   8192,
	}

	resp, err := a.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 3 genAI call failed: %v", err)
		}
		return nil, fmt.Errorf("genAI call failed: %w", err)
	}

	duration := time.Since(startTime)
	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 3 genAI call completed: tokens=%d/%d (total=%d), model=%s, duration=%v",
			resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, resp.Model, duration)
	}

	// Save usage metrics
	durationMs := int(duration.Milliseconds())
	a.saveUsageMetric(ctx, &task.Context, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens, "data.step3", &durationMs)

	var finalResult FinalResult
	if err := json.Unmarshal([]byte(resp.Content), &finalResult); err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 3 JSON parse failed (attempting cleanup): %v", err)
		}
		cleaned := extractJSON(resp.Content)
		if err := json.Unmarshal([]byte(cleaned), &finalResult); err != nil {
			if a.logger != nil {
				a.logger.Printf("[DataAgent] Step 3 JSON parse failed after cleanup: %v, content length=%d", err, len(resp.Content))
			}
			return nil, fmt.Errorf("failed to parse step 3 result: %w", err)
		}
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Step 3 JSON parse succeeded after cleanup")
		}
	}

	if a.logger != nil {
		a.logger.Printf("[DataAgent] Step 3 completed: %d data points, confidence=%.2f, sources=%d, duration=%v",
			len(finalResult.DataPoints), finalResult.Confidence, len(finalResult.Metadata.Sources), time.Since(startTime))
	}

	return &finalResult, nil
}

// extractJSON теперь в agent.go (общая функция для всех агентов)

// saveUsageMetric saves usage metrics if repository is available
func (a *DataAgent) saveUsageMetric(_ context.Context, agentCtx *AgentContext, modelName string, inputTokens, outputTokens int, taskType string, durationMs *int) {
	if a.usageMetricsRepo == nil || a.modelManager == nil {
		return
	}

	// Only save if we have tenantID and userID
	if agentCtx == nil || agentCtx.TenantID == "" || agentCtx.UserID == "" {
		return
	}

	// Convert model alias to actual model name if needed
	// modelName might contain an alias (e.g., "gemini_fast") instead of actual model name (e.g., "gemini-2.5-flash-lite")
	actualModelName := gen_combined.ConvertAliasToModel(modelName)
	
	// Determine provider from actual model name (similar to genai.go)
	provider := a.determineProvider(actualModelName)

	// Calculate cost using actual model name
	cost, err := a.modelManager.CalculateProcessingCost(actualModelName, inputTokens, outputTokens)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DataAgent] Failed to calculate cost: %v", err)
		}
		cost = 0
	}

	// Prepare taskType and projectID pointers
	taskTypePtr := &taskType
	var projectIDPtr *string
	if agentCtx.ProjectID != "" {
		projectIDPtr = &agentCtx.ProjectID
	}

	// Determine useCase from taskType
	useCase := "document_processing"
	if strings.HasPrefix(taskType, "data.") {
		useCase = "document_processing"
	}
	var useCasePtr *string
	if useCase != "" {
		useCasePtr = &useCase
	}

	// Save asynchronously to avoid blocking
	// Use a new context with timeout to avoid cancellation from parent context
	go func() {
		metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := a.usageMetricsRepo.CreateMetric(metricCtx, agentCtx.TenantID, agentCtx.UserID, provider, actualModelName, inputTokens, outputTokens, cost, taskTypePtr, projectIDPtr, durationMs, useCasePtr)
		if err != nil && a.logger != nil {
			a.logger.Printf("[DataAgent] Failed to save usage metric: %v", err)
		}
	}()
}

// determineProvider determines the provider from model name
func (a *DataAgent) determineProvider(modelName string) string {
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
