package scout

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EvidenceExtractionResult результат извлечения Evidence через LLM
type EvidenceExtractionResult struct {
	Quote            string  `json:"quote"`
	ReliabilityScore float64 `json:"reliabilityScore"`
	Reasoning        string  `json:"reasoning,omitempty"` // Обоснование оценки надежности
}

// EvidenceExtractorLLM извлекает Evidence через LLM
type EvidenceExtractorLLM struct {
	executor *ai.AITaskExecutor
}

// NewEvidenceExtractorLLM создает новый экстрактор Evidence через LLM
func NewEvidenceExtractorLLM(executor *ai.AITaskExecutor) *EvidenceExtractorLLM {
	return &EvidenceExtractorLLM{
		executor: executor,
	}
}

// ExtractEvidenceForSource извлекает Evidence для одного источника через LLM
func (e *EvidenceExtractorLLM) ExtractEvidenceForSource(ctx context.Context, content string, sourceURL string, sourceType string, extractedState map[string]models.MetricValue) (*models.Evidence, error) {
	if content == "" || sourceURL == "" {
		return nil, fmt.Errorf("content and sourceURL are required")
	}

	// Извлекаем релевантную цитату через LLM
	quote, err := e.extractRelevantQuote(ctx, content, extractedState)
	if err != nil {
		return nil, fmt.Errorf("failed to extract quote: %w", err)
	}

	// Оцениваем надежность источника через LLM
	reliabilityScore, err := e.calculateReliabilityScore(ctx, sourceURL, sourceType, content)
	if err != nil {
		// Fallback на среднее значение при ошибке
		reliabilityScore = 0.75
	}

	// Вычисляем хеш контента
	hash := calculateContentHash(content)

	return &models.Evidence{
		SourceURL:        sourceURL,
		SourceHash:       hash,
		ReliabilityScore: reliabilityScore,
		Quote:            quote,
	}, nil
}

// extractRelevantQuote извлекает релевантную цитату через LLM
func (e *EvidenceExtractorLLM) extractRelevantQuote(ctx context.Context, content string, extractedState map[string]models.MetricValue) (string, error) {
	// Формируем список метрик для контекста
	metricNames := make([]string, 0, len(extractedState))
	for metricName := range extractedState {
		metricNames = append(metricNames, metricName)
	}

	prompt := e.buildQuoteExtractionPrompt(content, metricNames)

	// Создаем типизированный верификатор
	verifier := ai.NewGenericVerifier(func(result struct {
		Quote string `json:"quote"`
	}) error {
		if result.Quote == "" {
			return fmt.Errorf("quote is required")
		}
		if len(result.Quote) > 1000 {
			return fmt.Errorf("quote is too long (max 1000 characters)")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5, // Регулярная задача: gpt-5-nano обычно достаточно
			MinContextWindow:         8000,
			MaxOutputSize:            512,
		},
		Verifier: verifier,
	}

	result, err := e.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", fmt.Errorf("failed to extract quote: %w", err)
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	var extractionResult struct {
		Quote string `json:"quote"`
	}
	if err := json.Unmarshal(jsonData, &extractionResult); err != nil {
		return "", fmt.Errorf("failed to unmarshal extraction result: %w", err)
	}

	// Обрезаем до 500 символов если нужно
	quote := extractionResult.Quote
	if len(quote) > 500 {
		quote = quote[:497] + "..."
	}

	return quote, nil
}

// buildQuoteExtractionPrompt строит промпт для извлечения релевантной цитаты
func (e *EvidenceExtractorLLM) buildQuoteExtractionPrompt(content string, metricNames []string) string {
	metricsContext := ""
	if len(metricNames) > 0 {
		metricsContext = fmt.Sprintf("\nMETRICS TO FIND EVIDENCE FOR:\n%s", strings.Join(metricNames, ", "))
	} else {
		metricsContext = "\nExtract the most relevant and informative quote from the content."
	}

	return fmt.Sprintf(`You are an evidence extraction agent for a risk analysis system. Your task is to extract the most relevant quote from source content that supports the extracted metrics.

SOURCE CONTENT:
%s%s

TASK: Extract the most relevant quote (1-3 sentences, max 500 characters) from the source content that provides evidence for the metrics mentioned above.

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "quote": "The extracted quote from the source content"
}

RULES:
1. The quote must be directly from the source content (exact text or close paraphrase)
2. The quote should be relevant to the metrics being extracted
3. The quote should be informative and provide context
4. Maximum length: 500 characters
5. If no relevant quote is found, extract the most informative general quote from the content
6. Return ONLY valid JSON, no markdown code fences or additional text

Return the quote as JSON:`, content, metricsContext)
}

// calculateReliabilityScore вычисляет надежность источника через LLM
func (e *EvidenceExtractorLLM) calculateReliabilityScore(ctx context.Context, sourceURL string, sourceType string, content string) (float64, error) {
	prompt := e.buildReliabilityPrompt(sourceURL, sourceType, content)

	// Создаем типизированный верификатор
	verifier := ai.NewGenericVerifier(func(result struct {
		ReliabilityScore float64 `json:"reliabilityScore"`
		Reasoning        string  `json:"reasoning,omitempty"`
	}) error {
		if result.ReliabilityScore < 0.0 || result.ReliabilityScore > 1.0 {
			return fmt.Errorf("reliabilityScore must be between 0.0 and 1.0")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5, // Регулярная задача: gpt-5-nano обычно достаточно
			MinContextWindow:         4000,
			MaxOutputSize:            256,
		},
		Verifier: verifier,
	}

	result, err := e.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return 0.75, fmt.Errorf("failed to calculate reliability score: %w", err) // Fallback на среднее значение
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return 0.75, fmt.Errorf("failed to marshal result: %w", err)
	}

	var reliabilityResult struct {
		ReliabilityScore float64 `json:"reliabilityScore"`
		Reasoning        string  `json:"reasoning,omitempty"`
	}
	if err := json.Unmarshal(jsonData, &reliabilityResult); err != nil {
		return 0.75, fmt.Errorf("failed to unmarshal reliability result: %w", err)
	}

	return reliabilityResult.ReliabilityScore, nil
}

// buildReliabilityPrompt строит промпт для оценки надежности источника
func (e *EvidenceExtractorLLM) buildReliabilityPrompt(sourceURL string, sourceType string, content string) string {
	// Берем первые 500 символов контента для контекста
	contentPreview := content
	if len(contentPreview) > 500 {
		contentPreview = contentPreview[:500] + "..."
	}

	return fmt.Sprintf(`You are a source reliability assessment agent for a risk analysis system. Your task is to assess the reliability of a data source based on its URL, type, and content.

SOURCE INFORMATION:
- URL: %s
- Type: %s
- Content Preview: %s

TASK: Assess the reliability of this source on a scale from 0.0 to 1.0, where:
- 0.9-1.0: Highly reliable (official government sources, academic institutions, established news organizations)
- 0.7-0.9: Reliable (reputable news sources, industry publications, verified organizations)
- 0.5-0.7: Moderately reliable (general news sources, blogs from recognized experts)
- 0.3-0.5: Low reliability (social media, unverified blogs, opinion pieces)
- 0.0-0.3: Very low reliability (unverified sources, suspicious domains)

Consider:
1. Domain reputation (.gov, .edu, established news domains)
2. Source type (official API, news article, social media, blog)
3. Content quality and professionalism
4. Known reputable sources (Reuters, Bloomberg, WSJ, FT, Economist, etc.)

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "reliabilityScore": 0.85,
  "reasoning": "Brief explanation of the assessment"
}

RULES:
1. reliabilityScore must be a number between 0.0 and 1.0
2. Provide brief reasoning for the score
3. Return ONLY valid JSON, no markdown code fences or additional text

Return the reliability assessment as JSON:`, sourceURL, sourceType, contentPreview)
}

// calculateContentHash вычисляет SHA256 хеш контента
func calculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("sha256:%x", hash)
}

