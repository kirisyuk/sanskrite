package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BaseVerifier базовая реализация верификатора с общими методами
type BaseVerifier struct{}

// CleanAndFixJSON очищает и исправляет JSON перед парсингом
func (v *BaseVerifier) CleanAndFixJSON(rawJSON string) (string, error) {
	s := strings.TrimSpace(rawJSON)
	if s == "" {
		return "", fmt.Errorf("JSON string is empty")
	}
	s = strings.TrimPrefix(s, "\uFEFF")
	// Удаляем Markdown code fences ```json ... ``` / ``` ... ```
	if strings.HasPrefix(s, "```") {
		// отбросить первый блок ```...\n
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
		if j := strings.LastIndex(s, "```"); j > 0 {
			s = s[:j]
		}
	}
	// Вырезаем чистую JSON-структуру по скобкам (игнорируя содержимое строк)
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '[' {
			start = i
			break
		}
	}
	if start > 0 {
		s = s[start:]
	}
	// Найти позицию закрывающей скобки, учитывая строки и экранирование
	buf := make([]byte, 0, len(s))
	inStr := false
	esc := false
	depthObj := 0
	depthArr := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		buf = append(buf, ch)
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		if ch == '"' {
			inStr = true
			continue
		}
		switch ch {
		case '{':
			depthObj++
		case '}':
			if depthObj > 0 {
				depthObj--
			}
		case '[':
			depthArr++
		case ']':
			if depthArr > 0 {
				depthArr--
			}
		}
	}
	cleaned := string(buf)
	// Проход для удаления однострочных/многострочных комментариев и хвостовых запятых вне строк
	out := make([]byte, 0, len(cleaned))
	inStr = false
	esc = false
	for i := 0; i < len(cleaned); i++ {
		ch := cleaned[i]
		if inStr {
			out = append(out, ch)
			if esc {
				esc = false
				continue
			}
			switch ch {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		if ch == '"' {
			inStr = true
			out = append(out, ch)
			continue
		}
		// strip // comments
		if ch == '/' && i+1 < len(cleaned) && cleaned[i+1] == '/' {
			// skip until newline
			i += 2
			for i < len(cleaned) && cleaned[i] != '\n' {
				i++
			}
			continue
		}
		// strip /* */ comments
		if ch == '/' && i+1 < len(cleaned) && cleaned[i+1] == '*' {
			i += 2
			for i+1 < len(cleaned) {
				if cleaned[i] == '*' && cleaned[i+1] == '/' {
					i += 1
					break
				}
				i++
			}
			continue
		}
		// remove trailing commas: ,\s*([}\]])
		if ch == ',' {
			j := i + 1
			for j < len(cleaned) && (cleaned[j] == ' ' || cleaned[j] == '\n' || cleaned[j] == '\r' || cleaned[j] == '\t') {
				j++
			}
			if j < len(cleaned) && (cleaned[j] == '}' || cleaned[j] == ']') {
				// skip comma (do not advance i; next loop will copy the brace)
				continue
			}
		}
		out = append(out, ch)
	}
	cleaned = strings.TrimSpace(string(out))
	if !json.Valid([]byte(cleaned)) {
		return "", fmt.Errorf("invalid JSON after cleaning")
	}
	return cleaned, nil
}

// ValidateAndParse проверяет валидность JSON и парсит его
func (v *BaseVerifier) ValidateAndParse(rawJSON string, target interface{}) error {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return fmt.Errorf("failed to clean JSON: %w", err)
	}

	if !json.Valid([]byte(cleaned)) {
		return fmt.Errorf("invalid JSON after cleaning")
	}

	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// GenericVerifier универсальный верификатор для любого типа
type GenericVerifier[T any] struct {
	BaseVerifier
	// ValidateFunc дополнительная функция валидации после парсинга
	ValidateFunc func(T) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *GenericVerifier[T]) VerifyAndParse(rawJSON string) (interface{}, error) {
	return v.VerifyAndParseTyped(rawJSON)
}

// VerifyAndParseTyped реализация типизированного верификатора
func (v *GenericVerifier[T]) VerifyAndParseTyped(rawJSON string) (T, error) {
	var zero T
	var result T

	if err := v.ValidateAndParse(rawJSON, &result); err != nil {
		return zero, err
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return zero, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewGenericVerifier создает новый экземпляр GenericVerifier
func NewGenericVerifier[T any](validateFunc func(T) error) *GenericVerifier[T] {
	return &GenericVerifier[T]{
		ValidateFunc: validateFunc,
	}
}

// SimpleJSONVerifier простой верификатор, который только проверяет валидность JSON
type SimpleJSONVerifier struct {
	BaseVerifier
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *SimpleJSONVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return result, nil
}

// NewSimpleJSONVerifier создает новый экземпляр SimpleJSONVerifier
func NewSimpleJSONVerifier() *SimpleJSONVerifier {
	return &SimpleJSONVerifier{}
}

// MapVerifier верификатор для map[string]interface{}
type MapVerifier struct {
	BaseVerifier
	// RequiredFields список обязательных полей
	RequiredFields []string
	// ValidateFunc дополнительная функция валидации
	ValidateFunc func(map[string]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *MapVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Проверяем обязательные поля
	for _, field := range v.RequiredFields {
		if _, ok := result[field]; !ok {
			return nil, fmt.Errorf("required field '%s' is missing", field)
		}
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewMapVerifier создает новый экземпляр MapVerifier
func NewMapVerifier(requiredFields []string, validateFunc func(map[string]interface{}) error) *MapVerifier {
	return &MapVerifier{
		RequiredFields: requiredFields,
		ValidateFunc:   validateFunc,
	}
}

// ArrayVerifier верификатор для массивов
type ArrayVerifier struct {
	BaseVerifier
	// MinItems минимальное количество элементов
	MinItems int
	// MaxItems максимальное количество элементов (0 = без ограничения)
	MaxItems int
	// ValidateFunc дополнительная функция валидации
	ValidateFunc func([]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *ArrayVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result []interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Проверяем количество элементов
	if len(result) < v.MinItems {
		return nil, fmt.Errorf("array has %d items, but minimum is %d", len(result), v.MinItems)
	}

	if v.MaxItems > 0 && len(result) > v.MaxItems {
		return nil, fmt.Errorf("array has %d items, but maximum is %d", len(result), v.MaxItems)
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewArrayVerifier создает новый экземпляр ArrayVerifier
func NewArrayVerifier(minItems, maxItems int, validateFunc func([]interface{}) error) *ArrayVerifier {
	return &ArrayVerifier{
		MinItems:     minItems,
		MaxItems:     maxItems,
		ValidateFunc: validateFunc,
	}
}

// SchemaVerifier верификатор с проверкой по JSON Schema (упрощенная версия)
type SchemaVerifier struct {
	BaseVerifier
	// SchemaValidateFunc функция проверки соответствия схеме
	SchemaValidateFunc func(interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *SchemaVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Применяем валидацию по схеме
	if v.SchemaValidateFunc != nil {
		if err := v.SchemaValidateFunc(result); err != nil {
			return nil, fmt.Errorf("schema validation failed: %w", err)
		}
	}

	return result, nil
}

// NewSchemaVerifier создает новый экземпляр SchemaVerifier
func NewSchemaVerifier(schemaValidateFunc func(interface{}) error) *SchemaVerifier {
	return &SchemaVerifier{
		SchemaValidateFunc: schemaValidateFunc,
	}
}

// RepairableVerifier верификатор с возможностью автоматического исправления данных
type RepairableVerifier struct {
	BaseVerifier
	// RepairFunc функция для исправления данных
	RepairFunc func(map[string]interface{}) (map[string]interface{}, error)
	// ValidateFunc функция валидации после исправления
	ValidateFunc func(map[string]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *RepairableVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Применяем исправление если функция задана
	if v.RepairFunc != nil {
		repaired, err := v.RepairFunc(result)
		if err != nil {
			return nil, fmt.Errorf("repair failed: %w", err)
		}
		result = repaired
	}

	// Валидируем после исправления
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation after repair failed: %w", err)
		}
	}

	return result, nil
}

// NewRepairableVerifier создает новый экземпляр RepairableVerifier
func NewRepairableVerifier(repairFunc func(map[string]interface{}) (map[string]interface{}, error), validateFunc func(map[string]interface{}) error) *RepairableVerifier {
	return &RepairableVerifier{
		RepairFunc:   repairFunc,
		ValidateFunc: validateFunc,
	}
}

// Примеры специализированных верификаторов

// EntityExtractionVerifier верификатор для извлечения сущностей
type EntityExtractionVerifier struct {
	GenericVerifier[EntityExtractionResult]
}

// EntityExtractionResult результат извлечения сущностей
type EntityExtractionResult struct {
	Entities []Entity `json:"entities"`
}

// Entity сущность
type Entity struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

// NewEntityExtractionVerifier создает новый верификатор для извлечения сущностей
func NewEntityExtractionVerifier() *EntityExtractionVerifier {
	return &EntityExtractionVerifier{
		GenericVerifier: GenericVerifier[EntityExtractionResult]{
			ValidateFunc: func(result EntityExtractionResult) error {
				if len(result.Entities) == 0 {
					return fmt.Errorf("no entities found")
				}
				for i, entity := range result.Entities {
					if entity.Type == "" {
						return fmt.Errorf("entity %d has empty type", i)
					}
					if entity.Value == "" {
						return fmt.Errorf("entity %d has empty value", i)
					}
				}
				return nil
			},
		},
	}
}

// SummarizationVerifier верификатор для суммаризации
type SummarizationVerifier struct {
	GenericVerifier[SummarizationResult]
}

// SummarizationResult результат суммаризации
type SummarizationResult struct {
	Summary    string   `json:"summary"`
	KeyPoints  []string `json:"keyPoints,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
}

// NewSummarizationVerifier создает новый верификатор для суммаризации
func NewSummarizationVerifier() *SummarizationVerifier {
	return &SummarizationVerifier{
		GenericVerifier: GenericVerifier[SummarizationResult]{
			ValidateFunc: func(result SummarizationResult) error {
				if result.Summary == "" {
					return fmt.Errorf("summary is empty")
				}
				if len(result.Summary) < 10 {
					return fmt.Errorf("summary is too short (minimum 10 characters)")
				}
				return nil
			},
		},
	}
}
