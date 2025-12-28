package scout

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ExtractEvidence извлекает Evidence из результатов выполнения источников через LLM
// Создает Evidence для каждого источника с релевантными цитатами из контента
func ExtractEvidence(ctx context.Context, extractor *EvidenceExtractorLLM, results []*ExecuteDataSourceResult, extractedState map[string]models.MetricValue) ([]models.Evidence, error) {
	var evidence []models.Evidence

	for _, result := range results {
		if result.SourceURL == "" {
			continue
		}

		// Вычисляем хеш если его нет
		if result.SourceHash == "" {
			hash := sha256.Sum256([]byte(result.Content))
			result.SourceHash = fmt.Sprintf("sha256:%x", hash)
		}

		// Извлекаем Evidence через LLM
		ev, err := extractor.ExtractEvidenceForSource(ctx, result.Content, result.SourceURL, result.SourceType, extractedState)
		if err != nil {
			// Логируем ошибку, но продолжаем с другими источниками
			continue
		}

		evidence = append(evidence, *ev)
	}

	return evidence, nil
}

// extractMetricKeywords извлекает ключевые слова из названий метрик
func extractMetricKeywords(extractedState map[string]models.MetricValue) []string {
	keywords := make([]string, 0, len(extractedState)*2)

	for metricName := range extractedState {
		// Добавляем название метрики
		keywords = append(keywords, strings.ToLower(metricName))

		// Добавляем слова из названия метрики (разделенные подчеркиванием)
		parts := strings.Split(metricName, "_")
		for _, part := range parts {
			if len(part) > 3 { // Игнорируем короткие слова
				keywords = append(keywords, strings.ToLower(part))
			}
		}
	}

	return keywords
}

// containsKeywords проверяет, содержит ли предложение ключевые слова
func containsKeywords(sentence string, keywords []string) bool {
	lowerSentence := strings.ToLower(sentence)
	for _, keyword := range keywords {
		if strings.Contains(lowerSentence, keyword) {
			return true
		}
	}
	return false
}

// splitIntoSentences разбивает текст на предложения
func splitIntoSentences(text string) []string {
	// Упрощенная реализация: разбиваем по точкам, восклицательным и вопросительным знакам
	text = strings.ReplaceAll(text, "!", ".")
	text = strings.ReplaceAll(text, "?", ".")

	sentences := strings.Split(text, ".")
	var result []string

	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) > 20 { // Игнорируем очень короткие "предложения"
			result = append(result, s)
		}
	}

	return result
}

// extractFirstRelevantSentences извлекает первые релевантные предложения
func extractFirstRelevantSentences(sentences []string, maxLength int) string {
	var result strings.Builder
	currentLength := 0

	for _, sentence := range sentences {
		if currentLength+len(sentence) > maxLength {
			break
		}
		if result.Len() > 0 {
			result.WriteString(" ")
		}
		result.WriteString(sentence)
		currentLength += len(sentence)
	}

	quote := result.String()
	if len(quote) > maxLength {
		quote = quote[:maxLength] + "..."
	}

	return quote
}
