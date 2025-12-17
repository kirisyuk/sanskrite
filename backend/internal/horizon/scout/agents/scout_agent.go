package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ScoutAgent извлекает структурированные данные из сырого контента
type ScoutAgent struct {
	executor *ai.AITaskExecutor
}

// NewScoutAgent создает новый Scout Agent
func NewScoutAgent(executor *ai.AITaskExecutor) *ScoutAgent {
	return &ScoutAgent{
		executor: executor,
	}
}

// ExtractFragmentData извлекает данные HorizonFragment из контента источника
func (a *ScoutAgent) ExtractFragmentData(ctx context.Context, content string, targetCanonicalPath string, entityPath string, sectorPath string) (*models.HorizonFragment, error) {
	prompt := a.buildExtractionPrompt(content, targetCanonicalPath, entityPath, sectorPath)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         8000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to extract fragment data: %w", err)
	}

	// Преобразуем результат в HorizonFragment
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var fragment models.HorizonFragment
	if err := json.Unmarshal(jsonData, &fragment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fragment: %w", err)
	}

	return &fragment, nil
}

// buildExtractionPrompt строит промпт для извлечения данных
func (a *ScoutAgent) buildExtractionPrompt(content string, targetCanonicalPath string, entityPath string, sectorPath string) string {
	return fmt.Sprintf(`You are a data extraction agent (Scout) for a risk analysis system. Extract structured information about an entity from the provided content.

TARGET ENTITY:
- Canonical Path: %s
- Entity Type Path: %s
- Sector Path: %s

CONTENT TO ANALYZE:
%s

TASK: Extract all relevant information about this entity and return it as a JSON object matching the HorizonFragment schema.

REQUIRED FIELDS:
- canonicalPath: Must match the target canonical path
- entityPath: Must match the provided entity path
- sectorPath: Must match the provided sector path
- state: Map of metrics (operational_health, capacity_utilization, etc.) with MetricValue objects
- description: Brief description of the entity
- homeLocation: Location object with ISO standards if available
- identifiers: Map of identifiers (ISIN, LEI, Ticker, etc.) if available
- kbRefs: Array of KB references (Wikidata, DBpedia) if available

METRICS:
- All metrics must be in range [0.0..1.0] for Magnitude Metrics or [-1.0..1.0] for Directional Metrics
- Each metric must include: value, confidence (0..1), observedAt (ISO 8601), provenance

Return ONLY valid JSON matching the schema. Do not include markdown code fences.`, targetCanonicalPath, entityPath, sectorPath, content)
}

